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
    libaec-dev \
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
COPY engines/geopython-workflow/native/grib_ccsds_preflight.c /tmp/grib_ccsds_preflight.c
RUN cc -std=c11 -Wall -Wextra -Werror -shared -fPIC /tmp/grib_ccsds_preflight.c \
      -o /usr/local/libexec/addp-grib-ccsds-preflight.so -laec \
    && rm /tmp/grib_ccsds_preflight.c
ENV GEOPYTHON_GRIB_CCSDS_PREFLIGHT_LIB=/usr/local/libexec/addp-grib-ccsds-preflight.so


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
COPY engines/geopython-workflow/raster_resources.py engines/geopython-workflow/runtime_metrics.py ./
COPY engines/geopython-workflow/workflow_engine.py .
COPY engines/geopython-workflow/api_server.py .
COPY engines/geopython-workflow/private_execution.py engines/geopython-workflow/private_files.py engines/geopython-workflow/private_file_api.py ./
COPY engines/geopython-workflow/container_entrypoint.sh .

RUN python -c "from osgeo import gdal; assert gdal.GetDriverByName('GRIB'), 'GRIB requires GRIB'; assert gdal.GetDriverByName('JP2OpenJPEG'), 'JPEG2000 requires JP2OpenJPEG'; assert gdal.GetDriverByName('GS7BG'), 'Surfer 7 requires GS7BG'; assert gdal.GetDriverByName('GSBG'), 'Surfer 6 Binary requires GSBG'; assert gdal.GetDriverByName('GSAG'), 'Surfer 6 ASCII requires GSAG'"

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

# Fixed independent GRIB2 IEEE64/bitmap bytes exercise the packaged worker.
RUN python - <<'PYGRIB'
import hashlib, tempfile
from pathlib import Path
from operators.grib import OPERATORS
raw=bytes.fromhex("""
475249420000000200000000000000d300000015010007000002000107ea0102030000000100000048030000
00000600000000060000000000000000000000000000000000000300000002000000000000000080a7d8c00e
e6b28030809896800f053700000f4240000f4240400000002204000000000000020000000000010000000667
0000000002ffffffffffff0000000c0500000005000402000000070600bc0000002d074071b4000000000040
71d4000000000040c387800000000040719400000000004071a4000000000037373737
""")
with tempfile.TemporaryDirectory(prefix='addp-grib-build-') as work:
    path=Path(work)/'input.grib2';path.write_bytes(raw)
    plan={'schema_version':'addp.workflow.access-plan/v1','source':{'kind':'file','format':'grib','access':{'method':'mounted_path','path':str(path)}}}
    info=OPERATORS['grib.inspect']['function'](access_plan=plan)
    assert info['format_info']['source_version']=='sha256:'+hashlib.sha256(raw).hexdigest()
    result=OPERATORS['grib.read_slice']['function'](access_plan=plan,source_version=info['format_info']['source_version'],selection=dict(variable='message-1',indices=[],x=0,y=0,width=3,height=2))
    assert result['values']==[['9999.0','281.25','282.25'],['283.25','NaN','285.25']]
    assert result['valid']==[[True,True,True],[True,False,True]]
    assert result['variable']['attributes']['GRIB_UNIT']['values']==['[K]']
    assert result['variable']['grid']['transform']==[249.5,1.,0.,-9.5,0.,-1.]
print('GRIB2 native units and bitmap image smoke passed')

