# 实时 Catalog 浏览归属讨论记录

更新时间：2026-05-04

本文记录 engine plugin 体系下“实时 catalog 浏览能力”应归属哪个模块的讨论结论。该问题与 Swagger 文档同步是两件事：Swagger 关注 API 文档治理，catalog 归属关注 System、Meta、Manager 的职责边界。

## 一、三个模块的核心职责

当前对 System、Meta、Manager 的理解基本正确：

- System：管理引擎实例。包括引擎登记、连接信息、凭据、租户权限、连接测试、连接状态和能力声明。
- Meta：负责元数据扫描、持久化和查询。把外部引擎中的目录、数据项、字段、空间信息等扫描进 `metadata.meta_node` 和 `metadata.meta_item`。
- Manager：负责基于元数据做数据管理和预览。它展示树、组合预览能力，并在需要时读取真实数据。

需要进一步区分的是：`catalog 浏览` 有两种语义。

## 二、实时 catalog 浏览是什么

实时 catalog 浏览指的是：用户还没有扫描，或者正在配置扫描范围时，系统需要临时连接真实引擎，列出当前真实存在的 namespace、bucket、目录、collection、label 等。

它回答的是：

```text
这个引擎现在真实有什么？
```

它不一定落库，不一定产生元数据资产，也不一定代表平台已经纳管这些对象。

典型场景：

- 扫描前选择 PostgreSQL 的 schema。
- 扫描前选择 MinIO 的 bucket 或 prefix。
- 配置数据源时检查 MongoDB 有哪些 database / collection。
- 需要立即确认某个引擎连接下的目录结构。

从 provider 角度，它调用的是：

```text
CatalogProvider.ListChildren()
```

这属于“连接到引擎并实时发现目录”的控制面能力。

## 三、扫描后元数据查询是什么

扫描后元数据查询指的是：Meta 已经把扫描结果落库，后续页面和模块查询的是 ADDP 自己保存的元数据快照。

它回答的是：

```text
平台已经扫描、记录、纳管了什么？
```

它依赖：

```text
metadata.meta_node
metadata.meta_item
```

典型场景：

- Manager 展示数据管理树。
- Manager 获取某个 item 的字段和空间元数据。
- Service 发布服务时读取已扫描的 item 元数据。
- Quality、Graph、Agent 等模块引用平台已纳管资产。

这部分应归 Meta。

## 四、为什么要决策实时 catalog 浏览归属

现在代码里已经同时存在两条类似能力。

第一条是 System 的实时 catalog API：

```text
GET /api/v1/system/engines/:id/namespaces
GET /api/v1/system/engines/:id/items?namespace=...
```

它基于 System 管理的引擎连接信息，通过 `common/dbbridge` / `CatalogProvider` 实时访问外部引擎。

第二条是 Meta 的实时浏览接口：

```text
GET /api/v1/meta/engines/:engine_id/storage/nodes
```

虽然它底层也已经走 `CatalogProvider.ListChildren()`，但接口命名偏对象存储/文件系统，并且会让 Meta 的公共面看起来像具体存储浏览器。

这就是需要决策的原因：同一类“实时连接真实引擎列目录”的能力，到底应由 System 统一提供，还是由 Meta 继续提供一部分。

## 五、倾向判断

推荐边界：

```text
System：提供实时 catalog 浏览能力。
Meta：提供扫描任务和扫描后元数据快照。
Manager：消费 Meta 快照做树和预览，需要读真实数据时走插件/provider 或后端预览服务。
```

理由：

1. 实时 catalog 浏览依赖引擎连接信息、凭据、租户权限和连接状态，这些天然在 System。
2. Meta 的价值是扫描、落库、索引、事件、元数据查询，而不是成为所有引擎的实时浏览代理。
3. 如果实时浏览分散在 System 和 Meta，前端会出现两套选择链路：数据库走 System，文件/对象走 Meta，长期会放大术语和行为不一致。
4. Engine plugin 规范也已经强调：System 管引擎控制面，Meta 用 provider 扫描并落库，Manager 基于 Meta 树组织预览。

## 六、System 现有 API 还不够完整

当前 System 的实时 API 已覆盖：

```text
/engines/:id/namespaces
/engines/:id/items?namespace=...
```

这对关系型数据库、MongoDB、Neo4j 的浅层 catalog 足够，但对对象存储和文件系统不够，因为它们需要多层递归浏览：

