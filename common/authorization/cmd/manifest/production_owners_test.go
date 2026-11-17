package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/addp/common/authorization"
	"github.com/addp/common/client"
	"github.com/addp/common/production"
)

func productionTestRegistry(t *testing.T) *production.Registry {
	t.Helper()
	r, err := client.NewProductionRegistry(client.NewTransferClient("", nil), client.NewDevelopClient("", nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func productionTestCatalog() authorization.AuthorizationCatalogReport {
	c := authorization.AuthorizationCatalogReport{}
	for _, module := range []string{"develop", "transfer"} {
		c.Permissions = append(c.Permissions, authorization.PermissionDescriptor{Key: module + ".production_receipt.read", OwnerModule: module, Status: "active", AllowedScopeTypes: []string{"tenant"}}, authorization.PermissionDescriptor{Key: module + ".task.execute", OwnerModule: module, Status: "active", AllowedScopeTypes: []string{"tenant"}})
	}
	for _, role := range []string{"tenant.system_runtime", "tenant.meta_runtime"} {
		c.Roles = append(c.Roles, authorization.BuiltinRoleDescriptor{Key: role, AllowedScopeTypes: []string{"tenant"}, AllowedPrincipalTypes: []string{"service_principal"}, Permissions: []string{"develop.production_receipt.read", "transfer.production_receipt.read"}})
	}
	return c
}

func TestProductionOwnerCatalogRejectsIncompleteOrExpandedMachineAuthority(t *testing.T) {
	tests := map[string]func(*authorization.AuthorizationCatalogReport){
		"missing receipt":       func(c *authorization.AuthorizationCatalogReport) { c.Permissions = c.Permissions[1:] },
		"disabled receipt":      func(c *authorization.AuthorizationCatalogReport) { c.Permissions[0].Status = "disabled" },
		"foreign receipt owner": func(c *authorization.AuthorizationCatalogReport) { c.Permissions[0].OwnerModule = "transfer" },
		"platform receipt": func(c *authorization.AuthorizationCatalogReport) {
			c.Permissions[0].AllowedScopeTypes = []string{"platform"}
		},
		"customizable receipt": func(c *authorization.AuthorizationCatalogReport) { c.Permissions[0].TenantCustomizable = true },
		"delegable receipt":    func(c *authorization.AuthorizationCatalogReport) { c.Permissions[0].Delegable = true },
		"missing execution": func(c *authorization.AuthorizationCatalogReport) {
			c.Permissions = append(c.Permissions[:1], c.Permissions[2:]...)
		},
		"disabled execution": func(c *authorization.AuthorizationCatalogReport) { c.Permissions[1].Status = "disabled" },
		"foreign execution":  func(c *authorization.AuthorizationCatalogReport) { c.Permissions[1].OwnerModule = "transfer" },
		"platform execution": func(c *authorization.AuthorizationCatalogReport) {
			c.Permissions[1].AllowedScopeTypes = []string{"platform"}
		},
		"missing System reader": func(c *authorization.AuthorizationCatalogReport) { c.Roles[0].Permissions = c.Roles[0].Permissions[1:] },
		"missing Meta reader":   func(c *authorization.AuthorizationCatalogReport) { c.Roles[1].Permissions = c.Roles[1].Permissions[1:] },
		"human template":        func(c *authorization.AuthorizationCatalogReport) { c.Roles[0].AllowedPrincipalTypes = []string{"user"} },
		"wrong scope":           func(c *authorization.AuthorizationCatalogReport) { c.Roles[0].AllowedScopeTypes = []string{"platform"} },
		"extra runtime": func(c *authorization.AuthorizationCatalogReport) {
			c.Roles = append(c.Roles, authorization.BuiltinRoleDescriptor{Key: "tenant.develop_runtime", Permissions: []string{"develop.production_receipt.read"}})
		},
		"unregistered declared producer": func(c *authorization.AuthorizationCatalogReport) {
			c.Permissions = append(c.Permissions, authorization.PermissionDescriptor{Key: "quality.production_receipt.read", OwnerModule: "quality", Status: "active"})
		},
	}
	registry := productionTestRegistry(t)
	names, err := checkProductionOwnerCatalog(productionTestCatalog(), registry)
	if err != nil || !slices.Equal(names, []string{"develop", "transfer"}) {
		t.Fatal(names, err)
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			c := productionTestCatalog()
			mutate(&c)
			if _, err := checkProductionOwnerCatalog(c, registry); err == nil {
				t.Fatal("invalid producer authority passed")
			}
		})
	}
}

func TestProductionOwnerCommandChecksActualRepositoryAndDoesNotContactServices(t *testing.T) {
	var out, errout bytes.Buffer
	if err := run([]string{"--check-production-owners", "--repository-root", testRepositoryRoot(t)}, &out, &errout); err != nil {
		t.Fatal(err, errout.String())
	}
	var result struct {
		Owners []string `json:"owners"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil || !slices.Equal(result.Owners, []string{"develop", "transfer"}) {
		t.Fatal(out.String(), err)
	}
	if err := run([]string{"--check-production-owners", "--check", "--repository-root", testRepositoryRoot(t)}, &out, &errout); err == nil {
		t.Fatal("combined command modes accepted")
	}
}

func TestProductionWiringRejectsConsumerCopiesAndMissingSharedFactory(t *testing.T) {
	for _, tc := range []string{"valid", "duplicate", "missing", "dot-import"} {
		t.Run(tc, func(t *testing.T) {
			root := t.TempDir()
			sources := map[string]string{
				"common/client/production_registry.go":  `package client; import "github.com/addp/common/production"; func factory(){production.NewRegistry()}`,
				"system/backend/internal/api/router.go": `package api; import c "github.com/addp/common/client"; func setup(){c.NewProductionRegistry()}`,
				"meta/backend/cmd/server/main.go":       `package main; import "github.com/addp/common/client"; func main(){client.NewProductionRegistry()}`,
				"meta/backend/cmd/worker/main.go":       `package main; import c "github.com/addp/common/client"; func main(){c.NewProductionRegistry()}`,
			}
			if tc == "duplicate" {
				sources["meta/backend/internal/service/legacy.go"] = `package service; import p "github.com/addp/common/production"; func old(){p.NewRegistry()}`
			}
			if tc == "dot-import" {
				sources["meta/backend/internal/service/legacy.go"] = `package service; import . "github.com/addp/common/production"; func old(){NewRegistry()}`
			}
			if tc == "missing" {
				sources["meta/backend/cmd/worker/main.go"] = `package main;func main(){}`
			}
			for path, source := range sources {
				target := filepath.Join(root, path)
				if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(target, []byte(source), 0600); err != nil {
					t.Fatal(err)
				}
			}
			err := checkProductionOwnerWiring(root, []authorization.PermissionManifestReference{{OwnerModule: "system"}, {OwnerModule: "meta"}})
			if (err == nil) != (tc == "valid") {
				t.Fatal(err)
			}
			if tc == "duplicate" && !strings.Contains(err.Error(), "legacy.go") {
				t.Fatal(err)
			}
		})
	}
}

func TestProductionOwnerCIRejectsMissingOrBlockedRegistration(t *testing.T) {
	root := testRepositoryRoot(t)
	workflow, err := os.ReadFile(filepath.Join(root, ".github/workflows/platform-ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	makefile, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct{ name, file, before, after string }{
		{"missing job", "workflow", "  production-owner-registration:", "  removed-production-owner-registration:"},
		{"upstream dependency", "workflow", "  production-owner-registration:", "  production-owner-registration:\n    needs: platform-tests"},
		{"disabled job", "workflow", "  production-owner-registration:", "  production-owner-registration:\n    if: 'false'"},
		{"removed execution", "workflow", "run: make test-production-owner-registration", "run: echo skipped"},
		{"narrowed manifest selection", "workflow", "'*/authorization/*'", "'develop/authorization/*'"},
		{"missing standard target", "make", "test-production-owner-registration: ##", "removed-production-owner-registration: ##"},
		{"missing runtime test", "make", "@cd meta/backend && GOWORK=off go test ./cmd/server ./cmd/worker ./internal/service", "@echo skipped"},
		{"missing aggregate reuse", "make", "@$(MAKE) test-production-owner-registration", "@echo skipped"},
	}
	if err := checkProductionOwnerCI(root); err != nil {
		t.Fatal(err)
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			temp := t.TempDir()
			w, m := string(workflow), string(makefile)
			if tc.file == "workflow" {
				w = strings.Replace(w, tc.before, tc.after, 1)
			} else {
				m = strings.Replace(m, tc.before, tc.after, 1)
			}
			if w == string(workflow) && m == string(makefile) {
				t.Fatal("fixture mutation did not apply")
			}
			if err := os.MkdirAll(filepath.Join(temp, ".github/workflows"), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(temp, ".github/workflows/platform-ci.yml"), []byte(w), 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(temp, "Makefile"), []byte(m), 0644); err != nil {
				t.Fatal(err)
			}
			if err := checkProductionOwnerCI(temp); err == nil {
				t.Fatal("invalid production owner CI registration passed")
			}
		})
	}
}
