package format

import (
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	PreviewMaterialText      = "text"
	PreviewMaterialMarkdown  = "markdown"
	PreviewMaterialJSON      = "json"
	PreviewMaterialGeoJSON   = "geojson"
	PreviewMaterialRawBinary = "raw_binary"
	PreviewMaterialTable     = "table"
	PreviewMaterialURL       = "url"
)

type PreviewHintInput struct {
	Name        string
	Path        string
	Format      FormatType
	DataType    string
	ContentType string
	Peek        []byte
}

type PreviewHint struct {
	DataType    string
	Format      FormatType
	Material    string
	Renderer    string
	Previewable bool
	TextLike    bool
}

func InferPreviewHint(input PreviewHintInput) PreviewHint {
	formatType := normalizePreviewFormat(input)
	dataType := strings.TrimSpace(input.DataType)
	if dataType == "" {
		dataType = previewDataType(formatType, input.ContentType)
	}
	material, renderer, previewable, textLike := previewSemantics(formatType, dataType, input.ContentType, input.Peek)
	return PreviewHint{
		DataType:    dataType,
		Format:      formatType,
		Material:    material,
		Renderer:    renderer,
		Previewable: previewable,
		TextLike:    textLike,
	}
}

func normalizePreviewFormat(input PreviewHintInput) FormatType {
	if input.Format != "" && input.Format != FormatUnknown {
		return input.Format
	}
	if input.ContentType != "" {
		if detected := MIMEToFormat(input.ContentType); detected != FormatUnknown {
			return detected
		}
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = filepath.Base(input.Path)
	}
	if name != "" {
		if detected := DetectFormat(name, input.Peek); detected != FormatUnknown {
			return detected
		}
	}
	if input.Path != "" {
		if detected := DetectFormat(input.Path, input.Peek); detected != FormatUnknown {
			return detected
		}
	}
	if isTextPreviewContentType(input.ContentType) || looksLikeText(input.Peek) {
		return FormatText
	}
	return FormatUnknown
}

func previewDataType(formatType FormatType, contentType string) string {
	if descriptor, ok := GetFormatDescriptor(formatType); ok && descriptor.DataType != "" {
		return descriptor.DataType
	}
	if capability, ok := GetFormatCapability(formatType); ok && capability.DataType != "" {
		return capability.DataType
	}
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	switch {
	case strings.HasPrefix(contentType, "image/"), strings.HasPrefix(contentType, "video/"), strings.HasPrefix(contentType, "audio/"):
		return FormatDataTypeMedia
	case strings.HasPrefix(contentType, "text/"):
		return FormatDataTypeDocument
	default:
		return FormatDataTypeFile
	}
}

func previewSemantics(formatType FormatType, dataType, contentType string, peek []byte) (string, string, bool, bool) {
	if formatType == FormatMarkdown {
		return PreviewMaterialMarkdown, "markdown", true, true
	}
	if formatType == FormatJSON {
		return PreviewMaterialJSON, "json", true, true
	}
	if IsImageFormat(formatType) || strings.HasPrefix(strings.ToLower(strings.TrimSpace(contentType)), "image/") {
		return PreviewMaterialRawBinary, "image", true, false
	}
	if isTextPreviewFormat(formatType) || isTextPreviewContentType(contentType) || looksLikeText(peek) {
		return PreviewMaterialText, "text", true, true
	}
	switch strings.TrimSpace(dataType) {
	case FormatDataTypeTable:
		return PreviewMaterialTable, "table", true, false
	case FormatDataTypeContainer:
		return PreviewMaterialJSON, "container", true, false
	case FormatDataTypeDocument:
		return PreviewMaterialRawBinary, "text", true, false
	case FormatDataTypeMedia:
		return PreviewMaterialRawBinary, "", true, false
	default:
		return PreviewMaterialRawBinary, "text", false, false
	}
}

func isTextPreviewFormat(formatType FormatType) bool {
	switch formatType {
	case FormatText, FormatXML, FormatSVG:
		return true
	default:
		return false
	}
}

func isTextPreviewContentType(contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	if idx := strings.Index(contentType, ";"); idx >= 0 {
		contentType = strings.TrimSpace(contentType[:idx])
	}
	if strings.HasPrefix(contentType, "text/") {
		return true
	}
	switch contentType {
	case "application/xml", "application/xhtml+xml", "application/json", "application/geo+json", "application/vnd.geo+json":
		return true
	default:
		return false
	}
}

func looksLikeText(peek []byte) bool {
	if len(peek) == 0 {
		return false
	}
	if !utf8.Valid(peek) {
		return false
	}
	for _, b := range peek {
		if b == 0 {
			return false
		}
		if b < 0x20 && b != '\n' && b != '\r' && b != '\t' && b != '\f' {
			return false
		}
	}
	return true
}
