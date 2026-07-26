package oauth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	commonauth "github.com/addp/common/authorization"
	"github.com/addp/system/internal/iam"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/ory/fosite"
	"github.com/ory/fosite/handler/pkce"
	"gorm.io/gorm/clause"
)

const authorizationRequestSecretPrefix = "addp_ars_"

type AuthorizationRequestInput struct {
	ClientID            string
	RedirectURI         string
	Scope               string
	CodeChallenge       string
	CodeChallengeMethod string
	Audit               iam.AuditMetadata
}

type CreatedAuthorizationRequest struct {
	RequestID     string
	RequestSecret string
	ExpiresIn     int
}

type AuthorizationRequestView struct {
	RequestID  string
	ClientID   string
	ClientName string
	Scope      string
	ExpiresAt  time.Time
}

type AuthorizationDecision string

const (
	AuthorizationDecisionApprove AuthorizationDecision = "approved"
	AuthorizationDecisionReject  AuthorizationDecision = "rejected"
)

type AuthorizationDecisionResult struct {
	RedirectURL string
	ClientID    string
	Scope       string
}

type DeviceAuthorizationDecisionInput struct {
	UserCode    string
	Approve     bool
	AuthContext commonauth.AuthContext
	Audit       iam.AuditMetadata
}

type ConsentBridge struct {
	provider   *Provider
	repository *iam.Repository
	requestTTL time.Duration
	now        func() time.Time
}

func NewConsentBridge(
	provider *Provider,
	repository *iam.Repository,
	requestTTL time.Duration,
) (*ConsentBridge, error) {
	if provider == nil || provider.OAuth2 == nil || provider.Storage == nil || repository == nil {
		return nil, errors.New("OAuth Consent Bridge 依赖不能为空")
	}
	if requestTTL <= 0 || requestTTL > 5*time.Minute {
		return nil, errors.New("OAuth Authorization Request 生命周期必须在 5 分钟以内")
	}
	return &ConsentBridge{
		provider:   provider,
		repository: repository,
		requestTTL: requestTTL,
		now:        func() time.Time { return time.Now().UTC() },
	}, nil
}

func (b *ConsentBridge) CreateAuthorizationRequest(
	ctx context.Context,
	input AuthorizationRequestInput,
) (*CreatedAuthorizationRequest, error) {
	requestID := uuid.New()
	form := url.Values{
		"client_id":             []string{input.ClientID},
		"redirect_uri":          []string{input.RedirectURI},
		"response_type":         []string{"code"},
		"state":                 []string{requestID.String()},
		"scope":                 []string{input.Scope},
		"audience":              []string{"addp.api"},
		"code_challenge":        []string{input.CodeChallenge},
		"code_challenge_method": []string{input.CodeChallengeMethod},
	}
	httpRequest, err := newFormRequest(ctx, http.MethodGet, "/oauth/authorize", form)
	if err != nil {
		return nil, err
	}
	requester, err := b.provider.OAuth2.NewAuthorizeRequest(ctx, httpRequest)
	if err != nil {
		return nil, err
	}
	if err := (&pkce.Handler{Config: b.provider.Config}).ValidateAuthorizeRequest(ctx, requester); err != nil {
		return nil, err
	}
	requester.SetID(requestID.String())
	if concrete, ok := requester.(*fosite.AuthorizeRequest); ok {
		concrete.RequestedAt = b.now()
	}
	requestSecret, err := generateRequestSecret()
	if err != nil {
		return nil, err
	}

	txCtx, err := b.provider.Storage.BeginTX(ctx)
	if err != nil {
		return nil, err
	}
	expiresAt := b.now().Add(b.requestTTL)
	if err := b.provider.Storage.createAuthorizationRequest(
		txCtx,
		requester,
		opaqueSignature(requestSecret),
		expiresAt,
	); err != nil {
		_ = b.provider.Storage.Rollback(txCtx)
		return nil, err
	}
	if err := writeOAuthAudit(txCtx, b.provider.Storage, input.Audit, iam.AuditEvent{
		EventName:  "oauth.authorization_request.created",
		Result:     iam.AuditResultSucceeded,
		RiskLevel:  iam.AuditRiskLow,
		ModuleName: "system",
		EntityType: "oauth_security_event",
		EntityID:   "oauth.authorization_request.created",
		Details: map[string]interface{}{
			"client_id": input.ClientID,
			"scope":     input.Scope,
		},
	}); err != nil {
		_ = b.provider.Storage.Rollback(txCtx)
		return nil, err
	}
	if err := b.provider.Storage.Commit(txCtx); err != nil {
		return nil, err
	}
	return &CreatedAuthorizationRequest{
		RequestID:     requestID.String(),
		RequestSecret: requestSecret,
		ExpiresIn:     int(b.requestTTL / time.Second),
	}, nil
}

