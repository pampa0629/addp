package dataitem

import "testing"

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

func TestValidateFormatRuleRejectsSingleFileComponents(t *testing.T) {
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

func TestValidateFormatRuleRejectsDirectoryTreeWithoutRule(t *testing.T) {
	rule := FormatRule{
		Format:       "parquet",
		DataType:     DataTypeTable,
		ItemType:     "lake_table",
		Organization: OrganizationWhole,
	}

	if err := ValidateFormatRule(rule); err == nil {
		t.Fatal("expected whole rule without WholeScope to be rejected")
	}
}

func TestBuiltinSingleFileRulesAreValid(t *testing.T) {
	rules := BuiltinSingleFileRules()
	if len(rules) == 0 {
		t.Fatal("BuiltinSingleFileRules() returned no rules")
	}

	seenCSV := false
	seenGeoPackage := false
	seenParquet := false
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
		if rule.Format == "parquet" && rule.ItemType == "lake_table" && rule.Organization == OrganizationSingle {
			seenParquet = true
		}
	}
	if !seenCSV || !seenGeoPackage || !seenParquet {
		t.Fatalf("builtin rules missing csv=%v geopackage=%v parquet=%v", seenCSV, seenGeoPackage, seenParquet)
	}
}

func TestMatchBuiltinSingleFileRule(t *testing.T) {
	rule, ok := MatchBuiltinSingleFileRule("geopackage")
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
