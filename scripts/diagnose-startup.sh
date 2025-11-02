#!/bin/bash
# 诊断服务启动失败问题

echo "==========================================="
echo "  ADDP 服务启动问题诊断"
echo "==========================================="
echo ""

# 1. 查看失败容器的日志
echo "1. 查看 system-backend 日志:"
echo "==========================================="
docker logs addp-system-backend 2>&1 | tail -50
echo ""

# 2. 查看容器状态
echo "2. 容器详细状态:"
echo "==========================================="
docker inspect addp-system-backend --format='{{json .State}}' | jq '.'
echo ""

# 3. 查看健康检查
echo "3. 健康检查配置:"
echo "==========================================="
docker inspect addp-system-backend --format='{{json .State.Health}}' | jq '.'
echo ""

# 4. 查看容器配置
echo "4. 环境变量:"
echo "==========================================="
docker inspect addp-system-backend --format='{{range .Config.Env}}{{println .}}{{end}}' | grep -v PASSWORD | grep -v SECRET
echo ""

# 5. 查看网络配置
echo "5. 网络连接:"
echo "==========================================="
docker inspect addp-system-backend --format='{{json .NetworkSettings.Networks}}' | jq '.'
echo ""

# 6. 查看依赖服务状态
echo "6. 依赖服务状态:"
echo "==========================================="
echo "PostgreSQL:"
docker ps --filter name=addp-postgres --format "{{.Names}}: {{.Status}}"
docker logs addp-postgres 2>&1 | tail -20

echo ""
echo "Redis:"
docker ps --filter name=addp-redis --format "{{.Names}}: {{.Status}}"

echo ""
echo "==========================================="
echo "  常见问题排查"
echo "==========================================="
echo ""

# 检查数据库连接
echo "7. 测试数据库连接:"
if docker exec addp-postgres pg_isready -U addp > /dev/null 2>&1; then
  echo "  ✅ PostgreSQL 可访问"
else
  echo "  ❌ PostgreSQL 不可访问"
fi

# 检查 Redis 连接
echo ""
echo "8. 测试 Redis 连接:"
if docker exec addp-redis redis-cli ping > /dev/null 2>&1; then
  echo "  ✅ Redis 可访问"
else
  echo "  ❌ Redis 不可访问"
fi

echo ""
echo "==========================================="
echo "  建议操作"
echo "==========================================="
echo ""
echo "查看完整日志:"
echo "  docker logs addp-system-backend"
echo ""
echo "进入容器调试:"
echo "  docker exec -it addp-system-backend sh"
echo ""
echo "重启容器:"
echo "  docker-compose -f docker-compose.prod.yml restart system-backend"
echo ""
echo "查看所有服务日志:"
echo "  docker-compose -f docker-compose.prod.yml logs"
echo ""
