# ADDP 内置数据类型与文件格式规范

本文定义 ADDP 首批内置数据类型与文件格式的确定性落地规则。概念边界见 [ADDP 数据类型和格式体系图](../concepts/addp数据类型和格式体系图.md)，item 识别规则见 [ADDP 数据项探测器规范](addp数据项探测器规范.md)，attributes 写入规则见 [ADDP 元数据 attributes 规范](addp元数据attributes规范.md)。

本文只记录已经形成规范共识的格式。尚未定稿的格式、插件 manifest、whole scope explain / confidence 等问题，分别进入 `docs/plan/` 下的对应构想文档或后续事项文档，不再依赖 `docs/next/` 里的公共待规范页。

代码实现中，内置格式的静态身份声明由各格式包自己的 `Descriptor()` 维护，位置为 `common/format/plugins/<format>/`；统一加载入口为 `common/format/builtin/init.go`。本文是规范语义来源，代码中的 descriptor 应与本文保持一致；`common/format` 根包承担运行时注册、能力发现和冲突诊断，不再维护集中式内置 descriptor 清单。

## 专题导航

按阅读主题分组；格式身份、`data_type`、`layout` 和能力边界以各格式正文为准。

| 主题 | 格式与内容 |
| --- | --- |
| [CAD](builtin-formats/CAD.md) | DWG、DXF |
| [三维模型与场景](builtin-formats/三维模型与场景.md) | GLB、glTF、OBJ、STL、FBX、IFC、3D Tiles、S3M、MAX、SKP、SGM、OSGB、OSGB Scene、DAE / 3DS |
| [PLY 与高斯泼溅](builtin-formats/PLY与高斯泼溅.md) | PLY 内容分类、SPLAT、KSPLAT |
| [点云](builtin-formats/点云.md) | LAS、LAZ、COPC、E57、PCD、XYZ、EPT |
| [表格与矢量交换](builtin-formats/表格与矢量交换.md) | CSV / TSV、JSON、GeoJSON、Shapefile、KML / GML / TopoJSON |
| [列式文件与数据容器](builtin-formats/列式文件与数据容器.md) | Excel、Parquet / ORC / Avro / Iceberg、SQLite / GeoPackage、FileGDB / PGeo、ZIP |
| [媒体与地图数据集](builtin-formats/媒体与地图数据集.md) | 图片、JPEG EXIF / GPS、GIF / WebP 动画、TIFF 分页、PMTiles、Raster mosaic、视频、音频 |
| [文档](builtin-formats/文档.md) | PDF、DOC / DOCX / RTF / PPTX / WPS |

跨格式的预览材料与组件协议见[Manager 数据预览语义协议](../../manager/docs/数据预览语义协议.md)。

## 编写模板

