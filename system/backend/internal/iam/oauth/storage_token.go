package oauth

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/iam"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/ory/fosite"
	fositeoauth2 "github.com/ory/fosite/handler/oauth2"
	"github.com/ory/fosite/handler/rfc8628"
	"gorm.io/gorm"
)

const (
	oauthFamilyAuthType         = "oauth"
	oauthRevocationReason       = "oauth_revoked"
	oauthReplayRevocationReason = "oauth_replay_detected"
)

func (s *Storage) CreateAccessTokenSession(
	ctx context.Context,
	signature string,
	request fosite.Requester,
) error {
	if err := validateStorageSignature(signature); err != nil {
		return err
	}
	requestID, err := uuid.Parse(request.GetID())
	if err != nil {
		return fosite.ErrInvalidRequest
	}
	db := s.dbFromContext(ctx)
	repository := iam.NewRepository(db)
	family, err := s.getFamilyByProtocolRequestID(ctx, db, requestID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		family, err = s.createOAuthFamily(ctx, repository, requestID, request)
	}
	if err != nil {
		return toFositeStorageError(err)
	}
	if err := enrichTokenTransactionAudit(ctx, repository, family); err != nil {
		return toFositeStorageError(err)
	}
	expiresAt := request.GetSession().GetExpiresAt(fosite.AccessToken)
	if expiresAt.IsZero() || expiresAt.After(family.ExpiresAt) {
		return fosite.ErrInvalidRequest
	}
	return toFositeStorageError(repository.CreateAccessToken(ctx, &iam.AccessToken{
		TokenHash: signature,
		FamilyID:  family.ID,
		ExpiresAt: expiresAt.UTC(),
		CreatedAt: s.now(),
	}))
}

func enrichTokenTransactionAudit(
	ctx context.Context,
	repository *iam.Repository,
	family *iam.RefreshTokenFamily,
) error {
	if family == nil {
		return fosite.ErrInvalidRequest
	}
	principalID := family.PrincipalID
	principalType := iam.PrincipalTypeUser
	contextType := family.ContextType
	var tenantID *int64
	if family.ContextType == iam.ContextTypeTenant {
		if family.TenantMembershipID == nil {
			return fosite.ErrInvalidRequest
		}
		membership, err := repository.GetTenantMembershipByID(ctx, *family.TenantMembershipID)
		if err != nil {
			return err
		}
		resolvedTenantID := membership.TenantID
		tenantID = &resolvedTenantID
	}
	updateTransactionAudit(ctx, func(event *iam.AuditEvent) {
		event.Metadata.PrincipalID = &principalID
		event.Metadata.PrincipalType = &principalType
		event.Metadata.ContextType = &contextType
		event.Metadata.TenantID = tenantID
		if event.Details == nil {
			event.Details = make(map[string]any)
		}
		event.Details["client_id"] = family.ClientID
		event.Details["scope"] = strings.Join(family.Scopes, " ")
	})
	return nil
}

func (s *Storage) GetAccessTokenSession(
	ctx context.Context,
	signature string,
	_ fosite.Session,
) (fosite.Requester, error) {
	if err := validateStorageSignature(signature); err != nil {
		return nil, err
	}
	repository := iam.NewRepository(s.dbFromContext(ctx))
	token, err := repository.GetAccessTokenByHash(ctx, signature)
	if err != nil {
		return nil, repositoryErrorToFosite(err)
	}
	family, err := repository.GetRefreshTokenFamily(ctx, token.FamilyID)
	if err != nil {
		return nil, repositoryErrorToFosite(err)
	}
	requester, err := s.requestFromFamily(ctx, family, fosite.AccessToken, token.ExpiresAt)
	if err != nil {
		return nil, err
	}
	if token.RevokedAt != nil || family.RevokedAt != nil || !token.ExpiresAt.After(s.now()) ||
		!family.ExpiresAt.After(s.now()) || !s.familyAuthorizationIsCurrent(ctx, family) {
		return requester, fosite.ErrInactiveToken
	}
	return requester, nil
}

