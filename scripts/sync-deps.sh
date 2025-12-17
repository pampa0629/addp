#!/bin/bash
set -e

echo "🔄 统一所有模块的依赖版本..."

# 定义统一的依赖版本
MYSQL_DRIVER_VERSION="v1.9.3"
GORM_VERSION="latest"  # 使用最新稳定版

MODULES=(
    "common"
    "system/backend"
    "manager/backend"
    "meta/backend"
    "transfer/backend"
    "orchestrator/backend"
    "develop/backend"
    "gateway"
)

for module in "${MODULES[@]}"; do
    echo ""
    echo "📦 处理模块: $module"
    cd "$module"
    
    # 检查是否使用了 MySQL driver
    if grep -q "go-sql-driver/mysql" go.mod 2>/dev/null || \
       grep -q "gorm.io/driver/mysql" go.mod 2>/dev/null; then
        echo "  升级 MySQL driver 到 $MYSQL_DRIVER_VERSION"
        go get github.com/go-sql-driver/mysql@$MYSQL_DRIVER_VERSION
    fi
    
    # 检查是否使用了 GORM
    if grep -q "gorm.io/gorm" go.mod 2>/dev/null; then
        echo "  升级 GORM 到 $GORM_VERSION"
        go get gorm.io/gorm@$GORM_VERSION
    fi
    
    # 清理并整理依赖
    go mod tidy
    
    cd - > /dev/null
done

echo ""
echo "✅ 所有模块依赖版本已统一！"
