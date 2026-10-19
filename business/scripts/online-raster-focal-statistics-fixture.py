#!/usr/bin/env python3
"""Owned NFS fixture and independent whole-pixel oracle for focal statistics."""
from __future__ import annotations
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

SUITE = 'raster-focal-statistics-persistence'
RUNTIME = 'addp-raster-focal-statistics-runtime'
CASES = {
    'sum-3.cog.tif': (3, 'sum'), 'min-3.cog.tif': (3, 'min'),
    'mean-5.cog.tif': (5, 'mean'), 'max-5.cog.tif': (5, 'max'),
    'stddev-5.cog.tif': (5, 'stddev'), 'reclassified-stddev-3.cog.tif': (3, 'stddev'),
}
TARGETS = tuple(CASES)
SOURCES = ('values.tif',)
TRANSFORM = (110., 1/1024, 1/8192, 30., 1/4096, -1/512)
WIDTH, HEIGHT = 519, 515



class FixtureError(RuntimeError):
    pass


def command(args, **kwargs):
    return subprocess.run(args, check=True, capture_output=True, text=True, timeout=120, **kwargs).stdout.strip()


if sys.argv[1:2] != ['--worker']:
    sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
    from scripts.utils.online_nfs_fixture import NFSExport, FixtureError, run_runtime_worker


def guard(env=os.environ):
    return NFSExport('focal-statistics', command).guard(env)


def owned_root(secret):
    return NFSExport('focal-statistics', command).owned_root(secret, os.environ['ADDP_ONLINE_TEST_RUN_ID'])


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
    values = x+2.*y-17
    values[0,1] = -9999; values[1,0] = np.nan
    values[508,508] = np.inf; values[510,510] = -np.inf
    values[200:207,200:207] = -9999
    alpha = np.full(values.shape,255.)
    alpha[0,0] = 128; alpha[509,509] = 0
    alpha[511,511] = np.nan; alpha[512,512] = -1; alpha[507,507] = np.inf
    mask = np.full(values.shape,255,dtype=np.uint8); mask[508,510] = 0
    return values,alpha,mask


def seed(root):
    import numpy as np
    from osgeo import gdal,osr
    if any((root/name).exists() for name in SOURCES) or list((root/'outputs').iterdir()):
        raise FixtureError('refusing to overwrite existing fixture data')
    values,alpha,mask=source_arrays()
    dataset=gdal.GetDriverByName('GTiff').Create(str(root/SOURCES[0]),WIDTH,HEIGHT,3,gdal.GDT_Float64)
    crs=osr.SpatialReference();crs.ImportFromEPSG(4326)
    dataset.SetProjection(crs.ExportToWkt());dataset.SetGeoTransform(TRANSFORM)
    dataset.GetRasterBand(1).WriteArray(np.full(values.shape,np.nan))
    band=dataset.GetRasterBand(2);band.WriteArray(values);band.SetNoDataValue(-9999)
    band.SetScale(17.);band.SetOffset(500.);band=None
    dataset.GetRasterBand(3).WriteArray(alpha)
    dataset.GetRasterBand(3).SetColorInterpretation(gdal.GCI_AlphaBand)
    with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):
        dataset.CreateMaskBand(gdal.GMF_PER_DATASET)
    dataset.GetRasterBand(1).GetMaskBand().WriteArray(mask)
    if dataset.GetFileList()!=[str(root/SOURCES[0])]:raise FixtureError('source must be self-contained')
    dataset=None
    return {'sources':{name:digest(root/name) for name in SOURCES}}


def expected_pixels(name):
    import numpy as np
    if name not in CASES:raise FixtureError('unknown exact focal statistics target')
    values,alpha,mask=source_arrays()
    valid=np.isfinite(values)&(values!=-9999)&np.isfinite(alpha)&(alpha>0)&(mask!=0)
    if name=='reclassified-stddev-3.cog.tif':
        values=np.where(values<512,-3.,np.where(values<1024,0.,7.))
    window,method=CASES[name];radius=window//2
    # Independent full-window NumPy reductions, no product code, GDAL analytics,
    # halo traversal, normalized moments or compensated summation implementation.
    padded=np.pad(np.where(valid,values,np.nan),radius,constant_values=np.nan)
    windows=np.lib.stride_tricks.sliding_window_view(padded,(window,window))
    samples=windows[valid].reshape(-1,window*window)
    operation={'sum':np.nansum,'min':np.nanmin,'mean':np.nanmean,'max':np.nanmax,'stddev':np.nanstd}[method]
    expected=np.full(values.shape,np.nan)
    expected[valid]=operation(samples,axis=1)
    return expected


def artifact_expectation():
    corners=[point(x,y) for x,y in ((0,0),(WIDTH,0),(0,HEIGHT),(WIDTH,HEIGHT))]
    return {'width':WIDTH,'height':HEIGHT,'band_count':1,'source_crs':'EPSG:4326','extent_srid':4326,
        'transform':list(TRANSFORM),'band_nodata':None,
        'extent':[min(p[0] for p in corners),min(p[1] for p in corners),max(p[0] for p in corners),max(p[1] for p in corners)]}


def verify(root, names):
    import numpy as np
    from osgeo import gdal, osr
    from osgeo_utils.samples.validate_cloud_optimized_geotiff import validate
    if len(set(names))!=len(names) or any(name not in TARGETS for name in names):
        raise FixtureError('invalid exact focal target inventory')
    if sorted(p.name for p in (root/'outputs').iterdir())!=sorted(names):
        raise FixtureError('unexpected output, sidecar or temporary file')
    result={'sources':{name:digest(root/name) for name in SOURCES},'targets':{}}
    crs=osr.SpatialReference();crs.ImportFromEPSG(4326)
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
        expected=expected_pixels(name);actual=band.ReadAsArray()
        if not np.allclose(actual,expected,rtol=1e-12,atol=1e-12,equal_nan=True):raise FixtureError('complete pixel values differ')
        if not np.array_equal(band.GetMaskBand().ReadAsArray(),np.where(np.isfinite(expected),255,0)):
            raise FixtureError('complete pixel mask differs')
        _,errors,_=validate(dataset,full_check=True)
        if errors:raise FixtureError('COG full layout validation failed')
        if band.GetOverviewCount()<1:raise FixtureError('expected COG overviews for cross-block image')
        if band.GetScale() is not None or band.GetOffset() is not None:raise FixtureError('source scale/offset leaked into output')
        result['targets'][name]={**fact,'cog_valid':True,'has_overviews':band.GetOverviewCount()>0,
            'valid_pixels':int(np.isfinite(expected).sum()),'full_pixel_and_mask_verification':True}
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
        return NFSExport('focal-statistics',command).lifecycle(action,secret,os.environ['ADDP_ONLINE_TEST_RUN_ID'])
    root=owned_root(secret)
    if action=='seed':
        result=runtime_worker('seed',root);(secret/'focal-statistics-source.json').write_text(json.dumps(result));return result
    if action=='verify':
        result=runtime_worker('verify',root,sys.argv[2:])
        if result['sources']!=json.loads((secret/'focal-statistics-source.json').read_text())['sources']:
            raise FixtureError('source file changed')
        return result
    raise FixtureError('unknown fixture action')


if __name__=='__main__':
    try:
        result=worker(sys.argv[2],Path(sys.argv[3]),sys.argv[4:]) if sys.argv[1:2]==['--worker'] else main(sys.argv[1])
        print(json.dumps(result,sort_keys=True))
    except Exception as error:
        print(f'Focal fixture failed: {type(error).__name__}: {error}',file=sys.stderr);raise SystemExit(1)
