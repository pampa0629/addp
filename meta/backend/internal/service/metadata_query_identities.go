package service

import (
	"context"
	"database/sql"
	"strings"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

// QueryDataItemIdentities reads only committed identity columns. Count and page
// share one owner snapshot; no source engine, scan, or authorization decision.
func (s *MetadataQueryService) QueryDataItemIdentities(ctx context.Context, tenantID uint, input authorizationfacts.IdentityQuery) (*authorizationfacts.IdentityPage, error) {
	if tenantID == 0 || input.Validate() != nil {
		return nil, commonapi.ErrBadRequest
	}
	result := &authorizationfacts.IdentityPage{Data: []authorizationfacts.DataItemIdentity{}, Page: input.Page, PageSize: input.PageSize}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Model(&models.MetaItem{}).Where("tenant_id = ? AND engine_id = ? AND deleted_at IS NULL", tenantID, input.EngineID)
		if input.Search != "" {
			// Literal name/path search: wildcard characters are not user SQL.
			pattern := "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(strings.ToLower(input.Search)) + "%"
			query = query.Where("(LOWER(name) LIKE ? ESCAPE '!' OR LOWER(full_name) LIKE ? ESCAPE '!')", pattern, pattern)
		}
		switch input.FingerprintFilter {
		case "include":
			if len(input.Fingerprints) == 0 {
				query = query.Where("1 = 0")
			} else {
				query = query.Where("fingerprint IN ?", input.Fingerprints)
			}
		case "exclude":
			if len(input.Fingerprints) > 0 {
				query = query.Where("fingerprint NOT IN ?", input.Fingerprints)
			}
		}
		if err := query.Count(&result.Total).Error; err != nil {
			return err
		}
		if err := query.Select("fingerprint AS item_fingerprint, engine_id, name, full_name, item_type").Order("fingerprint ASC").
			Offset((input.Page - 1) * input.PageSize).Limit(input.PageSize).Scan(&result.Data).Error; err != nil {
			return err
		}
		result.TotalPages = result.Total / int64(input.PageSize)
		if result.Total%int64(input.PageSize) != 0 {
			result.TotalPages++
		}
		return result.Validate(input)
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, err
}
