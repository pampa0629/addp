package excel

import (
	"context"
	"fmt"
	"github.com/addp/common/datatype"
	"io"
	"strings"

	"github.com/addp/common/contentio"
	"github.com/addp/common/format"
	"github.com/xuri/excelize/v2"
)

// Plugin 实现 Excel 格式插件。
type Plugin struct {
	options *format.ParseOptions
}

// NewPlugin 创建 Excel 插件。
func NewPlugin(opts *format.ParseOptions) *Plugin {
	if opts == nil {
		opts = format.DefaultParseOptions()
	}
	return &Plugin{options: opts}
}

func (p *Plugin) Format() format.FormatType {
	return format.FormatExcel
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:             "builtin-excel",
		Format:         format.FormatExcel,
		I18nKey:        "format.excel",
		DataType:       format.FormatDataTypeContainer,
		Layouts:        []string{format.FormatLayoutSingle},
		ProviderHints:  []string{format.FormatProviderContainer, format.FormatProviderTable},
		Identification: format.FormatIdentification{Extensions: []string{".xlsx", ".xls", ".xlsm"}, MimeTypes: []string{"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "application/vnd.ms-excel", "application/vnd.ms-excel.sheet.macroenabled.12"}},
		Providers:      format.FormatProviderDescriptor{ContainerInfo: true, TableInfo: true, TableSample: true, Table: true},
		ContentReaders: []string{string(format.ContentReaderTableSample), string(format.ContentReaderRawContent), string(format.ContentReaderContainerEntry)},
		EngineFamilies: []string{format.EngineFamilyObject, format.EngineFamilyFile},
	}
}

func (p *Plugin) Capabilities() format.FormatCapability {
	capability, ok := format.GetFormatCapability(format.FormatExcel)
	if ok {
		return capability
	}
	return format.FormatCapability{
		Format:        format.FormatExcel,
		DataType:      format.FormatDataTypeContainer,
		Layouts:       []string{format.FormatLayoutSingle},
		ProviderHints: []string{format.FormatProviderContainer, format.FormatProviderTable},
		ContentReaders: []string{
			string(format.ContentReaderTableSample),
			string(format.ContentReaderRawContent),
			string(format.ContentReaderContainerEntry),
		},
		Parse: true,
	}
}

func (p *Plugin) ResolveContainerChild(_ context.Context, parent contentio.Reader, parentRef contentio.Ref, child format.ContainerChildInfo, _ *format.ParseOptions) (*format.ContainerChildResource, error) {
	return format.NativeContainerChildResource(parent, parentRef, p.Format(), child, format.ChildTableParseOptions(child.Name, child.Properties)), nil
}

// DescribeContainer 从 Excel 文件中提取 workbook / sheet 容器信息。
func (p *Plugin) DescribeContainer(ctx context.Context, input io.Reader, options *format.ParseOptions) (*format.ContainerInfo, error) {
	opts := p.options
	if options != nil {
		opts = options
	}

	workbook, err := excelize.OpenReader(input)
	if err != nil {
		return nil, fmt.Errorf("failed to open excel file: %w", err)
	}
	defer workbook.Close()

	analyzeOpts := p.buildAnalyzeOptionsFromParseOptions(opts)
	if opts == nil || opts.ExtraParams == nil || opts.ExtraParams[format.ContainerChildLimitParam] == nil {
		analyzeOpts.SheetLimit = defaultSheetLimit
	}
	if analyzeOpts.RowLimit <= 0 {
		analyzeOpts.RowLimit = 20
	}

	analysis, err := Analyze(ctx, workbook, analyzeOpts)
	if err != nil {
		return nil, err
	}

	return p.convertToContainerInfo(analysis), nil
}

// DescribeTable 从 Excel 文件中提取 TableInfo。
func (p *Plugin) DescribeTable(ctx context.Context, input io.Reader, options *format.ParseOptions) (*datatype.TableDescribeResult, error) {
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

	analyzeOpts := p.buildAnalyzeOptionsFromParseOptions(opts)
	if p.hasExplicitSheetSelection(opts) {
		sheetName := p.getTargetSheetNameFromOptions(workbook, opts)
		if sheetName == "" {
			return format.TableDescribeResultFromSchema(&format.TableInfo{
				Name:       "excel_data",
				Fields:     []format.FieldInfo{},
				PrimaryKey: []string{},
			}), nil
		}
		sheetIndex := p.sheetIndex(workbook, sheetName)
		summary, _, err := analyzeSheet(workbook, sheetName, sheetIndex, *analyzeOpts)
		if err != nil {
			return nil, err
		}
		analysis := &WorkbookAnalysis{
			SheetCount:   len(workbook.GetSheetList()),
			DefaultSheet: sheetName,
			ActiveSheet:  sheetName,
			Sheets:       []SheetSummary{summary},
		}
		tableInfo, err := p.convertToTableInfo(analysis, opts)
		if err != nil {
			return nil, err
		}
		return format.TableDescribeResultFromSchema(tableInfo), nil
	}

	analysis, err := Analyze(ctx, workbook, analyzeOpts)
	if err != nil {
		return nil, err
	}
	tableInfo, err := p.convertToTableInfo(analysis, opts)
	if err != nil {
		return nil, err
	}
	return format.TableDescribeResultFromSchema(tableInfo), nil
}

// SampleTable 读取 Excel 表格样本。
func (p *Plugin) SampleTable(ctx context.Context, input io.Reader, offset, limit int64, options *format.ParseOptions) ([]map[string]interface{}, error) {
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
func (p *Plugin) convertToTableInfo(analysis *WorkbookAnalysis, opts *format.ParseOptions) (*format.TableInfo, error) {
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
		fieldType := datatype.FieldTypeString
		if i < len(sheet.ColumnTypes) {
			fieldType = mapExcelTypeToFieldType(sheet.ColumnTypes[i])
		}

		fields[i] = format.FieldInfo{
			Name:         header,
			Type:         fieldType, // Excel 没有原始类型概念
			Nullable:     true,
			IsPrimaryKey: false,
		}
	}

	// 创建 Excel 格式私有信息
	excelInfo := &Info{
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
		FormatInfo: map[string]interface{}{"excel": excelInfo},
	}

	return tableInfo, nil
}

func (p *Plugin) convertToContainerInfo(analysis *WorkbookAnalysis) *format.ContainerInfo {
	if analysis == nil {
		return &format.ContainerInfo{
			Format:        format.FormatExcel,
			ResourceCount: 1,
			Children:      []format.ContainerChildInfo{},
			FormatInfo:    map[string]interface{}{},
		}
	}

	children := make([]format.ContainerChildInfo, 0, len(analysis.Sheets))
	for _, sheet := range analysis.Sheets {
		rowCount := int64(sheet.RowCount)
		columnCount := sheet.ColumnCount
		hasHeader := sheet.HasHeader
		children = append(children, format.ContainerChildInfo{
			Name:        sheet.Name,
			ChildKind:   "sheet",
			DataType:    format.FormatDataTypeTable,
			RowCount:    &rowCount,
			ColumnCount: &columnCount,
			HasHeader:   &hasHeader,
			Fields:      excelSheetFields(sheet),
		})
	}

	return &format.ContainerInfo{
		Format:        format.FormatExcel,
		ChildCount:    analysis.SheetCount,
		DefaultChild:  analysis.DefaultSheet,
		ResourceCount: 1,
		Children:      children,
		FormatInfo: map[string]interface{}{
			"sheet_count":        analysis.SheetCount,
			"default_sheet":      analysis.DefaultSheet,
			"sampled_sheets":     len(children),
			"children_truncated": analysis.SheetCount > len(children),
		},
	}
}

func excelSheetFields(sheet SheetSummary) []format.FieldInfo {
	fields := make([]format.FieldInfo, 0, len(sheet.Headers))
	for i, header := range sheet.Headers {
		fieldType := datatype.FieldTypeString
		originalType := ""
		if i < len(sheet.ColumnTypes) {
			originalType = sheet.ColumnTypes[i]
			fieldType = mapExcelTypeToFieldType(originalType)
		}
		fields = append(fields, format.FieldInfo{
			Name:     header,
			Type:     fieldType,
			Nullable: true,
		})
	}
	return fields
}

// buildAnalyzeOptionsFromParseOptions 根据通用 ParseOptions 构建 Analyze 选项。
func (p *Plugin) buildAnalyzeOptionsFromParseOptions(opts *format.ParseOptions) *Options {
	analyzeOpts := &Options{
		SheetLimit:      1, // 默认只分析一个工作表
		RowLimit:        int(opts.SampleSize),
		ColumnLimit:     defaultColumnLimit,
		TypeDetectLimit: defaultTypeDetectLimit,
	}

	// 从 ExtraParams 中读取自定义参数
	if opts.ExtraParams != nil {
		if v, ok := opts.ExtraParams[format.ContainerChildLimitParam].(int); ok && v > 0 {
			analyzeOpts.SheetLimit = v
		}
		if v, ok := opts.ExtraParams[format.ContainerRowLimitParam].(int); ok && v >= 0 {
			analyzeOpts.RowLimit = v
		}
	}

	return analyzeOpts
}

// getTargetSheetNameFromOptions 根据 ParseOptions 获取目标工作表名称
func (p *Plugin) getTargetSheetNameFromOptions(workbook *excelize.File, opts *format.ParseOptions) string {
	// 优先使用指定的工作表名称
	if opts.SheetName != "" {
		return opts.SheetName
	}
	if opts.ExtraParams != nil {
		if childName, ok := opts.ExtraParams[format.ChildNameParam].(string); ok && strings.TrimSpace(childName) != "" {
			return strings.TrimSpace(childName)
		}
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

func (p *Plugin) hasExplicitSheetSelection(opts *format.ParseOptions) bool {
	if opts == nil {
		return false
	}
	if opts.SheetName != "" {
		return true
	}
	return opts.SheetIndex > 0
}

func (p *Plugin) sheetIndex(workbook *excelize.File, sheetName string) int {
	for i, name := range workbook.GetSheetList() {
		if name == sheetName {
			return i
		}
	}
	return 0
}

// mapExcelTypeToFieldType 将 Excel 类型字符串映射到 FieldType
func mapExcelTypeToFieldType(excelType string) datatype.FieldType {
	switch excelType {
	case "int":
		return datatype.FieldTypeInt
	case "float":
		return datatype.FieldTypeDouble // Excel 中的浮点数为双精度
	case "bool":
		return datatype.FieldTypeBool
	case "date":
		return datatype.FieldTypeDate
	default:
		return datatype.FieldTypeString
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
	_ = format.RegisterFormatPlugin(NewPlugin(nil))
}
