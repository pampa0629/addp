# ADDP 数据安全与隐私保护实现规范

版本：v1.0

更新日期：2026-08-31

本规范定义 ADDP `Security` 模块的唯一实现主线。概念解释见 [ADDP 数据安全与隐私保护体系图](../concepts/addp数据安全与隐私保护体系图.md)。

## 一、适用范围与强制原则

本规范适用于：

- Security 分类、等级、敏感数据类型、检测器和保护基线；
- 专业资源显式纳管、敏感发现、人工确认和保护策略；
- Security 到 Manager、Transfer、Develop、Service 的版本化保护投影；
- 各资源 Owner 在服务端数据出口的本地执行；
- Catalog 对 Security 专业事实的权限感知联邦展示。

强制原则：

1. Security 是安全分类分级、Finding、Assessment、ProtectionPolicy 和 ProtectionProjection 的唯一事实 owner。
2. Standard 不再拥有安全分类分级；Catalog 不复制 Security 可编辑事实。
3. Security 使用 owner 稳定专业资源身份，不以 Catalog UUID 作为发现、评估、策略或执行前置身份。
4. Catalog 与 Security 可并行消费 Meta 事实；Catalog 未建档不得阻断保护生效。
5. 未显式纳管的资源不进入 Security 路径，不增加远程调用、字段遍历、保护算法或保护审计。
6. 已进入纳管生命周期的资源必须 fail closed；策略缺失、损坏、冲突、过期或结构不匹配不得回退明文。
7. 保护只在 Owner 服务端出口执行，浏览器不接收明文后再遮盖。
8. 用户数据请求不同步调用 Security、Catalog 或 Meta，只读取 Owner 本地有效投影。
9. 第一阶段不开放原值揭示，不保留基于角色名称、记录创建人或管理员身份的本地绕过路径。

## 二、模块、运行角色与基础设施

| 项目 | 固定值 |
| --- | --- |
| 模块名 | `security` |
| 中文名 | 数据安全 |
| Backend 开发/生产端口 | `8194` |
| Frontend 开发端口 | `5191` |
| Frontend Docker 端口 | `8122` |
| API BasePath | `/api/v1/security` |
| PostgreSQL Schema | `security` |
| OAuth Client / Service Principal | `addp-security` |
| Backend 角色 | 管理 API、策略编译、投影变化流和激活协调 |
| Worker 角色 | 有界敏感发现 execution，无独立 HTTP 端口 |

Backend 和 Worker 都必须支持零 Engine Instance 启动。自身 PostgreSQL 和必需 Infra 可以构成 Ready 条件；System 注册是业务进程 Ready 的唯一控制面强依赖。Meta、Catalog、Manager、Standard、Model、Transfer、Develop 和 Service 不可达只失败相关操作或延迟同步，不影响 Security 启动和 Ready。

Security Worker 首期执行 `task_type=sensitive_data_discovery` 的 ad-hoc bounded execution，`source_task_id` 保存触发该执行的 ProtectionEnrollment ID。当前不建立可调度的扫描任务定义，也不发布 TaskProvider；未来出现可重用、可编排的明确需求时先修订本规范。

## 三、类型化专业资源引用

Security 中所有 Finding、Assessment、Enrollment 和 Policy 必须使用同一类型化目标值对象：

```json
{
  "owner_module": "meta",
  "resource_type": "data_item",
  "resource_identity": "<item-fingerprint>",
  "component_key": "userInfo.phone"
}
```

约束：

- `owner_module` 是稳定小写模块名；
- `resource_type` 使用 owner 公开稳定类型，不使用展示名称；
- `resource_identity` 使用 owner 公开稳定身份的规范字符串形式；
- `component_key` 可空，非空时必须由 owner 字段/组件契约提供，不允许 Security 按展示标题自行猜测；
- 引用不包含 `tenant_id`，Tenant 只来自 AuthContext 或 Service Access Token Context；
- 引用不包含 CatalogEntry ID、ResourceLocator、URL 或物理表名的兼容字段。

面向用户创建 ProtectionEnrollment 的唯一命令输入是 Meta 资源树返回的标准 DataItem `locator`。Security 必须验证 locator 同时包含有效 `engine_id`、leaf 类型、路径和 `item_id`，根据 `engine_id + full_name` 计算 DataItem fingerprint，再在聚合内部形成 `{meta, data_item, fingerprint}` 专业资源引用；浏览器不得提交或手工填写 fingerprint。ResourceLocator 只是创建命令的已校验选择上下文，不进入专业资源引用、Finding、Assessment、Policy 或 Owner 投影。

ProtectionEnrollment 首期固定纳管整个 DataItem，不接受 `component_key`。字段或文档组件只能由 Detector 在发现执行中产生，随后进入 Finding、Assessment 和保护规则；不得保留自由文本字段路径或 DataItem 级、字段级两条创建路线。

创建时同时冻结最小 protection target snapshot，只包含 `engine_id`、DataItem `item_type` 与 `full_name`。该快照只用于 Security 列表、详情和历史审计，以及校验 fingerprint 前像；不得作为资源存在性、授权、自动改绑或保护执行依据，也不得扩展为 Meta attributes、字段或样本副本。

正式 Assessment 还必须冻结确认时真正影响结论的 dependency snapshot，至少包含 owner 源版本或结构指纹、观测时间和快照 Hash。快照不复制完整 Meta attributes 或原始样本。

owner 资源消失、源版本或结构指纹变化时：

1. 原 Assessment 和历史保留；
2. 有效 Policy 进入待复核状态；
3. Owner 继续执行已安装的更严格保护，或在无法安全匹配时拒绝；
4. 不得按名称、路径相似度或 Catalog 来源重绑静默改绑安全事实。

## 四、Security 业务对象

| 对象 | 作用 | 并发/修订约束 |
| --- | --- | --- |
| SensitiveDataType | 定义敏感数据类型、不含原值的证据 Schema 和保护阈值 | 可变聚合根，使用 `version` |
| SecurityClassification | 定义安全类别 | 可变聚合根，使用 `version` |
| SecurityGrade | 定义等级、顺序和最低控制强度 | 可变聚合根，使用 `version` |
| Detector | 定义结构特征、内容特征、适用数据类型和版本 | 可变聚合根，发布修订不可变 |
| ProtectionBaseline | 定义 SensitiveDataType + SecurityGrade + action 的最低保护意图 | 可变聚合根，发布修订不可变 |
| ProtectionEnrollment | 把一个专业资源显式纳入 Security 生命周期 | 可变聚合根，使用 `version` |
| SensitiveFinding | 保存自动发现候选、置信度和非原值证据 | 不可变观测记录；复核结果单独保存 |
| ResourceSecurityAssessment | 保存正式分类、等级、敏感类型、依据和 dependency snapshot | 可变聚合根，每次确认产生不可变修订 |
| ProtectionPolicy | 针对 Assessment、消费 Owner 和动作显式收紧 ProtectionBaseline | 可变聚合根，每次创建、更新或撤销产生不可变修订 |
| ProtectionProjection | 面向单一消费 Owner 的可执行投影 | 编译产物，不可编辑，可从 Security 事实重建 |

