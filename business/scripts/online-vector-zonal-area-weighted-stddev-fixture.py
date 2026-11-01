#!/usr/bin/env python3
"""Owned faces/raster sources and independent complete GeoPackage area-weighted oracle."""
from __future__ import annotations
from collections import deque, Counter
from fractions import Fraction
import hashlib
import importlib
import json
import math
import os
from pathlib import Path
import re
import statistics
import subprocess
import sys
import tempfile

SUITE='vector-zonal-area-weighted-stddev-persistence'
PREFIX='vector-zonal-area-weighted-stddev'
RUNTIME='addp-vector-zonal-area-weighted-stddev-runtime'
WIDTH,HEIGHT=519,515
GRID=(100.,2.,0.,200.,0.,-4.)
ROTATED=(100.,2.,1.,200.,1.,-1.5)
REVERSED=(200.,-2.,0.,100.,0.,4.)
ORACLE='.vector-zonal-area-weighted-stddev-oracle.json'
BASE_COLUMNS=('total_count','valid_count','invalid_count','sum','min','max','mean','stddev')
AREA_COLUMNS=('covered_area_m2','valid_area_m2','area_weighted_mean')
CASES={
    'area-center.gpkg':('values.tif','zones.geojson',False,'zonal_'),
    'area-touched.gpkg':('values.tif','zones.geojson',True,'zonal_'),
    'control-default.gpkg':('values.tif','zones.geojson',False,'zonal_'),
    'control-false.gpkg':('values.tif','zones.geojson',False,'sd_'),
    'foot.gpkg':('foot.tif','foot.gpkg',False,'zonal_'),
    'survey.gpkg':('survey.tif','survey.gpkg',False,'zonal_'),
    'rotated.gpkg':('rotated.tif','rotated.geojson',False,'zonal_'),
    'reversed.gpkg':('reversed.tif','reversed.geojson',False,'zonal_'),
    'values-reclassified.gpkg':('values.tif','zones.geojson',False,'custom_'),
    'classified.gpkg':('values.tif',None,False,'zonal_'),
    'empty-prefix.gpkg':('values.tif','zones.geojson',False,''),
    'zero-negative.gpkg':('zero-negative.tif','zones.geojson',False,'zonal_'),
    'invalid-values.gpkg':('invalid.tif','small.geojson',False,'zonal_'),
    'empty-zones.gpkg':('small.tif','empty.gpkg',False,'zonal_'),
    'constant.gpkg':('constant.tif','small.geojson',False,'zonal_'),
    'sliver.gpkg':('small.tif','sliver.geojson',False,'zonal_'),
}
FAILURES=('flag_null','flag_zero','flag_one','flag_fraction','flag_string_true','flag_string_false','flag_list','flag_object',
    'percentile_null','percentile_value','raster_null','raster_reference','band_null','band_value','weight_pair','stddev_enabled',
    'values_band_zero','values_band_high','values_band_boolean','values_band_string','values_alpha','values_complex',
    'different_crs','geographic','compound','missing_crs','missing_grid','singular_grid',
    'collision_covered','collision_valid','collision_area_mean','collision_stats','invalid_face','nonpolygon',
    'touched_numeric','touched_null','prefix_null','prefix_boolean',
    'sd_flag_null','sd_flag_zero','sd_flag_one','sd_flag_fraction','sd_flag_string_true','sd_flag_string_false','sd_flag_list','sd_flag_object',
    'sd_area_missing','sd_area_false','sd_collision_default','sd_collision_empty')
RASTERS=('values.tif','foot.tif','survey.tif','rotated.tif','reversed.tif','zero-negative.tif','invalid.tif','small.tif','constant.tif','complex.tif','geographic.tif','compound.tif','missing_crs.tif','missing_grid.tif','singular_grid.tif')
VECTORS=('zones.geojson','foot.gpkg','survey.gpkg','rotated.geojson','reversed.geojson','small.geojson','empty.gpkg','different-crs.geojson','geographic.geojson','compound.gpkg','invalid.geojson','line.geojson','sliver.geojson','sd-collision.geojson','sd-empty-collision.geojson')
SOURCES=RASTERS+VECTORS

