# Engines 计算引擎目录说明

## 模块定位

`engines/` 集中管理不拥有 ADDP 业务配置事实的独立计算与 Notebook 运行时。GeoPython Workflow、Spark Workflow、DuckDB 和 Jupyter 是默认部署的内置运行时；Math Workflow 是 `addp.workflow/v1` 参考实现，用于示范扩展引擎规范；Model3D Workflow 是三维模型转换专用运行时；PointCloud Workflow 是点云处理专用运行时；Document Workflow 是文档转换专用运行时；SuperMap Workflow 是面向超图 iObjects C++ 的工作流运行时。Develop 和业务模块通过引擎能力声明和 HTTP API 发现算子、执行工作流、联邦查询或 Notebook。

本目录不是 `system.engines` 的源码镜像。拥有独立业务资源、数据库事实、权限、控制面和前端的 owner 模块继续位于仓库根目录；例如 `inference/` 拥有 Provider、Deployment、Profile 和凭据，其数据面以 `inference_runtime` Engine Instance 登记到 System，但不得因此把整个模块移动到本目录或复制第二套控制面。

## 重要目录与端口

```text
engines/
├── geopython-workflow/  # GeoPython Workflow 运行时，默认端口 8099
├── spark-workflow/   # Spark 工作流引擎，默认端口 8098
├── math-workflow/    # 数学工作流参考实现，默认端口 8089，开发环境自动启动服务但需手动注册
├── model3d-workflow/ # 三维模型转换运行时，默认端口 8101，开发环境自动启动并自注册，需配置 MODEL3D_CONVERTER_BIN 指向可执行文件路径
├── pointcloud-workflow/ # 点云处理运行时，默认端口 8102；绑定 engine runtime 内部 PDAL 后自注册，POINTCLOUD_PDAL_BIN 不指向宿主机全局命令
├── document-workflow/ # 文档转换运行时，默认端口 8105；镜像内固定 LibreOffice 与字体
├── supermap-workflow/ # 超图 iObjects C++ 工作流运行时，默认端口 8103；通过 Docker 绑定 SuperMap C++ SDK 和许可，不提交 SDK 到仓库
├── jupyter/          # 无头 Notebook Runtime API，默认端口 8097
├── duckdb/           # DuckDB 联邦查询 Runtime，默认端口 8104
└── docs/             # 引擎 API 与设计文档
```

端口以仓库根目录 `.env` 和 `scripts/dev/start.sh` 为准：`GEOPYTHON_WORKFLOW_PORT`、`SPARK_WORKFLOW_PORT`、`MATH_WORKFLOW_PORT`、`MODEL3D_WORKFLOW_PORT`、`POINTCLOUD_WORKFLOW_PORT`、`DOCUMENT_WORKFLOW_PORT`、`SUPERMAP_WORKFLOW_PORT`、`JUPYTER_API_PORT`、`DUCKDB_RUNTIME_PORT`。

## 开发规则

- 新增或修改引擎接口前，阅读 `docs/spec/addp工作流计算引擎接口规范.md`、`docs/spec/addp引擎插件接口规范.md` 和 `docs/spec/addp引擎能力声明规范.md`。
- 引擎应提供健康检查、算子发现、执行接口；所有实现 `addp.workflow/v1` 的运行时在注册时都必须提交完整 `engine.capabilities/v1`，System 按协议探测并保存，Common 通过唯一的通用 HTTP Provider 消费。参考实现可以随开发环境自动启动服务但不自注册，由用户在 System 中按扩展引擎手动注册。
- 生产 Runtime 必须先监听、再异步自注册，并在 System 尚未就绪时持续退避重试；注册失败不得阻塞 Runtime readiness。容器部署必须声明稳定服务名，不能把临时容器 IP 当作 Engine Instance 身份。
- 算子元数据要包含输入、输出、参数、示例和开发模式，保证 Develop 工作流画布可动态消费。
- `pointcloud-workflow` 的 PDAL 属于该 engine runtime 内部依赖；不得要求 Manager、System 或宿主机全局安装 PDAL。未绑定 PDAL 时健康检查应保持 `degraded`，且不自注册。
- `supermap-workflow` 的 SuperMap iObjects C++ 属于该 engine runtime 内部依赖；完整 SDK 作为外部只读母版保存，通过 Docker build context 构建稳定基础镜像，不提交 SDK、native `.so` 或许可文件到仓库。
- SuperMap Workflow 本地开发固定使用两层镜像：基础镜像承载指定版本的完整 C++ SDK 和系统依赖，代码镜像只编译并承载 ADDP C++ Runtime。`restart.sh -supermap-workflow` 和 `restart.sh -all` 必须按 C++ 源码、构建文件、基础镜像 ID 和目标平台计算构建指纹；不得保留 Java/GPA 回退、胖/瘦镜像分支、源码挂载或运行时编译路线。
- 引擎目录中不要沉淀一次性实验脚本；临时验证放到操作系统临时目录。

