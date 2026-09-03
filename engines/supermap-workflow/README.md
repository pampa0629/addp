# SuperMap Workflow

`supermap_workflow` 是 ADDP 面向超图 iObjects C++ 的工作流运行时，遵循 `addp.workflow/v1` HTTP 接口。运行时独立校验 `workflow_def`，按 `depends_on` 做稳定拓扑排序，并在单次执行上下文中串行执行 DAG；DAG 内部通过类型化 C++ 句柄传递 SuperMap 内存对象，HTTP 边界只返回 JSON 摘要、最终资产引用和血缘事件。

SuperMap C++ SDK、native `.so` 和许可文件不进入 ADDP 代码仓库。完整 SDK 作为外部只读母版长期保存，通过 `SUPERMAP_CPP_SDK_PATH` 作为 Docker build context 构建稳定基础镜像；许可作为受控制品单独注入。最终运行镜像从该基础镜像复制编译产物和 SDK `bin/bin`，不携带 SDK 的头文件、帮助、样例、支持库目录和构建工具；任何裁剪都不得删除或修改完整母版。

## 第一阶段范围

当前实现提供完整 `addp.workflow/v1` 外壳：

- `GET /health`
- `GET /api/operators`
- `POST /api/workflow`
- `POST /api/operators/{name}/invoke`
- `GET /api/executions/{execution_id}`

当前已内置真实 SuperMap 算子：

- `datasource.open`
- `datasource.open_postgis`
- `datasource.enable_postgis`
- `datasource.upgrade_udbx`
- `datasource.create`
- `dataset.select`
- `dataset.info`
- `dataset.project`
- `vector.filter`
- `vector.spatial_filter`
- `vector.buffer`
- `vector.dissolve`
- `vector.merge`
- `vector.feature_envelope`
- `vector.inner_point`
- `overlay.intersect`
- `overlay.clip`
- `overlay.erase`
- `overlay.union`
- `vector.query`
- `dataset.save`
- `osgb_scene_to_s3m`

这些算子运行在同一个 C++ 进程和同一次执行上下文内，DAG 边上传递 `UGDataSource`、`UGDatasetVector` 等对象的类型化共享句柄；输出到外部时再写入 UDBX 数据源。

HTTP 层使用独立线程池，确保长时间 SuperMap 算子执行期间，`/health`、算子发现和执行状态接口仍可响应。SuperMap 算子执行通过进程内统一锁串行化，DAG 节点也按稳定拓扑顺序串行执行，避免并发访问非线程安全的 Workspace、Datasource 或 Map 对象；HTTP 并发不等于 SuperMap 计算并发。

算子元数据使用 `param_type=input` 标识由工作流连线传入的参数，并用 `supermap.datasource`、`supermap.dataset`、`supermap.query_result` 等细分类型描述 DAG 内部对象，避免多个通用 `object` 输入只能按连线顺序推断。

`datasource.open_postgis` 只打开已有 SuperMap SDX+ for PostGIS 空间表所在数据源，不调用 SuperMap `create`，因此不会主动创建 SuperMap `sm*` 系统表。`datasource.enable_postgis` 是 direct-only 高危算子，用于 System 引擎管理入口显式启用 SuperMap SDX+ for PostGIS 空间工作区，可能在目标 PostgreSQL 数据库中创建 SuperMap 系统表，不进入 Develop 工作流画布。`datasource.open_postgresql` 与 `datasource.enable_postgresql` 对应实现完全不同的 SuperMap SDX+ for PostgreSQL，二者不得在同一 PostgreSQL 实例并存；SDK 固定使用 `sdx` schema，table session 对 Point 的物理 `SmX/SmY` 与其他类型的私有 Geometry 存储统一输出为 `SmGeometry` EWKB。`datasource.upgrade_udbx` 同样是 direct-only 高危算子：它先以 SQLite 只读检查 UDBX 的 SuperMap 系统表与 `SmRegister` 关键字段，再以可写方式打开原文件，由当前 iObjects C++ SDK 完成原位 schema 升级，关闭后再次检查并返回是否发生变更。它不得在 `datasource.open` 或其他普通读取链路中隐式执行；调用方必须先备份文件并记录审计信息。`locator` 只属于 Develop/UI 的资源选择契约；调用 runtime 前，Develop Backend 必须把它派生为 `connection_info`、`schema` 和 `table` 并移除，SuperMap runtime 不解析 ADDP locator。

