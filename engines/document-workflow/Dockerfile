FROM python:3.12-slim-bookworm

ENV PYTHONDONTWRITEBYTECODE=1
ENV PYTHONUNBUFFERED=1
ENV PORT=8105
ENV DOCUMENT_LIBREOFFICE_BIN=/usr/bin/soffice
ENV DOCUMENT_WORK_DIR=/work/document
ENV HOME=/work/document/home
ENV XDG_CACHE_HOME=/work/document/cache
ENV XDG_CONFIG_HOME=/work/document/config

WORKDIR /app

RUN apt-get update \
  && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    fonts-noto-cjk \
    fonts-noto-core \
    libreoffice-impress \
  && rm -rf /var/lib/apt/lists/* \
  && useradd --system --uid 10001 --home-dir /work/document/home --create-home addp \
  && mkdir -p /work/document/cache /work/document/config \
  && chown -R addp:addp /work/document

COPY engines/document-workflow/requirements.txt /app/requirements.txt
COPY common-python/pyproject.toml /tmp/addp-common-pyproject.toml
RUN python - <<'PY'
import tomllib
from pathlib import Path

project = tomllib.loads(Path("/tmp/addp-common-pyproject.toml").read_text())
Path("/tmp/addp-common-requirements.txt").write_text("\n".join(project["project"]["dependencies"]) + "\n")
PY
RUN python -m pip install --no-cache-dir --upgrade pip 'setuptools>=61.0' \
  && python -m pip install --no-cache-dir \
    -r /tmp/addp-common-requirements.txt \
    -r /app/requirements.txt

COPY common-python/README.md /common-python/README.md
COPY common-python/addp_common/__init__.py /common-python/addp_common/__init__.py
COPY common-python/addp_common/module_lifecycle.py /common-python/addp_common/module_lifecycle.py
COPY common-python/addp_common/client /common-python/addp_common/client
COPY common-python/addp_common/workflow_access.py /common-python/addp_common/workflow_access.py
COPY common-python/addp_common/workflow_runtime /common-python/addp_common/workflow_runtime
COPY common-python/pyproject.toml /common-python/pyproject.toml
RUN python -m pip install --no-cache-dir --no-build-isolation --no-deps /common-python \
  && rm -rf /common-python

COPY engines/document-workflow/api_server.py engines/document-workflow/operators.py /app/

USER addp
EXPOSE 8105
CMD ["python", "api_server.py"]
