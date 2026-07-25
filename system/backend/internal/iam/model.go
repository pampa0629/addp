package iam

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

type PrincipalType string

const (
	PrincipalTypeUser             PrincipalType = "user"
	PrincipalTypeServicePrincipal PrincipalType = "service_principal"
)

type PrincipalStatus string

const (
	PrincipalStatusActive      PrincipalStatus = "active"
	PrincipalStatusSuspended   PrincipalStatus = "suspended"
	PrincipalStatusDeactivated PrincipalStatus = "deactivated"
)

type LocalAccountStatus string

const (
	LocalAccountStatusActive   LocalAccountStatus = "active"
	LocalAccountStatusLocked   LocalAccountStatus = "locked"
	LocalAccountStatusDisabled LocalAccountStatus = "disabled"
)

type TenantStatus string

const (
	TenantStatusActive    TenantStatus = "active"
	TenantStatusSuspended TenantStatus = "suspended"
	TenantStatusClosed    TenantStatus = "closed"
)

type TenantMembershipStatus string

const (
	TenantMembershipStatusActive    TenantMembershipStatus = "active"
	TenantMembershipStatusSuspended TenantMembershipStatus = "suspended"
	TenantMembershipStatusEnded     TenantMembershipStatus = "ended"
)

type TenantMembershipSource string

const (
	TenantMembershipSourceManual        TenantMembershipSource = "manual"
	TenantMembershipSourceIDPJIT        TenantMembershipSource = "idp_jit"
	TenantMembershipSourceDirectorySync TenantMembershipSource = "directory_sync"
	TenantMembershipSourceBootstrap     TenantMembershipSource = "bootstrap"
)

type ContextType string

const (
	ContextTypePlatform ContextType = "platform"
	ContextTypeTenant   ContextType = "tenant"
)

type AssuranceLevel string

const (
	AssuranceLevelAAL1          AssuranceLevel = "aal1"
	AssuranceLevelAAL2          AssuranceLevel = "aal2"
	AssuranceLevelAAL3          AssuranceLevel = "aal3"
	AssuranceLevelNotApplicable AssuranceLevel = "not_applicable"
)

type AuditResult string

const (
	AuditResultSucceeded AuditResult = "succeeded"
	AuditResultFailed    AuditResult = "failed"
	AuditResultDenied    AuditResult = "denied"
	AuditResultIgnored   AuditResult = "ignored"
)

type AuditRiskLevel string

const (
	AuditRiskLow      AuditRiskLevel = "low"
	AuditRiskMedium   AuditRiskLevel = "medium"
	AuditRiskHigh     AuditRiskLevel = "high"
	AuditRiskCritical AuditRiskLevel = "critical"
)

type Principal struct {
	ID                    int64           `gorm:"primaryKey;autoIncrement"`
	PrincipalType         PrincipalType   `gorm:"column:principal_type;not null"`
	Status                PrincipalStatus `gorm:"column:status;not null;default:active"`
	AuthorizationVersion  int64           `gorm:"column:authorization_version;not null;default:1"`
	DeactivatedAt         *time.Time      `gorm:"column:deactivated_at"`
	StatusChangeRequestID *int64          `gorm:"column:status_change_request_id"`
	CreatedAt             time.Time       `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt             time.Time       `gorm:"column:updated_at;autoUpdateTime"`
}

func (Principal) TableName() string { return "system.principals" }

type User struct {
	ID           int64     `gorm:"primaryKey;autoIncrement:false"`
	DisplayName  string    `gorm:"column:display_name;not null"`
	PrimaryEmail *string   `gorm:"column:primary_email"`
	Locale       *string   `gorm:"column:locale"`
	CreatedAt    time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt    time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (User) TableName() string { return "system.users" }

type LocalAccount struct {
	ID                  int64              `gorm:"primaryKey;autoIncrement"`
	UserID              int64              `gorm:"column:user_id;not null;unique"`
	Username            string             `gorm:"column:username;not null"`
	NormalizedUsername  string             `gorm:"column:normalized_username;not null;unique"`
	PasswordHash        string             `gorm:"column:password_hash;not null"`
	Status              LocalAccountStatus `gorm:"column:status;not null;default:active"`
	LockedUntil         *time.Time         `gorm:"column:locked_until"`
	PasswordChangedAt   time.Time          `gorm:"column:password_changed_at;not null"`
	LastAuthenticatedAt *time.Time         `gorm:"column:last_authenticated_at"`
	CreatedAt           time.Time          `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt           time.Time          `gorm:"column:updated_at;autoUpdateTime"`
}

func (LocalAccount) TableName() string { return "system.local_accounts" }