`version` 只表示资源并发版本；发布修订号必须使用具有领域含义的字段。所有更新、删除、发布、审核、启停和退出纳管操作必须携带正整数 `version`，冲突返回 `409 resource_version_conflict`。

## 五、纳管状态机与激活屏障

`ProtectionEnrollment.state` 只允许：

```text
activating -> enrolling -> active -> releasing -> released
```

| 状态 | Security 行为 | Owner 行为 |
| --- | --- | --- |
| `activating` | 产生面向必要 Owner 的最小 `enrolling` 门禁变化，等待安装确认 | 未安装前仍是旧路径；Security UI/API 必须明确标记“保护未生效” |
| `enrolling` | 必要 Owner 已确认门禁，可启动发现和策略编译 | 相关动作使用 `deny` 的第一阶段默认门禁，不返回明文 |
| `active` | 有效 Policy 已编译并被必要 Owner 确认 | 使用本地有效投影 |
| `releasing` | 发布明确 release 变化并等待确认 | 继续使用最后有效保护，不因找不到新策略自动解除 |
| `released` | 保留历史和审计 | 只在已原子安装 release 后删除本地纳管索引 |

创建 Enrollment 返回 HTTP `201` 和 `state=activating`，不得返回假的“已生效”。Security 必须在必要 Owner 对门禁版本完成确认后才进入 `enrolling`。第一阶段结构化 DataItem 必要 Owner 固定为 Manager、Transfer、Develop 和 Service；尚未能执行字段保护的 Owner 必须安装资源级 `deny` 门禁，不得被从激活屏障中忽略。

Enrollment 查询响应必须分别返回每个 Owner 当前投影的 `projection_state` 与该版本的 `acknowledged` 状态。确认只表示 Owner 已安装当前 revision，不能在 UI 中被解释为字段级保护已经 active；产品界面必须区分“已遮盖”“保守拒绝”“等待安装”和“已解除”。

退出纳管只使用同一个 Release 子资源路径。Release 请求除 Enrollment `version` 和必填原因外，必须携带稳定依据 `basis=manual|no_supported_findings`；Enrollment 必须冻结 `release_basis`、`release_requested_by`、`release_requested_at` 和当次退出依据的 `release_source_snapshot_hash`。`no_supported_findings` 只允许在最近一次发现已成功、当前快照 Finding 数为 0，且同一 Enrollment 不存在 pending/running 发现 execution 时提交；服务端在同一退出事务内校验，不信任前端判断。

## 六、敏感发现执行

1. Security 只为已进入 `enrolling` 的显式 Enrollment 创建发现 execution。
2. Worker 使用 `addp-security` Tenant Service Access Token 精确读取 owner 已授权技术事实，不订阅 Meta 全量 DataItem 变化。
3. Detector 先使用字段路径、名称、注释、类型和结构；证据不足时才受控采样。
4. 受控采样必须使用统一 Engine Provider / content reader 和绑定本次 execution、Tenant、目标资源、`read` 效果及有效期的 Execution Authorization。
5. `addp-security` Service Principal 不获得 Tenant 全量数据读取权；创建 Enrollment 的当前 User 无权读取目标时，只允许元数据检测，不得使用 Security 管理权绕过 owner 数据授权。
6. 原始样本只存在于当前 Worker 有界处理内存，不写入 Finding、execution metadata、日志、错误或审计。
7. Finding 证据只保存命中规则、置信度、样本数量、格式符合数、检测器版本和不可逆证据摘要。
8. Worker 使用通用 execution lease；租约过期且未达到 `max_attempts` 时原执行回到 `pending`，达到上限时以不含原值的稳定错误码失败，不允许异常退出后永久停留在 `running`。
9. Enrollment 查询必须返回最近一次成功发现的摘要：`status=not_completed|completed`、该快照的 `finding_count`、`pending_review_count` 和 `reviewed_count`。列表查询必须通过 Finding 与不可变初审记录批量聚合，不得为每个 Enrollment 新增 Finding 或 review 查询。
10. `completed + finding_count=0` 只表示当前已实现检测能力对该次快照零命中，不是“资源已被证明不含敏感数据”的 Assessment。Enrollment 继续保持 `enrolling` 和资源级 `deny`，不得自动编译 `allow`。
11. 治理人员可以基于零命中摘要显式确认“当前无需保护”，但该确认的唯一效果是使用 `basis=no_supported_findings` 创建 Release 并进入 `releasing`；不创建空 Assessment、`allow` Policy 或第二条放行路径。

Meta 专用技术事实读取契约固定为 `GET /api/v1/meta/runtime/data-items/{fingerprint}/security-facts`，由 Meta owner 使用不可租户定制、不可委派的精确 Permission `meta.security_facts.read` 和固定 `addp-security` Client Guard 保护。Tenant 只能来自 Tenant Service Access Token，端点不接受客户端提交 Tenant ID，也不返回完整 attributes、连接信息或原始样本值。响应 Schema 固定为 `addp.data_item_security_facts/v1`，只包含 DataItem fingerprint、item type、字段路径/名称/注释/通用类型、Meta 观测时间以及由 `common/dataprotection.TableSchemaSnapshotHash` 计算的结构快照 Hash。Security 必须按 Enrollment 的精确 fingerprint 单项读取，不得把该端点扩展为全量列表或变化订阅。

## 七、Finding、Assessment 与唯一策略编译器

- Finding 只是候选证据，不能自动成为正式 Assessment。
- SensitiveDataType 和 ProtectionBaseline 定义保护阈值。Finding 达到阈值时，编译器允许在人工确认前生成保守临时决策。
- Finding 不达阈值或结构证据不足时，Enrollment 保持 `enrolling`，Owner 继续执行资源级 `deny`。
- 安全治理人员可以确认、调整或驳回 Finding。确认或调整形成 Assessment 修订；误报驳回保留 Finding 和复核记录，不删除历史证据。
- 治理界面只把 Enrollment 最近一次成功发现的 `latest_source_snapshot_hash` 作为当前可操作候选集；历史快照 Finding 继续保留用于审计，但不得与当前候选混合展示或复核。`GET /findings` 必须支持按 `enrollment_id + source_snapshot_hash` 精确过滤，并在同一分页响应中返回可选的不可变 `review`，避免前端逐条查询。
- 一个 Finding 只允许形成一次不可变初审记录；重复初审返回 `409`。初审后的治理调整必须在既有 Assessment 上新增 revision，不得改写 Finding、review 或历史 Assessment revision。
- `confirm` 继承 Finding 的 SensitiveDataType 及其默认等级；`adjust` 必须显式给出目标 SensitiveDataType 和 SecurityGrade；二者都在同一 `{tenant, enrollment, component_key}` Assessment 聚合上追加 revision。`reject` 只形成 review，不创建 Assessment。
- Assessment 聚合只保存当前 revision 指针、资源并发 `version` 和审计字段；每个不可变 revision 冻结来源 Finding/review、SensitiveDataType、SecurityClassification、SecurityGrade、来源结构快照 Hash 和已确认组件结构。该依赖快照不得包含原始样本值。
- 编译器合并有效 Assessment、ProtectionBaseline 和 ProtectionPolicy，对同一资源、组件、消费 Owner 和动作始终选择更严格结果。
- 候选基线与正式 Assessment 都必须进入同一编译器和同一投影变化流；不允许 Owner 实现“自动发现脱敏”与“正式策略脱敏”两条路线。
- 当 Finding 被驳回且同一组件不存在有效 Assessment 时，唯一编译器必须发布新的 `enrolling` 资源级拒绝投影；不得继续沿用旧 `active` 候选基线，也不得通过删除投影使 Owner 回到明文路径。

