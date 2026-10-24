#!/usr/bin/env python3
"""Owned NFS fixture and independent complete-edge GeoPackage oracle for D8 streams."""
from __future__ import annotations
from functools import lru_cache
from collections import Counter, defaultdict
import hashlib
import json
import math
import os
from pathlib import Path
import subprocess
import sys
import tempfile

SUITE='raster-stream-to-vector-d8-persistence'
RUNTIME='addp-raster-stream-to-vector-d8-runtime'
CASES={'metre.gpkg':('directions.tif','streams.tif',2,2,'direct'),
       'foot.gpkg':('foot-directions.tif','foot-streams.tif',2,2,'direct'),
       'survey.gpkg':('survey-directions.tif','survey-streams.tif',2,2,'direct'),
       'threshold.gpkg':('directions.tif',None,2,1,'threshold'),
       'dem.gpkg':('dem.tif',None,2,1,'direction'),
       'filled.gpkg':('bowl.tif',None,2,1,'fill'),
       'empty.gpkg':('directions.tif','empty-streams.tif',2,2,'empty'),
       'invalid.gpkg':('directions.tif','streams.tif',3,3,'invalid')}
TARGETS=tuple(CASES)
SOURCES=('directions.tif','streams.tif','empty-streams.tif','dem.tif','bowl.tif',
         'broken-streams.tif','nonbinary-streams.tif','coverage-streams.tif','shifted-streams.tif',
         'code.tif','code-streams.tif','cycle.tif','foot-directions.tif','foot-streams.tif',
         'survey-directions.tif','survey-streams.tif','isolated-streams.tif','mixed-streams.tif')
TRANSFORM=(500000.,2.,0.,3000000.,0.,-3.)
WIDTH,HEIGHT=519,515
FIRST_ROW=508
NEIGHBOURS=((-1,-1,32),(-1,0,64),(-1,1,128),(0,-1,16),(0,1,1),(1,-1,8),(1,0,4),(1,1,2))
FIELDS=('link_id','from_row','from_col','to_row','to_col','length_m')
SCHEMA=tuple((name,'Real' if name=='length_m' else 'Integer64') for name in FIELDS)
class FixtureError(RuntimeError):
    pass

def command(args, **kwargs):
    try:
        return subprocess.run(args, check=True, capture_output=True, text=True, timeout=120, **kwargs).stdout.strip()
    except subprocess.CalledProcessError as error:
        # Keep the worker's actual verification failure; stdout and stdin may
        # contain fixture state and are deliberately excluded from diagnostics.
        detail = (error.stderr or '').strip()[-4000:] or 'no stderr diagnostic'
        raise FixtureError(f'fixture command failed (exit {error.returncode}): {detail}') from error

if sys.argv[1:2]!=['--worker']:
    sys.path.insert(0,str(Path(__file__).resolve().parents[2]))
    from scripts.utils.online_nfs_fixture import NFSExport, FixtureError, run_runtime_worker

def guard(env=os.environ):
    return NFSExport('stream-to-vector-d8', command).guard(env)

def owned_root(secret):
    return NFSExport('stream-to-vector-d8', command).owned_root(secret, os.environ['ADDP_ONLINE_TEST_RUN_ID'])

def digest(path):
    if path.is_symlink() or not path.is_file() or path.stat().st_size <= 0:
        raise FixtureError('expected a positive-size ordinary owned file')
    return {'sha256': hashlib.sha256(path.read_bytes()).hexdigest(), 'size_bytes': path.stat().st_size}

def epsg(name):
    return 2222 if name.startswith('foot') else 2277 if name.startswith('survey') else 3857


def crs(name='metre.gpkg'):
    from osgeo import osr
    result=osr.SpatialReference();result.ImportFromEPSG(epsg(name));return result


