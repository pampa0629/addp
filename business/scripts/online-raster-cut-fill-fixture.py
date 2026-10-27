#!/usr/bin/env python3
"""Owned NFS DEM pairs and independent closed-count/Decimal complete CSV oracle."""
from __future__ import annotations
from collections import Counter
from decimal import Decimal,localcontext,InvalidOperation
from functools import lru_cache
import base64,csv,hashlib,io,json,math,os,re,subprocess,sys,tempfile
from pathlib import Path

SUITE='raster-cut-fill-persistence'
RUNTIME='addp-raster-cut-fill-runtime'
WIDTH,HEIGHT=519,515
FACTORS={'metre':1.,'foot':.3048,'us_survey_foot':1200/3937}
COLUMNS=('total_count','valid_count','invalid_count','cut_count','fill_count','unchanged_count',
         'valid_area_m2','cut_area_m2','fill_area_m2','unchanged_area_m2','cut_volume_m3','fill_volume_m3','net_increase_m3')
# (before,after,before unit,after unit,after stage), all data roles explicitly band2.
CASES={'mixed.csv':('before.tif','after.tif','metre','metre','direct'),
       'reversed.csv':('after.tif','before.tif','metre','metre','direct'),
       'unchanged.csv':('before.tif','unchanged.tif','metre','metre','direct'),
       'fill-only.csv':('before.tif','higher.tif','metre','metre','direct'),
       'cut-only.csv':('before.tif','lower.tif','metre','metre','direct'),
       'foot-metre.csv':('feet-before.tif','after.tif','foot','metre','direct'),
       'metre-survey.csv':('us-grid-before.tif','us-grid-after.tif','metre','us_survey_foot','direct'),
       'survey-foot.csv':('survey-before.tif','feet-after.tif','us_survey_foot','foot','direct'),
       'filled.csv':('zero.tif','bowl.tif','metre','metre','fill'),
       'reclassified.csv':('before.tif','after.tif','metre','metre','reclassify'),
       'int64.csv':('int-before.tif','int-after.tif','metre','metre','direct'),
       'uint64.csv':('uint-before.tif','uint-after.tif','metre','metre','direct')}
TARGETS=tuple(CASES)
BAD_SOURCES=('size.tif','shifted.tif','other-crs.tif','rotated.tif','south-up.tif','geographic.tif','no-crs.tif','no-grid.tif','scaled.tif','offset.tif','complex.tif','empty.tif','left.tif','right.tif')
SOURCES=tuple(dict.fromkeys([n for c in CASES.values() for n in c[:2]]+list(BAD_SOURCES)))
SPECIAL={(510,511),(508,512),(512,509),(509,510),(511,513),(513,512),(500,500),
         (509,511),(507,512),(511,509),(508,510),(510,513),(512,512),(510,514)}

class FixtureError(RuntimeError):pass

def command(args,**kwargs):
    try:return subprocess.run(args,check=True,capture_output=True,text=True,timeout=120,**kwargs).stdout.strip()
    except subprocess.CalledProcessError as error:
        raise FixtureError(f'fixture command failed (exit {error.returncode}): {(error.stderr or "no stderr diagnostic")[-4000:]}') from error

if sys.argv[1:2]!=['--worker']:
    sys.path.insert(0,str(Path(__file__).resolve().parents[2]))
    from scripts.utils.online_nfs_fixture import NFSExport,FixtureError,run_runtime_worker

def guard(env=os.environ):return NFSExport('cut-fill',command).guard(env)
def owned_root(secret):return NFSExport('cut-fill',command).owned_root(secret,os.environ['ADDP_ONLINE_TEST_RUN_ID'])
def digest(path):
    if path.is_symlink() or not path.is_file() or path.stat().st_size<=0:raise FixtureError('expected positive ordinary owned file')
    return {'sha256':hashlib.sha256(path.read_bytes()).hexdigest(),'size_bytes':path.stat().st_size}

