package dataitem

import (
	"testing"

	"github.com/addp/common/format"
)

func TestValidateFormatRuleRejectsMissingPrimaryRelatedRefSpec(t *testing.T) {
	rule := FormatRule{
		Format:   "bad_multi",
		DataType: DataTypeTable,
		Layout:   LayoutMulti,
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
		DataType: DataTypeTable,
		Layout:   LayoutMulti,
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
