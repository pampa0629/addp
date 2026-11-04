#!/usr/bin/env python3
"""Owned IDW sources, independent exhaustive rational oracle and immutable original COGs."""
from __future__ import annotations
from fractions import Fraction as Q
import hashlib
import json
import math
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
SUITE='raster-interpolate-idw-power-persistence'
PREFIX='raster-interpolate-idw-power'
RUNTIME='addp-raster-interpolate-idw-power-runtime'
ORACLE='.raster-interpolate-idw-power-oracle.json'
CASES={'p1-base.cog.tif': ('template.tif', 'points.gpkg', 4.0, 1, None, 1), 'p1-reversed.cog.tif': ('template.tif', 'reversed.gpkg', 4.0, 1, None, 1), 'p1-nearest.cog.tif': ('profile.tif', 'ties.gpkg', 1.0, 2, 2, 1), 'p1-coincident.cog.tif': ('template.tif', 'coincident.gpkg', 4.0, 10, 10, 1), 'p1-empty.cog.tif': ('template.tif', 'empty.gpkg', 4.0, 1, None, 1), 'p1-masked.cog.tif': ('coverage.tif', 'points.gpkg', 4.0, 1, None, 1), 'p1-foot-block.cog.tif': ('rectangular.tif', 'foot.gpkg', 10000.0, 1, 2, 1), 'p1-survey-block.cog.tif': ('sheared.tif', 'survey.gpkg', 10000.0, 1, 2, 1), 'p2-default.cog.tif': ('template.tif', 'points.gpkg', 4.0, 1, None, None), 'p2-explicit.cog.tif': ('template.tif', 'points.gpkg', 4.0, 1, None, 2), 'p2-nearest.cog.tif': ('profile.tif', 'ties.gpkg', 1.0, 2, 2, 2), 'p2-coincident.cog.tif': ('template.tif', 'coincident.gpkg', 4.0, 10, 10, 2), 'p2-empty.cog.tif': ('template.tif', 'empty.gpkg', 4.0, 1, None, 2), 'p2-masked.cog.tif': ('coverage.tif', 'points.gpkg', 4.0, 1, None, 2), 'p2-foot-block.cog.tif': ('rectangular.tif', 'foot.gpkg', 10000.0, 1, 2, 2), 'p2-survey-block.cog.tif': ('sheared.tif', 'survey.gpkg', 10000.0, 1, 2, 2), 'p3-base.cog.tif': ('template.tif', 'points.gpkg', 4.0, 1, None, 3), 'p3-reversed.cog.tif': ('template.tif', 'reversed.gpkg', 4.0, 1, None, 3), 'p3-nearest.cog.tif': ('profile.tif', 'ties.gpkg', 1.0, 2, 2, 3), 'p3-coincident.cog.tif': ('template.tif', 'coincident.gpkg', 4.0, 10, 10, 3), 'p3-empty.cog.tif': ('template.tif', 'empty.gpkg', 4.0, 1, None, 3), 'p3-masked.cog.tif': ('coverage.tif', 'points.gpkg', 4.0, 1, None, 3), 'p3-foot-block.cog.tif': ('rectangular.tif', 'foot.gpkg', 10000.0, 1, 2, 3), 'p3-survey-block.cog.tif': ('sheared.tif', 'survey.gpkg', 10000.0, 1, 2, 3)}
CONTROLS=(('p1-base.cog.tif', 'p1-reversed.cog.tif'), ('p1-base.cog.tif', 'p1-masked.cog.tif'), ('p2-default.cog.tif', 'p2-explicit.cog.tif'), ('p2-default.cog.tif', 'p2-masked.cog.tif'), ('p3-base.cog.tif', 'p3-reversed.cog.tif'), ('p3-base.cog.tif', 'p3-masked.cog.tif'))
FAILURES=('power_null', 'power_true', 'power_false', 'power_zero', 'power_negative', 'power_large', 'power_fraction', 'power_string', 'power_array', 'power_object', 'p1_min_zero', 'p1_max_zero', 'p1_counts_regular', 'p1_counts_empty', 'p1_counts_coincident', 'p1_duplicate_outside', 'p1_outside_null', 'p1_unselected_null', 'p1_line', 'p1_different_crs', 'p2_min_zero', 'p2_max_zero', 'p2_counts_regular', 'p2_counts_empty', 'p2_counts_coincident', 'p2_duplicate_outside', 'p2_outside_null', 'p2_unselected_null', 'p2_line', 'p2_different_crs', 'p3_min_zero', 'p3_max_zero', 'p3_counts_regular', 'p3_counts_empty', 'p3_counts_coincident', 'p3_duplicate_outside', 'p3_outside_null', 'p3_unselected_null', 'p3_line', 'p3_different_crs')
RASTERS=('template.tif', 'coverage.tif', 'profile.tif', 'rectangular.tif', 'sheared.tif')
VECTORS=('points.gpkg', 'reversed.gpkg', 'coincident.gpkg', 'empty.gpkg', 'foot.gpkg', 'survey.gpkg', 'ties.gpkg', 'duplicate_outside.gpkg', 'outside_null.gpkg', 'unselected_null.gpkg', 'line.gpkg', 'different_crs.gpkg')
SOURCES=RASTERS+VECTORS

