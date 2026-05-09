# ADDP 文件格式能力接口规范

本文定义 `common/format` 中的格式实现应提供哪些能力，以及这些能力分别解决什么问题。

本文只描述格式本身，不描述 Meta 的 item 归并逻辑。

## 核心原则

`common/format` 只回答格式自身的问题：

- 这个资源像什么格式。
- 这个格式能提取什么结构事实。
- 这个格式能否提供样本、提取结果、写出、转换、批量读写。

`common/format` 不回答 item 归并问题：

- 不决定 `single`、`multi`、`whole`。
- 不决定 claims、exclusive、component_files。
- 不决定 `meta_item.full_name`。
- 不直接写最终 `meta_item.attributes`。

Meta 负责把这些事实编排成最终 item。

## 能力分区

### 1. Identification

格式识别能力只做候选判断。

它回答：

- 扩展名像不像。
- MIME 像不像。
- magic bytes 像不像。
- 内容结构像不像。

它不直接生成 item。

### 2. Layout

格式布局能力描述格式自身如何组织资源。

它回答：

- 这是单文件、组件文件，还是 whole scope。
- 哪个资源是主资源。
- 哪些资源是必需组件。
- 哪些资源是可选组件。
- 是否允许跨目录匹配。

这一组能力的价值，是让 Meta 能统一编排规则，而不是让每个格式在 Meta 里各写一套分支。

### 3. Parse / Extract

格式解析和提取能力负责把原始资源转成平台能理解的结构事实。

常见产出：

- `type_info.table`
- `type_info.document`
- `type_info.media`
- `type_info.container`
- `format_info.<format>`
- `capabilities.spatial`
- `capabilities.extraction`
- `capabilities.statistics`

### 4. Sample / Extract

样本 / 提取能力负责提供 Manager、Meta、Transfer 可继续组装的数据。

它不直接负责 Manager preview，也不等于最终 attributes。

### 5. Transfer

Transfer 能力负责批量读写、组件读写和提交边界。

这部分要回答：

- 能不能 batch read。
- 能不能 batch write。
- 能不能 component read / write。
- 写出时怎样提交。

## FormatIdentificationCapability

识别能力建议至少说明以下内容：

- `Extensions`：支持的后缀。
- `MIMETypes`：支持的 MIME。
- `MagicBytes`：支持的文件头签名。
- `ContentSignatures`：支持的内容结构特征。
- `DefaultDataType`：默认落点数据类型。
- `SupportedLayouts`：支持的布局类型。
- `Priority`：识别冲突时的排序权重。
- `SupportsFallback`：是否能作为兜底格式处理。

### 说明

- `Priority` 只用于识别排序，不表示 item 优先级。
- `SupportedLayouts` 只表示格式自身支持哪些组织方式，不表示 Meta 一定按此认领。
- `.json` 不能直接等同于空间格式。

## FormatLayoutCapability

布局能力建议描述格式的资源组织规则：

- `ScopeKind`
- `PrimaryResourceRole`
- `RequiredComponents`
- `OptionalComponents`
- `SameNameRule`
- `CrossDirectoryAllowed`
- `WholeScopeExclusive`

### 示例

- CSV：`single`，一个资源即可。
- Shapefile：`multi`，主文件加同 basename 组件。
- Iceberg：`whole`，整体范围认领。

### 说明

这一组能力只服务 Meta 的调度，不直接等于 item 结果。

## Parse / Extract 能力说明

### Table 相关

表格解析能力应说明：

- 字段名如何获得。
- 行数如何获得。
- 原始字段类型如何保留。
- 主键、索引、采样是否可得。

### Document 相关

文档提取能力应说明：

- 标题、作者、页数、语言等是否可得。
- 是否可提取文本。
- 是否支持摘要或片段。

### Media 相关

媒体提取能力应说明：

- 宽高、时长、编码、颜色模式等是否可得。
- 是否支持缩略图。
- 是否支持 EXIF 或地理扩展信息。

### Container 相关

容器能力应说明：

- 是否能枚举内部对象。
- 是否能识别默认入口。
- 是否能返回内部对象摘要。

### Spatial 相关

空间能力应说明：

- geometry columns。
- primary geometry column。
- SRID / CRS。
- extent。
- spatial index。

## Sample / Extract 能力说明

样本 / 提取能力不需要和最终扫描结果一一对应，但要能给上层提供稳定的数据提取入口。

常见结果包括：

- table rows sample
- document text fragment
- media thumbnail material
- container children sample
- graph sample

Manager preview DTO 由 Manager 组装，不应进入 `common/format`。

## Transfer 能力说明

Transfer 能力要明确格式在批量读写中的角色。

建议说明：

- `BatchRead`
- `BatchWrite`
- `ComponentRead`
- `ComponentWrite`
- `CommitPolicy`

### 重要边界

- Format writer 负责编码格式。
- Engine writer 负责提交到目标存储。
- 多文件格式必须明确提交边界。

## 读取入口说明

format provider 不应该通过 `engine id` 自己构造读取器。

推荐方式是：

1. Meta、Manager 或 Transfer 根据 engine capability 构造读取抽象。
2. format provider 接收读取抽象和格式参数。
3. format provider 输出结构事实、样本或 DataBatch。

这样可以把连接、凭据、权限、重试、审计和对象枚举留在 engine 层，把编码 / 解码留在 format 层。

当前 engine 层已有 `ContentReadableProvider`、`RangeReadableProvider`、`CatalogProvider`、`BatchReadableProvider` 等基础能力，能够作为读取抽象的来源。但 format 层不应直接依赖这些具体 engine provider 接口；中间应有编排层适配出的 `ResourceReader` / `ComponentReader`。

这部分先按规范约束，不急于新增 engine 基础接口。等 Manager、Meta、Transfer 的调用路径稳定后，再判断是否需要把读取抽象正式放入 `common/engine/plugin`。

## 不建议保留的设计

- 不要在 `common/format` 放 item resolver。
- 不要让 format 自己决定 `organization`。
- 不要把 provider 输入做成过重 locator。
- 不要让 format provider 按 `engine id` 反向构造 engine 读取器。
- 不要把 Manager preview DTO 放进 format 层。
- 不要把 `geojson` 作为独立顶层格式。
