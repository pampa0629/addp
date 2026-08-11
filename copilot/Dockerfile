FROM localhost:5001/python:3.11-slim

WORKDIR /app

RUN apt-get update && apt-get install -y \
    gcc \
    postgresql-client \
    && rm -rf /var/lib/apt/lists/*

# 先安装第三方依赖；公共源码放到后续轻量层，避免源码变化使依赖层失效。
COPY copilot/backend/requirements.txt /tmp/copilot-requirements.txt
COPY common-python/pyproject.toml /tmp/addp-common-pyproject.toml
RUN python - <<'PY'
import tomllib
from pathlib import Path

project = tomllib.loads(Path("/tmp/addp-common-pyproject.toml").read_text())
dependencies = list(project["project"]["dependencies"])
dependencies.extend(project["project"]["optional-dependencies"]["inference-langchain"])
Path("/tmp/addp-common-requirements.txt").write_text("\n".join(dependencies) + "\n")
PY
RUN grep -v "common-python" /tmp/copilot-requirements.txt > /tmp/copilot-runtime-requirements.txt \
    && pip install --no-cache-dir --upgrade pip \
    && pip install --no-cache-dir \
        -r /tmp/addp-common-requirements.txt \
        -r /tmp/copilot-runtime-requirements.txt

# 安装公共源码包，不重复解析依赖或创建联网构建隔离环境。
COPY common-python/README.md /tmp/common-python/README.md
COPY common-python/pyproject.toml /tmp/common-python/pyproject.toml
COPY common-python/addp_common/ /tmp/common-python/addp_common/
RUN pip install --no-cache-dir --no-build-isolation --no-deps /tmp/common-python \
    && rm -rf /tmp/common-python

# 复制代码
COPY copilot/backend/ .

EXPOSE 8087

CMD ["uvicorn", "main:app", "--host", "0.0.0.0", "--port", "8087"]
