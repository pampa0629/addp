#!/usr/bin/env python3
"""Owned raster pairs and independent complete zonal CSV verification."""
from __future__ import annotations
import csv
from decimal import Decimal, InvalidOperation
from fractions import Fraction
import hashlib
import json
import math
import os
from pathlib import Path
import re
import shutil
import statistics
import subprocess
import sys
import tempfile

SUITE = 'raster-zonal-percentiles-persistence'
PREFIX = 'zonal-percentiles'
RUNTIME = 'addp-raster-zonal-percentiles-runtime'
WIDTH, HEIGHT = 1025, 513
GRID = (100., 2., .25, 200., .125, -3.)
ORACLE = '.zonal-percentiles-oracle.json'
BASE_COLUMNS = ('zone_value', 'total_count', 'valid_count', 'invalid_count', 'sum', 'min', 'max', 'mean', 'stddev')
PERCENTILES=(0.,25.,33.3,50.,75.,100.)
CASES = {
 'original.csv':('base-values.tif','base-zones.tif',None,2),
 'batch.csv':('base-values.tif','base-zones.tif',(0.,25.,33.3,50.,75.,100.),2),
 'p0.csv':('base-values.tif','base-zones.tif',(0.,),2),
 'p25.csv':('base-values.tif','base-zones.tif',(25.,),2),
 'p33.csv':('base-values.tif','base-zones.tif',(33.3,),2),
 'p50.csv':('base-values.tif','base-zones.tif',(50.,),2),
 'p75.csv':('base-values.tif','base-zones.tif',(75.,),2),
 'p100.csv':('base-values.tif','base-zones.tif',(100.,),2),
 'reclassified.csv':('base-values.tif','base-zones.tif',(0.,25.,33.3,50.,75.,100.),2),
 'many-zones.csv':('large-values.tif','large-zones.tif',(0.,25.,33.3,50.,75.,100.),2),
 'reversed.csv':('reversed-values.tif','reversed-zones.tif',(0.,25.,33.3,50.,75.,100.),2),
 'int64.csv':('int64.tif','native-zones.tif',(0.,25.,33.3,50.,75.,100.),2),
 'uint64.csv':('uint64.tif','native-zones.tif',(0.,25.,33.3,50.,75.,100.),2),
 'coverage.csv':('coverage-values.tif','coverage-zones.tif',(0.,25.,33.3,50.,75.,100.),2),
 'invalid-values.csv':('invalid-values.tif','base-zones.tif',(0.,25.,33.3,50.,75.,100.),2),
 'empty-zones.csv':('base-values.tif','empty-zones.tif',(0.,25.,33.3,50.,75.,100.),2),
}
FAILURES=('list_null','list_empty','scalar_number','scalar_boolean','scalar_string','scalar_object',
 'item_null','item_boolean','item_string','item_list','item_object','item_low','item_high','duplicate','descending',
 'values_band_zero','values_band_high','values_band_boolean','values_band_string',
 'zones_band_zero','zones_band_high','zones_band_boolean','zones_band_string',
 'values_alpha','zones_alpha','values_complex','zones_complex','fractional_zone','overflow_zone',
 'missing_crs','different_crs','missing_grid','singular_grid','different_size','shifted_grid','rotated_grid',
 'weights_conflict','weighted_stddev_conflict','frequency_conflict','frequency_null_conflict')
GRIDS=('missing_crs','different_crs','missing_grid','singular_grid','different_size','shifted_grid','rotated_grid')
SOURCES=tuple(dict.fromkeys([n for case in CASES.values()for n in case[:2]]+['complex.tif','fractional-zone.tif','overflow-zone.tif','weights.tif']+[n+'.tif'for n in GRIDS]))
CONTROLS=('p0.csv','p25.csv','p33.csv','p50.csv','p75.csv','p100.csv')

class FixtureError(RuntimeError): pass

def command(args, **kwargs):
    return subprocess.run(args, check=True, capture_output=True, text=True, timeout=120, **kwargs).stdout.strip()

if sys.argv[1:2] != ['--worker']:
    sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
    from scripts.utils.online_nfs_fixture import NFSExport, FixtureError, run_runtime_worker

