# 空间表 Row Geometry 编码协议方案

> 状态：阶段性落地方案。
> 本文用于记录 `common/format`、native table engine、Transfer planner 与 `vector_reproject` 之间已经收敛的空间几何行值协议。稳定约束后续应拆分进入格式能力规范、引擎能力声明规范、Transfer 任务规范和工作流算子规范。

## 背景

ADDP 的 table Transfer 以 `[]map[string]interface{}` 作为 format / engine 之间的行数据承载。普通字段天然可用 Go 基础类型表达，但 geometry 字段当前没有统一协议。

现有代码事实：

- Shapefile reader 读取 `.shp` 原生 shape 后，可按 `ParseOptions.GeometryEncoding` 输出 WKT、WKB 或 EWKB。
- GeoJSON reader 可按 `ParseOptions.GeometryEncoding` 输出 GeoJSON geometry object 或 EWKB。
- Shapefile writer 写入时调用 `common/spatial.ParseGeometryValue()`，支持 WKT、WKB / EWKB `[]byte`、hex 文本以及显式 `shapefile_shape` native row value。
- GeoJSON writer 可接受 GeoJSON geometry object 和 EWKB；如果明确收到非 4326 空间事实，会作为最后一道门禁失败。
- Transfer planner 已在空间表链路中按 source/target 能力选择 geometry encoding，并在 `target=geojson` 且 CRS 不匹配时插入 `spatial_reproject` 或选择 source-native read transform。

早期 `shapefile -> geojson` 依赖特殊路径，`geojson -> shapefile` 可能因 geometry row value 不匹配而在执行阶段失败。当前主路径已收敛为 planner 阶段编码协商，规划失败优于执行阶段失败。

## 目标

建立一条统一主路径：

```text
source reader / native table reader
  -> 声明并输出某种 GeometryEncoding
  -> Transfer planner 选择 pipeline 内部 geometry encoding
  -> 必要时用 common/spatial 做编码转换
  -> 必要时用 vector_reproject 做 CRS 转换
  -> target writer / native table writer 消费声明支持的 GeometryEncoding
```

核心目标：

1. geometry 字段的 row value 不再依赖 format 私有默认形态。
2. format 和 native table engine 都声明自己支持的 geometry 读写编码。
3. Transfer planner 基于能力声明选择编码，不硬编码格式类型。
4. Transfer planner 负责选择跨模块 geometry encoding；format / engine 只声明能力，不推荐对端未知时的编码。
5. 如果源和目标编码不匹配，先看 `common/spatial` 是否支持纯编码转换；不支持则任务规划失败。
6. CRS 转换第一阶段只支持 Arrow + EWKB 的 `vector_reproject` direct 调用。
7. 如果源和目标的原生 geometry encoding 一致，且不涉及 CRS 转换或中间几何算子，则允许 Transfer 直通原生编码。

## 术语

| 术语 | 说明 |
| --- | --- |
| GeometryEncoding | geometry 字段在 row value 中的编码形态。 |
| Format-native geometry | 用户可识别的文件格式原生几何表达，例如 Shapefile 的 shape record、GeoJSON geometry object。它可以作为同构直通优化，但不是跨格式默认协议。 |
| Engine-internal geometry | 数据库或计算引擎内部类型，例如 PostGIS `geometry`。它不是 ADDP row geometry encoding，不应暴露给用户或 Transfer pipeline。 |
| Native GeometryEncoding | 某个 format 在自身边界内最自然、且对应用户可识别格式语义的 geometry row encoding。 |
| Portable GeometryEncoding | 可跨 format / engine 传递的通用 geometry row encoding，例如 EWKB、WKB、WKT。 |
| Row geometry protocol | Transfer table pipeline 中 geometry 字段的稳定行值协议。 |
| Encoding conversion | 只改变几何对象的表达编码，不改变坐标数值和 CRS。 |
| Reproject | 改变坐标参考系统和坐标数值，例如 EPSG:3857 到 EPSG:4326。 |
| `geom.T` | Go 进程内的几何对象接口，来自 `github.com/twpayne/go-geom`。它不是文件格式、不是二进制编码，也不是 Shapefile 原生格式。 |

## GeometryEncoding 候选集

当前阶段固定以下编码，并明确区分 portable encoding 与 native encoding：