`osgb_scene_to_s3m` 同时支持 workflow 和 direct 模式，输入输出统一使用 `addp.workflow.access-plan/v1`。源当前接受 NFS `mounted_path`，运行时按访问计划中的 `server + export_path + nfs_version` 动态挂载；源目录必须包含 `metadata.xml` 与 `Data/Tile_*/Tile_*.osgb`。目标支持 NFS `mounted_path` 与 MinIO/S3 `object_store`：对象存储目标先在本地临时目录完成转换，再递归发布。算子在可写临时目录中镜像 OSGB 场景层级，先调用 `UGOSGBCacheBuilder::GenerateOSGBConfigFile` 生成源 SCP，再使用 `UGObliquePhotogrammetryBuilder::ProcessOSGB` 构建 S3M 3.01：纹理压缩固定为 DXT，几何压缩固定为 Draco，文件类型固定为 S3MB，存储类型固定为 `PURE_FILES`。Builder 保留瓦片局部坐标，运行时随后用 SuperMap `UGRefTranslator` 将 JSON manifest 的 `position` 与 `geoBounds` 从源 EPSG 规范化到 EPSG:4326。发布前必须验证 `version=3.01`、`crs=epsg:4326`、`position.unit=Degree`、位置经纬度范围、`s3m:TextureCompressionType=DXT`、`s3m:VertexCompressionType=DRACO` 及所有根瓦片存在。当前输出为 `config/scene.scp + config/Data/**/*.s3mb`，manifest 中的 `./Data/...` 相对 `config/scene.scp` 解析。Develop workflow 结果是业务存储中的 `format=s3m + layout=whole` 数据集并触发 Meta scan；Manager direct 结果写入 Manager infra MinIO 并由 Manager 维护生命周期。

UDBX 升级调试示例：

```bash
curl -s http://localhost:8103/api/operators/datasource.upgrade_udbx/invoke \
  -H 'Content-Type: application/json' \
  -d '{
    "params": {
      "connection_info": {},
      "path": "/tmp/supermap-out/legacy.udbx",
      "alias": "legacy_upgrade"
    }
  }'
```

NFS 正式调用时，`connection_info.engine_type` 必须为 `nfs`，`path` 必须是该 NFS export 内的相对路径。响应中 `schema_current=true` 表示当前 SDK 要求的系统表和关键字段齐备，`changed=true` 表示本次调用将旧 schema 升级到当前 schema。已是当前 schema 时算子保持幂等并返回 `changed=false`。

本地开发时，如果 System 中的 PostgreSQL 引擎登记为 `localhost` 或 `127.0.0.1`，SuperMap runtime 容器会通过 `SUPERMAP_RESOURCE_LOCALHOST_ALIAS` 将其映射为容器可访问的宿主机地址。`scripts/dev/start.sh` 和 `scripts/dev/restart.sh` 默认传入 `host.docker.internal`；生产部署应登记容器网络或集群内可直接访问的主机名。

## 本地依赖与两层镜像

SuperMap Workflow 只保留一条本地开发路线：稳定基础镜像承载完整 C++ SDK 和系统依赖，代码镜像在构建阶段编译 ADDP C++ 源码。不得恢复 Java/GPA 回退、胖镜像/瘦镜像判断、源码 bind mount、运行时编译或 `SUPERMAP_WORKFLOW_REBUILD` 等并行路径。

### 许可边界

当前开发与验证环境接受 `supermap_any_2026.lic12` 试用许可。该许可可被 iObjects C++ Runtime 正常识别，二维空间分析和 OSGB Scene 转 S3M 均可执行；不增加正式许可与试用许可双轨判断。

Runtime 启动时会验证 iObjects C++ Core 许可可用，`/health` 只表达当前许可足以启动和执行已验证算子，不承诺无试用水印。现有试用许可有效期截至 2026-12-31；到期前应更新 `vendor/license` 中的受控许可并重建完整基础镜像，不能只替换已经构建完成的代码镜像。

本地目录固定为：

