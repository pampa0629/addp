# Manager 数据管理模块

> 全域数据平台的数据管理和预览服务

## 📖 文档说明

本页提供快速入门与功能概览；模块契约、专题分工及验证证据统一从[相关文档](#-相关文档)进入。

页内导航：[快速开始](#-快速开始) · [支持范围](#-支持的数据类型) · [API 速查](#-主要-api-端点) · [核心架构](#️-核心架构) · [常见问题](#-常见问题) · [导入导出专题](#导入导出专题) · [资源树与预览状态](#资源树与预览状态专题)。

### 导入导出专题

| 阅读目的 | 权威正文 |
| --- | --- |
| 核对私有导入来源与服务身份 | [导入来源与核验](./docs/数据预览与资源树实现规范.md#导入来源与核验) |
| 核对导出发起用户与待执行会话 | [导出来源核验](./docs/数据预览与资源树实现规范.md#导出来源核验) |
| 核对当前用户派发、进度与失败处理 | [导入派发与执行进度](./docs/数据预览与资源树实现规范.md#导入派发与执行进度) |
| 核对资源刷新与 Monitor 跳转顺序 | [导入完成导航](./docs/数据预览与资源树实现规范.md#导入完成导航) |
| 区分现行支持与后续事项 | [支持边界与后续事项](./docs/数据预览与资源树实现规范.md#导入导出支持边界与后续事项) |

身份、Permission、来源绑定和失败语义以对应正文为准；标准入口、覆盖范围与实际验收边界见[模块验证说明](./CLAUDE.md#开发与验证)。用例和导航存在不表示已完成真实链路验收。

## 🎯 核心功能

- **数据探查**: 基于 System 引擎目录与 Meta 已扫描资源树开展探查，实际范围见[支持的数据类型](#-支持的数据类型)
- **数据预览**: 按已注册 PreviewProvider 生成前端展示材料，格式与渲染边界见[预览语义协议](./docs/数据预览语义协议.md#推荐组合矩阵)
- **数据剖析**: 对表格型 data item 按需执行有界采样，展示字段完整性、基数、统计指标、分布和描述性观察
- **空间数据可视化**: 基础预览 + 快显模式 + 中小规模 FlatGeobuf + 大规模 MVT/瓦片缓存 + TIFF/COG 栅格快显
- **三维与点云快显**: GLB、3D Tiles、KSplat 和 COPC 受管 artifact 预览，源点云 COPC 直接基础预览
- **数据检索**: 基于 Meilisearch + 向量数据库的混合检索（全文检索 + 语义检索）
- **向量化**: 对可支持的数据项生成向量表示，支持资源树一次性执行、可调度向量化任务和语义检索消费
- **元数据展示**: 与 Meta 模块集成，展示资源树 node / item facts

## 🚀 快速开始

环境准备与根 `.env` 配置沿[部署指南](../docs/guide/addp部署和开发步骤.md#快速开始)，以下开发命令在仓库根目录执行；源码开发与产品容器部署的区别见[运行模式说明](../scripts/docs/开发与容器部署.md)。

### 开发模式（推荐）

```bash
# 启动基础设施
bash scripts/infra/up.sh

# 启动 Manager 及其标准依赖
bash scripts/dev/start.sh -manager
```

模块依赖、Runtime 显式选择及局部启停范围统一见[模块选择指南](../scripts/docs/模块选择启动指南.md)。源码开发使用标准开发生命周期，产品部署保留容器方式，不以容器部署命令接管开发进程。

Manager Backend `8081`、Frontend `5174` 是本地开发首选端口，实际运行端口以启动输出和 `.dev-state/ports.env` 为准；Infra 的实际宿主机映射用 `bash scripts/infra/status.sh` 查询，不从 `.env` 首选值推断。Console 与模块直连的访问方式及完整端口规则见[端口规范](../docs/spec/addp端口分配.md)。

### Docker 部署

Manager 随根 Compose 的 `addp-platform` 部署；镜像准备、启动顺序与失败边界分别沿[本地 Docker 指南](../scripts/local/README.md#快速启动)、[生产启动指南](../scripts/prod/README.md#startsh)，不在模块目录维护另一套启动命令。

根 Compose 仅向宿主机发布 Nginx 统一入口，模块 Backend／Frontend 使用容器内部端口；公开入口由 `ADDP_PUBLIC_ORIGIN` 表达，`NGINX_PORT` 与 `NGINX_BIND_HOST` 分别控制发布端口和监听范围。域名、HTTPS 或上级反向代理部署应填写用户实际访问的 origin，不从开发端口或容器内部端口推断公开地址；统一规则见[部署端口与公开入口](../docs/spec/addp端口分配.md#前端-docker-端口规则)。

镜像预检、容器状态、回环 HTTP 探测、公开 HTTPS 与业务就绪分别认定；实际检查范围见[Local 状态说明](../scripts/local/README.md#3-statussh---查看状态)、[Prod 健康检查](../scripts/prod/README.md#health-checksh)。启动或健康检查成功不替代认证、预览、下载与清理验收，Manager 的标准入口及覆盖边界见[模块验证说明](./CLAUDE.md#开发与验证)。

## 📊 支持的数据类型

引擎实例能力与 Manager 适配范围分别认定：能力声明以[引擎能力规范](../docs/spec/addp引擎能力声明规范.md#一基本原则)为准，Manager 按当前 Provider 和资源事实选择预览路线，不在本入口维护固定引擎数量或完整格式清单。

### 数据库

表格、动态 schema 集合、图与键值等数据库来源按实际读取能力与已注册 Provider 匹配；职责与模式见[PreviewRegistry 与 PreviewProvider](./docs/数据预览与资源树实现规范.md#3-previewregistry-与-previewprovider)。事件流 Topic 等其他来源也有对应的内置路线，统一见[Provider 加载入口](./backend/internal/preview/preview_plugin_loader.go)。Apache Spark 是[计算目标](../docs/concepts/addp引擎体系图.md#spark-集群与内置运行时边界)，不列为 Manager 存储引擎。

### 对象存储

对象与文件源包括 MinIO／S3、NFS／FTP 等，实际目录与内容访问依赖实例能力和已扫描的 Meta 身份；目录发现、快照与预览的职责见[引擎体系边界](../docs/concepts/addp引擎体系图.md#实时-engine-catalog元数据快照和数据预览的边界)。原始下载仍须通过[下载计划准入](./docs/存储流与原始下载语义.md#下载能力声明)，不从源类型直接推导。

### 文件格式

格式识别及解析实现以[格式能力规范](../docs/spec/addp数据类型与格式能力规范.md#formatdescriptor-与能力分层)为准；可展示材料与 renderer 组合以[预览语义矩阵](./docs/数据预览语义协议.md#推荐组合矩阵)为准，识别到格式不等于当前已具备在线预览。模型、点云和 3DGS 的独有约束见[对应专题](./docs/三维模型、点云与高斯泼溅预览说明.md)。

页面准入、内容 Permission 与数据保护分别沿[预览权限边界](./docs/数据预览与资源树实现规范.md#一定位)核验。引擎可见、Meta 已扫描或 Provider 已注册都不授予内容读取权限，也不证明真实链路已通过；测试分层、固定版本证据与未覆盖项见[模块验证说明](./CLAUDE.md#开发与验证)。

## 📡 主要 API 端点

```
数据预览:     GET  /api/v1/manager/preview
当前剖析:     GET  /api/v1/manager/data-profiles/current?locator={ResourceLocator}
执行剖析:     POST /api/v1/manager/data-profile-executions
存储流预览:   GET  /api/v1/manager/storage-stream
原始下载:     GET  /api/v1/manager/downloads/file?locator={ResourceLocator}
上传数据:     POST /api/v1/manager/uploads
导入数据:     POST /api/v1/manager/imports
导出数据:     POST /api/v1/manager/exports
导出下载:     GET  /api/v1/manager/exports/{id}/file
快显能力:     GET  /api/v1/manager/quick-view/capability?locator={ResourceLocator}
来源驱动生成: POST /api/v1/manager/quick-view/actions
快显FlatGeobuf: GET  /api/v1/manager/quick-view/flatgeobuf?locator={ResourceLocator}
快显GeoJSON调试: GET  /api/v1/manager/quick-view/geojson?locator={ResourceLocator}
快显MVT瓦片: GET  /api/v1/manager/quick-view/tiles/{z}/{x}/{y}.mvt?locator={ResourceLocator}
栅格快显 COG内容:  GET  /api/v1/manager/raster_cog/{id}/content
派生任务列表: GET    /api/v1/manager/tasks?category={category}
派生任务创建: POST   /api/v1/manager/tasks/{task_type}
派生任务详情: GET    /api/v1/manager/tasks/{task_type}/{id}
派生任务更新: PUT    /api/v1/manager/tasks/{task_type}/{id}
派生任务删除: DELETE /api/v1/manager/tasks/{task_type}/{id}
派生任务执行: POST   /api/v1/manager/tasks/{task_type}/{id}/execute
失效任务重绑: POST   /api/v1/manager/tasks/{task_type}/{id}/rebind
执行状态:     GET    /api/v1/manager/executions/{execution_id}
栅格快显 COG结果:  GET  /api/v1/manager/raster_cog
点云 COPC 内容:     GET  /api/v1/manager/point_cloud_copc/{id}/content
点云 COPC 结果:     GET  /api/v1/manager/point_cloud_copc
瓦片缓存结果: GET  /api/v1/manager/vector_tile_cache
数据检索:     GET  /api/v1/manager/search
```

任务列表的 `category` 可取 `managed_quick_view` 或 `spatial_business`，只用于后端分类筛选；统一强类型 CRUD 与任务身份见[快显共用契约](./docs/quick-view/共用契约.md#目标模型)。受管快显的产品入口按源 ResourceLocator 与 capability action 生成任务并展示只读定义，不能从通用 CRUD 清单推导页面可编辑私有生成配置，见[Manager 开发规则](./CLAUDE.md#开发规则)及[COG 产品入口](./docs/quick-view/TIFF与COG.md#产品入口与只读任务定义)。

重绑仅恢复失效来源，不自动执行，范围及版本要求见[失效任务重绑](./docs/quick-view/共用契约.md#失效任务重绑)；执行中的并发拒绝与已有结果覆盖确认见[受管任务执行规则](./docs/quick-view/共用契约.md#受管快显任务语义身份)。用户 API 与 Orchestrator 专用 TaskProvider 的身份、Permission 边界见[模块 API 说明](./CLAUDE.md#核心-api)，标准入口及实际验收范围见[开发与验证](./CLAUDE.md#开发与验证)。路由登记不代表权限、任务执行、结果交付或清理已通过验收。

完整 API 文档和请求示例请查看 [数据预览与资源树实现规范](./docs/数据预览与资源树实现规范.md)、[数据剖析规范](./docs/数据剖析规范.md) 和 [快显实现规范](./docs/快显实现规范.md)。 导入导出按[专题导航](#导入导出专题)定位来源核验、执行进度与完成导航。

## 🏗️ 核心架构

### 预览插件系统（可扩展）
- 资源树事实、搜索和刷新统一由 Meta resource-tree API 提供
- Manager 预览、下载、快显和任务入口统一以 ResourceLocator 定位
- 通过 PreviewResolver 选择 PreviewProvider
- 支持外部插件动态加载
- 预览响应按 `frontend_renderer -> preview_material -> content.kind` 选择前端组件

### 向量化与混合检索

向量化以 data item 为对象，node 只作为批量选择范围。资源树触发的一次性 execution、任务入口创建的持久配置及 item 当前结果分别按[对象边界](./docs/向量化概念说明.md#对象边界)、[任务与执行边界](./docs/向量化概念说明.md#任务与执行边界)管理；结果状态不能由 execution 成功推定。

搜索只消费当前租户、模型绑定与维度匹配的 `ready` 结果；结果身份、状态与快照字段统一见[数据模型](./docs/向量化能力说明.md#数据模型)。Meilisearch 的全文／属性检索、pgvector 的向量结果及 Manager 的融合、分页和降级规则统一见[检索消费](./docs/向量化能力说明.md#检索消费)。

接口身份与 Permission 分工见[模块 API 说明](./CLAUDE.md#核心-api)，模型场景绑定与配置权限见[模型配置归属](./docs/向量化概念说明.md#模型配置归属)；配置权限、索引存在或向量就绪不代替内容访问授权。真实混合检索的前置资源、断言和清理范围见[Online 验收](./docs/向量化能力说明.md#online-验收)，标准入口及分层边界见[模块验证说明](./CLAUDE.md#开发与验证)。不同源码、环境与场景分别认定，未运行、失败或未覆盖项不计通过。

### 快显、矢量物化视图与瓦片缓存

预览偏好、任务定义、资源绑定、执行记录与当前结果分别管理，目标落点以[快显目标模型](./docs/quick-view/共用契约.md#目标模型)为准；表清单、关系及表级约束统一见[数据库架构](./docs/数据库架构.md#二数据库表清单)与[关系说明](./docs/数据库架构.md#32-关系说明)。

Manager 受管产物与 Business PMTiles／Raster mosaic 业务 item 分属不同生命周期；结果清理不扩大到源文件、源表或外部只读目标，见[源事实与派生产物分离](./docs/数据库架构.md#71-源事实与派生产物分离)。源 `format=copc` 直接基础预览，不创建转换任务或登记生成结果，详见[对应关系](./docs/数据库架构.md#32-关系说明)。

按结果类型进入权威正文：

- 矢量物化视图、infra PMTiles 缓存与业务瓦片集：[矢量快显与物化视图](./docs/quick-view/矢量快显与物化视图.md)。
- 受管 COG 与 Raster mosaic 业务数据集：[TIFF 与 COG](./docs/quick-view/TIFF与COG.md)、[Raster mosaic](./docs/quick-view/Raster-mosaic.md)。
- GLB、3D Tiles／S3M、KSplat 与 COPC：[模型与点云专题](./docs/三维模型、点云与高斯泼溅预览说明.md#三快显结果与业务结果)；PPTX 静态 PDF：[快显共用契约](./docs/quick-view/共用契约.md#pptx-静态-pdf-预览)。

PMTiles 是矢量瓦片归档格式，MVT 是归档内瓦片类型，分别进入 `config.tile.archive_format=pmtiles` 与 `config.tile.tile_type=mvt`，都不是任务类型；COG 是 TIFF profile 或 Manager COG 生成结果，不是新的基础 format。

当前 `vector_tile_cache_generation`、`vector_materialized_view_generation` 和 `raster_cog_generation` 由 Manager Backend 内部执行；COG／COPC 的 Runtime 调用与部署边界见下文[运行要求](#cogcopc-运行要求)。

#### COG／COPC 运行要求

- **访问计划与发布**：Manager 构造 `addp.workflow.access-plan/v1` 源／目标计划，通过 Runtime direct operator 生成并发布 Manager infra MinIO 受管结果。COG 的 `raster_to_cog` 调用、受控目录、结构校验和失败状态见[COG 执行契约](./docs/quick-view/TIFF与COG.md#执行与租约)；PointCloud 的 direct 算子与产物归属见[点云职责边界](./docs/三维模型、点云与高斯泼溅预览说明.md#一职责边界)。源读取与目标发布细节统一由访问计划和 Runtime 契约维护。
- **路径与端点可达**：NFS／NAS 源由 Manager 按 engine `mount_path`／`export_path` 派生的路径，必须在实际 Runtime 进程所在环境可读。原生开发使用宿主机路径；产品容器需提供对应挂载，并保证计划中的路径在容器内仍可访问。MinIO／S3 源和 infra 目标端点同样须从该 Runtime 可达，宿主机实际端点与 Docker 网络端点分别按[部署指南](../docs/guide/addp部署和开发步骤.md#第三步-构建模式-用于-docker-镜像构建)选择，不从 Manager 本机可达外推 Runtime 可达。
- **原生开发与产品容器**：GeoPython 使用独立 venv 原生进程，PointCloud 使用隔离 Conda 原生进程；依赖准备、路径与监听归属分别见[GeoPython 本地开发](../engines/geopython-workflow/docs/geopython-workflow容器管理指南.md#本地开发)、[PointCloud 开发启动](../engines/pointcloud-workflow/README.md#开发启动)。产品部署保留[GeoPython 产品镜像](../engines/geopython-workflow/docs/geopython-workflow容器管理指南.md#产品镜像)与[PointCloud 产品容器](../engines/pointcloud-workflow/README.md#产品部署)，不作为开发启动的备选路线。
- **COPC 工作目录**：开发工作目录由标准入口按 `POINTCLOUD_WORK_HOST_PATH` 解析并注入 `POINTCLOUD_WORK_DIR`／`CPL_TMPDIR`，需可写且空间充足；产品挂载以[Runtime Compose](../docker-compose.runtimes.yml)为准。COPC 先写本地临时文件，再按目标访问计划发布并由 Manager 登记 `point_cloud_copc`；工作目录规则与 PDAL 2.10.2 `writers.copc` 不能可靠直接写 `/vsis3/` 的既有记录统一见[PointCloud 运行约束](../engines/pointcloud-workflow/README.md)，不把 COPC 描述为纯流式写入。

依赖预检、进程就绪、真实转换与发布、页面读取及清理分别验收；引擎验证范围见[GeoPython 验证](../engines/geopython-workflow/docs/geopython-workflow容器管理指南.md#验证)、[PointCloud 测试](../engines/pointcloud-workflow/README.md#测试)，Manager 跨模块链路、固定版本证据与未覆盖项见[模块验证说明](./CLAUDE.md#开发与验证)。原生回归不替代产品容器验收，未运行、失败或未覆盖项不计通过。

三维模型、3D Tiles、点云 COPC 和高斯泼溅的预览路线请查看 [三维模型、点云与高斯泼溅预览说明](./docs/三维模型、点云与高斯泼溅预览说明.md)。详细架构请查看 [数据预览与资源树实现规范](./docs/数据预览与资源树实现规范.md)。

## 🐛 常见问题

### 数据预览失败？

先沿[快速开始](#-快速开始)核对当前运行模式、实际端口和公开入口，再关联本次资源、请求状态与稳定错误码。引擎离线但历史资源树仍可见、`503 engine_unavailable` 与恢复规则见[引擎连通性与历史资源树](./docs/数据预览与资源树实现规范.md#引擎连通性与历史资源树)；功能权限、源授权、Security 拒绝及网络／依赖失败的区分见[预览权限边界](./docs/数据预览与资源树实现规范.md#一定位)和[前端失败状态](./docs/数据预览与资源树实现规范.md#五前端实现路线)，不把成功空结果当作失败，也不把失败显示为空数据。

需要手动复测已登记引擎时，沿 System 的[连接测试契约](../system/docs/tables/engines表.md#测试连接已创建)使用当前 Tenant User 和 `system.engine.execute`；该接口是 POST，默认复测会写回连接状态，不能作为纯读取查询。须核对响应中的实际测试结果，不能仅凭 HTTP 成功认定连接可用，更不能据此推断 Manager 的源读取权限或预览已通过。

日志按模块与进程实例定位，实际本地来源和链路诊断见[Infra 运行日志](../scripts/infra/docs/运行日志.md#模块实例运行日志)，正文查询的身份、权限和实例范围见[日志查询规范](../docs/spec/addp模块服务运行日志规范.md#51-api-与授权)。单份本地输出、日志可查询或投递探针成功都不证明历史日志完整，也不代替本次预览请求的结果核对。

### 地图加载慢？

先只读核对[快显能力与源事实](./docs/quick-view/共用契约.md#快显能力)，再按[PG 表要求与动态 MVT 诊断](./docs/quick-view/矢量快显与物化视图.md#pg-表要求与矢量物化视图路线)检查性能模式、超时预算、推荐和重试响应头，不以慢请求直接推断权限不足。

`vector_materialized_view_generation`、`vector_tile_cache_generation` 的适用条件、页面引导与生成顺序统一见[物化视图任务与结果](./docs/quick-view/矢量快显与物化视图.md#矢量物化视图任务和结果)、[生成执行路线](./docs/quick-view/矢量快显与物化视图.md#生成执行路线)；ready 缓存与当前渲染源的选择见[渲染源选择](./docs/quick-view/共用契约.md#渲染源选择)。在此排查流程中，准备与生成须由有权限的用户显式发起，诊断和推荐不自动创建、修复或重建结果，也不授予源或优化目标的内容读取权。

其他故障、操作边界与恢复验证见[故障排查总入口](../docs/guide/addp常见故障排查.md#操作与验证边界)。预览、下载完整交付、任务结果与清理分别按[Manager 验证说明](./CLAUDE.md#开发与验证)、[下载验收边界](./docs/存储流与原始下载语义.md#验证入口与验收边界)认定；未执行、失败或未覆盖项不计通过。

## 📚 相关文档

### 模块规范与专题

- [模块说明](./CLAUDE.md) — 模块定位、核心 API、开发规则与权限职责。
- [数据预览与资源树实现规范](./docs/数据预览与资源树实现规范.md) — 数据探查、资源树、预览 API、PreviewResolver 和 PreviewProvider 的当前契约。
- [数据预览语义协议](./docs/数据预览语义协议.md) — `content.kind`、`preview_material`、`frontend_renderer` 等响应材料与渲染语义。
- [数据剖析规范](./docs/数据剖析规范.md) — 表格剖析的产品边界、Owner、采样执行、结果存储与呈现。
- [存储流与原始下载语义](./docs/存储流与原始下载语义.md) — `storage-stream`、`downloads/file`、DownloadPlan、身份保护与完整交付。
- [快显概念说明](./docs/快显概念说明.md) — 快显、矢量物化视图和瓦片缓存的价值、概念与职责边界。
- [快显实现规范](./docs/快显实现规范.md) — 各结果专题、表结构、任务、API、执行与清理规则的总入口。
- [三维模型、点云与高斯泼溅预览说明](./docs/三维模型、点云与高斯泼溅预览说明.md) — 三维模型、3D Tiles、点云和 3DGS 的预览、快显任务、状态与当前限制。
- [向量化概念说明](./docs/向量化概念说明.md) — 向量化价值、对象、任务和结果边界。
- [向量化能力说明](./docs/向量化能力说明.md) — 向量化结果、任务、执行、检索与 UI 行为。
- [数据库架构](./docs/数据库架构.md) — Manager schema 表清单、关系、索引与数据流。

### 资源树与预览状态专题

| 阅读目的 | 权威正文 |
| --- | --- |
| 区分 node／item 范围、扫描进度与终态回读 | [刷新范围](./docs/数据预览与资源树实现规范.md#刷新范围与入口) · [执行进度与失败](./docs/数据预览与资源树实现规范.md#执行进度与失败反馈) · [完成回读](./docs/数据预览与资源树实现规范.md#完成回读与状态保持) |
| 保持选择、展开、子树与缓存状态 | [资源树状态保持](./docs/数据预览与资源树实现规范.md#资源树状态保持) |
| 合并在途读取、隔离迟到响应并区分失败 | [请求隔离与失败状态](./docs/数据预览与资源树实现规范.md#请求隔离与失败状态) |
| 选择 renderer、合并 ancestors 与定位资源 | [预览材料与资源定位](./docs/数据预览与资源树实现规范.md#预览材料与资源定位) |
| 区分上传提交、识别、授权与目录回读 | [上传状态与完成回读](./docs/数据预览与资源树实现规范.md#上传状态与完成回读) |
| 恢复 locator／tab、iframe 与浏览历史 | [数据探查 URL 状态](./docs/数据预览与资源树实现规范.md#数据探查-url-状态) |

验收入口与分版本证据见[验证与历史证据](#验证与历史证据)。

### 共享规范与扩展

- [技术栈规约](../docs/spec/addp技术栈规约.md) — 技术栈与依赖版本。
- [数据引擎扩展指南](../docs/spec/addp数据引擎扩展指南.md) — 新引擎的接入与消费分工。
- [引擎插件接口规范](../docs/spec/addp引擎插件接口规范.md) — Engine Provider 接口与模块消费边界。
- [共享前端模块](../common-frontend/README.md) — 共享组件接入与业务模块职责。

### 验证与历史证据

- [模块验证说明](./CLAUDE.md#开发与验证) — 标准门禁、运行环境与实际覆盖范围。
- [Manager Online 场景](../docs/spec/testing/scenarios/Manager与空间成果.md) — 隔离链路的身份、固定断言及清理要求。
- [Manager 私有导入场景](../docs/spec/testing/scenarios/同步与引擎消费.md#manager-私有导入) — 私有来源、传输、扫描与结果验证的专项范围。
- [预览与私有导入历史证据](../docs/spec/testing/验收证据索引.md#manager-预览与私有导入历史证据) — 分版本结果与原始记录；其他专题的证据亦由该索引定位。
- [租户过程事件有限范围 T4](../docs/spec/testing/evidence/资源读取与预览历史证据.md#manager-租户过程事件有限范围-t42026-10-19) — 固定源码下的实际事件读取、清理与未覆盖范围；不代替当前事件或留存契约。

权限与来源约束以对应契约正文为准；各场景分别认定源码版本、失败记录、清理结果与未覆盖项，历史通过不外推当前工作区。文档、用例或导航存在不代表实际验收通过。

---

Copyright © 2025 ADDP Team
