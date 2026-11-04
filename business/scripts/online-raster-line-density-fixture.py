#!/usr/bin/env python3
"""Owned line-density sources, independent exhaustive rational oracle and immutable original COGs."""
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
SUITE='raster-line-density-persistence'
PREFIX='raster-line-density'
RUNTIME='addp-raster-line-density-runtime'
ORACLE='.raster-line-density-oracle.json'
CASES={'base.cog.tif': ('template.tif', 'lines.gpkg', 4.0), 'reversed.cog.tif': ('template.tif', 'reversed.gpkg', 4.0), 'doubled.cog.tif': ('template.tif', 'doubled.gpkg', 4.0), 'retraced.cog.tif': ('template.tif', 'retraced.gpkg', 4.0), 'multipart.cog.tif': ('template.tif', 'multipart.gpkg', 4.0), 'subdivided.cog.tif': ('template.tif', 'subdivided.gpkg', 4.0), 'overlap.cog.tif': ('template.tif', 'overlap.gpkg', 4.0), 'tangent.cog.tif': ('one.tif', 'tangent.gpkg', 1.0), 'inside-tangent.cog.tif': ('one.tif', 'inside-tangent.gpkg', 1.0), 'outside-tangent.cog.tif': ('one.tif', 'outside-tangent.gpkg', 1.0), 'endpoint.cog.tif': ('one.tif', 'endpoint.gpkg', 1.0), 'outside.cog.tif': ('template.tif', 'outside.gpkg', 4.0), 'empty.cog.tif': ('template.tif', 'empty.gpkg', 4.0), 'mask-alpha.cog.tif': ('coverage.tif', 'lines.gpkg', 4.0), 'foot-block.cog.tif': ('rectangular.tif', 'foot.gpkg', 4.0), 'survey-block.cog.tif': ('sheared.tif', 'survey.gpkg', 4.0)}
CONTROLS=(('base.cog.tif', 'reversed.cog.tif'), ('base.cog.tif', 'multipart.cog.tif'), ('base.cog.tif', 'subdivided.cog.tif'), ('base.cog.tif', 'mask-alpha.cog.tif'), ('tangent.cog.tif', 'outside-tangent.cog.tif'), ('tangent.cog.tif', 'endpoint.cog.tif'), ('outside.cog.tif', 'empty.cog.tif'))
SCALE_CONTROLS=(('base.cog.tif', 'doubled.cog.tif', 2), ('base.cog.tif', 'retraced.cog.tif', 2))
FAILURES=('radius_null', 'radius_true', 'radius_false', 'radius_zero', 'radius_negative', 'radius_string', 'radius_array', 'radius_object', 'null_geometry', 'empty_geometry', 'point', 'polygon', 'collection', 'zero_line', 'zero_component', 'z_line', 'outside_z_line', 'outside_zero_line', 'different_crs', 'geographic')
RASTERS=('template.tif', 'coverage.tif', 'rectangular.tif', 'sheared.tif', 'geographic.tif', 'one.tif')
VECTORS=('lines.gpkg', 'reversed.gpkg', 'doubled.gpkg', 'retraced.gpkg', 'multipart.gpkg', 'subdivided.gpkg', 'overlap.gpkg', 'tangent.gpkg', 'inside-tangent.gpkg', 'outside-tangent.gpkg', 'endpoint.gpkg', 'outside.gpkg', 'empty.gpkg', 'foot.gpkg', 'survey.gpkg', 'null_geometry.gpkg', 'empty_geometry.gpkg', 'point.gpkg', 'polygon.gpkg', 'collection.gpkg', 'zero_line.gpkg', 'zero_component.gpkg', 'z_line.gpkg', 'outside_z_line.gpkg', 'outside_zero_line.gpkg', 'different_crs.gpkg', 'geographic.gpkg')
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
 return {'shape':(2,515) if name=='rectangular.tif' else (515,2) if name=='sheared.tif' else (1,1) if name=='one.tif' else (9,9),
  'grid':(-.5,1.,0.,.5,0.,-1.) if name=='one.tif' else (0.,2.,0.,9.,0.,-3.) if name=='rectangular.tif' else (0.,1.,.5,9.,.25,-1.) if name=='sheared.tif' else (0.,1.,0.,9.,0.,-1.),
  'epsg':2222 if name=='rectangular.tif' else 2277 if name=='sheared.tif' else 4326 if name=='geographic.tif' else 3857}
