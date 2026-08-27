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

type MFACredentialStatus string

const (
	MFACredentialStatusActive   MFACredentialStatus = "active"
	MFACredentialStatusDisabled MFACredentialStatus = "disabled"
)

type IAMBootstrapStatus string

const (
	IAMBootstrapStatusPrepared  IAMBootstrapStatus = "prepared"
	IAMBootstrapStatusCompleted IAMBootstrapStatus = "completed"
)

type IAMRecoveryStatus string

const (
	IAMRecoveryStatusPrepared  IAMRecoveryStatus = "prepared"
	IAMRecoveryStatusCompleted IAMRecoveryStatus = "completed"
	IAMRecoveryStatusExpired   IAMRecoveryStatus = "expired"
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
	TenantMembershipSourceInvitation    TenantMembershipSource = "invitation"
	TenantMembershipSourceIDPJIT        TenantMembershipSource = "idp_jit"
	TenantMembershipSourceDirectorySync TenantMembershipSource = "directory_sync"
	TenantMembershipSourceBootstrap     TenantMembershipSource = "bootstrap"
)

type DepartmentStatus string

const (
	DepartmentStatusActive   DepartmentStatus = "active"
	DepartmentStatusDisabled DepartmentStatus = "disabled"
)

type OrganizationMembershipStatus string

const (
	OrganizationMembershipStatusActive OrganizationMembershipStatus = "active"
	OrganizationMembershipStatusEnded  OrganizationMembershipStatus = "ended"
)

type DepartmentMembershipType string

const (
	DepartmentMembershipTypePrimary    DepartmentMembershipType = "primary"
	DepartmentMembershipTypeAdditional DepartmentMembershipType = "additional"
)

type DepartmentRelationRole string

const (
	DepartmentRelationRoleMember DepartmentRelationRole = "member"
	DepartmentRelationRoleLeader DepartmentRelationRole = "leader"
)

type ProjectGroupStatus string

const (
	ProjectGroupStatusPlanned ProjectGroupStatus = "planned"
	ProjectGroupStatusActive  ProjectGroupStatus = "active"
	ProjectGroupStatusClosed  ProjectGroupStatus = "closed"
)

type ProjectGroupRelationRole string

const (
	ProjectGroupRelationRoleMember      ProjectGroupRelationRole = "member"
	ProjectGroupRelationRoleLeader      ProjectGroupRelationRole = "leader"
	ProjectGroupRelationRoleCoordinator ProjectGroupRelationRole = "coordinator"
)

type TenantInvitationStatus string

const (
	TenantInvitationStatusPending  TenantInvitationStatus = "pending"
	TenantInvitationStatusAccepted TenantInvitationStatus = "accepted"
	TenantInvitationStatusRevoked  TenantInvitationStatus = "revoked"
	TenantInvitationStatusExpired  TenantInvitationStatus = "expired"
)

type PrivilegedChangeType string

const (
	PrivilegedChangePlatformIdentitySuspend    PrivilegedChangeType = "platform_identity_suspend"
	PrivilegedChangePlatformIdentityReactivate PrivilegedChangeType = "platform_identity_reactivate"
	PrivilegedChangePlatformIdentityDeactivate PrivilegedChangeType = "platform_identity_deactivate"
	PrivilegedChangePlatformRoleGrant          PrivilegedChangeType = "platform_role_grant"
	PrivilegedChangePlatformRoleRevoke         PrivilegedChangeType = "platform_role_revoke"
)

type PrivilegedChangeStatus string

