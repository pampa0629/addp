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
| `fingerprint` | VARCHAR(64) | 数据指纹：基于 engine_id+路径的 SHA256 哈希，用于去重和数据血缘追踪 |
| `row_count` | BIGINT | 行数（表），nullable |
| `size_bytes` | BIGINT | 数据大小（字节），nullable |
| `data_updated_at` | TIMESTAMP | 被扫描数据的最后更新时间，nullable |
| `scanned_at` | TIMESTAMP | 数据项的扫描时间，nullable |
| `attributes` | JSONB | 项属性（列信息、文件类型等） |
| `created_at` | TIMESTAMP | 创建时间 |
| `deleted_at` | TIMESTAMP | 软删除时间（GORM 软删除），nullable |

### 字段说明

- **fingerprint**: 唯一索引，用于数据去重和血缘追踪
- **data_updated_at**: 表示被扫描的源数据的最后修改时间（如表的 last_modified_at）
- **scanned_at**: 表示 Meta 模块扫描该数据项的时间
- **size_bytes**: 统一的大小字段，适用于所有类型的数据项

## 相关文档

- [meta_node表](./meta_node表.md) - 节点表
- [数据库架构](../数据库架构.md) - Meta 模块架构
