# ADDP 数据类型和格式体系图

本文从概念层说明 ADDP 的数据类型、文件格式、格式插件和横切能力。数据项、资源链条和模块职责边界见 [ADDP 数据项体系图](addp数据项体系图.md)。

## 核心结论

数据类型比文件格式更高层：

- `data type` 回答“这个 data item 在用户观感和平台处理上是什么”。
- `format` 回答“这个 data item 或资源使用什么格式编码”。

二者是多对多语义中的常见一对多落点：

- 一个数据类型可以由多种格式承载，例如 `table` 可以来自 CSV、TSV、Parquet、Shapefile、GeoJSON、数据库表、JSON 记录集合。
- 一个格式也可能根据内容结构落到不同数据类型，例如 JSON 可以是 table、document 或 container。
- 数据库表、动态 schema 记录集合、graph 等引擎原生 item 可以没有文件格式，但仍必须有数据类型。

## 数据类型

数据类型是对一类 data item 的高层抽象。它们通常具备相似的数据特征、内容读取方式、处理手段和治理方式。

ADDP 只维护一套稳定的数据类型和类型信息语义。各模块不得按自己的展示、扫描或读写习惯重新发明平行的数据类型边界；具体代码结构、接口契约和字段落点见规范层文档。

第一版基础数据类型如下：

| 数据类型 | 含义 | 典型处理方式 |
|---|---|---|
| `table` | 有字段、行列或可推断字段的结构化数据 | 字段信息、表格样本、批量读写、统计分析 |
| `document` | 以阅读、解析和全文提取为主的数据 | 文档信息、文本片段、全文索引、摘要 |
| `media` | 图片、视频、音频等可感知媒体内容 | 媒体信息、原始内容、缩略图、播放、转码 |
| `container` | 内部包含子对象或子资源的数据 | 内部对象枚举、默认入口、子对象读取 |
| `graph` | 节点、边、关系结构的数据 | 图结构信息、关系查询、子图样本 |
| `model_3d` | 三维空间对象、网格、场景、构件或倾斜摄影模型 | 模型结构信息、三维预览、空间定位、LOD 或构件摘要 |
| `point_cloud` | 三维点集合及其点属性、空间范围和抽样结构 | 点云信息、抽样预览、空间定位、LOD / 分块读取 |
| `gaussian_splat` | 三维高斯泼溅场景数据 | 高斯基元信息、高斯泼溅预览、压缩或派生 splat 产物 |
| `unknown` | 暂未识别或暂不接入的数据 | 基础存储信息、原始内容或下载 |

### 当前支持汇总

本表按当前内置实现汇总数据类型、引擎和文件格式的关系。引擎侧事实来自内置 engine plugin 的能力声明；格式侧事实来自 `common/format/builtin` 加载的内置 `FormatDescriptor` 和已接入的容器 child 解析能力。

表中的“引擎支持”分两类：数据库、动态 schema 和图引擎可以产生原生 data item；对象存储和文件存储引擎只提供文件 / 对象内容承载，最终 data type 仍由格式识别、格式 provider 和扫描结果决定。Python Workflow、Spark Workflow、Math Workflow、Jupyter 等计算 / 脚本引擎不作为 data item 的原生存储来源列入本表。

