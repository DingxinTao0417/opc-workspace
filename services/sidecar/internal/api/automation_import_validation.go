package api

import (
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/models"
)

type automationImportRule struct {
	id         string
	preset     automationPresetDefinition
	version    int64
	configJSON string
}

type automationImportRun struct {
	id                 string
	rule               automationImportRule
	ruleVersion        int64
	triggerType        string
	sourceEventID      *string
	scheduledFor       *string
	logicalKey         string
	dedupeKey          string
	status             string
	attempt            int
	retryOfRunID       *string
	retryable          bool
	retryAt            *string
	configSnapshotJSON string
	actionSnapshotJSON string
	actionSnapshot     map[string]any
	actionValid        bool
	attemptContractOK  bool
	errorCode          *string
	resultType         *string
	resultID           *string
	resultSummary      string
	startedAt          string
}

type automationImportWorkflowEvent struct {
	id            string
	aggregateType string
	aggregateID   string
	action        string
	actorID       *string
	assignmentID  *string
	submissionID  *string
	artifactID    *string
	agentRunID    *string
	requestID     *string
	commandSeq    *int64
	previousJSON  *string
	currentJSON   *string
	createdAt     string
}

type automationImportEventIndex struct {
	byID              map[string]automationImportWorkflowEvent
	byAggregate       map[string][]automationImportWorkflowEvent
	byAggregateAction map[string][]automationImportWorkflowEvent
}

type automationImportRuleVersionProof struct {
	enabled    bool
	configJSON string
	createdAt  string
}

// validAutomationImportGraph validates the portable Automation history before
// target conflict scans, rollback-capacity probes, or writes. SQLite constraints
// protect an applied database, but they cannot prove that separately valid rows
// still describe one Automation execution graph.
func validAutomationImportGraph(packageData businessExportPackage) bool {
	tables := make(map[string]businessExportTable, len(packageData.Tables))
	for _, table := range packageData.Tables {
		tables[table.Name] = table
	}

	rules, ok := automationImportRules(tables["automation_rules"])
	if !ok {
		return false
	}
	events, ok := automationImportWorkflowEvents(tables["workflow_events"])
	if !ok {
		return false
	}
	ruleVersionProofs, ok := automationImportRuleVersionProofs(rules, events)
	if !ok {
		return false
	}
	runs, ok := automationImportRuns(tables["automation_runs"], rules)
	if !ok || !validAutomationImportRunChains(runs) {
		return false
	}

	inboxItems, ok := automationImportRowsByID(tables["inbox_items"])
	if !ok {
		return false
	}
	inboxSourceKeyCounts := make(map[string]int)
	for _, row := range inboxItems {
		if sourceKey, ok := row["source_event_key"].(string); ok {
			inboxSourceKeyCounts[sourceKey]++
		}
	}
	tasks, ok := automationImportRowsByID(tables["tasks"])
	if !ok {
		return false
	}
	reminders, ok := automationImportRowsByID(tables["reminders"])
	if !ok {
		return false
	}

	for _, run := range runs {
		if !validAutomationImportRuleVersionEvent(run, ruleVersionProofs) || !validAutomationImportSourceEvent(run, events) ||
			!validAutomationImportRunEvent(run, events) ||
			!validAutomationImportResult(run, events, inboxItems, tasks, reminders) ||
			!validAutomationImportFailureEvidence(run, inboxSourceKeyCounts) {
			return false
		}
	}
	return validAutomationImportReverseRelations(runs, events, inboxItems, reminders)
}

func validAutomationImportFailureEvidence(run automationImportRun, inboxSourceKeyCounts map[string]int) bool {
	if run.status != "failed" || run.errorCode == nil || *run.errorCode != "SOURCE_EVENT_CONFLICT" {
		return true
	}
	if run.rule.preset.PresetKey != automationPresetProjectCompleted {
		return false
	}
	return inboxSourceKeyCounts["automation:"+run.logicalKey] == 1
}

func validAutomationImportRuleVersionEvent(run automationImportRun, proofs map[string]automationImportRuleVersionProof) bool {
	proof, exists := proofs[automationImportRuleVersionKey(run.rule.id, run.ruleVersion)]
	return exists && proof.enabled && proof.configJSON == run.configSnapshotJSON && proof.createdAt <= run.startedAt
}

func automationImportRuleVersionProofs(
	rules map[string]automationImportRule,
	events automationImportEventIndex,
) (map[string]automationImportRuleVersionProof, bool) {
	proofs := make(map[string]automationImportRuleVersionProof)
	for _, event := range events.byID {
		if event.aggregateType != "automation_rule" {
			continue
		}
		rule, exists := rules[event.aggregateID]
		if !exists || !validAutomationImportEventEnvelope(event) || event.actorID == nil ||
			*event.actorID != models.BuiltinOwnerActorID || event.requestID != nil || event.commandSeq != nil ||
			event.assignmentID != nil || event.submissionID != nil || event.artifactID != nil || event.agentRunID != nil ||
			event.previousJSON == nil || event.currentJSON == nil {
			return nil, false
		}
		createdAt, createdOK := automationImportTime(event.createdAt)
		if !createdOK || event.createdAt != formatInboxTimestamp(createdAt.UTC()) {
			return nil, false
		}
		previous, previousOK := automationImportJSONObject(*event.previousJSON)
		current, currentOK := automationImportJSONObject(*event.currentJSON)
		previousState, previousValid := automationImportRuleEventState(previous, rule.preset, false)
		currentState, currentValid := automationImportRuleEventState(current, rule.preset, true)
		if !previousOK || !currentOK || !previousValid || !currentValid ||
			previousState.presetKey != currentState.presetKey || currentState.version != previousState.version+1 ||
			currentState.version > rule.version || !validAutomationImportRuleTransition(event.action, previousState, currentState) ||
			!validAutomationImportRuleEventSchedule(rule.preset, currentState, event.createdAt) {
			return nil, false
		}
		key := automationImportRuleVersionKey(rule.id, currentState.version)
		if _, duplicate := proofs[key]; duplicate {
			return nil, false
		}
		proofs[key] = automationImportRuleVersionProof{
			enabled: currentState.enabled, configJSON: currentState.configJSON, createdAt: event.createdAt,
		}
	}
	return proofs, true
}

type automationImportRuleEventSnapshot struct {
	presetKey  string
	enabled    bool
	config     automationConfig
	configJSON string
	nextRunAt  *string
	version    int64
	reason     string
}

