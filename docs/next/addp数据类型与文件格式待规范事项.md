# ADDP 数据类型与文件格式待规范事项

更新时间：2026-05-06

本文只记录“数据类型、文件格式、组织方式、横切能力、attributes 治理”中尚未形成正式规范、需要先讨论确认的事项。已确认或正在整理的 next 阶段文档包括：

- [ADDP 数据类型与格式体系图](addp数据类型与格式体系图.md)
- [ADDP 数据格式扩展指南](addp数据格式扩展指南.md)
- [ADDP 数据项 detector 规范](addp数据项detector规范.md)
- [ADDP 元数据 attributes 规范](addp元数据attributes规范.md)
- [ADDP 内置数据格式规范](addp内置数据格式规范.md)
- [ADDP 数据类型与文件格式跟进清单](addp数据类型与文件格式跟进清单.md)

当前阶段不保留旧 attributes、旧枚举和旧平铺字段兼容。旧数据可删除后重新 meta 扫描；与新规范矛盾的数据和代码应尽早暴露并修正。

## 一、容器内部对象暂不展开为子 item

### 已确认

SQLite、GeoPackage、Excel、ZIP 等容器类 data item 暂不展开内部子 item。外层容器文件本身生成一条 meta item：

- `organization=single`
- `data_type=container`
- `format=sqlite|geopackage|excel|zip|...`

内部 table、view、layer、sheet、文件等只写入 `type_info.container.children` 及对应 `format_info.<format>`，不生成独立 `meta_item`。

### 暂不进入开发的事项

以下事项只有在后续明确需要“内部对象独立授权、检索、血缘、传输或生命周期管理”时再讨论：

- 内部子 item 的 `name/full_name/node_id/fingerprint`。
- 容器 item 与子 item 的关系模型。
- Manager / Transfer 面向内部子 item 的独立路由。
- 子 item 的权限、搜索索引和生命周期语义。

## 二、whole scope 的 explain / confidence

### 背景

原 `directory_tree` 统一改为 `organization=whole` 的 whole scope detector。whole scope detector 可以声明 `Exclusive=true`，这会停止继续处理扫描范围下的剩余资源，风险较高，必须具备可审计依据。

### 待确认

1. `explain` 是否作为 detector 诊断信息写入扫描日志，还是进入 `format_info.<format>`。
2. `confidence` 是否需要正式字段；如果需要，枚举是否使用 `exact`、`strong`、`weak`。
3. 弱匹配是否只能记录诊断，不允许生成 whole item。
4. 范围下存在未认领异类资源时，是否一律拒绝独占。
5. whole scope 的 `Claims` 是否必须列出全部关键资源，还是允许仅列 manifest / 根范围。

### 倾向

规则判断仍以确定性结构为准。`explain` 优先作为诊断和审计信息；`confidence` 只用于冲突处理和防止弱匹配独占扫描范围，不替代格式规则。

## 三、对象存储跨层组件按 whole scope 处理

### 已确认

对象存储跨层组件认领不再按 sibling `multi` 自行扩展，也不引入独立 `mixed_collection`。当 manifest、数据文件、索引文件跨层级分布并构成一个整体数据集时，统一按 `organization=whole` 的 whole scope 处理。

### 待确认

1. manifest 引用的资源如何表达 claimed resources。
2. claimed resources 使用完整 object key、catalog path，还是统一资源标识。
3. 是否允许跨 bucket 引用；默认倾向不允许。
4. 是否允许跨 sibling prefix 引用；默认必须由格式规范显式声明。
5. 被 whole scope 认领的 object 是否禁止作为普通 object 落库。

### 倾向

默认 sibling `multi` 只认领同 prefix 的直接兄弟对象。跨层认领必须由具体格式 detector 显式声明为 whole scope，统一 detector 框架不能自行猜测。

## 四、mixed_collection 暂不保留

`mixed_collection` 暂不作为待规范事项保留。当前规则足够表达：

