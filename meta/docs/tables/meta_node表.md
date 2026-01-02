# meta_node 表结构和 API 说明

## 表结构概览

`metadata.meta_node` 表是元数据层级节点表，存储数据源的层级结构（engine → schema → table/bucket/folder）。

### 核心字段

| 字段名 | 类型 | 说明 |
|--------|------|------|
| `id` | SERIAL | PRIMARY KEY |
| `tenant_id` | INTEGER | 租户 ID |
| `engine_id` | INTEGER | 引擎 ID（system.engines） |
| `parent_node_id` | INTEGER | 父节点 ID（自引用） |
| `node_type` | VARCHAR | 节点类型：engine/schema/bucket/folder |
| `name` | VARCHAR | 节点名称 |
| `full_name` | VARCHAR | 完整名称 |
| `path` | TEXT | 路径 |
| `depth` | INTEGER | 层级深度 |
| `last_scan_at` | TIMESTAMP | 最后扫描时间 |
| `item_count` | INTEGER | 子项数量 |
| `total_size_bytes` | BIGINT | 总大小（字节） |
| `attributes` | JSONB | 节点属性 |

## 相关文档

- [meta_item表](./meta_item表.md) - 数据项表
- [数据库架构](../数据库架构.md) - Meta 模块架构
