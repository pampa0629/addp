# ADDP 各模块简要介绍

本文档提供 ADDP（全域数据平台）各模块的简要功能介绍。详细实现细节请参考各模块的 CLAUDE.md 或专项文档。

---

## 平台概览

**ADDP (All-Domain Data Platform)** 是一个企业级数据管理和分析平台，采用微服务架构，提供从数据接入、存储、处理、分析到服务发布的全生命周期管理能力。

**核心特性**：
- 微服务架构，模块化设计
- 多租户隔离，资源独立管理
- 插件化引擎系统，通过 provider 和 `engine.capabilities/v1` 声明能力
- 统一认证和权限管理（opaque Token、IAM、RBAC 与资源策略）
- 可视化工作流编排

---

## 核心模块

### 1. Console（控制台）

**职责定位**：控制台入口，集成所有模块功能，提供一致的用户体验

**核心能力**：
- 基于 iframe 的模块动态加载
- 一次登录访问所有模块（Browser AuthSession + iframe `postMessage`）
- 统一左侧导航菜单
- 支持独立访问和控制台嵌入两种模式

**端口**：
- 开发环境：5170
- 生产环境：80 (通过 Nginx)

**详细文档**：`console/CLAUDE.md`

---

### 2. System（核心系统）

**职责定位**：平台核心服务和唯一 IAM 逻辑权威，提供身份认证、组织与通用授权事实、引擎管理和日志审计

**核心能力**：
- 全局 User、Local Account、External Identity、Tenant Membership 和 Service Principal 管理
- Department、Project Group、Permission、Role 和 Role Assignment 管理
- 平台系统管理员、安全管理员和审计管理员三员分立
- 统一 AuthContext；业务资源 Grant / Policy 和最终授权判断归 owner 模块
- 引擎注册管理（统一保存连接配置、状态和结构化能力声明）
- opaque Access Token、Refresh Token Family 与 AuthContext
- 审计日志记录（操作、登录、API 调用）
- 系统配置和全局参数管理
- 模块注册表（各模块向 System 声明自身 API 地址和能力）
- TaskProvider 角色声明（各模块在模块注册时向 System 发布 capabilities，供 Orchestrator 和 Monitor 发现）
- API 文档（统一维护平台 OpenAPI 接口文档）

**端口**：
- Backend：8180

**详细文档**：`system/CLAUDE.md`

---

### 3. Gateway（API 网关）

**职责定位**：统一 API 入口，请求路由和转发

**核心能力**：
- 基于路径前缀的请求路由（`/api/v1/system/*` → System Backend）
- CORS 跨域支持
- 请求头、正文、查询参数透明转发
- 存活与就绪端点（`/health/live`、`/health/ready`）

**端口**：
- Gateway：8000

**详细文档**：`gateway/docs/gateway架构说明.md`

---

### 4. Manager（数据管理）

**职责定位**：数据预览、对象存储管理

**核心能力**：
- 数据源目录树展示（关系型/文档型/图数据库/对象存储/文件系统）
- 多类型数据预览：
  - 表格数据（PostgreSQL、MySQL、Doris、ClickHouse、MongoDB）
  - 空间数据（GeoJSON、Shapefile、PostGIS、快显与瓦片缓存）
  - 文件预览（图片、视频、PDF、Office 文档）
- 对象存储管理（MinIO/S3/OSS 的 Bucket 和对象管理）
- 预览插件系统（TextPreview、ImagePreview、PDFPreview、DocxPreview、PptxPreview）
- 瓦片缓存（PostGIS + MVT 为当前格式实现，任务类型统一为 `vector_tile_cache_generation`）
- 向量化（文本和图像向量化，支持语义相似度检索）
- 全文检索与语义检索（基于 Meilisearch 和向量数据库）

**端口**：
- Backend：8081
- Frontend：5174 (dev)

**详细文档**：`manager/CLAUDE.md`

---

### 5. Meta（元数据服务）

**职责定位**：元数据扫描、索引、搜索和管理

**核心能力**：
- 元数据扫描（统一通过 `CatalogProvider` 和 `CatalogFactsProvider`）：
  - 关系型：schema/database → table/view → field
  - 文档/图数据库：database → collection/graph
  - 对象存储/文件系统：bucket/root → prefix/dir → object/file
