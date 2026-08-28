package models

type AppSetting struct {
	Key              string `gorm:"column:key;primaryKey" json:"key"`
	ValueJSON        string `gorm:"column:value_json" json:"-"`
	SchemaVersion    int    `gorm:"column:schema_version" json:"schema_version"`
	Version          int64  `gorm:"column:version" json:"version"`
	UpdatedByActorID string `gorm:"column:updated_by_actor_id" json:"updated_by_actor_id"`
	UpdatedAt        string `gorm:"column:updated_at" json:"updated_at"`
}

func (AppSetting) TableName() string { return "app_settings" }
