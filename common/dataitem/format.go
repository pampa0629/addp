package dataitem

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/addp/common/contentio"
	"github.com/addp/common/format"
)

func DetectFormat(candidate Candidate) string {
	if value := strings.TrimSpace(interfaceString(candidate.Properties["format"])); value != "" {
		return normalizeFormat(value)
	}
	if candidate.ContentType != "" {
		if detected := format.MIMEToFormat(candidate.ContentType); detected != format.FormatUnknown {
			return string(detected)
		}
	}
	if detected := format.DetectFormat(candidate.Name, nil); detected != format.FormatUnknown {
		return string(detected)
	}
	if detected := format.DetectFormat(candidate.Path, nil); detected != format.FormatUnknown {
		return string(detected)
	}
	return string(format.FormatUnknown)
}

func DetectDataType(formatName string) DataType {
	capability, ok := format.GetFormatCapability(format.FormatType(formatName))
	if !ok {
		return DataTypeUnknown
	}
	return dataTypeFromString(capability.DataType)
}

func InferFormat(fileName, contentType, explicitFormat string) string {
	if explicitFormat != "" {
		if canonical := normalizeFormat(explicitFormat); canonical != string(format.FormatUnknown) {
			return canonical
		}
	}
	if contentType != "" {
		if detected := format.MIMEToFormat(contentType); detected != format.FormatUnknown {
			return string(detected)
		}
	}
	if detected := format.DetectFormat(fileName, nil); detected != format.FormatUnknown {
		return string(detected)
	}
	return normalizeFormat(filepath.Ext(fileName))
}

func InferDataType(formatName, contentType string) DataType {
	if dataType := DetectDataType(normalizeFormat(formatName)); dataType != DataTypeUnknown {
		return dataType
	}
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	switch {
	case strings.HasPrefix(contentType, "image/"), strings.HasPrefix(contentType, "video/"), strings.HasPrefix(contentType, "audio/"):
		return DataTypeMedia
	case contentType == "application/pdf", strings.HasPrefix(contentType, "text/"):
		return DataTypeDocument
	default:
		return DataTypeUnknown
	}
}

func MatchBuiltinSingleResourceRule(formatName string) (FormatRule, bool) {
	return singleResourceRuleFromCapability(formatName)
}