def direction_arrays():
    import numpy as np
    values=np.full((HEIGHT,WIDTH),np.nan);values[FIRST_ROW:,:]=1.;values[FIRST_ROW:,-1]=0
    alpha=np.full(values.shape,255.);mask=np.full(values.shape,255,np.uint8)
    # Every removed centre's western predecessor becomes a legal open zero.
    for row,col in ((510,100),(511,200),(512,300),(513,400),(514,510)):
        values[row,col-1]=0
    values[510,100]=np.nan;values[511,200]=np.inf
    values[512,300]=3;alpha[512,300]=0
    values[513,400]=3;alpha[513,400]=np.nan
    values[514,510]=3;mask[514,510]=0
    # Nested tributaries and confluences cross the 512 seam, with one right outlet.
    for (row,col),code in { (1,1):2,(1,3):8,(2,2):2,(5,1):128,(5,3):32,(4,2):128,
            (2,4):8,(3,3):1,(3,4):1,(3,5):1,(3,6):1,(3,7):1,(3,8):0,(2,6):4}.items():
        values[FIRST_ROW+row,510+col]=code
    # Crossing diagonals have separate cell-centre routes and separate outlets.
    for cell,code in {(508,20):0,(508,21):0,(509,20):128,(509,21):32}.items():values[cell]=code
    alpha[511,515]=128
    # Terminal confluence reserves an ID but has no edge of its own.
    values[508,5]=0;values[509,4]=128
    # Two adjacent confluences leave a one-cell segment with a genuine edge.
    for cell,code in {(509,7):2,(510,7):1,(510,8):1,(510,9):1,(511,8):128,(510,10):0}.items():values[cell]=code
    # Explicit west/north routes complete all eight compass directions.
    for cell,code in {(512,30):16,(512,29):32,(511,28):64,(510,28):128,(509,29):0}.items():values[cell]=code
    # Open each synthetic terminal to invalid coverage, preserving graph admission.
    values[511,10]=np.nan;values[511,9]=0
    values[508,29]=np.nan;values[508,28]=0
    return values,alpha,mask

def effective(values,alpha,mask):
    import numpy as np
    return np.where(np.isfinite(values)&(values!=-9999)&np.isfinite(alpha)&(alpha>0)&(mask!=0),values,np.nan)

def binary_values():
    import numpy as np
    values,alpha,mask=direction_arrays();valid=np.isfinite(effective(values,alpha,mask))
    result=np.where(valid,0.,np.nan)
    for row,col in ((1,1),(1,3),(2,2),(5,1),(5,3),(4,2),(2,4),(3,3),(3,4),(3,5),(3,6),(3,7),(3,8),(2,6)):
        result[FIRST_ROW+row,510+col]=1
    for cell in ((508,20),(508,21),(509,20),(509,21)):result[cell]=1
    # Hidden nonbinary values are excluded by each source's native validity.
    result[511,200]=np.inf;result[512,300]=result[513,400]=result[514,510]=3
    for cell in ((508,4),(508,5),(509,4),(509,7),(510,7),(510,8),(510,9),(511,8),(510,10),
                 (512,30),(512,29),(511,28),(510,28),(509,29)):result[cell]=1
    result[508,100:]=1
    return result,alpha,mask

def dem_values(filled=False):
    import numpy as np
    result=np.full((HEIGHT,WIDTH),np.nan)
    result[FIRST_ROW:,:]=np.arange(WIDTH,0,-1,dtype=float)
    if filled:result[FIRST_ROW+1:-1,1:-1]=-2
    return result

