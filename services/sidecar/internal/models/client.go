package models

type Client struct {
	ID          string  `gorm:"column:id;primaryKey" json:"id"`
	Name        string  `gorm:"column:name" json:"name"`
	ContactName *string `gorm:"column:contact_name" json:"contact_name"`
	Email       *string `gorm:"column:email" json:"email"`
	Phone       *string `gorm:"column:phone" json:"phone"`
	Notes       *string `gorm:"column:notes" json:"notes"`
	Status      string  `gorm:"column:status" json:"status"`
	Version     int64   `gorm:"column:version" json:"version"`
	CreatedAt   string  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt   string  `gorm:"column:updated_at" json:"updated_at"`
}

func (Client) TableName() string { return "clients" }
