package extractors

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/addp/common/format"
	csvParser "github.com/addp/common/format/csv"
)

// CSVExtractor CSV文件的元数据提取器
// 使用共享的 CSV parser 避免重复代码
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

func (e *CSVExtractor) Extract(ctx context.Context, input format.ExtractInput) (*format.ExtractedMetadata, error) {
	// 1. 构建解析选项
	opts := &format.ParseOptions{
		Encoding:   detectEncoding(input.ObjectKey),
		Delimiter:  detectDelimiter(input.ObjectKey),
		HasHeader:  true,
		SampleSize: 100,
	}

	// 2. 创建共享 CSV parser
	parser := csvParser.NewParser(opts)

	// 3. 解析 schema 使用新接口
	tableInfo, err := parser.ParseTableInfo(ctx, input.Reader, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CSV schema: %w", err)
	}

	// 4. 提取元数据
	metadata := &format.ExtractedMetadata{
		BasicInfo: format.BasicMetadata{
			FileName:     filepath.Base(input.ObjectKey),
			FileType:     "CSV",
			Size:         input.Size,
			ContentType:  input.ContentType,
			Encoding:     opts.Encoding,
			LastModified: input.LastModified,
			ETag:         input.ETag,
		},
		CustomAttrs: make(map[string]interface{}),
	}

	// 5. 转换 TableInfo 到 SchemaMetadata
	columns := make([]format.ColumnMetadata, len(tableInfo.Fields))
	for i, field := range tableInfo.Fields {
		columns[i] = format.ColumnMetadata{
			Name:     field.Name,
			Type:     mapFormatTypeToMetaType(field.Type),
			Nullable: field.Nullable,
		}
	}

	var rowCount int64
	if tableInfo.RowCount != nil {
		rowCount = *tableInfo.RowCount
	}

	metadata.SchemaInfo = &format.SchemaMetadata{
		Columns:  columns,
		RowCount: rowCount,
		Extra: map[string]interface{}{
			"delimiter":    string(opts.Delimiter),
			"has_header":   opts.HasHeader,
			"column_count": len(tableInfo.Fields),
		},
	}

	// 6. 添加统计信息
	metadata.CustomAttrs["statistics"] = map[string]interface{}{
		"total_rows":    rowCount,
		"total_columns": len(tableInfo.Fields),
		"encoding":      opts.Encoding,
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

// mapFormatTypeToMetaType 将 format.FieldType 映射到 meta 的类型字符串
func mapFormatTypeToMetaType(ft format.FieldType) string {
	switch ft {
	case format.FieldTypeInt:
		return "integer"
	case format.FieldTypeFloat:
		return "number"
	case format.FieldTypeBool:
		return "boolean"
	case format.FieldTypeDate:
		return "date"
	case format.FieldTypeTimestamp:
		return "datetime"
	default:
		return "string"
	}
}
