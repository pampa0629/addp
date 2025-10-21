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
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	sdk "github.com/addp/meta-extractor-sdk"
	"github.com/xuri/excelize/v2"
)

const (
	documentPlainTextLimit        = 20000
	documentPlainTextPreviewLimit = 400
)

// OfficeMetadata Office文档的类型化元数据
type OfficeMetadata struct {
	DocumentType   string    `json:"document_type"` // docx, pptx, xlsx
	Title          string    `json:"title"`
	Author         string    `json:"author"`
	Subject        string    `json:"subject"`
	Keywords       []string  `json:"keywords"`
	Description    string    `json:"description"`
	Creator        string    `json:"creator"`
	LastModifiedBy string    `json:"last_modified_by"`
	CreatedDate    time.Time `json:"created_date"`
	ModifiedDate   time.Time `json:"modified_date"`
	Revision       string    `json:"revision"`
	PageCount      int       `json:"page_count"`      // DOCX
	SlideCount     int       `json:"slide_count"`     // PPTX
	SheetCount     int       `json:"sheet_count"`     // XLSX
	WordCount      int       `json:"word_count"`      // DOCX
	CharacterCount int       `json:"character_count"` // DOCX
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
		"document_type":    m.DocumentType,
		"title":            m.Title,
		"author":           m.Author,
		"subject":          m.Subject,
		"keywords":         m.Keywords,
		"description":      m.Description,
		"creator":          m.Creator,
		"last_modified_by": m.LastModifiedBy,
		"created_date":     m.CreatedDate,
		"modified_date":    m.ModifiedDate,
		"revision":         m.Revision,
		"page_count":       m.PageCount,
		"slide_count":      m.SlideCount,
		"sheet_count":      m.SheetCount,
		"word_count":       m.WordCount,
		"character_count":  m.CharacterCount,
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
	sdk.RegisterMetadataType(&ExcelMetadata{})
}

// OfficeExtractor Office文档的元数据提取器
type OfficeExtractor struct{}

// ExcelMetadata Excel文件的类型化元数据
type ExcelMetadata struct {
	SheetCount   int              `json:"sheet_count"`
	DefaultSheet string           `json:"default_sheet"`
	Sheets       []ExcelSheetInfo `json:"sheets"`
}

const (
	excelSampleRowLimit     = 20
	excelTypeSampleRowLimit = 100
	excelReadRowLimit       = 200
)

// ExcelSheetInfo 单个工作表的元数据信息
type ExcelSheetInfo struct {
	Name          string                   `json:"name"`
	Index         int                      `json:"index"`
	RowCount      int                      `json:"row_count"`
	ColumnCount   int                      `json:"column_count"`
	HasHeader     bool                     `json:"has_header"`
	Headers       []string                 `json:"headers"`
	ColumnTypes   []string                 `json:"column_types"`
	SampleRows    []map[string]interface{} `json:"sample_rows,omitempty"`
	RowsTruncated bool                     `json:"rows_truncated"`
}

// TypeName 实现 TypedMetadata 接口
func (m *ExcelMetadata) TypeName() string {
	return "excel.metadata"
}

// Schema 实现 TypedMetadata 接口
func (m *ExcelMetadata) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"sheet_count":   map[string]string{"type": "integer", "description": "Number of sheets"},
			"default_sheet": map[string]string{"type": "string", "description": "Default active sheet"},
			"sheets": map[string]interface{}{
				"type":  "array",
				"items": excelSheetSchema(),
			},
		},
		"required": []string{"sheet_count", "sheets"},
	}
}

// ToMap 实现 TypedMetadata 接口
func (m *ExcelMetadata) ToMap() map[string]interface{} {
	sheets := make([]map[string]interface{}, 0, len(m.Sheets))
	for _, sheet := range m.Sheets {
		sheets = append(sheets, sheet.ToMap())
	}
	return map[string]interface{}{
		"sheet_count":   m.SheetCount,
		"default_sheet": m.DefaultSheet,
		"sheets":        sheets,
	}
}

