package models

type AgentRun struct {
	ID               string  `gorm:"column:id;primaryKey" json:"id"`
	TaskID           string  `gorm:"column:task_id" json:"task_id"`
	AssignmentID     string  `gorm:"column:assignment_id" json:"assignment_id"`
	ActorID          string  `gorm:"column:actor_id" json:"actor_id"`
	AdapterID        string  `gorm:"column:adapter_id" json:"adapter_id"`
	CreatedByActorID string  `gorm:"column:created_by_actor_id" json:"created_by_actor_id"`
	ParentRunID      *string `gorm:"column:parent_run_id" json:"parent_run_id"`
	// RestartOfRunID is projected only from a verified immutable workflow event.
	// It is not a database column and never substitutes for exact retry lineage.
	RestartOfRunID *string `gorm:"-" json:"restart_of_run_id,omitempty"`
	// StartGate is projected from immutable start-gate workflow events (ADR-030).
	StartGate                        *AgentRunStartGate `gorm:"-" json:"start_gate,omitempty"`
	Attempt                          int                `gorm:"column:attempt" json:"attempt"`
	Status                           string             `gorm:"column:status" json:"status"`
	ProviderID                       string             `gorm:"column:provider_id" json:"provider_id"`
	Model                            string             `gorm:"column:model" json:"model"`
	TaskVersion                      int64              `gorm:"column:task_version" json:"task_version"`
	AssignmentAssignedAt             string             `gorm:"column:assignment_assigned_at" json:"assignment_assigned_at"`
	ActorVersion                     int64              `gorm:"column:actor_version" json:"actor_version"`
	AdapterVersion                   int64              `gorm:"column:adapter_version" json:"adapter_version"`
	ProviderVersion                  int64              `gorm:"column:provider_version" json:"provider_version"`
	ProviderConfigVersion            int64              `gorm:"column:provider_config_version" json:"provider_config_version"`
	ExecutionContractVersion         int64              `gorm:"column:execution_contract_version" json:"execution_contract_version"`
	InputSnapshotJSON                string             `gorm:"column:input_snapshot_json" json:"-"`
	ResultText                       *string            `gorm:"column:result_text" json:"result_text"`
	ResultBytes                      *int               `gorm:"column:result_bytes" json:"result_bytes"`
	ErrorCode                        *string            `gorm:"column:error_code" json:"error_code"`
	OutputDeliveryStatus             string             `gorm:"column:output_delivery_status" json:"output_delivery_status"`
	OutputDeliveryErrorCode          *string            `gorm:"column:output_delivery_error_code" json:"output_delivery_error_code"`
	SubmissionID                     *string            `gorm:"column:submission_id" json:"submission_id"`
	ArtifactID                       *string            `gorm:"column:artifact_id" json:"artifact_id"`
	OutputDeliveryPendingText        *string            `gorm:"column:output_delivery_pending_text" json:"-"`
	OutputDeliveryPendingBytes       *int               `gorm:"column:output_delivery_pending_bytes" json:"-"`
	OutputDeliveryPendingCompletedAt *string            `gorm:"column:output_delivery_pending_completed_at" json:"-"`
	StartedAt                        *string            `gorm:"column:started_at" json:"started_at"`
	CompletedAt                      *string            `gorm:"column:completed_at" json:"completed_at"`
	CreatedAt                        string             `gorm:"column:created_at" json:"created_at"`
}

func (AgentRun) TableName() string { return "agent_runs" }

// AgentRunStartGate is the human-confirmed precondition for a queued Run.
type AgentRunStartGate struct {
	PredecessorRunID string  `json:"predecessor_run_id"`
	Require          string  `json:"require"`
	ExpiresAt        string  `json:"expires_at"`
	Status           string  `json:"status"`
	CloseReason      *string `json:"close_reason,omitempty"`
}
