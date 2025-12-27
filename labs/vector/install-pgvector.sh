#!/bin/bash
# 安装 pgvector 扩展

set -e

echo "开始安装 pgvector 扩展..."

# 更新包列表
apt-get update -qq

# 安装依赖
apt-get install -y -qq \
    build-essential \
    git \
    postgresql-server-dev-15 \
    wget

# 克隆 pgvector 源码
cd /tmp
git clone --branch v0.8.0 https://github.com/pgvector/pgvector.git
cd pgvector

# 编译并安装
make clean
make OPTFLAGS=""
make install

# 清理
cd /
rm -rf /tmp/pgvector

echo "✅ pgvector 扩展安装完成"
