# 对象内容插件

该目录存放对象存储内容处理插件的 JSON 配置。系统默认提供若干 `builtin:content-*` 插件，包括 PDF、Office 文档、图片、GeoJSON/JSON、SQLite、文本等处理器，示例也可作为第三方扩展的模板。

每个配置可声明匹配的扩展名或 Content-Type，并选择 `builtin` 或 `command` 模式。
