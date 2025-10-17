// Package csvextractor CSV文件元数据提取器插件
package csvextractor

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	sdk "github.com/addp/meta-extractor-sdk"
)

// CSVMetadata CSV文件的类型化元数据
type CSVMetadata struct {
	RowCount    int64    `json:"row_count"`
	ColumnCount int      `json:"column_count"`
	Columns     []string `json:"columns"`
	Delimiter   string   `json:"delimiter"`
	HasHeader   bool     `json:"has_header"`
	Encoding    string   `json:"encoding"`
}

// TypeName 实现 TypedMetadata 接口
func (m *CSVMetadata) TypeName() string {
	return "csv.metadata"
}

// Schema 实现 TypedMetadata 接口
func (m *CSVMetadata) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"row_count":    map[string]string{"type": "integer", "description": "Number of rows"},
			"column_count": map[string]string{"type": "integer", "description": "Number of columns"},
			"columns":      map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}},
			"delimiter":    map[string]string{"type": "string", "description": "Column delimiter"},
			"has_header":   map[string]string{"type": "boolean", "description": "Whether first row is header"},
			"encoding":     map[string]string{"type": "string", "description": "Character encoding"},
		},
		"required": []string{"row_count", "column_count"},
	}
}

// ToMap 实现 TypedMetadata 接口
func (m *CSVMetadata) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"row_count":    m.RowCount,
		"column_count": m.ColumnCount,
		"columns":      m.Columns,
		"delimiter":    m.Delimiter,
		"has_header":   m.HasHeader,
		"encoding":     m.Encoding,
	}
}

// FromMap 实现 TypedMetadata 接口
func (m *CSVMetadata) FromMap(data map[string]interface{}) error {
	if v, ok := data["row_count"].(float64); ok {
		m.RowCount = int64(v)
	}
	if v, ok := data["column_count"].(float64); ok {
		m.ColumnCount = int(v)
	}
	if v, ok := data["columns"].([]interface{}); ok {
		m.Columns = make([]string, len(v))
		for i, val := range v {
			if s, ok := val.(string); ok {
				m.Columns[i] = s
			}
		}
	}
	if v, ok := data["delimiter"].(string); ok {
		m.Delimiter = v
	}
	if v, ok := data["has_header"].(bool); ok {
		m.HasHeader = v
	}
	if v, ok := data["encoding"].(string); ok {
		m.Encoding = v
	}
	return nil
}

// init 函数：注册自定义元数据类型
func init() {
	sdk.RegisterMetadataType(&CSVMetadata{})
}

// CSVExtractor CSV文件的元数据提取器
type CSVExtractor struct{}

// SupportedTypes 返回支持的MIME类型
func (e *CSVExtractor) SupportedTypes() []string {
	return []string{
		"text/csv",
		"application/csv",
		"text/comma-separated-values",
	}
}

// Priority 返回优先级
func (e *CSVExtractor) Priority() int {
	return 60
}

// Extract 提取CSV文件元数据
func (e *CSVExtractor) Extract(ctx context.Context, input sdk.ExtractInput) (*sdk.Metadata, error) {
	// 1. 创建基础元数据
	metadata := sdk.NewMetadata(
		filepath.Base(input.ObjectKey),
		"CSV File",
		input.Size,
	)

	metadata.BasicInfo.ContentType = input.ContentType
	metadata.BasicInfo.LastModified = input.LastModified
	metadata.BasicInfo.ETag = input.ETag
	metadata.BasicInfo.Encoding = "UTF-8"

	// 2. 解析CSV
	reader := csv.NewReader(input.Reader)
	reader.TrimLeadingSpace = true

	// 读取第一行（可能是表头）
	firstRow, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read CSV: %w", err)
	}

	csvMeta := &CSVMetadata{
		ColumnCount: len(firstRow),
		Columns:     firstRow,
		Delimiter:   ",",
		HasHeader:   looksLikeHeader(firstRow),
		Encoding:    "UTF-8",
		RowCount:    1,
	}

	// 读取剩余行来统计行数（最多读取1000行来提高性能）
	maxRows := 1000
	for i := 0; i < maxRows; i++ {
		_, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		csvMeta.RowCount++
	}

	// 3. 添加类型化元数据
	metadata.AddTypedMetadata("csv_metadata", csvMeta)

	// 4. 如果有表头，创建SchemaInfo
	if csvMeta.HasHeader {
		columns := make([]sdk.ColumnInfo, len(firstRow))
		for i, name := range firstRow {
			columns[i] = sdk.ColumnInfo{
				Name: name,
				Type: "string", // CSV都是文本，无法推断类型
			}
		}

		metadata.SchemaInfo = &sdk.SchemaMetadata{
			Columns:  columns,
			RowCount: csvMeta.RowCount - 1, // 减去表头行
		}
	}

	// 5. 添加文件大小信息
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

// looksLikeHeader 判断第一行是否像表头
func looksLikeHeader(row []string) bool {
	if len(row) == 0 {
		return false
	}

	// 简单启发式：如果所有列都是非空的，且没有纯数字，可能是表头
	hasNonNumeric := false
	for _, cell := range row {
		if cell == "" {
			return false
		}
		// 检查是否为纯数字
		if !isNumeric(cell) {
			hasNonNumeric = true
		}
	}

	return hasNonNumeric
}

// isNumeric 检查字符串是否为数字
func isNumeric(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}

	// 简单检查：看是否能解析为浮点数
	for i, c := range s {
		if c == '-' || c == '+' {
			if i != 0 {
				return false
			}
			continue
		}
		if c == '.' {
			continue
		}
		if c < '0' || c > '9' {
			return false
		}
	}

	return true
}

// GetExtractor 返回提取器实例（供ADDP加载使用）
func GetExtractor() sdk.MetadataExtractor {
	return &CSVExtractor{}
}