## 启动与验证

Spark Workflow 确定性测试统一运行 `make test-spark-workflow`，自动发现该目录的 `test_*.py`；覆盖算子协议、可信租户上下文、System Service Bearer 和存储适配，不连接开发集群。根 `make test`、`make test-module MODULE=engines` 与 Platform CI 的独立 Spark Workflow Job 均消费同一入口，CI 使用现有运行时依赖和 Common Python 安装。该 T1 门禁也覆盖唯一 Gunicorn 入口的真实 HTTP 监听、就绪后异步注册、跨请求状态和公共 DAG 执行；不能代替真实 Worker 的 HDFS 分布式读取验收。

GeoPython Workflow 开发环境使用独立 Python 3.12 venv 和同版本原生 GDAL 绑定，PGeo 使用 MDBTools/unixODBC。`scripts/dev/geopython-workflow.sh` 是 start/restart 的唯一准备与启动实现，删除开发镜像路线；stop 按 PID 退出。GDAL/PROJ 路径派生自原生依赖，ODBC 配置限制在 `.dev-state/geopython-odbc`，不继承 Anaconda 的资源变量或系统 site-packages。预检失败发生在停止旧服务之前。开发和产品统一由 `python api_server.py` 进入单 Worker、四线程 Gunicorn，就绪后异步注册，跨请求状态保留在同一 Worker。`make test-geopython-workflow`、生命周期 T1 和原生 ArcGIS Runtime 读取分别验证这些契约；栅格 Hosted T4 独立调用根产品构建入口，核对镜像 ID、默认入口与零残留。

Spark Workflow 本地开发使用 Python 3.11/3.12 虚拟环境与原生 OpenJDK 11，`start.sh -spark-workflow` 和 `restart.sh -spark-workflow` 共用 `scripts/dev/spark-workflow.sh`，通过已有依赖安装互斥锁同步完整 requirements 与 editable Common Python。`stop.sh` 按原生 PID 停止；HTTP 绑定回环，就绪核对 PID、监听和 HTTP。macOS 需要本机 `/etc/hosts` 的 `127.0.0.1 host.docker.internal` 映射，Docker Worker 仍用内置解析。生产与 Hosted 产品验收保留镜像默认入口，使用 `scripts/utils/hosted-online.sh` 的独立产品启动，不依赖开发脚本构建 Spark 镜像。

Spark 产品镜像使用 Python 3.11 Bookworm 和官方 Temurin Java 11 JRE，安装同一仓库的 `common-python`。唯一构建入口为 `make build-images IMAGE_BUILD_ARGS="--services spark-workflow-engine --verify --jobs 1"`，使用根目录构建上下文；本地缓存与 CI 影响选择均覆盖共享 Python 包。镜像构建包含依赖一致性、Flask API 导入及本地 Spark 计算检查；该构建检查不代表分布式 Worker 验收。

HDFS 首版通过独立存储 Engine 的 locator 派生原生 URI，Spark 集群仍单独选择。Spark Workflow 的 `HADOOP_USER_NAME` 固定应用 Simple 用户，并传给 Executor；FileAdapter 校验 RPC authority、管理根、当前 Java UGI 用户，拒绝切换资源身份、glob 扩大读取和 HDFS 写回。`make test-common-hdfs` 使用独占 Hadoop 3.5.0 与 Spark 3.5.0 官方多架构固定 digest 镜像，调用生产 FileAdapter，在真实 Standalone Worker 上读取 CSV/JSON/Parquet、聚合并核对 Worker 主机与身份，覆盖根文件中文、空格和百分号名称。该 T2 已登记 Common gate 与 CI，System/Meta/Manager/Develop 正式链路仍须独立完成 T4。

```bash
bash scripts/dev/start.sh -geopython-workflow
bash scripts/dev/start.sh -spark-workflow
bash scripts/dev/start.sh -math-workflow
bash scripts/dev/start.sh -model3d-workflow
bash scripts/dev/start.sh -pointcloud-workflow
bash scripts/dev/start.sh -document-workflow
bash scripts/dev/start.sh -supermap-workflow
bash scripts/dev/start.sh -jupyter
bash scripts/dev/start.sh -duckdb
```

常用健康检查：

```bash
curl http://localhost:8099/health
curl http://localhost:8098/health
curl http://localhost:8089/health
curl http://localhost:8101/health
curl http://localhost:8102/health
curl http://localhost:8103/health
curl http://localhost:8097/health
curl http://localhost:8104/health
```

## 相关文档

- `engines/README.md`
- `engines/docs/README.md`
- `engines/docs/workflow-engine-api-v1.yaml`
- `develop/CLAUDE.md`
- `docs/spec/addp工作流计算引擎接口规范.md`
