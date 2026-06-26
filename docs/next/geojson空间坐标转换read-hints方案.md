# GeoJSON 标准化输出与空间坐标转换方案

> 状态：阶段性落地方案。本文用于记录 GeoJSON 导出、空间坐标参考转换、Transfer 规划和 Python Workflow 矢量算子的当前主路径；稳定约束后续应分别进入 `docs/spec/` 中的格式能力、引擎能力、Transfer 任务和工作流算子规范。

> 核心结论：ADDP 只保留一条主语义路线，即“导出标准 GeoJSON 前必须得到符合 GeoJSON 坐标约定的几何数据”。Transfer 负责表达目标格式约束和选择执行策略；Python Workflow 只承担几何坐标转换算子；PostGIS `ST_Transform` 等源端能力只是该语义下的优化实现，不形成长期双轨。

## 背景

GeoJSON 按现代通用约定应使用 WGS84 经纬度坐标，坐标语义等价于 CRS84 / EPSG:4326。ADDP 当前从 PostgreSQL 空间表导出 GeoJSON 时，如果源 geometry 是非 4326 投影坐标，现有链路会直接使用源坐标生成 GeoJSON：

```sql
ST_AsGeoJSON(geom)::json
```

这会导致 GeoJSON 文件内容仍是投影坐标，但 Meta scan 和前端地图按 4326 使用，最终出现空间范围异常、地图无法定位的问题。

这个问题不能简单放到 GeoJSON writer 中解决。GeoJSON writer 只负责把已经准备好的行数据编码为 GeoJSON，缺少源 engine 能力、源 CRS、转换函数和任务策略上下文。坐标转换应作为 Transfer 规划中的目标格式约束，由具备能力的执行者完成。

## 当前代码事实

### Transfer planner

`transfer/backend/internal/planner/table_export.go` 已在目标 GeoJSON 时生成目标 CRS 约束和 source read options：

```go
readOptionsForGeoJSONTarget(targetPlan.Format, targetPlan.FormatOptions, sourcePlan.SpatialInfo)
```

当前会注入：

```text
geometry_encoding=geojson
geometry_field=<可选>
geometry_target_srid=4326
geometry_transform_policy=required
```

目标格式是 GeoJSON 时，planner 必须明确目标 CRS、转换策略和可用执行路径；如果不能满足标准 GeoJSON 的 4326 约束，应在规划或执行阶段明确失败。

### PostgreSQL engine

`common/engine/plugins/postgresql/table_read_session.go` 当前在 `geometry_encoding=geojson` 时会把空间列读成 GeoJSON：

```sql
ST_AsGeoJSON("geom")::json AS "geom"
```

如果 planner 明确要求输出目标 SRID，PostgreSQL / PostGIS 可以用源端原生能力优化为：

```sql
ST_AsGeoJSON(ST_Transform("geom", 4326))::json AS "geom"
```

前提是源列 SRID 可确定，且 PostGIS 支持转换。

### GeoJSON format

`common/format/plugins/geojson/plugin.go` 的 writer 只消费行中的 geometry 字段，并把它放入 Feature 的 `geometry`：

```go
feature := map[string]interface{}{
    "type":       "Feature",
    "geometry":   geometry,
    "properties": properties,
}
```

GeoJSON writer 不应承担 CRS 判断和重投影职责。

### Python Workflow

`engines/python-workflow` 已使用 GeoPandas / Shapely，并已提供 `vector_reproject` 几何坐标转换算子。Transfer 通过 direct binary 调用单算子，不让 Python 接管完整外部数据 load/save。

Python Workflow 不应接管 Transfer 的整套外部数据 load/save。它应作为纯几何算子运行：接收 Transfer 传入的几何批数据，执行 CRS 转换，只返回转换后的几何批数据。

### Shapefile format

Shapefile reader 能从 `.prj` 读取 CRS definition，并尽量解析 SRID，也能把 SRID 写入 EWKB。但当前 common format 层没有通用 PROJ 坐标转换能力，因此 Shapefile -> GeoJSON 不能由 GeoJSON writer 或 Shapefile reader 隐式完成。

## 设计原则

