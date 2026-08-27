FROM python:3.12-slim

ENV PYTHONDONTWRITEBYTECODE=1
ENV PYTHONUNBUFFERED=1
ENV PORT=8102
ENV MAMBA_ROOT_PREFIX=/opt/conda
ENV PATH=/opt/conda/bin:$PATH
ENV POINTCLOUD_PDAL_BIN=/opt/conda/bin/pdal
ENV POINTCLOUD_WORK_DIR=/work/pointcloud
ENV CPL_TMPDIR=/work/pointcloud

WORKDIR /app

RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl bzip2 \
  && arch="$(uname -m)" \
  && case "$arch" in \
    x86_64) micromamba_arch="linux-64" ;; \
    aarch64|arm64) micromamba_arch="linux-aarch64" ;; \
    *) echo "unsupported architecture: $arch" >&2; exit 1 ;; \
  esac \
  && curl -Ls "https://micro.mamba.pm/api/micromamba/${micromamba_arch}/latest" \
    | tar -xvj -C /usr/local/bin --strip-components=1 bin/micromamba \
  && rm -rf /var/lib/apt/lists/* \
  && micromamba install -y -n base -c conda-forge python=3.12 pdal \
  && micromamba clean -a -y

COPY engines/pointcloud-workflow/requirements.txt /app/requirements.txt
COPY common-python/pyproject.toml /tmp/addp-common-pyproject.toml
RUN python - <<'PY'
import tomllib
from pathlib import Path

project = tomllib.loads(Path("/tmp/addp-common-pyproject.toml").read_text())
dependencies = project["project"]["dependencies"]
Path("/tmp/addp-common-requirements.txt").write_text("\n".join(dependencies) + "\n")
PY
RUN python -m pip install --no-cache-dir --upgrade pip \
  && python -m pip install --no-cache-dir \
    -r /tmp/addp-common-requirements.txt \
    -r /app/requirements.txt

# PointCloud 只打包其运行时实际使用的 common-python 子包，避免无关公共能力变化触发镜像重建。
COPY common-python/README.md /common-python/README.md
COPY common-python/addp_common/__init__.py /common-python/addp_common/__init__.py
COPY common-python/addp_common/module_lifecycle.py /common-python/addp_common/module_lifecycle.py
COPY common-python/addp_common/client /common-python/addp_common/client
COPY common-python/addp_common/workflow_access.py /common-python/addp_common/workflow_access.py
COPY common-python/addp_common/workflow_runtime /common-python/addp_common/workflow_runtime
COPY common-python/pyproject.toml /common-python/pyproject.toml
RUN python -m pip install --no-cache-dir --no-build-isolation --no-deps /common-python \
  && rm -rf /common-python

COPY engines/pointcloud-workflow/api_server.py engines/pointcloud-workflow/operators.py /app/

EXPOSE 8102
CMD ["python", "api_server.py"]