def enabled(name):return name!='control-default.gpkg'
def stddev_enabled(name):return not name.startswith('control-')
def stat_columns(name):return (*BASE_COLUMNS,*(AREA_COLUMNS if enabled(name)else()),*(['area_weighted_stddev']if stddev_enabled(name)else()))
def raster_crs(name):
    if name=='missing_crs.tif':return None
    if name=='foot.tif':return 'EPSG:2222'
    if name=='survey.tif':return 'EPSG:2277'
    return 'EPSG:4326'if name=='geographic.tif'else'EPSG:7415'if name=='compound.tif'else'EPSG:3857'
def case_crs(name):return raster_crs(CASES[name][0])
def vector_crs(name):
    return raster_crs(name.replace('.gpkg','.tif'))if name in('foot.gpkg','survey.gpkg')else 'EPSG:7415'if name=='compound.gpkg'else'EPSG:4326'if name in('different-crs.geojson','geographic.geojson')else'EPSG:3857'

class FixtureError(RuntimeError):pass

def command(args,**kwargs):return subprocess.run(args,check=True,capture_output=True,text=True,timeout=120,**kwargs).stdout.strip()
if sys.argv[1:2]!=['--worker']:
    sys.path.insert(0,str(Path(__file__).resolve().parents[2]))
    from scripts.utils.online_nfs_fixture import NFSExport,FixtureError,run_runtime_worker
    archive_files=importlib.import_module('business.scripts.online-raster-zonal-percentile-fixture').archive_files

def guard(env=os.environ):return NFSExport(PREFIX,command).guard(env)
def owned_root(secret):return NFSExport(PREFIX,command).owned_root(secret,os.environ['ADDP_ONLINE_TEST_RUN_ID'])
def encoded(value):return(json.dumps(value,allow_nan=False,sort_keys=True)+'\n').encode()
def digest(path):
    if path.is_symlink()or not path.is_file()or path.stat().st_size<=0:raise FixtureError('expected a positive ordinary file')
    return {'size_bytes':path.stat().st_size,'sha256':hashlib.sha256(path.read_bytes()).hexdigest()}

def main_arrays():
    import numpy as np
    y,x=np.indices((HEIGHT,WIDTH));values=x+2.*y-17
    values[0,1]=-9999;values[1,0]=np.nan;values[508,508]=np.inf;values[510,510]=-np.inf;values[200:207,200:207]=-9999
    alpha=np.full(values.shape,255.);alpha[0,0]=128;alpha[509,509]=0;alpha[511,511]=np.nan;alpha[512,512]=-1;alpha[507,507]=np.inf
    mask=np.full(values.shape,255,dtype=np.uint8);mask[508,510]=0
    return values,alpha,mask

def source_arrays(name):
    import numpy as np
    if name not in RASTERS:raise FixtureError('unknown exact raster source')
    dtype,nodata,grid='Float64',-9999.,ROTATED if name=='rotated.tif'else REVERSED if name=='reversed.tif'else GRID
    if name in('values.tif','foot.tif','survey.tif','rotated.tif','reversed.tif','zero-negative.tif'):
        values,alpha,mask=main_arrays()
        if name=='zero-negative.tif':values=np.where(values<512,0.,-4.);values[200:207,200:207]=nodata
    else:
        values=np.tile(np.array([[-8.,0.,5.,11.,nodata,nodata,nodata,nodata]]),(3,1));values[2]=nodata;values[2,0]=0.
        mask=np.full(values.shape,255,np.uint8);alpha=np.full(values.shape,128.)
        if name=='invalid.tif':values[:]=nodata
        if name=='constant.tif':values[:]=-4.;values[2,:]=nodata;values[2,0]=0.
        if name=='complex.tif':dtype='CFloat64';values=np.full(values.shape,1+2j)
        if name=='missing_grid.tif':grid=None
        if name=='singular_grid.tif':grid=(100.,1.,2.,200.,2.,4.)
    return np.stack([np.zeros(values.shape,dtype=values.dtype),values,alpha.astype(values.dtype)]),mask,dtype,nodata,raster_crs(name),grid

