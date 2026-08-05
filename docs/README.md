# ADDP 文档总览

这是 `docs/` 目录的入口页，用来快速找到平台概念、规范、规划和运行指南。

## 目录定位

- `concepts/`：概念层面介绍，解释平台“是什么”“为什么这样设计”。
- `spec/`：实现层面规范，定义开发约束、接口边界和统一规则。
- 模块目录下的 `README.md`、`CLAUDE.md`、`docs/`：记录只在单个模块内部生效的技术实现、helper 边界和局部流程。
- `skills/`：给 AI 自动使用的技能文档。
- `pr/`：宣传文章与对外材料。
- `plan/`：尚未完成的大型规划和设计稿。
- `next/`：正在推进中的跟进记录。
- `guide/`：部署、开发、排障等操作指南。

## 推荐阅读顺序

1. [核心概念总览](concepts/addp核心概念关系图.md)
2. [账号与权限体系](concepts/addp账号与权限体系图.md)
3. [模块架构图](concepts/addp模块架构图.md)
4. [国际化体系图](concepts/addp国际化体系图.md)
5. [开发原则](spec/addp开发原则.md)
6. [国际化开发规范](spec/addp国际化开发规范.md)
7. [前端路由与可恢复状态规范](spec/addp前端路由与可恢复状态规范.md)
8. [部署和开发步骤](guide/addp部署和开发步骤.md)
9. [常见故障排查](guide/addp常见故障排查.md)

## 常用入口

- [概念文档](concepts/)
- [规范文档](spec/)
- [术语表](concepts/addp术语表.md)
- [账号与权限体系](concepts/addp账号与权限体系图.md)
- [授权上下文规范](spec/addp授权上下文规范.md)
- [权限与角色发布规范](spec/addp权限与角色发布规范.md)
- [OAuth 授权规范](spec/addp%20OAuth授权规范.md)
- [System IAM 数据模型与迁移规范](../system/docs/IAM数据模型与迁移规范.md)
- [System OAuth 与 Fosite 实现说明](../system/docs/OAuth与Fosite实现说明.md)
- [IAM 三员初始化操作指南](guide/addp%20IAM三员初始化操作指南.md)
- [IAM owner 资源授权与 Asset 衔接设计](next/addp-IAM%20owner资源授权与Asset衔接设计.md)
- [IAM OIDC 启用设计](next/addp-IAM%20OIDC启用设计.md)
- [IAM 外部 IdP 与账号供应设计](next/addp-IAM外部IdP与账号供应设计.md)
- [元数据体系图](concepts/addp元数据体系图.md)
- [数据项体系图](concepts/addp数据项体系图.md)
- [数据类型和格式体系图](concepts/addp数据类型和格式体系图.md)
- [元数据扫描机制规范](spec/addp元数据扫描机制规范.md)
- [任务体系规范](spec/addp任务体系规范.md)
- [资源回收（Cleanup）体系规范](spec/addp-cleanup体系规范.md)
- [数据类型与文件格式扩展指南](spec/addp数据类型与文件格式扩展指南.md)
- [数据项探测器规范](spec/addp数据项探测器规范.md)
- [元数据 attributes 规范](spec/addp元数据attributes规范.md)
- [数据类型与格式能力规范](spec/addp数据类型与格式能力规范.md)
- [内容 I/O 抽象规范](spec/addp内容IO抽象规范.md)
- [内置数据类型与文件格式规范](spec/addp内置数据类型与文件格式规范.md)
- [国际化体系图](concepts/addp国际化体系图.md)
- [国际化开发规范](spec/addp国际化开发规范.md)
- [前端路由与可恢复状态规范](spec/addp前端路由与可恢复状态规范.md)
- [规划文档](plan/)
- [跟进文档](next/)
- [技能文档](skills/)

## AI 智能体能力开放主题

处理 ADDP Agent、平台级 Skill、Tool Manifest、Codex / Hermes Agent 接入、Python SDK / CLI、AG-UI、A2UI、智能体受委托身份或操作审批时，建议按以下顺序阅读：