同一个 Enrollment 的不同 Owner Projection 允许按各自已实现的执行能力分阶段从 `enrolling` 升级为 `active`：当前 Manager、Develop 与 Service 已发布各自字段级 `active` Projection，Transfer 继续执行资源级 `enrolling` deny。Manager 可以返回受保护预览，Develop 可以返回受保护查询结果，Service 可以通过已发布 QueryService 的直接查询内核返回受保护结果，但 Enrollment 整体状态仍为 `enrolling`；只有全部必要 Owner 都已安装并确认当前 `active` Projection，Enrollment 才进入 `active`。不得用“部分 Owner 已 active”伪装整体保护闭环已完成。

第一阶段保护效果严格度固定为：

```text
deny > suppress > mask > allow
```

`allow` 只表示当前 Security 保护决策不额外变换内容，不代表 owner 已授权资源动作。第一阶段的敏感组件不得编译 `allow`。

### 7.1 ProtectionPolicy 首期语义

ProtectionPolicy 不是第二份 ProtectionBaseline、任意策略 DSL 或资源 ACL。它只绑定一个正式 Assessment，并在一个确定消费出口上表达显式收紧：

```text
tenant + assessment_id + consumer_owner + action
```

首期约束固定为：

1. 可编辑 Policy 的 `consumer_owner` 只开放已实现字段级执行的 `manager`，`action` 只开放 `preview`；新增可编辑 Owner 或动作必须与对应 Owner 执行能力在同一变更中落地。`manager/profile` 是编译器根据同一敏感组件的有效 `preview` 决策派生的系统动作，不建立第二份可编辑 Policy。
2. 策略效果只允许 `mask|suppress|deny`，严格度必须大于或等于当前 Assessment 对应的有效 ProtectionBaseline；不得用 Policy 编译 `allow` 或降低保护强度。
3. `mask` 只表示继续使用 ProtectionBaseline 中已注册的算法和参数；Policy 不复制算法参数，也不接受任意表达式。
4. 同一绑定至多有一个 Policy 聚合。聚合保存资源并发 `version` 和当前修订号；每次创建、更新或撤销追加不可变 revision。
5. 撤销 Policy 通过 `DELETE` 表达，但不物理删除历史：携带 `version` 和原因，追加 `revoked` 修订。撤销后编译器回落到 Assessment + ProtectionBaseline，不解除纳管、不删除投影、不返回明文。
6. 没有显式 Policy 不是异常，也不要求用户为每个敏感字段创建策略。Assessment + ProtectionBaseline 是默认且完整的最低保护路径。
7. Policy 变更与 ProtectionProjection 新修订必须在同一数据库事务完成，并继续调用唯一编译器；禁止新增 Policy 专用投影生成旁路。

用途约束、原值揭示、限时放宽和双人审批仍属于后续范围，不得借 ProtectionPolicy 的首期 API 提前实现。

### 7.2 保护定义变化的影响传播

保护定义变更不能等待下一次人工复核或重新发现才偶然生效，也不能把每次变更扩大为全租户重编译。Security 必须按事实依赖精确定位受影响 Enrollment，并继续调用同一个投影编译器：

| 变化 | 受影响范围 | 编译行为 |
| --- | --- | --- |
| ProtectionBaseline 创建 | 当前 SensitiveDataType + SecurityGrade 已存在 Finding 或正式 Assessment 的 Enrollment | 新基线与对应新投影在同一事务生效 |
| ProtectionBaseline 完整更新、启停或改绑类型/等级 | 更新前绑定与更新后绑定范围的并集 | 基线新版本与全部受影响投影在同一事务生效 |
| ProtectionBaseline 删除 | 删除前绑定范围 | 必须携带资源 `version`；删除与受影响投影重编译在同一事务完成，无其他有效规则时回到 `enrolling` 资源级 deny |
| SensitiveDataType 默认等级或保护阈值更新 | 当前类型 Finding 所属 Enrollment | 只重新计算候选 Finding；正式 Assessment revision 已冻结类型、分类和等级，不随定义静默改写 |
| SecurityClassification、SecurityGrade 名称、描述、层级、排序或风险顺序更新 | 无执行影响 | 不发布空转投影；治理展示读取定义新版本，历史 Assessment revision 仍保留原引用 |

影响定位必须使用 Security 自有 Finding、Assessment current revision 和 Enrollment，不访问 Catalog，不扫描 Meta，不读取样本。每个受影响 Enrollment 在一次定义写事务中至多编译一次，并使用该 Enrollment 最近一次成功发现保存的 `latest_source_snapshot_hash`；缺少成功发现快照时不伪造 active 投影。

被 SensitiveFinding、SensitiveFindingReview 或 ResourceSecurityAssessmentRevision 引用的 SensitiveDataType、SecurityGrade、SecurityClassification 不允许删除。定义删除不得级联删除 Finding、review、Assessment、Policy、Projection 或历史修订。

## 八、Protection Projection v1

稳定 Schema 名称为 `addp.protection_projection/v1`。示例：

```json
{
  "schema_version": "addp.protection_projection/v1",
  "projection_id": "34bd62a9-b8a1-476e-80aa-80350d13cf87",
  "revision": "00000000000000000017",
  "consumer_owner": "manager",
  "state": "active",
  "target": {
    "owner_module": "meta",
    "resource_type": "data_item",
    "resource_identity": "<item-fingerprint>"
  },
  "source_snapshot_hash": "sha256:<hex>",
  "rules": [
    {
      "action": "preview",
      "component": {
        "key": "userInfo.phone",
        "path": [
          {"name": "userInfo", "container": "object"},
          {"name": "phone", "container": "scalar"}
        ],
        "value_type": "string",
        "schema_fingerprint": "sha256:<hex>"
      },
      "decision": {
        "effect": "mask",
        "algorithm": "addp.mask.keep_prefix_suffix/v1",
        "parameters": {
          "prefix_runes": 3,
          "suffix_runes": 4,
          "replacement": "****",
          "exact_runes": 11,
          "character_class": "ascii_digit"
        },
        "invalid_value_effect": "suppress"
      }
    },
    {
      "action": "profile",
      "component": {
        "key": "userInfo.phone",
        "path": [
          {"name": "userInfo", "container": "object"},
          {"name": "phone", "container": "scalar"}
        ],
        "value_type": "string",
        "schema_fingerprint": "sha256:<hex>"
      },
      "decision": {
        "effect": "suppress",
        "invalid_value_effect": "suppress"
      }
    }
  ],
  "valid_from": "2026-08-31T12:00:00Z",
  "expires_at": "2026-09-01T12:00:00Z",
  "checksum": "sha256:<canonical-json-hash>"
}
```

