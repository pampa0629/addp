#!/usr/bin/env python3
"""Owned polygon frequency sources and independent complete categorical oracle."""
from __future__ import annotations
from fractions import Fraction
import hashlib
import importlib
import json
import math
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
SUITE='vector-zonal-class-frequency-persistence'
PREFIX='vector-zonal-class-frequency'
RUNTIME='addp-vector-zonal-class-frequency-runtime'
ORACLE='.vector-zonal-class-frequency-oracle.json'
GRID=(100.,2.,0.,200.,0.,-3.)
CASES={
 'direct.gpkg':('values.tif','zones.gpkg','frequency',False,'zonal_'),
 'touched.gpkg':('values.tif','zones.gpkg','frequency',True,'zonal_'),
 'holes.gpkg':('values.tif','holes.gpkg','frequency',False,'zonal_'),
 'overlap.gpkg':('values.tif','overlap.gpkg','frequency',False,'zonal_'),
 'multipart.gpkg':('values.tif','multipart.gpkg','frequency',False,'zonal_'),
 'rotated.gpkg':('rotated.tif','rotated.gpkg','frequency',False,'zonal_'),
 'geographic.gpkg':('geographic.tif','geographic.gpkg','frequency',False,'zonal_'),
 'coverage.gpkg':('coverage.tif','zones.gpkg','frequency',False,'zonal_'),
 'cross-block.gpkg':('large.tif','large.gpkg','frequency',False,'zonal_'),
 'all-invalid.gpkg':('invalid.tif','zones.gpkg','frequency',False,'zonal_'),
 'outside.gpkg':('values.tif','outside.gpkg','frequency',False,'zonal_'),
 'empty.gpkg':('values.tif','empty.gpkg','frequency',False,'zonal_'),
 'business-fid.gpkg':('values.tif','fid.gpkg','frequency',False,'zonal_'),
 'business-uppercase-fid.gpkg':('values.tif','upper-fid.gpkg','frequency',False,'zonal_'),
 'business-mixed-fid.gpkg':('values.tif','mixed-fid.gpkg','frequency',False,'custom_'),
 'summary.gpkg':('values.tif','zones.gpkg','summary',False,'zonal_'),
}
FAILURES=('method_null','method_unknown','percentile_null','weights_null','weights_band','stddev','area','area_stddev','flag_string',
 'id_missing','id_null','id_duplicate','id_unknown','band_zero','band_high','band_boolean','band_string','band_null','alpha',
 'fractional','unsafe','different_crs','prefix_null','collision','touched_numeric','point','invalid_polygon','z_geometry',
 'summary_id','prefix_boolean','touched_null','complex')
RASTERS=('values.tif','rotated.tif','geographic.tif','coverage.tif','large.tif','invalid.tif','fractional.tif','unsafe.tif','complex.tif')
VECTORS=('zones.gpkg','holes.gpkg','overlap.gpkg','multipart.gpkg','rotated.gpkg','geographic.gpkg','large.gpkg','outside.gpkg','empty.gpkg','fid.gpkg','upper-fid.gpkg','mixed-fid.gpkg','duplicate.gpkg','different-crs.gpkg','collision.gpkg','point.gpkg','invalid-polygon.gpkg','z-geometry.gpkg')
SOURCES=RASTERS+VECTORS
ATTRS=[('zone_id','Integer64'),('label','String'),('nullable_label','String'),('_addp_fid','Integer64'),('_ADDP_FID_1','String')]
COLUMNS=('class_value','count','proportion','total_count','valid_count','invalid_count')
SUMMARY=('total_count','valid_count','invalid_count','sum','min','max','mean','stddev')
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
 if path.is_symlink() or not path.is_file() or path.stat().st_size<=0:raise FixtureError('expected positive ordinary file')
 return {'size_bytes':path.stat().st_size,'sha256':hashlib.sha256(path.read_bytes()).hexdigest()}
