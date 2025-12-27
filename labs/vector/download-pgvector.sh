#!/bin/bash
set -e

# 下载 pgvector 源码（如果本地没有）
if [ ! -d "pgvector-0.8.0" ]; then
    echo "下载 pgvector 源码..."
    curl -L https://github.com/pgvector/pgvector/archive/refs/tags/v0.8.0.tar.gz -o pgvector-0.8.0.tar.gz
    tar -xzf pgvector-0.8.0.tar.gz
    rm pgvector-0.8.0.tar.gz
    echo "✅ pgvector 源码下载完成"
else
    echo "✅ pgvector 源码已存在"
fi
