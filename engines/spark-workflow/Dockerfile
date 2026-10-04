# Spark 工作流引擎：Bookworm 提供系统依赖，官方 Temurin 提供 Java 11。
FROM localhost:5001/eclipse-temurin:11-jre-jammy AS java-runtime
FROM localhost:5001/python:3.11-slim-bookworm

COPY --from=java-runtime /opt/java/openjdk /opt/java

# 安装系统依赖，并与 Business Spark 集群统一使用 OpenJDK 11
RUN sed -i 's|http://deb.debian.org|https://deb.debian.org|g' /etc/apt/sources.list.d/debian.sources && \
    apt-get -o Acquire::Retries=5 update && \
    DEBIAN_FRONTEND=noninteractive apt-get -o Acquire::Retries=5 install -y --no-install-recommends \
    libfontconfig1 \
    libgeos-dev \
    libproj-dev \
    gdal-bin \
    libgdal-dev \
    procps \
    curl \
    && rm -rf /var/lib/apt/lists/*

# 使用稳定路径，避免把 Debian 的 amd64/arm64 架构目录写死到镜像配置中
ENV JAVA_HOME=/opt/java
ENV PATH="$JAVA_HOME/bin:$PATH"

# 设置工作目录
WORKDIR /app

# 配置 pip 使用清华源
RUN pip config set global.index-url https://pypi.tuna.tsinghua.edu.cn/simple

# 从公共包的唯一事实源读取其依赖。
COPY engines/spark-workflow/requirements.txt .
COPY common-python/pyproject.toml /tmp/addp-common-pyproject.toml
RUN python - <<'PY'
import tomllib
from pathlib import Path

project = tomllib.loads(Path("/tmp/addp-common-pyproject.toml").read_text())
Path("/tmp/addp-common-requirements.txt").write_text(
    "\n".join(project["project"]["dependencies"]) + "\n"
)
PY
RUN pip install --no-cache-dir --upgrade pip \
    && pip install --no-cache-dir -r /tmp/addp-common-requirements.txt -r requirements.txt

COPY common-python/README.md /common-python/README.md
COPY common-python/pyproject.toml /common-python/pyproject.toml
COPY common-python/addp_common/ /common-python/addp_common/
RUN pip install --no-cache-dir --no-build-isolation --no-deps /common-python \
    && rm -rf /common-python

# 复制应用代码
COPY engines/spark-workflow/*.py .
COPY engines/spark-workflow/operators ./operators

# 构建时通过生产连接器实际加载 Sedona/JDBC 依赖并计算，不启动开发服务。
RUN python -m pip check \
    && python -c "from api_server import app; assert app.test_client().get('/health').status_code == 200" \
    && python - <<'PY'
import os
import spark_connector

# Only System resource discovery is replaced by a build fixture. Session
# construction, Maven dependencies and function registration use production code.
os.environ["SPARK_MODE"] = "local"
spark_connector.get_engine = lambda engine_id, tenant_id: {
    "connection_info": {"host": "127.0.0.1", "port": 10000}
}
spark = spark_connector.SparkConnector().get_or_create_session(engine_id=1, tenant_id=1)
try:
    assert spark.sparkContext._jvm.java.lang.System.getProperty("java.specification.version") == "11"
    assert spark.range(3).count() == 3
    assert spark.range(3).selectExpr("sum(id) AS total").first().total == 3
    assert spark.sql("SELECT ST_AsText(ST_Point(1, 2)) AS geometry").first().geometry == "POINT (1 2)"
finally:
    spark.stop()
PY

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
