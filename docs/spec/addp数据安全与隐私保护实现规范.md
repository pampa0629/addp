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

ProtectionEnrollment 固定纳管整个 DataItem，不接受 `component_key`。字段或文档组件可以由 Detector 在发现执行中产生 Finding，也可以由治理人员从 Security 服务端实时读取并校验的 Meta 当前字段清单中选择后直接形成正式 Assessment；两者都不得接受自由文本字段路径，也不得形成 DataItem 级、字段级两条纳管路线。

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
| SensitiveDataType | 定义敏感数据类型、所属安全分类和自动发现后的初始安全等级 | 可变聚合根，使用 `version`；不保存识别阈值 |
| SecurityClassification | 定义安全类别 | 可变聚合根，使用 `version` |
| SecurityGrade | 定义等级、顺序和最低控制强度 | 可变聚合根，使用 `version` |
| DetectorCapability | 由平台可信代码定义结构/内容识别算法、证据来源、适用资源类型、实现方法、隐私边界、已知局限和能力版本 | 平台级只读注册表；能力键在一个平台版本内不可变 |
| Detector | 将一个已安装 DetectorCapability 绑定到一个 SensitiveDataType，并控制是否参与发现及自动采用置信度 | Tenant 可变聚合根，使用 `version`；不保存或执行租户提交的代码、SQL、脚本或任意正则 |
| ProtectionBaseline | 定义 SensitiveDataType + SecurityGrade + action 的最低保护意图 | 可变聚合根，发布修订不可变 |
| ProtectionEnrollment | 把一个专业资源显式纳入 Security 生命周期 | 可变聚合根，使用 `version` |
| SensitiveFinding | 保存自动发现候选、置信度和非原值证据 | 不可变观测记录；复核结果单独保存；查询必须提供完整规则解释 |
| ResourceSecurityAssessment | 保存正式分类、等级、敏感类型、依据和 dependency snapshot | 可变聚合根；Finding 复核、人工指定、调整或撤销均产生不可变修订 |
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
| `active` | 有效 Policy 已编译，且当前投影被必要 Owner 持久安装并确认 | 具体请求使用本地有效投影执行；当前引擎或数据形态无法证明安全执行时保守拒绝 |
| `releasing` | 发布明确 release 变化并等待确认 | 继续使用最后有效保护，不因找不到新策略自动解除 |
| `released` | 保留历史和审计 | 只在已原子安装 release 后删除本地纳管索引 |

创建 Enrollment 返回 HTTP `201` 和 `state=activating`，不得返回假的“已生效”。Security 必须在必要 Owner 对门禁版本完成确认后才进入 `enrolling`。第一阶段结构化 DataItem 必要 Owner 固定为 Manager、Transfer、Develop 和 Service；尚未能执行字段保护的 Owner 必须安装资源级 `deny` 门禁，不得被从激活屏障中忽略。

已退出记录上的“重新纳入保护”只是创建新 ProtectionEnrollment 的便捷入口，不是状态回退。请求必须携带已退出 Enrollment 的正整数 `version`；Security 在同一事务锁定并验证来源记录仍为 `released`，使用其冻结的目标引用和最小目标快照创建新的 `activating` Enrollment 与四个 Owner 的初始门禁。旧 Enrollment、退出原因、退出依据和时间保持只读。若同一目标已有未退出 Enrollment，返回 `409`，不得产生第二条活动生命周期。

Enrollment 查询响应必须分别返回每个 Owner 当前投影的 `projection_state`、该版本的 `acknowledged` 状态，以及从当前投影去重、稳定排序得到的 `rules[{action,effect}]`；旧的纯 `effects` 汇总字段删除，不保留双轨。确认只表示 Owner 已持久安装当前 revision，不能在 UI 中被解释为某个具体请求已经执行成功，也不能扩大为该 Owner 的所有引擎和数据形态均支持字段级处理。产品界面必须区分“字段规则已安装”“保守拒绝已安装”“等待安装”和“已解除”；遮盖、抑制等 effect 只能连同 action 表述为当前投影要求。具体请求无法满足动作、结构、血缘或执行器约束时仍须失效关闭。

退出纳管只使用同一个 Release 子资源路径。Release 请求除 Enrollment `version` 和必填原因外，必须携带稳定依据 `basis=manual|no_supported_findings`；Enrollment 必须冻结 `release_basis`、`release_requested_by`、`release_requested_at` 和当次退出依据的 `release_source_snapshot_hash`。`no_supported_findings` 只允许在最近一次发现已成功、当前快照 Finding 数为 0，且同一 Enrollment 不存在 pending/running 发现 execution 时提交；服务端在同一退出事务内校验，不信任前端判断。

## 六、敏感发现执行