# Independent GRIB1 simple/bitmap, scaled interval/layer and constant bytes.
raw=bytes.fromhex("""
475249420000720100001c020751ffc00b6401f41a01020300010300000000001500000000002000ff000004
000300753001adb080006d6001b96803e803e80000000000000008040000fbf0000022080000000000001027
0f0119011a011b011c011e011f01200121012201230037373737475249420000660100001c020751ff800b65
32641a01020300010306040002001500000000002000ff000003000300753003d09080006d6003d86003e803
e8000000000000001e088002c150000010000f00100011001200130014001500160017003737373747524942
0000540100001c020751ff800b6900021a0102030001012c0a0000001500000000002000ff00000200040075
3001adb08000697801b19803e803e8000000000000000c08000043118000000037373737
""")
with tempfile.TemporaryDirectory(prefix='addp-grib1-build-') as work:
    path=Path(work)/'input.grib1';path.write_bytes(raw)
    plan={'schema_version':'addp.workflow.access-plan/v1','source':{'kind':'file','format':'grib','access':{'method':'mounted_path','path':str(path)}}}
    info=OPERATORS['grib.inspect']['function'](access_plan=plan)
    assert info['format_info']['encoding']=='GRIB1' and info['format_info']['profile']=='grib1_regular_fields/v1'
    assert info['format_info']['source_version']=='sha256:'+hashlib.sha256(raw).hexdigest()
    expected=[([['9999.0','281.0','282.0','283.0'],['284.0','NaN','286.0','287.0'],['288.0','289.0','290.0','291.0']],[[True]*4,[True,False,True,True],[True]*4]),
        ([['-1.25','-1.0','-0.75'],['-0.5','-0.25','0.0'],['0.25','0.5','0.75']],[[True]*3]*3),([['280.0']*2]*4,[[True]*2]*4)]
    for number,(values,valid) in enumerate(expected,1):
        result=OPERATORS['grib.read_slice']['function'](access_plan=plan,source_version=info['format_info']['source_version'],selection=dict(variable=f'message-{number}',indices=[],x=0,y=0,width=len(values[0]),height=len(values)))
        assert result['values']==values and result['valid']==valid
        assert result['variable']['attributes']['GRIB_UNIT']['values']==['[K]']
        if number==2:
            facts=result['variable']['message']
            assert facts['time']['operation']=='accumulation' and facts['time']['start_utc']=='2026-01-02T06:00:00+00:00' and facts['time']['end_utc']=='2026-01-02T09:00:00+00:00'
            assert facts['level']['unit']=='kPa' and facts['level']['upper']==50 and facts['level']['lower']==100
print('GRIB1 values bitmap time and layer image smoke passed')

# Independent two-order grouping bytes: Kelvin, negative wind and0-6h rain.
raw=bytes.fromhex("""
475249420000000200000000000000e300000015010007000002000107ea010203000000010000004803000000
000c00000000060000000000000000000000000000000000000400000003000000000000000001c9c380068e77
803001ab3f0006bc3e40000f4240000f4240000000002204000000000000020000000000010000000367000000
0002ffffffffffff00000031050000000b0003000000000000000001000100ffffffffffffffff000000030004
000000010100000003020202000000080600fbf00000001707270f0119800120e200f8000000097e0001613737
3737475249420000000200000000000000d600000015010007000002000107ea01020300000001000000480300
0000000900000000060000000000000000000000000000000000000300000003000000000000000001c9c3800e
e6b2803001ab3f000f053700000f4240000f424000000000220400000000020202000000000001000000066700
0000000affffffffffff0000003105000000090003c0a000008002000000000100ffffffffffffffff00000003
00000000000101000000010202020000000606ff0000000c07000f00100000f037373737475249420000000200
000000000000ee00000015010007000002000107ea010203000000010000004803000000000900000000060000
000000000000000000000000000000000300000003000000000000000001c9c380068e77803001ab3f0006acfc
00000f4240000f4240000000003a040000000801080200000000000100000000010000000000ffffffffffff07
ea0102090000010000000001020100000006ff0000000000000031050000000900030000000080020000000001
00ffffffffffffffff0000000300000000000101000000010202020000000606ff0000000c07000000010000f0
37373737
""")
with tempfile.TemporaryDirectory(prefix='addp-grib-complex-build-') as work:
    path=Path(work)/'complex.grib2';path.write_bytes(raw)
    plan={'schema_version':'addp.workflow.access-plan/v1','source':{'kind':'file','format':'grib','access':{'method':'mounted_path','path':str(path)}}}
    info=OPERATORS['grib.inspect']['function'](access_plan=plan)
    expected=[([['9999.0','281.0','282.0','283.0'],['284.0','NaN','286.0','287.0'],['288.0','289.0','290.0','291.0']],'[K]'),
        ([['-1.25','-1.0','-0.75'],['-0.5','-0.25','0.0'],['0.25','0.5','0.75']],'[m/s]'),([['0.0','0.25','0.5'],['0.75','1.0','1.25'],['1.5','1.75','2.0']],'[kg/(m^2)]')]
    for number,(values,unit) in enumerate(expected,1):
        result=OPERATORS['grib.read_slice']['function'](access_plan=plan,source_version=info['format_info']['source_version'],selection=dict(variable=f'message-{number}',indices=[],x=0,y=0,width=len(values[0]),height=len(values)))
        assert result['values']==values and result['valid']==[[v!='NaN' for v in row] for row in values]
        assert result['variable']['attributes']['GRIB_UNIT']['values']==[unit]
        facts=result['variable']['message'];assert facts['data_template']==3 and facts['packing']['order']==2
        if number==3:
            assert facts['time']['operation']=='accumulation' and facts['time']['start_utc']=='2026-01-02T03:00:00+00:00' and facts['time']['end_utc']=='2026-01-02T09:00:00+00:00'
            assert result['variable']['attributes']['GRIB_FORECAST_SECONDS']['values']==['0']