# Geometry construction is separate from the analytic coverage oracle.
def face_specs():
    return [ [('rect',(.125,.125,518.875,514.875),[(200.125,200.125,206.875,206.875)])],
        [('rect',(500.125,499.125,517.875,514.875),[])],
        [('rect',(8.125,6.125,12.875,10.875),[]),('rect',(510.125,6.125,515.875,10.875),[])],
        [('rect',(200.125,200.125,206.875,206.875),[])],
        [('rect',(1000.125,1000.125,1001.875,1001.875),[])],
        [('rect',(30.125,20.125,30.25,24.875),[])],
        [('rect',(17.125,.125,17.875,.875),[])],
        [('rect',(500.125,499.125,517.875,514.875),[])],
        [('rect',(-3.875,-4.875,3.875,4.875),[])],
        [('rect',(1.-2**-40,.125,1.375,.875),[])],
        [('rect',(519.,515.,520.,516.),[])],
        [('rect',(519.,2.125,520.,4.875),[])],
        [('triangle',((.125,.125),(8.75,.125),(.125,8.75)),[])] ]

def properties():
    labels=('whole-with-hole','overlap','multipart','all-invalid','outside','thin','zero','duplicate','partial-outside','tiny','point-touch','edge-touch','triangle')
    return [{'sd_area_weighted_stddev':900+i,'source_row':i+1,'label':n,'occupied_mean':-i,'c_covered_area_m2':100+i,'v_valid_area_m2':200+i,'a_area_weighted_mean':300+i,
             'kept_covered_area_m2':400+i,'kept_valid_area_m2':500+i,'kept_area_weighted_mean':600+i}for i,n in enumerate(labels)]

def world(shape,grid=GRID):
    from shapely.affinity import affine_transform
    a,b,c,d,e,f=grid;return affine_transform(shape,[b,c,e,f,a,d])

def shapes_from_specs(specs):
    from shapely.geometry import Polygon,MultiPolygon,box
    result=[]
    for parts in specs:
        shapes=[]
        for kind,outer,holes in parts:
            ring=box(*outer).exterior.coords if kind=='rect'else outer
            shapes.append(Polygon(ring,[box(*hole).exterior.coords for hole in holes]))
        result.append(shapes[0]if len(shapes)==1 else MultiPolygon(shapes))
    return result

def vector_specs(name):
    if name=='sliver.geojson':return [[('rect',(1.-2**-40,.125,1.375,.875),[])],[('rect',(.125,2.125,.875,2.875),[])],[('rect',(8.,0.,9.,1.),[])],[('rect',(20.,20.,21.,21.),[])]]
    if name in('sd-collision.geojson','sd-empty-collision.geojson'):return vector_specs('small.geojson')
    return [[('rect',(.125,.125,3.875,.875),[])],[('rect',(4.125,1.125,7.875,1.875),[])],[('rect',(.125,2.125,.875,2.875),[])],[('rect',(100.125,100.125,101.875,101.875),[])]]if name=='small.geojson'else face_specs()

def vector_rows(name):
    from shapely.geometry import Polygon,LineString
    if name=='empty.gpkg':return []
    if name in('invalid.geojson','line.geojson'):
        shape=Polygon([(0,0),(1,1),(1,0),(0,1),(0,0)])if name=='invalid.geojson'else LineString([(0,0),(1,1)])
        return[(properties()[0],world(shape))]
    grid=ROTATED if name=='rotated.geojson'else REVERSED if name=='reversed.geojson'else GRID
    props=properties()
    if name in('sd-collision.geojson','sd-empty-collision.geojson'):
        key='zonal_area_weighted_stddev'if name=='sd-collision.geojson'else'area_weighted_stddev'
        props=[{**v,key:700+i}for i,v in enumerate(props)]
    return list(zip(props,[world(shape,grid)for shape in shapes_from_specs(vector_specs(name))]))

def reference(spec):
    from osgeo import osr
    crs=osr.SpatialReference();crs.SetFromUserInput(spec);crs.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER);return crs

