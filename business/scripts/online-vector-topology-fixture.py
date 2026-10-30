#!/usr/bin/env python3
"""Owned Hosted topology, repair, snapping, polygon and endpoint physical oracles."""
import hashlib
import json
import math
import os
from pathlib import Path
import subprocess
import sys

SUITE = 'vector-topology-persistence'
RUNTIME = 'addp-topology-runtime'
TARGETS = ('faces.geojson', 'diagnostics.geojson', 'repaired.geojson', 'snapped.gpkg', 'overlaps.gpkg', 'gaps.gpkg', 'dangling.gpkg', 'line-overlaps.gpkg', 'line-self-overlaps.gpkg', 'line-intersections.gpkg', 'line-self-intersections.gpkg', 'split-lines.gpkg', 'line-components.gpkg', 'line-chains.gpkg', 'line-chain-members.gpkg')
SOURCES = ('source.geojson', 'invalid.geojson', 'snap-input.gpkg', 'snap-reference.gpkg', 'overlap-input.gpkg', 'gap-input.gpkg', 'gap-coverage.gpkg', 'dangling-input.gpkg', 'line-overlap-input.gpkg', 'line-self-overlap-input.gpkg', 'line-intersection-input.gpkg', 'line-self-intersection-input.gpkg', 'split-line-input.gpkg', 'line-component-input.gpkg', 'line-chain-input.gpkg')
PORTS = ('faces', 'diagnostics', 'repaired', 'snapped', 'overlaps', 'gaps', 'dangling', 'line_overlaps', 'line_self_overlaps', 'line_intersections', 'line_self_intersections', 'split_lines', 'line_components', 'line_chains', 'line_chain_members')
BUFFER_CASE = 'multi-distance-buffer'
BUFFER_SOURCES = ('buffer-input.gpkg', 'buffer-conflict.gpkg')
BUFFER_TARGETS = ('buffers.gpkg',)
DIAGNOSTIC_CASE = 'operator-input-diagnostics'
DIAGNOSTIC_SOURCES = ('diagnostic-line.gpkg','diagnostic-reference.gpkg','diagnostic-crs.gpkg','diagnostic-conflict.gpkg')
DIAGNOSTIC_TITLE = '算子参数诊断独立普通User双语失败页'
DIAGNOSTIC_SCHEMA = 'addp.operator-input-diagnostics'
PROXIMITY_CASE = 'line-proximity'
PROXIMITY_SOURCES = ('proximity-source.gpkg','proximity-reference.gpkg')
PROXIMITY_TARGET = 'proximity-points.gpkg'
PROXIMITY_SCHEMA = 'addp.line-proximity'
PROXIMITY_TITLE = '近接检查独立普通User双源Point成果'
EXTENSION_CASE = 'line-extension'
EXTENSION_SOURCES = ('extension-source.gpkg','extension-reference.gpkg','extension-failure-reference.gpkg')
EXTENSION_TARGETS = ('extended-lines.gpkg','extension-details.gpkg')
EXTENSION_PORTS = ('result','details')
EXTENSION_SCHEMA = 'addp.line-extension'
EXTENSION_TITLE = '线端延伸独立普通User双成果与精确首次交点拒绝'
EXTENSION_FAILURE = '线端延伸的首次交点不能精确沿原方向落在参考线上，未生成任何延伸结果。'


def extension_features(kind):
    """Independent handwritten original and extended coordinates; no production calls."""
    keys=[9,7,3,5,1,11,13]
    if kind in ('source','result'):
        source=[[[[0,0],[0,0],[1,0]]],[[[10,0],[11,0]]],[[[20,0],[21,0]]],[[[30,0],[31,0]]],
                [[[40,0],[41,0]]],[[[50,0],[51,0]],[[60,0],[61,0]]],[[[70,0],[71,0]]]]
        result=[[[[-1,0],[0,0],[0,0],[1,0],[3,0]]],[[[10,0],[11,0],[13,0]]],[[[20,0],[21,0],[22,0]]],
                [[[30,0],[31,0]]],[[[40,0],[41,0]]],[[[50,0],[51,0],[52,0]],[[60,0],[61,0],[62,0]]],
                [[[70,0],[71,0],[71+2**-20,0]]]]
        return [{'properties':{'route_key':2**40+key,'grade':None if i%2 else 3+i,'enabled':None if i%3==1 else i%2==0,'note':None if i%2 else 'source-'+str(i)},
                 'geometry':{'type':'MultiLineString','coordinates':points}} for i,(key,points) in enumerate(zip(keys,source if kind=='source' else result))]
    if kind in ('reference','failure_reference'):
        lines=[('start',[[-1,-1],[-1,1]]),('first',[[3,-1],[3,1]]),('duplicate',[[3,1],[3,-1]]),('far',[[4,-1],[4,1]]),
               ('collinear',[[13,0],[15,0]]),('side',[[20.25,.1],[20.75,.1]]),('forward',[[22,-1],[22,1]]),('later',[[23,-1],[23,1]]),
               ('outside',[[33+2**-20,-1],[33+2**-20,1]]),('contact',[[41,-1],[41,1]]),('after-contact',[[43,-1],[43,1]]),
               ('part-one',[[52,-1],[52,1]]),('part-two',[[62,-1],[62,1]]),('micro',[[71+2**-20,-1],[71+2**-20,1]])]
        if kind=='failure_reference':lines=[('nonrepresentable-first',[[1,1],[2,-2]])]
        return [{'properties':{'ref_key':key,'note':'reference-'+str(i)},'geometry':{'type':'LineString','coordinates':points}} for i,(key,points) in enumerate(lines)]
    if kind!='details':raise FixtureError('unknown extension table')
    return [{'properties':{'source_id':2**40+keys[row],'reference_id':ref,'part_no':part,'end':end,
               'original_x':float(x),'original_y':0.,'target_x':float(target),'target_y':0.,'distance':float(distance)},
             'geometry':{'type':'LineString','coordinates':[[x,0],[target,0]]}}
            for row,ref,part,end,x,target,distance in [(0,'start',1,'start',0,-1,1),(0,'first',1,'end',1,3,2),(0,'duplicate',1,'end',1,3,2),
                (1,'collinear',1,'end',11,13,2),(2,'forward',1,'end',21,22,1),(5,'part-one',1,'end',51,52,1),
                (5,'part-two',2,'end',61,62,1),(6,'micro',1,'end',71,71+2**-20,2**-20)]]


def extension_field_types(kind):
    from osgeo import ogr
    if kind in ('source','result'):return {'route_key':(ogr.OFTInteger64,ogr.OFSTNone),'grade':(ogr.OFTInteger64,ogr.OFSTNone),
        'enabled':(ogr.OFTInteger,ogr.OFSTBoolean),'note':(ogr.OFTString,ogr.OFSTNone)}
    if kind in ('reference','failure_reference'):return {'ref_key':(ogr.OFTString,ogr.OFSTNone),'note':(ogr.OFTString,ogr.OFSTNone)}
    return {'source_id':(ogr.OFTInteger64,ogr.OFSTNone),'reference_id':(ogr.OFTString,ogr.OFSTNone),'part_no':(ogr.OFTInteger64,ogr.OFSTNone),
        'end':(ogr.OFTString,ogr.OFSTNone),**{k:(ogr.OFTReal,ogr.OFSTNone) for k in ('original_x','original_y','target_x','target_y','distance')}}


def seed_extension(path,kind):
    from osgeo import ogr,osr
    crs=osr.SpatialReference();crs.ImportFromEPSG(3857)
    ds=ogr.GetDriverByName('GPKG').CreateDataSource(str(path))
    layer=ds.CreateLayer(path.stem,crs,ogr.wkbMultiLineString if kind=='source' else ogr.wkbLineString,
        options=['GEOMETRY_NAME='+('source_shape' if kind=='source' else 'reference_shape')])
    for name,(native,subtype) in extension_field_types(kind).items():
        field=ogr.FieldDefn(name,native);field.SetSubType(subtype)
        if layer.CreateField(field)!=0:raise FixtureError('cannot seed extension field')
    for row in extension_features(kind):
        item=ogr.Feature(layer.GetLayerDefn())
        for key,value in row['properties'].items():
            if value is None:item.SetFieldNull(key)
            else:item.SetField(key,int(value) if type(value) is bool else value)
        item.SetGeometry(ogr.CreateGeometryFromJson(json.dumps(row['geometry'])))
        if layer.CreateFeature(item)!=0:raise FixtureError('cannot seed extension source')
        item=None
    layer=None;ds=None


def validate_extension_features(rows,kind):
    expected=extension_features(kind)
    if rows!=expected or any(type(row['properties'][key]) is not type(ref['properties'][key])
        for row,ref in zip(rows,expected) for key in ref['properties']):
        raise FixtureError('extension exact coordinates attributes IDs types direction or row order differ')


def verify_extension(path,kind):
    from osgeo import ogr,osr
    ds=ogr.Open(str(path),0)
    if ds is None or ds.GetLayerCount()!=1:raise FixtureError('extension requires one physical layer')
    layer=ds.GetLayer(0);crs=osr.SpatialReference();crs.ImportFromEPSG(3857);schema=layer.GetLayerDefn();types=extension_field_types(kind)
    if not layer.GetSpatialRef() or not layer.GetSpatialRef().IsSame(crs):raise FixtureError('extension CRS differs')
    if [schema.GetFieldDefn(i).GetName() for i in range(schema.GetFieldCount())]!=list(types):raise FixtureError('extension fields differ')
    if any((schema.GetFieldDefn(i).GetType(),schema.GetFieldDefn(i).GetSubType())!=pair for i,pair in enumerate(types.values())):
        raise FixtureError('extension physical field types differ')
    if kind in ('source','result'):
        # OGR coerces a corrupt SQLite BOOLEAN value such as 2 to True. Check
        # the original physical values before its logical Boolean decoder.
        import sqlite3
        connection=sqlite3.connect(path.resolve().as_uri()+'?mode=ro',uri=True)
        try:
            table='"'+layer.GetName().replace('"','""')+'"'
            values=connection.execute('SELECT "enabled" FROM '+table).fetchall()
            if any(value is not None and (type(value) is not int or value not in (0,1)) for (value,) in values):
                raise FixtureError('extension non-Boolean physical value')
        finally:connection.close()
    rows=[]
    for i,item in enumerate(layer,1):
        if item.GetFID()!=i:raise FixtureError('extension physical FID order differs')
        props=item.items()
        if 'enabled' in props and props['enabled'] is not None:
            if type(props['enabled']) not in (int,bool) or props['enabled'] not in (0,1):raise FixtureError('extension non-Boolean physical value')
            props['enabled']=bool(props['enabled'])
        rows.append({'properties':props,'geometry':proximity_geometry(item.GetGeometryRef())})
    validate_extension_features(rows,kind)
    if layer.GetGeomType()!=(ogr.wkbMultiLineString if kind in ('source','result') else ogr.wkbLineString):raise FixtureError('extension declared geometry type differs')
    geometry=layer.GetGeometryColumn()
    if not geometry or (kind in ('source','reference','failure_reference') and geometry!=('source_shape' if kind=='source' else 'reference_shape')):raise FixtureError('extension geometry column differs')
    result={**digest(path),'feature_count':len(rows),'layer_name':layer.GetName(),'geometry_column':geometry,'fid_column':layer.GetFIDColumn(),
        'source_crs':'EPSG:3857','features':rows,'field_types':{name:ogr.GetFieldTypeName(native) for name,(native,_) in types.items()}}
    ds=None
    return result


def extension_worker(action,root):
    if action=='seed':
        if any((root/name).exists() for name in EXTENSION_SOURCES) or list((root/'outputs').iterdir()):raise FixtureError('refusing existing extension data')
        for name,kind in zip(EXTENSION_SOURCES,('source','reference','failure_reference')):seed_extension(root/name,kind)
        return {'sources':{name:digest(root/name) for name in EXTENSION_SOURCES}}
    if action!='verify':raise FixtureError('unknown extension action')
    if sorted(p.name for p in root.iterdir())!=sorted(['.addp-online-owner','outputs',*EXTENSION_SOURCES]) or sorted(p.name for p in (root/'outputs').iterdir())!=sorted(EXTENSION_TARGETS):
        raise FixtureError('extension requires exactly three sources and two outputs, no failed publication')
    return {'sources':{name:digest(root/name) for name in EXTENSION_SOURCES},
        'source_tables':{name:verify_extension(root/name,kind) for name,kind in zip(EXTENSION_SOURCES,('source','reference','failure_reference'))},
        'targets':{name:verify_extension(root/'outputs'/name,port) for name,port in zip(EXTENSION_TARGETS,EXTENSION_PORTS)},'exact_inventory_verified':True}


INDEPENDENT_CASES = (EXTENSION_CASE, PROXIMITY_CASE, DIAGNOSTIC_CASE, BUFFER_CASE, 'line-network-normalize', 'line-network-normalize-chains')

NORMALIZE_CASE = 'line-network-normalize'
NORMALIZE_SOURCE = 'normalize-input.gpkg'
NORMALIZE_TARGETS = ('normalized-edges.gpkg', 'normalized-members.gpkg')
NORMALIZE_PORTS = ('result', 'members')

COMPOSITION_CASE = 'line-network-normalize-chains'
COMPOSITION_SOURCE = 'normalize-chains-input.gpkg'
COMPOSITION_PORTS = ('chains','chain_edges','edge_sources')
COMPOSITION_TARGETS = ('chains.gpkg','chain-edges.gpkg','edge-sources.gpkg')


