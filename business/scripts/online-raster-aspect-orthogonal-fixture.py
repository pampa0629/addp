#!/usr/bin/env python3
"""Owned aspect grids, independent Horn reference and complete original COG checks."""
from __future__ import annotations
import hashlib,json,math,os,re,subprocess,sys,tempfile
from fractions import Fraction as F
from pathlib import Path
PREFIX='aspect-orthogonal'
SUITE='raster-aspect-orthogonal-persistence'
RUNTIME='addp-raster-aspect-orthogonal-runtime'
NODATA=-9999.
# Each case defines its own source grid and one COG.
PLANES = {
    'north-n': ('north', 0.), 'north-e': ('north', 90.),
    'north-s': ('north', 180.), 'north-w': ('north', 270.),
    'rotated-n': ('rotated', 0.), 'rotated-e': ('rotated', 90.),
    'rotated-s': ('rotated', 180.), 'rotated-w': ('rotated', 270.),
    'quarter-turn': ('quarter-turn', 45.), 'half-turn': ('half-turn', 225.),
    'wrap-west': ('reverse', 359.95), 'wrap-east': ('reverse', .05),
    'feet': ('rotated', 35.), 'survey': ('reverse', 310.),
}
CASES = {
    **{name + '.cog.tif': (name + '.tif', 'degree') for name in PLANES},
    'coverage-cross-block.cog.tif': ('coverage-cross-block.tif', 'degree'),
    'invalid.cog.tif': ('invalid.tif', 'degree'),
}
TARGETS = tuple(CASES)
BAD_GRIDS = ('shear', 'near-orthogonal', 'reflect-column', 'reflect-row',
             'geographic', 'no-crs', 'no-grid', 'degenerate')
SOURCES = tuple(source for source, _ in CASES.values()) + tuple(
    name + '.tif' for name in (*BAD_GRIDS, 'unknown-unit', 'scaled', 'offset')
)
ANGLE_LIMIT_DEGREES = 5e-5
FACTORS={'metre':1.,'foot':.3048,'us_survey_foot':1200/3937}
CACHE_SCHEMA='addp.aspect-orthogonal-oracle/v1'
CONTROL_NAME=PREFIX+'-oracle-control.json'
ANCHOR='ADDP_ONLINE_ASPECT_ORTHOGONAL_ORACLE_ANCHOR_SHA256'
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
    orientation = PLANES[key][0] if key in PLANES else key
    axes={'north':(5.,0.,0.,-10.),'quarter-turn':(0.,10.,5.,0.),'half-turn':(-5.,0.,0.,10.),'reverse':(3.,-8.,-4.,-6.),
        'shear':(5.,1.,0.,-10.),'near-orthogonal':(3.,8.,4.,math.nextafter(-6.,0.)),
        'reflect-column':(-3.,8.,-4.,-6.),'reflect-row':(3.,-8.,4.,6.),'degenerate':(3.,6.,4.,8.)}.get(orientation,(3.,8.,4.,-6.))
    return {'width':519 if cross else 19,'height':515 if cross else 17,'grid':(40.,axes[0],axes[1],80.,axes[2],axes[3]),
        'epsg':4326 if key=='geographic'else 2222 if key=='feet'else 2277 if key=='survey'else 3857,
        'unit':'us_survey_foot'if key=='feet'else'foot'if key=='survey'else'metre'}

def source_arrays(name):
    import numpy as np
    spec=source_spec(name);rows,cols=np.indices((spec['height'],spec['width']))
    metres=((rows%17-8)**2-(cols%13-6)**2).astype(float)
    metres[3:9,3:12]=2*cols[3:9,3:12]+3*rows[3:9,3:12]
    key = name.removesuffix('.tif')
    if key in PLANES:
        from osgeo import osr
        reference = osr.SpatialReference()
        reference.ImportFromEPSG(spec['epsg'])
        _, ux, vx, _, uy, vy = spec['grid']
        bearing = PLANES[key][1]
        cardinal = {0.: (0., 1.), 90.: (1., 0.), 180.: (0., -1.), 270.: (-1., 0.)}
        radian = math.radians(bearing)
        east, north = cardinal.get(bearing, (math.sin(radian), math.cos(radian)))
        cols = cols - spec['width']//2
        rows = rows - spec['height']//2
        metres = -reference.GetLinearUnits() * (
            east * (ux * cols + vx * rows) + north * (uy * cols + vy * rows)
        )
    alpha=np.full(metres.shape,255.);mask=np.full(metres.shape,255,np.uint8)
    values=metres/FACTORS[spec['unit']]
    if name=='invalid.tif':values[:]=NODATA
    if name=='coverage-cross-block.tif':
        values[20:35,20:35]=0.;values[127,128]=NODATA;values[511,511]=NODATA
        values[508,512]=np.inf;values[512,509]=np.nan;alpha[509,510]=0.;alpha[511,513]=np.nan
        alpha[500,500]=128.;mask[513,512]=0
    return values,alpha,mask

