package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

var taskImportLifecycleStateKeys = []string{
	"status", "review_policy", "blocked_reason", "blocked_at", "blocked_from_status",
	"completed_at", "submitted_at", "reviewed_at", "current_submission_id", "version",
}

var taskOutputSubmittedStateKeys = []string{
	"status", "review_policy", "current_submission_id", "submitted_at", "reviewed_at", "version",
	"submission_id", "submission_sequence", "artifact_count", "artifacts",
}

var taskOutputArtifactSnapshotKeys = []string{
	"id", "position", "storage_kind", "name", "mime_type", "size_bytes", "sha256",
	"requires_followup", "produced_by_actor_id", "recorded_by_actor_id",
}

var taskArtifactDeletedSnapshotKeys = []string{
	"id", "submission_id", "position", "storage_kind", "name", "mime_type", "size_bytes", "sha256",
	"requires_followup", "produced_by_actor_id", "recorded_by_actor_id",
}

var taskAssignmentImportStateKeys = []string{
	"id", "task_id", "role", "actor_id", "actor", "assigned_by_actor_id", "assigned_by_actor",
	"assigned_at", "unassigned_at", "reason", "is_active", "inferred",
}

var taskAssignmentActorImportStateKeys = []string{
	"id", "type", "display_name", "status", "is_builtin", "version",
}

type taskArtifactImportReplay struct {
	artifactID           string
	inboxID              string
	taskID               string
	submissionID         string
	submissionStatus     string
	deletedAt            string
	deletedByActorID     string
	deleteReason         string
	previousInboxVersion int64
	deletedInboxVersion  int64
	deletedInboxPrevious map[string]any
	deletedInboxCurrent  map[string]any
}

type taskArtifactImportReplays struct {
	ordered                 []taskArtifactImportReplay
	byArtifactID            map[string]taskArtifactImportReplay
	byInboxID               map[string]taskArtifactImportReplay
	taskIDs                 map[string]struct{}
	changesRequestedTaskIDs map[string]struct{}
}

type taskArtifactSubmittedImportProof struct {
	event       automationImportWorkflowEvent
	taskVersion int64
}

type taskArtifactSubmittedImportValidation struct {
	taskID    string
	createdAt string
	proof     taskArtifactSubmittedImportProof
	valid     bool
}

type taskArtifactImportValidationIndex struct {
	events                   automationImportEventIndex
	assignments              []map[string]any
	artifactsBySubmission    map[string][]map[string]any
	submittedBySubmissionID  map[string]taskArtifactSubmittedImportValidation
	deletedEventByArtifactID map[string]automationImportWorkflowEvent
}

type taskArtifactSourceDeletedImportProof struct {
	event           automationImportWorkflowEvent
	previous        map[string]any
	current         map[string]any
	previousVersion int64
	currentVersion  int64
}

type taskArtifactSubmissionDispositionImportProof struct {
	status      string
	eventTime   time.Time
	taskVersion int64
}

type taskArtifactAssignmentImportRow struct {
	id             string
	taskID         string
	actorID        string
	assignedByID   string
	role           string
	assignedAt     string
	unassignedAt   *string
	reason         string
	assignedTime   time.Time
	unassignedTime *time.Time
}

type taskArtifactAssignmentImportEvents struct {
	created   map[string][]automationImportWorkflowEvent
	ended     map[string][]automationImportWorkflowEvent
	inbound   map[string][]automationImportWorkflowEvent
	outbound  map[string][]automationImportWorkflowEvent
	migration map[string][]automationImportWorkflowEvent
}

type taskArtifactInboxImportIdentity struct {
	artifactID string
	sourceKey  string
	payload    map[string]any
}

const taskArtifactInboxGapMigrationAction = "migration_task_artifact_inbox_gap"

// Artifact replay Tasks can retain ended assignment history even after a Task
// is reopened. Import restores only ended rows before their deferred Task
// foreign keys exist; active rows keep the ordinary online insert guards.
func validTaskArtifactAssignmentImportHistory(
	packageData businessExportPackage,
	replays taskArtifactImportReplays,
) bool {
	tables := make(map[string]businessExportTable, len(packageData.Tables))
	for _, table := range packageData.Tables {
		tables[table.Name] = table
	}
	_, _, ok := partitionTaskArtifactHistoricalAssignmentRows(
		tables["tasks"], tables["task_assignments"], replays.taskIDs,
	)
	if !ok {
		return false
	}
	events, ok := automationImportWorkflowEvents(tables["workflow_events"])
	tasks, tasksOK := automationImportRowsByID(tables["tasks"])
	actors, actorsOK := automationImportRowsByID(tables["actors"])
	assignments, assignmentsOK := automationImportRowsByID(tables["task_assignments"])
	return ok && tasksOK && actorsOK && assignmentsOK &&
		validTaskArtifactAssignmentImportGraph(
			tables["task_assignments"], replays, assignments, tasks, events, actors,
		)
}

func partitionTaskArtifactHistoricalAssignmentRows(
	tasksTable businessExportTable,
	assignmentsTable businessExportTable,
	protectedTaskIDs map[string]struct{},
) (businessExportTable, businessExportTable, bool) {
	tasks, ok := automationImportRowsByID(tasksTable)
	if !ok {
		return businessExportTable{}, businessExportTable{}, false
	}
	historical := businessExportTable{
		Name: assignmentsTable.Name, Columns: append([]string(nil), assignmentsTable.Columns...), Rows: make([][]any, 0),
	}
	remaining := businessExportTable{
		Name: assignmentsTable.Name, Columns: append([]string(nil), assignmentsTable.Columns...), Rows: make([][]any, 0, len(assignmentsTable.Rows)),
	}
	taskIDIndex := columnIndex(assignmentsTable.Columns, "task_id")
	unassignedAtIndex := columnIndex(assignmentsTable.Columns, "unassigned_at")
	if taskIDIndex < 0 || unassignedAtIndex < 0 {
		return businessExportTable{}, businessExportTable{}, false
	}
	for _, row := range assignmentsTable.Rows {
		taskID, taskIDOK := row[taskIDIndex].(string)
		if !taskIDOK || strings.TrimSpace(taskID) == "" {
			remaining.Rows = append(remaining.Rows, row)
			continue
		}
		if _, protected := protectedTaskIDs[taskID]; !protected {
			remaining.Rows = append(remaining.Rows, row)
			continue
		}
		task, taskExists := tasks[taskID]
		status, statusOK := task["status"].(string)
		if !taskExists || !statusOK {
			return businessExportTable{}, businessExportTable{}, false
		}
		unassignedAtValue := row[unassignedAtIndex]
		if unassignedAtValue == nil {
			if status == "done" || status == "cancelled" {
				return businessExportTable{}, businessExportTable{}, false
			}
			remaining.Rows = append(remaining.Rows, row)
			continue
		}
		unassignedAt, valueOK := unassignedAtValue.(string)
		if !valueOK || strings.TrimSpace(unassignedAt) == "" {
			return businessExportTable{}, businessExportTable{}, false
		}
		historical.Rows = append(historical.Rows, row)
	}
	return historical, remaining, true
}

func validTaskArtifactHistoricalAssignmentImportRows(
	table businessExportTable,
	assignmentRows map[string]map[string]any,
	events automationImportEventIndex,
	actors map[string]map[string]any,
) bool {
	for _, row := range automationImportRows(table) {
		id, idOK := row["id"].(string)
		taskID, taskIDOK := row["task_id"].(string)
		actorID, actorIDOK := row["actor_id"].(string)
		assignedByID, assignedByOK := row["assigned_by_actor_id"].(string)
		role, roleOK := row["role"].(string)
		assignedAt, assignedAtOK := row["assigned_at"].(string)
		unassignedAt, unassignedAtOK := row["unassigned_at"].(string)
		reason, reasonOK := row["reason"].(string)
		assignedTime, assignedTimeOK := taskArtifactImportTime(assignedAt)
		unassignedTime, unassignedTimeOK := taskArtifactImportTime(unassignedAt)
		if !idOK || !validCanonicalAutomationUUID(id) || !taskIDOK || !validCanonicalAutomationUUID(taskID) ||
			!actorIDOK || !validCanonicalAutomationUUID(actorID) || !assignedByOK || !validCanonicalAutomationUUID(assignedByID) ||
			!roleOK || (role != "assignee" && role != "reviewer") || !assignedAtOK || !unassignedAtOK ||
			!reasonOK || strings.TrimSpace(reason) == "" || strings.TrimSpace(reason) != reason || utf8.RuneCountInString(reason) > 1_000 ||
			!assignedTimeOK || !unassignedTimeOK || assignedByID != models.BuiltinOwnerActorID ||
			unassignedTime.Before(assignedTime) {
			return false
		}
		for _, referencedActorID := range []string{actorID, assignedByID} {
			actor, exists := actors[referencedActorID]
			status, statusOK := actor["status"].(string)
			if !exists || !statusOK || (status != "active" && status != "inactive") {
				return false
			}
		}
		if !validTaskArtifactAssignmentActorRole(actors, actorID, role) {
			return false
		}
		ended := make([]automationImportWorkflowEvent, 0, 1)
		for _, event := range automationImportAggregateActionEvents(events, "task", taskID, "assignment_ended") {
			if event.assignmentID != nil && *event.assignmentID == id {
				ended = append(ended, event)
			}
		}
		if len(ended) == 1 {
			if !validTaskArtifactAssignmentEndedImportEvent(
				events, actors, ended[0], row, id, taskID, actorID, assignedByID, role, assignedAt, unassignedAt, reason,
			) {
				return false
			}
			continue
		}
		if len(ended) > 1 {
			return false
		}
		if validTaskArtifactMigrationAssignmentImportHistory(events, row) ||
			validTaskArtifactAssignmentReassignedImportEvent(
				events, assignmentRows, actors, row, id, taskID, actorID, assignedByID, role, assignedAt, unassignedAt, reason,
			) {
			continue
		}
		return false
	}
	return true
}

func validTaskArtifactAssignmentImportGraph(
	table businessExportTable,
	replays taskArtifactImportReplays,
	assignments map[string]map[string]any,
	tasks map[string]map[string]any,
	events automationImportEventIndex,
	actors map[string]map[string]any,
) bool {
	protectedTaskIDs := replays.taskIDs
	if len(protectedTaskIDs) == 0 {
		return true
	}
	indexed, ok := taskArtifactAssignmentImportEventIndex(protectedTaskIDs, assignments, events)
	if !ok {
		return false
	}
	reopenedAt := make(map[string]time.Time, len(protectedTaskIDs))
	for taskID := range protectedTaskIDs {
		task, exists := tasks[taskID]
		status, statusOK := task["status"].(string)
		if !exists || !statusOK {
			return false
		}
		if status != "done" && status != "cancelled" {
			reopened := automationImportAggregateActionEvents(events, "task", taskID, "task_reopened")
			if len(reopened) != 0 {
				latest, valid := validTaskArtifactLatestReopenImportTime(events, taskID, task)
				if !valid {
					return false
				}
				reopenedAt[taskID] = latest
			} else {
				if _, changesRequested := replays.changesRequestedTaskIDs[taskID]; !changesRequested ||
					taskArtifactHasTerminalImportEvent(events, taskID) {
					return false
				}
			}
		}
	}

	seen := make(map[string]struct{})
	rows := make(map[string]taskArtifactAssignmentImportRow)
	originTimes := make(map[string]time.Time)
	originParents := make(map[string]string)
	activeIDs := make([]string, 0)
	for _, raw := range automationImportRows(table) {
		taskID, taskIDOK := raw["task_id"].(string)
		if !taskIDOK {
			continue
		}
		if _, protected := protectedTaskIDs[taskID]; !protected {
			continue
		}
		row, valid := taskArtifactAssignmentImportRowValues(raw, actors)
		if !valid {
			return false
		}
		seen[row.id] = struct{}{}
		rows[row.id] = row
		originTime, migrationOrigin, originParent, valid := validTaskArtifactAssignmentImportOrigin(
			row, raw, assignments, tasks[taskID], indexed, events, actors,
		)
		if !valid {
			return false
		}
		originTimes[row.id] = originTime
		originParents[row.id] = originParent
		terminationCount := 0
		if direct := indexed.ended[row.id]; len(direct) != 0 {
			if len(direct) != 1 || row.unassignedAt == nil || !validTaskArtifactAssignmentEndedImportEvent(
				events, actors, direct[0], raw, row.id, row.taskID, row.actorID, row.assignedByID,
				row.role, row.assignedAt, *row.unassignedAt, row.reason,
			) {
				return false
			}
			terminationCount++
		}
		if reassigned := indexed.outbound[row.id]; len(reassigned) != 0 {
			if len(reassigned) != 1 || row.unassignedAt == nil || !validTaskArtifactAssignmentReassignedImportEvent(
				events, assignments, actors, raw, row.id, row.taskID, row.actorID, row.assignedByID,
				row.role, row.assignedAt, *row.unassignedAt, row.reason,
			) {
				return false
			}
			terminationCount++
		}
		if row.unassignedAt == nil {
			if terminationCount != 0 {
				return false
			}
			activeIDs = append(activeIDs, row.id)
			continue
		}
		if terminationCount == 0 && migrationOrigin && row.reason == migrationAssignmentReason {
			terminationCount = 1
		}
		if terminationCount != 1 {
			return false
		}
	}
	if !validTaskArtifactAssignmentChains(rows, indexed, originTimes, originParents, activeIDs, reopenedAt) ||
		!validTaskArtifactAssignmentIntervals(rows) {
		return false
	}
	for id, raw := range assignments {
		taskID, taskIDOK := raw["task_id"].(string)
		if !taskIDOK {
			continue
		}
		if _, protected := protectedTaskIDs[taskID]; protected {
			if _, exists := seen[id]; !exists {
				return false
			}
		}
	}
	return true
}

func taskArtifactAssignmentImportEventIndex(
	protectedTaskIDs map[string]struct{},
	assignments map[string]map[string]any,
	events automationImportEventIndex,
) (taskArtifactAssignmentImportEvents, bool) {
	result := taskArtifactAssignmentImportEvents{
		created: make(map[string][]automationImportWorkflowEvent), migration: make(map[string][]automationImportWorkflowEvent),
		ended: make(map[string][]automationImportWorkflowEvent), inbound: make(map[string][]automationImportWorkflowEvent),
		outbound: make(map[string][]automationImportWorkflowEvent),
	}
	for taskID := range protectedTaskIDs {
		for _, event := range automationImportAggregateEvents(events, "task", taskID) {
			switch event.action {
			case "assignment_created", "assignment_ended", "migration_assignment_backfill":
				if event.assignmentID == nil || !taskArtifactAssignmentRowBelongsToTask(assignments, *event.assignmentID, taskID) {
					return taskArtifactAssignmentImportEvents{}, false
				}
				switch event.action {
				case "assignment_created":
					result.created[*event.assignmentID] = append(result.created[*event.assignmentID], event)
				case "assignment_ended":
					result.ended[*event.assignmentID] = append(result.ended[*event.assignmentID], event)
				case "migration_assignment_backfill":
					result.migration[*event.assignmentID] = append(result.migration[*event.assignmentID], event)
				}
			case "assignment_reassigned":
				if event.assignmentID == nil || event.previousJSON == nil || event.currentJSON == nil {
					return taskArtifactAssignmentImportEvents{}, false
				}
				previous, previousOK := automationImportJSONObject(*event.previousJSON)
				current, currentOK := automationImportJSONObject(*event.currentJSON)
				ended, endedOK := current["ended_assignment"].(map[string]any)
				created, createdOK := current["assignment"].(map[string]any)
				oldID, oldIDOK := ended["id"].(string)
				previousID, previousIDOK := previous["id"].(string)
				newID, newIDOK := created["id"].(string)
				if !previousOK || !currentOK || !endedOK || !createdOK ||
					!automationImportObjectHasExactKeys(previous, taskAssignmentImportStateKeys) ||
					!automationImportObjectHasExactKeys(current, []string{"ended_assignment", "assignment"}) ||
					!automationImportObjectHasExactKeys(ended, taskAssignmentImportStateKeys) ||
					!automationImportObjectHasExactKeys(created, taskAssignmentImportStateKeys) ||
					!oldIDOK || !previousIDOK || previousID != oldID || !newIDOK || *event.assignmentID != newID ||
					oldID == newID || !taskArtifactAssignmentRowBelongsToTask(assignments, oldID, taskID) ||
					!taskArtifactAssignmentRowBelongsToTask(assignments, newID, taskID) {
					return taskArtifactAssignmentImportEvents{}, false
				}
				result.outbound[oldID] = append(result.outbound[oldID], event)
				result.inbound[newID] = append(result.inbound[newID], event)
			}
		}
	}
	return result, true
}

