#!/bin/bash
# 用途：为单个模块初始化 Swagger 文档
# 使用：bash scripts/swagger/init-module-swagger.sh <module> <port> <description>
# 示例：bash scripts/swagger/init-module-swagger.sh manager 8081 "数据管理"

set -e

MODULE=$1
PORT=$2
DESC=$3

if [ -z "$MODULE" ] || [ -z "$PORT" ] || [ -z "$DESC" ]; then
    echo "用法: $0 <module> <port> <description>"
    echo "示例: $0 manager 8081 \"数据管理\""
    exit 1
fi

MODULE_DIR="${MODULE}/backend"
if [ ! -d "$MODULE_DIR" ]; then
    echo "错误: 模块目录 $MODULE_DIR 不存在"
    exit 1
fi

echo "=== 为 $MODULE 模块初始化 Swagger 文档 ==="

# 1. 添加 Go 依赖（固定版本与 System 模块保持一致）
echo "1. 添加 Go 依赖..."
cd "$MODULE_DIR"
go get github.com/swaggo/gin-swagger@v1.6.0
go get github.com/swaggo/files@v1.0.1
go get github.com/swaggo/swag@v1.16.4

# 2. 创建 swagger.go
echo "2. 创建 internal/models/swagger.go..."
mkdir -p internal/models
cat > internal/models/swagger.go << 'EOF'
package models

// ErrorResponse 错误响应
type ErrorResponse struct {
	Code    int    `json:"code" example:"400"`
	Message string `json:"message" example:"请求参数错误"`
}

// SuccessResponse 成功响应
type SuccessResponse struct {
	Code    int         `json:"code" example:"200"`
	Message string      `json:"message" example:"操作成功"`
	Data    interface{} `json:"data,omitempty"`
}
EOF

echo "✓ swagger.go 创建完成"
echo ""
echo "=== 后续步骤 ==="
echo "1. 在 cmd/server/main.go 的 main() 函数前添加 Swagger 注解"
echo "2. 在 internal/api/router.go 中注册 Swagger 路由"
echo "3. 为各 handler 添加 API 注解"
echo "4. 运行: cd $MODULE_DIR && swag init -g cmd/server/main.go -o docs --parseDependency --parseInternal"
echo ""
