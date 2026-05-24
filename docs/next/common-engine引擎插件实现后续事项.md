# Common Engine 引擎插件后续事项

更新时间：2026-05-24

只保留未决事项。

## 未决事项

1. Common datatype 统一抽象需先完成设计和分阶段迁移，见 [Common Datatype 统一抽象设计](common-datatype统一抽象设计.md)。`common/datatype` 的目标是收拢 ADDP 所有 data type / type info / field type / 横切基础结构；SQL metadata 字段模型只是 table 分支的一部分，后续应服从 `common/datatype`，不继续扩张 `plugin.ColumnInfo` 为平行模型。
2. SQL metadata provider 实现还需继续收敛：先维护 PostgreSQL、MySQL/Doris、ClickHouse、Spark SQL 的元数据来源和差异矩阵，再只对真实复用点抽 provider 内部 helper。
3. ClickHouse 字段原生属性是否进入 metadata 结果需要单独设计：`system.columns` 可提供主键、排序键、分区键、默认表达式、codec、TTL 等，但当前 `ColumnInfo` 公共模型较薄，不应为单一引擎直接扩字段；后续应先走 `common/datatype.FieldInfo` 的通用属性审定。
4. Doris 表级 `Native.engine` 暂不启用。虽然 Doris 复用 MySQL-compatible metadata helper，但需要先确认目标 Doris 版本的 `information_schema.tables.engine` 字段是否稳定存在；未确认前不打开 `IncludeEngine`，避免列表 SQL 因原生列差异失败。
5. 如果后续调整能力展示 API 或能力字段，要同步检查 `manager/docs/数据预览API重构方案.md`、`system/docs/tables/engines表.md`、`docs/spec/addp引擎能力声明规范.md`、`docs/spec/addp引擎插件接口规范.md`。

## 已冻结口径

- `engine.capabilities/v1` 只表达引擎自身 native 能力与 common/engine provider 能力。
- Transfer、Manager 预览等模块适配状态不进入 engine capabilities，也不进入 System 引擎能力展示模型。
- `compute.query`、`compute.workflow`、`compute.script` 是计算能力事实源；旧 `dev_modes` 只允许作为兼容派生概念。
- 工作流算子发现和执行通过 `WorkflowRuntimeProvider`；算子列表、参数、端口等动态能力不写入 capabilities。
- Common Engine 的 provider 是对上层模块的稳定能力契约；SQL metadata helper 只是 provider 内部实现复用工具，不作为新的对外抽象层。
- `common/sqldialect` 当前定位为查询 SQL helper，负责标识符引用、表名限定、分页、count/sample SQL 等；不要把 catalog metadata 探测逻辑混入其中。
- Metadata helper 只在多个引擎共享同一类事实来源时抽取，例如 MySQL/Doris 共享 `information_schema`；PostgreSQL、ClickHouse、Spark SQL 等差异较大的实现可以继续保留在插件内。
- GORM 只作为连接池、driver 和 raw SQL 执行工具，不承担 ADDP 的 catalog path、item metadata、系统库过滤、row count 策略等平台元数据语义。
- `plugin.ColumnInfo` 当前只作为 tabular provider 过渡 DTO，不作为 ADDP 字段元数据演进主模型；通用字段属性应围绕 `common/datatype.FieldInfo` 收敛。

## 推进顺序

1. 先推进 `common/datatype` 文档审阅，确认 data type、type info、field type、横切结构和 layout 归属边界；table / field 只是其中一个分支。
2. 补齐 SQL metadata provider 差异矩阵，标明 namespace 术语、元数据来源、表类型映射、字段信息来源、row count 策略、系统库过滤规则。
3. 只对确认共享同一元数据来源和语义的引擎抽公共 helper；不做大一统 `SQLMetadataDialect`。
4. 清理各插件中的真实问题，例如列表阶段触发重查询、标识符引用不一致、系统库过滤散落等。
5. 统一联动文档和展示字段。能力字段一旦调整，相关 API、表结构说明和规范文档要一起校正。

## SQL metadata provider 差异矩阵

