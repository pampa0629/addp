package dataitem

import "fmt"

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
