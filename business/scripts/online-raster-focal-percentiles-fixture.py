#!/usr/bin/env python3
"""Owned multi-percentile COGs with exhaustive independent rational expectations."""
from __future__ import annotations
from fractions import Fraction as Q
from functools import lru_cache
import hashlib,json,math,os,re,subprocess,sys,tempfile
from pathlib import Path
SUITE='raster-focal-percentiles-persistence'
PREFIX='raster-focal-percentiles'
RUNTIME='addp-raster-focal-percentiles-runtime'
ORACLE='.raster-focal-percentiles-oracle.json'
PERCENTILES=[0.,25.,33.3,50.,75.,100.]
# Source, window kind/size or metres, computation path. Controls retain their
# existing empty band descriptions; batch descriptions are asserted separately.
CASES={
 'square3.cog.tif':('values.tif','square_pixels',3,'batch'),
 'square3-stack.cog.tif':('values.tif','square_pixels',3,'singles'),
 'square5.cog.tif':('values.tif','square_pixels',5,'batch'),
 'square5-stack.cog.tif':('values.tif','square_pixels',5,'singles'),
 'square3-median.cog.tif':('values.tif','square_pixels',3,'median'),
 'square5-median.cog.tif':('values.tif','square_pixels',5,'median'),
 'circle.cog.tif':('metre.tif','circle_metres',3.,'batch'),
 'circle-stack.cog.tif':('metre.tif','circle_metres',3.,'singles'),
 'circle-median.cog.tif':('metre.tif','circle_metres',3.,'median'),
 'foot.cog.tif':('foot.tif','circle_metres',1.5,'batch'),
 'survey-foot.cog.tif':('survey.tif','circle_metres',1.5,'batch'),
 'reclassified.cog.tif':('values.tif','square_pixels',3,'reclassify'),
 'int64-unlocated.cog.tif':('int64.tif','square_pixels',3,'batch'),
 'uint64.cog.tif':('uint64.tif','square_pixels',5,'batch'),
 'cross-block.cog.tif':('cross.tif','square_pixels',5,'batch'),
 'invalid.cog.tif':('invalid.tif','square_pixels',3,'batch'),
}
FAILURES=('list_missing','list_null','list_empty','scalar_number','scalar_boolean','scalar_string','scalar_object',
 'item_boolean','item_string','item_null','item_list','item_object','item_low','item_high','duplicate','descending',
 'unused','unused_null','band_zero','band_high','band_boolean','band_alpha','square_size','square_radius',
 'circle_window','circle_radius_missing','circle_size','circle_geographic')
SOURCES=('values.tif','metre.tif','foot.tif','survey.tif','int64.tif','uint64.tif','cross.tif','invalid.tif','geographic.tif')
COMPARISONS=(('square3.cog.tif','square3-stack.cog.tif'),('square5.cog.tif','square5-stack.cog.tif'),('circle.cog.tif','circle-stack.cog.tif'))
MEDIANS=(('square3.cog.tif','square3-median.cog.tif'),('square5.cog.tif','square5-median.cog.tif'),('circle.cog.tif','circle-median.cog.tif'))
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
 if name not in SOURCES:raise FixtureError('unknown exact percentile source')
 return {'shape':(515,519)if name=='cross.tif'else(129,131)if name=='int64.tif'else(9,13),
  'grid':None if name=='int64.tif'else(500000.,2.,0.,3000000.,0.,-3.)if name in('metre.tif','foot.tif','survey.tif','geographic.tif')else(500000.,2.,.25,3000000.,-.5,-3.),
  'epsg':0 if name=='int64.tif'else 2222 if name=='foot.tif'else 2277 if name=='survey.tif'else 4326 if name=='geographic.tif'else 3857,
  'nodata':-2**63 if name=='int64.tif'else 2**64-2 if name=='uint64.tif'else-2.**40}
