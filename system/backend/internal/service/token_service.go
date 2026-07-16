package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/addp/system/internal/config"
	"github.com/addp/system/internal/models"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrInvalidAccessToken       = errors.New("invalid access token")
	ErrInvalidRefreshToken      = errors.New("invalid refresh token")
	ErrRefreshTokenReuse        = errors.New("refresh token reuse detected")
	ErrInvalidOAuthClient       = errors.New("invalid oauth client")
	ErrInvalidRedirectURI       = errors.New("invalid redirect uri")
	ErrInvalidScope             = errors.New("invalid scope")
	ErrInvalidPKCE              = errors.New("invalid pkce")
	ErrInvalidAuthorizationCode = errors.New("invalid authorization code")
	ErrAuthorizationPending     = errors.New("authorization pending")
	ErrSlowDown                 = errors.New("slow down")
	ErrAccessDenied             = errors.New("access denied")
	ErrExpiredToken             = errors.New("expired token")
	ErrUnsupportedGrantType     = errors.New("unsupported grant type")
)

type IssuedTokenPair struct {
	AccessToken     string
	RefreshToken    string
	AccessExpiresIn int
	Scope           string
}

type TokenService struct {
	db                 *gorm.DB
	authContextService *AuthContextService
	redis              *redis.Client
	cfg                *config.Config
	now                func() time.Time
}

func NewTokenService(db *gorm.DB, authContextService *AuthContextService, redisClient *redis.Client, cfg *config.Config) *TokenService {
	return &TokenService{db: db, authContextService: authContextService, redis: redisClient, cfg: cfg, now: time.Now}
}

func (s *TokenService) IssueFirstParty(user *models.User) (*IssuedTokenPair, error) {
	var tenantID uint
	if user.TenantID != nil {
		tenantID = *user.TenantID
	}
	if _, err := s.authContextService.ValidateIdentity(user.ID, tenantID); err != nil {
		return nil, err
	}
	return s.issueNewFamily(user.ID, user.TenantID, nil, models.AuthTypeFirstPartyAccessToken, []string{}, []string{})
}

func (s *TokenService) ResolveAccessToken(plainToken string) (*models.AuthorizationContext, error) {
	if !strings.HasPrefix(plainToken, "addp_at_") {
		return nil, ErrInvalidAccessToken
	}
	var token models.AccessToken
	if err := s.db.Where("token_hash = ?", hashToken(plainToken)).First(&token).Error; err != nil {
		return nil, ErrInvalidAccessToken
	}
	now := s.now()
	if token.RevokedAt != nil || !token.ExpiresAt.After(now) {
		return nil, ErrInvalidAccessToken
	}
	return s.authContextService.ResolveAccessToken(&token)
}

func (s *TokenService) RotateWebRefreshToken(refreshToken string) (*IssuedTokenPair, error) {
	return s.rotateRefreshToken(refreshToken, nil)
}

func (s *TokenService) RotateOAuthRefreshToken(refreshToken, clientID string) (*IssuedTokenPair, error) {
	return s.rotateRefreshToken(refreshToken, &clientID)
}

func (s *TokenService) RevokeRefreshToken(refreshToken string) error {
	var familyID string
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var token models.RefreshToken
		if err := tx.Where("token_hash = ?", hashToken(refreshToken)).First(&token).Error; err != nil {
			return ErrInvalidRefreshToken
		}
		familyID = token.FamilyID
		return revokeFamily(tx, familyID, "user_logout", s.now())
	})
	if err == nil {
		s.invalidateFamilyAccessTokens(familyID)
	}
	return err
}