class FixtureError(RuntimeError):pass
def command(args,**kwargs):return subprocess.run(args,check=True,capture_output=True,text=True,timeout=150,**kwargs).stdout.strip()
if sys.argv[1:2]!=['--worker']:
 sys.path.insert(0,str(Path(__file__).resolve().parents[2]))
 from scripts.utils.online_nfs_fixture import NFSExport,FixtureError,run_runtime_worker
 import importlib
 archive_files=importlib.import_module('business.scripts.online-raster-zonal-percentile-fixture').archive_files

def encoded(value):return (json.dumps(value,allow_nan=False,sort_keys=True)+'\n').encode()
def digest(path):
 if path.is_symlink()or not path.is_file()or path.stat().st_size<=0:raise FixtureError('positive ordinary file required')
 return {'size_bytes':path.stat().st_size,'sha256':hashlib.sha256(path.read_bytes()).hexdigest()}
def guard(env=os.environ):return NFSExport(PREFIX,command).guard(env)
def owned_root(secret):return NFSExport(PREFIX,command).owned_root(secret,os.environ['ADDP_ONLINE_TEST_RUN_ID'])
def raster_spec(name):
 if name not in RASTERS:raise FixtureError('unknown template')
 return {'shape':(2,515)if name=='rectangular.tif'else(515,2)if name=='sheared.tif'else(1,5)if name=='profile.tif'else(9,9),
  'grid':(-1.5,3.,0.,.5,0.,-1.)if name=='profile.tif'else(0.,2.,0.,9.,0.,-3.)if name=='rectangular.tif'else(0.,1.,.5,9.,.25,-1.)if name=='sheared.tif'else(0.,1.,0.,9.,0.,-1.),
  'epsg':2222 if name=='rectangular.tif'else 2277 if name=='sheared.tif'else 3857}

def source_arrays(name):
 import numpy as np
 spec=raster_spec(name);h,w=spec['shape'];row,col=np.indices((h,w));values=(row*3+col).astype(float);alpha=np.full((h,w),128.);mask=np.full((h,w),255,np.uint8)
 if name=='invalid.tif':values[:]=-9999.
 if name=='coverage.tif':mask[:]=0;alpha[:]=0;values[:]=-9999.;values[0,0]=np.nan;alpha[0,1]=np.nan
 return np.stack([values,np.full((h,w),42.),alpha]),mask

