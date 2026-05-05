# ADDP 格式规范：Parquet / ORC / Avro 湖表

更新时间：2026-05-05

本文定义 Parquet、ORC、Avro 单文件或目录树数据集在 ADDP meta item 中的 attributes 写入规则。

## 一、格式定位

| 场景 | `data_family` | `format` | `composition_type` |
|---|---|---|---|
| 单个 Parquet 文件 | `tabular` | `parquet` | `single_file` |
| Parquet 目录树 | `tabular` | `parquet` | `directory_tree` |
| ORC / Avro 单文件或目录树 | `tabular` | `orc` / `avro` | `single_file` 或 `directory_tree` |

目录树 item 的 `component_files` 只包含数据文件，不包含 `_SUCCESS`、`_metadata`、CRC 等辅助文件。

## 二、字段来源

- Parquet schema 直接生成 `schema.fields`。
- ORC / Avro 如 parser 暂未实现 schema 解析，可先只写 item 和 storage 分区，并在改进清单中标注差距。
- 分区字段来自目录结构解析，应进入 `schema` 或 `extensions.builtin.lake_table`，不得混入 `item.component_files`。

## 三、扩展归属

`extensions.builtin.lake_table` 保存湖表私有信息：

- `partition_columns`
- `partition_values_sample`
- `data_file_count`
- `auxiliary_file_count`
- `detected_format_counts`

统计信息进入 `extensions.statistics`。

## 四、实现要求

1. 目录树 detector 必须先于单文件格式处理。
2. `storage.total_size` 只统计 item 组件文件的大小，辅助文件是否计入必须在 detector 策略中明确。
3. Manager / Transfer 使用 `item.entry_path` 和 `item.component_files`，不得重新遍历目录猜测湖表。
