#!/usr/bin/env python3
"""Owned bilinear sampling sources; frozen independent centre-area GeoPackage oracle."""
from __future__ import annotations
from decimal import Decimal,localcontext
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
SUITE='raster-sample-points-bilinear-persistence'
PREFIX='raster-sample-points-bilinear'
RUNTIME='addp-raster-sample-points-bilinear-runtime'
ORACLE='.raster-sample-points-bilinear-oracle.json'
GRID=(100.,2.,0.,200.,0.,-3.)
CASES={
 'cell-default.gpkg':('values.tif','points.gpkg','direct',None),
 'cell-explicit.gpkg':('values.tif','points.gpkg','direct','cell'),
 'bilinear-support.gpkg':('support.tif','support-points.gpkg','direct','bilinear'),
 'center-ulp.gpkg':('centers.tif','center-points.gpkg','direct','bilinear'),
 'boundaries.gpkg':('values.tif','edges.gpkg','direct','bilinear'),
 'rotated.gpkg':('rotated.tif','rotated-points.gpkg','direct','bilinear'),
 'geographic.gpkg':('geographic.tif','geographic-points.gpkg','direct','bilinear'),
 'west-south.gpkg':('west-south.tif','west-south-points.gpkg','direct','bilinear'),
 'coverage.gpkg':('coverage.tif','coverage-points.gpkg','direct','bilinear'),
 'int64.gpkg':('int64.tif','integer-points.gpkg','direct','bilinear'),
 'uint64.gpkg':('uint64.tif','integer-points.gpkg','direct','bilinear'),
 'cross-block.gpkg':('large.tif','large-points.gpkg','direct','bilinear'),
 'combined.gpkg':('values.tif','points.gpkg','combined','bilinear'),
 'all-nodata.gpkg':('invalid.tif','inside-points.gpkg','direct','bilinear'),
 'all-outside.gpkg':('values.tif','outside-points.gpkg','direct','bilinear'),
 'empty.gpkg':('values.tif','empty-points.gpkg','direct','bilinear'),
}
FAILURES=('band_zero','band_negative','band_high','band_boolean','band_string','band_null','band_list','band_object',
 'different_crs','multipoint','line','polygon','collision_value','collision_status','alpha','inexact_int64','inexact_uint64','missing_grid',
 'method_null','method_boolean','method_number','method_empty','method_unknown','method_case','method_list','method_object')
RASTERS=('values.tif','support.tif','centers.tif','rotated.tif','geographic.tif','west-south.tif','coverage.tif','int64.tif','uint64.tif','large.tif','invalid.tif','inexact-int64.tif','inexact-uint64.tif','missing-grid.tif')
VECTORS=('points.gpkg','support-points.gpkg','center-points.gpkg','edges.gpkg','rotated-points.gpkg','geographic-points.gpkg','west-south-points.gpkg','coverage-points.gpkg','integer-points.gpkg','inexact-points.gpkg','large-points.gpkg','inside-points.gpkg','outside-points.gpkg','empty-points.gpkg','different-crs.gpkg','multipoint.gpkg','line.gpkg','polygon.gpkg','collision-value.gpkg','collision-status.gpkg')
SOURCES=RASTERS+VECTORS
ATTRS=[('source_row','Integer'),('label','String'),('nullable_label','String'),('business_value','Real')]
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
def source_arrays(name):
 import numpy as np
 shape=(515,519)if name=='large.tif'else(4,5)
 y,x=np.indices(shape);values=x+2.*y-3.;dtype='Float64';nodata=-9999.;grid=GRID;crs=3857
 alpha=np.full(shape,128.);mask=np.full(shape,255,np.uint8)
 values[0,1]=nodata;values[1,0]=np.nan;values[1,1]=np.inf;mask[1,2]=0;alpha[1,3]=0
 if name=='support.tif':values=x*x+3.*y*y-4.;mask[:]=255;alpha[:]=128;values[3,4]=nodata
 if name=='centers.tif':values[:]=nodata;values[2,2]=7.;mask[:]=255;alpha[:]=128
 if name=='rotated.tif':grid=(100.,2.,.5,200.,.25,-3.)
 if name=='geographic.tif':grid=(110.,.125,0.,40.,0.,-.25);crs=4326
 if name=='west-south.tif':grid=(100.,-2.,0.,200.,0.,3.)
 if name=='coverage.tif':
  nodata=99.;values=x+2.*y-3.;values[0,1]=nodata;values[3,4]=np.nan
  alpha[0,:]=[128,128,99,np.nan,-1];alpha[2,0]=np.inf
 if name in ('int64.tif','uint64.tif','inexact-int64.tif','inexact-uint64.tif'):
  dtype='UInt64'if'uint64'in name else'Int64';nodata=2**64-1 if dtype=='UInt64'else 2**63-1
  values=np.full(shape,8,dtype=dtype.lower());values[0,:]=[nodata,2**53,2**53+4,2**60,0]
  values[1,0]=nodata;values[2,1]=2**53+1
  if name.startswith('inexact'):values[:]=8;values[0,0]=2**53+1
  alpha=np.full(shape,128,dtype=values.dtype);mask[:]=255
 if name=='invalid.tif':values[:]=nodata
 if name=='missing-grid.tif':grid=None
 first=np.full(shape,np.nan if dtype=='Float64'else 7,dtype=values.dtype)
 return np.stack([first,values,alpha.astype(values.dtype)]),mask,dtype,nodata,crs,grid