def seed(root):
    import numpy as np
    from osgeo import gdal
    if any((root/name).exists() for name in SOURCES) or list((root/'outputs').iterdir()):
        raise FixtureError('refusing to overwrite existing fixture data')
    for name in SOURCES:
        values,alpha,mask=binary_values() if 'streams' in name else direction_arrays()
        if name in ('dem.tif','bowl.tif'):
            values=dem_values(name=='bowl.tif');alpha[:]=255;mask[:]=255
        elif name=='cycle.tif':values[509,10]=1;values[509,11]=16
        elif name=='empty-streams.tif':values=np.where(np.isfinite(effective(values,alpha,mask)),0.,np.nan)
        elif name=='broken-streams.tif':values[511,514]=0
        elif name=='nonbinary-streams.tif':values[509,100]=2
        elif name=='coverage-streams.tif':values[509,100]=np.nan
        elif name=='isolated-streams.tif':
            values=np.where(np.isfinite(effective(values,alpha,mask)),0.,np.nan);values[508,-1]=1
        elif name=='mixed-streams.tif':values[514,-1]=1
        elif name=='code-streams.tif':
            values[:]=np.nan;values[509,100]=1;alpha[:]=255;mask[:]=255
        if name=='code.tif':
            dataset=gdal.GetDriverByName('GTiff').Create(str(root/name),WIDTH,HEIGHT,2,gdal.GDT_UInt64,['TILED=YES','COMPRESS=DEFLATE'])
            dataset.SetProjection(crs(name).ExportToWkt());dataset.SetGeoTransform(TRANSFORM)
            for index in (1,2):
                data=np.full((HEIGHT,WIDTH),2**53,dtype=np.uint64)
                if index==2:data[509,100]=2**53+1
                band=dataset.GetRasterBand(index);band.WriteArray(data);band.SetNoDataValueAsUInt64(2**53);band=None
            dataset=None;continue
        dataset=gdal.GetDriverByName('GTiff').Create(str(root/name),WIDTH,HEIGHT,4,gdal.GDT_Float64,['TILED=YES','COMPRESS=DEFLATE'])
        dataset.SetProjection(crs(name).ExportToWkt())
        dataset.SetGeoTransform((500001.,2.,0.,3000000.,0.,-3.) if name=='shifted-streams.tif' else TRANSFORM)
        # Band 1 is deliberately unsuitable as direction and differs from the
        # selected river mask. Band 3 is all NoData for the all-invalid chain.
        for index,data in enumerate((np.full(values.shape,.25),np.where(np.isnan(values),-9999,values),np.full(values.shape,-9999.),alpha),1):
            band=dataset.GetRasterBand(index);band.WriteArray(data);band.SetNoDataValue(-9999);band=None
        dataset.GetRasterBand(2).SetUnitType('metre' if name in ('dem.tif','bowl.tif') else 'classification')
        dataset.GetRasterBand(4).SetColorInterpretation(gdal.GCI_AlphaBand)
        with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):dataset.CreateMaskBand(gdal.GMF_PER_DATASET)
        dataset.GetRasterBand(1).GetMaskBand().WriteArray(mask)
        if dataset.GetFileList()!=[str(root/name)]:raise FixtureError('source must be self-contained')
        dataset=None
    return {'sources':{name:digest(root/name) for name in SOURCES}}

@lru_cache(maxsize=8)
def expected_directions(name):
    import numpy as np
    if name not in CASES:raise FixtureError('unknown exact stream-vector target')
    if name=='invalid.gpkg':return np.full((HEIGHT,WIDTH),np.nan)
    if CASES[name][4] not in ('direction','fill'):
        result=effective(*direction_arrays());result.flags.writeable=False;return result
    values=dem_values()
    if name=='filled.gpkg':values[FIRST_ROW+1:-1,1:-1]=1.
    result=np.full(values.shape,np.nan);result[FIRST_ROW:,:]=1.;result[FIRST_ROW:,-1]=0
    # Interior filled flats take the horizontal shortest centre path to the
    # right open edge. Only the perimeter has a strict-downhill slope choice.
    if name=='filled.gpkg':
        import math
        for row,col in np.argwhere(np.isfinite(values)):
            if row not in (FIRST_ROW,HEIGHT-1) and col not in (0,WIDTH-1):continue
            candidates=[]
            for dy,dx,code in NEIGHBOURS:
                y,x=row+dy,col+dx
                if 0<=y<HEIGHT and 0<=x<WIDTH and values[y,x]<values[row,col]:
                    candidates.append((-(values[row,col]-values[y,x])/math.hypot(dx*2,dy*3),y,x,code))
            result[row,col]=min(candidates)[-1] if candidates else 0
    result.flags.writeable=False;return result

def accumulation_oracle(directions):
    # Independent scalar reverse traversal with an explicit call stack and
    # memoized tributaries. No production graph, indegree queue or forward pass.
    import numpy as np
    valid=np.isfinite(directions);selected=valid
    incoming={int(row*WIDTH+col):[] for row,col in np.argwhere(selected)}
    for cell in incoming:
        row,col=divmod(cell,WIDTH);code=directions[row,col]
        if code==0:continue
        step=next(((dy,dx) for dy,dx,value in NEIGHBOURS if value==code),None)
        if step is None:raise FixtureError('oracle invalid direction')
        y,x=row+step[0],col+step[1];target=y*WIDTH+x
        if not (0<=y<HEIGHT and 0<=x<WIDTH and target in incoming):raise FixtureError('oracle stream break')
        incoming[target].append(cell)
    memo={};active=set()
    for origin in incoming:
        stack=[(origin,False)]
        while stack:
            cell,expanded=stack.pop()
            if cell in memo:continue
            if expanded:
                donors=[memo[d] for d in incoming[cell]]
                memo[cell]=sum(d+1 for d in donors)
                active.remove(cell)
            else:
                if cell in active:raise FixtureError('oracle direction cycle')
                active.add(cell);stack.append((cell,True));stack.extend((d,False) for d in incoming[cell] if d not in memo)
    result=np.where(valid,0.,np.nan)
    for cell,value in memo.items():result.flat[cell]=value
    result.flags.writeable=False;return result

