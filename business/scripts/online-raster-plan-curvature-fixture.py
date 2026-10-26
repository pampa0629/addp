#!/usr/bin/env python3
"""Owned NFS fixture and independent nine-point least-squares curvature oracle."""
from __future__ import annotations
from functools import lru_cache
import hashlib,json,os,subprocess,sys,tempfile
from pathlib import Path

SUITE='raster-plan-curvature-persistence'
RUNTIME='addp-raster-plan-curvature-runtime'
WIDTH,HEIGHT=519,515
TRANSFORM=(500000.,2.,0.,3000000.,0.,-3.)
RTOL,ATOL=1e-10,1e-11
FACTORS={'metre':1.,'foot':.3048,'us_survey_foot':1200/3937}
SOURCE_UNITS={'dem.tif':(3857,1.,'metre'),'feet.tif':(2277,1200/3937,'foot'),'survey-feet.tif':(2222,.3048,'us_survey_foot')}
CASES={'plan.cog.tif':('dem.tif',2,'metre','direct','plan'),
 'feet.cog.tif':('feet.tif',2,'foot','direct','plan'),
 'survey-feet.cog.tif':('survey-feet.tif',2,'us_survey_foot','direct','plan'),
 'filled.cog.tif':('bowl.tif',2,'metre','fill','plan'),
 'reclassified.cog.tif':('dem.tif',2,'metre','reclassify','plan'),
 'flat.cog.tif':('flat.tif',2,'metre','direct','plan'),
 'stationary.cog.tif':('stationary.tif',2,'metre','direct','plan'),
 'invalid.cog.tif':('invalid.tif',2,'metre','direct','plan'),
 'sparse.cog.tif':('sparse.tif',2,'metre','direct','plan'),
 'small.cog.tif':('small.tif',2,'metre','direct','plan'),
 'mean-default.cog.tif':('dem.tif',2,'metre','direct',None),
 'mean-explicit.cog.tif':('dem.tif',2,'metre','direct','mean'),
 'profile-control.cog.tif':('dem.tif',2,'metre','direct','profile'),
 'tangential-control.cog.tif':('dem.tif',2,'metre','direct','tangential'),
 'height-transformed.cog.tif':('height-transformed.tif',2,'metre','direct','plan')}
TARGETS=tuple(CASES)
SOURCES=tuple(dict.fromkeys([c[0] for c in CASES.values()]+['rotated.tif','south-up.tif','geographic.tif','no-crs.tif','no-grid.tif','scaled.tif','offset.tif','complex.tif']))

class FixtureError(RuntimeError):pass

def command(args,**kwargs):
    try:return subprocess.run(args,check=True,capture_output=True,text=True,timeout=120,**kwargs).stdout.strip()
    except subprocess.CalledProcessError as error:
        detail=(error.stderr or '').strip()[-4000:] or 'no stderr diagnostic'
        raise FixtureError(f'fixture command failed (exit {error.returncode}): {detail}') from error

if sys.argv[1:2]!=['--worker']:
    sys.path.insert(0,str(Path(__file__).resolve().parents[2]))
    from scripts.utils.online_nfs_fixture import NFSExport,FixtureError,run_runtime_worker

def guard(env=os.environ):return NFSExport('plan-curvature',command).guard(env)
def owned_root(secret):return NFSExport('plan-curvature',command).owned_root(secret,os.environ['ADDP_ONLINE_TEST_RUN_ID'])
def digest(path):
    if path.is_symlink() or not path.is_file() or path.stat().st_size<=0:raise FixtureError('expected a positive-size ordinary owned file')
    return {'sha256':hashlib.sha256(path.read_bytes()).hexdigest(),'size_bytes':path.stat().st_size}

def grid_for(source):
    horizontal=SOURCE_UNITS.get(source,(3857,1.,'metre'))[1]
    return (500000.,2./horizontal,0.,3000000.,0.,-3./horizontal)

