package api

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/agentexec"
	"github.com/opc-workspace/opc-sidecar/internal/agentrunner"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const (
	agentRunFailedInboxSourceType          = "agent_run_failed"
	automationAgentRunFailureItemTitle     = "Agent 执行失败待排查"
	automationAgentRunFailureItemSummary   = "一次 Agent 执行已失败。请查看对应执行记录并人工判断下一步；处理此事项不会重试执行、修改任务或启动模型。"
	automationAgentRunFailureResultSummary = "已创建本地 Agent 失败诊断事项。"
)

var errAutomationSourceUnavailable = errors.New("automation source is unavailable")

type automationAgentRunFailureEvidence struct {
	AgentRunID string `json:"agent_run_id"`
	TaskID     string `json:"task_id"`
	Attempt    int    `json:"attempt"`
	ErrorCode  string `json:"error_code"`
	FailedAt   string `json:"failed_at"`
}

type automationAgentRunFailureAction struct {
	automationAgentRunFailureEvidence
	ActionType string `json:"action_type"`
	Priority   string `json:"priority"`
}

type automationAgentRunFailurePayload struct {
	automationAgentRunFailureEvidence
	AutomationRuleID string `json:"automation_rule_id"`
	AutomationRunID  string `json:"automation_run_id"`
	SourceEventID    string `json:"source_event_id"`
}

func agentRunFailedEventKey(runID string) string { return "agent-run:" + runID + ":failed" }

// Only stable producer codes may leave the Run ledger as notification metadata.
func safeAutomationAgentRunErrorCode(value string) string {
	switch value {
	case agentrunner.CodeExecutorUnavailable, agentrunner.CodeExecutorFailed,
		agentrunner.CodeProtocolInvalid, agentrunner.CodeResultTooLarge, agentrunner.CodeTimedOut,
		agentexec.ErrorCodeInvalidInput, agentexec.ErrorCodeModelEndpoint,
		agentexec.ErrorCodeModelUnavailable, agentexec.ErrorCodeModelFailed,
		agentexec.ErrorCodeModelTruncated, agentexec.ErrorCodeModelFiltered, agentexec.ErrorCodeModelResponseInvalid,
		agentexec.ErrorCodeEmptyResult, agentexec.ErrorCodeInvalidResult,
		agentRunIdentityChanged, "AGENT_MODEL_KEY_UNAVAILABLE", "AGENT_RUN_FAILED":
		return value
	default:
		return "AGENT_RUN_FAILED"
	}
}

func validAutomationAgentRunFailureEvidence(e automationAgentRunFailureEvidence) bool {
	at, err := time.Parse(time.RFC3339Nano, e.FailedAt)
	return validCanonicalAutomationUUID(e.AgentRunID) && validCanonicalAutomationUUID(e.TaskID) &&
		e.Attempt >= 1 && float64(e.Attempt) <= 9007199254740991 && e.ErrorCode == safeAutomationAgentRunErrorCode(e.ErrorCode) &&
		err == nil && at.Year() >= 1 && at.Year() <= 9999 && e.FailedAt == formatInboxTimestamp(at)
}

// Validate exact keys before struct decoding: encoding/json otherwise accepts
// duplicate keys and case-insensitive aliases, which cannot prove provenance.
func decodeAutomationAgentFailureObject(raw string, keys []string, target any) error {
	if len(raw) > 16384 {
		return errAutomationActionSnapshotInvalid
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return errAutomationActionSnapshotInvalid
	}
	allowed := map[string]bool{}
	for _, key := range keys {
		allowed[key] = true
	}
	seen := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return errAutomationActionSnapshotInvalid
		}
		key, ok := token.(string)
		if !ok || !allowed[key] || seen[key] {
			return errAutomationActionSnapshotInvalid
		}
		seen[key] = true
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil || string(value) == "null" {
			return errAutomationActionSnapshotInvalid
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') || len(seen) != len(keys) {
		return errAutomationActionSnapshotInvalid
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errAutomationActionSnapshotInvalid
	}
	if err := json.Unmarshal([]byte(raw), target); err != nil {
		return errAutomationActionSnapshotInvalid
	}
	return nil
}

var automationAgentRunFailureEvidenceKeys = []string{"agent_run_id", "task_id", "attempt", "error_code", "failed_at"}