def source_arrays(name):
 import numpy as np
 spec=raster_spec(name);h,w=spec['shape'];y,x=np.indices((h,w));values=((x%7)*3+(y%5)*5-20).astype(float)/4
 values[0,:4]=[0.,1.,8.,9.];values[2:5,3:6]=spec['nodata'];values[3,4]=0.
 alpha=np.full((h,w),255.);mask=np.full((h,w),255,np.uint8);alpha[5,5]=0;alpha[5,6]=128.;mask[6,5]=0
 values[6,7]=np.nan;values[7,8]=np.inf;alpha[7,7]=np.nan;alpha[6,6]=-1.;alpha[5,7]=np.inf
 if name=='invalid.tif':values[:]=spec['nodata']
 if name in('int64.tif','uint64.tif'):
  dtype=np.int64 if name=='int64.tif'else np.uint64;values=np.full((h,w),7,dtype)
  values[h-2,w-2]=spec['nodata'];values[h-2,w-1]=-2**63+1 if name=='int64.tif'else 2**64-3
  alpha=np.full((h,w),255,dtype);alpha[5,5]=0
 if name=='cross.tif':
  values[254:259,254:259]=spec['nodata'];values[256,256:260]=[0.,1.,8.,9.]
 return values,alpha,mask

def seed(root):
 import numpy as np
 from osgeo import gdal,osr
 if any((root/n).exists()for n in(*SOURCES,ORACLE))or list((root/'outputs').iterdir()):raise FixtureError('refusing to overwrite owned sources or old results')
 for name in SOURCES:
  spec=raster_spec(name);values,alpha,mask=source_arrays(name);h,w=spec['shape'];dtype=gdal.GDT_Int64 if name=='int64.tif'else gdal.GDT_UInt64 if name=='uint64.tif'else gdal.GDT_Float64
  ds=gdal.GetDriverByName('GTiff').Create(str(root/name),w,h,4,dtype,['TILED=YES','COMPRESS=DEFLATE'])
  if spec['grid']is not None:
   crs=osr.SpatialReference();crs.ImportFromEPSG(spec['epsg']);ds.SetSpatialRef(crs);ds.SetGeoTransform(spec['grid'])
  for i,data in enumerate((np.full(values.shape,42 if dtype!=gdal.GDT_Float64 else 42.25),values,np.full(values.shape,17),alpha),1):
   band=ds.GetRasterBand(i);band.WriteArray(data)
   if name=='int64.tif':band.SetNoDataValueAsInt64(spec['nodata'])
   elif name=='uint64.tif':band.SetNoDataValueAsUInt64(spec['nodata'])
   else:band.SetNoDataValue(spec['nodata'])
  if name=='values.tif':ds.GetRasterBand(2).SetScale(2.);ds.GetRasterBand(2).SetOffset(7.)
  ds.GetRasterBand(4).SetColorInterpretation(gdal.GCI_AlphaBand)
  with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):
   if ds.CreateMaskBand(gdal.GMF_PER_DATASET)!=gdal.CE_None:raise FixtureError('internal source mask creation failed')
  ds.GetRasterBand(1).GetMaskBand().WriteArray(mask);band=ds=None
 return {'sources':verify_sources(root)}

def same_crs(ds,epsg):
 from osgeo import osr
 actual=ds.GetSpatialRef()
 if not epsg:return actual is None
 if actual is None:return False
 wanted=osr.SpatialReference();wanted.ImportFromEPSG(epsg);actual.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER);wanted.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER)
 return bool(actual.IsSame(wanted))
