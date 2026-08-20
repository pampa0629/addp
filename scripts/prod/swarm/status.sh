#!/bin/bash
# Docker Swarm 服务状态查看脚本

set -e

echo "=================================================="
echo "  ADDP Docker Swarm 服务状态"
echo "=================================================="
echo ""

# 检查 Swarm 是否激活
if ! docker info 2>/dev/null | grep -q "Swarm: active"; then
    echo "❌ Swarm 未激活"
    exit 1
fi

# 节点信息
echo "📍 节点信息:"
docker node ls
echo ""

# 服务列表
echo "📋 服务列表:"
docker service ls
echo ""

# Transfer Worker 详情（高可用关键服务）
if docker service ls --format '{{.Name}}' | grep -q "addp_transfer-bounded-worker"; then
    echo "🔧 Transfer Worker 详情 (高可用):"
    docker service ps addp_transfer-bounded-worker --no-trunc
    echo ""

    # 获取副本数
    REPLICAS=$(docker service ls --format '{{.Replicas}}' --filter name=addp_transfer-bounded-worker)
    echo "副本状态: $REPLICAS"
    echo ""
fi

# Stack 信息
echo "📦 Stack 列表:"
docker stack ls
echo ""

# 网络信息
echo "🌐 网络列表:"
docker network ls --filter driver=overlay
echo ""

# 最近的任务事件
echo "📊 最近的服务事件 (最近 10 条):"
docker service ps --filter "desired-state=running" $(docker service ls -q) --format "table {{.Name}}\t{{.Image}}\t{{.Node}}\t{{.DesiredState}}\t{{.CurrentState}}" | head -11
echo ""

# 资源使用
echo "💻 节点资源使用:"
docker node ls -q | while read node; do
    echo "Node: $(docker node inspect $node --format '{{.Description.Hostname}}')"
    docker node inspect $node --format '  CPU: {{.Description.Resources.NanoCPUs}} cores'
    docker node inspect $node --format '  Memory: {{.Description.Resources.MemoryBytes}} bytes'
done
echo ""

# 提示命令
echo "=================================================="
echo "  常用命令"
echo "=================================================="
echo ""
echo "查看服务日志:"
echo "  docker service logs -f addp_transfer-bounded-worker"
echo ""
echo "查看特定服务的所有副本:"
echo "  docker service ps addp_transfer-bounded-worker"
echo ""
echo "扩展服务:"
echo "  docker service scale addp_transfer-bounded-worker=3"
echo ""
echo "更新服务镜像:"
echo "  docker service update --image addp-transfer-bounded-worker:v2.0 addp_transfer-bounded-worker"
echo ""
echo "回滚服务:"
echo "  docker service rollback addp_transfer-bounded-worker"
echo ""
echo "实时监控 (需要 watch 命令):"
echo "  watch -n 2 'docker service ls'"
echo ""
