package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const maxAIAgentInboxWindow = 1000

const aiInboxCurrentGenerationFailureFilter = "(generation.status = 'failed' OR " +
	"(generation.status = 'cancelled' AND generation.error_code = 'AI_GENERATION_INTERRUPTED'))"

type aiAgentInboxItem struct {
	Kind                     string  `json:"kind"`
	ID                       string  `json:"id"`
	CreatedAt                string  `json:"created_at"`
	Action                   *string `json:"action,omitempty"`
	SessionID                *string `json:"session_id,omitempty"`
	SessionTitle             *string `json:"session_title,omitempty"`
	GenerationID             *string `json:"generation_id,omitempty"`
	ProviderID               *string `json:"provider_id,omitempty"`
	ProviderName             *string `json:"provider_name,omitempty"`
	ProviderModel            *string `json:"provider_model,omitempty"`
	ProviderStatus           *string `json:"provider_status,omitempty"`
	HealthStatus             *string `json:"health_status,omitempty"`
	AdapterID                *string `json:"adapter_id,omitempty"`
	AdapterName              *string `json:"adapter_name,omitempty"`
	AdapterKey               *string `json:"adapter_key,omitempty"`
	AdapterStatus            *string `json:"adapter_status,omitempty"`
	IsolationStatus          *string `json:"isolation_status,omitempty"`
	ExecutionReady           *bool   `json:"execution_ready,omitempty"`
	TaskID                   *string `json:"task_id,omitempty"`
	TaskTitle                *string `json:"task_title,omitempty"`
	SubmissionID             *string `json:"submission_id,omitempty"`
	Sequence                 *int    `json:"sequence,omitempty"`
	SubmissionKind           *string `json:"submission_origin,omitempty"`
	RuleID                   *string `json:"rule_id,omitempty"`
	RuleName                 *string `json:"rule_name,omitempty"`
	SourceID                 *string `json:"source_id,omitempty"`
	SourceName               *string `json:"source_name,omitempty"`
	Component                *string `json:"component,omitempty"`
	Operation                *string `json:"operation,omitempty"`
	Attempt                  *int    `json:"attempt,omitempty"`
	Retryable                *bool   `json:"retryable,omitempty"`
	RetryAt                  *string `json:"retry_at,omitempty"`
	ErrorCode                *string `json:"error_code,omitempty"`
	RunStatus                *string `json:"run_status,omitempty"`
	RunDelivery              *string `json:"output_delivery_status,omitempty"`
	ContinuationStatus       *string `json:"continuation_status,omitempty"`
	ContinuationReason       *string `json:"continuation_reason,omitempty"`
	ContinuationMaxTurns     *int    `json:"continuation_max_turns,omitempty"`
	ContinuationTurnsStarted *int    `json:"continuation_turns_started,omitempty"`
	ContinuationExpiresAt    *string `json:"continuation_expires_at,omitempty"`
}

type aiAgentInboxMeta struct {
	KindFilter              string `json:"kind_filter"`
	Total                   int64  `json:"total"`
	ApprovalTotal           int64  `json:"approval_total"`
	AccessRequestTotal      int64  `json:"access_request_total"`
	ReviewTotal             int64  `json:"review_total"`
	AutomationFailureTotal  int64  `json:"automation_failure_total"`
	KnowledgeFailureTotal   int64  `json:"knowledge_failure_total"`
	GenerationFailureTotal  int64  `json:"generation_failure_total"`
	ProviderIssueTotal      int64  `json:"provider_issue_total"`
	AdapterIssueTotal       int64  `json:"adapter_issue_total"`
	MaintenanceFailureTotal int64  `json:"maintenance_failure_total"`
	AgentRunTotal           int64  `json:"agent_run_total"`
	ContinuationTotal       int64  `json:"continuation_total"`
	HasMore                 bool   `json:"has_more"`
	NextOffset              *int   `json:"next_offset"`
	WindowLimited           bool   `json:"window_limited"`
	AsOf                    string `json:"as_of"`
}

