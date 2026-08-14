# ADDP 数据质量规范

本文定义 ADDP 数据质量规则、规则应用、质量检查 execution、评分结果和质量问题的唯一语义与实现边界。本文是 Quality 与 Standard 模块相关实现的正式规范；`docs/plan/` 中的早期数据治理规划只作为背景材料，不作为当前契约。

## 1. 目标与范围

数据质量闭环由以下事实构成：

1. Standard 定义可复用的数据元质量规则。
2. Quality 将一份规则快照应用到确定的 PostgreSQL 表字段。
3. Quality 以持久 execution 执行检查，并把结果摘要写入 `common.task_executions.metadata`。
4. 未通过的规则维护为当前质量问题，后续 execution 更新同一问题，而不是重复创建同义工单。
5. Monitor 读取统一 execution 事实；其他模块不得复制 Quality 的执行历史或评分存储。

第一版只支持 PostgreSQL 表字段检查，不提供定时调度、事件触发、跨字段规则、自定义 SQL 规则或其他数据库方言。

## 2. 模块职责

| 模块 | 职责 | 禁止事项 |
| --- | --- | --- |
| Standard | 拥有数据元及其版本化质量规则定义；校验规则结构 | 不连接业务数据引擎，不执行质量 SQL，不保存字段应用关系 |
| Quality | 拥有规则应用、检查任务、执行 worker、评分和问题状态 | 不修改 Standard 数据元，不复制引擎凭据，不自建 execution 历史表 |
| Common | 提供版本化规则契约 codec、统一 execution 存储、SQL 方言标识符引用和数据库连接桥等稳定共享能力 | 不拥有规则定义、应用关系、评分或问题模型 |
| System | 拥有 Engine Instance、认证授权和 Execution Authorization | 不替 Quality 解释规则或计算评分 |
| Meta | 提供可定位的数据项和字段元数据；后续可作为显式创建入口的选择来源 | 当前不得在扫描完成后自动触发质量检查 |
| Monitor | 读取 `common.task_executions` 展示执行状态和通用指标 | 不解析或改写 Quality 私有表 |

Model、Asset、Portal 等模块后续若消费质量结果，应读取 Quality 对外契约，不得直接依赖 Quality 私有表结构。

## 3. 质量规则契约

### 3.1 唯一结构

数据元 `standard.elements.quality_rules` 必须使用以下版本化 JSON 结构：

```json
{
  "schema_version": "addp.quality.rules/v1",
  "rules": [
    {
      "rule_key": "0d6c7c6a-4f0d-4d4f-9e5a-6f8e5c7a1b2c",
      "type": "not_null",
      "enabled": true,
      "severity": "error",
      "message": "",
      "params": {}
    }
  ]
}
```

约束：

- `schema_version` 必须且只能为 `addp.quality.rules/v1`。
- `rules` 必须是数组；空数组表示该数据元尚未定义质量规则。
- `rule_key`、`type`、`enabled`、`severity`、`message` 和 `params` 是规则唯一字段集合。
- `rule_key` 必须是非空小写 UUID 字符串，同一规则文档内必须唯一；它是规则治理身份，不是数组位置或规则类型。
- `severity` 只允许 `error`、`warning`、`info`；它用于问题分级，不改变检查真值或评分公式。
- 所有规则参数只允许位于 `params`；不得把 `pattern`、`min`、`max` 等参数提升到规则顶层。
- 不兼容无 `schema_version` 的旧结构，不提供旧字段解析、自动升级或双轨保存。

### 3.2 v1 规则类型

| `type` | 语义 | `params` | 空值处理 |
| --- | --- | --- | --- |
| `not_null` | 值不得为 NULL | `{}` | NULL 失败 |
| `unique` | 所有非 NULL 值必须唯一 | `{}` | NULL 不参与唯一性；是否允许 NULL 由 `not_null` 单独表达 |
| `format` | 文本表示必须匹配 PostgreSQL 正则 | `{"pattern": "..."}` | NULL 跳过 |
| `length` | 文本表示长度必须位于闭区间 | `{"min": 0, "max": 100}`，至少提供一个边界 | NULL 跳过 |
| `value_range` | 数值必须位于闭区间 | `{"min": 0, "max": 100}`，至少提供一个边界 | NULL 跳过；非数值列导致 execution 失败 |
| `allowed_values` | 文本表示必须属于允许集合 | `{"values": ["A", "B"]}`，数组不得为空 | NULL 跳过 |

参数约束：