def parameters(name):
    source, _ = CASES[name]
    return {'band': 2, 'elevation_unit': source_spec(source)['unit']}


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

def angular_distance(actual, expected):
    return abs((actual - expected + 180.) % 360. - 180.)


def reference(name):
    """Independent Horn derivatives projected directly onto east and north."""
    import numpy as np
    source, _ = CASES[name]
    spec = source_spec(source)
    elevations, alpha, mask = source_arrays(source)
    valid = (elevations != NODATA) & np.isfinite(elevations) & np.isfinite(alpha) & (alpha > 0) & (mask > 0)
    samples = np.where(valid, elevations * FACTORS[spec['unit']], np.nan).astype(np.float32)
    a, b, c, d, e, f, g, h, i = (
        samples[dy:samples.shape[0]-2+dy, dx:samples.shape[1]-2+dx]
        for dy in range(3) for dx in range(3)
    )
    dc = ((c + f + f + i) - (a + d + d + g)).astype(np.float64)
    dr = ((g + h + h + i) - (a + b + b + c)).astype(np.float64)
    _, ux, vx, _, uy, vy = spec['grid']
    dc /= ux*ux + uy*uy
    dr /= vx*vx + vy*vy
    east, north = -(dc*ux + dr*vx), -(dc*uy + dr*vy)
    complete = np.logical_and.reduce([np.isfinite(value) for value in (a,b,c,d,e,f,g,h,i)])
    supported = complete & ((east != 0.) | (north != 0.))
    answer = np.full(samples.shape, np.nan)
    inner = answer[1:-1, 1:-1]
    inner[supported] = np.degrees(np.arctan2(east[supported], north[supported])) % 360.
    inner[inner == 360.] = 0.
    key = source.removesuffix('.tif')
    if key in PLANES:
        if not supported.all() or not np.all(angular_distance(inner, PLANES[key][1]) <= ANGLE_LIMIT_DEGREES):
            raise FixtureError('independent Horn reference disagrees with analytic slope direction')
    answer.flags.writeable = False
    return answer


def check_angles(name, actual, expected):
    import numpy as np
    valid = np.isfinite(expected)
    if (not np.array_equal(np.isfinite(actual), valid) or not np.isnan(actual[~valid]).all()
        or np.any((actual[valid] < 0.) | (actual[valid] >= 360.))
        or np.any(angular_distance(actual[valid], expected[valid]) > ANGLE_LIMIT_DEGREES)):
        raise FixtureError('complete projected aspect angles or NaN coverage differ')
    if name == 'north-n.cog.tif' and not np.all(actual[valid] == 0.):
        raise FixtureError('valid north-up north aspect must be exactly zero')
    if (name == 'wrap-west.cog.tif' and not np.all(actual[valid] > 359.)
        or name == 'wrap-east.cog.tif' and not np.all(actual[valid] < 1.)):
        raise FixtureError('aspect crossed the wrong side of projected north')


def artifact_expectation(name):
    source=CASES[name][0];s=source_spec(source);g=s['grid'];corners=[world(g,p)for p in((0,0),(s['width'],0),(0,s['height']),(s['width'],s['height']))]
    return {'width':s['width'],'height':s['height'],'band_count':1,'source_crs':'EPSG:'+str(s['epsg']),'extent_srid':s['epsg'],'transform':list(g),'band_nodata':None,
        'extent':[min(p[0]for p in corners),min(p[1]for p in corners),max(p[0]for p in corners),max(p[1]for p in corners)]}


