package postprocessor

import (
	"fmt"
	"strings"

	"github.com/addp/transfer/pkg/pipeline"
)

// extractPrimaryKeyMetadata 从配置中提取主键元数据
// 采用四级降级策略，优先级从高到低：
//   Level 0: ForcePrimaryKey（强制指定，最高优先级）
//   Level 1: Schema.PrimaryKey（新格式，标准方式）
//   Level 2: Schema.Metadata["table_metadata"]["primary_key"]（旧格式，向后兼容）
//   Level 3: Schema.Fields[].ExtendedAttributes["is_primary_key"]（字段级推断）
//   Level 4: nil（未检测到主键）
//
// config: 后处理器配置
// 返回：主键元数据或 nil
func extractPrimaryKeyMetadata(config PostProcessorConfig) *PrimaryKeyMetadata {
	// Level 0: 强制指定主键（最高优先级，用于覆盖自动检测）
	if len(config.ForcePrimaryKey) > 0 {
		return &PrimaryKeyMetadata{
			Columns: config.ForcePrimaryKey,
			Name:    config.PrimaryKeyName,
		}
	}

	if config.Schema == nil {
		return nil
	}

	// Level 1: 从 Schema.PrimaryKey 提取（新格式，标准方式）
	if config.Schema.PrimaryKey != nil && len(config.Schema.PrimaryKey.Columns) > 0 {
		pkMeta := &PrimaryKeyMetadata{
			Columns: config.Schema.PrimaryKey.Columns,
			Name:    config.Schema.PrimaryKey.Name,
		}
		// 如果配置中指定了约束名，覆盖 Schema 中的名称
		if config.PrimaryKeyName != "" {
			pkMeta.Name = config.PrimaryKeyName
		}
		return pkMeta
	}

	// Level 2: 从 Schema.Metadata["table_metadata"]["primary_key"] 提取（旧格式，向后兼容）
	if config.Schema.Metadata != nil {
		if tableMeta, ok := config.Schema.Metadata["table_metadata"].(map[string]interface{}); ok {
			// 检查是否有主键标志
			if hasPK, _ := tableMeta["has_primary_key"].(bool); hasPK {
				columns := []string{}

				// 提取主键列
				if pkCols, ok := tableMeta["primary_key"].([]interface{}); ok {
					for _, col := range pkCols {
						if colName, ok := col.(string); ok && colName != "" {
							columns = append(columns, colName)
						}
					}
				}

				if len(columns) > 0 {
					pkName, _ := tableMeta["primary_key_name"].(string)
					// 配置中的约束名优先
					if config.PrimaryKeyName != "" {
						pkName = config.PrimaryKeyName
					}
					return &PrimaryKeyMetadata{
						Columns: columns,
						Name:    pkName,
					}
				}
			}
		}
	}

	// Level 3: 从 Field.ExtendedAttributes["is_primary_key"] 提取（字段级推断）
	if config.Schema.Fields != nil {
		var pkColumns []string
		for _, field := range config.Schema.Fields {
			if field.ExtendedAttributes != nil {
				if isPK, ok := field.ExtendedAttributes["is_primary_key"].(bool); ok && isPK {
					pkColumns = append(pkColumns, field.Name)
				}
			}
		}
		if len(pkColumns) > 0 {
			return &PrimaryKeyMetadata{
				Columns: pkColumns,
				Name:    config.PrimaryKeyName, // 使用配置中的约束名（可能为空）
			}
		}
	}

	// Level 4: 未检测到主键
	return nil
}

// extractSpatialIndexMetadata 从配置中提取空间索引元数据
// config: 后处理器配置
// 返回：空间索引元数据列表
func extractSpatialIndexMetadata(config PostProcessorConfig) []SpatialIndexMetadata {
	if len(config.GeometryColumns) == 0 {
		return nil
	}

	indexes := make([]SpatialIndexMetadata, 0, len(config.GeometryColumns))
	_, tableName := splitTableName(config.TableName)

	for _, geomMeta := range config.GeometryColumns {
		idx := SpatialIndexMetadata{
			ColumnName: geomMeta.ColumnName,
			SRID:       geomMeta.SRID,
		}

		// 生成索引名（如果未指定）
		// 格式：{table}_{column}_idx 或 {prefix}_{column}_idx
		if config.SpatialIndexPrefix != "" {
			idx.IndexName = fmt.Sprintf("%s_%s_idx", config.SpatialIndexPrefix, geomMeta.ColumnName)
		} else {
			idx.IndexName = fmt.Sprintf("%s_%s_idx", tableName, geomMeta.ColumnName)
		}

		indexes = append(indexes, idx)
	}

	return indexes
}

// ConvertGeometryColumnsMap 将 Writer 的 geometryColumns 转换为 PostProcessor 格式
// 这个函数用于适配 Writer 内部的 geometryColumns 格式到 PostProcessor 的配置格式
//
// writerGeomCols: Writer 内部的几何列映射（map[列名]元数据）
// 返回：PostProcessor 配置格式的几何列映射
func ConvertGeometryColumnsMap(writerGeomCols map[string]struct {
	SRID        int
	SpatialType string
}) map[string]GeometryColumnMetadata {
	result := make(map[string]GeometryColumnMetadata, len(writerGeomCols))
	for colName, meta := range writerGeomCols {
		result[colName] = GeometryColumnMetadata{
			ColumnName:  colName,
			SRID:        meta.SRID,
			SpatialType: meta.SpatialType,
		}
	}
	return result
}

// generatePrimaryKeyConstraintName 生成主键约束名
// pk: 主键元数据
// tableName: 表名
// 返回：约束名（如果 pk.Name 已指定则返回，否则生成默认格式：{table}_pkey）
func generatePrimaryKeyConstraintName(pk *PrimaryKeyMetadata, tableName string) string {
	if pk.Name != "" {
		return pk.Name
	}
	// 默认格式：{table}_pkey
	_, table := splitTableName(tableName)
	return fmt.Sprintf("%s_pkey", table)
}

// validatePrimaryKeyColumns 验证主键列是否存在于 Schema 中
// pk: 主键元数据
// schema: Schema 信息
// 返回：true 表示所有主键列都存在
func validatePrimaryKeyColumns(pk *PrimaryKeyMetadata, schema *pipeline.Schema) bool {
	if pk == nil || len(pk.Columns) == 0 || schema == nil || len(schema.Fields) == 0 {
		return false
	}

	// 构建字段名集合
	fieldSet := make(map[string]bool)
	for _, field := range schema.Fields {
		fieldSet[strings.ToLower(field.Name)] = true
	}

	// 验证所有主键列是否存在
	for _, col := range pk.Columns {
		if !fieldSet[strings.ToLower(col)] {
			return false
		}
	}

	return true
}
