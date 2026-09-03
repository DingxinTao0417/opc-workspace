package models

type AIMemory struct {
	ID              string  `gorm:"column:id;primaryKey" json:"id"`
	Content         string  `gorm:"column:content" json:"content"`
	SourceMessageID *string `gorm:"column:source_message_id" json:"source_message_id,omitempty"`
	CreatedAt       string  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt       string  `gorm:"column:updated_at" json:"updated_at"`
}

func (AIMemory) TableName() string { return "ai_memories" }
