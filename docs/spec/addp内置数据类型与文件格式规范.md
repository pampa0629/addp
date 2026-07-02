# ADDP 内置数据类型与文件格式规范

本文定义 ADDP 首批内置数据类型与文件格式的确定性落地规则。概念边界见 [ADDP 数据类型和格式体系图](../concepts/addp数据类型和格式体系图.md)，item 识别规则见 [ADDP 数据项探测器规范](addp数据项探测器规范.md)，attributes 写入规则见 [ADDP 元数据 attributes 规范](addp元数据attributes规范.md)。

本文只记录已经形成规范共识的格式。尚未定稿的格式、插件 manifest、whole scope explain / confidence 等问题，分别进入 `docs/plan/` 下的对应构想文档或后续事项文档，不再依赖 `docs/next/` 里的公共待规范页。

代码实现中，内置格式的静态身份声明由各格式包自己的 `Descriptor()` 维护，位置为 `common/format/plugins/<format>/`；统一加载入口为 `common/format/builtin/init.go`。本文是规范语义来源，代码中的 descriptor 应与本文保持一致；`common/format` 根包承担运行时注册、能力发现和冲突诊断，不再维护集中式内置 descriptor 清单。

## 编写模板

新增内置格式时，按以下结构补充：

| 小节 | 必须说明 |
|---|---|
| 识别与组织 | `layout`、`data_type`、`format`、主资源或 whole scope、ref 规则 |
| attributes 写入 | `storage`、`item`、`type_info`、`format_info`、`capabilities` 的事实归属 |
| 消费要求 | Manager、Transfer、Search 等模块应如何消费已入库 meta item |
| 格式约束 | 不得重复推断、不得写入错误分区、不得保留旧字段等约束 |

格式私有字段只进入 `format_info.<format>`；跨格式能力只进入 `capabilities.<capability>`；字段、行数、页数、宽高、子对象等类型信息只进入 `type_info.<data_type>`。

## 通用写入与消费约束

除非具体格式小节另有说明，内置格式统一遵守以下规则：

1. `attributes.item` 只写 data item 核心语义，例如 `layout`、`data_type`、`format`、`refs`、`scope_exclusive`、`claim_policy`。
2. `type_info.<data_type>` 只写对应数据类型的通用元信息，例如表字段、文档页数、媒体宽高、容器 children。
3. `format_info.<format>` 只写格式私有信息，例如分隔符、ref 摘要、footer、EXIF、容器版本。
4. `capabilities.<capability>` 只写横切能力，例如 spatial、statistics、extraction、partitioning。
5. Manager、Transfer、Search 等消费者必须基于已入库 `meta_item` 和标准 attributes 消费，不得按扩展名、MIME、`engine_type` 或前端预览类型二次决定核心语义。
6. 子对象默认只作为父容器的轻量 children；未形成子 item 规范前，不得自动展开成独立 `meta_item`。
7. 样本行、正文、大文件原始内容、缩略图、转换产物等内容数据不得直接塞入 attributes，应通过 content reader、对象流、外部索引或任务产物获取。

## 总览

| 格式 / 场景 | `layout` | `data_type` | `format` | 说明 |
|---|---|---|---|---|
| CSV | `single` | `table` | `csv` | 单资源表格文件 |
| TSV | `single` | `table` | `tsv` | 单资源表格文件 |
| Excel | `single` | `container` | `excel` | 外层工作簿先作为容器 item |
| records JSON / JSON Lines | `single` | `table` | `json` | 行列结构 JSON |
| GeoJSON / FeatureCollection | `single` | `table` | `geojson` | GeoJSON 格式；spatial 为解析后的横切能力 |
| 任意对象 JSON / 配置 JSON | `single` | `document` 或 `container` | `json` | 按平台消费方式判断 |
| Shapefile | `multi` | `table` | `shapefile` | 同目录或同 prefix 的同 basename refs |
| 单个 Parquet | `single` | `table` | `parquet` | 单文件表 |
| sibling Parquet 文件组 | `multi` | `table` | `parquet` | 仅在有明确 ref 或 manifest 规则时成立 |
| ORC / Avro 单文件 | `single` | `table` | `orc` / `avro` | 单文件表 |
| Iceberg 表目录 | `whole` | `table` | `iceberg` | 整体表目录，需 whole scope 规则 |
| SQLite | `single` | `container` | `sqlite` | 内部表先写入 `type_info.container.children` |
| GeoPackage | `single` | `container` | `geopackage` | 内部 layer 先写入 `type_info.container.children` |
| ZIP | `single` | `container` | `zip` | 压缩包 entry 先写入 `type_info.container.children` |
| 图片 | `single` 或 TIFF sidecar `multi` | `media` | `jpeg` / `png` / `gif` / `tiff` / `image` | GPS 或 GeoTIFF 空间语义进入 spatial |
| Raster mosaic | `whole` | `media` | `raster_mosaic` | 由 `mosaic.addp.json` manifest 声明的栅格镶嵌数据集 |
| 视频 | `single` | `media` | `mp4` / `mov` / `mkv` / `avi` / `webm` / `video` | 第一阶段以元信息和 range / stream 播放为主 |
| 音频 | `single` | `media` | `mp3` / `wav` / `flac` / `aac` / `ogg` / `audio` | 第一阶段以元信息和 range / stream 播放为主 |
| PDF | `single` | `document` | `pdf` | 文档元信息和提取状态分区写入 |
| DOCX | `single` | `document` | `docx` | 第一阶段以内置格式识别和 raw / range 预览为主 |
| PPTX | `single` | `document` | `pptx` | 第一阶段以内置格式识别和 raw / range 预览为主 |
| WPS | `single` | `document` | `wps` | 第一阶段以内置格式识别和 raw / range 预览为主 |
| GLB | `single` | `model_3d` | `glb` | 第一阶段三维模型代表格式；预览优先走 raw / range / storage stream + 前端 Three.js |
| glTF | `multi` | `model_3d` | `gltf` | 由 `.gltf` manifest 声明的多资源三维模型，buffers / images 作为 related refs |
| OBJ | `single` | `model_3d` | `obj` | Wavefront OBJ 单体网格模型；第一阶段支持识别和轻量摘要 |
| STL | `single` | `model_3d` | `stl` | STL 单体网格模型；支持识别、轻量摘要和 GLB 快显 |
| PLY | `single` | `model_3d` | `ply` | PLY 单文件三维模型 / 点集合；第一阶段支持 header 识别和轻量摘要 |
| FBX | `single` | `model_3d` | `fbx` | FBX 单体网格模型；快显通过 GLB artifact 实现 |
| IFC | `single` | `model_3d` | `ifc` | IFC BIM 模型；第一阶段支持识别和轻量 BIM 摘要 |
| 3D Tiles | `whole` | `model_3d` | `3dtiles` | 由 `tileset.json` manifest 声明的分块三维场景 |
| OSGB | `single` | `model_3d` | `osgb` | 单个 `.osgb` 三维模型文件；快显通过 GLB artifact 实现 |
| OSGB Scene | `whole` | `model_3d` | `osgb_scene` | 由 `metadata.xml` manifest 声明的一套 OSGB 倾斜摄影三维模型场景 |
| LAS | `single` | `point_cloud` | `las` | 第一阶段点云代表格式；deep scan 读取 header 和轻量摘要，预览走抽样点集 |

## GLB

### 识别与组织

| 维度 | 取值 |
|---|---|
| `layout` | `single` |
| `data_type` | `model_3d` |
| `format` | `glb` |
| 主资源 | `meta_item.full_name` 指向 `.glb` 文件资源 |

GLB 是单资源二进制 glTF 模型。第一阶段 GLB 作为 `model_3d` 的代表格式接入，用于走通 Meta scan 到 Manager 三维预览主链路。`.gltf + .bin + textures`、OBJ、OSGB、OSGB Scene、3D Tiles、IFC / Revit 等格式不借用 `format=glb`，应分别建立自己的 format descriptor 和规则。

### attributes 写入

| 分区 | 写入内容 |
|---|---|
| `item` | `layout`、`data_type=model_3d`、`format=glb` |
| `type_info.model_3d` | `model_kind=mesh_scene`、节点数、mesh 数、顶点数、三角面数、材质数、纹理数、动画数、三维包围盒、单位、up axis 等跨格式模型摘要 |
| `format_info.glb` | glTF 版本、generator、asset copyright、extensions used / required、是否包含 embedded resources、buffer / image / accessor 摘要等 GLB 私有事实 |
| `capabilities.spatial` | 仅在模型有明确地理定位、CRS 或可解析空间范围时写入 |

### 消费要求

Manager 预览应消费已入库 `data_type=model_3d + format=glb` 和 storage / contentio 内容通道，优先通过 raw / range / storage stream 将 GLB 交给前端三维渲染器。预览器名称、前端材质、截图、转换产物、压缩结果不得写入 attributes。

### 格式约束

- 不得把 GLB 归为 `media` 或 `container`。
- 不得把 glTF scene graph 当成 ADDP `graph` data type。
- 不得把模型原始 JSON、binary chunk、纹理内容、前端渲染协议或缩略图写入 `type_info.model_3d`。
- 后续 `gltf` multi 规则应通过 `layout=multi` 和 related refs 表达，不应和 GLB 共享同一个 primary ref 规则。

