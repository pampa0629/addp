package plugin

import "testing"

func TestDynamicCollectionCatalogEntryUsesEstimatedRowCount(t *testing.T) {
	estimatedRowCount := int64(42)
	entry := DynamicCollectionCatalogEntry(
		EngineCatalogRootPath(DynamicSchemaCatalogModel(), 7),
		"analytics",
		"events",
		DynamicCollectionFacts{DocumentCount: &estimatedRowCount},
	)

	if entry.Table == nil || entry.Table.EstimatedRowCount == nil || *entry.Table.EstimatedRowCount != 42 {
		t.Fatalf("Table.EstimatedRowCount = %#v, want 42", entry.Table)
	}
	if entry.Table.RowCount != nil {
		t.Fatalf("Table.RowCount = %#v, catalog estimate must not be exact", entry.Table.RowCount)
	}
}

func TestDynamicCollectionCatalogEntryKeepsUnknownRowCountEmpty(t *testing.T) {
	entry := DynamicCollectionCatalogEntry(
		EngineCatalogRootPath(DynamicSchemaCatalogModel(), 7),
		"analytics",
		"events",
		DynamicCollectionFacts{},
	)

	if entry.Table == nil || entry.Table.EstimatedRowCount != nil || entry.Table.RowCount != nil {
		t.Fatalf("Table = %#v, unknown counts must stay empty", entry.Table)
	}
}
