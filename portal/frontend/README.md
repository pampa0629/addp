# Portal Frontend - 统一门户

全域数据平台的统一入口，提供集成式用户体验。

## 功能

- **统一导航**: 左侧菜单整合所有模块功能
- **模块集成**: 通过 iframe 动态加载各模块前端
- **统一认证**: 一次登录，访问所有模块
- **模块卡片**: 首页展示所有可用模块

## 快速开始

### 开发环境启动

```bash
# 1. 安装依赖
npm install

# 2. 启动 Portal
npm run dev
# 访问: http://localhost:5170

# 3. 确保模块前端也在运行
# Terminal 2: cd system/frontend && npm run dev (port 5173)
# Terminal 3: cd manager/frontend && npm run dev (port 5174)
```

### 访问

开发环境: **http://localhost:5170**

## 架构说明

### 模块加载方式

Portal 使用 **iframe 嵌入** 的方式加载各模块前端：

```
Portal (5170)
├── Login Page - 统一登录
└── Portal Page
    ├── Left Sidebar - 全局导航
    │   ├── 系统管理
    │   ├── 数据管理
    │   ├── 元数据
    │   └── 数据传输
    └── Main Area - iframe 动态加载
        ├── System Frontend (5173)
        ├── Manager Frontend (5174)
        ├── Meta Frontend (5175)
        └── Transfer Frontend (5176)
```

### 模块映射

Portal 根据菜单选择动态加载对应模块：

| 菜单项 | 加载模块 | URL |
|--------|---------|-----|
| 用户管理 | System | http://localhost:5173/users |
| 日志管理 | System | http://localhost:5173/logs |
| 存储引擎 | System | http://localhost:5173/resources |
| 数据源管理 | Manager | http://localhost:5174/ |
| 目录管理 | Manager | http://localhost:5174/directories |
| 数据预览 | Manager | http://localhost:5174/preview |

### 认证机制

- Portal 和各模块都使用相同的 JWT token
- Token 存储在 localStorage
- Portal 登录后，各模块自动共享认证状态
- 各模块也可以独立登录（standalone 模式）

## 与模块的关系

### Portal 的角色

Portal 是**用户界面层的聚合器**，不包含业务逻辑：
- 提供统一的用户入口和导航
- 集成展示各个独立模块
- 处理认证和权限控制

### 模块的独立性

各模块前端保持完全独立：
- 可以脱离 Portal 独立运行
- 有自己完整的路由和状态管理
- 可以单独部署给需要该模块的用户

## 生产部署

生产环境中，Portal 会被部署到 Gateway 的 8000 端口：

```yaml
portal-frontend:
  build: ./portal/frontend
  ports:
    - "8000:80"
```

用户访问 http://yourdomain.com 即可看到统一门户。

## 技术栈

- Vue 3 + Composition API
- Element Plus
- Pinia (状态管理)
- Vue Router
- iframe 通信