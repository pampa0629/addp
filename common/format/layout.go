package format

import formatregistry "github.com/addp/common/format/registry"

// Layout describes how content can be organized into a data item.
//
// Format capability uses layout as a declared possibility; data item detection
// uses the same values as the resolved item layout.
type Layout = string

const (
	LayoutSingle Layout = formatregistry.LayoutSingle
	LayoutMulti  Layout = formatregistry.LayoutMulti
	LayoutWhole  Layout = formatregistry.LayoutWhole
)

// NormalizeLayout returns the canonical layout value, or an empty string for unknown values.
func NormalizeLayout(value string) Layout {
	return Layout(formatregistry.NormalizeLayout(value))
}

// IsKnownLayout reports whether value is one of the supported item layout values.
func IsKnownLayout(value string) bool {
	return formatregistry.IsKnownLayout(value)
}

// NormalizeLayouts returns canonical, de-duplicated known layout values.
func NormalizeLayouts(values []string) []string {
	return formatregistry.NormalizeLayouts(values)
}

// ValidateLayouts rejects unknown layout values.
func ValidateLayouts(values []string) error {
	return formatregistry.ValidateLayouts(values)
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
