package excel

import (
	"context"
	"fmt"
	"io"

	"github.com/addp/common/format"
	"github.com/xuri/excelize/v2"
)

// Parser 实现 Excel 格式的解析器
type Parser struct {
	options *format.ParseOptions
}

// NewParser 创建 Excel 解析器
func NewParser(opts *format.ParseOptions) *Parser {
	if opts == nil {
		opts = format.DefaultParseOptions()
	}
	return &Parser{options: opts}
}

// SupportedFormats 返回支持的格式
func (p *Parser) SupportedFormats() []format.FormatType {
	return []format.FormatType{format.FormatExcel}
}

// ParseSchema 解析 Excel Schema
// 默认解析第一个工作表，可通过 options.SheetName 或 options.SheetIndex 指定
func (p *Parser) ParseSchema(ctx context.Context, input io.Reader) (*format.Schema, error) {
	workbook, err := excelize.OpenReader(input)
	if err != nil {
		return nil, fmt.Errorf("failed to open excel file: %w", err)
	}
	defer workbook.Close()

	// 构建分析选项
	analyzeOpts := p.buildAnalyzeOptions()

	// 使用内部 Analyze 函数
	analysis, err := Analyze(ctx, workbook, analyzeOpts)
	if err != nil {
		return nil, err
	}

	// 转换为标准 Schema
	return p.convertToSchema(analysis)
}

// ReadRecords 读取 Excel 数据记录
func (p *Parser) ReadRecords(ctx context.Context, input io.Reader, offset, limit int64) ([]map[string]interface{}, error) {
	workbook, err := excelize.OpenReader(input)
	if err != nil {
		return nil, fmt.Errorf("failed to open excel file: %w", err)
	}
	defer workbook.Close()

	// 确定要读取的工作表
	sheetName := p.getTargetSheetName(workbook)
	if sheetName == "" {
		return []map[string]interface{}{}, nil
	}

	// 获取行迭代器
	rowsIter, err := workbook.Rows(sheetName)
	if err != nil {
		return nil, fmt.Errorf("failed to get rows iterator: %w", err)
	}
	defer rowsIter.Close()

	// 读取表头
	var headers []string
	hasHeader := p.options.HasHeader
	if rowsIter.Next() {
		cols, err := rowsIter.Columns()
		if err != nil {
			return nil, fmt.Errorf("failed to read header row: %w", err)
		}
		if hasHeader {
			headers = trimStringSlice(cols)
		} else {
			// 没有表头，生成列名
			for i := 0; i < len(cols); i++ {
				headers = append(headers, fmt.Sprintf("Column%d", i+1))
			}
			// 需要把第一行也作为数据处理，这里先记录
		}
	}

	if len(headers) == 0 {
		return []map[string]interface{}{}, nil
	}

	// 跳过到 offset
	currentRow := int64(0)
	if !hasHeader {
		// 如果没有表头，第一行已经被读了，需要回退处理
		// 这里简化处理：重新打开迭代器
		rowsIter.Close()
		rowsIter, err = workbook.Rows(sheetName)
		if err != nil {
			return nil, err
		}
		defer rowsIter.Close()
	}

	for currentRow < offset && rowsIter.Next() {
		currentRow++
	}

	// 读取数据
	maxRows := limit
	if limit < 0 {
		maxRows = 1<<63 - 1 // 最大值
	}

	records := make([]map[string]interface{}, 0)
	readCount := int64(0)

	for readCount < maxRows && rowsIter.Next() {
		cols, err := rowsIter.Columns()
		if err != nil {
			continue // 跳过错误行
		}

		record := make(map[string]interface{}, len(headers))
		for i, header := range headers {
			if i < len(cols) {
				record[header] = trimValue(cols[i])
			} else {
				record[header] = ""
			}
		}

		records = append(records, record)
		readCount++
	}

	return records, nil
}

// CountRecords 统计总记录数
func (p *Parser) CountRecords(ctx context.Context, input io.Reader) (int64, error) {
	workbook, err := excelize.OpenReader(input)
	if err != nil {
		return 0, fmt.Errorf("failed to open excel file: %w", err)
	}
	defer workbook.Close()

	sheetName := p.getTargetSheetName(workbook)
	if sheetName == "" {
		return 0, nil
	}

	rowsIter, err := workbook.Rows(sheetName)
	if err != nil {
		return 0, fmt.Errorf("failed to get rows iterator: %w", err)
	}
	defer rowsIter.Close()

	count := int64(0)
	for rowsIter.Next() {
		count++
	}

	// 如果有表头，减去1
	if p.options.HasHeader && count > 0 {
		count--
	}

	return count, nil
}

