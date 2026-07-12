# SuperMap Workflow

`supermap_workflow` 是 ADDP 面向超图 iObjects Java / SPS 的工作流运行时，遵循 `addp.workflow/v1` HTTP 接口。运行时内部把 ADDP `workflow_def` 编译为 SuperMap SPS `IWorkflow`，由 `WorkflowExecutor.execute()` 一次性执行 DAG；DAG 内部通过 SPS `IDataItem` 传递 Java 内存对象，HTTP 边界只返回 JSON 摘要、最终资产引用和血缘事件。

SuperMap SDK、GPA/SPS jar、native `.so` 和许可文件不进入 ADDP 代码仓库。开发验证时通过 Docker volume 挂载本机已解压的 `objectsjava/bin_linux_arm64` 和 PDT scheduler 的 `gpa/libs`；生产部署时应制作受控私有镜像或挂载只读 SDK 卷，并把许可作为受控文件放入 SuperMap bin 目录或按 SuperMap 官方许可机制配置。

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

这些算子运行在同一个 JVM / 同一次 `WorkflowExecutor.execute()` 内，DAG 边上传递 `Datasource`、`DatasetVector` 等 Java 对象或轻量引用；输出到外部时再写入 UDBX 数据源。

算子元数据使用 `param_type=input` 标识由工作流连线传入的参数，并用 `supermap.datasource`、`supermap.dataset`、`supermap.query_result` 等细分类型描述 SPS DAG 内部对象，避免多个通用 `object` 输入只能按连线顺序推断。

`datasource.open_postgis` 只打开已有 PostGIS 空间表所在数据源，不调用 SuperMap `create`，因此不会主动创建 SuperMap `sm*` 系统表。`datasource.enable_postgis` 是 direct-only 高危算子，用于 System 引擎管理入口显式启用 SuperMap SDX+ 空间工作区，可能在目标 PostgreSQL 数据库中创建 SuperMap 系统表，不进入 Develop 工作流画布。`locator` 只属于 Develop/UI 的资源选择契约；调用 runtime 前，Develop Backend 必须把它派生为 `connection_info`、`schema` 和 `table` 并移除，SuperMap runtime 不解析 ADDP locator。

本地开发时，如果 System 中的 PostgreSQL 引擎登记为 `localhost` 或 `127.0.0.1`，SuperMap runtime 容器会通过 `SUPERMAP_RESOURCE_LOCALHOST_ALIAS` 将其映射为容器可访问的宿主机地址。`scripts/dev/start.sh` 和 `scripts/dev/restart.sh` 默认传入 `host.docker.internal`；生产部署应登记容器网络或集群内可直接访问的主机名。

## Docker 运行

开发瘦镜像只包含 ADDP runtime 源码，不包含 SuperMap SDK / GPA libs：

```bash
docker build -t addp-supermap-workflow-engine:dev engines/supermap-workflow
```

使用本机已解压的 SuperMap Java 组件运行。`SUPERMAP_OBJECTSJAVA_BIN` 应指向完整 iObjects Java `Bin` 目录；该目录必须包含 SuperMap native 依赖，例如 `libgeos311.so.3.11.1`、`libminizip.so.1`、`libpng12.so.0`。仅挂载 iObjectSpy Python 包里的 `objectsjava/bin_linux_arm64` 可能缺少部分二级 native 依赖。

推荐通过 ADDP 开发脚本启动，脚本会构建镜像、挂载 SDK/GPA libs、等待 `/health` 就绪，并把 runtime 暴露在 `SUPERMAP_WORKFLOW_PORT`（默认 `8103`）。如果 `INTERNAL_API_KEY` 可用，脚本会同时把 `supermap_workflow` 作为平台级内置引擎注册到 System：

```bash
SUPERMAP_OBJECTSJAVA_BIN_HOST=/path/to/supermap-iobjectsjava/Bin \
SUPERMAP_GPA_LIB_DIR_HOST=/path/to/scheduler/gpa/libs \
SUPERMAP_DATA_HOST_PATH=/path/to/supermap/data \
bash scripts/dev/start.sh -supermap-workflow
```

局部重启可使用：

```bash
SUPERMAP_OBJECTSJAVA_BIN_HOST=/path/to/supermap-iobjectsjava/Bin \
SUPERMAP_GPA_LIB_DIR_HOST=/path/to/scheduler/gpa/libs \
SUPERMAP_WORKFLOW_REBUILD=1 \
bash scripts/dev/restart.sh -supermap-workflow
```

`restart.sh -supermap-workflow` 默认复用已有 `SUPERMAP_WORKFLOW_IMAGE` 镜像；修改本目录 Java 源码或 `run.sh` 后，设置 `SUPERMAP_WORKFLOW_REBUILD=1` 强制重建。

### 私有胖镜像

推荐为日常全量启动构建私有胖镜像。胖镜像已经是完整 `supermap_workflow` engine，不是单纯 SuperMap SDK 基础镜像；它包含：

