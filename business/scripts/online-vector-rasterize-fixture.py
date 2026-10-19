#!/usr/bin/env python3
"""Owned NFS fixture and independent whole-pixel oracle for vector rasterization."""
from __future__ import annotations
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

SUITE = 'vector-rasterize-persistence'
RUNTIME = 'addp-raster-rasterize-runtime'
TARGETS = ('field.cog.tif', 'constant.cog.tif', 'roundtrip.cog.tif')
SOURCES = ('reference.tif', 'zones.geojson')
TRANSFORM = (110., 1/1024, 1/8192, 30., 1/4096, -1/512)
WIDTH, HEIGHT = 8, 6


class FixtureError(RuntimeError):
    pass


def command(args, **kwargs):
    return subprocess.run(args, check=True, capture_output=True, text=True, timeout=120, **kwargs).stdout.strip()


if sys.argv[1:2] != ['--worker']:
    sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
    from scripts.utils.online_nfs_fixture import NFSExport, FixtureError, run_runtime_worker


def guard(env=os.environ):
    return NFSExport('rasterize', command).guard(env)


def owned_root(secret):
    return NFSExport('rasterize', command).owned_root(secret, os.environ['ADDP_ONLINE_TEST_RUN_ID'])


def digest(path):
    if path.is_symlink() or not path.is_file() or path.stat().st_size <= 0:
        raise FixtureError('expected a positive-size ordinary owned file')
    return {'sha256': hashlib.sha256(path.read_bytes()).hexdigest(), 'size_bytes': path.stat().st_size}


def point(x, y):
    a,b,c,d,e,f = TRANSFORM
    return a+b*x+c*y, d+e*x+f*y


def faces():
    from shapely.geometry import Polygon, MultiPolygon, box
    from shapely.affinity import affine_transform
    geometries = [Polygon([(.125,.125),(5.875,.125),(5.875,5.875),(.125,5.875)],
                         holes=[[(1.125,1.125),(3.875,1.125),(3.875,3.875),(1.125,3.875)]]),
                  box(4.125,.125,6.875,3.875),
                  MultiPolygon([box(.125,4.125,.875,5.875),box(6.125,4.125,7.875,5.875)]),
                  box(6.125,.125,6.375,3.875)]
    a,b,c,d,e,f = TRANSFORM
    return [affine_transform(g,[b,c,e,f,a,d]) for g in geometries]


def source_arrays():
    import numpy as np
    values = np.array([[0,0,2,2,2,-1,-1,7], [0,2,2,-999,2,-1,7,7],
        [0,2,2,2,2,0,7,7], [-1,-1,0,0,-1,-1,7,7],
        [0,-1,0,-1,0,-1,7,7], [-1,0,-1,0,-1,0,7,7]], dtype=np.int16)
    alpha=np.full(values.shape,255,dtype=np.int16);alpha[1,4]=0;alpha[0,0]=128
    mask=np.full(values.shape,255,dtype=np.uint8);mask[4,5]=0
    return values,alpha,mask


def seed(root):
    from osgeo import gdal, osr
    from shapely.geometry import mapping
    if any((root/name).exists() for name in SOURCES) or list((root/'outputs').iterdir()):
        raise FixtureError('refusing to overwrite existing fixture data')
    values,alpha,mask=source_arrays()
    dataset=gdal.GetDriverByName('GTiff').Create(str(root/SOURCES[0]),WIDTH,HEIGHT,2,gdal.GDT_Int16)
    crs=osr.SpatialReference();crs.ImportFromEPSG(4326)
    dataset.SetProjection(crs.ExportToWkt());dataset.SetGeoTransform(TRANSFORM)
    dataset.GetRasterBand(1).WriteArray(values);dataset.GetRasterBand(1).SetNoDataValue(-999)
    dataset.GetRasterBand(2).WriteArray(alpha);dataset.GetRasterBand(2).SetColorInterpretation(gdal.GCI_AlphaBand)
    with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):
        dataset.CreateMaskBand(gdal.GMF_PER_DATASET);dataset.GetRasterBand(1).GetMaskBand().WriteArray(mask)
    dataset=None
    (root/SOURCES[1]).write_text(json.dumps({'type':'FeatureCollection','features':[
        {'type':'Feature','properties':{'class_code':value},'geometry':mapping(geometry)}
        for geometry,value in zip(faces(),[0,-3.25,2.75,7])]}))
    return {'sources':{name:digest(root/name) for name in SOURCES}}