def network_profile(case):
    if case==EXTENSION_CASE:
        return {"ports":EXTENSION_PORTS,"targets":EXTENSION_TARGETS,"schema":EXTENSION_SCHEMA,"title":EXTENSION_TITLE}
    if case==PROXIMITY_CASE:
        return {'ports':('result',),'targets':(PROXIMITY_TARGET,),'schema':PROXIMITY_SCHEMA,'title':PROXIMITY_TITLE}
    if case==DIAGNOSTIC_CASE:
        return {'schema':DIAGNOSTIC_SCHEMA,'title':DIAGNOSTIC_TITLE,'ports':(),'targets':()}
    if case==BUFFER_CASE:
        return {'source':BUFFER_SOURCES[0],'ports':('result',),'targets':BUFFER_TARGETS,
                'schema':'addp.topology-buffer','title':'多距缓冲独立普通User单成果与重名拒绝'}
    if case==NORMALIZE_CASE:
        return {'source':NORMALIZE_SOURCE,'ports':NORMALIZE_PORTS,'targets':NORMALIZE_TARGETS,
                'schema':'addp.topology-normalize','title':'重合规整独立普通User双成果与字段拒绝'}
    if case==COMPOSITION_CASE:
        return {'source':COMPOSITION_SOURCE,'ports':COMPOSITION_PORTS,'targets':COMPOSITION_TARGETS,
                'schema':'addp.topology-normalize-chains','title':'重合规整链路合并独立普通User三成果与字段拒绝'}
    raise FixtureError('unknown network case')


def proximity_features(kind):
    """Handwritten coordinates and expected rows; never calls the production algorithm."""
    if kind=='source':
        return [{'properties':{'route_key':2**40+key,'note':'source-'+str(i)},'geometry':{'type':'LineString','coordinates':points}}
                for i,(key,points) in enumerate([(9,[[0,0],[0,10]]),(7,[[20,0],[20,10]]),
                    (3,[[40,20],[40,30]]),(5,[[60,0],[60,10]]),(1,[[60,10],[60,0]])])]
    if kind=='reference':
        lines=[('z-east','LineString',[[.5,-1],[.5,1]]),('a-west','LineString',[[-.5,-1],[-.5,1]]),
            ('m-parts','MultiLineString',[[[.25,-1],[.25,1]],[[-.25,1],[-.25,-1]],[[.25,1],[.25,-1]]]),
            ('b-east-copy','LineString',[[.5,1],[.5,-1]]),('touch','LineString',[[19,0],[21,0]]),
            ('near-touch','LineString',[[20.5,-1],[20.5,1]]),('boundary','LineString',[[41,19],[41,21]]),
            ('micro','LineString',[[40+2**-20,19],[40+2**-20,21]]),('shared','LineString',[[60.5,-1],[60.5,1]])]
        return [{'properties':{'ref_key':key,'note':'reference-'+str(i)},'geometry':{'type':shape,'coordinates':points}}
                for i,(key,shape,points) in enumerate(lines)]
    if kind!='result':raise FixtureError('unknown proximity table')
    return [{'properties':{'source_id':2**40+key,'reference_id':ref,'distance':float(distance),'nearest_x':float(nx),'nearest_y':float(ny)},
             'geometry':{'type':'Point','coordinates':point}}
            for key,ref,distance,nx,ny,point in [
                (9,'z-east',.5,.5,0,[0,0]),(9,'a-west',.5,-.5,0,[0,0]),(9,'m-parts',.25,-.25,0,[0,0]),
                (9,'b-east-copy',.5,.5,0,[0,0]),(3,'micro',2**-20,40+2**-20,20,[40,20]),
                (5,'shared',.5,60.5,0,[60,0]),(1,'shared',.5,60.5,0,[60,0])]]


def proximity_field_types(kind):
    from osgeo import ogr
    return ({'route_key':ogr.OFTInteger64,'note':ogr.OFTString} if kind=='source' else
            {'ref_key':ogr.OFTString,'note':ogr.OFTString} if kind=='reference' else
            {'source_id':ogr.OFTInteger64,'reference_id':ogr.OFTString,'distance':ogr.OFTReal,'nearest_x':ogr.OFTReal,'nearest_y':ogr.OFTReal})


def seed_proximity(path,kind):
    from osgeo import ogr,osr
    crs=osr.SpatialReference();crs.ImportFromEPSG(3857)
    ds=ogr.GetDriverByName('GPKG').CreateDataSource(str(path))
    layer=ds.CreateLayer(path.stem,crs,ogr.wkbUnknown,options=['GEOMETRY_NAME='+('source_shape' if kind=='source' else 'reference_shape')])
    for name,native in proximity_field_types(kind).items():
        if layer.CreateField(ogr.FieldDefn(name,native))!=0:raise FixtureError('cannot seed proximity field')
    for row in proximity_features(kind):
        item=ogr.Feature(layer.GetLayerDefn())
        for key,value in row['properties'].items():item.SetField(key,value)
        item.SetGeometry(ogr.CreateGeometryFromJson(json.dumps(row['geometry'])))
        if layer.CreateFeature(item)!=0:raise FixtureError('cannot seed proximity source')
        item=None
    layer=None;ds=None


def proximity_geometry(geometry):
    if geometry is None or geometry.GetCoordinateDimension()!=2 or geometry.IsMeasured() or not geometry.IsValid():
        raise FixtureError('proximity geometry must be valid finite XY')
    name=geometry.GetGeometryName()
    if name=='POINT':value={'type':'Point','coordinates':list(geometry.GetPoint_2D())}
    elif name=='LINESTRING':value={'type':'LineString','coordinates':[list(point) for point in geometry.GetPoints()]}
    elif name=='MULTILINESTRING':value={'type':'MultiLineString','coordinates':[
        [list(point) for point in geometry.GetGeometryRef(i).GetPoints()] for i in range(geometry.GetGeometryCount())]}
    else:raise FixtureError('unexpected proximity geometry type')
    return value


def validate_proximity_features(rows,kind='result'):
    expected=proximity_features(kind)
    if rows!=expected or any(type(row['properties'][key]) is not type(ref['properties'][key])
        for row,ref in zip(rows,expected) for key in ref['properties']):
        raise FixtureError('proximity exact coordinates IDs values types or row order differ')


def verify_proximity(path,kind):
    from osgeo import ogr,osr
    ds=ogr.Open(str(path),0)
    if ds is None or ds.GetLayerCount()!=1:raise FixtureError('proximity requires one physical layer')
    layer=ds.GetLayer(0);crs=osr.SpatialReference();crs.ImportFromEPSG(3857);schema=layer.GetLayerDefn()
    if not layer.GetSpatialRef() or not layer.GetSpatialRef().IsSame(crs):raise FixtureError('proximity CRS differs')
    types=proximity_field_types(kind)
    if [schema.GetFieldDefn(i).GetName() for i in range(schema.GetFieldCount())]!=list(types):raise FixtureError('proximity fields differ')
    if any(schema.GetFieldDefn(schema.GetFieldIndex(name)).GetType()!=native or schema.GetFieldDefn(schema.GetFieldIndex(name)).GetSubType()!=ogr.OFSTNone for name,native in types.items()):raise FixtureError('proximity physical field types differ')
    rows=[]
    for i,item in enumerate(layer,1):
        if item.GetFID()!=i:raise FixtureError('proximity physical FID order differs')
        rows.append({'properties':item.items(),'geometry':proximity_geometry(item.GetGeometryRef())})
    validate_proximity_features(rows,kind)
    if kind=='result' and layer.GetGeomType()!=ogr.wkbPoint:raise FixtureError('proximity result must declare Point')
    geometry=layer.GetGeometryColumn()
    if not geometry or (kind!='result' and geometry!=('source_shape' if kind=='source' else 'reference_shape')):raise FixtureError('proximity geometry column differs')
    result={**digest(path),'feature_count':len(rows),'layer_name':layer.GetName(),'geometry_column':geometry,'fid_column':layer.GetFIDColumn(),
            'source_crs':'EPSG:3857','features':rows,'field_types':{name:ogr.GetFieldTypeName(native) for name,native in types.items()}}
    ds=None
    return result


def proximity_worker(action,root):
    if action=='seed':
        if any((root/name).exists() for name in PROXIMITY_SOURCES) or list((root/'outputs').iterdir()):raise FixtureError('refusing existing proximity data')
        for name,kind in zip(PROXIMITY_SOURCES,('source','reference')):seed_proximity(root/name,kind)
        return {'sources':{name:digest(root/name) for name in PROXIMITY_SOURCES}}
    if action!='verify':raise FixtureError('unknown proximity action')
    if sorted(p.name for p in root.iterdir())!=sorted(['.addp-online-owner','outputs',*PROXIMITY_SOURCES]) or [p.name for p in (root/'outputs').iterdir()]!=[PROXIMITY_TARGET]:
        raise FixtureError('proximity requires exactly two sources and one output')
    tables={name:verify_proximity(root/name,kind) for name,kind in zip(PROXIMITY_SOURCES,('source','reference'))}
    return {'sources':{name:digest(root/name) for name in PROXIMITY_SOURCES},'source_tables':tables,
            'targets':{PROXIMITY_TARGET:verify_proximity(root/'outputs'/PROXIMITY_TARGET,'result')},'exact_inventory_verified':True}


def diagnostic_worker(action, root):
    """Four tiny valid physical sources; no output is permitted by this case."""
    from osgeo import ogr, osr
    if action == 'seed':
        if any((root/name).exists() for name in DIAGNOSTIC_SOURCES) or list((root/'outputs').iterdir()):
            raise FixtureError('refusing existing diagnostic data')
        for i, name in enumerate(DIAGNOSTIC_SOURCES):
            crs=osr.SpatialReference();crs.ImportFromEPSG(3395 if i==2 else 3857)
            ds=ogr.GetDriverByName('GPKG').CreateDataSource(str(root/name))
            layer=ds.CreateLayer(Path(name).stem,crs,ogr.wkbLineString,options=['GEOMETRY_NAME=source_shape'])
            for field,kind in [('route_key',ogr.OFTString),('note',ogr.OFTString),*([('distance',ogr.OFTReal)] if i==3 else [])]:
                if layer.CreateField(ogr.FieldDefn(field,kind))!=0:raise FixtureError('cannot seed diagnostic field')
            row=ogr.Feature(layer.GetLayerDefn());row.SetField('route_key','route-'+str(i))
            row.SetField('note','PRIVATE_DIAGNOSTIC_SOURCE_VALUE')
            if i==3:row.SetField('distance',99.)
            row.SetGeometry(ogr.CreateGeometryFromWkt(f'LINESTRING (0 {i}, 10 {i})'))
            if layer.CreateFeature(row)!=0:raise FixtureError('cannot seed diagnostic source')
            row=None;layer=None;ds=None
    elif action!='verify':raise FixtureError('unknown diagnostic action')
    if sorted(p.name for p in root.iterdir())!=sorted(['.addp-online-owner','outputs',*DIAGNOSTIC_SOURCES]) or list((root/'outputs').iterdir()):
        raise FixtureError('diagnostic must have exact sources and no physical publication')
    sources={}
    for i,name in enumerate(DIAGNOSTIC_SOURCES):
        ds=ogr.Open(str(root/name),0)
        if ds is None or ds.GetLayerCount()!=1:raise FixtureError('diagnostic requires one source layer')
        layer=ds.GetLayer(0);crs=osr.SpatialReference();crs.ImportFromEPSG(3395 if i==2 else 3857)
        if layer.GetFeatureCount()!=1 or not layer.GetSpatialRef().IsSame(crs):raise FixtureError('diagnostic source count or CRS differs')
        fields=['route_key','note',*(['distance'] if i==3 else [])]
        if [layer.GetLayerDefn().GetFieldDefn(j).GetName() for j in range(layer.GetLayerDefn().GetFieldCount())]!=fields or layer.GetGeometryColumn()!='source_shape':
            raise FixtureError('diagnostic source schema differs')
        row=layer.GetNextFeature();shape=row.GetGeometryRef()
        expected={'route_key':'route-'+str(i),'note':'PRIVATE_DIAGNOSTIC_SOURCE_VALUE',**({'distance':99.} if i==3 else {})}
        if row.items()!=expected or shape.GetGeometryName()!='LINESTRING' or shape.GetCoordinateDimension()!=2 or shape.GetPoints()!=[(0.,float(i)),(10.,float(i))]:
            raise FixtureError('diagnostic source values or coordinates differ')
        sources[name]=digest(root/name)
        row=None;layer=None;ds=None
    return {'sources':sources,'targets':{},'exact_inventory_verified':True}


def buffer_features(kind):
    sources=[{'properties':{'route_key':2**40+i+1,'note':'甲' if i==0 else None,'grade':7 if i==0 else None,'enabled':True if i==0 else None},
              'geometry':{'type':'Point','coordinates':[i*10,0]}} for i in range(2)]
    if kind=='source':return sources
    if kind=='conflict':return [{**r,'properties':{**r['properties'],'distance':99.}} for r in sources]
    if kind!='result':raise FixtureError('unknown buffer table')
    return [{'properties':{**row['properties'],'distance':float(d)},
             'geometry':{'type':'Polygon','coordinates':[[[x+d,y],[x,y-d],[x-d,y],[x,y+d],[x+d,y]]]}}
            for d in (2,1,2) for row in sources for x,y in [row['geometry']['coordinates']]]


def seed_buffer(path,kind):
    from osgeo import ogr,osr
    crs=osr.SpatialReference();crs.ImportFromEPSG(3857)
    ds=ogr.GetDriverByName('GPKG').CreateDataSource(str(path))
    layer=ds.CreateLayer(path.stem,crs,ogr.wkbPoint,options=['GEOMETRY_NAME=buffer_shape'])
    for name,native in [('route_key',ogr.OFTInteger64),('note',ogr.OFTString),('grade',ogr.OFTInteger64),('enabled',ogr.OFTInteger),*([('distance',ogr.OFTReal)] if kind=='conflict' else [])]:
        field=ogr.FieldDefn(name,native)
        if name=='enabled':field.SetSubType(ogr.OFSTBoolean)
        if layer.CreateField(field)!=0:raise FixtureError('cannot seed buffer field')
    for row in buffer_features(kind):
        feature=ogr.Feature(layer.GetLayerDefn())
        for name,value in row['properties'].items():
            if value is not None:feature.SetField(name,value)
        feature.SetGeometry(ogr.CreateGeometryFromJson(json.dumps(row['geometry'])))
        if layer.CreateFeature(feature)!=0:raise FixtureError('cannot seed buffer feature')
    ds=None


