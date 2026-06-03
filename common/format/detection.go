package format

import (
	"path/filepath"
	"strings"

	"github.com/addp/common/datatype"
)

// DetectFormat 根据文件名和内容前缀检测格式。
func DetectFormat(filename string, peek []byte) FormatType {
	ext := strings.ToLower(filepath.Ext(filename))
	if format := extToFormat(ext); format != FormatUnknown {
		if needMagicValidation(format) && len(peek) > 0 && !validateMagicBytes(format, peek) {
			return FormatUnknown
		}
		return format
	}
	if len(peek) > 0 {
		if format := detectByDescriptorSignature(peek); format != FormatUnknown {
			return format
		}
		if format := detectByPluginSniffer(peek); format != FormatUnknown {
			return format
		}
		if format := detectByMagic(peek); format != FormatUnknown {
			return format
		}
		if LooksLikeTextContent(peek) {
			return FormatText
		}
	}
	return FormatUnknown
}

func extToFormat(ext string) FormatType {
	ext = strings.ToLower(strings.TrimSpace(ext))
	if formatType := descriptorFormatByExtension(ext); formatType != FormatUnknown {
		return formatType
	}
	return fallbackFormatByExtension(ext)
}

func descriptorFormatByExtension(ext string) FormatType {
	if ext == "" {
		return FormatUnknown
	}
	for _, descriptor := range ListFormatDescriptors() {
		for _, candidate := range descriptor.Identification.Extensions {
			if strings.EqualFold(candidate, ext) {
				return descriptor.Format
			}
		}
	}
	return FormatUnknown
}

func IsGeospatialFormat(format FormatType) bool {
	switch format {
	case FormatShapefile, FormatGeoPackage, FormatKML, FormatKMZ:
		return true
	default:
		return false
	}
}

func IsDocumentFormat(format FormatType) bool {
	dataType, ok := dataTypeForFormat(format)
	return ok && dataType == datatype.Document
}

func IsImageFormat(format FormatType) bool {
	if format == FormatImage {
		return true
	}
	descriptor, ok := GetFormatDescriptor(format)
	if ok {
		return descriptor.DataType == datatype.Media && descriptorHasMIMEPrefix(descriptor, "image/")
	}
	if mimeType, ok := fallbackMIMEForFormat(format); ok {
		return strings.HasPrefix(mimeType, "image/")
	}
	return false
}

func IsTableFormat(format FormatType) bool {
	dataType, ok := dataTypeForFormat(format)
	return ok && dataType == datatype.Table
}

func dataTypeForFormat(format FormatType) (datatype.DataType, bool) {
	if descriptor, ok := GetFormatDescriptor(format); ok {
		return descriptor.DataType, true
	}
	return fallbackDataTypeForFormat(format)
}

func descriptorHasMIMEPrefix(descriptor FormatDescriptor, prefix string) bool {
	for _, mimeType := range descriptor.Identification.MimeTypes {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(mimeType)), prefix) {
			return true
		}
	}
	return false
}
