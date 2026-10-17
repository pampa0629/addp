package service

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/addp/security/internal/models"
	"gorm.io/gorm"
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

func filterAccessScope(query *gorm.DB, owner, action string) *gorm.DB {
	if query.Dialector.Name() == "sqlite" {
		return query.Where("EXISTS (SELECT 1 FROM json_each(scopes) AS outlet WHERE json_extract(outlet.value, '$.consumer_owner') = ? AND json_extract(outlet.value, '$.action') = ?)", owner, action)
	}
	encoded, _ := json.Marshal([]models.ProtectionAccessScope{{ConsumerOwner: owner, Action: action}})
	return query.Where("scopes::jsonb @> ?::jsonb", string(encoded))
}