type Tenant struct {
	ID          int64        `gorm:"primaryKey;autoIncrement"`
	Code        string       `gorm:"column:code;not null;unique"`
	Name        string       `gorm:"column:name;not null"`
	Description string       `gorm:"column:description;not null"`
	Status      TenantStatus `gorm:"column:status;not null;default:active"`
	CreatedAt   time.Time    `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt   time.Time    `gorm:"column:updated_at;autoUpdateTime"`
}

func (Tenant) TableName() string { return "system.tenants" }

type TenantMembership struct {
	ID                   int64                  `gorm:"primaryKey;autoIncrement"`
	TenantID             int64                  `gorm:"column:tenant_id;not null"`
	PrincipalID          int64                  `gorm:"column:principal_id;not null"`
	Status               TenantMembershipStatus `gorm:"column:status;not null;default:active"`
	SourceType           TenantMembershipSource `gorm:"column:source_type;not null"`
	SourceRef            *string                `gorm:"column:source_ref"`
	JoinedAt             time.Time              `gorm:"column:joined_at;not null"`
	ExpiresAt            *time.Time             `gorm:"column:expires_at"`
	EndedAt              *time.Time             `gorm:"column:ended_at"`
	CreatedByPrincipalID *int64                 `gorm:"column:created_by_principal_id"`
	CreatedAt            time.Time              `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt            time.Time              `gorm:"column:updated_at;autoUpdateTime"`
}

func (TenantMembership) TableName() string { return "system.tenant_memberships" }

type AuditLog struct {
	ID            int64           `gorm:"primaryKey;autoIncrement"`
	PrincipalID   *int64          `gorm:"column:principal_id"`
	PrincipalType *PrincipalType  `gorm:"column:principal_type"`
	ContextType   *ContextType    `gorm:"column:context_type"`
	TenantID      *int64          `gorm:"column:tenant_id"`
	EventName     string          `gorm:"column:event_name;not null"`
	Result        AuditResult     `gorm:"column:result;not null"`
	RiskLevel     AuditRiskLevel  `gorm:"column:risk_level;not null"`
	ModuleName    string          `gorm:"column:module_name;not null"`
	HTTPMethod    *string         `gorm:"column:http_method"`
	ResourcePath  *string         `gorm:"column:resource_path"`
	HTTPStatus    *int            `gorm:"column:http_status"`
	RequestID     *string         `gorm:"column:request_id"`
	IPAddress     *string         `gorm:"column:ip_address"`
	UserAgent     *string         `gorm:"column:user_agent"`
	EntityType    *string         `gorm:"column:entity_type"`
	EntityID      *string         `gorm:"column:entity_id"`
	Details       json.RawMessage `gorm:"column:details;type:jsonb;not null"`
	CreatedAt     time.Time       `gorm:"column:created_at;autoCreateTime"`
}

func (AuditLog) TableName() string { return "system.audit_logs" }

type ContextSelectionTicket struct {
	ID                         int64          `gorm:"primaryKey;autoIncrement"`
	TokenHash                  string         `gorm:"column:token_hash;not null;unique"`
	PrincipalID                int64          `gorm:"column:principal_id;not null"`
	IssuedAuthorizationVersion int64          `gorm:"column:issued_authorization_version;not null"`
	ClientID                   string         `gorm:"column:client_id;not null"`
	AuthenticationMethods      pq.StringArray `gorm:"column:authentication_methods;type:text[];not null"`
	AssuranceLevel             AssuranceLevel `gorm:"column:assurance_level;not null"`
	AuthenticatedAt            time.Time      `gorm:"column:authenticated_at;not null"`
	StepUpExpiresAt            *time.Time     `gorm:"column:step_up_expires_at"`
	ExpiresAt                  time.Time      `gorm:"column:expires_at;not null"`
	ConsumedAt                 *time.Time     `gorm:"column:consumed_at"`
	CreatedAt                  time.Time      `gorm:"column:created_at;autoCreateTime"`
}

func (ContextSelectionTicket) TableName() string { return "system.context_selection_tickets" }

type ContextSelectionOption struct {
	ID                 int64       `gorm:"primaryKey;autoIncrement"`
	TicketID           int64       `gorm:"column:ticket_id;not null"`
	ContextType        ContextType `gorm:"column:context_type;not null"`
	TenantMembershipID *int64      `gorm:"column:tenant_membership_id"`
	CreatedAt          time.Time   `gorm:"column:created_at;autoCreateTime"`
}

func (ContextSelectionOption) TableName() string {
	return "system.context_selection_ticket_options"
}

