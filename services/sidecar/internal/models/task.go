package models

type Task struct {
	ID                 string  `gorm:"column:id;primaryKey" json:"id"`
	Title              string  `gorm:"column:title" json:"title"`
	Description        string  `gorm:"column:description" json:"description"`
	Kind               string  `gorm:"column:kind" json:"kind"`
	Status             string  `gorm:"column:status" json:"status"`
	ReviewPolicy       string  `gorm:"column:review_policy" json:"review_policy"`
	Priority           string  `gorm:"column:priority" json:"priority"`
	ProjectID          *string `gorm:"column:project_id" json:"project_id"`
	ProjectName        *string `gorm:"column:project_name;->" json:"project_name,omitempty"`
	ParentTaskID       *string `gorm:"column:parent_task_id" json:"parent_task_id"`
	ParentTaskTitle    *string `gorm:"column:parent_task_title;->" json:"parent_task_title,omitempty"`
	CompletionCriteria string  `gorm:"column:completion_criteria" json:"completion_criteria"`
	DueDate            *string `gorm:"column:due_date" json:"due_date"`
	PlannedDate        *string `gorm:"column:planned_date" json:"planned_date"`
	EstimatedMinutes   *int    `gorm:"column:estimated_minutes" json:"estimated_minutes"`
	ActualMinutes      int     `gorm:"column:actual_minutes" json:"actual_minutes"`
	ManualOrder        *int    `gorm:"column:manual_order" json:"manual_order"`
	Version            int64   `gorm:"column:version" json:"version"`
	SubtaskTotal       int64   `gorm:"column:subtask_total;->" json:"subtask_total"`
	SubtaskCompleted   int64   `gorm:"column:subtask_completed;->" json:"subtask_completed"`
	Tags               []Tag   `gorm:"-" json:"tags"`
	CreatedAt          string  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt          string  `gorm:"column:updated_at" json:"updated_at"`
	CompletedAt        *string `gorm:"column:completed_at" json:"completed_at"`
	BlockedReason      *string `gorm:"column:blocked_reason" json:"blocked_reason"`
	BlockedAt          *string `gorm:"column:blocked_at" json:"blocked_at"`
	BlockedFromStatus  *string `gorm:"column:blocked_from_status" json:"blocked_from_status"`
	SubmittedAt        *string `gorm:"column:submitted_at" json:"submitted_at"`
	ReviewedAt         *string `gorm:"column:reviewed_at" json:"reviewed_at"`
}

func (Task) TableName() string { return "tasks" }

type IdempotencyKey struct {
	Key            string  `gorm:"column:key;primaryKey"`
	Endpoint       string  `gorm:"column:endpoint;primaryKey"`
	ResourceID     string  `gorm:"column:resource_id"`
	RequestHash    *string `gorm:"column:request_hash"`
	ResponseBody   *string `gorm:"column:response_body"`
	ResponseStatus *int    `gorm:"column:response_status"`
	CreatedAt      string  `gorm:"column:created_at"`
}

func (IdempotencyKey) TableName() string { return "idempotency_keys" }
