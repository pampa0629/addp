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

// Extract 实现 FileMetadataExtractor 接口。
func (p *Parser) Extract(ctx context.Context, input format.ExtractInput) (*format.ExtractedMetadata, error) {
	// 读取 PDF 内容
	content, err := io.ReadAll(input.Reader)
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

	customAttrs := map[string]interface{}{
		"document_type": "pdf",
		"page_count":    pageCount,
		"encrypted":     bytes.Contains(content, []byte("/Encrypt")),
	}
	for _, field := range []struct {
		key   string
		value string
	}{
		{"title", docMeta["Title"]},
		{"author", docMeta["Author"]},
		{"subject", docMeta["Subject"]},
		{"creator", docMeta["Creator"]},
		{"producer", docMeta["Producer"]},
	} {
		if field.value != "" {
			customAttrs[field.key] = field.value
		}
	}

	return &format.ExtractedMetadata{
		BasicInfo: format.BasicMetadata{
			FileType:     "PDF",
			Size:         input.Size,
			ContentType:  input.ContentType,
			LastModified: input.LastModified,
			ETag:         input.ETag,
		},
		CustomAttrs: customAttrs,
	}, nil
}

// SupportedTypes 实现 FileMetadataExtractor 接口。
func (p *Parser) SupportedTypes() []string {
	return []string{"application/pdf"}
}

// Priority 实现 FileMetadataExtractor 接口。
func (p *Parser) Priority() int {
	return 100
}

func init() {
	parser := NewParser(nil)
	_ = format.RegisterExtractor(parser)
}