print('GRIB2 complex spatial interval smoke passed')

with tempfile.TemporaryDirectory() as directory:
    path=Path(directory)/'decimal.grib2'
    path.write_bytes(bytes.fromhex('475249420000000200000000000000d600000015010007000002000107ea010203000000010000004803000000000900000000060000000000000000000000000000000000000300000003000000000000000001c9c3800ee6b2803001ab3f000f053700000f4240000f4240000000002204000000000202020000000000010000000667000000000affffffffffff0000003105000000090003c3fa00000000000200000100ffffffffffffffff0000000300000000000101000000010202020000000606ff0000000c070179017a0000f037373737'))
    plan={'schema_version':'addp.workflow.access-plan/v1','source':{'kind':'file','format':'grib','access':{'method':'mounted_path','path':str(path)}}}
    info=OPERATORS['grib.inspect']['function'](access_plan=plan)
    result=OPERATORS['grib.read_slice']['function'](access_plan=plan,source_version=info['format_info']['source_version'],selection=dict(variable='message-1',indices=[],x=0,y=0,width=3,height=3))
    assert result['values']==[['-1.2300000190734863', '-1.2200000286102295', '-1.209999918937683'], ['-1.1999999284744263', '-1.1899999380111694', '-1.1799999475479126'], ['-1.1699999570846558', '-1.159999966621399', '-1.149999976158142']] and result['valid']==[[True, True, True], [True, True, True], [True, True, True]]
    assert result['variable']['attributes']['GRIB2_DECODE_PRECISION']['values']==['float32_scaling_float64_output']
print('GRIB2 decimal Float32 precision smoke passed')


with tempfile.TemporaryDirectory() as directory:
    path = Path(directory) / 'ecmwf-ccsds.grib2'
    path.write_bytes(bytes.fromhex('''
47524942ffff000200000000000000d500000015010062000020000107ea010203000000010000001102000100
010009040130303031000000480300000000200000000006000000000000000000000000000000000000080000
0004000000000000000001c9c380068e778030019bfcc006f94740000f4240000f424000000000220400000000
000002ffa10000000100000003670000000002ffffffffffff000000190500000020002a438700008002000008
000e2000800000000606ff00000012072009249249249249249249249237373737
'''))
    plan = {'schema_version': 'addp.workflow.access-plan/v1',
            'source': {'kind': 'file', 'format': 'grib', 'access': {'method': 'mounted_path', 'path': str(path)}}}
    info = OPERATORS['grib.inspect']['function'](access_plan=plan)
    result = OPERATORS['grib.read_slice']['function'](
        access_plan=plan, source_version=info['format_info']['source_version'],
        selection=dict(variable='message-1', indices=[], x=0, y=0, width=8, height=4))
    assert result['values'] == [[repr(270. + (y * 8 + x) / 4) for x in range(8)] for y in range(4)]
    assert result['valid'] == [[True] * 8 for _ in range(4)]
    assert result['variable']['message']['data_template'] == 42
    assert result['variable']['message']['local']['mars_stream'] == 'oper'
print('GRIB2 ECMWF CCSDS complete stream and values smoke passed')

with tempfile.TemporaryDirectory() as directory:
    path = Path(directory) / 'ecmwf-precipitation.grib2'
    path.write_bytes(bytes.fromhex('''
47524942ffff000200000000000000ee00000015010062000020010107ea010203000000010000001102000100
010009040130303031000000480300000000200000000006000000000000000000000000000000000000080000
0004000000000000000001c9c380068e77803001be51d006a92b700003d0900003d090000000003a0400000008
01c102ffa1000000010000000001ffffffffffffffffffffff07ea0108090000010000000001020100000096
0d000001c2000000190500000020002a00000000800c00000c000e2000800000000606ff0000001307100049
249249249249249249249037373737
'''))
    plan = {'schema_version': 'addp.workflow.access-plan/v1',
            'source': {'kind': 'file', 'format': 'grib', 'access': {'method': 'mounted_path', 'path': str(path)}}}
    info = OPERATORS['grib.inspect']['function'](access_plan=plan)
    result = OPERATORS['grib.read_slice']['function'](
        access_plan=plan, source_version=info['format_info']['source_version'],
        selection=dict(variable='message-1', indices=[], x=0, y=0, width=8, height=4))
    assert result['values'] == [[repr((y * 8 + x) / 4096) for x in range(8)] for y in range(4)]
    assert result['valid'] == [[True] * 8 for _ in range(4)]
    assert result['variable']['attributes']['GRIB_UNIT']['values'] == ['[m]']
    facts = result['variable']['message']
    assert facts['parameter']['local_table'] == 1 and facts['product_template'] == 8
    assert facts['time']['operation'] == 'accumulation' and facts['time']['increment_value'] == 450
    assert facts['time']['start_utc'] == '2026-01-02T03:00:00+00:00'
    assert facts['time']['end_utc'] == '2026-01-08T09:00:00+00:00'
