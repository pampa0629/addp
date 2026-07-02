# PointCloud Workflow

`pointcloud_workflow` 是 ADDP 点云处理专用工作流运行时，遵循 `addp.workflow/v1` HTTP 接口。

第一版只提供 direct operator：

- `las_to_copc`：LAS 点云生成 Manager 受管 COPC 快显 artifact。
- `laz_to_copc`：LAZ 点云生成 Manager 受管 COPC 快显 artifact。
- `e57_to_copc`：E57 扫描点云生成 Manager 受管 COPC 快显 artifact。

运行时通过 PDAL 执行实际转换。PDAL 是 `pointcloud_workflow` engine runtime 的内部依赖，不要求、也不建议安装为宿主机全局命令。生成的 COPC 是 Manager 私有快显 artifact，存放在 Manager infra MinIO 中，不自动升格为业务 data item。源数据本身已经是 `format=copc` 时由 Manager 基础预览直接读取，不进入该运行时。

容器运行时默认使用镜像内的 `/opt/conda/bin/pdal`。开发模式如需覆盖 PDAL 可执行文件路径，必须指向 engine runtime 绑定的绝对路径，例如随 engine 分发的 `engines/pointcloud-workflow/bin/pdal`，不能只写 `pdal` 这类依赖系统 `PATH` 的命令名，也不要指向宿主机全局安装路径：

```bash
export POINTCLOUD_PDAL_BIN=/Users/pampa/code/addp/engines/pointcloud-workflow/bin/pdal
```

## 启动

推荐使用容器镜像启动生产运行时，镜像内置 PDAL：

```bash
docker compose up -d pointcloud-workflow-engine
```

开发模式只启动 HTTP runtime；如果没有绑定 engine 内部 PDAL，健康检查会返回 `degraded` 且不会自注册到 System：

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