func taskArtifactAssignmentRowBelongsToTask(assignments map[string]map[string]any, assignmentID, taskID string) bool {
	row, exists := assignments[assignmentID]
	return exists && row["task_id"] == taskID
}

func taskArtifactAssignmentImportRowValues(
	row map[string]any,
	actors map[string]map[string]any,
) (taskArtifactAssignmentImportRow, bool) {
	id, idOK := row["id"].(string)
	taskID, taskIDOK := row["task_id"].(string)
	actorID, actorIDOK := row["actor_id"].(string)
	assignedByID, assignedByOK := row["assigned_by_actor_id"].(string)
	role, roleOK := row["role"].(string)
	assignedAt, assignedAtOK := row["assigned_at"].(string)
	unassignedAt, unassignedAtOK := automationImportOptionalString(row["unassigned_at"])
	reason, reasonOK := row["reason"].(string)
	assignedTime, assignedTimeOK := taskArtifactImportTime(assignedAt)
	if !idOK || !validCanonicalAutomationUUID(id) || !taskIDOK || !validCanonicalAutomationUUID(taskID) ||
		!actorIDOK || !validCanonicalAutomationUUID(actorID) || !assignedByOK || assignedByID != models.BuiltinOwnerActorID ||
		!roleOK || (role != "assignee" && role != "reviewer") || !assignedAtOK || !assignedTimeOK ||
		!unassignedAtOK || !reasonOK || !validTaskArtifactAssignmentActorRole(actors, actorID, role) ||
		!validTaskArtifactFinalAssignmentActor(actors, assignedByID, "owner", true) {
		return taskArtifactAssignmentImportRow{}, false
	}
	result := taskArtifactAssignmentImportRow{
		id: id, taskID: taskID, actorID: actorID, assignedByID: assignedByID, role: role,
		assignedAt: assignedAt, unassignedAt: unassignedAt, reason: reason, assignedTime: assignedTime,
	}
	if unassignedAt == nil {
		status, statusOK := actors[actorID]["status"].(string)
		if !statusOK || status != "active" || (reason != "" && reason != migrationAssignmentReason) {
			return taskArtifactAssignmentImportRow{}, false
		}
		return result, true
	}
	unassignedTime, timeOK := taskArtifactImportTime(*unassignedAt)
	if !timeOK || unassignedTime.Before(assignedTime) || strings.TrimSpace(reason) == "" ||
		strings.TrimSpace(reason) != reason || utf8.RuneCountInString(reason) > 1_000 {
		return taskArtifactAssignmentImportRow{}, false
	}
	result.unassignedTime = &unassignedTime
	return result, true
}

func validTaskArtifactFinalAssignmentActor(
	actors map[string]map[string]any,
	actorID, actorType string,
	isBuiltin bool,
) bool {
	actor, exists := actors[actorID]
	actualType, typeOK := actor["type"].(string)
	status, statusOK := actor["status"].(string)
	builtin, builtinOK := automationImportInt64(actor["is_builtin"])
	version, versionOK := automationImportInt64(actor["version"])
	return exists && typeOK && actualType == actorType && statusOK && status == "active" &&
		builtinOK && (builtin == 1) == isBuiltin && versionOK && version >= 1
}

func validTaskArtifactAssignmentImportOrigin(
	row taskArtifactAssignmentImportRow,
	raw map[string]any,
	assignments map[string]map[string]any,
	task map[string]any,
	indexed taskArtifactAssignmentImportEvents,
	events automationImportEventIndex,
	actors map[string]map[string]any,
) (time.Time, bool, string, bool) {
	originCount := 0
	originTime := time.Time{}
	migrationOrigin := false
	originParent := ""
	if created := indexed.created[row.id]; len(created) != 0 {
		if len(created) != 1 || !validTaskArtifactAssignmentCreatedImportEvent(created[0], raw, actors) {
			return time.Time{}, false, "", false
		}
		parsed, ok := taskArtifactImportTime(created[0].createdAt)
		if !ok {
			return time.Time{}, false, "", false
		}
		originCount++
		originTime = parsed
	}
	if inbound := indexed.inbound[row.id]; len(inbound) != 0 {
		if len(inbound) != 1 || inbound[0].currentJSON == nil {
			return time.Time{}, false, "", false
		}
		current, currentOK := automationImportJSONObject(*inbound[0].currentJSON)
		ended, endedOK := current["ended_assignment"].(map[string]any)
		oldID, oldIDOK := ended["id"].(string)
		oldRaw, oldExists := assignments[oldID]
		oldRow, oldOK := taskArtifactAssignmentImportRowValues(oldRaw, actors)
		if !currentOK || !endedOK || !oldIDOK || !oldExists || !oldOK || oldRow.unassignedAt == nil ||
			!validTaskArtifactAssignmentReassignedImportEvent(
				events, assignments, actors, oldRaw, oldRow.id, oldRow.taskID, oldRow.actorID, oldRow.assignedByID,
				oldRow.role, oldRow.assignedAt, *oldRow.unassignedAt, oldRow.reason,
			) {
			return time.Time{}, false, "", false
		}
		parsed, ok := taskArtifactImportTime(inbound[0].createdAt)
		if !ok {
			return time.Time{}, false, "", false
		}
		originCount++
		originTime = parsed
		originParent = oldID
	}
	if migration := indexed.migration[row.id]; len(migration) != 0 {
		if len(migration) != 1 || !validTaskArtifactMigrationAssignmentOriginForTask(events, raw, task) {
			return time.Time{}, false, "", false
		}
		parsed, ok := taskArtifactImportTime(migration[0].createdAt)
		if !ok {
			return time.Time{}, false, "", false
		}
		originCount++
		originTime = parsed
		migrationOrigin = true
	}
	if originCount != 1 || (migrationOrigin && row.reason != migrationAssignmentReason && row.unassignedAt == nil) ||
		(!migrationOrigin && row.reason != "" && row.unassignedAt == nil) {
		return time.Time{}, false, "", false
	}
	return originTime, migrationOrigin, originParent, true
}

func validTaskArtifactAssignmentChains(
	rows map[string]taskArtifactAssignmentImportRow,
	indexed taskArtifactAssignmentImportEvents,
	originTimes map[string]time.Time,
	originParents map[string]string,
	activeIDs []string,
	reopenedAt map[string]time.Time,
) bool {
	next := make(map[string]string)
	for oldID, events := range indexed.outbound {
		if len(events) != 1 || events[0].assignmentID == nil {
			return false
		}
		newID := *events[0].assignmentID
		if _, oldExists := rows[oldID]; !oldExists {
			return false
		}
		if _, newExists := rows[newID]; !newExists || originParents[newID] != oldID {
			return false
		}
		next[oldID] = newID
	}
	colors := make(map[string]uint8, len(rows))
	var visit func(string) bool
	visit = func(id string) bool {
		switch colors[id] {
		case 1:
			return false
		case 2:
			return true
		}
		colors[id] = 1
		if nextID := next[id]; nextID != "" && !visit(nextID) {
			return false
		}
		colors[id] = 2
		return true
	}
	for id := range rows {
		if !visit(id) {
			return false
		}
	}
	for _, activeID := range activeIDs {
		current := activeID
		visited := make(map[string]struct{})
		for originParents[current] != "" {
			if _, duplicate := visited[current]; duplicate {
				return false
			}
			visited[current] = struct{}{}
			current = originParents[current]
			if _, exists := rows[current]; !exists {
				return false
			}
		}
		row := rows[activeID]
		reopenTime, exists := reopenedAt[row.taskID]
		rootTime, rootExists := originTimes[current]
		if !rootExists || (exists && rootTime.Before(reopenTime)) {
			return false
		}
	}
	return true
}

func validTaskArtifactAssignmentIntervals(rows map[string]taskArtifactAssignmentImportRow) bool {
	groups := make(map[string][]taskArtifactAssignmentImportRow)
	for _, row := range rows {
		key := row.taskID + "\x00" + row.role
		groups[key] = append(groups[key], row)
	}
	for _, group := range groups {
		sort.Slice(group, func(left, right int) bool {
			if !group[left].assignedTime.Equal(group[right].assignedTime) {
				return group[left].assignedTime.Before(group[right].assignedTime)
			}
			if (group[left].unassignedAt == nil) != (group[right].unassignedAt == nil) {
				return group[left].unassignedAt != nil
			}
			if group[left].unassignedTime != nil && group[right].unassignedTime != nil &&
				!group[left].unassignedTime.Equal(*group[right].unassignedTime) {
				return group[left].unassignedTime.Before(*group[right].unassignedTime)
			}
			return group[left].id < group[right].id
		})
		for index := 1; index < len(group); index++ {
			previous := group[index-1]
			if previous.unassignedTime == nil || group[index].assignedTime.Before(*previous.unassignedTime) {
				return false
			}
		}
	}
	return true
}

func validTaskArtifactAssignmentCreatedImportEvent(
	event automationImportWorkflowEvent,
	row map[string]any,
	actors map[string]map[string]any,
) bool {
	id, idOK := row["id"].(string)
	taskID, taskIDOK := row["task_id"].(string)
	assignedAt, assignedAtOK := row["assigned_at"].(string)
	if !idOK || !taskIDOK || !assignedAtOK ||
		!validTaskArtifactImportEventEnvelope(event, models.BuiltinOwnerActorID, true) ||
		event.createdAt != assignedAt || event.assignmentID == nil || *event.assignmentID != id ||
		event.submissionID != nil || event.artifactID != nil || event.agentRunID != nil ||
		(event.commandSeq != nil && *event.commandSeq < 1) || event.previousJSON != nil || event.currentJSON == nil {
		return false
	}
	current, ok := automationImportJSONObject(*event.currentJSON)
	return ok && validTaskArtifactActiveAssignmentImportState(current, row, taskID, actors)
}

func validTaskArtifactActiveAssignmentImportState(
	state map[string]any,
	row map[string]any,
	taskID string,
	actors map[string]map[string]any,
) bool {
	id, idOK := row["id"].(string)
	actorID, actorIDOK := row["actor_id"].(string)
	role, roleOK := row["role"].(string)
	assignedAt, assignedAtOK := row["assigned_at"].(string)
	if !idOK || !actorIDOK || !roleOK || !assignedAtOK ||
		!automationImportObjectHasExactKeys(state, taskAssignmentImportStateKeys) ||
		state["id"] != id || state["task_id"] != taskID || state["role"] != role ||
		state["actor_id"] != actorID || state["assigned_by_actor_id"] != models.BuiltinOwnerActorID ||
		state["assigned_at"] != assignedAt || state["unassigned_at"] != nil || state["reason"] != nil ||
		state["is_active"] != true || state["inferred"] != false {
		return false
	}
	for key, expectedID := range map[string]string{
		"actor": actorID, "assigned_by_actor": models.BuiltinOwnerActorID,
	} {
		actor, ok := state[key].(map[string]any)
		if !ok || !validTaskArtifactAssignmentActorImportState(actor, expectedID, actors) {
			return false
		}
	}
	return validTaskArtifactAssignmentActorRole(actors, actorID, role)
}

func validTaskArtifactLatestReopenImportTime(
	events automationImportEventIndex,
	taskID string,
	task map[string]any,
) (time.Time, bool) {
	finalVersion, finalVersionOK := automationImportInt64(task["version"])
	if !finalVersionOK {
		return time.Time{}, false
	}
	latestVersion := int64(0)
	latestTime := time.Time{}
	seenVersions := make(map[int64]struct{})
	seenTerminalEvents := make(map[string]struct{})
	type reopenPoint struct {
		version int64
		at      time.Time
	}
	points := make([]reopenPoint, 0)
	reopened := automationImportAggregateActionEvents(events, "task", taskID, "task_reopened")
	if len(reopened) == 0 {
		return time.Time{}, false
	}
	for _, event := range reopened {
		if !validTaskArtifactImportEventEnvelope(event, models.BuiltinOwnerActorID, true) ||
			event.assignmentID != nil || event.submissionID != nil || event.artifactID != nil || event.agentRunID != nil ||
			event.commandSeq == nil || *event.commandSeq != 1 || event.previousJSON == nil || event.currentJSON == nil {
			return time.Time{}, false
		}
		previous, previousOK := automationImportJSONObject(*event.previousJSON)
		current, currentOK := automationImportJSONObject(*event.currentJSON)
		previousVersion, previousVersionOK := automationImportJSONInt64(previous["version"])
		currentVersion, currentVersionOK := automationImportJSONInt64(current["version"])
		createdAt, createdAtOK := taskArtifactImportTime(event.createdAt)
		terminalEventID, terminalOK := validTaskArtifactReopenTerminalPredecessor(
			events, taskID, event, previous, previousVersion, createdAt,
		)
		if !previousOK || !currentOK || !previousVersionOK || !currentVersionOK || !createdAtOK ||
			!automationImportObjectHasExactKeys(previous, taskImportLifecycleStateKeys) ||
			!automationImportObjectHasExactKeys(current, taskImportLifecycleStateKeys) ||
			(previous["status"] != "done" && previous["status"] != "cancelled") || current["status"] != "todo" ||
			previous["review_policy"] != "manual" || current["review_policy"] != "manual" ||
			current["blocked_reason"] != nil || current["blocked_at"] != nil || current["blocked_from_status"] != nil ||
			current["completed_at"] != nil || current["submitted_at"] != nil || current["reviewed_at"] != nil ||
			current["current_submission_id"] != nil || currentVersion != previousVersion+1 || finalVersion < currentVersion || !terminalOK {
			return time.Time{}, false
		}
		if _, reused := seenTerminalEvents[terminalEventID]; reused {
			return time.Time{}, false
		}
		seenTerminalEvents[terminalEventID] = struct{}{}
		if _, duplicate := seenVersions[currentVersion]; duplicate {
			return time.Time{}, false
		}
		seenVersions[currentVersion] = struct{}{}
		points = append(points, reopenPoint{version: currentVersion, at: createdAt})
		if currentVersion > latestVersion {
			latestVersion = currentVersion
			latestTime = createdAt
		}
	}
	sort.Slice(points, func(left, right int) bool { return points[left].version < points[right].version })
	for index := 1; index < len(points); index++ {
		if points[index].at.Before(points[index-1].at) {
			return time.Time{}, false
		}
	}
	return latestTime, latestVersion > 0
}

func validTaskArtifactReopenTerminalPredecessor(
	events automationImportEventIndex,
	taskID string,
	reopened automationImportWorkflowEvent,
	reopenPrevious map[string]any,
	previousVersion int64,
	reopenedAt time.Time,
) (string, bool) {
	type terminalCandidate struct {
		action          string
		event           automationImportWorkflowEvent
		previous        map[string]any
		current         map[string]any
		previousVersion int64
		currentVersion  int64
	}
	selected := terminalCandidate{}
	selectedFound := false
	for _, action := range []string{"task_completed", "task_cancelled", "task_review_accepted"} {
		for _, terminal := range automationImportAggregateActionEvents(events, "task", taskID, action) {
			terminalAt, terminalAtOK := automationImportTime(terminal.createdAt)
			if !terminalAtOK {
				return "", false
			}
			if terminalAt.After(reopenedAt) {
				continue
			}
			if terminal.previousJSON == nil || terminal.currentJSON == nil {
				return "", false
			}
			previous, previousOK := automationImportJSONObject(*terminal.previousJSON)
			current, currentOK := automationImportJSONObject(*terminal.currentJSON)
			currentVersion, currentVersionOK := automationImportJSONInt64(current["version"])
			previousTerminalVersion, previousTerminalVersionOK := automationImportJSONInt64(previous["version"])
			if !previousOK || !currentOK || !currentVersionOK || !previousTerminalVersionOK {
				return "", false
			}
			if currentVersion > previousVersion {
				continue
			}
			if !selectedFound || currentVersion > selected.currentVersion {
				selected = terminalCandidate{
					action: action, event: terminal, previous: previous, current: current,
					previousVersion: previousTerminalVersion, currentVersion: currentVersion,
				}
				selectedFound = true
			} else if currentVersion == selected.currentVersion {
				return "", false
			}
		}
	}
	if !selectedFound || selected.action == "task_completed" || reopened.aggregateID != taskID ||
		!validTaskArtifactImportEventEnvelope(selected.event, models.BuiltinOwnerActorID, true) ||
		selected.event.assignmentID != nil || selected.event.artifactID != nil || selected.event.agentRunID != nil ||
		selected.event.commandSeq == nil || *selected.event.commandSeq < 1 ||
		selected.currentVersion != selected.previousVersion+1 ||
		!taskArtifactLifecycleStateMatchesExceptVersion(selected.current, reopenPrevious) {
		return "", false
	}
	switch selected.action {
	case "task_cancelled":
		if selected.event.submissionID == nil &&
			validTaskArtifactReopenCancelledPredecessor(selected.previous, selected.current) {
			return selected.event.id, true
		}
	case "task_review_accepted":
		if selected.event.submissionID != nil &&
			validTaskArtifactReopenAcceptedPredecessor(selected.event, selected.previous, selected.current) {
			return selected.event.id, true
		}
	}
	return "", false
}