约束：

1. `projection_id` 稳定标识一个 consumer + target 投影，`revision` 是 20 位零填充十进制字符串，只用于等值和大小比较。
2. `consumer_owner` 由 Security 按固定消费者编译；运行时 feed 根据 OAuth Client 身份过滤，调用方不提交该值选择其他 Owner。
3. `state` 只允许 `enrolling|active`；退出纳管使用变化流 `operation=release`，不用空策略伪装解除。
4. `rules[].action` 是 consumer owner 已实现的稳定数据出口动作；第一阶段至少覆盖 `preview|profile|search_index|export|query|service_publish|service_execute|ai_context`。
5. `component.path` 是类型化递归路径，`container` 只允许 `object|array|scalar`；Owner 必须正确遍历嵌套 object 和 array，不得只处理顶层扁平字段。
6. `effect` 只允许 `allow|mask|suppress|deny`；未实现的 `filter`、假名化和原值揭示不得提前填入首期投影。
7. `algorithm` 和参数必须属于 `common/dataprotection` 公开注册的稳定契约；Owner 不得按 SensitiveDataType 名称硬编码私有规则。
8. `invalid_value_effect` 只能与当前决策同等或更严，首期手机号固定为 `suppress`。
9. `state=enrolling` 是激活屏障专用的资源级 `deny` 投影：`source_snapshot_hash` 必须为空且 `rules` 必须为空；Owner 命中后直接拒绝目标资源的相关动作，不尝试字段遍历。Security 不得在读取 Owner 技术事实之前伪造结构快照或字段规则。其有效期不能被解释为解除纳管；即使安装时已过有效期，Owner 仍安装纳管标记并按资源级 `deny` 处理，直到收到明确 `release`。
10. `state=active` 必须携带 `sha256:` 格式的 `source_snapshot_hash` 和至少一条可执行规则；Owner 必须校验 Schema、revision、checksum、有效期、target 和结构指纹后才原子切换。
11. 投影缺失、无法解析、checksum 错误、结构冲突或过期时，已纳管资源使用资源级 `deny`；不取消纳管标记。

结构化表数据的 `source_snapshot_hash` 与 `component.schema_fingerprint` 只能由
`common/dataprotection` 的规范算法生成，Security 和 Owner 不得各自拼装 JSON：

- `source_snapshot_hash` 的 preimage Schema 固定为 `addp.table_schema_snapshot/v1`，内容是按字段路径排序后的全部字段结构；
- 每个字段结构只包含 `path`、ADDP 标准 `type`、`element_type` 和 `nullable`，不包含注释、原生类型、样本值、统计值或展示顺序；
- 普通关系字段未声明 `FieldInfo.path` 时，规范路径固定为单段 `FieldInfo.name`；动态结构字段必须使用 Meta 保存的完整 `FieldInfo.path`；
- `component.schema_fingerprint` 的 preimage Schema 固定为 `addp.table_component_schema/v1`，内容是从记录根到目标 scalar 的每一级字段结构；
- 两者均为规范 JSON 的 SHA-256，外部形式固定为 `sha256:<lowercase-hex>`；重复路径、缺失路径、容器类型不匹配或目标值类型不匹配均视为结构冲突；
- Owner 在读取数据前用当前 Meta DataItem 结构快照校验一次，在服务端序列化前按相同组件路径执行。结构校验失败必须拒绝，不得按字段名猜测或退回明文。

`common/dataprotection` 只保存该 Schema 的稳定值对象、规范 JSON checksum、严格校验和确定性算法。它不保存 Enrollment、Finding、Assessment、Policy、AuthContext 或审计状态。

## 九、投影变化流与 Owner 确认

Security 在与投影修订发布相同的数据库事务中追加 consumer-specific、append-only 变化记录。Owner 使用固定 Service Principal 按 Tenant 拉取：

```http
GET /api/v1/security/runtime/protection-projections/changes?after_cursor={opaque}&limit=200
Authorization: Bearer <owner-service-access-token>
```

响应：

```json
{
  "schema_version": "security.protection_projection_changes/v1",
  "changes": [
    {
      "change_id": "opaque-change-id",
      "operation": "upsert",
      "projection": {}
    }
  ],
  "next_cursor": "opaque-cursor",
  "has_more": false
}
```

- `after_cursor` 为空表示从当前 Tenant 、当前消费 Owner 历史起点读取；
- `limit` 默认 200，最大 500；
- cursor 和 `change_id` 完全不透明，Owner 只能原样保存和回传；
- `operation` 只允许 `upsert|release`；`release` 包含 target、projection_id 和 release revision，不包含空 Projection；
- 变化历史首期不设保留窗口，以便 Owner 从起点恢复。

Owner 必须在单个本地数据库事务中：

1. 校验整批变化和 Projection；
2. 更新本地纳管索引及有效投影；
3. 执行该 Owner 对变化目标声明的派生结果清理或重写屏障；
4. 保存 `next_cursor`；
5. 提交后向 Security 确认已安装 cursor。

投影、派生结果处理和 cursor 必须共用同一事务。任何清理或重写失败都必须整体回滚，Owner 不得保存 cursor 或向 Security 确认该批变化。Owner 重启时还必须在对外服务前，以已安装投影重放同一派生结果收敛逻辑，覆盖升级前已确认 cursor、但尚未具备新执行器的历史状态。

同一 Owner 存在 Backend、bounded Worker、continuous Worker 等多个数据面进程时，投影表和 cursor 是 owner schema 内的共享持久事实，只允许一个同步进程推进 Security 变化流并发送 Owner 级 acknowledgement。每个实际读取进程必须在一次 execution 开始前比较共享持久 cursor 与本进程内存索引 cursor；不一致时先从 owner 本地数据库原子重载，再执行门禁。这样 acknowledgement 表示投影已经持久安装，而任一稍后执行的数据面进程都不会在缓存尚未刷新时返回明文。该检查只访问 Owner 本地数据库，不形成 Security 请求依赖。

Owner 的同步收敛明确分为两个不同屏障，不得混为一次“保存 cursor”操作：

- `ProjectionChangeBarrier` 在安装批次的本地数据库事务内运行，只处理可与投影、cursor 原子提交的 owner 派生数据；Manager 剖析结果等数据库内投影属于此类。
- `AcknowledgementBarrier` 在本地事务提交后、向 Security 回执前运行，等待旧 cursor 下已经开始的长生命周期读取或请求结束，并清除无法加入本地数据库事务的外部派生数据；Service 瓦片缓存等外部可重建结果属于此类。

