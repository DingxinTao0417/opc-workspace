package models

type ProjectAttachment struct {
	ID                 string  `gorm:"column:id;primaryKey"`
	ProjectID          string  `gorm:"column:project_id"`
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

func (ProjectAttachment) TableName() string { return "project_attachments" }

type ProjectAttachmentDeletionTombstone struct {
	AttachmentID  string `gorm:"column:attachment_id;primaryKey"`
	ProjectID     string `gorm:"column:project_id"`
	RelativePath  string `gorm:"column:relative_path"`
	SizeBytes     int64  `gorm:"column:size_bytes"`
	SHA256        string `gorm:"column:sha256"`
	DeletionScope string `gorm:"column:deletion_scope"`
	DeletedAt     string `gorm:"column:deleted_at"`
}

func (ProjectAttachmentDeletionTombstone) TableName() string {
	return "project_attachment_deletion_tombstones"
}
