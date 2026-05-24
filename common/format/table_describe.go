package format

import "github.com/addp/common/datatype"

// TableDescribeResultFromSchema converts the format operation schema into the
// common datatype describe result used by metadata attributes.
func TableDescribeResultFromSchema(info *TableInfo) *TableDescribeResult {
	if info == nil {
		return nil
	}
	result := &TableDescribeResult{
		Table: DatatypeTableInfo(info),
	}
	return result
}

// TableSchemaFromDescribeResult converts a datatype describe result back to
// the format operation schema needed by readers and writers.
func TableSchemaFromDescribeResult(result *TableDescribeResult) *TableInfo {
	if result == nil {
		return nil
	}
	return FormatTableInfo(result.Table)
}

func DatatypeTableInfo(info *TableInfo) *datatype.TableInfo {
	if info == nil {
		return nil
	}
	return info.TableInfo.Clone()
}

func FormatTableInfo(info *datatype.TableInfo) *TableInfo {
	if info == nil {
		return &TableInfo{}
	}
	cloned := info.Clone()
	return &TableInfo{
		TableInfo: *cloned,
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
