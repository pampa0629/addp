# Common Module

ADDP 平台的共享代码模块，提供各个微服务模块通用的工具和类型定义。

## 包说明

### client
提供与其他服务交互的客户端：
- `SystemClient`: 与 System 模块交互的客户端，用于获取资源配置、用户认证等

`client` 只表达跨服务 HTTP/API 调用边界，不作为 infra PostgreSQL `common` schema 的读写入口。

### jsonmap
decoded JSON map 的通用读取工具，用于读取嵌套 section、字符串、数字、时间等基础值。

`jsonmap` 不承载 `meta_item.attributes` 业务规范；attributes 标准分区、normalizer 和落库构造属于 Meta 模块。

### contentio
基于 Go `io` 之上的内容定位和读写抽象，提供 `Ref`、`Reader`、`Writer`、`Lister`、`RangeReader` 和 `Stat`。

`contentio` 不依赖 engine，也不解析格式；多 content 的组织规则属于 `common/format`、`common/dataitem` 或调用编排层，engine 到 contentio 的适配放在 `common/engine/contentadapter`。

### resourcetree
把 Meta 已落库的 catalog / item 事实投影为跨模块资源树视图，并提供 `ResourceLocator` / provider `CatalogPath` 的纯转换能力。

`resourcetree` 不持有 System / Meta client，不主动读取远程服务，不处理租户权限、token、降级策略、扫描或内容读取；上层模块自行获取 System / Meta 数据后，再调用资源树构建和路径转换能力。

`resourcetree` 中 attributes helper 只服务 `TreeNode.Metadata` 展示摘要，不作为通用 attributes 规范 API，也不写入持久 attributes。

### query
查询文本通用能力，包括 SQL / Cypher / MQL 参数绑定、SQL 副作用分析，以及跨 SQL 引擎的标识符引用、基础 SELECT / COUNT 和分页 SQL 生成。

`query` 不承载 catalog facts 探测或 PostGIS 等特定引擎扩展函数；PostGIS 空间表达式属于 `spatial`。

### spatial
空间数据通用能力，包括 CRS、MVT、WKB、坐标转换和 PostGIS 空间 SQL 表达式。

### format
文件格式、类型信息、格式信息、字段类型映射、parser / extractor / analyzer 等通用能力。

`format` 不直接决定 meta item 如何归并，也不绕过 Meta normalizer 写最终 attributes。

### execution
统一执行记录能力，负责 `common.task_executions` 的模型、仓储、统计查询和 SQL 迁移入口。

所有模块写入或查询统一执行记录时应复用 `common/execution`，不得在模块内新增私有执行历史主表或绕过仓储直接写 `common.task_executions`。

后续如果确实新增 `common` schema 共享表，应按领域新增 `common/<domain>` 包，并在领域包内提供模型、仓储和 `EnsureStore`；只有当 `common` schema 独立成远程 Common 服务时，才新增对应 `common/client`。

### taskprovider
TaskProvider 标准契约的纯解析和校验能力，包括 `task.capabilities/v2`、具体任务 `execution_contract`、执行参数实例校验与标准任务列表响应 `{items,total,page,page_size}`。

`taskprovider` 不访问 System 注册表，不调用 owner 模块，不处理执行调度；System、Monitor、Orchestrator 等模块只复用它判断跨模块契约是否符合规范。

### models
共享的数据模型：
- `Resource`: 资源信息结构体
- `Engine`: 引擎配置模型，`ConnectionInfo` 是连接信息事实源

## 使用方法

在其他模块的 `go.mod` 中引用：

```go
require (
    github.com/yourusername/addp/common v0.0.0
)

replace github.com/yourusername/addp/common => ../common
```

在代码中导入：

```go
import (
    "github.com/yourusername/addp/common/client"
)

// 使用 SystemClient
sysClient := client.NewSystemClient("http://localhost:8180", token)
engine, err := sysClient.GetEngine(1)
```

## 设计原则

1. **单一职责**: 只包含真正通用的代码
2. **边界清晰**: 通用概念和工具可进入 common，Meta item 识别、claims / exclusive、`meta_item.full_name` 决策和 attributes 落库构造属于 Meta
3. **零依赖**: 尽量减少外部依赖，只使用 Go 标准库
4. **无需旧兼容**: 开发阶段不为旧包名、旧数据或旧逻辑保留兼容层
5. **文档完善**: 所有公开函数和类型都有文档注释
