# Manager Frontend

数据管理模块的前端应用。

## 功能

- **数据源管理**: 管理各类数据源连接（MySQL, PostgreSQL, S3, HDFS 等）
- **目录管理**: 组织和管理上传的数据文件
- **数据预览**: 预览各种格式的数据文件

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
- 登录请求发送到 System backend (localhost:8080/api/auth/login)
- JWT token 存储在 localStorage
- 所有请求携带 token 访问 Manager backend

### API 端点

**开发模式**:
- 认证 API: `http://localhost:8080/api/auth/*`
- Manager API: `http://localhost:8081/api/*`

**生产模式**:
- 所有请求通过 Gateway: `http://localhost:8000/api/*`
- Gateway 自动路由到相应的后端服务

### 路由

所有路由使用 `/manager/` 作为 base path:
- `/manager/` - 数据源管理
- `/manager/directories` - 目录管理
- `/manager/preview` - 数据预览

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