package models

type FocusSession struct {
	ID                 string  `gorm:"column:id;primaryKey" json:"id"`
	TaskID             *string `gorm:"column:task_id" json:"task_id"`
	StartedAt          string  `gorm:"column:started_at" json:"started_at"`
	EndedAt            *string `gorm:"column:ended_at" json:"ended_at"`
	Status             string  `gorm:"column:status" json:"status"`
	PlannedSeconds     int64   `gorm:"column:planned_seconds" json:"planned_seconds"`
	AccumulatedSeconds int64   `gorm:"column:accumulated_seconds" json:"accumulated_seconds"`
	LastResumedAt      *string `gorm:"column:last_resumed_at" json:"last_resumed_at"`
	LastHeartbeatAt    *string `gorm:"column:last_heartbeat_at" json:"last_heartbeat_at"`
	EndReason          *string `gorm:"column:end_reason" json:"end_reason"`
	CreditedMinutes    int64   `gorm:"column:credited_minutes" json:"credited_minutes"`
	Version            int64   `gorm:"column:version" json:"version"`
	CreatedAt          string  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt          string  `gorm:"column:updated_at" json:"updated_at"`
}

func (FocusSession) TableName() string { return "focus_sessions" }

type FocusSessionInterval struct {
	ID              int64   `gorm:"column:id;primaryKey"`
	SessionID       string  `gorm:"column:session_id"`
	StartedAt       string  `gorm:"column:started_at"`
	EndedAt         *string `gorm:"column:ended_at"`
	DurationSeconds int64   `gorm:"column:duration_seconds"`
	CreatedAt       string  `gorm:"column:created_at"`
}

func (FocusSessionInterval) TableName() string { return "focus_session_intervals" }

type TaskFocusTotal struct {
	TaskID         string `gorm:"column:task_id;primaryKey"`
	ExactSeconds   int64  `gorm:"column:exact_seconds"`
	AppliedMinutes int64  `gorm:"column:applied_minutes"`
	UpdatedAt      string `gorm:"column:updated_at"`
}

func (TaskFocusTotal) TableName() string { return "task_focus_totals" }
