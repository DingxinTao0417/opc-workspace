package models

type AIWorkspaceAccessRequest struct {
	GenerationID string `gorm:"column:generation_id;primaryKey" json:"generation_id"`
	ScopesJSON   string `gorm:"column:scopes_json" json:"-"`
	Status       string `gorm:"column:status" json:"status"`
	CreatedAt    string `gorm:"column:created_at" json:"created_at"`
	UpdatedAt    string `gorm:"column:updated_at" json:"updated_at"`
}

func (AIWorkspaceAccessRequest) TableName() string { return "ai_workspace_access_requests" }
