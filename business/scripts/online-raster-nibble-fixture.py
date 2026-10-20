#!/usr/bin/env python3
"""Owned NFS fixture and independent whole-pixel oracle for nibble."""
from __future__ import annotations
from functools import lru_cache
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

SUITE = 'raster-nibble-persistence'
RUNTIME = 'addp-raster-nibble-runtime'
CASES = {'nibble.cog.tif': [7, 8], 'reclassified-nibble.cog.tif': [7, 8],
    'unchanged.cog.tif': [99]}
SAFE_INTEGER = 2**53-1
TARGETS = tuple(CASES)
SOURCES = ('values.tif',)
TRANSFORM = (100., 2.5, 0., 200., 0., -2.5)
WIDTH, HEIGHT = 519, 515
RECLASSIFICATION = [(0, 0), (10, -3), (20, SAFE_INTEGER-1), (30, SAFE_INTEGER),
    (40, -SAFE_INTEGER), (50, -(SAFE_INTEGER-1)), (100, 7), (101, 8)]



class FixtureError(RuntimeError):
    pass


def command(args, **kwargs):
    return subprocess.run(args, check=True, capture_output=True, text=True, timeout=120, **kwargs).stdout.strip()


if sys.argv[1:2] != ['--worker']:
    sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
    from scripts.utils.online_nfs_fixture import NFSExport, FixtureError, run_runtime_worker


def guard(env=os.environ):
    return NFSExport('nibble', command).guard(env)


def owned_root(secret):
    return NFSExport('nibble', command).owned_root(secret, os.environ['ADDP_ONLINE_TEST_RUN_ID'])


def digest(path):
    if path.is_symlink() or not path.is_file() or path.stat().st_size <= 0:
        raise FixtureError('expected a positive-size ordinary owned file')
    return {'sha256': hashlib.sha256(path.read_bytes()).hexdigest(), 'size_bytes': path.stat().st_size}


def point(x, y):
    a,b,c,d,e,f = TRANSFORM
    return a+b*x+c*y, d+e*x+f*y


def source_arrays():
    import numpy as np
    y,x = np.indices((HEIGHT,WIDTH))
    values=np.where((x+2*y)%2,7.,8.)
    # Four equal-distance donors around a non-donor centre: source row wins,
    # rather than smaller class or retaining the centre's original class.
    for row,column,value in [(10,12,0),(12,10,-3),(12,14,SAFE_INTEGER),(14,12,SAFE_INTEGER-1),
        (30,30,SAFE_INTEGER),(30,34,-SAFE_INTEGER),(0,0,-SAFE_INTEGER),
        (HEIGHT-1,WIDTH-1,SAFE_INTEGER-1),(HEIGHT-1,0,-(SAFE_INTEGER-1)),(0,WIDTH-1,-3)]:
        values[row,column]=value
    # Sparse original donors bound the independent brute-force oracle's work.
    candidates=np.flatnonzero((x>60)&(y>60))
    sites=np.random.default_rng(17431).choice(candidates,size=20,replace=False)
    labels=np.array([0,-3,SAFE_INTEGER-1,SAFE_INTEGER,-SAFE_INTEGER,-(SAFE_INTEGER-1)],dtype=np.float64)
    values.flat[sites]=labels[np.arange(len(sites))%len(labels)]
    values[0,1] = -9999; values[1,0] = np.nan
    values[508,508] = np.inf; values[510,510] = -np.inf
    values[200:207,200:207] = -9999
    alpha = np.full(values.shape,255.)
    alpha[0,0] = 128; alpha[509,509] = 0;alpha[100:103,:]=0
    alpha[511,511] = np.nan; alpha[512,512] = -1; alpha[507,507] = np.inf
    mask = np.full(values.shape,255,dtype=np.uint8); mask[508,510] = 0
    encoded=np.full(values.shape,100.)
    for raw,label in RECLASSIFICATION:encoded[values==label]=raw
    encoded[~np.isfinite(values)]=values[~np.isfinite(values)]
    encoded[values==-9999]=-9999
    unsafe=values.copy();unsafe[250,250]=float(2**53)
    nodonor=np.zeros(values.shape,dtype=np.float64)
    # Hidden unsafe values must be ignored by classification validation.
    values[509,509]=0.5;values[508,510]=float(2**53)
    return values,alpha,mask,encoded,unsafe,nodonor


