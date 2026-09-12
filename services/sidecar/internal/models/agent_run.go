package models

type AgentRun struct {
	ID                string  `gorm:"column:id;primaryKey" json:"id"`
	TaskID            string  `gorm:"column:task_id" json:"task_id"`
	AssignmentID      string  `gorm:"column:assignment_id" json:"assignment_id"`
	ActorID           string  `gorm:"column:actor_id" json:"actor_id"`
	AdapterID         string  `gorm:"column:adapter_id" json:"adapter_id"`
	CreatedByActorID  string  `gorm:"column:created_by_actor_id" json:"created_by_actor_id"`
	ParentRunID       *string `gorm:"column:parent_run_id" json:"parent_run_id"`
	Attempt           int     `gorm:"column:attempt" json:"attempt"`
	Status            string  `gorm:"column:status" json:"status"`
	ProviderID        string  `gorm:"column:provider_id" json:"provider_id"`
	Model             string  `gorm:"column:model" json:"model"`
	InputSnapshotJSON string  `gorm:"column:input_snapshot_json" json:"-"`
	ResultText        *string `gorm:"column:result_text" json:"result_text"`
	ResultBytes       *int    `gorm:"column:result_bytes" json:"result_bytes"`
	ErrorCode         *string `gorm:"column:error_code" json:"error_code"`
	StartedAt         *string `gorm:"column:started_at" json:"started_at"`
	CompletedAt       *string `gorm:"column:completed_at" json:"completed_at"`
	CreatedAt         string  `gorm:"column:created_at" json:"created_at"`
}

func (AgentRun) TableName() string { return "agent_runs" }