后置屏障失败时不得回滚已经安全安装的本地投影和 cursor，也不得 acknowledgement；同步进程必须以同一 cursor 幂等重试后置屏障。新请求在此期间已经依据持久新 cursor 门禁，旧请求由后置屏障阻止回执，因此不会出现“Security 已确认、旧数据流仍在输出”的窗口。

Develop 的后置屏障必须依据真实执行边界，而不是仅依据历史 `status`：本进程已经通过门禁且尚未结束的读取由 Gate 活动计数追踪，Notebook 由会话活动执行追踪，跨进程 bounded Worker 只认 `running` 且尚未过期的 execution lease，无租约的本地异步执行只认 `running` 且尚未过期的 Execution Authorization。`pending` 尚未开始读取，启动时必须按持久新 cursor 重新过门禁，因此不得阻塞回执；租约和授权均已过期的遗留 `running` 记录也不得形成永久屏障。不得通过手工改写历史 execution 状态推进回执。

```http
POST /api/v1/security/runtime/protection-projection-acknowledgements
Authorization: Bearer <owner-service-access-token>
Content-Type: application/json

{
  "applied_cursor": "opaque-cursor"
}
```

Security 从 AuthContext 中确定 Tenant 和固定 OAuth Client，不接受调用方提交 `tenant_id`、`consumer_owner`或“已应用资源列表”。确认是一个单调 consumer checkpoint，重复确认同一 cursor 必须幂等；倒退或未由 Security 签发的 cursor 返回 `409 protection_projection_cursor_conflict`。

Security 只在所有必要 Owner 的 acknowledgement 均覆盖对应门禁/有效投影变化时推进 Enrollment 状态。Security 不主动逐个调用 Owner 推送，不另建消息队列双路线。

## 十、Owner 本地执行契约

Owner 从当前请求解析出专业资源身份后，只允许一条本地门禁路径：

```text
解析 owner 专业资源身份
  -> 本地纳管索引查找
  -> 未命中：原有路径
  -> 命中 enrolling/失效：拒绝
  -> 命中 active：本地授权判断 + Projection 严格结果
  -> 服务端序列化前执行
```

专业资源身份及查询结果来源的可信事实按数据出口分为两类：

- Locator / 任务绑定出口：Owner 使用服务端已校验的 ResourceLocator、Meta DataItem fingerprint 或不可变发布快照；
- 查询出口：Owner 只消费 Engine `QueryRuntimeProvider.PrepareQuery()` 生成的 PreparedQuery，根据本地纳管范围决定是否调用其 `ReadSet()`，再按 Engine Catalog Model 转为 DataItem `engine_id + full_name` 指纹；命中纳管资源后继续从同一计划读取 `OutputLineage()`，不解析查询文本。Security、Meta 和 Catalog 不参与用户请求期间的身份或结果来源解析。

`QueryReadSet` 必须是普通查询的统一 PreparedQuery 计划事实；门禁后只能执行同一 PreparedQuery，不得重新提交查询，也不得为 Security 另做一次方言解析。未纳管资源不调用 Security、Meta 或 Catalog。Owner 在当前 Tenant 的本地索引完全没有纳管目标时不展开完整读依赖；只要该 Tenant 存在任一纳管目标，查询 Owner 就必须执行有界的 `ReadSet()` 分析并以本地指纹 map 精确判断本次查询是否命中。Projection v1 只有稳定 DataItem 指纹，没有可作为安全依据的 Engine 路由提示，因此不得在解析 ReadSet 前猜测“当前 Engine 与纳管无关”。未命中纳管目标的查询可能因 JOIN、View 或类似间接引用而承担该最小必要分析成本；不承诺绝对零负担。

当查询可能涉及已纳管 DataItem 而 Provider 无法得到完整 `QueryReadSet` 时，Owner 必须拒绝当前查询，不得只检查 `TargetPath`、默认 schema 或 SQL 顶层对象。这是当前请求的不可解析失败，不能扩大为租户级全局禁用 SQL。

查询命中已纳管 DataItem 后，Owner 必须取得完整 `QueryOutputLineage`，用其中每个 source 的实时 `Fields` 校验该资源 Projection 的 `source_snapshot_hash` 和组件 `schema_fingerprint`，再按该 Owner 的稳定查询出口动作执行：Develop 使用 `query`，Service 已发布查询服务使用 `service_execute`，Transfer bounded snapshot 查询源使用 `export`。identity 输出按原组件路径处理；direct binding 按明确的结果路径处理；受保护组件命中 derived binding、opaque source、缺失当前动作、结构漂移或无法解析 lineage 时拒绝整个查询。不得把 `preview`、`query` 或其他 Owner 的规则互相复用，不得按同名结果列猜测来源。字段保护必须在原始 `QueryResult` 或批次产生后、写 execution metadata、执行 Transfer 字段变换或返回 HTTP / Notebook Kernel 前完成。

第一阶段以 MongoDB 和 PostgreSQL 作为精确查询门禁与字段级 `query` 执行的验证范围。MongoDB PreparedQuery 的 ReadSet 必须覆盖主集合以及 `$lookup`、`$graphLookup`、嵌套 `$unionWith`；PostgreSQL PreparedQuery 的 ReadSet 必须按真实 `search_path` 解析关系并递归展开普通视图。PostgreSQL 普通标量、聚合和窗口函数只有在 Provider 根据当前连接目录证明全部可匹配候选均为不新增数据源的可信 `pg_catalog` 内置实现时才继续分析；该证明不使用函数名白名单，用户函数、扩展函数、表函数、集合返回函数和副作用边界不明的函数继续 fail-closed。结果血缘首期开放 PostgreSQL 直接列、显式别名和可证明的单来源 wildcard，以及 MongoDB `find`、`count`、`distinct`；MongoDB aggregate、PostgreSQL 派生敏感值、View 底层敏感值和无法消歧的多来源输出在命中纳管资源时拒绝，不以不完整遮盖冒充字段保护。其他尚不能证明完整读取闭包或结果血缘的 Provider 返回对应类型化 unresolved，只有该 Tenant 已存在纳管目标且当前出口需要判定时才拒绝，不影响完全未纳管 Tenant 的原路径。SQL PreparedQuery 的 `ReadOnly` 不能只做语句分类，还必须由 Provider 执行路径建立数据库只读事务；门禁通过后执行的仍是同一个 PreparedQuery。

通用执行规则：

- 未命中本地纳管索引时立即进入原有路径，不遍历字段、不调用 Security、不写保护审计；
- 同一个请求可以因 owner Resource Policy 拒绝，不得因 owner 授权降低 Security 效果；
- `mask` 只改变当前输出，不回写数据源、Meta 或 Catalog；
- `suppress` 不返回原值，结构化输出的缺失语义必须由对应 Owner 协议固定，不得用原值兜底；
- 处理完成的结果、日志、错误、搜索文档、前端状态和 AI 上下文都不得包含该决策下的原值；
- 审计只记录 Principal、Tenant、动作、目标、策略/投影版本、效果、结果和稳定原因码。

