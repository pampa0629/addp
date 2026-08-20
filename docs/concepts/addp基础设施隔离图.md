# ADDP 基础设施隔离图

本文档展示 ADDP 平台的基础设施架构,包括系统基础设施与业务数据库的隔离,以及各类资源的隔离机制。

---

## 目录

1. [基础设施架构概述](#基础设施架构概述)
2. [系统基础设施 vs 业务数据库](#系统基础设施-vs-业务数据库)
3. [资源隔离机制](#资源隔离机制)

---

## 基础设施架构概述

ADDP 采用**系统与业务数据分离**架构设计,确保平台元数据与用户业务数据隔离。

```mermaid
graph TB
    subgraph "系统基础设施 (ADDP 元数据)"
        InfraTitle[docker-compose.infra.yml<br/>项目名: addp-infra]

        PostgreSQL[(PostgreSQL<br/>系统数据库<br/>:15432)]
        Redis[(Redis<br/>缓存/事件/锁<br/>:16379)]
        MinIO[(MinIO<br/>系统文件存储<br/>:19000-19001)]
        Meilisearch[(Meilisearch<br/>全文搜索<br/>:17700)]
    end

    subgraph "业务数据库 (用户业务数据)"
        BizTitle[business/docker-compose.yml<br/>独立部署]

        BizPG[(business-postgres<br/>业务数据库<br/>:5433)]
        BizMinIO[(business-minio<br/>业务对象存储<br/>:9002-9003)]
        BizNeo4j[(business-neo4j<br/>图数据库<br/>:7474/:7687)]
        BizOther[(business-other<br/>业务xx引擎<br/>:9002-9003)]
    end

    subgraph "ADDP 模块"
        Modules[System, Manager, Meta<br/>Transfer, Orchestrator<br/>Develop, Service]
    end

    Modules --> PostgreSQL
    Modules --> Redis
    Modules --> MinIO
    Modules --> Meilisearch

    Modules -.通过引擎访问.-> BizPG
    Modules -.通过引擎访问.-> BizMinIO
    Modules -.通过引擎访问.-> BizNeo4j
    Modules -.通过引擎访问.-> BizOther

    classDef infra fill:#fce4ec,stroke:#880e4f
    classDef biz fill:#e8f5e9,stroke:#1b5e20
    classDef module fill:#e1f5ff,stroke:#01579b

    class InfraTitle,PostgreSQL,Redis,MinIO,Meilisearch infra
    class BizTitle,BizPG,BizMinIO,BizNeo4j,BizOther biz
    class Modules module
```

---

## 系统基础设施 vs 业务数据库

### 系统基础设施 (docker-compose.infra.yml)

**存储 ADDP 系统元数据**,包括:

**PostgreSQL** (端口 15432):
- 用户信息 (`system.users`)
- 引擎配置 (`system.engines`)
- 元数据存储 (`meta.meta_node`, `meta.meta_item`)
- 任务定义 (`orchestrator.orchestrations`)
- 服务配置 (`service.services`)
- Schema 隔离: `system`, `manager`, `meta`, `transfer`, `orchestrator`, `develop`, `service`

**Redis** (端口 16379):
- 用户会话缓存
- 引擎配置缓存
- 元数据缓存
- 事件发布、缓存和扫描范围锁；不承担 bounded execution 队列
- Key 命名规范: `{module}:{middleware}:{function}:{id}`

**MinIO** (端口 19000-19001):
- 用户头像、审计日志归档 (`system` bucket)
- 预览缓存、导入暂存、导出中转产物 (`manager` bucket)
- 瓦片缓存对象 (`manager` bucket,通过 Manager API 和 `storage_ref` 访问)
- 临时文件
- Bucket 隔离: `system`, `manager`, `meta`, `transfer`, `orchestrator`, `develop`, `service`

**Meilisearch** (端口 17700):
- 统一资产搜索索引: `assets` (开发环境) / `metadata-assets` (生产环境)
- Meta 和 Manager 模块共享此索引,通过 `asset_type` 区分数据类型 (table/object)

### 业务数据库 (business/docker-compose.yml, 独立部署)

**存储用户通过 ADDP 管理的实际业务数据**:

**business-postgres** (端口 5433):
- 用户上传的 PostgreSQL 数据
- 由用户手动注册为引擎
- ADDP 通过引擎插件访问

**business-minio** (端口 9002-9003):
- 用户上传的业务文件 (Shapefile、GeoJSON、图片、视频)
- 由用户手动注册为引擎
- ADDP 通过引擎插件访问

**business-neo4j** (端口 7474/7687):
- 图结构业务数据（知识图谱、关系网络）
- Neo4j Community Edition，支持 Cypher 查询语言
- Browser UI：http://localhost:7474，Bolt 协议：bolt://localhost:7687
- 由用户手动注册为引擎
- ADDP 通过引擎插件访问

---

## 资源隔离机制

ADDP 采用**模块化资源隔离**策略,确保模块资源独立管理:

```mermaid
graph TB
    Isolation[资源隔离机制]

    Isolation --> PG[PostgreSQL Schema 隔离]
    Isolation --> MinIOIso[MinIO Bucket 隔离]
    Isolation --> RedisIso[Redis Key 命名规范]
    Isolation --> ExecutionIso[PostgreSQL Execution Claim 隔离]
    Isolation --> MeiliIso[Meilisearch Index 命名规范]

    PG --> PGEx["system schema: 用户/引擎/日志<br/>manager schema: 数据源/预览配置<br/>meta schema: 元数据索引<br/>transfer schema: 传输任务<br/>orchestrator schema: 编排定义<br/>develop schema: 查询/工作流/Notebook<br/>service schema: 服务配置"]

    MinIOIso --> MinIOEx["system bucket: 用户头像/系统文件/审计日志归档<br/>manager bucket: 预览缓存/瓦片缓存对象/导入暂存/导出中转<br/>meta bucket: 扫描临时文件<br/>transfer bucket: 内部执行临时文件<br/>orchestrator bucket: 编排执行日志<br/>develop bucket: 查询结果/工作流输出<br/>service bucket: 服务缓存"]

    RedisIso --> RedisEx["system:cache:user:123<br/>system:session:abc<br/>manager:cache:preview:456<br/>meta:scan_dedup:..."]

    ExecutionIso --> ExecutionEx["common.task_executions<br/>module + task_type + bounded + pending<br/>attempt + lease_token + lease_expires_at"]

    MeiliIso --> MeiliEx["assets 统一索引<br/>包含 asset_type=table (表/集合)<br/>和 asset_type=object (文件/对象)"]

    classDef isolation fill:#fff9c4,stroke:#f57f17
    classDef mechanism fill:#e1f5ff,stroke:#01579b
    classDef example fill:#e8f5e9,stroke:#1b5e20

    class Isolation isolation
    class PG,MinIOIso,RedisIso,ExecutionIso,MeiliIso mechanism
    class PGEx,MinIOEx,RedisEx,ExecutionEx,MeiliEx example
```

### 隔离机制详情

**1. PostgreSQL Schema 隔离**:
- 按模块隔离: `system`、`manager`、`meta`、`transfer`、`orchestrator`、`develop`、`service`
- 避免表名冲突,权限独立管理
- 租户隔离: 所有业务表包含 `tenant_id` 字段

**2. MinIO Bucket 隔离**:
- 按模块隔离: `system`、`manager`、`meta`、`transfer`、`orchestrator`、`develop`、`service`
- 避免文件冲突,配额独立管理
- `manager` bucket 保存预览缓存、瓦片缓存对象、Manager 导入暂存和导出中转产物，前端通过 Manager API 访问，不直接依赖公开 bucket 路径
- 其他 bucket 均为私有访问

### ADDP infra locator

`addp-infra://` 是 ADDP 后端内部使用的基础设施资源定位符，用于 Manager、Transfer 等模块在服务间契约中定位系统基础设施资源。它不是业务 ResourceLocator，不进入用户资源树，不进入 System engines，也不写入 Meta item。

通用形式：

```text
addp-infra://{infra_kind}/{namespace}/{path...}?type={resource_type}
```

| 组成 | 说明 |
|---|---|
| `infra_kind` | 基础设施类型，例如 `minio`、`postgres`、`redis`、`meilisearch`。 |
| `namespace` | infra 内部命名空间。对 MinIO 是 bucket；对 PostgreSQL 可是 schema；对 Redis 可是 key namespace；对 Meilisearch 可是 index。 |
| `path` | namespace 内部路径或对象名。 |
| `type` | infra resource type，例如 `object`、`prefix`、`table`、`key`、`index`。 |

第一阶段执行链路只支持 `addp-infra://minio/...` 的 `object` / `prefix`，用于 Manager import source 和 Manager export target：

```text
addp-infra://minio/manager/tenant_7/import/20260622/upload-uuid/roads.shp?type=object
addp-infra://minio/manager/tenant_7/export/20260622/execution-id?type=prefix
```

约束：

1. infra MinIO 使用 `.env` 中系统 MinIO 配置，即 19000-19001 的 ADDP infra MinIO。
2. infra MinIO 不在 System engines 中登记，也不得被用户选择为业务数据引擎。
3. infra 暂存对象不写入 Meta node / item，不参与资源树展示或业务库扫描。
4. Manager 拥有自身 import / export 暂存对象的生命周期和 cleanup；Transfer 只按 locator 读取或写入，不承担 Manager 暂存清理职责。
5. `addp-infra://` 的设计不能假设 infra 只有 MinIO；新增 infra kind 前必须先补规范，再接入对应模块。

**3. Redis Key 命名规范**:
- 格式: `{module}:{middleware}:{function}:{id}`
- 示例: `system:cache:user:123`、`meta:scan_dedup:...`
- 便于按模块管理和清理

**4. PostgreSQL bounded execution claim 隔离**:
- Worker 固定按 `module + task_type + execution_boundary=bounded + status=pending` 领取。
- 运行所有权由 `attempt + lease_token + lease_expires_at` 标识，不使用 Redis key 表达 execution owner。
- owner scheduler 只创建 durable pending execution；Worker 不可用时 execution 保留排队事实。

**5. Meilisearch Index 命名**:
- **当前实现**: 使用统一索引 `assets` (开发环境) / `metadata-assets` (生产环境)
- Meta 和 Manager 模块共享此索引,通过 `asset_type` 字段区分数据类型
- **规范建议** (未实现): 格式 `{module}:{entity_type}`,如 `meta:tables`、`manager:datasources`

---

## 相关文档

- [返回核心概念关系图](addp核心概念关系图.md)
- [ADDP 配置介绍](../spec/addp配置介绍.md)
- [ADDP 端口分配](../spec/addp端口分配.md)

---

**文档版本**: v1.0
**创建日期**: 2026-02-16
**作者**: ADDP 开发团队
