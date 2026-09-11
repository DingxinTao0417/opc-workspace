package models

type AIMemoryEntry struct {
	ID                  string  `gorm:"column:id;primaryKey" json:"id"`
	SessionID           string  `gorm:"column:session_id" json:"session_id"`
	Kind                string  `gorm:"column:kind" json:"kind"`
	Content             string  `gorm:"column:content" json:"content"`
	Tags                string  `gorm:"column:tags" json:"tags"`
	Origin              string  `gorm:"column:origin" json:"origin"`
	Status              string  `gorm:"column:status" json:"status"`
	SourceMessageID     *string `gorm:"column:source_message_id" json:"source_message_id"`
	SourceMessageOffset int     `gorm:"column:source_message_offset" json:"source_message_offset"`
	Decision            *string `gorm:"column:decision" json:"decision"`
	MemoryID            *string `gorm:"column:memory_id" json:"memory_id"`
	CreatedAt           string  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt           string  `gorm:"column:updated_at" json:"updated_at"`
}

func (AIMemoryEntry) TableName() string { return "ai_memory_entries" }