def vector_crs(name):return 2222 if name=='foot.gpkg'else 2277 if name=='survey.gpkg'else 4326 if name=='different_crs.gpkg'else 3857
def vector_spec(name):
 from shapely.geometry import Point,LineString
 rows=[(Point(1.5,8.5),0.),(Point(2.5,8.5),9.),(Point(.5,.5),-7.),(Point(2.5,.5),7.)]
 if name=='reversed.gpkg':rows=rows[::-1]
 if name=='coincident.gpkg':rows=[(Point(.5,8.5),0.),(Point(2.5,8.5),-7.)]
 if name=='foot.gpkg':rows=[(Point(1.,7.5),-4.),(Point(1029.,7.5),12.)]
 if name=='survey.gpkg':rows=[(Point(.75,8.625),-4.),(Point(257.75,-505.375),12.)]
 if name=='ties.gpkg':rows=[(Point(-1.,0.),12.),(Point(0.,-1.),16.),(Point(0.,1.),4.),(Point(1.,0.),8.),(Point(-.75,.75),999.),(Point(2.,0.),0.),(Point(math.nextafter(2.,3.),0.),8.),(Point(5.,0.),0.),(Point(math.nextafter(5.,4.),0.),999.),(Point(8.,0.),0.),(Point(10.,0.),8.)]
 if name=='empty.gpkg':rows=[]
 if name=='duplicate_outside.gpkg':rows=[(Point(.5,8.5),0.),(Point(999.,999.),4.),(Point(999.,999.),4.)]
 if name in('outside_null.gpkg','unselected_null.gpkg'):rows=[(Point(.5,8.5),0.),(Point(999.,999.)if name=='outside_null.gpkg'else Point(2.5,8.5),None)]
 if name=='line.gpkg':rows=[(LineString([(0.,0.),(1.,1.)]),4.)]
 return [(geometry,{'height_value':value,'source_row':i+1,'label':'source-'+str(i+1)})for i,(geometry,value)in enumerate(rows)]
def vector_schema(name):return [('height_value','Real'),('source_row','Integer64'),('label','String')]

def seed(root):
 import numpy as np
 from osgeo import gdal,ogr,osr
 if any((root/n).exists()for n in (*SOURCES,ORACLE))or list((root/'outputs').iterdir()):raise FixtureError('refusing to overwrite owned sources or old results')
 for name in RASTERS:
  spec=raster_spec(name);data,mask=source_arrays(name);h,w=spec['shape'];ds=gdal.GetDriverByName('GTiff').Create(str(root/name),w,h,3,gdal.GDT_Float64,['TILED=YES','COMPRESS=DEFLATE'])
  crs=osr.SpatialReference();crs.ImportFromEPSG(spec['epsg']);ds.SetProjection(crs.ExportToWkt());ds.SetGeoTransform(spec['grid'])
  for i,a in enumerate(data,1):
   band=ds.GetRasterBand(i);band.WriteArray(a);band.SetNoDataValue(-9999.);band.SetScale(17.);band.SetOffset(500.);band.SetUnitType('ft')
  ds.GetRasterBand(3).SetColorInterpretation(gdal.GCI_AlphaBand)
  with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):ds.CreateMaskBand(gdal.GMF_PER_DATASET)
  ds.GetRasterBand(1).GetMaskBand().WriteArray(mask);band=ds=None
 for name in VECTORS:
  ds=ogr.GetDriverByName('GPKG').CreateDataSource(str(root/name));crs=osr.SpatialReference();crs.ImportFromEPSG(vector_crs(name));layer=ds.CreateLayer('points',crs,ogr.wkbUnknown,options=['GEOMETRY_NAME=shape','SPATIAL_INDEX=NO','FID=_source_pk'])
  for key,typ in vector_schema(name):layer.CreateField(ogr.FieldDefn(key,{'String':ogr.OFTString,'Integer64':ogr.OFTInteger64,'Real':ogr.OFTReal}[typ]))
  for geometry,props in vector_spec(name):
   feature=ogr.Feature(layer.GetLayerDefn())
   if geometry is not None:feature.SetGeometry(ogr.CreateGeometryFromWkb(geometry.wkb))
   for k,v in props.items():
    if v is None:feature.SetFieldNull(k)
    else:feature.SetField(k,v)
   if layer.CreateFeature(feature)!=ogr.OGRERR_NONE:raise FixtureError('source insert failed')
   feature=None
  layer=ds=None
 return {'sources':{n:digest(root/n)for n in SOURCES}}

def artifact_expectation(name):
 spec=raster_spec(CASES[name][0]);h,w=spec['shape'];a,b,c,d,e,f=spec['grid'];corners=[(a+b*x+c*y,d+e*x+f*y)for x,y in((0,0),(w,0),(0,h),(w,h))]
 return {'width':w,'height':h,'band_count':1,'source_crs':'EPSG:'+str(spec['epsg']),'extent_srid':spec['epsg'],'transform':list(spec['grid']),'band_nodata':None,'extent':[min(p[0]for p in corners),min(p[1]for p in corners),max(p[0]for p in corners),max(p[1]for p in corners)]}

