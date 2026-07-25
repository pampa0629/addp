package models

import (
	"time"

	systemauthorization "github.com/addp/system/internal/authorization"
	"github.com/lib/pq"
)

type DelegatedAccessToken struct {
	ID                  string         `gorm:"primaryKey;type:uuid" json:"id"`
	TokenHash           string         `gorm:"type:char(64);not null;uniqueIndex" json:"-"`
	SourceAccessTokenID string         `gorm:"type:uuid;not null;index" json:"source_access_token_id"`
	UserID              uint           `gorm:"not null;index" json:"user_id"`
	TenantID            *uint          `gorm:"index" json:"tenant_id"`
	ClientID            *string        `gorm:"type:varchar(100);index" json:"client_id"`
	DelegatedBy         string         `gorm:"type:varchar(100);not null;index" json:"delegated_by"`
	Audience            string         `gorm:"type:varchar(100);not null;index" json:"audience"`
	Scopes              pq.StringArray `gorm:"type:text[];not null" json:"scopes"`
	AgentRunID          string         `gorm:"type:varchar(100);not null;index" json:"agent_run_id"`
	ToolCallID          string         `gorm:"type:varchar(100);not null;index" json:"tool_call_id"`
	ExpiresAt           time.Time      `gorm:"not null;index" json:"expires_at"`
	RevokedAt           *time.Time     `gorm:"index" json:"revoked_at"`
	CreatedAt           time.Time      `gorm:"autoCreateTime" json:"created_at"`
}

func (DelegatedAccessToken) TableName() string { return "system.delegated_access_tokens" }

type DelegatedAccessTokenRequest struct {
	Audience   string   `json:"audience" binding:"required"`
	Scopes     []string `json:"scopes" binding:"required,min=1"`
	AgentRunID string   `json:"agent_run_id" binding:"required,max=100"`
	ToolCallID string   `json:"tool_call_id" binding:"required,max=100"`
}

type DelegatedAccessTokenResponse struct {
	AccessToken string   `json:"access_token"`
	TokenType   string   `json:"token_type" example:"Bearer"`
	ExpiresIn   int      `json:"expires_in" example:"120"`
	Audience    string   `json:"audience" example:"develop"`
	Scopes      []string `json:"scopes" example:"workflow.run"`
	AgentRunID  string   `json:"agent_run_id" example:"7a9f43a7-81f0-4cb4-b545-6bfef53ed922"`
	ToolCallID  string   `json:"tool_call_id" example:"call_abc123"`
}

func IsDelegatedToolScopeAllowed(audience, scope string) bool {
	tool, ok := (systemauthorization.ToolAuthorizationCatalog{}).FindToolAuthorization(scope)
	return ok && tool.Owner == audience && len(tool.RequiredScopes) == 1 && tool.RequiredScopes[0] == scope
}
