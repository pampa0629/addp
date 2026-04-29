# Spark Spark 工作流引擎 Dockerfile
# 使用 Debian Bullseye + Python 3.11 (避免 Trixie 的网络问题)
FROM localhost:5001/python:3.11-bullseye

# 安装系统依赖 (Bullseye 中的 OpenJDK 17 稳定可用)
RUN apt-get update && \
    apt-get install -y \
    openjdk-17-jre-headless \
    libgeos-dev \
    libproj-dev \
    gdal-bin \
    libgdal-dev \
    procps \
    || (sleep 10 && apt-get update && apt-get install -y \
    openjdk-17-jre-headless \
    libgeos-dev \
    libproj-dev \
    gdal-bin \
    libgdal-dev \
    procps) \
    && rm -rf /var/lib/apt/lists/*

# 设置 Java 环境变量
ENV JAVA_HOME=/usr/lib/jvm/java-17-openjdk-arm64
ENV PATH="$JAVA_HOME/bin:$PATH"

# 设置工作目录
WORKDIR /app

# 配置 pip 使用清华源
RUN pip config set global.index-url https://pypi.tuna.tsinghua.edu.cn/simple

# 复制依赖文件并安装 Python 依赖
# 注意: 使用 --no-deps 安装主要包,让 pip 自动解析依赖,避免 hash 冲突
COPY requirements.txt .
RUN pip install --no-cache-dir --upgrade pip && \
    pip install --no-cache-dir -r requirements.txt

# 复制应用代码
COPY *.py .
COPY operators ./operators

# 暴露 Flask API 端口
EXPOSE 8098

# 设置环境变量
ENV PYTHONUNBUFFERED=1
ENV PORT=8098

# 健康检查
HEALTHCHECK --interval=30s --timeout=10s --start-period=40s --retries=3 \
    CMD python -c "import requests; requests.get('http://localhost:8098/health')"

# 使用 Gunicorn 运行（生产级）
CMD ["gunicorn", "--bind", "0.0.0.0:8098", "api_server:app", "--workers", "4", "--timeout", "300"]