```text
engines/supermap-workflow/
├── vendor/
│   └── license/
├── Dockerfile.base
├── Dockerfile
├── CMakeLists.txt
├── src/
└── run.sh
```

`vendor/` 被仓库全局 `.gitignore` 忽略，并被本目录 `.dockerignore` 排除出日常代码镜像上下文。SuperMap SDK、native 库和许可文件不得提交到 Git，也不得推送到公共镜像仓库。完整 SDK 母版保存在仓库之外，并在内部制品库保留原始压缩包、版本号和 SHA-256。

首次安装或 SuperMap 组件升级时，显式指定完整 SDK 母版路径并构建稳定基础镜像：

```bash
SUPERMAP_CPP_SDK_PATH=/path/to/supermap-iobjectscpp-12.1.0-linux-arm64-all \
  bash scripts/build/build-supermap-workflow-base.sh
```

基础镜像默认名为 `addp-supermap-workflow-base:local`，包含编译工具、完整 SuperMap C++ SDK、许可和 NFS/SQLite/Qt 系统依赖。它不包含 ADDP Runtime 源码，因此日常业务代码修改不需要重建基础镜像。完整 SDK 基础镜像用于编译和未来扩展算子；最终运行镜像只承载运行期文件。

完整 SDK 自带的 FreeType 版本与 Ubuntu 24.04 Qt/Harfbuzz 不兼容。完整 SDK 母版和基础镜像仍原样保留该库；代码镜像的构建阶段和最终运行层只删除 SDK 副本中的 `libfreetype.so*`，统一使用系统 `libfreetype.so.6`。构建阶段不得全局设置 SDK `LD_LIBRARY_PATH`，否则 CMake 会误加载 SDK 自带 `libcurl/jsoncpp`；运行阶段只允许把 `/opt/supermap/bin/bin` 加入 `LD_LIBRARY_PATH`，不得加入 SDK `support/libs`。

### 镜像裁剪结果

以下是 2026-08-04 在 Linux ARM64 镜像上的实测结果。`压缩内容` 取自 `docker image inspect .Size`，`本地展开` 取自 Docker Desktop 的镜像列表；不同基础镜像补丁版本可能产生小幅变化。

| 镜像 | 压缩内容 | 本地展开 | 说明 |
| --- | ---: | ---: | --- |
| 原 Java/GPA 参考镜像 | 1.515 GB | 6.16 GB | ObjectsJava、GPA、JRE 和系统运行库 |
| 完整 C++ 候选 | 1.510 GB | 6.66 GB | 直接继承完整 SDK 基础镜像，未裁剪 |
| 第一轮 C++ 运行镜像 | 0.963 GB | 4.75 GB | 多阶段构建，只保留 SDK `bin/bin` 和运行依赖 |
| 当前 C++ 运行镜像 | 0.478 GB | 2.02 GB | 在第一轮基础上按动态追踪结果裁剪可选插件和资源包 |

当前 C++ 运行镜像相对 Java/GPA 参考镜像，压缩内容减少约 68.4%，本地展开体积减少约 67.2%；相对第一轮 C++ 运行镜像，分别继续减少约 50.4% 和 57.5%。第一轮裁剪排除了 `include/`、`help/`、`sample/`、`support/`、编译器、CMake 和其他开发文件，运行镜像不包含 JRE、ObjectsJava 或 GPA runtime。

第二轮对 20 节点二维 DAG、PostGIS 只读打开、CAD 检查与渲染、UDBX 升级和 OSGB 转 S3M 做了完整动态文件访问追踪，共确认 338 个实际打开的 SDK 文件。在与追踪结果零冲突的前提下，删除了 124 个未使用的 IFC/BIM、天气、海洋和 JVM 顶层插件文件，以及 `SilverLiningResource`、`OceanResource` 两个资源目录；这些文件在完整 SDK 中的展开体积合计约 2.25 GB。

S3M 转换实际会动态加载 PCL、VTK 和 OpenVDB，因此这些库必须保留。CAD 核心实际会加载 `libUDKernel.so`、`libUDDrawing.so`、`libUDComponents.so` 和 `libUDDgn.so`，也必须保留。完整 SDK 母版和基础镜像继续保存全部插件、头文件和开发资源，以支持未来新增算子；后续扩大裁剪范围时仍必须先补充动态追踪场景，再重跑 24 个算子、PostGIS、UDBX、CAD、S3M、动态 NFS 和 MinIO/S3 回归，不能仅按静态链接关系删除 `.so`、`.sdx`、`.spi`、`.l3d` 或资源文件。

