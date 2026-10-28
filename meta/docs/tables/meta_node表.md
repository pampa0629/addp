# meta_node 表结构和 API 说明

本篇维护节点字段、范围扫描事实与节点独有约束。逻辑关系和共用的租户—引擎外键见[数据库架构](../数据库架构.md#33-租户与引擎约束)，data item 字段见[数据项表](meta_item表.md)。

## 表结构概览

`meta.meta_node` 保存 Provider 的实际层级节点，数据项另由 `meta_item.node_id` 关联；不将示意层级固定为 engine → schema → table/file。节点类型与路径坐标沿[存储引擎路径体系](../../../docs/spec/addp存储引擎路径体系规范.md#三元数据树结构)。

### 核心字段

字段表为阅读摘要，完整声明见 [MetaNode 模型](../../backend/internal/models/node.go)；实际类型、索引与约束沿上述数据库说明核实，不由示例类型或字段值推定 CHECK／外键已存在。

| 字段名 | 类型 | 说明 |
|--------|------|------|
| `id` | SERIAL | PRIMARY KEY |
| `tenant_id` | INTEGER | 租户 ID |
| `engine_id` | INTEGER | 引擎 ID（system.engines） |
| `parent_node_id` | INTEGER | 父节点 ID（自引用），nullable |
| `node_type` | VARCHAR(64) | Provider 节点类型，例如 schema/database/bucket/prefix，不是固定层数 |
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

`scan_status` 表达最近一次覆盖本 node 范围的过程状态，`scanned_depth` 与 `scanned_at` 表达已完成深度和最近成功时间。三者的更新、失败与局部刷新边界统一见 [Scanned Depth](../../../docs/spec/addp元数据扫描机制规范.md#scanned-depth)；不能由某个属性存在或单 item 刷新推断父范围成功。`scan_error` 保留节点失败信息，通用诊断仍经显式安全 DTO 投影。

扫描调度配置不属于本表，定义与每次执行分别归 [scan_tasks](scan_tasks表.md)和 Common execution。

节点 API 的 `item_count`、`total_size_bytes` 不入表，由当前有效子树内未删除的逻辑数据项聚合数量与已知大小；上传、覆盖、删除或重新归属后按当前事实读取，不改变扫描状态。完整口径见[节点统计的唯一事实来源](../../../docs/spec/addp元数据扫描机制规范.md#节点统计的唯一事实来源)。[024 号迁移](../../backend/migrations/024_drop_node_scan_statistics.sql)删除旧统计列，无需重扫源数据；[025 号迁移](../../backend/migrations/025_clear_unsuccessful_node_scan_times.sql)仅清空旧 running／failed 节点的歧义扫描时间，不改内容、状态或已完成深度，也不补造无法还原的成功时间。

### 索引与唯一性声明

[MetaNode 模型](../../backend/internal/models/node.go)声明租户—引擎联合索引、父节点、节点类型、扫描状态／深度／时间及软删除索引；SQL 另声明以下唯一性：

- [008 号迁移](../../backend/migrations/008_add_unique_constraint_meta_node.sql)仅对 `deleted_at IS NULL` 的有效节点约束唯一。有父节点时键为 `(tenant_id, engine_id, parent_node_id, name, node_type)`；父节点为空时另用 `(tenant_id, engine_id, name, node_type)`，不能将 NULL 父节点直接当作普通联合唯一键处理。
- [014 号迁移](../../backend/migrations/014_add_meta_node_full_name_unique.sql)对 `(tenant_id, engine_id, node_type, full_name)` 建立语义路径唯一索引，条件是 `deleted_at IS NULL AND full_name <> ''`。该条件不能外推为空 full_name 或软删除记录都受同一限制。

008、014 包含重复节点及关联数据的清理步骤，不是可随意重跑的只读索引检查；现库是否具备这些约束按数据库架构中的迁移边界核对。模型与迁移源码不代替实际落库检查或运行验收。

标准入口及测试隔离见[模块开发与验证](../../CLAUDE.md#开发与验证)；[节点时间回归](../../backend/internal/repository/node_scan_time_postgres_integration_test.go)与[统计迁移回归](../../backend/internal/repository/node_statistics_postgres_integration_test.go)由现有 Meta PostgreSQL 门禁覆盖。源码、历史通过或有限扫描范围不能外推为当前数据库及全部 Provider 已验收；实际结果按报告覆盖的版本与范围判定。

## 相关文档

- [meta_item表](./meta_item表.md) - 数据项表
- [数据库架构](../数据库架构.md) - Meta 模块架构