Detector 执行采用唯一的“能力注册 + 租户绑定”路径：

1. `DetectorCapability` 由 Security 代码注册，稳定键同时包含能力名和版本，例如 `addp.detector.phone_metadata/v2`；注册信息公开描述目标形态、证据来源、适用 DataItem/字段类型、实际识别方法、隐私边界和已知局限，但不可由租户修改。前端必须把这些信息作为只读能力说明展示，不能只显示一个无法判断含义的能力名称。
2. `Detector` 必须精确引用一个已安装能力和当前 Tenant 下的一个 SensitiveDataType；同一 Tenant 对同一能力最多一个绑定。每个绑定必须配置 `confidence_threshold`，表示该能力的 Finding 可在人工复核前自动采用并触发保守保护的最低置信度；该阈值属于“能力如何识别”，不得放在 SensitiveDataType 或 ProtectionBaseline。删除或停用绑定后，该能力不再参与后续发现。
3. Worker 只加载当前 Tenant 已启用的 Detector，再按 Capability 的适用范围执行。检测结果的 `sensitive_data_type_id` 来自 Detector 绑定，不得通过 `SensitiveDataType.code`、名称或其他约定猜测。
4. Detector 创建、改绑、启停或删除后，Security 为当前 Tenant 所有 `enrolling|active` ProtectionEnrollment 创建有界重新发现 execution；已有 pending execution 时由该执行读取提交后的最新绑定，不重复入队。若任一受影响发现已经 running，配置写入返回 `409`，避免运行中的旧配置在新配置提交后覆盖当前结果；用户应等待该次发现结束后重试。变更不触发 Meta 全量扫描，也不给未纳管资源增加发现负担。
5. 平台新增检测算法必须新增版本化 Capability 并补充证据无原值、适用范围和确定性测试；不得在数据库中保存可执行的租户表达式。

检测能力按目标资源形态独立选择和运行，不组成隐式串行流水线。例如手机号首期两项能力的关系固定为：

- `addp.detector.phone_metadata/v2` 只处理表和集合的字符串字段。有 Meta 结构化字段路径时取路径末级名称；只有物理字段名时，先按 ADDP 确定性内部扁平路径分隔符 `__` 取语义末级名称。末级名称去除下划线、连字符和空格并转为小写后，必须与平台内置手机号别名集合精确匹配。识别得到的 Finding `component_key` 始终保留 Meta 发布的真实物理字段键，不把扁平列改写成嵌套组件。该能力不读取或校验结构化字段中的业务值。
- `addp.detector.phone_document/v1` 只处理文件的受控文本样本，查找独立的连续 11 位 ASCII 数字候选；它不参与表或集合字段发现，也不作为元数据检测后的“二次确认”。
- 两项能力都只产生 Finding。遮盖、抑制或拒绝由 ProtectionBaseline、唯一策略编译器和数据出口 Owner 执行；检测器不直接改写或遮盖原始数据。

结构化邮箱首期使用独立的 `addp.detector.email_metadata/v1` 能力。它与手机号字段元数据能力共享“结构化路径末级名称或 `__` 扁平路径语义末级名称”的确定性取值规则，但只在字符串字段的规范化末级名称与 `email`、`emailaddress`、`邮箱`、`电子邮箱` 精确匹配时产生邮箱 Finding。Finding 保留 Meta 发布的真实物理 `component_key`，不读取或验证邮箱业务值；邮箱格式或内容采样不是该能力的后续串行步骤。租户必须把该能力显式绑定到邮箱 SensitiveDataType，并为对应初始保护等级配置 ProtectionBaseline，识别结果才可能形成字段级保护。