func automationImportRuleEventState(
	object map[string]any,
	preset automationPresetDefinition,
	withReason bool,
) (automationImportRuleEventSnapshot, bool) {
	wantFields := 5
	if withReason {
		wantFields = 6
	}
	if len(object) != wantFields {
		return automationImportRuleEventSnapshot{}, false
	}
	presetKey, presetOK := object["preset_key"].(string)
	enabled, enabledOK := object["enabled"].(bool)
	configJSON, configOK := object["config_json"].(string)
	nextValue, nextExists := object["next_run_at"]
	nextRunAt, nextOK := automationImportOptionalString(nextValue)
	version, versionOK := automationImportJSONInt64(object["version"])
	reason := ""
	if withReason {
		var reasonOK bool
		reason, reasonOK = object["reason"].(string)
		if !reasonOK {
			return automationImportRuleEventSnapshot{}, false
		}
	} else if _, exists := object["reason"]; exists {
		return automationImportRuleEventSnapshot{}, false
	}
	config, canonicalConfig, validConfig := automationImportConfig(configJSON, preset)
	if !presetOK || presetKey != preset.PresetKey || !enabledOK || !configOK || !nextExists || !nextOK ||
		!versionOK || version < 1 || !validConfig || !validAutomationImportRuleNextRun(preset, enabled, nextRunAt) {
		return automationImportRuleEventSnapshot{}, false
	}
	return automationImportRuleEventSnapshot{
		presetKey: presetKey, enabled: enabled, config: config, configJSON: canonicalConfig,
		nextRunAt: nextRunAt, version: version, reason: reason,
	}, true
}

func validAutomationImportRuleEventSchedule(
	preset automationPresetDefinition,
	current automationImportRuleEventSnapshot,
	createdAt string,
) bool {
	if !current.enabled || preset.TriggerType == "event" {
		return current.nextRunAt == nil
	}
	created, valid := automationImportTime(createdAt)
	if !valid || current.nextRunAt == nil {
		return false
	}
	next, err := nextAutomationSchedule(preset.PresetKey, current.config, created.UTC())
	return err == nil && *current.nextRunAt == formatInboxTimestamp(next.UTC())
}

func validAutomationImportRuleNextRun(preset automationPresetDefinition, enabled bool, nextRunAt *string) bool {
	if !enabled || preset.TriggerType == "event" {
		return nextRunAt == nil
	}
	if nextRunAt == nil {
		return false
	}
	parsed, valid := automationImportTime(*nextRunAt)
	return valid && *nextRunAt == formatInboxTimestamp(parsed.UTC())
}

func validAutomationImportRuleTransition(
	action string,
	previous, current automationImportRuleEventSnapshot,
) bool {
	sameConfig := previous.configJSON == current.configJSON
	sameNextRun := sameOptionalString(previous.nextRunAt, current.nextRunAt)
	switch action {
	case "automation_rule_enabled":
		return !previous.enabled && current.enabled && sameConfig && current.reason == "enabled by owner"
	case "automation_rule_disabled":
		return previous.enabled && !current.enabled && sameConfig &&
			(current.reason == "disabled by owner" || current.reason == "dependency unavailable")
	case "automation_rule_updated":
		return previous.enabled == current.enabled && (!sameConfig || !sameNextRun) && current.reason == "configuration changed"
	default:
		return false
	}
}

func automationImportRuleVersionKey(ruleID string, version int64) string {
	return automationImportEventKey(ruleID, strconv.FormatInt(version, 10))
}

func automationImportRules(table businessExportTable) (map[string]automationImportRule, bool) {
	result := make(map[string]automationImportRule, len(table.Rows))
	for _, values := range automationImportRows(table) {
		id, idOK := values["id"].(string)
		key, keyOK := values["preset_key"].(string)
		version, versionOK := automationImportInt64(values["version"])
		configJSON, configOK := values["config_json"].(string)
		preset, presetOK := automationPresetByKey(key)
		if !idOK || !keyOK || !versionOK || version < 1 || !configOK || !presetOK ||
			preset.ID != id || !validCanonicalAutomationUUID(id) {
			return nil, false
		}
		_, canonicalConfig, configValid := automationImportConfig(configJSON, preset)
		if !configValid {
			return nil, false
		}
		if _, duplicate := result[id]; duplicate {
			return nil, false
		}
		result[id] = automationImportRule{id: id, preset: preset, version: version, configJSON: canonicalConfig}
	}
	return result, len(result) == len(automationPresets)
}

