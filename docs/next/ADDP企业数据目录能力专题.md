# ADDP 企业数据目录与 Catalog 模块专题

更新时间：2026-08-27

状态：阶段 0、阶段 1、阶段 2、阶段 3 与阶段 4 的代码收敛已完成；阶段 5 正在持续推进，Model、Standard Metric、Service QueryService 与经过筛选的 Develop 成果已完成

## 一、文档定位

本专题持续跟踪 ADDP 企业数据目录（Enterprise Data Catalog，EDC）及独立 `Catalog` 模块的架构决策、待确认事项和实施进度。

本文件位于 `docs/next/`，用于承载正在推进的设计，不直接替代正式概念和规范。进入代码实现前，已经稳定的概念必须同步进入：

- `docs/concepts/addp术语表.md`；
- `docs/concepts/addp核心概念关系图.md`；
- `docs/concepts/addp模块架构图.md`；
- 后续新增的 Catalog 概念文档与实现规范；
- 受影响模块的 `CLAUDE.md`、API、Swagger、测试和 CI/CD 门禁。

本专题不讨论底层数据复制，不把企业数据目录等同于资产门户，也不以兼容现有 Asset 自动发现或 Meta 搜索实现为目标。

### 1.1 本轮归拢后的主线

后续研发必须始终围绕下面这一条主线推进：

```text
Meta 自动发现并维护技术 DataItem
    → Catalog 建立企业目录身份并承载业务编目与语义关联
    → Asset 从 Catalog 选择、组合并发布资产
    → Portal 面向消费者提供已发布资产
```

其中 Standard 只定义可复用业务语义，Manager 只提供技术浏览和数据使用能力，System/IAM 只提供身份、组织、授权上下文和模块控制面。任何把业务元数据重新写回 Meta、让 Asset 绕过 Catalog 自动发现专业资源，或把 Manager 技术资源树直接扩展为企业目录树的实现，均视为偏离本专题主线。

阶段 0 正式术语与模块边界、阶段 1 实现契约、Catalog 模块核心闭环、组织语义协作以及 Manager / Asset / Portal 调用方收敛已经完成。当前代码已具备 Meta 可恢复变化源、自动建档、原子业务编目、精确语义/责任校验、显式来源重绑、不可变历史、Catalog 专属搜索投影、个人目录标记、Project Group 目录集合、Manager owner 内容索引和 AssetComponent 多条目发布主路径。旧 discoverable、Meta `AssetRecord`、Asset 自动发现与 `source_reference` 已删除，不得恢复双轨来源模型。

## 二、问题背景

ADDP 已经有多个管理数据和数据相关成果的专业模块：

- Meta 管理数据项身份、技术元数据、扫描、资源树和血缘；
- Standard 管理业务域、业务术语、数据元、指标、分类和分级；
- Model 管理业务实体、逻辑模型、数仓分层和模型关系；
- Quality 管理规则应用、质量检查、评分和问题治理；
- Service 管理已发布数据服务；
- Develop 管理查询、工作流和 Notebook 等开发成果；
- Manager 管理数据预览、检索、剖析和快显；
- Asset 管理数据资产的编目、发布、授权、评价和运营；
- Portal 面向消费者展示和申请已发布资产；
- System/IAM 管理 Tenant、Department、Project Group、User、Role 和授权上下文。

这些模块分别拥有专业事实，但当前缺少一个跨专业目录的统一关联与发现层，无法稳定回答：

> 企业有哪些数据、模型、指标和服务？它们是什么意思？由谁负责？质量如何？相互之间有什么关系？哪些已经完成治理，哪些已经作为资产发布？

此前 Meta 搜索、Manager 检索和 Asset 自动发现分别承担了一部分目录能力，导致“技术资源目录、企业数据目录和资产目录”边界不清。根因不是单个字段或接口缺失，而是 ADDP 尚未建立企业数据目录这一独立架构边界。

## 三、已确认的架构决策

以下决策已达成共识，后续设计不再并行保留其他路线：

1. ADDP 新增独立 `Catalog` 模块，承载企业数据目录能力。
2. Meta 与 Standard 之间不存在直接依赖关系；二者分别拥有技术事实和语义定义。
3. Catalog 通过专业模块的公开读契约和变化契约消费事实；专业模块不反向依赖 Catalog。
4. 业务语义定义仍归 Standard；语义定义与具体专业资源的关联事实归 Catalog。
5. 关联事实只在 Catalog 保存一份权威记录，不在 Meta 或 Standard 保存业务投影副本。
6. Catalog 数据库是目录身份、关联和治理事实源；Meilisearch 等搜索索引只是可重建投影。
7. 所有已被 Meta 正式识别并持久化的 DataItem 自动创建最小 `CatalogEntry`。
8. 第一次业务编目更新已有 CatalogEntry，不能创建第二个业务目录对象。
9. Department、Project Group 和个人工作视图影响责任、协作、权限和展示，不决定 CatalogEntry 是否存在。
10. CatalogEntry 不归属于某个 Workspace；个人或项目工作区只能引用它。
11. Manager 继续负责技术资源浏览、数据内容预览、剖析、快显和内容检索，不拥有企业业务目录事实。
12. Asset 从 Catalog 选择并组合一个或多个目录对象形成资产；Portal 只消费已发布资产。
13. 技术资源搜索、企业元数据搜索、内容检索和已发布资产搜索分别由对应 owner 管理，不能共用含义不清的“资产索引”。
14. Catalog 进程只在 System 注册成功后进入 Ready；Meta、Standard、Manager、Asset 等专业模块都不是 Catalog 的启动或 Ready 强依赖。对其他 owner 的同步失败进入可重试 reconciliation，不得让 Catalog 与专业模块形成循环启动。

以上“已确认”指概念边界和依赖原则已经确认，不代表实体字段、API、变化传播协议、权限规则或代码命名已经确认。后者必须按第十七节分阶段设计，不能由实现反向定义概念。

## 四、核心概念边界

### 4.1 专业资源目录

专业资源目录由各 owner 模块维护，回答某一类资源“有哪些、如何组织、有哪些专业事实”。例如：

- Meta 的技术资源目录；
- Standard 的术语、数据元和指标目录；
- Model 的业务实体和逻辑模型目录；
- Service 的已发布服务目录；
- Develop 的可复用开发成果目录。

专业模块继续拥有完整专业事实。Catalog 不接管其 CRUD、生命周期和详细数据模型。

### 4.2 企业数据目录

企业数据目录不是另一棵资源树，也不是专业资源的完整副本。它负责：

- 企业级稳定目录身份；
- 专业资源与目录身份的来源绑定；
- 业务语义与具体资源的关联；
- 责任、治理状态和业务分类；
- 跨模块关系；
- 统一搜索、筛选和关系导航；
- 权限感知的企业目录视图。

```text
专业模块事实
    + CatalogEntry 企业目录身份
    + 业务语义和责任关联
    + 跨模块关系
    + 搜索、筛选和权限视图
    = 企业数据目录能力
```

### 4.3 数据资产目录

数据资产目录是已经完成业务定义、责任确认、治理和正式发布的运营成果视图。

Asset 负责资产身份和版本、多目录对象的组合边界、发布与上下架、使用条件，以及申请、授权、评价和运营。资产不复制 CatalogEntry 的来源绑定和专业事实。

一个资产可以组合多个 CatalogEntry，一个 CatalogEntry 也可以被多个资产复用。

### 4.4 Engine Catalog 与企业 Catalog

ADDP 引擎体系中的 `EngineCatalogProvider`、catalog path 或数据库 catalog 表示引擎原生命名空间或扫描能力；本专题中的 `Catalog` 表示企业数据目录模块。

正式进入术语表时必须分别定义：

- Engine Catalog：数据引擎原生命名空间；
- Enterprise Data Catalog：跨专业目录的统一关联与发现能力；
- Catalog module：ADDP 中实现企业数据目录能力的 owner 模块；
- CatalogEntry：企业数据目录中的稳定对象身份。

当前术语表已将 `EngineCatalogProvider`、`EngineCatalogPath`、`EngineCatalogEntry` 和 `EngineCatalogFacts` 定义为引擎层词族，并把裸名 `Catalog` 与 `CatalogEntry` 保留给企业数据目录。生产代码的公共契约、调用方、Swagger 和 Python SDK 已按该边界迁移，不保留旧名兼容类型。

具体影响范围、迁移批次和门禁见 [ADDP Engine Catalog 命名收敛与迁移专题](ADDP引擎目录命名收敛专题.md)。企业 Catalog 的命名阻塞已解除；后续可按本专题阶段 1 设计企业 `CatalogEntry` 契约，不得回用引擎实时目录 DTO。

## 五、模块职责和依赖方向

Meta 与 Standard 各自独立，不通过彼此完成扫描或语义定义。Catalog 位于专业事实之上，Asset 和 Portal 位于 Catalog 的资产化与消费侧。

```mermaid
flowchart LR
    System["System / IAM\nTenant、组织、主体和授权上下文"]
    Meta["Meta\nDataItem 与技术元数据"]
    Standard["Standard\n业务域、术语、数据元、指标"]
    Model["Model\n实体与逻辑模型"]
    Quality["Quality\n质量结果"]
    Service["Service\n已发布服务"]
    Develop["Develop\n开发成果"]
    Catalog["Catalog\n企业目录身份、关联、责任、搜索"]
    Manager["Manager\n资源浏览、内容预览与检索"]
    Asset["Asset\n资产组合、发布与运营"]
    Portal["Portal\n已发布资产消费"]

    System -->|"AuthContext 与组织事实"| Catalog
    Meta -->|"公开事实与变化契约"| Catalog
    Standard -->|"公开事实与变化契约"| Catalog
    Model -->|"公开事实与变化契约"| Catalog
    Quality -->|"公开质量摘要"| Catalog
    Service -->|"公开事实与变化契约"| Catalog
    Develop -->|"公开事实与变化契约"| Catalog
    Meta -->|"ResourceLocator 与技术事实"| Manager
    Catalog -->|"目录摘要与导航"| Manager
    Catalog -->|"可选择的目录对象"| Asset
    Asset -->|"已发布资产"| Portal
```

