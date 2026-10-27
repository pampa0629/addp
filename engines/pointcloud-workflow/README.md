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

## COPC 坐标精度

五个算子使用同一规则逐轴选择 `scale_x/y/z`、`offset_x/y/z`：显式 `options` 优先，其次保留源 LAS/LAZ 文件头中的量化参数；源读取器没有可继承参数（当前 E57、PCD、XYZ）时，由点云引擎按实际坐标范围计算明确参数。不再隐式采用 `0.01`。显式 scale 必须是有限正数或 `auto`，offset 必须是有限数或 `auto`；布尔值、空值、非数值和非有限值均拒绝。部分显式配置仅覆盖对应字段，其他字段仍按相同规则选择；显式 `auto` 同样由引擎计算，不传给 COPC writer。

自动 offset 取该轴最小值。自动 scale 为该轴两端坐标到所选 offset 的最大距离除以 `2147483647`，再向正无穷取相邻浮点数，为 LAS 有符号 32 位整数编码保留舍入余量。offset 取最小值时，公式即该轴跨度除以 `2147483647`；若指定其他 offset，计算仍保证两端可编码。零跨度且坐标等于 offset 时，使用正 scale `1`，整数坐标为 `0`，该轴坐标精确保留；这不构成非零跨度轴的精度下限。显式参数导致整数溢出时拒绝，不静默增大 scale。量化误差以每轴实际 scale 的一半加浮点运算误差为界，不能由此推断测量精度提高；较细 scale 可能增大文件体积。

PDAL 2.10.2 的 COPC `auto` 实际使用 `0.0001` 下限和中心 offset，见其 [Grid 实现](https://github.com/PDAL/PDAL/blob/2.10.2/io/private/copcwriter/Grid.cpp#L116-L137)。引擎不依赖该行为，也不依赖会舍入数值的 PDAL JSON 坐标统计：转换前通过同一 PDAL reader 导出仅含 XYZ 双精度值的临时二进制 PLY，再以每块 16,384 点（384 KiB）计算范围和点数。此步骤额外完整读取源文件一次，临时空间为每点 24 字节加文件头，Python 不把全部点载入内存；PDAL 原生进程的内存需求由其 reader/writer 决定。临时坐标文件在范围读取结束后立即删除，失败时也清理；范围读取与正式转换共用调用超时预算。正式转换仍读取原文件，保留其颜色和其他维度的既有处理。

发布前核对 COPC 文件头中的实际 scale/offset 和点数。算子结果 `quantization` 记录 `scale`、`offset`（均按 X/Y/Z 排列）、`selection`（六个参数的 explicit/source/auto 来源）、`point_count`、`source_bounds` 和 `range_scan`（`elapsed_ms`、`temporary_bytes`）。Manager 已有 `copc_facts` 原样保存这些事实，不新增存储或生成入口。此规则仅影响后续显式发起的转换，不迁移、重建或覆盖已有快显。

## 开发启动

先安装 Conda（推荐 Miniforge），使 `conda` 可执行文件可用，再执行标准入口：

```bash
bash scripts/dev/start.sh -pointcloud-workflow
bash scripts/dev/restart.sh -pointcloud-workflow
```

两者共用 `scripts/dev/pointcloud-workflow.sh`，按 `native-packages.txt` 创建/同步独立环境，再复用平台 Python 依赖安装锁、完整 requirements、editable `common-python` 与依赖指纹。启动前检查 Python/PDAL 版本、LAS、E57、PCD、text、COPC 与二进制 PLY writer 驱动、资源目录，并实际转换三个 XYZ 点及读取 COPC 点数。失败不启动服务。已有原生进程运行时只校验环境，不修改依赖；依赖需变化时可在终端执行 restart，标准入口会先通过 stop.sh 停止所选 Runtime，再由 start.sh 同步依赖、验证原生能力并启动；准备失败的 Runtime 不启动，入口返回非零。

HTTP 仅监听 `127.0.0.1` 与已分配的 `POINTCLOUD_WORKFLOW_PORT`，按原生 PID 和监听归属判定就绪。System 与对象存储端点按宿主机实际地址直接访问，不进行容器 host gateway 改写。NFS 源文件直接读取本机路径；工作目录为 `${POINTCLOUD_WORK_HOST_PATH:-data/pointcloud-work}`，COPC 仍先写受控临时文件，再按 access plan 发布到目标存储。目录应有足够磁盘空间。

开发态 start/restart/stop 仅管理原生进程。开发入口拒绝使用非 Conda 前缀，也不会删除未知环境或接管外部监听者。日志为 `logs/pointcloud-workflow-engine.log`。

`POINTCLOUD_COPC_THREADS` 默认 4，限制为 `1..8`；进度回调间隔 `POINTCLOUD_PROGRESS_INTERVAL_SECONDS` 默认 5 秒，限制为 `1..60`。运行时专属 PDAL 绝对路径由开发入口注入。

## 产品部署

[Local](../../scripts/local/start.sh) 与 [Prod](../../scripts/prod/start.sh) 通过根 [docker-compose.runtimes.yml](../../docker-compose.runtimes.yml) 启动 `pointcloud-workflow-engine`。完整步骤见[部署指南](../../docs/guide/addp部署和开发步骤.md)。

产品镜像继续由根构建入口维护，与原生开发入口分离，不作为开发启动的备选路线。

## 测试

```bash
make test-pointcloud-workflow
make test-pointcloud-native
```

前者运行 HTTP/算子确定性测试；后者使用独立 PDAL 验证五种格式的真实 COPC 转换、精度与点数、溢出拒绝、源文件保留、临时范围文件开销与清理，以及原生依赖预检。两者均已登记 Platform CI；Hosted Manager T4 使用同一原生准备入口。
