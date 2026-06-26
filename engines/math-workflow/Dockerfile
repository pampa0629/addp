FROM python:3.11-slim

LABEL maintainer="ADDP Team"
LABEL description="Math Workflow Engine - 基础数学运算工作流引擎"

WORKDIR /app

# 安装依赖
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# 复制应用代码
COPY . .

# 暴露端口
EXPOSE 8089

# 健康检查
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD python -c "import urllib.request; urllib.request.urlopen('http://localhost:8089/health', timeout=2).read()"

# 启动应用
CMD ["python", "api_server.py"]
