package iam

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	commonapi "github.com/addp/common/api"
)

func TestGenerateOpaqueTokenUsesPrefixAndThirtyTwoRandomBytes(t *testing.T) {
	token, err := generateOpaqueToken("addp_cst_")
	if err != nil {
		t.Fatalf("generateOpaqueToken() error = %v", err)
	}
	if !strings.HasPrefix(token, "addp_cst_") {
		t.Fatalf("token = %q, want addp_cst_ prefix", token)
	}
	randomValue, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(token, "addp_cst_"))
	if err != nil {
		t.Fatalf("decode random token value: %v", err)
	}
	if len(randomValue) != 32 {
		t.Fatalf("random token bytes = %d, want 32", len(randomValue))
	}
	if hash := hashOpaqueToken(token); len(hash) != 64 || hash == token {
		t.Fatalf("token hash = %q", hash)
	}
}

func TestNormalizeBrowserSessionConfigRejectsInvalidOwners(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		owners []string
	}{
		{name: "missing", owners: nil},
		{name: "invalid", owners: []string{"Manager"}},
		{name: "duplicate", owners: []string{"manager", "manager"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := normalizeBrowserSessionConfig(BrowserSessionConfig{ResourceTicketOwners: testCase.owners})
			if !errors.Is(err, commonapi.ErrBadRequest) {
				t.Fatalf("normalizeBrowserSessionConfig() error = %v, want bad request", err)
			}
		})
	}
}

func TestNormalizeAuthenticationMethodsRejectsUnsupportedMethod(t *testing.T) {
	_, err := normalizeAuthenticationMethods([]string{"password", "custom_method"})
	if !errors.Is(err, commonapi.ErrBadRequest) {
		t.Fatalf("normalizeAuthenticationMethods() error = %v, want bad request", err)
	}
}
