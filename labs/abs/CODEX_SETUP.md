# Codex CLI 配置指南

## 当前配置状态

✅ **系统已配置为使用 Codex CLI 模式**

- Claude 相关代码已保留但暂停使用
- 默认代码生成器：`codex_cli`
- 配置文件：`.env`

## 快速启动步骤

### 1. 配置 Codex API Key

**重要**：Codex CLI 从 `~/.codex/.apikey` 文件读取 API Key

```bash
# 创建 .codex 目录（如果不存在）
mkdir -p ~/.codex

# 编辑 API Key 文件
vim ~/.codex/.apikey

# 或直接写入（请替换为您的实际 API Key）
echo "your-codex-api-key-here" > ~/.codex/.apikey

# 设置正确的权限
chmod 600 ~/.codex/.apikey
```

**验证 API Key 文件**：
```bash
# 检查文件是否存在
ls -la ~/.codex/.apikey

# 查看文件内容（确保没有多余空格或换行）
cat ~/.codex/.apikey
```

### 2. 验证 Codex CLI 可用

```bash
# 检查 codex 命令是否可用
which codex

# 测试 codex 是否工作正常
codex --version
```

### 3. 启动 ABS 系统

```bash
cd abs
./restart.sh
```

系统启动后，脚本会自动检测并显示：
```
✅ 使用 Codex CLI 模式（codex）
```

## 配置选项说明

### 当前 .env 配置（.env）

```bash
# 代码生成器选择
CODE_GENERATOR=codex_cli

# Codex CLI 配置
# API Key 存放位置：~/.codex/.apikey（不在 .env 中配置）
CODEX_CLI_PATH=codex                              # codex 命令路径
CODEX_CLI_ARGS="--skip-git-repo-check --full-auto"  # 命令行参数（必须加双引号）
CODEX_CLI_TIMEOUT=300s                            # 超时时间（5分钟）

# 服务器配置
PORT=8090
FRONTEND_URL=http://localhost:5180

# 工作空间配置（相对于 abs/ 根目录）
WORKSPACE_DIR=workspace
AUTO_RELOAD=true
APPS_DATA_FILE=workspace/apps.json
```

**注意事项**：
1. ✅ `CODEX_CLI_ARGS` 必须使用双引号包裹，否则会导致参数解析错误
2. ✅ Codex API Key 不在 `.env` 中配置，而是放在 `~/.codex/.apikey`

## 切换代码生成器

### 切换到 Codex API 模式

编辑 `.env`：

```bash
CODE_GENERATOR=codex
CODEX_API_KEY=your-codex-api-key-here
CODEX_BASE_URL=https://api.aicodemirror.com/api/codex/backend-api/codex
CODEX_MODEL=gpt-5
```

### 切换到 Claude 模式（如需启用）

编辑 `.env`：

```bash
CODE_GENERATOR=claude
ANTHROPIC_API_KEY=your-anthropic-api-key-here
ANTHROPIC_BASE_URL=https://api.anthropic.com
CLAUDE_MODEL=claude-sonnet-4-5-20250929
```

## 故障排查

### 问题：找不到 codex 命令

**解决方案**：
1. 检查 Codex CLI 是否已安装
2. 确认 `codex` 命令在 PATH 中
3. 如果安装在非标准位置，修改 `CODEX_CLI_PATH`：
   ```bash
   CODEX_CLI_PATH=/usr/local/bin/codex
   ```

### 问题：Codex API Key 未配置

**解决方案**：
1. 确认 `~/.codex/.apikey` 文件存在
2. 文件内容为有效的 API Key（不包含空格或换行）
3. 检查文件权限：
   ```bash
   chmod 600 ~/.codex/.apikey
   ```

### 问题：任务执行超时

**解决方案**：
增加超时时间，编辑 `.env`：
```bash
CODEX_CLI_TIMEOUT=600s  # 增加到 10 分钟
```

## 日志查看

启动后，可以通过以下方式查看日志：

```bash
# 查看启动日志
tail -f /tmp/abs-backend.log

# 查看前端日志（在 restart.sh 启动的终端）
# 前端日志会直接显示在终端
```

## 验证配置

启动成功后访问：

- **前端**：http://localhost:5180
- **后端 API**：http://localhost:8090
- **健康检查**：http://localhost:8090/health

在前端输入框中输入测试提示词，例如：
```
写一个 Python 程序打印 Hello World
```

如果一切正常，系统会：
1. 创建任务
2. 调用 Codex CLI 生成代码
3. 自动编译和部署
4. 在应用中心显示结果

## 相关文件

- 配置模板：`.env.example`
- 当前配置：`.env`
- 启动脚本：`restart.sh`
- 代码生成器选择逻辑：`backend/internal/service/task_service.go`
- 配置加载逻辑：`backend/internal/service/config.go`
