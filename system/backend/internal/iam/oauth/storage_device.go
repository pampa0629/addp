package oauth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lib/pq"
	"github.com/ory/fosite"
	"github.com/ory/fosite/handler/rfc8628"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type DeviceDecision string

const (
	DeviceDecisionApprove DeviceDecision = "approve"
	DeviceDecisionReject  DeviceDecision = "reject"
)

type ApprovedIdentityFacts struct {
	PrincipalID                int64
	ContextType                string
	TenantMembershipID         *int64
	IssuedAuthorizationVersion int64
	GrantedScopes              []string
	GrantedAudiences           []string
	AuthenticationMethods      []string
	AssuranceLevel             string
	AuthenticatedAt            time.Time
}

func (s *Storage) CreateDeviceAuthSession(
	ctx context.Context,
	deviceCodeSignature string,
	userCodeSignature string,
	request fosite.DeviceRequester,
) error {
	if err := validateStorageSignature(deviceCodeSignature); err != nil {
		return err
	}
	if err := validateStorageSignature(userCodeSignature); err != nil {
		return err
	}
	requestID, err := uuid.Parse(request.GetID())
	if err != nil {
		return fosite.ErrInvalidRequest
	}
	expiresAt := request.GetSession().GetExpiresAt(fosite.DeviceCode)
	if expiresAt.IsZero() {
		expiresAt = request.GetSession().GetExpiresAt(fosite.UserCode)
	}
	if expiresAt.IsZero() {
		return fosite.ErrInvalidRequest
	}
	requestedAt := request.GetRequestedAt().UTC()
	row := &deviceAuthorizationRow{
		ID:                  requestID,
		DeviceCodeHash:      deviceCodeSignature,
		UserCodeHash:        userCodeSignature,
		ClientID:            request.GetClient().GetID(),
		RequestedScopes:     pq.StringArray(append([]string(nil), request.GetRequestedScopes()...)),
		RequestedAudiences:  pq.StringArray(append([]string(nil), request.GetRequestedAudience()...)),
		Status:              "pending",
		PollIntervalSeconds: int(s.devicePollingInterval / time.Second),
		NextPollAt:          requestedAt.Add(s.devicePollingInterval),
		RequestedAt:         requestedAt,
		ExpiresAt:           expiresAt.UTC(),
		CreatedAt:           s.now(),
	}
	if err := s.dbFromContext(ctx).Create(row).Error; err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23505" &&
			postgresError.ConstraintName == "oauth_device_authorizations_user_code_hash_key" {
			return fosite.ErrExistingUserCodeSignature
		}
		return toFositeStorageError(err)
	}
	return nil
}

func (s *Storage) GetDeviceCodeSession(
	ctx context.Context,
	signature string,
	_ fosite.Session,
) (fosite.DeviceRequester, error) {
	if err := validateStorageSignature(signature); err != nil {
		return nil, err
	}
	var row deviceAuthorizationRow
	if err := s.dbFromContext(ctx).
		Where("device_code_hash = ? OR user_code_hash = ?", signature, signature).
		Take(&row).Error; err != nil {
		return nil, toFositeStorageError(err)
	}
	requester, err := s.requestFromDeviceRow(ctx, &row)
	if err != nil {
		return nil, err
	}
	if row.Status == "invalidated" {
		return requester, fosite.ErrInvalidatedDeviceCode
	}
	return requester, nil
}

func (s *Storage) InvalidateDeviceCodeSession(ctx context.Context, signature string) error {
	if err := validateStorageSignature(signature); err != nil {
		return err
	}
	now, err := s.databaseNow(ctx)
	if err != nil {
		return err
	}
	db := s.dbFromContext(ctx)
	var row deviceAuthorizationRow
	if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("device_code_hash = ?", signature).Take(&row).Error; err != nil {
		return toFositeStorageError(err)
	}
	if row.Status == "invalidated" {
		return fosite.ErrInvalidatedDeviceCode
	}
	if row.Status != "approved" || !row.ExpiresAt.After(now) {
		return fosite.ErrInvalidGrant
	}
	result := db.Model(&deviceAuthorizationRow{}).
		Where("id = ? AND status = 'approved' AND invalidated_at IS NULL", row.ID).
		Updates(map[string]interface{}{"status": "invalidated", "invalidated_at": now})
	if result.Error != nil {
		return toFositeStorageError(result.Error)
	}
	if result.RowsAffected != 1 {
		return fosite.ErrSerializationFailure
	}
	return nil
}