def verify_buffer(path,kind):
    from osgeo import ogr,osr
    ds=ogr.Open(str(path),0)
    if ds is None or ds.GetLayerCount()!=1:raise FixtureError('buffer requires one physical layer')
    layer=ds.GetLayer(0);schema=layer.GetLayerDefn();crs=osr.SpatialReference();crs.ImportFromEPSG(3857)
    if not layer.GetSpatialRef() or not layer.GetSpatialRef().IsSame(crs):raise FixtureError('buffer CRS differs')
    expected=buffer_features(kind);fields=list(expected[0]['properties'])
    if [schema.GetFieldDefn(i).GetName() for i in range(schema.GetFieldCount())]!=fields:raise FixtureError('buffer physical fields differ')
    for name in fields:
        field=schema.GetFieldDefn(schema.GetFieldIndex(name))
        native=ogr.OFTString if name=='note' else ogr.OFTReal if name=='distance' else ogr.OFTInteger if name=='enabled' else ogr.OFTInteger64
        if field.GetType()!=native or (name=='enabled' and field.GetSubType()!=ogr.OFSTBoolean):raise FixtureError('buffer physical field type differs')
    rows=[]
    for i,feature in enumerate(layer,1):
        shape=feature.GetGeometryRef()
        if feature.GetFID()!=i or shape is None or shape.GetCoordinateDimension()!=2 or shape.IsMeasured() or not shape.IsValid():raise FixtureError('buffer physical row or geometry differs')
        if kind=='result':
            if shape.GetGeometryName()!='POLYGON' or shape.GetGeometryCount()!=1:raise FixtureError('buffer must contain single-ring Polygon')
            points=[list(p) for p in shape.GetGeometryRef(0).GetPoints()]
            d=(2,1,2)[(i-1)//2] if i<=6 else 0;x=((i-1)%2)*10
            if shape.GetEnvelope()!=(x-d,x+d,-d,d) or shape.GetArea()!=2*d*d or any(math.hypot(px-x,py)!=d for px,py in points):raise FixtureError('buffer exact envelope area or radius differs')
            geometry={'type':'Polygon','coordinates':[points]}
        else:
            if shape.GetGeometryName()!='POINT':raise FixtureError('buffer source must be Point')
            geometry={'type':'Point','coordinates':list(shape.GetPoints()[0])}
        props={name:feature.GetField(name) for name in fields}
        if props['enabled'] is not None:
            if props['enabled'] not in (0,1):raise FixtureError('buffer boolean differs')
            props['enabled']=bool(props['enabled'])
        rows.append({'properties':props,'geometry':geometry})
    if rows!=expected or any(type(row['properties'][key]) is not type(ref['properties'][key]) for row,ref in zip(rows,expected) for key in fields):raise FixtureError('buffer exact coordinates order attributes or types differ')
    if not schema.GetGeomFieldDefn(0).GetName() or (kind!='result' and schema.GetGeomFieldDefn(0).GetName()!='buffer_shape'):raise FixtureError('buffer registered geometry name differs')
    return {**digest(path),'feature_count':len(rows),'crs':'EPSG:3857','layer_name':layer.GetName(),
            'geometry_column':schema.GetGeomFieldDefn(0).GetName(),'fid_column':layer.GetFIDColumn(),'fields':fields}


def buffer_worker(action,root):
    if action=='seed':
        if any((root/n).exists() for n in BUFFER_SOURCES) or list((root/'outputs').iterdir()):raise FixtureError('refusing existing buffer data')
        for name,kind in zip(BUFFER_SOURCES,('source','conflict')):seed_buffer(root/name,kind)
        return {'sources':{name:digest(root/name) for name in BUFFER_SOURCES}}
    if action!='verify':raise FixtureError('unknown buffer action')
    if sorted(p.name for p in root.iterdir())!=sorted(['.addp-online-owner','outputs',*BUFFER_SOURCES]) or sorted(p.name for p in (root/'outputs').iterdir())!=list(BUFFER_TARGETS):raise FixtureError('buffer physical inventory differs')
    for name,kind in zip(BUFFER_SOURCES,('source','conflict')):verify_buffer(root/name,kind)
    sources={name:digest(root/name) for name in BUFFER_SOURCES}
    return {'sources':sources,'targets':{BUFFER_TARGETS[0]:verify_buffer(root/'outputs'/BUFFER_TARGETS[0],'result')},'exact_inventory_verified':True}


def composition_source_features():
    gap=2**-40
    shapes=[[[0,0],[4,0]],[[3,0],[1,0]],[[10,0],[12,2],[10,4]],
        [[[12,2],[10,0]],[[10,4],[12,2]]],[[0,10],[4,10]],[[2,10],[2,12]],
        [[20,0],[22,0],[22,2],[20,0]],[[[20,0],[22,2],[22,0],[20,0]],[[20,0],[22,0]]],
        [[31,0],[31,0],[30,0],[30,0]],[[31+gap,0],[32,0]],[[40,0],[41,0],[42,0]]]
    grades=[1,2,7,None,1,1,2,None,None,3,4]
    return [{'properties':{'route_key':2**40+i+1,'note':None if i%2==0 else f'原属性-{i}',
                          'grade':grades[i],'enabled':(True,None,False)[i%3],
                          'chain_id':9000+i,'member_no':100+i},
             'geometry':{'type':'MultiLineString' if i in (3,7) else 'LineString','coordinates':points}}
            for i,points in enumerate(shapes)]


def composition_expected_features(port):
    # Handwritten original occurrences and chain traversal; never call production noding/merging.
    gap=2**-40
    edges=[[[0,0],[1,0]],[[1,0],[3,0]],[[3,0],[4,0]],[[10,0],[12,2]],[[10,4],[12,2]],
           [[0,10],[2,10]],[[2,10],[4,10]],[[2,10],[2,12]],
           [[20,0],[22,0]],[[22,0],[22,2]],[[20,0],[22,2]],
           [[30,0],[31,0]],[[31+gap,0],[32,0]],[[40,0],[41,0]],[[41,0],[42,0]]]
    visits=[(0,1,1,1,1,False),(0,1,1,2,2,False),(0,1,1,3,3,False),(1,1,1,1,2,True),
            (2,1,1,1,4,False),(2,1,2,1,5,True),(3,1,1,1,4,True),(3,2,1,1,5,False),
            (4,1,1,1,6,False),(4,1,1,2,7,False),(5,1,1,1,8,False),
            (6,1,1,1,9,False),(6,1,2,1,10,False),(6,1,3,1,11,True),
            (7,1,1,1,11,False),(7,1,2,1,10,True),(7,1,3,1,9,True),(7,2,1,1,9,False),
            (8,1,2,1,12,True),(9,1,1,1,13,False),(10,1,1,1,14,False),(10,1,2,1,15,False)]
    walks=[[(1,False),(2,False),(3,False)],[(4,False),(5,True)],[(6,False)],[(7,False)],[(8,False)],
           [(9,False),(10,False),(11,True)],[(12,False)],[(13,False)],[(14,False),(15,False)]]
    counts={edge:sum(v[4]==edge for v in visits) for edge in range(1,16)}
    row=lambda props,points:{'properties':props,'geometry':{'type':'LineString','coordinates':points}}
    if port=='chains':
        return [row({'chain_id':i,'member_count':len(walk)},
                    [p for n,(edge,reverse) in enumerate(walk) for p in (edges[edge-1][::-1] if reverse else edges[edge-1])[0 if n==0 else 1:]])
                for i,walk in enumerate(walks,1)]
    if port=='chain_edges':
        return [row({'edge_id':edge,'occurrence_count':counts[edge],'chain_id':chain,'member_no':member,
                     'source_part_no':1,'is_reversed':reverse},edges[edge-1])
                for chain,walk in enumerate(walks,1) for member,(edge,reverse) in enumerate(walk,1)]
    if port=='edge_sources':
        sources=composition_source_features()
        return [row({**sources[index]['properties'],'edge_id':edge,'source_id':sources[index]['properties']['route_key'],
                     'source_part_no':part,'source_segment_no':segment,'piece_no':piece,'is_reversed':reverse},
                    edges[edge-1][::-1] if reverse else edges[edge-1]) for index,part,segment,piece,edge,reverse in visits]
    raise FixtureError('unknown composition output')


def network_features(case,kind):
    if case==EXTENSION_CASE:return extension_features(kind)
    if case==PROXIMITY_CASE:return proximity_features(kind)
    if case==BUFFER_CASE:return buffer_features(kind)
    if case==COMPOSITION_CASE:
        return composition_source_features() if kind=='source' else composition_expected_features(kind)
    return normalize_source_features() if kind=='source' else normalize_expected_features(kind)


def verify_composition_relations(tables):
    chains={r['properties']['chain_id']:r for r in tables['chains']}
    edges={r['properties']['edge_id']:r for r in tables['chain_edges']}
    if len(chains)!=9 or len(edges)!=15 or len(tables['edge_sources'])!=22:raise FixtureError('composition cardinality differs')
    if sum(r['properties']['member_count'] for r in chains.values())!=len(edges):raise FixtureError('composition unique edge count differs')
    combinations=set()
    for edge_id,edge in edges.items():
        p=edge['properties'];sources=[r for r in tables['edge_sources'] if r['properties']['edge_id']==edge_id]
        if len(sources)!=p['occurrence_count']:raise FixtureError('composition occurrence count differs')
        points=chains[p['chain_id']]['geometry']['coordinates'][p['member_no']-1:p['member_no']+1]
        canonical=edge['geometry']['coordinates']
        if p['source_part_no']!=1 or points!=(canonical[::-1] if p['is_reversed'] else canonical):raise FixtureError('composition chain edge direction differs')
        for source in sources:
            original=source['properties'];reverse=original['is_reversed'] ^ p['is_reversed']
            if (source['geometry']['coordinates']!=(points[::-1] if reverse else points) or
                original['chain_id']<9000 or original['member_no']<100):raise FixtureError('composition original direction or business fields differ')
            combinations.add((original['is_reversed'],p['is_reversed']))
    if combinations!={(False,False),(False,True),(True,False),(True,True)}:raise FixtureError('composition four direction combinations differ')



def topology_case(environment=None):
    value = (os.environ if environment is None else environment).get('ADDP_ONLINE_TOPOLOGY_CASE', 'full')
    if value not in ('full', *INDEPENDENT_CASES):
        raise FixtureError('unknown topology case')
    return value


def normalize_source_features():
    rows = [
        ('业务甲', [[0,0],[4,0],[4,0],[4,2]], '主路', 7, True),
        ('逆向乙', [[3,0],[1,0]], None, None, None),
        ('穿越丙', [[2,-1],[2,1]], 'cross', 9, False),
        ('重复丁', [[[30,0],[32,0],[30,0]],[[32,0],[30,0]]], '重复经过', None, True),
        ('细分戊', [[30,0],[31,0],[32,0]], None, 5, None),
    ]
    return [{'properties':dict(route_key=key,note=note,grade=grade,enabled=enabled),
             'geometry':{'type':'MultiLineString' if key=='重复丁' else 'LineString','coordinates':points}}
            for key,points,note,grade,enabled in rows]


def normalize_expected_features(kind='result'):
    edges = [[[0,0],[1,0]],[[1,0],[2,0]],[[2,0],[3,0]],[[3,0],[4,0]],[[4,0],[4,2]],
             [[2,-1],[2,0]],[[2,0],[2,1]],[[30,0],[31,0]],[[31,0],[32,0]]]
    if kind=='result':
        return [{'properties':{'edge_id':i+1,'occurrence_count':count},
                 'geometry':{'type':'LineString','coordinates':points}}
                for i,(points,count) in enumerate(zip(edges,[1,2,2,1,1,1,1,4,4]))]
    if kind!='members': raise FixtureError('unknown normalization output')
    # Original occurrences, never generated by production intersection/noding.
    visits = [(0,1,1,1,1,False),(0,1,1,2,2,False),(0,1,1,3,3,False),(0,1,1,4,4,False),
              (0,1,3,1,5,False),(1,1,1,1,3,True),(1,1,1,2,2,True),
              (2,1,1,1,6,False),(2,1,1,2,7,False),
              (3,1,1,1,8,False),(3,1,1,2,9,False),(3,1,2,1,9,True),(3,1,2,2,8,True),
              (3,2,1,1,9,True),(3,2,1,2,8,True),(4,1,1,1,8,False),(4,1,2,1,9,False)]
    sources=normalize_source_features()
    return [{'properties':{**sources[row]['properties'],'edge_id':edge,'source_id':sources[row]['properties']['route_key'],
                           'source_part_no':part,'source_segment_no':segment,'piece_no':piece,'is_reversed':reverse},
             'geometry':{'type':'LineString','coordinates':edges[edge-1][::-1] if reverse else edges[edge-1]}}
            for row,part,segment,piece,edge,reverse in visits]


def validate_normalize_features(features,kind='result',case=NORMALIZE_CASE):
    expected=network_features(case,kind)
    if features!=expected: raise FixtureError('normalization exact coordinates, order, attributes or provenance differ')
    for row,oracle in zip(features,expected):
        if list(row['properties'])!=list(oracle['properties']): raise FixtureError('normalization field order differs')
        for field,value in oracle['properties'].items():
            if type(row['properties'][field]) is not type(value):
                raise FixtureError('normalization null, Integer64 or Boolean type differs')


def seed_normalize(path,case=NORMALIZE_CASE):
    from osgeo import ogr,osr
    crs=osr.SpatialReference();crs.ImportFromEPSG(3857)
    ds=ogr.GetDriverByName('GPKG').CreateDataSource(str(path))
    layer=ds.CreateLayer(path.stem,crs,ogr.wkbUnknown,options=['GEOMETRY_NAME=network_shape'])
    fields=[('route_key',ogr.OFTInteger64 if case==COMPOSITION_CASE else ogr.OFTString),('note',ogr.OFTString),('grade',ogr.OFTInteger64),('enabled',ogr.OFTInteger)]
    if case==COMPOSITION_CASE:fields += [('chain_id',ogr.OFTInteger64),('member_no',ogr.OFTInteger64)]
    for name,kind in fields:
        field=ogr.FieldDefn(name,kind)
        if name=='enabled':field.SetSubType(ogr.OFSTBoolean)
        assert layer.CreateField(field)==0
    for row in network_features(case,'source'):
        feature=ogr.Feature(layer.GetLayerDefn())
        for field,value in row['properties'].items():
            if value is not None:feature.SetField(field,value)
        feature.SetGeometry(ogr.CreateGeometryFromJson(json.dumps(row['geometry'])))
        if layer.CreateFeature(feature)!=0:raise FixtureError('cannot seed normalization source')
    ds=None


def verify_normalize(path,kind='result',case=NORMALIZE_CASE,records=None):
    from osgeo import ogr,osr
    ds=ogr.Open(str(path),0)
    if ds is None or ds.GetLayerCount()!=1:raise FixtureError('normalization requires exactly one GPKG layer')
    layer=ds.GetLayer(0);schema=layer.GetLayerDefn();crs=osr.SpatialReference();crs.ImportFromEPSG(3857)
    if not layer.GetSpatialRef() or not layer.GetSpatialRef().IsSame(crs):raise FixtureError('normalization CRS differs')
    expected=network_features(case,kind)
    fields=list(expected[0]['properties'])
    if [schema.GetFieldDefn(i).GetName() for i in range(schema.GetFieldCount())]!=fields:
        raise FixtureError('normalization physical fields differ')
    for field in fields:
        definition=schema.GetFieldDefn(schema.GetFieldIndex(field))
        native=ogr.OFTString if field=='note' or case==NORMALIZE_CASE and field in ('route_key','source_id') else ogr.OFTInteger if field in ('enabled','is_reversed') else ogr.OFTInteger64
        if definition.GetType()!=native or (field in ('enabled','is_reversed') and definition.GetSubType()!=ogr.OFSTBoolean):
            raise FixtureError('normalization physical Integer64/Boolean schema differs')
    rows=[]
    for number,feature in enumerate(layer,1):
        if feature.GetFID()!=number:raise FixtureError('normalization FID order differs')
        shape=feature.GetGeometryRef()
        if shape is None or shape.GetCoordinateDimension()!=2 or shape.IsMeasured() or not shape.IsValid():raise FixtureError('normalization geometry invalid')
        if shape.GetGeometryName()=='LINESTRING':
            geometry={'type':'LineString','coordinates':[list(p) for p in shape.GetPoints()]}
        elif kind=='source' and shape.GetGeometryName()=='MULTILINESTRING':
            geometry={'type':'MultiLineString','coordinates':[[list(p) for p in shape.GetGeometryRef(i).GetPoints()] for i in range(shape.GetGeometryCount())]}
        else:raise FixtureError('normalization geometry type differs')
        props={field:feature.GetField(field) for field in fields}
        for field in ('enabled','is_reversed'):
            if field in props and props[field] is not None:
                if props[field] not in (0,1):raise FixtureError('normalization Boolean value differs')
                props[field]=bool(props[field])
        rows.append({'properties':props,'geometry':geometry})
    validate_normalize_features(rows,kind,case)
    if records is not None:records[kind]=rows
    return {**digest(path),'feature_count':len(rows),'crs':'EPSG:3857','layer_name':layer.GetName(),
            'geometry_column':schema.GetGeomFieldDefn(0).GetName(),'fid_column':layer.GetFIDColumn(),'fields':fields}


def normalize_worker(action,root,case=NORMALIZE_CASE):
    profile=network_profile(case);source=profile['source'];targets=profile['targets'];ports=profile['ports']
    if action=='seed':
        if (root/source).exists() or list((root/'outputs').iterdir()):raise FixtureError('refusing existing data')
        seed_normalize(root/source,case)
        return {'sources':{source:digest(root/source)}}
    if action!='verify':raise FixtureError('unknown normalization action')
    if (sorted(p.name for p in root.iterdir())!=sorted(['.addp-online-owner','outputs',source]) or
        sorted(p.name for p in (root/'outputs').iterdir())!=sorted(targets)):
        raise FixtureError('normalization inventory contains extra source, output or sidecar')
    verify_normalize(root/source,'source',case)
    records={}
    outputs={name:verify_normalize(root/'outputs'/name,port,case,records) for name,port in zip(targets,ports)}
    if case==COMPOSITION_CASE:verify_composition_relations(records)
    return {'sources':{source:digest(root/source)},'targets':outputs,'exact_inventory_verified':True}


# Explicit coordinates are independent of the production matching algorithm.
SNAP_INPUT = (((0,0),(10,0)), ((0,10),(10,10)), ((0,20),(10,20)), ((5,30),(5,35)), ((50,40),(52,40)))
SNAP_REFERENCE = (((.5,0),(1,0)), ((10,0),(11,0)), ((.75,10),(10,10)), ((-.5,20),(.5,20)), ((0,30),(10,30)), ((51,40),(51,45)))
SNAP_EXPECTED = (((.5,0),(10,0)), ((0,10),(10,10)), ((-.5,20),(10,20)), ((5,30),(5,35)), ((50,40),(52,40)))


def rectangle(x0, y0, x1, y1):
    return [[x0,y0],[x1,y0],[x1,y1],[x0,y1],[x0,y0]]


def line_chain_source_features():
    rows = [
        ('中段', [[12,300],[14,300]], '主', 7), ('左段', [[10,300],[12,300]], '主', 7),
        ('反右', [[16,300],[14,300]], '主', 7),
        ('支左', [[28,300],[30,300]], '支', 8), ('支右', [[30,300],[32,300]], '支', 8),
        ('异支', [[30,300],[30,302]], '另', 8),
        ('变甲', [[40,300],[42,300]], '变', 9), ('变乙', [[42,300],[44,300]], '变', 10),
        ('空甲', [[50,300],[52,300]], None, 10), ('空乙', [[54,300],[52,300]], None, 10),
        ('微断', [[[0,320],[1,320]],[[1+2**-40,320],[2,320]]], '微', 11),
        ('环首', [[72,300],[72,302]], '环', 12), ('环接', [[70,300],[72,300]], '环', 12),
        ('环尾', [[72,302],[70,300]], '环', 12),
        ('原重', [[90,300],[90,300],[92,300],[92,300]], '原', 13),
        ('同源', [[[100,300],[102,300]],[[104,300],[102,300]]], '同源', 14),
    ]
    return [{'properties':{'route_key':key,'note':None if i in (1,8) else f'chain-original-{i}',
                           'grade':grade,'road':road},
             'geometry':{'type':'MultiLineString' if key in ('微断','同源') else 'LineString','coordinates':points}}
            for i,(key,points,road,grade) in enumerate(rows)]


def line_chain_expected_features(members=False):
    # Explicit original membership and directions. No production traversal.
    chains = [([(1,1,False),(0,1,False),(2,1,True)], [[10,300],[12,300],[14,300],[16,300]]),
        ([(3,1,False)],[[28,300],[30,300]]), ([(4,1,False)],[[30,300],[32,300]]),
        ([(5,1,False)],[[30,300],[30,302]]), ([(6,1,False)],[[40,300],[42,300]]),
        ([(7,1,False)],[[42,300],[44,300]]), ([(8,1,False),(9,1,True)],[[50,300],[52,300],[54,300]]),
        ([(10,1,False)],[[0,320],[1,320]]), ([(10,2,False)],[[1+2**-40,320],[2,320]]),
        ([(11,1,False),(13,1,False),(12,1,False)],[[72,300],[72,302],[70,300],[72,300]]),
        ([(14,1,False)],[[90,300],[90,300],[92,300],[92,300]]),
        ([(15,1,False),(15,2,True)],[[100,300],[102,300],[104,300]])]
    sources=line_chain_source_features();result=[]
    for chain_id,(entries,coordinates) in enumerate(chains,1):
        if not members:
            attrs=sources[entries[0][0]]['properties']
            result.append({'properties':{'road':attrs['road'],'grade':attrs['grade'],'chain_id':chain_id,'member_count':len(entries)},
                           'geometry':{'type':'LineString','coordinates':coordinates}})
            continue
        for member_no,(source,part,reverse) in enumerate(entries,1):
            original=sources[source];shape=original['geometry'];points=shape['coordinates'] if shape['type']=='LineString' else shape['coordinates'][part-1]
            result.append({'properties':{**original['properties'],'chain_id':chain_id,'member_no':member_no,
                                       'source_part_no':part,'is_reversed':reverse},
                           'geometry':{'type':'LineString','coordinates':points}})
    return result


def validate_line_chain_features(features, kind='result'):
    expected=line_chain_source_features() if kind=='source' else line_chain_expected_features(kind=='members')
    _validate_point_features(features,expected)
    for row in features:
        for name in ('grade','chain_id','member_count','member_no','source_part_no'):
            if name in row['properties'] and type(row['properties'][name]) is not int:
                raise FixtureError('chain integer attribute or membership ordinal differs')
        if kind=='members' and type(row['properties']['is_reversed']) is not bool:
            raise FixtureError('chain direction must remain Boolean')


def seed_line_chain(path):
    from osgeo import ogr
    _seed_attributed_line_source(path,line_chain_source_features(),extra_fields=[('road',ogr.OFTString)])


def verify_line_chain(path, kind='result'):
    source=kind=='source';expected=line_chain_source_features() if source else line_chain_expected_features(kind=='members')
    return _verify_attributed_lines(path,expected,lambda rows,_:validate_line_chain_features(rows,kind),source)


def line_component_source_features():
    tiny = 2**-40
    rows = [('多源', [[[14,260],[12,260],[10,260]], [[30,260],[32,260]], rectangle(50,260,52,262)]),
            ('T支', [[12,258],[12,260]]), ('桥甲', [[34,260],[35,260]]),
            ('桥接', [[32,260],[34,260]]), ('反重', [[14,260],[10,260]]),
            ('自交', [[70,260],[72,262],[70,262],[72,260]]),
            ('自叠', [[80,260],[82,260],[80,260]]),
            ('微断', [[[0,280],[1,280]], [[1+tiny,280],[2,280]]]),
            ('同接', [[[10,266],[12,266]], [[12,266],[14,266]]]),
            ('十交', [[11,259],[11,261]]),
            ('原重', [[90,260],[90,260],[92,260],[92,260]])]
    return [{'properties': {'route_key': identity, 'note': None if index == 1 else f'component-original-{index}', 'grade': 200+index},
             'geometry': {'type': 'MultiLineString' if identity in ('多源','微断','同接') else 'LineString', 'coordinates': points}}
            for index, (identity, points) in enumerate(rows)]


def line_component_expected_features():
    # Hand-specified memberships and original paths, never a grouping algorithm.
    rows = [(0,1,1,[[14,260],[12,260],[10,260]]), (0,2,2,[[30,260],[32,260]]),
            (0,3,3,rectangle(50,260,52,262)), (1,1,1,[[12,258],[12,260]]),
            (2,1,2,[[34,260],[35,260]]), (3,1,2,[[32,260],[34,260]]),
            (4,1,1,[[14,260],[10,260]]), (5,1,4,[[70,260],[72,262],[70,262],[72,260]]),
            (6,1,5,[[80,260],[82,260],[80,260]]), (7,1,6,[[0,280],[1,280]]),
            (7,2,7,[[1+2**-40,280],[2,280]]), (8,1,8,[[10,266],[12,266]]),
            (8,2,8,[[12,266],[14,266]]), (9,1,1,[[11,259],[11,261]]),
            (10,1,9,[[90,260],[90,260],[92,260],[92,260]])]
    sources = line_component_source_features()
    return [{'properties': {**sources[index]['properties'], 'source_id': sources[index]['properties']['route_key'],
                            'part_no': part, 'component_id': component},
             'geometry': {'type': 'LineString', 'coordinates': points}} for index,part,component,points in rows]


def validate_line_component_features(features, source=False):
    expected = line_component_source_features() if source else line_component_expected_features()
    _validate_point_features(features, expected)
    for feature in features:
        for name in ('grade',) if source else ('grade','part_no','component_id'):
            if type(feature['properties'][name]) is not int:
                raise FixtureError('line component attributes and group numbers must remain integers')


def seed_line_component(path):
    _seed_attributed_line_source(path, line_component_source_features())


def verify_line_component(path, source=False):
    return _verify_attributed_lines(path, line_component_source_features() if source else line_component_expected_features(),
                                    validate_line_component_features, source)


def split_line_source_features():
    rows = [('主线', [[10,200],[12,200],[16,200]]),
            ('横交', [[12,202],[12,198]]), ('T支', [[14,198],[14,200]]),
            ('端接', [[16,200],[17,201]]),
            ('多部', [[[34,200],[30,200]],[[34,204],[30,204]]]),
            ('多交', [[32,198],[32,202]]),
            ('闭环', rectangle(50,200,52,202)), ('环接', [[51,198],[51,200]]),
            ('微线', [[0,220],[1,220]]), ('微交', [[2**-40,219],[2**-40,221]]),
            ('原重', [[70,200],[70,200],[72,200],[72,200]]),
            ('内外交', [[100,200],[104,204],[100,204],[104,200]]),
            ('穿越', [[101,199],[101,205]]),
            ('反闭自交', [[120,200],[124,200],[120,204],[124,204],[120,200]]),
            ('多次到访', [[140,200],[142,200],[142,202],[140,202],[140,200],[138,200],[138,198],[140,198],[140,200],[142,198]]),
            ('同源多交', [[[164,200],[160,200]],[[161,198],[161,202]]]),
            ('切点原重', [[180,200],[182,202],[182,202],[184,204],[180,204],[182,202],[182,202],[184,200],[184,200]])]
    return [{'properties': {'route_key': identity, 'note': None if index in (1,11,15) else f'split-original-{index}', 'grade': 100+index},
             'geometry': {'type': 'MultiLineString' if identity in ('多部','同源多交') else 'LineString', 'coordinates': points}}
            for index, (identity, points) in enumerate(rows)]


def split_line_expected_features():
    # Explicit original-path fragments, not a call to the splitting algorithm.
    rows = [(0,1,1,[[10,200],[12,200]]), (0,1,2,[[12,200],[14,200]]), (0,1,3,[[14,200],[16,200]]),
            (1,1,1,[[12,202],[12,200]]), (1,1,2,[[12,200],[12,198]]),
            (2,1,1,[[14,198],[14,200]]), (3,1,1,[[16,200],[17,201]]),
            (4,1,1,[[34,200],[32,200]]), (4,1,2,[[32,200],[30,200]]), (4,2,1,[[34,204],[30,204]]),
            (5,1,1,[[32,198],[32,200]]), (5,1,2,[[32,200],[32,202]]),
            (6,1,1,[[50,200],[51,200]]), (6,1,2,[[51,200],[52,200],[52,202],[50,202],[50,200]]),
            (7,1,1,[[51,198],[51,200]]),
            (8,1,1,[[0,220],[2**-40,220]]), (8,1,2,[[2**-40,220],[1,220]]),
            (9,1,1,[[2**-40,219],[2**-40,220]]), (9,1,2,[[2**-40,220],[2**-40,221]]),
            (10,1,1,[[70,200],[70,200],[72,200],[72,200]]),
            (11,1,1,[[100,200],[101,201]]), (11,1,2,[[101,201],[102,202]]),
            (11,1,3,[[102,202],[104,204],[101,204]]), (11,1,4,[[101,204],[100,204],[101,203]]),
            (11,1,5,[[101,203],[102,202]]), (11,1,6,[[102,202],[104,200]]),
            (12,1,1,[[101,199],[101,201]]), (12,1,2,[[101,201],[101,203]]),
            (12,1,3,[[101,203],[101,204]]), (12,1,4,[[101,204],[101,205]]),
            (13,1,1,[[120,200],[124,200],[122,202]]),
            (13,1,2,[[122,202],[120,204],[124,204],[122,202]]), (13,1,3,[[122,202],[120,200]]),
            (14,1,1,[[140,200],[142,200],[142,202],[140,202],[140,200]]),
            (14,1,2,[[140,200],[138,200],[138,198],[140,198],[140,200]]), (14,1,3,[[140,200],[142,198]]),
            (15,1,1,[[164,200],[161,200]]), (15,1,2,[[161,200],[160,200]]),
            (15,2,1,[[161,198],[161,200]]), (15,2,2,[[161,200],[161,202]]),
            (16,1,1,[[180,200],[182,202]]),
            (16,1,2,[[182,202],[182,202],[184,204],[180,204],[182,202]]),
            (16,1,3,[[182,202],[182,202],[184,200],[184,200]])]
    sources = split_line_source_features()
    return [{'properties': {**sources[index]['properties'], 'source_id': sources[index]['properties']['route_key'],
                            'part_no': part, 'segment_no': segment},
             'geometry': {'type': 'LineString', 'coordinates': points}} for index,part,segment,points in rows]


def validate_split_line_features(features, source=False):
    expected = split_line_source_features() if source else split_line_expected_features()
    _validate_point_features(features, expected)
    for feature in features:
        for name in ('grade',) if source else ('grade','part_no','segment_no'):
            if type(feature['properties'][name]) is not int:
                raise FixtureError('split line integer attributes and ordinals must remain integers')


def seed_split_line(path):
    _seed_attributed_line_source(path, split_line_source_features())


def verify_split_line(path, source=False):
    return _verify_attributed_lines(path, split_line_source_features() if source else split_line_expected_features(),
                                    validate_split_line_features, source)


def _seed_attributed_line_source(path, features, extra_fields=()):
    from osgeo import ogr, osr
    crs = osr.SpatialReference(); crs.ImportFromEPSG(3857)
    dataset = ogr.GetDriverByName('GPKG').CreateDataSource(str(path))
    layer = dataset.CreateLayer(path.stem, crs, ogr.wkbUnknown)
    for name, kind in [('route_key',ogr.OFTString),('note',ogr.OFTString),('grade',ogr.OFTInteger64),*extra_fields]:
        layer.CreateField(ogr.FieldDefn(name,kind))
    for row in features:
        feature = ogr.Feature(layer.GetLayerDefn())
        for name, value in row['properties'].items():
            if value is not None: feature.SetField(name,value)
        feature.SetGeometry(ogr.CreateGeometryFromJson(json.dumps(row['geometry'])))
        if layer.CreateFeature(feature) != 0: raise FixtureError('cannot seed attributed line source')
    dataset = None


def _verify_attributed_lines(path, expected, validate, source):
    from osgeo import ogr, osr
    dataset = ogr.Open(str(path),0)
    if dataset is None or dataset.GetLayerCount()!=1: raise FixtureError('expected one attributed line GeoPackage layer')
    layer = dataset.GetLayer(0); crs = osr.SpatialReference(); crs.ImportFromEPSG(3857)
    if not layer.GetSpatialRef() or not layer.GetSpatialRef().IsSame(crs): raise FixtureError('attributed line CRS differs')
    fields = list(expected[0]['properties']); schema = layer.GetLayerDefn()
    if ([schema.GetFieldDefn(i).GetName() for i in range(schema.GetFieldCount())] != fields or
        any(schema.GetFieldDefn(i).GetType() != (ogr.OFTInteger64 if name in ('grade','part_no','segment_no','component_id','chain_id','member_count','member_no','source_part_no') else ogr.OFTInteger if name=='is_reversed' else ogr.OFTString) for i,name in enumerate(fields))):
        raise FixtureError('attributed line attribute/ordinal field types differ')
    if 'is_reversed' in fields and schema.GetFieldDefn(schema.GetFieldIndex('is_reversed')).GetSubType()!=ogr.OFSTBoolean:
        raise FixtureError('chain direction Boolean field subtype differs')
    features = []
    for feature in layer:
        geometry = feature.GetGeometryRef()
        if geometry is None or geometry.GetCoordinateDimension()!=2 or not geometry.IsValid():
            raise FixtureError('attributed line geometry invalid or non-XY')
        kind = geometry.GetGeometryName()
        if kind == 'LINESTRING':
            shape = {'type':'LineString','coordinates':[list(point[:2]) for point in geometry.GetPoints()]}
        elif kind == 'MULTILINESTRING' and source:
            shape = {'type':'MultiLineString','coordinates':[[list(point[:2]) for point in geometry.GetGeometryRef(i).GetPoints()] for i in range(geometry.GetGeometryCount())]}
        else: raise FixtureError('attributed line geometry type differs')
        properties={name:feature.GetField(name) for name in fields}
        if 'is_reversed' in fields:
            if properties['is_reversed'] not in (0,1): raise FixtureError('invalid chain Boolean direction value')
            properties['is_reversed']=bool(properties['is_reversed'])
        features.append({'properties':properties,'geometry':shape})
    validate(features,source)
    result = {**digest(path),'feature_count':len(features),'crs':'EPSG:3857','layer_name':layer.GetName(),
              'geometry_column':schema.GetGeomFieldDefn(0).GetName(),'fid_column':layer.GetFIDColumn(),'fields':fields}
    dataset = None
    return result


def line_self_intersection_source_features():
    tiny=2**-40
    rows=[('自蝴',[[800,140],[802,142],[800,142],[802,140]]),
          ('回访',[[820,140],[821,140],[821,141],[820,141],[821,140],[822,140]]),
          ('部接',[[[840,140],[844,140]],[[842,139],[842,141]],[[844,140],[845,141]]]),
          ('常零',[[860,140],[861,140],[861,140],[862,141]]),
          ('环缝',rectangle(870,140,872,142)),
          ('重复',[[[880,140],[881,140],[882,140]],[[880,140],[881,140],[882,140]]]),
          ('叠支',[[[900,140],[904,140]],[[902,140],[906,140]],
                   [[901,139],[901,141]],[[902,139],[902,141]],[[903,139],[903,141]],
                   [[904,139],[904,141]],[[905,139],[905,141]]]),
          ('邻返',[[[920,140],[924,140],[920,140]],[[922,139],[922,141]]]),
          ('叠独',[[[940,140],[944,140]],[[942,140],[946,140]],[[943,139],[943,141]],
                   [[950,140],[952,142],[950,142],[952,140]]]),
          ('微点',[[[0,160],[1,160]],[[0,159],[0,161]],[[tiny,159],[tiny,161]]]),
          ('同甲',[[980,140],[982,142],[980,142],[982,140]]),
          ('同乙',[[980,140],[982,142],[980,142],[982,140]]),
          ('近线',[[[1000,140],[1004,140]],[[1000,140+tiny],[1004,140+tiny]]]),
          ('外甲',[[1020,140],[1024,140]]),('外乙',[[1022,139],[1022,141]]),
          ('部环',[[[1040,140],[1042,140]],[[1042,140],[1042,142]],
                   [[1042,142],[1040,142]],[[1040,142],[1040,140]]])]
    multipart={'部接','重复','叠支','邻返','叠独','微点','近线','部环'}
    return [{'properties':{'route_key':identity,'note':f'self-intersection-original-{index}'},
             'geometry':{'type':'MultiLineString' if identity in multipart else 'LineString','coordinates':points}}
            for index,(identity,points) in enumerate(rows)]


def line_self_intersection_expected_features():
    # Handwritten isolated original XY: no production intersection/union oracle.
    rows=[('自蝴',[(801,141)]),('回访',[(821,140)]),('部接',[(842,140),(844,140)]),
          ('叠支',[(901,140),(905,140)]),('叠独',[(951,141)]),
          ('微点',[(0,160),(2**-40,160)]),('同甲',[(981,141)]),('同乙',[(981,141)]),
          ('部环',[(1040,140),(1040,142),(1042,140),(1042,142)])]
    return [{'properties':{'source_id':identity},'geometry':{'type':'Point','coordinates':list(point)}}
            for identity,points in rows for point in points]


def validate_line_self_intersection_features(features, source=False):
    return _validate_point_features(features,line_self_intersection_source_features() if source else line_self_intersection_expected_features())


def verify_line_self_intersection(path, source=False):
    return _verify_point_features(path,line_self_intersection_source_features() if source else line_self_intersection_expected_features(),source)


def line_intersection_source_features():
    tiny=2**-40
    rows=[('交横',[[600,100],[604,100]]),('交纵',[[602,98],[602,102]]),('交斜',[[600,98],[604,102]]),
          ('T干',[[620,100],[624,100]]),('T支',[[622,100],[622,102]]),
          ('接甲',[[640,100],[642,100]]),('接乙',[[642,100],[643,101]]),
          ('叠甲',[[660,100],[664,100]]),('叠乙',[[664,100],[662,100],[660,100]]),
          ('混甲',[[[680,100],[684,100]],[[686,99],[686,101]]]),
          ('混乙',[[[681,100],[683,100]],[[685,100],[687,100]]]),
          ('多甲',[[[700,100],[704,100]],[[704,100],[700,100]]]),
          ('多乙',[[[703,99],[703,101]],[[701,99],[701,101]],[[701,101],[701,99]]]),
          ('微横',[[0,120],[1,120]]),('微纵',[[[0,119],[0,121]],[[tiny,119],[tiny,121]]]),
          ('近甲',[[740,100],[742,100]]),('近乙',[[742+tiny,100],[744,100]]),
          ('自交',[[760,100],[762,102],[760,102],[762,100]])]
    return [{'properties':{'route_key':identity,'note':f'intersection-original-{index}'},
             'geometry':{'type':'MultiLineString' if identity in ('混甲','混乙','多甲','多乙','微纵') else 'LineString','coordinates':points}}
            for index,(identity,points) in enumerate(rows)]


def line_intersection_expected_features():
    # Handwritten original XY and original business IDs, independent of GEOS.
    rows=[('交横','交纵',(602,100)),('交横','交斜',(602,100)),('交纵','交斜',(602,100)),
          ('T干','T支',(622,100)),('接甲','接乙',(642,100)),('混甲','混乙',(686,100)),
          ('多甲','多乙',(701,100)),('多甲','多乙',(703,100)),
          ('微横','微纵',(0,120)),('微横','微纵',(2**-40,120))]
    return [{'properties':{'source_id_a':a,'source_id_b':b},'geometry':{'type':'Point','coordinates':list(point)}} for a,b,point in rows]


def validate_line_intersection_features(features, source=False):
    return _validate_point_features(features,line_intersection_source_features() if source else line_intersection_expected_features())


def verify_line_intersection(path, source=False):
    return _verify_point_features(path,line_intersection_source_features() if source else line_intersection_expected_features(),source)


def line_self_overlap_source_features():
    rows = [('折返',[[400,80],[404,80],[400,80]]),
            ('三过',[[410,80],[414,80],[410,80],[414,80]]),
            ('采样',[[420,80],[422,80],[424,80],[423,80],[421,80],[420,80]]),
            ('多部',[[[430,80],[434,80]],[[432,80],[436,80]],[[430,82],[434,82]],[[432,82],[436,82]],[[436,80],[437,80]]]),
            ('微线',[[440,0],[440,2**-40],[440,0]]),
            ('闭环',rectangle(450,80,452,82)),
            ('点交',[[460,80],[462,82],[460,82],[462,80]]),
            ('邻线',[[[470,80],[474,80]],[[470,80+2**-40],[474,80+2**-40]]]),
            ('零点',[[480,80],[480,80],[484,80],[484,80]]),
            ('零折',[[490,80],[490,80],[494,80],[494,80],[490,80]]),
            ('正向',[[[500,80],[504,80]],[[500,80],[504,80]]]),
            ('独甲',[[510,80],[514,80]]), ('独乙',[[510,80],[514,80]]),
            ('双环',rectangle(520,80,522,82)+rectangle(520,80,522,82)[1:]),
            ('接点',[[[530,80],[532,80]],[[532,80],[534,80]]])]
    return [{'properties':{'route_key':identity,'note':f'self-original-{index}'},
             'geometry':{'type':'MultiLineString' if identity in ('多部','邻线','正向','接点') else 'LineString',
                         'coordinates':coordinates}} for index,(identity,coordinates) in enumerate(rows)]


def line_self_overlap_expected_features():
    # Handwritten original-coordinate oracle: repeated visits never multiply length.
    rows = [('折返',4,[[[400,80],[404,80]]]), ('三过',4,[[[410,80],[414,80]]]),
            ('采样',4,[[[420,80],[421,80]],[[421,80],[422,80]],[[422,80],[423,80]],[[423,80],[424,80]]]),
            ('多部',4,[[[432,80],[434,80]],[[432,82],[434,82]]]),
            ('微线',2**-40,[[[440,0],[440,2**-40]]]), ('零折',4,[[[490,80],[494,80]]]),
            ('正向',4,[[[500,80],[504,80]]]),
            ('双环',8,[[[520,80],[522,80]],[[522,80],[522,82]],[[522,82],[520,82]],[[520,82],[520,80]]])]
    return [{'properties':{'source_id':identity,'overlap_length':length},
             'geometry':{'type':'MultiLineString','coordinates':parts}} for identity,length,parts in rows]


def validate_line_self_overlap_features(features, source=False):
    return _validate_linear_features(features, line_self_overlap_source_features() if source else line_self_overlap_expected_features(), source)


def verify_line_self_overlap(path, source=False):
    return _verify_linear(path, line_self_overlap_source_features() if source else line_self_overlap_expected_features(), source)


def line_overlap_source_features():
    rows = [('甲',[[300,60],[304,60]]), ('乙',[[302,60],[306,60]]),
            ('反甲',[[304,60],[300,60]]), ('内线',[[301,60],[302,60]]),
            ('接点',[[306,60],[307,60]]), ('交叉',[[305,59],[305,61]]),
            ('邻线',[[300,60+2**-40],[304,60+2**-40]]),
            ('多甲',[[[320,60],[324,60]],[[330,60],[334,60]],[[340,60],[341,60]]]),
            ('多乙',[[[322,60],[326,60]],[[332,60],[336,60]],[[341,60],[342,60]]]),
            ('微甲',[[350,0],[350,2**-40]]), ('微乙',[[350,2**-40],[350,0]]),
            ('自叠',[[360,60],[362,60],[360,60]]),
            ('折返',[[370,60],[372,60],[370,60]]), ('对照',[[371,60],[373,60]]),
            ('采样',[[380,60],[381,60],[382,60]]), ('反采',[[382,60],[380,60]])]
    return [{'properties':{'route_key':identifier,'note':f'line-original-{index}'},
             'geometry':{'type':'MultiLineString' if identifier in ('多甲','多乙') else 'LineString',
                         'coordinates':coordinates}} for index,(identifier,coordinates) in enumerate(rows)]


def line_overlap_expected_features():
    # Hand-specified pair intersections; never call the production algorithm.
    rows = [('甲','乙',2,[[[302,60],[304,60]]]), ('甲','反甲',4,[[[300,60],[304,60]]]),
            ('甲','内线',1,[[[301,60],[302,60]]]), ('乙','反甲',2,[[[302,60],[304,60]]]),
            ('反甲','内线',1,[[[301,60],[302,60]]]),
            ('多甲','多乙',4,[[[322,60],[324,60]],[[332,60],[334,60]]]),
            ('微甲','微乙',2**-40,[[[350,0],[350,2**-40]]]),
            ('折返','对照',1,[[[371,60],[372,60]]]),
            ('采样','反采',2,[[[380,60],[381,60]],[[381,60],[382,60]]])]
    return [{'properties':{'source_id_a':a,'source_id_b':b,'overlap_length':length},
             'geometry':{'type':'MultiLineString','coordinates':parts}} for a,b,length,parts in rows]


def validate_line_overlap_features(features, source=False):
    return _validate_linear_features(features, line_overlap_source_features() if source else line_overlap_expected_features(), source)


def _validate_linear_features(features, expected, source=False):
    if len(features)!=len(expected):raise FixtureError('line overlap source/pair count differs')
    for actual,wanted in zip(features,expected):
        if actual['properties']!=wanted['properties'] or actual['geometry']['type']!=wanted['geometry']['type']:
            raise FixtureError('line overlap source IDs, order, attributes, length or type differ')
        if source:
            if actual['geometry']!=wanted['geometry']:raise FixtureError('line overlap source coordinates changed')
        else:
            length=actual['properties']['overlap_length']
            if type(length) not in (int,float) or not math.isfinite(length) or length<=0:
                raise FixtureError('line overlap length must be finite positive')
        def parts(shape):
            return shape['coordinates'] if shape['type']=='MultiLineString' else [shape['coordinates']]
        actual_parts,wanted_parts=parts(actual['geometry']),parts(wanted['geometry'])
        if sorted(canonical(part) for part in actual_parts)!=sorted(canonical(part) for part in wanted_parts):
            raise FixtureError('line overlap exact components or coordinates differ')


def seed_line_overlap(path):
    return _seed_linear(path, line_overlap_source_features())


def _seed_linear(path, features):
    from osgeo import ogr,osr
    crs=osr.SpatialReference();crs.ImportFromEPSG(3857)
    dataset=ogr.GetDriverByName('GPKG').CreateDataSource(str(path));layer=dataset.CreateLayer(path.stem,crs,ogr.wkbUnknown)
    for name in ('route_key','note'):layer.CreateField(ogr.FieldDefn(name,ogr.OFTString))
    for row in features:
        feature=ogr.Feature(layer.GetLayerDefn())
        for name,value in row['properties'].items():feature.SetField(name,value)
        feature.SetGeometry(ogr.CreateGeometryFromJson(json.dumps(row['geometry'])))
        if layer.CreateFeature(feature)!=0:raise FixtureError('cannot seed line overlap source')
    dataset=None


def verify_line_overlap(path, source=False):
    return _verify_linear(path, line_overlap_source_features() if source else line_overlap_expected_features(), source)


def _verify_linear(path, expected, source=False):
    from osgeo import ogr,osr
    dataset=ogr.Open(str(path),0)
    if dataset is None or dataset.GetLayerCount()!=1:raise FixtureError('expected one line overlap GeoPackage layer')
    layer=dataset.GetLayer(0);crs=osr.SpatialReference();crs.ImportFromEPSG(3857)
    if not layer.GetSpatialRef() or not layer.GetSpatialRef().IsSame(crs):raise FixtureError('line overlap CRS differs')
    fields=list(expected[0]['properties'])
    schema=layer.GetLayerDefn()
    if ([schema.GetFieldDefn(i).GetName() for i in range(schema.GetFieldCount())]!=fields
        or any(schema.GetFieldDefn(i).GetType()!=(ogr.OFTReal if name=='overlap_length' else ogr.OFTString) for i,name in enumerate(fields))):
        raise FixtureError('line overlap field schema differs')
    features=[]
    for feature in layer:
        geometry=feature.GetGeometryRef()
        if geometry is None or geometry.GetCoordinateDimension()!=2 or not geometry.IsValid():raise FixtureError('line overlap geometry invalid or non-XY')
        kind=geometry.GetGeometryName()
        if kind=='LINESTRING' and source:shape={'type':'LineString','coordinates':[list(point[:2]) for point in geometry.GetPoints()]}
        elif kind=='MULTILINESTRING':shape={'type':'MultiLineString','coordinates':[
            [list(point[:2]) for point in geometry.GetGeometryRef(i).GetPoints()] for i in range(geometry.GetGeometryCount())]}
        else:raise FixtureError('line overlap geometry type differs')
        properties={name:feature.GetField(name) for name in fields}
        if not source and geometry.Length()!=properties['overlap_length']:raise FixtureError('line overlap physical length differs')
        features.append({'properties':properties,'geometry':shape})
    _validate_linear_features(features,expected,source)
    result={**digest(path),'feature_count':len(features),'crs':'EPSG:3857','layer_name':layer.GetName(),
            'geometry_column':schema.GetGeomFieldDefn(0).GetName(),'fid_column':layer.GetFIDColumn(),'fields':fields}
    dataset=None
    return result


def dangling_source_features():
    rows = [('孤线',[[200,0],[202,0]]), ('链甲',[[210,0],[211,0]]), ('链乙',[[211,0],[212,0]]),
            ('干线',[[220,0],[224,0]]), ('支线',[[222,0],[222,2]]),
            ('闭环',rectangle(230,0,232,2)), ('重甲',[[240,0],[242,0]]), ('重乙',[[242,0],[240,0]]),
            ('微甲',[[250,0],[251,0]]), ('微乙',[[251+2**-40,0],[252,0]]),
            ('多部',[[[260,0],[261,0]],[[261,0],[262,0]],[[261,0],[260,0]]]),
            ('叠内',[[240.5,0],[241.5,0]])]
    return [{'properties':{'route_key':identifier,'note':f'original-{index}'},
             'geometry':{'type':'MultiLineString' if identifier=='多部' else 'LineString','coordinates':coordinates}}
            for index,(identifier,coordinates) in enumerate(rows)]


def dangling_expected_features():
    # Explicit endpoints with original IDs; do not compute expected topology.
    rows = [('孤线',[(200,0),(202,0)]), ('链甲',[(210,0)]), ('链乙',[(212,0)]),
            ('干线',[(220,0),(224,0)]), ('支线',[(222,2)]),
            ('重甲',[(240,0),(242,0)]), ('重乙',[(240,0),(242,0)]),
            ('微甲',[(250,0),(251,0)]), ('微乙',[(251+2**-40,0),(252,0)]), ('多部',[(260,0),(262,0)])]
    return [{'properties':{'source_id':identifier},'geometry':{'type':'Point','coordinates':list(point)}}
            for identifier,points in rows for point in points]


def validate_dangling_features(features, source=False):
    return _validate_point_features(features,dangling_source_features() if source else dangling_expected_features())


def _validate_point_features(features, expected):
    if len(features)!=len(expected):raise FixtureError('point source/result count differs')
    for actual,wanted in zip(features,expected):
        if actual!=wanted:raise FixtureError('point source IDs, pairing, order, attributes or exact XY differ')
        geometry=actual['geometry'];kind=geometry.get('type');coordinates=geometry.get('coordinates')
        if kind=='Point':canonical([coordinates])
        elif kind=='LineString':canonical(coordinates)
        elif kind=='MultiLineString':
            for part in coordinates:canonical(part)
        else:raise FixtureError('point source/result geometry type differs')


def seed_dangling(path):
    from osgeo import ogr,osr
    crs=osr.SpatialReference();crs.ImportFromEPSG(3857)
    dataset=ogr.GetDriverByName('GPKG').CreateDataSource(str(path));layer=dataset.CreateLayer(path.stem,crs,ogr.wkbUnknown)
    for name in ('route_key','note'):layer.CreateField(ogr.FieldDefn(name,ogr.OFTString))
    for row in dangling_source_features():
        feature=ogr.Feature(layer.GetLayerDefn())
        for name,value in row['properties'].items():feature.SetField(name,value)
        feature.SetGeometry(ogr.CreateGeometryFromJson(json.dumps(row['geometry'])))
        if layer.CreateFeature(feature)!=0:raise FixtureError('cannot seed dangling source')
    dataset=None


def verify_dangling(path, source=False):
    return _verify_point_features(path,dangling_source_features() if source else dangling_expected_features(),source)


def _verify_point_features(path, expected, source=False):
    from osgeo import ogr,osr
    dataset=ogr.Open(str(path),0)
    if dataset is None or dataset.GetLayerCount()!=1:raise FixtureError('expected one point GeoPackage layer')
    layer=dataset.GetLayer(0);crs=osr.SpatialReference();crs.ImportFromEPSG(3857)
    if not layer.GetSpatialRef() or not layer.GetSpatialRef().IsSame(crs):raise FixtureError('point CRS differs')
    fields=list(expected[0]['properties']);schema=layer.GetLayerDefn()
    if ([schema.GetFieldDefn(i).GetName() for i in range(schema.GetFieldCount())]!=fields
        or any(schema.GetFieldDefn(i).GetType()!=ogr.OFTString for i in range(schema.GetFieldCount()))):
        raise FixtureError('point source ID/attribute schema differs')
    features=[]
    for feature in layer:
        geometry=feature.GetGeometryRef()
        if geometry is None or geometry.GetCoordinateDimension()!=2 or not geometry.IsValid():raise FixtureError('point geometry invalid or non-XY')
        kind=geometry.GetGeometryName()
        if kind=='POINT':shape={'type':'Point','coordinates':[geometry.GetX(),geometry.GetY()]}
        elif kind=='LINESTRING':shape={'type':'LineString','coordinates':[list(point[:2]) for point in geometry.GetPoints()]}
        elif kind=='MULTILINESTRING':shape={'type':'MultiLineString','coordinates':[
            [list(point[:2]) for point in geometry.GetGeometryRef(i).GetPoints()] for i in range(geometry.GetGeometryCount())]}
        else:raise FixtureError('point source/result geometry type differs')
        features.append({'properties':{name:feature.GetField(name) for name in fields},'geometry':shape})
    _validate_point_features(features,expected)
    result={**digest(path),'feature_count':len(features),'crs':'EPSG:3857','layer_name':layer.GetName(),
            'geometry_column':schema.GetGeomFieldDefn(0).GetName(),'fid_column':layer.GetFIDColumn(),'fields':fields}
    dataset=None
    return result


def gap_source_features(coverage=False):
    if coverage:
        shapes = [[rectangle(100,0,106,4)], [rectangle(100,0,104,4)],
                  [rectangle(120,0,126,6), rectangle(121,1,122,2)], [rectangle(140,0,144,4)]]
        geometries = [{'type':'Polygon','coordinates':rings} for rings in shapes]
        geometries += [{'type':'MultiPolygon','coordinates':[[rectangle(*bounds)] for bounds in
                        ((160,0,161,1),(161,1,162,2),(163,0,164,1))]}]
        geometries += [{'type':'Polygon','coordinates':[rectangle(*bounds)]} for bounds in
                       ((180,0,181,2**-40),(190,0,191,1))]
    else:
        shapes = [[rectangle(*bounds)] for bounds in
                  ((100,0,102,4),(103,0,105,4),(100,0,102,4),(99,-1,100,5),(123,3,124,4))]
        shapes += [[rectangle(140,0,144,4),rectangle(141,1,143,3)],[rectangle(190,0,191,1)]]
        geometries = [{'type':'Polygon','coordinates':rings} for rings in shapes]
    return [{'geometry':geometry,'properties':{'source_name':f'{"coverage" if coverage else "input"}-{index}'}}
            for index,geometry in enumerate(geometries)]


def gap_expected_features():
    # Independent hand-specified uncovered regions, not production difference.
    shapes = [([rectangle(102,0,103,4)],4), ([rectangle(105,0,106,4)],4),
              ([rectangle(120,0,126,6),rectangle(121,1,122,2),rectangle(123,3,124,4)],34),
              ([rectangle(141,1,143,3)],4), ([rectangle(160,0,161,1)],1),
              ([rectangle(161,1,162,2)],1), ([rectangle(163,0,164,1)],1),
              ([rectangle(180,0,181,2**-40)],2**-40)]
    return [{'properties':{'gap_area':area},'geometry':{'type':'Polygon','coordinates':rings}}
            for rings,area in shapes]


def validate_gap_features(features, source=None):
    expected = gap_expected_features() if source is None else gap_source_features(coverage=source)
    if len(features)!=len(expected): raise FixtureError('gap source/result count differs')
    for actual,wanted in zip(features,expected):
        geometry,reference=actual['geometry'],wanted['geometry']
        if source is None and (type(actual['properties'].get('gap_area')) not in (int,float)
            or not math.isfinite(actual['properties']['gap_area']) or actual['properties']['gap_area']<=0):
            raise FixtureError('gap area must be a finite positive number')
        if actual['properties']!=wanted['properties'] or geometry.get('type')!=reference['type']:
            raise FixtureError('gap attributes, exact area, source order or geometry type differ')
        parts=geometry['coordinates'] if geometry['type']=='MultiPolygon' else [geometry['coordinates']]
        wanted_parts=reference['coordinates'] if reference['type']=='MultiPolygon' else [reference['coordinates']]
        def rings(part):
            if not part: raise FixtureError('gap polygon requires an exterior ring')
            return canonical(part[0],True),tuple(sorted(canonical(ring,True) for ring in part[1:]))
        if (len(parts)!=len(wanted_parts) or sorted(rings(part) for part in parts)
                != sorted(rings(part) for part in wanted_parts)):
            raise FixtureError('gap components, holes or exact coordinates differ')


def seed_gap(path, coverage=False):
    from osgeo import ogr,osr
    crs=osr.SpatialReference();crs.ImportFromEPSG(3857)
    dataset=ogr.GetDriverByName('GPKG').CreateDataSource(str(path))
    layer=dataset.CreateLayer(path.stem,crs,ogr.wkbUnknown)
    layer.CreateField(ogr.FieldDefn('source_name',ogr.OFTString))
    for row in gap_source_features(coverage):
        feature=ogr.Feature(layer.GetLayerDefn());feature.SetField('source_name',row['properties']['source_name'])
        feature.SetGeometry(ogr.CreateGeometryFromJson(json.dumps(row['geometry'])))
        if layer.CreateFeature(feature)!=0:raise FixtureError('cannot seed gap source')
    dataset=None


def verify_gap(path, source=None):
    from osgeo import ogr,osr
    dataset=ogr.Open(str(path),0)
    if dataset is None or dataset.GetLayerCount()!=1:raise FixtureError('expected one gap GeoPackage layer')
    layer=dataset.GetLayer(0);crs=osr.SpatialReference();crs.ImportFromEPSG(3857)
    if not layer.GetSpatialRef() or not layer.GetSpatialRef().IsSame(crs):raise FixtureError('gap CRS differs')
    fields=['gap_area'] if source is None else ['source_name'];schema=layer.GetLayerDefn()
    if [schema.GetFieldDefn(i).GetName() for i in range(schema.GetFieldCount())]!=fields:
        raise FixtureError('gap field schema differs')
    if source is None and schema.GetFieldDefn(0).GetType()!=ogr.OFTReal:raise FixtureError('gap area must be Float64')
    features=[]
    for feature in layer:
        geometry=feature.GetGeometryRef()
        if geometry is None or geometry.GetCoordinateDimension()!=2 or not geometry.IsValid():raise FixtureError('gap geometry invalid or non-XY')
        properties={name:feature.GetField(name) for name in fields}
        if source is None and geometry.GetArea()!=properties['gap_area']:raise FixtureError('gap physical area differs')
        features.append({'properties':properties,'geometry':ogr_polygon_geometry(geometry)})
    validate_gap_features(features,source)
    result={**digest(path),'feature_count':len(features),'crs':'EPSG:3857','layer_name':layer.GetName(),
            'geometry_column':schema.GetGeomFieldDefn(0).GetName(),'fid_column':layer.GetFIDColumn(),'fields':fields}
    dataset=None
    return result


def overlap_source_features():
    polygons = [[rectangle(*bounds)] for bounds in ((0,0,4,4),(2,0,6,4),(1,1,2,2),(0,0,4,4),(6,0,7,4),(7,4,8,5))]
    polygons += [[rectangle(20,0,24,4), rectangle(21,1,23,3)], [rectangle(21,1,23,3)]]
    geometries = [{'type':'Polygon','coordinates':value} for value in polygons]
    geometries += [{'type':'MultiPolygon','coordinates':[[rectangle(*bounds)] for bounds in group]}
        for group in (((40,0,42,2),(44,0,45,1),(48,0,50,2)), ((41,0,43,2),(45,0,46,1),(49,0,51,2)))]
    geometries += [{'type':'Polygon','coordinates':[rectangle(60,0,61,2**-40)]} for _ in range(2)]
    identifiers = ['甲','乙','丙','丁','共边','共点','孔洞','孔内','多甲','多乙','微甲','微乙']
    return [{'geometry':geometry,'properties':{'parcel_id':identifier,'note':f'source-{index}'}}
            for index,(identifier,geometry) in enumerate(zip(identifiers,geometries))]


def overlap_expected_features():
    # Explicit fixture intersections, never generated with the product algorithm.
    pairs = [('甲','乙',8,[(2,0,4,4)]),('甲','丙',1,[(1,1,2,2)]),('甲','丁',16,[(0,0,4,4)]),
             ('乙','丁',8,[(2,0,4,4)]),('丙','丁',1,[(1,1,2,2)]),
             ('多甲','多乙',4,[(41,0,42,2),(49,0,50,2)]),('微甲','微乙',2**-40,[(60,0,61,2**-40)])]
    return [{'properties':{'source_id_a':a,'source_id_b':b,'overlap_area':area},
             'geometry':{'type':'MultiPolygon','coordinates':[[rectangle(*bounds)] for bounds in parts]}}
            for a,b,area,parts in pairs]


def validate_overlap_features(features, source=False):
    expected = overlap_source_features() if source else overlap_expected_features()
    if len(features) != len(expected): raise FixtureError('overlap pair/source count differs')
    for actual, wanted in zip(features, expected):
        geometry, reference = actual['geometry'], wanted['geometry']
        if not source and (type(actual['properties'].get('overlap_area')) not in (int,float)
            or not math.isfinite(actual['properties']['overlap_area']) or actual['properties']['overlap_area']<=0):
            raise FixtureError('overlap area must be a finite positive number')
        if actual['properties'] != wanted['properties'] or geometry.get('type') != reference['type']:
            raise FixtureError('overlap source IDs, attributes, order, area or type differ')
        parts = geometry['coordinates'] if geometry['type']=='MultiPolygon' else [geometry['coordinates']]
        wanted_parts = reference['coordinates'] if reference['type']=='MultiPolygon' else [reference['coordinates']]
        if (len(parts) != len(wanted_parts) or sorted(tuple(canonical(ring, True) for ring in part) for part in parts)
                != sorted(tuple(canonical(ring, True) for ring in part) for part in wanted_parts)):
            raise FixtureError('overlap polygon parts, holes or exact coordinates differ')


def seed_overlap(path):
    from osgeo import ogr, osr
    crs=osr.SpatialReference(); crs.ImportFromEPSG(3857)
    dataset=ogr.GetDriverByName('GPKG').CreateDataSource(str(path))
    layer=dataset.CreateLayer(path.stem,crs,ogr.wkbUnknown)
    for name in ('parcel_id','note'): layer.CreateField(ogr.FieldDefn(name,ogr.OFTString))
    for row in overlap_source_features():
        feature=ogr.Feature(layer.GetLayerDefn())
        for name,value in row['properties'].items(): feature.SetField(name,value)
        feature.SetGeometry(ogr.CreateGeometryFromJson(json.dumps(row['geometry'])))
        if layer.CreateFeature(feature)!=0: raise FixtureError('cannot seed overlap source')
    dataset=None


def ogr_polygon_geometry(geometry):
    # ExportToJson's default decimal precision can round tiny coordinates.
    # Read native doubles directly so the independent oracle loses no bits.
    kind=geometry.GetGeometryName()
    if kind=='MULTIPOLYGON':
        return {'type':'MultiPolygon','coordinates':[ogr_polygon_geometry(geometry.GetGeometryRef(i))['coordinates']
                for i in range(geometry.GetGeometryCount())]}
    if kind!='POLYGON': raise FixtureError('expected polygonal overlap geometry')
    return {'type':'Polygon','coordinates':[[list(point[:2]) for point in geometry.GetGeometryRef(i).GetPoints()]
            for i in range(geometry.GetGeometryCount())]}


def verify_overlap(path, source=False):
    from osgeo import ogr, osr
    dataset=ogr.Open(str(path),0)
    if dataset is None or dataset.GetLayerCount()!=1: raise FixtureError('expected one overlap GeoPackage layer')
    layer=dataset.GetLayer(0); crs=osr.SpatialReference(); crs.ImportFromEPSG(3857)
    if not layer.GetSpatialRef() or not layer.GetSpatialRef().IsSame(crs): raise FixtureError('overlap CRS differs')
    fields=['parcel_id','note'] if source else ['source_id_a','source_id_b','overlap_area']
    schema=layer.GetLayerDefn()
    if [schema.GetFieldDefn(i).GetName() for i in range(schema.GetFieldCount())]!=fields:
        raise FixtureError('overlap field schema differs')
    if not source and schema.GetFieldDefn(2).GetType()!=ogr.OFTReal: raise FixtureError('overlap area must be Float64')
    features=[]
    for feature in layer:
        geometry=feature.GetGeometryRef()
        if geometry is None or geometry.GetCoordinateDimension()!=2 or not geometry.IsValid(): raise FixtureError('overlap geometry invalid or non-XY')
        props={name:feature.GetField(name) for name in fields}
        if not source and geometry.GetArea()!=props['overlap_area']: raise FixtureError('overlap physical area differs')
        features.append({'properties':props,'geometry':ogr_polygon_geometry(geometry)})
    validate_overlap_features(features,source)
    result={**digest(path),'feature_count':len(features),'crs':'EPSG:3857','layer_name':layer.GetName(),
            'geometry_column':schema.GetGeomFieldDefn(0).GetName(),'fid_column':layer.GetFIDColumn(),'fields':sorted(fields)}
    dataset=None
    return result


class FixtureError(RuntimeError):
    pass


if sys.argv[1:2] != ['--worker']:
    sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
    from scripts.utils.online_nfs_fixture import NFSExport, FixtureError


def command(args, **kwargs):
    return subprocess.run(args, check=True, capture_output=True, text=True, timeout=120, **kwargs).stdout.strip()


def digest(path):
    if path.is_symlink() or not path.is_file() or path.stat().st_size <= 0:
        raise FixtureError('expected a nonempty ordinary file')
    return {'sha256': hashlib.sha256(path.read_bytes()).hexdigest(), 'size_bytes': path.stat().st_size}


def linework():
    coordinates = [
        [[120,30],[124,30],[124,34],[120,34],[120,30]],
        [[119,32],[125,32]], [[122,29],[122,35]], [[120,30],[124,30]]
    ]
    return {'type': 'FeatureCollection', 'crs': {'type': 'name', 'properties': {'name': 'EPSG:4326'}},
        'features': [{'type': 'Feature', 'properties': {'source_name': f'line-{i}'},
                      'geometry': {'type': 'LineString', 'coordinates': points}} for i, points in enumerate(coordinates)]}


def invalid_polygons():
    return {'type': 'FeatureCollection', 'crs': {'type': 'name', 'properties': {'name': 'EPSG:4326'}},
        'features': [{'type': 'Feature', 'properties': {'source_name': f'bow-{i}', 'grade': 7+i},
            'geometry': {'type': 'Polygon', 'coordinates': [[[x,30],[x+2,32],[x,32],[x+2,30],[x,30]]]}}
            for i, x in enumerate((120,124))]}


def seed_snap(path, coordinates):
    from osgeo import ogr, osr
    crs = osr.SpatialReference(); crs.ImportFromEPSG(3857)
    dataset = ogr.GetDriverByName('GPKG').CreateDataSource(str(path))
    layer = dataset.CreateLayer(path.stem, crs, ogr.wkbLineString)
    layer.CreateField(ogr.FieldDefn('source_name', ogr.OFTString))
    layer.CreateField(ogr.FieldDefn('grade', ogr.OFTInteger64))
    for index, points in enumerate(coordinates):
        feature = ogr.Feature(layer.GetLayerDefn())
        feature.SetField('source_name', f'snap-{index}'); feature.SetField('grade', 10+index)
        geometry = ogr.Geometry(ogr.wkbLineString)
        for x, y in points: geometry.AddPoint_2D(x, y)
        feature.SetGeometry(geometry)
        if layer.CreateFeature(feature) != 0: raise FixtureError('cannot seed projected snap source')
    dataset = None


def validate_snap_features(features, expected=SNAP_EXPECTED):
    if len(features) != len(expected): raise FixtureError('snap changed source row count')
    for index, (feature, points) in enumerate(zip(features, expected)):
        geometry = feature['geometry']
        if (feature['properties'] != {'source_name': f'snap-{index}', 'grade': 10+index}
            or geometry.get('type') != 'LineString'
            or geometry.get('coordinates') != [list(point) for point in points]):
            raise FixtureError('snap changed original-position coordinates, order or attributes')


def verify_snap(path, expected):
    from osgeo import ogr, osr
    dataset = ogr.Open(str(path), 0)
    if dataset is None or dataset.GetLayerCount() != 1: raise FixtureError('expected one projected GeoPackage layer')
    layer = dataset.GetLayer(0); crs = osr.SpatialReference(); crs.ImportFromEPSG(3857)
    crs.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER)
    if not layer.GetSpatialRef() or not layer.GetSpatialRef().IsSame(crs): raise FixtureError('snap lost projected CRS')
    schema = layer.GetLayerDefn()
    if ([schema.GetFieldDefn(i).GetName() for i in range(schema.GetFieldCount())] != ['source_name','grade']
        or schema.GetFieldDefn(1).GetType() != ogr.OFTInteger64):
        raise FixtureError('snap changed the attribute schema')
    features = []
    for feature in layer:
        geometry = feature.GetGeometryRef()
        if geometry is None or geometry.GetCoordinateDimension() != 2 or not geometry.IsValid():
            raise FixtureError('snap persisted invalid or non-XY geometry')
        features.append({'properties': {key: feature.GetField(key) for key in ('source_name','grade')},
                         'geometry': json.loads(geometry.ExportToJson())})
    validate_snap_features(features, expected)
    result = {**digest(path), 'feature_count': len(features), 'crs': 'EPSG:3857',
              'layer_name': layer.GetName(), 'geometry_column': schema.GetGeomFieldDefn(0).GetName(),
              'fields': ['grade','source_name']}
    dataset = None
    return result


