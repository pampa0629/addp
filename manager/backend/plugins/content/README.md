# 对象内容插件

该目录存放对象存储内容处理插件的 JSON 配置。系统默认提供若干 `builtin:content-*` 插件，包括 PDF、Office 文档、图片、视频、JSON、容器、文本等处理器，示例也可作为第三方扩展的模板。空间 JSON 仍使用标准 `format=json`，由 JSON 处理器根据内容事实输出地图预览材料。

每个配置可声明匹配的 Meta 标准 `format`、扩展名或 Content-Type，并选择 `builtin` 或 `command` 模式。匹配时优先使用 `format`，只有未提供或为 `unknown` 时才回退到扩展名和 Content-Type。