1. GeoJSON 的目标 CRS 约束属于 Transfer 规划语义，不属于 GeoJSON writer 私有选项。
2. Transfer 负责识别目标格式约束、读取源空间事实、选择执行策略和维护任务状态。
3. Transfer 不实现具体 CRS 转换算法，不直接依赖 PostGIS / GeoPandas / PROJ 的计算细节。
4. Python Workflow 只承担几何坐标转换等功能算子，不接管 ADDP engine / format 的完整 load/save 主链路。
5. PostGIS 源端转换是 `vector_reproject` 语义下的高性能优化，不是另一条业务路线。
6. 源 CRS 不确定时不得静默伪装为 4326；必须要求用户补充源 CRS，或在 required 策略下失败。
7. 不允许为 PG、Shapefile 或某个格式写临时分支；能力判断必须来自 engine capabilities、format facts、Meta attributes 或算子契约。

## 责任边界

```text
Meta scan
  -> 识别 GeoJSON / Shapefile / GeoPackage / 原生表的空间事实
  -> 记录 CRS、SRID、extent、geometry column 和风险提示

Transfer
  -> 识别目标格式约束，例如 GeoJSON 需要 EPSG:4326 / CRS84
  -> 选择执行策略：源端转换、Python 矢量算子、或失败
  -> 继续负责外部源和目标的读写、任务状态、错误处理和审计

Source engine / format
  -> 提供原生读取、写入和空间事实
  -> 具备能力时可以按 read hints 执行源端转换

Python Workflow
  -> 提供 vector_reproject 等几何转换功能算子
  -> 不直接成为 Transfer 外部数据连接、权限、格式读写的第二事实源

GeoJSON writer
  -> 只负责编码标准 GeoJSON FeatureCollection
  -> 不隐式猜测或转换 CRS
```

## Transfer 规划模型

Transfer 已在计划阶段生成目标格式约束，而不是只生成底层读写 options。

概念性计划字段：

```text
target_format = geojson
target_spatial_crs = EPSG:4326
spatial_transform_policy = required | best_effort | none
spatial_transform_strategy = source_native | python_vector_operator | none
```

字段语义：

| 字段 | 说明 |
| --- | --- |
| `target_spatial_crs` | 目标格式要求或用户指定的输出 CRS；GeoJSON 默认 `EPSG:4326`。 |
| `spatial_transform_policy` | 转换策略。`required` 表示必须满足目标 CRS；`best_effort` 表示能转则转，不能转时保留事实并提示；`none` 表示用户明确不转换。 |
| `spatial_transform_strategy` | planner 选择的执行策略，不应由用户直接指定为 engine 类型或实现细节。 |

GeoJSON 导出默认使用：

```text
target_spatial_crs=EPSG:4326
spatial_transform_policy=required
```

原因是 GeoJSON 的主要消费方，包括 Meta scan、Manager 地图预览和前端地图组件，都会按经纬度坐标理解内容。继续默认 best-effort 原样导出，容易生成看似成功但语义错误的数据。

如果确实需要导出非标准 CRS 的 GeoJSON，应作为显式高级选项，例如：

```text
allow_non_standard_geojson_crs=true
spatial_transform_policy=none
```

该选项第一阶段不建议实现为默认主路径。

## 执行策略

### source_native

适用于源端具备原生空间转换能力的场景，例如 PostgreSQL / PostGIS。

执行特点：

- Transfer 仍走当前表读取和 GeoJSON writer 主链路。
- planner 通过 read hints 请求源端把 geometry 转为目标 CRS。
- 源端直接输出 GeoJSON geometry value，避免大表先落到 Python。

这是 PG native table -> GeoJSON 的第一优先策略。

### python_vector_operator

适用于源端不能原生转换，但数据规模和运行时成本适合 Python Workflow 的场景，例如 Shapefile、已有 GeoJSON 文件、PostGIS 查询结果批等。

执行特点：

- Python 只承担几何批数据的 `vector_reproject`。
- Transfer 保留原始属性字段和写目标格式的责任。
- Python 输入只包含几何列和转换上下文，不包含其他字段。
- 失败直接报错，不做 GPKG 或其他中间产物降级。

