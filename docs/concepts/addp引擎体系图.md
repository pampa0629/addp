# ADDP 引擎体系图

本文档展示 ADDP 平台的引擎系统架构、分类体系和能力声明机制。

---

## 目录

1. [引擎概念](#引擎概念)
2. [引擎插件三层接口架构](#引擎插件三层接口架构)
3. [引擎分类体系](#引擎分类体系)
4. [支持的引擎列表](#支持的引擎列表)
5. [引擎能力声明](#引擎能力声明)

---

## 引擎概念

**引擎 (Engine)** 是 ADDP 平台中所有数据源和计算资源的统一抽象。引擎代表一个可以存储数据或执行计算的外部系统(数据库、对象存储)或内部模块(空间计算引擎)。

**核心属性**:
- **引擎类型** (EngineType): 如 `postgresql`、`mongodb`、`python_workflow`
- **引擎分类** (EngineCategory): `standard` 或 `extension`
- **连接信息** (ConnectionInfo): 数据库连接串、API 端点等
- **能力声明** (Capabilities): 引擎支持的功能列表(JSONB 格式)

---

## 引擎插件三层接口架构

ADDP 采用三层接口架构实现引擎插件化,支持灵活扩展:

```mermaid
classDiagram
    class EnginePlugin {
        <<interface>>
        +Type() string
        +DisplayName() string
        +EngineCategory() string
        +DefaultPort() int
        +TestConnection(info) error
        +BuildConnectionString(info) string
    }

    class StoragePlugin {
        <<interface>>
        +SupportsMetadataQuery() bool
    }

    class ComputePlugin {
        <<interface>>
        +GetSupportedOperators() []Operator
        +HealthCheckEndpoint() string
    }

    class NoSQLPlugin {
        <<interface>>
        +ListDatabases(client) []string
        +ListCollections(client, db) []string
        +GetCollectionStats(client, db, coll) Stats
        +IsSystemDatabase(db) bool
        +CreateClient(info) Client
        +CloseClient(client)
    }

    class RelationalDBPlugin {
        <<interface>>
        +ListSchemas(pool) []string
        +ListTables(pool, schema) []string
        +ListColumns(pool, schema, table) []Column
        +GetTableRowCount(pool, schema, table) int64
        +IsSystemSchema(schema) bool
    }

    class ObjectStoragePlugin {
        <<interface>>
        +ListBuckets(client) []Bucket
        +ListObjects(client, bucket) []Object
        +GetObjectMetadata(client, bucket, key) Metadata
        +InferContentType(key) string
    }

    class ConnectionPoolPlugin {
        <<interface>>
        +CreateConnectionPool(info) Pool
        +GetDialect() string
    }

    EnginePlugin <|-- StoragePlugin
    EnginePlugin <|-- ComputePlugin
    StoragePlugin <|-- NoSQLPlugin
    StoragePlugin <|-- RelationalDBPlugin
    StoragePlugin <|-- ObjectStoragePlugin
    RelationalDBPlugin <|-- ConnectionPoolPlugin

    class PostgreSQLPlugin {
        实现: EnginePlugin
        实现: StoragePlugin
        实现: RelationalDBPlugin
        实现: ConnectionPoolPlugin
    }

    class MongoDBPlugin {
        实现: EnginePlugin
        实现: StoragePlugin
        实现: NoSQLPlugin
    }

    class MinIOPlugin {
        实现: EnginePlugin
        实现: StoragePlugin
        实现: ObjectStoragePlugin
    }

    class PythonWorkflowPlugin {
        实现: EnginePlugin
        实现: ComputePlugin
    }

    EnginePlugin <|.. PostgreSQLPlugin
    StoragePlugin <|.. PostgreSQLPlugin
    RelationalDBPlugin <|.. PostgreSQLPlugin
    ConnectionPoolPlugin <|.. PostgreSQLPlugin

    EnginePlugin <|.. MongoDBPlugin
    StoragePlugin <|.. MongoDBPlugin
    NoSQLPlugin <|.. MongoDBPlugin

    EnginePlugin <|.. MinIOPlugin
    StoragePlugin <|.. MinIOPlugin
    ObjectStoragePlugin <|.. MinIOPlugin

    EnginePlugin <|.. PythonWorkflowPlugin
    ComputePlugin <|.. PythonWorkflowPlugin
```

**架构说明**:

**第一层:EnginePlugin (引擎基础接口)**
- 所有引擎必须实现的基础接口
- 定义引擎的基本信息和连接测试能力

**第二层:按功能分类的标记接口**
- **StoragePlugin**: 存储引擎标记(支持元数据查询)
- **ComputePlugin**: 计算引擎标记(支持算子/查询)

**第三层:按存储类型细分的功能接口**
- **RelationalDBPlugin**: 关系型数据库(PostgreSQL、MySQL、Doris、ClickHouse)
- **NoSQLPlugin**: NoSQL 数据库(MongoDB)
- **ObjectStoragePlugin**: 对象存储(MinIO、S3)
- **ConnectionPoolPlugin**: 连接池管理(用于关系型数据库)

**接口组合示例**:
- **PostgreSQL**: EnginePlugin + StoragePlugin + RelationalDBPlugin + ConnectionPoolPlugin
- **MongoDB**: EnginePlugin + StoragePlugin + NoSQLPlugin
- **MinIO**: EnginePlugin + StoragePlugin + ObjectStoragePlugin
- **Python Workflow**: EnginePlugin + ComputePlugin

---

## 引擎分类体系

ADDP 从多个维度对引擎进行分类,不同分类之间存在交叉。

### 1. 按功能分类

```mermaid
graph TB
    Engine[引擎 Engine]

    Engine --> Storage[存储引擎<br/>Storage Engine]
    Engine --> Compute[计算引擎<br/>Compute Engine]
    Engine --> Both[存储+计算<br/>Hybrid Engine]

    Storage --> StorageEx[PostgreSQL<br/>MySQL<br/>MinIO<br/>S3<br/>MongoDB]

    Compute --> SQLCompute[SQL查询计算<br/>PostgreSQL<br/>MySQL<br/>Doris<br/>ClickHouse]
    Compute --> OperatorCompute[算子工作流计算<br/>Python Workflow<br/>Spark Workflow]
    Compute --> NotebookCompute[Notebook计算<br/>Jupyter]

    Both --> HybridEx[PostgreSQL<br/>Doris<br/>ClickHouse<br/>MongoDB]

    classDef root fill:#fff9c4,stroke:#f57f17
    classDef category fill:#e1f5ff,stroke:#01579b
    classDef example fill:#f3e5f5,stroke:#4a148c

    class Engine root
    class Storage,Compute,Both category
    class StorageEx,SQLCompute,OperatorCompute,NotebookCompute,HybridEx example
```

**分类说明**:
- **存储引擎**: 提供数据存储能力(PostgreSQL、MinIO、MongoDB 等)
- **计算引擎**: 提供数据处理和分析能力
  - **SQL 查询计算**: 执行 SQL/MQL 查询
  - **算子工作流计算**: 执行空间和非空间算子工作流
  - **Notebook 计算**: Jupyter Notebook 交互式开发
- **存储+计算**: 同时提供存储和计算能力(PostgreSQL、Doris 等)

### 2. 按标准/扩展分类

```mermaid
graph LR
    Engine[引擎分类]

    Engine --> Standard[标准引擎<br/>Standard Engine]
    Engine --> Extension[扩展引擎<br/>Extension Engine]

    Standard --> StandardDB[(通过数据库/对象存储等<br/>标准协议访问)]
    StandardDB --> PG[PostgreSQL]
    StandardDB --> MySQL[MySQL]
    StandardDB --> Mongo[MongoDB]
    StandardDB --> Minio[MinIO/S3]
    StandardDB --> Others[Doris<br/>ClickHouse<br/>Spark SQL...]

    Extension --> ExtensionAPI[通过ADDP自定义<br/>HTTP API调用]
    ExtensionAPI --> PyWF[Python Workflow]
    ExtensionAPI --> SparkWF[Spark Workflow]
    ExtensionAPI --> JupyterWF[Jupyter]

    classDef root fill:#fff9c4,stroke:#f57f17
    classDef category fill:#e1f5ff,stroke:#01579b
    classDef standard fill:#e8f5e9,stroke:#1b5e20
    classDef extension fill:#f3e5f5,stroke:#4a148c

    class Engine root
    class Standard,Extension category
    class StandardDB,PG,MySQL,Mongo,Minio,Others standard
    class ExtensionAPI,PyWF,SparkWF,JupyterWF extension
```

**分类说明**:
- **标准引擎**: 通过标准协议(JDBC、S3、MongoDB Wire Protocol)访问的数据库/对象存储等
  - 类型命名: 直接使用数据库名称(如 `postgresql`、`mysql`、`mongodb`)
  - 一般由用户手动注册,需要填写连接信息
- **扩展引擎**: 通过ADDP自定义的 HTTP API 调用
  - 类型命名: 使用引擎名称(如 `python_workflow`、`spark_workflow`、`jupyter`)
  - 当前均为系统engines下几个模块自动注册,也可由用户自定义注册

### 3. 按注册方式分类

```mermaid
stateDiagram-v2
    引擎分类 --> 注册引擎: 用户手动创建
    引擎分类 --> 内置引擎: 系统启动自注册

    注册引擎 --> 关联到特定租户:tenant_id!=null
    注册引擎 --> 非内置:is_builtin = false
    注册引擎 --> 用户可删除/修改

    内置引擎 --> 全局可见 :tenant_id = null
    内置引擎 --> 内置: is_builtin = true
    内置引擎 --> 不可删除/修改
    内置引擎 --> 有全局唯一标识符<br/>unique_identifier
```

**分类说明**:
- **注册引擎 (Registered Engine)**:
  - 由用户通过前端表单或 API 手动创建
  - 关联到特定租户 (`tenant_id != null`)
  - 可被用户删除或修改
  - `is_builtin = false`
- **内置引擎 (Builtin Engine)**:
  - 由系统启动时自动注册
  - 不属于任何租户 (`tenant_id = null`),全局可见
  - 不可删除或修改核心配置(防止误操作)
  - `is_builtin = true`
  - 具有全局唯一标识符 (`unique_identifier`,如 `python_workflow`)

---

## 支持的引擎列表

ADDP 平台当前支持 **11 种**数据引擎:

```mermaid
graph TB
    subgraph "标准引擎 (8种)"
        PG[PostgreSQL<br/>关系型+空间<br/>:5432]
        MySQL[MySQL<br/>关系型<br/>:3306]
        Doris[Apache Doris<br/>HTAP实时分析<br/>:9030]
        CH[ClickHouse<br/>列式OLAP<br/>:9000]
        Mongo[MongoDB<br/>文档型NoSQL<br/>:27017]
        Spark[Spark SQL<br/>分布式查询<br/>:10000]
        Minio[MinIO<br/>对象存储<br/>:9000]
        S3[Amazon S3<br/>云对象存储<br/>:443]
    end

    subgraph "扩展引擎 (3种)"
        PyWF[Python Workflow<br/>python_workflow<br/>单节点工作流]
        SparkWF[Spark Workflow<br/>spark_workflow<br/>分布式工作流]
        Jupyter[Jupyter<br/>jupyter<br/>Notebook开发]
    end

    subgraph "插件接口实现"
        PG --> RDCP[RelationalDB<br/>ConnectionPool]
        MySQL --> RDCP
        Doris --> RDCP
        CH --> RDCP

        Mongo --> NoSQL[NoSQL<br/>Plugin]

        Minio --> OS[ObjectStorage<br/>Plugin]
        S3 --> OS

        PyWF --> Compute[Compute<br/>Plugin]
        SparkWF --> Compute
        Jupyter --> Compute
    end

    classDef standard fill:#e8f5e9,stroke:#1b5e20
    classDef extension fill:#fff9c4,stroke:#f57f17
    classDef interface fill:#e1f5ff,stroke:#01579b

    class PG,MySQL,Doris,CH,Mongo,Spark,Minio,S3 standard
    class PyWF,SparkWF,Jupyter extension
    class RDCP,NoSQL,OS,Compute interface
```

### 标准引擎详情

| 引擎 | 类型 | 默认端口 | 插件接口 | 主要能力 |
|------|------|---------|---------|---------|
| **PostgreSQL** | 关系型数据库 | 5432 | RelationalDB + ConnectionPool | SQL 查询,支持 PostGIS 空间扩展 |
| **MySQL** | 关系型数据库 | 3306 | RelationalDB + ConnectionPool | SQL 查询 |
| **Apache Doris** | HTAP 分析数据库 | 9030 | RelationalDB + ConnectionPool | 实时分析,OLAP 查询 |
| **ClickHouse** | 列式 OLAP 数据库 | 9000 | RelationalDB + ConnectionPool | 高性能分析查询 |
| **MongoDB** | 文档型 NoSQL | 27017 | NoSQL | MQL 查询,采样推断 Schema |
| **Spark SQL** | 分布式 SQL 引擎 | 10000 | Compute | 大规模 SQL 查询 |
| **MinIO** | 对象存储 | 9000 | ObjectStorage | S3 兼容,文件存储 |
| **Amazon S3** | 云对象存储 | 443 | ObjectStorage | AWS 云存储 |

### 扩展引擎详情

| 引擎 | 类型 | 能力 | 适用场景 |
|------|------|------|---------|
| **Python Workflow** | 工作流计算 | 21 个空间算子 | 中小规模数据分析(< 100 万行) |
| **Spark Workflow** | 分布式工作流 | 空间与非空间算子 | 大规模数据分析(> 100 万行) |
| **Jupyter** | Notebook 开发 | Python/Shell 交互式开发 | 数据探索,变量传递 |

---

## 引擎能力声明

引擎的 `capabilities` 字段是 JSONB 格式,声明引擎支持的功能。

### 能力结构

```mermaid
classDiagram
    class Capabilities {
        +storage[] StorageCapability
        +compute[] ComputeCapability
    }

    class StorageCapability {
        +type string
        +formats[] string
    }

    class ComputeCapability {
        +dev_modes[] string
        +supported_sources[] string
        +features[] string
        +description string
    }

    Capabilities "1" --> "*" StorageCapability
    Capabilities "1" --> "*" ComputeCapability

    class DevModes {
        <<enumeration>>
        query: 查询开发
        workflow: 可视化工作流
        notebook: Notebook开发
    }

    class Features {
        <<enumeration>>
        incremental: 增量处理
        scheduled: 定时调度
        parallel: 并行处理
        async: 异步执行
        retry: 失败重试
    }

    ComputeCapability --> DevModes
    ComputeCapability --> Features
```

### 能力示例

**PostgreSQL 引擎** (存储+计算):
```json
{
  "storage": [
    {
      "type": "relational_db",
      "formats": ["sql", "csv", "parquet"]
    }
  ],
  "compute": [
    {
      "dev_modes": ["query"],
      "supported_sources": ["postgresql"],
      "features": ["incremental", "scheduled"],
      "description": "SQL查询"
    }
  ]
}
```

**Python Workflow 引擎** (纯计算):
```json
{
  "compute": [
    {
      "dev_modes": ["workflow"],
      "supported_formats": ["geojson", "shapefile", "wkt"],
      "features": ["dag", "memory_efficient", "batch"],
      "description": "空间数据分析工作流"
    }
  ]
}
```

**MongoDB 引擎** (存储+计算):
```json
{
  "storage": [
    {
      "type": "document_db",
      "formats": ["json", "bson"]
    }
  ],
  "compute": [
    {
      "dev_modes": ["query"],
      "supported_sources": ["mongodb"],
      "features": ["aggregation", "flexible_schema"],
      "description": "MQL查询和聚合"
    }
  ]
}
```

### 开发方式 详解

**dev_modes 是引擎能力的核心字段**,声明引擎在 Develop 模块中提供的开发界面类型:

```mermaid
graph LR
    DevModes[dev_modes]

    DevModes --> Query[query<br/>查询开发]
    DevModes --> Workflow[workflow<br/>可视化工作流]
    DevModes --> Notebook[notebook<br/>Notebook开发]

    Query --> QueryUI[查询工作台界面<br/>Monaco编辑器<br/>SQL/MQL执行]
    Workflow --> WorkflowUI[工作流编辑器<br/>算子拖拽<br/>DAG可视化]
    Notebook --> NotebookUI[Notebook编辑器<br/>Jupyter界面<br/>交互式开发]

    QueryUI --> QueryEngine[PostgreSQL<br/>MySQL<br/>Doris<br/>ClickHouse<br/>MongoDB<br/>Spark SQL]
    WorkflowUI --> WorkflowEngine[Python Workflow<br/>Spark Workflow]
    NotebookUI --> NotebookEngine[Jupyter]

    classDef mode fill:#fff9c4,stroke:#f57f17
    classDef ui fill:#e1f5ff,stroke:#01579b
    classDef engine fill:#e8f5e9,stroke:#1b5e20

    class DevModes,Query,Workflow,Notebook mode
    class QueryUI,WorkflowUI,NotebookUI ui
    class QueryEngine,WorkflowEngine,NotebookEngine engine
```

**dev_modes 说明**:
- **query**: 查询开发,对应查询工作台界面,支持 SQL、MQL 等查询语言
- **workflow**: 可视化工作流,对应工作流编辑器,支持算子拖拽和 DAG 编排
- **notebook**: Notebook 开发,对应 Jupyter Notebook 编辑器,支持 Python 和 Shell

---

## 引擎插件系统

ADDP 采用插件化架构支持新引擎扩展:

```mermaid
graph TB
    subgraph "插件开发"
        Dev[开发新插件] --> Impl[实现对应接口<br/>EnginePlugin + 功能接口]
        Impl --> Register[在 dbbridge 中注册]
        Register --> Test[构建测试]
    end

    subgraph "插件位置"
        Plugins[common/engine/plugins/]
        Plugins --> PGPlugin[postgresql/]
        Plugins --> MongoPlugin[mongodb/]
        Plugins --> MinioPlugin[minio/]
        Plugins --> Custom[custom_engine/]
    end

    subgraph "接口定义"
        Interfaces[common/engine/plugin/interfaces.go]
        Interfaces --> EP[EnginePlugin]
        Interfaces --> SP[StoragePlugin]
        Interfaces --> CP[ComputePlugin]
        Interfaces --> Others[NoSQL/RelationalDB/...]
    end

    subgraph "桥接层"
        Bridge[common/dbbridge/bridge.go]
        Bridge --> Import[导入所有插件]
        Bridge --> Auto[自动注册]
    end

    Dev --> Plugins
    Impl --> Interfaces
    Register --> Bridge

    classDef dev fill:#fff9c4,stroke:#f57f17
    classDef location fill:#e8f5e9,stroke:#1b5e20
    classDef interface fill:#e1f5ff,stroke:#01579b
    classDef bridge fill:#f3e5f5,stroke:#4a148c

    class Dev,Impl,Register,Test dev
    class Plugins,PGPlugin,MongoPlugin,MinioPlugin,Custom location
    class Interfaces,EP,SP,CP,Others interface
    class Bridge,Import,Auto bridge
```

**新增引擎流程**(3 步):
1. 在 `common/engine/plugins/<enginetype>/` 创建插件,实现对应接口
2. 在 `common/dbbridge/bridge.go` 添加导入语句
3. 构建测试和功能验证

**详细指南**: 参考 [ADDP 数据引擎扩展指南](../addp数据引擎扩展指南.md)

---

## 相关文档

- [返回核心概念关系图](../addp核心概念关系图.md)
- [ADDP 数据引擎扩展指南](../addp数据引擎扩展指南.md)
- [ADDP 核心概念说明(详版)](../addp核心概念说明（详版）.md)

---

**文档版本**: v1.0
**创建日期**: 2026-02-16
**作者**: ADDP 开发团队
