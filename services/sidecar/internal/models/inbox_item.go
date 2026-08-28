package models

type InboxItem struct {
	ID                 string  `gorm:"column:id;primaryKey" json:"id"`
	Kind               string  `gorm:"column:kind" json:"kind"`
	Title              string  `gorm:"column:title" json:"title"`
	Summary            string  `gorm:"column:summary" json:"summary"`
	SourceEntityType   string  `gorm:"column:source_entity_type" json:"source_entity_type"`
	SourceEntityID     *string `gorm:"column:source_entity_id" json:"source_entity_id"`
	SourceEventKey     *string `gorm:"column:source_event_key" json:"source_event_key"`
	SourceDeletedAt    *string `gorm:"column:source_deleted_at" json:"source_deleted_at"`
	Priority           string  `gorm:"column:priority" json:"priority"`
	Status             string  `gorm:"column:status" json:"status"`
	ResolutionPolicy   string  `gorm:"column:resolution_policy" json:"resolution_policy"`
	DueAt              *string `gorm:"column:due_at" json:"due_at"`
	ReadAt             *string `gorm:"column:read_at" json:"read_at"`
	TriagedAt          *string `gorm:"column:triaged_at" json:"triaged_at"`
	SnoozedUntil       *string `gorm:"column:snoozed_until" json:"snoozed_until"`
	ResolvedByActorID  *string `gorm:"column:resolved_by_actor_id" json:"resolved_by_actor_id"`
	ResolvedAt         *string `gorm:"column:resolved_at" json:"resolved_at"`
	ResolutionReason   *string `gorm:"column:resolution_reason" json:"resolution_reason"`
	ResolutionMode     *string `gorm:"column:resolution_mode" json:"resolution_mode"`
	DismissedByActorID *string `gorm:"column:dismissed_by_actor_id" json:"dismissed_by_actor_id"`
	DismissedAt        *string `gorm:"column:dismissed_at" json:"dismissed_at"`
	DismissReason      *string `gorm:"column:dismiss_reason" json:"dismiss_reason"`
	PayloadJSON        string  `gorm:"column:payload_json" json:"-"`
	Version            int64   `gorm:"column:version" json:"version"`
	CreatedAt          string  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt          string  `gorm:"column:updated_at" json:"updated_at"`
}

func (InboxItem) TableName() string { return "inbox_items" }
