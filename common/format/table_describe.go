package format

import "github.com/addp/common/datatype"

// TableDescribeResultFromTableInfo wraps table type facts in a format describe result.
func TableDescribeResultFromTableInfo(info *datatype.TableInfo) *TableDescribeResult {
	if info == nil {
		return nil
	}
	result := &TableDescribeResult{
		Table: info.Clone(),
	}
	return result
}

// TableInfoFromDescribeResult returns the table type facts from a describe result.
func TableInfoFromDescribeResult(result *TableDescribeResult) *datatype.TableInfo {
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