| 数据类型 | 当前支持的原生 / 承载引擎 | 当前内置文件格式或容器子格式 | 说明 |
|---|---|---|---|
| `table` | 原生表格引擎：PostgreSQL、MySQL、Doris、ClickHouse、Spark SQL。动态 schema 引擎：MongoDB collection。文件 / 对象承载：NFS、S3、MinIO。 | `csv`、`tsv`、records `json` / JSON Lines、`geojson`、`shapefile`、`parquet`、`orc`、`avro`。容器 child 可归一为 table：Excel sheet、SQLite table / view、GeoPackage layer / table、ZIP 内部表格文件。 | 表格数据可以来自引擎原生 catalog leaf，也可以来自文件格式。空间语义通过 `capabilities.spatial` 表达，不新增空间表 data type。Iceberg 属于规范层 whole table 示例，当前未作为内置 format descriptor 注册。 |
| `document` | 文件 / 对象承载：NFS、S3、MinIO。当前没有专用原生 document catalog 引擎。 | `pdf`、`docx`、`pptx`、`wps`、`text`、`markdown`、文档型 `json`。ZIP 内部文档文件可作为 container child 被识别。 | MongoDB query 可以返回 document 形态结果，但 MongoDB collection data item 在当前语义中仍按动态 schema 记录集合归为 `table`。 |
| `media` | 文件 / 对象承载：NFS、S3、MinIO。当前没有专用原生 media catalog 引擎。 | 图片：`image`、`jpeg`、`png`、`gif`、`tiff`、`webp`、`bmp`、`svg`、`avif`、`heic`。栅格数据集：`raster_mosaic`。视频：`video`、`mp4`、`mov`、`mkv`、`avi`、`webm`。音频：`audio`、`mp3`、`wav`、`flac`、`aac`、`ogg`。ZIP 内部媒体文件可作为 container child 被识别。 | `jpeg`、`png`、`gif`、`tiff`、`image` 当前有图片媒体信息 provider；`raster_mosaic` 表示由 manifest、index、leaf COG 和 overview COG 组成的 whole-scope 栅格镶嵌数据集；其他媒体格式当前主要提供格式身份、MIME / 扩展名识别和 raw / range / stream 内容承载。 |
| `container` | 文件 / 对象承载：NFS、S3、MinIO。当前没有专用原生 container catalog 引擎；目录、prefix、bucket 只是 catalog / storage 形态，不是 `container` data type。 | `excel`、`sqlite`、`geopackage`、`zip`。 | 容器 item 先记录轻量 children；进入某个 child 后，再按 child 自身格式归一为 `table`、`document`、`media`、`unknown` 等类型。JSON 作为 container 仍是概念可表达方向，当前内置 JSON plugin 未提供容器信息 provider。 |
| `graph` | 原生图引擎：Neo4j。 | 当前没有内置 graph 文件格式 descriptor。 | RDF、GraphML、GEXF、图结构 JSON 仍是概念层典型来源；进入内置主线前需要先补 format descriptor、provider 和扫描规则。 |
| `model_3d` | 文件 / 对象承载：NFS、S3、MinIO。当前没有专用原生三维模型 catalog 引擎。 | 当前内置：`glb`、`gltf`、`obj`、`stl`、`fbx`、网格型 `ply`、`osgb`、`osgb_scene`、`3dtiles`。后续扩展：`ifc`、`rvt`、`3mx`、`slpk`。 | GLB / glTF、OBJ / STL / FBX、PLY mesh、单 OSGB、OSGB 倾斜摄影场景、3D Tiles、IFC / Revit BIM 都归入 `model_3d`；网格场景、倾斜摄影、BIM、分块场景等子形态由 `type_info.model_3d.model_kind` 表达，不新增平行 data type。OSGB scene 源 item 先做 Meta scan，预览通过转换后的 3D Tiles item 复用链路；OBJ / STL / FBX 可通过转换后的 GLB artifact 快显。 |
| `point_cloud` | 文件 / 对象承载：NFS、S3、MinIO。当前没有专用原生点云 catalog 引擎。 | 当前内置：`las`、点云型 `ply`。后续扩展：`laz`、`copc`、`pcd`、`ept`、`potree`、`e57`。 | 点云即使可被展开为 x/y/z 等列，也不默认归为 `table`；点数、点格式、维度、三维包围盒、scale / offset 等进入 `type_info.point_cloud`，空间参考进入 `capabilities.spatial`。 |
| `gaussian_splat` | 文件 / 对象承载：NFS、S3、MinIO。当前没有专用原生高斯泼溅 catalog 引擎。 | 当前内置：3DGS 型 `ply`、`.splat`、`.ksplat`。后续扩展：`.spz`。 | 高斯泼溅虽然通常由 PLY vertex 或 splat record 承载，但每条记录表示可渲染高斯基元，不是普通点云采样点；尺度、旋转、不透明度、球谐颜色等进入 `type_info.gaussian_splat`，格式私有事实进入 `format_info.<format>`。 |
| `unknown` | 文件 / 对象承载：NFS、S3、MinIO；其他存储扫描中无法判断内容语义的叶子也可落为 `unknown`。 | `unknown`。 | `unknown` 是识别失败或暂未接入时的兜底格式 / 数据类型组合；它保留 storage、item 等基础事实和 raw binary 读取能力，不引入 `file` data type。 |

