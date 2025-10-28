package builtin

import (
	// 导入内置类型映射器，触发 init() 自动注册
	_ "github.com/addp/common/database/postgresql"
	_ "github.com/addp/common/database/mysql"
	_ "github.com/addp/common/geo/shapefile"
)

// 此包仅用于自动注册所有内置类型映射器
// 使用时导入即可：
//   import _ "github.com/addp/common/format/builtin"