func (s *TokenService) CreateAuthorizationCode(userID uint, req *models.OAuthAuthorizationRequest) (string, error) {
	client, scopes, err := s.validateClientRequest(req.ClientID, req.RedirectURI, req.Scope, false)
	if err != nil {
		return "", err
	}
	if req.CodeChallengeMethod != "S256" || req.CodeChallenge == "" {
		return "", ErrInvalidPKCE
	}
	user, err := s.authContextService.ValidateIdentity(userID, s.userTenantID(userID))
	if err != nil {
		return "", err
	}
	plainCode, err := randomToken("addp_ac_")
	if err != nil {
		return "", err
	}
	code := models.OAuthAuthorizationCode{
		ID:                  uuid.NewString(),
		CodeHash:            hashToken(plainCode),
		ClientID:            client.ClientID,
		UserID:              user.ID,
		TenantID:            user.TenantID,
		RedirectURI:         req.RedirectURI,
		Scopes:              scopes,
		CodeChallenge:       req.CodeChallenge,
		CodeChallengeMethod: "S256",
		ExpiresAt:           s.now().Add(time.Duration(s.cfg.AuthorizationCodeMinutes) * time.Minute),
	}
	if err := s.db.Create(&code).Error; err != nil {
		return "", err
	}
	redirect, err := url.Parse(req.RedirectURI)
	if err != nil {
		return "", ErrInvalidRedirectURI
	}
	query := redirect.Query()
	query.Set("code", plainCode)
	query.Set("state", req.State)
	redirect.RawQuery = query.Encode()
	return redirect.String(), nil
}

func (s *TokenService) ExchangeAuthorizationCode(clientID, plainCode, redirectURI, verifier string) (*IssuedTokenPair, error) {
	var result *IssuedTokenPair
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var code models.OAuthAuthorizationCode
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("code_hash = ?", hashToken(plainCode)).First(&code).Error; err != nil {
			return ErrInvalidAuthorizationCode
		}
		now := s.now()
		if code.UsedAt != nil || !code.ExpiresAt.After(now) || code.ClientID != clientID || code.RedirectURI != redirectURI {
			return ErrInvalidAuthorizationCode
		}
		if code.CodeChallengeMethod != "S256" || pkceChallenge(verifier) != code.CodeChallenge {
			return ErrInvalidPKCE
		}
		usedAt := now
		if err := tx.Model(&code).Update("used_at", usedAt).Error; err != nil {
			return err
		}
		client := code.ClientID
		pair, err := s.issueNewFamilyTx(tx, code.UserID, code.TenantID, &client, models.AuthTypeOAuthAccessToken, []string{"addp-api"}, code.Scopes)
		if err != nil {
			return err
		}
		result = pair
		return nil
	})
	return result, err
}

func (s *TokenService) CreateDeviceAuthorization(clientID, scope string) (*models.DeviceAuthorizationResponse, error) {
	client, scopes, err := s.validateClientRequest(clientID, "", scope, true)
	if err != nil {
		return nil, err
	}
	plainDeviceCode, err := randomToken("addp_dc_")
	if err != nil {
		return nil, err
	}
	userCode, err := randomUserCode()
	if err != nil {
		return nil, err
	}
	now := s.now()
	device := models.OAuthDeviceAuthorization{
		ID:             uuid.NewString(),
		DeviceCodeHash: hashToken(plainDeviceCode),
		UserCodeHash:   hashToken(normalizeUserCode(userCode)),
		ClientID:       client.ClientID,
		Scopes:         scopes,
		Status:         models.OAuthDeviceStatusPending,
		IntervalSecs:   s.cfg.DevicePollIntervalSecs,
		ExpiresAt:      now.Add(time.Duration(s.cfg.DeviceCodeExpireMinutes) * time.Minute),
	}
	if err := s.db.Create(&device).Error; err != nil {
		return nil, err
	}
	verificationURI := s.cfg.ConsoleURL + "/oauth/device"
	return &models.DeviceAuthorizationResponse{
		DeviceCode:              plainDeviceCode,
		UserCode:                userCode,
		VerificationURI:         verificationURI,
		VerificationURIComplete: verificationURI + "?user_code=" + url.QueryEscape(userCode),
		ExpiresIn:               s.cfg.DeviceCodeExpireMinutes * 60,
		Interval:                s.cfg.DevicePollIntervalSecs,
	}, nil
}

