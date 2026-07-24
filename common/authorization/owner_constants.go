package authorization

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

const GeneratedOwnerConstantsReportSchemaVersion = "addp.generated_owner_constants/v1"

var pythonPermissionOwners = map[string]struct{}{
	"agent":   {},
	"copilot": {},
}

type GeneratedOwnerConstantFile struct {
	OwnerModule string `json:"owner_module"`
	Path        string `json:"path"`
	Content     []byte `json:"-"`
}

type GeneratedOwnerConstantsReport struct {
	SchemaVersion string                                `json:"schema_version"`
	Files         []GeneratedOwnerConstantFileReference `json:"files"`
}

type GeneratedOwnerConstantFileReference struct {
	OwnerModule string `json:"owner_module"`
	Path        string `json:"path"`
}

func GenerateOwnerPermissionConstants(report AuthorizationCatalogReport) ([]GeneratedOwnerConstantFile, error) {
	if report.SchemaVersion != AuthorizationCatalogReportSchemaVersion {
		return nil, fmt.Errorf("catalog schema_version must be %q", AuthorizationCatalogReportSchemaVersion)
	}
	if len(report.PermissionManifests) == 0 {
		return nil, fmt.Errorf("catalog must contain permission manifests")
	}

	permissionsByOwner := make(map[string][]PermissionDescriptor, len(report.PermissionManifests))
	for _, permission := range report.Permissions {
		if permission.Status != "active" {
			continue
		}
		permissionsByOwner[permission.OwnerModule] = append(permissionsByOwner[permission.OwnerModule], permission)
	}

	manifestByOwner := make(map[string]PermissionManifestReference, len(report.PermissionManifests))
	for _, manifest := range report.PermissionManifests {
		if _, exists := manifestByOwner[manifest.OwnerModule]; exists {
			return nil, fmt.Errorf("catalog contains duplicate owner manifest %q", manifest.OwnerModule)
		}
		manifestByOwner[manifest.OwnerModule] = manifest
	}

	owners := make([]string, 0, len(manifestByOwner))
	for owner := range manifestByOwner {
		owners = append(owners, owner)
	}
	sort.Strings(owners)

	files := make([]GeneratedOwnerConstantFile, 0, len(owners))
	for _, owner := range owners {
		permissions := append([]PermissionDescriptor(nil), permissionsByOwner[owner]...)
		sort.Slice(permissions, func(i, j int) bool { return permissions[i].Key < permissions[j].Key })
		if len(permissions) == 0 {
			return nil, fmt.Errorf("owner %q has no active permissions to generate", owner)
		}

		manifest := manifestByOwner[owner]
		var (
			path    string
			content []byte
			err     error
		)
		if _, python := pythonPermissionOwners[owner]; python {
			path = filepath.ToSlash(filepath.Join(owner, "backend", "authorization_permissions_generated.py"))
			content, err = generatePythonPermissionConstants(manifest, permissions)
		} else {
			path = filepath.ToSlash(filepath.Join(owner, "backend", "internal", "authorization", "permissions_generated.go"))
			content, err = generateGoPermissionConstants(manifest, permissions)
		}
		if err != nil {
			return nil, fmt.Errorf("generate owner %q permission constants: %w", owner, err)
		}
		files = append(files, GeneratedOwnerConstantFile{
			OwnerModule: owner,
			Path:        path,
			Content:     content,
		})
	}
	return files, nil
}

func WriteGeneratedOwnerPermissionConstants(repositoryRoot string, files []GeneratedOwnerConstantFile) error {
	root, err := validateRepositoryRoot(repositoryRoot)
	if err != nil {
		return err
	}
	for _, file := range files {
		path, err := generatedFilePath(root, file.Path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("create generated permission directory %s: %w", file.Path, err)
		}
		if err := os.WriteFile(path, file.Content, 0o644); err != nil {
			return fmt.Errorf("write generated permission file %s: %w", file.Path, err)
		}
	}
	return nil
}

func CheckGeneratedOwnerPermissionConstants(repositoryRoot string, files []GeneratedOwnerConstantFile) error {
	root, err := validateRepositoryRoot(repositoryRoot)
	if err != nil {
		return err
	}
	for _, file := range files {
		path, err := generatedFilePath(root, file.Path)
		if err != nil {
			return err
		}
		actual, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("generated permission file %s is missing; run --generate-owner-constants", file.Path)
			}
			return fmt.Errorf("read generated permission file %s: %w", file.Path, err)
		}
		if !bytes.Equal(actual, file.Content) {
			return fmt.Errorf("generated permission file %s is stale; run --generate-owner-constants", file.Path)
		}
	}
	return nil
}

func BuildGeneratedOwnerConstantsReport(files []GeneratedOwnerConstantFile) GeneratedOwnerConstantsReport {
	references := make([]GeneratedOwnerConstantFileReference, 0, len(files))
	for _, file := range files {
		references = append(references, GeneratedOwnerConstantFileReference{
			OwnerModule: file.OwnerModule,
			Path:        file.Path,
		})
	}
	sort.Slice(references, func(i, j int) bool {
		if references[i].OwnerModule == references[j].OwnerModule {
			return references[i].Path < references[j].Path
		}
		return references[i].OwnerModule < references[j].OwnerModule
	})
	return GeneratedOwnerConstantsReport{
		SchemaVersion: GeneratedOwnerConstantsReportSchemaVersion,
		Files:         references,
	}
}