第一阶段不建议让 Python 直接读取任意 ADDP engine locator。否则 Python 会复制 Transfer 的外部连接、权限、格式识别和写入语义。

### none

适用于以下场景：

- 源数据已经满足目标 CRS。
- 用户明确选择不转换，且目标格式允许该行为。
- planner 无可用转换策略，并且 policy 允许 best-effort 原样输出。

对标准 GeoJSON 导出，`none` 只能在源 CRS 已是 4326 时作为正常策略；源 CRS 非 4326 时不应作为默认降级。

## `vector_reproject` 算子契约

`vector_reproject` 是 CRS 转换的核心几何算子。它只解决“已有几何需要转换坐标参考”的问题，不解决“如何连接任意外部数据源并写入任意目标”的问题。

### 算子元数据

```text
name: vector_reproject
engine_type: python_workflow
category: 几何转换
execution_modes: ["workflow", "direct"]
```

`workflow` 是算子本体语义；`direct` 是 Transfer 受控调用单算子的运行态入口。两者共享同一几何转换逻辑，但 `direct` 只面向几何批，不接管 load/save。

### Workflow 输入参数

| 参数 | 类型 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- | --- |
| `input_gdf` | `GeoDataFrame` | 是 | 无 | 输入几何批，必须包含活动 geometry 列。 |
| `source_crs` | `string` | 条件必填 | 无 | 源 CRS。若 `input_gdf.crs` 为空，则必须提供；若 `input_gdf.crs` 已存在且与该值冲突，默认报错。 |
| `target_crs` | `string` | 否 | `EPSG:4326` | 目标 CRS。GeoJSON 标准化输出默认使用 `EPSG:4326`。 |

### Direct 输入输出参数

Transfer 侧的受控中间矢量产物只传 geometry，不传其他字段。当前主路径使用 Arrow IPC + EWKB geometry batch。

| 参数 | 类型 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- | --- |
| `binary_payload` | `bytes` | 是 | 无 | Arrow IPC 流，必须是单列几何批。 |
| `binary_payload.content_type` | `string` | 是 | 无 | 固定为 `application/vnd.apache.arrow.stream`。 |
| `binary_payload.encoding` | `string` | 是 | `arrow` | 传输编码。 |
| `binary_payload.name` | `string` | 是 | `geometry_batch` | 批名称，仅用于调度和诊断。 |
| `binary_payload.metadata.geometry_column` | `string` | 是 | 无 | 几何列名。 |
| `binary_payload.metadata.geometry_encoding` | `string` | 是 | `ewkb` | 几何编码。当前主路径仅接受 `ewkb`。 |
| `binary_payload.metadata.source_crs` | `string` | 条件必填 | 无 | 源 CRS。 |
| `binary_payload.metadata.target_crs` | `string` | 否 | `EPSG:4326` | 目标 CRS。 |

### Direct 输出

| 端口 | 类型 | 说明 |
| --- | --- | --- |
| `binary_payload` | `bytes` | 转换后的 Arrow IPC geometry batch。长度必须与输入一致，只返回 geometry，不返回其他属性字段。 |

输出 Arrow batch 的 schema metadata 必须保持自洽：`geometry_encoding=ewkb`，`source_crs` 和 `target_crs` 均写入转换后的当前 CRS（通常为 `EPSG:4326`）。不要在输出 batch 的 `source_crs` 中继续保留输入 CRS，否则下游 decoder 会把已转换坐标误标为旧 CRS。

### 行为规则

1. 输入必须是几何批数据，不得夹带其他属性字段。
2. 如果几何数组为空或长度非法，返回明确错误。
3. 如果 `geometry_encoding` 不是 `ewkb`，返回明确错误。
4. 如果源 CRS 缺失且未提供 `source_crs`，返回明确错误。
5. 如果转换失败，例如 CRS 字符串无法解析、PROJ 缺少定义或 geometry 无效导致失败，返回明确错误，不得原样返回并伪装成功。
6. 输出只返回转换后的几何数组；其他字段由 Transfer 在外层批次中原样保留和回填。

### 与 Transfer 的关系

Transfer 的批次职责不变：批次、每批数量、checkpoint、属性字段、写出和错误恢复仍由 Transfer 自己控制。Python 只处理 geometry payload。

