## Common 模块

`common` 模块提供共享代码,避免**所有其他后端模块**之间的重复 (Manager、Meta、Transfer、Orchestrator、Develop 和 GeoPython Workflow Engine 集成)。

**内容**:

- [client/system.go](common/client/system.go) - SystemClient 用于与 System 模块通信
- [models/engine.go](common/models/engine.go) - 共享的 Engine 模型和 `connection_info` 结构
- [config/loader.go](common/config/loader.go) - 集中式配置加载,带回退
- `common/jsonmap` - decoded JSON map 的通用读取工具,不承载 `meta_item.attributes` 业务规范
- `common/contentio` - 基于 Go `io` 的内容定位与读写抽象，负责 `Ref`、`Reader`、`Writer`、`Lister`、`RangeReader` 和 `Stat`
- `common/format` - 通用文件格式、FormatDescriptor、格式信息、format plugin、info provider、content reader 和 writer/provider
- `common/dataitem` - 候选内容集合到 data item 组织结果的通用解析能力，供 Meta 扫描和 Manager 容器动态预览复用；当前已落地 `ResolveItems()`、single / multi / whole 规则派生、related refs 还原 helper 和基础忽略策略
- `common/resourcetree` - Meta 已落库 catalog / item 事实到跨模块资源树视图的投影层，提供 `TreeNode`、`TreeBuilder`、`ResourceLocator` 和 provider `CatalogPath` 纯转换能力；不持有 System / Meta client，不主动读取远程服务，不处理租户权限、token、降级策略、扫描或内容读取
- [client/meta.go](common/client/meta.go) - MetaClient 用于跨模块调用 Meta API，Manager 等模块不应保留私有 Meta API client
- `common/engine/workflowaccess` - 把已解析的文件、对象或目录型存储资源转换为 `addp.workflow.access-plan/v1` 执行计划和脱敏审计计划；不保存任务定义、不决定产物归属，也不触发 Meta scan

`common-python/addp_common/workflow_runtime` 提供 Python Workflow Runtime 的协议执行核心，包括 workflow definition 校验、DAG 拓扑排序、引用解析、异步 execution 状态和标准错误。它是共享库，不是独立工作流引擎；各运行时仍负责自己的算子注册、内存对象类型和专业执行依赖。

`common-python/addp_common/tools` 提供 `addp.tool-manifest/v1`、Manifest 校验和 ToolExecutor。Python SDK 是唯一 HTTP Client 实现；`addp` CLI、ADDP Agent LangChain Tool Provider 和后续 MCP Adapter 只能调用 ToolExecutor，不得各自维护 HTTP 路径、认证或业务判断。ToolExecutor 在每次 Tool 调用边界向 System 换取绑定 owner audience、稳定 Tool Scope、AgentRun 和 ToolCall 的短期 Delegated Access Token，原始 User Access Token 不进入 owner Client。Internal API Key 调用需要租户上下文时，统一通过共享 Client 发送 `X-Tenant-ID`，不得把客户端提交的 tenant 当成已验证身份。

### 资源树与 locator 共享边界

资源树和 ResourceLocator 是平台级公共契约，但它们分为三层职责：

1. `common/resourcetree` 只提供纯模型与转换能力，包括 `ResourceLocator` 解析 / 构造、`TreeNode` 和 `TreeBuilder`。它不查询 System / Meta，不处理权限、租户、扫描或业务能力。
2. Meta Backend 提供资源树事实 API：`/api/v1/meta/resource-tree/{engine_id}`、`/node`、`/ancestors`、`/search`、`/refresh`。这些 API 读取已落库 `meta_node` / `meta_item`，并返回标准 `TreeNode` / ancestors 结果。
3. Manager、Transfer、Develop、Service、Agent 等模块只消费 locator 和 Meta resource-tree API。模块可以在业务 DTO 中保存执行快照或 capability 结果，但不得复制 TreeNode、locator parser / builder，也不得恢复模块私有资源树事实入口。