def guard(env=os.environ): return NFSExport(PREFIX, command).guard(env)
def owned_root(secret): return NFSExport(PREFIX, command).owned_root(secret, os.environ['ADDP_ONLINE_TEST_RUN_ID'])
def encoded(value): return (json.dumps(value, allow_nan=False, sort_keys=True)+'\n').encode()
def digest(path):
    if path.is_symlink() or not path.is_file() or path.stat().st_size <= 0: raise FixtureError('expected a positive ordinary file')
    return {'size_bytes': path.stat().st_size, 'sha256': hashlib.sha256(path.read_bytes()).hexdigest()}

def source_arrays(name):
    import numpy as np
    if name not in SOURCES:raise FixtureError('unknown exact source')
    dtype,nodata,crs,grid='Float64',-9999.,3857,GRID
    values=np.array([[-8.,0.,5.,11.,nodata,nodata,nodata,nodata],[-8.,0.,5.,nodata,nodata,nodata,nodata,nodata],[0.,nodata,nodata,nodata,nodata,nodata,nodata,nodata],[nodata]*8])
    classes=np.repeat(np.array([[0.],[-3.],[7.],[11.]]),8,axis=1)
    mask=np.full(values.shape,255,np.uint8);alpha=np.full(values.shape,128.);first=np.full(values.shape,nodata)
    if name.startswith('large-'):
        i=np.arange(WIDTH*HEIGHT,dtype=np.int64).reshape(HEIGHT,WIDTH)
        values=((i*37)%997-490).astype(float)/4;classes=(i%79-39).astype(float)
        values[i%257==4]=nodata;values[i%263==5]=np.inf
        mask=np.where(i%167==6,0,255).astype(np.uint8);alpha=np.where(i%179==7,0.,128.)
        if name=='large-zones.tif':values=classes;values[i%211==3]=nodata;mask=np.where(i%173==1,0,255).astype(np.uint8);alpha=np.where(i%181==2,0.,128.)
        first=np.full(values.shape,nodata)
    elif name in('int64.tif','uint64.tif'):
        dtype='Int64'if name=='int64.tif'else'UInt64';nodata=2**63-1 if dtype=='Int64'else 2**64-1
        values=np.tile(np.array([[nodata,nodata-1,0,12,6,2,8,4]],dtype=dtype.lower()),(4,1));first=np.zeros(values.shape,dtype=values.dtype);alpha=alpha.astype(values.dtype)
    elif name=='native-zones.tif':
        dtype='Int64';nodata=-9999;values=np.tile(np.array([[2**63-1]*4+[2**63-2]*2+[0]*2],dtype=np.int64),(4,1));first=np.zeros(values.shape,np.int64);alpha=alpha.astype(np.int64)
    elif name=='complex.tif':dtype='CFloat64';values=np.full(values.shape,1+2j);alpha=alpha.astype(complex)
    elif name in('base-zones.tif','reversed-zones.tif','coverage-zones.tif','empty-zones.tif','fractional-zone.tif','overflow-zone.tif')or name.endswith('_grid.tif')or name[:-4]in GRIDS:
        values=classes.copy()
        if name=='empty-zones.tif':values[:]=nodata
        if name=='fractional-zone.tif':values[:]=.5
        if name=='overflow-zone.tif':dtype='UInt64';nodata=2**64-1;values=np.full(values.shape,2**63,np.uint64);first=np.zeros(values.shape,np.uint64);alpha=alpha.astype(np.uint64)
        if name=='coverage-zones.tif':mask[0,0]=0;alpha[1,1]=0;alpha[2,1]=np.nan
        if name=='missing_crs.tif':crs=None
        if name=='different_crs.tif':crs=4326
        if name=='missing_grid.tif':grid=None
        if name=='singular_grid.tif':grid=(100.,1.,2.,200.,2.,4.)
        if name=='shifted_grid.tif':grid=(101.,*GRID[1:])
        if name=='rotated_grid.tif':grid=(*GRID[:2],.5,*GRID[3:])
        if name=='different_size.tif':values,first,mask,alpha=[a[:,:-1]for a in(values,first,mask,alpha)]
    elif name=='invalid-values.tif':values[:]=nodata
    elif name=='weights.tif':values[:]=1.
    elif name=='coverage-values.tif':
        values[0,4:]=[np.inf,np.nan,nodata,0.];mask[0,1]=0;alpha[0,2]=0;alpha[1,0]=np.nan;alpha[2,2]=255.
    data=np.stack([first.astype(values.dtype),values,alpha.astype(values.dtype)])
    if name.startswith('reversed-'):data,mask=data[:,::-1,::-1].copy(),mask[::-1,::-1].copy()
    return data,mask,dtype,nodata,crs,grid

