package format

import (
	"reflect"
	"testing"

	"github.com/addp/common/contentio"
)

func TestSameBasenameRelatedRefs(t *testing.T) {
	got := SameBasenameRelatedRefs("datasets/roads/roads.shp", []RelatedRefSpec{
		{Extension: ".shp", Role: contentio.RoleMain, Required: true, Primary: true},
		{Extension: "dbf", Required: true},
	})
	want := []RelatedRef{
		NewRelatedRef(contentio.NewRef("datasets/roads/roads.shp", contentio.RoleMain), true, true),
		NewRelatedRef(contentio.NewRef("datasets/roads/roads.dbf", "dbf"), true, false),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SameBasenameRelatedRefs() = %#v, want %#v", got, want)
	}
}

func TestSameBasenameRelatedRefsIncludesPlannedOptionalRefs(t *testing.T) {
	got := SameBasenameRelatedRefs("datasets/roads/roads.shp", []RelatedRefSpec{
		{Extension: ".shp", Role: contentio.RoleMain, Required: true, Primary: true},
		{Extension: ".prj", Role: "projection", Required: false},
	})
	want := []RelatedRef{
		NewRelatedRef(contentio.NewRef("datasets/roads/roads.shp", contentio.RoleMain), true, true),
		NewRelatedRef(contentio.NewRef("datasets/roads/roads.prj", "projection"), false, false),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SameBasenameRelatedRefs() = %#v, want planned refs %#v", got, want)
	}
}

func TestValidateRelatedRefSpecs(t *testing.T) {
	specs := []RelatedRefSpec{
		{Extension: ".shp", Role: contentio.RoleMain, Required: true, Primary: true},
		{Extension: ".dbf", Role: "attributes", Required: true},
	}
	if err := ValidateRelatedRefSpecs(specs); err != nil {
		t.Fatalf("ValidateRelatedRefSpecs() error = %v", err)
	}
}

func TestValidateRelatedRefSpecsRejectsMissingPrimary(t *testing.T) {
	specs := []RelatedRefSpec{
		{Extension: ".shp", Role: contentio.RoleMain, Required: true},
		{Extension: ".dbf", Role: "attributes", Required: true},
	}
	if err := ValidateRelatedRefSpecs(specs); err == nil {
		t.Fatal("ValidateRelatedRefSpecs() succeeded without primary spec")
	}
}

func TestValidateRelatedRefSpecsRejectsMultiplePrimary(t *testing.T) {
	specs := []RelatedRefSpec{
		{Extension: ".shp", Role: contentio.RoleMain, Required: true, Primary: true},
		{Extension: ".dbf", Role: "attributes", Required: true, Primary: true},
	}
	if err := ValidateRelatedRefSpecs(specs); err == nil {
		t.Fatal("ValidateRelatedRefSpecs() succeeded with multiple primary specs")
	}
}

func TestPrimaryRelatedRef(t *testing.T) {
	refs := []RelatedRef{
		NewRelatedRef(contentio.NewRef("datasets/roads/roads.dbf", "attributes"), true, false),
		NewRelatedRef(contentio.NewRef("datasets/roads/roads.shp", contentio.RoleMain), true, true),
	}
	got, err := PrimaryRelatedRef(refs)
	if err != nil {
		t.Fatalf("PrimaryRelatedRef() error = %v", err)
	}
	if got.Ref.Path != "datasets/roads/roads.shp" {
		t.Fatalf("PrimaryRelatedRef() = %#v, want roads.shp", got)
	}
}

func TestValidateRelatedRefsRejectsMissingPrimary(t *testing.T) {
	refs := []RelatedRef{
		NewRelatedRef(contentio.NewRef("datasets/roads/roads.dbf", "attributes"), true, false),
	}
	if err := ValidateRelatedRefs(refs); err == nil {
		t.Fatal("ValidateRelatedRefs() succeeded without primary ref")
	}
}

func TestValidateRelatedRefsRejectsMultiplePrimary(t *testing.T) {
	refs := []RelatedRef{
		NewRelatedRef(contentio.NewRef("datasets/roads/roads.shp", contentio.RoleMain), true, true),
		NewRelatedRef(contentio.NewRef("datasets/roads/roads.dbf", "attributes"), true, true),
	}
	if err := ValidateRelatedRefs(refs); err == nil {
		t.Fatal("ValidateRelatedRefs() succeeded with multiple primary refs")
	}
}

func TestDAETextureURIIsDecodedOnceAndRemainsLocal(t *testing.T) {
	for raw, want := range map[string]string{"textures/a%20b.PNG": "textures/a b.PNG", "a%2520b.jpg": "a%20b.jpg", "a+b.jpeg": "a+b.jpeg", "a.PNG": "a.PNG"} {
		actual, err := DecodeDAETextureRef(raw)
		if err != nil || actual != want {
			t.Fatal(raw, actual, err)
		}
	}
	for _, raw := range []string{"", "%", "a%2.png", "https://host/a.png", "//host/a.png", "/a.png", "../a.png", "%2e%2e/a.png", "a.png?x=1", "a.png#id", "a%3F.png", "a%00.png", "a.bmp"} {
		if actual, err := DecodeDAETextureRef(raw); err == nil {
			t.Fatal("unsafe DAE reference accepted", raw, actual)
		}
	}
}
