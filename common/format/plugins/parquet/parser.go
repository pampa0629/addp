// Package parquet 提供 Parquet 文件格式的解析能力
// 支持 Schema 推断和样本数据读取
// 使用纯 Go 实现（github.com/parquet-go/parquet-go），无 CGO 依赖
package parquet

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/addp/common/format"
	"github.com/addp/common/resource"
	parquetgo "github.com/parquet-go/parquet-go"
	parquetfmt "github.com/parquet-go/parquet-go/format"
)

// Plugin 实现 Parquet 格式 plugin。
type Plugin struct{}

func NewPlugin() *Plugin {
	return &Plugin{}
}

func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(fmt.Sprintf("failed to register parquet format plugin: %v", err))
	}
}

func (p *Plugin) Format() format.FormatType {
	return format.FormatParquet
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	descriptor, ok := format.GetFormatDescriptor(format.FormatParquet)
	if ok {
		return descriptor
	}
	return format.FormatDescriptor{
		ID:             "builtin-parquet",
		Format:         format.FormatParquet,
		DataType:       format.FormatDataTypeTable,
		Layouts:        []string{format.FormatLayoutSingle, format.FormatLayoutWhole},
		ProviderHints:  []string{format.FormatProviderTable},
		ContentReaders: []string{string(format.ContentReaderTableSample), string(format.ContentReaderScopeTableSample), string(format.ContentReaderRawContent)},
	}
}

func (p *Plugin) Capabilities() format.FormatCapability {
	capability, ok := format.GetFormatCapability(format.FormatParquet)
	if ok {
		return capability
	}
	return format.FormatCapability{
		Format:        format.FormatParquet,
		DataType:      format.FormatDataTypeTable,
		Layouts:       []string{format.FormatLayoutSingle, format.FormatLayoutWhole},
		ProviderHints: []string{format.FormatProviderTable},
		TransferRead:  true,
		TransferWrite: true,
		Parse:         true,
	}
}

// DescribeTable 从 Parquet 文件中提取 TableInfo（Schema + 行数）
func (p *Plugin) DescribeTable(ctx context.Context, input io.Reader, options *format.ParseOptions) (*format.TableInfo, error) {
	data, err := io.ReadAll(input)
	if err != nil {
		return nil, fmt.Errorf("failed to read parquet data: %w", err)
	}

	file, err := parquetgo.OpenFile(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("failed to open parquet file: %w", err)
	}

	fields := extractFields(file.Schema())
	rowCount := file.NumRows()

	return &format.TableInfo{
		Fields:   fields,
		RowCount: &rowCount,
	}, nil
}

// SampleTable 读取 Parquet 表格样本。
func (p *Plugin) SampleTable(ctx context.Context, input io.Reader, offset, limit int64, options *format.ParseOptions) ([]map[string]interface{}, error) {
	data, err := io.ReadAll(input)
	if err != nil {
		return nil, fmt.Errorf("failed to read parquet data: %w", err)
	}

	file, err := parquetgo.OpenFile(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("failed to open parquet file: %w", err)
	}

	// 提取列名（叶子列顺序）
	fieldNames := extractLeafColumnNames(file.Schema())

	if limit <= 0 {
		limit = 100
	}

	result := make([]map[string]interface{}, 0, limit)
	remainingOffset := offset
	if remainingOffset < 0 {
		remainingOffset = 0
	}

	for _, rg := range file.RowGroups() {
		if int64(len(result)) >= limit {
			break
		}
		if remainingOffset >= rg.NumRows() {
			remainingOffset -= rg.NumRows()
			continue
		}

		rows := rg.Rows()
		defer rows.Close()

		// 跳过当前 row group 内的局部 offset。
		if remainingOffset > 0 {
			buf := make([]parquetgo.Row, 1)
			skipped := int64(0)
			for skipped < remainingOffset {
				n, readErr := rows.ReadRows(buf)
				if n > 0 {
					skipped += int64(n)
				}
				if readErr == io.EOF {
					break
				}
				if readErr != nil {
					return nil, fmt.Errorf("failed to skip rows: %w", readErr)
				}
				if n == 0 {
					break
				}
			}
			remainingOffset = 0
		}

		buf := make([]parquetgo.Row, 1)
		for int64(len(result)) < limit {
			n, readErr := rows.ReadRows(buf)
			if readErr != nil {
				if readErr != io.EOF {
					return result, fmt.Errorf("failed to read row: %w", readErr)
				}
				if n == 0 {
					break
				}
			}
			if n == 0 {
				break
			}

			row := make(map[string]interface{}, len(buf[0]))
			for j, val := range buf[0] {
				if j < len(fieldNames) {
					row[fieldNames[j]] = valueToInterface(val)
				}
			}
			result = append(result, row)
			if readErr == io.EOF {
				break
			}
		}
	}

	return result, nil
}

