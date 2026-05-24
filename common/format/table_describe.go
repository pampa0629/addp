package format

import "github.com/addp/common/datatype"

// TableDescribeResultFromSchema converts the format operation schema into the
// common datatype describe result used by metadata attributes.
func TableDescribeResultFromSchema(info *datatype.TableInfo) *TableDescribeResult {
	if info == nil {
		return nil
	}
	result := &TableDescribeResult{
		Table: info.Clone(),
	}
	return result
}

// TableSchemaFromDescribeResult converts a datatype describe result back to
// the format operation schema needed by readers and writers.
func TableSchemaFromDescribeResult(result *TableDescribeResult) *datatype.TableInfo {
	if result == nil {
		return nil
	}
	if result.Table == nil {
		return &datatype.TableInfo{}
	}
	return result.Table.Clone()
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
