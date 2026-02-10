#!/bin/bash
# 租户虚拟环境初始化脚本
# 用法: ./init_tenant_venv.sh <tenant_id>

set -e

TENANT_ID=$1

if [ -z "$TENANT_ID" ]; then
    echo "❌ 错误: 请提供租户 ID"
    echo "用法: $0 <tenant_id>"
    exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

# 基础虚拟环境路径
BASE_VENV_PATH="${SCRIPT_DIR}/venv"

# 租户数据目录
TENANTS_DATA_PATH="${PROJECT_ROOT}/data/jupyter/tenants"
TENANT_DIR="${TENANTS_DATA_PATH}/tenant_${TENANT_ID}"
TENANT_VENV_PATH="${TENANT_DIR}/venv"

# ADDP API 基础 URL (从环境变量读取,默认 http://localhost:8000)
ADDP_API_BASE="${ADDP_API_BASE:-http://localhost:8000}"

# 检查基础虚拟环境是否存在
if [ ! -d "$BASE_VENV_PATH" ]; then
    echo "❌ 错误: 基础虚拟环境不存在: $BASE_VENV_PATH"
    echo "请先运行 scripts/dev/start.sh 初始化基础环境"
    exit 1
fi

# 检查租户虚拟环境是否已存在
if [ -d "$TENANT_VENV_PATH" ]; then
    echo "✅ 租户 $TENANT_ID 的虚拟环境已存在: $TENANT_VENV_PATH"

    # 显示已安装的包数量
    PKG_COUNT=$("$TENANT_VENV_PATH/bin/pip" list 2>/dev/null | wc -l | tr -d ' ')
    echo "   - 已安装包数量: $PKG_COUNT"
    echo "   - Kernel 名称: tenant_${TENANT_ID}"

    exit 0
fi

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "🔧 为租户 $TENANT_ID 创建虚拟环境"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

# 创建租户目录
mkdir -p "$TENANT_DIR"

# 获取 Python 解释器路径 (使用基础环境的 Python)
PYTHON_BIN="$BASE_VENV_PATH/bin/python3"

# 创建租户虚拟环境 (独立环境，不继承 system-site-packages)
echo "📦 创建虚拟环境..."
python3 -m venv "$TENANT_VENV_PATH"

# 验证虚拟环境
if [ ! -f "$TENANT_VENV_PATH/bin/python" ]; then
    echo "❌ 错误: 虚拟环境创建失败"
    exit 1
fi

echo "✅ 虚拟环境创建成功"

# 从 requirements.txt 安装基础库（使用 pip 缓存加速）
echo "📦 安装基础库（使用 pip 缓存）..."
REQUIREMENTS_FILE="${SCRIPT_DIR}/requirements.txt"
if [ -f "$REQUIREMENTS_FILE" ]; then
    # pip 会自动使用缓存，大幅加速后续租户的安装
    "$TENANT_VENV_PATH/bin/pip" install --quiet -r "$REQUIREMENTS_FILE"
    echo "✅ 基础库安装完成"
else
    echo "⚠️  警告: requirements.txt 不存在，跳过基础库安装"
fi

# 注册 Jupyter Kernel
echo "📝 注册 Jupyter Kernel..."

KERNEL_NAME="tenant_${TENANT_ID}"
KERNEL_DISPLAY_NAME="Python 3 (租户 ${TENANT_ID})"

# 设置 IPYTHONDIR 环境变量，让 Kernel 使用租户独立的 IPython 配置
IPYTHON_DIR="${TENANT_DIR}/.ipython"

"$TENANT_VENV_PATH/bin/python" -m ipykernel install \
    --name "$KERNEL_NAME" \
    --display-name "$KERNEL_DISPLAY_NAME" \
    --prefix="$TENANT_DIR" \
    --env TENANT_ID "$TENANT_ID" \
    --env ADDP_API_BASE "$ADDP_API_BASE" \
    --env IPYTHONDIR "$IPYTHON_DIR"

echo "✅ Kernel 注册成功: $KERNEL_NAME"

# 部署 IPython startup 脚本
echo "📄 部署 IPython startup 脚本..."

IPYTHON_STARTUP_DIR="${TENANT_DIR}/.ipython/profile_default/startup"
mkdir -p "$IPYTHON_STARTUP_DIR"

# 复制 startup 脚本
cp "${SCRIPT_DIR}/ipython_startup_00_addp_datasources.py" \
   "$IPYTHON_STARTUP_DIR/00-addp-datasources.py"

echo "✅ IPython startup 脚本部署成功 ($IPYTHON_STARTUP_DIR)"

# 将 Kernel 链接到全局 Jupyter kernels 目录
echo "🔗 注册 Kernel 到 Jupyter..."

# 自动检测 Jupyter kernels 目录（兼容 macOS 和 Linux）
if [ "$(uname)" = "Darwin" ]; then
    # macOS: 使用 ~/Library/Jupyter/kernels
    JUPYTER_KERNELS_DIR="${HOME}/Library/Jupyter/kernels"
else
    # Linux: 使用 ~/.local/share/jupyter/kernels
    JUPYTER_KERNELS_DIR="${HOME}/.local/share/jupyter/kernels"
fi

mkdir -p "$JUPYTER_KERNELS_DIR"

# 创建软链接
ln -sf "${TENANT_DIR}/share/jupyter/kernels/${KERNEL_NAME}" \
       "${JUPYTER_KERNELS_DIR}/${KERNEL_NAME}"

echo "✅ Kernel 已注册到 Jupyter ($JUPYTER_KERNELS_DIR)"

# 显示总结信息
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "✅ 租户 $TENANT_ID 虚拟环境初始化完成"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "   虚拟环境路径: $TENANT_VENV_PATH"
echo "   Kernel 名称: $KERNEL_NAME"
echo "   Kernel 显示名: $KERNEL_DISPLAY_NAME"
echo ""
echo "   可用库数量: $("$TENANT_VENV_PATH/bin/pip" list 2>/dev/null | wc -l | tr -d ' ')"
echo ""
echo "   环境变量:"
echo "     - TENANT_ID=$TENANT_ID"
echo "     - ADDP_API_BASE=$ADDP_API_BASE"
echo ""
echo "🚀 租户可以在 Jupyter Lab 中选择此 Kernel 开始使用"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
