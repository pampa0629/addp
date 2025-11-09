# Codex CLI 配置要点

## ⚠️ 两个关键问题已修正

### 1. API Key 存放位置

**❌ 错误**：在 `.env` 文件中配置 `CODEX_API_KEY`

**✅ 正确**：在 `~/.codex/.apikey` 文件中存放

```bash
# 创建并配置 API Key
mkdir -p ~/.codex
echo "your-actual-api-key-here" > ~/.codex/.apikey
chmod 600 ~/.codex/.apikey
```

**原因**：Codex CLI 默认从 `~/.codex/.apikey` 读取 API Key

---

### 2. CODEX_CLI_ARGS 必须加双引号

**❌ 错误**：
```bash
CODEX_CLI_ARGS=--skip-git-repo-check --full-auto
```

**✅ 正确**：
```bash
CODEX_CLI_ARGS="--skip-git-repo-check --full-auto"
```

**原因**：不加引号会导致 shell 参数解析错误

---

## 完整配置示例（backend/.env）

```bash
CODE_GENERATOR=codex_cli
CODEX_CLI_PATH=codex
CODEX_CLI_ARGS="--skip-git-repo-check --full-auto"  # ⚠️ 必须加双引号
CODEX_CLI_TIMEOUT=300s

PORT=8090
FRONTEND_URL=http://localhost:5180

WORKSPACE_DIR=./workspace
AUTO_RELOAD=true
APPS_DATA_FILE=./workspace/apps.json
```

---

## 快速验证清单

```bash
# 1. 检查 API Key 文件是否存在
ls -la ~/.codex/.apikey

# 2. 检查 codex 命令是否可用
which codex

# 3. 验证 .env 配置
cd /Users/pampa/code/addp/study/abs/backend
grep CODEX_CLI_ARGS .env
# 输出应该包含双引号：CODEX_CLI_ARGS="--skip-git-repo-check --full-auto"

# 4. 测试启动
cd /Users/pampa/code/addp/study/abs
./restart.sh
```

预期输出：
```
✅ 使用 Codex CLI 模式（codex）
```

---

## 常见错误

### 错误 1：参数解析失败
```
Error: unknown flag: --full
```
**原因**：`CODEX_CLI_ARGS` 没有加双引号
**解决**：修改为 `CODEX_CLI_ARGS="--skip-git-repo-check --full-auto"`

### 错误 2：API Key 未找到
```
Error: API key not found
```
**原因**：`~/.codex/.apikey` 文件不存在或为空
**解决**：创建文件并写入有效的 API Key

### 错误 3：codex 命令未找到
```
⚠️  警告: 未找到 codex 命令
```
**原因**：Codex CLI 未安装或不在 PATH 中
**解决**：安装 Codex CLI 或在 `.env` 中设置 `CODEX_CLI_PATH` 为完整路径

---

## 记住这两点

1. **API Key** → `~/.codex/.apikey` 文件
2. **CODEX_CLI_ARGS** → 必须用双引号包裹

这样配置就不会出错了！🎉
