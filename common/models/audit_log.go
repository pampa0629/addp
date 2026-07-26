package models

// AuditLogCreateRequest is the single cross-module append-only audit contract.
// IAM bigint identifiers stay decimal strings across the HTTP boundary.
type AuditLogCreateRequest struct {
	PrincipalID   *string        `json:"principal_id,omitempty"`
	PrincipalType *string        `json:"principal_type,omitempty"`
	ContextType   *string        `json:"context_type,omitempty"`
	TenantID      *string        `json:"tenant_id,omitempty"`
	EventName     string         `json:"event_name"`
	Result        string         `json:"result"`
	RiskLevel     string         `json:"risk_level"`
	ModuleName    string         `json:"module_name"`
	HTTPMethod    *string        `json:"http_method,omitempty"`
	ResourcePath  *string        `json:"resource_path,omitempty"`
	HTTPStatus    *int           `json:"http_status,omitempty"`
	RequestID     *string        `json:"request_id,omitempty"`
	IPAddress     *string        `json:"ip_address,omitempty"`
	UserAgent     *string        `json:"user_agent,omitempty"`
	EntityType    string         `json:"entity_type"`
	EntityID      string         `json:"entity_id"`
	Details       map[string]any `json:"details"`
}