### table

`table` 是所有表格型 data item 的通用数据类型。

典型来源：

- 数据库表。
- CSV / TSV。
- 严格记录集合型 JSON，例如顶层对象数组。
- GeoJSON FeatureCollection。
- Shapefile。
- Parquet / ORC / Avro。
- Iceberg 等目录型表格式。
- MongoDB collection 等动态 schema 的 JSON/BSON 记录集合。

CSV 和 JSON 虽然有文本属性，但只要平台把它们作为行列数据处理，就应归为 `table`。文本属性属于文件格式或读取方式，不应把 CSV 放进 `document`。

MongoDB collection 是动态 schema 的 JSON/BSON 记录集合容器。它既不是关系型数据库表，也不是 PDF / DOCX 这类以阅读和正文提取为主的 document；在 ADDP 当前语义中，它按记录集合归入 `table`，同时保留 collection 作为引擎原生 catalog 术语。具体 attributes 落点、采样画像和索引事实归属见规范层文档。

JSON 默认按 `document` 兜底；只有内容事实能严格证明它是记录集合时才升级为 `table`。当前 JSON 明确支持顶层对象数组和 JSON Lines 记录集合。GeoJSON `FeatureCollection.features` 是独立 `geojson` 格式，不再作为 `json` 格式的空间结构分支。`{"data":[...]}`、`{"rows":[...]}` 等结构是否作为 table，需要先补规范再实现，不能用字段名或习惯做隐式猜测。

JSON / GeoJSON 也不默认具备空间能力。只有实际记录里发现 GeoJSON geometry 结构，或字段值可被严格解析为 WKB / EWKB 几何时，才写入 `capabilities.spatial`。后端只表达 `data_type=table + capabilities.spatial` 这样的横切能力组合，不新增“空间表”数据类型；Manager 前端可以据此选择“表格 + 空间”的渲染方式。

### document

`document` 是以阅读、正文提取和片段检索为主的 data item。

典型来源：

- PDF。
- Word / WPS / RTF / Markdown / 纯文本。
- 配置文件或嵌套 JSON 文档。
- 文档型数据库中的单条阅读型记录。

`document` 只说明用户如何理解和消费该 item，不等于后端已经能完整解析正文。WPS 可以表达为 `data_type=document + format=wps`，即使当前后端只提供 raw content 或 range content。

### media

`media` 是以视觉、音频或视频消费为主的 data item。

典型来源：

- 图片：JPEG / PNG / GIF / TIFF / WebP / BMP / SVG / AVIF / HEIC / GeoTIFF。
- 栅格镶嵌数据集：由 manifest、index、leaf COG 和 overview COG 组成的 raster mosaic。
- 视频：MP4 / MOV / MKV / AVI / WebM 等容器格式。
- 音频：MP3 / WAV / FLAC / AAC / OGG 等音频格式。

图片、视频、音频之间的差异属于媒体类型信息，不新增基础数据类型。

媒体文件的容器格式、编码格式和横切能力应分层表达。视频编码、音频编码、帧率、采样率、码率、轨道数等细粒度事实暂不作为媒体主事实；GeoTIFF、带 GPS 的图片等空间语义也不新增“空间图片”或“视频数据”数据类型。具体字段归属见规范层文档。

### container

`container` 表示 item 内部包含子对象或子资源。

典型来源：

- Excel。
- SQLite。
- GeoPackage。
- ZIP / RAR / TAR。

容器是数据类型，不是内容布局。大多数容器文件外层仍是单资源 item，内部对象作为容器 child 摘要表达。

