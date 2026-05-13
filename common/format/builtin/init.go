package builtin

import (
	// 导入内置类型映射器，触发 init() 自动注册
	_ "github.com/addp/common/format/mappers/mysql"
	_ "github.com/addp/common/format/mappers/postgresql"
	_ "github.com/addp/common/format/mappers/spatialite"

	// 导入内置 TableProvider，触发 init() 自动注册
	_ "github.com/addp/common/format/plugins/csv"
	_ "github.com/addp/common/format/plugins/excel"
	_ "github.com/addp/common/format/plugins/json"
	_ "github.com/addp/common/format/plugins/parquet"
	_ "github.com/addp/common/format/plugins/shapefile"
	_ "github.com/addp/common/format/plugins/sqlite"
	_ "github.com/addp/common/format/plugins/zip"

	// 导入内置文档 info provider / text reader，触发 init() 自动注册
	_ "github.com/addp/common/format/plugins/text"

	// 导入内置 FileMetadataExtractor，触发 init() 自动注册
	_ "github.com/addp/common/format/plugins/image"
	_ "github.com/addp/common/format/plugins/pdf"
)

// 此包仅用于自动注册所有内置 TypeMapper、格式 provider / reader 和 FileMetadataExtractor。
// 使用时导入即可：
//   import _ "github.com/addp/common/format/builtin"
