package api

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

var taskAssignmentImportStateKeys = []string{
	"id", "task_id", "role", "actor_id", "actor", "assigned_by_actor_id", "assigned_by_actor",
	"assigned_at", "unassigned_at", "reason", "is_active", "inferred",
}

var taskAssignmentActorImportStateKeys = []string{
	"id", "type", "display_name", "status", "is_builtin", "version",
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
