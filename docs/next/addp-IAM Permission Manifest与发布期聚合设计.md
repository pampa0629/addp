# ADDP IAM Permission Manifest 与发布期聚合设计

更新日期：2026-07-27

状态：技术设计和发布门已实现。Manifest、内置 Role、owner-local 常量、确定性 SQL Seed、Common AuthContext Middleware/Guard 及全仓授权覆盖均已收敛；2026-07-27 实测 15 个 owner 的 678/678 个 OpenAPI Operation 已声明精确认证模式，9/9 个 Tool 已映射 Permission，覆盖报告 `complete=true`。

## 一、目标与边界

本文解决：

1. 每个 owner 如何声明自身 Permission；
2. System 如何在不知道业务实现的前提下管理跨模块 Role；
3. 多个模块 Manifest 如何通过唯一发布期流程生成 `system.permissions`；
4. 内置 Role、owner-local 常量、Swagger 和 SQL 如何保持一致；
5. Permission 新增、禁用、模块下线和未来可安装模块如何演进。

本文不定义：

- 具体业务模块的完整 Permission 清单；
- Tenant 自定义 Role 的管理 API 和页面；
- owner Resource Grant / Policy 实现；
- migration runner 或 `000006_iam_catalog_seed.up.sql` 的代码；
- 模块安装包格式或外部插件签名机制。

## 二、核心技术决策

| 决策 | 结论 |
| --- | --- |
| 定义所有权 | Permission 由对应 owner 模块定义，System 只定义自身能力 |
| 共享边界 | `common/authorization` 只持有 Schema、解析/校验器和共享类型，不持有业务目录 |
| 汇总时机 | 构建/发布期聚合，禁止业务模块启动时动态注册 |
| 运行时权威 | `system.permissions` 是已发布目录的运行时权威投影，不是业务定义源 |
| 内置 Role | `system/authorization/builtin_roles.yaml` 由 System IAM 拥有，可组合跨模块 Permission Key |
| Tenant Role | Tenant 自定义 Role 只存在于 System 数据库，不回写模块 Manifest |
| 代码常量 | 每个 owner 只生成自身 Permission 常量，不生成全平台业务常量包 |
| Tool 运行时目录 | 从唯一 Tool Manifest 生成 System 只读 Go 投影，不由 Runtime 扫描仓库 |
| 目录变更 | 只通过向前 SQL migration；已发布 Permission 不删除、不改 Key，只能显式禁用 |
| 模块状态 | 启停、心跳和健康状态不改变 Permission 或 Role 语义 |

## 三、文件与所有权

### 3.1 模块 Permission Manifest

每个定义 Permission 的模块使用固定路径：

```text
<module>/authorization/permissions.yaml
```

示例：

```text
system/authorization/permissions.yaml
manager/authorization/permissions.yaml
meta/authorization/permissions.yaml
asset/authorization/permissions.yaml
agent/authorization/permissions.yaml
```

规则：

- 一个 owner 只有一个 Manifest，不按前端、后端、API、Tool 或资源类型拆分多个事实源；
- Manifest 随 owner 模块源码版本化，模块评审者负责 Permission 语义；
- System Manifest 只定义 `platform`、`iam`、`audit`、`statistics` 和 System 自身能力；
- owner 不能在 Manifest 中声明其他模块的 Permission；
- Portal、Console、Gateway 等消费入口不复制 owner Permission；需要执行业务动作时引用事实 owner 的 Key；
- 模块没有需要授权的公开能力时不创建空 Manifest。

### 3.2 共享 Schema

统一机器可读 Schema 位于：

```text
common/authorization/schemas/permission-manifest-v1.schema.json
```

`common/authorization` 可以包含：

- Manifest DTO 和 Schema 校验器；
- Permission Key 解析和稳定 action 词汇；
- Scope、风险级别和生命周期枚举；
- 聚合冲突检测及共享 fixture；
- AuthContext 和 owner Authorizer 的共享类型。

它不能包含：

- 全平台 Permission 条目；
- Manager、Meta、Asset 等模块的业务常量；
- 内置 Role 的跨模块 Permission 组合；
- Resource Policy、路由表或资源类型注册中心。

### 3.3 System 内置 Role 模板

产品级内置 Role 使用唯一文件：

```text
system/authorization/builtin_roles.yaml
```

