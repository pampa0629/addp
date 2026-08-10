# ADDP Model 概念与数据约束规范

## 一、模块边界

Model 是 Tenant 级数据架构与建模事实的 owner，管理业务实体、实体关系、逻辑模型、数仓分层以及逻辑模型到 Standard 指标的引用。Standard 继续拥有业务域、数据元、维度层级和指标；Model 只保存经过 Standard API 验证的引用，不代理或复制 Standard 资源。

Model 当前不拥有物理表创建和多引擎物化。DDL 预览是 PostgreSQL 方言的设计辅助能力，不改变逻辑模型状态，也不产生物理资源。未来物化必须先形成独立规范，明确目标引擎、执行授权、任务与回收语义后再实现。

## 二、授权边界

Model 资源当前全部属于 Tenant，不存在 Department 或 Project Group Resource Scope Binding。所有 `model.*` Permission 只允许 Tenant Scope。`tenant.data_architect` 是面向 User Principal 的完整 Model 管理角色；`tenant.graph_runtime` 只保留 Graph 导入所需的 Entity 和 EntityRelation 只读权限。

Model 在写入前校验 Standard 引用时，不转发或保存 User Access Token。`addp-model` 使用当前 Tenant 的 Service Access Token 和专用 `tenant.model_runtime`，且该角色只包含 `standard.domain.read`、`standard.element.read`、`standard.dimension_hierarchy.read` 与 `standard.metric.read`。平台控制面的 `platform.model_runtime` 不参与 Tenant 业务引用校验。

Permission Guard 只判断候选能力，Repository 和 Service 仍必须对每个资源及其子资源执行 Tenant 隔离。任何父子写入、删除和关系创建都必须验证完整归属，不能只依赖请求中的全局 ID。

## 三、聚合与引用

- Entity 聚合包含 EntityAttribute；EntityRelation 是连接两个 Entity 聚合的独立关系事实。
- LogicalTable 聚合包含 LogicalField、TableRelation 和 FactMetricMapping。
- LogicalTable 的可选 Entity 引用只表达概念模型来源，不自动同步属性与字段。需要重新生成时必须由显式操作整体替换，不能隐式双向同步。
- DWLayer 是 Tenant 可配置事实。LogicalTable 必须引用已存在的 DWLayer，前端不得维护固定分层枚举作为第二事实源。
- Model 内部引用由数据库外键、唯一约束和 CHECK 约束保证；跨 Standard Schema 的引用由 Standard HTTP API 在写入前验证。

## 四、生命周期

Entity 和 LogicalTable 当前生命周期统一为 `draft` 与 `approved`。只有 `draft` 可修改；审批前必须完成聚合校验。已审批资源如需修改，必须通过显式重新打开操作回到 `draft`，不能在 `approved` 状态直接修改子资源。

`materialized` 不属于当前正式状态。租户资源回收的 logical 模式可以将已审批资源重新打开为 `draft`，physical 模式必须在单个数据库事务中按聚合顺序删除。

## 五、Mermaid 与 DDL

Mermaid 导入采用 Tenant 全量替换语义：先完整解析和校验可逆子集，再在单个事务中替换 Entity、EntityAttribute 和 EntityRelation。导入是破坏性聚合写入，要求 Entity 与 EntityRelation 的创建、删除权限；租户存在已审批实体时必须先全部重新打开。任何解析或写入错误都整体回滚，不返回部分成功。

Mermaid 可逆子集必须保存实体 code、显示名、属性 code、显示名、数据类型、主键、可空性和关系类型。子集外语法必须明确拒绝，不能静默丢失。

PostgreSQL DDL 预览只接受结构化物化配置。Schema、表、字段和分区标识符必须统一校验与引用；分区类型使用固定枚举；不接受任意 SQL 扩展字段。

## 六、完成条件

- 所有写操作具有 Tenant 隔离和领域不变量测试。
- 聚合删除、Mermaid 导入和 physical cleanup 使用事务。
- Model Schema 使用版本化 migration，不在服务启动时执行 `AutoMigrate`。
- API 错误包含稳定 `error_code`，Swagger、前端和跨模块客户端保持同步。