受控中间矢量产物定义为：

```text
单列 Arrow IPC stream
  - 列类型：binary
  - 列值：EWKB 几何字节
  - schema metadata：geometry_column / geometry_encoding / source_crs / target_crs
  - 输出 batch 的 source_crs / target_crs 均表示输出几何当前 CRS
```

Transfer 的处理流程如下：

```text
Transfer 读取 source batch
  -> 提取几何列，编码成 Arrow IPC + EWKB geometry batch
  -> 调用 Python vector_reproject 的 direct 入口
  -> 取回转换后的几何 batch
  -> 把几何回填到原批次
  -> writer 写 target batch / GeoJSON
```

这个过程不需要 GPKG 中间产物，也不需要 Python 接收其他字段。

## read hints 作为源端优化

read hints 只用于 source native strategy，不是 GeoJSON 转换的主概念。

`common/engine/plugin` 中定义稳定 key，避免散落字符串：

```go
const (
    TableReadHintGeometryEncoding        = "geometry_encoding"
    TableReadHintGeometryField           = "geometry_field"
    TableReadHintGeometryTargetSRID      = "geometry_target_srid"
    TableReadHintGeometryTransformPolicy = "geometry_transform_policy"
)

const (
    GeometryTransformPolicyNone     = "none"
    GeometryTransformPolicyRequired = "required"
)
```

字段语义：

| hint | 示例 | 说明 |
| --- | --- | --- |
| `geometry_encoding` | `geojson` | 要求源端把 geometry 字段读成 GeoJSON geometry value。 |
| `geometry_field` | `geom` | 指定空间字段；缺省时由空间元数据或目标 format options 决定。 |
| `geometry_target_srid` | `4326` | 请求源端将输出坐标转换到目标 SRID。 |
| `geometry_transform_policy` | `required` | 坐标转换策略。GeoJSON 标准导出使用 required。 |

`best_effort` 不进入当前主路径。GeoJSON 导出中 best-effort 容易生成“任务成功但地图不可用”的结果。如果后续确有业务价值，应作为显式高级策略补规范。

## 能力声明

只靠 `engine_type == postgresql` 判断会继续制造隐式分支。engine capabilities 已新增明确能力：

```go
type StoreCapability struct {
    // ...
    TableReadSpatialTransform bool `json:"table_read_spatial_transform,omitempty"`
}
```

含义：

- engine 的 table read session 能在读取表数据时对空间列执行 CRS 转换。
- 该能力不等于 engine 有空间 facts；空间 facts 只说明能描述 SRID、extent、geometry column。
- PostgreSQL / PostGIS 声明 true。
- NFS、MinIO、Shapefile format 本身不属于 native table engine，不声明该能力。

planner 应优先根据能力决定是否选择 `source_native`。如果能力未知，不应猜测。

## PostgreSQL 实现行为

PostgreSQL read session 收到：

```text
geometry_encoding=geojson
geometry_target_srid=4326
geometry_transform_policy=required
```

应执行：

1. 确定空间列。
2. 确定源 SRID：
   - 优先使用 `geometry(<type>,<srid>)` typmod。
   - typmod 不含 SRID 时，可查询样本 `ST_SRID(geom)`。
   - 如仍未知，视为不可转换。
3. 如果源 SRID 已是 4326，使用 `ST_AsGeoJSON(geom)`。
4. 如果源 SRID 非 4326 且可转换，使用 `ST_AsGeoJSON(ST_Transform(geom, 4326))`。
5. 如果不可转换，返回明确错误。

PostgreSQL engine 不应自行因为目标格式是 GeoJSON 而猜测要转 4326；它只响应 planner 明确传入的 read hints。

如果 planner 选择源端转换，Transfer 中后续写出链路看到的 `SpatialInfo` 也必须同步为输出 CRS，例如 `EPSG:4326`。未重算的源 `extent`、源空间索引状态和索引名不得继承到输出空间事实中，避免把源坐标范围或源物理索引误标为转换后的结果事实。

## GeoJSON writer 行为

GeoJSON writer 不执行坐标转换。

