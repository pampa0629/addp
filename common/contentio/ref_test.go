package contentio

import "testing"

func TestNewRefNormalizesDefaults(t *testing.T) {
	ref := NewRef("/datasets/roads.shp/", " Main ")
	if ref.Path != "datasets/roads.shp" {
		t.Fatalf("Path = %q, want datasets/roads.shp", ref.Path)
	}
	if ref.Role != RoleMain {
		t.Fatalf("Role = %q, want %q", ref.Role, RoleMain)
	}
}

func TestNewRefNormalizesNonMainRole(t *testing.T) {
	ref := NewRef("datasets/roads.dbf", " Attributes ")
	if ref.Role != "attributes" {
		t.Fatalf("Role = %q, want attributes", ref.Role)
	}
}

func TestBaseNameReturnsPathBasename(t *testing.T) {
	if got := BaseName(NewRef("/datasets/roads.shp/", RoleMain)); got != "roads.shp" {
		t.Fatalf("BaseName() = %q, want roads.shp", got)
	}
}