def source_arrays(name):
    import numpy as np
    h,w=(2,7) if name=='small.tif' else (3,3) if name=='stationary.tif' else (HEIGHT,WIDTH)
    rows,cols=np.indices((h,w));x=(cols-260)*2.;y=(260-rows)*3.
    values=.001*x*x+.002*x*y-.003*y*y+.4*x-.2*y
    if name=='bowl.tif':
        values[:]=10.;values[1:-1,1:-1]=2.
    elif name=='flat.tif':values[:]=42.
    elif name=='stationary.tif':values=np.outer([1.,-2.,1.],[1.,-2.,1.])
    elif name=='invalid.tif':values[:]=-9999.
    elif name=='sparse.tif':values[:]=float('nan');values[::5,::5]=0.
    elif name=='small.tif':values[:]=0.
    else:
        top=rows<170
        values=np.where(top&(cols<140),.125*((cols-60)*2)+.25*((60-rows)*3)+8,values)
        values=np.where(top&(cols>=140)&(cols<280),.01*((cols-200)*2)**2+.02*((60-rows)*3)**2-3,values)
        values=np.where(top&(cols>=280)&(cols<440),-.01*((cols-360)*2)**2-.02*((60-rows)*3)**2+3,values)
        values=np.where((rows>280)&(cols<140),.01*((cols-60)*2)**2-.01*((340-rows)*3)**2,values)
        values=np.where((rows>280)&(cols>=280)&(cols<440),0.,values);values[340,360]=1.
        values[210:213,200:203]=np.outer([1.,-2.,1.],[1.,-2.,1.])
        # A representable small, nonzero slope must stay valid without a threshold.
        values[220:240,80:110]=((cols[220:240,80:110]-95)*2.)*2.**-30
        # Regular representable samples, separate from Native numerical extremes.
        tiny=2.**-20
        values[230:233,130:133]=[[1.,1.,1.],[-tiny,0.,tiny],[1.,1.,1.]]
        xx=(cols[250:270,140:170]-155)*2.;yy=(260-rows[250:270,140:170])*3.
        values[250:270,140:170]=.25*xx*xx+.5*yy*yy+3*xx+4*yy
        xx=(cols[180:200,300:330]-315)*2.;yy=(190-rows[180:200,300:330])*3.
        values[180:200,300:330]=.5*(xx*xx+yy*yy)
        values[510,511]=-9999.;values[508,512]=np.inf;values[512,509]=np.nan
    alpha=np.full((h,w),255.);mask=np.full((h,w),255,np.uint8)
    if name not in ('bowl.tif','flat.tif','stationary.tif','invalid.tif','sparse.tif','small.tif'):
        alpha[509,510]=0;alpha[511,513]=np.nan;alpha[500,500]=128;mask[513,512]=0
    if name=='height-transformed.tif':values=np.where(values==-9999.,-9999.,8*values+64)
    return values,alpha,mask

def seed(root):
    import numpy as np
    from osgeo import gdal,osr
    if any((root/n).exists() for n in SOURCES) or list((root/'outputs').iterdir()):raise FixtureError('refusing to overwrite existing fixture data')
    for name in SOURCES:
        values,alpha,mask=source_arrays(name);h,w=values.shape
        epsg,horizontal,unit=SOURCE_UNITS.get(name,(3857,1.,'metre'))
        crs=osr.SpatialReference();crs.ImportFromEPSG(4326 if name=='geographic.tif' else epsg)
        ds=gdal.GetDriverByName('GTiff').Create(str(root/name),w,h,4,gdal.GDT_CFloat64 if name=='complex.tif' else gdal.GDT_Float64,['TILED=YES','COMPRESS=DEFLATE'])
        if name!='no-crs.tif':ds.SetSpatialRef(crs)
        grid=list(grid_for(name))
        if name=='rotated.tif':grid[2]=.25
        if name=='south-up.tif':grid[5]=3.
        if name!='no-grid.tif':ds.SetGeoTransform(grid)
        raw=np.where(values==-9999.,-9999.,values/FACTORS[unit])
        for index,array in enumerate((np.full(values.shape,.25),raw,np.full(values.shape,-17.),alpha),1):
            band=ds.GetRasterBand(index);band.WriteArray(array);band.SetNoDataValue(-9999.);band=None
        ds.GetRasterBand(2).SetUnitType(unit)
        ds.GetRasterBand(4).SetColorInterpretation(gdal.GCI_AlphaBand)
        if name=='scaled.tif':ds.GetRasterBand(2).SetScale(2.)
        if name=='offset.tif':ds.GetRasterBand(2).SetOffset(1.)
        with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):
            if ds.CreateMaskBand(gdal.GMF_PER_DATASET)!=gdal.CE_None:raise FixtureError('could not create internal source mask')
        ds.GetRasterBand(1).GetMaskBand().WriteArray(mask);ds=None
        ds=gdal.Open(str(root/name))
        if ds.GetFileList()!=[str(root/name)]:raise FixtureError('source must be self-contained')
        if not np.array_equal(ds.GetRasterBand(2).GetMaskBand().ReadAsArray(),mask):raise FixtureError('source mask changed')
        ds=None
    return {'sources':{name:digest(root/name) for name in SOURCES}}

