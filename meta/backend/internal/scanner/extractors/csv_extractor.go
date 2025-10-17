package extractors

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/addp/meta/internal/scanner"
)

// CSVExtractor CSV文件的元数据提取器
type CSVExtractor struct{}

func (e *CSVExtractor) SupportedTypes() []string {
	return []string{
		"text/csv",
		"application/csv",
		"text/comma-separated-values",
	}
}

func (e *CSVExtractor) Priority() int {
	return 100
}

func (e *CSVExtractor) Extract(ctx context.Context, input scanner.ExtractInput) (*scanner.Metadata, error) {
	// 1. 创建CSV reader
	reader := csv.NewReader(input.Reader)
	reader.TrimLeadingSpace = true

	// 尝试自动检测分隔符
	reader.Comma = detectDelimiter(input.ObjectKey)

	// 2. 读取表头
	headers, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read CSV headers: %w", err)
	}

	// 3. 读取样本数据（前100行用于类型推断）
	sampleRows := make([][]string, 0, 100)
	rowCount := int64(0)

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			// 容错处理：跳过损坏的行
			continue
		}

		rowCount++
		if len(sampleRows) < 100 {
			sampleRows = append(sampleRows, record)
		}
	}

	// 4. 推断列类型
	columns := make([]scanner.ColumnInfo, len(headers))
	for i, header := range headers {
		colType := inferColumnType(sampleRows, i)
		columns[i] = scanner.ColumnInfo{
			Name:     strings.TrimSpace(header),
			Type:     colType,
			Nullable: true,
		}

		// 添加示例值
		if len(sampleRows) > 0 && i < len(sampleRows[0]) {
			columns[i].Example = sampleRows[0][i]
		}
	}

	// 5. 构建元数据
	metadata := &scanner.Metadata{
		BasicInfo: scanner.BasicMetadata{
			FileName:     filepath.Base(input.ObjectKey),
			FileType:     "CSV",
			Size:         input.Size,
			ContentType:  input.ContentType,
			Encoding:     detectEncoding(input.ObjectKey),
			LastModified: input.LastModified,
			ETag:         input.ETag,
		},
		SchemaInfo: &scanner.SchemaMetadata{
			Columns:  columns,
			RowCount: rowCount,
			Extra: map[string]interface{}{
				"delimiter":     string(reader.Comma),
				"has_header":    true,
				"column_count":  len(headers),
			},
		},
		CustomAttrs: make(map[string]interface{}),
	}

	// 6. 添加样本数据（前5行）
	sampleSize := 5
	if len(sampleRows) < sampleSize {
		sampleSize = len(sampleRows)
	}

	sampleData := make([]map[string]interface{}, 0, sampleSize)
	for i := 0; i < sampleSize; i++ {
		row := make(map[string]interface{})
		for j, header := range headers {
			if j < len(sampleRows[i]) {
				row[header] = convertValue(sampleRows[i][j], columns[j].Type)
			}
		}
		sampleData = append(sampleData, row)
	}
	metadata.SchemaInfo.SampleData = sampleData

	// 7. 添加统计信息
	metadata.CustomAttrs["statistics"] = map[string]interface{}{
		"total_rows":    rowCount,
		"total_columns": len(headers),
		"sample_size":   len(sampleRows),
	}

	return metadata, nil
}

// detectDelimiter 检测CSV分隔符
func detectDelimiter(filename string) rune {
	// 根据文件扩展名猜测
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".tsv":
		return '\t'
	case ".psv":
		return '|'
	default:
		return ',' // 默认逗号
	}
}

// detectEncoding 检测文件编码
func detectEncoding(filename string) string {
	// 简单实现：根据文件名猜测
	// 实际项目中可以使用 golang.org/x/text/encoding/charmap
	if strings.Contains(strings.ToLower(filename), "gbk") ||
		strings.Contains(strings.ToLower(filename), "gb2312") {
		return "GBK"
	}
	return "UTF-8"
}

// inferColumnType 推断列的数据类型
func inferColumnType(rows [][]string, colIndex int) string {
	if len(rows) == 0 {
		return "string"
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

		if value == "" || strings.ToLower(value) == "null" || strings.ToLower(value) == "na" {
			nullCount++
			continue
		}

		// 检查是否为布尔值
		if isBool(value) {
			boolCount++
			continue
		}

		// 检查是否为整数
		if _, err := strconv.ParseInt(value, 10, 64); err == nil {
			intCount++
			continue
		}

		// 检查是否为浮点数
		if _, err := strconv.ParseFloat(value, 64); err == nil {
			floatCount++
			continue
		}

		// 检查是否为日期
		if isDate(value) {
			dateCount++
			continue
		}
	}

	// 根据统计结果判断类型（需要80%以上的样本符合）
	threshold := float64(totalCount) * 0.8

	if float64(boolCount) >= threshold {
		return "boolean"
	}
	if float64(intCount) >= threshold {
		return "integer"
	}
	if float64(floatCount+intCount) >= threshold {
		return "number"
	}
	if float64(dateCount) >= threshold {
		return "date"
	}

	return "string"
}

// isBool 检查是否为布尔值
func isBool(s string) bool {
	s = strings.ToLower(s)
	return s == "true" || s == "false" || s == "yes" || s == "no" ||
		s == "1" || s == "0" || s == "t" || s == "f" || s == "y" || s == "n"
}

// isDate 简单的日期检测
func isDate(s string) bool {
	// 常见日期格式
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
			// 简单匹配：检查数字和分隔符位置
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

// convertValue 将字符串值转换为适当的类型
func convertValue(s string, dataType string) interface{} {
	s = strings.TrimSpace(s)

	if s == "" || strings.ToLower(s) == "null" || strings.ToLower(s) == "na" {
		return nil
	}

	switch dataType {
	case "integer":
		if v, err := strconv.ParseInt(s, 10, 64); err == nil {
			return v
		}
	case "number":
		if v, err := strconv.ParseFloat(s, 64); err == nil {
			return v
		}
	case "boolean":
		return isBool(s) && (strings.ToLower(s) == "true" || strings.ToLower(s) == "yes" ||
			s == "1" || strings.ToLower(s) == "t" || strings.ToLower(s) == "y")
	}

	return s
}
