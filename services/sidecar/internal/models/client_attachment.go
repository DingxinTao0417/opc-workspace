package models

type ClientAttachment struct {
	ID                 string  `gorm:"column:id;primaryKey"`
	ClientID           string  `gorm:"column:client_id"`
	ActivityID         *string `gorm:"column:activity_id"`
	Name               string  `gorm:"column:name"`
	RelativePath       string  `gorm:"column:relative_path"`
	MimeType           string  `gorm:"column:mime_type"`
	SizeBytes          int64   `gorm:"column:size_bytes"`
	SHA256             string  `gorm:"column:sha256"`
	RecordedByActorID  string  `gorm:"column:recorded_by_actor_id"`
	IntegrityStatus    string  `gorm:"column:integrity_status"`
	IntegrityCheckedAt string  `gorm:"column:integrity_checked_at"`
	DeletedAt          *string `gorm:"column:deleted_at"`
	DeletedByActorID   *string `gorm:"column:deleted_by_actor_id"`
	DeleteReason       *string `gorm:"column:delete_reason"`
	CreatedAt          string  `gorm:"column:created_at"`
}

func (ClientAttachment) TableName() string { return "client_attachments" }

type ClientAttachmentDeletionTombstone struct {
	AttachmentID  string `gorm:"column:attachment_id;primaryKey"`
	ClientID      string `gorm:"column:client_id"`
	RelativePath  string `gorm:"column:relative_path"`
	SizeBytes     int64  `gorm:"column:size_bytes"`
	SHA256        string `gorm:"column:sha256"`
	DeletionScope string `gorm:"column:deletion_scope"`
	DeletedAt     string `gorm:"column:deleted_at"`
}

func (ClientAttachmentDeletionTombstone) TableName() string {
	return "client_attachment_deletion_tombstones"
}
