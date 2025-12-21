#!/bin/bash
# Doris 远程访问配置脚本
# 为 ADDP 平台配置远程访问权限

set -e

echo "=== Doris 远程访问配置 ==="

# 等待 Doris FE 就绪
echo "等待 Doris 就绪..."
MAX_RETRIES=30
RETRY_COUNT=0

until docker exec business-doris-fe mysql -h127.0.0.1 -P9030 -uroot --connect-timeout=5 -e "SHOW DATABASES;" >/dev/null 2>&1; do
  RETRY_COUNT=$((RETRY_COUNT + 1))
  if [ $RETRY_COUNT -ge $MAX_RETRIES ]; then
    echo "❌ Doris 启动超时"
    exit 1
  fi
  echo "等待中 ($RETRY_COUNT/$MAX_RETRIES)..."
  sleep 3
done

echo "✅ Doris 已就绪"

# 配置 root 用户远程访问（不推荐用于生产环境）
echo "配置 root 用户远程访问权限..."
docker exec business-doris-fe mysql -h127.0.0.1 -P9030 -uroot <<'EOF'
-- 设置 root 用户密码为空，允许所有主机访问
SET PASSWORD FOR 'root'@'%' = PASSWORD('');
GRANT ALL PRIVILEGES ON *.* TO 'root'@'%';
FLUSH PRIVILEGES;
EOF

echo "✅ root 用户远程访问已配置（密码为空）"

# 创建专用的 ADDP 用户（推荐）
echo "创建 ADDP 专用数据库用户..."
docker exec business-doris-fe mysql -h127.0.0.1 -P9030 -uroot <<'EOF'
-- 创建 addp 用户，密码为 addp_password
CREATE USER IF NOT EXISTS 'addp'@'%' IDENTIFIED BY 'addp_password';
GRANT ALL PRIVILEGES ON *.* TO 'addp'@'%';
FLUSH PRIVILEGES;
EOF

echo "✅ ADDP 用户创建成功"

# 验证配置
echo ""
echo "=== 配置完成 ==="
echo "可用连接方式:"
echo ""
echo "1. root 用户 (不推荐生产环境):"
echo "   Host: localhost"
echo "   Port: 9030"
echo "   User: root"
echo "   Password: (空)"
echo ""
echo "2. addp 用户 (推荐):"
echo "   Host: localhost"
echo "   Port: 9030"
echo "   User: addp"
echo "   Password: addp_password"
echo ""
echo "验证连接:"
docker exec business-doris-fe mysql -h127.0.0.1 -P9030 -uroot -e "SELECT User, Host FROM mysql.user WHERE User IN ('root', 'addp');"
