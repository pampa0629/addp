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
SUITE='raster-sample-lines-persistence'
PREFIX='raster-sample-lines'
RUNTIME='addp-raster-sample-lines-runtime'
ORACLE='.raster-sample-lines-oracle.json'
GRID=(100.,2.,0.,200.,0.,-3.)
CASES={
 'cell.gpkg':('values.tif','lines.gpkg','direct','cell',1.),
 'bilinear.gpkg':('values.tif','lines.gpkg','direct','bilinear',1.),
 'feet.gpkg':('feet.tif','feet.gpkg','direct','cell',.5),
 'survey-feet.gpkg':('survey-feet.tif','survey-feet.gpkg','direct','bilinear',.5),
 'corners.gpkg':('values.tif','corners.gpkg','direct','cell',2.),
 'short.gpkg':('values.tif','short.gpkg','direct','bilinear',10.),
 'closed.gpkg':('values.tif','closed.gpkg','direct','cell',1.),
 'endpoint.gpkg':('values.tif','endpoint.gpkg','direct','bilinear',2.),
 'rotated.gpkg':('rotated.tif','rotated.gpkg','direct','bilinear',1.),
 'rectangular.gpkg':('values.tif','lines.gpkg','direct','cell',.5),
 'outside.gpkg':('values.tif','outside.gpkg','direct','bilinear',2.),
 'nodata.gpkg':('invalid.tif','lines.gpkg','direct','cell',1.),
 'coverage.gpkg':('coverage.tif','lines.gpkg','direct','bilinear',1.),
 'business-fid.gpkg':('values.tif','fid.gpkg','direct','cell',1.),
 'business-uppercase-fid.gpkg':('values.tif','upper-fid.gpkg','direct','bilinear',1.),
 'business-mixed-fid.gpkg':('values.tif','mixed-fid.gpkg','direct','bilinear',1.),
}
FAILURES=('band_zero','band_high','band_boolean','band_string','band_null','alpha','method_null','method_unknown',
 'spacing_zero','spacing_negative','spacing_boolean','spacing_string','spacing_null','spacing_list',
 'id_geometry','id_null','id_unknown','id_duplicate','different_crs','geographic',
 'point','polygon','multiline','zero_line','z_line','collision_distance','collision_value','collision_status')
RASTERS=('values.tif','feet.tif','survey-feet.tif','rotated.tif','invalid.tif','coverage.tif','geographic.tif')
VECTORS=('lines.gpkg','feet.gpkg','survey-feet.gpkg','corners.gpkg','short.gpkg','closed.gpkg','endpoint.gpkg','rotated.gpkg','outside.gpkg','fid.gpkg','upper-fid.gpkg','mixed-fid.gpkg','duplicate.gpkg','different-crs.gpkg','geographic.gpkg','point.gpkg','polygon.gpkg','multiline.gpkg','zero-line.gpkg','z-line.gpkg','collision-distance.gpkg','collision-value.gpkg','collision-status.gpkg')
SOURCES=RASTERS+VECTORS
ATTRS=[('source_row','Integer64'),('label','String'),('nullable_label','String'),('_addp_fid','Integer64'),('_ADDP_FID_1','String')]
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
 shape=(9,9);y,x=np.indices(shape);values=x+2.*y-3.;alpha=np.full(shape,128.);mask=np.full(shape,255,np.uint8);crs=3857;grid=GRID
 values[0,1]=-9999;values[1,0]=np.nan;mask[1,2]=0;alpha[1,3]=0
 if name=='coverage.tif':alpha[2,:5]=[128,0,-1,np.nan,np.inf];values[3,3]=np.inf
 if name=='invalid.tif':values[:]=-9999
 if name=='feet.tif':crs=2222
 if name=='survey-feet.tif':crs=2277
 if name=='geographic.tif':crs=4326
 if name=='rotated.tif':grid=(100.,2.,.5,200.,.25,-3.)
 return np.stack([np.zeros(shape),values,alpha]),mask,'Float64',-9999.,crs,grid

def xy(grid,u,v):
 a,b,c,d,e,f=map(Fraction,grid);u,v=Fraction(u),Fraction(v)
 return float(a+b*u+c*v),float(d+e*u+f*v)

def vector_crs(name):return {'feet.gpkg':2222,'survey-feet.gpkg':2277,'geographic.gpkg':4326,'different-crs.gpkg':4326}.get(name,3857)
def vector_schema(name):
 field={'fid.gpkg':'fid','upper-fid.gpkg':'FID','mixed-fid.gpkg':'FiD'}.get(name)
 return ATTRS+([(field,'Integer64')]if field else[])+([(name[10:-5].replace('distance','distance_m').replace('value','sample_value').replace('status','sample_status'),'String')]if name.startswith('collision-')else[])