def xy(grid,u,v):
 a,b,c,d,e,f=map(Fraction,grid);u,v=Fraction(u),Fraction(v)
 return float(a+b*u+c*v),float(d+e*u+f*v)
def vector_crs(name):return 4326 if name in ('geographic-points.gpkg','different-crs.gpkg') else 3857
def vector_spec(name):
 from shapely.geometry import Point,MultiPoint,LineString,Polygon
 raster={'rotated-points.gpkg':'rotated.tif','geographic-points.gpkg':'geographic.tif','west-south-points.gpkg':'west-south.tif','large-points.gpkg':'large.tif'}.get(name,'values.tif')
 grid=source_arrays(raster)[5]
 hints=[(.5,.5),(2.5,.5),(3.5,.5),(2.,2.),(3.,2.5),(-.5,.5),(.5,4.5),(1.5,.5),(.5,1.5),(2.5,1.5),(3.5,1.5)]
 result=None
 if name=='support-points.gpkg':hints=[(.5,.5),(1.,1.),(1.,.5),(.5,1.),(1.25,1.75),(2.5,2.5),(.25,.5),(-.5,.5)]
 elif name=='center-points.gpkg':
  x,y=xy(grid,2.5,2.5);hints=[];result=[(Point(x,y),2.5,2.5)]
  result.extend((Point(xx,yy),2.5,2.5)for xx,yy in ((math.nextafter(x,-math.inf),y),(math.nextafter(x,math.inf),y),(x,math.nextafter(y,-math.inf)),(x,math.nextafter(y,math.inf))))
 elif name=='edges.gpkg':
  hints=[(0.,0.),(1.,2.),(5.,2.),(1.,4.),(0.,2.),(1.,0.),(.5,.5),(.25,1.5),(4.75,2.5)]
  result=[(Point(*xy(grid,u,v)),u,v)for u,v in hints]
  for u in(0.,.5,5.):
   x,y=xy(grid,u,2.5);result.extend((Point(math.nextafter(x,toward),y),u,2.5)for toward in(-math.inf,math.inf))
 elif name=='large-points.gpkg':hints=[(512.,512.),(512.5,512.5),(518.5,514.5),(512.,512.),(.5,.5),(519.,514.),(-1.,512.)]
 elif name=='coverage-points.gpkg':hints=[(c+.5,r+.5)for r in range(4)for c in range(5)]+[(2.,1.),(3.,3.),(3.5,3.),(4.,1.)]
 elif name=='integer-points.gpkg':hints=[(.5,.5),(1.5,.5),(2.5,.5),(3.5,.5),(4.5,.5),(2.,.5),(1.,2.),(2.5,2.5)]
 elif name=='inexact-points.gpkg':hints=[(1.,1.)]
 elif name=='inside-points.gpkg':hints=[(.5,.5),(4.5,3.5),(2.,2.)]
 elif name=='outside-points.gpkg':hints=[(-.5,.5),(5.,0.),(1.,4.)]
 elif name=='empty-points.gpkg':hints=[]
 elif name=='rotated-points.gpkg':hints += [(-.25,1.),(5.25,1.),(0.,0.),(5.,4.)]
 if result is None:result=[(Point(*xy(grid,u,v)),u,v)for u,v in hints]
 if name=='multipoint.gpkg':result=[(MultiPoint([(101,198.5),(105,198.5)]),.5,.5)]
 if name=='line.gpkg':result=[(LineString([(101,198.5),(105,198.5)]),.5,.5)]
 if name=='polygon.gpkg':result=[(Polygon([(101,198.5),(105,198.5),(105,192.5),(101,198.5)]),.5,.5)]
 rows=[]
 for i,(shape,u,v)in enumerate(result):
  props={'source_row':i+1,'label':'point-'+str(i+1),'nullable_label':None if i%2==0 else'kept','business_value':float(i)-2.5}
  if name in('collision-value.gpkg','collision-status.gpkg'):props['sample_value'if'value'in name else'sample_status']='occupied'
  rows.append((props,shape,u,v))
 return rows