| 引擎 | namespace 术语 | 元数据来源 | 表类型映射 | 字段信息来源 | row count 策略 | 系统 namespace 过滤 | 当前复用边界 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| PostgreSQL | schema | `information_schema.schemata/tables/columns` + `pg_class` + `pg_stat_user_tables` | `BASE TABLE` -> `table`，`VIEW` -> `view`，其他 `table_type` 转小写下划线 | `information_schema.columns`，主键来自约束表，注释来自 `col_description` | 列表和单表 metadata 均使用 `pg_class.reltuples` 统计估算，不主动 `ANALYZE`，不做真实 count | `pg_catalog`、`information_schema`、`pg_toast`、`pg_temp_*`、`pg_toast_*` | 暂留插件内；PostgreSQL 原生 catalog 语义较强，不与 MySQL/Doris 合并 |
| MySQL | database | `information_schema.schemata/tables/columns` | `BASE TABLE` -> `table`，`VIEW` -> `view`，其他 `table_type` 转小写下划线 | `information_schema.columns`，主键来自 `column_key`，注释来自 `column_comment` | 使用 `information_schema.tables.table_rows` 统计值 | `information_schema`、`mysql`、`performance_schema`、`sys` | 与 Doris 共享 `MySQLCompatibleMetadataDialect`；已启用表级 `Native.engine` |
| Doris | database | MySQL 兼容 `information_schema.schemata/tables/columns` | 同 MySQL 兼容逻辑 | 同 MySQL 兼容逻辑，注释能力按引擎实际返回 | 使用 `information_schema.tables.table_rows` 统计值 | MySQL 系统库 + `__internal_schema` | 与 MySQL 共享 `MySQLCompatibleMetadataDialect`；`Native.engine` 待确认 `information_schema.tables.engine` 稳定性后再启用 |
| ClickHouse | database | `system.databases`、`system.tables`、`system.columns` | `MaterializedView` -> `materialized_view`，`View`/其他包含 `View` 的 engine -> `view`，其他 -> `table` | `system.columns`，nullable 从类型字符串推断，当前不表达主键 | 使用 `system.tables.total_rows` 统计值 | `system`、`information_schema`、`INFORMATION_SCHEMA` | 暂留插件内；ClickHouse `system.*` 语义独立 |
| Spark SQL | database | `SHOW DATABASES`、`SHOW TABLES`、`DESCRIBE`，部分环境可查询 `information_schema` | 当前 `SHOW TABLES` 结果统一映射为 `table` | `DESCRIBE table` | 列表阶段不做真实 count；单表 metadata 显式请求时才执行 `COUNT(*)` | `information_schema`、`sys` | 暂留插件内；Spark metadata 更偏命令式接口 |

## 字段属性消费矩阵

原则：字段属性只有能影响扫描、展示、查询建议、质量检测、传输写入、建模标准化或智能生成中的至少一个决策，才进入 ADDP metadata 链路；仅因引擎能查到而没有明确消费方的原生细节，不进入公共模型。

| 字段属性 | 典型来源 | 语义层级 | 主要消费方 | 作用 | 建议归属 |
| --- | --- | --- | --- | --- | --- |
| `native_type` | 各 SQL/文件格式 provider | 通用基础属性 | Meta、Manager、Transfer、Model、Copilot | 保留原生类型，辅助 schema 展示、类型映射、导入导出和代码生成 | 已进入 `FieldInfo.Attributes.native_type` |
| `nullable` | `information_schema.columns`、`system.columns`、文件格式 schema | 通用结构语义 | Meta、Manager、Quality、Transfer | 展示字段约束，推荐非空质量规则，辅助写入校验 | `FieldInfo.Nullable` 顶层 |
| `primary_key` | PostgreSQL 约束表、MySQL `column_key`、部分引擎原生 metadata | 通用结构语义 | Meta、Manager、Quality、Model、Standard | 唯一性识别、主键规则推荐、模型字段识别 | `FieldInfo.PrimaryKey` 顶层；ClickHouse 需先确认原生来源和语义 |
| `comment` | `col_description`、`column_comment`、`system.columns.comment` | 通用描述语义 | Meta、Manager、Model、Standard、Copilot | 字段理解、数据元匹配、智能生成上下文 | `FieldInfo.Comment` 顶层 |
| `default_expression` | SQL 默认值、ClickHouse `default_expression` | 半通用结构语义 | Manager、Transfer、Model、Copilot | schema 还原、写入避让、生成建表语句 | 倾向进入 `FieldInfo.Attributes.default_expression` |
| `default_kind` | ClickHouse `DEFAULT` / `MATERIALIZED` / `ALIAS` | 引擎原生写入语义 | Transfer、Manager、Develop、Copilot | 避免写入 materialized/alias 列，解释字段生成方式 | 进入 `FieldInfo.Attributes.native.clickhouse.default_kind`，不进顶层 |
| `partition_key` | ClickHouse、Spark/Hive、分区表 metadata | 半通用布局语义 | Manager、Develop、Transfer、Monitor、Copilot | 查询过滤建议、写入分区提示、性能诊断 | 倾向定义通用 attributes；需先统一跨引擎语义 |
| `sorting_key` | ClickHouse `system.columns` / 表定义 | 引擎原生优化语义 | Develop、Monitor、Copilot | 查询条件和排序建议，解释 ClickHouse 表性能特征 | 进入 `FieldInfo.Attributes.native.clickhouse.is_in_sorting_key` |
| `codec` | ClickHouse `compression_codec` | 引擎原生存储语义 | Manager、Monitor | 存储诊断、压缩策略展示 | 暂放 native attributes；无明确消费前不展示为通用字段 |
| `ttl` | ClickHouse TTL metadata | 引擎原生生命周期语义 | Manager、Monitor、Governance | 生命周期展示、过期策略诊断 | 暂放 native attributes；需确认治理模块消费方式 |

## 验收标准

- SQL metadata provider 的事实来源、差异和复用边界有文档依据。
- 列表阶段不触发高成本全表扫描或逐表真实 count。
- 可复用逻辑只在共享语义明确的引擎家族内抽取，不引入无法表达差异的大型万能方言层。
