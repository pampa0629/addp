# common/format 收口与 Provider 化改造方案

更新时间：2026-05-09

本文聚焦 `common/format` 的底层收口顺序，不讨论 Manager / Transfer 的最终消费实现。

## 为什么先动这里

`common/format` 现在仍混着几类职责：

- 格式识别
- 格式能力声明
- 旧式 parser 注册
- Schema / Field 标准化
- 部分 engine 相关输入

如果底层不先收，上层很容易继续把旧概念带上去，最后只是换了名字，没有换边界。

## 当前问题

### 1. 识别和能力曾经混在一起

改造前同时存在：

- `DetectFormat`
- `FormatType`
- 独立 capability 子包
- 旧 parser 接口

这会导致“这个格式长什么样”和“这个格式能提供什么能力”混在一起。当前旧 parser 接口和独立 capability 子包已经删除，后续继续以 `common/format` 根包 capability registry 和 provider registry 为事实源。

### 2. parser 接口曾经直接绑定 engine

已删除的历史接口包括：

- 数据库表 parser：直接收 `*gorm.DB` 和 `enginePlugin`。
- 文档集合 parser：直接收 `client interface{}`。
- 文件表 parser：直接吃 `io.Reader` 并通过旧 registry 暴露。

这些接口更像历史过渡产物，不像稳定的格式能力边界。

### 3. `geojson` 仍被当成顶层格式

从上层模型看，`geo` 更适合落成 `json + spatial`，而不是单独再维持一套独立顶层格式口径。

### 4. 旧 registry 曾经是 parser registry，不是 capability registry

已删除的旧 registry 曾经按 parser 类型注册：

- file table parser
- db table parser
- doc collection parser

这和 `FormatCapability` / `FormatProvider` 方向不一致，因此已经删除。

## 目标形态

`common/format` 后续应分成三层：

1. **FormatCapability**
   - 说明格式能提供什么
   - 例如识别、布局、解析、写出、批量读写、空间能力

2. **Format Provider / Adapter**
   - 说明具体格式如何完成解析或写出
   - 尽量不再直接感知上层业务模型

3. **轻量工具层**
   - DetectFormat
   - MIME / extension 映射
   - Schema / Field 标准化
   - 类型映射

## 现有代码的收口顺序

### 已落地的第一步

本轮先删除 `geojson` 作为 `common/format` 顶层格式的事实口径：

- `common/format.FormatGeoJSON` 不再存在。
- `.geojson` 扩展名和 `application/geo+json` MIME 统一识别为 `FormatJSON`。
- 旧 `common/format/geojson` 包已删除，JSON 表格 provider 统一放到 `common/format/plugins/json`。
- `common/format` 根包 capability registry 中只保留 `json` 格式能力，并通过 `ProviderSpatial` 表达 JSON 的空间扩展可能性。
- Meta 对 `.geojson` 资源的落库语义调整为 `item.format=json`、`item.data_type=table`、`capabilities.spatial`。
- 普通 `.json` 仍默认为 `item.data_type=document`。

这一步只删除旧格式枚举，不否认 GeoJSON 作为一种 JSON 空间编码结构存在。后续 Manager 或 Transfer 如果需要表达“内容以 GeoJSON 编码输出”，应使用内容类型、空间编码或 preview kind 表达，而不是重新引入顶层 `format=geojson`。

同时已经补上 `common/format` 的第一版 provider 基础入口：

- 新增 `Provider` / `TableProvider` / `MultiTableProvider` / `ScopeTableProvider` / `ProviderRegistry`。
- 内置 CSV、Excel、JSON 空间扩展、Shapefile、Parquet 直接注册为 `TableProvider`。
- Manager 文件表预览已调整为调用 `GetTableProvider`。
- Manager 启动显式导入 `common/format/builtin`，确保内置格式 provider 注册稳定。
- 新增 `builtin:file-table` 预览插件声明，避免文件表 provider 只存在于代码工厂而没有插件声明。
- 旧文件表注册 API 已删除。

