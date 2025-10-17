// Package officeextractor Office文档元数据提取器插件
package officeextractor

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	sdk "github.com/addp/meta-extractor-sdk"
)

// OfficeMetadata Office文档的类型化元数据
type OfficeMetadata struct {
	DocumentType  string    `json:"document_type"`  // docx, pptx, xlsx
	Title         string    `json:"title"`
	Author        string    `json:"author"`
	Subject       string    `json:"subject"`
	Keywords      []string  `json:"keywords"`
	Description   string    `json:"description"`
	Creator       string    `json:"creator"`
	LastModifiedBy string   `json:"last_modified_by"`
	CreatedDate   time.Time `json:"created_date"`
	ModifiedDate  time.Time `json:"modified_date"`
	Revision      string    `json:"revision"`
	PageCount     int       `json:"page_count"`      // DOCX
	SlideCount    int       `json:"slide_count"`     // PPTX
	SheetCount    int       `json:"sheet_count"`     // XLSX
	WordCount     int       `json:"word_count"`      // DOCX
	CharacterCount int      `json:"character_count"` // DOCX
}

// TypeName 实现 TypedMetadata 接口
func (m *OfficeMetadata) TypeName() string {
	return "office.metadata"
}

// Schema 实现 TypedMetadata 接口
func (m *OfficeMetadata) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"document_type":    map[string]string{"type": "string", "description": "Document type (docx/pptx/xlsx)"},
			"title":            map[string]string{"type": "string", "description": "Document title"},
			"author":           map[string]string{"type": "string", "description": "Document author"},
			"subject":          map[string]string{"type": "string", "description": "Document subject"},
			"keywords":         map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}},
			"description":      map[string]string{"type": "string", "description": "Document description"},
			"creator":          map[string]string{"type": "string", "description": "Creating application"},
			"last_modified_by": map[string]string{"type": "string", "description": "Last modifier"},
			"created_date":     map[string]string{"type": "string", "format": "date-time"},
			"modified_date":    map[string]string{"type": "string", "format": "date-time"},
			"revision":         map[string]string{"type": "string", "description": "Revision number"},
			"page_count":       map[string]string{"type": "integer", "description": "Number of pages (DOCX)"},
			"slide_count":      map[string]string{"type": "integer", "description": "Number of slides (PPTX)"},
			"sheet_count":      map[string]string{"type": "integer", "description": "Number of sheets (XLSX)"},
			"word_count":       map[string]string{"type": "integer", "description": "Word count (DOCX)"},
			"character_count":  map[string]string{"type": "integer", "description": "Character count (DOCX)"},
		},
		"required": []string{"document_type"},
	}
}

// ToMap 实现 TypedMetadata 接口
func (m *OfficeMetadata) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"document_type":     m.DocumentType,
		"title":             m.Title,
		"author":            m.Author,
		"subject":           m.Subject,
		"keywords":          m.Keywords,
		"description":       m.Description,
		"creator":           m.Creator,
		"last_modified_by":  m.LastModifiedBy,
		"created_date":      m.CreatedDate,
		"modified_date":     m.ModifiedDate,
		"revision":          m.Revision,
		"page_count":        m.PageCount,
		"slide_count":       m.SlideCount,
		"sheet_count":       m.SheetCount,
		"word_count":        m.WordCount,
		"character_count":   m.CharacterCount,
	}
}

