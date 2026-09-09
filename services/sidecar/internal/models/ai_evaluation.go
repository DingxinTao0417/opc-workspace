package models

type AIEvaluationRun struct {
	ID                       string  `gorm:"column:id;primaryKey" json:"id"`
	ProviderID               string  `gorm:"column:provider_id" json:"provider_id"`
	ProviderNameSnapshot     string  `gorm:"column:provider_name_snapshot" json:"provider_name_snapshot"`
	ProviderModelSnapshot    string  `gorm:"column:provider_model_snapshot" json:"provider_model_snapshot"`
	ProviderProtocolSnapshot string  `gorm:"column:provider_protocol_snapshot" json:"provider_protocol_snapshot"`
	ProviderVersion          int64   `gorm:"column:provider_version" json:"provider_version"`
	DatasetVersion           int     `gorm:"column:dataset_version" json:"dataset_version"`
	SuiteKey                 string  `gorm:"column:suite_key;default:full" json:"suite_key"`
	Status                   string  `gorm:"column:status" json:"status"`
	TotalCases               int     `gorm:"column:total_cases" json:"total_cases"`
	CompletedCases           int     `gorm:"column:completed_cases" json:"completed_cases"`
	PassedCases              int     `gorm:"column:passed_cases" json:"passed_cases"`
	FailedCases              int     `gorm:"column:failed_cases" json:"failed_cases"`
	ErrorCases               int     `gorm:"column:error_cases" json:"error_cases"`
	CurrentCaseID            *string `gorm:"column:current_case_id" json:"current_case_id"`
	CancelRequested          bool    `gorm:"column:cancel_requested" json:"cancel_requested"`
	ErrorCode                *string `gorm:"column:error_code" json:"error_code"`
	StartedAt                *string `gorm:"column:started_at" json:"started_at"`
	CompletedAt              *string `gorm:"column:completed_at" json:"completed_at"`
	CreatedAt                string  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt                string  `gorm:"column:updated_at" json:"updated_at"`
}

func (AIEvaluationRun) TableName() string { return "ai_evaluation_runs" }

type AIEvaluationResult struct {
	ID             string  `gorm:"column:id;primaryKey" json:"id"`
	RunID          string  `gorm:"column:run_id" json:"run_id"`
	Sequence       int     `gorm:"column:sequence" json:"sequence"`
	CaseID         string  `gorm:"column:case_id" json:"case_id"`
	Language       string  `gorm:"column:language" json:"language"`
	Category       string  `gorm:"column:category" json:"category"`
	Status         string  `gorm:"column:status" json:"status"`
	FailureCodes   string  `gorm:"column:failure_codes" json:"-"`
	CitationStatus *string `gorm:"column:citation_status" json:"citation_status"`
	CitationCount  int     `gorm:"column:citation_count" json:"citation_count"`
	DurationMS     int64   `gorm:"column:duration_ms" json:"duration_ms"`
	InputBytes     int     `gorm:"column:input_bytes" json:"input_bytes"`
	OutputBytes    int     `gorm:"column:output_bytes" json:"output_bytes"`
	InputTokens    *int    `gorm:"column:input_tokens" json:"input_tokens"`
	OutputTokens   *int    `gorm:"column:output_tokens" json:"output_tokens"`
	TokenSource    *string `gorm:"column:token_source" json:"token_source"`
	ErrorCode      *string `gorm:"column:error_code" json:"error_code"`
	CreatedAt      string  `gorm:"column:created_at" json:"created_at"`
}

func (AIEvaluationResult) TableName() string { return "ai_evaluation_results" }
