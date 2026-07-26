package oauth

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lib/pq"
	"github.com/ory/fosite"
	"gorm.io/gorm"
)

type Storage struct {
	db                    *gorm.DB
	devicePollingInterval time.Duration
	now                   func() time.Time
}

func NewStorage(db *gorm.DB, devicePollingInterval time.Duration) (*Storage, error) {
	if db == nil {
		return nil, errors.New("OAuth Storage 数据库不能为空")
	}
	if devicePollingInterval < 5*time.Second {
		return nil, errors.New("OAuth Device 轮询间隔不能小于 5 秒")
	}
	return &Storage{
		db:                    db,
		devicePollingInterval: devicePollingInterval,
		now:                   func() time.Time { return time.Now().UTC() },
	}, nil
}

type oauthClientRow struct {
	ClientID         string         `gorm:"column:client_id"`
	DisplayName      string         `gorm:"column:display_name"`
	ClientType       string         `gorm:"column:client_type"`
	ClientSecretHash *string        `gorm:"column:client_secret_hash"`
	RedirectURIs     pq.StringArray `gorm:"column:redirect_uris;type:text[]"`
	GrantTypes       pq.StringArray `gorm:"column:grant_types;type:text[]"`
	ResponseTypes    pq.StringArray `gorm:"column:response_types;type:text[]"`
	AllowedScopes    pq.StringArray `gorm:"column:allowed_scopes;type:text[]"`
	AllowedAudiences pq.StringArray `gorm:"column:allowed_audiences;type:text[]"`
	TokenAuthMethod  string         `gorm:"column:token_endpoint_auth_method"`
	Status           string         `gorm:"column:status"`
}

func (oauthClientRow) TableName() string { return "system.oauth_clients" }

type authorizationRequestRow struct {
	ID                         uuid.UUID      `gorm:"column:id"`
	RequestSecretHash          string         `gorm:"column:request_secret_hash"`
	ClientID                   string         `gorm:"column:client_id"`
	RedirectURI                string         `gorm:"column:redirect_uri"`
	ResponseTypes              pq.StringArray `gorm:"column:response_types;type:text[]"`
	ResponseMode               string         `gorm:"column:response_mode"`
	RequestedScopes            pq.StringArray `gorm:"column:requested_scopes;type:text[]"`
	RequestedAudiences         pq.StringArray `gorm:"column:requested_audiences;type:text[]"`
	Status                     string         `gorm:"column:status"`
	PrincipalID                *int64         `gorm:"column:principal_id"`
	ContextType                *string        `gorm:"column:context_type"`
	TenantMembershipID         *int64         `gorm:"column:tenant_membership_id"`
	IssuedAuthorizationVersion *int64         `gorm:"column:issued_authorization_version"`
	GrantedScopes              pq.StringArray `gorm:"column:granted_scopes;type:text[]"`
	GrantedAudiences           pq.StringArray `gorm:"column:granted_audiences;type:text[]"`
	AuthenticationMethods      pq.StringArray `gorm:"column:authentication_methods;type:text[]"`
	AssuranceLevel             *string        `gorm:"column:assurance_level"`
	AuthenticatedAt            *time.Time     `gorm:"column:authenticated_at"`
	RequestedAt                time.Time      `gorm:"column:requested_at"`
	ExpiresAt                  time.Time      `gorm:"column:expires_at"`
	CompletedAt                *time.Time     `gorm:"column:completed_at"`
	CreatedAt                  time.Time      `gorm:"column:created_at"`
}

func (authorizationRequestRow) TableName() string {
	return "system.oauth_authorization_requests"
}

type authorizationCodeRow struct {
	ID                     int64      `gorm:"column:id"`
	CodeHash               string     `gorm:"column:code_hash"`
	AuthorizationRequestID uuid.UUID  `gorm:"column:authorization_request_id"`
	ExpiresAt              time.Time  `gorm:"column:expires_at"`
	InvalidatedAt          *time.Time `gorm:"column:invalidated_at"`
	CreatedAt              time.Time  `gorm:"column:created_at"`
}

func (authorizationCodeRow) TableName() string { return "system.oauth_authorization_codes" }

type pkceSessionRow struct {
	ID                     int64      `gorm:"column:id"`
	AuthorizationRequestID uuid.UUID  `gorm:"column:authorization_request_id"`
	AuthorizationCodeHash  *string    `gorm:"column:authorization_code_hash"`
	CodeChallenge          string     `gorm:"column:code_challenge"`
	CodeChallengeMethod    string     `gorm:"column:code_challenge_method"`
	VerifiedAt             *time.Time `gorm:"column:verified_at"`
	ConsumedAt             *time.Time `gorm:"column:consumed_at"`
	ExpiresAt              time.Time  `gorm:"column:expires_at"`
	CreatedAt              time.Time  `gorm:"column:created_at"`
}