func validTaskArtifactReopenCancelledPredecessor(previous, current map[string]any) bool {
	reason, reasonOK := current["reason"].(string)
	if !automationImportObjectHasExactKeys(previous, taskImportLifecycleStateKeys) ||
		!taskArtifactLifecycleStateHasExactKeys(current, true) || !reasonOK || strings.TrimSpace(reason) != reason ||
		reason == "" || utf8.RuneCountInString(reason) > 1_000 ||
		(previous["status"] != "todo" && previous["status"] != "in_progress" && previous["status"] != "blocked" &&
			previous["status"] != "waiting_review") || previous["review_policy"] != "manual" ||
		current["status"] != "cancelled" || current["review_policy"] != "manual" || current["blocked_reason"] != nil ||
		current["blocked_at"] != nil || current["blocked_from_status"] != nil || current["completed_at"] != nil {
		return false
	}
	for _, key := range []string{"review_policy", "submitted_at", "reviewed_at", "current_submission_id"} {
		if !taskArtifactImportValueEquals(previous[key], current[key]) {
			return false
		}
	}
	if previous["status"] != "blocked" {
		return previous["blocked_reason"] == nil && previous["blocked_at"] == nil && previous["blocked_from_status"] == nil
	}
	blockedReason, blockedReasonOK := previous["blocked_reason"].(string)
	blockedAt, blockedAtOK := previous["blocked_at"].(string)
	blockedFrom, blockedFromOK := previous["blocked_from_status"].(string)
	_, blockedTimeOK := automationImportTime(blockedAt)
	return blockedReasonOK && strings.TrimSpace(blockedReason) == blockedReason && blockedReason != "" &&
		blockedAtOK && blockedTimeOK && blockedFromOK &&
		(blockedFrom == "todo" || blockedFrom == "in_progress" || blockedFrom == "waiting_review")
}

func validTaskArtifactReopenAcceptedPredecessor(
	event automationImportWorkflowEvent,
	previous, current map[string]any,
) bool {
	includeReason := false
	if _, exists := current["reason"]; exists {
		includeReason = true
		reason, ok := current["reason"].(string)
		if !ok || strings.TrimSpace(reason) != reason || reason == "" || utf8.RuneCountInString(reason) > 1_000 {
			return false
		}
	}
	if event.submissionID == nil || !taskArtifactReviewStateHasExactKeys(previous, false) ||
		!taskArtifactReviewStateHasExactKeys(current, includeReason) || previous["status"] != "waiting_review" ||
		previous["review_policy"] != "manual" || previous["blocked_reason"] != nil || previous["blocked_at"] != nil ||
		previous["blocked_from_status"] != nil || previous["completed_at"] != nil || previous["reviewed_at"] != nil ||
		previous["current_submission_id"] != *event.submissionID || previous["submission_id"] != *event.submissionID ||
		previous["submission_status"] != "pending_review" || current["status"] != "done" ||
		current["review_policy"] != "manual" || current["blocked_reason"] != nil || current["blocked_at"] != nil ||
		current["blocked_from_status"] != nil || current["completed_at"] != event.createdAt ||
		current["reviewed_at"] != event.createdAt || current["current_submission_id"] != *event.submissionID ||
		current["submission_id"] != *event.submissionID || current["submission_status"] != "accepted" ||
		!taskArtifactImportValueEquals(previous["submitted_at"], current["submitted_at"]) {
		return false
	}
	return true
}

func taskArtifactLifecycleStateMatchesExceptVersion(actual, expected map[string]any) bool {
	for _, key := range taskImportLifecycleStateKeys {
		if key == "version" {
			continue
		}
		if !taskArtifactImportValueEquals(actual[key], expected[key]) {
			return false
		}
	}
	return true
}

func taskArtifactHasTerminalImportEvent(events automationImportEventIndex, taskID string) bool {
	for _, action := range []string{"task_completed", "task_cancelled", "task_review_accepted"} {
		if len(automationImportAggregateActionEvents(events, "task", taskID, action)) != 0 {
			return true
		}
	}
	return false
}

func validTaskArtifactMigrationAssignmentOriginForTask(
	events automationImportEventIndex,
	row map[string]any,
	task map[string]any,
) bool {
	if !validTaskArtifactMigrationAssignmentOrigin(events, row) {
		return false
	}
	taskID, taskIDOK := row["task_id"].(string)
	assignmentID, assignmentIDOK := row["id"].(string)
	assignedAt, assignedAtOK := row["assigned_at"].(string)
	taskCreatedAt, taskCreatedAtOK := task["created_at"].(string)
	if !taskIDOK || !assignmentIDOK || !assignedAtOK || !taskCreatedAtOK || assignedAt != taskCreatedAt {
		return false
	}
	taskCreatedTime, createdOK := taskArtifactImportTime(taskCreatedAt)
	for _, event := range automationImportAggregateActionEvents(events, "task", taskID, "migration_assignment_backfill") {
		if event.assignmentID == nil || *event.assignmentID != assignmentID {
			continue
		}
		eventTime, eventTimeOK := taskArtifactImportTime(event.createdAt)
		return createdOK && eventTimeOK && !eventTime.Before(taskCreatedTime)
	}
	return false
}

func taskArtifactImportTime(value string) (time.Time, bool) {
	if parsed, ok := automationImportTime(value); ok {
		return parsed, true
	}
	const sqliteTimestamp = "2006-01-02 15:04:05"
	parsed, err := time.ParseInLocation(sqliteTimestamp, value, time.UTC)
	return parsed, err == nil && parsed.Format(sqliteTimestamp) == value
}

func validTaskArtifactAssignmentEndedImportEvent(
	events automationImportEventIndex,
	actors map[string]map[string]any,
	event automationImportWorkflowEvent,
	row map[string]any,
	id, taskID, actorID, assignedByID, role, assignedAt, unassignedAt, reason string,
) bool {
	if !validTaskArtifactImportEventEnvelope(event, models.BuiltinOwnerActorID, true) ||
		event.createdAt != unassignedAt || event.assignmentID == nil || *event.assignmentID != id ||
		event.submissionID != nil || event.artifactID != nil || event.agentRunID != nil ||
		(event.commandSeq != nil && *event.commandSeq < 1) || event.previousJSON == nil || event.currentJSON == nil {
		return false
	}
	previous, previousOK := automationImportJSONObject(*event.previousJSON)
	current, currentOK := automationImportJSONObject(*event.currentJSON)
	return previousOK && currentOK && validTaskArtifactEndedAssignmentImportStates(
		events, actors, previous, current, row, id, taskID, actorID, assignedByID, role, assignedAt, unassignedAt, reason,
	)
}

func validTaskArtifactEndedAssignmentImportStates(
	events automationImportEventIndex,
	actors map[string]map[string]any,
	previous, current, row map[string]any,
	id, taskID, actorID, assignedByID, role, assignedAt, unassignedAt, reason string,
) bool {
	if !automationImportObjectHasExactKeys(previous, taskAssignmentImportStateKeys) ||
		!automationImportObjectHasExactKeys(current, taskAssignmentImportStateKeys) ||
		previous["id"] != id || current["id"] != id || previous["task_id"] != taskID || current["task_id"] != taskID ||
		previous["role"] != role || current["role"] != role || previous["actor_id"] != actorID || current["actor_id"] != actorID ||
		previous["assigned_by_actor_id"] != assignedByID || current["assigned_by_actor_id"] != assignedByID ||
		previous["assigned_at"] != assignedAt || current["assigned_at"] != assignedAt ||
		previous["unassigned_at"] != nil || previous["reason"] != nil || previous["is_active"] != true ||
		current["unassigned_at"] != unassignedAt || current["reason"] != reason || current["is_active"] != false {
		return false
	}
	previousInferred, previousInferredOK := previous["inferred"].(bool)
	currentInferred, currentInferredOK := current["inferred"].(bool)
	if !previousInferredOK || !currentInferredOK || previousInferred != currentInferred {
		return false
	}
	for _, key := range []string{"actor", "assigned_by_actor"} {
		previousActor, previousActorOK := previous[key].(map[string]any)
		currentActor, currentActorOK := current[key].(map[string]any)
		expectedID := actorID
		if key == "assigned_by_actor" {
			expectedID = assignedByID
		}
		if !previousActorOK || !currentActorOK ||
			!validTaskArtifactAssignmentActorImportState(previousActor, expectedID, actors) ||
			!taskArtifactImportObjectEquals(previousActor, currentActor) {
			return false
		}
	}
	for _, key := range []string{"id", "task_id", "role", "actor_id", "actor", "assigned_by_actor_id", "assigned_by_actor", "assigned_at", "inferred"} {
		if !taskArtifactImportValueEquals(previous[key], current[key]) {
			return false
		}
	}
	migrationReferences := 0
	for _, event := range automationImportAggregateActionEvents(events, "task", taskID, "migration_assignment_backfill") {
		if event.assignmentID != nil && *event.assignmentID == id {
			migrationReferences++
		}
	}
	if currentInferred != (migrationReferences == 1 && validTaskArtifactMigrationAssignmentOrigin(events, row)) ||
		(!currentInferred && migrationReferences != 0) {
		return false
	}
	return true
}

func validTaskArtifactAssignmentReassignedImportEvent(
	events automationImportEventIndex,
	assignments map[string]map[string]any,
	actors map[string]map[string]any,
	row map[string]any,
	id, taskID, actorID, assignedByID, role, assignedAt, unassignedAt, reason string,
) bool {
	type candidate struct {
		event    automationImportWorkflowEvent
		previous map[string]any
		ended    map[string]any
		created  map[string]any
	}
	matched := make([]candidate, 0, 1)
	for _, event := range automationImportAggregateActionEvents(events, "task", taskID, "assignment_reassigned") {
		if event.previousJSON == nil || event.currentJSON == nil {
			continue
		}
		previous, previousOK := automationImportJSONObject(*event.previousJSON)
		current, currentOK := automationImportJSONObject(*event.currentJSON)
		ended, endedOK := current["ended_assignment"].(map[string]any)
		created, createdOK := current["assignment"].(map[string]any)
		if previousOK && currentOK &&
			automationImportObjectHasExactKeys(current, []string{"ended_assignment", "assignment"}) &&
			endedOK && createdOK && ended["id"] == id {
			matched = append(matched, candidate{event: event, previous: previous, ended: ended, created: created})
		}
	}
	if len(matched) != 1 {
		return false
	}
	proof := matched[0]
	if !validTaskArtifactImportEventEnvelope(proof.event, models.BuiltinOwnerActorID, true) ||
		proof.event.createdAt != unassignedAt || proof.event.assignmentID == nil ||
		proof.event.submissionID != nil || proof.event.artifactID != nil || proof.event.agentRunID != nil ||
		proof.event.commandSeq != nil || !validTaskArtifactEndedAssignmentImportStates(
		events, actors, proof.previous, proof.ended, row,
		id, taskID, actorID, assignedByID, role, assignedAt, unassignedAt, reason,
	) {
		return false
	}
	newID := *proof.event.assignmentID
	createdActorID, createdActorOK := proof.created["actor_id"].(string)
	newRow, exists := assignments[newID]
	newUnassignedAt, newUnassignedOK := automationImportOptionalString(newRow["unassigned_at"])
	newReason, newReasonOK := newRow["reason"].(string)
	if !validCanonicalAutomationUUID(newID) || !createdActorOK || !validCanonicalAutomationUUID(createdActorID) ||
		!exists || !automationImportObjectHasExactKeys(proof.created, taskAssignmentImportStateKeys) ||
		proof.created["id"] != newID || proof.created["task_id"] != taskID || proof.created["role"] != role ||
		createdActorID == actorID || proof.created["assigned_by_actor_id"] != models.BuiltinOwnerActorID ||
		proof.created["assigned_at"] != unassignedAt || proof.created["unassigned_at"] != nil ||
		proof.created["reason"] != nil || proof.created["is_active"] != true || proof.created["inferred"] != false ||
		newRow["task_id"] != taskID || newRow["role"] != role || newRow["actor_id"] != createdActorID ||
		newRow["assigned_by_actor_id"] != models.BuiltinOwnerActorID || newRow["assigned_at"] != unassignedAt ||
		!newUnassignedOK || !newReasonOK || !validTaskArtifactAssignmentActorRole(actors, createdActorID, role) {
		return false
	}
	if newUnassignedAt == nil {
		finalActorStatus, statusOK := actors[createdActorID]["status"].(string)
		if !statusOK || finalActorStatus != "active" || newReason != "" {
			return false
		}
	} else {
		endedAt, endedOK := automationImportTime(*newUnassignedAt)
		assignedTime, assignedOK := automationImportTime(unassignedAt)
		if !endedOK || !assignedOK || endedAt.Before(assignedTime) || strings.TrimSpace(newReason) == "" ||
			strings.TrimSpace(newReason) != newReason || utf8.RuneCountInString(newReason) > 1_000 {
			return false
		}
	}
	for key, expectedID := range map[string]string{
		"actor": createdActorID, "assigned_by_actor": models.BuiltinOwnerActorID,
	} {
		actor, ok := proof.created[key].(map[string]any)
		if !ok || !validTaskArtifactAssignmentActorImportState(actor, expectedID, actors) {
			return false
		}
	}
	return true
}

func validTaskArtifactAssignmentActorImportState(
	state map[string]any,
	expectedID string,
	actors map[string]map[string]any,
) bool {
	if !automationImportObjectHasExactKeys(state, taskAssignmentActorImportStateKeys) || state["id"] != expectedID ||
		state["status"] != "active" {
		return false
	}
	actorType, typeOK := state["type"].(string)
	displayName, nameOK := state["display_name"].(string)
	isBuiltin, builtinOK := state["is_builtin"].(bool)
	version, versionOK := automationImportJSONInt64(state["version"])
	finalActor, actorExists := actors[expectedID]
	finalType, finalTypeOK := finalActor["type"].(string)
	finalDisplayName, finalDisplayNameOK := finalActor["display_name"].(string)
	finalStatus, finalStatusOK := finalActor["status"].(string)
	finalBuiltin, finalBuiltinOK := automationImportInt64(finalActor["is_builtin"])
	finalVersion, finalVersionOK := automationImportInt64(finalActor["version"])
	if !typeOK || !nameOK || strings.TrimSpace(displayName) != displayName || utf8.RuneCountInString(displayName) < 1 ||
		utf8.RuneCountInString(displayName) > 100 || !builtinOK || !versionOK || version < 1 ||
		!actorExists || !finalTypeOK || !finalDisplayNameOK || !finalStatusOK || (finalStatus != "active" && finalStatus != "inactive") ||
		!finalBuiltinOK || (finalBuiltin != 0 && finalBuiltin != 1) ||
		!finalVersionOK || finalVersion < version || finalType != actorType || (finalBuiltin == 1) != isBuiltin {
		return false
	}
	if finalVersion == version && (finalDisplayName != displayName || finalStatus != "active") {
		return false
	}
	switch actorType {
	case "owner", "system":
		return isBuiltin
	case "person", "agent":
		return !isBuiltin
	default:
		return false
	}
}

func validTaskArtifactAssignmentActorRole(actors map[string]map[string]any, actorID, role string) bool {
	actorType, ok := actors[actorID]["type"].(string)
	if !ok {
		return false
	}
	if role == "reviewer" {
		return actorID == models.BuiltinOwnerActorID && actorType == "owner"
	}
	return role == "assignee" && (actorType == "owner" || actorType == "person")
}

