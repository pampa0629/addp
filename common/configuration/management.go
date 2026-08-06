package configuration

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	commonauth "github.com/addp/common/authorization"
)

const ManagementSchemaVersion = "addp.configuration-management/v1"

const (
	ScopePlatformOnly                      = "platform_only"
	ScopePlatformDefaultWithTenantOverride = "platform_default_with_tenant_override"
	ScopeTenantOnly                        = "tenant_only"
)

var entryIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)

// ManagementDeclaration is the module-registry capability declaration. It
// deliberately contains navigation and authorization metadata only.
type ManagementDeclaration struct {
	SchemaVersion string            `json:"schema_version"`
	Entries       []ManagementEntry `json:"entries"`
}

type ManagementEntry struct {
	ID               string   `json:"id"`
	OwnerModule      string   `json:"owner_module"`
	ScopeTypes       []string `json:"scope_types"`
	FrontendRoute    string   `json:"frontend_route"`
	ReadPermission   string   `json:"read_permission"`
	UpdatePermission string   `json:"update_permission"`
}

func ValidateManagementDeclaration(owner string, declaration *ManagementDeclaration) error {
	if declaration == nil {
		return nil
	}
	if err := commonauth.ValidateOwnerModuleName(owner); err != nil {
		return err
	}
	if declaration.SchemaVersion != ManagementSchemaVersion {
		return fmt.Errorf("configuration management schema_version must be %q", ManagementSchemaVersion)
	}
	if len(declaration.Entries) == 0 {
		return fmt.Errorf("configuration management entries must not be empty")
	}
	seen := make(map[string]struct{}, len(declaration.Entries))
	for index := range declaration.Entries {
		entry := &declaration.Entries[index]
		if !entryIDPattern.MatchString(entry.ID) {
			return fmt.Errorf("configuration management entry id %q is invalid", entry.ID)
		}
		if _, exists := seen[entry.ID]; exists {
			return fmt.Errorf("configuration management entry id %q is duplicated", entry.ID)
		}
		seen[entry.ID] = struct{}{}
		if entry.OwnerModule != owner {
			return fmt.Errorf("configuration management entry %q owner_module must be %q", entry.ID, owner)
		}
		if err := validateScopeTypes(entry.ScopeTypes); err != nil {
			return fmt.Errorf("configuration management entry %q: %w", entry.ID, err)
		}
		ownerRoute := "/" + owner + "/"
		consoleRoute := "/configuration/" + owner
		isConsoleRoute := entry.FrontendRoute == consoleRoute || strings.HasPrefix(entry.FrontendRoute, consoleRoute+"/")
		if (!strings.HasPrefix(entry.FrontendRoute, ownerRoute) && !isConsoleRoute) || strings.Contains(entry.FrontendRoute, "?") || strings.Contains(entry.FrontendRoute, "#") {
			return fmt.Errorf("configuration management entry %q frontend_route must be an absolute %s owner route or %s Console route", entry.ID, ownerRoute, consoleRoute)
		}
		if err := commonauth.ValidatePermissionKey(entry.ReadPermission); err != nil {
			return fmt.Errorf("configuration management entry %q read_permission: %w", entry.ID, err)
		}
		if err := commonauth.ValidatePermissionKey(entry.UpdatePermission); err != nil {
			return fmt.Errorf("configuration management entry %q update_permission: %w", entry.ID, err)
		}
		if !strings.HasPrefix(entry.ReadPermission, owner+".") || !strings.HasPrefix(entry.UpdatePermission, owner+".") {
			return fmt.Errorf("configuration management entry %q permissions must be owned by %q", entry.ID, owner)
		}
	}
	sort.Slice(declaration.Entries, func(i, j int) bool { return declaration.Entries[i].ID < declaration.Entries[j].ID })
	return nil
}

func validateScopeTypes(scopeTypes []string) error {
	if len(scopeTypes) == 0 {
		return fmt.Errorf("scope_types must not be empty")
	}
	previous := ""
	for _, scopeType := range scopeTypes {
		switch scopeType {
		case ScopePlatformOnly, ScopePlatformDefaultWithTenantOverride, ScopeTenantOnly:
		default:
			return fmt.Errorf("scope_type %q is invalid", scopeType)
		}
		if previous != "" && scopeType <= previous {
			return fmt.Errorf("scope_types must be unique and sorted")
		}
		previous = scopeType
	}
	return nil
}

func EntryVisibleInContext(entry ManagementEntry, contextType string) bool {
	for _, scopeType := range entry.ScopeTypes {
		switch contextType {
		case "platform":
			if scopeType == ScopePlatformOnly || scopeType == ScopePlatformDefaultWithTenantOverride {
				return true
			}
		case "tenant":
			if scopeType == ScopeTenantOnly || scopeType == ScopePlatformDefaultWithTenantOverride {
				return true
			}
		}
	}
	return false
}