公共模型字段不足时，应优先扩展 `common/resourcetree.TreeNode`、`ResourceLocator` 或对应前端共享类型。只有业务动作和执行快照才放在业务 DTO 中，不能替代公共资源身份。

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
- 通用数据类型、格式能力、内容 I/O 抽象、资源树投影和候选内容组织规则可以放入 common；Meta 仍负责扫描调度、最终裁决、claims / exclusive 合并、`meta_item.full_name` 落库决策和 attributes normalizer。`common/resourcetree` 只消费已落库事实并生成资源树 / locator 视图，`common/dataitem` 已作为共享组织层落地，`common/contentio` 负责底层内容定位与读写抽象，详细边界见 [数据项体系图](addp数据项体系图.md)、[数据项探测器规范](../spec/addp数据项探测器规范.md) 和 [内容 I/O 抽象规范](../spec/addp内容IO抽象规范.md)
- common 的破坏性更改会影响所有模块 - 彻底测试

**另请参阅**: [docs/COMMON_MODULE.md](docs/COMMON_MODULE.md)

## Common Frontend

`common-frontend` 模块提供共享的 Vue 3 组件、工具和类型定义,供跨模块的前端复用。

**架构**: 分为两个子模块以避免不必要的依赖:

```
common-frontend/
├── basic/          # 基础 UI 组件 (无地图依赖)
│   └── src/
│       ├── components/  - StorageEngineForm, ResourceTree
│       ├── previews.js  - ImagePreview, MarkdownPreview, PdfPreview 等按需预览入口
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
import { StorageEngineForm } from '@common-ui'
import { formatFileSize, formatDateTime } from '@common-ui'
```

预览组件从独立入口导入，并由使用方模块声明对应预览依赖：

```javascript
import { ImagePreview, MarkdownPreview } from '@common-ui/previews'
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

- **预览组件**: GeoJsonPreview, TablePreview, ImagePreview（`ImagePreview` 等文件预览组件从 `@common-ui/previews` 导入）
- **资源选择组件**: ResourceTree, ResourceTreePicker
- **表单组件**: StorageEngineForm (PostgreSQL/MySQL/Doris/ClickHouse/MongoDB/Neo4j/MinIO/S3/NFS/Spark 配置)
- **地图组件**: MapContainer, OpenLayersRenderer, GaodeMapRenderer
- **工具**: formatFileSize, formatDateTime, detectFormatByExtension, isGeospatialFormat
- **类型**: FieldType, FormatType, EngineType (与后端模型对齐)

ResourceTree 是树展示组件；ResourceTreePicker 是表单级资源选择封装。跨模块选择已有资源时，ResourceTreePicker 输出的主身份只能是 `selection.identity.locator`。新表、新文件等创建目标应由业务表单组合 `ResourceTreePicker mode="node"` 选择父 node，再输入名称，输出 `parent_locator + name`，不得生成尚不存在资源的虚拟 locator。

资源选择的默认策略是“先过滤、后禁选”：明确不应出现的资源优先由业务侧通过 `nodeFilter` 排除；只有需要保留上下文时才使用 `selectableFilter` 让节点留在树中但不可提交。

**优势**:

- ✅ **模块化依赖**: 模块只安装需要的内容
- ✅ **减小打包体积**: 基础模块通过排除地图库节省约 2-3MB
- ✅ **类型安全**: 共享的类型定义确保前后端一致性
- ✅ **DRY 合规**: UI 组件复用而非复制
- ✅ **统一维护**: 所有共享组件集中在一处

**模块使用**:

- **System Frontend**: 使用 `basic` (引擎配置的 StorageEngineForm)
- **Manager Frontend**: 使用 `map` (数据预览的 GeoJsonPreview, TablePreview)
- **Meta Frontend**: 使用 `basic` (通用资源树和基础 UI)
- **Transfer Frontend**: 使用 `basic` (映射 UI 的字段类型工具)
- **Console Frontend**: 使用 `basic` (通用 UI 元素)

**另请参阅**: [common-frontend/README.md](common-frontend/README.md), [common-frontend/ARCHITECTURE.md](common-frontend/ARCHITECTURE.md)
