#!/usr/bin/env python3
"""Owned faces/raster sources and independent complete GeoPackage percentile oracle."""
from __future__ import annotations
from collections import deque
from fractions import Fraction
import hashlib
import importlib
import json
import math
import os
from pathlib import Path
import re
import statistics
import subprocess
import sys
import tempfile

SUITE='vector-zonal-percentile-persistence'
PREFIX='vector-zonal-percentile'
RUNTIME='addp-vector-zonal-percentile-runtime'
WIDTH,HEIGHT=519,515
GRID=(100.,2.,.25,200.,.125,-3.)
TRANSFORM=GRID
ORACLE='.vector-zonal-percentile-oracle.json'
BASE_COLUMNS=('total_count','valid_count','invalid_count','sum','min','max','mean','stddev')
CASES={
    'original.gpkg':('values.tif','zones.geojson',None,False,'zonal_'),
    'p0.gpkg':('values.tif','zones.geojson',0,False,'zonal_'),
    'p100.gpkg':('values.tif','zones.geojson',100,False,'zonal_'),
    'p50-odd.gpkg':('odd.tif','small.geojson',50,False,'zonal_'),
    'p50-even.gpkg':('even.tif','small.geojson',50,False,'zonal_'),
    'p25.gpkg':('values.tif','zones.geojson',25,False,'zonal_'),
    'p75.gpkg':('values.tif','zones.geojson',75,False,'zonal_'),
    'p33-center.gpkg':('values.tif','zones.geojson',33.3,False,'zonal_'),
    'p33-touched.gpkg':('values.tif','zones.geojson',33.3,True,'zonal_'),
    'custom.gpkg':('values.tif','zones.geojson',25,False,'custom_'),
    'empty-prefix.gpkg':('values.tif','zones.geojson',75,False,''),
    'classified.gpkg':('values.tif',None,90,False,'zonal_'),
    'int64.gpkg':('int64.tif','small.geojson',50,False,'zonal_'),
    'uint64.gpkg':('uint64.tif','small.geojson',50,False,'zonal_'),
    'invalid-values.gpkg':('invalid.tif','small.geojson',50,False,'zonal_'),
    'empty-zones.gpkg':('even.tif','empty.gpkg',50,False,'zonal_'),
}
FAILURES=('percentile_null','percentile_boolean','percentile_string','percentile_array','percentile_object','percentile_low','percentile_high',
    'band_zero','band_high','band_boolean','band_string','complex','alpha','touched_numeric','touched_null','prefix_null','prefix_boolean',
    'collision_stats','collision_percentile','collision_value','different_crs','missing_crs','missing_grid','singular_grid')
RASTERS=('values.tif','odd.tif','even.tif','int64.tif','uint64.tif','invalid.tif','complex.tif','missing_crs.tif','missing_grid.tif','singular_grid.tif')
VECTORS=('zones.geojson','small.geojson','different-crs.geojson','empty.gpkg')
SOURCES=RASTERS+VECTORS

class FixtureError(RuntimeError):pass

def command(args,**kwargs):return subprocess.run(args,check=True,capture_output=True,text=True,timeout=120,**kwargs).stdout.strip()
if sys.argv[1:2]!=['--worker']:
    sys.path.insert(0,str(Path(__file__).resolve().parents[2]))
    from scripts.utils.online_nfs_fixture import NFSExport,FixtureError,run_runtime_worker
    archive_files=importlib.import_module('business.scripts.online-raster-zonal-percentile-fixture').archive_files

def guard(env=os.environ):return NFSExport(PREFIX,command).guard(env)
def owned_root(secret):return NFSExport(PREFIX,command).owned_root(secret,os.environ['ADDP_ONLINE_TEST_RUN_ID'])
def encoded(value):return (json.dumps(value,allow_nan=False,sort_keys=True)+'\n').encode()
def digest(path):
    if path.is_symlink() or not path.is_file() or path.stat().st_size<=0:raise FixtureError('expected a positive ordinary file')
    return {'size_bytes':path.stat().st_size,'sha256':hashlib.sha256(path.read_bytes()).hexdigest()}
def main_arrays():
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
        box(30.125,20.125,30.25,24.875),box(17.125,.125,17.875,.875),
        box(500.125,499.125,517.875,514.875),box(-3.875,-4.875,3.875,4.875)]


def properties():
    return [{'source_row':i+1,'label':name,'occupied_mean':-i,'p_percentile':100+i,'v_percentile_value':200+i}
        for i,name in enumerate(('whole-with-hole','overlap','multipart','all-invalid','outside','thin','zero','duplicate','partial-outside'))]

