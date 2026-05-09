package csv

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/addp/common/format"
)

// Parser 实现 CSV 格式的解析器
type Parser struct {
	options *format.ParseOptions
}

// NewParser 创建 CSV 解析器
func NewParser(opts *format.ParseOptions) *Parser {
	if opts == nil {
		opts = format.DefaultParseOptions()
	}
	return &Parser{options: opts}
}

// ParseTableInfo 从 CSV 文件中提取 TableInfo
func (p *Parser) ParseTableInfo(ctx context.Context, input io.Reader, options *format.ParseOptions) (*format.TableInfo, error) {
	// 使用传入的 options，如果为 nil 则使用默认的
	opts := p.options
	if options != nil {
		opts = options
	}

	reader := csv.NewReader(input)
	p.configureReaderWithOptions(reader, opts)

	// 读取表头
	headers, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read CSV headers: %w", err)
	}

	// 读取样本数据用于类型推断
	sampleRows := make([][]string, 0, opts.SampleSize)
	rowCount := int64(0)
	for i := 0; i < opts.SampleSize; i++ {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			// 容错：跳过损坏的行
			continue
		}
		sampleRows = append(sampleRows, record)
		rowCount++
	}

	// 继续统计剩余行数
	for {
		_, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}
		rowCount++
	}

	// 构建 FieldInfo 列表
	fields := make([]format.FieldInfo, len(headers))
	for i, header := range headers {
		fieldType := p.inferColumnType(sampleRows, i)
		fields[i] = format.FieldInfo{
			Name:         strings.TrimSpace(header),
			Type:         fieldType,
			OriginalType: string(fieldType), // CSV 没有原始类型，使用推断类型的字符串表示
			Nullable:     true,              // CSV 默认允许 NULL
			IsPrimaryKey: false,
			Comment:      "",
		}
	}

	// 构建 CSVInfo 扩展
	csvInfo := &format.CSVInfo{
		Delimiter:  opts.Delimiter,
		Encoding:   opts.Encoding,
		HasHeader:  opts.HasHeader,
		QuoteChar:  '"',
		EscapeChar: '"',
		LineEnding: "\n",
	}

	// 构建 TableInfo
	tableInfo := &format.TableInfo{
		Name:       "csv_data", // CSV 文件没有表名，使用默认值
		RowCount:   &rowCount,
		Fields:     fields,
		PrimaryKey: []string{}, // CSV 没有主键
		Extensions: []format.ExtensionInfo{csvInfo},
	}

	return tableInfo, nil
}

// ReadPreview 读取 CSV 数据预览
func (p *Parser) ReadPreview(ctx context.Context, input io.Reader, offset, limit int64, options *format.ParseOptions) ([]map[string]interface{}, error) {
	// 使用传入的 options，如果为 nil 则使用默认的
	opts := p.options
	if options != nil {
		opts = options
	}

	reader := csv.NewReader(input)
	p.configureReaderWithOptions(reader, opts)

	// 读取表头
	headers, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read CSV headers: %w", err)
	}

	// 跳过到 offset
	for i := int64(0); i < offset; i++ {
		if _, err := reader.Read(); err != nil {
			if err == io.EOF {
				return []map[string]interface{}{}, nil
			}
			return nil, fmt.Errorf("failed to skip to offset: %w", err)
		}
	}

	// 读取数据
	maxRows := limit
	if limit < 0 {
		maxRows = 1<<63 - 1 // 最大值
	}

	records := make([]map[string]interface{}, 0)
	for i := int64(0); i < maxRows; i++ {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			// 容错：跳过损坏的行
			continue
		}

		row := make(map[string]interface{})
		for j, header := range headers {
			if j >= len(record) {
				row[header] = nil
				continue
			}

			value := record[j]
			if opts.Encoding == "utf-8" {
				value = strings.TrimSpace(value)
			}

			// 自动类型转换
			row[header] = p.convertValue(value)
		}

		records = append(records, row)
	}

	return records, nil
}

// ============ 辅助方法 ============

// configureReader 配置 CSV reader（使用默认 options）
func (p *Parser) configureReader(reader *csv.Reader) {
	p.configureReaderWithOptions(reader, p.options)
}

