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

本机原生转换、产品镜像构建和 Hosted Manager T4 分别验证，历史源码、产物身份、覆盖范围及限制统一见[测试与验收规范的证据索引](../../docs/spec/testing/验收证据索引.md#addp-验收证据索引)。本机页面成功不替代产品镜像或 Hosted 结果，后续成功也不自动证明早期未归因故障已修复。

统一 OSGB 转换器的 Rust → C++ 输入路径使用 NUL 终止的 `CString`，转换失败返回非零。Linux 产品与 macOS 原生工具包消费同一补丁，不增加备用转换路线；长暂存路径、无效 OSGB 的失败退出和旧产物保留分别验证。

本运行时的稳定集成面是 ADDP operator 契约，不是转换器内部 SDK。本地必需转换工具缺失时，`/health` 返回 HTTP 503 + unhealthy，并按工具名称列出结构化依赖事实，实例不作为可执行候选。健康不代替单次转换验收；执行失败或输出缺失仍由算子报错，不生成伪结果。

SKP 使用固定的 MIT 许可 `openskp[textures]==1.3.0`，在当前 Python Runtime 的独立子进程中执行 `skp_converter.py`，不调用 SuperMap、SketchUp 桌面 SDK 或 Assimp。输入为单文件，贴图须已内嵌；导出的毫米坐标通过统一场景根缩放为米，保持 Y 上轴和实例变换。只发布经过统一校验的 GLB，临时 JSON 与缩略图不会发布。首期不声明动态组件、动画、标注、孤立线段或全部 SKP 版本支持；固定版本的导出器未应用可见性，含隐藏组件、隐藏面或关闭图层的模型明确拒绝，避免发布错误场景。解析失败、空场景、缺失贴图和非法 GLB 保留旧产物。项目与发行版依据见平台内置格式规范。


MAX 的 Manager Hosted T4 扩展复用 `manager-internal-artifact-lineage`，从专用 Business MinIO 读取未修改的 CC-BY-SA-3.0 马模型及 ADDP 自建 4 × 4 RGB `(220,30,60)` 贴图。页面验证默认米、手选毫米和显式 bitmap 映射；正式生成声明 `source_unit=mm`，核对转换审计、五个静态网格、十二个 primitive、444 个顶点、米制场景尺寸和实际内嵌贴图像素，不固定跨 Blender 版本的 PNG 编码哈希。Meta 仅识别 MAX 格式身份；六种模型与点云、PPTX、栅格共同核对九项输入／输出血缘、缓存加载和零残留清理。确定性入口仍为 `make test-manager-online-runner` 与 `make test-online-runner`；真实验收仍为现有 Hosted suite，实际运行通过前不计为 T4 通过。

MAX 的工具版本与校验值集中在 `blender-assets.json`。macOS arm64 使用私有工具包内的 Blender 4.5.3；Linux 产品镜像使用 Debian Trixie 的 Blender 4.3.2 和显式安装的 NumPy。两者均使用固定提交的 `io_scene_max` 1.9.2。安装时校验下载归档，保留上游源码及许可：仓库 LICENSE、源码 GPL 许可声明和插件 manifest 的 GPL-3.0-or-later 声明均原样保留。开发工具包逐文件校验完整性，转换不向包内写入 Python 缓存。

MAX 首期覆盖静态网格、实例变换、基础颜色和漫反射图片，不声明动画、骨架或第三方渲染器材质保真。单个 MAX 文件的解析预算为 64 MiB。固定导入器目前不能可靠识别原始系统单位：用户可选择 `options.source_unit`（`mm/cm/m/km/in/ft/mi`）；未选则默认米 `m`。Manager 的 MAX 生成入口提供同一选项，缺省值无需用户确认。结果 `conversion` 记录实际单位、`unit_source=user|default` 与米制换算系数，不把默认米说成自动识别。

外部贴图必须通过 `options.texture_files` 明确映射，key 是 MAX 内保存的 bitmap 引用，value 是输入目录内的相对文件路径。不会读取原电脑绝对路径或猜测同名图片。Manager 的数据探查和任务创建入口提供相同的单位及外部贴图声明组件；声明传入既有快显动作并保存到任务 options，Develop/direct 算子沿用同一映射。缺失贴图、解析错误及不支持的贴图通道会使转换失败，保留旧产物。内置回归样例来自 `tests/fixtures/max/ATTRIBUTION.txt` 所列的 CC-BY-SA-3.0 模型，纹理是 ADDP 自建测试图片。

MAX 的确定性测试由 `make test-model3d-workflow` 自动发现，开发生命周期由 `make test-dev-lifecycle` 验证。正式 Linux 镜像构建入口为：

```bash
make build-images IMAGE_BUILD_ARGS='--services model3d-workflow-engine --verify --jobs 1'
```

现有 Product binary build CI 会按受影响路径选择 Model3D 镜像，构建后用真实 Blender 检查静态 GLB、贴图、默认米与手选毫米的 1000 倍比例、源文件不变及失败时旧产物保留。此检查不代替真实业务源、Manager 页面与 Monitor 的 T4 验收。

产品构建的最终 Runtime 转换检查还覆盖 FBX／OBJ 静态网格：FBX 使用 Assimp `v5.4.3`（提交 `c35200e38ea8f058812b83de2ef32c6093b0ece2`）的 [box.fbx](https://github.com/assimp/assimp/blob/c35200e38ea8f058812b83de2ef32c6093b0ece2/test/models/FBX/box.fbx)，保留在 `tests/fixtures/mesh/`，源 SHA-256 为 `ad2d79fe89d4d646bc7930dc952eb28e69976a321b387bf7851ecd3f37e667f8`；原始许可完整保留在同目录 `LICENSE-assimp.txt`。上游 [独立断言](https://github.com/assimp/assimp/blob/c35200e38ea8f058812b83de2ef32c6093b0ece2/test/unit/utFBXImporterExporter.cpp#L67) 为一个网格、36 个顶点和 12 个三角面。OBJ 使用 ADDP 自建三角形及漫反射 PNG，核对源坐标、三角面、UV 贴图引用、实际内嵌像素与不透明度。两者经正式 access plan 和 operator 发布，并验证源字节不变、无效输入失败且保留旧产物；不扩大为动画、全部 FBX 版本、复杂材质或 Manager Hosted 页面已验收。

FBX／OBJ 构建检查比较源角点坐标、三角面、UV 与实际贴图像素，允许语义等价的顶点合并，不把上游导入器测试中的顶点数直接作为 GLB 存储要求。Linux ARM64 构建、Linux x86_64 产品链路与 Manager 页面验收分别认定。

## 启动

```bash
./scripts/dev/restart.sh -model3d-workflow
```

## Linux amd64 / arm64 容器

本节仅用于 Linux 产品镜像构建和 Hosted 产品验收；本机开发使用前述私有原生工具包。

Linux mesh 转换器固定使用 Assimp 5.4.3 源码归档（SHA-256 `66dfbaee288f2bc43172440a55d0235dfc7bf885dda6435c038e8000e79582cb`），在 Converter 构建阶段静态编译到同一工具包的 `bin/assimp`，不再安装发行版 `assimp-utils`。唯一补丁检查 Unix `stat()` 的返回值：失败时立即返回不存在，避免读取未初始化的 `statbuf`，把当前工作目录中不存在的贴图误判为存在，继而放弃模型目录中的正确贴图。该缺陷已在原 Linux 产品镜像复现为 120 次相同 3DS 转换中的 9 次外部图片 URI 残留；保持自包含 GLB 校验，不增加重试、别名或补打包路线。

同一产品构建运行文件存在性回归，模拟 `stat()` 失败但输出结构残留普通文件标志，并核对真实文件、目录及缺失路径；最终 Runtime 使用真实 Assimp 反复转换带相对 PNG 引用的 3DS，核对几何、UV、内嵌像素、源文件不变和失败时旧产物保留。原有 FBX／OBJ／OSGB／SKP／MAX 检查继续执行。Linux amd64 的 Manager 页面与对象存储完整链路仍由原 Hosted suite 验收，构建检查不能替代 T4。

2026-10-15 已构建补丁后的 Linux ARM64 Converter 镜像 `sha256:efc24640b846c3be94237284a330f23e9c2ceb3859bd13792c4c3df904083fa1`，原生文件存在性回归通过。通过根 `make test-model3d-workflow` 增补临时探测，官方原函数在失败 `stat()` 的确定性输入上误判存在，仓库补丁正确拒绝；修复版转换器配合当前生产 operator、既有 Runtime 依赖镜像连续 120 次转换均通过，每次核对三顶点／一三角面、UV、自包含 PNG 及全部 2 × 2 RGB `(255,64,32)` 像素，缺图和损坏图片均拒绝且保留旧产物，源哈希不变、临时转换目录为空。合计 280 项通过，其中 278 项为现有测试、两项为临时探测。该组合验证不等同于完整新 Runtime 镜像：标准完整产品构建仍被 Debian HTTPS 软件源连接失败阻断，首次直连及临时本机代理两次失败均保留；未关闭证书验证或更换软件源。完整镜像及 x86_64 复验由既有 Platform CI 产品构建与 Manager Hosted 入口继续完成。

```bash
make build-images IMAGE_BUILD_ARGS='--services model3d-workflow-engine --verify --jobs 1'
```

统一构建入口按宿主机 CPU 选择 `linux/amd64` 或 `linux/arm64`，也可通过 `MODEL3D_DOCKER_PLATFORM` 显式选择这两种架构；其他平台直接拒绝。GitHub Hosted Ubuntu x86_64 使用 amd64，不通过 QEMU 运行 arm64 转换器。IfcConvert 固定同一上游版本，按 Docker 目标架构下载官方二进制；两个架构共用一个 Dockerfile 和 Linux 构建 patch。

该脚本会构建两个镜像（以下为 arm64 示例，amd64 使用相应 tag）：

- `addp/model3d-converter:linux-arm64`：基于 `fanvanzh/3dtiles` 源码构建 对应目标架构的 `_3dtile`，并应用 ADDP 的 Linux patch，同时绑定同架构 `IfcConvert`。
- `addp/model3d-workflow:linux-arm64`：内置 Python `model3d_workflow` runtime、Linux arm64 `_3dtile`、`IfcConvert`、`assimp` 和 Blender/MAX 导入器。

OSGB 基础颜色贴图统一由 OSG 读取 RGB／RGBA／BGRA 与 DXT1／DXT3／DXT5 像素，含透明或半透明像素时内嵌 RGBA PNG 并给关联材质设置 `alphaMode=BLEND`；全不透明图片继续输出 RGB JPEG。源行对齐和 PNG alpha 必须保留，不能仅改材质模式而丢弃图片 alpha；未知／损坏图片不能替换为白色占位图。该规则同时作用于共享的 OSGB 单体与场景转换代码；不声明源 AlphaFunc 阈值及全部材质保真；透明模式遵循 [glTF 2.0 Alpha Coverage](https://registry.khronos.org/glTF/specs/2.0/glTF-2.0.html#alpha-coverage)。现有 Linux 产品构建和 macOS 原生 prepare／verify 均运行同一 `osgb_compression_smoke.cpp`，增加压缩及原始透明像素、MIME、材质和行对齐断言；根 `make test-model3d-workflow`、`make test-module MODULE=engines`、开发生命周期及已有 Platform CI／产品构建登记覆盖变更，无新服务、API、依赖或公共测试入口。

SDK 压缩图片的 `dataType=0` 由实际压缩像素格式决定 DXT 解码，不套用原始 RGB 的 component type 校验。压缩及原始透明像素、MIME、材质和行对齐由同一构建自检验证。

Linux 静态 OSG 显式注册 zlib compressor，以读取超图等工具导出的压缩 OSGB。Converter 镜像构建在复制生产产物前，用自建三角形生成 zlib 压缩 OSGB、调用同一 `_3dtile` 转换，再校验 GLB 2.0 及顶点/三角面数量；该构建门禁不依赖 SuperMap SDK 或许可。

默认上游引用固定为 `fanvanzh/3dtiles@acbcf603f33fdfe3c34b704a8b019c4fd32a8376`。如需临时验证其他上游版本，可通过 `THREE_DTILES_REF=<commit-or-branch>` 覆盖，但生产镜像应使用固定 commit。

vcpkg baseline 保持上游固定版本。tinygltf `2.9.7` 通过唯一的 overlay port 固定到发布提交 `488a70a3df62a4df1a736e9e56fb8836580c4888`，下载提交归档并执行 SHA-512 校验，替换 baseline 中已无法通过校验的 tag 归档；不关闭完整性检查，也不升级其他原生依赖。构建入口与 overlay 安装契约由 `make test-dev-lifecycle` 验证，完整 Linux amd64 编译及实际转换由 Hosted Manager T4 验收。

运行时镜像内固定绑定：

```text
MODEL3D_CONVERTER_BIN=/opt/addp/model3d-workflow/bin/_3dtile
MODEL3D_MESH_CONVERTER_BIN=/opt/addp/model3d-workflow/bin/assimp
MODEL3D_IFC_CONVERTER_BIN=/opt/addp/model3d-workflow/bin/IfcConvert
MODEL3D_GAUSSIAN_SPLAT_NODE_BIN=/usr/bin/node
MODEL3D_BLENDER_BIN=/usr/bin/blender
MODEL3D_MAX_ADDON_PATH=/opt/addp/model3d-workflow/max-importer/source
GDAL_DATA=/opt/addp/model3d-workflow/bin/gdal
PROJ_DATA=/opt/addp/model3d-workflow/bin/proj
OSG_LIBRARY_PATH=/opt/addp/model3d-workflow/bin/osgPlugins-3.6.5
```

这些工具与资源路径由 Runtime 镜像唯一定义，正式 Compose 直接继承，不重复覆盖。容器健康检查只认 HTTP 200 + `status=healthy`；任一必需工具缺失时返回 HTTP 503 + unhealthy。Runtime 监听后仍异步登记身份，System 将未就绪实例标为 offline，工具恢复后由周期探测恢复 online；注册成功不能作为页面验收中的可执行证据。

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

随后 `scripts/local/start.sh` 和 `scripts/prod/start.sh` 会通过 `docker-compose.yml` 启动 `model3d-workflow-engine`，端口为 `8101`，服务启动后自动向 System 注册 `model3d_workflow` 引擎。Manager 只通过 common engine 的 `WorkflowRuntimeProvider` 调用 `osgb_to_glb`、`gltf_to_glb`、`fbx_to_glb`、`obj_to_glb`、`stl_to_glb`、`dae_to_glb`、`3ds_to_glb`、`skp_to_glb`、`max_to_glb`、`ifc_to_glb`、`osgb_scene_to_3dtiles` 和 `gaussian_splat_to_ksplat`，不直接调用 `_3dtile`、`assimp`、`IfcConvert` 或其他转换器。

## 测试

```bash
python3 -m venv engines/model3d-workflow/.venv
engines/model3d-workflow/.venv/bin/python -m pip install -r engines/model3d-workflow/requirements-dev.txt -e ./common-python
make test-model3d-workflow
```

标准目标通过 `MODEL3D_WORKFLOW_PYTHON` 指定测试解释器。已准备本页原生 Python 3.12 环境时，可显式传入 `MODEL3D_WORKFLOW_PYTHON=engines/model3d-workflow/venv/bin/python`；同一变量也可传给 `make test-module MODULE=engines`。

DAE / 3DS 的源引用必须是模型目录内大小写一致的相对路径，缺失贴图、动画及复杂贴图明确拒绝。全部单体 GLB 快显转换产物发布前统一校验容器结构、自包含资源、PNG/JPEG 实际解码，以及网格索引编码、对齐、范围和绘制数量；稀疏索引按替换后的有效值校验。转换器退出码为零不代表校验通过，校验失败时保留已有快显文件。 图片 URI 校验失败的诊断仅记录图片索引、URI 类型／长度及是否存在 bufferView，不输出 URI 内容；mesh 输出校验失败时在 Runtime 日志保留有界的转换器 stdout／stderr。Manager Hosted 退出前收集属于该 suite 的 Model3D Runtime 日志，再销毁容器；日志收集失败仍执行其余清理并使验收失败。受影响门禁为现有 `make test-model3d-workflow`、`make test-manager-online-runner` 及产品构建／Manager Hosted 编排，现有路径登记覆盖这些改动，无新增依赖或入口。该确定性门禁通过根 `make test-model3d-workflow`、`make test-module MODULE=engines` 和 Platform CI 注册；真实转换器及整个平台快显验收属于另行执行的集成验证。

DAE / 3DS 的基础颜色贴图还必须关联到网格实际使用的材质和有效 `TEXCOORD_n`：仅嵌入图片、未绑定材质、UV 缺失、顶点数不匹配或越界，都不能发布为成功产物。发布失败保留之前有效的目标文件。

POSITION 与基础颜色 UV 的存储偏移、分量对齐和步长同时按 glTF 2.0 校验；显式步长必须按 4 字节对齐并位于 4–252 字节内，无符号整数 UV 必须提供步长。合法紧密排列与交错排列均可使用，非法存储布局在发布前拒绝。

DAE 源贴图引用的百分号转义必须完整且合法，仅解码一次，再检查目录范围和文件名大小写；非法转义在调用转换器前拒绝。3DS 使用字面文件名，不解码百分号。源摘要中的声明引用保持原样。

DAE 的 XML 解析器直接拒绝 DTD 和实体声明，UTF-8 / UTF-16 下执行相同边界；普通注释中的声明字面文本不会误判为 DTD。相应测试自动进入同一 Model3D 门禁。

所有单体 GLB 快显发布前还校验场景与节点：默认场景（未声明时为第一个场景）必须能引用到网格；节点索引、父子层级及变换必须有效。循环、多父节点、空白场景、非有限变换和非法四元数不能发布为成功。合法单位缩放、上轴转换、平移及负缩放保持原样，沿用转换器与统一 GLB 发布路线。

SKP 真实解析与转换回归自动进入同一 `make test-model3d-workflow`：自建 SKP 包含共享组件和内嵌贴图，验证米制尺寸、Y 上轴、实例共享、实际贴图像素、源文件不变，以及隐藏内容／损坏输入拒绝和旧产物保留。根 `make build-images IMAGE_BUILD_ARGS="--services model3d-workflow-engine --verify --jobs 1 --tag <独立验证标签>"` 还在生产 Runtime 镜像内生成并转换 SKP，检查 GLB 与米制尺寸。原生构建、NFS 本机页面与 S3／Hosted 的结果分别见[测试与验收规范的证据索引](../../docs/spec/testing/验收证据索引.md#addp-验收证据索引)，不以某一环境通过替代其他环境的验收。