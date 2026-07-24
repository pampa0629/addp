package authorization

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const PermissionManifestSchemaVersion = "addp.permission_manifest/v1"

var (
	moduleNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	keyPartPattern    = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

	allowedActions = map[string]struct{}{
		"approve": {}, "cancel": {}, "close": {}, "create": {}, "delete": {},
		"execute": {}, "export": {}, "link": {}, "offline": {}, "publish": {},
		"read": {}, "reject": {}, "retry": {}, "revoke": {}, "suspend": {},
		"unlink": {}, "update": {},
	}
	allowedRiskLevels = map[string]struct{}{
		"low": {}, "medium": {}, "high": {}, "critical": {},
	}
	allowedStatuses = map[string]struct{}{
		"active": {}, "disabled": {},
	}
	reservedSystemDomains = map[string]struct{}{
		"audit": {}, "iam": {}, "platform": {}, "statistics": {}, "system": {},
	}
	scopeOrder = map[string]int{
		"platform": 0, "tenant": 1, "department": 2, "project_group": 3,
	}
)

//go:embed schemas/permission-manifest-v1.schema.json
var schemaFS embed.FS

type PermissionManifest struct {
	SchemaVersion   string                 `json:"schema_version" yaml:"schema_version"`
	OwnerModule     string                 `json:"owner_module" yaml:"owner_module"`
	ManifestVersion int                    `json:"manifest_version" yaml:"manifest_version"`
	Permissions     []PermissionDefinition `json:"permissions" yaml:"permissions"`
}

type PermissionDefinition struct {
	Key                string   `json:"key" yaml:"key"`
	AllowedScopeTypes  []string `json:"allowed_scope_types" yaml:"allowed_scope_types"`
	RiskLevel          string   `json:"risk_level" yaml:"risk_level"`
	TenantCustomizable bool     `json:"tenant_customizable" yaml:"tenant_customizable"`
	Delegable          bool     `json:"delegable" yaml:"delegable"`
	Status             string   `json:"status" yaml:"status"`
	NameI18nKey        string   `json:"name_i18n_key" yaml:"name_i18n_key"`
	DescriptionI18nKey string   `json:"description_i18n_key" yaml:"description_i18n_key"`
}

type PermissionDescriptor struct {
	Key                string   `json:"key"`
	OwnerModule        string   `json:"owner_module"`
	Action             string   `json:"action"`
	AllowedScopeTypes  []string `json:"allowed_scope_types"`
	RiskLevel          string   `json:"risk_level"`
	TenantCustomizable bool     `json:"tenant_customizable"`
	Delegable          bool     `json:"delegable"`
	Status             string   `json:"status"`
	NameI18nKey        string   `json:"name_i18n_key"`
	DescriptionI18nKey string   `json:"description_i18n_key"`
}

func PermissionManifestSchema() []byte {
	data, err := schemaFS.ReadFile("schemas/permission-manifest-v1.schema.json")
	if err != nil {
		panic(fmt.Sprintf("read embedded permission manifest schema: %v", err))
	}
	return bytes.Clone(data)
}

func LoadPermissionManifest(path string) (PermissionManifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return PermissionManifest{}, err
	}
	defer file.Close()

	manifest, err := DecodePermissionManifest(file)
	if err != nil {
		return PermissionManifest{}, fmt.Errorf("parse permission manifest %s: %w", path, err)
	}
	return manifest, nil
}

func DecodePermissionManifest(r io.Reader) (PermissionManifest, error) {
	decoder := yaml.NewDecoder(r)
	decoder.KnownFields(true)

	var manifest PermissionManifest
	if err := decoder.Decode(&manifest); err != nil {
		return PermissionManifest{}, err
	}

	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return PermissionManifest{}, fmt.Errorf("multiple YAML documents are not allowed")
		}
		return PermissionManifest{}, err
	}

	if err := ValidatePermissionManifest(manifest); err != nil {
		return PermissionManifest{}, err
	}
	return manifest, nil
}

func ValidatePermissionManifest(manifest PermissionManifest) error {
	if manifest.SchemaVersion != PermissionManifestSchemaVersion {
		return fmt.Errorf("schema_version must be %q", PermissionManifestSchemaVersion)
	}
	if !moduleNamePattern.MatchString(manifest.OwnerModule) {
		return fmt.Errorf("owner_module %q is not a stable module name", manifest.OwnerModule)
	}
	if manifest.ManifestVersion < 1 {
		return fmt.Errorf("manifest_version must be a positive integer")
	}
	if len(manifest.Permissions) == 0 {
		return fmt.Errorf("permissions must not be empty")
	}

	previousKey := ""
	for i, permission := range manifest.Permissions {
		if err := validatePermission(manifest.OwnerModule, permission); err != nil {
			return fmt.Errorf("permissions[%d]: %w", i, err)
		}
		if previousKey != "" && permission.Key <= previousKey {
			return fmt.Errorf("permissions must be strictly sorted by key: %q follows %q", permission.Key, previousKey)
		}
		previousKey = permission.Key
	}

	return nil
}

