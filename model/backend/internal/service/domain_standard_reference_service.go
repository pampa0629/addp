package service

import (
	"context"
	"github.com/addp/common/domainreference"
)

func (s *EntityService) ListDomainStandardReferences(ctx context.Context, tenantID, domainID int64, kind string, page, size int) (*domainreference.Page, error) {
	return s.repo.ListDomainStandardReferences(ctx, tenantID, domainID, kind, page, size)
}