@lru_cache(maxsize=8)
def expected_streams(name):
    import numpy as np
    directions=expected_directions(name);stage=CASES[name][4]
    if stage in ('threshold','direction','fill'):
        return np.where(np.isfinite(directions),np.where(accumulation_oracle(directions)>=2,1.,0.),np.nan)
    if stage=='invalid':return np.full(directions.shape,np.nan)
    values=effective(*binary_values())
    return np.where(np.isfinite(directions),0.,np.nan) if stage=='empty' else values

@lru_cache(maxsize=8)
def expected_lines(name):
    """Group each directed edge by walking unique donors upstream, not forward labels."""
    import numpy as np
    directions,streams=expected_directions(name),expected_streams(name)
    river={tuple(map(int,c)) for c in np.argwhere(streams==1)}
    edges,donors={},defaultdict(list)
    for cell in river:
        code=directions[cell]
        if code==0:continue
        step=next(((dy,dx) for dy,dx,value in NEIGHBOURS if value==code),None)
        if step is None:raise FixtureError('oracle invalid direction')
        target=cell[0]+step[0],cell[1]+step[1]
        if target not in river:raise FixtureError('oracle stream break')
        edges[cell]=target;donors[target].append(cell)
    starts=sorted(c for c in river if len(donors[c])!=1)
    ids={cell:i+1 for i,cell in enumerate(starts)}
    if any(c not in edges and not donors[c] for c in river):raise FixtureError('oracle isolated stream cell')
    origins,groups={},defaultdict(dict)
    for cell,target in edges.items():
        cursor=cell;walked=[];seen=set()
        while cursor not in ids and cursor not in origins:
            if cursor in seen:raise FixtureError('oracle stream cycle')
            seen.add(cursor);walked.append(cursor);cursor=donors[cursor][0]
        start=cursor if cursor in ids else origins[cursor]
        for member in walked:origins[member]=start
        groups[start][cell]=target
    metres=1. if epsg(name)==3857 else .3048 if epsg(name)==2222 else 1200/3937
    result=[]
    for start,component in sorted(groups.items()):
        cells=[start]
        while cells[-1] in component:cells.append(component[cells[-1]])
        coordinates=[(TRANSFORM[0]+(col+.5)*TRANSFORM[1],TRANSFORM[3]+(row+.5)*TRANSFORM[5]) for row,col in cells]
        length=math.fsum(math.hypot((b[1]-a[1])*TRANSFORM[1],(b[0]-a[0])*TRANSFORM[5])*metres for a,b in zip(cells,cells[1:]))
        result.append({'fields':(ids[start],*start,*cells[-1],length),'coordinates':coordinates,'cells':cells})
    return result,edges,tuple(ids[c] for c in starts if c not in edges)