func AggregatePermissionManifests(manifests []PermissionManifest) ([]PermissionDescriptor, error) {
	if len(manifests) == 0 {
		return nil, fmt.Errorf("at least one permission manifest is required")
	}

	owners := make(map[string]struct{}, len(manifests))
	keys := make(map[string]string)
	descriptors := make([]PermissionDescriptor, 0)
	for i, manifest := range manifests {
		if err := ValidatePermissionManifest(manifest); err != nil {
			return nil, fmt.Errorf("manifests[%d]: %w", i, err)
		}
		if _, exists := owners[manifest.OwnerModule]; exists {
			return nil, fmt.Errorf("multiple manifests declare owner_module %q", manifest.OwnerModule)
		}
		owners[manifest.OwnerModule] = struct{}{}

		for _, permission := range manifest.Permissions {
			if owner, exists := keys[permission.Key]; exists {
				return nil, fmt.Errorf("permission key %q is declared by both %q and %q", permission.Key, owner, manifest.OwnerModule)
			}
			keys[permission.Key] = manifest.OwnerModule
			descriptors = append(descriptors, PermissionDescriptor{
				Key:                permission.Key,
				OwnerModule:        manifest.OwnerModule,
				Action:             actionFromKey(permission.Key),
				AllowedScopeTypes:  append([]string(nil), permission.AllowedScopeTypes...),
				RiskLevel:          permission.RiskLevel,
				TenantCustomizable: permission.TenantCustomizable,
				Delegable:          permission.Delegable,
				Status:             permission.Status,
				NameI18nKey:        permission.NameI18nKey,
				DescriptionI18nKey: permission.DescriptionI18nKey,
			})
		}
	}

	sort.Slice(descriptors, func(i, j int) bool {
		return descriptors[i].Key < descriptors[j].Key
	})
	return descriptors, nil
}

func LoadAndAggregatePermissionManifests(paths ...string) ([]PermissionDescriptor, error) {
	manifests := make([]PermissionManifest, 0, len(paths))
	for _, path := range paths {
		manifest, err := LoadPermissionManifest(path)
		if err != nil {
			return nil, err
		}
		manifests = append(manifests, manifest)
	}
	return AggregatePermissionManifests(manifests)
}

func validatePermission(owner string, permission PermissionDefinition) error {
	parts := strings.Split(permission.Key, ".")
	if len(parts) != 3 {
		return fmt.Errorf("key %q must use {domain}.{resource}.{action}", permission.Key)
	}
	for _, part := range parts {
		if !keyPartPattern.MatchString(part) {
			return fmt.Errorf("key %q contains invalid segment %q", permission.Key, part)
		}
	}
	if owner == "system" {
		if _, ok := reservedSystemDomains[parts[0]]; !ok {
			return fmt.Errorf("system manifest cannot declare domain %q", parts[0])
		}
	} else if parts[0] != owner {
		return fmt.Errorf("owner %q cannot declare permission key %q", owner, permission.Key)
	}
	if _, ok := allowedActions[parts[2]]; !ok {
		return fmt.Errorf("key %q uses unsupported action %q", permission.Key, parts[2])
	}
	if _, ok := allowedRiskLevels[permission.RiskLevel]; !ok {
		return fmt.Errorf("key %q uses unsupported risk_level %q", permission.Key, permission.RiskLevel)
	}
	if _, ok := allowedStatuses[permission.Status]; !ok {
		return fmt.Errorf("key %q uses unsupported status %q", permission.Key, permission.Status)
	}
	if err := validateScopes(owner, permission.Key, permission.AllowedScopeTypes); err != nil {
		return err
	}
	if permission.TenantCustomizable && !hasTenantScope(permission.AllowedScopeTypes) {
		return fmt.Errorf("key %q is tenant_customizable but has no tenant scope", permission.Key)
	}
	if expected := "permissions." + permission.Key + ".name"; permission.NameI18nKey != expected {
		return fmt.Errorf("key %q name_i18n_key must be %q", permission.Key, expected)
	}
	if expected := "permissions." + permission.Key + ".description"; permission.DescriptionI18nKey != expected {
		return fmt.Errorf("key %q description_i18n_key must be %q", permission.Key, expected)
	}
	return nil
}

func validateScopes(owner, key string, scopes []string) error {
	if len(scopes) == 0 {
		return fmt.Errorf("key %q must declare allowed_scope_types", key)
	}
	previousRank := -1
	for _, scope := range scopes {
		rank, ok := scopeOrder[scope]
		if !ok {
			return fmt.Errorf("key %q uses unsupported scope %q", key, scope)
		}
		if rank <= previousRank {
			return fmt.Errorf("key %q allowed_scope_types must be unique and ordered", key)
		}
		if owner != "system" && scope == "platform" {
			return fmt.Errorf("non-system owner %q cannot declare platform scope for key %q", owner, key)
		}
		previousRank = rank
	}
	return nil
}

func hasTenantScope(scopes []string) bool {
	for _, scope := range scopes {
		if scope != "platform" {
			return true
		}
	}
	return false
}

func actionFromKey(key string) string {
	parts := strings.Split(key, ".")
	return parts[len(parts)-1]
}

func ValidateEmbeddedPermissionManifestSchema() error {
	if !json.Valid(PermissionManifestSchema()) {
		return fmt.Errorf("embedded permission manifest schema is not valid JSON")
	}
	return nil
}
