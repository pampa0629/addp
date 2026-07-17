package models

import "time"

const (
	SubjectTypeUser               = "user"
	AuthTypeFirstPartyAccessToken = "first_party_access_token"
	AuthTypeOAuthAccessToken      = "oauth_access_token"
	AuthTypeResourceAccessTicket  = "resource_access_ticket"
	AuthTypeDelegatedAccessToken  = "delegated_access_token"
)

// AuthorizationContext is the canonical projection of an authenticated user access token.
type AuthorizationContext struct {
	SubjectType string    `json:"subject_type" example:"user"`
	UserID      uint      `json:"user_id" example:"12"`
	Username    string    `json:"username" example:"alice"`
	UserType    UserType  `json:"user_type" example:"tenant_admin"`
	TenantID    *uint     `json:"tenant_id" example:"3"`
	AuthType    string    `json:"auth_type" example:"first_party_access_token"`
	ClientID    *string   `json:"client_id"`
	Audiences   []string  `json:"audiences"`
	Scopes      []string  `json:"scopes"`
	DelegatedBy *string   `json:"delegated_by"`
	AgentRunID  *string   `json:"agent_run_id"`
	ToolCallID  *string   `json:"tool_call_id"`
	IssuedAt    time.Time `json:"issued_at"`
	ExpiresAt   time.Time `json:"expires_at"`
}
