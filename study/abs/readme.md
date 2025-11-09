# ABS - AI Bootstrapping System

**AI-powered code generation and auto-deployment system**

ABS (AI-Bootstrapping) 允许你用自然语言描述想要构建的功能，系统会自动：
1. 使用 Claude AI 生成代码
2. 自动编译代码
3. 自动部署并运行应用

所有这些都通过一个简洁美观的浮动输入界面完成。

---

## Features

- 🤖 **AI Code Generation** - 基于 Claude Sonnet 4.5
- 📝 **Natural Language Input** - 用简单的中英文描述需求
- ⚡ **Auto-Compile** - 自动编译 Go 代码（更多语言即将支持）
- 🚀 **Auto-Deploy** - 自动启动应用程序
- 🔄 **Real-time Updates** - 基于 WebSocket 的实时进度追踪
- 🎨 **Beautiful UI** - 现代化界面，浮动输入框设计
- 📊 **Task History** - 追踪所有生成的应用历史

---

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│  Frontend (Vue 3 + Pinia)                                   │
│  - Floating input component                                 │
│  - Real-time task status updates via WebSocket             │
│  - Task history and management                             │
│  Port: 5180                                                 │
└─────────────────┬───────────────────────────────────────────┘
                  │
                  │ HTTP + WebSocket
                  ▼
┌─────────────────────────────────────────────────────────────┐
│  Backend (Go + Gin)                                         │
│  - REST API for task management                            │
│  - Claude API integration                                  │
│  - Code generation & compilation                           │
│  - WebSocket for real-time updates                        │
│  Port: 8090                                                │
└─────────────────┬───────────────────────────────────────────┘
                  │
                  │ HTTPS
                  ▼
┌─────────────────────────────────────────────────────────────┐
│  Claude API (Anthropic)                                     │
│  - Code generation via Messages API                        │
│  - Model: claude-sonnet-4-5-20250929                       │
└─────────────────────────────────────────────────────────────┘
```

---

## Quick Start

### Prerequisites

- **Go 1.23+** - 后端运行环境
- **Node.js 18+** - 前端运行环境
- **Anthropic API Key** - 在 https://console.anthropic.com/ 获取

### Installation

```bash
# 1. 进入 ABS 目录
cd /path/to/addp/study/abs

# 2. 初始化（安装依赖并创建配置文件）
make init

# 3. 编辑 backend/.env 添加你的 Claude API key
vim backend/.env
# 设置: CLAUDE_API_KEY=sk-ant-...
```

### Running ABS

**方式 1: 使用启动脚本（推荐）**

```bash
./start.sh
```

**方式 2: 使用 Make**

```bash
# 同时启动前后端
make dev

# 或分别启动:
make dev-backend  # 终端 1
make dev-frontend # 终端 2
```

**方式 3: 手动启动**

```bash
# 终端 1 - Backend
cd backend
go run cmd/server/main.go

# 终端 2 - Frontend
cd frontend
npm run dev
```

### Access the Application

浏览器访问:

**http://localhost:5180**

---

## Usage

### 创建第一个应用

1. **点击右下角的浮动输入框**
2. **输入提示词** - 例如:
   ```
   创建一个简单的 REST API 服务器，包含用户管理的 CRUD 接口
   ```
   或
   ```
   Create a simple REST API server with endpoints to manage users (CRUD operations)
   ```
3. **点击 "Generate & Deploy"** 或按 `Ctrl+Enter` / `Cmd+Enter`
4. **实时查看进度**:
   - ⏳ Pending (等待中)
   - 🔄 Processing (AI 生成代码中)
   - 🔨 Compiling (编译中)
   - 🚀 Deploying (部署中)
   - ✅ Completed (完成)

### Example Prompts

```
创建一个简单的 HTTP 服务器，返回 "Hello World"
```

```
构建一个 TODO 列表 API，包含以下接口:
- GET /todos - 列出所有待办事项
- POST /todos - 创建待办事项
- PUT /todos/:id - 更新待办事项
- DELETE /todos/:id - 删除待办事项
```

```
创建一个网页爬虫，从任意 URL 获取标题
```

```
构建一个 CLI 工具，生成随机密码
```

### 查看任务详情

- 所有任务显示在主面板
- 点击任务卡片查看完整详情
- 实时查看任务处理日志
- 生成的代码存储在 `backend/workspace/<task-id>/`

---

## API Reference

### REST Endpoints

#### Create Task
```http
POST /api/tasks
Content-Type: application/json

