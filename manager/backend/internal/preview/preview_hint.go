package preview

import (
	"path/filepath"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

const (
	previewMaterialText        = "text"
	previewMaterialMarkdown    = "markdown"
	previewMaterialJSON        = "json"
	previewMaterialGeoJSON     = "geojson"
	previewMaterialRawBinary   = "raw_binary"
	previewMaterialTable       = "table"
	previewMaterialContainer   = "container"
	previewMaterialUnsupported = "unsupported"
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
	if isTextPreviewContentType(input.ContentType) || format.LooksLikeTextContent(input.Peek) {
		return format.FormatText
	}
	return format.FormatUnknown
}

func previewHintDataType(formatType format.FormatType, contentType string) string {
	if descriptor, ok := format.GetFormatDescriptor(formatType); ok && descriptor.DataType != "" {
		return string(descriptor.DataType)
	}
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	switch {
	case strings.HasPrefix(contentType, "image/"), strings.HasPrefix(contentType, "video/"), strings.HasPrefix(contentType, "audio/"):
		return string(datatype.Media)
	case strings.HasPrefix(contentType, "text/"):
		return string(datatype.Document)
	default:
		return string(datatype.Unknown)
	}
}

func previewHintSemantics(formatType format.FormatType, dataType, contentType string, peek []byte) (string, string, bool, bool) {
	if formatType == format.FormatMarkdown {
		return previewMaterialMarkdown, "markdown", true, true
	}
	if formatType == format.FormatGeoJSON {
		return previewMaterialGeoJSON, "map", true, true
	}
	if formatType == format.FormatJSON {
		return previewMaterialJSON, "json", true, true
	}
	if documentRenderer := documentPreviewRenderer(formatType); documentRenderer != "" {
		return previewMaterialRawBinary, documentRenderer, true, false
	}
	if videoPreviewFormat(formatType, contentType) {
		return previewMaterialRawBinary, "video", true, false
	}
	if format.IsImageFormat(formatType) || strings.HasPrefix(strings.ToLower(strings.TrimSpace(contentType)), "image/") {
		return previewMaterialRawBinary, "image", true, false
	}
	if isTextPreviewFormat(formatType) || isTextPreviewContentType(contentType) || format.LooksLikeTextContent(peek) {
		return previewMaterialText, "text", true, true
	}
	switch strings.TrimSpace(dataType) {
	case string(datatype.Table):
		return previewMaterialTable, "table", true, false
	case string(datatype.Container):
		return previewMaterialContainer, "container", true, false
	case string(datatype.Document):
		return previewMaterialUnsupported, "unsupported", false, false
	case string(datatype.Media):
		return previewMaterialUnsupported, "unsupported", false, false
	default:
		return previewMaterialUnsupported, "unsupported", false, false
	}
}

func documentPreviewRenderer(formatType format.FormatType) string {
	switch formatType {
	case format.FormatPDF:
		return "pdf"
	case format.FormatDOCX:
		return "docx"
	case format.FormatPPTX:
		return "pptx"
	case format.FormatWPS:
		return "wps"
	default:
		return ""
	}
}

func videoPreviewFormat(formatType format.FormatType, contentType string) bool {
	if formatType == format.FormatVideo || formatType == format.FormatMP4 {
		return true
	}
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	return strings.HasPrefix(contentType, "video/")
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
