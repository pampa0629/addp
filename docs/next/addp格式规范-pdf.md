# ADDP 格式规范：PDF

更新时间：2026-05-05

本文定义 PDF 文件在 ADDP meta item 中的 attributes 写入规则。

## 一、格式定位

| 维度 | 取值 |
|---|---|
| `item_type` | `object` 或 `file` 迁移期兼容 |
| `data_family` | `document` |
| `format` | `pdf` |
| `composition_type` | `single_file` |

## 二、字段来源

PDF 没有表格型 `schema.fields`。页数、标题、作者、加密状态等来自 PDF extractor；文本预览和全文提取状态进入提取扩展。

## 三、扩展归属

`extensions.document` 保存文档标准信息：

- `page_count`
- `title`
- `author`
- `subject`
- `creator`
- `producer`
- `creation_date`
- `modified_date`
- `encrypted`

`extensions.extraction` 保存提取状态和摘要：

- `metadata_extracted`
- `extractor_available`
- `extracted_metadata`
- `plain_text_preview`

## 四、实现要求

1. 文档字段不得平铺到 attributes 顶层。
2. 搜索索引优先消费 `extensions.document` 和 `extensions.extraction`。
3. 大文本不得直接写入 attributes；只允许写预览、摘要或外部索引引用。
