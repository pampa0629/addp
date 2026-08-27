#!/bin/bash
# 用途：批量初始化多个模块的 Swagger 文档
# 使用：bash scripts/swagger/batch-init-swagger.sh <p0|p1|p2|all>

set -e

BATCH=$1

if [ -z "$BATCH" ]; then
    echo "用法: $0 <p0|p1|p2|all>"
    echo "  p0  - 初始化 P0 模块 (Manager, Meta)"
    echo "  p1  - 初始化 P1 模块 (Develop, Service, Orchestrator)"
    echo "  p2  - 初始化 P2 模块 (Monitor, Standard, Model, Transfer)"
    echo "  all - 初始化所有模块"
    exit 1
fi

init_module() {
    local module=$1
    local port=$2
    local desc=$3
    echo "=== 初始化 $module 模块 ==="
    bash scripts/swagger/init-module-swagger.sh "$module" "$port" "$desc"
    echo ""
}

init_p0() {
    init_module "manager" "8081" "数据管理"
    init_module "meta" "8082" "元数据"
}

init_p1() {
    init_module "orchestrator" "8084" "工作流编排"
    init_module "develop" "8185" "数据开发"
    init_module "service" "8086" "数据服务"
}

init_p2() {
    init_module "monitor" "8100" "执行监控"
    init_module "standard" "8110" "数据标准"
    init_module "model" "8181" "数据建模"
    init_module "transfer" "8083" "数据传输"
	init_module "catalog" "8192" "企业数据目录"
	init_module "workbench" "8193" "服务消费工作台"
}

case $BATCH in
    p0)
        echo "初始化 P0 模块..."
        init_p0
        ;;
    p1)
        echo "初始化 P1 模块..."
        init_p1
        ;;
    p2)
        echo "初始化 P2 模块..."
        init_p2
        ;;
    all)
        echo "初始化所有模块..."
        init_p0
        init_p1
        init_p2
        ;;
    *)
        echo "错误: 无效的批次 '$BATCH'"
        exit 1
        ;;
esac

echo "✓ 批量初始化完成"
