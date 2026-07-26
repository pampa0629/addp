package oauth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ory/fosite"
)

type recordingPollLimiter struct {
	signature string
	limited   bool
	err       error
}

func (l *recordingPollLimiter) ShouldRateLimit(_ context.Context, signature string) (bool, error) {
	l.signature = signature
	return l.limited, l.err
}

func newTestStrategy(t *testing.T, limiter DevicePollLimiter) *Strategy {
	t.Helper()
	strategy, err := NewStrategy(StrategyConfig{
		AccessTokenLifespan:    15 * time.Minute,
		RefreshTokenLifespan:   30 * 24 * time.Hour,
		AuthorizeCodeLifespan:  5 * time.Minute,
		DeviceCodeLifespan:     10 * time.Minute,
		UserCodePepper:         []byte("0123456789abcdef0123456789abcdef"),
		PreviousUserCodePepper: []byte("abcdef0123456789abcdef0123456789"),
	}, limiter)
	if err != nil {
		t.Fatalf("NewStrategy() error = %v", err)
	}
	return strategy
}

func TestStrategyGeneratesADDPTokenFormatsAndSHA256Signatures(t *testing.T) {
	strategy := newTestStrategy(t, &recordingPollLimiter{})
	tests := []struct {
		name   string
		prefix string
		issue  func() (string, string, error)
	}{
		{name: "authorization code", prefix: authorizeCodePrefix, issue: func() (string, string, error) {
			return strategy.GenerateAuthorizeCode(context.Background(), nil)
		}},
		{name: "access token", prefix: accessTokenPrefix, issue: func() (string, string, error) {
			return strategy.GenerateAccessToken(context.Background(), nil)
		}},
		{name: "refresh token", prefix: refreshTokenPrefix, issue: func() (string, string, error) {
			return strategy.GenerateRefreshToken(context.Background(), nil)
		}},
		{name: "device code", prefix: deviceCodePrefix, issue: func() (string, string, error) {
			return strategy.GenerateDeviceCode(context.Background())
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			token, signature, err := test.issue()
			if err != nil {
				t.Fatalf("issue error = %v", err)
			}
			if !strings.HasPrefix(token, test.prefix) || len(strings.TrimPrefix(token, test.prefix)) != 43 {
				t.Fatalf("token = %q", token)
			}
			if signature != opaqueSignature(token) || len(signature) != 64 {
				t.Fatalf("signature = %q", signature)
			}
		})
	}
}

func TestStrategyRejectsWrongPrefixMalformedAndExpiredToken(t *testing.T) {
	strategy := newTestStrategy(t, &recordingPollLimiter{})
	now := time.Now().UTC()
	strategy.now = func() time.Time { return now }
	request := fosite.NewRequest()
	request.RequestedAt = now.Add(-time.Minute)
	session := NewIAMSession()
	session.SetExpiresAt(fosite.AccessToken, now.Add(time.Minute))
	request.Session = session
	token, _, err := strategy.GenerateAccessToken(context.Background(), request)
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}
	if err := strategy.ValidateAccessToken(context.Background(), request, token); err != nil {
		t.Fatalf("ValidateAccessToken() error = %v", err)
	}
	if err := strategy.ValidateAccessToken(context.Background(), request, strings.Replace(token, accessTokenPrefix, refreshTokenPrefix, 1)); !errors.Is(err, fosite.ErrTokenSignatureMismatch) {
		t.Fatalf("wrong prefix error = %v", err)
	}
	if err := strategy.ValidateAccessToken(context.Background(), request, accessTokenPrefix+"not-base64!"); !errors.Is(err, fosite.ErrTokenSignatureMismatch) {
		t.Fatalf("malformed token error = %v", err)
	}
	session.SetExpiresAt(fosite.AccessToken, now)
	if err := strategy.ValidateAccessToken(context.Background(), request, token); !errors.Is(err, fosite.ErrTokenExpired) {
		t.Fatalf("expired token error = %v", err)
	}
}

func TestStrategyUserCodeNormalizationPepperRotationAndFormat(t *testing.T) {
	strategy := newTestStrategy(t, &recordingPollLimiter{})
	code, signature, err := strategy.GenerateUserCode(context.Background())
	if err != nil {
		t.Fatalf("GenerateUserCode() error = %v", err)
	}
	if len(code) != 9 || code[4] != '-' {
		t.Fatalf("user code = %q", code)
	}
	for _, character := range strings.ReplaceAll(code, "-", "") {
		if !strings.ContainsRune(userCodeSymbols, character) {
			t.Fatalf("user code contains ambiguous character %q", character)
		}
	}
	lowerSpaced := strings.ToLower(code[:4] + " " + code[5:])
	signatures, err := strategy.UserCodeSignatures(lowerSpaced)
	if err != nil {
		t.Fatalf("UserCodeSignatures() error = %v", err)
	}
	if len(signatures) != 2 || signatures[0] != signature || signatures[0] == signatures[1] {
		t.Fatalf("signatures = %#v, generated = %q", signatures, signature)
	}
	if strings.Contains(strings.Join(signatures, ""), strings.ReplaceAll(code, "-", "")) {
		t.Fatal("signature leaked the user code")
	}
	if _, err := strategy.UserCodeSignature(context.Background(), "ABOI-1000"); !errors.Is(err, fosite.ErrTokenSignatureMismatch) {
		t.Fatalf("ambiguous user code error = %v", err)
	}
}

func TestStrategyDelegatesDevicePollingByHashedCode(t *testing.T) {
	limiter := &recordingPollLimiter{limited: true}
	strategy := newTestStrategy(t, limiter)
	code, _, err := strategy.GenerateDeviceCode(context.Background())
	if err != nil {
		t.Fatalf("GenerateDeviceCode() error = %v", err)
	}
	limited, err := strategy.ShouldRateLimit(context.Background(), code)
	if err != nil || !limited {
		t.Fatalf("ShouldRateLimit() = %v, %v", limited, err)
	}
	if limiter.signature != opaqueSignature(code) || strings.Contains(limiter.signature, code) {
		t.Fatalf("limiter signature = %q", limiter.signature)
	}
}

func TestStrategyRequiresPepperAndPersistentLimiter(t *testing.T) {
	config := StrategyConfig{
		AccessTokenLifespan:   time.Minute,
		RefreshTokenLifespan:  time.Hour,
		AuthorizeCodeLifespan: time.Minute,
		DeviceCodeLifespan:    time.Minute,
		UserCodePepper:        make([]byte, 32),
	}
	if _, err := NewStrategy(config, nil); err == nil {
		t.Fatal("NewStrategy() accepted a nil Device Poll Limiter")
	}
	config.UserCodePepper = []byte("short")
	if _, err := NewStrategy(config, &recordingPollLimiter{}); err == nil {
		t.Fatal("NewStrategy() accepted a short User Code pepper")
	}
}
