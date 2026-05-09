package dataitem

import "fmt"

// MatchBuiltinSingleResourceRule 返回内置 single resource 格式声明。
func MatchBuiltinSingleResourceRule(formatName string) (FormatRule, bool) {
	for _, rule := range BuiltinSingleResourceRules() {
		if rule.Format == formatName {
			return rule, true
		}
	}
	return FormatRule{}, false
}

// BuiltinSingleResourceRules 返回 common/dataitem 内置的 single resource 和容器格式声明。
func BuiltinSingleResourceRules() []FormatRule {
	return []FormatRule{
		singleResourceRule("csv", DataTypeTable, "table", []string{".csv"}),
		singleResourceRule("tsv", DataTypeTable, "table", []string{".tsv"}),
		singleResourceRule("json", DataTypeDocument, "file", []string{".json"}),
		singleResourceRule("excel", DataTypeContainer, "file", []string{".xls", ".xlsx"}),
		singleResourceRule("parquet", DataTypeTable, "table", []string{".parquet"}),
		singleResourceRule("orc", DataTypeTable, "table", []string{".orc"}),
		singleResourceRule("avro", DataTypeTable, "table", []string{".avro"}),
		singleResourceRule("pdf", DataTypeDocument, "file", []string{".pdf"}),
		singleResourceRule("jpeg", DataTypeMedia, "file", []string{".jpg", ".jpeg"}),
		singleResourceRule("png", DataTypeMedia, "file", []string{".png"}),
		singleResourceRule("gif", DataTypeMedia, "file", []string{".gif"}),
		singleResourceRule("tiff", DataTypeMedia, "file", []string{".tif", ".tiff"}),
		containerResourceRule("sqlite", DataTypeContainer, "file", []string{".sqlite", ".sqlite3", ".db"}),
		containerResourceRule("geopackage", DataTypeContainer, "file", []string{".gpkg"}),
	}
}

func singleResourceRule(format string, family DataType, itemType string, exts []string) FormatRule {
	return FormatRule{
		Format:       format,
		DataType:     family,
		ItemType:     itemType,
		Organization: OrganizationSingle,
		Priority:     10,
		Entry:        EntryRule{Extensions: exts},
	}
}

func containerResourceRule(format string, family DataType, itemType string, exts []string) FormatRule {
	return FormatRule{
		Format:       format,
		DataType:     family,
		ItemType:     itemType,
		Organization: OrganizationSingle,
		Priority:     20,
		Entry:        EntryRule{Extensions: exts},
		Container:    &ContainerRule{ExpandInternalItems: false},
	}
}

// ValidateFormatRule 校验格式声明是否符合组合形态的条件约束。
func ValidateFormatRule(rule FormatRule) error {
	if rule.Format == "" {
		return fmt.Errorf("format rule requires Format")
	}
	if rule.ItemType == "" {
		return fmt.Errorf("format rule %s requires ItemType", rule.Format)
	}
	if rule.DataType == "" {
		return fmt.Errorf("format rule %s requires DataType", rule.Format)
	}
	if rule.Organization == "" {
		return fmt.Errorf("format rule %s requires Organization", rule.Format)
	}
	if len(rule.Entry.Extensions) == 0 && len(rule.Entry.MIMETypes) == 0 && rule.Organization != OrganizationWhole {
		return fmt.Errorf("format rule %s requires Entry", rule.Format)
	}

	switch rule.Organization {
	case OrganizationSingle:
		if rule.Components != nil || rule.WholeScope != nil {
			return fmt.Errorf("single rule %s must not declare Components or WholeScope", rule.Format)
		}
	case OrganizationMulti:
		if rule.Components == nil {
			return fmt.Errorf("multi rule %s requires Components", rule.Format)
		}
		if len(rule.Components.RequiredExtensions) == 0 {
			return fmt.Errorf("multi rule %s requires RequiredExtensions", rule.Format)
		}
		if rule.Components.EntryExtension == "" {
			return fmt.Errorf("multi rule %s requires EntryExtension", rule.Format)
		}
		if rule.Container != nil || rule.WholeScope != nil {
			return fmt.Errorf("multi rule %s must not declare Container or WholeScope", rule.Format)
		}
	case OrganizationWhole:
		if rule.WholeScope == nil {
			return fmt.Errorf("whole rule %s requires WholeScope", rule.Format)
		}
		if rule.Components != nil || rule.Container != nil {
			return fmt.Errorf("whole rule %s must not declare Components or Container", rule.Format)
		}
	default:
		return fmt.Errorf("format rule %s has unsupported Organization %q", rule.Format, rule.Organization)
	}
	return nil
}
