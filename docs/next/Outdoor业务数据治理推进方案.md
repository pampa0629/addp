# 基于 Outdoor 业务文档的数据治理推进方案

> 本文说明如何把 [Outdoor 业务理解](Outdoor领域理解.md) 转化为 ADDP 中可治理、可计算、可供 Copilot 使用的语义资产。它是推进方案，不把尚未审核的候选对象直接当作平台事实。

> 当前状态（2026-09-01）：Outdoor 核心治理闭环已完成，生产只保留 `MongoDB -> Transfer ODS -> Develop DIM/DWD/DWS -> Model seal/publish -> Quality -> Service` 一条路线。任务 `47/48` 已软删除，且无现存编排引用；Catalog 中对应 source binding 为 `missing`，历史 execution 和软删除记录仅作审计事实，不是可执行入口。DWD 当前严格区分报名、实际参加和当前负责三类事实；唯一编排 `10` 已按修订口径完成新一轮 20 步全量重算。由于尚未给定业务调度周期，暂不配置 Cron，这不是遗留迁移任务。

## 1. 目标与边界

目标不是把 MongoDB 四个 collection 原样搬进 ADDP，而是建立一条可验证的链路：

```text
业务确认
  -> 物理事实核验
  -> Standard 语义资产
  -> Meta 物理字段绑定
  -> Model 逻辑实体与关系
  -> Transfer 将 MongoDB 贴源同步到 PostgreSQL ODS
  -> Model 准备 DIM/DWD/DWS 物化批次
  -> Develop 基于 ODS 计算 DIM/DWD，再基于同批 DIM/DWD 计算 DWS
  -> Quality 数据门禁
  -> Orchestrator 统一重算
  -> Copilot/Service 消费已发布指标结果
```

模块边界保持如下：

- `Standard` 拥有 Outdoor 业务域、术语、数据元、码值、单位、指标和定义文档；
- `Meta` 拥有 MongoDB collection、字段路径、动态 schema 采样事实和资源定位；
- `Model` 拥有租户级实体、实体关系、逻辑表、Standard 指标引用和逻辑表物化结构控制面，负责受控 DDL、staging 准备、结构校验与原子发布；
- `Transfer` 只负责引擎间数据同步：通过 bounded query-source 执行只读 MongoDB MQL，将嵌套 BSON 做贴源结构整理后写入任务自身配置的 PostgreSQL ODS 固定目标；它不认识 Model；
- `Develop` 只在 PostgreSQL 内执行保存的通用关系查询：先读取 ODS 计算 DIM/DWD staging，再读取同批 sealed DIM/DWD 计算 DWS staging；它不认识 Model，生产计算不直查 MongoDB；
- `Quality` 负责物化结果的主键、引用完整性、业务关系和指标结果门禁；
- `Orchestrator` 只引用各 owner 模块的持久任务，形成支持手动执行和定时调度的唯一全量重算 DAG；
- `Copilot` 只消费经过验证的资源事实、已审核语义上下文和已发布指标结果；MongoDB MQL 编译结果只保留为指标金样和开发期回归工具，不作为生产指标计算路线；
- `Graph`、`Asset`、`Service` 等模块只在各自 owner 边界内消费已发布语义，不新增第二套 Outdoor 事实源。

照片、人脸、强度自动计算、群组排行和路线分析不进入第一批闭环。它们保留在业务文档中作为后续专题，避免不确定语义阻塞核心人员/活动指标。

## 2. 推进原则

### 2.1 文档先于代码

业务文档中的“已确认”“观察事实”“待确认”必须分开。待确认内容只能作为候选或结构化澄清，不能自动写入已批准指标、查询编译规则或小模型固定提示词。

### 2.2 主体事实与摘要分离

活动成员、当前主领队、活动状态等主体事实优先来自 `Outdoors`；`Persons.myOutdoors`、`entriedOutdoors`、`caredOutdoors` 是人员侧索引或摘要。任何冲突都要保留数据质量证据，不能静默选择一方。

### 2.3 指标先声明粒度，再声明公式

每个指标必须明确：统计对象、事实粒度、关系口径、时间范围、状态过滤、去重键、空值处理、零分母规则、事实来源和结果单位。指标名称相似不代表可以共用公式。

### 2.4 小模型只做受约束的语义选择

27B 模型不应从整篇业务文档自由编写 MongoDB 查询。模型只选择已注册的实体、关系、字段、指标、状态枚举和计算操作；缺少必需语义时返回澄清；查询文本由确定性编译器生成。

### 2.5 敏感信息最小化

OpenID、电话、紧急联系人、人脸向量、人脸框、外部账号和照片原始内容不进入默认 Copilot 上下文。语义上下文只提供字段作用、身份解析方式、脱敏后的样例和权限约束。

## 3. 阶段一：冻结业务语义基线

### 3.1 当前业务决策

| 决策 | 当前口径 |
| --- | --- |
| 初始发起人与当前主领队 | 创建账号 `Outdoors._openid` 表示初始发起来源；当前负责活动的人按 `leader`/当前主领队计算，不能用转让后的 `members[0]` 还原历史发起人 |
| 报名活动 | 报名、替补、占坑均计入；退出后不计入 |
| 实际参加活动 | `报名中`、`领队`、`领队组`计入；替补、占坑、浏览中不计入 |
| 活动成行 | `拟定中`为草稿，`已取消`为未成行，其他状态统一为成行 |
| 群组与活动 | 不建立必然组织关系 |
| 照片/人脸 | 第一阶段暂不治理；`Photos._openid` 不作为活动归属键 |
| 定向活动重叠率 | `|A∩B|/|A|` 与 `|A∩B|/|B|` 同时返回 |

### 3.2 保留为工作假设、待历史数据验证的内容

人员侧 `myOutdoors`、`entriedOutdoors`、`caredOutdoors` 均属于可能悬空的缓存索引，活动主体事实不依赖这些数组；`addMembers` 是否计入实际参加仍待业务确认，第一阶段先排除。强度公式不进入本批次，直接使用最终强度数字。默认排除草稿、取消和缺少活动日期的活动；零分母时结果为 0。

### 3.3 交付物

- 更新后的 [Outdoor 业务理解](Outdoor领域理解.md)；
- 一份业务决策记录，记录决策人、确认时间和适用范围；
- 每个待确认问题的状态：`待确认`、`已确认` 或 `不适用`；
- 不把一次 MongoDB 数据快照中的记录数写成长期业务规则。

## 4. 阶段二：Meta 物理事实与数据质量核验

### 4.1 资源与字段事实

通过 MongoDB Engine 和 Meta 的统一扫描路径登记：

- `Outdoor.Persons`、`Outdoor.Outdoors`、`Outdoor.Groups`、`Outdoor.Photos` 四个 collection；
- MongoDB 目录路径 `database -> collection`；
- 嵌套字段完整路径，如 `members.entryInfo.status`、`title.level`、`myOutdoors.id`；
- 数值/string 混合、object/array 混合、空数组和字段缺失情况；
- 动态键结构：`Groups.members.*`、`Persons.facecodes.*`、`Outdoors.photos.*`；
- 记录数、字段覆盖率和采样范围。

当前已完成的 Meta 基线核验：Business MongoDB engine `11` 的 `Outdoor/Outdoors` 集合已深度扫描，记录数为 2,383；字段画像覆盖 `_id`、`_openid`、`status`、`members`、`members.personid`、`members.entryInfo.status`、`title.date` 和 `title.level`。其中 `title.level` 当前画像为字符串，不能在语义层假设其原生类型为数值；第一阶段仅使用其最终强度业务值，数值计算必须由确定性查询计划显式转换并对非数值值做质量处理。Meta 页面已能展开到集合并查看预览和字段属性，说明本批次物理资源事实具备进入字段绑定和指标回归的条件。

Meta 只记录结构化字段事实，不写入人脸向量、照片原图或完整样本记录作为长期元数据。动态键要表达为“按业务标识索引的映射”，不能把某一次样本中的键注册成标准字段。

### 4.2 第一批关系一致性检查

| 检查 | 目标 |
| --- | --- |
| 活动成员 `personid` 是否存在 | 识别悬空人员引用 |
| 主领队是否能映射成员或人员 | 识别转让、退出和快照不一致 |
| `entriedOutdoors` 活动 ID 是否存在 | 识别人员侧历史摘要失效 |
| `myOutdoors` 与当前主领队是否一致 | 观察转让后的摘要保留规则 |
| 成员状态和值类型分布 | 发现新状态和历史脏值 |
| 活动状态分布 | 验证草稿、取消、成行分类 |

照片、人脸和 `Photos._openid` 关系只记录观察结果，不作为第一批指标的依赖。质量检查结果由 Meta/Quality 的正式 owner 管理；不能在 Copilot 中临时查询并把检查结果当作业务定义。

### 4.3 本阶段门禁

只有字段路径、关系事实和数据质量结果可复现，才允许进入 Standard 绑定和指标验算。若扫描覆盖率不足，查询生成必须返回“需要补充元数据事实”，不能由模型根据样本猜字段。

## 5. 阶段三：Standard 语义资产

### 5.1 业务域与术语

在 Standard 建立 `Outdoor` 业务域，并优先登记：

- 户外参与者、户外活动、初始发起人、当前主领队、领队组、活动成员；
- 报名、实际参加、替补、占坑、关注/收藏；
- 活动状态、成行活动、活动路线、集合地点、活动强度；
- 定向活动重叠率。