func validTaskArtifactMigrationAssignmentImportHistory(events automationImportEventIndex, row map[string]any) bool {
	return row["reason"] == migrationAssignmentReason && validTaskArtifactMigrationAssignmentOrigin(events, row)
}

func validTaskArtifactMigrationAssignmentOrigin(events automationImportEventIndex, row map[string]any) bool {
	id, idOK := row["id"].(string)
	taskID, taskIDOK := row["task_id"].(string)
	expectedAssignmentID, assignmentIDOK := taskArtifactMigrationDerivedID(taskID, '5')
	expectedEventID, eventIDOK := taskArtifactMigrationDerivedID(taskID, '6')
	if !idOK || !taskIDOK || !assignmentIDOK || !eventIDOK || id != expectedAssignmentID ||
		row["role"] != "assignee" || row["actor_id"] != models.BuiltinOwnerActorID ||
		row["assigned_by_actor_id"] != models.BuiltinOwnerActorID {
		return false
	}
	matched := make([]automationImportWorkflowEvent, 0, 1)
	migrationEvents := automationImportAggregateActionEvents(events, "task", taskID, "migration_assignment_backfill")
	for _, event := range migrationEvents {
		if event.assignmentID != nil && *event.assignmentID == id {
			matched = append(matched, event)
		}
	}
	if len(migrationEvents) != 1 || len(matched) != 1 {
		return false
	}
	event := matched[0]
	if event.id != expectedEventID || !validTaskArtifactMigrationEventTime(event.createdAt) ||
		event.actorID == nil || *event.actorID != models.BuiltinOwnerActorID || event.requestID != nil || event.commandSeq != nil ||
		event.previousJSON != nil || event.currentJSON == nil || event.submissionID != nil || event.artifactID != nil || event.agentRunID != nil {
		return false
	}
	current, ok := automationImportJSONObject(*event.currentJSON)
	return ok && taskArtifactImportObjectEquals(current, map[string]any{
		"source": "schema_v7_migration", "inferred": true, "role": "assignee",
	})
}

func validTaskArtifactMigrationEventTime(value string) bool {
	_, ok := taskArtifactImportTime(value)
	return ok
}

func taskArtifactMigrationDerivedID(taskID string, versionNibble byte) (string, bool) {
	if !validCanonicalAutomationUUID(taskID) || (versionNibble != '5' && versionNibble != '6') {
		return "", false
	}
	derived := taskID[:14] + string(versionNibble) + taskID[15:]
	return derived, validCanonicalAutomationUUID(derived)
}

func validTaskArtifactInboxEventReverseClosure(
	inboxes map[string]map[string]any,
	events automationImportEventIndex,
	replays taskArtifactImportReplays,
) bool {
	projectedCounts := make(map[string]int)
	deletedCounts := make(map[string]int)
	candidates := make(map[string]struct{})
	for _, event := range events.byID {
		if event.aggregateType != "inbox_item" || (event.action != "source_projected" && event.action != "source_deleted") {
			continue
		}
		finalRow, finalExists := inboxes[event.aggregateID]
		switch event.action {
		case "source_projected":
			if event.currentJSON == nil {
				return false
			}
			current, currentOK := automationImportJSONObject(*event.currentJSON)
			identity, candidate, stateOK := taskArtifactInboxImportIdentityFromState(current)
			if !candidate {
				continue
			}
			if !currentOK || !stateOK || !finalExists ||
				!validTaskArtifactProjectedReverseImportEvent(event, current, identity, finalRow) {
				return false
			}
			candidates[event.aggregateID] = struct{}{}
			projectedCounts[event.aggregateID]++
		case "source_deleted":
			if event.previousJSON == nil || event.currentJSON == nil {
				return false
			}
			previous, previousOK := automationImportJSONObject(*event.previousJSON)
			current, currentOK := automationImportJSONObject(*event.currentJSON)
			previousIdentity, previousCandidate, previousStateOK := taskArtifactInboxImportIdentityFromState(previous)
			currentIdentity, currentCandidate, currentStateOK := taskArtifactInboxImportIdentityFromState(current)
			if !previousCandidate && !currentCandidate {
				continue
			}
			if !previousOK || !currentOK || !previousStateOK || !currentStateOK || !finalExists ||
				previousIdentity.artifactID != currentIdentity.artifactID ||
				previousIdentity.sourceKey != currentIdentity.sourceKey ||
				!taskArtifactImportObjectEquals(previousIdentity.payload, currentIdentity.payload) {
				return false
			}
			if _, planned := replays.byInboxID[event.aggregateID]; !planned {
				return false
			}
			candidates[event.aggregateID] = struct{}{}
			deletedCounts[event.aggregateID]++
		}
	}
	for inboxID, row := range inboxes {
		_, candidate, valid := taskArtifactInboxImportIdentityFromRow(row)
		if !candidate {
			continue
		}
		if !valid {
			return false
		}
		candidates[inboxID] = struct{}{}
	}
	for inboxID := range candidates {
		row, exists := inboxes[inboxID]
		deletedAt, deletedAtOK := automationImportOptionalString(row["source_deleted_at"])
		if !exists || !deletedAtOK || projectedCounts[inboxID] != 1 {
			return false
		}
		_, replayed := replays.byInboxID[inboxID]
		if deletedAt == nil {
			if replayed || deletedCounts[inboxID] != 0 {
				return false
			}
		} else if !replayed || deletedCounts[inboxID] != 1 {
			return false
		}
	}
	return true
}

func taskArtifactInboxImportIdentityFromRow(
	row map[string]any,
) (taskArtifactInboxImportIdentity, bool, bool) {
	sourceType, typeOK := row["source_entity_type"].(string)
	artifactID, idOK := row["source_entity_id"].(string)
	sourceKey, keyOK := row["source_event_key"].(string)
	payloadRaw, payloadRawOK := row["payload_json"].(string)
	payload, payloadOK := automationImportJSONObject(payloadRaw)
	keyArtifactID, reservedKey := taskArtifactImportArtifactIDFromEventKey(sourceKey)
	payloadArtifactID, payloadArtifactOK := payload["artifact_id"].(string)
	payloadTaskID, payloadTaskOK := payload["task_id"].(string)
	payloadSubmissionID, payloadSubmissionOK := payload["submission_id"].(string)
	reservedPayload := payloadOK && payloadArtifactOK && validCanonicalAutomationUUID(payloadArtifactID) &&
		payloadTaskOK && validCanonicalAutomationUUID(payloadTaskID) && payloadSubmissionOK &&
		validCanonicalAutomationUUID(payloadSubmissionID) && idOK && payloadArtifactID == artifactID
	candidate := sourceType == taskArtifactInboxSourceType || reservedKey || reservedPayload
	if !candidate {
		return taskArtifactInboxImportIdentity{}, false, true
	}
	if !typeOK || sourceType != taskArtifactInboxSourceType || !idOK || !validCanonicalAutomationUUID(artifactID) ||
		!keyOK || !reservedKey || keyArtifactID != artifactID || !payloadRawOK || !payloadOK ||
		payloadArtifactID != artifactID {
		return taskArtifactInboxImportIdentity{}, true, false
	}
	return taskArtifactInboxImportIdentity{artifactID: artifactID, sourceKey: sourceKey, payload: payload}, true, true
}

func taskArtifactInboxImportIdentityFromState(
	state map[string]any,
) (taskArtifactInboxImportIdentity, bool, bool) {
	sourceType, typeOK := state["source_entity_type"].(string)
	artifactID, idOK := state["source_entity_id"].(string)
	sourceKey, keyOK := state["source_event_key"].(string)
	payload, payloadOK := state["payload_json"].(map[string]any)
	_, payloadHasArtifact := payload["artifact_id"]
	_, payloadHasTask := payload["task_id"]
	_, payloadHasSubmission := payload["submission_id"]
	keyArtifactID, reservedKey := taskArtifactImportArtifactIDFromEventKey(sourceKey)
	candidate := sourceType == taskArtifactInboxSourceType || reservedKey ||
		(payloadOK && payloadHasArtifact && payloadHasTask && payloadHasSubmission)
	if !candidate {
		return taskArtifactInboxImportIdentity{}, false, true
	}
	if !automationImportObjectHasExactKeys(state, projectCompletionInboxEventStateKeys) || !typeOK ||
		sourceType != taskArtifactInboxSourceType || !idOK || !validCanonicalAutomationUUID(artifactID) ||
		!keyOK || !reservedKey || keyArtifactID != artifactID || !payloadOK || payload["artifact_id"] != artifactID {
		return taskArtifactInboxImportIdentity{}, true, false
	}
	return taskArtifactInboxImportIdentity{artifactID: artifactID, sourceKey: sourceKey, payload: payload}, true, true
}

func taskArtifactImportArtifactIDFromEventKey(value string) (string, bool) {
	const prefix = "task-artifact:"
	const suffix = ":followup"
	if !strings.HasPrefix(value, prefix) || !strings.HasSuffix(value, suffix) {
		return "", false
	}
	artifactID := strings.TrimSuffix(strings.TrimPrefix(value, prefix), suffix)
	return artifactID, validCanonicalAutomationUUID(artifactID) && value == taskArtifactFollowupEventKey(artifactID)
}

func validTaskArtifactProjectedReverseImportEvent(
	event automationImportWorkflowEvent,
	state map[string]any,
	identity taskArtifactInboxImportIdentity,
	finalRow map[string]any,
) bool {
	createdAt, createdAtOK := finalRow["created_at"].(string)
	finalPayloadRaw, payloadOK := finalRow["payload_json"].(string)
	finalPayload, finalPayloadOK := automationImportJSONObject(finalPayloadRaw)
	if !createdAtOK || !payloadOK || !finalPayloadOK ||
		!validTaskArtifactImportEventEnvelope(event, models.BuiltinSystemActorID, true) || event.createdAt != createdAt ||
		event.assignmentID != nil || event.submissionID != nil || event.artifactID != nil || event.agentRunID != nil ||
		event.commandSeq == nil || *event.commandSeq != 1 || event.previousJSON != nil || event.currentJSON == nil ||
		state["kind"] != "event" || state["source_deleted_at"] != nil || state["resolution_policy"] != "manual" ||
		state["status"] != "open" || state["version"] == nil || finalRow["kind"] != state["kind"] ||
		finalRow["source_entity_type"] != taskArtifactInboxSourceType || finalRow["source_entity_id"] != identity.artifactID ||
		finalRow["source_event_key"] != identity.sourceKey || !taskArtifactImportObjectEquals(finalPayload, identity.payload) {
		return false
	}
	version, versionOK := automationImportJSONInt64(state["version"])
	return versionOK && version == 1
}

func taskArtifactImportReplayPlan(packageData businessExportPackage) (taskArtifactImportReplays, bool) {
	result := taskArtifactImportReplays{
		ordered:                 make([]taskArtifactImportReplay, 0),
		byArtifactID:            make(map[string]taskArtifactImportReplay),
		byInboxID:               make(map[string]taskArtifactImportReplay),
		taskIDs:                 make(map[string]struct{}),
		changesRequestedTaskIDs: make(map[string]struct{}),
	}
	tables := make(map[string]businessExportTable, len(packageData.Tables))
	for _, table := range packageData.Tables {
		tables[table.Name] = table
	}
	events, ok := automationImportWorkflowEvents(tables["workflow_events"])
	if !ok {
		return taskArtifactImportReplays{}, false
	}
	artifacts, ok := automationImportRowsByID(tables["task_artifacts"])
	if !ok {
		return taskArtifactImportReplays{}, false
	}
	tasks, ok := automationImportRowsByID(tables["tasks"])
	if !ok {
		return taskArtifactImportReplays{}, false
	}
	submissions, ok := automationImportRowsByID(tables["task_submissions"])
	if !ok {
		return taskArtifactImportReplays{}, false
	}
	inboxes, ok := automationImportRowsByID(tables["inbox_items"])
	if !ok {
		return taskArtifactImportReplays{}, false
	}
	legacyGaps, ok := taskArtifactLegacyInboxGapImportProofs(artifacts, inboxes, events)
	if !ok {
		return taskArtifactImportReplays{}, false
	}
	artifactsBySubmission := make(map[string][]map[string]any)
	assignments := automationImportRows(tables["task_assignments"])
	for _, artifact := range artifacts {
		submissionID, submissionOK := artifact["submission_id"].(string)
		if !submissionOK {
			return taskArtifactImportReplays{}, false
		}
		artifactsBySubmission[submissionID] = append(artifactsBySubmission[submissionID], artifact)
	}
	for submissionID := range artifactsBySubmission {
		sort.Slice(artifactsBySubmission[submissionID], func(left, right int) bool {
			leftPosition, _ := automationImportInt64(artifactsBySubmission[submissionID][left]["position"])
			rightPosition, _ := automationImportInt64(artifactsBySubmission[submissionID][right]["position"])
			if leftPosition != rightPosition {
				return leftPosition < rightPosition
			}
			leftID, _ := artifactsBySubmission[submissionID][left]["id"].(string)
			rightID, _ := artifactsBySubmission[submissionID][right]["id"].(string)
			return leftID < rightID
		})
	}
	validation, ok := newTaskArtifactImportValidationIndex(events, assignments, artifactsBySubmission)
	if !ok {
		return taskArtifactImportReplays{}, false
	}

	for _, inbox := range automationImportRows(tables["inbox_items"]) {
		if inbox["source_entity_type"] != taskArtifactInboxSourceType || inbox["source_deleted_at"] == nil {
			continue
		}
		replay, valid := validTaskArtifactImportReplay(inbox, artifacts, tasks, submissions, validation)
		if !valid {
			return taskArtifactImportReplays{}, false
		}
		if _, duplicate := result.byArtifactID[replay.artifactID]; duplicate {
			return taskArtifactImportReplays{}, false
		}
		if _, duplicate := result.byInboxID[replay.inboxID]; duplicate {
			return taskArtifactImportReplays{}, false
		}
		result.ordered = append(result.ordered, replay)
		result.byArtifactID[replay.artifactID] = replay
		result.byInboxID[replay.inboxID] = replay
		result.taskIDs[replay.taskID] = struct{}{}
		if replay.submissionStatus == "changes_requested" {
			result.changesRequestedTaskIDs[replay.taskID] = struct{}{}
		}
	}
	if !addLegacyTaskArtifactImportProtection(
		&result, artifacts, tasks, submissions, legacyGaps, validation,
	) {
		return taskArtifactImportReplays{}, false
	}
	sort.Slice(result.ordered, func(left, right int) bool {
		return result.ordered[left].inboxID < result.ordered[right].inboxID
	})
	if !validTaskArtifactInboxEventReverseClosure(inboxes, events, result) {
		return taskArtifactImportReplays{}, false
	}
	return result, true
}

func newTaskArtifactImportValidationIndex(
	events automationImportEventIndex,
	assignments []map[string]any,
	artifactsBySubmission map[string][]map[string]any,
) (*taskArtifactImportValidationIndex, bool) {
	result := &taskArtifactImportValidationIndex{
		events:                   events,
		assignments:              assignments,
		artifactsBySubmission:    artifactsBySubmission,
		submittedBySubmissionID:  make(map[string]taskArtifactSubmittedImportValidation),
		deletedEventByArtifactID: make(map[string]automationImportWorkflowEvent),
	}
	for _, event := range events.byID {
		if event.aggregateType != "task" || event.action != "task_artifact_deleted" || event.artifactID == nil {
			continue
		}
		artifactID := *event.artifactID
		if _, duplicate := result.deletedEventByArtifactID[artifactID]; duplicate {
			return nil, false
		}
		result.deletedEventByArtifactID[artifactID] = event
	}
	return result, true
}

func (index *taskArtifactImportValidationIndex) validSubmittedEvent(
	taskID, submissionID, createdAt string,
	task, submission, targetArtifact map[string]any,
) (taskArtifactSubmittedImportProof, bool) {
	validation, cached := index.submittedBySubmissionID[submissionID]
	if !cached {
		proof, valid := validTaskArtifactSubmittedImportEvent(
			index.events, taskID, submissionID, createdAt, task, submission,
			index.artifactsBySubmission[submissionID],
		)
		validation = taskArtifactSubmittedImportValidation{
			taskID: taskID, createdAt: createdAt, proof: proof, valid: valid,
		}
		index.submittedBySubmissionID[submissionID] = validation
	}
	if !validation.valid || validation.taskID != taskID || validation.createdAt != createdAt {
		return taskArtifactSubmittedImportProof{}, false
	}
	producedBy, producedByOK := targetArtifact["produced_by_actor_id"].(string)
	if !producedByOK || !validTaskArtifactSubmissionAssignmentActors(
		index.assignments, taskID, producedBy, createdAt,
	) {
		return taskArtifactSubmittedImportProof{}, false
	}
	return validation.proof, true
}