def seed(root):
    from osgeo import gdal,ogr
    from shapely.geometry import mapping
    if any((root/n).exists()for n in(*SOURCES,ORACLE))or list((root/'outputs').iterdir()):raise FixtureError('refusing to overwrite source or previous result')
    for name in RASTERS:
        data,mask,dtype,nodata,crs,grid=source_arrays(name);ds=gdal.GetDriverByName('GTiff').Create(str(root/name),data.shape[2],data.shape[1],3,getattr(gdal,'GDT_'+dtype),options=['COMPRESS=DEFLATE','TILED=YES','BLOCKXSIZE=512','BLOCKYSIZE=512'])
        if crs:ds.SetProjection(reference(crs).ExportToWkt())
        if grid:ds.SetGeoTransform(grid)
        for index,values in enumerate(data,1):
            band=ds.GetRasterBand(index);band.WriteArray(values)
            if dtype=='Int64':band.SetNoDataValueAsInt64(nodata)
            elif dtype=='UInt64':band.SetNoDataValueAsUInt64(nodata)
            else:band.SetNoDataValue(nodata)
            band.SetScale(17.);band.SetOffset(500.)
        ds.GetRasterBand(3).SetColorInterpretation(gdal.GCI_AlphaBand)
        with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):ds.CreateMaskBand(gdal.GMF_PER_DATASET)
        ds.GetRasterBand(1).GetMaskBand().WriteArray(mask);band=ds=None
    attrs=properties()[0]
    for name in VECTORS:
        if name.endswith('.gpkg'):
            ds=ogr.GetDriverByName('GPKG').CreateDataSource(str(root/name));layer=ds.CreateLayer('zones',reference(vector_crs(name)),ogr.wkbPolygon if name=='empty.gpkg'else ogr.wkbUnknown)
            for key in attrs:layer.CreateField(ogr.FieldDefn(key,ogr.OFTString if key=='label'else ogr.OFTInteger))
            for props,shape in vector_rows(name):
                feature=ogr.Feature(layer.GetLayerDefn());feature.SetGeometry(ogr.CreateGeometryFromWkb(shape.wkb))
                for key,value in props.items():feature.SetField(key,value)
                if layer.CreateFeature(feature)!=ogr.OGRERR_NONE:raise FixtureError('source vector write failed')
                feature=None
            layer=ds=None
        else:(root/name).write_text(json.dumps({'type':'FeatureCollection','crs':{'type':'name','properties':{'name':vector_crs(name)}},'features':[{'type':'Feature','properties':props,'geometry':mapping(shape)}for props,shape in vector_rows(name)]}))
    return {'sources':{n:digest(root/n)for n in SOURCES}}

def valid_pixels(name):
    import numpy as np
    data,mask,_,nodata,_,_=source_arrays(name)
    return np.isfinite(data[1])&(data[1]!=nodata)&np.isfinite(data[2])&(data[2]>0)&(mask!=0)

def rectangle_area(rect,shape):
    import numpy as np
    # Binary fixture coordinates make these closed-form overlaps exact Float64.
    y,x=np.indices(shape);a,b,c,d=rect
    return np.maximum(0,np.minimum(x+1,c)-np.maximum(x,a))*np.maximum(0,np.minimum(y+1,d)-np.maximum(y,b))

def clipped_triangle(vertices,x,y):
    # Independent rational Sutherland-Hodgman clipping; no GEOS or product call.
    poly=[(Fraction(a),Fraction(b))for a,b in vertices]
    for axis,bound,lower in((0,Fraction(x),True),(0,Fraction(x+1),False),(1,Fraction(y),True),(1,Fraction(y+1),False)):
        out=[]
        for a,b in zip(poly,poly[1:]+poly[:1]):
            inside_a=a[axis]>=bound if lower else a[axis]<=bound;inside_b=b[axis]>=bound if lower else b[axis]<=bound
            if inside_a:out.append(a)
            if inside_a!=inside_b:
                f=(bound-a[axis])/(b[axis]-a[axis]);out.append((a[0]+f*(b[0]-a[0]),a[1]+f*(b[1]-a[1])))
        poly=out
        if not poly:return Fraction(0)
    return abs(sum((a[0]*b[1]-a[1]*b[0]for a,b in zip(poly,poly[1:]+poly[:1])),Fraction(0)))/2

def analytic_coverage(parts,shape):
    import numpy as np
    areas=np.zeros(shape);centers=np.zeros(shape,bool);y,x=np.indices(shape);cx,cy=x+.5,y+.5
    for kind,outer,holes in parts:
        if kind=='rect':
            piece=rectangle_area(outer,shape);a,b,c,d=outer;inside=(cx>a)&(cx<c)&(cy>b)&(cy<d)
            for hole in holes:
                piece-=rectangle_area(hole,shape);a,b,c,d=hole;inside&=~((cx>=a)&(cx<=c)&(cy>=b)&(cy<=d))
        else:
            piece=np.zeros(shape)
            for yy in range(max(0,math.floor(min(v[1]for v in outer))),min(shape[0],math.ceil(max(v[1]for v in outer)))):
                for xx in range(max(0,math.floor(min(v[0]for v in outer))),min(shape[1],math.ceil(max(v[0]for v in outer)))):piece[yy,xx]=float(clipped_triangle(outer,xx,yy))
            # This fixture's isosceles right triangle avoids all pixel centers on its diagonal.
            inside=(cx>outer[0][0])&(cy>outer[0][1])&(cx+cy<outer[1][0]+outer[1][1])
        areas+=piece;centers|=inside
    return areas,centers