def source_arrays(name):
    import numpy as np
    dtype,nodata,crs,grid='Float64',-9999.,3857,GRID
    if name=='values.tif':values,alpha,mask=main_arrays()
    else:
        values=np.tile(np.array([[-8.,0.,5.,11.,nodata,nodata,nodata,nodata]]),(3,1))
        values[2]=nodata;values[2,0]=0.
        mask=np.full(values.shape,255,np.uint8);alpha=np.full(values.shape,128.)
        if name=='odd.tif':values[:,3]=nodata
        if name=='invalid.tif':values[:]=nodata
        if name in ('int64.tif','uint64.tif'):
            dtype='Int64' if name=='int64.tif' else 'UInt64';nodata=2**63-1 if dtype=='Int64' else 2**64-1
            values=np.tile(np.array([[nodata,nodata-1,0,12,6,2,8,4]],dtype=dtype.lower()),(3,1));alpha=np.full(values.shape,128,dtype=values.dtype)
        if name=='complex.tif':dtype='CFloat64';values=values.astype(complex);values[:]=1+2j
        if name=='missing_crs.tif':crs=None
        if name=='missing_grid.tif':grid=None
        if name=='singular_grid.tif':grid=(100.,1.,2.,200.,2.,4.)
    first=np.zeros(values.shape,dtype=values.dtype)
    return np.stack([first,values,alpha.astype(values.dtype)]),mask,dtype,nodata,crs,grid

def vector_rows(name):
    from shapely.geometry import box
    if name=='empty.gpkg':return []
    if name=='zones.geojson':faces=pixel_faces()
    elif name=='small.geojson':faces=[box(.125,.125,3.875,.875),box(4.125,1.125,7.875,1.875),box(.125,2.125,.875,2.875),box(100.125,100.125,101.875,101.875)]
    elif name=='different-crs.geojson':faces=[box(-1.,-1.,1.,1.)]
    else:raise FixtureError('unknown vector source')
    return list(zip(properties()[:len(faces)],[face if name=='different-crs.geojson' else world(face) for face in faces]))

def seed(root):
    from osgeo import gdal,ogr,osr
    from shapely.geometry import mapping
    if any((root/n).exists() for n in (*SOURCES,ORACLE)) or list((root/'outputs').iterdir()):raise FixtureError('refusing to overwrite source or previous result')
    for name in RASTERS:
        data,mask,dtype,nodata,crs,grid=source_arrays(name)
        ds=gdal.GetDriverByName('GTiff').Create(str(root/name),data.shape[2],data.shape[1],3,getattr(gdal,'GDT_'+dtype),options=['COMPRESS=DEFLATE','TILED=YES','BLOCKXSIZE=512','BLOCKYSIZE=512'])
        if crs:
            reference=osr.SpatialReference();reference.ImportFromEPSG(crs);ds.SetProjection(reference.ExportToWkt())
        if grid:ds.SetGeoTransform(grid)
        for index,values in enumerate(data,1):
            band=ds.GetRasterBand(index);band.WriteArray(values)
            if dtype=='Int64':band.SetNoDataValueAsInt64(nodata)
            elif dtype=='UInt64':band.SetNoDataValueAsUInt64(nodata)
            else:band.SetNoDataValue(nodata)
            band.SetScale(17.);band.SetOffset(500.)
        ds.GetRasterBand(3).SetColorInterpretation(gdal.GCI_AlphaBand)
        with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):ds.CreateMaskBand(gdal.GMF_PER_DATASET)
        ds.GetRasterBand(1).GetMaskBand().WriteArray(mask);band=ds=None
    for name in VECTORS:
        if name=='empty.gpkg':
            ds=ogr.GetDriverByName('GPKG').CreateDataSource(str(root/name));crs=osr.SpatialReference();crs.ImportFromEPSG(3857)
            layer=ds.CreateLayer('empty',crs,ogr.wkbPolygon)
            for key in properties()[0]:layer.CreateField(ogr.FieldDefn(key,ogr.OFTString if key=='label' else ogr.OFTInteger))
            layer=ds=None
        else:
            (root/name).write_text(json.dumps({'type':'FeatureCollection','crs':{'type':'name','properties':{'name':'EPSG:'+('4326' if name=='different-crs.geojson' else '3857')}},'features':[
                {'type':'Feature','properties':props,'geometry':mapping(shape)} for props,shape in vector_rows(name)]}))
    return {'sources':{n:digest(root/n) for n in SOURCES}}