def source_arrays(name):
 import numpy as np
 shape=(515,519)if name=='large.tif'else(9,9);y,x=np.indices(shape);values=((x+2*y)%3-1).astype(float);alpha=np.full(shape,128.);mask=np.full(shape,255,np.uint8)
 values[0,1]=-9999;values[1,0]=np.nan;mask[1,2]=0;alpha[1,3]=0
 dtype='Float64';grid=GRID;crs=3857
 if name=='coverage.tif':alpha[2,:5]=[128,0,-1,np.nan,np.inf];values[3,3]=np.inf
 if name=='invalid.tif':values[:]=-9999
 if name=='rotated.tif':grid=(100.,2.,.5,200.,.25,-3.)
 if name=='geographic.tif':grid=(110.,.125,0.,40.,0.,-.25);crs=4326
 if name=='fractional.tif':values[2,2]=.25
 if name=='unsafe.tif':values[2,2]=2**53
 if name=='complex.tif':values=values.astype(complex);values[:]=1+2j;dtype='CFloat64'
 return np.stack([np.zeros(shape,dtype=values.dtype),values,alpha.astype(values.dtype)]),mask,dtype,-9999.,crs,grid

def vector_crs(name):return 4326 if name in('geographic.gpkg','different-crs.gpkg')else 3857

def vector_schema(name):
 field={'fid.gpkg':'fid','upper-fid.gpkg':'FID','mixed-fid.gpkg':'FiD'}.get(name)
 return ATTRS+([(field,'Integer64')]if field else[])+([('zonal_count','Integer64')]if name=='collision.gpkg'else[])

def vector_spec(name):
 from shapely.geometry import box,Polygon,MultiPolygon,Point
 from shapely.affinity import affine_transform
 faces=[box(.125,.125,8.875,8.875),box(1.125,1.125,4.875,4.875)]
 if name=='holes.gpkg':faces=[Polygon(box(.125,.125,8.875,8.875).exterior.coords,[box(2.125,2.125,5.875,5.875).exterior.coords])]
 if name=='overlap.gpkg':faces+=[faces[0]]
 if name=='multipart.gpkg':faces=[MultiPolygon([box(.125,.125,1.875,1.875),box(6.125,6.125,8.875,8.875)])]
 if name=='large.gpkg':faces=[box(510.125,510.125,518.875,514.875),box(.125,.125,2.875,2.875)]
 if name=='outside.gpkg':faces=[box(-10,-10,-9,-9)]
 if name=='empty.gpkg':faces=[]
 if name=='point.gpkg':faces=[Point(2,2)]
 if name=='invalid-polygon.gpkg':faces=[Polygon([(0,0),(4,4),(0,4),(4,0),(0,0)])]
 if name=='z-geometry.gpkg':faces=[Polygon([(0,0,1),(3,0,1),(0,3,1),(0,0,1)])]
 raster='rotated.tif'if name=='rotated.gpkg'else'geographic.tif'if name=='geographic.gpkg'else'values.tif';a,b,c,d,e,f=source_arrays(raster)[5]
 rows=[]
 for i,face in enumerate(faces):
  props={'zone_id':(1 if name=='duplicate.gpkg'else i+1),'label':'zone-'+str(i+1),'nullable_label':None if i%2==0 else'kept','_addp_fid':10+i,'_ADDP_FID_1':'ordinary'}
  field={'fid.gpkg':'fid','upper-fid.gpkg':'FID','mixed-fid.gpkg':'FiD'}.get(name)
  if field:props[field]=2**53+1+i
  if name=='collision.gpkg':props['zonal_count']=17
  rows.append((props,affine_transform(face,[b,c,e,f,a,d])))
 return rows

def schema(name):
 _,vector,mode,_,prefix=CASES[name]
 return vector_schema(vector)+[(prefix+k,'Real'if k=='proportion'or k in SUMMARY[3:]else'Integer64')for k in(SUMMARY if mode=='summary'else COLUMNS)]