| 编码 | row value 类型 | 说明 |
| --- | --- | --- |
| `ewkb` | `[]byte` | 首选通用二进制协议。可携带 SRID 和扩展维度信息，但 CRS / SRID 事实仍以 `datatype.SpatialInfo` 为准。 |
| `wkb` | `[]byte` | 标准 WKB，不承诺携带 SRID。适合没有 SRID 要求的内部链路。 |
| `ewkb_hex` | `string` | EWKB 十六进制文本。主要用于 JSON 友好边界或 SQL 参数便利形态；第一阶段可作为 `common/spatial` 支持的变体，不要求所有 format / engine 声明。 |
| `wkb_hex` | `string` | WKB 十六进制文本。第一阶段可作为 `common/spatial` 支持的变体，不要求所有 format / engine 声明。 |
| `wkt` | `string` | 可读性好，但不作为高性能 Transfer 主协议。 |
| `geojson` | `map[string]interface{}` 或 JSON object | GeoJSON format 的原生行值表达。可作为 native encoding，也可由明确声明支持的 writer 消费，但不作为跨格式首选协议。 |
| `shapefile_shape` | format 原生 shape record | Shapefile 的原生几何记录表达。它对用户有价值，因为对应 `.shp` 文件格式本身；仅用于 Shapefile 同构链路或 Shapefile plugin 内部。 |

`format.GeometryEncoding` 已包含 `wkt`、`wkb`、`ewkb`、`geojson` 和 `shapefile_shape`。`wkb_hex` / `ewkb_hex` 暂作为 `common/spatial` 可解析 / 可输出的变体，不要求所有 format / engine 放进能力声明。

`geom.T` 不列入 `GeometryEncoding`。它是 Go 内存对象模型，可作为 `common/spatial` 内部解析和转换的中间对象，但不应作为 Transfer 跨模块 row value 协议。

PostGIS `geometry` 也不列入 `GeometryEncoding`。它是 PostgreSQL / PostGIS 内部类型，对 ADDP 用户没有直接可交换价值。PostGIS 对 Transfer 有价值的是可通过 SQL 接口导出的编码，例如 `ewkb`、`ewkb_hex`、`wkb`、`wkt`、`ewkt`、`geojson`，以及源端空间函数能力，例如 `ST_Transform`。

### WKB 与 EWKB 差异

WKB 是 OGC 标准二进制几何编码，表达 geometry type、坐标、ring / part 结构等几何本体。

EWKB 是扩展 WKB。对 ADDP 第一阶段最关键的差异是：

- EWKB 可携带 SRID。
- EWKB 可表达部分扩展维度标志，例如 Z / M。

但 ADDP 不把 EWKB 内的 SRID 当作空间事实唯一来源。稳定 CRS / SRID 事实仍以 `datatype.SpatialInfo` 为准；EWKB 的 SRID 只是 row value 编码能力和算子传输能力。

## 单一路线

Transfer 主链路统一通过 planner 选择一个 pipeline geometry encoding。当前阶段把 EWKB 作为通用主协议，但不是所有场景都强制二进制化。

通用主协议：

```text
pipeline_geometry_encoding = ewkb
pipeline_geometry_value_type = []byte
```

EWKB 作为通用主协议的理由：

- Shapefile reader 已能输出 EWKB。
- Shapefile writer 已能消费 EWKB bytes。
- native PostgreSQL 可自然输出 / 消费 WKB/EWKB 语义。
- `vector_reproject` 可用 Arrow 承载 EWKB geometry batch。
- GeoJSON reader / writer 缺口明确，改造范围可控。

format / engine 的能力目标不是越多越好。当前阶段每类空间读写能力优先满足：

```text
format-native encoding + ewkb
```

其他编码按真实消费需求增加。已有实现可以先保留，例如 Shapefile 的 WKT / WKB；Manager 预览需要 GeoJSON 时，应优先通过 `common/spatial` 的编码转换函数生成 GeoJSON geometry，而不是要求每个 format reader 都直接支持 `geojson` 输出。

原生编码直通策略：

```text
source_native_geometry_encoding == target_native_geometry_encoding
AND 不需要 CRS 转换
AND 不需要中间几何算子读取或改写 geometry
AND source reader 与 target writer 都声明支持该编码
=> 允许直接使用 native geometry encoding
```

这意味着：

