# ADDP 数据类型与文件格式后续规范事项

更新时间：2026-05-06

本文只记录“数据类型、文件格式、组合形态、扩展语义、attributes 治理”中尚未形成正式规范、需要先讨论确认的事项。已确认的概念和实现规则已并入：

- [ADDP 数据类型与格式体系图](../concepts/addp数据类型与格式体系图.md)
- [ADDP 数据类型与文件格式规范](../spec/addp数据格式扩展指南.md)

这些事项会影响平台级边界、插件接入方式、模型归属或长期演进方向，未确认前不应直接进入开发。

## 一、容器文件内部对象是否展开为子 item

### 背景

SQLite、GeoPackage、Excel 等属于 `container_file`。当前正式规范要求容器文件本身先作为一条 meta item，内部表、图层、sheet 优先写入 attributes。

### 待确认

1. Excel sheet 是否生成子 meta item。
2. SQLite table / view 是否生成子 meta item。
3. GeoPackage layer / tile matrix 是否生成子 meta item。
4. 子 item 的 `name/full_name/node_id/fingerprint` 如何生成。
5. 子 item 与容器 item 的父子关系使用 `MetaNode`、`MetaItem` 关系表，还是 attributes 内部结构。
6. Manager 和 Transfer 是消费容器 item，还是消费内部子 item。

### 倾向

先保持“容器文件一条 item，内部对象写入 attributes”。只有当 Manager / Transfer 明确需要对内部对象独立授权、检索、预览或传输时，再设计子 item 规范。

## 二、directory-tree 的 explain / confidence

### 背景

`directory_tree` detector 可以声明 `Exclusive=true`，这会停止继续处理目录下剩余资源。该能力风险高，必须有可审计依据。

### 待确认

1. `explain` / `confidence` 是否入库。
2. 如果入库，写入 `attributes.item`、`extensions.builtin.<format>`，还是扫描日志。
3. `confidence` 枚举是否使用 `exact`、`strong`、`weak`。
4. 弱匹配是否允许生成候选 item，还是只记录诊断。
5. 目录下存在未认领异类资源时，是否一律拒绝独占。

### 倾向

规则判断仍以确定性结构为准。`explain` 用于诊断和审计，`confidence` 只用于冲突处理和防止弱匹配独占目录，不替代格式规则。

## 三、对象存储跨层组件认领

### 背景

对象存储没有真实目录，只有 object key 和 prefix。某些格式的 manifest、数据文件、索引文件可能分布在不同层级。

### 待确认

1. 跨层组件是否只允许 `directory_tree` 和 `mixed_collection` 使用。
2. manifest 引用的资源如何表达 claimed resources。
3. claimed resources 使用完整 object key、catalog path，还是统一资源标识。
4. 跨 bucket / sibling prefix 引用是否允许。
5. 被跨层认领的 object 是否禁止作为普通 object 落库。

### 倾向

默认 sibling multi-file 只认领同 prefix 的直接兄弟对象。跨层认领必须由格式实现层显式声明，统一 detector 框架不能自行猜测。

## 四、mixed_collection 组合形态

### 背景

遥感影像镶嵌、专业软件工程数据包、主文件加资源目录等，可能既不是简单多文件，也不是单纯目录树。

### 待确认

1. `mixed_collection` 的入口资源是 manifest、主文件还是集合根目录。
2. `name/full_name` 取 manifest 路径、根路径还是格式定义的数据集名。
3. 是否允许局部独占目录。
4. 组件资源如何分组、排序和去重。
5. Manager / Transfer 如何读取集合入口和组件列表。

### 倾向

先按具体格式逐个形成规范，不抽象出过度通用的集合匹配器。

## 五、第三方插件扩展声明机制

### 背景

attributes 已约束标准扩展命名空间：`spatial`、`media`、`document`、`statistics`、`extraction`。私有扩展要求使用反向域名或插件 ID 形式，但缺少显式声明机制。

### 待确认

1. 插件 manifest 是否统一声明 `extension_namespaces`。
2. 私有命名空间命名规则是否只允许反向域名，还是允许平台插件 ID。
3. 每个扩展字段是否声明类型、来源、是否可展示、是否可索引、是否可诊断。
4. 私有扩展被平台稳定消费时，晋升为标准扩展的流程是什么。
5. 插件输出字段和平台标准字段冲突时，normalizer 如何记录冲突。

### 倾向

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

平台行为仍只能依赖标准 attributes 分区和平台标准扩展。私有扩展默认只用于展示、诊断或插件自身处理。

## 六、Manager 内容预览插件能力描述

### 背景

Manager 主 provider 已按 Meta 标准属性确定性路由，但对象内容插件仍需要清晰能力声明。

### 待确认

