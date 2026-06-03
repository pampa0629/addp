package dataitem

import (
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
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
