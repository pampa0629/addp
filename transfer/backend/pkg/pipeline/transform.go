package pipeline

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// TransformPipeline 转换管道
type TransformPipeline struct {
	transforms []Transform
}

// NewTransformPipeline 创建转换管道
func NewTransformPipeline(transforms ...Transform) *TransformPipeline {
	return &TransformPipeline{
		transforms: transforms,
	}
}

// Apply 应用所有转换
func (p *TransformPipeline) Apply(ctx context.Context, batch *DataBatch) (*DataBatch, error) {
	var err error
	for _, transform := range p.transforms {
		batch, err = transform.Apply(ctx, batch)
		if err != nil {
			return nil, fmt.Errorf("transform %s failed: %w", transform.Name(), err)
		}
	}
	return batch, nil
}

// Name 返回管道名称
func (p *TransformPipeline) Name() string {
	return "TransformPipeline"
}

// AddTransform 添加转换器
func (p *TransformPipeline) AddTransform(transform Transform) {
	p.transforms = append(p.transforms, transform)
}

// FieldMapping 字段映射配置
type FieldMapping struct {
	Source       string      // 源字段名
	Target       string      // 目标字段名
	Type         string      // 目标类型（string, int, float, bool, datetime）
	Format       string      // 格式（用于日期等）
	DefaultValue interface{} // 默认值
	Transform    string      // 转换函数（可选）
}

// FieldMappingTransform 字段映射转换器
type FieldMappingTransform struct {
	mappings []FieldMapping
}

// NewFieldMappingTransform 创建字段映射转换器
func NewFieldMappingTransform(mappings []FieldMapping) *FieldMappingTransform {
	return &FieldMappingTransform{
		mappings: mappings,
	}
}

// Apply 应用字段映射
func (t *FieldMappingTransform) Apply(ctx context.Context, batch *DataBatch) (*DataBatch, error) {
	if len(t.mappings) == 0 {
		return batch, nil
	}

	newRows := make([]map[string]interface{}, 0, len(batch.Rows))

	for _, row := range batch.Rows {
		newRow := make(map[string]interface{})

		for _, mapping := range t.mappings {
			value, exists := row[mapping.Source]
			usedDefault := false

			// 使用默认值
			if !exists || value == nil {
				value = mapping.DefaultValue
				usedDefault = true
			}

			value = normalizeEmptyValue(value, usedDefault)

			// 类型转换
			if value != nil && mapping.Type != "" {
				var err error
				value, err = convertType(value, mapping.Type, mapping.Format)
				if err != nil {
					return nil, fmt.Errorf("type conversion failed for field %s: %w", mapping.Source, err)
				}
			}

			newRow[mapping.Target] = value
		}

		newRows = append(newRows, newRow)
	}

	batch.Rows = newRows
	return batch, nil
}

// Name 返回转换器名称
func (t *FieldMappingTransform) Name() string {
	return "FieldMapping"
}

// FilterCondition 过滤条件
type FilterCondition struct {
	Field    string      // 字段名
	Operator string      // 操作符（eq, ne, gt, lt, gte, lte, in, not_in, contains）
	Value    interface{} // 比较值
}

// FilterTransform 过滤转换器
type FilterTransform struct {
	conditions []FilterCondition
	mode       string // "and" 或 "or"
}

// NewFilterTransform 创建过滤转换器
func NewFilterTransform(conditions []FilterCondition, mode string) *FilterTransform {
	if mode == "" {
		mode = "and"
	}
	return &FilterTransform{
		conditions: conditions,
		mode:       mode,
	}
}

// Apply 应用过滤
func (t *FilterTransform) Apply(ctx context.Context, batch *DataBatch) (*DataBatch, error) {
	if len(t.conditions) == 0 {
		return batch, nil
	}

	filteredRows := make([]map[string]interface{}, 0, len(batch.Rows))

	for _, row := range batch.Rows {
		if t.matchRow(row) {
			filteredRows = append(filteredRows, row)
		}
	}

	batch.Rows = filteredRows
	return batch, nil
}

// matchRow 判断行是否匹配条件
func (t *FilterTransform) matchRow(row map[string]interface{}) bool {
	if len(t.conditions) == 0 {
		return true
	}

	if t.mode == "or" {
		// OR 模式：任一条件满足即可
		for _, cond := range t.conditions {
			if t.matchCondition(row, cond) {
				return true
			}
		}
		return false
	} else {
		// AND 模式：所有条件都要满足
		for _, cond := range t.conditions {
			if !t.matchCondition(row, cond) {
				return false
			}
		}
		return true
	}
}