图中箭头表示消费方向，不表示跨 Schema 查询。所有跨模块读取必须通过公开 API、统一 Client 或后续确定的单一变化契约完成。

依赖强度统一解释如下：

| 依赖关系 | 强度与运行契约 |
| --- | --- |
| Catalog → System | 唯一业务模块级强依赖；Catalog 可以先启动为 Alive，但只有完成 System 注册并取得有效控制面资格后才能 Ready |
| Catalog → 自身必需 Infra | 部署依赖；Catalog 自有 PostgreSQL、搜索基础设施等不可用时可以 Not Ready，不属于业务模块耦合 |
| Catalog → Meta / Standard / 其他专业 owner | 非启动、非 Ready 强依赖；只在同步、校验或查询当前事实时按请求调用，失败只影响本次操作并进入重试或 reconciliation |
| Manager / Asset → Catalog | 非启动、非 Ready 强依赖；Catalog 不可达时对应企业目录摘要、资产选源等请求明确失败或降级，不能回退到旧发现路径 |
| 任意业务模块 → 另一业务模块数据库 | 禁止；不得跨 Schema 查询、建立跨模块数据库外键或把对方私有表作为本模块启动条件 |

“没有运行层强依赖”不等于禁止业务调用，而是要求模块能够独立完成启动和 Ready 判断，跨模块不可达只影响真正需要该能力的当前请求或后台同步。不得以本地副本、硬编码地址或旧 API fallback 把软依赖重新变成双轨事实源。

### 5.1 Catalog 拥有的事实

Catalog 拥有：

- CatalogEntry 企业目录身份；
- CatalogEntry 与专业资源的来源绑定及历史；
- 资源特定的业务名称、业务描述和治理状态；
- 资源与 Standard 语义对象的关联；
- 责任部门、业务责任人、数据管理员和协作范围；
- 跨专业目录关系及其来源、版本和证据；
- 企业目录搜索所需的可重建索引；
- 收藏、关注、治理队列、编目草稿和目录集合等 Catalog 内协作事实。

### 5.2 Catalog 不拥有的事实

Catalog 不拥有：

- DataItem 的技术结构、路径、格式和扫描状态；
- 业务术语、数据元、指标和业务域的定义；
- 数据质量规则、执行和问题；
- 数据内容、预览结果、剖析结果和快显产物；
- 资产发布、申请、授权、评价和运营；
- Department、Project Group、User 和成员关系；
- 专业模块内部的完整对象副本。

## 六、CatalogEntry 身份模型

### 6.1 企业身份与技术身份分离

Meta DataItem fingerprint 是技术资源身份，用于在同一路径和引擎内稳定识别已扫描对象。路径重命名、移动或引擎替换会产生新的技术身份。

`catalog_entry_id` 是企业目录身份，独立于当前物理位置，用于承载长期业务说明、责任、语义关联和治理历史。

```text
CatalogEntry --represents--> Meta DataItem
CatalogEntry --belongs-to-domain--> Standard Domain
CatalogEntry --applies-term--> Standard Glossary Term
CatalogComponent --implements--> Standard Element
```

来源绑定属于 Catalog。Meta 不保存 `catalog_entry_id` 投影，Standard 也不保存反向资源列表。

### 6.2 来源绑定

来源绑定至少需要表达：

- `tenant_id`；
- `catalog_entry_id`；
- `source_module`；
- `source_type`；
- `source_identity`；
- 当前状态和生效时间；
- 失效原因、替换关系和历史版本。

对 Meta DataItem，`source_identity` 使用 Meta 对外稳定提供的 fingerprint，不把数据库行 ID、路径字符串或临时扫描 ID 作为企业身份。

同一个当前专业资源只能有一个有效的 CatalogEntry 来源绑定。历史绑定可以保留，但不能同时形成两个当前目录身份。

### 6.3 重命名、移动和显式重绑

自动扫描不得通过名称相似度、结构相似度或模糊匹配猜测资源重命名。

默认行为：

1. 原 fingerprint 不再出现时，原来源绑定进入 `missing`；
2. 新 fingerprint 出现时，自动创建新的 `discovered` CatalogEntry；
3. 治理人员确认两者是同一业务资源后，执行显式重绑；
4. 重绑事务合并或终止新建的临时目录身份，把新来源绑定到原 CatalogEntry，并保留历史证据。

显式重绑的详细状态机、冲突处理和审计要求仍需单独设计，但不能引入自动模糊跟随路线。

## 七、DataItem 自动建档决策

### 7.1 创建范围

所有被 Meta 正式识别并持久化的 DataItem 都自动创建最小 CatalogEntry，包括尚未完成业务编目的对象。

不自动创建顶级 CatalogEntry 的对象：

- 资源树 node、数据库目录节点和文件夹；
- ScanTask、execution 和扫描日志；
- 缓存、临时文件和模块内部配置；
- 字段和 DataItem component。

字段和组件作为 CatalogEntry 的下级 `CatalogComponent` 管理，用于字段级语义关联和搜索，但默认不是顶级企业目录对象。

如果某类对象不应进入企业资源盘点，应在 Meta 扫描范围或 DataItem 识别规则中排除。Catalog 不建立第二套“扫描到了但不建档”的永久过滤规则。

### 7.2 自动创建语义

Meta 扫描完成后，Catalog 通过公开变化契约幂等执行：

```text
ensure CatalogEntry(
    tenant_id,
    source_module = meta,
    source_type = data_item,
    source_identity = item_fingerprint
)
```

自动创建只建立最小身份和来源绑定，不代表已经业务编目、业务认证、租户全员可见、获得内容访问权、形成资产或发布到 Portal。

### 7.3 为什么不在第一次业务编目时创建

如果等到第一次人工编目才创建 CatalogEntry，会产生以下问题：

- 无法统计尚未治理的资源范围和治理覆盖率；
- 无法给未编目资源分配责任和治理任务；
- 同一资源可能被不同部门重复创建目录身份；
- 重命名、删除和来源失效缺少连续历史；
- 企业目录退化为“已治理对象清单”，不能承担完整资源盘点；
- Asset、Quality、Standard 等模块缺少统一关联锚点。

因此，ADDP 采用“自动建立身份，按阶段补充治理事实”的单一路线。

## 八、生命周期模型

来源状态和治理成熟度是两个正交维度，不能混成一个状态字段。

### 8.1 来源状态

`source_status` 候选值：

- `active`：当前专业资源仍存在；
- `missing`：当前来源不可见、已删除或超出扫描范围，等待确认或重绑。

### 8.2 治理状态

`governance_status` 候选值：

- `discovered`：自动发现，只有最小目录身份；
- `curated`：已补充业务名称、描述、业务域和基本责任；
- `certified`：经过治理确认，可作为可信资源；
- `deprecated`：业务上不再推荐使用，但保留历史和影响关系。

### 8.3 资产发布状态

资产的 `draft`、`published`、`offline` 等状态仍由 Asset 管理，不进入 CatalogEntry 治理状态。

```mermaid
flowchart LR
    Scan["Meta 正式扫描入库"] --> Discovered["CatalogEntry\ndiscovered"]
    Discovered -->|"业务编目"| Curated["curated"]
    Curated -->|"治理确认"| Certified["certified"]
    Certified -->|"不再推荐"| Deprecated["deprecated"]
    Curated -->|"选择和组合"| AssetDraft["Asset draft"]
    Certified -->|"选择和组合"| AssetDraft
    AssetDraft -->|"发布"| Published["Asset published"]
```

CatalogEntry 来源消失时只改变 `source_status`，不自动清空业务语义、责任和治理历史。

## 九、业务语义关联

Standard 定义“语义是什么”：Domain、Glossary Term、Element、Metric、CodeSet、Classification 和 GradingLevel。

Catalog 定义“这个语义适用于哪个具体资源”：

- CatalogEntry 归属哪个业务域；
- CatalogEntry 应用哪些术语；
- 哪个字段或组件实现哪个数据元；
- 具体数据项支撑哪些指标、模型和服务。

关联事实由 Catalog 保存唯一权威记录。Catalog 创建关联时通过 Standard 公开 API 验证对象存在、属于同一 Tenant 且处于允许引用的生命周期；不能使用跨 Schema 外键或复制 Standard 名称作为权威事实。

Standard Domain 表达业务语义边界，Department 表达组织结构，二者不能合并。一个业务域可能由多个部门共同治理，一个部门也可能负责多个业务域。

## 十、Department、Project Group 与 Workspace

### 10.1 ADDP 当前基础

System/IAM 已经定义并持久化 Department、Department Membership、Project Group、Project Group Membership，以及对应 Scope 的 Role Assignment 和 AuthContext 投影。

当前缺口主要是完整的公开管理 API、Console 管理体验，以及业务模块如何使用这些作用域表达资源责任和协作范围，而不是底层表完全缺失。

### 10.2 Department 的目录职责

Department 表达稳定组织责任范围，可用于：

- CatalogEntry 的主要责任部门；
- 默认治理队列；
- Department Scope 的目录操作授权；
- 责任统计和治理覆盖率；
- 组织调整时的正式责任移交。

Department 不自动获得底层数据内容访问权。最终资源访问仍由对应 owner 模块执行。

### 10.3 Project Group 的目录职责

Project Group 表达跨部门、面向特定目标和期限的协作集合，可用于：

- 共享编目任务；
- 目录对象集合；
- 草稿评审和协作；
- 项目期限内的目录操作授权；
- 项目使用的数据清单。

Project Group 关闭时，临时目录授权和协作任务应失效或移交，但 CatalogEntry 及其长期责任不能随项目组一起消失。

### 10.4 个人工作视图

第一阶段不新增全局 `PersonalWorkspace` 实体。“我的目录”通过当前 User 的关系动态形成。首轮正式实现只纳入以下可由 Catalog 权威判断的关系：

- 我负责的 CatalogEntry；
- 我的收藏；
- 我的关注。

分配给我的治理任务仍由独立治理队列表达，不重复为个人目录关系；最近访问仍属于 Console 交互历史。编目草稿和保存搜索只有在形成明确生命周期与产品需求后再纳入，不为补齐“工作区”概念预建实体。

