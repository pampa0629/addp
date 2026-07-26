package oauth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/ory/fosite"
	"github.com/ory/fosite/handler/oauth2"
	"github.com/ory/fosite/handler/rfc8628"
)

const (
	authorizeCodePrefix = "addp_ac_"
	accessTokenPrefix   = "addp_at_"
	refreshTokenPrefix  = "addp_rt_"
	deviceCodePrefix    = "addp_dc_"
	randomTokenBytes    = 32
	userCodeLength      = 8
	userCodeSymbols     = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
)

type DevicePollLimiter interface {
	ShouldRateLimit(ctx context.Context, deviceCodeSignature string) (bool, error)
}

type StrategyConfig struct {
	AccessTokenLifespan    time.Duration
	RefreshTokenLifespan   time.Duration
	AuthorizeCodeLifespan  time.Duration
	DeviceCodeLifespan     time.Duration
	UserCodePepper         []byte
	PreviousUserCodePepper []byte
}

type Strategy struct {
	config      StrategyConfig
	pollLimiter DevicePollLimiter
	now         func() time.Time
}

func NewStrategy(config StrategyConfig, pollLimiter DevicePollLimiter) (*Strategy, error) {
	if config.AccessTokenLifespan <= 0 || config.RefreshTokenLifespan <= 0 ||
		config.AuthorizeCodeLifespan <= 0 || config.DeviceCodeLifespan <= 0 {
		return nil, errors.New("OAuth strategy 生命周期必须大于零")
	}
	if len(config.UserCodePepper) < 32 {
		return nil, errors.New("OAuth User Code pepper 至少需要 32 字节")
	}
	if len(config.PreviousUserCodePepper) > 0 && len(config.PreviousUserCodePepper) < 32 {
		return nil, errors.New("OAuth 前一 User Code pepper 至少需要 32 字节")
	}
	if pollLimiter == nil {
		return nil, errors.New("OAuth Device Poll Limiter 不能为空")
	}
	return &Strategy{config: config, pollLimiter: pollLimiter, now: func() time.Time {
		return time.Now().UTC()
	}}, nil
}

func (*Strategy) AccessTokenSignature(_ context.Context, token string) string {
	return opaqueSignature(token)
}

func (*Strategy) RefreshTokenSignature(_ context.Context, token string) string {
	return opaqueSignature(token)
}

func (*Strategy) AuthorizeCodeSignature(_ context.Context, token string) string {
	return opaqueSignature(token)
}

func (*Strategy) GenerateAccessToken(context.Context, fosite.Requester) (string, string, error) {
	return generateOpaqueToken(accessTokenPrefix)
}

func (*Strategy) GenerateRefreshToken(context.Context, fosite.Requester) (string, string, error) {
	return generateOpaqueToken(refreshTokenPrefix)
}

func (*Strategy) GenerateAuthorizeCode(context.Context, fosite.Requester) (string, string, error) {
	return generateOpaqueToken(authorizeCodePrefix)
}

func (s *Strategy) ValidateAccessToken(_ context.Context, requester fosite.Requester, token string) error {
	return s.validateOpaque(requester, token, accessTokenPrefix, fosite.AccessToken, s.config.AccessTokenLifespan)
}

func (s *Strategy) ValidateRefreshToken(_ context.Context, requester fosite.Requester, token string) error {
	return s.validateOpaque(requester, token, refreshTokenPrefix, fosite.RefreshToken, s.config.RefreshTokenLifespan)
}

func (s *Strategy) ValidateAuthorizeCode(_ context.Context, requester fosite.Requester, token string) error {
	return s.validateOpaque(requester, token, authorizeCodePrefix, fosite.AuthorizeCode, s.config.AuthorizeCodeLifespan)
}

func (*Strategy) GenerateDeviceCode(context.Context) (string, string, error) {
	return generateOpaqueToken(deviceCodePrefix)
}

func (*Strategy) DeviceCodeSignature(_ context.Context, code string) (string, error) {
	return opaqueSignature(code), nil
}

func (s *Strategy) ValidateDeviceCode(_ context.Context, requester fosite.DeviceRequester, code string) error {
	if err := validateOpaqueSyntax(code, deviceCodePrefix); err != nil {
		return err
	}
	if isExpired(requester, fosite.DeviceCode, s.config.DeviceCodeLifespan, s.now()) {
		return fosite.ErrDeviceExpiredToken
	}
	return nil
}

