# ADDP 数据类型与文件格式待规范事项

更新时间：2026-05-05

本文记录“数据类型、文件格式、组合形态、扩展语义、attributes 治理”后续推进中需要先讨论并形成规范的事项。  
这些事项会影响平台级边界、插件接入方式、模型归属或长期演进方向，不应在未确认规范前直接实现。

相关文档：

- `docs/next/addp数据类型与文件格式概念规范.md`
- `docs/next/addp数据类型与文件格式落地指南.md`
- `docs/next/addp数据类型与文件格式改进清单.md`
- `docs/spec/addp数据格式扩展指南.md`
- `docs/spec/addp引擎插件接口规范.md`
- `docs/spec/addp引擎能力声明规范.md`

## 一、第三方插件扩展声明机制

### 背景

现有 attributes 已采用：

```json
{
  "schema_version": 1,
  "storage": {},
  "item": {},
  "schema": {},
  "extensions": {}
}
```

并已约束标准扩展命名空间：

- `spatial`
- `media`
- `document`
- `statistics`
- `extraction`
- `unqualified`

私有扩展目前要求使用反向域名或插件 ID 形式，但缺少显式声明机制。  
后续如果允许第三方 parser、extractor、detector 或内容预览插件写入私有扩展，就必须先定义“插件声明什么、平台校验什么、哪些字段可被消费”。

### 待确认问题

1. 插件 manifest 是否需要统一声明 `extension_namespaces`。
2. 私有命名空间命名规则是否只允许反向域名，还是也允许平台插件 ID。
3. 每个扩展字段是否需要声明：
   - 字段名
   - 类型
   - 来源 parser / extractor
   - 是否可展示
   - 是否可索引
   - 是否可用于诊断
   - 是否允许参与预览或转换能力判断
4. 私有扩展被平台稳定消费时，晋升为标准扩展的流程是什么。
5. 插件输出字段和平台标准字段冲突时，normalizer 如何记录冲突原因。

### 倾向方案

先引入轻量声明，不做全字段强 schema：

```json
{
  "extension_namespaces": [
    {
      "name": "com.vendor.format_x",
      "owner": "vendor-format-x",
      "version": 1,
      "fields": [
        {
          "name": "sensor_model",
          "type": "string",
          "display": true,
          "index": false
        }
      ]
    }
  ]
}
```

平台行为仍只能依赖 `storage`、`item`、`schema` 和平台标准扩展。  
私有扩展默认只用于展示、诊断或插件自身处理，不能直接决定平台主路由。

## 二、Manager 内容预览插件能力描述

### 背景

`manager` 主 provider 已按 Meta 标准属性确定性路由。  
对象内容插件已经优先按 Meta 标准 `format` 匹配，`extension` / `content_type` 只作为迁移期兜底。

但内容插件仍存在自身能力描述问题：

- `priority` 是否继续存在。
- `match.extensions` / `match.content_types` 何时允许参与匹配。
- 命令型插件 payload 可以携带 `format`，但未声明其扩展字段和能力边界。
- 多文件、容器文件、目录树 item 的内容处理能力如何表达。

### 待确认问题

1. 内容插件是否必须声明支持的 `data_family`、`format`、`composition_type`。
2. `priority` 是否仅允许在同一标准匹配结果内解决冲突，而不参与语义抢路由。
3. `extension` / `content_type` 兜底何时删除。
4. 内容插件是否允许声明私有扩展字段用于展示。
5. 命令型插件是否需要声明输入 payload schema 和输出 content schema。
6. 多文件内容插件是否统一使用 `entry_path` + `component_files`，是否允许自行枚举 sibling。

### 倾向方案

内容插件 manifest 分层：

- `match.standard`：只基于 Meta 标准属性。
- `match.legacy`：迁移期扩展名和 Content-Type 兜底，标记为可删除。
- `capabilities`：声明预览输出 kind、是否支持 stream、是否支持 composite。
- `extensions`：声明插件产出的私有扩展命名空间。

示例：

```json
{
  "name": "builtin:content-shapefile",
  "type": "builtin",
  "match": {
    "standard": {
      "data_families": ["tabular"],
      "formats": ["shapefile"],
      "composition_types": ["multi_file"]
    },
    "legacy": {
      "extensions": [".shp"],
      "content_types": []
    }
  },
  "capabilities": {
    "preview_kinds": ["table", "geojson"],
    "stream": true,
    "composite": true
  }
}
```

## 三、是否引入“引擎原生 item”组合形态

### 背景

当前 `common/dataitem` 主要面向文件、对象和目录树。  
数据库表、文档集合、图 label / relationship 不是文件组合，但也是 MetaItem。

现有扫描中，数据库表仍通过旧适配层做边界转换，例如：

- `common/engine/plugin.TableInfo`
- `common/format.ScannerTableInfo`
- `common/format.ScannerFieldInfo`
- `common/format.TableInfo`

如果把所有 item 都强行塞进文件组合形态，会造成概念混乱；但如果数据库和集合完全绕过 `dataitem`，又可能导致 `attributes.item` 的统一性不足。

### 待确认问题

