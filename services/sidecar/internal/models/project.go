package models

type Project struct {
	ID                 string  `gorm:"column:id;primaryKey" json:"id"`
	Name               string  `gorm:"column:name" json:"name"`
	Description        string  `gorm:"column:description" json:"description"`
	ClientID           *string `gorm:"column:client_id" json:"client_id"`
	Status             string  `gorm:"column:status" json:"status"`
	StartDate          *string `gorm:"column:start_date" json:"start_date"`
	DueDate            *string `gorm:"column:due_date" json:"due_date"`
	AmountMinor        *int64  `gorm:"column:amount_minor" json:"amount_minor"`
	Color              *string `gorm:"column:color" json:"color"`
	Version            int64   `gorm:"column:version" json:"version"`
	ArchivedFromStatus *string `gorm:"column:archived_from_status" json:"-"`
	CreatedAt          string  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt          string  `gorm:"column:updated_at" json:"updated_at"`
}

func (Project) TableName() string { return "projects" }