- `geojson` geometry object 可以继续作为 GeoJSON format 的本地读写表达。
- 同构或明确兼容的 native encoding 可以作为优化路径。
- 一旦需要跨格式转换、CRS 转换、Python 算子、通用 spatial helper 或未知中间 transform，planner 必须切回 portable encoding，优先 EWKB。

## 能力声明

### Format 空间编码能力

格式插件应声明空间表 row geometry 编码能力。概念结构如下：

```go
type SpatialTableEncodingCapability struct {
    GeometryReadEncodings  []format.GeometryEncoding
    GeometryWriteEncodings []format.GeometryEncoding
    DefaultReadEncoding    format.GeometryEncoding
    DefaultWriteEncoding   format.GeometryEncoding
    NativeReadEncoding     format.GeometryEncoding
    NativeWriteEncoding    format.GeometryEncoding
}
```

语义：

| 字段 | 说明 |
| --- | --- |
| `GeometryReadEncodings` | reader 可按请求输出的 geometry row encoding。 |
| `GeometryWriteEncodings` | writer 可接受的 geometry row encoding。 |
| `DefaultReadEncoding` | 未指定 `ParseOptions.GeometryEncoding` 时的读出编码。主要服务预览和调试，不作为 Transfer 主协议。 |
| `DefaultWriteEncoding` | 未指定写入编码时 writer 的自然接收形态。 |
| `NativeReadEncoding` | reader 在 format 原生语义下的自然输出编码。 |
| `NativeWriteEncoding` | writer 在 format 原生语义下的自然接收编码。 |

format 不声明 `PreferredTransferEncoding`。在不知道 source、target、transform 和 CRS 约束的情况下，format 无法推荐 Transfer 应该使用哪个编码；编码选择属于 Transfer planner 的职责。

示例：

```text
GeoJSON:
  read:  geojson, ewkb
  write: geojson, ewkb
  default_read: geojson
  default_write: geojson
  native_read: geojson
  native_write: geojson

Shapefile:
  read:  shapefile_shape, wkt, wkb, ewkb
  write: shapefile_shape, wkt, wkb, ewkb
  default_read: wkt
  default_write: wkt
  native_read: shapefile_shape  # Shapefile 格式原生表达，仅限 Shapefile 同构链路或插件内部
  native_write: shapefile_shape # Shapefile 格式原生表达，仅限 Shapefile 同构链路或插件内部
```

Shapefile 的 `.shp` 原生 shape record 对用户有价值，因为它是 Shapefile 文件格式本身的几何表达。`shapefile_shape` 可以作为 Shapefile 同构链路的 native encoding，用于 Shapefile -> Shapefile 且不需要 CRS 转换、不需要几何算子、不跨语言处理的场景。Transfer 不应让非 Shapefile format writer 消费 `shapefile_shape`，跨 format 或需要 reproject 时必须转为 portable encoding，优先 EWKB。

### Native table 空间编码能力

native table engine 也应声明空间列的批量读写编码能力。概念结构如下：

```go
type NativeTableSpatialEncodingCapability struct {
    GeometryReadEncodings  []format.GeometryEncoding
    GeometryWriteEncodings []format.GeometryEncoding
    ReadTransform          bool
    WriteTransform         bool
    NativeSpatialFunctions bool
}
```

语义：

| 字段 | 说明 |
| --- | --- |
| `GeometryReadEncodings` | native table read session 可输出的 geometry encoding。 |
| `GeometryWriteEncodings` | native table write session 可消费的 geometry encoding。 |
| `ReadTransform` | 读取时是否能按 hint 做 CRS 转换。PostGIS 可声明支持。 |
| `WriteTransform` | 写入时是否能按 hint 做 CRS 转换。第一阶段可不启用。 |
| `NativeSpatialFunctions` | engine 是否能在内部对空间列执行原生空间函数，例如 PostGIS `ST_Transform`、`ST_AsMVT`。这不是 geometry encoding。 |

native table 的空间编码能力应属于 engine capabilities，而不是 Transfer 私有表。当前落点为 `engine.capabilities/v1` 的 `storage.store.table_spatial_encoding`。

PostGIS 示例：

```text
PostgreSQL / PostGIS:
  read:  ewkb, geojson
  write: ewkb
  read_transform: true
  native_spatial_functions: true
```

PostGIS 的 `geometry` 内部类型不作为 `native_read_encoding` 或 `native_write_encoding` 暴露。通过 PostgreSQL 接口跨出 provider 后，应使用可交换编码表达。第一阶段优先支持 `ewkb`；`ewkb_hex`、`wkb`、`wkt`、`ewkt` 等可以按真实调用需求后续补充。已有 `geojson` read 能力可保留，主要服务 GeoJSON 输出或预览场景。

