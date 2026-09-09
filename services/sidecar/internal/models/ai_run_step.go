package models

type AIRunStep struct {
	ID           string  `gorm:"column:id;primaryKey" json:"id"`
	GenerationID string  `gorm:"column:generation_id" json:"generation_id"`
	Sequence     int     `gorm:"column:sequence" json:"sequence"`
	Kind         string  `gorm:"column:kind" json:"kind"`
	Status       string  `gorm:"column:status" json:"status"`
	TurnIndex    *int    `gorm:"column:turn_index" json:"turn_index"`
	ToolName     *string `gorm:"column:tool_name" json:"tool_name"`
	StartedAt    string  `gorm:"column:started_at" json:"started_at"`
	CompletedAt  *string `gorm:"column:completed_at" json:"completed_at"`
	DurationMS   *int64  `gorm:"column:duration_ms" json:"duration_ms"`
	InputBytes   int     `gorm:"column:input_bytes" json:"input_bytes"`
	OutputBytes  int     `gorm:"column:output_bytes" json:"output_bytes"`
	InputTokens  *int    `gorm:"column:input_tokens" json:"input_tokens"`
	OutputTokens *int    `gorm:"column:output_tokens" json:"output_tokens"`
	TokenSource  *string `gorm:"column:token_source" json:"token_source"`
	ErrorCode    *string `gorm:"column:error_code" json:"error_code"`
	CreatedAt    string  `gorm:"column:created_at" json:"created_at"`
}

func (AIRunStep) TableName() string { return "ai_run_steps" }
