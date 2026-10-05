package filestore

// workspaceAvatarRow is the stored record of one workspace avatar file. The
// file store reconciles these rows against the files on disk.
type WorkspaceAvatarRow struct {
	ID                 string  `gorm:"column:id"`
	RelativePath       string  `gorm:"column:relative_path"`
	Extension          string  `gorm:"column:extension"`
	MimeType           string  `gorm:"column:mime_type"`
	SizeBytes          int64   `gorm:"column:size_bytes"`
	SHA256             string  `gorm:"column:sha256"`
	IntegrityStatus    string  `gorm:"column:integrity_status"`
	IntegrityCheckedAt string  `gorm:"column:integrity_checked_at"`
	CreatedAt          string  `gorm:"column:created_at"`
	DeletedAt          *string `gorm:"column:deleted_at"`
}

func (WorkspaceAvatarRow) TableName() string { return "workspace_avatars" }