该文件由 System IAM 拥有，因为 Role 是统一 IAM 策略，不属于单个业务 owner。它只引用聚合后存在的 Permission Key，不复制 Permission 的 owner、Scope、风险或展示字段。

平台三员、Statistics Viewer、Tenant 管理 Role 和首批业务 Role 都从该文件生成。Tenant 自定义 Role 是运行时业务数据，不进入此文件。

## 四、Permission Manifest v1

### 4.1 结构示例

```yaml
schema_version: addp.permission_manifest/v1
owner_module: manager
manifest_version: 1
permissions:
  - key: manager.data_item.read
    allowed_scope_types:
      - tenant
      - department
      - project_group
    risk_level: low
    tenant_customizable: true
    delegable: true
    status: active
    name_i18n_key: permissions.manager.data_item.read.name
    description_i18n_key: permissions.manager.data_item.read.description

  - key: manager.resource_grant.create
    allowed_scope_types:
      - tenant
    risk_level: high
    tenant_customizable: false
    delegable: false
    status: active
    name_i18n_key: permissions.manager.resource_grant.create.name
    description_i18n_key: permissions.manager.resource_grant.create.description
```

Manifest 根对象和每个 Permission 都禁止未知字段，避免拼写错误被静默忽略。

### 4.2 根字段

| 字段 | 规则 |
| --- | --- |
| `schema_version` | 固定为 `addp.permission_manifest/v1` |
| `owner_module` | ADDP 稳定模块名，必须等于 Manifest 所在模块 |
| `manifest_version` | 从 1 开始的正整数；Manifest 语义变化时严格递增 |
| `permissions` | 非空、按 `key` 字典序排列、Key 不重复 |

`manifest_version` 是 owner 包内目录版本，不替代 SQL migration 版本。聚合器用它拒绝版本回退和“内容变化但版本不变”；System 运行时仍只以已应用 migration 和数据库状态为权威。

### 4.3 Permission 字段

| 字段 | 规则 |
| --- | --- |
| `key` | 全局稳定 `{domain}.{resource}.{action}`；发布后不可修改或复用 |
| `allowed_scope_types` | `platform | tenant | department | project_group` 的非空有序集合 |
| `risk_level` | `low | medium | high | critical` |
| `tenant_customizable` | Tenant 自定义 Role 是否可以引用 |
| `delegable` | 是否允许进入 OAuth/Agent 委托能力集合；不代表自动授权 |
| `status` | `active | disabled`；已发布条目禁用后仍保留在 Manifest |
| `name_i18n_key` | 稳定名称翻译 Key |
| `description_i18n_key` | 稳定说明翻译 Key |

`owner_module` 不在每个条目重复保存，由 Manifest 根字段注入生成的 Descriptor。`action` 不在源条目重复填写，由 `key` 最后一段确定；聚合器验证 action 来自允许词汇表，并在 SQL 投影时写入 `system.permissions.action`。

### 4.4 Namespace 规则

- 普通 owner 的 Permission Key 第一段必须等于 `owner_module`；
- System Manifest 可以使用保留 domain：`system`、`platform`、`iam`、`audit`、`statistics`；
- owner 变更不通过修改 `owner_module` 实现。能力转移必须新增新 Key、迁移调用方并显式禁用旧 Key；
- 不允许别名、兼容 Key、通配符或前缀匹配；
- `resource_grant` internal Permission 仍属于资源 owner，例如 `manager.resource_grant.create`。

### 4.5 状态与不可变性

Permission 首次进入已发布 SQL 后：

- Key、owner 和 action 永久不可变；
- Scope、风险、可定制性和可委托性变化必须经过安全评审和新 migration；
- 收窄能力时必须处理受影响 Role、Principal 授权版本和 Token Family；
- 不再使用时把 `status` 改为 `disabled`，不能从 Manifest 删除；
- disabled Permission 不进入新 AuthContext，也不能新增到 Role；历史 Role Permission 和审计引用仍保留。

## 五、内置 Role Manifest

### 5.1 结构示例

```yaml
schema_version: addp.builtin_roles/v1
manifest_version: 1
roles:
  - key: tenant.data_viewer
    role_type: tenant_builtin
    name_i18n_key: roles.tenant.data_viewer.name
    description_i18n_key: roles.tenant.data_viewer.description
    allowed_scope_types:
      - tenant
      - department
      - project_group
    allowed_principal_types:
      - user
    permissions:
      - manager.data_item.read
      - meta.catalog.read
```