func (b *ConsentBridge) GetAuthorizationRequest(
	ctx context.Context,
	requestID string,
) (*AuthorizationRequestView, error) {
	parsedID, err := uuid.Parse(requestID)
	if err != nil {
		return nil, commonapi.ErrNotFound
	}
	var row authorizationRequestRow
	if err := b.provider.Storage.dbFromContext(ctx).
		Where("id = ? AND status = 'pending' AND expires_at > ?", parsedID, b.now()).
		Take(&row).Error; err != nil {
		return nil, toFositeStorageError(err)
	}
	var client oauthClientRow
	if err := b.provider.Storage.dbFromContext(ctx).
		Where("client_id = ? AND status = 'active'", row.ClientID).Take(&client).Error; err != nil {
		return nil, toFositeStorageError(err)
	}
	return &AuthorizationRequestView{
		RequestID:  row.ID.String(),
		ClientID:   row.ClientID,
		ClientName: client.DisplayName,
		Scope:      strings.Join(row.RequestedScopes, " "),
		ExpiresAt:  row.ExpiresAt.UTC(),
	}, nil
}

func (b *ConsentBridge) CancelAuthorizationRequest(
	ctx context.Context,
	requestID string,
	requestSecret string,
	audit iam.AuditMetadata,
) (bool, error) {
	parsedID, err := uuid.Parse(requestID)
	if err != nil || !strings.HasPrefix(requestSecret, authorizationRequestSecretPrefix) {
		return false, commonapi.ErrBadRequest
	}
	txCtx, err := b.provider.Storage.BeginTX(ctx)
	if err != nil {
		return false, err
	}
	db := b.provider.Storage.dbFromContext(txCtx)
	var row authorizationRequestRow
	if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", parsedID).Take(&row).Error; err != nil {
		_ = b.provider.Storage.Rollback(txCtx)
		return false, toFositeStorageError(err)
	}
	if row.RequestSecretHash != opaqueSignature(requestSecret) {
		_ = b.provider.Storage.Rollback(txCtx)
		return false, commonapi.ErrBadRequest
	}
	if row.Status == "cancelled" {
		_ = b.provider.Storage.Rollback(txCtx)
		return false, nil
	}
	if row.Status != "pending" || !row.ExpiresAt.After(b.now()) {
		_ = b.provider.Storage.Rollback(txCtx)
		return false, commonapi.ErrConflict
	}
	databaseNow, err := b.provider.Storage.databaseNow(txCtx)
	if err != nil {
		_ = b.provider.Storage.Rollback(txCtx)
		return false, err
	}
	if err := db.Model(&authorizationRequestRow{}).Where("id = ? AND status = 'pending'", row.ID).
		Updates(map[string]interface{}{"status": "cancelled", "completed_at": databaseNow}).Error; err != nil {
		_ = b.provider.Storage.Rollback(txCtx)
		return false, toFositeStorageError(err)
	}
	if err := writeOAuthAudit(txCtx, b.provider.Storage, audit, iam.AuditEvent{
		EventName:  "oauth.authorization_request.cancelled",
		Result:     iam.AuditResultSucceeded,
		RiskLevel:  iam.AuditRiskLow,
		ModuleName: "system",
		EntityType: "oauth_security_event",
		EntityID:   "oauth.authorization_request.cancelled",
		Details: map[string]interface{}{
			"client_id": row.ClientID,
			"scope":     strings.Join(row.RequestedScopes, " "),
		},
	}); err != nil {
		_ = b.provider.Storage.Rollback(txCtx)
		return false, err
	}
	if err := b.provider.Storage.Commit(txCtx); err != nil {
		return false, err
	}
	return true, nil
}