def seed(root):
    from osgeo import gdal, osr
    if any((root/name).exists() for name in (*SOURCES, ORACLE)) or list((root/'outputs').iterdir()): raise FixtureError('refusing to overwrite source or previous result')
    for name in SOURCES:
        data, mask, dtype, nodata, crs, grid = source_arrays(name)
        ds = gdal.GetDriverByName('GTiff').Create(str(root/name), data.shape[2], data.shape[1], 3, getattr(gdal, 'GDT_'+dtype), options=['COMPRESS=DEFLATE'])
        if crs:
            reference = osr.SpatialReference(); reference.ImportFromEPSG(crs); ds.SetProjection(reference.ExportToWkt())
        if grid: ds.SetGeoTransform(grid)
        for index, values in enumerate(data, 1):
            band = ds.GetRasterBand(index); band.WriteArray(values)
            if dtype == 'Int64': band.SetNoDataValueAsInt64(nodata)
            elif dtype == 'UInt64': band.SetNoDataValueAsUInt64(nodata)
            else: band.SetNoDataValue(nodata)
            band.SetScale(17.); band.SetOffset(500.)
        ds.GetRasterBand(3).SetColorInterpretation(gdal.GCI_AlphaBand)
        with gdal.config_option('GDAL_TIFF_INTERNAL_MASK', 'YES'): ds.CreateMaskBand(gdal.GMF_PER_DATASET)
        ds.GetRasterBand(1).GetMaskBand().WriteArray(mask); band = ds = None
    return {'sources': {name: digest(root/name) for name in SOURCES}}

def class_rules(): return [{'value': value, 'class': 10 if value in (-3, 0) else 20 if value == 2 else 30} for value in (-3, 0, 2, 7, 11)]
def columns(name):return list(BASE_COLUMNS)+[k for i in range(1,len(CASES[name][2]or())+1)for k in(f'percentile_{i}',f'percentile_value_{i}')]

def groups(case, reclassified=False):
    import numpy as np
    value_source, zone_source, _, zone_band = case
    data, mask, _, nodata, _, _ = source_arrays(value_source)
    zones, zone_mask, _, zone_nodata, _, _ = source_arrays(zone_source)
    values, classes = data[1], zones[zone_band-1]
    covered = (classes != zone_nodata) & np.isfinite(classes) & (zone_mask != 0) & np.isfinite(zones[2]) & (zones[2] > 0)
    valid = (values != nodata) & np.isfinite(values) & (mask != 0) & np.isfinite(data[2]) & (data[2] > 0)
    counts, samples = {}, {}
    for raw, value, accepted in zip(classes[covered], values[covered], valid[covered]):
        code = int(raw)
        if reclassified: code = 10 if code in (-3, 0) else 20 if code == 2 else 30
        counts[code] = counts.get(code, 0)+1
        if accepted: samples.setdefault(code, []).append(float(value))
    return [(code, counts[code], sorted(samples.get(code, []))) for code in sorted(counts)]

def oracle():
    populations, results = {}, {}
    for name, case in CASES.items():
        key = (case[0], case[1], case[3], name == 'reclassified.csv')
        if key not in populations: populations[key] = groups(case, key[-1])
        rows = []
        for code, total, values in populations[key]:
            count = len(values)
            stats = [math.fsum(values), values[0], values[-1], statistics.fmean(values), statistics.pstdev(values)] if count else [None]*5
            row = dict(zip(BASE_COLUMNS, [code, total, count, total-count, *stats]))
            for index,p in enumerate(case[2]or(),1):
                p=float(p);value=None
                if count:
                    position=(count-1)*Fraction(p)/100;low=position.numerator//position.denominator;weight=position-low
                    value=float(Fraction(values[low])*(1-weight)+Fraction(values[low+bool(weight)])*weight)
                row[f'percentile_{index}']=p;row[f'percentile_value_{index}']=value
            rows.append(row)
        results[name] = rows
    return results