// Schema versions before the Task Artifact Inbox projection did not backfill
// Inbox rows for existing follow-up Artifacts. A later export can therefore
// contain a valid deleted follow-up Artifact with complete Task history but no
// Inbox aggregate. Protect those Tasks so ended assignments are restored via
// the history path, while requiring the same submission, disposition, and
// deletion proofs as a projected Artifact.
func addLegacyTaskArtifactImportProtection(
	result *taskArtifactImportReplays,
	artifacts, tasks, submissions map[string]map[string]any,
	legacyGaps map[string]struct{},
	validation *taskArtifactImportValidationIndex,
) bool {
	events := validation.events
	for artifactID, artifact := range artifacts {
		if _, projected := result.byArtifactID[artifactID]; projected {
			continue
		}
		requiresFollowup, followupOK := automationImportInt64(artifact["requires_followup"])
		deletedAt, deletedAtOK := automationImportOptionalString(artifact["deleted_at"])
		if !followupOK || !deletedAtOK {
			return false
		}
		if requiresFollowup != 1 || deletedAt == nil {
			continue
		}
		if _, provenLegacyGap := legacyGaps[artifactID]; !provenLegacyGap {
			return false
		}
		taskID, taskIDOK := artifact["task_id"].(string)
		submissionID, submissionIDOK := artifact["submission_id"].(string)
		deletedBy, deletedByOK := artifact["deleted_by_actor_id"].(string)
		deleteReason, reasonOK := artifact["delete_reason"].(string)
		createdAt, createdAtOK := artifact["created_at"].(string)
		createdTime, createdTimeOK := automationImportTime(createdAt)
		deletedTime, deletedTimeOK := automationImportTime(*deletedAt)
		if !validCanonicalAutomationUUID(artifactID) || !taskIDOK || !validCanonicalAutomationUUID(taskID) ||
			!submissionIDOK || !validCanonicalAutomationUUID(submissionID) || !deletedByOK ||
			deletedBy != models.BuiltinOwnerActorID || !reasonOK || strings.TrimSpace(deleteReason) == "" ||
			strings.TrimSpace(deleteReason) != deleteReason || utf8.RuneCountInString(deleteReason) > 1_000 ||
			!createdAtOK || !createdTimeOK || !deletedTimeOK || deletedTime.Before(createdTime) {
			return false
		}
		task, taskExists := tasks[taskID]
		submission, submissionExists := submissions[submissionID]
		if !taskExists || !submissionExists || submission["task_id"] != taskID {
			return false
		}
		disposition, dispositionOK := validTaskArtifactSubmissionDispositionImportEvent(
			events, taskID, submissionID, task, submission,
		)
		submitted, submittedOK := validation.validSubmittedEvent(
			taskID, submissionID, createdAt, task, submission, artifact,
		)
		deleteTaskVersion, deletionOK := validTaskArtifactDeletedImportEvent(
			validation, taskID, submissionID, artifactID, *deletedAt, deleteReason,
			artifact, task, nil, submitted,
		)
		if !dispositionOK || !submittedOK || !deletionOK || createdTime.After(disposition.eventTime) ||
			disposition.eventTime.After(deletedTime) || submitted.taskVersion >= disposition.taskVersion ||
			disposition.taskVersion > deleteTaskVersion {
			return false
		}
		result.taskIDs[taskID] = struct{}{}
		if disposition.status == "changes_requested" {
			result.changesRequestedTaskIDs[taskID] = struct{}{}
		}
	}
	return true
}

func taskArtifactLegacyInboxGapImportProofs(
	artifacts, inboxes map[string]map[string]any,
	events automationImportEventIndex,
) (map[string]struct{}, bool) {
	result := make(map[string]struct{})
	inboxEvidence := taskArtifactImportInboxEvidenceIDs(inboxes, events)
	for _, event := range events.byID {
		if event.action != taskArtifactInboxGapMigrationAction {
			continue
		}
		if event.aggregateType != "task" || event.artifactID == nil || event.submissionID == nil ||
			event.assignmentID != nil || event.agentRunID != nil || event.requestID != nil || event.commandSeq != nil ||
			event.previousJSON != nil || event.currentJSON == nil ||
			!validTaskArtifactImportEventEnvelope(event, models.BuiltinOwnerActorID, false) {
			return nil, false
		}
		artifactID := *event.artifactID
		artifact, exists := artifacts[artifactID]
		expectedEventID, eventIDOK := taskArtifactInboxGapMigrationEventID(artifactID)
		taskID, taskIDOK := artifact["task_id"].(string)
		submissionID, submissionIDOK := artifact["submission_id"].(string)
		createdAt, createdAtOK := artifact["created_at"].(string)
		requiresFollowup, followupOK := automationImportInt64(artifact["requires_followup"])
		markerTime, markerTimeOK := taskArtifactImportTime(event.createdAt)
		artifactTime, artifactTimeOK := taskArtifactImportTime(createdAt)
		current, currentOK := automationImportJSONObject(*event.currentJSON)
		currentRequiresFollowup, currentFollowupOK := automationImportJSONInt64(current["requires_followup"])
		_, hasInboxEvidence := inboxEvidence[artifactID]
		if !exists || !eventIDOK || event.id != expectedEventID || !taskIDOK || event.aggregateID != taskID ||
			!submissionIDOK || *event.submissionID != submissionID || !createdAtOK || !followupOK || requiresFollowup != 1 ||
			!markerTimeOK || !artifactTimeOK || markerTime.Before(artifactTime) || !currentOK || !currentFollowupOK ||
			currentRequiresFollowup != 1 || !automationImportObjectHasExactKeys(current, []string{
			"source", "artifact_id", "task_id", "submission_id", "artifact_created_at", "requires_followup",
		}) || current["source"] != "schema_v51_migration" || current["artifact_id"] != artifactID ||
			current["task_id"] != taskID || current["submission_id"] != submissionID || current["artifact_created_at"] != createdAt ||
			hasInboxEvidence {
			return nil, false
		}
		if _, duplicate := result[artifactID]; duplicate {
			return nil, false
		}
		result[artifactID] = struct{}{}
	}
	return result, true
}

func taskArtifactInboxGapMigrationEventID(artifactID string) (string, bool) {
	if !validCanonicalAutomationUUID(artifactID) {
		return "", false
	}
	derived := artifactID[:14] + "6" + artifactID[15:]
	return derived, validCanonicalAutomationUUID(derived)
}

func taskArtifactImportInboxEvidenceIDs(
	inboxes map[string]map[string]any,
	events automationImportEventIndex,
) map[string]struct{} {
	result := make(map[string]struct{})
	for _, inbox := range inboxes {
		if artifactID, ok := inbox["source_entity_id"].(string); ok {
			result[artifactID] = struct{}{}
		}
	}
	for _, event := range events.byID {
		if event.aggregateType != "inbox_item" {
			continue
		}
		for _, raw := range []*string{event.previousJSON, event.currentJSON} {
			if raw == nil {
				continue
			}
			state, ok := automationImportJSONObject(*raw)
			if !ok {
				continue
			}
			if artifactID, ok := state["source_entity_id"].(string); ok {
				result[artifactID] = struct{}{}
			}
			if sourceKey, ok := state["source_event_key"].(string); ok {
				if artifactID, reserved := taskArtifactImportArtifactIDFromEventKey(sourceKey); reserved {
					result[artifactID] = struct{}{}
				}
			}
			if payload, ok := state["payload_json"].(map[string]any); ok {
				if artifactID, ok := payload["artifact_id"].(string); ok {
					result[artifactID] = struct{}{}
				}
			}
		}
	}
	return result
}

func validTaskArtifactImportReplay(
	inbox map[string]any,
	artifacts map[string]map[string]any,
	tasks map[string]map[string]any,
	submissions map[string]map[string]any,
	validation *taskArtifactImportValidationIndex,
) (taskArtifactImportReplay, bool) {
	events := validation.events
	inboxID, inboxIDOK := inbox["id"].(string)
	artifactID, artifactIDOK := inbox["source_entity_id"].(string)
	sourceKey, sourceKeyOK := inbox["source_event_key"].(string)
	deletedAt, deletedAtOK := automationImportOptionalString(inbox["source_deleted_at"])
	payloadJSON, payloadJSONOK := inbox["payload_json"].(string)
	createdAt, createdAtOK := inbox["created_at"].(string)
	updatedAt, updatedAtOK := inbox["updated_at"].(string)
	resolutionPolicy, resolutionPolicyOK := inbox["resolution_policy"].(string)
	status, statusOK := inbox["status"].(string)
	if !inboxIDOK || !validCanonicalAutomationUUID(inboxID) || !artifactIDOK || !validCanonicalAutomationUUID(artifactID) ||
		!sourceKeyOK || sourceKey != taskArtifactFollowupEventKey(artifactID) || !deletedAtOK || deletedAt == nil ||
		!payloadJSONOK || !createdAtOK || !updatedAtOK || inbox["kind"] != "event" ||
		!resolutionPolicyOK || (resolutionPolicy != "manual" && resolutionPolicy != "all_required_tasks_done") ||
		!statusOK || (status != "open" && status != "tracking" && status != "resolved" && status != "dismissed") {
		return taskArtifactImportReplay{}, false
	}
	deletedTime, deletedTimeOK := automationImportTime(*deletedAt)
	createdTime, createdTimeOK := automationImportTime(createdAt)
	updatedTime, updatedTimeOK := automationImportTime(updatedAt)
	if !deletedTimeOK || !createdTimeOK || !updatedTimeOK || deletedTime.Before(createdTime) || updatedTime.Before(deletedTime) {
		return taskArtifactImportReplay{}, false
	}
	artifact, artifactExists := artifacts[artifactID]
	if !artifactExists {
		return taskArtifactImportReplay{}, false
	}
	taskID, taskIDOK := artifact["task_id"].(string)
	submissionID, submissionIDOK := artifact["submission_id"].(string)
	requiresFollowup, followupOK := automationImportInt64(artifact["requires_followup"])
	artifactDeletedAt, artifactDeletedOK := automationImportOptionalString(artifact["deleted_at"])
	deletedBy, deletedByOK := automationImportOptionalString(artifact["deleted_by_actor_id"])
	deleteReason, reasonOK := automationImportOptionalString(artifact["delete_reason"])
	artifactCreatedAt, artifactCreatedOK := artifact["created_at"].(string)
	if !taskIDOK || !validCanonicalAutomationUUID(taskID) || !submissionIDOK || !validCanonicalAutomationUUID(submissionID) ||
		!followupOK || requiresFollowup != 1 || !artifactDeletedOK || artifactDeletedAt == nil || *artifactDeletedAt != *deletedAt ||
		!deletedByOK || deletedBy == nil || *deletedBy != models.BuiltinOwnerActorID || !reasonOK || deleteReason == nil ||
		strings.TrimSpace(*deleteReason) == "" || strings.TrimSpace(*deleteReason) != *deleteReason ||
		!artifactCreatedOK || artifactCreatedAt != createdAt {
		return taskArtifactImportReplay{}, false
	}
	task, taskExists := tasks[taskID]
	submission, submissionExists := submissions[submissionID]
	if !taskExists || !submissionExists || submission["task_id"] != taskID {
		return taskArtifactImportReplay{}, false
	}
	disposition, dispositionOK := validTaskArtifactSubmissionDispositionImportEvent(
		events, taskID, submissionID, task, submission,
	)
	if !dispositionOK {
		return taskArtifactImportReplay{}, false
	}
	payload, payloadOK := validTaskArtifactImportPayload(payloadJSON, artifact, submission)
	if !payloadOK {
		return taskArtifactImportReplay{}, false
	}
	submittedEvent, submittedOK := validation.validSubmittedEvent(
		taskID, submissionID, artifactCreatedAt, task, submission, artifact,
	)
	if !submittedOK || !validTaskArtifactProjectedImportEvent(
		events, inboxID, artifactID, sourceKey, artifactCreatedAt, submission, artifact, payload, submittedEvent.event,
	) {
		return taskArtifactImportReplay{}, false
	}
	sourceDeleted, deletedOK := validTaskArtifactSourceDeletedImportEvent(
		events, inboxID, artifactID, sourceKey, *deletedAt, payload, inbox,
	)
	deleteTaskVersion, artifactDeletedOK := validTaskArtifactDeletedImportEvent(
		validation, taskID, submissionID, artifactID, *deletedAt, *deleteReason,
		artifact, task, &sourceDeleted.event, submittedEvent,
	)
	submittedTime, submittedTimeOK := automationImportTime(artifactCreatedAt)
	if !deletedOK || !artifactDeletedOK || !submittedTimeOK || submittedTime.After(disposition.eventTime) ||
		disposition.eventTime.After(deletedTime) || submittedEvent.taskVersion >= disposition.taskVersion ||
		disposition.taskVersion > deleteTaskVersion {
		return taskArtifactImportReplay{}, false
	}
	return taskArtifactImportReplay{
		artifactID: artifactID, inboxID: inboxID, taskID: taskID, submissionID: submissionID,
		submissionStatus: disposition.status, deletedAt: *deletedAt,
		deletedByActorID: *deletedBy, deleteReason: *deleteReason,
		previousInboxVersion: sourceDeleted.previousVersion, deletedInboxVersion: sourceDeleted.currentVersion,
		deletedInboxPrevious: sourceDeleted.previous, deletedInboxCurrent: sourceDeleted.current,
	}, true
}

func validTaskArtifactSubmissionDispositionImportEvent(
	events automationImportEventIndex,
	taskID, submissionID string,
	task, submission map[string]any,
) (taskArtifactSubmissionDispositionImportProof, bool) {
	status, statusOK := submission["status"].(string)
	origin, originOK := submission["origin"].(string)
	isInferred, inferredOK := automationImportInt64(submission["is_inferred"])
	if !statusOK || !originOK || origin != taskSubmissionOriginManual || !inferredOK || isInferred != 0 {
		return taskArtifactSubmissionDispositionImportProof{}, false
	}
	for _, action := range []string{"task_review_accepted", "task_changes_requested", "task_submission_withdrawn"} {
		matched := taskArtifactSubmissionDispositionEvents(events, taskID, submissionID, action)
		if action == taskArtifactSubmissionDispositionAction(status) {
			if len(matched) != 1 {
				return taskArtifactSubmissionDispositionImportProof{}, false
			}
			continue
		}
		if len(matched) != 0 {
			return taskArtifactSubmissionDispositionImportProof{}, false
		}
	}
	eventAction := taskArtifactSubmissionDispositionAction(status)
	if eventAction == "" {
		return taskArtifactSubmissionDispositionImportProof{}, false
	}
	event := taskArtifactSubmissionDispositionEvents(events, taskID, submissionID, eventAction)[0]
	if status == "withdrawn" {
		return validTaskArtifactWithdrawnSubmissionImportEvent(events, event, task, submission, submissionID)
	}
	return validTaskArtifactReviewedSubmissionImportEvent(event, task, submission, submissionID, status)
}

func taskArtifactSubmissionDispositionAction(status string) string {
	switch status {
	case "accepted":
		return "task_review_accepted"
	case "changes_requested":
		return "task_changes_requested"
	case "withdrawn":
		return "task_submission_withdrawn"
	default:
		return ""
	}
}

func taskArtifactSubmissionDispositionEvents(
	events automationImportEventIndex,
	taskID, submissionID, action string,
) []automationImportWorkflowEvent {
	matched := make([]automationImportWorkflowEvent, 0, 1)
	for _, event := range automationImportAggregateActionEvents(events, "task", taskID, action) {
		if event.submissionID != nil && *event.submissionID == submissionID {
			matched = append(matched, event)
		}
	}
	return matched
}

