# Model3D Workflow

`model3d_workflow` 是 ADDP 三维模型转换专用工作流运行时，遵循 `addp.workflow/v1` HTTP 接口。

以下转换算子同时支持 Develop workflow 和 Manager direct 调用：

- `osgb_to_glb`：单个 `.osgb` 文件转换为持久化 GLB。
- `gltf_to_glb`：`.gltf` manifest 声明的多资源模型打包为持久化 GLB。
- `fbx_to_glb`：FBX 单体网格模型转换为持久化 GLB。
- `obj_to_glb`：OBJ 单体网格模型转换为持久化 GLB。
- `stl_to_glb`：STL 单体网格模型转换为持久化 GLB。
- `dae_to_glb`：Collada 1.4.0 / 1.4.1 静态模型及 PNG/JPEG 漫反射贴图转换为自包含 GLB。
- `3ds_to_glb`：3DS 静态网格及 PNG/JPEG 漫反射贴图转换为自包含 GLB。
- `skp_to_glb`：SketchUp 单文件中的静态网格、组件变换及内嵌 PNG/JPEG 贴图转换为自包含 GLB。
- `max_to_glb`：MAX 静态网格、节点变换及明确声明的漫反射贴图转换为自包含 GLB。
- `ifc_to_glb`：IFC BIM 模型转换为持久化 GLB。
- `osgb_scene_to_3dtiles`：一套 OSGB 倾斜摄影数据集转换为 3D Tiles，支持 NFS/localfs/MinIO/S3 source 输出到 NFS/localfs/MinIO/S3 target。
- `gaussian_splat_to_ksplat`：`gaussian_splat` 的 `ply` / `splat` 源转换为持久化 `.ksplat` 文件。源格式已经是 `ksplat` 时直接读取，不进入转换算子。

Runtime Operator Spec 统一消费 `addp.workflow.access-plan/v1`。Manager direct 调用选择 infra 目标并登记私有快显 artifact；Develop workflow 调用保存公开 ResourceLocator 参数、选择业务目标，并在成功后触发 Meta scan。Runtime 不解析 ADDP locator，也不决定产物归属。

运行时通过随引擎绑定的专业转换器执行实际转换。OSGB / OSGB Scene 使用绑定的 `_3dtile`，普通 mesh 模型使用 Assimp；IFC 使用专用 IfcConvert，默认传入 `--center-model`。MAX 通过引擎内部的 Blender 子进程转换，不增加 Blender 服务。高斯泼溅继续使用运行时内置 Node 脚本与现有锁定依赖。转换器必须绑定当前部署中的真实原生文件，不从系统 PATH 搜索。可用环境变量覆盖到同一运行时部署中的实际可执行文件路径，但不能只写 `_3dtile`、`assimp` 或 `IfcConvert` 这类依赖系统 `PATH` 的命令名：

```bash
export MODEL3D_CONVERTER_BIN=/path/to/_3dtile
export MODEL3D_MESH_CONVERTER_BIN=/path/to/assimp
export MODEL3D_IFC_CONVERTER_BIN=/path/to/IfcConvert
export MODEL3D_GAUSSIAN_SPLAT_NODE_BIN=/path/to/node
export MODEL3D_BLENDER_BIN=/path/to/Blender
export MODEL3D_MAX_ADDON_PATH=/path/to/max-importer/source
```

开发环境绑定私有工具包的绝对可执行文件路径；容器内绑定 `/opt/addp/model3d-workflow/bin/` 的原生转换器。

macOS Apple Silicon 开发态使用 `.dev-state/model3d-native/<指纹>/` 中的私有工具包，不调用 Docker wrapper。`scripts/dev/model3d-workflow.sh` 是 start/restart 共用的唯一准备与启动实现，Python 使用隔离的 3.12 venv。工具包固定现有 3dtiles 源码、vcpkg baseline、Cargo.lock、ADDP patch、Assimp 5.2.5 与官方 IfcConvert 0.8.4-e8eb5e4。版本更新改变指纹；运行中发现依赖漂移时先拒绝，不能修改活动环境或偷偷使用旧工具。