@lru_cache(maxsize=64)
def source_spec(name):
    if name not in SOURCES:raise FixtureError('unknown exact cut/fill source')
    unit='foot' if name.startswith('feet-') else 'us_survey_foot' if name in ('survey-before.tif','us-grid-after.tif') else 'metre'
    epsg=2277 if name.startswith('us-grid-') else 32650 if name=='other-crs.tif' else 4326 if name=='geographic.tif' else 3857
    horizontal=1200/3937 if epsg==2277 else 1.
    grid=[500000.,2./horizontal,0.,3000000.,0.,-3./horizontal]
    if name=='shifted.tif':grid[0]+=1.
    if name=='rotated.tif':grid[2]=.25
    if name=='south-up.tif':grid[5]=3.
    integer=name.startswith(('int-','uint-'))
    nodata=2**64-1 if name.startswith('uint-') else -2**63 if integer else -9999.
    after=name in ('after.tif','higher.tif','lower.tif','unchanged.tif','feet-after.tif','us-grid-after.tif','int-after.tif','uint-after.tif')
    return {'unit':unit,'epsg':epsg,'horizontal':horizontal,'grid':tuple(grid),'integer':integer,'nodata':nodata,'after':after,
            'width':WIDTH-1 if name=='size.tif' else WIDTH,'height':HEIGHT}

def ordinary(name,row,col):
    spec=source_spec(name);base=(row+col)%7-3;delta=(2*row+col)%5-1
    if name in ('zero.tif','empty.tif'):value=0 if name=='zero.tif' else spec['nodata']
    elif name=='bowl.tif':value=2.
    else:
        value=base+(2 if name=='higher.tif' else -3 if name=='lower.tif' else delta if name in ('after.tif','feet-after.tif','us-grid-after.tif','int-after.tif','uint-after.tif') else 0)
        if name.startswith('int-'):value+=2**60
        elif name.startswith('uint-'):value+=2**64-100
    return value if spec['integer'] else float(value)/FACTORS[spec['unit']]

def stored_pixel(name,row,col):
    spec=source_spec(name);value=ordinary(name,row,col);alpha=255.;mask=255
    if name in ('zero.tif','bowl.tif','empty.tif','left.tif','right.tif'):
        if name=='bowl.tif' and (row in (0,HEIGHT-1) or col in (0,WIDTH-1)):value=10.
        if name=='left.tif' and col>=WIDTH//2 or name=='right.tif' and col<WIDTH//2:mask=0
        return value,alpha,mask
    shift=-1 if spec['after'] else 0
    if (row,col)==(510+shift,511):value=spec['nodata']
    if (row,col)==(508+shift,512):value=spec['nodata'] if spec['integer'] else math.inf
    if (row,col)==(512+shift,509):value=spec['nodata'] if spec['integer'] else math.nan
    if (row,col)==(509+shift,510):alpha=0.
    if (row,col)==(511+shift,513):alpha=0. if spec['integer'] else math.nan
    if (row,col)==(513+shift,512):mask=0
    if (row,col)==(500,500):alpha=128.
    if (row,col)==(510,514) and spec['integer']:
        value=spec['nodata']+ (2 if spec['after'] else 1) if name.startswith('int-') else spec['nodata']-(2 if spec['after'] else 1)
    return value,alpha,mask


