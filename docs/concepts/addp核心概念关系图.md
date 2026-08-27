# ADDP 核心概念关系图

本文档是 ADDP 核心概念的总览索引页,使用可视化图表展示核心概念全景,并提供详细文档的导航链接。

---

## 目录

1. [核心概念全景图](#核心概念全景图)
2. [核心概念导航](#核心概念导航)
3. [文档索引](#文档索引)

---

## 核心概念全景图

ADDP 平台的核心概念体系:

```mermaid
mindmap
  root((ADDP 核心概念))
    模块架构
      console:控制台
      gateway:网关路由
      system:系统配置管理
      核心数据流转模块
        transfer:数据传输
        meta:元数据
        catalog:企业数据目录
        manager:数据管理
        develop:数据开发
        service:数据服务
        orchestrator:任务编排
      monitor:任务监控
      copilot:AI助手
      共享模块
        common 后端
        common-frontend 前端
      扩展运行时
        内置与专用运行时 geopython_workflow / spark_workflow / model3d_workflow / pointcloud_workflow / supermap_workflow / jupyter
        用户自研扩展运行时
    引擎体系
      Provider 化引擎插件接口
        EnginePlugin 基础
        EngineCatalogProvider
        EngineCatalogFactsProvider
        ChangeStreamReaderProvider
        QueryRuntimeProvider
      引擎分类
        按功能: 存储/计算/兼备
        按类型: 标准/扩展
        按来源: 注册/内置
      engine.capabilities/v1
    账号与权限
      全局 User
        Local Account
        External Identity
        Tenant Membership
      组织
        Department 层级组织
        Project Group 跨部门协作
      授权
        Role 与 Permission
        Resource Grant 与 Policy
        显式 Deny
      平台治理
        平台三员分立
        聚合统计独立授权
    元数据体系
      数据层次结构
        数据节点 Node
        数据项 Item
      Meta Detector
      Attributes 分区
      FormatPlugin 编排
      元数据扫描流程
      数据类型与格式
        数据类型
      table/document/media/container/graph/model_3d/point_cloud/gaussian_splat/unknown
      文件格式
        csv/json/parquet/shapefile/sqlite/geopackage/pdf/image
      capability 分层
        engine capability
        format descriptor/provider status
        item capabilities
      provider / reader 体系
        TableInfoProvider
        TableSampleReader
        DocumentInfoProvider
        DocumentTextReader
        MediaInfoProvider
        ContainerChildResolver
        EngineCatalogFactsProvider
        GraphSampleProvider
        Spatial 横切能力
    企业数据目录
      CatalogEntry 企业稳定身份
      SourceBinding 专业来源绑定
      CatalogComponent 字段或组件
      业务语义关联与责任
      目录可见性与企业元数据搜索
      Meta → Catalog → Asset → Portal
    数据开发
      开发方式（capabilities.compute）
        Query → 查询工作台
        Workflow → 工作流编辑器
        Notebook 开发 → Notebook编辑器
    任务编排
      TaskProvider 模块角色声明与动态发现
      任务编排流 vs 算子工作流
      DAG 依赖管理
      定时调度
    监控与执行
      统一执行监控：Monitor 模块
      任务状态流转
      跨模块监控集成：common
      数据服务
      查询服务
      瓦片服务
      服务注册
    数据血缘
      lineage facts
      关系证据与当前投影
      published service 依赖
      Meta graph API
      common-frontend LineageViewer
    系统基础设施infra
      PostgreSQL
      Redis
      MinIO
      Meilisearch
      资源隔离机制
        Schema/Bucket/Key/Queue/Index
    业务数据库
      business-postgres
      business-minio
    其他
      Browser AuthSession 与 AuthContext
      gateway 路由机制
      运行模式
        控制台:console
        独立模块
      后台执行角色
        独立 Worker 进程
          Transfer Bounded Worker
          Transfer Continuous Worker
          Meta Scan Worker
        Backend 内嵌 Execution Worker
          Quality Check Worker
        非 Execution Worker
          Owner Scheduler
          Dispatcher
          Maintenance Loop
```

---

## 核心概念导航

### 1. [模块架构](addp模块架构图.md)

**ADDP 各个模块及其依赖关系**

- 模块总览图 (Console、Gateway、System、Manager、Meta、Transfer、Orchestrator、Develop、Service、Monitor)
- 模块分层架构 (前端层、网关层、服务层、数据层)
- 共享模块 (common、common-frontend)
- 扩展运行时 (内置与专用运行时 geopython_workflow、spark_workflow、model3d_workflow、pointcloud_workflow、supermap_workflow、jupyter，以及用户自研扩展运行时)
- 基础设施 (PostgreSQL、Redis、MinIO、Meilisearch)

📄 **[阅读完整文档 →](addp模块架构图.md)**

---

### 2. [引擎体系](addp引擎体系图.md)

**引擎系统架构、分类体系和能力声明机制**

- Provider 化引擎插件架构 (EnginePlugin、EngineCatalogProvider、EngineCatalogFactsProvider、StoreProvider、QueryRuntimeProvider)
- 引擎分类体系 (按功能、按来源、按注册方式)
- 当前支持的引擎列表
- 结构化能力声明 (`engine.capabilities/v1`)
- 引擎插件系统

📄 **[阅读完整文档 →](addp引擎体系图.md)**

---

### 3. [账号与权限](addp账号与权限体系图.md)

**统一身份、Tenant 隔离、组织关系和权限控制模型**

- 全局 User 与 Tenant Membership
- Local Account、External Identity 和 Service Principal
- Department、Project Group 和 Tenant 隔离
- Role / Permission 与 owner Resource Grant / Policy
- 平台三员分立和跨租户聚合统计授权
- opaque User Access Token 与 AuthContext

📄 **[阅读完整文档 →](addp账号与权限体系图.md)**

---

### 4. [元数据体系](addp元数据体系图.md)

**元数据管理体系，包括资源层次、数据项识别、attributes normalizer 和扫描流程**

- 元数据层次结构 (engine、node、data item)
- Meta Detector 识别 data item 边界
- Meta Normalizer 生成标准 attributes
- FormatPlugin、info provider、content reader 的编排边界
- 元数据扫描流程

📄 **[阅读完整文档 →](addp元数据体系图.md)**

---

### 5. [数据类型和格式](addp数据类型和格式体系图.md)

**数据类型、文件格式、FormatPlugin 和 provider / reader 体系**

- 数据类型分类 (table、document、media、container、graph、unknown)
- 文件格式体系 (csv、json、parquet、shapefile、sqlite/geopackage、pdf、图片等)
- capability 分层 (engine capability、format descriptor / provider status、item capabilities)
- provider / reader 矩阵与跨模块边界 (TableInfoProvider、TableSampleReader、DocumentInfoProvider、DocumentTextReader、MediaInfoProvider 等)

📄 **[阅读完整文档 →](addp数据类型和格式体系图.md)**

---

### 6. [数据开发](addp数据开发体系图.md)

**三种数据开发方式及其与引擎能力的关系**

- 三种开发方式概述 (查询开发、算子工作流、Notebook 开发)
- 查询开发 (Query) - SQL/MQL 查询
- 算子工作流 (Workflow) - 可视化 DAG 工作流
- Notebook 开发 - Jupyter Notebook 交互式开发
- capabilities.compute 与开发界面映射

📄 **[阅读完整文档 →](addp数据开发体系图.md)**

---

### 7. [任务编排](addp任务编排体系图.md)

**任务库机制、任务编排流与算子工作流的区别、以及跨模块编排能力**

- 任务库机制（模块 TaskProvider 角色声明、跨模块任务调用）
- 任务编排流 vs 算子工作流 (粗粒度 vs 细粒度、跨模块 vs 单引擎)
- 编排 DAG 示例
- 依赖管理与声明输出绑定
- 调度方式 (Cron 定时调度、手动触发)

📄 **[阅读完整文档 →](addp任务编排体系图.md)**

---

### 8. [监控与执行](addp监控与执行体系图.md)

**统一执行监控架构和跨模块任务追踪机制**

- 统一执行监控架构 (common.task_executions 表、Monitor 模块)
- 任务执行状态流转 (pending → running → success/failed)
- 跨模块监控集成
- 各模块任务类型

📄 **[阅读完整文档 →](addp监控与执行体系图.md)**

---

### 9. [数据服务](addp数据服务体系图.md)

**数据服务发布机制,包括 OGC 标准服务和查询服务 API**

- 数据服务概述
- 服务发布流程
- 查询服务 API (RESTful 接口、条件查询、分页、空间查询)
- 瓦片服务
- 服务注册

📄 **[阅读完整文档 →](addp数据服务体系图.md)**

---

### 10. [数据血缘](../spec/addp数据血缘能力规范.md)

**数据项来源、派生、服务依赖和执行证据的统一关系视图**

- 真实读写 owner 写入版本化 `lineage_facts`
- Meta 保存关系证据和当前投影，PostgreSQL 关系表是第一阶段唯一事实源
- 图查询统一使用 Meta lineage graph API
- `common-frontend/graph` 提供可嵌入的 `LineageViewer`
- 字段级血缘和 SQL 自动解析属于后续阶段

📄 **[阅读完整规范 →](../spec/addp数据血缘能力规范.md)**

---

### 11. [基础设施隔离](addp基础设施隔离图.md)

**基础设施架构,包括系统基础设施与业务数据库的隔离**

- 基础设施架构概述
- 系统基础设施 vs 业务数据库 (ADDP 元数据 vs 用户业务数据)
- 资源隔离机制 (PostgreSQL Schema、MinIO Bucket、Redis Key、bounded execution claim、Meilisearch Index)

📄 **[阅读完整文档 →](addp基础设施隔离图.md)**

---

### 12. [企业数据目录](addp企业数据目录体系图.md)

**企业资源稳定身份、业务语义关联、责任和权限感知发现**

- CatalogEntry 与 Meta DataItem fingerprint 的身份分离
- Meta、Standard、Catalog、Manager、Asset、Portal 的事实边界
- DataItem 自动建档、来源失效和显式重绑
- Department / Project Group / User 与目录身份的关系
- 技术资源、企业元数据、内容和资产搜索的所有权

📄 **[阅读完整文档 →](addp企业数据目录体系图.md)**

---

### 13. [认证与路由](addp登录认证的原理说明.md)

**Browser AuthSession、登录恢复、静默刷新和 Console iframe 认证机制**

- opaque Token 与 AuthContext 流程
- 页面启动恢复、主动刷新和多标签页互斥
- 独立模块与 Console 统一登录
- Console iframe `postMessage` 认证机制
- 认证中间件原理（Go 代码）
- Token 自动刷新机制（JavaScript 代码）
- 安全特性（bcrypt、Token Hash、轮换撤销、多租户隔离）

> Gateway 路由规则和 Console 架构图见：[ADDP 模块架构图](addp模块架构图.md)

📄 **[阅读完整文档 →](addp登录认证的原理说明.md)**

---

## 文档索引

### 架构与模块

- **[ADDP 模块架构图](addp模块架构图.md)** - 模块总览、分层架构、共享模块、扩展运行时
- **[引擎体系图](addp引擎体系图.md)** - 引擎插件架构、分类体系、能力声明
- **[基础设施隔离图](addp基础设施隔离图.md)** - 系统与业务分离、资源隔离机制

### 用户与权限

- **[账号与权限体系](addp账号与权限体系图.md)** - User、Tenant Membership、组织、角色、资源授权和 AuthContext
- **[登录认证原理说明](addp登录认证的原理说明.md)** - Browser AuthSession、静默恢复、Token 轮换和 iframe 认证（Gateway 路由与 Console 架构见[模块架构图](addp模块架构图.md)）

### 数据管理

- **[元数据体系图](addp元数据体系图.md)** - 元数据层次、扫描流程、attributes 结构
- **[数据项体系图](addp数据项体系图.md)** - engine、node、data item 链条和模块职责边界
- **[数据类型和格式体系图](addp数据类型和格式体系图.md)** - 数据类型、文件格式、能力分层、provider / reader 体系
- **[企业数据目录体系图](addp企业数据目录体系图.md)** - CatalogEntry、来源绑定、业务语义、责任和企业目录搜索边界

### 数据开发与编排

- **[数据开发体系图](addp数据开发体系图.md)** - 查询开发、算子工作流、Notebook 开发
- **[任务编排体系图](addp任务编排体系图.md)** - 任务库、编排流、DAG 依赖、调度方式

### 监控与服务

- **[监控与执行体系图](addp监控与执行体系图.md)** - 统一执行监控、状态流转、跨模块监控
- **[数据服务体系图](addp数据服务体系图.md)** - OGC 标准服务、查询服务 API

### 治理与血缘

- **[数据血缘能力规范](../spec/addp数据血缘能力规范.md)** - 统一 execution facts、Meta 关系投影、服务依赖和查看器边界
- **[数据项体系图](addp数据项体系图.md)** - data item 身份、字段边界和血缘职责归属

---

## 核心概念关系总结

ADDP 平台的核心概念之间存在紧密的关联:

```mermaid
graph TB
    引擎[引擎体系] --> 元数据[元数据体系]
    引擎 --> 开发[数据开发]
    引擎 --> 服务[数据服务]

    元数据 --> 类型[数据类型与格式]
    元数据 --> 开发
    元数据 --> 目录[企业数据目录]
    标准[数据标准] --> 目录
    账号[账号与权限] --> 目录
    目录 --> 资产[数据资产]
    资产 --> 门户[资产门户]

    开发 --> 编排[任务编排]
    编排 --> 监控[统一监控]
    开发 --> 血缘[数据血缘]
    监控 --> 血缘
    服务 --> 血缘
    元数据 --> 血缘

    账号 --> 隔离[基础设施隔离]
    账号 --> 认证[认证与路由]

    模块[模块架构] --> 引擎
    模块 --> 账号
    模块 --> 元数据
    模块 --> 开发
    模块 --> 编排
    模块 --> 监控
    模块 --> 服务

    classDef architecture fill:#fff9c4,stroke:#f57f17
    classDef engine fill:#e1f5ff,stroke:#01579b
    classDef data fill:#e8f5e9,stroke:#1b5e20
    classDef auth fill:#fce4ec,stroke:#880e4f
    classDef dev fill:#f3e5f5,stroke:#4a148c

    class 模块 architecture
    class 引擎,元数据,类型,服务 engine
    class 开发,编排,监控 data
    class 账号,隔离,认证 auth
```

**关键关联**:
1. **模块架构**是基础,定义了 ADDP 的整体结构
2. **引擎体系**驱动**元数据**、**数据开发**和**数据服务**
3. **数据开发**的结果可以被**任务编排**使用,形成复杂的数据流水线
4. **所有任务**的执行被**统一监控**
5. **账号与权限**确保**基础设施隔离**和**认证与路由**的安全性

---

## 相关文档

- [ADDP 各模块简要介绍](addp各模块功能介绍.md) - 各模块功能概述
- [ADDP 开发原则](../spec/addp开发原则.md) - 开发指导原则
- [ADDP 配置介绍](../spec/addp配置介绍.md) - 环境变量和配置管理
- [ADDP 技术栈规约](../spec/addp技术栈规约.md) - 依赖版本规范
- [ADDP 共享模块介绍](addp共享模块介绍.md) - common 和 common-frontend 详解

---

**文档版本**: v1.0
**创建日期**: 2026-02-16
**作者**: ADDP 开发团队
