package models

type InboxItemTask struct {
	ID                string  `gorm:"column:id;primaryKey" json:"id"`
	InboxItemID       string  `gorm:"column:inbox_item_id" json:"inbox_item_id"`
	TaskRefID         string  `gorm:"column:task_ref_id" json:"task_ref_id"`
	TaskID            *string `gorm:"column:task_id" json:"task_id"`
	TaskTitleSnapshot string  `gorm:"column:task_title_snapshot" json:"task_title_snapshot"`
	RelationType      string  `gorm:"column:relation_type" json:"relation_type"`
	IsRequired        bool    `gorm:"column:is_required" json:"is_required"`
	Position          int     `gorm:"column:position" json:"position"`
	LinkedByActorID   string  `gorm:"column:linked_by_actor_id" json:"linked_by_actor_id"`
	LinkedAt          string  `gorm:"column:linked_at" json:"linked_at"`
	UnlinkedByActorID *string `gorm:"column:unlinked_by_actor_id" json:"unlinked_by_actor_id"`
	UnlinkedAt        *string `gorm:"column:unlinked_at" json:"unlinked_at"`
	UnlinkReason      *string `gorm:"column:unlink_reason" json:"unlink_reason"`
}

func (InboxItemTask) TableName() string { return "inbox_item_tasks" }
