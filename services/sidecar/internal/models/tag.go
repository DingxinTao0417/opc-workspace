package models

type Tag struct {
	ID        string `gorm:"column:id;primaryKey" json:"id"`
	Name      string `gorm:"column:name" json:"name"`
	Color     string `gorm:"column:color" json:"color"`
	Version   int64  `gorm:"column:version" json:"version"`
	CreatedAt string `gorm:"column:created_at" json:"created_at"`
}

func (Tag) TableName() string { return "tags" }
