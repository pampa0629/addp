#!/usr/bin/env python3
"""Owned NFS fixture and independent whole-pixel oracle for proximity."""
from __future__ import annotations
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

SUITE = 'raster-proximity-persistence'
RUNTIME = 'addp-raster-proximity-runtime'
CASES = {
    'metres.cog.tif': ('metres.tif', 2, [0, -3]),
    'feet.cog.tif': ('feet.tif', 1, [0, 4294967295]),
}
TARGETS = tuple(CASES)
SOURCES = ('metres.tif', 'feet.tif')
WIDTH, HEIGHT = 519, 515
GRIDS = {
    'metres.tif': (3857, (100., 2.5, 0., 200., 0., -2.5), 1.),
    'feet.tif': (2277, (3000000., 3., 0., 10000000., 0., -3.), 1200/3937),
}


class FixtureError(RuntimeError):
    pass


def command(args, **kwargs):
    return subprocess.run(args, check=True, capture_output=True, text=True, timeout=120, **kwargs).stdout.strip()


if sys.argv[1:2] != ['--worker']:
    sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
    from scripts.utils.online_nfs_fixture import NFSExport, FixtureError, run_runtime_worker


def guard(env=os.environ):
    return NFSExport('proximity', command).guard(env)


def owned_root(secret):
    return NFSExport('proximity', command).owned_root(secret, os.environ['ADDP_ONLINE_TEST_RUN_ID'])


def digest(path):
    if path.is_symlink() or not path.is_file() or path.stat().st_size <= 0:
        raise FixtureError('expected a positive-size ordinary owned file')
    return {'sha256': hashlib.sha256(path.read_bytes()).hexdigest(), 'size_bytes': path.stat().st_size}


def source_arrays(name):
    import numpy as np
    if name not in SOURCES:raise FixtureError('unknown source')
    feet=name=='feet.tif'
    nodata=4294967294 if feet else -9999
    values=np.full((HEIGHT,WIDTH),42 if feet else 9,dtype=np.uint32 if feet else np.int32)
    values[0,0]=0;values[-1,-1]=4294967295 if feet else -3
    values[200:207,200:207]=nodata
    alpha=None if feet else np.full(values.shape,255,dtype=np.int32)
    mask=np.full(values.shape,255,dtype=np.uint8)
    if feet:
        values[1,1]=4294967295;mask[1,1]=0
    else:
        sites=np.random.default_rng(37).choice(WIDTH*HEIGHT,size=20,replace=False)
        values.flat[sites[::2]]=0;values.flat[sites[1::2]]=-3
        values[0,1]=0;alpha[0,1]=0
        alpha[0,0]=128;alpha[100:103,:]=0;alpha[511,511]=-1
        values[508,510]=-3;mask[508,510]=0
    return values,alpha,mask,nodata


def seed(root):
    import numpy as np
    from osgeo import gdal,osr
    if any((root/name).exists() for name in SOURCES) or list((root/'outputs').iterdir()):
        raise FixtureError('refusing to overwrite existing fixture data')
    for name in SOURCES:
        values,alpha,mask,nodata=source_arrays(name)
        epsg,transform,_=GRIDS[name]
        dataset=gdal.GetDriverByName('GTiff').Create(str(root/name),WIDTH,HEIGHT,1 if alpha is None else 3,
            gdal.GDT_UInt32 if alpha is None else gdal.GDT_Int32)
        crs=osr.SpatialReference();crs.ImportFromEPSG(epsg)
        dataset.SetProjection(crs.ExportToWkt());dataset.SetGeoTransform(transform)
        if alpha is not None:
            dataset.GetRasterBand(1).WriteArray(np.zeros(values.shape,dtype=np.int32))
            dataset.GetRasterBand(3).WriteArray(alpha)
            dataset.GetRasterBand(3).SetColorInterpretation(gdal.GCI_AlphaBand)
        band=dataset.GetRasterBand(1 if alpha is None else 2)
        band.WriteArray(values);band.SetNoDataValue(nodata);band.SetScale(17.);band.SetOffset(500.);band=None
        with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):
            dataset.CreateMaskBand(gdal.GMF_PER_DATASET)
        dataset.GetRasterBand(1).GetMaskBand().WriteArray(mask)
        if dataset.GetFileList()!=[str(root/name)]:raise FixtureError('source must be self-contained')
        dataset=None
    return {'sources':{name:digest(root/name) for name in SOURCES}}


