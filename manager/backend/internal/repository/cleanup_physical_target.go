package repository

import (
	"context"
	"encoding/json"
	"strings"

	commonapi "github.com/addp/common/api"
	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
)

// A short PG mutex serializes physical-target registration. It is never held
// across a provider request and never remains occupied by an operation. This
// closes the exact-object versus directory-prefix registration race without
// serializing unrelated uploads during their external work.
func lockPhysicalTargetRegistry(ctx context.Context, tx *gorm.DB, tokens []CleanupTargetToken) error {
	tenants := map[uint]bool{}
	for _, token := range tokens {
		if token.Key.Kind == CleanupTargetObject {
			tenants[token.Key.TenantID] = true
		}
	}
	for tenant := range tenants {
		registry := CleanupTargetToken{Key: CleanupTargetKey{TenantID: tenant, Kind: CleanupTargetGeneration, Identity: "physical-target-registry"}}
		if _, err := lockCleanupTarget(ctx, tx, registry); err != nil {
			return err
		}
		var held []models.CleanupTarget
		if err := tx.Session(&gorm.Session{NewDB: true, Context: ctx}).Where("tenant_id = ? AND kind = ? AND operation_id <> ''", tenant, CleanupTargetObject).Find(&held).Error; err != nil {
			return err
		}
		for _, token := range tokens {
			if token.Key.Kind != CleanupTargetObject || token.Key.TenantID != tenant {
				continue
			}
			var next []string
			if json.Unmarshal([]byte(token.Key.Identity), &next) != nil || len(next) != 2 {
				return commonapi.ErrConflict
			}
			for _, prior := range held {
				var old []string
				if json.Unmarshal([]byte(prior.Identity), &old) != nil || len(old) != 2 {
					return commonapi.ErrConflict
				}
				if next[0] == old[0] && (next[1] == old[1] || strings.HasPrefix(next[1], strings.TrimSuffix(old[1], "/")+"/") || strings.HasPrefix(old[1], strings.TrimSuffix(next[1], "/")+"/")) {
					return commonapi.ErrConflict
				}
			}
		}
	}
	return nil
}
