# Manager 数据探查预览改进设计

> 状态：历史方案，已被当前 catalog leaf / graph facts 主路径取代
> 创建日期：2026-04-25
> 当前准则：Neo4j catalog leaf 统一为 `graph`；label、relationship type 和连接模式进入 `type_info.graph`，不作为独立 Meta item 或 catalog leaf。

---

## 一、背景与问题

当前 Manager 数据探查（DataExplorer）的预览展示存在以下问题：

1. **引擎层无内容**：点击引擎节点时，右侧面板为空，没有展示引擎基本信息。
2. **节点层展示粗糙**：节点只显示名称，缺少 node_type、full_name、元数据（item 数量、扫描时间等）。
3. **数据项层缺少元数据前置**：直接跳到数据预览，没有先展示 item 的类型、full_name、字段数、行数等元数据。
4. **术语不符合引擎原生语义**：前端硬编码了"表"、"集合"等词，没有根据引擎类型动态展示正确术语。
5. **Neo4j 图预览已收敛为 graph leaf 预览**：不再以 label 或 relationship type 作为独立预览对象。
6. **部分引擎预览为空**：NFS 目录节点、对象存储 prefix 节点点击后无有效展示。

---

## 二、各引擎 Node/Item Type 规范核对

以下为目标态（与 `docs/spec/addp存储引擎路径体系规范.md` 对齐，阶段一已完成）：

| 引擎 | 目录/预览能力 | Node Type（层级） | Item Type |
|------|---------|-----------------|-----------|
| PostgreSQL | Catalog/Metadata/SQLRuntime | `schema` | `table` / `view` |
| MySQL | Catalog/Metadata/SQLRuntime | `database` | `table` / `view` |
| Doris | Catalog/Metadata/SQLRuntime | `database` | `table` / `view` |
| ClickHouse | Catalog/Metadata/SQLRuntime | `database` | `table` / `view` |
| MongoDB | Catalog/MetadataSampling/DocumentRuntime | `database` | `collection` |
| Neo4j | Catalog/Facts/GraphRuntime | `database` | `graph` |
| MinIO | Catalog/Metadata/ContentReadable | `bucket` / `prefix` | `object` / `table` |
| S3 | Catalog/Metadata/ContentReadable | `bucket` / `prefix` | `object` / `table` |
| NFS | Catalog/Metadata/ContentReadable | `root`（透明）/ `dir` | `file` / `table` |

代码与规范已对齐：Neo4j label / relationship type 是 graph item 的结构视角，不参与 catalog leaf 身份划分。

---

## 三、三层展示设计

### 3.1 引擎层（Engine Level）

**触发时机**：用户在左侧树中点击引擎根节点。

**展示内容**（从 System 模块读取）：

| 字段 | 来源 | 说明 |
|------|------|------|
| 引擎名称 | system.engines.name | 用户自定义名称 |
| 引擎类型 | system.engines.engine_type | 使用 `DisplayName()` 展示（如 "PostgreSQL"） |
| 连接地址 | system.engines.connection_info | 仅展示 host:port，不展示密码 |
| 连接状态 | system.engines 中已有的状态字段 | 直接复用 System 模块维护的状态，不在 Manager 侧重复 TestConnection |
| 顶层节点数 | meta 模块 | 一级 node 数量（schema/database/bucket 数） |
| 数据项总数 | meta 模块 | 所有 item 数量汇总 |
| 最近扫描时间 | meta 模块 | 最新一次扫描完成时间 |
| 扫描状态 | meta 模块 | completed / pending / failed |

> **连接状态说明**：System 模块负责引擎管理，应由 System 定时维护连接状态并存储。Manager 直接读取该状态展示，无需自行发起 TestConnection。如 System 尚未提供该字段，作为后续 System 模块的改进点记录，Manager 侧暂不展示连接状态。

**操作按钮**：
- 触发扫描（刷新整个引擎）
- 查看引擎配置（跳转 System 引擎管理页）

---

### 3.2 节点层（Node Level）

**触发时机**：用户点击树中的 node（schema/database/bucket/prefix/dir）。

**展示内容**：

#### 3.2.1 节点元数据卡片

| 字段 | 来源 | 说明 |
|------|------|------|
| 节点类型标签 | 插件 `NodeTypeLabel(nodeType)` | 引擎原生术语，如 "Schema"、"数据库"、"Bucket"、"目录" |
| Full Name | meta_node.full_name | 节点在引擎内的完整路径 |
| 子节点数 | meta_node.attributes 或 meta 查询 | 下一级 node 数量（如有） |
| 数据项数 | meta 查询 | 直属 item 数量 |
| 扫描状态 | meta_node.scan_status | completed / pending / failed |
| 最近扫描时间 | meta_node.scanned_at | 格式化时间 |
| 附加属性 | meta_node.attributes | 引擎特有属性（如 PostgreSQL schema 的 owner） |

#### 3.2.2 子节点列表

