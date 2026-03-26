# dev_items 表结构和 API 说明

## 字段变更历史

### 2026-02-11 表结构优化

本次优化旨在消除冗余字段，优化数据结构，提升代码一致性：

| 变更类型 | 字段 | 说明 |
|---------|------|------|
| ❌ **已删除** | `is_scheduled` | 可通过 `schedule IS NOT NULL AND schedule != ''` 推导，已从数据库表中删除 |
| 🔄 **已迁移** | `query_type` | 迁移到 `content.query_type`，顶层字段保留用于向后兼容 |
| 🔄 **已迁移** | `engine_id` | 迁移到 `execution_config.engine_id`，顶层字段保留用于向后兼容 |

**注意**：
- 代码已更新为优先读取新位置（`content.query_type`、`execution_config.engine_id`）
- 旧字段保留用于向后兼容，新创建的记录会同时写入新旧两个位置
- 判断是否启用调度：使用 `schedule IS NOT NULL AND schedule != ''`

---

## 一、表结构概览

`develop.dev_items` 表是 Develop 模块的开发项定义表，统一存储 SQL 查询、工作流、Notebook 等开发项。支持多种开发类型，实现灵活的内容存储和执行配置。

### 核心功能

- **统一开发项管理**：支持 query（SQL/MQL）、workflow（工作流）、notebook 等类型
- **灵活内容存储**：使用 JSONB 存储不同类型的开发项内容
- **执行配置**：支持定时调度、超时配置、引擎绑定
- **状态追踪**：记录最后执行状态和时间
- **软删除**：支持逻辑删除，保留历史记录

---

## 二、表结构定义

### 2.1 核心字段

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | SERIAL | PRIMARY KEY | 开发项唯一标识 |
| `tenant_id` | INTEGER | NOT NULL, INDEXED | 租户 ID |
| `name` | VARCHAR(255) | NOT NULL | 开发项名称（唯一标识符） |
| `display_name` | VARCHAR(255) | | 显示名称（前端优先显示） |
| `dev_type` | VARCHAR(50) | NOT NULL, INDEXED | 开发类型：'query'、'workflow'、'notebook' |
| `query_type` | VARCHAR(50) | INDEXED | ⚠️ **已废弃**（保留兼容）：查询类型，已迁移到 `content.query_type` |
| `content` | JSONB | NOT NULL | 开发项内容（SQL、工作流定义等），推荐结构见下文 |
| `execution_config` | JSONB | | 执行配置（引擎、参数等），推荐结构见下文 |
| `engine_id` | INTEGER | INDEXED | ⚠️ **已废弃**（保留兼容）：绑定的引擎 ID，已迁移到 `execution_config.engine_id` |
| `schedule` | VARCHAR(100) | | Cron 表达式（启用调度时必填） |
| `timeout` | INTEGER | DEFAULT 300 | 超时时间（秒） |
| `description` | TEXT | | 描述信息 |
| `tags` | TEXT[] | | 标签数组（用于分类和搜索） |
| `created_by` | INTEGER | | 创建者 ID |
| `updated_by` | INTEGER | | 更新者 ID |
| `created_at` | TIMESTAMP | DEFAULT NOW() | 创建时间 |
| `updated_at` | TIMESTAMP | DEFAULT NOW() | 更新时间 |
| `deleted_at` | TIMESTAMP | INDEXED | 软删除时间（NULL 表示未删除） |
| `status` | VARCHAR(50) | DEFAULT 'active', INDEXED | 状态：'active'、'inactive'、'archived' |
| `last_execution_id` | INTEGER | | 最后执行记录 ID |
| `last_execution_status` | VARCHAR(50) | | 最后执行状态 |
| `last_executed_at` | TIMESTAMP | | 最后执行时间 |

### 2.2 数据库索引

| 索引名 | 字段 | 说明 |
|--------|------|------|
| `idx_dev_items_tenant_type` | tenant_id, dev_type | 按租户和类型查询 |
| `idx_dev_items_query_type` | query_type | 按查询类型过滤（已废弃字段） |
| `idx_dev_items_status` | status | 按状态过滤 |
| `idx_dev_items_deleted` | deleted_at | 软删除查询 |
| `idx_dev_items_content_query_type` | (content -> 'query_type') | **新增**：JSONB GIN 索引，查询 content.query_type |
| `idx_dev_items_execution_config_engine_id` | (execution_config -> 'engine_id') | **新增**：JSONB GIN 索引，查询 execution_config.engine_id |

### 2.3 推荐的 Content 结构

#### Query 类型（dev_type='query'）

```json
{
  "query_type": "sql",
  "query": "SELECT * FROM cities WHERE population > 1000000",
  "limit": 1000
}
```

#### Workflow 类型（dev_type='workflow'）

```json
{
  "workflow_definition": {
    "nodes": [...],
    "edges": [...]
  },
  "inputs": {
    "data_source_id": 1
  }
}
```

### 2.4 推荐的 ExecutionConfig 结构

```json
{
  "engine_id": 1,
  "engine_type": "python_workflow",
  "timeout": 600
}
```

---

## 三、DevType 说明

| 值 | 含义 | content 推荐结构 |
|---|------|------------|
| `query` | SQL/MQL 查询 | `{"query_type": "sql", "query": "SELECT * FROM cities"}` |
| `workflow` | GIS 工作流 | `{"workflow_definition": {...}}` |
| `script` | 脚本开发 | `{"script": "print('hello')", "language": "python"}` |
| `notebook` | Jupyter Notebook | `{"cells": [...], "metadata": {...}}` |

---

## 四、API 端点说明

### 5.1 POST /api/develop/items - 创建开发项

**请求体**：

```json
{
  "name": "查询大城市",
  "display_name": "查询人口大于100万的城市",
  "dev_type": "query",
  "query_type": "sql",
  "content": {
    "sql": "SELECT * FROM cities WHERE population > 1000000",
    "engine_id": 1
  },
  "description": "查询所有大城市",
  "tags": ["城市", "人口"]
}
```

**响应**（201 Created）：返回完整 DevItem 对象

---

### 5.2 GET /api/develop/items - 查询开发项列表

**查询参数**：
- `dev_type`：按类型过滤
- `query_type`：按查询类型过滤
- `status`：按状态过滤
- `tag`：按标签过滤
- `keyword`：搜索名称或描述

---

### 5.3 POST /api/develop/items/:id/execute - 执行开发项

**响应**：

```json
{
  "execution_id": "uuid-xxxx",
  "status": "pending"
}
```

---

## 六、相关文档

- [dev_executions表](./dev_executions表.md) - 执行记录表
- [数据库架构](../数据库架构.md) - Develop 模块架构
