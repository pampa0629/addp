package service

import (
	"fmt"
	"strings"

	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
)

func validateTenantStandardScope(refs *repository.TenantReferenceRepository, tenantID int64, scopeType string, ownerDomainID *int64) (string, error) {
	scopeType = strings.TrimSpace(scopeType)
	switch scopeType {
	case models.StandardScopeTenantCommon:
		if ownerDomainID != nil {
			return "", fmt.Errorf("%w: tenant_common scope cannot define owner_domain_id", ErrInvalidStandardScope)
		}
	case models.StandardScopeDomain:
		if ownerDomainID == nil || *ownerDomainID <= 0 {
			return "", fmt.Errorf("%w: domain scope requires owner_domain_id", ErrInvalidStandardScope)
		}
		if err := refs.RequireDomain(tenantID, ownerDomainID); err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("%w: scope_type must be tenant_common or domain", ErrInvalidStandardScope)
	}
	return scopeType, nil
}
