package format

import (
	"fmt"
	"strings"
)

// DefaultWriteExtension returns the canonical file extension for a format write.
//
// For formats whose writer has multiple user-facing encodings under the same
// FormatType, write options may refine the descriptor's default extension.
func DefaultWriteExtension(formatType FormatType, options *WriteOptions) string {
	if formatType == FormatJSON {
		switch writeOptionString(options, "json_mode") {
		case "jsonl", "lines", "ndjson":
			return ".jsonl"
		}
	}
	descriptor, ok := GetFormatDescriptor(formatType)
	if !ok || len(descriptor.Identification.Extensions) == 0 {
		return ""
	}
	return NormalizeExtension(descriptor.Identification.Extensions[0])
}

func writeOptionString(options *WriteOptions, key string) string {
	if options == nil || options.ExtraParams == nil {
		return ""
	}
	value, ok := options.ExtraParams[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.ToLower(strings.TrimSpace(typed))
	default:
		return strings.ToLower(strings.TrimSpace(fmt.Sprint(typed)))
	}
}

func nestedWriteOptionString(options *WriteOptions, parentKey, childKey string) string {
	if options == nil || options.ExtraParams == nil {
		return ""
	}
	switch nested := options.ExtraParams[parentKey].(type) {
	case map[string]interface{}:
		value, ok := nested[childKey]
		if !ok || value == nil {
			return ""
		}
		return strings.ToLower(strings.TrimSpace(fmt.Sprint(value)))
	case map[string]string:
		return strings.ToLower(strings.TrimSpace(nested[childKey]))
	default:
		return ""
	}
}
