package format

import "strings"

// NormalizeFormat maps a user supplied format, extension, MIME type or filename
// to ADDP's canonical format identifier. Unknown values stay unknown instead of
// becoming ad-hoc format names.
func NormalizeFormat(value string) FormatType {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" || normalized == string(FormatUnknown) {
		return FormatUnknown
	}
	if _, ok := GetFormatDescriptor(FormatType(normalized)); ok {
		return FormatType(normalized)
	}
	if detected := MIMEToFormat(normalized); detected != FormatUnknown {
		return detected
	}
	if detected := DetectFormat(normalized, nil); detected != FormatUnknown {
		return detected
	}
	if strings.Contains(normalized, "/") || strings.Contains(normalized, "\\") {
		return FormatUnknown
	}
	extensionCandidate := strings.TrimPrefix(normalized, ".")
	if extensionCandidate == "" {
		return FormatUnknown
	}
	if detected := DetectFormat("file."+extensionCandidate, nil); detected != FormatUnknown {
		return detected
	}
	return FormatUnknown
}
