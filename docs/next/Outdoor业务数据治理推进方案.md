# 基于 Outdoor 业务文档的数据治理推进方案

> 本文说明如何把 [Outdoor 业务理解](Outdoor领域理解.md) 转化为 ADDP 中可治理、可计算、可供 Copilot 使用的语义资产。它是推进方案，不把尚未审核的候选对象直接当作平台事实。

## 1. 目标与边界

目标不是把 MongoDB 四个 collection 原样搬进 ADDP，而是建立一条可验证的链路：

```text
业务确认
  -> 物理事实核验
  -> Standard 语义资产
  -> Meta 物理字段绑定
  -> Model 逻辑实体与关系
  -> Model 准备 DIM/DWD/DWS 物化批次
  -> Transfer 执行只读 MongoDB MQL 并流式写入 DIM/DWD staging
  -> Develop 基于 DIM/DWD 计算 DWS 指标
  -> Quality 数据门禁
  -> Orchestrator 统一重算
  -> Copilot/Service 消费已发布指标结果
```

模块边界保持如下：

- `Standard` 拥有 Outdoor 业务域、术语、数据元、码值、单位、指标和定义文档；
- `Meta` 拥有 MongoDB collection、字段路径、动态 schema 采样事实和资源定位；
- `Model` 拥有租户级实体、实体关系、逻辑表、Standard 指标引用和逻辑表物化结构控制面，负责受控 DDL、staging 准备、结构校验与原子发布；
- `Transfer` 通过 bounded query-source 任务执行只读 MongoDB MQL，将嵌套 BSON 整形成扁平 DIM/DWD 行并流式写入 Model 为本次物化批次签发的 PostgreSQL staging；
- `Develop` 只在 PostgreSQL 内通过保存的 SQL 查询任务读取同批次 DIM/DWD staging 并计算指标汇总数据；生产指标不能旁路直查 MongoDB；
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
Model 准备物化批次与 staging
  -> Transfer bounded query-source 任务使用只读 MongoDB MQL 计算并搬运 DIM/DWD
  -> Develop SQL 查询任务只读本批次 DIM/DWD 并计算 DWS
  -> Quality 门禁
  -> Model 原子发布本次完整重算结果
