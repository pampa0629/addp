#!/bin/bash
# 一键部署脚本 - 从开发机传输文件到服务器并部署
# 使用方法: ./scripts/deploy-to-server.sh pampa@192.168.1.182

set -e

if [ -z "$1" ]; then
  echo "使用方法: $0 user@server-ip [remote-dir]"
  echo "示例: $0 pampa@192.168.1.182"
  echo "       $0 pampa@192.168.1.182 /opt/addp"
  echo ""
  echo "默认部署到用户目录: ~/addp"
  exit 1
fi

SERVER="$1"
REMOTE_DIR="${2:-~/addp}"  # 默认使用用户目录，可通过第二个参数自定义

echo "==========================================="
echo "  ADDP 一键部署到服务器"
echo "==========================================="
echo ""
echo "目标服务器: $SERVER"
echo "远程目录: $REMOTE_DIR"
echo ""

# 测试 SSH 连接
echo "==> 测试 SSH 连接..."
if ! ssh -o ConnectTimeout=5 "$SERVER" "echo '连接成功'" > /dev/null 2>&1; then
  echo "❌ 错误: 无法 SSH 连接到 $SERVER"
  echo ""
  echo "请确保:"
  echo "  1. 服务器 SSH 已启用（macOS: System Settings → Sharing → Remote Login）"
  echo "  2. 可以免密登录（ssh-copy-id $SERVER）"
  exit 1
fi
echo "✅ SSH 连接正常"
echo ""

# 确认继续
read -p "是否继续部署？(y/N) " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
  echo "已取消"
  exit 0
fi

# 传输文件
echo "==> 传输文件到服务器..."
echo "排除: node_modules, bin, .git, logs"

rsync -avz --progress \
  --exclude 'node_modules' \
  --exclude 'bin' \
  --exclude '.git' \
  --exclude 'logs' \
  --exclude '*.log' \
  --exclude '.DS_Store' \
  --exclude '.dev-pids' \
  ./ "$SERVER:$REMOTE_DIR/"

echo "✅ 文件传输完成"
echo ""

# 在服务器上部署
echo "==> 在服务器上执行部署..."
ssh "$SERVER" bash << ENDSSH
set -e

# 创建目录（如果不存在）
mkdir -p $REMOTE_DIR
cd $REMOTE_DIR

DEPLOY_DIR=\$(pwd)
echo "部署目录: \$DEPLOY_DIR"
echo ""

echo "检查 Docker..."
if ! docker --version > /dev/null 2>&1; then
  echo "❌ 错误: Docker 未安装"
  exit 1
fi

echo "✅ Docker 已安装"
echo ""

# 检查并部署 Business 基础设施
echo "==========================================="
echo "  部署 Business 基础设施"
echo "==========================================="
echo ""

cd business

if [ ! -f ".env" ]; then
  echo "创建 Business .env 文件..."
  cp .env.prod.example .env
  echo "⚠️  请稍后配置 business/.env 中的密码"
fi

echo "启动 Business 基础设施..."
docker-compose -f docker-compose.prod.yml up -d

echo "等待 Business 基础设施就绪..."
sleep 10

docker-compose -f docker-compose.prod.yml ps

echo ""
echo "==========================================="
echo "  构建并部署 ADDP 系统"
echo "==========================================="
echo ""

cd \$DEPLOY_DIR

if [ ! -f ".env" ]; then
  echo "创建 ADDP .env 文件..."
  cp .env.example .env
  echo "⚠️  请稍后配置 .env 中的密钥和密码"
fi

chmod +x scripts/build-and-deploy-local.sh

echo "开始构建和部署..."
./scripts/build-and-deploy-local.sh

ENDSSH

echo ""
echo "==========================================="
echo "  ✅ 部署完成！"
echo "==========================================="
# 获取实际部署目录
ACTUAL_DIR=$(ssh "$SERVER" "cd $REMOTE_DIR && pwd")

echo ""
echo "下一步："
echo "  1. SSH 登录服务器: ssh $SERVER"
echo "  2. 配置 Business 环境变量: vim $ACTUAL_DIR/business/.env"
echo "  3. 配置 ADDP 环境变量: vim $ACTUAL_DIR/.env"
echo "  4. 重启服务:"
echo "     cd $ACTUAL_DIR/business && docker-compose -f docker-compose.prod.yml restart"
echo "     cd $ACTUAL_DIR && docker-compose -f docker-compose.prod.yml restart"
echo ""
echo "访问服务:"
echo "  - Portal: http://$SERVER:5170"
echo "  - Gateway: http://$SERVER:8000"
echo ""
