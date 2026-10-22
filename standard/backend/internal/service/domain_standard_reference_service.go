package service

import (
	"context"
	"github.com/addp/common/domainreference"
)

// ListDomainStandardReferences reads the local owner's explicit evidence only.
func (s *CatalogResourceService) ListDomainStandardReferences(ctx context.Context, tenantID, domainID int64, kind string, page, size int) (*domainreference.Page, error) {
	return s.repository.ListDomainStandardReferences(ctx, tenantID, domainID, kind, page, size)
}
