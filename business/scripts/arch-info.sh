#!/usr/bin/env bash

# Quick Reference for Business Infrastructure Architecture Management
# 业务基础设施架构管理快速参考

cat << 'EOF'

╔═══════════════════════════════════════════════════════════════════╗
║     Business Infrastructure - Architecture Management             ║
╚═══════════════════════════════════════════════════════════════════╝

📋 快速命令

1. 验证当前架构是否正确
   cd business && ./scripts/verify-arch.sh

2. 重启服务（自动适配正确架构）
   cd business && ./scripts/restart.sh

3. 查看容器架构信息
   docker inspect business-postgres | grep Architecture

4. 查看镜像架构信息
   docker image inspect postgis/postgis:15-3.4 | grep Architecture

5. 手动拉取指定架构镜像
   # ARM64 (Apple Silicon)
   docker pull --platform=linux/arm64 postgis/postgis:15-3.4

   # AMD64 (Intel/AMD)
   docker pull --platform=linux/amd64 postgis/postgis:15-3.4

═══════════════════════════════════════════════════════════════════

🎯 推荐工作流程

首次部署:
  1. cd business
  2. cp .env.example .env
  3. ./scripts/start.sh
  4. ./scripts/verify-arch.sh

架构不匹配时:
  1. cd business
  2. ./scripts/restart.sh  # 自动检测并修复
  3. ./scripts/verify-arch.sh  # 验证修复结果

═══════════════════════════════════════════════════════════════════

🔍 当前系统信息

EOF

echo "系统架构: $(uname -m)"
echo "Docker 版本: $(docker --version)"
echo ""

if docker ps --filter "name=business-postgres" -q &>/dev/null; then
    echo "业务数据库状态: 运行中 ✓"
    POSTGRES_ARCH=$(docker inspect business-postgres 2>/dev/null | grep -m1 '"Architecture"' | awk -F'"' '{print $4}')
    echo "容器架构: ${POSTGRES_ARCH}"

    SYSTEM_ARCH=$(uname -m)
    case "${SYSTEM_ARCH}" in
        x86_64) EXPECTED="amd64" ;;
        arm64|aarch64) EXPECTED="arm64" ;;
        *) EXPECTED="unknown" ;;
    esac

    if [ "${POSTGRES_ARCH}" = "${EXPECTED}" ]; then
        echo "架构状态: 匹配 ✅"
    else
        echo "架构状态: 不匹配 ⚠️  (预期: ${EXPECTED}, 实际: ${POSTGRES_ARCH})"
        echo ""
        echo "建议运行: cd business && ./scripts/restart.sh"
    fi
else
    echo "业务数据库状态: 未运行 ✗"
    echo ""
    echo "建议运行: cd business && ./scripts/start.sh"
fi

cat << 'EOF'

═══════════════════════════════════════════════════════════════════

📚 详细文档

- 架构方案详解: business/docs/ARCHITECTURE.md
- 启动脚本说明: business/scripts/restart.sh
- 验证脚本说明: business/scripts/verify-arch.sh

═══════════════════════════════════════════════════════════════════

EOF
