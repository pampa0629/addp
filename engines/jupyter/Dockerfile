FROM localhost:5001/python:3.12-slim

WORKDIR /workspace
ENV WORKSPACE_DIR=/workspace/notebooks

# 安装系统依赖
RUN apt-get update && apt-get install -y \
    gcc g++ curl nodejs npm \
    libpq-dev \
    gdal-bin libgdal-dev \
    && rm -rf /var/lib/apt/lists/*

COPY common-python /opt/addp-common
COPY engines/jupyter/requirements.txt /tmp/jupyter-requirements.txt
RUN pip install --no-cache-dir /opt/addp-common \
    && pip install --no-cache-dir -r /tmp/jupyter-requirements.txt

COPY engines/jupyter/labextension /tmp/addp-notebook-bridge
RUN cd /tmp/addp-notebook-bridge \
    && npm ci --no-audit --no-fund \
    && npm run build \
    && mkdir -p /usr/local/share/jupyter/labextensions/@addp/notebook-bridge \
    && cp -a labextension/. /usr/local/share/jupyter/labextensions/@addp/notebook-bridge/ \
    && rm -rf /tmp/addp-notebook-bridge

# 复制 API 服务器
COPY engines/jupyter/api_server.py /app/
COPY engines/jupyter/config.py /app/
COPY engines/jupyter/startup.sh /app/
RUN chmod +x /app/startup.sh

# 创建工作目录
RUN mkdir -p /workspace/notebooks

EXPOSE 8097

CMD ["/bin/bash", "/app/startup.sh"]