Owner 可以使用最后一份未过期、checksum 正确且结构仍匹配的投影。Security 暂时不可达不影响当前数据请求；投影过期后必须拒绝已纳管动作。

### 10.1 Manager `profile` 动作

Manager 数据剖析不得直接复用 `preview` 对行值的遮盖算法。唯一 Security 编译器必须为每个已保护组件同时产生 `preview` 与 `profile` 规则，并按有效 `preview` 决策确定 `profile`：

- 有效 `preview=deny` 时，`profile=deny`；
- 有效 `preview=mask|suppress` 时，`profile=suppress`；
- 第一阶段敏感组件不产生 `profile=allow|mask`。

`profile=suppress` 的稳定结果语义是：Manager 在持久化前删除该组件以及全部祖先容器的字段剖析对象，并同步删除指向这些字段的全局观察；祖先 object/array 的 Top N 可能聚合并携带敏感叶子值，因此不能只删除叶子字段。因此 Top N、min/max、分布、基数、空值统计和类型专属指标都不会通过目标或祖先剖析进入数据库或 API 响应。表级 `field_count` 表示保护后的可见字段数。规则组件无法与本次 Meta 结构和剖析字段精确匹配、投影失效或出现未知效果时统一拒绝，不按名称猜测、不保留旧结果。

第一阶段已纳管 DataItem 只开放 `kind=all` 剖析。`kind=condition` 的条件值既会影响源读取，也可能进入 execution 配置；在形成独立的条件值保护契约前必须拒绝。投影 upsert、revision 变化和 release 都必须在 Owner 本地事务屏障中删除该目标的历史剖析结果，并清除历史 execution 配置中的条件原值；release 后需要用户重新剖析，不恢复退出纳管前的缓存结果。

### 10.2 Service `service_execute` 动作

Service 已发布 QueryService 的 REST Query 与 OGC API Features 共用唯一结构化查询内核，因此首个字段级切片只开放该内核的直接 PreparedQuery 执行，并使用独立 `service_execute` 动作。Security 必须从同一基线为 Service 编译独立规则；只属于 `manager/preview` 的 ProtectionPolicy 不得收紧 Develop 或 Service 投影。

Service 必须在同一个 PreparedQuery 上依次完成 `ReadSet()`、命中判定、`OutputLineage()`、真实执行和结果保护。保护发生在协议格式化、CSV/GeoJSON 序列化、HTTP 返回和执行审计之前。`suppress` 必须同时删除结果值和公开字段清单；`mask` 只改变返回副本，不回写数据源或发布契约。

分页 cursor 与 OGC feature ID 可能依赖尚未保护的稳定键或排序值。它们只能在当前请求内存中从原始结果计算，并使用带机密性与完整性保护的随机 nonce AEAD 不透明令牌返回；仅签名但仍可解码出原始 JSON 的令牌不满足数据保护要求。Service 不接受旧明文签名令牌的兼容解析。

当前不能提供完整 PreparedQuery 读取闭包与输出血缘的联邦查询、图查询，以及尚未建立独立 `service_publish` 或其他动作执行器的数据查询辅助、查询样例和瓦片路径，继续使用资源级失效关闭，不得借用 `service_execute` 或 Manager 规则开放。

### 10.3 Transfer `export` 动作

`export` 是 Transfer Owner 对“受保护数据离开源 DataItem”的稳定动作，不是 Transfer `task_type`。Transfer 任务类型仍唯一使用 `sync`，不得因数据安全恢复 `export` 任务类型或第二条执行路线。

字段级 `export` 首先覆盖两条 `runtime.boundary=bounded + load.mode=snapshot` 主路径：PostgreSQL 结构化 TablePipeline，以及 MongoDB collection 到 `mongodb_extended_jsonl` 的原始记录格式导出。PostgreSQL 原生表源按服务端已解析的精确 ResourceLocator 与当前表结构校验投影；只读查询源必须从同一 PreparedQuery 取得 `ReadSet()` 和 `OutputLineage()`。MongoDB 原始记录导出按精确 collection Locator 与 Meta 当前字段结构校验投影，在 Provider 保留 BSON 标量类型的文档对象上执行规则，随后才生成 Canonical Extended JSON；Transfer 不解析已经编码的 EJSON，也不把预览友好值作为导出输入。保护结果必须位于用户字段映射、类型转换、空间处理、目标 writer 和文件格式序列化之前。`mask` 只修改当次批次副本；`suppress` 必须同时移除值和对外字段结构；执行记录、进度、错误和血缘不得记录原值。Security 的 `export` 投影保持引擎无关，是否存在可执行的字段级导出路径由 Transfer Owner 判定。

未纳管租户或未命中纳管目标的 bounded 任务继续原路径；不解析字段，不调用 Security 或 Meta，不写保护审计。命中纳管目标后，Projection 非 active、动作缺失、schema 漂移、查询血缘不完整或 derived 敏感输出都拒绝整个执行。

除上述 MongoDB collection 原始记录格式导出外，其他非 PostgreSQL 源、raw copy、watermark incremental、Kafka bounded replay、continuous/CDC 与从 encoded 内容作为源的路径，在各自建立并验证可证明的字段身份、结构新鲜度和执行屏障前继续使用资源级失效关闭，不得借用已有执行器开放。MongoDB 投影非 active、缺少 `export` 动作、Meta 结构缺失或漂移、组件路径不匹配、值保护失败时拒绝整个导出，不得降级为未保护 EJSON。

## 十一、Security API 契约

公开管理 API 使用 User Access Token、当前 Tenant AuthContext、精确 Permission 和资源版本。首期唯一资源路径：

| 方法 | 路径 | 作用 |
| --- | --- | --- |
| `GET/POST` | `/sensitive-data-types` | 列表/创建敏感数据类型 |
| `GET/PUT/DELETE` | `/sensitive-data-types/{id}` | 详情/完整更新/删除 |
| `GET/POST` | `/classifications` | 安全分类管理 |
| `GET/PUT/DELETE` | `/classifications/{id}` | 分类详情/完整更新/删除 |
| `GET/POST` | `/grades` | 安全等级管理 |
| `GET/PUT/DELETE` | `/grades/{id}` | 等级详情/完整更新/删除 |
| `GET/POST` | `/detectors` | 检测器管理 |
| `GET/PUT/DELETE` | `/detectors/{id}` | 检测器详情/完整更新/删除 |
| `GET/POST` | `/protection-baselines` | 保护基线管理 |
| `GET/PUT/DELETE` | `/protection-baselines/{id}` | 基线详情/完整更新/删除；删除 body 必须携带 `version`，写入与受影响投影重编译保持原子 |
| `GET/POST` | `/protection-enrollments` | 纳管列表/创建；GET 使用 `scope=current|released|all` 服务端分页筛选，默认 `current` |
| `GET` | `/protection-enrollments/{id}` | 纳管详情与激活进度 |
| `POST` | `/protection-enrollments/{id}/releases` | 显式退出纳管，body 必须携带 `version` 和原因 |
| `POST` | `/protection-enrollments/{id}/discovery-executions` | 携带 `version` 显式创建一次有界重新发现执行；同一纳管同时至多一个 pending/running 执行 |
| `GET` | `/findings` | 敏感发现分页查询；支持按 Enrollment 与来源快照精确过滤，并返回可选初审记录 |
| `GET` | `/findings/{id}` | 不含原值的证据详情与可选初审记录 |
| `POST` | `/findings/{id}/reviews` | 确认、调整或驳回 Finding |
| `GET` | `/assessments` | 正式资源安全评估列表 |
| `GET` | `/assessments/{id}` | 评估详情和修订历史 |
| `POST` | `/assessments/{id}/revisions` | 在同一评估聚合上形成新的正式修订 |
| `GET/POST` | `/protection-policies` | 保护策略列表/创建；创建产生首个不可变修订 |
| `GET/PUT/DELETE` | `/protection-policies/{id}` | 策略详情/完整更新/撤销；更新和撤销均携带 `version` 并追加不可变修订 |
| `GET` | `/protection-projections` | 面向治理人员的投影状态和 Owner 确认进度，不返回原值 |

