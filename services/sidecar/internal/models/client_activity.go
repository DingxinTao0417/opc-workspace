package models

type ClientActivity struct {
	ID               string  `gorm:"column:id;primaryKey" json:"id"`
	ClientID         string  `gorm:"column:client_id" json:"client_id"`
	Kind             string  `gorm:"column:kind" json:"kind"`
	Title            string  `gorm:"column:title" json:"title"`
	Body             *string `gorm:"column:body" json:"body"`
	OccurredAt       string  `gorm:"column:occurred_at" json:"occurred_at"`
	CreatedByActorID string  `gorm:"column:created_by_actor_id" json:"created_by_actor_id"`
	SourceType       *string `gorm:"column:source_type" json:"source_type"`
	SourceID         *string `gorm:"column:source_id" json:"source_id"`
	Version          int64   `gorm:"column:version" json:"version"`
	DeletedAt        *string `gorm:"column:deleted_at" json:"deleted_at"`
	DeletedByActorID *string `gorm:"column:deleted_by_actor_id" json:"deleted_by_actor_id"`
	DeleteReason     *string `gorm:"column:delete_reason" json:"delete_reason"`
	CreatedAt        string  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt        string  `gorm:"column:updated_at" json:"updated_at"`
}

func (ClientActivity) TableName() string { return "client_activities" }
