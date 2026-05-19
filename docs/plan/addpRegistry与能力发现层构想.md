# ADDP Registry 与能力发现层构想

更新时间：2026-05-06

本文是基于 next 阶段数据类型、内容布局、文件格式和插件化规范的构想文档，不作为最终规范。

## 背景

ADDP 存在多个 registry：engine、dataitem、format、preview、object content。它们职责不同，不应简单合并成一个大注册中心。

## 倾向

保留多个职责清晰的 registry，新增统一能力发现视图。

| Registry | 职责 |
|---|---|
| engine registry | 连接、catalog、读取和引擎能力 |
| dataitem registry | detector、layout 识别、claims |
| format registry | format plugin、info provider、content reader、type_info、format_info、capabilities |
| preview registry | 已识别 data item 的展示能力 |
| transfer registry | 导入、导出、转换和传输能力 |

## 能力发现视图

能力发现层不替代各 registry，只汇总可观测能力：

```json
{
  "plugin_id": "builtin-shapefile",
  "detects": [
    {
      "layout": "multi",
      "data_type": "table",
      "format": "shapefile"
    }
  ],
  "parses": ["type_info.table", "format_info.shapefile", "capabilities.spatial"],
  "previews": ["table", "table.spatial"],
  "transfers": ["read", "export"]
}
```

## 待讨论

1. 能力发现结果是否落库，还是运行时查询。
2. 插件加载顺序和冲突处理如何记录。
3. 同一插件同时注册 detector、format plugin、info provider、content reader、preview handler 时，是否需要事务式启停。
4. 能力发现视图是否暴露给管理端 UI。
5. meta 扫描是否记录“当时使用了哪些能力版本”。
