package service

import (
	"context"
	"database/sql"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

// One bounded owner query reads one committed snapshot and proves every item.
// Protocol, database and cancellation failures never return a partial batch.
func (s *MetadataQueryService) QueryDataItemAuthorizationFacts(ctx context.Context, tenant uint, query authorizationfacts.AuthorizationFactsQuery) (*authorizationfacts.AuthorizationFactsBatch, error) {
	if tenant == 0 || query.Validate() != nil {
		return nil, commonapi.ErrBadRequest
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := &authorizationfacts.AuthorizationFactsBatch{Data: make([]authorizationfacts.AuthorizationFactsResult, 0, len(query.Fingerprints))}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var items []models.MetaItem
		if err := tx.Select("id", "tenant_id", "engine_id", "node_id", "item_type", "name", "full_name", "fingerprint", "attributes").Where("tenant_id=? AND fingerprint IN ? AND deleted_at IS NULL", tenant, query.Fingerprints).Find(&items).Error; err != nil {
			return err
		}
		lookup, err := loadMetadataIdentityNodes(tx, tenant, items)
		if err != nil {
			return err
		}
		byFingerprint := map[string]models.MetaItem{}
		for _, item := range items {
			byFingerprint[item.Fingerprint] = item
		}
		observedAt := tx.NowFunc().UTC()
		for _, fp := range query.Fingerprints {
			if err := ctx.Err(); err != nil {
				return err
			}
			entry := authorizationfacts.AuthorizationFactsResult{ItemFingerprint: fp}
			if item, ok := byFingerprint[fp]; ok {
				entry.Facts, err = projectDataItemAuthorizationFacts(item, observedAt, lookup)
				if err != nil {
					return err
				}
			}
			result.Data = append(result.Data, entry)
		}
		return result.Validate(query)
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