## glTF

### 识别与组织

| 维度 | 取值 |
|---|---|
| `layout` | `multi` |
| `data_type` | `model_3d` |
| `format` | `gltf` |
| 主资源 | `.gltf` manifest，`meta_item.full_name` 来源于该 manifest 文件 |
| related refs | manifest 自身、`buffers[].uri`、`images[].uri` 中可解析且存在的本地相对资源 |

glTF 是 JSON manifest 加外部二进制 buffer、纹理图片等资源组成的多资源三维模型。扫描实现必须解析 `.gltf` manifest 后才能建立 data item 边界：`buffers[].uri` 和 `images[].uri` 中的本地相对路径进入 `item.refs`，其中 `.gltf` manifest 为 `role=manifest` 且 `primary=true`，buffer 资源使用 `role=buffer` / `buffer_1` 等唯一 role，image 资源使用 `role=image` / `image_1` 等唯一 role。`data:` URI 和 `http://` / `https://` 等外部 URL 不形成本地 ref。

如果 `.gltf` manifest 引用的任一本地相对资源不在当前扫描可见候选集合内，扫描不得产出完整 glTF item。不得通过同 basename、同目录图片或 `.bin` 文件推断 glTF refs；资源边界只以 manifest 显式 URI 为准。被 manifest 引用并命中的本地资源必须进入 detector claims，避免 `.bin`、纹理图片等被重复落为独立 item。

### attributes 写入

| 分区 | 写入内容 |
|---|---|
| `item` | `layout=multi`、`data_type=model_3d`、`format=gltf`、`refs` 中记录 manifest、buffer、image 等 related refs |
| `type_info.model_3d` | `model_kind=mesh_scene`、节点数、mesh 数、顶点数、三角面数、材质数、纹理数、动画数、三维包围盒、单位、up axis 等跨格式模型摘要 |
| `format_info.gltf` | glTF 版本、generator、asset copyright、默认 scene、scene / buffer / image / accessor 数量、extensions used / required、外部资源数量等 glTF 私有事实 |
| `capabilities.spatial` | 仅在模型有明确地理定位、CRS 或可解析空间范围时写入 |

### 消费要求

Manager 第一阶段应基于已入库 `data_type=model_3d + layout=multi + format=gltf` 和 `item.refs` 消费 glTF 多资源模型。主快显路线是通过 `model_3d_glb_generation` 调用 `model3d_workflow` 的 `gltf_to_glb` direct operator，将 manifest、buffer 和纹理资源打包为 Manager infra MinIO 中的 GLB artifact，再复用 `model_3d` / GLB 前端预览链路。后续如需要浏览器直接预览原生 glTF，必须保持 manifest 相对 URI 可通过 content stream 或同等资源代理访问；不得把 glTF 源 item 伪装为 GLB。

### 格式约束

- 不得把 `.gltf` 当普通 `json` 表、文档或容器入库。
- 不得把 `.bin`、纹理图片、KTX2 等被 manifest 引用的资源重复落成独立 item。
- 不得使用同 basename 规则、目录图片扫描或前端运行时猜测来补全 glTF refs。
- glTF JSON 原文、buffer 内容、纹理内容、前端渲染 URL 和转换产物不得写入 `type_info.model_3d`。

## OBJ

### 识别与组织

| 维度 | 取值 |
|---|---|
| `layout` | `single` |
| `data_type` | `model_3d` |
| `format` | `obj` |
| 主资源 | `meta_item.full_name` 指向 `.obj` 文件资源 |

OBJ 按单资源三维模型接入，主 item 是 `.obj` 文件。扫描器应读取 OBJ 文本中的 `mtllib`，将命中的本地 `.mtl` 作为 related refs / claims；对于 `.mtl` 中命中的本地贴图资源，也作为同一 OBJ item 的 refs / claims。不得在没有 manifest 边界的情况下把目录提升为 `layout=multi`。如果 OBJ 声明了 `mtllib`，但对应 `.mtl` 不存在，扫描不得凭同目录图片或同 basename 图片猜测贴图关系。扫描可读取 OBJ 文本中的 `v`、`f`、`o`、`g`、`mtllib`、`usemtl` 形成轻量摘要；对于 P3BJet 等导出器写在注释头部的 `BoundingBox(...)`、`Vertices:`、`Faces:`，可优先作为声明摘要写入，避免超大 OBJ 必须全量逐行解析。

### attributes 写入

| 分区 | 写入内容 |
|---|---|
| `item` | `layout=single`、`data_type=model_3d`、`format=obj` |
| `type_info.model_3d` | `model_kind=mesh_scene`、mesh 数、顶点数、三角面数、三维包围盒等跨格式模型摘要 |
| `format_info.obj` | OBJ 私有摘要，例如 face 数、object / group 数、material library 数、是否使用材质、声明顶点 / 面数、扫描行数和 `scan_complete` |

### 消费要求

Manager 基础预览可将单文件 OBJ 源文件通过 storage stream 交给前端三维渲染器直接读取；该基础预览不查找、替换或依赖 Manager 受管 GLB artifact。快显路线仍是通过 `model_3d_glb_generation` 调用 `model3d_workflow` 的 `obj_to_glb` direct operator，由运行时内置的 `assimp export` 生成 Manager infra MinIO 中的自包含 GLB artifact，再复用 `model_3d` / GLB 前端预览链路。OBJ 的 `.mtl` 和纹理引用由转换器从源文件相对路径解析并嵌入 GLB，不作为浏览器基础预览访问源目录贴图的主路径。转换前应校验 OBJ 声明的本地 `.mtl` 是否存在；缺失时任务应失败并提示缺失材质库，不能登记一个无贴图灰模为 ready artifact。

超大 OBJ deep scan 不应因为行数超过预算而失败。扫描器可以返回预算内 partial 摘要，并通过 `format_info.obj.scan_complete=false` 与 `scanned_line_count` 表示未做全量解析；如果文件头部已声明顶点数、面数和包围盒，这些声明事实可进入 `type_info.model_3d` 与 `format_info.obj.declared_*`。

## STL

### 识别与组织

| 维度 | 取值 |
|---|---|
| `layout` | `single` |
| `data_type` | `model_3d` |
| `format` | `stl` |
| 主资源 | `meta_item.full_name` 指向 `.stl` 文件资源 |

STL 按单资源三维模型接入，支持 ASCII STL 与 binary STL 的轻量摘要和 GLB 快显。STL 材质表达弱，不能把材质、纹理或业务语义臆造进 `type_info.model_3d`。

### attributes 写入

| 分区 | 写入内容 |
|---|---|
| `item` | `layout=single`、`data_type=model_3d`、`format=stl` |
| `type_info.model_3d` | `model_kind=mesh_scene`、mesh 数、顶点数、三角面数、三维包围盒等跨格式模型摘要 |
| `format_info.stl` | STL 私有摘要，例如 encoding、顶点数、三角面数、扫描行数 / 采样三角面数和 `scan_complete` |

### 消费要求

Manager 基础预览可将单文件 STL 源文件通过 storage stream 交给前端三维渲染器直接读取；该基础预览不查找、替换或依赖 Manager 受管 GLB artifact。快显路线仍是通过 `model_3d_glb_generation` 调用 `model3d_workflow` 的 `stl_to_glb` direct operator，由运行时内置的 `assimp export` 生成 Manager infra MinIO 中的 GLB artifact，再复用 `model_3d` / GLB 前端预览链路。STL 材质表达弱，转换结果不应臆造材质、纹理或业务语义。

超大 ASCII STL deep scan 不应因为行数超过预算而失败，应返回预算内 partial 摘要并写入 `format_info.stl.scan_complete=false`。Binary STL 的 triangle count 来自 80 字节 header 后的 uint32 计数；扫描器可以只读取预算内三角面用于 bounds 采样，不能为了计算 bounds 强制全量读取超大 binary STL。

## PLY

### 识别与组织

| 维度 | 取值 |
|---|---|
| `layout` | `single` |
| `data_type` | 按 PLY header 判定为 `model_3d`、`point_cloud` 或 `gaussian_splat` |
| `format` | `ply` |
| 主资源 | `meta_item.full_name` 指向 `.ply` 文件资源 |

PLY 是单资源、内容敏感格式。它既可能表达 polygon mesh，也可能只包含顶点集合或 Gaussian Splat 导出数据；扫描不得仅凭 `.ply` 扩展名臆造面、材质、三角面或点云事实。实现必须优先读取 PLY header，记录 format、version、element counts、vertex properties 和可识别的 Gaussian Splat 信号，并按 header 事实确定 `data_type`。

### attributes 写入

| 分区 | 写入内容 |
|---|---|
| `item` | `layout=single`、`format=ply`、按 header 判定后的 `data_type` |
| `type_info.model_3d` | 仅 mesh PLY 写入；`model_kind`、顶点数和 mesh 相关轻量事实 |
| `type_info.point_cloud` | 仅点云型 PLY 写入；点数、维度列表、颜色 / intensity 能力 |
| `type_info.gaussian_splat` | 仅 3DGS 型 PLY 写入；高斯基元数量、opacity / scale / rotation / spherical harmonics 能力 |
| `format_info.ply` | PLY 私有摘要，例如 encoding、version、layout、vertex / face count、element counts、vertex property 数、是否 Gaussian Splat |