// FromMap 实现 TypedMetadata 接口
func (m *ExcelMetadata) FromMap(data map[string]interface{}) error {
	if v, ok := data["sheet_count"].(float64); ok {
		m.SheetCount = int(v)
	}
	if v, ok := data["default_sheet"].(string); ok {
		m.DefaultSheet = v
	}
	if rawSheets, ok := data["sheets"].([]interface{}); ok {
		m.Sheets = make([]ExcelSheetInfo, 0, len(rawSheets))
		for _, raw := range rawSheets {
			if sheetMap, ok := raw.(map[string]interface{}); ok {
				var sheet ExcelSheetInfo
				sheet.FromMap(sheetMap)
				m.Sheets = append(m.Sheets, sheet)
			}
		}
	}
	return nil
}

// ToMap 序列化 ExcelSheetInfo
func (s *ExcelSheetInfo) ToMap() map[string]interface{} {
	rows := make([]map[string]interface{}, 0, len(s.SampleRows))
	for _, row := range s.SampleRows {
		rows = append(rows, row)
	}
	return map[string]interface{}{
		"name":           s.Name,
		"index":          s.Index,
		"row_count":      s.RowCount,
		"column_count":   s.ColumnCount,
		"has_header":     s.HasHeader,
		"headers":        s.Headers,
		"column_types":   s.ColumnTypes,
		"sample_rows":    rows,
		"rows_truncated": s.RowsTruncated,
	}
}

// FromMap 反序列化 ExcelSheetInfo
func (s *ExcelSheetInfo) FromMap(data map[string]interface{}) {
	if v, ok := data["name"].(string); ok {
		s.Name = v
	}
	if v, ok := data["index"].(float64); ok {
		s.Index = int(v)
	}
	if v, ok := data["row_count"].(float64); ok {
		s.RowCount = int(v)
	}
	if v, ok := data["column_count"].(float64); ok {
		s.ColumnCount = int(v)
	}
	if v, ok := data["has_header"].(bool); ok {
		s.HasHeader = v
	}
	if rawHeaders, ok := data["headers"].([]interface{}); ok {
		s.Headers = make([]string, 0, len(rawHeaders))
		for _, header := range rawHeaders {
			if val, ok := header.(string); ok {
				s.Headers = append(s.Headers, val)
			}
		}
	}
	if rawTypes, ok := data["column_types"].([]interface{}); ok {
		s.ColumnTypes = make([]string, 0, len(rawTypes))
		for _, typ := range rawTypes {
			if val, ok := typ.(string); ok {
				s.ColumnTypes = append(s.ColumnTypes, val)
			}
		}
	}
	if rawRows, ok := data["sample_rows"].([]interface{}); ok {
		s.SampleRows = make([]map[string]interface{}, 0, len(rawRows))
		for _, raw := range rawRows {
			if rowMap, ok := raw.(map[string]interface{}); ok {
				s.SampleRows = append(s.SampleRows, rowMap)
			}
		}
	}
	if v, ok := data["rows_truncated"].(bool); ok {
		s.RowsTruncated = v
	}
}