## Transfer Planner 编码协商

Transfer planner 在构建 table plan 时应执行编码协商。

输入事实：

- source endpoint kind：native / encoded。
- target endpoint kind：native / encoded。
- source spatial facts：`datatype.SpatialInfo`。
- target spatial constraints：目标格式或用户指定的 CRS、geometry type、dimension。
- source read geometry encodings。
- target write geometry encodings。
- `common/spatial` 支持的 encoding conversion 矩阵。
- workflow direct operator 支持的 reproject payload encoding。

协商步骤：

```text
1. 判断是否为空间表链路
   - source 有 SpatialInfo，或 source format / native table 声明 spatial table encoding。

2. 读取 source 支持的 GeometryEncoding 集合。

3. 读取 target 支持的 GeometryEncoding 集合。

4. 优先选择 Transfer pipeline encoding：
   - 如果需要 reproject，则强制使用 ewkb，因为第一阶段 reproject 只支持 Arrow + EWKB。
   - 如果不需要 reproject，且 source / target 声明支持同一个 native encoding，并且没有中间几何 transform，则优先使用 native encoding。
   - 如果 native encoding 不能直通，则选择通用首选编码 ewkb。
   - 如果 source 和 target 都直接支持 ewkb，直接使用 ewkb。
   - 如果 source 支持 A，target 支持 B，且 common/spatial 支持 A -> B，则插入 encoding conversion。
   - 如果需要 reproject，但 source 不能输出 ewkb，必须先找到 A -> ewkb 的编码转换路径。

5. 设置 source read hints / parse options：
   - encoded source：设置 `ParseOptions.GeometryEncoding`。
   - native source：设置 read hints，例如 `geometry_encoding=ewkb`。

6. 设置 target write hints / write options：
   - encoded target：设置 geometry write encoding 或约定 writer 消费 `ewkb`。
   - native target：设置 write hints，例如 `geometry_encoding=ewkb`。

7. 无法找到编码匹配或转换路径时，在 planner 阶段失败。
```

规划失败优于执行阶段失败。错误信息应包含：

- source 支持的读编码。
- target 支持的写编码。
- planner 尝试选择的 pipeline encoding。
- `common/spatial` 是否支持所需编码转换。
- 如涉及 CRS 转换，说明 `vector_reproject` 只支持 Arrow + EWKB。

### 编码选择优先级

Transfer planner 的编码选择顺序应固定为：

```text
1. 如果需要 reproject：
   强制选择 ewkb，并规划 Arrow + EWKB。

2. 如果不需要 reproject：
   先看 source / target 是否支持同一个 native encoding。
   能直通则使用 native encoding。

3. 如果 native encoding 不能直通：
   选择首选通用编码 ewkb。

4. 如果 ewkb 不可用：
   尝试 source encoding -> common/spatial -> target encoding。

5. 仍不可用：
   planner 阶段失败。
```

native encoding 直通必须满足：

- source 和 target 显式声明同一个 native encoding。
- 不需要 CRS 转换。
- 不需要 Python 算子或其他中间算子理解 geometry。
- 没有 field mapping 之外的 geometry transform。
- 目标 writer 明确声明能消费该 native encoding。
- 该 native encoding 对 ADDP 用户有格式语义价值，且能在当前 Transfer 执行边界内稳定表达。

不允许把数据库内部类型伪装成 native encoding。例如 PostGIS `geometry` 只能作为 PostgreSQL provider 内部对象存在；Transfer planner 应使用 PostGIS 声明的 `ewkb`、`ewkb_hex`、`geojson` 等可导出编码。

## 编码转换与 CRS 转换的边界

编码转换只改变 geometry 表达，不改变坐标数值。

示例：

```text
geojson geometry object -> EWKB bytes
EWKB bytes -> WKB hex
WKT -> EWKB bytes
```

CRS 转换会改变坐标数值。

示例：

```text
EWKB(EPSG:3857) -> EWKB(EPSG:4326)
```

第一阶段：