### 消费要求

Manager 按 `data_type` 选择消费路线。`data_type=model_3d` 的普通 mesh PLY 可通过 storage stream 交给前端 Three.js PLYLoader 做基础预览；该基础预览不查找、替换或依赖 Manager 受管 GLB artifact。点云型 PLY 走点云路线；Gaussian Splat PLY 走高斯泼溅 renderer，不得简单等同于 mesh GLB，也不开放 `model_3d` 的 GLB 快显任务入口。

PLY deep scan 不应读取完整大文件数据体；header 级事实足以完成首轮识别和轻量摘要。模型包围盒、材质、纹理、法线等事实只有在稳定解析后才可写入。

## SPLAT

### 识别与组织

| 维度 | 取值 |
|---|---|
| `layout` | `single` |
| `data_type` | `gaussian_splat` |
| `format` | `splat` |
| 主资源 | `meta_item.full_name` 指向 `.splat` 文件资源 |

`.splat` 是高斯泼溅的直接消费格式。Meta scan 通过扩展名和内置 descriptor 将其归为 `gaussian_splat`，不归入 `model_3d` 或 `point_cloud`。

### attributes 写入

| 分区 | 写入内容 |
|---|---|
| `item` | `layout=single`、`data_type=gaussian_splat`、`format=splat` |
| `type_info.gaussian_splat` | `representation=3d_gaussian_splatting`；不强制扫描完整文件计算基元数 |
| `format_info.splat` | `encoding=splat` 等格式私有轻量事实 |

### 消费要求

Manager 可使用高斯泼溅 renderer 直接读取 `.splat` URL 做基础预览；不得开放 GLB 快显生成入口。需要平台受管 artifact、统一对象存储发布或大模型优化时，走 `gaussian_splat_ksplat_generation` 专用路线，由 `model3d_workflow` 的 `gaussian_splat_to_ksplat` operator 转换为 KSplat artifact，不复用 `model_3d_glb_generation`。

## KSPLAT

### 识别与组织

| 维度 | 取值 |
|---|---|
| `layout` | `single` |
| `data_type` | `gaussian_splat` |
| `format` | `ksplat` |
| 主资源 | `meta_item.full_name` 指向 `.ksplat` 文件资源 |

`.ksplat` 是面向高斯泼溅渲染优化的压缩消费格式。Meta scan 通过扩展名和内置 descriptor 将其归为 `gaussian_splat`，不归入 `model_3d` 或 `point_cloud`。

### attributes 写入

| 分区 | 写入内容 |
|---|---|
| `item` | `layout=single`、`data_type=gaussian_splat`、`format=ksplat` |
| `type_info.gaussian_splat` | `representation=3d_gaussian_splatting`；不强制扫描完整文件计算基元数 |
| `format_info.ksplat` | `encoding=ksplat` 等格式私有轻量事实 |

### 消费要求

Manager 可使用高斯泼溅 renderer 直接读取 `.ksplat` URL 做基础预览；不得开放 GLB 快显生成入口，也不为源 `.ksplat` 创建 KSplat 快显任务。`.ksplat` 同时是高斯泼溅受管快显的目标文件格式；`gaussian_splat_ksplat_generation` 只对 `ply` / `splat` 源执行真实转换，不复用 `model_3d_glb_generation`。

## FBX

### 识别与组织

| 维度 | 取值 |
|---|---|
| `layout` | `single` |
| `data_type` | `model_3d` |
| `format` | `fbx` |
| 主资源 | `meta_item.full_name` 指向 `.fbx` 文件资源 |

FBX 第一阶段按单资源三维模型接入。扫描只做格式识别和轻量 header 摘要，不解析完整 proprietary scene graph；复杂节点、材质和动画事实不得臆造进 `type_info.model_3d`。

### attributes 写入

| 分区 | 写入内容 |
|---|---|
| `item` | `layout=single`、`data_type=model_3d`、`format=fbx` |
| `type_info.model_3d` | `model_kind=mesh_scene`、mesh 数等可稳定确认的跨格式模型摘要 |
| `format_info.fbx` | FBX 私有摘要，例如 encoding |

### 消费要求

Manager 基础预览可将单文件 FBX 源文件通过 storage stream 交给前端三维渲染器直接读取；该基础预览不查找、替换或依赖 Manager 受管 GLB artifact。快显路线仍是通过 `model_3d_glb_generation` 调用 `model3d_workflow` 的 `fbx_to_glb` direct operator，由运行时内置的 `assimp export` 生成 Manager infra MinIO 中的 GLB artifact，再复用 `model_3d` / GLB 前端预览链路。`_3dtile -f fbx` 的输出语义是 3D Tiles 目录，不得用于登记 GLB artifact。

## IFC

### 识别与组织

| 维度 | 取值 |
|---|---|
| `layout` | `single` |
| `data_type` | `model_3d` |
| `format` | `ifc` |
| 主资源 | `meta_item.full_name` 指向 `.ifc` 文件资源 |

IFC 第一阶段按单资源 BIM 模型接入。IFC 是 STEP Physical File 形态的 BIM / 参数化建筑模型，不得把 `.ifc` 当普通 `text`、`document`、`table` 或普通 mesh 入库。扫描器只做格式识别和轻量 STEP 摘要，不解析完整构件属性树、楼层空间树或几何表达。

### attributes 写入

| 分区 | 写入内容 |
|---|---|
| `item` | `layout=single`、`data_type=model_3d`、`format=ifc` |
| `type_info.model_3d` | `model_kind=bim_model`；其它跨格式三维模型摘要只有能稳定确认时才写入 |
| `format_info.ifc` | IFC 私有轻量摘要，例如 `encoding=step`、schema identifiers / schema version、entity count、entity type counts、扫描行数和 `scan_complete` |
| `capabilities.spatial` | 仅在 IFC 中存在明确 CRS、地理定位或可解析空间范围时写入 |

### 消费要求

Manager 当前不要求浏览器直接解析 IFC。主快显路线是通过 `model_3d_glb_generation` 调用 `model3d_workflow` 的 `ifc_to_glb` direct operator，由运行时绑定的 `IfcConvert` 生成 Manager infra MinIO 中的 GLB artifact，再复用 `model_3d` / GLB 前端预览链路。IFC -> 3D Tiles、构件属性查询、楼层 / 空间树和属性服务不属于第一阶段。

### 格式约束

- 不得把 IFC 归为 `document`、`table`、`container` 或普通 `mesh_scene`。
- 不得把 IFC 构件属性集、楼层、空间树、几何体、缩略图、转换产物或前端渲染协议写入 `type_info.model_3d`。
- 不得复用 FBX / OBJ / STL 的 GLB 快显任务或假定 `assimp` 能稳定转换 IFC；IFC 只能通过 `ifc_to_glb` 专用 operator 进入 GLB 快显路线。

## 3D Tiles

### 识别与组织

| 维度 | 取值 |
|---|---|
| `layout` | `whole` |
| `data_type` | `model_3d` |
| `format` | `3dtiles` |
| 主入口 | scope 根目录下的 `tileset.json` |
| 主资源表达 | `meta_item.full_name` 指向 tileset 所在目录，`item.refs` 中 `tileset.json` 的 `role=manifest` 且 `primary=true` |

3D Tiles 是以 `tileset.json` 为 manifest 的分块三维场景。ADDP 不把 `tileset.json` 当普通 JSON 表或文档入库，而是把其所在目录 / prefix 识别为一个 `layout=whole` 的 `model_3d` data item。3D Tiles 1.0、1.1 都使用同一个 `format=3dtiles`；版本差异写入 `format_info.3dtiles.asset_version`、extensions 和后续格式私有字段，不拆分 data type 或 format。

内容识别不能只依赖文件名；扫描实现必须解析 manifest 并校验 `asset.version` 与 `root`，解析失败时不得产出 3D Tiles item。

### attributes 写入

| 分区 | 写入内容 |
|---|---|
| `item` | `layout=whole`、`data_type=model_3d`、`format=3dtiles`、`refs` 中记录 `tileset.json` manifest；被认领的瓦片资源由 detector claims 表达 |
| `type_info.model_3d` | `model_kind=tiled_scene`、LOD 数量、三维包围盒等跨格式模型摘要 |
| `format_info.3dtiles` | `manifest_ref`、`asset_version`、`tileset_version`、`geometric_error`、`root_refine`、tile/content/leaf/max_depth 统计、extensions used / required、property 数量等 3D Tiles 私有事实 |
| `capabilities.spatial` | 仅在 manifest 中存在可解析 `region` 等明确地理范围时写入；3D Tiles region 按 EPSG:4326 经纬度范围表达 |

### 消费要求

Manager 预览应消费已入库 `data_type=model_3d + format=3dtiles`、`layout=whole` 和 `tileset.json` 入口，通过 storage stream 或同等内容通道保持 tileset 相对资源可访问。预览 DTO 可以使用 `frontend_renderer=3dtiles`，但 renderer 名称、加载 URL 重写规则、瓦片抽稀策略、截图或转换产物不得写入 Meta attributes。

### 格式约束