def vector_schema(name):return ATTRS+[(('sample_value' if 'value' in name else 'sample_status'),'String')] if name.startswith('collision-') else ATTRS
def schema(name):return ATTRS+[('sample_value','Real'),('sample_status','String')]
def seed(root):
 from osgeo import gdal,ogr,osr
 if any((root/n).exists()for n in (*SOURCES,ORACLE))or list((root/'outputs').iterdir()):raise FixtureError('refusing to overwrite source or previous result')
 for name in RASTERS:
  data,mask,dtype,nodata,crs,grid=source_arrays(name)
  ds=gdal.GetDriverByName('GTiff').Create(str(root/name),data.shape[2],data.shape[1],3,getattr(gdal,'GDT_'+dtype),options=['COMPRESS=DEFLATE','TILED=YES','BLOCKXSIZE=512','BLOCKYSIZE=512'])
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
  rows=vector_spec(name);ds=ogr.GetDriverByName('GPKG').CreateDataSource(str(root/name));reference=osr.SpatialReference();reference.ImportFromEPSG(vector_crs(name))
  kind={'multipoint.gpkg':ogr.wkbMultiPoint,'line.gpkg':ogr.wkbLineString,'polygon.gpkg':ogr.wkbPolygon}.get(name,ogr.wkbPoint)
  layer=ds.CreateLayer('points',reference,kind,options=['GEOMETRY_NAME=shape','SPATIAL_INDEX=NO'])
  for key,typ in vector_schema(name):layer.CreateField(ogr.FieldDefn(key,{'Integer':ogr.OFTInteger,'String':ogr.OFTString,'Real':ogr.OFTReal}[typ]))
  for props,shape,_,_ in rows:
   feature=ogr.Feature(layer.GetLayerDefn());feature.SetGeometry(ogr.CreateGeometryFromWkb(shape.wkb))
   for key,value in props.items():
    if value is None:feature.SetFieldNull(key)
    else:feature.SetField(key,value)
   if layer.CreateFeature(feature)!=ogr.OGRERR_NONE:raise FixtureError('source point insert failed')
   feature=None
  layer=ds=None
 return {'sources':{n:digest(root/n)for n in SOURCES}}

