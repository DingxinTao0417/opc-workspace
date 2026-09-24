package api

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const aiAgentFailureSourceGuide = `agent_run_failed 是人工启用预设后产生的执行失败诊断，来源另需outputs。subject是原failed AgentRun（version=null，attempt不是版本），related为当前Task及创建通知成功的AutomationRun，两种成功不可混淆。缺失Run（含便携导入未携带Run）为unavailable，不重新创建或换目标。可另读agents指南/执行详情；Agent重试须本条agent_execution及独立人工确认，文件另须agent_files。automation.retry只重试创建通知，不重跑Agent；解决诊断不修复Run或完成Task。pending只恢复登记，不是失败通知。`

// The column and keys are exclusively code-owned. Rebuild only enumerated
// metadata, never load a raw event, Inbox payload or business action snapshot.
func aiAgentFailureMetadataColumns(column string, fields ...string) string {
	pairs := make([]string, 0, len(fields))
	types := make([]string, 0, len(fields))
	for _, field := range fields {
		pairs = append(pairs, "'"+field+"',json_extract("+column+",'$."+field+"')")
		kind := "text"
		if field == "attempt" {
			kind = "integer"
		}
		types = append(types, "json_type("+column+",'$."+field+"')='"+kind+"'")
	}
	// json_extract converts true to 1. Preserve the original JSON types before
	// reconstructing metadata so a boolean cannot impersonate an integer.
	return "json_object(" + strings.Join(pairs, ",") + ") AS metadata_json,(SELECT COUNT(*) FROM json_each(" + column + ")) AS field_count,COALESCE((" + strings.Join(types, " AND ") + "),0) AS field_types_valid"
}

type aiAgentFailureMetadata struct {
	MetadataJSON    string
	FieldCount      int
	FieldTypesValid bool
}

func aiAgentFailureSameTime(left, right string) bool {
	a, e1 := time.Parse(time.RFC3339Nano, left)
	b, e2 := time.Parse(time.RFC3339Nano, right)
	return e1 == nil && e2 == nil && a.Equal(b)
}

