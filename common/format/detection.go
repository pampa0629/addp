package format

import (
	"path/filepath"
	"strings"
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
		return detectByMagic(peek)
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
	if capability, ok := GetFormatCapability(format); ok {
		return capability.Spatial
	}
	switch format {
	case FormatShapefile, FormatGeoPackage, FormatKML, FormatKMZ:
		return true
	default:
		return false
	}
}

func IsDocumentFormat(format FormatType) bool {
	if capability, ok := GetFormatCapability(format); ok {
		return capability.DataType == FormatDataTypeDocument
	}
	switch format {
	case FormatPDF, FormatDOCX, FormatPPTX, FormatWPS, FormatText, FormatMarkdown:
		return true
	default:
		return false
	}
}

func IsImageFormat(format FormatType) bool {
	switch format {
	case FormatImage, FormatJPEG, FormatPNG, FormatGIF, FormatTIFF, FormatWebP, FormatBMP, FormatSVG, FormatAVIF, FormatHEIC:
		return true
	default:
		return false
	}
}

func IsTableFormat(format FormatType) bool {
	if capability, ok := GetFormatCapability(format); ok && capability.DataType == FormatDataTypeTable {
		return true
	}
	switch format {
	case FormatCSV, FormatExcel, FormatTSV:
		return true
	default:
		return false
	}
}
