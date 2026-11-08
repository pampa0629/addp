#!/usr/bin/env python3
"""Owned orthogonal DEMs, exact cell-clipping oracle and original CSV/COG bytes."""
from __future__ import annotations
import base64,csv,hashlib,io,json,math,os,re,subprocess,sys,tempfile
from decimal import Decimal,localcontext
from fractions import Fraction as F
from pathlib import Path

PREFIX='sight-orthogonal'
SUITE='raster-sight-orthogonal-persistence'
RUNTIME='addp-raster-sight-orthogonal-runtime'
NODATA=-9999.
GROUPS=('north','rotated','quarter-turn','half-turn','subpixel','coverage','feet','survey-cross-block')
CASES={
 'north.csv':('north.tif','sight'), 'north.cog.tif':('north.tif','view'),
 'rotated.csv':('rotated.tif','sight'), 'rotated.cog.tif':('rotated.tif','view'),
 'quarter-turn.csv':('quarter-turn.tif','sight'), 'quarter-turn.cog.tif':('quarter-turn.tif','view'),
 'half-turn.csv':('half-turn.tif','sight'), 'half-turn.cog.tif':('half-turn.tif','view'),
 'subpixel.csv':('subpixel.tif','sight'), 'subpixel.cog.tif':('subpixel.tif','view'),
 'coverage.csv':('coverage.tif','sight'), 'coverage.cog.tif':('coverage.tif','view'),
 'feet.csv':('feet.tif','sight'), 'feet.cog.tif':('feet.tif','view'),
 'survey-cross-block.csv':('survey-cross-block.tif','sight'), 'survey-cross-block.cog.tif':('survey-cross-block.tif','view')}
TARGETS=tuple(CASES)
BAD_GRIDS=('shear','near-orthogonal','reflect-column','reflect-row','geographic','no-crs','no-grid','degenerate')
SOURCES=tuple(g+'.tif'for g in GROUPS)+tuple(g+'.tif'for g in BAD_GRIDS)
FACTORS={'metre':1.,'foot':.3048,'us_survey_foot':1200/3937}
CACHE_SCHEMA='addp.sight-orthogonal-oracle/v1'
CONTROL_NAME=PREFIX+'-oracle-control.json'
ANCHOR='ADDP_ONLINE_SIGHT_ORTHOGONAL_ORACLE_ANCHOR_SHA256'
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
    key=name.removesuffix('.tif');cross=key=='survey-cross-block'
    axes={'north':(5.,0.,0.,-10.),'quarter-turn':(0.,10.,5.,0.),'half-turn':(-5.,0.,0.,10.),
        'shear':(5.,1.,0.,-10.),'near-orthogonal':(3.,8.,4.,math.nextafter(-6.,0.)),
        'reflect-column':(-3.,8.,-4.,-6.),'reflect-row':(3.,-8.,4.,6.),'degenerate':(3.,6.,4.,8.)}.get(key,(3.,8.,4.,-6.))
    return {'width':519 if cross else 19,'height':515 if cross else 17,'grid':(40.,axes[0],axes[1],80.,axes[2],axes[3]),
        'epsg':4326 if key=='geographic'else 2222 if key=='feet'else 2277 if cross else 3857,
        'unit':'foot'if key=='feet'else'us_survey_foot'if cross else'metre'}
def source_arrays(name):
    import numpy as np
    spec=source_spec(name);values=np.zeros((spec['height'],spec['width']));alpha=np.full(values.shape,255.);mask=np.full(values.shape,255,np.uint8)
    row,col=(511,511)if name=='survey-cross-block.tif'else(8,8)
    values[row,col+2]=3.;values[row,col+1]=NODATA;values[row+1,col+1]=NODATA
    if name=='quarter-turn.tif':values[row,col+1]=10.;values[row+1,col+1]=0.
    if name=='coverage.tif':
        values[row,col+1]=NODATA;values[row,col+2]=0.;mask[row-1,col]=0;alpha[row+1,col]=0;alpha[row-1,col+1]=128;values[row-1,col+1]=3.
    if name=='subpixel.tif':values[row-1,col+1]=4.;values[row,col+1]=0.
    return values,alpha,mask

def world(grid,point):
    a,b,c,d,e,f=map(F,grid);x,y=map(F,point)
    return [float(a+b*x+c*y),float(d+e*x+f*y)]
def pixel(grid,point):
    a,b,c,d,e,f=map(F,grid);x,y=map(F,point);det=b*f-c*e
    return ((x-a)*f-(y-d)*c)/det,((y-d)*b-(x-a)*e)/det

