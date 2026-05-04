# Console 模块说明

## 模块定位

Console 是 ADDP 的统一前端入口，负责登录、全局导航、主题/语言切换、模块健康和 Swagger 文档入口，并通过 iframe 集成各业务模块前端。

## 技术栈与端口

- 前端：Vue 3 + Vue Router + Pinia + Element Plus。
- 开发端口：`5170`，启动脚本环境变量 `CONSOLE_FE_PORT`。
- 开发代理：`/api` 统一代理到 Gateway `http://localhost:8000`。

## 重要目录

```text
console/frontend/
├── src/
│   ├── views/             # Login、Portal、ApiDocs
│   ├── components/portal/ # PortalHeader、PortalSidebar、PortalIframe、PortalHome
│   ├── config/            # portalConfig、searchIndex
│   ├── store/             # auth、lang、theme
│   └── api/               # auth、client、copilot、meta
├── vite.config.js
└── README.md
```

## 开发规则

- 新增前端模块入口时，优先更新 `console/frontend/src/config/portalConfig.js`，并同步健康检查或 Swagger 代理配置。
- Console 只做入口聚合，不承载业务模块的核心业务逻辑。
- 前端样式遵守 `common-frontend/docs/addp前端风格设计规范.md`，不要硬编码 ADDP 主题色。
- 各模块仍应支持独立运行，Console iframe 集成不能破坏 standalone 模式。

## 开发与验证

```bash
bash scripts/dev/start.sh -console
cd console/frontend && npm run build
```

访问：`http://localhost:5170`

## 相关文档

- `console/frontend/README.md`
- `common-frontend/CLAUDE.md`
- `common-frontend/docs/addp前端风格设计规范.md`
- `docs/addp部署和开发步骤.md`
