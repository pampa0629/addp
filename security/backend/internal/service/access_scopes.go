package service

import (
	"slices"
	"strings"

	"github.com/addp/security/internal/models"
)

func validAccessScope(owner, action string) bool {
	return owner == managerProtectionOwner && action == managerPreviewAction || owner == developProtectionOwner && action == developQueryAction
}

func canonicalAccessScopes(scopes []models.ProtectionAccessScope) ([]models.ProtectionAccessScope, bool) {
	if len(scopes) < 1 || len(scopes) > 2 {
		return nil, false
	}
	result := append([]models.ProtectionAccessScope(nil), scopes...)
	for i := range result {
		result[i].ConsumerOwner = strings.TrimSpace(result[i].ConsumerOwner)
		result[i].Action = strings.TrimSpace(result[i].Action)
		if !validAccessScope(result[i].ConsumerOwner, result[i].Action) {
			return nil, false
		}
		for j := 0; j < i; j++ {
			if result[i] == result[j] {
				return nil, false
			}
		}
	}
	slices.SortFunc(result, func(a, b models.ProtectionAccessScope) int { return strings.Compare(a.ConsumerOwner, b.ConsumerOwner) })
	return result, true
}