- 不得把 3D Tiles 归为 `json`、`container` 或 `media`。
- 不得把瓦片文件列表、二进制 tile 内容、纹理内容或前端渲染状态写入 `type_info.model_3d`。
- OSGB 倾斜摄影目录、I3S、Cesium ion 资产等需要单独 format 规则；不得用 `format=3dtiles` 兼容所有目录型三维模型。
- 3D Tiles 内嵌的 b3dm / i3dm / pnts / glTF 是 tileset 内容资源，不默认作为独立 data item 重复入库。
- 3D Tiles 1.1 的 structured metadata、multiple contents、implicit tiling 等能力先作为 `format_info.3dtiles` 的格式私有事实扩展；只有形成跨格式模型事实时才提升到 `type_info.model_3d` 或 `capabilities`。

## OSGB

### 识别与组织

| 维度 | 取值 |
|---|---|
| `layout` | `single` |
| `data_type` | `model_3d` |
| `format` | `osgb` |
| 主资源 | `meta_item.full_name` 指向 `.osgb` 文件资源 |

OSGB 在 ADDP 中首先表示单个 `.osgb` 文件。它类似单个 TIFF：可以被扫描为普通 single item，但浏览器直接解析和渲染成本较高。单 OSGB 的快显路线是通过 `model_3d_glb_generation` 调用 `model3d_workflow` 的 `osgb_to_glb` direct operator，生成 Manager infra MinIO 中的 GLB artifact，再复用 `model_3d` / GLB 前端预览链路。

单个 `.osgb` 文件只在未被 `osgb_scene` whole scope 强命中 claims 覆盖时落为独立 item。不得因为同目录存在多个 `.osgb` 文件就自动推断为倾斜摄影场景。

### attributes 写入

| 分区 | 写入内容 |
|---|---|
| `item` | `layout=single`、`data_type=model_3d`、`format=osgb` |
| `type_info.model_3d` | 第一阶段可为空或仅写跨格式稳定摘要；不得为了快显写入 GLB artifact 信息 |
| `format_info.osgb` | 单文件 OSGB 私有摘要；第一阶段没有稳定 parser 时可以不写 |
| `capabilities.spatial` | 仅在单文件解析出明确 CRS 或空间范围时写入 |

### 消费要求

Manager 不应把 `.osgb` 原始文件直接暴露给前端解析作为主路径。快显能力应消费已入库 `data_type=model_3d + layout=single + format=osgb` item，通过 `model_3d_glb_generation` 任务调用 `model3d_workflow` 的 `osgb_to_glb` direct operator，生成并登记 `manager.model_3d_glb` GLB artifact；该 artifact 存放在 Manager infra MinIO 中，不自动成为业务存储中的新 data item。

### 格式约束

- 不得把单个 `.osgb` 文件归为 `osgb_scene`、`container` 或 `unknown`。
- 不得把单 OSGB 快显 GLB artifact 写入 `format_info.osgb` 或 `type_info.model_3d`。
- 不得在单文件规则中解析目录级 `metadata.xml`；目录级 metadata 只属于 `osgb_scene`。

## OSGB Scene

### 识别与组织

| 维度 | 取值 |
|---|---|
| `layout` | `whole` |
| `data_type` | `model_3d` |
| `format` | `osgb_scene` |
| 主入口 | scope 根目录下的 `metadata.xml` |
| 主资源表达 | `meta_item.full_name` 指向 OSGB Scene 根目录，`item.refs` 中 `metadata.xml` 的 `role=manifest` 且 `primary=true` |

OSGB Scene 在 ADDP 中表示一套 OSGB 倾斜摄影三维模型数据集，不占用单文件 `format=osgb` 名称。扫描命中根目录下 `metadata.xml` 且可解析为 `ModelMetadata` 后，整个目录 / prefix 识别为一个 `layout=whole` 的 `model_3d` data item。`.osgb` 叶子文件由 detector claims 认领，`refs` 只保留 manifest 等关键入口。

### attributes 写入

| 分区 | 写入内容 |
|---|---|
| `item` | `layout=whole`、`data_type=model_3d`、`format=osgb_scene`、`refs` 中记录 `metadata.xml` manifest |
| `type_info.model_3d` | `model_kind=photogrammetry_scene`、数据集总大小等跨格式模型摘要 |
| `format_info.osgb_scene` | `manifest_ref`、`data_dir`、`metadata_version`、`srs`、`srs_origin`、`color_source` 等 OSGB Scene 私有事实 |
| `capabilities.spatial` | 当 `metadata.xml` 中 `SRS` 可解析为 `EPSG:<srid>` 时写入 `srid` 和 `crs_ref` |

### 消费要求

Manager 第一阶段不直接预览 OSGB Scene 源数据。OSGB Scene 源 item 应通过 `model_3d_tiles_generation` 任务转换为目标业务存储中的 `format=3dtiles`、`layout=whole` item；转换完成后触发 Meta deep scan，并复用 3D Tiles 预览链路。

### 格式约束

- 不得把 OSGB Scene 目录归为普通 `container`、`unknown` 或每个 `.osgb` 单文件 item。
- 不得仅因出现 `.osgb` 文件就独占整个目录；必须有根目录 `metadata.xml` 强命中。
- 不得把 OSGB Scene 源数据伪装为 `format=3dtiles`；转换结果才是 `format=3dtiles`。
- OSGB Scene 直接预览、缩略图和转换产物不写入 `type_info.model_3d` 或 `format_info.osgb_scene`。

## LAS

### 识别与组织

| 维度 | 取值 |
|---|---|
| `layout` | `single` |
| `data_type` | `point_cloud` |
| `format` | `las` |
| 主资源 | `meta_item.full_name` 指向 `.las` 文件资源 |

LAS 是单资源点云格式。第一阶段 LAS 作为 `point_cloud` 的代表格式接入，用于走通点云 header 解析、Meta attributes 写入和 Manager 抽样预览主链路。LAZ / COPC、EPT、Potree、E57、PCD、点云型 PLY 等格式不借用 `format=las`，应分别建立自己的 format descriptor 和规则。

### attributes 写入

| 分区 | 写入内容 |
|---|---|
| `item` | `layout`、`data_type=point_cloud`、`format=las` |
| `type_info.point_cloud` | `point_cloud_kind=raw_point_cloud`、点数、point format、维度数量、维度列表、三维包围盒、scale、offset、是否包含颜色 / intensity / classification 等跨格式点云摘要 |
| `format_info.las` | LAS 版本、header size、offset to point data、point data record length、VLR / EVLR 数量、system identifier、generating software 等 LAS 私有事实 |
| `capabilities.spatial` | CRS、空间参考定义、空间范围等可解析空间事实 |
| `capabilities.statistics` | 分类分布、回波摘要、抽样规模、密度估算等可选画像事实 |

### 消费要求

Manager 点云预览应基于已入库 `data_type=point_cloud + format=las` 和标准内容通道读取抽样点集，不得把完整 LAS 内容或点样本塞入 attributes。大文件预览应通过抽样、分块或后续派生 LOD 产物实现；前端渲染协议属于 Manager preview DTO，不是 Meta attributes。

### 格式约束

- 不得仅因 LAS 点记录可列化为 x/y/z、intensity、classification 等字段而归为 `table`。
- 不得把 LAS header 私有字段写入 `type_info.point_cloud`；LAS 原生 header 细节进入 `format_info.las`。
- CRS、空间定位和空间范围进入 `capabilities.spatial`，不写入 `format_info.las` 的私有字段作为平台行为事实。
- LAZ / COPC 压缩和层级结构需要单独 format 规则；不得用 `format=las` 兼容读取压缩点云。

## PLY

### 识别与组织

| 维度 | 取值 |
|---|---|
| `layout` | `single` |
| `format` | `ply` |
| 主资源 | `meta_item.full_name` 指向 `.ply` 文件资源 |

PLY 是内容敏感格式，不能仅凭扩展名固定归入单一数据类型。Meta deep scan 必须读取 PLY header，并按以下规则确定 `data_type`：

| header 事实 | `data_type` | `format_info.ply.layout` |
|---|---|---|
| `element face` 数量大于 0 | `model_3d` | `mesh` |
| 无 face，且具备 `x/y/z`、`opacity`、`scale_0..2`、`rot_0..3`、`f_dc_0..2` 或颜色字段 | `gaussian_splat` | `gaussian_splat` |
| 无 face，且具备 `chunk` element 与 `packed_position`、`packed_rotation`、`packed_scale`、`packed_color` vertex 属性 | `gaussian_splat` | `gaussian_splat` |
| 无 face，且不满足高斯泼溅字段组合 | `point_cloud` | `point_cloud` |

SuperSplat 压缩 PLY 仍归入 `data_type=gaussian_splat`、`format=ply`，并通过 `format_info.ply.is_compressed_splat=true` 表达格式私有事实。Manager 基础预览读取该标记选择非渐进加载策略，不把压缩 PLY 转入 GLB 快显路线。

### attributes 写入

| 分区 | 写入内容 |
|---|---|
| `item` | `layout=single`、`format=ply`、按 header 判定后的 `data_type` |
| `type_info.model_3d` | 仅 mesh PLY 写入；`model_kind=mesh_scene`、vertex count、mesh count 等跨格式三维模型摘要 |
| `type_info.point_cloud` | 仅点云型 PLY 写入；`point_cloud_kind=raw_point_cloud`、点数、维度列表、颜色 / intensity 能力等跨格式点云摘要 |
| `type_info.gaussian_splat` | 仅 3DGS 型 PLY 写入；`representation=3d_gaussian_splatting`、splat count、opacity / scale / rotation / spherical harmonics 能力 |
| `format_info.ply` | encoding、version、layout、vertex count、face count、header line count、vertex properties、element counts、是否高斯泼溅等 PLY 私有事实 |

