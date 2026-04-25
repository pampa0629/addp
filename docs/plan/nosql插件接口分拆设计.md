# NoSQL 插件接口分拆设计（先实施）

> 状态：待评审  
> 创建日期：2026-04-24  
> 目标：在不引入别名、不新增 NoSQLBasePlugin 名称的前提下，完成 MongoDB 与 Neo4j 的插件接口分拆，消除语义混淆。

---

## 一、背景与问题

当前 `NoSQLPlugin` 同时服务 MongoDB 与 Neo4j，但接口命名和数据结构明显偏向文档数据库：

- `ListCollections()`
- `CollectionInfo`
- `GetCollectionStats()`

这导致 Neo4j 的 Node Label 被映射为 collection，语义不准确；同时 Neo4j 的 Relationship 又通过 `GraphDBPlugin` 额外补充，形成混合模型。

你已确认：
1. 不要搞别名；
2. 名称直接用 `NoSQLPlugin`；
3. 先做接口分拆，再修订 node type。

---

## 二、设计目标

1. **保留 `NoSQLPlugin` 这个名字**，但让它只承载真正可跨 NoSQL 引擎共享的能力。
2. 将 MongoDB 文档模型能力与 Neo4j 图模型能力拆开，避免语义污染。
3. 让 Meta / Manager / System 这三个支持 Neo4j 引擎的模块都能按正确语义调用。
4. 改动可渐进落地，避免一次性大爆炸。

---

## 三、分拆后的接口结构

### 3.1 NoSQLPlugin（保留名称，收敛为公共基础能力）

仅保留共有能力：

```go
type NoSQLPlugin interface {
    StoragePlugin

    // 公共能力
    ListDatabases(ctx context.Context, connInfo ConnectionInfo) ([]DatabaseInfo, error)
    IsSystemDatabase(databaseName string) bool
    CreateClient(ctx context.Context, connInfo ConnectionInfo) (interface{}, error)
    CloseClient(ctx context.Context, client interface{}) error
}
```

### 3.2 DocumentDBPlugin（新增，承载文档模型专属能力）

```go
type DocumentDBPlugin interface {
    NoSQLPlugin

    ListCollections(ctx context.Context, connInfo ConnectionInfo, database string) ([]CollectionInfo, error)
    GetCollectionStats(ctx context.Context, connInfo ConnectionInfo, database, collection string) (*CollectionStats, error)
}
```

适用：MongoDB（及未来 Couchbase 等文档数据库）。

### 3.3 GraphDBPlugin（保留，承载图模型专属能力）

现有 `GraphDBPlugin` 已存在，继续使用并增强语义地位：

```go
type GraphDBPlugin interface {
    NoSQLPlugin

    ListNodeLabels(ctx context.Context, connInfo ConnectionInfo, database string) ([]NodeLabelInfo, error)
    ListRelationshipTypes(ctx context.Context, connInfo ConnectionInfo, database string) ([]RelationshipTypeInfo, error)
    GetGraphSchema(ctx context.Context, connInfo ConnectionInfo, database string) (*GraphSchema, error)
}
```

适用：Neo4j（及未来 Neptune/ArangoDB 图模型子集）。

---

## 四、数据语义统一规则（为后续 node type 修订做准备）

> 本文档阶段不改 item_type 值，只定义接口语义边界；具体落库值改动在下一份 node type 文档执行。

- MongoDB：`DocumentDBPlugin` 路径输出集合语义。
- Neo4j：`GraphDBPlugin` 路径输出节点标签 + 关系语义。
- 不再要求 Neo4j 从“collection 语义”解释自身模型。

---

## 五、模块影响分析

### 5.1 common（接口层）

文件：
- `common/engine/plugin/interfaces.go`

改动：
1. `NoSQLPlugin` 删除 `ListCollections`、`GetCollectionStats`。
2. 新增 `DocumentDBPlugin`。
3. `GraphDBPlugin` 明确继承 `NoSQLPlugin`（已有）。

