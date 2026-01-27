#!/bin/bash

# ============================================================================
# Redis 数据查看脚本
# 功能：连接到 Redis 服务，查看运行时数据和持久化数据
# 使用方法：
#   ./show-redis.sh              # 显示所有 keys 和基本信息
#   ./show-redis.sh --keys       # 只显示所有 keys
#   ./show-redis.sh --info       # 只显示 Redis 服务器信息
#   ./show-redis.sh --get <key>  # 查看特定 key 的内容
#   ./show-redis.sh --pattern <pattern>  # 查看匹配模式的 keys
#   ./show-redis.sh --monitor    # 实时监控 Redis 命令
#   ./show-redis.sh --save       # 手动保存 RDB 快照
#   ./show-redis.sh --bgsave     # 后台保存 RDB 快照
# ============================================================================

set -e

# 获取脚本所在目录
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

# 加载环境变量
if [ -f "$PROJECT_ROOT/.env" ]; then
    set -a
    source "$PROJECT_ROOT/.env"
    set +a
else
    echo "❌ 错误: 找不到 .env 文件 ($PROJECT_ROOT/.env)"
    exit 1
fi

# 从环境变量获取 Redis 配置
REDIS_HOST="${REDIS_HOST:-localhost}"
REDIS_PORT="${REDIS_PORT:-16379}"
REDIS_PASSWORD="${REDIS_PASSWORD:-}"

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
RESET='\033[0m'

# 日期格式化
timestamp() {
    date '+%Y-%m-%d %H:%M:%S'
}

# 打印标题
print_header() {
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}"
    echo -e "${BLUE}$1${RESET}"
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}"
}

# 执行 Redis 命令的函数
redis_exec() {
    # 检查 redis-cli 是否存在
    if command -v redis-cli &> /dev/null; then
        redis-cli -h "$REDIS_HOST" -p "$REDIS_PORT" "$@"
    else
        # 使用 Docker 连接 Redis
        local docker_container=$(docker ps --filter "ancestor=redis:*" -q | head -1)
        if [ -z "$docker_container" ]; then
            docker_container=$(docker ps --filter "name=redis" -q | head -1)
        fi

        if [ -n "$docker_container" ]; then
            if [ -n "$REDIS_PASSWORD" ]; then
                docker exec -e REDISCLI_AUTH="$REDIS_PASSWORD" "$docker_container" redis-cli "$@"
            else
                docker exec "$docker_container" redis-cli "$@"
            fi
        else
            echo "ERROR: No Redis container found"
            return 1
        fi
    fi
}

# 设置环境变量来避免密码警告（用于本地 redis-cli）
if [ -n "$REDIS_PASSWORD" ]; then
    export REDISCLI_AUTH="$REDIS_PASSWORD"
fi

# 检查 Redis 连接
check_connection() {
    if ! redis_exec ping &>/dev/null; then
        echo -e "${RED}❌ 错误: 无法连接到 Redis ($REDIS_HOST:$REDIS_PORT)${RESET}"
        echo -e "${YELLOW}💡 请检查：${RESET}"
        echo "  1. Redis 服务是否运行: docker ps | grep redis"
        echo "  2. 主机和端口配置是否正确: REDIS_HOST=$REDIS_HOST, REDIS_PORT=$REDIS_PORT"
        echo "  3. 密码配置是否正确: REDIS_PASSWORD=$REDIS_PASSWORD"
        exit 1
    fi
}

# 显示 Redis 基本信息
show_info() {
    print_header "📊 Redis 服务器信息"
    echo -e "${CYAN}连接地址: ${RESET}$REDIS_HOST:$REDIS_PORT"
    echo -e "${CYAN}连接时间: ${RESET}$(timestamp)"
    echo ""

    redis_exec info server | grep -E "redis_version|redis_mode|process_id|uptime_in_seconds" | sed 's/^/  /'
    echo ""

    print_header "💾 内存使用情况"
    redis_exec info memory | grep -E "used_memory_human|used_memory_peak_human|maxmemory_human" | sed 's/^/  /'
    echo ""

    print_header "📈 统计信息"
    redis_exec info stats | grep -E "total_connections_received|total_commands_processed" | sed 's/^/  /'
    echo ""

    print_header "🗄️ 数据库统计"
    redis_exec info keyspace | grep -E "db[0-9]" | sed 's/^/  /'
    echo ""
}

