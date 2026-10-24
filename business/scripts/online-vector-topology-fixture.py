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
TARGETS = ('faces.geojson', 'diagnostics.geojson', 'repaired.geojson', 'snapped.gpkg', 'overlaps.gpkg', 'gaps.gpkg', 'dangling.gpkg', 'line-overlaps.gpkg', 'line-self-overlaps.gpkg', 'line-intersections.gpkg', 'line-self-intersections.gpkg', 'split-lines.gpkg', 'line-components.gpkg')
SOURCES = ('source.geojson', 'invalid.geojson', 'snap-input.gpkg', 'snap-reference.gpkg', 'overlap-input.gpkg', 'gap-input.gpkg', 'gap-coverage.gpkg', 'dangling-input.gpkg', 'line-overlap-input.gpkg', 'line-self-overlap-input.gpkg', 'line-intersection-input.gpkg', 'line-self-intersection-input.gpkg', 'split-line-input.gpkg', 'line-component-input.gpkg')
PORTS = ('faces', 'diagnostics', 'repaired', 'snapped', 'overlaps', 'gaps', 'dangling', 'line_overlaps', 'line_self_overlaps', 'line_intersections', 'line_self_intersections', 'split_lines', 'line_components')
# Explicit coordinates are independent of the production matching algorithm.
SNAP_INPUT = (((0,0),(10,0)), ((0,10),(10,10)), ((0,20),(10,20)), ((5,30),(5,35)), ((50,40),(52,40)))
SNAP_REFERENCE = (((.5,0),(1,0)), ((10,0),(11,0)), ((.75,10),(10,10)), ((-.5,20),(.5,20)), ((0,30),(10,30)), ((51,40),(51,45)))
SNAP_EXPECTED = (((.5,0),(10,0)), ((0,10),(10,10)), ((-.5,20),(10,20)), ((5,30),(5,35)), ((50,40),(52,40)))


def rectangle(x0, y0, x1, y1):
    return [[x0,y0],[x1,y0],[x1,y1],[x0,y1],[x0,y0]]


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
            ('原重', [[70,200],[70,200],[72,200],[72,200]])]
    return [{'properties': {'route_key': identity, 'note': None if index == 1 else f'split-original-{index}', 'grade': 100+index},
             'geometry': {'type': 'MultiLineString' if identity == '多部' else 'LineString', 'coordinates': points}}
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
            (10,1,1,[[70,200],[70,200],[72,200],[72,200]])]
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


def _seed_attributed_line_source(path, features):
    from osgeo import ogr, osr
    crs = osr.SpatialReference(); crs.ImportFromEPSG(3857)
    dataset = ogr.GetDriverByName('GPKG').CreateDataSource(str(path))
    layer = dataset.CreateLayer(path.stem, crs, ogr.wkbUnknown)
    for name, kind in [('route_key',ogr.OFTString),('note',ogr.OFTString),('grade',ogr.OFTInteger64)]:
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
        any(schema.GetFieldDefn(i).GetType() != (ogr.OFTInteger64 if name in ('grade','part_no','segment_no','component_id') else ogr.OFTString) for i,name in enumerate(fields))):
        raise FixtureError('attributed line attribute/ordinal field types differ')
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
        features.append({'properties':{name:feature.GetField(name) for name in fields},'geometry':shape})
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


def worker(action, root):
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
    for name in TARGETS:
        path = root / 'outputs' / name
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
    return json.loads(command(['docker', 'exec', '-i', RUNTIME, 'python', '-', '--worker', action, str(root)], input=Path(__file__).read_text()))


def archive_physical_evidence(root, directory, result):
    """Retain only this synthetic fixture's verified bytes for offline audit."""
    archive = directory / 'topology-physical'
    if archive.is_symlink() or (archive/'outputs').is_symlink():
        raise FixtureError('physical archive must not follow symlinks')
    (archive/'outputs').mkdir(parents=True, exist_ok=True)
    for relative, expected in [*((name, result['sources'][name]) for name in SOURCES),
                               *((f'outputs/{name}', result['targets'][name]) for name in TARGETS)]:
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
        result = worker(sys.argv[2], Path(sys.argv[3])) if sys.argv[1:2] == ['--worker'] else main(sys.argv[1])
        print(json.dumps(result, sort_keys=True))
    except Exception as error:
        print(f'Topology fixture failed: {type(error).__name__}: {error}', file=sys.stderr)
        raise SystemExit(1)