1. Security 只为已进入 `enrolling` 的显式 Enrollment 创建发现 execution。
2. Worker 使用 `addp-security` Tenant Service Access Token 精确读取 owner 已授权技术事实，不订阅 Meta 全量 DataItem 变化。
3. Detector 先使用字段路径、名称、注释、类型和结构；证据不足时才受控采样。
4. 受控采样必须使用统一 Engine Provider / content reader 和绑定本次 execution、Tenant、目标资源、`read` 效果及有效期的 Execution Authorization。
5. `addp-security` Service Principal 不获得 Tenant 全量数据读取权；创建 Enrollment 的当前 User 无权读取目标时，只允许元数据检测，不得使用 Security 管理权绕过 owner 数据授权。
6. 原始样本只存在于当前 Worker 有界处理内存，不写入 Finding、execution metadata、日志、错误或审计。
7. Finding 证据只保存命中规则、置信度、样本数量、格式符合数、检测器版本和不可逆证据摘要。结构化字段名识别还应保存不含业务值的语义末级名称、规范化名称和实际命中别名；Finding 查询必须同时返回 Capability 的适用范围、完整识别方法、隐私边界和已知局限，供用户逐项核实原因。
8. Worker 使用通用 execution lease；租约过期且未达到 `max_attempts` 时原执行回到 `pending`，达到上限时以不含原值的稳定错误码失败，不允许异常退出后永久停留在 `running`。
9. Enrollment 查询必须返回最近一次成功发现的摘要：`status=not_completed|completed`、该快照的 `finding_count`、`pending_review_count` 和 `reviewed_count`。列表查询必须通过 Finding 与不可变初审记录批量聚合，不得为每个 Enrollment 新增 Finding 或 review 查询。
10. `completed + finding_count=0` 只表示当前已启用检测能力对该次快照零命中，不是“资源已被证明不含敏感数据”的 Assessment。Enrollment 继续保持 `enrolling` 和资源级 `deny`，不得自动编译 `allow`。
11. 治理人员可以基于零命中摘要显式确认“当前无需保护”，但该确认的唯一效果是使用 `basis=no_supported_findings` 创建 Release 并进入 `releasing`；不创建空 Assessment、`allow` Policy 或第二条放行路径。
12. 识别质量摘要必须直接聚合现有事实，不新增统计表或异步双写。`current_finding_count` 与 `awaiting_review_count` 只取状态非 `released` Enrollment 的 `latest_discovery_execution_id + latest_source_snapshot_hash`；人工复核质量样本按 `{enrollment_id, component_key, detector_version}` 只取最新 review，分别统计 `confirm|adjust|reject`。确认属于敏感数据的比率为 `(confirm + adjust) / reviewed_sample_count`，分母为零时返回 `null`，不得显示伪造的 `0%`。
13. 来源为 `manual` 的 Assessment 当前修订分别统计 `sensitive` 与 `not_sensitive`，只表达当前人工补充及已撤销数量。该数据可以按 SensitiveDataType 过滤，但不得分摊到 Detector、宣称为已证明漏检或据此自动修改检测配置。

Meta 专用技术事实读取契约固定为 `GET /api/v1/meta/runtime/data-items/{fingerprint}/security-facts`，由 Meta owner 使用不可租户定制、不可委派的精确 Permission `meta.security_facts.read` 和固定 `addp-security` Client Guard 保护。Tenant 只能来自 Tenant Service Access Token，端点不接受客户端提交 Tenant ID，也不返回完整 attributes、连接信息或原始样本值。响应 Schema 固定为 `addp.data_item_security_facts/v1`，只包含 DataItem fingerprint、item type、字段路径/名称/注释/通用类型、Meta 观测时间以及由 `common/dataprotection.TableSchemaSnapshotHash` 计算的结构快照 Hash。Security 必须按 Enrollment 的精确 fingerprint 单项读取，不得把该端点扩展为全量列表或变化订阅。

## 七、Finding、Assessment 与唯一策略编译器

