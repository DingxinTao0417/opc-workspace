package models

type FinancialEntry struct {
	ID               string  `gorm:"column:id;primaryKey"`
	Type             string  `gorm:"column:type"`
	AmountMinor      int64   `gorm:"column:amount_minor"`
	Currency         string  `gorm:"column:currency"`
	OccurredOn       string  `gorm:"column:occurred_on"`
	Status           string  `gorm:"column:status"`
	Category         string  `gorm:"column:category"`
	ClientID         *string `gorm:"column:client_id"`
	ProjectID        *string `gorm:"column:project_id"`
	InvoiceID        *string `gorm:"column:invoice_id"`
	Notes            string  `gorm:"column:notes"`
	CreatedByActorID string  `gorm:"column:created_by_actor_id"`
	VoidedAt         *string `gorm:"column:voided_at"`
	VoidedByActorID  *string `gorm:"column:voided_by_actor_id"`
	VoidReason       *string `gorm:"column:void_reason"`
	Version          int64   `gorm:"column:version"`
	CreatedAt        string  `gorm:"column:created_at"`
	UpdatedAt        string  `gorm:"column:updated_at"`
}

func (FinancialEntry) TableName() string { return "financial_entries" }
