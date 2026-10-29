#!/usr/bin/env python3
"""Owned NFS DEM pairs and independent closed-count/Decimal complete CSV oracle."""
from __future__ import annotations
from collections import Counter
from decimal import Decimal,localcontext,InvalidOperation
from functools import lru_cache
import base64,csv,hashlib,io,json,math,os,re,subprocess,sys,tempfile
from pathlib import Path

SUITE='raster-surface-area-persistence'
RUNTIME='addp-raster-surface-area-runtime'
WIDTH,HEIGHT=519,515
FACTORS={'metre':1.,'foot':.3048,'us_survey_foot':1200/3937}
COLUMNS=('total_quad_count','valid_quad_count','invalid_quad_count','projected_area_m2',
         'surface_area_low_m2','surface_area_high_m2','surface_area_mean_m2','surface_to_projected_ratio')
# (source, elevation unit, stage), explicit band2 except intermediate rasters.
CASES={'flat.csv':('flat.tif','metre','direct'),'plane.csv':('plane.tif','metre','direct'),
       'diagonals.csv':('diagonals.tif','metre','direct'),'mixed.csv':('mixed.tif','metre','direct'),
       'feet.csv':('feet.tif','foot','direct'),'survey-feet.csv':('survey-feet.tif','us_survey_foot','direct'),
       'us-grid.csv':('us-grid.tif','metre','direct'),'filled.csv':('bowl.tif','metre','fill'),
       'reclassified.csv':('mixed.tif','metre','reclassify'),'int64.csv':('int64.tif','metre','direct'),
       'uint64.csv':('uint64.tif','metre','direct'),'single.csv':('single.tif','metre','direct')}
TARGETS=tuple(CASES)
BAD_SOURCES=('rotated.tif','south-up.tif','geographic.tif','no-crs.tif','no-grid.tif','scaled.tif','offset.tif',
             'complex.tif','complex-alpha.tif','unknown-unit.tif','invalid.tif','sparse.tif','width-one.tif','height-one.tif')
SOURCES=tuple(dict.fromkeys([c[0]for c in CASES.values()]+list(BAD_SOURCES)))

class FixtureError(RuntimeError):pass

def command(args,**kwargs):
    try:return subprocess.run(args,check=True,capture_output=True,text=True,timeout=120,**kwargs).stdout.strip()
    except subprocess.CalledProcessError as error:
        raise FixtureError(f'fixture command failed (exit {error.returncode}): {(error.stderr or "no stderr diagnostic")[-4000:]}') from error

if sys.argv[1:2]!=['--worker']:
    sys.path.insert(0,str(Path(__file__).resolve().parents[2]))
    from scripts.utils.online_nfs_fixture import NFSExport,FixtureError,run_runtime_worker

def guard(env=os.environ):return NFSExport('surface-area',command).guard(env)
def owned_root(secret):return NFSExport('surface-area',command).owned_root(secret,os.environ['ADDP_ONLINE_TEST_RUN_ID'])
def digest(path):
    if path.is_symlink() or not path.is_file() or path.stat().st_size<=0:raise FixtureError('expected positive ordinary owned file')
    return {'sha256':hashlib.sha256(path.read_bytes()).hexdigest(),'size_bytes':path.stat().st_size}

@lru_cache(maxsize=64)
def source_spec(name):
    if name not in SOURCES:raise FixtureError('unknown exact surface area source')
    epsg=2222 if name=='survey-feet.tif'else 2277 if name=='us-grid.tif'else 4326 if name=='geographic.tif'else 3857
    horizontal=.3048 if name=='survey-feet.tif'else 1200/3937 if name=='us-grid.tif'else 1.
    unit='foot'if name=='feet.tif'else'us_survey_foot'if name=='survey-feet.tif'else'metre'
    grid=[500000.,2./horizontal,0.,3000000.,0.,-3./horizontal]
    if name=='rotated.tif':grid[2]=.25
    if name=='south-up.tif':grid[5]=3.
    integer=name in ('int64.tif','uint64.tif');nodata=2**64-1 if name=='uint64.tif'else -2**63 if integer else -9999.
    return {'unit':unit,'epsg':epsg,'horizontal':horizontal,'grid':tuple(grid),'integer':integer,'nodata':nodata,
            'width':2 if name=='single.tif'else 1 if name=='width-one.tif'else WIDTH,
            'height':2 if name=='single.tif'else 1 if name=='height-one.tif'else HEIGHT}