def in_cell(point,grid,c,r,w=1,h=1):
 # Independent quadrilateral half-plane tests, no affine inversion or product helper.
 with localcontext()as context:
  context.prec=5000
  a,b,k,d,e,f=map(Decimal.from_float,grid);x,y=map(Decimal.from_float,(point.x,point.y))
  def vertex(u,v):return a+b*u+k*v,d+e*u+f*v
  vertices=[vertex(c,r),vertex(c+w,r),vertex(c+w,r+h),vertex(c,r+h)]
  orientation=1 if b*f-k*e>0 else -1
  for i in range(4):
   ax,ay=vertices[i];bx,by=vertices[(i+1)%4];cross=((bx-ax)*(y-ay)-(by-ay)*(x-ax))*orientation
   if cross<0 or (cross==0 and i in (1,2)):return False
  return True

def cell(point,grid,width,height,u,v):
 if not in_cell(point,grid,0,0,width,height):return None
 candidates=[(r,c)for r in range(max(0,math.floor(v)-1),min(height,math.floor(v)+2))for c in range(max(0,math.floor(u)-1),min(width,math.floor(u)+2))if in_cell(point,grid,c,r)]
 if len(candidates)!=1:raise FixtureError('independent point-cell witness is not unique')
 return candidates[0]
def class_rules():return [{'min':-100,'max':0,'class':-2},{'min':0,'max':100000,'class':7}]
def centre_support(point,grid,width,height,u,v):
 # Independent centre-parallelogram areas, no product inverse/support or GDAL resampler.
 if not in_cell(point,grid,0,0,width,height):return None
 with localcontext()as context:
  context.prec=5000;g=list(map(Decimal.from_float,grid));p=tuple(map(Decimal.from_float,(point.x,point.y)))
  def center(c,r):
   c,r=Decimal(c)+Decimal('.5'),Decimal(r)+Decimal('.5')
   return(g[0]+g[1]*c+g[2]*r,g[3]+g[4]*c+g[5]*r)
  def sub(a,b):return(a[0]-b[0],a[1]-b[1])
  def cross(a,b):return a[0]*b[1]-a[1]*b[0]
  witnesses=[]
  for r in range(math.floor(v-.5)-1,math.floor(v-.5)+2):
   for c in range(math.floor(u-.5)-1,math.floor(u-.5)+2):
    origin=center(c,r);east=sub(center(c+1,r),origin);south=sub(center(c,r+1),origin);offset=sub(p,origin);area=Fraction(cross(east,south))
    a,b=Fraction(cross(offset,south))/area,Fraction(cross(east,offset))/area
    if 0<=a<1 and 0<=b<1:witnesses.append([(r+j,c+i,wx*wy)for j,wy in enumerate((1-b,b))for i,wx in enumerate((1-a,a))if wx*wy>0])
  if len(witnesses)!=1:raise FixtureError('independent centre support witness is not unique')
  return witnesses[0]

def oracle():
 import numpy as np
 results={}
 for name,(raster,vector,mode,method)in CASES.items():
  data,mask,_,nodata,_,grid=source_arrays(raster);height,width=mask.shape;rows=[]
  for props,shape,u,v in vector_spec(vector):
   if method in(None,'cell'):
    index=cell(shape,grid,width,height,u,v);support=None if index is None else[(*index,Fraction(1))]
   else:support=centre_support(shape,grid,width,height,u,v)
   value=None;status='outside'
   if support is not None:
    status='nodata'
    if all(0<=r<height and 0<=c<width and mask[r,c]!=0 and np.isfinite(data[1,r,c])and data[1,r,c]!=nodata and np.isfinite(data[2,r,c])and data[2,r,c]!=nodata and data[2,r,c]>0 for r,c,w in support):
     total=Fraction(0)
     for r,c,w in support:
      original=data[1,r,c];number=float(original)
      if np.issubdtype(data.dtype,np.integer)and int(number)!=int(original):raise FixtureError('successful oracle requires exact native integers')
      if mode=='combined':number=(-2. if number<0 else 7.)*2.+1.
      total+=Fraction(number)*w
     value=float(total);status='ok'
   rows.append({'properties':{**props,'sample_value':value,'sample_status':status},'geometry_wkb':shape.wkb_hex,'geometry_coordinates':[shape.x,shape.y]})
  results[name]=rows
 return results

