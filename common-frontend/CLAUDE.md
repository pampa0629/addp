# Common-Frontend 共享前端模块说明

## 模块定位

`common-frontend/` 是 ADDP Vue 前端共享库，提供基础 UI、地图预览、资源树、资源树选择器、认证组合式函数、DAG/Graph 相关组件和通用格式化工具。

## 重要目录

```text
common-frontend/
├── basic/          # 基础组件、认证 composables、资源树选择器等
├── map/            # OpenLayers/地图相关预览组件
├── dag/            # DAG 组件与相关工具
├── graph/          # 图相关共享前端能力
└── docs/           # 架构、风格、资源树选择器等文档
```

## 开发规则

- 新增前端页面或共享组件前，先阅读 `common-frontend/docs/addp前端风格设计规范.md` 和 `common-frontend/README.md`。
- AI 助手类功能必须遵守风格规范中的“AI 助手入口规范”：默认使用魔法棒入口并固定在页面右下角，各模块保持入口和助手面板的基础交互一致；可复用的入口、面板和状态能力优先沉淀到 `common-frontend/`。
- 不要硬编码 ADDP 主题色，应使用平台主题变量和已有共享能力。
- `common-frontend` 不应保留自己的 `node_modules`；各前端模块通过 `overrides` 和 Vite alias 保持 Vue 单一实例。
- 地图相关组件放在 `map/`，不引入地图依赖的基础组件放在 `basic/`。
- 文件预览组件从 `@common-ui/previews` 按需导入，使用方模块自行声明 `geotiff`、`marked`、`mammoth`、`jszip` 等预览依赖；不要从 `@common-ui` 主入口导出预览组件。
- 修改共享组件后至少验证一个实际消费模块，例如 `manager`、`develop`、`asset` 或 `portal`。

## 验证

```bash
cd console/frontend && npm run build
cd manager/frontend && npm run build
```

根据实际改动选择受影响模块构建，不需要为了纯前端修改重启后端。

## 相关文档

- `common-frontend/README.md`
- `common-frontend/docs/ARCHITECTURE.md`
- `common-frontend/docs/addp前端风格设计规范.md`
- `docs/spec/addp智能体交互协议规范.md`
- `docs/spec/addp智能体评测规范.md`
- `common-frontend/basic/composables/AUTH_USAGE_GUIDE.md`
