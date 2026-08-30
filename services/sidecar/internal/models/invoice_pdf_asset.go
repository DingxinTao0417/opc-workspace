package models

type InvoicePDFAsset struct {
	ID                   string `gorm:"column:id;primaryKey"`
	InvoiceID            string `gorm:"column:invoice_id"`
	FileName             string `gorm:"column:file_name"`
	RelativePath         string `gorm:"column:relative_path"`
	MimeType             string `gorm:"column:mime_type"`
	SizeBytes            int64  `gorm:"column:size_bytes"`
	SHA256               string `gorm:"column:sha256"`
	GeneratedFromVersion int64  `gorm:"column:generated_from_version"`
	GeneratedAt          string `gorm:"column:generated_at"`
	IntegrityStatus      string `gorm:"column:integrity_status"`
	IntegrityCheckedAt   string `gorm:"column:integrity_checked_at"`
}

func (InvoicePDFAsset) TableName() string { return "invoice_pdf_assets" }