### graph

`graph` 表示节点、边和关系结构数据。

典型来源：

- Neo4j graph item。
- RDF。
- GraphML / GEXF。
- 图结构 JSON。

图结构既可以来自引擎原生查询，也可以来自文件格式。引擎原生图数据通常不经过文件格式解码。

graph 的核心是节点和关系。Neo4j label、relationship type、RDF class 或采样推断出的结构簇，都是图结构的分组、分类或投影视角，不是比 graph 更底层的独立 data type。平台可以在 Manager 或 Graph 模块中按这些视角展示和筛选，但 graph item 本身仍表示一个可被查询、采样、预览和治理的图整体。

通用 graph 类型信息只描述结构摘要，例如节点形状、关系形状、连接模式、属性结构和计数。实际节点样本、路径探索结果、图算法结果和前端图组件数据属于读取、查询或 Graph 模块能力，不进入 graph 的通用类型信息。

### model_3d

`model_3d` 是所有三维模型型 data item 的通用数据类型。

典型来源：

- GLB / glTF 单体或多资源模型。
- OBJ / STL / PLY 等网格模型。
- 单 OSGB 模型文件。
- OSGB 倾斜摄影场景。
- 3D Tiles 分块三维场景。
- IFC / Revit 等 BIM / 参数化建筑模型。

`model_3d` 不只表示普通网格模型，也覆盖倾斜摄影、BIM 构件模型和分块三维场景。子形态通过 `type_info.model_3d.model_kind` 表达，第一版取值建议为 `mesh_scene`、`photogrammetry_scene`、`bim_model`、`tiled_scene`、`generic`。单个 `.osgb` 文件使用 `format=osgb`，一套 OSGB 倾斜摄影场景使用 `format=osgb_scene`；二者和 Revit / IFC 都不新增独立 data type。它们的格式组织、构件属性、LOD、纹理、空间参考和原生摘要分别进入 layout、`format_info.<format>`、`type_info.model_3d` 和 `capabilities.spatial`。

三维模型的预览数据、转换产物、缩略图、瓦片化结果、构件查询结果不进入 `type_info.model_3d`。Manager 应基于已入库 item 和标准 attributes 选择内容读取或派生预览能力。

### point_cloud

`point_cloud` 是所有点云型 data item 的通用数据类型。

典型来源：

- LAS / LAZ。
- COPC。
- PCD。
- 点云型 PLY。
- XYZ / PTS / PTX 等文本点云。
- EPT / Potree 等 whole-scope 点云数据集。
- E57 等多站扫描集合。

点云虽然可以被展开成 x/y/z、intensity、classification 等列，但用户和平台的主要消费方式是点云抽样、三维预览、空间范围、点属性、LOD 和分块读取，因此不应仅因“可列化”而归为 `table`。点属性摘要进入 `type_info.point_cloud` 或 `capabilities.statistics`；真实点样本、抽稀点集和可视化瓦片属于内容读取或 Manager 派生产物，不写入 attributes。

### gaussian_splat

`gaussian_splat` 是三维高斯泼溅型 data item 的通用数据类型。

典型来源：

- 3D Gaussian Splatting PLY。
- `.splat`。
- `.ksplat`。
- `.spz`。

高斯泼溅与普通点云的共同点是都可以由大量 vertex-like 记录组成；关键区别是高斯泼溅的每个记录不是采样点，而是带尺度、旋转、不透明度和视角相关颜色的可渲染高斯基元。因此它不能归入 `point_cloud`，也不应走 `model_3d` 的 mesh / GLB 转换路线。

PLY 是内容敏感格式：同一个 `format=ply` 可以根据 header 分别落为三种 data type：

- `element face` 数量大于 0：`data_type=model_3d`，`format=ply`，`format_info.ply.layout=mesh`。
- 没有 face，且具备 `x/y/z`、`opacity`、`scale_0..2`、`rot_0..3`、`f_dc_0..2` 或颜色字段：`data_type=gaussian_splat`，`format=ply`，`format_info.ply.layout=gaussian_splat`。
- 没有 face，且不满足高斯泼溅字段组合：`data_type=point_cloud`，`format=ply`，`format_info.ply.layout=point_cloud`。

