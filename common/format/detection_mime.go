package format

import (
	"mime"
	"path/filepath"
	"strings"
)

// MIMEToFormat 将 MIME 类型转换为标准格式类型。
func MIMEToFormat(mimeType string) FormatType {
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	if idx := strings.Index(mimeType, ";"); idx > 0 {
		mimeType = strings.TrimSpace(mimeType[:idx])
	}
	if formatType := descriptorFormatByMIME(mimeType); formatType != FormatUnknown {
		return formatType
	}
	if formatType := fallbackFormatByMIME(mimeType); formatType != FormatUnknown {
		return formatType
	}
	for _, descriptor := range ListFormatDescriptors() {
		for _, candidate := range descriptor.Identification.MimeTypes {
			if strings.HasSuffix(candidate, "/*") && strings.HasPrefix(mimeType, strings.TrimSuffix(candidate, "*")) {
				return descriptor.Format
			}
		}
	}
	return FormatUnknown
}

// FormatToMIME 将格式类型转换为主要 MIME 类型。
func FormatToMIME(format FormatType) string {
	if descriptor, ok := GetFormatDescriptor(format); ok && len(descriptor.Identification.MimeTypes) > 0 {
		return descriptor.Identification.MimeTypes[0]
	}
	if mimeType, ok := fallbackMIMEForFormat(format); ok {
		return mimeType
	}
	return "application/octet-stream"
}

func descriptorFormatByMIME(mimeType string) FormatType {
	if mimeType == "" {
		return FormatUnknown
	}
	for _, descriptor := range ListFormatDescriptors() {
		for _, candidate := range descriptor.Identification.MimeTypes {
			if strings.EqualFold(candidate, mimeType) {
				return descriptor.Format
			}
		}
	}
	return FormatUnknown
}

// GuessContentType 结合文件名和内容猜测 MIME 类型。
func GuessContentType(filename string, peek []byte) string {
	ext := filepath.Ext(filename)
	if mimeType := mime.TypeByExtension(ext); mimeType != "" {
		return mimeType
	}
	return FormatToMIME(DetectFormat(filename, peek))
}
