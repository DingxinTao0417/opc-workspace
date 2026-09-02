package models

type AIProvider struct {
	ID              string  `gorm:"column:id;primaryKey" json:"id"`
	Name            string  `gorm:"column:name" json:"name"`
	Protocol        string  `gorm:"column:protocol" json:"protocol"`
	BaseURL         string  `gorm:"column:base_url" json:"base_url"`
	Model           string  `gorm:"column:model" json:"model"`
	Status          string  `gorm:"column:status" json:"status"`
	HealthStatus    string  `gorm:"column:health_status" json:"health_status"`
	HealthErrorCode *string `gorm:"column:health_error_code" json:"health_error_code"`
	HasKey          bool    `gorm:"column:has_key" json:"has_key"`
	LastHealthAt    *string `gorm:"column:last_health_at" json:"last_health_at"`
	Version         int64   `gorm:"column:version" json:"version"`
	CreatedAt       string  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt       string  `gorm:"column:updated_at" json:"updated_at"`
}

func (AIProvider) TableName() string { return "ai_providers" }