func (b *ConsentBridge) DecideAuthorization(
	ctx context.Context,
	requestID string,
	decision AuthorizationDecision,
	authContext commonauth.AuthContext,
	audit iam.AuditMetadata,
) (*AuthorizationDecisionResult, error) {
	parsedID, err := uuid.Parse(requestID)
	if err != nil || (decision != AuthorizationDecisionApprove && decision != AuthorizationDecisionReject) {
		return nil, commonapi.ErrBadRequest
	}
	if err := commonauth.ValidateAuthContext(authContext); err != nil {
		return nil, fmt.Errorf("validate authorization decision AuthContext: %v: %w", err, commonapi.ErrUnauthorized)
	}
	if authContext.Principal.Type != "user" {
		return nil, fmt.Errorf("validate authorization decision principal type: %w", commonapi.ErrUnauthorized)
	}
	txCtx, err := b.provider.Storage.BeginTX(ctx)
	if err != nil {
		return nil, err
	}
	db := b.provider.Storage.dbFromContext(txCtx)
	var row authorizationRequestRow
	if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", parsedID).Take(&row).Error; err != nil {
		_ = b.provider.Storage.Rollback(txCtx)
		return nil, toFositeStorageError(err)
	}
	if row.Status != "pending" || !row.ExpiresAt.After(b.now()) {
		_ = b.provider.Storage.Rollback(txCtx)
		return nil, commonapi.ErrConflict
	}
	requester, err := b.rebuildAuthorizeRequester(txCtx, &row)
	if err != nil {
		_ = b.provider.Storage.Rollback(txCtx)
		return nil, err
	}

	var redirectURL string
	databaseNow, err := b.provider.Storage.databaseNow(txCtx)
	if err != nil {
		_ = b.provider.Storage.Rollback(txCtx)
		return nil, err
	}
	if decision == AuthorizationDecisionReject {
		if err := db.Model(&authorizationRequestRow{}).Where("id = ? AND status = 'pending'", row.ID).
			Updates(map[string]interface{}{"status": "rejected", "completed_at": databaseNow}).Error; err != nil {
			_ = b.provider.Storage.Rollback(txCtx)
			return nil, toFositeStorageError(err)
		}
		redirectURL, err = b.writeAuthorizationError(txCtx, requester, fosite.ErrAccessDenied)
	} else {
		session, identity, err := b.sessionFromAuthContext(txCtx, authContext, row.RequestedAt)
		if err != nil {
			_ = b.provider.Storage.Rollback(txCtx)
			return nil, fmt.Errorf("build authorization decision session: %w", err)
		}
		if err := db.Model(&authorizationRequestRow{}).Where("id = ? AND status = 'pending'", row.ID).
			Updates(map[string]interface{}{
				"status":                       "approved",
				"principal_id":                 identity.principalID,
				"context_type":                 identity.contextType,
				"tenant_membership_id":         identity.tenantMembershipID,
				"issued_authorization_version": identity.authorizationVersion,
				"granted_scopes":               row.RequestedScopes,
				"granted_audiences":            row.RequestedAudiences,
				"authentication_methods":       pq.StringArray(authContext.Authentication.Methods),
				"assurance_level":              authContext.Authentication.AssuranceLevel,
				"authenticated_at":             authContext.Authentication.AuthenticatedAt.UTC(),
				"completed_at":                 databaseNow,
			}).Error; err != nil {
			_ = b.provider.Storage.Rollback(txCtx)
			return nil, toFositeStorageError(err)
		}
		response, err := b.provider.OAuth2.NewAuthorizeResponse(txCtx, requester, session)
		if err != nil {
			_ = b.provider.Storage.Rollback(txCtx)
			return nil, err
		}
		redirectURL, err = b.writeAuthorizationResponse(txCtx, requester, response)
	}
	if err != nil {
		_ = b.provider.Storage.Rollback(txCtx)
		return nil, err
	}
	eventName := "oauth.authorization." + string(decision)
	if err := writeOAuthAudit(txCtx, b.provider.Storage, audit, iam.AuditEvent{
		EventName:  eventName,
		Result:     iam.AuditResultSucceeded,
		RiskLevel:  iam.AuditRiskMedium,
		ModuleName: "system",
		EntityType: "oauth_security_event",
		EntityID:   eventName,
		Details: map[string]interface{}{
			"client_id": row.ClientID,
			"decision":  string(decision),
			"scope":     strings.Join(row.RequestedScopes, " "),
		},
	}); err != nil {
		_ = b.provider.Storage.Rollback(txCtx)
		return nil, err
	}
	if err := b.provider.Storage.Commit(txCtx); err != nil {
		return nil, err
	}
	return &AuthorizationDecisionResult{
		RedirectURL: redirectURL,
		ClientID:    row.ClientID,
		Scope:       strings.Join(row.RequestedScopes, " "),
	}, nil
}

