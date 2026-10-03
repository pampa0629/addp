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
- `ifc_to_glb`：IFC BIM 模型转换为持久化 GLB。
- `osgb_scene_to_3dtiles`：一套 OSGB 倾斜摄影数据集转换为 3D Tiles，支持 NFS/localfs/MinIO/S3 source 输出到 NFS/localfs/MinIO/S3 target。
- `gaussian_splat_to_ksplat`：`gaussian_splat` 的 `ply` / `splat` 源转换为持久化 `.ksplat` 文件。源格式已经是 `ksplat` 时直接读取，不进入转换算子。

Runtime Operator Spec 统一消费 `addp.workflow.access-plan/v1`。Manager direct 调用选择 infra 目标并登记私有快显 artifact；Develop workflow 调用保存公开 ResourceLocator 参数、选择业务目标，并在成功后触发 Meta scan。Runtime 不解析 ADDP locator，也不决定产物归属。

运行时通过随引擎绑定的专业转换器执行实际转换。OSGB / OSGB Scene 默认使用 `engines/model3d-workflow/scripts/converters/_3dtile`，glTF / FBX / OBJ / STL / DAE / 3DS 这类 mesh 模型转 GLB 默认使用 `engines/model3d-workflow/scripts/converters/assimp`。IFC 已由 common format 识别为 `data_type=model_3d + format=ifc + layout=single`，BIM 语义不进入 mesh converter，`ifc_to_glb` 默认使用 `engines/model3d-workflow/scripts/converters/IfcConvert`。glTF / FBX / OBJ / STL / DAE / 3DS 生成的 GLB artifact 必须自包含；其中 glTF / FBX / OBJ 必须嵌入纹理，避免前端从原始源目录相对加载贴图。IFC 生成 GLB 时默认传入 `--center-model`，避免大坐标直接影响前端初始观察。`gaussian_splat_to_ksplat` 使用运行时内置 Node 脚本 `create_ksplat.mjs` 和 `@mkkellogg/gaussian-splats-3d` 生成 `.ksplat`，不调用 mesh / OSGB / IFC 转换器；生成时优先使用 `options.scene_center`，否则由 `options.bounds_3d` / `options.sampled_bounds_3d` 推导中心，并默认使用 `section_size=262144`、`block_size=5.0`、`bucket_size=256` 组织 KSplat section，让渐进加载尽量先显示模型中心区域。`.ksplat` 源已经是前端目标渲染格式，不进入该 operator；其视角状态由 `manager.preview_state` 保存。可用环境变量覆盖到同一运行时部署中的实际可执行文件路径，但不能只写 `_3dtile`、`assimp` 或 `IfcConvert` 这类依赖系统 `PATH` 的命令名：

```bash
export MODEL3D_CONVERTER_BIN=/path/to/_3dtile
export MODEL3D_MESH_CONVERTER_BIN=/path/to/assimp
export MODEL3D_IFC_CONVERTER_BIN=/path/to/IfcConvert
export MODEL3D_GAUSSIAN_SPLAT_NODE_BIN=/path/to/node
```

开发环境的三个 wrapper 及共享 Docker 调用脚本必须受 Git 版本管理，不依赖被忽略的本机 `bin/` 目录；容器内继续绑定 `/opt/addp/model3d-workflow/bin/` 的原生转换器。

本运行时的稳定集成面是 ADDP operator 契约，不是转换器内部 SDK。转换器缺失、执行失败或输出缺失时，`/health` 会标记 `conversion_ready=false`，引擎连接测试和 operator 发现会失败，不生成伪结果。

## 启动

```bash
cd engines/model3d-workflow
pip install -r requirements.txt
PORT=8101 python api_server.py
```

## Linux amd64 / arm64 容器

Apple Silicon 本机优先使用 Docker Desktop 的 Linux arm64 后端运行 `model3d_workflow`，不要在 macOS host 上原生构建或执行 `_3dtile`。

```bash
cd engines/model3d-workflow
./scripts/build-linux-images.sh
```

统一构建入口按宿主机 CPU 选择 `linux/amd64` 或 `linux/arm64`，也可通过 `MODEL3D_DOCKER_PLATFORM` 显式选择这两种架构；其他平台直接拒绝。GitHub Hosted Ubuntu x86_64 使用 amd64，不通过 QEMU 运行 arm64 转换器。IfcConvert 固定同一上游版本，按 Docker 目标架构下载官方二进制；两个架构共用一个 Dockerfile 和 Linux 构建 patch。

该脚本会构建两个镜像（以下为 arm64 示例，amd64 使用相应 tag）：

- `addp/model3d-converter:linux-arm64`：基于 `fanvanzh/3dtiles` 源码构建 对应目标架构的 `_3dtile`，并应用 ADDP 的 Linux patch，同时绑定同架构 `IfcConvert`。
- `addp/model3d-workflow:linux-arm64`：内置 Python `model3d_workflow` runtime、Linux arm64 `_3dtile`、`IfcConvert` 和 `assimp`。

