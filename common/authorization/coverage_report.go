package authorization

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const AuthorizationCoverageReportSchemaVersion = "addp.authorization_coverage_report/v1"

var validAuthorizationModes = map[string]struct{}{
	"authenticated":   {},
	"delegated_tool":  {},
	"internal":        {},
	"permission":      {},
	"public":          {},
	"resource_ticket": {},
	"self":            {},
}

type AuthorizationCoverageReport struct {
	SchemaVersion string                       `json:"schema_version"`
	Complete      bool                         `json:"complete"`
	OpenAPI       []OpenAPICoverageSource      `json:"openapi"`
	ToolManifest  ToolManifestCoverageSource   `json:"tool_manifest"`
	Issues        []AuthorizationCoverageIssue `json:"issues"`
}

type OpenAPICoverageSource struct {
	OwnerModule                string `json:"owner_module"`
	Path                       string `json:"path"`
	Status                     string `json:"status"`
	OperationCount             int    `json:"operation_count"`
	DeclaredAuthOperationCount int    `json:"declared_auth_operation_count"`
}

type ToolManifestCoverageSource struct {
	Path            string `json:"path"`
	Status          string `json:"status"`
	ToolCount       int    `json:"tool_count"`
	MappedToolCount int    `json:"mapped_tool_count"`
}

type AuthorizationCoverageIssue struct {
	Code        string `json:"code"`
	SourceType  string `json:"source_type"`
	OwnerModule string `json:"owner_module,omitempty"`
	SourcePath  string `json:"source_path"`
	Method      string `json:"method,omitempty"`
	Path        string `json:"path,omitempty"`
	ToolName    string `json:"tool_name,omitempty"`
	Detail      string `json:"detail"`
}

type toolManifestDocument struct {
	Tools []toolManifestDefinition `json:"tools"`
}

type toolManifestDefinition struct {
	Name  string `json:"name"`
	Owner string `json:"owner"`
	Auth  struct {
		Audience            string   `json:"audience"`
		RequiredScopes      []string `json:"required_scopes"`
		RequiredPermissions []string `json:"required_permissions"`
	} `json:"auth"`
}

func BuildRepositoryAuthorizationCoverageReport(repositoryRoot string, catalog AuthorizationCatalogReport) (AuthorizationCoverageReport, error) {
	root, err := validateRepositoryRoot(repositoryRoot)
	if err != nil {
		return AuthorizationCoverageReport{}, err
	}
	if catalog.SchemaVersion != AuthorizationCatalogReportSchemaVersion {
		return AuthorizationCoverageReport{}, fmt.Errorf("catalog schema_version must be %q", AuthorizationCatalogReportSchemaVersion)
	}

	permissionCatalog := make(map[string]PermissionDescriptor, len(catalog.Permissions))
	for _, permission := range catalog.Permissions {
		permissionCatalog[permission.Key] = permission
	}
	ownerCatalog := make(map[string]struct{}, len(catalog.PermissionManifests))
	for _, manifest := range catalog.PermissionManifests {
		ownerCatalog[manifest.OwnerModule] = struct{}{}
	}

	report := AuthorizationCoverageReport{
		SchemaVersion: AuthorizationCoverageReportSchemaVersion,
		OpenAPI:       make([]OpenAPICoverageSource, 0, len(catalog.PermissionManifests)),
		Issues:        make([]AuthorizationCoverageIssue, 0),
	}
	referencedPermissions := make(map[string]struct{})
	for _, manifest := range catalog.PermissionManifests {
		source, referenced, issues := inspectOwnerOpenAPI(root, manifest.OwnerModule, permissionCatalog)
		report.OpenAPI = append(report.OpenAPI, source)
		report.Issues = append(report.Issues, issues...)
		for key := range referenced {
			referencedPermissions[key] = struct{}{}
		}
	}

	toolSource, tools, toolIssues := inspectToolManifest(root, ownerCatalog, permissionCatalog)
	report.ToolManifest = toolSource
	report.Issues = append(report.Issues, toolIssues...)
	for _, tool := range tools {
		for _, key := range tool.RequiredPermissions {
			referencedPermissions[key] = struct{}{}
		}
	}
	for _, permission := range catalog.Permissions {
		if permission.Status != "active" {
			continue
		}
		if _, referenced := referencedPermissions[permission.Key]; referenced {
			continue
		}
		report.Issues = append(report.Issues, AuthorizationCoverageIssue{
			Code:        "unreferenced_active_permission",
			SourceType:  "permission_catalog",
			OwnerModule: permission.OwnerModule,
			SourcePath:  filepath.ToSlash(filepath.Join(permission.OwnerModule, "authorization", "permissions.yaml")),
			Detail:      fmt.Sprintf("active Permission %q must be referenced by an OpenAPI operation or delegated Tool", permission.Key),
		})
	}
	canonicalizeAuthorizationCoverageReport(&report)
	report.Complete = len(report.Issues) == 0
	return report, nil
}

