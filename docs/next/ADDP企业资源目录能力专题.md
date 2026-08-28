# ADDP 企业资源目录与 Catalog 模块专题

更新时间：2026-08-28

状态：阶段 0 至阶段 6 的架构、实现与确定性门禁已收口；企业资源目录、资产目录和引擎资源树的命名边界及 `AssetCategory` 单路径迁移已完成；专用 macOS T4 和停机恢复场景作为外部条件验证继续保留，不阻塞本专题完成

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

## 二十二、当前推进状态

| 工作项 | 状态 | 说明 |
| --- | --- | --- |
| 企业目录根因和目标 | 已确认 | 缺少跨专业目录统一身份、关联和发现层 |
| 独立 Catalog 模块方向 | 已确认 | 不再比较 Meta / Asset 扩展路线 |
| CatalogEntry 自动建档 | 已确认 | 所有正式持久化 DataItem 自动创建 |
| Meta / Standard / Catalog 边界 | 已确认 | 技术事实、语义定义、资源关联分别归属 |
| Department / Project Group / Workspace 边界 | 已完成 | 组织与协作不决定目录身份；评估结论为当前不新增统一 Workspace |
| 搜索所有权 | 已确认 | Meta、Catalog、Manager、Asset 分别拥有对应搜索 |
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

阶段 5 的专业来源接入、动态当前事实、基础联邦关系视图、默认治理目录、权限资源盘点、企业目录上下文导航、“待归类”与“待分配部门”虚拟治理入口、七维治理覆盖率、治理缺口处置、联邦来源身份导航、列表分面、编目/治理队列名称选择器、Manager 精确定位和统一 Workspace 评估，以及阶段 6 的显式成员批量治理均已完成。Project Group 组合接口与无成员空状态已通过，真实成员名称只缺有 membership 的验收身份证据。专用 Runner 首跑 `enterprise-catalog-publishing` 以及停机追赶、显式重绑、System 恢复等专项 T4 已纳入统一验证体系，它们需要专用 User Token、Tenant、Fixture Engine、Domain、Department 或服务停机窗口，属于外部条件验证而非本专题实现缺口。技术来源详情中的 fingerprint、Item ID 等继续只保留在明确的技术溯源区。统一 Workspace 评估结论是当前不新增；只有第 10.5 节的端到端触发条件成立时才重开。

## 二十三、后续会话接力清单

新会话应以本专题和 [企业资源目录实现规范](../spec/addp企业资源目录实现规范.md) 为事实基线，不重新讨论已确认的模块边界，也不要恢复 Meta / Standard 反向投影、Catalog owner 全量副本或默认平铺全部 DataItem 的旧方向。

1. 先检查工作区和运行态，保留其他并行改造；本轮统一重启与主要目录交互验收已经完成，不要重复重启或重做已通过项。
2. 治理覆盖率、Meta 血缘邻居和 Model 专业节点的来源身份导航已完成运行态验收；后续会话不要重复重启或重复制造样例，只有相关契约再次变更时才重跑这些路径。
3. 若能提供具有有效 Project Group membership 的验收身份，只补验集合筛选、创建选择器和详情中的项目组名称；不为验收临时修改组织数据，也不把名称加入 AuthContext 或 Catalog 副本。
4. 在专用 Runner 配置 User Token、Tenant、Fixture Engine、Domain、Department 后执行 `enterprise-catalog-publishing`，回填自动建档、完整编目、失效引用治理与清理证据。
5. 在明确的停机窗口执行来源 missing、停机追赶、显式重绑、System 恢复和 owner `503` 专项 T4；不得在共享开发服务上擅自停止 System、Standard、Meta 或 Catalog。
6. 推荐继任项的名称选择器已有定向门禁；当前 Tenant 只有 998 条 `discovered` 条目，没有可进入弃用流程的 `curated|certified` 样本。后续只在自然产生合适治理状态样本后补运行态证据，不为验收篡改 CatalogEntry。
7. 如需重跑完整 Catalog 模块门禁，显式使用 `CATALOG_POSTGRES_TEST_DSN='postgres://addp:addp_password@127.0.0.1:15432/addp_test?sslmode=disable' make test-module MODULE=catalog`；不得新建本地测试 database。
8. System 全量 IAM PostgreSQL 门禁与全仓授权门禁已在 AssetCategory permission migration 109 收口后通过。后续若再次失败，应按对应 owner 根因处理，不得放宽计数、跳过测试或恢复 `asset.catalog.*` 兼容权限。
9. 治理缺口处置、企业目录上下文导航、“待归类”“待分配部门”和七维治理覆盖率均已完成运行态验收；后续会话不要重复重启或重做共享环境点击，只在相关契约再次变化时重跑。
10. 旧 `accountability` 已从 API 响应、Swagger、URL 和页面删除，只在负向测试与规范删除说明中出现；不得恢复复合维度或兼容 query。
11. 统一 Workspace 评估已收口为“当前不新增”；不得为了形式上统一而添加空壳模块、`workspace_id` 或把引擎 `SpatialWorkspace` 与企业协作概念合并；只在第 10.5 节触发条件成立时重开文档优先评估。
12. 显式成员批量治理已经完成代码、Swagger、PostgreSQL、前端、Online 契约登记和本机真实写入验收；后续不要增加“全部匹配筛选”、手工 owner ID、Tenant 级 `revision` 或逐条部分成功路线。专用 macOS 只需随既有 `enterprise-catalog-publishing` 做当前页选择和对话框的周期回归，不在永久 fixture 上重复提交批量写入。
13. **已完成**：正常统一重启后只读确认 Asset schema 中不再存在 `catalogs_id_seq`、`catalogs_pkey`、`idx_asset_catalogs_*` 或 `idx_asset_assets_catalog_id`；迁移由 Asset 启动自动应用，没有手工改库或单独接管整套服务。
14. **已完成**：AssetCategory 父目录选择器、完整更新契约、所有前端更新调用方、同 Tenant 目录图事务锁、环路/跨 Tenant/同级重名/版本冲突校验、统一 Asset 门禁，以及真实页面“移动→移回→刷新”和“重命名→恢复”验收均已通过；测试目录已恢复原名称与层级，树高亮与详情状态同步，浏览器无 warning/error。后续不要新增 `/move` 路由、局部更新负载、手工 ID 输入或第二份目录关系。