- Finding 只是候选证据，不能自动成为正式 Assessment。
- Detector 绑定定义自动采用置信度。Finding 达到产生该 Finding 的当前有效绑定阈值，且 SensitiveDataType 默认等级存在有效 ProtectionBaseline 时，编译器允许在人工确认前生成保守临时决策。
- Finding 不达阈值或结构证据不足时，Enrollment 保持 `enrolling`，Owner 继续执行资源级 `deny`。
- 安全治理人员可以确认、调整或驳回 Finding。确认或调整形成 Assessment 修订；误报驳回保留 Finding 和复核记录，不删除历史证据。
- 治理界面只把 Enrollment 最近一次成功发现的 `latest_discovery_execution_id` 作为当前可操作候选集，并同时保留该次事实来源的 `latest_source_snapshot_hash`；相同 Meta 快照在检测器配置变化后产生新的发现执行，旧 Finding 继续保留用于审计但不得混入当前待办或投影编译。`GET /findings` 必须支持按 `enrollment_id + source_snapshot_hash + discovery_execution_id` 精确过滤，并在同一分页响应中返回可选的不可变 `review`，避免前端逐条查询。
- 集中“待复核候选”不新增 Queue 或 Task 持久化实体，只使用 `GET /findings?snapshot_scope=current&review_state=pending` 读取未退出 Enrollment 最新成功发现中尚无初审记录的 Finding；该查询可继续按 `sensitive_data_type_id` 和精确 `detector_version` 筛选，并在每条响应中附带不含资源指纹和原始值的 Enrollment 目标快照，供治理界面定位资源。`snapshot_scope` 仅允许 `all|current`，`review_state` 仅允许 `all|pending|reviewed`，默认均为 `all` 且省略；复核写入后候选依事实自然退出视图，前端不得另存队列状态。
- 一个 Finding 只允许形成一次不可变初审记录；重复初审返回 `409`。初审后的治理调整必须在既有 Assessment 上新增 revision，不得改写 Finding、review 或历史 Assessment revision。
- `confirm` 继承 Finding 的 SensitiveDataType 及其默认等级；`adjust` 必须显式给出目标 SensitiveDataType 和 SecurityGrade；二者都在同一 `{tenant, enrollment, component_key}` Assessment 聚合上追加 revision。`reject` 只形成 review，不创建 Assessment。
- 自动发现漏检时，治理人员可以在既有 Enrollment 上人工指定敏感组件。组件候选必须由 Security Backend 使用 `addp-security` Tenant Service Access Token 精确读取 Meta security facts，并只返回当前尚未形成任何正式 Assessment 的组件；已经确认、调整或撤销过的组件必须在既有 Assessment 上继续治理，不得重新列入“遗漏字段”候选。创建命令只提交所选 `component_key`、Enrollment `version`、SensitiveDataType、SecurityGrade 和原因；服务端必须重新读取并校验当前组件、结构指纹和 Tenant，不信任浏览器提交组件结构。
- Assessment 聚合只保存当前 revision 指针、资源并发 `version` 和审计字段；每个不可变 revision 通过 `source_kind=finding|manual` 区分来源，通过 `conclusion=sensitive|not_sensitive` 表达当前正式结论，并冻结可选 Finding/review、SensitiveDataType、SecurityClassification、SecurityGrade、来源结构快照 Hash 和已确认组件结构。该依赖快照不得包含原始样本值。
- 治理人员发现既有正式 Assessment 错误时，必须携带 Assessment `version` 和原因追加 `conclusion=not_sensitive` 修订；不得删除或改写 Assessment、Finding、review 或历史 revision。后续重新认定为敏感时仍在同一 Assessment 聚合追加 `sensitive` 修订。
- 编译器合并有效 Assessment、ProtectionBaseline 和 ProtectionPolicy，对同一资源、组件、消费 Owner 和动作始终选择更严格结果。
- 候选基线与正式 Assessment 都必须进入同一编译器和同一投影变化流；不允许 Owner 实现“自动发现脱敏”与“正式策略脱敏”两条路线。
- 当 Finding 被驳回或正式 Assessment 被撤销，且同一组件不存在其他有效正式结论时，唯一编译器必须移除该字段规则；若整个资源不再有字段规则，则发布新的 `enrolling` 资源级拒绝投影。不得通过删除投影使 Owner 回到明文路径。

Finding 查询响应必须同时提供只读的 `explanation`，把已经存在的控制面事实组织成一条可审计解释链，但不得创建第二份业务事实或由前端重新推导保护决策：

1. `capability` 说明产生该 Finding 的平台检测能力，`automatic_adoption_threshold` 取当前 Tenant 的 Detector 绑定；Finding 的置信度仍以不可变观测值为准。
2. `decision_state` 只允许 `automatic|formal|awaiting_review|detector_inactive|baseline_missing|rejected|revoked|superseded`。`automatic` 表示当前 Finding 达到绑定阈值并命中有效 ProtectionBaseline；`formal` 表示当前组件由有效 `sensitive` Assessment revision 支撑；`revoked` 表示该组件当前正式结论已撤销为 `not_sensitive`；`superseded` 表示该历史 Finding 的确认结果已不再是当前组件的有效 Assessment revision；其余状态必须明确说明为什么没有形成字段级规则。`governance_source=detector_default|assessment` 在基线缺失时仍保留候选结论的来源。
3. `effective_*_id`、`assessment_id` 和 `baseline` 必须由 Security 后端按唯一编译器使用的同一候选选择规则组装；前端不得根据名称、默认值或列表数据自行猜测。
4. `outlets` 必须读取当前已发布 Projection，并按组件精确列出每个 Owner 的 `projection_state`、安装确认状态以及投影中的 action/effect/algorithm。它描述当前控制面真实产物，而不是根据 ProtectionBaseline 预测的结果，也不是具体数据请求的执行记录；资源级 `enrolling` 拒绝没有字段规则时返回空 `rules`。
5. `explanation` 是查询期只读组合结果，不持久化、不进入 Projection checksum，也不包含原始敏感值、样本、连接信息或完整投影载荷。列表查询必须批量装配，禁止逐 Finding 发起数据库查询。