- `format.pattern` 必须是非空字符串，并由 PostgreSQL 正则引擎解释。
- `length.min`、`length.max` 必须是非负整数，二者同时存在时 `min <= max`。
- `value_range.min`、`value_range.max` 必须是 JSON number，二者同时存在时 `min <= max`。
- `allowed_values.values` 只允许非空字符串，且值不得重复。
- `data_type`、`custom` 不是 v1 规则类型，前后端和执行器均不得保留入口或兼容分支。

`rule_key` 的唯一 owner 是 Standard。新规则由 Standard 在首次创建时生成随机 UUID；规则重排、类型修改、参数修改、级别修改和说明修改都必须保留原 key，删除规则时 key 一并删除。Quality 只能从 Standard 规则快照继承 key，不得基于 RuleApplication、Engine 或物理字段生成第二套规则身份。Standard 更新请求必须提交完整规则文档，服务端不得根据数组位置补 key 或在编辑时重新生成 key；迁移完成后不再接受无 key 文档。

引入 `rule_key` 前没有可恢复的规则身份事实。存量回填只允许使用以下一次性确定性算法，不得按整个规则数组位置、规则类型或模糊相似度猜测：

```text
namespace = f3889a4a-1675-4623-b6e3-773f9125a04d
canonical_rule = PostgreSQL jsonb 文本表示(rule 删除 rule_key 后)
rule_fingerprint = SHA-256(UTF-8(canonical_rule))
duplicate_occurrence = 相同 rule_fingerprint 规则组内按当前数组顺序从 1 开始的序号
name = addp.quality.rule-backfill/v1
     + |tenant_id={tenant_id}
     + |element_id={element_id}
     + |rule_fingerprint={rule_fingerprint 小写十六进制}
     + |duplicate_occurrence={duplicate_occurrence}
digest = SHA-256(namespace UUID 的 16 字节网络序表示 + UTF-8(name))
rule_key = digest 前 128 位，并按 RFC 9562 设置 version=8、variant=10
```

该算法覆盖 `type + enabled + severity + message + params` 的完整规则结构，不包含 RuleApplication ID、Engine、schema、table、column，也不包含规则在整个数组中的位置。内容完全相同的 Standard 规则与 Quality 快照得到相同 key；完全相同的重复规则通过组内序号区分。历史快照内容已经不同或无法唯一映射时必须保留为不同身份或拒绝迁移，不能伪造连续性。

确定性算法只用于一次性存量回填。回填后的正常创建和编辑仍使用并保留 Standard 持有的随机 UUID，不得根据规则内容重新计算身份。Standard 与 Quality 的迁移实现必须使用同一固定测试向量校验输出；旧的数组位置 MD5 算法必须被覆盖，不保留运行时兼容分支。

### 3.3 保存与快照

Standard 在创建和更新数据元时校验完整规则文档。Quality 创建规则应用时从 Standard 读取并再次校验规则文档，只保存其中 `enabled=true` 的规则快照及其 `schema_version`，包括每条规则的 `rule_key`。

规则应用快照是后续 execution 的事实来源。Standard 数据元规则改变后，不得静默修改已有规则应用；用户必须显式重新创建或刷新规则应用。每次 execution 还必须把实际使用的规则应用 ID、规则快照和目标范围写入 `execution_config`，保证历史可审计。

`rule_key` 引入及身份算法修正迁移只更新 Standard 当前规则定义、Quality 当前 RuleApplication 快照和可按旧 key 唯一映射的当前 Issue。已完成 execution 的 `execution_config` 与 `metadata` 是不可变历史，不做回写；迁移时不得存在已经冻结旧身份的 `pending|running` Quality execution。若旧 Issue 无法按其 RuleApplication 内的旧 key 唯一映射到规则，迁移必须拒绝启动并报告冲突，不能按类型、数组位置或相似内容猜测、合并问题。

## 4. 规则应用与检查任务

### 4.1 规则应用

一条 RuleApplication 表示：某个数据元的规则快照被应用于一个确定的 PostgreSQL Engine Instance、schema、table 和 column。

RuleApplication 只持久化 `element_id` 和质量规则快照，不复制数据元名称或编码。Quality 列表 API 必须通过租户服务身份按当前页 `element_id` 集合从 Standard 批量读取当前摘要，并在响应中投影只读 `element: {id, name, code}`；该投影不是历史快照，也不得要求浏览器额外拥有 `standard.element.read`。Standard 不可用或引用失效时列表请求整体失败，不能静默退回裸 ID 或逐条请求。

