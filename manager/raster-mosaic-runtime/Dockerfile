FROM localhost:5001/python:3.11-slim

RUN apt-get update && apt-get install -y \
    build-essential \
    g++ \
    gcc \
    gdal-bin \
    libgdal-dev \
    libjpeg-dev \
    zlib1g-dev \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY manager/raster-mosaic-runtime/requirements.txt .
RUN GDAL_VERSION="$(gdal-config --version)" \
    && pip install --no-cache-dir "GDAL==${GDAL_VERSION}" \
    && pip install --no-cache-dir -r requirements.txt

COPY manager/raster-mosaic-runtime/app.py .
COPY manager/raster-mosaic-runtime/runtime/ ./runtime/

EXPOSE 8291

ENV PYTHONUNBUFFERED=1
ENV PORT=8291

CMD ["gunicorn", "--bind", "0.0.0.0:8291", "--workers", "2", "--timeout", "120", "app:app"]
