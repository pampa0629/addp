# local_engines 表结构和 API 说明

## 一、表结构概览

`transfer.local_engines` 表存储 Transfer 模块私有的存储引擎配置，用于不希望在 System 模块中共享的临时数据源。

### 核心功能

- **私有引擎配置**：存储 Transfer 模块独立的数据源配置
- **临时数据源**：支持一次性或临时使用的数据源
- **完整连接信息**：存储加密的连接信息

---

## 二、表结构定义

### 2.1 核心字段

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | SERIAL | PRIMARY KEY | 引擎唯一标识 |
| `tenant_id` | INTEGER | NOT NULL, INDEXED | 租户 ID |
| `name` | VARCHAR(255) | NOT NULL | 引擎名称 |
| `engine_type` | VARCHAR(50) | NOT NULL | 引擎类型 |
| `description` | TEXT | | 描述 |
| `is_active` | BOOLEAN | DEFAULT true | 是否激活 |
| `connection_info` | JSONB | NOT NULL | 连接信息（加密存储） |
| `created_at` | TIMESTAMP | DEFAULT NOW() | 创建时间 |

---

## 三、相关文档

- [tasks表](./tasks表.md) - 任务定义表
- [数据库架构](../数据库架构.md) - Transfer 模块架构
