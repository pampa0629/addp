## Common 模块

`common` 模块提供共享代码,避免**所有其他后端模块**之间的重复 (Manager、Meta、Transfer、Orchestrator、Develop 和 GeoPython Workflow 集成)。

**内容**:

- [client/system.go](common/client/system.go) - SystemClient 用于与 System 模块通信
- [models/engine.go](common/models/engine.go) - 共享的 Engine 模型和 `connection_info` 结构
- [config/loader.go](common/config/loader.go) - 部署环境配置读取；目标实现不得保留 System shared config 或环境变量 fallback 双轨
- `common/config` - 根环境部署配置、服务地址构造、端口可用性检查和时区读取；模块注册端口直接使用 owner 已加载的部署配置，不维护共享默认端口表
- `common/security` - System、Inference、Monitor 等模块共享的 AES-256-GCM 敏感凭据加解密；不承载 IAM 或业务字段识别
- `common/buildinfo` - Go 服务统一构建身份，由模块生命周期健康响应复用；构建脚本通过链接参数注入 build ID、Git commit、源码指纹和构建时间，进程启动时间由包初始化记录
- `common/modulelifecycle` - Go Backend 统一的进程存活、System 注册生命周期状态、就绪门禁和 `/health/live`、`/health/ready` 响应；只保存当前进程瞬时状态，System 仍是模块定义、实例和租约的唯一持久事实源
- `common/jsonmap` - decoded JSON map 的通用读取工具,不承载 `meta_item.attributes` 业务规范
- `common/taskprovider` - `task.capabilities/v2`、标准任务列表响应、任务级 `execution_contract` 和执行输入实例校验；校验失败返回包含稳定 rule、path 和约束值的结构化错误。任务类型能力不再保存静态 `execution_schema`，Orchestrator 必须从具体任务详情取得精确输入/输出契约
- `common/runtimehealth` - ADDP 应用层后台运行实例的公共心跳模型、发布器和查询仓库；只发布进程活性、角色、容量与当前占用，不承载 execution/runtime/delivery 的领取权或 fencing token
- `common/query` - 查询参数绑定、SQL 副作用分析和跨 SQL 引擎的基础方言能力；不承载 Engine Catalog facts 或 PostGIS 空间扩展语义
- `common/engine/selection` - 基于规范化 Engine capabilities 的 Engine Instance 解析和筛选 helper
- `common/middleware/ratelimit` - Redis 原子固定窗口限流能力，供认证等多实例安全边界复用；Redis 不可用时由调用方定义失败关闭响应，不提供进程内存回退路径
- `common/contentio` - 基于 Go `io` 的内容定位与读写抽象，负责 `Ref`、`Reader`、`Writer`、`Lister`、`RangeReader` 和 `Stat`
- `common/format` - 通用文件格式、FormatDescriptor、格式信息、format plugin、info provider、content reader 和 writer/provider
- `common/format/pmtiles`、`common/format/rastermosaic` - 格式域内的 PMTiles v3 归档和 Raster Mosaic Schema 实现
- `common/dataitem` - 候选内容集合到 data item 组织结果的通用解析能力，供 Meta 扫描和 Manager 容器动态预览复用；当前已落地 `ResolveItems()`、single / multi / whole 规则派生、related refs 还原 helper 和基础忽略策略
- `common/resourcetree` - Meta 已落库 Engine Catalog / item 事实到跨模块资源树视图的投影层，提供 `TreeNode`、`TreeBuilder`、`ResourceLocator` 和 provider `EngineCatalogPath` 纯转换能力；不持有 System / Meta client，不主动读取远程服务，不处理租户权限、token、降级策略、扫描或内容读取
- [client/meta.go](common/client/meta.go) - MetaClient 是跨模块调用 Meta API 的唯一共享 Client；只接受 `ServiceTokenProvider`，按 Tenant 获取短期 Service Access Token 并只发送 Bearer，Manager 等模块不得保留私有 Meta Client、代传 User Token 或恢复 Internal API Key / Tenant Header
- [client/service_token.go](common/client/service_token.go) - OAuthServiceTokenSource 按 `tenant_id` 或显式 `context_type=platform` 向 System 换取短期 Service Access Token，并按 Context 独立缓存
- [client/system_service.go](../../common/client/system_service.go) - SystemServiceClient 是 Service Principal 调用 System 的 Bearer-only Client；Tenant 请求使用不可变 `WithTenantID`，平台模块注册、心跳以及随模块注册发布 TaskProvider 声明使用 Platform Context。模块注册返回可查询快照和完成信号的生命周期对象，状态固定为 `starting|registered|recovering|failed|stopped`，供 `common/modulelifecycle` 执行就绪判断。实例首次注册成功后，无论生命周期 Context 从心跳等待、请求或重试阶段取消，Client 都必须使用独立的限时 Context 注销该实例。Go 进程入口必须传入信号 Context，并在退出前等待生命周期完成信号。`SystemAPIError` 必须保留 System 错误的方法、路径、HTTP 状态、稳定错误码、错误文案和受限长度的原始响应正文，与 `common-python` 的模块注册客户端共用同一诊断语义
- `common/client` 的 Tenant owner Client 统一通过 `TenantAPIError` 保留下游 HTTP 状态码和稳定 `error_code`，通过 `TenantTransportError` 表达连接失败和超时；调用方只能使用 `errors.As`、`TenantAPIStatusCode()`、`TenantAPIErrorCode()` 分类，不得解析本地化错误正文。`StandardClient` 的引用校验会将资源不存在和跨租户资源统一收敛为不可探测的“不存在”语义。
- `common/engine/workflowaccess` - 把已解析的文件、对象或目录型存储资源转换为 `addp.workflow.access-plan/v1` 执行计划和脱敏审计计划；不保存任务定义、不决定产物归属，也不触发 Meta scan

