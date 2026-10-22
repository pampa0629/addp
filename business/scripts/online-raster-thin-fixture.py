#!/usr/bin/env python3
"""Owned NFS fixture and independent whole-pixel oracle for raster thinning."""
from __future__ import annotations
from functools import lru_cache
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

SUITE = 'raster-thin-persistence'
RUNTIME = 'addp-raster-thin-runtime'
CASES = {'thin.cog.tif': 2, 'reclassified-thin.cog.tif': 1, 'empty-thin.cog.tif': 6}
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
    return NFSExport('thin', command).guard(env)


def owned_root(secret):
    return NFSExport('thin', command).owned_root(secret, os.environ['ADDP_ONLINE_TEST_RUN_ID'])


def digest(path):
    if path.is_symlink() or not path.is_file() or path.stat().st_size <= 0:
        raise FixtureError('expected a positive-size ordinary owned file')
    return {'sha256': hashlib.sha256(path.read_bytes()).hexdigest(), 'size_bytes': path.stat().st_size}


def point(x, y):
    a,b,c,d,e,f = TRANSFORM
    return a+b*x+c*y, d+e*x+f*y


def source_arrays():
    import numpy as np
    y,x=np.indices((HEIGHT,WIDTH))
    labels=np.array([-SAFE_INTEGER,-3,SAFE_INTEGER-1,SAFE_INTEGER],dtype=np.float64)
    values=labels[(x//19+2*(y//23))%len(labels)]
    # A single explicit zero class: thick lines across both 510/512 seams,
    # ring, T/Y junctions, diagonal, isolated cell, border and small square.
    values[505:512,:]=0;values[:,505:512]=0
    values[30:70,30:75]=0;values[35:65,35:70]=-3
    values[218:225,180:260]=0;values[218:280,216:223]=0
    values[310:370,316:323]=0
    for row in range(285,320):
        values[row,row-4:row+3]=0;values[row,635-row:642-row]=0
    for row in range(100,170):values[row,row-2:row+3]=0
    values[80,80]=0;values[85:87,85:87]=0
    values[0:5,0:20]=0;values[0:20,0:5]=0
    values[0,1]=-9999;values[1,0]=np.nan
    values[508,508]=np.inf;values[510,510]=-np.inf
    values[200:207,200:207]=-9999
    alpha=np.full(values.shape,255.)
    alpha[0,0]=128;alpha[509,509]=0;alpha[511,511]=np.nan
    alpha[512,512]=-1;alpha[507,507]=np.inf
    mask=np.full(values.shape,255,dtype=np.uint8);mask[508,510]=0
    # Non-integral raw data is legal for reclassification, not direct thinning.
    continuous=np.where(values==0,1.5,-2.5)
    continuous[~np.isfinite(values)]=values[~np.isfinite(values)]
    continuous[values==-9999]=-9999
    fractional=values.copy();fractional[250,250]=0.5
    overflow=values.copy();overflow[250,250]=float(2**53)
    # Classification validation must ignore unsafe values hidden by validity.
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
    if name not in CASES:raise FixtureError('unknown exact thinning target')
    values,alpha,mask,continuous,_,_=source_arrays()
    valid=np.isfinite(values)&(values!=-9999)&np.isfinite(alpha)&(alpha>0)&(mask!=0)
    foreground=0
    if name=='reclassified-thin.cog.tif':
        valid=np.isfinite(continuous)&(continuous!=-9999)&np.isfinite(alpha)&(alpha>0)&(mask!=0)
        values=np.where(continuous>=1,SAFE_INTEGER,-3.)
        foreground=SAFE_INTEGER
    if name=='empty-thin.cog.tif':valid[:]=False
    image=valid&(values==foreground)
    # Independent whole-image scalar oracle; no product imports, NumPy stencil,
    # halo traversal or scratch files. Neighbours are N, NE, E, SE, S, SW, W, NW.
    offsets=((-1,0),(-1,1),(0,1),(1,1),(1,0),(1,-1),(0,-1),(-1,-1))
    while True:
        previous_count=int(image.sum())
        for phase in (0,1):
            snapshot=np.pad(image,1);result=image.copy()
            for row,col in np.argwhere(image):
                n,ne,e,se,s,sw,w,nw=[bool(snapshot[row+1+dy,col+1+dx]) for dy,dx in offsets]
                transitions=sum((not a) and (b or c) for a,b,c in ((e,ne,n),(n,nw,w),(w,sw,s),(s,se,e)))
                adjacent_a=sum((e or ne,n or nw,w or sw,s or se))
                adjacent_b=sum((ne or n,nw or w,sw or s,se or e))
                protected=((ne or n or not se) and e) if phase==0 else ((sw or s or not nw) and w)
                if transitions==1 and min(adjacent_a,adjacent_b) in (2,3) and not protected:result[row,col]=False
            image=result
        if int(image.sum())==previous_count:break
    expected=np.where(valid,image.astype(float),np.nan)
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
        raise FixtureError('invalid exact raster thinning target inventory')
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
            'exact_integer_pixels_verified':True,'binary_skeleton_verified':True,
            'foreground_pixels':int((actual==1).sum()),'background_pixels':int((actual==0).sum())}
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
        return NFSExport('thin',command).lifecycle(action,secret,os.environ['ADDP_ONLINE_TEST_RUN_ID'])
    root=owned_root(secret)
    if action=='seed':
        result=runtime_worker('seed',root);(secret/'thin-source.json').write_text(json.dumps(result));return result
    if action=='verify':
        result=runtime_worker('verify',root,sys.argv[2:])
        if result['sources']!=json.loads((secret/'thin-source.json').read_text())['sources']:
            raise FixtureError('source file changed')
        return result
    raise FixtureError('unknown fixture action')


if __name__=='__main__':
    try:
        result=worker(sys.argv[2],Path(sys.argv[3]),sys.argv[4:]) if sys.argv[1:2]==['--worker'] else main(sys.argv[1])
        print(json.dumps(result,sort_keys=True))
    except Exception as error:
        print(f'Raster thinning fixture failed: {type(error).__name__}: {error}',file=sys.stderr);raise SystemExit(1)