func (s *TokenService) ApproveDeviceAuthorization(userID uint, userCode string, approve bool) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var device models.OAuthDeviceAuthorization
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_code_hash = ?", hashToken(normalizeUserCode(userCode))).First(&device).Error; err != nil {
			return ErrExpiredToken
		}
		if device.Status != models.OAuthDeviceStatusPending || !device.ExpiresAt.After(s.now()) {
			return ErrExpiredToken
		}
		if !approve {
			return tx.Model(&device).Update("status", models.OAuthDeviceStatusDenied).Error
		}
		user, err := s.authContextService.ValidateIdentity(userID, s.userTenantID(userID))
		if err != nil {
			return err
		}
		return tx.Model(&device).Updates(map[string]any{
			"status":    models.OAuthDeviceStatusApproved,
			"user_id":   user.ID,
			"tenant_id": user.TenantID,
		}).Error
	})
}

func (s *TokenService) ExchangeDeviceCode(clientID, plainDeviceCode string) (*IssuedTokenPair, error) {
	var result *IssuedTokenPair
	var flowErr error
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var device models.OAuthDeviceAuthorization
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("device_code_hash = ?", hashToken(plainDeviceCode)).First(&device).Error; err != nil {
			return ErrExpiredToken
		}
		now := s.now()
		if device.ClientID != clientID || !device.ExpiresAt.After(now) {
			return ErrExpiredToken
		}
		if device.LastPolledAt != nil && now.Sub(*device.LastPolledAt) < time.Duration(device.IntervalSecs)*time.Second {
			return ErrSlowDown
		}
		if err := tx.Model(&device).Update("last_polled_at", now).Error; err != nil {
			return err
		}
		switch device.Status {
		case models.OAuthDeviceStatusPending:
			flowErr = ErrAuthorizationPending
			return nil
		case models.OAuthDeviceStatusDenied:
			flowErr = ErrAccessDenied
			return nil
		case models.OAuthDeviceStatusUsed:
			flowErr = ErrExpiredToken
			return nil
		case models.OAuthDeviceStatusApproved:
			if device.UserID == nil {
				return ErrAccessDenied
			}
		default:
			return ErrExpiredToken
		}
		client := device.ClientID
		pair, err := s.issueNewFamilyTx(tx, *device.UserID, device.TenantID, &client, models.AuthTypeOAuthAccessToken, []string{"addp-api"}, device.Scopes)
		if err != nil {
			return err
		}
		if err := tx.Model(&device).Update("status", models.OAuthDeviceStatusUsed).Error; err != nil {
			return err
		}
		result = pair
		return nil
	})
	if err == nil && flowErr != nil {
		return nil, flowErr
	}
	return result, err
}

func (s *TokenService) rotateRefreshToken(plainToken string, expectedClientID *string) (*IssuedTokenPair, error) {
	if !strings.HasPrefix(plainToken, "addp_rt_") {
		return nil, ErrInvalidRefreshToken
	}
	var result *IssuedTokenPair
	var familyID string
	reuseDetected := false
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var token models.RefreshToken
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("token_hash = ?", hashToken(plainToken)).First(&token).Error; err != nil {
			return ErrInvalidRefreshToken
		}
		familyID = token.FamilyID
		var family models.RefreshTokenFamily
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&family, "id = ?", token.FamilyID).Error; err != nil {
			return ErrInvalidRefreshToken
		}
		now := s.now()
		if token.UsedAt != nil {
			reuseDetected = true
			return revokeFamily(tx, family.ID, "refresh_token_reuse", now)
		}
		if token.RevokedAt != nil || family.RevokedAt != nil || !token.ExpiresAt.After(now) || !family.ExpiresAt.After(now) {
			return ErrInvalidRefreshToken
		}
		if !sameClient(family.ClientID, expectedClientID) {
			return ErrInvalidOAuthClient
		}
		var tenantID uint
		if family.TenantID != nil {
			tenantID = *family.TenantID
		}
		if _, err := s.authContextService.ValidateIdentity(family.UserID, tenantID); err != nil {
			return err
		}
		usedAt := now
		if err := tx.Model(&token).Update("used_at", usedAt).Error; err != nil {
			return err
		}
		pair, refreshID, err := s.issueTokensForFamilyTx(tx, &family, &token.ID)
		if err != nil {
			return err
		}
		if err := tx.Model(&token).Update("replaced_by_token_id", refreshID).Error; err != nil {
			return err
		}
		result = pair
		return nil
	})
	if reuseDetected && err == nil {
		s.invalidateFamilyAccessTokens(familyID)
		return nil, ErrRefreshTokenReuse
	}
	return result, err
}