const (
	PrivilegedChangeStatusPending   PrivilegedChangeStatus = "pending"
	PrivilegedChangeStatusApproved  PrivilegedChangeStatus = "approved"
	PrivilegedChangeStatusRejected  PrivilegedChangeStatus = "rejected"
	PrivilegedChangeStatusCancelled PrivilegedChangeStatus = "cancelled"
	PrivilegedChangeStatusApplied   PrivilegedChangeStatus = "applied"
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

type MFACredential struct {
	ID                  int64               `gorm:"primaryKey;autoIncrement"`
	UserID              int64               `gorm:"column:user_id;not null"`
	Method              string              `gorm:"column:method;not null"`
	Status              MFACredentialStatus `gorm:"column:status;not null"`
	SecretCiphertext    []byte              `gorm:"column:secret_ciphertext;not null"`
	SecretNonce         []byte              `gorm:"column:secret_nonce;not null"`
	KeyVersion          int                 `gorm:"column:key_version;not null"`
	LastAcceptedCounter *int64              `gorm:"column:last_accepted_counter"`
	CreatedAt           time.Time           `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt           time.Time           `gorm:"column:updated_at;autoUpdateTime"`
}

func (MFACredential) TableName() string { return "system.mfa_credentials" }

type MFAChallenge struct {
	ID                         int64          `gorm:"primaryKey;autoIncrement"`
	TokenHash                  string         `gorm:"column:token_hash;not null;unique"`
	PrincipalID                int64          `gorm:"column:principal_id;not null"`
	Purpose                    string         `gorm:"column:purpose;not null"`
	SourceFamilyID             *int64         `gorm:"column:source_family_id"`
	IssuedAuthorizationVersion int64          `gorm:"column:issued_authorization_version;not null"`
	AuthenticationMethods      pq.StringArray `gorm:"column:authentication_methods;type:text[];not null"`
	AuthenticatedAt            time.Time      `gorm:"column:authenticated_at;not null"`
	ExpiresAt                  time.Time      `gorm:"column:expires_at;not null"`
	FailedAttempts             int            `gorm:"column:failed_attempts;not null"`
	ConsumedAt                 *time.Time     `gorm:"column:consumed_at"`
	CreatedAt                  time.Time      `gorm:"column:created_at;autoCreateTime"`
}

func (MFAChallenge) TableName() string { return "system.mfa_challenges" }

type MFAEnrollment struct {
	ID                         int64      `gorm:"primaryKey;autoIncrement"`
	TokenHash                  string     `gorm:"column:token_hash;not null;unique"`
	PrincipalID                int64      `gorm:"column:principal_id;not null"`
	SourceFamilyID             int64      `gorm:"column:source_family_id;not null"`
	IssuedAuthorizationVersion int64      `gorm:"column:issued_authorization_version;not null"`
	SecretCiphertext           []byte     `gorm:"column:secret_ciphertext;not null"`
	SecretNonce                []byte     `gorm:"column:secret_nonce;not null"`
	KeyVersion                 int        `gorm:"column:key_version;not null"`
	ExpiresAt                  time.Time  `gorm:"column:expires_at;not null"`
	FailedAttempts             int        `gorm:"column:failed_attempts;not null"`
	ConsumedAt                 *time.Time `gorm:"column:consumed_at"`
	CreatedAt                  time.Time  `gorm:"column:created_at;autoCreateTime"`
}

func (MFAEnrollment) TableName() string { return "system.mfa_enrollments" }

type IAMBootstrapState struct {
	Singleton   bool               `gorm:"column:singleton;primaryKey"`
	Status      IAMBootstrapStatus `gorm:"column:status;not null"`
	SecretHash  *string            `gorm:"column:secret_hash"`
	PreparedAt  time.Time          `gorm:"column:prepared_at;not null"`
	ExpiresAt   time.Time          `gorm:"column:expires_at;not null"`
	CompletedAt *time.Time         `gorm:"column:completed_at"`
}

func (IAMBootstrapState) TableName() string { return "system.iam_bootstrap_state" }

type IAMRecoveryAttempt struct {
	ID          int64             `gorm:"primaryKey;autoIncrement"`
	SecretHash  *string           `gorm:"column:secret_hash"`
	Status      IAMRecoveryStatus `gorm:"column:status;not null"`
	PreparedAt  time.Time         `gorm:"column:prepared_at;not null"`
	ExpiresAt   time.Time         `gorm:"column:expires_at;not null"`
	CompletedAt *time.Time        `gorm:"column:completed_at"`
	ExpiredAt   *time.Time        `gorm:"column:expired_at"`
	CreatedAt   time.Time         `gorm:"column:created_at;autoCreateTime"`
}

func (IAMRecoveryAttempt) TableName() string { return "system.iam_recovery_attempts" }

type Tenant struct {
	ID                       int64        `gorm:"primaryKey;autoIncrement"`
	Code                     string       `gorm:"column:code;not null;unique"`
	Name                     string       `gorm:"column:name;not null"`
	Description              string       `gorm:"column:description;not null"`
	Status                   TenantStatus `gorm:"column:status;not null;default:active"`
	InitializedAt            *time.Time   `gorm:"column:initialized_at"`
	InitializedByPrincipalID *int64       `gorm:"column:initialized_by_principal_id"`
	CreatedAt                time.Time    `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt                time.Time    `gorm:"column:updated_at;autoUpdateTime"`
}

func (Tenant) TableName() string { return "system.tenants" }

type Role struct {
	ID                    int64          `gorm:"primaryKey;autoIncrement"`
	TenantID              *int64         `gorm:"column:tenant_id"`
	RoleKey               string         `gorm:"column:role_key;not null"`
	Name                  *string        `gorm:"column:name"`
	Description           *string        `gorm:"column:description"`
	NameI18nKey           *string        `gorm:"column:name_i18n_key"`
	DescriptionI18nKey    *string        `gorm:"column:description_i18n_key"`
	RoleType              string         `gorm:"column:role_type;not null"`
	AllowedScopeTypes     pq.StringArray `gorm:"column:allowed_scope_types;type:text[];not null"`
	AllowedPrincipalTypes pq.StringArray `gorm:"column:allowed_principal_types;type:text[];not null"`
	Immutable             bool           `gorm:"column:immutable;not null"`
	Status                string         `gorm:"column:status;not null"`
	CreatedByPrincipalID  *int64         `gorm:"column:created_by_principal_id"`
	CreatedAt             time.Time      `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt             time.Time      `gorm:"column:updated_at;autoUpdateTime"`
}

func (Role) TableName() string { return "system.roles" }

type RoleAssignment struct {
	ID                   int64      `gorm:"primaryKey;autoIncrement"`
	PrincipalID          int64      `gorm:"column:principal_id;not null"`
	RoleID               int64      `gorm:"column:role_id;not null"`
	ScopeType            string     `gorm:"column:scope_type;not null"`
	TenantID             *int64     `gorm:"column:tenant_id"`
	DepartmentID         *int64     `gorm:"column:department_id"`
	ProjectGroupID       *int64     `gorm:"column:project_group_id"`
	Status               string     `gorm:"column:status;not null"`
	ValidFrom            time.Time  `gorm:"column:valid_from;not null"`
	ValidUntil           *time.Time `gorm:"column:valid_until"`
	SourceType           string     `gorm:"column:source_type;not null"`
	CreatedByPrincipalID *int64     `gorm:"column:created_by_principal_id"`
	RevokedByPrincipalID *int64     `gorm:"column:revoked_by_principal_id"`
	RevokedAt            *time.Time `gorm:"column:revoked_at"`
	Reason               string     `gorm:"column:reason;not null"`
	CreatedAt            time.Time  `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt            time.Time  `gorm:"column:updated_at;autoUpdateTime"`
}

func (RoleAssignment) TableName() string { return "system.role_assignments" }

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

type Department struct {
	ID        int64            `gorm:"primaryKey;autoIncrement"`
	TenantID  int64            `gorm:"column:tenant_id;not null"`
	ParentID  *int64           `gorm:"column:parent_id"`
	Code      string           `gorm:"column:code;not null"`
	Name      string           `gorm:"column:name;not null"`
	Status    DepartmentStatus `gorm:"column:status;not null"`
	Version   int64            `gorm:"column:version;not null"`
	CreatedAt time.Time        `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt time.Time        `gorm:"column:updated_at;autoUpdateTime"`
}

func (Department) TableName() string { return "system.departments" }

type DepartmentMembership struct {
	ID                 int64                        `gorm:"primaryKey;autoIncrement"`
	TenantID           int64                        `gorm:"column:tenant_id;not null"`
	DepartmentID       int64                        `gorm:"column:department_id;not null"`
	TenantMembershipID int64                        `gorm:"column:tenant_membership_id;not null"`
	MembershipType     DepartmentMembershipType     `gorm:"column:membership_type;not null"`
	RelationRole       DepartmentRelationRole       `gorm:"column:relation_role;not null"`
	Status             OrganizationMembershipStatus `gorm:"column:status;not null"`
	EndedAt            *time.Time                   `gorm:"column:ended_at"`
	Version            int64                        `gorm:"column:version;not null"`
	CreatedAt          time.Time                    `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt          time.Time                    `gorm:"column:updated_at;autoUpdateTime"`
}

func (DepartmentMembership) TableName() string { return "system.department_memberships" }

type ProjectGroup struct {
	ID          int64              `gorm:"primaryKey;autoIncrement"`
	TenantID    int64              `gorm:"column:tenant_id;not null"`
	Code        string             `gorm:"column:code;not null"`
	Name        string             `gorm:"column:name;not null"`
	Description string             `gorm:"column:description;not null"`
	Status      ProjectGroupStatus `gorm:"column:status;not null"`
	StartsAt    *time.Time         `gorm:"column:starts_at"`
	EndsAt      *time.Time         `gorm:"column:ends_at"`
	Version     int64              `gorm:"column:version;not null"`
	CreatedAt   time.Time          `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt   time.Time          `gorm:"column:updated_at;autoUpdateTime"`
}

func (ProjectGroup) TableName() string { return "system.project_groups" }

type ProjectGroupMembership struct {
	ID                 int64                        `gorm:"primaryKey;autoIncrement"`
	TenantID           int64                        `gorm:"column:tenant_id;not null"`
	ProjectGroupID     int64                        `gorm:"column:project_group_id;not null"`
	TenantMembershipID int64                        `gorm:"column:tenant_membership_id;not null"`
	RelationRole       ProjectGroupRelationRole     `gorm:"column:relation_role;not null"`
	Status             OrganizationMembershipStatus `gorm:"column:status;not null"`
	EndedAt            *time.Time                   `gorm:"column:ended_at"`
	Version            int64                        `gorm:"column:version;not null"`
	CreatedAt          time.Time                    `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt          time.Time                    `gorm:"column:updated_at;autoUpdateTime"`
}

func (ProjectGroupMembership) TableName() string { return "system.project_group_memberships" }

type TenantInvitation struct {
	ID                    int64                  `gorm:"primaryKey;autoIncrement"`
	TenantID              int64                  `gorm:"column:tenant_id;not null"`
	Email                 string                 `gorm:"column:email;not null"`
	NormalizedEmail       string                 `gorm:"column:normalized_email;not null"`
	SecretHash            string                 `gorm:"column:secret_hash;not null;unique"`
	Status                TenantInvitationStatus `gorm:"column:status;not null"`
	ExpiresAt             time.Time              `gorm:"column:expires_at;not null"`
	AcceptedAt            *time.Time             `gorm:"column:accepted_at"`
	AcceptedByPrincipalID *int64                 `gorm:"column:accepted_by_principal_id"`
	RevokedAt             *time.Time             `gorm:"column:revoked_at"`
	RevokedByPrincipalID  *int64                 `gorm:"column:revoked_by_principal_id"`
	ExpiredAt             *time.Time             `gorm:"column:expired_at"`
	CreatedByPrincipalID  int64                  `gorm:"column:created_by_principal_id;not null"`
	CreatedAt             time.Time              `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt             time.Time              `gorm:"column:updated_at;autoUpdateTime"`
}

func (TenantInvitation) TableName() string { return "system.tenant_invitations" }

type PrivilegedChangeRequest struct {
	ID                     int64                  `gorm:"primaryKey;autoIncrement"`
	ChangeType             PrivilegedChangeType   `gorm:"column:change_type;not null"`
	TargetPrincipalID      int64                  `gorm:"column:target_principal_id;not null"`
	TargetRoleID           *int64                 `gorm:"column:target_role_id"`
	ScopeType              string                 `gorm:"column:scope_type;not null"`
	Reason                 string                 `gorm:"column:reason;not null"`
	RequestedByPrincipalID int64                  `gorm:"column:requested_by_principal_id;not null"`
	Status                 PrivilegedChangeStatus `gorm:"column:status;not null"`
	RequestedAt            time.Time              `gorm:"column:requested_at;not null;default:now()"`
	DecidedAt              *time.Time             `gorm:"column:decided_at"`
	AppliedAt              *time.Time             `gorm:"column:applied_at"`
	CreatedAt              time.Time              `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt              time.Time              `gorm:"column:updated_at;autoUpdateTime"`
}

func (PrivilegedChangeRequest) TableName() string {
	return "system.privileged_change_requests"
}

type PrivilegedChangeApproval struct {
	ID                  int64     `gorm:"primaryKey;autoIncrement"`
	RequestID           int64     `gorm:"column:request_id;not null;unique"`
	ReviewerPrincipalID int64     `gorm:"column:reviewer_principal_id;not null"`
	Decision            string    `gorm:"column:decision;not null"`
	Reason              string    `gorm:"column:reason;not null"`
	DecidedAt           time.Time `gorm:"column:decided_at;not null;default:now()"`
	CreatedAt           time.Time `gorm:"column:created_at;autoCreateTime"`
}

func (PrivilegedChangeApproval) TableName() string {
	return "system.privileged_change_approvals"
}

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

// ExecutionAuthorization records a short-lived authorization boundary for one
// execution. It is an auditable database fact, not a bearer credential.
type ExecutionAuthorization struct {
	ID                                   int64      `gorm:"primaryKey;autoIncrement"`
	ActorPrincipalID                     int64      `gorm:"column:actor_principal_id;not null"`
	TenantID                             int64      `gorm:"column:tenant_id;not null"`
	TenantMembershipID                   int64      `gorm:"column:tenant_membership_id;not null"`
	IssuedAuthorizationVersion           int64      `gorm:"column:issued_authorization_version;not null"`
	SourceType                           string     `gorm:"column:source_type;not null"`
	SourceDefinitionID                   *int64     `gorm:"column:source_definition_id"`
	SourceDefinitionVersion              *string    `gorm:"column:source_definition_version"`
	SourceNotebookSessionAuthorizationID *uuid.UUID `gorm:"column:source_notebook_session_authorization_id;type:uuid"`
	SourceExecutionAttempt               *int       `gorm:"column:source_execution_attempt"`
	SourceExecutionLeaseToken            *uuid.UUID `gorm:"column:source_execution_lease_token;type:uuid"`
	ExecutionID                          uuid.UUID  `gorm:"column:execution_id;type:uuid;not null"`
	Audience                             string     `gorm:"column:audience;not null"`
	ExpiresAt                            time.Time  `gorm:"column:expires_at;not null"`
	SealedAt                             *time.Time `gorm:"column:sealed_at"`
	RevokedAt                            *time.Time `gorm:"column:revoked_at"`
	RevokedReason                        *string    `gorm:"column:revoked_reason"`
	CreatedAt                            time.Time  `gorm:"column:created_at;autoCreateTime"`
}

type ExecutionAuthorizationEngineAccess struct {
	AuthorizationID int64          `gorm:"column:authorization_id;primaryKey"`
	EngineID        int64          `gorm:"column:engine_id;primaryKey"`
	Effects         pq.StringArray `gorm:"column:effects;type:text[];not null"`
}

func (ExecutionAuthorizationEngineAccess) TableName() string {
	return "system.execution_authorization_engine_accesses"
}

func (ExecutionAuthorization) TableName() string {
	return "system.execution_authorizations"
}

// NotebookSessionAuthorization records the user-derived authorization facts
// for one interactive Notebook Session. It is a database reference, not a
// bearer credential.
type NotebookSessionAuthorization struct {
	ID                         uuid.UUID      `gorm:"column:id;type:uuid;primaryKey"`
	SessionID                  uuid.UUID      `gorm:"column:session_id;type:uuid;not null;unique"`
	TaskID                     int64          `gorm:"column:task_id;not null"`
	ActorPrincipalID           int64          `gorm:"column:actor_principal_id;not null"`
	TenantID                   int64          `gorm:"column:tenant_id;not null"`
	TenantMembershipID         int64          `gorm:"column:tenant_membership_id;not null"`
	TokenFamilyID              int64          `gorm:"column:token_family_id;not null"`
	IssuedAuthorizationVersion int64          `gorm:"column:issued_authorization_version;not null"`
	Audience                   string         `gorm:"column:audience;not null"`
	Operations                 pq.StringArray `gorm:"column:operations;type:text[];not null"`
	ExpiresAt                  time.Time      `gorm:"column:expires_at;not null"`
	RevokedAt                  *time.Time     `gorm:"column:revoked_at"`
	RevokedReason              *string        `gorm:"column:revoked_reason"`
	CreatedAt                  time.Time      `gorm:"column:created_at;autoCreateTime"`
}

func (NotebookSessionAuthorization) TableName() string {
	return "system.notebook_session_authorizations"
}

// TaskAuthorizationSubject is the durable IAM binding used by a persisted
// task definition for scheduled execution. It contains no credential.
type TaskAuthorizationSubject struct {
	ID                   int64     `gorm:"primaryKey;autoIncrement"`
	OwnerModule          string    `gorm:"column:owner_module;not null"`
	TaskType             string    `gorm:"column:task_type;not null"`
	TaskRef              uuid.UUID `gorm:"column:task_ref;type:uuid;not null"`
	DefinitionHash       string    `gorm:"column:definition_hash;not null"`
	TenantID             int64     `gorm:"column:tenant_id;not null"`
	PrincipalID          int64     `gorm:"column:principal_id;not null"`
	TenantMembershipID   int64     `gorm:"column:tenant_membership_id;not null"`
	AuthorizationVersion int64     `gorm:"column:authorization_version;not null"`
	AuthorizedAt         time.Time `gorm:"column:authorized_at;not null"`
	CreatedAt            time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt            time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (TaskAuthorizationSubject) TableName() string { return "system.task_authorization_subjects" }

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
