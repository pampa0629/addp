package models

import (
	"encoding/json"
	"fmt"

	"github.com/addp/common/format"
)

// ============ FieldInfo / TableColumn 转换 ============

// FieldInfoFromCommon 将 common/format.FieldInfo 转换为 manager TableColumn
func FieldInfoFromCommon(fi format.FieldInfo) TableColumn {
	return TableColumn{
		Name:         fi.Name,
		DataType:     fi.OriginalType,
		IsNullable:   fi.Nullable,
		IsPrimaryKey: fi.IsPrimaryKey,
		Comment:      fi.Comment,
		// DefaultValue 未映射，common/format.FieldInfo 中没有此字段
	}
}

// FieldInfoToCommon 将 manager TableColumn 转换为 common/format.FieldInfo
func FieldInfoToCommon(tc TableColumn) format.FieldInfo {
	// 推断统一类型（简化版，实际应根据 OriginalType 映射）
	fieldType := inferFieldType(tc.DataType)

	return format.FieldInfo{
		Name:         tc.Name,
		Type:         fieldType,
		OriginalType: tc.DataType,
		Nullable:     tc.IsNullable,
		IsPrimaryKey: tc.IsPrimaryKey,
		Comment:      tc.Comment,
	}
}

// inferFieldType 根据原始类型推断统一的字段类型（简化实现）
func inferFieldType(originalType string) format.FieldType {
	// 简单的类型推断逻辑
	switch {
	case contains(originalType, "int"):
		return format.FieldTypeInt
	case contains(originalType, "bigint"):
		return format.FieldTypeBigInt
	case contains(originalType, "float"), contains(originalType, "double"), contains(originalType, "decimal"), contains(originalType, "numeric"):
		return format.FieldTypeFloat
	case contains(originalType, "bool"):
		return format.FieldTypeBool
	case contains(originalType, "timestamp"), contains(originalType, "datetime"):
		return format.FieldTypeTimestamp
	case contains(originalType, "time"):
		return format.FieldTypeTime
	case contains(originalType, "date"):
		return format.FieldTypeDate
	case contains(originalType, "json"), contains(originalType, "jsonb"):
		return format.FieldTypeJSON
	case contains(originalType, "geometry"), contains(originalType, "geography"), contains(originalType, "point"), contains(originalType, "polygon"):
		return format.FieldTypeGeometry
	default:
		return format.FieldTypeString
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || findSubstr(s, substr))
}

func findSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ============ ManagedTable 转换方法 ============

// ToTableInfo 将 ManagedTable 转换为 common/format.TableInfo
func (m *ManagedTable) ToTableInfo() (*format.TableInfo, error) {
	var fields []format.FieldInfo

	// 解析 Schema (json.RawMessage) 到 []FieldInfo
	if m.Schema != nil && len(m.Schema) > 0 {
		// 尝试解析为 []TableColumn
		var columns []TableColumn
		if err := json.Unmarshal(m.Schema, &columns); err == nil {
			// 转换为 []FieldInfo
			fields = make([]format.FieldInfo, len(columns))
			for i, col := range columns {
				fields[i] = FieldInfoToCommon(col)
			}
		} else {
			// 如果解析失败，尝试直接解析为 []FieldInfo（兼容性）
			if err := json.Unmarshal(m.Schema, &fields); err != nil {
				return nil, fmt.Errorf("failed to unmarshal schema: %w", err)
			}
		}
	}

	// 提取主键字段
	primaryKey := make([]string, 0)
	for _, field := range fields {
		if field.IsPrimaryKey {
			primaryKey = append(primaryKey, field.Name)
		}
	}

	return &format.TableInfo{
		Name:       m.TableName,
		RowCount:   m.RowCount,
		SizeBytes:  m.TableSize,
		Fields:     fields,
		PrimaryKey: primaryKey,
	}, nil
}

// FromTableInfo 从 common/format.TableInfo 更新 ManagedTable
func (m *ManagedTable) FromTableInfo(ti *format.TableInfo) error {
	// 转换 []FieldInfo 为 []TableColumn
	columns := make([]TableColumn, len(ti.Fields))
	for i, field := range ti.Fields {
		columns[i] = FieldInfoFromCommon(field)
	}

	// 序列化为 JSON
	schemaJSON, err := json.Marshal(columns)
	if err != nil {
		return fmt.Errorf("failed to marshal schema: %w", err)
	}

	m.Schema = schemaJSON
	m.RowCount = ti.RowCount
	m.TableSize = ti.SizeBytes

	return nil
}

// ============ ManagedFile 转换方法 ============

// ToTableInfo 将 ManagedFile（结构化文件）转换为 common/format.TableInfo
func (mf *ManagedFile) ToTableInfo() (*format.TableInfo, error) {
	var fields []format.FieldInfo

	// 解析 Schema (json.RawMessage) 到 []FieldInfo
	if mf.Schema != nil && len(mf.Schema) > 0 {
		// 尝试解析为 []TableColumn
		var columns []TableColumn
		if err := json.Unmarshal(mf.Schema, &columns); err == nil {
			// 转换为 []FieldInfo
			fields = make([]format.FieldInfo, len(columns))
			for i, col := range columns {
				fields[i] = FieldInfoToCommon(col)
			}
		} else {
			// 如果解析失败，尝试直接解析为 []FieldInfo（兼容性）
			if err := json.Unmarshal(mf.Schema, &fields); err != nil {
				return nil, fmt.Errorf("failed to unmarshal schema: %w", err)
			}
		}
	}

	// 提取主键字段
	primaryKey := make([]string, 0)
	for _, field := range fields {
		if field.IsPrimaryKey {
			primaryKey = append(primaryKey, field.Name)
		}
	}

	// 文件的"表名"使用文件名
	return &format.TableInfo{
		Name:       mf.FileName,
		RowCount:   mf.RowCount,
		SizeBytes:  &mf.Size,
		Fields:     fields,
		PrimaryKey: primaryKey,
	}, nil
}

// FromTableInfo 从 common/format.TableInfo 更新 ManagedFile
func (mf *ManagedFile) FromTableInfo(ti *format.TableInfo) error {
	// 转换 []FieldInfo 为 []TableColumn
	columns := make([]TableColumn, len(ti.Fields))
	for i, field := range ti.Fields {
		columns[i] = FieldInfoFromCommon(field)
	}

	// 序列化为 JSON
	schemaJSON, err := json.Marshal(columns)
	if err != nil {
		return fmt.Errorf("failed to marshal schema: %w", err)
	}

	mf.Schema = schemaJSON
	mf.RowCount = ti.RowCount
	if ti.SizeBytes != nil {
		mf.Size = *ti.SizeBytes
	}

	return nil
}

// ============ 批量转换 ============

// FieldInfosFromCommon 批量转换 common/format.FieldInfo 数组
func FieldInfosFromCommon(fields []format.FieldInfo) []TableColumn {
	columns := make([]TableColumn, len(fields))
	for i, field := range fields {
		columns[i] = FieldInfoFromCommon(field)
	}
	return columns
}

// FieldInfosToCommon 批量转换 TableColumn 数组
func FieldInfosToCommon(columns []TableColumn) []format.FieldInfo {
	fields := make([]format.FieldInfo, len(columns))
	for i, col := range columns {
		fields[i] = FieldInfoToCommon(col)
	}
	return fields
}
