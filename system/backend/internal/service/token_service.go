package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net"
	"net/url"
	"sort"
	"strconv"
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
	ErrInvalidAccessToken              = errors.New("invalid access token")
	ErrInvalidRefreshToken             = errors.New("invalid refresh token")
	ErrRefreshTokenReuse               = errors.New("refresh token reuse detected")
	ErrInvalidOAuthClient              = errors.New("invalid oauth client")
	ErrInvalidRedirectURI              = errors.New("invalid redirect uri")
	ErrInvalidScope                    = errors.New("invalid scope")
	ErrInvalidPKCE                     = errors.New("invalid pkce")
	ErrInvalidAuthorizationCode        = errors.New("invalid authorization code")
	ErrInvalidAuthorizationDecision    = errors.New("invalid authorization decision")
	ErrAuthorizationRequestUnavailable = errors.New("authorization request unavailable")
	ErrAuthorizationPending            = errors.New("authorization pending")
	ErrSlowDown                        = errors.New("slow down")
	ErrAccessDenied                    = errors.New("access denied")
	ErrExpiredToken                    = errors.New("expired token")
	ErrUnsupportedGrantType            = errors.New("unsupported grant type")
	ErrInvalidDelegation               = errors.New("invalid delegation")
)

type IssuedTokenPair struct {
	AccessToken                   string
	RefreshToken                  string
	AccessExpiresIn               int
	Scope                         string
	ResourceAccessTickets         map[string]string
	ResourceAccessTicketExpiresIn int
}

type AuthorizationDecisionResult struct {
	RedirectURL string
	ClientID    string
	Scope       string
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
	if strings.HasPrefix(plainToken, "addp_rat_") {
		return s.resolveResourceAccessTicket(plainToken)
	}
	if strings.HasPrefix(plainToken, "addp_dat_") {
		return s.resolveDelegatedAccessToken(plainToken)
	}
	if !strings.HasPrefix(plainToken, "addp_at_") {
		return nil, ErrInvalidAccessToken
	}
	token, err := s.loadActiveAccessToken(plainToken)
	if err != nil {
		return nil, err
	}
	return s.authContextService.ResolveAccessToken(token)
}

func (s *TokenService) loadActiveAccessToken(plainToken string) (*models.AccessToken, error) {
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
	var family models.RefreshTokenFamily
	if err := s.db.First(&family, "id = ?", token.FamilyID).Error; err != nil || family.RevokedAt != nil || !family.ExpiresAt.After(now) {
		return nil, ErrInvalidAccessToken
	}
	return &token, nil
}

