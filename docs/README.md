# ADDP 文档总览

这是 `docs/` 目录的入口页，用来快速找到平台概念、规范、规划和运行指南。

## 目录定位

- `concepts/`：概念层面介绍，解释平台“是什么”“为什么这样设计”。
- `spec/`：实现层面规范，定义开发约束、接口边界和统一规则。
- 模块目录下的 `README.md`、`CLAUDE.md`、`docs/`：记录只在单个模块内部生效的技术实现、helper 边界和局部流程。
- 仓库根目录 `skills/`：平台共享任务方法，由 Agent Runtime 按需加载；本目录 `docs/skills/` 保存 Skill 规范与使用说明。
- `pr/`：宣传文章与对外材料。
- `plan/`：尚未实施或待确认的远期设计；已完成方案归入正式文档，历史过程由 Git 和 CI 保存。
- `next/`：正在推进的未完成设计、待确认事项和验收缺口；已实现内容迁入 concepts/spec，不追加过程流水。
- `guide/`：部署、开发、排障等操作指南。

## 推荐阅读顺序

1. [核心概念总览](concepts/addp核心概念关系图.md)
2. [账号与权限体系](concepts/addp账号与权限体系图.md)
3. [模块架构图](concepts/addp模块架构图.md)：平台总览、模块职责、共享能力与运行机制的专题入口。
4. [国际化体系图](concepts/addp国际化体系图.md)
5. [开发原则](spec/addp开发原则.md)
6. [新模块开发指南](spec/addp新模块开发指南.md#阅读导航)：按篇首导航选择接入步骤、排查与交付检查清单。
7. [测试与验收规范](spec/addp测试与验收规范.md)
8. [国际化开发规范](spec/addp国际化开发规范.md)
9. [前端路由与可恢复状态规范](spec/addp前端路由与可恢复状态规范.md)
10. [部署和开发步骤](guide/addp部署和开发步骤.md)
11. [常见故障排查](guide/addp常见故障排查.md)
12. [开发服务生命周期与构建身份规范](spec/addp开发服务生命周期与构建身份规范.md)

## 常用入口

- [概念文档](concepts/)
- [规范文档](spec/)
- [术语表](concepts/addp术语表.md)
- [核心对象 ER 图（元模型）](concepts/addp核心对象ER图（元模型）.md)：对象说明、模块关系图与引用约束的专题入口。
- [账号与权限体系](concepts/addp账号与权限体系图.md)
- [授权上下文规范](spec/addp授权上下文规范.md)
- [权限与角色发布规范](spec/addp权限与角色发布规范.md)
- [OAuth 授权规范](spec/addp%20OAuth授权规范.md)
- [System IAM 数据模型与迁移规范](../system/docs/IAM数据模型与迁移规范.md)
- [System OAuth 与 Fosite 实现说明](../system/docs/OAuth与Fosite实现说明.md)
- [IAM 三员初始化操作指南](guide/addp%20IAM三员初始化操作指南.md)
- [IAM OIDC 启用设计](next/addp-IAM%20OIDC启用设计.md)
- [IAM 外部 IdP 与账号供应设计](next/addp-IAM外部IdP与账号供应设计.md)
- [元数据体系图](concepts/addp元数据体系图.md)
- [企业资源目录体系图](concepts/addp企业资源目录体系图.md)
- [企业资源目录实现规范](spec/addp企业资源目录实现规范.md)
- [数据项体系图](concepts/addp数据项体系图.md)
- [数据类型和格式体系图](concepts/addp数据类型和格式体系图.md)
- [元数据扫描机制规范](spec/addp元数据扫描机制规范.md)
- [任务体系规范](spec/addp任务体系规范.md)：统一原则、核心对象和接入清单；任务、执行、协议与 owner 专题见 `tasks/` 导航。
- [调度与任务编排统一改造专题](next/ADDP调度与任务编排统一改造专题.md)
- [数据服务消费与数据应用规范](spec/addp数据服务消费与数据应用规范.md)
- [Workbench 后续事项与验收](next/ADDP%20Workbench数据服务消费与数据应用专题.md)
- [数据质量规范](spec/addp数据质量规范.md)
- [数据血缘能力规范](spec/addp数据血缘能力规范.md)
- [资源回收（Cleanup）体系规范](spec/addp-cleanup体系规范.md)
- [数据类型与文件格式扩展指南](spec/addp数据类型与文件格式扩展指南.md)
- [数据项探测器规范](spec/addp数据项探测器规范.md)
- [元数据 attributes 规范](spec/addp元数据attributes规范.md)
- [数据类型与格式能力规范](spec/addp数据类型与格式能力规范.md)
- [内容 I/O 抽象规范](spec/addp内容IO抽象规范.md)
- [内置数据类型与文件格式规范](spec/addp内置数据类型与文件格式规范.md)：总览与通用约束；各格式按主题在 `builtin-formats/` 中维护。
- [国际化体系图](concepts/addp国际化体系图.md)
- [国际化开发规范](spec/addp国际化开发规范.md)
- [前端路由与可恢复状态规范](spec/addp前端路由与可恢复状态规范.md)
- [数据库无关分析计算契约（目标设计）](spec/addp引擎插件接口规范.md#数据库无关分析计算契约)
- [测试与验收规范](spec/addp测试与验收规范.md)：统一原则与标准入口；分层规范、Owner 门禁、Online 场景和证据索引见其阅读导航。
- [Online 专用 Runner 首次验收待办](next/ADDP统一测试与Online验收体系方案.md)
- [GitHub 外部治理与 T4 Runner 待办](next/ADDP持续集成体系改进专题.md)
- [模块启动、就绪与注册治理待办](next/ADDP模块与引擎注册治理专题.md)
- [模块服务运行日志规范](spec/addp模块服务运行日志规范.md)
- [模块服务运行日志验收与后续事项](next/ADDP模块服务运行日志设计.md)
- [平台运行观测规范](spec/addp平台运行观测规范.md)
- [平台运行监控后续事项](next/ADDP平台运行监控与可观测性设计.md)
- [Manager 前端浏览器回归测试专题](next/Manager前端浏览器回归测试专题.md)
- [ADDP 企业资源目录与 Catalog 模块专题](next/ADDP企业资源目录能力专题.md)
- [Engine Catalog 概念与读取边界](concepts/addp引擎体系图.md#实时-engine-catalog元数据快照和数据预览的边界)
- [本体定义与发布规范](spec/addp本体定义与发布规范.md)
- [Ontology 后续设计与验收](next/ADDP%20Ontology最小设计契约.md)
- [Outdoor 业务语义](concepts/Outdoor业务语义.md)
- [Outdoor 数据治理与指标实现规范](spec/Outdoor数据治理与指标实现规范.md)
- [栅格计算与成果契约规范](spec/addp栅格计算与成果契约规范.md)
- [规划文档](plan/)
- [跟进文档](next/)
- [技能文档](skills/)

## 远期设计入口

以下仅是未实施设计或待补证范围，当前支持能力以概念、规范和模块文档为准：

- [跨网离线表同步设计](plan/跨网离线表同步设计.md)：单向包链、源侧封包与目标提交、恢复和实施前决策。
- [Manager 第三方预览扩展](plan/addpManager内容预览插件能力构想.md)、[属性扩展声明](plan/addp第三方插件扩展声明构想.md)、[跨 Registry 能力发现](plan/addpRegistry与能力发现层构想.md)：未确认的扩展方案。
- [数据导入后续事项](plan/数据导入流程设计.md)：上传源保留、失败补偿及批量/大文件增强。
- [文件系统引擎后续事项](plan/nfs-storage-engine-plan.md)、[文件与目录表格验收](plan/湖仓一体化第二阶段详细工作规划.md)：首版之外的接入与组合证据。
- [Graph 与 Agent 后续方向](plan/development_analysis_and_roadmap.md)、[铁路两侧耕地面积实验](plan/铁路占耕地面积计算-AI自动化实验.md)：待选定范围的能力与业务场景。

## AI 智能体能力开放主题

处理 ADDP Agent、平台级 Skill、Tool Manifest、Codex / Hermes Agent 接入、Python SDK / CLI、AG-UI、A2UI、智能体受委托身份或操作审批时，建议按以下顺序阅读：

1. [术语表](concepts/addp术语表.md)：统一 Agent Runtime、AgentRun、Skill、Tool、ResultRef、Interaction、Presentation 和评测术语。
2. [智能体 Tool 开放规范](spec/addp智能体Tool开放规范.md)：确认 Tool Manifest、ToolExecutor、Python SDK、CLI 和 Adapter 的唯一能力开放路径。
3. [ADDP Skill 规范](skills/addp-Skill规范.md)：确认平台级 Skill 的目录、粒度、渐进式加载和 Tool 引用。
4. [智能体交互协议规范](spec/addp智能体交互协议规范.md)：确认 AgentRun、AG-UI、消息 parts、Interaction、ResultRef、Presentation 和 A2UI Catalog。
5. [智能体评测规范](spec/addp智能体评测规范.md)：确认场景、在线证据、统一门禁、报告比较和正式发布评测基线。
6. [授权上下文规范](spec/addp授权上下文规范.md)与 [OAuth 授权规范](spec/addp%20OAuth授权规范.md)：确认 AuthContext、OAuth Scope、委托身份和 owner 写审批。
7. [API Agent 友好性](spec/api/AI-Agent友好性.md)：确认 `x-ai-hint` 与 HTTP 重试提示；通用错误格式见[响应与错误](spec/api/响应与错误.md)。
8. [Agent 模块说明](../agent/CLAUDE.md)、[common-python 模块说明](../common-python/CLAUDE.md)与 [common-frontend 说明](../common-frontend/README.md)：查看当前实现。
9. [ADDP 智能体能力开放体系专题](next/ADDP智能体能力开放体系专题.md)：查看未完成能力和验收缺口。
10. [铁路占耕地面积计算 AI 自动化实验](plan/铁路占耕地面积计算-AI自动化实验.md)：查看尚未闭合的空间计算业务场景与验收问题。
11. [本体定义与发布规范](spec/addp本体定义与发布规范.md)：查看已实现 Tenant/平台定义、PG 权威修订、FalkorDB 投影及 Agent 消费；绑定与真实求值见[后续设计](next/ADDP%20Ontology最小设计契约.md)。

AI 模型接入、在线厂商账号、内网模型服务、模型档案、场景绑定或推理密钥管理，应先阅读 [ADDP AI 推理接口规范](spec/addp%20AI推理接口规范.md)。Agent 与 Copilot 只消费统一推理数据面，不拥有 Provider 或 API Key。

## 资源定位、资源树与数据检索主题

处理资源树展示、搜索定位、数据检索结果跳转或预览定位时，建议按以下顺序阅读：

1. [数据项体系图](concepts/addp数据项体系图.md)：确认 engine、node、data item、资源树和数据检索的概念边界。
2. [术语表](concepts/addp术语表.md)：确认 ResourceLocator、resource tree、data retrieval 等术语。
3. [路径统一和指纹计算](spec/addp路径统一和指纹计算.md)：确认 full_name、fingerprint 和 ResourceLocator 的统一规则。
4. [存储引擎路径体系规范](spec/addp存储引擎路径体系规范.md)：确认对象存储、文件系统和数据库类引擎的路径规则。
5. [共享模块介绍](concepts/addp共享模块介绍.md)：确认 `common/resourcetree`、Meta resource-tree API、`ResourceTree` 和 `ResourceTreePicker` 的职责边界。
6. [元数据 attributes 规范](spec/addp元数据attributes规范.md)：确认 locator 不作为 attributes 标准事实持久化。
7. [Manager 数据预览与资源树实现规范](../manager/docs/数据预览与资源树实现规范.md)：确认 Manager 资源树、预览 API、PreviewResolver 和 PreviewProvider 当前实现契约。
8. [Manager 数据预览语义协议](../manager/docs/数据预览语义协议.md)：确认 `content.kind`、`preview_material` 和 `frontend_renderer` 等预览响应语义。
9. [Manager 数据剖析规范](../manager/docs/数据剖析规范.md)：确认表格数据剖析的 Manager owner、按需采样、ad-hoc execution、结果和 Meta scan 边界。
10. [Manager 快显概念说明](../manager/docs/快显概念说明.md)：确认快显、矢量物化视图和瓦片缓存的概念边界。
11. [Manager 快显实现规范](../manager/docs/快显实现规范.md)：确认矢量物化视图任务、结果、外部 3857 目标、瓦片缓存和 UI 引导闭环。
12. [Manager 向量化概念说明](../manager/docs/向量化概念说明.md)：确认 Manager 资源树 item / node 向量化、向量化任务和向量化结果的模块内概念边界。
13. [Manager 向量化能力说明](../manager/docs/向量化能力说明.md)：确认 Manager 向量化结果字段、状态枚举、API、执行配置和 UI 契约。

## Workbench 数据服务消费与数据应用主题

处理已发布数据服务的动态查询、参数输入、表格/图表/地图展示、Data Application、BI 能力边界或大屏展示模式时，建议按以下顺序阅读：

1. [术语表](concepts/addp术语表.md)：确认 Workbench、Data Application、Data Application Component 和 Consumer Descriptor 等稳定平台术语。
2. [模块架构图](concepts/addp模块架构图.md)：确认 Console、System、Service、Asset 和 Portal 的现有边界。
3. [数据服务体系图](concepts/addp数据服务体系图.md)：确认查询服务、瓦片服务和外部服务注册的发布与执行职责。
4. [API 设计规范](spec/addp-API设计规范.md)：确认结构化查询、cursor 分页、Bearer、Service Principal 和 BFF 边界。
5. [企业资源目录实现规范](spec/addp企业资源目录实现规范.md)：确认 CatalogEntry、Asset 选源、授权来源与 owner 最终授权边界。
6. [数据服务消费与数据应用规范](spec/addp数据服务消费与数据应用规范.md)：查看 Service 契约、应用创作/运行与共享 renderer；真实 BI 和剩余验收见[后续事项](next/ADDP%20Workbench数据服务消费与数据应用专题.md)。
7. [Outdoor 数据治理与指标实现规范](spec/Outdoor数据治理与指标实现规范.md)：首个业务样板的已确认口径与链路；业务规则不进入 Workbench 平台默认值。

## 企业资源目录主题

处理业务元数据、CatalogEntry、DataItem 自动建档、语义关联、责任、企业目录搜索、来源重绑或 Asset 选源时，建议按以下顺序阅读：

1. [术语表](concepts/addp术语表.md)：确认 Engine Catalog、Enterprise Catalog、CatalogEntry 和 CatalogComponent 的边界。
2. [企业资源目录体系图](concepts/addp企业资源目录体系图.md)：确认 Meta、Standard、Catalog、Manager、Asset、Portal 的事实所有权和端到端主线。
3. [企业资源目录实现规范](spec/addp企业资源目录实现规范.md)：确认身份、来源变化、状态机、权限、API、搜索和迁移约束。
4. [账号与权限体系](concepts/addp账号与权限体系图.md)：确认 Department、Project Group、User 和 AuthContext。
5. [企业资源目录与 Catalog 模块专题](next/ADDP企业资源目录能力专题.md)：查看尚未完成的设计、验收和后续工作；稳定契约以正式文档为准。

## 数据安全与隐私保护主题

处理敏感数据识别、安全分类分级、保护纳管、动态遮盖、Owner 出口保护或 Catalog 安全事实联邦展示时，建议按以下顺序阅读：

1. [术语表](concepts/addp术语表.md)：确认 Security module、专业资源引用、Finding、Assessment、Policy 和 ProtectionProjection 等稳定术语。
2. [数据安全与隐私保护体系图](concepts/addp数据安全与隐私保护体系图.md)：确认 Security、Meta、Catalog、Standard、IAM 和资源 Owner 的事实与执行边界。
3. [数据安全与隐私保护实现规范](spec/addp数据安全与隐私保护实现规范.md)：确认纳管状态机、受控发现、投影 v1、变化流、Owner 失效关闭和首期 API 契约。
4. [账号与权限体系](concepts/addp账号与权限体系图.md)：确认 AuthContext、Service Principal、Resource Grant / Policy 与 Security 保护效果的正交关系。
5. [企业资源目录体系图](concepts/addp企业资源目录体系图.md)：确认 Catalog 是后续企业目录身份与联邦展示层，不是 Security 发现或保护生效的前置。
6. [数据安全与隐私保护体系专题](next/ADDP数据安全与隐私保护体系专题.md)：查看未完成保护能力与真实验收边界。

## 栅格、TIFF / COG 与空间快显主题

处理 TIFF / GeoTIFF / COG 识别、栅格快显、COG 生成结果或后续栅格算子时，建议按以下顺序阅读：

1. [数据类型和格式体系图](concepts/addp数据类型和格式体系图.md)：确认 TIFF / GeoTIFF 仍属于 `data_type=media`、`format=tiff`，空间事实进入 `capabilities.spatial`。
2. [数据项探测器规范](spec/addp数据项探测器规范.md)：确认 `tif + tfw/hdr/aux.xml/ovr` 等 related refs 的数据项边界。
3. [元数据 attributes 规范](spec/addp元数据attributes规范.md)：确认 `format_info.tiff`、`type_info.media`、`capabilities.spatial` 的字段归属。
4. [Manager 快显概念说明](../manager/docs/快显概念说明.md)：确认 `direct_tiff_client`、`client_cog_render`、`raster_cog` 与矢量快显的概念边界。
5. [Manager TIFF 与 COG 快显](../manager/docs/quick-view/TIFF与COG.md)：确认 COG 内容接口、前端 geotiff.js 消费、底图和 related ref 行为；共用 Quick View capability 见[共用契约](../manager/docs/quick-view/共用契约.md#快显能力)。
6. [Manager raster_cog 表](../manager/docs/tables/raster_cog表.md) 与 [task_definitions 表](../manager/docs/tables/task_definitions表.md)：确认 COG 结果和统一派生任务定义的字段、状态和生命周期。
7. [栅格计算与成果契约规范](spec/addp栅格计算与成果契约规范.md)：查看已实现的通用栅格、成果写入、资源与验收契约；DEM 与规模缺口见[后续专题](next/栅格算子体系后续专题.md)。

## SuperMap 工作流运行时主题

处理 SuperMap 数据格式、空间算法、`supermap_workflow` 工作流运行时、SuperMap 算子接入或后续血缘设计时，建议按以下顺序阅读：

1. [术语表](concepts/addp术语表.md)：确认 ADDP Operator、Workflow Runtime、SuperMap iObjects C++ 和 `supermap_workflow` 的术语边界。
2. [引擎体系图](concepts/addp引擎体系图.md)：确认 `EnginePlugin + WorkflowRuntimeProvider + HTTP runtime` 的模块边界。
3. [ADDP 工作流计算引擎接口规范](spec/addp工作流计算引擎接口规范.md)：统一 HTTP 协议、算子、Develop 资源与执行、Runtime 接入及矢量/栅格契约的专题入口。
4. [SuperMap Workflow Engine README](../engines/supermap-workflow/README.md)：查看 C++ Runtime、算子、镜像构建、SDK 母版、裁剪结果和验证方式。
5. [数据血缘能力规范](spec/addp数据血缘能力规范.md)：确认统一执行事实、关系类型、Meta collector 和服务依赖边界。

## 数据血缘与服务依赖主题

处理数据项上游 / 下游关系、执行事实、字段级血缘、已发布服务依赖或血缘查看器时，建议按以下顺序阅读：

1. [数据血缘能力规范](spec/addp数据血缘能力规范.md)：确认唯一存储路线、Meta 归属、采集边界、时态和 API。
2. [术语表](concepts/addp术语表.md)：确认 lineage、lineage facts、published service、service dependency 和 field ref。
3. [数据项体系图](concepts/addp数据项体系图.md)：确认 data item 身份、字段边界和模块职责。
4. [任务体系规范](spec/addp任务体系规范.md)：确认 `common.task_executions` 与 `lineage_facts` 的执行结果契约。
5. [数据服务体系图](concepts/addp数据服务体系图.md)：确认服务发布版本和 source dependency 事实。
6. [共享模块介绍](concepts/addp共享模块介绍.md)：确认 `common-frontend/graph` 的 `LineageViewer` 嵌入边界。

## 三维模型、倾斜摄影与点云主题

处理 GLB / glTF、OSGB、OSGB Scene、3D Tiles、BIM、普通三维网格、点云或高斯泼溅格式时，建议按以下顺序阅读：

1. [数据类型和格式体系图](concepts/addp数据类型和格式体系图.md)：确认 `model_3d`、`point_cloud` 与 `gaussian_splat` 的数据类型边界。
2. [内置数据类型与文件格式规范](spec/addp内置数据类型与文件格式规范.md)：确认 GLB、3D Tiles、OSGB、OSGB Scene、IFC、LAS 和 PLY 的稳定规则。
3. [数据项探测器规范](spec/addp数据项探测器规范.md)：确认 whole-scope 场景、manifest、refs 和 claims 规则。
4. [元数据 attributes 规范](spec/addp元数据attributes规范.md)：确认 `type_info.model_3d`、`type_info.point_cloud`、`type_info.gaussian_splat` 和 `format_info.<format>` 的字段归属。
5. [Manager 三维模型、点云与高斯泼溅预览说明](../manager/docs/三维模型、点云与高斯泼溅预览说明.md)：确认三维模型、3D Tiles、点云、3DGS 基础预览、快显任务、结果状态和视角保存边界。
6. [Model3D Workflow](../engines/model3d-workflow/README.md)：处理 OSGB、glTF、FBX、OBJ、OSGB Scene 和 3DGS 转换运行时、对象存储 staging / publish 或 Docker 部署时阅读。
7. [PointCloud Workflow](../engines/pointcloud-workflow/README.md)：处理 LAS / LAZ / E57 / PCD / XYZ 转 COPC、PDAL 运行时绑定和点云工作目录配置时阅读。
8. [三维与点云后续路线](next/三维与点云后续路线.md)：处理 IFC 转换验证、3MX / SLPK / EPT / Potree / SPZ 等尚未落地能力时阅读。

## 数据类型与格式主题

处理数据类型、内容布局、文件格式、attributes、provider 或内容 I/O 抽象时，建议按以下顺序阅读：

1. [术语表](concepts/addp术语表.md)：先统一 data item、data type、format、detector 等术语。
2. [数据项体系图](concepts/addp数据项体系图.md)：确认 engine、node、data item 链条和模块职责边界。
3. [数据类型和格式体系图](concepts/addp数据类型和格式体系图.md)：确认数据类型、文件格式和横切能力的概念边界。
4. [元数据体系图](concepts/addp元数据体系图.md)：确认 Meta 扫描、detector、normalizer 和消费边界。
5. [元数据扫描机制规范](spec/addp元数据扫描机制规范.md)：确认 basic / deep、`scanned_depth`、`force`、扫描目标和跨模块触发规则。
6. [数据类型与格式能力规范](spec/addp数据类型与格式能力规范.md)：确认 FormatPlugin、info provider、content reader、provider / reader 矩阵和注册方式。
7. [数据项探测器规范](spec/addp数据项探测器规范.md)：确认 item 识别、主资源、组件和 claims 规则。
8. [元数据 attributes 规范](spec/addp元数据attributes规范.md)：确认 attributes 分区和字段归属。
9. [内容 I/O 抽象规范](spec/addp内容IO抽象规范.md)：确认读取抽象和调用链。
10. [内置数据类型与文件格式规范](spec/addp内置数据类型与文件格式规范.md)：对照首批内置格式的落地规则。
11. [数据类型与文件格式扩展指南](spec/addp数据类型与文件格式扩展指南.md)：按实现清单落地新 data type / format。

## 任务、执行、编排与监控主题

处理跨模块任务定义、执行记录、TaskProvider、Orchestrator 编排或 Monitor 执行监控时，建议按以下顺序阅读：

1. [任务体系规范](spec/addp任务体系规范.md)：确认任务定义、执行记录、触发来源、TaskProvider 和监控边界。
2. [任务编排体系图](concepts/addp任务编排体系图.md)：理解任务级 DAG 和跨模块编排概念。
3. [监控与执行体系图](concepts/addp监控与执行体系图.md)：理解 Monitor 与 `common.task_executions` 的关系。
4. [元数据扫描机制规范](spec/addp元数据扫描机制规范.md)：处理 Meta ScanTask 与 execution 时阅读。
5. [Transfer 任务语义与同步模式](../transfer/docs/transfer-任务语义与同步模式.md)：处理 Transfer 全量、增量、持续同步、状态、重试、进度或日志语义时阅读。
6. [Transfer 后续能力清单](next/transfer后续能力清单.md)：评估尚未实现的同步模式或运行时能力时阅读。
7. [ArcGIS SDE 支持设计](next/ArcGIS%20SDE支持设计.md)：处理 Oracle Enterprise Geodatabase workspace、非版本化要素类读写、traditional versioning 逻辑变化源或后续 branch versioning 时阅读。

## 数据质量主题

处理数据元质量规则、规则应用、质量检查、评分、问题工单或 Quality 执行链时，建议按以下顺序阅读：

1. [数据质量规范](spec/addp数据质量规范.md)：确认规则契约、模块边界、PostgreSQL 方言、执行授权、评分与问题状态机。
2. [术语表](concepts/addp术语表.md)：确认质量规则、规则应用、质量检查、质量分和质量问题术语。
3. [任务体系规范](spec/addp任务体系规范.md)：确认 execution 状态、TaskProvider、父子 execution 和持久执行要求。
4. [授权上下文规范](spec/addp授权上下文规范.md)：确认 User AuthContext 与 Execution Authorization 边界。
5. [Standard 模块说明](../standard/CLAUDE.md)与 [Quality 模块说明](../quality/CLAUDE.md)：查看当前 owner 实现。

## 数据标准与建模主题

处理业务域、数据元、维度层级、指标、业务实体、逻辑模型、数仓分层、Mermaid 模型导入或 Standard 引用约束时，建议按以下顺序阅读：

1. [术语表](concepts/addp术语表.md)：确认数据标准、业务实体、逻辑模型和数仓分层术语。
2. [Standard 模块说明](../standard/CLAUDE.md)：确认业务域、数据元和指标定义的事实所有权。
3. [Model 模块说明](../model/CLAUDE.md)：确认维度层级、指标实现、Model 当前 API、权限和验证方式。
4. [Model 概念与数据约束规范](../model/docs/model概念与数据约束规范.md)：确认聚合、生命周期、并发版本、Mermaid 集合修订和 Standard 引用删除屏障。

## 资源回收与生命周期主题

处理系统级资源回收、跨模块 owner 边界、派生产物回收、生命周期事件或 cleanup result 时，建议按以下顺序阅读：

1. [资源回收（Cleanup）体系规范](spec/addp-cleanup体系规范.md)：确认 System coordinator、模块 executor、scan / execute、result 模型和禁止规则。
2. [术语表](concepts/addp术语表.md)：确认 cleanup、owner module、artifact state、physical artifact 等术语。
3. [任务体系规范](spec/addp任务体系规范.md)：确认 cleanup 不纳入 TaskProvider，也不进入 Orchestrator 编排。