每个术语需要定义、别名、适用范围、事实来源和禁止推断项。比如“发起活动”必须说明初始发起与当前主领队的区别，不能只绑定一个模糊同义词。

### 5.2 数据元与码值

数据元候选包括：人员标识、OpenID、昵称、活动标识、活动日期、活动地点、强度等级、累积公里数、累积爬升、强度调整比例、活动状态、成员报名状态和群组标识。

成员状态应建立码值集，但“是否实际参加”不应只依赖码值名称。推荐同时登记经审核的业务映射：

```text
报名中 -> actual_participation = true
领队 -> actual_participation = true
领队组 -> actual_participation = true
替补中 -> actual_participation = false
占坑中 -> actual_participation = false
浏览中 -> pending_confirmation
```

OpenID 等身份字段应设置安全等级；Copilot 只消费字段语义和经过授权的人员候选，不消费原始敏感值。

### 5.3 第一批指标

| 指标 | 粒度 | 核心公式/过滤 | 主要事实源 |
| --- | --- | --- | --- |
| 当前负责活动数 | 人员 | 当前主领队为该人员的活动 ID 去重数 | `Outdoors.leader`/成员角色 |
| 报名活动数 | 人员 | 报名、替补、占坑关系的活动 ID 去重数，排除退出 | 活动成员或 `entriedOutdoors` |
| 实际参加活动数 | 人员 | 状态为报名中、领队、领队组的活动 ID 去重数 | `Outdoors.members[]` |
| 当前负责或参加的不同活动数 | 人员 | 当前负责集合与实际参加集合按活动 ID 去重并集 | `Outdoors` |
| A 视角的 B 重叠率 | 人员对 | `|A∩B| / |A|` | 实际参加活动集合 |
| B 视角的 A 重叠率 | 人员对 | `|A∩B| / |B|` | 实际参加活动集合 |

每个指标进入 Standard 前都要补齐：时间过滤、草稿和取消状态处理、失效引用、零分母、是否将领队角色计为参加，以及结果为空时的返回语义。

## 6. 阶段四：Standard 与 Meta 的物理绑定

绑定应以“标准语义 -> 物理字段路径/映射结构”为主，不把 MongoDB 路径直接当作术语：

| 标准语义 | 物理绑定候选 | 绑定说明 |
| --- | --- | --- |
| 人员标识 | `Persons._id`、`members[].personid` | 同一人员标识在不同 collection 的引用路径 |
| 当前主领队 | `Outdoors.leader`、`members[].entryInfo.status` | 需要验证 leader 对象和成员角色的一致性 |
| 成员报名状态 | `Outdoors.members[].entryInfo.status` | 码值映射决定报名与实际参加 |
| 活动标识 | `Outdoors._id`、人员摘要 `*.id` | 活动主体以 `Outdoors._id` 为准 |
| 活动强度 | `title.level`、`title.addedLength`、`title.addedUp`、`title.adjustLevel` | 公式未闭合前只绑定数据元，不发布派生指标 |

`Persons` 中的摘要数组、成员展示快照和活动主体事实属于不同事实层级，绑定时必须明确“当前主体事实”“历史快照”“加速索引”三类角色。

## 7. 阶段五：Model 逻辑模型

Model 只保存经过 Standard 验证的引用，第一批建议形成：

- `OutdoorPerson`：人员身份、履历和可授权的展示属性；
- `OutdoorActivity`：活动主体、日期地点、强度、状态和当前主领队；
- `OutdoorParticipation`：人员与活动的报名、角色和实际参加状态。

当前 MongoDB 把成员嵌入活动文档，不代表逻辑模型也必须保持嵌套。Model 的实体关系应服务于粒度和指标计算，但不能伪造 MongoDB 中不存在的历史事实。

```mermaid
erDiagram
    OutdoorPerson ||--o{ OutdoorParticipation : participates
    OutdoorActivity ||--o{ OutdoorParticipation : has
    OutdoorPerson ||--o{ OutdoorActivity : leads
```

群组、照片和人脸暂不进入第一批 Model 设计；群组与活动之间不建立默认关系。

## 8. 阶段六：维度物化与指标计算

生产链路固定为：

```text
Transfer bounded query-source 任务将 MongoDB 贴源同步到固定 PostgreSQL ODS
  -> Model 准备 DIM/DWD/DWS 物化批次与 staging
  -> Develop SQL 查询任务读取 ODS 计算 DIM/DWD staging
  -> Model seal DIM/DWD 批次
  -> Develop SQL 查询任务读取同批 sealed DIM/DWD 计算 DWS staging
  -> Model seal DWS 批次
  -> Quality 门禁
  -> Model 原子发布本次完整重算结果
```

Model 拥有物化结构和发布边界，只根据已审批模型生成受控 DDL；不接受任意 DDL。Transfer 只负责 MongoDB 到 PostgreSQL ODS 的跨引擎流式同步；Develop 负责 PostgreSQL 内 ODS -> DIM/DWD -> DWS 的通用关系计算。两者都不创建、删除或修改正式逻辑表，也不依赖 Model。Orchestrator 只控制依赖、触发和执行追踪，不复制任务实现。所有生产计算都从 PostgreSQL ODS 起步；DWS 只读取同批 sealed DIM/DWD，禁止直接读取 `Outdoor.Outdoors` 或 `Outdoor.Persons`。

第一批物理表如下：

| 物理表 | 粒度 | 用途 |
| --- | --- | --- |
| `dim_outdoor_person` | 每个人员一行 | 稳定人员标识及授权展示属性 |
| `dim_outdoor_activity` | 每个有效活动一行 | 活动日期、状态和最终强度 |
| `dwd_outdoor_participation` | 每个活动与人员组合一行 | 合并报名、实际参加和当前负责三类关系 |
| `dws_outdoor_person_metric` | 每个人员、指标、指标版本和统计范围一行 | 四项人员活动数指标 |
| `dws_outdoor_person_pair_metric` | 每个无序人员对、指标、指标版本和统计范围一行 | 共同活动数、两个分母和两个方向的重叠率 |

`dwd_outdoor_participation` 必须以 `person_id + activity_id` 为复合主键，至少包含 `is_signup`、`is_actual_participant` 和 `is_current_leader`。同一人员在同一活动中的多种关系合并为一行布尔事实；当前主领队即使不在 `members[]` 中，也要进入该事实表。人员侧摘要数组不参与事实生成。

两张 DWS 都属于指标事实表，不是业务实体表。人员对统一按稳定人员标识排序为 `person_id_a < person_id_b`，一对人员只存一行，同时保存 A→B 和 B→A 两个方向结果。Top 10 固定取同一次重算中 `outdoor_responsible_or_actual_activity_count` 降序、人员标识升序的前十名。DWS 不重复保存 `run_id`：重算 lineage 由 `common.task_executions` 和 Model MaterializationBatch 统一表达；结果表保留 `calculated_at` 作为业务消费时间事实。

MongoDB `title.level` 的真实值包含 `1.9`、`2.2` 等小数。ODS 保留贴源值；`dim_outdoor_activity.activity_intensity` 和 `dwd_outdoor_participation.activity_intensity` 必须统一使用 `decimal`，由 Develop 任务 `52/53` 在业务加工阶段显式转换，转换失败写 `NULL` 并交给 Quality 观测；不得使用 `int` 截断业务值。

### 8.1 Transfer 与 Develop 持久任务

第一批建立八个可独立审计和重试的持久任务：

1. Transfer `outdoor_ods_persons_refresh`（`74`）；
2. Transfer `outdoor_ods_activities_refresh`（`75`）；
3. Transfer `outdoor_ods_activity_members_refresh`（`76`）；
4. Develop `outdoor_dim_person_from_ods_refresh`（`51`）；
5. Develop `outdoor_dim_activity_from_ods_refresh`（`52`）；
6. Develop `outdoor_dwd_participation_from_ods_refresh`（`53`）；
7. Develop `outdoor_dws_person_metric_refresh`（`49`）；
8. Develop `outdoor_dws_person_pair_metric_refresh`（`50`）。

前三个任务属于 Transfer bounded query-source：MongoDB MQL 只做 ODS 所需的确定性结构整理，普通对象子字段通过 `$project` 投影，成员数组通过 `$unwind` 展开，再写入三个固定 ODS 目标。Transfer 不解释 Standard 码值或 Model 业务粒度，不提供递归 JSON 自动摊平。后五个任务属于 Develop PostgreSQL SQL 查询：`51/52/53` 读取 ODS 生成 DIM/DWD，`49/50` 只读取同一父编排下已 sealed 的 DIM/DWD 生成 DWS。

Transfer 三个任务按各自配置持有固定 ODS 目标；Develop 五个 writer 任务不保存物化批次标识、逻辑表标识或 Model 物理目标名。Orchestrator 先执行对应 Model prepare，再把 `staging_locator` 作为 Develop writer 的 `target_locator`；Develop 的关系查询参数只绑定 Transfer 的固定 ODS 输出或同一父编排中已 sealed 上游批次 locator。Transfer 与 Develop 都不承担模型 DDL 或正式表替换；失败不能把半成品标记为成功。

### 8.2 Quality 门禁

第一批门禁至少包括：维表主键非空且唯一、DWD 复合主键唯一、DWD 人员和活动引用完整、实际参加集合是报名集合子集、各布尔关系口径可复现、DWS 粒度唯一，以及 Top 10 足够时人员对结果恰好为 45 行。门禁失败时本次总编排失败，不发布成功结论。

### 8.3 Orchestrator 总编排