- 全文检索索引（Meilisearch，支持中文分词）
- 定时扫描（Cron 表达式配置）
- 事件驱动自动扫描（System 注册引擎触发 Meta 扫描）
- 统一层级元数据模型（resource → node → item）

**端口**：
- Backend：8082
- Frontend：5175 (dev)

**详细文档**：`meta/CLAUDE.md`

---

### 6. Transfer（数据传输）

**职责定位**：数据同步、搬运和格式转换任务管理

**核心能力**：
- 数据同步（source endpoint 到 target endpoint）：PostgreSQL、MySQL、MinIO、S3、CSV、Shapefile
- 格式转换（同一 data type 内 representation / format 转换）：CSV、GeoJSON、Parquet、Shapefile 等
- 全量同步；增量、水位、CDC 等高级能力后续专题设计
- 字段映射和类型转换
- PostgreSQL bounded execution claim（attempt、lease token、heartbeat、过期恢复）
- 定时调度（Cron 表达式配置）

**端口**：
- Backend：8083
- Frontend：5176 (dev)

**详细文档**：`transfer/CLAUDE.md`

---

### 7. Orchestrator（任务编排）

**职责定位**：跨模块 DAG 任务编排、调度和执行

**核心能力**：
- 跨模块任务编排（Meta 扫描 → Transfer 传输 → Manager 预览）
- DAG 拓扑排序，检测循环依赖
- 执行参数来源（任务默认值、Step 固定值、直接依赖步骤声明的稳定输出）
- 定时调度（Cron 表达式配置）
- 任务依赖管理（depends_on）
- 能力注册中心（发现各模块提供的任务）

**端口**：
- Backend：8084
- Frontend：5177 (dev)

**详细文档**：`orchestrator/CLAUDE.md`

---

### 8. Develop（数据开发）

**职责定位**：查询执行、工作流开发、Notebook 交互式开发

**核心能力**：
- **查询开发**：SQL/MQL 编辑、执行、结果展示（支持 PostgreSQL、MySQL、Doris、ClickHouse、MongoDB）
- **算子工作流**：基于算子的可视化 DAG 工作流（空间和非空间算子，细粒度计算节点）
- **Notebook 开发**：基于 Jupyter 的交互式 Notebook 环境（Python、Shell）
- 代码编辑器（语法高亮、自动补全、格式化）
- 执行历史记录和结果回溯

**端口**：
- Backend：8185
- Frontend：5178 (dev)

**详细文档**：`develop/CLAUDE.md`

---

---

### 9. Copilot（AI 助手）

**职责定位**：基于大语言模型（LLM）的 AI 辅助开发模块

**核心能力**：
- **自然语言转 SQL**：用户用自然语言描述查询需求，AI 自动匹配元数据生成 SQL
- **自然语言转工作流**：用户描述处理需求，AI 生成 GIS 工作流 DAG 定义
- **多轮对话记忆**：支持上下文关联的多轮对话
- **多 LLM 支持**：支持 OpenAI、Claude、Ollama、DashScope 等，租户可自定义模型和 API Key
- **元数据感知**：调用 Meta 模块智能匹配候选数据源，提升生成准确率
- **多租户隔离**：对话历史和 LLM 配置按租户完全隔离

**端口**：
- Backend：8087

**详细文档**：`copilot/CLAUDE.md`

---

### 10. Service（数据服务）

**职责定位**：内部数据发布、外部服务代理接入、矢量瓦片地图服务

**核心能力**（三大服务系统）：
- **查询服务**：将内部数据库表或 SQL 查询发布为标准服务（REST Query API、OGC API Features 1.0、WFS 2.0）
- **瓦片服务**：发布高性能矢量瓦片地图（XYZ Tiles、WMTS 1.0、OGC Tiles API），支持动态生成和静态预存
- **注册服务**：代理管理外部第三方 OGC 服务（支持 WMS/WFS/WMTS/OGC API/XYZ/REST），含健康检查和元数据自动刷新
- **服务目录**：聚合三大服务系统的统一发现入口，支持按协议类型筛选
- **权限控制**：管理端 User Access Token + AuthContext + 租户隔离，数据端可配置公开或需认证访问

**端口**：
- Backend：8086
- Frontend：5180 (dev)

**详细文档**：`service/CLAUDE.md`

---

### 11. Standard（数据标准管理）

**职责定位**：企业级数据标准定义和管理，提供数据元、业务术语、码值集等基础规范