展示该节点下的直属子节点（如 schema 下的子 schema，bucket 下的 prefix）：
- 类型图标 + 类型标签（使用引擎术语，来自 `typeLabel`）
- 名称
- 数据项数量

支持分页（默认每页 50 条），避免大量 table 的 schema 撑爆页面。

#### 3.2.3 数据项列表

展示该节点下的直属 item，按 item_type 分组展示：
- 类型图标 + 类型标签（使用引擎术语，来自 `typeLabel`）
- 名称
- 关键元数据（行数/大小/字段数，视引擎而定）

同样支持分页（默认每页 50 条）。

**操作按钮**：
- 触发扫描（刷新该节点）

---

### 3.3 数据项层（Item Level）

**触发时机**：用户点击树中的 item（table/collection/graph/file/object）。

**展示分两部分，元数据区默认展开，支持收拢以节省空间**：

#### 3.3.1 元数据信息区（上方，可收拢）

以 key-value 列表形式展示，key 使用 i18n 翻译键，value 直接显示原始值：

| 字段 | i18n key | 来源 |
|------|----------|------|
| 数据项类型 | `meta.itemType` | 插件 `ItemTypeLabel(itemType)` 返回的 i18n key |
| Full Name | `meta.fullName` | meta_item.full_name |
| 字段数 / 属性数 | `meta.columnCount` | meta_item.attributes |
| 行数 / 文档数 | `meta.rowCount` | meta_item.attributes |
| 大小 | `meta.size` | meta_item.attributes |
| 扫描时间 | `meta.scannedAt` | meta_item.scanned_at |
| 附加属性 | 动态 key | meta_item.attributes 中的其余字段 |

附加属性（attributes 中的引擎特有字段）以 key-value 列表追加展示，key 直接使用字段名（英文），不强制翻译。

#### 3.3.2 数据预览区（下方）

根据 item_type 选择对应的预览方式：

| Item Type | 预览方式 | Provider |
|-----------|---------|---------|
| `table` / `view` | 分页表格（列名 + 数据行） | DatabasePreviewProvider |
| `collection` | 分页表格（动态 schema 字段结构推断 + 记录数据） | DynamicSchemaCollectionPreviewProvider |
| `graph` | 图结构视图、节点 / 关系采样、按 node shape / relationship shape 筛选 | GraphPreviewProvider |
| `file` | 文件内容预览（文本/图片/空间数据等） | FileSystemPreviewProvider |
| `object` | 对象内容预览（同上） | ObjectStoragePreviewProvider |
| `table` | Parquet/ORC 表格预览 | ScopeTablePreviewProvider |

---

## 四、避免硬编码的机制

### 4.1 术语 i18n key 生成

**设计原则**：
- `EnginePlugin` 不变，不感知 meta 的 node/item 概念
- 插件无需实现术语翻译接口，后端按 `"engine.term." + term` 规则填充 i18n key
- 若后续确实需要引擎级自定义映射，应通过 `EngineCapabilities` 的 catalog model 声明序列化表达，而不是新增插件私有接口

后端在构建 `TreeNode` 时，按 `engine.term.<nodeType>` 规则填入 `typeLabel` 字段（语义从"已翻译文本"改为"i18n key"）。前端用该 key 查 i18n 字典，找不到时 fallback 到 key 本身（直接显示英文 type 名）。

i18n 字典（前端 `locales/zh-CN.json` 等）统一维护 `engine.term.*` 命名空间：

```json
{
  "engine": {
    "term": {
      "schema": "Schema",
      "database": "Database",
      "bucket": "Bucket",
      "prefix": "Prefix",
      "dir": "Directory",
      "table": "Table",
      "view": "View",
      "collection": "Collection",
      "graph": "Graph",
      "file": "File",
      "object": "Object",
      "table": "Table"
    }
  }
}
```

> **i18n 原则**：后端只传 key，不传已翻译文本；前端负责翻译。英文术语（Schema、Bucket、Collection 等）在中文环境下也可直接使用英文，不强制翻译为中文。

### 4.2 后端 API 响应增强

`TreeNode` 的 `typeLabel` 字段语义调整为 i18n key，由后端通过插件方法填充，前端查 i18n 字典后展示：

```go
type TreeNode struct {
    // 现有字段...
    Type      string `json:"type"`
    TypeLabel string `json:"typeLabel"` // i18n key，如 "engine.nodeType.schema"
}
```

### 4.3 预览 API 响应增强

预览响应中附加来自 Meta 的 item facts，attributes 以 key-value 列表形式返回。当前实现中该结构属于 Manager 预览 DTO，不是 engine `EngineCatalogFacts`：

```go
type TablePreview struct {
    // 现有字段...
    ItemMeta *PreviewItemFacts `json:"itemMeta,omitempty"` // 来自 Meta 模块
}

type PreviewItemFacts struct {
    ItemType      string            `json:"itemType"`      // 原始类型值
    ItemTypeI18nKey string          `json:"itemTypeI18nKey"` // i18n key
    FullName      string            `json:"fullName"`
    Attributes    []MetaAttribute   `json:"attributes"`    // key-value 列表
    ScannedAt     *time.Time        `json:"scannedAt"`
}

type MetaAttribute struct {
    Key   string      `json:"key"`
    Value interface{} `json:"value"`
}
```