def oracle():
 """Independent all-point radius/order, Fraction p2 and Decimal Euclidean power."""
 from pyproj import CRS
 from decimal import Decimal,localcontext
 def decimal(q):return Decimal(q.numerator)/Decimal(q.denominator)
 results={}
 with localcontext()as context:
  context.prec=2000
  for name,(template,points,radius,minimum,maximum,power)in CASES.items():
   power=2 if power is None else power
   spec=raster_spec(template);h,w=spec['shape'];a,b,c,d,e,f=map(Q,spec['grid']);factor=Q(CRS.from_epsg(spec['epsg']).axis_info[0].unit_conversion_factor);limit=Q(radius)**2;answer=[]
   samples=[(Q(shape.x),Q(shape.y),Q(float(props['height_value'])))for shape,props in vector_spec(points)]
   for row in range(h):
    line=[]
    for col in range(w):
     u,v=Q(2*col+1,2),Q(2*row+1,2);x,y=a+b*u+c*v,d+e*u+f*v;terms=[]
     for px,py,z in samples:
      dist=((px-x)**2+(py-y)**2)*factor**2
      if dist<=limit:terms.append((dist,px,py,z))
     terms.sort()
     if maximum is not None:terms=terms[:maximum]
     if terms and terms[0][0]==0:value=float(terms[0][3])
     elif len(terms)<minimum:value=None
     elif power==2:value=float(sum((z/dist for dist,_,_,z in terms),Q(0))/sum((1/dist for dist,_,_,_ in terms),Q(0)))
     else:
      weights=[1/decimal(dist).sqrt()**power for dist,_,_,_ in terms]
      value=float(sum((decimal(z)*weight for (_,_,_,z),weight in zip(terms,weights)),Decimal(0))/sum(weights))
     line.append(value)
    answer.append(line)
   results[name]=answer
 return results

def verify_sources(root):
 import numpy as np
 from osgeo import gdal,ogr,osr
 for name in RASTERS:
  spec=raster_spec(name);data,mask=source_arrays(name);ds=gdal.Open(str(root/name));crs=osr.SpatialReference();crs.ImportFromEPSG(spec['epsg']);crs.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER);actual=ds.GetSpatialRef();actual.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER)
  if ds is None or ds.GetDriver().ShortName!='GTiff'or ds.RasterCount!=3 or(ds.RasterYSize,ds.RasterXSize)!=spec['shape']or ds.GetGeoTransform()!=spec['grid']or not actual.IsSame(crs)or ds.GetFileList()!=[str(root/name)]:raise FixtureError('template grid/CRS/inventory changed: '+name)
  for i,a in enumerate(data,1):
   band=ds.GetRasterBand(i)
   if band.DataType!=gdal.GDT_Float64 or band.GetNoDataValue()!=-9999. or band.GetScale()!=17. or band.GetOffset()!=500. or band.GetUnitType()!='ft'or band.GetColorInterpretation()!=(gdal.GCI_AlphaBand if i==3 else gdal.GCI_GrayIndex if i==1 else gdal.GCI_Undefined)or not np.array_equal(band.ReadAsArray(),a,equal_nan=True)or not np.array_equal(band.GetMaskBand().ReadAsArray(),mask):raise FixtureError('full template pixel/mask/alpha/scale/unit changed')
  band=ds=None
 for name in VECTORS:
  ds=ogr.Open(str(root/name));layer=ds.GetLayer(0);crs=osr.SpatialReference();crs.ImportFromEPSG(vector_crs(name));actual=layer.GetSpatialRef();actual.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER);crs.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER);definition=layer.GetLayerDefn();schema=[(definition.GetFieldDefn(i).GetName(),definition.GetFieldDefn(i).GetTypeName())for i in range(definition.GetFieldCount())];rows=vector_spec(name)
  if ds.GetLayerCount()!=1 or not actual.IsSame(crs)or layer.GetGeometryColumn()!='shape'or layer.GetFIDColumn()!='_source_pk'or schema!=vector_schema(name)or layer.GetFeatureCount()!=len(rows):raise FixtureError('source point schema/CRS changed')
  for feature,(geometry,props)in zip(layer,rows):
   actual=feature.GetGeometryRef()
   if (actual is None)!=(geometry is None)or(actual is not None and bytes(actual.ExportToWkb())!=geometry.wkb):raise FixtureError('source point full geometry/order changed')
   if {k:feature.GetField(k)for k,_ in schema}!=props:raise FixtureError('source point full attributes changed')
  feature=layer=ds=None
 return {n:digest(root/n)for n in SOURCES}

