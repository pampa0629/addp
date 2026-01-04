package pdf

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/addp/common/format"
)

// Parser 实现 PDF 格式的解析器
type Parser struct {
	options *format.ParseOptions
}

// NewParser 创建 PDF 解析器
func NewParser(opts *format.ParseOptions) *Parser {
	if opts == nil {
		opts = format.DefaultParseOptions()
	}
	return &Parser{options: opts}
}

// SupportedFormats 返回支持的格式
func (p *Parser) SupportedFormats() []format.FormatType {
	return []format.FormatType{format.FormatPDF}
}

// ParseSchema 解析 PDF Schema
// 对于 PDF 文件，schema 包含文档元数据字段
func (p *Parser) ParseSchema(ctx context.Context, input io.Reader) (*format.Schema, error) {
	// 读取 PDF 内容
	content, err := io.ReadAll(input)
	if err != nil {
		return nil, fmt.Errorf("failed to read PDF content: %w", err)
	}

	// 验证 PDF 文件头
	if !bytes.HasPrefix(content, []byte("%PDF-")) {
		return nil, fmt.Errorf("not a valid PDF file")
	}

	// 构建 schema（PDF 作为文档类型的 schema）
	fields := []format.Field{
		{Name: "version", Type: format.FieldTypeString, Nullable: false},
		{Name: "page_count", Type: format.FieldTypeInt, Nullable: false},
		{Name: "title", Type: format.FieldTypeString, Nullable: true},
		{Name: "author", Type: format.FieldTypeString, Nullable: true},
		{Name: "subject", Type: format.FieldTypeString, Nullable: true},
		{Name: "keywords", Type: format.FieldTypeString, Nullable: true},
		{Name: "creator", Type: format.FieldTypeString, Nullable: true},
		{Name: "producer", Type: format.FieldTypeString, Nullable: true},
		{Name: "is_encrypted", Type: format.FieldTypeBool, Nullable: false},
		{Name: "has_forms", Type: format.FieldTypeBool, Nullable: false},
	}

	recordCount := int64(1) // PDF 文档作为单条记录
	return &format.Schema{
		Fields:      fields,
		RecordCount: &recordCount,
	}, nil
}

// ReadRecords 读取 PDF 数据记录（返回 PDF 元数据作为单条记录）
func (p *Parser) ReadRecords(ctx context.Context, input io.Reader, offset, limit int64) ([]map[string]interface{}, error) {
	if offset > 0 {
		return []map[string]interface{}{}, nil
	}

	// 读取 PDF 内容
	content, err := io.ReadAll(input)
	if err != nil {
		return nil, fmt.Errorf("failed to read PDF content: %w", err)
	}

	// 验证 PDF
	if !bytes.HasPrefix(content, []byte("%PDF-")) {
		return nil, fmt.Errorf("not a valid PDF file")
	}

	// 提取元数据
	version := extractPDFVersion(content)
	docMeta := extractPDFMetadata(content)
	pageCount := estimatePageCount(content)

	// 构建记录
	record := map[string]interface{}{
		"version":      version,
		"page_count":   pageCount,
		"title":        docMeta["Title"],
		"author":       docMeta["Author"],
		"subject":      docMeta["Subject"],
		"keywords":     docMeta["Keywords"],
		"creator":      docMeta["Creator"],
		"producer":     docMeta["Producer"],
		"is_encrypted": bytes.Contains(content, []byte("/Encrypt")),
		"has_forms":    bytes.Contains(content, []byte("/AcroForm")),
	}

	return []map[string]interface{}{record}, nil
}

// CountRecords 统计总记录数（PDF 文档始终为1）
func (p *Parser) CountRecords(ctx context.Context, input io.Reader) (int64, error) {
	return 1, nil
}