---

## 五、Neo4j 图预览 Provider

本节为历史设计记录。当前不再新增 label / relationship 专属 preview provider；Neo4j 预览应围绕 `graph` leaf 和 `type_info.graph` 结构事实展开。

### GraphPreviewProvider

当前唯一主路径是 graph item 预览。Provider 消费 `type_info.graph.node_shapes`、`type_info.graph.relationship_shapes` 和 graph sample provider，支持按 node shape / relationship shape 作为展示筛选条件。

禁止事项：

- 不新增 `item_type=label`、`item_type=relationship` 或 `item_type=relationship_type`。
- 不把 Neo4j label / relationship type 注册成独立 catalog leaf。
- 不为 label / relationship type 新增独立 preview provider。

---

## 六、前端改造

### 6.1 右侧面板三态设计

```
DataExplorer 右侧面板
  ├── EnginePanel.vue     ← 引擎层（点击引擎根节点时显示）
  ├── NodePanel.vue       ← 节点层（点击 node 时显示）
  └── ItemPanel.vue       ← 数据项层（点击 item 时显示）
      ├── ItemMetaCard    ← 元数据信息区（上方）
      └── DataPreview     ← 数据预览区（下方，复用现有预览组件）
```

### 6.2 EnginePanel 数据来源

调用 System 模块 API 获取引擎详情：
```
GET /api/v1/system/engines/:id
```
连接状态通过：
```
POST /api/v1/system/engines/:id/test
```

### 6.3 NodePanel 数据来源

调用 Meta resource-tree API：
```
GET /api/v1/meta/resource-tree/:engine_id/node?locator=...
```
响应中的 `typeLabel` 字段直接用于展示，无需前端映射。

### 6.4 ItemPanel 数据来源

元数据区：从树节点的 `metadata` 字段读取。资源树节点使用 `node_id`，数据项使用 `item_id`；如需详情，通过标准 locator 或对应真实 ID 调用后端接口。
预览区：调用现有预览 API：
```
GET /api/v1/manager/preview?locator=...
```

---

## 七、与现有 Plan 的关系

阶段一（NoSQL 接口分拆 + Neo4j node type 修订）已完成，本文档直接进入阶段二。

---

## 八、实施步骤

### 阶段一：前置（已完成）
- NoSQL 接口分拆
- Neo4j node type 修订

### 阶段二：后端基础改造
1. `TreeNode` 增加 `TypeLabel` 字段（i18n key），`TreeBuilder.convertMetaNode` 按 `engine.term.<nodeType>` 规则填充
2. `PreviewResolver` 按 `engine.term.<itemType>` 规则填充预览项类型 i18n key
3. `ExplorerService` 构建树时沿用 Meta 节点类型，不引入插件私有术语映射接口
4. 预览响应增加 `ItemMeta` 结构（含 i18n key + key-value attributes），`PreviewResolver` 从 meta 模块读取并填充

### 阶段三：Neo4j 图预览
5. 完善 `GraphPreviewProvider`，围绕 graph item 消费 `type_info.graph`
6. 在 graph 预览中提供 node shape / relationship shape 展示与筛选
7. 注册到 PreviewRegistry

### 阶段四：前端改造
8. 新增 / 完善 i18n 字典（`engine.term.*` / `meta.*` 命名空间）
9. 新增 `EnginePanel.vue`（引擎层展示）
10. 新增 `NodePanel.vue`（节点层展示，含分页子节点列表 + 分页数据项列表）
11. 改造 `ItemPanel.vue`（可收拢元数据卡片 + 数据预览）
12. `DataExplorer.vue` 根据点击节点类型切换三态面板

### 阶段五：验收
13. 各引擎全路径测试（引擎 → 节点 → 数据项 → 预览）
14. i18n 一致性检查（无硬编码中文字符串）

---

## 九、验收标准

1. 点击任意引擎根节点，右侧显示引擎信息（名称、类型、连接状态（若 System 已提供）、扫描统计）。
2. 点击任意 node，右侧显示节点类型标签（i18n 翻译）、full_name、元数据、分页子节点/数据项列表。
3. 点击任意 item，右侧先显示可收拢的元数据卡片（默认展开），再显示数据预览。
4. 所有引擎的类型标签均通过 i18n key 机制展示，前后端均无硬编码中文字符串。
5. Neo4j graph leaf 有可用的数据预览，label / relationship type 可作为图结构视角展示或筛选。
6. NFS dir 节点和对象存储 prefix 节点点击后有有效展示（分页子目录/子对象列表）。
7. 所有 9 种引擎均通过全路径测试。

---

## 十、引擎连接状态

**引擎连接状态**：用 System 模块，引擎管理表中的 is active。