def seed(root):
    import numpy as np
    from osgeo import gdal,osr
    if any((root/n).exists() for n in SOURCES) or list((root/'outputs').iterdir()):raise FixtureError('refusing to overwrite fixture data')
    for name in SOURCES:
        spec=source_spec(name);h,w=spec['height'],spec['width'];rows,cols=np.indices((h,w));base=(rows+cols)%7-3;delta=(2*rows+cols)%5-1
        if name in ('zero.tif','empty.tif','bowl.tif'):values=np.full((h,w),0. if name=='zero.tif' else spec['nodata'] if name=='empty.tif' else 2.)
        else:
            values=base+(2 if name=='higher.tif' else -3 if name=='lower.tif' else delta if name in ('after.tif','feet-after.tif','us-grid-after.tif','int-after.tif','uint-after.tif') else 0)
            if spec['integer']:values=values.astype(object)+(2**64-100 if name.startswith('uint-') else 2**60)
            else:values=values.astype(float)/FACTORS[spec['unit']]
        alpha=np.full((h,w),255.);mask=np.full((h,w),255,dtype=np.uint8)
        if name=='bowl.tif':values[0,:]=values[-1,:]=10.;values[:,0]=values[:,-1]=10.
        if name=='left.tif':mask[:,WIDTH//2:]=0
        if name=='right.tif':mask[:,:WIDTH//2]=0
        for y,x in SPECIAL:
            if y<h and x<w:values[y,x],alpha[y,x],mask[y,x]=stored_pixel(name,y,x)
        dtype=gdal.GDT_UInt64 if name.startswith('uint-') else gdal.GDT_Int64 if spec['integer'] else gdal.GDT_CFloat64 if name=='complex.tif' else gdal.GDT_Float64
        ds=gdal.GetDriverByName('GTiff').Create(str(root/name),w,h,4,dtype,['TILED=YES','COMPRESS=DEFLATE'])
        crs=osr.SpatialReference();crs.ImportFromEPSG(spec['epsg'])
        if name!='no-crs.tif':ds.SetSpatialRef(crs)
        if name!='no-grid.tif':ds.SetGeoTransform(spec['grid'])
        for i,data in enumerate((np.full((h,w),.25 if not spec['integer'] else 1),values,np.full((h,w),-17 if not name.startswith('uint-') else 17),alpha),1):
            band=ds.GetRasterBand(i);band.WriteArray(np.asarray(data,dtype=np.uint64 if name.startswith('uint-') else np.int64 if spec['integer'] else np.complex128 if name=='complex.tif' else np.float64));band.SetNoDataValue(spec['nodata']);band=None
        ds.GetRasterBand(2).SetUnitType(spec['unit']);ds.GetRasterBand(4).SetColorInterpretation(gdal.GCI_AlphaBand)
        if name=='scaled.tif':ds.GetRasterBand(2).SetScale(2.)
        if name=='offset.tif':ds.GetRasterBand(2).SetOffset(1.)
        with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):
            if ds.CreateMaskBand(gdal.GMF_PER_DATASET)!=gdal.CE_None:raise FixtureError('cannot create owned source mask')
        ds.GetRasterBand(1).GetMaskBand().WriteArray(mask);ds=None
        ds=gdal.Open(str(root/name))
        if ds.GetFileList()!=[str(root/name)]:raise FixtureError('source must be self-contained')
        for y,x in SPECIAL:
            if y<h and x<w:
                expected=stored_pixel(name,y,x)[0];actual=ds.GetRasterBand(2).ReadAsArray(x,y,1,1)[0,0]
                if not (actual==expected or not spec['integer'] and math.isnan(expected) and np.isnan(actual)):raise FixtureError('native source value changed')
        ds=None
    return {'sources':{n:digest(root/n) for n in SOURCES}}


@lru_cache(maxsize=36)
def _expected_row(name, measurement_grid=None, horizontal_metres=None):
    """Count one 35x35 joint period, then correct every exceptional physical cell."""
    if name not in CASES:raise FixtureError('unknown exact cut/fill target')
    before,after,unit,later_unit,stage=CASES[name];pairs=Counter()
    def pair(r,c,special=False):
        a,aa,am=stored_pixel(before,r,c) if special else (ordinary(before,r,c),255,255)
        b,ba,bm=stored_pixel(after,r,c) if special else (ordinary(after,r,c),255,255)
        if not(am and bm and math.isfinite(aa) and aa>0 and math.isfinite(ba) and ba>0 and math.isfinite(a) and math.isfinite(b) and a!=source_spec(before)['nodata'] and b!=source_spec(after)['nodata']):return None
        if stage=='fill':b=10.
        elif stage=='reclassify':b=-2. if b<0 else 3.
        return a,b
    for r in range(35):
        for c in range(35):pairs[pair(r,c)]+=((HEIGHT-1-r)//35+1)*((WIDTH-1-c)//35+1)
    for r,c in SPECIAL:
        pairs[pair(r,c)]-=1
        actual=pair(r,c,True)
        if actual is not None:pairs[actual]+=1
    pairs.pop(None,None)
    with localcontext() as context:
        context.prec=200;cut=fill=Decimal(0);cuts=fills=same=0
        u=Decimal.from_float(FACTORS[unit]);v=Decimal.from_float(FACTORS[later_unit])
        for (a,b),count in pairs.items():
            if not count:continue
            delta=Decimal(a)*u-Decimal(b)*v if isinstance(a,int) and isinstance(b,int) else Decimal.from_float(float(a))*u-Decimal.from_float(float(b))*v
            if delta>0:cut+=delta*count;cuts+=count
            elif delta<0:fill-=delta*count;fills+=count
            else:same+=count
        spec=source_spec(before);grid=measurement_grid if measurement_grid is not None else spec['grid'];horizontal=horizontal_metres if horizontal_metres is not None else spec['horizontal'];area=Decimal.from_float(grid[1])*Decimal.from_float(-grid[5])*Decimal.from_float(horizontal)**2
        count=cuts+fills+same;values=[WIDTH*HEIGHT,count,WIDTH*HEIGHT-count,cuts,fills,same]+[float(x*area) for x in (count,cuts,fills,same,cut,fill,fill-cut)]
    return dict(zip(COLUMNS,values))


def expected_row(name,geometry=None):
    grid=tuple(geometry['measurement_grid']) if geometry is not None else None
    factor=geometry['horizontal_metres'] if geometry is not None else None
    return dict(_expected_row(name,grid,factor))


def validate_records(rows,name,*,preview=False,geometry=None):
    if len(rows)!=1 or set(rows[0])!=set(COLUMNS):raise FixtureError('complete single-row CSV schema differs')
    expected=expected_row(name,geometry)
    for i,key in enumerate(COLUMNS):
        value=rows[0][key];target=expected[key]
        if i<6:
            if type(value) is not int or value!=target:raise FixtureError('exact cut/fill count differs')
        elif isinstance(value,bool) or not isinstance(value,(int,float)) or not math.isfinite(value) or (not math.isclose(value,target,rel_tol=1e-14,abs_tol=1e-12) if preview else value!=target):
            raise FixtureError('complete cut/fill area or volume differs')


def parse_csv(payload,name,geometry=None):
    try:text=payload.decode('utf-8');reader=csv.DictReader(io.StringIO(text,newline=''),strict=True);rows=[]
    except (UnicodeError,csv.Error) as error:raise FixtureError('malformed UTF-8 CSV') from error
    if reader.fieldnames!=list(COLUMNS):raise FixtureError('CSV column order differs')
    for record in reader:
        if set(record)!=set(COLUMNS) or any(v is None or v=='' for v in record.values()):raise FixtureError('malformed complete CSV row')
        row={}
        for i,key in enumerate(COLUMNS):
            token=record[key]
            if i<6:
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
        path=root/'outputs'/name;fact=digest(path);payload=path.read_bytes();parse_csv(payload,name,geometry)
        targets[name]={**fact,**geometry,'row_count':1,'schema':list(COLUMNS),'complete_table_verified':True,
                       'independent_decimal_oracle_verified':True,'count_and_area_conservation_verified':True,
                       'nonnegative_gross_and_net_difference_verified':True,'csv_base64':base64.b64encode(payload).decode('ascii')}
    return {'sources':{n:digest(root/n) for n in SOURCES},'targets':targets}


def worker(action,root,names):
    if action not in ('seed','verify'):raise FixtureError('unknown worker action')
    if action=='verify' and list(Path(tempfile.gettempdir()).glob('addp-raster-*')):raise FixtureError('Runtime raster execution workspace has residuals')
    return seed(root) if action=='seed' else {**verify(root,names),'runtime_workspaces_removed':True}

def runtime_worker(action,root,names=()):return run_runtime_worker(command,RUNTIME,SUITE,Path(__file__),action,root,names)
def main(action):
    secret=guard()
    if action in ('start','stop'):return NFSExport('cut-fill',command).lifecycle(action,secret,os.environ['ADDP_ONLINE_TEST_RUN_ID'])
    root=owned_root(secret)
    if action=='seed':
        result=runtime_worker('seed',root);(secret/'cut-fill-source.json').write_text(json.dumps(result));return result
    if action=='verify':
        result=runtime_worker('verify',root,sys.argv[2:])
        if result['sources']!=json.loads((secret/'cut-fill-source.json').read_text())['sources']:raise FixtureError('source file changed')
        return result
    raise FixtureError('unknown fixture action')

if __name__=='__main__':
    try:
        result=worker(sys.argv[2],Path(sys.argv[3]),sys.argv[4:]) if sys.argv[1:2]==['--worker'] else main(sys.argv[1]);print(json.dumps(result,sort_keys=True))
    except Exception as error:print(f'Cut/fill fixture failed: {type(error).__name__}: {error}',file=sys.stderr);raise SystemExit(1)