def vector_spec(name):
 from shapely.geometry import Point,Polygon,LineString,MultiLineString
 raster='rotated.tif'if name=='rotated.gpkg'else'values.tif';grid=source_arrays(raster)[5]
 routes=[[(-.5,.5),(3.5,.5),(3.5,3.5)],[(.5,5.5),(5.5,5.5)]]
 if name=='corners.gpkg':routes=[[(.5,.5),(2.5,.5),(2.5,.5),(2.5,4.5)]]
 if name=='short.gpkg':routes=[[(2.5,2.5),(2.75,2.5)]]
 if name=='closed.gpkg':routes=[[(2.5,2.5),(3.5,2.5),(3.5,3.5),(2.5,2.5)]]
 if name=='endpoint.gpkg':routes=[[(.5,5.5),(2.5,5.5)]]
 if name=='outside.gpkg':routes=[[(-3.5,.5),(-.5,.5)]]
 if name=='zero-line.gpkg':routes=[[(1.,1.),(1.,1.)]]
 shapes=[LineString([xy(grid,*p)for p in route])for route in routes]
 if name=='point.gpkg':shapes=[Point(*xy(grid,1,1))]
 if name=='polygon.gpkg':shapes=[Polygon([xy(grid,*p)for p in[(0,0),(3,0),(0,3),(0,0)]])]
 if name=='multiline.gpkg':shapes=[MultiLineString(shapes)]
 if name=='z-line.gpkg':shapes=[LineString([(x,y,1)for x,y in shapes[0].coords])]
 rows=[]
 for i,shape in enumerate(shapes):
  props={'source_row':1 if name=='duplicate.gpkg'else i+1,'label':'line-'+str(i+1),'nullable_label':None if i%2==0 else'kept','_addp_fid':10+i,'_ADDP_FID_1':'ordinary'}
  for key,kind in vector_schema(name):
   if key not in props:props[key]=2**53+1+i if kind=='Integer64'else'occupied'
  rows.append((props,shape,0,0))
 return rows

def schema(name):return vector_schema(CASES[name][1])+[('distance_m','Real'),('sample_value','Real'),('sample_status','String')]
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
  kind=ogr.wkbUnknown
  layer=ds.CreateLayer('points',reference,kind,options=['GEOMETRY_NAME=shape','SPATIAL_INDEX=NO','FID=_source_pk'])
  for key,typ in vector_schema(name):layer.CreateField(ogr.FieldDefn(key,{'Integer':ogr.OFTInteger,'Integer64':ogr.OFTInteger64,'String':ogr.OFTString,'Real':ogr.OFTReal}[typ]))
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

def chain_oracle(line, spacing, unit):
    """Restart a scalar segment walk per sample; Decimal computes the roots."""
    coordinates = list(line.coords); edges = []
    with localcontext() as context:
        context.prec = 2000
        for a, b in zip(coordinates, coordinates[1:]):
            dx = Decimal.from_float(b[0])-Decimal.from_float(a[0]); dy = Decimal.from_float(b[1])-Decimal.from_float(a[1])
            squared = (dx*dx+dy*dy)*Decimal.from_float(unit)**2
            if squared: edges.append((a, b, Fraction(float(squared.sqrt()))))
    length = sum((edge[2] for edge in edges), Fraction(0)); step = Fraction(spacing)
    count = (length/step).numerator//(length/step).denominator
    positions = [i*step for i in range(count+1) if i*step < length]+[length]
    answer = []
    for distance in positions:
        if distance == 0: xy = coordinates[0]
        elif distance == length: xy = coordinates[-1]
        else:
            previous = Fraction(0)
            for a, b, segment in edges:
                if distance <= previous+segment:
                    t = (distance-previous)/segment
                    xy = tuple(float(Fraction(a[i])+(Fraction(b[i])-Fraction(a[i]))*t) for i in (0, 1)); break
                previous += segment
        answer.append((float(distance), xy))
    return answer