Role 条目必须按 Key 排序，Permission Key 列表按字典序排序且无重复。名称和说明沿用稳定 i18n Key，不把中英文文案写入 SQL。

### 5.2 聚合验证

每个内置 Role 必须满足：

- 所有 Permission 在聚合目录中存在且 active；
- Role `allowed_scope_types` 必须是全部 Permission Scope 交集中的非空显式子集；不能声明任一 Permission 不支持的 Scope；
- Role `allowed_principal_types` 必须是 `user | service_principal` 的非空有序集合；平台三员只允许 `user`，专用 internal Role 只允许 `service_principal`；
- `resource_grant` internal Permission 只能进入仅允许 `service_principal` 的专用内置 Role；
- 平台三员 Permission 和互斥矩阵符合已确认角色矩阵；
- Tenant 内置业务 Role 不包含 Platform、三员或 Internal Permission；Tenant 管理 Role 可以按已确认角色矩阵引用不可由 Tenant 自定义组合的治理 Permission，Service Principal 内置 Role 可以引用明确的 Internal Permission；
- `tenant_customizable` 只约束 Tenant 自定义 Role，不能反向限制经过版本化评审的内置 Role 模板；
- 不使用 Role 继承、Permission 通配符或按前缀展开。

## 六、发布期聚合器

### 6.1 输入

唯一聚合器读取：

1. Permission Manifest v1 Schema；
2. 仓库内所有 `<module>/authorization/permissions.yaml`；
3. `system/authorization/builtin_roles.yaml`；
4. 稳定模块名清单和允许 action 词汇；
5. 需要校验的路由、Swagger 和 Tool Manifest 授权声明。

文件发现只发生在构建、CI 和显式发布流程。System 运行时二进制不扫描目录，也不调用业务模块获取 Manifest。

### 6.2 输出

聚合器生成或校验：

| 输出 | 归属 |
| --- | --- |
| `000006_iam_catalog_seed.up.sql` 及后续目录 migration | System IAM migration |
| owner-local Go / Python / TypeScript Permission 常量 | 对应 owner 模块 |
| 聚合目录摘要和冲突报告 | CI 构建产物，不作为运行时第二事实源 |
| Swagger / OpenAPI required Permission 覆盖报告 | 对应 API owner |
| Tool Manifest Permission / audience / delegable 报告 | 对应 Tool owner |
| `system/backend/internal/authorization/tools_generated.go` | System Runtime 使用的 Tool 授权只读投影 |
| 内置 Role 矩阵一致性报告 | System IAM |

生成的 owner-local 常量只能包含当前 owner 的 Permission。System 业务代码除自身 Permission 外不生成全平台常量；System 的 Role Service 从数据库目录读取 Key 和元数据。

owner-local 常量的唯一目标路径固定为：

```text
<go-owner>/backend/internal/authorization/permissions_generated.go
agent/backend/authorization_permissions_generated.py
copilot/backend/authorization_permissions_generated.py
```

Go 和 Python 文件都必须包含“由聚合器生成、禁止手工修改”头部，只输出当前 owner 的 active Permission Key，并提供不可变顺序的全量 Key 集合。生成文件是 Manifest 的代码投影，不是第二事实源；CI 必须使用同一命令检查字节漂移。

### 6.3 确定性

相同输入必须产生字节级一致输出：

- 文件和条目按稳定 Key 排序；
- YAML map 顺序不参与语义，生成前先规范化；
- 时间戳、绝对路径、机器名和随机值不得进入生成物；
- SQL 使用明确列名和稳定约束名，不使用 `SELECT *`；
- CI 使用唯一聚合命令的 `--check` 只读模式验证全部 Manifest；后续生成 SQL 和 owner-local 常量时复用同一发现、解析和聚合核心。

### 6.4 首批命令契约

首批只读命令固定为：

```bash
cd common
go run ./authorization/cmd/manifest --check --repository-root ..
```

规则：