### 消费要求

Manager 需要按 `data_type` 分别选择快显路线：

- mesh PLY 可走三维模型快显路线，后续可接 PLY -> GLB。
- 点云型 PLY 走点云快显路线。
- 3DGS 型 PLY 走高斯泼溅快显路线，不转换为 GLB。

### 格式约束

- 不得把所有 `.ply` 固定归入 `model_3d` 或 `point_cloud`。
- 不得仅因 3DGS PLY 使用 vertex 记录就归为 `point_cloud`。
- 不得把高斯泼溅的渲染协议、排序结果、压缩产物或派生 splat artifact 写入 `type_info.gaussian_splat`。
- PLY header 私有属性列表进入 `format_info.ply`，跨格式通用摘要进入对应 `type_info.<data_type>`。

## CSV / TSV

### 识别与组织

| 维度 | CSV | TSV |
|---|---|---|
| `layout` | `single` | `single` |
| `data_type` | `table` | `table` |
| `format` | `csv` | `tsv` |
| 主资源 | `meta_item.full_name` 指向文件资源 | `meta_item.full_name` 指向文件资源 |

CSV / TSV 是单资源表格文件。字段名来自表头；无表头时由 parser 生成稳定列名。字段类型来自采样推断。

### attributes 写入

| 分区 | 写入内容 |
|---|---|
| `item` | `layout`、`data_type`、`format` |
| `type_info.table` | `fields`、`row_count`、`primary_key`、`native.delimiter`、`native.has_header`、`native.quote_char`、`native.escape_char` |
| `format_info.csv` / `format_info.tsv` | `encoding`、`line_ending`、文件级解析摘要等格式私有信息 |
| `capabilities.statistics` | 采样统计、画像摘要、空值率等可选统计能力 |

### 格式约束

- 不得把 CSV / TSV 放入 `document`，除非明确按文档而不是表格消费。
- 分隔符和表头判断以 `type_info.table.native` 为准；编码以 `format_info.csv|tsv` 为准，不在消费者侧二次猜测。

## Excel

### 识别与组织

| 维度 | 取值 |
|---|---|
| `layout` | `single` |
| `data_type` | `container` |
| `format` | `excel` |
| 主资源 | `meta_item.full_name` 指向工作簿文件 |

当前阶段 Excel 文件先作为一个容器 item。内部 sheet 是内部子 item，不自动展开为独立 `meta_item`。

### attributes 写入

| 分区 | 写入内容 |
|---|---|
| `item` | `layout`、`data_type`、`format` |
| `type_info.container` | `children`、`default_child`、`child_count`、sheet 摘要 |
| `format_info.excel` | 工作簿版本、sheet 数量、默认 sheet、采样策略等工作簿或格式层事实 |

### 内部读取

Manager 可以基于 `type_info.container.children` 展示 sheet 列表；进入某个 sheet 的表格内容读取时，应由容器读取能力定位内部对象，再交给 `TableInfoProvider` / `TableSampleReader` 归一为表语义。

当某个 sheet 被归一为 table item 或 table describe 结果时，`sheet_name`、`sheet_index` 等当前表级来源原生事实写入 `type_info.table.native`，不得写入 `format_info.excel`。外层工作簿的 `sheet_count`、`default_sheet` 等工作簿级事实只写入 `format_info.excel`，不得重复写入 `type_info.container.native`。

### 格式约束

- 不得把所有 sheet 字段合并成外层工作簿的 `type_info.table.fields`。
- 不得改变 Manager / Transfer 的外层 item 路由语义。

## JSON

### 识别与组织

| 场景 | `layout` | `data_type` | `format` |
|---|---|---|---|
| records array | `single` | `table` | `json` |
| JSON Lines | `single` | `table` | `json` |
| 任意对象、配置文件、嵌套文档 | `single` | `document` 或 `container` | `json` |

`.json` 后缀不能直接等同于表格。必须验证内容结构，并按平台消费方式确定 `data_type`。如果 `.json` 文件内容前缀能严格证明其为 GeoJSON `FeatureCollection`，Meta deep scan 应升格识别为 `format=geojson`，由 GeoJSON 小节处理。

### attributes 写入

| 分区 | 写入内容 |
|---|---|
| `item` | `layout`、`data_type`、`format` |
| `type_info.table` | records / JSON Lines 的字段、行数 |
| `type_info.document` | 文档型 JSON 的标题、语言、编码、页数、字数、大小等通用文档结构信息 |
| `type_info.container` | 容器型 JSON 的内部对象摘要、默认入口、子对象数量 |
| `format_info.json` | `structure`、编码、对象层级摘要等格式私有信息 |
| `capabilities.spatial` | 仅记录值中严格解析出 WKB / EWKB 等几何字段时写入空间能力 |
| `capabilities.statistics` | 是否采样、采样规模、动态结构推断方式等统计或画像事实 |

### 格式约束

- 不得只按扩展名把 JSON 判为 `table` 或 `spatial`。
- `.json` 文件中出现严格 GeoJSON `FeatureCollection` 结构时，应识别为 `format=geojson`，不得继续写入 `format_info.json`。
- 不得把 JSON 私有结构字段写入 `capabilities.spatial`。
- 插件推导出来的记录数、几何类型、bbox 等归一事实不得写入 `format_info.json`；记录数进入 `type_info.table.row_count`，空间范围进入 `capabilities.spatial.extent`。

## GeoJSON

### 识别与组织

| 维度 | 取值 |
|---|---|
| `layout` | `single` |
| `data_type` | `table` |
| `format` | `geojson` |
| 主资源 | `meta_item.full_name` 指向 GeoJSON 文件资源 |

GeoJSON 是单资源空间矢量表格式。`.geojson`、`application/geo+json`、`application/vnd.geo+json` 是强识别事实；`.json` 只是普通 JSON 后缀，只有内容前缀严格匹配 GeoJSON `FeatureCollection` 结构时，才识别为 `format=geojson`。Meta deep scan 对 `.json` 表格候选必须读取内容前缀后调用统一格式探测，不得只按后缀固定为 JSON provider。

当前内置 GeoJSON table 只支持 `FeatureCollection.features` 记录集合。单个 `Feature`、`GeometryCollection` 或任意带 `geometry` 字段的 JSON 对象是否进入 GeoJSON table 主路径，需要先补规范再实现。

### attributes 写入

| 分区 | 写入内容 |
|---|---|
| `item` | `layout`、`data_type`、`format` |
| `type_info.table` | `properties` 字段、平台统一几何字段、`row_count` |
| `format_info.geojson` | `structure`、原文显式 `bbox` / `crs`、`coordinate_range_out_of_wgs84`、属性摘要等 GeoJSON 格式私有事实 |
| `capabilities.spatial` | 实际记录中发现 geometry 时写入几何字段、SRID / CRS、extent 等空间能力 |
| `capabilities.statistics` | 是否采样、采样规模、动态结构推断方式等统计或画像事实 |

### 格式约束

- GeoJSON 类似 Shapefile，默认是 `data_type=table`；但 spatial 仍是横切能力，只能来自解析结果。
- 不得因为 `format=geojson` 直接写入 `capabilities.spatial`。FeatureCollection 没有有效 geometry 时，不写空间能力。
- 不得把 GeoJSON 私有结构字段写入 `format_info.json`；格式私有事实进入 `format_info.geojson`。
- 记录数进入 `type_info.table.row_count`，空间范围进入 `capabilities.spatial.extent`。只有 GeoJSON 原文显式声明的 `bbox` 可作为格式事实保留在 `format_info.geojson.bbox`。
- 没有显式可解析 CRS / SRID 时，只有实际或显式 bbox 落在经纬度范围 `x=[-180,180]`、`y=[-90,90]` 内，才可按 GeoJSON 默认语义写入 `EPSG:4326`。bbox 明显越界时不得默认写入 4326，应保留 `capabilities.spatial.extent`，省略 SRID / CRS，并在 `format_info.geojson.coordinate_range_out_of_wgs84=true` 标记。
- `format_info.geojson.structure` 应来自 GeoJSON provider 的格式私有事实；`.json` 后缀被升格为 GeoJSON 时，也只能写入 `format_info.geojson`，不得同时写入 `format_info.json`。

### 空间行值编码

| 方向 | 支持的 geometry encoding | 默认 encoding | native encoding | 说明 |
|---|---|---|---|---|
| read | `geojson`、`ewkb` | `geojson` | `geojson` | `geojson` 用于预览、调试和格式本地场景；`ewkb` 用于 Transfer 跨格式 / 跨 engine 链路。 |
| write | `geojson`、`ewkb` | `geojson` | `geojson` | writer 可把 `ewkb` 转为 GeoJSON geometry object，但不执行 CRS 转换。 |