日常启动和重启统一使用：

```bash
bash scripts/dev/start.sh -supermap-workflow
bash scripts/dev/restart.sh -supermap-workflow
```

`restart.sh -supermap-workflow` 和 `restart.sh -all` 会根据 C++ 源码、`CMakeLists.txt`、`run.sh`、Dockerfile、基础镜像 ID 和目标平台计算构建指纹。指纹与现有代码镜像一致时直接复用；输入变化或镜像不存在时才在镜像构建阶段重新编译。之后脚本会替换 8103 容器、等待 healthy 并向 System 注册。C++ 编译失败会在旧容器被删除前终止，不会启动旧二进制。

代码镜像默认名为 `addp-supermap-workflow-engine:dev`。可配置项只保留运行参数：

```bash
SUPERMAP_WORKFLOW_BASE_IMAGE=addp-supermap-workflow-base:local
SUPERMAP_WORKFLOW_IMAGE=addp-supermap-workflow-engine:dev
SUPERMAP_WORKFLOW_PORT=8103
SUPERMAP_DATA_HOST_PATH=/path/to/supermap/data
SUPERMAP_OUTPUT_HOST_PATH=/tmp/supermap-out
SUPERMAP_WORKFLOW_MEMORY_LIMIT=8g
```

工作流算子由 Manager、Meta、Develop 等调用方实时从 `/api/operators` 发现。只修改 SuperMap C++ 代码时，无需重启 System、Manager 或 Meta；局部重启 SuperMap Workflow 即可。

## 验证

健康检查：

```bash
curl http://localhost:8103/health
```

执行 UDBX 叠加分析 DAG：

```bash
curl -s http://localhost:8103/api/workflow \
  -H 'Content-Type: application/json' \
  -d '{
    "workflow_def": {
      "tasks": [
        {
          "id": "open_input",
          "operator": "datasource.open",
          "params": {"path": "/mnt/supermap/data/example_data.udbx", "alias": "example_data", "read_only": true},
          "depends_on": []
        },
        {
          "id": "create_output",
          "operator": "datasource.create",
          "params": {"path": "/tmp/supermap-out/addp_overlay_result.udbx", "alias": "overlay_output", "overwrite": true},
          "depends_on": []
        },
        {
          "id": "select_landuse",
          "operator": "dataset.select",
          "params": {"datasource": {"$ref": "open_input"}, "dataset_name": "Landuse_R"},
          "depends_on": ["open_input"]
        },
        {
          "id": "select_geomor",
          "operator": "dataset.select",
          "params": {"datasource": {"$ref": "open_input"}, "dataset_name": "Geomor_R"},
          "depends_on": ["open_input"]
        },
        {
          "id": "intersect",
          "operator": "overlay.intersect",
          "params": {
            "input_dataset": {"$ref": "select_landuse"},
            "overlay_dataset": {"$ref": "select_geomor"},
            "output_datasource": {"$ref": "create_output"},
            "output_dataset_name": "OverlayOutput",
            "overwrite": true
          },
          "depends_on": ["select_landuse", "select_geomor", "create_output"]
        }
      ]
    },
    "input_data": {}
  }'
```

期望 `status=success`，`all_results.intersect.kind=supermap_dataset`，并生成目标 UDBX 文件。

上面的 `/tmp/supermap-out` 只用于 runtime 直接调试。Develop 正式任务中，`datasource.create` 应通过 `target_parent_locator + target_name` 选择 NFS 目录；Develop Backend 在执行期把 NFS 引擎的通用连接事实（`server`、`export_path`、可选 `nfs_version`）和挂载根内相对 `path` 传给 runtime，runtime 在容器内动态挂载该 NFS export 后由 SuperMap C++ 直接写入 UDBX。NFS 引擎不需要、也不应配置工作流专用挂载路径。UDBX 暂不支持直接写入 MinIO / S3。

执行已验证的示例分析 DAG：