def validate_records(rows, expected, name):
    if len(rows) != len(expected): raise FixtureError('complete CSV row count differs')
    for actual, wanted in zip(rows, expected):
        if set(actual) != set(columns(name)): raise FixtureError('complete CSV columns differ')
        for key, target in wanted.items():
            value = actual[key]
            if target is None:
                if value is not None: raise FixtureError('null group was replaced')
            elif key in BASE_COLUMNS[:4]:
                if type(value) is not int or value != target: raise FixtureError('exact Int64 class or count differs')
            elif type(value) not in (int, float) or not math.isfinite(value): raise FixtureError('nonfinite or nonnumeric statistic')
            elif key.startswith('percentile_')and not key.startswith('percentile_value_')or key in('min','max'):
                if value!=target:raise FixtureError('exact percentile or endpoint differs')
            elif key.startswith('percentile_value_'):
                p=CASES[name][2][int(key.rsplit('_',1)[1])-1]
                if target==0 or p in(0,50,100):
                    if value!=target:raise FixtureError('exact percentile or endpoint differs')
                elif not math.isclose(value,target,rel_tol=4e-15,abs_tol=0.):raise FixtureError('independent complete percentile differs')
            elif not math.isclose(value,target,rel_tol=1e-12,abs_tol=1e-12):raise FixtureError('independent complete statistic differs')

def read_csv(path, name):
    with path.open(newline='', encoding='utf-8') as stream:
        reader = csv.DictReader(stream)
        if reader.fieldnames != columns(name): raise FixtureError('CSV schema differs')
        rows = []
        for record in reader:
            if set(record) != set(columns(name)) or any(value is None for value in record.values()): raise FixtureError('malformed CSV record')
            row = {}
            for key, text in record.items():
                if key in BASE_COLUMNS[:4]:
                    if re.fullmatch(r'-?(0|[1-9][0-9]*)', text) is None: raise FixtureError('CSV Int64 must be an exact integer')
                    row[key] = int(text)
                elif text == '': row[key] = None
                else:
                    try: number = Decimal(text)
                    except InvalidOperation: raise FixtureError('CSV number malformed') from None
                    if not number.is_finite(): raise FixtureError('CSV number nonfinite')
                    row[key] = float(number)
            rows.append(row)
    return rows

