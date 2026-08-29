package models

type AgentAdapter struct {
	ID              string  `gorm:"column:id;primaryKey" json:"id"`
	AdapterKey      string  `gorm:"column:adapter_key" json:"adapter_key"`
	Kind            string  `gorm:"column:kind" json:"kind"`
	DisplayName     string  `gorm:"column:display_name" json:"display_name"`
	ExecutableRef   string  `gorm:"column:executable_ref" json:"-"`
	ManifestJSON    string  `gorm:"column:manifest_json" json:"-"`
	ProtocolVersion string  `gorm:"column:protocol_version" json:"protocol_version"`
	Status          string  `gorm:"column:status" json:"status"`
	HealthStatus    string  `gorm:"column:health_status" json:"health_status"`
	HealthErrorCode *string `gorm:"column:health_error_code" json:"health_error_code"`
	IsolationStatus string  `gorm:"column:isolation_status" json:"isolation_status"`
	ExecutionReady  bool    `gorm:"column:execution_ready" json:"execution_ready"`
	LastHealthAt    *string `gorm:"column:last_health_at" json:"last_health_at"`
	Version         int64   `gorm:"column:version" json:"version"`
	CreatedAt       string  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt       string  `gorm:"column:updated_at" json:"updated_at"`
}

func (AgentAdapter) TableName() string { return "agent_adapters" }
