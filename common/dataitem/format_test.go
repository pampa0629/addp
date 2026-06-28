package dataitem

import (
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	_ "github.com/addp/common/format/builtin"
)

func TestValidateFormatRuleRejectsMissingPrimaryRelatedRefSpec(t *testing.T) {
	rule := FormatRule{
		Format:   "bad_multi",
		DataType: datatype.Table,
		Layout:   format.LayoutMulti,
		Entry:    EntryRule{Extensions: []string{".main"}},
		RelatedRefSpecs: []format.RelatedRefSpec{
			{Extension: ".main", Role: "main", Required: true},
		},
	}

	if err := ValidateFormatRule(rule); err == nil {
		t.Fatal("ValidateFormatRule() succeeded without primary related ref spec")
	}
}

func TestValidateFormatRuleAcceptsSinglePrimaryRelatedRefSpec(t *testing.T) {
	rule := FormatRule{
		Format:   "good_multi",
		DataType: datatype.Table,
		Layout:   format.LayoutMulti,
		Entry:    EntryRule{Extensions: []string{".main"}},
		RelatedRefSpecs: []format.RelatedRefSpec{
			{Extension: ".main", Role: "main", Required: true, Primary: true},
			{Extension: ".side", Role: "side", Required: true},
		},
	}

	if err := ValidateFormatRule(rule); err != nil {
		t.Fatalf("ValidateFormatRule() error = %v", err)
	}
}

func TestInferDataTypeUsesFormatDescriptorForContentType(t *testing.T) {
	if got := InferDataType("", "image/png"); got != datatype.Media {
		t.Fatalf("InferDataType(image/png) = %q, want media", got)
	}
	if got := InferDataType("", "text/plain; charset=utf-8"); got != datatype.Document {
		t.Fatalf("InferDataType(text/plain) = %q, want document", got)
	}
}

func TestInferDataTypeDoesNotClassifyUnknownMIMEPrefix(t *testing.T) {
	if got := InferDataType("", "application/x-custom"); got != datatype.Unknown {
		t.Fatalf("InferDataType(application/x-custom) = %q, want unknown", got)
	}
}

func TestGenericBinaryMIMEDoesNotOverrideOSGBExtension(t *testing.T) {
	candidate := Candidate{
		Name:        "Tile_4_L20_00010t3.osgb",
		Path:        "3d/single-osgb/Tile_4_L20_00010t3.osgb",
		ContentType: "application/octet-stream",
	}
	if got := DetectFormat(candidate); got != string(format.FormatOSGB) {
		t.Fatalf("DetectFormat() = %q, want osgb", got)
	}
	if got := InferFormat(candidate.Name, candidate.ContentType, ""); got != string(format.FormatOSGB) {
		t.Fatalf("InferFormat() = %q, want osgb", got)
	}
	if got := InferDataType(string(format.FormatOSGB), candidate.ContentType); got != datatype.Model3D {
		t.Fatalf("InferDataType(osgb, application/octet-stream) = %q, want model_3d", got)
	}
}

func TestBuiltinMultiRulesIncludesGeoTIFFSidecars(t *testing.T) {
	t.Parallel()

	var tiffRule *FormatRule
	for _, rule := range BuiltinMultiRules() {
		if rule.Format == string(format.FormatTIFF) {
			copied := rule
			tiffRule = &copied
			break
		}
	}
	if tiffRule == nil {
		t.Fatal("BuiltinMultiRules() missing TIFF rule")
	}
	if tiffRule.DataType != datatype.Media || tiffRule.Layout != format.LayoutMulti {
		t.Fatalf("TIFF rule = %#v, want media multi", tiffRule)
	}
	seen := map[string]format.RelatedRefSpec{}
	for _, spec := range tiffRule.RelatedRefSpecs {
		seen[format.NormalizeExtension(spec.Extension)] = spec
	}
	for _, ext := range []string{".tif", ".tfw", ".hdr", ".aux.xml"} {
		if _, ok := seen[ext]; !ok {
			t.Fatalf("TIFF rule refs = %#v, missing %s", tiffRule.RelatedRefSpecs, ext)
		}
	}
}
