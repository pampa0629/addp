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

### 1. 识别和能力混在一起

现在既有：

- `DetectFormat`
- `FormatType`
- `common/format/capability`
- `common/format/interface.go` 里的旧 parser 接口

这会导致“这个格式长什么样”和“这个格式能提供什么能力”混在一起。

### 2. parser 接口还在直接绑定 engine

例如：

- `DBTableParser` 直接收 `*gorm.DB` 和 `enginePlugin`
- `DocCollectionParser` 直接收 `client interface{}`
- `FileTableParser` 直接吃 `io.Reader`

这些接口更像历史过渡产物，不像稳定的格式能力边界。

### 3. `geojson` 仍被当成顶层格式

从上层模型看，`geo` 更适合落成 `json + spatial`，而不是单独再维持一套独立顶层格式口径。

### 4. 旧 registry 是 parser registry，不是 capability registry

当前 `common/format/registry.go` 还是按 parser 类型注册：

- file table parser
- db table parser
- doc collection parser

这和后续的 `FormatCapability` / `FormatProvider` 方向不一致。

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
- `common/format/capability` 中只保留 `json` 格式，并通过 `ProviderSpatial` 表达 JSON 的空间扩展可能性。
- Meta 对 `.geojson` 资源的落库语义调整为 `item.format=json`、`item.data_type=table`、`capabilities.spatial`。
- 普通 `.json` 仍默认为 `item.data_type=document`。

这一步只删除旧格式枚举，不否认 GeoJSON 作为一种 JSON 空间编码结构存在。后续 Manager 或 Transfer 如果需要表达“内容以 GeoJSON 编码输出”，应使用内容类型、空间编码或 preview kind 表达，而不是重新引入顶层 `format=geojson`。

同时已经补上 `common/format` 的第一版 provider 基础入口：

- 新增 `Provider` / `TableProvider` / `ProviderRegistry`。
- 内置 CSV、Excel、JSON 空间扩展、Shapefile、Parquet 直接注册为 `TableProvider`。
- Manager 文件表预览已从直接调用 `GetFileTableParser` 调整为调用 `GetTableProvider`。
- Manager 启动显式导入 `common/format/builtin`，确保内置格式 provider 注册稳定。
- 新增 `builtin:file-table` 预览插件声明，避免文件表 provider 只存在于代码工厂而没有插件声明。
- `FileTableParser` 接口、`RegisterFileTableParser`、`GetFileTableParser` 等旧文件表注册 API 已删除。

这不是最终接口，只是先把文件表主路径切到 provider。下一步应继续把 Shapefile 多组件读取、Parquet scope 读取和 Transfer batch 读写迁移到同一套 provider 语义。

### 第一阶段：能力声明收口

先把 `common/format/capability` 作为事实源收稳：

- 保留 `FormatCapability`
- 明确 `Format`、`DataType`、`EngineFamily`
- 让它成为上层消费的统一格式能力视图

这一层先不要再长出新的 parser 依赖。

### 第二阶段：旧 parser 接口退出主路径

把 `common/format/interface.go` 里的接口逐步降级为适配器语义：

- `DBTableParser`
- `FileTableParser`
- `DocCollectionParser`

它们不能继续作为平台主抽象。  
最终应被新的 provider 接口替代，并删除旧接口、旧注册入口和旧调用路径。

当前代码已经完成第一处主路径替换：

```text
Manager FileTablePreviewProvider
  旧：format.GetFileTableParser(format)
  新：format.GetTableProvider(format)
```

旧文件表注册入口已经删除。各格式包内部仍保留 `ParseTableInfo` / `ReadPreview` 这类实现方法，但它们不再通过旧 registry 暴露给上层，而是由 `TableProvider` 统一注册和消费。

### 第三阶段：检测逻辑收敛

`DetectFormat` 保留为辅助工具，但不再承担组织方式判断。

它应该只回答：

- 这更像什么格式
- 是哪类扩展名 / MIME / magic bytes
- 默认落到哪个 `data type`

它不应该再决定：

- item organization
- claims / exclusive
- whole scope / multi component 路由

### 第四阶段：目录结构清理

建议后续逐步把 `common/format` 收成更清楚的几组：

- `capability/`：格式能力声明
- `detect/` 或保留现有 detection 工具
- `schema/`：Schema / Field 标准化
- `mapper/`：类型映射
- `provider/`：格式解析 / 写出适配
- `builtin/`：内置能力注册

## 关键改造点

### 1. `common/format/registry.go`

当前 registry 还是 parser registry。  
后续应向 capability registry + provider registry 过渡。

### 2. `common/format/interface.go`

这些接口不应继续成为平台主接口。

它们要么迁入 provider 层作为内部实现，要么直接删除。

### 3. `common/format/detection.go`

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

### 4. `common/format/parquet/lake_table.go`

这一类代码说明 format 层已经开始背负组织方式和读取策略。

后续应把它从“隐式业务逻辑”收回到明确 provider 或 adapter。

### 5. `common/format/shapefile/*`

Shapefile 是典型的多组件格式，适合验证：

- component 读取
- component 写出
- manifest / 主资源 / 组件角色

它适合作为后续 provider 化的样板。

## 与上层的关系

- `common/resource` 负责读取抽象
- `common/format` 负责格式能力和格式编解码
- `meta` 负责 item 归并
- `manager` 负责预览组装
- `transfer` 负责编排批量读写

顺序上应是：

1. 先收 `common/format`
2. 再接 `common/resource`
3. 再收 `manager`
4. 再收 `transfer`

## 暂不做

- 暂不在本轮一次性删除所有旧 parser，但最终必须删除旧 parser 主接口。
- 暂不在本轮重命名全部 format 包，但最终需要删除不符合新模型的旧目录和旧 API。
- 暂不把 Transfer 中的空间编码枚举 `geojson` 误删；那里表达的是几何编码格式，不是 ADDP 顶层 `FormatType`。
- 暂不把 Manager 当前前端 preview kind 的 `geojson` 误删；它表达的是展示内容类型，后续要改成由 `data_type + capabilities.spatial` 组装。
- 暂不在这里定义最终 Go 接口
- 暂不直接改上层 Manager / Transfer 调用

## 结论

`common/format` 是这一轮改造的底座。  
底座不收，上层很难真正收口。

最终目标不是兼容旧实现，而是删除旧代码、旧逻辑、旧 API 和旧数据。
