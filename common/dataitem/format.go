package dataitem

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

// DetectFormat selects a canonical format from facts already attached to the candidate.
// MIME and extension recognition facts are owned by common/format descriptors and fallbacks.
func DetectFormat(candidate Candidate) string {
	if value := strings.TrimSpace(interfaceString(candidate.Properties["format"])); value != "" {
		return normalizeFormat(value)
	}
	if candidate.ContentType != "" && !format.IsGenericMIME(candidate.ContentType) {
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

func DefaultDataTypeForFormat(formatName string) datatype.DataType {
	descriptor, ok := format.GetFormatDescriptor(format.NormalizeFormat(formatName))
	if !ok {
		return datatype.Unknown
	}
	return descriptor.DataType
}

// InferFormat normalizes explicit format first, then delegates MIME and extension recognition to common/format.
func InferFormat(fileName, contentType, explicitFormat string) string {
	if explicitFormat != "" {
		if canonical := normalizeFormat(explicitFormat); canonical != string(format.FormatUnknown) {
			return canonical
		}
	}
	if contentType != "" && !format.IsGenericMIME(contentType) {
		if detected := format.MIMEToFormat(contentType); detected != format.FormatUnknown {
			return string(detected)
		}
	}
	if detected := format.DetectFormat(fileName, nil); detected != format.FormatUnknown {
		return string(detected)
	}
	return string(format.FormatUnknown)
}

func InferDataType(formatName, contentType string) datatype.DataType {
	if dataType := DefaultDataTypeForFormat(normalizeFormat(formatName)); dataType != datatype.Unknown {
		return dataType
	}
	if !format.IsGenericMIME(contentType) {
		if detected := format.MIMEToFormat(contentType); detected != format.FormatUnknown {
			return DefaultDataTypeForFormat(string(detected))
		}
	}
	return datatype.Unknown
}

func MatchBuiltinSingleResourceRule(formatName string) (FormatRule, bool) {
	return singleResourceRuleFromDescriptor(formatName)
}

func BuiltinSingleResourceRules() []FormatRule {
	rules := []FormatRule{}
	seen := map[string]struct{}{}
	for _, descriptor := range format.ListFormatDescriptors() {
		rule, ok := singleResourceRuleFromDescriptor(string(descriptor.Format))
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

func singleResourceRuleFromDescriptor(formatName string) (FormatRule, bool) {
	descriptor, ok := format.GetFormatDescriptor(format.FormatType(normalizeFormat(formatName)))
	if !ok || !format.HasLayout(descriptor.Layouts, format.LayoutSingle) || len(descriptor.Identification.Extensions) == 0 {
		return FormatRule{}, false
	}
	dataType := descriptor.DataType
	if dataType == datatype.Unknown {
		return FormatRule{}, false
	}
	rule := FormatRule{
		Format:   string(descriptor.Format),
		DataType: dataType,
		Layout:   format.LayoutSingle,
		Priority: 10,
		Entry:    EntryRule{Extensions: append([]string(nil), descriptor.Identification.Extensions...)},
	}
	if dataType == datatype.Container {
		rule.Priority = 20
	}
	return rule, true
}

func BuiltinMultiRules() []FormatRule {
	rules := []FormatRule{}
	for _, descriptor := range format.ListFormatDescriptors() {
		if !format.HasLayout(descriptor.Layouts, format.LayoutMulti) {
			continue
		}
		specs := RelatedRefSpecs(format.FormatType(descriptor.Format))
		if len(specs) == 0 {
			continue
		}
		rules = append(rules, FormatRule{
			Format:          string(descriptor.Format),
			DataType:        descriptor.DataType,
			Layout:          format.LayoutMulti,
			Priority:        100,
			Entry:           EntryRule{Extensions: append([]string(nil), descriptor.Identification.Extensions...)},
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
	for _, descriptor := range format.ListFormatDescriptors() {
		if !format.HasLayout(descriptor.Layouts, format.LayoutWhole) ||
			(len(descriptor.Identification.Extensions) == 0 && len(descriptor.Identification.FileNames) == 0 && len(descriptor.Identification.RelativePaths) == 0) {
			continue
		}
		dataType := descriptor.DataType
		if dataType == datatype.Unknown {
			continue
		}
		rules = append(rules, FormatRule{
			Format:   string(descriptor.Format),
			DataType: dataType,
			Layout:   format.LayoutWhole,
			Priority: 80,
			Entry:    EntryRule{Extensions: append([]string(nil), descriptor.Identification.Extensions...)},
			WholeScope: &WholeScopeRule{
				IgnoredFileNames:     []string{"_SUCCESS", "_metadata", "_common_metadata"},
				RequiredFileNames:    append([]string(nil), descriptor.Identification.FileNames...),
				RequiredPaths:        append([]string(nil), descriptor.Identification.RelativePaths...),
				RequiresStrongMatch:  true,
				ExclusiveOnStrongHit: true,
				ClaimAllOnStrongHit:  len(descriptor.Identification.FileNames) > 0 || len(descriptor.Identification.RelativePaths) > 0,
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

func RelatedRefSpecs(formatType format.FormatType) []format.RelatedRefSpec {
	if provider, err := format.GetMultiTableInfoProvider(formatType); err == nil {
		return provider.RelatedRefSpecs()
	}
	if reader, err := format.GetMultiTableSampleReader(formatType); err == nil {
		return reader.RelatedRefSpecs()
	}
	if reader, err := format.GetMultiTableReaderProvider(formatType); err == nil {
		return reader.RelatedRefSpecs()
	}
	if writer, err := format.GetMultiTableWriterProvider(formatType); err == nil {
		return writer.RelatedRefSpecs()
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
	if rule.Layout == "" {
		return fmt.Errorf("format rule %s requires Layout", rule.Format)
	}
	if len(rule.Entry.Extensions) == 0 && rule.Layout != format.LayoutWhole {
		return fmt.Errorf("format rule %s requires Entry", rule.Format)
	}
	switch rule.Layout {
	case format.LayoutSingle:
		if rule.Refs != nil || rule.WholeScope != nil {
			return fmt.Errorf("single rule %s must not declare Refs or WholeScope", rule.Format)
		}
	case format.LayoutMulti:
		if rule.Refs == nil && len(rule.RelatedRefSpecs) == 0 {
			return fmt.Errorf("multi rule %s requires Refs", rule.Format)
		}
		if len(rule.RelatedRefSpecs) > 0 {
			if err := format.ValidateRelatedRefSpecs(rule.RelatedRefSpecs); err != nil {
				return fmt.Errorf("multi rule %s has invalid RelatedRefSpecs: %w", rule.Format, err)
			}
		}
		if rule.Refs != nil {
			if len(rule.Refs.RequiredExtensions) == 0 {
				return fmt.Errorf("multi rule %s requires RequiredExtensions", rule.Format)
			}
			if rule.Refs.EntryExtension == "" {
				return fmt.Errorf("multi rule %s requires EntryExtension", rule.Format)
			}
		}
		if rule.WholeScope != nil {
			return fmt.Errorf("multi rule %s must not declare WholeScope", rule.Format)
		}
	case format.LayoutWhole:
		if rule.WholeScope == nil {
			return fmt.Errorf("whole rule %s requires WholeScope", rule.Format)
		}
		if rule.Refs != nil {
			return fmt.Errorf("whole rule %s must not declare Refs", rule.Format)
		}
	default:
		return fmt.Errorf("format rule %s has unsupported Layout %q", rule.Format, rule.Layout)
	}
	return nil
}

func NormalizeCandidate(candidate Candidate) Candidate {
	candidate.Path = strings.TrimSpace(candidate.Path)
	candidate.Name = strings.TrimSpace(candidate.Name)
	if candidate.Name == "" && candidate.Path != "" {
		candidate.Name = filepath.Base(candidate.Path)
	}
	candidate.Extension = format.NormalizeExtension(candidate.Extension)
	if candidate.Extension == "" {
		candidate.Extension = format.NormalizeExtension(filepath.Ext(candidate.Name))
	}
	candidate.BaseName = strings.TrimSpace(candidate.BaseName)
	if candidate.BaseName == "" && candidate.Name != "" {
		candidate.BaseName = strings.TrimSuffix(candidate.Name, filepath.Ext(candidate.Name))
	}
	return candidate
}

func refRuleFromSpecs(specs []format.RelatedRefSpec) *RefRule {
	if len(specs) == 0 {
		return nil
	}
	required := []string{}
	optional := []string{}
	entry := ""
	for _, spec := range specs {
		ext := format.NormalizeExtension(spec.Extension)
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
		RequiredExtensions: required,
		OptionalExtensions: optional,
		EntryExtension:     entry,
	}
}

func normalizeFormat(value string) string {
	return string(format.NormalizeFormat(value))
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
