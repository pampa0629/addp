ARG CONVERTER_IMAGE=addp/model3d-converter:linux-arm64
ARG PYTHON_IMAGE=python:3.12-slim
FROM ${CONVERTER_IMAGE} AS converter

FROM ${PYTHON_IMAGE}

ENV PYTHONDONTWRITEBYTECODE=1
ENV PYTHONUNBUFFERED=1
ENV MODEL3D_CONVERTER_BIN=/opt/addp/model3d-workflow/bin/_3dtile
ENV MODEL3D_MESH_CONVERTER_BIN=/usr/bin/assimp
ENV GDAL_DATA=/opt/addp/model3d-workflow/bin/gdal
ENV PROJ_DATA=/opt/addp/model3d-workflow/bin/proj
ENV OSG_LIBRARY_PATH=/opt/addp/model3d-workflow/bin/osgPlugins-3.6.5
ENV PORT=8101

WORKDIR /app

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates assimp-utils libgl1 libglu1-mesa libx11-6 libxrandr2 libxi6 libxxf86vm1 \
  && rm -rf /var/lib/apt/lists/*

COPY requirements.txt /app/requirements.txt
RUN python -m pip install --no-cache-dir --upgrade pip \
  && python -m pip install --no-cache-dir -r /app/requirements.txt

COPY api_server.py operators.py /app/
COPY --from=converter /opt/addp/model3d-workflow/bin/ /opt/addp/model3d-workflow/bin/

EXPOSE 8101
CMD ["python", "api_server.py"]
