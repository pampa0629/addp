FROM localhost:5001/python:3.11-slim

WORKDIR /app

RUN apt-get update && apt-get install -y \
    gcc \
    postgresql-client \
    && rm -rf /var/lib/apt/lists/*

# 先安装 common-python（本地共享包）
COPY common-python/ /tmp/common-python/
RUN pip install --no-cache-dir /tmp/common-python && rm -rf /tmp/common-python

# 安装其他依赖（过滤掉本地路径引用，本地开发时 requirements.txt 保留该行）
COPY agent/backend/requirements.txt .
RUN grep -v "common-python" requirements.txt | pip install --no-cache-dir -r /dev/stdin

# 复制代码
COPY agent/backend/ .

EXPOSE 8190

CMD ["uvicorn", "main:app", "--host", "0.0.0.0", "--port", "8190"]
