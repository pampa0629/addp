package dataitem

import "fmt"

// MatchBuiltinSingleFileRule 返回单文件或容器文件匹配的内置格式声明。
func MatchBuiltinSingleFileRule(formatName string) (FormatRule, bool) {
	for _, rule := range BuiltinSingleFileRules() {
		if rule.Format == formatName {
			return rule, true
		}
	}
	return FormatRule{}, false
}

// BuiltinSingleFileRules 返回 common/dataitem 内置的单文件和容器文件格式声明。
func BuiltinSingleFileRules() []FormatRule {
	return []FormatRule{
		singleFileRule("csv", DataFamilyTabular, "table", []string{".csv"}),
		singleFileRule("tsv", DataFamilyTabular, "table", []string{".tsv"}),
		singleFileRule("geojson", DataFamilyTabular, "table", []string{".geojson", ".json"}),
		singleFileRule("excel", DataFamilyTabular, "table", []string{".xls", ".xlsx"}),
		singleFileRule("parquet", DataFamilyTabular, "lake_table", []string{".parquet"}),
		singleFileRule("orc", DataFamilyTabular, "lake_table", []string{".orc"}),
		singleFileRule("avro", DataFamilyTabular, "lake_table", []string{".avro"}),
		singleFileRule("pdf", DataFamilyDocument, "file", []string{".pdf"}),
		singleFileRule("jpeg", DataFamilyImage, "file", []string{".jpg", ".jpeg"}),
		singleFileRule("png", DataFamilyImage, "file", []string{".png"}),
		singleFileRule("gif", DataFamilyImage, "file", []string{".gif"}),
		singleFileRule("tiff", DataFamilyImage, "file", []string{".tif", ".tiff"}),
		containerFileRule("sqlite", DataFamilyTabular, "file", []string{".sqlite", ".sqlite3", ".db"}),
		containerFileRule("geopackage", DataFamilyTabular, "file", []string{".gpkg"}),
	}
}

func singleFileRule(format string, family DataFamily, itemType string, exts []string) FormatRule {
	return FormatRule{
		Format:          format,
		DataFamily:      family,
		ItemType:        itemType,
		CompositionType: CompositionTypeSingleFile,
		Priority:        10,
		Entry:           EntryRule{Extensions: exts},
	}
}

func containerFileRule(format string, family DataFamily, itemType string, exts []string) FormatRule {
	return FormatRule{
		Format:          format,
		DataFamily:      family,
		ItemType:        itemType,
		CompositionType: CompositionTypeContainerFile,
		Priority:        20,
		Entry:           EntryRule{Extensions: exts},
		Container:       &ContainerRule{ExpandInternalItems: false},
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
	if rule.DataFamily == "" {
		return fmt.Errorf("format rule %s requires DataFamily", rule.Format)
	}
	if rule.CompositionType == "" {
		return fmt.Errorf("format rule %s requires CompositionType", rule.Format)
	}
	if len(rule.Entry.Extensions) == 0 && len(rule.Entry.MIMETypes) == 0 && rule.CompositionType != CompositionTypeDirectoryTree {
		return fmt.Errorf("format rule %s requires Entry", rule.Format)
	}

	switch rule.CompositionType {
	case CompositionTypeSingleFile:
		if rule.Components != nil || rule.DirectoryTree != nil || rule.Collection != nil {
			return fmt.Errorf("single_file rule %s must not declare Components, DirectoryTree or Collection", rule.Format)
		}
	case CompositionTypeMultiFile:
		if rule.Components == nil {
			return fmt.Errorf("multi_file rule %s requires Components", rule.Format)
		}
		if len(rule.Components.RequiredExtensions) == 0 {
			return fmt.Errorf("multi_file rule %s requires RequiredExtensions", rule.Format)
		}
		if rule.Components.EntryExtension == "" {
			return fmt.Errorf("multi_file rule %s requires EntryExtension", rule.Format)
		}
		if rule.Container != nil || rule.DirectoryTree != nil {
			return fmt.Errorf("multi_file rule %s must not declare Container or DirectoryTree", rule.Format)
		}
	case CompositionTypeContainerFile:
		if rule.Container == nil {
			return fmt.Errorf("container_file rule %s requires Container", rule.Format)
		}
		if rule.Components != nil || rule.DirectoryTree != nil || rule.Collection != nil {
			return fmt.Errorf("container_file rule %s must not declare Components, DirectoryTree or Collection", rule.Format)
		}
	case CompositionTypeDirectoryTree:
		if rule.DirectoryTree == nil {
			return fmt.Errorf("directory_tree rule %s requires DirectoryTree", rule.Format)
		}
		if rule.Components != nil || rule.Container != nil {
			return fmt.Errorf("directory_tree rule %s must not declare Components or Container", rule.Format)
		}
	case CompositionTypeMixedCollection:
		if rule.Collection == nil {
			return fmt.Errorf("mixed_collection rule %s requires Collection", rule.Format)
		}
	default:
		return fmt.Errorf("format rule %s has unsupported CompositionType %q", rule.Format, rule.CompositionType)
	}
	return nil
}
