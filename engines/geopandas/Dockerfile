# GeoPandas Engine Dockerfile
FROM python:3.11

# Install system dependencies for GeoPandas
RUN apt-get update && apt-get install -y \
    libgeos-dev \
    libproj-dev \
    gdal-bin \
    libgdal-dev \
    && rm -rf /var/lib/apt/lists/*

# Set working directory
WORKDIR /app

# Copy requirements and install Python dependencies
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Copy application code
COPY operators.py .
COPY workflow_engine.py .
COPY api_server.py .

# Expose Flask port
EXPOSE 8099

# Set environment variables
ENV PYTHONUNBUFFERED=1
ENV PORT=8099

# Run with Gunicorn for production
CMD ["gunicorn", "--bind", "0.0.0.0:8099", "--workers", "4", "--timeout", "600", "api_server:app"]
