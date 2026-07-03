# PointCloud Workflow

`pointcloud_workflow` 是 ADDP 点云处理专用工作流运行时，遵循 `addp.workflow/v1` HTTP 接口。

第一版只提供 direct operator：

- `las_to_copc`：LAS 点云生成 Manager 受管 COPC 快显 artifact。
- `laz_to_copc`：LAZ 点云生成 Manager 受管 COPC 快显 artifact。
- `e57_to_copc`：E57 扫描点云生成 Manager 受管 COPC 快显 artifact。
- `pcd_to_copc`：PCD 点云生成 Manager 受管 COPC 快显 artifact。
- `xyz_to_copc`：简单文本 XYZ 点云生成 Manager 受管 COPC 快显 artifact。

运行时通过 PDAL 执行实际转换。PDAL 是 `pointcloud_workflow` engine runtime 的内部依赖，不要求、也不建议安装为宿主机全局命令。Manager 负责把源数据派生为本地挂载路径或 `/vsicurl/` URI，并传入 Manager infra MinIO 发布计划；运行时不解析 ADDP locator。当前 PDAL 2.10.2 实测 `writers.copc` 不能可靠直接写 `/vsis3/` 目标，因此单一路线是 PDAL 先写入受控工作目录，再上传为 Manager 私有 COPC artifact。生成的 COPC 存放在 Manager infra MinIO 中，不自动升格为业务 data item。源数据本身已经是 `format=copc` 时由 Manager 基础预览直接读取，不进入该运行时。XYZ 第一阶段只支持简单确定性文本 XYZ，不引入列映射 UI 或通用文本 schema 配置。

PCD / XYZ 转换仍以 PDAL 为唯一执行路线。为覆盖 NFS 样例和常见轻量样本：

- `pcd_to_copc` 会在临时目录中规范化 legacy PCD header 的 `VERSION .x` / `VERSION 0.0-0.6` 写法为 PDAL 可读取的 `VERSION 0.7`，不修改源文件。
- `xyz_to_copc` 显式使用 `readers.text`，按空白分隔的 `X Y Z` 三列读取。

容器运行时默认使用镜像内的 `/opt/conda/bin/pdal`。如需覆盖 PDAL 可执行文件路径，必须指向 engine runtime 内部绑定的绝对路径，不能只写 `pdal` 这类依赖系统 `PATH` 的命令名，也不要指向宿主机全局安装路径。

COPC 写入不是纯流式写。运行时默认使用 `/work/pointcloud` 作为 `POINTCLOUD_WORK_DIR` 和 `CPL_TMPDIR`，开发脚本和 Compose 会把该目录挂载为可配置的宿主机目录 `${POINTCLOUD_WORK_HOST_PATH:-data/pointcloud-work}`。大点云转换时应把该目录放在容量足够的磁盘或卷上。

COPC 生成默认向 PDAL `writers.copc` 传入 `threads=4`，可通过容器环境变量 `POINTCLOUD_COPC_THREADS` 调整，运行时会限制在 `1..8`。该参数只控制单个 COPC 转换任务内部的压缩/写入并行度，不改变 Manager 任务调度并发。进度回调发送间隔默认 5 秒，可通过 `POINTCLOUD_PROGRESS_INTERVAL_SECONDS` 调整到 `1..60` 秒。

## 启动

推荐使用开发脚本或容器镜像启动运行时，镜像内置 PDAL：

```bash
bash scripts/dev/start.sh -pointcloud-workflow
bash scripts/dev/restart.sh -pointcloud-workflow
```

开发脚本会启动 `pointcloud-workflow-engine` 容器，并将 `${POINTCLOUD_DATA_HOST_PATH:-business/nfs/data}` 挂载到容器内同一路径，使 Manager 传入的 NFS 本地文件路径可被容器直接读取。`${POINTCLOUD_WORK_HOST_PATH:-data/pointcloud-work}` 会挂载为 `/work/pointcloud`，供 PDAL/GDAL 临时随机写使用。容器通过 `host.docker.internal` 访问宿主机上的 System 和 infra MinIO，并自注册为 `localhost:8102`，供宿主机上的 Manager 后端调用。

```bash
docker compose up -d pointcloud-workflow-engine
```

仅调试 HTTP runtime 时可以直接运行 Python 服务；该方式不会提供 PDAL，除非显式绑定 engine runtime 内部的 PDAL 路径，因此 `/health` 会返回 `degraded` 且不会自注册到 System。开发和端到端验证应使用上面的容器路线：

```bash
cd engines/pointcloud-workflow
pip install -r requirements.txt
PORT=8102 python api_server.py
```

## 测试

```bash
python -m pip install -r engines/pointcloud-workflow/requirements-dev.txt
PYTHONPATH=engines/pointcloud-workflow:engines/docs pytest engines/pointcloud-workflow
```