func automationImportRuns(table businessExportTable, rules map[string]automationImportRule) (map[string]automationImportRun, bool) {
	result := make(map[string]automationImportRun, len(table.Rows))
	seenDedupe := make(map[string]struct{}, len(table.Rows))
	seenLogicalAttempt := make(map[string]struct{}, len(table.Rows))
	for _, values := range automationImportRows(table) {
		id, idOK := values["id"].(string)
		ruleID, ruleOK := values["rule_id"].(string)
		rule, exists := rules[ruleID]
		ruleVersion, versionOK := automationImportInt64(values["rule_version"])
		triggerType, triggerOK := values["trigger_type"].(string)
		sourceEventID, sourceOK := automationImportOptionalString(values["source_event_id"])
		scheduledFor, scheduleOK := automationImportOptionalString(values["scheduled_for"])
		logicalKey, logicalOK := values["logical_key"].(string)
		dedupeKey, dedupeOK := values["dedupe_key"].(string)
		status, statusOK := values["status"].(string)
		attemptValue, attemptOK := automationImportInt64(values["attempt"])
		retryOfRunID, retryOK := automationImportOptionalString(values["retry_of_run_id"])
		retryableValue, retryableOK := automationImportInt64(values["retryable"])
		retryAt, retryAtOK := automationImportOptionalString(values["retry_at"])
		causedByRunID, causeOK := automationImportOptionalString(values["caused_by_run_id"])
		causalDepth, depthOK := automationImportInt64(values["causal_depth"])
		configJSON, configOK := values["config_snapshot_json"].(string)
		actionJSON, actionOK := values["action_snapshot_json"].(string)
		errorCode, errorOK := automationImportOptionalString(values["error_code"])
		resultType, resultTypeOK := automationImportOptionalString(values["result_type"])
		resultID, resultIDOK := automationImportOptionalString(values["result_id"])
		resultSummary, summaryOK := values["result_summary"].(string)
		startedAt, startedOK := values["started_at"].(string)
		endedAt, endedOK := values["ended_at"].(string)

		if !idOK || !validCanonicalAutomationUUID(id) || !ruleOK || !exists || !rule.preset.Available ||
			!versionOK || ruleVersion < 2 || ruleVersion > rule.version || !triggerOK ||
			!sourceOK || !scheduleOK || !logicalOK || !dedupeOK || !statusOK || !attemptOK || attemptValue < 1 || attemptValue > automationMaxAttempts ||
			!retryOK || !retryableOK || (retryableValue != 0 && retryableValue != 1) || !retryAtOK ||
			!causeOK || causedByRunID != nil || !depthOK || causalDepth != 0 || !configOK || !actionOK ||
			!errorOK || !resultTypeOK || !resultIDOK || !summaryOK || !startedOK || !endedOK {
			return nil, false
		}
		started, startedValid := automationImportTime(startedAt)
		ended, endedValid := automationImportTime(endedAt)
		if !startedValid || !endedValid || ended.Before(started) || startedAt != endedAt ||
			startedAt != formatInboxTimestamp(started.UTC()) {
			return nil, false
		}
		if retryOfRunID != nil && !validCanonicalAutomationUUID(*retryOfRunID) {
			return nil, false
		}
		if !validAutomationImportTrigger(rule, triggerType, sourceEventID, scheduledFor, logicalKey) ||
			dedupeKey != logicalKey+":attempt:"+strconv.FormatInt(attemptValue, 10) {
			return nil, false
		}
		config, canonicalConfig, configValid := automationImportConfig(configJSON, rule.preset)
		if !configValid {
			return nil, false
		}
		action, canonicalAction, actionValid, actionContractValid := automationImportAction(actionJSON, rule.preset, config)
		attemptContractValid := triggerType == rule.preset.TriggerType && actionContractValid
		if !validAutomationImportRunOutcome(
			status, int(attemptValue), retryableValue == 1, retryAt, errorCode, resultType, resultID, resultSummary, started,
		) || !validAutomationImportSafetyFailure(
			rule.preset.PresetKey, status, retryableValue == 1, retryAt, errorCode,
			actionValid, attemptContractValid,
		) {
			return nil, false
		}
		if triggerType == "schedule" && status == "failed" && (errorCode == nil || *errorCode != "ACTION_WRITE_FAILED") {
			return nil, false
		}
		logicalAttempt := logicalKey + "\x00" + strconv.FormatInt(attemptValue, 10)
		if _, duplicate := result[id]; duplicate {
			return nil, false
		}
		if _, duplicate := seenDedupe[dedupeKey]; duplicate {
			return nil, false
		}
		if _, duplicate := seenLogicalAttempt[logicalAttempt]; duplicate {
			return nil, false
		}
		seenDedupe[dedupeKey] = struct{}{}
		seenLogicalAttempt[logicalAttempt] = struct{}{}
		result[id] = automationImportRun{
			id: id, rule: rule, ruleVersion: ruleVersion, triggerType: triggerType,
			sourceEventID: sourceEventID, scheduledFor: scheduledFor, logicalKey: logicalKey,
			dedupeKey: dedupeKey, status: status, attempt: int(attemptValue), retryOfRunID: retryOfRunID,
			retryable: retryableValue == 1, retryAt: retryAt,
			configSnapshotJSON: canonicalConfig, actionSnapshotJSON: canonicalAction, actionSnapshot: action,
			actionValid: actionValid, attemptContractOK: attemptContractValid,
			errorCode: errorCode, resultType: resultType, resultID: resultID, resultSummary: resultSummary,
			startedAt: startedAt,
		}
	}
	return result, true
}

func validAutomationImportTrigger(rule automationImportRule, triggerType string, sourceEventID, scheduledFor *string, logicalKey string) bool {
	if triggerType == "event" {
		return sourceEventID != nil && validCanonicalAutomationUUID(*sourceEventID) && scheduledFor == nil &&
			logicalKey == "event:"+rule.id+":"+*sourceEventID
	}
	if triggerType == "schedule" && sourceEventID == nil && scheduledFor != nil {
		parsed, valid := automationImportTime(*scheduledFor)
		return valid && *scheduledFor == formatInboxTimestamp(parsed.UTC()) &&
			logicalKey == "schedule:"+rule.id+":"+*scheduledFor
	}
	return false
}

func validAutomationImportRunOutcome(
	status string,
	attempt int,
	retryable bool,
	retryAt, errorCode, resultType, resultID *string,
	resultSummary string,
	startedAt time.Time,
) bool {
	if attempt == 1 {
		// Retry identity is checked with the whole chain below.
	} else if attempt < 2 {
		return false
	}
	switch status {
	case "succeeded":
		return !retryable && retryAt == nil && errorCode == nil && resultType != nil && resultID != nil &&
			validAutomationImportSuccessSummary(*resultType, resultSummary) &&
			validCanonicalAutomationUUID(*resultID)
	case "failed":
		if errorCode == nil || strings.TrimSpace(*errorCode) == "" || resultType != nil || resultID != nil ||
			resultSummary != "本地动作未提交；业务来源事实保持不变。" {
			return false
		}
		if *errorCode != "ACTION_WRITE_FAILED" {
			return !retryable && retryAt == nil && (*errorCode == "SOURCE_EVENT_CONFLICT" ||
				*errorCode == "ACTION_SNAPSHOT_INVALID" || *errorCode == "ATTEMPT_CONTRACT_INVALID" ||
				*errorCode == "SOURCE_EVENT_INVALID")
		}
		if attempt >= automationMaxAttempts {
			return !retryable && retryAt == nil
		}
		delay := time.Minute
		if attempt == 2 {
			delay = 5 * time.Minute
		}
		return retryable && retryAt != nil && *retryAt == formatInboxTimestamp(startedAt.Add(delay).UTC())
	case "skipped":
		return attempt == 1 && !retryable && retryAt == nil && errorCode != nil &&
			*errorCode == "SCHEDULE_WINDOW_EXPIRED" && resultType == nil && resultID == nil &&
			resultSummary == "离线期间错过的旧计划窗口已折叠，不创建过期提醒。"
	default:
		// No current preset emits cancelled runs, so accepting one would create a
		// history state that the runtime itself cannot produce.
		return false
	}
}

