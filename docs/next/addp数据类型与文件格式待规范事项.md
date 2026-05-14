# ADDP 数据类型与文件格式待规范事项

更新时间：2026-05-12

本文只记录数据项、数据类型、文件格式、FormatPlugin、attributes 和 Manager 内容读取中尚未进入正式规范的事项。已经定稿的规则不在本文重复，统一查看：

- [ADDP 数据项体系图](../concepts/addp数据项体系图.md)
- [ADDP 数据类型和格式体系图](../concepts/addp数据类型和格式体系图.md)
- [ADDP 数据类型与格式能力规范](../spec/addp数据类型与格式能力规范.md)
- [ADDP 数据类型与文件格式扩展指南](../spec/addp数据类型与文件格式扩展指南.md)
- [ADDP 数据项探测器规范](../spec/addp数据项探测器规范.md)
- [ADDP 元数据 attributes 规范](../spec/addp元数据attributes规范.md)
- [ADDP 内置数据类型与文件格式规范](../spec/addp内置数据类型与文件格式规范.md)

当前已确认：新增普通文件格式优先只改 `common/format`。只有突破现有 data item 识别或 attributes 映射能力时，才补 Meta detector / normalizer。

## 当前实现状态概览

1. `common/format` 已有 FormatPlugin、FormatDescriptor、FormatCapability、info provider、content reader、manifest 和能力发现视图等基础能力。
2. Meta 已有 `single`、`multi`、`whole` 三类组织方式和 `FormatRule` 校验；普通 `single` 格式可以从 `common/format` capability 派生识别规则。
3. Excel、SQLite、GeoPackage、ZIP 容器内部对象已写入 `type_info.container.children`，暂未升格为独立 data item。
4. Manager 内容读取已有自己的 content handler / preview DTO / 前端渲染体系，并已部分使用 format descriptor 生成匹配默认值，但还没有完全改为基于能力发现视图派生。
5. `content_index.table` 的稀疏行索引已用于 CSV 等表格文件扫描和 Manager range preview；其他索引类型和失效规则仍未扩展。

## 一、容器内部对象升格条件

### 已确认

SQLite、GeoPackage、Excel、ZIP 等容器类 data item 暂不自动展开内部子 item。外层容器文件本身生成一条 data item：

- `organization=single`
- `data_type=container`
- `format=sqlite|geopackage|excel|zip|...`

内部 table、view、layer、sheet、entry 等先写入 `type_info.container.children` 及对应 `format_info.<format>`。

### 当前实现状态

Excel、SQLite、GeoPackage、ZIP 已有容器 children 提取实现。容器父级只写入 sheet / table / view / layer / entry 的轻量 children 索引、默认入口和容器统计；字段、样本行、正文、媒体内容和 GeoPackage 单个 layer 的空间字段、SRID、extent、空间索引等 child 内容不得写入父容器。Excel / SQLite / GeoPackage 的表格 child 在选中后由 native child resolver 复用父资源和 child options 读取；ZIP 普通文件 entry 由 stream child resolver 打开后，再按 entry 自身的 `data_type` / `format` 交给对应 reader。

Meta 默认只做一层容器 children 索引，不递归扫描嵌套容器。ZIP 中还有 ZIP 等嵌套关系由消费方在选中 child 后按需继续走同一套 `ContainerInfoProvider` / `ContainerChildResolver` 链路，并受最大深度、最大 children 数和解压大小等策略约束。

### 待确认

只有明确需要内部对象独立授权、检索、血缘、传输或生命周期管理时，才讨论子 item 升格。届时需要补：

1. 内部子 item 的 `name/full_name/node_id/fingerprint` 规则。
2. 容器 item 与子 item 的关系模型。
3. Manager 面向内部子 item 的路由方式。
4. 子 item 的权限、搜索索引和生命周期语义。

## 二、第三方格式声明机制

FormatDescriptor / FormatPlugin 已经能表达内置格式身份和能力，但第三方扩展还需要更严格的 manifest 规则。

### 当前实现状态

`common/format` 已有最小 manifest 加载能力，manifest 当前只包装一个 `descriptor`，注册后等同于注册 FormatDescriptor。descriptor 本身已经包含 format identity、identification、providers、content readers、version、priority、engine families 和 transfer 能力等字段。

当前 manifest 还不能声明 Go 实现加载方式、命令型 reader、私有字段 schema、禁用状态或审计信息。descriptor 冲突已有运行时诊断结构，但尚未明确是否落库、是否对 Manager / Meta 暴露，以及第三方加载顺序如何审计。

### 待确认

