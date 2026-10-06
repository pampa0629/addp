# ADDP 计算引擎开发指南

本目录集中管理不拥有 ADDP 业务配置事实的独立计算和 Notebook 运行时。GeoPython Workflow、Spark Workflow、DuckDB、Jupyter 是默认部署的内置运行时；Math Workflow 是 `addp.workflow/v1` 参考实现，用于示范扩展引擎规范；Model3D Workflow 是三维模型转换专用运行时；PointCloud Workflow 是点云处理运行时；Document Workflow 是文档转换运行时；SuperMap Workflow 是面向超图 iObjects C++ 的空间计算运行时。

`engines/` 不是 `system.engines` 的源码镜像，也不是所有 Engine Type 的目录。数据库、对象存储等插件位于 `common/engine/plugins/`；Inference 因拥有 Provider Connection、Model Deployment、Model Profile、凭据、控制面 API 和前端，保留为根目录 `inference/` 业务模块，其数据面端点另以 `engine_type=inference_runtime` 登记到 System。只有未来将不拥有这些业务事实的独立推理执行面拆为单独服务时，才在本目录新增 `inference-runtime/`，且不得复制 Inference 控制面。

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
├── jupyter/            # 无头 Notebook 执行运行时，API 默认端口 8097
├── duckdb/             # DuckDB 联邦查询运行时，API 默认端口 8104
└── docs/               # 引擎 API 与设计文档
```

## 本地开发运行方式

支持 macOS 的开发服务采用原生进程；产品构建与 Hosted 产品验收独立使用镜像。Infra 与 Business 保留现有容器部署用途。当前开发启动盘点如下，待调整项尚未改动生命周期：

| Runtime | 当前开发启动 | 依赖支持与后续工作 |
| --- | --- | --- |
| Spark Workflow | 原生 Python 3.11/3.12 + OpenJDK 11 | 已统一标准启动、重启、停止；macOS 配置本机 `host.docker.internal` 映射，Worker 使用 Docker 内置解析 |
| GeoPython Workflow | 原生 Python 3.12 + GDAL + MDBTools/ODBC | 独立 venv；GDAL 绑定与原生库同版本；产品栅格 T4 独立构建和启动镜像 |
| PointCloud Workflow | Docker | [PDAL 支持 macOS 环境](https://pdal.org/en/stable/quickstart.html)；建议在 Runtime 独立环境安装 PDAL，继续使用 `POINTCLOUD_PDAL_BIN`，预检 COPC、LAS、E57、PCD、text 驱动及可写临时目录 |
| Document Workflow | Docker | [LibreOffice 支持 Intel 与 Apple Silicon macOS](https://hr.libreoffice.org/get-help/install-howto/macos/)；可继续用 `DOCUMENT_LIBREOFFICE_BIN` 绑定原生程序，现有转换已为每次执行创建独立 profile，仍需验证中文字体与多页 PPTX 转 PDF |
| SuperMap Workflow | Docker，当前接入 Linux ARM64 C++ SDK | 先核对同版本 SDK 的 macOS 支持及许可；不能从当前 Linux 制品推断厂商仅支持 Linux |
| Math、Model3D、DuckDB、Jupyter | 原生进程 | 保持当前标准生命周期；Model3D 的外部转换器依赖另行核对平台范围 |

### GeoPython 原生开发

2026-10-05 的本机只读盘点发现：`venv` 与测试用 `.venv` 均继承 Anaconda 的系统包，加载 GDAL 3.6.2，缺少 PGeo 驱动；测试环境的 `pip check` 还报告了继承的 Spyder、Numba 等无关包冲突。Homebrew 已安装 GDAL 3.12.1 和 unixODBC，但尚未安装 MDBTools。Homebrew 的 `ogrinfo --formats` 在隔离 Anaconda 环境变量后可发现 PGeo、ODBC、OpenFileGDB 与 GPKG。这些结果只证明依赖路线可行，尚不代表原生 Python 绑定、真实 MDB 读取或 Runtime 启动验收通过。

开发生命周期采用以下单一路线：

- 以独立 Python 3.12 虚拟环境安装完整 requirements 与 editable Common Python，不继承系统 site-packages；[Python GDAL 绑定](https://gdal.org/en/stable/api/python/python_bindings.html)与所链接的原生 GDAL 版本匹配，不直接加载 Homebrew 为另一 Python 主次版本编译的扩展。
- GDAL/PROJ 资源目录由所选原生依赖派生，只注入 GeoPython 子进程，隔离终端继承的 `GDAL_DATA`、`GDAL_DRIVER_PATH`、`PROJ_DATA` 与 `PROJ_LIB`，保持离线坐标处理。
- 按 [PGeo 官方配置要求](https://gdal.org/en/stable/drivers/vector/pgeo.html)补齐 MDBTools ODBC 驱动与 Access 驱动名；优先使用 Runtime 专属 ODBC 配置，不改宿主机其他应用的配置。
- 标准 start/restart/stop 使用原生进程身份、监听归属和健康检查；预检失败保留已有服务，删除开发镜像启动路径。应用统一使用现有产品的单 Worker、四线程 Gunicorn 语义，先监听再异步注册，不新增另一条 Flask 开发入口。原生消费回环数据源时不注入容器专用的 `GEOPYTHON_WORKFLOW_LOOPBACK_HOST`。System 注册、访问计划与业务引擎连接事实继续由现有模块拥有。
- 栅格 Hosted T4 通过根 `make build-images` 构建并独立启动产品镜像，核验实际 image ID、默认入口和退出清理；不再借开发入口证明产品构建身份。ArcGIS 集成入口直接使用同一原生环境，不再要求开发容器。

macOS 先安装 `brew install python@3.12 gdal mdbtools pkg-config`，随后使用 `./scripts/dev/restart.sh -geopython-workflow`。首次切换须先由用户停止旧 GeoPython 开发容器，释放其监听端口；生命周期不会自动接管容器。虚拟环境统一放在 `engines/geopython-workflow/venv`，ODBC 驱动配置位于 `.dev-state/geopython-odbc`，只注入 Runtime 子进程。

最小验证范围包括 `make test-dev-lifecycle`、`make test-geopython-workflow`、`make test-raster-online-runner`、`make test-platform` 和按变更影响计算的 `make test-changed`。真实 Access/PGeo 样本及 Oracle Spatial 往返沿用 `make test-arcgis-open-formats`；仅验证 Runtime 读取时使用 `make test-arcgis-open-formats ARCGIS_OPEN_FORMATS_ARGS=--runtime-only`，该范围不包含 Transfer 和 Oracle 往返；完整跨模块栅格与 Console 链路使用已登记的 `raster-workflow` Hosted T4。确定性测试通过不能替代原生依赖和产品镜像各自的真实运行证据。

## 引擎分类

### 工作流运行时

通过 `EnginePlugin + WorkflowRuntimeProvider` 纳入统一引擎体系，能力声明为 `compute.workflow.supported=true`。

**已有引擎**：
- `geopython_workflow` - GeoPython Workflow，适合中小规模空间与数据处理。
- `spark_workflow` - Spark Workflow 引擎，适合分布式计算。
- `math_workflow` - Math Workflow 参考实现，开发环境可自动启动服务但不会自动注册；需要使用时在 System 引擎管理中按扩展引擎手动注册。
- `model3d_workflow` - Model3D Workflow 三维模型转换运行时，提供 `osgb_to_glb` 和 `osgb_scene_to_3dtiles` direct 算子；开发环境启动时会自注册到 System，实际转换需通过 `MODEL3D_CONVERTER_BIN` 配置引擎部署内的 `_3dtile` 或等价转换器可执行文件路径。
- `pointcloud_workflow` - PointCloud Workflow 点云处理运行时，提供 `las_to_copc`、`laz_to_copc`、`e57_to_copc`、`pcd_to_copc` 和 `xyz_to_copc` direct 算子；绑定 engine runtime 内部 PDAL 后会自注册到 System，实际转换通过 `POINTCLOUD_PDAL_BIN` 指向引擎部署内的 PDAL 可执行文件路径，不依赖宿主机全局 PDAL。Manager 负责派生源 URI 和 Manager infra MinIO 发布计划，运行时通过 PDAL 读取源 URI，先写入受控工作目录，再发布为 Manager 私有 COPC artifact。
- `document_workflow` - Document Workflow 文档转换运行时，提供 `document_to_pdf` workflow/direct 算子；LibreOffice 和字体固定在运行镜像中，Manager 通过访问计划生成私有 PDF 快显 artifact，Runtime 不解析 ResourceLocator 或拥有任务定义。
- `supermap_workflow` - SuperMap Workflow 超图工作流运行时，对外实现 `addp.workflow/v1`，对内绑定 SuperMap iObjects C++ SDK；运行时独立校验并按稳定拓扑顺序执行 DAG，同一 C++ 执行上下文内通过类型化共享句柄传递 Datasource、Dataset 等对象。完整 SDK 作为仓库外只读母版保存，最终运行镜像只包含已验证的运行期文件，许可作为受控制品单独注入。

### 脚本 / Notebook 运行时

通过 `EnginePlugin + ScriptRuntimeProvider` 纳入统一引擎体系，能力声明为 `compute.script.supported=true`。

**已有引擎**：
- `jupyter` - 仅由 Develop 通过租户 Service Access Token 调用的无头 Notebook 运行时，不暴露共享 Lab。

### 联邦查询运行时

通过 `EnginePlugin + FederatedQueryRuntimeProvider` 纳入统一引擎体系，能力声明为
`compute.query.federation.supported=true`。

**已有引擎**：
- `duckdb` - 由 Develop 和 Service 通过租户 Service Access Token 调用的内置联邦查询 Runtime；执行授权限定可挂载的源 Engine，支持 PostgreSQL、MySQL、MinIO 和 S3。

## 工作流运行时 HTTP 协议

工作流引擎对外实现 `addp.workflow/v1` HTTP 协议，业务模块不直接拼接这些 URL，而是通过 common engine 的 `WorkflowRuntimeProvider` 调用。

### 1. 算子发现
```
GET /api/operators
```

返回格式:
```json
{
  "status": "success",
  "operators": [
    {
      "id": "buffer",
      "name": "buffer",
      "display_name": "缓冲区分析",
      "engine_type": "geopython_workflow",
      "category": "空间分析",
      "category_path": ["空间分析"],
      "description": "对几何对象生成缓冲区",
      "execution_modes": ["workflow"],
      "parameters": [
        {
          "name": "distance",
          "type": "float",
          "required": true,
          "description": "缓冲区距离",
          "min": 0
        }
      ],
      "output_ports": [
        {
          "name": "default",
          "type": "geodataframe",
          "is_default": true,
          "description": "缓冲区分析结果"
        }
      ]
    }
  ],
  "count": 1
}
```

### 2. 工作流执行
```
POST /api/workflow
```

请求格式:
```json
{
  "workflow_def": {
    "tasks": []
  },
  "input_data": {}
}
```

响应格式:
```json
{
  "status": "success",
  "execution_id": "uuid",
  "final_result": {},
  "all_results": {}
}
```

### 3. 单算子 direct 调用
```
POST /api/operators/{name}/invoke
```

该接口只允许调用 `execution_modes` 包含 `direct` 的算子，用于业务模块受控调用单个算子。它不创建 Develop/Orchestrator/Monitor 任务；凡是需要调度、重试、跨模块编排或统一监控的执行，必须走工作流。

### 4. 健康检查
```
GET /health
```

## 能力声明

引擎能力统一使用 `engine.capabilities/v1` 结构。Common engine 插件由 `Capabilities()` 声明能力模板；非插件内置 Workflow Runtime 在启动注册时提交与 `engine_type` 一致的标准能力声明。

能力只表达引擎自身 native / provider 能力，例如 `compute.workflow`、`compute.script`、`storage.catalog`、`storage.store`。不要在引擎能力中维护 Transfer、Preview、Develop 等模块对引擎的适配列表。

工作流引擎的算子列表、参数、输出端口等动态能力不写入 `capabilities`，通过 `WorkflowRuntimeProvider.ListOperators()` 实时发现。

## 引擎注册

工作流运行时注册到 System 资源中心后，才会成为 ADDP 可发现和可调用的引擎实例。生产内置运行时在自身监听就绪后异步自注册；System 未就绪时持续进行有上限的退避重试，注册过程不阻塞 Runtime readiness。参考实现和用户自研扩展运行时可以在 System 引擎管理中手动注册。

**生产内置运行时自注册端点**: `POST http://system-backend:8180/api/v1/system/runtime/engines`