def overview_count(name):
 h,w=raster_spec(CASES[name][0])['shape'];count=0
 while max(h,w)>128:h,w=max(1,h//2),max(1,w//2);count+=1
 return count

def nearest_overviews(dataset,expected):
    import numpy as np
    band=dataset.GetRasterBand(1);previous=expected;h,w=expected.shape;dimensions=[]
    while max(h,w)>128:w,h=max(1,w//2),max(1,h//2);dimensions.append((w,h))
    if band.GetOverviewCount()!=len(dimensions):raise FixtureError('incorrect exact overview count')
    for i,shape in enumerate(dimensions):
        level=band.GetOverview(i)
        if (level.XSize,level.YSize)!=shape:raise FixtureError('overview dimensions differ')
        # GDAL reuses a preceding overview only while its X size decreases.
        base=previous if previous.shape[1]>shape[0]else expected
        ys=np.floor(.5+np.arange(shape[1])*base.shape[0]/shape[1]).astype(int);xs=np.floor(.5+np.arange(shape[0])*base.shape[1]/shape[0]).astype(int);wanted=base[np.ix_(ys,xs)]
        if not np.array_equal(level.ReadAsArray(),wanted,equal_nan=True) or not np.array_equal(level.GetMaskBand().ReadAsArray()!=0,np.isfinite(wanted)):raise FixtureError('complete nearest overview pixels or mask differ')
        previous=wanted
    level=band=None

def verify(root,names,expectations):
 import numpy as np
 from osgeo import gdal,osr
 from osgeo_utils.samples.validate_cloud_optimized_geotiff import validate
 if len(set(names))!=len(names)or any(n not in CASES for n in names):raise FixtureError('unknown output inventory')
 if {p.name for p in root.iterdir()}!={*SOURCES,'outputs','.addp-online-owner',ORACLE}or {p.name for p in(root/'outputs').iterdir()}!=set(names):raise FixtureError('unexpected private file or output')
 records={}
 result={'sources':verify_sources(root),'targets':{},'full_cog_checks':len(names),'full_source_pixels_verified':True,'full_source_vectors_verified':True}
 for name in names:
  spec=raster_spec(CASES[name][0]);wanted=np.array(expectations[name],dtype=np.float64);path=root/'outputs'/name;ds=gdal.Open(str(path));band=ds.GetRasterBand(1);reference=osr.SpatialReference();reference.ImportFromEPSG(spec['epsg']);reference.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER);actual_crs=ds.GetSpatialRef();actual_crs.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER)
  if ds.GetFileList()!=[str(path)]or ds.RasterCount!=1 or(ds.RasterYSize,ds.RasterXSize)!=spec['shape']or ds.GetGeoTransform()!=spec['grid']or not actual_crs.IsSame(reference)or ds.GetMetadata('IMAGE_STRUCTURE').get('LAYOUT')!='COG'or band.DataType!=gdal.GDT_Float64 or not math.isnan(band.GetNoDataValue())or band.GetScale()is not None or band.GetOffset()is not None or band.GetUnitType():raise FixtureError('COG/grid/CRS/type/NaN/template metadata differs')
  actual=band.ReadAsArray()
  if not np.array_equal(actual,wanted,equal_nan=True)or not np.array_equal(actual==0,wanted==0)or not np.array_equal(band.GetMaskBand().ReadAsArray()!=0,np.isfinite(wanted)):raise FixtureError('complete independent IDW values/mask/zero differ')
  records[name]=actual.copy()
  nearest_overviews(ds,wanted)
  _,errors,_=validate(ds,full_check=True)
  if errors:raise FixtureError('COG layout invalid')
  band=ds=None;result['targets'][name]={**digest(path),'cog_valid':True,'full_pixel_and_mask_verification':True,'independent_scalar_oracle_verified':True,'template_mask_ignored_verified':True,'nearest_overviews_verified':True,'legal_zero_verified':True,'minimum_support_verified':True,'nearest_support_verified':True,'distance_power_verified':True,'power':CASES[name][5]or 2,'power_parameter_omitted':CASES[name][5]is None,'overview_count':overview_count(name),'has_overviews':bool(overview_count(name))}
 controls=[]
 for left,right in CONTROLS:
  if left in records and right in records:
   if not np.array_equal(records[left],records[right],equal_nan=True):raise FixtureError('actual unlimited or point-order COGs differ')
   controls.append([left,right])
 result['actual_control_pairs_verified']=controls
 witness_names=('p1-base.cog.tif','p2-default.cog.tif','p3-base.cog.tif')
 if all(n in records for n in witness_names):
  if [records[n][0,0]for n in witness_names]!=[3.,1.8,1.]:raise FixtureError('actual distance-power analytic witness differs')
  result['actual_power_witness_verified']=list(witness_names)
 else:result['actual_power_witness_verified']=None

 return result