type aiAgentInboxRow struct {
	Kind             string  `gorm:"column:kind"`
	ID               string  `gorm:"column:id"`
	CreatedAt        string  `gorm:"column:created_at"`
	ActionJSON       string  `gorm:"column:action_json"`
	SessionID        string  `gorm:"column:session_id"`
	SessionTitle     string  `gorm:"column:session_title"`
	GenerationID     string  `gorm:"column:generation_id"`
	ProviderID       string  `gorm:"column:provider_id"`
	ProviderName     string  `gorm:"column:provider_name"`
	ProviderModel    string  `gorm:"column:provider_model"`
	ProviderStatus   string  `gorm:"column:provider_status"`
	HealthStatus     string  `gorm:"column:health_status"`
	TaskID           string  `gorm:"column:task_id"`
	TaskTitle        string  `gorm:"column:task_title"`
	SubmissionID     string  `gorm:"column:submission_id"`
	Sequence         int     `gorm:"column:sequence"`
	SubmissionOrigin string  `gorm:"column:submission_origin"`
	RuleID           string  `gorm:"column:rule_id"`
	PresetKey        string  `gorm:"column:preset_key"`
	SourceID         string  `gorm:"column:source_id"`
	SourceName       string  `gorm:"column:source_name"`
	Operation        string  `gorm:"column:operation"`
	Attempt          int     `gorm:"column:attempt"`
	Retryable        bool    `gorm:"column:retryable"`
	RetryAt          *string `gorm:"column:retry_at"`
	ErrorCode        *string `gorm:"column:error_code"`
	RunStatus        string  `gorm:"column:run_status"`
	RunDelivery      string  `gorm:"column:output_delivery_status"`
	AdapterID        string  `gorm:"column:adapter_id"`
	AdapterName      string  `gorm:"column:adapter_name"`
	AdapterKey       string  `gorm:"column:adapter_key"`
	AdapterStatus    string  `gorm:"column:adapter_status"`
	IsolationStatus  string  `gorm:"column:isolation_status"`
	ExecutionReady   int     `gorm:"column:execution_ready"`
}

const aiInboxPlanExclusion = `NOT EXISTS (
	SELECT 1
	FROM ai_work_plan_revisions AS revision
	JOIN (
		SELECT session_id, MAX(version) AS version
		FROM ai_work_plan_revisions
		GROUP BY session_id
	) AS latest ON latest.session_id = revision.session_id AND latest.version = revision.version
	JOIN json_each(revision.plan_json, '$.steps') AS step
	WHERE revision.session_id = generation.session_id
		AND json_extract(step.value, '$.proposal_id') = proposal.id
		AND NOT ` + aiWorkPlanRevisionClosedFilter + `
)`

const aiInboxProviderIssueFilter = `(
	(provider.status = 'unconfigured' AND provider.health_status = 'unknown') OR
	(provider.status = 'unavailable' AND provider.health_status = 'unhealthy')
)`

// DB constraints make an enabled adapter always execution-ready and healthy.
// Only terminal health/isolation failures need human recovery attention;
// intentional disable and never-checked unknown stay out of the queue.
const aiInboxAdapterIssueFilter = `(
	adapter.health_status IN ('blocked', 'unhealthy')
	OR adapter.isolation_status = 'unsupported'
)`

const aiInboxMaintenanceFailureFilter = `item.source_entity_id IN (
	'backup:create',
	'backup:verify',
	'backup:drill',
	'backup:restore',
	'database:startup',
	'database:migration',
	'sidecar:startup',
	'database:runtime',
	'storage:low_space'
)`

const aiInboxAgentRunAttentionFilter = `(
	run.status IN ('queued', 'running', 'failed', 'interrupted') OR
	run.output_delivery_status IN ('pending', 'retained')
) AND NOT (
	run.status IN ('failed', 'interrupted') AND EXISTS (
		SELECT 1
		FROM agent_runs AS retry
		WHERE retry.parent_run_id = run.id
			AND retry.task_id = run.task_id
			AND retry.attempt = run.attempt + 1
	)
)`

var aiInboxContinuationReasons = map[string]struct{}{
	"ready": {}, "pending_approval": {}, "run_active": {}, "output_pending": {},
	"provider_busy": {}, "generating": {}, "evidence_unavailable": {}, "plan_changed": {},
	"generation_failed": {}, "generation_cancelled": {}, "restore_pending": {},
}

