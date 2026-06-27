# Python Workflow Engine Dockerfile
FROM localhost:5001/python:3.11-slim

# Install system dependencies for Python Workflow Engine (including build tools)
RUN apt-get update && apt-get install -y \
    build-essential \
    g++ \
    gcc \
    libgeos-dev \
    libproj-dev \
    gdal-bin \
    libgdal-dev \
    && rm -rf /var/lib/apt/lists/*

# Set working directory
WORKDIR /app

# Copy requirements and install Python dependencies
COPY engines/python-workflow/requirements.txt .
COPY common-python/ /tmp/common-python/
RUN GDAL_VERSION="$(gdal-config --version)" \
    && pip install --no-cache-dir "GDAL==${GDAL_VERSION}" \
    && pip install --no-cache-dir -r requirements.txt \
    && pip install --no-cache-dir /tmp/common-python

# Copy application code
COPY engines/python-workflow/operators/ ./operators/
COPY engines/python-workflow/workflow_engine.py .
COPY engines/python-workflow/api_server.py .

# Expose Flask port
EXPOSE 8099

# Set environment variables
ENV PYTHONUNBUFFERED=1
ENV PORT=8099
ENV PYTHON_WORKFLOW_GUNICORN_TIMEOUT=7200

# Run with Gunicorn for production
CMD ["sh", "-c", "exec gunicorn --bind 0.0.0.0:8099 --workers 4 --timeout ${PYTHON_WORKFLOW_GUNICORN_TIMEOUT:-7200} api_server:app"]