def expected_pixels(name):
    import numpy as np
    if name not in CASES:raise FixtureError('unknown exact proximity target')
    source,_,codes=CASES[name]
    values,alpha,mask,nodata=source_arrays(source)
    valid=(values!=nodata)&(mask!=0)
    if alpha is not None:valid &= alpha>0
    targets=np.argwhere(valid & np.isin(values,codes))
    y,x=np.indices(values.shape,dtype=np.float64)
    expected=np.full(values.shape,np.inf)
    # Independent brute-force distances to every valid target centre: no product
    # transform, envelope, transpose, traversal or GDAL distance implementation.
    for row,column in targets:
        expected=np.minimum(expected,np.hypot(x-column,y-row))
    _,transform,factor=GRIDS[source]
    return np.where(valid,expected*transform[1]*factor,np.nan)


def artifact_expectation(name):
    epsg,transform,_=GRIDS[CASES[name][0]]
    a,b,c,d,e,f=transform
    corners=[(a+b*x+c*y,d+e*x+f*y) for x,y in ((0,0),(WIDTH,0),(0,HEIGHT),(WIDTH,HEIGHT))]
    return {'width':WIDTH,'height':HEIGHT,'band_count':1,'source_crs':f'EPSG:{epsg}','extent_srid':epsg,
        'transform':list(transform),'band_nodata':None,
        'extent':[min(p[0] for p in corners),min(p[1] for p in corners),max(p[0] for p in corners),max(p[1] for p in corners)]}


def verify(root,names):
    import numpy as np
    from osgeo import gdal,osr
    from osgeo_utils.samples.validate_cloud_optimized_geotiff import validate
    if len(set(names))!=len(names) or any(name not in TARGETS for name in names):
        raise FixtureError('invalid exact proximity target inventory')
    if sorted(p.name for p in (root/'outputs').iterdir())!=sorted(names):
        raise FixtureError('unexpected output, sidecar or temporary file')
    result={'sources':{name:digest(root/name) for name in SOURCES},'targets':{}}
    for name in names:
        path=root/'outputs'/name;fact=digest(path);dataset=gdal.Open(str(path))
        epsg,transform,_=GRIDS[CASES[name][0]]
        crs=osr.SpatialReference();crs.ImportFromEPSG(epsg)
        if dataset is None or dataset.GetDriver().ShortName!='GTiff' or dataset.RasterCount!=1:
            raise FixtureError('expected one-band GeoTIFF')
        if (dataset.RasterXSize,dataset.RasterYSize,dataset.GetGeoTransform())!=(WIDTH,HEIGHT,transform):
            raise FixtureError('source grid changed')
        if not dataset.GetProjection():raise FixtureError('source CRS changed')
        actual_crs=osr.SpatialReference();actual_crs.ImportFromWkt(dataset.GetProjection())
        if not actual_crs.IsSame(crs):raise FixtureError('source CRS changed')
        band=dataset.GetRasterBand(1)
        if band.DataType!=gdal.GDT_Float64 or band.GetNoDataValue() is None or not np.isnan(band.GetNoDataValue()):
            raise FixtureError('Float64 or NaN NoData changed')
        if band.GetUnitType()!='metre':raise FixtureError('distance unit must be metre')
        expected=expected_pixels(name);actual=band.ReadAsArray()
        if not np.allclose(actual,expected,rtol=1e-14,atol=1e-14,equal_nan=True):raise FixtureError('complete pixel values differ')
        if not np.array_equal(band.GetMaskBand().ReadAsArray(),np.where(np.isfinite(expected),255,0)):
            raise FixtureError('complete pixel mask differs')
        _,errors,_=validate(dataset,full_check=True)
        if errors:raise FixtureError('COG full layout validation failed')
        if band.GetOverviewCount()<1:raise FixtureError('expected COG overviews for cross-block image')
        if band.GetScale() is not None or band.GetOffset() is not None:raise FixtureError('source scale/offset leaked into output')
        result['targets'][name]={**fact,'cog_valid':True,'has_overviews':True,'distance_unit':'metre',
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
        return NFSExport('proximity',command).lifecycle(action,secret,os.environ['ADDP_ONLINE_TEST_RUN_ID'])
    root=owned_root(secret)
    if action=='seed':
        result=runtime_worker('seed',root);(secret/'proximity-source.json').write_text(json.dumps(result));return result
    if action=='verify':
        result=runtime_worker('verify',root,sys.argv[2:])
        if result['sources']!=json.loads((secret/'proximity-source.json').read_text())['sources']:
            raise FixtureError('source file changed')
        return result
    raise FixtureError('unknown fixture action')


if __name__=='__main__':
    try:
        result=worker(sys.argv[2],Path(sys.argv[3]),sys.argv[4:]) if sys.argv[1:2]==['--worker'] else main(sys.argv[1])
        print(json.dumps(result,sort_keys=True))
    except Exception as error:
        print(f'Proximity fixture failed: {type(error).__name__}: {error}',file=sys.stderr);raise SystemExit(1)
