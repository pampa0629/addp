# task_resource_bindings 表结构说明

> 状态：当前实现说明。`manager.task_resource_bindings` 是统一派生任务与外部资源之间的规范化关系投影。

## 一、用途

任务类型的 `config` 结构不同，资源回收不能靠猜测 `source_engine_id`、`target_engine_id`、`locator` 等 JSON 路径。任务创建或更新时，类型适配器必须在同一事务中重建本表绑定；资源回收和引擎生命周期检查只消费本表。

## 二、字段

| 字段 | 语义 |
| --- | --- |
| `id` | 绑定行主键；不作为任务或外部资源身份 |
| `task_definition_id` / `tenant_id` | 所属统一任务定义与租户 |
| `role` | `source` 或 `target` |
| `engine_id` | 资源所属 Engine Instance ID |
| `locator` | 规范 ResourceLocator |
| `item_id` / `item_fingerprint` | 可用时保存的 Meta item 身份与稳定指纹 |
| `ordinal` | 同一角色存在多个资源时的稳定序号 |
| `created_at` | 创建时间 |

### 索引声明与引用完整性

#### 模型与初始化 SQL

当前模型见 [`TaskResourceBinding`](../../backend/internal/models/task_definition.go)，初始化入口见 [`ensureTaskDefinitionSchema`](../../backend/internal/repository/database.go)。除 `id` 主键外，索引分工如下：

| 来源 | 索引 | 列与边界 |
| --- | --- | --- |
| GORM 模型普通索引 | `idx_manager_task_resource_task` | `task_definition_id`；按任务加载绑定，不保证父任务存在 |
| GORM 模型普通索引 | `idx_manager_task_resource_engine` | `(tenant_id, engine_id)`；按租户与引擎定位依赖，不保证引擎存在或当前可用 |
| 初始化显式 SQL 唯一索引 | `uq_manager_task_resource_binding` | `(task_definition_id, role, ordinal)`；同任务同角色同序号最多一行，无部分索引谓词 |

模型本身没有该联合唯一索引的 `uniqueIndex` 声明；仅执行模型 `AutoMigrate` 不能据此认定唯一索引已创建。唯一键不包含 `tenant_id`，也不按 `engine_id`、`locator` 或指纹去重；不同角色或序号可以指向同一资源。本表没有 `deleted_at`，不能套用任务定义的活跃行部分唯一规则。

#### 引用与字段有效性

当前模型和初始化 SQL 未声明指向任务、Tenant、Engine 或 Meta item 的外键、复合租户外键及 `ON DELETE CASCADE`。`task_definition_id` 的普通索引和三列唯一索引均不证明父任务存在，也不保证绑定租户与父任务一致；资源是否存在、可访问或已失效，仍由 Owner 按当前事实与权限核验。

`role=source|target` 是应用契约，当前声明没有对应 CHECK；`NOT NULL` 不等于非空 Locator、正数 ID 或非负序号校验。绑定是服务端从已校验任务配置生成的内部投影，不是客户端独立写入口，也不授予源或目标访问权。配置写入和任务操作边界见 [task_definitions 表说明](task_definitions表.md#四唯一-api)。

## 三、回收语义

- `source` 缺失或其引擎不再属于当前租户时，任务成为回收候选。
- `target` 用于引擎删除影响定位，但业务目标不存在不等同于源任务垃圾。
- 绑定行只保存资源身份事实；聚合后的 `active|missing` 状态和 `missing_engine|missing_source` 原因写入所属 `task_definitions`，不得写入最近执行状态。
- 逻辑回收标记绑定失效、禁用任务并清空下次调度；任务重新保存并重建有效绑定时恢复为 `active`。逻辑回收保留绑定投影，物理删除范围见下文。
- 受管快显任务的显式源重绑必须由 Manager 从新 ResourceLocator 重新投影绑定，不能接受客户端直接写入 `engine_id`、`item_id`、指纹或绑定行；重绑本身不触发 execution。
- 删除任务定义不自动删除 Business/Meta 业务结果。

### 投影替换与删除边界

[`task_definition_repository.go`](../../backend/internal/repository/task_definition_repository.go) 在创建或更新任务的同一事务中删除旧绑定、写入新投影；失败由事务返回并回滚，不能把“先删后写”理解为允许独立提交的两个操作。

- 普通任务删除 `deleteTaskDefinition`：按任务 ID 与 Tenant 显式删除绑定，再按 ID、Tenant 和任务类型调用 GORM 删除任务。任务模型使用 `gorm.DeletedAt`，因此这里是任务软删除、绑定物理删除；并非数据库级联。
- 失效任务合并 `RetireMissingTask`：锁定原任务并校验版本、失效状态和同租户同类型替代任务后，在同一事务中删除旧绑定并软删除旧定义；不将旧 execution 或 artifact 改挂给替代任务。业务条件统一见[失效任务重绑](../quick-view/共用契约.md#失效任务重绑)。
- Cleanup `HardDelete`：在同一事务显式删除绑定与任务行。可执行该路径的范围以[Cleanup 的 Manager 职责](../../../docs/spec/addp-cleanup体系规范.md#4-manager)为准；Engine 生命周期回收保留用户定义并标记失效，不能因请求 `physical_cleanup` 就硬删除任务。

直接删除父任务行不属于上述受控事务路径，当前声明不能保证自动清除孤儿绑定。绑定删除也不删除 System／Meta 资源、源数据或 Business 结果；结果清理由各 Owner 的独立契约约束。

### 初始化与验收边界

初始化先执行模型迁移，再由显式 SQL 创建唯一索引；Manager 启动不自动执行历史 migrations 目录。统一任务初始化还包含旧表清理，不能为只读检查索引而启动初始化或重放 SQL；入口与风险统一见[数据库架构的 Schema 收敛方式](../数据库架构.md#schema-收敛方式)。

现有用例按实际断言区分：

| 用例来源 | 已写入用例的断言范围 |
| --- | --- |
| SQLite：`TestDeleteTaskDefinitionRemovesResourceBindings`、`TestRetireMissingTaskKeepsCanonicalReplacement` | 更新后绑定状态恢复、普通删除后绑定清零、旧失效定义退出读取且替代任务保留；夹具手工建表，未创建上述唯一索引 |
| SQLite：`TestCleanupTaskDefinitionRepositoryListsDisabledAndHardDeletes` | 已禁用定义仍被列出、绑定失效不污染最近执行摘要、硬删除后绑定清零；同样不能证明 PostgreSQL 索引约束 |
| PostgreSQL：`TestIntegrationPostgresManagerUnifiedTaskDefinitionLifecycle` | 调用统一初始化、创建两条源／目标绑定、Cleanup 后任务与绑定均无残留；不等同于直接验证重复绑定拒绝或数据库外键级联 |

实现回归沿 [Manager 开发与验证](../../CLAUDE.md#开发与验证)的 `make test-manager-backend`、`make test-manager-postgres`；PostgreSQL 用例由现有 `TestIntegrationPostgresManager` 前缀命中。上述用例未直接覆盖重复 `(task_definition_id, role, ordinal)` 的拒绝、跨租户父子不一致、孤儿引用、非法字段值及删除／替换中途失败的回滚矩阵，不能据源码存在推定这些场景已通过。

实际部署仍须只读核对索引列顺序、唯一性、有效性，以及外键／CHECK 等真实约束和残留引用；仅有同名索引或初始化代码不足以证明落库结果。文档静态检查不能替代数据库约束、事务失败或真实权限的验收。

## 四、相关文档

- [task_definitions 表结构说明](./task_definitions表.md)
- [快显实现规范](../快显实现规范.md)
