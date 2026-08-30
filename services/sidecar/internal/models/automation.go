package models

type AutomationRule struct {
	ID         string  `gorm:"column:id;primaryKey" json:"id"`
	PresetKey  string  `gorm:"column:preset_key" json:"preset_key"`
	Enabled    bool    `gorm:"column:enabled" json:"enabled"`
	ConfigJSON string  `gorm:"column:config_json" json:"-"`
	NextRunAt  *string `gorm:"column:next_run_at" json:"next_run_at"`
	Version    int64   `gorm:"column:version" json:"version"`
	CreatedAt  string  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt  string  `gorm:"column:updated_at" json:"updated_at"`
}

func (AutomationRule) TableName() string { return "automation_rules" }

type AutomationRun struct {
	ID                 string  `gorm:"column:id;primaryKey" json:"id"`
	RuleID             string  `gorm:"column:rule_id" json:"rule_id"`
	RuleVersion        int64   `gorm:"column:rule_version" json:"rule_version"`
	TriggerType        string  `gorm:"column:trigger_type" json:"trigger_type"`
	SourceEventID      *string `gorm:"column:source_event_id" json:"source_event_id"`
	ScheduledFor       *string `gorm:"column:scheduled_for" json:"scheduled_for"`
	LogicalKey         string  `gorm:"column:logical_key" json:"logical_key"`
	DedupeKey          string  `gorm:"column:dedupe_key" json:"dedupe_key"`
	Status             string  `gorm:"column:status" json:"status"`
	Attempt            int     `gorm:"column:attempt" json:"attempt"`
	RetryOfRunID       *string `gorm:"column:retry_of_run_id" json:"retry_of_run_id"`
	Retryable          bool    `gorm:"column:retryable" json:"retryable"`
	RetryAt            *string `gorm:"column:retry_at" json:"retry_at"`
	CausedByRunID      *string `gorm:"column:caused_by_run_id" json:"caused_by_run_id"`
	CausalDepth        int     `gorm:"column:causal_depth" json:"causal_depth"`
	ConfigSnapshotJSON string  `gorm:"column:config_snapshot_json" json:"-"`
	ActionSnapshotJSON string  `gorm:"column:action_snapshot_json" json:"-"`
	ErrorCode          *string `gorm:"column:error_code" json:"error_code"`
	ResultType         *string `gorm:"column:result_type" json:"result_type"`
	ResultID           *string `gorm:"column:result_id" json:"result_id"`
	ResultSummary      string  `gorm:"column:result_summary" json:"result_summary"`
	StartedAt          string  `gorm:"column:started_at" json:"started_at"`
	EndedAt            string  `gorm:"column:ended_at" json:"ended_at"`
}

func (AutomationRun) TableName() string { return "automation_runs" }

type AutomationEventDelivery struct {
	ID                 string  `gorm:"column:id;primaryKey" json:"id"`
	RuleID             string  `gorm:"column:rule_id" json:"rule_id"`
	PresetKey          string  `gorm:"column:preset_key" json:"preset_key"`
	RuleVersion        int64   `gorm:"column:rule_version" json:"rule_version"`
	SourceEventID      string  `gorm:"column:source_event_id" json:"source_event_id"`
	LogicalKey         string  `gorm:"column:logical_key" json:"logical_key"`
	ConfigSnapshotJSON string  `gorm:"column:config_snapshot_json" json:"-"`
	ActionSnapshotJSON string  `gorm:"column:action_snapshot_json" json:"-"`
	DeliveryAttempts   int     `gorm:"column:delivery_attempts" json:"delivery_attempts"`
	AvailableAt        string  `gorm:"column:available_at" json:"available_at"`
	LastErrorCode      *string `gorm:"column:last_error_code" json:"last_error_code"`
	LastErrorAt        *string `gorm:"column:last_error_at" json:"last_error_at"`
	CapturedAt         string  `gorm:"column:captured_at" json:"captured_at"`
	UpdatedAt          string  `gorm:"column:updated_at" json:"updated_at"`
}

func (AutomationEventDelivery) TableName() string { return "automation_event_deliveries" }