稳定身份为：

```text
tenant_id + element_id + engine_id + schema_name + table_name + column_name
```

数据库必须对该身份建立唯一约束。重复创建应返回冲突，不得产生两条同时生效的同义应用。`schema_name`、`table_name` 和 `column_name` 都必须明确提供；Quality 不隐式补充 PostgreSQL 默认 schema，避免请求含义与持久化身份不一致。

规则应用创建页必须通过 System 的实时 Catalog 按 Engine Instance 级联选择 schema、table 和 column，不能继续把它们作为自由文本。System Catalog 列表只返回层级节点，表字段通过同一 Catalog 控制面的按需 facts 接口读取。Catalog 只负责创建时的资源发现和归属校验，不改变 RuleApplication 的稳定身份，也不新增 `item_id`、ResourceLocator 或 CatalogPath 持久字段。Quality 后端必须在保存前再次从 System Catalog 确认 schema、table 和 column 属于当前 Tenant 的目标 Engine，不能只信任前端提交值，也不能依赖 Meta 是否已完成扫描。

数据元候选是 Quality 创建工作流所需的跨模块只读投影。浏览器必须通过 Quality 的 `GET /rule-applications/element-candidates` 搜索，Quality 使用租户服务身份从 Standard 读取并只返回 `id + name + code + quality_rules`；不得让仅持有 `quality.rule_application.create` 的浏览器额外依赖 `standard.element.read`，也不得保留浏览器直连 Standard 的并行路径。用户选择数据元后，创建页必须展示将被冻结的全部启用规则及其类型、级别、参数和说明；没有启用规则时前端阻止提交，后端仍执行同一事实校验。

RuleApplication 是当前生效的规则快照，不是执行历史。删除时必须遵循：

1. 若 `pending|running` execution 已冻结该 RuleApplication，删除返回冲突；不得让执行结束后重新产生已失去 owner 的 Issue。
2. 任务触发冻结规则快照与删除必须通过数据库行锁串行化，不能依赖请求时序规避竞态。
3. 无活动 execution 引用时，在同一事务中删除该 `tenant_id + rule_application_id` 下全部 `rule_key` 对应的当前 Issue，再删除 RuleApplication。
4. 已完成 execution 的 `execution_config` 和 `metadata.rule_details` 继续作为不可变历史保留，不因 RuleApplication 删除而改写或清理；历史记录中的 `rule_key` 是执行时冻结的事实。

RuleApplication 可通过唯一 `PUT /rule-applications/{id}` 显式切换 `enabled`，请求体必须且只能包含布尔字段 `enabled`。启停只影响未来 execution 的规则冻结；已进入 `pending|running` 的 execution 继续使用其不可变快照。手动停用不改变已有 Issue 状态，因为停止检查不等于问题已解决或已忽略；问题治理仍通过 Issue 的独立人工状态流转完成。重新启用前必须确认绑定 Engine 当前仍为本 Tenant 的 active PostgreSQL Engine；停用不依赖 Engine 可用性。更新只能修改 `enabled`、`updated_by` 和 `updated_at`，不得用整行保存覆盖并发变化，也不得通过空请求隐式解释为停用。

### 4.2 检查任务

一条 CheckTask 表示对确定 PostgreSQL Engine Instance 上一个确定表执行其全部已启用规则应用。CheckTask 是纯手动/Orchestrator 显式执行的任务定义，不拥有调度开关；任务是否可执行由目标 Engine Instance 生命周期和执行授权共同决定。v1 任务范围固定为：

```text
tenant_id + engine_id + schema_name + table_name
```

`schema_name` 和 `table_name` 都必须提供；不支持“整个 schema”或“整个引擎”的隐式扩展范围。任务语义身份必须有数据库唯一约束。任务只支持手动执行或由 Orchestrator 显式执行：

检查任务创建和更新页必须通过 System 的实时 Catalog 按 Engine Instance 级联选择 schema 和 table，不能继续把二者作为自由文本。Catalog 只负责表单中的资源发现和保存前归属校验，不改变 CheckTask 的稳定身份，也不新增 CatalogPath 持久字段。Quality 后端必须在创建和更新前再次确认 schema 和 table 属于当前 Tenant 的目标 Engine；不能只信任前端提交值，也不能依赖 Meta 扫描状态。

