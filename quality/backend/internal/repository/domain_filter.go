package repository

import "gorm.io/gorm"

// A missing filter includes all owners; zero selects tenant-public objects.
func filterOwnerDomain(query *gorm.DB, ownerDomainID *int64) *gorm.DB {
	if ownerDomainID == nil {
		return query
	}
	if *ownerDomainID == 0 {
		return query.Where("owner_domain_id IS NULL")
	}
	return query.Where("owner_domain_id = ?", *ownerDomainID)
}