1. 内容插件是否必须声明支持的 `data_family`、`format`、`composition_type`。
2. `priority` 是否仅允许在同一标准匹配结果内解决冲突。
3. `extension` / `content_type` 兜底何时删除。
4. 内容插件是否允许声明私有扩展字段用于展示。
5. 命令型插件是否需要声明输入 payload schema 和输出 content schema。
6. 多文件内容插件是否统一使用 `entry_path` + `component_files`，是否允许自行枚举 sibling。

### 倾向

内容插件 manifest 分层：

- `match.standard`：只基于 Meta 标准属性。
- `match.legacy`：迁移期扩展名和 Content-Type 兜底，标记为可删除。
- `capabilities`：声明预览输出 kind、stream、composite 等能力。
- `extensions`：声明插件产出的私有扩展命名空间。

## 七、是否引入 engine_native 组合形态

### 背景

数据库表、文档集合、图 label / relationship 不是文件组合，但也是 MetaItem。它们当前主要由引擎 catalog 原生暴露边界。

### 待确认

1. 是否新增 `composition_type=engine_native`。
2. 关系型表、文档集合、图 label / relationship 是否都归入该组合形态。
3. `engine_native` 的 `entry_path`、`component_files`、`physical_path` 如何表达。
4. 这类 item 的 `format` 是数据库引擎类型、逻辑格式，还是空。
5. `data_family` 对文档集合、图节点/边是否需要扩充。

### 倾向

不要把数据库表伪装成文件组合。可以考虑 `engine_native`，但必须明确它不是文件格式，也不要求 `component_files`。

## 八、TableInfo / ObjectInfo / Scanner* 模型收口

### 背景

代码中仍存在多套平行模型：

- `common/engine/plugin.TableInfo`
- `common/engine/plugin.ObjectInfo`
- `common/format.TableInfo`
- `common/format.ObjectInfo`
- `common/format.ScannerTableInfo`
- `common/format.ScannerFieldInfo`

### 待确认

1. 哪个模型作为平台内部 canonical model。
2. `plugin.TableInfo` 是否只作为引擎 catalog DTO。
3. `ScannerTableInfo` / `ScannerFieldInfo` 是否删除，还是保留为迁移适配层。
4. 文档集合采样 schema 是否统一进入 `common/format.TableInfo`。
5. 图 label / relationship 是否也用 `TableInfo` 表达结构属性。

### 倾向

以 `common/format.TableInfo`、`common/format.FieldInfo`、`common/format.ObjectInfo` 作为解析结果 canonical model。引擎插件层模型保留 catalog 基础信息，`Scanner*` 逐步删除。

## 九、Registry 与能力发现层收口

### 背景

当前存在多个 registry：engine、dataitem、format、preview、object content。它们职责不同，不能简单合并成一个大注册中心。

### 待确认

1. 是否需要统一能力发现 API，而不是统一 registry。
2. 各 registry 的职责边界是否保持：
   - engine registry：连接和 catalog 能力。
   - dataitem registry：组合形态 detector。
   - format registry：parser / extractor。
   - preview registry：已识别 item 的展示能力。
3. 能力发现结果是否由 meta 落库。
4. 插件加载顺序和冲突处理如何记录。
5. 第三方插件是否可以同时注册 detector、parser、extractor、preview handler。

### 倾向

保留多个职责清晰的 registry，新增统一“能力声明 / 发现视图”。

## 十、空间扩展标准口径

### 背景

空间应统一作为 `attributes.extensions.spatial`。当前最小字段集和跨格式映射仍需正式确认。

### 待确认

1. 标准字段是否固定为 `geometry_column`、`geometry_type`、`geometry_types`、`srid`、`extent`、`crs`、`feature_count`。
2. 多几何字段如何表达。
3. 栅格影像空间元数据是否仍使用 `spatial`，还是拆出 `raster`。
4. PostGIS、Shapefile、GeoJSON、GeoTIFF 的空间字段映射是否统一。
5. Manager 空间预览依赖哪些字段，缺失时如何降级。

### 倾向

先定义最小稳定字段集。格式私有空间细节进入格式或插件私有命名空间，只有跨格式稳定消费的字段才晋升到 `spatial` 标准字段。

## 十一、建议讨论顺序

1. 容器文件内部对象是否展开为子 item。
2. `directory_tree` 的 `explain/confidence`。
3. 对象存储跨层组件认领。
4. 第三方插件扩展声明机制。
5. Manager 内容预览插件能力描述。
6. 是否引入 `engine_native`。
7. 模型收口与能力发现层。
8. 空间扩展标准口径。

## 十二、当前可继续开展但不阻塞规范的事项

在上述规范确认前，仍可继续：

- 做已支持格式的真实引擎端到端验证。
- 清理旧平铺读取，改为读取标准 attributes 分区。
- 补齐已有标准扩展的明确字段映射。
- 为已有清晰规则的格式补 detector 和 parser 回归测试。
- 增加 normalizer 回归测试。
- 清理 Manager 中不影响插件声明机制的历史兜底。