def source_arrays(name):
    import numpy as np
    spec=source_spec(name);rows,cols=np.indices((spec['height'],spec['width']))
    values=(rows%7-3)**2-2*(cols%5-2)**2+(rows%3-1)*(cols%2)-4
    if spec['integer']:values=(values.astype(object)+(2**60 if name=='int64.tif'else 2**63)).astype(np.int64 if name=='int64.tif'else np.uint64)
    else:values=values.astype(float)/FACTORS[spec['unit']]
    alpha=np.full(values.shape,255.);mask=np.full(values.shape,255,np.uint8)
    if name=='flat.tif':values[:]=0.
    elif name=='plane.tif':values=2.*cols-3.*rows-100.
    elif name=='single.tif':values=np.array([[0.,2.],[3.,-1.]])
    elif name=='bowl.tif':values[:]=10.;values[1:-1,1:-1]=2.
    elif name=='invalid.tif':values[:]=spec['nodata']
    elif name=='sparse.tif':values[:]=np.nan;values[::2,::2]=0.
    elif name not in ('diagonals.tif','width-one.tif','height-one.tif'):
        values[510,511]=spec['nodata'];values[508,512]=spec['nodata']if spec['integer']else np.inf
        values[512,509]=spec['nodata']if spec['integer']else np.nan
        alpha[509,510]=0.;alpha[511,513]=0. if spec['integer']else np.nan;alpha[500,500]=128.;mask[513,512]=0
        if spec['integer']:values[510,514]=spec['nodata']+1 if name=='int64.tif'else spec['nodata']-1
    return values,alpha,mask


def seed(root):
    import numpy as np
    from osgeo import gdal,osr
    if any((root/n).exists()for n in SOURCES)or list((root/'outputs').iterdir()):raise FixtureError('refusing to overwrite fixture data')
    for name in SOURCES:
        spec=source_spec(name);values,alpha,mask=source_arrays(name);integer=spec['integer'];complex_=name in ('complex.tif','complex-alpha.tif')
        dtype=gdal.GDT_UInt64 if name=='uint64.tif'else gdal.GDT_Int64 if integer else gdal.GDT_CFloat64 if complex_ else gdal.GDT_Float64
        native=np.uint64 if name=='uint64.tif'else np.int64 if integer else np.complex128 if complex_ else np.float64
        ds=gdal.GetDriverByName('GTiff').Create(str(root/name),spec['width'],spec['height'],4,dtype,['TILED=YES','COMPRESS=DEFLATE'])
        crs=osr.SpatialReference();crs.ImportFromEPSG(spec['epsg'])
        if name!='no-crs.tif':ds.SetSpatialRef(crs)
        if name!='no-grid.tif':ds.SetGeoTransform(spec['grid'])
        for i,data in enumerate((np.full(values.shape,1),values,np.full(values.shape,17),alpha),1):
            band=ds.GetRasterBand(i);band.WriteArray(np.asarray(data,dtype=native));band.SetNoDataValue(spec['nodata']);band=None
        ds.GetRasterBand(2).SetUnitType('fathom'if name=='unknown-unit.tif'else spec['unit']);ds.GetRasterBand(4).SetColorInterpretation(gdal.GCI_AlphaBand)
        if name=='scaled.tif':ds.GetRasterBand(2).SetScale(2.)
        if name=='offset.tif':ds.GetRasterBand(2).SetOffset(1.)
        with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):
            if ds.CreateMaskBand(gdal.GMF_PER_DATASET)!=gdal.CE_None:raise FixtureError('cannot create owned source mask')
        ds.GetRasterBand(1).GetMaskBand().WriteArray(mask);ds=None
        ds=gdal.Open(str(root/name));band=ds.GetRasterBand(2)
        if ds.GetFileList()!=[str(root/name)]or not np.array_equal(band.ReadAsArray(),values,equal_nan=True)or not np.array_equal(band.GetMaskBand().ReadAsArray(),mask):raise FixtureError('native source bytes or coverage changed')
        band=ds=None
    return {'sources':{n:digest(root/n)for n in SOURCES}}


def stored_pixel(name,row,col,*,exceptional=True):
    spec=source_spec(name);unit=spec['unit']
    raw=(row%7-3)**2-2*(col%5-2)**2+(row%3-1)*(col%2)-4
    if spec['integer']:raw+=2**60 if name=='int64.tif'else 2**63
    else:raw=float(raw)/FACTORS[unit]
    if name=='flat.tif':raw=0.
    elif name=='plane.tif':raw=2.*col-3.*row-100.
    elif name=='single.tif':raw=((0.,2.),(3.,-1.))[row][col]
    elif name=='bowl.tif':raw=10. if row in (0,HEIGHT-1)or col in (0,WIDTH-1)else 2.
    valid=True
    if exceptional and name not in ('flat.tif','plane.tif','single.tif','bowl.tif','diagonals.tif'):
        if (row,col)in ((510,511),(508,512),(512,509),(509,510),(511,513),(513,512)):valid=False
        if spec['integer']and(row,col)==(510,514):raw=spec['nodata']+1 if name=='int64.tif'else spec['nodata']-1
    return raw,valid


