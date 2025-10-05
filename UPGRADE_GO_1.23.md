# 升级到 Go 1.23 指南

## 当前状态
- **系统 Go 版本**: 1.21.0
- **目标版本**: 1.23.x
- **所有模块已配置**: go 1.23

## 升级步骤

### 方法 1: 使用 Homebrew（推荐，macOS）

```bash
# 1. 更新 Homebrew
brew update

# 2. 升级 Go
brew upgrade go

# 3. 验证版本
go version
# 预期输出: go version go1.23.x darwin/amd64

# 4. 清理旧版本（可选）
brew cleanup go
```

### 方法 2: 从官网下载安装包

```bash
# 1. 访问 Go 官网下载页面
open https://go.dev/dl/

# 2. 下载 macOS (ARM64 或 AMD64) 安装包
# 例如: go1.23.5.darwin-amd64.pkg

# 3. 双击安装包，按提示安装

# 4. 验证安装
go version
```

### 方法 3: 使用 go install（不推荐）

```bash
# Go 1.21 可能无法直接安装 1.23
# 建议使用方法 1 或 2
```

## 升级后验证

```bash
# 1. 检查 Go 版本
go version

# 2. 进入项目目录
cd /Users/zengzhiming/code/addp

# 3. 测试编译各模块
cd system/backend && go build -o /tmp/system-test ./cmd/server/main.go
cd ../../meta/backend && go build -o /tmp/meta-test ./cmd/server/main.go
cd ../../manager/backend && go build -o /tmp/manager-test ./cmd/server/main.go
cd ../../transfer/backend && go build -o /tmp/transfer-test ./cmd/server/main.go
cd ../../gateway && go build -o /tmp/gateway-test ./main.go

# 4. 如果全部成功
echo "✅ 所有模块编译成功！"
```

## 升级后的依赖处理

```bash
# 在每个模块目录下运行（可选）
go mod tidy

# 更新所有依赖到最新兼容版本（谨慎）
go get -u ./...
go mod tidy
```

## 可能的问题

### 问题 1: Homebrew 安装了多个 Go 版本

```bash
# 查看已安装的版本
brew list --versions go

# 卸载旧版本
brew uninstall go@1.21

# 确保使用正确版本
which go
# 应该输出: /opt/homebrew/bin/go (Apple Silicon) 或 /usr/local/bin/go (Intel)
```

### 问题 2: PATH 环境变量问题

```bash
# 检查 PATH
echo $PATH

# 如果 Go 不在 PATH 中，添加到 ~/.zshrc 或 ~/.bash_profile
export PATH=$PATH:/usr/local/go/bin

# 重新加载配置
source ~/.zshrc  # 或 source ~/.bash_profile
```

### 问题 3: GOROOT 环境变量冲突

```bash
# 检查 GOROOT
echo $GOROOT

# 如果设置了旧路径，取消设置
unset GOROOT

# 或在 ~/.zshrc 中删除 GOROOT 相关配置
```

## 验证清单

- [ ] `go version` 显示 1.23.x
- [ ] `which go` 指向正确的 Go 安装路径
- [ ] 所有模块编译成功
- [ ] 运行时测试通过

## 回退方案（如果需要）

```bash
# 使用 Homebrew 回退到 1.21
brew uninstall go
brew install go@1.21
brew link go@1.21

# 或者重新安装 1.21 的安装包
# 从 https://go.dev/dl/ 下载对应版本
```

## 推荐：一键升级脚本

创建 `upgrade-go.sh`:

```bash
#!/bin/bash
set -e

echo "🚀 开始升级 Go 到 1.23..."

# 检查是否有 Homebrew
if command -v brew &> /dev/null; then
    echo "✅ 检测到 Homebrew"
    echo "📦 更新 Homebrew..."
    brew update

    echo "⬆️  升级 Go..."
    brew upgrade go || brew install go

    echo "🧹 清理旧版本..."
    brew cleanup go
else
    echo "❌ 未检测到 Homebrew"
    echo "请手动从 https://go.dev/dl/ 下载并安装 Go 1.23"
    exit 1
fi

echo ""
echo "✅ Go 升级完成！"
go version

echo ""
echo "🔍 测试编译..."
cd "$(dirname "$0")"

for module in system/backend meta/backend manager/backend transfer/backend gateway; do
    if [ -d "$module" ]; then
        echo "  编译 $module..."
        (cd "$module" && go build -o /tmp/test-build .) && echo "    ✅ 成功"
    fi
done

echo ""
echo "🎉 全部完成！所有模块已统一到 Go 1.23"
```

使用方法:
```bash
chmod +x upgrade-go.sh
./upgrade-go.sh
```

---

**完成升级后，请运行：**
```bash
git add -A
git commit -m "chore: 统一 Go 版本到 1.23"
git push
```
