package service

import (
	"context"
	"errors"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/dataitem/authorizationfacts"
	"gorm.io/gorm"
)

// One authenticated owner request retains the exact existing projection and
// per-item committed snapshot. A failure never returns a partial batch.
func (s *MetadataQueryService) QueryDataItemAuthorizationFacts(ctx context.Context, tenant uint, query authorizationfacts.AuthorizationFactsQuery) (*authorizationfacts.AuthorizationFactsBatch, error) {
	if tenant == 0 || query.Validate() != nil {
		return nil, commonapi.ErrBadRequest
	}
	result := &authorizationfacts.AuthorizationFactsBatch{Data: make([]authorizationfacts.AuthorizationFactsResult, 0, len(query.Fingerprints))}
	for _, fp := range query.Fingerprints {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		facts, err := s.GetDataItemAuthorizationFacts(ctx, tenant, fp)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		result.Data = append(result.Data, authorizationfacts.AuthorizationFactsResult{ItemFingerprint: fp, Facts: facts})
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := result.Validate(query); err != nil {
		return nil, err
	}
	return result, nil
}
