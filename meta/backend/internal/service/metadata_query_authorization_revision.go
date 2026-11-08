package service

import (
	"context"
	"errors"

	"github.com/addp/common/dataitem/authorizationfacts"
)

func (s *MetadataQueryService) GetAuthorizationBasisRevision(ctx context.Context, tenantID uint) (*authorizationfacts.Revision, error) {
	if tenantID == 0 {
		return nil, errors.New("authorization basis tenant required")
	}
	var result authorizationfacts.Revision
	err := s.db.WithContext(ctx).Raw(`SELECT ? AS schema_version, CAST(g.generation AS text) AS generation,
 CAST(COALESCE(r.version,0) AS text) AS version FROM meta.authorization_basis_generation g
 LEFT JOIN meta.authorization_basis_revisions r ON r.tenant_id=? WHERE g.singleton`, authorizationfacts.RevisionSchemaVersion, tenantID).Scan(&result).Error
	if err != nil {
		return nil, err
	}
	if err := result.Validate(); err != nil {
		return nil, err
	}
	return &result, nil
}
