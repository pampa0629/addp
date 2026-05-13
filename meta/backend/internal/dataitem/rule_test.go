package dataitem

import (
	"testing"

	"github.com/addp/common/format"
)

func TestValidateFormatRuleAcceptsMultiFileRule(t *testing.T) {
	rule := FormatRule{
		Format:       "shapefile",
		DataType:     DataTypeTable,
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
	seenDOCX := false
	seenMarkdown := false
	seenParquet := false
	seenPDF := false
	seenPPTX := false
	seenText := false
	seenWPS := false
	seenZIP := false
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
		if rule.Format == string(format.FormatDOCX) &&
			rule.DataType == DataTypeDocument &&
			rule.Organization == OrganizationSingle {
			seenDOCX = true
		}
		if rule.Format == string(format.FormatMarkdown) &&
			rule.DataType == DataTypeDocument &&
			rule.Organization == OrganizationSingle {
			seenMarkdown = true
		}
		if rule.Format == "parquet" && rule.DataType == DataTypeTable && rule.Organization == OrganizationSingle {
			seenParquet = true
		}
		if rule.Format == string(format.FormatPDF) &&
			rule.DataType == DataTypeDocument &&
			rule.Organization == OrganizationSingle {
			seenPDF = true
		}
		if rule.Format == string(format.FormatPPTX) &&
			rule.DataType == DataTypeDocument &&
			rule.Organization == OrganizationSingle {
			seenPPTX = true
		}
		if rule.Format == string(format.FormatText) &&
			rule.DataType == DataTypeDocument &&
			rule.Organization == OrganizationSingle {
			seenText = true
		}
		if rule.Format == string(format.FormatWPS) &&
			rule.DataType == DataTypeDocument &&
			rule.Organization == OrganizationSingle {
			seenWPS = true
		}
		if rule.Format == string(format.FormatZIP) &&
			rule.DataType == DataTypeContainer &&
			rule.Organization == OrganizationSingle {
			seenZIP = true
		}
	}
	if !seenCSV || !seenGeoPackage || !seenDOCX || !seenMarkdown || !seenParquet || !seenPDF || !seenPPTX || !seenText || !seenWPS || !seenZIP {
		t.Fatalf("builtin rules missing csv=%v geopackage=%v docx=%v markdown=%v parquet=%v pdf=%v pptx=%v text=%v wps=%v zip=%v", seenCSV, seenGeoPackage, seenDOCX, seenMarkdown, seenParquet, seenPDF, seenPPTX, seenText, seenWPS, seenZIP)
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
}

func TestMatchBuiltinSingleResourceRuleDerivesRawRangeDocumentsFromFormatCapability(t *testing.T) {
	for _, formatType := range []format.FormatType{format.FormatPDF, format.FormatDOCX, format.FormatPPTX, format.FormatWPS} {
		t.Run(string(formatType), func(t *testing.T) {
			rule, ok := MatchBuiltinSingleResourceRule(string(formatType))
			if !ok {
				t.Fatalf("expected %s rule to derive from format capability", formatType)
			}
			if rule.Organization != OrganizationSingle {
				t.Fatalf("Organization = %q, want single", rule.Organization)
			}
			if rule.DataType != DataTypeDocument {
				t.Fatalf("DataType = %q, want document", rule.DataType)
			}
		})
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
}

func TestMatchBuiltinSingleResourceRuleDoesNotDeriveUnknownFormat(t *testing.T) {
	if rule, ok := MatchBuiltinSingleResourceRule("not-a-real-format"); ok {
		t.Fatalf("unexpected rule: %#v", rule)
	}
}
