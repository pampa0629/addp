# Spark Spark 工作流引擎 Dockerfile
# 使用 Debian Bullseye + Python 3.11 (避免 Trixie 的网络问题)
FROM localhost:5001/python:3.11-bullseye

# 安装系统依赖，并与 Business Spark 集群统一使用 OpenJDK 11
RUN sed -i 's|http://deb.debian.org|https://deb.debian.org|g' /etc/apt/sources.list && \
    apt-get -o Acquire::Retries=5 update && \
    DEBIAN_FRONTEND=noninteractive apt-get -o Acquire::Retries=5 install -y --no-install-recommends \
    openjdk-11-jre-headless \
    libgeos-dev \
    libproj-dev \
    gdal-bin \
    libgdal-dev \
    procps \
    && ln -s "$(dirname "$(dirname "$(readlink -f "$(command -v java)")")")" /opt/java \
    && rm -rf /var/lib/apt/lists/*

# 使用稳定路径，避免把 Debian 的 amd64/arm64 架构目录写死到镜像配置中
ENV JAVA_HOME=/opt/java
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
