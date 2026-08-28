package models

type TaskSubmission struct {
	ID                 string  `gorm:"column:id;primaryKey" json:"id"`
	TaskID             string  `gorm:"column:task_id" json:"task_id"`
	Sequence           int     `gorm:"column:sequence" json:"sequence"`
	Status             string  `gorm:"column:status" json:"status"`
	Summary            string  `gorm:"column:summary" json:"summary"`
	SubmittedByActorID string  `gorm:"column:submitted_by_actor_id" json:"submitted_by_actor_id"`
	SubmittedAt        string  `gorm:"column:submitted_at" json:"submitted_at"`
	ReviewedByActorID  *string `gorm:"column:reviewed_by_actor_id" json:"reviewed_by_actor_id"`
	ReviewedAt         *string `gorm:"column:reviewed_at" json:"reviewed_at"`
	ReviewReason       *string `gorm:"column:review_reason" json:"review_reason"`
	WithdrawnByActorID *string `gorm:"column:withdrawn_by_actor_id" json:"withdrawn_by_actor_id"`
	WithdrawnAt        *string `gorm:"column:withdrawn_at" json:"withdrawn_at"`
	IsInferred         bool    `gorm:"column:is_inferred" json:"is_inferred"`
}

func (TaskSubmission) TableName() string { return "task_submissions" }

type TaskArtifact struct {
	ID                 string  `gorm:"column:id;primaryKey" json:"id"`
	TaskID             string  `gorm:"column:task_id" json:"task_id"`
	SubmissionID       string  `gorm:"column:submission_id" json:"submission_id"`
	Position           int     `gorm:"column:position" json:"position"`
	StorageKind        string  `gorm:"column:storage_kind" json:"storage_kind"`
	Name               string  `gorm:"column:name" json:"name"`
	ContentText        *string `gorm:"column:content_text" json:"-"`
	ReferenceURL       *string `gorm:"column:reference_url" json:"-"`
	StructuredJSON     *string `gorm:"column:structured_json" json:"-"`
	RelativePath       *string `gorm:"column:relative_path" json:"-"`
	MimeType           *string `gorm:"column:mime_type" json:"mime_type"`
	SizeBytes          *int64  `gorm:"column:size_bytes" json:"size_bytes"`
	SHA256             *string `gorm:"column:sha256" json:"sha256"`
	RequiresFollowup   bool    `gorm:"column:requires_followup" json:"requires_followup"`
	ProducedByActorID  string  `gorm:"column:produced_by_actor_id" json:"produced_by_actor_id"`
	RecordedByActorID  string  `gorm:"column:recorded_by_actor_id" json:"recorded_by_actor_id"`
	IntegrityStatus    string  `gorm:"column:integrity_status" json:"integrity_status"`
	IntegrityCheckedAt *string `gorm:"column:integrity_checked_at" json:"integrity_checked_at"`
	DeletedAt          *string `gorm:"column:deleted_at" json:"deleted_at"`
	DeletedByActorID   *string `gorm:"column:deleted_by_actor_id" json:"deleted_by_actor_id"`
	DeleteReason       *string `gorm:"column:delete_reason" json:"delete_reason"`
	CreatedAt          string  `gorm:"column:created_at" json:"created_at"`
}

func (TaskArtifact) TableName() string { return "task_artifacts" }

type ArtifactDeletionTombstone struct {
	ArtifactID    string `gorm:"column:artifact_id;primaryKey"`
	TaskID        string `gorm:"column:task_id"`
	RelativePath  string `gorm:"column:relative_path"`
	SizeBytes     int64  `gorm:"column:size_bytes"`
	SHA256        string `gorm:"column:sha256"`
	DeletionScope string `gorm:"column:deletion_scope"`
	DeletedAt     string `gorm:"column:deleted_at"`
}

func (ArtifactDeletionTombstone) TableName() string { return "artifact_deletion_tombstones" }