def class_rules():return[{'min':-100,'max':512,'class':-3},{'min':512,'max':1024,'class':0},{'min':1024,'max':2048,'class':7}]

def classified_rows():
    import numpy as np
    import shapely
    from shapely.geometry import box
    values=source_arrays('values.tif')[0][1];valid=valid_pixels('values.tif')
    codes=np.where(values<512,-3,np.where(values<1024,0,7));seen=np.zeros(values.shape,dtype=bool);rows=[]
    for y,x in zip(*np.where(valid)):
        if seen[y,x]:continue
        seen[y,x]=True;pending=deque([(y,x)]);members=[]
        while pending:
            r,c=pending.popleft();members.append((r,c))
            for rr,cc in ((r-1,c),(r+1,c),(r,c-1),(r,c+1)):
                if 0<=rr<HEIGHT and 0<=cc<WIDTH and valid[rr,cc] and not seen[rr,cc] and codes[rr,cc]==codes[y,x]:seen[rr,cc]=True;pending.append((rr,cc))
        covered=np.zeros(values.shape,dtype=bool);rr,cc=zip(*members);covered[rr,cc]=True;strips=[]
        for r in range(HEIGHT):
            edges=np.diff(np.pad(covered[r].astype(int),(1,1)))
            strips.extend(box(int(a),r,int(b),r+1) for a,b in zip(np.flatnonzero(edges==1),np.flatnonzero(edges==-1)))
        rows.append(({'class_value':int(codes[y,x])},world(shapely.union_all(strips)),covered))
    return rows


def schema(name):
    attrs=[('class_value','Integer64')]if CASES[name][1]is None else[(k,'String'if k=='label'else'Integer')for k in properties()[0]]
    return attrs+[(CASES[name][3]+k,'Integer64'if i<3 else'Real')for i,k in enumerate(stat_columns(name))]

def summary(values,valid,areas,covered,scale,*,enabled_area,enabled_stddev):
    from decimal import Decimal,localcontext
    import numpy as np
    required=covered&valid;samples=[float(v)for v in values[required]];total=int(covered.sum());count=len(samples)
    result=dict(zip(BASE_COLUMNS,[total,count,total-count,*([math.fsum(samples),min(samples),max(samples),statistics.fmean(samples),statistics.pstdev(samples)]if samples else[None]*5)]))
    if enabled_area:
        with localcontext()as context:
            context.prec=2000
            # Group equal bounded fixture pairs with exact multiplicity; no sample is dropped.
            all_mass=sum((Decimal.from_float(float(a))*n for a,n in Counter(areas.flat).items()),Decimal(0))
            pairs=Counter((float(v),float(a))for v,a in zip(values[valid&(areas>0)],areas[valid&(areas>0)]))
            mass=sum((Decimal.from_float(a)*n for(v,a),n in pairs.items()),Decimal(0))
            numerator=sum((Decimal.from_float(v)*Decimal.from_float(a)*n for(v,a),n in pairs.items()),Decimal(0))
            unit=Decimal(scale.numerator)/Decimal(scale.denominator)
            result.update(covered_area_m2=float(all_mass*unit),valid_area_m2=float(mass*unit),area_weighted_mean=float(numerator/mass)if mass else None)
            if enabled_stddev:
                mean=numerator/mass if mass else None
                variance=sum((Decimal.from_float(a)*n*(Decimal.from_float(v)-mean)**2 for(v,a),n in pairs.items()),Decimal(0))/mass if mass else None
                result['area_weighted_stddev']=float(variance.sqrt())if variance is not None else None
    return result