func (s *TokenService) IssueDelegatedAccessToken(sourcePlainToken string, req *models.DelegatedAccessTokenRequest) (*models.DelegatedAccessTokenResponse, error) {
	source, err := s.loadActiveAccessToken(sourcePlainToken)
	if err != nil {
		return nil, err
	}
	if source.AuthType != models.AuthTypeFirstPartyAccessToken && source.AuthType != models.AuthTypeOAuthAccessToken {
		return nil, ErrInvalidDelegation
	}

	audience := strings.TrimSpace(req.Audience)
	agentRunID := strings.TrimSpace(req.AgentRunID)
	toolCallID := strings.TrimSpace(req.ToolCallID)
	if audience == "" || agentRunID == "" || toolCallID == "" || len(agentRunID) > 100 || len(toolCallID) > 100 {
		return nil, ErrInvalidDelegation
	}
	scopes := normalizeScopeValues(req.Scopes)
	if len(scopes) == 0 {
		return nil, ErrInvalidScope
	}
	for _, scope := range scopes {
		if !models.IsDelegatedToolScopeAllowed(audience, scope) {
			return nil, ErrInvalidScope
		}
	}
	if source.AuthType == models.AuthTypeOAuthAccessToken && !contains(source.Scopes, "addp.api") {
		for _, scope := range scopes {
			if !contains(source.Scopes, scope) {
				return nil, ErrInvalidScope
			}
		}
	}

	var tenantID uint
	if source.TenantID != nil {
		tenantID = *source.TenantID
	}
	if _, err := s.authContextService.ValidateIdentity(source.UserID, tenantID); err != nil {
		return nil, err
	}

	now := s.now()
	expiresAt := now.Add(time.Duration(s.delegatedAccessTokenExpireMinutes()) * time.Minute)
	if source.ExpiresAt.Before(expiresAt) {
		expiresAt = source.ExpiresAt
	}
	if !expiresAt.After(now) {
		return nil, ErrInvalidAccessToken
	}
	plainToken, err := randomToken("addp_dat_")
	if err != nil {
		return nil, err
	}
	delegatedBy := "addp-web"
	if source.ClientID != nil && strings.TrimSpace(*source.ClientID) != "" {
		delegatedBy = *source.ClientID
	}
	token := models.DelegatedAccessToken{
		ID:                  uuid.NewString(),
		TokenHash:           hashToken(plainToken),
		SourceAccessTokenID: source.ID,
		UserID:              source.UserID,
		TenantID:            source.TenantID,
		ClientID:            source.ClientID,
		DelegatedBy:         delegatedBy,
		Audience:            audience,
		Scopes:              scopes,
		AgentRunID:          agentRunID,
		ToolCallID:          toolCallID,
		ExpiresAt:           expiresAt,
	}
	if err := s.db.Create(&token).Error; err != nil {
		return nil, err
	}
	expiresIn := int((expiresAt.Sub(now) + time.Second - 1) / time.Second)
	return &models.DelegatedAccessTokenResponse{
		AccessToken: plainToken,
		TokenType:   "Bearer",
		ExpiresIn:   expiresIn,
		Audience:    audience,
		Scopes:      []string(token.Scopes),
		AgentRunID:  agentRunID,
		ToolCallID:  toolCallID,
	}, nil
}

func (s *TokenService) resolveDelegatedAccessToken(plainToken string) (*models.AuthorizationContext, error) {
	var token models.DelegatedAccessToken
	if err := s.db.Where("token_hash = ?", hashToken(plainToken)).First(&token).Error; err != nil {
		return nil, ErrInvalidAccessToken
	}
	now := s.now()
	if token.RevokedAt != nil || !token.ExpiresAt.After(now) {
		return nil, ErrInvalidAccessToken
	}
	var source models.AccessToken
	if err := s.db.First(&source, "id = ?", token.SourceAccessTokenID).Error; err != nil || source.RevokedAt != nil || !source.ExpiresAt.After(now) {
		return nil, ErrInvalidAccessToken
	}
	if source.UserID != token.UserID || !sameUint(source.TenantID, token.TenantID) || !sameClient(source.ClientID, token.ClientID) {
		return nil, ErrInvalidAccessToken
	}
	var family models.RefreshTokenFamily
	if err := s.db.First(&family, "id = ?", source.FamilyID).Error; err != nil || family.RevokedAt != nil || !family.ExpiresAt.After(now) {
		return nil, ErrInvalidAccessToken
	}
	return s.authContextService.ResolveDelegatedAccessToken(&token)
}

