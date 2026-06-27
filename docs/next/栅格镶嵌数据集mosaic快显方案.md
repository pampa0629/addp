# 栅格镶嵌数据集（mosaic）方案

更新时间：2026-06-26

状态说明：本文为 `docs/next/` 讨论稿，用于统一“从一批 TIFF 创建 mosaic 数据集，并在 ADDP 中作为业务 item 预览”的概念边界、任务语义和后续落地路径。本文不替代正式 `docs/spec/` 或 Manager 稳定文档；正式实现前，应先把术语、Meta item 边界、任务类型、目标存储和预览协议写入对应规范。

相关文档：

- `docs/concepts/addp数据类型和格式体系图.md`
- `docs/spec/addp数据项探测器规范.md`
- `docs/spec/addp元数据attributes规范.md`
- `docs/next/栅格算子体系后续专题.md`
- `manager/docs/快显概念说明.md`
- `manager/docs/快显实现规范.md`
- `manager/docs/数据预览与资源树实现规范.md`

## 一、核心结论

mosaic 不是 Manager 快显缓存对象，也不是单个超大 COG 文件。

mosaic 是 **ADDP 业务级栅格镶嵌数据集 item**。它由用户从一个源 node 创建，创建时用户必须选择目标业务存储位置。任务完成后，目标业务存储中产生一套 mosaic 数据集文件，Meta 扫描或登记后形成一个新的业务 item。用户后续预览的是这个 mosaic item，而不是几千个源 TIFF 的临时集合。

核心路线：

```text
源 node
  -> 创建 mosaic 任务
  -> 用户选择目标业务存储位置
  -> 任务在目标业务存储中生成 mosaic 数据集
  -> Meta 形成 mosaic 业务 item
  -> Manager 预览 mosaic item
```

关键约束：

1. 创建入口是 **node**，不是任意临时文件列表作为长期对象。
2. 创建结果是 **业务 item**，不是 Manager 私有 artifact。
3. mosaic 数据集内部用于预览和渲染的栅格文件必须是 COG，包括全局 overview COG 和 leaf COG。
4. Manager infra MinIO 不承载 mosaic 数据集、leaf COG、overview 或瓦片等长期结果。
5. 创建任务必须纳入 ADDP 任务体系，任务类型是新的 mosaic 生成任务。
6. Python 引擎需要新增 mosaic 生成能力，承担重型栅格处理。
7. 预览过程尽量不调用 Python 引擎；Manager/common 负责在线预览路径。
8. Manager 只消费 mosaic item 做预览，不拥有 mosaic 数据的生命周期。

## 二、为什么必须是业务 item

几千个 TIFF 不能在 Manager 中“同时预览”为一个长期对象。Manager 的预览入口必须绑定到稳定的 item 身份，否则会出现几个问题：

1. 无法保存预览对象身份。
2. 无法进入 Meta、检索、权限、血缘和治理链路。
3. 无法判断源集合变化后的 stale 关系。
4. 无法在不同用户、不同页面、不同任务之间复用。
5. 无法清楚地区分“源目录”与“生成后的业务数据集”。

因此，mosaic 任务的结果必须生成一个新的业务 item。这个 item 可以由业务存储中的 manifest 文件、目录约定或后续专用 data item detector 识别。

建议目标 item 语义：

```text
data_type = media
format = raster_mosaic
layout = whole
```

### 2.1 data type

mosaic 建议归入 `data_type=media`。

理由：

1. mosaic 的主要消费方式是地图上的栅格视觉浏览，和 GeoTIFF 同属栅格媒体数据。
2. 它没有行列字段和记录语义，不应归入 `table`。
3. 它虽然由目录和多个文件组成，但“目录型组织”是内容布局，不是用户消费意义上的 `container`。
4. 空间能力仍通过 `capabilities.spatial` 表达，不新增空间类 data type。

因此，mosaic 的推荐组合是：

```text
data_type = media
format = raster_mosaic
capabilities.spatial = {...}
```

### 2.2 layout

mosaic 建议归入 `layout=whole`。

理由：

1. mosaic 是一个目录型数据集，`mosaic.addp.json` 是主 manifest，但完整 item 边界是整个数据集目录。
2. `index/`、`overviews/`、`tiles/`、`styles/`、`stats/` 和 leaf COG，都是该数据集内部组成。
3. `layout=multi` 更适合 Shapefile、TIFF + sidecar 这类同 basename sibling refs；mosaic 不是少量 sibling refs，而是一个 whole-scope 数据集。
4. `layout=container` 不存在；container 是 data type。mosaic 不应归为 `data_type=container`。

探测器应以 `mosaic.addp.json` 或后续规范声明的 manifest 规则识别 whole scope，并将 whole scope 根目录作为 `meta_item.full_name`。manifest 路径可以进入 `attributes.item.refs` 或 `format_info.raster_mosaic.manifest_ref`，但不替代 whole scope 根范围。

