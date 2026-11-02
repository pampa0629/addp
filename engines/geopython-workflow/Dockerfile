# GeoPython Workflow Dockerfile
FROM localhost:5001/python:3.11-slim

# Install system dependencies for GeoPython Workflow (including build tools)
RUN apt-get update && apt-get install -y \
    build-essential \
    g++ \
    gcc \
    libgeos-dev \
    libproj-dev \
    gdal-bin \
    libgdal-dev \
    libhdf5-dev \
    libnetcdf-dev \
    pkg-config \
    unixodbc \
    odbc-mdbtools \
    && rm -rf /var/lib/apt/lists/*

# Metadata-only HDF5 composition check, linked to this Runtime's native stack.
COPY engines/geopython-workflow/native/netcdf_preflight.c /tmp/netcdf_preflight.c
RUN mkdir -p /usr/local/libexec \
    && cc -std=c11 -D_POSIX_C_SOURCE=200809L -Wall -Wextra -Werror /tmp/netcdf_preflight.c \
        -o /usr/local/libexec/addp-netcdf-preflight $(pkg-config --cflags --libs hdf5 netcdf) \
    && rm /tmp/netcdf_preflight.c
ENV GEOPYTHON_NETCDF_PREFLIGHT_BIN=/usr/local/libexec/addp-netcdf-preflight

# GDAL PGeo probes the standard Microsoft Access ODBC driver names. Debian's
# MDB Tools package registers only MDBTools, so expose the same driver under
# the two names used by GDAL without assuming a CPU architecture path.
RUN python - <<'PY'
from pathlib import Path

driver = next(Path("/usr/lib").glob("*/odbc/libmdbodbc.so"))
config = Path("/etc/odbcinst.ini")
text = config.read_text()
text = text.replace("Driver=libmdbodbc.so", f"Driver={driver}")
text = text.replace("Setup=libmdbodbc.so", f"Setup={driver}")
aliases = "".join(
    f"\n[{name}]\nDescription=MDBTools Driver for GDAL PGeo\nDriver={driver}\nSetup={driver}\nFileUsage=1\n"
    for name in ("Microsoft Access Driver (*.mdb, *.accdb)", "Microsoft Access Driver (*.mdb)")
)
config.write_text(text.rstrip() + "\n" + aliases)
PY

# Set working directory
WORKDIR /app

# Copy requirements and install Python dependencies
COPY engines/geopython-workflow/requirements.txt .
COPY common-python/pyproject.toml /tmp/addp-common-pyproject.toml
RUN python - <<'PY'
import tomllib
from pathlib import Path

project = tomllib.loads(Path("/tmp/addp-common-pyproject.toml").read_text())
Path("/tmp/addp-common-requirements.txt").write_text(
    "\n".join(project["project"]["dependencies"]) + "\n"
)
PY
RUN GDAL_VERSION="$(gdal-config --version)" \
    && pip install --no-cache-dir --upgrade pip \
    && pip install --no-cache-dir "GDAL==${GDAL_VERSION}" \
    && pip install --no-cache-dir \
        -r requirements.txt \
        -r /tmp/addp-common-requirements.txt

COPY common-python/README.md /tmp/common-python/README.md
COPY common-python/pyproject.toml /tmp/common-python/pyproject.toml
COPY common-python/addp_common/ /tmp/common-python/addp_common/
RUN pip install --no-cache-dir --no-build-isolation --no-deps /tmp/common-python \
    && rm -rf /tmp/common-python

# Copy application code
COPY engines/geopython-workflow/operators/ ./operators/
COPY engines/geopython-workflow/geometry_batches.py .
COPY engines/geopython-workflow/raster_resources.py .
COPY engines/geopython-workflow/workflow_engine.py .
COPY engines/geopython-workflow/api_server.py .
COPY engines/geopython-workflow/container_entrypoint.sh .

RUN python -c "from osgeo import gdal; assert gdal.GetDriverByName('JP2OpenJPEG'), 'JPEG2000 requires JP2OpenJPEG'; assert gdal.GetDriverByName('GS7BG'), 'Surfer 7 requires GS7BG'; assert gdal.GetDriverByName('GSBG'), 'Surfer 6 Binary requires GSBG'; assert gdal.GetDriverByName('GSAG'), 'Surfer 6 ASCII requires GSAG'"

# Read with the actual native ORC wheel before admitting the product image.
RUN python - <<'PYORC'
import io, pyorc
assert pyorc.__version__ == '0.12.0'
stream = io.BytesIO()
with pyorc.Writer(stream, 'struct<id:bigint>') as writer:
    writer.writerows([(2**63-1,), (None,)])
stream.seek(0)
assert pyorc.Reader(stream).read() == [(2**63-1,), (None,)]
PYORC

# Admit the linked NetCDF-4 checker and the same GDAL reader in the actual image.
RUN python - <<'PYNETCDF'
import json, os, tempfile
from pathlib import Path
import numpy as np
from osgeo import gdal
from operators import _native_process
with tempfile.TemporaryDirectory(prefix='addp-netcdf-build-') as temporary:
    path=Path(temporary)/'input.nc'
    ds=gdal.GetDriverByName('netCDF').CreateMultiDimensional(str(path),options=['FORMAT=NC4'])
    root=ds.GetRootGroup();y=root.CreateDimension('y','','',2);x=root.CreateDimension('x','','',3)
    grid=root.CreateMDArray('grid',[y,x],gdal.ExtendedDataType.Create(gdal.GDT_Int32))
    grid.Write(np.arange(6,dtype=np.int32).reshape(2,3))
    grid=None;x=None;y=None;root=None;ds=None
    checked=_native_process.run([os.environ['GEOPYTHON_NETCDF_PREFLIGHT_BIN'],str(path)],capture_output=True,text=True,check=True,timeout=30)
    assert json.loads(checked.stdout)=={'schema':'addp.netcdf4.composition/v1','profile':'root_numeric_v1','complete':True}
    ds=gdal.OpenEx(f'NETCDF:"{path}"',gdal.OF_MULTIDIM_RASTER|gdal.OF_READONLY,allowed_drivers=['netCDF'])
    root=ds.GetRootGroup();grid=root.OpenMDArray('grid')
    assert grid.ReadAsArray().tolist()==[[0,1,2],[3,4,5]]
    grid=None;root=None;ds=None
print('NetCDF-4 root numeric composition and GDAL image smoke passed')
PYNETCDF

# Verify the packaged API and health route without starting a service or contacting System.
RUN python -c "import api_server; assert api_server.app.test_client().get('/health').status_code == 200"

# Expose Flask port
EXPOSE 8099

# Set environment variables
ENV PYTHONUNBUFFERED=1
ENV PORT=8099
ENV PROJ_NETWORK=OFF
ENV GEOPYTHON_WORKFLOW_GUNICORN_TIMEOUT=7200

# Run with Gunicorn for production
CMD ["sh", "container_entrypoint.sh"]
