#!/bin/bash
set -e

echo "🚀 开始升级 Go 到 1.23..."
echo ""

# 检查当前 Go 版本
CURRENT_VERSION=$(go version 2>/dev/null | awk '{print $3}' || echo "未安装")
echo "📌 当前 Go 版本: $CURRENT_VERSION"
echo ""

# 检查是否有 Homebrew
if command -v brew &> /dev/null; then
    echo "✅ 检测到 Homebrew"
    echo ""

    echo "📦 更新 Homebrew..."
    brew update
    echo ""

    echo "⬆️  升级 Go..."
    if brew list go &> /dev/null; then
        brew upgrade go
    else
        brew install go
    fi
    echo ""

    echo "🧹 清理旧版本..."
    brew cleanup go
    echo ""
else
    echo "❌ 未检测到 Homebrew"
    echo ""
    echo "请选择以下方式之一安装 Go 1.23:"
    echo "1. 安装 Homebrew: /bin/bash -c \"\$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)\""
    echo "2. 手动下载: https://go.dev/dl/"
    echo ""
    exit 1
fi

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "✅ Go 升级完成！"
echo ""
NEW_VERSION=$(go version | awk '{print $3}')
echo "📌 新版本: $NEW_VERSION"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

# 检查是否符合要求
if [[ "$NEW_VERSION" == *"go1.23"* ]]; then
    echo "✅ 版本符合要求 (go1.23.x)"
else
    echo "⚠️  警告: 版本不是 1.23.x，请检查安装"
    exit 1
fi

echo ""
echo "🔍 测试编译所有模块..."
echo ""

# 进入项目根目录
PROJECT_ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$PROJECT_ROOT"

# 测试编译各模块
MODULES=(
    "system/backend:./cmd/server/main.go"
    "manager/backend:./cmd/server/main.go"
    "meta/backend:./cmd/server/main.go"
    "transfer/backend:./cmd/server/main.go"
    "gateway:./main.go"
)

SUCCESS_COUNT=0
TOTAL_COUNT=${#MODULES[@]}

for module_info in "${MODULES[@]}"; do
    IFS=':' read -r module_path build_path <<< "$module_info"

    if [ -d "$module_path" ]; then
        echo "  📦 编译 $module_path..."
        if (cd "$module_path" && go build -o /tmp/test-build "$build_path" 2>&1 | grep -v "go: downloading"); then
            echo "     ✅ 成功"
            SUCCESS_COUNT=$((SUCCESS_COUNT + 1))
        else
            echo "     ❌ 失败"
        fi
    else
        echo "  ⏭️  跳过 $module_path (目录不存在)"
    fi
    echo ""
done

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
if [ $SUCCESS_COUNT -eq $TOTAL_COUNT ]; then
    echo "🎉 全部完成！所有 $TOTAL_COUNT 个模块编译成功"
    echo ""
    echo "✅ 项目已统一到 Go 1.23"
    echo ""
    echo "下一步:"
    echo "  git add -A"
    echo "  git commit -m \"chore: 统一 Go 版本到 1.23\""
    echo "  git push"
else
    echo "⚠️  完成 $SUCCESS_COUNT/$TOTAL_COUNT 个模块编译"
    echo ""
    echo "请检查失败的模块并解决问题"
fi
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
