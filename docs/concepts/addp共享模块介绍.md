## Common 模块

`common` 模块提供共享代码,避免**所有其他后端模块**之间的重复 (Manager、Meta、Transfer、Orchestrator、Develop 和 Python Workflow Engine 集成)。

**内容**:

- [client/system.go](common/client/system.go) - SystemClient 用于与 System 模块通信
- [models/engine.go](common/models/engine.go) - 共享的 Engine 模型和 `connection_info` 结构
- [config/loader.go](common/config/loader.go) - 集中式配置加载,带回退
- `common/jsonmap` - decoded JSON map 的通用读取工具,不承载 `meta_item.attributes` 业务规范
- `common/contentio` - 基于 Go `io` 的内容定位与读写抽象，负责 `Ref`、`Reader`、`Writer`、`Lister`、`RangeReader` 和 `Stat`
- `common/format` - 通用文件格式、FormatCapability、类型信息、格式信息、format plugin、info provider、content reader 和 writer/provider
- `common/dataitem` - 候选内容集合到 data item 组织结果的通用解析能力，供 Meta 扫描和 Manager 容器动态预览复用；当前已落地 `ResolveItems()`、single / multi / whole 规则派生、related refs 还原 helper 和基础忽略策略
- [client/meta.go](common/client/meta.go) - MetaClient 用于跨模块调用 Meta API，Manager 等模块不应保留私有 Meta API client

**使用模式**:

```go
// 在模块的 go.mod 中
require (github.com/addp/common v0.0.0)
replace github.com/addp/common => ../../common

// 使用别名导入以避免冲突
import (
    commonClient "github.com/addp/common/client"
)

// 使用 SystemClient 获取引擎
client := commonClient.NewSystemClient(systemURL, jwtToken)
engines, err := client.ListEngines("postgresql")
engine, err := client.GetEngine(engineID)

// 使用 connection_info 作为连接信息事实源
// 需要底层 driver DSN 的数据库类引擎，由对应 engine plugin 的 DSNProvider.BuildDSN() 构建
connInfo := engine.ConnectionInfo
```

**关键设计原则**:

- 最小外部依赖 (仅 Go 标准库)
- 所有模块使用相同的 SystemClient 实现
- Engine 模型在所有服务中是规范的
- `connection_info` 是所有引擎连接信息的统一事实源；DSN 不是所有引擎的通用抽象
- 通用数据类型、格式能力、内容 I/O 抽象和候选内容组织规则可以放入 common；Meta 仍负责扫描调度、最终裁决、claims / exclusive 合并、`meta_item.full_name` 落库决策和 attributes normalizer。`common/dataitem` 已作为共享组织层落地，`common/contentio` 负责底层内容定位与读写抽象，详细边界见 [数据项体系图](addp数据项体系图.md)、[数据项探测器规范](../spec/addp数据项探测器规范.md) 和 [内容 I/O 抽象规范](../spec/addp内容IO抽象规范.md)
- common 的破坏性更改会影响所有模块 - 彻底测试

**另请参阅**: [docs/COMMON_MODULE.md](docs/COMMON_MODULE.md)

## Common Frontend

`common-frontend` 模块提供共享的 Vue 3 组件、工具和类型定义,供跨模块的前端复用。

**架构**: 分为两个子模块以避免不必要的依赖:

```
common-frontend/
├── basic/          # 基础 UI 组件 (无地图依赖)
│   └── src/
│       ├── components/  - EngineForm, ImagePreview, ResourceTree
│       ├── utils/       - 格式化器, 类型工具
│       ├── types/       - FieldType, FormatType, EngineType
│       └── index.js
│
└── map/            # 地图相关组件 (需要 ol 和 @amap/amap-jsapi-loader)
    └── src/
        ├── components/  - MapContainer, GeoJsonPreview, TablePreview
        ├── composables/ - useMapConfig, useGaodeMap, useOpenLayersMap
        └── utils/       - 地理工具, 格式化器
```

**使用模式**:

**对于无地图功能的模块** (System, Transfer):

```javascript
// vite.config.js
resolve: {
  alias: {
    '@common-ui': resolve(__dirname, '../../common-frontend/basic/src')
  }
}

// 在组件中
import { EngineForm, ImagePreview } from '@common-ui'
import { formatFileSize, formatDateTime } from '@common-ui'
```

**对于有地图功能的模块** (Manager):

```javascript
// vite.config.js
resolve: {
  alias: {
    '@common-ui-map': resolve(__dirname, '../../common-frontend/map/src')
  }
}

// package.json 依赖
{
  "ol": "^9.2.4",
  "@amap/amap-jsapi-loader": "^1.0.1"
}

// 在组件中
import { TablePreview, GeoJsonPreview } from '@common-ui-map'
```

**关键组件**:

- **预览组件**: GeoJsonPreview, TablePreview, ImagePreview
- **表单组件**: EngineForm (PostgreSQL/MinIO/S3 配置)
- **地图组件**: MapContainer, OpenLayersRenderer, GaodeMapRenderer
- **工具**: formatFileSize, formatDateTime, detectFormatByExtension, isGeospatialFormat
- **类型**: FieldType, FormatType, EngineType (与后端模型对齐)

**优势**:

- ✅ **模块化依赖**: 模块只安装需要的内容
- ✅ **减小打包体积**: 基础模块通过排除地图库节省约 2-3MB
- ✅ **类型安全**: 共享的类型定义确保前后端一致性
- ✅ **DRY 合规**: UI 组件复用而非复制
- ✅ **统一维护**: 所有共享组件集中在一处

**模块使用**:

- **System Frontend**: 使用 `basic` (引擎配置的 EngineForm)
- **Manager Frontend**: 使用 `map` (数据预览的 GeoJsonPreview, TablePreview)
- **Meta Frontend**: 使用 `basic` (通用资源树和基础 UI)
- **Transfer Frontend**: 使用 `basic` (映射 UI 的字段类型工具)
- **Console Frontend**: 使用 `basic` (通用 UI 元素)

**另请参阅**: [common-frontend/README.md](common-frontend/README.md), [common-frontend/ARCHITECTURE.md](common-frontend/ARCHITECTURE.md)