def valid_pixels(name):
    import numpy as np
    data,mask,_,nodata,_,_=source_arrays(name)
    return np.isfinite(data[1])&(data[1]!=nodata)&np.isfinite(data[2])&(data[2]>0)&(mask!=0)

def summary(name,covered,p):
    values=source_arrays(name)[0][1];samples=sorted(float(v) for v in values[covered&valid_pixels(name)])
    total=int(covered.sum());count=len(samples)
    row=dict(zip(BASE_COLUMNS,[total,count,total-count,*([math.fsum(samples),samples[0],samples[-1],statistics.fmean(samples),statistics.pstdev(samples)] if samples else [None]*5)]))
    if p is not None:
        value=None
        if count:
            position=(count-1)*Fraction(float(p))/100;low=position.numerator//position.denominator;weight=position-low
            value=float(Fraction(samples[low])*(1-weight)+Fraction(samples[low+bool(weight)])*weight)
        row.update(percentile=float(p),percentile_value=value)
    return row

def class_rules():return [{'min':-100,'max':512,'class':-3},{'min':512,'max':1024,'class':0},{'min':1024,'max':2048,'class':7}]

def classified_rows():
    import numpy as np
    import shapely
    from shapely.geometry import box
    values=source_arrays('values.tif')[0][1];valid=valid_pixels('values.tif')
    codes=np.where(values<512,-3,np.where(values<1024,0,7));seen=np.zeros(values.shape,dtype=bool);rows=[]
    for y,x in zip(*np.where(valid)):
        if seen[y,x]:continue
        seen[y,x]=True;pending=deque([(y,x)]);members=[]
        while pending:
            r,c=pending.popleft();members.append((r,c))
            for rr,cc in ((r-1,c),(r+1,c),(r,c-1),(r,c+1)):
                if 0<=rr<HEIGHT and 0<=cc<WIDTH and valid[rr,cc] and not seen[rr,cc] and codes[rr,cc]==codes[y,x]:seen[rr,cc]=True;pending.append((rr,cc))
        covered=np.zeros(values.shape,dtype=bool);rr,cc=zip(*members);covered[rr,cc]=True;strips=[]
        for r in range(HEIGHT):
            edges=np.diff(np.pad(covered[r].astype(int),(1,1)))
            strips.extend(box(int(a),r,int(b),r+1) for a,b in zip(np.flatnonzero(edges==1),np.flatnonzero(edges==-1)))
        rows.append(({'class_value':int(codes[y,x])},world(shapely.union_all(strips)),covered))
    return rows

def schema(name):
    _,vector,p,_,prefix=CASES[name]
    attrs=[('class_value','Integer64')] if vector is None else [(key,'String' if key=='label' else 'Integer') for key in properties()[0]]
    return attrs+[(prefix+key,'Integer64' if i<3 else 'Real') for i,key in enumerate((*BASE_COLUMNS,*(('percentile','percentile_value') if p is not None else ())))]

def oracle():
    import numpy as np
    import shapely
    from shapely.affinity import affine_transform
    results={};populations={}
    for name,(raster,vector,p,touched,prefix) in CASES.items():
        key=(raster,vector,touched)
        if key not in populations:
            if vector is None:rows=classified_rows()
            else:
                data=source_arrays(raster)[0];y,x=np.indices(data.shape[1:]);centers=shapely.points(x+.5,y+.5);cells=shapely.box(x,y,x+1,y+1) if touched else None
                # Independent inverse of our binary affine, not product/GDAL coverage.
                a,b,c,d,e,f=GRID;det=b*f-c*e
                inverse=[f/det,-c/det,-e/det,b/det,(c*d-f*a)/det,(e*a-b*d)/det];rows=[]
                for props,shape in vector_rows(vector):
                    pixel=affine_transform(shape,inverse)
                    covered=shapely.area(shapely.intersection(cells,pixel))>0 if touched else shapely.contains(pixel,centers)
                    rows.append((props,shape,covered))
            populations[key]=rows
        results[name]=[{'properties':dict(props,**{prefix+k:v for k,v in summary(raster,covered,p).items()}),'geometry_wkb':shape.wkb_hex} for props,shape,covered in populations[key]]
    return results

