FROM localhost:5001/python:3.12-slim

WORKDIR /app

RUN apt-get update && apt-get install -y \
    gcc \
    postgresql-client \
    && rm -rf /var/lib/apt/lists/*

# 先安装第三方依赖；公共源码放到后续轻量层，避免源码变化使依赖层失效。
COPY agent/backend/requirements.txt /tmp/agent-requirements.txt
COPY common-python/pyproject.toml /tmp/addp-common-pyproject.toml
RUN python - <<'PY'
import tomllib
from pathlib import Path

project = tomllib.loads(Path("/tmp/addp-common-pyproject.toml").read_text())
dependencies = list(project["project"]["dependencies"])
dependencies.extend(project["project"]["optional-dependencies"]["inference-langchain"])
Path("/tmp/addp-common-requirements.txt").write_text("\n".join(dependencies) + "\n")
PY
RUN grep -v "common-python" /tmp/agent-requirements.txt > /tmp/agent-runtime-requirements.txt \
    && pip install --no-cache-dir --upgrade pip \
    && pip install --no-cache-dir \
        -r /tmp/addp-common-requirements.txt \
        -r /tmp/agent-runtime-requirements.txt

# 安装公共源码包，不重复解析依赖或创建联网构建隔离环境。
COPY common-python/README.md /tmp/common-python/README.md
COPY common-python/pyproject.toml /tmp/common-python/pyproject.toml
COPY common-python/addp_common/ /tmp/common-python/addp_common/
RUN pip install --no-cache-dir --no-build-isolation --no-deps /tmp/common-python \
    && rm -rf /tmp/common-python

# 保留仓库层级，开发和容器复用平台 Skill 唯一加载路径。
WORKDIR /app/agent/backend
COPY agent/backend/ .
COPY skills/ /app/skills/
RUN python -m pip check \
    && python -c "from main import app; from agents.main_agent import _load_skill_registry; assert _load_skill_registry(), 'agent_skills_missing'; app.openapi()"

EXPOSE 8190

CMD ["uvicorn", "main:app", "--host", "0.0.0.0", "--port", "8190"]

ARG BUILD_ARCH=amd64
ARG BUILD_TYPE=release
ARG GOOS=linux
COPY dist/${BUILD_TYPE}-${GOOS}-${BUILD_ARCH}/runtime-log /app/runtime-log
ENTRYPOINT ["/app/runtime-log", "launch", "--module", "agent", "--role", "backend", "--"]