func validTaskArtifactReviewedSubmissionImportEvent(
	event automationImportWorkflowEvent,
	task, submission map[string]any,
	submissionID, status string,
) (taskArtifactSubmissionDispositionImportProof, bool) {
	reviewedBy, reviewedByOK := submission["reviewed_by_actor_id"].(string)
	reviewedAt, reviewedAtOK := submission["reviewed_at"].(string)
	reviewReason, reasonOK := automationImportOptionalString(submission["review_reason"])
	withdrawnBy, withdrawnByOK := automationImportOptionalString(submission["withdrawn_by_actor_id"])
	withdrawnAt, withdrawnAtOK := automationImportOptionalString(submission["withdrawn_at"])
	submittedAt, submittedAtOK := submission["submitted_at"].(string)
	if !reviewedByOK || reviewedBy != models.BuiltinOwnerActorID || !reviewedAtOK || !reasonOK ||
		!withdrawnByOK || withdrawnBy != nil || !withdrawnAtOK || withdrawnAt != nil || !submittedAtOK ||
		!validTaskArtifactImportEventEnvelope(event, models.BuiltinOwnerActorID, true) || event.createdAt != reviewedAt ||
		event.assignmentID != nil || event.submissionID == nil || *event.submissionID != submissionID ||
		event.artifactID != nil || event.agentRunID != nil || event.commandSeq == nil || *event.commandSeq < 1 ||
		event.previousJSON == nil || event.currentJSON == nil {
		return taskArtifactSubmissionDispositionImportProof{}, false
	}
	if status == "changes_requested" && (*event.commandSeq != 1 || reviewReason == nil) {
		return taskArtifactSubmissionDispositionImportProof{}, false
	}
	previous, previousOK := automationImportJSONObject(*event.previousJSON)
	current, currentOK := automationImportJSONObject(*event.currentJSON)
	if !previousOK || !currentOK || !taskArtifactReviewStateHasExactKeys(previous, false) ||
		!taskArtifactReviewStateHasExactKeys(current, reviewReason != nil) ||
		previous["status"] != "waiting_review" || previous["review_policy"] != "manual" ||
		previous["blocked_reason"] != nil || previous["blocked_at"] != nil || previous["blocked_from_status"] != nil ||
		previous["completed_at"] != nil || previous["submitted_at"] != submittedAt || previous["reviewed_at"] != nil ||
		previous["current_submission_id"] != submissionID || previous["submission_id"] != submissionID ||
		previous["submission_status"] != "pending_review" || current["status"] != map[bool]string{true: "done", false: "in_progress"}[status == "accepted"] ||
		current["review_policy"] != "manual" || current["blocked_reason"] != nil || current["blocked_at"] != nil ||
		current["blocked_from_status"] != nil || current["submitted_at"] != submittedAt || current["reviewed_at"] != reviewedAt ||
		current["current_submission_id"] != submissionID || current["submission_id"] != submissionID ||
		current["submission_status"] != status {
		return taskArtifactSubmissionDispositionImportProof{}, false
	}
	if status == "accepted" {
		if current["completed_at"] != reviewedAt {
			return taskArtifactSubmissionDispositionImportProof{}, false
		}
	} else if current["completed_at"] != nil {
		return taskArtifactSubmissionDispositionImportProof{}, false
	}
	if reviewReason == nil {
		if _, exists := current["reason"]; exists {
			return taskArtifactSubmissionDispositionImportProof{}, false
		}
	} else if current["reason"] != *reviewReason || strings.TrimSpace(*reviewReason) != *reviewReason || *reviewReason == "" ||
		utf8.RuneCountInString(*reviewReason) > 1_000 {
		return taskArtifactSubmissionDispositionImportProof{}, false
	}
	previousVersion, previousVersionOK := automationImportJSONInt64(previous["version"])
	currentVersion, currentVersionOK := automationImportJSONInt64(current["version"])
	finalTaskVersion, finalVersionOK := automationImportInt64(task["version"])
	eventTime, eventTimeOK := automationImportTime(event.createdAt)
	if !previousVersionOK || !currentVersionOK || currentVersion != previousVersion+1 ||
		!finalVersionOK || finalTaskVersion < currentVersion || !eventTimeOK {
		return taskArtifactSubmissionDispositionImportProof{}, false
	}
	return taskArtifactSubmissionDispositionImportProof{
		status: status, eventTime: eventTime, taskVersion: currentVersion,
	}, true
}

func taskArtifactReviewStateHasExactKeys(state map[string]any, includeReason bool) bool {
	keys := make([]string, 0, len(taskImportLifecycleStateKeys)+3)
	keys = append(keys, taskImportLifecycleStateKeys...)
	keys = append(keys, "submission_id", "submission_status")
	if includeReason {
		keys = append(keys, "reason")
	}
	return automationImportObjectHasExactKeys(state, keys)
}

func validTaskArtifactWithdrawnSubmissionImportEvent(
	events automationImportEventIndex,
	event automationImportWorkflowEvent,
	task, submission map[string]any,
	submissionID string,
) (taskArtifactSubmissionDispositionImportProof, bool) {
	reviewedBy, reviewedByOK := automationImportOptionalString(submission["reviewed_by_actor_id"])
	reviewedAt, reviewedAtOK := automationImportOptionalString(submission["reviewed_at"])
	reviewReason, reviewReasonOK := automationImportOptionalString(submission["review_reason"])
	withdrawnBy, withdrawnByOK := submission["withdrawn_by_actor_id"].(string)
	withdrawnAt, withdrawnAtOK := submission["withdrawn_at"].(string)
	if !reviewedByOK || reviewedBy != nil || !reviewedAtOK || reviewedAt != nil ||
		!reviewReasonOK || reviewReason != nil || !withdrawnByOK || withdrawnBy != models.BuiltinOwnerActorID || !withdrawnAtOK ||
		!validTaskArtifactImportEventEnvelope(event, models.BuiltinOwnerActorID, true) || event.createdAt != withdrawnAt ||
		event.assignmentID != nil || event.submissionID == nil || *event.submissionID != submissionID ||
		event.artifactID != nil || event.agentRunID != nil || event.commandSeq == nil || *event.commandSeq != 1 ||
		event.previousJSON == nil || event.currentJSON == nil {
		return taskArtifactSubmissionDispositionImportProof{}, false
	}
	previous, previousOK := automationImportJSONObject(*event.previousJSON)
	current, currentOK := automationImportJSONObject(*event.currentJSON)
	reason, reasonOK := current["reason"].(string)
	if !previousOK || !currentOK || !automationImportObjectHasExactKeys(previous, []string{"submission_id", "submission_status"}) ||
		!automationImportObjectHasExactKeys(current, []string{"submission_id", "submission_status", "reason"}) ||
		previous["submission_id"] != submissionID || previous["submission_status"] != "pending_review" ||
		current["submission_id"] != submissionID || current["submission_status"] != "withdrawn" ||
		!reasonOK || strings.TrimSpace(reason) != reason || reason == "" || utf8.RuneCountInString(reason) > 1_000 {
		return taskArtifactSubmissionDispositionImportProof{}, false
	}
	matchedVersion := int64(0)
	matched := 0
	for _, cancelled := range automationImportAggregateActionEvents(events, "task", event.aggregateID, "task_cancelled") {
		currentVersion, valid := validTaskArtifactCancellationForWithdrawal(
			cancelled, event, task, submission, submissionID, withdrawnAt, reason,
		)
		if valid {
			matched++
			matchedVersion = currentVersion
		}
	}
	eventTime, eventTimeOK := automationImportTime(event.createdAt)
	if matched != 1 || !eventTimeOK {
		return taskArtifactSubmissionDispositionImportProof{}, false
	}
	return taskArtifactSubmissionDispositionImportProof{
		status: "withdrawn", eventTime: eventTime, taskVersion: matchedVersion,
	}, true
}

func validTaskArtifactCancellationForWithdrawal(
	cancelled, withdrawn automationImportWorkflowEvent,
	task, submission map[string]any,
	submissionID, withdrawnAt, reason string,
) (int64, bool) {
	if !validTaskArtifactImportEventEnvelope(cancelled, models.BuiltinOwnerActorID, true) ||
		cancelled.requestID == nil || withdrawn.requestID == nil || *cancelled.requestID != *withdrawn.requestID ||
		cancelled.createdAt != withdrawnAt || cancelled.commandSeq == nil || withdrawn.commandSeq == nil ||
		*cancelled.commandSeq <= *withdrawn.commandSeq || cancelled.assignmentID != nil || cancelled.submissionID != nil ||
		cancelled.artifactID != nil || cancelled.agentRunID != nil || cancelled.previousJSON == nil || cancelled.currentJSON == nil {
		return 0, false
	}
	previous, previousOK := automationImportJSONObject(*cancelled.previousJSON)
	current, currentOK := automationImportJSONObject(*cancelled.currentJSON)
	submittedAt, submittedAtOK := submission["submitted_at"].(string)
	if !previousOK || !currentOK || !submittedAtOK ||
		!automationImportObjectHasExactKeys(previous, taskImportLifecycleStateKeys) ||
		!taskArtifactLifecycleStateHasExactKeys(current, true) ||
		(previous["status"] != "waiting_review" && previous["status"] != "blocked") ||
		previous["review_policy"] != "manual" || previous["completed_at"] != nil ||
		previous["submitted_at"] != submittedAt || previous["reviewed_at"] != nil ||
		previous["current_submission_id"] != submissionID || current["status"] != "cancelled" ||
		current["review_policy"] != "manual" || current["blocked_reason"] != nil || current["blocked_at"] != nil ||
		current["blocked_from_status"] != nil || current["completed_at"] != nil || current["submitted_at"] != submittedAt ||
		current["reviewed_at"] != nil || current["current_submission_id"] != submissionID || current["reason"] != reason {
		return 0, false
	}
	if previous["status"] == "waiting_review" {
		if previous["blocked_reason"] != nil || previous["blocked_at"] != nil || previous["blocked_from_status"] != nil {
			return 0, false
		}
	} else {
		blockedReason, reasonOK := previous["blocked_reason"].(string)
		blockedAt, blockedAtOK := previous["blocked_at"].(string)
		_, blockedTimeOK := automationImportTime(blockedAt)
		if !reasonOK || strings.TrimSpace(blockedReason) == "" || !blockedAtOK || !blockedTimeOK ||
			previous["blocked_from_status"] != "waiting_review" {
			return 0, false
		}
	}
	previousVersion, previousVersionOK := automationImportJSONInt64(previous["version"])
	currentVersion, currentVersionOK := automationImportJSONInt64(current["version"])
	finalVersion, finalVersionOK := automationImportInt64(task["version"])
	return currentVersion, previousVersionOK && currentVersionOK && currentVersion == previousVersion+1 &&
		finalVersionOK && finalVersion >= currentVersion
}

func taskArtifactLifecycleStateHasExactKeys(state map[string]any, includeReason bool) bool {
	keys := make([]string, 0, len(taskImportLifecycleStateKeys)+1)
	keys = append(keys, taskImportLifecycleStateKeys...)
	if includeReason {
		keys = append(keys, "reason")
	}
	return automationImportObjectHasExactKeys(state, keys)
}

func validTaskArtifactImportPayload(
	raw string,
	artifact, submission map[string]any,
) (map[string]any, bool) {
	artifactID, artifactIDOK := artifact["id"].(string)
	artifactName, nameOK := artifact["name"].(string)
	storageKind, storageOK := artifact["storage_kind"].(string)
	taskID, taskIDOK := artifact["task_id"].(string)
	submissionID, submissionIDOK := submission["id"].(string)
	sequence, sequenceOK := automationImportInt64(submission["sequence"])
	decoded, decodedOK := automationImportJSONObject(raw)
	taskTitle, titleOK := decoded["task_title"].(string)
	if !artifactIDOK || !nameOK || !storageOK || !taskIDOK || !submissionIDOK ||
		!sequenceOK || sequence < 1 || !decodedOK || !titleOK || strings.TrimSpace(taskTitle) != taskTitle ||
		utf8.RuneCountInString(taskTitle) < 2 || utf8.RuneCountInString(taskTitle) > 200 {
		return nil, false
	}
	expected := map[string]any{
		"artifact_id": artifactID, "artifact_name": artifactName, "storage_kind": storageKind,
		"task_id": taskID, "task_title": taskTitle, "submission_id": submissionID,
		"submission_sequence": sequence,
	}
	if len(decoded) == 9 {
		projectID, projectIDOK := decoded["project_id"].(string)
		projectName, projectNameOK := decoded["project_name"].(string)
		if !projectIDOK || !validCanonicalAutomationUUID(projectID) || !projectNameOK ||
			strings.TrimSpace(projectName) != projectName || utf8.RuneCountInString(projectName) < 2 ||
			utf8.RuneCountInString(projectName) > 100 {
			return nil, false
		}
		expected["project_id"] = projectID
		expected["project_name"] = projectName
	} else if len(decoded) != 7 {
		return nil, false
	}
	if !taskArtifactImportObjectEquals(decoded, expected) {
		return nil, false
	}
	canonical, err := json.Marshal(expected)
	return expected, err == nil && raw == string(canonical)
}

func validTaskArtifactSubmittedImportEvent(
	events automationImportEventIndex,
	taskID, submissionID, createdAt string,
	task, submission map[string]any,
	artifacts []map[string]any,
) (taskArtifactSubmittedImportProof, bool) {
	matched := make([]automationImportWorkflowEvent, 0, 1)
	for _, event := range automationImportAggregateActionEvents(events, "task", taskID, "task_output_submitted") {
		if event.submissionID != nil && *event.submissionID == submissionID {
			matched = append(matched, event)
		}
	}
	if len(matched) != 1 {
		return taskArtifactSubmittedImportProof{}, false
	}
	event := matched[0]
	if !validTaskArtifactImportEventEnvelope(event, models.BuiltinOwnerActorID, true) ||
		event.createdAt != createdAt || event.assignmentID != nil || event.artifactID != nil || event.agentRunID != nil ||
		event.commandSeq == nil || *event.commandSeq != 1 || event.previousJSON == nil || event.currentJSON == nil {
		return taskArtifactSubmittedImportProof{}, false
	}
	previous, previousOK := automationImportJSONObject(*event.previousJSON)
	current, currentOK := automationImportJSONObject(*event.currentJSON)
	if !previousOK || !currentOK || !automationImportObjectHasExactKeys(previous, taskImportLifecycleStateKeys) ||
		!automationImportObjectHasExactKeys(current, taskOutputSubmittedStateKeys) ||
		previous["review_policy"] != "manual" || current["review_policy"] != "manual" ||
		(previous["status"] != "todo" && previous["status"] != "in_progress") || current["status"] != "waiting_review" ||
		current["current_submission_id"] != submissionID || current["submission_id"] != submissionID ||
		current["submitted_at"] != createdAt || current["reviewed_at"] != nil {
		return taskArtifactSubmittedImportProof{}, false
	}
	previousVersion, previousVersionOK := automationImportJSONInt64(previous["version"])
	currentVersion, currentVersionOK := automationImportJSONInt64(current["version"])
	finalTaskVersion, finalVersionOK := automationImportInt64(task["version"])
	sequence, sequenceOK := automationImportInt64(submission["sequence"])
	currentSequence, currentSequenceOK := automationImportJSONInt64(current["submission_sequence"])
	artifactCount, countOK := automationImportJSONInt64(current["artifact_count"])
	submittedAt, submittedAtOK := submission["submitted_at"].(string)
	if !previousVersionOK || !currentVersionOK || currentVersion != previousVersion+1 ||
		!finalVersionOK || finalTaskVersion < currentVersion || !sequenceOK || !currentSequenceOK || currentSequence != sequence ||
		!countOK || artifactCount != int64(len(artifacts)) || !submittedAtOK || submittedAt != createdAt ||
		submission["submitted_by_actor_id"] != models.BuiltinOwnerActorID || submission["origin"] != "manual" {
		return taskArtifactSubmittedImportProof{}, false
	}
	artifactValues, valuesOK := current["artifacts"].([]any)
	if !valuesOK || len(artifactValues) != len(artifacts) {
		return taskArtifactSubmittedImportProof{}, false
	}
	for index, value := range artifactValues {
		snapshot, snapshotOK := value.(map[string]any)
		if !snapshotOK || !validTaskOutputArtifactImportSnapshot(snapshot, artifacts[index], false) {
			return taskArtifactSubmittedImportProof{}, false
		}
	}
	return taskArtifactSubmittedImportProof{event: event, taskVersion: currentVersion}, true
}

