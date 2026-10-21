#!/usr/bin/env python3
"""Owned NFS fixture and independent whole-pixel oracle for boundary clean."""
from __future__ import annotations
from collections import Counter
from functools import lru_cache
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

SUITE = 'raster-boundary-clean-persistence'
RUNTIME = 'addp-raster-boundary-clean-runtime'
CASES = {'clean.cog.tif': 2, 'reclassified-clean.cog.tif': 1, 'empty-clean.cog.tif': 6}
SAFE_INTEGER = 2**53-1
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
    return NFSExport('boundary-clean', command).guard(env)


def owned_root(secret):
    return NFSExport('boundary-clean', command).owned_root(secret, os.environ['ADDP_ONLINE_TEST_RUN_ID'])


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
    labels=np.array([-SAFE_INTEGER,-3,0,SAFE_INTEGER-1,SAFE_INTEGER],dtype=np.float64)
    values=labels[(x//19+2*(y//23))%len(labels)]
    # Fine protrusions, isolated cells and notches cross both 510-pixel halo seams.
    values[20:27,20:27]=0; values[23,23]=7
    values[45:52,20:27]=7; values[48,23]=0
    values[508:513,500:519]=np.array([-3.,0.,7.,0.,-3.])[:,None]
    values[0,1] = -9999; values[1,0] = np.nan
    values[508,508] = np.inf; values[510,510] = -np.inf
    values[200:207,200:207] = -9999
    alpha = np.full(values.shape,255.)
    alpha[0,0] = 128; alpha[509,509] = 0
    alpha[511,511] = np.nan; alpha[512,512] = -1; alpha[507,507] = np.inf
    mask = np.full(values.shape,255,dtype=np.uint8); mask[508,510] = 0
    valid=np.isfinite(values)&(values!=-9999)&np.isfinite(alpha)&(alpha>0)&(mask!=0)
    # Four exact global count ties, including adjacent high-precision classes;
    # zero is the globally dominant class, regardless of its numeric code.
    for label in labels[labels!=0]:
        sites=np.argwhere(valid&(values==label))
        if len(sites)<40000:raise FixtureError('insufficient deterministic tie population')
        excess=sites[40000:];values[excess[:,0],excess[:,1]]=0
    continuous=x+2.*y-17
    continuous[~np.isfinite(values)]=values[~np.isfinite(values)]
    continuous[values==-9999]=-9999
    fractional=values.copy();fractional[250,250]=0.5
    overflow=values.copy();overflow[250,250]=float(2**53)
    # Hidden unsafe values must be ignored by classification validation.
    values[509,509]=0.5;values[508,510]=float(2**53)
    return values,alpha,mask,continuous,fractional,overflow


def seed(root):
    import numpy as np
    from osgeo import gdal,osr
    if any((root/name).exists() for name in SOURCES) or list((root/'outputs').iterdir()):
        raise FixtureError('refusing to overwrite existing fixture data')
    values,alpha,mask,continuous,fractional,overflow=source_arrays()
    dataset=gdal.GetDriverByName('GTiff').Create(str(root/SOURCES[0]),WIDTH,HEIGHT,6,gdal.GDT_Float64)
    crs=osr.SpatialReference();crs.ImportFromEPSG(4326)
    dataset.SetProjection(crs.ExportToWkt());dataset.SetGeoTransform(TRANSFORM)
    for index,array in enumerate((continuous,values,alpha,fractional,overflow,np.full(values.shape,-9999.)),1):
        band=dataset.GetRasterBand(index);band.WriteArray(array);band.SetNoDataValue(-9999)
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
    if name not in CASES:raise FixtureError('unknown exact boundary clean target')
    values,alpha,mask,continuous,_,_=source_arrays()
    valid=np.isfinite(values)&(values!=-9999)&np.isfinite(alpha)&(alpha>0)&(mask!=0)
    if name=='reclassified-clean.cog.tif':
        valid=np.isfinite(continuous)&(continuous!=-9999)&np.isfinite(alpha)&(alpha>0)&(mask!=0)
        values=np.where(continuous<512,-3.,np.where(continuous<1024,0.,7.))
    if name=='empty-clean.cog.tif':valid[:]=False
    # Scalar whole-image Counter and immutable stages share no product rank,
    # searchsorted, block/halo traversal or intermediate-file implementation.
    counts=Counter(int(value) for value in values[valid])
    def neighbours(y,x):
        return ((ny,nx) for ny in range(max(0,y-1),min(HEIGHT,y+2))
                for nx in range(max(0,x-1),min(WIDTH,x+2)) if valid[ny,nx])
    expanded=np.full(values.shape,np.nan);expected=expanded.copy()
    for y,x in zip(*np.nonzero(valid)):
        choices=[int(values[ny,nx]) for ny,nx in neighbours(y,x)]
        expanded[y,x]=max(choices,key=lambda value:(counts[value],-value))
    for y,x in zip(*np.nonzero(valid)):
        original=values[y,x]
        expected[y,x]=original if any(expanded[ny,nx]==original for ny,nx in neighbours(y,x)) else expanded[y,x]
    expected.flags.writeable=False
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
        raise FixtureError('invalid exact boundary clean target inventory')
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
        if not np.array_equal(actual,expected,equal_nan=True):raise FixtureError('complete pixel values differ')
        if not np.array_equal(band.GetMaskBand().ReadAsArray(),np.where(np.isfinite(expected),255,0)):
            raise FixtureError('complete pixel mask differs')
        _,errors,_=validate(dataset,full_check=True)
        if errors:raise FixtureError('COG full layout validation failed')
        if band.GetOverviewCount()<1:raise FixtureError('expected COG overviews for cross-block image')
        if band.GetColorTable() is not None:raise FixtureError('unexpected classification color table')
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
        return NFSExport('boundary-clean',command).lifecycle(action,secret,os.environ['ADDP_ONLINE_TEST_RUN_ID'])
    root=owned_root(secret)
    if action=='seed':
        result=runtime_worker('seed',root);(secret/'boundary-clean-source.json').write_text(json.dumps(result));return result
    if action=='verify':
        result=runtime_worker('verify',root,sys.argv[2:])
        if result['sources']!=json.loads((secret/'boundary-clean-source.json').read_text())['sources']:
            raise FixtureError('source file changed')
        return result
    raise FixtureError('unknown fixture action')


if __name__=='__main__':
    try:
        result=worker(sys.argv[2],Path(sys.argv[3]),sys.argv[4:]) if sys.argv[1:2]==['--worker'] else main(sys.argv[1])
        print(json.dumps(result,sort_keys=True))
    except Exception as error:
        print(f'Boundary clean fixture failed: {type(error).__name__}: {error}',file=sys.stderr);raise SystemExit(1)