print('GRIB2 ECMWF CCSDS precipitation interval and source metres smoke passed')

with tempfile.TemporaryDirectory() as directory:
    path = Path(directory) / 'ecmwf-precipitation-0h.grib2'
    path.write_bytes(bytes.fromhex('''
47524942ffff000200000000000000e000000015010062000020010107ea010203000000010000001102
000100010009040130303031000000480300000000200000000006000000000000000000000000000000
0000000800000004000000000000000001c9c380068e77803001be51d006a92b700003d0900003d09000
0000003a040000000801c102ffa1000000010000000001ffffffffffffffffffffff07ea010203000001
00000000010201000000000d000001c2000000190500000020002a000000000000000000000e20008000
00000606ff000000050737373737
'''))
    plan = {'schema_version': 'addp.workflow.access-plan/v1',
            'source': {'kind': 'file', 'format': 'grib', 'access': {'method': 'mounted_path', 'path': str(path)}}}
    info = OPERATORS['grib.inspect']['function'](access_plan=plan)
    result = OPERATORS['grib.read_slice']['function'](
        access_plan=plan, source_version=info['format_info']['source_version'],
        selection=dict(variable='message-1', indices=[], x=0, y=0, width=8, height=4))
    assert result['values'] == [['0.0'] * 8 for _ in range(4)]
    assert result['valid'] == [[True] * 8 for _ in range(4)]
    facts, attributes = result['variable']['message'], result['variable']['attributes']
    assert facts['packing']['bits_per_value'] == 0
    assert facts['time']['start_utc'] == facts['time']['end_utc'] == '2026-01-02T03:00:00+00:00'
    assert attributes['GRIB_UNIT']['values'] == ['[m]']
    assert attributes['GRIB2_DECODE_PRECISION']['values'] == ['exact_zero_float64_output']
print('GRIB2 ECMWF CCSDS 0h exact zero smoke passed')

with tempfile.TemporaryDirectory() as directory:
    path = Path(directory) / 'ifs-0p1-0h.grib2'
    # ECMWF IFS 2026-11-17 00 UTC, 0.1 degree surface total precipitation.
    path.write_bytes(bytes.fromhex('''
47524942ffff000200000000000000e000000015010062000020010107ea0b1100000000010000001102
0001000100090401303030310000004803000062ee900000000006ffffffffffffffffffffffffffffff
00000e100000070900000000ffffffff055d4a800aba950030855d4a800ab90e60000186a0000186a000
0000003a040000000801c102ffa1000000010000000001ffffffffffffffffffffff07ea0b1100000001
00000000010201000000000d000000e100000019050062ee90002a000000000000000000000e20008000
00000606ff000000050737373737
'''))
    plan = {'schema_version': 'addp.workflow.access-plan/v1',
            'source': {'kind': 'file', 'format': 'grib', 'access': {'method': 'mounted_path', 'path': str(path)}}}
    info = OPERATORS['grib.inspect']['function'](access_plan=plan)
    result = OPERATORS['grib.read_slice']['function'](
        access_plan=plan, source_version=info['format_info']['source_version'],
        selection=dict(variable='message-1', indices=[], x=3596, y=1798, width=4, height=3))
    assert result['values'] == [['0.0'] * 4 for _ in range(3)]
    assert result['valid'] == [[True] * 4 for _ in range(3)]
    assert result['variable']['message']['time']['increment_value'] == 225
    assert [v['size'] for v in result['variable']['dimensions']] == ['1801', '3600']
print('GRIB2 ECMWF 0.1 degree precipitation smoke passed')

PYGRIB

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