def validate_properties(actual,wanted,name):
    if set(actual)!=set(wanted):raise FixtureError('complete properties/schema differ')
    prefix=CASES[name][4];p=CASES[name][2]
    numeric={prefix+k:k for k in (*BASE_COLUMNS,'percentile','percentile_value')}
    for key,target in wanted.items():
        value=actual[key];stat=numeric.get(key)
        if target is None:
            if value is not None:raise FixtureError('null was replaced')
        elif stat is None or stat in BASE_COLUMNS[:3]:
            if type(value)!=type(target) or value!=target:raise FixtureError('original property or exact count differs')
        elif type(value) not in (int,float) or not math.isfinite(value):raise FixtureError('nonnumeric or nonfinite statistic')
        elif stat in ('percentile','min','max') or (stat=='percentile_value' and (target==0 or p in (0,50,100))):
            if value!=target:raise FixtureError('exact percentile or endpoint differs')
        elif not math.isclose(value,target,rel_tol=4e-15 if stat=='percentile_value' else 1e-12,abs_tol=0. if stat=='percentile_value' else 1e-12):raise FixtureError('independent complete statistic differs')

def verify_vector(path,expected,expected_schema,crs_code,*,name=None,ordered=True):
    from osgeo import ogr,osr
    from shapely import wkb
    ds=ogr.Open(str(path),0)
    if ds is None or ds.GetLayerCount()!=1:raise FixtureError('exactly one vector layer required')
    layer=ds.GetLayer(0);crs=layer.GetSpatialRef();reference=osr.SpatialReference();reference.ImportFromEPSG(crs_code)
    if crs is None:raise FixtureError('vector CRS missing')
    crs.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER);reference.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER)
    if not crs.IsSame(reference):raise FixtureError('vector CRS changed')
    definition=layer.GetLayerDefn();actual_schema=[(definition.GetFieldDefn(i).GetName(),definition.GetFieldDefn(i).GetTypeName()) for i in range(definition.GetFieldCount())]
    if actual_schema!=expected_schema:raise FixtureError('field names/order/types changed: '+str(actual_schema))
    remaining=list(expected);count=0;records=[]
    for feature in layer:
        geometry=feature.GetGeometryRef()
        if geometry is None or geometry.GetGeometryType() not in (ogr.wkbPolygon,ogr.wkbMultiPolygon):raise FixtureError('two dimensional polygon required')
        shape=wkb.loads(bytes(geometry.ExportToWkb()))
        if shape.is_empty or not shape.is_valid or shape.has_z:raise FixtureError('invalid output geometry')
        props={key:feature.GetField(key) for key,_ in expected_schema}
        matches=[i for i,row in enumerate(remaining) if shape.equals(wkb.loads(row['geometry_wkb'],hex=True)) and (not ordered or i==0)]
        if not ordered:matches=[i for i in matches if all(props.get(k)==v for k,v in remaining[i]['properties'].items() if k=='class_value')]
        if len(matches)!=1:raise FixtureError('full geometry/order/unique membership differs')
        wanted=remaining.pop(matches[0])['properties']
        if name:validate_properties(props,wanted,name)
        elif props!=wanted:raise FixtureError('source properties changed')
        records.append(props);count+=1
    if remaining:raise FixtureError('missing features')
    fact={'layer_name':layer.GetName(),'feature_count':count,'schema':[list(v) for v in actual_schema],'crs':'EPSG:'+str(crs_code),'complete_geometry_attributes_statistics_verified':True}
    ds=None;return fact,records