def verify_sources(root):
 import numpy as np
 from osgeo import gdal
 for name in SOURCES:
  spec=raster_spec(name);values,alpha,mask=source_arrays(name);ds=gdal.Open(str(root/name));dtype=gdal.GDT_Int64 if name=='int64.tif'else gdal.GDT_UInt64 if name=='uint64.tif'else gdal.GDT_Float64
  if ds is None or ds.RasterCount!=4 or(ds.RasterYSize,ds.RasterXSize)!=spec['shape']or ds.GetGeoTransform(can_return_null=True)!=spec['grid']or not same_crs(ds,spec['epsg'])or ds.GetFileList()!=[str(root/name)]:raise FixtureError('source grid/CRS/inventory changed')
  for i,wanted in enumerate((np.full(values.shape,42 if dtype!=gdal.GDT_Float64 else 42.25),values,np.full(values.shape,17),alpha),1):
   band=ds.GetRasterBand(i);nd=band.GetNoDataValueAsInt64()if name=='int64.tif'else band.GetNoDataValueAsUInt64()if name=='uint64.tif'else band.GetNoDataValue()
   if band.DataType!=dtype or nd!=spec['nodata']or band.GetColorInterpretation()!=(gdal.GCI_AlphaBand if i==4 else gdal.GCI_GrayIndex if i==1 else gdal.GCI_Undefined)or not np.array_equal(band.ReadAsArray(),wanted,equal_nan=True)or not np.array_equal(band.GetMaskBand().ReadAsArray(),mask)or band.GetScale()!=(2. if name=='values.tif'and i==2 else None)or band.GetOffset()!=(7. if name=='values.tif'and i==2 else None)or band.GetUnitType():raise FixtureError('complete source values/mask/alpha/NoData/scale changed')
  band=ds=None
 return {n:digest(root/n)for n in SOURCES}

def class_rules():return [{'min':None,'max':0,'class':0},{'min':0,'max':10,'class':10},{'min':10,'max':None,'class':20}]
def band_count(name):return 1 if CASES[name][3]=='median'else len(PERCENTILES)
def descriptions(name):return ['percentile='+repr(p)for p in PERCENTILES]if CASES[name][3]in('batch','reclassify')else ['']*band_count(name)
def artifact_expectation(name):
 spec=raster_spec(CASES[name][0]);h,w=spec['shape'];grid=spec['grid'];corners=[]
 if grid is not None:
  a,b,c,d,e,f=grid;corners=[(a+b*x+c*y,d+e*x+f*y)for x,y in((0,0),(w,0),(0,h),(w,h))]
 return {'width':w,'height':h,'band_count':band_count(name),'source_crs':'EPSG:'+str(spec['epsg'])if spec['epsg']else'','extent_srid':spec['epsg'],'transform':list(grid)if grid is not None else[],'band_nodata':None,'extent':[min(p[0]for p in corners),min(p[1]for p in corners),max(p[0]for p in corners),max(p[1]for p in corners)]if corners else[]}