### 5.2 MongoDB 插件

文件：
- `common/engine/plugins/mongodb/nosql.go`

改动：
1. 确保实现 `DocumentDBPlugin`（本质已有方法，主要是编译约束调整）。
2. 行为不变。

### 5.3 Neo4j 插件

文件：
- `common/engine/plugins/neo4j/plugin.go`

改动：
1. 不再承担“文档集合语义”的主路径职责。
2. 实现 `GraphDBPlugin` 主路径。
3. （下一阶段）在关系类型枚举中过滤 `RTREE_*`。

### 5.4 Meta 扫描

文件：
- `meta/backend/internal/service/scan_nosql_service.go`

改动：
1. 入口仍按 `NoSQLPlugin` 获取数据库列表。
2. 扫描数据库内对象时，改为分支：
   - `DocumentDBPlugin`：走 collection 扫描
   - `GraphDBPlugin`：走 node label + relationship 扫描
3. 取消“统一按 ListCollections 扫描”的旧逻辑。

### 5.5 Manager 预览

文件：
- `manager/backend/internal/service/preview_provider_doc_collection.go`

改动：
1. 插件类型断言从 `NoSQLPlugin` 改为更精确：
   - 文档预览：优先 `DocumentDBPlugin`
   - Neo4j 预览：可以保留当前 parser 路径，但调用方不再依赖 `ListCollections` 在 `NoSQLPlugin` 上存在
2. 保持前端接口兼容。

### 5.6 System 模块

System 主要负责引擎配置与连通性，几乎不受接口拆分影响，仅受编译依赖影响（无需行为调整）。

---

## 六、实施步骤（本阶段执行顺序）

### 步骤 1：接口定义调整（common）

- 修改 `interfaces.go`
- 新增 `DocumentDBPlugin`
- 修复编译报错点

### 步骤 2：插件实现对齐（mongodb / neo4j）

- MongoDB 显式满足 `DocumentDBPlugin`
- Neo4j 聚焦 `GraphDBPlugin`

### 步骤 3：Meta 扫描服务分流

- `ScanDatabase()` 中按接口类型分流扫描对象
- 保持数据库节点扫描逻辑不变

### 步骤 4：Manager 预览调用修正

- 避免将文档语义强绑在 `NoSQLPlugin`
- 预览链路可运行

### 步骤 5：回归测试

- MongoDB：数据库列表、集合扫描、预览
- Neo4j：数据库列表、节点标签扫描、关系扫描、预览
- 编译与基本接口联调通过

---

## 七、兼容性与风险

### 7.1 风险：编译影响面广

`NoSQLPlugin` 是基础接口，修改后会触发多个模块编译错误。

应对：按“common → plugins → meta → manager”顺序修复，避免来回返工。

### 7.2 风险：Manager 预览行为变化

Manager 当前 doc preview provider 直接断言 `NoSQLPlugin`。

应对：在 provider 内根据引擎类型 + parser 能力做更精确断言，优先保证用户侧功能不回退。

### 7.3 风险：与 node type 改造耦合

接口分拆与 item_type 改名（collection→label，relationship_type→relationship）存在天然关联。

应对：严格两阶段推进：
1) 本文先完成接口分拆；
2) 再执行 node type 修订。

---

## 八、验收标准（本阶段）

1. 代码中 `NoSQLPlugin` 不再包含 `ListCollections`、`GetCollectionStats`。
2. 新增 `DocumentDBPlugin`，MongoDB 走该接口。
3. Neo4j 的主语义调用走 `GraphDBPlugin`。
4. Meta 与 Manager 编译通过，MongoDB/Neo4j 基础扫描与预览可用。
5. 尚未改动 item_type 值（留待下一阶段）。

---

## 九、后续衔接

本设计完成后，进入下一阶段文档《Neo4j node type 修订设计》，执行：
- `collection` → `label`
- `relationship_type` → `relationship`
- 过滤 `RTREE_*` 内部关系
- 文档与代码同步修订