{
  "prompt": "Create a simple HTTP server"
}
```

**Response:**
```json
{
  "task": {
    "id": "abc123...",
    "prompt": "Create a simple HTTP server",
    "status": "pending",
    "logs": [],
    "created_at": "2025-01-08T10:30:00Z"
  },
  "message": "Task created successfully"
}
```

#### Get Task
```http
GET /api/tasks/:id
```

#### List Tasks
```http
GET /api/tasks
```

### WebSocket

连接到 `ws://localhost:8090/ws` 接收实时更新:

```javascript
const ws = new WebSocket('ws://localhost:8090/ws')

ws.onmessage = (event) => {
  const update = JSON.parse(event.data)
  console.log(update)
  // {
  //   task_id: "abc123...",
  //   status: "processing",
  //   message: "使用 CODEX 生成代码...",
  //   log: "Code generated successfully"
  // }
}
```

---

## Configuration

### Environment Variables

编辑 `backend/.env`:

```bash
# 必需
CODE_GENERATOR=codex                                   # 默认直连 Codex API，可改为 codex_cli 或 claude
CODEX_API_KEY=sk-ant-api03-...                         # Codex / OpenAI API key

# 推荐配置（指向 aicodemirror 代理）
CODEX_BASE_URL=https://api.aicodemirror.com/api/codex/backend-api/codex
CODEX_MODEL=gpt-5

# Codex CLI（允许后端驱动本地 codex exec）
CODEX_CLI_PATH=codex                                   # CLI 可执行文件路径
CODEX_CLI_ARGS="--skip-git-repo-check --full-auto"       # 额外 CLI 参数
CODEX_CLI_TIMEOUT=300s                                 # 单次 CLI 运行的超时时长

# 其它服务配置
PORT=8090                                              # 后端端口
FRONTEND_URL=http://localhost:5180                     # 前端 URL (用于 CORS)
WORKSPACE_DIR=./workspace                              # 生成代码存储路径
AUTO_RELOAD=true                                       # 代码变更时自动重载

# Legacy Claude（如需切回）
CLAUDE_API_KEY=sk-ant-...                              # 你的 Anthropic API key
CLAUDE_MODEL=claude-sonnet-4-5-20250929                # 使用的 Claude 模型
```

### Codex 代理环境准备（aicodemirror）

1. 安装 Codex 官方 CLI（任选其一）:
   ```bash
   npm install -g @openai/codex
   # or
   brew install codex
   ```
2. 清理并创建配置目录:
   ```bash
   rm -rf ~/.codex
   mkdir ~/.codex
   ```
3. 在代理仪表板创建 API Key，将值填入 `abs/backend/.env` 的 `CODEX_API_KEY`。
4. 新建 `~/.codex/auth.json`:
   ```json
   {
     "OPENAI_API_KEY": "你的API_KEY"
   }
   ```
5. 新建 `~/.codex/config.toml`（原样粘贴即可）:
   ```toml
   model_provider = "aicodemirror"
   model = "gpt-5"
   model_reasoning_effort = "medium"
   disable_response_storage = true
   preferred_auth_method = "apikey"

   [model_providers.aicodemirror]
   name = "aicodemirror"
   base_url = "https://api.aicodemirror.com/api/codex/backend-api/codex"
   wire_api = "responses"
   ```
6. 重启终端并执行 `codex -V` 确认 CLI 安装成功。

完成后，ABS 后端会直接通过 `CODEX_BASE_URL` 调用该代理，前端无需额外调整。

### 使用 Codex CLI 模式

如果希望由 Codex CLI 完成规划/执行，可在 `.env` 中设置：

```bash
CODE_GENERATOR=codex_cli
CODEX_CLI_PATH=codex                     # 或绝对路径 /usr/local/bin/codex
CODEX_CLI_ARGS="--skip-git-repo-check --full-auto"
CODEX_CLI_TIMEOUT=300s
```

同时请确保：