// ExtractMetadata 提取 Excel 元数据
func (p *Parser) ExtractMetadata(ctx context.Context, input io.Reader) (map[string]interface{}, error) {
	workbook, err := excelize.OpenReader(input)
	if err != nil {
		return nil, fmt.Errorf("failed to open excel file: %w", err)
	}
	defer workbook.Close()

	// 使用内部 Analyze 获取详细信息
	analyzeOpts := p.buildAnalyzeOptions()
	analysis, err := Analyze(ctx, workbook, analyzeOpts)
	if err != nil {
		return nil, err
	}

	// 构建元数据
	metadata := map[string]interface{}{
		"sheet_count":      analysis.SheetCount,
		"default_sheet":    analysis.DefaultSheet,
		"active_sheet":     analysis.ActiveSheet,
		"sheets":           analysis.Sheets,
		"sheets_truncated": analysis.Summary["sheets_truncated"],
		"rows_truncated":   analysis.Summary["rows_truncated"],
	}

	// 添加工作簿属性
	if props, err := workbook.GetDocProps(); err == nil {
		metadata["creator"] = props.Creator
		metadata["created"] = props.Created
		metadata["modified"] = props.Modified
		metadata["title"] = props.Title
		metadata["subject"] = props.Subject
	}

	return metadata, nil
}

// buildAnalyzeOptions 根据 ParseOptions 构建 Analyze 选项
func (p *Parser) buildAnalyzeOptions() *Options {
	opts := &Options{
		SheetLimit:      1, // 默认只分析一个工作表
		RowLimit:        int(p.options.SampleSize),
		ColumnLimit:     defaultColumnLimit,
		TypeDetectLimit: defaultTypeDetectLimit,
	}

	// 从 ExtraParams 中读取自定义参数
	if p.options.ExtraParams != nil {
		if v, ok := p.options.ExtraParams["sheet_limit"].(int); ok && v > 0 {
			opts.SheetLimit = v
		}
		if v, ok := p.options.ExtraParams["row_limit"].(int); ok && v > 0 {
			opts.RowLimit = v
		}
		if v, ok := p.options.ExtraParams["column_limit"].(int); ok && v > 0 {
			opts.ColumnLimit = v
		}
	}

	return opts
}

// getTargetSheetName 获取目标工作表名称
func (p *Parser) getTargetSheetName(workbook *excelize.File) string {
	// 优先使用指定的工作表名称
	if p.options.SheetName != "" {
		return p.options.SheetName
	}

	// 使用指定的工作表索引
	sheetList := workbook.GetSheetList()
	if len(sheetList) == 0 {
		return ""
	}

	if p.options.SheetIndex >= 0 && p.options.SheetIndex < len(sheetList) {
		return sheetList[p.options.SheetIndex]
	}

	// 默认使用活动工作表
	activeIndex := workbook.GetActiveSheetIndex()
	if activeIndex >= 0 && activeIndex < len(sheetList) {
		return sheetList[activeIndex]
	}

	// 降级到第一个工作表
	return sheetList[0]
}

// convertToSchema 将 WorkbookAnalysis 转换为标准 Schema
func (p *Parser) convertToSchema(analysis *WorkbookAnalysis) (*format.Schema, error) {
	if len(analysis.Sheets) == 0 {
		return &format.Schema{Fields: []format.Field{}}, nil
	}

	// 使用第一个工作表的信息（或指定的工作表）
	sheet := analysis.Sheets[0]

	// 构建字段列表
	fields := make([]format.Field, len(sheet.Headers))
	for i, header := range sheet.Headers {
		fieldType := format.FieldTypeString
		if i < len(sheet.ColumnTypes) {
			fieldType = mapExcelTypeToFieldType(sheet.ColumnTypes[i])
		}

		fields[i] = format.Field{
			Name:     header,
			Type:     fieldType,
			Nullable: true,
		}
	}

	schema := &format.Schema{
		Fields: fields,
	}

	// 添加记录数信息
	if sheet.RowCount > 0 {
		rowCount := int64(sheet.RowCount)
		schema.RecordCount = &rowCount
	}

	return schema, nil
}

// mapExcelTypeToFieldType 将 Excel 类型字符串映射到 FieldType
func mapExcelTypeToFieldType(excelType string) format.FieldType {
	switch excelType {
	case "int":
		return format.FieldTypeInt
	case "float":
		return format.FieldTypeFloat
	case "bool":
		return format.FieldTypeBool
	case "date":
		return format.FieldTypeDate
	default:
		return format.FieldTypeString
	}
}

// trimValue 清理单元格值
func trimValue(s string) interface{} {
	trimmed := trimStringSlice([]string{s})[0]
	if trimmed == "" {
		return nil
	}
	return trimmed
}

func init() {
	// 注册到全局 format registry
	_ = format.Register(NewParser(nil))
}
