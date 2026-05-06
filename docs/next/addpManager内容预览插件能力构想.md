# ADDP Manager 内容预览插件能力构想

更新时间：2026-05-06

本文是基于 next 阶段数据类型、组织方式、文件格式和 attributes 规范的构想文档，不作为最终规范。

## 目标

Manager 内容预览插件应消费 meta 已识别的标准 data item，不重新判断组织方式，不按扩展名抢路由，不自行枚举 sibling 组件。

## 匹配输入

插件匹配只基于标准字段：

- `meta_item.item_type`
- `meta_item.full_name`
- `attributes.item.organization`
- `attributes.item.data_type`
- `attributes.item.format`
- `attributes.item.component_files`
- `attributes.storage.physical_path`
- `attributes.type_info`
- `attributes.format_info`
- `attributes.capabilities`

## manifest 构想

```json
{
  "id": "builtin-shapefile-preview",
  "version": "0.1.0",
  "preview": {
    "match": {
      "organization": ["multi"],
      "data_type": ["table"],
      "format": ["shapefile"],
      "capabilities": ["spatial"]
    },
    "priority": 100,
    "input": {
      "requires_component_files": true
    },
    "output": {
      "kind": "map_table",
      "stream": false
    }
  }
}
```

## 约束

- `priority` 只在同一标准匹配结果内解决冲突。
- 插件不得用扩展名、MIME 或 provider 优先级覆盖 meta 识别结果。
- multi item 使用 `meta_item.full_name` 作为主资源，并使用 `component_files` 读取组件资源。
- whole item 使用 `meta_item.full_name` 作为 whole scope 根范围；manifest 等格式入口写入 `format_info.<format>`。
- 私有 `format_info` 只能用于展示细节，不得改变核心路由。

## 待讨论

1. 输出 `kind` 是否需要统一枚举。
2. command 型预览是否需要输入 payload schema。
3. 大文件 preview 是否统一声明 stream / paging / tile 能力。
4. spatial preview 缺失 `capabilities.spatial` 时是否拒绝还是降级。
5. 插件异常是否进入 Manager 诊断面板。
