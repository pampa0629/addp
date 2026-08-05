#!/bin/bash
# setup-env.sh - 生产环境 .env 初始化脚本
#
# 功能:
#   1. 检查 .env 文件是否存在
#   2. 若不存在，从 .env.example 复制并自动生成安全随机密钥
#   3. 生成当前 IAM、基础设施和模块 OAuth Client 所需的独立 Secret
#   4. 已有 .env 时只校验，不静默轮换持久化数据依赖的密钥
#
# 用法: bash scripts/prod/setup-env.sh

GREEN='\033[0;32m'
YELLOW='\033[0;33m'
RED='\033[0;31m'
NC='\033[0m'

if ! command -v openssl > /dev/null 2>&1; then
  echo -e "${RED}错误: 生产环境初始化需要 openssl${NC}"
  exit 1
fi

# 生成 32 字节随机值，以十六进制保存。
gen_secret() {
  openssl rand -hex 32
}

# 生成仅含字母数字的 24 位随机密码。
gen_password() {
  openssl rand -base64 32 | tr -dc 'a-zA-Z0-9' | head -c 24
}

# 生成 Base64 编码的 32 字节密钥。
gen_b64_key() {
  openssl rand -base64 32
}

ENV_FILE=".env"
EXAMPLE_FILE=".env.example"

SERVICE_SECRET_KEYS=(
  ASSET_SERVICE_CLIENT_SECRET
  DEVELOP_SERVICE_CLIENT_SECRET
  DUCKDB_SERVICE_CLIENT_SECRET
  MANAGER_SERVICE_CLIENT_SECRET
  META_SERVICE_CLIENT_SECRET
  MONITOR_SERVICE_CLIENT_SECRET
  ORCHESTRATOR_SERVICE_CLIENT_SECRET
  PORTAL_SERVICE_CLIENT_SECRET
  QUALITY_SERVICE_CLIENT_SECRET
  SERVICE_SERVICE_CLIENT_SECRET
  TRANSFER_SERVICE_CLIENT_SECRET
)

env_value() {
  local key="$1"
  sed -n "s/^${key}=//p" "$ENV_FILE" | tail -n 1
}

require_secret() {
  local key="$1"
  local value
  value="$(env_value "$key")"
  case "$value" in
    ""|*change-in-production*|*replace-with-*|*WILL_BE_GENERATED*|dev-internal-key*|\
    addp_password|addp_redis|minioadmin|your-master-key-change-in-production|\
    addp_kafka_admin|addp_kafka_connect|addp_kafka_transfer)
      echo -e "${RED}错误: ${key} 未配置有效的生产 Secret${NC}"
      return 1
      ;;
  esac
}

require_b64_key() {
  local key="$1"
  local value decoded_size
  value="$(env_value "$key")"
  decoded_size="$(printf '%s' "$value" | openssl base64 -d -A 2>/dev/null | wc -c | tr -d ' ')"
  if [ "$decoded_size" != "32" ]; then
    echo -e "${RED}错误: ${key} 必须是 Base64 编码的 32 字节密钥${NC}"
    return 1
  fi
}

validate_production_env() {
  if [ "$(env_value ENV)" != "production" ]; then
    echo -e "${RED}错误: 生产启动要求 ENV=production${NC}"
    return 1
  fi

  local key value previous
  for key in \
    POSTGRES_PASSWORD REDIS_PASSWORD MINIO_ROOT_PASSWORD MEILISEARCH_MASTER_KEY \
    INTERNAL_API_KEY INFRA_KAFKA_ADMIN_PASSWORD INFRA_KAFKA_CONNECT_PASSWORD \
    INFRA_KAFKA_TRANSFER_PASSWORD; do
    require_secret "$key" || return 1
  done

  for key in ENCRYPTION_KEY OAUTH_USER_CODE_PEPPER IAM_MFA_ENCRYPTION_KEY; do
    require_secret "$key" || return 1
    require_b64_key "$key" || return 1
  done

  local seen_secrets=()
  for key in "${SERVICE_SECRET_KEYS[@]}"; do
    require_secret "$key" || return 1
    value="$(env_value "$key")"
    if [ "${#value}" -lt 32 ] || [ "${#value}" -gt 72 ]; then
      echo -e "${RED}错误: ${key} 长度必须为 32-72 字节${NC}"
      return 1
    fi
    for previous in "${seen_secrets[@]}"; do
      if [ "$value" = "$previous" ]; then
        echo -e "${RED}错误: 模块 OAuth Client Secret 不得复用${NC}"
        return 1
      fi
    done
    seen_secrets+=("$value")
  done
}

replace_env() {
  local key="$1"
  local value="$2"
  sed -i.bak "s|^${key}=.*|${key}=${value}|" "$ENV_FILE"
  rm -f "${ENV_FILE}.bak"
}

if [ -f "$ENV_FILE" ]; then
  validate_production_env
  chmod 600 "$ENV_FILE"
  echo -e "${GREEN}✓ 已有 .env 通过生产配置校验，未修改任何 Secret${NC}"
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
chmod 600 "$ENV_FILE"

replace_env ENV production
replace_env POSTGRES_PASSWORD "$(gen_password)"
replace_env REDIS_PASSWORD "$(gen_password)"
replace_env MINIO_ROOT_PASSWORD "$(gen_password)"
replace_env MEILISEARCH_MASTER_KEY "$(gen_secret)"
replace_env INTERNAL_API_KEY "$(gen_secret)"
replace_env ENCRYPTION_KEY "$(gen_b64_key)"
replace_env OAUTH_USER_CODE_PEPPER "$(gen_b64_key)"
replace_env IAM_MFA_ENCRYPTION_KEY "$(gen_b64_key)"
replace_env INFRA_KAFKA_ADMIN_PASSWORD "$(gen_secret)"
replace_env INFRA_KAFKA_CONNECT_PASSWORD "$(gen_secret)"
replace_env INFRA_KAFKA_TRANSFER_PASSWORD "$(gen_secret)"

for key in "${SERVICE_SECRET_KEYS[@]}"; do
  replace_env "$key" "$(gen_secret)"
done

validate_production_env

echo -e "${GREEN}✓ .env 已生成，密钥已自动随机化${NC}"
echo -e "${YELLOW}所有 Secret 仅保存在 .env，未输出到终端。${NC}"
echo -e "${YELLOW}启动前请复核公共 URL、反向代理网段与 Business Engine 连接信息。${NC}"