def verify(root,names):
    from osgeo import ogr
    if len(set(names))!=len(names) or any(n not in TARGETS for n in names):raise FixtureError('invalid exact vector target inventory')
    if sorted(p.name for p in root.iterdir())!=sorted([*SOURCES,'outputs','.addp-online-owner']):raise FixtureError('unexpected source sidecar or temporary file')
    if sorted(p.name for p in (root/'outputs').iterdir())!=sorted(names):raise FixtureError('unexpected output, sidecar or temporary file')
    result={'sources':{name:digest(root/name) for name in SOURCES},'targets':{}}
    for name in names:
        path=root/'outputs'/name;fact=digest(path);data=ogr.Open(str(path))
        if data is None or data.GetDriver().GetName()!='GPKG' or data.GetLayerCount()!=1:raise FixtureError('expected exactly one GeoPackage layer')
        layer=data.GetLayer(0);reference=layer.GetSpatialRef()
        if reference is None or not reference.IsSame(crs(name)):raise FixtureError('source CRS changed')
        definition=layer.GetLayerDefn();schema=[(definition.GetFieldDefn(i).GetName(),definition.GetFieldDefn(i).GetTypeName()) for i in range(definition.GetFieldCount())]
        if schema!=list(SCHEMA):raise FixtureError('GeoPackage field order, names or storage types differ')
        expected,edges,skipped=expected_lines(name)
        if layer.GetFeatureCount()!=len(expected):raise FixtureError('complete feature count differs')
        observed=Counter();vertices=0
        for feature,truth in zip(layer,expected):
            fields=tuple(feature.GetField(k) for k in FIELDS)
            if (any(type(v) is not int for v in fields[:5]) or fields[:5]!=truth['fields'][:5]
                or type(fields[5]) is not float or not math.isfinite(fields[5]) or fields[5]<=0
                or not math.isclose(fields[5],truth['fields'][5],rel_tol=1e-12,abs_tol=1e-9)):
                raise FixtureError('complete ID, row/column or metre length differs')
            shape=feature.GetGeometryRef()
            if (shape is None or ogr.GT_Flatten(shape.GetGeometryType())!=ogr.wkbLineString
                or shape.Is3D() or shape.IsMeasured() or shape.GetPointCount()<2):raise FixtureError('expected a nondegenerate two-dimensional LineString')
            coordinates=[shape.GetPoint_2D(i) for i in range(shape.GetPointCount())]
            if coordinates!=truth['coordinates']:raise FixtureError('complete centre coordinates or downstream order differs')
            if any(a==b for a,b in zip(coordinates,coordinates[1:])):raise FixtureError('zero-length edge')
            observed.update(zip(truth['cells'],truth['cells'][1:]));vertices+=len(coordinates)
        if observed!=Counter(edges.items()):raise FixtureError('every actual stream edge must appear exactly once')
        result['targets'][name]={**fact,'layer_name':layer.GetName(),'schema':[list(x) for x in SCHEMA],
            'crs':'EPSG:'+str(epsg(name)),'feature_count':len(expected),'edge_count':len(edges),'vertex_count':vertices,
            'skipped_terminal_link_ids':list(skipped),'complete_geometry_attributes_verified':True,
            'independent_edge_coverage_verified':True,'downstream_centres_verified':True,'metre_lengths_verified':True,
            'terminal_confluence_verified':True,'link_id_order_verified':True,'empty_schema_crs_verified':True}
        layer=data=None
    return result


def worker(action,root,names):
    if action not in {'seed','verify'}:raise FixtureError('unknown worker action')
    if action=='verify' and list(Path(tempfile.gettempdir()).glob('addp-raster-*')):raise FixtureError('Runtime raster execution workspace has residuals')
    return seed(root) if action=='seed' else {**verify(root,names),'runtime_workspaces_removed':True}


def runtime_worker(action,root,names=()):
    return run_runtime_worker(command,RUNTIME,SUITE,Path(__file__),action,root,names)


def main(action):
    secret=guard()
    if action in {'start','stop'}:return NFSExport('stream-to-vector-d8',command).lifecycle(action,secret,os.environ['ADDP_ONLINE_TEST_RUN_ID'])
    root=owned_root(secret)
    if action=='seed':
        result=runtime_worker('seed',root);(secret/'stream-to-vector-d8-source.json').write_text(json.dumps(result));return result
    if action=='verify':
        result=runtime_worker('verify',root,sys.argv[2:])
        if result['sources']!=json.loads((secret/'stream-to-vector-d8-source.json').read_text())['sources']:raise FixtureError('source file changed')
        return result
    raise FixtureError('unknown fixture action')


if __name__=='__main__':
    try:
        result=worker(sys.argv[2],Path(sys.argv[3]),sys.argv[4:]) if sys.argv[1:2]==['--worker'] else main(sys.argv[1])
        print(json.dumps(result,sort_keys=True))
    except Exception as error:
        print(f'D8 stream vector fixture failed: {type(error).__name__}: {error}',file=sys.stderr);raise SystemExit(1)
