package builtin

import (
	// 导入内置类型映射器，触发 init() 自动注册
	_ "github.com/addp/common/format/mappers/mysql"
	_ "github.com/addp/common/format/mappers/postgresql"
	_ "github.com/addp/common/format/mappers/spatialite"

	// 导入内置 TableProvider，触发 init() 自动注册
	_ "github.com/addp/common/format/codecs/csv"
	_ "github.com/addp/common/format/codecs/excel"
	_ "github.com/addp/common/format/codecs/json"
	_ "github.com/addp/common/format/codecs/parquet"
	_ "github.com/addp/common/format/codecs/shapefile"
	// SQLite 目前还没有 table provider，暂不导入
	// _ "github.com/addp/common/format/codecs/sqlite"

	// 导入内置 DocumentProvider，触发 init() 自动注册
	_ "github.com/addp/common/format/codecs/text"

	// 导入内置 FileMetadataExtractor，触发 init() 自动注册
	_ "github.com/addp/common/format/codecs/image"
	_ "github.com/addp/common/format/codecs/pdf"
)

// 此包仅用于自动注册所有内置 TypeMapper、TableProvider、DocumentProvider 和 FileMetadataExtractor。
// 使用时导入即可：
//   import _ "github.com/addp/common/format/builtin"
