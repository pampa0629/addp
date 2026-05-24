package metaattr

import "github.com/addp/common/datatype"

func TableInfoAttributes(info *datatype.TableInfo) map[string]interface{} {
	return datatype.TableInfoAttributes(info)
}