// SupportedTypes 返回支持的MIME类型
func (e *OfficeExtractor) SupportedTypes() []string {
	return []string{
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document",   // .docx
		"application/vnd.openxmlformats-officedocument.presentationml.presentation", // .pptx
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",         // .xlsx
		"application/vnd.ms-word.document.macroEnabled.12",                          // .docm
		"application/vnd.ms-powerpoint.presentation.macroEnabled.12",                // .pptm
		"application/vnd.ms-excel.sheet.macroEnabled.12",                            // .xlsm
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

	if docType == "docx" {
		if plainText, err := extractDocxPlainText(content); err == nil {
			plainText = strings.TrimSpace(plainText)
			if plainText != "" {
				trimmed := truncateRunes(plainText, documentPlainTextLimit)
				metadata.CustomAttrs["plain_text"] = trimmed
				metadata.CustomAttrs["plain_text_preview"] = truncateRunes(trimmed, documentPlainTextPreviewLimit)
			}
		}
	}

	if docType == "pptx" {
		if plainText, err := extractPptxPlainText(zipReader); err == nil {
			plainText = strings.TrimSpace(plainText)
			if plainText != "" {
				trimmed := truncateRunes(plainText, documentPlainTextLimit)
				metadata.CustomAttrs["plain_text"] = trimmed
				metadata.CustomAttrs["plain_text_preview"] = truncateRunes(trimmed, documentPlainTextPreviewLimit)
			}
		}
	}

	// 8. 若为Excel，提取工作表结构与示例数据
	if docType == "xlsx" {
		excelMeta, schemaInfo, previewData, err := e.extractExcelDetails(content)
		if err != nil {
			metadata.CustomAttrs["excel_error"] = err.Error()
		} else {
			metadata.AddTypedMetadata("excel_metadata", excelMeta)
			if schemaInfo != nil {
				metadata.SchemaInfo = schemaInfo
			}
			if previewData != nil {
				if text := excelPreviewToPlainText(previewData); strings.TrimSpace(text) != "" {
					trimmed := truncateRunes(strings.TrimSpace(text), documentPlainTextLimit)
					metadata.CustomAttrs["plain_text"] = trimmed
					metadata.CustomAttrs["plain_text_preview"] = truncateRunes(trimmed, documentPlainTextPreviewLimit)
				}
				metadata.PreviewData = previewData
			}
			metadata.CustomAttrs["sheet_count"] = excelMeta.SheetCount
			metadata.CustomAttrs["default_sheet"] = excelMeta.DefaultSheet
		}
	}

	return metadata, nil
}

func extractDocxPlainText(content []byte) (string, error) {
	reader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return "", err
	}

	for _, file := range reader.File {
		if file.Name != "word/document.xml" {
			continue
		}

		rc, err := file.Open()
		if err != nil {
			return "", err
		}
		defer rc.Close()

		text, err := parseDocxDocumentXML(rc)
		if err != nil {
			return "", err
		}
		return text, nil
	}

	return "", fmt.Errorf("word/document.xml not found")
}

func parseDocxDocumentXML(r io.Reader) (string, error) {
	decoder := xml.NewDecoder(r)
	var builder strings.Builder
	var inText bool

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}

		switch t := token.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "t":
				inText = true
			case "tab":
				builder.WriteRune('\t')
			case "br":
				builder.WriteRune('\n')
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "p":
				builder.WriteRune('\n')
			}
		case xml.CharData:
			if inText {
				builder.WriteString(strings.ReplaceAll(string(t), "\r", ""))
			}
		}
	}

	return builder.String(), nil
}

func extractPptxPlainText(zipReader *zip.Reader) (string, error) {
	slideFiles := make([]*zip.File, 0)
	noteFiles := make([]*zip.File, 0)
	for _, file := range zipReader.File {
		name := file.Name
		if strings.HasPrefix(name, "ppt/slides/slide") && strings.HasSuffix(name, ".xml") {
			slideFiles = append(slideFiles, file)
		} else if strings.HasPrefix(name, "ppt/notesSlides/notesSlide") && strings.HasSuffix(name, ".xml") {
			noteFiles = append(noteFiles, file)
		}
	}

	sort.Slice(slideFiles, func(i, j int) bool { return slideFiles[i].Name < slideFiles[j].Name })
	sort.Slice(noteFiles, func(i, j int) bool { return noteFiles[i].Name < noteFiles[j].Name })

	var builder strings.Builder
	appendText := func(text string) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		if builder.Len() > 0 {
			builder.WriteString("\n\n")
		}
		builder.WriteString(text)
	}

	for _, file := range slideFiles {
		rc, err := file.Open()
		if err != nil {
			continue
		}
		text, err := parsePptSlideText(rc)
		_ = rc.Close()
		if err == nil {
			appendText(text)
		}
	}

	for _, file := range noteFiles {
		rc, err := file.Open()
		if err != nil {
			continue
		}
		text, err := parsePptSlideText(rc)
		_ = rc.Close()
		if err == nil {
			appendText(text)
		}
	}

	if builder.Len() == 0 {
		return "", fmt.Errorf("no slide text found")
	}

	return builder.String(), nil
}

