#!/usr/bin/env python3
"""Owned slope grids, independent Horn reference and complete original COG checks."""
from __future__ import annotations
import hashlib,json,math,os,re,subprocess,sys,tempfile
from fractions import Fraction as F
from pathlib import Path
PREFIX='slope-orthogonal'
SUITE='raster-slope-orthogonal-persistence'
RUNTIME='addp-raster-slope-orthogonal-runtime'
NODATA=-9999.
GROUPS=('north','rotated','quarter-turn','half-turn','feet','survey','coverage-cross-block','invalid')
CASES={g+'-'+u+'.cog.tif':(g+'.tif',u)for g in GROUPS for u in('degree','percent')}
TARGETS=tuple(CASES)
BAD_GRIDS=('shear','near-orthogonal','reflect-column','reflect-row','geographic','no-crs','no-grid','degenerate')
SOURCES=tuple(g+'.tif'for g in GROUPS+BAD_GRIDS+('unknown-unit','scaled','offset'))
FACTORS={'metre':1.,'foot':.3048,'us_survey_foot':1200/3937}
CACHE_SCHEMA='addp.slope-orthogonal-oracle/v1'
CONTROL_NAME=PREFIX+'-oracle-control.json'
ANCHOR='ADDP_ONLINE_SLOPE_ORTHOGONAL_ORACLE_ANCHOR_SHA256'
class FixtureError(RuntimeError):pass
if sys.argv[1:2]!=['--worker']:
    sys.path.insert(0,str(Path(__file__).resolve().parents[2]))
    from scripts.utils.online_nfs_fixture import NFSExport,FixtureError,run_runtime_worker


def command(args,**kwargs):
    try:return subprocess.run(args,check=True,capture_output=True,text=True,timeout=150,**kwargs).stdout.strip()
    except subprocess.CalledProcessError as error:raise FixtureError('fixture command failed: '+(error.stderr or 'no stderr')[-4000:])from error

def encoded(value):return json.dumps(value,sort_keys=True,separators=(',',':'),allow_nan=False).encode()

def digest(path):
    if path.is_symlink()or not path.is_file()or path.stat().st_size<=0:raise FixtureError('expected positive ordinary owned file')
    return {'size_bytes':path.stat().st_size,'sha256':hashlib.sha256(path.read_bytes()).hexdigest()}

def source_spec(name):
    key=name.removesuffix('.tif');cross=key=='coverage-cross-block'
    axes={'north':(5.,0.,0.,-10.),'quarter-turn':(0.,10.,5.,0.),'half-turn':(-5.,0.,0.,10.),
        'shear':(5.,1.,0.,-10.),'near-orthogonal':(3.,8.,4.,math.nextafter(-6.,0.)),
        'reflect-column':(-3.,8.,-4.,-6.),'reflect-row':(3.,-8.,4.,6.),'degenerate':(3.,6.,4.,8.)}.get(key,(3.,8.,4.,-6.))
    return {'width':519 if cross else 19,'height':515 if cross else 17,'grid':(40.,axes[0],axes[1],80.,axes[2],axes[3]),
        'epsg':4326 if key=='geographic'else 2222 if key=='feet'else 2277 if key=='survey'else 3857,
        'unit':'foot'if key=='feet'else'us_survey_foot'if key=='survey'else'metre'}

def source_arrays(name):
    import numpy as np
    spec=source_spec(name);rows,cols=np.indices((spec['height'],spec['width']))
    metres=((rows%17-8)**2-(cols%13-6)**2).astype(float)
    metres[3:9,3:12]=2*cols[3:9,3:12]+3*rows[3:9,3:12]
    alpha=np.full(metres.shape,255.);mask=np.full(metres.shape,255,np.uint8)
    values=metres/FACTORS[spec['unit']]
    if name=='invalid.tif':values[:]=NODATA
    if name=='coverage-cross-block.tif':
        values[20:35,20:35]=0.;values[127,128]=NODATA;values[511,511]=NODATA
        values[508,512]=np.inf;values[512,509]=np.nan;alpha[509,510]=0.;alpha[511,513]=np.nan
        alpha[500,500]=128.;mask[513,512]=0
    return values,alpha,mask

def parameters(name):
    source,unit=CASES[name]
    return {'band':2,'elevation_unit':source_spec(source)['unit'],'slope_unit':unit}