def validate_properties(actual,wanted,name=None):
 if set(actual)!=set(wanted):raise FixtureError('complete properties/schema differ')
 for key,target in wanted.items():
  value=actual[key]
  if type(value)!=type(target)or value!=target:raise FixtureError('exact original property, sampled value/status or null differs')

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
  if shape.geom_type!=wanted_shape.geom_type or not shape.equals_exact(wanted_shape,0)or shape.has_z or shape.is_empty:raise FixtureError('full geometry/order differs')
  validate_properties({key:feature.GetField(key)for key,_ in expected_schema},wanted['properties'],name);count+=1
 if count!=len(expected):raise FixtureError('missing features')
 fact={'layer_name':layer.GetName(),'feature_count':count,'schema':[list(v)for v in actual_schema],'crs':'EPSG:'+str(crs_code),'complete_geometry_attributes_statistics_verified':True}
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
  expected=[{'properties':p,'geometry_wkb':shape.wkb_hex}for p,shape,_,_ in vector_spec(name)]
  verify_vector(root/name,expected,vector_schema(name),vector_crs(name));result['sources'][name]=digest(root/name)
 for name in names:
  raster,vector,_,_=CASES[name];fact=verify_vector(root/'outputs'/name,expectations[name],schema(name),vector_crs(vector),name=name)
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
        expectations = oracle(); raw = encoded({'schema':'addp.bilinear-point-sampling-oracle/v1','scope':oracle_scope(root,names[0]),'expectations':expectations})
        with (root/ORACLE).open('xb') as stream: stream.write(raw)
        (root/ORACLE).chmod(0o444); anchor = hashlib.sha256(raw).hexdigest(); names=[]
    elif action == 'verify' and len(names)>=2 and all(re.fullmatch('[0-9a-f]{64}',v)for v in names[:2]):
        anchor, script_sha, names = names[0], names[1], names[2:]; path = root/ORACLE
        if path.is_symlink() or not path.is_file() or hashlib.sha256(path.read_bytes()).hexdigest() != anchor: raise FixtureError('oracle cache hash changed')
        envelope = json.loads(path.read_bytes())
        if envelope.get('schema')!='addp.bilinear-point-sampling-oracle/v1' or envelope.get('scope')!=oracle_scope(root,script_sha):raise FixtureError('oracle root/run/source/script scope changed')
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
    if action == 'verify': args = [os.environ['ADDP_ONLINE_RASTER_SAMPLE_POINTS_BILINEAR_ORACLE_SHA256'],script_sha,*args]
    result = runtime_worker(action, root, args)
    if action == 'seed':
        with state.open('xb') as stream: stream.write(encoded(result))
    elif action != 'cleanup-oracle':
        if result['sources'] != json.loads(state.read_bytes())['sources']: raise FixtureError('source file changed')
        artifacts = Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR'])
        history = secret/(PREFIX+'-archives.json'); old = json.loads(history.read_bytes()) if history.exists() else {'sources': {}, 'targets': {}, 'oracle': {}}
        if action != 'oracle' and not history.is_file(): raise FixtureError('source archive history missing')
        result['archived_oracle']=archive_files(root,artifacts,[ORACLE],old['oracle'],PREFIX+'-oracle')
        result['archived_sources'] = archive_files(root, artifacts, SOURCES, old['sources'], 'raster-sample-points-bilinear-sources')
        result['archived_gpkgs'] = archive_files(root/'outputs', artifacts, sys.argv[2:], old['targets'], 'raster-sample-points-bilinear-gpkgs')
        history.write_bytes(encoded({'sources': result['archived_sources'], 'targets': result['archived_gpkgs'],'oracle':result['archived_oracle']}))
    return result

if __name__ == '__main__':
    try:
        result = worker(sys.argv[2], Path(sys.argv[3]), sys.argv[4:]) if sys.argv[1:2] == ['--worker'] else main(sys.argv[1])
        print(json.dumps(result, allow_nan=False, sort_keys=True))
    except Exception as error:
        print(f'Point sampling fixture failed: {type(error).__name__}: {error}', file=sys.stderr); raise SystemExit(1)
