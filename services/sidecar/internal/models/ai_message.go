package models

type AIMessage struct {
	ID                string  `gorm:"column:id;primaryKey" json:"id"`
	SessionID         string  `gorm:"column:session_id" json:"session_id"`
	Role              string  `gorm:"column:role" json:"role"`
	Status            string  `gorm:"column:status" json:"status"`
	Content           string  `gorm:"column:content" json:"content"`
	Reasoning         *string `gorm:"column:reasoning" json:"reasoning"`
	ModelSnapshot     *string `gorm:"column:model_snapshot" json:"-"`
	TaskID            *string `gorm:"column:task_id" json:"task_id"`
	TaskTitleSnapshot *string `gorm:"column:task_title_snapshot" json:"task_title_snapshot"`
	CreatedAt         string  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt         string  `gorm:"column:updated_at" json:"updated_at"`
}

func (AIMessage) TableName() string { return "ai_messages" }