```

Model 拥有物化结构和发布边界，只根据已审批模型生成受控 DDL；不接受任意 DDL。Transfer 负责 MongoDB 到 PostgreSQL staging 的跨引擎流式搬运，Develop 只负责 PostgreSQL 内的 DWS 查询计算；两者都不创建、删除或修改正式逻辑表。Orchestrator 只控制依赖、触发和执行追踪，不复制任务实现。所有 DWS 指标计算必须只读取本批次的 DIM/DWD staging，禁止直接读取 `Outdoor.Outdoors` 或 `Outdoor.Persons`。

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

MongoDB `title.level` 的真实值包含 `1.9`、`2.2` 等小数。`dim_outdoor_activity.activity_intensity` 和 `dwd_outdoor_participation.activity_intensity` 必须统一使用 `decimal`，由源端 MQL 显式转换，转换失败写 `NULL` 并交给 Quality 观测；不得使用 `int` 截断业务值。

### 8.1 Transfer 与 Develop 持久任务

第一批建立五个可独立审计和重试的持久任务：

1. `outdoor_dim_person_full_refresh`；
2. `outdoor_dim_activity_full_refresh`；
3. `outdoor_dwd_participation_full_refresh`；
4. `outdoor_dws_person_metric_full_refresh`；
5. `outdoor_dws_person_pair_metric_full_refresh`。

前三个任务属于 Transfer bounded query-source：MongoDB MQL 的最后阶段必须输出与 Model 逻辑字段同名的扁平字段；普通对象子字段通过 `$project` 投影，成员数组通过 `$unwind` 展开，多关系事实通过 `$group` / `$unionWith` 合并。Transfer 不提供递归 JSON 自动摊平。后两个任务属于 Develop PostgreSQL SQL 查询，只读取同一父编排下已完成的 DIM/DWD staging。

五个 writer 任务定义均不保存物化批次标识、逻辑表标识或物理目标名。Orchestrator 先执行对应 Model prepare，再把其 `staging_locator` 作为 writer 的 `target_locator`；Develop 的关系输入同样只绑定同一父编排中已 sealed 上游批次的 locator。Transfer 与 Develop 都不承担模型 DDL 或正式表替换；失败不能把半成品标记为成功。

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
| Transfer | 只读 MongoDB MQL、流式查询读取、DIM/DWD 跨引擎搬运和受限 staging 写入 |
| Develop | 只读同批次 DIM/DWD，在 PostgreSQL 内计算 DWS 并写入受限 staging |
| Quality | 主键、引用、关系口径和指标结果门禁 |
| Orchestrator | 单一 DAG、手动执行、定时执行和失败传播 |
| Query/MQL | 仅作为源事实金样，结果与 DWS 可复现一致 |
| 端到端 | 源数据 -> DIM/DWD -> DWS -> 质量门禁 -> 指标结果消费 |

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
| 6 | Transfer | 执行只读 MongoDB MQL，将扁平 DIM/DWD 结果流式搬运到 Model 受管 staging |
| 7 | Develop | 只基于同批次 DIM/DWD，在 PostgreSQL 内生成 DWS 数据 |
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
 -> Model 准备物化批次
 -> Transfer 通过只读 MQL 生成并搬运 DIM/DWD 到 staging
 -> Develop 基于同批次 DIM/DWD 计算 DWS
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

## 13. 首轮验证得到的能力缺口

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

结论：当前不需要修改 MongoDB 动态 schema 扫描器；Standard 数据元、Model 属性和首个指标的业务域绑定已完成，下一步进入可执行查询计划编译与事实门禁。

### 13.2 已配置的 Standard 数据元

已创建并审批：人员标识、人员昵称、活动日期、活动状态、最终强度、成员人员标识、成员状态；活动标识已创建并审批，并已绑定 `户外域`。此前编码误录的人员标识草稿已删除并按 `outdoor_person_id` 重建。

### 13.3 Model 绑定与审批结果

人员、活动、活动参与三个实体的关键属性已关联 Standard 数据元，并分别补齐主键属性；人员 -> 活动参与、活动 -> 活动参与两条一对多关系保持不变。三个实体均已审批通过。`actual_participation` 是由成员状态映射得到的派生属性，不直接绑定物理字段。

### 13.4 首个指标的真实数据验算

使用 `Outdoor.Outdoors` 的真实数据按已审批口径独立验算：排除 `拟定中`、`已取消` 和缺少 `title.date` 的活动；展开 `members[]`；仅保留 `报名中`、`领队`、`领队组`；按 `members.personid + Outdoors._id` 复合去重。2026-08-26 当前快照得到 681 个有效活动、583 个出现实际参加关系的人员、4,886 个去重后的人员-活动关系。示例最高值为人员 `W7cw8J25dhqgDMHA`（昵称“攀爬”）实际参加 286 个活动。此前记录的 1,099 人和 6,799 条关系混入了未应用有效活动过滤的结果，不能作为该指标口径的回归基线。

该结果证明业务口径和物理字段可以闭合；当前 Standard 指标已绑定 `户外域`，但尚未绑定可执行 MQL/查询计划，下一步要补确定性编译和结果回归，而不是继续增加指标数量。

下一步不再继续批量创建指标，而是把已审批指标接入可执行查询计划、Meta/Copilot 事实门禁和真实数据回归。

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

### 13.8 第二个指标：报名活动数（2026-08-25）

已在 Standard 正式创建并审批 `outdoor_signup_activity_count`（指标 ID `5`），所属业务域为 `户外域`。指标沿用首个指标的有效活动过滤和复合去重计划，仅将成员状态集合扩展为：

```text
报名中、领队、领队组、替补中、占坑中
```

`浏览中` 不计入报名关系。语义计算配置仍使用 `count_distinct_array_elements`，以 `members.personid + Outdoors._id` 去重；未使用 `Persons.entriedOutdoors[]` 作为主体事实源。该指标已通过审批，但尚未进行独立的真实数据结果回归；下一步应执行其 MQL 并与实际参加活动数做边界对照，确认替补和占坑关系确实只增加报名指标、不增加实际参加指标。

随后已在 Develop 查询工作台执行对应 MQL，使用同一 `Outdoor/Outdoors` Meta 资源和相同的有效活动过滤，执行成功，耗时 37ms；工作台展示前 500 行并标记结果截断。该执行只证明计划可运行，完整人员总量和报名/实际参加差异仍需通过全量汇总或专门对照查询回归，不以工作台的 500 行展示上限作为统计总量。

### 13.9 报名与实际参加边界回归（2026-08-25）

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

### 13.10 第三个指标：当前负责活动数（2026-08-25）

已在 Standard 正式创建并审批 `outdoor_current_responsible_activity_count`（指标 ID `6`），所属业务域为 `户外域`。指标以 `Outdoors.leader.personid` 作为当前主领队身份，按 `Outdoors._id` 去重；不使用创建账号 `_openid`，也不使用人员侧 `myOutdoors[]` 缓存索引。有效活动过滤与前两个指标一致。

为表达该指标，Copilot 增加通用语义操作 `count_distinct_documents`：`group_by` 声明文档归属字段，`distinct_by` 声明文档身份字段；确定性编译器补充非空身份过滤和两级 `$group`，模型不直接生成 MQL pipeline。当前快照独立回归得到 75 位当前主领队、577 条有效活动负责关系，最高人员 `W7cw8J25dhqgDMHA` 当前负责 224 个活动。

核对 Meta 时发现 `Outdoors` 动态 schema 正好达到 200 字段上限，旧扫描顺序会先耗尽在按字典序靠前的大型嵌套对象上，导致真实存在的顶层 `leader` 没有进入字段事实。扫描器已改为按层、按父路径交错采集，使有限字段预算优先覆盖不同顶层对象，再逐层补嵌套字段；没有增加 Outdoor 字段白名单，也没有提高字段数量上限。重新扫描后必须以 Meta 出现 `leader.personid` 作为第三个指标 Copilot 闭环的发布门禁。

2026-08-25 的第一次重扫仍未通过该门禁。进一步回归发现，字段预算虽然已改为跨头尾样本采集，但嵌套字段仍按单文档顺序扩展，第一份文档的深层字段会再次耗尽预算。扫描器随后改为两阶段跨样本采集：先合并所有样本的顶层字段，再按父路径和样本统一交错扩展嵌套字段。新增单元测试覆盖“第一份文档拥有大量嵌套字段、后续文档才出现 `leader.personid`”的场景；MongoDB 集成采样测试和 `go test ./common/engine/plugins/mongodb` 均通过。

第二次重扫于 `2026-08-25 15:27:01+08` 完成，Meta 查询结果为 `leader.personid` 存在（`t`），同时已登记 `leader`、`leader.entryInfo`、`leader.userInfo` 等字段。该结果满足第三个指标的物理字段发布门禁；后续指标 MQL 验证可以使用真实 Meta 资源继续推进。

随后用真实 Meta 字段集合编译并执行第三个指标的 MQL，执行成功。结果为 75 位当前主领队、577 条当前负责活动关系，人员 `W7cw8J25dhqgDMHA` 的去重活动数为 224，与独立 MongoDB 聚合回归一致。至此，第三个指标完成“Standard 定义 -> Meta 字段门禁 -> Copilot 确定性编译 -> MongoDB 执行 -> 结果对照”闭环。

### 13.11 第四个指标：当前负责或实际参加的不同活动数（2026-08-25）

已在 Standard 创建并审批 `outdoor_responsible_or_actual_activity_count`（指标 ID `7`，当前版本 `4`），所属业务域为“户外域”。指标粒度为人员，计算对象是两个集合的并集。其语义计算配置已通过指标详情页正式保存，数据库确认 `derivation_config` 为 JSON object，操作为 `count_distinct_document_and_array_elements`；不保存模型生成的 MQL 文本。

指标计算对象是两个集合的并集：

- `Outdoors.leader.personid` 代表当前主领队负责的活动；
- `Outdoors.members[]` 中状态为 `报名中`、`领队`、`领队组` 的成员代表实际参加的活动；
- 两个来源都应用有效活动过滤：排除 `拟定中`、`已取消` 和缺少 `title.date` 的活动；
- 以 `人员标识 + Outdoors._id` 去重后按人员计数，不能把两个指标结果直接相加。

为表达这一稳定的跨层集合语义，Copilot 新增通用操作 `count_distinct_document_and_array_elements`。编译器生成一条确定性 MongoDB 管道：成员数组分支先展开并过滤，当前主领队分支通过 `$unionWith` 合并，随后按人员和活动 ID 两级去重计数。该操作要求同时声明数组字段、数组分组字段、文档分组字段、活动身份字段和成员状态过滤，不接受 Outdoor 专用隐式规则。

当前 Outdoor 快照的真实聚合结果为 583 位人员、4,888 条人员-活动去重关系；人员 `W7cw8J25dhqgDMHA` 为 286 条。独立 MongoDB 聚合与 Copilot 编译后的管道结果一致。与实际参加关系 4,886 条相比，合并后增加 2 条去重关系，说明结果确实是集合并集而非计数相加。

### 13.12 双向实际参加活动重叠率（2026-08-25）

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

### 13.13 维度建模生产路线决策（2026-08-25）

经确认，第一批指标的生产计算统一改为基于维度建模成果：Model 准备物化批次和受限 staging，Transfer bounded query-source 任务使用只读 MongoDB MQL 生成并流式搬运 DIM/DWD，Develop PostgreSQL 查询任务基于同批次 DIM/DWD 计算 DWS，Quality 执行门禁后由 Model 原子发布，最终由一个 Orchestrator DAG 完成手动或定时全量重算。现有直接从 MongoDB 产出最终指标的 MQL 只保留为金样和回归证据，不再作为生产路线。

实施前的模型修订项为：

- `dwd_outdoor_participation` 增加 `is_signup` 和 `is_current_leader`，粒度改为“每个有效活动与人员组合一行，多种关系合并”；
- `dws_outdoor_person_metric` 和 `dws_outdoor_person_pair_metric` 统一为 `fact`；
- 为两张 DWS 补齐能表达人员、人员对、指标版本和统计范围的复合业务键；
- 五项 Standard 指标全部建立到对应事实表的指标映射；
- 建立三个 Transfer bounded query-source 任务、两个 Develop PostgreSQL 持久查询任务、Model 物化批次任务、Quality 门禁和唯一的 `outdoor_governance_full_refresh` 总编排；
- 替代任务验证通过后删除旧的“户外活动重叠度”直算任务，禁止保留双轨生产路线。

如果实施过程中发现现有模块缺少通用多输入关联、安全物化、跨表质量断言或共享批次上下文能力，必须先给出模块职责内的通用设计并确认，再修改代码。

### 13.14 当前改造进度与接力点（2026-08-26）

当前生产路线已经进一步收敛：MongoDB 文档中的嵌套 BSON/JSON 由只读 MQL 在源端确定性整形，普通对象子字段使用 `$project` 投影，数组使用 `$unwind` 展开，多来源关系按需要使用 `$unionWith` / `$group` 合并和去重，最后一个 `$project` 必须输出与 Model 逻辑表字段同名的扁平结果。Transfer 不提供通用递归 JSON 自动摊平，避免猜测数组粒度、字段命名、缺失值和目标类型。

本轮已经完成：

- 在通用 Engine Provider 中新增 `QueryReadSessionProvider` / `QueryReadSession`，并在能力声明中增加 `compute.query.read_session`；
- MongoDB Provider 已实现只读 `find` / `aggregate` 的流式游标读取，不附加工作台预览上限，并拒绝 `$out`、`$merge` 等写入阶段；
- Transfer planner/executor 已支持 bounded query-source，要求显式声明查询语言、查询文本、参数、输出字段映射和目标类型，并能逐批读取查询结果；
- 文档已同步明确：Transfer 负责跨引擎 DIM/DWD 搬运，Develop 只负责 PostgreSQL 内 DWS 计算；
- 局部验证已经通过：

```bash
cd common && GOWORK=off go test ./engine/plugin ./engine/plugins/mongodb
cd transfer/backend && GOWORK=off go test ./internal/planner ./internal/executor ./internal/models
git diff --check
```

2026-08-26 的模块边界复盘否定了上述 write-attempt 路线：即使调用收口位于 `common/client`，Transfer/Develop 保存 LogicalTable ID、调用 Model API 和持有 Model Permission 仍然构成业务语义强依赖。经确认后，唯一生产路线改为：

1. Model `materialization_prepare` 冻结逻辑表结构并创建本批唯一 staging，稳定输出 `batch_id + staging_locator`；
2. Transfer 使用通用 bounded query-source “写入已存在表”能力，Develop 使用通用“关系输入 -> 已存在表结果”能力；两者的 `target_locator` 及 Develop `input_locators` 均由 Orchestrator 从上游稳定输出绑定；
3. writer 只使用自身的通用 Engine read/write 授权，不认识 Model，稳定输出 `execution_id + target_locator + row_count`；
4. Model `materialization_seal` 根据 `batch_id + writer_execution_id + target_locator` 验证 writer 成功终态、同父编排和 Actor 血缘、staging 身份、字段结构与管理标记，成功后把批次置为 `sealed`；Model 不校验 writer 来自哪个业务模块；
5. Quality 门禁和 Model publish 只消费 sealed 批次；动态目标 writer 失败时整个编排失败，下次重算从新 prepare 开始，Model 回收失败或过期 staging。

因此当前改造必须删除 `materialization-write-attempts` 路由、实体、数据表、Common Client 方法、`model.materialization_write.execute`、Transfer/Develop 的 `MODEL_URL` 与全部 Model 字段/分支。本节记录的是已确认目标；在代码、迁移、Swagger 和门禁全部收敛前，不将该改造标记为完成。

Model PostgreSQL 标准门禁入口为：

```bash
ADDP_TEST_MODEL_POSTGRES_DSN='postgres://.../addp_test?sslmode=disable' make test-model-postgres
```

`develop-query-worker` 的独立进程与部署登记继续保留，但数据面要改为只消费 execution 中冻结的通用 `relation_inputs/input_locators/target_locator`，不创建 Model attempt、不读 Model context。所有 Orchestrator 查询租约过期都收敛失败，不为 Model 场景保留特例。

经讨论确认后，Model 已完成 `materialization-read-context/v1`、MaterializationGroup 主资源、`materialization_group_publish` TaskProvider 任务以及同一 PostgreSQL 事务内的多表原子交换；旧单表 publish 只保留给不属于任何物化组的逻辑表。组发布 PostgreSQL 门禁已覆盖组 CRUD 版本控制、多批次同一 execution、任一 staging 标记异常时全部旧目标保持不变、修复后一次性切换以及提交后幂等重试。

Develop 的同批次上游读取采用通用受控关系单路线：保存任务只在 `content.relation_inputs[]` 声明小写 alias，SQL 只引用 `addp_input.<alias>`；Orchestrator 将已 sealed 的 DIM/DWD locator 绑定到 `input_locators.<alias>`，将 DWS prepare locator 绑定到 `target_locator`。Query Worker 校验全部 locator 同属目标 PostgreSQL Engine，再通过 PostgreSQL AST 拒绝物理表、未声明输入和越界 CTE，并仅改写受控关系节点。ResourceLocator 只是 execution 参数，不进入 Develop 任务定义。

Quality 类型化 `materialization_gate`、MaterializationGroup 发布交接和真实 PostgreSQL 五类断言门禁已经完成。门禁成功稳定输出物化组 ID/版本，Model 组发布在入队和实际 DDL 发布前双重校验 `expected_group_id + expected_group_version`；该策略只约束本 Outdoor 总编排，不扩大为其他物化组的全局强制政策。

真实数据复核后进一步收敛模型：活动维度与参与事实的 `activity_intensity` 已从 `int` 修订为 `decimal`，避免截断 MongoDB 中的小数最终强度；两张 DWS 的重复 `run_id` 已删除并重新审批。执行 lineage 只保留在统一 execution 和 Model MaterializationBatch 控制事实中，不进入 DWS 业务行，不新增 Orchestrator 上下文注入功能。

三条源端 MQL 已使用 MongoDB 当前快照完成全量只读回归：人员维度 2,188 行，活动维度 681 行，活动参与事实 4,946 行；其中 `is_actual_participant = true` 为 4,888 行、报名关系 4,946 行、当前主领队关系 577 行。三个输出的日期和强度转换均无空值。活动参与事实中有 19 个不同人员标识在 `Persons` 中不存在，这是待治理的真实数据异常；当前正式 Quality 门禁将人员外键也设为阻断级 `error`，不得为了让首轮重算通过而静默降级为 `warning`。这些数字仅作为 2026-08-26 当前快照的迁移回归证据，不进入业务定义。

2026-08-27 已建立待统一编排的正式任务与门禁：Transfer 任务 `74/75/76` 分别生成人员维度、活动维度和参与事实；Develop 任务 `49/50` 分别计算人员指标和 Top 10 人员对指标；Model 物化组 `outdoor_governed_refresh` 的 ID 为 `1`、版本为 `1`，成员为逻辑表 `3/4/5/6/7`；Quality 门禁 `outdoor_governed_materialization_gate` 已配置 9 项断言。旧 Develop 任务 `47/48` 只能在新编排真实重算验证通过后删除，不保留双轨生产路线。

Model 另已修复“空 `partition_by` 被误判为分区表”的配置语义分裂：未分区物化在前端请求与后端持久化中都同时省略 `partition_by/partition_type`，TaskProvider 和 prepare 只按非空 `partition_by` 识别分区设计，`015_normalize_empty_partition_materialization` migration 负责清理已有空值配置。Model Go 全量测试、前端单测/E2E/构建及 PostgreSQL 标准门禁已通过；待 Model 服务重启应用 migration 后，继续创建 `outdoor_governance_full_refresh` 唯一 DAG 并执行真实全量回归。

### 13.15 会话接力清单（2026-08-27）

#### 不可回退的架构决策

- Transfer 和 Develop 都不认识、不调用、不依赖 Model，任务定义不保存 LogicalTable ID、MaterializationBatch ID 或 Model 业务上下文。
- Model 是逻辑表结构和物化生命周期的唯一 owner，负责 prepare、受控 DDL、seal、结构校验、组原子 publish 和 staging 回收。
- Transfer/Develop 只执行通用“读取 ResourceLocator -> 写入已存在 ResourceLocator”数据面能力；不执行 DDL、truncate、drop 或正式表替换。
- Orchestrator 通过 TaskProvider 稳定 `outputs` 和运行时 `parameters` 显式绑定不同 owner 的任务，是唯一跨模块组合层；禁止回到 Common Client 中的 owner-specific 写协议。
- Quality 仅通过受限 Model Client 读取同一父编排中的 sealed staging；组发布必须绑定门禁输出的物化组 ID 和版本。
- 生产路线只允许一条。新 DAG 真实重算和校验通过后删除旧 Develop 任务 `47/48`，不保留兼容分支或双轨入口。

#### 当前已持久事实

| Owner | 资源 | ID/版本 | 关键契约 |
| --- | --- | --- | --- |
| Model | `dwd_outdoor_participation` / `dim_outdoor_activity` / `dim_outdoor_person` | `3/4/5` | DIM/DWD 逻辑表，已审批 |
| Model | `dws_outdoor_person_metric` / `dws_outdoor_person_pair_metric` | `6/7` | DWS 逻辑表，已删除 `run_id` 并重新审批 |
| Transfer | `outdoor_dim_person_refresh` / `outdoor_dim_activity_refresh` / `outdoor_dwd_participation_refresh` | `74/75/76` | bounded MQL query-source，runtime `target_locator`，append 到已存在 staging |
| Develop | `outdoor_dws_person_metric_refresh` | `49` | `relation_inputs=[person,participation]`，只读 `addp_input.*` |
| Develop | `outdoor_dws_person_pair_metric_refresh` | `50` | `relation_inputs=[person_metric,participation]`，只读 `addp_input.*` |
| Model | `outdoor_governed_refresh` | 组 `1`，版本 `1` | 成员顺序 `5,4,3,6,7`，同一 PostgreSQL Engine 原子发布 |
| Quality | `outdoor_governed_materialization_gate` | 任务 `1`，版本 `1` | 绑定物化组 `1@1`，9 项 `severity=error` 断言 |

Quality 当前 9 项断言是：人员主键唯一、活动主键唯一、参与事实的人员/活动标识非空、参与复合键唯一、参与到人员/活动的两项外键、人员指标粒度唯一、人员对指标粒度唯一。当前未配置 `predicate_implication` 或 Top 10 结果 45 行的 `row_count` 断言；Quality 模块已具备这些通用能力，但是否加入本门禁应在首轮运行证据出来后明确决定，不由接力会话自行扩展。

#### 当前运行态（接力前必须重新核验）

- 代码已新增 Model migration `015_normalize_empty_partition_materialization.up.sql`，但当前运行库的最新 migration 仍是 `014_replace_write_attempt_with_seal.up.sql`。
- 逻辑表 `3/4/5/6/7` 的当前运行库记录仍带有空 `partition_by` 和 `partition_type=range`，因而旧 Model 进程不会把它们发布为 prepare/seal 任务。
- System 中 Model TaskProvider 声明已正常持久（`module_definitions.version=3`），包含 prepare/seal/single publish/group publish 四类能力。
- `outdoor_governance_full_refresh` 还没有持久化，当前 Orchestrator 同名记录数为 `0`。
- 工作区包含多模块未提交改动，均视为用户和前续会话的有效成果；接力时不得 reset、checkout 或覆盖无关改动，也不得未经授权提交。

Model 重启后先执行三项只读验证：

1. `model.schema_migrations` 最新版本为 `015_normalize_empty_partition_materialization.up.sql`；
2. 逻辑表 `3/4/5/6/7` 的 `materialization` 均已省略两个分区键；
3. Orchestrator 任务库中 Model prepare 和 seal 各出现 `5` 个可用任务，group publish 出现物化组 `1`。逻辑表 `2` 没有完整物化目标，不应出现在 prepare/seal 列表中。

#### 待创建的唯一 DAG

Orchestrator 内部绑定必须使用完整字符串 `{{step_id.outputs.path}}`，且被引用步骤必须同时出现在当前步骤的直接 `depends_on` 中。按下表创建：

| Step ID | Provider / Task | `depends_on` | 运行时 parameters |
| --- | --- | --- | --- |
| `prepare_person` | `model/materialization_prepare/5` | 无 | `{}` |
| `write_person` | `transfer/sync/74` | `prepare_person` | `target_locator={{prepare_person.outputs.staging_locator}}` |
| `seal_person` | `model/materialization_seal/5` | `prepare_person,write_person` | `batch_id={{prepare_person.outputs.batch_id}}`; `writer_execution_id={{write_person.outputs.execution_id}}`; `target_locator={{write_person.outputs.target_locator}}` |
| `prepare_activity` | `model/materialization_prepare/4` | 无 | `{}` |
| `write_activity` | `transfer/sync/75` | `prepare_activity` | `target_locator={{prepare_activity.outputs.staging_locator}}` |
| `seal_activity` | `model/materialization_seal/4` | `prepare_activity,write_activity` | 同上，绑定本链 prepare/write 输出 |
| `prepare_participation` | `model/materialization_prepare/3` | 无 | `{}` |
| `write_participation` | `transfer/sync/76` | `prepare_participation` | `target_locator={{prepare_participation.outputs.staging_locator}}` |
| `seal_participation` | `model/materialization_seal/3` | `prepare_participation,write_participation` | 同上，绑定本链 prepare/write 输出 |
| `prepare_person_metric` | `model/materialization_prepare/6` | 无 | `{}` |
| `write_person_metric` | `develop/query/49` | `prepare_person_metric,seal_person,seal_participation` | `target_locator={{prepare_person_metric.outputs.staging_locator}}`; `input_locators.person={{seal_person.outputs.staging_locator}}`; `input_locators.participation={{seal_participation.outputs.staging_locator}}` |
| `seal_person_metric` | `model/materialization_seal/6` | `prepare_person_metric,write_person_metric` | 绑定 `prepare_person_metric` 的 batch 与 `write_person_metric` 的 execution/target |
| `prepare_pair_metric` | `model/materialization_prepare/7` | 无 | `{}` |
| `write_pair_metric` | `develop/query/50` | `prepare_pair_metric,seal_person_metric,seal_participation` | `target_locator={{prepare_pair_metric.outputs.staging_locator}}`; `input_locators.person_metric={{seal_person_metric.outputs.staging_locator}}`; `input_locators.participation={{seal_participation.outputs.staging_locator}}` |
| `seal_pair_metric` | `model/materialization_seal/7` | `prepare_pair_metric,write_pair_metric` | 绑定 `prepare_pair_metric` 的 batch 与 `write_pair_metric` 的 execution/target |
| `quality_gate` | `quality/materialization_gate/1` | `seal_person,seal_activity,seal_participation,seal_person_metric,seal_pair_metric` | `{}` |
| `publish_group` | `model/materialization_group_publish/1` | `quality_gate` | `expected_group_id={{quality_gate.outputs.materialization_group_id}}`; `expected_group_version={{quality_gate.outputs.materialization_group_version}}` |

编排创建后先保存为未定时状态，手动执行一次；第一轮必须从 Monitor 核对所有子 execution 共用同一父 execution、五个 batch 均进入 sealed、Quality 结果与组发布是否按门禁结果停止或继续。当前 19 个悬空人员引用很可能使 `quality_gate` 失败；这是预期的治理证据，应停下讨论“修正源数据、补齐人员维度，还是显式改变门禁政策”，不得为追求绿色执行而无证据降级。

#### 验证与收尾

- Model 空分区修复已通过：`cd model/backend && go test ./... -count=1`、`make test-model-frontend`、`ADDP_TEST_MODEL_POSTGRES_DSN='postgres://addp:addp_password@localhost:15432/addp_test?sslmode=disable' make test-model-postgres`、`make test-platform`。
- 真实 DAG 成功后，必须查验五张正式表的行数、结构指纹、Model 管理标记、指标粒度和 45 对 Top 10 结果，并记录 execution ID 作为回归证据。
- 只有新路线端到端通过后才删除 Develop `47/48`；删除前再根据当时数据状态向用户确认精确目标，删除后重跑受影响的 Develop/Orchestrator 门禁。
- 最后把编排 ID、首次成功 execution ID、实际行数、Quality 结果、旧任务删除结果和是否启用 Cron 补回本节，再将本专题标记为完成。

### 13.6 Model 逻辑表与星型关系（2026-08-25）

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

### 13.7 Meta -> Develop 首个指标执行回归（2026-08-25）

在已登录 Console 中，通过 Develop 查询工作台选择 Business MongoDB、MQL 和 `Outdoor` 数据库，执行与首个指标计划一致的确定性查询。查询使用 Meta 已验证的 `Outdoors` collection，并按以下顺序执行：活动状态和日期有效性过滤、展开 `members`、成员状态过滤、人员与活动复合去重、按人员计数。

本次真实执行结果：请求成功，执行耗时 61ms，返回 500 行（工作台结果展示上限为 500 行，结果标记为截断）；结果中包含人员 `W7cw8J25dhqgDMHA` 的 `actual_activity_count = 286`，与独立回归结果一致。Develop 执行日志确认目标定位为 `addp://engine/11/path/Outdoor?type=database`，执行状态为 `success`。这证明 Meta 资源事实、MQL 预检、MongoDB 执行器和结果回传已经形成闭环；全量人员数量应通过专门的汇总查询或导出处理，不能把“返回行数 500”误当作人员总数。

同时，在同一工作台使用 AI 查询助手提交相同业务描述，资源发现阶段正确列出 `Groups`、`Outdoors`、`Persons`、`Photos` 并要求确认 `Outdoors`。确认后的第二次请求最终返回 HTTP 200，但本地 `qwen3.8:27b-mlx` 结构化推理耗时约 58.9 秒，网关总耗时约 59.1 秒；因此页面长时间显示生成中是低延迟体验问题，不是资源发现失败或 API 丢响应。当前查询助手仍应补充明确的长耗时状态提示或异步执行体验，但这不阻塞确定性计划编译和指标回归。