`raster_mosaic` 是否作为正式内置 format，需要后续先补 `docs/concepts/addp数据类型和格式体系图.md`、`docs/spec/addp数据项探测器规范.md` 和 `docs/spec/addp元数据attributes规范.md`。在规范确认前，不应把 mosaic 硬塞进现有 `tiff` 或 `raster_cog` 概念。

## 三、创建入口

### 3.1 从 node 创建

用户在资源树中选择一个 node，例如：

```text
Business MinIO
  addp/
    images/
      srtm_40_01.tif
      srtm_46_02.tif
      ...
```

在该 node 上执行“创建栅格镶嵌数据集”动作。

选择 node 的意义：

1. node 表示一个业务组织范围，例如 bucket prefix 或 NFS directory。
2. 任务可以在 node 下按规则发现源 TIFF。
3. 任务配置可以记录源 node locator、过滤规则、递归深度和命名策略。
4. 结果 item 仍写入用户选择的目标业务存储，不反写源 node。

### 3.2 用户选择目标业务存储

创建任务时，用户必须选择目标存储位置，例如：

```text
Business MinIO / addp/mosaics/srtm/
NFS /data/mosaics/srtm/
```

目标位置承载完整 mosaic 数据集，包括：

1. 数据集 manifest。
2. mosaic source index。
3. 全局 overview。
4. 可选低层级瓦片。
5. leaf COG。
6. 样式、统计、诊断和任务摘要。

这些都是业务数据集的一部分，不进入 Manager infra MinIO。后续预览只面对 COG leaf 和全局 overview COG；leaf COG 的生成位置由 `placement.mode` 决定：`in_place` 会把原 node 内非 COG TIFF 原地规范化为 COG，文件名保持不变；`detached` 会把所有 leaf COG 写入目标 mosaic 数据集。

## 四、目标数据集结构

建议业务存储中的 mosaic 数据集采用目录型布局：

```text
srtm_mosaic/
  mosaic.addp.json
  index/
    source-index.geojson
    source-index.json
  overviews/
    overview.cog.tif
  tiles/
    z/x/y.webp
  derived/
    leaf-cog/
      <source-id>.cog.tif
  styles/
    default.json
  stats/
    band-1.json
```

其中：

| 对象 | 说明 |
| --- | --- |
| `mosaic.addp.json` | 数据集 manifest，表达 mosaic item 的主身份和组成。 |
| `index/` | 空间索引、源文件引用、范围、优先级、分辨率和 NoData 规则。示例使用 GeoJSON/JSON 只是表达逻辑，不规定必须使用某种物理格式。 |
| `overviews/` | 全局低分辨率概览 COG，不是完整分辨率全局 COG。 |
| `tiles/` | 可选低层级瓦片缓存。 |
| `derived/` | mosaic 使用的 leaf COG 等优化产物。不是源 TIFF 副本目录。 |
| `styles/` | 默认渲染配置，如 DEM 色带、影像 RGB 组合。 |
| `stats/` | 统计、直方图、显示范围等。 |

`mosaic.addp.json` 应是 detector 识别 mosaic item 的主资源。它不是普通业务文本，而是 mosaic 数据集的清单。

这里不建议使用 `part-000001.cog.tif` 这类默认命名，也不建议在方案阶段规定 `sources.parquet`。`part` 只是批处理系统常见的分片命名，容易误导为 mosaic 必然要把源 TIFF 拆分或复制一遍；Parquet 也只是索引表的一种可能物理格式，和 mosaic 概念没有必然关系。当前应先把 index 视为“源栅格空间索引和读取计划”，具体落地用 JSON、GeoJSON、SQLite、FlatGeobuf 或 Parquet，需要等数据规模、查询方式和现有 ADDP 依赖统一后再定。

## 五、任务职责

mosaic 创建任务不只是登记源集合，而是一次完整的数据准备任务。它应完成所有影响预览性能和数据集自洽性的优化工作。

任务阶段建议：

```text
读取源 node
  -> 发现源 TIFF / COG
  -> 校验 CRS、band、dtype、NoData、分辨率
  -> 生成或登记 mosaic 内部 leaf COG
  -> 将 leaf COG 写入目标业务存储
  -> 生成 mosaic index
  -> 生成全局 overview
  -> 可选生成低层级瓦片
  -> 写入 mosaic manifest
  -> 触发或等待 Meta 扫描形成业务 item
```

### 5.1 leaf COG

源 TIFF 不应复制到 mosaic 数据集目录。mosaic 数据集内部的栅格文件必须是 COG。

是否已经是 COG，不能只看文件后缀、文件名或 MIME。`.tif`、`.tiff`、`.cog.tif` 都只能作为候选发现线索，不能作为 COG 事实。

COG 判定必须来自内容级校验：

