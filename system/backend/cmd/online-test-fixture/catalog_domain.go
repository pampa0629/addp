package main

// Permissions are limited to the disposable Catalog acceptance Tenant.
func catalogDomainPermissions() []string {
	permissions := []string{"catalog.entry.read", "catalog.inventory.read", "model.entity.read", "model.entity.create", "model.entity.delete", "standard.domain.create", "standard.domain.read", "standard.domain.delete"}
	for _, kind := range []string{"glossary", "element", "code_set", "document", "metric"} {
		for _, action := range []string{"create", "read", "delete"} {
			permissions = append(permissions, "standard."+kind+"."+action)
		}
	}
	return permissions
}

var catalogDomainPreparerPermissions = []string{"standard.domain.create", "standard.domain.read", "standard.element.create", "standard.element.read", "standard.element.update", "standard.element.publish"}