- 只认领部分资源：使用 `organization=multi`。
- 整个范围归并为一个 item：使用 `organization=whole`。

未来遇到 `multi` 和 `whole` 都无法表达的真实格式，再重新提出具体问题讨论。

## 五、第三方插件扩展声明机制

该事项先单独形成构想文档：[第三方插件扩展声明构想](addp第三方插件扩展声明构想.md)。

待讨论重点：

1. 插件 manifest 是否统一声明 `format_info` 和 `capabilities` 命名空间。
2. 私有命名空间命名规则是否只允许反向域名，还是允许平台插件 ID。
3. 私有字段是否声明类型、来源、是否可展示、是否可索引、是否可诊断。
4. 私有字段被平台稳定消费时，如何晋升为标准字段。
5. 插件输出字段和平台标准字段冲突时，normalizer 如何记录冲突。

## 六、Manager 内容预览插件能力描述

该事项先单独形成构想文档：[Manager 内容预览插件能力构想](addpManager内容预览插件能力构想.md)。

待讨论重点：

1. 内容插件是否必须声明支持的 `data_type`、`format`、`organization`。
2. `priority` 是否仅允许在同一标准匹配结果内解决冲突。
3. 内容插件是否允许读取 `format_info` 私有字段用于展示。
4. 命令型插件是否需要声明输入 payload schema 和输出 content schema。
5. multi / whole 内容插件是否统一使用 `meta_item.full_name`、`component_files` 和 whole scope manifest，不允许自行枚举 sibling。

## 七、引擎原生 item 按 single 处理

### 已确认

不引入 `engine_native` 组织方式。数据库表、文档集合、图 label / relationship 等引擎原生 item 统一按 `organization=single` 表达。

### 整理思路

`single` 的含义是“一个引擎资源对应一个 data item”，不是“单文件”。因此：

- PostgreSQL table：`organization=single`、`data_type=table`；无格式私有信息时不写 `format` 和 `format_info`。
- MongoDB collection：可按平台消费方式识别为 `data_type=table` 或 `document`，`organization=single`。
- Neo4j label / relationship：可按 `data_type=graph` 或后续图规范定义，`organization=single`。

引擎原生 item 不要求 `component_files`。`meta_item.full_name` 已是引擎内唯一逻辑标识和定位事实源，不再定义通用 `entry_path`。

## 八、TableInfo / ObjectInfo / Scanner* 模型收口

该事项移入 [ADDP 数据类型与文件格式跟进清单](addp数据类型与文件格式跟进清单.md)，作为实现阶段的模型收口任务推进。

## 九、Registry 与能力发现层收口

该事项先单独形成构想文档：[Registry 与能力发现层构想](addpRegistry与能力发现层构想.md)。

待讨论重点：

1. 是否需要统一能力发现 API，而不是统一 registry。
2. engine、dataitem、format、preview 等 registry 的职责边界如何固定。
3. 能力发现结果是否由 meta 落库，还是仅运行时查询。
4. 插件加载顺序和冲突处理如何记录。
5. 第三方插件是否可以同时注册 detector、parser、extractor、preview handler。

## 十、空间扩展标准口径

该事项移入 [ADDP 数据类型与文件格式跟进清单](addp数据类型与文件格式跟进清单.md)，作为 `capabilities.spatial` 最小字段集和跨格式映射任务推进。

## 十一、建议讨论顺序

1. whole scope 的 `explain/confidence`。
2. 对象存储跨层组件 whole scope 认领规则。
3. 第三方插件扩展声明机制。
4. Manager 内容预览插件能力描述。
5. Registry 与能力发现层收口。
6. 引擎原生 item 的 `format` 口径。

## 十二、当前可继续开展但不阻塞讨论的事项

- 做已支持格式的真实引擎端到端验证。
- 清理旧 attributes 读取和旧字段写入。
- 补齐新标准分区的字段映射。
- 为已有清晰规则的格式补 detector 和 parser 回归测试。
- 增加 normalizer 回归测试。
- 清理 Manager 中不符合新规范的历史兜底。