同一个 Enrollment 的不同 Owner Projection 允许按各自已实现的执行能力分阶段从 `enrolling` 升级为 `active`。当前 Manager 的 `preview|profile`、Develop 的受支持结构化 `query`、Service 已发布 QueryService 的 `service_execute`，以及 Transfer bounded snapshot 中统一 Native TablePipeline 结构化导出、PostgreSQL 可证明血缘的查询导出和 MongoDB collection 到 `mongodb_extended_jsonl` 的 `export`，均可安装并确认字段级 `active` Projection；未实现执行器的查询、服务或传输形态仍保持资源级 `enrolling` deny。只有全部必要 Owner 都已安装并确认当前 `active` Projection，Enrollment 才进入 `active`。不得用“部分 Owner 已 active”伪装整体保护闭环已完成，也不得把某个 Owner 的已支持路径扩大解释为该模块所有输出路径都已完成适配。

第一阶段保护效果严格度固定为：

```text
deny > suppress > mask > allow
```

`allow` 只表示当前 Security 保护决策不额外变换内容，不代表 owner 已授权资源动作。敏感组件只有在有效 ProtectionExemption 被编译为限时覆盖时才能执行 `allow`，不得从 ProtectionBaseline、ProtectionPolicy 或 Owner 私有配置产生 `allow`。

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

用途约束和双人审批仍属于后续范围，不得借 ProtectionPolicy 或 ProtectionExemption API 伪装实现。

### 7.2 ProtectionExemption 首期语义

ProtectionExemption 是原值揭示的唯一控制面入口，不是 IAM 授权、ProtectionPolicy 的宽松效果或 Owner 本地开关。其绑定键固定为：

```text
tenant + assessment_id + consumer_owner + action
```

首期约束固定为：

1. 只能绑定当前结论为 `sensitive` 的正式 Assessment；每个 Exemption revision 必须冻结批准时的 Assessment revision，组件路径、类型、分类和等级继续取该不可变修订，不接受自由文本字段路径。Assessment 后续产生任何新修订时，旧豁免立即失效且不得在结论再次变化后静默恢复；需要原值时必须基于新修订重新批准。
2. 只开放已经具备字段级结果保护执行器的 `manager/preview`、`develop/query`、`service/service_execute` 和 `transfer/export`；不得豁免资源级 `enrolling` 门禁、结构冲突、血缘不明、无执行器或 Owner 授权拒绝。
3. 豁免效果固定为 `allow`，API 不接受调用方选择效果或遮盖算法。它只取消该组件在该出口动作上的 Security 内容变换，不增加 Owner Permission、Resource Grant 或数据访问范围。
4. 首期投影没有请求主体维度，因此一个豁免对当前 Tenant 内所有已通过 Owner 授权的该动作调用者生效。API 与界面必须明确展示此范围；不得按用户名、角色名称或客户端名称硬编码选择性放行。
5. `expires_at` 必填，必须晚于创建或续期时间且不得超过 30 天；批准依据必填且最长 2000 字符。
6. 同一绑定至多有一个 Exemption 聚合。创建、续期和撤销都追加不可变 revision，并使用聚合 `version` 做并发控制；已过期聚合可以续期，历史不得改写或物理删除。
7. 有效性由当前 revision 的 `state=active`、`expires_at` 以及冻结的 Assessment revision 仍为当前修订共同决定。到期不依赖 Security 在线通知：投影规则保留默认决策，并携带限时 `allow` 覆盖，Owner 在每次服务端执行时按本地时间选择；到期或 Assessment 被修订时立即回落到默认决策。
8. 编译优先级固定为 `ProtectionBaseline -> ProtectionPolicy 收紧 -> ProtectionExemption 限时覆盖`。豁免到期或撤销后回落到已经收紧后的结果，而不是越过 Policy 回落到更宽松基线。
9. Exemption 变更与 ProtectionProjection 新修订必须在同一事务内完成，并调用唯一编译器。Owner 只消费投影，不读取 Exemption 表、不保存审批依据，也不增加本地放行接口。

### 7.3 保护定义变化的影响传播

保护定义变更不能等待下一次人工复核或重新发现才偶然生效，也不能把每次变更扩大为全租户重编译。Security 必须按事实依赖精确定位受影响 Enrollment，并继续调用同一个投影编译器：

| 变化 | 受影响范围 | 编译行为 |
| --- | --- | --- |
| ProtectionBaseline 创建 | 当前 SensitiveDataType + SecurityGrade 已存在 Finding 或正式 Assessment 的 Enrollment | 新基线与对应新投影在同一事务生效 |
| ProtectionBaseline 完整更新、启停或改绑类型/等级 | 更新前绑定与更新后绑定范围的并集 | 基线新版本与全部受影响投影在同一事务生效 |
| ProtectionBaseline 删除 | 删除前绑定范围 | 必须携带资源 `version`；删除与受影响投影重编译在同一事务完成，无其他有效规则时回到 `enrolling` 资源级 deny |
| SensitiveDataType 自动发现初始等级更新 | 当前类型 Finding 所属 Enrollment | 只重新计算候选 Finding；正式 Assessment revision 已冻结类型、分类和等级，不随定义静默改写 |
| Detector 自动采用置信度更新 | 当前 Tenant 的 `enrolling|active` Enrollment | 与 Detector 改绑、启停使用同一有界重新发现路径；新 execution 读取提交后的唯一当前绑定，不改写历史 Finding |
| SecurityClassification、SecurityGrade 名称、描述、层级、排序或风险顺序更新 | 无执行影响 | 不发布空转投影；治理展示读取定义新版本，历史 Assessment revision 仍保留原引用 |

