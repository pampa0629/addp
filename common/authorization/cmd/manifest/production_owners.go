package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/addp/common/authorization"
	"github.com/addp/common/client"
	"github.com/addp/common/production"
)

func writeProductionOwnerReport(root string, catalog authorization.AuthorizationCatalogReport, output io.Writer) error {
	// Only compose readers. Publication checks perform no network IO.
	registry, err := client.NewProductionRegistry(client.NewTransferClient("", nil), client.NewDevelopClient("", nil, nil))
	if err != nil {
		return err
	}
	owners, err := checkProductionOwnerCatalog(catalog, registry)
	if err != nil {
		return err
	}
	if err := checkProductionOwnerWiring(root, catalog.PermissionManifests); err != nil {
		return err
	}
	if err := checkProductionOwnerCI(root); err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(struct {
		Owners []string `json:"owners"`
	}{owners})
}

func checkProductionOwnerCatalog(catalog authorization.AuthorizationCatalogReport, registry *production.Registry) ([]string, error) {
	permissions := make(map[string]authorization.PermissionDescriptor, len(catalog.Permissions))
	for _, permission := range catalog.Permissions {
		permissions[permission.Key] = permission
	}
	registered := map[string]bool{}
	var names []string
	for _, owner := range registry.Owners() {
		receipt, ok := permissions[owner.ReceiptPermission]
		if !ok || receipt.OwnerModule != owner.Module || receipt.Status != "active" || receipt.TenantCustomizable || receipt.Delegable || !slices.Equal(receipt.AllowedScopeTypes, []string{"tenant"}) {
			return nil, fmt.Errorf("producer %s requires its active Tenant-only non-delegable machine receipt permission", owner.Module)
		}
		execute, ok := permissions[owner.ExecutionPermission]
		if !ok || execute.OwnerModule != owner.Module || execute.Status != "active" || !slices.Contains(execute.AllowedScopeTypes, "tenant") {
			return nil, fmt.Errorf("producer %s execution permission is not active in its Tenant scope", owner.Module)
		}
		readers := map[string]bool{"tenant.system_runtime": false, "tenant.meta_runtime": false}
		for _, role := range catalog.Roles {
			if !slices.Contains(role.Permissions, owner.ReceiptPermission) {
				continue
			}
			if _, expected := readers[role.Key]; !expected || !slices.Equal(role.AllowedPrincipalTypes, []string{"service_principal"}) || !slices.Equal(role.AllowedScopeTypes, []string{"tenant"}) {
				return nil, fmt.Errorf("producer %s receipt permission escapes fixed Tenant System/Meta service roles: %s", owner.Module, role.Key)
			}
			readers[role.Key] = true
		}
		for role, found := range readers {
			if !found {
				return nil, fmt.Errorf("producer %s receipt reader role missing: %s", owner.Module, role)
			}
		}
		registered[owner.ReceiptPermission] = true
		names = append(names, owner.Module)
	}
	for _, permission := range catalog.Permissions {
		if permission.Status == "active" && strings.HasSuffix(permission.Key, ".production_receipt.read") && !registered[permission.Key] {
			return nil, fmt.Errorf("active producer receipt permission has no shared registration: %s", permission.Key)
		}
	}
	return names, nil
}