- TaskProvider `supports_schedule=false`
- TaskProvider `supports_cancel=false`
- `trigger_type` 只接受 `manual`
- `source` 只记录真实调用来源，不扩展触发类型
- CheckTask API、TaskProvider 任务摘要和前端不得暴露 `enabled`、`schedule` 或 `next_run_at` 等调度活状态字段。

创建和更新任务不保存 schedule、`next_run_at` 或任务授权主体。未来开放 owner 调度前，必须先更新本规范和任务体系规范。

CheckTask 列表中的运行状态只能投影 `last_execution_id + last_execution_status + last_run_at`，不得再建立任务运行状态字段。前端必须展示该最近 execution 摘要并提供详情入口；`pending|running` 期间按列表摘要轮询，并禁用编辑、重复执行和删除，终态后停止轮询并恢复操作。后端的任务行锁和 active execution 检查仍是并发正确性的最终防线。

## 5. PostgreSQL SQL 编译

### 5.1 方言边界

Quality v1 只允许目标 Engine Plugin 为 PostgreSQL。API、任务创建、规则应用创建和执行前都必须校验该事实；前端只展示 PostgreSQL Engine Instance。

规则应用和检查任务的历史列表必须同时读取 `active`、`disabled` PostgreSQL Engine Instance，以名称和 ID 回显已有绑定；`deleting` 不进入正常业务展示或选择。新建和更新只允许选择 `active` 引擎，前端提交前与后端都必须再次校验生命周期，不能把历史回显集合误作可选集合。

SQL 编译必须遵守：

1. schema、table、column 等标识符使用 `common/query.Dialect` 的 PostgreSQL 引用能力，不能直接字符串拼接。
2. 正则、边界和允许值全部使用参数绑定，不能通过转义后拼入 SQL。
3. 编译结果由 SQL 文本和参数数组共同构成；repository 或 executor 必须原样传给数据库驱动。
4. 每条规则使用一条聚合查询同时返回 `total_count` 和 `failed_count`，避免对同一表重复执行无关计数。
5. 规则结构、参数、目标方言或数据库表达式错误都属于 execution 错误，不能跳过规则后继续宣告成功。

Quality failed execution 的 `error_details.code` 必须使用稳定领域错误码；原始数据库、SQL、连接和外部服务错误只写服务日志，不得返回给前端。v1 错误码包括：`quality.authorization.issue_failed`、`quality.authorization.persist_failed`、`quality.execution.authorization_missing`、`quality.execution.authorization_failed`、`quality.execution.unsupported_engine`、`quality.execution.target_connection_failed`、`quality.execution.rule_snapshot_invalid`、`quality.execution.no_rule_applications`、`quality.execution.rule_compile_failed`、`quality.execution.sql_execution_failed`、`quality.execution.result_invalid`、`quality.execution.lease_expired`、`quality.issue.reconcile_failed` 和兜底码 `quality.execution.failed`。前端必须使用同一映射按错误码本地化展示失败原因；执行列表和详情都只能展示该稳定原因，不得展示持久化的安全摘要或内部错误文本。

### 5.2 空表与规则真值

空表上所有合法规则均视为通过，`pass_rate=100`，因为不存在反例。该规则只适用于至少存在一条有效规则的 execution；没有规则应用时 execution 必须失败，不能以空集计算出 100 分。

## 6. Execution Authorization

每次质量检查都必须持有绑定该 execution、Tenant、`addp-quality` audience、目标 Source Engine 和只读效果的 Execution Authorization。

- Console/API 手动执行：Quality 使用当前请求的 User Access Token 向 System 签发授权；不得使用 Service Principal 代替用户数据权限。
- Orchestrator 子 execution：Quality 只根据可验证的 `parent_execution_id` 向 System 派生授权；不得接受客户端自报用户身份或复用任务创建人权限。
- Quality 的 Service Principal 只用于调用 System 控制面和消费已经签发给 `addp-quality` 的授权。
- execution 只保存授权 ID、允许效果和过期时间等授权事实摘要，不保存 User Token、Service Token 或引擎凭据。
- 获取目标引擎连接必须消费本次 Execution Authorization；授权缺失、过期、audience 不匹配或 Engine 不在授权范围时 execution 失败。

## 7. 持久执行生命周期

Quality 使用数据库任务队列作为唯一执行路线，不使用请求内 goroutine、第二套消息队列或进程内状态。

### 7.1 创建与领取