func parsePptSlideText(r io.Reader) (string, error) {
	decoder := xml.NewDecoder(r)
	var builder strings.Builder
	var inText bool

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}

		switch t := token.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "t":
				inText = true
			case "br":
				builder.WriteRune('\n')
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "p":
				builder.WriteRune('\n')
			}
		case xml.CharData:
			if inText {
				builder.WriteString(strings.ReplaceAll(string(t), "\r", ""))
			}
		}
	}

	return builder.String(), nil
}

func truncateRunes(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
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

// excelSheetSchema 返回Excel工作表的schema定义
func excelSheetSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]string{"type": "string", "description": "Sheet name"},
			"index": map[string]string{
				"type":        "integer",
				"description": "Sheet index (zero-based)",
			},
			"row_count": map[string]string{
				"type":        "integer",
				"description": "Approximate number of data rows (excluding header)",
			},
			"column_count": map[string]string{
				"type":        "integer",
				"description": "Number of columns",
			},
			"has_header": map[string]string{
				"type":        "boolean",
				"description": "Whether the first row is treated as header",
			},
			"headers": map[string]interface{}{
				"type": "array",
				"items": map[string]string{
					"type": "string",
				},
			},
			"column_types": map[string]interface{}{
				"type": "array",
				"items": map[string]string{
					"type": "string",
				},
			},
			"sample_rows": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type":                 "object",
					"additionalProperties": map[string]string{"type": "string"},
				},
			},
			"rows_truncated": map[string]string{
				"type":        "boolean",
				"description": "Whether sample rows are truncated",
			},
		},
	}
}

// extractExcelDetails 解析Excel工作簿，提取工作表结构和样例数据
func (e *OfficeExtractor) extractExcelDetails(content []byte) (*ExcelMetadata, *sdk.SchemaMetadata, map[string]interface{}, error) {
	file, err := excelize.OpenReader(bytes.NewReader(content))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("打开Excel失败: %w", err)
	}
	defer file.Close()

	sheetNames := file.GetSheetList()
	if len(sheetNames) == 0 {
		emptyMeta := &ExcelMetadata{
			SheetCount:   0,
			DefaultSheet: "",
			Sheets:       []ExcelSheetInfo{},
		}
		preview := map[string]interface{}{
			"kind":         "excel",
			"sheet_count":  0,
			"active_sheet": "",
			"sheets":       []map[string]interface{}{},
		}
		return emptyMeta, nil, preview, nil
	}

	defaultIdx := file.GetActiveSheetIndex()
	if defaultIdx < 0 || defaultIdx >= len(sheetNames) {
		defaultIdx = 0
	}
	defaultSheet := sheetNames[defaultIdx]

	meta := &ExcelMetadata{
		SheetCount:   len(sheetNames),
		DefaultSheet: defaultSheet,
		Sheets:       make([]ExcelSheetInfo, 0, len(sheetNames)),
	}

	previewSheets := make([]map[string]interface{}, 0, len(sheetNames))
	var schemaInfo *sdk.SchemaMetadata

	for idx, sheetName := range sheetNames {
		sheetInfo, sheetPreview, sheetSchema := extractExcelSheet(file, sheetName, idx, sheetName == defaultSheet)
		meta.Sheets = append(meta.Sheets, sheetInfo)
		previewSheets = append(previewSheets, sheetPreview)

		// 选取默认工作表的schema信息
		if sheetSchema != nil && schemaInfo == nil {
			schemaInfo = sheetSchema
		}
	}

	if schemaInfo == nil && len(meta.Sheets) > 0 {
		// 回退到第一个工作表
		schemaInfo = schemaFromSheet(meta.Sheets[0])
	}

	preview := map[string]interface{}{
		"kind":          "excel",
		"sheet_count":   len(sheetNames),
		"active_sheet":  defaultSheet,
		"sheets":        previewSheets,
		"default_sheet": defaultSheet,
	}

	return meta, schemaInfo, preview, nil
}

