#!/usr/bin/env python3
"""Owned NFS fixture and independent complete GeoPackage vector-zone oracle."""
from __future__ import annotations
from collections import deque
from functools import lru_cache
import hashlib
import json
import math
import os
from pathlib import Path
import statistics
import subprocess
import sys
import tempfile

SUITE='vector-zonal-statistics-persistence'
RUNTIME='addp-vector-zonal-statistics-runtime'
SOURCES=('values.tif','zones.geojson')
TARGETS=('center.gpkg','touched.gpkg','classified.gpkg')
FEATURE_COUNTS=dict(zip(TARGETS,(6,6,4)))
WIDTH,HEIGHT=519,515
TRANSFORM=(110.,1/1024,1/8192,30.,1/4096,-1/512)
COLUMNS=('total_count','valid_count','invalid_count','sum','min','max','mean','stddev')


class FixtureError(RuntimeError):
    pass


def command(args,**kwargs):
    return subprocess.run(args,check=True,capture_output=True,text=True,timeout=120,**kwargs).stdout.strip()


if sys.argv[1:2]!=['--worker']:
    sys.path.insert(0,str(Path(__file__).resolve().parents[2]))
    from scripts.utils.online_nfs_fixture import NFSExport,FixtureError,run_runtime_worker


def guard(env=os.environ):
    return NFSExport('vector-zonal-statistics',command).guard(env)


def owned_root(secret):
    return NFSExport('vector-zonal-statistics',command).owned_root(secret,os.environ['ADDP_ONLINE_TEST_RUN_ID'])


def digest(path):
    if path.is_symlink() or not path.is_file() or path.stat().st_size<=0:
        raise FixtureError('expected a positive-size ordinary owned file')
    return {'sha256':hashlib.sha256(path.read_bytes()).hexdigest(),'size_bytes':path.stat().st_size}


def source_arrays():
    import numpy as np
    y,x=np.indices((HEIGHT,WIDTH));values=x+2.*y-17
    values[0,1]=-9999;values[1,0]=np.nan
    values[508,508]=np.inf;values[510,510]=-np.inf;values[200:207,200:207]=-9999
    alpha=np.full(values.shape,255.);alpha[0,0]=128;alpha[509,509]=0
    alpha[511,511]=np.nan;alpha[512,512]=-1;alpha[507,507]=np.inf
    mask=np.full(values.shape,255,dtype=np.uint8);mask[508,510]=0
    return values,alpha,mask


def world(shape):
    from shapely.affinity import affine_transform
    a,b,c,d,e,f=TRANSFORM
    return affine_transform(shape,[b,c,e,f,a,d])


def pixel_faces():
    from shapely.geometry import Polygon,MultiPolygon,box
    return [Polygon(box(.125,.125,518.875,514.875).exterior.coords,
                [box(200.125,200.125,206.875,206.875).exterior.coords]),
        box(500.125,499.125,517.875,514.875),
        MultiPolygon([box(8.125,6.125,12.875,10.875),box(510.125,6.125,515.875,10.875)]),
        box(200.125,200.125,206.875,206.875),box(1000.125,1000.125,1001.875,1001.875),
        box(30.125,20.125,30.25,24.875)]


def properties():
    return [{'zone_code':i,'label':name,'occupied_sum':-i} for i,name in enumerate(
        ('whole-with-hole','overlap','multipart','all-invalid','outside','thin'))]


def seed(root):
    import numpy as np
    from osgeo import gdal,osr
    from shapely.geometry import mapping
    if any((root/name).exists() for name in SOURCES) or list((root/'outputs').iterdir()):
        raise FixtureError('refusing to overwrite existing fixture data')
    values,alpha,mask=source_arrays()
    dataset=gdal.GetDriverByName('GTiff').Create(str(root/SOURCES[0]),WIDTH,HEIGHT,3,gdal.GDT_Float64)
    crs=osr.SpatialReference();crs.ImportFromEPSG(4326)
    dataset.SetProjection(crs.ExportToWkt());dataset.SetGeoTransform(TRANSFORM)
    dataset.GetRasterBand(1).WriteArray(np.full(values.shape,np.nan))
    band=dataset.GetRasterBand(2);band.WriteArray(values);band.SetNoDataValue(-9999)
    band.SetScale(17);band.SetOffset(500);band=None
    dataset.GetRasterBand(3).WriteArray(alpha);dataset.GetRasterBand(3).SetColorInterpretation(gdal.GCI_AlphaBand)
    with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):dataset.CreateMaskBand(gdal.GMF_PER_DATASET)
    dataset.GetRasterBand(1).GetMaskBand().WriteArray(mask)
    if dataset.GetFileList()!=[str(root/SOURCES[0])]:raise FixtureError('source must be self-contained')
    dataset=None
    (root/SOURCES[1]).write_text(json.dumps({'type':'FeatureCollection','features':[
        {'type':'Feature','properties':props,'geometry':mapping(world(shape))}
        for props,shape in zip(properties(),pixel_faces())]}))
    return {'sources':{n:digest(root/n) for n in SOURCES}}


