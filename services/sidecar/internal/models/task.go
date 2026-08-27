package models

type Task struct {
	ID               string  `gorm:"column:id;primaryKey" json:"id"`
	Title            string  `gorm:"column:title" json:"title"`
	Description      string  `gorm:"column:description" json:"description"`
	Status           string  `gorm:"column:status" json:"status"`
	Priority         string  `gorm:"column:priority" json:"priority"`
	ProjectID        *string `gorm:"column:project_id" json:"project_id"`
	DueDate          *string `gorm:"column:due_date" json:"due_date"`
	PlannedDate      *string `gorm:"column:planned_date" json:"planned_date"`
	EstimatedMinutes *int    `gorm:"column:estimated_minutes" json:"estimated_minutes"`
	ActualMinutes    int     `gorm:"column:actual_minutes" json:"actual_minutes"`
	ManualOrder      *int    `gorm:"column:manual_order" json:"manual_order"`
	CreatedAt        string  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt        string  `gorm:"column:updated_at" json:"updated_at"`
	CompletedAt      *string `gorm:"column:completed_at" json:"completed_at"`
}

func (Task) TableName() string { return "tasks" }

type IdempotencyKey struct {
	Key        string `gorm:"column:key;primaryKey"`
	Endpoint   string `gorm:"column:endpoint;primaryKey"`
	ResourceID string `gorm:"column:resource_id"`
	CreatedAt  string `gorm:"column:created_at"`
}

func (IdempotencyKey) TableName() string { return "idempotency_keys" }
