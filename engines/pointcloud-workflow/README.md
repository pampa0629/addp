# PointCloud Workflow

`pointcloud_workflow` 是 ADDP 点云处理专用工作流运行时，遵循 `addp.workflow/v1` HTTP 接口。

第一版只提供 direct operator：

- `las_to_copc`：LAS 点云生成 Manager 受管 COPC 快显 artifact。
- `laz_to_copc`：LAZ 点云生成 Manager 受管 COPC 快显 artifact。
- `e57_to_copc`：E57 扫描点云生成 Manager 受管 COPC 快显 artifact。

运行时通过 PDAL 执行实际转换。生成的 COPC 是 Manager 私有快显 artifact，存放在 Manager infra MinIO 中，不自动升格为业务 data item。源数据本身已经是 `format=copc` 时由 Manager 基础预览直接读取，不进入该运行时。

可用环境变量覆盖 PDAL 可执行文件路径，但不能只写 `pdal` 这类依赖系统 `PATH` 的命令名：

```bash
export POINTCLOUD_PDAL_BIN=/path/to/pdal
```

## 启动

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