个人不能作为 CatalogEntry 唯一且不可替代的长期组织责任边界。User 可以担任业务责任人、数据管理员、技术维护者或任务执行人，但应同时存在可移交的组织责任。

### 10.5 是否新增统一 Workspace

第一阶段不新增全局 Workspace 模块，也不在 CatalogEntry 上增加 `workspace_id`。

Catalog 可以拥有 Project Group 范围的目录集合、草稿和任务，这些事实只引用 System 中的 Project Group。个人工作视图按 User 动态计算。

只有当 Develop、Model、Quality、Catalog 等多个模块都明确需要共享同一个工作空间身份、成员、生命周期、工具环境和产物集合时，才重新评估独立 Workspace 能力。即使新增，也不应默认放入 System/IAM；System 只继续拥有身份、组织和授权事实。

## 十一、目录组织和用户视图

企业数据目录不采用单一树表示所有维度，使用“业务主分类 + 分面筛选 + 关系图”。

主导航优先引用 Standard Domain。候选分面包括对象类型、来源模块、来源系统、责任部门、责任人、协作项目组、来源状态、治理状态、业务域、术语、质量、鲜活度、认证状态、安全等级和时空范围。

Catalog 需要表达跨模块关系，例如：

```text
业务实体 --推导出--> 逻辑模型
逻辑模型 --物化为--> CatalogEntry / DataItem
DataItem --支撑--> 指标
指标 --被暴露为--> 数据服务
工作流 --产生--> DataItem
Asset --组合--> CatalogEntry、指标、服务和模型
```

关系必须记录 owner、来源、版本、观察时间和证据，不能只有无来源的边。

## 十二、搜索所有权

| 搜索能力 | Owner | 主要对象与目标 |
| --- | --- | --- |
| 技术资源树搜索 | Meta | 按引擎、路径、技术类型定位 node 和 DataItem |
| 企业元数据搜索 | Catalog | 按业务语义、责任、质量、关系和治理状态发现企业资源 |
| 数据内容、全文、向量和空间检索 | Manager | 检索 DataItem 内容并进入预览或分析 |
| 已发布资产搜索 | Asset | 搜索可申请、授权和运营的已发布资产 |

不同 owner 可以使用同一 Meilisearch 基础设施，但必须使用不同索引、文档语义和权限过滤。不能继续让 DataItem、内容检索文档和已发布资产共用含义不清的 `asset` 文档模型或索引名称。

Catalog 搜索索引可以包含从专业模块派生的名称、类型、路径摘要、业务域、责任和质量摘要，但索引只用于检索和展示，可以从 Catalog 权威事实及专业模块重建。

## 十三、Manager、Asset 与 Portal 的衔接

### 13.1 Manager

Manager 保持技术使用入口：通过 Meta 资源树浏览资源，负责数据预览、内容读取、剖析、快显和内容检索；可以从 Data Explorer 跳转 CatalogEntry，并在有权限时展示 Catalog 业务摘要。

Catalog 不依赖 Manager 的预览或快显结果才能建立目录身份。Manager 也不写 Catalog 的语义关联和责任事实。

### 13.2 Asset

Asset 的目标来源模型为：

```text
Asset
└── AssetComponent[]
    ├── catalog_entry_id
    ├── role
    └── sort_order
```

`AssetComponent` 是概念名称，最终表名和 API 在 Asset 设计阶段确定。核心约束已经明确：

- 一个资产可以组合多个 CatalogEntry；
- Asset 不再使用 `{source_module, source_reference}` 直接引用专业模块；
- 资产发布前校验 CatalogEntry 当前有效性和必要治理条件；
- CatalogEntry 的技术与语义变化不复制到 Asset 权威表；
- 资产需要冻结的发布说明和承诺由 Asset 自己版本化。

现有 Asset 自动发现并直接创建资产草稿的路线应由“专业资源先进入 Catalog，再由 Asset 选择和组合”替代。迁移完成后删除旧路线，不保留兼容分支。

### 13.3 Portal

Portal 只消费 Asset 已发布消费接口，不直接搜索 Catalog 全量资源，也不绕过 Asset 申请和授权数据内容。

## 十四、一致性、变化传播和删除

### 14.1 单一事实源

- 专业事实：各专业模块；
- CatalogEntry、来源绑定、语义关联和责任：Catalog；
- 搜索索引：可重建投影；
- 资产发布与运营：Asset；
- 组织和主体：System/IAM。

不存在 Meta、Standard、Catalog 三处双写关联的方案。

### 14.2 变化传播

Catalog 应消费各专业模块的公开变化契约，并使用统一幂等处理器完成新增、更新和失效。还需要使用同一处理器提供可恢复的 reconciliation，修复事件丢失、停机或索引重建后的差异。

变化传输最终采用事件、outbox/change feed 或按游标拉取，仍需结合 ADDP 现有基础设施确定；正式实现只能选择一条权威变化契约，不能长期保留事件与全量轮询两套业务逻辑。

### 14.3 删除和来源失效

- 专业资源删除或不可见时，Catalog 先标记来源 `missing`；
- 不立即物理删除 CatalogEntry 的业务说明、责任、关系和审计；
- 对已被资产、关系或治理记录引用的 CatalogEntry，必须保留可解释历史；
- 是否最终物理清理由 Catalog 生命周期规范和 ADDP cleanup 体系统一决定；
- Catalog 不反向阻止专业模块删除，除非未来明确建立删除协调契约。

## 十五、权限与可发现性

必须分开判断：

1. CatalogEntry 是否存在；
2. 用户是否可以发现目录条目；
3. 哪些业务和技术元数据可见；
4. 是否可以预览或读取数据内容；
5. 是否可以申请或使用已发布资产。

自动创建 CatalogEntry 不等于租户内全员可见，更不等于获得数据访问权。

| 视图 | 范围 | 主要用户 |
| --- | --- | --- |
| 资源总览 | 权限允许的 `discovered` 及以上 CatalogEntry | 技术人员、治理人员 |
| 治理目录 | `curated`、`certified` 等已治理对象 | 业务人员、数据管理员 |
| 资产门户 | Asset 已发布对象 | 数据消费者 |

Catalog 是自身目录事实和目录操作权限的 owner；底层资源内容权限仍由 Meta、Manager、Service 或实际资源 owner 判断。Catalog 不建立复制全平台业务资源 ACL 的中央大表。

发现权限默认值、字段级元数据脱敏、Department / Project Group 的 Resource Scope Binding 以及 Catalog 与 owner 权限校验方式仍需形成独立规范。

## 十六、第一阶段范围

第一阶段聚焦 Meta DataItem，不同时把所有专业模块一次性接入。

### 16.1 纳入范围

- Catalog 模块骨架、注册、认证和权限清单；
- DataItem 自动创建 CatalogEntry；
- CatalogEntry 详情和来源绑定；
- `source_status`、`governance_status`；
- Standard Domain、Glossary Term、Element 的基础关联；
- 责任部门、业务责任人和数据管理员；
- 企业元数据搜索和基础分面；
- 从 Manager Data Explorer 跳转 Catalog；
- 从 Catalog 选择 DataItem 发起资产化。

### 16.2 暂不纳入

- 通用 Workspace 模块；
- 任意关系类型 DSL；
- 自动模糊重命名识别；
- 跨租户目录共享；
- 把字段全部升级为顶级 CatalogEntry；
- 自动生成业务语义或责任并直接作为权威事实；
- 一次性接入所有 Develop 临时查询、Notebook 和 execution；
- 用 Catalog 替代 Manager 数据预览或 Asset 发布流程。

## 十七、实施阶段与门禁

### 阶段0：正式概念固化

- [x] 确认命名方向：引擎层使用 `EngineCatalog*`，裸名 `Catalog` 和 `CatalogEntry` 保留给企业目录；
- [x] 按 [Engine Catalog 命名收敛专题](ADDP引擎目录命名收敛专题.md) 完成正式文档和代码迁移；
- [x] 更新术语表，增加 Enterprise Data Catalog、Catalog module、CatalogEntry 和 CatalogComponent；
- [x] 更新核心概念关系图和模块架构图；
- [x] 新增 [企业数据目录体系图](../concepts/addp企业数据目录体系图.md) 与 [企业数据目录实现规范](../spec/addp企业数据目录实现规范.md)；
- [x] 修订 System、Meta、Standard、Manager、Asset、Portal 模块边界；
- [x] 明确技术资源、企业元数据、内容和已发布资产搜索所有权，以及资源总览、治理目录、资产门户三个视图；
- [x] 将已经确认的独立 Catalog、自动建档、软依赖和单一事实源共识固化为阶段 0 正式基线。

完成门槛：正式文档只保留独立 Catalog 模块这一条架构路线。

### 阶段1：对象与接口盘点

- [x] 确认 DataItem 使用 fingerprint 作为对外稳定来源身份，并由 Meta 变化源提供 opaque 单调 `source_version`；
- [x] 定义 Catalog 消费 Meta、Standard、System 的精确批量读取契约；
- [x] 选择 Meta owner-local append-only 变化日志 + Catalog 游标拉取和重放作为唯一变化传播机制；
- [x] 定义 UUID CatalogEntry 聚合、来源绑定、组件、语义关联、责任基数和并发版本模型；
- [x] 定义目录可见性、来源失效、治理状态、merged 墓碑、显式重绑和审计状态机；
- [x] 盘点并确定现有 Meta 搜索、Manager 搜索和 Asset 自动发现的删除范围；
- [x] 在实现前识别 Catalog 新模块涉及的 T0-T5 门禁、根 Makefile、CI 自动发现、数据库和外部搜索依赖。

完成门槛：数据库、API、事件和权限设计可以支撑单一路线实现。

### 阶段2：Catalog 基础能力

- [x] 按 `docs/spec/addp新模块开发指南.md` 创建 Catalog 模块；
- [x] 使用统一模块生命周期契约：`/health/live`、`/health/ready`、System 注册 Ready 门禁和同 ID 恢复；
- [x] 增加数据库迁移、权限 Manifest、API、Swagger 和前端入口；
- [x] 实现 DataItem 自动建档和幂等 reconciliation；
- [x] 实现生命周期、来源详情、显式重绑、历史和基础目录搜索；
- [x] 同步根 Makefile、模块自动发现、Gateway、Console、开发脚本和 GitHub Actions；
- [ ] 运行新模块开发指南要求的最小充分 T0-T3 门禁，并确认 main push 后的 CI 能自动命中。