所有分页管理列表使用 `{data,total,page,page_size,total_pages}`，`page_size` 最大 100。创建返回 `201`，完整更新返回新资源。写入请求使用具体 DTO、snake_case 字段和必填 `version`，不接受旧 ID、旧字段、兼容 query 或 `map[string]interface{}` 隐藏契约。

Runtime API 只接受 Tenant Service Access Token，并同时校验固定 OAuth Client 和 Security-owned Runtime Permission。Catalog 页面展示 Security 摘要时，使用当前 User Access Token 直接调用 Security 权限感知摘要 API；Catalog Backend 不代理、不复制安全事实。具体联邦摘要端点与 Catalog 首个展示切片一次实现，不预建无消费者 API。

## 十二、Permission、认证与 Swagger

Security 是以下 Permission 词族的唯一 owner：

```text
security.sensitive_data_type.*
security.classification.*
security.grade.*
security.detector.*
security.protection_baseline.*
security.enrollment.*
security.finding.read
security.finding.update
security.assessment.read
security.assessment.update
security.policy.read
security.policy.create
security.policy.update
security.policy.delete
security.protection_projection.read
security.protection_projection.update
security.audit.read
```

实施时必须在 `security/authorization/permissions.yaml` 声明精确 Permission，不能在 Handler、Swagger 或前端中发明未登记字符串。Runtime `read|update` Permission 只授予固定参与 Owner 的 Service Principal，不进入 Tenant 自定义 Role 选项。`update` 只允许写入当前固定消费 Owner 的单调 acknowledgement checkpoint，不表示可编辑 Projection。

Security 管理 API 只使用 canonical Bearer Tenant AuthContext；不接受 Internal API Key、`X-Tenant-ID`、调用方提交 Principal 或 Cookie 认证。Runtime API 使用 `addp-manager`、`addp-transfer`、`addp-develop`、`addp-service` 等各自 Tenant Service Access Token，Security 从 AuthContext 中识别固定 Client 和 Tenant。

所有公开 API 必须使用中英双语 Swagger 注解，声明 `x-addp-auth-mode`、`x-addp-required-permissions`、完整请求/响应 DTO 与 `400|401|403|404|409|500|503` 中实际可能状态。Runtime 调用方必须依据 HTTP 状态和稳定 `error_code` 分支，不解析本地化 `error` 文案。实施后必须通过：

```bash
bash scripts/swagger/gen-swagger.sh security
bash scripts/swagger/check-route-coverage.sh security
cd common && go run ./authorization/cmd/manifest --coverage-report --repository-root ..
```

## 十三、共享包边界

| 路径 | 唯一职责 |
| --- | --- |
| `common/secretcipher` | 敏感配置值 AES-256-GCM 加解密；由现有 `common/security` 直接重命名，不保留转发包 |
| `common/dataprotection` | ProtectionProjection 值对象、严格校验、checksum、路径遍历和确定性保护算法 |
| `common/client/security.go` | Security Bearer-only Client、变化流和 acknowledgement 调用；不实现业务决策 |

`common/dataprotection` 首期只开放 `addp.mask.keep_prefix_suffix/v1`、抑制和拒绝执行语义。算法按 Unicode rune 计数；参数非法、值长度不足或不符合投影已确认值类型时执行 `invalid_value_effect`，不返回原值。

`addp.mask.keep_prefix_suffix/v1` 不接受宽松或未知参数，固定要求 `prefix_runes`、`suffix_runes`、`replacement`、`exact_runes` 和 `character_class`。首期 `character_class` 只允许 `ascii_digit`；手机号投影固定 `exact_runes=11`。长度不等于 11、包含非 ASCII 数字或类型不是 string 都是 invalid value，必须执行 `invalid_value_effect`。

## 十四、Outdoor 手机号首个纵向切片

目标引用：

```json
{
  "owner_module": "meta",
  "resource_type": "data_item",
  "resource_identity": "<Outdoor.Persons fingerprint>",
  "component_key": "userInfo.phone"
}
```

唯一执行主线：

1. Meta 扫描 `Outdoor.Persons`，保存 DataItem fingerprint、`userInfo.phone` 字段路径和结构事实。
2. Catalog 可并行自动建档，但不参与保护激活。
3. 用户创建 Security Enrollment；Manager、Transfer、Develop、Service 先安装资源级门禁。
4. Security Worker 用字段事实和必要的受控样本生成 Finding。
5. 高置信度手机号 Finding 按基线编译 `mask`；正式人工确认后改由 Assessment 修订支撑同一投影。
6. 首个切片只把 Manager Projection 升级为 `active`；其余必要 Owner 继续保留资源级 `enrolling` deny，因此 Enrollment 整体仍为 `enrolling`。Manager 后台拉取并原子安装投影，预览在 Provider 读取原始行之后、HTTP 序列化之前递归处理嵌套字段。
7. `13661384499` 返回 `136****4499`；非 11 位 ASCII 数字手机号候选执行 `suppress`，不返回原值。
8. 前端、搜索索引、剖析样例、日志、错误和 AI 上下文都不得包含原始手机号。

Manager 不把保护逻辑放入 MongoDB 专用 PreviewProvider，也不盲目放入会被剖析内部复用的原始 PreviewResolver。执行点必须位于 Manager 面向外部的服务端响应边界；剖析等内部读取可处理原始样本，但其持久化结果和对外返回仍必须执行投影。

Manager `/preview` 是 Locator 型出口：`PreviewResolver` 已从服务端校验的 ResourceLocator 和 Meta item 得到 DataItem fingerprint，因此该路径直接查本地纳管索引，不调用 `PreparedQuery.ReadSet()`。`PreparedQuery.ReadSet()` 只用于无法由 Locator、任务绑定或发布快照确定完整资源集合的自由查询出口。

Manager 必须按已实现的动作执行器逐项开放已纳管 DataItem，不得把 `preview` 规则猜测或复用为其他出口规则：

