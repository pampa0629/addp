# Common-Frontend 共享前端模块说明

本页导航：[模块定位](#模块定位) · [重要目录](#重要目录) · [开发规则](#开发规则) · [验证](#验证) · [相关文档](#相关文档)。

## 模块定位

`common-frontend/` 是 ADDP Vue 前端共享库，提供基础 UI、地图预览、资源树、资源树选择器、认证组合式函数、DAG/Graph 相关组件和通用格式化工具。

## 重要目录

```text
common-frontend/
├── basic/          # 基础组件、认证 composables、资源树选择器等
├── chart/          # 数据服务结果图表
├── map/            # OpenLayers/地图相关预览组件
├── dag/            # DAG 组件与相关工具
├── graph/          # 图相关共享前端能力
├── agent-ui/       # Agent A2UI 共享协议适配
└── docs/           # 架构、风格、资源树选择器等文档
```

## 开发规则

- 新增前端页面或共享组件前，先阅读[前端风格设计规范](docs/addp前端风格设计规范.md)和[共享前端总入口](README.md)。
- 资源选择按[README 接入导航](README.md#资源树选择器接入导航)定位场景；共享组件与 Owner 的职责、权限复核和失败语义以[选择器职责边界](docs/components/资源定位与树组件.md#表单选择与领域树的职责边界)为准，不在模块入口重复维护细则。
- AI 助手类功能必须遵守风格规范中的“AI 助手入口规范”：默认使用魔法棒入口并固定在页面右下角，各模块保持入口和助手面板的基础交互一致；可复用的入口、面板和状态能力优先沉淀到 `common-frontend/`。
- 不要硬编码 ADDP 主题色，应使用平台主题变量和已有共享能力。
- `common-frontend` 不应保留自己的 `node_modules`；各前端模块通过 `overrides` 和 Vite alias 保持 Vue 单一实例。
- 地图相关组件放在 `map/`，不引入地图依赖的基础组件放在 `basic/`。
- 文件预览组件从 `@common-ui/previews` 按需导入，使用方模块自行声明 `geotiff`、`marked`、`mammoth`、`jszip` 等预览依赖；不要从 `@common-ui` 主入口导出预览组件。
- 修改共享组件后至少验证一个实际消费模块，例如 `manager`、`develop`、`asset` 或 `portal`。

模块的公开开发入口由 [moduleFrontend.mjs](basic/src/utils/moduleFrontend.mjs) 唯一拥有：`withModuleFrontend()` 统一开发与生产的 base、HMR 及顶层页面重定向，Console 的 `createModuleFrontendProxies()` 统一代理模块资源。模块开发端口只承载内部服务，独立界面也从 Console origin 的正式路径访问，避免跨端口轮换同一刷新 Cookie。

接入步骤统一见[新模块前端接入指南](../docs/spec/addp新模块开发指南.md#创建新模块前端)，公开路径与导航规则见[前端路由事实源](../docs/spec/addp前端路由与可恢复状态规范.md#二公开路由事实源)。

## 验证

共享组件、契约和唯一所有权测试统一按[README 测试入口](README.md#测试)执行；消费模块使用已登记的标准前端门禁验证自身组合，浏览器夹具、端口与清理规则见[浏览器测试隔离](README.md#浏览器测试隔离)。测试层级与门禁选择遵循[测试与验收规范](../docs/spec/addp测试与验收规范.md#三标准入口)。

构建、确定性浏览器回归与真实后端验收分别记录，未运行、失败或跳过项不计为通过。根据实际改动选择受影响模块的标准门禁，不需要为了纯前端修改重启后端。

## 相关文档

| 阅读目的 | 权威入口 |
| --- | --- |
| 组件索引、共享验证与依赖 | [共享前端总入口](README.md) |
| 子包职责、导出与接入 | [共享前端架构](docs/ARCHITECTURE.md) |
| 资源选择契约、接入示例与验收范围 | [资源树选择器](docs/components/资源定位与树组件.md#资源树选择器)、[选择与创建示例](docs/components/资源定位与树组件.md#选择与创建示例) |
| 视觉、交互、主题与工程约束 | [前端风格设计规范](docs/addp前端风格设计规范.md) |
| 地图预览、CRS 与底图展示 | [Map 说明](map/README.md) |
| DAG 编辑、只读展示与 API 分类 | [DAG 说明](dag/README.md) |
| 认证组合式函数的使用 | [认证使用指南](basic/composables/AUTH_USAGE_GUIDE.md) |
| Agent 受信任组件与协议边界 | [交互协议](../docs/spec/addp智能体交互协议规范.md)、[评测规范](../docs/spec/addp智能体评测规范.md) |