def parameters(name):
    source,kind=CASES[name];spec=source_spec(source);x,y=(511.5,511.5)if source=='survey-cross-block.tif'else(8.5,8.5)
    start,target=(x,y),(x+4,y)
    if source=='quarter-turn.tif'and kind=='sight':target=(x+3,y+3)
    if source=='subpixel.tif':start,target=(8.25,8.),(11.75,8.)if kind=='sight'else(8.5,8.5)
    params={'band':2,'elevation_unit':spec['unit'],'observer_xy':world(spec['grid'],start),
        'observer_height_m':0. if source=='half-turn.tif'else 1.,'target_height_m':0. if source=='half-turn.tif'else 1.}
    if kind=='sight':params['target_xy']=world(spec['grid'],target)
    else:
        params['radius_metres']={'quarter-turn.tif':math.nextafter(10.,0.),'half-turn.tif':math.nextafter(10.,math.inf)}.get(source,10.)
    return params

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
        ds.GetRasterBand(2).SetUnitType(spec['unit']);ds.GetRasterBand(4).SetColorInterpretation(gdal.GCI_AlphaBand)
        with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):ds.CreateMaskBand(gdal.GMF_PER_DATASET)
        ds.GetRasterBand(1).GetMaskBand().WriteArray(mask);ds=None
        ds=gdal.Open(str(root/name))
        if (ds.GetFileList()!=[str(root/name)]or not np.array_equal(ds.GetRasterBand(2).ReadAsArray(),values)
            or not np.array_equal(ds.GetRasterBand(1).GetMaskBand().ReadAsArray(),mask)
            or not np.array_equal(ds.GetRasterBand(4).ReadAsArray(),alpha)
            or ds.GetRasterBand(4).GetColorInterpretation()!=gdal.GCI_AlphaBand
            or ds.GetRasterBand(2).GetUnitType()!=spec['unit']
            or(name!='no-grid.tif'and ds.GetGeoTransform()!=spec['grid'])):raise FixtureError('stored source facts differ')
        ds=None
    return {'sources':{n:digest(root/n)for n in SOURCES}}

def ray(values,valid,start,target,heights,vertical):
    """Clip independently against each closed cell, without the product iterator."""
    if start==target:return 1
    h,w=values.shape;start_z=F(values[math.floor(start[1]),math.floor(start[0])].item())*vertical+F(heights[0]);end_z=F(values[math.floor(target[1]),math.floor(target[0])].item())*vertical+F(heights[1]);blocked=missing=False
    for row in range(max(0,math.floor(min(start[1],target[1]))-1),min(h,math.floor(max(start[1],target[1]))+1)):
        for col in range(max(0,math.floor(min(start[0],target[0]))-1),min(w,math.floor(max(start[0],target[0]))+1)):
            enter,leave=F(0),F(1)
            for a,b,low in zip(start,target,(col,row)):
                if a==b:
                    if not low<=a<=low+1:break
                else:
                    first,last=sorted(((low-a)/(b-a),(low+1-a)/(b-a)));enter,leave=max(enter,first),min(leave,last)
            else:
                if enter>=leave:continue
                if not valid[row,col]:missing=True;continue
                ground=F(values[row,col].item())*vertical;left=start_z+(end_z-start_z)*enter;right=start_z+(end_z-start_z)*leave
                blocked|=ground>min(left,right)or(ground==left and enter>0)or(ground==right and leave<1)or ground==(left+right)/2
    return 0 if blocked else 2 if missing else 1

def reference(name):
    import numpy as np
    from osgeo import osr
    source,kind=CASES[name];spec=source_spec(source);values,alpha,mask=source_arrays(source);valid=(values!=NODATA)&(mask>0)&(alpha>0)&np.isfinite(values)&np.isfinite(alpha)
    params=parameters(name);observer=pixel(spec['grid'],params['observer_xy']);vertical=F(FACTORS[spec['unit']]);crs=osr.SpatialReference();crs.ImportFromEPSG(spec['epsg']);horizontal=F(crs.GetLinearUnits())
    _,b,c,_,e,f=map(F,spec['grid'])
    def squared(target):
        dc,dr=target[0]-observer[0],target[1]-observer[1]
        return ((b*dc+c*dr)**2+(e*dc+f*dr)**2)*horizontal**2
    if kind=='sight':
        target=pixel(spec['grid'],params['target_xy']);state=ray(values,valid,observer,target,(params['observer_height_m'],params['target_height_m']),vertical);distance=squared(target)
        with localcontext()as context:
            context.prec=200;distance=float((Decimal(distance.numerator)/Decimal(distance.denominator)).sqrt())
        return {'distance_m':distance,'status':{0:'blocked',1:'visible',2:'indeterminate'}[state]}
    radius=F(params['radius_metres']);answer=np.full(values.shape,255,np.uint8)
    bound=math.ceil(radius/(min(abs(v)for v in(b,c,e,f)if v)*horizontal))+2
    for row in range(max(0,math.floor(observer[1])-bound),min(spec['height'],math.ceil(observer[1])+bound+1)):
        for col in range(max(0,math.floor(observer[0])-bound),min(spec['width'],math.ceil(observer[0])+bound+1)):
            target=(F(2*col+1,2),F(2*row+1,2))
            if valid[row,col]and squared(target)<=radius**2:answer[row,col]=ray(values,valid,observer,target,(params['observer_height_m'],params['target_height_m']),vertical)
    answer.flags.writeable=False;return answer

