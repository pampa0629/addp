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

COPY requirements.txt /app/requirements.txt
RUN python -m pip install --no-cache-dir --upgrade pip \
  && python -m pip install --no-cache-dir -r /app/requirements.txt

COPY api_server.py operators.py /app/

EXPOSE 8102
CMD ["python", "api_server.py"]