1. API 在 CheckTask 行锁事务内检查该任务不存在 `pending|running` execution。
2. 同一事务创建 `common.task_executions(status=pending, started_at=NULL)`，冻结任务目标和启用规则应用快照，并更新任务最近执行摘要。
3. 事务提交后签发 Execution Authorization，再以条件更新把授权事实附加到仍为 pending 的 execution；签发或附加失败必须将该 execution 置为 failed。
4. worker 只领取已附加 Execution Authorization 的 pending execution，使用 `FOR UPDATE SKIP LOCKED` 原子推进为 `running`，设置 `started_at`、`lease_owner` 和 `lease_expires_at`。
5. 数据库领取事务必须短小；外部 HTTP、目标数据库查询和结果计算都在事务外完成。

### 7.2 lease 与恢复

- running worker 在长任务期间续租。
- worker 启动和固定周期都扫描 lease 已过期的 running execution。
- 每次成功领取时增加 `attempt`；lease 过期后，未达到最大尝试次数的 execution 回到 pending，达到上限则进入 failed 并写稳定错误码。
- 只有持有当前 lease 的 worker 可以写进度和终态，防止过期 worker 覆盖新 worker 结果。
- 终态只允许 `success`、`failed`、`timeout`、`cancelled`，并按任务体系规范写完成时间和耗时。

Quality 当前不提供取消，因为目标 SQL 的可靠中断、连接回收和终态确认尚未形成闭环。

System lifecycle cleanup 与任务触发必须使用相同的 CheckTask 行锁顺序串行化。cleanup 的 scan 报告和 execute 最终判断都必须查询 `common.task_executions` 中该任务的 `pending|running` execution，不能把 CheckTask 的最近执行摘要当作并发事实。logical cleanup 只有在命中任务均无活动 execution 时，才可在同一事务中禁用 RuleApplication，并将 open Issue 置为 ignored、记录系统处理时间且保持 `resolved_by=NULL`；任一更新失败必须整体回滚。
tenant physical cleanup 同样必须在按 ID 稳定顺序锁定命中 CheckTask 并查询活动 execution 后，以单一事务按 ID 顺序、按 `tenant_id + id` 删除 Issue、CheckTask 和 RuleApplication；任一删除失败、候选跨 Tenant 或目标已并发变化必须整体回滚，execution 历史不受影响。logical cleanup 对 RuleApplication 和 Issue 也使用稳定 ID 顺序，避免重叠 lifecycle cleanup 形成交叉锁顺序。

## 8. 结果与评分契约

成功 execution 的 `metadata` 使用以下唯一结构：

```json
{
  "schema_version": "addp.quality.execution-result/v1",
  "quality_score": 97.5,
  "total_rules": 2,
  "passed_rules": 1,
  "failed_rules": 1,
  "field_scores": [
    {
      "column": "email",
      "score": 97.5,
      "rule_count": 2
    }
  ],
  "rule_details": [
    {
      "rule_application_id": 12,
      "rule_key": "0d6c7c6a-4f0d-4d4f-9e5a-6f8e5c7a1b2c",
      "type": "format",
      "severity": "error",
      "message": "",
      "schema": "public",
      "table": "users",
      "column": "email",
      "total_count": 1000,
      "failed_count": 50,
      "pass_rate": 95,
      "passed": false
    }
  ]
}
```

评分公式：

- 单条规则 `pass_rate = (total_count - failed_count) / total_count * 100`；空表为 100。
- 单条规则仅在 `failed_count=0` 时通过。
- 字段分数是该字段所有规则 `pass_rate` 的算术平均。
- 总质量分是全部规则 `pass_rate` 的算术平均，不按行数、severity 或字段数加权。
- 任一规则未能完整执行时整个 execution 为 failed，不写部分成功评分。
- `metadata` 是结果唯一来源；API 和前端不得读取或生成平行的 `result` 字段。

## 9. 质量问题

### 9.1 当前问题身份

Issue 表示某个规则应用当前仍存在的质量问题，不表示一次 execution 的失败事件。稳定身份为：

```text
tenant_id + rule_application_id + rule_key
```

数据库必须建立三字段唯一约束。每次成功检查后：

- 规则失败：按 `tenant_id + rule_application_id + rule_key` upsert 同一 Issue，更新 `last_execution_id`、计数、通过率、规则快照摘要和 `last_observed_at`；新问题为 `open`。
- 规则通过：若存在 open Issue，则自动标记为 `resolved`，记录 `resolved_at` 和本次 execution。
- execution 失败：不改变任何 Issue 当前状态，因为没有形成完整的新质量事实。

