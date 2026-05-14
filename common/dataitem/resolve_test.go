package dataitem

import (
	"testing"

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