- 接收 geometry value 并写出 GeoJSON。
- 通过 `WriteOptions.SpatialInfo` 或 format options 确定 geometry field。
- 不写旧版 GeoJSON `crs` 字段。
- 如果上游明确传入 `SpatialInfo` 且显示非 4326，必须返回错误；GeoJSON writer 是最后一道格式约束门禁，不执行隐式坐标转换。

## Meta scan 配套行为

Meta scan 不应因为文件格式是 GeoJSON 就无条件标记 SRID=4326。

当前规则：

1. 如果 GeoJSON 内存在可识别 CRS / SRID，按实际值记录。
2. 如果没有 CRS 信息但 bbox 明显落在经纬度范围内，可按 GeoJSON 默认语义标记 4326。
3. 如果 bbox 明显越界，例如 x 不在 `[-180,180]` 或 y 不在 `[-90,90]`，不得标记为 4326；应记录：

```text
srid = unknown
format_info.geojson.coordinate_range_out_of_wgs84 = true
```

4. 前端地图遇到 `coordinate_range_out_of_wgs84=true` 或未知 SRID，应提示“坐标系未知或非 WGS84，无法直接定位”，而不是按 4326 强行显示。

## 做不到时的统一处理

| 场景 | required 行为 |
| --- | --- |
| PG 可确定源 SRID，支持 `ST_Transform` | 源端转换到 4326。 |
| PG 源 SRID 未知 | 失败，提示需要补充源 CRS 或修复源表 SRID。 |
| 源 engine 不声明空间转换能力 | 不走 source native；可尝试 Python strategy，若不可用则失败。 |
| Shapefile 有 `.prj` 但无 source native 能力 | 不由 format reader 隐式转换；走 Python geometry batch strategy，若该策略不可用则失败。 |
| Shapefile 无 `.prj` | 失败，要求用户显式提供 source CRS。 |
| GeoJSON bbox 越界且无 CRS | Meta 标记未知 / 越界；Transfer 标准化导出要求用户补 source CRS 后再转换。 |
| GeoJSON writer 收到非 4326 空间事实 | 不转换；必须返回错误，要求上游先转换。 |

## 落地状态

### 第一阶段：规范和 PG 主链路

1. 已将目标格式 CRS 约束、转换策略、read hints、能力声明沉淀到正式规范。
2. `common/engine/plugin` 已增加 read hint 常量和 `TableReadSpatialTransform` / `table_spatial_encoding.read_transform` 能力字段。
3. PostgreSQL capabilities 已声明源端空间转换能力。
4. Transfer planner 对 PG native table -> GeoJSON 选择 `source_native`，注入 `geometry_target_srid=4326` 和 `required`。
5. PostgreSQL read session 已实现 `ST_Transform(..., 4326)`。
6. PG 非 4326 空间表导出 GeoJSON 已有集成测试覆盖。

### 第二阶段：Python 几何 batch 算子

1. Python Workflow 已增加 `vector_reproject` 几何 batch 算子。
2. 算子输入只接 Arrow IPC + EWKB 几何数组和 CRS 上下文。
3. 算子输出只返回转换后的 EWKB 几何数组。
4. 工作流算子元数据和 direct binary 契约已有测试覆盖。
5. Transfer 已验证 `batch -> Python vector_reproject -> batch write` 的受控链路。

### 第三阶段：Meta scan 防误判

1. GeoJSON bbox 越界时不再默认标记 4326。
2. `format_info.geojson.coordinate_range_out_of_wgs84=true` 用于记录坐标越界提示。
3. 前端空间能力展示和地图预览应基于该提示或未知 SRID 给出不可定位说明。

## 暂不建议

1. 不建议让 Python Workflow 接管 Transfer 的完整外部数据 load/save。
2. 不建议让 Python 算子接收其他属性字段。
3. 不建议让 GeoJSON writer 自动猜测并转换坐标。
4. 不建议根据 `engine_type` 在 planner 中硬编码 PG 特例。
5. 不建议在 Shapefile format reader 中私自引入 PROJ 转换。
6. 不建议默认 best-effort 导出非 4326 GeoJSON。
7. 不建议在 GeoJSON 文件中写旧版 `crs` 字段作为主方案。
