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
		Fields:     append([]datatype.FieldInfo(nil), info.Fields...),
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
		Fields:     append([]datatype.FieldInfo(nil), info.Fields...),
		PrimaryKey: append([]string(nil), info.PrimaryKey...),
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