// FromMap 实现 TypedMetadata 接口
func (m *OfficeMetadata) FromMap(data map[string]interface{}) error {
	if v, ok := data["document_type"].(string); ok {
		m.DocumentType = v
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
	if v, ok := data["description"].(string); ok {
		m.Description = v
	}
	if v, ok := data["creator"].(string); ok {
		m.Creator = v
	}
	if v, ok := data["last_modified_by"].(string); ok {
		m.LastModifiedBy = v
	}
	if v, ok := data["revision"].(string); ok {
		m.Revision = v
	}
	if v, ok := data["page_count"].(float64); ok {
		m.PageCount = int(v)
	}
	if v, ok := data["slide_count"].(float64); ok {
		m.SlideCount = int(v)
	}
	if v, ok := data["sheet_count"].(float64); ok {
		m.SheetCount = int(v)
	}
	if v, ok := data["word_count"].(float64); ok {
		m.WordCount = int(v)
	}
	if v, ok := data["character_count"].(float64); ok {
		m.CharacterCount = int(v)
	}
	return nil
}

// init 函数：注册自定义元数据类型
func init() {
	sdk.RegisterMetadataType(&OfficeMetadata{})
}

// OfficeExtractor Office文档的元数据提取器
type OfficeExtractor struct{}

// SupportedTypes 返回支持的MIME类型
func (e *OfficeExtractor) SupportedTypes() []string {
	return []string{
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document",     // .docx
		"application/vnd.openxmlformats-officedocument.presentationml.presentation",   // .pptx
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",           // .xlsx
		"application/vnd.ms-word.document.macroEnabled.12",                            // .docm
		"application/vnd.ms-powerpoint.presentation.macroEnabled.12",                  // .pptm
		"application/vnd.ms-excel.sheet.macroEnabled.12",                              // .xlsm
	}
}

// Priority 返回优先级
func (e *OfficeExtractor) Priority() int {
	return 55
}

// Extract 提取Office文档元数据
func (e *OfficeExtractor) Extract(ctx context.Context, input sdk.ExtractInput) (*sdk.Metadata, error) {
	// 1. 读取文件内容到内存
	content, err := io.ReadAll(input.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read office document: %w", err)
	}

	// 2. 打开ZIP压缩包（Office文档是ZIP格式）
	zipReader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return nil, fmt.Errorf("failed to open office document as zip: %w", err)
	}

	// 3. 确定文档类型
	docType := e.detectDocumentType(input.ObjectKey, zipReader)

	// 4. 提取元数据
	officeMeta, err := e.extractOfficeMetadata(zipReader, docType)
	if err != nil {
		return nil, fmt.Errorf("failed to extract office metadata: %w", err)
	}

	officeMeta.DocumentType = docType

	// 5. 创建基础元数据
	var description string
	switch docType {
	case "docx":
		description = "Microsoft Word Document"
	case "pptx":
		description = "Microsoft PowerPoint Presentation"
	case "xlsx":
		description = "Microsoft Excel Spreadsheet"
	default:
		description = "Office Document"
	}

	metadata := sdk.NewMetadata(
		filepath.Base(input.ObjectKey),
		description,
		input.Size,
	)

	metadata.BasicInfo.ContentType = input.ContentType
	metadata.BasicInfo.LastModified = input.LastModified
	metadata.BasicInfo.ETag = input.ETag

	// 6. 添加类型化元数据
	metadata.AddTypedMetadata("office_metadata", officeMeta)

	// 7. 添加自定义属性
	metadata.CustomAttrs["document_type"] = docType
	metadata.CustomAttrs["title"] = officeMeta.Title
	metadata.CustomAttrs["author"] = officeMeta.Author
	metadata.CustomAttrs["file_size"] = input.Size
	metadata.CustomAttrs["file_size_human"] = formatFileSize(input.Size)

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

// detectDocumentType 检测文档类型
func (e *OfficeExtractor) detectDocumentType(filename string, zipReader *zip.Reader) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".docx", ".docm":
		return "docx"
	case ".pptx", ".pptm":
		return "pptx"
	case ".xlsx", ".xlsm":
		return "xlsx"
	}

	// 通过ZIP内容判断
	for _, f := range zipReader.File {
		if strings.Contains(f.Name, "word/") {
			return "docx"
		} else if strings.Contains(f.Name, "ppt/") {
			return "pptx"
		} else if strings.Contains(f.Name, "xl/") {
			return "xlsx"
		}
	}

	return "unknown"
}

