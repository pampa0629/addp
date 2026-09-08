package service

import (
	"regexp"
	"strings"
)

const (
	maxStandardStableCodeLength   = 100
	maxStandardCategoryCodeLength = 50
)

var standardStableCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func normalizeStandardStableCode(value string, maxLength int) (string, error) {
	code := strings.TrimSpace(value)
	if !validStandardStableCode(code, maxLength) {
		return "", ErrInvalidStandardCode
	}
	return code, nil
}

func validStandardStableCode(code string, maxLength int) bool {
	return code != "" && len(code) <= maxLength && standardStableCodePattern.MatchString(code)
}