`common-python/addp_common/workflow_runtime` 提供 Python Workflow Runtime 的协议执行核心，包括 workflow definition 校验、DAG 拓扑排序、引用解析、异步 execution 状态和标准错误。它是共享库，不是独立工作流引擎；各运行时仍负责自己的算子注册、内存对象类型和专业执行依赖。

`common-python/addp_common/tools` 提供 `addp.tool-manifest/v1`、Manifest 校验和 ToolExecutor。Python SDK 是唯一 HTTP Client 实现；`addp` CLI、ADDP Agent LangChain Tool Provider 和后续 MCP Adapter 只能调用 ToolExecutor，不得各自维护 HTTP 路径、认证或业务判断。ToolExecutor 在每次 Tool 调用边界向 System 换取绑定 owner audience、稳定 Tool Scope、AgentRun 和 ToolCall 的短期 Delegated Access Token，原始 User Access Token 不进入 owner Client。服务身份访问 owner API 时使用独立 Service Principal 的短期 Service Access Token；调用方只发送 Bearer，不发送 Internal API Key 或客户端自报的 Tenant Header。

`common-python/addp_common/resources` 提供跨 Copilot、Agent 和业务 AI 入口共享的 `ResourceFact`。它只承载 owner 已确认的资源身份及有限事实（locator、引擎、数据类型、字段和空间信息），不负责搜索、租户权限或领域生成。Copilot 的 `ResourceResolutionService` 负责统一意图提取、候选发现、补充检索、排序和重新校验；Query、Workflow、Notebook、Transfer 通过各自 `ResourceResolutionPolicy` 表达差异。

`common-python/addp_common/client.BaseClient` 为 Python owner Client 提供 Tenant Service Access Token 请求主路：调用方显式传入 `OAuthServiceTokenSource + tenant_id`，Client 在每次请求前取得短期 Bearer；若因授权版本变化首次收到 401，只精确失效被拒绝 Token、重新取 Token 并重放一次。非 401 不重试，不得回退到 Internal API Key、User Token 或 Tenant Header。

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

// 服务按 Tenant Context 获取 System 引擎事实
tokenSource, err := commonClient.NewOAuthServiceTokenSource(systemURL, clientID, clientSecret, nil)
if err != nil {
    return err
}
client := commonClient.NewSystemServiceClient(systemURL, tokenSource, nil).WithTenantID(tenantID)
engines, err := client.ListEngines()
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

### 血缘查看器边界

`common-frontend/graph` 是血缘查看器的唯一共享实现位置，提供 `LineageViewer`、血缘 DTO 标准化和宿主注入的 lineage API client / composable。查看器消费 Meta 的血缘查询 API，不保存血缘事实，也不自行拼接 ResourceLocator。

Manager、Service、Asset、Portal 等宿主页面可以直接嵌入同一个查看器。宿主负责权限上下文、业务路由和节点点击后的跳转目标；不要求先跳转到 Manager 的独立页面。`basic/` 不承载图谱逻辑，避免基础组件引入 G6 等图依赖。

**架构**: 按依赖边界划分子模块:

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
├── map/            # 地图相关组件 (需要 ol 和 @amap/amap-jsapi-loader)
│   └── src/
│       ├── components/  - MapContainer, GeoJsonPreview, TablePreview
│       ├── composables/ - useMapConfig, useGaodeMap, useOpenLayersMap
│       └── utils/       - 地理工具, 格式化器
│
└── graph/          # 图和血缘查看器（使用 G6）
    └── src/
        ├── components/  - LineageViewer 等图组件
        ├── composables/ - lineage API 注入与查询状态
        ├── types/       - 血缘 DTO 和节点 / 边类型
        └── index.js
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
