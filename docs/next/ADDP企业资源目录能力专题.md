# ADDP 企业资源目录与 Catalog 模块专题

更新时间：2026-09-30

状态：阶段 0 至阶段 6 的既定架构与实现已收口；2026-09-27 根据真实使用反馈重新打开目录价值呈现、业务域适用性和 StandardMapping 落地清单；2026-09-30 进入责任、业务授权决定与实际数据访问的贯通设计，见第二十六节。D1-D3 业务规则已确认并同步正式规范；精确目标、委派、决定消费和执行契约尚待定义，未实施新的授权代码。既有 T4 外部条件验证继续保留，不把旧阶段的完成误作新清单的完成。

## 一、文档定位

本专题持续跟踪 ADDP 企业资源目录（Enterprise Catalog）及独立 `Catalog` 模块的架构决策、待确认事项和实施进度。本文不使用 EDC 缩写，避免重新把范围收窄为 Enterprise Data Catalog。

本文件位于 `docs/next/`，用于承载正在推进的设计，不直接替代正式概念和规范。进入代码实现前，已经稳定的概念必须同步进入：

- `docs/concepts/addp术语表.md`；
- `docs/concepts/addp核心概念关系图.md`；
- `docs/concepts/addp模块架构图.md`；
- 后续新增的 Catalog 概念文档与实现规范；
- 受影响模块的 `CLAUDE.md`、API、Swagger、测试和 CI/CD 门禁。

本专题不讨论底层数据复制，不把企业资源目录等同于资产门户，也不以兼容现有 Asset 自动发现或 Meta 搜索实现为目标。

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
- Asset 管理资产组合、AssetCategory 多级资产目录、发布、授权、评价和运营；
- Portal 面向消费者展示和申请已发布资产；
- System/IAM 管理 Tenant、Department、Project Group、User、Role 和授权上下文。

这些模块分别拥有专业事实，但当前缺少一个跨专业目录的统一关联与发现层，无法稳定回答：

> 企业有哪些数据、模型、指标和服务？它们是什么意思？由谁负责？质量如何？相互之间有什么关系？哪些已经完成治理，哪些已经作为资产发布？

此前 Meta 搜索、Manager 检索和 Asset 自动发现分别承担了一部分目录能力，导致“技术资源目录、企业资源目录和资产目录”边界不清。根因不是单个字段或接口缺失，而是 ADDP 尚未建立企业资源目录这一独立架构边界。

## 三、已确认的架构决策

以下决策已达成共识，后续设计不再并行保留其他路线：

1. ADDP 新增独立 `Catalog` 模块，承载企业资源目录能力。
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

### 4.2 企业资源目录

企业资源目录不是另一棵资源树，也不是专业资源的完整副本。它负责：

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
    = 企业资源目录能力
```

### 4.3 资产目录与 AssetCategory

资产目录（Asset Directory）是面向消费者组织已发布资产的多级业务导航，不是企业资源目录的子树或副本。目录中的单个分类节点统一称 `AssetCategory`，整体树称 `AssetCategoryTree`。

Asset 负责 AssetCategory 定义和层级、资产归类、资产身份和版本、多目录对象的组合边界、发布与上下架、使用条件，以及申请、授权、评价和运营。Portal 只读取已启用且包含已发布资产的分类树。

一个资产可以组合多个 CatalogEntry，一个 CatalogEntry 也可以被多个资产复用。

业务域、企业资源组织方式和资产目录不要求一致：Domain 是少量稳定的治理边界；Catalog 使用搜索、分面和关系组织企业资源；AssetCategory 则按消费者理解的数据主题或消费场景形成多级导航。资产可以跨业务域组合多个 CatalogEntry，发布人可以参考组成资源获得分类建议，但必须显式确认资产归类，不能自动复制 Catalog 的 Domain、Department 或资源类型结构。

`Catalog` 与 `Category` 的用词边界固定为：Catalog 是带描述的系统化登记和发现体系；Category 是分类体系中的单个类别。Asset 模块不得继续以 `Catalog` 命名分类实体、表、字段、路由或权限。

### 4.4 Engine Catalog 与企业 Catalog

ADDP 引擎体系中的 `EngineCatalogProvider`、catalog path 或数据库 catalog 表示引擎原生命名空间或扫描能力；本专题中的 `Catalog` 表示企业资源目录模块。

正式进入术语表时必须分别定义：

- Engine Catalog：数据引擎原生命名空间；
- Enterprise Catalog：跨专业目录的统一关联与发现能力；
- Catalog module：ADDP 中实现企业资源目录能力的 owner 模块；
- CatalogEntry：企业资源目录中的稳定对象身份。

当前术语表已将 `EngineCatalogProvider`、`EngineCatalogPath`、`EngineCatalogEntry` 和 `EngineCatalogFacts` 定义为引擎层词族，并把裸名 `Catalog` 与 `CatalogEntry` 保留给企业资源目录。生产代码的公共契约、调用方、Swagger 和 Python SDK 已按该边界迁移，不保留旧名兼容类型。

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

Standard 定义“业务语义是什么”：Domain、Glossary Term、Element、Metric 和 CodeSet；Security 独立定义 SecurityClassification、SecurityGrade 和 SensitiveDataType。

Catalog 定义“这个语义适用于哪个具体资源”：

- CatalogEntry 归属哪个业务域；
- CatalogEntry 应用哪些术语；
- 哪个字段或组件实现哪个数据元；
- 具体数据项支撑哪些指标、模型和服务。

关联事实由 Catalog 保存唯一权威记录。Catalog 创建关联时通过 Standard 公开 API 验证对象存在、属于同一 Tenant 且处于允许引用的生命周期；不能使用跨 Schema 外键或复制 Standard 名称作为权威事实。

Standard Domain 表达业务语义边界，Department 表达组织结构，二者不能合并。一个业务域可能由多个部门共同治理，一个部门也可能负责多个业务域。

### 9.1 与完整语义层的关系及能力差距

本节只用于 ADDP 内部架构检查，不改变对外科普图书的通用定位，也不把完整语义层收缩为 Catalog 模块。评估基准采用[《数据治理 100 问》第 046 问](../books/数据治理100问/05-元数据、资产与语义篇/046-语义层与指标平台.md)中对语义层和指标平台的通用定义：完整语义层不仅包含业务语义与物理数据的映射，还要能够基于实体、事件、维度、度量、关系、层级、粒度和访问规则生成可执行查询；指标平台还要覆盖指标口径、版本、计算和服务。

ADDP 当前已经形成由多个 owner 共同组成的治理型语义基础：Standard 定义可复用语义与指标，Model 定义业务实体、事实、维度、粒度和模型关系，Meta 提供物理结构与血缘，Catalog 建立企业身份、资源语义关联、责任和治理视图。Catalog 只是其中的语义关联与治理枢纽，不是完整 Semantic Layer，也不应接管专业语义定义、模型设计或查询编译。

| 完整语义体系能力 | 当前 Owner / 现状 | 评估 | 后续边界 |
| --- | --- | --- | --- |
| 业务术语、别名、数据元、码值和单位 | Standard 已有对应权威对象 | 已具备基础 | 继续由 Standard 定义，Catalog 只保存资源应用关系 |
| 维度层级 | Model 已有 LogicalTable 聚合内权威对象 | 已具备基础 | 继续由 Model 定义，Catalog 只保存必要的资源身份与关系投影 |
| 指标名称、定义、公式、依赖、单位和生命周期 | Standard Metric 已有基础对象和结构化计算配置 | 部分具备 | 继续核查统计对象、事实粒度、业务时间、过滤范围、可分析维度、生效版本和服务方式，不在 Catalog 补字段 |
| 业务实体及实体关系 | Model Entity 已有权威聚合 | 已具备基础 | 继续由 Model 维护，Catalog 只建立企业目录身份并动态读取专业事实 |
| 事实表、维度表、度量字段、粒度和星型关系 | Model LogicalTable、LogicalField 和 TableRelation 已表达基础模型 | 已具备基础 | 聚合性质、业务时间与可用维度必须由 Model / Standard 的明确契约提供，Catalog 不推断 |
| 业务事件，例如下单、签约、发货和回款 | 当前可由事实表或领域模型间接表达，尚无统一一等语义身份 | 尚未统一 | 先通过真实查询场景判断是否需要 Event 聚合；未确认前不预建空壳实体 |
| 物理表、字段、格式、技术画像和血缘 | Meta 是唯一技术事实 owner | 已具备基础 | 继续由 Meta 提供当前事实与关系，其他模块不复制 |
| 业务语义、模型与实际资源的稳定绑定 | Catalog 已拥有 CatalogEntry、来源绑定、语义关联和组件数据元关联；Model 内生专业映射仍归 Model | 已具备核心基础 | 保持“专业事实归专业模块、资源应用关系归 Catalog”，不建立反向投影或双写 |
| 企业级发现、责任、目录可见性和治理生命周期 | Catalog 已实现目录视图、责任、编目、认证、弃用和治理覆盖率 | 已具备核心能力 | 这是 Enterprise Catalog 职责，不等同于底层数据访问授权或语义查询执行 |
| 统一语义查询契约 | 尚无能够统一表达实体、指标、维度、时间、过滤和排序的稳定请求模型 | 缺失 | 必须先从高价值端到端用例设计逻辑语义查询，不得直接以 SQL、CatalogEntry 字段或某个引擎方言反向定义 |
| 语义解析、关联路径选择和逻辑计划 | 尚无统一 planner / compiler owner | 缺失 | 需要独立确定事实来源、版本选择、聚合正确性和错误契约；不能塞入 Catalog 列表或搜索服务 |
| 指标计算与统一查询 / 服务输出 | Standard 保存定义但不执行引擎查询；现有 Engine、Orchestrator、Service 只分别拥有执行、编排和已发布服务能力 | 缺失统一闭环 | 先明确查询编译与执行交接协议，再决定复用现有模块还是形成独立 Semantic Runtime 边界 |
| 面向 BI、应用和 AI 的稳定语义接口 | 当前主要消费目录、模型、指标或已发布服务的各自 API | 缺失统一入口 | 未来接口应消费权威语义查询契约，不让 BI、应用和 AI 各自重新解释底层表字段 |
| 语义权限与底层数据访问 | System/IAM 提供身份授权，Catalog 提供目录可见性，实际内容访问仍由资源 owner 判断 | 部分具备 | 必须保持“可发现不等于可访问”，后续语义计划需要携带并下推资源、行列及组织范围约束 |
| 指标和模型变更影响分析 | Catalog 已联邦组合 Meta 血缘、Model / Standard 专业关系与目录身份 | 部分具备 | 继续保留关系 owner 与证据；未来补足语义查询、报表、服务和消费者依赖后才能形成完整影响链 |

因此，ADDP 当前结论固定为：`Standard + Model + Meta + Catalog` 已形成治理型语义基础，最大的架构缺口是统一语义查询契约、语义解析与指标执行闭环。该缺口是跨模块能力问题，不自动成为 Catalog 待办，也不预先推出必须新增 `semantic` 模块。只有出现可验证的端到端查询场景，并且现有 owner 无法在不复制事实、不形成循环依赖的前提下承载稳定查询身份、逻辑计划和执行生命周期时，才文档优先评估独立 Semantic Runtime。

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

截至 2026-08-27 的跨模块评估结论是：当前不新增全局 Workspace 模块，也不在 CatalogEntry 或 Develop、Model、Quality 的专业实体上增加 `workspace_id`。这是已完成的架构判断，不是待实现的暂缓项。

Catalog 可以拥有 Project Group 范围的目录集合、草稿和任务，这些事实只引用 System 中的 Project Group。个人工作视图按 User 动态计算。

本轮核对表明：

- System Project Group 已经权威表达跨部门成员与授权作用域；
- Catalog Collection 已经表达项目组对 CatalogEntry 的命名协作集合，不改变目录身份、责任或底层访问权；
- Develop DevTask、Model Entity / LogicalTable 与 Quality RuleApplication / CheckTask 拥有不同的专业聚合根、状态机、执行环境和清理边界，当前没有一个需要四个模块共同维护的稳定协作身份或统一产物生命周期；
- 引擎插件中的 `SpatialWorkspace` 是具体 Engine Instance 的厂商空间能力事实，与企业协作 Workspace 无关，不得复用或合并。

只有当出现一个明确的端到端用例，并且多个 owner 模块确实需要共享同一稳定身份、成员边界、创建/关闭生命周期、工具或运行环境以及跨模块产物集合，而 Project Group 加模块自有聚合无法在不复制事实的前提下表达时，才重新评估独立 Workspace 能力。重新评估必须先修订术语和概念规范；即使新增，也不应默认放入 System/IAM 或 Catalog，System 只继续拥有身份、组织和授权事实。

## 十一、目录组织和用户视图

企业资源目录不采用单一树表示所有维度，使用“业务主分类 + 分面筛选 + 关系图”。业务域保持少量、稳定并服务治理责任，不承担资产门户的多级导航。目录浏览在同一 `/entries` 路由内以 Standard Domain 为主分类，按“业务域 → 责任部门 → 资源类型”逐步收窄权威分页列表；这只是即时聚合的导航读模型，不建立 Domain—Department—Entry Type 的持久化父子关系，也不复制 CatalogEntry。

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

Catalog 是自身目录事实和目录操作权限的 owner，不建立复制全平台业务资源 ACL 的中央大表。底层资源访问仍由相应访问模块执行检查；不能把“哪个模块执行检查”混同于“哪个模块维护授权事实”。同一物理表或文件经不同模块访问时，其授权事实的唯一归属及责任参与方式正在第二十六节中收敛，不能让 Meta、Manager、Develop、Quality 各维护一份互相冲突的源数据授权。

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
- [x] 更新术语表，增加 Enterprise Catalog、Catalog module、CatalogEntry 和 CatalogComponent；
- [x] 更新核心概念关系图和模块架构图；
- [x] 新增 [企业资源目录体系图](../concepts/addp企业资源目录体系图.md) 与 [企业资源目录实现规范](../spec/addp企业资源目录实现规范.md)；
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
- [x] 运行新模块开发指南要求的最小充分 T0-T3 门禁，并确认 main push 后的 CI 能自动命中；本地完整 `make test-module MODULE=catalog` 已通过，Catalog 的 Backend、Frontend、PostgreSQL、Swagger、授权、模块自动发现与 CI 注册均由标准入口覆盖。

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
- [x] 已登记 `enterprise-catalog-publishing` T4，通过真实 owner API 验证 Meta → Catalog → Asset → AssetCategory → Portal 唯一路线、下架后目录隐藏、删除与零临时资源残留；保持手工 workflow，等待专用 Runner 首次真实通过。

完成门槛：资源发现、企业编目、资产发布只有一条端到端链路。

### 阶段5：扩展专业目录

- [x] 接入 Model Entity / LogicalTable：owner-local 变化日志、动态批量解析、最小已观察投影、专用权限和 Catalog 自动建档；
- [x] 接入 Standard Metric：Standard owner-local 变化日志、动态批量解析、最小已观察投影、专用权限和 Catalog 自动建档；
- [x] 接入 Service QueryService：owner-local 变化日志、动态批量解析、最小已观察投影、专用权限和 Catalog 自动建档；
- [x] 接入经过筛选且具备稳定 owner 身份的 Develop 成果；
- [x] 以动态引用接入 Quality 当前摘要，不复制评分、Issue 或 execution 历史；
- [x] 以当前 User Token 和共享图组件接入 Meta DataItem 血缘视图，不复制血缘事实；
- [x] 接入 Model、Standard owner 的权限感知专业关系查询契约；Catalog 只做当前 User Token 下的联邦展示，不复制专业关系边；
- [x] 在明确弃用迁移用例后建立 Catalog 唯一自有业务关系“推荐继任项”；不扩展为通用关系表或可配置关系类型；
- [x] 建立 `curated → certified` 独立权限、状态约束和聚合审计主路径；
- [x] 实现已固化口径的 Catalog 自有治理覆盖率动态聚合 API、Console 页面和门禁；
- [x] 实现已固化契约的联邦影响分析与来源身份导航，不复制 owner 关系边；
- [x] 打通治理覆盖率到权威缺口列表和现有编目编辑器的处置闭环；不新增覆盖率明细表、治理任务实体或搜索投影字段；
- [x] 将企业目录浏览收敛为“主业务域 + 上下文责任部门/资源类型分面 + 权威分页列表”；复用 `/entries/facets`，不新增企业目录树实体或第二页面路线；
- [x] 在资源盘点的业务域导航中增加“待归类”虚拟治理入口；严格复用 `primary_domain=missing` 权威缺口，不创建特殊 Domain 或第二查询路线；
- [x] 将复合 `accountability` 覆盖率拆为责任部门、业务责任人、数据管理员三个原子维度，并在责任部门导航增加“待分配部门”虚拟治理入口；
- [x] 根据真实跨模块协作需求评估统一 Workspace；结论为当前不新增，不预建模块、实体或 `workspace_id`。

完成门槛：每类对象都有明确 owner、稳定引用、同步契约和权限边界。

### 阶段6：规模化治理操作

- [x] 资源盘点支持当前页显式多选，不提供“全部匹配筛选”或隐式全选；单次最多 200 条；
- [x] 实现主业务域、责任部门两种原子批量分配命令，目标只能通过 owner 动态名称候选选择，不允许手工输入 ID；
- [x] 每个 CatalogEntry 携带独立 `version`，服务端稳定排序加锁、任一失败整批回滚，成功逐条递增版本；
- [x] Model 业务实体/逻辑模型与 Standard 指标继续由专业 owner 维护主业务域，混入批次时整体拒绝；
- [x] 每条成功变更写独立审计并共享 `batch_id`，同步投递搜索投影任务；
- [x] 补齐后端事务门禁、前端交互测试、Swagger、统一 Online/CI 登记和专题验证记录。

完成门槛：治理人员可以在不暴露稳定 ID、不复制 owner 事实、不引入 Tenant 级热点锁的前提下，安全处置一批明确选中的目录治理缺口。

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

已固化到 [企业资源目录实现规范](../spec/addp企业资源目录实现规范.md)：

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

- 确认 ADDP 缺少跨专业目录的企业资源目录能力；
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

- 确认此前业务元数据与企业资源目录共识仍作为后续研发基线；
- 明确阶段 0 正式文档确认先于 Catalog 代码实现；
- 明确除 System 与自身必需 Infra 外，不建立业务模块启动或 Ready 强依赖；
- 识别企业目录实体与既有引擎 `CatalogEntry` 的术语冲突，并确认通过 `EngineCatalog*` 词族迁移释放企业 `CatalogEntry`；
- 将阶段 0 至阶段 5 的工作项改为可持续勾选的跟进清单。

### 2026-08-26：完成阶段 0 与阶段 1 契约基线

- 新增正式 [企业资源目录体系图](../concepts/addp企业资源目录体系图.md) 和 [企业资源目录实现规范](../spec/addp企业资源目录实现规范.md)；
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
- 后端聚合、约束和审计、完整更新 API、Swagger、前端编辑与详情展示均已实现；Catalog Go、Frontend 测试与构建、PostgreSQL 迁移及聚合集成门禁均通过。当时记录的 System IAM 测试数据隔离与审计计数阻塞已在后续迁移收口中解除，当前完整 `make test-changed` 已通过。

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
- 重启门禁同时暴露 Develop 正在收敛的旧 Model materialization 装配和测试残留。已按“Develop 不调用 Model API、不持有 Model Permission”删除旧配置测试与 Swagger 字段，查询输入统一收敛到 `content.query_parameters[]`，其中 `type=relation` 表示数据表参数；并行构建失败现在会在启动服务前被准确拦截；
- Catalog Backend 全量 Go 测试、Frontend 10 个测试文件 33 项测试与生产构建、Catalog / Develop PostgreSQL 门禁、Develop Backend 全量 Go 测试、Develop 53 个公开路由 Swagger 覆盖、Online Runner 84 项确定性测试全部通过；
- `enterprise-catalog-publishing` 真实 T4 仍只能由专用 Runner 执行：本机未配置其 User Access Token、Tenant、Fixture Engine、Domain 和 Department 输入，禁止从浏览器会话或生产数据猜测。套件实现、清理语义、分发/预检和 CI 登记已经通过本地确定性门禁。

### 2026-08-27：治理目录、资源盘点与人类可读分面实现

- 企业目录列表固定为同一批 `CatalogEntry` 的两个权限视图：省略 `view` 唯一表示默认 `governance`，只展示 `curated|certified|deprecated`；显式 `view=inventory` 展示包含 `discovered` 在内的全量当前可见条目，并额外要求 `catalog.inventory.read`，缺少权限时返回 `403`，不静默降级；
- 全量 DataItem 自动建档决策不变。视图切分只改变默认发现体验，不增加“扫描到但不建档”或另一套目录实体；Manager 需要按 fingerprint 定位已发现条目时，在当前 User 具备盘点权限的前提下显式使用 `view=inventory`；
- 新增 `GET /api/v1/catalog/entries/facets`。Catalog 只从当前调用方、当前视图实际可见的 CatalogEntry 计算 Domain、Accountable Department 和 Source Engine 引用 ID 与计数；Standard / System 再按事实所有权动态解析名称、编码、类型和可引用状态，Catalog 不复制 owner 全量表；
- 分面 owner 不可达时，只把对应分面标记为 `unavailable`，列表、Catalog Ready 和其他分面继续工作；前端明确提示不可用，不把裸 ID 回退为候选项或表格显示值；
- 企业目录列表的主业务域、责任部门和来源引擎均改为可搜索下拉，来源引擎列展示 `名称 · 引擎类型`；ID 只保留在 URL、API 和选项值中。Console 菜单从“企业资源总览”收敛为“资源浏览”，避免把默认已治理资源视图误解为技术资源全量树；
- Catalog 为动态读取 System 脱敏 Engine Runtime Descriptor 增加最小 `system.engine_descriptor.read` 授权，System migration 101 只授予内置 `tenant.catalog_runtime`，不授予引擎管理权限；新增独立前向迁移门禁，验证迁移前无授权、迁移后恰好一条授权、版本为 101 且 `dirty=false`；

### 2026-08-27：编目候选的人类可读交互契约

- 已确认 Catalog 编辑者不应被要求同时拥有 Standard 或 System 管理权限；`catalog.entry.update` 决定其是否可以维护目录聚合，owner 候选读取由 `addp-catalog` 运行身份完成；
- Domain、Glossary、Element、Department 和 User 收敛到 Catalog `GET /reference-candidates` 单一前端入口，Standard / System 分别提供只允许 `addp-catalog` 的分页搜索路由；候选始终动态查询，不在 Catalog 保存全表副本或搜索投影；
- 候选接口只返回当前可建立新关联的对象，名称/编码用于交互，字符串稳定 ID 只作为提交值；owner 不可达返回明确 `503`，不回退为手工 ID 输入；
- 推荐继任项与治理任务条目筛选复用 CatalogEntry 名称搜索；既有关联显示使用已观察摘要，不把裸 ID 伪装为业务名称；
- Standard `GET /references/candidates`、System `GET /runtime/catalog-references/candidates`、公共 Go Client 与 Catalog `GET /reference-candidates` 已完成；三层均使用当前 Tenant、分页搜索和最小显示摘要，不接受客户端 Tenant ID；
- Catalog 编辑器已将 Domain、Glossary、Element、Department、User 全部改为名称候选选择；推荐继任项继续使用 CatalogEntry 名称搜索，治理任务条目筛选也已删除 UUID 输入；详情和任务列表在名称不可用时显示明确占位，不把稳定 ID 当作业务文案；
- 候选 SQL 已登记进 Standard 与 System 一次性 PostgreSQL 门禁。Common Client、Standard、System、Catalog 全量 Go 定向测试，Catalog Frontend 10 个文件 35 项测试与生产构建，Standard PostgreSQL 门禁、System 候选专项 PostgreSQL 门禁、三模块 Swagger 生成与路由覆盖，以及全仓授权覆盖门禁均已通过；带规定测试 DSN 的 `make test-module MODULE=catalog` 与 `MODULE=standard` 也已完整通过 T0-T3；
- 实现轮未重启服务；后续统一重启已完成五类选择器与治理任务名称筛选验收，详见下方运行态验收记录。Standard 或 System 单点不可达的 `503` 继续由定向测试覆盖；不得为验收恢复手工 ID 输入或本地候选副本。

### 2026-08-27：Project Group 集合名称的动态解析契约

- Project Group membership 与 Catalog Collection Scope 判断继续只使用 AuthContext 授权事实；Project Group 名称是 System 的可变组织事实，不加入 `addp.auth_context/v1`，避免名称随 Access Token 生命周期陈旧；
- Catalog 不保存 Project Group 名称副本，也不把 Project Group 混入 Domain、Glossary、Element、Department、User 的编目候选；System 精确批量解析扩展 `project_group` 类型，仅供当前有效成员集合的显示组合；
- Catalog `GET /me/project-groups` 只组合当前 User 已有 membership 与 `catalog.collection.read|update` Scope，动态返回名称、成员角色和读写能力；System 不可达只令该请求返回明确 `503`，不影响 Catalog Ready 或集合权威事实；
- Collection 列表、创建选择器和详情必须消费该组合视图，不显示或回退为裸 Project Group ID。
- System 精确解析已支持 `project_group`，migration 102 只向内置 `tenant.catalog_runtime` 增加 `iam.project_group.read`；Catalog 集合写路径同时要求同一 Project Group 上的 read 与 update，禁止跨 Scope 拼接 Permission；
- Catalog `GET /me/project-groups`、Collection 列表/创建/详情名称显示和无裸 ID 回退均已实现；System 不可达时前端保留集合事实浏览并明确提示名称与创建选项不可用；
- Common Client、System IAM/API/migration、Catalog Backend 全量 Go 测试、Catalog Frontend 10 个文件 35 项测试与生产构建、System 155 / Catalog 18 个公开路由覆盖、Catalog PostgreSQL 门禁、migration 102 独立前向 PostgreSQL 门禁及 System Catalog 引用 PostgreSQL 门禁均已通过；本轮未重启服务；
- 实现时全仓 `make test-authorization` 曾被并行 Transfer 授权清单漂移阻断；并行改造完成后已重新执行并通过，当前 System 155、Catalog 18 等公开路由及授权声明一致，不再保留该阻断项。
- `source_engine_id` 在 Catalog 列表协议中固定以十进制字符串输出，并有超过 JavaScript 安全整数范围的回归测试，避免前端选错引擎；Swagger 已重新生成，Catalog 18 个公开路由方法覆盖一致；
- 已通过 Catalog Backend `go test ./...`、Catalog Frontend 10 个文件 36 项测试与生产构建、Manager Frontend 49 个文件 216 项测试与生产构建、Console Frontend 11 个文件 54 项测试与生产构建、Catalog PostgreSQL 门禁、migration 101 独立 PostgreSQL 前向门禁和在线套件确定性单测；
- 根 `make test-module MODULE=catalog` 已完成平台 T0、Catalog Go、前端和 Swagger 门禁，最后仅因该次命令没有向子门禁传入 `CATALOG_POSTGRES_TEST_DSN` 而退出；同一标准 `make test-catalog-postgres` 已使用本地允许的 `addp_test` 单独通过，不是 Catalog 数据库测试失败；
- System 全量 IAM PostgreSQL 门禁仍有并行改造产生的既有失败，包括 `tenant.data_viewer` 权限期望漂移、execution audit 计数和测试 Tenant 重复事实；本轮 migration 101 的静态测试与独立前向 PostgreSQL 门禁均通过，不以修改无关 IAM 断言旁路全量问题；
- 实现轮未接管用户侧 `keepalive restart -all`；用户随后完成统一重启，运行态结果已在下一节回填。后续会话不需要为已验收项目重复重启。

### 2026-08-27：统一重启后的目录交互运行态验收

- 用户完成统一重启后，System、Catalog、Gateway 的 Ready 均返回 `200`；System 当前 `schema_migrations=103, dirty=false`，其中 migration 102 的 Catalog Project Group 读取授权已经实际生效；
- Console `/catalog/entries` 默认省略 `view` 并展示治理目录，当前治理条目为 0；切换 `view=inventory` 后展示 998 条资源。业务域分面显示“名称 · 编码”，来源引擎分面与列表显示“名称 · 类型”，不再显示或要求输入引擎 ID；
- Catalog 编辑器运行态验证了 Domain、Glossary、Element、User 的真实名称候选；Department 同样走 System 动态下拉且请求返回 `200`，当前 Tenant 没有可选部门，所以为空。所有候选仅把稳定 ID 作为选项值，没有手工 ID 回退；
- 责任治理队列验收时发现候选请求遗漏 `view=inventory`，在默认治理目录为空时会错误地没有候选。已将治理任务目录候选固定为资源盘点查询，补充回归测试；浏览器复验返回 20 个名称候选，Catalog Frontend 10 个测试文件 36 项测试和生产构建通过；
- 项目组目录集合的 `/me/project-groups` 运行态返回 `200`；当前登录用户没有有效 Project Group membership，页面正确显示无成员空状态且未回退为裸 ID。实际成员名称组合仍需在具有有效 membership 的验收身份下补证，不为验收临时修改组织数据；
- Manager Data Explorer 使用带 `item_id` 的规范 Locator 精确定位 `public_test.gdb` 成功，并按 fingerprint 动态展示 Catalog 摘要。首次点击“打开企业目录”暴露出跨模块跳转误用同模块 Router 同步的问题：Manager iframe 被错误改写为 Catalog 本地路径，而 Console 拒绝 synchronized 跨模块请求；现已改为直接走 Console 导航桥，不再修改 Manager Router。Manager Frontend 49 个测试文件 216 项测试与生产构建通过；浏览器复验后顶层 URL、Catalog iframe 和条目内容均准确切换到 CatalogEntry `30b94349-9434-407d-8577-b3f1472cd7ea`；
- 五类候选与项目组请求在 Gateway 日志中均为 `200`；跨模块跳转修复后的复验没有新增浏览器 warning/error（日志中保留了修复前的 Router warning 与同步拒绝错误作为根因证据）。Standard/System 单点不可达的 `503` 语义继续由定向测试覆盖；本轮不通过停止共享服务做破坏性运行态演练。
- 使用规定的本地 `addp_test` DSN 重新执行完整 `make test-module MODULE=catalog`，平台 T0、Catalog Go/Frontend、Swagger 与 PostgreSQL T2 全部通过；`make test-authorization` 也已通过。首次以 PTY 执行时发现 Online Workbench MySQL 测试假 Docker 对所有 `exec` 无条件读取 stdin，导致 `mysql -e` 在开放终端上永久等待；已增加开放 stdin 回归测试，并仅在 SQL heredoc 场景读取输入，Online Runner 85 项测试及完整模块门禁复验通过；
- 当前数据库没有任何 Project Group 或 Project Group membership，无法在不制造组织数据的前提下补验真实名称组合；`enterprise-catalog-publishing` 所需的 User Token、Tenant、Fixture Engine、Domain、Department 等 9 项环境变量也全部未配置，因此继续保留为专用 Runner 外部前置条件，而不是本地实现缺口。

### 2026-08-27：治理覆盖率与联邦影响分析实现

- `GET /api/v1/catalog/governance/coverage` 固定使用资源盘点权限，单条数据库聚合语句直接统计 active CatalogEntry 的治理状态和七个治理维度，不新增覆盖率表、缓存投影或后台同步；组件数据元只以具有 active CatalogComponent 的条目为分母，无组件的专业条目明确计为不适用；
- 业务定义要求业务名称与说明同时存在；责任部门、业务责任人和数据管理员分别使用可独立处置的原子覆盖维度，`curated` 状态仍同时要求三项完整。主业务域允许 Catalog 自有 primary Domain 或 Model / Standard 最近观察摘要中的 owner `domain_id`，页面明确该口径不代表数据质量、底层授权或资产发布资格；
- Console 新增 `/catalog/governance/coverage`，只向具有 `catalog.inventory.read` 的用户展示菜单与全局搜索结果；页面显示有效条目、治理状态分布、适用分母、未覆盖数和覆盖率，不把 998 个盘点数据项再次平铺成另一个列表；
- 新增 `POST /api/v1/catalog/entries/resolve-sources`，最多按 200 个 `{source_module,source_type,source_identity}` 精确查询 Catalog 当前来源绑定。接口只复用当前 User 的目录可见性，具有盘点权限时可解析 `inventory` 条目，否则盘点条目自然不可见；跨 Tenant、不存在和不可见统一 `found=false`；
- Catalog 详情把推荐继任、Model / Standard 专业关系和 Meta 血缘分别标注为联邦影响分析的治理、专业和血缘分区。专业节点使用 owner 正整数稳定身份、Meta 节点使用 owner 返回的 fingerprint 动态解析 CatalogEntry 导航；owner 图仍由当前 User Token 直连查询，Catalog Backend 不代理、不复制边，也没有新增通用关系表；
- 定向审查后将治理覆盖率从多次计数收敛为同一条数据库聚合，避免并发更新造成分母和分子来自不同快照；专业关系缺少显示名时使用明确占位，不把资源 ID 回退为业务文案；
- Catalog Backend 全量 Go 测试、Catalog PostgreSQL 迁移/推荐继任/治理覆盖率与来源解析门禁、Catalog Frontend、Console Frontend、Catalog 20 个公开路由 Swagger 覆盖及全仓授权门禁均已通过；完整 `make test-module MODULE=catalog` 使用规定的本地 `addp_test` DSN 通过；
- 原五维契约运行态验收时，System、Catalog、Gateway Ready 均为 `200`，页面动态展示 998 个 active 条目且没有产生第二份条目清单或覆盖率投影；责任维度拆分后的七维契约不沿用该旧响应证据，留待下一次正常统一重启后重新读取当前事实；
- Model Entity“订单”当前关系动态返回“订单—客户”一对多关系，两个 owner 稳定 ID 均解析到可见 CatalogEntry，并成功跳转到“客户”；Meta DataItem `test` 通过 fingerprint 解析出血缘图中的三个其他目录条目，4 个节点、3 条关系保持由 Meta 当前 User Token 查询，点击成功跳转到 `public_test.parquet`。Gateway 中治理覆盖率和四次来源解析请求均为 `200`；
- 运行态复验发现 Element Plus 表格切换时会调用占位行插槽，新页面直接拼接占位值曾产生 `dimensions.undefined`、`source.undefined` i18n warning。现已在渲染边界使用空值安全标签函数并新增回归测试；Catalog Frontend 11 个测试文件 40 项测试与生产构建通过，全新浏览器页重复覆盖率、血缘加载和跨条目跳转后 warning/error 均为 0。

### 2026-08-27：治理覆盖率到权威缺口处置闭环

- 覆盖率页面的“未覆盖”数字直接下钻到既有企业资源盘点列表，沿用 CatalogEntry 详情和现有编目编辑器；不新增覆盖率明细表、治理任务实体、专业事实副本或 Meilisearch 投影字段；
- `GET /api/v1/catalog/entries` 增加必须成对使用的 `coverage_dimension=<固定七维之一>` 与 `coverage_state=missing`，并固定要求 `view=inventory`。缺参、未知维度、治理目录视图或与名称全文搜索并用均返回 `400`；
- 缺口列表和覆盖率聚合复用同一组 PostgreSQL 适用性与覆盖谓词，并只计算 active CatalogEntry。PostgreSQL 门禁逐维断言列表 `total` 与覆盖率 `not_covered` 完全一致；
- 前端以可恢复 URL 保存缺口维度和状态，缺口视图显示明确提示、禁用名称搜索并可一键退出；其他类型、来源状态、治理状态、可见性、业务域、责任部门和引擎等结构化筛选仍可继续组合；
- Catalog Swagger 已重新生成，20 个公开路由覆盖一致；带规定 `addp_test` DSN 的完整 `make test-module MODULE=catalog` 已通过平台 T0、Catalog Go、Frontend 测试与生产构建、PostgreSQL T2 门禁。本轮不重启服务，运行态点击链路留待下一次正常统一重启后顺带验收。

### 2026-08-27：企业目录上下文导航

- 企业目录继续使用唯一 `/catalog/entries` 页面和同一批 CatalogEntry，不新增目录树表、树节点身份、第二个目录页面或双轨查询；Standard Domain 是主业务分类，Department 只是可交叉的责任分面，二者不固化为父子关系；
- 既有 `GET /api/v1/catalog/entries/facets` 扩展可选 `primary_domain_id`、`accountable_department_id`、`entry_type`：业务域统计覆盖当前视图，责任部门随业务域收窄，资源类型随业务域与部门收窄，来源引擎再受三项共同约束；所有统计直接来自 Catalog 权威库；
- `/entries` 列表和 `/entries/facets` 复用主业务域与责任部门过滤谓词，并统一只查询 active CatalogEntry；同时补齐 `data_application` 作为合法列表与导航资源类型，不保留前端可选而后端拒绝的契约漂移；
- Console 将重复的业务域、责任部门、资源类型下拉替换为三段可键盘操作、显示名称与数量的导航区，选择写入可恢复 URL；来源状态、治理状态、可见性和来源引擎继续作为高级筛选，权威分页列表仍位于同页下方；
- 统一 `enterprise-catalog-publishing` T4 浏览器用例已改为校验三段目录导航和引擎名称选择器。Catalog Frontend 12 个测试文件 44 项测试及生产构建、Catalog Backend 全量 Go、PostgreSQL 上下文分面门禁、Online Runner 86 项确定性测试、Catalog 20 个公开路由 Swagger 覆盖和带规定 `addp_test` DSN 的完整 `make test-module MODULE=catalog` 均通过；本轮不单独重启服务。

### 2026-08-27：资源盘点“待归类”虚拟治理入口

- “待归类”只在资源盘点的业务域导航中作为治理动作出现，不创建特殊 Standard Domain、不进入 Domain 分面候选，也不建立第二套目录树或列表 API；
- 点击唯一映射到 `view=inventory&coverage_dimension=primary_domain&coverage_state=missing`，并清除名称搜索、业务域、责任部门和资源类型选择；普通导航选择也会清除缺口状态，保证同一 URL 只有一种列表语义；
- 进入缺口后隐藏普通三段导航，继续显示既有缺口提示、权威分页列表和结构化筛选，退出后恢复正常目录导航。责任覆盖率随后拆为三个原子维度，“待分配部门”只使用 `accountable_department=missing`；
- `enterprise-catalog-publishing` T4 浏览器契约已纳入“待归类”入口可见性。Catalog Frontend 12 个测试文件 46 项测试及生产构建、Online Runner 86 项确定性测试通过；本轮不单独重启服务，运行态点击留待下一次正常统一重启后顺带验收。

### 2026-08-27：责任覆盖率原子化与“待分配部门”入口

- 删除复合 `accountability` 覆盖率枚举，不保留旧 query、前端解析或 Swagger 兼容分支；责任覆盖率唯一拆为 `accountable_department`、`business_owner`、`data_steward` 三个原子维度，使每个未覆盖数字都对应一种明确处置动作；
- 七个覆盖维度仍由同一条 PostgreSQL 聚合动态计算，不新增表、迁移、缓存或搜索投影。测试增加只有责任部门而没有责任人的条目，分别验证三个维度不会再联动；
- 资源盘点的责任部门导航增加“待分配部门”虚拟入口，唯一映射到 `coverage_dimension=accountable_department&coverage_state=missing`。入口保留已选业务域，清除名称搜索、责任部门和资源类型；它不是 System Department，也不进入 Department 分面候选；
- Catalog Swagger 已按批量治理契约重新生成，21 个公开路由覆盖一致；统一 T4 验证七个覆盖率维度、“待归类”“待分配部门”两个虚拟入口以及资源盘点显式多选/批量治理对话框。Catalog Frontend 14 个测试文件 53 项测试与生产构建、Catalog Go、PostgreSQL T2 和 Online Runner 86 项均通过；本轮不重启服务。
- 用户统一重启后，System、Catalog、Gateway Ready 与 Console 均为 `200`。覆盖率页面动态展示 1,002 个 active 条目和七个维度，责任部门、业务责任人、数据管理员分别独立显示且旧 `accountability` 不再出现；组件数据元适用 189 个、不适用 813 个；
- 浏览器实际点击“待归类”得到 `view=inventory&coverage_dimension=primary_domain&coverage_state=missing`；选择“客户域”后点击“待分配部门”得到 `view=inventory&primary_domain_id=1&coverage_dimension=accountable_department&coverage_state=missing`。两个缺口页均隐藏普通导航、禁用名称搜索，退出后恢复业务域选中状态；客户域下资源类型即时收窄为 2 个业务实体和 2 个逻辑模型，浏览器 warning/error 为 0。

### 2026-08-27：专用 macOS 完整验证交付

- 没有新增第二套 Online suite、workflow 或 Make 目标；继续使用既有 `make local-ci`、`make test-online ONLINE_SUITE=enterprise-catalog-publishing`、`online-host-gate.sh`、`online-preflight.py`、专用 PostgreSQL Engine Fixture 和 `online-t4-gates.yml`；
- 现有 `enterprise-catalog-publishing` 已扩展为完整目录主链路：连续两次真实 Meta 扫描验证 fingerprint / CatalogEntry UUID 幂等，验证 `inventory` / `governance` 视图、七维治理覆盖率、Meta fingerprint 精确来源解析、编目、AssetComponent 发布、Portal 同身份消费、AssetCategory 目录树与分类子树消费及零临时资源残留；
- 同一 suite 新增真实浏览器阶段：以同一专用 User 正常登录，验证治理覆盖率页、CatalogEntry 详情、Domain / Department / Entry Type 三段名称导航和 Engine 名称选择器，并在临时资产发布后打开 Portal AssetCategory 页面确认目录名称和唯一 Asset 卡片；拒绝 `undefined` 文案、浏览器 warning/error 和失败业务响应，浏览器报告写入仓库外 `enterprise-catalog-publishing-browser.json`；
- 专用 macOS 验证矩阵固定为 `ECV-00` 至 `ECV-08`，完整命令、环境边界、通过证据和 Artifact 清单已写入 `scripts/README.md`。T0-T3 使用独立 Local CI checkout 执行 `make local-ci LOCAL_CI_ARGS=--full`；T4 使用 `addp-online` Runner checkout 手工触发现有 Online workflow，二者不合并为不安全的 `test-all`；
- 确定性脚本协议、Host Gate 生命周期和 Online CI 登记检查已纳入现有 `make test-online-runner` / `make test-platform`；另一台 macOS 只负责真实环境首跑和回传证据，不需要临时补脚本或修改仓库内 `.env`。

### 2026-08-27：统一 Workspace 评估收口

- 核对 Catalog、Develop、Model、Quality 的专业聚合与 System 组织事实后，确认当前不存在一个同时共享身份、成员、生命周期、环境和产物集合的跨模块工作空间用例；
- System Project Group 继续作为跨部门协作成员事实，Catalog Collection 继续作为企业目录协作聚合，专业产物和执行环境留在各 owner 模块；
- 统一 Workspace 从“暂缓实现”收敛为“当前明确不新增”，因此没有代码、数据库迁移或 API 变更，也不增加空壳模块；
- 本专题阶段 5 的最后待办已完成。后续只在第 10.5 节所列的端到端触发条件成立时重开架构评估。

### 2026-08-27：Catalog 显式成员批量治理收口

- 全局并发规范已明确区分“集合整体替换”和“显式成员批量命令”：前者使用集合 `revision`，后者逐条携带 `id + version`；不得用筛选条件在执行时隐式展开成员，也不为 Catalog 制造 Tenant 级全局修订热点；
- 新增唯一 `POST /api/v1/catalog/entries/batch_governance`，单次接收 1 至 200 个互不重复的明确成员，只支持 `assign_primary_domain` 和 `assign_accountable_department`；同时要求 `catalog.inventory.read` 与 `catalog.entry.update`；
- Catalog 在事务前只向 Standard 或 System 精确解析一次目标，在事务内按 UUID 稳定排序锁定全部 CatalogEntry；任一条目不存在、跨 Tenant、非 active、版本冲突、引用失效或 owner 边界不适用时整批回滚；
- 批量主业务域只替换 Catalog 自有 primary Domain，保留 secondary Domain 与 Glossary；批量责任部门只替换 accountable department，保留业务责任人、数据管理员和技术维护者，并原子解决对应责任转移任务；
- Model `business_entity|logical_model` 与 Standard `metric` 的主业务域仍由专业 owner 维护，混入批次时整体返回 `catalog_batch_governance_unsupported_entry`，不产生部分写入；
- 每个成功条目独立递增版本、写入 `catalog.entry.batch_governance_applied` 审计并共享 `batch_id`，同时投递搜索投影任务；响应按原请求顺序返回新版本；
- Console 仅在资源盘点且当前 User 同时具备两项权限时展示当前页多选。目标通过既有 owner 动态名称候选选择，不出现 Domain、Department 或 Engine ID 输入；冲突失败保留选择和对话框输入供刷新后重新确认；
- Catalog Go 全量测试、PostgreSQL 原子回滚门禁、Frontend 14 个测试文件 53 项、生产构建、Swagger 21 路由覆盖和 Online Runner 86 项均通过。`enterprise-catalog-publishing` 已把显式多选与批量治理对话框纳入现有真实浏览器阶段，但不在永久 fixture 上重复提交治理写入。
- 本机统一重启后完成真实写入验收：在两个并行页面中明确选择同一组 `public_test.parquet` 与 `public_test_shapefile.shp`，首个页面成功批量分配“户外域”，两个条目分别从版本 2 递增到 3，并产生同一 `batch_id=7ccc4cf6-27e2-46e4-b5e3-bff2050f16ea` 下的两条审计；第二个过期页面返回版本冲突、保留两项选择和对话框输入，数据库没有第二组审计或版本递增；
- 运行态同时验证 Department 目标使用 System 动态名称候选且当前 Tenant 无可选部门，不回退为手工 ID；选中 Model 逻辑模型后选择主业务域操作，页面明确提示其事实由 Model / Standard 维护并禁用提交。验收结束后通过 Catalog“业务编目”完整聚合更新移除两条临时 Domain 关联，两个条目均递增到版本 4、主业务域计数恢复为 0；浏览器列表选择恢复为 0，详情页和列表页 warning/error 均为 0。

### 2026-08-27：企业资源目录、资产目录与引擎资源树命名收口

- 中文产品名统一为“企业资源目录”，专业英文名为 `Enterprise Catalog`；模块、聚合根和稳定身份继续使用 `catalog`、`CatalogEntry`，不把 Catalog 解释为多级树，也不再用“企业数据目录”缩小其对数据项、标准、模型、指标、服务和开发成果的覆盖范围；
- Asset 面向 Portal 的多级消费导航统一为“资产目录”；节点、树、表、字段、路由和权限分别收敛为 `AssetCategory`、`AssetCategoryTree`、`asset.categories`、`category_id`、`/categories` 和 `asset.category.*`。旧 `Catalog` 分类模型、`asset.catalogs`、`catalog_id`、`/catalogs`、`asset.catalog.*` 不保留运行时兼容分支；
- 既有环境只在 Asset schema migration 中执行一次性原地改名并保留数据；若新旧表或新旧字段同时存在则直接失败，避免猜测权威事实源。System migration 109 将旧权限绑定原子迁移到新权限后禁用旧权限，不让角色授权丢失，也不保留双权限路线；
- AssetCategory 更新和删除统一携带聚合 `version`，使用乐观并发控制；资产创建、编辑、列表、批量归类、搜索投影、Portal 分类树和 Common Asset Client 已全部改用 `category_id/category_name`。发布资产搜索索引仍是 Asset 可重建派生投影，Asset 启动时从权威已发布资产重建，不成为第二事实源；
- Manager / Common 的技术浏览结构固定称“引擎资源树”或 `ResourceTree`，Console 英文交互可称 `Data Explorer`；它按 Engine Node / Item 展示技术资源，不改名为 Catalog，也不承担企业关联或资产分类；
- UI 中 Department、Domain、Engine 等稳定 ID 均由 owner 动态名称选择器提供，不要求用户输入内部 ID。企业资源目录继续使用搜索、分面和关系导航；资产目录才提供独立多级分类树，二者不复制、不要求同构；
- Asset、Catalog、Portal、Console、Common Client、Swagger、授权生成物、确定性 Online 契约和 CI 自动发现已同步。`make test-module MODULE=asset`（指定 `addp_test`）、`make test-module MODULE=catalog`（指定 `addp_test`）、`make test-module MODULE=portal`、System IAM PostgreSQL、全仓授权门禁及对应前后端测试与生产构建均已通过；不需要为本轮新建测试入口或第二套 CI workflow。
- 仓库聚合 `make test-changed` 已在 24 个受影响注册模块上完整通过，覆盖 platform T0、各 owner T1/T2/T3、System migration 109、Swagger、授权和 CI 登记。期间同步修复了 `changed-gate.py` 对已删除 tracked file 的扫描错误，并将 Transfer 旧文本快照收敛到已有 runtime-target 不扫描契约；两者均已纳入原有测试体系。

### 2026-08-28：统一重启后的运行态与物理命名收口

- 用户统一重启后，System、Gateway、Console、Catalog、Asset、Portal 和 Workbench 进程均已启动；System migration 为 `109 / dirty=false`，`asset.category.*` 四项权限均为 active，`asset.categories` 存在且 `asset.catalogs` 不存在；
- Catalog 启动瞬间因 Workbench 尚未就绪记录过一次同步延迟，此后没有重复告警；Workbench 变化接口已注册，Catalog 的 `workbench/catalog_resources` checkpoint 已推进，确认是可恢复的启动时序而不是持续依赖故障；
- 浏览器只读验收确认企业资源目录治理视图、三段名称导航、Workbench 数据应用条目和资产目录管理页均正常加载，页面 warning/error 为 0。运行态发现并修正了产品术语尾差：整体导航统一展示“资产目录 / Asset Directory”，分类树内部代码和 API 继续使用 `AssetCategory`，不再使用会与 Enterprise Catalog 冲突的 `Asset Catalog`；
- Asset schema 的表和字段虽然已经收敛，但 PostgreSQL 仍会保留表改名前的序列、主键与三个索引名。迁移现已原子重命名 `categories_id_seq`、`categories_pkey` 并删除三个旧冗余索引；若新旧序列或主键同时存在则直接失败，不保留双轨物理对象；
- Asset、Portal、Console 定向前端测试与生产构建全部通过；`make test-module MODULE=asset` 完整通过 platform T0、Asset Go T1、前端 T1/T3、PostgreSQL T2、Swagger、授权和 CI 自动发现；
- 用户随后正常统一重启，System、Gateway、Catalog、Asset、Portal、Workbench Ready 与 Console 均为 `200`。Asset schema 中五个旧 `catalog*` 序列、主键和索引对象已全部归零，`categories_id_seq`、`categories_pkey` 与三个新索引完整存在；迁移由 Asset 正常启动自动应用，没有手工改库。Catalog 启动时仍仅出现一次 Workbench 时序告警，随后 checkpoint 已在重启后继续推进，确认自动恢复链路有效。

### 2026-08-28：资产目录子树消费语义收口

- Asset 管理端 `GET /assets?category_id=` 固定表示“直接归属于该 AssetCategory”，用于精确归类、调整和治理；不隐式展开后代目录；
- Asset 消费端 `GET /consumer/assets?category_id=` 和 Portal `GET /categories/{id}/assets` 固定表示“当前 AssetCategory 及其全部后代”，选择父目录即可浏览整个子树中的已上架资产；
- 子树展开由 Asset 在同 Tenant 内递归解析权威 `AssetCategory` 关系，Portal 只传递用户选中的一个节点；不让前端展开 ID，不新增 `include_descendants` 兼容开关，不建立第二份目录投影；
- 消费目录树只返回含有已上架资产的分支，节点 `count` 为当前节点整棵子树的已上架资产总数；因此父节点展示数量与点入后的列表总量一致。管理端目录列表和树仍保留直接归属数；
- 非法目录 ID 返回 `400`，不存在或跨 Tenant 节点返回 `404`；同一子树 ID 集合同时用于 PostgreSQL 列表查询和 Meilisearch 过滤，不允许搜索路径与数据库路径产生不同的目录语义；
- SQLite 服务单测、消费 API 契约、搜索过滤单测和真实 PostgreSQL 递归 CTE 门禁已纳入现有 Asset 测试体系；Asset / Portal Swagger 已同步子树和计数语义，不新增 CI workflow。
- 用户统一重启后，System、Gateway、Asset 和 Portal Ready 均为 `200`；Asset 目录管理页正常展示当前 Tenant 的 6 个多级目录节点，Portal 父目录路由 `/portal/categories/1` 正常返回 0 项，Portal BFF 与 Asset 消费路由均为 `200`，相关页面无 browser warning/error；
- 当前运行库有 6 个 AssetCategory，但没有任何 `published` Asset，因此本次不为验收临时制造资产或修改发布状态。“父目录子树 `count` 与点入后列表总数一致”已由 API 契约和 PostgreSQL 门禁确定性覆盖，真实数据运行态证据只在自然产生包含后代已上架资产的父目录后补验，不视为实现阻断。

### 2026-08-28：AssetCategory 目录移动契约

- 目录位置属于 AssetCategory 聚合状态，不新增 `/move` 动词路由或独立关联实体；唯一 `PUT /categories/{id}` 收敛为完整更新，请求同时携带 `version`、`name`、`parent_id`、`description` 和 `sort_order`；
- `parent_id=null` 明确表示移动到根目录；正整数表示移动到同 Tenant 的目标目录。目标不存在、跨 Tenant、指向自身或任一后代、或移动后与同级目录重名时整次更新失败；
- 后端让 AssetCategory 创建、完整更新和删除共享当前 Tenant 的目录图事务锁；更新在锁内校验完整层级、条件匹配 `id + tenant_id + version` 并递增版本，任一失败不产生部分移动，也不允许并发挂接与删除破坏树结构。版本冲突继续返回 `409 + asset_category_version_conflict`；
- 前端复用已加载的 AssetCategory 权威树组装父目录选择器，用户只看到名称和层级，不输入 ID；编辑当前节点时从候选中排除自身和全部后代，并提供明确的“根目录”选项；
- 完整更新请求必须显式包含 `parent_id`、`description` 和 `sort_order`，后端区分字段缺失与合法零值（`null`、空说明和排序 `0`），避免遗漏字段被误解释为移动到根目录或清空聚合状态；Swagger 同步将三者标为 required，并将 `parent_id` 标为 `x-nullable`；
- 乐观锁冲突时编辑对话框保留当前输入并提示重新加载，只有用户显式选择“重新加载”才以最新版本覆盖表单；目录名称、父目录、排序和说明始终作为一个整体提交；
- SQLite 服务测试与生产 Router API 契约覆盖同 Tenant 移动、移动到根目录、自身/后代/跨 Tenant 拒绝、同级重名和版本冲突；真实 PostgreSQL 门禁覆盖目录图加锁、移动和环路拒绝，前端 Vitest 覆盖候选排除与可读层级路径，生产构建通过。未增加新 workflow，继续由统一 Asset 测试入口执行。
- 统一重启后的真实页面验收使用测试目录 `path3` 完成 `root3 -> root2 -> root3` 可恢复移动；两次提交均即时刷新树和右侧父目录详情，最终再次刷新仍保持 `root3/path3` 原结构，浏览器 warning/error 为 0。首次验收同时发现“提交后清空详情但树保留旧高亮”的前端状态分裂，根因已收口为树内部 current key 与 `selected` 必须由同一 `selectCategory` 同步；修复后移动、移回和刷新三条路径均保持选中详情。
- 调用方复核发现资产工作台的“重命名目录”仍按旧局部契约只发送 `version + name`，会被完整更新 API 正确拒绝。该入口现已统一保存并回传 `parent_id`、`description`、`sort_order`，不再通过局部负载隐式清空或移动目录；Vitest 增加第二调用方契约回归。真实页面使用 `path3 -> path3-runtime -> path3` 完成可恢复重命名验收，最终仍位于 `root3`，浏览器 warning/error 为 0。
- 统一 `enterprise-catalog-publishing` T4 已补齐资产目录消费链路：发布临时 Asset 后确认对应 AssetCategory 出现在 Portal 目录树、子树 `count=1` 且分类列表只返回该 Asset；同一 User 浏览器继续打开 `/portal/categories/:id`，确认目录名称和唯一 Asset 卡片。资产正式下架并删除后，先确认空分类立即从 Portal 树消失，再按聚合 `version` 删除临时 AssetCategory。继续复用既有 suite、workflow、Gateway User Token 和零残留清理，不新增 Portal 投影、测试路由或第二套脚本。

### 2026-08-28：Catalog 编目状态与页面信息架构收口

- `curated → discovered` 固定命名为“撤销编目”，继续复用唯一 `PUT /entries/:id` 聚合更新，不新增撤销接口、第二权限或兼容请求。请求必须原子清空 Catalog 自有业务名称、业务说明、Domain、Glossary、责任、组件 Element 和推荐继任关系，同时恢复 `visibility=inventory`；CatalogEntry 稳定身份、来源绑定、专业事实、版本与历史保留；
- 撤销编目只接受完整重置形状，部分清理、`certified → discovered` 和 `deprecated → discovered` 均由服务端拒绝。成功事件固定为 `catalog.entry.curation_withdrawn`，与普通 `catalog.entry.updated` 审计区分；
- 页面固定标题统一为“企业资源目录”，侧边栏入口统一为“资源浏览”，两个视图分别称“已治理资源”和“资源盘点”。条目操作按状态显示：`discovered` 为“开始编目”，`curated` 为“编辑编目”并在更多操作提供明确治理操作，`certified` 与 `deprecated` 不再显示通用编目编辑器；不再对所有状态显示含义模糊的“业务编目”；
- “已治理资源”和“资源盘点”继续作为同一 `/entries` 路由内的两个权限视图，不拆成两个侧边栏页面，也不混成一张无范围提示的列表。页面按“企业资源目录总体说明 → 查看范围及当前结果数 → 浏览维度 → 条件筛选 → 结果列表”组织；查看范围区域明确显示当前视图说明，默认省略 `view` 仍唯一表示 `governance`，显式 `view=inventory` 仍唯一表示资源盘点；
- 列表不再以三列密集按钮模拟目录树，业务域、责任部门、资源类型改为三个可搜索、可独立清除、显示名称与数量的正交分面；“待归类”“待分配部门”继续唯一映射既有治理缺口查询。来源状态、治理状态、可见性与来源引擎收进默认折叠的高级筛选，避免基础浏览和治理诊断混在同一视觉层级；
- 详情页按用户问题划分为“概览 / 编目信息 / 专业事实 / 关系与历史”四个可恢复 Tab。概览只回答资源是什么、治理到哪一步、来自哪里和谁负责；编目信息承载 Catalog 自有业务事实；专业事实动态读取 owner 当前事实；关系与历史承载推荐继任、联邦关系、血缘与审计。`tab` 写入 canonical URL，列表返回上下文继续保留；
- Catalog Backend 全量 Go 测试、Catalog Frontend 15 个测试文件 57 项测试与生产构建、Catalog 22 个公开路由 Swagger 覆盖、Online 套件登记检查均通过。统一重启后只读验收确认已编目 Workbench 条目显示“编辑编目”、四个详情 Tab 和新的概览层级；两条误编目 Workbench 数据应用已通过正式界面撤销，分别从版本 2 升至 3、版本 6 升至 7，均恢复为 `discovered + inventory`，离开户外域并进入“待归类”，来源绑定仍为 active，且产生独立 `catalog.entry.curation_withdrawn` 审计。运行态核验同时发现 Console 侧边栏仍残留“目录浏览 / Catalog Browse”，已收敛为“资源浏览 / Resource Browse”；Console Frontend 12 个测试文件 59 项测试与生产构建通过，页面刷新后旧称归零。

### 2026-08-28：认证、撤销认证与弃用治理生命周期收口

- CatalogEntry 治理状态的唯一可逆闭环固定为 `curated → certified → curated`：认证确认当前完整编目聚合，撤销认证必须填写原因并保留全部编目事实；重新认证不是新状态、认证副本或旁路接口，而是“撤销认证 → 编辑编目 → 认证”；
- 通用 `PUT /entries/:id` 已严格收窄为 `discovered|curated` 阶段的完整业务编目和撤销编目。认证、撤销认证、弃用以及弃用信息维护唯一使用 `PUT /entries/:id/governance`，只更新治理状态、可选推荐继任项、聚合版本与领域审计，不调用 Standard / System，也不复制专业事实；
- 当时的 `certified` 状态冻结业务名称、说明、可见性、语义关联、责任关系和组件数据元关联，通用编目接口与前端编辑器均拒绝修改；当时 `deprecated` 不可恢复，只允许填写原因后更新或清除推荐继任项。该弃用规则已按 26.13 节的新共识调整，保留此处历史验收记录；
- 认证前服务端重新检查有效来源、业务名称与说明、`department|tenant` 可见性、有效主业务域、一个责任部门、一个业务责任人及至少一个数据管理员。认证、撤销认证、弃用、弃用信息维护分别写入 `catalog.entry.certified`、`catalog.entry.certification_withdrawn`、`catalog.entry.deprecated`、`catalog.entry.deprecation_updated`；
- 详情页按职责显示操作：`curated` 保留“编辑编目”，更多治理操作提供“认证资源”“弃用资源”“撤销编目”；`certified` 只提供“弃用资源”和“撤销认证”；`deprecated` 只提供“维护弃用信息”。弃用与维护弃用通过 CatalogEntry 名称选择继任项，不允许输入 UUID；
- 后端全量 Go 测试、Catalog Frontend 16 个测试文件 58 项测试与生产构建、Catalog PostgreSQL 标准门禁、完整 `make test-module MODULE=catalog`、Online Runner 86 项确定性测试，以及全仓 Swagger 严格覆盖均已通过；Catalog 当前 24 个公开路由方法与运行路由一致。PostgreSQL 门禁已纳入“认证 → 撤销认证 → 重新认证”的真实事务闭环，不新增独立脚本；
- 统一重启后 Gateway、System、Catalog Ready 均为 `200`，治理子资源经 Gateway 返回认证拦截而非 `404`，Catalog 启动日志确认 `PUT /entries/:id/governance` 已注册。当前已治理资源为 0、资源盘点为 1045；抽验 `discovered` 详情只显示“开始编目”，不显示认证、弃用或更多治理入口。当前没有自然形成的 `curated|certified` 样本，因此不为验收制造不可恢复的弃用事实；受控已编目样本的四个治理入口已登记到统一 `enterprise-catalog-publishing` T4 周期回归。

### 2026-08-31：语义层与指标平台能力基线评估

- 对外科普图书继续保持厂商和实现中立，不写入 ADDP 模块映射；其通用概念只作为 ADDP 内部能力检查基准；
- Catalog 不等同于完整语义层，固定定位为 Enterprise Catalog 与语义关联、治理枢纽。Standard、Model、Meta、Catalog 分别拥有语义定义、业务与逻辑建模、物理事实、资源绑定与治理，任何一个模块都不单独代表完整语义层；
- 当前治理型语义基础已经形成，统一语义查询契约、语义 planner / compiler、指标计算与统一服务输出仍是主要能力缺口。缺口暂不归入 Catalog，不预建 `semantic` 模块；只有真实端到端查询用例证明需要稳定独立运行边界时，才文档优先重新评估 Semantic Runtime。

### 2026-09-06：选定 Outdoor 作为首条语义闭环验证样板

- 端到端验收案例固定采用 [Outdoor 业务数据治理推进方案](Outdoor业务数据治理推进方案.md#1320-户外域语义闭环验证基线2026-09-06) 中的三个已发布指标、四张正式逻辑表和三个查询服务，不另建一套演示领域或重复专题；
- 当前只读基线确认三个 Outdoor MetricDefinition 已生效，但 Model MetricImplementation 为 0；相关十个 CatalogEntry 均已自动建档但仍处于 `discovered + inventory`，因此定义到实现、企业编目和责任落标均作为明确红灯，不把已有固定 SQL 服务误判为完整语义层；
- Catalog 专题只跟踪目录边界与跨模块缺口，具体业务口径、测试夹具和验收结果由 Outdoor 专题唯一维护。可表达性核验已经确认当前 MetricImplementation 尚缺机器可读分组、过滤和同源角色契约，不能先写装饰性记录；下一步先收敛该最小契约和方向性指标的单值语义，再推进指标实现与 Catalog 编目，最后以组合查询验证是否需要稳定语义运行边界。

## 二十二、当前推进状态

| 工作项 | 状态 | 说明 |
| --- | --- | --- |
| 企业目录根因和目标 | 已确认 | 缺少跨专业目录统一身份、关联和发现层 |
| 独立 Catalog 模块方向 | 已确认 | 不再比较 Meta / Asset 扩展路线 |
| CatalogEntry 自动建档 | 已确认 | 所有正式持久化 DataItem 自动创建 |
| Meta / Standard / Catalog 边界 | 已确认 | 技术事实、语义定义、资源关联分别归属 |
| Department / Project Group / Workspace 边界 | 已完成 | 组织与协作不决定目录身份；评估结论为当前不新增统一 Workspace |
| 搜索所有权 | 已确认 | Meta、Catalog、Manager、Asset 分别拥有对应搜索 |
| 责任与实际数据授权贯通 | D1-D3 已确认，正式规范已同步，实施契约待定义 | 责任事实仍归 Catalog；物理源数据授权由 System 引擎访问控制领域唯一维护，访问模块共同执行。首次授权、同人确认／办理与责任移交规则见第二十六节及授权上下文规范 5.5；未实施代码，不沿用既有目录门禁作为数据授权证据 |
| 企业目录实体正式命名 | 已解锁 | Engine Catalog 词族已迁移，裸名 `CatalogEntry` 正式保留给企业目录 |
| 正式术语和概念文档修订 | 已完成 | 企业目录体系图、实现规范、核心概念、模块架构和 owner 边界已同步 |
| 对象、API、变化和权限契约 | 核心设计已完成 | UUID 聚合、游标变化源、批量解析、权限、可见性和重绑状态机已固化 |
| 实现与门禁影响盘点 | 已完成 | Meta / Manager / Asset 删除范围及 T0-T5 登记点已固化 |
| Catalog 模块实现 | 核心能力已完成 | 自动建档、查询、编目、重绑、历史、搜索投影、PostgreSQL 门禁和平台登记已落地；T4 已登记待专用 Runner 首跑 |
| 组织、语义与协作实现 | 已完成 | 组织管理、语义、责任、失效治理队列、个人目录标记、Project Group 目录集合、状态推进和审计均已完成 |
| Manager / Asset 旧路线删除 | 已完成 | Manager owner 内容索引、AssetComponent 单路径、Portal 已发布消费均已收敛，旧 discoverable、AssetRecord 和 `source_reference` 已删除 |
| 企业资源目录 / 资产目录 / 引擎资源树命名收口 | 已完成 | Enterprise Catalog 使用 `CatalogEntry`；Asset 多级导航使用 `AssetCategory`；Manager 技术树使用 `ResourceTree`，三者不复制、不兼容旧分类契约；Portal 按 AssetCategory 子树消费，管理端按直接归属管理；AssetCategory 可通过名称层级选择器安全调整父目录 |
| Model 专业目录接入 | 已完成 | Entity / LogicalTable 自动建档，当前专业事实动态解析，Catalog 仅保存最小可重建投影且不复制 Model 语义 |
| Standard Metric 专业目录接入 | 已完成 | Metric 自动建档，当前专业事实动态解析，指标定义与内生关系仍只归 Standard |
| Service QueryService 专业目录接入 | 已完成 | QueryService 自动建档，当前最小摘要动态解析，服务定义、执行契约和消费描述仍只归 Service |
| Develop 可复用开发成果接入 | 已完成 | 只为 `query|workflow` DevTask 自动建档，当前专业事实动态解析，任务内容和执行事实仍只归 Develop |
| Quality 当前摘要接入 | 已完成 | PostgreSQL DataItem 详情按结构化物理引用动态组合评分与问题摘要，Catalog 不复制 Quality 事实 |
| Meta DataItem 血缘联邦视图 | 已通过运行态验收 | 当前 User Token 直连 Meta、共享图组件按需加载、不复制边；抽验详情成功展示 4 个节点、3 条关系 |
| Model / Standard 专业关系联邦视图 | 已通过运行态验收 | Standard 关系空状态正常；Model Entity 抽验成功动态返回 2 条一对多专业关系，均由当前 User Token 直连 owner |
| Catalog 推荐继任关系 | 已完成，待运行态验收 | 唯一 Catalog 自有跨条目关系；Catalog 定向门禁、System IAM PostgreSQL 和全仓授权门禁均已通过；不建立空泛通用关系表，不与 owner 专业关系类型重叠 |
| Catalog 治理覆盖率 | 已通过七维运行态验收 | 1,002 个 active 条目动态聚合；责任部门、业务责任人、数据管理员独立显示，组件数据元适用 189 个，不保存覆盖率投影 |
| 治理覆盖率缺口处置闭环 | 已通过运行态验收 | 未覆盖数字与两个虚拟入口均进入同口径权威缺口列表并复用现有编目编辑器；不新增明细表、任务实体或搜索投影 |
| 联邦影响分析与目录导航 | 已通过运行态验收 | Model 稳定 ID 和 Meta fingerprint 均已动态解析并完成可见 CatalogEntry 跳转；不代理、不复制 owner 关系边 |
| 治理目录 / 资源盘点视图 | 已通过运行态验收 | 默认治理目录当前 0 条；显式资源盘点展示 998 条且仍使用同一批自动建档 CatalogEntry |
| Catalog 列表人类可读分面选择器 | 已通过运行态验收 | Domain 与 Engine Instance 已展示真实名称；Department 动态解析请求成功但当前无候选，列表不再保留裸 ID 输入和引擎 ID 列 |
| 企业目录上下文导航 | 已通过运行态验收 | 客户域 4 项即时收窄为 2 个业务实体和 2 个逻辑模型，URL 可恢复；不建立企业目录树或第二查询路线 |
| 资源盘点“待归类”入口 | 已通过运行态验收 | 虚拟入口复用 `primary_domain=missing` 权威缺口；不伪装成 Domain、不复制条目 |
| 资源盘点“待分配部门”入口 | 已通过运行态验收 | 保留上游业务域并复用 `accountable_department=missing`；不伪装成 Department、不复制责任事实 |
| Catalog 编目与治理队列名称选择器 | 已通过运行态验收 | 五类 owner 动态选择器、真实名称候选及治理队列 20 个 CatalogEntry 名称候选已验证；治理队列遗漏 inventory 视图的问题已修复并加回归测试 |
| Project Group 集合名称解析 | 接口与空状态已通过运行态验收 | `/me/project-groups` 返回 200 且无裸 ID 回退；当前身份没有有效 membership，实际成员名称组合待具备成员关系的验收身份补证 |
| Catalog 显式成员批量治理 | 已通过运行态验收 | 当前页显式多选、Domain/Department 名称候选、逐条版本、整批原子回滚、owner 边界、共享 `batch_id` 审计、过期页面冲突保留和测试事实回收均已在真实页面验证；专用 macOS 继续随现有 T4 做周期回归，不再承担首次验收 |
| Catalog 编目状态与页面信息架构 | 已完成并通过运行态验收 | 撤销编目原子重置、状态化操作、同页“查看范围”、正交名称分面、默认折叠高级筛选和四个可恢复详情 Tab 已实现；两个范围不拆分页面、不复制列表，两条误编目 Workbench 样本已正式撤销并核验审计、来源绑定、户外域清除与待归类归属 |
| Catalog 认证与弃用治理生命周期 | 原生命周期已验收；新增撤销弃用及责任移交见 26.13 | 通用编目与治理子资源单路径分离；认证冻结编目事实，撤销认证保留事实；弃用可按独立权限撤销为已编目，责任可单独移交；继任项只能名称选择 |
| 跨模块语义体系能力基线 | 已建立 | Catalog 只承担语义绑定、治理和发现；Standard、Model、Meta 保持专业 owner。统一语义查询、计划编译、指标执行和 BI / AI 稳定接口是后续跨模块缺口，不默认归 Catalog 或预建新模块 |

阶段 5 的专业来源接入、动态当前事实、基础联邦关系视图、默认治理目录、权限资源盘点、企业目录上下文导航、“待归类”与“待分配部门”虚拟治理入口、七维治理覆盖率、治理缺口处置、联邦来源身份导航、列表分面、编目/治理队列名称选择器、Manager 精确定位和统一 Workspace 评估，以及阶段 6 的显式成员批量治理均已完成。Project Group 组合接口与无成员空状态已通过，真实成员名称只缺有 membership 的验收身份证据。专用 Runner 首跑 `enterprise-catalog-publishing` 以及停机追赶、显式重绑、System 恢复等专项 T4 已纳入统一验证体系，它们需要专用 User Token、Tenant、Fixture Engine、Domain、Department 或服务停机窗口，属于外部条件验证而非本专题实现缺口。技术来源详情中的 fingerprint、Item ID 等继续只保留在明确的技术溯源区。统一 Workspace 评估结论是当前不新增；只有第 10.5 节的端到端触发条件成立时才重开。

## 二十三、后续会话接力清单

新会话应以本专题和 [企业资源目录实现规范](../spec/addp企业资源目录实现规范.md) 为事实基线，不重新讨论已确认的模块边界，也不要恢复 Meta / Standard 反向投影、Catalog owner 全量副本或默认平铺全部 DataItem 的旧方向。

1. 先检查工作区和运行态，保留其他并行改造；本轮统一重启与主要目录交互验收已经完成，不要重复重启或重做已通过项。
2. 治理覆盖率、Meta 血缘邻居和 Model 专业节点的来源身份导航已完成运行态验收；后续会话不要重复重启或重复制造样例，只有相关契约再次变更时才重跑这些路径。
3. 若能提供具有有效 Project Group membership 的验收身份，只补验集合筛选、创建选择器和详情中的项目组名称；不为验收临时修改组织数据，也不把名称加入 AuthContext 或 Catalog 副本。
4. 在专用 Runner 配置 User Token、Tenant、Fixture Engine、Domain、Department 后执行 `enterprise-catalog-publishing`，回填自动建档、完整编目、失效引用治理与清理证据。
5. 在明确的停机窗口执行来源 missing、停机追赶、显式重绑、System 恢复和 owner `503` 专项 T4；不得在共享开发服务上擅自停止 System、Standard、Meta 或 Catalog。
6. 推荐继任项的名称选择器已有定向门禁；当前已治理资源为 0、资源盘点为 1045，没有可进入弃用流程的 `curated|certified` 样本。后续只在自然产生合适治理状态样本后补运行态写入证据，不为验收篡改 CatalogEntry。
7. 如需重跑完整 Catalog 模块门禁，先用 `bash scripts/infra/status.sh` 核对 `addp-postgres` 实际宿主机端口，再显式设置指向本地 `addp_test` 的 `CATALOG_POSTGRES_TEST_DSN` 并执行 `make test-module MODULE=catalog`；不得假定首选端口当前有效，也不得新建本地测试 database。
8. System 全量 IAM PostgreSQL 门禁与全仓授权门禁已在 AssetCategory permission migration 109 收口后通过。后续若再次失败，应按对应 owner 根因处理，不得放宽计数、跳过测试或恢复 `asset.catalog.*` 兼容权限。
9. 治理缺口处置、企业目录上下文导航、“待归类”“待分配部门”和七维治理覆盖率均已完成运行态验收；后续会话不要重复重启或重做共享环境点击，只在相关契约再次变化时重跑。
10. 旧 `accountability` 已从 API 响应、Swagger、URL 和页面删除，只在负向测试与规范删除说明中出现；不得恢复复合维度或兼容 query。
11. 统一 Workspace 评估已收口为“当前不新增”；不得为了形式上统一而添加空壳模块、`workspace_id` 或把引擎 `SpatialWorkspace` 与企业协作概念合并；只在第 10.5 节触发条件成立时重开文档优先评估。
12. 显式成员批量治理已经完成代码、Swagger、PostgreSQL、前端、Online 契约登记和本机真实写入验收；后续不要增加“全部匹配筛选”、手工 owner ID、Tenant 级 `revision` 或逐条部分成功路线。专用 macOS 只需随既有 `enterprise-catalog-publishing` 做当前页选择和对话框的周期回归，不在永久 fixture 上重复提交批量写入。
13. **已完成**：正常统一重启后只读确认 Asset schema 中不再存在 `catalogs_id_seq`、`catalogs_pkey`、`idx_asset_catalogs_*` 或 `idx_asset_assets_catalog_id`；迁移由 Asset 启动自动应用，没有手工改库或单独接管整套服务。
14. **已完成**：AssetCategory 父目录选择器、完整更新契约、所有前端更新调用方、同 Tenant 目录图事务锁、环路/跨 Tenant/同级重名/版本冲突校验、统一 Asset 门禁，以及真实页面“移动→移回→刷新”和“重命名→恢复”验收均已通过；测试目录已恢复原名称与层级，树高亮与详情状态同步，浏览器无 warning/error。后续不要新增 `/move` 路由、局部更新负载、手工 ID 输入或第二份目录关系。
15. **已完成**：Catalog 编目状态与页面信息架构已完成实现、确定性门禁、统一重启和真实页面验收；`4ff43d3a-3815-49fc-80e7-831dd7cc92b8` 与 `f0359420-a884-4e11-a87f-bd60e37a65e2` 已正式撤销编目，均恢复为 `discovered + inventory`、离开户外域并进入“待归类”，来源绑定保持 active，且分别产生 `catalog.entry.curation_withdrawn` 审计。后续不要直接写数据库、新建撤销路由或恢复通用“业务编目”按钮。
16. **原生命周期已完成**：Catalog 认证与弃用治理生命周期已完成当时的文档、前后端、Swagger、模块门禁及只读页面验收；新增撤销弃用与已弃用责任移交按 26.13 节推进。不得为验收直接改开发库或制造真实治理变更；受控样本的状态化入口继续由统一 T4 周期回归验证。

## 二十四、2026-09-27 目录价值呈现复核与新一轮清单

用户实际体验表明，虽然 Catalog 已有跨来源建档、专业事实联邦、责任、语义、关系、个人集合与治理覆盖率，主入口仍容易被理解成“只给资源打业务域和部门”。新一轮改进不扩大 Catalog 的专业事实所有权，目标是让使用者沿“找到资源 → 理解含义和来源 → 识别关系与治理缺口 → 协作治理 → 判断能否进入资产发布”走通现有能力，再补真正缺失的语义绑定。

1. **业务域规则和误导性操作（代码与门禁已完成，运行态只读核对已完成，页面点击待验）**：业务域定义唯一归 Standard；Model Entity / LogicalTable、Standard MetricDefinition 的主域由各自 owner 管理，Catalog 只动态引用，不能在批量治理中提供主域写入。Standard 的 `platform|tenant_common` 指标在 owner 无 `domain_id` 时属于“公共且无专属归属域”，可以编目和认证，在主域覆盖率中记为不适用而非待归类。Standard 变化源带 `scope_type` 并对存量指标补发变化事件，保证已有 Catalog 来源摘要能追上当前事实。
2. **已有能力可见性（代码与门禁已完成，运行态路由核对已完成，页面点击待验）**：Console 企业目录导航及功能搜索补上 Catalog 已有的“我的目录”“项目集合”“责任治理队列”入口；不新增第二套数据或页面协议。资源详情明确区分 Catalog 自有语义关联与 owner 管理的主域，提供跳转专业事实的入口。
3. **领域一致性回归（只读核对已完成，页面点击待验）**：以 Outdoor 固定样本核对 Standard 指标、Model 模型、Meta DataItem 与 Catalog 主域的 owner 和适用性，不能把 Quality 计划/规则的 `owner_domain_id` 误认作被检查 DataItem 的主域；记录真正的来源冲突，不通过复制业务域字段消除表面不一致。
4. **StandardMapping 正式闭环（代码与门禁已完成，真实页面只读部分已验，业务写入闭环待做）**：Catalog 已把组件 Element 旧关联替换为有独立身份和并发版本的 StandardMapping 候选、审核、驳回与撤回路径；人工候选写入使用 `catalog.entry.update`，审核决策使用独立的 `catalog.standard_mapping.review`，不借用条目认证权限。Catalog 数据字典只读取已审核映射并精确解析冻结的 Standard ElementRevision；治理覆盖率只计算已审核映射，条目认证审计固定当时的已审核映射 ID 与版本。前端详情以独立面板展示候选及审核动作，旧条目编辑负载不再包含映射；历史 `component_element_associations` 在数据库迁移事务中保留证据并转为 `legacy/proposed`，随后删旧表，不伪造历史修订和审核。Catalog、Standard 的完整 `make test-module` 门禁、System IAM 全量 PostgreSQL 门禁、Console 前端单测/浏览器测试/构建、Swagger 路由覆盖均已通过；候选更新的返回版本与持久化版本一致性已有回归测试。重启后运行态的进程、迁移、角色授权与路由已只读核对，登录态候选表单与未审核字典已点击验收；候选提交、审核及已审核字典的真实业务闭环仍待验。Copilot 专用 Service Principal 提交候选的对接尚未实施，不能把人工候选 API 误记为 Copilot 集成完成。
5. **按角色组织入口（进行中）**：基于同一 `CatalogEntry` 查询，区分普通使用者的资源发现/理解、业务治理者的待办/覆盖率、管理员的资源盘点；优先改文案、默认视图和导航，不为每个角色复制资源实体或新建目录树。当前先在同一资源浏览页按视图收敛列表列：默认“已治理资源”突出业务说明和治理状态，不再以来源引擎、来源状态和目录可见性占据主要阅读空间，但来源失效仍在名称旁明确提示；“资源盘点”保留完整诊断列。既有责任治理队列、覆盖率和盘点权限入口不改。还需结合真实使用者与治理者身份检查菜单优先级及默认进入方式，不能仅凭管理员账号代替普通使用者验收。Asset 发布仍归 Asset，Portal 消费仍只归 Portal。

本轮代码变更的验收门禁为 Standard、Catalog、Console 的 `make test-module`，其中 Standard/Catalog 包含 PostgreSQL T2，Console 包含前端测试和构建；运行态页面证据须在相应服务加载新版本后另记，不把编译或单元测试等同于真实页面验收。

门禁记录：Catalog、Console 的完整 `make test-module` 均已通过；Standard 的 T0、Go T1、前端 T1/T3（含 103 项浏览器测试及构建）通过，PostgreSQL T2 由 `make test-standard-postgres` 单独通过，包含存量摘要补发与重复迁移幂等回归。Standard 首次完整命令因 `.env` 测试密码与运行中的 `addp-postgres` 凭据不一致，在 T2 连接前失败；随后只读提取该容器自身配置并对 URL 密码编码，未修改数据库凭据。

2026-09-28 重启后运行态核对：Catalog、Standard、Model、Meta 的 `/health/ready` 均 Ready，Console/Catalog 前端返回 200，Catalog 未登录 API 返回预期 401。当前服务下发的 Console 导航/功能搜索代码已有“我的目录”“项目集合”“责任治理队列”入口，Catalog 前端下发代码已有 owner 主域写入限制。只读数据库核对 Standard 唯一 Outdoor 域 `id=1`：3 个 Standard MetricDefinition、3 个 Model Entity、4 个 Model LogicalTable 的 owner 主域均为 `1`，对应 10 条 Catalog 当前 active 来源摘要也均为 `domain_id=1`，没有发现这批 owner 来源的主域冲突；这 10 条仍为 `discovered + inventory`，不把来源主域误记为已业务编目。Meta 中可辨认的 `ods_outdoor_*`、`dim_outdoor_*`、`dwd_outdoor_*`、`dws_outdoor_*` DataItem 仍是资源盘点项，当前没有主域；Meta 扫描本身不推断业务域，应由 Catalog 治理关联，不能据名称自动判定其域。浏览器自动化两次超时，因此登录态页面点击与交互验收尚未完成，不记为通过。

StandardMapping 权限已确认并落实到 Manifest、IAM 和 Catalog API：`catalog.entry.update` 用于人工创建、编辑、删除候选；独立的 `catalog.standard_mapping.review` 用于审核通过、驳回和撤回；`catalog.entry.certify` 只用于整个目录条目认证。第一阶段不强制提交者与审核者为不同自然人，但各操作独立审计。Copilot 的候选提交若后续接入，只允许专用 Service Principal，始终只产生 `proposed`，不赋予 review 权限；当前尚无 Copilot 专用提交入口。

2026-09-28 再次重启后的 StandardMapping 只读运行态核对：System、Standard、Catalog、Gateway 的 `/health/ready` 与 Console/Catalog 前端入口均返回 200；System `schema_migrations` 为 `161,false`，`catalog.standard_mappings` 已存在且旧 `catalog.component_element_associations` 已不存在。新权限 `catalog.standard_mapping.review` 为 active，已授予产品内置 `tenant.administrator` 角色；Catalog 当前 Swagger 已包含候选、修订选项、审核通过、驳回与撤回路由，未登录访问候选和修订选项均返回预期 401。运行中的 Catalog 前端已下发 `StandardMappingPanel`，其候选创建、修订选项与审核调用均在当前资源中。`make test-catalog-frontend` 再次通过（16 个测试文件、60 项测试及构建）；现有前端测试验证 API 路径，但不等同实际组件点击。主库目前有 2355 个 Catalog DataItem Component、7 个已发布 Standard ElementRevision，但 StandardMapping 记录为 0；这只能证明结构和路由加载，不能证明任一实际字段与标准之间存在合理的业务映射。只读核对发现 `outdoor.dws_outdoor_person_metric.person_id` 与已发布标准“人员标识”在名称和数据类型上相符，可以作为待业务确认的验收候选，尚未认定或写入。该次检查时 Mac 锁屏，浏览器自动化未能完成页面点击；也不为验收在共享主库制造未经业务确认的已审核映射。其后的页面只读验收与待完成的写入闭环分别记录如下，不能把路由和结构核对误记为主库业务闭环通过。

同日解锁后的真实页面只读验收：从 Console 目录详情及 Catalog 页面打开 `dws_outdoor_person_metric`，在“编目信息”看到独立字段标准映射面板和空状态；候选表单可按名称选择 `person_id`、Standard“人员标识 · outdoor_person_id”以及“R1 · 人员标识”，无须输入技术 ID。“专业事实”的联邦数据字典仍把 `person_id` 显示为“未关联数据元”，符合主库没有已审核映射的现状。验收发现候选表单误用“保存编目”按钮文案，已改为新建时“提交候选”、编辑时“保存修改”；空映射表渲染还曾请求 `catalog.mapping.sources.undefined` 翻译，定位为无真实映射行时的占位渲染，已只对具有映射身份的行翻译来源。新浏览器页再次打开空表后无 warning/error，主库映射数保持 0。此轮仅证明选择器、空状态和未审核字典的真实页面行为；映射写入、审核后的字典和覆盖率，以及不同权限主体的按钮状态尚未验收，不能视为正式业务闭环完成。

本次前端修正后的 `make test-module MODULE=catalog` 已通过（平台 T0、Catalog Go T1、前端 16 个文件/60 项测试与构建、Catalog PostgreSQL T2；测试库仅用 `addp_test`）。现有门禁和这次页面观察没有代替正式落标的业务确认；只有确认 `dws_outdoor_person_metric.person_id` 与 Standard“人员标识”R1 的语义一致，才在共享开发主库提交并审核候选。

同日按角色入口的第一步：Catalog 同一资源浏览路由已按 `governance|inventory` 显示不同列表列，未增加资源副本或第二套目录。真实页面只读检查表明，“已治理资源”表头为目录名称、资源类型、业务说明、治理状态、更新时间；“资源盘点”保留来源状态、可见性和来源引擎等诊断列，且新页面无浏览器 warning/error。已治理资源的来源失效仍在名称旁保留显式警示，不因隐藏诊断列而掩盖异常。当前真实 Tenant 默认已治理视图为 0 项，因此只验到表头与空态，不能据此声称有业务内容可发现；现有跨模块 `enterprise-catalog-publishing` 浏览器用例已增加同一已编目样本在两种视图中的列断言，等待专用 T4 再验真实内容。`make test-module MODULE=console` 已通过平台 T0、107 项前端单测、81 项确定性浏览器测试与构建；`make test-module MODULE=catalog` 使用容器实际 `addp` 用户和允许的 `addp_test` 重跑后通过平台 T0、Catalog Go、60 项前端单测与构建、PostgreSQL T2。首次 Catalog 完整命令误用 `postgres` 用户导致 T2 认证失败，随后只修正测试 DSN，未修改数据库、迁移或凭据。Online T4 在本机没有专用环境，本轮未执行。

同日补齐映射列表的固定修订展示：此前前端以 `element_revision_id` 直接拼出“R ID”，会将数据库 ID 错当修订号。现在 CatalogEntry 详情只通过现有 Standard 精确修订批量接口动态取得数据元名称、编码和真正的 `revision_no`，返回可丢弃的 `element_reference` 展示摘要；历史已撤回发布修订仍按固定 ID 解析，不能滑到当前修订。Standard 不可达、固定修订缺失或迁移候选尚未固定修订分别显示明确不可用/待补选状态，映射关系仍可读取；组件失效也不把裸 ID 当名称展示。没有新增数据库列、反向投影、兼容路径或新的 Standard 端点。后端测试覆盖历史修订、缺失、未固定、Standard 不可达时详情继续可读；前端测试覆盖人类可读文案与 ID/修订号区别。Catalog Swagger 已同步，完整 `make test-module MODULE=catalog` 再次通过平台 T0、Go T1、前端 17 个文件/62 项测试与生产构建、PostgreSQL T2。当前共享开发库仍无正式映射，不以单测冒充真实已审核列表的运行态验收；后端新响应需服务加载新代码后方可在页面观察。

## 二十五、业务域上下文与资源浏览收拢

本节回应“Catalog 不能只像一个给资源填写业务域和部门的表单”的真实使用反馈。参考[《数据治理 100 问》第 043 问](../books/数据治理100问/05-元数据、资产与语义篇/043-资源目录体系与数据资产目录.md)对企业目录的通用定位：各专业模块维护事实，企业目录提供跨目录身份、关联、发现与治理视角。ADDP 的具体模块边界仍按本专题和正式规范执行：Standard 定义业务域、标准与指标，Model 定义实体和逻辑模型，Catalog 组织企业资源身份与自身关联，Asset 负责资产组合和发布；书中“企业目录也可提供资产视图”的通用表述不意味着把 Asset 事实迁入 Catalog。

### 25.1 页面目的与导航

- 业务域定义仍由 Standard 唯一维护；Catalog 只为 DataItem 等自有归属关系的资源引用权威 Domain，Model / Standard 对象的主域仍以专业 owner 为准。业务域虽有定义和跨模块语义，但也是目录浏览的一项正交维度，不应独占 Catalog 首屏或侧边栏独立入口。
- Catalog 统一以 `/entries` 为“资源浏览”入口：未选择业务域时只展示多维筛选和权威分页列表；选择任一业务域后，在同页增加定义、层级和按类型汇总的紧凑上下文，类型入口继续改变同一列表的 `entry_type` 筛选。业务域下拉可搜索 Standard 当前全部业务域，包含零资源域；ID 只进入规范 `primary_domain_id` URL，不要求用户输入。不为部门、资源类型各建同构页面，也不建立第二套资源列表、目录树或 `CatalogEntry`。
- 切换业务域应更新同页上下文、URL、计数与列表；各区独立加载和显示不可用状态，一个专业模块暂不可达不得让 Catalog 页面或服务整体不可用。域上下文的类型数量使用当前查看范围、精确主域与责任部门分面，不包含名称与高级筛选；列表结果数另按完整查询显示，不得混称为同一口径。
- “全部相关信息”表示完整的导航范围与可继续分页查看的对象，不表示把所有数据项一次性渲染成超长页面。域内资源只展示按类型数量和同页筛选入口，具体条目由下方权威分页列表呈现。

### 25.2 页面信息结构与事实来源

| 区域 | 首版展示与用户要回答的问题 | 权威事实 / 现有入口 | 边界与当前缺口 |
| --- | --- | --- | --- |
| 业务域选择与概况 | 当前域的名称、编码、定义和层级路径；“我正在看哪个业务范围？” | Standard Domain，经 Catalog `GET /domains` 裁剪动态读取 | Catalog `/entries/facets` 只列当前可见条目实际引用的域，不能作为包含零资源域的完整选择列表；Catalog `/reference-candidates` 是编辑专用且要求 `catalog.entry.update`。读取权限契约见 25.5。 |
| 专业语义 | 该域定义的术语、数据元与指标各有多少、是什么；“本域如何定义业务概念？” | Standard 的按 `owner_domain_id` 查询和专业详情；指标也有 CatalogEntry 可导航身份 | 公共且无专属归属域的标准不算本域对象；不把 Standard 定义复制为 Catalog 可编辑记录，不因为 Catalog 可读就绕过 Standard 专业读取权限。 |
| 模型与企业资源 | 本域业务实体、逻辑模型、指标、数据项、服务、应用等分类数量与分页入口；“定义落到了哪些具体资源？” | Model / Standard 对象的 owner 主域；Meta DataItem、Service 等以 Catalog 自有主域关联；Catalog `/entries` 与 `/entries/facets` 已有按 `primary_domain_id` 的权限感知列表 | Model / Standard 主域由 owner 维护，Catalog 只动态引用；未业务归类的 Meta DataItem 不因技术名称含 `outdoor` 就归入户外域。同一专业对象已有 CatalogEntry 时不在两个资源区重复计数。 |
| 已证实的关联 | 从所选资源进入“标准映射、模型关系、来源、血缘、继任”等已有详情；“这些对象之间有什么已确认的关系？” | Catalog StandardMapping / 来源绑定；Model、Standard、Meta 各自的专业关系读取 | 同属一个域只是共同出现，不能画成依赖边。首版只从对象进入现有关系详情，不预设一张尚无权威查询契约的域级全量关系图。 |
| 治理情况 | 域内已编目、已认证、待治理资源及可处理入口；“缺口在哪里、谁负责？” | CatalogEntry 状态、责任、治理任务与当前资源盘点查询 | 现有 `/governance/coverage` 是全局统计且没有业务域参数，不能直接显示为域内覆盖率；域级覆盖口径与权限须单独确认。`discovered` 和缺口只对有盘点权限的治理者出现。 |
| 资产产出 | 未来可显示“使用本域资源的已发布资产”，并跳至 Asset / Portal；“哪些成果已对外提供？” | Asset 的发布与组成关系 | Asset 可组合多个域的 CatalogEntry，不能把资产强行判为单一域。现有公开路由未提供按 CatalogEntry 反查已发布资产的明确契约，首版不伪造数量或在 Catalog 保存资产副本。 |

### 25.3 “属于”和“相关”的判定

1. **归属本域**：Standard / Model 对象采用 owner 声明的专属主业务域；由 Catalog 管理主域的资源采用 Catalog 当前确认的主域。此组构成首版默认的“域内资源”与数量口径，先按精确选中域计算，不暗含父域下全部子域。
2. **与本域有关**：其他域对象只有存在明确的 secondary Domain 关联或由事实 owner 提供的专业关系、Catalog 已审核 StandardMapping 等证据时，才可作为另一个标明关系类型和来源的区域呈现；不得并入“归属本域”计数。首版先提供逐对象已有关系的进入路径，域级反向关联检索待契约核查后再做。
3. **尚待归类**：没有已确认主域的 DataItem 留在有盘点权限的治理缺口视图，不列入任一选中域。`platform|tenant_common` 且无专属域的公共标准是“公共”，不是治理缺口。技术路径、名称相似、同一部门或 Asset 组合均不能自动证明域归属。
4. 如果未来需要“包含子域”，必须提供显式开关并分别标注当前域与子域数量；不能悄悄改变精确域筛选和计数语义。业务域不是内容访问权限、审批边界或 AssetCategory 树。

### 25.4 首版交互与验收样例

建议从任意可见业务域切换到另一个域时，同一 `/entries` 页面切换定义、分组数量和权威列表；刷新可分享 URL 后保持所选域。阅读者默认看到其可发现的已治理资源，拥有 `catalog.inventory.read` 的人员才可切换到资源盘点和缺口。零资源域仍能选中并看到 Standard 定义，列表显示当前范围无结果；owner 不可达时显示“当前专业信息不可用”，已有列表与分面仍可独立使用。

验收先以户外域检查一条真实导航链：在资源浏览中选域 → 看见 Standard 定义与 Model / Standard 已声明归属的目录对象 → 进入各自 owner 详情 → 返回同一筛选列表。再选择另一个域或无资源域，证明页面、URL、权限和空态均不依赖硬编码的户外域。当前已观察到的 Outdoor 名称、数量和技术路径只用于核对，不替代业务确认；不得为填满页面而把未确认 DataItem 或 StandardMapping 写入共享主库。

### 25.5 实施前必须确认的契约

| 问题 | 当前只读核对 | 建议方向 / 待确认决定 |
| --- | --- | --- |
| 普通 Catalog 阅读者如何选择所有可浏览业务域？ | Standard `/domains` 要求 `standard.domain.read`；Catalog 分面仅含已引用且当前可见的域，编辑候选要求 `catalog.entry.update`。 | **已确认**：`catalog.entry.read` 可以发现本 Tenant 的业务域名称、编码、定义和层级。Catalog 通过自身只读接口动态读取 Standard 当前 Domain，并严格裁剪为域概况；不授予 Standard 专业管理权限，不持久化 Domain 副本。 |
| 域内专业定义是否向仅有 Catalog 阅读权限的人展示？ | Standard 术语、数据元、指标各有独立 read Permission；Model 实体与逻辑模型亦有独立 read Permission。 | **已确认**：Catalog 读者可看目录自身有权展示的资源摘要和治理事实；术语、数据元和指标清单由页面持当前 User Token 分别读取 Standard，缺少对应读取权限时不请求并明确显示不可访问。详情入口还须满足 Console 的相应路由权限；首版不使用 Catalog 运行身份代理专业详情，也不将专业定义写入 Catalog。 |
| 域级治理数字从哪里来？ | 现有 Catalog `/governance/coverage` 只聚合全局盘点，`/entries` 可按主域查询。 | 先确定所选域、治理视图、适用对象与子域的分母口径，再决定是否需要 Catalog 的域范围即时聚合；不得给全局数字换上域名称。 |
| 域级关系和资产结果是否进入首版？ | 现有关系主要在条目详情按 owner 联邦展示；Asset 没有明确的按域或按 CatalogEntry 反查已发布资产的公开消费契约。 | 首版只提供有证据的逐对象关系导航；资产结果与域级关系汇总待查询、权限及跨域计数契约确认后追加，不建泛化关系表或资产投影。 |

首版已确认的域概况、资源分组和 Standard 专业定义分页摘要收拢到唯一 `/entries` 页面，资源盘点只对额外具有 `catalog.inventory.read` 的人员开放。专业定义分别请求 Standard `/glossaries`、`/elements`、`/metrics`，限定 `scope_type=domain` 与精确的 `owner_domain_id`；每类数量采用该 owner 分页响应的 `total`，含其当前可读生命周期对象，不等同已发布数，也不等同 CatalogEntry 数量。公共范围定义不算所选域对象；父域不隐含子域。三个读取权限、加载、拒绝和不可用状态独立，不能因一类失败阻断 Catalog 资源列表。域级治理覆盖率、全量关系图和资产结果仍待确认，不得用域内 CatalogEntry 数量冒充其统计。本节不授权更改业务域归属、标准映射或资产发布事实。

### 25.6 首版实现跟进

- [x] 在 Catalog 提供 `GET /domains`：使用现有 Standard 公共读取契约和 Catalog Tenant 运行权限动态获取业务域树，只返回当前 Tenant 的名称、编码、定义、层级与字符串稳定 ID；Standard 不可达只影响这次概况读取。
- [x] 第一版曾增加 `/domains` 独立页面；真实使用后确认与资源浏览重复，本轮收拢到唯一 `/entries` 页面，不保留前端旧路由或菜单入口。
- [x] `/entries` 从 Standard 域概况读取完整下拉候选；选中后同页展示定义、路径与可点击的类型计数，保留唯一权威分页列表、规范 URL、零资源域、权限与独立不可用状态。Catalog 模块门禁（含 PostgreSQL 集成）、Console 前端门禁和共享前端测试已通过；真实 Tenant 的多域交互验收仍单列如下。
- [x] 普通 `catalog.entry.read` 与额外 `catalog.inventory.read` 的页面范围分离；专业详情只跳转 owner/既有条目页面，不用 Catalog 服务身份扩权代理。
- [x] 在选域上下文展示 Standard 术语、数据元、指标的独立权限分页摘要；以 Standard 精确归属域查询的 `total` 计数，按当前 User Token 授权，独立错误/空态，详情路由另验权限。Catalog 前端定向测试与构建、完整 `make test-module MODULE=catalog`（含 PostgreSQL 集成）通过；前端变更由现有 Catalog 自动发现和 CI 门禁覆盖，无新增路由或 Swagger 契约。
- [ ] 在运行中的真实 Tenant 验收至少两个业务域、一个空域以及 Standard 短时不可达；当前单测、构建和 PostgreSQL 集成门禁不能替代这项交互验收。
- [ ] 讨论跨资源关系、域级治理覆盖率与 Asset 结果的独立权限、查询和计数契约；确认后再纳入页面，不以同域共现推断关系。

2026-09-28 本机只读核验：开发 Tenant 的 Standard 当前仅有一个有效业务域“户外域”，当前浏览器的权限验收账号也没有 Catalog 模块权限，因此不能据此勾选真实多域、空域与降级验收。现有 `enterprise-catalog-publishing` 在线浏览器用例已补充对已配置业务域的定义展示、类型筛选和刷新后 URL 保持的断言；该在线用例需要专用 User 凭据与环境，尚未在本机执行。后续应在专用验收环境准备第二个业务域和一个零资源域，运行现有在线门禁，再人工核对 Standard 不可达时列表独立可用。

同日补齐该 Online 套件的契约回归：Python 断言现使用独立 `component_standard_mapping` 治理覆盖维度，编目请求不再携带已删除的 `component_elements` 或旧的 `deprecation_reason` 字段；重复运行若发现永久夹具的主业务域与专用环境配置不一致，会在写入前明确失败，不擅自改写业务归属。`make test-online-runner` 的确定性测试与 CI 注册检查通过；这不等于已执行专用环境的 `enterprise-catalog-publishing` T4，也不满足上面的多域、空域及 Standard 不可达真实交互验收。

同日新增 Standard 专业定义摘要：页面持当前 User Token 分别请求术语、数据元和指标的精确归属域分页列表；某类无读取权限不发请求，`403` 与 owner 不可用分别提示，域切换忽略旧响应；只有满足 Console 专业详情路由权限时才给出跳转。此摘要不占用 CatalogEntry 的资源类型计数，也不复制专业定义。定向测试覆盖请求参数、独立权限、旧响应与失败分类；尚未执行真实多域/空域及 Standard 故障的在线交互验收。

### 25.7 本域专业内容的收拢与扩展

- 选中业务域后的概况卡片始终展示 Standard 定义与 Catalog 资源类型入口；“本域专业内容”作为卡片内默认收拢、可展开的区域，按需读取专业模块，不另设独立页面或同构资源列表。切换业务域时按新的精确域重新读取，过期响应不得覆盖当前域。
- Standard 的术语、数据元、指标仍由 Standard 分别授权和分页。Model 的实体、逻辑模型已经拥有 CatalogEntry 并进入上方类型入口与下方权威列表；展开区仅提供保留 `domain_id` 的专业页面入口，不重复列出这些对象或再制造一组资源数量。
- Quality 的规则、方案及当前待处理问题分别按 Quality 公开列表的 `owner_domain_id` 精确过滤，使用当前 User Token 和各自的 `quality.*.read` 权限；仅显示 owner 返回的数量、少量导航摘要及保留域筛选的专业入口。不保存 Quality 投影，不使用需要额外 Monitor 权限的质量 Overview 冒充域质量评分。Quality 的“本域负责”是治理责任归属，不表示被检查的物理数据属于本域；待处理问题只计 `open`，不等于全部问题或域内数据质量结论。
- 上方 Catalog 类型数采用当前查看范围、主域和责任部门分面；展开区的 Standard 与 Quality 数字仅按所选精确业务域及各自专业权限计算，不随责任部门、目录范围、名称或资源类型筛选变化。界面分别标出这些口径；某个 owner 不可用或拒绝访问只影响自己的区域，不能阻断 Catalog 列表。公共归属、子域及仅有名称相似的对象均不并入本域数量。

实施跟进：

- [x] 将独立的 Standard 摘要收进业务域卡片的默认收拢区，展开后按需加载；Model 提供带精确域筛选且受 Console 路由权限约束的专业入口，不重复目录对象。
- [x] Quality 规则、方案及 `open` 问题使用当前 User Token 分别查询精确 `owner_domain_id`，独立处理读取权限、拒绝访问、owner 不可用与域切换旧响应；不调用 Quality Overview 推断域质量评分。
- [x] 中英文页面分别说明 Catalog 分面与专业 owner 数字的不同范围。定向测试覆盖请求路径、权限、旧响应与收拢结构；`make test-catalog-frontend` 和使用本机已核实 PostgreSQL 映射的 `make test-module MODULE=catalog` 已通过。平台 Swagger 覆盖检查对当前 Agent FastAPI 投影有 warn-only 告警，不属于本次 Catalog 前端变更。
- [x] 在既有 `enterprise-catalog-publishing` Online 浏览器用例中补充卡片默认收拢、展开后 Standard / Quality 各自按当前 Tenant 授权呈现、再次收拢的断言；无需新增 suite、权限或 CI 登记。此用例仍须在专用 T4 环境实际执行，且当前专用账号若无 Model / Quality 导航权限，不能据此声称这些跨模块跳转已完成验收。
- [ ] 在专用真实 Tenant 验收展开、切域、Model/Quality 导航与 Standard/Quality 权限缺失或暂不可达状态；现有单测、构建和 PostgreSQL 门禁不等于真实浏览器验收。

2026-09-28 本机只读交互核对：运行中的户外域目录卡片已能展示 Standard 术语、数据元、指标，Model 专业入口及 Quality 规则、方案、`open` 问题；点击一条指标定义后进入 Standard 对应指标详情。当前 Tenant 仍只有一个可用业务域；屏幕随后锁定，未继续核对切域、Model / Quality 跳转和故障降级。因此以上观察仅是局部运行态证据，不勾选完整验收项。

同日门禁：`make test-online-runner` 的确定性测试通过（238 项），`make test-console-frontend` 串行重跑通过（单测 132 项、浏览器回归 81 项及构建）。首次与 Online Runner 并行执行时，Console 的一条既有 Transfer 权限浏览器用例在 5 秒内未匹配到 iframe；失败快照中已有 iframe，串行重跑未复现，暂不修改非本轮所有的 Transfer 用例。新增断言所在的完整 T4 Online suite 未在本机执行，仍需专用隔离部署验收。

## 二十六、2026-09-30 责任、业务授权决定与实际数据访问贯通设计

### 26.1 本轮范围和决策状态

用户认可总体方向，并已确认下述 D1-D3 业务边界。此次推进正式规范同步、实现缺口盘点和现有专题跟进，不新增权限、角色、审批实体、数据库表或 API，不修改运行中的授权，不重启服务。正式目标规则已同步到授权上下文规范 5.5、术语表、账号与权限体系、引擎体系、模块架构、Catalog 实现规范及 Catalog／System CLAUDE；业务边界确认不代表表级授权代码已经实施。

已认可的方向：

1. Catalog 继续唯一维护条目的责任部门、业务责任人、数据管理员和技术维护者；三类责任账号不是三个全平台 IAM Role。业务责任人与数据管理员承担业务治理工作，技术维护者不是编目提交人，项目组也不是长期责任部门的替代物。
2. 功能权限与目标资源访问授权必须在同一资源和动作上下文内同时满足，并继续检查租户、主体、有效期、资源状态、安全保护和 Explicit Deny；Console 隐藏入口不能替代后端、Runtime 或 Worker 的检查。
3. 条目信息可见、承担责任、实际数据访问是三个独立事实。责任部门成员、责任账号、业务域成员或协作集合成员不因此自动获得实际数据读取、写入、下载或执行权限。
4. 未分配 Catalog 责任不等于数据公开，也不阻止已有显式授权成立。没有授权默认拒绝；教学数据的租户共享也必须是显式、可撤销的授权，而不是关闭权限检查。
5. 同一物理表、文件或对象的同一种数据动作只能有一个授权事实源；Manager、Develop、Quality、Transfer 等访问执行点不能分别维护各自的源数据 ACL。
6. 已确认将物理源数据的授权事实归入 System 的引擎访问控制领域，作为其职责的显式扩展；不是把所有业务资源 ACL 放进 System IAM。模型、质量方案、服务和数据应用等专业对象的授权仍归其所属模块。正式职责与业务规则已固化，精确资源身份、接口及决定证据仍待定义，不得标记为已实现。
7. Catalog 可以提供查看、申请和办理授权的业务入口，但不能成为基础数据授权的唯一入口或运行前提。System 引擎管理需要独立可用的正式入口，两个入口使用同一授权写路径，不保存授权副本。
8. D1 已确认：尚未确定业务责任时，显式指定且具备对应管理范围的接入授权主体，可以向指定账号或项目组授予明确选中资源的限时只读权限；不包含全租户公开、写入或 DDL。业务责任明确后，新增共享及扩大权限转由业务责任人确认。限时不等于默认无限期，具体期限数值尚未确定。
9. D2 已确认：普通只读共享允许同一账号兼任业务确认与授权办理，但两方面的资格必须分别满足并分别留痕；不普遍强制两人，不因此开放申请人自批或放宽既有敏感动作的复核规则。
10. D3 已确认：业务责任移交立即终止旧责任的新确认资格，不一律撤销既有有效 Grant；新责任人复核共享的合理性，必要时由有办理资格的主体撤销。尚未履约的决定重新核验当前责任，已有规则仍按自身范围、期限、当前主体、撤销及 Deny 判断。

“资源授权权威”在本专题中只表示资源访问规则的唯一维护方，不表示业务负责人、IAM Role、页面入口或实际读取数据的模块。已经存在的目录可见性和责任治理队列不构成表级数据授权的实现证据。

### 26.2 事实归属与执行位置

| 事实或行为 | 归属／执行位置 | 边界和状态 |
| --- | --- | --- |
| User、Department、Project Group、成员关系、功能 Permission 和 Role Assignment | System IAM | 继续复用已有身份体系，不将人员姓名或部门名编码进新角色 |
| CatalogEntry 的责任关系、语义关联、条目信息可见范围 | Catalog | 已有事实源；本轮不把它们直接变成内容授权 |
| Engine Instance、实时引擎资源发现 | System 引擎领域 | 已有能力；实时发现不要求 Meta Item 或 CatalogEntry 先存在 |
| Meta DataItem、结构、扫描任务和扫描结果 | Meta | 已有能力；扫描与建档不授予普通用户内容读取权 |
| 物理源数据资源的 Allow、Explicit Deny、期限、来源及撤销 | 已确认由 System 引擎访问控制领域维护，尚未实施 | 正式职责已同步，精确契约待定义；不复制 Meta attributes、Catalog 责任全表或其他专业模块 ACL |
| 业务确认人的资格和一次授权的业务决定 | 资格引用 Catalog 当前责任与显式功能权限；决定的记录位置和消费契约待确认 | 不预建通用审批引擎，也不把一段理由文字或请求方自报负责人当作有效决定 |
| 数据预览、SQL、质量检查、导出及后台执行的访问检查 | 对应访问模块与 Runtime | 消费同一源数据授权契约；共同规则放 common，common 不成为另一份授权事实源 |
| 敏感识别、分类分级与保护效果 | Security；访问模块执行保护 | 保护或原值例外不替代源数据访问授权 |
| 已发布资产的申请、审批与跨 owner 履约 | Asset | 复用已有流程与 owner 规则；审批通过与最终授权生效不能混同 |

源数据授权目标已确认按 Tenant 内登记的 Engine 身份和完整结构化 EngineCatalogPath 表达逻辑资源，不强制依赖 CatalogEntry UUID 或 Meta Item ID，也不新增第二份资源树。用户未在 ADDP 改变绑定或规则时，外部同名重建仍按同一逻辑资源处理；不同路径或不同 Engine 不自动接收原规则。SQL 中的资源定位与授权目标必须使用同一规范，不能通过字符串拆分或正则猜测；指纹相同不是当前访问许可，仍须检查主体、动作、有效期、撤销与 Deny。

### 26.3 责任—业务决定—授权办理建议矩阵

以下为已确认的职责分工，不是当前权限的说明，也不以责任身份自动签发 IAM Role 或 Grant。明确业务责任后，新共享及扩大权限需要业务责任人确认；普通只读共享允许同一账号在分别满足确认与办理资格时兼任。

| 身份／能力 | 业务职责 | 授权过程中的建议作用 | 明确不自动取得 |
| --- | --- | --- | --- |
| 责任部门 | 对资源承担稳定组织责任、协调责任移交 | 提供责任范围和组织协调依据 | 全部门读取、写入权限；全部成员的业务确认资格 |
| 业务责任人 | 确认含义、用途与业务共享边界 | 对新共享或权限扩大作出业务确认；仍要求明确功能权限和当前有效责任范围 | 任意授权管理权、数据写入权、引擎凭据 |
| 数据管理员 | 编目维护、明确数据范围、协调标准和质量问题 | 整理申请、协助界定对象／动作／主体／期限；有办理权限时落实有效决定 | 默认业务批准权、SQL 执行权或全库访问权 |
| 技术维护者 | 维护结构、加工链路、接口和运行条件 | 校验技术可执行性、处理故障 | 业务批准权、DDL、写入或敏感原值访问权 |
| 具备授权办理功能与资源管理范围的主体 | 将有效授权依据落实为规则，执行撤销和到期处理 | 在唯一授权维护方创建、修改或撤销规则 | 凭办理权限独立批准任意共享，或绕过资源范围与业务确认 |
| 资源使用者 | 按允许用途使用数据 | 在对应功能和资源动作均允许时访问 | 因看见条目、加入集合或承担责任而越权 |

同人兼任不等于申请人自批，也不放宽既有敏感动作、Security 原值例外或平台三员分立规则。具体确认资格必须基于修改前的当前权威事实，禁止在同一请求中先把自己设为负责人，再据此完成提权、批准或扩大共享。

Catalog 内的盘点读取、普通编目、责任管理和条目信息共享也需要分别明确操作边界。前面已经认可的权限拆分方向不等同于已完成拆分：当前批量分配仍使用 `catalog.inventory.read + catalog.entry.update`；不能把此入口视为源数据授权入口。

### 26.4 首次接入：发现、扫描、访问和授权维护分开

| 操作 | 不要求先存在的对象 | 建议授权条件 | 尚待完善的边界 |
| --- | --- | --- | --- |
| 登记引擎连接 | Meta Item、CatalogEntry | 引擎登记功能和明确租户范围 | 登记连接不自动授予数据访问或授权维护权 |
| 实时列出 namespace、表、目录或对象 | Meta 扫描结果 | 实时发现功能及对应引擎资源范围 | 名称与结构也可能敏感；底层连接权限仅为平台可达上限 |
| 发起 Meta 扫描 | Catalog 责任或条目 | 扫描功能和明确扫描范围 | 发起人不因发起扫描取得内容访问权 |
| Meta Worker 执行扫描 | Catalog 可用性 | 有效任务授权依据、服务身份及必要的执行范围 | 服务身份不扩张任务范围；结构扫描与需读文件头／样本的扫描分别核验访问能力 |
| 办理源数据访问授权 | Meta Item、CatalogEntry | 授权办理功能、目标管理范围及有效业务／接入授权依据 | D1 已限定接入主体只能向指定账号／项目组授予选定资源的限时只读；第一份管理授权的具体委派契约仍需设计，不能把登记引擎自动视为授权依据 |
| 预览、SQL、检查或导出 | 完成业务编目 | 相应模块功能、实际涉及资源的动作授权和安全条件 | 单有整库连接凭据或引擎级 read 效果不足以证明表级限制 |

当前已有 System 实时发现和 Console 调用 Meta 的扫描编排，可复用，不另建扫描流程。“初始授权管理员”不是新的 Catalog 责任或永久实体，只是对某引擎资源授予授权维护能力的表达；首次授予由有权主体显式办理，不能按 `created_by` 或 `tenant_admin` 自动放行。计划扫描的长期授权来源、用户变化及到期检查与现有执行授权体系一起设计，不持久化原始 User Token。

“实时发现 → 选择资源”与“Meta 扫描 → 保存技术事实”不形成授权循环。缺少实时发现能力的引擎暂不承诺扫描前叶子选择，也不能让用户自由填表名作为未经校验的授权目标；应标记该能力缺口，单独确认纳管条件。

### 26.5 未编目、Catalog 不可用与责任变更

已确认业务边界如下；具体委派、决定消费、身份及执行契约仍须先定义再实施：

- 未分配业务责任：数据不默认公开；已经显式授予且仍有效的规则可以使用。按已确认 D1，显式接入授权主体只能在其管理范围内，向指定账号／项目组授予明确选中资源的限时只读；不借“暂时没有负责人”扩大到租户公开、写入或 DDL。
- 未部署 Catalog：System 实时发现、源数据授权入口和 Meta 扫描不得以 Catalog 为启动前提；Catalog 自有业务确认不能伪装存在。无业务责任时仍使用 D1 的有限接入授权边界，而不是默认全租户可用；超出该边界的请求不能在实现中自行放行。
- 已部署 Catalog 暂时不可用：不把不可用当成“无负责人”，不自动切换到接入授权主体。已有有效 Grant 的访问与业务模块 Ready 不因 Catalog 故障受影响；需要当前责任事实的新业务确认不能使用过期缓存默许，应只阻断这项行政办理，恢复后重新核验。
- 正式明确业务责任：不建立 System 可编辑的业务责任副本，不产生两套互相独立的业务负责人。D1 已确认新增共享和扩大权限转由业务责任人确认；原接入主体即使保留技术办理权限，也不能继续独立决定新共享。办理权限的委派／收回契约仍待设计；D3 已确认不因责任移交一律撤销既有有效访问规则。
- 业务责任人更换或失效：旧责任不能继续支持新的业务确认；尚未履约的决定必须复核当前责任资格与相关版本。已有访问 Grant 继续按原范围、期限和有效主体执行，不自动扩大或续期；新责任人复核其合理性，需要撤销时由有办理资格的主体落实。
- User、Department 或 Project Group 的成员资格变化：按当前身份和成员关系判断实际访问，不把授权时的成员列表永久固化为访问名单。主体失效、规则撤销、到期或 Explicit Deny 都必须通过统一契约在访问点生效。
- Catalog 来源重绑：目录身份连续不等于源数据授权连续。新物理目标必须重新精确确认，不把旧表授权自动迁移到另一张表。

### 26.6 已确认决策与尚未定义的实施契约

| 决策 | 已确认边界／推荐方向 | 剩余事项 |
| --- | --- | --- |
| D1：首次授权与无 Catalog 的业务决定资格（已确认业务边界，未实施） | 无业务责任时，显式接入授权主体在被授予的管理范围内，向指定账号／项目组授予选定资源的限时只读；不含租户公开、写入或 DDL。明确业务责任后，新共享及扩大权限须经业务责任人确认 | 第一份管理授权的委派契约、期限默认值／上限及责任确立后的办理资格校验仍需具体设计；不能以此状态表示已完成数据授权 |
| D2：业务确认与授权办理的分工（已确认，未实施） | 普通只读共享允许同一账号兼任确认和办理，但必须分别具备当前业务确认资格、办理功能及目标管理范围，不普遍强制双人办理 | 决定证据如何绑定精确对象／主体／动作／期限及当前责任。申请人自批不在本次批准范围内，未明确前不据此开放；既有敏感动作复核规则继续执行，不预建通用审批流程 |
| D3：责任变化对已有授权的影响（已确认，未实施） | 原责任人立即失去基于该责任的新确认资格；不一律撤销既有有效 Grant，新责任人复核共享并由有办理资格的主体落实必要撤销 | 尚未履约决定的重确认与版本核验契约，以及责任变更和 Grant 写入之间的并发边界；移交不改变既有规则的期限、范围或当前主体检查 |

D1-D3 业务边界已确认，并已固化到正式规范。剩余工作是精确实施契约，不再把已确认业务规则列为待讨论。仍有会改变业务结果、权限范围或模块边界的歧义时，暂停依赖它的代码或迁移；其他独立盘点与验证继续。不能把尚未解决的身份或执行问题隐含到“默认公开”“注册者自动拥有”或“管理员兜底”中。

已确认的具体效果（尚未实施）：

- D2：普通只读共享不普遍强制两人办理。业务责任人同时具备授权办理功能和目标管理范围时，可以确认后自行办理；缺少办理资格时，由有资格的办理人落实其有效决定。两步的资格、决定与履约结果仍分别核验和留痕，不能因为同人兼任而合并成一次无条件 Allow。敏感动作和申请人自批另行明确，在未明确前不据此开放。
- D3：只更换业务责任人，不一律撤销原有有效访问规则；新责任人接手后复核其合理性，需要撤销时由具备办理功能与目标管理范围的主体落实，新责任人不因接任自动取得办理权限。未撤销且未到期的授权继续按原范围执行，不能新增或扩大。旧责任人丧失的是基于旧责任的新业务确认资格，不当然意味着其作为使用者的独立 Grant 失效；账号失效、成员退出、到期、撤销或 Deny 则仍立即按各自事实限制访问。尚未履约的决定重新核验当前责任，不继续使用旧责任资格；具体复核／重新确认条件在契约中明确。

例如：甲确认给项目组乙读取表 C，办理人落实后，业务责任人由甲换为丙。按已确认 D3，乙的既有 C 只读规则不会仅因换负责人就消失；丙可以复核并要求有办理资格的主体撤销，甲不能再凭旧责任确认扩大乙的范围。这是已确认、尚未实施的业务效果，不是现有系统行为。

### 26.7 实施顺序、验证范围与接力清单

1. 本节矩阵与 D1-D3 已确认，术语表、账号与权限体系、授权上下文、引擎体系、模块架构、Catalog 规范和相关模块 CLAUDE 已同步；后续契约调整继续先改正式规范，不以专题草案取代正式规范。
2. 源数据逻辑目标已明确；继续定义发现／扫描／内容动作、授权维护范围、决定证据、到期撤销及执行期校验的唯一契约，复用已有 ResourceAccessRule 的规则语义、AuthContext 和 Execution Authorization；不新增全平台中央 ACL、另一套角色或双写来源，不追踪物理创建实例。
3. 以一个 PostgreSQL 引擎、明确的表 C/D、两个账号、部门和项目组完成首条纵向闭环：接入、发现、扫描授权、业务确认、实际 Grant、Manager 预览与 SQL 执行。禁止给 Notebook 用户代码整库宽权限凭据后仅依赖入口检查；无法限制实际资源范围时不开放该路径。
4. 迁移前只读盘点当前授权需求，经确认建立必要显式规则再统一切换；当前租户级可达性不能自动回填为全租户读取 Grant，不保留永久兼容放行。不清空开发数据库、不手工清除迁移 dirty 状态。
5. 再扩展 Quality、Transfer、其他引擎及 Catalog 统一办理入口。Asset 沿既有申请／审批／owner 履约路线接入，不因本节另建第二套发布授权。

实施门禁须同次纳入现有根 Makefile、模块自动发现和 CI：System／Common 授权与执行变更覆盖 T0/T1、System IAM PostgreSQL T2；Catalog、Manager、Develop 等实际消费者按影响扩散运行模块 T0-T3；扫描范围变化补 Meta 集成；真实跨模块闭环归 T4。在已有 suite 确认能够承载后再补断言，不能登记空壳 suite，也不能把其他 macOS 的未来执行算成本机通过。

最低验收案例：

- 未扫描、未编目时可以在允许的引擎范围实时发现和选择 C；没有发现权限时不泄露名称、结构及分面数量。
- 接入主体只能在显式管理范围内向选定账号／项目组授予 C 的限时只读；未选中的 D、无限期、租户公开、写入及 DDL 不因接入资格被允许。明确业务责任后，该主体不能绕过业务确认新增共享或扩大范围。
- 有功能权限无 C 的数据授权、有 C 授权无对应功能权限，均被后端拒绝；未授权资源默认拒绝。
- 同一份 C 读取规则在 Manager 与 SQL 路径一致；查询 C/D、读 C 写 D 分别检查完整对象集合及动作，不靠所选引擎或正则代替。
- 部门、个人和项目组授权按当前成员关系生效；加入 Catalog 集合只影响按已确认规则共享的条目信息，不授予实际数据访问。
- 扫描任务范围与实际 Worker 访问一致；服务账号不能扩展用户或任务授权，内容采样不能借结构发现权限绕过。
- Catalog 未部署、暂不可用、责任未分配和责任变更分别验证；不可用不能降级为无责任；审批或办理所需事实无法确认时拒绝新操作，不阻断已有合法数据操作或模块启动。
- 修改自己责任后自批授权、伪造确认主体／版本／资源、重放过期决定，以及规则到期／撤销／Deny 均有拒绝证据。
- 外部同名重建保持同一逻辑目标，并按当前仍有效的规则判断；不同 Engine、不同路径及 Catalog 重绑不自动转移原目标规则。运行凭据不能突破执行资源范围，接入不能触发未授权的源端写入。
- 业务决定与实际履约分开记录；未经授权维护方持久化不得显示已生效，历史审批或页面可见不能成为当前 Allow。
- 同人确认与办理在两方面资格均满足时成功；缺少当前确认资格、办理功能或目标管理范围任一项均拒绝，不能借同人规则开放申请人自批。
- 责任移交后，已有有效 C 只读规则保持原范围；旧责任不能作出新确认或履约未完成的旧决定，新责任人也不自动取得办理权。撤销、到期、成员退出或 Deny 的独立限制仍正常生效。

本轮跟进：

- [x] 核对正式责任／可见性规范、System 实时资源发现、现有扫描编排及 Catalog 功能门禁，明确责任、决定、授权规则和访问执行不是同一事实。
- [x] 建立建议职责矩阵、首次接入与故障边界、关键决策和统一测试计划；保留建议／待确认标识，不伪装为已实现。
- [x] 确认 D1 的业务边界：显式接入主体向指定账号／项目组授予选定资源的限时只读；明确责任后，新共享及扩大权限转由业务责任人确认。尚未实施，不含具体期限或自动委派规则。
- [x] 确认 D2、D3：普通只读共享可由分别具备两方面资格的同一账号确认与办理；责任移交不一律撤销既有有效 Grant，不开放申请人自批。
- [x] 将职责与业务边界同步到正式概念、授权上下文规范 5.5、Catalog 实现规范和模块说明；所有新增描述明确标记为目标规则，尚未实施表级贯通。
- [x] 明确逻辑资源目标：Tenant 内 Engine 与完整 EngineCatalogPath；外部同名重建不自动改变授权；撤回物理创建实例及源端配合前置要求，正式规范和测试语义同步。
- [ ] 定义初始管理委派、当前责任与决定核验、执行期范围及唯一授权写入契约。
- [ ] 实施和验证 PostgreSQL C/D 首条闭环，再按真实消费者扩散。

前一轮验证记录：专题文件的 `git diff --check` 通过。已尝试根入口 `make test-changed`；共享工作区影响计算选中 26 个注册模块，门禁在执行测试前因缺少 PostgreSQL 测试 DSN、MySQL／OceanBase 测试条件等 T2 环境而失败退出，不能计为测试通过。正式规范同步后的验证结果另记；不为文档变更配置其他模块凭据、修改它们的测试或启动服务。新的数据授权 T1-T4 尚无功能验收证据。

2026-09-30 正式规范同步验证：本轮 9 个文档的 `git diff --check` 通过；`make test-authorization` 退出 0，共享授权单测、Manifest／生成常量／Tool Catalog／SQL 种子一致性与全部 owner Swagger 路由覆盖通过，只证明既有授权声明的一致性，不证明新表级授权已实现。再次运行 `make test-changed`，当前工作区 62 个变更文件命中 26 个注册模块；因缺少 PostgreSQL、MySQL 和 OceanBase 的必需 T2 条件，在测试执行前退出 2，不能计为全量通过。当前无新 API、Permission 或测试入口，不新增空壳 CI 登记；后续实现须由现有 `test-system-iam-postgres`、`test-common-postgres`、`test-catalog-postgres`、`test-manager-postgres`、`test-develop-postgres` 等 T2 门禁及真实消费者 T3/T4 验证。

### 26.8 首条闭环可复用能力与阻止直接实施的缺口

以下来自 2026-09-30 只读核对，不是新增实现，也不以已有安全保护或引擎级授权冒充源数据表级授权。

| 已有能力与事实位置 | 首条闭环可复用部分 | 仍缺的契约／证明 |
| --- | --- | --- |
| System 实时 Engine Catalog；`common/engine/plugin/providers.go` 的 EngineCatalogPath | 未扫描时发现和选择资源；复用唯一结构化路径表达逻辑授权目标，不另建资源树 | 须补 Tenant、主体、动作、有效规则及执行范围检查；不要求物理创建实例。不同 Engine 或路径不自动转移授权 |
| System `ExecutionEngineAccessScope` | 当前执行身份、租户、租约、引擎及 read/write/ddl 效果检查 | 目前只有 EngineID 和 Effects，没有表 C/D 范围；必须把精确目标、当前规则及执行限制贯通，不能只增加一张 Grant 表 |
| Common PreparedQuery／QueryReadSet；PostgreSQL `query_read_set.go` | 一次性冻结查询请求、原生 AST、当前目录解析、JOIN／CTE／子查询与视图依赖闭包；不能证明时 unresolved | 须让实际执行读取集合和获准逻辑路径一致，防止 C 查询借依赖读取未授权 D；解析与执行的不同连接仍需处理，但不增加 OID 连续性检查 |
| PostgreSQL `analytical_execution.go` 与共享 SQL 执行主路 | 已有分析查询在执行事务内锁定来源并核验绑定结构，可评估复用其原生执行边界，不再创建第二个 Execute 接口 | 该校验目前只服务编译分析查询、有限普通表及绑定字段；不是普通 SQL 的通用资源门禁，也不验证源数据 Grant。后续只为实际读取范围一致性服务，不扩展成物理实例追踪 |
| Workbench owner 本地 ResourceAccessRule；Common 授权与 AuthContext 能力 | Allow／Deny、期限、来源、撤销及当前身份语义已有实现参考 | Workbench 的应用对象规则不能直接充当物理源数据事实；规则仍须由已确认 System 引擎访问控制领域维护，不向每个消费者复制 ACL |
| Catalog 当前责任关系与来源身份 | 核验业务责任人的当前资格并提供业务入口 | 授权决定还需绑定精确目标、接收主体、动作、期限和责任依据；职责变更与办理并发、重放和幂等不能靠请求方自报身份解决 |

接下来的顺序是先收敛“精确逻辑资源 → 业务确认／接入依据 → 唯一规则 → 实际执行”的契约，再写实现与测试。不重新实现 SQL 解析、不新增独立审批引擎、不增加物理创建实例、不修改生产凭据或替用户扩大授权。

2026-09-30 最新已确认边界：保留现有 DataItem 指纹算法；Catalog 不新增来源表改名功能，业务名称编辑不修改物理名称。授权针对 ADDP 中登记的逻辑资源：用户未在 ADDP 改变绑定或规则时，外部删除后同名重建不自动改变原授权。不同 Engine、路径和 Catalog 来源重绑不自动转移规则。撤回此前“重建必须重新授权”和物理对象连续性证明的实施目标；不再将 OID、创建实例或源端配合作为授权前提。接入本身不允许任何未经用户明确授权的非只读操作。

本轮实施范围：同步上述正式概念与规范，删除本专题未采用的源端配合草案，并将既有 Common PostgreSQL T2 案例收敛为逻辑定位验证。删除跨操作 OID 相等／不等的断言，不新增身份注册库、源端触发器、授权迁移或 API，不修改指纹算法。这些验证只证明定位路径与指纹行为，尚不表示表级 Grant 及真实消费者闭环已实现。

2026-09-30 身份证据验证已完成：在本地允许的 `addp_test` 中，既有 PostgreSQL 原生目录解析器与 PreparedQuery 实际验证了以下边界。临时表使用唯一名称，只清理本轮拥有的两种表名，并核验零残留。

| 真实操作 | DataItem 指纹 | 当前 QueryReadSet 路径 | 已确认的逻辑资源处理 |
| --- | --- | --- | --- |
| 外部改名 | 改变 | 改变，旧名不再解析 | 不同路径，不自动跟随授权 |
| 删除后按原名重建 | 与原值相同 | 与原集合相同 | 仍按同一逻辑目标和当前有效规则处理，不自动延长、扩大或恢复已撤销的授权 |

测试位于 `common/engine/plugins/postgresql/query_read_set_integration_test.go` 的 `TestIntegrationResolvePostgresQueryReadSetKeepsLogicalTargetAcrossSameNameRecreation`，由既有 Common PostgreSQL 默认 T2 与 CI 正则自动覆盖。既有脚本增加 `--test query-read-set` 明确分组，供最小范围验证；默认入口与 CI 仍执行全组，不增加任意正则覆盖或放宽 Skip 检查。脚本分组的默认全组、限定范围、未知参数拒绝与 Skip 拒绝，由现有 `scripts/test/module-gate_test.py` 覆盖，已验证先失败后通过。

此前定位案例与脚本验证记录（OID 断言已在本轮删除；调整后结果另记，不把旧结果当作新测试通过）：

- `git diff --check` 与脚本语法检查通过。
- `make test-platform` 退出 0，平台一致性、测试入口及 CI 登记检查通过；其中脚本分组测试、共享授权声明和 owner Swagger 路由覆盖也通过，不表示新表级授权已实现。
- `make test-go` 退出 0，所有已跟踪 Go 模块依赖一致性与 T1 测试通过；普通 Go 入口不替代真实数据库 T2。
- `python3 scripts/test/module-gate_test.py` 退出 0，18 个测试通过。
- 在核实当前 Infra 实际端口、容器归属后，向 `bash scripts/test/common-postgres-gate.sh --test query-read-set` 显式注入测试连接条件，退出 0，8 个真实 PostgreSQL 用例通过，无 Skip，包括上述身份案例与清理断言。
- 默认 `make test-common-postgres` 未通过：已有数值计算矩阵执行期间，本地 PostgreSQL 日志记录一个后端进程退出码 2，随后进入自动恢复；当时新身份案例尚未运行。数据库后来自行恢复可连接，已停止本轮全组测试进程，未重启或重置数据库。异常退出的具体根因仍未查明，不能归因于身份案例，也不能把分组通过计作完整门禁通过；execution store、保护投影存储及结果桥接的后续 T2 未验证。完整门禁仍由既有 CI `common-postgres` 作业覆盖，但此次本地异常须继续调查，不能靠增加超时或跳过用例掩盖。
- `make test-changed` 在执行测试前因缺少各 owner PostgreSQL DSN 及 MySQL／OceanBase 条件退出 2，未计为通过；本轮不扩大其他模块凭据或启动服务。

后续回到功能权限与逻辑资源授权的交集：贯通 PostgreSQL C/D 的真实授权，不再为创建实例、恢复或标识复用建设追踪机制。Catalog 业务名称与来源绑定历史仍不承担数据授权事实。

首条验证建议限定一个 PostgreSQL 引擎的显式表 C/D：先覆盖实时选择、明确限时只读 Grant、Manager 预览与受控只读 SQL；必须拒绝未授权 D 及无法证明完整读取集合的查询。写入、DDL、任意 Notebook 用户代码和其他消费者没有完成相应执行约束前，不计入通过范围，也不得新增默认放行。逻辑目标已经确认；初始管理委派及决定履约契约仍会影响权限结果，在确认前不实施依赖它们的数据库迁移。

### 26.9 已确认的逻辑资源授权边界与后续闭环

2026-09-30 用户明确：接入数据库不授权 ADDP 执行任何非只读操作；源库外部同名重建、且用户未在 ADDP 进行相应绑定或授权变更时，可以按同一逻辑资源处理。此前本节的物理创建实例与源端配合草案已撤回并删除，不作为后续实施前提。正式规则已同步到术语表和授权上下文规范 5.5；运行层表级贯通仍未实现。

#### 授权目标及变化处理

授权目标是当前 Tenant 内“已登记 Engine ＋完整结构化 EngineCatalogPath”的逻辑资源。路径来自引擎规范及实际发现，不新增资源树、物理实例注册库或指纹算法。Catalog 身份、Meta 行 ID 和指纹不成为另一套访问规则。

| 情况 | 逻辑资源与授权处理 |
| --- | --- |
| 原表内容更新或源库外部同名重建，ADDP 内绑定与授权未变 | 仍按同一逻辑目标；当前规则、主体、动作、有效期、撤销、Deny 和安全条件继续核验 |
| 表暂时不存在 | 访问失败，保留逻辑目标与规则；重新出现后不扩大范围、不续期、不恢复已撤销规则 |
| 外部改名或移动到不同路径 | 不自动把原规则移到新路径，也不自动跟随原生 OID |
| ADDP 显式改接不同 Engine／路径，或 Catalog 重绑到不同来源 | 校验新目标，原规则不自动迁移；目录身份和业务历史可以按既有 Catalog 规则延续 |
| 同一实际引擎经明确确认搬迁并保留 Engine ID | 沿现有引擎更新规范处理，不增加创建实例追踪 |
| 仅编辑 Catalog 业务名称／说明 | 不改源表、不改变实际数据访问范围 |
| 登记、发现、扫描或只读访问 | 不安装源端触发器、不建源端角色、不写身份记录，不借连接凭据执行未明确授权的非只读操作 |

#### 下一条纵向闭环

实际允许访问须同时满足：功能权限、精确逻辑资源动作规则、当前主体／组织关系、有效期及安全条件。责任身份或加入协作集合不能代替实际 Grant。

1. System 引擎访问控制领域唯一保存、办理和判断源数据规则；Catalog 提供业务责任与确认入口，但不保存 Grant 副本，不成为数据操作的启动依赖。
2. 首轮目标为一个 PostgreSQL 引擎的明确表 C/D，访问路径限定 Manager 预览与受控只读 SQL。规则含明确主体、动作、期限及授权依据；范围不从现有引擎级 read 效果推导。
3. 复用 PreparedQuery／QueryReadSet，对完整读取集合逐项核验。给 C 的授权不能允许 `JOIN D`、视图依赖 D 或其他未授权路径；无法证明完整读取范围时拒绝该查询。解析与执行条件须保持一致，不为此追踪 C 的物理创建历史。
4. 默认拒绝、Deny 优先、到期与撤销生效；部门／项目组按当前有效成员关系匹配。目录责任、角色功能权限和资源 Grant 分别核验，不因承担责任自动赋予数据读取或办理权。
5. 用既有 owner T2 和真实消费者 T4 证明上述链路，再扩展 Quality／Transfer。Notebook 任意代码、写入及 DDL 不因只读闭环完成而自动开放。

2026-09-30 当前实现复核：`system/backend/internal/iam/execution_authorization_service.go` 与 `common/client/system_execution_authorization.go` 的 ExecutionEngineAccessScope 仍只有 EngineID 和 Effects；System 尚无表级源数据规则模型及办理 API。Workbench 已有 ResourceAccessRule，但它的 ResourceID、Permission 和数据库表专用于 Data Application，不能复制成源数据授权或直接拿来授予表权限。可复用的是 Allow／Deny、到期、撤销与履约语义，不是其应用对象模型。当前未新增 System 表、API 或迁移。

剩余契约集中在初始管理资格的显式委派、责任确认与办理核验、唯一规则写入及执行消费；不再以“永久物理身份证明”阻塞这些工作。第一份管理委派不能自动授予引擎登记人或凭角色名称放行；具体授权办理 Permission、管理范围与期限仍须先收敛再实施。

#### 最低验收案例

- 有预览功能权限但无 C 读取规则，拒绝；有 C 规则但无预览功能权限，同样拒绝。
- 有 C 只读规则时，C 查询允许，D 查询及 C/D JOIN 拒绝；不同 Engine、schema 或其他完整路径下的同名表不匹配 C。
- 部门／项目组授权按当前成员生效，退出成员、到期、撤销与 Deny 不被个人目录或协作集合绕过。
- 外部同名重建继续按同一逻辑目标与当前有效规则处理；显式改绑不同目标不转移旧规则。
- Catalog 不可用不阻断已有有效规则的数据访问；需要当前责任确认的新办理不能降级为无责任自动放行。
- 登记和只读链路不触发未授权源端写入。定位案例里的 DDL 仅在标准 T2 已核验的 disposable 测试库及本轮唯一夹具上执行，不对已接入业务源库执行。

2026-09-30 本轮调整后验证：

- `git diff --check`、`bash -n scripts/test/common-postgres-gate.sh` 通过；`python3 scripts/test/module-gate_test.py` 退出 0，18 项通过。
- `make test-go` 退出 0，全部已跟踪 Go 模块的依赖检查和 T1 通过；`make test-platform` 退出 0，平台一致性、测试／CI 登记、授权声明及 Swagger 覆盖通过。二者不证明新表级授权已实现，也不替代 T4。
- 先运行 `bash scripts/infra/status.sh`，核实当前工作区 `addp-postgres` 的实际映射为 `25432`；只向标准入口注入当前测试连接条件，不输出密钥，不采用 `.env` 首选端口。`bash scripts/test/common-postgres-gate.sh --test query-read-set` 首次因新定位案例读取目录超时退出 1；同轮 Docker 只读诊断也明显延迟，取得的数据库活动快照没有阻塞者，尚无足够证据确认具体根因。未增加超时或修改实现，原样重跑退出 0，8 项通过，无 Skip，新定位案例完成后清理断言通过。
- `make test-changed` 计算共享工作区 40 个变更文件、26 个注册模块，在测试执行前因缺少 owner PostgreSQL DSN 和 MySQL／OceanBase 等必需 T2 条件退出 2，不能计为全量通过。共享工作区另有前端镜像构建及 IAM 页面并行变更，不属于本轮修改。
- 本轮没有运行完整 `make test-common-postgres` 或真实 C/D 授权 T4；完整 Common PostgreSQL 仍由既有 CI `common-postgres` 作业调用标准入口覆盖。更名后的定位案例命中原有正则，不新增测试路线、外部依赖或空壳 CI 登记。未重启 ADDP 服务，未修改业务源库或运行中的权限。

### 26.10 第一份管理委派（后端已落地，源读取消费者待贯通）

2026-09-30 继续核对现有 Permission Manifest、内置 Role 和 Role Assignment 规范：System 已有引擎登记／发现和租户角色分配能力，但没有源数据授权管理委派 Permission 或引擎资源管理范围事实。现有 Role Assignment Scope 只有 platform、tenant、department、project_group；Role 只组合功能 Permission，不能把 Engine ID 或表路径塞进 Role，冒充资源管理范围。`tenant.security_manager` 是 Security 业务治理角色，也不是源数据授权管理的隐含权威。

已确认采用下面这条最小路线，不为首份委派再建一套组织或责任体系：

1. **谁有资格委派**：设置独立、精确的 Tenant 功能 Permission `system.engine_access_delegation.create/read/revoke`，通过现有 IAM 自定义 Role／Role Assignment 显式授予账号。创建、撤销为 high 风险，读取为 low；不分配给内置 Role，不按管理员名称、引擎登记人或 Catalog 责任自动放行，也不自动给现有账号回填。
2. **委派什么**：有上述资格的账号在 System 中，显式选择同 Tenant 的 Engine 与有效用户成员。这是一份引擎级、可到期、可撤销的管理资格，不是对源数据的读取 Grant。该范围为后续扫描前实时发现和选取叶子提供管理依据，但不表示本轮已接入扫描出口，也不能因此向普通使用者授予整库读取；后续只读 Grant 仍逐项选择逻辑资源。首版不实现 namespace、通配符或递归继承，必须填写有限到期时间且不得超过成员关系期限，不设默认期限或额外期限上限。
3. **管理账号后续能做什么**：首次源读取授权贯通后，仅在被委派范围内，向指定账号／项目组办理明确资源的限时只读；不自动获得内容读取、写入、DDL 或向他人转委派管理资格。已经明确业务责任的目标仍要求当前业务确认，不能继续独立批准共享。本轮不包含这些实际读取规则。
4. **三类责任如何参与**：Catalog 的业务责任人负责已确认的业务共享决定；数据管理员协助界定范围、维护编目；技术维护者维护技术链路。谁实际办理规则，仍独立检查功能 Permission 与 System 当前管理委派，不能用责任身份替代。Catalog 不新增第四类“授权管理员”责任，不保存管理委派副本。
5. **唯一办理路径**：System 保存管理委派、实际读取规则和履约证据；Catalog 后续仅组合现有责任与同一办理能力。先贯通 System → Manager 预览 → 受控 SQL 的 C/D 闭环，不先扩大至全部数据出口或把尚未实现的检查接成 Allow。

用户已确认以上路线。首版精确契约已纳入授权上下文规范：独立的 create/read/revoke Tenant Permission，通过既有自定义 Role 显式分配；一个 Engine 对一个有效 User Membership，必须显式指定到期时间与原因，不设默认期限、不实现 namespace 递归、不自动给内置 Role 或账号赋权。本轮先实现 System 的委派管理接口、版本化迁移与事务审计，不将其描述为表级读取授权完成。

本轮验收清单：

- [x] 引擎访问控制领域持久化委派，支持创建、分页读取、详情与版本化撤销；历史不可恢复／删除。
- [x] 当前 Tenant、User 类型、有效成员关系、引擎实时目录能力、有限期限、重叠委派、并发版本与事务审计测试。
- [x] Permission Manifest、生成常量、向前迁移 `000166`、Swagger 与中文／英文权限展示词汇同次发布；不自动授权内置 Role。
- [x] `make test-authorization`、全部 Go T1 与最终完整 `make test-system-iam-postgres`（IAM、OAuth、API、迁移包）通过。
- [ ] 完整 `make test-module MODULE=system` 与 `make test-changed` 通过。未通过原因分别记录，不用分项通过替代全量通过。
- [ ] 委派管理的前端办理入口、实际源读取规则、业务确认及真实消费者授权闭环。当前只有后端管理资格，不是用户已可办理或读取的证明。

2026-10-01 本轮落地记录：

- 唯一事实表为 `system.engine_access_delegations`，由 System 的 `internal/engineaccess` 维护；不写 Catalog 副本。新增四个 API 方法，Tenant 从当前认证上下文取得，拒绝请求另传 `tenant_id`；列表复用共享分页（默认 10，最多 100），不返回引擎连接凭据。
- 创建和撤销在同一事务中校验当前 IAM 功能权限、主体／成员／引擎状态与期限，保存安全审计、推进接收账号授权版本并撤销旧会话。角色名称、登记身份和 Catalog 责任不产生隐含资格。撤销须匹配版本；到期、撤销及身份失效通过当前状态呈现，不设置“恢复历史”的旁路。
- `TestEngineAccessDelegationAgainstPostgres` 已由既有 `system-iam-postgres-gate.sh` API 包的 `AgainstPostgres` 发现规则自动覆盖，不新增临时脚本或第二套测试入口。验证包含跨租户拒绝、缺少权限、失效身份／期限、并发创建与撤销、历史不可改写、审计失败整笔回滚、旧会话失效以及测试 schema 清理；使用标准隔离库 `addp_iam_test`，不连接业务源库。
- `make test-go`、`make test-authorization` 和 System 前端 `npm run build` 通过。完整 System 模块门禁的 Go T1 与 18 个前端测试文件／84 个用例通过，Playwright 14/15 通过，服务账号生命周期用例失败（授权原因填写后的按钮可用性检查，输出含 `diagnostic artifact validation only`）；该用例及运行配置存在并行修改，本轮不覆盖修改或宣称整个前端门禁通过。
- 完整 System PostgreSQL 首次运行的 IAM、OAuth、API 包通过，迁移包发现旧断言仍期望 137 个 System Permission；本轮新增三项后应为 140，已同步修正。`bash scripts/test/system-iam-postgres-gate.sh --package migration` 重跑通过；随后最终完整 `make test-system-iam-postgres` 退出 0，IAM、OAuth、API 与迁移四包全部通过，无跳过，包含迁移首次运行／重复运行以及新接口分页契约断言。
- `make test-changed` 在执行测试前因缺少多个模块的 PostgreSQL、MySQL、OceanBase T2 连接条件退出，不能计为全量通过。现有 `.github/workflows/release-and-t2-gates.yml` 的 System IAM PostgreSQL 作业调用 `make test-system-iam-postgres`，覆盖本轮迁移和 API；其他模块 T2 仍由各自既有 CI 作业覆盖。
- 未重启或接管用户的 ADDP 服务，未修改业务源库、生产／开发业务权限或现有账号授权；迁移仅在标准测试数据库中验证，未提交代码。

后续仍需：源逻辑资源限时只读规则、业务责任确认依据，以及 Manager 预览／受控 SQL 对完整 C/D 读取集合的最终校验。管理委派接口不是上述消费者已经实施的证明。

### 26.11 首次读取授权实施前的责任缺失边界（已确认）

2026-10-01 继续推进时，工作区已提交到 `b8886e03f`。只读核对发现一个不能从 D1-D3 自动推导的业务边界：**从未建立业务责任，与建立责任后因撤销编目、责任移交或身份失效而暂时缺少有效责任人，不采用同一种授权办理路径。** 用户已确认本节规则，并补充必须避免无人有权发现和处理条目的责任孤立；已同步授权上下文规范，不代表源读取授权已经实现。

#### 当前实现证据

- Catalog `entry_update.go` 的 `isWithdrawnCurationShape` 要求撤销编目时清空全部人工责任；`TestEntryUpdateWithdrawsCurationAtomically` 明确验证责任被清空、来源绑定仍保留。这是已确认的编目行为，不是需要取消的实现缺陷。
- Catalog 当前 Runtime `/runtime/references/resolve` 专用于 `addp-asset`，按 CatalogEntry UUID 返回可引用／可发布状态，不返回当前责任资格，也不按 Engine 与完整路径证明某个源资源从未建立业务责任。不能借该接口的 `found=false` 或资源盘点状态批准源读取。
- System 的模块启用状态、进程注册状态以及请求失败，均不能单独证明某个源资源从未建立业务责任；把 Catalog 关闭、服务暂不可达或条目查不到直接解释为“接入阶段”，会产生授权降级。
- 现有引擎管理委派只表达办理范围，不包含源读取 Allow；本轮未创建实际读取规则、接口、迁移或消费者放行路径，故这里是需要预先封闭的设计缺口，不是已经证实的运行态越权。

#### 已确认业务规则

| 情况 | 新增或扩大源读取授权的处理 | 已有有效读取规则 |
| --- | --- | --- |
| 确认尚未建立业务责任的接入资源 | 按 D1，由具备功能权限与当前引擎管理委派的主体，向指定账号／项目组办理精确叶子的限时只读 | 继续独立检查功能、主体、范围、期限、撤销及 Deny |
| 已建立且当前有效的业务责任 | 按 D2，先由当前业务责任人确认，再由具备办理功能与管理范围的主体落实；两方面资格分别核验 | 同上，不因责任移交自动续期或扩大 |
| 已建立责任，但撤销编目、责任人失效、移交未完成或无法核验 | 不自动返回 D1；暂停这项目标的新共享／扩大，先修复或确认责任 | 按 D3 与已有规则自身条件判断，不因责任缺失一律撤销 |
| Catalog 暂不可用或后来被停用 | 不以故障或停用恢复接入主体的独立批准资格；仅阻断依赖当前责任事实的新办理 | 不增加 Catalog 在线依赖，不阻断其他模块启动 |

首版不开放“恢复接入阶段”的自动或手工旁路。如果以后确有撤回业务纳管的业务需求，应另行明确办理资格、证据及对既有规则的影响，不能把普通编目编辑当成授权阶段变更。

后续仍须定义如何可核验地区分“从未建立责任”与“曾建立但当前缺失”，以及责任变更与规则写入的并发边界；不能提前选定一个 System 可编辑的责任副本、永久缓存、跨 Schema 查询或未经确认的阶段实体来解决。System 可以保存实际授权的履约证据，但不得据此伪造 Catalog 的当前责任。

#### 确认后的实施与验收顺序

1. 将确认后的边界先同步到授权上下文规范；细化一次业务决定绑定的精确目标、接收主体、动作、期限、确认人和当前资格证据。不把请求方自报负责人或理由文字当成批准。
2. 在 System 引擎访问控制领域实现唯一的精确读取规则与办理核验，复用现有 Allow／Deny、当前组织关系及期限语义；不自动给现有连接或账号回填读取权。
3. 贯通 Manager 预览与受控只读 SQL：授权 C 后允许 C，拒绝 D、C/D JOIN 和读取 D 的视图依赖；功能权限或资源规则任一缺失均拒绝。复用 PreparedQuery／QueryReadSet，不另写 SQL 解析器。
4. 同次覆盖 T1 规则判定、System／Catalog PostgreSQL T2 的当前责任与履约边界、Common 的读取集合、真实消费者及现有 T4 套件。补充“撤销编目不恢复接入批准资格”“责任失效不批量撤销既有 Grant”“Catalog 停用不能降级”三个拒绝／保持案例。

上一轮仅文档变更的验证记录：当时只更新既有专题，不更改已确认的撤销编目功能、运行中的权限或服务生命周期；没有进行业务源库写入。`make test-changed` 识别一个专题文档变更、无受影响业务模块，执行平台 T0 后退出 0；`git diff --check` 通过。此结果只覆盖当时的文档变更，不替代 System 完整门禁的未通过项，也不将未实现的源读取闭环记为通过。本轮代码实施与验证见 26.12。

### 26.12 责任失效后的可发现与修复通道

本轮先完善不依赖新增业务决定的 Catalog 修复通道：队列、分页数量与详情统一条目可见性，租户治理人员复用显式读取、盘点和编目维护权限处理原责任部门或人员失效的条目；普通部门治理人员不额外获得盘点权限。任务条目选择器也按调用者实际权限选择视图。责任修复不成为源数据访问或共享批准的旁路。

实施与验收清单：

- [x] 队列数据、total 与精确条目筛选复用详情可见性，并验证跨租户拒绝。
- [x] 部门责任失效后，显式具有租户盘点与维护权限的人员仍可查看条目，替换责任并自动解决旧任务；版本、审计与原来源保持一致。
- [x] 前端候选不再对无盘点权限的部门治理人员固定发送盘点查询，提示当前可见范围与修复方式。
- [x] T1、Catalog PostgreSQL T2、前端测试与构建经标准模块入口验证；新增 PostgreSQL 场景登记进既有 Catalog gate，由现有 CI 作业执行。

进一步检索发现：`system/authorization/builtin_roles.yaml` 的 `tenant.administrator` 已明确包含 Catalog 读取、盘点、维护和认证权限，现有 System IAM 测试也覆盖最后一个有效租户管理员的 Assignment 撤销、Membership 停用／期限变更及 Principal 停用拒绝。本轮在同一 IAM 场景补充迁移后的实际 AuthContext 权限断言，复用已有保护，不新增角色或默认授权；Catalog 不按角色名绕过 Permission。

不能据此宣称全部防孤立问题已完成：保护通道面向已初始化的有效 Tenant，不保证每个自定义治理角色都有值守人员，也不保证及时处理。弃用条目责任移交的新共识及实施见 26.13；已认证条目先撤销认证的独立权限要求继续保留，不自动扩权。

2026-10-01 验证记录：

- `CATALOG_POSTGRES_TEST_DSN=<已核对端口的 addp_test DSN> make test-module MODULE=catalog` 退出 0，覆盖平台 T0、Catalog Go T1、前端测试与生产构建、Catalog PostgreSQL T2。随后增补的认证修复案例已通过再次执行 `make test-catalog-postgres` 验证：直接更新认证条目拒绝、无认证权限撤销拒绝、有权撤销后替换责任成功；同一 helper 同时登记到 T1 和 T2。
- `ADDP_SYSTEM_POSTGRES_TEST_DSN=<已核对端口的 addp_iam_test DSN> bash scripts/test/system-iam-postgres-gate.sh --package iam` 退出 0。`TestTenantAdministrationClosureAgainstPostgres` 验证真实迁移后的租户管理员 AuthContext 明确持有四项 Catalog 修复权限，并保持最后一个有效租户管理员的已有保护。
- `bash scripts/swagger/gen-swagger.sh catalog` 与 `git diff --check` 通过。现有 CI 的 Catalog PostgreSQL 和 System IAM 作业覆盖新增场景，无需新增 workflow。
- 全工作区 `make test-changed` 识别 26 个受影响注册模块，在执行前因其他模块必需 T2 环境配置缺失退出；包括 Common MySQL／OceanBase、Model MySQL 及多模块 PostgreSQL DSN。未将聚合门禁计为通过，未覆盖其他会话的并行改动，也未补建外部测试基础设施。
- 未运行真实浏览器运行态验收，未重启或接管应用服务，未写入业务源库；只使用现有标准入口及允许的测试 database。

该项已确认：允许已弃用条目仅移交责任，并允许有独立权限的用户撤销弃用；实施、边界与验收见下一节。

### 26.13 撤销弃用与已弃用条目责任移交（已确认）

2026-10-01 用户确认继续实施以下两条单一路径，不新增权限项、不自动授予数据读取权、不改写源对象或资产发布状态：

| 操作 | 当前资格 | 改变与保留 |
| --- | --- | --- |
| 撤销弃用 | 当前条目可见，具有读取、编目维护及 `catalog.entry.deprecate` 权限；不限定原弃用人 | 必填原因、校验版本；`deprecated → curated`，不恢复认证；清除当前推荐继任项，其原关系留在审计中；保留其他编目事实 |
| 移交责任 | 当前条目可见，具有读取及编目维护权限；无需弃用权限 | 仅替换责任部门、业务负责人、数据管理员、可选技术负责人；保持弃用、业务定义、语义、可见范围设置和来源冻结；替换失效责任后自动解决旧任务 |

责任移交仍要求一个有效责任部门、一个有效业务负责人和至少一个有效数据管理员，由 System 当前事实核验。部门可见条目更换责任部门后，其可发现范围随之变化，前端预先提示；原责任部门失效时，由具有显式租户读取、盘点和维护权限的治理人员处理，不按角色名绕过权限。认证条目继续先撤销认证，不能使用弃用责任子资源绕过认证冻结。

实施清单：

- [x] 同步术语表、目录状态与 API 规范、授权上下文边界。
- [x] 治理子资源支持撤销弃用，责任子资源 `PUT /entries/:id/responsibilities` 只接受版本、原因及完整责任关系；拒绝夹带其他字段。
- [x] 同事务版本锁、任务解决、审计及搜索投影；分别记录 `catalog.entry.deprecation_withdrawn` 和 `catalog.entry.responsibilities_transferred`。
- [x] 详情“更多”提供权限感知的“撤销弃用”“移交责任”；责任移交复用原编目编辑器的责任选择部分，不另建同类编辑器。
- [x] T1 与 PostgreSQL 复用同一生命周期场景，登记到既有 Catalog 门禁；前端新增精确请求及责任专用校验案例。
- [x] 完整 Catalog 模块门禁与最终工作区检查通过；不把源码与自动化测试通过视为真实页面运行态验收完成。

2026-10-01 验证记录：

- `CATALOG_POSTGRES_TEST_DSN=<已核对端口的 addp_test DSN> make test-module MODULE=catalog` 最终退出 0，覆盖平台 T0、Catalog Go T1、21 个前端测试文件／94 项用例及生产构建、Catalog PostgreSQL T2。
- `make test-catalog-postgres` 在相同 DSN 下独立通过；责任生命周期场景共用于 SQLite T1 和 PostgreSQL T2，覆盖当前引用失效、隐藏／跨租户拒绝、版本冲突、审计失败整笔回滚、部门可见范围变化、旧任务解决与继任历史保留。真实路由场景覆盖读取／维护／弃用权限交集、严格请求体、Canonical BIGINT 字符串、当前可见性、校验服务不可用时拒绝及撤销后的版本冲突。
- 首轮新路由测试使用 SQLite 自动迁移时发现 schema 索引夹具不适用；已改为标准 PostgreSQL 门禁，并复用 Common 的 Canonical AuthContext 测试构造器，不复制另一套表结构或认证事实规则。最终 PostgreSQL 门禁无跳过。
- Swagger 已重新生成，平台严格路由覆盖检查通过；Catalog 当前 35 个公开路由方法。新增路由测试登记到 `scripts/test/catalog-postgres-gate.sh`，现有 `.github/workflows/release-and-t2-gates.yml` 的 Catalog 作业已调用该入口，无需新增 workflow。
- `git diff --check` 通过。未执行真实浏览器或 T4 在线验收，未重启／接管应用服务，未修改业务源库、现有账号授权或资产发布状态；本轮没有新增数据库迁移，也没有提交代码。

后续推荐：先用已有受控测试样本核对两个入口及部门可见范围变化，不对真实业务资源制造弃用或责任移交；之后返回 26.11 的当前责任可核验依据与精确源读取授权闭环。

### 26.14 弃用生命周期纳入既有 Online 验收（脚本已扩展，真实 T4 待执行）

2026-10-01 继续沿用已确认的 26.13 契约。本轮不新增业务 API、数据库迁移、账号授权或独立测试体系；只扩展已登记的 `enterprise-catalog-publishing` suite，并修正文档中两处仍暗示推荐继任项由普通编目更新维护的旧描述。

- [x] 专用永久 Catalog fixture 验证 `curated → deprecated → deprecated → curated`，使用已弃用责任子资源增减可选技术负责人，确实改变责任集合；移交仍保持弃用，其他编目事实和来源身份不变。
- [x] 旧版本撤销弃用、旧版本责任移交均须返回 `409 catalog_entry_version_conflict`，重新读取确认无副作用；当前版本成功写入须递增版本。
- [x] 撤销弃用只回到已编目、不恢复认证，当前继任项为空；最终通过正式 API 恢复完整编目聚合，并重新读取核对业务名称、说明、域、术语、责任和可见范围设置，不再仅检查业务名称。
- [x] 成功、失败和写响应丢失路径共用恢复逻辑：先读取当前状态，必要时撤销弃用，再恢复聚合；不重试旧命令，不回滚不可变审计或并发版本。恢复失败使 suite 失败，不报告零残留通过。
- [x] 专用 User 的身份预检显式要求 `catalog.entry.deprecate`，不自动赋权；复用现有 User、Domain、Department，不新增第二个身份。跨部门可发现范围和权限交集仍由 26.13 的 Catalog T2 覆盖，本轮单账号 T4 脚本不冒称验证这些条件。
- [x] 脚本故障注入纳入既有 `make test-online-runner`；现有根 Makefile、Platform CI 与 `Online T4 gates` 已覆盖，无需新增登记、suite 或 workflow。运行指南新增 `ECV-09`。
- [ ] 在专用 `addp_online` 部署执行真实 T4，并确认恢复与零残留报告；普通开发环境 Ready 不作为该项证据。

本轮只读检查发现本机 Catalog `/health/ready` 返回 200；未重启或接管服务，也未对真实条目制造弃用、移交或授权变更。工作区存在另一批 System 页面改动，本轮不修改或覆盖。

本轮验证记录：

- `make test-online-runner` 在最终脚本输入下退出 0，包含 Catalog 脚本的 13 项确定性用例及故障注入子场景，Online CI 登记一致性检查通过；这不是实际服务 T4 的运行证据。
- `make test-platform` 退出 0，现有测试／CI 登记、授权清单、全仓 Swagger 覆盖与 Online runner 检查通过。
- 重新执行 `bash scripts/infra/status.sh` 确认 PostgreSQL 实际端口为 `25432` 后，`CATALOG_POSTGRES_TEST_DSN=<该端口的 addp_test DSN> make test-catalog-postgres` 退出 0，包含已弃用责任移交／撤销弃用与真实 HTTP 权限交集场景，无跳过。
- 全工作区 `make test-changed` 在执行测试前因未注入 Catalog、System T2 DSN 而退出 2；它还包含并行 System／Console 变更，未计为全工作区通过。本轮随后独立验证上述 Catalog 与平台门禁，不覆盖或修改并行实现。
- 真正的 `make test-online ONLINE_SUITE=enterprise-catalog-publishing` 未执行：本机不是专用 `addp_online` 部署，不能拿个人开发环境或真实条目代替隔离验收。既有 `Online T4 gates` 同名 suite 负责真实复验。
- `git diff --check` 通过；没有提交代码。

后续优先返回 26.11：明确“当前责任及是否曾建立业务责任”的权威核验契约，再贯通 System 精确源读取规则与 Manager／受控 SQL。不能把本轮生命周期验收脚本当作数据授权闭环完成。

### 26.15 当前责任核验契约盘点（建立时点已确认，分步实施）

2026-10-01 先只读盘点 Catalog 与 System 当前代码，随后用户确认下述建立时点。继续遵守 26.11 已确认边界：首步仅实施 Catalog 内部历史依据及保守迁移，不新增源读取规则、Runtime API、Permission 或 System 责任副本，不改运行中的责任与授权。后续目标核验和并发契约仍是待讨论草案。

#### 当前实现新增证据

| 核对对象 | 当前事实 | 对授权核验的影响 |
| --- | --- | --- |
| `entry_update.go / validateCurationTransition` | 完成编目必须有责任部门、业务负责人和数据管理员；但保持 `discovered` 的更新仍允许保存这些责任，未强制清空 | “首次完成编目”与“首次明确保存业务负责人”不是同一时点，必须先确定何时退出独立接入批准路径 |
| `entry_update.go / Update` | 盘点时旧审计只记录前后编目状态及 `responsibility_count`，没有当时完整责任角色或主体；本轮新写审计增加精确责任角色与主体 | 旧责任数量不能证明一个始终为 `discovered` 的条目是否曾有业务负责人；不能把缺少相应历史当作从未建立 |
| `source_rebind.go / History` | 用户历史接口只返回最近 200 条审计，并按当前条目可见范围过滤 | 不用于授权端证明历史不存在；条目不可见、历史截断与从未建立是不同事实 |
| `source_entry_resolution.go / ResolveSourceEntries` | 只查询当前可见的 active 条目与 current SourceBinding | `found=false` 不能作为初始授权依据；核验还须考虑既有来源绑定历史和企业身份合并，不能因重绑丢失旧目标的责任历史 |
| `governance_task_service.go / ReconcileTenant` | `active/needs_transfer` 来自后台回查 System；保存有最近核验时间 | 条目列表中的责任状态适合展示，不代替新业务确认时对主体和当前成员关系的核验 |
| `source_sync.go / applyMetaChange` | 专业观察变化也会递增 CatalogEntry 聚合版本 | 首版可保守使用聚合版本拒绝过期依据，但会因非责任变化要求重新核验；不能未经需求新增独立责任版本 |
| `system/internal/engineaccess` | 现有服务只创建、读取、撤销限时引擎管理委派，写入同事务核验身份、权限和 Engine | 它尚不保存精确叶子读取规则，也不消费 Catalog 当前责任证据；不存在可直接启用的完整源读取授权路径 |

#### 已确认的业务分界

用户已确认：**首次明确保存一个经 System 核验有效的 `business_owner` 即算建立过业务责任，不等待条目完成编目；只批量分配责任部门、业务域不算。** 这表示该目标以后不能仅因负责人被移除、条目仍为 `discovered` 或撤销编目，回到接入主体独立批准新共享的路径。它不表示编目已经完成，也不自动授予该负责人功能权限、办理资格或内容访问权；责任不完整时仍须先补齐或修复，不能据此直接批准。

本轮不采用“首次完成编目才算建立”的替代路线。建立历史与当前是否具备业务确认资格是两个不同事实；本地历史标记不能独立作为新共享的准入依据。

#### 确认后按以下顺序细化，不一次预建授权平台

1. **Catalog 本地历史与当前资格**：本轮先实施历史部分：`catalog.entries.business_responsibility_established` 为不可由 API 编辑的 nullable boolean，`false` 仅用于正式创建的新条目，`true` 表示建立过，`NULL` 表示旧证据未知。保存有效业务负责人随完整责任替换同事务置 `true`；清空责任、撤销编目、主体失效、弃用／撤销弃用、移交和来源观察不得清除。数据库约束禁止退回；迁移只从确定的正向历史补 `true`，不补 `false`，不伪造首次时间或确认人，不递增业务版本。当前资格和精确目标核验尚未接入。证据归 Catalog，不建立 System 可编辑的责任副本。
2. **精确目标核验读契约**：输入由服务端核实的当前 Tenant、Engine 与完整结构化 EngineCatalogPath；输出区分“证实从未建立”“当前责任可核验”“曾建立但当前无法使用”与“无法判定”。绑定企业身份、来源绑定与当前版本；当前主体资格动态回查 System。这些是响应语义草案，不是新增可编辑阶段实体。未扫描、同步滞后、无可见条目、历史不完整或服务故障不能输出“证实从未建立”。
3. **确认与办理证据**：精确绑定目标、接收账号或项目组、只读动作、明确到期时间、真实确认人及责任依据；业务决定不等于读取规则已经生效。System 分别核验办理功能、当前委派范围及主体，再走唯一规则写路径，Catalog 不保存 Grant 副本。
4. **并发边界须单独明确**：只做“读取责任 → 写 System 规则”，或再读一次版本，仍不能排除两步之间完成责任移交。必须明确哪一个权威接受时点决定旧确认已履约，并与责任变更形成可验证的顺序；在这项设计确认前，不声称聚合版本或短期证据已经消除并发窗口，不引入跨 Schema 事务、永久锁或过期缓存放行。
5. **没有 Catalog 的部署**：既有管理委派允许独立建立，但不证明某目标从未建立业务责任。未部署与曾部署后不可用必须有可靠依据才能区分；部署／停用状态和 HTTP 失败均不是目标级证明。该证明路径仍需设计，不用阻断所有模块启动或全部既有访问来代替精确控制。

本轮本地历史验收复用 Catalog T1 与已登记的 PostgreSQL T2：保存／移除未编目负责人的建立依据、其他责任不触发、撤销编目保留、事务回滚、来源观察与重绑保留、弃用责任移交、旧正向历史补录、未知不降级和数据库拒绝回退。实际来源授权、责任移交与规则写入竞态、无 Catalog 部署证明尚未实施，不计为已通过。

#### 本轮实施与验证结果

- [x] 术语表、授权上下文规范、Catalog 实现规范及模块边界同步；不采用“完成编目才建立”的第二路线。
- [x] 正式新建路径明确 `false`；保存经核验有效业务负责人同事务置 `true`，完整责任审计、原有版本与搜索 outbox 仍在同一事务；不新增用户可编辑字段或公开 API。
- [x] PostgreSQL 自有 schema 迁移补列、正向历史回填和单调触发器同事务执行；旧未知不能更新成 `false`，不伪造确认人、时间、权限，不修改聚合版本或新增审计／投影。
- [x] Catalog T1 及现有 PostgreSQL T2 覆盖建立时点、非负责人责任、移除／撤销保留、有效性与版本拒绝、审计故障整笔回滚、移交／弃用生命周期保留、旧数据迁移、原始旧表结构升级、重复迁移与禁止回退。来源重绑保留目标身份历史由既有 T1 用例覆盖；精确资源跨绑定授权尚未实施。
- [ ] 精确目标当前责任核验、业务决定履约与 System 规则写入；尤其责任变更与跨模块规则写入的接受时点，以及未部署 Catalog 的可信证明，仍须按上述顺序讨论确认。不能把本轮内部标记单独用作授权放行条件。

验证证据：先以 `bash scripts/infra/status.sh` 确认本机 PostgreSQL 实际映射 `25432`，仅使用 `addp_test`。`CATALOG_POSTGRES_TEST_DSN=<已核对测试连接> make test-module MODULE=catalog` 最终退出 0，覆盖平台 T0、Catalog Go T1、21 个前端文件／94 项用例与生产构建、Catalog PostgreSQL T2。首跑曾因旧表升级夹具预编译 `SELECT *` 的结果结构缓存冲突失败；改为显式选择待核验字段后，`make test-catalog-postgres` 及完整模块重跑均通过，未改应用迁移路线。`git diff --check` 通过。

工作区 `make test-changed` 同时识别其他会话的 System／Console 改动，因未配置 `ADDP_SYSTEM_POSTGRES_TEST_DSN` 在执行前退出 2，不计为全工作区通过。新增用例复用已登记标准门禁和既有 CI Catalog 作业，不另建旁路脚本。未执行真实 Online T4，不重启服务、不变更真实条目、不操作业务源库；开发库迁移尚未执行，将在正式启动时由 Catalog 自有迁移入口执行。

### 26.16 精确责任核验与办理时点（分界已确认，接口未实施）

2026-10-01 继续核对现有实现，用户随后确认下述正式接受办理分界。该分界已同步术语表、授权上下文规范 5.5、Catalog 实现规范和模块说明；精确读契约及公开接口仍待细化，不表示已经具有表级授权能力。确认后本轮仅更新文档；不增加 Runtime API、Permission、业务决定表或源读取规则，不操作实际权限与业务源库。

#### 先把“读到责任”与“可以办理”分开

核验输入应由办理服务确定当前 Tenant，以及经过引擎目录能力验证的 Engine 和完整结构化 EngineCatalogPath。当前账号、功能权限和管理委派由 System 核验，不能接受前端提交的责任身份或角色名称作为证明。核验输出只是本次判定依据，不是 Access Token、Grant，也不能自行打开 Manager 或 SQL 读取入口。

Catalog 应提供的最小事实范围如下，字段名、公开路径和服务身份 Permission 尚未定稿：

- 精确请求目标及定位是否明确；不能用名称搜索、CatalogEntry UUID 或 fingerprint 替代完整目标。
- 对应的企业身份、来源绑定身份、是否为当前绑定及聚合版本；查询不能受普通目录可见性过滤后，把“不可见”解释成“没有责任”。这不意味着向普通用户开放全租户条目，内部核验仍须限定可信调用方、当前 Tenant 和必要信息。
- 业务责任建立历史为“建立过／可证明尚未建立／未知”；条目内部 `false` 单独不证明一个实际目标从未建立责任。
- 当前明确保存的业务负责人，以及本次动态回查 System 后的主体和 Tenant Membership 有效性。后台 `active`、最近核验时间与列表摘要不代替这次检查。
- 任何定位歧义、身份合并链无法核实、历史不足、同步滞后或权威服务错误，都不能转换成接入主体可独立批准；错误不伪装成空结果。

| 判定情形 | 后续处理边界 |
| --- | --- |
| 精确目标的历史可证明尚未建立业务责任 | 还须由 System 核验显式管理委派、接收主体、只读范围和到期时间；核验结果本身不授予读取权 |
| 建立过，当前业务负责人有效且符合业务确认条件 | 进入当前责任人的业务确认路径；仍须分别核验功能、办理资格和目标管理范围 |
| 建立过，但当前负责人缺失、失效或尚未修复 | 拒绝新共享或扩大权限，走已确认的责任修复路径；不影响原有仍有效的规则 |
| 尚未扫描、尚未同步、缺少历史或无法精确关联 | 无法判定，不得按“从未建立”放行；不能靠新建一个 `false` 条目消除旧目标的未知历史 |

这四项是核验结果的含义，不是新增治理状态或可由用户切换的流程。业务负责人有效也不等于其他全部准入条件已经满足。

#### 当前定位能力还有哪一块未贯通

现有 `ResolveSourceEntries` 按来源身份查询当前可见、active 条目与 current 绑定，适合界面导航，不承担上述授权证明。来源重绑会保留旧绑定，并把临时企业身份合并到目标企业身份；只查当前绑定会漏掉旧目标的历史，不能因此让旧目标重新进入独立接入批准路径。

Meta 变化摘要当前携带 fingerprint、Engine、Item、原生 item type、`full_name`，并已为 table/view 补充结构化 `schema_name/table_name`；它没有直接携带所有引擎通用的完整 EngineCatalogPath。后续须复用 Provider 声明模型与已有结构化定位能力核实映射，不能拆分 `full_name` 或猜测 namespace。首版 PostgreSQL 闭环只承诺已明确覆盖的 table 目标，不能宣称已经覆盖全部引擎、view 或文件组合资源。旧绑定无法可靠映射时输出无法判定，不要求向源库安装任何身份追踪机制。

精确目标定位属于独立工作，可以先细化；跨模块授权生效实现则依赖下面的业务时点决定。

#### 已确认的业务时点

已确认规范要求：尚未正式接受办理的决定必须重新核验当前责任。下面区分最终接受与权限生效，不能实现成“业务负责人一确认，日后都能凭旧确认授权”：

1. 甲是当前业务负责人，确认向项目组共享 C，范围和到期时间均明确。
2. 办理方请求最终责任核验；Catalog 在受控操作中核实甲仍是当前负责人，并正式接受这一次精确办理。
3. C 的责任移交给乙。
4. System 才完成读取规则写入，权限实际生效。

**用户已确认**：以第 2 步的“正式接受本次办理”作为责任资格的分界；在接受之前完成移交的，甲的旧确认必须由乙重新确认；在接受之后才移交的，只允许已经接受的同一次、同参数办理继续完成。第 1 步的业务确认不等于第 2 步的接受，更不等于第 4 步的权限生效。授权上下文规范已据此明确“尚未正式接受办理”的边界，不再保留以 System 提交时责任必须未变更为准的另一条路线。

该分界仍要求第 2 步的接受与责任变更有可验证先后顺序。简单再读一次版本、缩短证据期限或事后撤销都不能替代这个要求，不用跨 Schema 事务、永久锁或责任副本绕过这一问题。

仍须在实现前解决：接受动作与 Catalog 责任移交必须有可验证的先后顺序；接受记录精确绑定目标、接收主体、动作、期限、真实确认人和当时责任依据；重试不得改变业务参数或产生第二份规则；System 写规则时仍独立核验当前办理资格、管理范围与接收主体有效性。接受凭据不能变成永久令牌，Catalog 故障不影响旧规则执行，也不能为新的接受降级放行。

2026-10-01 用户确认：5 分钟是系统自动办理窗口，不是接受人员操作时限，可以采用。该窗口从正式接受开始，截止时间不晚于本次拟授权的绝对到期时间；规则已经成功写入但响应丢失，同次、同参数重试只返回原办理结果，不再次授权。到期仍未生效的，重新核验当前责任并接受，不延续旧依据；移交后须由新负责人重新确认。短期限不能替代精确绑定、一次办理的幂等身份、当前 System 资格复核或接受与移交的顺序保证。已同步正式规范，窗口代码及端到端消费尚未实施。

#### 确认后最小验收清单

- [ ] 精确定位：同名不同 schema、不同 Engine、跨 Tenant 均不能串用责任；旧绑定／合并历史和不完整定位不得误判为从未建立。
- [ ] 当前资格：移除负责人、主体／成员关系失效、修复及后台状态滞后均由新鲜事实判断，不自动授予业务确认或办理功能。
- [ ] 先后顺序：用确定性并发屏障覆盖“移交先于接受”及“接受先于移交”，不以普通顺序单元测试声称消除竞态。
- [ ] 失败与重试：接受成功后响应丢失、System 写入失败、重复提交、参数变化及证据到期分别有明确结果，不重复扩大授权。
- [ ] 生效边界：确认、接受和实际生效在界面与审计中可区分；现有合法读取在 Catalog 不可达时仍能执行，新办理不能降级放行。

后续沿用根 Makefile：Catalog／System owner 的 T1 与 PostgreSQL T2 验证各自事务和幂等约束，跨模块先后顺序、权限实际执行和故障恢复纳入已登记 Online 体系；实现时同步确认自动发现与 CI 编排，不现在登记空占位测试。当前这些验收项均未实现、未运行，不计为通过。

本轮文档更新验证：`git diff --check` 和 `make test-platform` 均退出 0；后者覆盖现有平台一致性、CI 登记、授权清单和 Swagger 路由覆盖检查，不证明本节尚未实现的责任核验或授权并发能力。未重跑模块 T1／T2、未执行真实 Online T4、不重启服务；26.15 的代码验收结果仍按该节记录的范围解释。

### 26.17 自动办理窗口与下一步接入边界

2026-10-01 用户确认正式接受后 5 分钟的系统自动办理窗口。该决定已同步术语表、授权上下文规范 5.5.3、Catalog 实现规范 3.2.1 和模块说明；不改变人员填写／审核时间，也不改变拟授予的访问到期时间。本轮没有新增接受记录、公开接口、Permission 或源读取规则，没有重启服务、修改真实权限或操作业务源库。

#### 已确定的窗口验收要求

| 情形 | 必须验证的结果 |
| --- | --- |
| 正式接受后 5 分钟内、拟授权尚未到期 | 仅允许同一次、同精确参数办理继续；System 仍检查当前功能、管理范围及接收主体 |
| 拟授权早于 5 分钟到期 | 使用更早的绝对截止时间，不能借接受窗口延长访问 |
| 请求到达时未到期，等待数据库锁后已到期 | 权威写入事务拒绝新规则；不以入口检查代替事务检查 |
| 重试／重发／接受后的责任移交 | 不修改接受时间和截止时间，不顺延访问到期时间 |
| 窗口到期、规则尚未生效 | 旧接受依据不再可用；重新核验并接受，当前责任变更须由新负责人重新确认 |
| 规则已生效，但响应丢失后在窗口外重试 | 同次、同参数返回原办理结果，不新增规则；该结果不代表规则当前仍有效 |
| 同一办理标识更换目标／接收主体／动作／到期时间 | 拒绝复用，不作为新请求悄悄扩大授权 |

#### 实现前的权限接入缺口

本轮只读核对 `catalog/backend/internal/api/router.go`、Catalog／System 的生成权限清单及 `system/backend/internal/engineaccess/service.go`：

- 现有 `catalog.entry.update` 负责编目维护，不能替代业务共享确认；现有 Runtime 引用解析只允许 Asset 调用，不是 System 最终责任核验／接受接口。
- 现有 `system.engine_access_delegation.create/read/revoke` 只维护管理委派，不是表级源读取规则的创建／读取／撤销权限；不能用创建委派权限顺带开放读取授权。
- 现有来源解析受条目信息可见性和 current 绑定过滤，适合导航；不能原样用来证明目标没有业务责任。

2026-10-01 用户已确认：**业务共享确认权限与源读取授权办理权限独立，首版仅显式分配**。继续由 System IAM 通过 Role Assignment 分配，与当前责任／精确管理范围相交；不自动加入内置角色、不自动分配给编目人或责任人。该策略已同步正式规范，不再作为待讨论事项。权限发布规范同时要求 active Permission 必须有真实消费入口，不能先启用无人使用的权限或编造 Swagger 占位 Operation。随后依次推进：

1. 定义可信调用方和精确目标核验契约；仅承诺首版 PostgreSQL table，拒绝不完整定位和未知历史，不建设 System 责任副本。
2. 在 Catalog 权威事务中实现最终接受，复用条目锁与责任变更确定先后顺序；保存一次办理的不可变依据，不建立 Grant 副本。
3. 在 System 唯一规则写入事务中实现当前资格复核、窗口截止及同次幂等；查原结果与新写入严格区分，避免接受到期后重新授权。
4. 通过标准 owner 门禁验证，再接入 Manager 预览与 SQL 执行的 C／D 精确范围；未贯通消费者前，不宣称表级权限闭环完成。

权限与可信接口契约未定前，不新增暂时无法被安全调用的接受表或孤立服务函数。后续测试须由既有 `make test-module MODULE=catalog`、System owner T1／T2 和 Online 标准入口拥有；新增 PostgreSQL 顶层用例须同时更新 owner gate 的筛选范围，不把未运行用例计作通过，也不登记空占位测试。

本轮验证：先由 `bash scripts/infra/status.sh` 核对 PostgreSQL 实际映射 `25432`，再使用 `addp_test` 执行 `CATALOG_POSTGRES_TEST_DSN=<已核对测试连接> make test-module MODULE=catalog`，退出 0，覆盖平台 T0、Catalog Go T1、21 个前端测试文件／94 项测试及生产构建、Catalog PostgreSQL T2；`git diff --check` 通过。这只证明现有 Catalog 门禁通过，不证明本节未实施的接受窗口与源读取授权逻辑。工作区变更发现还包含其他会话的 System／Console 改动，本轮只验证本次明确 owner Catalog，没有声称全工作区 `make test-changed` 或 Online T4 通过；不改动其他会话的实现。

### 26.18 独立权限策略固化与已观察身份校验

2026-10-01 继续推进，先固化已确认的独立 Permission／显式角色分配策略，再检查精确目标消费基础。未新增 Permission、迁移、业务确认或正式接受接口，不修改实际账号授权和业务源库，不重启服务。

#### 已完成的独立修复

Catalog 共用数值解析函数原来直接将浮点转为整数，可能把来源引擎 `9.5` 截成 `9`，继而查询另一引擎的质量摘要。现改为只接受有限、无小数且处于浮点安全整数范围内的浮点值；原生整数保持完整精度，不新增字符串兼容解析。无效输入保持未解析，不能作为专业模块查询目标，不改写原始来源身份或责任历史。

回归测试先在旧实现上复现小数截断、超范围／非有限值错误接受，以及错误调用 Quality 的问题；随后修改同一个共用解析函数，列表、搜索投影、收藏、协作集合、来源解析和数据字典沿用该函数，不另建解析路线。Quality 公共客户端已有回包引用一致性校验，此轮不重复建设；错误发生在请求前的目标解析，回包校验不能纠正一个已被串用的请求目标。这项修复不等于完整 EngineCatalogPath 校验或源读取授权完成。

新增用例由现有 `make test-module MODULE=catalog` 的 Go T1 自动发现，既有 CI `make test-go` 覆盖；未新增 PostgreSQL 顶层测试或测试依赖，无须修改 T2 筛选及工作流注册。本轮先核对 Infra 实际端口，再使用 `addp_test` 执行 `CATALOG_POSTGRES_TEST_DSN=<已核对测试连接> make test-module MODULE=catalog`：旧代码运行退出 2，仅新增身份校验回归用例失败；修复后同一入口退出 0，平台 T0、Catalog Go T1、前端 21 个文件／94 项测试及生产构建、Catalog PostgreSQL T2 均通过。`git diff --check` 通过。日志分别在系统临时目录 `/tmp/addp-catalog-observed-identity-red.log`、`/tmp/addp-catalog-observed-identity-green.log`；未运行 Online T4，也未声称其他会话 System／Console 改动通过本轮验证。

#### 已确认的弃用边界

已有规范允许已弃用条目移交责任，并允许具备对应权限的人员撤销弃用。2026-10-01 用户确认弃用与新源读取授权办理的边界如下；已同步正式规范，接口仍待实现：

- 已弃用条目禁止新的业务共享确认与正式接受办理。
- 弃用前已正式接受的同一次、同参数办理，按原 5 分钟窗口继续，不刷新接受依据。
- 已生效的访问规则不因目录弃用自动撤销；必要撤销由具备独立办理资格的主体在 System 权威写路径完成。

撤销弃用不自动恢复未接受的旧决定或过期依据。接下来推进精确目标当前责任核验与正式接受契约，再贯通 System 规则写入和真实消费者，不先登记孤立权限或接受表。核对事务发现同步先锁绑定、人工维护先锁条目的顺序冲突，先统一此基础，避免在接受路径上延续相反锁顺序。

### 26.19 已弃用条目办理边界与来源同步锁顺序收敛

2026-10-01 用户确认 26.18 的弃用边界，已同步术语表、授权上下文规范、Catalog 实现规范和模块指引。该规则约束后续业务确认及正式接受接口，不将现有编目状态更新误记为授权办理已经上线。

核对现有写路径发现：人工编目、治理和来源重绑先锁企业条目，而 Meta／其他专业来源同步先锁来源绑定、再更新条目；多条目的同步事件顺序还可能与来源重绑的 UUID 顺序相反。本轮已将两个来源同步处理器统一为“checkpoint → 只读定位绑定 → 本批既有条目按 UUID 升序加锁 → current 来源绑定按 UUID 升序加锁 → 应用变化”，删除原先逐事件先锁绑定的路线。锁内重新核对绑定仍属于已锁定条目；若移至本批之外或不再 current，整批拒绝并回滚，游标不推进，由同一变化源重试，不追到未锁定条目继续写入。新发现条目在同一事务内创建，同批重复引用复用已创建绑定；重复或旧版本事件不倒退来源版本、不重复递增聚合版本。

新增回归覆盖两个来源处理器的聚合／绑定顺序及 UUID 排序、预定位后绑定移动的回滚、批内重复事件与不同来源类型身份隔离，以及更新时保留弃用状态和不可回退责任历史。绑定移动用测试回调确定性插入在预定位和加锁之间；这是锁内重核验验证，不是已完成真实多连接并发压力测试，更不是正式接受与 System 授权事务贯通验证。

测试沿用现有 Go T1 自动发现，三组断言也加入已登记的 `TestPostgresResponsibilityRecoveryUsesEntryVisibility`，由 `make test-catalog-postgres` 及既有 CI T2 执行，不新增测试数据库或重复 CI 路线。先用 `CATALOG_POSTGRES_TEST_DSN=<已核对 addp_test 连接> make test-module MODULE=catalog` 在旧代码上复现新增锁顺序用例失败，退出 2；修复后同一入口退出 0，平台 T0、Catalog Go T1、前端测试／生产构建和 PostgreSQL T2 均通过。日志在 `/tmp/addp-catalog-source-lock-red.log` 与 `/tmp/addp-catalog-source-lock-green.log`。本轮未变更 HTTP 契约、未重启服务、未修改真实账号授权或业务源库，未运行 Online T4，其他会话的 System／Console 改动不计入本轮验收。

优先下一步仍是 26.18 所述精确目标责任核验及可信正式接受消费契约：先核对 System 发起消费时的服务主体、独立功能 Permission、精确 Tenant／Engine／EngineCatalogPath 和同次参数身份，再让真实权威写路径消费 5 分钟接受依据。不得仅增加无人消费的接受表、占位 Permission 或只读核验端点；表级 Grant 和正式接受接口仍待实现。

### 26.20 正式接受消费链路的实施前核对

2026-10-01 继续推进时，工作区初始干净。先查清真实调用入口和部署事实，不新建接受表、占位 Permission 或模块间责任副本，不修改真实授权或重启服务。本节区分后续已确认的修订原则与仍待讨论的退出协议、期限建议，均不能当作已实施的授权规则。

#### 当前实现事实

- Catalog 当前 Runtime 引用解析仅允许 `addp-asset` 调用，用途是条目引用解析，不是精确源目标责任核验或正式接受办理。System 当前 `engineaccess` 只有管理委派，没有表级源读取 Grant 写入和接受依据消费入口。
- 当前未找到供 System 调用 Catalog 的 `addp-system` Confidential OAuth Client／Service Principal。后续需要沿既有机器身份路线建设真实调用方，不借用 Asset 的身份，不把调用者提交的责任人、Permission 或接受时间当可信事实。
- System 的 `module_definitions` 是平台级持久定义，注册时创建，管理员停用只更新 `enabled`；租约和进程活性另存于 `module_runtime_instances`。当前仓库未发现删除模块定义的业务入口。迁移 `000070` 也保留旧注册表中的模块定义，而不迁移旧运行实例为有效租约。
- 这份记录可以区分“有持久 Catalog 注册记录”与“当前实例可达”，但没有 Tenant 级业务责任历史，也不能证明某个精确目标从未建立责任。Catalog 的现有条目查询受可见性和 current 绑定过滤，查不到条目同样不能作为无责任证明。
- Catalog 的业务 HTTP 路由受统一 Ready 守卫约束，Ready 包含 System 注册状态。该事实可用于后续验证首次注册与接入办理的先后关系；仅先后读取一次模块状态，还不能证明并发授权安全。

#### 已确认修订：按资源核验，不以曾注册模块永久锁定

2026-10-01 用户认可修订后的原则，并补充“初期部署全套试用，后来只需单个技术人员完成 Transfer 和简单计算，因此决定撤销 Catalog”的场景。此前平台级持久注册判据过于粗糙，已撤回，不按该草案实施。

1. 某项精确资源已建立业务责任，或者无法可靠判断其责任历史时，不能因为 Catalog 失联、停用或查询失败，自动退回接入主体独立审批。能够可靠确认从未建立业务责任的资源，仍可使用有限接入授权。
2. 部署、注册和试用模块本身不建立业务责任。模块注册记录只能提供运行与部署线索，不代替目标级责任证明，也不使其他 Tenant、新资源或整个系统永久进入必须依赖 Catalog 的授权路径。
3. Catalog 是可选模块，用户可以明确决定停止使用；不能因曾部署过而永久阻断 Transfer、计算及其他正常能力。长期不使用 Catalog 的部署也是有效使用场景，不应被强制要求最终升级为完整目录治理。
4. 主动退出与临时故障必须区分。退出不能直接清除不可回退责任历史，不能自动授予内容访问、写入或全租户公开；已有有效规则仍由 System 按原范围判定，不因模块退出自动撤销或扩大。System 继续是唯一源数据规则权威，不建第二套授权规则。

#### 主动退出后的批准安排：已确认架构，细化协议待落实

建议采用最小退出处理：没有建立业务责任且能够可靠证明的范围，不要求逐条制造负责人或审批任务；已建立业务责任的范围，则明确谁承接后续批准职责后退出。单人使用可以由同一技术人员承担经明确授权的相应职责，不凭“技术人员”“管理员”称谓或登记连接自动补权。

2026-10-01 后续讨论已确认：System 保存精确源目标的最小批准要求；退出使用独立治理 Permission、明确范围、有效承接安排与审计；System 独立批准还要有独立批准 Permission 和当前有效的引擎管理委派。不是清空责任历史，也不是恢复临时接入。细化状态、并发及仍需讨论的权限边界见 26.21。

此处只记录目标和设计进展，没有新增退出接口或真实授权。后续需要证明的是精确责任核验与明确交接的顺序，不是只锁一次模块注册记录就宣称授权安全。

#### 待确认建议二：接入阶段限时只读的上限

授权上下文规范 5.5.2 已要求显式绝对到期时间，但期限上限尚未确认。建议接入主体在目标从未建立业务责任时，独立批准的只读授权最长 **24 小时**，且不晚于接收主体相应有效期；不预填期限、不提供永久选项，也不因重试自动续期。再次办理重新核验当前责任历史，不能用重复接入授权绕过已经建立的业务责任。

该建议仅限制接入阶段独立批准的新只读共享，不改变已有管理委派期限，也不擅自限制已由业务负责人确认的授权期限。此前确认的 **5 分钟**仍是正式接受后供系统完成写入的窗口，两者不能混用。

24 小时仍未获确认，也不能套用于长期不使用 Catalog 的全部数据操作或主动退出后的授权安排；未部署／主动退出的长期使用场景与临时接入共享需要分开讨论，不能要求用户不断续办临时权限才能使用 Transfer 或计算。

#### 确认后的实施与验收顺序

先同步正式规范，再将可信调用、Catalog 权威接受和 System 唯一规则写入作为一个可实际消费的办理链路实施；不先发布孤立权限。责任已经建立但当前失效或历史未知时，拒绝进入接入独立批准路径。随后贯通 Manager 预览和 SQL 的精确读取集合，不用页面可见性代替内容授权。

除 26.17 的窗口、同次参数和响应丢失矩阵外，新增验收必须覆盖：部署试用不自动建立责任、单个资源的责任或故障不扩大为全平台锁定、已建立／未知责任的范围失联不自动降级、明确退出不等于停用或清空历史、退出后的既有访问与新的批准安排、责任建立与授权／交接并发，以及确认后的接入期限和主体有效期。已登记的 Catalog／System owner T1、PostgreSQL T2 和 Online T4 分别拥有其断言；新增 PostgreSQL 顶层用例同次纳入 owner gate，真实跨模块链路纳入既有 Online 分发体系。此处是实施清单，不表示已新增或运行上述测试。

本节初次调查仅更新本专题。`make test-changed` 在启动时发现 1 个文档改动、无受影响 owner，执行平台 T0 后退出 0；本专题的 `git diff --check` 通过。门禁运行期间工作区出现其他会话的 System／前端改动，已保留，未将这些后续变更算作本轮完整 owner 验收。未运行接受／Grant 的功能验证或真实 Online T4，未宣称该能力上线。

用户确认按资源核验并补充主动撤销场景后，已同步术语表、授权上下文规范、Catalog 实现规范和本专题，删除平台级永久锁定的草案路线。`make test-changed` 发现这 4 个文档改动、无受影响 owner，平台 T0 退出 0；`git diff --check` 通过。本次不变更代码、数据库、真实授权或服务运行状态，未执行退出／交接功能验证；待讨论协议不计为已实现。

### 26.21 System 最小批准要求与可选 Catalog 交接

2026-10-01 用户认可由 System 保存最小业务确认要求，并允许通过独立权限明确交接、解除要求。本轮先将这一架构补充落入术语表及授权规范 5.5.5，核对现有消费路径，不以新增表或占位 Permission 冒充实现。

#### 已确认的事实边界

- Catalog 保存企业条目的业务责任、业务授权决定及当前业务核验；正式受理回执归属已在 26.22 更新为 System。
- System 的引擎访问控制领域保存精确源目标的批准要求和唯一 Grant；不复制 Catalog 的负责人、责任部门或责任历史。
- 批准要求只影响新授权办理，不是内容访问 Allow，不替代接收主体、动作、期限及当前办理资格检查。
- 部署或注册 Catalog 不自动启用要求；停用、失联不自动解除。退出是有独立资格、明确范围、有效承接安排和审计的治理动作。
- 独立批准资格是独立功能 Permission 与有效引擎授权管理委派的交集，不由编目、责任身份、登记人或角色称谓推导。首版不自动加入内置 Role，不回填真实账号。
- 长期不部署或主动退出 Catalog 的部署应能正常使用；不被强制要求每 24 小时续办临时接入授权。具体 Grant 期限仍由相应批准契约确定。

#### 当前实现核对与消费缺口

已重新核对 `system/backend/internal/engineaccess/service.go`：只有管理委派的创建、查询和撤销，使用当前 Tenant User、独立 Permission、有效期及事务审计；没有精确源目标的批准要求或表级 Grant 写入口。`catalog/backend/internal/service/entry_update.go` 与 `entry_responsibilities.go` 当前在 Catalog 本地事务保存责任，`replaceResponsibilities` 同事务把建立依据置为 true，但没有同步 System 批准要求。

Catalog 的来源解析面向当前绑定与当前用户可见条目，返回 Meta 身份、最小已观察摘要等导航信息；它不是包括旧绑定历史在内的无责任证明。不能把当前解析为空、条目不可见、来源丢失或系统中尚无新策略行解释成“允许独立批准”。

这意味着首次启用、存量初始化、责任写入和退出都必须接入真实办理链路：只新建 System 策略表而让责任更新及 Grant 不消费，或者只在授权前发一次 Catalog GET，都不能完成改造。

#### 事务契约的确定部分

1. 精确目标唯一键复用 `Tenant + Engine + 完整结构化 EngineCatalogPath`；不能使用 CatalogEntry UUID、Meta 行 ID 或指纹替代。对不同目标分别处理，不按名称相似或整库递归扩展。
2. System 的批准要求使用并发版本，写入与审计同事务；变更和 Grant 写入必须共享该目标的并发边界。模块启停、心跳、旧同步回放不写这个事实。
3. 首次建立业务责任不能留下“Catalog 已建立责任，System 仍可独立批准”的窗口。System 要求已生效而 Catalog 保存失败时，保留收紧要求，显式修复或交接；不以失败补偿自动放开。协调协议须能区分未接受、已收紧待完成及已完成的同次请求，不声称跨数据库原子事务。
4. 原因、精确范围、真实操作人和本次参数身份必须可追溯。User 事实来自当前 AuthContext，机器调用限定自己的 OAuth Client、Tenant Context 和精确 Permission；不信任请求正文自报的责任人、权限或接受时间。同步转发 User Bearer 仅在当前调用栈内使用，不能持久化。
5. 退出在 System 核验独立治理资格及有效承接安排后变更要求；不删除 Catalog 历史或已有 Grant。不要求不可达的 Catalog 恢复在线才能允许发起明确退出，也不把故障当退出完成。
6. 没有批准要求的存量目标保持待核清，不能伪造“从未负责”。长期不使用 Catalog 的新部署通过明确初始化建立独立批准安排，不依赖模块注册记录推断。
7. 已明确退出后，旧业务责任或重新部署 Catalog 不自动恢复业务确认要求。重新启用使用新的明确治理操作与当前版本；旧请求重试不得变成一次重新启用。

#### 两处已确认的业务选择（2026-10-01）

**首次启用资格。** 用户认可：把精确源目标的批准要求改为需要 Catalog 确认，须同时具有独立治理配置 Permission 与该引擎的有效管理委派。现有 `catalog.entry.update` 只控制编目信息维护，不隐式扩展为授权治理权。缺少资格时不能先保存源目标业务责任再让 System 继续独立批准；界面应明确提示需要由具备资格者完成纳管。首次保存有效 `business_owner` 即建立业务责任的规则不变；标准、模型等非物理源目标的责任维护不因此要求引擎管理资格。

**退出与已接受办理的先后。** 用户认可：退出不取消在退出前已正式接受的同次、同参数办理，仍在原 5 分钟内完成；System 继续核验办理资格与接收主体，不增加范围或刷新窗口。退出后发起的新办理使用新的批准安排。System 成功写入后的响应丢失重试只返回原结果，不再授予或续期。该业务规则已确认，但两模块如何证明接受与退出的先后仍须通过事务协议落实。

#### 实施与验收清单

2026-10-02 接入顺序：先接通生产 Runtime 只读核清／关闭、真实 Bearer 客户端及 Catalog 已提交待核清请求的后台消费者；不提前开放缺少可信 owner 依据的新受理。随后补齐准备与受理的完整身份链。核清服务使用现有 `addp-catalog` 身份，不需要等待 `addp-system` 反查身份配置，因此可独立验证故障恢复，不能据此宣称整个工作包完成。

- [x] 最小批准要求、权威归属、模块可选性与明确退出原则已确认，并同步术语表及规范。
- [x] 核对现有 System 委派与 Catalog 两条责任写路径，确认它们尚无批准要求及 Grant 消费链路。
- [x] 确认首次启用资格与退出时已接受办理的处理；唯一请求、版本、同次参数及可信调用契约仍待落实。
- [ ] 同次实现 System 批准要求、独立批准／交接、唯一 Grant 写入口与对应真实消费 Permission；Catalog 责任纳管及正式接受同时消费。迁移不得跨 Schema 读取 Catalog、创建责任副本或篡改真实授权。
- [ ] 将存量目标可靠初始化、失败后的显式修复、退出及重新启用纳入同一协议；不使用模块存在性或策略行缺失做开放式兜底。
- [ ] T1：精确目标、当前资格、版本冲突、5 分钟、同参重试、部署不启用、失联不解除、退出不改已有 Grant；由现有 System／Catalog Go 测试自动发现。
- [ ] T2：真实并发证明纳管与 Grant、退出与接受、重试与重新启用的先后，失败后无额外授权；新增顶层 PostgreSQL 用例同次加入 `make test-system-iam-postgres`／`make test-catalog-postgres` 的 owner 筛选，不能只增加测试文件。
- [ ] 真实 HTTP／IAM 合同变化同次更新 Swagger、Manifest、常量、覆盖报告及向前迁移；沿 `make test-authorization` 和两个 owner 的现有 T0-T3 标准入口，不发布无人消费的 Permission。
- [ ] Manager 预览与 SQL 完整 QueryReadSet 贯通同一表级规则；共享代码变化按 `make test-changed` 的真实依赖扩散验证，不人为裁剪消费者。
- [ ] T4：在已有隔离 Online 分发体系中接入真实 System—Catalog—Manager／SQL 链路与退出场景，首轮真实通过前不计为能力上线，不用 Repository 夹具替代。

本轮只修改以上 4 个文档，不修改代码、业务数据库、真实账号授权或服务状态。工作区其他服务生命周期及 System 启停改动来自并行工作，保留但不算作本轮实现。

#### 本轮验证记录

- `make test-platform` 通过，覆盖平台规范、权限声明、Swagger 路由覆盖和测试入口一致性。
- `make test-changed` 首次因未提供 `ADDP_SYSTEM_POSTGRES_TEST_DSN` 在前置检查中退出；通过 `bash scripts/infra/status.sh` 核对实际 PostgreSQL 端口为 `25432` 后，显式指定规定测试库 `addp_iam_test` 重跑，最终通过。自动发现包含并行工作中的 System 改动，因此除 T0 外还执行了 System Go T1、前端 90 项单元测试、20 项页面测试、构建及 IAM PostgreSQL T2；测试库仅由标准门禁准备和清理，没有操作开发业务库。
- `git diff --check` 通过。以上结果验证当前工作区，不证明本节待实施的批准要求、退出或源数据 Grant 链路已经完成；这些能力没有运行验收，T4 仍未执行。

### 26.22 业务确认与正式受理的事务归属（已确认，待实现）

2026-10-01 用户先认可 26.21 的两项选择，随后明确认可本节权威调整：Catalog 管业务批准，System 管正式受理与实际授权。已同步术语表、授权上下文规范、Catalog 实现规范及两模块 `CLAUDE.md`。权威归属已确认，不等于完整协调协议已经实现；此前故障检查发现 26.23 的本地锁失效问题，本轮先实现持久协调底座，真实业务批准与授权链路仍需贯通。

#### 具体问题与现有证据

Catalog 的 `entry_update.go`、`entry_responsibilities.go`、`entry_governance.go` 都在本地事务中锁定条目；`source_sync_locks.go` 也采用条目先于来源绑定的顺序。这些锁可供未来核验路径与本地责任移交、弃用及重绑排序，却不能与 System 的批准要求交接共享同一事务；当前尚无正式接受接口。System 的 `engineaccess/service.go` 当前只有管理委派，没有可以承接正式接受的持久办理回执。

例如：Catalog 本地接受已经提交，通知 System 的请求却超时；随后 System 完成退出。System 若一律拒绝旧要求，会错误取消退出前已接受的办理；若一律允许旧依据，又可能允许退出后才完成接受的请求。客户端时间、两个服务的时钟比较、再读取一次版本和模块在线状态都不能证明这里的先后。

Workbench 已有 Asset 授权到 owner Grant 的幂等办理路径，但它不处理 Catalog 当前责任与 System 退出的跨模块排序，也不是源数据权威；不能借用其授权表、Asset 服务身份或简单复制该路径。

#### 已确认调整：Catalog 管业务确认，System 管正式受理回执

保留已经确认的业务边界：Catalog 唯一维护业务责任和业务授权决定，System 唯一维护源目标批准要求及实际 Grant。**正式接受办理的持久受理点**落在 System 的引擎访问控制领域，不要求 Catalog 单独提交一个“已接受”事实再通知 System。

这是对原先“正式接受依据完全归 Catalog”的已确认调整；它不允许 System 自行判定谁是业务负责人，也不产生可独立编辑的责任副本。业务确认仍由 Catalog 完成。

业务调用方向如下。此图只表示已确认的职责和调用方向，不证明故障时的协调正确性；原“持本地锁调用 System”实现设想不再视为充分方案，故障收敛见 26.23。

```mermaid
sequenceDiagram
    participant C as Catalog
    participant S as System 引擎访问控制
    C->>C: 持久保存精确业务决定
    C->>C: 锁定条目与来源，最终核验当前责任、决定和状态
    C->>S: 当前请求内交付受限的业务核验依据
    S->>S: 同一精确目标边界内核验批准要求并持久受理回执
    S-->>C: 原办理编号与不可延长的截止时间
    C-->>C: 结束核验锁定，按 System 回执呈现受理结果
    S->>S: 独立核验办理资格和接收主体，幂等写入 Grant
```

流程中的 System 写回执是正式受理点，不是 Grant 生效点。Catalog 已持久化的业务决定必须先于这一点存在；最终核验阶段不得依赖尚未提交的责任、业务决定或其他写入，不能在 System 受理后再要求 Catalog 第二次成功提交“接受状态”才能判断是否受理。Catalog 若需展示结果，应读取或按同次编号恢复 System 回执，不建立第二份可编辑受理状态。

System 回执只承载本次精确办理编号、目标与参数绑定、被消费的 Catalog 决定引用、批准要求版本、服务端受理时间、原截止时间及审计依据；不复制业务负责人、部门、责任历史或业务决定正文，不从这些引用推导接收方数据访问。回执不是账号凭据，不能用于不同目标、不同接收主体或新的办理。可信调用及当前主体核验仍须在具体 API 合同中定义，不能接受浏览器正文自报的“已批准”“接受时间”或责任身份。

核验期间复用 Catalog 条目先于当前来源绑定的锁顺序；退出、受理和 Grant 在 System 使用同一精确目标的事务边界。不得让 System 持锁同步反向调用需要这些 Catalog 锁的接口，形成循环等待。网络调用须限时，未知结果只能按原编号查询或同参重试，不自动放宽批准要求。

仅上述本地事务锁和限时 HTTP 调用，无法维持 Catalog 崩溃后至 System 受理提交之间的责任先后。业务方向不变，但实际协议必须先解决这一故障窗口，不能把响应超时当作对端请求被撤销。

#### 需要证明的结果

| 先后或故障 | 必须得到的结果 |
| --- | --- |
| 责任移交、弃用或重绑先于最终核验 | 旧决定不能完成新的正式受理 |
| System 退出先于正式受理 | 旧 Catalog 请求不能按已退出的批准要求新建回执 |
| System 正式受理先于退出 | 原同次、同参办理可在原 5 分钟窗口内完成，不续期 |
| System 受理提交失败 | 没有成功回执，也不产生 Grant |
| System 受理成功但响应丢失 | 根据原编号恢复原回执，不重新核验并延长已受理窗口 |
| Catalog 最终核验后的响应或连接失败 | 不抹去 System 已提交回执；它不依赖 Catalog 再提交一份接受状态 |
| Grant 已写入但响应丢失 | 返回原结果，不重复授予；访问仍另查当前有效性 |

#### 推进状态

- [x] 首次启用资格与退出时原办理窗口已确认，正式规范已更新。
- [x] 现有两模块的事务、锁和可复用边界已核对。
- [x] 正式受理回执归 System 已获用户确认；术语、正式规范及模块指导已同步，删除正式规范中的 Catalog 单独接受路径。
- [x] 26.23 局部等待的业务影响已确认，允许先持久登记待核清请求，再发送及按原编号恢复。
- [ ] 在内部协调底座之上贯通最小回执、可信请求及真实批准要求／业务决定；不发布未消费的占位 Permission 或未证明安全的 Grant 接口。
- [ ] T1/T2 覆盖上表两个方向的实际先后和故障；沿已有 System／Catalog owner 门禁注册，不用本方案图示代替事务证明。
- [ ] T4 在已有隔离 Online 体系验证实际跨模块调用、响应丢失重试和退出，不接管个人开发环境。

上一轮仅变更已授权的 4 个概念、规范和专题文档；并行会话的开发生命周期改动保持原样。未重启服务，未修改业务数据库或真实授权。方案当时尚未获确认，也未实现，不能计为授权能力上线。

上一轮验证：`make test-changed` 退出码为 0；变更发现结果仅要求平台 T0，已通过。当时未执行本方案的 T2 数据库并发验证或 T4 跨模块运行验收，相关实现尚待归属确认。平台门禁中的隔离测试夹具结果不代表本方案已经实现或通过在线验收。

### 26.23 核验事务中断与未知受理结果（已确认，分步实现）

本节来自落实 26.22 时的故障检查，不撤销已确认的权威分工。前一轮提出“Catalog 持条目／来源锁调用 System”时遗漏了锁在事务回滚或进程故障后释放的情况；不能把正常时序成立当成故障时也成立。

#### 证据与反例

现有 Catalog 的 `entry_update.go`、`entry_responsibilities.go`、`entry_governance.go` 和 `source_rebind.go` 均使用本地 GORM 事务及 `FOR UPDATE`。当前依赖 GORM v1.31.2 的 `Transaction` 在回调错误、panic 或提交错误时回滚；这些锁不是跨进程可恢复的持久协调事实。System 的事务只能在自己数据库内排序，不能发现 Catalog 锁是否仍存在。

因此以下交错不能被 26.22 原图排除：

1. Catalog 在本地锁内核验旧责任和决定，发出精确受理请求。
2. Catalog 进程故障或本地事务中断，核验锁被释放；System 尚未提交回执。
3. Catalog 恢复后的另一请求完成责任移交、弃用或来源重绑。
4. 已送达 System 的旧请求继续处理并提交受理回执。

这会让“责任变更先完成，却仍按旧依据新受理”发生。它是待实施协议的设计缺口，不表示当前已上线代码存在受理接口或已发生越权。短 HTTP 超时、追加一次 GET、比较两个时钟或自动重试都不能修复这一窗口。

#### 已确认：先持久记录本次待核清协调事实，再允许发送

用户已认可局部等待的业务影响。保留单一受理权威，通过最小、可恢复的协调事实防止上述交错：

- Catalog 在本地条目／来源事务核验后，持久记录同次精确业务决定对应的待核清请求，再发送；这一事实不是正式受理、批准要求副本或源数据 Grant。
- 对该请求影响的条目，责任变化、弃用和来源重绑在提交前必须核清请求结果；后台来源同步、责任失效对账等可能改变核验依据的写路径也必须消费同一协调事实，不能只拦截人工入口。其他条目、普通说明维护、既有有效 Grant 和模块 Ready 不受影响；批次同步受阻时保留 checkpoint，不跳过受阻对象伪装同步完成。
- 未知结果只按原编号核清：System 已受理则保留原窗口；System 若未受理，须在同一精确目标及请求边界持久关闭原请求，拒绝后来送达的原编号请求。一次查询“暂时没有回执”不等于关闭成功。
- Catalog 确认 System 的受理或关闭结果后，解除本地待核清状态。Catalog 进程退出不删除它；System 不可达时，不仅因超时或过了 5 分钟就删除它。
- System 的“关闭原请求”与“正式受理原请求”必须竞争同一边界，只有一个结果；关闭先完成的，延迟受理请求被拒绝；受理先完成的，关闭返回原受理结果，不取消已受理办理。重复关闭、受理和查询不得更换参数或刷新窗口。原编号须绑定 Tenant、精确目标、参数及可信调用来源，不能换一个目标复用。它是协议的最小事实，不是通用任务调度器或第二套业务审批。
- System 的明确退出仍可在 Catalog 不可达时执行；退出版本变更阻止旧要求的新受理，Catalog 恢复后再核清原请求。退出前已受理的同次办理仍按原窗口继续。

**已确认的业务影响：** 当 System 不可达且该条目确有未核清的受理请求时，暂缓提交该条目的责任变更、弃用或来源重绑，直到 System 核清并封闭旧请求。无待核清请求的条目不新增这个依赖，不阻塞整模块启动；既有合法读取不被此状态撤销。

实施顺序为：先完成内部持久协调事实、System 受理／关闭仲裁和 Catalog 写路径保护，再贯通真实业务决定、批准要求、可信机器调用及恢复消费者。底座不对外发布受理或 Grant API，不新增未消费的 Permission；底层事务测试不代替完整跨模块授权验收。

#### 后续验收要求

- T1：同次请求参数不可变，未知、关闭、受理和实际生效分开；不存在回执与已持久关闭严格区分。
- T2：实际重现核验进程／事务中断后锁释放，证明待核清事实仍在；覆盖责任变更与受理／关闭的两个方向、延迟旧请求及响应丢失。
- T4：在已有隔离 Online 体系注入 Catalog 崩溃、System 不可达及恢复，证明只阻止受影响条目的新变更，不阻塞 Ready 或已有合法访问。

上轮仅同步规范，未修改生产代码、业务数据库、真实授权或服务状态。本轮已在用户确认局部等待后开始实现内部协调底座；业务数据库、真实授权和开发服务仍不改动。

#### 上轮验证记录（规范调整，不计为协调协议实现）

- `make test-changed` 首次在前置检查中因未注入两模块测试 DSN 退出，没有执行测试。通过 `bash scripts/infra/status.sh` 核对当前 PostgreSQL 实际端口 `25432` 后，分别注入 `addp_iam_test` 与 `addp_test` 的测试连接重跑，最终退出码为 0。
- 自动发现命中 Catalog 和 System，平台 T0、两模块 Go T1、Catalog 前端 94 项单元测试及生产构建、System 前端 90 项单元测试及 20 项独立浏览器测试和生产构建、两模块 PostgreSQL T2 均通过；现有标准入口及 CI 自动发现已经覆盖，无需另建测试入口。测试仅操作规定测试库，不重启开发服务。
- `git diff --check` 通过。以上结果证明现有门禁通过；本节协调协议没有实现，未执行其新增故障场景或 T4 验收，不计为受理／Grant 链路完成。并行会话的生命周期脚本及规范改动未修改、未归为本轮实现。

#### 本轮内部协调底座与后续清单

- System：迁移 `000167` 保存同次请求的不可变 `accepted/closed` 结果；请求编号和精确结构化目标双重事务串行化，结果及最小审计一起提交。仓储拒绝非事务调用和无核验回调的新增受理；原窗口来自数据库墙钟，重试不重算。
- Catalog：`fulfillment_checks` 保存发送前精确请求及核清时间；数据库触发器覆盖责任、来源及治理依据写入。普通业务名称／说明编辑保留相同责任行身份，仍允许执行。业务责任建立与完成编目相互独立，不额外要求条目达到已编目状态；新增办理仍须由后续可信生产者核验当前责任及批准要求，不能把底层可登记当作具备业务批准资格。
- 测试：新增同参恢复、受理／关闭竞争、回滚、期限、历史不可改写、持久待核清保护、条目竞争及来源同步 checkpoint 回滚用例，已登记到既有两模块 PostgreSQL 门禁。T0-T3 继续由 `make test-changed` 自动发现，不另建 CI 路线。
- 当前边界：底座没有 HTTP 生产者、可信核清消费者或真实 Grant 写入；测试中的局部核清只是夹具，不代表已经验证跨模块受理。批准要求退出与正式受理的真实竞争、进程崩溃／失联 T4 尚待后续贯通。
- 来源同步仍沿用既有整批事务：批内某条目待核清时整批回滚、checkpoint 不前移，恢复后重试原批；不能跳过该条目并假装已消费。其他条目的人工维护及独立 owner 同步不使用全局等待锁；不把这点误报为同一 feed 内完全无队头影响。

后续优先贯通真实业务决定、版本化批准要求和可信机器请求／恢复消费者，再开放明确 Permission 保护的实际办理入口。完整授权交付必须包含退出、未知结果恢复及隔离 T4，不能用本轮底层测试代替。

#### 本轮验证记录（2026-10-01，内部协调底座）

- 首次 `make test-changed` 在新增 System PostgreSQL 用例中失败：引擎夹具未指定 `system.engines`，错误访问了未限定 Schema 的 `engines`。已修正夹具，未改变生产 Schema 或权限；通过标准入口 `bash scripts/test/system-iam-postgres-gate.sh --package engineaccess` 专项重跑通过。
- 最终重新运行 `make test-changed`，退出码为 0：平台 T0、Catalog／System Go T1、Catalog 前端 94 项单元测试及生产构建、System 前端 90 项单元测试、20 项独立浏览器测试及生产构建、两模块 PostgreSQL T2 均通过。新增用例已由标准入口执行，不依赖手工挑选用例；既有 CI 模块注册覆盖本轮变更，无需新增 workflow。
- 新增 PostgreSQL 验证包括：`000167` 前向迁移与重复执行；受理／关闭并发只有一个结果及审计；迟到受理、同参恢复与原窗口；核验失败和事务回滚；数据库墙钟期限；Catalog 待核清事实在后续事务回滚后保留；责任／来源／弃用变更保护；未完成编目的责任可独立建立；普通说明编辑及相同责任身份的快照刷新；来源同步整批回滚与 checkpoint 保持、核清后原批重试。
- `git diff --check` 通过。测试只使用 `addp_test` 和 `addp_iam_test`；未重启开发服务，未操作业务数据库、真实 Grant 或源端数据，未提交代码。并行会话的开发生命周期改动未修改、未归为本轮实现。
- 未运行：真实跨模块受理／Grant 链路及进程崩溃、System 失联／恢复、批准要求退出竞争的 T4。当前尚无对应生产调用链，事务与夹具测试不能替代这些验收；后续需将完整链路纳入已有隔离 Online T4 体系后执行。

### 26.24 承接资格与批准要求版本（已确认，分步实施）

用户已确认：退出时指定的承接人是确保当前有资格承接的检查对象，不是该资源永久唯一审批人。交接后，其他同时具备独立批准 Permission 和有效引擎管理委派的账号也可办理；两项资格缺一不可，不能单凭承接身份放行。承接账号只进入交接审计，不进入批准要求的可编辑白名单。

本轮先将最小版本化批准要求接入内部受理仲裁：缺失要求不是独立批准许可；新 Catalog 受理必须精确匹配当前 `catalog` 要求及版本。退出采用新的 `independent` 版本，不改写之前已受理结果；重新启用也使用新版本，原请求重试不得自动重新接管。要求变更与同目标受理使用同一事务锁边界，版本变更和审计同事务，失败无副作用。

内部初始化／变更仍要求显式资格核验回调，不替代真实 User AuthContext、独立治理 Permission、管理委派或承接资格的生产核验。未贯通真实调用前，不发布配置、退出或 Grant API，不生成占位 Permission；不向 Catalog 复制批准要求。真实业务决定、来源纳管、可信办理人来源及恢复消费者仍是后续同一链路的必需工作。

当前实现范围：

- 迁移 `000168` 新建 System 权威表；目标身份不可变，初始化版本为 1，后续必须逐次递增，拒绝删除与截断。完整结构化路径精确比较；摘要仅限制索引大小，不代替身份。没有存量自动初始化、责任副本或审批人白名单。
- 内部变更命令要求 owner 事务、租户 User 审计信息、原因、预期版本和资格核验回调；显式退出还要求承接账号，并仅将其写入同事务审计。
- 新 Catalog 受理消费当前模式及精确版本；已持久 `accepted/closed` 结果继续按原同次参数恢复，不因交接重写或延长。
- 仍没有生产配置／退出消费者、真实 User 资格判断或 Grant 写入；本节内部变更命令不能视为用户已可执行交接。

门禁：现有 `make test-changed` 自动覆盖 System T0-T3；新增 `AgainstPostgres` 用例由既有 `make test-system-iam-postgres` 的 engineaccess／migration 包自动发现。重点验证两个方向的退出／受理竞争、版本冲突、重新启用不消费旧请求、缺失要求拒绝、精确目标隔离与审计回滚，不把夹具中的资格回调当作真实业务批准或 T4 证据。

聚焦复核可使用 `bash scripts/test/system-iam-postgres-gate.sh --package engineaccess` 和 `bash scripts/test/system-iam-postgres-gate.sh --package migration --test engine-access-coordination`；沿用相同的显式 disposable DSN、数据库身份校验、跨进程互斥、Skip 拒绝及清理。默认 CI 仍发现完整包与全部 PostgreSQL 用例，不以分组结果替代全量通过。两个新增迁移用例的基线建立、前向升级和重复执行各自使用限时预算，不共用一个被基线耗尽的截止时间；授权办理窗口仍是 5 分钟，不受测试预算影响。

2026-10-01 本轮验证与交接记录：

- 聚焦 PostgreSQL `--package engineaccess` 通过，包含 14 个受理仲裁场景：要求缺失／独立模式拒绝、资格回调失败、版本冲突、重新启用、完整长路径隔离、两个先后方向的实际锁竞争、同参恢复、期限与不可变结果等。
- `--package migration --test engine-access-coordination` 通过，验证 `000167/000168` 的前向升级、重复执行、不可变保护、无默认批准安排及无 IAM 自动补权。完整迁移包初次运行的 `000167` 用例曾耗尽共享的一分钟测试预算；改为各阶段分别限时后，两个新增用例的聚焦复核通过。初次完整包已停止，不能计为完整迁移包通过。
- `make test-system-iam-runner` 通过（4 项），覆盖跨进程互斥、失败／终止释放与聚焦选择不改变默认完整发现；`git diff --check` 通过。
- 本次 `make test-changed` 已执行公共 T0、Catalog Go／前端（94 项）及 PostgreSQL；到 System Go T1 时失败。运行过程中另一组运行节点 IP 改动加入 `000169`，引起最新迁移版本断言和 `host_node_ips` 非空约束相关注册测试失败。已按实际清单同步版本断言为 169，未接手或覆盖并行注册实现；共享工作区全量门禁尚未重新通过，待并行改动稳定后再运行。运行过程中出现新共享依赖改动，早先通过的门禁也不代表最新全工作区已完成验证。
- 单独 `make test-system-frontend` 执行通过（90 项单测、20 项浏览器测试及构建）；其后并行前端改动仍需由对应改造的最终门禁覆盖，不把此次快照结果当作最新工作区的全量结论。
- 未运行真实授权／退出 API、Grant 写入及跨进程故障 T4：当前尚无对应生产链路，不以私有回调或数据库夹具冒充业务批准资格和完整授权验收。本轮没有重启开发服务、调整开发库状态或修改源端数据。

下一步优先在此底座上贯通真实业务决定、可信办理账号来源及当前资格核验，并覆盖责任首次纳管与明确交接。公开办理入口和真实 Grant 必须等该链路及恢复消费者共同具备后再发布；共享工作区全量门禁另需先在并行改动收口后重新验证。

### 26.25 只读核清路径与真实身份链路（2026-10-01，分步推进）

用户要求不等待、不接手并行的运行节点 IP 改造，继续本专题工作。本轮只涉及源授权受理核清及对应文档；不重启开发服务、不修改业务库或源端、不发布真实授权 API。

已完成的内部只读核清：

- `engineaccess.Repository.readFulfillment` 使用独立只读事务，拒绝复用受理写事务，避免将自身未提交结果作为恢复证据。
- 查询与受理／关闭共用一个完整绑定匹配实现；精确比较租户、Engine、机器调用主体、完整结构化目标、业务决定引用、批准要求版本、接收主体、动作及原绝对到期时间。
- 查询不取仲裁锁、不创建受理或关闭结果、不修改要求或追加办理审计。未找到与绑定冲突、数据库错误分别保留；未找到后同次请求仍可能正常受理。
- 已提交的 `accepted/closed` 结果按原参数返回；退出、更改批准要求及到期不改写历史，不重算五分钟窗口。恢复历史结果不等于当前具备读取或新 Grant 写入资格。
- 仍是内部仓储，不是公开核清 API；Catalog 尚没有据此自动解除待核清保护的可信消费者，不以测试中的局部时间戳更新替代该消费者。

测试与 CI：新增 3 个 PostgreSQL 子场景，由既有 System engineaccess 包的 `AgainstPostgres` 用例自动发现，现有 `make test-system-iam-postgres` 与 `release-and-t2-gates.yml` 已覆盖，无需新增入口或 workflow。执行 `bash scripts/test/system-iam-postgres-gate.sh --package engineaccess` 通过，共 17 个场景；新增验证覆盖完整绑定冲突、查询无副作用、未提交不可见及不等待受理锁、退出后原窗口过期仍可查询、过期关闭结果保留。专项结果不替代共享工作区全量门禁或 T4。

最终复核：上述 PostgreSQL 专项再次通过，包含查询取消时保留原错误且不返回结果；`make test-platform` 完整退出码为 0，覆盖现有测试／CI 登记、依赖一致性、启动边界、授权 Manifest 及 Swagger 路由覆盖等公共门禁；`git diff --check` 通过。平台一致性通过不等于 System 全包 T1、全部 PostgreSQL 历史迁移或真实跨模块授权 T4 已通过。

后续可信身份链路的现有能力盘点：

| 现有能力 | 可以复用的部分 | 不能据此推导的能力 |
| --- | --- | --- |
| System `/auth/context` 与模块认证中间件 | 用户交互时取得可信 User、Tenant Membership、授权版本及实际 Permission | 浏览器正文中的操作者 ID 不是可信来源；不得保存原始 User Token 供后台重放 |
| System IAM 当前资格核验及引擎管理委派 | 当前账号、成员关系、角色 Permission、委派、到期与授权版本事实 | 业务责任或委派本身不代替独立确认／办理 Permission |
| Execution Authorization 不可变来源核验 | 从 owner 持久事实取得操作者来源，再回查当前 IAM 的原则 | 其具体父子 execution 表及任务租约契约只适用于执行；不能为共享授权伪造任务或直接套用该凭据 |
| Catalog 待核清保护 + System 原回执查询 | 核验提交后发送、跨故障保留保护、按同次参数查询原结果 | 还没有真实业务决定生产者、可信 provenance 查询、核清消费者或 Grant |

接下来的优先实施顺序保持原已确认边界：先落实 Catalog 自有持久业务决定及从真实 User AuthContext 派生的操作来源，再由 System 独立核验当前办理资格。需要读取 Catalog 核验依据时，在进入 System 目标锁之前完成，依靠已持久待核清保护保持依据；不能持 System 目标锁同步反向调用 Catalog。随后贯通受理与只读核清消费者、首次责任纳管及明确退出，最后发布实际 Grant 与执行消费。具体接口与 Permission 必须随真实消费者同次发布，不生成占位 Permission、授权副本或新身份凭据路线。

本轮未运行共享工作区全量 `make test-changed`、真实跨模块核清或授权 T4：前者上一轮已有并行改动相关失败记录，本轮按用户要求不接手该实现；后两者尚无生产调用链。新增确定性 PostgreSQL 用例已经本地执行，不留给另一台 macOS 才首次发现问题。

### 26.26 原操作人身份底线与并发核验（2026-10-01，分步推进）

本轮继续 26.25 的内部链路，不接手并行运行节点 IP 改造、不重启或停止服务，不操作业务库或源端。

已实施：

- System 内部办理请求不可变绑定原操作账号的 User Principal、Tenant Membership 和授权版本，精确参数比较及审计消费同一绑定；bigint 身份字段用十进制字符串编码。不新增身份实体、Token、角色或责任副本，不保留缺失身份字段的兼容路径。
- 首次受理由 System 自身事务核验当前 User、成员关系、租户、授权版本和成员到期时间；进入请求／目标边界前锁定身份，业务核验后重新检查数据库墙钟。即使业务核验夹具返回通过，机器主体冒充用户、授权版本过时或成员到期仍不得受理。
- 关闭未受理请求及同参历史核清不要求原操作账号继续有效，避免账号失效导致待核清保护永远无法解除。返回历史回执不恢复新办理资格、不刷新原窗口；实际 Grant 仍需未来消费者核验当前资格。
- 复用 IAM Repository 持有身份事实及现有管理委派的身份判断，不改变管理委派写入的锁强度、Token 有效期检查或 Permission 守卫。

并发测试发现并修复的根因：受理若对 Principal 持 `FOR UPDATE`，而明确退出已持目标锁、审计插入又等待 Principal 外键引用检查，就可能循环等待。只读身份核验改为 Principal → Membership → Tenant 的 `FOR SHARE`，既阻止状态与版本写入，又与审计 `KEY SHARE` 共存；不通过跳过竞争用例或弱化身份核验绕过。回归同时验证同账号不同目标可以独立办理，三类身份行在受理提交前不能取得写锁，提交后锁释放。

测试与 CI：沿用 System owner 的 T0/T1/T2/T3 门禁。新增单元用例和 3 个 PostgreSQL 子场景由现有自动发现覆盖，无需新增 Make／workflow 路线。`bash scripts/test/system-iam-postgres-gate.sh --package engineaccess` 已通过，共 20 个子场景（含原有退出／受理双向竞争、不可变历史及故障恢复）；门禁核对测试库身份、拒绝 Skip 并清理自有 schema。进一步执行 `make test-module MODULE=system`，整体退出码为 0：平台 T0、System 全包 Go T1、前端 91 个单元用例、20 个 Playwright 用例及构建、完整 IAM／OAuth／API／Migration／engineaccess PostgreSQL T2 全部通过。完整 T2 再次覆盖本轮最终版本，包括真实非主键更新在受理提交前被阻塞，提交后锁释放；不是只证明 `FOR UPDATE` 请求会等待。

未运行共享工作区全量 `make test-changed`，本轮明确 owner 为 System，并按用户要求不接手并行 IP 实现；完整 System 门禁通过不代表其他 owner 全量门禁通过。真实跨模块授权 T4 尚无生产调用链，未运行、不计为通过。最终 `git diff --check` 与本轮 Go 文件格式检查通过，无重启、提交或业务源端写入。

仍未完成：真实 Catalog 业务决定生产者、从已验证 User AuthContext 派生并持久化的可信操作来源、System 对 owner 来源的验证、独立确认／办理 Permission 和目标委派消费者、核清消费者、实际 Grant 与执行消费。当前绑定字段和身份检查不构成可信来源证明，也不能把测试回调当作业务批准；因此仍不发布公开受理、批准要求变更或 Grant 接口，不新增占位 Permission。

下一步优先贯通 Catalog 自有持久业务决定及可信操作来源，再让 System 消费 owner 持久事实与当前办理资格，保留目标锁内禁止网络调用的约束。该链路和核清消费者齐备后才开放实际授权；不先靠页面按钮或服务账号权限伪装完成。

### 26.27 真实业务决定的最小闭环与业务选择（2026-10-01，已确认）

本节记录 26.26 后核对的生产者与可复用流程。用户随后确认下述普通只读自用和期限选择，现已同步授权上下文规范 5.5 和术语表；确认方案不等于真实业务决定、跨模块受理或源读取 Grant 已上线。最初这一轮只更新专题，后续实施证据单独追加；并行运行节点 IP 改动、服务生命周期和业务数据保持不动。

#### 已核实的实现事实

| 现有能力 | 可复用部分 | 不可混同的边界 |
| --- | --- | --- |
| Asset 的 `ApplicationService` 与 `GrantFulfillmentService` | 已上架资产的申请、审批和 owner 履约分工 | 当前审批通过只接入 `application` 资产，履约目标是 Workbench `data_application`；不是数据库表共享的业务决定。其默认 30 天和相对期限不能直接套用到源授权 |
| Catalog 责任聚合 | `business_owner` 精确责任关系，以及条目、来源与变更的现有并发边界 | 每个条目最多一名业务责任人；数据管理员、技术维护者和编目维护人员不是第二名业务确认人，不能用“找另一个负责人”暗中改变责任模型 |
| 模块认证与 User AuthContext | 已验证的 User Principal、Tenant、Tenant Membership、授权版本，以及实际 Permission | Catalog 当前编目审计的 `UpdateEntryActor` 只有类型与 ID，不是源授权办理的完整可信来源。不能把浏览器传入的 ID 补进结构后就声称可信，也不保存 User Token 或可编辑 Role 快照 |
| Catalog 待核清事实与 System 不可变回执 | 已有内部故障协调和身份底线，可供真实消费者接入 | 待核清不是业务批准；System 回执不是 Grant，测试回调不是当前业务责任核验。实际生产者、可信来源消费和自动核清尚未贯通 |

证据入口：`asset/backend/internal/service/application_service.go`、`asset/backend/internal/service/grant_fulfillment_service.go`、`catalog/backend/internal/service/entry_update.go`、`common/authorization/auth_context.go`，以及 26.23—26.26 的实现与验证记录。这是源码核对，不是运行中的真实授权验收。

#### 下一轮最小闭环建议

不另建通用审批引擎，也不把尚未上架的数据库表强制包装为 Asset。沿已确认的 Catalog 业务入口，先打通“有效业务责任人明确确认一次精确只读共享 → 有资格的办理人提交本次办理 → System 正式受理 → 原结果核清”，再接入实际 Grant 与 Manager / SQL 的真实消费。

每次业务决定绑定一个当前条目及其精确源目标、接收账号或项目组、`read`、明确的绝对到期时间和用途原因。确认人的身份由真实交互中的已验证 User AuthContext 派生；Catalog 保存自己拥有的决定与责任依据，不复制 System Grant。尚无独立申请流程时不伪造申请人，不为这一闭环先新增申请单状态机；Asset 申请仍归 Asset，其后接入时再明确消费业务决定的契约。

必须分别记录并核验三种身份：

- **接收主体**：实际拟获得数据读取权限的账号或项目组。
- **业务确认人**：当前有效业务责任人，且具备独立业务确认 Permission；决定来源由 Catalog 自有持久事实证明。
- **授权办理人**：当前具备独立办理 Permission 与目标引擎有效管理委派的 User；原操作者来源由真实办理交互派生并持久绑定，不从业务确认人的身份自动推导。

三者可能重合，但不因为重合而省略任何资格。机器调用主体只负责跨模块调用，不能代替确认人或办理人。System 的当前身份底线仍须与 owner 来源验证、当前功能及委派检查共同消费；不能只增加新的字段、表或页面按钮就勾选闭环完成。

#### 用户已确认的两个选择

1. **普通只读的自用确认**。允许业务责任人明确确认向自己授予只读，也允许其确认向自己当前所在的项目组授予只读；这必须是一次显式共享决定，不是指定责任后自动得权。同人还需分别满足确认与办理资格，完整记录自用或项目组受益关系，并保留当前主体、安全保护和 Explicit Deny 检查。不允许敏感原值例外、写入、DDL 或其他受独立复核规则约束的动作自批。此处包括无独立申请单时的直接自用共享，不能通过“不存在申请人”规避记录边界。
2. **首版普通只读授权的期限上限**。每次必须填写未来的绝对到期时间，不提供默认值或永久选项，但暂不设全平台统一的固定天数上限；到期不得自动续期。当前主体资格、规则撤销及安全条件持续约束实际访问。不得照搬 Asset 默认 30 天或把 System 五分钟自动办理窗口当作数据访问期限；引擎管理委派仍遵守其既有成员有效期约束。

上述选择已确认，真实生产者与消费者按最小闭环实施；已有合法访问不因此暂停或改权，不增加占位权限或第二套批准底座。26.18 的 24 小时建议不是已确认规则，本次明确的“无全平台统一天数上限”取代该待定建议。

#### 验证与 CI 计划

确认后，真实 Catalog producer 和 System consumer 必须随独立 Permission、路由守卫、审计、迁移、Swagger 及可信机器调用一起实施。T1 覆盖身份来源、参数绑定及自用规则；Catalog/System 现有 PostgreSQL T2 入口覆盖当前责任失效、变更顺序、待核清持久保护和回执不可变；跨模块真实调用、响应丢失及 Grant 的 C/D 访问边界归隔离 T4。沿现有 owner 自动发现和 Online 标准体系扩展，不增加占位 suite，也不接管用户开发服务。

本轮仅修改专题说明，实施前识别为平台 T0；不新增测试入口或 CI 依赖。`make test-platform` 完整退出码为 0，覆盖平台生命周期夹具、共享前端、测试／CI 登记、依赖一致性、授权 Manifest 和 Swagger 路由覆盖；`git diff --check` 通过。未运行包含并行改动的全工作区 `make test-changed`，也未运行真实业务决定、跨模块受理或 Grant 的 T2/T4，相关生产调用链尚未实现；不以此前完整 System 门禁或此次 T0 代替这些业务验证。

### 26.28 普通只读业务共享确认生产入口（2026-10-01，已实施，授权闭环未完成）

本轮按 26.27 已确认的自用与期限规则推进，先发布真实业务决定的生产入口，不以私有回调或新增字段冒充跨模块授权完成。未重启或停止开发服务，未修改业务库、源端或并行运行节点 IP／日志实现，未提交代码。

已实施范围：

- Catalog 新增 `POST /entries/:id/sharing_decisions` 和 `GET /entries/:id/sharing_decisions/:decision_id`。当前 Tenant User 必须同时具备条目读取和独立 `catalog.sharing_decision.create`；新确认还必须是当前有效业务负责人。编目维护、资源盘点、责任身份或引擎管理委派不能替代该功能 Permission。
- 每次明确一个接收账号或项目组、未来绝对到期时间及用途，固定动作 `read`。允许确认人本人或其当前项目组受益，并记录相应关系；不提供默认期限、永久选项或统一天数上限。身份来源为已验证 User AuthContext，不接受正文中的确认人、路径、动作或时间证明，不保存 User Token。
- 首条源目标范围仅为 Meta 已扫描的数据库 `table`。只动态读取租户范围的 Meta 当前 item／完整祖先链及 System 当前引擎描述，校验指纹、父节点、Engine 和层级声明，复用 `common/resourcetree` 构造保留原名的完整路径。不连接源库、不读取数据值、不拆 `full_name`，不支持的目标明确拒绝而不扩大成引擎授权。
- `catalog.sharing_decisions` 仅保存 Catalog 自有不可变决定历史及来源／责任依据，绑定条目版本、来源绑定及版本、责任关系 ID、确认人 Principal／Tenant Membership／授权版本、接收主体、完整路径、用途和期限。新确认、聚合版本、审计和投影任务同事务提交；等待条目／来源／责任锁后重新检查版本及权限有效时间。
- 同 `decision_id`、同确认人及同业务参数重试返回原记录，不重复确认、递增版本、追加审计或延期；换参返回冲突。历史读取只返回当前仍可见条目下本人的原决定，不要求仍是业务负责人。责任移交影响新的确认，不改写旧历史。
- System 迁移 `000170_catalog_sharing_decision_permission` 只登记独立 Permission，不为内置角色补权，不创建 Role Assignment／Grant，不递增账号授权版本；权限管理的中英文资源名称同步补齐。Catalog Swagger 已生成并覆盖新路由。

边界仍保持：业务决定不是正式受理回执或实际 Grant。当前没有对应前端按钮，也没有可信 System 消费、自动受理／核清、首次批准要求纳管、明确退出或真实源授权写入入口；本轮不创建待核清事实、不冻结责任、不改变已有合法访问。System 后续仍须独立核验办理人当前资格及目标委派，消费 Catalog 权威来源，而不是信任浏览器传回这份 JSON。

测试与 CI 登记：

- 新增 T1 覆盖本人／项目组受益、期限与微秒精度、身份及独立 Permission、核验期间权限到期、责任移交后的历史与重试、来源变化、不支持的目标及外部核验失败。HTTP 客户端契约测试核对真实 Meta／System 路径和 Tenant Service Token，确认仅发出元数据 GET。
- Catalog PostgreSQL T2 覆盖决定不可 UPDATE／DELETE／TRUNCATE、路径基础约束、同参恢复、责任变更后的新确认拒绝，以及决定插入后的审计失败使决定与条目版本一起回滚；API T2 覆盖守卫、严格请求、201／200、409、跨租户及他人历史 404。
- 新增用例沿现有 owner 自动发现；Catalog T2 已纳入 `scripts/test/catalog-postgres-gate.sh`，System `AgainstPostgres` 自动发现迁移 170 前向升级／重复执行测试，既有 `release-and-t2-gates.yml` 编排已覆盖。无新增测试库、服务依赖或旁路 suite，本地仅使用 `addp_test`／`addp_iam_test`。

本轮验证结果及未通过项：

- 已执行 `make test-module MODULE=catalog`，但公共 T0 在 `scripts/test/infra-port-resolution.sh` 失败：并行新增的 `addp-runtime-log-api` 未被该夹具提供端口证据。未绕过为全模块通过；按现有 `module-gate.py` 注册计划继续执行 owner 独立步骤。
- Catalog Go T1、`make test-catalog-frontend`（94 项及构建）和 `make test-catalog-postgres` 全部通过；最后新增到期／审计回滚测试后再次执行 Catalog Go T1 与 PostgreSQL T2 通过。`bash scripts/swagger/check-route-coverage.sh catalog` 通过，覆盖 37 个路由方法。
- System 全包 Go T1、`make test-system-frontend`（91 项单元测试、20 项浏览器测试及构建）通过。完整 `make test-system-iam-postgres` 已执行，迁移 170 新用例通过，整体在现有 `TestRunnerAgainstPostgres` 的 System Permission 总数断言失败（实际 141、预期 140，与并行新增 `platform.module_log.read` 对应）；后续 engineaccess 包尚不能计为该次完整门禁通过。
- 随后通过标准聚焦入口 `bash scripts/test/system-iam-postgres-gate.sh --package engineaccess` 再次验证，20 个受理仲裁子场景通过，包括身份／成员到期、实际锁竞争、同参恢复、只读核清、窗口上限及历史不可变；该结果只证明内部仲裁底座，不代替完整 System T2 或真实 Catalog 消费。
- `make test-authorization` 的 Manifest／内置角色检查通过，但整体授权覆盖报告因并行 `platform.module_log.read` 尚无生成后的 Swagger 路由引用而失败。尝试 `bash scripts/swagger/gen-swagger.sh catalog system`，Catalog 成功，System 在并行 `module_runtime_log_handler.go` 中未导入的 `models.ErrorResponse` 类型处失败；未改动该实现或伪造 Swagger。
- 未执行全工作区 `make test-changed`，不把上述 owner 结果推广到并行改动；未运行真实跨模块授权、响应丢失恢复或 C／D 源数据访问的 T4，因为实际消费与 Grant 链路尚未实现。所有失败和未运行项均不计为通过。
- 本轮相关文件 `git diff --check`、Go 格式及 JSON 解析检查通过；全工作区检查曾发现并行 Transfer 启动文件的行尾空白，未修改该文件，也不据局部结果宣称全工作区检查通过。

下一步优先让 System 从 Catalog 持久决定和当前责任取得可信核验依据，接入独立办理 Permission／目标委派以及正式受理和只读核清消费者；不能将本轮“确认已记录”直接显示成“授权已生效”。该消费链路及故障恢复齐备后，再开放实际 Grant 与业务页面。公共 T0、System 日志 Swagger 及 Permission 总数断言待并行改动收口后复核，不影响继续梳理已确认授权协议。

### 26.29 普通只读共享支持明确的长期有效模式（2026-10-01，授权闭环仍未完成）

用户提出稳定组织的授权可以长期有效后，确认采用显式有效期选择。本节取代 26.27—26.28 中“不提供永久选项”的阶段性限制，不追溯改变旧决定或旧回执的到期时间。

#### 规则与实现范围

- 每次必须明确 `expiry_mode`：`at_time` 为指定到期时间，须填写未来绝对日期且无统一天数上限；`until_revoked` 为长期有效（直至撤销），日期省略或为 `null`。遗漏模式及矛盾参数拒绝，不以漏填日期、无穷时间或日期哨兵表示永久。
- 长期有效不代表永久拥有权限；当前身份、组织成员关系、功能权限、规则撤销、Explicit Deny 及安全条件仍约束实际访问。临时接入、引擎管理委派、敏感原值复核、写入、DDL 和 Token 期限不随本次变更放宽。
- Catalog 的业务决定和 System 的内部受理请求都绑定模式与日期；同次重试不能改成另一模式、延长期限或刷新受理时间。纯值校验、UTC 微秒规范化及比较由 `common/authorization` 唯一提供，责任、业务决定和授权事实仍分别归原 owner。
- System 五分钟自动办理窗口不变：指定日期时为 `min(原受理时间 + 5 分钟, 到期时间)`；长期有效时为 `原受理时间 + 5 分钟`。该窗口约束系统自动办理，不是人员接受动作的操作倒计时。
- Catalog 在既有 Schema 事务及排他锁内将有限期历史标记为 `at_time`，保留原到期时间、审计和版本，并恢复不可变触发器。System 新增向前迁移 `000172`，在同一事务和排他锁内补齐模式及请求绑定，保留原期限与受理窗口；不修改旧迁移，不创建 Grant，也不改动 IAM 授权版本。
- 已更新正式授权规范、术语表、Catalog 实现规范及模块说明；Catalog API 的双语 Swagger 同步模式及可空日期。仍无业务共享前端入口、真实 System 消费与 Grant 链路，本轮不能用来证明源数据访问授权已生效。

#### 验证与测试体系

新增或扩展的断言包括：遗漏模式、矛盾日期、明确长期有效、期限及模式重试冲突、独立功能权限、五分钟窗口、不刷新窗口、数据库不可变保护及保留有限期历史的迁移。新 System 迁移用例由默认 `AgainstPostgres` 自动发现，并纳入现有 `engine-access-coordination` 聚焦门禁及其脚本回归；Catalog 扩展现有门禁用例，Common 由既有授权与 Go T1 门禁覆盖，不新增旁路 CI。

本轮验证结果：

- `make test-module MODULE=system` 完整通过，包括公共 T0、System Go T1、前端 91 项单元测试和 24 项浏览器测试、构建、完整 IAM PostgreSQL 门禁以及运行日志 T2 门禁。新 `000172` 的历史保留、重复迁移和不可变保护，以及长期有效的五分钟受理窗口均已验证。
- 首次 `make test-module MODULE=catalog` 在公共 T0 的 openGauss 启动脚本回归中超时，未执行后续 owner 步骤；同一公共 T0 后续在 System 标准入口中复跑通过，未修改 openGauss 实现。Catalog 通过标准模块计划分别完成 Go T1、`make test-catalog-frontend`（94 项测试及构建）与 `make test-catalog-postgres`；不能将首次命令记为通过。
- `make test-authorization`、Common 完整 Go T1、`bash scripts/swagger/gen-swagger.sh catalog system` 及 Swagger 路由覆盖通过。Catalog 最后一次 Go T1 同时验证模式必填、两种枚举及日期的 `date-time`／`x-nullable` 契约。26.28 的授权覆盖、System Swagger 和公共 T0 阶段性阻塞本轮已复核解除。
- 初次 Catalog T1 暴露共享 SQLite 测试夹具缺失新模式，已修正并完整复跑。历史 Schema 迁移测试在同一连接上改表后触发 pgx 缓存查询布局冲突，测试连接改用保留类型编码且不缓存描述的 `DescribeExec`；不修改生产连接策略。中途简单协议尝试导致 JSON 编码错误，已移除；完整 Catalog PostgreSQL 门禁随后通过。
- 未执行涵盖全部并行变更的 `make test-changed`；未运行实际 Catalog→System 消费、Grant 写入、响应丢失恢复或 C／D 源数据访问 T4，因该闭环尚未实现。局部门禁结果不能推广为整个工作区或真实授权闭环通过。

只使用 `addp_test`／`addp_iam_test`，不修改开发业务库、不重启服务、不提交代码，不接手并行日志或 IP 改动。

下一步仍优先贯通 System 对 Catalog 持久决定与当前责任的可信消费，以及独立办理资格、正式受理和核清恢复；再开放实际 Grant 和业务页面，避免只完成期限选择就宣称授权闭环完成。

### 26.30 持久业务决定的当前本地依据核验（2026-10-01，跨模块消费未完成）

本轮先补齐 Catalog 自有事实的核验，保持历史读取、当前依据和正式受理三个概念分离：

- 内部 `lockCurrentSharingDecisionBasis` 只接受租户、条目与决定引用，在已打开的 Catalog 事务内读取不可变决定；按条目→当前来源→原责任关系锁定，核对 active DataItem、未弃用、原 Meta 来源绑定及版本、原 active 业务负责人关系 ID 和确认账号。
- 普通业务名称／说明编辑导致聚合版本增加，不使决定单凭版本差异失效；版本倒退拒绝。原责任移交后，同账号通过新关系再次接任不能复活旧决定；来源变化或失效、弃用和决定到期都拒绝作为新的本地依据。
- 指定到期时间在锁定后由 PostgreSQL 墙钟判断；长期有效仍核验当前依据。不写入新的决定、审计、投影、待核清或受理状态，不延长期限，不调用远端模块。历史 GET 保留原读取语义，不用该核验替代历史查询。
- 普通业务确认的创建事务在写入决定与聚合版本后、提交审计前复用当前依据核验；失败回滚整个创建事务。未来准备消费者必须在持有这些锁的同一事务中提交待核清事实后才能发送，不以单独完成核验宣称故障协调已贯通。
- 本地责任 active 或缓存 VerifiedAt 不证明 System 身份和功能权限当前有效。可信机器调用、操作账号来源、实时源路径、独立办理资格、管理委派、接收主体与批准要求仍须由后续消费者分别核验；未发布新的 Permission 或实际授权接口。

验证：`make test-module MODULE=catalog` 完整通过，覆盖公共 T0、Catalog Go T1、前端 94 项测试与构建、PostgreSQL T2。新增单元测试覆盖 19 种当前依据情形及事务要求；既有 `TestPostgresSharingDecisionIsAtomicImmutableAndRetryable` 增加名称编辑、原责任失效和来源变化的事务回滚验证，均通过。沿用 Go T1 自动发现和已登记的 `make test-catalog-postgres`，不新增门禁或旁路 CI。

本轮未执行整个共享工作区的 `make test-changed`，未验证尚未实现的跨模块真实受理／Grant T4 链路；上述通过仅对应本轮 Catalog owner 范围。未重启服务、未修改开发业务库或并行日志／IP 实现。

下一步以这一 owner 核验为基础，落实持久待核清准备与 System 可信消费的同次绑定，再贯通受理、核清和实际 Grant；仍不能将本地核验返回的决定对象当作访问凭据。

### 26.31 持久决定到待核清请求的内部登记（2026-10-01，生产消费未接通）

本轮补齐 Catalog 内部事务登记原语 `prepareSharingFulfillment`，不是公开办理服务：

- 首次登记读取持久决定，按条目→来源→原责任锁核验当前依据；精确 EngineCatalogPath 必须与决定一致，包括节点原名、大小写和层级。接收主体、动作和期限仅从决定派生，不能传入另一份决定正文。
- 不可变请求绑定调用 Service Principal、原操作 User Principal／Membership／授权版本、批准要求版本、完整目标和决定引用。结构有效不等于身份可信；未来生产消费者仍须从真实认证上下文派生来源并独立核验当前 Permission、管理委派及接收主体。
- 同编号重试比较完整绑定并返回原记录和创建时间；不刷新期限、不重开已核清请求，也不把历史恢复当成当前资格核验。换参拒绝，首次登记失败整体回滚。数据库已有的待核清保护继续覆盖责任、来源和弃用写入，普通说明编辑仍允许。
- 原语只在调用方 Catalog 事务内工作，无网络 IO、无 System 受理时间／窗口副本、无 Grant；发送必须在事务提交后进行。未增加 HTTP 路由或占位 Permission，现有业务确认 API 不自动建立待核清事实，不冻结用户的正常编目操作。

新增 Go T1 覆盖完整绑定、换参、同参及已核清恢复、失效决定的新请求拒绝、错误引用和事务回滚；既有 `TestPostgresSharingDecisionIsAtomicImmutableAndRetryable` 增加实际登记后的 JSONB 同参恢复、责任／来源／弃用保护、普通名称编辑及嵌套回滚验证。沿用 Catalog 自动发现和已登记 PostgreSQL T2，不新增测试路线或 CI 占位。

验证结果：

- `make test-module MODULE=catalog` 在公共 `make test-platform` 阶段失败：并行运行日志工作中的 `scripts/test/system-runtime-log-gate.sh` 缺少 owned build COPY／ADD 输入登记。本轮没有改动该工作或将失败计为通过。
- 随后复用 `scripts/test/module-gate.py` 的 Catalog 标准计划（`plan_module(..., include_platform=False)`、`run_steps(..., dry_run=False)`），Catalog 完整 Go T1、前端 94 项测试与构建、既有 `make test-catalog-postgres` 全部通过，最终退出码 0。首次 Go T1 中新夹具的 SQLite attached-schema AutoMigrate 索引错误已按现有服务测试夹具方式修正；最终门禁覆盖修正后文件。
- 格式与本轮文档差异检查通过。没有执行全共享工作区的 `make test-changed`；公共 T0 未通过，生产跨模块 T4 未运行，不能宣称整体验收通过。

下一步优先贯通真实办理消费者的认证来源和 System 独立资格核验，再接入提交后发送、只读核清与关闭竞争；不能用内部测试参数或仓储回调代替可信跨模块调用。未重启服务、未写开发业务数据库或源端、未提交代码；真实跨模块 T4 和实际 Grant 尚未验证。

### 26.32 待核清请求的内部故障恢复消费（2026-10-01，真实 System 调用未接通）

本轮补齐 `reconcileSharingFulfillment` 的内部恢复流程，不开放授权、核清 API 或自动恢复任务：

- 只消费已经提交的 Catalog 原待核清记录，拒绝在调用方事务内运行；远端查询、关闭均在 Catalog 锁与事务之外进行。
- 先查询原结果；仅明确的权威“未找到”进入同编号、同参数关闭，超时、拒绝或其他错误不进入关闭。关闭可能返回竞争中已先提交的 `accepted`，不能假定返回值一定为 `closed`。
- 核对请求编号、Tenant 和完整绑定，包括原调用主体、User／Membership／授权版本、精确路径、决定、批准要求版本、接收主体、动作和期限；未知结果或任何错配都保留待核清保护。
- 验证原结果后，在条目→待核清记录锁内使用数据库墙钟填写一次核清时间。同参及并发恢复保留原时间；本地提交失败仍保留原请求，可以继续查询原结果。恢复不要求原账号、原责任或拟授权期限仍有效，不复活旧批准，也不建立 System 受理时间、截止时间或 Grant 副本。
- 共用严格类型解码和绑定比较，拒绝不完整／额外字段／拼接 JSON，保留大整数身份精度。集成测试发现并修复了规范化时间后仍以原 JSON 字节比较的错误：同一绝对时间不同 UTC offset 应相同，实际相差一微秒则拒绝；沿用共享有效期规范，不新增兼容字段或第二条时间路线。

新增 Go T1 覆盖受理／关闭结果、响应丢失、错配、失效历史、重复完成、禁止事务内远端调用、适配器参数隔离及本地取消后的同参恢复。新增 `TestPostgresSharingFulfillmentRecovery` 覆盖真正提交的准备记录、核清事务回滚后的保护、未知关闭结果保留保护、远端调用期间另一个事务可更新条目、两个完成事务竞争保留同一时间、核清提交后责任变更恢复，以及恢复后原责任失效的历史读取。

测试已纳入既有 `scripts/test/catalog-postgres-gate.sh` 和 `make test-catalog-postgres`；既有 `.github/workflows/release-and-t2-gates.yml` 的 Catalog PostgreSQL Job 调用同一入口，模块影响自动发现已覆盖本轮文件，无需新增 CI 路线。没有 API 契约、Permission 或 Schema 变更，不修改 Swagger 或创建 migration。

验证结果：

- `make test-module MODULE=catalog` 在公共 T0 失败：当时并行基础设施改动中的 `docker-compose.infra.yml` 存在重复 `extends`，`test-ontology-infra-config` 无法解析 Compose。本轮未修改该文件，不将公共门禁计为通过。
- 随后复用 `scripts/test/module-gate.py` 的标准 Catalog owner 计划（`include_platform=False`），最终完整 Go T1、前端 94 项测试与构建、完整 `make test-catalog-postgres` 均通过，退出码 0。追加本地取消恢复用例后，再通过同一标准计划单独重跑 Go T1，退出码 0；没有以旧结果覆盖新增用例。
- 初次 PostgreSQL 验证发现的本轮时间比较回归已修复；最终 T2 覆盖修正后的准备与恢复代码，拒绝意外 Skip，并清理新增场景拥有的 Catalog 测试 Schema。格式及差异检查通过。
- 没有执行全共享工作区 `make test-changed`，公共 T0 未通过；真实跨模块 T4、实际办理与 Grant 均未验证，不能宣称整体授权闭环完成。

下一步优先把原操作账号的可信来源、System 独立办理 Permission／有效引擎管理委派及 owner 持久事实核验接入同一真实消费链，再接入本轮恢复流程的可信 System 客户端和生产调度。当前只有内部接口及受控测试替身；恢复逻辑通过不等于已认证的跨模块调用已具备。未重启或停止用户服务、未写开发业务数据库或源端、未提交代码。

### 26.33 统一待核清准备与权威仲裁的精确目标边界（2026-10-02，生产授权闭环仍未完成）

本轮检查真实消费前的失败路径，发现 Catalog 内部准备原语只核对路径一致，而 System 仲裁另有限定：若双方接受的目标集合不同，可能先登记待核清、阻止责任或来源变更，随后却无法被 System 受理或关闭。为避免该问题，在首次登记前共用同一值边界：

- `common/authorization.EncodeSharingTarget` 唯一校验带结构根的有效叶子路径、正 int64 Engine ID、最多 64 个 segment（含根）、完整 JSON 最多 16 KiB；沿用 System 已有的内部限制，不改变引擎路径模型、Meta 扫描层级或现有业务确认 API。
- Catalog 准备和恢复绑定读取、System 受理／关闭及精确批准要求均复用该实现，删除 System 本地重复校验。不能消费的目标在首次登记待核清之前拒绝，不以先冻结再重试远端作为处理方式。
- 保留节点原名、大小写、空白和分隔符，无效 UTF-8 字符串明确拒绝，避免 JSON 编码悄然替换为同一字符。按完整编码后的字节数限制，不以字符数或编码前长度替代。
- 共享库只负责值契约，不核验来源存在性、身份、责任、Permission、委派或批准要求；没有新增公开 API、占位 Permission、Schema／migration、授权或后台任务。内部测试通过不证明跨模块调用已认证，也不证明 Grant 已生效。

测试沿现有 Common／Catalog／System Go T1 自动发现，Catalog PostgreSQL 和 System engineaccess PostgreSQL 使用已登记标准入口；`.github/workflows/platform-ci.yml` 的 `make test-go` 与既有 `release-and-t2-gates.yml` owner T2 已覆盖，无需增加旁路 CI。新增边界用例覆盖 64／65 层、16 KiB／超限、大 Engine ID、中文与 JSON 转义的实际字节数、无效 UTF-8、精确身份保留，以及 Catalog 拒绝后零待核清记录。正式实现规范和 Catalog 模块说明同步更新。

本轮验证记录：

- 通过 `scripts/test/module-gate.py` 的已登记 owner 计划运行 Catalog 与 System 完整 Go T1，退出码 0；本轮共享值校验的两组测试及全部子场景在 Common 完整 Go T1 的 verbose 输出中通过。
- `make test-catalog-postgres` 全部通过，包括待核清数据库保护、事务回滚、恢复竞争与历史读取。随后追加真实 PostgreSQL 的 65 层／16 KiB 超限目标用例，在不可变历史触发器保持开启的情况下，证明拒绝准备后零待核清、责任仍可转移，整个夹具事务回滚；追加后再次通过 Catalog 完整 Go T1 和完整 PostgreSQL T2，退出码均为 0。没有使用开发业务库；测试只使用允许的 `addp_test`，由标准入口管理测试 Schema。
- Common 完整 Go T1 与重新执行的 `make test-go` 均未通过：当前内置角色 Manifest 版本实际 105、断言仍为 104，Permission 实际 462、断言仍为 456；Develop `GET /exports/{id}/file` 的 `resource_ticket` Swagger 声明出现不允许的 conditional Permission。这些与并行日志／Develop 改动对应，本轮未修改其实现或基线，不能将新增用例通过推广为 Common 或全 Go 门禁通过。
- `make test-platform` 已执行，最后在 `test-online-runner` 的三个 Hosted public-origin 脚本夹具 20 秒超时处失败；未修改超时或以先前成功的子门禁冒充公共 T0 通过。
- `make test-changed` 已执行，标准预检识别全部受影响模块，但因缺少多项 PostgreSQL、MySQL、OceanBase T2 环境参数而退出，未进入全工作区执行；不把本轮 owner 结果推广到全部并行变更。
- System engineaccess PostgreSQL 首次调用因测试 DSN 环境变量名不正确未进入测试；修正为入口规定的 `ADDP_SYSTEM_POSTGRES_TEST_DSN` 后，标准入口因另一套 IAM PostgreSQL 门禁正在运行而拒绝抢锁。本轮未完成该项验证，不计为通过；后续复核使用 `bash scripts/test/system-iam-postgres-gate.sh --package engineaccess`，CI 由既有 System IAM PostgreSQL Job 验证。未中断另一套门禁或绕过共享测试库锁。
- 本轮 Go 格式、相关已跟踪文件 `git diff --check` 和新增文件行尾空白检查通过。已审查值校验、原身份保留、登记之前拒绝及失败事务边界；没有发现需要更改既定授权方案的问题。真实跨模块 T4、实际办理及 Grant 尚未实现和验证。

下一步优先接通真实办理消费的可信操作账号来源、owner 持久依据和 System 独立资格核验，再贯通提交后发送及权威核清；仍不先开放只靠内部参数或测试回调工作的授权页面。未重启或停止用户服务，未写开发业务数据库或源端，未提交代码。

### 26.34 首次受理消费当前引擎管理委派（2026-10-02，仍为内部事务能力）

本轮把已确认的目标管理资格底线接入 System `settleFulfillment` 的首次受理路径，未新增公开办理 API、Permission、Grant 或 migration：

- 当前 User／Membership／Tenant 核验后，按引擎→委派顺序锁定本租户目标引擎及原操作账号 Membership 的有效委派；不从租户管理员角色、其他账号的委派或 Catalog 责任身份推导该范围。
- 引擎与委派均使用共享行锁，再进入请求／精确目标边界。同一引擎不同目标仍可并行；引擎退出或委派撤销不能越过已持锁的受理事务。业务核验后再次按数据库墙钟检查引擎状态、委派生效及到期时间，行锁不冻结期限。
- 缺少委派、其他引擎／Membership 的委派、已撤销或已到期委派拒绝新受理，失败不保存回执或办理审计。原结果同参读取与关闭不重新要求历史操作人的资格，撤销不能改写旧回执或刷新原窗口；新编号仍须满足当前资格。
- 这是引擎管理范围检查，不是内容读取授权，也不替代独立办理 Permission、可信 owner 持久事实、接收主体或 Catalog 当前业务依据。真实 Grant 写入时仍须核验当前资格。内部业务核验测试回调不作为生产消费者，不改变 Catalog 现有业务确认 API。

规范先补入 `docs/spec/addp授权上下文规范.md` 5.5.5，再落实代码；`system/CLAUDE.md` 同步说明实现范围。测试沿既有 System Go T1 自动发现及 `system-iam-postgres-gate.sh --package engineaccess`；已有 `platform-ci.yml` 的 Go 门禁与 `release-and-t2-gates.yml` 的 System IAM PostgreSQL Job 覆盖，无新增 CI 路线或占位登记。

验证结果：

- System 完整 Go T1 通过：复用 `scripts/test/module-gate.py` 的已登记 owner 计划选择 Go T1，最终退出码 0。新增值检查覆盖有效状态、精确 Tenant／Engine／Membership、缺失事实、非有效引擎、已撤销及到期边界。
- `bash scripts/test/system-iam-postgres-gate.sh --package engineaccess` 完整通过，追加竞争用例后再次通过，最后退出码 0、无意外 Skip。实际覆盖委派缺失、跨 Membership／引擎拒绝、核验期间到期回滚、撤销后历史核清／关闭、撤销先持写锁并提交后等待受理被拒绝、受理持共享锁阻止撤销和引擎退出，以及不同目标并行。只使用允许的 `addp_iam_test`，标准入口重置并清理 owner 测试 Schema，未绕过测试库锁。
- 初次夹具调用漏传撤销版本参数导致编译失败，已纠正；短期成员夹具的建立与到期断言统一使用数据库时间，避免两种时间来源影响一秒边界。以上通过结果均覆盖修正后文件。
- `make test-platform` 本轮完整通过，退出码 0，包含 CI 登记、授权清单及 Swagger 覆盖检查；本轮没有 API 契约变化，不修改 Swagger。
- `make test-module MODULE=system` 首次未注入 PostgreSQL DSN，在统一预检退出，没有执行完整模块计划。随后显式核实 Infra 实际端口并使用上述标准 owner Go T1／PostgreSQL T2 与公共 T0；未运行 System 前端或其他 System T2，不把该范围计为完整模块验收。
- 全共享工作区的 `make test-changed` 已执行，受影响模块被识别，但缺少其他 owner 的 PostgreSQL、MySQL、OceanBase 环境参数，预检拒绝，未进入执行。无关并行变更的完整门禁不计为通过。Go 格式与本轮相关文件 `git diff --check` 通过。

下一步优先把真实操作账号来源、独立办理 Permission 与 Catalog 持久业务依据核验接入可信跨模块消费，再贯通已实现的提交后发送和核清原语；这些条件齐备前不开放实际受理／源数据 Grant 接口。真实跨模块 T4、表级 Grant、Manager／SQL 执行侧的统一资源裁决尚未实现和验证。未重启或停止用户服务，未写开发业务数据库或源端，未提交代码。

### 26.35 真实 IAM 写入与受理核验的锁顺序审查（2026-10-02，历史调查，修复见 26.36）

本轮原计划补齐首次受理的接收主体有效性核验。静态审查发现，不能只在受理路径上增加身份／组织共享锁：既有 IAM 写入路径与 26.31—26.34 的受理核验顺序存在反向等待条件，需先协调真实写入消费者。未修改运行代码，没有将接收主体门禁或生产授权闭环计为完成。

可复核的代码证据：

- `system/backend/internal/iam/repository_user_authorization_source.go`：受理核验依次对 Principal、Tenant Membership、Tenant 取 `FOR SHARE`；随后进入引擎、委派及精确目标边界。
- `system/backend/internal/iam/tenant_role_service.go` 的角色分配事务：先对操作人 Principal 和 Tenant 取写锁，再锁作用域、目标 Membership，最后对目标 Principal 取写锁。角色管理操作人与办理账号不是同一人时，存在反向等待：受理事务已持目标 Principal 的共享锁并等待 Tenant；角色分配事务已持 Tenant 写锁并等待同一目标 Membership／Principal。PostgreSQL 的死锁回滚不构成成功办理，也不能作为正常业务协调协议。
- `system/backend/internal/iam/organization_service.go` 的项目组成员创建、更新和结束事务：先锁项目组，再锁成员 Principal。若受理在锁定办理人后再锁接收项目组，还会与涉及该办理人的成员维护形成反向等待。部门成员路径及角色作用域写入也需纳入同一审查，不能孤立指定一个“组织在前”或“身份在前”的新顺序。

上述结论来自代码与真实行锁模式的静态审查，本轮尚未执行这两条真实消费者的并发复现，不宣称已观测到运行中的死锁。26.34 已通过的独立目标、引擎退出和委派撤销测试仍只证明其原验证范围，不能外推为 IAM 角色／组织写入已经贯通。

建议下一步先讨论并限定一次 System IAM 并发边界收敛：不改变功能权限、资源权限、责任分工或模块权威，仅统一受理核验与涉及的角色／成员写事务取锁顺序；同一次涉及多个账号时也须有确定顺序，并在锁后重新核验其权威关联。完整调查影响路径后先修订规范，再改代码，不能通过捕获死锁无限重试、跳过资格、放宽为无锁读取或另建权限副本规避。

后续验收清单：

- [ ] 在既有 System IAM PostgreSQL 标准入口内复现真实角色分配与受理资格读取的竞争，而不只测试手写的相似 SQL。
- [ ] 覆盖项目组／部门成员变更、角色授予和撤销、多账号相互操作，以及提交与回滚后的当前资格结果。
- [ ] 补齐接收主体有效性核验，覆盖跨租户、账号失效、成员到期和组织关闭；提交前按数据库墙钟复核，失败不留下新受理结果。
- [ ] 保持历史回执只读核清及未受理请求关闭不依赖已失效的原办理／接收资格。
- [ ] 运行 System Go T1、真实 IAM／engineaccess PostgreSQL T2 和平台 T0；已有 CI 自动发现覆盖时不新建第二套入口。再继续可信 owner 持久依据、独立办理 Permission 与实际 Grant 消费。

本轮仅更新本专题；文档差异通过 `git diff --check`。未运行代码门禁、未执行上述竞争复现，均不计为通过。未重启或停止用户服务，未写开发业务数据库或源端，未提交代码。

### 26.36 IAM 写事务与受理核验统一账号锁顺序（2026-10-02，已实施并通过 System 完整门禁）

用户已确认先收敛上述真实写事务的竞争，再补接收主体门禁；本节是 26.35 历史调查之后的当前进度。首先在允许的 `addp_iam_test` 内，通过既有 `scripts/test/system-iam-postgres-gate.sh --package engineaccess` 调用真实 Tenant Role Service 和 IAM 身份读取仓储，成功复现原角色分配路径的 `SQLSTATE 40P01`。测试 Hook 仅控制交错，不替换 SQL、权限或权威来源。

落实的唯一顺序与边界：

- 所有受影响 Principal（含需锁定的操作者）去重后按 ID 升序取得写锁，再取得 Membership、Tenant、组织、Role、Assignment 等下层锁。共享身份读取保留 Principal → Membership → Tenant → 精确请求／目标的顺序；不增加全局互斥或无限重试。
- 角色分配与撤销、角色定义修改与停用、部门／项目组成员创建／更新／结束、部门停用／恢复、项目组关闭及租户生命周期改为上述顺序。租户成员有效期维护也不再先锁 Membership 后写 Principal；租户成员建立与生命周期纳入同事务的审计操作者锁。组织定义维护与租户详情更新先锁其操作者，避免持有下层锁后在审计外键检查中反向等待。
- 从 Membership／Assignment 定位账号的预读只用于发现不可变身份；锁后重读当前身份范围，保留原状态、版本、类型、作用域及有效期核验。角色、组织和租户批量变更先发现账号集合并有序锁定，再锁聚合根、重查集合；发现集合变化整笔返回冲突，不漏掉新成员、不在下层锁内倒序补锁。
- 保留数据库授权版本触发器和身份不可变保护。不修改已执行迁移，不新增迁移、默认权限、实际 Grant、公开受理接口或权限副本；接收主体门禁及跨模块可信链仍未完成。

测试与 CI：

- 两个 T1 用例约束账号去重排序、预读集合不被修改、当前集合增减／替换时拒绝以及读取错误原样传递。
- 真实 PostgreSQL 用例覆盖角色分配／撤销／定义变更、部门及项目组成员维护与生命周期、租户成员有效期、租户生命周期与共享资格读取的竞争，以及两账号相互分配角色。批量集合变化覆盖角色、部门、项目组和租户，并验证冲突后的聚合状态未变。
- 测试直接并入已有 `TestFulfillmentArbitrationAgainstPostgres`，继续由 System IAM PostgreSQL 标准门禁和既有 CI Job 自动发现；没有新增第二套入口。真实 IAM 写入竞争覆盖 15 个场景，批量账号集合变化覆盖 4 个场景，全部通过。
- 完整 owner 验证使用注入允许测试 DSN 的 `make test-module MODULE=system`，退出码为 0：平台 T0、System 后端 Go T1、前端 91 个单元测试与 33 个 Playwright 用例及构建、完整 IAM PostgreSQL T2（IAM／OAuth／API／迁移／engineaccess）、隔离 Runtime Log T2 均通过。运行日志门禁验证投递故障重试与恢复，并完成测试资源自动清理；前端构建仍有既有大包体积告警，不是失败门禁。
- 本轮 Go 格式及 `git diff --check` 通过。上述结果只覆盖 System owner 标准门禁；无关并行工作区变更没有计为全平台验收通过。

本轮收口时的下一优先项：补齐首次受理的接收主体当前有效性核验（账号／租户成员／组织范围、到期与关闭、提交前数据库墙钟）；后续实施进展见 §26.37。历史回执核清与未受理关闭仍不能被失效资格阻断。其后再接通可信 owner 持久依据与真正授权消费，不能把本轮并发修复当作生产数据授权闭环。

本轮不接管或重启用户的开发服务，不操作开发业务数据库或源端，不提交代码。

### 26.37 首次受理接收主体有效性门禁（2026-10-02，已实施并通过 System 完整门禁）

本轮限定为内部源授权受理事务：核验接收 User 及本 Tenant Membership，或本 Tenant 的有效 Department／Project Group；不新增真实 Grant、功能 Permission 或公开接口。先按 ID 升序共享锁定办理人与接收账号，再核验当前身份／组织事实及数据库期限，保留历史核清和未受理关闭不受后来主体失效阻断。

落实的边界：

- User 接收方必须是有效用户账号，且在当前 Tenant 中有未结束、未暂停、未到期的 Membership；机器身份、其他 Tenant 成员或不存在的账号不得产生新受理结果。接收身份不绑定其历史授权版本，操作者来源仍按原版本严格校验。
- Department／Project Group 必须属于当前 Tenant 且仍有效。向组织共享不要求组织已有人，不在受理时把组织展开为一组个人授权；将来消费权限仍须实时核验成员关系。
- 操作者与接收账号的 Principal 去重升序共享锁定，再取得成员、Tenant、组织及接入管理范围锁。首次业务核验前和持久受理结果写入前都使用数据库墙钟核验有效期；IAM 生命周期写入必须等待已有受理事务结束，随后新的受理不能再使用失效主体。
- 首次失败不留受理回执或审计半成品；已有回执核清、同参数历史结果恢复、未受理关闭不因接收方后来失效而被阻断。不新增公开接口、真实 Grant、Permission、迁移或专业数据副本，不写源端。

测试与 CI：

- 新增接收资格 T1 边界测试及 21 个真实 PostgreSQL 场景，直接并入已有 `TestFulfillmentArbitrationAgainstPostgres`；覆盖不存在／跨 Tenant、账号失效／机器身份、成员到期、部门停用、项目组关闭、业务核验期间到期、真实角色与生命周期写入竞争、失败回滚及历史恢复。
- `bash scripts/test/system-iam-postgres-gate.sh --package engineaccess` 通过；使用现有 `addp_iam_test`，不另建数据库或测试路线。
- 注入已核实实际端口的允许测试 DSN 后，`make test-module MODULE=system` 完整通过，退出码 0：平台 T0、System 后端 Go T1、前端 91 个单元测试与 33 个 Playwright 用例及构建、完整 IAM PostgreSQL T2（IAM／OAuth／API／迁移／engineaccess）、隔离 Runtime Log T2 均通过。日志门禁完成真实投递故障重试、恢复及测试资源自动清理；前端构建仍有既有大包体积告警，不是失败门禁。
- 首轮 PostgreSQL 失败来自测试夹具：结束成员时缺少结束时间、委派到期略晚于成员到期、真实生命周期审计缺少上下文。分别改用实际成员结束服务、限制委派期限、补全审计身份后重新运行；以上通过结果均覆盖修正后的文件。Go 格式与 `git diff --check` 通过。
- 上述结果只覆盖 System owner 的标准验收范围，不宣称全部并行工作区或真实跨模块授权 T4 已通过。
- 已有 System Go T1、System IAM PostgreSQL T2 和平台 T0 自动发现上述用例；CI 继续使用既有 System owner 门禁及 `system-iam-postgres-verification`，无需新增入口或修改编排。

下一优先项：设计并接通 Catalog 已持久业务决定到 System 的可信受理／核清调用和独立办理 Permission，再落实实际授权写入及消费。内部受理底座不等于用户已能完成生产数据授权闭环。

本轮不接管或重启用户的开发服务，不操作开发业务数据库或源端，不提交代码。

### 26.38 可信受理接入盘点与交付顺序（2026-10-02，调查时的计划，后续进展见 §26.41—26.45）

本轮回到完整消费链，而不是继续单独增加事务内部检查。源码确认：Catalog 已有真实业务确认 API，双方已有持久请求、受理及核清的内部原语；但尚无完整的可信生产调用。此前 T1／T2 通过不代表用户已能办理源数据授权，更不代表实际数据访问已经生效。

#### 已存在能力与生产缺口

| 环节 | 已有事实 | 下一交付包必须补齐的内容 |
| --- | --- | --- |
| 业务确认 | Catalog 保存不可变业务决定，绑定原责任关系、来源、目标和共享参数 | 明确确认人当前资格的核验语义；不能仅用历史账号 ID 或责任缓存证明仍有资格 |
| 办理请求 | Catalog 内部准备方法保存完整请求绑定；提交前不发网络请求 | 从已核验的当前 User AuthContext 提取原办理人，不接受请求体自行声明身份；校验独立办理 Permission 和精确目标管理范围 |
| 可信 owner 依据 | Catalog 可在本地事务核验决定的当前依据 | 提供专用、最小范围的 Runtime 读取契约，返回与该持久请求精确绑定的依据；普通用户读取决定的 API 不能充当 System 的机器核验接口 |
| System 受理 | 已有原操作者、管理委派、接收主体、精确请求／目标竞争及持久结果门禁 | 接通认证后的 Runtime 入口、独立资格与 owner 依据校验；测试回调不作为生产业务证明 |
| 核清与恢复 | Catalog 内部恢复原语支持读取原结果，明确不存在时再关闭同请求；关闭也可能返回已受理结果 | 接通真实客户端、提交后发送和故障恢复消费者；响应丢失、重启或超时不能清除本地待核清状态 |
| 实际内容授权 | 尚无本链路的真实 Grant 写入与执行侧消费闭环 | 后续交付真实授权写入、撤销和执行时裁决；受理结果不等于 Grant，更不等于已有内容访问权 |

服务身份的具体缺口：System 当前内置 Confidential OAuth Client、Tenant Runtime Client 清单没有 `addp-system`，未具备可直接反查 Catalog 依据的独立 Runtime 身份。拟复用现有 OAuth／Service Principal／Tenant Membership／最小 Runtime Role 机制补齐，不新建 Token 类型，不借用 Catalog 的凭据、平台管理员 Token 或原用户 Token 执行后台核清。身份配置、凭据生成与部署、清单发布和迁移必须一起完成，不能只加一个必需密钥而破坏标准启动。该方案目前是接入计划，不是已经创建或发布的服务身份。

#### 业务确认人与办理人的授权版本边界（已确认）

业务确认人与授权办理人可以不是同一账号，必须区分两种授权版本：

- 办理请求绑定的原操作者授权版本：沿用已确认规则，首次受理严格核验；过时拒绝，不能把旧请求改成新版本继续办理。
- 业务决定保存的确认人授权版本：保留当时确认的审计事实，不用作永久资格快照；与原操作者的严格版本绑定不同。

用户已确认：确认人的历史授权版本保留用于审计；首次受理实时核验其当前账号、原租户成员身份、原业务负责人责任关系和业务确认所需权限。若仍具备资格，不仅因授权版本变化就要求重新确认；若撤权或移交责任则不再接受新办理。历史已受理结果和核清仍遵循既有规则。正式规则已补入授权上下文规范 5.5.1，后续受理实现据此继续推进。

#### 按完整用户结果交付，而不是按内部函数交付

1. **真实准备→受理→核清工作包**：上述语义确认后，先同步正式规范，再一次补齐可信原办理人来源、独立功能 Permission、两侧 Runtime 身份与专用依据契约、真实调用和恢复消费者。System 在获取身份／请求／目标锁前读取可信 owner 依据；持久待核清保护维持 owner 依据稳定，不在锁内跨模块反查。历史结果同参恢复不重新冒充一次新受理。
2. **纳管与明确退出工作包**：接通首次责任纳管与 System 精确目标批准要求的生产协调，以及用户明确撤销 Catalog 纳管后的资格核验和交接。不存在批准要求、Catalog 失联、曾经部署过 Catalog，都不能自行推导批准模式；保持 Catalog 可选部署，不把其在线状态变成其他业务模块的启动强依赖。
3. **实际授权与数据消费工作包**：受理后由 System 权威写入 Grant；按当前资格及办理窗口再次核验，窗口不能通过重试刷新。选定 Manager 预览与 Develop SQL 只读路径贯通功能权限与资源权限的交集，并覆盖期限、撤销、跨 Tenant、接收主体变更及完整读取目标集合。未经明确用户操作不写源端。

首版业务确认接收方继续限于当前 API 支持的 User／Project Group；System 内部具备 Department 核验能力，不代表可以未经讨论扩大业务入口。功能 Permission 的 Scope 沿用现行发布规范：Tenant Context 的通用功能入口使用 Tenant Scope；不把 Department／Project Group Assignment 直接放大为全租户功能 Allow，也不从责任身份自动生成 IAM 角色。

#### 实施与验收清单

- [x] 确认确认人授权版本变化的业务含义，更新正式规范；对应 T1／T2 边界用例与受理核验同步实施。
- [ ] 真实接入同次交付身份配置、owner Permission 清单／角色显式分配、迁移、双语错误、Swagger 产物和路由覆盖；不先发布没有生产消费者的占位权限。
- [ ] 覆盖伪造请求体账号、错误 Client／Tenant／Scope、缺少独立办理权限、业务确认撤权／责任移交、完整绑定不匹配、受理资格自然到期；失败不得留下新受理或 Grant 半成品。
- [ ] 覆盖提交后发送、响应丢失、同参重试、读取未找到与关闭竞争、进程重启后的核清；证明旧请求不变参、不刷新窗口、不因本地超时释放依据保护。
- [ ] 复用 Catalog／System 已登记 T1 与 PostgreSQL T2 标准入口；本地仅使用允许的测试库，平台 T0 核验授权清单、迁移及 Swagger 登记。真实跨模块故障验证纳入现有 Online T4 体系；现有自动发现不能命中时，同次补 CI 登记与一致性检查。
- [ ] 分别报告“业务决定”“已受理”“已写入 Grant”“执行侧实际可读”的验收结果，不能相互替代；真实首次授予与撤销通过前不宣称生产数据授权闭环完成。

本轮仅补充专题计划，没有新增 API、身份、权限、迁移或运行代码；待确认项不写入正式授权规范。专题文档 `git diff --check` 通过；按统一入口尝试 `make test-changed`，识别当前共享工作区 179 个变更文件及 26 个受影响模块后，因未注入各 owner 所需的 PostgreSQL／MySQL／OceanBase 测试连接参数而在预检失败，未执行代码门禁，不计为通过。后续运行代码交付仍须通过已登记的 owner 门禁及对应 CI，不能用本轮文档检查替代。未重启或停止服务，未操作开发业务数据库或源端，未提交代码。

### 26.39 业务确认人当前 IAM 资格核验（2026-10-02，内部受理门禁已实施并通过 System 完整门禁）

本轮落实 §26.38 已确认的授权版本边界，先同步授权上下文规范 5.5.1／5.5.5、术语表及两个模块的职责说明，再修改 System 内部首次受理路径。这里只完成确认人的当前 IAM 资格检查；Catalog 原业务负责人责任连续性及可信 owner 依据仍由下一工作包接通，不能把测试回调视为生产业务证明。

落实的单一路径：

- 确认人来源必须包含原 User Principal、原 Tenant Membership 及历史授权版本；不能从办理人推导。首次受理要求原账号、原成员身份及当前 Tenant 仍有效，且具有本 Tenant Scope 的 `catalog.entry.read` 与 `catalog.sharing_decision.create`。历史确认版本保留审计意义，不与当前版本作相等比较；办理人的原请求版本检查保持严格。
- 办理人、确认人及接收 User 的全部 Principal 先去重升序共享锁定，再锁成员、Tenant、管理范围、请求和精确目标。复用 IAM 当前有效角色权限读取，不持久化一份权限、业务决定或责任副本。
- 角色授予、撤销及定义写入必须等待受理事务结束；角色授权的自然到期不会被锁冻结，完成业务核验后按数据库墙钟再次复核。首次资格失败不留下新受理回执或审计半成品。
- 确认人后来撤权或成员失效，阻止新的首次受理，但不阻断同参数历史结果恢复、只读核清或未受理关闭。确认人与办理人是同一账号时，也不能借用确认人的规则放宽办理人的版本绑定。

测试与验收：

- T1 覆盖无关版本变化、缺失身份与权限、原成员不匹配、跨 Tenant、部门／项目组 Scope、未来生效和期限边界；另外约束同一身份作为办理人时仍须严格匹配版本，以及多个授权中仍有有效授权时的判断。
- 12 个真实 PostgreSQL 场景并入既有 `TestFulfillmentArbitrationAgainstPostgres`，覆盖真实角色授予／撤销、角色定义变更、成员暂停、自然到期、确认人 ID 小于办理人的锁顺序、撤权等待受理提交、历史恢复及同人兼任。只使用既有 `addp_iam_test` 和 System IAM PostgreSQL 标准入口。
- 首轮门禁发现并修正两处测试夹具问题：无关角色采用已发布且允许租户自定义的权限；并发 Hook 精确暂停目标 Principal，而不是暂停加入确认人后顺序可能变化的第一个账号。没有放宽权限门禁、并发断言或业务规则。
- 本轮最小数据库门禁 `bash scripts/test/system-iam-postgres-gate.sh --package engineaccess` 通过；追加两个并发场景后，注入通过 `scripts/infra/status.sh` 核实实际端口的允许测试 DSN，完整运行 `make test-module MODULE=system`，退出码 0。平台 T0、System 后端 Go T1、前端 91 个单元测试与 36 个 Playwright 用例及构建、完整 IAM PostgreSQL T2（IAM／OAuth／API／迁移／engineaccess）、隔离 Runtime Log T2 全部通过，12 个确认人 PostgreSQL 场景全部命中且通过。日志门禁覆盖真实投递故障重试、恢复及资源自动清理；前端构建只有既有大包体积告警。
- Go 格式检查、受影响文件 `git diff --check` 及围绕身份连续性、事务锁序、自然到期、历史恢复和模块权威边界的人工代码审查通过。上述结果仅覆盖 System owner 标准门禁，不宣称其他并行工作区变更或真实跨模块授权 T4 通过。
- 既有 System Go T1、System IAM PostgreSQL T2 和 CI `system-iam-postgres-verification` 自动发现这些用例；不新增第二套测试入口。未新增公开 API、Permission、Runtime 身份、Grant 或迁移，因此本轮没有 Swagger 或 CI 编排变更。

下一优先项仍是 §26.38 的真实准备→受理→核清工作包：补齐独立 Runtime 身份、可信 owner 持久依据、原办理人来源与独立办理权限，再接通真实客户端和恢复消费者。当前内部资格门禁不代表用户已能完成生产授权，受理回执也不代表已写入 Grant 或已能读取数据。

本轮未接管或重启用户服务，未操作开发业务数据库或源端，未提交代码。

### 26.40 精确目标批准要求的显式初始化与管理读取（2026-10-02，管理 API 与标准门禁已完成，完整授权链路未接通）

真实首次受理必须消费 System 当前精确目标的批准要求；只保留内部仓储无法在生产中显式建立这项事实。本轮先补齐此前置，不以已部署 Catalog、自动扫描或已有目录条目推导批准方式，也不把初始化当作业务确认或实际数据授权。

- [x] 正式授权上下文规范 5.5.5 明确首次初始化、管理读取、独立功能权限与有效管理委派的交集，以及不覆盖既有要求的边界。
- [x] 发布单一 POST 初始化、GET 分页和 GET 详情契约：`/api/v1/system/engines/:id/access_approval_requirements`。只接受当前 User Tenant 身份；请求体只声明精确路径和原因；固定创建 `catalog` 模式、版本 1。错误跨租户／引擎引用返回 404，已有要求返回 409，不自动启用或覆盖退出后的模式。
- [x] 000174 发布 `.initialize`（high risk）与 `.read`（low risk），均 Tenant Scope、不可委托、允许租户自定义。无内置 Role 默认授权，无 Assignment、授权版本或 Grant 变更；清单、生成常量、中英文本和 Swagger 同步。
- [x] 复用 IAM 当前角色权限及管理委派，按身份 → 引擎／委派 → 精确目标排序；目标锁等待后复核数据库墙钟期限。初始化和审计同事务，失败不留配置半成品；不读取或写入源数据、不复制 Catalog 责任。
- [x] 用例并入既有 System T1、API PostgreSQL T2 及迁移前向升级 T2；自动发现仍由 `make test-module MODULE=system` 和既有 IAM CI 覆盖，不新增测试数据库或第二套入口。
- [x] 完成本轮标准门禁与人工审查，真实结果、修复过程及未验证范围如下。

本轮验证与收口：

- 平台 T0 和 System 后端 Go T1 已在本轮 `make test-module MODULE=system` 的对应阶段通过。组合入口随后因另一份测试 Vite 占用 4173 中断；没有停止或复用它，端口释放后使用原有分项入口补齐剩余门禁，不把组合入口的失败退出码记为通过。
- `make test-system-frontend` 最终退出码 0：91 个单元测试、38 个 Playwright 用例和生产构建通过；仅有既有大包体积告警。中间曾因并行日志页面用例在抽屉关闭动画结束前点击同名按钮而失败；该会话补齐抽屉隐藏等待后，完整前端门禁重跑通过，本轮未修改日志页面业务实现。
- 注入经 `scripts/infra/status.sh` 核实端口的 `addp_iam_test` 测试 DSN，`make test-system-iam-postgres` 最终退出码 0，覆盖 IAM、OAuth、API、全部前向迁移、engineaccess 及日志来源仓储。新 API 用例覆盖功能权限与管理委派的交集、跨租户隐藏、重复／并发初始化、不同精确路径隔离、权限和委派撤销、目标锁等待后 Token 到期，以及审计失败整事务回滚；000174 前向迁移验证无内置默认授权、无 Assignment 或授权版本变化且可重复运行。
- 全量迁移回归发现 System Permission 旧数量断言仍为 142；本轮两个 Permission 加并行日志来源一个 Permission 后实际为 145，已同步精确断言并重跑通过，没有放宽断言、跳过迁移或操作开发库 dirty 状态。
- `make test-authorization`、`bash scripts/swagger/check-route-coverage.sh system` 最终退出码 0，System 184 个公开路由方法覆盖一致；`make test-system-runtime-log` 隔离门禁退出码 0，真实投递故障／恢复、Loki 查询边界和测试资源自动清理通过。复用已有自动发现与 CI，没有新增旁路门禁。
- Go 格式、`git diff --check` 和身份／权限、锁顺序、到期、事务回滚及模块边界人工审查通过。以上只覆盖本轮 System owner 和授权清单校验，不宣称其他并行模块改动或真实跨模块源授权 T4 已通过。

未接管、停止或重启用户服务；没有执行开发业务数据库迁移，没有访问或写入源端，没有提交代码。

这不是完整纳管或源授权闭环：当前只有受保护的管理 API，没有前端配置入口；Catalog 首次责任纳管的生产协调、可信 Runtime owner 依据、准备→受理→核清消费者、退出／重新启用和实际 Grant 写入仍未完成。下一优先项回到 §26.38 的可信生产调用工作包，不能继续把内部回调或受理回执当成业务授权完成。

### 26.41 生产核清接口、共享客户端与 Catalog 故障恢复消费者（2026-10-02，恢复子链路已实施，首次受理与 Grant 尚未接通）

本轮先接通 §26.38 工作包中的故障恢复部分，避免将新受理发布建立在只能内部调用的恢复原语上。先同步授权上下文规范 5.5.5、企业资源目录实现规范和 System IAM 迁移说明；这里只消费已提交的原请求，不创建新办理或实际内容授权。

- [x] System 发布单一 Runtime `POST /runtime/engine-access-fulfillments/:request_id/resolve` 与 `/close`，使用当前 `addp-catalog` Tenant Service 身份和独立 `system.engine_access_fulfillment.execute`。Tenant、Principal、Membership、授权版本和 Token 期限均取可信 AuthContext；不接受正文 Tenant 或伪造调用主体。
- [x] 只读查询返回已提交、完整绑定一致的原结果；只有显式 `found=false` 才能进入同编号关闭。404、绑定冲突、鉴权失败、通信或解析错误均保留为错误，不解释为关闭。失效调用服务不能借绑定冲突探测历史。
- [x] 关闭复用同请求／精确目标竞争边界，返回胜出的原不可变结果；调用服务 IAM、授权版本、Permission 和 Token 期限须保持有效至提交。原人员或接收主体后来失效不阻断历史核清；等待目标锁后 Token 到期会回滚关闭及审计。关闭不能生成 Grant。
- [x] `common/authorization` 统一传输原绑定与原结果，身份与版本按十进制字符串传输；`common/client` 复用现有 Tenant Service Token 客户端，不透传 User Token 或 `X-Tenant-ID`。没有新增 Token 类型、业务事实副本或旁路请求路径。
- [x] Catalog 服务组合接入真实 System 客户端和后台恢复消费者，复用来源同步周期。仅处理提交至少一分钟的未核清原请求，每批 100 条按创建时间和请求编号游标推进；旧请求持续失败不饿死下一批。单项远端调用限时 30 秒，故障保留原保护并延后重试，不门控 Ready、不刷新五分钟办理窗口。
- [x] 000176 仅新增核清机器 Permission 并显式授予内置 `tenant.catalog_runtime`，推进受影响 Runtime Principal 授权版本并撤销其活动会话族。无 User Role 默认授予、无新 Assignment、无 Grant。清单、生成常量、双语文本、Swagger 与精确快照断言同步。
- [x] 用例并入既有 Catalog／System T1 与 PostgreSQL T2 自动发现，CI 沿用 `catalog-postgres` 和 `system-iam-postgres-verification`；没有新增测试库、第二套入口或绕过路由覆盖扫描。

验收记录：Catalog 完整模块门禁 `make test-module MODULE=catalog`、共享全仓 `make test-go` 已通过；System 组合入口的 T0、Go T1、前端 91 个单元测试、39 个 Playwright 用例及构建通过，但组合入口首轮在数据库阶段失败，不将整次入口记为通过。修正后完整 `make test-system-iam-postgres` 已通过 IAM、OAuth、API、迁移、源访问协调与仓储用例；`make test-system-runtime-log`、`make test-system-iam-runner` 和 `make test-authorization` 分项通过。全量 `make test-changed` 因其他受影响 owner 所需 PostgreSQL／MySQL／OceanBase 测试连接配置缺失在预检失败，未执行，不计为通过。

修复与复跑记录：000176 新增角色权限会由现有 IAM 触发器推进持有者授权版本，因此删除迁移中的重复手动递增，保留“仅推进一次”的精确断言；前向迁移分项和完整数据库门禁均验证通过。处理了测试身份的空角色数组契约、权限快照和唯一路由登记遗漏，没有放宽鉴权或路由扫描。首轮 IAM 的 `context_invalid` 未在随后复跑重现，未确认根因，也未据此修改生产身份实现。完整平台门禁另有既有 Online 脚本测试 20 秒超时；`make test-online-runner` 和完整 `make test-platform` 串行复跑均以退出码 0 通过，没有修改超时阈值，不推断未确认的超时原因。最终 `git diff --check` 通过。

本轮回归特别覆盖：关闭已提交但响应丢失后重启取回原结果；HTTP 错误不清保护；完整绑定冲突；原请求参数中大整数身份不丢精度；连续失败 100 条仍处理第 101 条；等待目标锁期间服务 Token 到期整事务回滚；000176 前向迁移的机器权限边界、授权版本仅推进一次和重复运行安全。

下一优先项是可信原办理人来源与 Catalog owner 持久依据，接通正式准备→首次受理；随后才是明确退出及实际 Grant 写入／执行侧裁决。尚未建立 `addp-system` 独立 Runtime 身份，未发布正式准备或新受理入口，未运行真实 OAuth 双服务 Online T4，不能宣称完整生产源授权闭环。§26.38 的完整工作包清单仍保持未完成。

未接管、停止或重启用户服务；000176 仅在允许的测试库验证，未执行开发业务数据库迁移，未访问或写入源端，未提交代码。

### 26.42 正式办理前的业务决定读取边界（2026-10-02，用户已确认）

继续 §26.38 的真实准备→首次受理工作包时，发现人类办理入口还缺少一项权限语义，必须先确认，不能通过删除过滤条件或复用业务确认权限来绕过。

当前事实：

- `GET /entries/:id/sharing_decisions/:decision_id` 是“读取本人共享确认记录”，路由要求 `catalog.entry.read` 与 `catalog.sharing_decision.create`；`EntryService.GetSharingDecision` 在当前可见条目内另以 `confirmed_by=当前 User` 精确过滤。该契约允许原确认人读取自己的历史，不是授权办理人的候选查询。
- 已确认业务确认人与授权办理人可以是不同账号，两人的功能权限、责任资格及目标管理范围分别核验。因此，不能为了办理而要求另一名办理人同时取得业务确认权，也不能直接删除 `confirmed_by` 限制，使所有业务确认者读取全部决定。
- System 的可信机器反查与人的浏览是两个契约。Runtime 反查只为同次已持久待核清请求核验完整绑定、原操作身份及当前业务依据；机器 Role 不能转化为人的目录读取权，不得用机器反查接口提供未经授权的决定列表。

已确认边界（已同步授权上下文与企业资源目录实现规范）：

1. 在 Catalog 办理入口，当前 User 须同时满足条目可见、独立源读取授权办理 Permission 及该精确目标所属引擎的有效管理委派，才能取得这个条目下仍可办理的业务决定摘要。部门、项目组成员、编目权限或资源责任身份不自动满足办理资格。
2. 摘要只提供办理所需的决定引用、精确目标、接收主体、动作、有效期和确认信息；不提供全租户历史枚举，不默认开放业务用途等完整历史正文。选择与提交时均重新核验，不把候选摘要视为正式受理依据。
3. 取得摘要不授予创建、修改或重新确认决定的权限，不改变原确认人读取本人历史的现有语义。旧历史读取与办理候选是不同职责，不增加旧接口的宽松身份分支。
4. System 引擎管理中的 Catalog 模式办理也不能借机器反查泄露 Catalog 人类不可见的事实；不部署 Catalog 或完成明确退出后的独立批准入口沿用其独立资格，不因此引入 Catalog 目录读取前置。

用户已确认上述人类读取范围，解除依赖该决定的暂停。先接通人类候选读取与 System 真实资格核验，再贯通 §26.38 的正式准备／新受理；新权限必须有当前真实消费入口，不先发布无消费者的服务身份或只有测试回调的办理接口。已完成的 §26.41 核清恢复链路不受影响。

独立收口：纠正 `catalog/CLAUDE.md` 中“核清只有测试替身”的过时说明，使其与生产客户端、后台恢复、正式规范及 §26.41 实施状态一致。没有修改业务代码、角色、权限、迁移、密钥或运行中的服务。

本轮验证：`make test-authorization` 退出码 0，授权清单及 Swagger 覆盖一致；`git diff --check` 通过。按默认入口运行 `make test-changed`，当前共享工作区命中 96 个变更文件、23 个受影响 owner；由于未注入 PostgreSQL／MySQL／OceanBase 所需测试连接参数，在门禁预检阶段退出，未执行各 owner 的完整门禁，不计为通过。本轮仅修改上述说明和待确认计划，不以这些检查宣称首次受理已实现。

### 26.43 人类办理候选与 System 当前管理范围（2026-10-02，后端子链路已实施并通过对应门禁）

落实 §26.42 已确认边界，先接通另一名人类办理人的摘要读取，避免把“同一个人既确认又办理”变成实现前提。这里只实现首次受理之前的浏览与当次资格观察，没有发布正式准备／新受理入口。

- [x] Catalog 单一 `GET /entries/:id/sharing_decision_candidates`：当前 User、条目可见、`catalog.entry.read` 与独立 `system.engine_access_fulfillment.create` 取交集。只返回当前条目下来源、原责任关系及期限有效的分页摘要；不返回业务用途正文或全租户历史，不放宽原确认人本人历史读取。
- [x] System 单一 `GET /engines/:id/access_handling_scope`：从当前 User AuthContext 取得原账号、Tenant Membership、授权版本和 Token 期限，以真实 IAM 与有效引擎管理委派核验。使用身份→引擎／委派共享锁，等待后按数据库墙钟复核；只返回当次观察，不签发新凭据，不创建受理或 Grant，也不依赖 Catalog 在线。
- [x] Catalog 使用本次已验证的 User Bearer 调用 System；不保留 Token，不替换为 Service 凭据，不在 401 时机器重试。Bearer 语法由共享认证 helper 唯一提供；不同大小写和合法分隔符使用同一解析路径。
- [x] 远端资格读取不持有 Catalog 锁。返回后使用新的只读快照复核条目可见性、来源版本、原责任关系及有效期，按同一快照计数和分页；请求期间到期的 Token、办理权限或盘点权限不能沿用。候选查询不建立待核清保护，业务责任移交与换源不会因为浏览而冻结。
- [x] 000178 只登记 Tenant Scope、高风险、不可委托、可租户定制的独立人类办理 Permission。没有默认角色授予、Assignment、数据 Grant 或现有账号授权版本变化；机器 `.execute`、编目权、确认权和引擎管理委派都不能替代独立功能权限。
- [x] 清单、生成常量、双语文本、Swagger、路由覆盖、模块职责与迁移说明同步；新用例并入既有 Catalog／System／Common T1、Catalog PostgreSQL T2、System IAM PostgreSQL T2。000178 纳入原 `engine-access-coordination` 分组及其入口测试，完整 CI 自动发现仍使用既有 owner 门禁，没有另建测试库或测试路线。
- [x] 正式准备→可信 owner 依据→System 首次受理随后在 §26.45 接通，并建立 `addp-system` 最小 Runtime 身份；尚无前端办理入口、实际 Grant 或执行侧数据访问结果。候选摘要不能代替正式受理依据，提交仍须核验确认人当前 IAM、接收主体及精确目标批准要求。

回归重点：不同办理人不能取得确认权；权限／管理委派撤销、跨 Tenant、过时授权版本、真实锁等待期间 Token 到期均拒绝；System 鉴权或通信失败不返回摘要；责任移交、同账号重新接任、换源、弃用、可见性改变及决定到期后旧候选不继续出现。摘要只读，不产生新的待核清记录。

验证记录：先通过 `bash scripts/infra/status.sh` 核实 PostgreSQL 实际宿主机端口为 25432；Catalog 使用 `addp_test`，System 使用 `addp_iam_test`，由既有标准门禁负责夹具与清理。以下最终结果退出码均为 0：

- `make test-module MODULE=catalog`：平台 T0、Catalog Go T1、前端单元／Playwright／构建与真实 PostgreSQL T2 全部通过，包含本轮新增候选、到期及可见性竞争用例。前端仍有既有包体积告警，不是失败。
- `make test-system-iam-postgres`：完整 IAM、OAuth、API、迁移、engineaccess 与 repository 分组均通过。不同办理人的实际权限／管理委派交集、撤销及真实引擎锁等待期间 Token 到期已验证；000178 前向迁移、重复运行、无默认授予及没有改变无关主体授权版本已验证。
- `make test-go`：当前共享后端改动的全仓 Go T1 通过；`make test-authorization`：权限清单、生成产物及 Swagger 路由覆盖一致；`make test-system-iam-runner`：标准数据库门禁的入口与互斥测试通过。
- `git diff --check` 通过。`make test-changed` 识别当前共享工作区 126 个变更文件、23 个受影响 owner，但因其他受影响门禁所需 PostgreSQL／MySQL／OceanBase 测试连接未注入而在预检失败，未执行全套 owner 门禁，不计为通过。本轮没有运行真实 OAuth 双服务 Online T4，也没有验证实际 Grant 或执行侧内容读取；既有 CI 自动发现覆盖已完成的 T0-T2，不能用它们冒充 T4 结果。

修复与复跑记录：轻量 SQLite 复用既有 attached-schema 建表 helper，不能用 GORM 在 main 中创建索引；可见性竞争须先进入已编目状态，不能制造非法的“已发现且租户可见”记录；同账号重新接任须替换旧关系，不能违反关系唯一约束重复插入；手工 AuthContext 的 Permission 数组保持规范排序。新增权限后，System 全迁移权限数量快照同步由 146 更新为 147；完整数据库门禁重新运行并通过。没有放宽数据库约束、认证或生产权限来使测试通过。

下一优先项：接通 §26.38 的正式准备与可信 owner 持久依据，再复用 §26.41 已接通的核清恢复。当前子链路不代表生产源授权闭环完成。本轮不接管、停止或重启用户服务，不操作开发业务数据库或源端，不提交代码。

### 26.44 System 反查身份的部署边界（2026-10-03，已确认，实施结果见 §26.45）

继续 §26.38 完整工作包前，核对独立 Runtime 身份的现有部署路线，发现一项需要先确认的启动策略。不是重新讨论 Catalog／System 的授权职责，也不改变已确认的办理资格。

调查阶段的源码事实（改造前）：

- `system/backend/internal/iam/service_credential_provisioner.go` 的 `Apply` 在任何凭据写入前校验全部内置服务 Secret；缺失任一项返回错误。当前内置 Client 与 Tenant Runtime Client 清单均没有 `addp-system`。
- System 配置及根 `.env.example` 没有 `SYSTEM_SERVICE_CLIENT_SECRET`；生产 Compose 尚未注入该项。`scripts/prod/setup-env.sh` 对全新配置生成独立随机 Secret，对已有配置只校验，不自动增加或轮换 Secret。
- 正式配置规范将每个内置模块的独立 Service Client Secret 规定为生产必需项。若简单把 `addp-system` 加入现有必填集合，已有部署缺少新凭据就会导致 System 启动失败；不能只补 OAuth Client／Permission 而遗漏这一后果。
- §26.41 已发布的历史核清／关闭使用 `addp-catalog`，不需要 System 反查 Catalog 的新身份。其恢复能力不应因为新的受理前置尚未配置而被关闭。

已确认部署边界：独立 `addp-system` 凭据用于可信新受理，不成为所有部署的 System Ready 前置。未配置该凭据时，System 及既有模块仍可启动，正式新受理明确返回能力未配置；不得借用其他模块 Secret、User Token 或管理员身份反查，也不得退回独立批准、取消 Catalog 批准要求或放宽已有访问规则。配置后仅启用同一条 OAuth Client Credentials 路线，不增加认证 fallback 或第二条受理路径。新增身份只获得当前 Tenant、精确持久请求的 owner 依据读取权，不获得源数据读取或业务确认权。

用户已确认此部署边界并授权继续完整工作包。正式规范已明确可选凭据只阻断新受理；实施按同一 OAuth 路线同步配置、身份、准备、反查、首次受理与测试。不修改 `.env` 或运行中的服务，既有候选与核清链路保持可用。

确认后的同次交付要求：

- [x] 正式规范明确未配置新身份时的 Ready、接口错误和既有核清行为；配置缺失不能解释为 Catalog 已退出。
- [x] 单一凭据配置／Provisioner 路线同时覆盖全新与已有 Tenant、独立 Secret 注入、生产 Compose 和配置初始化；已有配置不静默轮换。
- [x] 一次贯通人类正式准备、提交后发送、专用 Runtime owner 依据、System 首次受理及已有核清恢复；网络调用必须在数据库锁外，参与主体按同一稳定顺序锁定。
- [x] T0 复验 Permission／Role／Swagger／部署契约；现有 Common、Catalog、System T1 与 PostgreSQL T2 覆盖未配置凭据可继续初始化、错身份／跨 Tenant 拒绝、完整绑定、自然到期、响应丢失及受理／关闭竞争。复用原 Make／CI 自动发现，不另建本地测试库；不将此计为真实部署启动验收。
- [ ] 真实 OAuth 双服务链仍由 Online T4 证明，受控 Transport／认证夹具不算 T4；受理回执、Grant 写入和实际内容读取分别验收。

本节保留部署边界的调查与确认记录；正式准备及首次受理的本轮实现和验证见下一节，数据授权闭环仍未完成。

调查阶段验证：`make test-authorization` 通过，权限生成产物与全部模块 Swagger 路由覆盖一致；`git diff --check` 通过。`make test-changed` 当时识别共享工作区 45 个变更文件、System owner，但因未注入 `ADDP_SYSTEM_POSTGRES_TEST_DSN` 在 T2 环境预检失败，未执行受影响模块门禁，不计为通过。该调查阶段仅新增本节专题记录，没有修改生产代码；未运行 PostgreSQL T2 或真实 OAuth 双服务 Online T4，没有接管或重启服务。

### 26.45 正式准备→可信反查→首次受理（2026-10-03，后端子链路已实施，非数据授权闭环）

用户确认 §26.44 的可选凭据边界后，一次接通本轮后端消费链。先同步授权上下文、企业资源目录实现规范、配置规范及模块说明，再实现单一公开路径；不把内部回调或请求正文当作可信资格。

- [x] Catalog 人类 `POST /entries/:id/sharing_fulfillments` 只接收请求编号、决定编号和批准要求版本。从可信 User AuthContext 派生原办理身份，通过 System 实时核验独立办理 Permission 与引擎管理委派；从当前 Tenant Service AuthContext 取得调用主体。源目标、接收方、动作及有效期来自持久决定，不接受正文覆盖。
- [x] 网络资格观察不持 Catalog 锁；本地条目锁内按数据库墙钟复核权限、可见性及当前依据，提交完整不可变待核清绑定后才发送。远端错误、响应丢失或绑定错误保留 `pending`，不回滚已提交保护；复用原核清／关闭及后台恢复消费者，同参结果不延长窗口。
- [x] Catalog `POST /runtime/sharing-fulfillments/:request_id/basis` 仅允许独立 `addp-system` Tenant Service OAuth 与 `catalog.sharing_fulfillment.read`。精确匹配已提交且未核清的请求，重查当前来源和原业务负责人责任，返回最小原绑定及确认身份；不提供用途正文或跨租户／通用决定枚举。
- [x] System `POST /runtime/engine-access-fulfillments/:request_id/accept` 仅允许 `addp-catalog` 当前 Tenant Service 身份及 `.execute`。在本地数据库锁外反查，随后按稳定顺序共享锁定参与主体；事务内独立核验当前机器 IAM、原办理人精确授权版本与 `.create`、引擎管理委派、原确认人的当前确认权限、接收主体及批准要求模式／版本。确认人的历史授权版本仍只作审计，不强制与当前版本相等。
- [x] 原 `accepted/closed` 完整绑定结果可直接恢复，不依赖新凭据、原人员或接收方仍有效。反查期间已有结果先提交时转入历史核清；失效机器调用者不能借冲突探测历史。新受理失败不留下结果／审计半成品，办理窗口仍为原受理时间起最多五分钟，限时共享再受拟授予到期时间截断。
- [x] 000180 只登记最小机器依据读取权、`tenant.system_runtime` 和 `addp-system`，接入存量已初始化 Tenant 与后续新 Tenant。OAuth Client 初始停用且无 Secret，凭据由唯一 Provisioner 配置；移除可选配置停用 Client、推进其授权版本并撤销活动 Token Family。不新增人类默认权限、平台角色、Grant 或可编辑责任副本。
- [x] `.env.example`、生产 Compose 和初始化脚本同步可选独立 `SYSTEM_SERVICE_CLIENT_SECRET`。新配置生成独立随机值；已有配置缺失／留空可校验通过且不静默补写，借用其他服务 Secret 拒绝。缺失不阻断 System Ready，只使首次受理明确返回 `503 fulfillment_capability_unavailable`；历史核清／关闭和同参结果恢复仍可用，不推断 Catalog 退出。
- [x] Swagger、Permission／Role 清单、生成常量、双语错误与权限说明同步。新增用例挂入已有 Catalog PostgreSQL、System IAM PostgreSQL 和 Common Go 入口；000180 加入原迁移协调分组及 runner 登记测试，生产配置测试复用已有平台配置门禁。CI 沿用 `catalog-postgres`、`system-iam-postgres-verification` 和已有 T0/T1 自动发现，不新增本地数据库或旁路入口。
- [ ] 真实双服务 OAuth、跨进程故障与重启的 Online T4：本轮尚未运行，需纳入既有 `make test-online` 的专用部署验证，不能用 HTTP 替身或 T2 凭据测试冒充。
- [ ] 人类候选选择／正式办理前端、首次纳管／明确退出的生产协调、实际 Grant 写入与执行侧裁决仍待贯通；不把本轮受理回执等同数据已可访问。

验收范围及结果：

- `make test-module MODULE=catalog` 最终退出码 0：平台 T0、Catalog Go T1、前端单元／Playwright／构建及真实 PostgreSQL T2 通过。新用例覆盖提交后发送、锁外调用、不同确认人／办理人、专用反查守卫、伪造正文、完整绑定、大整数身份和待核清恢复；前端无本轮功能改动，仍有既有包体积告警。
- `make test-system-iam-postgres` 完整退出码 0，覆盖 IAM、OAuth、API、全部前向迁移、engineaccess 与 repository。新用例验证五分钟／限时截断、缺独立办理权限、确认撤权、伪造 owner 响应、旧要求版本、接收方无效、反查期间关闭先提交、旧人员失效后的同参历史恢复和不可变审计；000180 前向迁移与重复运行安全。
- 真实 OAuth PostgreSQL T2 验证可选 System 凭据：未配置不能签发，配置后仅得到当前 Tenant 的最小依据读取权，平台 Token 拒绝，移除配置后旧 Token 失效且新签发拒绝。该结果不是双服务 Online T4，也不证明真实开发环境已启动。
- `make test-authorization test-system-iam-runner test-platform` 退出码 0，权限与角色快照、生成产物、Swagger 路由覆盖、部署及门禁登记一致；`make test-ontology-infra-config` 在补充可选凭据回归后退出码 0。新增或留空的配置仅在操作系统临时目录夹具测试，不读取或写入工作区 `.env`。
- 全仓 `make test-go` 最终复跑退出码 0，Catalog、Common、System 及其余已登记 Go 模块通过。一次共享工作区复跑曾遇到并行日志改动 `common/cmd/runtime-log` 的 `receiverCredential` 尚未定义编译错误；未修改或覆盖该并行工作，核对当前文件后重新运行并通过，不将失败记为通过。最后的 `make test-authorization` 与 `git diff --check` 也通过。
- 默认 `make test-changed` 首次识别共享工作区 72 个变更文件和 26 个受影响模块，但 PostgreSQL／MySQL／OceanBase 多 owner 测试连接参数未注入，在预检失败，未执行全套门禁，不计为通过。上述本轮 owner PostgreSQL 门禁使用已核实宿主机 25432 的 `addp_test`／`addp_iam_test`，由标准入口隔离夹具并清理；不改变生产／开发迁移状态。各已登记 owner CI 承担尚未本地完成的扩散门禁，不宣称全部并行工作已验收。

下一优先项：在既有 Online T4 体系补齐真实 OAuth 双服务首次受理与响应丢失恢复用例；证明链路后再推进实际 Grant 写入及只读执行侧裁决。业务决定、已受理、已写入 Grant、实际可读必须分别验收。后续用例与门禁接入进度见 §26.46，尚无真实 T4 通过证据。

本轮没有启动、停止、接管或重启 ADDP 应用服务；没有操作开发业务库迁移状态、访问或写入源端、读取或改写工作区 `.env`，没有提交代码。000180 仅在允许的测试库验证。

### 26.46 正式受理纳入现有 Online 体系（2026-10-03，用例已接入，真实 T4 未运行）

承接 §26.45，复用现有 `enterprise-catalog-publishing` suite、专用 macOS profile 和 `make test-online`，不另建脚本、测试库或 workflow。实施前明确本轮属于验收脚本、T0／脚本确定性验证及 T4 用例接入，不修改生产授权算法、公开 API 或 Swagger；确认原 Make 登记、Platform CI 和手工 `Online T4 gates` 已覆盖修改文件。

- [x] 在原企业目录发布链中增加 `ECV-10`：同一专用 User 显式确认限时只读共享，通过 Catalog 正式准备接口触发真实 Catalog→System→Catalog 的 Tenant Service OAuth 反查与首次受理。必须核对决定、目标、接收方、原办理身份、批准要求版本及完整回执；`pending`、`closed` 或响应绑定错误不计成功。
- [x] 复用永久数据源对应的业务负责人，不自动移交责任或赋权。测试 User 另需确认、独立办理、批准要求初始化／读取 Permission 及当前引擎管理委派。首次仅通过正式 API 初始化精确 fixture 表的 Catalog 批准要求；已有独立批准模式停止，不覆盖或降级。
- [x] 专用 Runner 的准入新增独立 `SYSTEM_SERVICE_CLIENT_SECRET` 非空检查，在任何服务生命周期动作前拒绝缺失配置。此为该 T4 的前置，不改变普通部署允许留空的 System Ready 边界；脚本不读取工作区 `.env`，不自行签发或打印 Service Token。
- [x] 调用方丢弃首次已提交响应作为可用结果，再仅用原三个输入重放，核对完整回执、原受理时间与最多五分钟截止完全不变；决定明确十分钟到期且不得在恢复时续期。两个机器接口使用人类 Token 均须返回 403。
- [x] 主验收报告升级为 `addp.enterprise-catalog-publishing/v3`，浏览器协议仍为 v2。不可变业务决定、已核清请求和 System 回执单列为长期审计事实，不直接删除，也不混入临时 Asset／AssetCategory 的残留数量。成功路径仍恢复完整编目聚合；不确定提交与待核清保护使清理失败时，同时保留原请求编号、原失败原因和清理错误，不清保护或伪报零残留。
- [x] 同步测试与验收规范、`scripts/README.md` 的案例／环境／证据边界。原 `make test-online-runner` 自动执行新增正常、错误绑定、跨 Tenant 回执、错误办理身份、窗口非法／续期、错误决定期限、机器身份隔离、分页批准要求、缺业务负责人及失败清理证据用例。仍保持手工调度，首次真实通过前不加入夜间任务。
- [ ] 在专用干净部署运行 `make test-online ONLINE_SUITE=enterprise-catalog-publishing` 并取得真实双服务证据。本轮没有执行该项：个人共享工作区不是专用 Online 部署，不能接管现有服务或绕过仓库外凭据／隔离库准入。
- [ ] Catalog→System 之间真实网络中断、跨进程重启后恢复的 T4 尚未覆盖。调用方同参重放不冒充这些故障；现有 T1／PostgreSQL T2 的受控 Transport 及核清竞争仍与 T4 分开报告。
- [ ] Grant 写入、执行侧只读裁决和正式人类办理 UI 仍未贯通；本轮不创建或验证源 Grant，不把回执等同数据可访问。

最终验证：

- `make test-online-runner` 最终退出码 0；全部既有脚本组、新增企业目录用例和 Online CI 登记一致性检查通过。这是确定性脚本验证，不是真实 T4 结果。
- `make test-platform` 最终退出码 0，包含上述 Online 脚本验证、现有平台一致性与 Permission／Swagger 门禁。没有新公开接口，不需要生成新的 Swagger。
- `git diff --check` 通过。默认 `make test-changed` 当时识别共享工作区 17 个变更文件、22 个受影响 owner，但并行共享依赖改动需要的 PostgreSQL／MySQL／OceanBase 测试参数未注入，在 T2 环境预检失败，未运行全套 owner 门禁，不计为通过；未覆盖的扩散验证仍由原 owner CI 承担。

下一优先项：在现有专用 Runner 上手工执行 `Online T4 gates / enterprise-catalog-publishing`，先取得 `ECV-10` 的真实 OAuth 首次受理证据，再推进实际 Grant 写入与执行侧验收。首次接入需管理员在专用环境预置上述 Permission／管理委派和独立机器凭据；不能通过放宽权限或改变开发配置来让验收通过。本轮没有启动、停止、重启或接管用户 ADDP 服务，没有访问源端、操作开发库或迁移状态，没有修改 `.env`、提交代码或覆盖并行引擎研发。

### 26.47 人类办理入口接入盘点与同编号拒绝换参（2026-10-03，接收方候选范围已确认）

继续接入人类页面前先核对权限与恢复契约，不把只有 API 的能力误记为用户已经能完成办理。本机未配置 `ADDP_ONLINE_ENV_FILE`，共享工作区也不是专用干净部署；真实 T4 仍未执行，不启动或接管用户服务。

发现的页面前置缺口：共享确认支持同 Tenant 的有效 User／Project Group，而 Catalog 现有通用候选仅支持 Domain／Glossary／Element／Department／User；`GET /me/project-groups` 只提供当前账号的有效成员项目组，不能用它代表全部可接收共享的项目组。System 的 Catalog Runtime 候选也没有 Project Group 分支。若限制为本人项目组，会无依据缩小已确认的共享接收范围；若直接枚举全 Tenant，又会扩大组织信息读取范围，需要先确认。

当时提出并随后获确认的范围：具有独立共享确认权的当前业务负责人，可为本条目选择本 Tenant 的有效账号或项目组，包括本人未加入的项目组；只返回名称、编码、稳定 ID 和状态，不返回项目组成员、角色或数据。此候选读取不授予共享权，更不授予源访问；提交仍由 Catalog／System 核验。复用既有 System 组织事实、分页协议和鉴权能力，消费者明确限定条目范围；不把现有 `catalog.entry.read` 的通用候选接口直接扩大为全 Tenant 项目组枚举，不借用组织管理权限，不添加手填 ID 或第二套组织副本。当时依赖它的页面暂停；确认后的后端实现及剩余页面前置见 §26.48。

办理人展示接收方名称是不同的读取需求：应只解析其已经有权读取的候选决定所指向的主体，沿用独立办理权和当前引擎管理委派核验，不据此获得全 Tenant 候选枚举或业务用途正文。正式办理所需批准要求版本已有独立 `system.engine_access_approval_requirement.read` 读取接口；页面不能从独立办理权推导该权限，也不能让用户手填版本。这些契约须在正式页面交付前落实，不以浏览器缓存制造权威事实。

独立可继续的验证工作：在原 `ECV-10` 补齐同一决定编号改用途、同一办理编号改批准要求版本的 `409 catalog_sharing_decision_conflict` 拒绝；拒绝后读取／同参恢复必须仍返回原不可变决定和回执，不能新增办理编号或延长窗口。仅扩展现有脚本与标准门禁，不改变生产权限、公开 API、Swagger、Grant 或生命周期。沿用 `make test-online-runner`、`make test-platform` 及现有手工 Online CI；新拒绝请求本身允许留存操作审计，不能将场景预期数量冒充数据库审计计数核验。

本轮实施及验证：

- [x] 两种同编号换参拒绝、规范状态／错误码、拒绝后原决定／回执不变和原三参数重放已加入现有脚本及报告；新增测试覆盖错误接受、错误冲突码、拒绝后篡改和 int64 最大批准要求版本。不采用加一制造非法溢出版本。
- [x] 同步测试与验收规范及 scripts 案例表；没有新公开接口或 CI 入口，原自动发现覆盖本轮改动。
- `python3 -m unittest scripts/test/enterprise-catalog-publishing-online_test.py`：27 项退出码 0。这是根 Make 中已有脚本组的定位复验，不是真实 T4。
- `make test-online-runner` 最终复跑退出码 2，末组 275 项仅剩并行 Elasticsearch 注册表与清单断言不同步：`online-gate.py` 已注册 `elasticsearch-consumer-flow`，`online-gate_test.py:76` 的精确预期集合未包含它。本轮 Catalog 用例通过；不覆盖并行引擎研发，也不把整体失败记为通过。
- `make test-platform` 最终退出码 2，同样在上述 Online 组失败；未执行的后续步骤不记为通过。早期复跑曾发现既有回执篡改夹具误把新增 409 当成功回执解析，已修正为只对 200 注入篡改，最终上述两次完整门禁均不再有该错误。
- 默认 `make test-changed` 识别共享工作区 95 个变更文件、26 个 owner，因 PostgreSQL／MySQL／OceanBase 所需参数未注入在预检失败，未执行全套 owner 门禁。原 owner CI 继续承担未完成的扩散验证，不修改开发配置补齐测试条件。
- `git diff --check` 通过；真实双服务 T4、跨进程恢复、实际 Grant 和正式办理 UI 均未完成。没有启停用户服务、修改开发数据库／源端、改写 `.env` 或提交代码。

接续确认：用户同意上述条目级接收方候选读取范围。当前按该范围实现最小候选链路，不扩大通用条目读取或本人项目组接口；办理人的接收方名称仍须按已有可读决定限定。正式人类页面和真实 T4 尚未完成，真实 T4 仍是实际 Grant 写入前的独立验收前置，不能以页面或脚本替身绕过。

### 26.48 条目级接收方候选与办理摘要名称（2026-10-03，后端已实施，页面尚未接入）

按 §26.47 用户确认的范围，先同步企业资源目录实现规范与授权上下文规范，再实现唯一候选链路；没有新增组织副本或扩大通用候选接口。

- [x] Catalog 单一 `GET /entries/:id/sharing_recipient_candidates`：当前 Tenant User 同时具备 `catalog.entry.read`、独立 `catalog.sharing_decision.create`，且是可见有效 Meta 数据项的当前有效业务负责人，才能取得同 Tenant 的有效 User／Project Group 最小分页候选。支持空关键词浏览及名称／编码查询，不要求本人加入接收项目组，不返回成员、角色或数据。
- [x] System 原 Catalog Runtime 候选链增加 Project Group：沿用可信 Tenant Service 身份与组织读取交集，不开放人类通用组织枚举。`tenant.catalog_runtime` 清单补齐既有 000102 已登记的 `iam.project_group.read`，清单版本推进至 109；不修改已发布迁移，不添加新 Permission、人类 Role Assignment 或数据 Grant。
- [x] 办理候选只对当前可读决定里的接收主体精确批量解析，增加 `recipient_name`、`recipient_code` 与 `recipient_label_status=resolved|not_found`。缺失名称不以 ID 冒充；依赖失败或错误身份绑定返回错误，不降级为未经核验的候选。没有扩大业务用途正文或全租户历史读取范围。
- [x] Catalog 在锁外调用 System，返回后用新只读快照复核权限期限、条目可见性、来源版本和原责任关系；查询期间换源、移交、同账号重新接任、弃用、Token／盘点权限到期均不沿用旧资格。办理摘要名称解析后还重读同一分页，候选变化拒绝返回旧摘要。候选查询不创建决定、待核清保护、受理或 Grant。
- [x] 同步 Common 客户端、System Runtime 守卫、清单与双语 Swagger；用例并入原 Catalog／System／Common Go T1、Catalog PostgreSQL T2、System IAM PostgreSQL T2，原 Make 与 CI 自动发现覆盖，无新增测试库或旁路入口。
- [x] System 完整模块门禁发现前一轮 `tenant.system_runtime` 与 `sharing_fulfillment` 的角色管理页中英文标签遗漏；按现有后端事实补齐前端词条，不放宽清单覆盖测试或权限。
- [ ] 共享确认与正式办理页面尚未接入，实际 Grant 和执行侧数据读取尚未实现，真实双服务 Online T4 尚未运行。

实施回归范围包括非成员项目组、有效账号、跨 Tenant 隔离、维护权不能替代确认权、确认权不能替代条目读取、非业务负责人拒绝、非法分页／重复身份／停用主体／错误绑定拒绝，以及网络调用期间责任、来源、可见性和期限变化。到期测试首次曾偶发失败，随后原样复测通过；最终等待测试夹具的宿主机和 PostgreSQL 墙钟都越过原截止点后再断言，消除跨时钟边界竞争，不增加生产宽限期。

当前验证证据：

- `make test-module MODULE=catalog` 退出码 0，平台 T0、Catalog Go T1、前端 94 项单元测试及构建、PostgreSQL T2 通过；随后新增的有效账号和盘点权限到期用例又由 `make test-catalog-postgres` 与最终 `make test-go` 覆盖，退出码均为 0。当前 Catalog 前端标准入口是单元测试和构建，不把本轮结果写成新增页面的浏览器验收。
- System 原 `catalog-reference-candidates` PostgreSQL 标准分组退出码 0，真实迁移与非成员／空成员项目组、停用和跨 Tenant 用例通过。首次 `make test-module MODULE=system` 在上述双语标签检查失败，后续步骤未运行、不计为通过；修复后完整复跑退出码 0，平台 T0、System Go T1、91 项前端单元测试、39 项 Playwright 用例、构建、完整 IAM PostgreSQL T2 与运行日志 T2 均通过。运行日志标准门禁确认自建容器／网络／卷和临时源目录已清理，不涉及用户 ADDP 服务。
- `make test-authorization` 退出码 0，权限／角色生成产物与全部 Swagger 路由覆盖一致。最新 `make test-go` 退出码 0，覆盖共享消费者及新增 System Runtime 三项组织 Permission 缺一拒绝测试。
- 默认 `make test-changed` 识别共享工作区 164 个变更文件、26 个 owner，但其他 owner 所需 PostgreSQL／MySQL／OceanBase 参数未注入，在预检失败；未运行全套扩散门禁、不计为通过，仍由现有 owner CI 验证。Catalog／System 数据库验证只使用已核实宿主机 25432 的 `addp_test`／`addp_iam_test`，夹具及清理由标准入口管理。
- 最终 `git diff --check` 通过；本轮权限边界与最小 DTO 已按实现规范复核。真实双服务 Online T4、正式办理页面及实际 Grant 验证未执行，不以已有 T0-T2 结果冒充。

下一项需确认的页面契约：目前人类只有正式准备 POST，没有原办理请求的只读查询／找回接口。Runtime 的反查、核清接口只面向机器，不能直接给人调用；正式准备 POST 可能首次受理，不能当作页面刷新 GET，也不能依靠浏览器缓存成为唯一请求记录。现有后台恢复命令在权威明确未受理时会申请关闭原请求，也不能直接复用为人的只读刷新。

当时建议首版只读查询限定为：当前 User 在仍可见条目下，仅查看本人作为原办理人的请求；同时核验独立办理功能 Permission 和当前引擎管理范围。不因同项目组、责任部门或编目维护权开放他人办理历史；请求编号与原参数来自后端持久记录，结果由 System 精确查询，不新增受理／关闭／Grant。同参重试另由用户显式触发，沿用原请求编号和参数；查询失败不能解释为已关闭或已授权。该范围已获用户“同意，继续”确认，实施与验证进度见 §26.49；不再把它列为待确认的页面契约。

本轮没有启动、停止、接管或重启用户服务，没有访问或修改源端、开发业务数据库迁移状态或工作区 `.env`，没有提交代码，也没有覆盖并行 Elasticsearch／三维及其他模块研发。

### 26.49 原办理请求只读找回（2026-10-03，共享编译阻塞已解除，PostgreSQL 专项已通过）

用户已确认 §26.48 的首版人类只读范围。本轮先完善持久请求找回与刷新契约，不把正式准备 POST、机器 Runtime 或后台关闭／核清消费者复用为人的刷新入口。

- [x] 先更新企业目录实现规范和授权上下文规范，再增加 `GET /entries/:id/sharing_fulfillments` 与 `GET /entries/:id/sharing_fulfillments/:request_id`；复用当前条目读取及独立 `system.engine_access_fulfillment.create`，无新 Permission、迁移、Role Assignment 或 Grant。
- [x] 分页找回仅查询当前 Tenant、本人原办理且当前仍可见条目下的请求。逐个按原完整绑定中的引擎核验当前管理范围，再计数和稳定分页；不能用条目换源后的新引擎资格查看旧引擎请求。没有资格的原引擎不暴露条目或数量，依赖查询失败仍报错，不伪装为空列表。每页最多 100 条，拒绝非法、空值、重复及未知 query。
- [x] 详情按原持久完整绑定向 System 只读反查；原操作身份和历史授权版本只匹配原请求，不替换为当前版本，也不能恢复当前资格。权威明确未找到才返回 `pending`；`accepted`／`closed` 保留原受理时点及窗口，过期历史不刷新。错误身份、绑定、结果形状或不一致历史拒绝返回。
- [x] 网络调用在 Catalog 只读快照之外；返回前复核当前权限期限、条目可见、原绑定和原引擎管理资格。查询不持条目写锁，不新增请求、业务审计，不填写 `resolved_at`、提交、关闭、续期或写 Grant；本地已核清而权威反查未找到属于错误，不重新变成 pending。
- [x] 最小返回保留原请求编号、决定编号、批准要求版本、目标、接收方、动作和拟授权期限；版本／整数 ID 保持字符串精度，不向人返回机器调用身份、原 IAM 授权版本或业务用途正文。详情只追加动态权威状态，不落受理状态副本。
- [x] 新用例自动进入 Catalog Go T1；真实 PostgreSQL 复用 `TestPostgresSharingFulfillmentRecovery` 和现有 API 路由测试，因此既有 `make test-module MODULE=catalog`、`make test-catalog-postgres` 及 Hosted PostgreSQL CI 覆盖，无新入口、测试数据库或 workflow 旁路。Swagger 三份产物已同步。
- [x] 恢复轮次的 Go T1、新增服务和 API PostgreSQL T2 已通过，原共享编译阻塞解除。共享确认及原请求查询界面见 §26.50；新办理页面、实际 Grant 及执行侧只读裁决仍未贯通，真实双服务 T4 未执行。

验证用例：本人分页及跨人／跨 Tenant 隔离、原引擎资格过滤后计数、维护权不能替代独立办理权、原 IAM 版本变化后按当前资格读取、机器身份拒绝、非法分页和未知 query、pending／accepted／closed 精确反查、错误绑定和依赖故障拒绝、查询期间撤销管理范围／隐藏条目／权限到期、已弃用已核清历史可读、权威历史不一致拒绝、网络期间不持条目写锁和查询零写入。编写轮次未执行，恢复轮次已由下述标准门禁通过。

前轮验证证据与阻塞（历史记录，编译问题现已解除）：

- `make test-authorization` 退出码 0，权限／角色生成一致、新增 GET 的独立功能 Permission 与 Catalog 43 个公开路由方法的 Swagger 覆盖通过。
- 使用 `bash scripts/infra/status.sh` 核实 PostgreSQL 实际映射为 25432，随后通过标准入口注入 `addp_test` 的 Catalog 测试 DSN。`make test-module MODULE=catalog` 在平台 T0 的共享 Go 编译步骤失败，后续 Catalog T1、前端与 T2 未运行；`make test-catalog-postgres` 的 Repository 组通过，但 Service 组编译失败，新增 History 和 API 用例未运行，不能记为 T2 通过。
- 两个入口均报告 `common/execution/observation.go:365: ref.FieldName undefined`：共享工作区另一个并行血缘改动已从 `LineageResourceRef` 移除 `FieldName`，现有安全摘要调用尚未同步。只读比较 HEAD 与工作区已确认其来源；本轮不恢复旧字段或覆盖并行 owner 的修改来绕过门禁。
- 默认 `make test-changed` 识别共享工作区 278 个变更文件、27 个受影响 owner，但未注入整套 PostgreSQL／MySQL／OceanBase T2 环境，在预检拒绝执行，未计为通过。新 History 输入以最终 Catalog 专属门禁为验收前置；本轮尚未完成，不把既往轮次的通过证据复用为本轮结果。

本轮恢复验证：用户已修复共享编译。以实际 25432 映射和 `addp_test` 显式 DSN 运行 `make test-catalog-postgres`，退出码 0；Repository、Service（包含 History）及实际 API 路由组均通过。`make test-module MODULE=catalog` 退出码 0，平台 T0、Go T1、前端测试／构建和 Catalog PostgreSQL T2 均通过；最终界面证据见 §26.50。

当前优先项转到人类界面及办理查询契约，见 §26.50。页面刷新只用只读 GET，当前仍不宣称 accepted 代表实际数据已经可访问。

本轮没有启动、停止、接管或重启用户 ADDP 服务，没有修改源端、开发业务数据库、迁移 dirty 状态或 `.env`，没有提交代码或新增长期脚本。

### 26.50 人类共享确认及原请求查询界面（2026-10-03）

- [x] 数据项详情增加“共享确认与请求” Tab，复用既有 Console 模块路由；公开 URL 保存本人原确认编号，刷新 GET 找回，不自动 POST，不写浏览器审批凭据或第二份授权状态。
- [x] 当前业务负责人及独立确认 Permission 同时满足才可新确认。接收方使用条目专用候选下拉，支持按名称或编码搜索；用途、有效期方式均必填，永久必须显式选择，指定日期须为未来日期。不手填主体 ID，不由编目维护权推导共享资格。
- [x] 提交前冻结原编号和参数，并先同步公开 URL；重复点击只发送一次。通信失败后显式同参重试保留原编号、版本、日期和用途；刷新／恢复只读本人原确认。没有原参数时不根据现状态重构旧命令。
- [x] 原办理请求分页和权威结果通过 §26.49 两个 GET 消费；权限撤销或切换账号清除旧记录，过期返回忽略；网络失败不显示为 pending，accepted 明示不等于数据访问生效。
- [x] `SharingPanel` 是领域组合，使用既有 Element Plus 表单、选择、日期及分页；中文、英文文案同步。测试采用实际 SFC 挂载与受控 API，覆盖默认候选加载、连续点击防重、原参数重试、刷新只读、撤权清除与迟到响应、失败不伪装为 pending。复用仓库已有 `jsdom@26.1.0` 测试版本，新增到 Catalog devDependencies 和锁文件，不建立共享 node_modules。
- [x] 根 `make test-catalog-frontend` 自动发现新增测试并构建；既有 Platform CI frontend matrix 的 Catalog 行调用同一入口并按锁文件安装，因此无需新 workflow 或旁路脚本。
- [x] 本节发现的人类办理入口缺口已转入 §26.51：用户确认精确目标最小观察契约，不放宽既有配置读取 Permission，不手填、默认或舍入批准要求版本。
- [ ] 实际 Grant 写入、执行侧裁决与真实双服务 Online T4 仍未贯通；组件测试不是浏览器 T3 或 Online T4，不把已有 API 测试冒充实际用户授权。

本轮门禁：`CATALOG_POSTGRES_TEST_DSN='postgres://addp:addp_password@127.0.0.1:25432/addp_test?sslmode=disable' make test-module MODULE=catalog` 与同连接的 `make test-catalog-postgres` 均退出码 0；`make test-catalog-frontend` 23 个测试文件、104 个用例通过并完成 Vite 生产构建（保留既有 chunk 大小提示）。新增测试由既有 CI 标准入口自动执行，不新增测试库或运行服务。浏览器 T3 和真实双服务 Online T4 本轮未运行，不能计为通过。共享工作区另有 Agent 等并行改动，本轮只验收 Catalog owner，不把 Catalog 结果外推为整套工作区验收。

本节提出的“办理人读取精确目标当前批准要求”已获用户确认，后续实现与验证见 §26.51；本人当前办理资格与引擎委派用于核验，不通过 Catalog 机器身份代替人，也不隐式扩大为整个引擎配置列表读取权。

### 26.51 精确目标批准要求观察与人类正式办理（2026-10-03）

用户已确认 §26.50 的最小读取范围。本轮沿用现有权限及管理委派，不新增 Permission、默认授权或迁移。

- [x] System 只读 POST `/engines/:id/access_handling_requirement`：完整结构路径精确查询，正文无 Engine／Tenant／操作者覆盖，版本以字符串无损返回；缺失不初始化，无要求枚举及内容授权。
- [x] Catalog 复用本条目候选分页，在共享 Tab 中下拉选择，自动读取目标要求；正式提交固定原编号、决定及版本，显式同参重试，刷新只读原请求。pending 仍可同参显式重试；GET 找到 accepted／closed 后结束不确定重试。切换账号、撤权或切换公开原请求身份时清理旧命令，迟到响应不复活旧状态。
- [x] System T1 守卫、T2 真实数据库验证精确身份、无配置读取扩权、撤权／到期复核与零批准写副作用；Catalog 实际组件交互验证固定参数、防重、恢复、撤权和旧响应隔离。原请求 URL 身份无效时明确报错，不退回新办理表单；另一名合格办理人不因缺少业务负责人身份或确认 Permission 而被界面误挡。
- [x] System Swagger 生成与覆盖检查通过，189 个公开路由方法一致；Catalog 模块门禁、System 完整 PostgreSQL 门禁及前端门禁通过。测试继续由现有自动发现与 CI owner Job 执行，不新建库或旁路脚本。
- [ ] System 完整模块门禁：并行 Redis 能力变更与既有注册测试断言不一致，完整 Go T1 未通过；本轮不修改该并行变更。

本轮不接实际 Grant，受理成功仍不表示实际访问生效；不启停用户服务，不提交代码。

界面使用路径：先由具备当前业务负责人身份和确认权限的账号，在有效数据库数据项的“共享确认与请求”中明确接收方、用途和有效期；办理人另需 `catalog.entry.read`、`system.engine_access_fulfillment.create` 及当前有效引擎管理委派。打开本条目的共享确认下拉后，选择一个当前候选，System 自动返回该精确目标的批准模式及无损版本，再显式提交。精确目标须已有明确的 Catalog 批准要求；没有要求时显示查询失败，不能靠此页面自动纳管或初始化。原编号通过公开 URL 保留，刷新只读找回；原命令参数丢失后不从当前状态重构旧 POST。

已完成前端最小门禁：`make test-catalog-frontend` 退出码 0，23 个文件／119 个用例及 Vite 生产构建通过（既有 chunk 大小提示保留）。新增交互实际挂载 SFC，API 合约覆盖结构路径、无损 Engine ID、字符串版本及三字段正式正文。该证据属于确定性前端测试，不是本次新办理链路的浏览器 T3 或真实双服务 Online T4。

本轮模块证据：使用 Infra 实际映射 25432 及 `addp_test` 显式 DSN 运行 `make test-module MODULE=catalog`，退出码 0，平台 T0、Go T1、当时 118 个前端用例／构建及全部 Catalog PostgreSQL T2 通过；之后仅补充公开编号错误态，最新前端门禁为上面的 119 个用例。`make test-module MODULE=system` 的平台 T0、API 与 engineaccess Go T1 通过，但完整 Go T1 被既有 `TestRedisRegistrationStoresOnlyConnectionCapabilities` 阻断：并行 Redis 插件已声明目录／原生读取能力，测试仍断言 `CatalogModel == nil`。未改动该并行能力或测试，不将完整 System 模块门禁标为通过。另以 `addp_iam_test` 运行标准 `system-iam-postgres-gate.sh --package api`，退出码 0，本轮精确观察、真实授权／管理委派及锁等待到期用例通过。

补充门禁：`ADDP_SYSTEM_POSTGRES_TEST_DSN='postgres://addp:addp_password@127.0.0.1:25432/addp_iam_test?sslmode=disable' make test-system-iam-postgres` 退出码 0，IAM、OAuth、API、迁移、engineaccess 及 repository 的完整 PostgreSQL T2 通过；`make test-system-frontend` 退出码 0，18 个文件／91 个单元用例、39 个既有浏览器用例及 Vite 构建通过。该 System 浏览器证据不能外推为 Catalog 新办理界面的 T3；本次新办理链路的 Catalog 浏览器 T3 与真实双服务 Online T4 未运行，不能计为通过。

后续优先恢复 System 完整模块门禁：由并行 Redis 能力变更同步其注册契约测试。业务主线下一阶段是以 System 原受理回执推进幂等 Grant 写入，仍须核验原五分钟窗口、拟授予期限及当前办理资格，不能把本轮的 accepted 显示当作内容访问已经生效。
