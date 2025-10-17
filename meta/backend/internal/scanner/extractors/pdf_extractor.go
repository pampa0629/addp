package extractors

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/addp/meta/internal/scanner"
)

// PDFExtractor PDF文件的元数据提取器
// 注意：这是一个简化实现，只提取基本信息
// 如需完整PDF解析，建议使用 github.com/pdfcpu/pdfcpu 或 github.com/ledongthuc/pdf
type PDFExtractor struct{}

func (e *PDFExtractor) SupportedTypes() []string {
	return []string{
		"application/pdf",
	}
}

func (e *PDFExtractor) Priority() int {
	return 80
}

func (e *PDFExtractor) Extract(ctx context.Context, input scanner.ExtractInput) (*scanner.Metadata, error) {
	// 1. 读取PDF文件头和基本信息
	content, err := io.ReadAll(input.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read PDF content: %w", err)
	}

	// 2. 验证PDF文件头
	if !bytes.HasPrefix(content, []byte("%PDF-")) {
		return nil, fmt.Errorf("not a valid PDF file")
	}

	// 3. 提取PDF版本
	version := extractPDFVersion(content)

	// 4. 提取PDF元数据（Info字典）
	docMeta := extractPDFMetadata(content)

	// 5. 估算页数（简单实现）
	pageCount := estimatePageCount(content)

	// 6. 构建基础元数据
	metadata := &scanner.Metadata{
		BasicInfo: scanner.BasicMetadata{
			FileName:     filepath.Base(input.ObjectKey),
			FileType:     fmt.Sprintf("PDF (%s)", version),
			Size:         input.Size,
			ContentType:  input.ContentType,
			LastModified: input.LastModified,
			ETag:         input.ETag,
		},
		CustomAttrs: make(map[string]interface{}),
	}

	// 7. 添加PDF专用元数据
	metadata.CustomAttrs["pdf_metadata"] = map[string]interface{}{
		"version":     version,
		"page_count":  pageCount,
		"title":       docMeta["Title"],
		"author":      docMeta["Author"],
		"subject":     docMeta["Subject"],
		"keywords":    docMeta["Keywords"],
		"creator":     docMeta["Creator"],
		"producer":    docMeta["Producer"],
		"is_encrypted": bytes.Contains(content, []byte("/Encrypt")),
		"has_forms":    bytes.Contains(content, []byte("/AcroForm")),
	}

	// 8. 构建文档元数据结构
	metadata.CustomAttrs["document_metadata"] = &scanner.DocumentMetadata{
		Title:     docMeta["Title"],
		Author:    docMeta["Author"],
		Subject:   docMeta["Subject"],
		Keywords:  parseKeywords(docMeta["Keywords"]),
		Creator:   docMeta["Creator"],
		Producer:  docMeta["Producer"],
		PageCount: pageCount,
	}

	return metadata, nil
}

// extractPDFVersion 提取PDF版本号
func extractPDFVersion(content []byte) string {
	// PDF文件头格式: %PDF-1.4
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
	// 格式示例: /Info 12 0 obj << /Title (Document Title) /Author (Author Name) >>
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
	// 格式: /Title (Document Title) 或 /Title <hex string>
	// 1. 尝试括号形式
	pattern1 := regexp.MustCompile(fmt.Sprintf(`/%s\s*\((.*?)\)`, fieldName))
	if matches := pattern1.FindStringSubmatch(infoDict); len(matches) > 1 {
		return unescapePDFString(matches[1])
	}

	// 2. 尝试十六进制形式
	pattern2 := regexp.MustCompile(fmt.Sprintf(`/%s\s*<([0-9A-Fa-f]+)>`, fieldName))
	if matches := pattern2.FindStringSubmatch(infoDict); len(matches) > 1 {
		return decodeHexString(matches[1])
	}

	return ""
}

// unescapePDFString 解码PDF字符串转义
func unescapePDFString(s string) string {
	// 处理常见转义
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
	// 简单实现：每两个字符转换为一个字节
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
	// 方法1: 查找 /Type /Page 出现次数（简单但可能不准确）
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

	// 无法确定页数
	return -1
}

// parseKeywords 解析关键词字符串为数组
func parseKeywords(keywords string) []string {
	if keywords == "" {
		return []string{}
	}

	// 常见分隔符：逗号、分号、空格
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