唯一总编排命名为 `outdoor_governance_full_refresh`，手动执行和 Cron 调度必须进入同一个 DAG：

```mermaid
flowchart LR
    P[人员维度] --> F[活动参与事实]
    A[活动维度] --> F
    F --> M[人员指标]
    M --> O[Top 10 人员对指标]
    O --> Q[Quality 完整性门禁]
```

Orchestrator 的父执行 ID 作为同一次重算的稳定执行 lineage 贯穿所有子 execution 和 MaterializationBatch，不复制进 DWS 业务行。下游任务只能在依赖任务成功后启动，任一节点失败时终止后续节点。

## 9. 阶段七：Copilot 语义上下文与结果消费

### 8.1 面向 27B 的最小语义包

Copilot 不直接喂入整篇讨论稿，而是由已批准 Standard 资产和 Meta 事实生成版本化语义包。语义包至少包含：

```json
{
  "domain": "outdoor",
  "entities": ["person", "activity", "participation"],
  "identities": {
    "person": ["Persons._id"],
    "activity": ["Outdoors._id"]
  },
  "relationships": [
    "person_current_leader_activity",
    "person_signup_activity",
    "person_actual_participation_activity"
  ],
  "approved_metrics": [
    "current_responsible_activity_count",
    "signup_activity_count",
    "actual_participation_activity_count",
    "directional_activity_overlap_rate"
  ],
  "uncertainties": [
    "browser_status",
    "leader_transfer_my_outdoors_retention"
  ]
}
```

实际字段路径、枚举和指标定义应由系统从已审核事实生成，不在提示词中复制第二份易漂移的规则。

### 8.2 结构化查询计划

对于“某人参加了多少活动”这类请求，模型应输出语义计划：

1. 解析人员候选，不以昵称直接当稳定身份；
2. 选择 `actual_participation_activity` 关系；
3. 选择 `activity_id` 去重；
4. 应用已批准的成员状态映射；
5. 明确时间范围、活动状态和空值规则；
6. 生产查询读取已发布的 DWS 指标结果；
7. 返回指标版本、计算批次、字段证据和结果引用。

MongoDB MQL 编译器继续用于金样生成、源事实核验和开发期回归，不作为生产指标结果接口。Copilot 不得针对同一指标在运行时重新生成一条并行计算路线。

对于定向重叠率，计划必须明确两个人、实际参加关系和两个方向的分母。不能把用户的“重叠度”自动改写为 Jaccard 或对称相似度。

### 8.3 澄清机制

以下情况必须返回结构化澄清：

- 同名人员无法唯一解析；
- 用户说“报名”但未说明是否包含替补/占坑，而当前指标不存在批准口径；
- 用户说“参加”但 `浏览中` 是否计入仍未确认；
- 未提供时间范围，而指标定义要求时间窗口；
- 用户说“重叠度”但未说明是实际参加还是报名集合；
- 分母为空、活动状态过滤或失效引用处理未闭合；
- 查询需要未扫描、字段覆盖不足或动态结构无法安全解释。

小模型可以提出候选解释，但不能把候选解释写入 `assumptions` 后继续编译。

## 9. 阶段七：验证与小模型回归集

### 9.1 指标金样

为每个已批准指标准备人工可核对的金样：

- 选定人员的稳定 ID、昵称候选和授权范围；
- 参与活动 ID 集合及每个成员状态；
- 领队转让、退出、替补、占坑和重复摘要案例；
- 共同活动集合和两个方向的重叠率；
- 空集合、零分母、失效引用和草稿/取消活动案例。

金样必须记录“期望集合”和“期望公式”，不能只记录最终数字，否则无法定位去重、过滤和分母错误。

### 9.2 分层验证

| 层级 | 验证内容 |
| --- | --- |
| Meta | 字段路径、动态 schema、记录数和关系质量检查 |
| Standard | 术语、数据元、码值、指标公式和生命周期校验 |
| Model | 实体粒度、关系方向、指标引用、物化批次和受控发布 |
| Copilot | 语义计划、澄清、敏感信息过滤和资源事实引用 |
| Transfer | 只读 MongoDB MQL、流式查询读取、贴源结构整理和 PostgreSQL ODS 固定目标写入 |
| Develop | 只读 PostgreSQL ODS 计算 DIM/DWD，再只读同批 sealed DIM/DWD 计算 DWS，并写入受限 staging |
| Quality | 主键、引用、关系口径和指标结果门禁 |
| Orchestrator | 单一 DAG、手动执行、定时执行和失败传播 |
| Query/MQL | 高级 MQL 仅作为源事实金样；生产 ODS MQL 与后续 SQL 可分层复现 |
| 端到端 | MongoDB -> ODS -> DIM/DWD -> DWS -> 质量门禁 -> 指标结果消费 |

### 9.3 27B 验收标准

重点不是让模型“记住文档”，而是验证它是否能在受控上下文中稳定完成：

1. 正确区分报名、替补、占坑和实际参加；
2. 正确区分初始发起人与当前主领队；
3. 使用活动 ID 去重，不用标题去重；
4. 正确返回两个方向的重叠率；
5. 对未确认语义主动澄清，不猜测；
6. 不泄露 OpenID 等敏感信息；
7. 生成的 MQL 能由确定性校验器验证并复现指标公式。

## 10. 模块责任与实施顺序

| 顺序 | Owner 模块 | 主要交付 |
| ---: | --- | --- |
| 1 | 业务/架构 + 文档 | 确认业务口径，更新 Outdoor 业务文档和决策记录 |
| 2 | Meta | 完成 MongoDB `Persons`、`Outdoors`、`Groups` 的深度扫描和字段事实 |
| 3 | Quality | 关系一致性、状态分布、失效引用质量检查 |
| 4 | Standard | 建立 Outdoor 域、术语、数据元、码值、指标和绑定入口 |
| 5 | Model | 建立逻辑模型和指标引用，准备受控物化批次 |
| 6 | Transfer | 执行只读 MongoDB MQL，将贴源结构整理结果流式写入 PostgreSQL ODS 固定目标 |
| 7 | Develop | 基于 ODS 生成 DIM/DWD，再基于同批 sealed DIM/DWD 生成 DWS |
| 8 | Quality | 执行物化和指标结果门禁 |
| 9 | Orchestrator | 建立唯一的手动/定时全量重算 DAG |
| 10 | Copilot/Service/Monitor | 消费已发布结果、提供解释并统一观测执行 |

不能先在 Copilot 中硬编码 Outdoor 规则，再反向补 Standard；也不能在 Model 中复制一套独立指标定义。每次实现必须同步对应测试入口和 CI 门禁，至少覆盖受影响模块的标准测试。

## 11. 第一轮建议范围

第一轮只做 Person、Activity、Participation 三个核心对象和六项指标：

1. 当前负责活动数；
2. 报名活动数；
3. 实际参加活动数；
4. 当前负责或实际参加的不同活动数；
5. A 视角的 B 重叠率；
6. B 视角的 A 重叠率。

第一轮的最小闭环是：

```text
业务口径确认
 -> Persons/Outdoors Meta 深度扫描
 -> Standard 术语/数据元/指标
 -> Model Person/Activity/Participation
 -> Transfer 通过只读 MQL 将 MongoDB 贴源同步到 PostgreSQL ODS
 -> Model 准备物化批次
 -> Develop 基于 ODS 生成 DIM/DWD，再基于同批 sealed DIM/DWD 计算 DWS
 -> Quality 门禁
 -> Orchestrator 统一重算
 -> 金样回归与结果消费
```

只有这条链路在样例和边界案例上通过，才扩展到 `Groups`、`Photos`、人脸识别、强度公式和路线分析。

## 12. 当前租户的首轮配置记录（2026-08-24）

本轮使用已登录 Console，通过 Standard 和 Model 完成最小垂直切片：

| 模块 | 已配置内容 | 状态 |
| --- | --- | --- |
| Standard 业务域 | 已核对 `户外域 / outdoor`，未重复创建 | 已存在 |
| Standard 码值集 | `outdoor_member_status`，录入报名中、领队、领队组、替补中、占坑中、浏览中六项 | 已创建 |
| Standard 业务术语 | 初始发起人、当前主领队、实际参加、成行活动、定向活动重叠率 | 已审批 |
| Standard 指标 | `outdoor_actual_participation_activity_count`（实际参加活动数） | 已审批 |
| Standard 指标 | `outdoor_signup_activity_count`（报名活动数） | 已审批 |
| Standard 指标 | `outdoor_current_responsible_activity_count`（当前负责活动数） | 已审批 |
| Standard 指标 | `outdoor_responsible_or_actual_activity_count`（当前负责或实际参加的不同活动数） | 已审批 |
| Standard 指标 | `outdoor_directional_actual_participation_overlap_rate`（定向实际参加活动重叠率，指标 ID `8`） | 已审批 |
| Model 实体 | 活动、人员、活动参与；已绑定 Outdoor 数据元并补齐主键 | 已审批 |
| Model 关系 | 人员 -> 活动参与（一对多）；活动 -> 活动参与（一对多） | 已配置 |

旧的“参加活动次数”及其派生依赖因口径不完整已删除；客户域、性别码值集等无冲突资产未修改。

## 13. 首轮验证与收敛记录