def seed(root):
    from osgeo import gdal,osr
    if any((root/name).exists() for name in SOURCES) or list((root/'outputs').iterdir()):
        raise FixtureError('refusing to overwrite existing fixture data')
    values,alpha,mask,encoded,unsafe,nodonor=source_arrays()
    dataset=gdal.GetDriverByName('GTiff').Create(str(root/SOURCES[0]),WIDTH,HEIGHT,5,gdal.GDT_Float64)
    crs=osr.SpatialReference();crs.ImportFromEPSG(3857)
    dataset.SetProjection(crs.ExportToWkt());dataset.SetGeoTransform(TRANSFORM)
    for index,array in enumerate((encoded,values,alpha,unsafe,nodonor),1):
        band=dataset.GetRasterBand(index);band.SetNoDataValue(-9999);band.WriteArray(array)
        band.SetScale(17.);band.SetOffset(500.);band=None
    dataset.GetRasterBand(3).SetColorInterpretation(gdal.GCI_AlphaBand)
    with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):
        dataset.CreateMaskBand(gdal.GMF_PER_DATASET)
    dataset.GetRasterBand(1).GetMaskBand().WriteArray(mask)
    if dataset.GetFileList()!=[str(root/SOURCES[0])]:raise FixtureError('source must be self-contained')
    dataset=None
    return {'sources':{name:digest(root/name) for name in SOURCES}}


@lru_cache(maxsize=3)
def expected_pixels(name):
    import numpy as np
    if name not in CASES:raise FixtureError('unknown exact nibble target')
    values,alpha,mask,encoded,_,_=source_arrays()
    valid=np.isfinite(values)&(values!=-9999)&np.isfinite(alpha)&(alpha>0)&(mask!=0)
    if name=='reclassified-nibble.cog.tif':
        valid=np.isfinite(encoded)&(encoded!=-9999)&np.isfinite(alpha)&(alpha>0)&(mask!=0)
        values=np.full(encoded.shape,np.nan)
        for raw,label in RECLASSIFICATION:values[encoded==raw]=label
    targets=valid & np.isin(values,CASES[name])
    expected=values.copy()
    if targets.any():
        y,x=np.indices(values.shape,dtype=np.int64)
        best=np.full(values.shape,np.iinfo(np.int64).max,dtype=np.int64)
        # Brute-force original donor centres in row-major order. Strictly less
        # retains the first equal distance, without product EDT/nearest helpers.
        donors=np.argwhere(valid & ~targets)
        if not 0<len(donors)<=32:raise FixtureError('independent donor oracle must stay bounded')
        for row,column in donors:
            squared=(y-int(row))**2+(x-int(column))**2
            use=targets & (squared<best)
            expected[use]=values[row,column];best[use]=squared[use]
    expected[~valid]=np.nan
    expected.flags.writeable=False
    return expected


def artifact_expectation():
    corners=[point(x,y) for x,y in ((0,0),(WIDTH,0),(0,HEIGHT),(WIDTH,HEIGHT))]
    return {'width':WIDTH,'height':HEIGHT,'band_count':1,'source_crs':'EPSG:3857','extent_srid':3857,
        'transform':list(TRANSFORM),'band_nodata':None,
        'extent':[min(p[0] for p in corners),min(p[1] for p in corners),max(p[0] for p in corners),max(p[1] for p in corners)]}


