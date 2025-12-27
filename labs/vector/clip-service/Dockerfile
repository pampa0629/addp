FROM python:3.11-slim

WORKDIR /app

# 安装系统依赖（gcc/g++ 用于编译 Python 扩展，patch 用于 lmdb）
RUN apt-get update && apt-get install -y \
    gcc \
    g++ \
    patch \
    && rm -rf /var/lib/apt/lists/*

# 复制依赖文件
COPY requirements.txt .

# 安装 Python 依赖
RUN pip install --no-cache-dir -r requirements.txt

# 复制服务代码
COPY server.py .

# 暴露端口
EXPOSE 8888

# 启动服务
CMD ["python", "server.py"]