1. 宿主机已安装 Codex CLI (`npm install -g @openai/codex` 或 `brew install codex`) 并执行过一次 `codex login`/`codex -V` 以完成认证；
2. `.codex` 目录中的 `auth.json`/`config.toml` 配置了可用的 API key、代理等；
3. ABS 后端所在用户可以访问 CLI，可通过 `which codex` 验证；
4. 若 CLI 需要特定环境变量（如 `OPENAI_API_KEY`），与 `.env` 中的 `CODEX_API_KEY` 保持一致即可。

后端会为每个任务创建独立的临时工作区，调用 `codex exec --json` 执行任务，再将 CLI 产出的文件内容回传给现有流水线。CLI 若在任务过程中修改文件/输出命令，都会限制在该临时目录，不会污染仓库。

---

## Project Structure

```
abs/
├── backend/                   # Go 后端
│   ├── cmd/
│   │   └── server/
│   │       └── main.go       # 入口文件
│   ├── internal/
│   │   ├── api/              # HTTP 处理器 & 路由
│   │   │   ├── handler.go
│   │   │   └── router.go
│   │   ├── models/           # 数据模型
│   │   │   └── task.go
│   │   └── service/          # 业务逻辑
│   │       ├── config.go         # 配置管理
│   │       ├── code_generator.go # 生成器接口
│   │       ├── codex_client.go   # Codex API 客户端
│   │       ├── claude_client.go  # Claude API 客户端（保留）
│   │       ├── task_service.go   # 任务处理
│   │       └── websocket.go      # WebSocket 管理器
│   ├── workspace/            # 生成的代码存储
│   ├── .env.example          # 环境变量模板
│   ├── .env                  # 你的配置（已忽略）
│   └── go.mod
│
├── frontend/                  # Vue 3 前端
│   ├── src/
│   │   ├── api/              # API 客户端
│   │   │   ├── client.js     # HTTP 客户端
│   │   │   └── websocket.js  # WebSocket 客户端
│   │   ├── components/       # Vue 组件
│   │   │   └── FloatingInput.vue
│   │   ├── store/            # Pinia 状态管理
│   │   │   └── task.js
│   │   ├── assets/
│   │   ├── App.vue           # 主应用组件
│   │   └── main.js           # 入口文件
│   ├── package.json
│   └── vite.config.js
│
├── Makefile                   # 构建命令
├── start.sh                   # 启动脚本
└── README.md                  # 本文件
```

---

## How It Works

### Code Generation Flow

```
1. 用户通过浮动输入框提交提示词
         ↓
2. 前端发送 POST /api/tasks
         ↓
3. 后端创建任务并异步开始处理
         ↓
4. Task Service 调用 Codex API（可选切回 Claude）
         ↓
5. Codex 生成代码
         ↓
6. 代码写入 workspace/<task-id>/
         ↓
7. 如果检测到 Go 代码:
   - 运行 `go mod init`
   - 运行 `go mod tidy`
   - 运行 `go build`
         ↓
8. 启动编译后的应用
         ↓
9. 任务标记为完成
         ↓
10. 用户看到成功消息，可以访问运行的应用
```

### WebSocket Updates

整个过程中，通过 WebSocket 发送实时更新:

- 状态变更 (pending → processing → compiling → deploying → completed)
- 日志消息 (编译输出、部署信息)
- 错误消息 (如果失败)

---

## Troubleshooting

### 后端无法启动

**错误: `CODEX_API_KEY environment variable is required`**

解决方案: 默认使用 Codex，需要在 `backend/.env` 中填入 key:
```bash
CODE_GENERATOR=codex
CODEX_API_KEY=sk-code-your-key
```

**错误: `CLAUDE_API_KEY environment variable is required`**

解决方案: 仅当 `CODE_GENERATOR=claude` 时需要。将 Anthropic key 写入:
```bash
CLAUDE_API_KEY=sk-ant-your-key-here
```

**错误: `bind: address already in use`**

解决方案: 端口 8090 已被占用。可以:
1. 杀死占用端口的进程: `lsof -ti:8090 | xargs kill`
2. 在 `backend/.env` 中修改端口: `PORT=8091`

### 前端无法启动

**错误: `Cannot find module 'vue'`**