// ExtractMetadata 提取 PDF 元数据
func (p *Parser) ExtractMetadata(ctx context.Context, input io.Reader) (map[string]interface{}, error) {
	// 读取 PDF 内容
	content, err := io.ReadAll(input)
	if err != nil {
		return nil, fmt.Errorf("failed to read PDF content: %w", err)
	}

	// 验证 PDF
	if !bytes.HasPrefix(content, []byte("%PDF-")) {
		return nil, fmt.Errorf("not a valid PDF file")
	}

	// 提取元数据
	version := extractPDFVersion(content)
	docMeta := extractPDFMetadata(content)
	pageCount := estimatePageCount(content)

	// 构建元数据
	metadata := map[string]interface{}{
		"version":      version,
		"page_count":   pageCount,
		"title":        docMeta["Title"],
		"author":       docMeta["Author"],
		"subject":      docMeta["Subject"],
		"keywords":     parseKeywords(docMeta["Keywords"]),
		"creator":      docMeta["Creator"],
		"producer":     docMeta["Producer"],
		"is_encrypted": bytes.Contains(content, []byte("/Encrypt")),
		"has_forms":    bytes.Contains(content, []byte("/AcroForm")),
	}

	return metadata, nil
}

// extractPDFVersion 提取 PDF 版本号
func extractPDFVersion(content []byte) string {
	// PDF 文件头格式: %PDF-1.4
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

// extractPDFMetadata 从 PDF Info 字典中提取元数据
func extractPDFMetadata(content []byte) map[string]string {
	metadata := make(map[string]string)

	// 查找 Info 字典
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

// extractPDFField 从 Info 字典中提取指定字段
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

// unescapePDFString 解码 PDF 字符串转义
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

// estimatePageCount 估算 PDF 页数
func estimatePageCount(content []byte) int {
	// 方法1: 查找 /Type /Page 出现次数（简单但可能不准确）
	pagePattern := regexp.MustCompile(`/Type\s*/Page[^s]`)
	matches := pagePattern.FindAll(content, -1)
	pageCount := len(matches)

	if pageCount > 0 {
		return pageCount
	}

	// 方法2: 查找 /Count 字段（在 Pages 对象中）
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

// ============ ObjectInfoParser 接口实现 ============

// ParseObjectInfo 实现 ObjectInfoParser 接口
// 从 PDF 文件中提取 ObjectInfo（包含 PDFInfo 扩展信息）
func (p *Parser) ParseObjectInfo(ctx context.Context, input io.Reader, basicInfo format.ObjectBasicInfo) (*format.ObjectInfo, error) {
	// 读取 PDF 内容
	content, err := io.ReadAll(input)
	if err != nil {
		return nil, fmt.Errorf("failed to read PDF content: %w", err)
	}

	// 验证 PDF
	if !bytes.HasPrefix(content, []byte("%PDF-")) {
		return nil, fmt.Errorf("not a valid PDF file")
	}

	// 提取元数据
	docMeta := extractPDFMetadata(content)
	pageCount := estimatePageCount(content)

	// 构建 PDFInfo
	pdfInfo := &format.PDFInfo{
		PageCount: pageCount,
		Title:     docMeta["Title"],
		Author:    docMeta["Author"],
		Subject:   docMeta["Subject"],
		Creator:   docMeta["Creator"],
		Producer:  docMeta["Producer"],
		Encrypted: bytes.Contains(content, []byte("/Encrypt")),
	}

	// 构建 ObjectInfo
	objectInfo := &format.ObjectInfo{
		Key:         basicInfo.Key,
		SizeBytes:   basicInfo.SizeBytes,
		ModifiedAt:  basicInfo.ModifiedAt,
		ContentType: basicInfo.ContentType,
		ETag:        basicInfo.ETag,
		Extensions:  []format.ExtensionInfo{pdfInfo},
	}

	return objectInfo, nil
}

// SupportedContentTypes 实现 ObjectInfoParser 接口
// 返回支持的 MIME 类型
func (p *Parser) SupportedContentTypes() []string {
	return []string{
		"application/pdf",
	}
}

func init() {
	parser := NewParser(nil)
	// 注册为 ObjectInfoParser
	_ = format.RegisterObjectInfoParser(parser)
}