**核心能力**：
- **数据域管理**：业务领域分类，组织数据标准的归属
- **业务术语**：定义业务概念和术语表（Glossary），支持术语与数据元映射
- **数据元管理**：定义数据的最小业务单元，包含数据类型、长度、精度、质量规则、默认值、取值范围等
- **码值集管理**：枚举值集合定义（CodeSet 和 CodeItem），数据元可关联码值集
- **质量规则**：数据元级别的质量校验规则定义（正则表达式、范围校验等）

**端口**：
- Backend：8110
- Frontend：5181 (dev)

**详细文档**：`standard/CLAUDE.md`（待创建）

---

### 12. Model（数据模型管理）

**职责定位**：数据建模和数据仓库分层管理，提供概念模型、逻辑模型、物理模型的统一管理

**核心能力**：
- **数仓分层管理**（DWLayer）：定义 ODS/DWD/DWS/ADS 数据仓库分层，配置命名规范和质量 SLA
- **业务实体建模**（Entity）：概念层实体定义、实体属性管理、实体关系建模（一对一/一对多/多对多）
- **逻辑表建模**（LogicalTable）：支持多种表类型（实体表/事实表/维度表/ODS/DWD/DWS/ADS）
- **字段定义**：逻辑表字段管理，可引用 Standard 模块的数据元，确保字段规范一致性
- **表关系管理**：逻辑表间的外键关系和关联关系定义
- **物化配置**：逻辑表的物理实现配置（目标引擎、分区策略等）
- **标准集成**：实体属性和逻辑字段可引用 Standard 模块的数据元（element_id），实现数据规范的统一管理

**端口**：
- Backend：8181
- Frontend：5182 (dev)

**详细文档**：`model/CLAUDE.md`（待创建）

---

### 13. Quality（数据质量）

**职责定位**：数据质量检查、评分与问题管理

**核心能力**：
- **规则应用管理**：将 Standard 模块数据元的质量规则映射到具体数据库表字段，支持字段级规则绑定
- **检查任务管理**：定义质量检查任务，可指定引擎、Schema、表的检查范围，支持手动触发执行
- **SQL 规则引擎**：自动将质量规则转换为 SQL 查询，支持 6 种基础规则类型（非空、唯一性、格式正则、长度范围、数值范围、枚举值）
- **质量评分**：异步执行检查，计算表级综合质量评分和字段级评分明细
- **问题工单**：检查失败自动生成问题工单，支持状态流转（待处理 → 已解决/已忽略）
- **执行监控**：执行记录写入 `common.task_executions`，与 Monitor 模块统一监控

**端口**：
- Backend：8182
- Frontend：5183 (dev)

**详细文档**：`quality/CLAUDE.md`

---

### 14. Monitor（执行监控）

**职责定位**：全平台任务执行监控、统计分析、健康检查

**核心能力**：
- 统一监控所有模块的任务执行记录（使用 `common.task_executions` 表）
- 统计分析（任务成功率、平均执行时长、失败原因）
- 健康检查（服务存活状态、资源占用）
- 执行日志查看和搜索
- 任务重试和取消管理

**端口**：
- Backend：8100
- Frontend：5179 (dev)

**详细文档**：`monitor/docs/Monitor模块实施报告.md`

---

## 扩展运行时

### GeoPython Workflow 运行时

**职责定位**：基于 Python 的单节点工作流运行时

**核心能力**：
- 提供 21 个空间算子（buffer、centroid、intersection、union 等）
- DAG 内存计算（GeoDataFrame 全程内存传递，避免中间序列化）
- 支持空间和非空间数据处理
- 适用场景：中小规模数据（< 100 万行）
- 执行引擎：GeoPython Workflow（单节点内存计算，底层使用 GeoPandas 等库）

**端口**：8099

**详细文档**：`engines/geopython-workflow/README.md`

---

### Spark Workflow 运行时

**职责定位**：基于 Spark 的分布式工作流运行时；执行时必须绑定实际 Spark 通用引擎资源

**核心能力**：
- 大规模分布式空间数据处理
- 支持空间和非空间算子
- 适用场景：大规模数据（> 100 万行）
- 执行引擎：Apache Spark（分布式计算）
- 自动资源调度和任务并行

**端口**：8091

**详细文档**：`engines/spark-workflow/README.md`

---

### Jupyter Engine

**职责定位**：交互式 Notebook 执行引擎