def verify(root, names, expectations):
    import numpy as np
    from osgeo import gdal, osr
    if len(set(names)) != len(names) or any(name not in CASES for name in names): raise FixtureError('unexpected target names')
    if set(p.name for p in root.iterdir()) != {*SOURCES, 'outputs', '.addp-online-owner', ORACLE}: raise FixtureError('unexpected source sidecar or private file')
    if set(p.name for p in (root/'outputs').iterdir()) != set(names): raise FixtureError('unexpected output or failed target')
    result = {'sources': {}, 'targets': {}, 'full_csv_checks': len(names), 'full_source_pixels_verified': True}
    for name in SOURCES:
        data, mask, dtype, nodata, crs, grid = source_arrays(name); ds = gdal.Open(str(root/name)); fact = digest(root/name)
        if (ds is None or ds.GetDriver().ShortName != 'GTiff' or ds.RasterCount != 3 or (ds.RasterYSize, ds.RasterXSize) != mask.shape
            or ds.GetFileList() != [str(root/name)] or ds.GetGeoTransform(can_return_null=True) != grid): raise FixtureError('source dimensions/grid/inventory changed')
        if crs:
            reference = osr.SpatialReference(); reference.ImportFromEPSG(crs)
            if not ds.GetProjection() or not osr.SpatialReference(wkt=ds.GetProjection()).IsSame(reference): raise FixtureError('source CRS changed')
        elif ds.GetProjection(): raise FixtureError('missing source CRS fabricated')
        for index, values in enumerate(data, 1):
            band = ds.GetRasterBand(index)
            if (band.DataType != getattr(gdal, 'GDT_'+dtype) or band.GetNoDataValue() != nodata or band.GetScale() != 17. or band.GetOffset() != 500.
                or band.GetColorInterpretation() != (gdal.GCI_AlphaBand if index == 3 else gdal.GCI_GrayIndex if index == 1 else gdal.GCI_Undefined)
                or not np.array_equal(band.ReadAsArray(), values, equal_nan=True) or not np.array_equal(band.GetMaskBand().ReadAsArray(), mask)):
                raise FixtureError('complete source pixels/mask/type/NoData/scale changed')
        band = ds = None; result['sources'][name] = fact
    records = {}
    for name in names:
        rows = read_csv(root/'outputs'/name, name); validate_records(rows, expectations[name], name); records[name] = rows
        result['targets'][name] = {**digest(root/'outputs'/name), 'row_count': len(rows), 'schema': columns(name), 'complete_table_verified': True}
    if 'original.csv'in records:
        for name in('batch.csv',*CONTROLS,'reversed.csv'):
            if name in records and [{key:row[key]for key in BASE_COLUMNS}for row in records[name]]!=records['original.csv']:raise FixtureError('requested percentiles changed original nine columns')
    compared=[]
    for index,name in enumerate(CONTROLS,1):
        if 'batch.csv'in records and name in records:
            selected=[{'zone_value':row['zone_value'],'p':row[f'percentile_{index}'],'value':row[f'percentile_value_{index}']}for row in records['batch.csv']]
            single=[{'zone_value':row['zone_value'],'p':row['percentile_1'],'value':row['percentile_value_1']}for row in records[name]]
            if selected!=single:raise FixtureError('actual batch and single percentile CSVs differ')
            compared.append(name)
    if 'reversed.csv'in records and records['reversed.csv']!=records['batch.csv']:raise FixtureError('source order changed percentiles')
    result['actual_single_controls_verified']=compared
    return result

def archive_files(root, artifacts, names, previous, prefix):
    directory = artifacts/prefix; manifest = artifacts/(prefix+'-manifest.json')
    if manifest.exists() or directory.exists():
        if (manifest.is_symlink() or directory.is_symlink() or not manifest.is_file() or not directory.is_dir()
            or json.loads(manifest.read_bytes()) != previous or set(p.name for p in directory.iterdir()) != set(previous)):
            raise FixtureError('previous archive inventory changed')
        for name, receipt in previous.items():
            if digest(directory/name) != {key: receipt[key] for key in ('size_bytes', 'sha256')}: raise FixtureError('previous archived bytes changed')
    elif previous: raise FixtureError('previous archive was deleted')
    else: directory.mkdir()
    if not set(previous) <= set(names): raise FixtureError('archive scope shrank')
    receipts = dict(previous)
    for name in names:
        source = root/name; fact = digest(source)
        wanted = {'path': prefix+'/'+name, **fact}
        if name in previous:
            if previous[name] != wanted: raise FixtureError('source or previous output changed')
        else:
            with source.open('rb') as src, (directory/name).open('xb') as dst: shutil.copyfileobj(src, dst)
            (directory/name).chmod(0o444)
            if digest(directory/name) != fact: raise FixtureError('archived byte identity differs')
            receipts[name] = wanted
    temporary = manifest.with_suffix('.pending')
    with temporary.open('xb') as stream: stream.write(encoded(receipts))
    temporary.replace(manifest)
    return receipts

def oracle_scope(root,script_sha):
 marker=root/'.addp-online-owner'
 if root.is_symlink()or root.resolve()!=root or marker.is_symlink()or not marker.is_file()or not re.fullmatch(PREFIX+r'-[0-9]+-[0-9]+',marker.read_text()):raise FixtureError('invalid owned oracle root/run')
 return {'root':str(root),'run_id':marker.read_text(),'fixture_sha256':script_sha,'source_files':{n:digest(root/n)for n in SOURCES}}

