package models

type ContentItem struct {
	ID                 string  `gorm:"column:id;primaryKey" json:"id"`
	Title              string  `gorm:"column:title" json:"title"`
	Platform           string  `gorm:"column:platform" json:"platform"`
	Status             string  `gorm:"column:status" json:"status"`
	ScheduledAt        *string `gorm:"column:scheduled_at" json:"scheduled_at"`
	ScheduledTimezone  *string `gorm:"column:scheduled_timezone" json:"scheduled_timezone"`
	PublishedAt        *string `gorm:"column:published_at" json:"published_at"`
	ProjectID          *string `gorm:"column:project_id" json:"project_id"`
	Notes              *string `gorm:"column:notes" json:"notes"`
	ExternalLink       *string `gorm:"column:external_link" json:"external_link"`
	ManualOrder        int64   `gorm:"column:manual_order" json:"manual_order"`
	ArchivedFromStatus *string `gorm:"column:archived_from_status" json:"archived_from_status"`
	Version            int64   `gorm:"column:version" json:"version"`
	CreatedAt          string  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt          string  `gorm:"column:updated_at" json:"updated_at"`
}

func (ContentItem) TableName() string { return "content_items" }

type ContentItemTask struct {
	ContentItemID string `gorm:"column:content_item_id;primaryKey" json:"content_item_id"`
	TaskID        string `gorm:"column:task_id;primaryKey" json:"task_id"`
	IsRequired    bool   `gorm:"column:is_required" json:"is_required"`
	LinkedAt      string `gorm:"column:linked_at" json:"linked_at"`
}

func (ContentItemTask) TableName() string { return "content_item_tasks" }