影响定位必须使用 Security 自有 Finding、Assessment current revision 和 Enrollment，不访问 Catalog，不扫描 Meta，不读取样本。每个受影响 Enrollment 在一次定义写事务中至多编译一次，并使用该 Enrollment 最近一次成功发现保存的 `latest_discovery_execution_id` 候选集及其 `latest_source_snapshot_hash`；缺少成功发现执行时不伪造 active 投影。

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
6. `effect` 只允许 `allow|mask|suppress|deny`；普通决策不得为 `allow`。原值揭示必须表示为带 `valid_until` 与 `fallback` 的限时决策，`fallback` 必须为更严格且不再嵌套限时覆盖的 `mask|suppress|deny`。未实现的 `filter` 和假名化不得填入投影。
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

`protection_projection_entries`、`protection_projection_checkpoints` 及其存储迁移元数据是统一的 Owner 本地投影存储契约，必须由 `common/dataprotection/projectionstore` 唯一定义和迁移。各 Owner 只选择自身 schema、固定 `consumer_owner` 和可选的本地变化屏障，不得复制 DDL、增加 Owner 私有列或维护独立迁移路线。存储初始化必须在数据库事务和 schema 级迁移锁内顺序执行公共迁移，记录已应用版本，并在载入投影前校验真实 PostgreSQL 列、类型、可空性、默认值、主键和必要索引。已存在表与当前契约不一致、迁移版本未知或迁移失败时，Owner 必须启动失败而不得进入 Ready。

新的数据出口 Owner 不得自行建表；必须复用同一 `projectionstore` 构造入口，并通过真实 PostgreSQL 同构门禁证明新 schema 与已有 Owner 一致。平台一致性门禁还必须拒绝 `common/dataprotection/projectionstore` 之外出现这些投影存储表的 Go/SQL 定义，防止新模块复制 DDL 形成第二条迁移路线。数据库存储迁移版本与 `addp.protection_projection/v1` 业务投影协议版本分开管理：前者保证本地持久结构收敛，后者保证 Security 与 Owner 对投影语义的一致理解。

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

第一阶段以 MongoDB 和 PostgreSQL 作为精确查询门禁与字段级 `query` 执行的验证范围。MongoDB PreparedQuery 的 ReadSet 必须覆盖主集合以及 `$lookup`、`$graphLookup`、嵌套 `$unionWith`；PostgreSQL PreparedQuery 的 ReadSet 必须按真实 `search_path` 解析关系并递归展开普通视图。PostgreSQL 普通标量、聚合和窗口函数只有在 Provider 根据当前连接目录证明全部可匹配候选均为不新增数据源的可信 `pg_catalog` 内置实现时才继续分析；该证明不使用函数名白名单，用户函数、扩展函数、表函数、集合返回函数和副作用边界不明的函数继续 fail-closed。结果血缘首期开放 PostgreSQL 直接列、显式别名和可证明的单来源 wildcard，以及 MongoDB `find`、`count`、`distinct` 和透明 aggregate；透明 aggregate 只允许 `$match`、`$unwind`、`$project`、`$sort`，投影只允许字段保留、排除、直接别名和不改变非空原值的 `$ifNull: ["$field", null]`。其他 MongoDB aggregate、PostgreSQL 派生敏感值、View 底层敏感值和无法消歧的多来源输出在命中纳管资源时拒绝，不以不完整遮盖冒充字段保护。Transfer、Develop、Service 等查询 Owner 必须统一消费这一 PreparedQuery 契约，不得按引擎类型建立“某引擎字段级保护、其他引擎资源级拒绝”的分支。其他尚不能证明完整读取闭包或结果血缘的 Provider 返回对应类型化 unresolved，只有该 Tenant 已存在纳管目标且当前出口需要判定时才拒绝，不影响完全未纳管 Tenant 的原路径。SQL PreparedQuery 的 `ReadOnly` 不能只做语句分类，还必须由 Provider 执行路径建立数据库只读事务；门禁通过后执行的仍是同一个 PreparedQuery。

通用执行规则：