func validAutomationImportSuccessSummary(resultType, summary string) bool {
	switch resultType {
	case "inbox_item":
		return summary == "已创建本地核对事项。"
	case "task":
		return summary == "已创建本地发票逾期跟进任务。"
	case "reminder":
		return summary == "已创建本地提醒。"
	default:
		return false
	}
}

func validAutomationImportSafetyFailure(
	presetKey, status string,
	retryable bool,
	retryAt, errorCode *string,
	actionValid, attemptContractValid bool,
) bool {
	if status != "failed" || errorCode == nil || *errorCode == "ACTION_WRITE_FAILED" {
		return attemptContractValid
	}
	if retryable || retryAt != nil {
		return false
	}
	switch presetKey {
	case automationPresetInvoiceOverdue:
		switch *errorCode {
		case "ACTION_SNAPSHOT_INVALID":
			// The producer reports this either before the attempt contract is
			// inspected, or after a valid source transition fails the final
			// action/source binding. The latter predicate is checked with the
			// source event below.
			return !actionValid || attemptContractValid
		case "SOURCE_EVENT_INVALID":
			return attemptContractValid
		case "ATTEMPT_CONTRACT_INVALID":
			return actionValid && !attemptContractValid
		default:
			return false
		}
	case automationPresetProjectCompleted:
		return *errorCode == "SOURCE_EVENT_CONFLICT" && actionValid
	default:
		return false
	}
}

func validAutomationImportRunChains(runs map[string]automationImportRun) bool {
	groups := make(map[string][]automationImportRun)
	for _, run := range runs {
		groups[run.logicalKey] = append(groups[run.logicalKey], run)
	}
	for _, chain := range groups {
		sort.Slice(chain, func(left, right int) bool { return chain[left].attempt < chain[right].attempt })
		for index := range chain {
			run := chain[index]
			if run.attempt != index+1 {
				return false
			}
			if index == 0 {
				if run.retryOfRunID != nil {
					return false
				}
				continue
			}
			previous := chain[index-1]
			if run.retryOfRunID == nil || *run.retryOfRunID != previous.id || !previous.retryable || previous.status != "failed" ||
				run.rule.id != previous.rule.id || run.ruleVersion != previous.ruleVersion ||
				run.triggerType != previous.triggerType || !reflect.DeepEqual(run.sourceEventID, previous.sourceEventID) ||
				!reflect.DeepEqual(run.scheduledFor, previous.scheduledFor) ||
				run.configSnapshotJSON != previous.configSnapshotJSON || run.actionSnapshotJSON != previous.actionSnapshotJSON {
				return false
			}
		}
	}
	return true
}

func automationImportWorkflowEvents(table businessExportTable) (automationImportEventIndex, bool) {
	result := automationImportEventIndex{
		byID:              make(map[string]automationImportWorkflowEvent, len(table.Rows)),
		byAggregate:       make(map[string][]automationImportWorkflowEvent),
		byAggregateAction: make(map[string][]automationImportWorkflowEvent),
	}
	for _, values := range automationImportRows(table) {
		id, idOK := values["id"].(string)
		aggregateType, typeOK := values["aggregate_type"].(string)
		aggregateID, aggregateOK := values["aggregate_id"].(string)
		action, actionOK := values["action"].(string)
		actorID, actorOK := automationImportOptionalString(values["actor_id"])
		assignmentID, assignmentOK := automationImportOptionalString(values["assignment_id"])
		submissionID, submissionOK := automationImportOptionalString(values["submission_id"])
		artifactID, artifactOK := automationImportOptionalString(values["artifact_id"])
		agentRunID, agentOK := automationImportOptionalString(values["agent_run_id"])
		requestID, requestOK := automationImportOptionalString(values["request_id"])
		commandSeq, commandOK := automationImportOptionalInt64(values["command_seq"])
		previousJSON, previousOK := automationImportOptionalString(values["previous_json"])
		currentJSON, currentOK := automationImportOptionalString(values["current_json"])
		createdAt, createdOK := values["created_at"].(string)
		if !idOK || !typeOK || !aggregateOK || !actionOK || !actorOK ||
			!assignmentOK || !submissionOK || !artifactOK || !agentOK || !requestOK || !commandOK || !previousOK || !currentOK ||
			!createdOK {
			return automationImportEventIndex{}, false
		}
		if _, duplicate := result.byID[id]; duplicate {
			return automationImportEventIndex{}, false
		}
		event := automationImportWorkflowEvent{
			id: id, aggregateType: aggregateType, aggregateID: aggregateID, action: action,
			actorID: actorID, assignmentID: assignmentID, submissionID: submissionID,
			artifactID: artifactID, agentRunID: agentRunID, requestID: requestID, commandSeq: commandSeq,
			previousJSON: previousJSON, currentJSON: currentJSON, createdAt: createdAt,
		}
		result.byID[id] = event
		aggregateKey := automationImportEventKey(aggregateType, aggregateID)
		result.byAggregate[aggregateKey] = append(result.byAggregate[aggregateKey], event)
		actionKey := automationImportEventKey(aggregateType, aggregateID, action)
		result.byAggregateAction[actionKey] = append(result.byAggregateAction[actionKey], event)
	}
	return result, true
}

