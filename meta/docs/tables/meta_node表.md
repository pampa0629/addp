# meta_node 表结构和 API 说明

## 表结构概览

`meta.meta_node` 表是元数据层级节点表，存储数据源的层级结构（engine → schema → table/bucket/folder）。

### 核心字段

| 字段名 | 类型 | 说明 |
|--------|------|------|
| `id` | SERIAL | PRIMARY KEY |
| `tenant_id` | INTEGER | 租户 ID |
| `engine_id` | INTEGER | 引擎 ID（system.engines） |
| `parent_node_id` | INTEGER | 父节点 ID（自引用），nullable |
| `node_type` | VARCHAR(64) | 节点类型：schema/bucket/prefix/database |
| `name` | VARCHAR(255) | 节点名称 |
| `full_name` | TEXT | 完整名称，nullable |
| `path` | TEXT | 路径，nullable |
| `depth` | INTEGER | 层级深度 |
| `scan_status` | VARCHAR(20) | 扫描状态：pending/running/completed/failed，默认 `pending` |
| `scanned_depth` | VARCHAR(10) | 已完成扫描深度：none/basic/deep，默认 `none` |
| `scanned_at` | TIMESTAMP | 节点范围最近成功扫描完成时间，开始或失败不覆盖，nullable |
| `scan_error` | TEXT | 最后一次节点扫描错误信息，nullable |
| `attributes` | JSONB | 节点属性，nullable |
| `created_at` | TIMESTAMP | 创建时间 |
| `deleted_at` | TIMESTAMP | 软删除时间（GORM 软删除），nullable |

### 字段说明

- **scan_status**: 索引字段，用于快速查询扫描状态
- **scanned_depth**: 索引字段，用于判断节点已完成 basic/deep 的哪一层扫描
- **scanned_at**: 索引字段，记录节点范围最近成功扫描完成时间
- **scan_error**: 最近一次节点扫描失败时的错误信息

扫描调度配置不属于 `meta_node`。定时、手动、engine 绑定扫描策略统一由 `scan_tasks` 和 `common.task_executions` 表达。

节点 API 的 `item_count` 和 `total_size_bytes` 不存储在本表。Meta 查询层按当前有效父子关系批量聚合子树内未删除的逻辑数据项数量和已知 `size_bytes` 合计；上传、覆盖、删除和重新归属后读取即可更新，且不改变本表扫描状态。旧统计列由 `024_drop_node_scan_statistics.sql` 删除，无需重新扫描源数据。

## 相关文档

- [meta_item表](./meta_item表.md) - 数据项表
- [数据库架构](../数据库架构.md) - Meta 模块架构