1. 指标新建和详情界面现已补充业务域选择；`outdoor_actual_participation_activity_count` 已通过正式更新接口绑定 `户外域`，未改变其数据元或 Model 引用。
2. Model 属性可以表达标量类型，但不能表达 `members[]` 展开、`title.date`/`title.level` 嵌套路径及数组元素过滤；这些必须由 Meta locator 和逻辑表绑定承载。
3. Model 关系类型只有一对一、一对多、多对多；主体 -> 活动参与应使用一对多，不能误建反向的“活动参与 -> 活动”一对多。
4. Model 属性的数据元选择闭环已验证可用；Standard 数据元详情页现已补充业务域编辑入口，早期创建的“活动标识”已通过带版本校验的正式更新接口绑定 `户外域`，未改变其 Model 引用。
5. 指标审批只校验标准定义，不校验 MongoDB 字段事实、状态码值和可执行计算计划；发布前仍需要 Meta/Copilot 事实门禁。

### 13.1 Meta 核验结果（2026-08-24）

通过已登录 Console 的 Meta 扫描页面及 `meta.meta_item` 只读核验，MongoDB `Business MongoDB` 的 `Outdoor` 数据库已完成 `deep` 扫描。当前已确认：

- `Outdoors` item id 为 `51659`，`Persons` item id 为 `51657`，`Groups` item id 为 `51658`，`Photos` item id 为 `51656`；
- `Outdoors.members.personid`、`Outdoors.members.entryInfo.status`、`Outdoors.title.date`、`Outdoors.title.level`、`Outdoors._id` 已保留为字段路径；
- `Persons._id`、`Persons.userInfo.nickName`、`Persons.myOutdoors`、`Persons.entriedOutdoors`、`Persons.caredOutdoors` 已保留为字段路径；
- `Photos` 暂不作为第一批指标事实源，不能用 `Photos._openid` 推断活动归属。

结论：不需要修改 MongoDB 动态 schema 扫描器；Standard 数据元、Model 属性和指标业务域绑定均已完成，可执行查询、事实门禁和生产物化的最终结果见 13.13～13.16。

### 13.2 已配置的 Standard 数据元

已创建并审批：人员标识、人员昵称、活动日期、活动状态、最终强度、成员人员标识、成员状态；活动标识已创建并审批，并已绑定 `户外域`。此前编码误录的人员标识草稿已删除并按 `outdoor_person_id` 重建。

### 13.3 Model 绑定与审批结果

人员、活动、活动参与三个实体的关键属性已关联 Standard 数据元，并分别补齐主键属性；人员 -> 活动参与、活动 -> 活动参与两条一对多关系保持不变。三个实体均已审批通过。`actual_participation` 是由成员状态映射得到的派生属性，不直接绑定物理字段。

### 13.4 首个指标的真实数据验算

使用 `Outdoor.Outdoors` 的真实数据按已审批口径独立验算：排除 `拟定中`、`已取消` 和缺少 `title.date` 的活动；展开 `members[]`；仅保留 `报名中`、`领队`、`领队组`；按 `members.personid + Outdoors._id` 复合去重。2026-08-26 当前快照得到 681 个有效活动、583 个出现实际参加关系的人员、4,886 个去重后的人员-活动关系。示例最高值为人员 `W7cw8J25dhqgDMHA`（昵称“攀爬”）实际参加 286 个活动。此前记录的 1,099 人和 6,799 条关系混入了未应用有效活动过滤的结果，不能作为该指标口径的回归基线。

该结果证明业务口径和物理字段可以闭合。后续已完成已审批指标的确定性查询计划、Meta/Copilot 事实门禁、真实数据回归与生产物化；本节不再保留过渡期待办。

### 13.5 首个指标的通用查询计划能力（2026-08-25）

首个“实际参加活动数”指标暴露了原有 MQL 强类型计划的边界：`count_array_elements` 只能计算每条文档数组长度，不能表达“展开 `members[]`、过滤成员状态、按人员分组、按活动 `_id` 去重后计数”。这不是 Outdoor 专属规则，应由 Copilot 的通用计划能力承担。

已补充 `count_distinct_array_elements` 语义操作，计划必须声明：

- `field`：待展开的数组字段；
- `element_filters`：数组元素级过滤条件；
- `group_by`：数组元素归属实体字段，例如 `members.personid`；
- `distinct_by`：去重身份字段，例如活动主体 `Outdoors._id`。

编译器按以下固定顺序生成单个 MQL `aggregate` command：

```text
有效活动过滤
  -> $unwind members
  -> 成员状态过滤
  -> 身份字段非空过滤
  -> group_by + distinct_by 复合去重
  -> 按 group_by 聚合计数
```

模型只选择已验证 collection、字段和状态值，不生成 pipeline。`in` 状态集合会被展开为独立的标量参数；活动日期的存在性和空字符串过滤也属于计划条件。该路径仍由 Develop 负责保存、预检和执行，Copilot 只返回候选查询和参数定义。

当前运行中的 `Outdoor.Outdoors` 快照按同一计划独立回归得到：583 个出现实际参加关系的人员、4,886 条去重后的人员-活动关系，最高人员“攀爬”（`W7cw8J25dhqgDMHA`）为 286 个活动。该快照与 2026-08-24 文档基线的总量不同，属于源数据变化；回归必须同时记录快照时间和口径，不能把任一批次数字写成业务定义。

重叠率批量计算对象固定为“当前负责或实际参加的不同活动数”最多的 10 个人：先按指标 `outdoor_responsible_or_actual_activity_count` 的活动去重并集降序排序，人员标识作为同值时的稳定次序；再对实际人数生成无序人员对，10 人时共 45 对。每一对只存一行，但同时输出 A→B 与 B→A 两个方向的重叠率。少于 10 人时按实际人数生成组合，空集合返回空结果且不报错。

Standard 指标 `outdoor_actual_participation_activity_count` 已通过登录 Console 的正式更新接口写入上述强类型计划，当前版本为 4、状态仍为已审批。配置只保存语义计划，不保存 MQL 文本；Meta collection、字段事实和执行范围仍由 Develop/Copilot 的资源事实链路提供。

### 13.6 第二个指标：报名活动数（2026-08-25）

已在 Standard 正式创建并审批 `outdoor_signup_activity_count`（指标 ID `5`），所属业务域为 `户外域`。指标沿用首个指标的有效活动过滤和复合去重计划，仅将成员状态集合扩展为：

```text
报名中、领队、领队组、替补中、占坑中
```

`浏览中` 不计入报名关系。语义计算配置仍使用 `count_distinct_array_elements`，以 `members.personid + Outdoors._id` 去重；未使用 `Persons.entriedOutdoors[]` 作为主体事实源。该指标已通过审批，且后续已完成报名与实际参加的集合边界回归；替补和占坑只增加报名关系，不增加实际参加关系。

随后已在 Develop 查询工作台执行对应 MQL，使用同一 `Outdoor/Outdoors` Meta 资源和相同的有效活动过滤，执行成功，耗时 37ms；工作台展示前 500 行并标记结果截断。该执行只证明计划可运行，完整人员总量和报名/实际参加差异仍需通过全量汇总或专门对照查询回归，不以工作台的 500 行展示上限作为统计总量。

### 13.7 报名与实际参加边界回归（2026-08-25）

使用 Business MongoDB 中 `Outdoor.Outdoors` 的当前快照，直接执行与两个指标相同的有效活动过滤和 `members.personid + Outdoors._id` 复合去重，并对报名集合与实际参加集合做集合差异计算，结果如下：

| 校验项 | 当前快照结果 |
| --- | ---: |
| 有效活动数 | 681 |
| 报名人员-活动关系数 | 4,944 |
| 实际参加人员-活动关系数 | 4,886 |
| 报名但未实际参加 | 58 |
| 实际参加但未报名 | 0 |
| 其中替补中 | 34 |
| 其中占坑中 | 24 |

`34 + 24 = 58`，且实际参加集合没有反向差异，证明当前数据同时满足三条边界：替补和占坑计入报名、不计入实际参加；`浏览中` 不会被两个集合纳入；活动主体事实和活动 ID 去重逻辑闭合。该结果是快照回归证据，不应写入指标定义作为固定业务常量。

### 13.8 第三个指标：当前负责活动数（2026-08-25）

已在 Standard 正式创建并审批 `outdoor_current_responsible_activity_count`（指标 ID `6`），所属业务域为 `户外域`。指标以 `Outdoors.leader.personid` 作为当前主领队身份，按 `Outdoors._id` 去重；不使用创建账号 `_openid`，也不使用人员侧 `myOutdoors[]` 缓存索引。有效活动过滤与前两个指标一致。

为表达该指标，Copilot 增加通用语义操作 `count_distinct_documents`：`group_by` 声明文档归属字段，`distinct_by` 声明文档身份字段；确定性编译器补充非空身份过滤和两级 `$group`，模型不直接生成 MQL pipeline。当前快照独立回归得到 75 位当前主领队、577 条有效活动负责关系，最高人员 `W7cw8J25dhqgDMHA` 当前负责 224 个活动。

核对 Meta 时发现 `Outdoors` 动态 schema 正好达到 200 字段上限，旧扫描顺序会先耗尽在按字典序靠前的大型嵌套对象上，导致真实存在的顶层 `leader` 没有进入字段事实。扫描器已改为按层、按父路径交错采集，使有限字段预算优先覆盖不同顶层对象，再逐层补嵌套字段；没有增加 Outdoor 字段白名单，也没有提高字段数量上限。重新扫描后必须以 Meta 出现 `leader.personid` 作为第三个指标 Copilot 闭环的发布门禁。