def seed(root):
    import numpy as np
    from osgeo import gdal,osr
    if any((root/n).exists()for n in SOURCES)or list((root/'outputs').iterdir()):raise FixtureError('refusing existing data')
    for name in SOURCES:
        spec=source_spec(name);values,alpha,mask=source_arrays(name)
        ds=gdal.GetDriverByName('GTiff').Create(str(root/name),spec['width'],spec['height'],4,gdal.GDT_Float64,['TILED=YES','COMPRESS=DEFLATE'])
        crs=osr.SpatialReference();crs.ImportFromEPSG(spec['epsg'])
        if name!='no-crs.tif':ds.SetSpatialRef(crs)
        if name!='no-grid.tif':ds.SetGeoTransform(spec['grid'])
        for i,data in enumerate((np.full(values.shape,17.),values,np.ones(values.shape),alpha),1):
            band=ds.GetRasterBand(i);band.WriteArray(data);band.SetNoDataValue(NODATA);band=None
        ds.GetRasterBand(2).SetUnitType('fathom' if name=='unknown-unit.tif' else spec['unit']);
        if name=='scaled.tif':ds.GetRasterBand(2).SetScale(2.)
        if name=='offset.tif':ds.GetRasterBand(2).SetOffset(1.)
        ds.GetRasterBand(4).SetColorInterpretation(gdal.GCI_AlphaBand)
        with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):ds.CreateMaskBand(gdal.GMF_PER_DATASET)
        ds.GetRasterBand(1).GetMaskBand().WriteArray(mask);ds=None
        ds=gdal.Open(str(root/name))
        if (ds.GetFileList()!=[str(root/name)]or not np.array_equal(ds.GetRasterBand(2).ReadAsArray(),values,equal_nan=True)
            or not np.array_equal(ds.GetRasterBand(1).GetMaskBand().ReadAsArray(),mask)
            or not np.array_equal(ds.GetRasterBand(4).ReadAsArray(),alpha,equal_nan=True)
            or ds.GetRasterBand(4).GetColorInterpretation()!=gdal.GCI_AlphaBand
            or ds.GetRasterBand(2).GetUnitType()!=('fathom' if name=='unknown-unit.tif' else spec['unit'])
            or(name!='no-grid.tif'and ds.GetGeoTransform()!=spec['grid'])):raise FixtureError('stored source facts differ')
        ds=None
    return {'sources':{n:digest(root/n)for n in SOURCES}}

def world(grid,point):
    a,b,c,d,e,f=map(F,grid);x,y=map(F,point)
    return [float(a+b*x+c*y),float(d+e*x+f*y)]

def reference(name):
    """Independent Horn nine-point arithmetic; never calls the product or DEMProcessing.

    GDAL 3.8.4 gdaldem_lib.cpp GDALSlopeHornAlg publishes the float input
    addition order, double gradient/angle and final Float32 rounding. Our
    controlled sources retain those original precision facts, not exact-real DEMs.
    """
    import numpy as np
    from osgeo import osr
    source,unit=CASES[name];spec=source_spec(source);values,alpha,mask=source_arrays(source)
    valid=(values!=NODATA)&np.isfinite(values)&np.isfinite(alpha)&(alpha>0)&(mask>0)
    z=np.where(valid,values*FACTORS[spec['unit']],np.nan).astype(np.float32)
    w=[z[dy:z.shape[0]-2+dy,dx:z.shape[1]-2+dx]for dy in range(3)for dx in range(3)]
    complete=np.logical_and.reduce([valid[dy:valid.shape[0]-2+dy,dx:valid.shape[1]-2+dx]for dy in range(3)for dx in range(3)])
    crs=osr.SpatialReference();crs.ImportFromEPSG(spec['epsg']);horizontal=crs.GetLinearUnits();g=spec['grid']
    dx=((w[0]+w[3]+w[3]+w[6])-(w[2]+w[5]+w[5]+w[8])).astype(float)/math.hypot(g[1],g[4])
    dy=((w[6]+w[7]+w[7]+w[8])-(w[0]+w[1]+w[1]+w[2])).astype(float)/-math.hypot(g[2],g[5])
    magnitude=np.sqrt(dx*dx+dy*dy)/(8*horizontal)
    answer=np.full(z.shape,np.nan);inner=answer[1:-1,1:-1]
    keys,inverse=np.unique(magnitude[complete],return_inverse=True)
    rounded=np.array([float(np.float32(math.degrees(math.atan(float(v)))if unit=='degree'else 100*float(v)))for v in keys])
    inner[complete]=rounded[inverse];answer.flags.writeable=False
    return answer