# 显示所有 keys
show_all_keys() {
    print_header "🔑 所有 Redis Keys"

    local count=$(redis_exec dbsize | grep -oE '[0-9]+')
    echo -e "${CYAN}总 Keys 数: ${RESET}$count"
    echo ""

    if [ "$count" -eq 0 ]; then
        echo -e "${YELLOW}ℹ️ Redis 中没有数据${RESET}"
        return
    fi

    # 分页显示（每次最多显示 50 个）
    local page_size=50
    local cursor=0
    local total=0

    while true; do
        local result=$(redis_exec scan "$cursor" count "$page_size")
        cursor=$(echo "$result" | head -1)

        # 显示当前页的 keys
        echo "$result" | tail -n +2 | while IFS= read -r key; do
            if [ -n "$key" ]; then
                # 获取 key 的类型和大小
                local key_type=$(redis_exec type "$key")
                local key_size=$(redis_exec strlen "$key" 2>/dev/null || echo "N/A")

                # 根据类型获取更多信息
                local key_info=""
                case $key_type in
                    string)
                        key_info="[STRING] size=$key_size bytes"
                        ;;
                    hash)
                        local hlen=$(redis_exec hlen "$key")
                        key_info="[HASH] fields=$hlen"
                        ;;
                    list)
                        local llen=$(redis_exec llen "$key")
                        key_info="[LIST] length=$llen"
                        ;;
                    set)
                        local scard=$(redis_exec scard "$key")
                        key_info="[SET] members=$scard"
                        ;;
                    zset)
                        local zcard=$(redis_exec zcard "$key")
                        key_info="[ZSET] members=$zcard"
                        ;;
                    *)
                        key_info="[$key_type]"
                        ;;
                esac

                echo -e "  ${CYAN}$key${RESET} $key_info"
                total=$((total + 1))
            fi
        done

        # 如果 cursor 为 0，说明遍历完成
        if [ "$cursor" -eq 0 ]; then
            break
        fi
    done

    echo ""
    echo -e "${GREEN}✓ 共显示 $total 个 keys${RESET}"
}

# 显示特定 key 的内容
show_key_content() {
    local key="$1"

    if ! redis_exec exists "$key" &>/dev/null; then
        echo -e "${RED}❌ Key 不存在: $key${RESET}"
        return 1
    fi

    print_header "🔍 Key 详情: $key"

    local key_type=$(redis_exec type "$key")
    local ttl=$(redis_exec ttl "$key")

    echo -e "${CYAN}类型: ${RESET}$key_type"

    if [ "$ttl" -eq -1 ]; then
        echo -e "${CYAN}过期时间: ${RESET}永不过期"
    elif [ "$ttl" -eq -2 ]; then
        echo -e "${CYAN}过期时间: ${RESET}Key 已过期"
    else
        echo -e "${CYAN}过期时间: ${RESET}$ttl 秒"
    fi

    echo ""
    echo -e "${BLUE}内容:${RESET}"

    case $key_type in
        string)
            redis_exec get "$key" | sed 's/^/  /'
            ;;
        hash)
            echo "  (Hash 类型，包含以下字段:)"
            redis_exec hgetall "$key" | sed 's/^/    /'
            ;;
        list)
            echo "  (List 类型，包含以下元素:)"
            redis_exec lrange "$key" 0 -1 | nl | sed 's/^/    /'
            ;;
        set)
            echo "  (Set 类型，包含以下成员:)"
            redis_exec smembers "$key" | nl | sed 's/^/    /'
            ;;
        zset)
            echo "  (Sorted Set 类型，包含以下成员:)"
            redis_exec zrange "$key" 0 -1 withscores | sed 's/^/    /'
            ;;
        *)
            echo -e "${YELLOW}ℹ️ 不支持的类型: $key_type${RESET}"
            ;;
    esac

    echo ""
}

# 查找匹配模式的 keys
find_pattern_keys() {
    local pattern="$1"

    print_header "🔎 查找匹配模式的 Keys: $pattern"

    local keys=$(redis_exec keys "$pattern")

    if [ -z "$keys" ]; then
        echo -e "${YELLOW}ℹ️ 没有找到匹配的 keys${RESET}"
        return
    fi

    local count=0
    echo "$keys" | while IFS= read -r key; do
        if [ -n "$key" ]; then
            count=$((count + 1))
            local key_type=$(redis_exec type "$key")
            echo -e "  ${CYAN}$key${RESET} ($key_type)"
        fi
    done

    local total=$(echo "$keys" | grep -c .)
    echo ""
    echo -e "${GREEN}✓ 共找到 $total 个匹配的 keys${RESET}"
}