首次安装需要 Conda、Rust 1.92.0、CMake 4.4.0 和 Xcode Command Line Tools。原生依赖只装入私有 Conda 前缀，OSGB 第三方库静态链接，GDAL/PROJ 资源随包提供。首次冷构建耗时较长，安装过程记录到工具包构建日志；后续启动校验缓存并做真实转换预检，不重复编译。当前只验证 Darwin arm64；其他开发平台明确拒绝，不能以容器回退替代未验证的原生安装。

开发工具准备（不启动、停止或注册服务）：

```bash
engines/model3d-workflow/venv/bin/python engines/model3d-workflow/native_setup.py prepare .dev-state/model3d-native
engines/model3d-workflow/venv/bin/python engines/model3d-workflow/blender_setup.py prepare .dev-state/model3d-blender
```

工具包完整性损坏时不会覆盖旧缓存或使用其他转换器：先停止该 Runtime，把报错中的单个指纹目录移出 `.dev-state/model3d-native`，再执行准备命令重建；下载、固定提交源码和 vcpkg 二进制缓存仍可复用。

准备成功后由用户在自己的终端运行 `./scripts/dev/restart.sh -model3d-workflow`。全量和局部重启均先通过 stop.sh 停止所选服务，再由 start.sh 执行同一准备与转换验证；准备失败的服务不启动，入口返回非零。

2026-10-08 本地页面验收：已运行的 macOS 原生 Runtime 经 Manager 正式快显入口完成 Business NFS 的 DAE、带贴图 3DS、IFC 建筑及单体 OSGB 转换。四个任务均为成功、结果可用，浏览器实际显示几何；3DS 彩色贴图与 OSGB 地表贴图可见，旋转和缩放可操作。OSGB execution `a15774db-d803-424c-8d23-ef5fe11413a7` 的 Monitor 详情记录源 item 766 与平台内部 GLB 产物。此记录证明本机页面链路；产品与 Hosted 验收独立记录如下。