func (s *TokenService) issueNewFamily(userID uint, tenantID *uint, clientID *string, authType string, audiences, scopes []string) (*IssuedTokenPair, error) {
	var pair *IssuedTokenPair
	err := s.db.Transaction(func(tx *gorm.DB) error {
		issued, err := s.issueNewFamilyTx(tx, userID, tenantID, clientID, authType, audiences, scopes)
		pair = issued
		return err
	})
	return pair, err
}

func (s *TokenService) issueNewFamilyTx(tx *gorm.DB, userID uint, tenantID *uint, clientID *string, authType string, audiences, scopes []string) (*IssuedTokenPair, error) {
	now := s.now()
	family := models.RefreshTokenFamily{
		ID:        uuid.NewString(),
		UserID:    userID,
		TenantID:  tenantID,
		ClientID:  clientID,
		AuthType:  authType,
		Audiences: audiences,
		Scopes:    scopes,
		ExpiresAt: now.Add(time.Duration(s.cfg.RefreshTokenExpireDays) * 24 * time.Hour),
	}
	if err := tx.Create(&family).Error; err != nil {
		return nil, err
	}
	pair, _, err := s.issueTokensForFamilyTx(tx, &family, nil)
	return pair, err
}

func (s *TokenService) issueTokensForFamilyTx(tx *gorm.DB, family *models.RefreshTokenFamily, parentTokenID *string) (*IssuedTokenPair, string, error) {
	now := s.now()
	accessPlain, err := randomToken("addp_at_")
	if err != nil {
		return nil, "", err
	}
	refreshPlain, err := randomToken("addp_rt_")
	if err != nil {
		return nil, "", err
	}
	refreshExpiry := now.Add(time.Duration(s.cfg.RefreshTokenExpireDays) * 24 * time.Hour)
	if family.ExpiresAt.Before(refreshExpiry) {
		refreshExpiry = family.ExpiresAt
	}
	refreshID := uuid.NewString()
	refresh := models.RefreshToken{
		ID:            refreshID,
		FamilyID:      family.ID,
		TokenHash:     hashToken(refreshPlain),
		ParentTokenID: parentTokenID,
		ExpiresAt:     refreshExpiry,
	}
	access := models.AccessToken{
		ID:        uuid.NewString(),
		TokenHash: hashToken(accessPlain),
		FamilyID:  family.ID,
		UserID:    family.UserID,
		TenantID:  family.TenantID,
		ClientID:  family.ClientID,
		AuthType:  family.AuthType,
		Audiences: family.Audiences,
		Scopes:    family.Scopes,
		ExpiresAt: now.Add(time.Duration(s.cfg.AccessTokenExpireMinutes) * time.Minute),
	}
	if err := tx.Create(&refresh).Error; err != nil {
		return nil, "", err
	}
	if err := tx.Create(&access).Error; err != nil {
		return nil, "", err
	}
	return &IssuedTokenPair{
		AccessToken:     accessPlain,
		RefreshToken:    refreshPlain,
		AccessExpiresIn: s.cfg.AccessTokenExpireMinutes * 60,
		Scope:           strings.Join([]string(family.Scopes), " "),
	}, refreshID, nil
}

