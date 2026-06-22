# Manager Frontend

数据管理模块的前端应用。

## 功能

- **数据探查**: 浏览存储引擎中的数据资源（数据库、表、对象存储等）
- **数据预览**: 预览各种格式的数据文件（空间数据、文档、图片等）
- **数据检索**: 基于全文检索和语义检索的数据资产查询
- **向量化任务**: 管理数据向量化任务
- **空间数据服务**: MVT 瓦片服务和空间数据可视化

## 开发

```bash
# 安装依赖
npm install

# 开发模式（端口 5174）
npm run dev

# 构建生产版本
npm run build

# 预览生产版本
npm run preview
```

## 架构说明

### 认证

Manager 前端使用 System 模块的认证服务：
- 登录请求发送到 System backend (localhost:8180/api/v1/system/login)
- JWT token 存储在 localStorage
- 所有请求携带 token 访问 Manager backend

### API 端点

**开发模式**:
- 认证 API: `http://localhost:8180/api/v1/system/*`
- Manager API: `http://localhost:8081/api/v1/manager/*`

**生产模式**:
- 所有请求通过 Gateway: `http://localhost:8000/api/v1/*`
- Gateway 自动路由到相应的后端服务

### 路由

所有路由使用 `/manager/` 作为 base path:
- `/manager/data-explorer` - 数据探查（默认首页）
- `/manager/data-retrieval` - 数据检索
- `/manager/vectorization-tasks` - 向量化任务
- `/manager/spatial-preview` - 空间预览

**注意**: 引擎管理由 System 模块负责，Manager 模块仅提供数据访问和预览服务。

## Docker 部署

```bash
# 从项目根目录
docker-compose --profile full up -d manager-frontend

# 访问
# http://localhost:8091
```

## 技术栈

- Vue 3 + Composition API
- Vite
- Element Plus
- Pinia (状态管理)
- Vue Router
- Axios