- 未命中本地纳管索引时立即进入原有路径，不遍历字段、不调用 Security、不写保护审计；
- 同一个请求可以因 owner Resource Policy 拒绝，不得因 owner 授权降低 Security 效果；
- `mask` 只改变当前输出，不回写数据源、Meta 或 Catalog；
- `suppress` 不返回原值，结构化输出的缺失语义必须由对应 Owner 协议固定，不得用原值兜底；
- 处理完成的结果、日志、错误、搜索文档、前端状态和 AI 上下文都不得包含该决策下的原值；
- 审计只记录 Principal、Tenant、动作、目标、策略/投影版本、效果、结果和稳定原因码。

Manager 的 `downloads/file` 原始下载、`storage-stream` 单叶子内容流与 `storage-assets` 多文件预览子资源都可能绕过结构化 `preview` 执行器，因此必须先定位到服务端校验的 Meta DataItem，再使用 Manager 本地投影索引判定。这些出口尚无对原始字节流的字段级执行器：未纳管 DataItem 继续原路径，命中任何 `enrolling|active` 本地投影时必须拒绝整个请求，不得借用 `preview` 规则处理或放行原始文件。

`storage-stream` 必须同时接受 DataItem `locator` 和叶子 `storage_ref`：`locator` 是保护目标主身份，且必须包含 `item_id`；`storage_ref` 只表示该 DataItem 内实际读取的叶子。Manager 必须从 Meta 按 `item_id` 读取当前 item，校验 Tenant、Engine、item type 和 locator 路径，并按 `item.layout` 验证叶子归属：`single` 只允许主内容，`multi` 只允许 `item.refs`，`whole` 只允许 scope 边界内的子内容。不得接受调用方直传 fingerprint，也不得保留只依赖 `engine_id + storage_ref` 的无主身份路径。

`storage-assets` 为保持 manifest 相对路径解析，必须在 URL 路径中携带 `engine_id + item_id`，形态固定为 `/storage-assets/{engine_id}/items/{item_id}/{storage_ref}`。相对子资源请求会自然保留 item 身份；Manager 仍必须按 ID 回查 Meta 并校验 `storage_ref` 在该 DataItem 范围内。不使用无法由相对 URL 继承的 query 参数作为唯一身份。

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

字段级 `export` 首先覆盖 `runtime.boundary=bounded + load.mode=snapshot` 中三类主路径：统一 Native TablePipeline 结构化导出、Provider 可证明完整血缘的查询导出，以及 MongoDB collection 到 `mongodb_extended_jsonl` 的原始记录格式导出。原生表源不按 `engine_type` 建立保护分支；只要解析后的源端形态为 Native TablePipeline，就必须按服务端已解析的精确 ResourceLocator 与当前表结构校验投影，并在统一 `BatchData` 上执行保护。只读查询源必须从同一 PreparedQuery 取得 `ReadSet()` 和 `OutputLineage()`；当前已验证 PostgreSQL 可证明查询，以及只包含 `$match`、`$unwind`、`$project`、`$sort` 透明阶段的 MongoDB aggregate，投影表达式范围遵循引擎插件接口规范。MongoDB 原始记录导出按精确 collection Locator 与 Meta 当前字段结构校验投影，在 Provider 保留 BSON 标量类型的文档对象上执行规则，随后才生成 Canonical Extended JSON；Transfer 不解析已经编码的 EJSON，也不把预览友好值作为导出输入。保护结果必须位于用户字段映射、类型转换、空间处理、目标 writer 和文件格式序列化之前。`mask` 只修改当次批次副本；`suppress` 必须同时移除值和对外字段结构；执行记录、进度、错误和血缘不得记录原值。Security 的 `export` 投影保持引擎无关，是否存在可执行的字段级导出路径由 Transfer Owner 判定。

未纳管租户或未命中纳管目标的 bounded 任务继续原路径；不解析字段，不调用 Security 或 Meta，不写保护审计。命中纳管目标后，Projection 非 active、动作缺失、schema 漂移、查询血缘不完整或 derived 敏感输出都拒绝整个执行。

除上述已验证查询子集外的其他查询源、raw copy、watermark incremental、Kafka bounded replay、continuous/CDC 与从 encoded 内容作为源的路径，在各自建立并验证可证明的字段身份、结构新鲜度和执行屏障前继续使用资源级失效关闭，不得借用已有执行器开放。任一已支持路径的投影非 active、缺少 `export` 动作、Meta 结构缺失或漂移、组件路径不匹配或值保护失败时，必须拒绝整个导出，不得降级为未保护数据。

## 十一、Security API 契约

公开管理 API 使用 User Access Token、当前 Tenant AuthContext、精确 Permission 和资源版本。首期唯一资源路径：

