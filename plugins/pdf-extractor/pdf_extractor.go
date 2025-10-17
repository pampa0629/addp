// Package pdfextractor PDF文件元数据提取器插件
package pdfextractor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	sdk "github.com/addp/meta-extractor-sdk"
)

// PDFMetadata PDF文件的类型化元数据
type PDFMetadata struct {
	Version     string    `json:"version"`
	PageCount   int       `json:"page_count"`
	Title       string    `json:"title"`
	Author      string    `json:"author"`
	Subject     string    `json:"subject"`
	Keywords    []string  `json:"keywords"`
	Creator     string    `json:"creator"`
	Producer    string    `json:"producer"`
	CreatedDate time.Time `json:"created_date"`
	ModifiedDate time.Time `json:"modified_date"`
	IsEncrypted bool      `json:"is_encrypted"`
	HasForms    bool      `json:"has_forms"`
}

// TypeName 实现 TypedMetadata 接口
func (m *PDFMetadata) TypeName() string {
	return "pdf.metadata"
}

// Schema 实现 TypedMetadata 接口
func (m *PDFMetadata) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"version":       map[string]string{"type": "string", "description": "PDF version"},
			"page_count":    map[string]string{"type": "integer", "description": "Number of pages"},
			"title":         map[string]string{"type": "string", "description": "Document title"},
			"author":        map[string]string{"type": "string", "description": "Author name"},
			"subject":       map[string]string{"type": "string", "description": "Document subject"},
			"keywords":      map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}},
			"creator":       map[string]string{"type": "string", "description": "Creating application"},
			"producer":      map[string]string{"type": "string", "description": "PDF producer"},
			"created_date":  map[string]string{"type": "string", "format": "date-time"},
			"modified_date": map[string]string{"type": "string", "format": "date-time"},
			"is_encrypted":  map[string]string{"type": "boolean", "description": "Whether the PDF is encrypted"},
			"has_forms":     map[string]string{"type": "boolean", "description": "Whether the PDF contains forms"},
		},
		"required": []string{"version", "page_count"},
	}
}

// ToMap 实现 TypedMetadata 接口
func (m *PDFMetadata) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"version":       m.Version,
		"page_count":    m.PageCount,
		"title":         m.Title,
		"author":        m.Author,
		"subject":       m.Subject,
		"keywords":      m.Keywords,
		"creator":       m.Creator,
		"producer":      m.Producer,
		"created_date":  m.CreatedDate.Format(time.RFC3339),
		"modified_date": m.ModifiedDate.Format(time.RFC3339),
		"is_encrypted":  m.IsEncrypted,
		"has_forms":     m.HasForms,
	}
}

// FromMap 实现 TypedMetadata 接口
func (m *PDFMetadata) FromMap(data map[string]interface{}) error {
	if v, ok := data["version"].(string); ok {
		m.Version = v
	}
	if v, ok := data["page_count"].(float64); ok {
		m.PageCount = int(v)
	}
	if v, ok := data["title"].(string); ok {
		m.Title = v
	}
	if v, ok := data["author"].(string); ok {
		m.Author = v
	}
	if v, ok := data["subject"].(string); ok {
		m.Subject = v
	}
	if v, ok := data["keywords"].([]interface{}); ok {
		m.Keywords = make([]string, len(v))
		for i, val := range v {
			if s, ok := val.(string); ok {
				m.Keywords[i] = s
			}
		}
	}
	if v, ok := data["creator"].(string); ok {
		m.Creator = v
	}
	if v, ok := data["producer"].(string); ok {
		m.Producer = v
	}
	if v, ok := data["is_encrypted"].(bool); ok {
		m.IsEncrypted = v
	}
	if v, ok := data["has_forms"].(bool); ok {
		m.HasForms = v
	}
	return nil
}

// init 函数：注册自定义元数据类型
func init() {
	sdk.RegisterMetadataType(&PDFMetadata{})
}

// PDFExtractor PDF文件的元数据提取器
type PDFExtractor struct{}

// SupportedTypes 返回支持的MIME类型
func (e *PDFExtractor) SupportedTypes() []string {
	return []string{"application/pdf"}
}

// Priority 返回优先级
func (e *PDFExtractor) Priority() int {
	return 80
}

// Extract 提取PDF文件元数据
func (e *PDFExtractor) Extract(ctx context.Context, input sdk.ExtractInput) (*sdk.Metadata, error) {
	// 1. 读取PDF内容
	content, err := io.ReadAll(input.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read PDF content: %w", err)
	}

	// 2. 验证PDF文件头
	if !bytes.HasPrefix(content, []byte("%PDF-")) {
		return nil, fmt.Errorf("not a valid PDF file")
	}

	// 3. 创建基础元数据
	metadata := sdk.NewMetadata(
		filepath.Base(input.ObjectKey),
		"PDF Document",
		input.Size,
	)

	metadata.BasicInfo.ContentType = input.ContentType
	metadata.BasicInfo.LastModified = input.LastModified
	metadata.BasicInfo.ETag = input.ETag

	// 4. 提取PDF元数据
	pdfMeta := e.extractPDFInfo(content)

	// 5. 添加类型化元数据
	metadata.AddTypedMetadata("pdf_metadata", pdfMeta)

	// 6. 添加其他自定义属性
	metadata.CustomAttrs["file_size"] = input.Size
	metadata.CustomAttrs["file_size_human"] = formatFileSize(input.Size)
	metadata.CustomAttrs["file_extension"] = filepath.Ext(input.ObjectKey)

	return metadata, nil
}

