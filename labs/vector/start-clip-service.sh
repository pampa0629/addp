#!/bin/bash
# CLIP 服务快速启动脚本（本地 Python 方式）

set -e

echo "🚀 启动 CLIP 向量化服务..."

cd "$(dirname "$0")/clip-service"

# 检查虚拟环境
if [ ! -d "venv" ]; then
    echo "📦 创建 Python 虚拟环境..."
    python3 -m venv venv
fi

# 激活虚拟环境
source venv/bin/activate

# 安装依赖
if [ ! -f "venv/.installed" ]; then
    echo "📦 安装依赖（首次运行需要下载 ~1GB）..."
    pip install --upgrade pip -q
    pip install -r requirements.txt -q
    touch venv/.installed
fi

# 设置 Hugging Face 镜像站（加速模型下载）
export HF_ENDPOINT=https://hf-mirror.com

# 启动服务
echo "✅ 启动 CLIP 服务（端口 8888）..."
echo "提示: 首次运行会自动下载 CLIP 模型（约 800MB），请耐心等待"
echo "使用 Hugging Face 镜像站: $HF_ENDPOINT"
python server.py