def verify(root, names):
    import numpy as np
    from osgeo import gdal, osr
    from osgeo_utils.samples.validate_cloud_optimized_geotiff import validate
    if len(set(names))!=len(names) or any(name not in TARGETS for name in names):
        raise FixtureError('invalid exact nibble target inventory')
    if sorted(p.name for p in (root/'outputs').iterdir())!=sorted(names):
        raise FixtureError('unexpected output, sidecar or temporary file')
    result={'sources':{name:digest(root/name) for name in SOURCES},'targets':{}}
    crs=osr.SpatialReference();crs.ImportFromEPSG(3857)
    for name in names:
        path=root/'outputs'/name;fact=digest(path);dataset=gdal.Open(str(path))
        if dataset is None or dataset.GetDriver().ShortName!='GTiff' or dataset.RasterCount!=1:
            raise FixtureError('expected one-band GeoTIFF')
        if (dataset.RasterXSize,dataset.RasterYSize,dataset.GetGeoTransform())!=(WIDTH,HEIGHT,TRANSFORM):
            raise FixtureError('source grid changed')
        if not dataset.GetProjection():raise FixtureError('source CRS changed')
        actual_crs=osr.SpatialReference();actual_crs.ImportFromWkt(dataset.GetProjection())
        if not actual_crs.IsSame(crs):raise FixtureError('source CRS changed')
        band=dataset.GetRasterBand(1)
        if band.DataType!=gdal.GDT_Float64 or band.GetNoDataValue() is None or not np.isnan(band.GetNoDataValue()):
            raise FixtureError('Float64 or NaN NoData changed')
        if band.GetUnitType() or band.GetColorTable() is not None:raise FixtureError('classification output inherited units or a colour table')
        expected=expected_pixels(name);actual=band.ReadAsArray()
        if not np.array_equal(actual,expected,equal_nan=True):raise FixtureError('complete pixel values differ')
        if not np.array_equal(band.GetMaskBand().ReadAsArray(),np.where(np.isfinite(expected),255,0)):
            raise FixtureError('complete pixel mask differs')
        _,errors,_=validate(dataset,full_check=True)
        if errors:raise FixtureError('COG full layout validation failed')
        if band.GetOverviewCount()<1:raise FixtureError('expected COG overviews for cross-block image')
        if band.GetScale() is not None or band.GetOffset() is not None:raise FixtureError('source scale/offset leaked into output')
        result['targets'][name]={**fact,'cog_valid':True,'has_overviews':band.GetOverviewCount()>0,
            'valid_pixels':int(np.isfinite(expected).sum()),'full_pixel_and_mask_verification':True,
            'exact_integer_pixels_verified':True}
        band=dataset=None
    return result


def worker(action, root, names):
    if action not in {'seed','verify'}:raise FixtureError('unknown worker action')
    if action=='verify' and list(Path(tempfile.gettempdir()).glob('addp-raster-*')):
        raise FixtureError('Runtime raster execution workspace has residuals')
    return seed(root) if action=='seed' else {**verify(root,names),'runtime_workspaces_removed':True}


def runtime_worker(action, root, names=()):
    return run_runtime_worker(command,RUNTIME,SUITE,Path(__file__),action,root,names)


def main(action):
    secret=guard()
    if action in {'start','stop'}:
        return NFSExport('nibble',command).lifecycle(action,secret,os.environ['ADDP_ONLINE_TEST_RUN_ID'])
    root=owned_root(secret)
    if action=='seed':
        result=runtime_worker('seed',root);(secret/'nibble-source.json').write_text(json.dumps(result));return result
    if action=='verify':
        result=runtime_worker('verify',root,sys.argv[2:])
        if result['sources']!=json.loads((secret/'nibble-source.json').read_text())['sources']:
            raise FixtureError('source file changed')
        return result
    raise FixtureError('unknown fixture action')


if __name__=='__main__':
    try:
        result=worker(sys.argv[2],Path(sys.argv[3]),sys.argv[4:]) if sys.argv[1:2]==['--worker'] else main(sys.argv[1])
        print(json.dumps(result,sort_keys=True))
    except Exception as error:
        print(f'Nibble fixture failed: {type(error).__name__}: {error}',file=sys.stderr);raise SystemExit(1)
