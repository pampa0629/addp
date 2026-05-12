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

## 一、whole scope 的诊断信息

### 背景

`organization=whole` 会把整个目录、prefix、schema 或扫描范围归并为一个 data item，并可能阻止范围内其他资源继续落 item。这个能力风险较高，需要可审计诊断。

### 待确认

1. `explain` 是只进入扫描日志，还是也写入 `format_info.<format>`。
2. 是否需要正式 `confidence` 字段；如果需要，枚举是否为 `exact`、`strong`、`weak`。
3. 弱匹配是否只能记录诊断，不允许生成 whole item。
4. 范围下存在未认领异类资源时，是否一律拒绝 `exclusive=true`。
5. whole scope 的 claims 是否必须列出全部关键资源，还是允许只列 manifest / 根范围。

### 当前倾向

规则判断以确定性结构为准。`explain` 优先作为诊断和审计信息；`confidence` 只用于冲突处理和防止弱匹配独占扫描范围，不替代格式规则。

## 二、对象存储跨层组件归并

### 已确认

默认 sibling `multi` 只认领同 prefix 的直接兄弟对象。跨层级分布的 manifest、数据文件、索引文件如果构成一个整体数据集，应按 `organization=whole` 处理，不扩展出新的 `mixed_collection` 组织方式。

### 待确认

1. manifest 引用资源如何表达 claimed resources。
2. claimed resources 使用完整 object key、catalog path，还是统一资源标识。
3. 是否允许跨 bucket 引用；默认倾向不允许。
4. 是否允许跨 sibling prefix 引用；默认必须由具体格式规范显式声明。
5. 被 whole scope 认领的 object 是否禁止作为普通 object 落库。

## 三、容器内部对象升格条件

### 已确认

SQLite、GeoPackage、Excel、ZIP 等容器类 data item 暂不自动展开内部子 item。外层容器文件本身生成一条 data item：

- `organization=single`
- `data_type=container`
- `format=sqlite|geopackage|excel|zip|...`

内部 table、view、layer、sheet、entry 等先写入 `type_info.container.children` 及对应 `format_info.<format>`。

### 待确认

只有明确需要内部对象独立授权、检索、血缘、传输或生命周期管理时，才讨论子 item 升格。届时需要补：

1. 内部子 item 的 `name/full_name/node_id/fingerprint` 规则。
2. 容器 item 与子 item 的关系模型。
3. Manager 面向内部子 item 的路由方式。
4. 子 item 的权限、搜索索引和生命周期语义。

## 四、第三方格式声明机制

FormatDescriptor / FormatPlugin 已经能表达内置格式身份和能力，但第三方扩展还需要更严格的 manifest 规则。

### 待确认

1. manifest 是否允许同时声明 format identity、identification、providers 和 content readers。
2. 私有 `format_info` 和 `capabilities` 命名空间命名规则，是反向域名还是 plugin ID。
3. 私有字段是否需要声明类型、来源、是否可展示、是否可索引、是否可诊断。
4. 私有字段被平台稳定消费时，如何晋升为标准字段。
5. descriptor 冲突时，优先级、覆盖、诊断记录如何落库或暴露。

## 五、能力发现视图

### 已确认

能力发现需要区分两类事实：

- descriptor / capability 声明了什么能力。
- 当前进程实际注册了哪些 Go 实现。

### 待确认

1. 能力发现结果是否需要由 Meta 落库，还是仅运行时查询。
2. Manager 是否应从能力发现视图派生内容 handler，而不是维护独立扩展名清单。
3. 能力发现结果是否需要包含版本、来源、冲突诊断和禁用状态。
4. 第三方插件加载顺序和冲突处理如何审计。

## 六、Manager 内容读取插件边界

### 已确认

`common/format` 不定义 preview 概念，不返回 Manager 面向前端的 DTO，不推荐前端渲染器。Manager 可以有自己的内容 DTO 和前端插件体系，但不能反向约束 common。

### 待确认

1. Manager 内容插件是否必须基于 `data_type`、`format`、`organization`、`capabilities` 匹配。
2. `priority` 是否仅允许在同一标准匹配结果内解决冲突。
3. 内容插件是否允许读取 `format_info` 私有字段用于展示。
4. 命令型内容插件是否需要声明输入 payload schema 和输出 content schema。
5. multi / whole 内容插件是否统一使用 `meta_item.full_name`、`component_files` 和 whole scope manifest，不允许自行枚举 sibling。

## 七、content_index 扩展

CSV 的表格稀疏行索引已经确认为 `content_index.table` 的一个标准结构。后续还需要确认：

1. JSON Lines、Parquet row group、文档页码、媒体关键帧是否进入 `content_index`。
2. 每类索引的逻辑单位和物理偏移单位如何声明。
3. 索引失效规则是否统一使用 size、etag、last_modified_at、fingerprint。
4. content reader 如何声明自己能消费哪类 `content_index`。

## 八、建议讨论顺序

1. whole scope 的 `explain/confidence`。
2. 对象存储跨层组件 whole scope 认领规则。
3. 第三方格式 manifest。
4. 能力发现视图是否落库。
5. Manager 内容插件边界。
6. content_index 扩展规则。