def expected_pixels(name):
    import numpy as np
    from shapely.geometry import Point, Polygon
    if name not in TARGETS:raise FixtureError('unknown exact rasterization target')
    if name=='roundtrip.cog.tif':
        values,alpha,mask=source_arrays()
        return np.where((values!=-999)&(alpha>0)&(mask>0),values,np.nan)
    expected=np.full((HEIGHT,WIDTH),np.nan)
    geometries=faces();values=[-1.5]*len(geometries) if name=='constant.cog.tif' else [0,-3.25,2.75,7]
    # Scalar independent geometry/cell intersections, never product or GDAL Rasterize.
    for y in range(HEIGHT):
        for x in range(WIDTH):
            cell=Polygon([point(x,y),point(x+1,y),point(x+1,y+1),point(x,y+1)])
            for geometry,value in zip(geometries,values):
                covered=(geometry.intersection(cell).area>0 if name=='constant.cog.tif'
                         else geometry.contains(Point(point(x+.5,y+.5))))
                if covered:expected[y,x]=value
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
        raise FixtureError('invalid exact rasterization target inventory')
    if sorted(p.name for p in (root/'outputs').iterdir())!=sorted(names):
        raise FixtureError('unexpected output, sidecar or temporary file')
    result={'sources':{name:digest(root/name) for name in SOURCES},'targets':{}}
    crs=osr.SpatialReference();crs.ImportFromEPSG(4326)
    for name in names:
        path=root/'outputs'/name;fact=digest(path);dataset=gdal.Open(str(path))
        if dataset is None or dataset.GetDriver().ShortName!='GTiff' or dataset.RasterCount!=1:
            raise FixtureError('expected one-band GeoTIFF')
        if (dataset.RasterXSize,dataset.RasterYSize,dataset.GetGeoTransform())!=(WIDTH,HEIGHT,TRANSFORM):
            raise FixtureError('reference grid changed')
        actual_crs=osr.SpatialReference();actual_crs.ImportFromWkt(dataset.GetProjection())
        if not actual_crs.IsSame(crs):raise FixtureError('reference CRS changed')
        band=dataset.GetRasterBand(1)
        if band.DataType!=gdal.GDT_Float64 or band.GetNoDataValue() is None or not np.isnan(band.GetNoDataValue()):
            raise FixtureError('Float64 or NaN NoData changed')
        expected=expected_pixels(name);actual=band.ReadAsArray()
        if not np.array_equal(actual,expected,equal_nan=True):raise FixtureError('complete pixel values differ')
        if not np.array_equal(band.GetMaskBand().ReadAsArray(),np.where(np.isfinite(expected),255,0)):
            raise FixtureError('complete pixel mask differs')
        _,errors,_=validate(dataset,full_check=True)
        if errors:raise FixtureError('COG full layout validation failed')
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
        return NFSExport('rasterize',command).lifecycle(action,secret,os.environ['ADDP_ONLINE_TEST_RUN_ID'])
    root=owned_root(secret)
    if action=='seed':
        result=runtime_worker('seed',root);(secret/'rasterize-source.json').write_text(json.dumps(result));return result
    if action=='verify':
        result=runtime_worker('verify',root,sys.argv[2:])
        if result['sources']!=json.loads((secret/'rasterize-source.json').read_text())['sources']:
            raise FixtureError('source file changed')
        return result
    raise FixtureError('unknown fixture action')


if __name__=='__main__':
    try:
        result=worker(sys.argv[2],Path(sys.argv[3]),sys.argv[4:]) if sys.argv[1:2]==['--worker'] else main(sys.argv[1])
        print(json.dumps(result,sort_keys=True))
    except Exception as error:
        print(f'Rasterize fixture failed: {type(error).__name__}: {error}',file=sys.stderr);raise SystemExit(1)