`.splat` 和 `.ksplat` 单文件按格式身份归为 `data_type=gaussian_splat`、`format=splat|ksplat`，复用高斯泼溅预览路线。

### unknown

`unknown` 用于暂未识别或暂不接入的数据。

`unknown` 不是失败状态，而是平台对资源保持可管理、可检索、可下载的兜底语义。后续探测能力增强后，可以重新扫描并升级为更具体的数据类型。

`file` 不是基础数据类型。文件、对象、目录、bucket、prefix、root 等只表示 catalog / storage 形态；路径、名称、大小、MIME、etag、hash、last_modified 等事实属于 storage 或 catalog 标准字段。无法判断内容语义时，data item 使用 `data_type=unknown`，不得新增 `data_type=file` 或 `type_info.file`。

## 文件格式

文件格式回答 item 或资源的编码方式，例如：

- `csv`、`tsv`
- `json`
- `geojson`
- `parquet`、`orc`、`avro`
- `shapefile`
- `sqlite`、`geopackage`
- `excel`
- `zip`、`rar`
- `pdf`、`wps`
- `jpeg`、`png`、`tiff`
- `glb`、`gltf`、`obj`、`stl`、`osgb`、`osgb_scene`、`3dtiles`、`ifc`
- `las`、`laz`、`copc`、`pcd`、`ept`

文件格式不等于数据类型，也不等于内容布局：

- Shapefile = `data_type=table` + `layout=multi` + `format=shapefile` + `capabilities.spatial`。
- GeoJSON = `data_type=table` + `layout=single` + `format=geojson`，当 feature 实际包含 geometry 时再附加 `capabilities.spatial`。
- GeoTIFF = `data_type=media` + `layout=single` + `format=tiff` + `capabilities.spatial`。
- Raster mosaic = `data_type=media` + `layout=whole` + `format=raster_mosaic` + `capabilities.spatial`。
- Excel = `data_type=container` + `layout=single` + `format=excel`。
- Iceberg = `data_type=table` + `layout=whole` + `format=iceberg`。
- GLB = `data_type=model_3d` + `layout=single` + `format=glb`。
- glTF = `data_type=model_3d` + `layout=multi` + `format=gltf`，`.gltf` manifest 为 primary ref，`buffers[].uri` / `images[].uri` 中命中的本地相对资源作为 related refs。
- OBJ / STL / FBX = `data_type=model_3d` + `layout=single` + 对应 `format`，普通单体网格语义由 `model_kind=mesh_scene` 表达；三者均可生成 GLB 快显。
- PLY mesh = `data_type=model_3d` + `layout=single` + `format=ply`，由 `element face` 判定为网格模型。
- PLY point cloud = `data_type=point_cloud` + `layout=single` + `format=ply`，没有 face 且不满足高斯泼溅字段组合。
- PLY / SPLAT / KSPLAT Gaussian Splat = `data_type=gaussian_splat` + `layout=single` + `format=ply|splat|ksplat`，PLY 由 header 中的高斯基元属性判定，SPLAT / KSPLAT 由格式身份判定，均不转换为 GLB。
- 3D Tiles = `data_type=model_3d` + `layout=whole` + `format=3dtiles`，`tileset.json` 作为 manifest ref，分块场景语义由 `model_kind=tiled_scene` 表达；1.0 / 1.1 版本差异写入 `format_info.3dtiles`，不拆分 data type。
- OSGB = `data_type=model_3d` + `layout=single` + `format=osgb`，表示单个 `.osgb` 文件；浏览器快显通过转换后的 GLB artifact 实现。
- OSGB Scene = `data_type=model_3d` + `layout=whole` + `format=osgb_scene`，表示由 `metadata.xml` 和 `Data/` 组织的一套倾斜摄影场景，倾斜摄影语义可由 `model_kind=photogrammetry_scene` 表达。
- LAS = `data_type=point_cloud` + `layout=single` + `format=las`，CRS 和空间范围进入 `capabilities.spatial`。