func MarshalGeneratedOwnerConstantsReport(report GeneratedOwnerConstantsReport) ([]byte, error) {
	if report.SchemaVersion != GeneratedOwnerConstantsReportSchemaVersion {
		return nil, fmt.Errorf("schema_version must be %q", GeneratedOwnerConstantsReportSchemaVersion)
	}
	canonical := GeneratedOwnerConstantsReport{
		SchemaVersion: report.SchemaVersion,
		Files:         append([]GeneratedOwnerConstantFileReference(nil), report.Files...),
	}
	sort.Slice(canonical.Files, func(i, j int) bool {
		if canonical.Files[i].OwnerModule == canonical.Files[j].OwnerModule {
			return canonical.Files[i].Path < canonical.Files[j].Path
		}
		return canonical.Files[i].OwnerModule < canonical.Files[j].OwnerModule
	})
	data, err := json.MarshalIndent(canonical, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal generated owner constants report: %w", err)
	}
	return append(data, '\n'), nil
}

func generateGoPermissionConstants(manifest PermissionManifestReference, permissions []PermissionDescriptor) ([]byte, error) {
	var source strings.Builder
	fmt.Fprintln(&source, "// Code generated by common/authorization/cmd/manifest; DO NOT EDIT.")
	fmt.Fprintf(&source, "// Source: %s (manifest_version=%d).\n\n", manifest.Path, manifest.ManifestVersion)
	fmt.Fprintln(&source, "package authorization")
	fmt.Fprintln(&source)
	fmt.Fprintln(&source, "const (")

	identifiers := make(map[string]string, len(permissions))
	for _, permission := range permissions {
		identifier := "Permission" + goPermissionIdentifier(permission.Key)
		if previous, exists := identifiers[identifier]; exists {
			return nil, fmt.Errorf("permission keys %q and %q generate duplicate identifier %q", previous, permission.Key, identifier)
		}
		identifiers[identifier] = permission.Key
		fmt.Fprintf(&source, "\t%s = %q\n", identifier, permission.Key)
	}
	fmt.Fprintln(&source, ")")
	fmt.Fprintln(&source)
	fmt.Fprintln(&source, "var permissionKeys = [...]string{")
	for _, permission := range permissions {
		fmt.Fprintf(&source, "\t%q,\n", permission.Key)
	}
	fmt.Fprintln(&source, "}")
	fmt.Fprintln(&source)
	fmt.Fprintln(&source, "// PermissionKeys returns the active owner-local Permission keys in stable order.")
	fmt.Fprintln(&source, "func PermissionKeys() []string {")
	fmt.Fprintln(&source, "\treturn append([]string(nil), permissionKeys[:]...)")
	fmt.Fprintln(&source, "}")

	formatted, err := format.Source([]byte(source.String()))
	if err != nil {
		return nil, fmt.Errorf("format generated Go source: %w", err)
	}
	return formatted, nil
}

func generatePythonPermissionConstants(manifest PermissionManifestReference, permissions []PermissionDescriptor) ([]byte, error) {
	var source strings.Builder
	fmt.Fprintln(&source, "# Code generated by common/authorization/cmd/manifest; DO NOT EDIT.")
	fmt.Fprintf(&source, "# Source: %s (manifest_version=%d).\n\n", manifest.Path, manifest.ManifestVersion)
	fmt.Fprintln(&source, "from typing import Final")
	fmt.Fprintln(&source)

	identifiers := make(map[string]string, len(permissions))
	for _, permission := range permissions {
		identifier := strings.ToUpper(strings.ReplaceAll(permission.Key, ".", "_"))
		if previous, exists := identifiers[identifier]; exists {
			return nil, fmt.Errorf("permission keys %q and %q generate duplicate identifier %q", previous, permission.Key, identifier)
		}
		identifiers[identifier] = permission.Key
		fmt.Fprintf(&source, "%s: Final[str] = %q\n", identifier, permission.Key)
	}
	fmt.Fprintln(&source)
	fmt.Fprintln(&source, "PERMISSION_KEYS: Final[tuple[str, ...]] = (")
	for _, permission := range permissions {
		fmt.Fprintf(&source, "    %q,\n", permission.Key)
	}
	fmt.Fprintln(&source, ")")
	return []byte(source.String()), nil
}

func goPermissionIdentifier(key string) string {
	parts := strings.FieldsFunc(key, func(r rune) bool { return r == '.' || r == '_' })
	var identifier strings.Builder
	for _, part := range parts {
		runes := []rune(part)
		if len(runes) == 0 {
			continue
		}
		runes[0] = unicode.ToUpper(runes[0])
		identifier.WriteString(string(runes))
	}
	return identifier.String()
}

func validateRepositoryRoot(repositoryRoot string) (string, error) {
	if repositoryRoot == "" {
		return "", fmt.Errorf("repository root must not be empty")
	}
	root, err := filepath.Abs(repositoryRoot)
	if err != nil {
		return "", fmt.Errorf("resolve repository root: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("stat repository root: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("repository root %q is not a directory", repositoryRoot)
	}
	return root, nil
}

func generatedFilePath(root, relativePath string) (string, error) {
	if relativePath == "" || filepath.IsAbs(relativePath) {
		return "", fmt.Errorf("generated file path %q must be repository-relative", relativePath)
	}
	clean := filepath.Clean(filepath.FromSlash(relativePath))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("generated file path %q escapes repository root", relativePath)
	}
	return filepath.Join(root, clean), nil
}
