ARG CONVERTER_IMAGE=addp/model3d-converter:linux-arm64
ARG PYTHON_IMAGE=python:3.12-slim
FROM ${CONVERTER_IMAGE} AS converter

FROM ${PYTHON_IMAGE}

ENV PYTHONDONTWRITEBYTECODE=1
ENV PYTHONUNBUFFERED=1
ENV MODEL3D_CONVERTER_BIN=/opt/addp/model3d-workflow/bin/_3dtile
ENV MODEL3D_MESH_CONVERTER_BIN=/usr/bin/assimp
ENV MODEL3D_IFC_CONVERTER_BIN=/opt/addp/model3d-workflow/bin/IfcConvert
ENV MODEL3D_GAUSSIAN_SPLAT_NODE_BIN=/usr/bin/node
ENV GDAL_DATA=/opt/addp/model3d-workflow/bin/gdal
ENV PROJ_DATA=/opt/addp/model3d-workflow/bin/proj
ENV OSG_LIBRARY_PATH=/opt/addp/model3d-workflow/bin/osgPlugins-3.6.5
ENV PORT=8101

WORKDIR /app

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates assimp-utils libgl1 libglu1-mesa libx11-6 libxrandr2 libxi6 libxxf86vm1 nodejs npm \
  && rm -rf /var/lib/apt/lists/*

COPY engines/model3d-workflow/requirements.txt /app/requirements.txt
COPY common-python/pyproject.toml /tmp/addp-common-pyproject.toml
RUN python - <<'PY'
import tomllib
from pathlib import Path

project = tomllib.loads(Path("/tmp/addp-common-pyproject.toml").read_text())
Path("/tmp/addp-common-requirements.txt").write_text(
    "\n".join(project["project"]["dependencies"]) + "\n"
)
PY
RUN python -m pip install --no-cache-dir --upgrade pip \
  && python -m pip install --no-cache-dir \
    -r /tmp/addp-common-requirements.txt \
    -r /app/requirements.txt
RUN python -m pip install --no-cache-dir --upgrade setuptools

COPY common-python/README.md /common-python/README.md
COPY common-python/pyproject.toml /common-python/pyproject.toml
COPY common-python/addp_common/ /common-python/addp_common/
RUN python -m pip install --no-cache-dir --no-build-isolation --no-deps /common-python \
  && rm -rf /common-python

COPY engines/model3d-workflow/package.json engines/model3d-workflow/package-lock.json /app/
RUN npm ci --omit=dev

COPY engines/model3d-workflow/api_server.py engines/model3d-workflow/operators.py engines/model3d-workflow/create_ksplat.mjs /app/
COPY --from=converter /opt/addp/model3d-workflow/bin/ /opt/addp/model3d-workflow/bin/

EXPOSE 8101
CMD ["python", "api_server.py"]
