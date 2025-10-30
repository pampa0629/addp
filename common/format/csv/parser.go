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

// SupportedFormats 返回支持的格式
func (p *Parser) SupportedFormats() []format.FormatType {
	return []format.FormatType{format.FormatCSV}
}

// ParseSchema 解析 CSV Schema
func (p *Parser) ParseSchema(ctx context.Context, input io.Reader) (*format.Schema, error) {
	reader := csv.NewReader(input)
	p.configureReader(reader)

	// 读取表头
	headers, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read CSV headers: %w", err)
	}

	// 读取样本数据用于类型推断
	sampleRows := make([][]string, 0, p.options.SampleSize)
	for i := 0; i < p.options.SampleSize; i++ {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			// 容错：跳过损坏的行
			continue
		}
		sampleRows = append(sampleRows, record)
	}

	// 构建 Schema
	schema := &format.Schema{
		Fields: make([]format.Field, len(headers)),
	}

	for i, header := range headers {
		fieldType := p.inferColumnType(sampleRows, i)
		schema.Fields[i] = format.Field{
			Name:     strings.TrimSpace(header),
			Type:     fieldType,
			Nullable: true,
		}
	}

	return schema, nil
}

// ReadRecords 读取 CSV 数据记录
func (p *Parser) ReadRecords(ctx context.Context, input io.Reader, offset, limit int64) ([]map[string]interface{}, error) {
	reader := csv.NewReader(input)
	p.configureReader(reader)

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
			if p.options.Encoding == "utf-8" {
				value = strings.TrimSpace(value)
			}

			// 自动类型转换
			row[header] = p.convertValue(value)
		}

		records = append(records, row)
	}

	return records, nil
}

// CountRecords 统计总记录数
func (p *Parser) CountRecords(ctx context.Context, input io.Reader) (int64, error) {
	reader := csv.NewReader(input)
	p.configureReader(reader)

	// 跳过表头
	if _, err := reader.Read(); err != nil {
		return 0, err
	}

	count := int64(0)
	for {
		_, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			// 容错：跳过损坏的行
			continue
		}
		count++
	}

	return count, nil
}

// ExtractMetadata 提取 CSV 元数据
func (p *Parser) ExtractMetadata(ctx context.Context, input io.Reader) (map[string]interface{}, error) {
	reader := csv.NewReader(input)
	p.configureReader(reader)

	// 读取表头
	headers, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read CSV headers: %w", err)
	}

	// 统计行数
	rowCount := int64(0)
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

	metadata := map[string]interface{}{
		"delimiter":    string(p.options.Delimiter),
		"has_header":   p.options.HasHeader,
		"column_count": len(headers),
		"row_count":    rowCount,
		"encoding":     p.options.Encoding,
		"headers":      headers,
	}

	return metadata, nil
}

// configureReader 配置 CSV reader
func (p *Parser) configureReader(reader *csv.Reader) {
	reader.Comma = p.options.Delimiter
	reader.TrimLeadingSpace = true
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1 // 允许可变字段数

	// 跳过指定行数
	for i := 0; i < p.options.SkipRows; i++ {
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
		return format.FieldTypeFloat
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