func (s *TokenService) validateClientRequest(clientID, redirectURI, scope string, device bool) (*models.OAuthClient, []string, error) {
	var client models.OAuthClient
	if err := s.db.First(&client, "client_id = ? AND is_active = ?", clientID, true).Error; err != nil {
		return nil, nil, ErrInvalidOAuthClient
	}
	if device && !client.DeviceFlowEnabled {
		return nil, nil, ErrInvalidOAuthClient
	}
	if !device && !contains(client.RedirectURIs, redirectURI) {
		return nil, nil, ErrInvalidRedirectURI
	}
	scopes := normalizeScopes(scope)
	if len(scopes) == 0 {
		scopes = []string{"addp.api"}
	}
	for _, requested := range scopes {
		if !contains(client.AllowedScopes, requested) {
			return nil, nil, ErrInvalidScope
		}
	}
	return &client, scopes, nil
}

func (s *TokenService) userTenantID(userID uint) uint {
	var user models.User
	if err := s.db.Select("tenant_id").First(&user, userID).Error; err != nil || user.TenantID == nil {
		return 0
	}
	return *user.TenantID
}

func (s *TokenService) invalidateFamilyAccessTokens(familyID string) {
	if s.redis == nil || familyID == "" {
		return
	}
	var hashes []string
	if err := s.db.Model(&models.AccessToken{}).Where("family_id = ?", familyID).Pluck("token_hash", &hashes).Error; err != nil {
		return
	}
	keys := make([]string, 0, len(hashes))
	for _, hash := range hashes {
		keys = append(keys, "auth:context:"+hash)
	}
	if len(keys) > 0 {
		_ = s.redis.Del(context.Background(), keys...).Err()
	}
}

func revokeFamily(tx *gorm.DB, familyID, reason string, now time.Time) error {
	if err := tx.Model(&models.RefreshTokenFamily{}).Where("id = ? AND revoked_at IS NULL", familyID).Updates(map[string]any{"revoked_at": now, "revoked_reason": reason}).Error; err != nil {
		return err
	}
	if err := tx.Model(&models.RefreshToken{}).Where("family_id = ? AND revoked_at IS NULL", familyID).Update("revoked_at", now).Error; err != nil {
		return err
	}
	return tx.Model(&models.AccessToken{}).Where("family_id = ? AND revoked_at IS NULL", familyID).Update("revoked_at", now).Error
}

func randomToken(prefix string) (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(value), nil
}

func randomUserCode() (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	value := make([]byte, 8)
	random := make([]byte, len(value))
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	for i := range value {
		value[i] = alphabet[int(random[i])%len(alphabet)]
	}
	return string(value[:4]) + "-" + string(value[4:]), nil
}

func hashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

func pkceChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

func normalizeUserCode(code string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(code), "-", ""))
}

func normalizeScopes(scope string) []string {
	set := map[string]struct{}{}
	for _, value := range strings.Fields(scope) {
		set[value] = struct{}{}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func sameClient(actual, expected *string) bool {
	if actual == nil || expected == nil {
		return actual == nil && expected == nil
	}
	return *actual == *expected
}

func OAuthErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrAuthorizationPending):
		return "authorization_pending"
	case errors.Is(err, ErrSlowDown):
		return "slow_down"
	case errors.Is(err, ErrAccessDenied):
		return "access_denied"
	case errors.Is(err, ErrExpiredToken):
		return "expired_token"
	case errors.Is(err, ErrInvalidOAuthClient):
		return "invalid_client"
	case errors.Is(err, ErrUnsupportedGrantType):
		return "unsupported_grant_type"
	case errors.Is(err, ErrInvalidScope):
		return "invalid_scope"
	case errors.Is(err, ErrInvalidRedirectURI):
		return "invalid_request"
	case errors.Is(err, ErrInvalidAuthorizationCode), errors.Is(err, ErrInvalidRefreshToken), errors.Is(err, ErrInvalidPKCE), errors.Is(err, ErrRefreshTokenReuse):
		return "invalid_grant"
	default:
		return "server_error"
	}
}