func (b *ConsentBridge) DecideDeviceAuthorization(
	ctx context.Context,
	input DeviceAuthorizationDecisionInput,
) error {
	if err := commonauth.ValidateAuthContext(input.AuthContext); err != nil ||
		input.AuthContext.Principal.Type != "user" {
		return commonapi.ErrUnauthorized
	}
	signatures, err := b.provider.Strategy.UserCodeSignatures(input.UserCode)
	if err != nil {
		return commonapi.ErrBadRequest
	}
	var snapshot deviceAuthorizationRow
	if err := b.provider.Storage.dbFromContext(ctx).
		Where("user_code_hash IN ?", signatures).Take(&snapshot).Error; err != nil {
		return toFositeStorageError(err)
	}
	txCtx, err := b.provider.Storage.BeginTX(ctx)
	if err != nil {
		return err
	}
	_, identity, err := b.sessionFromAuthContext(txCtx, input.AuthContext, snapshot.RequestedAt)
	if err != nil {
		_ = b.provider.Storage.Rollback(txCtx)
		return err
	}
	decision := DeviceDecisionReject
	var facts *ApprovedIdentityFacts
	if input.Approve {
		decision = DeviceDecisionApprove
		facts = &ApprovedIdentityFacts{
			PrincipalID:                identity.principalID,
			ContextType:                identity.contextType,
			TenantMembershipID:         cloneInt64Pointer(identity.tenantMembershipID),
			IssuedAuthorizationVersion: identity.authorizationVersion,
			GrantedScopes:              append([]string(nil), snapshot.RequestedScopes...),
			GrantedAudiences:           append([]string(nil), snapshot.RequestedAudiences...),
			AuthenticationMethods:      append([]string(nil), input.AuthContext.Authentication.Methods...),
			AssuranceLevel:             input.AuthContext.Authentication.AssuranceLevel,
			AuthenticatedAt:            input.AuthContext.Authentication.AuthenticatedAt.UTC(),
		}
	}
	if err := b.provider.Storage.DecideDeviceAuthorization(
		txCtx,
		snapshot.UserCodeHash,
		decision,
		facts,
	); err != nil {
		_ = b.provider.Storage.Rollback(txCtx)
		return err
	}
	eventName := "oauth.device.authorization.rejected"
	if input.Approve {
		eventName = "oauth.device.authorization.approved"
	}
	if err := writeOAuthAudit(txCtx, b.provider.Storage, input.Audit, iam.AuditEvent{
		EventName:  eventName,
		Result:     iam.AuditResultSucceeded,
		RiskLevel:  iam.AuditRiskMedium,
		ModuleName: "system",
		EntityType: "oauth_security_event",
		EntityID:   eventName,
		Details: map[string]interface{}{
			"client_id": snapshot.ClientID,
			"decision":  string(decision),
			"scope":     strings.Join(snapshot.RequestedScopes, " "),
		},
	}); err != nil {
		_ = b.provider.Storage.Rollback(txCtx)
		return err
	}
	return b.provider.Storage.Commit(txCtx)
}

type approvedIdentity struct {
	principalID          int64
	contextType          string
	tenantMembershipID   *int64
	authorizationVersion int64
}

