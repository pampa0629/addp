package builtin

import (
	// 导入内置类型映射器，触发 init() 自动注册
	_ "github.com/addp/common/format/mappers/mysql"
	_ "github.com/addp/common/format/mappers/postgresql"
	_ "github.com/addp/common/format/mappers/spatialite"

	// 导入内置 table 格式能力，触发 init() 自动注册
	_ "github.com/addp/common/format/plugins/avro"
	_ "github.com/addp/common/format/plugins/csv"
	_ "github.com/addp/common/format/plugins/excel"
	_ "github.com/addp/common/format/plugins/geojson"
	_ "github.com/addp/common/format/plugins/glb"
	_ "github.com/addp/common/format/plugins/json"
	_ "github.com/addp/common/format/plugins/las"
	_ "github.com/addp/common/format/plugins/orc"
	_ "github.com/addp/common/format/plugins/osgb"
	_ "github.com/addp/common/format/plugins/parquet"
	_ "github.com/addp/common/format/plugins/rastermosaic"
	_ "github.com/addp/common/format/plugins/shapefile"
	_ "github.com/addp/common/format/plugins/sqlite"
	_ "github.com/addp/common/format/plugins/unknown"
	_ "github.com/addp/common/format/plugins/zip"

	// 导入内置文档 info provider / text reader，触发 init() 自动注册
	_ "github.com/addp/common/format/plugins/docx"
	_ "github.com/addp/common/format/plugins/pdf"
	_ "github.com/addp/common/format/plugins/pptx"
	_ "github.com/addp/common/format/plugins/text"
	_ "github.com/addp/common/format/plugins/tiles3d"
	_ "github.com/addp/common/format/plugins/wps"

	// 导入内置媒体信息 provider，触发 init() 自动注册
	_ "github.com/addp/common/format/plugins/image"
	_ "github.com/addp/common/format/plugins/media"
)

// 此包仅用于自动注册所有内置 TypeMapper、格式 provider / reader。
// 使用时导入即可：
//   import _ "github.com/addp/common/format/builtin"