完成门槛：扫描入库的 DataItem 有且只有一个 CatalogEntry，重复同步不产生重复对象。

### 阶段3：组织、语义与协作

- [x] 补齐 System Department / Project Group 的公开管理 API 和 Console 体验；
- [x] 实现 Catalog 责任关系的原子维护、System 精确校验和责任快照；
- [x] 实现责任失效对账和治理队列；
- [x] 实现 Domain、Glossary Term 和 Element 关联及 Standard 精确校验；
- [x] 实现“我的目录”、收藏、关注和 Project Group 目录集合；
- [x] 实现单向治理状态推进、条件权限、乐观并发和必要审计。

完成门槛：目录可以回答“是什么、属于哪个业务域、由谁负责、谁正在治理”。

### 阶段4：Manager 与 Asset 收敛

- [x] Manager Data Explorer 展示 Catalog 摘要和跳转；
- [x] Manager 内容检索与 Catalog 元数据搜索拆分索引和 API；
- [x] Asset 改为通过企业目录条目选择和组合资源；
- [x] 删除 Asset 直接调用多个专业模块自动创建草稿资产的旧路线；
- [x] 删除 Meta 中以 Asset 命名的 DataItem 搜索文档和发现接口；
- [x] 删除通用 `source_reference` 资产来源路径；
- [x] 已登记 `enterprise-catalog-publishing` T4，通过真实 owner API 验证 Meta → Catalog → Asset → Portal 唯一路线、下架后删除与零临时资源残留；保持手工 workflow，等待专用 Runner 首次真实通过。

完成门槛：资源发现、企业编目、资产发布只有一条端到端链路。

### 阶段5：扩展专业目录

- [x] 接入 Model Entity / LogicalTable：owner-local 变化日志、动态批量解析、最小已观察投影、专用权限和 Catalog 自动建档；
- [x] 接入 Standard Metric：Standard owner-local 变化日志、动态批量解析、最小已观察投影、专用权限和 Catalog 自动建档；
- [x] 接入 Service QueryService：owner-local 变化日志、动态批量解析、最小已观察投影、专用权限和 Catalog 自动建档；
- [x] 接入经过筛选且具备稳定 owner 身份的 Develop 成果；
- [x] 以动态引用接入 Quality 当前摘要，不复制评分、Issue 或 execution 历史；
- [x] 以当前 User Token 和共享图组件接入 Meta DataItem 血缘视图，不复制血缘事实；
- [ ] 等待 Model、Standard 等 owner 形成权限感知关系查询契约后接入其他专业关系；
- [ ] 在明确人工企业关系用例和关系类型后评估 Catalog 自有业务关系；
- [ ] 建立认证、治理覆盖率和影响分析；
- [ ] 根据真实跨模块协作需求评估统一 Workspace。

完成门槛：每类对象都有明确 owner、稳定引用、同步契约和权限边界。

## 十八、旧路线迁移结果与剩余缺口

以下原实现冲突已按单一路线完成迁移：

1. Meta 搜索中混淆资源与资产的 `AssetRecord` 词族已删除；
2. Manager 已使用 owner-local 内容索引，Catalog 独立拥有企业元数据搜索；
3. Asset 索引只表达资产发布与运营语义，不再承担专业资源发现；
4. Asset 已删除跨 Meta、Service、Standard、Develop 的自动发现创建路径；
5. Asset 已以 `AssetComponent` 组合多个 `CatalogEntry`，旧 `source_module + source_reference` 已删除；
6. Manager 保留技术资源树，通过 Catalog 摘要和跳转连接企业目录；Console 另提供独立 Catalog 入口；
7. Catalog 个人与项目组协作视图已完成，不再存在由旧路线迁移遗留的功能缺口。

迁移必须删除旧路径，不保留兼容字段、双写、fallback query 或并行发现流程。

### 18.1 已完成的代码删除范围

| 旧路线 | 原 owner 与主要位置 | 迁移结果 |
| --- | --- | --- |
| Meta `AssetRecord`、`IndexAsset`、`IndexTableAsset`、`IndexCatalogAsset` 与 `assetIndex` | `meta/backend/internal/search`、`internal/service/indexer_*_asset.go`、`scanprocessor`、`scanruntime`、`metacleanup/meilisearch.go` | 已删除；业务元数据归 Catalog，内容检索归 Manager |
| Meta `/api/v1/meta/assets/discoverable` | `meta/backend/internal/api/asset_discoverable.go`、router、契约测试和 Swagger | 已删除；Asset 只从 CatalogEntry 选源 |
| Manager 直接消费 Meta `MEILISEARCH_ASSET_INDEX` | `manager/backend/internal/config/config.go`、`internal/service/search_service.go` | 已删除；切换为 `manager_content_documents` owner 索引和受限写入契约 |
| Asset 四模块自动发现 | `asset/backend/internal/service/type_service.go`、`asset_service.go`、config 和测试 | 已删除；资产由显式 AssetComponent 组合创建 |
| Asset 单一 `source_module + source_reference` | `asset/backend/internal/models/models.go`、`common/client/asset.go`、详情/列表前端和 cleanup | 已原子删除；不保留兼容字段或 fallback query |
| 通用 discoverable DTO | `common/client/discovery.go` | 已随 owner 路由和 Asset 调用一并删除 |

Meta 旧索引当前同时承载技术摘要和文档内容。迁移时不能简单把它整体改名为 Catalog 索引：企业业务元数据进入 Catalog `catalog_entries`，内容全文/向量检索进入 Manager owner 索引，Meta 只保留技术资源树查询所需的轻量能力。

### 18.2 新模块登记与门禁范围

Catalog 新模块实现必须同时覆盖：

| 层级 | 必须验证的事实 |
| --- | --- |
| T0 | 术语和正式规范、端口唯一性、Go 依赖一致性、模块自动发现、Permission Manifest 聚合、Swagger route/auth coverage、Gateway/Console/脚本/Compose/CI 登记一致性 |
| T1 | Meta 变化 DTO 与 cursor 校验、Catalog 幂等处理、状态机、版本冲突、可见性、跨 Tenant 不可探测、模块生命周期和前端组件/路由 |
| T2 | Meta DataItem 写入与变化日志同事务、既有数据回填、Catalog 一源一条目与 partial unique constraint、checkpoint 原子性、重绑双聚合事务和投影任务 |
| T3 | Catalog 列表/详情/编目/冲突保留/权限反馈、Console iframe 路由和 Manager 跳转 |
| T4 | `enterprise-catalog-publishing` 已登记 Meta 扫描→自动建档→编目→AssetComponent 发布→Portal 消费→零临时残留；停机追赶、missing、显式重绑与 System 恢复保持后续专项 T4 |
| T5 | 第一阶段无独立发布认证；沿用平台安装包与 HA 发布门禁，待 Catalog 引入专用外部依赖时再登记 owner T5 |

必须登记或确认自动发现的位置包括：

- 根 `Makefile` 的 Catalog frontend、Meta/Catalog PostgreSQL 门禁和 `test-integration`；
- `scripts/test/module-gate.py` / `changed-gate.py` 的 Git 自动发现结果；
- `scripts/dev/start.sh`、`restart.sh`、`stop.sh`、`modtidy.sh`、`detect-common.sh` 和前端依赖脚本；
- `scripts/swagger/gen-swagger.sh`、`check-route-coverage.sh`、`verify-swagger.sh`；
- `.env.example`、`docker-compose.yml`、镜像构建选择和 GitHub Actions；
- System `addp-catalog` Service Principal / OAuth Client / Secret 供应和 Permission Manifest 聚合；
- Gateway 动态模块注册说明、Console 模块配置、API 文档中心、搜索入口和中英文 i18n；
- `scripts/infra/init-postgresql.sql` 中 Catalog Schema 登记。

### 18.3 阶段 2 实施工包

为了始终保持一条可验证主线，阶段 2 按以下顺序推进：

1. **A—Meta 变化源**：建立 `data_item_changes`、回填迁移、统一 Repository 事务写入、公开 cursor API 和 `common/client` DTO；
2. **B—Catalog Backend**：建立 Schema、聚合、checkpoint、幂等消费、基础查询/编目/重绑、Permission、Swagger 和模块生命周期；
3. **C—平台登记**：接入 System 服务身份、Gateway、脚本、Compose、CI、根 Makefile 和 PostgreSQL 门禁；
4. **D—Catalog Frontend**：列表、详情、编目、治理状态、来源历史、冲突保留和 Console 入口；
5. **E—调用方收敛**：Manager 摘要/跳转与内容索引拆分，Asset CatalogEntry 多选组合，删除本节全部旧路线；
6. **F—端到端验收**：补齐 Meta → Catalog → Asset → Portal T4 和专题最终迁移证据。

## 十九、阶段 1 决策与剩余实施问题

已固化到 [企业数据目录实现规范](../spec/addp企业数据目录实现规范.md)：

1. CatalogEntry 使用 UUID 稳定身份和 BIGINT 聚合根并发版本；CatalogComponent 是无独立版本的聚合子对象；
2. Meta 使用同事务 append-only DataItem 变化日志，Catalog 通过 opaque cursor 拉取、幂等应用和从起点重放；
3. 显式重绑只允许 `missing` 原条目接管无人工治理的 `discovered` 临时条目，新条目留下 `merged` 墓碑；
4. `curated` 及以上必须有一个责任部门、一个业务责任人和至少一个数据管理员，技术维护者可多选；
5. `discovered` 默认 `inventory` 可见，目录可见性与内容访问权分离；
6. Meta DataItem 的 primary / secondary Domain、Glossary 和组件 Element 关联归 Catalog；Model Entity / LogicalTable 的 primary Domain、Element、Metric 与建模关系归 Model，Catalog 只维护辅助业务域、企业术语和企业责任，不建立专业语义副本；
7. 结构字段存在时才创建 CatalogComponent，字段重命名不做模糊跟随；
8. Catalog Backend / Frontend / Docker Frontend 端口分别为 `8192` / `5189` / `8120`，Schema 为 `catalog`，索引为 `catalog_entries`，前端路由为 `/catalog`。