@lru_cache(maxsize=15)
def expected_dem(name):
    import numpy as np
    if name not in CASES:raise FixtureError('unknown exact curvature target')
    source,_,unit,stage,_=CASES[name];values,alpha,mask=source_arrays(source)
    # Reproduce stored-value roundoff, independently of the production reader.
    raw=np.where(values==-9999.,-9999.,values/FACTORS[unit])
    valid=np.isfinite(raw)&(raw!=-9999.)&np.isfinite(alpha)&(alpha>0)&(mask!=0)
    if stage=='fill':result=np.full(values.shape,10.)
    elif stage=='reclassify':result=np.where(valid,np.where(raw<0,0.,1.),np.nan)
    else:result=np.where(valid,raw,np.nan)
    result.flags.writeable=False;return result

@lru_cache(maxsize=15)
def expected_pixels(name):
    import numpy as np
    import math
    values=expected_dem(name);result=np.full(values.shape,np.nan)
    if min(values.shape)<3:return result
    windows=np.lib.stride_tricks.sliding_window_view(values,(3,3));complete=np.isfinite(windows).all(axis=(-2,-1))
    if not complete.any():return result
    x=np.tile(np.arange(-1,2),3);y=np.repeat(np.arange(1,-2,-1),3)
    design=np.column_stack((x*x,x*y,y*y,x,y,np.ones(9)))
    samples=windows[complete].reshape(-1,9);factor=FACTORS[CASES[name][2]]
    relative=(samples-samples[:,4:5])*factor
    a,b,c,p,q,_=np.linalg.lstsq(design,relative.T,rcond=None)[0]
    # Linear columns are exactly orthogonal to all quadratic/constant columns.
    # Sum their projections faithfully: SVD residual noise is amplified by 1/g.
    px,qy=gradient_moments(name);p=px[1:-1,1:-1][complete];q=qy[1:-1,1:-1][complete]
    first=np.empty((len(p),2,2));second=np.empty_like(first)
    first[:,0,0]=1+p*p;first[:,1,1]=1+q*q;first[:,0,1]=first[:,1,0]=p*q
    normal=np.sqrt(1+p*p+q*q)
    second[:,0,0]=-2*a/4./normal;second[:,1,1]=-2*c/9./normal;second[:,0,1]=second[:,1,0]=-b/6./normal
    if CASES[name][4] in (None,'mean'):
        answer=np.trace(np.linalg.solve(first,second),axis1=1,axis2=2)/2.
    else:
        flat_mask,undefined=semantic_masks(name)
        flat=flat_mask[1:-1,1:-1][complete];active=~(flat|undefined[1:-1,1:-1][complete])
        answer=np.full(len(p),np.nan);answer[flat]=0.
        if CASES[name][4]=='plan':
            # Implicit level curve: unit tangent and its horizontal normal acceleration.
            # Independent least-squares Hessian, not production convolution kernels.
            speed=np.hypot(p[active],q[active])
            tangent=np.column_stack((-q[active]/speed,p[active]/speed))
            hessian=np.empty((len(speed),2,2))
            hessian[:,0,0]=2*a[active]/4.;hessian[:,1,1]=2*c[active]/9.
            hessian[:,0,1]=hessian[:,1,0]=b[active]/6.
            answer[active]=-np.einsum('bi,bij,bj->b',tangent,hessian,tangent)/speed
        else:
            tangent=np.column_stack((-q[active],p[active]) if CASES[name][4]=='tangential' else (p[active],q[active]))
            answer[active]=np.einsum('bi,bij,bj->b',tangent,second[active],tangent)/np.einsum('bi,bij,bj->b',tangent,first[active],tangent)
    result[1:-1,1:-1][complete]=answer
    result.flags.writeable=False;return result