2026-10-08 产品与 Hosted 验收：提交 `95428d6e3fec29d72c6f1c8634b1252eeae8e2fb` 的 Linux arm64 标准镜像构建、本次 [Linux x86_64 Hosted Manager T4](https://github.com/pampa0629/addp/actions/runs/37742891117) 及同一提交的 [Product binary build](https://github.com/pampa0629/addp/actions/runs/37742303485/job/113195593339) 均通过。两种架构的产品构建覆盖压缩 OSGB、DXT1/DXT1A/DXT3/DXT5 贴图、SKP 米制缩放，以及 MAX 静态 GLB、声明贴图、默认米／手选毫米和失败保留旧产物。Hosted 使用正式 Model3D 产品镜像的默认入口；普通租户 User 经 System/Gateway/Meta/Manager/Monitor 完成真实资源与产物链路，DAE／3DS 的 GLB 及内嵌 PNG 校验、生成入口和缓存预览两项浏览器用例均通过。五类产物（DAE、3DS、点云、PPTX、栅格）的 Manager 与 Monitor 血缘一致，产物回收和 Infra 清理为零残留。Hosted 三维格式覆盖 DAE／3DS；IFC 与单体 OSGB 的页面验证范围为上述 macOS 本地验收。

2026-10-09 复核 IFC／单体 OSGB Hosted 扩展验收：[运行 37788054702](https://github.com/pampa0629/addp/actions/runs/37788054702) 在 OSGB 生成阶段失败，生成入口浏览器检查通过，退出清理及 Infra 零残留通过；缓存预览未执行，不能计为七类产物验收通过。标准产品构建已追加最终 Runtime 中的压缩 OSGB 和多种暂存路径长度验证，复现了上游 Rust → C++ 输入路径未转为 NUL 终止字符串的越界读取。现有统一转换器补丁改用 `CString` 并在转换失败时返回非零；产品与 macOS 原生工具包消费同一补丁，不增加备用转换路线。Linux ARM64 标准产品构建的修复前长路径回归失败，修复后九种路径长度、无效 OSGB 的非零退出及旧产物保留均通过；Model3D 272 项、Manager Online runner 51 项和构建登记门禁通过。macOS 私有工具包通过标准 `native_setup.py prepare` 重建为指纹 `621681c3cf634777`，内置 OSGB／OBJ／IFC 转换预检通过；同一压缩 OSGB 夹具的九种暂存路径、无效输入非零退出及旧产物保留也通过。本轮未重启开发服务。修复已提交为 `245ac18c57dad63fc1c4be612f57a0e7454af691`；[Hosted 复验 37867825873](https://github.com/pampa0629/addp/actions/runs/37867825873) 已通过 Linux x86_64 正式产品链路：四种模型（DAE／3DS／IFC／OSGB）的 GLB、几何及声明贴图校验通过，生成入口与缓存预览两项浏览器用例通过，IFC 立方体和 OSGB 红色贴图三角形截图已复核。七类产物（四种模型、点云、PPTX、栅格）的七项输入／输出血缘一致，Manager／Monitor 事实相等；派生产物回收及 Infra 清理均零残留。同一代码提交的 Product binary build 通过；Platform CI 整轮因 GeoPython Job 被取消而为 cancelled，不计为整轮通过。

2026-10-09 SKP Hosted 扩展已接入同一 Manager suite：Business 自有英寸三角形的两个实例使用同一内嵌彩色 PNG，Runtime 原生实测生成的 GLB 尺寸为 `[0.0762, 0, 0.0508]` 米，保留共享网格、实例位移与贴图。Meta 只核对格式身份，不伪造 SKP 解析事实；生成入口、缓存消费、Manager／Monitor 血缘与领域清理均沿用现有主路径，完整报告扩为八项输入／输出。`make test-manager-online-runner` 的 53 项测试和完整 `make test-online-runner` 均通过；真实 [Hosted SKP 首轮 37877375089](https://github.com/pampa0629/addp/actions/runs/37877375089) 的五种模型生成入口通过，但缓存预览在 COG 检查处因底图与栅格两个画布触发严格匹配错误，尚未走到模型缓存预览；领域清理通过，Infra 零残留。验收脚本已改为在 Range 响应和加载结束后检查 COG 的 WebGL 画布，保留页面错误与缓存请求检查；[复验 37884331256，attempt 2](https://github.com/pampa0629/addp/actions/runs/37884331256/attempts/2) 已通过提交 `8df7e9b5f58c9ea43c8c719b5489bf76cb9bd3c7` 的正式 Linux x86_64 产品链路：五种模型的生成入口与缓存预览均通过；SKP 的 1804 字节 GLB 保留两个共享网格实例、米制尺寸和原始内嵌 PNG，截图可见两个彩色三角形。八类产物的八项输入／输出血缘相等，领域清理和 Infra 清理零残留。该运行 attempt 1 在 vcpkg 下载 GEOS 时因 `download.osgeo.org:443` 连接失败退出，未执行页面验收；同一提交重跑通过，不改动依赖或转换路线。

同轮确定性门禁通过：`make test-model3d-workflow`（272 项）、`make test-manager-online-runner`（47 项）及 `make test-dev-lifecycle`（86 项 Python 测试和后续 Shell／Go 检查）。开发生命周期首轮有三个 Infra 夹具超时，独立诊断及完整重跑通过。ARM64 构建曾遇到镜像和官方文件下载中断，使用相同版本与标准入口重试后完整通过；本轮未修改或重启开发服务。

本运行时的稳定集成面是 ADDP operator 契约，不是转换器内部 SDK。转换器缺失、执行失败或输出缺失时，`/health` 会标记 `conversion_ready=false`，引擎连接测试和 operator 发现会失败，不生成伪结果。

SKP 使用固定的 MIT 许可 `openskp[textures]==1.3.0`，在当前 Python Runtime 的独立子进程中执行 `skp_converter.py`，不调用 SuperMap、SketchUp 桌面 SDK 或 Assimp。输入为单文件，贴图须已内嵌；导出的毫米坐标通过统一场景根缩放为米，保持 Y 上轴和实例变换。只发布经过统一校验的 GLB，临时 JSON 与缩略图不会发布。首期不声明动态组件、动画、标注、孤立线段或全部 SKP 版本支持；固定版本的导出器未应用可见性，含隐藏组件、隐藏面或关闭图层的模型明确拒绝，避免发布错误场景。解析失败、空场景、缺失贴图和非法 GLB 保留旧产物。项目与发行版依据见平台内置格式规范。


MAX 的工具版本与校验值集中在 `blender-assets.json`。macOS arm64 使用私有工具包内的 Blender 4.5.3；Linux 产品镜像使用 Debian Trixie 的 Blender 4.3.2 和显式安装的 NumPy。两者均使用固定提交的 `io_scene_max` 1.9.2。安装时校验下载归档，保留上游源码及许可：仓库 LICENSE、源码 GPL 许可声明和插件 manifest 的 GPL-3.0-or-later 声明均原样保留。开发工具包逐文件校验完整性，转换不向包内写入 Python 缓存。

MAX 首期覆盖静态网格、实例变换、基础颜色和漫反射图片，不声明动画、骨架或第三方渲染器材质保真。单个 MAX 文件的解析预算为 64 MiB。固定导入器目前不能可靠识别原始系统单位：用户可选择 `options.source_unit`（`mm/cm/m/km/in/ft/mi`）；未选则默认米 `m`。Manager 的 MAX 生成入口提供同一选项，缺省值无需用户确认。结果 `conversion` 记录实际单位、`unit_source=user|default` 与米制换算系数，不把默认米说成自动识别。

外部贴图必须通过 `options.texture_files` 明确映射，key 是 MAX 内保存的 bitmap 引用，value 是输入目录内的相对文件路径。不会读取原电脑绝对路径或猜测同名图片。Manager 的数据探查和任务创建入口提供相同的单位及外部贴图声明组件；声明传入既有快显动作并保存到任务 options，Develop/direct 算子沿用同一映射。缺失贴图、解析错误及不支持的贴图通道会使转换失败，保留旧产物。内置回归样例来自 `tests/fixtures/max/ATTRIBUTION.txt` 所列的 CC-BY-SA-3.0 模型，纹理是 ADDP 自建测试图片。

MAX 的确定性测试由 `make test-model3d-workflow` 自动发现，开发生命周期由 `make test-dev-lifecycle` 验证。正式 Linux 镜像构建入口为：

```bash
make build-images IMAGE_BUILD_ARGS='--services model3d-workflow-engine --verify --jobs 1'
```

现有 Product binary build CI 会按受影响路径选择 Model3D 镜像，构建后用真实 Blender 检查静态 GLB、贴图、默认米与手选毫米的 1000 倍比例、源文件不变及失败时旧产物保留。此检查不代替真实业务源、Manager 页面与 Monitor 的 T4 验收。

## 启动

```bash
./scripts/dev/restart.sh -model3d-workflow
```

## Linux amd64 / arm64 容器

本节仅用于 Linux 产品镜像构建和 Hosted 产品验收；本机开发使用前述私有原生工具包。

```bash
make build-images IMAGE_BUILD_ARGS='--services model3d-workflow-engine --verify --jobs 1'
```

统一构建入口按宿主机 CPU 选择 `linux/amd64` 或 `linux/arm64`，也可通过 `MODEL3D_DOCKER_PLATFORM` 显式选择这两种架构；其他平台直接拒绝。GitHub Hosted Ubuntu x86_64 使用 amd64，不通过 QEMU 运行 arm64 转换器。IfcConvert 固定同一上游版本，按 Docker 目标架构下载官方二进制；两个架构共用一个 Dockerfile 和 Linux 构建 patch。

该脚本会构建两个镜像（以下为 arm64 示例，amd64 使用相应 tag）：

- `addp/model3d-converter:linux-arm64`：基于 `fanvanzh/3dtiles` 源码构建 对应目标架构的 `_3dtile`，并应用 ADDP 的 Linux patch，同时绑定同架构 `IfcConvert`。
- `addp/model3d-workflow:linux-arm64`：内置 Python `model3d_workflow` runtime、Linux arm64 `_3dtile`、`IfcConvert`、`assimp` 和 Blender/MAX 导入器。

Linux 静态 OSG 显式注册 zlib compressor，以读取超图等工具导出的压缩 OSGB。Converter 镜像构建在复制生产产物前，用自建三角形生成 zlib 压缩 OSGB、调用同一 `_3dtile` 转换，再校验 GLB 2.0 及顶点/三角面数量；该构建门禁不依赖 SuperMap SDK 或许可。

默认上游引用固定为 `fanvanzh/3dtiles@acbcf603f33fdfe3c34b704a8b019c4fd32a8376`。如需临时验证其他上游版本，可通过 `THREE_DTILES_REF=<commit-or-branch>` 覆盖，但生产镜像应使用固定 commit。

vcpkg baseline 保持上游固定版本。tinygltf `2.9.7` 通过唯一的 overlay port 固定到发布提交 `488a70a3df62a4df1a736e9e56fb8836580c4888`，下载提交归档并执行 SHA-512 校验，替换 baseline 中已无法通过校验的 tag 归档；不关闭完整性检查，也不升级其他原生依赖。构建入口与 overlay 安装契约由 `make test-dev-lifecycle` 验证，完整 Linux amd64 编译及实际转换由 Hosted Manager T4 验收。

运行时镜像内固定绑定：

```text
MODEL3D_CONVERTER_BIN=/opt/addp/model3d-workflow/bin/_3dtile
MODEL3D_MESH_CONVERTER_BIN=/usr/bin/assimp
MODEL3D_IFC_CONVERTER_BIN=/opt/addp/model3d-workflow/bin/IfcConvert
MODEL3D_GAUSSIAN_SPLAT_NODE_BIN=/usr/bin/node
MODEL3D_BLENDER_BIN=/usr/bin/blender
MODEL3D_MAX_ADDON_PATH=/opt/addp/model3d-workflow/max-importer/source
GDAL_DATA=/opt/addp/model3d-workflow/bin/gdal
PROJ_DATA=/opt/addp/model3d-workflow/bin/proj
OSG_LIBRARY_PATH=/opt/addp/model3d-workflow/bin/osgPlugins-3.6.5
```

本机验证：

```bash
docker run --rm --platform linux/arm64 \
  -p 8101:8101 \
  -e SYSTEM_URL="${SYSTEM_URL:-http://host.docker.internal:8180}" \
  -e MODEL3D_WORKFLOW_SERVICE_CLIENT_SECRET="${MODEL3D_WORKFLOW_SERVICE_CLIENT_SECRET}" \
  -v /mnt/addp-nfs:/mnt/addp-nfs \
  addp/model3d-workflow:linux-arm64
```

如果源数据位于其他目录，需要把宿主机路径以同一路径挂载进容器，保证 Manager 传给 `model3d_workflow` 的 NFS / localfs 路径在容器内也能访问。`docker-compose.yml` 提供：

```text
MODEL3D_DATA_HOST_PATH=./business/nfs/data
MODEL3D_DATA_CONTAINER_PATH=/Users/pampa/code/addp/business/nfs/data
```

三维模型 GLB 和高斯泼溅 KSplat 快显 artifact 由 `model3d_workflow` 直接上传到 Manager infra MinIO。MinIO endpoint 统一来自 ADDP infra MinIO 配置，不为 `model3d_workflow` 另设专用 endpoint。Docker Compose 部署时，Manager 与 runtime 同在 Compose 网络内，统一使用 `minio:9000`；macOS 本机开发使用宿主机 Python Runtime 与私有原生转换器，Manager 与 Runtime 统一访问实际 Infra MinIO 宿主机端口。

OSGB Scene 的对象存储 source 由运行时 staging：先递归下载到本地临时 workspace，再调用 `_3dtile`。对象存储 target 由运行时发布：转换器先输出到本地临时 workspace，再递归上传到 MinIO/S3，并最后上传 `tileset.json`。

## ADDP Docker 部署集成

完整平台镜像构建时，使用 ADDP 构建脚本统一构建和推送：

```bash
bash scripts/build/build-images.sh --services model3d-workflow-engine --force
```

该入口会调用本目录的 `scripts/build-linux-images.sh`，并生成：

- `${REGISTRY}/addp-model3d-converter:${IMAGE_TAG}`
- `${REGISTRY}/addp-model3d-workflow-engine:${IMAGE_TAG}`

随后 `scripts/local/start.sh` 和 `scripts/prod/start.sh` 会通过 `docker-compose.yml` 启动 `model3d-workflow-engine`，端口为 `8101`，服务启动后自动向 System 注册 `model3d_workflow` 引擎。Manager 只通过 common engine 的 `WorkflowRuntimeProvider` 调用 `osgb_to_glb`、`gltf_to_glb`、`fbx_to_glb`、`obj_to_glb`、`stl_to_glb`、`dae_to_glb`、`3ds_to_glb`、`skp_to_glb`、`ifc_to_glb`、`osgb_scene_to_3dtiles` 和 `gaussian_splat_to_ksplat`，不直接调用 `_3dtile`、`assimp`、`IfcConvert` 或其他转换器。

## 测试

```bash
python3 -m venv engines/model3d-workflow/.venv
engines/model3d-workflow/.venv/bin/python -m pip install -r engines/model3d-workflow/requirements-dev.txt -e ./common-python
make test-model3d-workflow
```

DAE / 3DS 的源引用必须是模型目录内大小写一致的相对路径，缺失贴图、动画及复杂贴图明确拒绝。全部单体 GLB 快显转换产物发布前统一校验容器结构、自包含资源、PNG/JPEG 实际解码，以及网格索引编码、对齐、范围和绘制数量；稀疏索引按替换后的有效值校验。转换器退出码为零不代表校验通过，校验失败时保留已有快显文件。该确定性门禁通过根 `make test-model3d-workflow`、`make test-module MODULE=engines` 和 Platform CI 注册；真实转换器及整个平台快显验收属于另行执行的集成验证。

DAE / 3DS 的基础颜色贴图还必须关联到网格实际使用的材质和有效 `TEXCOORD_n`：仅嵌入图片、未绑定材质、UV 缺失、顶点数不匹配或越界，都不能发布为成功产物。发布失败保留之前有效的目标文件。

POSITION 与基础颜色 UV 的存储偏移、分量对齐和步长同时按 glTF 2.0 校验；显式步长必须按 4 字节对齐并位于 4–252 字节内，无符号整数 UV 必须提供步长。合法紧密排列与交错排列均可使用，非法存储布局在发布前拒绝。

DAE 源贴图引用的百分号转义必须完整且合法，仅解码一次，再检查目录范围和文件名大小写；非法转义在调用转换器前拒绝。3DS 使用字面文件名，不解码百分号。源摘要中的声明引用保持原样。

DAE 的 XML 解析器直接拒绝 DTD 和实体声明，UTF-8 / UTF-16 下执行相同边界；普通注释中的声明字面文本不会误判为 DTD。相应测试自动进入同一 Model3D 门禁。

所有单体 GLB 快显发布前还校验场景与节点：默认场景（未声明时为第一个场景）必须能引用到网格；节点索引、父子层级及变换必须有效。循环、多父节点、空白场景、非有限变换和非法四元数不能发布为成功。合法单位缩放、上轴转换、平移及负缩放保持原样，沿用转换器与统一 GLB 发布路线。

SKP 真实解析与转换回归自动进入同一 `make test-model3d-workflow`：自建 SKP 包含共享组件和内嵌贴图，验证米制尺寸、Y 上轴、实例共享、实际贴图像素、源文件不变，以及隐藏内容／损坏输入拒绝和旧产物保留。根 `make build-images IMAGE_BUILD_ARGS="--services model3d-workflow-engine --verify --jobs 1 --tag <独立验证标签>"` 还在生产 Runtime 镜像内生成并转换 SKP，检查 GLB 与米制尺寸。2026-10-07 的 Linux ARM64 构建及转换通过；amd64 由现有镜像构建门禁验证，本地 Business NFS 的 Manager 页面端到端验收已通过：`3d/skp/static-textured-cubes.skp`（Meta item 20245，9421 字节）经快显任务 8 发布 4312 字节的 GLB 2.0，两个共享组件实例及内嵌彩色贴图显示、旋转正常，产物尺寸符合米制约定。S3 源与 Hosted T4 尚待验证。
