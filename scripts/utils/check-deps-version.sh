#!/usr/bin/env bash

set -e

echo "🔍 检查ADDP所有模块的依赖版本一致性..."
echo ""

# 定义需要检查的关键依赖及其期望版本（使用函数而非关联数组）
get_expected_version() {
    case "$1" in
        "github.com/gin-gonic/gin") echo "v1.11.0" ;;
        "gorm.io/gorm") echo "v1.31.1" ;;
        "github.com/golang-jwt/jwt/v5") echo "v5.3.0" ;;
        "github.com/redis/go-redis/v9") echo "v9.17.2" ;;
        "github.com/minio/minio-go/v7") echo "v7.0.95" ;;
        "github.com/go-sql-driver/mysql") echo "v1.9.3" ;;
        "github.com/hibiken/asynq") echo "v0.25.1" ;;
        "github.com/jackc/pgx/v5") echo "v5.7.2" ;;
        "github.com/twpayne/go-geom") echo "v1.6.1" ;;
        *) echo "" ;;
    esac
}

# 依赖列表
DEPENDENCIES=(
    "github.com/gin-gonic/gin"
    "gorm.io/gorm"
    "github.com/golang-jwt/jwt/v5"
    "github.com/redis/go-redis/v9"
    "github.com/minio/minio-go/v7"
    "github.com/go-sql-driver/mysql"
    "github.com/hibiken/asynq"
    "github.com/jackc/pgx/v5"
    "github.com/twpayne/go-geom"
)

# 模块列表
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

INCONSISTENT=0

# 检查每个模块
for module in "${MODULES[@]}"; do
    if [ ! -f "$module/go.mod" ]; then
        echo "⚠️  $module: go.mod not found"
        continue
    fi

    echo "📦 检查模块: $module"

    # 检查每个关键依赖
    for dep in "${DEPENDENCIES[@]}"; do
        expected=$(get_expected_version "$dep")
        actual=$(grep "$dep" "$module/go.mod" | grep -v "// indirect" | awk '{print $2}' | head -1)

        if [ -n "$actual" ]; then
            if [ "$actual" != "$expected" ]; then
                echo "  ❌ $dep: 期望 $expected, 实际 $actual"
                INCONSISTENT=1
            else
                echo "  ✅ $dep: $actual"
            fi
        fi
    done
    echo ""
done

if [ $INCONSISTENT -eq 0 ]; then
    echo "✨ 所有模块依赖版本一致！"
    exit 0
else
    echo "⚠️  发现版本不一致，请运行 bash scripts/dev/upgrade-deps.sh 统一版本"
    exit 1
fi