- 首个纵向切片已实现 `preview` 服务端递归字段保护；
- `profile` 已实现独立统计结果保护执行器和历史结果清理屏障：`suppress` 删除敏感组件及祖先容器剖析，`deny` 拒绝动作；已纳管目标首期不接受条件剖析；
- Manager 内容检索写入契约必须显式区分 `technical_metadata` 与 `extracted_content`，不得靠 `data_item_type`、字段是否为空或调用方约定推断。`technical_metadata` 只允许 DataItem 身份、名称、路径、类型、结构、字段定义和规模等 Meta 技术事实，不得携带数据行、字段值、文件正文、正文预览或正文派生属性；它不是 Security `search_index` 数据出口动作。
- `extracted_content` 包含文件正文、正文预览或正文派生的标题、作者、关键词和 Metadata，属于 Security `search_index` 数据出口。未纳管 DataItem 在一次本地索引未命中后沿原路径写入；已纳管 DataItem 必须命中本地有效 `search_index` 规则和独立执行器，`enrolling`、投影损坏、规则缺失或执行器缺失都拒绝写入，不得降级为原文索引。
- 任一 DataItem 首次进入纳管、投影修订或 release 时，Manager 必须在确认变化游标前幂等清除该 DataItem 的既有全文索引记录，防止纳管前原文继续可检索。Meilisearch 是外部可重建投影，清除动作先于本地投影与 cursor 事务提交；清除失败时事务回滚且不 acknowledgement，清除成功后即使数据库事务失败也只造成安全的暂时缺索引，重试继续收敛。
- 结构化表/集合当前只生成 `technical_metadata`，不得采样数据行或行值；文件/文档正文的敏感发现与 `search_index` 保护执行器未闭环前，不得将其声明为已纳管的结构化手机号切片能力。

文件/文档正文发现使用与技术事实分离的受控样本契约：

- `DataItemSecurityFacts` 继续是无数据值的 Meta 技术事实，不得塞入正文或预览；
- 有字段的结构化 DataItem 必须在 `DataItemSecurityFacts.source_snapshot_hash` 中返回规范表结构哈希；无字段的文档技术事实必须使用空哈希，不伪造空表结构。该空值不得用于编译 active Projection，文档 active Projection 的快照哈希只能来自后续受控正文样本；
- 只有显式 Enrollment 的 Security Worker 可以按确定 fingerprint 调用 Meta runtime sample，Meta 以 owner 身份临时打开源文档并按固定上限抽取文本，不从已持久化的 `plain_text_preview` 回读；
- 原始样本文本只存在于当前内存调用链，不写入 Meta attributes、Finding、Assessment、execution metadata、日志或错误；Finding 只保存组件、文本快照哈希、匹配规则和命中计数；
- 文本快照哈希由规范化的抽取文本和 `truncated` 标记计算，Manager 对收到的 `extracted_content` 重新计算同一哈希后才执行 `search_index` 规则，避免拿旧规则处理新正文；
- 首期文档组件固定为虚拟标量 `$document.text`。它不是 Engine 字段路径，只用于把全文索引载荷作为一个保护边界；执行器必须覆盖正文、预览、标题、作者、关键词、标签、描述及 Metadata 中的所有字符串，不能只改 `content` 后让高亮或派生属性泄露。

已纳管判定只查 Manager 本地投影存储。未纳管 DataItem 在一次本地 map miss 后继续原剖析或搜索路径，不调用 Security、Meta 或 Catalog；已纳管动作缺少对应规则或 Owner 执行器时必须拒绝，不得回退到明文。

## 十五、迁移与旧路径删除

首次实施必须在同一变更中：

- 从 Standard 删除 Classification、GradingLevel、Element Revision `classification_id/security_level`、API、Permission、前端、Swagger、i18n、测试和 Catalog/数据字典引用；
- 在 Security 创建新的 SecurityClassification、SecurityGrade 和 ProtectionBaseline；
- 将 `common/security` 直接重命名为 `common/secretcipher`，修改所有真实消费者；
- 不保留旧 ID 映射、历史数据迁移、双写、兼容字段、兼容 API、兼容 query、转发包或回退到 Standard 的路径；
- 开发/测试数据使用 Security 新事实重新创建，不把旧 Standard ID 带入新 Schema。

## 十六、测试与验收

实施前必须把 Security Backend、Worker、Frontend、PostgreSQL 门禁、Swagger 和 Dockerfile 纳入根 `Makefile`、`make test-platform`、`make test-module MODULE=security`、`make test-changed` 与 `.github/workflows/` 路径选择。不能等 CI 失败后再补登记。

最小验收包括：

1. 未纳管资源不命中本地索引，不调用 Security，预览行为与变更前一致。
2. 纳管创建后在必要 Owner 门禁确认前保持 `activating`，不伪造已生效。
3. `enrolling` 状态的相关数据动作拒绝且不返回明文。
4. Catalog 尚未建档时，有效 Manager 投影仍能返回 `136****4499`。
5. 嵌套 object 和 array 中的手机号按类型化路径递归处理。
6. 非空但格式无效的手机号候选执行 `suppress`，不返回原值。
7. 投影缺失、损坏、过期或结构指纹不匹配时拒绝已纳管动作。
8. Owner 只在原子保存变化与 cursor 后发送 acknowledgement；重启可从 checkpoint 恢复。
9. 退出纳管在所有必要 Owner 确认 release 前继续执行最后有效保护。
10. 样本、原始敏感值、Token、凭据和原始请求体不进入日志、错误、Finding、execution metadata 或审计。
11. 管理 API 的并发版本、Tenant 隔离、Permission、Runtime 固定 Client Guard、Swagger 路由覆盖和 IAM 授权覆盖均通过。
12. 已删除的 Standard 安全分类分级和 `common/security` import 在代码、Swagger、i18n、测试和文档中无残留。
13. ProtectionPolicy 不能降低基线；创建、更新和撤销均保留不可变修订，撤销后投影回落到基线而不是明文。
14. 显式重新发现拒绝同一纳管的并发 pending/running 执行；结构快照变化后发布携带最新 Hash 的新投影，组件结构未变化的正式 Assessment 可以续用，组件结构冲突时保守保护。
15. ProtectionBaseline 变化按旧、新绑定精准重编译且与定义写入原子提交；SensitiveDataType 默认等级或阈值变化只影响未复核候选，正式 Assessment 不漂移；无关 Enrollment 不产生投影新版本，编译失败时定义写入回滚。

代码实施后的标准本地入口至少为：

```bash
make test-module MODULE=security
make test-platform
make test-changed
```

## 十七、后续范围

以下内容不进入首个手机号纵向切片，需要时先修订本规范：

- 原值揭示、用途约束、限时例外和双人审批；
- 行过滤和单元格级任意策略 DSL；
- 可逆令牌化、假名化、静态脱敏产物和匿名化评估；
- 文档文本、图像、人脸、车牌、媒体和图数据敏感发现；
- 自动定时发现任务、TaskProvider 和 Orchestrator 编排；
- 把 Security 摘要写入 Catalog 搜索投影。