func (s *TokenService) resolveResourceAccessTicket(plainToken string) (*models.AuthorizationContext, error) {
	var ticket models.ResourceAccessTicket
	if err := s.db.Where("token_hash = ?", hashToken(plainToken)).First(&ticket).Error; err != nil {
		return nil, ErrInvalidAccessToken
	}
	now := s.now()
	if ticket.RevokedAt != nil || !ticket.ExpiresAt.After(now) {
		return nil, ErrInvalidAccessToken
	}
	var family models.RefreshTokenFamily
	if err := s.db.First(&family, "id = ?", ticket.FamilyID).Error; err != nil {
		return nil, ErrInvalidAccessToken
	}
	if family.AuthType != models.AuthTypeFirstPartyAccessToken || family.RevokedAt != nil || !family.ExpiresAt.After(now) {
		return nil, ErrInvalidAccessToken
	}
	token := models.AccessToken{
		UserID:    family.UserID,
		TenantID:  family.TenantID,
		ClientID:  family.ClientID,
		AuthType:  models.AuthTypeResourceAccessTicket,
		Audiences: []string{ticket.Owner},
		Scopes:    []string{models.BrowserResourceAccessScope},
		ExpiresAt: ticket.ExpiresAt,
		CreatedAt: ticket.CreatedAt,
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
	return s.revokeRefreshToken(refreshToken, nil)
}

func (s *TokenService) RevokeOAuthRefreshToken(refreshToken, clientID string) error {
	if strings.TrimSpace(clientID) == "" {
		return ErrInvalidOAuthClient
	}
	return s.revokeRefreshToken(refreshToken, &clientID)
}

func (s *TokenService) revokeRefreshToken(refreshToken string, expectedClientID *string) error {
	var familyID string
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var token models.RefreshToken
		if err := tx.Where("token_hash = ?", hashToken(refreshToken)).First(&token).Error; err != nil {
			return ErrInvalidRefreshToken
		}
		familyID = token.FamilyID
		var family models.RefreshTokenFamily
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&family, "id = ?", familyID).Error; err != nil {
			return ErrInvalidRefreshToken
		}
		if !sameClient(family.ClientID, expectedClientID) {
			return ErrInvalidOAuthClient
		}
		return revokeFamily(tx, familyID, "user_logout", s.now())
	})
	if err == nil {
		s.invalidateFamilyAccessTokens(familyID)
	}
	return err
}

func (s *TokenService) CreateAuthorizationRequest(clientID, redirectURI, scope, codeChallenge, codeChallengeMethod string) (*models.OAuthAuthorizationRequestCreatedResponse, error) {
	client, scopes, err := s.validateClientRequest(clientID, redirectURI, scope, false)
	if err != nil {
		return nil, err
	}
	if codeChallengeMethod != "S256" || !validPKCEChallenge(codeChallenge) {
		return nil, ErrInvalidPKCE
	}
	requestSecret, err := randomToken("addp_ars_")
	if err != nil {
		return nil, err
	}
	now := s.now()
	expiresIn := s.cfg.AuthorizationCodeMinutes * 60
	request := models.OAuthAuthorizationRequest{
		ID:                  uuid.NewString(),
		RequestSecretHash:   hashToken(requestSecret),
		ClientID:            client.ClientID,
		RedirectURI:         redirectURI,
		Scopes:              scopes,
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: "S256",
		Status:              models.OAuthAuthorizationRequestPending,
		ExpiresAt:           now.Add(time.Duration(expiresIn) * time.Second),
	}
	if err := s.db.Create(&request).Error; err != nil {
		return nil, err
	}
	return &models.OAuthAuthorizationRequestCreatedResponse{
		RequestID:     request.ID,
		RequestSecret: requestSecret,
		ExpiresIn:     expiresIn,
	}, nil
}

func (s *TokenService) GetAuthorizationRequest(requestID string) (*models.OAuthAuthorizationRequestView, error) {
	var request models.OAuthAuthorizationRequest
	if err := s.db.First(&request, "id = ?", requestID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrAuthorizationRequestUnavailable
		}
		return nil, err
	}
	if !s.authorizationRequestPending(&request) {
		return nil, ErrAuthorizationRequestUnavailable
	}
	var client models.OAuthClient
	if err := s.db.First(&client, "client_id = ? AND is_active = ?", request.ClientID, true).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrAuthorizationRequestUnavailable
		}
		return nil, err
	}
	return &models.OAuthAuthorizationRequestView{
		RequestID:  request.ID,
		ClientID:   request.ClientID,
		ClientName: client.Name,
		Scope:      strings.Join([]string(request.Scopes), " "),
		ExpiresAt:  request.ExpiresAt,
	}, nil
}

func (s *TokenService) CancelAuthorizationRequest(requestID, requestSecret string) (string, string, bool, error) {
	var clientID, scope string
	cancelled := false
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var request models.OAuthAuthorizationRequest
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND request_secret_hash = ?", requestID, hashToken(requestSecret)).First(&request).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrAuthorizationRequestUnavailable
			}
			return err
		}
		clientID = request.ClientID
		scope = strings.Join([]string(request.Scopes), " ")
		if request.Status != models.OAuthAuthorizationRequestPending {
			return nil
		}
		now := s.now()
		if err := tx.Model(&request).Updates(map[string]any{
			"status":       models.OAuthAuthorizationRequestCancelled,
			"completed_at": now,
		}).Error; err != nil {
			return err
		}
		cancelled = true
		return nil
	})
	return clientID, scope, cancelled, err
}

