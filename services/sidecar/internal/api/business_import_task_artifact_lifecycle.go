package api

import (
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/opc-workspace/opc-sidecar/internal/models"
)

var taskImportLifecycleStateKeys = []string{
	"status", "review_policy", "blocked_reason", "blocked_at", "blocked_from_status",
	"completed_at", "submitted_at", "reviewed_at", "current_submission_id", "version",
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

func taskArtifactImportTime(value string) (time.Time, bool) {
	if parsed, ok := automationImportTime(value); ok {
		return parsed, true
	}
	const sqliteTimestamp = "2006-01-02 15:04:05"
	parsed, err := time.ParseInLocation(sqliteTimestamp, value, time.UTC)
	return parsed, err == nil && parsed.Format(sqliteTimestamp) == value
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
