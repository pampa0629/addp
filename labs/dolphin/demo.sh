#!/bin/bash

# 演示环境快捷操作脚本

set -e

COMPOSE_FILE="docker-compose-demo.yml"

case "$1" in
    start)
        echo "🚀 启动演示环境..."
        bash scripts/start-demo.sh
        ;;
    test)
        echo "🧪 测试空间算子工作流..."
        docker-compose -f $COMPOSE_FILE exec dolphinscheduler bash /scripts/test-in-container.sh
        ;;
    logs)
        echo "📋 查看日志..."
        docker-compose -f $COMPOSE_FILE logs -f
        ;;
    shell)
        echo "🐚 进入容器..."
        docker-compose -f $COMPOSE_FILE exec dolphinscheduler bash
        ;;
    stop)
        echo "⏹️  停止演示环境..."
        docker-compose -f $COMPOSE_FILE down
        echo "✅ 已停止"
        ;;
    restart)
        echo "🔄 重启演示环境..."
        docker-compose -f $COMPOSE_FILE restart
        echo "✅ 已重启"
        ;;
    clean)
        echo "🗑️  清理演示环境（删除所有数据）..."
        echo "警告: 这将删除所有容器和数据！"
        echo "按 Ctrl+C 取消，或按 Enter 继续..."
        read
        docker-compose -f $COMPOSE_FILE down -v
        echo "✅ 清理完成"
        ;;
    status)
        echo "📊 服务状态..."
        docker-compose -f $COMPOSE_FILE ps
        ;;
    *)
        echo "演示环境管理脚本"
        echo ""
        echo "用法: $0 {start|test|logs|shell|stop|restart|clean|status}"
        echo ""
        echo "命令:"
        echo "  start   - 启动演示环境"
        echo "  test    - 测试空间算子工作流"
        echo "  logs    - 查看日志"
        echo "  shell   - 进入容器"
        echo "  stop    - 停止服务"
        echo "  restart - 重启服务"
        echo "  clean   - 清理所有数据（危险）"
        echo "  status  - 查看服务状态"
        echo ""
        echo "快速开始: $0 start"
        exit 1
        ;;
esac
