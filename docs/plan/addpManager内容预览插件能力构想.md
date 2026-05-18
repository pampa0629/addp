# ADDP Manager 内容预览插件能力构想

更新时间：2026-05-06

本文是基于 next 阶段数据类型、组织方式、文件格式和 attributes 规范的构想文档，不作为最终规范。

## 目标

Manager 内容预览插件应消费 meta 已识别的标准 data item，不重新判断组织方式，不按扩展名抢路由，不自行枚举 sibling refs。
插件本身不直接承担格式解析职责；它依赖资源读取抽象、FormatPlugin、info provider 和 content reader 提供的结果，再组装最终 preview。

## 表预览统一口径

Manager 的表预览不需要对外再拆成 `filetable` 和 `laketable` 两套能力。

对 Manager 来说，它们只是同一类 `table preview` 的不同来源路径：

- `filetable`：单文件或多相关文件形成的表格来源。
- `laketable`：目录型或范围型表格来源。
- `native table`：引擎原生表格来源。

对外应统一成一套表预览 DTO 和一套表预览能力声明；差异只保留在内部读取计划和资源抽象层，不直接下沉到展示层。

因此，preview manifest 中不建议再把 `filetable`、`laketable` 作为独立预览类型，而应围绕 `data_type=table`、`format`、`organization`、`capabilities` 来做匹配。

当前实现已经先把底层读取链路收口到 `TableProvider` / `MultiTableProvider` / `ScopeTableProvider`。`builtin:scope-table` 作为目录型表格来源路由，直接对应 `item.data_type=table + item.organization=whole`。

Manager 请求层已经新增 `ScopePath`，用于承载 `organization=whole` 的目录型表格范围；`PhysicalPath` 只用于 `organization=single` 的单文件表。Provider 选择基于 `data_type=table + organization`：whole table 走 `builtin:scope-table`，single 文件表走 `builtin:file-table`。新扫描结果不再使用 `item_type=lake_table`。

## 匹配输入

插件匹配只基于标准字段：

- `meta_item.item_type`
- `meta_item.full_name`
- `attributes.item.organization`
- `attributes.item.data_type`
- `attributes.item.format`
- `attributes.item.refs`
- `attributes.storage.physical_path`
- `attributes.type_info`
- `attributes.format_info`
- `attributes.capabilities`

其中，插件不应直接依赖 `engine id` 构造读取器，也不应自己找 sibling refs。
应由上层编排层先构造 `contentio.Reader / contentio.MultiReader / NativeCursor`，再交给插件或其依赖的 provider。
对于表预览，编排层可以根据资源组织方式选择不同读取计划，但对插件暴露的仍应是统一的表格输入。

## manifest 构想

```json
{
  "id": "builtin-table-preview",
  "version": "0.1.0",
  "preview": {
    "match": {
      "data_type": ["table"],
      "organization": ["single", "multi", "whole"]
    },
    "priority": 100,
    "input": {
      "requires_resource_reader": true
    },
    "output": {
      "mode": "table",
      "stream": false,
      "paging": true
    }
  }
}
```

## 约束

- `priority` 只在同一标准匹配结果内解决冲突。
- 插件不得用扩展名、MIME 或 provider 优先级覆盖 meta 识别结果。
- multi item 使用 `meta_item.full_name` 作为主资源，并使用 `refs` 读取ref 资源。
- whole item 使用 `meta_item.full_name` 作为 whole scope 根范围；manifest 等格式入口写入 `format_info.<format>`。
- 私有 `format_info` 只能用于展示细节，不得改变核心路由。
- 插件输出的 `kind` 是 Manager 展示语义，不是 format 的标准类型。

## 待讨论

1. 输出 `kind` 是否需要统一枚举。
2. command 型预览是否需要输入 payload schema。
3. 大文件 preview 是否统一声明 stream / paging / tile 能力。
4. spatial preview 缺失 `capabilities.spatial` 时是否拒绝还是降级。
5. 插件异常是否进入 Manager 诊断面板。
