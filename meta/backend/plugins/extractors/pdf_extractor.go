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

	// 2. 构建 ObjectBasicInfo
	basicInfo := format.ObjectBasicInfo{
		Key:         input.ObjectKey,
		SizeBytes:   input.Size,
		ContentType: input.ContentType,
		ETag:        input.ETag,
		ModifiedAt:  input.LastModified,
	}

	// 3. 使用新接口提取 ObjectInfo
	objectInfo, err := parser.ParseObjectInfo(ctx, input.Reader, basicInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to extract PDF metadata: %w", err)
	}

	// 4. 从 ObjectInfo 中提取 PDFInfo
	var pdfInfo *format.PDFInfo
	for _, ext := range objectInfo.Extensions {
		if ext.ExtensionType() == "pdf" {
			if pdf, ok := ext.(*format.PDFInfo); ok {
				pdfInfo = pdf
				break
			}
		}
	}

	if pdfInfo == nil {
		return nil, fmt.Errorf("no PDF info in object")
	}

	// 5. 构建基础元数据
	version := "unknown"
	if pdfInfo.PageCount > 0 {
		version = "PDF"
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

	// 6. 添加 PDF 专用元数据
	metadata.CustomAttrs["pdf_metadata"] = map[string]interface{}{
		"page_count": pdfInfo.PageCount,
		"title":      pdfInfo.Title,
		"author":     pdfInfo.Author,
		"subject":    pdfInfo.Subject,
		"creator":    pdfInfo.Creator,
		"producer":   pdfInfo.Producer,
		"encrypted":  pdfInfo.Encrypted,
	}

	// 7. 构建文档元数据结构（兼容旧代码）
	metadata.CustomAttrs["document_metadata"] = &DocumentMetadata{
		Title:     pdfInfo.Title,
		Author:    pdfInfo.Author,
		Subject:   pdfInfo.Subject,
		Keywords:  []string{}, // PDFInfo 中没有 keywords，使用空数组
		Creator:   pdfInfo.Creator,
		Producer:  pdfInfo.Producer,
		PageCount: pdfInfo.PageCount,
	}

	return metadata, nil
}
