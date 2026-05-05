# ADDP 格式规范：图片

更新时间：2026-05-05

本文定义图片文件在 ADDP meta item 中的 attributes 写入规则。

## 一、格式定位

| 维度 | 取值 |
|---|---|
| `item_type` | `object` 或 `file` 迁移期兼容 |
| `data_family` | `image` |
| `format` | `jpeg`、`png`、`gif`、`tiff`、`image` |
| `composition_type` | `single_file` |

## 二、字段来源

图片没有 `schema.fields`。基础对象属性来自存储枚举，媒体属性来自图片 extractor。

## 三、扩展归属

`extensions.media` 保存平台标准媒体信息：

- `width`
- `height`
- `format`
- `color_space`
- `bit_depth`
- `has_alpha`
- `orientation`

如图片包含 GPS，可同步写入 `extensions.spatial`，但不得把所有图片都视为空间数据。

## 四、实现要求

1. 图片尺寸、颜色模式等不得平铺到 attributes 顶层。
2. EXIF 中平台不消费的字段应进入合规私有命名空间。
3. GeoTIFF 的空间语义需另行补充空间影像规范。
