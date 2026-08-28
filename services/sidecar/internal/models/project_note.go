package models

type ProjectNote struct {
	ID               string  `gorm:"column:id;primaryKey" json:"id"`
	ProjectID        string  `gorm:"column:project_id" json:"project_id"`
	Title            string  `gorm:"column:title" json:"title"`
	Body             string  `gorm:"column:body" json:"body"`
	OccurredAt       string  `gorm:"column:occurred_at" json:"occurred_at"`
	CreatedByActorID string  `gorm:"column:created_by_actor_id" json:"created_by_actor_id"`
	Version          int64   `gorm:"column:version" json:"version"`
	DeletedAt        *string `gorm:"column:deleted_at" json:"deleted_at"`
	DeletedByActorID *string `gorm:"column:deleted_by_actor_id" json:"deleted_by_actor_id"`
	DeleteReason     *string `gorm:"column:delete_reason" json:"delete_reason"`
	CreatedAt        string  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt        string  `gorm:"column:updated_at" json:"updated_at"`
}

func (ProjectNote) TableName() string { return "project_notes" }