def artifact_expectation(name):
    source=CASES[name][0];s=source_spec(source);g=s['grid'];corners=[world(g,p)for p in((0,0),(s['width'],0),(0,s['height']),(s['width'],s['height']))]
    return {'width':s['width'],'height':s['height'],'band_count':1,'source_crs':'EPSG:'+str(s['epsg']),'extent_srid':s['epsg'],'transform':list(g),'band_nodata':255,
        'extent':[min(p[0]for p in corners),min(p[1]for p in corners),max(p[0]for p in corners),max(p[1]for p in corners)]}

def cache_path(scope):return Path(tempfile.gettempdir())/('addp-sight-orthogonal-oracle-'+hashlib.sha256(encoded(scope)).hexdigest())
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
    import numpy as np,pandas as pd
    validate_scope(root,scope);directory=cache_path(scope);directory.mkdir(mode=0o700);marker=directory/'owner.json';marker.write_bytes(encoded(scope));marker.chmod(0o400)
    arrays={};expectations={}
    for name,(_,kind)in CASES.items():
        expected=reference(name)
        if kind=='sight':expectations[name]={'row':expected,'preview_row':json.loads(pd.DataFrame([expected]).to_json(orient='records',double_precision=15))[0]}
        else:
            path=directory/(name+'.npy')
            with path.open('xb')as stream:np.save(stream,expected,allow_pickle=False)
            path.chmod(0o400);arrays[name]=digest(path);expectations[name]={'artifact':artifact_expectation(name),'state_counts':{str(code):int((expected==code).sum())for code in(0,1,2,255)}}
    # Actual north-up and rotated output comparisons also run on every reread.
    if expectations['north.csv']!=expectations['rotated.csv']:raise FixtureError('baseline CSV oracle differs')
    if not np.array_equal(np.load(directory/'north.cog.tif.npy',allow_pickle=False),np.load(directory/'rotated.cog.tif.npy',allow_pickle=False)):raise FixtureError('baseline COG oracle differs')
    return {'scope':scope,'arrays':arrays,'expectations':expectations}

def expected_array(control,name):
    import numpy as np
    directory=cache_directory(control['scope']);path=directory/(name+'.npy')
    if path.is_symlink()or path.stat().st_mode&0o777!=0o400 or digest(path)!=control['arrays'][name]:raise FixtureError('frozen expected array changed')
    value=np.load(path,allow_pickle=False);s=source_spec(CASES[name][0])
    if value.shape!=(s['height'],s['width'])or value.dtype!=np.dtype('uint8'):raise FixtureError('oracle array type or shape changed')
    value.flags.writeable=False;return value

def parse_csv(raw,expected):
    try:
        header,row=list(csv.reader(io.StringIO(raw.decode('utf-8'),newline=''),strict=True))
        actual={'distance_m':float(row[0]),'status':row[1]}
        if header!=['distance_m','status']or len(row)!=2 or actual!=expected or not math.isfinite(actual['distance_m'])or actual['distance_m']<=0:raise ValueError()
    except (ValueError,IndexError,UnicodeError,csv.Error):raise FixtureError('complete exact CSV differs')from None
    return actual