def seed(root):
 from osgeo import gdal,ogr,osr
 if any((root/n).exists()for n in(*SOURCES,ORACLE))or list((root/'outputs').iterdir()):raise FixtureError('refusing to overwrite sources/results')
 for name in RASTERS:
  data,mask,dtype,nodata,crs,grid=source_arrays(name);ds=gdal.GetDriverByName('GTiff').Create(str(root/name),data.shape[2],data.shape[1],3,getattr(gdal,'GDT_'+dtype),options=['COMPRESS=DEFLATE','TILED=YES','BLOCKXSIZE=512','BLOCKYSIZE=512']);reference=osr.SpatialReference();reference.ImportFromEPSG(crs);ds.SetProjection(reference.ExportToWkt());ds.SetGeoTransform(grid)
  for i,values in enumerate(data,1):
   band=ds.GetRasterBand(i);band.WriteArray(values);band.SetNoDataValue(nodata);band.SetScale(17.);band.SetOffset(500.)
  ds.GetRasterBand(3).SetColorInterpretation(gdal.GCI_AlphaBand)
  with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):ds.CreateMaskBand(gdal.GMF_PER_DATASET)
  ds.GetRasterBand(1).GetMaskBand().WriteArray(mask);band=ds=None
 for name in VECTORS:
  ds=ogr.GetDriverByName('GPKG').CreateDataSource(str(root/name));reference=osr.SpatialReference();reference.ImportFromEPSG(vector_crs(name));layer=ds.CreateLayer('source',reference,ogr.wkbUnknown,options=['FID=_source_pk'])
  for key,kind in vector_schema(name):layer.CreateField(ogr.FieldDefn(key,getattr(ogr,'OFT'+kind)))
  for props,shape in vector_spec(name):
   feature=ogr.Feature(layer.GetLayerDefn())
   for key,value in props.items():
    if value is not None:feature.SetField(key,value)
   feature.SetGeometry(ogr.CreateGeometryFromWkb(shape.wkb));layer.CreateFeature(feature);feature=None
  layer=ds=None
 return {'sources':{n:digest(root/n)for n in SOURCES}}

def oracle():
 import numpy as np,shapely,statistics
 from shapely.affinity import affine_transform
 results={}
 for name,(raster,vector,mode,touched,prefix)in CASES.items():
  data,mask,_,nd,_,grid=source_arrays(raster);y,x=np.indices(mask.shape);a,b,c,d,e,f=map(Fraction,grid);det=b*f-c*e
  inverse=list(map(float,[f/det,-c/det,-e/det,b/det,(c*d-f*a)/det,(e*a-b*d)/det]));centers=shapely.points(x+.5,y+.5);cells=shapely.box(x,y,x+1,y+1)if touched else None;rows=[]
  valid=(mask!=0)&np.isfinite(data[1])&(data[1]!=nd)&np.isfinite(data[2])&(data[2]!=nd)&(data[2]>0)
  for props,shape in vector_spec(vector):
   pixel=affine_transform(shape,inverse);covered=shapely.area(shapely.intersection(cells,pixel))>0 if touched else shapely.contains(pixel,centers)
   total=int(covered.sum());samples=[int(v)for v in data[1][covered&valid]];count=len(samples)
   if mode=='summary':members=[dict(zip(SUMMARY,[total,count,total-count,*([float(sum(samples)),float(min(samples)),float(max(samples)),statistics.fmean(samples),statistics.pstdev(samples)]if samples else[None]*5)]))]
   else:
    counts={}
    for value in samples:counts[value]=counts.get(value,0)+1
    members=[dict(zip(COLUMNS,[code,n,float(Fraction(n,count)),total,count,total-count]))for code,n in sorted(counts.items())]if counts else[dict(zip(COLUMNS,[None,0,None,total,0,total]))]
   rows.extend({'properties':{**props,**{prefix+k:v for k,v in member.items()}},'geometry_wkb':shape.wkb_hex,'geometry_geojson':__import__('shapely.geometry',fromlist=['mapping']).mapping(shape)}for member in members)
  results[name]=rows
 return results

def validate_properties(actual,wanted,name=None):
 if set(actual)!=set(wanted):raise FixtureError('complete properties/schema differ')
 for key,target in wanted.items():
  value=actual[key]
  if type(value)!=type(target)or value!=target:
   if name and CASES[name][2]=='summary'and key.endswith(('mean','stddev'))and type(value)in(int,float)and type(target)in(int,float)and math.isclose(value,target,rel_tol=1e-13,abs_tol=1e-13):continue
   raise FixtureError('exact class/count/ratio/source/null differs: '+key)