func MarshalAuthorizationCoverageReport(report AuthorizationCoverageReport) ([]byte, error) {
	if report.SchemaVersion != AuthorizationCoverageReportSchemaVersion {
		return nil, fmt.Errorf("schema_version must be %q", AuthorizationCoverageReportSchemaVersion)
	}
	canonical := report
	canonical.OpenAPI = append([]OpenAPICoverageSource(nil), report.OpenAPI...)
	canonical.Issues = append([]AuthorizationCoverageIssue(nil), report.Issues...)
	canonicalizeAuthorizationCoverageReport(&canonical)
	canonical.Complete = len(canonical.Issues) == 0
	data, err := json.MarshalIndent(canonical, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal authorization coverage report: %w", err)
	}
	return append(data, '\n'), nil
}

func inspectOwnerOpenAPI(root, owner string, permissions map[string]PermissionDescriptor) (OpenAPICoverageSource, map[string]struct{}, []AuthorizationCoverageIssue) {
	relativePath := filepath.ToSlash(filepath.Join(owner, "backend", "docs", "swagger.json"))
	if _, python := pythonPermissionOwners[owner]; python {
		relativePath = filepath.ToSlash(filepath.Join(owner, "backend", "openapi.json"))
	}
	source := OpenAPICoverageSource{OwnerModule: owner, Path: relativePath, Status: "available"}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relativePath)))
	if err != nil {
		source.Status = "missing"
		return source, nil, []AuthorizationCoverageIssue{{
			Code:        "missing_openapi_document",
			SourceType:  "openapi",
			OwnerModule: owner,
			SourcePath:  relativePath,
			Detail:      "OpenAPI/Swagger document is required for authorization coverage",
		}}
	}

	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		source.Status = "invalid"
		return source, nil, []AuthorizationCoverageIssue{{
			Code:        "invalid_openapi_document",
			SourceType:  "openapi",
			OwnerModule: owner,
			SourcePath:  relativePath,
			Detail:      "OpenAPI/Swagger document is not valid JSON",
		}}
	}

	paths, _ := document["paths"].(map[string]any)
	issues := make([]AuthorizationCoverageIssue, 0)
	referencedPermissions := make(map[string]struct{})
	pathNames := make([]string, 0, len(paths))
	for path := range paths {
		pathNames = append(pathNames, path)
	}
	sort.Strings(pathNames)
	for _, path := range pathNames {
		pathItem, _ := paths[path].(map[string]any)
		methods := make([]string, 0)
		for method := range pathItem {
			if isOpenAPIMethod(method) {
				methods = append(methods, strings.ToUpper(method))
			}
		}
		sort.Strings(methods)
		for _, method := range methods {
			operation, _ := pathItem[strings.ToLower(method)].(map[string]any)
			source.OperationCount++
			authMode, _ := operation["x-addp-auth-mode"].(string)
			if authMode == "" {
				issues = append(issues, openAPIIssue("missing_auth_mode", owner, relativePath, method, path, "operation must declare x-addp-auth-mode"))
				continue
			}
			source.DeclaredAuthOperationCount++
			if _, valid := validAuthorizationModes[authMode]; !valid {
				issues = append(issues, openAPIIssue("invalid_auth_mode", owner, relativePath, method, path, fmt.Sprintf("unsupported x-addp-auth-mode %q", authMode)))
				continue
			}

			requiredPermissions, valid := stringListExtension(operation["x-addp-required-permissions"])
			if !valid {
				issues = append(issues, openAPIIssue("invalid_required_permissions", owner, relativePath, method, path, "x-addp-required-permissions must be an array of unique Permission keys"))
				continue
			}
			requiresPermissions := authMode == "permission" || authMode == "delegated_tool" || authMode == "resource_ticket"
			if requiresPermissions && len(requiredPermissions) == 0 {
				issues = append(issues, openAPIIssue("missing_required_permissions", owner, relativePath, method, path, fmt.Sprintf("auth mode %q requires x-addp-required-permissions", authMode)))
				continue
			}
			if !requiresPermissions && len(requiredPermissions) > 0 {
				issues = append(issues, openAPIIssue("unexpected_required_permissions", owner, relativePath, method, path, fmt.Sprintf("auth mode %q must not declare business Permission keys", authMode)))
				continue
			}
			conditionalPermissions, valid := stringListExtension(operation["x-addp-conditional-permissions"])
			if !valid {
				issues = append(issues, openAPIIssue("invalid_conditional_permissions", owner, relativePath, method, path, "x-addp-conditional-permissions must be an array of unique Permission keys"))
				continue
			}
			if authMode != "permission" && len(conditionalPermissions) > 0 {
				issues = append(issues, openAPIIssue("unexpected_conditional_permissions", owner, relativePath, method, path, fmt.Sprintf("auth mode %q must not declare conditional Permission keys", authMode)))
				continue
			}
			staticPermissions := make(map[string]struct{}, len(requiredPermissions))
			for _, key := range requiredPermissions {
				staticPermissions[key] = struct{}{}
			}
			for _, key := range requiredPermissions {
				referencedPermissions[key] = struct{}{}
				permission, exists := permissions[key]
				if !exists {
					issues = append(issues, openAPIIssue("unknown_permission", owner, relativePath, method, path, fmt.Sprintf("operation references unknown Permission %q", key)))
					continue
				}
				if permission.Status != "active" {
					issues = append(issues, openAPIIssue("disabled_permission", owner, relativePath, method, path, fmt.Sprintf("operation references non-active Permission %q", key)))
				}
			}
			for _, key := range conditionalPermissions {
				if _, duplicate := staticPermissions[key]; duplicate {
					issues = append(issues, openAPIIssue("overlapping_conditional_permission", owner, relativePath, method, path, fmt.Sprintf("conditional Permission %q must not also be a static all-of Permission", key)))
					continue
				}
				referencedPermissions[key] = struct{}{}
				permission, exists := permissions[key]
				if !exists {
					issues = append(issues, openAPIIssue("unknown_conditional_permission", owner, relativePath, method, path, fmt.Sprintf("operation references unknown conditional Permission %q", key)))
					continue
				}
				if permission.Status != "active" {
					issues = append(issues, openAPIIssue("disabled_conditional_permission", owner, relativePath, method, path, fmt.Sprintf("operation references non-active conditional Permission %q", key)))
				}
			}
		}
	}
	return source, referencedPermissions, issues
}

