package preview

import (
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

const (
	previewMaterialText      = "text"
	previewMaterialMarkdown  = "markdown"
	previewMaterialJSON      = "json"
	previewMaterialRawBinary = "raw_binary"
	previewMaterialTable     = "table"
)

type previewHintInput struct {
	Name        string
	Path        string
	Format      format.FormatType
	DataType    string
	ContentType string
	Peek        []byte
}

type previewHint struct {
	DataType    string
	Format      format.FormatType
	Material    string
	Renderer    string
	Previewable bool
	TextLike    bool
}

func inferPreviewHint(input previewHintInput) previewHint {
	formatType := normalizePreviewHintFormat(input)
	dataType := strings.TrimSpace(input.DataType)
	if dataType == "" {
		dataType = previewHintDataType(formatType, input.ContentType)
	}
	material, renderer, previewable, textLike := previewHintSemantics(formatType, dataType, input.ContentType, input.Peek)
	return previewHint{
		DataType:    dataType,
		Format:      formatType,
		Material:    material,
		Renderer:    renderer,
		Previewable: previewable,
		TextLike:    textLike,
	}
}

func normalizePreviewHintFormat(input previewHintInput) format.FormatType {
	if input.Format != "" && input.Format != format.FormatUnknown {
		return input.Format
	}
	if input.ContentType != "" {
		if detected := format.MIMEToFormat(input.ContentType); detected != format.FormatUnknown {
			return detected
		}
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = filepath.Base(input.Path)
	}
	if name != "" {
		if detected := format.DetectFormat(name, input.Peek); detected != format.FormatUnknown {
			return detected
		}
	}
	if input.Path != "" {
		if detected := format.DetectFormat(input.Path, input.Peek); detected != format.FormatUnknown {
			return detected
		}
	}
	if isTextPreviewContentType(input.ContentType) || looksLikeText(input.Peek) {
		return format.FormatText
	}
	return format.FormatUnknown
}

func previewHintDataType(formatType format.FormatType, contentType string) string {
	if descriptor, ok := format.GetFormatDescriptor(formatType); ok && descriptor.DataType != "" {
		return string(descriptor.DataType)
	}
	if capability, ok := format.GetFormatCapability(formatType); ok && capability.DataType != "" {
		return string(capability.DataType)
	}
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	switch {
	case strings.HasPrefix(contentType, "image/"), strings.HasPrefix(contentType, "video/"), strings.HasPrefix(contentType, "audio/"):
		return string(datatype.DataTypeMedia)
	case strings.HasPrefix(contentType, "text/"):
		return string(datatype.DataTypeDocument)
	default:
		return string(datatype.DataTypeFile)
	}
}

func previewHintSemantics(formatType format.FormatType, dataType, contentType string, peek []byte) (string, string, bool, bool) {
	if formatType == format.FormatMarkdown {
		return previewMaterialMarkdown, "markdown", true, true
	}
	if formatType == format.FormatJSON {
		return previewMaterialJSON, "json", true, true
	}
	if format.IsImageFormat(formatType) || strings.HasPrefix(strings.ToLower(strings.TrimSpace(contentType)), "image/") {
		return previewMaterialRawBinary, "image", true, false
	}
	if isTextPreviewFormat(formatType) || isTextPreviewContentType(contentType) || looksLikeText(peek) {
		return previewMaterialText, "text", true, true
	}
	switch strings.TrimSpace(dataType) {
	case string(datatype.DataTypeTable):
		return previewMaterialTable, "table", true, false
	case string(datatype.DataTypeContainer):
		return previewMaterialJSON, "container", true, false
	case string(datatype.DataTypeDocument):
		return previewMaterialRawBinary, "text", true, false
	case string(datatype.DataTypeMedia):
		return previewMaterialRawBinary, "", true, false
	default:
		return previewMaterialRawBinary, "text", false, false
	}
}

func isTextPreviewFormat(formatType format.FormatType) bool {
	switch formatType {
	case format.FormatText, format.FormatXML, format.FormatSVG:
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