def valid_pixels():
    import numpy as np
    values,alpha,mask=source_arrays()
    return np.isfinite(values)&(values!=-9999)&np.isfinite(alpha)&(alpha>0)&(mask!=0)


def summary(covered):
    values=source_arrays()[0];samples=values[covered&valid_pixels()].tolist()
    total=int(covered.sum());count=len(samples)
    return [total,count,total-count,*( [math.fsum(samples),min(samples),max(samples),statistics.fmean(samples),
        statistics.pstdev(samples)] if samples else [None]*5)]


@lru_cache(maxsize=3)
def expected_rows(name):
    import numpy as np
    import shapely
    from shapely.geometry import box
    if name not in TARGETS:raise FixtureError('unknown exact vector-zone target')
    values=source_arrays()[0];valid=valid_pixels();rows=[]
    if name!='classified.gpkg':
        y,x=np.indices(values.shape)
        centers=shapely.points(x+.5,y+.5)
        cells=shapely.box(x,y,x+1,y+1) if name=='touched.gpkg' else None
        for props,shape in zip(properties(),pixel_faces()):
            # Independent pixel-plane geometry oracle; no GDAL rasterization,
            # product windows, streaming statistics or Polygonize is used.
            covered=(shapely.area(shapely.intersection(cells,shape))>0 if cells is not None
                else shapely.contains(shape,centers))
            rows.append((props,world(shape),summary(covered)))
        return rows
    codes=np.where(values<512,-3,np.where(values<1024,0,7));seen=np.zeros(values.shape,dtype=bool)
    for y,x in zip(*np.where(valid)):
        if seen[y,x]:continue
        seen[y,x]=True;pending=deque([(y,x)]);members=[]
        while pending:
            r,c=pending.popleft();members.append((r,c))
            for rr,cc in ((r-1,c),(r+1,c),(r,c-1),(r,c+1)):
                if 0<=rr<HEIGHT and 0<=cc<WIDTH and valid[rr,cc] and not seen[rr,cc] and codes[rr,cc]==codes[y,x]:
                    seen[rr,cc]=True;pending.append((rr,cc))
        covered=np.zeros(values.shape,dtype=bool)
        rr,cc=zip(*members);covered[rr,cc]=True
        # Union maximal horizontal runs rather than hundreds of thousands of
        # individual cells; exact binary affine preserves all cell boundaries.
        strips=[]
        for r in range(HEIGHT):
            edges=np.diff(np.pad(covered[r].astype(int),(1,1)))
            strips.extend(box(int(a),r,int(b),r+1) for a,b in zip(np.flatnonzero(edges==1),np.flatnonzero(edges==-1)))
        shape=world(shapely.union_all(strips))
        rows.append(({'class_value':int(codes[y,x])},shape,summary(covered)))
    return rows


def schema(name):
    prefix='stats_' if name=='touched.gpkg' else 'zonal_'
    attrs=[('class_value','Integer64')] if name=='classified.gpkg' else [
        ('zone_code','Integer'),('label','String'),('occupied_sum','Integer')]
    return attrs+[(prefix+c,'Integer64' if i<3 else 'Real') for i,c in enumerate(COLUMNS)]


