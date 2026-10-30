#!/bin/bash
# Read-only Node/npm diagnostics for source-development environment preparation.
set -uo pipefail
if [ "$#" -ne 0 ]; then
  echo '用法: bash scripts/dev/doctor.sh（不接受安装、版本覆盖或执行参数）' >&2
  exit 2
fi

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "$ROOT_DIR/scripts/utils/node-runtime.sh" || exit 1
required=$(addp_node_required_major) || exit 1

printf 'Node 要求: 稳定主版本 %s (%s/.node-version)\n' "$required" "$ROOT_DIR"
node_command=$(command -v node || true)
printf 'Node 路径: %s\n' "${node_command:-未找到}"
status=0
node_ready=false
if addp_require_node_runtime; then
  node_ready=true
  printf 'Node 版本: %s\n' "$(node --version)"
else
  status=1
fi

npm_command=$(command -v npm || true)
printf 'npm 路径: %s\n' "${npm_command:-未找到}"
if [ -z "$npm_command" ]; then
  echo '缺少 npm；请准备 Node 安装随附的 npm。' >&2
  status=1
elif [ "$node_ready" = true ]; then
  if npm_version=$(npm_config_logs_max=0 npm_config_update_notifier=false npm --version 2>/dev/null) && [ -n "$npm_version" ]; then
    printf 'npm 版本: %s\n' "$npm_version"
  else
    echo '无法读取 npm 版本；请检查上述可执行路径及安装。' >&2
    status=1
  fi
else
  echo 'npm 版本: 未执行（Node 校验未通过）'
fi

if [ "$status" -eq 0 ]; then
  echo 'Node/npm 工具链检查通过。其他工具、Runtime、Infra 和业务服务尚未由本检查验证。'
else
  printf '请安装并选择根 .node-version 声明的 Node 稳定主版本 %s 及 npm。\n' "$required"
  printf '官方发行目录: https://nodejs.org/dist/latest-v%s.x/\n' "$required"
  echo '已安装多个版本时，须让当前命令的 PATH 优先指向正确安装；目录名不能代替实际版本检查。'
  echo '可对单条命令使用 env PATH="/实际的Node安装目录/bin:$PATH" make dev-doctor；不要求版本管理器或全局 Shell 配置。'
fi
printf '准备流程: %s/docs/guide/addp部署和开发步骤.md\n' "$ROOT_DIR"
echo '本检查不读取 .env，不安装依赖、不修改配置、不启停服务。'
exit "$status"