func (s *Strategy) GenerateUserCode(context.Context) (string, string, error) {
	code := make([]byte, userCodeLength)
	for index := range code {
		symbolIndex, err := randomSymbolIndex(len(userCodeSymbols))
		if err != nil {
			return "", "", err
		}
		code[index] = userCodeSymbols[symbolIndex]
	}
	displayCode := string(code[:4]) + "-" + string(code[4:])
	signature, err := s.UserCodeSignature(context.Background(), displayCode)
	if err != nil {
		return "", "", err
	}
	return displayCode, signature, nil
}

func randomSymbolIndex(symbolCount int) (int, error) {
	if symbolCount <= 0 || symbolCount > 256 {
		return 0, errors.New("OAuth User Code 字符集无效")
	}
	limit := 256 - (256 % symbolCount)
	buffer := []byte{0}
	for {
		if _, err := rand.Read(buffer); err != nil {
			return 0, errors.New("生成 OAuth User Code 失败")
		}
		if int(buffer[0]) < limit {
			return int(buffer[0]) % symbolCount, nil
		}
	}
}

func (s *Strategy) UserCodeSignature(_ context.Context, code string) (string, error) {
	normalized, err := normalizeUserCode(code)
	if err != nil {
		return "", err
	}
	return userCodeSignature(s.config.UserCodePepper, normalized), nil
}

func (s *Strategy) UserCodeSignatures(code string) ([]string, error) {
	normalized, err := normalizeUserCode(code)
	if err != nil {
		return nil, err
	}
	signatures := []string{userCodeSignature(s.config.UserCodePepper, normalized)}
	if len(s.config.PreviousUserCodePepper) > 0 {
		signatures = append(signatures, userCodeSignature(s.config.PreviousUserCodePepper, normalized))
	}
	return signatures, nil
}

func (s *Strategy) ValidateUserCode(_ context.Context, requester fosite.DeviceRequester, code string) error {
	if _, err := normalizeUserCode(code); err != nil {
		return err
	}
	if isExpired(requester, fosite.UserCode, s.config.DeviceCodeLifespan, s.now()) {
		return fosite.ErrDeviceExpiredToken
	}
	return nil
}

func (s *Strategy) ShouldRateLimit(ctx context.Context, code string) (bool, error) {
	if err := validateOpaqueSyntax(code, deviceCodePrefix); err != nil {
		return false, err
	}
	return s.pollLimiter.ShouldRateLimit(ctx, opaqueSignature(code))
}

func (s *Strategy) validateOpaque(
	requester fosite.Requester,
	token string,
	prefix string,
	tokenType fosite.TokenType,
	lifespan time.Duration,
) error {
	if err := validateOpaqueSyntax(token, prefix); err != nil {
		return err
	}
	if isExpired(requester, tokenType, lifespan, s.now()) {
		return fosite.ErrTokenExpired
	}
	return nil
}

func isExpired(requester fosite.Requester, tokenType fosite.TokenType, lifespan time.Duration, now time.Time) bool {
	if requester == nil || requester.GetSession() == nil {
		return true
	}
	expiresAt := requester.GetSession().GetExpiresAt(tokenType)
	if expiresAt.IsZero() {
		expiresAt = requester.GetRequestedAt().Add(lifespan)
	}
	return !expiresAt.After(now)
}

func generateOpaqueToken(prefix string) (string, string, error) {
	randomBytes := make([]byte, randomTokenBytes)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", "", errors.New("生成 OAuth 凭据失败")
	}
	token := prefix + base64.RawURLEncoding.EncodeToString(randomBytes)
	return token, opaqueSignature(token), nil
}

func opaqueSignature(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func validateOpaqueSyntax(token, prefix string) error {
	if !strings.HasPrefix(token, prefix) {
		return fosite.ErrTokenSignatureMismatch
	}
	encoded := strings.TrimPrefix(token, prefix)
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(decoded) != randomTokenBytes {
		return fosite.ErrTokenSignatureMismatch
	}
	return nil
}

func normalizeUserCode(code string) (string, error) {
	normalized := strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(code)))
	if len(normalized) != userCodeLength {
		return "", fosite.ErrTokenSignatureMismatch
	}
	for _, character := range normalized {
		if !strings.ContainsRune(userCodeSymbols, character) {
			return "", fosite.ErrTokenSignatureMismatch
		}
	}
	return normalized, nil
}

func userCodeSignature(pepper []byte, normalizedCode string) string {
	mac := hmac.New(sha256.New, pepper)
	_, _ = mac.Write([]byte(normalizedCode))
	return hex.EncodeToString(mac.Sum(nil))
}

var _ oauth2.CoreStrategy = (*Strategy)(nil)
var _ rfc8628.RFC8628CodeStrategy = (*Strategy)(nil)
