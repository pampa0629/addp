#!/bin/bash
# 自动生成服务器 .env 配置文件

set -e

REGISTRY="${1:-}"

if [ -z "$REGISTRY" ]; then
  echo "使用方法: $0 <registry-address>"
  echo "示例: $0 192.168.31.238:5001"
  exit 1
fi

ENV_FILE=".env"
EXAMPLE_FILE=".env.example"

echo "==========================================="
echo "  生成服务器 .env 配置"
echo "==========================================="
echo ""

# 检查 .env.example 是否存在
if [ ! -f "$EXAMPLE_FILE" ]; then
  echo "❌ 错误: $EXAMPLE_FILE 不存在"
  exit 1
fi

# 检查是否已有 .env
if [ -f "$ENV_FILE" ]; then
  echo "⚠️  $ENV_FILE 已存在"
  read -p "是否覆盖? (y/N): " -n 1 -r
  echo
  if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo "已取消"
    exit 0
  fi
  # 备份
  cp "$ENV_FILE" "${ENV_FILE}.backup.$(date +%Y%m%d_%H%M%S)"
  echo "✅ 已备份原文件"
fi

echo "==> 从 $EXAMPLE_FILE 创建 $ENV_FILE..."
cp "$EXAMPLE_FILE" "$ENV_FILE"

echo "==> 生成安全密钥..."
JWT_SECRET=$(openssl rand -base64 32)
ENCRYPTION_KEY=$(openssl rand -base64 32)
POSTGRES_PASSWORD=$(openssl rand -base64 16 | tr -d '/+=' | cut -c1-16)
REDIS_PASSWORD=$(openssl rand -base64 16 | tr -d '/+=' | cut -c1-16)
MINIO_PASSWORD=$(openssl rand -base64 16 | tr -d '/+=' | cut -c1-16)
SUPER_ADMIN_PASSWORD=$(openssl rand -base64 16 | tr -d '/+=' | cut -c1-20)

echo "==> 配置环境变量..."

# 设置 Registry
sed -i.tmp "s|REGISTRY=.*|REGISTRY=$REGISTRY|g" "$ENV_FILE"

# 设置安全密钥
sed -i.tmp "s|JWT_SECRET=.*|JWT_SECRET=$JWT_SECRET|g" "$ENV_FILE"
sed -i.tmp "s|ENCRYPTION_KEY=.*|ENCRYPTION_KEY=$ENCRYPTION_KEY|g" "$ENV_FILE"

# 设置密码
sed -i.tmp "s|POSTGRES_PASSWORD=.*|POSTGRES_PASSWORD=$POSTGRES_PASSWORD|g" "$ENV_FILE"
sed -i.tmp "s|REDIS_PASSWORD=.*|REDIS_PASSWORD=$REDIS_PASSWORD|g" "$ENV_FILE"
sed -i.tmp "s|MINIO_ROOT_PASSWORD=.*|MINIO_ROOT_PASSWORD=$MINIO_PASSWORD|g" "$ENV_FILE"

# 设置超级管理员密码
sed -i.tmp "s|SUPER_ADMIN_PASSWORD=.*|SUPER_ADMIN_PASSWORD=$SUPER_ADMIN_PASSWORD|g" "$ENV_FILE"

# 清理临时文件
rm -f "${ENV_FILE}.tmp"

echo "✅ 配置完成"
echo ""

echo "==========================================="
echo "  生成的配置"
echo "==========================================="
echo ""
echo "Registry: $REGISTRY"
echo "JWT_SECRET: $JWT_SECRET"
echo "ENCRYPTION_KEY: $ENCRYPTION_KEY"
echo "POSTGRES_PASSWORD: $POSTGRES_PASSWORD"
echo "REDIS_PASSWORD: $REDIS_PASSWORD"
echo "MINIO_PASSWORD: $MINIO_PASSWORD"
echo ""
echo "超级管理员账号:"
echo "  用户名: SuperAdmin"
echo "  密码: $SUPER_ADMIN_PASSWORD"
echo "  邮箱: superadmin@addp.com"
echo ""

echo "⚠️  重要: 请保存这些密钥，特别是 JWT_SECRET 和超级管理员密码"
echo ""

# 保存密钥到安全文件
SECRETS_FILE=".env.secrets.$(date +%Y%m%d_%H%M%S).txt"
cat > "$SECRETS_FILE" <<EOF
ADDP 服务器密钥配置
生成时间: $(date)
Registry: $REGISTRY

JWT_SECRET=$JWT_SECRET
ENCRYPTION_KEY=$ENCRYPTION_KEY
POSTGRES_PASSWORD=$POSTGRES_PASSWORD
REDIS_PASSWORD=$REDIS_PASSWORD
MINIO_PASSWORD=$MINIO_PASSWORD

超级管理员账号:
  用户名: SuperAdmin
  密码: $SUPER_ADMIN_PASSWORD
  邮箱: superadmin@addp.com

请妥善保管此文件！
EOF

echo "✅ 密钥已保存到: $SECRETS_FILE"
echo ""

echo "下一步:"
echo "  1. 查看配置: cat $ENV_FILE"
echo "  2. 运行部署: REGISTRY=$REGISTRY ./scripts/clean-deploy.sh"
echo ""