func excelPreviewToPlainText(preview map[string]interface{}) string {
	if preview == nil {
		return ""
	}

	sheetsValue, ok := preview["sheets"]
	if !ok {
		return ""
	}

	sheets := coerceSliceOfMap(sheetsValue)
	if len(sheets) == 0 {
		return ""
	}

	const maxSheets = 5
	const maxRows = 50

	var builder strings.Builder

	for idx, sheet := range sheets {
		if idx >= maxSheets {
			break
		}
		headers := toStringSlice(sheet["headers"])
		rows := coerceSliceOfMap(sheet["rows"])
		name := toString(sheet["name"])
		if name == "" {
			name = fmt.Sprintf("Sheet%d", idx+1)
		}

		builder.WriteString("Sheet: ")
		builder.WriteString(name)
		builder.WriteString("\n")

		if len(headers) == 0 && len(rows) > 0 {
			headers = mapKeys(rows[0])
		}

		for rowIdx, row := range rows {
			if rowIdx >= maxRows {
				break
			}
			values := make([]string, 0, len(headers))
			if len(headers) == 0 {
				headers = mapKeys(row)
			}
			for _, header := range headers {
				val := toString(row[header])
				values = append(values, val)
			}
			builder.WriteString(strings.Join(values, "\t"))
			builder.WriteByte('\n')
		}

		builder.WriteByte('\n')
	}

	return builder.String()
}

// extractExcelSheet 解析单个工作表
func extractExcelSheet(file *excelize.File, sheetName string, index int, isDefault bool) (ExcelSheetInfo, map[string]interface{}, *sdk.SchemaMetadata) {
	info := ExcelSheetInfo{
		Name:        sheetName,
		Index:       index,
		ColumnTypes: []string{},
		SampleRows:  []map[string]interface{}{},
	}

	dimCols, dimRows := sheetDimensions(file, sheetName)

	rowsIter, err := file.Rows(sheetName)
	if err != nil {
		info.ColumnCount = dimCols
		info.RowCount = maxInt(0, dimRows)
		return finalizeExcelSheet(info, []string{}, [][]string{}, dimCols, dimRows, false), excelPreviewFromSheet(info), schemaFromSheet(info)
	}
	defer rowsIter.Close()

	headerCandidate := []string{}
	rawRows := make([][]string, 0, excelReadRowLimit)
	maxColumns := dimCols
	rowIndex := 0
	readLimit := excelReadRowLimit

	for rowsIter.Next() {
		rowIndex++
		if readLimit > 0 && rowIndex > readLimit {
			break
		}

		raw, err := rowsIter.Columns()
		if err != nil {
			continue
		}

		row := trimStringSlice(raw)
		if len(row) > maxColumns {
			maxColumns = len(row)
		}

		if rowIndex == 1 {
			headerCandidate = row
			continue
		}

		rawRows = append(rawRows, row)
	}

	// 没有数据行时，仍需考虑只有表头的情况
	hasHeader := looksLikeHeaderRow(headerCandidate)
	info.HasHeader = hasHeader

	if !hasHeader && len(headerCandidate) > 0 {
		// 无表头，则表头行作为第一条数据
		rawRows = append([][]string{headerCandidate}, rawRows...)
	}

	if maxColumns == 0 && len(headerCandidate) > 0 {
		maxColumns = len(headerCandidate)
	}

	headers := buildHeaders(headerCandidate, maxColumns, hasHeader)
	normalizedRows := normalizeRows(rawRows, maxColumns)
	info.Headers = headers
	info.ColumnCount = maxColumns

	estimatedRows := estimateRowCount(dimRows, len(normalizedRows), hasHeader)
	info.RowCount = estimatedRows

	columnTypes := inferExcelColumnTypes(normalizedRows, maxColumns, excelTypeSampleRowLimit)
	info.ColumnTypes = columnTypes

	sampleRows := buildSampleRows(headers, normalizedRows, excelSampleRowLimit)
	info.SampleRows = sampleRows
	if estimatedRows > len(sampleRows) {
		info.RowsTruncated = true
	}

	finalInfo := finalizeExcelSheet(info, headerCandidate, normalizedRows, maxColumns, dimRows, hasHeader)
	preview := excelPreviewFromSheet(finalInfo)
	var schema *sdk.SchemaMetadata
	if isDefault || len(headers) > 0 {
		schema = schemaFromSheet(finalInfo)
	}

	return finalInfo, preview, schema
}