1. [术语表](concepts/addp术语表.md)：统一 Agent Runtime、AgentRun、Skill、Tool、ResultRef、Interaction、Presentation 和评测术语。
2. [智能体 Tool 开放规范](spec/addp智能体Tool开放规范.md)：确认 Tool Manifest、ToolExecutor、Python SDK、CLI 和 Adapter 的唯一能力开放路径。
3. [ADDP Skill 规范](skills/addp-Skill规范.md)：确认平台级 Skill 的目录、粒度、渐进式加载和 Tool 引用。
4. [智能体交互协议规范](spec/addp智能体交互协议规范.md)：确认 AgentRun、AG-UI、消息 parts、Interaction、ResultRef、Presentation 和 A2UI Catalog。
5. [智能体评测规范](spec/addp智能体评测规范.md)：确认场景、在线证据、统一门禁、报告比较和正式发布评测基线。
6. [授权上下文规范](spec/addp授权上下文规范.md)与 [OAuth 授权规范](spec/addp%20OAuth授权规范.md)：确认 AuthContext、OAuth Scope、委托身份和 owner 写审批。
7. [ADDP API 设计规范](spec/addp-API设计规范.md)：确认 AI Agent 友好 API、`x-ai-hint` 和错误格式。
8. [Agent 模块说明](../agent/CLAUDE.md)、[common-python 模块说明](../common-python/CLAUDE.md)与 [common-frontend 说明](../common-frontend/README.md)：查看当前实现。
9. [ADDP 智能体能力开放体系专题](next/ADDP智能体能力开放体系专题.md)：查看架构决策、阶段实施历史和延期条件。
10. [铁路占耕地面积计算 AI 自动化实验](plan/铁路占耕地面积计算-AI自动化实验.md)：查看 `workflow-analysis` 的业务实验背景。

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

## 栅格、TIFF / COG 与空间快显主题

处理 TIFF / GeoTIFF / COG 识别、栅格快显、COG 生成结果或后续栅格算子时，建议按以下顺序阅读：

1. [数据类型和格式体系图](concepts/addp数据类型和格式体系图.md)：确认 TIFF / GeoTIFF 仍属于 `data_type=media`、`format=tiff`，空间事实进入 `capabilities.spatial`。
2. [数据项探测器规范](spec/addp数据项探测器规范.md)：确认 `tif + tfw/hdr/aux.xml/ovr` 等 related refs 的数据项边界。
3. [元数据 attributes 规范](spec/addp元数据attributes规范.md)：确认 `format_info.tiff`、`type_info.media`、`capabilities.spatial` 的字段归属。
4. [Manager 快显概念说明](../manager/docs/快显概念说明.md)：确认 `direct_tiff_client`、`client_cog_render`、`raster_cog` 与矢量快显的概念边界。
5. [Manager 快显实现规范](../manager/docs/快显实现规范.md)：确认 Quick View capability、COG 内容接口、前端 geotiff.js 消费、底图和 related ref 行为。
6. [Manager raster_cog 表](../manager/docs/tables/raster_cog表.md) 与 [raster_cog_tasks 表](../manager/docs/tables/raster_cog_tasks表.md)：确认 COG 结果和任务定义的字段、状态和生命周期。
7. [栅格算子体系后续专题](next/栅格算子体系后续专题.md)：查看 `raster_reproject`、`raster_clip`、`raster_statistics`、`raster_to_tiles` 等第一阶段之外的后续算子规划。

## SuperMap 工作流运行时主题

处理 SuperMap 数据格式、空间算法、`supermap_workflow` 工作流运行时、SuperMap 算子接入或后续血缘设计时，建议按以下顺序阅读：

1. [术语表](concepts/addp术语表.md)：确认 ADDP Operator、Workflow Runtime、SuperMap iObjects C++ 和 `supermap_workflow` 的术语边界。
2. [引擎体系图](concepts/addp引擎体系图.md)：确认 `EnginePlugin + WorkflowRuntimeProvider + HTTP runtime` 的模块边界。
3. [ADDP 工作流计算引擎接口规范](spec/addp工作流计算引擎接口规范.md)：确认工作流运行时必须实现的统一 HTTP 协议。
4. [SuperMap Workflow Engine README](../engines/supermap-workflow/README.md)：查看 C++ Runtime、算子、镜像构建、SDK 母版、裁剪结果和验证方式。
5. [工作流运行时结果产物与血缘专题](next/工作流运行时结果产物与血缘专题.md)：处理 Runtime 节点事件、调用方输入资源事实、`produced_targets`、统一执行记录和血缘采集边界时阅读。
6. [Meta 模块血缘扩展设计](plan/meta模块血缘扩展设计.md)：处理 SuperMap runtime 血缘事件落库和资产级血缘关系时阅读。

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

## 资源回收与生命周期主题

处理系统级资源回收、跨模块 owner 边界、派生产物回收、生命周期事件或 cleanup result 时，建议按以下顺序阅读：

1. [资源回收（Cleanup）体系规范](spec/addp-cleanup体系规范.md)：确认 System coordinator、模块 executor、scan / execute、result 模型和禁止规则。
2. [术语表](concepts/addp术语表.md)：确认 cleanup、owner module、artifact state、physical artifact 等术语。
3. [任务体系规范](spec/addp任务体系规范.md)：确认 cleanup 不纳入 TaskProvider，也不进入 Orchestrator 编排。