```text
bucket -> prefix -> object
root -> directory -> file
```

因此如果决定归 System，需要补一个更通用的实时 catalog children API，例如：

```text
GET /api/v1/system/engines/:id/catalog/children?path=...
```

或更结构化一点：

```text
POST /api/v1/system/engines/:id/catalog/children
```

请求体可表达 `CatalogPath`，避免对象存储、文件系统、数据库、图数据库都硬塞进一个字符串 path。

响应应使用中性的 `CatalogNode`，不要叫 `ObjectNode`：

```json
{
  "nodes": [
    {
      "name": "public",
      "path": "public",
      "kind": "namespace",
      "term": "schema",
      "is_container": true,
      "stats": {}
    }
  ]
}
```

## 七、对现有 Meta API 的处理建议

短期：

- 把 `/engines/:engine_id/storage/nodes` 的公开描述改成“实时 catalog 节点浏览”，避免对象存储独占描述。
- 在代码命名层面逐步把 `ObjectNode`、`ListObjectStorageNodes` 改为 `CatalogNode`、`ListCatalogChildren`。
- 明确该接口是过渡接口，不应作为 Meta 长期公共抽象。

中期：

- 在 System 补充通用 catalog children API。
- Meta 前端扫描页从 `/meta/engines/:id/storage/nodes` 切换到 System 的通用 catalog API。
- Meta 保留扫描后的 `/engines/:id/tree`、`/nodes`、`/items`、`/items/:id/fields` 等元数据快照 API。

长期：

- 删除或内部化 Meta 的实时浏览接口。
- Meta 的公开 API 只展示元数据中枢能力，不再展示具体引擎形态优先的对象存储接口。
- Manager 预览需要读取真实数据时，应通过 Manager 后端的预览 provider 消费 engine plugin 能力，而不是让 Meta 充当内容读取代理。

## 八、推荐的最终边界

最终边界可以简化为：

```text
System:
  引擎控制面 + 实时 catalog 发现

Meta:
  扫描任务 + 元数据快照 + 元数据查询 + 索引/事件

Manager:
  数据管理体验 + 树展示 + 数据预览 + 真实数据读取组合
```

其中最重要的区分是：

```text
实时 catalog：真实引擎当前有什么，归 System。
扫描后 metadata：平台已经记录纳管了什么，归 Meta。
数据 preview：用户要看数据内容，归 Manager。
```

## 九、改进计划

既然确认实时 catalog 浏览能力应归 System，后续改造应按“先统一抽象、再迁移调用、最后收敛旧接口”的顺序推进，避免前端和模块间继续扩散两套 catalog 语义。

### 1. 目标原则

- **边界收敛**：System 统一提供实时 catalog 发现；Meta 只负责扫描任务、元数据落库和元数据快照查询；Manager 负责数据管理体验和预览。
- **术语统一**：公共 API、DTO、前端类型统一使用 `Catalog`、`CatalogNode`、`CatalogPath`、`ListCatalogChildren` 等中性命名，避免继续使用 `storage`、`object` 这类偏对象存储的术语作为平台级抽象。
- **能力复用**：后端实时浏览统一复用 `common/dbbridge` 与 engine plugin 的 `CatalogProvider.ListChildren()`，不在 System、Meta、Manager 中重复实现具体引擎浏览逻辑。
- **路径中立**：不要把所有引擎强行压成单一字符串 path；需要支持数据库 namespace/table、对象存储 bucket/prefix/object、文件系统 directory/file、图数据库 label/relationship 等不同 catalog 层级。
- **渐进迁移**：Meta 现有实时浏览接口先标记为过渡接口，完成 System 新接口和前端迁移后再删除或内部化。

### 2. 阶段一：盘点现状和定义统一模型

- 盘点 System 当前 `/engines/:id/namespaces`、`/engines/:id/items` 的调用方、DTO、Swagger 和前端使用位置。
- 盘点 Meta 当前 `/engines/:engine_id/storage/nodes` 的 Handler、Service、DTO、Swagger、前端扫描页调用位置和测试覆盖。
- 在 `common/` 或 engine plugin 共享包中确认 `CatalogNode`、`CatalogPath`、`CatalogListRequest`、`CatalogListResponse` 的统一定义位置，避免各模块各自声明一套近似结构。
- 明确 `CatalogNode.kind`、`CatalogNode.term`、`is_container`、`path`、`stats`、`metadata` 等字段语义，并补充到 engine plugin 接口规范或相关能力声明文档。
- 明确权限模型：实时 catalog 浏览必须基于 System 的引擎实例、租户权限、凭据解密和连接状态校验执行。

