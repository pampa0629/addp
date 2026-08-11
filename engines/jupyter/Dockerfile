FROM localhost:5001/python:3.12-slim AS python-runtime

WORKDIR /workspace
ENV WORKSPACE_DIR=/workspace/notebooks

# 安装运行时系统依赖
RUN sed -i 's|http://deb.debian.org|https://deb.debian.org|g' \
        /etc/apt/sources.list.d/debian.sources \
    && apt-get -o Acquire::Retries=5 update \
    && apt-get -o Acquire::Retries=5 install -y --no-install-recommends \
        curl \
        gdal-bin \
    && rm -rf /var/lib/apt/lists/*

COPY engines/jupyter/requirements.txt /tmp/jupyter-requirements.txt
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
    && pip install --no-cache-dir \
        -r /tmp/addp-common-requirements.txt \
        -r /tmp/jupyter-requirements.txt
RUN pip install --no-cache-dir --upgrade setuptools

COPY common-python/README.md /opt/addp-common/README.md
COPY common-python/pyproject.toml /opt/addp-common/pyproject.toml
COPY common-python/addp_common/ /opt/addp-common/addp_common/
RUN pip install --no-cache-dir --no-build-isolation --no-deps /opt/addp-common \
    && rm -rf /opt/addp-common

FROM localhost:5001/node:20-alpine AS labextension-builder

WORKDIR /build

COPY engines/jupyter/labextension/package.json ./package.json
COPY engines/jupyter/labextension/package-lock.json ./package-lock.json
RUN npm ci --no-audit --no-fund

COPY engines/jupyter/labextension/tsconfig.json ./tsconfig.json
COPY engines/jupyter/labextension/src/ ./src/
COPY --from=python-runtime \
    /usr/local/lib/python3.12/site-packages/jupyterlab/staging/package.json \
    /tmp/jupyterlab-core/package.json
RUN ./node_modules/.bin/tsc \
    && ./node_modules/.bin/build-labextension \
        --core-path /tmp/jupyterlab-core \
        .

FROM python-runtime

COPY --from=labextension-builder /build/labextension/ \
    /usr/local/share/jupyter/labextensions/@addp/notebook-bridge/

# 复制 API 服务器
COPY engines/jupyter/api_server.py /app/
COPY engines/jupyter/config.py /app/
COPY engines/jupyter/startup.sh /app/
RUN chmod +x /app/startup.sh

# 创建工作目录
RUN mkdir -p /workspace/notebooks

EXPOSE 8097

CMD ["/bin/bash", "/app/startup.sh"]