def artifact_expectation(name):
    source=CASES[name][0];s=source_spec(source);g=s['grid'];corners=[world(g,p)for p in((0,0),(s['width'],0),(0,s['height']),(s['width'],s['height']))]
    return {'width':s['width'],'height':s['height'],'band_count':1,'source_crs':'EPSG:'+str(s['epsg']),'extent_srid':s['epsg'],'transform':list(g),'band_nodata':None,
        'extent':[min(p[0]for p in corners),min(p[1]for p in corners),max(p[0]for p in corners),max(p[1]for p in corners)]}


def cache_path(scope):return Path(tempfile.gettempdir())/('addp-slope-orthogonal-oracle-'+hashlib.sha256(encoded(scope)).hexdigest())

def validate_scope(root,scope,sources=True):
    if (scope.get('schema')!=CACHE_SCHEMA or not re.fullmatch(PREFIX+r'-[0-9]+-[0-9]+',scope.get('run_id',''))
        or scope.get('source_root')!=str(root.resolve())or(root/'.addp-online-owner').is_symlink()or(root/'.addp-online-owner').read_text()!=scope['run_id']
        or not re.fullmatch('[0-9a-f]{64}',scope.get('fixture_sha256',''))or set(scope.get('sources',{}))!=set(SOURCES)):
        raise FixtureError('oracle scope differs')
    if sources and scope['sources']!={n:digest(root/n)for n in SOURCES}:raise FixtureError('source file changed')

def cache_directory(scope):
    directory=cache_path(scope);marker=directory/'owner.json'
    if directory.is_symlink()or not directory.is_dir()or directory.stat().st_uid!=os.getuid()or directory.stat().st_mode&0o777!=0o700 or marker.is_symlink()or not marker.is_file()or marker.stat().st_mode&0o777!=0o400 or marker.read_bytes()!=encoded(scope):raise FixtureError('oracle cache owner changed')
    return directory

def prepare_oracle(root,scope):
    import numpy as np
    validate_scope(root,scope);directory=cache_path(scope);directory.mkdir(mode=0o700);marker=directory/'owner.json';marker.write_bytes(encoded(scope));marker.chmod(0o400)
    arrays={};expectations={}
    for name in CASES:
        expected=reference(name);path=directory/(name+'.npy')
        with path.open('xb')as stream:np.save(stream,expected,allow_pickle=False)
        path.chmod(0o400);arrays[name]=digest(path);expectations[name]={'artifact':artifact_expectation(name),'valid_pixels':int(np.isfinite(expected).sum()),'zero_pixels':int((expected==0).sum()),'unit':CASES[name][1]}
    for unit in('degree','percent'):
        original=np.load(directory/('north-'+unit+'.cog.tif.npy'),allow_pickle=False)
        for group in('rotated','quarter-turn','half-turn'):
            if not np.array_equal(original,np.load(directory/(group+'-'+unit+'.cog.tif.npy'),allow_pickle=False),equal_nan=True):raise FixtureError('north-up frozen Horn contrast differs')
    return {'scope':scope,'arrays':arrays,'expectations':expectations}


def expected_array(control,name):
    import numpy as np
    directory=cache_directory(control['scope']);path=directory/(name+'.npy')
    if path.is_symlink()or path.stat().st_mode&0o777!=0o400 or digest(path)!=control['arrays'][name]:raise FixtureError('frozen expected array changed')
    value=np.load(path,allow_pickle=False);s=source_spec(CASES[name][0])
    if value.shape!=(s['height'],s['width'])or value.dtype!=np.dtype('float64'):raise FixtureError('oracle array type or shape changed')
    value.flags.writeable=False;return value