@lru_cache(maxsize=36)
def _expected_row(name,measurement_grid=None,horizontal_metres=None):
    """Independent periodic vertex counts and vector cross products, standard library only."""
    if name not in CASES:raise FixtureError('unknown exact surface area target')
    source,unit,stage=CASES[name];spec=source_spec(source);rows,cols=spec['height']-1,spec['width']-1
    def quad(row,col,exceptional):
        samples=[]
        for dy,dx in ((0,0),(0,1),(1,0),(1,1)):
            value,valid=stored_pixel(source,row+dy,col+dx,exceptional=exceptional)
            if not valid:return None
            samples.append(10. if stage=='fill'else (-2. if value<0 else 3.)if stage=='reclassify'else value)
        return tuple(samples)
    quads=Counter()
    if source in ('flat.tif','plane.tif','single.tif')or stage=='fill':quads[quad(0,0,False)]=rows*cols
    else:
        # The ordinary source has period21 rows/10 columns; every exceptional
        # sample corrects its four affected anchors, including block seams.
        for row in range(min(21,rows)):
            for col in range(min(10,cols)):quads[quad(row,col,False)]+=((rows-1-row)//21+1)*((cols-1-col)//10+1)
        if source!='diagonals.tif':
            points=((510,511),(508,512),(512,509),(509,510),(511,513),(513,512),(510,514))
            anchors={(r-dy,c-dx)for r,c in points for dy,dx in ((0,0),(0,1),(1,0),(1,1))if 0<=r-dy<rows and 0<=c-dx<cols}
            for row,col in anchors:
                quads[quad(row,col,False)]-=1;actual=quad(row,col,True)
                if actual is not None:quads[actual]+=1
    quads=+quads
    with localcontext()as context:
        context.prec=200;D=lambda v:Decimal(v)if isinstance(v,int)else Decimal.from_float(v)
        grid=spec['grid']if measurement_grid is None else measurement_grid;horizontal=spec['horizontal']if horizontal_metres is None else horizontal_metres
        X=D(grid[1])*D(horizontal);Y=-D(grid[5])*D(horizontal);F=D(FACTORS[unit]);low=high=Decimal(0)
        def triangle(a,b,c):
            u=[b[i]-a[i]for i in range(3)];v=[c[i]-a[i]for i in range(3)]
            cross=[u[1]*v[2]-u[2]*v[1],u[2]*v[0]-u[0]*v[2],u[0]*v[1]-u[1]*v[0]]
            return sum(n*n for n in cross).sqrt()/2
        for samples,count in quads.items():
            a,b,c,d=[D(v)*F for v in samples];A,B,C,Dd=(Decimal(0),Y,a),(X,Y,b),(Decimal(0),Decimal(0),c),(X,Decimal(0),d)
            ad=triangle(A,B,Dd)+triangle(A,Dd,C);bc=triangle(A,B,C)+triangle(B,Dd,C)
            low+=min(ad,bc)*count;high+=max(ad,bc)*count
        count=sum(quads.values());total=(spec['width']-1)*(spec['height']-1);projected=count*X*Y;mean=(low+high)/2
        if not count:raise FixtureError('no valid independent reference quadrilateral')
        return dict(zip(COLUMNS,[total,count,total-count,float(projected),float(low),float(high),float(mean),float(mean/projected)]))


def expected_row(name,geometry=None):
    grid=tuple(geometry['measurement_grid'])if geometry is not None else None
    factor=geometry['horizontal_metres']if geometry is not None else None
    return dict(_expected_row(name,grid,factor))


def validate_records(rows,name,*,preview=False,geometry=None):
    if len(rows)!=1 or set(rows[0])!=set(COLUMNS):raise FixtureError('complete single-row CSV schema differs')
    expected=expected_row(name,geometry)
    for i,key in enumerate(COLUMNS):
        value=rows[0][key];target=expected[key]
        if i<3:
            if type(value) is not int or value!=target:raise FixtureError('exact surface area quadrilateral count differs')
        elif isinstance(value,bool) or not isinstance(value,(int,float)) or not math.isfinite(value) or value!=target:
            raise FixtureError('complete surface area fields differ')


def parse_csv(payload,name,geometry=None):
    try:text=payload.decode('utf-8');reader=csv.DictReader(io.StringIO(text,newline=''),strict=True);rows=[]
    except (UnicodeError,csv.Error) as error:raise FixtureError('malformed UTF-8 CSV') from error
    if reader.fieldnames!=list(COLUMNS):raise FixtureError('CSV column order differs')
    for record in reader:
        if set(record)!=set(COLUMNS) or any(v is None or v=='' for v in record.values()):raise FixtureError('malformed complete CSV row')
        row={}
        for i,key in enumerate(COLUMNS):
            token=record[key]
            if i<3:
                if re.fullmatch(r'0|[1-9][0-9]*',token) is None:raise FixtureError('CSV counts must be exact nonnegative integers')
                row[key]=int(token)
            else:
                try:value=Decimal(token)
                except InvalidOperation as error:raise FixtureError('CSV quantity must be finite') from error
                if not value.is_finite():raise FixtureError('CSV quantity must be finite')
                row[key]=float(value)
        rows.append(row)
    validate_records(rows,name,geometry=geometry);return rows


def measurement_geometry(root,name):
    # Read the actual stored source CRS/grid, independently of the product helper.
    # WKT unit serialization can shift the nominal survey-foot factor by one ULP.
    from osgeo import gdal,osr
    before=CASES[name][0];spec=source_spec(before);source=gdal.Open(str(root/before))
    grid=source.GetGeoTransform();crs=osr.SpatialReference();crs.ImportFromWkt(source.GetProjection());horizontal=crs.GetLinearUnits()
    nominal=osr.SpatialReference();nominal.ImportFromEPSG(spec['epsg'])
    if (tuple(grid)!=spec['grid'] or not crs.IsSame(nominal) or not math.isfinite(horizontal) or horizontal<=0
        or not math.isclose(horizontal,spec['horizontal'],rel_tol=1e-15,abs_tol=0)):
        raise FixtureError('source measurement grid or CRS units differ')
    source=None
    return {'measurement_grid':list(grid),'horizontal_metres':horizontal,'source_measurement_geometry_verified':True}


def verify(root,names):
    if (root/'.addp-online-owner').is_symlink() or not (root/'.addp-online-owner').is_file():raise FixtureError('missing owned fixture marker')
    if len(set(names))!=len(names) or any(n not in TARGETS for n in names):raise FixtureError('unknown exact CSV inventory')
    if {p.name for p in root.iterdir()}!={*SOURCES,'outputs','.addp-online-owner'}:raise FixtureError('unexpected source, sidecar or private file')
    if {p.name for p in (root/'outputs').iterdir()}!=set(names):raise FixtureError('unexpected output or sidecar')
    targets={}
    for name in names:
        geometry=measurement_geometry(root,name)
        path=root/'outputs'/name;fact=digest(path);payload=path.read_bytes();rows=parse_csv(payload,name,geometry)
        # Existing ordinary-table preview contract is pandas JSON double_precision=15.
        import pandas as pd
        preview=pd.DataFrame({key:pd.array([rows[0][key]],dtype='int64'if i<3 else'Float64')for i,key in enumerate(COLUMNS)})
        serialized=json.loads(preview.to_json(orient='records',double_precision=15))[0]
        targets[name]={**fact,**geometry,'preview_serialized_row':serialized,'row_count':1,'schema':list(COLUMNS),'complete_table_verified':True,
                       'independent_decimal_oracle_verified':True,'count_and_area_conservation_verified':True,
                       'dual_diagonal_and_centre_domain_verified':True,'csv_base64':base64.b64encode(payload).decode('ascii')}
    return {'sources':{n:digest(root/n) for n in SOURCES},'targets':targets}


def worker(action,root,names):
    if action not in ('seed','verify'):raise FixtureError('unknown worker action')
    if action=='verify' and list(Path(tempfile.gettempdir()).glob('addp-raster-*')):raise FixtureError('Runtime raster execution workspace has residuals')
    return seed(root) if action=='seed' else {**verify(root,names),'runtime_workspaces_removed':True}

def runtime_worker(action,root,names=()):return run_runtime_worker(command,RUNTIME,SUITE,Path(__file__),action,root,names)
def main(action):
    secret=guard()
    if action in ('start','stop'):return NFSExport('surface-area',command).lifecycle(action,secret,os.environ['ADDP_ONLINE_TEST_RUN_ID'])
    root=owned_root(secret)
    if action=='seed':
        result=runtime_worker('seed',root);(secret/'surface-area-source.json').write_text(json.dumps(result));return result
    if action=='verify':
        result=runtime_worker('verify',root,sys.argv[2:])
        if result['sources']!=json.loads((secret/'surface-area-source.json').read_text())['sources']:raise FixtureError('source file changed')
        return result
    raise FixtureError('unknown fixture action')

if __name__=='__main__':
    try:
        result=worker(sys.argv[2],Path(sys.argv[3]),sys.argv[4:]) if sys.argv[1:2]==['--worker'] else main(sys.argv[1]);print(json.dumps(result,sort_keys=True))
    except Exception as error:print(f'Surface area fixture failed: {type(error).__name__}: {error}',file=sys.stderr);raise SystemExit(1)