GeoJSON 标准导出前，Transfer 必须保证传入 writer 的几何坐标已经满足 4326 / WGS84 经纬度约束。GeoJSON source 如果没有可识别 CRS 且坐标范围明显越界，不得被当作 4326 source 直接导出；需要用户补充 source CRS 后再由 planner 选择源端 transform 或 `vector_reproject`。

## Shapefile

### 识别与组织

| 维度 | 取值 |
|---|---|
| `layout` | `multi` |
| `data_type` | `table` |
| `format` | `shapefile` |
| 主资源 | `.shp`，即 `meta_item.full_name` |
| 必需 refs | `.shp`、`.shx`、`.dbf` |
| 可选 refs | `.prj`、`.cpg`、`.sbn`、`.sbx` |

Shapefile 是空间矢量表，不是单个 `.shp` 文件。ref 匹配规则是同目录或同 prefix 下相同 basename；不得跨目录递归匹配；不独占目录。

### attributes 写入

| 分区 | 写入内容 |
|---|---|
| `item` | `layout=multi`、`data_type=table`、`format=shapefile`、`refs`、`file_count` |
| `type_info.table` | `.dbf` 非空间字段、平台统一几何字段、`row_count`、`primary_key`、`native.shape_type`、`native.dbf_version`、`native.encoding` |
| `format_info.shapefile` | `base_name`、`ref_extensions`、`has_prj`、`has_cpg`、文件组件级摘要 |
| `capabilities.spatial` | `geometry_columns`、`primary_geometry_column`、`srid`、`crs_ref`、`crs_definitions`、`extent`、`has_spatial_index` |

字段规则：

- `.dbf` 提供非空间字段。
- `.shp` 提供平台统一几何字段。
- 平台统一几何字段的字段类型为 `geometry`。默认 sample 行值为 WKT 字符串；连续读取可通过 `ParseOptions.GeometryEncoding` 请求 `wkb` 或 `ewkb`，行值为 `[]byte`。
- `ewkb` 可以携带 SRID；Shapefile 的 SRID / CRS 事实仍以 `.prj` 解析结果和 `capabilities.spatial` / `TableInfo.SpatialInfo` 为准。
- Shapefile 原生 shape type 是格式事实，写入 `format_info.shapefile.shape_type` 或 `type_info.table.native.shape_type`；平台标准几何拓扑必须写入 `capabilities.spatial.geometry_columns[].geometry_type`。
- Shapefile `Point` family 归一为 `Point`，`MultiPoint` family 归一为 `MultiPoint`，`PolyLine` family 归一为 `MultiLineString`，`Polygon` family 归一为 `MultiPolygon`。`NullShape` 不生成空间字段；其他未纳入标准主路径的 shape type 必须返回明确 unsupported 或落为 `Geometry`，不得伪造成具体拓扑。
- 字段类型映射为 ADDP 通用字段类型。原始 DBF 类型属于 Shapefile format plugin 内部事实；如需给 Manager 展示，只能写入只读 attributes，不能进入 Transfer / engine / format writer 的执行决策。
- 记录数来自真实 Shapefile 记录数，不写固定占位值。
- 写出 `.prj` 时，format writer 只接受 `WriteOptions.ExtraParams["crs_definition"]` 作为 CRS 定义文本；该字段表达定义内容，不得写成 `format` 或 PostGIS `spatial_ref_sys`。

### 空间行值编码

| 方向 | 支持的 geometry encoding | 默认 encoding | native encoding | 说明 |
|---|---|---|---|---|
| read | `shapefile_shape`、`wkt`、`wkb`、`ewkb` | `wkt` | `shapefile_shape` | 默认 `wkt` 服务 sample / preview / 调试；Transfer 跨格式链路优先请求 `ewkb`。 |
| write | `shapefile_shape`、`wkt`、`wkb`、`ewkb` | `wkt` | `shapefile_shape` | `shapefile_shape` 只用于 Shapefile 同构且无需 CRS 转换的 native passthrough。 |

Shapefile 的 `.shp` 原生 shape record 对用户有格式语义价值，因此可以作为 `shapefile_shape` 暴露；但非 Shapefile writer 不应消费该编码。跨 format、写入 native table 或需要 CRS 转换时，Transfer 应切换到 portable encoding，第一阶段优先 `ewkb`。

### 标准写入示例

```json
{
  "schema_version": 1,
  "storage": {
    "physical_path": "/shp/",
    "total_size": 3069403
  },
  "item": {
    "layout": "multi",
    "data_type": "table",
    "format": "shapefile",
    "refs": [
      {"path": "/shp/farmland.shp", "role": "main", "required": true, "primary": true, "extension": ".shp"},
      {"path": "/shp/farmland.shx", "role": "index", "required": true, "extension": ".shx"},
      {"path": "/shp/farmland.dbf", "role": "attributes", "required": true, "extension": ".dbf"},
      {"path": "/shp/farmland.prj", "role": "projection", "extension": ".prj"}
    ],
    "file_count": 4
  },
  "type_info": {
    "table": {
      "fields": [
        {
          "name": "geometry",
          "type": "geometry",
          "native_type": "Polygon",
          "nullable": false
        }
      ],
      "row_count": 1234,
      "primary_key": []
    }
  },
  "format_info": {
    "shapefile": {
      "base_name": "farmland",
      "ref_extensions": ["dbf", "prj", "shp", "shx"],
      "has_prj": true,
      "has_cpg": false,
      "shape_type": "Polygon"
    }
  },
  "capabilities": {
    "spatial": {
      "geometry_columns": [
        {
          "name": "geometry",
          "geometry_type": "MultiPolygon",
          "srid": 0,
          "dimension": 2,
          "nullable": false
        }
      ],
      "primary_geometry_column": "geometry",
      "extent": null,
      "has_spatial_index": false
    }
  }
}
```

### ref 读取

Manager 内容读取必须使用 `meta_item.full_name` 作为主内容路径，并使用 `attributes.item.refs` 读取相关内容。Transfer 写出 Shapefile 时必须明确 ref 提交边界，不能只写 `.shp`。

### 格式约束

- 不得把 `.shp` 单独作为完整 Shapefile item。
- 不得把 Shapefile 作为 whole scope detector。
- 不得把 `base_name`、`ref_extensions`、`has_prj`、`has_cpg` 写入 attributes 顶层或长期写入 `format_info.unqualified`。

## Parquet / ORC / Avro / Iceberg

### 识别与组织

| 场景 | `layout` | `data_type` | `format` |
|---|---|---|---|
| 单个 Parquet 文件 | `single` | `table` | `parquet` |
| 一组明确归并的 sibling Parquet 文件 | `multi` | `table` | `parquet` |
| 单个 ORC 文件 | `single` | `table` | `orc` |
| 单个 Avro 文件 | `single` | `table` | `avro` |
| Iceberg 表目录 | `whole` | `table` | `iceberg` |

Parquet、ORC、Avro 是表格型数据的文件格式，不应直接称为“湖表”。一组同类 Parquet 文件只有在有明确ref 规则或 manifest 规则时才能归并为 `multi` item。Iceberg 等表格式目录由规范声明后可作为 `layout=whole` 的 table item。

### attributes 写入

| 分区 | 写入内容 |
|---|---|
| `item` | `layout`、`data_type=table`、`format`、可选 `refs`、whole scope 的 `scope_exclusive` 和 `claim_policy` |
| `type_info.table` | 字段、原始字段类型、行数或估算行数、`native.partition_columns` |
| `format_info.<format>` | 文件 footer、编码、压缩、row group、schema 版本、manifest 摘要、scope 文件清单等格式私有信息 |
| `capabilities.partitioning` | 分区数量、分区样例、分区范围等画像能力 |
| `capabilities.statistics` | 可轻量获得的列统计、采样统计 |

`whole` item 的范围由 `meta_item.full_name` 表达，`item.scope_exclusive=true`、`item.claim_policy=whole_scope` 表达独占语义。`refs` 只包含规范认定的数据文件或 manifest 关键资源，不包含 `_SUCCESS`、`_metadata`、`_common_metadata`、CRC 等辅助文件，除非具体格式规范另有说明。

### 表格读取

上层统一按 `data_type=table` 消费。单文件表、multi 文件表、scope 表和引擎原生表的读取差异由 contentio 抽象和 format provider 收口：元信息走 `TableInfoProvider` / `MultiTableInfoProvider` / `ScopeTableInfoProvider`，预览探查走 `TableSampleReader` / `MultiTableSampleReader` / `ScopeTableSampleReader`，Transfer 全量读写走 `TableReaderProvider` / `MultiTableReaderProvider` / `ScopeTableReaderProvider` / writer provider。不向 Manager / Transfer 暴露 `filetable` / `laketable` 两套业务概念。

### 格式约束

- 不得把多个独立 sibling Parquet 文件误合成一个 `whole` item。
- 不得把 Parquet 直接叫作湖表。
- 不得在 Manager 中按目录临时拼装 scope 表；whole scope 必须由 Meta 已入库 item 表达。

## SQLite / GeoPackage

### 识别与组织

| 格式 | `layout` | `data_type` | `format` | 主资源 |
|---|---|---|---|---|
| SQLite | `single` | `container` | `sqlite` | SQLite 文件 |
| GeoPackage | `single` | `container` | `geopackage` | GeoPackage 文件 |

容器文件本身先作为一个 item；`meta_item.full_name` 指向容器文件。内部表、view、图层等先写入 `type_info.container.children`。