2026-08-25 的第一次重扫仍未通过该门禁。进一步回归发现，字段预算虽然已改为跨头尾样本采集，但嵌套字段仍按单文档顺序扩展，第一份文档的深层字段会再次耗尽预算。扫描器随后改为两阶段跨样本采集：先合并所有样本的顶层字段，再按父路径和样本统一交错扩展嵌套字段。新增单元测试覆盖“第一份文档拥有大量嵌套字段、后续文档才出现 `leader.personid`”的场景；MongoDB 集成采样测试和 `go test ./common/engine/plugins/mongodb` 均通过。

第二次重扫于 `2026-08-25 15:27:01+08` 完成，Meta 查询结果为 `leader.personid` 存在（`t`），同时已登记 `leader`、`leader.entryInfo`、`leader.userInfo` 等字段。该结果满足第三个指标的物理字段发布门禁；后续指标 MQL 验证可以使用真实 Meta 资源继续推进。

随后用真实 Meta 字段集合编译并执行第三个指标的 MQL，执行成功。结果为 75 位当前主领队、577 条当前负责活动关系，人员 `W7cw8J25dhqgDMHA` 的去重活动数为 224，与独立 MongoDB 聚合回归一致。至此，第三个指标完成“Standard 定义 -> Meta 字段门禁 -> Copilot 确定性编译 -> MongoDB 执行 -> 结果对照”闭环。

### 13.9 第四个指标：当前负责或实际参加的不同活动数（2026-08-25）

曾在旧版 Standard 创建并审批 `outdoor_responsible_or_actual_activity_count`（旧指标 ID `7`、版本 `4`），所属业务域为“户外域”。指标粒度为人员，计算对象是两个集合的并集；旧实现曾把 `count_distinct_document_and_array_elements` 结构化计算配置保存在 Standard 指标中。

指标定义/实现拆分后，该记录只作为历史验证证据：Standard 应重建业务定义修订，原粒度、来源、过滤、去重和 MongoDB 可执行配置应在 Model MetricImplementation 中重建并冻结对应 MetricDefinitionRevision。旧 `derivation_config` 已随迁移删除，不自动伪造新实现。

指标计算对象是两个集合的并集：

- `Outdoors.leader.personid` 代表当前主领队负责的活动；
- `Outdoors.members[]` 中状态为 `报名中`、`领队`、`领队组` 的成员代表实际参加的活动；
- 两个来源都应用有效活动过滤：排除 `拟定中`、`已取消` 和缺少 `title.date` 的活动；
- 以 `人员标识 + Outdoors._id` 去重后按人员计数，不能把两个指标结果直接相加。

为表达这一稳定的跨层集合语义，Copilot 新增通用操作 `count_distinct_document_and_array_elements`。编译器生成一条确定性 MongoDB 管道：成员数组分支先展开并过滤，当前主领队分支通过 `$unionWith` 合并，随后按人员和活动 ID 两级去重计数。该操作要求同时声明数组字段、数组分组字段、文档分组字段、活动身份字段和成员状态过滤，不接受 Outdoor 专用隐式规则。

当前 Outdoor 快照的真实聚合结果为 583 位人员、4,888 条人员-活动去重关系；人员 `W7cw8J25dhqgDMHA` 为 286 条。独立 MongoDB 聚合与 Copilot 编译后的管道结果一致。与实际参加关系 4,886 条相比，合并后增加 2 条去重关系，说明结果确实是集合并集而非计数相加。

### 13.10 双向实际参加活动重叠率（2026-08-25）

业务要求的“重叠度”不是 Jaccard 或对称相似度，而是两个方向的条件比例：

```text
A 视角的 B 重叠率 = |A 实际参加活动 ∩ B 实际参加活动| / |A 实际参加活动|
B 视角的 A 重叠率 = |A 实际参加活动 ∩ B 实际参加活动| / |B 实际参加活动|
```

该指标以 `Outdoors` 活动主体文档为唯一事实源，不使用 `Persons.myOutdoors[]`、`entriedOutdoors[]` 或 `caredOutdoors[]` 摘要。Copilot 新增通用语义操作 `directional_overlap_rate`，计划必须声明：

- `field`：成员数组 `members`；
- `entity_field`：成员人员标识 `members.personid`；
- `entity_values`：待比较的两个稳定人员标识；
- `activity_id_field`：活动标识 `_id`；
- `element_filters`：实际参加状态 `报名中`、`领队`、`领队组`。

确定性编译顺序为：有效活动过滤 -> 展开 `members[]` -> 实际参加状态过滤 -> 人员与活动标识去重 -> 形成两个人的活动集合 -> 计算交集、两个分母和两个方向比例。任一分母为 0 时对应比例返回 0；输出同时包含共同活动数、两个人各自活动数、`overlap_rate_from_left` 和 `overlap_rate_from_right`。该操作已加入 Copilot 编译器和 27B 语义提示约束，并由单元测试覆盖集合去重、双向输出和零分母保护。

结构化推理的严格响应 Schema 已同步登记 `entity_field`、`entity_values` 和 `activity_id_field`，避免模型按提示生成合法计划后又被响应契约拒绝。聚合管道使用 `$facet` 收束人员活动集合，使过滤后没有任何匹配记录时仍返回一行结果，并把共同活动数、两个分母和两个方向比例全部稳定为 0，而不是返回空结果。

使用当前 `Outdoor.Outdoors` 快照对实际参加活动数最高的两名人员执行真实 MongoDB 聚合回归：人员 `W7cw8J25dhqgDMHA` 有 286 个去重活动，人员 `W7Y6ad2AWotkW4_c` 有 193 个去重活动，共同活动 32 个；A→B 重叠率为 `32 / 286 = 0.11188811188811189`，B→A 重叠率为 `32 / 193 = 0.16580310880829016`。两个分母与独立的实际参加活动计数查询一致，证明集合构造、活动 ID 去重和双向分母均闭合。上述数字只作为 2026-08-25 当前快照的回归证据，不进入指标定义。

随后按已冻结的批量口径完成 Top 10 全量回归：先以“当前负责或实际参加的不同活动数”降序、人员标识升序稳定选出 10 人，再基于实际参加活动集合生成 45 组无序人员对。当前快照得到 45/45 对结果，零分母人员对为 0，共同活动数最小为 0、最大为 178；既覆盖完全无交集，也覆盖高度单向重叠。批量调度只是对同一个双人强类型计划替换 `entity_1`、`entity_2` 参数并逐对执行，不新增批量专用 MQL 语义或 Outdoor 硬编码编译路径。

使用查询工作台的本地 `qwen3.8:27b-mlx` 做了真实生成回归。资源发现阶段正确返回 Outdoor 范围内的 `Groups`、`Outdoors`、`Persons`、`Photos` 四个真实候选，并在用户确认 `Outdoors` 后进入语义规划；模型没有自行猜测 collection。首轮语义规划要求补充两个人员稳定标识和“实际参加”状态口径，符合缺少必需语义时必须澄清的门禁。补充 `members.personid` 字面测试值、`members.entryInfo.status` 三个已批准状态和有效活动过滤后，第二轮生成超过 110 秒仍未返回结果，Copilot 与 Inference 健康检查均正常、前端无错误日志。因最终强类型计划尚未返回，本次不能把 27B 计划生成标记为通过；该现象进一步证明查询助手需要明确的超时、取消或异步任务反馈，不能无限保持禁用态“生成中”。

### 13.11 Model 逻辑表与星型关系（2026-08-25）

在已有 Person、Activity、Participation 业务实体和“活动参与事实”逻辑表基础上，已通过 Model 正式界面补齐三张可执行逻辑表：

| 逻辑表 | 物理表 | 类型 | 状态 |
| --- | --- | --- | --- |
| 人员维度 | `dim_outdoor_person` | dimension | 已审批 |
| 活动维度 | `dim_outdoor_activity` | dimension | 已审批 |
| 活动参与事实 | `dwd_outdoor_participation` | fact | 已审批 |

事实表粒度为“每行一条有效户外活动中的人员参与关系”，复合主键为 `person_id + activity_id`。星型关系已配置为：

- `活动参与事实.person_id -> 人员维度.person_id`（FK）
- `活动参与事实.activity_id -> 活动维度.activity_id`（FK）

事实表保留成员状态、活动日期、实际参加标识和最终强度字段；实际参加标识是由成员状态按业务口径派生的治理字段。该逻辑模型用于承载指标粒度和维度导航，不改变 MongoDB `Outdoors` 作为活动主体事实源的原则。

### 13.12 Meta -> Develop 首个指标执行回归（2026-08-25）

在已登录 Console 中，通过 Develop 查询工作台选择 Business MongoDB、MQL 和 `Outdoor` 数据库，执行与首个指标计划一致的确定性查询。查询使用 Meta 已验证的 `Outdoors` collection，并按以下顺序执行：活动状态和日期有效性过滤、展开 `members`、成员状态过滤、人员与活动复合去重、按人员计数。

本次真实执行结果：请求成功，执行耗时 61ms，返回 500 行（工作台结果展示上限为 500 行，结果标记为截断）；结果中包含人员 `W7cw8J25dhqgDMHA` 的 `actual_activity_count = 286`，与独立回归结果一致。Develop 执行日志确认目标定位为 `addp://engine/11/path/Outdoor?type=database`，执行状态为 `success`。这证明 Meta 资源事实、MQL 预检、MongoDB 执行器和结果回传已经形成闭环；全量人员数量应通过专门的汇总查询或导出处理，不能把“返回行数 500”误当作人员总数。