func (b *ConsentBridge) sessionFromAuthContext(
	ctx context.Context,
	authContext commonauth.AuthContext,
	requestedAt time.Time,
) (*IAMSession, approvedIdentity, error) {
	principalID, err := strconv.ParseInt(authContext.Principal.ID, 10, 64)
	if err != nil || principalID <= 0 {
		return nil, approvedIdentity{}, fmt.Errorf("parse principal ID: %w", commonapi.ErrUnauthorized)
	}
	authorizationVersion, err := strconv.ParseInt(authContext.Authorization.AuthorizationVersion, 10, 64)
	if err != nil || authorizationVersion <= 0 {
		return nil, approvedIdentity{}, fmt.Errorf("parse authorization version: %w", commonapi.ErrUnauthorized)
	}
	repository := iam.NewRepository(b.provider.Storage.dbFromContext(ctx))
	principal, err := repository.LockPrincipal(ctx, principalID)
	if err != nil {
		return nil, approvedIdentity{}, fmt.Errorf("lock authorization principal: %w", err)
	}
	if principal.Status != iam.PrincipalStatusActive || principal.AuthorizationVersion != authorizationVersion {
		return nil, approvedIdentity{}, fmt.Errorf("validate authorization principal state: %w", commonapi.ErrUnauthorized)
	}
	identity := approvedIdentity{
		principalID:          principalID,
		contextType:          authContext.Context.Type,
		authorizationVersion: authorizationVersion,
	}
	switch authContext.Context.Type {
	case "tenant":
		if authContext.Context.TenantMembershipID == nil {
			return nil, approvedIdentity{}, commonapi.ErrUnauthorized
		}
		membershipID, err := strconv.ParseInt(*authContext.Context.TenantMembershipID, 10, 64)
		if err != nil {
			return nil, approvedIdentity{}, commonapi.ErrUnauthorized
		}
		membership, err := repository.LockTenantMembershipByID(ctx, membershipID)
		if err != nil || membership.PrincipalID != principalID ||
			membership.Status != iam.TenantMembershipStatusActive ||
			(membership.ExpiresAt != nil && !membership.ExpiresAt.After(b.now())) {
			return nil, approvedIdentity{}, commonapi.ErrUnauthorized
		}
		tenant, err := repository.GetTenant(ctx, membership.TenantID)
		if err != nil || tenant.Status != iam.TenantStatusActive {
			return nil, approvedIdentity{}, commonapi.ErrUnauthorized
		}
		identity.tenantMembershipID = &membershipID
	case "platform":
		if authContext.Context.TenantMembershipID != nil ||
			(authContext.Authentication.AssuranceLevel != "aal2" && authContext.Authentication.AssuranceLevel != "aal3") {
			return nil, approvedIdentity{}, fmt.Errorf("validate platform authorization context: %w", commonapi.ErrUnauthorized)
		}
		hasRole, err := repository.HasEffectivePlatformRole(ctx, principalID, b.now())
		if err != nil {
			return nil, approvedIdentity{}, fmt.Errorf("query effective platform role: %w", err)
		}
		if !hasRole {
			return nil, approvedIdentity{}, fmt.Errorf("validate effective platform role: %w", commonapi.ErrUnauthorized)
		}
	default:
		return nil, approvedIdentity{}, commonapi.ErrUnauthorized
	}

	session := NewIAMSession()
	session.PrincipalID = principalID
	session.ContextType = identity.contextType
	session.TenantMembershipID = cloneInt64Pointer(identity.tenantMembershipID)
	session.IssuedAuthorizationVersion = authorizationVersion
	session.Subject = authContext.Principal.ID
	session.AuthenticationMethods = append([]string(nil), authContext.Authentication.Methods...)
	session.AssuranceLevel = authContext.Authentication.AssuranceLevel
	session.AuthenticatedAt = authContext.Authentication.AuthenticatedAt.UTC()
	session.RequestedAt = requestedAt.UTC()
	session.SetExpiresAt(
		fosite.AuthorizeCode,
		b.now().Add(b.provider.Config.GetAuthorizeCodeLifespan(ctx)).Round(time.Second),
	)
	return session, identity, nil
}

func (b *ConsentBridge) rebuildAuthorizeRequester(
	ctx context.Context,
	row *authorizationRequestRow,
) (fosite.AuthorizeRequester, error) {
	var pkceRow pkceSessionRow
	if err := b.provider.Storage.dbFromContext(ctx).
		Where("authorization_request_id = ?", row.ID).Take(&pkceRow).Error; err != nil {
		return nil, toFositeStorageError(err)
	}
	form := url.Values{
		"client_id":             []string{row.ClientID},
		"redirect_uri":          []string{row.RedirectURI},
		"response_type":         []string{strings.Join(row.ResponseTypes, " ")},
		"state":                 []string{row.ID.String()},
		"scope":                 []string{strings.Join(row.RequestedScopes, " ")},
		"audience":              append([]string(nil), row.RequestedAudiences...),
		"code_challenge":        []string{pkceRow.CodeChallenge},
		"code_challenge_method": []string{pkceRow.CodeChallengeMethod},
	}
	httpRequest, err := newFormRequest(ctx, http.MethodGet, "/oauth/authorize", form)
	if err != nil {
		return nil, err
	}
	requester, err := b.provider.OAuth2.NewAuthorizeRequest(ctx, httpRequest)
	if err != nil {
		return nil, err
	}
	requester.SetID(row.ID.String())
	if concrete, ok := requester.(*fosite.AuthorizeRequest); ok {
		concrete.RequestedAt = row.RequestedAt.UTC()
	}
	return requester, nil
}

