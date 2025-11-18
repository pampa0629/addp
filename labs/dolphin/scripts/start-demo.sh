#!/bin/bash

# DolphinScheduler 演示环境启动脚本

set -e

echo "=========================================="
echo "DolphinScheduler 演示环境启动"
echo "=========================================="
echo ""

# 检查 Docker 是否运行
if ! docker info > /dev/null 2>&1; then
    echo "❌ 错误: Docker 未运行，请先启动 Docker"
    exit 1
fi

# 检查 docker-compose 是否可用
if ! command -v docker-compose &> /dev/null; then
    echo "❌ 错误: docker-compose 未安装"
    exit 1
fi

# 创建必要的目录
echo "📁 创建必要的目录..."
mkdir -p backend/spatial
mkdir -p backend/examples

# 安装 Python 依赖（本地测试用）
if [ ! -f "backend/requirements.txt" ]; then
    echo "📦 创建 requirements.txt..."
    echo "shapely==2.0.2" > backend/requirements.txt
fi

echo "🐋 启动 Docker Compose 服务..."
docker-compose -f docker-compose-demo.yml up -d

echo ""
echo "⏳ 等待服务启动（可能需要 1-2 分钟）..."
echo "   - PostgreSQL 初始化"
echo "   - ZooKeeper 启动"
echo "   - DolphinScheduler 启动"
echo ""

# 等待 DolphinScheduler 健康检查
MAX_ATTEMPTS=40
ATTEMPT=0
while [ $ATTEMPT -lt $MAX_ATTEMPTS ]; do
    if docker-compose -f docker-compose-demo.yml exec -T dolphinscheduler curl -f http://localhost:12345/dolphinscheduler/actuator/health > /dev/null 2>&1; then
        echo "✅ DolphinScheduler 已就绪！"
        break
    fi
    ATTEMPT=$((ATTEMPT + 1))
    echo "   等待中... ($ATTEMPT/$MAX_ATTEMPTS)"
    sleep 3
done

if [ $ATTEMPT -eq $MAX_ATTEMPTS ]; then
    echo "⚠️  服务启动超时，请检查日志："
    echo "   docker-compose -f docker-compose-demo.yml logs dolphinscheduler"
    exit 1
fi

# 在容器内安装 Python 依赖
echo ""
echo "📦 在容器内安装 Python 依赖..."
docker-compose -f docker-compose-demo.yml exec -T dolphinscheduler pip3 install shapely==2.0.2 || {
    echo "⚠️  警告: pip 安装失败，可能需要手动安装"
}

echo ""
echo "=========================================="
echo "✅ 演示环境启动完成！"
echo "=========================================="
echo ""
echo "🌐 访问地址:"
echo "   DolphinScheduler UI: http://localhost:12345/dolphinscheduler/ui"
echo ""
echo "🔑 默认登录信息:"
echo "   用户名: admin"
echo "   密码: dolphinscheduler123"
echo ""
echo "📚 下一步操作:"
echo "   1. 浏览器打开 UI 地址"
echo "   2. 使用默认账号登录"
echo "   3. 参考 DOLPHIN_INTEGRATION_GUIDE.md 创建空间分析工作流"
echo ""
echo "🛠️  常用命令:"
echo "   查看日志: docker-compose -f docker-compose-demo.yml logs -f"
echo "   停止服务: docker-compose -f docker-compose-demo.yml down"
echo "   重启服务: docker-compose -f docker-compose-demo.yml restart"
echo ""