def source_arrays(name):
 import numpy as np
 spec=raster_spec(name);h,w=spec['shape'];row,col=np.indices((h,w));values=(row*3+col).astype(float);alpha=np.full((h,w),128.);mask=np.full((h,w),255,np.uint8)
 if name=='invalid.tif':values[:]=-9999.
 if name=='coverage.tif':mask[:]=0;alpha[:]=0;values[:]=-9999.;values[0,0]=np.nan;alpha[0,1]=np.nan
 return np.stack([values,np.full((h,w),42.),alpha]),mask

def vector_crs(name):return 2222 if name=='foot.gpkg' else 2277 if name=='survey.gpkg' else 32650 if name=='different_crs.gpkg' else 4326 if name=='geographic.gpkg' else 3857
def vector_spec(name):
 from shapely import from_wkt
 from shapely.geometry import LineString,MultiLineString
 base=[LineString([(-4.,8.5),(13.,8.5)]),LineString([(1.5,6.5),(3.5,6.5),(2.5,4.5),(1.5,6.5)]),LineString([(-1.,1.5),(5.,4.5),(-1.,4.5),(5.,1.5)])]
 rows=list(base)
 if name=='reversed.gpkg':rows=rows[::-1]
 elif name=='doubled.gpkg':rows=rows*2
 elif name=='retraced.gpkg':rows=[LineString([*g.coords,*list(g.coords)[-2::-1]]) for g in base]
 elif name=='multipart.gpkg':rows=[MultiLineString(base)]
 elif name=='subdivided.gpkg':
  rows=[]
  for g in base:
   old=list(g.coords);parts=[]
   for a,b in zip(old,old[1:]):parts.extend([a,((a[0]+b[0])/2,(a[1]+b[1])/2)])
   rows.append(LineString([*parts,old[-1]][::-1]))
 elif name=='overlap.gpkg':rows.append(LineString([(0.,8.5),(4.,8.5)]))
 elif name in('tangent.gpkg','inside-tangent.gpkg','outside-tangent.gpkg'):
  y=math.nextafter(1.,0.) if name=='inside-tangent.gpkg' else math.nextafter(1.,math.inf) if name=='outside-tangent.gpkg' else 1.
  rows=[LineString([(-2.,y),(2.,y)])]
 elif name=='endpoint.gpkg':rows=[LineString([(1.,0.),(2.,0.)])]
 elif name=='outside.gpkg':rows=[LineString([(999.,999.),(1000.,1000.)])]
 elif name=='empty.gpkg':rows=[]
 elif name=='foot.gpkg':rows=[LineString([(-10.,7.5),(1050.,7.5)]),LineString([(-10.,4.),(1050.,20.)])]
 elif name=='survey.gpkg':rows=[LineString([(-10.,30.),(257.75,-505.375)]),LineString([(-10.,0.),(270.,-400.)])]
 special={'null_geometry.gpkg':None,'empty_geometry.gpkg':'LINESTRING EMPTY','point.gpkg':'POINT(0 0)','polygon.gpkg':'POLYGON((0 0,1 0,1 1,0 0))','collection.gpkg':'GEOMETRYCOLLECTION(LINESTRING(0 0,1 1))','zero_line.gpkg':'LINESTRING(0 0,0 0)','zero_component.gpkg':'MULTILINESTRING((0 0,1 1),(2 2,2 2))','z_line.gpkg':'LINESTRING Z(0.5 8.5 1,1.5 8.5 2)','outside_z_line.gpkg':'LINESTRING Z(999 999 1,1000 1000 2)','outside_zero_line.gpkg':'LINESTRING(999 999,999 999)'}
 if name in special:rows=[None if special[name] is None else from_wkt(special[name])]
 return [(geometry,{'height_value':(-4. if i%2 else 0.),'source_row':i+1,'label':'source-'+str(i+1)})for i,geometry in enumerate(rows)]
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
  ds=ogr.GetDriverByName('GPKG').CreateDataSource(str(root/name));crs=osr.SpatialReference();crs.ImportFromEPSG(vector_crs(name));layer=ds.CreateLayer('lines',crs,ogr.wkbUnknown,options=['GEOMETRY_NAME=shape','SPATIAL_INDEX=NO','FID=_source_pk'])
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
 """All original segments; independent high-precision quadratic t intersection."""
 from decimal import Decimal as D,localcontext
 from pyproj import CRS
 def decimal(v):
  q=Q(v);return D(q.numerator)/D(q.denominator)
 results={}
 with localcontext() as context:
  context.prec=2000
  for name,(template,lines,radius) in CASES.items():
   spec=raster_spec(template);h,w=spec['shape'];gt=list(map(decimal,spec['grid']));factor=decimal(CRS.from_epsg(spec['epsg']).axis_info[0].unit_conversion_factor);r=decimal(radius)/factor
   segments=[]
   for geometry,_ in vector_spec(lines):
    for part in (geometry.geoms if geometry.geom_type=='MultiLineString' else [geometry]):
     vertices=[tuple(map(decimal,p))for p in part.coords];segments.extend(zip(vertices,vertices[1:]))
   answer=[]
   for row in range(h):
    line=[]
    for col in range(w):
     u,v=D(col)+D('.5'),D(row)+D('.5');x,y=gt[0]+gt[1]*u+gt[2]*v,gt[3]+gt[4]*u+gt[5]*v;total=D(0)
     for (ax,ay),(bx,by) in segments:
      dx,dy=bx-ax,by-ay;a=dx*dx+dy*dy
      if not a:continue
      b=2*((ax-x)*dx+(ay-y)*dy);c=(ax-x)**2+(ay-y)**2-r*r;delta=b*b-4*a*c
      if delta<=0:continue
      left=max(D(0),(-b-delta.sqrt())/(2*a));right=min(D(1),(-b+delta.sqrt())/(2*a))
      if right>left:total+=(right-left)*a.sqrt()*factor
     line.append(float(total/(D.from_float(math.pi)*decimal(radius)**2)))
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
        selected=previous if previous.shape[1]>shape[0] else expected
        ys=np.floor(.5+np.arange(shape[1])*selected.shape[0]/shape[1]).astype(int);xs=np.floor(.5+np.arange(shape[0])*selected.shape[1]/shape[0]).astype(int);wanted=selected[np.ix_(ys,xs)]
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
  if ds.GetFileList()!=[str(path)]or ds.RasterCount!=1 or(ds.RasterYSize,ds.RasterXSize)!=spec['shape']or ds.GetGeoTransform()!=spec['grid']or not actual_crs.IsSame(reference)or ds.GetMetadata('IMAGE_STRUCTURE').get('LAYOUT')!='COG'or band.DataType!=gdal.GDT_Float64 or not math.isnan(band.GetNoDataValue())or band.GetScale()is not None or band.GetOffset()is not None or band.GetUnitType()!='metre/m2':raise FixtureError('COG/grid/CRS/type/NaN/template metadata differs')
  actual=band.ReadAsArray()
  if not np.array_equal(actual,wanted,equal_nan=True)or not np.array_equal(actual==0,wanted==0)or not np.array_equal(band.GetMaskBand().ReadAsArray()!=0,np.isfinite(wanted)):raise FixtureError('complete independent density values/mask/zero differ')
  records[name]=actual.copy()
  nearest_overviews(ds,wanted)
  _,errors,_=validate(ds,full_check=True)
  if errors:raise FixtureError('COG layout invalid')
  band=ds=None;result['targets'][name]={**digest(path),'cog_valid':True,'full_pixel_and_mask_verification':True,'independent_scalar_oracle_verified':True,'template_mask_ignored_verified':True,'nearest_overviews_verified':True,'legal_zero_verified':True,'line_length_and_full_circle_area_verified':True,'unit_metres_per_square_metre_verified':True,'overview_count':overview_count(name),'has_overviews':bool(overview_count(name))}
 controls=[]
 for left,right in CONTROLS:
  if left in records and right in records:
   if not np.array_equal(records[left],records[right],equal_nan=True):raise FixtureError('actual point-order or template-isolation COGs differ')
   controls.append([left,right])
 result['actual_control_pairs_verified']=controls
 scale_proof=[]
 for left,right,multiple in SCALE_CONTROLS:
  if left in records and right in records:
   if not np.array_equal(records[right],records[left]*multiple):raise FixtureError('actual doubled or retraced line density differs')
   scale_proof.append([left,right,multiple])
 result['actual_scale_controls_verified']=scale_proof

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
        expectations = oracle(); raw = encoded({'schema':'addp.line-density-oracle/v1','scope':oracle_scope(root,names[0]),'expectations':expectations})
        with (root/ORACLE).open('xb') as stream: stream.write(raw)
        (root/ORACLE).chmod(0o444); anchor = hashlib.sha256(raw).hexdigest(); names=[]
    elif action == 'verify' and len(names)>=2 and all(re.fullmatch('[0-9a-f]{64}',v)for v in names[:2]):
        anchor, script_sha, names = names[0], names[1], names[2:]; path = root/ORACLE
        if path.is_symlink() or not path.is_file() or hashlib.sha256(path.read_bytes()).hexdigest() != anchor: raise FixtureError('oracle cache hash changed')
        envelope = json.loads(path.read_bytes())
        if envelope.get('schema')!='addp.line-density-oracle/v1' or envelope.get('scope')!=oracle_scope(root,script_sha):raise FixtureError('oracle root/run/source/script scope changed')
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
    if action == 'verify': args = [os.environ['ADDP_ONLINE_RASTER_LINE_DENSITY_ORACLE_SHA256'],script_sha,*args]
    result = runtime_worker(action, root, args)
    if action == 'seed':
        with state.open('xb') as stream: stream.write(encoded(result))
    elif action != 'cleanup-oracle':
        if result['sources'] != json.loads(state.read_bytes())['sources']: raise FixtureError('source file changed')
        artifacts = Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR'])
        history = secret/(PREFIX+'-archives.json'); old = json.loads(history.read_bytes()) if history.exists() else {'sources': {}, 'targets': {}, 'oracle': {}}
        if action != 'oracle' and not history.is_file(): raise FixtureError('source archive history missing')
        result['archived_oracle']=archive_files(root,artifacts,[ORACLE],old['oracle'],PREFIX+'-oracle')
        result['archived_sources'] = archive_files(root, artifacts, SOURCES, old['sources'], 'raster-line-density-sources')
        result['archived_cogs'] = archive_files(root/'outputs', artifacts, sys.argv[2:], old['targets'], 'raster-line-density-cogs')
        history.write_bytes(encoded({'sources': result['archived_sources'], 'targets': result['archived_cogs'],'oracle':result['archived_oracle']}))
    return result

if __name__ == '__main__':
    try:
        result = worker(sys.argv[2], Path(sys.argv[3]), sys.argv[4:]) if sys.argv[1:2] == ['--worker'] else main(sys.argv[1])
        print(json.dumps(result, allow_nan=False, sort_keys=True))
    except Exception as error:
        print(f'Line-density fixture failed: {type(error).__name__}: {error}', file=sys.stderr); raise SystemExit(1)