def verify_vector(path,expected,expected_schema,crs_code,*,name=None):
 from osgeo import ogr,osr
 from shapely import wkb
 ds=ogr.Open(str(path),0)
 if ds is None or ds.GetDriver().GetName()!='GPKG'or ds.GetLayerCount()!=1:raise FixtureError('exactly one GeoPackage layer required')
 layer=ds.GetLayer(0);crs=layer.GetSpatialRef();reference=osr.SpatialReference();reference.ImportFromEPSG(crs_code)
 if crs is None:raise FixtureError('vector CRS missing')
 crs.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER);reference.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER)
 if not crs.IsSame(reference):raise FixtureError('vector CRS changed')
 definition=layer.GetLayerDefn();actual_schema=[(definition.GetFieldDefn(i).GetName(),definition.GetFieldDefn(i).GetTypeName())for i in range(definition.GetFieldCount())]
 if actual_schema!=expected_schema:raise FixtureError('field names/order/types changed: '+str(actual_schema))
 if layer.GetFeatureCount()!=len(expected):raise FixtureError('physical feature count differs')
 count=0
 for feature in layer:
  geometry=feature.GetGeometryRef()
  if geometry is None or count>=len(expected):raise FixtureError('full geometry/count differs')
  shape=wkb.loads(bytes(geometry.ExportToWkb()));wanted=expected[count];wanted_shape=wkb.loads(wanted['geometry_wkb'],hex=True)
  if shape.geom_type!=wanted_shape.geom_type or not shape.equals_exact(wanted_shape,0)or shape.has_z!=wanted_shape.has_z or shape.wkb!=wanted_shape.wkb or shape.is_empty:raise FixtureError('full geometry/order differs')
  validate_properties({key:feature.GetField(key)for key,_ in expected_schema},wanted['properties'],name);count+=1
 if count!=len(expected):raise FixtureError('missing features')
 if name and layer.GetFIDColumn().casefold()in {key.casefold()for key,_ in expected_schema}:raise FixtureError('storage FID consumed a business field')
 ids=[feature.GetFID()for feature in layer]
 if len(set(ids))!=len(expected):raise FixtureError('storage FID is not unique')
 fact={'layer_name':layer.GetName(),'feature_count':count,'schema':[list(v)for v in actual_schema],'crs':'EPSG:'+str(crs_code),'complete_geometry_attributes_statistics_verified':True,'storage_fid_column':layer.GetFIDColumn(),'storage_fids':ids,'independent_storage_fid_verified':True}
 layer=ds=None;return fact

def verify(root,names,expectations):
 import numpy as np
 from osgeo import gdal,osr
 if len(set(names))!=len(names)or any(n not in CASES for n in names):raise FixtureError('unexpected target names')
 if set(p.name for p in root.iterdir())!={*SOURCES,'outputs','.addp-online-owner',ORACLE}:raise FixtureError('unexpected source sidecar/private file')
 if set(p.name for p in(root/'outputs').iterdir())!=set(names):raise FixtureError('unexpected output or failed target')
 result={'sources':{},'targets':{},'full_gpkg_checks':len(names),'full_source_pixels_verified':True,'full_source_vectors_verified':True}
 for name in RASTERS:
  data,mask,dtype,nodata,crs,grid=source_arrays(name);ds=gdal.Open(str(root/name))
  if ds is None or ds.GetDriver().ShortName!='GTiff'or ds.RasterCount!=3 or(ds.RasterYSize,ds.RasterXSize)!=mask.shape or ds.GetFileList()!=[str(root/name)]or ds.GetGeoTransform(can_return_null=True)!=grid:raise FixtureError('source grid/type/inventory changed')
  reference=osr.SpatialReference();reference.ImportFromEPSG(crs);actual=osr.SpatialReference(wkt=ds.GetProjection());actual.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER);reference.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER)
  if not ds.GetProjection()or not actual.IsSame(reference):raise FixtureError('source CRS changed')
  for index,values in enumerate(data,1):
   band=ds.GetRasterBand(index);nd=band.GetNoDataValueAsInt64()if dtype=='Int64'else band.GetNoDataValueAsUInt64()if dtype=='UInt64'else band.GetNoDataValue()
   if band.DataType!=getattr(gdal,'GDT_'+dtype)or nd!=nodata or band.GetScale()!=17. or band.GetOffset()!=500. or band.GetColorInterpretation()!=(gdal.GCI_AlphaBand if index==3 else gdal.GCI_GrayIndex if index==1 else gdal.GCI_Undefined)or not np.array_equal(band.ReadAsArray(),values,equal_nan=True)or not np.array_equal(band.GetMaskBand().ReadAsArray(),mask):raise FixtureError('complete source pixels/mask/NoData/scale changed')
  band=ds=None;result['sources'][name]=digest(root/name)
 for name in VECTORS:
  expected=[{'properties':p,'geometry_wkb':shape.wkb_hex}for p,shape in vector_spec(name)]
  verify_vector(root/name,expected,vector_schema(name),vector_crs(name));result['sources'][name]=digest(root/name)
 for name in names:
  raster,vector,*_=CASES[name];fact=verify_vector(root/'outputs'/name,expectations[name],schema(name),vector_crs(vector),name=name)
  result['targets'][name]={**digest(root/'outputs'/name),**fact}
 return result

