package iam

import (
	"fmt"
	"strings"

	commonapi "github.com/addp/common/api"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

var usernameCaseFolder = cases.Fold()

func NormalizeUsername(username string) (string, error) {
	normalized := strings.TrimSpace(username)
	normalized = norm.NFKC.String(normalized)
	normalized = usernameCaseFolder.String(normalized)
	normalized = strings.TrimSpace(normalized)
	if normalized == "" {
		return "", fmt.Errorf("%w: username must not be empty", commonapi.ErrBadRequest)
	}
	return normalized, nil
}

func NormalizeTenantCode(code string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(code))
	if normalized == "" {
		return "", fmt.Errorf("%w: tenant code must not be empty", commonapi.ErrBadRequest)
	}
	return normalized, nil
}