def oracle():
 import numpy as np
 results={}
 for name,(raster,vector,mode,method,spacing)in CASES.items():
  data,mask,_,nd,crs,grid=source_arrays(raster);height,width=mask.shape;unit=__import__('pyproj').CRS.from_epsg(crs).axis_info[0].unit_conversion_factor;rows=[]
  for props,line,_,_ in vector_spec(vector):
   for distance,coords in chain_oracle(line,spacing,unit):
    from shapely.geometry import Point
    point=Point(*coords);a,b,c,d,e,f=map(Fraction,grid);x,y=Fraction(coords[0])-a,Fraction(coords[1])-d;u,v=(f*x-c*y)/(b*f-c*e),(b*y-e*x)/(b*f-c*e)
    if method=='cell':
     index=cell(point,grid,width,height,u,v);support=None if index is None else[(*index,Fraction(1))]
    else:support=centre_support(point,grid,width,height,u,v)
    value=None;status='outside'
    if support is not None:
     status='nodata'
     if all(0<=r<height and 0<=c<width and mask[r,c]!=0 and np.isfinite(data[1,r,c])and data[1,r,c]!=nd and np.isfinite(data[2,r,c])and data[2,r,c]!=nd and data[2,r,c]>0 for r,c,w in support):
      value=float(sum((Fraction(float(data[1,r,c]))*w for r,c,w in support),Fraction(0)));status='ok'
    rows.append({'properties':{**props,'distance_m':distance,'sample_value':value,'sample_status':status},'geometry_wkb':point.wkb_hex,'geometry_coordinates':list(coords)})
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
  if shape.geom_type!=wanted_shape.geom_type or not shape.equals_exact(wanted_shape,0)or shape.has_z!=wanted_shape.has_z or shape.wkb!=wanted_shape.wkb or shape.is_empty:raise FixtureError('full geometry/order differs')
  validate_properties({key:feature.GetField(key)for key,_ in expected_schema},wanted['properties'],name);count+=1
 if count!=len(expected):raise FixtureError('missing features')
 if name and layer.GetFIDColumn().casefold()in {key.casefold()for key,_ in expected_schema}:raise FixtureError('storage FID consumed business field')
 ids=[f.GetFID()for f in layer]
 if len(set(ids))!=len(expected):raise FixtureError('storage FIDs not unique')
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
  expected=[{'properties':p,'geometry_wkb':shape.wkb_hex}for p,shape,_,_ in vector_spec(name)]
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
        expectations = oracle(); raw = encoded({'schema':'addp.line-sampling-oracle/v1','scope':oracle_scope(root,names[0]),'expectations':expectations})
        with (root/ORACLE).open('xb') as stream: stream.write(raw)
        (root/ORACLE).chmod(0o444); anchor = hashlib.sha256(raw).hexdigest(); names=[]
    elif action == 'verify' and len(names)>=2 and all(re.fullmatch('[0-9a-f]{64}',v)for v in names[:2]):
        anchor, script_sha, names = names[0], names[1], names[2:]; path = root/ORACLE
        if path.is_symlink() or not path.is_file() or hashlib.sha256(path.read_bytes()).hexdigest() != anchor: raise FixtureError('oracle cache hash changed')
        envelope = json.loads(path.read_bytes())
        if envelope.get('schema')!='addp.line-sampling-oracle/v1' or envelope.get('scope')!=oracle_scope(root,script_sha):raise FixtureError('oracle root/run/source/script scope changed')
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
    if action == 'verify': args = [os.environ['ADDP_ONLINE_RASTER_SAMPLE_LINES_ORACLE_SHA256'],script_sha,*args]
    result = runtime_worker(action, root, args)
    if action == 'seed':
        with state.open('xb') as stream: stream.write(encoded(result))
    elif action != 'cleanup-oracle':
        if result['sources'] != json.loads(state.read_bytes())['sources']: raise FixtureError('source file changed')
        artifacts = Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR'])
        history = secret/(PREFIX+'-archives.json'); old = json.loads(history.read_bytes()) if history.exists() else {'sources': {}, 'targets': {}, 'oracle': {}}
        if action != 'oracle' and not history.is_file(): raise FixtureError('source archive history missing')
        result['archived_oracle']=archive_files(root,artifacts,[ORACLE],old['oracle'],PREFIX+'-oracle')
        result['archived_sources'] = archive_files(root, artifacts, SOURCES, old['sources'], 'raster-sample-lines-sources')
        result['archived_gpkgs'] = archive_files(root/'outputs', artifacts, sys.argv[2:], old['targets'], 'raster-sample-lines-gpkgs')
        history.write_bytes(encoded({'sources': result['archived_sources'], 'targets': result['archived_gpkgs'],'oracle':result['archived_oracle']}))
    return result

if __name__ == '__main__':
    try:
        result = worker(sys.argv[2], Path(sys.argv[3]), sys.argv[4:]) if sys.argv[1:2] == ['--worker'] else main(sys.argv[1])
        print(json.dumps(result, allow_nan=False, sort_keys=True))
    except Exception as error:
        print(f'Point sampling fixture failed: {type(error).__name__}: {error}', file=sys.stderr); raise SystemExit(1)