type RefreshTokenFamily struct {
	ID                         int64          `gorm:"primaryKey;autoIncrement"`
	ProtocolRequestID          *uuid.UUID     `gorm:"column:protocol_request_id;type:uuid"`
	PrincipalID                int64          `gorm:"column:principal_id;not null"`
	ContextType                ContextType    `gorm:"column:context_type;not null"`
	TenantMembershipID         *int64         `gorm:"column:tenant_membership_id"`
	IssuedAuthorizationVersion int64          `gorm:"column:issued_authorization_version;not null"`
	ClientID                   string         `gorm:"column:client_id;not null"`
	AuthType                   string         `gorm:"column:auth_type;not null"`
	Audiences                  pq.StringArray `gorm:"column:audiences;type:text[];not null"`
	Scopes                     pq.StringArray `gorm:"column:scopes;type:text[];not null"`
	AuthenticationMethods      pq.StringArray `gorm:"column:authentication_methods;type:text[];not null"`
	AssuranceLevel             AssuranceLevel `gorm:"column:assurance_level;not null"`
	AuthenticatedAt            time.Time      `gorm:"column:authenticated_at;not null"`
	StepUpExpiresAt            *time.Time     `gorm:"column:step_up_expires_at"`
	ExpiresAt                  time.Time      `gorm:"column:expires_at;not null"`
	RevokedAt                  *time.Time     `gorm:"column:revoked_at"`
	RevokedReason              *string        `gorm:"column:revoked_reason"`
	CreatedAt                  time.Time      `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt                  time.Time      `gorm:"column:updated_at;autoUpdateTime"`
}

func (RefreshTokenFamily) TableName() string { return "system.refresh_token_families" }

type AccessToken struct {
	ID        int64      `gorm:"primaryKey;autoIncrement"`
	TokenHash string     `gorm:"column:token_hash;not null;unique"`
	FamilyID  int64      `gorm:"column:family_id;not null"`
	ExpiresAt time.Time  `gorm:"column:expires_at;not null"`
	RevokedAt *time.Time `gorm:"column:revoked_at"`
	CreatedAt time.Time  `gorm:"column:created_at;autoCreateTime"`
}

func (AccessToken) TableName() string { return "system.access_tokens" }

type RefreshToken struct {
	ID                  int64      `gorm:"primaryKey;autoIncrement"`
	TokenHash           string     `gorm:"column:token_hash;not null;unique"`
	FamilyID            int64      `gorm:"column:family_id;not null"`
	IssuedAccessTokenID int64      `gorm:"column:issued_access_token_id;not null;unique"`
	ParentTokenID       *int64     `gorm:"column:parent_token_id"`
	ReplacedByTokenID   *int64     `gorm:"column:replaced_by_token_id"`
	ExpiresAt           time.Time  `gorm:"column:expires_at;not null"`
	UsedAt              *time.Time `gorm:"column:used_at"`
	ReuseDetectedAt     *time.Time `gorm:"column:reuse_detected_at"`
	RevokedAt           *time.Time `gorm:"column:revoked_at"`
	CreatedAt           time.Time  `gorm:"column:created_at;autoCreateTime"`
}

func (RefreshToken) TableName() string { return "system.refresh_tokens" }

type ResourceAccessTicket struct {
	ID        int64      `gorm:"primaryKey;autoIncrement"`
	TokenHash string     `gorm:"column:token_hash;not null;unique"`
	FamilyID  int64      `gorm:"column:family_id;not null"`
	Owner     string     `gorm:"column:owner;not null"`
	ExpiresAt time.Time  `gorm:"column:expires_at;not null"`
	RevokedAt *time.Time `gorm:"column:revoked_at"`
	CreatedAt time.Time  `gorm:"column:created_at;autoCreateTime"`
}

func (ResourceAccessTicket) TableName() string { return "system.resource_access_tickets" }

type DelegatedAccessToken struct {
	ID                  int64          `gorm:"primaryKey;autoIncrement"`
	TokenHash           string         `gorm:"column:token_hash;not null;unique"`
	SourceAccessTokenID int64          `gorm:"column:source_access_token_id;not null"`
	Audience            string         `gorm:"column:audience;not null"`
	Scopes              pq.StringArray `gorm:"column:scopes;type:text[];not null"`
	AgentRunID          string         `gorm:"column:agent_run_id;not null"`
	ToolCallID          string         `gorm:"column:tool_call_id;not null"`
	ExpiresAt           time.Time      `gorm:"column:expires_at;not null"`
	RevokedAt           *time.Time     `gorm:"column:revoked_at"`
	CreatedAt           time.Time      `gorm:"column:created_at;autoCreateTime"`
}

func (DelegatedAccessToken) TableName() string { return "system.delegated_access_tokens" }

type LocalUserIdentity struct {
	PrincipalID          int64
	PrincipalStatus      PrincipalStatus
	AuthorizationVersion int64
	DisplayName          string
	PrimaryEmail         *string
	Locale               *string
	AccountID            int64
	Username             string
	NormalizedUsername   string
	PasswordHash         string
	AccountStatus        LocalAccountStatus
	LockedUntil          *time.Time
	PasswordChangedAt    time.Time
	LastAuthenticatedAt  *time.Time
}

type EffectiveTenantMembership struct {
	MembershipID     int64
	TenantID         int64
	TenantCode       string
	TenantName       string
	MembershipStatus TenantMembershipStatus
	TenantStatus     TenantStatus
	JoinedAt         time.Time
	ExpiresAt        *time.Time
}
