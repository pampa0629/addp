# PointCloud Workflow

`pointcloud_workflow` 是 ADDP 点云处理专用工作流运行时，遵循 `addp.workflow/v1` HTTP 接口。

以下转换算子同时支持 Develop workflow 和 Manager direct 调用：

- `las_to_copc`：LAS 点云转换为持久化 COPC。
- `laz_to_copc`：LAZ 点云转换为持久化 COPC。
- `e57_to_copc`：E57 扫描点云转换为持久化 COPC。
- `pcd_to_copc`：PCD 点云转换为持久化 COPC。
- `xyz_to_copc`：简单文本 XYZ 点云转换为持久化 COPC。

运行时通过 PDAL 执行实际转换。PDAL 是 `pointcloud_workflow` engine runtime 的内部依赖，不要求、也不建议安装为宿主机全局命令。Runtime Operator Spec 统一消费 `addp.workflow.access-plan/v1`，对象存储源由运行时生成受控读取方式；运行时不解析 ADDP locator。当前 PDAL 2.10.2 实测 `writers.copc` 不能可靠直接写 `/vsis3/` 目标，因此单一路线是 PDAL 先写入受控工作目录，再按目标计划发布。Manager direct 结果属于 infra 快显 artifact；Develop workflow 结果属于用户选择的业务存储并触发 Meta scan。源数据本身已经是 `format=copc` 时直接读取，不进入该运行时。XYZ 第一阶段只支持简单确定性文本 XYZ，不引入列映射 UI 或通用文本 schema 配置。

PCD / XYZ 转换仍以 PDAL 为唯一执行路线。为覆盖 NFS 样例和常见轻量样本：

- `pcd_to_copc` 会在临时目录中规范化 legacy PCD header 的 `VERSION .x` / `VERSION 0.0-0.6` 写法为 PDAL 可读取的 `VERSION 0.7`，不修改源文件。
- `xyz_to_copc` 显式使用 `readers.text`，按空白分隔的 `X Y Z` 三列读取。

开发运行时使用独立 Conda 前缀 `engines/pointcloud-workflow/venv`，其中同时安装 Python 3.12、`libpdal-core=2.10.2` 和 `libpdal-e57=2.10.2`。PDAL 及其 GDAL、PROJ、E57 原生库属于该运行时；不安装到 Conda base，不继承 Homebrew 原生库或其他环境的资源、插件路径。生产 Compose 仍使用正式产品镜像中的 `/opt/conda/bin/pdal`。

## 开发启动

先安装 Conda（推荐 Miniforge），使 `conda` 可执行文件可用，再执行标准入口：

```bash
bash scripts/dev/start.sh -pointcloud-workflow
bash scripts/dev/restart.sh -pointcloud-workflow
```

两者共用 `scripts/dev/pointcloud-workflow.sh`，按 `native-packages.txt` 创建/同步独立环境，再复用平台 Python 依赖安装锁、完整 requirements、editable `common-python` 与依赖指纹。启动前检查 Python/PDAL 版本、LAS、E57、PCD、text、COPC 驱动、资源目录，并实际转换三个 XYZ 点及读取 COPC 点数。失败不启动服务。已有原生进程运行时只校验环境，不修改依赖；依赖需变化时可在终端执行 restart，标准入口会先通过 stop.sh 停止所选 Runtime，再由 start.sh 同步依赖、验证原生能力并启动；准备失败的 Runtime 不启动，入口返回非零。

HTTP 仅监听 `127.0.0.1` 与已分配的 `POINTCLOUD_WORKFLOW_PORT`，按原生 PID 和监听归属判定就绪。System 与对象存储端点按宿主机实际地址直接访问，不进行容器 host gateway 改写。NFS 源文件直接读取本机路径；工作目录为 `${POINTCLOUD_WORK_HOST_PATH:-data/pointcloud-work}`，COPC 仍先写受控临时文件，再按 access plan 发布到目标存储。目录应有足够磁盘空间。

首次迁移需由用户停止并删除旧 `pointcloud-workflow-engine` 开发容器；若已有普通 Python venv，先将其移出上述前缀再启动。开发入口拒绝使用非 Conda 前缀，也不会删除未知环境或接管外部监听者。此后的 start/restart/stop 仅管理原生进程。日志为 `logs/pointcloud-workflow-engine.log`。

当前已有旧开发容器和普通 venv 的工作区，首次切换由用户在终端执行（旧 venv 移到临时目录保留）：

```bash
docker stop pointcloud-workflow-engine
docker rm pointcloud-workflow-engine
pointcloud_backup=$(mktemp -d /tmp/addp-pointcloud-old.XXXXXX)
mv engines/pointcloud-workflow/venv "$pointcloud_backup/venv"
./scripts/dev/restart.sh -pointcloud-workflow
```

`POINTCLOUD_COPC_THREADS` 默认 4，限制为 `1..8`；进度回调间隔 `POINTCLOUD_PROGRESS_INTERVAL_SECONDS` 默认 5 秒，限制为 `1..60`。运行时专属 PDAL 绝对路径由开发入口注入。

## 产品部署

```bash
docker compose up -d pointcloud-workflow-engine
```

产品镜像继续由根构建入口维护，与原生开发入口分离，不作为开发启动的备选路线。

## 测试

```bash
make test-pointcloud-workflow
make test-pointcloud-native
```

前者运行 HTTP/算子确定性测试；后者使用独立 PDAL 验证五种格式的真实 COPC 转换与原生依赖预检。两者均已登记 Platform CI；Hosted Manager T4 使用同一原生准备入口。