- `--repository-root` 必须显式提供，命令不根据当前目录向上猜测仓库根；
- `--check` 是当前唯一模式，只读取文件并把规范化 Catalog Report JSON 写到 stdout，不修改 Manifest、SQL、常量或数据库；
- 只发现仓库根下一层的 `<module>/authorization/permissions.yaml`，不递归扫描构建目录、依赖目录或安装包；
- 首批内置 Permission owner 白名单为 `agent`、`asset`、`copilot`、`develop`、`graph`、`manager`、`meta`、`model`、`monitor`、`orchestrator`、`quality`、`service`、`standard`、`system`、`transfer`；Portal、Console 和 Gateway 只消费事实 owner Permission，不成为 Permission owner；
- Manifest 的 `owner_module` 必须与所在一级模块目录名完全一致；未知 owner 和跨目录声明直接失败；
- Builtin Role 固定读取 `system/authorization/builtin_roles.yaml`；
- Report 包含 Permission Manifest 和 Builtin Role Manifest 的仓库相对路径与版本，以及规范化 Permission 和 Role；不包含时间戳、绝对路径、主机名或随机值，相同输入必须输出字节级一致结果；
- Report 是 CI 构建产物，不提交为第二份目录事实源。后续 SQL、常量和覆盖报告由同一 Catalog Report 投影生成。

### 6.5 常量与覆盖报告命令

同一 CLI 提供七个互斥模式：

```bash
cd common

# 只读校验 Manifest 并输出 Catalog Report
go run ./authorization/cmd/manifest --check --repository-root ..

# 写入 owner-local Permission 常量
go run ./authorization/cmd/manifest --generate-owner-constants --repository-root ..

# 只读检查常量是否与 Manifest 字节一致
go run ./authorization/cmd/manifest --check-owner-constants --repository-root ..

# 只读输出路由/Swagger/Tool 授权覆盖报告
go run ./authorization/cmd/manifest --coverage-report --repository-root ..

# 生成 System Runtime Tool 授权目录
go run ./authorization/cmd/manifest --generate-tool-catalog --repository-root ..

# 只读检查 System Runtime Tool 授权目录字节漂移
go run ./authorization/cmd/manifest --check-tool-catalog --repository-root ..

# 只读检查已发布的首批 IAM Catalog Seed SQL 摘要
go run ./authorization/cmd/manifest --check-sql-seed --repository-root ..
```

覆盖报告只消费真实 OpenAPI/Swagger 扩展和 `addp.tool-manifest/v1`，不根据 URL、HTTP Method、Handler 名或 Tool 名猜测 Permission。报告必须稳定列出：

- 缺少 `x-addp-auth-mode` 的公开 OpenAPI Operation；
- `permission | delegated_tool | resource_ticket` 模式缺少 `x-addp-required-permissions` 的 Operation；
- 引用未知或 disabled Permission 的 Operation；Operation 可以显式消费其他事实 owner 的 Permission，但不得使用前缀或路由推断；
- FastAPI 模块缺少可供发布检查的 OpenAPI 投影；
- Tool Manifest 缺少 Permission 映射、audience 与 owner 不一致，或映射到不可委托 Permission。

在历史路由注解补齐前，`--coverage-report` 只输出完整差距并保持成功退出，用于 CI 稳定生成审查产物；覆盖问题不能改变或过滤 SQL migration 中已经通过 Manifest 与 Role 校验的目录事实。`000006` 作为已发布历史 migration 只校验固定 SHA-256 摘要，后续目录变化只进入新的向前 migration；覆盖报告的 `complete=true` 是 IAM Runtime 一次性切换及恢复服务流量的发布门禁。

这两个门禁必须分开：部分旧 API 只有在目标 IAM Runtime 重写时才能删除或拆分，而 Runtime 又依赖完整的首批 IAM DDL。禁止把路由覆盖清零设为 `000006` 的前置条件形成循环依赖，也禁止因尚未清零而降低 Runtime 切换门禁。

## 七、SQL 与运行时目录

### 7.1 首次种子

首批 `000006_iam_catalog_seed.up.sql` 在显式事务中：

1. 插入各 owner Manifest 的 Permission 投影；
2. 插入 System 内置 Role，`created_by_principal_id` 为空；
3. 以 `source_type=product` 插入完整展开的 Role Permission，不创建或引用虚构 System Principal；
4. 插入平台三员 Role Conflict；
5. 插入内置 OAuth Client；
6. 不创建 User、Role Assignment、默认密码或可用三员账号。

首次种子使用确定性显式 `INSERT`，不使用启动时 `ON CONFLICT DO UPDATE` reconciliation。

`000006` 在首次发布时由规范化 Catalog Report 确定性生成。发布后删除重新生成入口，`--check-sql-seed` 只读检查其固定 SHA-256 摘要，防止新 Manifest 版本误写历史 migration。

### 7.2 后续目录变更

已发布 `000006` 不得重写。后续 Permission 或内置 Role 变化生成新的向前 migration：