def oracle(root):
    import numpy as np
    from osgeo import gdal
    results={};populations={};summaries={}
    for name,(raster,vector,touched,prefix)in CASES.items():
        data=source_arrays(raster)[0];values=data[1];valid=valid_pixels(raster)
        if name=='values-reclassified.gpkg':values=np.where(values<512,-3.,np.where(values<1024,0.,7.))
        key=(raster,vector)
        if key not in populations:
            if vector is None:rows=[(props,shape,covered.astype(float),covered)for props,shape,covered in classified_rows()]
            else:
                specs=[]if vector=='empty.gpkg'else vector_specs(vector)
                rows=[(props,shape,*analytic_coverage(parts,values.shape))for(props,shape),parts in zip(vector_rows(vector),specs)]
            populations[key]=rows
        source=gdal.Open(str(root/raster));actual=source.GetSpatialRef();units=actual.GetLinearUnits();source=None
        grid=source_arrays(raster)[-1];scale=abs(Fraction(grid[1])*Fraction(grid[5])-Fraction(grid[2])*Fraction(grid[4]))*Fraction(units)**2
        sk=(raster,vector,touched,name=='values-reclassified.gpkg',enabled(name),stddev_enabled(name))
        if sk not in summaries:summaries[sk]=[summary(values,valid,areas,areas>0 if touched else centers,scale,enabled_area=enabled(name),enabled_stddev=stddev_enabled(name))for _,_,areas,centers in populations[key]]
        results[name]=[{'properties':dict(props,**{prefix+k:v for k,v in statistics_.items()}),'geometry_wkb':shape.wkb_hex}for(props,shape,_,_),statistics_ in zip(populations[key],summaries[sk])]
    return results

def validate_properties(actual,wanted,name):
    if set(actual)!=set(wanted):raise FixtureError('complete properties/schema differ')
    numeric={CASES[name][3]+k:k for k in stat_columns(name)}
    for key,target in wanted.items():
        value=actual[key];stat=numeric.get(key)
        if target is None:
            if value is not None:raise FixtureError('null was replaced')
        elif stat is None or stat in BASE_COLUMNS[:3]:
            if type(value)!=type(target)or value!=target:raise FixtureError('original property or exact count differs: '+key)
        elif type(value)not in(int,float)or not math.isfinite(value):raise FixtureError('nonnumeric or nonfinite statistic')
        elif stat in(*AREA_COLUMNS,'area_weighted_stddev'):
            if value!=target:raise FixtureError('exact area statistic differs: '+key)
        elif stat in('min','max'):
            if value!=target:raise FixtureError('exact endpoint differs')
        elif not math.isclose(value,target,rel_tol=1e-12,abs_tol=1e-12):raise FixtureError('independent complete statistic differs')

def reference_crs(spec):return reference(spec)

def verify_vector(path,expected,expected_schema,crs_spec,*,name=None,ordered=True):
    from osgeo import ogr,osr
    from shapely import wkb
    ds=ogr.Open(str(path),0)
    if ds is None or ds.GetLayerCount()!=1:raise FixtureError('exactly one vector layer required')
    layer=ds.GetLayer(0);crs=layer.GetSpatialRef();reference=reference_crs(crs_spec)
    if crs is None:raise FixtureError('vector CRS missing')
    crs.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER);reference.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER)
    if not crs.IsSame(reference):raise FixtureError('vector CRS changed')
    definition=layer.GetLayerDefn();actual_schema=[(definition.GetFieldDefn(i).GetName(),definition.GetFieldDefn(i).GetTypeName()) for i in range(definition.GetFieldCount())]
    if actual_schema!=expected_schema:raise FixtureError('field names/order/types changed: '+str(actual_schema))
    remaining=list(expected);count=0;records=[]
    for feature in layer:
        geometry=feature.GetGeometryRef()
        if geometry is None or geometry.GetGeometryType() not in (ogr.wkbPolygon,ogr.wkbMultiPolygon):raise FixtureError('two dimensional polygon required')
        shape=wkb.loads(bytes(geometry.ExportToWkb()))
        if shape.is_empty or not shape.is_valid or shape.has_z:raise FixtureError('invalid output geometry')
        props={key:feature.GetField(key) for key,_ in expected_schema}
        matches=[i for i,row in enumerate(remaining) if shape.equals(wkb.loads(row['geometry_wkb'],hex=True)) and (not ordered or i==0)]
        if not ordered:matches=[i for i in matches if all(props.get(k)==v for k,v in remaining[i]['properties'].items() if k=='class_value')]
        if len(matches)!=1:raise FixtureError('full geometry/order/unique membership differs')
        wanted=remaining.pop(matches[0])['properties']
        if name:validate_properties(props,wanted,name)
        elif props!=wanted:raise FixtureError('source properties changed')
        records.append(props);count+=1
    if remaining:raise FixtureError('missing features')
    fact={'layer_name':layer.GetName(),'feature_count':count,'schema':[list(v) for v in actual_schema],'crs':crs_spec,'complete_geometry_attributes_statistics_verified':True}
    ds=None;return fact,records