1. Python 生成任务通过 GDAL 或等价栅格库打开源文件，读取真实 TIFF/GeoTIFF 结构。
2. 优先使用 GDAL COG 识别结果或 rio-cogeo validate 这类明确校验器。
3. 至少校验 tiled layout、block size、overview、压缩、NoData、CRS、extent、band/dtype，以及远程 Range 读取所需的布局事实。
4. 对 MinIO/S3 源 COG，应通过 GDAL `/vsicurl/`、presigned URL 或业务存储可读 URI 验证 Range 友好读取，而不是只验证本地文件名。
5. 校验失败、依赖缺失或结论不确定时，一律按“不是可引用 COG”处理，由任务生成新的 leaf COG。
6. 校验摘要必须写入 mosaic index，例如 `leaf_kind`、`source_locator`、`source_fingerprint`、`cog_validation.method`、`cog_validation.status`、`block_size`、`has_internal_overviews` 等。

建议主路径：

1. `in_place` 模式：源文件不是 COG 时，在原 node 内以“临时新文件 + 校验 + 替换”的方式规范化为 COG，文件名保持不变；源文件经内容级校验确认已经是 COG 时，不再复制或重写。
2. `detached` 模式：所有 leaf COG 都写入目标 mosaic 数据集，不依赖源 node 中的原文件；即使源文件已经是 COG，也应复制或重写到目标数据集，使生成后的 mosaic item 和源 node 断开长期读取依赖。
3. manifest/index 记录 leaf COG 路径、原始源文件 locator、fingerprint、extent、分辨率和读取参数。原始源 locator 是血缘和追踪信息，不是 `detached` mosaic 的在线预览读取依赖。

建议当前主路线是：**mosaic 面向预览的 leaf 读取对象统一为 COG**。`in_place` 模式原地规范化源 TIFF，文件名不变；`detached` 模式用额外存储生成独立目标数据集。两种模式生成完成后都只是一个 `format=raster_mosaic` 业务 item。

已确认边界：生成时区分 `in_place` 和 `detached`；生成后统一是 `format=raster_mosaic` 业务 item。`in_place` 通过原地规范化避免 TB 级数据无谓重复存储；`detached` 通过把 leaf COG 放入目标数据集，换取源 node 变更或删除后 mosaic 仍可独立预览。

### 5.2 mosaic index

index 至少需要记录：

1. leaf COG 路径。
2. bbox / extent。
3. CRS。
4. 分辨率。
5. band、dtype、NoData。
6. 重叠优先级。
7. 源文件 locator 和 fingerprint 快照。

这个 index 是后续预览、切片和刷新判断的核心事实。

### 5.3 全局 overview

全局 overview 是 mosaic 的低分辨率概览源，用于全幅显示和小比例尺浏览。

它应该是一个 COG，但不是完整分辨率全局 COG，也不承担保存全部细节的职责。它应控制像素规模，例如按目标最大像素数或最低层级瓦片需求生成。

### 5.4 低层级瓦片

低层级瓦片是可选优化。

有了 overview 后，仍可能需要低层级瓦片，因为：

1. overview 是数据源，低层级瓦片是已渲染结果。
2. overview 动态切片仍需要读取、拉伸、配色和编码。
3. 高频访问、多人并发和固定样式下，低层级瓦片能显著减少重复计算。

建议：

1. overview 是默认必做。
2. 低层级瓦片可由任务配置启用。
3. 一期如需简化，可以先只生成 overview，保留瓦片生成配置字段。

## 六、存储归属

所有 mosaic 数据集长期结果都写入用户选择的业务存储。

不进入 Manager infra MinIO 的对象包括：

1. leaf COG。
2. mosaic manifest。
3. mosaic index。
4. global overview。
5. 低层级瓦片。
6. 样式和统计。

理由：

1. 几千个 TIFF、几个 TB 的数据不能假设 infra MinIO 有足够容量。
2. mosaic 是业务数据集，不是 Manager 私有缓存。
3. 用户需要知道数据集在哪里、如何治理、如何备份和如何共享。
4. 删除或迁移 mosaic 应遵循业务存储和 Meta item 的生命周期。

Manager 可以有短期运行时缓存，但该缓存不能成为 mosaic 数据集的主事实，也不能替代业务存储结果。

## 七、Meta 与资源树

### 7.1 源 node

源 node 是创建入口。

源 node 不等于 mosaic。它只是告诉任务从哪里发现源 TIFF。

### 7.2 源 TIFF item

源 TIFF 仍是独立业务 item。它们可以继续单独预览、下载、扫描和治理。

### 7.3 mosaic item

mosaic 创建完成后，目标业务存储中应出现一个新的 mosaic item。

这个 item 是用户后续预览和治理的对象。Manager 快显能力查询应针对 mosaic item，而不是针对源 node 或源 TIFF 列表。

建议 locator 仍来自目标业务存储，例如：

```text
addp://engine/26/path/addp/mosaics/srtm/mosaic.addp.json?type=object&item_id=9001
```