def validate_repaired(features):
    if len(features) != 2:
        raise FixtureError('repair must retain both source rows')
    for i, (feature, x) in enumerate(zip(features, (120,124))):
        props = feature['properties']; geometry = feature['geometry']
        expected_fields = {'source_name', 'grade', 'geometry_valid', 'geometry_reason', 'after_valid', 'after_reason'}
        if (set(props) != expected_fields or props['source_name'] != f'bow-{i}' or props['grade'] != 7+i
            or props['geometry_valid'] is not False or not props['geometry_reason'].startswith('Self-intersection')
            or props['after_valid'] is not True or props['after_reason'] != 'Valid Geometry'):
            raise FixtureError('repair lost source attributes or before/after validity')
        expected = {canonical([[x,30],[x+2,30],[x+1,31],[x,30]], True),
                    canonical([[x,32],[x+2,32],[x+1,31],[x,32]], True)}
        if geometry['type'] != 'MultiPolygon' or len(geometry['coordinates']) != 2 or any(len(part) != 1 for part in geometry['coordinates']):
            raise FixtureError('repair must retain two triangles per source feature')
        actual = [canonical(part[0], True) for part in geometry['coordinates']]
        if len(set(actual)) != 2 or set(actual) != expected:
            raise FixtureError('repair differs from independent triangle coordinates')