### 3. 阶段二：System 增加通用实时 catalog API

- 在 System 后端新增通用接口，优先采用结构化请求体：`POST /api/v1/system/engines/:id/catalog/children`。
- 请求体表达 `CatalogPath`，至少支持根节点、namespace、bucket、prefix、database、schema、collection 等层级，不要求调用方硬编码 `geom` 或其他单一字段假设。
- 响应体统一返回 `CatalogNode[]`，并保持与现有 `CatalogProvider.ListChildren()` 输出语义一致。
- 新接口需要复用现有引擎获取、凭据处理、连接测试、能力声明和 provider 构造逻辑，不新增绕过 System 引擎管理模型的连接入口。
- Swagger 注解、路由覆盖检查和 API 文档必须同步更新，避免出现已实现接口但 Swagger 缺失的情况。
- 保留现有 `/namespaces`、`/items` 作为兼容或快捷接口的必要性需要单独评估；若不再需要，应制定删除计划而非长期并存。

### 4. 阶段三：Meta 接口语义过渡和内部重命名

- 将 Meta `/engines/:engine_id/storage/nodes` 的公开描述调整为“过渡性的实时 catalog 节点浏览接口”，避免继续宣称它是对象存储专用能力。
- 后端命名逐步从 `ObjectNode`、`ListObjectStorageNodes`、`storage nodes` 迁移到 `CatalogNode`、`ListCatalogChildren`、`catalog children`。
- Meta 内部如果扫描流程仍需实时浏览选择扫描范围，应通过 System 新 catalog API 或共享 provider 抽象获取目录，而不是扩大 Meta 公共实时浏览接口。
- Meta 公开 API 应继续聚焦扫描后的 `/engines/:id/tree`、`/nodes`、`/items`、`/items/:id/fields` 等元数据快照能力。
- 在 Swagger 中明确该 Meta 实时浏览接口为迁移期接口，后续不鼓励新调用方接入。

### 5. 阶段四：前端调用链迁移

- Meta 扫描配置页从 `/api/v1/meta/engines/:id/storage/nodes` 切换到 `/api/v1/system/engines/:id/catalog/children`。
- 抽取通用 catalog 浏览 client 和类型，优先放在 `common-frontend/basic` 或各模块共享 API client 层，避免 System、Meta 页面重复维护树节点适配逻辑。
- 前端展示文案统一使用“Catalog / 目录 / 命名空间 / 节点”等中性概念，根据 `term` 展示 schema、bucket、prefix、collection、label 等引擎原生术语。
- 扫描配置页只使用实时 catalog 选择扫描范围；扫描完成后的数据树、字段、空间信息展示继续使用 Meta 元数据快照 API。
- Manager 若需要展示已纳管资产树，应继续消费 Meta 快照；若需要读取真实数据内容，应走 Manager 后端预览能力，不把 Meta 实时浏览接口作为预览代理。

### 6. 阶段五：旧接口下线和边界固化

- 在确认前端和后端调用方全部迁移后，删除或内部化 Meta `/engines/:engine_id/storage/nodes`。
- 清理遗留的 `ObjectNode`、`storage nodes` 等平台级错误命名，保留仅在具体对象存储 provider 内部有意义的 object/bucket/prefix 术语。
- 清理 System 中与新通用 catalog API 重叠的旧浅层接口，若保留 `/namespaces`、`/items`，需明确它们只是便捷封装而非新的抽象边界。
- 更新相关规范文档，确保“实时 catalog 归 System，扫描后 metadata 归 Meta，数据 preview 归 Manager”成为后续开发准则。
- 如果迁移过程中发现 provider 能力声明不足，应同步补充 engine capability，避免前端通过猜测引擎类型决定浏览层级。

### 7. 建议验收标准

- System Swagger 中存在通用实时 catalog children API，且路由覆盖检查通过。
- Meta Swagger 不再把实时浏览描述为对象存储专用能力，并明确其迁移状态。
- Meta 扫描配置页不再调用 `/api/v1/meta/engines/:id/storage/nodes`。
- 已扫描资产树、字段、空间信息仍全部来自 Meta 元数据快照 API。
- Manager 不依赖 Meta 实时浏览接口读取真实数据内容。
- 代码中平台级 DTO 和 service 命名不再使用 `ObjectNode` 表达通用 catalog 节点。
- PostgreSQL schema/table、MinIO bucket/prefix/object、文件系统 directory/file 至少各有一条手工或自动验证路径。