// finalizeExcelSheet 对工作表元数据做收尾处理
func finalizeExcelSheet(info ExcelSheetInfo, header []string, rows [][]string, columnCount, dimensionRows int, hasHeader bool) ExcelSheetInfo {
	if info.ColumnCount == 0 {
		info.ColumnCount = columnCount
	}
	if info.RowCount == 0 {
		estimated := estimateRowCount(dimensionRows, len(rows), hasHeader)
		info.RowCount = estimated
	}
	if len(info.Headers) == 0 {
		info.Headers = buildHeaders(header, info.ColumnCount, hasHeader)
	}
	if len(info.ColumnTypes) == 0 && info.ColumnCount > 0 {
		info.ColumnTypes = inferExcelColumnTypes(rows, info.ColumnCount, excelTypeSampleRowLimit)
	}
	return info
}

// schemaFromSheet 将ExcelSheetInfo转换为SchemaMetadata
func schemaFromSheet(sheet ExcelSheetInfo) *sdk.SchemaMetadata {
	if sheet.ColumnCount == 0 || len(sheet.Headers) == 0 {
		return nil
	}
	columns := make([]sdk.ColumnInfo, 0, sheet.ColumnCount)
	for idx, header := range sheet.Headers {
		colType := "string"
		if idx < len(sheet.ColumnTypes) && sheet.ColumnTypes[idx] != "" {
			colType = sheet.ColumnTypes[idx]
		}
		columns = append(columns, sdk.ColumnInfo{
			Name:     header,
			Type:     colType,
			Nullable: true,
		})
	}

	sampleData := sheet.SampleRows
	if len(sampleData) > excelSampleRowLimit {
		sampleData = sampleData[:excelSampleRowLimit]
	}

	return &sdk.SchemaMetadata{
		Columns:    columns,
		RowCount:   int64(sheet.RowCount),
		SampleData: sampleData,
		Extra: map[string]interface{}{
			"sheet_name": sheet.Name,
			"has_header": sheet.HasHeader,
		},
	}
}

// excelPreviewFromSheet 构建预览结构
func excelPreviewFromSheet(sheet ExcelSheetInfo) map[string]interface{} {
	rows := make([]map[string]interface{}, 0, len(sheet.SampleRows))
	for _, row := range sheet.SampleRows {
		rows = append(rows, row)
	}

	return map[string]interface{}{
		"name":           sheet.Name,
		"index":          sheet.Index,
		"row_count":      sheet.RowCount,
		"column_count":   sheet.ColumnCount,
		"has_header":     sheet.HasHeader,
		"headers":        sheet.Headers,
		"column_types":   sheet.ColumnTypes,
		"rows":           rows,
		"rows_truncated": sheet.RowsTruncated,
	}
}

func coerceSliceOfMap(value interface{}) []map[string]interface{} {
	switch v := value.(type) {
	case []map[string]interface{}:
		return v
	case []interface{}:
		result := make([]map[string]interface{}, 0, len(v))
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				result = append(result, m)
			}
		}
		return result
	default:
		return nil
	}
}

