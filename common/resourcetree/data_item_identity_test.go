package resourcetree

import (
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
)

func TestDataItemIdentityFromCatalogPathUsesMetaFingerprintRules(t *testing.T) {
	tests := []struct {
		name     string
		model    plugin.EngineCatalogModelSpec
		path     plugin.EngineCatalogPath
		fullName string
		itemType string
	}{
		{
			name: "table", model: plugin.TabularCatalogModel(plugin.EngineCatalogTermSchema),
			path:     plugin.TabularItemPath(7, plugin.EngineCatalogTermSchema, "public", "persons"),
			fullName: "public.persons", itemType: "table",
		},
		{
			name: "collection", model: plugin.DynamicSchemaCatalogModel(),
			path:     plugin.EngineCatalogBranchLeafPath(plugin.DynamicSchemaCatalogModel(), 8, plugin.EngineCatalogTermDatabase, "Outdoor", plugin.EngineCatalogTermCollection, plugin.EngineCatalogKindCollection, "Persons"),
			fullName: "Outdoor.Persons", itemType: "collection",
		},
		{
			name: "object", model: plugin.ObjectCatalogModel(),
			path:     plugin.ObjectItemPath(9, "uploads", "2026/persons.csv"),
			fullName: "uploads/2026/persons.csv", itemType: "object",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			identity, err := DataItemIdentityFromCatalogPath(test.model, test.path)
			if err != nil {
				t.Fatal(err)
			}
			if identity.FullName != test.fullName || identity.ItemType != test.itemType {
				t.Fatalf("identity = %#v", identity)
			}
			if identity.Fingerprint != models.GenerateItemFingerprint(test.path.EngineID, test.fullName) {
				t.Fatalf("fingerprint = %q", identity.Fingerprint)
			}
		})
	}
}

func TestDataItemIdentityFromCatalogPathRejectsBranchAndWrongModel(t *testing.T) {
	branch := plugin.TabularNamespacePath(7, plugin.EngineCatalogTermSchema, "public")
	if _, err := DataItemIdentityFromCatalogPath(plugin.TabularCatalogModel(plugin.EngineCatalogTermSchema), branch); err == nil {
		t.Fatal("branch path was accepted as a DataItem")
	}
	leaf := plugin.TabularItemPath(7, plugin.EngineCatalogTermSchema, "public", "persons")
	if _, err := DataItemIdentityFromCatalogPath(plugin.DynamicSchemaCatalogModel(), leaf); err == nil {
		t.Fatal("table path was accepted by dynamic collection model")
	}
	leaf.Segments[len(leaf.Segments)-1].Kind = plugin.EngineCatalogKindCollection
	if _, err := DataItemIdentityFromCatalogPath(plugin.TabularCatalogModel(plugin.EngineCatalogTermSchema), leaf); err == nil {
		t.Fatal("leaf with a mismatched kind was accepted")
	}
}