func BuiltinSingleResourceRules() []FormatRule {
	rules := []FormatRule{}
	seen := map[string]struct{}{}
	for _, capability := range format.ListFormatCapabilities() {
		rule, ok := singleResourceRuleFromCapability(string(capability.Format))
		if !ok {
			continue
		}
		if _, exists := seen[rule.Format]; exists {
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

func singleResourceRuleFromCapability(formatName string) (FormatRule, bool) {
	capability, ok := format.GetFormatCapability(format.FormatType(normalizeFormat(formatName)))
	if !ok || !containsString(capability.Layouts, format.FormatLayoutSingle) || len(capability.Extensions) == 0 {
		return FormatRule{}, false
	}
	dataType := dataTypeFromString(capability.DataType)
	if dataType == DataTypeUnknown && capability.DataType != format.FormatDataTypeFile {
		return FormatRule{}, false
	}
	rule := FormatRule{
		Format:       string(capability.Format),
		DataType:     dataType,
		Organization: OrganizationSingle,
		Priority:     10,
		Entry:        EntryRule{Extensions: append([]string(nil), capability.Extensions...)},
	}
	if dataType == DataTypeContainer {
		rule.Priority = 20
		rule.Container = &ContainerRule{ExpandInternalItems: false}
	}
	return rule, true
}

func BuiltinMultiRules() []FormatRule {
	rules := []FormatRule{}
	for _, capability := range format.ListFormatCapabilities() {
		if !containsString(capability.Layouts, format.FormatLayoutMulti) {
			continue
		}
		specs := RelatedRefSpecs(format.FormatType(capability.Format))
		if len(specs) == 0 {
			continue
		}
		rules = append(rules, FormatRule{
			Format:          string(capability.Format),
			DataType:        dataTypeFromString(capability.DataType),
			Organization:    OrganizationMulti,
			Priority:        100,
			Entry:           EntryRule{Extensions: append([]string(nil), capability.Extensions...)},
			Refs:            refRuleFromSpecs(specs),
			RelatedRefSpecs: specs,
		})
	}
	sort.SliceStable(rules, func(i, j int) bool {
		if rules[i].Priority != rules[j].Priority {
			return rules[i].Priority > rules[j].Priority
		}
		return rules[i].Format < rules[j].Format
	})
	return rules
}

func BuiltinWholeScopeRules() []FormatRule {
	rules := []FormatRule{}
	for _, capability := range format.ListFormatCapabilities() {
		if !containsString(capability.Layouts, format.FormatLayoutWhole) || len(capability.Extensions) == 0 {
			continue
		}
		dataType := dataTypeFromString(capability.DataType)
		if dataType == DataTypeUnknown {
			continue
		}
		rules = append(rules, FormatRule{
			Format:       string(capability.Format),
			DataType:     dataType,
			Organization: OrganizationWhole,
			Priority:     80,
			Entry:        EntryRule{Extensions: append([]string(nil), capability.Extensions...)},
			WholeScope: &WholeScopeRule{
				AllowRecursive:       true,
				IgnoredFileNames:     []string{"_SUCCESS", "_metadata", "_common_metadata"},
				RequiresStrongMatch:  true,
				ExclusiveOnStrongHit: true,
			},
		})
	}
	sort.SliceStable(rules, func(i, j int) bool {
		if rules[i].Priority != rules[j].Priority {
			return rules[i].Priority > rules[j].Priority
		}
		return rules[i].Format < rules[j].Format
	})
	return rules
}

func RelatedRefSpecs(formatType format.FormatType) []contentio.RelatedRefSpec {
	if provider, err := format.GetTableProvider(formatType); err == nil {
		if specProvider, ok := provider.(format.RelatedRefSpecProvider); ok {
			return specProvider.RelatedRefSpecs()
		}
	}
	if plugin, err := format.GetFormatPlugin(formatType); err == nil {
		if specProvider, ok := plugin.(format.RelatedRefSpecProvider); ok {
			return specProvider.RelatedRefSpecs()
		}
	}
	return nil
}

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
		if rule.Refs != nil || rule.WholeScope != nil {
			return fmt.Errorf("single rule %s must not declare Refs or WholeScope", rule.Format)
		}
	case OrganizationMulti:
		if rule.Refs == nil && len(rule.RelatedRefSpecs) == 0 {
			return fmt.Errorf("multi rule %s requires Refs", rule.Format)
		}
		if rule.Refs != nil {
			if len(rule.Refs.RequiredExtensions) == 0 {
				return fmt.Errorf("multi rule %s requires RequiredExtensions", rule.Format)
			}
			if rule.Refs.EntryExtension == "" {
				return fmt.Errorf("multi rule %s requires EntryExtension", rule.Format)
			}
		}
		if rule.Container != nil || rule.WholeScope != nil {
			return fmt.Errorf("multi rule %s must not declare Container or WholeScope", rule.Format)
		}
	case OrganizationWhole:
		if rule.WholeScope == nil {
			return fmt.Errorf("whole rule %s requires WholeScope", rule.Format)
		}
		if rule.Refs != nil || rule.Container != nil {
			return fmt.Errorf("whole rule %s must not declare Refs or Container", rule.Format)
		}
	default:
		return fmt.Errorf("format rule %s has unsupported Organization %q", rule.Format, rule.Organization)
	}
	return nil
}

func NormalizeCandidate(candidate Candidate) Candidate {
	candidate.Path = strings.TrimSpace(candidate.Path)
	candidate.Name = strings.TrimSpace(candidate.Name)
	if candidate.Name == "" && candidate.Path != "" {
		candidate.Name = filepath.Base(candidate.Path)
	}
	candidate.Extension = contentio.NormalizeExtension(candidate.Extension)
	if candidate.Extension == "" {
		candidate.Extension = contentio.NormalizeExtension(filepath.Ext(candidate.Name))
	}
	candidate.BaseName = strings.TrimSpace(candidate.BaseName)
	if candidate.BaseName == "" && candidate.Name != "" {
		candidate.BaseName = strings.TrimSuffix(candidate.Name, filepath.Ext(candidate.Name))
	}
	return candidate
}

func dataTypeFromString(value string) DataType {
	switch DataType(strings.ToLower(strings.TrimSpace(value))) {
	case DataTypeTable:
		return DataTypeTable
	case DataTypeDocument:
		return DataTypeDocument
	case DataTypeMedia:
		return DataTypeMedia
	case DataTypeContainer:
		return DataTypeContainer
	case DataTypeGraph:
		return DataTypeGraph
	case DataTypeFile:
		return DataTypeFile
	case DataTypeUnknown:
		return DataTypeUnknown
	default:
		return DataTypeUnknown
	}
}

func refRuleFromSpecs(specs []contentio.RelatedRefSpec) *RefRule {
	if len(specs) == 0 {
		return nil
	}
	required := []string{}
	optional := []string{}
	entry := ""
	for _, spec := range specs {
		ext := contentio.NormalizeExtension(spec.Extension)
		if ext == "" {
			continue
		}
		if spec.Required {
			required = append(required, ext)
		} else {
			optional = append(optional, ext)
		}
		if spec.Primary {
			entry = ext
		}
	}
	if entry == "" && len(required) > 0 {
		entry = required[0]
	}
	return &RefRule{
		MatchScope:         RefMatchScopeSameDirectory,
		MatchKey:           RefMatchKeyBaseName,
		RequiredExtensions: required,
		OptionalExtensions: optional,
		EntryExtension:     entry,
	}
}

func normalizeFormat(value string) string {
	value = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), ".")
	if value == "" {
		return string(format.FormatUnknown)
	}
	if detected := format.DetectFormat("file."+value, nil); detected != format.FormatUnknown {
		return string(detected)
	}
	if detected := format.MIMEToFormat(value); detected != format.FormatUnknown {
		return string(detected)
	}
	return value
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func interfaceString(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	default:
		return ""
	}
}