// extractOfficeMetadata 提取Office元数据
func (e *OfficeExtractor) extractOfficeMetadata(zipReader *zip.Reader, docType string) (*OfficeMetadata, error) {
	meta := &OfficeMetadata{
		Keywords: []string{},
	}

	// 1. 提取核心属性（core.xml）
	if coreProps, err := e.extractCoreProperties(zipReader); err == nil {
		meta.Title = coreProps.Title
		meta.Author = coreProps.Creator
		meta.Subject = coreProps.Subject
		meta.Description = coreProps.Description
		meta.LastModifiedBy = coreProps.LastModifiedBy
		meta.Revision = coreProps.Revision

		if keywords := strings.TrimSpace(coreProps.Keywords); keywords != "" {
			meta.Keywords = strings.Split(keywords, ",")
			for i := range meta.Keywords {
				meta.Keywords[i] = strings.TrimSpace(meta.Keywords[i])
			}
		}

		if !coreProps.Created.IsZero() {
			meta.CreatedDate = coreProps.Created
		}
		if !coreProps.Modified.IsZero() {
			meta.ModifiedDate = coreProps.Modified
		}
	}

	// 2. 提取应用程序属性（app.xml）
	if appProps, err := e.extractAppProperties(zipReader); err == nil {
		meta.Creator = appProps.Application

		switch docType {
		case "docx":
			meta.PageCount = appProps.Pages
			meta.WordCount = appProps.Words
			meta.CharacterCount = appProps.Characters
		case "pptx":
			meta.SlideCount = appProps.Slides
		case "xlsx":
			// Excel的sheet数量需要从workbook.xml读取
			if sheetCount, err := e.countExcelSheets(zipReader); err == nil {
				meta.SheetCount = sheetCount
			}
		}
	}

	return meta, nil
}

// CoreProperties 核心属性结构
type CoreProperties struct {
	XMLName        xml.Name  `xml:"coreProperties"`
	Title          string    `xml:"title"`
	Subject        string    `xml:"subject"`
	Creator        string    `xml:"creator"`
	Keywords       string    `xml:"keywords"`
	Description    string    `xml:"description"`
	LastModifiedBy string    `xml:"lastModifiedBy"`
	Revision       string    `xml:"revision"`
	Created        time.Time `xml:"created"`
	Modified       time.Time `xml:"modified"`
}

// AppProperties 应用程序属性结构
type AppProperties struct {
	XMLName     xml.Name `xml:"Properties"`
	Application string   `xml:"Application"`
	Pages       int      `xml:"Pages"`
	Words       int      `xml:"Words"`
	Characters  int      `xml:"Characters"`
	Slides      int      `xml:"Slides"`
}

// extractCoreProperties 提取核心属性
func (e *OfficeExtractor) extractCoreProperties(zipReader *zip.Reader) (*CoreProperties, error) {
	for _, f := range zipReader.File {
		if f.Name == "docProps/core.xml" {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()

			content, err := io.ReadAll(rc)
			if err != nil {
				return nil, err
			}

			var props CoreProperties
			if err := xml.Unmarshal(content, &props); err != nil {
				return nil, err
			}

			return &props, nil
		}
	}

	return nil, fmt.Errorf("core.xml not found")
}

// extractAppProperties 提取应用程序属性
func (e *OfficeExtractor) extractAppProperties(zipReader *zip.Reader) (*AppProperties, error) {
	for _, f := range zipReader.File {
		if f.Name == "docProps/app.xml" {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()

			content, err := io.ReadAll(rc)
			if err != nil {
				return nil, err
			}

			var props AppProperties
			if err := xml.Unmarshal(content, &props); err != nil {
				return nil, err
			}

			return &props, nil
		}
	}

	return nil, fmt.Errorf("app.xml not found")
}

// countExcelSheets 统计Excel工作表数量
func (e *OfficeExtractor) countExcelSheets(zipReader *zip.Reader) (int, error) {
	for _, f := range zipReader.File {
		if f.Name == "xl/workbook.xml" {
			rc, err := f.Open()
			if err != nil {
				return 0, err
			}
			defer rc.Close()

			content, err := io.ReadAll(rc)
			if err != nil {
				return 0, err
			}

			// 简单统计<sheet>标签数量
			count := strings.Count(string(content), "<sheet ")
			return count, nil
		}
	}

	return 0, fmt.Errorf("workbook.xml not found")
}

// GetExtractor 返回提取器实例（供ADDP加载使用）
func GetExtractor() sdk.MetadataExtractor {
	return &OfficeExtractor{}
}