- 编码转换放在 `common/spatial`。
- CRS 转换不放在 `common/spatial`，由具备能力的 native engine 或 `vector_reproject` 完成。
- `vector_reproject` direct 调用只支持 Arrow + EWKB geometry batch。
- `vector_reproject` 输出 Arrow batch 的 `source_crs` / `target_crs` 均应写入输出几何当前 CRS，不能继续保留输入 CRS。
- Transfer 在 reproject 或源端 read transform 后，传给下游 writer 的 `SpatialInfo` 必须更新为输出 CRS；未重新计算的 `extent`、空间索引状态和索引名不得从源数据继承。

这意味着如果 source 输出不是 EWKB，而又需要 reproject，planner 必须先规划 source 输出 EWKB，或先插入编码转换到 EWKB。不能把 GeoJSON object、WKT 或 Shapefile native shape 直接交给 reproject 算子。

## Reproject 第一阶段约束

`vector_reproject` 的 direct payload 固定为：

```text
payload = Arrow IPC stream
geometry column = EWKB
其他属性字段 = 不进入 payload
```

输入参数：

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `source_crs` | string | 是 | 源 CRS，例如 `EPSG:3857`。 |
| `target_crs` | string | 是 | 目标 CRS，例如 `EPSG:4326`。 |
| `geometry_column` | string | 是 | Transfer 原始 row 中的 geometry 字段名，用于回填。 |
| `binary_payload` | Arrow IPC + EWKB | 是 | 只包含几何批。 |

输出：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `binary_payload` | Arrow IPC + EWKB | 转换后的几何批，行数必须与输入一致。 |

失败条件：

- 源 CRS 缺失。
- 目标 CRS 缺失。
- 输入 payload 不是 Arrow IPC。
- geometry encoding 不是 EWKB。
- 输出几何数量与输入行数不一致。
- 算子不可发现或不支持 direct execution mode。

## 典型链路

### Shapefile -> GeoJSON

```text
Shapefile reader
  - requested read encoding: ewkb
  - row.geometry: []byte(EWKB)

Transfer planner
  - target GeoJSON requires EPSG:4326
  - source CRS != EPSG:4326
  - insert spatial_reproject with Arrow + EWKB

vector_reproject
  - Arrow + EWKB -> Arrow + EWKB

GeoJSON writer
  - accepts ewkb
  - encodes Feature.geometry as GeoJSON geometry object
```

### GeoJSON -> Shapefile

```text
GeoJSON reader
  - requested read encoding: ewkb
  - row.geometry: []byte(EWKB)

Transfer planner
  - target Shapefile accepts ewkb
  - target CRS defaults to source CRS unless user specifies target CRS
  - no reproject if CRS unchanged

Shapefile writer
  - consumes EWKB
  - writes native shp.Shape
```

### Native PostgreSQL -> GeoJSON

```text
PostgreSQL read session
  - if engine declares read transform support:
      read geometry already transformed to EPSG:4326
  - otherwise:
      read geometry as EWKB

Transfer planner
  - if not source-native transformed, insert vector_reproject

GeoJSON writer
  - consumes EWKB
```

### GeoJSON -> Native PostgreSQL

```text
GeoJSON reader
  - requested read encoding: ewkb

PostgreSQL writer
  - consumes EWKB if declared supported
  - creates / writes geometry column using SpatialInfo
```

## Format 改造要求

### GeoJSON reader

必须支持：

```text
ParseOptions.GeometryEncoding = geojson | ewkb
```

行为：

- `geojson`：输出 GeoJSON geometry object，用于预览、调试和格式本地场景。
- `ewkb`：输出 `[]byte`。SRID 优先来自 GeoJSON CRS 或上游 `SpatialInfo`；没有显式 CRS 且 bbox 落在经纬度范围内时可按 GeoJSON 默认语义使用 4326；bbox 明显越界时不得把 EWKB 标成 4326，应保持 SRID unknown。

`wkb` 可作为后续增强，不作为第一阶段必需项。

### GeoJSON writer

必须支持写入：

```text
geojson object
ewkb []byte
```

writer 内部负责把通用二进制 geometry 转成 GeoJSON geometry object，但不负责 CRS 转换。写出标准 GeoJSON 前，Transfer 必须保证目标 CRS 已满足约束。

`wkb`、`wkb_hex`、`ewkb_hex` 可通过 `common/spatial` 转换函数支持，不要求 GeoJSON writer 第一阶段直接声明全部编码。

### Shapefile reader

显式声明并支持：

```text
shapefile_shape
wkt
wkb
ewkb
```