同时，在同一工作台使用 AI 查询助手提交相同业务描述，资源发现阶段正确列出 `Groups`、`Outdoors`、`Persons`、`Photos` 并要求确认 `Outdoors`。确认后的第二次请求最终返回 HTTP 200，但本地 `qwen3.8:27b-mlx` 结构化推理耗时约 58.9 秒，网关总耗时约 59.1 秒；因此页面长时间显示生成中是低延迟体验问题，不是资源发现失败或 API 丢响应。当前查询助手仍应补充明确的长耗时状态提示或异步执行体验，但这不阻塞确定性计划编译和指标回归。

### 13.13 最终生产路线与首轮成功回归（2026-08-28）

本节记录 Outdoor 生产改造的最终事实。过渡期的旧任务、旧 17 步 DAG 和 Transfer 直接写 DIM/DWD 路线均已退出；唯一生产路线为：

```text
MongoDB Outdoor
  -> Transfer：固定目标的跨引擎快照同步，MQL 整形并写入 PostgreSQL ODS
  -> Develop：只读取 PostgreSQL ODS，计算 DIM/DWD 并写入 Model 准备的 staging
  -> Model：prepare / seal，持有逻辑表结构和物化生命周期
  -> Develop：只读取同批 sealed DIM/DWD，计算两张 DWS staging
  -> Quality：对五个 sealed batch 执行统一门禁
  -> Model：对物化组执行一次原子发布
```

模块边界保持不变：Transfer 和 Develop 均不知道 Model；Transfer 只承担引擎间数据同步，Develop 只承担通用关系查询参数到既有目标表的查询计算，Model 独占逻辑表 DDL 和物化生命周期，Orchestrator 是唯一跨业务模块组合层。MongoDB 嵌套对象仍由任务 MQL 的 `$project` 确定性展开，数组由 `$unwind` 展开，再使用 Transfer 既有 `field_mapping` 完成目标字段名和类型映射；没有新增递归 JSON 自动摊平或 Outdoor 专用 Provider。

数仓分层配置已同步收敛为当前确实使用的三层：`ODS`（贴源层，排序 1）、`DWD`（明细层，排序 2）和 `DWS`（汇总层，排序 3），没有为尚未使用的 ADS 建立配置。ODS 的命名规范为 `ods_{domain}_{entity}`，并明确允许确定性的嵌套展开、数组拆行和类型规范化，但不承载业务口径加工；DWD 同时承载 `dwd_{domain}_{entity}` 事实明细和 `dim_{domain}_{entity}` 维度模型，DWS 使用 `dws_{domain}_{subject}`。这只是 Model 拥有的 Tenant 数仓分层分类事实，不把三张 Transfer ODS 物理表登记为 Model LogicalTable：ODS 物理表继续由 Transfer 任务生成，由 Meta/Catalog 发现和治理，避免 Model 与 Transfer 同时拥有 ODS DDL 和生命周期。

Standard 侧已完成旧码值集 Tenant/Domain 不一致的收敛：`outdoor_member_status` 已纠正到 Tenant 1 / 户外域，`gender` 已纠正到 Tenant 1 / 客户域；Standard Schema 迁移新增“Tenant 码值集必须有业务域”约束并已应用到运行库，同时纳入 PostgreSQL 门禁。“成员状态”数据元已通过正式修订生命周期发布 R2，值域类型为枚举，绑定 `outdoor_member_status` 已发布修订 R1；其编译质量规则包含六个允许值。重启后的迁移已移除 `current_revision_id` 持久化指针，并把 R1/R2 收敛为两个不重叠的半开生效区间，当前时点动态解析为 R2。该修正只发生在 Standard owner 内，没有为 Model、Transfer 或 Develop 新增 Standard 依赖。

最终持久资源如下：

| Owner | 资源 | ID/版本 | 最终用途 |
| --- | --- | --- | --- |
| Transfer | `outdoor_ods_persons_refresh` | `74` | MongoDB `Outdoor` 多 collection 查询 -> `outdoor.ods_outdoor_persons` |
| Transfer | `outdoor_ods_activities_refresh` | `75` | MongoDB `Outdoors` -> `outdoor.ods_outdoor_activities` |
| Transfer | `outdoor_ods_activity_members_refresh` | `76` | MongoDB `Outdoors.members[]` -> `outdoor.ods_outdoor_activity_members` |
| Develop | `outdoor_dim_person_from_ods_refresh` | `51` | ODS 人员、活动、成员关系 -> 人员维 staging |
| Develop | `outdoor_dim_activity_from_ods_refresh` | `52` | ODS 活动 -> 活动维 staging |
| Develop | `outdoor_dwd_participation_from_ods_refresh` | `53` | ODS 活动、成员关系 -> 参与事实 staging |
| Develop | `outdoor_dws_person_metric_refresh` / `outdoor_dws_person_pair_metric_refresh` | `49/50` | sealed DIM/DWD -> 两张 DWS staging |
| Model | 五张逻辑表 | `3/4/5/6/7`，版本 `49/21/20/28/33` | prepare、seal、组原子发布到 `outdoor` Schema |
| Model | `outdoor_governed_refresh` | 组 `1@13` | 五表同批原子发布 |
| Quality | `outdoor_governed_materialization_gate` | 任务 `1`、版本 `4`，绑定组 `1@13` | 10 项阻断级断言，含 `member_status` 枚举值域 |
| Quality | `Outdoor 成员状态质量检查` | RuleApplication `15`、CheckTask `10` | 按已冻结的数据元修订检查正式 DWD 字段 |
| Service | `outdoor_person_metric` / `outdoor_person_pair_metric` | 查询服务 `24/25` | 对外提供两张 DWS 的私有 REST 查询服务 |
| Orchestrator | `outdoor_governance_full_refresh` | 编排 `10` | 20 步唯一全量重算 DAG；支持手动执行，预留 Cron 但未配置业务调度周期 |

20 步 DAG 由三条无依赖 Transfer ODS 根节点、五条 Model prepare、五条 Develop 计算、五条 Model seal、一个 Quality gate 和一个 Model group publish 组成。三条 DIM/DWD Develop 任务通过各自 `type=relation` 的同名查询参数直接绑定 Transfer 的固定 ODS `target_locator`，通过 `target_locator` 绑定各自 Model prepare 的 staging；DWS、门禁和组发布继续使用 sealed batch 输出。原 17 步 DAG 中三条“Transfer 直接写 Model staging”的节点已删除。

真实执行过程中补齐了两个根因：

- Transfer 的执行契约虽然已声明 `execution_id / target_locator / row_count`，但固定目标成功路径没有把它们写入 `common.task_executions.metadata.outputs`。现已让表同步、水位增量和原始复制三类有界执行统一持久化稳定输出；固定目标 ODS 可被 Orchestrator 正常传递给 Develop。
- 参与事实 SQL 中 `m.person_id = a.leader_person_id` 在源活动无当前领队时会得到 `NULL`，与逻辑表的非空布尔字段冲突。任务 `53` 已改为 `COALESCE(..., FALSE)`；最终事实表三个布尔字段均无空值。
- ODS 保留 MongoDB 中文成员状态，Develop 任务 `53` 在 DWD 业务加工阶段确定性映射为 Standard 发布码值：`报名中 -> signup`、`领队 -> leader`、`领队组 -> leader_group`、`替补中 -> alternate`、`占坑中 -> hold`；当前参与事实口径仍排除仅浏览记录。Transfer 不解释 Standard，也没有新增对 Standard 或 Model 的依赖。

首轮成功父执行为 `f10767a9-703f-494c-85a4-f60e28b3c638`，开始于 `2026-08-28 15:48:26 +08:00`，完成于 `15:50:07`。20 个步骤全部成功，Quality 结果为 `passed=true`，9 项断言全部通过且 `failed_count=0`；组发布 execution 为 `dad629ca-5fab-46fe-a12b-3be30ee3e940`，五张逻辑表在 `2026-08-28 15:50:02 +08:00` 同批进入 `published`。

最终行数与数据约束证据：

| 层级 | 表 | 行数 |
| --- | --- | ---: |
| ODS | `ods_outdoor_persons` | 2,188 |
| ODS | `ods_outdoor_activities` | 2,383 |
| ODS | `ods_outdoor_activity_members` | 6,954 |
| DIM | `dim_outdoor_person` | 2,207 |
| DIM | `dim_outdoor_activity` | 681 |
| DWD | `dwd_outdoor_participation` | 4,946 |
| DWS | `dws_outdoor_person_metric` | 8,828 |
| DWS | `dws_outdoor_person_pair_metric` | 45 |

`dwd_outdoor_participation` 的 `is_signup / is_actual_participant / is_current_leader` 空值数为 0，`person_id + activity_id` 重复组数为 0。Transfer 三条执行输出的 `target_locator` 分别稳定指向三个正式 ODS 表，`row_count` 与上述 ODS 行数一致；Develop 五条执行输出的 `row_count` 与 DIM/DWD/DWS 行数一致。

新路线通过后，旧 Develop 任务 `47`（户外活动重叠度）和 `48`（`outdoor_dwd_activity_full_refresh`）已通过正式删除流程软删除，且删除前确认没有任何现存 Orchestrator 编排引用。生产侧不再保留旧任务入口或双轨路线。

2026-08-28 完成 PostgreSQL 命名空间收敛：由受控数据库操作一次性创建 `outdoor` Schema，Owner 为 `business`，撤销 `PUBLIC` 的 Schema 权限；Schema 的创建不属于 Transfer、Develop、Model 或 Orchestrator 的业务职责。Transfer 三个固定目标和 Model 五张逻辑表的物化目标均通过各自正式配置生命周期改为 `addp://engine/2/path/outdoor?type=schema&node_id=373`，Develop 继续只消费编排传入的 locator，Orchestrator DAG 没有新增 Schema 硬编码。