这不是最终接口，只是先把表格格式的主路径切到 provider。Shapefile 多 ref 读取和 Parquet scope 读取已经先迁到同一套 provider 语义；Transfer batch 读写后续单独处理。

`common/contentio` 也已经补上最小读取抽象，并用 Manager Shapefile 预览验证了多 ref 链路：

```text
engine ContentReadableProvider
  -> contentio.Reader
  -> []contentio.Ref
  -> format.MultiTableProvider
  -> Manager 面向前端的 DTO
```

Shapefile 的ref 物化已经从 Manager 下沉到 FormatPlugin / table content reader，Manager 只负责把 engine provider 适配为 `contentio.Reader`，并把已确认的 `[]contentio.Ref` 交给 format provider。

Parquet lake table 预览也已经验证 scope 链路：

```text
engine CatalogProvider + ContentReadableProvider
  -> contentio.Reader
  -> format.ScopeTableProvider
  -> Manager 面向前端的 DTO
```

`common/format/plugins/parquet` 不再保留直接依赖 engine plugin 的预览 helper；目录列举和内容打开由上层编排层通过 `contentio.Reader` 提供。

Meta 的 lake table schema 提取也已经改为通过 `format.GetTableProvider(format.FormatParquet)` 调用 Parquet provider，不再直接 new Parquet parser。

Manager object content 中的 GeoJSON / Parquet 内容预览也已经改为通过 `TableProvider` 提取表语义，Manager 只保留 preview content DTO 组装。

Manager lake table 预览已经按 engine 类型选择 `contentio.Reader`：

- 对象存储 lake table 使用 `object storage content reader`。
- 文件系统 lake table 使用 `file system content reader`。

Manager 使用 `builtin:scope-table` 路由目录型表格资源。它是 scope 表预览 provider 名称，不是 item type。

当前 Manager 已先区分单文件 `PhysicalPath` 和目录型 `ScopePath`：

- `item.organization=single` 时，`storage.physical_path` 进入 `PhysicalPath`。
- `item.organization=whole` 时，`storage.physical_path` 进入 `ScopePath`。

Manager provider 选择也已经改为看标准 attributes：

- `item.data_type=table` 且 `item.organization=whole` 时，走 `builtin:scope-table`。
- `item.data_type=table` 且 `item.organization=single` / 文件表格式时，走 `builtin:file-table`。

新扫描结果不再产出 `item_type=lake_table`。Parquet / ORC / Avro 这类表格文件或目录型表格 scope 只是 `item_type=table + item.format=parquet/orc/avro + item.organization=single/whole` 的组合语义。

### 第一阶段：能力声明收口

先把 `common/format` 根包 capability registry 作为事实源收稳：

- 保留 `FormatCapability`
- 明确 `Format`、`DataType`、`EngineFamily`
- 让它成为上层消费的统一格式能力视图

这一层先不要再长出新的 parser 依赖。

### 第二阶段：旧 parser 接口退出主路径

旧 parser 主接口已经删除：

- `DBTableParser`
- `FileTableParser`
- `DocCollectionParser`

它们不再作为平台主抽象存在，也不再提供注册或获取入口。

当前代码已经完成主路径替换：

```text
Manager FileTablePreviewProvider
  -> format.GetTableProvider(format)
```

旧文件表注册入口已经删除。各格式包内部仍保留 `ParseTableInfo` / `ReadPreview` 这类实现方法，但它们不再通过旧 registry 暴露给上层，而是由 `TableProvider` 统一注册和消费。

同时，旧 `TypeMapping` 兼容 facade 和无人调用的 `common/format/db` parser 包也已删除。类型映射统一走 `TypeMapper` 注册表。

### 第三阶段：检测逻辑收敛

`DetectFormat` 保留为辅助工具，但不再承担组织方式判断。

它应该只回答：

- 这更像什么格式
- 是哪类扩展名 / MIME / magic bytes
- 默认落到哪个 `data type`

它不应该再决定：