仍需在后续专业扩展或 Asset 迁移前确认，但不阻塞 Catalog 基础模块：

1. Catalog 业务关系与 Meta lineage 在统一关系图中的查询联合和证据优先级；
2. Asset 发布版本需要冻结的 Catalog 摘要最小集合。

## 二十、行业调研依据

本专题参考以下官方资料，形成的共同判断是：技术扫描或摄取应先建立可追踪资源身份，再通过业务域、团队、项目和发布流程逐步治理；项目空间可以控制协作和发布，但不应让企业资源在编目前完全没有身份。

- Microsoft Purview Data Map 与 Unified Catalog：<https://learn.microsoft.com/en-us/azure/purview/overview>
- Microsoft Purview Governance Domains：<https://learn.microsoft.com/en-us/purview/unified-catalog-governance-domains>
- Microsoft Purview Data Asset Search：<https://learn.microsoft.com/en-us/purview/unified-catalog-data-assets-search>
- Amazon DataZone Inventory 与发布：<https://docs.aws.amazon.com/datazone/latest/userguide/publishing-data.html>
- Amazon DataZone Projects：<https://docs.aws.amazon.com/datazone/latest/userguide/working-with-projects.html>
- OpenMetadata Domains 与 Data Products：<https://docs.open-metadata.org/v1.12.x/how-to-guides/data-governance/domains-%26-data-products>
- OpenMetadata Data Ownership：<https://docs.open-metadata.org/v1.12.x/how-to-guides/guide-for-data-users/data-ownership>
- DataHub Metadata Model：<https://github.com/datahub-project/datahub/blob/master/docs/modeling/metadata-model.md>

## 二十一、决策记录

### 2026-08-17：建立专题

- 确认 ADDP 缺少跨专业目录的企业数据目录能力；
- 识别 Meta 扩展、独立模块、Asset 扩展和前端聚合等候选方案；
- 暂未决定模块归属。

### 2026-08-25：收敛独立 Catalog 路线

- 确认新增独立 Catalog 模块；
- 确认 Meta 与 Standard 无直接依赖；
- 确认语义定义归 Standard、资源语义关联归 Catalog；
- 确认关联不投影回 Meta 或 Standard；
- 确认 Manager、Asset、Portal 的新边界和搜索所有权拆分；
- 确认所有已扫描持久化 DataItem 自动创建最小 CatalogEntry；
- 确认 Department、Project Group 和个人工作视图不决定 CatalogEntry 是否存在；
- 确认第一阶段不新增全局 Workspace，先使用 System 组织事实和 Catalog 内协作视图。

### 2026-08-26：重新归拢研发主线

- 确认此前业务元数据与企业数据目录共识仍作为后续研发基线；
- 明确阶段 0 正式文档确认先于 Catalog 代码实现；
- 明确除 System 与自身必需 Infra 外，不建立业务模块启动或 Ready 强依赖；
- 识别企业目录实体与既有引擎 `CatalogEntry` 的术语冲突，并确认通过 `EngineCatalog*` 词族迁移释放企业 `CatalogEntry`；
- 将阶段 0 至阶段 5 的工作项改为可持续勾选的跟进清单。

### 2026-08-26：完成阶段 0 与阶段 1 契约基线

- 新增正式 [企业数据目录体系图](../concepts/addp企业数据目录体系图.md) 和 [企业数据目录实现规范](../spec/addp企业数据目录实现规范.md)；
- 将 Catalog 纳入核心概念、模块架构、端口分配、文档导航及 System、Meta、Standard、Manager、Asset、Portal 边界；
- 固化 UUID CatalogEntry、来源绑定历史、CatalogComponent 聚合、语义与责任基数；
- 固化 Meta append-only 变化日志、Catalog cursor 拉取、幂等 checkpoint 和重放对账单一路线；
- 固化四轴状态、目录可见性、显式重绑与 merged 墓碑规则；
- 完成现有实现删除范围和新模块 T0-T5 门禁盘点。

### 2026-08-26：Catalog 核心闭环落地

- Meta 建立 DataItem append-only 变化源、首次回填、opaque cursor API 和公共客户端，Catalog 以 checkpoint 同事务幂等消费；
- 新建独立 Catalog Backend / Frontend，完成 System 服务身份、Gateway、Console、Compose、开发脚本、Swagger、授权和 CI 自动发现登记；
- CatalogEntry 自动建档、组件同步、四轴状态、可见性列表与详情已经落地；
- Standard 与 System 分别提供 `addp-catalog` 专用精确批量解析契约，Catalog 不跨 Schema 查询、不保存反向权威投影；
- `PUT /entries/:id` 以完整聚合和 `version` 原子维护业务信息、Domain、Glossary、Element、责任、可见性和治理状态；
- `POST /entries/:id/rebind-source` 落实 missing 原条目、无人工治理临时条目、双版本、原因、证据、merged 墓碑和双聚合审计；
- `GET /entries/:id/history` 提供来源绑定历史和领域审计，前端明确处理版本冲突、来源重绑和 merged 跳转；
- Catalog 专属 `catalog_entries` Meilisearch 投影由数据库任务异步维护，失败退避重试、租约恢复、可重建，并作为 Catalog 自身 Infra 参与 Ready；
- 企业目录搜索已按 Tenant、目录可见性、业务域、责任部门、来源引擎和治理状态分面，返回前仍由 PostgreSQL 权威事实执行可见性校验。

### 2026-08-26：调用方迁移与 Console 入口收口

- Manager 已将内容检索收回 owner-local 索引，Data Explorer 只展示 Catalog 摘要并跳转企业目录；
- Asset 已切换为显式选择 CatalogEntry、组合 AssetComponent 后创建与发布，Portal 只消费已发布资产；
- Meta discoverable、`AssetRecord` 搜索词族、Asset 跨专业模块自动发现、通用 discovery DTO 和 `source_reference` 已删除；
- Console 已在“目录与资产”分组、首页卡片、侧边栏、默认路由、全局功能搜索、API 文档中心和健康检查中登记 Catalog，公开入口为 `/catalog/entries`；
- Console 契约测试锁定上述入口，并锁定 Asset 创建必须经 CatalogEntry Picker 提交组件聚合。

### 2026-08-26：组织管理与责任治理闭环

- System 已提供 Department / Project Group 的分页管理、层级维护、成员维护、启停/关闭、乐观并发和 Console 组织管理界面；Department 结构变更与成员写入按聚合根串行化，并禁止形成层级环；
- Catalog 周期性复用 System 精确批量解析契约对账当前责任，不建立组织变化副本，也不把 System 可用性纳入 Catalog Ready；
- 失效责任原位转为 `needs_transfer`，同一责任只产生一个 open `responsibility_transfer` 治理任务；引用恢复或完整责任聚合替换会自动解决任务，不提供独立关单路线；
- 新增 `/api/v1/catalog/governance/tasks`、Catalog 责任治理队列前端入口、双语 Swagger、任务约束与 PostgreSQL 门禁；
- Catalog Backend 全量 Go 测试、Frontend 9 项测试与生产构建、Swagger 路由覆盖和 `test-catalog-postgres` 均已通过；System 组织代码的相关包测试、前端测试与构建已通过，System 完整 PostgreSQL 门禁仍受并行 Workbench migration 92 的既有 SQL 阻塞，不由本专题旁路修改。

### 2026-08-26：个人目录与 Project Group 协作闭环

- Catalog 以 `entry_marks` 保存 User 对 CatalogEntry 的收藏与关注，两种标记相互独立；`PUT /me/entries/{id}/marks` 采用完整状态替换，不保留增量开关双路线；
- “我的目录”按责任、收藏或关注关系动态查询并再次执行 CatalogEntry 权威可见性过滤，不创建 `PersonalWorkspace`、个人目录条目副本或反向投影；治理任务继续使用独立治理队列，最近访问继续由 Console 负责；
- Catalog 以独立 `Collection` 聚合实现 Project Group 目录集合，成员只引用 CatalogEntry，不改变目录身份、责任或资产身份；完整成员替换受集合 `version` 乐观并发保护并写入独立审计；
- 集合访问同时要求当前 User 的有效 Project Group 成员关系、Tenant 或该 Project Group 精确权限作用域，以及每个 CatalogEntry 的权威可见性；Project Group 关闭或成员退出后集合保留但不再可见；
- 新增 `catalog.collection.read/update` 权限并把 `catalog.entry.read` 扩展到 Project Group 作用域；内置 Tenant Administrator 获得集合权限，自定义 Project Group 角色由租户按需分配，不复制 System 组织事实；
- Catalog Backend 全量 Go 测试、Frontend 6 个测试文件共 17 项测试与生产构建、15 个公开 API 的 Swagger 路由覆盖、授权一致性门禁及 `test-catalog-postgres` 均已通过；PostgreSQL 门禁同时锁定集合成员唯一性和 Project Group 内集合名称大小写不敏感唯一性。

### 2026-08-26：Model 专业资源接入 Catalog

- Model 以 `model.catalog_resource_changes` 保存 Entity / LogicalTable 的 owner-local append-only 变化日志，迁移时回填存量对象，后续由同库触发器覆盖新增、更新和删除；Catalog 使用独立 `model/catalog_resource_changes` checkpoint 拉取重放，Meta 与 Model 任一来源失败都不阻塞另一来源、Catalog Ready 或企业编目；
- 所有持久化 Entity 和 LogicalTable（包括 draft）分别自动创建 `business_entity` 和 `logical_model` CatalogEntry；Catalog 只保存来源身份、状态、版本及列表搜索所需最小已观察摘要，不保存 Model 对象、字段、关系或指标的只读备份；
- 详情读取通过 Model `POST /runtime/catalog-references/resolve` 动态解析当前专业摘要并返回 Model 详情路由；Model 暂不可达或来源已删除时显式标记 `unavailable` / `missing`，仅展示可重建的最近观测投影，不把投影伪装成当前权威事实；
- Model 主业务域和内生语义只能在 Model 修改。Catalog 编辑器仅维护辅助业务域、Glossary 和企业责任，Backend 同时拒绝 Model 条目的 Catalog primary Domain 或组件 Element 副本；
- 新增不可委派、不可定制的 Tenant 权限 `model.catalog.read`，只授予内置 `tenant.catalog_runtime`，两个 Model 路由同时受 `addp-catalog` Service Client Guard 约束；System migration 95 完成既有环境升级；
- Model / Catalog Backend 全量 Go 测试、Common Model Client 测试、Catalog Frontend 6 个测试文件共 18 项测试与生产构建、Model 56 / Catalog 15 个公开 API Swagger 覆盖、授权一致性门禁、`test-model-postgres`、`test-catalog-postgres` 和 migration 95 专项 PostgreSQL 门禁均已通过。