def cache_path(scope):return Path(tempfile.gettempdir())/('addp-aspect-orthogonal-oracle-'+hashlib.sha256(encoded(scope)).hexdigest())

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
        path=root/'outputs'/name;fact=digest(path);expected=expected_array(control,name);spec=artifact_expectation(name);ds=gdal.Open(str(path));band=ds.GetRasterBand(1);actual=band.ReadAsArray();crs=osr.SpatialReference();crs.ImportFromEPSG(spec['extent_srid'])
        if (ds.GetDriver().ShortName!='GTiff'or ds.RasterCount!=1 or(ds.RasterXSize,ds.RasterYSize)!=(spec['width'],spec['height'])or list(ds.GetGeoTransform())!=spec['transform']or not ds.GetSpatialRef().IsSame(crs)
            or band.DataType!=gdal.GDT_Float64 or ds.GetFileList()!=[str(path)] or ds.GetMetadata('IMAGE_STRUCTURE').get('LAYOUT')!='COG' or band.GetNoDataValue()is None or not math.isnan(band.GetNoDataValue())or band.GetUnitType()!=CASES[name][1]or band.GetScale()is not None or band.GetOffset()is not None or band.GetColorTable()is not None
            or not np.array_equal(band.GetMaskBand().ReadAsArray()!=0,np.isfinite(expected))or validate(ds,full_check=True)[1]):raise FixtureError('complete Horn COG pixels/mask/affine/CRS/unit/layout differs')
        check_angles(name, actual, expected)
        previous=actual;h,w=expected.shape;dimensions=[]
        while max(h,w)>128:w,h=max(1,w//2),max(1,h//2);dimensions.append((w,h))
        if band.GetOverviewCount()!=len(dimensions):raise FixtureError('overview count differs')
        for i,(w,h)in enumerate(dimensions):
            level=band.GetOverview(i);ys=np.floor(.5+np.arange(h)*previous.shape[0]/h).astype(int);xs=np.floor(.5+np.arange(w)*previous.shape[1]/w).astype(int);wanted=previous[np.ix_(ys,xs)]
            if(level.XSize,level.YSize)!=(w,h)or not np.array_equal(level.ReadAsArray(),wanted,equal_nan=True)or not np.array_equal(level.GetMaskBand().ReadAsArray()!=0,np.isfinite(wanted)):raise FixtureError('complete nearest overview differs')
            previous=wanted
        result['targets'][name]={**fact,'cog_valid':True,'has_overviews':bool(dimensions),'overview_count':len(dimensions),'valid_pixels':int(np.isfinite(expected).sum()),'zero_pixels':int((expected==0).sum()),'unit':CASES[name][1],'full_pixel_and_mask_verification':True,'independent_horn_verified':True,'nearest_overviews_verified':True};band=ds=None
    result['oracle_cache']={'scope_sha256':hashlib.sha256(encoded(control['scope'])).hexdigest(),'read_only_expected':True,'expected_arrays_loaded':len(names),'full_target_checks':len(names)}
    return result


def archive_files(root,artifacts,verified):
    directory=artifacts/'aspect-orthogonal-cogs';directory.mkdir(exist_ok=True);manifest={}
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
    if action=='prepare-oracle':
        import base64, io
        import numpy as np
        prepared = prepare_oracle(root, control)
        stream = io.BytesIO()
        np.savez_compressed(stream, **{name: expected_array(prepared, name) for name in TARGETS})
        return {**prepared, 'frozen_bytes': base64.b64encode(stream.getvalue()).decode('ascii')}
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
        import base64
        import io
        import zipfile
        control = runtime_worker(action, root, [json.dumps(scope)])
        frozen = base64.b64decode(control.pop('frozen_bytes'), validate=True)
        with zipfile.ZipFile(io.BytesIO(frozen)) as arrays:
            if sorted(arrays.namelist()) != sorted(name + '.npy' for name in TARGETS):
                raise FixtureError('frozen expectation archive inventory differs')
            for name, fact in control['arrays'].items():
                original = arrays.read(name + '.npy')
                if fact != {'size_bytes': len(original), 'sha256': hashlib.sha256(original).hexdigest()}:
                    raise FixtureError('frozen expectation archive member differs')
        artifact = Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR']) / (PREFIX + '-expected.npz')
        with artifact.open('xb') as stream:
            stream.write(frozen)
        manifest = artifact.with_name(PREFIX + '-expected-manifest.json')
        with manifest.open('xb') as stream:
            stream.write(encoded({
                'scope': scope, 'scope_sha256': hashlib.sha256(encoded(scope)).hexdigest(),
                'archive': {'path': artifact.name, **digest(artifact)}, 'arrays': control['arrays'],
            }))
        path.write_bytes(encoded(control));path.chmod(0o600)
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
    except Exception as error:print('Aspect orthogonal fixture failed: '+str(error),file=sys.stderr);raise SystemExit(1)
