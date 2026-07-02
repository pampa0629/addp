# Model3D Workflow

`model3d_workflow` 是 ADDP 三维模型转换专用工作流运行时，遵循 `addp.workflow/v1` HTTP 接口。

第一版只提供 direct operator：

- `osgb_to_glb`：单个 `.osgb` 文件转换为 GLB 快显 artifact。
- `gltf_to_glb`：`.gltf` manifest 声明的多资源模型打包为 GLB 快显 artifact。
- `fbx_to_glb`：FBX 单体网格模型转换为 GLB 快显 artifact。
- `obj_to_glb`：OBJ 单体网格模型转换为 GLB 快显 artifact。
- `stl_to_glb`：STL 单体网格模型转换为 GLB 快显 artifact。
- `ifc_to_glb`：IFC BIM 模型转换为 GLB 快显 artifact。
- `osgb_scene_to_3dtiles`：一套 OSGB 倾斜摄影数据集转换为 3D Tiles，支持 NFS/localfs/MinIO/S3 source 输出到 NFS/localfs/MinIO/S3 target。
- `gaussian_splat_to_ksplat`：高斯泼溅源生成 Manager 受管 KSplat 快显 artifact；只接受 `ply` / `splat` 源并转换为 `.ksplat` 文件。源格式已经是 `ksplat` 时由 Manager 基础预览直接读取，不创建 KSplat 快显任务，也不登记受管快显结果。

运行时通过随引擎绑定的专业转换器执行实际转换。OSGB / OSGB Scene 默认使用 `engines/model3d-workflow/bin/_3dtile`，glTF / FBX / OBJ / STL 这类 mesh 模型转 GLB 默认使用 `engines/model3d-workflow/bin/assimp`。IFC 已由 common format 识别为 `data_type=model_3d + format=ifc + layout=single`，BIM 语义不进入 mesh converter，`ifc_to_glb` 默认使用 `engines/model3d-workflow/bin/IfcConvert`。glTF / FBX / OBJ / STL 生成的 GLB artifact 必须自包含；其中 glTF / FBX / OBJ 必须嵌入纹理，避免前端从原始源目录相对加载贴图。IFC 生成 GLB 时默认传入 `--center-model`，避免大坐标直接影响前端初始观察。`gaussian_splat_to_ksplat` 使用运行时内置 Node 脚本 `create_ksplat.mjs` 和 `@mkkellogg/gaussian-splats-3d` 生成 `.ksplat`，不调用 mesh / OSGB / IFC 转换器；生成时优先使用 `options.scene_center`，否则由 `options.bounds_3d` / `options.sampled_bounds_3d` 推导中心，并默认使用 `section_size=262144`、`block_size=5.0`、`bucket_size=256` 组织 KSplat section，让渐进加载尽量先显示模型中心区域。`.ksplat` 源已经是前端目标渲染格式，不进入该 operator；其视角状态由 `manager.preview_state` 保存。可用环境变量覆盖到同一运行时部署中的实际可执行文件路径，但不能只写 `_3dtile`、`assimp` 或 `IfcConvert` 这类依赖系统 `PATH` 的命令名：

```bash
export MODEL3D_CONVERTER_BIN=/path/to/_3dtile
export MODEL3D_MESH_CONVERTER_BIN=/path/to/assimp
export MODEL3D_IFC_CONVERTER_BIN=/path/to/IfcConvert
export MODEL3D_GAUSSIAN_SPLAT_NODE_BIN=/path/to/node
```

本运行时的稳定集成面是 ADDP operator 契约，不是转换器内部 SDK。转换器缺失、执行失败或输出缺失时，`/health` 会标记 `conversion_ready=false`，引擎连接测试和 operator 发现会失败，不生成伪结果。

## 启动

```bash
cd engines/model3d-workflow
pip install -r requirements.txt
PORT=8101 python api_server.py
```

## Apple Silicon / Linux arm64 容器

Apple Silicon 本机优先使用 Docker Desktop 的 Linux arm64 后端运行 `model3d_workflow`，不要在 macOS host 上原生构建或执行 `_3dtile`。

```bash
cd engines/model3d-workflow
./scripts/build-linux-arm64-images.sh
```

该脚本会构建两个镜像：

- `addp/model3d-converter:linux-arm64`：基于 `fanvanzh/3dtiles` 源码构建 Linux arm64 `_3dtile`，并应用 ADDP 的 arm64-linux patch，同时绑定 Linux arm64 `IfcConvert`。
- `addp/model3d-workflow:linux-arm64`：内置 Python `model3d_workflow` runtime、Linux arm64 `_3dtile`、`IfcConvert` 和 `assimp`。

默认上游引用固定为 `fanvanzh/3dtiles@acbcf603f33fdfe3c34b704a8b019c4fd32a8376`。如需临时验证其他上游版本，可通过 `THREE_DTILES_REF=<commit-or-branch>` 覆盖，但生产镜像应使用固定 commit。

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
  -e INTERNAL_API_KEY="${INTERNAL_API_KEY:-}" \
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

该入口会调用本目录的 `scripts/build-linux-arm64-images.sh`，并生成：

- `${REGISTRY}/addp-model3d-converter:${IMAGE_TAG}`
- `${REGISTRY}/addp-model3d-workflow-engine:${IMAGE_TAG}`

随后 `scripts/local/start.sh` 和 `scripts/prod/start.sh` 会通过 `docker-compose.yml` 启动 `model3d-workflow-engine`，端口为 `8101`，服务启动后自动向 System 注册 `model3d_workflow` 引擎。Manager 只通过 common engine 的 `WorkflowRuntimeProvider` 调用 `osgb_to_glb`、`gltf_to_glb`、`fbx_to_glb`、`obj_to_glb`、`stl_to_glb`、`ifc_to_glb`、`osgb_scene_to_3dtiles` 和 `gaussian_splat_to_ksplat`，不直接调用 `_3dtile`、`assimp`、`IfcConvert` 或其他转换器。

## 测试

```bash
python -m pip install -r engines/model3d-workflow/requirements-dev.txt
PYTHONPATH=engines/model3d-workflow:engines/docs pytest engines/model3d-workflow
```
