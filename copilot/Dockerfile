FROM python:3.11-slim

WORKDIR /app

# 安装系统依赖
RUN apt-get update && apt-get install -y \
    gcc \
    postgresql-client \
    && rm -rf /var/lib/apt/lists/*

# 安装 Python 依赖
COPY backend/requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# 复制代码
COPY backend/ .

# 暴露端口
EXPOSE 8087

# 启动服务
CMD ["uvicorn", "main:app", "--host", "0.0.0.0", "--port", "8087"]