解决方案: 安装依赖:
```bash
cd frontend
npm install
```

### 代码生成失败

**错误: `API error (status 401)`**

解决方案: API key 无效。检查 `CODEX_API_KEY`（或使用 Claude 时的 `CLAUDE_API_KEY`）是否正确。

**错误: `compilation failed`**

解决方案: 生成的代码有语法错误。可能原因:
- 提示词太模糊
- 模型误解了需求

尝试:
1. 提示词更具体
2. 查看日志了解具体编译错误
3. 手动修复 `workspace/<task-id>/` 中的代码

### WebSocket 无法连接

**错误: `WebSocket disconnected`**

解决方案:
1. 确保后端在 8090 端口运行
2. 检查浏览器控制台的 CORS 错误
3. 验证 `backend/.env` 中的 `FRONTEND_URL` 与前端 URL 匹配

---

## Development

### 添加新语言支持

当前 ABS 自动编译 Go 代码。添加其他语言支持:

1. 编辑 `backend/internal/service/task_service.go`
2. 在 `compileCode()` 中添加检测逻辑:
   ```go
   if strings.Contains(task.Code, "package.json") {
       // Node.js 项目
       cmd := exec.Command("npm", "install")
       // ...
   }
   ```
3. 在 `deployCode()` 中添加部署逻辑

### 自定义 Claude 提示词

编辑 `backend/internal/service/claude_client.go` 中的系统提示词:

```go
systemPrompt := `You are an expert code generator...`
```

### 前端定制

- **颜色/样式**: 编辑 `frontend/src/components/FloatingInput.vue` 中的样式
- **布局**: 编辑 `frontend/src/App.vue`
- **API 调用**: 编辑 `frontend/src/api/client.js`

---

## Limitations

### Current Version (v0.1.0)

- ✅ 支持 Go 代码生成和编译
- ✅ 单文件和多文件项目
- ⚠️ 不支持需要外部服务的项目（数据库、API）
- ⚠️ 没有自动端口管理（生成的应用可能冲突）
- ⚠️ 没有进程管理（应用在后台运行，无监控）
- ⚠️ 新部署不会自动停止旧应用

### Planned Features

- [ ] 多语言支持 (Python, Node.js, Rust)
- [ ] 为生成的应用提供 Docker 容器化
- [ ] 运行应用的进程管理器
- [ ] 端口自动分配
- [ ] 与外部服务集成（PostgreSQL, Redis）
- [ ] 代码编辑界面
- [ ] 生成代码的版本历史
- [ ] 导出生成的项目为 zip

---

## Security Notes

⚠️ **警告**: 这是开发工具，存在安全隐患:

1. **生成的代码在你的机器上运行** - 不要使用不可信的提示词
2. **没有沙箱** - 生成的应用有完整系统访问权限
3. **API key 存储** - 保护好 `.env` 文件（已在 `.gitignore` 中）
4. **无身份验证** - 任何有访问权限的人都可以生成代码

**生产环境建议:**

- 添加用户身份验证
- 编译前实现代码审查
- 在容器中运行生成的代码（Docker）
- 添加资源限制（CPU、内存、磁盘）
- 实现任务创建的频率限制

---

## Make Commands

```bash
make help          # 显示所有可用命令
make init          # 初始化项目（安装依赖）
make dev           # 启动前后端开发模式
make dev-backend   # 仅启动后端
make dev-frontend  # 仅启动前端
make stop          # 停止所有服务
make clean         # 清理构建产物和工作空间
```

---

## Contributing

这是 `/study/abs` 下的内部研究项目。欢迎改进:

1. 用各种提示词测试系统
2. 报告 bug
3. 建议新功能
4. 提交改进的 PR

---

## License

ADDP 内部项目 - 查看主 ADDP 仓库的许可证详情。

---

## Support

问题和疑问:

1. 仔细阅读本 README
2. 查看后端日志: `backend/workspace/<task-id>/`
3. 检查浏览器控制台的前端错误
4. 向 ADDP 团队寻求帮助

---

## Acknowledgments

- **Anthropic** - Claude AI API
- **Gin** - Go HTTP 框架
- **Vue 3** - 前端框架
- **Vite** - 构建工具

---

**Happy AI Coding! 🤖✨**