func validTaskArtifactProjectedImportEvent(
	events automationImportEventIndex,
	inboxID, artifactID, sourceKey, createdAt string,
	submission, artifact, payload map[string]any,
	submittedEvent automationImportWorkflowEvent,
) bool {
	projected := automationImportAggregateActionEvents(events, "inbox_item", inboxID, "source_projected")
	if len(projected) != 1 {
		return false
	}
	event := projected[0]
	if !validTaskArtifactImportEventEnvelope(event, models.BuiltinSystemActorID, true) || event.createdAt != createdAt ||
		event.requestID == nil || submittedEvent.requestID == nil || *event.requestID != *submittedEvent.requestID ||
		event.assignmentID != nil || event.submissionID != nil || event.artifactID != nil || event.agentRunID != nil ||
		event.commandSeq == nil || *event.commandSeq != 1 || event.previousJSON != nil || event.currentJSON == nil {
		return false
	}
	current, ok := automationImportJSONObject(*event.currentJSON)
	artifactName, nameOK := artifact["name"].(string)
	taskTitle, titleOK := payload["task_title"].(string)
	sequence, sequenceOK := automationImportInt64(submission["sequence"])
	priority, priorityOK := current["priority"].(string)
	_, priorityValid := validPriorities[priority]
	if !ok || !nameOK || !titleOK || !priorityOK || !priorityValid || !sequenceOK ||
		!automationImportObjectHasExactKeys(current, projectCompletionInboxEventStateKeys) {
		return false
	}
	expected := map[string]any{
		"kind": "event", "title": taskArtifactFollowupTitle(artifactName),
		"summary":            fmt.Sprintf("任务「%s」第 %d 批产出已明确标记为需要后续处理。", taskTitle, sequence),
		"source_entity_type": taskArtifactInboxSourceType, "source_entity_id": artifactID,
		"source_event_key": sourceKey, "source_deleted_at": nil, "priority": priority,
		"resolution_policy": "manual", "status": "open", "due_at": nil, "read_at": nil,
		"triaged_at": nil, "snoozed_until": nil, "resolved_by_actor_id": nil, "resolved_at": nil,
		"resolution_reason": nil, "resolution_mode": nil, "dismissed_by_actor_id": nil,
		"dismissed_at": nil, "dismiss_reason": nil, "payload_json": payload, "version": int64(1),
	}
	return taskArtifactImportObjectEquals(current, expected)
}

func validTaskArtifactSourceDeletedImportEvent(
	events automationImportEventIndex,
	inboxID, artifactID, sourceKey, deletedAt string,
	payload, finalRow map[string]any,
) (taskArtifactSourceDeletedImportProof, bool) {
	deleted := automationImportAggregateActionEvents(events, "inbox_item", inboxID, "source_deleted")
	if len(deleted) != 1 {
		return taskArtifactSourceDeletedImportProof{}, false
	}
	event := deleted[0]
	if !validTaskArtifactImportEventEnvelope(event, models.BuiltinOwnerActorID, true) || event.createdAt != deletedAt ||
		event.assignmentID != nil || event.submissionID != nil || event.artifactID != nil || event.agentRunID != nil ||
		event.commandSeq == nil || *event.commandSeq != 1 || event.previousJSON == nil || event.currentJSON == nil {
		return taskArtifactSourceDeletedImportProof{}, false
	}
	previous, previousOK := automationImportJSONObject(*event.previousJSON)
	current, currentOK := automationImportJSONObject(*event.currentJSON)
	if !previousOK || !currentOK ||
		!automationImportObjectHasExactKeys(previous, projectCompletionInboxEventStateKeys) ||
		!automationImportObjectHasExactKeys(current, projectCompletionInboxEventStateKeys) ||
		previous["source_deleted_at"] != nil || current["source_deleted_at"] != deletedAt ||
		(previous["status"] != "resolved" && previous["status"] != "dismissed") ||
		current["source_entity_type"] != taskArtifactInboxSourceType || current["source_entity_id"] != artifactID ||
		current["source_event_key"] != sourceKey || !taskArtifactImportValueEquals(current["payload_json"], payload) {
		return taskArtifactSourceDeletedImportProof{}, false
	}
	for key, value := range previous {
		if key != "source_deleted_at" && key != "version" && !taskArtifactImportValueEquals(value, current[key]) {
			return taskArtifactSourceDeletedImportProof{}, false
		}
	}
	previousVersion, previousVersionOK := automationImportJSONInt64(previous["version"])
	currentVersion, currentVersionOK := automationImportJSONInt64(current["version"])
	if !previousVersionOK || previousVersion < 2 || !currentVersionOK || currentVersion != previousVersion+1 ||
		!validTaskArtifactInboxPostDeleteImportHistory(events, inboxID, deletedAt, current, finalRow) {
		return taskArtifactSourceDeletedImportProof{}, false
	}
	return taskArtifactSourceDeletedImportProof{
		event: event, previous: previous, current: current,
		previousVersion: previousVersion, currentVersion: currentVersion,
	}, true
}

func validTaskArtifactInboxPostDeleteImportHistory(
	events automationImportEventIndex,
	inboxID, deletedAt string,
	deletedState, finalRow map[string]any,
) bool {
	finalVersion, finalVersionOK := automationImportInt64(finalRow["version"])
	currentVersion, currentVersionOK := automationImportJSONInt64(deletedState["version"])
	finalUpdatedAt, updatedAtOK := finalRow["updated_at"].(string)
	lastTime, deletedTimeOK := automationImportTime(deletedAt)
	finalUpdatedTime, finalUpdatedTimeOK := automationImportTime(finalUpdatedAt)
	if !finalVersionOK || !currentVersionOK || finalVersion < currentVersion || !updatedAtOK ||
		!deletedTimeOK || !finalUpdatedTimeOK || finalUpdatedTime.Before(lastTime) {
		return false
	}
	state := deletedState
	consumed := make(map[string]struct{})
	for currentVersion < finalVersion {
		matched := make([]struct {
			event   automationImportWorkflowEvent
			current map[string]any
		}, 0, 1)
		for _, candidate := range automationImportAggregateEvents(events, "inbox_item", inboxID) {
			if candidate.action == "source_projected" || candidate.action == "source_deleted" ||
				candidate.previousJSON == nil || candidate.currentJSON == nil {
				continue
			}
			previous, previousOK := automationImportJSONObject(*candidate.previousJSON)
			next, nextOK := automationImportJSONObject(*candidate.currentJSON)
			previousState, previousStateOK := taskArtifactInboxImportEventState(previous, false)
			nextState, nextStateOK := taskArtifactInboxImportEventState(next, true)
			previousEventVersion, versionOK := automationImportJSONInt64(previousState["version"])
			if !previousOK || !nextOK || !previousStateOK || !nextStateOK || !versionOK || previousEventVersion != currentVersion ||
				!taskArtifactImportObjectEquals(previousState, state) {
				continue
			}
			if !validTaskArtifactInboxPostDeleteImportEvent(candidate, previous, next, previousState, nextState, lastTime) {
				return false
			}
			matched = append(matched, struct {
				event   automationImportWorkflowEvent
				current map[string]any
			}{event: candidate, current: nextState})
		}
		if len(matched) != 1 {
			return false
		}
		step := matched[0]
		if _, duplicate := consumed[step.event.id]; duplicate {
			return false
		}
		consumed[step.event.id] = struct{}{}
		state = step.current
		currentVersion, _ = automationImportJSONInt64(state["version"])
		lastTime, _ = automationImportTime(step.event.createdAt)
	}
	return lastTime.Equal(finalUpdatedTime) && taskArtifactInboxStateMatchesRow(state, finalRow)
}

func taskArtifactInboxImportEventState(state map[string]any, allowReason bool) (map[string]any, bool) {
	if automationImportObjectHasExactKeys(state, projectCompletionInboxEventStateKeys) {
		return state, true
	}
	if !allowReason || len(state) != len(projectCompletionInboxEventStateKeys)+1 {
		return nil, false
	}
	reason, reasonOK := state["reason"].(string)
	if !reasonOK || strings.TrimSpace(reason) != reason || reason == "" || utf8.RuneCountInString(reason) > 1_000 {
		return nil, false
	}
	copyState := make(map[string]any, len(projectCompletionInboxEventStateKeys))
	for _, key := range projectCompletionInboxEventStateKeys {
		value, exists := state[key]
		if !exists {
			return nil, false
		}
		copyState[key] = value
	}
	return copyState, true
}

func validTaskArtifactInboxPostDeleteImportEvent(
	event automationImportWorkflowEvent,
	rawPrevious, rawCurrent, previous, current map[string]any,
	lastTime time.Time,
) bool {
	expectedActor := models.BuiltinOwnerActorID
	switch event.action {
	case "read", "updated", "snoozed", "unsnoozed", "resolved", "dismissed", "reopened", "force_resolved":
	case "automatically_resolved", "automatically_reopened":
		expectedActor = models.BuiltinSystemActorID
	default:
		return false
	}
	eventTime, eventTimeOK := automationImportTime(event.createdAt)
	previousVersion, previousVersionOK := automationImportJSONInt64(previous["version"])
	currentVersion, currentVersionOK := automationImportJSONInt64(current["version"])
	if !eventTimeOK || eventTime.Before(lastTime) || !previousVersionOK || !currentVersionOK ||
		currentVersion != previousVersion+1 || !validTaskArtifactImportEventEnvelope(event, expectedActor, true) ||
		event.assignmentID != nil || event.submissionID != nil || event.artifactID != nil || event.agentRunID != nil ||
		event.commandSeq == nil || *event.commandSeq != 1 || previous["source_deleted_at"] == nil ||
		!taskArtifactInboxStateEqualExcept(previous, current, taskArtifactInboxPostDeleteChangedKeys(event.action)) {
		return false
	}
	reason, hasReason := rawCurrent["reason"].(string)
	if _, previousHasReason := rawPrevious["reason"]; previousHasReason {
		return false
	}
	switch event.action {
	case "read":
		return !hasReason && previous["read_at"] == nil && current["read_at"] == event.createdAt
	case "updated":
		return !hasReason && taskArtifactInboxNonterminalStatus(previous["status"]) &&
			taskArtifactImportValueEquals(current["status"], previous["status"]) &&
			taskArtifactInboxTriagedAtTransition(previous, current, event.createdAt) &&
			(!taskArtifactImportValueEquals(previous["title"], current["title"]) ||
				!taskArtifactImportValueEquals(previous["summary"], current["summary"]) ||
				!taskArtifactImportValueEquals(previous["priority"], current["priority"]) ||
				!taskArtifactImportValueEquals(previous["due_at"], current["due_at"]))
	case "snoozed":
		snoozedUntil, ok := current["snoozed_until"].(string)
		until, timeOK := automationImportTime(snoozedUntil)
		return !hasReason && ok && timeOK && until.After(eventTime) && taskArtifactInboxNonterminalStatus(previous["status"]) &&
			taskArtifactImportValueEquals(current["status"], previous["status"]) &&
			taskArtifactInboxTriagedAtTransition(previous, current, event.createdAt)
	case "unsnoozed":
		return !hasReason && taskArtifactInboxNonterminalStatus(previous["status"]) && previous["snoozed_until"] != nil &&
			current["snoozed_until"] == nil && taskArtifactImportValueEquals(current["status"], previous["status"]) &&
			taskArtifactInboxTriagedAtTransition(previous, current, event.createdAt)
	case "resolved", "force_resolved", "automatically_resolved":
		mode := "manual"
		actorID := models.BuiltinOwnerActorID
		resolutionReason := reason
		if event.action == "force_resolved" {
			mode = "forced"
		}
		if event.action == "automatically_resolved" {
			mode = "automatic"
			actorID = models.BuiltinSystemActorID
			resolutionReason = "所有必需任务已完成"
			if hasReason {
				return false
			}
		} else if !hasReason || reason == "" {
			return false
		}
		return taskArtifactInboxNonterminalStatus(previous["status"]) && current["status"] == "resolved" &&
			current["snoozed_until"] == nil && current["resolved_by_actor_id"] == actorID && current["resolved_at"] == event.createdAt &&
			current["resolution_reason"] == resolutionReason && current["resolution_mode"] == mode &&
			taskArtifactInboxTriagedAtTransition(previous, current, event.createdAt)
	case "dismissed":
		return hasReason && reason != "" && taskArtifactInboxNonterminalStatus(previous["status"]) && current["status"] == "dismissed" &&
			current["snoozed_until"] == nil && current["dismissed_by_actor_id"] == models.BuiltinOwnerActorID &&
			current["dismissed_at"] == event.createdAt && current["dismiss_reason"] == reason &&
			taskArtifactInboxTriagedAtTransition(previous, current, event.createdAt)
	case "reopened", "automatically_reopened":
		if hasReason && event.action != "reopened" {
			return false
		}
		return taskArtifactInboxTerminalStatus(previous["status"]) && taskArtifactInboxNonterminalStatus(current["status"]) &&
			current["snoozed_until"] == nil && current["resolved_by_actor_id"] == nil && current["resolved_at"] == nil &&
			current["resolution_reason"] == nil && current["resolution_mode"] == nil && current["dismissed_by_actor_id"] == nil &&
			current["dismissed_at"] == nil && current["dismiss_reason"] == nil
	default:
		return false
	}
}

func taskArtifactInboxPostDeleteChangedKeys(action string) map[string]struct{} {
	keys := map[string]struct{}{"version": {}}
	switch action {
	case "read":
		keys["read_at"] = struct{}{}
	case "updated":
		for _, key := range []string{"title", "summary", "priority", "due_at", "triaged_at"} {
			keys[key] = struct{}{}
		}
	case "snoozed", "unsnoozed":
		keys["snoozed_until"] = struct{}{}
		keys["triaged_at"] = struct{}{}
	case "resolved", "force_resolved", "automatically_resolved":
		for _, key := range []string{"status", "snoozed_until", "triaged_at", "resolved_by_actor_id", "resolved_at", "resolution_reason", "resolution_mode"} {
			keys[key] = struct{}{}
		}
	case "dismissed":
		for _, key := range []string{"status", "snoozed_until", "triaged_at", "dismissed_by_actor_id", "dismissed_at", "dismiss_reason"} {
			keys[key] = struct{}{}
		}
	case "reopened", "automatically_reopened":
		for _, key := range []string{"status", "snoozed_until", "resolved_by_actor_id", "resolved_at", "resolution_reason", "resolution_mode", "dismissed_by_actor_id", "dismissed_at", "dismiss_reason"} {
			keys[key] = struct{}{}
		}
	}
	return keys
}

func taskArtifactInboxStateEqualExcept(previous, current map[string]any, changed map[string]struct{}) bool {
	for _, key := range projectCompletionInboxEventStateKeys {
		if _, allowed := changed[key]; allowed {
			continue
		}
		if !taskArtifactImportValueEquals(previous[key], current[key]) {
			return false
		}
	}
	return true
}

func taskArtifactInboxTriagedAtTransition(previous, current map[string]any, createdAt string) bool {
	if previous["triaged_at"] == nil {
		return current["triaged_at"] == createdAt
	}
	return taskArtifactImportValueEquals(current["triaged_at"], previous["triaged_at"])
}

func taskArtifactInboxTerminalStatus(value any) bool {
	return value == "resolved" || value == "dismissed"
}

func taskArtifactInboxNonterminalStatus(value any) bool {
	return value == "open" || value == "tracking"
}

func validTaskArtifactDeletedImportEvent(
	validation *taskArtifactImportValidationIndex,
	taskID, submissionID, artifactID, deletedAt, deleteReason string,
	artifact, task map[string]any,
	sourceDeletedEvent *automationImportWorkflowEvent,
	submitted taskArtifactSubmittedImportProof,
) (int64, bool) {
	event, exists := validation.deletedEventByArtifactID[artifactID]
	if !exists || event.aggregateType != "task" || event.aggregateID != taskID ||
		event.action != "task_artifact_deleted" || event.artifactID == nil || *event.artifactID != artifactID {
		return 0, false
	}
	if !validTaskArtifactImportEventEnvelope(event, models.BuiltinOwnerActorID, true) || event.createdAt != deletedAt ||
		event.submissionID == nil || *event.submissionID != submissionID ||
		event.assignmentID != nil || event.agentRunID != nil || event.commandSeq == nil || *event.commandSeq != 1 ||
		event.previousJSON == nil || event.currentJSON == nil || event.requestID == nil {
		return 0, false
	}
	if sourceDeletedEvent != nil && (sourceDeletedEvent.requestID == nil || *event.requestID != *sourceDeletedEvent.requestID) {
		return 0, false
	}
	previous, previousOK := automationImportJSONObject(*event.previousJSON)
	current, currentOK := automationImportJSONObject(*event.currentJSON)
	if !previousOK || !currentOK || !automationImportObjectHasExactKeys(previous, []string{"artifact", "version"}) ||
		!automationImportObjectHasExactKeys(current, []string{"artifact_id", "deleted_at", "reason", "version"}) ||
		current["artifact_id"] != artifactID || current["deleted_at"] != deletedAt || current["reason"] != deleteReason {
		return 0, false
	}
	snapshot, snapshotOK := previous["artifact"].(map[string]any)
	if !snapshotOK || !validTaskOutputArtifactImportSnapshot(snapshot, artifact, true) {
		return 0, false
	}
	previousVersion, previousVersionOK := automationImportJSONInt64(previous["version"])
	currentVersion, currentVersionOK := automationImportJSONInt64(current["version"])
	finalVersion, finalVersionOK := automationImportInt64(task["version"])
	if !previousVersionOK || previousVersion < submitted.taskVersion || !currentVersionOK || currentVersion != previousVersion+1 ||
		!finalVersionOK || finalVersion < currentVersion {
		return 0, false
	}
	return previousVersion, true
}

