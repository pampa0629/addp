# Manager 前端快速开始

从 [Manager 模块 README](../README.md)进入开发启动与部署说明。开发模式按模块的标准脚本启动 Manager 和依赖，以启动输出及 `.dev-state/ports.env` 确认访问地址。页面准入和默认落点见[前端公开路由](../CLAUDE.md#前端公开路由)。

## 预览接入

- Manager 负责资源树、定位和预览编排。预览请求和材料选择分别见[数据预览与资源树实现规范](../docs/数据预览与资源树实现规范.md)和[数据预览语义协议](../docs/数据预览语义协议.md)。
- 通用表格和地图预览复用[共享前端模块](../../common-frontend/README.md)，Manager 通过 `@common-ui` 和 `@common-ui-map` 引入共享组件。
- 自定义运行时预览插件放在 `public/plugins/`，通过 `/plugins/manifest.json` 登记脚本。清单配置、注册方式和示例见[自定义预览插件开发指南](public/plugins/README.md)。

## 验证

按 [Manager 开发与验证](../CLAUDE.md#开发与验证)选择标准门禁。需要验证真实资源树或预览链路时，按 [Manager Online 场景](../../docs/spec/testing/scenarios/Manager与空间成果.md)确认身份、权限、断言和清理范围。