这样 mosaic 保持 ADDP 统一 ResourceLocator 语义，不需要 Manager 私有 locator。

## 八、Manager 的职责

Manager 不拥有 mosaic 数据集的长期产物。Manager 的职责是：

1. 在资源树 node 上提供“创建 mosaic”入口。
2. 创建和执行 mosaic 任务。
3. 任务完成后引导 Meta 扫描或刷新目标位置。
4. 对 mosaic item 提供预览能力判断。
5. 读取业务存储中的 mosaic manifest、overview、index 和 leaf COG 进行地图快显。
6. 可选提供运行时缓存，但缓存不是主产物。

因此，Manager 的表不应表达“Manager 拥有的 mosaic 数据”。如果需要表，应表达任务定义、执行摘要或预览状态，而不是替代业务 item。

## 九、预览路线

mosaic item 的预览可以走专用 render source，例如：

```text
render_source = raster_mosaic_tile
```

前端不直接读取几千个 leaf COG，也不解析目标业务库内部结构。前端只请求 Manager 的统一瓦片入口：

```http
GET /api/v1/manager/raster_mosaic/tiles/{z}/{x}/{y}.png?locator={mosaic_item_locator}
```

后端根据 mosaic item locator：

1. 读取 manifest。
2. 低层级优先读取 overview 或 tiles。
3. 中高层级按 index 查询相交 leaf COG。
4. 读取对应 window。
5. 合成并返回图片瓦片。

全幅显示不应临时读取几千个 leaf COG 的 overview。它应优先使用 mosaic 数据集内的 global overview COG 或低层级 tiles。

### 9.1 前后端分工建议

Manager 前端已有单 TIFF 快显能力，当前实现使用 OpenLayers `GeoTIFFSource` 和 `geotiff.fromUrl`，并针对 `client_cog_render` 设置了适合 Range 读取的 `allowFullFile`、`blockSize`、`cacheSize` 和认证 header。这些能力应该复用，但不应把它扩展成“前端同时打开几千个 COG 做 mosaic 合成”的主路线。

建议采用混合分工：

1. mosaic 主预览以后端为主。后端读取 manifest、index、overview COG、tiles 和 leaf COG window，完成空间索引查询、重叠优先级、NoData 合成、拉伸、配色和图片编码，向前端提供统一瓦片接口。
2. 前端负责地图交互、底图、透明度、fit extent、图层切换、样式参数选择、加载状态和错误展示。
3. 前端 geotiff.js 能力复用在单个 leaf COG 查看、global overview COG 直接查看、显示范围采样、元数据读取和开发调试。
4. 小规模 mosaic 或内部诊断可以允许前端直接读取少量 COG，但不能作为产品主路径，也不能成为正式性能承诺。

选择后端主导主预览的理由：

1. 几千个 COG 的读取计划需要空间索引和窗口查询，前端不应下载并维护完整读取计划。
2. 源文件可能跨 MinIO、NFS 或后续其他业务存储，后端更适合统一鉴权、签名、Range 读取和连接复用。
3. 重叠区域的优先级、NoData、重采样、色带和多 band 合成需要统一结果，不能分散在浏览器端。
4. 浏览器并发请求、内存和 WebGL 纹理资源有限，几千个 COG 的 mosaic 合成会导致不稳定体验。
5. 后端瓦片入口更容易接入低层级瓦片缓存、运行时缓存、任务预热和监控。

### 9.2 item 内部 leaf COG 查看

mosaic 应支持查看 item 内部的单个 leaf COG，但这属于 mosaic item 的内部浏览能力，不应把每个 leaf COG 自动提升为同级 Meta item。

已确认方式：

1. `mosaic.addp.json` 或 `format_info.raster_mosaic` 中提供 source index 摘要。
2. Manager 预览 mosaic item 时，根据 source index 将 leaf COG 暴露为现有 multi ref 预览描述。
3. 用户选择某个 leaf 后，继续使用现有 `GET /api/v1/manager/preview?locator=...&ref_path=...` 机制读取该 leaf 的基础信息和单图预览。
4. `ref_path` 使用 mosaic 数据集内部的 leaf COG 路径；Manager 后端负责读取 manifest/index 并校验该 leaf 属于当前 mosaic item。
5. 如用户确实需要治理单个 leaf COG，应在业务存储中按普通 TIFF/COG item 单独扫描该文件所在范围；这和 mosaic 内部浏览是两个不同入口。

单 leaf COG 的查看复用已有 multi 单文件查看能力和 `RasterTIFFQuickView.vue` / `rasterGeoTIFFSourceOptions.js`。mosaic item 内部不应出现需要前端直接读取的原始大 TIFF。

这个规则类似容器 child 的预览语义：内部对象可以被查看，但默认不自动升格为外部 Meta item。不同之处在于 mosaic 的整体 data type 仍是 `media`，不是 `container`。

## 十、与现有 raster_cog 的关系