本次没有把旧业务表搬迁为最终数据。确认配置完成后，先删除 `public/outdoor` 中既有的 8 张 Outdoor 表，使两处旧表计数归零，再由编排 `10` 从 MongoDB 全量重建。重算父执行 `cef7516f-0b47-4694-a2fb-c5236887c821` 于 `2026-08-28 16:23:15 +08:00` 开始、`16:24:55` 成功完成，20 个步骤全部成功；Quality 执行 `ac79ba1f-53c9-4328-92f2-1db8f616f1ff` 成功，9 项阻断级断言全部通过；Model 组发布 execution 为 `17101a08-d387-4920-bcc4-14863f954cf5`，五张表同批进入 `published`。重建行数与上表完全一致，8 张物理表只存在于 `outdoor`，`public` 不再保留同名表。随后重扫 Meta：`outdoor` 的 8 个数据项均为 active，原 `public` 三个 ODS 数据项均为 deleted。

### 13.14 标准码值、字段质量与数据服务收口（2026-08-28）

Model 已把三个业务实体和三张 DIM/DWD 逻辑表所引用的数据元修订固化为审批时快照，并完成重新审批；当前逻辑表 `3/4/5/6/7` 的版本分别为 `49/21/20/28/33`，物化组仍为 `1@13`。这使业务定义引用稳定修订，但没有让 Develop、Transfer 或 Quality 直接依赖 Model。

Quality 通用物化门禁新增 `allowed_values` 断言后，正式门禁任务 `1` 已更新到版本 `4`，在原 9 项主键、非空、外键和指标粒度断言之外，增加 `dwd_outdoor_participation.member_status` 的六值枚举门禁。字段级治理同时建立 RuleApplication `15` 和 CheckTask `10`，二者绑定“成员状态”已冻结的数据元修订与正式 DWD 字段。

最终全量回归父执行为 `333bb5e7-d92e-447f-b563-fee22d608c24`，开始于 `2026-08-28 19:44:23 +08:00`，完成于 `19:46:04`。20 个步骤全部成功；Quality 门禁 execution `82120f15-cc5c-4bb5-bef8-6de61fc69b1a` 的 10 项断言全部通过，各项 `failed_count=0`；Model 组发布 execution `17891574-b849-4b42-a4f1-59a8c0ae3bfa` 成功。正式 DWD 共 4,946 行，成员状态分布为 `signup=4,076`、`leader=670`、`leader_group=142`、`alternate=34`、`hold=24`，空值或枚举外值为 0。字段质量 execution `7bfb63df-061b-4fb7-8797-e3fab5ff5e82` 进一步得到 `quality_score=100`、`failed_rules=0`、`failed_count=0`。

查询服务 `24`（`outdoor_person_metric`）和 `25`（`outdoor_person_pair_metric`）均处于 active/private 状态，分别绑定 `outdoor.dws_outdoor_person_metric` 和 `outdoor.dws_outdoor_person_pair_metric`。最终发布后两项服务的依赖快照检查均为“当前有效”，REST 数据预览均成功返回首批 20 行；`public` Schema 中仍无 Outdoor 同名表。至此，Outdoor 的 ODS 同步、DIM/DWD/DWS 计算、Model 受控发布、Quality 门禁、字段质量检查和查询服务已形成单一路线闭环。

### 13.15 MongoDB 结构整理易用性收敛与存量迁移（2026-08-31）

Transfer 的 MongoDB 基础结构整理已收敛为一类数据的通用配置，不包含 Outdoor 专用规则：用户只选择“一份文档形成一行”或“一个数组元素形成一行”、Meta 已识别的单个数组字段、需要输出的源字段，以及是否输出数组序号。单数组模式允许选择多个数组元素叶子字段，并允许选择多个不位于任何数组下的父文档叶子字段随每个元素行重复携带；不会同时展开多个数组。MongoDB `_id` 自动作为记录标识或父记录标识保留；基础模式只生成一个可选 `$unwind` 和一个 `$project`，不再向用户暴露仅为构造 MQL 服务的筛选、排序、投影别名、空值补齐或保留空数组选项。包含业务聚合或其他高级阶段的只读语句继续使用高级 MQL，但执行仍走同一条 Provider 主路径。

结构整理与 PostgreSQL 字段映射的职责已经拆开：结构整理步骤只决定输出行和源字段，系统为嵌套路径生成确定性的内部扁平名；字段映射步骤固定展示这些源字段，不允许混入未选择的 MongoDB 原始字段，也不能删除记录标识、父记录标识或数组序号。PostgreSQL 目标字段名、类型、可空性、格式和默认值只在字段映射步骤维护。因此 `activity_id` 不再是基础 MQL 中的用户别名，而是 `_id -> activity_id` 的目标映射；数组成员任务同理使用 `members__index -> member_index`。

存量任务 `74/75/76` 已通过 Transfer 正式配置界面迁移到该规范，并保留既有目标列和非空约束。三条新 MQL 均删除无效的 `_id` 非空 `$match` 和不影响快照语义的 `$sort`；任务 `74/75` 只保留 `$project`，任务 `76` 只保留 `$unwind members` 和 `$project`。迁移后分别执行成功：

| 任务 | execution | 读取/写入行数 | 重算前后内容 MD5 |
| --- | --- | ---: | --- |
| `74` 人员 ODS | `fbf092e8-4a4f-4a97-acb4-84af23a0974e` | 2,188 / 2,188 | `1889243fae686c0b7cddef86d5132a2f` |
| `75` 活动 ODS | `741f944b-e887-48ac-9a4a-bf86cd9dece2` | 2,383 / 2,383 | `75b8b269eefd5d338bde2f7e1f3871c0` |
| `76` 活动成员 ODS | `a811bc02-51f0-4d16-a98f-3e54e13902ff` | 6,954 / 6,954 | `908df099639df8d6c770771a01094d35` |

三张 ODS 表的行数和按稳定键排序计算的全表内容哈希均与迁移前一致，任务最终状态均为 `idle`，执行状态均为 `success`。本次只修改 Transfer 自有的查询构建、字段映射和任务定义，没有让 Transfer 或 Develop 感知 Model，也没有改变编排 `10` 的模块依赖关系。

最终端到端回归父执行为 `a644c024-f684-4151-97bd-71a095b5c281`，于 `2026-08-31 16:50:53 +08:00` 开始、`16:52:34` 成功完成。20 个步骤全部成功，分别为 Transfer 3 步、Develop 5 步、Model 11 步和 Quality 1 步。Quality 门禁 execution `30ac41d5-041f-4874-86a4-686f8f6301ab` 的 10 项断言全部通过，各项 `failed_count=0`；Model 组发布 execution `1c0cf616-2621-442d-bb97-47add2c11eb1` 成功原子发布五张逻辑表。最终表行数为人员维 2,207、活动维 681、参与事实 4,946、人员指标 8,828、人员对指标 45。发布后重新检查查询服务 `24/25`，两者均为 `active/private`、依赖快照“当前有效”，REST 数据预览均成功返回 20 行。

### 13.16 Transfer 确认体验、电话类型与最终回归收口（2026-08-31）

Transfer 任务确认页和任务详情页不再向用户展示 Engine ID，而是通过既有 System Engine Client 动态解析并展示 `Business MongoDB`、`Business PostgreSQL` 等引擎名称。确认页按任务与装载、源与目标、字段映射分区展示；源 MQL 和完整 JSON 默认折叠，并删除与页头重复的底部操作区。该改动只发生在 Transfer 前端，没有新增模块依赖或第二条 API 路线。

任务 `76` 补充成员昵称和电话后，第一次执行暴露 PostgreSQL Provider 的类型一致性缺口：`mixed` 字段的物理建表类型本来就是 `TEXT`，但既有列校验没有把 `mixed` 与 PostgreSQL `text` 视为兼容。Common PostgreSQL Provider 已收敛为单一规则：`string / mixed / unknown` 均以 `TEXT` 表示，`mixed` 目标可以继续写入既有 `text` 列；对应回归测试覆盖该事实。

源数据全量核验得到：6,954 个成员元素中，6,942 条电话和昵称均为字符串；8 条电话为三位正整数而昵称为字符串；4 条电话和昵称同时缺失，其中 2 条仍有 `personid`，另 2 条连 `userInfo` 和 `personid` 都缺失。因此电话在 MongoDB 物理画像中为 `mixed`，但业务语义仍是标识文本。任务 `76` 已通过正式配置流程把电话目标类型明确收敛为 `string / PostgreSQL TEXT`，真实缺失继续保存为 `NULL`，没有伪造默认值。最新独立执行 `7b788ae2-d095-41fa-b115-1beb3407926c` 成功写入 6,954 行，目标表电话和昵称各有 6,950 个非空值。

Standard 当前已有“手机号码”和“人员昵称”数据元，两者发布修订均明确允许为空，且没有发布非空质量规则。当前不存在把上述 4 条缺失判定为错误的业务依据，因此没有为了制造绿色或红色结论而新增 `not_null` 规则；Meta 字段覆盖率和 ODS `NULL` 已完整保留真实事实。若业务以后确认成员快照必须包含电话或昵称，应先修订并发布 Standard 数据元规则，再由 Quality 建立规则应用，不能在 Transfer 中解释质量语义。