def verify(root,names):
    from osgeo import ogr,osr
    from shapely import wkb
    if len(set(names))!=len(names) or any(n not in TARGETS for n in names):raise FixtureError('invalid exact target inventory')
    if sorted(p.name for p in (root/'outputs').iterdir())!=sorted(names):raise FixtureError('unexpected output, sidecar or temporary file')
    result={'sources':{n:digest(root/n) for n in SOURCES},'targets':{}}
    crs=osr.SpatialReference();crs.ImportFromEPSG(4326)
    for name in names:
        path=root/'outputs'/name;fact=digest(path);data=ogr.Open(str(path),0)
        if data is None or data.GetDriver().GetName()!='GPKG' or data.GetLayerCount()!=1:
            raise FixtureError('expected exactly one GeoPackage layer')
        layer=data.GetLayer(0)
        actual_crs=layer.GetSpatialRef()
        if not actual_crs:raise FixtureError('source CRS missing')
        actual_crs.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER)
        crs.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER)
        if not actual_crs.IsSame(crs):raise FixtureError('source CRS changed')
        definition=layer.GetLayerDefn()
        actual_schema=[(definition.GetFieldDefn(i).GetName(),definition.GetFieldDefn(i).GetTypeName()) for i in range(definition.GetFieldCount())]
        if actual_schema!=schema(name):raise FixtureError('field names, order or types changed')
        remaining=list(expected_rows(name));count=0;nulls=0
        for feature in layer:
            geometry=feature.GetGeometryRef()
            if geometry is None or geometry.GetGeometryType() not in (ogr.wkbPolygon,ogr.wkbMultiPolygon):
                raise FixtureError('expected two dimensional polygon geometry')
            shape=wkb.loads(bytes(geometry.ExportToWkb()))
            if shape.is_empty or not shape.is_valid or shape.has_z:raise FixtureError('invalid output geometry')
            matches=[i for i,(props,expected,_) in enumerate(remaining) if shape.equals(expected) and all(feature.GetField(k)==v for k,v in props.items())]
            if len(matches)!=1 or (name!='classified.gpkg' and matches[0]!=0):raise FixtureError('full geometry, attributes or row order differs')
            _,_,expected=remaining.pop(matches[0]);prefix='stats_' if name=='touched.gpkg' else 'zonal_'
            for i,(column,wanted) in enumerate(zip(COLUMNS,expected)):
                actual=feature.GetField(prefix+column)
                if wanted is None:
                    if actual is not None:raise FixtureError('empty statistics must remain null')
                elif i<3:
                    if type(actual) is not int or actual!=wanted:raise FixtureError('complete pixel counts differ')
                elif type(actual) not in (int,float) or not math.isfinite(actual) or not math.isclose(actual,wanted,rel_tol=1e-12,abs_tol=1e-12):
                    raise FixtureError('complete statistics differ')
            nulls+=expected[1]==0;count+=1
        if remaining:raise FixtureError('missing vector zones')
        result['targets'][name]={**fact,'layer_name':layer.GetName(),'feature_count':count,'null_zone_count':nulls,
            'schema':[list(v) for v in actual_schema],'crs':'EPSG:4326','complete_geometry_attributes_statistics_verified':True}
        data=None
    return result


def worker(action,root,names):
    if action not in {'seed','verify'}:raise FixtureError('unknown worker action')
    if action=='seed':return seed(root)
    if list(Path(tempfile.gettempdir()).glob('addp-raster-*')):raise FixtureError('Runtime raster workspace has residuals')
    return {**verify(root,names),'runtime_workspaces_removed':True}


def runtime_worker(action,root,names=()):
    return run_runtime_worker(command,RUNTIME,SUITE,Path(__file__),action,root,names)


def main(action):
    secret=guard()
    if action in {'start','stop'}:
        return NFSExport('vector-zonal-statistics',command).lifecycle(action,secret,os.environ['ADDP_ONLINE_TEST_RUN_ID'])
    root=owned_root(secret)
    if action=='seed':
        result=runtime_worker('seed',root);(secret/'vector-zonal-statistics-source.json').write_text(json.dumps(result));return result
    if action=='verify':
        result=runtime_worker('verify',root,sys.argv[2:])
        if result['sources']!=json.loads((secret/'vector-zonal-statistics-source.json').read_text())['sources']:raise FixtureError('source files changed')
        return result
    raise FixtureError('unknown fixture action')


if __name__=='__main__':
    try:
        result=worker(sys.argv[2],Path(sys.argv[3]),sys.argv[4:]) if sys.argv[1:2]==['--worker'] else main(sys.argv[1])
        print(json.dumps(result,sort_keys=True))
    except Exception as error:
        print(f'Vector-zone fixture failed: {type(error).__name__}: {error}',file=sys.stderr);raise SystemExit(1)
