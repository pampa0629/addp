# ADDP 计算引擎开发指南

本目录集中管理不拥有 ADDP 业务配置事实的独立计算和 Notebook 运行时。GeoPython Workflow、Spark Workflow、DuckDB、Jupyter 是默认部署的内置运行时；Math Workflow 是 `addp.workflow/v1` 参考实现，用于示范扩展引擎规范；Model3D Workflow 是三维模型转换专用运行时；PointCloud Workflow 是点云处理运行时；Document Workflow 是文档转换运行时；SuperMap Workflow 是面向超图 iObjects C++ 的空间计算运行时。

`engines/` 不是 `system.engines` 的源码镜像，也不是所有 Engine Type 的目录。数据库、对象存储等插件位于 `common/engine/plugins/`；Inference 因拥有 Provider Connection、Model Deployment、Model Profile、凭据、控制面 API 和前端，保留为根目录 `inference/` 业务模块，其数据面端点另以 `engine_type=inference_runtime` 登记到 System。只有未来将不拥有这些业务事实的独立推理执行面拆为单独服务时，才在本目录新增 `inference-runtime/`，且不得复制 Inference 控制面。

协议、元数据校验和扩展要求见[工作流运行时文档入口](docs/README.md)；各 Runtime 的运行说明及专题见[参考实现](#参考实现)。

## 目录结构

```
engines/
├── geopython-workflow/    # GeoPython Workflow 运行时，默认端口 8099
├── spark-workflow/     # Spark Workflow 分布式工作流引擎，默认端口 8098
├── math-workflow/      # Math Workflow 数学工作流参考实现，默认端口 8089
├── model3d-workflow/   # Model3D Workflow 三维模型转换运行时，默认端口 8101
├── pointcloud-workflow/ # PointCloud Workflow 点云处理运行时，默认端口 8102
├── document-workflow/ # Document Workflow 文档转换运行时，默认端口 8105
├── supermap-workflow/  # SuperMap Workflow 超图 iObjects C++ 工作流运行时，默认端口 8103
├── jupyter/            # Notebook Runtime 与隔离交互会话，API 默认端口 8097
├── duckdb/             # DuckDB 联邦查询运行时，API 默认端口 8104
└── docs/               # 引擎 API 与设计文档
```

## 本地开发运行方式

支持 macOS 的开发服务采用原生进程；产品构建与 Hosted 产品验收独立使用镜像。Infra 与 Business 保留现有容器部署用途。当前开发启动盘点如下，实际入口以 `scripts/dev/start.sh`、各 Runtime 的共享准备脚本及模块 README 为准：

| Runtime | 当前开发启动 | 依赖支持与后续工作 |
| --- | --- | --- |
| Spark Workflow | 原生 Python 3.11/3.12 + OpenJDK 11 | 已统一标准启动、重启、停止；macOS 配置本机 `host.docker.internal` 映射，Worker 使用 Docker 内置解析 |
| GeoPython Workflow | 原生 Python 3.12 + GDAL + MDBTools/ODBC | 独立 venv；GDAL 绑定与原生库同版本；产品栅格 T4 独立构建和启动镜像 |
| PointCloud Workflow | 原生 Python 3.12 + 独立 Conda PDAL 2.10.2 | 已统一原生生命周期；GDAL、PROJ、E57 库随 Runtime 环境隔离，预检 COPC、LAS、E57、PCD、text 驱动及实际转换；详见 `pointcloud-workflow/README.md` |
| Document Workflow | 原生 Python 3.12 + 私有 LibreOffice 与中文字体 | 已统一原生生命周期；启动检查依赖完整性与版本，每次转换使用独立 profile，真实 PPTX 转 PDF 由 `make test-document-native` 独立验证；详见 `document-workflow/README.md` |
| SuperMap Workflow | Docker，当前接入 Linux ARM64 C++ SDK | 先核对同版本 SDK 的 macOS 支持及许可；不能从当前 Linux 制品推断厂商仅支持 Linux |
| Model3D Workflow | 原生 Python + 私有转换工具包 | 当前开发安装支持 macOS arm64；转换器版本与完整性由私有包校验，删除开发 Docker wrapper；详见 `model3d-workflow/README.md` |
| Math、DuckDB、Jupyter | 原生进程 | 沿用各自标准生命周期与依赖准备入口 |

### GeoPython 原生开发

环境安装、GDAL/PROJ 隔离、ODBC 配置与标准启停入口见 [GeoPython 运行说明](geopython-workflow/docs/geopython-workflow容器管理指南.md#本地开发)。本节保留运行架构与验收边界：

- 开发 start/restart/stop 管理原生进程，以进程身份、监听归属和健康检查判定就绪；重启先停止所选服务再准备依赖，准备失败不启动该服务。
- 开发和产品统一使用单 Worker、四线程 Gunicorn，先监听再异步注册，不新增另一条 Flask 开发入口。System 注册、访问计划与业务引擎连接事实继续由现有模块拥有。
- 矢量文件 batch 算子和矢量瓦片一样，在同一 Runtime Python 环境的新进程中初始化原生 GDAL／PROJ，避免 macOS 系统 SQLite 在 Gunicorn fork 后的 HTTP 线程中崩溃。批处理访问计划只通过标准输入传递，原生进程异常不回退到 HTTP Worker 内执行。`make test-geopython-workflow` 的真实 Gunicorn 回归覆盖 KML／GML／TopoJSON 的图层读取和 Worker 存活，FileGDB 注册入口回归覆盖独立进程间的写入、错误传递、提交与清理；不新增测试依赖或 CI 入口。
- 栅格 Hosted T4 通过根 `make build-images` 构建并独立启动产品镜像，核验实际 image ID、默认入口和退出清理；不再借开发入口证明产品构建身份。ArcGIS 集成入口直接使用同一原生环境，不再要求开发容器。

完整 owner 验证使用仓库根目录的 `make test-module MODULE=engines`；跨模块工作区变更使用 `make test-changed` 按实际依赖扩散。`make test-dev-lifecycle`、`make test-geopython-workflow` 和 `make test-raster-online-runner` 用于对应专项，不替代完整 owner 门禁。真实 Access/PGeo 样本及 Oracle Spatial 往返沿用 `make test-arcgis-open-formats`；仅验证 Runtime 读取时使用 `make test-arcgis-open-formats ARCGIS_OPEN_FORMATS_ARGS=--runtime-only`，该范围不包含 Transfer 和 Oracle 往返；完整跨模块栅格与 Console 链路使用已登记的 `raster-workflow` Hosted T4。确定性测试通过不能替代原生依赖和产品镜像各自的真实运行证据。

## 引擎分类

### 工作流运行时

通过 `EnginePlugin + WorkflowRuntimeProvider` 纳入统一引擎体系，能力声明为 `compute.workflow.supported=true`。

**已有引擎**：
- `geopython_workflow` - GeoPython Workflow，适合中小规模空间与数据处理。
- `spark_workflow` - Spark Workflow 引擎，适合分布式计算。
- `math_workflow` - Math Workflow 参考实现，手动注册后可用；启动与连接配置见 [Math README](math-workflow/README.md)。
- `model3d_workflow` - Model3D Workflow 三维模型转换运行时，提供三维模型、BIM、高斯泼溅及 OSGB Scene 的持久化转换，算子均支持 workflow/direct；完整清单、工具绑定和启动方式见 [Model3D README](model3d-workflow/README.md)。
- `pointcloud_workflow` - PointCloud Workflow 点云处理运行时，提供 `las_to_copc`、`laz_to_copc`、`e57_to_copc`、`pcd_to_copc` 和 `xyz_to_copc` workflow/direct 算子；PDAL 属于运行时内部依赖。Runtime 监听后异步登记到 System，PDAL 缺失不阻止身份登记，健康与可执行性按 [Runtime 注册规范](../docs/spec/workflow/Runtime注册与扩展.md#引擎注册)判定。实际转换通过 `POINTCLOUD_PDAL_BIN` 指向引擎部署内的 PDAL 可执行文件路径，不依赖宿主机全局 PDAL。Manager 负责派生源 URI 和 Manager infra MinIO 发布计划，运行时通过 PDAL 读取源 URI，先写入受控工作目录，再发布为 Manager 私有 COPC artifact。
- `document_workflow` - Document Workflow 文档转换运行时，提供 `document_to_pdf` workflow/direct 算子；开发模式使用私有原生 LibreOffice 和字体，产品部署使用正式容器镜像；依赖准备与启动方式见 [Document README](document-workflow/README.md)。Manager 通过访问计划生成私有 PDF 快显 artifact，Runtime 不解析 ResourceLocator 或拥有任务定义。
- `supermap_workflow` - SuperMap Workflow 超图工作流运行时，对外实现 `addp.workflow/v1`，对内绑定 SuperMap iObjects C++ SDK；运行时独立校验并按稳定拓扑顺序执行 DAG，同一 C++ 执行上下文内通过类型化共享句柄传递 Datasource、Dataset 等对象。完整 SDK 作为仓库外只读母版保存，最终运行镜像只包含已验证的运行期文件，许可作为受控制品单独注入。

### 脚本 / Notebook 运行时

通过 `EnginePlugin + ScriptRuntimeProvider` 纳入统一引擎体系，能力声明为 `compute.script.supported=true`；该声明描述 Runtime 能力，不代表消费模块已开放排队派发，边界见[能力声明规范](../docs/spec/addp引擎能力声明规范.md#一基本原则)。

当前内置实现为 `jupyter`，接口与平台接线按以下层次区分：

| 层次 | 当前范围与权威正文 |
| --- | --- |
| Runtime 接口 | 已有无头执行、内容摘要校验及交互会话控制面；受保护接口仅供 `addp-develop` 以租户 Service Access Token 调用，字段与错误语义见 [Jupyter API](jupyter/README.md#api-接口)。 |
| 排队派发 | 一次性 Notebook（`script`）当前拒绝派发，可核验撤权的运行授权尚待确认和接入；执行错误、授权来源与恢复边界统一见[任务契约](../docs/spec/tasks/模块任务契约.md#develop-执行监督与授权)。 |
| 交互会话 | Develop 创建并代理短期隔离 JupyterLab 会话，不进入上述队列，浏览器不直连 Runtime，也不暴露共享 Lab；Session 与逐次数据读取授权的分工见[授权规范](../docs/spec/addp授权上下文规范.md#53-notebook-会话授权)，Runtime 清理与 Bridge 约束见[会话说明](jupyter/README.md#4-交互会话控制面)。 |

确定性门禁、镜像检查与真实队列派发／撤权闭环分别认定，覆盖与未完成项见 [Jupyter 验证边界](jupyter/README.md#依赖库)；接口存在、进程就绪或局部测试不能代替跨模块 Notebook T4，未执行或未覆盖项不计为通过。

### 联邦查询运行时

通过 `EnginePlugin + FederatedQueryRuntimeProvider` 纳入统一引擎体系，能力声明为
`compute.query.federation.supported=true`。

**已有引擎**：
- `duckdb` - 由 Develop 和 Service 通过租户 Service Access Token 调用的内置联邦查询 Runtime；执行授权限定可挂载的源 Engine，支持 PostgreSQL、MySQL、MinIO 和 S3。

## 工作流运行时 HTTP 协议

工作流引擎对外实现 `addp.workflow/v1` HTTP 协议，业务模块通过 Common Engine 的 `WorkflowRuntimeProvider` 调用，不直接拼接 Runtime 私有 URL。

五个标准接口、请求响应、可信租户与执行授权、同步或异步状态及错误码统一见 [HTTP 协议](../docs/spec/workflow/HTTP协议.md)；算子必填字段、参数、effects 与输出端口见[算子元数据与实现规范](../docs/spec/workflow/算子元数据与实现.md)。[OpenAPI、共享示例和元数据校验入口](docs/README.md)集中维护协议材料，本页不再复制请求与响应模板。

单算子入口只允许调用 `execution_modes` 包含 `direct` 的算子，用于业务模块受控调用单个算子。它不创建 Develop/Orchestrator/Monitor 任务；凡是需要调度、重试、跨模块编排或统一监控的执行，必须走工作流。具体约束见 [direct 调用规范](../docs/spec/workflow/HTTP协议.md#调用单个算子)。

## 能力声明

引擎能力统一使用 `engine.capabilities/v1` 结构。Common engine 插件由 `Capabilities()` 声明能力模板；非插件内置 Workflow Runtime 在启动注册时提交与 `engine_type` 一致的标准能力声明。

能力只表达引擎自身 native / provider 能力，例如 `compute.workflow`、`compute.script`、`storage.catalog`、`storage.store`。不要在引擎能力中维护 Transfer、Preview、Develop 等模块对引擎的适配列表。

工作流引擎的算子列表、参数、输出端口等动态能力不写入 `capabilities`，通过 `WorkflowRuntimeProvider.ListOperators()` 实时发现。

声明结构与校验规则见[引擎能力声明规范](../docs/spec/addp引擎能力声明规范.md)，Workflow Runtime 的提交及探测要求见[注册规范](../docs/spec/workflow/Runtime注册与扩展.md#引擎注册)。

## 引擎注册

实例身份、稳定访问地址、认证、完整能力声明、协议探测，以及生产自注册与扩展引擎手动注册的要求，统一遵循 [Runtime 注册规范](../docs/spec/workflow/Runtime注册与扩展.md#引擎注册)。注册与 Runtime 健康分别判定，本页不再维护注册端点或 payload 示例。

Math Workflow 是参考实现，服务启动后仍需在 System 中手动注册。启动范围见[模块选择指南](../scripts/docs/模块选择启动指南.md#显式选择-runtime-联调)，连接填写见 [Math README](math-workflow/README.md#开发启动)。

## 新增独立 Runtime checklist

仅不拥有业务配置事实的独立计算运行时进入 `engines/`。接入步骤统一见[扩展新引擎指南](../docs/spec/workflow/Runtime注册与扩展.md#扩展新引擎指南)，实现与交付时按以下入口核对：

| 范围 | 规范或入口 |
| --- | --- |
| 算子元数据、公共执行核心、HTTP 与注册 | [Runtime 接入步骤](../docs/spec/workflow/Runtime注册与扩展.md#python-runtime-接入步骤) |
| 开发启动、重启、停止、依赖准备与构建身份 | [开发服务生命周期与构建身份规范](../docs/spec/addp开发服务生命周期与构建身份规范.md) |
| 产品容器与构建登记 | 根 [Runtime Compose](../docker-compose.runtimes.yml)、[统一镜像构建入口](../scripts/build/build-images.sh)；标准部署通过 [Local](../scripts/local/start.sh) / [Prod](../scripts/prod/start.sh) 启动 Runtime project |
| 测试入口、CI 覆盖与运行验收 | [验证与交付](../docs/spec/workflow/Runtime注册与扩展.md#5-验证与交付)、[测试与验收规范](../docs/spec/addp测试与验收规范.md) |

## 参考实现

按 Runtime 查找运行说明与专题；公共协议和字段定义在上面的统一入口维护。

| Runtime | 运行说明 | 专题 |
| --- | --- | --- |
| Math Workflow | [README](math-workflow/README.md) | 协议与公共执行核心参考实现、手动注册 |
| GeoPython Workflow | [运行说明](geopython-workflow/docs/geopython-workflow容器管理指南.md) | [矢量与栅格算子模块](geopython-workflow/operators/README.md) |
| Spark Workflow | [README](spark-workflow/README.md) | [引擎关系与资源绑定](spark-workflow/docs/spark-design.md)、[Spark 与 Sedona 用户手册](spark-workflow/docs/spark-user-guide.md) |
| Model3D Workflow | [README](model3d-workflow/README.md) | 三维模型、BIM、高斯泼溅与 3D Tiles 转换 |
| PointCloud Workflow | [README](pointcloud-workflow/README.md) | 点云转 COPC 运行时与 PDAL 依赖 |
| Document Workflow | [README](document-workflow/README.md) | PPTX 转 PDF 运行时与原生依赖 |
| SuperMap Workflow | [README](supermap-workflow/README.md) | iObjects C++ SDK、镜像和许可 |
| DuckDB | [README](duckdb/README.md) | Develop 与 Service 共用的联邦查询运行时 |
| Jupyter | [README](jupyter/README.md) | [Runtime 接口、排队派发限制与交互会话](#脚本--notebook-运行时) |

## 相关文档

- [ADDP 引擎体系图](../docs/concepts/addp引擎体系图.md)
- [Common模块文档](../common/README.md)
- [System模块文档](../system/CLAUDE.md)