## 类型信息与格式信息

`xxx info` 是对应数据类型的通用元数据。每个 data type 只有一类通用 type info：

| 数据类型 | 类型信息示例 |
|---|---|
| `table` | 字段列表、字段类型、主键、行数、大小、表级原生摘要 |
| `document` | 标题、语言、编码、页数、字数、大小 |
| `media` | 媒体种类、MIME、宽高、时长、编码、颜色空间 |
| `container` | child 数量、默认 child、child 轻量摘要、child refs |
| `graph` | node shapes、relationship shapes、连接模式、属性结构、节点数、关系数 |
| `model_3d` | model_kind、mesh / node / material / texture / animation 数量、LOD 数量、三维包围盒、单位、up axis |
| `point_cloud` | point_cloud_kind、点数、点格式、维度列表、三维包围盒、scale / offset、颜色 / intensity / classification 能力 |
| `gaussian_splat` | representation、splat_count、opacity / scale / rotation / spherical harmonics 能力、三维包围盒 |

这些 type info 是结构事实，不是内容数据，也不是格式私有信息。文档正文、表格样本、图片缩略图、原始二进制、视频流、图节点样本等必须通过 content reader、sample reader、query provider 或业务模块结果表达，不写入 `type_info`。

每个 data type 只有一类通用 info。格式、引擎和扫描实现只负责提供事实；具体如何写入 attributes 由规范层定义。

格式信息是某个具体文件格式才有的描述：

| 文件格式 | 格式信息示例 |
|---|---|
| `csv` | delimiter、encoding、has_header、quote_char |
| `shapefile` | base_name、ref_extensions、has_prj、shape_type、dbf_version |
| `json` | structure、encoding、对象层级摘要 |
| `geojson` | structure、feature_count、properties、geometry_types、bbox、crs |
| `sqlite` | sqlite_version、table_count、tables |
| `zip` | compression_method、entry_count、encrypted |

类型信息不等于内容数据。`table info` 描述字段、行数、主键等元数据；表格样本、文档原文片段、图片缩略图、原始二进制内容等属于内容读取能力。

空间、时间、统计、提取、语义、分区、索引等是横切事实，不新增为基础 data type，也不塞进某个 type info。具体落点见元数据 attributes 规范。

内容读取访问索引不是 data type、本体类型信息或格式私有信息；它只描述如何更快定位内容窗口，例如表格稀疏行索引。

## 格式能力

格式能力描述平台能否识别某种内容编码、能否获取对应数据类型信息、能否读取样本或连续内容，以及能否执行写出或传输。格式能力不决定最终 data item 边界；最终 item 由数据项识别和 Meta 扫描上下文共同确认。

格式能力可以覆盖：

- 格式身份：稳定格式 ID、名称、默认数据类型。
- 格式探测：扩展名、MIME、magic bytes、内容签名。
- 布局能力：single / multi / whole、primary ref、related refs 规则、manifest 规则。
- 元信息能力：数据类型信息和格式信息。
- 内容读取能力：样本、文本片段、缩略图、原始内容、范围内容。
- 横切能力事实：spatial、temporal、statistics、extraction 等候选事实。
- transfer 相关能力：批量读写、related refs 写入、提交边界。

格式能力不负责：

- 构造 engine reader。
- 接收 `engine_id` 后反向访问存储。
- 最终决定 data item 边界。
- 直接写 `meta_item.attributes`。
- 返回 Manager 或 Frontend 专用展示协议。

## 格式身份与格式识别

`format identity` 定义“平台支持的这个格式是谁”。它是静态注册事实。

`format detection` 是“给定一个 content，判断它像哪个格式”的动态过程。它输入文件名、MIME、magic bytes、内容签名或 ref 上下文，输出指向某个 format identity 的识别结果。

`format normalization` 是消费已有 format-like 字符串时的归一化过程。识别不到就是 `unknown`，不能把裸后缀或未知字符串当作 format 写入系统语义字段。