- 新增 Permission：插入新行；
- 元数据变化：按 Key 更新允许变化的显式字段；
- 禁用 Permission：置为 disabled，停止新 Role 引用；
- 内置 Role 变化：显式增删 Role Permission 行；
- 权限收窄或禁用：递增受影响 Principal 的 `authorization_version`，撤销其 Token Family；
- 已有 Tenant 自定义 Role 引用 disabled Permission 时保留历史关系，但 AuthContext 不投影该 Permission。

目录 migration 在全部 ADDP 服务停止时执行，不依赖 owner 在线。禁止 System 在启动后根据 Manifest 内容动态 reconciliation 数据库。

### 7.3 System 查询边界

System Role 管理只查询运行时目录，以支持：

- 展示可组合 Permission；
- 验证 Tenant 自定义 Role；
- 计算 Role Assignment 和 AuthContext；
- 处理 Permission disabled、Role 变更和授权版本失效。

System 不根据 Permission Key 猜测路由、资源表或业务判断，也不调用 owner 执行 Role 计算。owner 使用 AuthContext 中的 Key 和自身生成常量执行功能校验。

## 八、模块生命周期

### 8.1 启停和故障

Module Registry 的 `up/down`、心跳和路由健康只描述可用性，不修改 Permission：

- 模块 down 时已配置 Role 保持原语义；实际请求因服务不可用失败，不变成权限拒绝；
- 模块恢复后继续使用相同 Permission Key；
- System 不因模块暂时离线禁用 Permission 或重写 Role；
- 审计能够区分 `permission_denied` 与 `owner_unavailable`。

### 8.2 显式下线

模块永久下线必须作为版本化产品变更：

1. 禁止新入口和新授权；
2. 通过新 migration 禁用该 owner Permission；
3. 递增受影响 Principal 授权版本并撤销 Token Family；
4. 迁移或关闭 owner 资源、Grant 和审计责任；
5. 最后删除路由和模块部署。

不能以停止心跳代替模块下线 migration。

### 8.3 未来可安装模块

未来模块安装包必须携带同一个 Permission Manifest v1。安装/升级控制面先完成签名、owner namespace、版本和冲突校验，再生成并审批向前 migration。安装期间不允许模块直接调用 System API 写入 `system.permissions`。

第一阶段只实现仓库内置模块的发布期聚合；未来安装包复用同一 Schema 和 migration 语义，不另建运行时注册协议。

## 九、测试与门禁

至少覆盖：

- Manifest Schema、未知字段、排序、枚举和版本递增；
- 普通 owner 跨 namespace、System 保留 domain 和全局 Key 冲突；
- action 从 Key 派生并属于允许词汇；
- published Key 被删除、改名、换 owner 或内容变化但版本未递增时失败；
- 内置 Role 引用不存在/disabled Permission、Scope 交集为空或误含 internal Permission 时失败；
- owner-local 常量只包含自身 Key，生成两次字节一致；
- System Tool 授权目录只包含通过 Manifest 校验的 Tool，生成两次字节一致且返回值不能暴露可变内部切片；
- SQL seed 与所有 Manifest、Role 模板完全一致，生成两次字节一致；
- Swagger 路由、Tool Manifest 和 owner Permission Manifest 一致，并在 IAM Runtime 切换前使覆盖报告 `complete=true`；
- 模块 down 不改变目录；显式禁用 Permission 后 AuthContext 不再投影并使旧 Family 失效。

核心聚合测试不需要数据库；生成 SQL 的约束、授权版本和撤销行为必须在 PostgreSQL 15 migration 测试中验证。

## 十、已完成的实施记录

1. 建立 `permission-manifest-v1.schema.json` 和共享解析/校验类型；
2. 先为 System、Manager、Meta 三个模块建立最小 Manifest，验证 namespace 和跨模块冲突；
3. 建立 `builtin_roles.yaml`，把已确认 Role 矩阵转成机器可读模板；
4. 实现确定性聚合器和只读 CI check；
5. 迁移所有其余 owner Permission，生成 owner-local 常量；
6. 将路由、Swagger、Tool Manifest 和前端入口接入同一 Permission 契约覆盖报告，删除无真实消费入口或身份边界不合规的未发布 Permission；
7. Catalog、Role 和 owner-local 常量门禁通过后生成并验证 `000006_iam_catalog_seed.up.sql`；
8. 重写 IAM Runtime、删除或拆分旧路由，并把授权覆盖报告收敛为 `complete=true`；
9. 删除旧硬编码角色、零散 Permission 字符串和任何运行时 Permission 注册入口。