1. manifest 是否允许同时声明 format identity、identification、providers 和 content readers。
2. 私有 `format_info` 和 `capabilities` 命名空间命名规则，是反向域名还是 plugin ID。
3. 私有字段是否需要声明类型、来源、是否可展示、是否可索引、是否可诊断。
4. 私有字段被平台稳定消费时，如何晋升为标准字段。
5. descriptor 冲突时，优先级、覆盖、诊断记录如何落库或暴露。

## 三、能力发现视图

### 已确认

能力发现需要区分两类事实：

- descriptor / capability 声明了什么能力。
- 当前进程实际注册了哪些 Go 实现。

### 当前实现状态

`common/format` 已有 `ListFormatCapabilityViews()` / `GetFormatCapabilityView()`。视图可以返回 descriptor 声明的 data type、layouts、identification、providers、content readers，也能标识当前进程是否注册了 FormatPlugin、TableInfoProvider、TableSampleReader、DocumentInfoProvider、DocumentTextReader、MediaInfoProvider 和 legacy MetadataExtractor。

当前能力发现仍是运行时内存查询，没有 Meta 落库模型，也没有统一 API 给 Manager 直接消费。Manager 现阶段仍主要靠自己的 content handler 配置和内置 handler 选择逻辑工作。

### 待确认

1. 能力发现结果是否需要由 Meta 落库，还是仅运行时查询。
2. Manager 是否应从能力发现视图派生内容 handler，而不是维护独立扩展名清单。
3. 能力发现结果是否需要包含版本、来源、冲突诊断和禁用状态。
4. 第三方插件加载顺序和冲突处理如何审计。

## 四、Manager 内容读取插件边界

### 已确认

`common/format` 不定义 preview 概念，不返回 Manager 面向前端的 DTO，不推荐前端渲染器。Manager 可以有自己的内容 DTO 和前端插件体系，但不能反向约束 common。

### 当前实现状态

Manager 后端已有 ObjectContentHandler / PreviewProvider 体系和独立的 ObjectPreview DTO。内置 handler 覆盖 PDF、DOCX、WPS、PPTX、image、JSON、GeoJSON、Excel、Shapefile、SQLite、text、markdown 等内容类型，其中一部分 handler 会使用 format descriptor 中的扩展名和 MIME 作为 matcher 默认值。

文件表格预览已经能通过 `format.TableProvider` / `TableSampleReader` 读取 CSV、JSON、Parquet、Shapefile 等格式，并可消费 `type_info.table` 和 `content_index.table`。但 Manager 仍维护 preview kind、handler 类型、前端 renderer 和部分格式别名，不是完全由 `data_type`、`format`、`organization`、`capabilities` 或能力发现视图派生。

### 待确认

1. Manager 内容插件是否必须基于 `data_type`、`format`、`organization`、`capabilities` 匹配。
2. `priority` 是否仅允许在同一标准匹配结果内解决冲突。
3. 内容插件是否允许读取 `format_info` 私有字段用于展示。
4. 命令型内容插件是否需要声明输入 payload schema 和输出 content schema。
5. multi / whole 内容插件是否统一使用 `meta_item.full_name`、`component_files` 和 whole scope manifest，不允许自行枚举 sibling。

## 五、content_index 扩展

CSV 的表格稀疏行索引已经确认为 `content_index.table` 的一个标准结构。

### 当前实现状态

`common/format` 已定义 `ContentIndex`、`ContentIndexInfo`、`sparse_row_index`、逻辑单位 `row` 和物理偏移单位 `byte`。TableInfo 可以夹带 content index，Meta 会把它写入 `attributes.content_index.table`。

Manager 文件表预览会读取 `content_index.table`，在 engine 支持 range read 时按 anchor 打开局部流，并通过 `TableSampleOptions.InputIsPositioned` 告诉 TableSampleReader 当前输入已经从某个行边界开始。当前主要覆盖 CSV 类稀疏行索引；Parquet row group、JSON Lines、文档页码、媒体关键帧等还未统一。

### 待确认

1. JSON Lines、Parquet row group、文档页码、媒体关键帧是否进入 `content_index`。
2. 每类索引的逻辑单位和物理偏移单位如何声明。
3. 索引失效规则是否统一使用 size、etag、last_modified_at、fingerprint。
4. content reader 如何声明自己能消费哪类 `content_index`。

## 六、建议讨论顺序

1. 容器内部对象升格条件。
2. 第三方格式 manifest。
3. 能力发现视图是否落库。
4. Manager 内容插件边界。
5. content_index 扩展规则。