def verify(root,names,expectations):
    import numpy as np
    from osgeo import gdal,osr
    if len(set(names))!=len(names) or any(n not in CASES for n in names):raise FixtureError('unexpected target names')
    if set(p.name for p in root.iterdir())!={*SOURCES,'outputs','.addp-online-owner',ORACLE}:raise FixtureError('unexpected source sidecar/private file')
    if set(p.name for p in (root/'outputs').iterdir())!=set(names):raise FixtureError('unexpected output or failed target')
    result={'sources':{},'targets':{},'full_gpkg_checks':len(names),'full_source_pixels_verified':True,'full_source_vectors_verified':True};records={}
    for name in RASTERS:
        data,mask,dtype,nodata,crs,grid=source_arrays(name);ds=gdal.Open(str(root/name))
        if ds is None or ds.GetDriver().ShortName!='GTiff' or ds.RasterCount!=3 or (ds.RasterYSize,ds.RasterXSize)!=mask.shape or ds.GetFileList()!=[str(root/name)] or ds.GetGeoTransform(can_return_null=True)!=grid:raise FixtureError('source grid/type/inventory changed')
        reference=osr.SpatialReference()
        if crs:
            reference.ImportFromEPSG(crs)
            if not ds.GetProjection() or not osr.SpatialReference(wkt=ds.GetProjection()).IsSame(reference):raise FixtureError('source CRS changed')
        elif ds.GetProjection():raise FixtureError('missing CRS fabricated')
        for index,values in enumerate(data,1):
            band=ds.GetRasterBand(index)
            if band.DataType!=getattr(gdal,'GDT_'+dtype) or band.GetNoDataValue()!=nodata or band.GetScale()!=17. or band.GetOffset()!=500. or band.GetColorInterpretation()!=(gdal.GCI_AlphaBand if index==3 else gdal.GCI_GrayIndex if index==1 else gdal.GCI_Undefined) or not np.array_equal(band.ReadAsArray(),values,equal_nan=True) or not np.array_equal(band.GetMaskBand().ReadAsArray(),mask):raise FixtureError('complete source pixels/mask/NoData/scale changed')
        band=ds=None;result['sources'][name]=digest(root/name)
    attrs=[(key,'String' if key=='label' else 'Integer') for key in properties()[0]]
    for name in VECTORS:
        expected=[{'properties':p,'geometry_wkb':s.wkb_hex} for p,s in vector_rows(name)]
        verify_vector(root/name,expected,attrs,4326 if name=='different-crs.geojson' else 3857);result['sources'][name]=digest(root/name)
    for name in names:
        path=root/'outputs'/name
        if gdal.IdentifyDriver(str(path)).ShortName!='GPKG':raise FixtureError('GeoPackage driver required')
        fact,records[name]=verify_vector(path,expectations[name],schema(name),3857,name=name,ordered=name!='classified.gpkg')
        result['targets'][name]={**digest(path),**fact}
    if 'original.gpkg' in records:
        baseline=[{k:row['zonal_'+k] for k in BASE_COLUMNS} for row in records['original.gpkg']]
        for name,case in CASES.items():
            if name in records and case[0]=='values.tif' and case[1]=='zones.geojson' and not case[3]:
                if [{k:row[case[4]+k] for k in BASE_COLUMNS} for row in records[name]]!=baseline:raise FixtureError('percentile changed original eight fields')
    return result

def worker(action, root, names):
    if action == 'seed' and not names: return seed(root)
    if action == 'cleanup-oracle':
        path = root/ORACLE
        if path.is_symlink(): raise FixtureError('oracle symlink')
        path.unlink(missing_ok=True); return {'oracle_cache_removed': not path.exists()}
    if action == 'oracle' and not names:
        expectations = oracle(); raw = encoded(expectations)
        with (root/ORACLE).open('xb') as stream: stream.write(raw)
        (root/ORACLE).chmod(0o444); anchor = hashlib.sha256(raw).hexdigest()
    elif action == 'verify' and names and re.fullmatch('[0-9a-f]{64}', names[0]):
        anchor, names = names[0], names[1:]; path = root/ORACLE
        if path.is_symlink() or not path.is_file() or hashlib.sha256(path.read_bytes()).hexdigest() != anchor: raise FixtureError('oracle cache hash changed')
        expectations = json.loads(path.read_bytes())
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
    args = sys.argv[2:]
    if action == 'verify': args = [os.environ['ADDP_ONLINE_VECTOR_ZONAL_PERCENTILE_ORACLE_SHA256'], *args]
    result = runtime_worker(action, root, args)
    if action == 'seed':
        with state.open('x') as stream: stream.write(encoded(result))
    elif action != 'cleanup-oracle':
        if result['sources'] != json.loads(state.read_bytes())['sources']: raise FixtureError('source file changed')
        artifacts = Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR'])
        history = secret/(PREFIX+'-archives.json'); old = json.loads(history.read_bytes()) if history.exists() else {'sources': {}, 'targets': {}}
        if action != 'oracle' and not history.is_file(): raise FixtureError('source archive history missing')
        result['archived_sources'] = archive_files(root, artifacts, SOURCES, old['sources'], 'vector-zonal-percentile-sources')
        result['archived_gpkgs'] = archive_files(root/'outputs', artifacts, sys.argv[2:], old['targets'], 'vector-zonal-percentile-gpkgs')
        history.write_bytes(encoded({'sources': result['archived_sources'], 'targets': result['archived_gpkgs']}))
    return result

if __name__ == '__main__':
    try:
        result = worker(sys.argv[2], Path(sys.argv[3]), sys.argv[4:]) if sys.argv[1:2] == ['--worker'] else main(sys.argv[1])
        print(json.dumps(result, allow_nan=False, sort_keys=True))
    except Exception as error:
        print(f'Zonal percentile fixture failed: {type(error).__name__}: {error}', file=sys.stderr); raise SystemExit(1)