func (b *ConsentBridge) writeAuthorizationResponse(
	ctx context.Context,
	requester fosite.AuthorizeRequester,
	response fosite.AuthorizeResponder,
) (string, error) {
	recorder := httptest.NewRecorder()
	b.provider.OAuth2.WriteAuthorizeResponse(ctx, recorder, requester, response)
	return validatedRedirectLocation(recorder)
}

func (b *ConsentBridge) writeAuthorizationError(
	ctx context.Context,
	requester fosite.AuthorizeRequester,
	oauthError error,
) (string, error) {
	recorder := httptest.NewRecorder()
	b.provider.OAuth2.WriteAuthorizeError(ctx, recorder, requester, oauthError)
	return validatedRedirectLocation(recorder)
}

func validatedRedirectLocation(recorder *httptest.ResponseRecorder) (string, error) {
	if recorder.Code != http.StatusFound && recorder.Code != http.StatusSeeOther {
		return "", errors.New("Fosite 未生成 OAuth 回跳响应")
	}
	location := recorder.Header().Get("Location")
	parsed, err := url.Parse(location)
	if err != nil || !parsed.IsAbs() || parsed.Fragment != "" {
		return "", errors.New("Fosite 生成了无效 OAuth 回跳地址")
	}
	return location, nil
}

func newFormRequest(ctx context.Context, method, path string, form url.Values) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, method, "http://system.local"+path, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Form = form
	request.PostForm = form
	return request, nil
}

func generateRequestSecret() (string, error) {
	randomBytes := make([]byte, randomTokenBytes)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", errors.New("生成 OAuth Authorization Request Secret 失败")
	}
	return authorizationRequestSecretPrefix + base64.RawURLEncoding.EncodeToString(randomBytes), nil
}

func (s *Storage) createAuthorizationRequest(
	ctx context.Context,
	requester fosite.AuthorizeRequester,
	requestSecretHash string,
	expiresAt time.Time,
) error {
	if err := validateStorageSignature(requestSecretHash); err != nil {
		return err
	}
	requestID, err := uuid.Parse(requester.GetID())
	if err != nil || !requester.IsRedirectURIValid() {
		return fosite.ErrInvalidRequest
	}
	responseMode := requester.GetResponseMode()
	if responseMode == fosite.ResponseModeDefault {
		responseMode = fosite.ResponseModeQuery
	}
	row := &authorizationRequestRow{
		ID:                 requestID,
		RequestSecretHash:  requestSecretHash,
		ClientID:           requester.GetClient().GetID(),
		RedirectURI:        requester.GetRedirectURI().String(),
		ResponseTypes:      pq.StringArray(append([]string(nil), requester.GetResponseTypes()...)),
		ResponseMode:       string(responseMode),
		RequestedScopes:    pq.StringArray(append([]string(nil), requester.GetRequestedScopes()...)),
		RequestedAudiences: pq.StringArray(append([]string(nil), requester.GetRequestedAudience()...)),
		Status:             "pending",
		RequestedAt:        requester.GetRequestedAt().UTC(),
		ExpiresAt:          expiresAt.UTC(),
		CreatedAt:          s.now(),
	}
	if err := s.dbFromContext(ctx).Create(row).Error; err != nil {
		return toFositeStorageError(err)
	}
	return toFositeStorageError(s.dbFromContext(ctx).Create(&pkceSessionRow{
		AuthorizationRequestID: requestID,
		CodeChallenge:          requester.GetRequestForm().Get("code_challenge"),
		CodeChallengeMethod:    requester.GetRequestForm().Get("code_challenge_method"),
		ExpiresAt:              expiresAt.UTC(),
		CreatedAt:              s.now(),
	}).Error)
}

func writeOAuthAudit(
	ctx context.Context,
	storage *Storage,
	metadata iam.AuditMetadata,
	event iam.AuditEvent,
) error {
	event.Metadata = metadata
	return iam.NewAuditWriter(iam.NewRepository(storage.dbFromContext(ctx))).Write(ctx, event)
}
