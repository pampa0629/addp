# Develop Module - SQL 开发模块

ADDP 平台的 SQL 开发模块，提供基于 Web 的 SQL 编辑器，支持多数据库连接和查询执行。

## ✨ 功能特性

- **Monaco Editor** - 与 VS Code 相同的专业编辑器
  - SQL 语法高亮
  - 代码格式化 (sql-formatter)
  - 快捷键支持 (Ctrl+Enter 执行)
  - 暗黑主题

- **多数据库支持**
  - PostgreSQL
  - MySQL
  - 支持扩展更多数据库

- **查询执行**
  - 实时 SQL 执行
  - 结果表格展示
  - 执行历史记录
  - 超时控制 (最长 5 分钟)

- **结果处理**
  - 分页加载
  - 列排序
  - 导出 CSV
  - 错误提示

## 🚀 快速开始

### 开发模式

```bash
# 1. 启动后端 (端口 8085)
cd develop/backend
go run cmd/server/main.go

# 2. 启动前端 (端口 5177)
cd develop/frontend
npm install
npm run dev

# 访问: http://localhost:5177
```

### Docker 部署

```bash
# 从项目根目录
docker-compose -f develop/docker-compose.yml up -d

# 访问后端: http://localhost:8085
# 访问前端: http://localhost:8094
```

## 🏗️ 项目结构

```
develop/
├── backend/
│   ├── cmd/server/main.go          # 应用入口
│   ├── internal/
│   │   ├── api/                    # HTTP 处理器
│   │   │   ├── sql_handler.go     # SQL 执行接口
│   │   │   └── router.go          # 路由配置
│   │   ├── service/
│   │   │   └── sql_execution_service.go  # SQL 执行服务
│   │   ├── repository/
│   │   │   ├── database.go        # 数据库连接
│   │   │   └── execution_repository.go
│   │   ├── models/
│   │   │   ├── execution.go       # 执行记录模型
│   │   │   └── script.go          # 脚本模型 (Phase 2)
│   │   ├── middleware/
│   │   │   └── auth.go            # JWT 认证
│   │   └── config/
│   │       └── config.go          # 配置管理
│   ├── Dockerfile
│   └── .env                        # 配置文件
│
└── frontend/
    ├── src/
    │   ├── views/
    │   │   └── SQLEditor.vue       # SQL 编辑器主界面
    │   ├── components/
    │   │   ├── MonacoEditor.vue    # Monaco 编辑器组件
    │   │   └── SQLResult.vue       # 结果表格组件
    │   ├── api/
    │   │   ├── client.js           # Axios 客户端
    │   │   ├── auth.js             # 认证 API
    │   │   └── sql.js              # SQL API
    │   └── router/
    │       └── index.js            # 路由配置
    ├── Dockerfile
    ├── nginx.conf
    └── vite.config.js
```

## 📡 API 接口

### SQL 执行
```
POST /api/develop/execute
Authorization: Bearer <token>

Request:
{
  "resource_id": 1,
  "sql": "SELECT * FROM users LIMIT 10",
  "timeout": 30000
}

Response:
{
  "columns": ["id", "name", "email"],
  "rows": [
    {"id": 1, "name": "Alice", "email": "alice@example.com"}
  ],
  "execution_time_ms": 45,
  "rows_count": 1
}
```

### 测试连接
```
GET /api/develop/test/:resource_id
Authorization: Bearer <token>

Response:
{
  "success": true,
  "message": "连接测试成功"
}
```

### 健康检查
```
GET /api/develop/health

Response:
{
  "status": "ok",
  "service": "develop",
  "version": "0.1.0"
}
```

## ⚙️ 配置

### 环境变量 (backend/.env)

```bash
# Server
PORT=8085
ENV=development

# System Service Integration
SYSTEM_SERVICE_URL=http://localhost:8080
ENABLE_SERVICE_INTEGRATION=true

# Database (fallback)
DB_HOST=localhost
DB_PORT=5432
DB_USER=addp
DB_PASSWORD=addp_password
DB_NAME=addp
DB_SCHEMA=develop

# JWT (from System service)
JWT_SECRET=your-secret-key

# Encryption (from System service)
ENCRYPTION_KEY=your-32-byte-key
```

## 🔐 认证流程

1. 用户通过 System 模块登录获取 JWT token
2. 前端将 token 存储在 localStorage
3. 每次 API 请求携带 `Authorization: Bearer <token>` 头
4. 后端验证 token (委托给 System 服务)
5. 从 token 提取 user_id 和 tenant_id

## 🗄️ 数据库 Schema

```sql
-- develop.executions - 执行历史记录
CREATE TABLE develop.executions (
    id SERIAL PRIMARY KEY,
    resource_id INTEGER NOT NULL,
    sql_content TEXT NOT NULL,
    status VARCHAR(20) NOT NULL,
    rows_affected INTEGER,
    execution_time_ms INTEGER,
    error_message TEXT,
    executed_by INTEGER NOT NULL,
    tenant_id INTEGER NOT NULL,
    started_at TIMESTAMP DEFAULT NOW(),
    completed_at TIMESTAMP
);

-- 索引
CREATE INDEX idx_executions_tenant ON develop.executions(tenant_id);
CREATE INDEX idx_executions_started ON develop.executions(started_at DESC);
```

## 🛣️ Roadmap

### Phase 1: 基础 SQL 编辑器 ✅ (已完成)
- [x] Monaco Editor 集成
- [x] SQL 执行和结果展示
- [x] 多数据源连接
- [x] JWT 认证
- [x] 执行历史记录

### Phase 2: 脚本管理 (计划中)
- [ ] SQL 脚本保存
- [ ] 版本管理
- [ ] 上线/下线管理
- [ ] 脚本列表页面

### Phase 3: 高级功能 (计划中)
- [ ] SQL 自动补全 (基于元数据)
- [ ] 语法验证
- [ ] 查询优化建议
- [ ] 执行统计和监控

### Phase 4: 依赖管理 (计划中)
- [ ] SQL 节点依赖关系
- [ ] DAG 可视化
- [ ] 批量执行

## 🔧 开发指南

### 添加新数据库驱动

在 `sql_execution_service.go` 中添加新的连接类型：

```go
func (s *SQLExecutionService) getDBConnection(resource *Resource) (*gorm.DB, error) {
    switch strings.ToLower(resource.ResourceType) {
    case "postgresql":
        return s.connectPostgreSQL(resource)
    case "mysql":
        return s.connectMySQL(resource)
    case "clickhouse":  // 新增数据库
        return s.connectClickHouse(resource)
    default:
        return nil, fmt.Errorf("不支持的数据库类型: %s", resource.ResourceType)
    }
}
```

### 扩展 Monaco Editor 功能

在 `MonacoEditor.vue` 中添加新特性：

```javascript
// 添加自定义补全提供者
monaco.languages.registerCompletionItemProvider('sql', {
  provideCompletionItems: (model, position) => {
    // 返回自定义补全项
  }
})
```

## 📝 注意事项

- **端口配置**: 后端使用 8085 端口(避免与 Orchestrator 8084 冲突)
- **超时控制**: SQL 执行默认 30 秒超时，最长 5 分钟
- **连接池管理**: 连接池 TTL 为 30 分钟，自动清理过期连接
- **安全性**: 不支持 DDL 操作 (CREATE/DROP/ALTER)，仅限 DQL/DML

## 🤝 贡献

欢迎提交 Issue 和 Pull Request！

## 📄 License

MIT License