func (s *Storage) ShouldRateLimit(ctx context.Context, deviceCodeSignature string) (bool, error) {
	if err := validateStorageSignature(deviceCodeSignature); err != nil {
		return false, nil
	}
	limited := false
	db := s.dbFromContext(ctx)
	startedTransaction := ctx.Value(transactionContextKey{}) == nil
	if startedTransaction {
		db = db.Begin()
		if db.Error != nil {
			return false, db.Error
		}
	}
	finish := func(operationError error) error {
		if !startedTransaction {
			return operationError
		}
		if operationError != nil {
			_ = db.Rollback().Error
			return operationError
		}
		return db.Commit().Error
	}

	var row deviceAuthorizationRow
	lookupError := db.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("device_code_hash = ?", deviceCodeSignature).Take(&row).Error
	if lookupError != nil {
		if errors.Is(lookupError, gorm.ErrRecordNotFound) {
			return false, finish(nil)
		}
		return false, finish(lookupError)
	}
	now, nowError := s.databaseNow(ctx)
	if nowError != nil {
		return false, finish(nowError)
	}
	if !row.ExpiresAt.After(now) || row.Status == "invalidated" {
		return false, finish(nil)
	}
	interval := row.PollIntervalSeconds
	if now.Before(row.NextPollAt) {
		limited = true
		interval += 5
	}
	updates := map[string]interface{}{
		"last_polled_at":        now,
		"next_poll_at":          now.Add(time.Duration(interval) * time.Second),
		"poll_interval_seconds": interval,
	}
	if updateError := db.Model(&deviceAuthorizationRow{}).Where("id = ?", row.ID).Updates(updates).Error; updateError != nil {
		return false, finish(updateError)
	}
	return limited, finish(nil)
}

func (s *Storage) DecideDeviceAuthorization(
	ctx context.Context,
	userCodeSignature string,
	decision DeviceDecision,
	facts *ApprovedIdentityFacts,
) error {
	if err := validateStorageSignature(userCodeSignature); err != nil {
		return err
	}
	if ctx.Value(transactionContextKey{}) == nil {
		txCtx, err := s.BeginTX(ctx)
		if err != nil {
			return err
		}
		if err := s.decideDeviceAuthorization(txCtx, userCodeSignature, decision, facts); err != nil {
			_ = s.Rollback(txCtx)
			return err
		}
		return s.Commit(txCtx)
	}
	return s.decideDeviceAuthorization(ctx, userCodeSignature, decision, facts)
}

func (s *Storage) decideDeviceAuthorization(
	ctx context.Context,
	userCodeSignature string,
	decision DeviceDecision,
	facts *ApprovedIdentityFacts,
) error {
	db := s.dbFromContext(ctx)
	var row deviceAuthorizationRow
	if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("user_code_hash = ?", userCodeSignature).Take(&row).Error; err != nil {
		return toFositeStorageError(err)
	}
	now, err := s.databaseNow(ctx)
	if err != nil {
		return err
	}
	if row.Status != "pending" || !row.ExpiresAt.After(now) {
		return fosite.ErrInvalidGrant
	}
	switch decision {
	case DeviceDecisionReject:
		return toFositeStorageError(db.Model(&deviceAuthorizationRow{}).Where("id = ? AND status = 'pending'", row.ID).
			Updates(map[string]interface{}{"status": "rejected", "decided_at": now}).Error)
	case DeviceDecisionApprove:
		if facts == nil || facts.PrincipalID <= 0 || facts.IssuedAuthorizationVersion <= 0 ||
			facts.AuthenticatedAt.IsZero() || facts.AuthenticatedAt.After(now) {
			return fosite.ErrInvalidRequest
		}
		return toFositeStorageError(db.Model(&deviceAuthorizationRow{}).Where("id = ? AND status = 'pending'", row.ID).
			Updates(map[string]interface{}{
				"status":                       "approved",
				"granted_scopes":               pq.StringArray(facts.GrantedScopes),
				"granted_audiences":            pq.StringArray(facts.GrantedAudiences),
				"principal_id":                 facts.PrincipalID,
				"context_type":                 facts.ContextType,
				"tenant_membership_id":         facts.TenantMembershipID,
				"issued_authorization_version": facts.IssuedAuthorizationVersion,
				"authentication_methods":       pq.StringArray(facts.AuthenticationMethods),
				"assurance_level":              facts.AssuranceLevel,
				"authenticated_at":             facts.AuthenticatedAt.UTC(),
				"decided_at":                   now,
			}).Error)
	default:
		return fosite.ErrInvalidRequest
	}
}

var _ rfc8628.DeviceAuthStorage = (*Storage)(nil)