func toStringSlice(value interface{}) []string {
	switch v := value.(type) {
	case []string:
		return v
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, item := range v {
			result = append(result, toString(item))
		}
		return result
	default:
		return nil
	}
}

func toString(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	case []byte:
		return string(v)
	case float64:
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%f", v), "0"), ".")
	case int:
		return fmt.Sprintf("%d", v)
	case int64:
		return fmt.Sprintf("%d", v)
	case uint:
		return fmt.Sprintf("%d", v)
	case bool:
		if v {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", v)
	}
}

func mapKeys(m map[string]interface{}) []string {
	if m == nil {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// sheetDimensions 解析工作表的维度信息
func sheetDimensions(file *excelize.File, sheetName string) (columns int, rows int) {
	dimension, err := file.GetSheetDimension(sheetName)
	if err != nil || dimension == "" {
		return 0, 0
	}

	parts := strings.Split(dimension, ":")
	if len(parts) == 1 {
		col, row, err := excelize.CellNameToCoordinates(parts[0])
		if err != nil {
			return 0, 0
		}
		return col, row
	}

	startCol, startRow, err := excelize.CellNameToCoordinates(parts[0])
	if err != nil {
		return 0, 0
	}
	endCol, endRow, err := excelize.CellNameToCoordinates(parts[1])
	if err != nil {
		return 0, 0
	}

	if endCol < startCol || endRow < startRow {
		return 0, 0
	}

	return endCol - startCol + 1, endRow - startRow + 1
}

// buildHeaders 构造表头
func buildHeaders(candidate []string, columnCount int, hasHeader bool) []string {
	if columnCount <= 0 {
		return []string{}
	}
	headers := make([]string, columnCount)
	used := make(map[string]int)
	for i := 0; i < columnCount; i++ {
		var name string
		if hasHeader && i < len(candidate) {
			name = strings.TrimSpace(candidate[i])
		}
		if name == "" {
			name = fmt.Sprintf("Column%d", i+1)
		}
		lower := strings.ToLower(name)
		if count, exists := used[lower]; exists {
			count++
			used[lower] = count
			name = fmt.Sprintf("%s_%d", name, count)
		} else {
			used[lower] = 1
		}
		headers[i] = name
	}
	return headers
}

// normalizeRows 将数据行补齐到统一长度
func normalizeRows(rows [][]string, columnCount int) [][]string {
	if columnCount <= 0 {
		return rows
	}
	normalized := make([][]string, len(rows))
	for i, row := range rows {
		normalized[i] = padRow(row, columnCount)
	}
	return normalized
}

// padRow 补齐单行数据
func padRow(row []string, columnCount int) []string {
	padded := make([]string, columnCount)
	for i := 0; i < columnCount; i++ {
		if i < len(row) {
			padded[i] = strings.TrimSpace(row[i])
		} else {
			padded[i] = ""
		}
	}
	return padded
}

// buildSampleRows 构造示例数据
func buildSampleRows(headers []string, rows [][]string, limit int) []map[string]interface{} {
	if len(headers) == 0 || len(rows) == 0 {
		return []map[string]interface{}{}
	}

	max := len(rows)
	if limit > 0 && limit < max {
		max = limit
	}

	result := make([]map[string]interface{}, 0, max)
	for i := 0; i < max; i++ {
		row := rows[i]
		entry := make(map[string]interface{}, len(headers))
		for j, header := range headers {
			if j < len(row) {
				entry[header] = row[j]
			} else {
				entry[header] = ""
			}
		}
		result = append(result, entry)
	}
	return result
}

// inferExcelColumnTypes 推断列类型
func inferExcelColumnTypes(rows [][]string, columnCount int, sampleLimit int) []string {
	if columnCount <= 0 {
		return []string{}
	}

	samples := make([][]string, columnCount)
	for _, row := range rows {
		for col := 0; col < columnCount; col++ {
			if col < len(row) {
				if sampleLimit <= 0 || len(samples[col]) < sampleLimit {
					samples[col] = append(samples[col], row[col])
				}
			}
		}
	}

	columnTypes := make([]string, columnCount)
	for i := 0; i < columnCount; i++ {
		columnTypes[i] = inferColumnType(samples[i])
	}
	return columnTypes
}

// inferColumnType 根据示例值推断列类型
func inferColumnType(values []string) string {
	if len(values) == 0 {
		return "string"
	}

	var intCount, floatCount, boolCount, dateCount, nullCount int

	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" || strings.EqualFold(value, "null") || strings.EqualFold(value, "na") {
			nullCount++
			continue
		}
		if isBool(value) {
			boolCount++
			continue
		}
		if isInteger(value) {
			intCount++
			continue
		}
		if isFloat(value) {
			floatCount++
			continue
		}
		if isDate(value) {
			dateCount++
			continue
		}
	}

	nonNull := len(values) - nullCount
	if nonNull <= 0 {
		return "string"
	}

	if boolCount == nonNull {
		return "boolean"
	}
	if dateCount >= nonNull/2 && dateCount > 0 {
		return "date"
	}
	if floatCount+intCount == nonNull {
		if floatCount > 0 {
			return "number"
		}
		return "integer"
	}

	return "string"
}

