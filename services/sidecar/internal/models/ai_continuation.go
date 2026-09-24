package models

// AIContinuation is an explicit, bounded authorization lease. WorkspaceJSON is
// operational authorization state, never an API response or portable export.
type AIContinuation struct {
	ID                    string  `gorm:"column:id;primaryKey" json:"id"`
	SessionID             string  `gorm:"column:session_id" json:"session_id"`
	ProviderID            string  `gorm:"column:provider_id" json:"provider_id"`
	ProviderVersion       int64   `gorm:"column:provider_version" json:"provider_version"`
	ProviderConfigVersion int64   `gorm:"column:provider_config_version" json:"provider_config_version"`
	ProviderName          string  `gorm:"column:provider_name" json:"provider_name"`
	ProviderKind          string  `gorm:"column:provider_kind" json:"provider_kind"`
	ProviderProtocol      string  `gorm:"column:provider_protocol" json:"provider_protocol"`
	ProviderModel         string  `gorm:"column:provider_model" json:"provider_model"`
	WorkspaceJSON         string  `gorm:"column:workspace_json" json:"-"`
	InitialPlanVersion    int64   `gorm:"column:initial_plan_version" json:"initial_plan_version"`
	CurrentPlanVersion    int64   `gorm:"column:current_plan_version" json:"current_plan_version"`
	LastObservationHash   string  `gorm:"column:last_observation_hash" json:"last_observation_hash"`
	MaxTurns              int     `gorm:"column:max_turns" json:"max_turns"`
	TurnsStarted          int     `gorm:"column:turns_started" json:"turns_started"`
	Status                string  `gorm:"column:status" json:"status"`
	Reason                string  `gorm:"column:reason" json:"reason"`
	Version               int64   `gorm:"column:version" json:"version"`
	CurrentGenerationID   *string `gorm:"column:current_generation_id" json:"current_generation_id"`
	LastGenerationID      *string `gorm:"column:last_generation_id" json:"last_generation_id"`
	CreatedAt             string  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt             string  `gorm:"column:updated_at" json:"updated_at"`
	ExpiresAt             string  `gorm:"column:expires_at" json:"expires_at"`
}

func (AIContinuation) TableName() string { return "ai_continuations" }

// AIContinuationTurn links exactly one accepted turn to the authoritative
// generation. The ledger stores neither a second status nor model content.
type AIContinuationTurn struct {
	ContinuationID  string `gorm:"column:continuation_id;primaryKey" json:"continuation_id"`
	TurnIndex       int    `gorm:"column:turn_index;primaryKey;autoIncrement:false" json:"turn_index"`
	GenerationID    string `gorm:"column:generation_id" json:"generation_id"`
	PlanVersion     int64  `gorm:"column:plan_version" json:"plan_version"`
	ObservationHash string `gorm:"column:observation_hash" json:"observation_hash"`
	CreatedAt       string `gorm:"column:created_at" json:"created_at"`
}

func (AIContinuationTurn) TableName() string { return "ai_continuation_turns" }
