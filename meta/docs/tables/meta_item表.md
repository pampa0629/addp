# meta_item 表结构和 API 说明

本篇维护数据项字段、指纹声明和组成资源的存储约束。节点字段见[节点表](meta_node表.md)，逻辑关系与共用的租户—引擎外键见[数据库架构](../数据库架构.md#33-租户与引擎约束)。

## 表结构概览

`meta.meta_item` 表是数据项表，存储表、对象、文件系统叶子资源等具体数据项的元数据。文件 / 对象 / 目录只表示 catalog 或 storage 形态，不是 `data_type=file`。

### 核心字段

字段表为阅读摘要，完整声明见 [MetaItem 模型](../../backend/internal/models/item.go)；数据库类型、索引与已执行迁移按上述架构说明核实。

| 字段名 | 类型 | 说明 |
|--------|------|------|
| `id` | SERIAL | PRIMARY KEY |
| `tenant_id` | INTEGER | 租户 ID |
| `engine_id` | INTEGER | 引擎 ID |
| `node_id` | INTEGER | 所属节点 ID |
| `item_type` | VARCHAR(64) | 引擎叶子术语，例如 table/view/collection/file/object，不是 data_type |
| `name` | VARCHAR(255) | 项名称 |
| `full_name` | TEXT | 引擎内规范全名；具体坐标由路径规范决定 |
| `fingerprint` | VARCHAR(64) | 规范 full_name 与永久 engine_id 派生的身份指纹，不是内容哈希 |
| `row_count` | BIGINT | 精确表行数的列表投影，未知为 NULL，不能用估算值或未知冒充 0 |
| `size_bytes` | BIGINT | 数据大小（字节），nullable |
| `data_updated_at` | TIMESTAMP | 被扫描数据的最后更新时间，nullable |
| `scanned_at` | TIMESTAMP | 最近成功扫描完成时间，nullable；更新边界见 Scanned Depth 规范 |
| `scanned_depth` | VARCHAR(10) | 已完成扫描深度 none/basic/deep，默认 none |
| `attributes` | JSONB | 项属性（`storage`、`item`、`type_info.*`、`format_info.*`、`capabilities` 等标准分区） |
| `created_at` | TIMESTAMP | 创建时间 |
| `deleted_at` | TIMESTAMP | 软删除时间（GORM 软删除），nullable |

### 字段说明

身份、路径与 fingerprint 的计算统一见[存储引擎路径体系](../../../docs/spec/addp存储引擎路径体系规范.md#二full_name-规则)和[路径与指纹规范](../../../docs/spec/addp路径统一和指纹计算.md#四通用原则)，不在表说明维护另一套拼接算法；大小、源修改时间及标准 attributes 分区沿[唯一事实源](../../../docs/spec/addp元数据attributes规范.md#唯一事实源)。`data_updated_at` 表达源数据修改时间，不与 Meta 的成功扫描时间混用。

`row_count` 仅投影精确 `type_info.table.row_count`，估算值保留在 `estimated_row_count`；完整口径见 [type_info 规范](../../../docs/spec/addp元数据attributes规范.md#type_info-结构约定)。[017 号迁移](../../backend/migrations/017_reset_ambiguous_row_counts.sql)一次性清理无法区分精确／估算的旧行数，不据当前样本补造历史。`scanned_depth` 的落库列与索引由 [015 号迁移](../../backend/migrations/015_add_scanned_depth.sql)声明，实际深度和时间更新沿 [Scanned Depth](../../../docs/spec/addp元数据扫描机制规范.md#scanned-depth)，不能用 attributes 字段存在与否替代。

对带已认领关联资源的单体模型，`size_bytes` 与 `attributes.storage.total_size` 均为完整数据项大小。例如 OBJ 包含明确引用的 MTL 和贴图，其大小合计由 detector 提供；不能在对象存储扫描输入中覆盖为主 OBJ 大小。源对象的 ETag、修改时间及内容哈希仍指向主文件，下载主文件不包含关联资源。旧独立组件的精确认领、成功事务清理与主身份保留沿[探测器身份规则](../../../docs/spec/addp数据项探测器规范.md#item-身份规则)，不再写成仅 deep 才清理；已知 item refresh 不重新发现或清理兄弟 item。

### 指纹与唯一性声明

[MetaItem 模型](../../backend/internal/models/item.go)将 fingerprint 声明为非空及 `idx_meta_item_fingerprint` 唯一索引，未声明 `deleted_at IS NULL` 的部分过滤。[仓储](../../backend/internal/repository/scan_repository_item.go)按 fingerprint 查询含软删除的记录并复用／恢复身份，不能从软删除推断同指纹可以重复插入。声明不等于当前数据库已建索引，实际约束沿数据库架构核对。

标准验证入口及范围见[模块开发与验证](../../CLAUDE.md#开发与验证)；[组件认领 PostgreSQL 回归](../../backend/internal/repository/claimed_components_postgres_test.go)覆盖已声明的持久化边界。门禁源码存在、历史通过或单一格式实测均不替代目标库约束检查、全部格式及真实授权链路验收，未运行项目不计为通过。

## 相关文档

- [meta_node表](./meta_node表.md) - 节点表
- [数据库架构](../数据库架构.md) - Meta 模块架构
