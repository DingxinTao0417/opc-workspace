package models

// Action proposals contain explicitly disclosed approval data, not run logs.
type AIActionProposal struct {
	ID            string  `gorm:"column:id;primaryKey"`
	GenerationID  string  `gorm:"column:generation_id"`
	Fingerprint   string  `gorm:"column:fingerprint"`
	ActionJSON    string  `gorm:"column:action_json"`
	PreviewJSON   string  `gorm:"column:preview_json"`
	Status        string  `gorm:"column:status"`
	ResultID      *string `gorm:"column:result_id"`
	ResultVersion *int64  `gorm:"column:result_version"`
	CreatedAt     string  `gorm:"column:created_at"`
	DecidedAt     *string `gorm:"column:decided_at"`
}

func (AIActionProposal) TableName() string { return "ai_action_proposals" }