### attributes 写入

| 分区 | SQLite | GeoPackage |
|---|---|---|
| `item` | `layout`、`data_type`、`format` | `layout`、`data_type`、`format` |
| `type_info.container` | 内部表、view、默认入口、对象数量 | 内部 layer、表、默认入口、对象数量 |
| `format_info.sqlite` | SQLite 版本、内部表数量、表清单、pragma 摘要 | 不适用 |
| `format_info.geopackage` | 不适用 | gpkg 容器级元数据和 layer / table 统计摘要 |
| `capabilities.spatial` | 仅 SpatiaLite 等可确认空间能力时写入 | 外层容器不写入；选中具体 layer 后由 child `TableInfo.SpatialInfo` 表达空间字段、SRID / CRS、extent 和空间索引 |

当内部表、view 或 layer 被归一为 table describe 结果时，SQLite `sqlite_master.type` 只映射到 `type_info.table.kind`。当前不为 SQLite / GeoPackage 增加表级 native key；page size、page count、内部表 / 视图 / 索引数量等是容器或文件级事实，继续留在 `format_info.sqlite/geopackage`。

### 内部读取

Manager 展示容器 children 时消费 `type_info.container`；进入内部表或 layer 预览时，由容器读取能力定位内部对象，再交给 table info / sample reader 和 spatial 横切能力处理。

### 格式约束

- 不得把容器内所有表字段合并成外层 item 的 `type_info.table.fields`。
- 不得把容器内单个表或 layer 的字段、样本行、空间字段、SRID、extent、空间索引等 child 内容写入外层容器 attributes。
- 不得把 GeoPackage 的格式私有元数据混入 `capabilities.spatial`。

## ZIP / 压缩包

### 识别与组织

| 维度 | 取值 |
|---|---|
| `layout` | `single` |
| `data_type` | `container` |
| `format` | `zip` |
| 主资源 | `meta_item.full_name` 指向 ZIP 文件 |

ZIP 压缩包先作为一个容器 item。压缩包内部 entry 是内部子对象，不自动展开为独立 `meta_item`。

### attributes 写入

| 分区 | 写入内容 |
|---|---|
| `item` | `layout`、`data_type`、`format` |
| `type_info.container` | entry 轻量 `children`、`default_child`、`child_count` |
| `format_info.zip` | `entry_count`、`file_count`、`directory_count`、`sampled_children`、`children_truncated` 等容器统计 |

`type_info.container.children` 只能记录 entry 定位和摘要，例如 `name`、`kind`、`data_type`、`path`、压缩前后大小、压缩方法和可推断的 child `format`。不得把 entry 的字段、行样本、文档正文或媒体内容写入父容器。

### 内部读取

Manager 展示 ZIP 容器时消费 `type_info.container`。进入某个普通文件 entry 的内容预览时，由 `ContainerChildResolver` 把 entry 解析为 stream child resource，再交给对应 data type 的 info provider / content reader 处理；不得在 Manager 或 Meta 中为 ZIP 单独解压并绕过通用链路。

### 格式约束

- 不得把压缩包内文件内容、字段数组、行样本或正文片段写入外层容器 attributes。
- 不得把 ZIP 内部文件的格式识别结果提升为父容器 `format`。
- RAR、TAR 等其他压缩格式进入内置主线前，应先明确 descriptor、MIME、解包依赖和 entry 读取边界。

## 图片

### 识别与组织

| 维度 | 取值 |
|---|---|
| `layout` | `single` |
| `data_type` | `media` |
| `format` | `jpeg`、`png`、`gif`、`tiff`、`image` |
| 主资源 | `meta_item.full_name` 指向图片文件 |

`image` 是图片兜底格式，只在无法稳定识别具体图片格式时使用。JPEG、PNG、GIF、TIFF 等具体格式应优先写入具体 `format`。GeoTIFF 不新增独立基础格式，表达为 `format=tiff + capabilities.spatial`。COG 是 TIFF 的云优化 profile，不新增 `format=cog`。

TIFF / GeoTIFF 如果存在 `.tfw`、`.tifw`、`.wld`、`.prj`、`.aux.xml`、`.ovr`、`.hdr` 等同 basename sidecar，应按 `layout=multi` 归并为一个 item，primary content 仍为 `.tif` / `.tiff`。如果没有 sidecar，则按普通 `layout=single` 图片 item 处理。具体 ref 白名单见 [ADDP 数据项探测器规范](addp数据项探测器规范.md)。

WebP、BMP、SVG、AVIF、HEIC / HEIF 进入内置主线前，应先明确 descriptor、MIME、预览方式和后端解析边界；在仅能 raw / range 预览时，不应标记为后端已经具备完整 `MediaInfoProvider`。

### attributes 写入

| 分区 | 写入内容 |
|---|---|
| `item` | `layout`、`data_type`、`format`；TIFF sidecar multi item 还应写入 `refs` |
| `type_info.media` | `kind=image`、`mime_type`、宽高、`encoding`、`color_space`、`size_bytes` 等媒体信息 |
| `format_info.<format>` | EXIF、TIFF tag、压缩方式等格式私有信息；TIFF 使用 `format_info.tiff.profile` 表达 `plain_tiff` / `geotiff` / `cog` |
| `capabilities.spatial` | 图片 GPS 或 GeoTIFF 可确定空间信息 |

### 预览读取

图片预览面向 `data_type=media`。如果图片包含 GPS 或 GeoTIFF 空间信息，可以额外启用空间能力展示，但图片本身仍是 `media` 类型。

GIF、WebP、TIFF 等多帧或多页图片仍表达为 `kind=image`。动图播放、帧数、页数、首帧缩略图等属于媒体信息或内容读取能力，不应改写为 `kind=video`。

大图、GeoTIFF、多页 TIFF 等不应依赖全量 base64 作为首屏预览。Manager 应优先使用 raw / range URL、缩略图、降采样或切片能力；后端是否生成缩略图由 `MediaThumbnailReader` 或后续媒体读取能力声明。

### 格式约束

- 不得给图片写入 `type_info.table.fields`。
- 不得把所有图片都视为空间数据。
- 不得把 GeoTIFF 表达为新的基础数据类型。
- 不得把 COG 表达为新的基础 `format`；COG 只能作为 `format_info.tiff.profile=cog` 或 Manager COG 生成结果表达。

## Raster mosaic

### 识别与组织

| 维度 | 取值 |
|---|---|
| `layout` | `whole` |
| `data_type` | `media` |
| `format` | `raster_mosaic` |
| 主资源 | whole scope 根目录或 prefix，`mosaic.addp.json` 是 manifest，不是 `full_name` |

`raster_mosaic` 是由 ADDP mosaic 生成任务产生的栅格镶嵌数据集。内置 descriptor 使用确定性文件名 `mosaic.addp.json` 作为候选识别事实，不声明 `.json` 扩展名，避免和普通 JSON 冲突。Meta 落库前必须读取 manifest 内容确认 schema、`format=raster_mosaic`、`data_type=media` 和 `layout=whole`，不得仅根据文件名或 leaf COG 后缀判定。

### attributes 写入

| 分区 | 写入内容 |
|---|---|
| `item` | `layout=whole`、`data_type=media`、`format=raster_mosaic`、`scope_exclusive=true`、`claim_policy=whole_scope` |
| `storage` | whole scope 根范围的 `physical_path`、总大小等存储事实 |
| `format_info.raster_mosaic` | `manifest_ref`、`index_ref`、`overview_ref`、leaf 数量、COG 校验摘要等格式私有事实 |
| `capabilities.spatial` | mosaic 的 CRS、extent、分辨率范围等空间能力 |

whole item 不应把全部 leaf COG 展开写入 `item.refs`。leaf COG、overview、index、tiles、stats、styles 等都是 mosaic item 内部组成；是否展示单个 leaf COG 由 Manager 读取 manifest/index 后提供内部查看能力。

### 消费要求

Manager 预览应消费已入库的 `format=raster_mosaic` item，读取业务存储中的 manifest、index、overview COG 和 leaf COG window。常规在线预览不得重新调用 Python Workflow；重建 overview、重建 leaf COG、重算 stats 或重建 tile cache 应进入任务体系。

## 视频

### 识别与组织

| 维度 | 取值 |
|---|---|
| `layout` | `single` |
| `data_type` | `media` |
| `format` | `mp4`、`mov`、`mkv`、`avi`、`webm`、`video` |
| 主资源 | `meta_item.full_name` 指向视频文件 |

`format` 表达视频文件或容器格式。H.264、H.265、AV1、VP9、AAC、Opus 等编码不作为基础 `format`，也不作为当前 `MediaInfo` 主事实；如需保留，应进入受控 `format_info.<format>` 或后续媒体提取结果。

`video` 是兜底格式，只在无法稳定识别具体视频容器时使用。第一阶段视频格式目标是稳定识别、记录轻量元信息，并支持 raw / range / stream 播放链路，不要求后端转码。

### attributes 写入

| 分区 | 写入内容 |
|---|---|
| `item` | `layout`、`data_type`、`format` |
| `type_info.media` | `kind=video`、`mime_type`、宽高、`duration_ms`、`encoding`、`size_bytes` 等当前通用媒体信息 |
| `format_info.<format>` | 容器版本、轨道摘要、metadata atom、字幕轨、封面帧等格式私有信息 |
| `capabilities.extraction` | 仅在已有明确抽帧、OCR、语音转写或字幕提取任务状态时写入 |