## 十、执行进展记录

更新时间：2026-05-04

### 1. 已完成

- **System 通用 API 已落地**：新增 `POST /api/v1/system/engines/:id/catalog/children`，由 System 基于引擎实例、用户权限和连接信息统一提供实时 catalog 子节点浏览。
- **System DTO 已补充**：新增 `CatalogListChildrenRequest`、`CatalogListChildrenResponse`、`CatalogPath`、`CatalogSegment`、`CatalogNode`、`CatalogListOptions`，用于 Swagger 和前后端契约表达。
- **后端能力复用已打通**：`common/dbbridge` 新增 `ListCatalogChildren()`，System service 通过该入口复用 engine plugin 的 `CatalogProvider.ListChildren()`，没有在 System 内重复实现具体引擎浏览逻辑。
- **后端公共客户端已补充**：`common/client/SystemClient` 新增 `ListCatalogChildren()` / `ListCatalogChildrenWithToken()`，后续 Meta、Manager、Transfer 等模块需要实时浏览时可以统一调用 System。
- **Meta 前端扫描配置页已迁移**：扫描页加载真实 catalog 顶层节点时，已从旧的 Meta 实时浏览链路切换为 System `catalog/children` API；扫描完成后的树和字段信息仍继续使用 Meta 元数据快照 API。
- **前端共享 client 已抽取**：`common-frontend/basic/src/api/catalog.js` 提供 `listCatalogChildren()`、`listCatalogBrowserNodes()` 和 `CatalogNode` 到扫描页树节点的适配逻辑，Meta 前端不再维护临时 catalog 转换。
- **路径传递已改进**：共享适配会保留 System 返回的 `catalog_path`，前端后续继续下钻时可直接传递结构化 `CatalogPath`，不再只能从 `bucket/prefix` 字符串反推。
- **Meta 实时浏览旧接口已下线**：删除 `/api/v1/meta/engines/:engine_id/storage/nodes` 路由、Handler、Swagger path、`ObjectNode` DTO 以及 `ListObjectStorageNodes`/`ResourceDiscoveryService`，Meta 公开 API 只保留扫描和元数据快照查询。
- **规范已同步**：`docs/spec/addp引擎插件接口规范.md` 已更新上层消费规则，明确 System 提供实时 catalog 浏览控制面，Meta 聚焦扫描和元数据快照。
- **Swagger 已同步**：System 和 Meta 的 Swagger 文档已重新生成，并通过路由覆盖检查。
- **Manager 边界已检查**：Manager 已纳管资产树继续消费 Meta 快照 API；真实数据预览后端直接使用 engine plugin provider，不依赖 Meta 实时浏览接口。

### 2. 已验证

- `bash scripts/swagger/check-route-coverage.sh system` 通过，System 公开路由和 Swagger 覆盖一致。
- `bash scripts/swagger/check-route-coverage.sh meta` 通过，Meta 公开路由和 Swagger 覆盖一致。
- `go test ./client ./dbbridge` 在 `common/` 下通过。
- `go test ./...` 在 `system/backend/` 下通过。
- `go test ./internal/api ./internal/models ./internal/service` 在 `meta/backend/` 下通过。
- `npm run build` 在 `meta/frontend/` 下通过；构建仅保留既有 chunk 体积警告。
- `rg "storage/nodes|ObjectNode|ListObjectStorageNodes"` 检查确认 Meta 后端、Meta 前端和 Swagger 生成产物中无旧实时浏览接口残留；`common-frontend` 中仅保留对象存储预览自身的 `getObjectNodeTypeLabel` 文案工具。

### 3. 下一步待办

- **评估浅层旧接口去留**：评估 System `/engines/:id/namespaces` 和 `/engines/:id/items` 是否仍需要保留为快捷接口；如果保留，应在文档中注明它们只是 `catalog/children` 的便捷封装。
- **补充 System 文档说明**：在 System 模块文档中补充 `POST /api/v1/system/engines/:id/catalog/children`，并标注 `/namespaces`、`/items` 的快捷封装定位。
- **补充典型引擎验证**：在实际环境中用 PostgreSQL schema/table、MinIO bucket/prefix/object、NFS root/directory/file 分别验证 `catalog/children` 的根节点和子节点浏览行为。
