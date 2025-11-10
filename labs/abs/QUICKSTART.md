# ABS Quick Start Guide

## 一分钟快速开始

```bash
# 1. 进入 ABS 项目根目录
cd abs  # 或完整路径: cd /path/to/addp/labs/abs

# 2. 初始化依赖和配置模板
make init

# 3. 配置 Codex CLI（默认代码生成器）
mkdir -p ~/.codex
echo "your-codex-api-key-here" > ~/.codex/.apikey   # 写入真实 API Key
chmod 600 ~/.codex/.apikey                          # 确保只有当前用户可读

# 4. 验证 codex CLI 是否可用
which codex
codex --version

# 5. 启动 ABS（同时启动前后端）
./restart.sh

# 6. 打开浏览器访问 UI
open http://localhost:5180  # 或复制到浏览器地址栏
```

> 提示：`.env` 已将 `CODE_GENERATOR` 设为 `codex_cli`，并将 `CODEX_CLI_ARGS` 包裹在双引号中（千万不要去掉）。

## 测试示例

启动后，在浮动输入框中尝试以下提示词：

### 示例 1: Hello World 服务器
```
创建一个简单的 HTTP 服务器，监听 8888 端口，访问根路径返回 "Hello from ABS!"
```

### 示例 2: TODO API
```
Create a REST API with these endpoints:
- GET /todos - return a list of todos
- POST /todos - create a new todo
- GET /todos/:id - get a specific todo
- DELETE /todos/:id - delete a todo

Use in-memory storage. Listen on port 9999.
```

### 示例 3: 文件服务器
```
构建一个静态文件服务器，可以浏览当前目录的文件列表，监听 7777 端口
```

## 查看结果

- 实时进度显示在浮动框中
- 所有任务在主面板显示
- 生成的代码在 `workspace/<task-id>/` (相对于abs根目录)
- 编译后的程序会自动运行
- 应用访问地址: `http://localhost:5180?app=<task-id>`

## 故障排除

### 端口被占用
```bash
# 查看并杀死占用端口的进程
lsof -ti:8090 | xargs kill  # 后端
lsof -ti:5180 | xargs kill  # 前端
```

### Codex CLI 未安装或不可用
```bash
# 重新安装 CLI（任选其一）
npm install -g @openai/codex
# 或
brew install codex

# 将安装路径加入 PATH 后再次验证
which codex
```

### 找不到 Codex API Key 文件
```bash
ls -la ~/.codex/.apikey  # 应该存在且权限为 600
cat ~/.codex/.apikey     # 确认内容为有效 API Key
```

### 依赖未安装
```bash
# 从abs根目录执行
cd backend && go mod download
cd ../frontend && npm install
```

### CODEX_CLI_ARGS 解析错误
确保 `.env` 中保留双引号，例如：
```bash
CODEX_CLI_ARGS="--skip-git-repo-check --full-auto"
```

## 停止服务

按 `Ctrl+C` 或运行：
```bash
make stop
```

---

更多详情请查看 [README.md](README.md)