第 1 至第 4 步已在编写完整 IAM DDL 前完成，`000006` 的输入和摘要门禁已稳定。

## 十一、已确认的技术决策

1. Permission 定义归 owner 模块，System 只拥有自身 Permission 和产品级内置 Role 模板。
2. `common/authorization` 只提供 Schema、校验器和共享类型，不保存业务目录。
3. Manifest 固定使用 `<module>/authorization/permissions.yaml` 和 `addp.permission_manifest/v1`。
4. `owner_module` 位于 Manifest 根对象；Permission `action` 从 Key 派生，不在条目中重复。
5. 已发布 Permission 永不删除、改名、换 owner 或复用；停止使用时显式 `disabled`。
6. 唯一发布期聚合器生成 SQL seed、owner-local 常量、System Tool 授权目录和契约校验产物；不采用运行时注册或 reconciliation。
7. System 只管理 Permission 契约、Role 组合、Assignment 和 AuthContext，不知道路由实现、资源结构或 owner Policy。
8. 模块健康状态不改变授权目录；永久下线必须通过显式向前 migration。
9. 未来可安装模块复用同一 Manifest 和 migration 路线，不另建动态 Permission 注册协议。

## 十二、当前实现状态

已完成：

- Permission Manifest 与 Builtin Role Manifest v1 Schema；
- Go 严格 YAML 解码、语义校验、跨 Manifest owner/Key 冲突检测和稳定 Descriptor 排序；
- 稳定 owner 白名单中 15 个模块的 Permission Manifest 已全部落地，包含 System 的 105 个完整 Permission 和其他 owner 当前确认的真实 Permission，合计 243 个；
- `system/authorization/builtin_roles.yaml` 的首批 18 个无缺失依赖 Role：9 个平台/Tenant 管理 Role，以及全部 9 个首批业务 Role；
- 15 份 owner-local Go/Python Permission 常量生成物、写入模式和只读字节漂移检查；
- System Runtime Tool 授权目录的确定性 Go 生成物，以及 `--generate-tool-catalog` / `--check-tool-catalog` 字节漂移门禁；
- `addp.authorization_coverage_report/v1` 路由/Swagger/Tool 授权覆盖报告，以及根 `test-authorization` CI 入口；
- Asset 的 34 条公开业务路由已改为真实命名 Handler 并完成 Swagger IAM 投影；Agent 11 条、Copilot 6 条 FastAPI Operation 已生成确定性 OpenAPI 投影；
- 9 个 Tool Scope 已全部映射到精确、可委托的 owner Permission；Copilot SQL/Navigate 已删除请求体身份，Graph 到 Copilot KG 抽取已改为内部服务认证；
- 唯一仓库聚合入口、稳定 owner 白名单、目录 owner 一致性校验、确定性 Catalog Report 和只读 `--check` CLI；
- `000006_iam_catalog_seed.up.sql` 固定摘要门禁、`000009_iam_catalog_restore_actions.up.sql` 向前目录变更，以及 243 个 Permission、18 个 Role、278 个 Role Permission、三员冲突和 `addp-cli` 的 PostgreSQL 15 约束测试；
- 对 Schema、未知字段、多文档、namespace、action、Scope、Principal 类型、i18n Key、排序、Role Permission 引用和真实仓库 Manifest 的测试。

稳定 owner 白名单的首批目录、九个业务内置 Role 和 owner-local 代码常量已全部机器化。Asset、Agent、Copilot 的确定性 OpenAPI 投影和全部 Tool Permission 映射已收敛；其余 Go owner 也已完成精确 `x-addp-auth-mode`、Permission 与 Common AuthContext Middleware/Guard 切换。2026-07-27 全仓报告覆盖 15 个 owner 的 678/678 个 OpenAPI Operation 和 9/9 个 Tool，`complete=true`，授权覆盖发布门已关闭。

System 旧 `/users` CRUD、物理删除 `/tenants`、混合语义 `/logs` 和自研 Auth/OAuth 路由已经删除；目标 `/platform`、`/tenant`、`/users/me`、Fosite OAuth 和 Invitation 路由是唯一生产代码路径。System Swagger 路由覆盖校验为 80 个公开方法一致，另有 1 个 `internal` 审计写入 Operation。

