package models

const (
	BuiltinOwnerActorID  = "00000000-0000-5000-8000-000000000001"
	BuiltinSystemActorID = "00000000-0000-5000-8000-000000000002"
)

type Actor struct {
	ID           string `gorm:"column:id;primaryKey" json:"id"`
	Type         string `gorm:"column:type" json:"type"`
	DisplayName  string `gorm:"column:display_name" json:"display_name"`
	Status       string `gorm:"column:status" json:"status"`
	IsBuiltin    bool   `gorm:"column:is_builtin" json:"is_builtin"`
	Notes        string `gorm:"column:notes" json:"notes"`
	MetadataJSON string `gorm:"column:metadata_json" json:"-"`
	Version      int64  `gorm:"column:version" json:"version"`
	CreatedAt    string `gorm:"column:created_at" json:"created_at"`
	UpdatedAt    string `gorm:"column:updated_at" json:"updated_at"`
}

func (Actor) TableName() string { return "actors" }

type TaskAssignment struct {
	ID                string  `gorm:"column:id;primaryKey" json:"id"`
	TaskID            string  `gorm:"column:task_id" json:"task_id"`
	ActorID           string  `gorm:"column:actor_id" json:"actor_id"`
	Role              string  `gorm:"column:role" json:"role"`
	AssignedByActorID string  `gorm:"column:assigned_by_actor_id" json:"assigned_by_actor_id"`
	AssignedAt        string  `gorm:"column:assigned_at" json:"assigned_at"`
	UnassignedAt      *string `gorm:"column:unassigned_at" json:"unassigned_at"`
	Reason            string  `gorm:"column:reason" json:"reason"`
}

func (TaskAssignment) TableName() string { return "task_assignments" }

type WorkflowEvent struct {
	ID            string  `gorm:"column:id;primaryKey" json:"id"`
	AggregateType string  `gorm:"column:aggregate_type" json:"aggregate_type"`
	AggregateID   string  `gorm:"column:aggregate_id" json:"aggregate_id"`
	Action        string  `gorm:"column:action" json:"action"`
	ActorID       *string `gorm:"column:actor_id" json:"actor_id"`
	AssignmentID  *string `gorm:"column:assignment_id" json:"assignment_id"`
	SubmissionID  *string `gorm:"column:submission_id" json:"submission_id"`
	ArtifactID    *string `gorm:"column:artifact_id" json:"artifact_id"`
	AgentRunID    *string `gorm:"column:agent_run_id" json:"agent_run_id"`
	RequestID     *string `gorm:"column:request_id" json:"request_id"`
	CommandSeq    *int    `gorm:"column:command_seq" json:"command_seq"`
	PreviousJSON  *string `gorm:"column:previous_json" json:"previous_json"`
	CurrentJSON   *string `gorm:"column:current_json" json:"current_json"`
	CreatedAt     string  `gorm:"column:created_at" json:"created_at"`
}

func (WorkflowEvent) TableName() string { return "workflow_events" }