默认上游引用固定为 `fanvanzh/3dtiles@acbcf603f33fdfe3c34b704a8b019c4fd32a8376`。如需临时验证其他上游版本，可通过 `THREE_DTILES_REF=<commit-or-branch>` 覆盖，但生产镜像应使用固定 commit。

vcpkg baseline 保持上游固定版本。tinygltf `2.9.7` 通过唯一的 overlay port 固定到发布提交 `488a70a3df62a4df1a736e9e56fb8836580c4888`，下载提交归档并执行 SHA-512 校验，替换 baseline 中已无法通过校验的 tag 归档；不关闭完整性检查，也不升级其他原生依赖。构建入口与 overlay 安装契约由 `make test-dev-lifecycle` 验证，完整 Linux amd64 编译及实际转换由 Hosted Manager T4 验收。

运行时镜像内固定绑定：

```text
MODEL3D_CONVERTER_BIN=/opt/addp/model3d-workflow/bin/_3dtile
MODEL3D_MESH_CONVERTER_BIN=/usr/bin/assimp
MODEL3D_IFC_CONVERTER_BIN=/opt/addp/model3d-workflow/bin/IfcConvert
MODEL3D_GAUSSIAN_SPLAT_NODE_BIN=/usr/bin/node
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

三维模型 GLB 和高斯泼溅 KSplat 快显 artifact 由 `model3d_workflow` 直接上传到 Manager infra MinIO。MinIO endpoint 统一来自 ADDP infra MinIO 配置，不为 `model3d_workflow` 另设专用 endpoint。Docker Compose 部署时，Manager 与 runtime 同在 Compose 网络内，统一使用 `minio:9000`；macOS 本机开发时，推荐使用宿主机 Python runtime 加 Docker `_3dtile` / `assimp` wrapper，Manager 与 runtime 统一访问 `localhost:19000`。

OSGB Scene 的对象存储 source 由运行时 staging：先递归下载到本地临时 workspace，再调用 `_3dtile`。对象存储 target 由运行时发布：转换器先输出到本地临时 workspace，再递归上传到 MinIO/S3，并最后上传 `tileset.json`。

## ADDP Docker 部署集成

完整平台镜像构建时，使用 ADDP 构建脚本统一构建和推送：

```bash
bash scripts/build/build-images.sh --services model3d-workflow-engine --force
```

该入口会调用本目录的 `scripts/build-linux-images.sh`，并生成：

- `${REGISTRY}/addp-model3d-converter:${IMAGE_TAG}`
- `${REGISTRY}/addp-model3d-workflow-engine:${IMAGE_TAG}`

随后 `scripts/local/start.sh` 和 `scripts/prod/start.sh` 会通过 `docker-compose.yml` 启动 `model3d-workflow-engine`，端口为 `8101`，服务启动后自动向 System 注册 `model3d_workflow` 引擎。Manager 只通过 common engine 的 `WorkflowRuntimeProvider` 调用 `osgb_to_glb`、`gltf_to_glb`、`fbx_to_glb`、`obj_to_glb`、`stl_to_glb`、`dae_to_glb`、`3ds_to_glb`、`ifc_to_glb`、`osgb_scene_to_3dtiles` 和 `gaussian_splat_to_ksplat`，不直接调用 `_3dtile`、`assimp`、`IfcConvert` 或其他转换器。

## 测试

```bash
python3 -m venv engines/model3d-workflow/.venv
engines/model3d-workflow/.venv/bin/python -m pip install -r engines/model3d-workflow/requirements-dev.txt -e ./common-python
make test-model3d-workflow
```

DAE / 3DS 的源引用必须是模型目录内大小写一致的相对路径，缺失贴图、动画及复杂贴图明确拒绝。全部单体 GLB 快显转换产物发布前统一校验容器结构、自包含资源、PNG/JPEG 实际解码，以及网格索引编码、对齐、范围和绘制数量；稀疏索引按替换后的有效值校验。转换器退出码为零不代表校验通过，校验失败时保留已有快显文件。该确定性门禁通过根 `make test-model3d-workflow`、`make test-module MODULE=engines` 和 Platform CI 注册；真实转换器及整个平台快显验收属于另行执行的集成验证。

DAE / 3DS 的基础颜色贴图还必须关联到网格实际使用的材质和有效 `TEXCOORD_n`：仅嵌入图片、未绑定材质、UV 缺失、顶点数不匹配或越界，都不能发布为成功产物。发布失败保留之前有效的目标文件。

DAE 的 XML 解析器直接拒绝 DTD 和实体声明，UTF-8 / UTF-16 下执行相同边界；普通注释中的声明字面文本不会误判为 DTD。相应测试自动进入同一 Model3D 门禁。