// configureReaderWithOptions 配置 CSV reader（使用指定 options）
func (p *Parser) configureReaderWithOptions(reader *csv.Reader, opts *format.ParseOptions) {
	reader.Comma = opts.Delimiter
	reader.TrimLeadingSpace = true
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1 // 允许可变字段数

	// 跳过指定行数
	for i := 0; i < opts.SkipRows; i++ {
		reader.Read()
	}
}

// inferColumnType 推断列的数据类型
func (p *Parser) inferColumnType(rows [][]string, colIndex int) format.FieldType {
	if len(rows) == 0 {
		return format.FieldTypeString
	}

	// 统计各类型的出现次数
	intCount := 0
	floatCount := 0
	boolCount := 0
	dateCount := 0
	nullCount := 0
	totalCount := 0

	for _, row := range rows {
		if colIndex >= len(row) {
			continue
		}

		value := strings.TrimSpace(row[colIndex])
		totalCount++

		if value == "" || strings.EqualFold(value, "null") || strings.EqualFold(value, "na") {
			nullCount++
			continue
		}

		// 检查布尔值
		if p.isBool(value) {
			boolCount++
			continue
		}

		// 检查整数
		if _, err := strconv.ParseInt(value, 10, 64); err == nil {
			intCount++
			continue
		}

		// 检查浮点数
		if _, err := strconv.ParseFloat(value, 64); err == nil {
			floatCount++
			continue
		}

		// 检查日期
		if p.isDate(value) {
			dateCount++
			continue
		}
	}

	// 根据统计结果判断类型（需要80%以上的样本符合）
	threshold := float64(totalCount) * 0.8

	if float64(boolCount) >= threshold {
		return format.FieldTypeBool
	}
	if float64(intCount) >= threshold {
		return format.FieldTypeInt
	}
	if float64(floatCount+intCount) >= threshold {
		return format.FieldTypeDouble // CSV 中的浮点数默认为双精度
	}
	if float64(dateCount) >= threshold {
		return format.FieldTypeTimestamp
	}

	return format.FieldTypeString
}

// convertValue 将字符串转换为适当的类型
func (p *Parser) convertValue(s string) interface{} {
	s = strings.TrimSpace(s)

	if s == "" || strings.EqualFold(s, "null") || strings.EqualFold(s, "na") {
		return nil
	}

	// 尝试布尔值
	if p.isBool(s) {
		lower := strings.ToLower(s)
		return lower == "true" || lower == "yes" || s == "1" || lower == "t" || lower == "y"
	}

	// 尝试整数
	if v, err := strconv.ParseInt(s, 10, 64); err == nil {
		return v
	}

	// 尝试浮点数
	if v, err := strconv.ParseFloat(s, 64); err == nil {
		return v
	}

	return s
}

// isBool 检查是否为布尔值
func (p *Parser) isBool(s string) bool {
	s = strings.ToLower(s)
	return s == "true" || s == "false" || s == "yes" || s == "no" ||
		s == "1" || s == "0" || s == "t" || s == "f" || s == "y" || s == "n"
}

// isDate 简单的日期检测
func (p *Parser) isDate(s string) bool {
	datePatterns := []string{
		"2006-01-02",
		"2006/01/02",
		"01-02-2006",
		"01/02/2006",
		"2006-01-02 15:04:05",
		"02-Jan-2006",
	}

	for _, pattern := range datePatterns {
		if len(s) == len(pattern) {
			match := true
			for i, ch := range pattern {
				if ch >= '0' && ch <= '9' {
					if i >= len(s) || (s[i] < '0' || s[i] > '9') {
						match = false
						break
					}
				} else if ch != rune(s[i]) {
					match = false
					break
				}
			}
			if match {
				return true
			}
		}
	}
	return false
}

// DetectDelimiter 检测 CSV 分隔符（辅助函数）
func DetectDelimiter(filename string) rune {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".tsv":
		return '\t'
	case ".psv":
		return '|'
	default:
		return ','
	}
}

func init() {
	parser := NewParser(nil)
	_ = format.RegisterTableProvider(format.NewTableProvider(
		format.FormatCSV,
		parser.ParseTableInfo,
		parser.ReadPreview,
	))
}