func automationAgentRunFailureEventFromJSON(raw string) (automationAgentRunFailureEvidence, error) {
	var evidence automationAgentRunFailureEvidence
	if err := decodeAutomationAgentFailureObject(raw, automationAgentRunFailureEvidenceKeys, &evidence); err != nil || !validAutomationAgentRunFailureEvidence(evidence) {
		return evidence, errAutomationSourceEventInvalid
	}
	return evidence, nil
}

func automationAgentRunFailureActionFromSnapshot(snapshot map[string]any) (automationAgentRunFailureAction, error) {
	var action automationAgentRunFailureAction
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return action, errAutomationActionSnapshotInvalid
	}
	return automationAgentRunFailureActionFromJSON(string(raw))
}

func automationAgentRunFailureActionFromJSON(raw string) (automationAgentRunFailureAction, error) {
	var action automationAgentRunFailureAction
	keys := append(append([]string{}, automationAgentRunFailureEvidenceKeys...), "action_type", "priority")
	if err := decodeAutomationAgentFailureObject(raw, keys, &action); err != nil || !validAutomationAgentRunFailureEvidence(action.automationAgentRunFailureEvidence) || action.ActionType != "inbox_item" {
		return action, errAutomationActionSnapshotInvalid
	}
	if _, ok := validPriorities[action.Priority]; !ok {
		return action, errAutomationActionSnapshotInvalid
	}
	return action, nil
}

func automationAgentRunFailurePayloadFromJSON(raw string) (automationAgentRunFailurePayload, error) {
	var payload automationAgentRunFailurePayload
	keys := append(append([]string{}, automationAgentRunFailureEvidenceKeys...), "automation_rule_id", "automation_run_id", "source_event_id")
	if err := decodeAutomationAgentFailureObject(raw, keys, &payload); err != nil || !validAutomationAgentRunFailureEvidence(payload.automationAgentRunFailureEvidence) || payload.AutomationRuleID != "00000000-0000-5000-8000-000000000105" || !validCanonicalAutomationUUID(payload.AutomationRunID) || !validCanonicalAutomationUUID(payload.SourceEventID) {
		return payload, errAutomationActionSnapshotInvalid
	}
	return payload, nil
}

func automationAgentRunFailureEvidenceFromEvent(event models.WorkflowEvent) (automationAgentRunFailureEvidence, error) {
	var evidence automationAgentRunFailureEvidence
	if event.CurrentJSON == nil {
		return evidence, errAutomationSourceEventInvalid
	}
	evidence, err := automationAgentRunFailureEventFromJSON(*event.CurrentJSON)
	at, timeErr := time.Parse(time.RFC3339Nano, event.CreatedAt)
	if err != nil || timeErr != nil || !validCanonicalAutomationUUID(event.ID) || event.AggregateType != "agent_run" || event.AggregateID != evidence.AgentRunID || event.Action != "agent_run_failed" || event.ActorID == nil || *event.ActorID != models.BuiltinSystemActorID || event.AgentRunID == nil || *event.AgentRunID != evidence.AgentRunID || event.AssignmentID != nil || event.SubmissionID != nil || event.ArtifactID != nil || event.CommandSeq != nil || event.RequestID != nil || event.PreviousJSON != nil || formatInboxTimestamp(at) != evidence.FailedAt {
		return evidence, errAutomationSourceEventInvalid
	}
	return evidence, nil
}

func automationAgentRunFailureEvidenceFromRun(run models.AgentRun) (automationAgentRunFailureEvidence, error) {
	var evidence automationAgentRunFailureEvidence
	if run.Status != "failed" || run.OutputDeliveryStatus != agentRunOutputNotReady || run.ErrorCode == nil || run.CompletedAt == nil {
		return evidence, errAutomationSourceUnavailable
	}
	at, err := time.Parse(time.RFC3339Nano, *run.CompletedAt)
	if err != nil {
		return evidence, errAutomationSourceUnavailable
	}
	evidence = automationAgentRunFailureEvidence{AgentRunID: run.ID, TaskID: run.TaskID, Attempt: run.Attempt, ErrorCode: safeAutomationAgentRunErrorCode(*run.ErrorCode), FailedAt: formatInboxTimestamp(at)}
	if !validAutomationAgentRunFailureEvidence(evidence) {
		return evidence, errAutomationSourceUnavailable
	}
	return evidence, nil
}

func enqueueAgentRunFailureAutomationDelivery(tx *gorm.DB, eventID string, evidence automationAgentRunFailureEvidence, nowText string) error {
	return enqueueAutomationEventDelivery(tx, automationPresetAgentRunFailed, eventID, nowText, func(priority string) map[string]any {
		return map[string]any{"action_type": "inbox_item", "agent_run_id": evidence.AgentRunID, "task_id": evidence.TaskID, "attempt": evidence.Attempt, "error_code": evidence.ErrorCode, "failed_at": evidence.FailedAt, "priority": priority}
	})
}

