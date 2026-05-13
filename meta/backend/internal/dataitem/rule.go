package dataitem

import (
	"fmt"
	"sort"

	commonformat "github.com/addp/common/format"
)

// MatchBuiltinSingleResourceRule 返回内置 single resource 格式声明。
func MatchBuiltinSingleResourceRule(formatName string) (FormatRule, bool) {
	for _, rule := range explicitSingleResourceRules() {
		if rule.Format == formatName {
			return rule, true
		}
	}
	return singleResourceRuleFromCapability(formatName)
}

// BuiltinSingleResourceRules 返回 Meta 可识别的 single resource 规则。
//
// 规则来源分两类：
//  1. Meta 显式规则：表达当前仍需要 Meta 明确保留的 data item 语义或历史补充。
//  2. common/format capability：表达普通 single 格式的默认 data_type。
//
// 新增普通格式时，应优先在 common/format 声明 capability；Meta 只在组织方式、
// 组件、whole scope、容器 children 或内容结构判断需要特殊处理时新增显式规则。
func BuiltinSingleResourceRules() []FormatRule {
	rules := explicitSingleResourceRules()
	seen := make(map[string]struct{}, len(rules))
	for _, rule := range rules {
		seen[rule.Format] = struct{}{}
	}
	for _, capability := range commonformat.ListFormatCapabilities() {
		if _, exists := seen[string(capability.Format)]; exists {
			continue
		}
		rule, ok := singleResourceRuleFromCapability(string(capability.Format))
		if !ok {
			continue
		}
		rules = append(rules, rule)
		seen[rule.Format] = struct{}{}
	}
	sort.SliceStable(rules, func(i, j int) bool {
		if rules[i].Priority != rules[j].Priority {
			return rules[i].Priority > rules[j].Priority
		}
		return rules[i].Format < rules[j].Format
	})
	return rules
}

func explicitSingleResourceRules() []FormatRule {
	return []FormatRule{
		singleResourceRule("csv", DataTypeTable, []string{".csv"}),
		singleResourceRule("tsv", DataTypeTable, []string{".tsv"}),
		singleResourceRule("json", DataTypeDocument, []string{".json"}),
		singleResourceRule("excel", DataTypeContainer, []string{".xls", ".xlsx"}),
		singleResourceRule("parquet", DataTypeTable, []string{".parquet"}),
		singleResourceRule("orc", DataTypeTable, []string{".orc"}),
		singleResourceRule("avro", DataTypeTable, []string{".avro"}),
		containerResourceRule("sqlite", DataTypeContainer, []string{".sqlite", ".sqlite3", ".db"}),
		containerResourceRule("geopackage", DataTypeContainer, []string{".gpkg"}),
	}
}

func singleResourceRuleFromCapability(formatName string) (FormatRule, bool) {
	capability, ok := commonformat.GetFormatCapability(commonformat.FormatType(formatName))
	if !ok || !containsString(capability.Layouts, string(OrganizationSingle)) {
		return FormatRule{}, false
	}
	if len(capability.Extensions) == 0 {
		return FormatRule{}, false
	}
	dataType := dataTypeFromFormatCapability(capability.DataType)
	if dataType == "" {
		return FormatRule{}, false
	}
	return singleResourceRule(
		string(capability.Format),
		dataType,
		capability.Extensions,
	), true
}

func singleResourceRule(format string, family DataType, exts []string) FormatRule {
	return FormatRule{
		Format:       format,
		DataType:     family,
		Organization: OrganizationSingle,
		Priority:     10,
		Entry:        EntryRule{Extensions: exts},
	}
}

func containerResourceRule(format string, family DataType, exts []string) FormatRule {
	return FormatRule{
		Format:       format,
		DataType:     family,
		Organization: OrganizationSingle,
		Priority:     20,
		Entry:        EntryRule{Extensions: exts},
		Container:    &ContainerRule{ExpandInternalItems: false},
	}
}

// ValidateFormatRule 校验格式声明是否符合组织方式的条件约束。
func ValidateFormatRule(rule FormatRule) error {
	if rule.Format == "" {
		return fmt.Errorf("format rule requires Format")
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

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
