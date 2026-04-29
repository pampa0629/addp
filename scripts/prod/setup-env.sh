#!/bin/bash
# setup-env.sh - 生产环境 .env 初始化脚本
#
# 功能:
#   1. 检查 .env 文件是否存在
#   2. 若不存在，从 .env.example 复制并自动生成安全随机密钥
#   3. 替换所有需要修改的占位密钥
#
# 用法: bash scripts/prod/setup-env.sh

GREEN='\033[0;32m'
YELLOW='\033[0;33m'
RED='\033[0;31m'
NC='\033[0m'

# 生成随机密钥（32字节十六进制）
gen_secret() {
  # 优先用 openssl，其次用 /dev/urandom
  if command -v openssl > /dev/null 2>&1; then
    openssl rand -hex 32
  else
    cat /dev/urandom | tr -dc 'a-f0-9' | head -c 64
  fi
}

# 生成随机密码（字母数字，16位）
gen_password() {
  if command -v openssl > /dev/null 2>&1; then
    openssl rand -base64 16 | tr -dc 'a-zA-Z0-9' | head -c 16
  else
    cat /dev/urandom | tr -dc 'a-zA-Z0-9' | head -c 16
  fi
}

# 生成 Base64 编码的 32 字节密钥（用于 ENCRYPTION_KEY，AES-256）
gen_b64_key() {
  if command -v openssl > /dev/null 2>&1; then
    openssl rand -base64 32
  else
    cat /dev/urandom | head -c 32 | base64
  fi
}

ENV_FILE=".env"
EXAMPLE_FILE=".env.example"

if [ -f "$ENV_FILE" ]; then
  updated=false

  ensure_env() {
    local key="$1"
    local value="$2"

    if grep -q "^${key}=" "$ENV_FILE"; then
      return
    fi

    printf '\n%s=%s\n' "$key" "$value" >> "$ENV_FILE"
    updated=true
  }

  ensure_env "ENCRYPTION_KEY" "$(gen_b64_key)"

  if [ "$updated" = true ]; then
    echo -e "${GREEN}✓ .env 文件已存在，已补齐缺失的安全密钥${NC}"
  else
    echo -e "${GREEN}✓ .env 文件已存在，跳过初始化${NC}"
  fi
  exit 0
fi

# PostgreSQL 密码只在数据目录首次初始化时生效。若已有数据卷却重新随机生成 .env，
# 应用会使用新密码连接旧数据库，导致 password authentication failed。
if docker volume inspect addp-infra_postgres_data > /dev/null 2>&1; then
  echo -e "${RED}错误: 检测到已有 PostgreSQL 数据卷 addp-infra_postgres_data，但当前目录没有 .env${NC}"
  echo -e "${YELLOW}请从原部署目录复制 .env 后再启动；如果确认要全新部署，请先执行:${NC}"
  echo -e "${YELLOW}  docker compose -f docker-compose.infra.yml down -v${NC}"
  echo -e "${YELLOW}  或手动删除数据卷: docker volume rm addp-infra_postgres_data${NC}"
  exit 1
fi

if [ ! -f "$EXAMPLE_FILE" ]; then
  echo -e "${RED}错误: .env.example 不存在，无法初始化 .env${NC}"
  exit 1
fi

echo -e "${YELLOW}未找到 .env 文件，正在从 .env.example 自动生成...${NC}"

cp "$EXAMPLE_FILE" "$ENV_FILE"

# 生成各密钥
POSTGRES_PASS=$(gen_password)
REDIS_PASS=$(gen_password)
MINIO_PASS=$(gen_password)
JWT=$(gen_secret)
MEILI_KEY=$(gen_secret)
ENCRYPTION_KEY=$(gen_b64_key)

# 替换占位值（使用 | 作为分隔符避免 / 冲突）
sed -i.bak \
  -e "s|^POSTGRES_PASSWORD=.*|POSTGRES_PASSWORD=${POSTGRES_PASS}|" \
  -e "s|^REDIS_PASSWORD=.*|REDIS_PASSWORD=${REDIS_PASS}|" \
  -e "s|^MINIO_ROOT_PASSWORD=.*|MINIO_ROOT_PASSWORD=${MINIO_PASS}|" \
  -e "s|^JWT_SECRET=.*|JWT_SECRET=${JWT}|" \
  -e "s|^MEILISEARCH_MASTER_KEY=.*|MEILISEARCH_MASTER_KEY=${MEILI_KEY}|" \
  -e "s|^ENCRYPTION_KEY=.*|ENCRYPTION_KEY=${ENCRYPTION_KEY}|" \
  "$ENV_FILE"

rm -f "${ENV_FILE}.bak"

echo -e "${GREEN}✓ .env 已生成，密钥已自动随机化${NC}"
echo -e "${YELLOW}  POSTGRES_PASSWORD: ${POSTGRES_PASS}${NC}"
echo -e "${YELLOW}  REDIS_PASSWORD:    ${REDIS_PASS}${NC}"
echo -e "${YELLOW}  MINIO_PASSWORD:    ${MINIO_PASS}${NC}"
echo -e "${YELLOW}  ENCRYPTION_KEY:    (已生成，见 .env)${NC}"
echo -e "${YELLOW}  JWT_SECRET:        (已生成，见 .env)${NC}"
echo -e "${YELLOW}  MEILISEARCH_KEY:   (已生成，见 .env)${NC}"
echo -e "${YELLOW}如需自定义，请编辑 .env 后重新运行 start.sh${NC}"