func validAutomationImportSourceEvent(run automationImportRun, events automationImportEventIndex) bool {
	if run.triggerType == "schedule" {
		if run.status == "failed" && run.errorCode != nil {
			if run.rule.preset.PresetKey == automationPresetInvoiceOverdue &&
				(*run.errorCode == "ACTION_SNAPSHOT_INVALID" || *run.errorCode == "ATTEMPT_CONTRACT_INVALID") {
				return true
			}
			if run.rule.preset.PresetKey == automationPresetProjectCompleted && *run.errorCode == "SOURCE_EVENT_CONFLICT" {
				return true
			}
		}
		return run.sourceEventID == nil
	}
	if run.status == "failed" && run.errorCode != nil {
		if run.rule.preset.PresetKey == automationPresetInvoiceOverdue {
			switch *run.errorCode {
			case "ACTION_SNAPSHOT_INVALID":
				if !run.actionValid {
					return true
				}
				if !run.attemptContractOK {
					return false
				}
				action, err := automationInvoiceOverdueActionFromSnapshot(run.actionSnapshot)
				if err != nil {
					return false
				}
				current, sourceValid := automationImportInvoiceSourceTransition(run, events, action)
				return sourceValid && !automationImportInvoiceActionMatchesSource(action, current)
			case "ATTEMPT_CONTRACT_INVALID":
				return run.actionValid && !run.attemptContractOK
			case "SOURCE_EVENT_INVALID":
				if !run.actionValid || !run.attemptContractOK {
					return false
				}
				action, err := automationInvoiceOverdueActionFromSnapshot(run.actionSnapshot)
				if err != nil {
					return false
				}
				_, sourceValid := automationImportInvoiceSourceTransition(run, events, action)
				return !sourceValid
			}
		}
		if run.rule.preset.PresetKey == automationPresetProjectCompleted && *run.errorCode == "SOURCE_EVENT_CONFLICT" {
			return true
		}
	}
	if run.sourceEventID == nil {
		return false
	}
	event, exists := events.byID[*run.sourceEventID]
	if !exists || event.currentJSON == nil || !validAutomationImportEventEnvelope(event) {
		return false
	}
	switch run.rule.preset.PresetKey {
	case automationPresetProjectCompleted:
		action, err := automationProjectCompletionActionFromSnapshot(run.actionSnapshot)
		if err != nil || event.aggregateType != "project" || event.aggregateID != action.ProjectID ||
			event.action != "project_completed" || event.actorID == nil || *event.actorID != models.BuiltinOwnerActorID ||
			event.commandSeq == nil || *event.commandSeq != 1 || event.previousJSON == nil ||
			event.assignmentID != nil || event.submissionID != nil || event.artifactID != nil || event.agentRunID != nil {
			return false
		}
		current, ok := automationImportJSONObject(*event.currentJSON)
		previous, previousOK := automationImportJSONObject(*event.previousJSON)
		if !ok || !previousOK || !validAutomationImportProjectCompletionTransition(previous, current, action) {
			return false
		}
		return true
	case automationPresetInvoiceOverdue:
		action, err := automationInvoiceOverdueActionFromSnapshot(run.actionSnapshot)
		if err != nil {
			return false
		}
		current, sourceValid := automationImportInvoiceSourceTransition(run, events, action)
		return sourceValid && automationImportInvoiceActionMatchesSource(action, current)
	default:
		return false
	}
}

func automationImportInvoiceActionMatchesSource(
	action automationInvoiceOverdueAction,
	current automationInvoiceEventSnapshot,
) bool {
	return current.InvoiceNumber == action.InvoiceNumber &&
		sameOptionalString(current.ProjectID, action.ProjectID) &&
		action.Title == automationInvoiceOverdueTaskTitle(current.InvoiceNumber) &&
		action.Description == automationInvoiceOverdueTaskDescription(
			current.InvoiceNumber, current.DueDate, current.AmountMinor, current.Currency,
		)
}

func automationImportInvoiceSourceTransition(
	run automationImportRun,
	events automationImportEventIndex,
	action automationInvoiceOverdueAction,
) (automationInvoiceEventSnapshot, bool) {
	if run.sourceEventID == nil {
		return automationInvoiceEventSnapshot{}, false
	}
	event, exists := events.byID[*run.sourceEventID]
	if !exists || event.currentJSON == nil || !validAutomationImportEventEnvelope(event) ||
		event.aggregateType != "invoice" || event.aggregateID != action.InvoiceID || event.action != "invoice_overdue" ||
		event.actorID == nil || *event.actorID != models.BuiltinSystemActorID ||
		event.commandSeq == nil || *event.commandSeq != 1 || event.previousJSON == nil ||
		event.assignmentID != nil || event.submissionID != nil || event.artifactID != nil || event.agentRunID != nil {
		return automationInvoiceEventSnapshot{}, false
	}
	previous, previousErr := decodeAutomationInvoiceEventSnapshot(*event.previousJSON)
	current, currentErr := decodeAutomationInvoiceEventSnapshot(*event.currentJSON)
	valid := previousErr == nil && currentErr == nil && validAutomationInvoiceEventSnapshot(previous) &&
		validAutomationInvoiceEventSnapshot(current) && (previous.Status == "sent" || previous.Status == "viewed") &&
		current.Status == "overdue" && current.Version == previous.Version+1 &&
		sameAutomationInvoiceTransitionFacts(previous, current)
	return current, valid
}

func validAutomationImportProjectCompletionTransition(
	previous, current map[string]any,
	action automationProjectCompletionAction,
) bool {
	if len(previous) != 11 || len(current) != 11 || current["id"] != action.ProjectID || current["name"] != action.ProjectName ||
		current["status"] != "completed" || current["archived_from_status"] != nil || previous["archived_from_status"] != nil ||
		(previous["status"] != "in_progress" && previous["status"] != "paused") {
		return false
	}
	for _, key := range []string{"id", "name", "description", "client_id", "start_date", "due_date", "amount_minor", "color"} {
		if !reflect.DeepEqual(previous[key], current[key]) {
			return false
		}
	}
	previousVersion, previousOK := automationImportJSONInt64(previous["version"])
	currentVersion, currentOK := automationImportJSONInt64(current["version"])
	return previousOK && currentOK && previousVersion >= 1 && currentVersion == previousVersion+1
}

func validAutomationImportRunEvent(run automationImportRun, events automationImportEventIndex) bool {
	wantAction := map[string]string{
		"succeeded": "automation_run_succeeded",
		"failed":    "automation_run_failed",
		"skipped":   "automation_run_skipped",
	}[run.status]
	count := 0
	for _, event := range automationImportAggregateEvents(events, "automation_run", run.id) {
		count++
		if !validAutomationImportEventEnvelope(event) || event.action != wantAction ||
			event.actorID == nil || *event.actorID != models.BuiltinSystemActorID ||
			event.requestID != nil || event.commandSeq != nil ||
			event.previousJSON != nil || event.currentJSON == nil || event.assignmentID != nil ||
			event.submissionID != nil || event.artifactID != nil || event.agentRunID != nil ||
			event.createdAt != run.startedAt {
			return false
		}
		current, ok := automationImportJSONObject(*event.currentJSON)
		if !ok || len(current) != 6 || current["rule_id"] != run.rule.id || current["status"] != run.status {
			return false
		}
		attempt, attemptOK := automationImportJSONInt64(current["attempt"])
		if !attemptOK || int(attempt) != run.attempt || !automationImportJSONOptionalStringEquals(current, "result_type", run.resultType) ||
			!automationImportJSONOptionalStringEquals(current, "result_id", run.resultID) ||
			!automationImportJSONOptionalStringEquals(current, "error_code", run.errorCode) {
			return false
		}
	}
	return count == 1
}