func (s *Storage) DeleteAccessTokenSession(ctx context.Context, signature string) error {
	if err := validateStorageSignature(signature); err != nil {
		return err
	}
	repository := iam.NewRepository(s.dbFromContext(ctx))
	token, err := repository.GetAccessTokenByHash(ctx, signature)
	if err != nil {
		return repositoryErrorToFosite(err)
	}
	if token.RevokedAt != nil {
		return nil
	}
	return repositoryErrorToFosite(repository.RevokeAccessToken(ctx, token.ID, s.now()))
}

func (s *Storage) CreateRefreshTokenSession(
	ctx context.Context,
	signature string,
	accessSignature string,
	request fosite.Requester,
) error {
	if err := validateStorageSignature(signature); err != nil {
		return err
	}
	if err := validateStorageSignature(accessSignature); err != nil {
		return err
	}
	requestID, err := uuid.Parse(request.GetID())
	if err != nil {
		return fosite.ErrInvalidRequest
	}
	db := s.dbFromContext(ctx)
	repository := iam.NewRepository(db)
	family, err := s.getFamilyByProtocolRequestID(ctx, db, requestID)
	if err != nil {
		return toFositeStorageError(err)
	}
	accessToken, err := repository.GetAccessTokenByHash(ctx, accessSignature)
	if err != nil || accessToken.FamilyID != family.ID || accessToken.RevokedAt != nil {
		return fosite.ErrInvalidRequest
	}
	var parentTokenID *int64
	if request.GetRequestForm().Get("grant_type") == string(fosite.GrantTypeRefreshToken) {
		var parent iam.RefreshToken
		if err := db.Where(
			"family_id = ? AND used_at IS NOT NULL AND replaced_by_token_id IS NULL AND reuse_detected_at IS NULL",
			family.ID,
		).Order("id DESC").Take(&parent).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fosite.ErrSerializationFailure
			}
			return toFositeStorageError(err)
		}
		parentTokenID = &parent.ID
	}
	expiresAt := request.GetSession().GetExpiresAt(fosite.RefreshToken)
	if expiresAt.IsZero() || expiresAt.After(family.ExpiresAt) {
		expiresAt = family.ExpiresAt
	}
	replacement := &iam.RefreshToken{
		TokenHash:           signature,
		FamilyID:            family.ID,
		IssuedAccessTokenID: accessToken.ID,
		ParentTokenID:       parentTokenID,
		ExpiresAt:           expiresAt.UTC(),
		CreatedAt:           s.now(),
	}
	if err := repository.CreateRefreshToken(ctx, replacement); err != nil {
		return repositoryErrorToFosite(err)
	}
	if parentTokenID != nil {
		if err := repository.LinkRefreshTokenReplacement(ctx, *parentTokenID, replacement.ID); err != nil {
			return repositoryErrorToFosite(err)
		}
	}
	return nil
}

func (s *Storage) GetRefreshTokenSession(
	ctx context.Context,
	signature string,
	_ fosite.Session,
) (fosite.Requester, error) {
	if err := validateStorageSignature(signature); err != nil {
		return nil, err
	}
	repository := iam.NewRepository(s.dbFromContext(ctx))
	token, err := repository.GetRefreshTokenByHash(ctx, signature)
	if err != nil {
		return nil, repositoryErrorToFosite(err)
	}
	family, err := repository.GetRefreshTokenFamily(ctx, token.FamilyID)
	if err != nil {
		return nil, repositoryErrorToFosite(err)
	}
	requester, err := s.requestFromFamily(ctx, family, fosite.RefreshToken, token.ExpiresAt)
	if err != nil {
		return nil, err
	}
	if token.UsedAt != nil && token.ReplacedByTokenID != nil {
		return requester, fosite.ErrInactiveToken
	}
	if token.RevokedAt != nil || token.ReuseDetectedAt != nil || family.RevokedAt != nil ||
		!token.ExpiresAt.After(s.now()) || !family.ExpiresAt.After(s.now()) ||
		!s.familyAuthorizationCanRotate(ctx, family) {
		return requester, fosite.ErrNotFound
	}
	return requester, nil
}