### 预览读取

视频预览面向 `data_type=media + type_info.media.kind=video`。Manager 应优先使用 range / stream URL 播放，不应通过后端全量 base64 返回视频内容。转码、抽帧、封面图、字幕提取和语音转写属于后续媒体处理能力，不是格式识别的前置条件。

Search 或语义索引可消费 `capabilities.extraction` 或外部索引引用，但不应把抽帧结果、完整字幕或语音转写全文直接塞入 `attributes`。

### 格式约束

- 不得把视频编码当作基础 `format`。
- 不得把视频写入 `type_info.document` 或 `type_info.table`。
- 不得因为视频包含音轨就拆成多个基础 data item。

## 音频

### 识别与组织

| 维度 | 取值 |
|---|---|
| `layout` | `single` |
| `data_type` | `media` |
| `format` | `mp3`、`wav`、`flac`、`aac`、`ogg`、`audio` |
| 主资源 | `meta_item.full_name` 指向音频文件 |

`audio` 是兜底格式，只在无法稳定识别具体音频格式时使用。第一阶段音频格式目标是稳定识别、记录轻量元信息，并支持 raw / range / stream 播放链路。

### attributes 写入

| 分区 | 写入内容 |
|---|---|
| `item` | `layout`、`data_type`、`format` |
| `type_info.media` | `kind=audio`、`mime_type`、`duration_ms`、`encoding`、`size_bytes` 等当前通用媒体信息 |
| `format_info.<format>` | ID3 / Vorbis comment / RIFF chunk / 封面图等格式私有信息 |
| `capabilities.extraction` | 仅在已有明确语音转写、音乐识别或摘要任务状态时写入 |

### 预览读取

音频预览面向 `data_type=media + type_info.media.kind=audio`。Manager 应优先使用 range / stream URL 播放；语音转写、摘要、声纹、音乐识别等属于后续提取或语义能力，不是音频格式识别的前置条件。

### 格式约束

- 不得把音频写入 `type_info.document`。
- 歌词、转写全文和封面大图必须作为提取结果或内容读取结果管理，不进入基础 attributes。

## PDF

### 识别与组织

| 维度 | 取值 |
|---|---|
| `layout` | `single` |
| `data_type` | `document` |
| `format` | `pdf` |
| 主资源 | `meta_item.full_name` 指向 PDF 文件 |

### attributes 写入

| 分区 | 写入内容 |
|---|---|
| `item` | `layout`、`data_type`、`format` |
| `type_info.document` | 页数、标题、语言、编码、字数、大小等通用文档结构信息 |
| `format_info.pdf` | PDF 版本、author、subject、creator、producer、加密状态、读取限制、字体、页面结构等格式私有信息 |
| `capabilities.extraction` | 文本提取状态、OCR 状态、文本片段、摘要、外部索引引用 |

### 文档读取

Manager 文档内容读取消费 `type_info.document` 和 `capabilities.extraction`。全文索引或大文本内容应通过外部索引引用或提取任务管理，不直接塞入 attributes。

### 格式约束

- 不得给 PDF 写入 `type_info.table.fields`。
- 不得把 PDF 文档提取状态写入 `format_info.pdf`。

## DOCX / PPTX / WPS

### 识别与组织

| 维度 | DOCX | PPTX | WPS |
|---|---|---|---|
| `layout` | `single` | `single` | `single` |
| `data_type` | `document` | `document` | `document` |
| `format` | `docx` | `pptx` | `wps` |
| 主资源 | `meta_item.full_name` 指向 DOCX 文件 | `meta_item.full_name` 指向 PPTX 文件 | `meta_item.full_name` 指向 WPS 文件 |

DOCX / PPTX / WPS 是单资源文档文件。内置规范要求稳定识别格式，并让 Manager 通过 engine / contentio / storage-stream 等内容通道消费原始文件流。DOCX / PPTX 可以实现轻量 `DocumentTextReader` 进入全文检索链路；WPS 格式变体较多，未实现可靠 reader 时不声明后端解析能力，并在 deep scan 中记录不可抽取状态。

### attributes 写入

| 分区 | 写入内容 |
|---|---|
| `item` | `layout`、`data_type`、`format` |
| `type_info.document` | 仅在后端已有确定解析事实时写入页数、标题、语言、编码、字数、大小等通用文档结构信息；没有解析事实时不得写入空壳对象 |
| `format_info.docx` / `format_info.pptx` / `format_info.wps` | 仅在后端已有确定解析事实时写入格式私有信息 |
| `capabilities.extraction` | 写入文本提取、转换、OCR、摘要或外部索引任务状态；没有后端 reader 时应明确记录 `status=unsupported` 和原因 |

### 预览读取

Manager 文档预览应优先消费 `frontend_renderer`、`preview_material`、`content.kind` 等后端语义字段，并优先使用 raw / range / storage-stream URL 读取存储叶子内容；扩展名和 MIME 只作为兜底识别依据。没有 URL 时才允许在受限大小内使用 `raw_binary` + base64 兜底。

`preview_material` 是 Manager 面向前端的展示材料或展示状态协议，取值如 `url`、`raw_binary`、`text`、`json`、`markdown`、`geojson`、`table`、`container`、`unsupported`。它不等同于 `common/format` 的 `content_readers` 声明；不得把 `raw_content`、`range_content`、`binary_content` 等 descriptor 能力名称写入 `preview_material`。

`frontend_renderer` 是 Manager 对前端渲染组件的建议，前端选择预览组件时应按 `frontend_renderer`、`preview_material`、`content.kind` 的顺序兜底。`content.kind` 表示内容的大类，不能替代展示材料协议；例如 `content.kind=json` 且 `preview_material=geojson`、`frontend_renderer=map` 时，应按地图预览处理。

GeoJSON 虽然是 `data_type=table`，但对象内容预览应优先生成 GeoJSON / map 材料，而不是被通用表格内容 handler 抢占。通用表格预览仍可服务 CSV、Parquet、普通 JSON records 等表格材料；GeoJSON map 预览由 Manager 根据 `format=geojson` 或内容探测后的 GeoJSON 事实生成 `preview_material=geojson`。

推荐组合如下：

| 内容语义 | `content.kind` | `preview_material` | `frontend_renderer` |
|---|---|---|---|
| 普通文本 | `text` | `text` | `text` |
| Markdown | `markdown` | `markdown` | `markdown` |
| 普通 JSON | `json` | `json` | `json` |
| GeoJSON / 空间 JSON | `json` | `geojson` | `map` |
| 图片 URL 预览 | `image` | `url` | `image` |
| 视频 URL 预览 | `video` | `url` | `video` |
| PDF / DOCX / PPTX / WPS URL 预览 | 对应格式名 | `url` | 对应格式名 |
| GLB 三维模型 URL 预览 | `model_3d` | `url` | `model_3d` |
| 3D Tiles 分块三维场景 URL 预览 | `model_3d` | `url` | `3dtiles` |
| LAS 点云抽样预览 | `point_cloud` | `json` | `point_cloud` |
| 高斯泼溅 URL 预览 | `gaussian_splat` | `url` | `gaussian_splat` |
| 小体积原始二进制兜底 | 对应格式名 | `raw_binary` | 对应格式名 |
| 表格材料 | `table` | `table` | `table` |
| 容器索引 / 子对象导航 | `container` | `container` | `container` |
| 不支持在线预览状态 | `unsupported` | `unsupported` | `unsupported` |

当后端没有任何可展示材料（无 `text`、`json`、`geojson`、`data`、`url` 等）时，不应返回 `truncated=true`，避免前端提示“仅展示部分”但实际没有内容。若格式不可在线预览，应返回明确说明文本，并让用户通过下载查看。

unknown 二进制仍应保留底层 binary 读取能力，供后续计算端或专业解析引擎使用；但 Manager 不认识该格式时，不应把二进制探测样本当作前端预览材料，也不应把“不支持在线预览”的提示文案伪装成 text 材料。此时应返回 `preview_material=unsupported`、`frontend_renderer=unsupported`，并可在 metadata 中记录 `binary_probe`、`probe_truncated` 等探测事实。

容器子项、组合文件相关文件和 ref preview hint 同样不得把未知 document / media 标记为 `raw_binary`。只有 Manager 已有明确 renderer 的格式（如 PDF、DOCX、PPTX、WPS、图片、视频）才能用 `raw_binary` 作为预览材料提示；未知格式应使用 `unsupported`。

Transfer、Search 等模块不得因为 `data_type=document` 就假设存在可搜索全文；全文、缩略图、转换产物和摘要必须来自后续提取或转换任务，并通过 `capabilities.extraction` 或外部索引引用管理。

### 格式约束

- 不得把 DOCX / PPTX / WPS 归为 unknown binary。
- 不得给 DOCX / PPTX / WPS 写入 `type_info.table.fields`。
- 不得在没有后端解析事实时虚报 `type_info.document`、`DocumentInfoProvider` 或 `DocumentTextReader` 能力。
- 不得为了 Manager 预览默认全量读取大文档并返回 base64。