func validAutomationImportResult(
	run automationImportRun,
	events automationImportEventIndex,
	inboxItems, tasks, reminders map[string]map[string]any,
) bool {
	if run.status != "succeeded" {
		return true
	}
	if run.resultType == nil || run.resultID == nil || *run.resultType != run.rule.preset.ActionType {
		return false
	}
	switch *run.resultType {
	case "inbox_item":
		row, exists := inboxItems[*run.resultID]
		if !exists || row["source_entity_type"] != automationInboxSourceType || row["source_entity_id"] != run.id ||
			row["source_event_key"] != "automation:"+run.logicalKey {
			return false
		}
		payload, payloadOK := row["payload_json"].(string)
		if !payloadOK {
			return false
		}
		payloadObject, ok := automationImportJSONObject(payload)
		if !ok || len(payloadObject) != 5 || payloadObject["automation_rule_id"] != run.rule.id ||
			payloadObject["automation_run_id"] != run.id || payloadObject["preset_key"] != run.rule.preset.PresetKey {
			return false
		}
		action, err := automationProjectCompletionActionFromSnapshot(run.actionSnapshot)
		return err == nil && payloadObject["project_id"] == action.ProjectID && payloadObject["project_name"] == action.ProjectName &&
			validAutomationImportInboxCreationEvent(run, action, payloadObject, events)
	case "task":
		if _, exists := tasks[*run.resultID]; exists {
			// Task facts are user-editable after creation. The immutable creation
			// event below is the source of truth for the original action contract.
		}
		return validAutomationImportTaskCreationEvent(run, events)
	case "reminder":
		row, exists := reminders[*run.resultID]
		if !exists || row["source_entity_type"] != "automation" || row["source_entity_id"] != run.id ||
			row["source_event_key"] != "reminder:"+*run.resultID+":due" ||
			row["created_by_actor_id"] != models.BuiltinSystemActorID || row["series_id"] != *run.resultID {
			return false
		}
		occurrence, occurrenceOK := automationImportInt64(row["occurrence_number"])
		return occurrenceOK && occurrence == 1 && validAutomationImportReminderCreationEvent(run, events)
	default:
		return false
	}
}

func validAutomationImportInboxCreationEvent(
	run automationImportRun,
	action automationProjectCompletionAction,
	payload map[string]any,
	events automationImportEventIndex,
) bool {
	if run.resultID == nil {
		return false
	}
	count := 0
	for _, event := range automationImportAggregateActionEvents(events, "inbox_item", *run.resultID, "source_projected") {
		count++
		if !validAutomationImportEventEnvelope(event) || event.actorID == nil || *event.actorID != models.BuiltinSystemActorID ||
			event.commandSeq == nil || *event.commandSeq != 1 || event.requestID != nil || event.previousJSON != nil ||
			event.currentJSON == nil || event.assignmentID != nil || event.submissionID != nil || event.artifactID != nil ||
			event.agentRunID != nil || event.createdAt != run.startedAt {
			return false
		}
		current, ok := automationImportJSONObject(*event.currentJSON)
		if !ok || len(current) != 23 || current["kind"] != "event" || current["title"] != action.Title ||
			current["summary"] != automationProjectCompletionItemSummary || current["source_entity_type"] != automationInboxSourceType ||
			current["source_entity_id"] != run.id || current["source_event_key"] != "automation:"+run.logicalKey ||
			current["source_deleted_at"] != nil || current["priority"] != action.Priority || current["resolution_policy"] != "manual" ||
			current["status"] != "open" || current["due_at"] != nil || current["read_at"] != nil || current["triaged_at"] != nil ||
			current["snoozed_until"] != nil || current["resolved_by_actor_id"] != nil || current["resolved_at"] != nil ||
			current["resolution_reason"] != nil || current["resolution_mode"] != nil || current["dismissed_by_actor_id"] != nil ||
			current["dismissed_at"] != nil || current["dismiss_reason"] != nil || !reflect.DeepEqual(current["payload_json"], payload) {
			return false
		}
		version, versionOK := automationImportJSONInt64(current["version"])
		if !versionOK || version != 1 {
			return false
		}
	}
	return count == 1
}

func validAutomationImportReminderCreationEvent(run automationImportRun, events automationImportEventIndex) bool {
	if run.resultID == nil || run.scheduledFor == nil {
		return false
	}
	expectedAction := automationScheduleActionSnapshot(run.rule.preset.PresetKey)
	count := 0
	for _, event := range automationImportAggregateActionEvents(events, "reminder", *run.resultID, "reminder_created") {
		count++
		if !validAutomationImportEventEnvelope(event) || event.actorID == nil || *event.actorID != models.BuiltinSystemActorID ||
			event.commandSeq == nil || *event.commandSeq != 1 || event.requestID != nil ||
			event.previousJSON != nil || event.currentJSON == nil || event.assignmentID != nil || event.submissionID != nil ||
			event.artifactID != nil || event.agentRunID != nil || event.createdAt != run.startedAt {
			return false
		}
		current, ok := automationImportJSONObject(*event.currentJSON)
		if !ok || len(current) != 21 || current["source_entity_type"] != "automation" || current["source_entity_id"] != run.id ||
			current["title"] != expectedAction["title"] || current["summary"] != expectedAction["summary"] ||
			current["priority"] != expectedAction["priority"] || current["status"] != "scheduled" ||
			current["source_event_key"] != "reminder:"+*run.resultID+":due" ||
			current["created_by_actor_id"] != models.BuiltinSystemActorID || current["series_id"] != *run.resultID ||
			current["recurrence_type"] != "none" || current["recurrence_timezone"] != "UTC" ||
			current["fired_at"] != nil || current["inbox_item_id"] != nil || current["cancelled_by_actor_id"] != nil ||
			current["cancelled_at"] != nil || current["cancel_reason"] != nil {
			return false
		}
		wantTriggerAt := event.createdAt
		if run.attempt == 1 {
			wantTriggerAt = *run.scheduledFor
		}
		if current["trigger_at"] != wantTriggerAt {
			return false
		}
		interval, intervalOK := automationImportJSONInt64(current["recurrence_interval"])
		occurrence, occurrenceOK := automationImportJSONInt64(current["occurrence_number"])
		anchor, anchorOK := automationImportJSONInt64(current["recurrence_anchor_day"])
		version, versionOK := automationImportJSONInt64(current["version"])
		if !intervalOK || interval != 1 || !occurrenceOK || occurrence != 1 || !anchorOK || anchor != 1 || !versionOK || version != 1 {
			return false
		}
	}
	return count == 1
}

