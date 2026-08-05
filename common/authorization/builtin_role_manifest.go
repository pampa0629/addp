package authorization

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const BuiltinRoleManifestSchemaVersion = "addp.builtin_roles/v1"

var (
	principalTypeOrder = map[string]int{
		"user": 0, "service_principal": 1,
	}
	platformUserOnlyRoles = map[string]struct{}{
		"platform.system_administrator":   {},
		"platform.security_administrator": {},
		"platform.audit_administrator":    {},
	}
)

//go:embed schemas/builtin-role-manifest-v1.schema.json
var builtinRoleSchemaFS embed.FS

type BuiltinRoleManifest struct {
	SchemaVersion   string                  `json:"schema_version" yaml:"schema_version"`
	ManifestVersion int                     `json:"manifest_version" yaml:"manifest_version"`
	Roles           []BuiltinRoleDefinition `json:"roles" yaml:"roles"`
}

type BuiltinRoleDefinition struct {
	Key                   string   `json:"key" yaml:"key"`
	RoleType              string   `json:"role_type" yaml:"role_type"`
	NameI18nKey           string   `json:"name_i18n_key" yaml:"name_i18n_key"`
	DescriptionI18nKey    string   `json:"description_i18n_key" yaml:"description_i18n_key"`
	AllowedScopeTypes     []string `json:"allowed_scope_types" yaml:"allowed_scope_types"`
	AllowedPrincipalTypes []string `json:"allowed_principal_types" yaml:"allowed_principal_types"`
	Permissions           []string `json:"permissions" yaml:"permissions"`
}

type BuiltinRoleDescriptor struct {
	Key                   string   `json:"key"`
	RoleType              string   `json:"role_type"`
	NameI18nKey           string   `json:"name_i18n_key"`
	DescriptionI18nKey    string   `json:"description_i18n_key"`
	AllowedScopeTypes     []string `json:"allowed_scope_types"`
	AllowedPrincipalTypes []string `json:"allowed_principal_types"`
	Permissions           []string `json:"permissions"`
}

func BuiltinRoleManifestSchema() []byte {
	data, err := builtinRoleSchemaFS.ReadFile("schemas/builtin-role-manifest-v1.schema.json")
	if err != nil {
		panic(fmt.Sprintf("read embedded builtin role manifest schema: %v", err))
	}
	return bytes.Clone(data)
}

func LoadBuiltinRoleManifest(path string) (BuiltinRoleManifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return BuiltinRoleManifest{}, err
	}
	defer file.Close()

	manifest, err := DecodeBuiltinRoleManifest(file)
	if err != nil {
		return BuiltinRoleManifest{}, fmt.Errorf("parse builtin role manifest %s: %w", path, err)
	}
	return manifest, nil
}

func DecodeBuiltinRoleManifest(r io.Reader) (BuiltinRoleManifest, error) {
	decoder := yaml.NewDecoder(r)
	decoder.KnownFields(true)

	var manifest BuiltinRoleManifest
	if err := decoder.Decode(&manifest); err != nil {
		return BuiltinRoleManifest{}, err
	}

	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return BuiltinRoleManifest{}, fmt.Errorf("multiple YAML documents are not allowed")
		}
		return BuiltinRoleManifest{}, err
	}

	if err := ValidateBuiltinRoleManifest(manifest); err != nil {
		return BuiltinRoleManifest{}, err
	}
	return manifest, nil
}

func ValidateBuiltinRoleManifest(manifest BuiltinRoleManifest) error {
	if manifest.SchemaVersion != BuiltinRoleManifestSchemaVersion {
		return fmt.Errorf("schema_version must be %q", BuiltinRoleManifestSchemaVersion)
	}
	if manifest.ManifestVersion < 1 {
		return fmt.Errorf("manifest_version must be a positive integer")
	}
	if len(manifest.Roles) == 0 {
		return fmt.Errorf("roles must not be empty")
	}

	previousKey := ""
	for i, role := range manifest.Roles {
		if err := validateBuiltinRole(role); err != nil {
			return fmt.Errorf("roles[%d]: %w", i, err)
		}
		if previousKey != "" && role.Key <= previousKey {
			return fmt.Errorf("roles must be strictly sorted by key: %q follows %q", role.Key, previousKey)
		}
		previousKey = role.Key
	}
	return nil
}

func ResolveBuiltinRoles(manifest BuiltinRoleManifest, permissions []PermissionDescriptor) ([]BuiltinRoleDescriptor, error) {
	if err := ValidateBuiltinRoleManifest(manifest); err != nil {
		return nil, err
	}
	if len(permissions) == 0 {
		return nil, fmt.Errorf("permission catalog must not be empty")
	}

	catalog := make(map[string]PermissionDescriptor, len(permissions))
	for _, permission := range permissions {
		if _, exists := catalog[permission.Key]; exists {
			return nil, fmt.Errorf("permission catalog contains duplicate key %q", permission.Key)
		}
		catalog[permission.Key] = permission
	}

	descriptors := make([]BuiltinRoleDescriptor, 0, len(manifest.Roles))
	for _, role := range manifest.Roles {
		for _, permissionKey := range role.Permissions {
			permission, exists := catalog[permissionKey]
			if !exists {
				return nil, fmt.Errorf("role %q references unknown permission %q", role.Key, permissionKey)
			}
			if permission.Status != "active" {
				return nil, fmt.Errorf("role %q references non-active permission %q", role.Key, permissionKey)
			}
			for _, scope := range role.AllowedScopeTypes {
				if !contains(permission.AllowedScopeTypes, scope) {
					return nil, fmt.Errorf("role %q scope %q is not allowed by permission %q", role.Key, scope, permissionKey)
				}
			}
			if isResourceGrantPermission(permissionKey) && !onlyAllowsServicePrincipal(role.AllowedPrincipalTypes) {
				return nil, fmt.Errorf("role %q must allow only service_principal when it contains internal permission %q", role.Key, permissionKey)
			}
		}

		descriptors = append(descriptors, BuiltinRoleDescriptor{
			Key:                   role.Key,
			RoleType:              role.RoleType,
			NameI18nKey:           role.NameI18nKey,
			DescriptionI18nKey:    role.DescriptionI18nKey,
			AllowedScopeTypes:     append([]string(nil), role.AllowedScopeTypes...),
			AllowedPrincipalTypes: append([]string(nil), role.AllowedPrincipalTypes...),
			Permissions:           append([]string(nil), role.Permissions...),
		})
	}

	sort.Slice(descriptors, func(i, j int) bool {
		return descriptors[i].Key < descriptors[j].Key
	})
	return descriptors, nil
}