默认读编码仍为 `wkt`，服务预览、日志和调试；Transfer 如源和目标都是 Shapefile 且不涉及 CRS 转换，可以显式请求 `shapefile_shape` 走 native passthrough。

### Shapefile writer

显式声明并支持：

```text
shapefile_shape
wkt
wkb
ewkb
```

`shapefile_shape` 只用于 Shapefile 同构且无需 CRS 转换的 native passthrough；writer 必须校验 native shape type 与目标 `.shp` shape type 一致。`wkb_hex`、`ewkb_hex` 可作为 `common/spatial` 输入解析能力保留，不要求 Shapefile writer 第一阶段在能力声明中全部列出。可选：如果要支持 GeoJSON geometry object，应作为 writer 本地增强能力声明，不作为 Transfer 首选路径。

## Native Engine 改造要求

native table reader / writer 应避免把空间列变成各自私有形态后再交给 Transfer。

推荐：

- PostgreSQL read session 支持 `geometry_encoding=ewkb`。
- PostgreSQL write session 支持 EWKB 写入。
- 如果 read session 支持 `geometry_target_srid` 并声明 `ReadTransform=true`，Transfer 可优先用源端转换。
- 不支持空间编码能力的 native engine，在空间 table Transfer 中应被 planner 拒绝，而不是执行阶段失败。
- native engine 不需要声明越多编码越好。第一阶段优先声明和实现 `ewkb`；已有 `geojson` read 能力可保留。

## 测试矩阵

当前至少覆盖以下测试：

| 场景 | 期望 |
| --- | --- |
| GeoJSON reader + `GeometryEncodingEWKB` | geometry 字段为 `[]byte`，可被 `common/spatial.ParseGeometryValue` 解析。 |
| GeoJSON writer 写 EWKB bytes | 输出合法 FeatureCollection。 |
| GeoJSON -> Shapefile planner | source parse options 请求 EWKB，target writer 接受 EWKB，无需特殊硬编码。 |
| Shapefile -> GeoJSON planner | source parse options 请求 EWKB；若 CRS 非 4326，插入 reproject。 |
| GeoJSON(4326) -> Shapefile | 不插入 reproject，直接 EWKB 写出。 |
| Shapefile -> Shapefile | CRS 不变且无需 geometry transform 时，source parse options 请求 `shapefile_shape`。 |
| Shapefile(3857) -> GeoJSON | 插入 Arrow + EWKB reproject。 |
| 编码无法匹配 | planner 阶段失败，错误包含 source/target 编码集合。 |
| reproject 输入非 EWKB | planner 阶段失败，不调用算子。 |

## 迁移步骤

1. 已确认 GeometryEncoding 枚举、row value 类型和能力声明位置。
2. 已在 `common/spatial` 补齐 GeoJSON geometry object <-> `geom.T` / WKB / EWKB 的编码转换。
3. GeoJSON reader 已支持 `ParseOptions.GeometryEncoding=geojson|ewkb`。
4. GeoJSON writer 已支持 GeoJSON geometry object 和 `[]byte(EWKB)`。
5. Shapefile 插件已声明并实现 `shapefile_shape|wkt|wkb|ewkb` 读写编码。
6. GeoJSON 插件已声明空间读写编码能力。
7. native table engine 已支持空间编码能力声明；PostgreSQL 已落地 `ewkb` read/write、`geojson` read 和 source-native read transform。
8. Transfer planner 已增加空间 row encoding 协商。
9. Transfer planner 已将 `target=geojson` reproject 逻辑收敛到目标 CRS 约束；第一阶段 reproject payload 只走 Arrow + EWKB。
10. 编码选择已通过 format / native table capability 决策；不得新增仅靠具体 engine / format 组合触发的临时编码分支。

## 后续增强点

1. `wkb_hex` / `ewkb_hex` 暂作为 `common/spatial` 可解析 / 可输出变体，不要求所有 format / engine 声明；确有跨模块调用需求时再纳入能力声明。
2. Format 空间编码能力当前使用可选接口 `SpatialEncodingCapabilityProvider`。待规范稳定后，可评估是否沉淀到 `FormatDescriptor` 静态字段。
3. Shapefile writer 第一阶段不直接支持 GeoJSON geometry object。主路径是 GeoJSON reader 输出 EWKB，Shapefile writer 消费 EWKB，避免把便利兜底变成新的隐式协议。