func validAutomationImportTaskCreationEvent(run automationImportRun, events automationImportEventIndex) bool {
	action, err := automationInvoiceOverdueActionFromSnapshot(run.actionSnapshot)
	if err != nil || run.resultID == nil || run.sourceEventID == nil {
		return false
	}
	count := 0
	for _, event := range automationImportAggregateActionEvents(events, "task", *run.resultID, "task_created_from_automation") {
		count++
		if !validAutomationImportEventEnvelope(event) || event.actorID == nil || *event.actorID != models.BuiltinSystemActorID ||
			event.commandSeq == nil || *event.commandSeq != 1 || event.requestID != nil ||
			event.previousJSON != nil || event.currentJSON == nil || event.assignmentID != nil || event.submissionID != nil ||
			event.artifactID != nil || event.agentRunID != nil || event.createdAt != run.startedAt {
			return false
		}
		current, ok := automationImportJSONObject(*event.currentJSON)
		if !ok || len(current) != 16 || current["id"] != *run.resultID || current["title"] != action.Title ||
			current["description"] != action.Description || current["kind"] != "followup" || current["status"] != "todo" ||
			current["review_policy"] != "none" || current["priority"] != action.Priority ||
			!reflect.DeepEqual(current["project_id"], automationImportOptionalStringValue(action.ProjectID)) ||
			current["due_date"] != nil || current["planned_date"] != nil ||
			current["automation_rule_id"] != run.rule.id || current["automation_run_id"] != run.id ||
			current["automation_preset_key"] != run.rule.preset.PresetKey || current["source_event_id"] != *run.sourceEventID ||
			current["invoice_id"] != action.InvoiceID {
			return false
		}
		version, versionOK := automationImportJSONInt64(current["version"])
		if !versionOK || version != 1 {
			return false
		}
	}
	return count == 1
}

func validAutomationImportReverseRelations(
	runs map[string]automationImportRun,
	events automationImportEventIndex,
	inboxItems, reminders map[string]map[string]any,
) bool {
	conflictKeys := make(map[string]struct{})
	for _, run := range runs {
		if run.status == "failed" && run.errorCode != nil && *run.errorCode == "SOURCE_EVENT_CONFLICT" &&
			run.rule.preset.PresetKey == automationPresetProjectCompleted {
			conflictKeys["automation:"+run.logicalKey] = struct{}{}
		}
	}
	for _, row := range inboxItems {
		if sourceKey, ok := row["source_event_key"].(string); ok {
			if _, conflict := conflictKeys[sourceKey]; conflict {
				continue
			}
		}
		if row["source_entity_type"] != "automation" {
			continue
		}
		runID, ok := row["source_entity_id"].(string)
		run, exists := runs[runID]
		if !ok || !exists || run.status != "succeeded" || run.resultType == nil || *run.resultType != "inbox_item" ||
			run.resultID == nil || row["id"] != *run.resultID {
			return false
		}
	}
	reminderSeriesRuns := make(map[string]automationImportRun)
	for _, run := range runs {
		if run.status != "succeeded" || run.resultType == nil || *run.resultType != "reminder" || run.resultID == nil {
			continue
		}
		if _, duplicate := reminderSeriesRuns[*run.resultID]; duplicate {
			return false
		}
		reminderSeriesRuns[*run.resultID] = run
	}
	for _, row := range reminders {
		seriesID, seriesOK := row["series_id"].(string)
		run, seriesIsAutomation := reminderSeriesRuns[seriesID]
		sourceIsAutomation := row["source_entity_type"] == "automation"
		if !seriesIsAutomation && !sourceIsAutomation {
			continue
		}
		runID, runIDOK := row["source_entity_id"].(string)
		if !seriesOK || !seriesIsAutomation || !sourceIsAutomation || !runIDOK || runID != run.id || run.resultID == nil {
			return false
		}
		if row["id"] == *run.resultID {
			continue
		}
		if !validAutomationImportReminderDescendant(run, row, events) {
			return false
		}
	}
	for _, event := range events.byID {
		if event.aggregateType == "automation_run" {
			if _, exists := runs[event.aggregateID]; !exists {
				return false
			}
		}
		if event.action == "task_created_from_automation" {
			if event.aggregateType != "task" {
				return false
			}
			if event.currentJSON == nil {
				return false
			}
			current, ok := automationImportJSONObject(*event.currentJSON)
			runID, runOK := current["automation_run_id"].(string)
			run, exists := runs[runID]
			if !ok || !runOK || !exists || run.status != "succeeded" || run.resultType == nil || *run.resultType != "task" ||
				run.resultID == nil || *run.resultID != event.aggregateID {
				return false
			}
		}
	}
	return true
}

