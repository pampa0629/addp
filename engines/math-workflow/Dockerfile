FROM python:3.11-slim

LABEL maintainer="ADDP Team"
LABEL description="Math Workflow Engine - 基础数学运算工作流引擎"

WORKDIR /app

COPY engines/math-workflow/requirements.txt .
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
COPY engines/math-workflow/ .

# 暴露端口
EXPOSE 8089

# 健康检查
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD python -c "import urllib.request; urllib.request.urlopen('http://localhost:8089/health', timeout=2).read()"

# 启动应用
CMD ["python", "api_server.py"]
