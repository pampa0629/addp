FROM python:3.12-slim

ENV PYTHONDONTWRITEBYTECODE=1
ENV PYTHONUNBUFFERED=1
ENV PORT=8102
ENV POINTCLOUD_PDAL_BIN=/usr/bin/pdal

WORKDIR /app

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates pdal \
  && rm -rf /var/lib/apt/lists/*

COPY requirements.txt /app/requirements.txt
RUN python -m pip install --no-cache-dir --upgrade pip \
  && python -m pip install --no-cache-dir -r /app/requirements.txt

COPY api_server.py operators.py /app/

EXPOSE 8102
CMD ["python", "api_server.py"]
