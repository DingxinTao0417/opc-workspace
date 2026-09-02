package models

type AISession struct {
	ID        string `gorm:"column:id;primaryKey" json:"id"`
	Title     string `gorm:"column:title" json:"title"`
	Persist   bool   `gorm:"column:persist" json:"persist"`
	Version   int64  `gorm:"column:version" json:"version"`
	CreatedAt string `gorm:"column:created_at" json:"created_at"`
	UpdatedAt string `gorm:"column:updated_at" json:"updated_at"`
}

func (AISession) TableName() string { return "ai_sessions" }