def verify(root,names,control):
    import numpy as np
    from osgeo import gdal,osr
    from osgeo_utils.samples.validate_cloud_optimized_geotiff import validate
    validate_scope(root,control['scope']);directory=cache_directory(control['scope'])
    if set(p.name for p in directory.iterdir())!={'owner.json',*[n+'.npy'for n in control['arrays']]}:raise FixtureError('oracle cache inventory changed')
    if len(set(names))!=len(names)or any(n not in CASES for n in names)or set(p.name for p in root.iterdir())!={*SOURCES,'outputs','.addp-online-owner'}or set(p.name for p in(root/'outputs').iterdir())!=set(names):raise FixtureError('source/output or sidecar inventory differs')
    result={'sources':control['scope']['sources'],'targets':{}}
    for name in names:
        path=root/'outputs'/name;fact=digest(path);expected=expected_array(control,name);spec=artifact_expectation(name);ds=gdal.Open(str(path));band=ds.GetRasterBand(1);crs=osr.SpatialReference();crs.ImportFromEPSG(spec['extent_srid'])
        if (ds.GetDriver().ShortName!='GTiff'or ds.RasterCount!=1 or(ds.RasterXSize,ds.RasterYSize)!=(spec['width'],spec['height'])or list(ds.GetGeoTransform())!=spec['transform']or not ds.GetSpatialRef().IsSame(crs)
            or band.DataType!=gdal.GDT_Float64 or band.GetNoDataValue()is None or not math.isnan(band.GetNoDataValue())or band.GetUnitType()!=CASES[name][1]or band.GetScale()is not None or band.GetOffset()is not None or band.GetColorTable()is not None
            or not np.array_equal(band.ReadAsArray(),expected,equal_nan=True)or not np.array_equal(band.GetMaskBand().ReadAsArray()!=0,np.isfinite(expected))or validate(ds,full_check=True)[1]):raise FixtureError('complete Horn COG pixels/mask/affine/CRS/unit/layout differs')
        previous=expected;h,w=expected.shape;dimensions=[]
        while max(h,w)>128:w,h=max(1,w//2),max(1,h//2);dimensions.append((w,h))
        if band.GetOverviewCount()!=len(dimensions):raise FixtureError('overview count differs')
        for i,(w,h)in enumerate(dimensions):
            level=band.GetOverview(i);ys=np.floor(.5+np.arange(h)*previous.shape[0]/h).astype(int);xs=np.floor(.5+np.arange(w)*previous.shape[1]/w).astype(int);wanted=previous[np.ix_(ys,xs)]
            if(level.XSize,level.YSize)!=(w,h)or not np.array_equal(level.ReadAsArray(),wanted,equal_nan=True)or not np.array_equal(level.GetMaskBand().ReadAsArray()!=0,np.isfinite(wanted)):raise FixtureError('complete nearest overview differs')
            previous=wanted
        result['targets'][name]={**fact,'cog_valid':True,'has_overviews':bool(dimensions),'overview_count':len(dimensions),'valid_pixels':int(np.isfinite(expected).sum()),'zero_pixels':int((expected==0).sum()),'unit':CASES[name][1],'full_pixel_and_mask_verification':True,'independent_horn_verified':True,'nearest_overviews_verified':True};band=ds=None
    for unit in('degree','percent'):
        baseline='north-'+unit+'.cog.tif'
        for group in('rotated','quarter-turn','half-turn'):
            name=group+'-'+unit+'.cog.tif'
            if name in names:
                if baseline not in names:raise FixtureError('north-up actual COG counterpart absent')
                a=gdal.Open(str(root/'outputs'/baseline));b=gdal.Open(str(root/'outputs'/name))
                if not np.array_equal(a.ReadAsArray(),b.ReadAsArray(),equal_nan=True)or not np.array_equal(a.GetRasterBand(1).GetMaskBand().ReadAsArray(),b.GetRasterBand(1).GetMaskBand().ReadAsArray()):raise FixtureError('actual north-up COG contrast differs')
                a=b=None
    result['oracle_cache']={'scope_sha256':hashlib.sha256(encoded(control['scope'])).hexdigest(),'read_only_expected':True,'expected_arrays_loaded':len(names),'full_target_checks':len(names)}
    return result


def archive_files(root,artifacts,verified):
    directory=artifacts/'slope-orthogonal-cogs';directory.mkdir(exist_ok=True);manifest={}
    if directory.is_symlink()or any(p.is_symlink()or not p.is_file()or p.name not in verified['targets']for p in directory.iterdir()):raise FixtureError('archive inventory differs')
    for name,fact in verified['targets'].items():
        path=directory/name
        if not path.exists():
            with (root/'outputs'/name).open('rb')as src,path.open('xb')as dest:
                while data:=src.read(1024*1024):dest.write(data)
        if digest(path)!={k:fact[k]for k in('size_bytes','sha256')}:raise FixtureError('archive original byte hash changed')
        manifest[name]={'path':directory.name+'/'+name,**digest(path)}
    return manifest

def cleanup_oracle(root,control):
    validate_scope(root,control['scope'],sources=False);path=cache_path(control['scope'])
    if not path.exists()and not path.is_symlink():return {'oracle_cache_removed':True}
    directory=cache_directory(control['scope'])
    if any(p.is_dir()and not p.is_symlink()for p in directory.iterdir()):raise FixtureError('unexpected cache subdirectory')
    for p in directory.iterdir():p.unlink()
    directory.rmdir();return {'oracle_cache_removed':True}

def worker(action,root,args):
    if action=='seed':return seed(root)
    control=json.loads(args[0])
    if action=='cleanup-oracle':return cleanup_oracle(root,control)
    if list(Path(tempfile.gettempdir()).glob('addp-raster-*')):raise FixtureError('Runtime raster workspace residual')
    if action=='prepare-oracle':return prepare_oracle(root,control)
    if action!='verify':raise FixtureError('unknown worker action')
    return {**verify(root,args[1:],control),'runtime_workspaces_removed':True}

def guard(env=os.environ):return NFSExport(PREFIX,command).guard(env)

def runtime_worker(action,root,args=()):return run_runtime_worker(command,RUNTIME,SUITE,Path(__file__),action,root,args)

def private_control(secret):
    path=secret/CONTROL_NAME
    if path.is_symlink()or not path.is_file()or path.stat().st_uid!=os.getuid()or path.stat().st_mode&0o777!=0o600:raise FixtureError('private oracle anchor missing')
    control=json.loads(path.read_bytes());scope=control['scope']
    if scope['fixture_sha256']!=digest(Path(__file__))['sha256']or scope['run_id']!=os.environ['ADDP_ONLINE_TEST_RUN_ID']or(os.environ.get(ANCHOR)and digest(path)['sha256']!=os.environ[ANCHOR]):raise FixtureError('oracle anchor changed')
    return path,control

def main(action):
    export=NFSExport(PREFIX,command);secret=export.guard(os.environ);run=os.environ['ADDP_ONLINE_TEST_RUN_ID']
    if action in('start','stop'):return export.lifecycle(action,secret,run)
    root=export.owned_root(secret,run);snapshot=secret/(PREFIX+'-source.json')
    if action=='seed':
        value=runtime_worker(action,root,());snapshot.write_bytes(encoded(value));return value
    if action=='prepare-oracle':
        scope={'schema':CACHE_SCHEMA,'run_id':run,'source_root':str(root.resolve()),'fixture_sha256':digest(Path(__file__))['sha256'],'sources':json.loads(snapshot.read_bytes())['sources']}
        path=secret/CONTROL_NAME;fd=os.open(path,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
        with os.fdopen(fd,'wb')as stream:stream.write(encoded({'scope':scope}))
        control=runtime_worker(action,root,[json.dumps(scope)]);path.write_bytes(encoded(control));path.chmod(0o600)
        return {'sources':scope['sources'],'targets':{},'archived_cogs':{},'expectations':control['expectations'],'scope_sha256':hashlib.sha256(encoded(scope)).hexdigest(),'anchor_sha256':digest(path)['sha256'],'expected_array_count':16,'runtime_workspaces_removed':True}
    if action=='cleanup-oracle'and not(secret/CONTROL_NAME).exists():return {'oracle_cache_removed':True,'not_started':True}
    path,control=private_control(secret)
    if action=='verify'and not os.environ.get(ANCHOR):raise FixtureError('oracle anchor required')
    result=runtime_worker(action,root,[json.dumps(control),*sys.argv[2:]])
    if action=='cleanup-oracle':path.unlink();return result
    if result['sources']!=json.loads(snapshot.read_bytes())['sources']:raise FixtureError('source file changed')
    result['archived_cogs']=archive_files(root,Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR']),result);return result

if __name__=='__main__':
    try:print(json.dumps(worker(sys.argv[2],Path(sys.argv[3]),sys.argv[4:])if sys.argv[1:2]==['--worker']else main(sys.argv[1]),sort_keys=True,allow_nan=False))
    except Exception as error:print('Slope orthogonal fixture failed: '+str(error),file=sys.stderr);raise SystemExit(1)