func (pkceSessionRow) TableName() string { return "system.oauth_pkce_sessions" }

type deviceAuthorizationRow struct {
	ID                         uuid.UUID      `gorm:"column:id"`
	DeviceCodeHash             string         `gorm:"column:device_code_hash"`
	UserCodeHash               string         `gorm:"column:user_code_hash"`
	ClientID                   string         `gorm:"column:client_id"`
	RequestedScopes            pq.StringArray `gorm:"column:requested_scopes;type:text[]"`
	RequestedAudiences         pq.StringArray `gorm:"column:requested_audiences;type:text[]"`
	GrantedScopes              pq.StringArray `gorm:"column:granted_scopes;type:text[]"`
	GrantedAudiences           pq.StringArray `gorm:"column:granted_audiences;type:text[]"`
	Status                     string         `gorm:"column:status"`
	PrincipalID                *int64         `gorm:"column:principal_id"`
	ContextType                *string        `gorm:"column:context_type"`
	TenantMembershipID         *int64         `gorm:"column:tenant_membership_id"`
	IssuedAuthorizationVersion *int64         `gorm:"column:issued_authorization_version"`
	AuthenticationMethods      pq.StringArray `gorm:"column:authentication_methods;type:text[]"`
	AssuranceLevel             *string        `gorm:"column:assurance_level"`
	AuthenticatedAt            *time.Time     `gorm:"column:authenticated_at"`
	PollIntervalSeconds        int            `gorm:"column:poll_interval_seconds"`
	NextPollAt                 time.Time      `gorm:"column:next_poll_at"`
	LastPolledAt               *time.Time     `gorm:"column:last_polled_at"`
	RequestedAt                time.Time      `gorm:"column:requested_at"`
	ExpiresAt                  time.Time      `gorm:"column:expires_at"`
	DecidedAt                  *time.Time     `gorm:"column:decided_at"`
	InvalidatedAt              *time.Time     `gorm:"column:invalidated_at"`
	CreatedAt                  time.Time      `gorm:"column:created_at"`
}

func (deviceAuthorizationRow) TableName() string {
	return "system.oauth_device_authorizations"
}

func (s *Storage) GetClient(ctx context.Context, id string) (fosite.Client, error) {
	var row oauthClientRow
	err := s.dbFromContext(ctx).
		Where("client_id = ? AND status = 'active'", id).
		Take(&row).Error
	if err != nil {
		return nil, toFositeStorageError(err)
	}
	var secret []byte
	if row.ClientSecretHash != nil {
		secret = []byte(*row.ClientSecretHash)
	}
	return &Client{
		ID:            row.ClientID,
		SecretHash:    secret,
		RedirectURIs:  append([]string(nil), row.RedirectURIs...),
		GrantTypes:    append([]string(nil), row.GrantTypes...),
		ResponseTypes: append([]string(nil), row.ResponseTypes...),
		Scopes:        append([]string(nil), row.AllowedScopes...),
		Audiences:     append([]string(nil), row.AllowedAudiences...),
		Public:        row.ClientType == "public",
	}, nil
}

func (s *Storage) databaseNow(ctx context.Context) (time.Time, error) {
	var now time.Time
	if err := s.dbFromContext(ctx).Raw("SELECT transaction_timestamp()").Scan(&now).Error; err != nil {
		return time.Time{}, toFositeStorageError(err)
	}
	return now.UTC(), nil
}

// Client assertion authentication is fail-closed until its replay table and key policy are enabled.
func (*Storage) ClientAssertionJWTValid(context.Context, string) error {
	return fosite.ErrInvalidRequest
}

func (*Storage) SetClientAssertionJWT(context.Context, string, time.Time) error {
	return fosite.ErrInvalidRequest
}

