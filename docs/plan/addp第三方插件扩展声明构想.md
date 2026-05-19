# ADDP 第三方插件扩展声明构想

更新时间：2026-05-06

本文是基于 next 阶段数据类型、内容布局、文件格式和 attributes 规范的构想文档，不作为最终规范。

## 目标

第三方插件可以扩展 detector、format plugin、info provider、content reader 和 preview 能力，但不能直接污染平台核心 attributes。插件输出应显式声明自己写入的 `format_info` 私有命名空间和 `capabilities` 横切能力命名空间。

## 基本原则

- 平台核心字段只由 meta normalizer 裁决。
- 插件不得覆盖 `storage`、`item` 和标准 `type_info` 字段。
- 格式私有信息进入 `format_info.<namespace>`。
- 横切能力进入 `capabilities.<capability>`，新增非标准 capability 需要声明。
- 私有字段默认只用于展示、诊断或插件自身处理；平台稳定消费前必须先晋升为标准字段。

## manifest 构想

```json
{
  "id": "vendor-format-x",
  "version": "0.1.0",
  "attributes": {
    "format_info_namespaces": [
      {
        "name": "com.vendor.format_x",
        "owner": "vendor-format-x",
        "version": 1,
        "fields": [
          {
            "name": "sensor_model",
            "type": "string",
            "display": true,
            "index": false,
            "diagnostic": false
          }
        ]
      }
    ],
    "capabilities": [
      {
        "name": "spatial",
        "writes_standard_fields": ["geometry_columns", "extent"]
      }
    ]
  }
}
```

## 待讨论

1. 私有命名空间是否强制反向域名。
2. 字段类型是否只使用平台基础类型，还是允许 JSON Schema 子集。
3. 插件字段是否允许被 search 默认索引。
4. 字段冲突是否进入扫描日志，还是进入 normalizer 诊断表。
5. 私有字段晋升为标准字段时，是否要求补迁移脚本；当前阶段倾向不迁移旧数据，重新扫描生成。