const aiAgentInboxUnion = `
	SELECT
		'approval' AS kind,
		proposal.id AS id,
		proposal.created_at AS created_at,
		proposal.action_json AS action_json,
		session.id AS session_id,
		session.title AS session_title,
		generation.id AS generation_id,
		'' AS provider_id,
		'' AS provider_name,
		'' AS provider_model,
		'' AS provider_status,
		'' AS health_status,
		'' AS task_id,
		'' AS task_title,
		'' AS submission_id,
		0 AS sequence,
		'' AS submission_origin,
		'' AS rule_id,
		'' AS preset_key,
		'' AS source_id,
		'' AS source_name,
		'' AS operation,
		0 AS attempt,
		0 AS retryable,
		NULL AS retry_at,
		NULL AS error_code,
		'' AS run_status,
		'' AS output_delivery_status,
		'' AS adapter_id,
		'' AS adapter_name,
		'' AS adapter_key,
		'' AS adapter_status,
		'' AS isolation_status,
		0 AS execution_ready
	FROM ai_action_proposals AS proposal
	JOIN ai_generations AS generation ON generation.id = proposal.generation_id
	JOIN ai_sessions AS session ON session.id = generation.session_id
	WHERE proposal.status = 'pending'
		AND session.persist = 1
		AND ` + aiInboxPlanExclusion + `
	UNION ALL
	SELECT
		'access_request' AS kind,
		request.generation_id AS id,
		request.created_at AS created_at,
		'' AS action_json,
		session.id AS session_id,
		session.title AS session_title,
		generation.id AS generation_id,
		'' AS provider_id,
		'' AS provider_name,
		'' AS provider_model,
		'' AS provider_status,
		'' AS health_status,
		'' AS task_id,
		'' AS task_title,
		'' AS submission_id,
		0 AS sequence,
		'' AS submission_origin,
		'' AS rule_id,
		'' AS preset_key,
		'' AS source_id,
		'' AS source_name,
		'' AS operation,
		0 AS attempt,
		0 AS retryable,
		NULL AS retry_at,
		NULL AS error_code,
		'' AS run_status,
		'' AS output_delivery_status,
		'' AS adapter_id,
		'' AS adapter_name,
		'' AS adapter_key,
		'' AS adapter_status,
		'' AS isolation_status,
		0 AS execution_ready
	FROM ai_workspace_access_requests AS request
	JOIN ai_generations AS generation ON generation.id = request.generation_id
	JOIN ai_sessions AS session ON session.id = generation.session_id
	WHERE request.status = 'open'
		AND generation.status = 'completed'
		AND session.persist = 1
	UNION ALL
	SELECT
		'review' AS kind,
		submission.id AS id,
		submission.submitted_at AS created_at,
		'' AS action_json,
		'' AS session_id,
		'' AS session_title,
		'' AS generation_id,
		'' AS provider_id,
		'' AS provider_name,
		'' AS provider_model,
		'' AS provider_status,
		'' AS health_status,
		task.id AS task_id,
		task.title AS task_title,
		submission.id AS submission_id,
		submission.sequence AS sequence,
		CASE WHEN submission.origin = '' THEN 'manual' ELSE submission.origin END AS submission_origin,
		'' AS rule_id,
		'' AS preset_key,
		'' AS source_id,
		'' AS source_name,
		'' AS operation,
		0 AS attempt,
		0 AS retryable,
		NULL AS retry_at,
		NULL AS error_code,
		'' AS run_status,
		'' AS output_delivery_status,
		'' AS adapter_id,
		'' AS adapter_name,
		'' AS adapter_key,
		'' AS adapter_status,
		'' AS isolation_status,
		0 AS execution_ready
	FROM task_submissions AS submission
	JOIN tasks AS task ON task.id = submission.task_id
	WHERE submission.status = 'pending_review'
		AND task.status = 'waiting_review'
		AND task.current_submission_id = submission.id
	UNION ALL
	SELECT
		'automation_failure' AS kind,
		run.id AS id,
		run.ended_at AS created_at,
		'' AS action_json,
		'' AS session_id,
		'' AS session_title,
		'' AS generation_id,
		'' AS provider_id,
		'' AS provider_name,
		'' AS provider_model,
		'' AS provider_status,
		'' AS health_status,
		'' AS task_id,
		'' AS task_title,
		'' AS submission_id,
		0 AS sequence,
		'' AS submission_origin,
		rule.id AS rule_id,
		rule.preset_key AS preset_key,
		'' AS source_id,
		'' AS source_name,
		'' AS operation,
		run.attempt AS attempt,
		run.retryable AS retryable,
		run.retry_at AS retry_at,
		run.error_code AS error_code,
		'' AS run_status,
		'' AS output_delivery_status,
		'' AS adapter_id,
		'' AS adapter_name,
		'' AS adapter_key,
		'' AS adapter_status,
		'' AS isolation_status,
		0 AS execution_ready
	FROM automation_runs AS run
	JOIN automation_rules AS rule ON rule.id = run.rule_id
	WHERE run.status = 'failed'
		AND NOT EXISTS (
			SELECT 1 FROM automation_runs AS child WHERE child.retry_of_run_id = run.id
		)
	UNION ALL
	SELECT
		'knowledge_failure' AS kind,
		job.id AS id,
		job.completed_at AS created_at,
		'' AS action_json,
		'' AS session_id,
		'' AS session_title,
		'' AS generation_id,
		'' AS provider_id,
		'' AS provider_name,
		'' AS provider_model,
		'' AS provider_status,
		'' AS health_status,
		'' AS task_id,
		'' AS task_title,
		'' AS submission_id,
		0 AS sequence,
		'' AS submission_origin,
		'' AS rule_id,
		'' AS preset_key,
		source.id AS source_id,
		source.name AS source_name,
		job.operation AS operation,
		job.attempt AS attempt,
		0 AS retryable,
		NULL AS retry_at,
		job.error_code AS error_code,
		'' AS run_status,
		'' AS output_delivery_status,
		'' AS adapter_id,
		'' AS adapter_name,
		'' AS adapter_key,
		'' AS adapter_status,
		'' AS isolation_status,
		0 AS execution_ready
	FROM knowledge_index_jobs AS job
	JOIN knowledge_sources AS source ON source.id = job.source_id AND source.deleted_at IS NULL
	WHERE job.status = 'failed'
		AND job.id = (
			SELECT latest.id
			FROM knowledge_index_jobs AS latest
			WHERE latest.source_id = job.source_id
			ORDER BY latest.created_at DESC, latest.id DESC
			LIMIT 1
		)
		AND NOT EXISTS (
			SELECT 1 FROM knowledge_index_jobs AS child WHERE child.retry_of_job_id = job.id
		)
	UNION ALL
	SELECT
		'generation_failure' AS kind,
		generation.id AS id,
		generation.updated_at AS created_at,
		'' AS action_json,
		session.id AS session_id,
		session.title AS session_title,
		generation.id AS generation_id,
		'' AS provider_id,
		'' AS provider_name,
		'' AS provider_model,
		'' AS provider_status,
		'' AS health_status,
		'' AS task_id,
		'' AS task_title,
		'' AS submission_id,
		0 AS sequence,
		'' AS submission_origin,
		'' AS rule_id,
		'' AS preset_key,
		'' AS source_id,
		'' AS source_name,
		'' AS operation,
		0 AS attempt,
		0 AS retryable,
		NULL AS retry_at,
		generation.error_code AS error_code,
		'' AS run_status,
		'' AS output_delivery_status,
		'' AS adapter_id,
		'' AS adapter_name,
		'' AS adapter_key,
		'' AS adapter_status,
		'' AS isolation_status,
		0 AS execution_ready
	FROM ai_generations AS generation
	JOIN ai_sessions AS session ON session.id = generation.session_id
	WHERE ` + aiInboxCurrentGenerationFailureFilter + `
		AND session.persist = 1
		AND generation.id = (
			SELECT latest.id
			FROM ai_generations AS latest
			WHERE latest.session_id = generation.session_id
			ORDER BY latest.created_at DESC, latest.id DESC
			LIMIT 1
		)
	UNION ALL
	SELECT
		'provider_issue' AS kind,
		provider.id AS id,
		provider.updated_at AS created_at,
		'' AS action_json,
		'' AS session_id,
		'' AS session_title,
		'' AS generation_id,
		provider.id AS provider_id,
		provider.name AS provider_name,
		provider.model AS provider_model,
		provider.status AS provider_status,
		provider.health_status AS health_status,
		'' AS task_id,
		'' AS task_title,
		'' AS submission_id,
		0 AS sequence,
		'' AS submission_origin,
		'' AS rule_id,
		'' AS preset_key,
		'' AS source_id,
		'' AS source_name,
		'' AS operation,
		0 AS attempt,
		0 AS retryable,
		NULL AS retry_at,
		provider.health_error_code AS error_code,
		'' AS run_status,
		'' AS output_delivery_status,
		'' AS adapter_id,
		'' AS adapter_name,
		'' AS adapter_key,
		'' AS adapter_status,
		'' AS isolation_status,
		0 AS execution_ready
	FROM ai_providers AS provider
	WHERE ` + aiInboxProviderIssueFilter + `
	UNION ALL
	SELECT
		'maintenance_failure' AS kind,
		item.id AS id,
		item.created_at AS created_at,
		'' AS action_json,
		'' AS session_id,
		'' AS session_title,
		'' AS generation_id,
		'' AS provider_id,
		'' AS provider_name,
		'' AS provider_model,
		'' AS provider_status,
		'' AS health_status,
		'' AS task_id,
		'' AS task_title,
		'' AS submission_id,
		0 AS sequence,
		'' AS submission_origin,
		'' AS rule_id,
		'' AS preset_key,
		item.source_entity_id AS source_id,
		'' AS source_name,
		'' AS operation,
		0 AS attempt,
		0 AS retryable,
		NULL AS retry_at,
		NULL AS error_code,
		'' AS run_status,
		'' AS output_delivery_status,
		'' AS adapter_id,
		'' AS adapter_name,
		'' AS adapter_key,
		'' AS adapter_status,
		'' AS isolation_status,
		0 AS execution_ready
	FROM inbox_items AS item
	WHERE item.source_entity_type = 'system_maintenance'
		AND item.status IN ('open', 'tracking')
		AND item.source_deleted_at IS NULL
		AND ` + aiInboxMaintenanceFailureFilter + `
	UNION ALL
	SELECT
		'agent_run' AS kind,
		run.id AS id,
		COALESCE(run.completed_at, run.created_at) AS created_at,
		'' AS action_json,
		'' AS session_id,
		'' AS session_title,
		'' AS generation_id,
		'' AS provider_id,
		'' AS provider_name,
		'' AS provider_model,
		'' AS provider_status,
		'' AS health_status,
		run.task_id AS task_id,
		substr(task.title, 1, 500) AS task_title,
		'' AS submission_id,
		0 AS sequence,
		'' AS submission_origin,
		'' AS rule_id,
		'' AS preset_key,
		'' AS source_id,
		'' AS source_name,
		'' AS operation,
		run.attempt AS attempt,
		0 AS retryable,
		NULL AS retry_at,
		NULL AS error_code,
		run.status AS run_status,
		run.output_delivery_status AS output_delivery_status,
		'' AS adapter_id,
		'' AS adapter_name,
		'' AS adapter_key,
		'' AS adapter_status,
		'' AS isolation_status,
		0 AS execution_ready
	FROM agent_runs AS run
	JOIN tasks AS task ON task.id = run.task_id
	WHERE ` + aiInboxAgentRunAttentionFilter + `
	UNION ALL
	SELECT
		'continuation' AS kind,
		continuation.id AS id,
		continuation.updated_at AS created_at,
		'' AS action_json,
		session.id AS session_id,
		session.title AS session_title,
		COALESCE(continuation.current_generation_id, '') AS generation_id,
		'' AS provider_id,
		'' AS provider_name,
		'' AS provider_model,
		'' AS provider_status,
		'' AS health_status,
		'' AS task_id,
		'' AS task_title,
		'' AS submission_id,
		continuation.max_turns AS sequence,
		'' AS submission_origin,
		'' AS rule_id,
		'' AS preset_key,
		'' AS source_id,
		'' AS source_name,
		continuation.reason AS operation,
		continuation.turns_started AS attempt,
		0 AS retryable,
		continuation.expires_at AS retry_at,
		NULL AS error_code,
		continuation.status AS run_status,
		'' AS output_delivery_status,
		'' AS adapter_id,
		'' AS adapter_name,
		'' AS adapter_key,
		'' AS adapter_status,
		'' AS isolation_status,
		0 AS execution_ready
	FROM ai_continuations AS continuation
	JOIN ai_sessions AS session ON session.id = continuation.session_id
	WHERE continuation.status IN ('waiting', 'running')
		AND session.persist = 1
	UNION ALL
	SELECT
		'adapter_issue' AS kind,
		adapter.id AS id,
		adapter.updated_at AS created_at,
		'' AS action_json,
		'' AS session_id,
		'' AS session_title,
		'' AS generation_id,
		'' AS provider_id,
		'' AS provider_name,
		'' AS provider_model,
		'' AS provider_status,
		adapter.health_status AS health_status,
		'' AS task_id,
		'' AS task_title,
		'' AS submission_id,
		0 AS sequence,
		'' AS submission_origin,
		'' AS rule_id,
		'' AS preset_key,
		'' AS source_id,
		'' AS source_name,
		'' AS operation,
		0 AS attempt,
		0 AS retryable,
		NULL AS retry_at,
		adapter.health_error_code AS error_code,
		'' AS run_status,
		'' AS output_delivery_status,
		adapter.id AS adapter_id,
		adapter.display_name AS adapter_name,
		adapter.adapter_key AS adapter_key,
		adapter.status AS adapter_status,
		adapter.isolation_status AS isolation_status,
		adapter.execution_ready AS execution_ready
	FROM agent_adapters AS adapter
	WHERE ` + aiInboxAdapterIssueFilter

