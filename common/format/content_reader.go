package format

import "strings"

// DescriptorHasContentReader reports whether a descriptor declares a content reader.
//
// It only checks the descriptor / capability declaration. It does not mean a Go
// implementation can be obtained from ProviderRegistry; callers should use the
// corresponding Get*Reader function when they need an executable reader.
func DescriptorHasContentReader(descriptor FormatDescriptor, reader FormatContentReader) bool {
	target := strings.ToLower(strings.TrimSpace(string(reader)))
	if target == "" {
		return false
	}
	for _, candidate := range descriptor.ContentReaders {
		if strings.ToLower(strings.TrimSpace(candidate)) == target {
			return true
		}
	}
	return false
}

// SupportsContentReader reports whether a registered format declares a content reader.
func SupportsContentReader(formatType FormatType, reader FormatContentReader) bool {
	if descriptor, ok := GetFormatDescriptor(formatType); ok {
		return DescriptorHasContentReader(descriptor, reader)
	}
	if capability, ok := GetFormatCapability(formatType); ok {
		return capabilityHasContentReader(capability, reader)
	}
	return false
}

func capabilityHasContentReader(capability FormatCapability, reader FormatContentReader) bool {
	target := strings.ToLower(strings.TrimSpace(string(reader)))
	if target == "" {
		return false
	}
	for _, candidate := range capability.ContentReaders {
		if strings.ToLower(strings.TrimSpace(candidate)) == target {
			return true
		}
	}
	return false
}
