package oauth

import (
	"context"

	"github.com/google/uuid"
	"github.com/ory/fosite"
	fositeoauth2 "github.com/ory/fosite/handler/oauth2"
	"github.com/ory/fosite/handler/pkce"
	"gorm.io/gorm/clause"
)

func (s *Storage) CreateAuthorizeCodeSession(ctx context.Context, signature string, request fosite.Requester) error {
	if err := validateStorageSignature(signature); err != nil {
		return err
	}
	requestID, err := uuid.Parse(request.GetID())
	if err != nil {
		return fosite.ErrInvalidRequest
	}
	expiresAt := request.GetSession().GetExpiresAt(fosite.AuthorizeCode)
	if expiresAt.IsZero() {
		return fosite.ErrInvalidRequest
	}
	var authorizationRequest authorizationRequestRow
	if err := s.dbFromContext(ctx).Where("id = ?", requestID).Take(&authorizationRequest).Error; err != nil {
		return toFositeStorageError(err)
	}
	if expiresAt.After(authorizationRequest.ExpiresAt) {
		expiresAt = authorizationRequest.ExpiresAt
	}
	request.GetSession().SetExpiresAt(fosite.AuthorizeCode, expiresAt.UTC())
	return toFositeStorageError(s.dbFromContext(ctx).Create(&authorizationCodeRow{
		CodeHash:               signature,
		AuthorizationRequestID: requestID,
		ExpiresAt:              expiresAt.UTC(),
		CreatedAt:              s.now(),
	}).Error)
}

func (s *Storage) GetAuthorizeCodeSession(
	ctx context.Context,
	signature string,
	_ fosite.Session,
) (fosite.Requester, error) {
	if err := validateStorageSignature(signature); err != nil {
		return nil, err
	}
	var code authorizationCodeRow
	if err := s.dbFromContext(ctx).Where("code_hash = ?", signature).Take(&code).Error; err != nil {
		return nil, toFositeStorageError(err)
	}
	var request authorizationRequestRow
	if err := s.dbFromContext(ctx).Where("id = ?", code.AuthorizationRequestID).Take(&request).Error; err != nil {
		return nil, toFositeStorageError(err)
	}
	requester, err := s.requestFromAuthorizationRow(ctx, &request, fosite.AuthorizeCode, code.ExpiresAt.UTC())
	if err != nil {
		return nil, err
	}
	if code.InvalidatedAt != nil {
		return requester, fosite.ErrInvalidatedAuthorizeCode
	}
	return requester, nil
}

func (s *Storage) InvalidateAuthorizeCodeSession(ctx context.Context, signature string) error {
	if err := validateStorageSignature(signature); err != nil {
		return err
	}
	now := s.now()
	var code authorizationCodeRow
	db := s.dbFromContext(ctx)
	if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("code_hash = ?", signature).Take(&code).Error; err != nil {
		return toFositeStorageError(err)
	}
	if code.InvalidatedAt != nil || !code.ExpiresAt.After(now) {
		return fosite.ErrInvalidatedAuthorizeCode
	}
	if err := db.Model(&authorizationCodeRow{}).
		Where("id = ? AND invalidated_at IS NULL", code.ID).
		Update("invalidated_at", now).Error; err != nil {
		return toFositeStorageError(err)
	}
	if err := db.Model(&pkceSessionRow{}).
		Where("authorization_code_hash = ? AND consumed_at IS NULL", signature).
		Update("consumed_at", now).Error; err != nil {
		return toFositeStorageError(err)
	}
	return nil
}

func (s *Storage) CreatePKCERequestSession(ctx context.Context, signature string, requester fosite.Requester) error {
	if err := validateStorageSignature(signature); err != nil {
		return err
	}
	requestID, err := uuid.Parse(requester.GetID())
	if err != nil {
		return fosite.ErrInvalidRequest
	}
	result := s.dbFromContext(ctx).Model(&pkceSessionRow{}).
		Where("authorization_request_id = ? AND authorization_code_hash IS NULL", requestID).
		Update("authorization_code_hash", signature)
	if result.Error != nil {
		return toFositeStorageError(result.Error)
	}
	if result.RowsAffected != 1 {
		return fosite.ErrNotFound
	}
	return nil
}

func (s *Storage) GetPKCERequestSession(
	ctx context.Context,
	signature string,
	_ fosite.Session,
) (fosite.Requester, error) {
	if err := validateStorageSignature(signature); err != nil {
		return nil, err
	}
	var pkceRow pkceSessionRow
	if err := s.dbFromContext(ctx).Where("authorization_code_hash = ?", signature).Take(&pkceRow).Error; err != nil {
		return nil, toFositeStorageError(err)
	}
	var request authorizationRequestRow
	if err := s.dbFromContext(ctx).Where("id = ?", pkceRow.AuthorizationRequestID).Take(&request).Error; err != nil {
		return nil, toFositeStorageError(err)
	}
	requester, err := s.requestFromAuthorizationRow(ctx, &request, fosite.AuthorizeCode, pkceRow.ExpiresAt.UTC())
	if err != nil {
		return nil, err
	}
	requester.GetRequestForm().Set("code_challenge", pkceRow.CodeChallenge)
	requester.GetRequestForm().Set("code_challenge_method", pkceRow.CodeChallengeMethod)
	return requester, nil
}

func (s *Storage) DeletePKCERequestSession(ctx context.Context, signature string) error {
	if err := validateStorageSignature(signature); err != nil {
		return err
	}
	result := s.dbFromContext(ctx).Model(&pkceSessionRow{}).
		Where("authorization_code_hash = ? AND verified_at IS NULL AND expires_at > ?", signature, s.now()).
		Update("verified_at", s.now())
	if result.Error != nil {
		return toFositeStorageError(result.Error)
	}
	if result.RowsAffected != 1 {
		return fosite.ErrNotFound
	}
	return nil
}

var _ fositeoauth2.AuthorizeCodeStorage = (*Storage)(nil)
var _ pkce.PKCERequestStorage = (*Storage)(nil)