Runtime 使用自身 Confidential OAuth Client 获取 Platform Service Access Token，并只发送 `Authorization: Bearer <platform_service_access_token>`。

**注册数据格式**:
```json
{
  "engine_type": "geopython_workflow",
  "name": "GeoPython Workflow",
  "description": "基于 Python 的工作流执行引擎",
  "connection_info": {
    "protocol": "http",
    "host": "geopython-workflow-engine",
    "port": 8099
  },
  "is_builtin": true
}
```

容器 Runtime 必须通过 `host` 声明稳定、可从其他 ADDP 服务访问的服务名；本地 Runtime 未声明时由 System 根据请求来源规范化为 `localhost`。不得把临时容器 IP 保存为 Engine Instance 身份。

Math Workflow 是参考实现，随无参数全量启动运行服务，但不会随 `-develop` 隐式启动，也不会自动注册。需要使用时，显式执行 `scripts/dev/start.sh -math-workflow`，再在 System 引擎管理中使用扩展引擎注册表单填入示例值、测试连接并保存。

## 新增独立 Runtime checklist

创建不拥有业务配置事实的独立计算 Runtime 时，请遵循以下步骤：

- [ ] 在`engines/`目录下创建引擎目录
- [ ] 实现 `addp.workflow/v1` HTTP 协议（`/health`、`/api/operators`、`/api/workflow`、`/api/operators/{name}/invoke`、`/api/executions/{id}`）
- [ ] 在 common engine 插件中声明 `engine.capabilities/v1` 能力
- [ ] 决定注册方式：生产运行时可配置启动自注册；参考实现可通过 System 引擎管理手动注册
- [ ] 添加到 `scripts/dev/start.sh`
- [ ] 如需容器化部署，添加到 `docker-compose.runtimes.yml` 和 `scripts/build/build-images.sh`；现有 `scripts/local/start.sh`、`scripts/prod/start.sh` 自动启动 Runtime project，并由平台一致性门禁核对登记
- [ ] 编写 README 说明引擎功能和使用方法

