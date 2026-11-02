package engineaccess

import (
	"errors"
	"testing"

	commonapi "github.com/addp/common/api"
	shared "github.com/addp/common/authorization"
	"github.com/addp/common/engine/plugin"
	"github.com/google/uuid"
)

func TestPersistentNodeGrantHasOneExplicitTargetVariant(t *testing.T) {
	recursive := false
	input := CreateIndependentGrantInput{Actor: Actor{TenantID: 1, PrincipalID: 2, MembershipID: 3, AuthorizationVersion: 4}, EngineID: 8,
		RequestID: uuid.New(), CatalogPath: plugin.TabularNamespacePath(8, "schema", "a.b/ c"), NodeRecursive: &recursive,
		RecipientType: "user", RecipientID: 7, Action: shared.SourceAccessRead, ExpiryMode: shared.SharingExpiryUntilRevoked, Reason: "Explicit node scope"}
	grant, err := prepareIndependentGrant(input)
	if err != nil || grant.NodeRecursive == nil || *grant.NodeRecursive || grant.ItemFingerprint != "" || grant.BoundaryHash != "" {
		t.Fatalf("node variant: %+v %v", grant, err)
	}
	recursive = true
	if *grant.NodeRecursive {
		t.Fatal("request mutation enlarged persisted scope")
	}
	for _, mutation := range []func(*CreateIndependentGrantInput){
		func(i *CreateIndependentGrantInput) { i.NodeRecursive = nil },
		func(i *CreateIndependentGrantInput) { i.ItemFingerprint = "fingerprint" },
		func(i *CreateIndependentGrantInput) { i.ExpectedBoundaryHash = "digest" },
		func(i *CreateIndependentGrantInput) {
			i.CatalogPath = plugin.TabularItemPath(8, "schema", "public", "table")
		},
		func(i *CreateIndependentGrantInput) { i.CatalogPath = plugin.TabularNamespacePath(8, "database", "db") },
	} {
		bad := input
		mutation(&bad)
		if _, err := prepareIndependentGrant(bad); !errors.Is(err, commonapi.ErrBadRequest) {
			t.Fatalf("ambiguous target accepted: %+v %v", bad, err)
		}
	}
	root := input
	root.CatalogPath = plugin.EngineCatalogRootPath(plugin.TabularCatalogModel("schema"), 8)
	if _, err := prepareIndependentGrant(root); err != nil {
		t.Fatalf("canonical engine root: %v", err)
	}
	changed := *grant
	changed.NodeRecursive = &recursive
	if sameIndependentGrant(grant, &changed) {
		t.Fatal("recursive expansion treated as same command")
	}
}