// looksLikeHeaderRow 简单判断首行是否为表头
func looksLikeHeaderRow(row []string) bool {
	if len(row) == 0 {
		return false
	}
	hasNonNumeric := false
	for _, cell := range row {
		value := strings.TrimSpace(cell)
		if value == "" {
			return false
		}
		if !isNumeric(value) {
			hasNonNumeric = true
		}
	}
	return hasNonNumeric
}

// estimateRowCount 估算数据行数（排除表头）
func estimateRowCount(dimensionRows int, loadedRows int, hasHeader bool) int {
	if dimensionRows <= 0 {
		if hasHeader && loadedRows > 0 {
			return maxInt(0, loadedRows-1)
		}
		return loadedRows
	}
	if hasHeader {
		if dimensionRows > 0 {
			dimensionRows--
		}
	}
	if dimensionRows < loadedRows {
		return loadedRows
	}
	return dimensionRows
}

// trimStringSlice 去除元素两端空格
func trimStringSlice(values []string) []string {
	result := make([]string, len(values))
	for i, v := range values {
		result[i] = strings.TrimSpace(v)
	}
	return result
}

func isNumeric(value string) bool {
	if value == "" {
		return false
	}
	return isInteger(value) || isFloat(value)
}

func isInteger(value string) bool {
	_, err := strconv.ParseInt(value, 10, 64)
	return err == nil
}

var floatPattern = regexp.MustCompile(`^[+-]?(\d+\.\d+|\.\d+|\d+\.)$`)

func isFloat(value string) bool {
	if floatPattern.MatchString(value) {
		return true
	}
	_, err := strconv.ParseFloat(value, 64)
	return err == nil
}

func isBool(value string) bool {
	switch strings.ToLower(value) {
	case "true", "false", "yes", "no", "y", "n", "1", "0":
		return true
	default:
		return false
	}
}

var dateLayouts = []string{
	time.RFC3339,
	"2006-01-02",
	"2006/01/02",
	"2006-1-2",
	"02-01-2006",
	"02/01/2006",
	"2006-01-02 15:04:05",
	"2006/01/02 15:04:05",
	"01/02/2006",
	"1/2/2006",
	"02-Jan-2006",
	"02-Jan-06",
}

func isDate(value string) bool {
	if value == "" {
		return false
	}
	for _, layout := range dateLayouts {
		if _, err := time.Parse(layout, value); err == nil {
			return true
		}
	}
	return false
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// GetExtractor 返回提取器实例（供ADDP加载使用）
func GetExtractor() sdk.MetadataExtractor {
	return &OfficeExtractor{}
}