1. 是否新增 `CompositionTypeEngineNative` 或类似概念。
2. 关系型表、文档集合、图 label / relationship 是否都归入该组合形态。
3. `engine_native` 的 `entry_path`、`component_files`、`physical_path` 应如何表达。
4. 这类 item 的 `format` 是数据库引擎类型、逻辑格式，还是保留为空。
5. `data_family` 对文档集合、图节点/边的表达是否需要扩充。

### 倾向方案

先不要把数据库表伪装成文件组合。  
可以考虑引入：

```text
composition_type = engine_native
```

用于表达由引擎 catalog 原生暴露的 item。  
但需要先明确它不是文件格式，也不要求 `component_files`。

## 四、TableInfo / ObjectInfo / Scanner* 模型收口

### 背景

概念规范已经明确：

- `TableInfo` 描述表格型数据。
- `FieldInfo` 只属于结构化数据语义，应挂在 `TableInfo` 下。
- `ObjectInfo` 描述图片、视频、PDF 等对象型数据，不承载字段 schema 语义。

代码中仍存在多套平行模型：

- `common/engine/plugin.TableInfo`
- `common/engine/plugin.ObjectInfo`
- `common/format.TableInfo`
- `common/format.ObjectInfo`
- `common/format.ScannerTableInfo`
- `common/format.ScannerFieldInfo`

### 待确认问题

1. 哪个模型作为平台内部 canonical model。
2. `plugin.TableInfo` 是否只作为引擎 catalog 返回 DTO，还是也承载结构元数据。
3. `ScannerTableInfo` / `ScannerFieldInfo` 是否删除，还是保留为迁移适配层。
4. 文档集合的采样 schema 是否统一进入 `common/format.TableInfo`。
5. 图 label / relationship 是否也用 `TableInfo` 表达结构属性，还是保留独立 schema model。

### 倾向方案

以 `common/format.TableInfo`、`common/format.FieldInfo`、`common/format.ObjectInfo` 作为解析结果 canonical model。  
引擎插件层的 `TableInfo` / `ObjectInfo` 只保留 catalog 基础信息。  
`Scanner*` 模型作为迁移层逐步删除。

## 五、Registry 与能力发现层收口

### 背景

当前至少存在这些 registry：

- `common/format` parser / detector registry
- `common/dataitem` detector registry
- `common/engine/plugin` registry
- `manager.PreviewRegistry`
- `manager.ObjectContentRegistry`

它们分别解决不同问题，但边界尚未完全固化。  
如果直接“统一 registry”，可能把组合形态识别、格式解析、引擎能力、预览能力混成一个大而模糊的注册中心。

### 待确认问题

1. 是否需要一个统一的能力发现 API，而不是一个统一 registry。
2. 各 registry 是否保留职责边界：
   - engine registry：连接和 catalog 能力
   - dataitem registry：组合形态 detector
   - format registry：parser / extractor
   - preview registry：已识别 item 的展示能力
3. 能力发现结果是否由 `meta` 落库，还是各模块运行时查询。
4. 插件加载顺序和冲突处理如何记录。
5. 第三方插件是否可以同时注册 detector、parser、extractor、preview handler。

### 倾向方案

保留多个职责清晰的 registry，新增统一“能力声明/发现视图”。  
平台通过能力视图回答“支持哪些格式、组合形态、扩展、预览方式”，但不把所有注册逻辑合并成一个入口。

## 六、空间扩展标准口径

### 背景

空间应统一作为：

```text
attributes.extensions.spatial
```

当前仍需要统一：

- `SpatialInfo` 的字段结构。
- shapefile、geojson、postgresql、影像空间元数据映射。
- 哪些预览能力依赖空间扩展。
- 如何避免按格式或固定字段名判断空间能力。

### 待确认问题

1. 标准空间扩展字段是否固定为：
   - `geometry_column`
   - `geometry_type`
   - `srid`
   - `bbox`
   - `crs`
   - `feature_count`
2. 多几何字段如何表达。
3. 栅格影像空间元数据是否使用同一个 `spatial`，还是在 `media`/`raster` 中补充。
4. PostGIS 表、Shapefile、GeoJSON、GeoTIFF 的空间字段映射是否统一。
5. Manager 空间预览能力依赖哪些字段，缺失时如何降级。

### 倾向方案

先定义 `extensions.spatial` 的最小稳定字段集。  
格式私有空间细节进入格式或插件私有命名空间，只有跨格式稳定消费的字段才晋升到 `spatial` 标准字段。

## 七、建议讨论顺序

1. 先确认第三方扩展声明机制。
2. 再确认 Manager 内容插件能力描述。
3. 然后决定是否引入 `engine_native` 组合形态。
4. 接着收口 `TableInfo` / `ObjectInfo` / `Scanner*`。
5. 最后处理 registry 与统一能力发现层。
6. 空间扩展标准口径可并行讨论，但落地前应先形成最小字段集。

## 八、当前可继续开展但不阻塞规范的事项

在上述规范确认前，仍可继续做：

- 清理旧平铺读取，改为读取标准 attributes 分区。
- 补齐已有标准扩展的明确字段映射。
- 为已经有清晰规则的多文件、目录树、容器文件补 detector。
- 增加 normalizer 回归测试。
- 清理 `manager` 中不影响插件声明机制的调试输出和历史兜底。
