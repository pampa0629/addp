#!/bin/bash
# 在本机搭建 Docker Registry 私有镜像仓库
# 用于局域网部署场景
#
# 使用方法:
#   ./scripts/setup-local-registry.sh           # 使用默认端口 5000
#   ./scripts/setup-local-registry.sh 5001      # 使用自定义端口 5001

set -e

REGISTRY_PORT="${1:-5000}"  # 支持通过参数指定端口，默认 5000
REGISTRY_DIR="$HOME/.addp-registry"

echo "=========================================="
echo "  ADDP 本地私有镜像仓库搭建脚本"
echo "=========================================="
echo ""
echo "配置信息："
echo "  - Registry 端口: $REGISTRY_PORT"
echo "  - 数据目录: $REGISTRY_DIR"
echo ""

# 检查端口是否被占用
echo "==> 检查端口 $REGISTRY_PORT 是否可用..."
if lsof -i :$REGISTRY_PORT > /dev/null 2>&1; then
  echo "❌ 错误: 端口 $REGISTRY_PORT 已被占用"
  echo ""
  echo "占用端口的进程:"
  lsof -i :$REGISTRY_PORT
  echo ""
  echo "解决方法："
  echo ""
  echo "方法一: 关闭占用端口的服务"
  echo "  - 如果是 macOS AirPlay Receiver (常见):"
  echo "    系统偏好设置 -> 共享 -> 关闭 'AirPlay 接收器'"
  echo ""
  echo "方法二: 使用其他端口运行 Registry"
  echo "  ./scripts/setup-local-registry.sh 5001"
  echo ""
  echo "方法三: 强制停止占用进程 (谨慎)"
  echo "  lsof -ti :$REGISTRY_PORT | xargs kill -9"
  echo ""
  exit 1
fi
echo "✅ 端口 $REGISTRY_PORT 可用"
echo ""

# 1. 创建数据目录
echo "==> 创建 Registry 数据目录..."
mkdir -p "$REGISTRY_DIR/data"
mkdir -p "$REGISTRY_DIR/config"

# 2. 创建 Registry 配置文件（启用删除功能）
echo "==> 创建 Registry 配置..."
cat > "$REGISTRY_DIR/config/config.yml" <<EOF
version: 0.1
log:
  level: info
  formatter: text
storage:
  filesystem:
    rootdirectory: /var/lib/registry
  delete:
    enabled: true
http:
  addr: :5000
  headers:
    X-Content-Type-Options: [nosniff]
    Access-Control-Allow-Origin: ['*']
    Access-Control-Allow-Methods: ['HEAD', 'GET', 'OPTIONS', 'DELETE']
    Access-Control-Allow-Headers: ['Authorization', 'Accept', 'Cache-Control']
    Access-Control-Max-Age: [1728000]
    Access-Control-Allow-Credentials: [true]
    Access-Control-Expose-Headers: ['Docker-Content-Digest']
EOF

# 3. 停止并移除旧的 Registry 容器（如果存在）
echo "==> 检查并清理旧的 Registry 容器..."
if docker ps -a | grep -q addp-registry; then
  docker stop addp-registry 2>/dev/null || true
  docker rm addp-registry 2>/dev/null || true
fi

# 4. 启动 Registry 容器
echo "==> 启动 Docker Registry 容器..."
docker run -d \
  --name addp-registry \
  --restart=always \
  -p $REGISTRY_PORT:5000 \
  -v "$REGISTRY_DIR/data:/var/lib/registry" \
  -v "$REGISTRY_DIR/config/config.yml:/etc/docker/registry/config.yml" \
  registry:2

# 5. 等待 Registry 启动
echo "==> 等待 Registry 启动..."
sleep 3

# 6. 获取本机局域网 IP
echo ""
echo "==> 检测本机局域网 IP 地址..."
if [[ "$OSTYPE" == "darwin"* ]]; then
  # macOS
  LOCAL_IP=$(ipconfig getifaddr en0 2>/dev/null || ipconfig getifaddr en1 2>/dev/null || echo "127.0.0.1")
else
  # Linux
  LOCAL_IP=$(hostname -I | awk '{print $1}')
fi

echo ""
echo "=========================================="
echo "  ✅ Registry 搭建成功！"
echo "=========================================="
echo ""
echo "Registry 信息："
echo "  - 容器名称: addp-registry"
echo "  - 端口: $REGISTRY_PORT"
echo "  - 数据目录: $REGISTRY_DIR/data"
echo "  - 本机访问: http://localhost:$REGISTRY_PORT"
echo "  - 局域网访问: http://$LOCAL_IP:$REGISTRY_PORT"
echo ""
echo "下一步操作："
echo "  1. 在服务器上配置信任此 Registry:"
echo "     sudo vim /etc/docker/daemon.json"
echo "     添加: {\"insecure-registries\": [\"$LOCAL_IP:$REGISTRY_PORT\"]}"
echo "     然后: sudo systemctl restart docker"
echo ""
echo "  2. 构建并推送镜像:"
echo "     ./scripts/push-to-local-registry.sh $LOCAL_IP:$REGISTRY_PORT"
echo ""
echo "  3. 在服务器上拉取镜像:"
echo "     REGISTRY=$LOCAL_IP:$REGISTRY_PORT ./scripts/deploy-from-registry.sh"
echo ""
echo "管理命令："
echo "  - 查看状态: docker logs addp-registry"
echo "  - 停止服务: docker stop addp-registry"
echo "  - 启动服务: docker start addp-registry"
echo "  - 查看镜像列表: curl http://localhost:$REGISTRY_PORT/v2/_catalog"
echo "=========================================="