def oracle_scope(root,script_sha):
 marker=root/'.addp-online-owner'
 if root.is_symlink()or root.resolve()!=root or marker.is_symlink()or not marker.is_file()or not re.fullmatch(PREFIX+r'-[0-9]+-[0-9]+',marker.read_text()):raise FixtureError('invalid oracle owned root/run')
 return {'root':str(root),'run_id':marker.read_text(),'fixture_sha256':script_sha,'source_files':{n:digest(root/n)for n in SOURCES}}

def worker(action, root, names):
    if action == 'seed' and not names: return seed(root)
    if action == 'cleanup-oracle':
        path = root/ORACLE
        if path.is_symlink(): raise FixtureError('oracle symlink')
        path.unlink(missing_ok=True); return {'oracle_cache_removed': not path.exists()}
    if action == 'oracle' and len(names)==1 and re.fullmatch('[0-9a-f]{64}',names[0]):
        expectations = oracle(); raw = encoded({'schema':'addp.vector-class-frequency-oracle/v1','scope':oracle_scope(root,names[0]),'expectations':expectations})
        with (root/ORACLE).open('xb') as stream: stream.write(raw)
        (root/ORACLE).chmod(0o444); anchor = hashlib.sha256(raw).hexdigest(); names=[]
    elif action == 'verify' and len(names)>=2 and all(re.fullmatch('[0-9a-f]{64}',v)for v in names[:2]):
        anchor, script_sha, names = names[0], names[1], names[2:]; path = root/ORACLE
        if path.is_symlink() or not path.is_file() or hashlib.sha256(path.read_bytes()).hexdigest() != anchor: raise FixtureError('oracle cache hash changed')
        envelope = json.loads(path.read_bytes())
        if envelope.get('schema')!='addp.vector-class-frequency-oracle/v1' or envelope.get('scope')!=oracle_scope(root,script_sha):raise FixtureError('oracle root/run/source/script scope changed')
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
    if action == 'verify': args = [os.environ['ADDP_ONLINE_VECTOR_ZONAL_CLASS_FREQUENCY_ORACLE_SHA256'],script_sha,*args]
    result = runtime_worker(action, root, args)
    if action == 'seed':
        with state.open('xb') as stream: stream.write(encoded(result))
    elif action != 'cleanup-oracle':
        if result['sources'] != json.loads(state.read_bytes())['sources']: raise FixtureError('source file changed')
        artifacts = Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR'])
        history = secret/(PREFIX+'-archives.json'); old = json.loads(history.read_bytes()) if history.exists() else {'sources': {}, 'targets': {}, 'oracle': {}}
        if action != 'oracle' and not history.is_file(): raise FixtureError('source archive history missing')
        result['archived_oracle']=archive_files(root,artifacts,[ORACLE],old['oracle'],PREFIX+'-oracle')
        result['archived_sources'] = archive_files(root, artifacts, SOURCES, old['sources'], 'vector-zonal-class-frequency-sources')
        result['archived_gpkgs'] = archive_files(root/'outputs', artifacts, sys.argv[2:], old['targets'], 'vector-zonal-class-frequency-gpkgs')
        history.write_bytes(encoded({'sources': result['archived_sources'], 'targets': result['archived_gpkgs'],'oracle':result['archived_oracle']}))
    return result

if __name__ == '__main__':
    try:
        result = worker(sys.argv[2], Path(sys.argv[3]), sys.argv[4:]) if sys.argv[1:2] == ['--worker'] else main(sys.argv[1])
        print(json.dumps(result, allow_nan=False, sort_keys=True))
    except Exception as error:
        print(f'Point sampling fixture failed: {type(error).__name__}: {error}', file=sys.stderr); raise SystemExit(1)
