# ADDP 核心概念说明

本文档详细解释 ADDP (全域数据平台) 中的核心概念,帮助开发者和用户准确理解平台架构。

---

## 目录

1. [平台与架构](#平台与架构)
2. [用户与权限](#用户与权限)
3. [引擎系统](#引擎系统)
4. [元数据管理](#元数据管理)
5. [数据管理](#数据管理)
6. [数据传输](#数据传输)
7. [数据开发](#数据开发)
8. [编排调度](#编排调度)
9. [数据服务](#数据服务)
10. [基础设施](#基础设施)
11. [其他核心概念](#其他核心概念)

---

## 一、平台与架构

### ADDP (全域数据平台)

**ADDP (All-Domain Data Platform)** 是一个企业级的数据管理和分析平台,采用微服务架构,支持多租户隔离。平台提供从数据接入、存储、处理、分析到服务发布的全生命周期管理能力。

**核心特性**:
- 微服务架构,模块化设计
- 多租户隔离,资源独立管理
- 插件化引擎系统,支持扩展
- 统一认证和权限管理
- 可视化工作流编排

### 模块

ADDP 平台由以下核心模块组成:

| 模块 | 说明 | 端口 |
|------|------|------|
| **Portal** | 统一门户入口,集成所有模块功能 | 5170 (dev) / 80 (prod) |
| **System** | 核心系统服务:用户认证、引擎管理、日志 | 8080 |
| **Gateway** | API 网关,处理请求路由和转发 | 8000 |
| **Manager** | 数据管理:数据源连接、文件上传、数据预览 | 8081 |
| **Meta** | 元数据服务:元数据扫描、索引、搜索 | 8082 |
| **Transfer** | 数据传输:导入、导出、同步任务 | 8083 |
| **Orchestrator** | 工作流编排:跨模块任务调度 | 8084 |
| **Develop** | 数据开发:查询执行、工作流、Notebook 开发 | 8085 |
| **Service** | 数据服务:API 发布、OGC 标准服务 | 8086 |

### 共享模块

**common** (后端共享库):
- 数据库客户端(PostgreSQL、MySQL、MongoDB 等)
- 对象存储客户端(MinIO、S3)
- 数据模型(用户、引擎、任务等)
- 配置加载器
- 工具函数(JWT、加密、日志等)

**common-frontend** (前端共享库):
- **basic/**: 基础 UI 组件,无地图依赖
  - StorageEngineForm (数据源表单)
  - ImagePreview (图片预览)
  - formatters (数据格式化工具)
- **map/**: 地图相关组件,依赖 OpenLayers 和高德地图
  - GeoJsonPreview (GeoJSON 预览)
  - ShapefilePreview (Shapefile 预览)
  - TablePreview (表格数据预览)

### engines 目录

**engines/** 目录包含 ADDP 平台的内置扩展引擎实现:

| 引擎 | 类型 | 说明 | 路径 |
|------|------|------|------|
| **python_workflow** | 空间计算引擎 | 基于 Python 的内存空间计算,提供 21 个空间算子 | `engines/python_workflow/` |
| **spark-workflow** | 分布式空间计算引擎 | 基于 Spark 的大规模空间数据处理 | `engines/spark-workflow/` |
| **jupyter** | Notebook 执行引擎 | 交互式 Notebook 环境，支持 Python 和 Shell | `engines/jupyter/` |

这些引擎在系统启动时自动注册为**内置引擎**,全局可用。

### 微服务架构

ADDP 采用基于 Docker 的微服务架构:
- 每个模块独立部署,有自己的 Backend 和 Frontend
- 通过 Gateway 统一路由外部请求
- 共享基础设施(PostgreSQL、Redis、MinIO、Meilisearch)
- 模块间通过 HTTP API 通信

---

## 二、用户与权限

### 用户类型

**超级管理员 (SuperAdmin)**:
- 系统级最高权限用户
- 管理所有租户和系统配置
- 默认账号: `SuperAdmin` / `20251001#SuperAdmin`
- **注意**: 默认禁用,需在 `.env` 中设置 `ENABLE_SUPER_ADMIN=true` 启用

**租户管理员 (Tenant Admin)**:
- 管理单个租户内的所有资源
- 管理租户内的用户和权限
- 默认账号: `admin` / `123456` (管理 "默认租户")

**普通用户 (Regular User)**:
- 仅能访问被授权的资源
- 执行数据查询、上传、分析等操作
- 由租户管理员创建和管理

### 租户 (Tenant)

**租户** 是 ADDP 平台中资源隔离的基本单位:
- 每个租户拥有独立的用户、引擎、数据资源
- 租户间数据完全隔离
- 支持多租户部署,降低运维成本
- 数据表通过 `tenant_id` 字段实现隔离

**内置租户**:
- 系统启动时自动创建 "默认租户" (ID=1)
- 所有用户必须归属于某个租户

### 角色与权限

**RBAC 权限模型** (Role-Based Access Control):
- **系统角色**: SuperAdmin (跨租户权限)
- **租户角色**: Admin、Developer、Viewer 等
- **权限控制**: 基于角色的资源访问控制

---

## 三、引擎系统

### 引擎 (Engine)

**引擎** 是 ADDP 平台中所有数据源和计算资源的统一抽象。引擎代表一个可以存储数据或执行计算的外部系统(数据库、对象存储)或内部模块(空间计算引擎)。

**核心属性**:
- **引擎类型** (EngineType): 如 `postgresql`、`api.python-workflow`
- **引擎分类** (EngineCategory): `standard` 或 `extension`
- **连接信息** (ConnectionInfo): 数据库连接串、API 端点等
- **能力声明** (Capabilities): 引擎支持的功能列表

### 引擎分类
下面按照不同维度分类，相互存在交叉。

#### 1. 按功能分类

**存储引擎**:
- 提供数据存储能力
- 示例: PostgreSQL、MySQL、MinIO、S3
- 能力: `{"storage": [{"type": "relational_db"}]}`

**计算引擎**:
- 提供数据处理和分析能力
- 计算能力包括:
  - **SQL等查询语言的计算**: 执行SQL等查询(PostgreSQL、MySQL、Doris、MongoDB)
  - **算子计算**: 执行空间和非空间的算子工作流(Python、Spark 工作流引擎)
  - **Notebook 计算**: Jupyter Notebook 交互式开发
  - **组合计算**: 同时支持多种计算方式
- 能力: `{"compute": [{"dev_modes": ["query"], "description": "SQL查询"}]}`

**计算存储兼有**:
- 同时提供存储和计算能力
- 示例: PostgreSQL (存储数据 + SQL 查询)、Doris (OLAP 存储 + 分析查询)
- 能力: `{"storage": [...], "compute": [...]}`

#### 2. 按来源分类

**标准引擎 (Standard Engine)**:
- 通过标准协议(JDBC、S3)访问的外部数据库/存储
- 由用户手动注册,需要填写连接信息
- 示例: PostgreSQL、MySQL、Doris、ClickHouse、MongoDB、MinIO、S3
- 类型命名: 直接使用数据库名称(如 `postgresql`、`mysql`)

**扩展引擎 (Extension Engine / API Engine)**:
- ADDP 平台内置的计算模块,通过 HTTP API 调用
- ADDP平台内置了若干扩展引擎（均放在engines目录下）；用户也可以按照约定的标准自定义开发，然后注册到平台中
- 示例: python工作流引擎、Spark 工作流引擎
- 类型命名: 以 `api.` 开头(如 `api.python-workflow`)

#### 3. 按注册方式分类

**注册引擎 (Registered Engine)**:
- 由用户通过前端表单或 API 手动创建，并注册进来
- 关联到特定租户(`tenant_id`)
- 可被用户删除或修改
- `is_builtin = false`

**内置引擎 (Builtin Engine)**:
- 由系统启动时自动注册
- 不属于任何租户(`tenant_id = null`),全局可见
- 不可删除或修改核心配置(防止误操作)
- `is_builtin = true`
- 具有全局唯一标识符(`unique_identifier`,如 `"api.python-workflow"`)

### 引擎能力 (Capabilities)

引擎的 `capabilities` 字段是 JSONB 格式,描述引擎支持的功能:

**能力结构**:
```json
{
  "storage": [
    {
      "type": "relational_db",        // 关系型数据库
      "formats": ["parquet", "csv"]   // 支持的数据格式
    }
  ],
  "compute": [
    {
      "dev_modes": ["query"],           // 开发方式
      "supported_sources": ["postgresql", "mysql"],  // 支持的数据源
      "features": ["incremental", "scheduled"],      // 功能特性
      "description": "SQL查询"        // 能力描述
    }
  ]
}
```

**开发方式** (`dev_modes`):
**dev_modes 是引擎能力的核心字段**，声明引擎在 Develop 模块中提供的开发界面类型：
- `query`: 查询开发（数据库计算引擎），对应查询工作台界面，支持 SQL、MQL 等查询语言
- `workflow`: 可视化工作流（工作流计算引擎），对应工作流编辑器
- `notebook`: 基于 Jupyter 的 Notebook 开发（Notebook 执行引擎），对应 Notebook 编辑器

**功能特性** (`features`):
声明引擎支持的功能特性，例如：
- `incremental`: 增量处理
- `scheduled`: 定时调度
- `parallel`: 并行处理
- `async`: 异步执行
- `retry`: 失败重试

**支持的数据源** (`supported_sources`):
声明引擎支持处理的数据源类型，例如：
- `postgresql`, `mysql`: 关系型数据库
- `minio`, `s3`: 对象存储
- `geojson`, `shapefile`: 空间数据格式

**示例**:

PostgreSQL 引擎:
```json
{
  "storage": [{"type": "relational_db"}],
  "compute": [{"dev_modes": ["query"], "description": "SQL查询"}]
}
```

GeoPandas 引擎:
```json
{
  "compute": [{
    "dev_modes": ["workflow"],
    "supported_formats": ["geojson", "wkt", "shapely"],
    "features": ["dag", "memory_efficient", "batch"],
    "description": "空间数据分析"
  }]
}
```

### 支持的引擎列表

**标准引擎**:
- PostgreSQL - 关系型数据库,支持空间扩展(PostGIS)
- MySQL - 关系型数据库
- Doris - OLAP 分析型数据库
- ClickHouse - 列式存储 OLAP 数据库
- MongoDB - 文档型数据库
- Apache Spark - 分布式 SQL 查询引擎
- MinIO - 开源对象存储(S3 兼容)
- S3 - AWS 对象存储

**扩展引擎**:
- **Python Workflow** - 基于python的单节点工作流计算引擎
  - 类型: `api.python-workflow`
  - 能力: 算子工作流(多个空间和非空间算子)
  - 适用场景: 中小规模空间数据分析(< 100 万行)
- **Spark Workflow** - 基于spark的分布式工作流引擎，需要用户手动设置spark标准计算引擎支持
  - 类型: `api.spark_workflow`
  - 能力: 大规模数据处理
  - 适用场景: 大规模数据分析(> 100 万行)
- **Jupyter** - Notebook 执行引擎
  - 类型: `api.jupyter`
  - 能力: 交互式 Python Notebook
  - 适用场景: Notebook 开发、数据探索

### 引擎插件系统

ADDP 采用插件化架构支持新引擎扩展:
- **插件位置**: `common/database/plugins/`
- **接口定义**: `common/database/plugin/interface.go`
- **插件注册**: 通过 `dbbridge` 自动加载

**新增引擎指南**: 参考 `docs/addp新增存储引擎指南.md`

---

## 四、元数据管理

### 元数据 (Metadata)

**元数据** 是描述数据的数据,包括:
- 数据库元数据: 库名、表名、字段名、数据类型
- 文件元数据: 文件名、大小、格式、路径
- 空间元数据: 坐标系、边界框、要素数量
- 统计元数据: 记录数、字段分布、空值率

**作用**:
- 数据资产目录管理
- 全文搜索和数据发现
- 数据血缘追踪
- 数据质量评估

### 元数据扫描

**基础扫描 (Basic Scan)**:
- 获取数据库/文件的基本结构信息
- 扫描库名、表名、字段名、数据类型
- 快速,资源占用少
- 适合大量数据源的初步扫描

**深度扫描 (Deep Scan)**:
- 在基础扫描基础上,分析数据内容
- 统计记录数、字段分布、空值率、唯一值
- 检测空间数据的边界框、坐标系
- 耗时较长,资源占用大
- 适合重要数据源的详细分析

### 元数据索引

**Meilisearch 全文搜索**:
- 将扫描的元数据存入 Meilisearch
- 支持中文分词和模糊搜索
- 提供快速的元数据资产搜索
- 索引字段: 表名、字段名、描述、标签

### 定时调度

**Cron 定时扫描**:
- 支持配置 Cron 表达式自动触发扫描
- 保持元数据与数据源同步
- 示例: `0 2 * * *` (每天凌晨 2 点扫描)

---

## 五、数据管理

### 数据源 (Data Source)

**数据源** 是指通过引擎连接的外部数据库或存储系统:
- 基于引擎实例创建
- 包含具体的连接配置(主机、端口、认证)
- 一个引擎可以创建多个数据源实例

### 上传目录 (Upload Directory)

**上传目录** 是 MinIO/S3 中的文件组织结构:
- 按租户隔离:`{tenant_id}/`
- 支持多级目录结构
- 存储用户上传的文件(Shapefile、GeoJSON、图片、视频等)
- 与元数据扫描集成,自动索引文件

### 数据预览 (Data Preview)

ADDP 支持多种数据类型的预览:

**表格数据预览**:
- 显示前 N 行记录
- 支持 PostgreSQL、MySQL、Doris、ClickHouse
- 前端组件: `TablePreview`

**空间数据预览**:
- 在地图上可视化空间几何
- 支持 GeoJSON、Shapefile、PostGIS
- 前端组件: `GeoJsonPreview`、`ShapefilePreview`

**文件预览**:
- 图片: JPG、PNG、GIF (组件: `ImagePreview`)
- 视频: MP4、AVI (组件: `VideoPreview`)
- 文档: PDF、CSV (在线查看)

### 对象存储 (Object Storage)

**MinIO/S3 文件管理**:
- 分块上传,支持大文件(> 5GB)
- 断点续传,网络中断后可恢复
- 按模块隔离 Bucket: `system-files`、`manager-files`
- 预签名 URL,安全访问私有文件

### 数据类型 (Data Type)

**关系型数据** (Relational):
- 存储在 PostgreSQL、MySQL、Doris 等数据库
- 结构化表格数据
- 支持 SQL 查询

**文档型数据** (Document):
- 存储在 MongoDB
- JSON/BSON 格式
- 灵活的 Schema

**空间型数据** (Spatial):
- 几何对象(点、线、面)
- 存储在 PostGIS、文件(Shapefile、GeoJSON)
- 支持空间分析

**文件型数据** (File):
- 存储在 MinIO/S3
- 图片、视频、文档等非结构化数据

### 数据格式 (Data Format)

**空间数据格式**:
- **Shapefile**: ESRI 标准格式(.shp + .shx + .dbf + .prj)
- **GeoJSON**: 基于 JSON 的空间数据格式
- **GeoPackage**: SQLite 扩展的空间数据库格式
- **WKT/WKB**: 空间几何的文本/二进制表示

**表格数据格式**:
- **CSV**: 逗号分隔值文件
- **Excel**: .xlsx 格式
- **Parquet**: 列式存储格式(高性能)

**其他格式**:
- 图片: JPG、PNG、GIF、TIFF
- 视频: MP4、AVI、MOV
- 文档: PDF、TXT

---

## 六、数据传输

### 导入 (Import)

**从外部数据源导入数据到 ADDP**:
- 支持的源: PostgreSQL、MySQL、CSV、Shapefile、S3
- 支持的目标: PostgreSQL、Doris、ClickHouse
- 数据转换: 格式转换、字段映射、类型转换

### 导出 (Export)

**从 ADDP 导出数据到外部系统**:
- 支持的目标: PostgreSQL、MySQL、MinIO、S3、CSV
- 支持过滤条件和字段选择
- 批量导出,支持大数据量

### 同步 (Sync)

**数据源间的增量/全量同步**:
- 增量同步: 基于时间戳或 ID 增量
- 全量同步: 完整覆盖目标表
- 定时同步: 配置 Cron 表达式自动同步
- 双向同步: A ↔ B 数据同步

### 传输任务 (Transfer Task)

**基于 Asynq 的异步任务队列**:
- 任务类型: `transfer:import`、`transfer:export`、`transfer:sync`
- 优先级队列: `critical`、`default`、`low`
- 并发控制,避免资源争抢
- 任务重试,失败自动重试

### 传输记录 (Transfer Record)

**任务执行历史和状态**:
- 状态: `pending`、`running`、`success`、`failed`
- 记录: 开始时间、结束时间、处理行数、错误信息
- 日志: 详细的执行日志,便于排查问题

---

## 七、数据开发

### 查询开发

**SQL等查询语言的 编辑、执行、结果展示**:
- 支持多种数据库的 SQL 方言(PostgreSQL、MySQL、Doris、ClickHouse)，MQL（MongoDB）
- 代码编辑器: 语法高亮、自动补全、格式化
- 结果展示: 表格视图、导出 CSV
- 执行历史: 保存 SQL 和结果,可回溯

### 算子工作流 (Operator Workflow)

**算子工作流** 是基于**数据处理算子**的可视化 DAG 工作流,用于空间和非空间的数据分析和处理。
 
**核心特点**:
- **节点粒度**: 细粒度算子(如 buffer、intersection、centroid)
- **DAG 层级**: 算子级别的有向无环图
- **数据传递**: GeoDataFrame 在内存中传递
- **执行引擎**: GeoPandas (内存计算) 或 Spark 工作流引擎 (分布式计算)
- **适用场景**: 数据分析、地理计算

**算子** 是工作流中的计算节点,每个算子执行一个特定的数据处理操作:
- **输入**: 上游算子的输出(GeoDataFrame)
- **参数**: 算子特定的配置参数
- **输出**: 处理后的 GeoDataFrame


**工作流 DAG 示例**:
```json
{
  "tasks": [
    {
      "id": "t1",
      "operator": "buffer",
      "params": {
        "distance": 1000,
        "resolution": 16
      },
      "depends_on": []
    },
    {
      "id": "t2",
      "operator": "centroid",
      "params": {
        "input_gdf": {"$ref": "t1"}
      },
      "depends_on": ["t1"]
    }
  ]
}
```

**执行引擎选择**:
- **Python Workflow**: 数据量 < 100 万行,内存计算,快速
- **Spark Workflow 引擎**: 数据量 > 100 万行,分布式计算,可扩展

### Notebook开发

**Jupyter Notebook 交互式开发**:
- **Jupyter 引擎**: 交互式 Notebook 环境
- 代码编辑器: 支持 Python、Shell
- 变量传递: 工作流间传递变量
- 结果输出: 文本、图表、GeoDataFrame

### 典型使用场景

- **城市缓冲区分析**: 对兴趣点创建 1000 米缓冲区 → 统计缓冲区内人口
- **空间相交分析**: 两个空间图层的相交 → 提取交集部分
- **质心计算**: 计算多边形的质心 → 用于地图标注
- **格式转换**: GeoJSON → Shapefile 或反向转换

---

## 八、编排调度

### 任务库 (Task Template)

**任务库** 是由各模块提供的可复用任务集合:

**任务提供者**:
- **Transfer 模块**: 提供数据导入、导出、同步任务
- **Meta 模块**: 提供元数据扫描任务(基础扫描、深度扫描)
- **Manager 模块**: 提供 MVT 瓦片生成、数据预览任务
- **Develop 模块**: 提供查询执行、工作流执行、Notebook 执行任务

**工作原理**:
1. 各模块在 System 模块中注册自己的能力和任务 API
2. Orchestrator 模块通过能力注册中心发现可用任务
3. 编排时选择任务,配置参数,设置依赖关系
4. 执行时通过动态引擎调用机制调用对应模块的任务 API

**任务特点**:
- 预定义的任务配置(参数、引擎、超时)
- 可被多个编排引用
- 跨模块数据流贯通
- 示例: "扫描 PostgreSQL 元数据"、"生成 MVT 瓦片"、"导入 CSV 数据"

### 任务 (Task)

**任务** 是编排中的具体执行单元:
- 调用特定引擎的 API
- 配置参数(支持模板化)
- 设置超时和重试策略
- 依赖其他任务(DAG 依赖)

### 任务编排流 (Task Orchestration Flow)

**任务编排流** 是基于**业务任务**的跨模块 DAG 工作流,用于复杂的数据流水线和 ETL 作业。

**核心特点**:
- **节点粒度**: 粗粒度业务任务(如扫描元数据、导入数据、生成瓦片)
- **DAG 层级**: 任务级别的有向无环图
- **数据传递**: 参数模板 `{{stepID.field}}` 引用前序任务结果
- **执行引擎**: 跨模块动态引擎调用(Meta、Transfer、Manager、Develop 等)
- **适用场景**: 跨模块数据流水线、定时 ETL 作业

**编排定义示例**:
```json
{
  "name": "数据处理流水线",
  "steps": [
    {
      "id": "scan_metadata",
      "name": "扫描元数据",
      "engine_identifier": "meta.scanner.default",
      "parameters": {
        "engine_id": 1,
        "scan_type": "full"
      },
      "depends_on": [],
      "timeout": 300
    },
    {
      "id": "generate_mvt",
      "name": "生成 MVT 瓦片",
      "engine_identifier": "manager.mvt.default",
      "parameters": {
        "engine_id": 1,
        "schema": "public",
        "table": "{{scan_metadata.table_name}}"
      },
      "depends_on": ["scan_metadata"],
      "timeout": 600
    }
  ],
  "schedule": "0 2 * * *"
}
```

### 调度 (Scheduling)

**定时调度**:
- 基于 Cron 表达式配置调度规则
- 示例: `0 2 * * *` (每天凌晨 2 点执行)
- 自动创建执行记录并异步执行

**手动触发**:
- 通过 API 或前端手动触发执行
- 支持传入参数覆盖默认配置

### 依赖管理

**DAG 拓扑排序**:
- 使用 Kahn 算法自动解析任务依赖
- 检测循环依赖,防止死锁
- 按拓扑顺序依次执行任务

**参数模板化**:
- 支持 `{{stepID.field}}` 语法引用前序任务结果
- 嵌套字段引用: `{{step1.result.nested.field}}`
- 自动类型转换(字符串、数字、对象)

### 两种工作流对比

| 维度 | 算子工作流 (Develop) | 任务编排流 (Orchestrator) |
|------|---------------------|-------------------------|
| **节点粒度** | 细粒度算子 (buffer, centroid) | 粗粒度业务任务 (扫描元数据, 导入数据) |
| **DAG 层级** | 算子级别 DAG | 任务级别 DAG |
| **执行引擎** | GeoPandas/Spark 工作流引擎 引擎 | 跨模块动态引擎调用 |
| **数据传递** | GeoDataFrame 内存传递 | 参数模板 `{{stepID.field}}` |
| **适用场景** | 空间数据分析、地理计算 | 跨模块数据流水线、ETL 作业 |
| **存储表** | `develop.dev_items` | `orchestrator.orchestrations` |
| **执行记录** | `develop.dev_executions` | `orchestrator.executions` |
| **前端界面** | 工作流画布 (算子拖拽) | 编排表单 (步骤配置) |

### 典型使用场景

**任务编排流场景**:
- 每日凌晨扫描数据库元数据 → 生成 MVT 瓦片 → 预缓存热点区域
- 从 CSV 导入数据 → 执行空间分析 → 导出结果到 S3
- 多数据源同步: PostgreSQL → MySQL → MongoDB
- 跨模块工作流: Meta 扫描 → Transfer 传输 → Manager 预览

**嵌套调用模式**:
Orchestrator 可以调用 Develop 模块的工作流任务作为一个步骤:
```json
{
  "steps": [
    {
      "id": "extract_data",
      "name": "提取数据",
      "engine_identifier": "develop.sql.default",
      "parameters": {
        "engine_id": 1,
        "sql": "SELECT * FROM cities WHERE population > 1000000"
      }
    },
    {
      "id": "spatial_analysis",
      "name": "空间分析工作流",
      "engine_identifier": "develop.workflow.default",
      "parameters": {
        "workflow_name": "city_buffer_analysis",
        "input_table": "{{extract_data.result_table}}"
      }
    },
    {
      "id": "export_result",
      "name": "导出结果",
      "engine_identifier": "transfer.export.default",
      "parameters": {
        "source_table": "{{spatial_analysis.output_table}}",
        "target_format": "geojson"
      }
    }
  ]
}
```

**说明**:
- Orchestrator 只知道 Develop 模块提供的任务(如 SQL 执行、工作流执行)
- 具体使用哪个引擎(GeoPandas、Spark 工作流引擎)由 Develop 模块内部决定
- 通过 `engine_identifier` 调用 Develop 模块注册的任务,而不直接引用底层引擎

---

## 九、数据服务

### 数据服务 (Data Service)

**数据服务** 是将数据以 API 形式对外发布:
- RESTful API: GET、POST 请求访问数据
- 权限控制: 公开服务、需认证服务
- 访问统计: 记录调用次数、流量

### 服务注册

**服务元数据注册**:
- 服务名称、描述、版本
- API 端点和参数定义
- 数据源配置(引擎、表、字段)

### OGC 标准服务

**支持的 OGC 标准**:
- **WMS** (Web Map Service): 地图图片服务
- **WFS** (Web Feature Service): 矢量要素服务
- **WMTS** (Web Map Tile Service): 瓦片地图服务
- **WCS** (Web Coverage Service): 栅格数据服务

### 查询服务

**RESTful 数据查询接口**:
- 条件查询: WHERE 条件过滤
- 分页查询: offset/limit
- 字段选择: 指定返回字段
- 空间查询: 边界框查询、空间关系查询

---

## 十、基础设施

### 系统基础设施

**ADDP 元数据存储** (docker-compose.infra.yml):
- **PostgreSQL**: 存储用户、引擎、元数据索引、任务定义等
  - Schema 隔离: `system`、`manager`、`metadata`、`transfer`、`orchestrator`、`develop`、`service`
  - 端口: 15432
- **Redis**: 缓存、任务队列 (Asynq)、会话管理
  - Key 命名规范: `{module}:{middleware}:{function}:{id}`
  - 端口: 16379
- **MinIO**: 系统文件存储 (用户头像、预览缓存、MVT 瓦片、临时文件)
  - Bucket 隔离: `system`、`manager`、`meta`、`transfer`、`orchestrator`、`develop`、`service`
  - API 端口: 19000
  - Console 端口: 19001
- **Meilisearch**: 全文搜索引擎 (元数据资产搜索)
  - Index 命名: `{module}:{entity_type}`
  - 端口: 17700

### 业务数据库

**用户业务数据存储** (business/docker-compose.yml, 独立部署):
- **business-postgres**: 用户通过 ADDP 管理的实际业务数据
  - 端口: 5433
- **business-minio**: 用户上传的业务文件 (Shapefile、GeoJSON、图片、视频)
  - API 端口: 9002
  - Console 端口: 9003

### 资源隔离

**PostgreSQL Schema 隔离**:
- 按模块隔离: `system`、`manager`、`metadata`、`transfer`、`orchestrator`、`develop`、`service`
- 避免表名冲突,权限独立管理

**MinIO Bucket 隔离**:
- 按模块隔离: `system`、`manager`、`meta`、`transfer`、`orchestrator`、`develop`、`service`
- 避免文件冲突,配额独立管理
- `manager` bucket 设置为公开读(MVT 瓦片需前端直接访问)
- 其他 bucket 均为私有访问

**Redis Key 命名规范**:
- 格式: `{module}:{middleware}:{function}:{id}`
- 示例: `system:cache:user:123`、`transfer:asynq:task:456`

**Asynq Queue 命名规范**:
- 格式: `{module}:{priority}`
- 示例: `transfer:critical`、`meta:default`

### 认证 (Authentication)

**JWT Token 认证**:
- 用户登录后返回 JWT token
- Token 存储在 localStorage (前端)
- 请求携带 Authorization Header: `Bearer <token>`
- 后端中间件验证 token 有效性和权限

### 网关 (Gateway)

**API 路由和请求转发**:
- 统一入口: `http://localhost:8000`
- 路由规则: 根据路径前缀转发到对应模块
  - `/api/system/*` → System Backend
  - `/api/manager/*` → Manager Backend
  - `/api/meta/*` → Meta Backend
  - ...
- 生产环境中,所有外部请求通过 Gateway 路由

---

## 十一、其他核心概念

### Portal (统一门户)

**Portal** 是 ADDP 的统一入口:
- 集成所有模块功能,提供一致的用户体验
- 左侧边栏: 所有模块的导航菜单
- 主区域: 通过 iframe 动态加载模块前端
- 一次登录,访问所有模块

**两种访问模式**:
1. **统一门户模式** (推荐):
   - 单一入口: http://localhost:5170 (dev) / http://localhost:8000 (prod)
   - 集成导航和统一认证
2. **独立模块模式**:
   - 直接访问各模块前端 (如 http://localhost:5173)
   - 适合独立部署单个模块

### Backend 与 Worker 分离

部分模块采用 Backend/Worker 分离架构:

**Backend**:
- 处理 HTTP API 请求
- 执行业务逻辑
- 返回即时响应

**Worker**:
- 执行后台任务(扫描、传输、计算等)
- 基于 Asynq 任务队列
- 异步处理,不阻塞请求

**采用分离架构的模块**:
- **Meta**: Backend (API) + Worker (元数据扫描)
- **Transfer**: Backend (API) + Worker (数据导入/导出/同步)

### Copilot (AI 助手)

**Copilot** 是 ADDP 的 AI 辅助功能:
- SQL 生成: 根据自然语言生成 SQL 查询
- 数据分析建议: 推荐合适的分析方法
- 代码补全: Python/SQL 代码智能补全
- 错误诊断: 分析错误信息并提供修复建议

### 配置中心

**统一的环境变量和配置管理**:
- 根配置文件: `.env`
- 模块配置文件: `{module}/.env.local`
- 配置优先级: 模块配置 > 根配置
- 详细说明: `docs/addp配置介绍.md`

### 日志

**结构化日志**:
- 日志格式: JSON 格式,便于解析和搜索
- 日志级别: DEBUG、INFO、WARN、ERROR
- 租户隔离: 日志中包含 `tenant_id` 字段
- 日志输出: 统一输出到 `logs/` 目录,按模块和前后端分离

### 缓存

**Redis 缓存策略**:
- 用户信息缓存: TTL 30 分钟
- 引擎配置缓存: TTL 5 分钟
- 元数据缓存: TTL 10 分钟
- 缓存失效: 数据更新时主动失效

### 文件上传

**分块上传和断点续传**:
- 大文件分块: 每块 5MB
- 并行上传: 多个分块并发上传
- 断点续传: 网络中断后从上次位置继续
- 进度反馈: 实时显示上传进度

### 数据预处理

**数据清洗和格式转换**:
- 数据类型转换: 字符串 → 数字、日期等
- 空值处理: 填充默认值、删除空行
- 格式转换: CSV → Parquet、GeoJSON → Shapefile
- 编码转换: GBK → UTF-8

---

## 附录: 术语对照表

| 中文 | 英文 | 说明 |
|------|------|------|
| 全域数据平台 | All-Domain Data Platform (ADDP) | 平台全称 |
| 引擎 | Engine | 数据源和计算资源的统一抽象 |
| 标准引擎 | Standard Engine | 外部数据库/存储 |
| 扩展引擎 | Extension Engine / API Engine | 内置计算模块 |
| 注册引擎 | Registered Engine | 用户手动创建 |
| 内置引擎 | Builtin Engine | 系统自动注册 |
| 算子 | Operator | 数据处理的基本单元 |
| 算子工作流 | Operator Workflow | 算子级别的 DAG |
| 任务编排流 | Task Orchestration Flow | 任务级别的 DAG |
| 元数据 | Metadata | 描述数据的数据 |
| 租户 | Tenant | 资源隔离的基本单位 |
| 统一门户 | Portal | 集成所有模块的入口 |

---

## 文档版本

- **版本**: v1.0
- **更新日期**: 2025-12-29
- **作者**: ADDP 开发团队

---

## 相关文档

- [ADDP 开发原则](addp开发原则.md)
- [ADDP 配置介绍](addp配置介绍.md)
- [ADDP 部署和开发步骤](addp部署和开发步骤.md)
- [ADDP 新增存储引擎指南](addp新增存储引擎指南.md)
- [ADDP 数据类型扩展指南](addp数据类型扩展指南.md)
- [ADDP 共享模块介绍](addp共享模块介绍.md)
- [ADDP 技术栈规约](addp技术栈规约.md)
- [System 模块详情](../system/CLAUDE.md)
- [Manager 模块详情](../manager/CLAUDE.md)
- [Meta 模块详情](../meta/CLAUDE.md)
- [Transfer 模块详情](../transfer/CLAUDE.md)
- [Orchestrator 模块详情](../orchestrator/CLAUDE.md)
- [Develop 模块详情](../develop/CLAUDE.md)
- [Service 模块详情](../service/CLAUDE.md)