`raster_cog` 仍服务单个 TIFF item 的快显。

mosaic 不进入 `raster_cog`：

1. mosaic 是业务数据集 item，不是单 TIFF COG 结果。
2. leaf COG 是 mosaic 数据集内部组成，不是 Manager 的 `raster_cog` 结果。
3. mosaic 的 overview 和 tiles 属于业务数据集，不属于 Manager infra artifact。

后续可以复用 `tiff_to_cog` 中已经沉淀的 GDAL 参数经验，但 mosaic 不应通过 Manager 逐个调用 `tiff_to_cog` 来生成 leaf COG。mosaic 生成应作为新的 ADDP 任务类型，并在 Python 引擎中增加基于 GDAL Python API 的专用处理能力。

## 十一、任务体系与运行时边界

mosaic 创建任务必须纳入 ADDP 任务体系。Manager 可以提供创建入口、参数校验、任务提交和状态展示，但不应在 Manager 后端内完成几个 TB 栅格数据的重型转换。

建议边界：

1. Manager 后端负责：资源树 node 选择、目标业务存储选择、任务参数组装、任务提交、任务状态查询、Meta 刷新触发和预览接口。
2. Python 引擎负责：源 TIFF 扫描后的批量读取、COG 生成、全局 overview COG 生成、统计、低层级瓦片预生成和 manifest/index 写入。
3. ADDP 任务体系负责：任务定义、调度、执行记录、失败重试、日志、进度和结果登记。
4. common/common-python 负责：manifest schema、index 读写模型、COG/overview 元数据结构、locator 解析和可复用校验逻辑。

进度汇总边界已收敛为：Python 引擎不直接写 `common.task_executions`，也不持有 Manager 数据库连接。Python 在 GDAL callback 或阶段切换时向 Manager 内部事件入口发送进度摘要，Manager 校验 execution 归属后更新统一 execution。事件只保存最近阶段、文件计数、当前文件、单文件进度和总进度，不保存几千个源文件的全量明细。

内部事件入口：

```http
POST /api/v1/manager/internal/executions/{execution_id}/events
X-Internal-API-Key: <internal-key>
X-Tenant-ID: <tenant-id>
```

事件示例：

```json
{
  "phase": "leaf_cog",
  "event": "file_progress",
  "total_files": 3200,
  "processed_files": 36,
  "failed_files": 0,
  "current_file": "srtm_40_01.tif",
  "file_progress": 62,
  "overall_progress": 18
}
```

已确认的边界：

1. Python 引擎生成完成后，不直接调用 Meta，也不写 `common.task_executions`。Manager 在 `build_raster_mosaic` 成功返回后，通过 `common/client.MetaClient.CreateManualScanRun` 触发目标数据集根目录的 Meta deep scan。
2. Manager 将 Meta scan execution id 写入 mosaic generation execution metadata，便于在统一执行记录里追踪“生成”和“元数据入库”两个阶段。
3. 触发 Meta scan 失败时，mosaic generation execution 视为失败，因为结果不能形成可预览、可治理的业务 item。

仍需讨论确认的边界：

1. Manager 后端是否只提交任务，还是也参与源 node 的候选文件预扫描。
2. manifest/index 后续是否需要增加独立 JSON Schema 文件、兼容策略和 schema migration 规则。
3. 当前 Manager 只负责触发异步 Meta scan，不同步等待 Meta 扫描完成。是否需要任务体系支持“父子 execution 聚合状态”，后续结合执行监控统一设计。

## 十二、预览运行时边界

预览过程应尽量不调用 Python 引擎。

原因：

1. 预览是高频在线请求，Python 引擎更适合离线或批处理型生成任务。
2. 在线预览需要低延迟、连接复用、缓存、鉴权和统一 HTTP 生命周期，Manager 后端更适合承载。
3. mosaic 数据集创建阶段已经完成 COG、overview COG、index、stats 和可选 tiles 的优化，预览阶段不应重复触发重型处理。

建议边界：

1. Manager 后端负责：瓦片接口、manifest/index 读取、overview COG 或 leaf COG window 读取、合成、样式应用、图片编码和短期缓存。
2. common 负责：mosaic manifest/index 的 Go 侧结构、读取校验、空间查询辅助、渲染参数模型。
3. Python 引擎不参与常规预览；只有离线重建 overview、重建 leaf COG、重算 stats、重建 tile cache 时才进入任务体系。

仍需讨论确认的边界：

1. COG window 读取和重采样在 Manager 内部直接实现，还是抽到 common raster 包。
2. 低层级瓦片缓存的读取和失效逻辑放在 Manager，还是抽成 common 能力。
3. 渲染样式和统计拉伸的通用模型是否先进入 attributes 规范，再落 common。

## 十三、现有实现可复用点

已确认的现状：