def verify(root,names,control):
    import numpy as np
    from osgeo import gdal,osr
    from osgeo_utils.samples.validate_cloud_optimized_geotiff import validate
    validate_scope(root,control['scope']);directory=cache_directory(control['scope'])
    if set(p.name for p in directory.iterdir())!={'owner.json',*[n+'.npy'for n in control['arrays']]}:raise FixtureError('oracle cache inventory changed')
    if len(set(names))!=len(names)or any(n not in CASES for n in names)or set(p.name for p in root.iterdir())!={*SOURCES,'outputs','.addp-online-owner'}or set(p.name for p in(root/'outputs').iterdir())!=set(names):raise FixtureError('source/output or sidecar inventory differs')
    result={'sources':control['scope']['sources'],'targets':{}}
    for name in names:
        path=root/'outputs'/name;fact=digest(path)
        if CASES[name][1]=='sight':
            raw=path.read_bytes();row=parse_csv(raw,control['expectations'][name]['row']);result['targets'][name]={**fact,'row':row,'csv_base64':base64.b64encode(raw).decode(),'complete_csv_verified':True};continue
        expected=expected_array(control,name);spec=artifact_expectation(name);ds=gdal.Open(str(path));band=ds.GetRasterBand(1);crs=osr.SpatialReference();crs.ImportFromEPSG(spec['extent_srid'])
        if (ds.GetDriver().ShortName!='GTiff'or ds.RasterCount!=1 or (ds.RasterXSize,ds.RasterYSize)!=(spec['width'],spec['height'])or list(ds.GetGeoTransform())!=spec['transform']or not ds.GetSpatialRef().IsSame(crs)
            or band.DataType!=gdal.GDT_Byte or band.GetNoDataValue()!=255 or band.GetUnitType()!=''or band.GetScale()is not None or band.GetOffset()is not None or band.GetColorTable()is not None
            or not np.array_equal(band.ReadAsArray(),expected)or not np.array_equal(band.GetMaskBand().ReadAsArray()!=0,expected!=255)or validate(ds,full_check=True)[1]):raise FixtureError('complete COG pixels/mask/affine/CRS/layout differs')
        previous=expected;h,w=expected.shape;dimensions=[]
        while max(h,w)>128:w,h=max(1,w//2),max(1,h//2);dimensions.append((w,h))
        if band.GetOverviewCount()!=len(dimensions):raise FixtureError('overview count differs')
        for i,(w,h)in enumerate(dimensions):
            level=band.GetOverview(i);ys=np.floor(.5+np.arange(h)*previous.shape[0]/h).astype(int);xs=np.floor(.5+np.arange(w)*previous.shape[1]/w).astype(int);wanted=previous[np.ix_(ys,xs)]
            if (level.XSize,level.YSize)!=(w,h)or not np.array_equal(level.ReadAsArray(),wanted)or not np.array_equal(level.GetMaskBand().ReadAsArray()!=0,wanted!=255):raise FixtureError('complete nearest overview differs')
            previous=wanted
        result['targets'][name]={**fact,'cog_valid':True,'has_overviews':bool(dimensions),'overview_count':len(dimensions),'valid_pixels':int((expected!=255).sum()),'state_counts':{str(code):int((expected==code).sum())for code in(0,1,2,255)},'full_pixel_and_mask_verification':True,'independent_cell_clipping_verified':True,'nearest_overviews_verified':True};band=ds=None
    if 'rotated.csv'in names and (root/'outputs/north.csv').read_bytes()!=(root/'outputs/rotated.csv').read_bytes():raise FixtureError('actual north-up CSV contrast differs')
    if 'rotated.cog.tif'in names:
        a=gdal.Open(str(root/'outputs/north.cog.tif'));b=gdal.Open(str(root/'outputs/rotated.cog.tif'))
        if not np.array_equal(a.ReadAsArray(),b.ReadAsArray())or not np.array_equal(a.GetRasterBand(1).GetMaskBand().ReadAsArray(),b.GetRasterBand(1).GetMaskBand().ReadAsArray()):raise FixtureError('actual north-up COG contrast differs')
        a=b=None
    result['oracle_cache']={'scope_sha256':hashlib.sha256(encoded(control['scope'])).hexdigest(),'read_only_expected':True,'expected_arrays_loaded':sum(n.endswith('.tif')for n in names),'full_target_checks':len(names)}
    return result

def archive_files(root,artifacts,verified):
    directory=artifacts/'sight-orthogonal-files';directory.mkdir(exist_ok=True);manifest={}
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
        return {'sources':scope['sources'],'targets':{},'archived_files':{},'expectations':control['expectations'],'scope_sha256':hashlib.sha256(encoded(scope)).hexdigest(),'anchor_sha256':digest(path)['sha256'],'expected_array_count':8,'runtime_workspaces_removed':True}
    if action=='cleanup-oracle'and not(secret/CONTROL_NAME).exists():return {'oracle_cache_removed':True,'not_started':True}
    path,control=private_control(secret)
    if action=='verify'and not os.environ.get(ANCHOR):raise FixtureError('oracle anchor required')
    result=runtime_worker(action,root,[json.dumps(control),*sys.argv[2:]])
    if action=='cleanup-oracle':path.unlink();return result
    if result['sources']!=json.loads(snapshot.read_bytes())['sources']:raise FixtureError('source file changed')
    result['archived_files']=archive_files(root,Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR']),result);return result
if __name__=='__main__':
    try:print(json.dumps(worker(sys.argv[2],Path(sys.argv[3]),sys.argv[4:])if sys.argv[1:2]==['--worker']else main(sys.argv[1]),sort_keys=True,allow_nan=False))
    except Exception as error:print('Sight orthogonal fixture failed: '+str(error),file=sys.stderr);raise SystemExit(1)