func completeAIInboxAgentFailureSource(tx *gorm.DB, ref aiInboxSourceReference, result *aiInboxSourceResult) error {
	if ref.Kind != "event" || ref.SourceEntityID == nil || ref.SourceEventKey == nil {
		return errAIInboxSourceIdentity
	}
	var metadata aiAgentFailureMetadata
	if err := tx.Table("inbox_items").Select(aiAgentFailureMetadataColumns("payload_json", "agent_run_id", "task_id", "attempt", "error_code", "failed_at", "automation_rule_id", "automation_run_id", "source_event_id")).Where("id=?", ref.ID).Take(&metadata).Error; err != nil {
		return err
	}
	payload, err := automationAgentRunFailurePayloadFromJSON(metadata.MetadataJSON)
	if err != nil || metadata.FieldCount != 8 || !metadata.FieldTypesValid || payload.AgentRunID != *ref.SourceEntityID || *ref.SourceEventKey != agentRunFailedEventKey(payload.AgentRunID) {
		return errAIInboxSourceIdentity
	}
	var live struct {
		ID, TaskID, Status, OutputDeliveryStatus string
		Attempt                                  int
		ErrorCode, CompletedAt                   *string
	}
	if err := tx.Table("agent_runs").Select("id,task_id,status,output_delivery_status,attempt,error_code,completed_at").Where("id=?", payload.AgentRunID).Take(&live).Error; err != nil {
		return err
	}
	if live.TaskID != payload.TaskID || live.Status != "failed" || live.OutputDeliveryStatus != agentRunOutputNotReady || live.Attempt != payload.Attempt || live.ErrorCode == nil || safeAutomationAgentRunErrorCode(*live.ErrorCode) != payload.ErrorCode || live.CompletedAt == nil || !aiAgentFailureSameTime(*live.CompletedAt, payload.FailedAt) {
		return errAIInboxSourceIdentity
	}
	var event struct {
		FieldTypesValid                               bool
		MetadataJSON                                  string
		FieldCount                                    int
		AggregateType, AggregateID, Action, CreatedAt string
		AgentRunID, ActorID                           *string
		EnvelopeValid                                 bool
	}
	if err := tx.Table("workflow_events").Select("aggregate_type,aggregate_id,action,actor_id,agent_run_id,created_at,(previous_json IS NULL AND request_id IS NULL AND command_seq IS NULL AND assignment_id IS NULL AND submission_id IS NULL AND artifact_id IS NULL) AS envelope_valid,"+aiAgentFailureMetadataColumns("current_json", "agent_run_id", "task_id", "attempt", "error_code", "failed_at")).Where("id=?", payload.SourceEventID).Take(&event).Error; err != nil {
		return err
	}
	evidence, err := automationAgentRunFailureEventFromJSON(event.MetadataJSON)
	if err != nil || event.FieldCount != 5 || !event.FieldTypesValid || evidence != payload.automationAgentRunFailureEvidence || event.AggregateType != "agent_run" || event.AggregateID != live.ID || event.Action != "agent_run_failed" || !event.EnvelopeValid || event.ActorID == nil || *event.ActorID != models.BuiltinSystemActorID || event.AgentRunID == nil || *event.AgentRunID != live.ID || !aiAgentFailureSameTime(event.CreatedAt, payload.FailedAt) {
		return errAIInboxSourceIdentity
	}
	var automation struct {
		FieldTypesValid                             bool
		MetadataJSON                                string
		FieldCount                                  int
		ID, RuleID, Status, TriggerType, LogicalKey string
		RuleVersion                                 int64
		SourceEventID, ResultType, ResultID         *string
		Priority                                    string
		ConfigFieldCount                            int
	}
	if err := tx.Table("automation_runs").Select("id,rule_id,rule_version,status,trigger_type,logical_key,source_event_id,result_type,result_id,json_extract(config_snapshot_json,'$.priority') AS priority,(SELECT COUNT(*) FROM json_each(config_snapshot_json)) AS config_field_count,"+aiAgentFailureMetadataColumns("action_snapshot_json", "action_type", "agent_run_id", "task_id", "attempt", "error_code", "failed_at", "priority")).Where("id=?", payload.AutomationRunID).Take(&automation).Error; err != nil {
		return err
	}
	var snapshot map[string]any
	if json.Unmarshal([]byte(automation.MetadataJSON), &snapshot) != nil {
		return errAIInboxSourceIdentity
	}
	action, err := automationAgentRunFailureActionFromSnapshot(snapshot)
	preset, ok := automationPresetByKey(automationPresetAgentRunFailed)
	if err != nil || !ok || automation.FieldCount != 7 || !automation.FieldTypesValid || automation.ConfigFieldCount != 1 || action.automationAgentRunFailureEvidence != evidence || action.Priority != automation.Priority || automation.RuleID != preset.ID || automation.RuleID != payload.AutomationRuleID || automation.RuleVersion < 1 || automation.Status != "succeeded" || automation.TriggerType != "event" || automation.SourceEventID == nil || *automation.SourceEventID != payload.SourceEventID || automation.LogicalKey != "event:"+automation.RuleID+":"+payload.SourceEventID || automation.ResultType == nil || *automation.ResultType != "inbox_item" || automation.ResultID == nil || *automation.ResultID != ref.ID {
		return errAIInboxSourceIdentity
	}
	var rule struct{ PresetKey string }
	if err := tx.Table("automation_rules").Select("preset_key").Where("id=?", automation.RuleID).Take(&rule).Error; err != nil {
		return err
	}
	if rule.PresetKey != automationPresetAgentRunFailed {
		return errAIInboxSourceIdentity
	}
	task, err := loadAIInboxSourceEntity(tx, "tasks", "task", live.TaskID)
	if err != nil {
		return err
	}
	result.Subject = &aiInboxSourceEntity{Type: "agent_run", ID: live.ID, Status: &live.Status, Route: agentRunRoute(live.TaskID, live.ID)}
	result.Related = []aiInboxSourceEntity{task, {Type: "automation_run", ID: automation.ID, Status: &automation.Status, Route: aiAutomationRoute("run", automation.ID)}}
	result.LookupStatus = "available"
	return nil
}