func validAutomationImportReminderDescendant(
	run automationImportRun,
	row map[string]any,
	events automationImportEventIndex,
) bool {
	if run.resultID == nil || row["series_id"] != *run.resultID {
		return false
	}
	occurrence, occurrenceOK := automationImportInt64(row["occurrence_number"])
	if !occurrenceOK || occurrence <= 1 {
		return false
	}
	rowID, idOK := row["id"].(string)
	if !idOK {
		return false
	}
	count := 0
	for _, event := range automationImportAggregateActionEvents(events, "reminder", rowID, "reminder_recurrence_scheduled") {
		count++
		if !validAutomationImportEventEnvelope(event) || event.actorID == nil || *event.actorID != models.BuiltinSystemActorID ||
			event.commandSeq == nil || *event.commandSeq != 1 || event.requestID != nil || event.previousJSON != nil ||
			event.currentJSON == nil || event.assignmentID != nil || event.submissionID != nil || event.artifactID != nil || event.agentRunID != nil {
			return false
		}
		current, ok := automationImportJSONObject(*event.currentJSON)
		if !ok || len(current) != 21 || current["source_entity_type"] != "automation" || current["source_entity_id"] != run.id ||
			current["series_id"] != *run.resultID || current["status"] != "scheduled" ||
			current["source_event_key"] != "reminder:"+rowID+":due" || row["source_event_key"] != current["source_event_key"] ||
			current["created_by_actor_id"] != models.BuiltinSystemActorID || current["fired_at"] != nil ||
			current["inbox_item_id"] != nil || current["cancelled_by_actor_id"] != nil || current["cancelled_at"] != nil ||
			current["cancel_reason"] != nil || row["created_at"] != event.createdAt {
			return false
		}
		currentOccurrence, ok := automationImportJSONInt64(current["occurrence_number"])
		interval, intervalOK := automationImportJSONInt64(current["recurrence_interval"])
		anchor, anchorOK := automationImportJSONInt64(current["recurrence_anchor_day"])
		version, versionOK := automationImportJSONInt64(current["version"])
		recurrenceType, typeOK := current["recurrence_type"].(string)
		timezone, timezoneOK := current["recurrence_timezone"].(string)
		triggerAt, triggerOK := current["trigger_at"].(string)
		if !ok || currentOccurrence != occurrence || !intervalOK || !anchorOK || !versionOK || version != 1 ||
			!typeOK || recurrenceType == "none" || !timezoneOK || !triggerOK ||
			validateReminderRecurrence(recurrenceType, int(interval), timezone, int(anchor)) != nil {
			return false
		}
		if parsed, valid := automationImportTime(triggerAt); !valid || triggerAt != formatInboxTimestamp(parsed.UTC()) {
			return false
		}
	}
	return count == 1
}

func validAutomationImportEventEnvelope(event automationImportWorkflowEvent) bool {
	if !validCanonicalAutomationUUID(event.id) {
		return false
	}
	_, valid := automationImportTime(event.createdAt)
	return valid
}

func automationImportAggregateEvents(events automationImportEventIndex, aggregateType, aggregateID string) []automationImportWorkflowEvent {
	return events.byAggregate[automationImportEventKey(aggregateType, aggregateID)]
}

func automationImportAggregateActionEvents(events automationImportEventIndex, aggregateType, aggregateID, action string) []automationImportWorkflowEvent {
	return events.byAggregateAction[automationImportEventKey(aggregateType, aggregateID, action)]
}

func automationImportEventKey(parts ...string) string {
	return strings.Join(parts, "\x00")
}

func automationImportConfig(raw string, preset automationPresetDefinition) (automationConfig, string, bool) {
	object, ok := automationImportJSONObject(raw)
	if !ok {
		return automationConfig{}, "", false
	}
	if preset.TriggerType == "event" {
		if len(object) != 1 {
			return automationConfig{}, "", false
		}
		if _, ok := object["priority"].(string); !ok {
			return automationConfig{}, "", false
		}
	} else {
		if len(object) != 2 {
			return automationConfig{}, "", false
		}
		if _, ok := object["local_time"].(string); !ok {
			return automationConfig{}, "", false
		}
		if _, ok := object["timezone"].(string); !ok {
			return automationConfig{}, "", false
		}
	}
	config, err := decodeAutomationConfig(preset.PresetKey, raw)
	if err != nil {
		return automationConfig{}, "", false
	}
	canonical, err := encodeAutomationConfig(config)
	return config, canonical, err == nil
}

func automationImportAction(raw string, preset automationPresetDefinition, config automationConfig) (map[string]any, string, bool, bool) {
	object, ok := automationImportJSONObject(raw)
	if !ok {
		return nil, "", false, false
	}
	canonical, err := json.Marshal(object)
	if err != nil {
		return nil, "", false, false
	}
	switch preset.PresetKey {
	case automationPresetProjectCompleted:
		action, err := automationProjectCompletionActionFromSnapshot(object)
		if err != nil {
			return object, string(canonical), false, false
		}
		return object, string(canonical), true, action.Priority == config.Priority
	case automationPresetInvoiceOverdue:
		action, err := automationInvoiceOverdueActionFromSnapshot(object)
		if err != nil {
			return object, string(canonical), false, false
		}
		return object, string(canonical), true, action.Priority == config.Priority
	case automationPresetDailyToday, automationPresetWeeklyReview:
		expected := automationScheduleActionSnapshot(preset.PresetKey)
		if !reflect.DeepEqual(object, expected) {
			return object, string(canonical), false, false
		}
		return object, string(canonical), true, true
	default:
		return object, string(canonical), false, false
	}
}

func automationImportRows(table businessExportTable) []map[string]any {
	result := make([]map[string]any, 0, len(table.Rows))
	for _, row := range table.Rows {
		values := make(map[string]any, len(table.Columns))
		for index, column := range table.Columns {
			if index < len(row) {
				values[column] = row[index]
			}
		}
		result = append(result, values)
	}
	return result
}

func automationImportRowsByID(table businessExportTable) (map[string]map[string]any, bool) {
	result := make(map[string]map[string]any, len(table.Rows))
	for _, row := range automationImportRows(table) {
		id, ok := row["id"].(string)
		if !ok || id == "" {
			return nil, false
		}
		if _, duplicate := result[id]; duplicate {
			return nil, false
		}
		result[id] = row
	}
	return result, true
}

func automationImportJSONObject(raw string) (map[string]any, bool) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var object map[string]any
	if err := decoder.Decode(&object); err != nil || object == nil {
		return nil, false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, false
	}
	return object, true
}

func automationImportOptionalString(value any) (*string, bool) {
	if value == nil {
		return nil, true
	}
	text, ok := value.(string)
	return &text, ok
}

func automationImportOptionalInt64(value any) (*int64, bool) {
	if value == nil {
		return nil, true
	}
	parsed, ok := automationImportInt64(value)
	return &parsed, ok
}

func automationImportTime(value string) (time.Time, bool) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	return parsed, err == nil
}

func automationImportInt64(value any) (int64, bool) {
	return importInteger(value)
}

func automationImportJSONInt64(value any) (int64, bool) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	parsed, err := number.Int64()
	return parsed, err == nil
}

func automationImportJSONOptionalStringEquals(object map[string]any, key string, expected *string) bool {
	value, exists := object[key]
	if !exists {
		return false
	}
	if expected == nil {
		return value == nil
	}
	text, ok := value.(string)
	return ok && text == *expected
}

func automationImportOptionalStringValue(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}
