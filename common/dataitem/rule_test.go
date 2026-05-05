package dataitem

import "testing"

func TestValidateFormatRuleAcceptsMultiFileRule(t *testing.T) {
	rule := FormatRule{
		Format:          "shapefile",
		DataFamily:      DataFamilyTabular,
		ItemType:        "table",
		CompositionType: CompositionTypeMultiFile,
		Entry:           EntryRule{Extensions: []string{".shp"}},
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
		Format:          "csv",
		DataFamily:      DataFamilyTabular,
		ItemType:        "table",
		CompositionType: CompositionTypeSingleFile,
		Entry:           EntryRule{Extensions: []string{".csv"}},
		Components:      &ComponentRule{},
	}

	if err := ValidateFormatRule(rule); err == nil {
		t.Fatal("expected single_file rule with Components to be rejected")
	}
}

func TestValidateFormatRuleRejectsDirectoryTreeWithoutRule(t *testing.T) {
	rule := FormatRule{
		Format:          "parquet",
		DataFamily:      DataFamilyTabular,
		ItemType:        "lake_table",
		CompositionType: CompositionTypeDirectoryTree,
	}

	if err := ValidateFormatRule(rule); err == nil {
		t.Fatal("expected directory_tree rule without DirectoryTree to be rejected")
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
		if rule.Format == "csv" && rule.CompositionType == CompositionTypeSingleFile {
			seenCSV = true
		}
		if rule.Format == "geopackage" && rule.CompositionType == CompositionTypeContainerFile {
			seenGeoPackage = true
		}
		if rule.Format == "parquet" && rule.ItemType == "lake_table" && rule.CompositionType == CompositionTypeSingleFile {
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
	if rule.CompositionType != CompositionTypeContainerFile {
		t.Fatalf("CompositionType = %q, want container_file", rule.CompositionType)
	}
}