// formatFileSize 格式化文件大小为人类可读格式
func formatFileSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(size)/float64(div), "KMGTPE"[exp])
}

// extractPDFInfo 提取PDF信息
func (e *PDFExtractor) extractPDFInfo(content []byte) *PDFMetadata {
	meta := &PDFMetadata{
		Version:   extractPDFVersion(content),
		PageCount: estimatePageCount(content),
		Keywords:  []string{},
	}

	// 提取Info字典
	infoDict := extractPDFMetadata(content)
	meta.Title = infoDict["Title"]
	meta.Author = infoDict["Author"]
	meta.Subject = infoDict["Subject"]
	meta.Creator = infoDict["Creator"]
	meta.Producer = infoDict["Producer"]
	meta.Keywords = parseKeywords(infoDict["Keywords"])

	// 检测加密和表单
	meta.IsEncrypted = bytes.Contains(content, []byte("/Encrypt"))
	meta.HasForms = bytes.Contains(content, []byte("/AcroForm"))

	return meta
}

// extractPDFVersion 提取PDF版本号
func extractPDFVersion(content []byte) string {
	if len(content) < 8 {
		return "unknown"
	}

	header := string(content[:8])
	if strings.HasPrefix(header, "%PDF-") {
		version := strings.TrimPrefix(header, "%PDF-")
		return strings.TrimSpace(version)
	}

	return "unknown"
}

// extractPDFMetadata 从PDF Info字典中提取元数据
func extractPDFMetadata(content []byte) map[string]string {
	metadata := make(map[string]string)

	// 查找Info字典
	infoPattern := regexp.MustCompile(`/Info\s+\d+\s+\d+\s+obj\s*<<(.*?)>>`)
	matches := infoPattern.FindSubmatch(content)

	if len(matches) < 2 {
		return metadata
	}

	infoDict := string(matches[1])

	// 提取各个字段
	fields := []string{"Title", "Author", "Subject", "Keywords", "Creator", "Producer"}
	for _, field := range fields {
		value := extractPDFField(infoDict, field)
		if value != "" {
			metadata[field] = value
		}
	}

	return metadata
}

// extractPDFField 从Info字典中提取指定字段
func extractPDFField(infoDict, fieldName string) string {
	// 格式: /Title (Document Title)
	pattern1 := regexp.MustCompile(fmt.Sprintf(`/%s\s*\((.*?)\)`, fieldName))
	if matches := pattern1.FindStringSubmatch(infoDict); len(matches) > 1 {
		return unescapePDFString(matches[1])
	}

	// 格式: /Title <hex string>
	pattern2 := regexp.MustCompile(fmt.Sprintf(`/%s\s*<([0-9A-Fa-f]+)>`, fieldName))
	if matches := pattern2.FindStringSubmatch(infoDict); len(matches) > 1 {
		return decodeHexString(matches[1])
	}

	return ""
}

// unescapePDFString 解码PDF字符串转义
func unescapePDFString(s string) string {
	s = strings.ReplaceAll(s, `\n`, "\n")
	s = strings.ReplaceAll(s, `\r`, "\r")
	s = strings.ReplaceAll(s, `\t`, "\t")
	s = strings.ReplaceAll(s, `\\`, `\`)
	s = strings.ReplaceAll(s, `\(`, `(`)
	s = strings.ReplaceAll(s, `\)`, `)`)
	return s
}

// decodeHexString 解码十六进制字符串
func decodeHexString(hexStr string) string {
	var result []byte
	for i := 0; i < len(hexStr); i += 2 {
		if i+1 < len(hexStr) {
			var b byte
			fmt.Sscanf(hexStr[i:i+2], "%02x", &b)
			result = append(result, b)
		}
	}
	return string(result)
}

// estimatePageCount 估算PDF页数
func estimatePageCount(content []byte) int {
	// 方法1: 查找 /Type /Page 出现次数
	pagePattern := regexp.MustCompile(`/Type\s*/Page[^s]`)
	matches := pagePattern.FindAll(content, -1)
	pageCount := len(matches)

	if pageCount > 0 {
		return pageCount
	}

	// 方法2: 查找 /Count 字段（在Pages对象中）
	countPattern := regexp.MustCompile(`/Type\s*/Pages.*?/Count\s+(\d+)`)
	if matches := countPattern.FindSubmatch(content); len(matches) > 1 {
		var count int
		fmt.Sscanf(string(matches[1]), "%d", &count)
		return count
	}

	return -1
}

// parseKeywords 解析关键词字符串为数组
func parseKeywords(keywords string) []string {
	if keywords == "" {
		return []string{}
	}

	delimiters := regexp.MustCompile(`[,;\s]+`)
	parts := delimiters.Split(keywords, -1)

	result := []string{}
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}

	return result
}

// GetExtractor 返回提取器实例（供ADDP加载使用）
func GetExtractor() sdk.MetadataExtractor {
	return &PDFExtractor{}
}
