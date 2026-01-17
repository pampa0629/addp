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

// ============ FileTableParser 接口实现 ============

// ParseTableInfo 从 Excel 文件中提取 TableInfo
// 实现 format.FileTableParser 接口
func (p *Parser) ParseTableInfo(ctx context.Context, input io.Reader, options *format.ParseOptions) (*format.TableInfo, error) {
	// 使用传入的 options，如果为 nil 则使用默认的
	opts := p.options
	if options != nil {
		opts = options
	}

	workbook, err := excelize.OpenReader(input)
	if err != nil {
		return nil, fmt.Errorf("failed to open excel file: %w", err)
	}
	defer workbook.Close()

	// 构建分析选项
	analyzeOpts := p.buildAnalyzeOptionsFromParseOptions(opts)

	// 使用内部 Analyze 函数
	analysis, err := Analyze(ctx, workbook, analyzeOpts)
	if err != nil {
		return nil, err
	}

	// 转换为 TableInfo
	return p.convertToTableInfo(analysis, opts)
}

// ReadPreview 读取 Excel 数据预览
// 实现 format.FileTableParser 接口
func (p *Parser) ReadPreview(ctx context.Context, input io.Reader, offset, limit int64, options *format.ParseOptions) ([]map[string]interface{}, error) {
	// 使用传入的 options，如果为 nil 则使用默认的
	opts := p.options
	if options != nil {
		opts = options
	}

	workbook, err := excelize.OpenReader(input)
	if err != nil {
		return nil, fmt.Errorf("failed to open excel file: %w", err)
	}
	defer workbook.Close()

	// 确定要读取的工作表
	sheetName := p.getTargetSheetNameFromOptions(workbook, opts)
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
	hasHeader := opts.HasHeader
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
		}
	}

	if len(headers) == 0 {
		return []map[string]interface{}{}, nil
	}

	// 如果没有表头，需要重新打开迭代器以读取第一行数据
	if !hasHeader {
		rowsIter.Close()
		rowsIter, err = workbook.Rows(sheetName)
		if err != nil {
			return nil, err
		}
		defer rowsIter.Close()
	}

	// 跳过到 offset
	currentRow := int64(0)
	for currentRow < offset && rowsIter.Next() {
		currentRow++
	}

	// 读取数据
	maxRows := limit
	if limit < 0 {
		maxRows = 1<<63 - 1
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

// convertToTableInfo 将 WorkbookAnalysis 转换为 TableInfo
func (p *Parser) convertToTableInfo(analysis *WorkbookAnalysis, opts *format.ParseOptions) (*format.TableInfo, error) {
	if len(analysis.Sheets) == 0 {
		return &format.TableInfo{
			Name:       "excel_data",
			Fields:     []format.FieldInfo{},
			PrimaryKey: []string{},
		}, nil
	}

	// 使用第一个工作表的信息（或指定的工作表）
	sheet := analysis.Sheets[0]

	// 构建字段列表
	fields := make([]format.FieldInfo, len(sheet.Headers))
	for i, header := range sheet.Headers {
		fieldType := format.FieldTypeString
		if i < len(sheet.ColumnTypes) {
			fieldType = mapExcelTypeToFieldType(sheet.ColumnTypes[i])
		}

		fields[i] = format.FieldInfo{
			Name:         header,
			Type:         fieldType,
			OriginalType: "", // Excel 没有原始类型概念
			Nullable:     true,
			IsPrimaryKey: false,
		}
	}

	// 创建 ExcelInfo 扩展
	excelInfo := &format.ExcelInfo{
		SheetName:  sheet.Name,
		SheetIndex: sheet.Index,
		SheetCount: analysis.SheetCount,
	}

	// 构建 TableInfo
	rowCount := int64(sheet.RowCount)
	tableInfo := &format.TableInfo{
		Name:       sheet.Name, // 使用工作表名称作为表名
		RowCount:   &rowCount,
		Fields:     fields,
		PrimaryKey: []string{},
		Extensions: []format.ExtensionInfo{excelInfo},
	}

	return tableInfo, nil
}

// buildAnalyzeOptionsFromParseOptions 根据 ParseOptions 构建 Analyze 选项
func (p *Parser) buildAnalyzeOptionsFromParseOptions(opts *format.ParseOptions) *Options {
	analyzeOpts := &Options{
		SheetLimit:      1, // 默认只分析一个工作表
		RowLimit:        int(opts.SampleSize),
		ColumnLimit:     defaultColumnLimit,
		TypeDetectLimit: defaultTypeDetectLimit,
	}

	// 从 ExtraParams 中读取自定义参数
	if opts.ExtraParams != nil {
		if v, ok := opts.ExtraParams["sheet_limit"].(int); ok && v > 0 {
			analyzeOpts.SheetLimit = v
		}
		if v, ok := opts.ExtraParams["row_limit"].(int); ok && v > 0 {
			analyzeOpts.RowLimit = v
		}
		if v, ok := opts.ExtraParams["column_limit"].(int); ok && v > 0 {
			analyzeOpts.ColumnLimit = v
		}
	}

	return analyzeOpts
}

// getTargetSheetNameFromOptions 根据 ParseOptions 获取目标工作表名称
func (p *Parser) getTargetSheetNameFromOptions(workbook *excelize.File, opts *format.ParseOptions) string {
	// 优先使用指定的工作表名称
	if opts.SheetName != "" {
		return opts.SheetName
	}

	// 使用指定的工作表索引
	sheetList := workbook.GetSheetList()
	if len(sheetList) == 0 {
		return ""
	}

	if opts.SheetIndex >= 0 && opts.SheetIndex < len(sheetList) {
		return sheetList[opts.SheetIndex]
	}

	// 默认使用活动工作表
	activeIndex := workbook.GetActiveSheetIndex()
	if activeIndex >= 0 && activeIndex < len(sheetList) {
		return sheetList[activeIndex]
	}

	// 降级到第一个工作表
	return sheetList[0]
}

// mapExcelTypeToFieldType 将 Excel 类型字符串映射到 FieldType
func mapExcelTypeToFieldType(excelType string) format.FieldType {
	switch excelType {
	case "int":
		return format.FieldTypeInt
	case "float":
		return format.FieldTypeDouble // Excel 中的浮点数为双精度
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
	parser := NewParser(nil)
	// 注册为 FileTableParser
	_ = format.RegisterFileTableParser(parser)
}