| 方法 | 路径 | 作用 |
| --- | --- | --- |
| `GET/POST` | `/sensitive-data-types` | 列表/创建敏感数据类型 |
| `GET/PUT/DELETE` | `/sensitive-data-types/{id}` | 详情/完整更新/删除 |
| `GET` | `/definition-profiles` | 查询平台随版本提供的只读推荐定义方案 |
| `POST` | `/definition-profile-applications` | 显式、幂等地按稳定编码补齐当前 Tenant 缺失的推荐分类和等级；不覆盖已有同编码定义 |
| `GET/POST` | `/classifications` | 安全分类管理 |
| `GET/PUT/DELETE` | `/classifications/{id}` | 分类详情/完整更新/删除 |
| `GET/POST` | `/grades` | 安全等级管理 |
| `GET/PUT/DELETE` | `/grades/{id}` | 等级详情/完整更新/删除 |
| `GET/POST` | `/detectors` | 检测器管理 |
| `GET/PUT/DELETE` | `/detectors/{id}` | 检测器详情/完整更新/删除 |
| `GET` | `/detector-capabilities` | 查询当前平台版本已安装的只读检测能力注册表 |
| `GET/POST` | `/protection-baselines` | 保护基线管理 |
| `GET/PUT/DELETE` | `/protection-baselines/{id}` | 基线详情/完整更新/删除；删除 body 必须携带 `version`，写入与受影响投影重编译保持原子 |
| `GET/POST` | `/protection-enrollments` | 纳管列表/创建；GET 使用 `scope=current|released|all` 服务端分页筛选，默认 `current`；`released` 按退出完成时间倒序 |
| `GET` | `/protection-enrollments/{id}` | 纳管详情与激活进度 |
| `POST` | `/protection-enrollments/{id}/re-enrollments` | 携带已退出记录的 `version` 创建新的 ProtectionEnrollment；旧记录保持只读，目标已有未退出记录时冲突 |
| `POST` | `/protection-enrollments/{id}/releases` | 显式退出纳管，body 必须携带 `version` 和原因 |
| `POST` | `/protection-enrollments/{id}/discovery-executions` | 携带 `version` 显式创建一次有界重新发现执行；同一纳管同时至多一个 pending/running 执行 |
| `GET` | `/findings` | 敏感发现分页查询；支持按 Enrollment、来源快照、当前快照、复核状态、敏感类型和识别能力版本筛选，并返回目标资源快照、可选初审记录及后端组装的只读保护解释链 |
| `GET` | `/findings/{id}` | 不含原值的证据详情、可选初审记录及后端组装的只读保护解释链 |
| `POST` | `/findings/{id}/reviews` | 确认、调整或驳回 Finding |
| `GET` | `/discovery-quality` | 即时聚合识别质量摘要；可按 `sensitive_data_type_id` 过滤，使用 `security.finding.read`，不持久化第二份统计事实 |
| `GET` | `/protection-enrollments/{id}/components` | 实时读取并只返回尚未形成正式 Assessment、可人工指定的 Meta 当前组件，不返回业务值 |
| `GET/POST` | `/assessments` | 正式资源安全评估列表/从 Meta 当前组件人工创建正式评估；列表支持按 Enrollment 精确过滤 |
| `GET` | `/assessments/{id}` | 评估详情和修订历史 |
| `POST` | `/assessments/{id}/revisions` | 在同一评估聚合上形成新的正式修订 |
| `DELETE` | `/assessments/{id}` | 携带 `version` 和原因，追加 `not_sensitive` 修订以撤销当前正式结论，不删除历史 |
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
security.assessment.create
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
15. ProtectionBaseline 变化按旧、新绑定精准重编译且与定义写入原子提交；SensitiveDataType 自动发现初始等级变化只影响未复核候选；Detector 自动采用置信度变化走有界重新发现；正式 Assessment 不漂移；无关 Enrollment 不产生投影新版本，编译失败时定义写入回滚。
16. ProtectionExemption 只能绑定正式敏感 Assessment 和四个已实现字段级出口；创建、续期、撤销保留不可变修订并原子发布投影。有效期内只对指定动作返回原值，到期即使 Security 不可达也自动回落到 Policy 与 Baseline；结构冲突、血缘不明和 Owner 授权拒绝仍然 fail closed。

代码实施后的标准本地入口至少为：

```bash
make test-module MODULE=security
make test-platform
make test-changed
```

## 十七、后续范围

以下内容不进入首个手机号纵向切片，需要时先修订本规范：

- 按主体或用途约束的原值揭示、双人审批；
- 行过滤和单元格级任意策略 DSL；
- 可逆令牌化、假名化、静态脱敏产物和匿名化评估；
- 文档文本、图像、人脸、车牌、媒体和图数据敏感发现；
- 自动定时发现任务、TaskProvider 和 Orchestrator 编排；
- 把 Security 摘要写入 Catalog 搜索投影。