@lru_cache(maxsize=15)
def gradient_moments(name):
    import math
    import numpy as np
    values=expected_dem(name);p=np.full(values.shape,np.nan);q=p.copy()
    if min(values.shape)>=3:
        windows=np.lib.stride_tricks.sliding_window_view(values,(3,3));complete=np.isfinite(windows).all(axis=(-2,-1))
        samples=windows[complete].reshape(-1,9);x=np.tile(np.arange(-1,2),3);y=np.repeat(np.arange(1,-2,-1),3)
        relative=(samples-samples[:,4:5])*FACTORS[CASES[name][2]]
        flat=(samples==samples[:,4:5]).all(axis=1);px=np.zeros(len(samples));qy=px.copy()
        px[~flat]=np.array([math.fsum(row*x)/12. for row in relative[~flat]],dtype=float)
        qy[~flat]=np.array([math.fsum(row*y)/18. for row in relative[~flat]],dtype=float)
        p[1:-1,1:-1][complete]=px;q[1:-1,1:-1][complete]=qy
    p.flags.writeable=False;q.flags.writeable=False;return p,q


@lru_cache(maxsize=15)
def semantic_masks(name):
    import numpy as np
    values=expected_dem(name);flat=np.zeros(values.shape,bool)
    if min(values.shape)>=3:
        windows=np.lib.stride_tricks.sliding_window_view(values,(3,3));complete=np.isfinite(windows).all(axis=(-2,-1))
        samples=windows[complete].reshape(-1,9)
        flat[1:-1,1:-1][complete]=(samples==samples[:,4:5]).all(axis=1)
    p,q=gradient_moments(name);undefined=(p==0)&(q==0)&~flat
    flat.flags.writeable=False;undefined.flags.writeable=False
    return flat,undefined


def artifact_expectation(name):
    source=CASES[name][0];h,w=expected_shape(name);grid=grid_for(source);a,b,c,d,e,f=grid
    corners=[(a+b*x+c*y,d+e*x+f*y) for x,y in ((0,0),(w,0),(0,h),(w,h))]
    epsg=SOURCE_UNITS.get(source,(3857,1.,'metre'))[0]
    return {'width':w,'height':h,'band_count':1,'source_crs':'EPSG:'+str(epsg),'extent_srid':epsg,'transform':list(grid),'band_nodata':None,
        'extent':[min(p[0] for p in corners),min(p[1] for p in corners),max(p[0] for p in corners),max(p[1] for p in corners)]}

def expected_shape(name):return (2,7) if name=='small.cog.tif' else (3,3) if name=='stationary.cog.tif' else (HEIGHT,WIDTH)

