package models

type Reminder struct {
	ID                 string  `gorm:"column:id;primaryKey" json:"id"`
	SourceEntityType   string  `gorm:"column:source_entity_type" json:"source_entity_type"`
	SourceEntityID     *string `gorm:"column:source_entity_id" json:"source_entity_id"`
	Title              string  `gorm:"column:title" json:"title"`
	Summary            string  `gorm:"column:summary" json:"summary"`
	Priority           string  `gorm:"column:priority" json:"priority"`
	TriggerAt          string  `gorm:"column:trigger_at" json:"trigger_at"`
	Status             string  `gorm:"column:status" json:"status"`
	SourceEventKey     string  `gorm:"column:source_event_key" json:"source_event_key"`
	CreatedByActorID   string  `gorm:"column:created_by_actor_id" json:"created_by_actor_id"`
	SeriesID           string  `gorm:"column:series_id" json:"series_id"`
	RecurrenceType     string  `gorm:"column:recurrence_type" json:"recurrence_type"`
	RecurrenceInterval int     `gorm:"column:recurrence_interval" json:"recurrence_interval"`
	RecurrenceTimezone string  `gorm:"column:recurrence_timezone" json:"recurrence_timezone"`
	OccurrenceNumber   int64   `gorm:"column:occurrence_number" json:"occurrence_number"`
	FiredAt            *string `gorm:"column:fired_at" json:"fired_at"`
	InboxItemID        *string `gorm:"column:inbox_item_id" json:"inbox_item_id"`
	CancelledByActorID *string `gorm:"column:cancelled_by_actor_id" json:"cancelled_by_actor_id"`
	CancelledAt        *string `gorm:"column:cancelled_at" json:"cancelled_at"`
	CancelReason       *string `gorm:"column:cancel_reason" json:"cancel_reason"`
	Version            int64   `gorm:"column:version" json:"version"`
	CreatedAt          string  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt          string  `gorm:"column:updated_at" json:"updated_at"`
}

func (Reminder) TableName() string { return "reminders" }