| 维度 | Format Identity | Format Detection |
|---|---|---|
| 回答的问题 | 平台支持哪些格式以及这些格式能做什么 | 当前 content 看起来是什么格式 |
| 性质 | 静态注册事实 | 动态识别过程 |
| 输入 | 格式注册信息 | 文件名、MIME、magic bytes、内容片段、ref 上下文 |
| 输出 | 格式身份和能力 | 指向某个 format 的识别结果 |
| 是否决定 item | 不决定 | 不最终决定，只给 Meta detector 提供格式候选 |

Shapefile 这类 multi 格式尤其要区分：单个 `.shp/.dbf/.shx` 的识别不等于 data item 归并；最终 item 边界由 Meta detector 根据 format layout 和候选 content 上下文决定。

未知扩展名文本文件不需要预先注册一个具体 format。当扩展名、MIME、内容签名和 magic bytes 都失败后，可以根据内容前缀的文本特征识别为 `format=text`；没有内容证据时保持 `unknown`。剩余 unknown 非文本内容只能作为 raw binary 兜底，不引入 `binary` data type 或 `binary` format。

## 横切能力

横切能力是不属于单一数据类型、也不属于单一文件格式，但会影响平台处理能力的附加语义。

| 横切能力 | 含义 |
|---|---|
| `spatial` | 空间字段、CRS、extent、geometry type、空间索引 |
| `temporal` | 时间字段、时间范围、时间粒度 |
| `statistics` | 采样统计、空值率、min/max、质量画像 |
| `extraction` | OCR、文本提取、摘要、提取状态 |
| `semantic` | embedding、语义索引、向量表示 |
| `partitioning` | 分区字段、分区范围、分区样例 |
| `indexing` | 空间索引、全文索引、向量索引等能力描述 |

`spatial` 是典型横切能力：

- PostGIS 表 = `data_type=table` + `capabilities.spatial`。
- Shapefile = `data_type=table` + `format=shapefile` + `capabilities.spatial`。
- GeoTIFF = `data_type=media` + `format=tiff` + `capabilities.spatial`。
- Raster mosaic = `data_type=media` + `format=raster_mosaic` + `capabilities.spatial`。

空间能力不应新增为 data type，也不应塞进某个格式私有字段。

## attributes 分层

基于上述概念，data item attributes 分层表达。具体 JSON 字段和写入规则见元数据 attributes 规范。

| 分区 | 回答的问题 | 示例 |
|---|---|---|
| `storage` | 这个 item 在引擎侧的存储和访问属性是什么 | bucket、path、physical_path、size、etag、content_type |
| `item` | 这个 data item 的核心语义是什么 | layout、data_type、format、refs、scope_exclusive |
| `type_info` | 对应数据类型的通用元数据是什么 | table fields、media width/height、document page_count、container children |
| `format_info` | 对应文件、容器或格式解析层面的私有信息是什么 | CSV encoding、Shapefile refs、SQLite version |
| `access_index` | 面向内容读取的通用访问索引是什么 | table sparse_row_index |
| `capabilities` | 这个 item 有哪些横切能力 | spatial、temporal、statistics、extraction |

`meta_item` 表字段仍是 item 身份和归属的事实源。attributes 不重复保存 `id`、`tenant_id`、`engine_id`、`node_id`、`name`、`full_name`、`fingerprint` 等表字段。

文件 / 对象 / 目录没有独立的 file type info；基础事实属于 storage，内容语义属于对应 data type。

## 后续阅读

- [ADDP 术语表](addp术语表.md)
- [ADDP 数据项体系图](addp数据项体系图.md)
- [ADDP 数据项探测器规范](../spec/addp数据项探测器规范.md)
- [ADDP 数据类型与格式能力规范](../spec/addp数据类型与格式能力规范.md)
- [ADDP 内置数据类型与文件格式规范](../spec/addp内置数据类型与文件格式规范.md)
- [ADDP 数据类型与文件格式扩展指南](../spec/addp数据类型与文件格式扩展指南.md)