def nearest_overviews(dataset,expected):
    import numpy as np
    band=dataset.GetRasterBand(1);previous=expected;dimensions=[];h,w=expected.shape
    while max(h,w)>128:
        # COG custom-grid overview dimensions use integer halving, not ceil.
        # GDAL frmts/gtiff/cogdriver.cpp: nTmpXSize /= 2.
        w,h=max(1,w//2),max(1,h//2);dimensions.append((w,h))
    if band.GetOverviewCount()!=len(dimensions):raise FixtureError('unexpected exact overview count')
    for i,shape in enumerate(dimensions):
        level=band.GetOverview(i)
        if (level.XSize,level.YSize)!=shape:raise FixtureError('incorrect overview dimensions')
        ys=np.floor(.5+np.arange(shape[1])*previous.shape[0]/shape[1]).astype(int)
        xs=np.floor(.5+np.arange(shape[0])*previous.shape[1]/shape[0]).astype(int)
        wanted=previous[np.ix_(ys,xs)]
        if not np.allclose(level.ReadAsArray(),wanted,rtol=RTOL,atol=ATOL,equal_nan=True) or not np.array_equal(level.GetMaskBand().ReadAsArray()!=0,np.isfinite(wanted)):
            raise FixtureError('complete nearest overview pixels or mask differ')
        previous=wanted
    level=band=None

def mode_controls(root,names):
    """Independent checks on saved products; never generate the full plan oracle."""
    import math
    import numpy as np
    from osgeo import gdal
    facts={'tiny_gradient_verified':False,'circle_verified':False,'height_invariance_verified':False,
           'plan_distinction_verified':False,'directional_identity_verified':False,'directional_identity_pixels':0,
           'plan_tangential_identity_verified':False,'plan_tangential_identity_pixels':0}
    if 'plan.cog.tif' not in names:return facts
    def read(name):
        ds=gdal.Open(str(root/'outputs'/name));values=ds.GetRasterBand(1).ReadAsArray();ds=None;return values
    actual=read('plan.cog.tif');tiny=2.**-20
    if not math.isclose(actual[231,131],-4./(3*tiny),rel_tol=RTOL,abs_tol=ATOL):
        raise FixtureError('small nonzero gradient contour curvature differs')
    # Circle x^2+y^2=constant: convex elevation gives -1/r, concave +1/r.
    circle=-1/math.hypot(6.,6.)
    if not math.isclose(actual[188,318],circle,rel_tol=RTOL,abs_tol=ATOL):raise FixtureError('circle inverse radius differs')
    if not math.isclose(actual[260,155],-17/25**1.5,rel_tol=RTOL,abs_tol=ATOL):raise FixtureError('plan analytic curvature differs')
    facts.update(tiny_gradient_verified=True,circle_verified=True,plan_distinction_verified=True)
    if 'height-transformed.cog.tif' in names:
        if not np.allclose(actual,read('height-transformed.cog.tif'),rtol=RTOL,atol=ATOL,equal_nan=True):
            raise FixtureError('positive height scale and shift changed plan curvature')
        facts['height_invariance_verified']=True
    flat,undefined=semantic_masks('plan.cog.tif')
    if all(n in names for n in ('mean-explicit.cog.tif','profile-control.cog.tif','tangential-control.cog.tif')):
        mean,profile,tangential=(read(n) for n in ('mean-explicit.cog.tif','profile-control.cog.tif','tangential-control.cog.tif'))
        active=np.isfinite(actual)&np.isfinite(profile)&np.isfinite(mean)&np.isfinite(tangential)&~flat&~undefined
        if not active.any() or not np.allclose((tangential+profile)[active],(2*mean)[active],rtol=RTOL,atol=ATOL):
            raise FixtureError('saved directional curvatures do not sum to twice the mean')
        facts.update(directional_identity_verified=True,directional_identity_pixels=int(active.sum()))
        # Reuse the independently projected linear terms on the saved coverage.
        p,q=gradient_moments('plan.cog.tif');gradient=np.hypot(p[active],q[active])
        if not np.allclose(tangential[active],actual[active]*gradient/np.hypot(1,gradient),rtol=RTOL,atol=ATOL):
            raise FixtureError('saved plan and tangential relation differs')
        facts.update(plan_tangential_identity_verified=True,plan_tangential_identity_pixels=int(active.sum()))
    return facts


def verify(root,names):
    import numpy as np
    from osgeo import gdal,osr
    from osgeo_utils.samples.validate_cloud_optimized_geotiff import validate
    if len(set(names))!=len(names) or any(n not in TARGETS for n in names):raise FixtureError('invalid exact curvature target inventory')
    if sorted(p.name for p in root.iterdir())!=sorted([*SOURCES,'outputs','.addp-online-owner']):raise FixtureError('unexpected source sidecar or temporary file')
    if sorted(p.name for p in (root/'outputs').iterdir())!=sorted(names):raise FixtureError('unexpected output, sidecar or temporary file')
    result={'sources':{n:digest(root/n) for n in SOURCES},'targets':{}}
    for name in names:
        path=root/'outputs'/name;fact=digest(path);ds=gdal.Open(str(path));spec=artifact_expectation(name)
        if ds is None or ds.GetDriver().ShortName!='GTiff' or ds.RasterCount!=1:raise FixtureError('expected one-band GeoTIFF')
        if (ds.RasterXSize,ds.RasterYSize,list(ds.GetGeoTransform()))!=(spec['width'],spec['height'],spec['transform']):raise FixtureError('source grid changed')
        crs=osr.SpatialReference();crs.ImportFromEPSG(spec['extent_srid'])
        if ds.GetSpatialRef() is None or not ds.GetSpatialRef().IsSame(crs):raise FixtureError('source CRS changed')
        band=ds.GetRasterBand(1);actual=band.ReadAsArray();expected=expected_pixels(name)
        if band.DataType!=gdal.GDT_Float64 or band.GetNoDataValue() is None or not np.isnan(band.GetNoDataValue()):raise FixtureError('Float64 or NaN NoData changed')
        if not np.allclose(actual,expected,rtol=RTOL,atol=ATOL,equal_nan=True):
            row,col=np.argwhere(~np.isclose(actual,expected,rtol=RTOL,atol=ATOL,equal_nan=True))[0]
            raise FixtureError(f'complete pixel values differ: {name}, centre=({row},{col}), actual={actual[row,col]}, expected={expected[row,col]}')
        if not np.array_equal(band.GetMaskBand().ReadAsArray()!=0,np.isfinite(expected)):raise FixtureError('complete pixel mask differs')
        flat,undefined=semantic_masks(name)
        if CASES[name][4] in ('profile','tangential','plan'):
            if not np.all(actual[flat]==0.) or not np.isnan(actual[undefined]).all():raise FixtureError('flat zero or nonflat stationary NoData differs')
        if name=='mean-explicit.cog.tif' and 'mean-default.cog.tif' in names:
            other=gdal.Open(str(root/'outputs'/'mean-default.cog.tif'))
            if other.GetRasterBand(1).ReadRaster()!=band.ReadRaster():raise FixtureError('default mean and explicit mean differ')
            other=None
        if band.GetUnitType()!='1/metre':raise FixtureError('curvature must use inverse metres')
        if band.GetScale() is not None or band.GetOffset() is not None or band.GetColorTable() is not None:raise FixtureError('source encoding leaked into result')
        if validate(ds,full_check=True)[1]:raise FixtureError('COG full layout validation failed')
        nearest_overviews(ds,expected)
        result['targets'][name]={**fact,'cog_valid':True,'has_overviews':band.GetOverviewCount()>0,'overview_count':band.GetOverviewCount(),
            'valid_pixels':int(np.isfinite(expected).sum()),'full_pixel_and_mask_verification':True,'independent_quadratic_oracle_verified':True,
            'complete_neighbourhood_verified':True,'inverse_metre_unit_verified':True,'nearest_overviews_verified':True,'nine_equal_weights_verified':True,
            'curvature_method':CASES[name][4] or 'mean','flat_zero_verified':True,'zero_gradient_nodata_verified':True,
            'flat_centres':int(flat.sum()),'undefined_stationary_centres':int(undefined.sum()),
            'mean_control_verified':CASES[name][4] in (None,'mean'),
            'profile_control_verified':CASES[name][4]=='profile','tangential_control_verified':CASES[name][4]=='tangential'}
        band=ds=None
    result['mean_controls_equal']=all(n in names for n in ('mean-default.cog.tif','mean-explicit.cog.tif'))
    result.update(mode_controls(root,names))
    return result

def worker(action,root,names):
    if action not in {'seed','verify'}:raise FixtureError('unknown worker action')
    if action=='verify' and list(Path(tempfile.gettempdir()).glob('addp-raster-*')):raise FixtureError('Runtime raster execution workspace has residuals')
    return seed(root) if action=='seed' else {**verify(root,names),'runtime_workspaces_removed':True}

def runtime_worker(action,root,names=()):return run_runtime_worker(command,RUNTIME,SUITE,Path(__file__),action,root,names)

def main(action):
    secret=guard()
    if action in {'start','stop'}:return NFSExport('plan-curvature',command).lifecycle(action,secret,os.environ['ADDP_ONLINE_TEST_RUN_ID'])
    root=owned_root(secret)
    if action=='seed':
        result=runtime_worker('seed',root);(secret/'plan-curvature-source.json').write_text(json.dumps(result));return result
    if action=='verify':
        result=runtime_worker('verify',root,sys.argv[2:])
        if result['sources']!=json.loads((secret/'plan-curvature-source.json').read_text())['sources']:raise FixtureError('source file changed')
        return result
    raise FixtureError('unknown fixture action')

if __name__=='__main__':
    try:
        result=worker(sys.argv[2],Path(sys.argv[3]),sys.argv[4:]) if sys.argv[1:2]==['--worker'] else main(sys.argv[1]);print(json.dumps(result,sort_keys=True))
    except Exception as error:
        print(f'Raster curvature fixture failed: {type(error).__name__}: {error}',file=sys.stderr);raise SystemExit(1)
