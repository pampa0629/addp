FROM python:3.12-slim

WORKDIR /workspace

# 安装系统依赖
RUN apt-get update && apt-get install -y \
    gcc g++ curl \
    libpq-dev \
    gdal-bin libgdal-dev \
    && rm -rf /var/lib/apt/lists/*

# 安装 Python 库
RUN pip install --no-cache-dir \
    jupyterlab==4.0.9 \
    notebook==7.0.6 \
    ipykernel==6.27.1 \
    pandas==2.2.0 \
    geopandas==1.0.0 \
    matplotlib==3.8.2 \
    plotly==5.18.0 \
    folium==0.15.1 \
    psycopg2-binary==2.9.9 \
    pymysql==1.1.0 \
    minio==7.2.5 \
    sqlalchemy==2.0.25 \
    papermill==2.5.0 \
    nbconvert==7.12.0 \
    flask==3.0.0 \
    flask-cors==4.0.0

# 复制配置文件和 API 服务器
COPY jupyter_notebook_config.py /root/.jupyter/
COPY api_server.py /app/
COPY startup.sh /app/
RUN chmod +x /app/startup.sh

# 创建工作目录
RUN mkdir -p /workspace/notebooks

EXPOSE 8088 8097

CMD ["/bin/bash", "/app/startup.sh"]