def verify_source_vector(path,name):
    from osgeo import ogr
    from shapely import wkb
    ds=ogr.Open(str(path),0)
    if ds is None or ds.GetLayerCount()!=1:raise FixtureError('source vector inventory differs')
    layer=ds.GetLayer(0);crs=layer.GetSpatialRef()
    if crs is None or not crs.IsSame(reference(vector_crs(name))):raise FixtureError('source vector CRS differs')
    wanted_schema=[(k,'String'if k=='label'else'Integer')for k in(vector_rows(name)[0][0]if vector_rows(name)else properties()[0])];definition=layer.GetLayerDefn()
    actual=[(definition.GetFieldDefn(i).GetName(),definition.GetFieldDefn(i).GetTypeName())for i in range(definition.GetFieldCount())]
    if actual!=wanted_schema:raise FixtureError('source vector schema differs')
    expected=vector_rows(name);features=list(layer)
    if len(features)!=len(expected):raise FixtureError('source vector count differs')
    for feature,(props,geometry)in zip(features,expected):
        actual=wkb.loads(bytes(feature.GetGeometryRef().ExportToWkb()))
        if not actual.equals_exact(geometry,0)or{key:feature.GetField(key)for key in props}!=props:raise FixtureError('source vector geometry/properties changed')
    features=layer=ds=None

def verify(root,names,expectations):
    import numpy as np
    from osgeo import gdal
    if root.is_symlink()or(root/'outputs').is_symlink()or not(root/'outputs').is_dir():raise FixtureError('physical directory symlink or invalid output directory')
    if len(set(names))!=len(names)or any(n not in CASES for n in names):raise FixtureError('unexpected target names')
    if set(p.name for p in root.iterdir())!={*SOURCES,'outputs','.addp-online-owner',ORACLE}:raise FixtureError('unexpected source sidecar/private file')
    if set(p.name for p in(root/'outputs').iterdir())!=set(names):raise FixtureError('unexpected output or failed target')
    result={'sources':{},'targets':{},'full_gpkg_checks':len(names),'full_source_pixels_verified':True,'full_source_vectors_verified':True};records={}
    for name in RASTERS:
        data,mask,dtype,nodata,crs,grid=source_arrays(name);ds=gdal.Open(str(root/name))
        if ds is None or ds.GetDriver().ShortName!='GTiff'or ds.RasterCount!=3 or(ds.RasterYSize,ds.RasterXSize)!=mask.shape or ds.GetFileList()!=[str(root/name)]or ds.GetGeoTransform(can_return_null=True)!=grid:raise FixtureError('source grid/type/inventory changed')
        if crs:
            if not ds.GetProjection()or not ds.GetSpatialRef().IsSame(reference(crs)):raise FixtureError('source CRS changed')
        elif ds.GetProjection():raise FixtureError('missing CRS fabricated')
        for index,values in enumerate(data,1):
            band=ds.GetRasterBand(index)
            if band.DataType!=getattr(gdal,'GDT_'+dtype)or band.GetNoDataValue()!=nodata or band.GetScale()!=17. or band.GetOffset()!=500. or band.GetColorInterpretation()!=(gdal.GCI_AlphaBand if index==3 else gdal.GCI_GrayIndex if index==1 else gdal.GCI_Undefined)or not np.array_equal(band.ReadAsArray(),values,equal_nan=True)or not np.array_equal(band.GetMaskBand().ReadAsArray(),mask):raise FixtureError('complete source pixels/mask/NoData/scale changed')
        band=ds=None;result['sources'][name]=digest(root/name)
    for name in VECTORS:verify_source_vector(root/name,name);result['sources'][name]=digest(root/name)
    for name in names:
        path=root/'outputs'/name
        if gdal.IdentifyDriver(str(path)).ShortName!='GPKG':raise FixtureError('GeoPackage driver required')
        fact,records[name]=verify_vector(path,expectations[name],schema(name),case_crs(name),name=name,ordered=name!='classified.gpkg');result['targets'][name]={**digest(path),**fact}
    for left,right in(('area-center.gpkg','control-default.gpkg'),('area-center.gpkg','control-false.gpkg')):
        if left in records and right in records:
            lp,rp=CASES[left][3],CASES[right][3]
            if[{k:v[lp+k]for k in BASE_COLUMNS}for v in records[left]]!=[{k:v[rp+k]for k in BASE_COLUMNS}for v in records[right]]:raise FixtureError('area mode changed original eight fields')
    if 'area-center.gpkg'in records and'control-false.gpkg'in records:
        if [[v['zonal_'+k]for k in(*BASE_COLUMNS,*AREA_COLUMNS)]for v in records['area-center.gpkg']]!=[[v['sd_'+k]for k in(*BASE_COLUMNS,*AREA_COLUMNS)]for v in records['control-false.gpkg']]:raise FixtureError('area standard deviation changed original eleven fields')
    if 'area-center.gpkg'in records and'area-touched.gpkg'in records:
        if [[v['zonal_'+k]for k in(*AREA_COLUMNS,'area_weighted_stddev')]for v in records['area-center.gpkg']]!=[[v['zonal_'+k]for k in(*AREA_COLUMNS,'area_weighted_stddev')]for v in records['area-touched.gpkg']]:raise FixtureError('area statistics changed with all_touched')
    return result