execution 中每条规则的发生记录保留在不可变的 `metadata.rule_details`，不得通过复制 Issue 行表达历史。

### 9.2 状态机

Issue 状态只允许：

```text
open -> resolved
open -> ignored
resolved -> open   （后续检查再次失败）
ignored -> open    （后续检查再次失败）
```

人工接口只允许 `open -> resolved|ignored`，并要求填写可审计的处理说明；其他转换由新检查事实驱动。状态、处理人和处理时间必须在一个事务内更新。数据库应使用 check constraint 限制状态枚举。

## 10. API 与分页

- 所有租户资源查询必须从认证上下文获取 Tenant，不能相信客户端传入的 Tenant ID。
- 创建使用 `POST`，完整替换使用 `PUT`；Quality v1 的更新接口使用完整请求模型，不保留“指针字段即局部更新”的伪 PUT。
- 列表响应遵循 ADDP 统一分页结构，参数使用 `page`、`page_size`；必须返回真实 `total`，并应用稳定排序。
- Quality 当前数据量和 Console 跳页需求使用 offset 分页；索引必须覆盖常用 Tenant、状态、目标范围和稳定排序字段。未来切换游标分页时必须更新 API 规范并删除 offset 路线。
- 执行记录列表可按 `status` 筛选；只接受 `pending`、`running`、`success`、`failed`、`timeout`、`cancelled`，未传表示全部。结果必须按 `created_at DESC, id DESC` 稳定排序。
- 错误使用统一错误 envelope、稳定错误码和国际化消息；数据库、SQL 和外部服务原始错误不得直接返回给前端。
- Swagger 注解、生成文件、前端调用和后端行为必须在同一变更中同步。

## 11. 数据库约束与索引基线

至少需要：

- RuleApplication 稳定身份唯一索引。
- CheckTask 稳定身份唯一索引。
- Issue `tenant_id + rule_application_id + rule_key` 唯一索引。
- Issue 列表的 `(tenant_id, status, updated_at DESC, id DESC)` 组合索引。
- CheckTask 列表的 `(tenant_id, updated_at DESC, id DESC)` 组合索引。
- pending execution 领取与 lease 恢复使用匹配过滤条件的部分索引；不为全部终态记录维护无用队列索引。
- Issue 必须以同租户 RuleApplication 为 owner 并使用级联删除外键兜底；业务删除仍显式执行当前投影清理，不能把生命周期语义隐藏在数据库级联中。

所有 ID 使用平台现有 bigint 语义，时间使用带时区时间事实。禁止仅依赖 GORM 应用层校验代替数据库唯一约束、check constraint 或并发控制。

Quality 私有表只通过模块内连续、带校验和、持有数据库迁移锁的 SQL migration 演进。服务启动不得同时运行 GORM `AutoMigrate`、启动期临时 DDL 或第二套迁移路线；已应用 migration 内容变化、版本缺失、版本越界或约束不满足时必须拒绝启动。

## 12. 当前明确不支持

以下能力不得出现在 API、TaskProvider capability 或前端可操作入口中：

- MySQL、ClickHouse 或其他非 PostgreSQL 方言
- 自定义 SQL、跨字段、跨表或聚合业务规则
- schedule、owner scheduler 和 Meta 扫描事件自动触发
- CheckTask 调度状态字段 `enabled`、`schedule` 和 `next_run_at`
- 伪取消、仅修改状态的取消
- 自动按字段名匹配数据元的 AutoMap
- 无规则成功、规则错误跳过、旧规则结构兼容解析

后续扩展必须先明确规则语义、Provider 能力矩阵、授权效果和测试基线，再修订本文。

## 13. 验证基线

实现至少覆盖：

1. 六类规则文档校验和拒绝旧结构。
2. 标识符引用、参数绑定、恶意名称/值和 PostgreSQL SQL 集成测试。
3. 空表、无规则、部分失败、非法规则和目标 SQL 错误。
4. 手动 User 授权、Orchestrator 父 execution 派生、授权过期和 Engine 越权。
5. pending claim、并发 worker、lease 过期恢复、最大尝试失败和服务重启恢复。
6. Issue 按 `tenant_id + rule_application_id + rule_key` 去重、自动 reopen/resolve、人工状态机、并发更新和 RuleApplication 删除级联。
7. 列表分页、Tenant 隔离、HTTP 状态码、错误 envelope 和 Swagger 路由覆盖。
8. 前端 Engine 选择、任务执行轮询、metadata 结果展示、错误反馈和路由恢复。