- item organization
- claims / exclusive
- whole scope / multi ref 路由

### 第四阶段：目录结构清理

当前目录结构已按以下边界收口：

- 根包：稳定 facade、格式识别、capability registry、provider / reader 接口与注册表、通用 info 模型。
- `registry/`：format descriptor 的运行时注册、查询、能力发现和冲突诊断。
- `plugins/`：具体格式实现。descriptor、provider、reader 和测试尽量在格式目录内闭合。
- `mappers/`：数据库或格式原生类型到 ADDP 通用字段类型的映射。
- `builtin/`：内置格式插件和 type mapper 的统一加载入口。

不再恢复独立 capability 子包；格式能力由 descriptor 派生并在根包提供统一消费入口。

## 关键改造点

### 1. provider registry 与 capability registry

parser registry 已删除。当前保留两类事实源：

- capability registry：描述格式事实和能力。
- provider registry：注册格式 provider 实现。

后续改造应继续围绕这两类 registry 进行，不再引入 parser registry。

### 2. `common/format/detection.go`

保留识别工具，但识别结果要尽量轻。

尤其不要把 `geojson` 继续当成独立顶层格式事实。

当前口径：

| 输入 | 输出 |
|---|---|
| `.json` | `FormatJSON` |
| `.geojson` | `FormatJSON` |
| `application/json` | `FormatJSON` |
| `application/geo+json` | `FormatJSON` |

是否为空间数据不由 `FormatType` 判断，而由解析结果、Meta attributes 或 `SpatialProvider` 判断。

### 3. `common/format/plugins/*`

具体内置格式实现统一放入 `common/format/plugins/*`：

- `plugins/csv`
- `plugins/excel`
- `plugins/json`
- `plugins/parquet`
- `plugins/shapefile`
- `plugins/image`
- `plugins/pdf`
- `plugins/sqlite`

`plugins` 表达“格式插件实现”，避免把文件格式与编码方式混淆，也为未来第三方 format plugin 预留清晰边界。

### 4. `common/format/plugins/parquet/table_file.go`

这一类代码只保留 Parquet / ORC / Avro 等表格文件格式的轻量判断。

当前已经先删除 Parquet 直接调用 engine provider 的预览 helper，并新增 `ScopeTableProvider` 处理 scope 表读取。原 `lake_table.go` 已改名为 `table_file.go`，避免在 `common/format` 里继续传播 lake table 概念。这里不能长出 engine、preview 或 item 归并逻辑。

### 5. `common/format/plugins/shapefile/*`

Shapefile 是典型的多refs格式，适合验证：

- multi read
- multi write
- manifest / 主资源 / refs角色

它适合作为后续 provider 化的样板。

当前 Shapefile 已经作为 `MultiTableProvider` 样板落地：ref 集合由上层根据 layout 或 Meta attributes 提供，ref 物化与解析由 FormatPlugin / table content reader 内部完成。

## 与上层的关系

- `common/contentio` 负责读取抽象
- `common/format` 负责格式能力和格式编解码
- `meta` 负责 item 归并
- `manager` 负责预览组装
- `transfer` 负责编排批量读写

顺序上应是：

1. 先收 `common/format`
2. 再接 `common/contentio`
3. 再收 `manager`
4. 再收 `transfer`

## 暂不做

- 暂不把 Transfer 中的空间编码枚举 `geojson` 误删；那里表达的是几何编码格式，不是 ADDP 顶层 `FormatType`。
- 暂不把 Manager 当前前端 preview kind 的 `geojson` 误删；它表达的是展示内容类型，后续要改成由 `data_type + capabilities.spatial` 组装。
- 暂不处理 Transfer 读写链路；Transfer 单独开一轮。
- `builtin:scope-table` 是 Manager 内部 provider 名称，保留它不代表保留 `lake_table` item type。

## 结论

`common/format` 是这一轮改造的底座。  
底座不收，上层很难真正收口。

最终目标不是兼容旧实现，而是删除旧代码、旧逻辑、旧 API 和旧数据。