func validTaskArtifactSubmissionAssignmentActors(
	assignments []map[string]any,
	taskID, producedByActorID, submittedAt string,
) bool {
	submittedTime, submittedOK := automationImportTime(submittedAt)
	if !submittedOK {
		return false
	}
	assignees := 0
	reviewers := 0
	for _, assignment := range assignments {
		if assignment["task_id"] != taskID {
			continue
		}
		assignedAt, assignedAtOK := assignment["assigned_at"].(string)
		assignedTime, assignedTimeOK := taskArtifactImportTime(assignedAt)
		unassignedAt, unassignedOK := automationImportOptionalString(assignment["unassigned_at"])
		if !assignedAtOK || !assignedTimeOK || !unassignedOK || submittedTime.Before(assignedTime) {
			continue
		}
		if unassignedAt != nil {
			unassignedTime, valid := taskArtifactImportTime(*unassignedAt)
			if !valid || !submittedTime.Before(unassignedTime) {
				continue
			}
		}
		role, roleOK := assignment["role"].(string)
		actorID, actorOK := assignment["actor_id"].(string)
		if !roleOK || !actorOK {
			return false
		}
		switch role {
		case "assignee":
			if actorID != producedByActorID {
				return false
			}
			assignees++
		case "reviewer":
			if actorID != models.BuiltinOwnerActorID {
				return false
			}
			reviewers++
		default:
			return false
		}
	}
	return assignees == 1 && reviewers == 1
}

func validTaskOutputArtifactImportSnapshot(snapshot, artifact map[string]any, includeSubmission bool) bool {
	keys := taskOutputArtifactSnapshotKeys
	if includeSubmission {
		keys = taskArtifactDeletedSnapshotKeys
	}
	if !automationImportObjectHasExactKeys(snapshot, keys) {
		return false
	}
	requiresFollowup, followupOK := automationImportInt64(artifact["requires_followup"])
	position, positionOK := automationImportInt64(artifact["position"])
	if !followupOK || (requiresFollowup != 0 && requiresFollowup != 1) || !positionOK {
		return false
	}
	expected := map[string]any{
		"id": artifact["id"], "position": position, "storage_kind": artifact["storage_kind"],
		"name": artifact["name"], "mime_type": artifact["mime_type"], "size_bytes": artifact["size_bytes"],
		"sha256": artifact["sha256"], "requires_followup": requiresFollowup == 1,
		"produced_by_actor_id": artifact["produced_by_actor_id"],
		"recorded_by_actor_id": artifact["recorded_by_actor_id"],
	}
	if includeSubmission {
		expected["submission_id"] = artifact["submission_id"]
	}
	return taskArtifactImportObjectEquals(snapshot, expected)
}

func validTaskArtifactImportEventEnvelope(event automationImportWorkflowEvent, actorID string, requireRequest bool) bool {
	if !validAutomationImportEventEnvelope(event) || event.actorID == nil || *event.actorID != actorID {
		return false
	}
	if !requireRequest {
		return event.requestID == nil
	}
	return event.requestID != nil && validCanonicalAutomationUUID(*event.requestID)
}

func taskArtifactInboxStateMatchesRow(state, row map[string]any) bool {
	if !automationImportObjectHasExactKeys(state, projectCompletionInboxEventStateKeys) {
		return false
	}
	payloadRaw, ok := row["payload_json"].(string)
	if !ok {
		return false
	}
	payload, ok := automationImportJSONObject(payloadRaw)
	if !ok {
		return false
	}
	for _, key := range projectCompletionInboxEventStateKeys {
		expected := row[key]
		if key == "payload_json" {
			expected = payload
		}
		if !taskArtifactImportValueEquals(state[key], expected) {
			return false
		}
	}
	return true
}

func taskArtifactImportObjectEquals(actual, expected map[string]any) bool {
	if len(actual) != len(expected) {
		return false
	}
	for key, expectedValue := range expected {
		actualValue, exists := actual[key]
		if !exists || !taskArtifactImportValueEquals(actualValue, expectedValue) {
			return false
		}
	}
	return true
}

func taskArtifactImportValueEquals(actual, expected any) bool {
	if actualInteger, actualOK := importInteger(actual); actualOK {
		expectedInteger, expectedOK := importInteger(expected)
		return expectedOK && actualInteger == expectedInteger
	}
	if actualObject, actualOK := actual.(map[string]any); actualOK {
		expectedObject, expectedOK := expected.(map[string]any)
		return expectedOK && taskArtifactImportObjectEquals(actualObject, expectedObject)
	}
	if actualValues, actualOK := actual.([]any); actualOK {
		expectedValues, expectedOK := expected.([]any)
		if !expectedOK || len(actualValues) != len(expectedValues) {
			return false
		}
		for index := range actualValues {
			if !taskArtifactImportValueEquals(actualValues[index], expectedValues[index]) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(actual, expected)
}

func activateHistoricalAssignmentActors(tx *gorm.DB, table businessExportTable) ([]string, error) {
	actorIndex := columnIndex(table.Columns, "actor_id")
	assignedByIndex := columnIndex(table.Columns, "assigned_by_actor_id")
	if len(table.Rows) == 0 {
		return nil, nil
	}
	if actorIndex < 0 || assignedByIndex < 0 {
		return nil, errors.New("historical Task assignment actor columns are incomplete")
	}
	seen := make(map[string]struct{})
	ids := make([]string, 0)
	for _, row := range table.Rows {
		for _, index := range []int{actorIndex, assignedByIndex} {
			id, ok := row[index].(string)
			if !ok || id == "" {
				return nil, errors.New("historical Task assignment actor is invalid")
			}
			if _, duplicate := seen[id]; duplicate {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	activated := make([]string, 0)
	for _, id := range ids {
		var status string
		if err := tx.Table("actors").Select("status").Where("id = ?", id).Row().Scan(&status); err != nil {
			return nil, fmt.Errorf("read historical Task assignment actor: %w", err)
		}
		if status == "active" {
			continue
		}
		if status != "inactive" {
			return nil, errors.New("historical Task assignment actor status is invalid")
		}
		result := tx.Exec("UPDATE actors SET status = 'active' WHERE id = ? AND status = 'inactive'", id)
		if result.Error != nil {
			return nil, fmt.Errorf("activate historical Task assignment actor: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return nil, errors.New("historical Task assignment actor changed during import")
		}
		activated = append(activated, id)
	}
	return activated, nil
}

func restoreHistoricalAssignmentActors(tx *gorm.DB, actorIDs []string) error {
	for index := len(actorIDs) - 1; index >= 0; index-- {
		result := tx.Exec("UPDATE actors SET status = 'inactive' WHERE id = ? AND status = 'active'", actorIDs[index])
		if result.Error != nil {
			return fmt.Errorf("restore historical Task assignment actor: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return errors.New("historical Task assignment actor was not restored")
		}
	}
	return nil
}

func insertTaskArtifactImportRows(
	tx *gorm.DB,
	table businessExportTable,
	replays taskArtifactImportReplays,
) error {
	if len(replays.ordered) == 0 {
		return insertBusinessImportRows(tx, table)
	}
	idIndex := columnIndex(table.Columns, "id")
	deletedAtIndex := columnIndex(table.Columns, "deleted_at")
	deletedByIndex := columnIndex(table.Columns, "deleted_by_actor_id")
	reasonIndex := columnIndex(table.Columns, "delete_reason")
	if idIndex < 0 || deletedAtIndex < 0 || deletedByIndex < 0 || reasonIndex < 0 {
		return errors.New("Task Artifact import columns are incomplete")
	}
	staged := businessExportTable{Name: table.Name, Columns: table.Columns, Rows: make([][]any, 0, len(table.Rows))}
	for _, row := range table.Rows {
		artifactID, _ := row[idIndex].(string)
		if _, replay := replays.byArtifactID[artifactID]; !replay {
			staged.Rows = append(staged.Rows, row)
			continue
		}
		copyRow := append([]any(nil), row...)
		copyRow[deletedAtIndex] = nil
		copyRow[deletedByIndex] = nil
		copyRow[reasonIndex] = nil
		staged.Rows = append(staged.Rows, copyRow)
	}
	return insertBusinessImportRows(tx, staged)
}

func insertTaskArtifactInboxImportRows(
	tx *gorm.DB,
	table businessExportTable,
	replays taskArtifactImportReplays,
) error {
	if len(replays.ordered) == 0 {
		return insertBusinessImportRows(tx, table)
	}
	idIndex := columnIndex(table.Columns, "id")
	deletedAtIndex := columnIndex(table.Columns, "source_deleted_at")
	versionIndex := columnIndex(table.Columns, "version")
	updatedAtIndex := columnIndex(table.Columns, "updated_at")
	if idIndex < 0 || deletedAtIndex < 0 || versionIndex < 0 || updatedAtIndex < 0 {
		return errors.New("Task Artifact Inbox import columns are incomplete")
	}
	staged := businessExportTable{Name: table.Name, Columns: table.Columns, Rows: make([][]any, 0, len(table.Rows))}
	for _, row := range table.Rows {
		inboxID, _ := row[idIndex].(string)
		replay, planned := replays.byInboxID[inboxID]
		if !planned {
			staged.Rows = append(staged.Rows, row)
			continue
		}
		copyRow := append([]any(nil), row...)
		for index, column := range table.Columns {
			if column == "payload_json" {
				continue
			}
			if value, exists := replay.deletedInboxPrevious[column]; exists {
				copyRow[index] = value
			}
		}
		copyRow[deletedAtIndex] = nil
		copyRow[versionIndex] = replay.previousInboxVersion
		copyRow[updatedAtIndex] = replay.deletedAt
		staged.Rows = append(staged.Rows, copyRow)
	}
	return insertBusinessImportRows(tx, staged)
}

func finalizeTaskArtifactImportReplays(
	tx *gorm.DB,
	artifactTable businessExportTable,
	inboxTable businessExportTable,
	replays taskArtifactImportReplays,
) error {
	if len(replays.ordered) == 0 {
		return nil
	}
	artifactRows, ok := importRowsByStringColumn(artifactTable, "id")
	if !ok {
		return errors.New("Task Artifact import rows are invalid")
	}
	inboxRows, ok := importRowsByStringColumn(inboxTable, "id")
	if !ok {
		return errors.New("Task Artifact Inbox import rows are invalid")
	}
	for _, replay := range replays.ordered {
		finalInbox, inboxExists := inboxRows[replay.inboxID]
		finalArtifact, artifactExists := artifactRows[replay.artifactID]
		if !inboxExists || !artifactExists {
			return errors.New("Task Artifact import replay row is missing")
		}
		finalInboxVersion, versionOK := importInteger(finalInbox[columnIndex(inboxTable.Columns, "version")])
		finalUpdatedAt, updatedOK := finalInbox[columnIndex(inboxTable.Columns, "updated_at")].(string)
		if !versionOK || !updatedOK {
			return errors.New("Task Artifact Inbox import replay state is invalid")
		}
		inboxResult := tx.Exec(`
			UPDATE inbox_items
			SET source_deleted_at = ?, version = ?, updated_at = ?
			WHERE id = ? AND source_deleted_at IS NULL AND version = ?
		`, replay.deletedAt, replay.deletedInboxVersion, replay.deletedAt, replay.inboxID, replay.previousInboxVersion)
		if inboxResult.Error != nil {
			return fmt.Errorf("restore Task Artifact Inbox tombstone: %w", inboxResult.Error)
		}
		if inboxResult.RowsAffected != 1 {
			return errors.New("Task Artifact Inbox replay did not match its staged row")
		}
		artifactResult := tx.Exec(`
			UPDATE task_artifacts
			SET deleted_at = ?, deleted_by_actor_id = ?, delete_reason = ?
			WHERE id = ? AND deleted_at IS NULL
		`, replay.deletedAt, replay.deletedByActorID, replay.deleteReason, replay.artifactID)
		if artifactResult.Error != nil {
			return fmt.Errorf("restore Task Artifact soft deletion: %w", artifactResult.Error)
		}
		if artifactResult.RowsAffected != 1 {
			return errors.New("Task Artifact replay did not match its staged row")
		}
		if finalInboxVersion != replay.deletedInboxVersion {
			updates, valid := taskArtifactInboxFinalImportUpdates(inboxTable, finalInbox)
			if !valid {
				return errors.New("Task Artifact Inbox final replay state is invalid")
			}
			result := tx.Table("inbox_items").Where(
				"id = ? AND source_deleted_at = ? AND version = ?",
				replay.inboxID, replay.deletedAt, replay.deletedInboxVersion,
			).Updates(updates)
			if result.Error != nil {
				return fmt.Errorf("restore Task Artifact Inbox post-deletion history: %w", result.Error)
			}
			if result.RowsAffected != 1 {
				return errors.New("Task Artifact Inbox post-deletion replay did not match its tombstone")
			}
		} else if finalUpdatedAt != replay.deletedAt {
			return errors.New("Task Artifact Inbox deletion timestamp is inconsistent")
		}
		if err := verifyExactBusinessImportRow(tx, artifactTable, finalArtifact); err != nil {
			return err
		}
		if err := verifyExactBusinessImportRow(tx, inboxTable, finalInbox); err != nil {
			return err
		}
	}
	return nil
}

func taskArtifactInboxFinalImportUpdates(
	table businessExportTable,
	row []any,
) (map[string]any, bool) {
	columns := []string{
		"title", "summary", "priority", "resolution_policy", "status", "due_at", "read_at", "triaged_at",
		"snoozed_until", "resolved_by_actor_id", "resolved_at", "resolution_reason", "resolution_mode",
		"dismissed_by_actor_id", "dismissed_at", "dismiss_reason", "version", "updated_at",
	}
	updates := make(map[string]any, len(columns))
	for _, column := range columns {
		index := columnIndex(table.Columns, column)
		if index < 0 || index >= len(row) {
			return nil, false
		}
		updates[column] = row[index]
	}
	return updates, true
}

func importRowsByStringColumn(table businessExportTable, column string) (map[string][]any, bool) {
	index := columnIndex(table.Columns, column)
	if index < 0 {
		return nil, false
	}
	result := make(map[string][]any, len(table.Rows))
	for _, row := range table.Rows {
		value, ok := row[index].(string)
		if !ok || value == "" {
			return nil, false
		}
		if _, duplicate := result[value]; duplicate {
			return nil, false
		}
		result[value] = row
	}
	return result, true
}

func verifyExactBusinessImportRow(tx *gorm.DB, table businessExportTable, expected []any) error {
	idIndex := columnIndex(table.Columns, "id")
	if idIndex < 0 {
		return errors.New("business import replay table has no id")
	}
	id, ok := expected[idIndex].(string)
	if !ok || id == "" {
		return errors.New("business import replay row id is invalid")
	}
	quoted := make([]string, len(table.Columns))
	for index, column := range table.Columns {
		quoted[index] = quoteIdentifier(column)
	}
	actual := make([]any, len(table.Columns))
	destinations := make([]any, len(actual))
	for index := range actual {
		destinations[index] = &actual[index]
	}
	row := tx.Raw(
		"SELECT "+strings.Join(quoted, ",")+" FROM "+quoteIdentifier(table.Name)+" WHERE id = ?", id,
	).Row()
	if err := row.Scan(destinations...); err != nil {
		return fmt.Errorf("read restored import row %s: %w", table.Name, err)
	}
	want, err := canonicalBusinessImportKey(append([]any(nil), expected...))
	if err != nil {
		return err
	}
	got, err := canonicalBusinessImportKey(actual)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("restored import row %s does not match its package", table.Name)
	}
	return nil
}