**核心能力**：
- Jupyter Notebook 交互式开发环境
- 支持 Python 和 Shell 代码执行
- 变量传递和工作流集成
- 结果输出（文本、图表、GeoDataFrame）
- 代码单元格管理和执行状态跟踪

**端口**：8888

**详细文档**：`engines/jupyter/README.md`

---

## 共享模块

### common（后端共享库）

**职责定位**：后端模块间共享代码，避免重复

**核心能力**：
- **SystemClient**：与 System 模块通信（ListEngines、GetEngine）
- **Resource 模型**：共享数据模型（用户、引擎、任务等）
- **Engine connection_info**：共享引擎连接信息模型；DSN 仅由需要底层 driver 的数据库类插件按需构建
- **部署配置加载器**：读取进程环境和根 `.env` 中的部署配置；不得从 System 获取 owner 普通运行配置或建立 fallback（`common/config/loader.go`）
- **工具函数**：AuthContext、加密、日志、类型转换等；业务模块不解析用户 Token

**模块路径**：`common/`

**使用模式**：
```go
// go.mod 中引用
require (github.com/addp/common v0.0.0)
replace github.com/addp/common => ../../common

// 导入使用
import commonClient "github.com/addp/common/client"
import commonModels "github.com/addp/common/models"
```

**详细文档**：`docs/concepts/addp共享模块介绍.md`

---

### common-frontend（前端共享库）

**职责定位**：前端模块间共享组件、工具和类型定义

**核心能力**：
- **basic 子模块**（无地图依赖）：
  - StorageEngineForm（数据源配置表单）
  - formatters（数据格式化工具：formatFileSize、formatDateTime）
  - 类型定义（FieldType、FormatType、ResourceType）
- **previews 入口**（按需预览组件）：
  - ImagePreview（图片预览）
  - MarkdownPreview / PdfPreview / DocxPreview 等文件预览组件
- **map 子模块**（地图相关，依赖 OpenLayers 和高德地图）：
  - GeoJsonPreview（GeoJSON 预览）
  - TablePreview（表格数据预览，支持 Shapefile 等空间表）
  - MapContainer（地图容器，支持 OpenLayers 和高德地图）

**模块路径**：`common-frontend/`

**使用模式**：
```javascript
// vite.config.js 中配置别名
resolve: {
  alias: {
    '@common-ui': resolve(__dirname, '../../common-frontend/basic/src'),
    '@common-ui-map': resolve(__dirname, '../../common-frontend/map/src')
  }
}

// 导入使用
import { StorageEngineForm } from '@common-ui'
import { ImagePreview } from '@common-ui/previews'
import { TablePreview, GeoJsonPreview } from '@common-ui-map'
```

**详细文档**：`common-frontend/README.md`、`common-frontend/docs/ARCHITECTURE.md`

---

## 相关文档

- [ADDP 核心概念关系图](addp核心概念关系图.md) - 概念关系 Mermaid 图
- [ADDP 模块架构图](addp模块架构图.md) - 模块架构 Mermaid 图
- [ADDP 开发原则](../spec/addp开发原则.md) - 开发指导原则
- [ADDP 部署和开发步骤](../guide/addp部署和开发步骤.md) - 快速启动指南
- [ADDP 配置规范](../spec/addp配置介绍.md) - 配置分层、管理能力和环境变量
- [ADDP 端口分配](../spec/addp端口分配.md) - 完整端口列表
- [ADDP 技术栈规约](../spec/addp技术栈规约.md) - Go 和前端依赖版本规范
- [ADDP 共享模块介绍](../concepts/addp共享模块介绍.md) - common 和 common-frontend 详细说明

**各模块详细文档**：
- System: `system/CLAUDE.md`
- Gateway: `gateway/docs/gateway架构说明.md`
- Manager: `manager/CLAUDE.md`
- Meta: `meta/CLAUDE.md`
- Transfer: `transfer/CLAUDE.md`
- Orchestrator: `orchestrator/CLAUDE.md`
- Develop: `develop/CLAUDE.md`
- Service: `service/CLAUDE.md`
- Standard: `standard/CLAUDE.md`
- Model: `model/CLAUDE.md`
- Quality: `quality/CLAUDE.md`
- Monitor: `monitor/docs/Monitor模块实施报告.md`

---

## 文档版本

- **版本**: v2.2
- **更新日期**: 2026-02-22
- **更新内容**: 新增 Quality（数据质量）模块介绍，Monitor 模块编号调整为 14