func (s *TokenService) DecideAuthorization(userID uint, req *models.OAuthAuthorizationDecisionRequest) (*AuthorizationDecisionResult, error) {
	if req.Decision != models.OAuthAuthorizationDecisionApproved && req.Decision != models.OAuthAuthorizationDecisionRejected {
		return nil, ErrInvalidAuthorizationDecision
	}
	user, err := s.authContextService.ValidateIdentity(userID, s.userTenantID(userID))
	if err != nil {
		return nil, err
	}
	var result *AuthorizationDecisionResult
	err = s.db.Transaction(func(tx *gorm.DB) error {
		var request models.OAuthAuthorizationRequest
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&request, "id = ?", req.RequestID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrAuthorizationRequestUnavailable
			}
			return err
		}
		if !s.authorizationRequestPending(&request) {
			return ErrAuthorizationRequestUnavailable
		}
		client, scopes, err := validateClientRequestDB(tx, request.ClientID, request.RedirectURI, strings.Join([]string(request.Scopes), " "), false)
		if err != nil || request.CodeChallengeMethod != "S256" || !validPKCEChallenge(request.CodeChallenge) {
			return ErrAuthorizationRequestUnavailable
		}
		now := s.now()
		status := models.OAuthAuthorizationRequestRejected
		resultName, resultValue := "error", "access_denied"
		if req.Decision == models.OAuthAuthorizationDecisionApproved {
			plainCode, err := randomToken("addp_ac_")
			if err != nil {
				return err
			}
			code := models.OAuthAuthorizationCode{
				ID:                  uuid.NewString(),
				CodeHash:            hashToken(plainCode),
				ClientID:            client.ClientID,
				UserID:              user.ID,
				TenantID:            user.TenantID,
				RedirectURI:         request.RedirectURI,
				Scopes:              scopes,
				CodeChallenge:       request.CodeChallenge,
				CodeChallengeMethod: "S256",
				ExpiresAt:           now.Add(time.Duration(s.cfg.AuthorizationCodeMinutes) * time.Minute),
			}
			if err := tx.Create(&code).Error; err != nil {
				return err
			}
			status, resultName, resultValue = models.OAuthAuthorizationRequestApproved, "code", plainCode
		}
		if err := tx.Model(&request).Updates(map[string]any{"status": status, "completed_at": now}).Error; err != nil {
			return err
		}
		redirectURL, err := authorizationRedirect(request.RedirectURI, request.ID, resultName, resultValue)
		if err != nil {
			return err
		}
		result = &AuthorizationDecisionResult{
			RedirectURL: redirectURL,
			ClientID:    request.ClientID,
			Scope:       strings.Join(scopes, " "),
		}
		return nil
	})
	return result, err
}

func (s *TokenService) authorizationRequestPending(request *models.OAuthAuthorizationRequest) bool {
	return request.Status == models.OAuthAuthorizationRequestPending && request.ExpiresAt.After(s.now())
}