// matchCondition 判断单个条件
func (t *FilterTransform) matchCondition(row map[string]interface{}, cond FilterCondition) bool {
	value, exists := row[cond.Field]
	if !exists {
		return false
	}

	switch cond.Operator {
	case "eq":
		return fmt.Sprintf("%v", value) == fmt.Sprintf("%v", cond.Value)
	case "ne":
		return fmt.Sprintf("%v", value) != fmt.Sprintf("%v", cond.Value)
	case "gt":
		return compareValues(value, cond.Value) > 0
	case "lt":
		return compareValues(value, cond.Value) < 0
	case "gte":
		return compareValues(value, cond.Value) >= 0
	case "lte":
		return compareValues(value, cond.Value) <= 0
	case "contains":
		return strings.Contains(fmt.Sprintf("%v", value), fmt.Sprintf("%v", cond.Value))
	default:
		return false
	}
}

// Name 返回转换器名称
func (t *FilterTransform) Name() string {
	return "Filter"
}

// normalizeEmptyValue 统一空值处理，避免将空字符串写入数值型列
func normalizeEmptyValue(value interface{}, usedDefault bool) interface{} {
	if value == nil {
		return nil
	}

	switch v := value.(type) {
	case string:
		if strings.TrimSpace(v) == "" && usedDefault {
			return nil
		}
	case []byte:
		if len(v) == 0 && usedDefault {
			return nil
		}
	}

	return value
}

// convertType 类型转换
func convertType(value interface{}, targetType, format string) (interface{}, error) {
	if value == nil {
		return nil, nil
	}

	// 空间类型保持原始 []byte 格式，不做转换
	if targetType == "geometry" {
		// 如果已经是 []byte（WKB 格式），直接返回
		if b, ok := value.([]byte); ok {
			return b, nil
		}
		// 如果是字符串（WKT 或 HEXEWKB），保持字符串
		if s, ok := value.(string); ok {
			return s, nil
		}
		return value, nil
	}

	valueStr := fmt.Sprintf("%v", value)

	switch targetType {
	case "string":
		return valueStr, nil

	case "int", "integer":
		if v, ok := value.(int64); ok {
			return v, nil
		}
		if v, ok := value.(int); ok {
			return int64(v), nil
		}
		return strconv.ParseInt(valueStr, 10, 64)

	case "float":
		if v, ok := value.(float64); ok {
			return v, nil
		}
		return strconv.ParseFloat(valueStr, 64)

	case "bool", "boolean":
		if v, ok := value.(bool); ok {
			return v, nil
		}
		return strconv.ParseBool(valueStr)

	case "datetime", "date", "timestamp":
		if v, ok := value.(time.Time); ok {
			return v, nil
		}
		if format == "" {
			format = time.RFC3339
		}
		return time.Parse(format, valueStr)

	case "json":
		// JSON 类型保持原样，可能是 string 或已解析的对象
		return value, nil

	default:
		return value, nil
	}
}

// compareValues 比较值
func compareValues(a, b interface{}) int {
	aStr := fmt.Sprintf("%v", a)
	bStr := fmt.Sprintf("%v", b)

	// 尝试数值比较
	if aFloat, err := strconv.ParseFloat(aStr, 64); err == nil {
		if bFloat, err := strconv.ParseFloat(bStr, 64); err == nil {
			if aFloat > bFloat {
				return 1
			} else if aFloat < bFloat {
				return -1
			}
			return 0
		}
	}

	// 字符串比较
	return strings.Compare(aStr, bStr)
}

// RenameFieldsTransform 重命名字段转换器
type RenameFieldsTransform struct {
	mappings map[string]string // old_name -> new_name
}

// NewRenameFieldsTransform 创建重命名字段转换器
func NewRenameFieldsTransform(mappings map[string]string) *RenameFieldsTransform {
	return &RenameFieldsTransform{
		mappings: mappings,
	}
}

// Apply 应用字段重命名
func (t *RenameFieldsTransform) Apply(ctx context.Context, batch *DataBatch) (*DataBatch, error) {
	for _, row := range batch.Rows {
		for oldName, newName := range t.mappings {
			if value, exists := row[oldName]; exists {
				row[newName] = value
				delete(row, oldName)
			}
		}
	}
	return batch, nil
}

// Name 返回转换器名称
func (t *RenameFieldsTransform) Name() string {
	return "RenameFields"
}

// SelectFieldsTransform 选择字段转换器（只保留指定字段）
type SelectFieldsTransform struct {
	fields []string
}

// NewSelectFieldsTransform 创建选择字段转换器
func NewSelectFieldsTransform(fields []string) *SelectFieldsTransform {
	return &SelectFieldsTransform{
		fields: fields,
	}
}

// Apply 应用字段选择
func (t *SelectFieldsTransform) Apply(ctx context.Context, batch *DataBatch) (*DataBatch, error) {
	if len(t.fields) == 0 {
		return batch, nil
	}

	newRows := make([]map[string]interface{}, 0, len(batch.Rows))

	for _, row := range batch.Rows {
		newRow := make(map[string]interface{})
		for _, field := range t.fields {
			if value, exists := row[field]; exists {
				newRow[field] = value
			}
		}
		newRows = append(newRows, newRow)
	}

	batch.Rows = newRows
	return batch, nil
}

// Name 返回转换器名称
func (t *SelectFieldsTransform) Name() string {
	return "SelectFields"
}