def oracle_scope(root,script_sha):
 marker=root/'.addp-online-owner'
 if root.is_symlink()or root.resolve()!=root or marker.is_symlink()or not marker.is_file()or not re.fullmatch(PREFIX+r'-[0-9]+-[0-9]+',marker.read_text()):raise FixtureError('invalid owned oracle root/run')
 return {'root':str(root),'run_id':marker.read_text(),'fixture_sha256':script_sha,'source_files':{n:digest(root/n)for n in SOURCES}}

def worker(action, root, names):
    if action == 'seed' and not names: return seed(root)
    if action == 'cleanup-oracle':
        path = root/ORACLE
        if path.is_symlink(): raise FixtureError('oracle symlink')
        path.unlink(missing_ok=True); return {'oracle_cache_removed': not path.exists()}
    if action == 'oracle' and len(names)==1 and re.fullmatch('[0-9a-f]{64}',names[0]):
        expectations = oracle(); raw = encoded({'schema':'addp.idw-oracle/v1','scope':oracle_scope(root,names[0]),'expectations':expectations})
        with (root/ORACLE).open('xb') as stream: stream.write(raw)
        (root/ORACLE).chmod(0o444); anchor = hashlib.sha256(raw).hexdigest(); names=[]
    elif action == 'verify' and len(names)>=2 and all(re.fullmatch('[0-9a-f]{64}',v)for v in names[:2]):
        anchor, script_sha, names = names[0], names[1], names[2:]; path = root/ORACLE
        if path.is_symlink() or not path.is_file() or hashlib.sha256(path.read_bytes()).hexdigest() != anchor: raise FixtureError('oracle cache hash changed')
        envelope = json.loads(path.read_bytes())
        if envelope.get('schema')!='addp.idw-oracle/v1' or envelope.get('scope')!=oracle_scope(root,script_sha):raise FixtureError('oracle root/run/source/script scope changed')
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
    if action == 'verify': args = [os.environ['ADDP_ONLINE_RASTER_INTERPOLATE_IDW_POWER_ORACLE_SHA256'],script_sha,*args]
    result = runtime_worker(action, root, args)
    if action == 'seed':
        with state.open('xb') as stream: stream.write(encoded(result))
    elif action != 'cleanup-oracle':
        if result['sources'] != json.loads(state.read_bytes())['sources']: raise FixtureError('source file changed')
        artifacts = Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR'])
        history = secret/(PREFIX+'-archives.json'); old = json.loads(history.read_bytes()) if history.exists() else {'sources': {}, 'targets': {}, 'oracle': {}}
        if action != 'oracle' and not history.is_file(): raise FixtureError('source archive history missing')
        result['archived_oracle']=archive_files(root,artifacts,[ORACLE],old['oracle'],PREFIX+'-oracle')
        result['archived_sources'] = archive_files(root, artifacts, SOURCES, old['sources'], 'raster-interpolate-idw-power-sources')
        result['archived_cogs'] = archive_files(root/'outputs', artifacts, sys.argv[2:], old['targets'], 'raster-interpolate-idw-power-cogs')
        history.write_bytes(encoded({'sources': result['archived_sources'], 'targets': result['archived_cogs'],'oracle':result['archived_oracle']}))
    return result

if __name__ == '__main__':
    try:
        result = worker(sys.argv[2], Path(sys.argv[3]), sys.argv[4:]) if sys.argv[1:2] == ['--worker'] else main(sys.argv[1])
        print(json.dumps(result, allow_nan=False, sort_keys=True))
    except Exception as error:
        print(f'IDW fixture failed: {type(error).__name__}: {error}', file=sys.stderr); raise SystemExit(1)
