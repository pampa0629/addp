package format

import "github.com/addp/common/datatype"

// TableDescribeResultFromSchema converts the format operation schema into the
// common datatype describe result used by metadata attributes.
func TableDescribeResultFromSchema(info *TableInfo) *datatype.TableDescribeResult {
	if info == nil {
		return nil
	}
	result := &datatype.TableDescribeResult{
		Table:        DatatypeTableInfo(info),
		Spatial:      info.SpatialInfo.Clone(),
		ContentIndex: info.ContentIndex.Clone(),
		FormatInfo:   cloneInterfaceMap(info.FormatInfo),
	}
	return result
}

// TableSchemaFromDescribeResult converts a datatype describe result back to
// the format operation schema needed by readers and writers.
func TableSchemaFromDescribeResult(result *datatype.TableDescribeResult) *TableInfo {
	if result == nil {
		return nil
	}
	info := FormatTableInfo(result.Table)
	info.SpatialInfo = result.Spatial.Clone()
	info.FormatInfo = cloneInterfaceMap(result.FormatInfo)
	info.ContentIndex = result.ContentIndex.Clone()
	return info
}

func DatatypeTableInfo(info *TableInfo) *datatype.TableInfo {
	if info == nil {
		return nil
	}
	return &datatype.TableInfo{
		Name:       info.Name,
		RowCount:   info.RowCount,
		SizeBytes:  info.SizeBytes,
		CreatedAt:  info.CreatedAt,
		UpdatedAt:  info.UpdatedAt,
		Fields:     DatatypeFieldInfos(info.Fields),
		PrimaryKey: append([]string(nil), info.PrimaryKey...),
	}
}

func FormatTableInfo(info *datatype.TableInfo) *TableInfo {
	if info == nil {
		return &TableInfo{}
	}
	return &TableInfo{
		Name:       info.Name,
		RowCount:   info.RowCount,
		SizeBytes:  info.SizeBytes,
		CreatedAt:  info.CreatedAt,
		UpdatedAt:  info.UpdatedAt,
		Fields:     FormatFieldInfos(info.Fields),
		PrimaryKey: append([]string(nil), info.PrimaryKey...),
	}
}

func DatatypeFieldInfos(fields []FieldInfo) []datatype.FieldInfo {
	if len(fields) == 0 {
		return nil
	}
	result := make([]datatype.FieldInfo, 0, len(fields))
	for i, field := range fields {
		result = append(result, datatype.FieldInfo{
			Name:            field.Name,
			Type:            field.Type,
			Nullable:        field.Nullable,
			PrimaryKey:      field.IsPrimaryKey,
			Comment:         field.Comment,
			Size:            datatypeFieldSize(field),
			Precision:       datatypeFieldPrecision(field),
			Scale:           datatypeFieldScale(field),
			OrdinalPosition: i + 1,
		})
	}
	return result
}

func FormatFieldInfos(fields []datatype.FieldInfo) []FieldInfo {
	if len(fields) == 0 {
		return nil
	}
	result := make([]FieldInfo, 0, len(fields))
	for _, field := range fields {
		size := field.Size
		if size == 0 {
			size = field.Precision
		}
		result = append(result, FieldInfo{
			Name:         field.Name,
			Type:         field.Type,
			Nullable:     field.Nullable,
			IsPrimaryKey: field.PrimaryKey,
			Comment:      field.Comment,
			Size:         size,
			Precision:    field.Scale,
		})
	}
	return result
}

func datatypeFieldSize(field FieldInfo) int {
	switch field.Type {
	case datatype.FieldTypeString, datatype.FieldTypeBytes:
		return field.Size
	default:
		return 0
	}
}

func datatypeFieldPrecision(field FieldInfo) int {
	switch field.Type {
	case datatype.FieldTypeInt, datatype.FieldTypeBigInt, datatype.FieldTypeFloat, datatype.FieldTypeDouble, datatype.FieldTypeDecimal:
		return field.Size
	default:
		return 0
	}
}

func datatypeFieldScale(field FieldInfo) int {
	switch field.Type {
	case datatype.FieldTypeFloat, datatype.FieldTypeDouble, datatype.FieldTypeDecimal:
		return field.Precision
	default:
		return 0
	}
}

func cloneInterfaceMap(values map[string]interface{}) map[string]interface{} {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]interface{}, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
