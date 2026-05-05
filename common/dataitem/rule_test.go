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