func (s *Storage) DeleteRefreshTokenSession(ctx context.Context, signature string) error {
	if err := validateStorageSignature(signature); err != nil {
		return err
	}
	repository := iam.NewRepository(s.dbFromContext(ctx))
	token, err := repository.GetRefreshTokenByHash(ctx, signature)
	if err != nil {
		return repositoryErrorToFosite(err)
	}
	if token.ReuseDetectedAt != nil {
		return nil
	}
	if token.UsedAt == nil || token.ReplacedByTokenID == nil {
		return fosite.ErrSerializationFailure
	}
	return repositoryErrorToFosite(repository.MarkRefreshTokenReuseDetected(ctx, token.ID, s.now()))
}

func (s *Storage) RotateRefreshToken(ctx context.Context, requestID string, refreshSignature string) error {
	if err := validateStorageSignature(refreshSignature); err != nil {
		return err
	}
	parsedRequestID, err := uuid.Parse(requestID)
	if err != nil {
		return fosite.ErrInvalidRequest
	}
	db := s.dbFromContext(ctx)
	repository := iam.NewRepository(db)
	snapshot, err := repository.GetRefreshTokenByHash(ctx, refreshSignature)
	if err != nil {
		return repositoryErrorToFosite(err)
	}
	familySnapshot, err := repository.GetRefreshTokenFamily(ctx, snapshot.FamilyID)
	if err != nil || familySnapshot.ProtocolRequestID == nil || *familySnapshot.ProtocolRequestID != parsedRequestID {
		return fosite.ErrNotFound
	}
	principal, err := repository.LockPrincipal(ctx, familySnapshot.PrincipalID)
	if err != nil {
		return repositoryErrorToFosite(err)
	}
	family, err := repository.LockRefreshTokenFamily(ctx, familySnapshot.ID)
	if err != nil {
		return repositoryErrorToFosite(err)
	}
	token, err := repository.LockRefreshTokenByHash(ctx, refreshSignature)
	if err != nil {
		return repositoryErrorToFosite(err)
	}
	if token.ID != snapshot.ID || token.FamilyID != family.ID || token.UsedAt != nil ||
		token.RevokedAt != nil || token.ReuseDetectedAt != nil || family.RevokedAt != nil ||
		!token.ExpiresAt.After(s.now()) || !family.ExpiresAt.After(s.now()) {
		return fosite.ErrSerializationFailure
	}
	contextActive, err := repository.RefreshTokenFamilyContextIsActive(ctx, principal, family, s.now())
	if err != nil {
		return repositoryErrorToFosite(err)
	}
	if !contextActive || family.IssuedAuthorizationVersion > principal.AuthorizationVersion {
		return fosite.ErrNotFound
	}
	accessToken, err := repository.LockAccessToken(ctx, token.IssuedAccessTokenID)
	if err != nil {
		return repositoryErrorToFosite(err)
	}
	if accessToken.FamilyID != family.ID || accessToken.RevokedAt != nil {
		return fosite.ErrSerializationFailure
	}
	if family.IssuedAuthorizationVersion < principal.AuthorizationVersion {
		previousVersion := family.IssuedAuthorizationVersion
		if err := repository.AdvanceRefreshTokenFamilyAuthorizationVersion(ctx, family.ID, principal.AuthorizationVersion); err != nil {
			return repositoryErrorToFosite(err)
		}
		family.IssuedAuthorizationVersion = principal.AuthorizationVersion
		updateTransactionAudit(ctx, func(event *iam.AuditEvent) {
			if event.Details == nil {
				event.Details = make(map[string]any)
			}
			event.Details["previous_authorization_version"] = previousVersion
			event.Details["authorization_version"] = principal.AuthorizationVersion
			event.Details["authorization_version_advanced"] = true
		})
	}
	now := s.now()
	if err := repository.MarkRefreshTokenUsed(ctx, token.ID, now); err != nil {
		return fosite.ErrSerializationFailure
	}
	if err := repository.RevokeAccessToken(ctx, accessToken.ID, now); err != nil {
		return repositoryErrorToFosite(err)
	}
	return nil
}

func (s *Storage) RevokeRefreshToken(ctx context.Context, requestID string) error {
	if ctx.Value(transactionContextKey{}) != nil {
		return s.revokeFamilyByRequestID(ctx, requestID)
	}
	txCtx, err := s.BeginTX(ctx)
	if err != nil {
		return err
	}
	if err := s.revokeFamilyByRequestID(txCtx, requestID); err != nil {
		_ = s.Rollback(txCtx)
		return err
	}
	return s.Commit(txCtx)
}

