# Manager 插件配置

本目录只描述 Manager 的数据预览行为策略，不描述文件格式事实。格式名称、扩展名、MIME、数据类型和能力声明统一来自 `common/format` descriptor。

运行时读取两个固定配置文件：

```text
plugins/
├── README.md
├── preview.json   # 主预览 Provider 的禁用、覆盖和 command 扩展
└── content.json   # 对象内容 Handler 的禁用、覆盖和 command 扩展
```

默认 PreviewProvider 和 ObjectContentHandler 均由 Go fallback 注册。配置文件只用于禁用默认实现、覆盖少量策略参数，或接入外部 command 扩展；同名配置会替换默认实现，不形成并行分支。

## preview.json

`preview.json` 只支持顶层字段 `version`、`description`、`providers`、`notes`。未知字段会被拒绝加载。

PreviewProvider 负责选择数据源或节点层级的预览方式，例如数据库表、对象 catalog、schema 节点、容器子项等。主链路由 Meta 标准属性确定性选择 provider，配置不参与格式语义判断。

禁用内置 provider 示例：

```json
{
  "providers": [
    {
      "name": "builtin:file-catalog",
      "type": "builtin",
      "builtin": "file-catalog",
      "enabled": false
    }
  ]
}
```

接入外部 command provider 示例：

```json
{
  "providers": [
    {
      "name": "command:custom-provider",
      "type": "command",
      "command": "/opt/addp/preview/custom-provider",
      "timeout": 15
    }
  ]
}
```

## content.json

`content.json` 只支持顶层字段 `version`、`description`、`content_plugins`、`notes`。未知字段会被拒绝加载。

ObjectContentHandler 负责具体文件内容的展示策略，例如图片、视频、音频、PDF、Office 文档、JSON、文本和容器文件。内置处理器的匹配规则来自 `common/format` descriptor；例如 WebP、AVIF、MP4、FLAC 等具体格式不在 Manager 配置中重复维护。

覆盖内置 handler 策略示例：

```json
{
  "content_plugins": [
    {
      "name": "builtin:content-container",
      "type": "builtin",
      "builtin": "container",
      "max_bytes": 1073741824
    }
  ]
}
```

接入外部 command content handler 示例：

```json
{
  "content_plugins": [
    {
      "name": "command:content-custom",
      "type": "command",
      "command": "/opt/addp/preview/custom-preview",
      "match": {
        "formats": ["custom"]
      },
      "max_bytes": 10485760
    }
  ]
}
```

## 扩展原则

新增格式的事实优先加入 `common/format`。如果新格式能落到已有通用处理器，例如 media、text、raw document、container 或 parquet table，Manager 不需要新增配置。

只有新增 Manager 展示行为时，才修改本目录配置；如果行为需要新的 Go 实现，先实现 handler/provider/factory，再在 `content.json` 或 `preview.json` 中启用、覆盖或禁用。

## 调用链

```text
用户请求预览
  ↓
PreviewResolver 根据 Meta 标准属性选择默认注册的 Provider
  ↓
Provider 执行对应预览
  ↓
如果是对象内容预览，调用 ObjectContentRegistry
  ↓
ContentHandler 优先根据 Meta 标准 format 匹配，必要时再使用扩展名和 Content-Type
```

## 参考代码

- 主预览加载器: [internal/preview/preview_plugin_loader.go](../internal/preview/preview_plugin_loader.go)
- 内容加载器: [internal/objectcontent/object_content_plugin_loader.go](../internal/objectcontent/object_content_plugin_loader.go)
- 配置字段校验: [internal/pluginmanifest/manifest.go](../internal/pluginmanifest/manifest.go)
