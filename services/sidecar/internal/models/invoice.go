package models

type Invoice struct {
	ID            string  `gorm:"column:id;primaryKey"`
	InvoiceNumber string  `gorm:"column:invoice_number"`
	ClientID      string  `gorm:"column:client_id"`
	ProjectID     *string `gorm:"column:project_id"`
	AmountMinor   int64   `gorm:"column:amount_minor"`
	Currency      string  `gorm:"column:currency"`
	Status        string  `gorm:"column:status"`
	IssueDate     string  `gorm:"column:issue_date"`
	DueDate       string  `gorm:"column:due_date"`
	PaidDate      *string `gorm:"column:paid_date"`
	Notes         string  `gorm:"column:notes"`
	Version       int64   `gorm:"column:version"`
	CreatedAt     string  `gorm:"column:created_at"`
	UpdatedAt     string  `gorm:"column:updated_at"`
}

func (Invoice) TableName() string { return "invoices" }