新增内置格式时，在对应专题中按以下结构补充，并同步本页总览；新增阅读主题时同步专题导航。

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
| 单个 Parquet / GeoParquet | `single` | `table` | `parquet` | 单文件表；GeoParquet 仍使用 `format=parquet`，空间语义进入 `capabilities.spatial` |
| sibling Parquet 文件组 | `multi` | `table` | `parquet` | 仅在有明确 ref 或 manifest 规则时成立 |
| ORC / Avro 单文件 | `single` | `table` | `orc` / `avro` | 单文件表 |
| Iceberg 表目录 | `whole` | `table` | `iceberg` | 整体表目录，需 whole scope 规则 |
| SQLite | `single` | `container` | `sqlite` | 内部表先写入 `type_info.container.children` |
| GeoPackage | `single` | `container` | `geopackage` | 内部 layer 先写入 `type_info.container.children` |
| File Geodatabase | `whole` | `container` | `filegdb` | `.gdb` 目录整体构成容器，内部 feature class / table 为 child |
| Personal Geodatabase | `single` | `container` | `pgeo` | `.mdb` 单文件容器，只允许作为只读 source |
| ZIP | `single` | `container` | `zip` | 压缩包 entry 先写入 `type_info.container.children` |
| 图片 | `single` 或 TIFF sidecar `multi` | `media` | `jpeg` / `png` / `gif` / `tiff` / `webp` / `bmp` / `image` | GPS 或 GeoTIFF 空间语义进入 spatial |
| Raster mosaic | `whole` | `media` | `raster_mosaic` | 由 `mosaic.addp.json` manifest 声明的栅格镶嵌数据集 |
| ERDAS Imagine IMG | `single` | `media` | `img` | 自包含 HFA；Runtime 深扫确认，快显派生受管 COG；外部 spill / dependent 组件暂不支持 |
| 矢量瓦片集 | `single` | `media` | `pmtiles` | PMTiles v3 单文件；当前内部瓦片编码固定为 MVT |
| 视频 | `single` | `media` | `mp4` / `mov` / `mkv` / `avi` / `webm` / `video` | 第一阶段以元信息和 range / stream 播放为主 |
| 音频 | `single` | `media` | `mp3` / `wav` / `flac` / `aac` / `ogg` / `audio` | 第一阶段以元信息和 range / stream 播放为主 |
| PDF | `single` | `document` | `pdf` | 文档元信息和提取状态分区写入 |
| DOC | `single` | `document` | `doc` | Word 97-2003 二进制文档；浏览器端统一 Office 预览 |
| DOCX | `single` | `document` | `docx` | 第一阶段以内置格式识别和 raw / range 预览为主 |
| RTF | `single` | `document` | `rtf` | 富文本交换文档；浏览器端统一 Office 预览并按 RTF 声明的代码页解码 |
| PPTX | `single` | `document` | `pptx` | 源文档保持 PPTX 身份，在线预览使用受管静态 PDF |
| WPS | `single` | `document` | `wps` | 第一阶段以内置格式识别和 raw / range 预览为主 |
| DWG | `single` | `cad` | `dwg` | 二维 CAD 图纸；scan 读取 header，预览使用浏览器端 LibreDWG/WebGL |
| DXF | `single` | `cad` | `dxf` | 二维 CAD 交换图纸；scan 读取 ASCII/Binary DXF header，预览使用浏览器端 LibreDWG/WebGL |
| GLB | `single` | `model_3d` | `glb` | 第一阶段三维模型代表格式；预览优先走 raw / range / storage stream + 前端 Three.js |
| glTF | `multi` | `model_3d` | `gltf` | 由 `.gltf` manifest 声明的多资源三维模型，buffers / images 作为 related refs |
| OBJ | `single` | `model_3d` | `obj` | Wavefront OBJ 单体网格模型；第一阶段支持识别和轻量摘要 |
| STL | `single` | `model_3d` | `stl` | STL 单体网格模型；支持识别、轻量摘要和 GLB 快显 |
| PLY | `single` | `model_3d` / `point_cloud` / `gaussian_splat` | `ply` | 按 header 区分 mesh、点云与高斯泼溅，写入对应类型的轻量摘要 |
| FBX | `single` | `model_3d` | `fbx` | FBX 单体网格模型；快显通过 GLB artifact 实现 |
| DAE / Collada | `single` | `model_3d` | `dae` | Collada 1.4.0 / 1.4.1；摘要记录版本、单位、上轴、网格和贴图引用；静态模型经 Assimp 生成 GLB 快显 |
| 3DS | `single` | `model_3d` | `3ds` | 3DS chunk 模型；摘要记录网格、顶点、三角面、材质和贴图引用；静态模型经 Assimp 生成 GLB 快显 |
| SKP | `single` | `model_3d` | `skp` | SketchUp 原生文件；格式身份识别，静态网格经 Model3D 生成自包含 GLB 快显 |
| MAX | `single` | `model_3d` | `max` | 3ds Max 原生场景；格式身份识别，支持范围内的静态网格经 Model3D 生成 GLB 快显 |
| IFC | `single` | `model_3d` | `ifc` | IFC BIM 模型；第一阶段支持识别和轻量 BIM 摘要 |
| 3D Tiles | `whole` | `model_3d` | `3dtiles` | 由 `tileset.json` manifest 声明的分块三维场景 |
| OSGB | `single` | `model_3d` | `osgb` | 单个 `.osgb` 三维模型文件；快显通过 GLB artifact 实现 |
| OSGB Scene | `whole` | `model_3d` | `osgb_scene` | 由 `metadata.xml` manifest 声明的一套 OSGB 倾斜摄影三维模型场景 |
| LAS | `single` | `point_cloud` | `las` | LAS 原始点云格式；deep scan 读取 header 和轻量摘要，预算内可做 JSON 小样本基础预览，可视化主线转 COPC |
| LAZ | `single` | `point_cloud` | `laz` | LAS 压缩点云格式；deep scan 读取 LAS-family header 和轻量摘要，不复用 `format=las` |
| COPC | `single` | `point_cloud` | `copc` | Cloud Optimized Point Cloud；第一阶段读取 LAS-family header 摘要，作为分块点云主线格式接入 |
| E57 | `single` | `point_cloud` | `e57` | ASTM E57 单文件扫描点云；deep scan 读取 header 和预算内 XML 轻量摘要 |
| PCD | `single` | `point_cloud` | `pcd` | Point Cloud Data 单文件点云；deep scan 读取 PCD header 轻量摘要 |
| XYZ | `single` | `point_cloud` | `xyz` | 文本 XYZ 点云；deep scan 做预算内采样摘要 |
| EPT | `whole` | `point_cloud` | `ept` | Entwine Point Tiles whole-scope 点云数据集；`ept.json` manifest 强命中 |

## 原生键值数据集

Redis 的连接指定逻辑数据库识别为单个 `item_type=keyspace`、`data_type=key_value`、`layout=single` 数据项。没有统一表字段，不声明 `format=redis`；值结构由原生内容读取提供。空键空间也可识别，单个键不登记 Meta item，原始字节、原生类型、长度和 TTL 不落库。详见键值数据集路径、attributes 和 Engine Provider 规范。