1. Manager 已作为 TaskProvider 注册到 System，现有 `raster_cog_generation`、`vector_tile_cache_generation` 等任务类型通过 `GET /tasks`、`POST /tasks/{task_type}/{id}/execute`、`GET /executions/{execution_id}` 进入 ADDP 任务体系。
2. `raster_cog_generation` 当前由 Manager 后端创建 `common.task_executions`，后台执行器通过 `WorkflowRuntimeProvider.InvokeOperator("tiff_to_cog")` direct 调用 Python Workflow。
3. Python Workflow 现有 `tiff_to_cog` 是单文件窄口径算子，负责执行 GDAL 转换，不访问 Manager 数据库，也不登记 artifact state。
4. 现有 `tiff_to_cog` 和 `manager.raster_cog` 语义绑定 Manager infra MinIO 的单 TIFF 快显产物，不适合作为 mosaic 数据集的主语义。
5. Manager 前端已有 `RasterTIFFQuickView.vue`、`rasterGeoTIFFSourceOptions.js`，适合复用到单 leaf COG 和 overview COG 的直接查看。

建议复用：

1. 复用 TaskProvider 标准 endpoint 和 `common.task_executions` 记录模式。
2. 复用 Manager 选择 workflow runtime、direct invoke Python operator 的调用方式。
3. 复用 Python raster operator 中的 GDAL 命令执行、`gdalinfo -json`、COG 参数处理思路。
4. 复用前端单 COG 快显能力作为 mosaic 内部 leaf 查看能力。
5. Python mosaic 生成主路线采用 GDAL Python API，而不是拼接 GDAL CLI 黑盒命令：`gdal.Translate` 负责 TIFF 到 COG，`gdal.BuildVRT` 负责 virtual mosaic，`gdal.Translate` 或 `gdal.Warp` 负责全局 overview COG。

不建议复用：

1. 不复用 `manager.raster_cog` 表表达 mosaic 产物。
2. 不复用 infra MinIO 作为 mosaic 结果目标。
3. 不把 `tiff_to_cog` 扩展成多职责的 mosaic 生成算子，也不让 Manager 逐个 TIFF 调用 `tiff_to_cog` 来拼装 mosaic。
4. 不让前端直接拼接几千个 COG。

## 十四、开工建议

建议第一阶段只做最小闭环：

1. 新增任务类型：`raster_mosaic_generation`。
2. 新增 Manager 任务定义表：`manager.raster_mosaic_tasks`。
3. 新增 Python mosaic 执行能力：入口命名为 `build_raster_mosaic`，参数使用 Manager 解析后的 `access_plan`，内部使用 GDAL Python API，按 `placement.mode` 生成 leaf COG、virtual mosaic、overview COG、index 和 manifest。
4. Manager 后端负责创建任务、校验参数、选择 Python Workflow runtime、把源 node 和目标业务存储解析为 GDAL 技术访问计划，并接收 Python 通过 GDAL callback 汇总后的进度事件。
5. Meta 先通过 `mosaic.addp.json` detector 识别 `data_type=media`、`format=raster_mosaic`、`layout=whole`。
6. 预览先实现 Manager 后端瓦片接口，优先读 overview COG；中高层级再按 index 读 leaf COG window。

第一阶段不做：

1. 不做前端多 COG 合成。
2. 不做 Python 参与在线预览。
3. 不做完整调度、取消和复杂参数覆盖。
4. 不做独立 Manager Worker。
5. 不做低层级瓦片缓存的复杂失效策略。

## 十五、已确认边界与仍需讨论点

### 15.1 创建位置与 TIFF 规范化

mosaic 创建位置分为两种模式：

| 方案 | 含义 | 优点 | 缺点 |
| --- | --- | --- | --- |
| `in_place` | 在原 node 创建 mosaic；非 COG TIFF 原地规范化为 COG，文件名保持不变；已是 COG 的文件不处理。 | 避免重复存储几个 TB 数据；适合就地优化原数据目录。 | 会修改原 node 内非 COG TIFF 的文件内容，必须采用临时新文件 + 校验 + 替换的安全流程；一期仅允许 NFS/localfs，不允许 MinIO/S3。 |
| `detached` | 创建到新 node，所有 leaf COG 都写入目标 mosaic 数据集，不修改原 node。 | 原 node 完全不变；目标 mosaic 数据集边界清晰。 | 需要更多存储空间；已是 COG 的源也需要复制或重写到目标数据集。 |

已确认：**一期采用显式 placement 模式**。任务配置必须声明：

```json
{
  "placement": {
    "mode": "in_place"
  }
}
```

或：

```json
{
  "placement": {
    "mode": "detached"
  }
}
```

`in_place` 模式下，`target` 可以省略，服务端默认使用 `source.node_locator`。如果源 TIFF 不是 COG，任务必须先在同一 node 生成临时 COG，校验成功后再删除旧文件并把临时文件替换为原文件名；如果源文件已经是 COG，则不处理。对象存储没有本地文件系统级原子替换能力，因此一期明确禁止 MinIO/S3 的 `in_place`，对象存储必须使用 `detached`。