func createAutomationAgentRunFailureInboxItem(tx *gorm.DB, runID string, input automationAttemptInput, nowText string) (automationActionResult, error) {
	action, err := automationAgentRunFailureActionFromSnapshot(input.ActionSnapshot)
	if input.ActionSnapshotJSON != "" {
		action, err = automationAgentRunFailureActionFromJSON(input.ActionSnapshotJSON)
	}
	if err != nil {
		return automationActionResult{}, err
	}
	preset, ok := automationPresetByKey(input.Rule.PresetKey)
	if !ok || !preset.Available || input.Rule.PresetKey != automationPresetAgentRunFailed || input.Rule.ID != preset.ID || !input.Rule.Enabled || input.Rule.Version < 1 || input.TriggerType != "event" || input.SourceEventID == nil || !validCanonicalAutomationUUID(*input.SourceEventID) || input.ScheduledFor != nil || input.LogicalKey != "event:"+input.Rule.ID+":"+*input.SourceEventID || input.Attempt < 1 || input.Attempt > automationMaxAttempts || input.Config.Priority != action.Priority || input.Config.LocalTime != "" || input.Config.Timezone != "" || (input.Attempt == 1 && input.RetryOfRunID != nil) || (input.Attempt > 1 && (input.RetryOfRunID == nil || !validCanonicalAutomationUUID(*input.RetryOfRunID))) {
		return automationActionResult{}, errAutomationAttemptContractInvalid
	}
	var event models.WorkflowEvent
	if err := tx.First(&event, "id=?", *input.SourceEventID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return automationActionResult{}, errAutomationSourceEventInvalid
		}
		return automationActionResult{}, err
	}
	evidence, err := automationAgentRunFailureEvidenceFromEvent(event)
	if err != nil || evidence != action.automationAgentRunFailureEvidence {
		return automationActionResult{}, errAutomationSourceEventInvalid
	}
	var original models.AgentRun
	if err := tx.Select("id", "task_id", "attempt", "status", "error_code", "completed_at", "output_delivery_status").First(&original, "id=?", action.AgentRunID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return automationActionResult{}, errAutomationSourceUnavailable
		}
		return automationActionResult{}, err
	}
	liveEvidence, err := automationAgentRunFailureEvidenceFromRun(original)
	if err != nil || liveEvidence != evidence {
		return automationActionResult{}, errAutomationSourceUnavailable
	}
	var task models.Task
	if err := tx.Select("id").First(&task, "id=?", action.TaskID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return automationActionResult{}, errAutomationSourceUnavailable
		}
		return automationActionResult{}, err
	}
	key := agentRunFailedEventKey(action.AgentRunID)
	var existing models.InboxItem
	err = tx.Select("id").Where("source_event_key=?", key).Take(&existing).Error
	if err == nil {
		return automationActionResult{}, errAutomationInboxSourceConflict
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return automationActionResult{}, err
	}
	payload := automationAgentRunFailurePayload{automationAgentRunFailureEvidence: evidence, AutomationRuleID: input.Rule.ID, AutomationRunID: runID, SourceEventID: *input.SourceEventID}
	raw, err := json.Marshal(payload)
	if err != nil {
		return automationActionResult{}, err
	}
	sourceID := evidence.AgentRunID
	item := models.InboxItem{ID: uuid.NewString(), Kind: "event", Title: automationAgentRunFailureItemTitle, Summary: automationAgentRunFailureItemSummary, SourceEntityType: agentRunFailedInboxSourceType, SourceEntityID: &sourceID, SourceEventKey: &key, Priority: action.Priority, Status: "open", ResolutionPolicy: "manual", PayloadJSON: string(raw), Version: 1, CreatedAt: nowText, UpdatedAt: nowText}
	if err := tx.Create(&item).Error; err != nil {
		return automationActionResult{}, err
	}
	if err := recordInboxWorkflowEventAs(tx, item.ID, "source_projected", models.BuiltinSystemActorID, nil, inboxItemEventState(item, ""), "", nowText); err != nil {
		return automationActionResult{}, err
	}
	return automationActionResult{Type: "inbox_item", ID: item.ID, Summary: automationAgentRunFailureResultSummary}, nil
}