## 参考实现

- **Math Workflow Engine**: [math-workflow](./math-workflow/) - 最小工作流参考实现，手动注册示例。
- **GeoPython Workflow**: [geopython-workflow](./geopython-workflow/) - Python 数据处理工作流实现。
- **Spark Workflow Engine**: [spark-workflow](./spark-workflow/) - Spark / Sedona 工作流实现。
- **Model3D Workflow Engine**: [model3d-workflow](./model3d-workflow/) - OSGB 快显和 OSGB Scene 转 3D Tiles 三维模型转换运行时。
- **PointCloud Workflow Engine**: [pointcloud-workflow](./pointcloud-workflow/) - LAS / LAZ / E57 / PCD / XYZ 转 COPC 点云快显转换运行时。
- **Document Workflow Engine**: [document-workflow](./document-workflow/) - Office 文档转 PDF 的专业转换运行时。
- **SuperMap Workflow Engine**: [supermap-workflow](./supermap-workflow/) - 超图 iObjects C++ 空间计算工作流运行时。

- **DuckDB Federated Query Runtime**: [duckdb](./duckdb/) - Develop 与 Service 共用的联邦查询运行时。

## 相关文档

- [ADDP架构设计方案](/Users/pampa/.claude/plans/buzzing-bubbling-porcupine.md)
- [Common模块文档](../common/README.md)
- [System模块文档](../system/CLAUDE.md)