def canonical(points, ring=False):
    if any(len(point) != 2 or any(type(value) not in (int, float) or not math.isfinite(value) for value in point) for point in points):
        raise FixtureError('expected finite XY coordinates')
    vertices = [tuple(point) for point in points]
    if ring:
        if len(vertices) < 4 or vertices[0] != vertices[-1]:
            raise FixtureError('polygon must be closed')
        vertices = vertices[:-1]
        rotations = [tuple(sequence[i:] + sequence[:i]) for sequence in (vertices, list(reversed(vertices))) for i in range(len(vertices))]
        return min(rotations)
    return min(tuple(vertices), tuple(reversed(vertices)))


def expected_geometries(name):
    if name == 'faces.geojson':
        return {canonical([[x,y],[x+2,y],[x+2,y+2],[x,y+2],[x,y]], True)
                for x in (120,122) for y in (30,32)}
    if name == 'diagnostics.geojson':
        return {canonical(points) for points in (
            [[119,32],[120,32]], [[124,32],[125,32]], [[122,29],[122,30]], [[122,34],[122,35]])}
    raise FixtureError('unexpected target name')


def validate_features(name, features):
    if name in ('line-chains.gpkg','line-chain-members.gpkg'):
        return validate_line_chain_features(features,'members' if name=='line-chain-members.gpkg' else 'result')
    if name == 'line-components.gpkg':
        return validate_line_component_features(features)
    if name == 'split-lines.gpkg':
        return validate_split_line_features(features)
    if name == 'line-self-intersections.gpkg':
        return validate_line_self_intersection_features(features)
    if name == 'line-intersections.gpkg':
        return validate_line_intersection_features(features)
    if name == 'line-self-overlaps.gpkg':
        return validate_line_self_overlap_features(features)
    if name == 'line-overlaps.gpkg':
        return validate_line_overlap_features(features)
    if name == 'dangling.gpkg':
        return validate_dangling_features(features)
    if name == 'gaps.gpkg':
        return validate_gap_features(features)
    if name == 'overlaps.gpkg':
        return validate_overlap_features(features)
    if name == 'snapped.gpkg':
        return validate_snap_features(features)
    if name == 'repaired.geojson':
        return validate_repaired(features)
    actual = []
    for feature in features:
        geometry = feature['geometry']; coordinates = geometry['coordinates']
        if name == 'faces.geojson':
            if geometry['type'] != 'Polygon' or len(coordinates) != 1 or feature['properties'] != {}:
                raise FixtureError('faces must be attribute-free single-ring polygons')
            actual.append(canonical(coordinates[0], True))
        else:
            if geometry['type'] != 'LineString' or feature['properties'] != {'issue_type': 'dangle'}:
                raise FixtureError('diagnostics must contain only dangle classification')
            actual.append(canonical(coordinates))
    if len(actual) != 4 or len(set(actual)) != 4 or set(actual) != expected_geometries(name):
        raise FixtureError('output differs from the independent coordinate oracle')