func (s *Storage) RevokeAccessToken(ctx context.Context, requestID string) error {
	parsedRequestID, err := uuid.Parse(requestID)
	if err != nil {
		return fosite.ErrNotFound
	}
	db := s.dbFromContext(ctx)
	family, err := s.getFamilyByProtocolRequestID(ctx, db, parsedRequestID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fosite.ErrNotFound
	}
	if err != nil {
		return toFositeStorageError(err)
	}
	return toFositeStorageError(db.Model(&iam.AccessToken{}).
		Where("family_id = ? AND revoked_at IS NULL", family.ID).
		Update("revoked_at", s.now()).Error)
}

func (s *Storage) revokeFamilyByRequestID(ctx context.Context, requestID string) error {
	parsedRequestID, err := uuid.Parse(requestID)
	if err != nil {
		return fosite.ErrNotFound
	}
	db := s.dbFromContext(ctx)
	repository := iam.NewRepository(db)
	familySnapshot, err := s.getFamilyByProtocolRequestID(ctx, db, parsedRequestID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fosite.ErrNotFound
	}
	if err != nil {
		return toFositeStorageError(err)
	}
	if _, err := repository.LockPrincipal(ctx, familySnapshot.PrincipalID); err != nil {
		return repositoryErrorToFosite(err)
	}
	family, err := repository.LockRefreshTokenFamily(ctx, familySnapshot.ID)
	if err != nil {
		return repositoryErrorToFosite(err)
	}
	if family.RevokedAt != nil {
		return nil
	}
	var reusedCount int64
	if err := db.Model(&iam.RefreshToken{}).
		Where("family_id = ? AND reuse_detected_at IS NOT NULL", family.ID).
		Count(&reusedCount).Error; err != nil {
		return toFositeStorageError(err)
	}
	reason := oauthRevocationReason
	if reusedCount > 0 {
		reason = oauthReplayRevocationReason
		updateTransactionAudit(ctx, func(event *iam.AuditEvent) {
			event.EventName = "oauth.token.refresh_reuse_detected"
			event.EntityID = event.EventName
			event.Result = iam.AuditResultDenied
			event.RiskLevel = iam.AuditRiskHigh
		})
	}
	return repositoryErrorToFosite(repository.RevokeTokenFamily(ctx, family.ID, s.now(), reason))
}

func (s *Storage) createOAuthFamily(
	ctx context.Context,
	repository *iam.Repository,
	requestID uuid.UUID,
	request fosite.Requester,
) (*iam.RefreshTokenFamily, error) {
	session, ok := request.GetSession().(*IAMSession)
	if !ok || session.PrincipalID <= 0 || session.IssuedAuthorizationVersion <= 0 ||
		session.ContextType == "" || session.AssuranceLevel == "" || session.AuthenticatedAt.IsZero() {
		return nil, fosite.ErrInvalidRequest
	}
	expiresAt := session.GetExpiresAt(fosite.RefreshToken)
	if expiresAt.IsZero() {
		expiresAt = session.GetExpiresAt(fosite.AccessToken)
	}
	if expiresAt.IsZero() {
		return nil, fosite.ErrInvalidRequest
	}
	now := s.now()
	family := &iam.RefreshTokenFamily{
		ProtocolRequestID:          &requestID,
		PrincipalID:                session.PrincipalID,
		ContextType:                iam.ContextType(session.ContextType),
		TenantMembershipID:         cloneInt64Pointer(session.TenantMembershipID),
		IssuedAuthorizationVersion: session.IssuedAuthorizationVersion,
		ClientID:                   request.GetClient().GetID(),
		AuthType:                   oauthFamilyAuthType,
		Audiences:                  pq.StringArray(append([]string(nil), request.GetGrantedAudience()...)),
		Scopes:                     pq.StringArray(append([]string(nil), request.GetGrantedScopes()...)),
		AuthenticationMethods:      pq.StringArray(append([]string(nil), session.AuthenticationMethods...)),
		AssuranceLevel:             iam.AssuranceLevel(session.AssuranceLevel),
		AuthenticatedAt:            session.AuthenticatedAt.UTC(),
		ExpiresAt:                  expiresAt.UTC(),
		CreatedAt:                  now,
		UpdatedAt:                  now,
	}
	if err := repository.CreateRefreshTokenFamily(ctx, family); err != nil {
		return nil, err
	}
	return family, nil
}

