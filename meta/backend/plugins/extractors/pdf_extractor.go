package extractors

import (
	"github.com/addp/common/format"
	"context"
	"fmt"
	"path/filepath"

	pdfParser "github.com/addp/common/format/pdf"
)

// DocumentMetadata 文档元数据（本地定义）
type DocumentMetadata struct {
	Title     string   `json:"title"`
	Author    string   `json:"author"`
	Subject   string   `json:"subject"`
	Keywords  []string `json:"keywords"`
	Creator   string   `json:"creator"`
	Producer  string   `json:"producer"`
	PageCount int      `json:"page_count"`
}

// PDFExtractor PDF文件的元数据提取器
// 使用共享的 PDF parser 避免重复代码
type PDFExtractor struct{}

func (e *PDFExtractor) SupportedTypes() []string {
	return []string{
		"application/pdf",
	}
}

func (e *PDFExtractor) Priority() int {
	return 80
}

func (e *PDFExtractor) Extract(ctx context.Context, input format.ExtractInput) (*format.ExtractedMetadata, error) {
	// 1. 创建共享 PDF parser
	parser := pdfParser.NewParser(nil)

	// 2. 提取元数据
	pdfMeta, err := parser.ExtractMetadata(ctx, input.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to extract PDF metadata: %w", err)
	}

	// 3. 构建基础元数据
	version := "unknown"
	if v, ok := pdfMeta["version"].(string); ok {
		version = v
	}

	metadata := &format.ExtractedMetadata{
		BasicInfo: format.BasicMetadata{
			FileName:     filepath.Base(input.ObjectKey),
			FileType:     fmt.Sprintf("PDF (%s)", version),
			Size:         input.Size,
			ContentType:  input.ContentType,
			LastModified: input.LastModified,
			ETag:         input.ETag,
		},
		CustomAttrs: make(map[string]interface{}),
	}

	// 4. 添加 PDF 专用元数据（直接使用解析器返回的元数据）
	metadata.CustomAttrs["pdf_metadata"] = pdfMeta

	// 5. 构建文档元数据结构（兼容旧代码）
	pageCount := 0
	if pc, ok := pdfMeta["page_count"].(int); ok {
		pageCount = pc
	}

	title := ""
	if t, ok := pdfMeta["title"].(string); ok {
		title = t
	}

	author := ""
	if a, ok := pdfMeta["author"].(string); ok {
		author = a
	}

	subject := ""
	if s, ok := pdfMeta["subject"].(string); ok {
		subject = s
	}

	creator := ""
	if c, ok := pdfMeta["creator"].(string); ok {
		creator = c
	}

	producer := ""
	if p, ok := pdfMeta["producer"].(string); ok {
		producer = p
	}

	keywords := []string{}
	if kw, ok := pdfMeta["keywords"].([]string); ok {
		keywords = kw
	}

	metadata.CustomAttrs["document_metadata"] = &DocumentMetadata{
		Title:     title,
		Author:    author,
		Subject:   subject,
		Keywords:  keywords,
		Creator:   creator,
		Producer:  producer,
		PageCount: pageCount,
	}

	return metadata, nil
}