### 2026-08-26：Standard Metric 专业资源接入 Catalog

- 所有已持久化 Metric（包括 `draft`、`approved`、`deprecated`）自动创建 `metric` CatalogEntry；Standard 专业状态与 Catalog 治理状态保持正交；
- Standard 通过同库 trigger 将 Metric 聚合根新增、版本推进和删除写入 append-only `standard.catalog_resource_changes`，迁移首次回填存量指标；Catalog 为 Standard 使用独立 checkpoint，Meta、Model、Standard 任一来源失败不阻塞其他来源或 Catalog Ready；
- Catalog 专业同步器与动态解析器已收敛为可登记的统一 owner 适配主路径，Model 和 Standard 仅负责各自公开客户端适配，后续专业模块不再增加 EntryService 特例；
- 当前 Metric 摘要通过 `POST /api/v1/standard/runtime/catalog-references/resolve` 动态读取，详情跳转 `/standard/metrics/{id}`；Standard 不可达或指标已删除时只展示明确标记的最后观测投影；
- Metric 定义、公式、类型、状态、主业务域、分类、单位、数据元映射和依赖关系全部留在 Standard。Catalog Backend 与 Console 同时拒绝 Metric primary Domain 和组件 Element 副本，只维护辅助业务域、Glossary、企业责任和目录治理事实；
- 新增不可委派、不可定制的 Tenant 权限 `standard.catalog.read`，只授予 `tenant.catalog_runtime`，两个 Metric 目录来源路由受 `addp-catalog` Service Client Guard 约束；System migration 96 完成既有环境升级；
- Standard / Model / Catalog Backend 全量 Go 测试、Common Standard Client 测试、Catalog Frontend 6 个测试文件共 20 项测试与生产构建、Standard 88 / Catalog 15 个公开 API Swagger 覆盖、授权一致性门禁、`test-standard-postgres`、`test-catalog-postgres` 和 migration 96 专项 PostgreSQL 门禁均已通过。

### 2026-08-26：Service QueryService 专业资源接入 Catalog

- 第四类专业目录只接入具备正式发布快照、稳定 ID 和 Consumer Descriptor 的 QueryService，并自动建立 `data_service` CatalogEntry；`active`、`inactive`、`error` 共享同一目录身份，只有物理删除才标记来源 `missing`；
- Service 通过同库 trigger 将 QueryService 新增、更新和删除追加到 `service.catalog_resource_changes`，首次迁移回填存量服务；变化日志 ID 同时作为单调摘要版本，Catalog 使用独立 Service checkpoint，不把 Service 可达性纳入 Catalog Ready；
- 当前摘要通过 `POST /api/v1/service/runtime/catalog-references/resolve` 动态读取，详情跳转 `/service/published-services/{id}`；Catalog 仅保存名称、编码、服务状态、配置类型、访问模式和必要 Engine ID，Service SQL、发布快照、协议、输出契约、稳定键、端点与 Consumer Descriptor 不进入 Catalog；
- QueryService 当前没有 owner Domain，因此 primary / secondary Domain、Glossary、企业责任、目录可见性和治理状态均由 Catalog 维护；Catalog Backend 与 Console 拒绝为 Service 来源提交组件 Element 副本；
- 新增不可委派、不可定制的 Tenant 权限 `service.catalog.read`，只授予 `tenant.catalog_runtime`；两个目录来源路由同时受 `addp-catalog` Service Client Guard 约束，System migration 97 完成既有环境升级；
- GraphQueryService、TileService、RegisteredService 没有统一稳定消费契约，本轮明确不从管理 DTO 推断企业服务语义，待各自 owner 契约成熟后再扩展；
- Service / Catalog Backend 全量 Go 测试、Common Service Client 测试、Catalog Frontend 6 个测试文件共 21 项测试与生产构建、Service 58 / Catalog 15 个公开 API Swagger 覆盖、授权一致性门禁、`test-service-postgres`、`test-catalog-postgres` 和 migration 97 专项 PostgreSQL 门禁均已通过。

### 2026-08-26：Develop 可复用开发成果接入 Catalog

- 只接入已持久化且可重复编辑或被 Orchestrator 稳定引用的 `query|workflow` DevTask，自动建立 `development_artifact` CatalogEntry；`active`、`inactive`、`archived` 保持同一企业目录身份，软删除或物理删除标记来源 `missing`；
- `script` / Notebook 当前只有空的闭合执行契约，且带有交互会话与私有文件语义，本轮明确排除；即时查询、execution、执行历史、运行结果、Notebook Session 和 ToolApproval 均不伪造 CatalogEntry；
- Develop 通过同库 trigger 将 `query|workflow` DevTask 的新增、更新和删除写入 `develop.catalog_resource_changes`，首次迁移回填存量对象；Catalog 使用独立 Develop checkpoint，不把 Develop 可达性纳入 Catalog Ready；
- 当前摘要通过 `POST /api/v1/develop/runtime/catalog-references/resolve` 动态读取，详情跳转 `/develop/sql?action=edit&id={id}` 或 `/develop/workflow?action=edit&id={id}`；Catalog 只保存名称、说明、开发类型、专业状态和必要 Engine ID，不复制 `content`、查询文本、工作流 DAG、参数、物化输入、执行配置或执行契约；
- DevTask 当前没有 owner Domain，因此 primary / secondary Domain、Glossary、企业责任、可见性和治理状态归 Catalog；Catalog Backend 与 Console 同时拒绝为 Develop 来源保存组件 Element 副本；
- 新增不可委派、不可定制的 Tenant 权限 `develop.catalog.read`，只授予 `tenant.catalog_runtime`；两个目录来源路由受 `addp-catalog` Service Client Guard 约束，System migration 98 完成既有环境升级；
- Develop 相关 Go 测试、Common Develop Client 测试、Catalog Backend 全量 Go 测试、Catalog Frontend 6 个测试文件共 22 项测试与生产构建、Develop / Catalog PostgreSQL 门禁、T2 PostgreSQL CI 登记一致性、Develop / Catalog Swagger 生成、授权一致性与 migration 98 专项 PostgreSQL 门禁均已通过。

### 2026-08-26：Quality 当前摘要动态接入 Catalog

- Quality 评分、Issue 和 execution 历史不创建新的 CatalogEntry，也不复制到 Catalog 表或搜索索引；Catalog 只在 DataItem 详情请求中按需动态组合当前摘要；
- Meta DataItem 变化摘要新增 owner 直接提供的 `schema_name + table_name`，迁移 20 为存量 DataItem 重新发出变化；Catalog 不拆分 `full_name`、locator 或搜索文本猜测 PostgreSQL 定位；
- Quality 新增 `POST /api/v1/quality/runtime/catalog-summaries/resolve`，按 1 至 200 个 `{engine_id, schema_name, table_name}` 精确返回是否配置、最近 execution 状态、当前有效评分、open Issue 数量与详情路径；最近 execution 非 success 时不把更旧评分伪装为当前结果；
- 未配置明确表达为 `not_configured`，Quality 不可达明确表达为 `unavailable`，两者都不伪造评分；Quality 不可达只影响当前详情组合，不影响 Catalog Ready；
- 新增不可委派、不可定制的 `quality.catalog.read`，只授予 `tenant.catalog_runtime`；Quality 路由同时固定校验 `addp-catalog` Service Client，System migration 99 完成既有环境升级；
- Meta PostgreSQL 变化门禁、Quality 全部 PostgreSQL 门禁、migration 99 专项 PostgreSQL 门禁、Quality / Catalog / Common 相关 Go 测试、Catalog Frontend 22 项测试与生产构建、Swagger 路由覆盖和授权一致性门禁均已通过。

### 2026-08-26：血缘与跨模块关系统一视图审计

本轮只完成事实、身份和权限边界审计，尚未进入实现，避免先造一套通用关系表再反向解释其含义。

- Meta 已经拥有唯一数据血缘图接口、时态关系证据、当前投影和共享 `LineageViewer`；`derive`、`serve` 等血缘边继续只归 Meta，Catalog 不复制血缘节点、边或证据；
- Model 建模关系、Standard 指标依赖等专业内生关系继续只归各自 owner；Catalog 只把 owner 返回的节点按稳定来源身份解析到 CatalogEntry，不能把专业关系改写为 Catalog 可编辑事实；
- Catalog 后续如出现明确的人工企业关系，只能拥有与专业关系不重叠的业务关系类型。统一视图按 `owner_module + relation_kind + evidence` 保留来源，不按相同端点合并或设“证据优先级”；
- `data_item` 可通过当前 Meta SourceBinding 的 `item_id` 定位血缘主体；已经自动建档的血缘数据项可通过 Meta 稳定身份映射回 CatalogEntry。`execution`、`field_ref` 等非目录主体保持专业节点，不为统一图伪造 CatalogEntry；
- Catalog Backend 不能直接以 `addp-catalog` Service Token 代理用户调用现有 Meta 图接口：该接口要求当前用户具备 `meta.lineage.read` 并执行资源可见性校验，服务身份代理会丢失用户授权上下文；
- 因此推荐把后续能力分为“Catalog 业务关系事实”和“权限感知的联邦关系视图”两层。前者只有出现明确业务用例才建模；后者只做动态查询编排和身份映射，不落专业边副本。

统一视图的第一阶段授权路线已确认并实施：Catalog Frontend 在当前 User Token 下直接调用 Meta 图查询 API，并使用共享图组件展示。只有影响分析等服务端用例明确需要后端联合时，才设计可验证的 User Delegation 契约，禁止使用 Catalog Service Token 扩权代查。

