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

const defaultReadLimit int64 = 8 * 1024 * 1024
const readLimitParam = "document_metadata_read_limit"

// Plugin 实现 PDF 格式插件。当前只提供轻量文档信息，正文提取仍不在格式主线内。
type Plugin struct {
	options *format.ParseOptions
}

// NewPlugin 创建 PDF 插件。
func NewPlugin(opts *format.ParseOptions) *Plugin {
	if opts == nil {
		opts = format.DefaultParseOptions()
	}
	return &Plugin{options: opts}
}

// Parser 是旧 extractor 入口的兼容别名。
type Parser = Plugin

// NewParser 创建旧 extractor 兼容入口。
func NewParser(opts *format.ParseOptions) *Parser {
	return NewPlugin(opts)
}

func (p *Plugin) Format() format.FormatType {
	return format.FormatPDF
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	descriptor, ok := format.GetFormatDescriptor(p.Format())
	if ok {
		return descriptor
	}
	return format.FormatDescriptor{
		ID:            "builtin-pdf",
		Format:        p.Format(),
		DataType:      format.FormatDataTypeDocument,
		Layouts:       []string{format.FormatLayoutSingle},
		ProviderHints: []string{format.FormatProviderDocument},
		ContentReaders: []string{
			string(format.ContentReaderRawContent),
			string(format.ContentReaderRangeContent),
		},
	}
}

func (p *Plugin) Capabilities() format.FormatCapability {
	capability, ok := format.GetFormatCapability(p.Format())
	if ok {
		return capability
	}
	return format.FormatCapability{
		Format:        p.Format(),
		DataType:      format.FormatDataTypeDocument,
		Layouts:       []string{format.FormatLayoutSingle},
		ProviderHints: []string{format.FormatProviderDocument},
		ContentReaders: []string{
			string(format.ContentReaderRawContent),
			string(format.ContentReaderRangeContent),
		},
	}
}

func (p *Plugin) DescribeDocument(ctx context.Context, input io.Reader, options *format.ParseOptions) (*format.DocumentInfo, error) {
	metadata, err := p.extractWithOptions(ctx, input, "", 0, options)
	if err != nil {
		return nil, err
	}
	info := &format.DocumentInfo{Format: p.Format()}
	if title, ok := metadata.CustomAttrs["title"].(string); ok {
		info.Title = title
	}
	if metadata.BasicInfo.Size > 0 {
		size := metadata.BasicInfo.Size
		info.SizeBytes = &size
	}
	return info, nil
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

// Extract 实现 FileMetadataExtractor 接口，保留给 Meta 深度扫描兼容链路使用。
func (p *Plugin) Extract(ctx context.Context, input format.ExtractInput) (*format.ExtractedMetadata, error) {
	metadata, err := p.extract(ctx, input.Reader, input.ContentType, input.Size)
	if err != nil {
		return nil, err
	}
	metadata.BasicInfo.LastModified = input.LastModified
	metadata.BasicInfo.ETag = input.ETag
	return metadata, nil
}

func (p *Plugin) extract(ctx context.Context, input io.Reader, contentType string, size int64) (*format.ExtractedMetadata, error) {
	return p.extractWithOptions(ctx, input, contentType, size, nil)
}

func (p *Plugin) extractWithOptions(ctx context.Context, input io.Reader, contentType string, size int64, options *format.ParseOptions) (*format.ExtractedMetadata, error) {
	readLimit := pdfReadLimit(options)

	// 读取 PDF 内容
	content, err := io.ReadAll(io.LimitReader(input, readLimit+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read PDF content: %w", err)
	}
	if int64(len(content)) > readLimit {
		return nil, fmt.Errorf("PDF metadata extraction exceeds limit %d bytes", readLimit)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
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
			FileType:    "PDF",
			Size:        size,
			ContentType: contentType,
		},
		CustomAttrs: customAttrs,
	}, nil
}

func pdfReadLimit(options *format.ParseOptions) int64 {
	if options != nil && options.ExtraParams != nil {
		switch value := options.ExtraParams[readLimitParam].(type) {
		case int:
			if value > 0 {
				return int64(value)
			}
		case int64:
			if value > 0 {
				return value
			}
		case float64:
			if value > 0 {
				return int64(value)
			}
		}
	}
	return defaultReadLimit
}

// SupportedTypes 实现 FileMetadataExtractor 接口。
func (p *Plugin) SupportedTypes() []string {
	return []string{"application/pdf"}
}

// Priority 实现 FileMetadataExtractor 接口。
func (p *Plugin) Priority() int {
	return 100
}

func init() {
	plugin := NewPlugin(nil)
	if err := format.RegisterFormatPlugin(plugin); err != nil {
		panic(err)
	}
	_ = format.RegisterExtractor(plugin)
}