func optionalAIAgentInboxKind(c *gin.Context) (string, bool) {
	kind := strings.TrimSpace(c.Query("kind"))
	if kind == "" {
		return "all", true
	}
	if kind != "all" && kind != "approval" && kind != "access_request" && kind != "review" && kind != "automation_failure" && kind != "knowledge_failure" && kind != "generation_failure" && kind != "provider_issue" && kind != "adapter_issue" && kind != "maintenance_failure" && kind != "agent_run" && kind != "continuation" {
		writeError(c, http.StatusUnprocessableEntity, "AI_INBOX_KIND_INVALID", "kind must be all, approval, access_request, review, automation_failure, knowledge_failure, generation_failure, provider_issue, adapter_issue, maintenance_failure, agent_run, or continuation")
		return "", false
	}
	return kind, true
}

func (a *API) listAIAgentInbox(c *gin.Context) {
	kind, ok := optionalAIAgentInboxKind(c)
	if !ok {
		return
	}
	limit, ok := queryInt(c, "limit", 50, 1, 50)
	if !ok {
		return
	}
	offset, ok := queryInt(c, "offset", 0, 0, maxAIAgentInboxWindow-1)
	if !ok {
		return
	}
	effectiveLimit := limit
	if offset+effectiveLimit > maxAIAgentInboxWindow {
		effectiveLimit = maxAIAgentInboxWindow - offset
	}

	var rows []aiAgentInboxRow
	var approvalTotal, accessRequestTotal, reviewTotal, automationFailureTotal, knowledgeFailureTotal, generationFailureTotal, providerIssueTotal, adapterIssueTotal, maintenanceFailureTotal, agentRunTotal, continuationTotal int64
	var verifiedRestartSources []string
	var verifiedRestartSourcesJSON = "[]"
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var err error
		verifiedRestartSources, err = verifiedAgentRunRestartSupersededIDs(tx)
		if err != nil {
			return err
		}
		if len(verifiedRestartSources) > 0 {
			encoded, marshalErr := json.Marshal(verifiedRestartSources)
			if marshalErr != nil {
				return marshalErr
			}
			verifiedRestartSourcesJSON = string(encoded)
		}
		approvalQuery := tx.Table("ai_action_proposals AS proposal").
			Joins("JOIN ai_generations AS generation ON generation.id = proposal.generation_id").
			Joins("JOIN ai_sessions AS session ON session.id = generation.session_id").
			Where("proposal.status = 'pending' AND session.persist = 1").
			Where(aiInboxPlanExclusion)
		if err := approvalQuery.Count(&approvalTotal).Error; err != nil {
			return err
		}
		if err := tx.Table("ai_workspace_access_requests AS request").
			Joins("JOIN ai_generations AS generation ON generation.id = request.generation_id").
			Joins("JOIN ai_sessions AS session ON session.id = generation.session_id").
			Where("request.status = 'open' AND generation.status = 'completed' AND session.persist = 1").
			Count(&accessRequestTotal).Error; err != nil {
			return err
		}
		if err := tx.Table("task_submissions AS submission").
			Joins("JOIN tasks AS task ON task.id = submission.task_id").
			Where("submission.status = 'pending_review' AND task.status = 'waiting_review' AND task.current_submission_id = submission.id").
			Count(&reviewTotal).Error; err != nil {
			return err
		}
		if err := tx.Table("automation_runs AS run").
			Where("run.status = 'failed'").
			Where("NOT EXISTS (SELECT 1 FROM automation_runs AS child WHERE child.retry_of_run_id = run.id)").
			Count(&automationFailureTotal).Error; err != nil {
			return err
		}
		if err := tx.Table("knowledge_index_jobs AS job").
			Joins("JOIN knowledge_sources AS source ON source.id = job.source_id AND source.deleted_at IS NULL").
			Where("job.status = 'failed'").
			Where("job.id = (SELECT latest.id FROM knowledge_index_jobs AS latest WHERE latest.source_id = job.source_id ORDER BY latest.created_at DESC, latest.id DESC LIMIT 1)").
			Where("NOT EXISTS (SELECT 1 FROM knowledge_index_jobs AS child WHERE child.retry_of_job_id = job.id)").
			Count(&knowledgeFailureTotal).Error; err != nil {
			return err
		}
		if err := tx.Table("ai_generations AS generation").
			Joins("JOIN ai_sessions AS session ON session.id = generation.session_id").
			Where(aiInboxCurrentGenerationFailureFilter).
			Where("session.persist = 1").
			Where("generation.id = (SELECT latest.id FROM ai_generations AS latest WHERE latest.session_id = generation.session_id ORDER BY latest.created_at DESC, latest.id DESC LIMIT 1)").
			Count(&generationFailureTotal).Error; err != nil {
			return err
		}
		if err := tx.Table("ai_providers AS provider").
			Where(aiInboxProviderIssueFilter).
			Count(&providerIssueTotal).Error; err != nil {
			return err
		}
		if err := tx.Table("agent_adapters AS adapter").
			Where(aiInboxAdapterIssueFilter).
			Count(&adapterIssueTotal).Error; err != nil {
			return err
		}
		if err := tx.Table("inbox_items AS item").
			Where("item.source_entity_type = 'system_maintenance' AND item.status IN ('open', 'tracking') AND item.source_deleted_at IS NULL").
			Where(aiInboxMaintenanceFailureFilter).
			Count(&maintenanceFailureTotal).Error; err != nil {
			return err
		}
		agentRunQuery := tx.Table("agent_runs AS run").
			Joins("JOIN tasks AS task ON task.id = run.task_id").
			Where(aiInboxAgentRunAttentionFilter)
		if len(verifiedRestartSources) > 0 {
			agentRunQuery = agentRunQuery.Where("run.id NOT IN ?", verifiedRestartSources)
		}
		if err := agentRunQuery.Count(&agentRunTotal).Error; err != nil {
			return err
		}
		if err := tx.Table("ai_continuations AS continuation").
			Joins("JOIN ai_sessions AS session ON session.id = continuation.session_id").
			Where("continuation.status IN ('waiting', 'running') AND session.persist = 1").
			Count(&continuationTotal).Error; err != nil {
			return err
		}
		return tx.Raw(`SELECT * FROM (`+aiAgentInboxUnion+`) AS inbox
			WHERE (? = 'all' OR inbox.kind = ?)
				AND (inbox.kind <> 'agent_run' OR inbox.id NOT IN (SELECT value FROM json_each(?)))
			ORDER BY inbox.created_at DESC, inbox.kind ASC, inbox.id ASC
			LIMIT ? OFFSET ?`, kind, kind, verifiedRestartSourcesJSON, effectiveLimit, offset).Scan(&rows).Error
	})
	if err != nil {
		writeDatabaseError(c)
		return
	}

	items := make([]aiAgentInboxItem, 0, len(rows))
	for _, row := range rows {
		item := aiAgentInboxItem{Kind: row.Kind, ID: row.ID, CreatedAt: row.CreatedAt}
		switch row.Kind {
		case "approval":
			var action struct {
				Action string `json:"action"`
			}
			if json.Unmarshal([]byte(row.ActionJSON), &action) != nil || strings.TrimSpace(action.Action) == "" || utf8.RuneCountInString(action.Action) > 80 {
				writeDatabaseError(c)
				return
			}
			item.Action = &action.Action
			item.SessionID = &row.SessionID
			item.SessionTitle = &row.SessionTitle
			item.GenerationID = &row.GenerationID
		case "access_request":
			if strings.TrimSpace(row.SessionID) == "" || strings.TrimSpace(row.SessionTitle) == "" ||
				utf8.RuneCountInString(row.SessionTitle) > 200 || row.GenerationID != row.ID {
				writeDatabaseError(c)
				return
			}
			item.SessionID = &row.SessionID
			item.SessionTitle = &row.SessionTitle
			item.GenerationID = &row.GenerationID
		case "review":
			item.TaskID = &row.TaskID
			item.TaskTitle = &row.TaskTitle
			item.SubmissionID = &row.SubmissionID
			item.Sequence = &row.Sequence
			item.SubmissionKind = &row.SubmissionOrigin
		case "automation_failure":
			preset, ok := automationPresetByKey(row.PresetKey)
			if !ok || row.Attempt < 1 || row.Attempt > automationMaxAttempts || strings.TrimSpace(row.RuleID) == "" {
				writeDatabaseError(c)
				return
			}
			item.RuleID = &row.RuleID
			item.RuleName = &preset.Name
			item.Attempt = &row.Attempt
			item.Retryable = &row.Retryable
			item.RetryAt = row.RetryAt
			item.ErrorCode = row.ErrorCode
		case "knowledge_failure":
			if strings.TrimSpace(row.SourceID) == "" || strings.TrimSpace(row.SourceName) == "" ||
				utf8.RuneCountInString(row.SourceName) > 255 || (row.Operation != "import" && row.Operation != "reindex") ||
				row.Attempt < 1 || row.ErrorCode == nil || strings.TrimSpace(*row.ErrorCode) == "" || utf8.RuneCountInString(*row.ErrorCode) > 120 {
				writeDatabaseError(c)
				return
			}
			item.SourceID = &row.SourceID
			item.SourceName = &row.SourceName
			item.Operation = &row.Operation
			item.Attempt = &row.Attempt
			item.ErrorCode = row.ErrorCode
		case "generation_failure":
			if strings.TrimSpace(row.SessionID) == "" || strings.TrimSpace(row.SessionTitle) == "" ||
				utf8.RuneCountInString(row.SessionTitle) > 200 || strings.TrimSpace(row.GenerationID) == "" ||
				row.GenerationID != row.ID || row.ErrorCode == nil || strings.TrimSpace(*row.ErrorCode) == "" ||
				utf8.RuneCountInString(*row.ErrorCode) > 120 {
				writeDatabaseError(c)
				return
			}
			item.SessionID = &row.SessionID
			item.SessionTitle = &row.SessionTitle
			item.GenerationID = &row.GenerationID
			item.ErrorCode = row.ErrorCode
		case "provider_issue":
			if strings.TrimSpace(row.ProviderID) == "" || row.ProviderID != row.ID ||
				strings.TrimSpace(row.ProviderName) == "" || utf8.RuneCountInString(row.ProviderName) > 120 ||
				strings.TrimSpace(row.ProviderModel) == "" || utf8.RuneCountInString(row.ProviderModel) > 160 ||
				!((row.ProviderStatus == "unconfigured" && row.HealthStatus == "unknown") ||
					(row.ProviderStatus == "unavailable" && row.HealthStatus == "unhealthy")) ||
				(row.ErrorCode != nil && (strings.TrimSpace(*row.ErrorCode) == "" || utf8.RuneCountInString(*row.ErrorCode) > 120)) {
				writeDatabaseError(c)
				return
			}
			item.ProviderID = &row.ProviderID
			item.ProviderName = &row.ProviderName
			item.ProviderModel = &row.ProviderModel
			item.ProviderStatus = &row.ProviderStatus
			item.HealthStatus = &row.HealthStatus
			item.ErrorCode = row.ErrorCode
		case "adapter_issue":
			executionReady := row.ExecutionReady == 1
			needsAttention := row.HealthStatus == "blocked" || row.HealthStatus == "unhealthy" || row.IsolationStatus == "unsupported"
			if strings.TrimSpace(row.AdapterID) == "" || row.AdapterID != row.ID ||
				strings.TrimSpace(row.AdapterName) == "" || utf8.RuneCountInString(row.AdapterName) > 120 ||
				strings.TrimSpace(row.AdapterKey) == "" || utf8.RuneCountInString(row.AdapterKey) > 120 ||
				(row.AdapterStatus != "enabled" && row.AdapterStatus != "disabled") ||
				!needsAttention ||
				(row.ErrorCode != nil && (strings.TrimSpace(*row.ErrorCode) == "" || utf8.RuneCountInString(*row.ErrorCode) > 120)) {
				writeDatabaseError(c)
				return
			}
			item.AdapterID = &row.AdapterID
			item.AdapterName = &row.AdapterName
			item.AdapterKey = &row.AdapterKey
			item.AdapterStatus = &row.AdapterStatus
			item.HealthStatus = &row.HealthStatus
			item.IsolationStatus = &row.IsolationStatus
			item.ExecutionReady = &executionReady
			item.ErrorCode = row.ErrorCode
		case "maintenance_failure":
			incident, ok := systemMaintenanceIncidentForSourceID(row.SourceID)
			if !ok {
				writeDatabaseError(c)
				return
			}
			item.Component = &incident.component
			item.Operation = &incident.operation
			item.ErrorCode = &incident.failureCode
		case "agent_run":
			_, validRunStatus := agentRunStatusSet[row.RunStatus]
			_, validRunDelivery := agentRunOutputDeliveryStatusSet[row.RunDelivery]
			if strings.TrimSpace(row.TaskID) == "" || strings.TrimSpace(row.TaskTitle) == "" ||
				utf8.RuneCountInString(row.TaskTitle) > 500 || row.Attempt < 1 ||
				!validRunStatus || !validRunDelivery {
				writeDatabaseError(c)
				return
			}
			item.TaskID = &row.TaskID
			item.TaskTitle = &row.TaskTitle
			item.Attempt = &row.Attempt
			item.RunStatus = &row.RunStatus
			item.RunDelivery = &row.RunDelivery
		case "continuation":
			if strings.TrimSpace(row.SessionID) == "" || strings.TrimSpace(row.SessionTitle) == "" ||
				utf8.RuneCountInString(row.SessionTitle) > 200 ||
				(row.RunStatus != "waiting" && row.RunStatus != "running") ||
				row.Sequence < 1 || row.Sequence > 8 || row.Attempt < 0 || row.Attempt > row.Sequence ||
				row.RetryAt == nil {
				writeDatabaseError(c)
				return
			}
			if _, err := time.Parse(time.RFC3339Nano, *row.RetryAt); err != nil {
				writeDatabaseError(c)
				return
			}
			if _, ok := aiInboxContinuationReasons[row.Operation]; !ok {
				writeDatabaseError(c)
				return
			}
			item.SessionID = &row.SessionID
			item.SessionTitle = &row.SessionTitle
			item.ContinuationStatus = &row.RunStatus
			item.ContinuationReason = &row.Operation
			item.ContinuationMaxTurns = &row.Sequence
			item.ContinuationTurnsStarted = &row.Attempt
			item.ContinuationExpiresAt = row.RetryAt
		default:
			writeDatabaseError(c)
			return
		}
		items = append(items, item)
	}

	filteredTotal := approvalTotal + accessRequestTotal + reviewTotal + automationFailureTotal + knowledgeFailureTotal + generationFailureTotal + providerIssueTotal + adapterIssueTotal + maintenanceFailureTotal + agentRunTotal + continuationTotal
	if kind == "approval" {
		filteredTotal = approvalTotal
	} else if kind == "access_request" {
		filteredTotal = accessRequestTotal
	} else if kind == "review" {
		filteredTotal = reviewTotal
	} else if kind == "automation_failure" {
		filteredTotal = automationFailureTotal
	} else if kind == "knowledge_failure" {
		filteredTotal = knowledgeFailureTotal
	} else if kind == "generation_failure" {
		filteredTotal = generationFailureTotal
	} else if kind == "provider_issue" {
		filteredTotal = providerIssueTotal
	} else if kind == "adapter_issue" {
		filteredTotal = adapterIssueTotal
	} else if kind == "maintenance_failure" {
		filteredTotal = maintenanceFailureTotal
	} else if kind == "agent_run" {
		filteredTotal = agentRunTotal
	} else if kind == "continuation" {
		filteredTotal = continuationTotal
	}
	accessibleTotal := filteredTotal
	if accessibleTotal > maxAIAgentInboxWindow {
		accessibleTotal = maxAIAgentInboxWindow
	}
	next := offset + len(items)
	hasMore := int64(next) < accessibleTotal
	var nextOffset *int
	if hasMore {
		nextOffset = &next
	}
	meta := aiAgentInboxMeta{
		KindFilter: kind, Total: filteredTotal, ApprovalTotal: approvalTotal, AccessRequestTotal: accessRequestTotal, ReviewTotal: reviewTotal, AutomationFailureTotal: automationFailureTotal, KnowledgeFailureTotal: knowledgeFailureTotal, GenerationFailureTotal: generationFailureTotal, ProviderIssueTotal: providerIssueTotal, AdapterIssueTotal: adapterIssueTotal, MaintenanceFailureTotal: maintenanceFailureTotal, AgentRunTotal: agentRunTotal, ContinuationTotal: continuationTotal,
		HasMore: hasMore, NextOffset: nextOffset, WindowLimited: filteredTotal > maxAIAgentInboxWindow,
		AsOf: a.options.Now().UTC().Format(time.RFC3339Nano),
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "meta": meta})
}