- ADDP SuperMap Workflow Java runtime
- SuperMap iObjects Java Bin
- SuperMap GPA/SPS libs
- 可选 `.lic12` 许可文件
- NFS 动态挂载依赖 `nfs-common`

构建：

```bash
./scripts/build/build-supermap-workflow-image.sh \
  --objectsjava-bin /tmp/addp-supermap-iobjectjava-bin \
  --gpa-libs /path/to/scp-dc-scheduler/scheduler/gpa/libs \
  --license /path/to/supermap_any_2026.lic12
```

也可以把许可文件放入 `engines/supermap-workflow/license/`，该目录已被 Git 忽略，构建脚本会自动读取其中第一个 `.lic12` 文件。构建成功后，开发脚本会通过镜像 label `addp.supermap.bundled=true` 识别胖镜像，启动时不再要求 `SUPERMAP_OBJECTSJAVA_BIN_HOST` 和 `SUPERMAP_GPA_LIB_DIR_HOST`。

本地快速开发时，`start.sh` / `restart.sh` 默认把 `engines/supermap-workflow/src` 挂载到容器 `/app/src`。修改 Java 算子代码后只需重启 SuperMap runtime，`run.sh` 会在容器内重新编译；修改 Dockerfile、系统依赖、SDK/GPA libs 或 `run.sh` 时才需要重建胖镜像。

也可以直接运行 Docker 容器：

```bash
docker run --rm --platform linux/arm64 \
  --cap-add SYS_ADMIN \
  --security-opt apparmor=unconfined \
  -p 8103:8103 \
  -e PORT=8103 \
  -e SUPERMAP_OBJECTSJAVA_BIN=/opt/supermap/objectsjava/bin_linux_arm64 \
  -e SUPERMAP_GPA_LIB_DIR=/opt/supermap/gpa/libs \
  -v "/path/to/supermap-iobjectsjava/Bin:/opt/supermap/objectsjava/bin_linux_arm64:ro" \
  -v "/path/to/scheduler/gpa/libs:/opt/supermap/gpa/libs:ro" \
  -v "/path/to/supermap/data:/mnt/supermap/data:ro" \
  -v "/tmp/supermap-out:/tmp/supermap-out" \
  addp-supermap-workflow-engine:dev
```

如果使用完整 Java 组件镜像或解压目录，应把 iObjects Java 的 native / jar bin 目录挂载到 `SUPERMAP_OBJECTSJAVA_BIN`，并把包含 `gpa-sps-core-*.jar`、Jackson、Hutool 等依赖的 GPA libs 目录挂载到 `SUPERMAP_GPA_LIB_DIR`。许可文件 `supermap_any_2026.lic12` 应位于 SuperMap bin 目录或由生产镜像按 SuperMap 授权要求放置。

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

上面的 `/tmp/supermap-out` 只用于 runtime 直接调试。Develop 正式任务中，`datasource.create` 应通过 `target_parent_locator + target_name` 选择 NFS 目录；Develop Backend 在执行期把 NFS 引擎的通用连接事实（`server`、`export_path`、可选 `nfs_version`）和挂载根内相对 `path` 传给 runtime，runtime 在容器内动态挂载该 NFS export 后由 SuperMap Java 直接写入 UDBX。NFS 引擎不需要、也不应配置工作流专用挂载路径。UDBX 暂不支持直接写入 MinIO / S3。

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

该示例已验证 `vector.filter`、`vector.inner_point`、`vector.feature_envelope`、`dataset.project`、`vector.dissolve` 可在同一 SuperMap SPS DAG 中连续执行，并在输出 UDBX 中生成 `LanduseLarge`、`LanduseLargePoint`、`LanduseLargeEnvelope`、`LanduseLarge3857`、`LanduseLargeDissolve`。

Develop 任务管理验证可以通过标准开发任务 API 保存同一 `workflow_definition`，`execution_config.engine_id` 指向 System 中已注册的 `supermap_workflow` 引擎实例。统一执行记录只保存结果摘要和 runtime execution id；完整节点结果可在 runtime 本地状态中查询，后续资产登记和血缘应把需要长期保留的输出数据集引用结构化写入 ADDP 任务/血缘模型。

## 架构约定

- Develop 前端只生成 ADDP `workflow_def`，不直接生成 SuperMap `processflow` JSON。
- 每个 SuperMap 算子在该 runtime 内包装为 SPS Process，不拆成独立 OS 进程或独立 HTTP 服务。
- DAG 内部优先传递 `DatasetVector`、`Datasource`、`Recordset`、`Geometry` 或轻量 handle 等 Java 对象；只有输入、最终输出、显式保存、checkpoint、跨进程边界才落盘。
- HTTP 响应不得返回完整大对象，只返回摘要、保存结果引用和 `lineage_events`。
- 真实空间算子接入时不得硬编码空间字段名，必须从 SuperMap 数据集/字段元数据读取。