`detached` 模式下，`target.storage_locator` 必填，且不得等于 `source.node_locator`。该模式必须把所有 leaf COG 写入目标 mosaic 数据集，不修改原 node。

术语边界：`placement.mode` 只影响生成过程。生成完成后，结果统一是一个 `format=raster_mosaic` 的业务 item，不再按 `in_place` / `detached` 区分 item 语义。

### 15.2 Manager 与 Python 的任务职责边界

我的建议：

1. Manager 负责任务定义、任务提交、源 node/目标位置参数校验、runtime 选择、execution 状态、Meta 刷新触发。
2. Manager 负责把 ADDP locator、engine connection、业务存储权限和 placement 策略解析为 `access_plan`，包括 GDAL root URI、GDAL env、候选过滤规则、目标数据集 root 和进度回调信息。
3. Python operator 负责重型 GDAL 工作：按 `access_plan` 做源发现后的读取、COG 生成、overview COG、index、manifest、stats、可选 tiles。
4. 源 node 的候选文件预扫描可以分两步：Manager 做轻量资源树/locator 校验；Python 做真实递归扫描和 GDAL 校验。

### 15.3 Manager 与 common 的预览职责边界

我的建议：

1. Manager 负责 mosaic 预览 HTTP API、鉴权、manifest/index 查询、运行时缓存和错误响应语义。
2. 栅格 COG window 读取、重采样、NoData 合成、色带/拉伸和图片编码建议放入独立 `raster-mosaic-runtime` sidecar，不把 GDAL 依赖直接压进 Manager 主进程。该 sidecar 只服务 `raster_mosaic`，不是通用栅格引擎。
3. manifest/index 的 Go 结构和基础校验进入 common，避免 Meta、Manager 后续重复；Python 生成侧的 schema builder 进入 common-python。
4. 如果后续 Service 或 Portal 也要复用 mosaic 预览，再把空间索引查询、渲染参数模型和 sidecar client 抽到 common。

## 十六、待补规范

后续正式落地前，需要先补这些规范：

1. 在数据类型和格式体系中定义 `raster_mosaic`。
2. 在数据项探测器规范中定义 `mosaic.addp.json` 如何形成 item。
3. 在 attributes 规范中定义 mosaic 的 `format_info.raster_mosaic`、`capabilities.spatial`、source index 摘要和 overview 摘要。
4. 在 Manager 快显文档中补 mosaic item 的 capability 和 render source。
5. 在任务体系中定义 mosaic 创建任务类型，例如 `raster_mosaic_generation`。
6. 在存储流和下载语义中定义 mosaic item 的下载行为，是下载 manifest、打包完整数据集，还是按策略导出。
7. 在 Manager 预览协议中固化 mosaic 内部 leaf COG 查看参数和返回语义；当前实现已采用父 `locator` + `ref_path` 复用 multi 单文件预览。

## 十七、当前落地状态

截至 2026-06-26，已完成第一阶段任务入口、Python 最小生成主路径、Meta 入库触发和第一版 Manager 快显闭环：

