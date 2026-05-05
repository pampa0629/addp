# ADDP 格式规范：CSV / TSV

更新时间：2026-05-05

本文定义 CSV / TSV 在 ADDP meta item 中的 attributes 写入规则。

## 一、格式定位

| 维度 | CSV | TSV |
|---|---|---|
| `item_type` | `table` 或 `file` 迁移期兼容 | `table` 或 `file` 迁移期兼容 |
| `data_family` | `tabular` | `tabular` |
| `format` | `csv` | `tsv` |
| `composition_type` | `single_file` | `single_file` |

## 二、字段来源

- 表头存在时，字段名来自表头。
- 无表头时，字段名由 parser 生成，例如 `column_1`、`column_2`。
- 字段类型来自采样推断。
- 记录数如无法完整扫描，可为空；采样信息应写入扩展。

## 三、扩展归属

`extensions.builtin.csv` 保存 CSV / TSV 私有信息：

- `delimiter`
- `encoding`
- `has_header`
- `quote_char`
- `escape_char`
- `line_ending`
- `sample_size`

统计和采样摘要可进入 `extensions.statistics`。

## 四、实现要求

1. `format` 由 Meta 标准识别结果决定，Manager 不得按扩展名二次猜测分隔符。
2. 编码、分隔符、表头判断属于 parser 结果，不得平铺到 attributes 顶层。
3. `schema.fields` 必须有明确采样策略。