def worker(action, root, case=None):
    case=topology_case() if case is None else topology_case({"ADDP_ONLINE_TOPOLOGY_CASE":case})
    if case==EXTENSION_CASE:return extension_worker(action,root)
    if case==PROXIMITY_CASE:return proximity_worker(action,root)
    if case==DIAGNOSTIC_CASE:return diagnostic_worker(action,root)
    if case==BUFFER_CASE:return buffer_worker(action,root)
    if case in (NORMALIZE_CASE,COMPOSITION_CASE):return normalize_worker(action,root,case)
    if action == 'seed':
        if any((root / name).exists() for name in SOURCES) or list((root / 'outputs').iterdir()):
            raise FixtureError('refusing existing data')
        (root / 'source.geojson').write_text(json.dumps(linework(), sort_keys=True))
        (root / 'invalid.geojson').write_text(json.dumps(invalid_polygons(), sort_keys=True))
        seed_snap(root / 'snap-input.gpkg', SNAP_INPUT)
        seed_snap(root / 'snap-reference.gpkg', SNAP_REFERENCE)
        seed_overlap(root / 'overlap-input.gpkg')
        seed_gap(root / 'gap-input.gpkg')
        seed_gap(root / 'gap-coverage.gpkg', coverage=True)
        seed_dangling(root / 'dangling-input.gpkg')
        seed_line_overlap(root / 'line-overlap-input.gpkg')
        _seed_linear(root / 'line-self-overlap-input.gpkg', line_self_overlap_source_features())
        _seed_linear(root / 'line-intersection-input.gpkg', line_intersection_source_features())
        _seed_linear(root / 'line-self-intersection-input.gpkg', line_self_intersection_source_features())
        seed_split_line(root / 'split-line-input.gpkg')
        seed_line_component(root / 'line-component-input.gpkg')
        seed_line_chain(root / 'line-chain-input.gpkg')
        return {'sources': {name: digest(root / name) for name in SOURCES}}
    if action != 'verify':
        raise FixtureError('unknown worker action')
    from osgeo import ogr, osr
    if sorted(path.name for path in root.iterdir()) != sorted(['.addp-online-owner', 'outputs', *SOURCES]) or sorted(path.name for path in (root / 'outputs').iterdir()) != sorted(TARGETS):
        raise FixtureError('unexpected source, output, sidecar or temporary file')
    crs = osr.SpatialReference()
    if crs.ImportFromEPSG(4326) != 0:
        raise FixtureError('cannot load the fixture CRS')
    crs.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER)
    results = {}
    verify_snap(root / 'snap-input.gpkg', SNAP_INPUT)
    verify_snap(root / 'snap-reference.gpkg', SNAP_REFERENCE)
    verify_overlap(root / 'overlap-input.gpkg', source=True)
    verify_gap(root / 'gap-input.gpkg', source=False)
    verify_gap(root / 'gap-coverage.gpkg', source=True)
    verify_dangling(root / 'dangling-input.gpkg', source=True)
    verify_line_overlap(root / 'line-overlap-input.gpkg', source=True)
    verify_line_self_overlap(root / 'line-self-overlap-input.gpkg', source=True)
    verify_line_intersection(root / 'line-intersection-input.gpkg', source=True)
    verify_line_self_intersection(root / 'line-self-intersection-input.gpkg', source=True)
    verify_split_line(root / 'split-line-input.gpkg', source=True)
    verify_line_component(root / 'line-component-input.gpkg', source=True)
    verify_line_chain(root / 'line-chain-input.gpkg',kind='source')
    for name in TARGETS:
        path = root / 'outputs' / name
        if name in ('line-chains.gpkg','line-chain-members.gpkg'):
            results[name]=verify_line_chain(path,'members' if name=='line-chain-members.gpkg' else 'result')
            continue
        if name == 'line-components.gpkg':
            results[name] = verify_line_component(path)
            continue
        if name == 'split-lines.gpkg':
            results[name] = verify_split_line(path)
            continue
        if name == 'line-self-intersections.gpkg':
            results[name] = verify_line_self_intersection(path)
            continue
        if name == 'line-intersections.gpkg':
            results[name] = verify_line_intersection(path)
            continue
        if name == 'line-self-overlaps.gpkg':
            results[name] = verify_line_self_overlap(path)
            continue
        if name == 'line-overlaps.gpkg':
            results[name] = verify_line_overlap(path)
            continue
        if name == 'dangling.gpkg':
            results[name] = verify_dangling(path)
            continue
        if name == 'gaps.gpkg':
            results[name] = verify_gap(path)
            continue
        if name == 'overlaps.gpkg':
            results[name] = verify_overlap(path)
            continue
        if name == 'snapped.gpkg':
            results[name] = verify_snap(path, SNAP_EXPECTED)
            continue
        features = json.loads(path.read_text())['features']
        validate_features(name, features)
        dataset = ogr.Open(str(path), 0)
        if dataset is None or dataset.GetLayerCount() != 1:
            raise FixtureError('expected exactly one vector layer')
        layer = dataset.GetLayer(0)
        if not layer.GetSpatialRef() or not layer.GetSpatialRef().IsSame(crs):
            raise FixtureError('source CRS was not preserved')
        if layer.GetFeatureCount() != len(features) or any(not item.GetGeometryRef().IsValid() for item in layer):
            raise FixtureError('persisted geometries must all be valid')
        results[name] = {**digest(path), 'feature_count': len(features), 'crs': 'EPSG:4326',
                         'fields': sorted(features[0]['properties'])}
        dataset = None
    return {'sources': {name: digest(root / name) for name in SOURCES}, 'targets': results, 'exact_inventory_verified': True}


