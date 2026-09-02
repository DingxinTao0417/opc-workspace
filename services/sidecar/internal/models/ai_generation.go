package models

type AIGeneration struct {
	ID         string  `gorm:"column:id;primaryKey" json:"id"`
	SessionID  string  `gorm:"column:session_id" json:"session_id"`
	ProviderID string  `gorm:"column:provider_id" json:"provider_id"`
	Status     string  `gorm:"column:status" json:"status"`
	ErrorCode  *string `gorm:"column:error_code" json:"error_code"`
	Content    *string `gorm:"column:content" json:"content"`
	CreatedAt  string  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt  string  `gorm:"column:updated_at" json:"updated_at"`
}

func (AIGeneration) TableName() string { return "ai_generations" }