### 2026-08-26：Meta DataItem 血缘联邦视图接入 Catalog

- Catalog 详情只对 active Meta DataItem 的结构化 `item_id` 发起血缘查询，不解析 `full_name`、locator 或名称推断主体；
- Catalog Frontend 使用当前 User Access Token 直接调用 `GET /api/v1/meta/lineage/graph`，Meta 继续拥有血缘事实并执行 `meta.lineage.read`、Tenant 和资源可见性校验；
- 复用 `common-frontend/graph` 的 `LineageViewer`、查询 DTO 和双语词条；G6 独立按需加载，不进入 Catalog 首屏主包；
- Catalog Backend 不新增关系表或代理 API，不使用 `addp-catalog` Service Token 扩权代查；无权限、主体不存在和 Meta 暂不可达分别展示且不影响目录详情；
- 同轮修正 Catalog Frontend 重复拼接 `/api/v1` 的路径错误，API 调用统一相对共享客户端的 `/api/v1` baseURL，并增加路径契约测试；
- Catalog Frontend 8 个测试文件共 26 项测试及生产构建通过；本地 `localhost:5170` 当前未运行，真实页面验收等待用户下一次重启后完成。

### 2026-08-26：Model / Standard 专业关系联邦视图

- 统一采用 `addp.professional_relations/v1` 一跳关系图契约，但不建立公共关系事实表；节点使用 `{owner_module, resource_type, resource_id}`，边保留 namespaced `relation_kind` 和 owner 可直接证明的字段端点、权重、备注等证据；
- Model 新增当前 User Token 路由 `GET /api/v1/model/entities/:id/relations` 与 `GET /api/v1/model/logical-tables/:id/relations`，覆盖 Entity 关系、LogicalTable 来源 Entity、表关联和事实表指标引用；
- Standard 新增当前 User Token 路由 `GET /api/v1/standard/metrics/:id/relations`，覆盖基准指标以及当前指标参与的上游、下游直接依赖；
- Catalog Frontend 只对 active Model Entity、Model LogicalTable、Standard Metric 动态请求 owner，按用户实际的 Model / Standard read Permission 分别授权；无权限、主体消失和 owner 不可达独立展示，不影响 Catalog Ready；
- Catalog 不复制节点或边，不使用 `model.catalog.read`、`standard.catalog.read` 机器权限代查，不把 Domain、Element、分类、单位等尚非 CatalogEntry 来源的对象伪装成目录节点；
- Model / Standard Swagger 已重新生成并通过路由覆盖检查；Model / Standard 关系服务和 API 包测试、Catalog Frontend 9 个测试文件共 30 项测试及生产构建已经通过。

### 2026-08-26：Catalog 推荐继任关系

- Catalog 不建立泛化 `CatalogRelation`、关系类型配置或任意关系编辑器；体系图中提前出现的通用关系实体删除；
- 当前唯一 Catalog 自有跨条目关系固定为“推荐继任项”，服务于 `curated|certified → deprecated` 后的治理迁移；业务依赖和数据血缘仍归专业 owner，同义、首选和术语替代仍归 Standard；
- 推荐继任使用 CatalogEntry 聚合字段 `recommended_successor_entry_id`，通过既有完整 `PUT /entries/:id` 和聚合 `version` 原子维护，不新增第二条写路径；
- 旧条目与继任项保持两个独立企业身份，旧条目继续显示和审计；`merged_into_entry_id` 仍只表达同一身份归并；
- 目标建立时必须同 Tenant、active、来源有效并处于 `curated` 或 `certified`；一个旧条目最多一个推荐继任项，一个继任项可以承接多个旧条目。
- 后端聚合、约束和审计、完整更新 API、Swagger、前端编辑与详情展示均已实现；Catalog Go、Frontend 测试与构建、PostgreSQL 迁移及聚合集成门禁均通过。平台 `test-changed` 已运行至 System PostgreSQL 门禁，被当前 System IAM 可重复的测试数据隔离与审计计数失败阻断，与本项 Catalog 实现无文件依赖关系。

### 2026-08-26：首次运行态验收

- Console `/catalog/entries` 已可正常加载，企业目录列表、DataItem 详情、业务编目表单和来源治理历史均可见，不再出现空白页；
- Meta DataItem 血缘使用当前 User Token 加载成功，抽验条目展示 4 个节点、3 条关系；Standard Metric 当前专业事实和专业关系路由成功，无直接关系时明确展示空状态；Service QueryService 与 Develop DevTask 当前专业事实均动态解析成功；
- 运行态发现 Element Plus 空表占位行会以缺失业务字段调用单元格 slot，从而构造 `catalog.*.undefined` 国际化键。已统一为“只有状态值存在时才解析翻译”，空表和详情页重载后无新告警，并增加回归测试；
- 运行态发现 Catalog 进程重启后对已存在的 Meilisearch 索引重复提交创建任务，异步任务以 `index_already_exists` 失败，导致后台投影重试和搜索 503。已收敛为先读取现有索引、仅对 `index_not_found` 创建，并以回归测试锁定重启复用语义；
- 本次 `keepalive restart -all` 在验收时仍持有生命周期锁，Model Backend 日志为 `.dev-bins/addp-model: No such file or directory`，因此 Model 专业关系返回 503；Catalog Backend 也仍是 Meilisearch 修复前的已运行二进制。待该生命周期操作结束并应用新二进制后，需复验 Model 关系与 Catalog 搜索；
- 当前 996 个 CatalogEntry 均为 `discovered`，没有可无副作用验证的 `deprecated -> curated|certified` 存量组合。本次只读验证业务编目入口，不为验收制造不可逆的真实治理迁移；推荐继任写路继续由 PostgreSQL 聚合集成测试覆盖，首个受控治理样例出现后再补真实 UI 写入证据。

### 2026-08-27：重启可靠性根因收敛

- 用户再次发起重启后，只读核对发现运行中的 keepalive 仍是 2026-08-26 23:46 启动并持续持锁的原进程，Catalog 仍运行 23:47 构建的旧二进制，Model 构建产物仍不存在；因此搜索 503 和 Model 关系 503 不能作为修复后运行态结论；
- 根因是 `start.sh` 并行编译阶段对每个子任务执行 `wait ... || true`，会吞掉编译失败、错误打印“所有服务编译完成”，随后才以缺失二进制失败。现已建立统一并行构建等待函数：等待所有构建收尾、逐项保留失败诊断，并在任一构建失败时终止启动，不再进入服务启动阶段；
- 开发环境生命周期与原子构建回归测试新增“并行构建失败必须传播、成功必须正常返回、失败前仍等待其他任务收尾”契约；该测试、三个脚本的 Bash 语法检查和 diff 检查均通过；
- 当前 Model Backend `go test ./...` 全量通过，证明现有 Model 源码可编译。待原 keepalive 生命周期结束后使用新脚本重新启动，再完成 Catalog 搜索和 Model 专业关系两项运行态复验。

### 2026-08-27：全量重启与运行态验收收口

- 使用修正后的 `keepalive restart -all` 完成全量重启；19 个模块 Swagger 生成与路由覆盖、全部 Go Backend 和选定 Worker 编译、System 注册、业务 Backend Ready、工作流与 Notebook Runtime、Gateway 以及 19 个 Frontend 均成功；
- 新构建的 Catalog、Model、Standard Ready 均为 200。Catalog Meilisearch 重启复用成功，后台不再出现 `index_already_exists` 投影重试；通过 Console 提交企业目录名称搜索返回 200；
- Model Entity 当前专业事实动态解析成功，专业关系路由返回 200，抽验实体展示 2 条真实一对多关系；Standard Metric 专业关系、Meta DataItem 血缘 4 节点/3 关系、Service QueryService 与 Develop DevTask 当前专业事实均再次通过；
- 重启门禁同时暴露 Develop 正在收敛的旧 Model materialization 装配和测试残留。已按“Develop 不调用 Model API、不持有 Model Permission”删除旧配置测试与 Swagger 字段，唯一公开契约改为通用 `content.relation_inputs`；并行构建失败现在会在启动服务前被准确拦截；
- Catalog Backend 全量 Go 测试、Frontend 10 个测试文件 33 项测试与生产构建、Catalog / Develop PostgreSQL 门禁、Develop Backend 全量 Go 测试、Develop 53 个公开路由 Swagger 覆盖、Online Runner 84 项确定性测试全部通过；
- `enterprise-catalog-publishing` 真实 T4 仍只能由专用 Runner 执行：本机未配置其 User Access Token、Tenant、Fixture Engine、Domain 和 Department 输入，禁止从浏览器会话或生产数据猜测。套件实现、清理语义、分发/预检和 CI 登记已经通过本地确定性门禁。

### 2026-08-27：治理目录、资源盘点与人类可读分面实现

- 企业目录列表固定为同一批 `CatalogEntry` 的两个权限视图：省略 `view` 唯一表示默认 `governance`，只展示 `curated|certified|deprecated`；显式 `view=inventory` 展示包含 `discovered` 在内的全量当前可见条目，并额外要求 `catalog.inventory.read`，缺少权限时返回 `403`，不静默降级；
- 全量 DataItem 自动建档决策不变。视图切分只改变默认发现体验，不增加“扫描到但不建档”或另一套目录实体；Manager 需要按 fingerprint 定位已发现条目时，在当前 User 具备盘点权限的前提下显式使用 `view=inventory`；
- 新增 `GET /api/v1/catalog/entries/facets`。Catalog 只从当前调用方、当前视图实际可见的 CatalogEntry 计算 Domain、Accountable Department 和 Source Engine 引用 ID 与计数；Standard / System 再按事实所有权动态解析名称、编码、类型和可引用状态，Catalog 不复制 owner 全量表；
- 分面 owner 不可达时，只把对应分面标记为 `unavailable`，列表、Catalog Ready 和其他分面继续工作；前端明确提示不可用，不把裸 ID 回退为候选项或表格显示值；
- 企业目录列表的主业务域、责任部门和来源引擎均改为可搜索下拉，来源引擎列展示 `名称 · 引擎类型`；ID 只保留在 URL、API 和选项值中。Console 菜单从“企业资源总览”收敛为“目录浏览”，避免把默认治理目录误解为技术资源全量树；
- Catalog 为动态读取 System 脱敏 Engine Runtime Descriptor 增加最小 `system.engine_descriptor.read` 授权，System migration 101 只授予内置 `tenant.catalog_runtime`，不授予引擎管理权限；新增独立前向迁移门禁，验证迁移前无授权、迁移后恰好一条授权、版本为 101 且 `dirty=false`；

