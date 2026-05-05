# ADDP 格式规范：Excel

更新时间：2026-05-05

本文定义 Excel 文件在 ADDP meta item 中的 attributes 写入规则。

## 一、格式定位

| 维度 | 取值 |
|---|---|
| `item_type` | `table` 或 `file` 迁移期兼容 |
| `data_family` | `tabular` |
| `format` | `excel` |
| `composition_type` | `single_file` |

## 二、字段来源

- 默认工作表由 parser 策略决定。
- 字段名可来自首行表头；无表头时生成稳定列名。
- 字段类型来自采样推断。
- 多工作表信息进入格式私有扩展；是否拆成多个子 item 属于后续规范事项。

## 三、扩展归属

`extensions.builtin.excel` 保存 Excel 私有信息：

- `sheet_name`
- `sheet_index`
- `sheet_count`
- `has_header`
- `sample_size`

## 四、实现要求

1. 当前阶段一个 Excel 文件先作为一个 item。
2. 不得把 sheet 信息写入 attributes 顶层。
3. 如未来支持按 sheet 展开子 item，应先补充容器 / 子 item 规范。