def offsets(name):
 from pyproj import CRS
 source,kind,size,_=CASES[name]
 if kind=='square_pixels':return [(dy,dx)for dy in range(-size//2+1,size//2+1)for dx in range(-size//2+1,size//2+1)]
 spec=raster_spec(source);factor=Q(CRS.from_epsg(spec['epsg']).axis_info[0].unit_conversion_factor);dx=Q(spec['grid'][1])*factor;dy=Q(-spec['grid'][5])*factor
 return [(y,x)for y in range(-2,3)for x in range(-2,3)if(x*dx)**2+(y*dy)**2<=Q(size)**2]
@lru_cache(maxsize=16)
def expected_pixels(name):
 """Enumerate all actual neighbours; memoize only identical sorted populations."""
 import numpy as np
 source,_,_,stage=CASES[name];spec=raster_spec(source);raw,alpha,mask=source_arrays(source)
 valid=np.isfinite(raw)&(raw!=spec['nodata'])&np.isfinite(alpha)&(alpha>0)&(mask!=0);values=raw.astype(np.float64)
 if stage=='reclassify':values=np.where(values<0,0.,np.where(values<10,10.,20.))
 ps=[50.]if stage=='median'else PERCENTILES;h,w=values.shape;answer=np.full((len(ps),h,w),np.nan);known={};neighbours=offsets(name)
 for y,x in zip(*np.nonzero(valid)):
  sample=tuple(sorted(float(values[y+dy,x+dx])for dy,dx in neighbours if 0<=y+dy<h and 0<=x+dx<w and valid[y+dy,x+dx]))
  if sample not in known:
   row=[]
   for p in ps:
    pos=(len(sample)-1)*Q(p)/100;i=pos.numerator//pos.denominator;t=pos-i
    row.append(float(Q(sample[i])*(1-t)+Q(sample[i+bool(t)])*t))
   known[sample]=row
  answer[:,y,x]=known[sample]
 answer.flags.writeable=False;return answer

def oracle():
 import numpy as np
 return {name:np.where(np.isfinite(expected_pixels(name)),expected_pixels(name),None).tolist()for name in CASES}
def assert_pixels(actual,wanted,ps):
 import numpy as np
 if actual.shape!=wanted.shape or not np.array_equal(np.isfinite(actual),np.isfinite(wanted))or not np.array_equal(actual==0,wanted==0):raise FixtureError('complete pixel mask/zero/shape differs')
 for i,p in enumerate(ps):
  equal=np.array_equal(actual[i],wanted[i],equal_nan=True)if p in(0,25,50,75,100)else np.allclose(actual[i],wanted[i],rtol=4e-15,atol=0.,equal_nan=True)
  if not equal:raise FixtureError('complete independent linear percentile values differ')

def overview_count(name):
 h,w=raster_spec(CASES[name][0])['shape'];count=0
 while max(h,w)>128:h,w=max(1,h//2),max(1,w//2);count+=1
 return count

def nearest_overviews(band,expected,ps):
 import numpy as np
 previous=expected;h,w=expected.shape[1:];dimensions=[]
 while max(h,w)>128:w,h=max(1,w//2),max(1,h//2);dimensions.append((w,h))
 if band.GetOverviewCount()!=len(dimensions):raise FixtureError('incorrect exact overview count')
 for i,shape in enumerate(dimensions):
  level=band.GetOverview(i)
  if(level.XSize,level.YSize)!=shape:raise FixtureError('overview dimensions differ')
  ys=np.floor(.5+np.arange(shape[1])*previous.shape[1]/shape[1]).astype(int);xs=np.floor(.5+np.arange(shape[0])*previous.shape[2]/shape[0]).astype(int);wanted=previous[:,ys[:,None],xs]
  assert_pixels(level.ReadAsArray()[None,:,:],wanted,ps)
  if not np.array_equal(level.GetMaskBand().ReadAsArray()!=0,np.isfinite(wanted[0])):raise FixtureError('complete overview mask differs')
  previous=wanted

def verify(root,names,expectations):
 import numpy as np
 from osgeo import gdal
 from osgeo_utils.samples.validate_cloud_optimized_geotiff import validate
 if len(set(names))!=len(names)or any(n not in CASES for n in names):raise FixtureError('unknown output inventory')
 if {p.name for p in root.iterdir()}!={*SOURCES,'outputs','.addp-online-owner',ORACLE}or {p.name for p in(root/'outputs').iterdir()}!=set(names):raise FixtureError('unexpected private file or output')
 result={'sources':verify_sources(root),'targets':{},'full_cog_checks':len(names),'full_source_pixels_verified':True};actual_outputs={}
 for name in names:
  spec=raster_spec(CASES[name][0]);wanted=np.array(expectations[name],dtype=np.float64);path=root/'outputs'/name;ds=gdal.Open(str(path));ps=[50.]if CASES[name][3]=='median'else PERCENTILES
  if ds is None or ds.GetFileList()!=[str(path)]or ds.RasterCount!=band_count(name)or(ds.RasterYSize,ds.RasterXSize)!=spec['shape']or ds.GetGeoTransform(can_return_null=True)!=spec['grid']or not same_crs(ds,spec['epsg'])or ds.GetMetadata('IMAGE_STRUCTURE').get('LAYOUT')!='COG':raise FixtureError('complete COG/grid/CRS/band count differs')
  values=[]
  for i in range(1,ds.RasterCount+1):
   band=ds.GetRasterBand(i)
   if band.DataType!=gdal.GDT_Float64 or not math.isnan(band.GetNoDataValue())or band.GetScale()is not None or band.GetOffset()is not None or band.GetUnitType()or band.GetColorInterpretation()==gdal.GCI_AlphaBand or band.GetDescription()!=descriptions(name)[i-1]:raise FixtureError('COG type/NaN/description/metadata differs')
   data=band.ReadAsArray();assert_pixels(data[None,:,:],wanted[i-1:i],[ps[i-1]])
   if not np.array_equal(band.GetMaskBand().ReadAsArray()!=0,np.isfinite(wanted[i-1])):raise FixtureError('complete band mask differs')
   nearest_overviews(band,wanted[i-1:i],[ps[i-1]]);values.append(data)
  actual_outputs[name]=np.stack(values);_,errors,_=validate(ds,full_check=True)
  if errors:raise FixtureError('COG layout invalid')
  band=ds=None;result['targets'][name]={**digest(path),'cog_valid':True,'full_pixel_and_mask_verification':True,'independent_scalar_oracle_verified':True,'band_order_and_descriptions_verified':True,'nearest_overviews_verified':True,'legal_zero_verified':True,'band_count':band_count(name),'band_descriptions':descriptions(name),'overview_count':overview_count(name),'has_overviews':bool(overview_count(name))}
 for a,b in COMPARISONS:
  if a in names and b in names and not np.array_equal(actual_outputs[a],actual_outputs[b],equal_nan=True):raise FixtureError('actual batch and single-stack controls differ')
 for a,b in MEDIANS:
  if a in names and b in names and not np.array_equal(actual_outputs[a][3],actual_outputs[b][0],equal_nan=True):raise FixtureError('actual P50 and median controls differ')
 result['actual_control_pairs_verified']=[list(pair)for pair in(*COMPARISONS,*MEDIANS)if all(n in names for n in pair)]
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
        expectations = oracle(); raw = encoded({'schema':'addp.focal-percentiles-oracle/v1','scope':oracle_scope(root,names[0]),'expectations':expectations})
        with (root/ORACLE).open('xb') as stream: stream.write(raw)
        (root/ORACLE).chmod(0o444); anchor = hashlib.sha256(raw).hexdigest(); names=[]
    elif action == 'verify' and len(names)>=2 and all(re.fullmatch('[0-9a-f]{64}',v)for v in names[:2]):
        anchor, script_sha, names = names[0], names[1], names[2:]; path = root/ORACLE
        if path.is_symlink() or not path.is_file() or hashlib.sha256(path.read_bytes()).hexdigest() != anchor: raise FixtureError('oracle cache hash changed')
        envelope = json.loads(path.read_bytes())
        if envelope.get('schema')!='addp.focal-percentiles-oracle/v1' or envelope.get('scope')!=oracle_scope(root,script_sha):raise FixtureError('oracle root/run/source/script scope changed')
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
    if action == 'verify': args = [os.environ['ADDP_ONLINE_RASTER_FOCAL_PERCENTILES_ORACLE_SHA256'],script_sha,*args]
    result = runtime_worker(action, root, args)
    if action == 'seed':
        with state.open('xb') as stream: stream.write(encoded(result))
    elif action != 'cleanup-oracle':
        if result['sources'] != json.loads(state.read_bytes())['sources']: raise FixtureError('source file changed')
        artifacts = Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR'])
        history = secret/(PREFIX+'-archives.json'); old = json.loads(history.read_bytes()) if history.exists() else {'sources': {}, 'targets': {}, 'oracle': {}}
        if action != 'oracle' and not history.is_file(): raise FixtureError('source archive history missing')
        result['archived_oracle']=archive_files(root,artifacts,[ORACLE],old['oracle'],PREFIX+'-oracle')
        result['archived_sources'] = archive_files(root, artifacts, SOURCES, old['sources'], 'raster-focal-percentiles-sources')
        result['archived_cogs'] = archive_files(root/'outputs', artifacts, sys.argv[2:], old['targets'], 'raster-focal-percentiles-cogs')
        history.write_bytes(encoded({'sources': result['archived_sources'], 'targets': result['archived_cogs'],'oracle':result['archived_oracle']}))
    return result

if __name__ == '__main__':
    try:
        result = worker(sys.argv[2], Path(sys.argv[3]), sys.argv[4:]) if sys.argv[1:2] == ['--worker'] else main(sys.argv[1])
        print(json.dumps(result, allow_nan=False, sort_keys=True))
    except Exception as error:
        print(f'multi-percentile fixture failed: {type(error).__name__}: {error}', file=sys.stderr); raise SystemExit(1)
