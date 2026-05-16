# ADDP 引擎体系图

本文档只描述 ADDP 引擎体系的概念关系和模块调用链。插件接口规范见 [../spec/addp引擎插件接口规范.md](../spec/addp引擎插件接口规范.md)，能力声明结构见 [../spec/addp引擎能力声明规范.md](../spec/addp引擎能力声明规范.md)，路径语义见 [../spec/addp存储引擎路径体系规范.md](../spec/addp存储引擎路径体系规范.md)。

---

## 一、核心概念

| 概念 | 含义 |
| --- | --- |
| Engine Instance | System 中的一条引擎实例，保存租户、名称、类型、连接配置、能力声明和连接状态。 |
| Engine Plugin | `common/engine/plugins/<engine_type>` 下的插件实现，负责连接、校验、测试和能力暴露。 |
| Capability | 插件返回的结构化能力声明，版本为 `engine.capabilities/v1`。 |
| Catalog | 引擎中的真实目录层级，如 schema/table、bucket/object、database/label。 |
| Item | 可被描述、预览、读取或写入的叶子数据项。 |

### 实时 Catalog、元数据快照和数据预览的边界

ADDP 中容易混淆的三个概念需要明确区分：

| 概念 | 回答的问题 | 归属模块 | 典型场景 |
| --- | --- | --- | --- |
| 实时 Catalog | 真实引擎当前有什么？ | System | 扫描前选择 PostgreSQL schema、MinIO bucket/prefix、MongoDB collection、NFS 目录等。 |
| 元数据快照 | 平台已经扫描、记录、纳管了什么？ | Meta | 查询 `metadata.meta_node`、`metadata.meta_item`，展示已扫描资产树、字段、空间信息和扫描状态。 |
| 数据预览 | 用户要查看真实数据内容。 | Manager | 表格预览、对象/文件预览、空间瓦片和后端 preview provider 组合。 |

边界原则：

- System 负责引擎控制面和实时 catalog 发现，对外提供 `POST /api/v1/system/engines/:id/catalog/children`。
- Meta 负责扫描任务、元数据落库、元数据快照查询和索引事件，不再提供新的实时浏览公共接口。
- Manager 负责数据管理体验和数据预览；展示已纳管资产时消费 Meta 快照，读取真实内容时走 Manager 后端预览能力。

---

## 二、全局架构

```mermaid
graph TB
    System[System<br/>引擎登记/加密/能力声明/连接状态]
    Common[common/engine/plugin<br/>插件注册表与 provider]
    Meta[Meta<br/>扫描 catalog 并落 meta_node/meta_item]
    Manager[Manager<br/>探查树/预览/缓存]
    Develop[Develop<br/>查询/工作流/Notebook]
    Service[Service<br/>查询服务/空间服务发布]

    System --> Common
    Meta --> System
    Manager --> System
    Develop --> System
    Service --> System

    Meta --> Common
    Manager --> Common
    Develop --> Common
    Service --> Common

    Meta --> MetaStore[(metadata.meta_node/meta_item)]
    Manager --> MetaStore
```

---

## 三、Provider 关系

```mermaid
classDiagram
    class EnginePlugin {
        +Type()
        +DisplayName()
        +EngineOrigin()
        +ValidateConnectionInfo()
        +TestConnection()
        +Capabilities()
    }

    class DSNProvider {
        +BuildDSN()
    }

    class CatalogModelProvider {
        +CatalogModel()
    }

    class CatalogProvider {
        +ListChildren()
        +ResolvePath()
    }

    class ItemMetadataProvider {
        +DescribeItem()
    }

    class StoreProvider {
        +StoreSemantics()
    }

    class ContentReadableProvider {
        +OpenContent()
    }

    class ContentWritableProvider {
        +CreateContent()
    }

    class RangeReadableProvider {
        +OpenRange()
    }

    class RangeWritableProvider {
        +WriteRange()
    }

    class BatchReadableProvider {
        +ReadBatch()
    }

    class BatchWritableProvider {
        +WriteBatch()
    }

    class QueryRuntimeProvider {
        +QueryLanguages()
        +GenerateSampleQuery()
        +ExecuteRuntimeQuery()
    }

    class WorkflowRuntimeProvider {
        +ListOperators()
        +ExecuteWorkflow()
    }

    class ScriptRuntimeProvider {
        +OpenSession()
    }

    EnginePlugin <|-- CatalogModelProvider
    EnginePlugin <|-- CatalogProvider
    EnginePlugin <|-- ItemMetadataProvider
    EnginePlugin <|-- DSNProvider
    EnginePlugin <|-- StoreProvider
    StoreProvider <|-- ContentReadableProvider
    StoreProvider <|-- ContentWritableProvider
    StoreProvider <|-- RangeReadableProvider
    StoreProvider <|-- RangeWritableProvider
    StoreProvider <|-- BatchReadableProvider
    StoreProvider <|-- BatchWritableProvider
    EnginePlugin <|-- QueryRuntimeProvider
    EnginePlugin <|-- WorkflowRuntimeProvider
    EnginePlugin <|-- ScriptRuntimeProvider
```

---

## 四、模块消费关系

| 模块 | 消费方式 |
| --- | --- |
| System | 调用 `EnginePlugin` 做连接测试、连接信息校验和 capabilities 刷新；连接信息统一保存为 `connection_info` map。 |
| Meta | 调用 `CatalogProvider` 和 `ItemMetadataProvider` 扫描真实目录与叶子元数据；先按 `CatalogModelSpec` 理解目录层级，再结合 provider 组合选择扫描策略。 |
| Manager | 使用 Meta 树展示探查目录；预览结构化数据优先使用 preview / batch read，预览对象或文件优先使用 preview / content read。 |
| Develop | 根据 `capabilities.compute` 选择查询、工作流或 Notebook 引擎。 |
| Service | 使用 query runtime 和 Meta item/spatial 元数据发布数据服务。 |
| Transfer | 执行面仍由 Transfer Reader/Writer 承担，后续通过插件能力和 Transfer 模块适配层统一配置来源；高吞吐读写优先消费 batch / stream 能力。 |

---

## 五、当前支持的引擎

| 引擎族 | 引擎 |
| --- | --- |
| 表格型 | PostgreSQL、MySQL、Doris、ClickHouse、Spark SQL |
| 文档型 | MongoDB |
| 图数据库 | Neo4j |
| 对象存储 | MinIO、S3 |
| 文件系统 | NFS |
| 工作流运行时 | Python Workflow、Spark Workflow、Math Workflow |
| 脚本/Notebook | Jupyter |

---

## 六、调用原则

- 上层模块优先按 capabilities 判断可用性，不按 `engine_type` 硬编码功能入口。
- `engine_family` 只保留粗分类意义；涉及 catalog 层级、item 术语和 Meta 扫描编排时，统一以 `CatalogModelSpec` 与 provider 组合为准。
- Meta 的扫描目标字段使用 `catalog_paths` 表达文件系统、对象存储等 catalog model 下的路径。
- 目录发现统一走 `CatalogProvider.ListChildren`。
- 叶子元数据统一走 `ItemMetadataProvider.DescribeItem`。
- 查询统一走对应 runtime provider。
- 旧 `ListSchemas/ListTables/ListColumns/ListBuckets/ListCollections` 只能作为插件内部 helper，不作为上层契约。
