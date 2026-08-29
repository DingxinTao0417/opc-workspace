package models

type ClientFollowup struct {
	ID                string  `gorm:"column:id;primaryKey" json:"id"`
	ClientID          string  `gorm:"column:client_id" json:"client_id"`
	AssignedActorID   string  `gorm:"column:assigned_actor_id" json:"assigned_actor_id"`
	ScheduledAt       string  `gorm:"column:scheduled_at" json:"scheduled_at"`
	Timezone          string  `gorm:"column:timezone" json:"timezone"`
	Channel           string  `gorm:"column:channel" json:"channel"`
	Purpose           string  `gorm:"column:purpose" json:"purpose"`
	Notes             *string `gorm:"column:notes" json:"notes"`
	Status            string  `gorm:"column:status" json:"status"`
	Priority          string  `gorm:"column:priority" json:"priority"`
	CompletedAt       *string `gorm:"column:completed_at" json:"completed_at"`
	Result            *string `gorm:"column:result" json:"result"`
	NextStep          *string `gorm:"column:next_step" json:"next_step"`
	SkippedAt         *string `gorm:"column:skipped_at" json:"skipped_at"`
	SkipReason        *string `gorm:"column:skip_reason" json:"skip_reason"`
	CancelledAt       *string `gorm:"column:cancelled_at" json:"cancelled_at"`
	CancelReason      *string `gorm:"column:cancel_reason" json:"cancel_reason"`
	RescheduledFromID *string `gorm:"column:rescheduled_from_id" json:"rescheduled_from_id"`
	Version           int64   `gorm:"column:version" json:"version"`
	CreatedAt         string  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt         string  `gorm:"column:updated_at" json:"updated_at"`
}

func (ClientFollowup) TableName() string { return "client_followups" }