1. `common/execution` 已新增任务类型 `raster_mosaic_generation`。
2. Manager 已新增 `manager.raster_mosaic_tasks` 任务定义表、模型、仓库和服务。
3. Manager TaskProvider 已支持 `GET /tasks?task_type=raster_mosaic_generation`、`GET /tasks/{task_type}/{id}` 和 `POST /tasks/{task_type}/{id}/execute`。
4. Manager 私有任务配置 API 已支持 `/raster_mosaic_tasks` 的创建、查询、更新、删除。
5. Manager TaskProvider capability 已声明 `raster_mosaic_generation`，指向后续前端任务入口 `/manager/spatial-quick-view/raster-mosaic?tab=tasks`。
6. Python Workflow 已新增 `build_raster_mosaic` operator 和元数据注册，并已接入 GDAL Python API 的最小真实主路径：发现源 TIFF/COG、内容级 COG 判断、按 `placement.mode` 生成 leaf COG、构建 VRT、生成全局 overview COG、写入 `index/source-index.json` 和 `mosaic.addp.json`。
7. Manager 已新增 mosaic execution 进度事件汇总入口：`POST /api/v1/manager/internal/executions/{execution_id}/events`，由 Manager 校验内部 key、租户和 execution 归属后更新 `common.task_executions`。
8. Python Workflow 的 `build_raster_mosaic` 契约已收敛为 `access_plan + placement + cog/overview/tiles`，不再要求 Python 识别 ADDP locator。
9. Manager 已新增 `ManagerRasterMosaicExecutor` 调用主路径，可以选择支持 direct 的 Python Workflow runtime，组装 GDAL `access_plan` 并调用 `build_raster_mosaic`；Python 返回 manifest/index/overview 引用后，Manager 写回任务执行结果。
10. Python Workflow 镜像构建已安装与系统 GDAL 版本匹配的 Python binding，并在 operator 内使用 GDAL path-specific option 分别绑定 source root 和 target root 的 `/vsis3/` 参数，避免源 MinIO 与目标 MinIO 不同 endpoint/凭据时只能使用一套全局 `AWS_*` 配置。
11. `common/format` 已新增 `raster_mosaic` descriptor，`mosaic.addp.json` 只作为确定性候选文件名；Meta 落库前必须读取 manifest 内容校验 `schema_version`、`format=data_type/layout` 等关键字段。
12. `common/dataitem` 已支持 whole-scope manifest 识别，`raster_mosaic` 可以 claim 数据集目录，避免内部 leaf COG 被 TIFF multi item 先认领。
13. Meta 扫描链路已支持读取 `mosaic.addp.json` 并生成 `data_type=media`、`format=raster_mosaic`、`layout=whole` 的业务 item，attributes 写入 manifest/index/overview 引用、leaf/source 数量和空间能力摘要。
14. Manager mosaic generation 成功后已触发目标数据集根目录的 Meta manual deep scan，并把 Meta scan execution id 写入 generation execution metadata；扫描请求按 dataset root catalog path 提交，不展开几千个 leaf COG。
15. `common/rastermosaic` 和 `common-python/addp_common.raster_mosaic` 已提供 manifest/source-index v1 的共享 schema 常量、结构和校验/生成辅助，Python Workflow 不再手写 schema 字符串。
16. Manager 已禁止对象存储 `in_place`，MinIO/S3 源 node 必须用 `detached` 生成，避免非原子替换风险进入主路径。
17. Manager quick view capability 已识别 `format=raster_mosaic` item，并返回 `render_source=raster_mosaic_tile`、空间范围和瓦片 URL 模板。
18. Manager 已新增 `GET /api/v1/manager/raster_mosaic/tiles/{z}/{x}/{y}.png`，第一版只基于全局 overview COG 出 PNG/WebP 图片瓦片。
19. `raster-mosaic-runtime` sidecar 已接入开发启动脚本。该 runtime 只服务 `raster_mosaic`，负责 GDAL 读取 overview COG、重采样、NoData 透明化和图片编码；Manager 后端负责鉴权、item/manifest 查询、业务存储访问参数解析和 runtime 调用。
20. Manager 前端 `SpatialPreview.vue` / `RasterTIFFQuickView.vue` 已支持 `raster_mosaic_tile`，可以复用现有 OpenLayers 地图、底图切换、透明度和全幅显示能力；进入 mosaic 快显时不再触发无意义的基础表格预览请求。
21. 真实业务 MinIO 样例已跑通：从 `addp/images` 的 28 个 GeoTIFF 生成 `addp/mosaics/srtm-test/srtm-test`，产出 `mosaic.addp.json`、`index/source-index.json`、`overviews/overview.cog.tif`，Meta 侧形成一个 `format=raster_mosaic`、`layout=whole` 的业务 item，前端地图可显示 mosaic 图层。
22. Manager 父 mosaic 预览已返回 leaf refs，前端可复用现有 multi ref 下拉；选择 leaf 后通过父 `locator` + `ref_path` 进入已有单文件预览链路，leaf COG 不自动升格为同级 Meta item。

下一步需要继续讨论并确认：

1. 中高层级是否进入 leaf COG window 合成主路径，以及 overview 与 leaf 合成的切换阈值、重叠策略和缓存策略。
2. 栅格渲染风格参数协议，包括波段选择、色带、透明度、拉伸、NoData 和后续 data tile 能力的边界。
3. 任务体系是否需要父子 execution 或关联 execution 视图，让用户能从 mosaic generation 直接看到后续 Meta scan 的完成状态。
4. mosaic item 的下载、删除、移动和存储清理语义，尤其是 detached 数据集的目录级生命周期。

## 十八、当前结论

一句话收敛：

```text
mosaic 是由 node 创建、输出到用户业务存储、最终进入 Meta 的业务级栅格镶嵌数据集 item。
```

当前建议：

1. 从源 node 创建 mosaic 任务。
2. 用户必须选择目标业务存储位置。
3. 任务结果是新的 mosaic 业务 item。
4. mosaic 推荐 `data_type=media`、`format=raster_mosaic`、`layout=whole`。
5. leaf COG、index、overview COG、tiles 等优化结果都属于业务数据集，写入目标业务存储。
6. Manager infra MinIO 不参与 mosaic 长期产物存储。
7. Manager 负责创建任务和预览 mosaic item，不拥有 mosaic 数据集。
8. 全幅显示依赖 mosaic 数据集内的 global overview 或低层级 tiles。
9. 中高层级预览按 mosaic index 读取相交 leaf COG。
10. mosaic item 内部可以查看单个 leaf COG，但 leaf COG 默认不自动升格为同级 Meta item。