func authorizationRedirect(rawRedirectURI, state, resultName, resultValue string) (string, error) {
	redirect, err := url.Parse(rawRedirectURI)
	if err != nil {
		return "", ErrInvalidRedirectURI
	}
	query := redirect.Query()
	query.Set(resultName, resultValue)
	query.Set("state", state)
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
		if len(verifier) < 43 || len(verifier) > 128 || code.CodeChallengeMethod != "S256" || pkceChallenge(verifier) != code.CodeChallenge {
			return ErrInvalidPKCE
		}
		if err := ensureClientActive(tx, clientID); err != nil {
			return err
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
		if err := ensureClientActive(tx, clientID); err != nil {
			return err
		}
		switch device.Status {
		case models.OAuthDeviceStatusDenied:
			flowErr = ErrAccessDenied
			return nil
		case models.OAuthDeviceStatusUsed:
			flowErr = ErrExpiredToken
			return nil
		case models.OAuthDeviceStatusPending, models.OAuthDeviceStatusApproved:
		default:
			flowErr = ErrExpiredToken
			return nil
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
	rotatedResourceTickets := false
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
		rotatedResourceTickets = family.AuthType == models.AuthTypeFirstPartyAccessToken
		if expectedClientID != nil {
			if err := ensureClientActive(tx, *expectedClientID); err != nil {
				return err
			}
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
	if err == nil && (rotatedResourceTickets || reuseDetected) {
		s.invalidateFamilyAccessTokens(familyID)
	}
	if reuseDetected && err == nil {
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
	resourceAccessTickets := map[string]string{}
	resourceAccessTicketExpiresIn := 0
	if family.AuthType == models.AuthTypeFirstPartyAccessToken {
		if err := tx.Model(&models.ResourceAccessTicket{}).
			Where("family_id = ? AND revoked_at IS NULL", family.ID).
			Update("revoked_at", now).Error; err != nil {
			return nil, "", err
		}
		ticketExpiry := now.Add(time.Duration(s.resourceAccessTicketExpireMinutes()) * time.Minute)
		if access.ExpiresAt.Before(ticketExpiry) {
			ticketExpiry = access.ExpiresAt
		}
		if family.ExpiresAt.Before(ticketExpiry) {
			ticketExpiry = family.ExpiresAt
		}
		resourceAccessTicketExpiresIn = int((ticketExpiry.Sub(now) + time.Second - 1) / time.Second)
		for _, owner := range models.BrowserResourceAccessOwners {
			plainTicket, err := randomToken("addp_rat_")
			if err != nil {
				return nil, "", err
			}
			ticket := models.ResourceAccessTicket{
				ID:        uuid.NewString(),
				TokenHash: hashToken(plainTicket),
				FamilyID:  family.ID,
				Owner:     owner,
				ExpiresAt: ticketExpiry,
			}
			if err := tx.Create(&ticket).Error; err != nil {
				return nil, "", err
			}
			resourceAccessTickets[owner] = plainTicket
		}
	}
	return &IssuedTokenPair{
		AccessToken:                   accessPlain,
		RefreshToken:                  refreshPlain,
		AccessExpiresIn:               s.cfg.AccessTokenExpireMinutes * 60,
		Scope:                         strings.Join([]string(family.Scopes), " "),
		ResourceAccessTickets:         resourceAccessTickets,
		ResourceAccessTicketExpiresIn: resourceAccessTicketExpiresIn,
	}, refreshID, nil
}

func (s *TokenService) resourceAccessTicketExpireMinutes() int {
	if s.cfg.ResourceAccessTicketExpireMinutes > 0 {
		return s.cfg.ResourceAccessTicketExpireMinutes
	}
	return s.cfg.AccessTokenExpireMinutes
}

func (s *TokenService) delegatedAccessTokenExpireMinutes() int {
	if s.cfg.DelegatedAccessTokenExpireMinutes > 0 {
		return s.cfg.DelegatedAccessTokenExpireMinutes
	}
	return 2
}

func (s *TokenService) validateClientRequest(clientID, redirectURI, scope string, device bool) (*models.OAuthClient, []string, error) {
	return validateClientRequestDB(s.db, clientID, redirectURI, scope, device)
}

func validateClientRequestDB(db *gorm.DB, clientID, redirectURI, scope string, device bool) (*models.OAuthClient, []string, error) {
	var client models.OAuthClient
	if err := db.First(&client, "client_id = ? AND is_active = ?", clientID, true).Error; err != nil {
		return nil, nil, ErrInvalidOAuthClient
	}
	if device && !client.DeviceFlowEnabled {
		return nil, nil, ErrInvalidOAuthClient
	}
	if !device && !redirectURIAllowed(client.RedirectURIs, redirectURI) {
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

func validPKCEChallenge(challenge string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(challenge)
	return err == nil && len(decoded) == sha256.Size
}

func redirectURIAllowed(registeredURIs []string, requestedURI string) bool {
	requested, err := parseOAuthRedirectURI(requestedURI)
	if err != nil {
		return false
	}
	for _, registeredURI := range registeredURIs {
		registered, err := parseOAuthRedirectURI(registeredURI)
		if err != nil {
			continue
		}
		if registeredURI == requestedURI {
			return true
		}
		if rfc8252LoopbackRedirectMatch(registered, requested) {
			return true
		}
	}
	return false
}

func parseOAuthRedirectURI(rawURI string) (*url.URL, error) {
	parsed, err := url.ParseRequestURI(rawURI)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return nil, ErrInvalidRedirectURI
	}
	return parsed, nil
}

func rfc8252LoopbackRedirectMatch(registered, requested *url.URL) bool {
	registeredIP := net.ParseIP(registered.Hostname())
	requestedIP := net.ParseIP(requested.Hostname())
	if registered.Scheme != "http" || requested.Scheme != "http" ||
		registeredIP == nil || !registeredIP.IsLoopback() ||
		requestedIP == nil || !requestedIP.IsLoopback() ||
		registered.Hostname() != requested.Hostname() || registered.Port() != "" ||
		registered.EscapedPath() != requested.EscapedPath() ||
		registered.RawQuery != requested.RawQuery || registered.ForceQuery != requested.ForceQuery {
		return false
	}
	port, err := strconv.Atoi(requested.Port())
	return err == nil && port >= 1 && port <= 65535
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
	var accessTokens []models.AccessToken
	if err := s.db.Select("id", "token_hash").Where("family_id = ?", familyID).Find(&accessTokens).Error; err != nil {
		return
	}
	hashes := make([]string, 0, len(accessTokens))
	accessTokenIDs := make([]string, 0, len(accessTokens))
	for _, token := range accessTokens {
		hashes = append(hashes, token.TokenHash)
		accessTokenIDs = append(accessTokenIDs, token.ID)
	}
	if len(accessTokenIDs) > 0 {
		var delegatedHashes []string
		if err := s.db.Model(&models.DelegatedAccessToken{}).
			Where("source_access_token_id IN ?", accessTokenIDs).
			Pluck("token_hash", &delegatedHashes).Error; err != nil {
			return
		}
		hashes = append(hashes, delegatedHashes...)
	}
	var resourceHashes []string
	if err := s.db.Model(&models.ResourceAccessTicket{}).Where("family_id = ?", familyID).Pluck("token_hash", &resourceHashes).Error; err != nil {
		return
	}
	hashes = append(hashes, resourceHashes...)
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
	if err := tx.Model(&models.AccessToken{}).Where("family_id = ? AND revoked_at IS NULL", familyID).Update("revoked_at", now).Error; err != nil {
		return err
	}
	if err := tx.Model(&models.DelegatedAccessToken{}).
		Where("source_access_token_id IN (?) AND revoked_at IS NULL",
			tx.Model(&models.AccessToken{}).Select("id").Where("family_id = ?", familyID),
		).
		Update("revoked_at", now).Error; err != nil {
		return err
	}
	return tx.Model(&models.ResourceAccessTicket{}).Where("family_id = ? AND revoked_at IS NULL", familyID).Update("revoked_at", now).Error
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

func normalizeScopeValues(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func sameUint(left, right *uint) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
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

func ensureClientActive(tx *gorm.DB, clientID string) error {
	var count int64
	if err := tx.Model(&models.OAuthClient{}).Where("client_id = ? AND is_active = ?", clientID, true).Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return ErrInvalidOAuthClient
	}
	return nil
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
	case errors.Is(err, ErrAuthorizationRequestUnavailable):
		return "authorization_request_expired"
	case errors.Is(err, ErrInvalidOAuthClient):
		return "invalid_client"
	case errors.Is(err, ErrUnsupportedGrantType):
		return "unsupported_grant_type"
	case errors.Is(err, ErrInvalidScope):
		return "invalid_scope"
	case errors.Is(err, ErrInvalidRedirectURI), errors.Is(err, ErrInvalidAuthorizationDecision):
		return "invalid_request"
	case errors.Is(err, ErrInvalidAuthorizationCode), errors.Is(err, ErrInvalidRefreshToken), errors.Is(err, ErrInvalidPKCE), errors.Is(err, ErrRefreshTokenReuse):
		return "invalid_grant"
	default:
		return "server_error"
	}
}