任务配置收敛后再次执行唯一总编排 `outdoor_governance_full_refresh`。父 execution `ae667fad-b3ac-46e9-aa19-10e1d5d39466` 于 `2026-08-31 23:14:05 +08:00` 开始、`23:15:46` 成功完成，20 个子步骤全部成功。Quality execution `ec9c3ef8-058d-475b-a52f-0faad41a6f98` 的 10 项阻断级断言全部通过且 `failed_count=0`；Model 组发布 execution `081302c6-8e7c-44f7-913a-a15f4c648286` 成功。最终行数保持为 ODS 人员 2,188、ODS 活动 2,383、ODS 成员 6,954、人员维 2,207、活动维 681、参与事实 4,946、人员指标 8,828、人员对指标 45。查询服务 `24/25` 的依赖快照复查均为“当前有效”，REST 数据预览均成功返回 20 行。

### 13.17 DWD 参与语义与人员对分母修订（2026-09-01）

进一步对照 Standard 已审批指标定义与生产 DWD 后发现，旧任务 `53` 把仅来自 `Outdoors.leader.personid`、但不在有效 `members[]` 关系中的两条当前领队事实同时标记为 `is_signup=true` 和 `is_actual_participant=true`。这会让“实际参加活动数”与“当前负责或实际参加活动数”的生产结果都变成 4,888，掩盖两个集合本应存在的 2 条差异。问题不在 Transfer ODS 或 Model 结构，而在 Develop 对三类业务事实的布尔派生。

任务 `53` 已通过 Develop 正式配置接口收敛为单一语义：

- 有效成员关系继续按成员状态分别派生 `is_signup` 和 `is_actual_participant`；
- 当前领队来源只派生 `is_current_leader=true`；若同一人员-活动同时存在有效成员关系，最终分组的 `BOOL_OR` 仍会得到对应报名、实际参加标记；
- 仅有当前领队来源的关系保留在 DWD 中，但 `is_signup=false`、`is_actual_participant=false`，不再混入成员事实口径。

人员对任务 `50` 同步明确了两个不同职责：Top 10 候选仍按 `outdoor_responsible_or_actual_activity_count@4` 排序；双向重叠率的左右分母则分别读取 `outdoor_actual_participation_activity_count@4`，与交集所使用的实际参加集合保持同一口径。任务没有新增 Outdoor 专用执行能力，也没有改变 Develop、Model、Transfer 的模块边界。

修订后执行唯一总编排 `outdoor_governance_full_refresh`。父 execution `d0ee8ad2-966a-4ba0-ad70-7be2d1902ddc` 于 `2026-09-01 16:59:51 +08:00` 开始、`17:01:32` 成功完成，耗时 100,994ms；20 个子步骤全部成功，其中 Transfer 3 步、Develop 5 步、Model 11 步、Quality 1 步。Quality execution `99772cfd-dfe9-4bf9-9030-163a22157f16` 成功通过任务 `1@4` 的 10 项阻断级断言，Model 组发布 execution `cc2238e1-2d5c-4fc3-ad2b-c4e88cf5a3f5` 成功原子发布五张逻辑表。

新一轮生产结果如下：

| 校验项 | 结果 |
| --- | ---: |
| ODS 人员 / 活动 / 成员 | 2,188 / 2,383 / 6,954 |
| DIM 人员 / 活动 | 2,207 / 681 |
| DWD 人员-活动关系 | 4,946 |
| DWD 报名关系 | 4,944 |
| DWD 实际参加关系 | 4,886 |
| DWD 当前负责关系 | 577 |
| DWD 仅当前负责、非报名且非实际参加 | 2 |
| DWS 人员指标 / 人员对指标 | 8,828 / 45 |
| 实际参加 / 当前负责 / 负责或实际参加 / 报名指标总量 | 4,886 / 577 / 4,888 / 4,944 |
| 人员对实际参加分母不一致数 | 0 |
| 人员对双向比例公式不一致数 | 0 |

独立从 ODS 重算得到报名 4,944、实际参加 4,886、当前负责 577、负责或实际参加 4,888，与 DWD/DWS 完全一致。两条仅当前负责关系分别为 `W7Yv8Z25dhqgCt8g + 281fb4bf5d0eff7d067110722894dd00` 和 `W7rtxZ25dhqgFZtJ + 3b07eb945d10524e0708e91f6a49b02f`，两者均只保留 `is_current_leader=true`。`public` Schema 中仍无 8 张 Outdoor 同名表；查询服务 `24/25` 均保持 `active`，继续读取 `outdoor` Schema 中两张正式 DWS 表。

### 13.18 活动有效性、人员展示与即时重叠度收敛（2026-09-03）

经业务确认，Outdoor 生产口径从“四项人员预计算指标 + Top 10 人员对预计算”收敛为两项人员预计算指标和一项即时参数化指标：

- `当前主领队活动数`：人员出现在 `Outdoors.leader.personid` 的有效活动去重数，不把 `leader_group` 等同于主领队；
- `参加活动数（含主领队）`：当前主领队活动集合与 `members[]` 中 `signup | leader | leader_group` 成员活动集合的并集去重数；
- `两人活动重叠度`：调用时传入两个稳定人员 ID，基于上述“参加活动（含主领队）”集合即时返回共同活动数、双方活动数和两个方向的条件比例，不再预计算全部人员对。

“活动进行中”是时间推导事实，不增加独立活动状态码值。ODS 保留 MongoDB 原始状态；治理计算使用统一的 `is_effective_activity`：活动日期有效、已经开始，且原始状态不是 `拟定中` 或 `已取消`。因此源状态仍为 `已发布` 或 `报名截止`、但已经开始的活动按已成行参与计算，避免领队漏确认使指标失真。

Standard 新增活动状态码值集，保留并明确现有成员状态码值集；不新增领队码值集，因为主领队是活动与人员之间的权威关系，不是枚举。Model 中所有含人员的正式表同时保留稳定人员 ID 和昵称，ID 用于关联与计算、昵称用于展示；ODS 只保存源昵称快照，DIM 作为下游规范昵称来源。

最终标准成果固定为：

- 活动状态码值集 `outdoor_activity_status`，按源系统六个可观测状态发布 `draft=拟定中`、`published=已发布`、`registration_closed=报名截止`、`confirmed=已成行`、`completed=已结束`、`cancelled=已取消`；“活动进行中”由日期和状态共同推导，不作为第七个源状态；
- 成员状态码值集继续使用 `outdoor_member_status`，发布值保持 `signup | leader | leader_group | alternate | hold | browsing`。它表达人员在活动成员名单中的关系状态，也就是业务所称的“队员性质”；
- 不创建领队码值集。当前主领队只由 `leader.personid` 关系确定，`members[].entryInfo.status=领队组` 不能替代该关系；
- 人员预计算指标只保留 `outdoor_current_responsible_activity_count` 和 `outdoor_responsible_or_actual_activity_count`，展示名称分别收敛为“当前主领队活动次数”和“参加活动次数（含当前主领队）”；
- 即时重叠指标使用新代码 `outdoor_directional_participation_overlap_rate`。旧的实际参加、报名、批量人员对重叠指标定义在引用解除后删除，不保留兼容指标。

最终物理成果只保留四张 Model 逻辑表：`dim_outdoor_person`、`dim_outdoor_activity`、`dwd_outdoor_participation` 和 `dws_outdoor_person_metric`。其中 `dim_outdoor_activity` 增加标准化活动状态、`is_effective_activity`、当前主领队 ID 和昵称；`dwd_outdoor_participation`、`dws_outdoor_person_metric` 增加人员昵称。参与事实本身只保存有效活动中确实参加的人员以及当前主领队，因此删除只为旧指标服务的 `is_actual_participant`、`is_signup` 字段；是否为当前主领队继续由 `is_current_leader` 明确表达。`dws_outdoor_person_pair_metric` 及其 Model 组成员、Develop 任务、Quality 断言、Service 和 Orchestrator 节点全部删除。

Develop 只保留四条正式计算任务：人员维、活动维、参与事实和人员指标汇总。参与事实只写入 `is_effective_activity=true` 的活动；人员指标汇总每人只产生上述两个指标，并直接携带 `person_nickname`。活动状态中文源值到发布码值的映射发生在 Develop，Transfer ODS 继续原样保存源状态。

即时重叠度由 Service SQL 模式 Query Service 声明 `person_id_a`、`person_id_b` 两个必填字符串命名参数。Workbench 只消费 Consumer Descriptor：复用人员指标服务形成两个可选择的人员列表，通过 Selection Binding 写入两个 Application Parameter，再驱动重叠度 Component；Workbench 不读取 Engine、Model、Develop 或物理表。Service 只通过已发布的 PostgreSQL 表和 Engine Runtime 执行固定 SQL，不依赖 Model 或 Develop。

即时服务返回两人的 ID、昵称、共同活动数、各自参加活动数，以及 A 视角和 B 视角的条件比例；人员 ID 只用于精确关联，界面展示不再按 ID 二次查询昵称。查询服务 `outdoor_person_metric` 刷新发布快照后提供两项预计算指标；旧的 `outdoor_person_pair_metric` 删除，由 `outdoor_directional_participation_overlap` 参数化 SQL 服务唯一替代。

新路线发布后必须删除 `dws_outdoor_person_pair_metric` 逻辑表和物理表、Develop 人员对预计算任务、对应质量断言、Query Service 和 Orchestrator 节点；旧路线不得与即时查询并存。Standard、Transfer、Model、Develop、Quality、Service、Workbench、Orchestrator 和 Meta/Catalog 必须在同一轮刷新与验收中闭环。