def runtime_worker(action, root):
    info = json.loads(command(['docker', 'inspect', RUNTIME]))[0]
    labels = info['Config']['Labels']
    if labels.get('com.addp.online-runtime') != SUITE or labels.get('com.addp.online-run-id') != os.environ['ADDP_ONLINE_TEST_RUN_ID'] or info['Config']['Cmd'] != ['sh', 'container_entrypoint.sh']:
        raise FixtureError('foreign Runtime or non-product entry')
    return json.loads(command(['docker', 'exec', '-i', RUNTIME, 'python', '-', '--worker', action, str(root), topology_case()], input=Path(__file__).read_text()))


def archive_physical_evidence(root, directory, result, case=None):
    """Retain only this synthetic fixture's verified bytes for offline audit."""
    case=topology_case() if case is None else topology_case({'ADDP_ONLINE_TOPOLOGY_CASE':case})
    sources,targets=(EXTENSION_SOURCES,EXTENSION_TARGETS) if case==EXTENSION_CASE else (PROXIMITY_SOURCES,(PROXIMITY_TARGET,)) if case==PROXIMITY_CASE else (DIAGNOSTIC_SOURCES,()) if case==DIAGNOSTIC_CASE else (BUFFER_SOURCES,BUFFER_TARGETS) if case==BUFFER_CASE else ((network_profile(case)['source'],),network_profile(case)['targets']) if case in (NORMALIZE_CASE,COMPOSITION_CASE) else (SOURCES,TARGETS)
    archive = directory / 'topology-physical'
    if archive.is_symlink() or (archive/'outputs').is_symlink():
        raise FixtureError('physical archive must not follow symlinks')
    (archive/'outputs').mkdir(parents=True, exist_ok=True)
    for relative, expected in [*((name, result['sources'][name]) for name in sources),
                               *((f'outputs/{name}', result['targets'][name]) for name in targets)]:
        source, target = root/relative, archive/relative
        actual = digest(source)
        if any(actual[key] != expected[key] for key in ('sha256','size_bytes')):
            raise FixtureError('physical source changed before evidence capture')
        if target.exists() or target.is_symlink():
            if digest(target) != actual: raise FixtureError('refusing to replace a different physical evidence snapshot')
        else:
            target.write_bytes(source.read_bytes())
        if digest(target) != actual: raise FixtureError('physical evidence capture differs')


def main(action):
    case=topology_case()
    export = NFSExport('topology', command)
    secret = export.guard(os.environ)
    run_id = os.environ['ADDP_ONLINE_TEST_RUN_ID']
    if action in {'start','stop'}:
        return export.lifecycle(action, secret, run_id)
    root = export.owned_root(secret, run_id)
    result = runtime_worker(action, root)
    baseline = secret / 'topology-source.json'
    if action == 'seed':
        baseline.write_text(json.dumps(result))
    elif result['sources'] != json.loads(baseline.read_text())['sources']:
        raise FixtureError('source file changed')
    else:
        archive_physical_evidence(root, Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR']), result)
    return result


if __name__ == '__main__':
    try:
        result = worker(sys.argv[2], Path(sys.argv[3]), sys.argv[4] if len(sys.argv)>4 else 'full') if sys.argv[1:2] == ['--worker'] else main(sys.argv[1])
        print(json.dumps(result, sort_keys=True))
    except Exception as error:
        print(f'Topology fixture failed: {type(error).__name__}: {error}', file=sys.stderr)
        raise SystemExit(1)
