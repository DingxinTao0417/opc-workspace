package models

type KnowledgeSource struct {
	ID              string  `gorm:"column:id;primaryKey" json:"id"`
	Name            string  `gorm:"column:name" json:"name"`
	Title           string  `gorm:"column:title" json:"title"`
	SourceType      string  `gorm:"column:source_type" json:"source_type"`
	ImportMode      string  `gorm:"column:import_mode" json:"import_mode"`
	MimeType        string  `gorm:"column:mime_type" json:"mime_type"`
	SizeBytes       int64   `gorm:"column:size_bytes" json:"size_bytes"`
	ContentSHA256   string  `gorm:"column:content_sha256" json:"content_sha256"`
	OriginalContent []byte  `gorm:"column:original_content" json:"-"`
	Status          string  `gorm:"column:status" json:"status"`
	LastIndexedAt   *string `gorm:"column:last_indexed_at" json:"last_indexed_at"`
	DeletedAt       *string `gorm:"column:deleted_at" json:"deleted_at"`
	DeleteReason    *string `gorm:"column:delete_reason" json:"delete_reason"`
	Version         int64   `gorm:"column:version" json:"version"`
	CreatedAt       string  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt       string  `gorm:"column:updated_at" json:"updated_at"`
}

func (KnowledgeSource) TableName() string { return "knowledge_sources" }

type KnowledgeDocument struct {
	ID               string `gorm:"column:id;primaryKey" json:"id"`
	SourceID         string `gorm:"column:source_id" json:"source_id"`
	Title            string `gorm:"column:title" json:"title"`
	Language         string `gorm:"column:language" json:"language"`
	ExtractorVersion string `gorm:"column:extractor_version" json:"extractor_version"`
	ContentText      string `gorm:"column:content_text" json:"-"`
	ContentSHA256    string `gorm:"column:content_sha256" json:"content_sha256"`
	Status           string `gorm:"column:status" json:"status"`
	Version          int64  `gorm:"column:version" json:"version"`
	CreatedAt        string `gorm:"column:created_at" json:"created_at"`
	UpdatedAt        string `gorm:"column:updated_at" json:"updated_at"`
}

func (KnowledgeDocument) TableName() string { return "knowledge_documents" }

type KnowledgeChunk struct {
	ID            string `gorm:"column:id;primaryKey" json:"id"`
	DocumentID    string `gorm:"column:document_id" json:"document_id"`
	SourceID      string `gorm:"column:source_id" json:"source_id"`
	ChunkIndex    int    `gorm:"column:chunk_index" json:"chunk_index"`
	StartChar     int    `gorm:"column:start_char" json:"start_char"`
	EndChar       int    `gorm:"column:end_char" json:"end_char"`
	StartLine     int    `gorm:"column:start_line" json:"start_line"`
	EndLine       int    `gorm:"column:end_line" json:"end_line"`
	StartPage     int    `gorm:"column:start_page" json:"start_page"`
	EndPage       int    `gorm:"column:end_page" json:"end_page"`
	Content       string `gorm:"column:content" json:"content"`
	SearchText    string `gorm:"column:search_text" json:"-"`
	ContentSHA256 string `gorm:"column:content_sha256" json:"content_sha256"`
	IndexVersion  int64  `gorm:"column:index_version" json:"index_version"`
	CreatedAt     string `gorm:"column:created_at" json:"created_at"`
}

func (KnowledgeChunk) TableName() string { return "knowledge_chunks" }

type KnowledgeIndexJob struct {
	ID              string  `gorm:"column:id;primaryKey" json:"id"`
	SourceID        string  `gorm:"column:source_id" json:"source_id"`
	Operation       string  `gorm:"column:operation" json:"operation"`
	Status          string  `gorm:"column:status" json:"status"`
	Stage           string  `gorm:"column:stage" json:"stage"`
	Progress        int     `gorm:"column:progress" json:"progress"`
	Attempt         int     `gorm:"column:attempt" json:"attempt"`
	RetryOfJobID    *string `gorm:"column:retry_of_job_id" json:"retry_of_job_id"`
	ErrorCode       *string `gorm:"column:error_code" json:"error_code"`
	CancelRequested bool    `gorm:"column:cancel_requested" json:"cancel_requested"`
	StartedAt       *string `gorm:"column:started_at" json:"started_at"`
	CompletedAt     *string `gorm:"column:completed_at" json:"completed_at"`
	CreatedAt       string  `gorm:"column:created_at" json:"created_at"`
}

func (KnowledgeIndexJob) TableName() string { return "knowledge_index_jobs" }