func (s *Storage) requestFromAuthorizationRow(
	ctx context.Context,
	row *authorizationRequestRow,
	tokenType fosite.TokenType,
	tokenExpiresAt time.Time,
) (fosite.Requester, error) {
	client, err := s.GetClient(ctx, row.ClientID)
	if err != nil {
		return nil, err
	}
	session := NewIAMSession()
	if row.PrincipalID != nil {
		session.PrincipalID = *row.PrincipalID
		session.Subject = strconv.FormatInt(*row.PrincipalID, 10)
	}
	if row.ContextType != nil {
		session.ContextType = *row.ContextType
	}
	session.TenantMembershipID = cloneInt64Pointer(row.TenantMembershipID)
	if row.IssuedAuthorizationVersion != nil {
		session.IssuedAuthorizationVersion = *row.IssuedAuthorizationVersion
	}
	session.AuthenticationMethods = append([]string(nil), row.AuthenticationMethods...)
	if row.AssuranceLevel != nil {
		session.AssuranceLevel = *row.AssuranceLevel
	}
	if row.AuthenticatedAt != nil {
		session.AuthenticatedAt = row.AuthenticatedAt.UTC()
	}
	session.RequestedAt = row.RequestedAt.UTC()
	session.SetExpiresAt(tokenType, tokenExpiresAt)

	return &fosite.Request{
		ID:                row.ID.String(),
		RequestedAt:       row.RequestedAt.UTC(),
		Client:            client,
		RequestedScope:    fosite.Arguments(append([]string(nil), row.RequestedScopes...)),
		GrantedScope:      fosite.Arguments(append([]string(nil), row.GrantedScopes...)),
		RequestedAudience: fosite.Arguments(append([]string(nil), row.RequestedAudiences...)),
		GrantedAudience:   fosite.Arguments(append([]string(nil), row.GrantedAudiences...)),
		Form: url.Values{
			"client_id":     []string{row.ClientID},
			"redirect_uri":  []string{row.RedirectURI},
			"response_type": append([]string(nil), row.ResponseTypes...),
		},
		Session: session,
	}, nil
}

func (s *Storage) requestFromDeviceRow(ctx context.Context, row *deviceAuthorizationRow) (fosite.DeviceRequester, error) {
	client, err := s.GetClient(ctx, row.ClientID)
	if err != nil {
		return nil, err
	}
	session := NewIAMSession()
	if row.PrincipalID != nil {
		session.PrincipalID = *row.PrincipalID
		session.Subject = strconv.FormatInt(*row.PrincipalID, 10)
	}
	if row.ContextType != nil {
		session.ContextType = *row.ContextType
	}
	session.TenantMembershipID = cloneInt64Pointer(row.TenantMembershipID)
	if row.IssuedAuthorizationVersion != nil {
		session.IssuedAuthorizationVersion = *row.IssuedAuthorizationVersion
	}
	session.AuthenticationMethods = append([]string(nil), row.AuthenticationMethods...)
	if row.AssuranceLevel != nil {
		session.AssuranceLevel = *row.AssuranceLevel
	}
	if row.AuthenticatedAt != nil {
		session.AuthenticatedAt = row.AuthenticatedAt.UTC()
	}
	session.RequestedAt = row.RequestedAt.UTC()
	session.SetExpiresAt(fosite.DeviceCode, row.ExpiresAt.UTC())
	session.SetExpiresAt(fosite.UserCode, row.ExpiresAt.UTC())

	state := fosite.UserCodeUnused
	switch row.Status {
	case "approved", "invalidated":
		state = fosite.UserCodeAccepted
	case "rejected":
		state = fosite.UserCodeRejected
	}
	return &fosite.DeviceRequest{
		UserCodeState: state,
		Request: fosite.Request{
			ID:                row.ID.String(),
			RequestedAt:       row.RequestedAt.UTC(),
			Client:            client,
			RequestedScope:    fosite.Arguments(append([]string(nil), row.RequestedScopes...)),
			GrantedScope:      fosite.Arguments(append([]string(nil), row.GrantedScopes...)),
			RequestedAudience: fosite.Arguments(append([]string(nil), row.RequestedAudiences...)),
			GrantedAudience:   fosite.Arguments(append([]string(nil), row.GrantedAudiences...)),
			Form:              url.Values{"client_id": []string{row.ClientID}},
			Session:           session,
		},
	}, nil
}

func validateStorageSignature(signature string) error {
	if len(signature) != sha256HexLength {
		return fosite.ErrNotFound
	}
	for _, character := range signature {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return fosite.ErrNotFound
		}
	}
	return nil
}

const sha256HexLength = 64

func toFositeStorageError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fosite.ErrNotFound
	}
	return fmt.Errorf("OAuth Storage 操作失败: %w", err)
}

func isUniqueViolation(err error, constraint string) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == "23505" &&
		(constraint == "" || postgresError.ConstraintName == constraint)
}

var _ fosite.Storage = (*Storage)(nil)