func validateBuiltinRole(role BuiltinRoleDefinition) error {
	parts := strings.Split(role.Key, ".")
	if len(parts) != 2 || !keyPartPattern.MatchString(parts[0]) || !keyPartPattern.MatchString(parts[1]) {
		return fmt.Errorf("key %q must use {scope}.{role}", role.Key)
	}
	switch role.RoleType {
	case "platform_builtin":
		if parts[0] != "platform" {
			return fmt.Errorf("platform_builtin key %q must use platform namespace", role.Key)
		}
	case "tenant_builtin":
		if parts[0] != "tenant" {
			return fmt.Errorf("tenant_builtin key %q must use tenant namespace", role.Key)
		}
	default:
		return fmt.Errorf("key %q uses unsupported role_type %q", role.Key, role.RoleType)
	}
	if expected := "roles." + role.Key + ".name"; role.NameI18nKey != expected {
		return fmt.Errorf("key %q name_i18n_key must be %q", role.Key, expected)
	}
	if expected := "roles." + role.Key + ".description"; role.DescriptionI18nKey != expected {
		return fmt.Errorf("key %q description_i18n_key must be %q", role.Key, expected)
	}
	if err := validateRoleScopes(role); err != nil {
		return err
	}
	if err := validatePrincipalTypes(role.Key, role.AllowedPrincipalTypes); err != nil {
		return err
	}
	if _, userOnly := platformUserOnlyRoles[role.Key]; userOnly && !onlyAllowsUser(role.AllowedPrincipalTypes) {
		return fmt.Errorf("platform governance role %q must allow only user", role.Key)
	}
	if len(role.Permissions) == 0 {
		return fmt.Errorf("key %q must declare permissions", role.Key)
	}
	previousPermission := ""
	for _, permission := range role.Permissions {
		if !isPermissionKey(permission) {
			return fmt.Errorf("key %q contains invalid permission key %q", role.Key, permission)
		}
		if previousPermission != "" && permission <= previousPermission {
			return fmt.Errorf("key %q permissions must be strictly sorted", role.Key)
		}
		previousPermission = permission
	}
	return nil
}

func validateRoleScopes(role BuiltinRoleDefinition) error {
	if len(role.AllowedScopeTypes) == 0 {
		return fmt.Errorf("key %q must declare allowed_scope_types", role.Key)
	}
	previousRank := -1
	for _, scope := range role.AllowedScopeTypes {
		rank, ok := scopeOrder[scope]
		if !ok {
			return fmt.Errorf("key %q uses unsupported scope %q", role.Key, scope)
		}
		if rank <= previousRank {
			return fmt.Errorf("key %q allowed_scope_types must be unique and ordered", role.Key)
		}
		if role.RoleType == "platform_builtin" && scope != "platform" {
			return fmt.Errorf("platform role %q cannot use scope %q", role.Key, scope)
		}
		if role.RoleType == "tenant_builtin" && scope == "platform" {
			return fmt.Errorf("tenant role %q cannot use platform scope", role.Key)
		}
		previousRank = rank
	}
	return nil
}

func validatePrincipalTypes(roleKey string, principalTypes []string) error {
	if len(principalTypes) == 0 {
		return fmt.Errorf("key %q must declare allowed_principal_types", roleKey)
	}
	previousRank := -1
	for _, principalType := range principalTypes {
		rank, ok := principalTypeOrder[principalType]
		if !ok {
			return fmt.Errorf("key %q uses unsupported principal type %q", roleKey, principalType)
		}
		if rank <= previousRank {
			return fmt.Errorf("key %q allowed_principal_types must be unique and ordered", roleKey)
		}
		previousRank = rank
	}
	return nil
}

func isPermissionKey(key string) bool {
	parts := strings.Split(key, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if !keyPartPattern.MatchString(part) {
			return false
		}
	}
	return true
}

func isResourceGrantPermission(key string) bool {
	parts := strings.Split(key, ".")
	return len(parts) == 3 && parts[1] == "resource_grant"
}

func onlyAllowsUser(principalTypes []string) bool {
	return len(principalTypes) == 1 && principalTypes[0] == "user"
}

func onlyAllowsServicePrincipal(principalTypes []string) bool {
	return len(principalTypes) == 1 && principalTypes[0] == "service_principal"
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func ValidateEmbeddedBuiltinRoleManifestSchema() error {
	if !json.Valid(BuiltinRoleManifestSchema()) {
		return fmt.Errorf("embedded builtin role manifest schema is not valid JSON")
	}
	return nil
}