# 实时监控
monitor_redis() {
    print_header "📡 实时监控 Redis 命令（按 Ctrl+C 停止）"
    echo -e "${YELLOW}开始时间: $(timestamp)${RESET}"
    echo ""
    redis_exec monitor
}

# 手动保存
manual_save() {
    print_header "💾 手动保存 Redis 数据"
    echo -e "${YELLOW}执行 SAVE 命令...${RESET}"
    redis_exec save
    echo -e "${GREEN}✓ 数据已保存到 RDB 文件${RESET}"
}

# 后台保存
background_save() {
    print_header "💾 后台保存 Redis 数据"
    echo -e "${YELLOW}执行 BGSAVE 命令...${RESET}"
    redis_exec bgsave
    echo ""
    sleep 1
    redis_exec lastsave | sed 's/^/  最后保存时间: /'
    echo -e "${GREEN}✓ 后台保存已启动${RESET}"
}

# 显示帮助信息
show_help() {
    cat <<EOF
${BLUE}========== Redis 数据查看脚本 ==========${RESET}

${GREEN}使用方法:${RESET}
  ./show-redis.sh [选项] [参数]

${GREEN}选项:${RESET}
  (无)              显示所有 keys 和基本信息（默认）
  --keys            只显示所有 keys
  --info            只显示 Redis 服务器信息
  --get <key>       查看特定 key 的内容
  --pattern <pat>   查找匹配模式的 keys (支持通配符 * ?)
  --monitor         实时监控 Redis 命令
  --save            手动保存 RDB 快照（会锁定 Redis）
  --bgsave          后台保存 RDB 快照（不锁定）
  --help            显示此帮助信息

${GREEN}示例:${RESET}
  ./show-redis.sh                        # 显示所有 keys
  ./show-redis.sh --get my-key           # 查看特定 key
  ./show-redis.sh --pattern "asynq:*"   # 查找所有 asynq 相关 keys
  ./show-redis.sh --info                 # 显示服务器信息
  ./show-redis.sh --monitor              # 监控实时命令

${GREEN}环境变量配置:${RESET}
  REDIS_HOST=$REDIS_HOST
  REDIS_PORT=$REDIS_PORT
  REDIS_PASSWORD=${REDIS_PASSWORD:-(未设置)}

EOF
}

# 主程序
main() {
    # 检查 redis-cli 或 Docker 是否可用
    if ! command -v redis-cli &> /dev/null && ! command -v docker &> /dev/null; then
        echo -e "${RED}❌ 错误: redis-cli 和 Docker 都未找到${RESET}"
        echo -e "${YELLOW}请安装：${RESET}"
        echo "  - Redis: brew install redis (macOS) 或 apt-get install redis-tools (Linux)"
        echo "  - 或使用 Docker: https://www.docker.com/"
        exit 1
    fi

    # 检查连接
    check_connection

    # 处理命令行参数
    case "${1:-}" in
        --help)
            show_help
            ;;
        --keys)
            show_all_keys
            ;;
        --info)
            show_info
            ;;
        --get)
            if [ -z "$2" ]; then
                echo -e "${RED}❌ 错误: --get 需要指定 key 名称${RESET}"
                echo "用法: ./show-redis.sh --get <key>"
                exit 1
            fi
            show_key_content "$2"
            ;;
        --pattern)
            if [ -z "$2" ]; then
                echo -e "${RED}❌ 错误: --pattern 需要指定匹配模式${RESET}"
                echo "用法: ./show-redis.sh --pattern <pattern>"
                exit 1
            fi
            find_pattern_keys "$2"
            ;;
        --monitor)
            monitor_redis
            ;;
        --save)
            manual_save
            ;;
        --bgsave)
            background_save
            ;;
        "")
            # 默认：显示基本信息和所有 keys
            show_info
            show_all_keys
            ;;
        *)
            echo -e "${RED}❌ 未知选项: $1${RESET}"
            echo "请使用 --help 查看帮助信息"
            exit 1
            ;;
    esac
}

main "$@"
