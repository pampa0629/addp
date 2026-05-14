package dataitem

import (
	"testing"

	"github.com/addp/common/format"
	_ "github.com/addp/common/format/builtin"
)

func TestResolveItemsGroupsShapefileComponents(t *testing.T) {
	t.Parallel()

	size := int64(10)
	result, err := ResolveItems(ResolveInput{
		ScopeKind: ScopeKindContainer,
		Candidates: []Candidate{
			{Path: "roads.shp", Name: "roads.shp", SizeBytes: &size},
			{Path: "roads.shx", Name: "roads.shx", SizeBytes: &size},
			{Path: "roads.dbf", Name: "roads.dbf", SizeBytes: &size},
			{Path: "roads.prj", Name: "roads.prj", SizeBytes: &size},
			{Path: "readme.md", Name: "readme.md", SizeBytes: &size},
		},
	})
	if err != nil {
		t.Fatalf("ResolveItems() error = %v", err)
	}
	if len(result.Items) != 2 {
		t.Fatalf("items = %#v, want shapefile + markdown", result.Items)
	}
	item := result.Items[0]
	if item.Organization != OrganizationMulti || item.Format != "shapefile" || item.EntryPath != "roads.shp" {
		t.Fatalf("first item = %#v, want multi shapefile", item)
	}
	if len(item.ComponentList) != 4 {
		t.Fatalf("components = %#v, want 4", item.ComponentList)
	}
	if !result.Claims["roads.shp"] || !result.Claims["roads.shx"] || !result.Claims["roads.dbf"] {
		t.Fatalf("claims = %#v, want shapefile components claimed", result.Claims)
	}
	if result.Items[1].Organization != OrganizationSingle || result.Items[1].DataType != DataTypeDocument {
		t.Fatalf("second item = %#v, want document single", result.Items[1])
	}
}

func TestResolveItemsIgnoresSystemEntries(t *testing.T) {
	t.Parallel()

	result, err := ResolveItems(ResolveInput{
		ScopeKind: ScopeKindContainer,
		Candidates: []Candidate{
			{Path: "__MACOSX/._data.csv", Name: "._data.csv"},
			{Path: ".DS_Store", Name: ".DS_Store"},
			{Path: "data.csv", Name: "data.csv"},
		},
		Options: ResolveOptions{IncludeIgnored: true},
	})
	if err != nil {
		t.Fatalf("ResolveItems() error = %v", err)
	}
	if len(result.Ignored) != 2 {
		t.Fatalf("ignored = %#v, want macOS entries ignored", result.Ignored)
	}
	if len(result.Items) != 1 || result.Items[0].Format != "csv" {
		t.Fatalf("items = %#v, want one csv item", result.Items)
	}
}

func TestResolveItemsDetectsWholeScopePartitionedTable(t *testing.T) {
	t.Parallel()

	size := int64(10)
	result, err := ResolveItems(ResolveInput{
		ScopeKind: ScopeKindDirectory,
		ScopePath: "dataset",
		Candidates: []Candidate{
			{Path: "dataset/dt=2026-05-05/part-000.parquet", Name: "part-000.parquet", SizeBytes: &size},
			{Path: "dataset/dt=2026-05-06/part-001.parquet", Name: "part-001.parquet", SizeBytes: &size},
		},
		Options: ResolveOptions{AllowWholeScope: true},
	})
	if err != nil {
		t.Fatalf("ResolveItems() error = %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("items = %#v, want one whole scope item", result.Items)
	}
	item := result.Items[0]
	if item.Organization != OrganizationWhole || item.Format != string(format.FormatParquet) || item.EntryPath != "dataset" {
		t.Fatalf("item = %#v, want parquet whole scope", item)
	}
	if !result.Exclusive {
		t.Fatal("whole scope table should be exclusive")
	}
	if !result.Claims["dataset/dt=2026-05-05/part-000.parquet"] || !result.Claims["dataset/dt=2026-05-06/part-001.parquet"] {
		t.Fatalf("claims = %#v, want parquet parts claimed", result.Claims)
	}
}

func TestResolveItemsKeepsSiblingTablesAsSingles(t *testing.T) {
	t.Parallel()

	size := int64(10)
	result, err := ResolveItems(ResolveInput{
		ScopeKind: ScopeKindDirectory,
		ScopePath: "dataset",
		Candidates: []Candidate{
			{Path: "dataset/sales.parquet", Name: "sales.parquet", SizeBytes: &size},
			{Path: "dataset/customers.parquet", Name: "customers.parquet", SizeBytes: &size},
		},
		Options: ResolveOptions{AllowWholeScope: true},
	})
	if err != nil {
		t.Fatalf("ResolveItems() error = %v", err)
	}
	if result.Exclusive {
		t.Fatal("independent sibling tables should not become an exclusive whole scope")
	}
	if len(result.Items) != 2 {
		t.Fatalf("items = %#v, want two single table items", result.Items)
	}
	for _, item := range result.Items {
		if item.Organization != OrganizationSingle {
			t.Fatalf("item = %#v, want single organization", item)
		}
	}
}

func TestResolveItemsDoesNotFoldClaimedMultiComponentsIntoWholeScope(t *testing.T) {
	t.Parallel()

	size := int64(10)
	result, err := ResolveItems(ResolveInput{
		ScopeKind: ScopeKindDirectory,
		ScopePath: "dataset",
		Candidates: []Candidate{
			{Path: "dataset/roads.shp", Name: "roads.shp", SizeBytes: &size},
			{Path: "dataset/roads.shx", Name: "roads.shx", SizeBytes: &size},
			{Path: "dataset/roads.dbf", Name: "roads.dbf", SizeBytes: &size},
			{Path: "dataset/part-000.parquet", Name: "part-000.parquet", SizeBytes: &size},
			{Path: "dataset/part-001.parquet", Name: "part-001.parquet", SizeBytes: &size},
		},
		Options: ResolveOptions{AllowWholeScope: true},
	})
	if err != nil {
		t.Fatalf("ResolveItems() error = %v", err)
	}
	if len(result.Items) != 2 {
		t.Fatalf("items = %#v, want shapefile multi + parquet whole scope", result.Items)
	}
	if result.Items[0].Organization != OrganizationMulti || result.Items[1].Organization != OrganizationWhole {
		t.Fatalf("items = %#v, want multi before whole", result.Items)
	}
	if result.Items[1].SizeBytes == nil || *result.Items[1].SizeBytes != 20 {
		t.Fatalf("whole item size = %#v, want only parquet component sizes", result.Items[1].SizeBytes)
	}
	if !result.Claims["dataset/roads.shp"] || !result.Claims["dataset/part-001.parquet"] {
		t.Fatalf("claims = %#v, want both multi and whole components claimed", result.Claims)
	}
}