func inspectToolManifest(
	root string,
	owners map[string]struct{},
	permissions map[string]PermissionDescriptor,
) (ToolManifestCoverageSource, []ToolAuthorization, []AuthorizationCoverageIssue) {
	const relativePath = "common-python/addp_common/tools/manifest.json"
	source := ToolManifestCoverageSource{Path: relativePath, Status: "available"}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relativePath)))
	if err != nil {
		source.Status = "missing"
		return source, nil, []AuthorizationCoverageIssue{{
			Code:       "missing_tool_manifest",
			SourceType: "tool_manifest",
			SourcePath: relativePath,
			Detail:     "Tool Manifest is required for authorization coverage",
		}}
	}

	var document toolManifestDocument
	if err := json.Unmarshal(data, &document); err != nil {
		source.Status = "invalid"
		return source, nil, []AuthorizationCoverageIssue{{
			Code:       "invalid_tool_manifest",
			SourceType: "tool_manifest",
			SourcePath: relativePath,
			Detail:     "Tool Manifest is not valid JSON",
		}}
	}

	sort.Slice(document.Tools, func(i, j int) bool { return document.Tools[i].Name < document.Tools[j].Name })
	issues := make([]AuthorizationCoverageIssue, 0)
	tools := make([]ToolAuthorization, 0, len(document.Tools))
	seenTools := make(map[string]struct{}, len(document.Tools))
	for _, tool := range document.Tools {
		source.ToolCount++
		if tool.Name == "" {
			issues = append(issues, toolIssue("invalid_tool_name", tool, relativePath, "tool name must not be empty"))
			continue
		}
		if _, duplicate := seenTools[tool.Name]; duplicate {
			issues = append(issues, toolIssue("duplicate_tool_name", tool, relativePath, fmt.Sprintf("tool name %q is declared more than once", tool.Name)))
			continue
		}
		seenTools[tool.Name] = struct{}{}
		if _, exists := owners[tool.Owner]; !exists {
			issues = append(issues, toolIssue("unknown_tool_owner", tool, relativePath, fmt.Sprintf("tool owner %q is not a stable Permission owner", tool.Owner)))
		}
		if tool.Auth.Audience != tool.Owner {
			issues = append(issues, toolIssue("tool_audience_owner_mismatch", tool, relativePath, fmt.Sprintf("tool audience %q must equal owner %q", tool.Auth.Audience, tool.Owner)))
		}
		if len(tool.Auth.RequiredScopes) != 1 || tool.Auth.RequiredScopes[0] != tool.Name {
			issues = append(issues, toolIssue("invalid_tool_scope", tool, relativePath, "required_scopes must contain only the stable Tool name"))
		}
		if len(tool.Auth.RequiredPermissions) == 0 {
			issues = append(issues, toolIssue("missing_tool_permission_mapping", tool, relativePath, "delegated Tool must explicitly map to owner Permission keys"))
			continue
		}
		source.MappedToolCount++
		seen := make(map[string]struct{}, len(tool.Auth.RequiredPermissions))
		for _, key := range tool.Auth.RequiredPermissions {
			if _, duplicate := seen[key]; duplicate {
				issues = append(issues, toolIssue("duplicate_tool_permission", tool, relativePath, fmt.Sprintf("Permission %q is mapped more than once", key)))
				continue
			}
			seen[key] = struct{}{}
			permission, exists := permissions[key]
			if !exists {
				issues = append(issues, toolIssue("unknown_tool_permission", tool, relativePath, fmt.Sprintf("tool references unknown Permission %q", key)))
				continue
			}
			if permission.OwnerModule != tool.Owner {
				issues = append(issues, toolIssue("cross_owner_tool_permission", tool, relativePath, fmt.Sprintf("Permission %q is owned by %q", key, permission.OwnerModule)))
			}
			if permission.Status != "active" {
				issues = append(issues, toolIssue("disabled_tool_permission", tool, relativePath, fmt.Sprintf("Permission %q is not active", key)))
			}
			if !permission.Delegable {
				issues = append(issues, toolIssue("non_delegable_tool_permission", tool, relativePath, fmt.Sprintf("Permission %q is not delegable", key)))
			}
		}
		tools = append(tools, ToolAuthorization{
			Name:                tool.Name,
			Owner:               tool.Owner,
			RequiredScopes:      append([]string(nil), tool.Auth.RequiredScopes...),
			RequiredPermissions: append([]string(nil), tool.Auth.RequiredPermissions...),
		})
	}
	return source, tools, issues
}

