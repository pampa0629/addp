package format

import (
	"fmt"
	"sort"
	"strings"
)

// Layout describes how content can be organized into a data item.
//
// Format capability uses layout as a declared possibility; data item detection
// uses the same values as the resolved item layout.
type Layout = string

const (
	LayoutSingle Layout = "single"
	LayoutMulti  Layout = "multi"
	LayoutWhole  Layout = "whole"
)

// NormalizeLayout returns the canonical layout value, or an empty string for unknown values.
func NormalizeLayout(value string) Layout {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case LayoutSingle:
		return LayoutSingle
	case LayoutMulti:
		return LayoutMulti
	case LayoutWhole:
		return LayoutWhole
	default:
		return ""
	}
}

// IsKnownLayout reports whether value is one of the supported item layout values.
func IsKnownLayout(value string) bool {
	return NormalizeLayout(value) != ""
}

// NormalizeLayouts returns canonical, de-duplicated known layout values.
func NormalizeLayouts(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		layout := NormalizeLayout(value)
		if layout == "" {
			continue
		}
		if _, ok := seen[layout]; ok {
			continue
		}
		seen[layout] = struct{}{}
		result = append(result, layout)
	}
	sort.Strings(result)
	return result
}

// ValidateLayouts rejects unknown layout values.
func ValidateLayouts(values []string) error {
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		if !IsKnownLayout(value) {
			return fmt.Errorf("unsupported layout %q", value)
		}
	}
	return nil
}

// HasLayout reports whether values contains layout after canonical normalization.
func HasLayout(values []string, layout Layout) bool {
	target := NormalizeLayout(layout)
	if target == "" {
		return false
	}
	for _, value := range values {
		if NormalizeLayout(value) == target {
			return true
		}
	}
	return false
}
