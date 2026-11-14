package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/addp/system/internal/config"
	"github.com/addp/system/internal/iam"
	"github.com/pquerna/otp/totp"
	"gorm.io/gorm"
	"net/http"
	"time"
)

// Only the empty, owned Hosted database reaches this fixture. Obtain the
// observer session through password and real TOTP on the public API.
func prepareModuleRegistryPlatform(ctx context.Context, db *gorm.DB, cfg *config.Config) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	repo := iam.NewRepository(db)
	identity := iam.NewIdentityService(repo, nil)
	cipher, err := iam.NewMFACredentialCipher(cfg.IAMMFAEncryptionKey)
	if err != nil {
		return "", err
	}
	bootstrap, err := iam.NewBootstrapService(repo, identity, cipher, time.Hour, nil, nil)
	if err != nil {
		return "", err
	}
	secret, _, err := bootstrap.Prepare(ctx)
	if err != nil {
		return "", err
	}
	inputs, err := platformBootstrapInputs(time.Now(), "online-registry", "Online registry")
	if err != nil {
		return "", err
	}
	if _, err := bootstrap.Apply(ctx, iam.BootstrapApplyInput{BootstrapSecret: secret, Administrators: inputs}); err != nil {
		return "", err
	}
	type loginResult struct {
		NextAction string `json:"next_action"`
		MFA        struct {
			ChallengeToken string `json:"challenge_token"`
		} `json:"mfa"`
		Session struct {
			AccessToken string `json:"access_token"`
		} `json:"session"`
	}
	client := &http.Client{Timeout: 10 * time.Second}
	post := func(path string, input any) (*loginResult, error) {
		raw, err := json.Marshal(input)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.SystemServiceURL+path, bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("registry fixture authentication transport failed")
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("registry fixture authentication returned HTTP %d", resp.StatusCode)
		}
		var result loginResult
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("registry fixture authentication response invalid")
		}
		return &result, nil
	}
	login, err := post("/api/v1/system/login", map[string]string{"username": inputs[0].Username, "password": inputs[0].Password})
	if err != nil {
		return "", err
	}
	if login.NextAction != "verify_mfa" || login.MFA.ChallengeToken == "" {
		return "", fmt.Errorf("registry fixture requires MFA challenge")
	}
	code, err := totp.GenerateCode(inputs[0].TOTPSecret, time.Now())
	if err != nil {
		return "", err
	}
	result, err := post("/api/v1/system/auth/mfa-verifications", map[string]string{"challenge_token": login.MFA.ChallengeToken, "code": code})
	if err != nil {
		return "", err
	}
	if result.NextAction != "session_issued" || result.Session.AccessToken == "" {
		return "", fmt.Errorf("registry fixture Platform session missing")
	}
	return result.Session.AccessToken, nil
}