### 12.1 IAM 管理 API 一次性切换门（已确认）

目标路由必须以 AuthContext 模式分开，不再使用一组 `/users` 或 `/logs` 根据旧 `user_type` 切换语义：

| 边界 | 唯一目标路由 | 核心 Permission | 关键语义 |
| --- | --- | --- | --- |
| Platform Tenant 生命周期 | `/platform/tenants` | `platform.tenant.*` | create/read/update 与 suspend/restore/close 分开；close 终态，不物理删除 |
| Platform User 生命周期 | `/platform/users` | `iam.user.*` | 管理全局 User 资料和 suspend/reactivate；目标持有 Platform Role 时必须消费已批准的高权限变更请求 |
| Tenant 成员管理 | `/tenant/memberships` 与 `/tenant/invitations` | `iam.tenant_membership.*` / `iam.tenant_invitation.*` | Tenant 从当前 Tenant AuthContext 获得，不接受可跨租户的 `tenant_id`；公开入会通过邀请、IdP JIT 或同步完成，不开放绕过来源治理的直接 create |
| Platform 审计 | `/platform/audit/events` | `audit.event.read/export` | 只接受 Platform AuthContext，可查询、详情、审计统计与导出 |
| Tenant 审计 | `/tenant/audit/events` | `audit.tenant_event.read/export` | 只查询当前 Tenant，不接受客户端 `tenant_id` |
| User self | `/users/me` 与 `/users/me/password` | `self` | 只消费第一方 Browser Access Token，不并入平台 User 管理 API |

现有状态机是可逆 suspend 与不可逆 close/deactivate 的明确区分，因此不能借用普通 `update` Permission 执行恢复。切换前应以向前 migration 新增：

- `platform.tenant.restore`；
- `iam.user.reactivate`；
- `iam.tenant_membership.restore`。

本切换门已于 2026-07-25 确认并完成。新增 Permission 需同步递增 System Permission Manifest 和 Builtin Role Manifest 版本，生成新的向前 SQL migration，不重写已生成的 `000006`。新 Handler/Service/Repository/Swagger 与前端调用已一次性切换，旧 User/Tenant/Log Handler、Service、DTO 和路由已删除。

### 12.2 当前验证基线

2026-07-25 在当前工作树执行：

```bash
cd common && go test ./...
cd system/backend && go test ./...
cd system/backend && go vet ./...
git diff --check
```

以上全部通过。`common` 的 `go vet ./...` 仍有两个与 IAM 无关的存量问题：MongoDB BSON `primitive.E` 使用未命名字段，以及 PMTiles `WriteTo` 方法名触发 `io.WriterTo` 签名检查。本轮 IAM 不跨边界修改它们。

### 12.3 Tenant Invitation 实施门（已确认）

Permission 目录、目标 DDL、Invitation / Enrollment Runtime 与公开 API 已按本节路线实现。AuthContext 不允许没有 Platform Role 或 Tenant Membership 的 User 建立普通会话，也不使用租户管理员直接调用的 Membership create API 代替邀请；否则会绕过邀请持有者确认，并重新让 Tenant 管理权获得全局 User 创建语义。

唯一已实现路线是：

1. `system.tenant_invitations` 只保存邀请 Secret Hash、Tenant、邀请邮箱、过期时间、状态、创建/撤销/接受人和审计引用，明文 Secret 只交付一次；
2. 新 User 通过邀请 Secret 进入专用注册端点，在同一事务创建 User、Local Account、Tenant Membership、消费邀请和写入审计；
3. 已有 User 使用第一方 Browser Session 和邀请 Secret 接受邀请；
4. 已有但尚无任何可用 Context 的 User，本地认证后只签发一次性 Enrollment Ticket，不增加第三种 AuthContext 模式；Enrollment Ticket 只能与邀请 Secret 一起消费；
5. 首阶段允许 Console 显示一次性邀请链接，不在 IAM 中内嵌邮件发送器；后续通过独立 Notification Provider 交付同一链接。

该决策已于 2026-07-25 确认。实现必须使用单一 Invitation / Enrollment 路线，不开放直接 Membership create，不以内嵌邮件发送器、临时 AuthContext 或旧 User 创建接口形成旁路。

该实施门已完成：全部现有 OpenAPI Operation 均声明 `x-addp-auth-mode` 和精确 Permission，授权覆盖报告为 `complete=true`。