### 2026-08-27：编目候选的人类可读交互契约

- 已确认 Catalog 编辑者不应被要求同时拥有 Standard 或 System 管理权限；`catalog.entry.update` 决定其是否可以维护目录聚合，owner 候选读取由 `addp-catalog` 运行身份完成；
- Domain、Glossary、Element、Department 和 User 收敛到 Catalog `GET /reference-candidates` 单一前端入口，Standard / System 分别提供只允许 `addp-catalog` 的分页搜索路由；候选始终动态查询，不在 Catalog 保存全表副本或搜索投影；
- 候选接口只返回当前可建立新关联的对象，名称/编码用于交互，字符串稳定 ID 只作为提交值；owner 不可达返回明确 `503`，不回退为手工 ID 输入；
- 推荐继任项与治理任务条目筛选复用 CatalogEntry 名称搜索；既有关联显示使用已观察摘要，不把裸 ID 伪装为业务名称；
- 本轮实现清单：补齐 Standard/System owner 候选 API 与公共 Client、Catalog 聚合候选 API、Swagger/路由门禁、编辑器五类选择器、治理任务条目选择器、双语文本与前后端测试。
- `source_engine_id` 在 Catalog 列表协议中固定以十进制字符串输出，并有超过 JavaScript 安全整数范围的回归测试，避免前端选错引擎；Swagger 已重新生成，Catalog 16 个公开路由方法覆盖一致；
- 已通过 Catalog Backend `go test ./...`、Catalog Frontend 10 个文件 35 项测试与生产构建、Manager Frontend 49 个文件 215 项测试与生产构建、Console Frontend 11 个文件 54 项测试与生产构建、Catalog PostgreSQL 门禁、migration 101 独立 PostgreSQL 前向门禁和在线套件确定性单测；
- 根 `make test-module MODULE=catalog` 已完成平台 T0、Catalog Go、前端和 Swagger 门禁，最后仅因该次命令没有向子门禁传入 `CATALOG_POSTGRES_TEST_DSN` 而退出；同一标准 `make test-catalog-postgres` 已使用本地允许的 `addp_test` 单独通过，不是 Catalog 数据库测试失败；
- System 全量 IAM PostgreSQL 门禁仍有并行改造产生的既有失败，包括 `tenant.data_viewer` 权限期望漂移、execution audit 计数和测试 Tenant 重复事实；本轮 migration 101 的静态测试与独立前向 PostgreSQL 门禁均通过，不以修改无关 IAM 断言旁路全量问题；
- 运行态重启未由本轮接管：工作区已有用户侧 `keepalive restart -all` 前台保活进程持续持有生命周期锁，用户随后明确要求不再由 Codex 重启。后续会话不得终止或并发接管该进程，除非用户重新授权。

## 二十二、当前推进状态

| 工作项 | 状态 | 说明 |
| --- | --- | --- |
| 企业目录根因和目标 | 已确认 | 缺少跨专业目录统一身份、关联和发现层 |
| 独立 Catalog 模块方向 | 已确认 | 不再比较 Meta / Asset 扩展路线 |
| CatalogEntry 自动建档 | 已确认 | 所有正式持久化 DataItem 自动创建 |
| Meta / Standard / Catalog 边界 | 已确认 | 技术事实、语义定义、资源关联分别归属 |
| Department / Project Group / Workspace 边界 | 已确认 | 组织与协作不决定目录身份，Workspace 暂缓 |
| 搜索所有权 | 已确认 | Meta、Catalog、Manager、Asset 分别拥有对应搜索 |
| 企业目录实体正式命名 | 已解锁 | Engine Catalog 词族已迁移，裸名 `CatalogEntry` 正式保留给企业目录 |
| 正式术语和概念文档修订 | 已完成 | 企业目录体系图、实现规范、核心概念、模块架构和 owner 边界已同步 |
| 对象、API、变化和权限契约 | 核心设计已完成 | UUID 聚合、游标变化源、批量解析、权限、可见性和重绑状态机已固化 |
| 实现与门禁影响盘点 | 已完成 | Meta / Manager / Asset 删除范围及 T0-T5 登记点已固化 |
| Catalog 模块实现 | 核心能力已完成 | 自动建档、查询、编目、重绑、历史、搜索投影、PostgreSQL 门禁和平台登记已落地；T4 已登记待专用 Runner 首跑 |
| 组织、语义与协作实现 | 已完成 | 组织管理、语义、责任、失效治理队列、个人目录标记、Project Group 目录集合、状态推进和审计均已完成 |
| Manager / Asset 旧路线删除 | 已完成 | Manager owner 内容索引、AssetComponent 单路径、Portal 已发布消费均已收敛，旧 discoverable、AssetRecord 和 `source_reference` 已删除 |
| Model 专业目录接入 | 已完成 | Entity / LogicalTable 自动建档，当前专业事实动态解析，Catalog 仅保存最小可重建投影且不复制 Model 语义 |
| Standard Metric 专业目录接入 | 已完成 | Metric 自动建档，当前专业事实动态解析，指标定义与内生关系仍只归 Standard |
| Service QueryService 专业目录接入 | 已完成 | QueryService 自动建档，当前最小摘要动态解析，服务定义、执行契约和消费描述仍只归 Service |
| Develop 可复用开发成果接入 | 已完成 | 只为 `query|workflow` DevTask 自动建档，当前专业事实动态解析，任务内容和执行事实仍只归 Develop |
| Quality 当前摘要接入 | 已完成 | PostgreSQL DataItem 详情按结构化物理引用动态组合评分与问题摘要，Catalog 不复制 Quality 事实 |
| Meta DataItem 血缘联邦视图 | 已通过运行态验收 | 当前 User Token 直连 Meta、共享图组件按需加载、不复制边；抽验详情成功展示 4 个节点、3 条关系 |
| Model / Standard 专业关系联邦视图 | 已通过运行态验收 | Standard 关系空状态正常；Model Entity 抽验成功动态返回 2 条一对多专业关系，均由当前 User Token 直连 owner |
| Catalog 推荐继任关系 | 已完成，待运行态验收 | 唯一 Catalog 自有跨条目关系；Catalog 定向门禁已通过，全量变更门禁被 System IAM 现有失败阻断；不建立空泛通用关系表，不与 owner 专业关系类型重叠 |
| 治理目录 / 资源盘点视图 | 代码与定向门禁已完成，待新二进制运行态验收 | 默认治理目录只展示已编目条目；资源盘点在独立权限视图中展示全量 DataItem，不改变全量自动建档决策 |
| Catalog 列表人类可读分面选择器 | 代码与定向门禁已完成，待新二进制运行态验收 | Domain、Department、Engine Instance 由 Catalog 计算可见引用集，Standard / System 动态解析名称；列表筛选不再保留裸 ID 输入和引擎 ID 列 |

阶段 5 的专业来源接入、动态当前事实、联邦关系视图、默认治理目录、权限资源盘点和列表人类可读分面均已完成代码与定向门禁。当前第一优先级是等待用户允许的新一轮全量重启后，对 migration 101、Catalog Ready、`governance|inventory` 切换、三个动态下拉、无裸引擎 ID 显示以及 Manager 精确定位进行运行态验收。第二优先级是把“ID 留在协议、不进入主要交互”继续落实到 Catalog 编辑器和治理队列：Domain、Glossary、Department/User、Element、推荐继任项及治理任务条目筛选仍需基于事实 owner 的权限感知候选接口改为名称选择；技术来源详情中的 fingerprint、Item ID 等可保留在明确的技术溯源区，但不得作为主要操作输入。专用 Runner 的 `enterprise-catalog-publishing` 首跑与来源 missing、停机追赶、显式重绑、System 恢复专项 T4 继续作为验收证据回填。统一 Workspace 继续保持暂缓，只有多个专业模块出现同一组可验证的成员、生命周期、环境和产物聚合需求时才重新评估。

## 二十三、后续会话接力清单

新会话应以本专题和 [企业数据目录实现规范](../spec/addp企业数据目录实现规范.md) 为事实基线，不重新讨论已确认的模块边界，也不要恢复 Meta / Standard 反向投影、Catalog owner 全量副本或默认平铺全部 DataItem 的旧方向。

1. 先检查工作区与生命周期锁；用户本轮已明确“不用你重启”，不得停止或替换现有 keepalive。只有用户重新授权重启后，才执行 `bash scripts/dev/keepalive.sh restart -all`。
2. 新二进制运行后，确认 System `schema_migrations` 为 `version=101, dirty=false`，Catalog Backend Ready 为 200，Catalog Service Token 在 Tenant 上调用 Engine Runtime Descriptor 不再返回 403。
3. 使用 Console `/catalog/entries` 验收：默认 URL 省略 `view` 且只显示治理目录；有 `catalog.inventory.read` 时可切换资源盘点；三个分面只显示人类可读名称；owner 单点不可达只禁用对应分面；来源引擎列不显示数字 ID。
4. 验证 Manager Data Explorer 对 discovered DataItem 的 Catalog 精确定位仍可用，并确认无盘点权限的用户不会通过该跳转获得额外目录可见性。
5. 如需重跑完整 Catalog 模块门禁，显式使用 `CATALOG_POSTGRES_TEST_DSN='postgres://addp:addp_password@127.0.0.1:15432/addp_test?sslmode=disable' make test-module MODULE=catalog`；不得新建本地测试 database。
6. 继续 Catalog 编辑器的人类可读候选接口前，先分别确认 Standard、System 与 Catalog 的候选所有权、权限和分页/搜索契约；文档先行，一次删除所有对应裸 ID 输入，不建立兼容双轨。
7. System 全量 IAM PostgreSQL 门禁的既有失败应按各自 owner 根因独立修复；不得为了让 migration 101 显绿而放宽计数、跳过测试或修改无关权限期望。
