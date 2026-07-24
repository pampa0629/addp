package authorization

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

const AuthorizationCatalogReportSchemaVersion = "addp.authorization_catalog_report/v1"

var stablePermissionOwnerModules = []string{
	"agent",
	"asset",
	"copilot",
	"develop",
	"graph",
	"manager",
	"meta",
	"model",
	"monitor",
	"orchestrator",
	"quality",
	"service",
	"standard",
	"system",
	"transfer",
}

type PermissionManifestReference struct {
	OwnerModule     string `json:"owner_module"`
	ManifestVersion int    `json:"manifest_version"`
	Path            string `json:"path"`
}

type BuiltinRoleManifestReference struct {
	ManifestVersion int    `json:"manifest_version"`
	Path            string `json:"path"`
}

type AuthorizationCatalogReport struct {
	SchemaVersion       string                        `json:"schema_version"`
	PermissionManifests []PermissionManifestReference `json:"permission_manifests"`
	BuiltinRoleManifest BuiltinRoleManifestReference  `json:"builtin_role_manifest"`
	Permissions         []PermissionDescriptor        `json:"permissions"`
	Roles               []BuiltinRoleDescriptor       `json:"roles"`
}

func StablePermissionOwnerModules() []string {
	return append([]string(nil), stablePermissionOwnerModules...)
}

func LoadRepositoryAuthorizationCatalog(repositoryRoot string) (AuthorizationCatalogReport, error) {
	if repositoryRoot == "" {
		return AuthorizationCatalogReport{}, fmt.Errorf("repository root must not be empty")
	}
	root, err := filepath.Abs(repositoryRoot)
	if err != nil {
		return AuthorizationCatalogReport{}, fmt.Errorf("resolve repository root: %w", err)
	}
	if info, err := os.Stat(root); err != nil {
		return AuthorizationCatalogReport{}, fmt.Errorf("stat repository root: %w", err)
	} else if !info.IsDir() {
		return AuthorizationCatalogReport{}, fmt.Errorf("repository root %q is not a directory", repositoryRoot)
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return AuthorizationCatalogReport{}, fmt.Errorf("read repository root: %w", err)
	}
	paths := make([]string, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name(), "authorization", "permissions.yaml")
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return AuthorizationCatalogReport{}, fmt.Errorf("stat permission manifest: %w", err)
		}
		if !info.Mode().IsRegular() {
			return AuthorizationCatalogReport{}, fmt.Errorf(
				"permission manifest %s is not a regular file",
				filepath.ToSlash(filepath.Join(entry.Name(), "authorization", "permissions.yaml")),
			)
		}
		paths = append(paths, path)
	}
	if len(paths) == 0 {
		return AuthorizationCatalogReport{}, fmt.Errorf("no permission manifests found under repository root")
	}
	sort.Strings(paths)

	allowedOwners := make(map[string]struct{}, len(stablePermissionOwnerModules))
	for _, owner := range stablePermissionOwnerModules {
		allowedOwners[owner] = struct{}{}
	}

	manifests := make([]PermissionManifest, 0, len(paths))
	references := make([]PermissionManifestReference, 0, len(paths))
	for _, path := range paths {
		owner := filepath.Base(filepath.Dir(filepath.Dir(path)))
		if _, allowed := allowedOwners[owner]; !allowed {
			return AuthorizationCatalogReport{}, fmt.Errorf("permission manifest directory %q is not a stable permission owner", owner)
		}
		manifest, err := LoadPermissionManifest(path)
		if err != nil {
			return AuthorizationCatalogReport{}, err
		}
		if manifest.OwnerModule != owner {
			return AuthorizationCatalogReport{}, fmt.Errorf(
				"permission manifest %s declares owner_module %q, want directory owner %q",
				filepath.ToSlash(filepath.Join(owner, "authorization", "permissions.yaml")),
				manifest.OwnerModule,
				owner,
			)
		}
		manifests = append(manifests, manifest)
		references = append(references, PermissionManifestReference{
			OwnerModule:     owner,
			ManifestVersion: manifest.ManifestVersion,
			Path:            filepath.ToSlash(filepath.Join(owner, "authorization", "permissions.yaml")),
		})
	}

	permissions, err := AggregatePermissionManifests(manifests)
	if err != nil {
		return AuthorizationCatalogReport{}, err
	}
	roleManifestPath := filepath.Join(root, "system", "authorization", "builtin_roles.yaml")
	roleManifest, err := LoadBuiltinRoleManifest(roleManifestPath)
	if err != nil {
		return AuthorizationCatalogReport{}, err
	}
	roles, err := ResolveBuiltinRoles(roleManifest, permissions)
	if err != nil {
		return AuthorizationCatalogReport{}, err
	}

	sort.Slice(references, func(i, j int) bool {
		return references[i].OwnerModule < references[j].OwnerModule
	})
	return AuthorizationCatalogReport{
		SchemaVersion:       AuthorizationCatalogReportSchemaVersion,
		PermissionManifests: references,
		BuiltinRoleManifest: BuiltinRoleManifestReference{
			ManifestVersion: roleManifest.ManifestVersion,
			Path:            "system/authorization/builtin_roles.yaml",
		},
		Permissions: permissions,
		Roles:       roles,
	}, nil
}

func MarshalAuthorizationCatalogReport(report AuthorizationCatalogReport) ([]byte, error) {
	if report.SchemaVersion != AuthorizationCatalogReportSchemaVersion {
		return nil, fmt.Errorf("schema_version must be %q", AuthorizationCatalogReportSchemaVersion)
	}

	canonical := AuthorizationCatalogReport{
		SchemaVersion:       report.SchemaVersion,
		PermissionManifests: append([]PermissionManifestReference(nil), report.PermissionManifests...),
		BuiltinRoleManifest: report.BuiltinRoleManifest,
		Permissions:         append([]PermissionDescriptor(nil), report.Permissions...),
		Roles:               append([]BuiltinRoleDescriptor(nil), report.Roles...),
	}
	sort.Slice(canonical.PermissionManifests, func(i, j int) bool {
		return canonical.PermissionManifests[i].OwnerModule < canonical.PermissionManifests[j].OwnerModule
	})
	sort.Slice(canonical.Permissions, func(i, j int) bool {
		return canonical.Permissions[i].Key < canonical.Permissions[j].Key
	})
	sort.Slice(canonical.Roles, func(i, j int) bool {
		return canonical.Roles[i].Key < canonical.Roles[j].Key
	})

	data, err := json.MarshalIndent(canonical, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal authorization catalog report: %w", err)
	}
	return append(data, '\n'), nil
}
