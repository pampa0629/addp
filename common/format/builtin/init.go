package builtin

import (
	// 导入内置类型映射器，触发 init() 自动注册
	_ "github.com/addp/common/format/mappers/mysql"
	_ "github.com/addp/common/format/mappers/postgresql"

	// 导入内置 TableProvider，触发 init() 自动注册
	_ "github.com/addp/common/format/csv"
	_ "github.com/addp/common/format/excel"
	_ "github.com/addp/common/format/geojson"
	_ "github.com/addp/common/format/shapefile"
	// SQLite parser 暂未实现新接口，暂不导入
	// _ "github.com/addp/common/format/sqlite"

	// 导入内置 FileMetadataExtractor，触发 init() 自动注册
	_ "github.com/addp/common/format/image"
	_ "github.com/addp/common/format/pdf"
)

// 此包仅用于自动注册所有内置解析器（TypeMapper、TableProvider、FileMetadataExtractor）
// 使用时导入即可：
//   import _ "github.com/addp/common/format/builtin"