func checkProductionOwnerWiring(root string, manifests []authorization.PermissionManifestReference) error {
	const factory = "common/client/production_registry.go"
	required := map[string]bool{"system/backend/internal/api/router.go": false, "meta/backend/cmd/server/main.go": false, "meta/backend/cmd/worker/main.go": false}
	directories := []string{"common/client"}
	for _, manifest := range manifests {
		directories = append(directories, manifest.OwnerModule+"/backend")
	}
	for _, directory := range directories {
		base := filepath.Join(root, directory)
		if _, err := os.Stat(base); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return err
		}
		err := filepath.WalkDir(base, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			relative = filepath.ToSlash(relative)
			source, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				return err
			}
			imports := map[string]string{}
			for _, item := range source.Imports {
				target, err := strconv.Unquote(item.Path.Value)
				if err != nil {
					return err
				}
				name := filepath.Base(target)
				if item.Name != nil {
					name = item.Name.Name
				}
				imports[name] = target
			}
			var violation error
			ast.Inspect(source, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					if name, ok := call.Fun.(*ast.Ident); ok && name.Name == "NewRegistry" && imports["."] == "github.com/addp/common/production" {
						violation = fmt.Errorf("producer registry uses a dot-import bypass: %s", relative)
					}
					return true
				}
				qualifier, ok := selector.X.(*ast.Ident)
				if !ok {
					return true
				}
				if selector.Sel.Name == "NewRegistry" && imports[qualifier.Name] == "github.com/addp/common/production" && relative != factory {
					violation = fmt.Errorf("producer registry must be composed only in %s, found %s", factory, relative)
				}
				if selector.Sel.Name == "NewProductionRegistry" && imports[qualifier.Name] == "github.com/addp/common/client" {
					if _, expected := required[relative]; expected {
						required[relative] = true
					}
				}
				return true
			})
			return violation
		})
		if err != nil {
			return err
		}
	}
	for path, found := range required {
		if !found {
			return fmt.Errorf("runtime is missing the shared production registry factory: %s", path)
		}
	}
	return nil
}

// Check the independent gate as well as its reuse by the aggregate entry point.
func checkProductionOwnerCI(root string) error {
	source, err := os.ReadFile(filepath.Join(root, ".github/workflows/platform-ci.yml"))
	if err != nil {
		return err
	}
	var workflow struct {
		Jobs map[string]struct {
			Needs any    `yaml:"needs"`
			If    string `yaml:"if"`
			Steps []struct {
				ID  string `yaml:"id"`
				If  string `yaml:"if"`
				Run string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(source, &workflow); err != nil {
		return err
	}
	job, found := workflow.Jobs["production-owner-registration"]
	if !found || job.Needs != nil || job.If != "" {
		return fmt.Errorf("production owner CI must be registered independently of aggregate jobs")
	}
	selected, executed := false, false
	for _, step := range job.Steps {
		if step.ID == "selection" && step.If == "" {
			selected = strings.Contains(step.Run, "bash scripts/ci/select-gate-by-paths.sh")
			for _, pattern := range []string{"Makefile", ".github/workflows/platform-ci.yml", "scripts/ci/*", "common/*", "*/authorization/*", "*/backend/*"} {
				selected = selected && strings.Contains(step.Run, "'"+pattern+"'")
			}
		}
		if strings.TrimSpace(step.Run) == "make test-production-owner-registration" && step.If == "steps.selection.outputs.run == 'true'" {
			executed = true
		}
	}
	if !selected || !executed {
		return fmt.Errorf("production owner CI is missing its path coverage or standard gate execution")
	}
	source, err = os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		return err
	}
	recipes := map[string]string{}
	current := ""
	for _, line := range strings.Split(string(source), "\n") {
		if strings.HasPrefix(line, "\t") {
			recipes[current] += line + "\n"
			continue
		}
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		current = ""
		if name, _, ok := strings.Cut(line, ":"); ok {
			current = name
		}
	}
	gate := recipes["test-production-owner-registration"]
	for _, command := range []string{
		"cd common && GOWORK=off go test ./production ./client ./authorization/... -count=1",
		"go run ./authorization/cmd/manifest --check-production-owners --repository-root ..",
		"cd system/backend && GOWORK=off go test ./internal/api",
		"cd meta/backend && GOWORK=off go test ./cmd/server ./cmd/worker ./internal/service",
	} {
		if !strings.Contains(gate, command) {
			return fmt.Errorf("production owner standard gate is missing %s", command)
		}
	}
	if !strings.Contains(recipes["test-authorization"], "$(MAKE) test-production-owner-registration") {
		return fmt.Errorf("authorization gate must reuse production owner standard gate")
	}
	return nil
}
