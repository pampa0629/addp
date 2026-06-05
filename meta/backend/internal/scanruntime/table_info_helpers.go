package scanruntime

import (
	"github.com/addp/common/datatype"
	commonJSON "github.com/addp/common/jsonmap"
)

func tableInfoFromMetaAttributes(attrs map[string]interface{}) *datatype.TableInfo {
	return datatype.TableInfoFromPayload(commonJSON.Section(attrs, "type_info.table"), "")
}