def worker(action, root, names):
    if action == 'seed' and not names: return seed(root)
    if action == 'cleanup-oracle':
        path = root/ORACLE
        if path.is_symlink(): raise FixtureError('oracle symlink')
        path.unlink(missing_ok=True); return {'oracle_cache_removed': not path.exists()}
    if action == 'oracle' and len(names)==1 and re.fullmatch('[0-9a-f]{64}',names[0]):
        expectations = oracle(); raw = encoded({'schema':'addp.zonal-percentiles-oracle/v1','scope':oracle_scope(root,names[0]),'expectations':expectations})
        with (root/ORACLE).open('xb') as stream: stream.write(raw)
        (root/ORACLE).chmod(0o444); anchor = hashlib.sha256(raw).hexdigest(); names=[]
    elif action == 'verify' and len(names)>=2 and all(re.fullmatch('[0-9a-f]{64}',v)for v in names[:2]):
        anchor, script_sha, names = names[0], names[1], names[2:]; path = root/ORACLE
        if path.is_symlink() or not path.is_file() or hashlib.sha256(path.read_bytes()).hexdigest() != anchor: raise FixtureError('oracle cache hash changed')
        envelope = json.loads(path.read_bytes())
        if envelope.get('schema')!='addp.zonal-percentiles-oracle/v1' or envelope.get('scope')!=oracle_scope(root,script_sha):raise FixtureError('oracle root/run/source/script scope changed')
        if path.stat().st_mode & 0o222:raise FixtureError('oracle cache became writable')
        expectations = envelope['expectations']
        if set(expectations) != set(CASES): raise FixtureError('oracle scope changed')
    else: raise FixtureError('unknown oracle action or anchor')
    if list(Path(tempfile.gettempdir()).glob('addp-raster-*')): raise FixtureError('Runtime execution workspace residual')
    result = {**verify(root, names, expectations), 'runtime_workspaces_removed': True, 'oracle_sha256': anchor}
    if action == 'oracle': result['expectations'] = expectations
    return result

def runtime_worker(action, root, names=()): return run_runtime_worker(command, RUNTIME, SUITE, Path(__file__), action, root, names)


def main(action):
    secret = guard()
    if action in {'start', 'stop'}: return NFSExport(PREFIX, command).lifecycle(action, secret, os.environ['ADDP_ONLINE_TEST_RUN_ID'])
    root = owned_root(secret); state = secret/(PREFIX+'-source.json')
    args = sys.argv[2:]; script_sha=hashlib.sha256(Path(__file__).read_bytes()).hexdigest()
    if action=='oracle':args=[script_sha]
    if action == 'verify': args = [os.environ['ADDP_ONLINE_ZONAL_PERCENTILES_ORACLE_SHA256'],script_sha,*args]
    result = runtime_worker(action, root, args)
    if action == 'seed':
        with state.open('xb') as stream: stream.write(encoded(result))
    elif action != 'cleanup-oracle':
        if result['sources'] != json.loads(state.read_bytes())['sources']: raise FixtureError('source file changed')
        artifacts = Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR'])
        history = secret/(PREFIX+'-archives.json'); old = json.loads(history.read_bytes()) if history.exists() else {'sources': {}, 'targets': {}, 'oracle': {}}
        if action != 'oracle' and not history.is_file(): raise FixtureError('source archive history missing')
        result['archived_oracle']=archive_files(root,artifacts,[ORACLE],old['oracle'],PREFIX+'-oracle')
        result['archived_sources'] = archive_files(root, artifacts, SOURCES, old['sources'], 'zonal-percentiles-sources')
        result['archived_csvs'] = archive_files(root/'outputs', artifacts, sys.argv[2:], old['targets'], 'zonal-percentiles-csvs')
        history.write_bytes(encoded({'sources': result['archived_sources'], 'targets': result['archived_csvs'],'oracle':result['archived_oracle']}))
    return result

if __name__ == '__main__':
    try:
        result = worker(sys.argv[2], Path(sys.argv[3]), sys.argv[4:]) if sys.argv[1:2] == ['--worker'] else main(sys.argv[1])
        print(json.dumps(result, allow_nan=False, sort_keys=True))
    except Exception as error:
        print(f'zonal multi-percentile fixture failed: {type(error).__name__}: {error}', file=sys.stderr); raise SystemExit(1)
