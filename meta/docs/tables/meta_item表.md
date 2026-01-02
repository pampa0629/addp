# meta_item 表结构和 API 说明

## 表结构概览

`metadata.meta_item` 表是数据项表，存储表、文件、对象等具体数据项的元数据。

### 核心字段

| 字段名 | 类型 | 说明 |
|--------|------|------|
| `id` | SERIAL | PRIMARY KEY |
| `tenant_id` | INTEGER | 租户 ID |
| `engine_id` | INTEGER | 引擎 ID |
| `node_id` | INTEGER | 所属节点 ID |
| `item_type` | VARCHAR | 项类型：table/view/file/object |
| `name` | VARCHAR | 项名称 |
| `full_name` | VARCHAR | 完整名称 |
| `row_count` | BIGINT | 行数（表） |
| `size_bytes` | BIGINT | 大小（文件/对象） |
| `object_size_bytes` | BIGINT | 对象大小 |
| `last_modified_at` | TIMESTAMP | 最后修改时间 |
| `attributes` | JSONB | 项属性（列信息、文件类型等） |

## 相关文档

- [meta_node表](./meta_node表.md) - 节点表
- [数据库架构](../数据库架构.md) - Meta 模块架构