func (p *Plugin) DescribeTableScope(ctx context.Context, reader resource.ResourceReader, scope resource.ResourceRef, options *format.ParseOptions) (*format.TableInfo, error) {
	input, err := openFirstParquet(ctx, reader, scope)
	if err != nil {
		return nil, err
	}
	defer input.Close()
	return p.DescribeTable(ctx, input, options)
}

func (p *Plugin) SampleTableScope(ctx context.Context, reader resource.ResourceReader, scope resource.ResourceRef, offset, limit int64, options *format.ParseOptions) ([]map[string]interface{}, error) {
	input, err := openFirstParquet(ctx, reader, scope)
	if err != nil {
		return nil, err
	}
	defer input.Close()
	return p.SampleTable(ctx, input, offset, limit, options)
}

// valueToInterface 将 parquet.Value 转换为 Go 原生类型
func valueToInterface(v parquetgo.Value) interface{} {
	if v.IsNull() {
		return nil
	}
	switch v.Kind() {
	case parquetgo.Boolean:
		return v.Boolean()
	case parquetgo.Int32:
		return v.Int32()
	case parquetgo.Int64:
		return v.Int64()
	case parquetgo.Int96:
		return v.Int96().String()
	case parquetgo.Float:
		return v.Float()
	case parquetgo.Double:
		return v.Double()
	case parquetgo.ByteArray:
		return string(v.ByteArray())
	case parquetgo.FixedLenByteArray:
		return string(v.ByteArray())
	default:
		return v.String()
	}
}

// extractFields 从 Parquet Schema 提取 FieldInfo 列表
func extractFields(schema *parquetgo.Schema) []format.FieldInfo {
	if schema == nil {
		return nil
	}

	fields := schema.Fields()
	result := make([]format.FieldInfo, 0, len(fields))

	for _, f := range fields {
		fieldInfo := format.FieldInfo{
			Name:         f.Name(),
			Nullable:     f.Optional(),
			OriginalType: parquetTypeString(f),
			Type:         mapParquetType(f),
		}
		result = append(result, fieldInfo)
	}

	return result
}

// extractLeafColumnNames 提取叶子列名（与 parquet.Row 中的 Value 顺序对应）
func extractLeafColumnNames(schema *parquetgo.Schema) []string {
	if schema == nil {
		return nil
	}
	fields := schema.Fields()
	names := make([]string, 0, len(fields))
	for _, f := range fields {
		names = append(names, f.Name())
	}
	return names
}

// parquetTypeString 返回 Parquet 类型的字符串表示
func parquetTypeString(f parquetgo.Field) string {
	if f.Type() == nil {
		return "unknown"
	}
	return strings.ToLower(f.Type().String())
}

// mapParquetType 将 Parquet 类型映射到 ADDP 统一类型
func mapParquetType(f parquetgo.Field) format.FieldType {
	if f.Type() == nil {
		return format.FieldTypeUnknown
	}

	// 先检查逻辑类型
	lt := f.Type().LogicalType()
	if lt != nil {
		switch {
		case lt.Date != nil:
			return format.FieldTypeDate
		case lt.Time != nil:
			return format.FieldTypeTime
		case lt.Timestamp != nil:
			return format.FieldTypeTimestamp
		case lt.Decimal != nil:
			return format.FieldTypeDecimal
		case lt.UTF8 != nil:
			return format.FieldTypeString
		case lt.UUID != nil:
			return format.FieldTypeUUID
		case lt.List != nil:
			return format.FieldTypeArray
		case lt.Map != nil:
			return format.FieldTypeJSON
		}
	}

	// 按物理类型映射
	switch f.Type() {
	case parquetgo.BooleanType:
		return format.FieldTypeBool
	case parquetgo.Int32Type:
		return format.FieldTypeInt
	case parquetgo.Int64Type:
		return format.FieldTypeBigInt
	case parquetgo.FloatType:
		return format.FieldTypeFloat
	case parquetgo.DoubleType:
		return format.FieldTypeDouble
	case parquetgo.ByteArrayType:
		return format.FieldTypeString
	case parquetgo.Int96Type:
		return format.FieldTypeTimestamp
	default:
		return format.FieldTypeString
	}
}

// 确保 parquetfmt 包被使用（避免 unused import）
var _ *parquetfmt.LogicalType
