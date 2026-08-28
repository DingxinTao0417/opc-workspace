package models

type TaskSavedView struct {
	ID             string `gorm:"column:id;primaryKey" json:"id"`
	Name           string `gorm:"column:name" json:"name"`
	DefinitionJSON string `gorm:"column:definition_json" json:"-"`
	SchemaVersion  int    `gorm:"column:schema_version" json:"schema_version"`
	Version        int64  `gorm:"column:version" json:"version"`
	CreatedAt      string `gorm:"column:created_at" json:"created_at"`
	UpdatedAt      string `gorm:"column:updated_at" json:"updated_at"`
}

func (TaskSavedView) TableName() string { return "task_saved_views" }