```bash
SUPERMAP_DATA_HOST_PATH=/path/to/supermap-iobjectspy-2026-linux-arm64/data \
SUPERMAP_OUTPUT_HOST_PATH=/tmp/supermap-out \
./scripts/dev/restart.sh -supermap-workflow

curl -s http://localhost:8103/api/workflow \
  -H 'Content-Type: application/json' \
  -d '{
    "workflow_def": {
      "tasks": [
        {"id":"open_input","operator":"datasource.open","params":{"path":"/mnt/supermap/data/example_data.udbx","alias":"example_data","read_only":true},"depends_on":[]},
        {"id":"create_output","operator":"datasource.create","params":{"path":"/tmp/supermap-out/addp_supermap_analysis.udbx","alias":"addp_supermap_analysis","overwrite":true},"depends_on":[]},
        {"id":"select_landuse","operator":"dataset.select","params":{"datasource":{"$ref":"open_input"},"dataset_name":"Landuse_R"},"depends_on":["open_input"]},
        {"id":"filter_large","operator":"vector.filter","params":{"dataset":{"$ref":"select_landuse"},"output_datasource":{"$ref":"create_output"},"output_dataset_name":"LanduseLarge","attribute_filter":"Area > 1000","overwrite":true},"depends_on":["select_landuse","create_output"]},
        {"id":"inner_point","operator":"vector.inner_point","params":{"input_dataset":{"$ref":"filter_large"},"output_datasource":{"$ref":"create_output"},"output_dataset_name":"LanduseLargePoint","overwrite":true},"depends_on":["filter_large","create_output"]},
        {"id":"envelope","operator":"vector.feature_envelope","params":{"input_dataset":{"$ref":"filter_large"},"output_datasource":{"$ref":"create_output"},"output_dataset_name":"LanduseLargeEnvelope","overwrite":true},"depends_on":["filter_large","create_output"]},
        {"id":"project","operator":"dataset.project","params":{"dataset":{"$ref":"filter_large"},"output_datasource":{"$ref":"create_output"},"output_dataset_name":"LanduseLarge3857","target_epsg":3857,"overwrite":true},"depends_on":["filter_large","create_output"]},
        {"id":"dissolve","operator":"vector.dissolve","params":{"input_dataset":{"$ref":"filter_large"},"output_datasource":{"$ref":"create_output"},"output_dataset_name":"LanduseLargeDissolve","field_names":["Area_1"],"dissolve_type":"multipart","overwrite":true},"depends_on":["filter_large","create_output"]},
        {"id":"final_info","operator":"dataset.info","params":{"dataset":{"$ref":"dissolve"}},"depends_on":["dissolve"]}
      ]
    },
    "input_data": {}
  }'
```

该示例用于验证 `vector.filter`、`vector.inner_point`、`vector.feature_envelope`、`dataset.project`、`vector.dissolve` 可在同一 SuperMap C++ DAG 执行上下文中连续执行，并在输出 UDBX 中生成 `LanduseLarge`、`LanduseLargePoint`、`LanduseLargeEnvelope`、`LanduseLarge3857`、`LanduseLargeDissolve`。

Develop 任务管理验证可以通过标准开发任务 API 保存同一 `workflow_definition`，`execution_config.engine_id` 指向 System 中已注册的 `supermap_workflow` 引擎实例。统一执行记录只保存结果摘要和 runtime execution id；完整节点结果可在 runtime 本地状态中查询，后续资产登记和血缘应把需要长期保留的输出数据集引用结构化写入 ADDP 任务/血缘模型。

## 架构约定

- Develop 前端只生成 ADDP `workflow_def`，不直接生成 SuperMap `processflow` JSON。
- 每个 SuperMap 算子在该 runtime 内实现为 C++ Operator，不拆成独立 OS 进程或独立 HTTP 服务。
- DAG 内部优先传递 `UGDatasetVector`、`UGDataSource`、`UGRecordset`、`UGGeometry` 的共享句柄；只有输入、最终输出、显式保存、checkpoint、跨进程边界才落盘。
- HTTP 响应不得返回完整大对象，只返回摘要、保存结果引用和 `lineage_events`。
- 真实空间算子接入时不得硬编码空间字段名，必须从 SuperMap 数据集/字段元数据读取。