func (s *Storage) requestFromFamily(
	ctx context.Context,
	family *iam.RefreshTokenFamily,
	tokenType fosite.TokenType,
	tokenExpiresAt time.Time,
) (fosite.Requester, error) {
	if family == nil || family.ProtocolRequestID == nil {
		return nil, fosite.ErrNotFound
	}
	client, err := s.GetClient(ctx, family.ClientID)
	if err != nil {
		return nil, err
	}
	session := NewIAMSession()
	session.PrincipalID = family.PrincipalID
	session.ContextType = string(family.ContextType)
	session.TenantMembershipID = cloneInt64Pointer(family.TenantMembershipID)
	session.IssuedAuthorizationVersion = family.IssuedAuthorizationVersion
	session.Subject = strconv.FormatInt(family.PrincipalID, 10)
	session.AuthenticationMethods = append([]string(nil), family.AuthenticationMethods...)
	session.AssuranceLevel = string(family.AssuranceLevel)
	session.AuthenticatedAt = family.AuthenticatedAt.UTC()
	session.RequestedAt = family.CreatedAt.UTC()
	session.SetExpiresAt(tokenType, tokenExpiresAt.UTC())
	session.SetExpiresAt(fosite.RefreshToken, family.ExpiresAt.UTC())
	return &fosite.Request{
		ID:                family.ProtocolRequestID.String(),
		RequestedAt:       family.CreatedAt.UTC(),
		Client:            client,
		RequestedScope:    fosite.Arguments(append([]string(nil), family.Scopes...)),
		GrantedScope:      fosite.Arguments(append([]string(nil), family.Scopes...)),
		RequestedAudience: fosite.Arguments(append([]string(nil), family.Audiences...)),
		GrantedAudience:   fosite.Arguments(append([]string(nil), family.Audiences...)),
		Form:              url.Values{"client_id": []string{family.ClientID}},
		Session:           session,
	}, nil
}

func (s *Storage) getFamilyByProtocolRequestID(
	ctx context.Context,
	db *gorm.DB,
	requestID uuid.UUID,
) (*iam.RefreshTokenFamily, error) {
	var family iam.RefreshTokenFamily
	if err := db.WithContext(ctx).Where("protocol_request_id = ?", requestID).Take(&family).Error; err != nil {
		return nil, err
	}
	return &family, nil
}

func (s *Storage) familyAuthorizationIsCurrent(ctx context.Context, family *iam.RefreshTokenFamily) bool {
	if family == nil {
		return false
	}
	principal, err := iam.NewRepository(s.dbFromContext(ctx)).GetPrincipal(ctx, family.PrincipalID)
	return err == nil && principal.Status == iam.PrincipalStatusActive &&
		principal.AuthorizationVersion == family.IssuedAuthorizationVersion
}

func (s *Storage) familyAuthorizationCanRotate(ctx context.Context, family *iam.RefreshTokenFamily) bool {
	if family == nil {
		return false
	}
	repository := iam.NewRepository(s.dbFromContext(ctx))
	principal, err := repository.GetPrincipal(ctx, family.PrincipalID)
	if err != nil || family.IssuedAuthorizationVersion > principal.AuthorizationVersion {
		return false
	}
	active, err := repository.RefreshTokenFamilyContextIsActive(ctx, principal, family, s.now())
	return err == nil && active
}

func repositoryErrorToFosite(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, commonapi.ErrNotFound) {
		return fosite.ErrNotFound
	}
	if errors.Is(err, commonapi.ErrConflict) {
		return fosite.ErrSerializationFailure
	}
	return err
}

var _ fositeoauth2.CoreStorage = (*Storage)(nil)
var _ fositeoauth2.TokenRevocationStorage = (*Storage)(nil)
var _ rfc8628.RFC8628CoreStorage = (*Storage)(nil)