def worker(action, root, names):
    if action == 'seed' and not names: return seed(root)
    if action == 'cleanup-oracle':
        path = root/ORACLE
        if path.is_symlink(): raise FixtureError('oracle symlink')
        path.unlink(missing_ok=True); return {'oracle_cache_removed': not path.exists()}
    if action == 'oracle' and not names:
        expectations = oracle(root); raw = encoded(expectations)
        with (root/ORACLE).open('xb') as stream: stream.write(raw)
        (root/ORACLE).chmod(0o444); anchor = hashlib.sha256(raw).hexdigest()
    elif action == 'verify' and names and re.fullmatch('[0-9a-f]{64}', names[0]):
        anchor, names = names[0], names[1:]; path = root/ORACLE
        if path.is_symlink() or not path.is_file() or hashlib.sha256(path.read_bytes()).hexdigest() != anchor: raise FixtureError('oracle cache hash changed')
        expectations = json.loads(path.read_bytes())
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
    args = sys.argv[2:]
    if action == 'verify': args = [os.environ['ADDP_ONLINE_VECTOR_ZONAL_AREA_WEIGHTED_STDDEV_ORACLE_SHA256'], *args]
    result = runtime_worker(action, root, args)
    if action == 'seed':
        with state.open('x') as stream: stream.write(encoded(result))
    elif action != 'cleanup-oracle':
        if result['sources'] != json.loads(state.read_bytes())['sources']: raise FixtureError('source file changed')
        artifacts = Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR'])
        history = secret/(PREFIX+'-archives.json'); old = json.loads(history.read_bytes()) if history.exists() else {'sources': {}, 'targets': {}}
        if action != 'oracle' and not history.is_file(): raise FixtureError('source archive history missing')
        result['archived_sources'] = archive_files(root, artifacts, SOURCES, old['sources'], 'vector-zonal-area-weighted-stddev-sources')
        result['archived_gpkgs'] = archive_files(root/'outputs', artifacts, sys.argv[2:], old['targets'], 'vector-zonal-area-weighted-stddev-gpkgs')
        history.write_bytes(encoded({'sources': result['archived_sources'], 'targets': result['archived_gpkgs']}))
    return result

if __name__ == '__main__':
    try:
        result = worker(sys.argv[2], Path(sys.argv[3]), sys.argv[4:]) if sys.argv[1:2] == ['--worker'] else main(sys.argv[1])
        print(json.dumps(result, allow_nan=False, sort_keys=True))
    except Exception as error:
        print(f'Zonal area-weighted fixture failed: {type(error).__name__}: {error}', file=sys.stderr); raise SystemExit(1)