func stringListExtension(value any) ([]string, bool) {
	if value == nil {
		return nil, true
	}
	raw, ok := value.([]any)
	if !ok {
		return nil, false
	}
	values := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, item := range raw {
		value, ok := item.(string)
		if !ok || value == "" {
			return nil, false
		}
		if _, duplicate := seen[value]; duplicate {
			return nil, false
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	sort.Strings(values)
	return values, true
}

func isOpenAPIMethod(value string) bool {
	switch strings.ToLower(value) {
	case "delete", "get", "head", "options", "patch", "post", "put":
		return true
	default:
		return false
	}
}

func openAPIIssue(code, owner, sourcePath, method, path, detail string) AuthorizationCoverageIssue {
	return AuthorizationCoverageIssue{
		Code:        code,
		SourceType:  "openapi",
		OwnerModule: owner,
		SourcePath:  sourcePath,
		Method:      method,
		Path:        path,
		Detail:      detail,
	}
}

func toolIssue(code string, tool toolManifestDefinition, sourcePath, detail string) AuthorizationCoverageIssue {
	return AuthorizationCoverageIssue{
		Code:        code,
		SourceType:  "tool_manifest",
		OwnerModule: tool.Owner,
		SourcePath:  sourcePath,
		ToolName:    tool.Name,
		Detail:      detail,
	}
}

func canonicalizeAuthorizationCoverageReport(report *AuthorizationCoverageReport) {
	sort.Slice(report.OpenAPI, func(i, j int) bool {
		return report.OpenAPI[i].OwnerModule < report.OpenAPI[j].OwnerModule
	})
	sort.Slice(report.Issues, func(i, j int) bool {
		left := report.Issues[i]
		right := report.Issues[j]
		leftKey := strings.Join([]string{left.SourceType, left.OwnerModule, left.SourcePath, left.Path, left.Method, left.ToolName, left.Code, left.Detail}, "\x00")
		rightKey := strings.Join([]string{right.SourceType, right.OwnerModule, right.SourcePath, right.Path, right.Method, right.ToolName, right.Code, right.Detail}, "\x00")
		return leftKey < rightKey
	})
}
