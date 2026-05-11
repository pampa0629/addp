package dataitem

import (
	"testing"

	"github.com/addp/common/format"
)

func TestValidateFormatRuleAcceptsMultiFileRule(t *testing.T) {
	rule := FormatRule{
		Format:       "shapefile",
		DataType:     DataTypeTable,
		ItemType:     "table",
		Organization: OrganizationMulti,
		Entry:        EntryRule{Extensions: []string{".shp"}},
		Components: &ComponentRule{
			RequiredExtensions: []string{".shp", ".shx", ".dbf"},
			EntryExtension:     ".shp",
		},
	}

	if err := ValidateFormatRule(rule); err != nil {
		t.Fatalf("ValidateFormatRule() error = %v", err)
	}
}

func TestValidateFormatRuleRejectsSingleResourceComponents(t *testing.T) {
	rule := FormatRule{
		Format:       "csv",
		DataType:     DataTypeTable,
		ItemType:     "table",
		Organization: OrganizationSingle,
		Entry:        EntryRule{Extensions: []string{".csv"}},
		Components:   &ComponentRule{},
	}

	if err := ValidateFormatRule(rule); err == nil {
		t.Fatal("expected single rule with Components to be rejected")
	}
}

func TestValidateFormatRuleRejectsWholeWithoutRule(t *testing.T) {
	rule := FormatRule{
		Format:       "parquet",
		DataType:     DataTypeTable,
		ItemType:     "table",
		Organization: OrganizationWhole,
	}

	if err := ValidateFormatRule(rule); err == nil {
		t.Fatal("expected whole rule without WholeScope to be rejected")
	}
}

func TestBuiltinSingleResourceRulesAreValid(t *testing.T) {
	rules := BuiltinSingleResourceRules()
	if len(rules) == 0 {
		t.Fatal("BuiltinSingleResourceRules() returned no rules")
	}

	seenCSV := false
	seenGeoPackage := false
	seenMarkdown := false
	seenParquet := false
	seenText := false
	for _, rule := range rules {
		if err := ValidateFormatRule(rule); err != nil {
			t.Fatalf("ValidateFormatRule(%s) error = %v", rule.Format, err)
		}
		if rule.Format == "csv" && rule.Organization == OrganizationSingle {
			seenCSV = true
		}
		if rule.Format == "geopackage" && rule.Organization == OrganizationSingle {
			seenGeoPackage = true
		}
		if rule.Format == string(format.FormatMarkdown) &&
			rule.DataType == DataTypeDocument &&
			rule.ItemType == "file" &&
			rule.Organization == OrganizationSingle {
			seenMarkdown = true
		}
		if rule.Format == "parquet" && rule.ItemType == "table" && rule.Organization == OrganizationSingle {
			seenParquet = true
		}
		if rule.Format == string(format.FormatText) &&
			rule.DataType == DataTypeDocument &&
			rule.ItemType == "file" &&
			rule.Organization == OrganizationSingle {
			seenText = true
		}
	}
	if !seenCSV || !seenGeoPackage || !seenMarkdown || !seenParquet || !seenText {
		t.Fatalf("builtin rules missing csv=%v geopackage=%v markdown=%v parquet=%v text=%v", seenCSV, seenGeoPackage, seenMarkdown, seenParquet, seenText)
	}
}

func TestMatchBuiltinSingleResourceRule(t *testing.T) {
	rule, ok := MatchBuiltinSingleResourceRule("geopackage")
	if !ok {
		t.Fatal("expected geopackage rule to match")
	}
	if rule.Organization != OrganizationSingle {
		t.Fatalf("Organization = %q, want single", rule.Organization)
	}
	if rule.DataType != DataTypeContainer {
		t.Fatalf("DataType = %q, want container", rule.DataType)
	}
}

func TestMatchBuiltinSingleResourceRuleDerivesMarkdownFromFormatCapability(t *testing.T) {
	rule, ok := MatchBuiltinSingleResourceRule(string(format.FormatMarkdown))
	if !ok {
		t.Fatal("expected markdown rule to derive from format capability")
	}
	if rule.Organization != OrganizationSingle {
		t.Fatalf("Organization = %q, want single", rule.Organization)
	}
	if rule.DataType != DataTypeDocument {
		t.Fatalf("DataType = %q, want document", rule.DataType)
	}
	if rule.ItemType != "file" {
		t.Fatalf("ItemType = %q, want file", rule.ItemType)
	}
}

func TestMatchBuiltinSingleResourceRuleDerivesTextFromFormatCapability(t *testing.T) {
	rule, ok := MatchBuiltinSingleResourceRule(string(format.FormatText))
	if !ok {
		t.Fatal("expected text rule to derive from format capability")
	}
	if rule.Organization != OrganizationSingle {
		t.Fatalf("Organization = %q, want single", rule.Organization)
	}
	if rule.DataType != DataTypeDocument {
		t.Fatalf("DataType = %q, want document", rule.DataType)
	}
	if rule.ItemType != "file" {
		t.Fatalf("ItemType = %q, want file", rule.ItemType)
	}
}

func TestMatchBuiltinSingleResourceRuleDoesNotDeriveUnknownFormat(t *testing.T) {
	if rule, ok := MatchBuiltinSingleResourceRule("not-a-real-format"); ok {
		t.Fatalf("unexpected rule: %#v", rule)
	}
}
