"""Native planar linework and existing multiport persistence contracts."""

import json
import time

import geopandas as gpd
from geopandas.testing import assert_geodataframe_equal
import numpy as np
import pandas as pd
import pytest
import shapely
from shapely.geometry import LineString, MultiLineString, Point, Polygon, box

from operators import get_operator, list_operators
from workflow_engine import execute_workflow


def overlay_inputs(a=None, b=None):
    a = [box(0, 0, 2, 2)] if a is None else a
    b = [box(1, 1, 3, 3)] if b is None else b
    left = gpd.GeoDataFrame({'a_key': pd.Series(range(1, len(a)+1), dtype='Int64')},
                            geometry=a, crs='EPSG:3857').rename_geometry('source_shape')
    right = gpd.GeoDataFrame({'b_key': pd.Series(range(11, 11+len(b)), dtype='string')},
                             geometry=b, crs='EPSG:3857').rename_geometry('reference_shape')
    return left, right


def exact_overlay(a, b):
    return get_operator('intersection')(a, b, 'a_key', 'b_key')


@pytest.mark.parametrize('a,b,expected', [
    (Point(1,0), Point(1,0), Point(1,0)),
    (Point(1,0), LineString([(0,0),(2,0)]), Point(1,0)),
    (Point(1,0), box(0,-1,2,1), Point(1,0)),
    (LineString([(0,0),(2,0)]), Point(1,0), Point(1,0)),
    (LineString([(0,0),(2,0)]), LineString([(1,-1),(1,1)]), Point(1,0)),
    (LineString([(-1,0),(3,0)]), box(0,-1,2,1), LineString([(0,0),(2,0)])),
    (box(0,-1,2,1), Point(1,0), Point(1,0)),
    (box(0,-1,2,1), LineString([(-1,0),(3,0)]), LineString([(0,0),(2,0)])),
    (box(0,0,2,2), box(1,1,3,3), box(1,1,2,2)),
    (box(0,0,1,1), box(1,0,2,1), LineString([(1,0),(1,1)])),
    (box(0,0,1,1), box(1,1,2,2), Point(1,1)),
])
def test_intersection_overlay_keeps_every_intersection_dimension(a,b,expected):
    left,right=overlay_inputs([a],[b]);result=exact_overlay(left,right)
    assert len(result)==1 and result.geometry.iloc[0].equals(expected)
    assert result.source_id_a.tolist()==[1] and result.source_id_b.tolist()==['11']
    assert result.geometry.name=='source_shape' and result.crs==left.crs


def test_intersection_overlay_geometry_collection_and_multipart_are_one_pair_row():
    a=shapely.MultiPolygon([box(0,0,2,2),box(4,0,5,1),box(7,0,8,1)])
    b=shapely.MultiPolygon([box(1,1,3,3),box(5,0,6,1),box(8,1,9,2)])
    expected=shapely.GeometryCollection([box(1,1,2,2),LineString([(5,0),(5,1)]),Point(8,1)])
    left,right=overlay_inputs([a],[b]);result=exact_overlay(left,right)
    assert len(result)==1 and result.geometry.iloc[0].geom_type=='GeometryCollection'
    assert result.geometry.iloc[0].equals(expected)
    left,right=overlay_inputs([expected],[box(-1,-1,10,10)])
    assert exact_overlay(left,right).geometry.iloc[0].equals(expected)
    left,right=overlay_inputs([shapely.MultiPoint([(0,0),(1,0),(3,0)])],[box(-1,-1,2,1)])
    assert len(exact_overlay(left,right))==1
    assert exact_overlay(left,right).geometry.iloc[0].equals(shapely.MultiPoint([(0,0),(1,0)]))


def test_intersection_overlay_pairs_keep_original_types_attributes_and_source_order():
    left,right=overlay_inputs([box(0,0,4,4),box(0,0,4,4)], [Point(3,3),Point(1,1),Point(2,2)])
    left['a_key']=pd.Series([2**60+1,2**60+3],dtype='Int64')
    left['grade']=pd.Series([None,2**53+1],dtype='Int64')
    left['enabled']=pd.Series([None,False],dtype='boolean')
    left['geometry']=pd.Series(['普通业务',None],dtype='string')
    right['kind']=pd.Categorical(['c','a','b'])
    right['note']=pd.Series([None,'甲','乙'],dtype='string')
    right=right.rename_geometry('geometry')
    left.index=[9,9];right.index=[4,-2,4]
    originals=[f.copy(deep=True) for f in (left,right)]
    result=exact_overlay(left,right)
    assert list(result.columns)==[*left.columns,*[c for c in right.columns if c!=right.geometry.name],'source_id_a','source_id_b']
    assert result.source_id_a.tolist()==[2**60+1]*3+[2**60+3]*3
    assert result.source_id_b.tolist()==['11','12','13']*2
    assert [tuple(g.coords[0]) for g in result.geometry]==[(3,3),(1,1),(2,2)]*2
    for name in left.columns:
        if name!=left.geometry.name:
            pd.testing.assert_series_equal(result[name],left[name].iloc[[0,0,0,1,1,1]].reset_index(drop=True))
    for name in right.columns:
        if name!=right.geometry.name:
            pd.testing.assert_series_equal(result[name],right[name].iloc[[0,1,2,0,1,2]].reset_index(drop=True))
    assert result.source_id_a.dtype==left.a_key.dtype and result.source_id_b.dtype==right.b_key.dtype
    assert result.index.equals(pd.RangeIndex(6))
    for current,before in zip((left,right),originals):assert_geodataframe_equal(current,before)


@pytest.mark.parametrize('empty',['left','right','both','disjoint'])
def test_intersection_overlay_empty_preserves_full_schema_and_admission(empty):
    left,right=overlay_inputs()
    left['flag']=pd.Series([None],dtype='boolean');right['value']=pd.Series([None],dtype='Int64')
    if empty in ['left','both']:left=left.iloc[:0]
    if empty in ['right','both']:right=right.iloc[:0]
    if empty=='disjoint':right[right.geometry.name]=gpd.GeoSeries([Point(99,99)],crs=right.crs)
    result=exact_overlay(left,right)
    assert len(result)==0 and result.geometry.name==left.geometry.name and result.crs==left.crs
    for frame in [left,right]:
        for name in frame.columns:
            if name!=frame.geometry.name:assert result[name].dtype==frame[name].dtype
    assert result.source_id_a.dtype==left.a_key.dtype and result.source_id_b.dtype==right.b_key.dtype
    right=right.set_crs(None,allow_override=True)
    with pytest.raises(ValueError):exact_overlay(left,right)


def test_intersection_overlay_retains_binary_tiny_nonempty_results():
    step=2**-20
    left,right=overlay_inputs([box(0,0,step,step)],[box(0,0,1,1)])
    assert exact_overlay(left,right).geometry.iloc[0].area==2**-40
    left,right=overlay_inputs([Point(2**-40,0)],[LineString([(0,0),(1,0)])])
    assert tuple(exact_overlay(left,right).geometry.iloc[0].coords[0])==(2**-40,0)


def test_intersection_overlay_bbox_candidates_are_not_intersections_and_pairs_are_not_deduplicated():
    hole=Polygon([(0,0),(4,0),(4,4),(0,4),(0,0)],holes=[[(1,1),(2,1),(2,2),(1,2),(1,1)]])
    left,right=overlay_inputs([hole],[Point(1.5,1.5)])
    assert len(exact_overlay(left,right))==0
    left,right=overlay_inputs([box(0,0,1,1)]*2,[box(0,0,1,1)]*2)
    result=exact_overlay(left,right)
    assert list(zip(result.source_id_a,result.source_id_b))==[(1,'11'),(1,'12'),(2,'11'),(2,'12')]


@pytest.mark.parametrize('side',['a','b'])
def test_intersection_overlay_explicit_identifiers_cannot_be_duplicates_or_index_fallback(side):
    left,right=overlay_inputs([Point(0,0),Point(1,0)],[Point(0,0),Point(1,0)])
    frame=left if side=='a' else right;field='a_key' if side=='a' else 'b_key'
    frame[field]=[7,7]
    with pytest.raises(ValueError):exact_overlay(left,right)
    frame.index.name=field;frame.drop(columns=[field],inplace=True)
    with pytest.raises(ValueError):exact_overlay(left,right)


@pytest.mark.parametrize('bad',[Point(float('inf'),0),Polygon([(0,0),(2,2),(0,2),(2,0),(0,0)])])
def test_intersection_overlay_invalid_computed_geometry_fails_without_repair(monkeypatch,bad):
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime import WorkflowExecutionError
    left,right=overlay_inputs();monkeypatch.setattr(shapely,'intersection',lambda *a,**k:bad)
    with pytest.raises(WorkflowExecutionError) as caught:GeoPythonWorkflowRunner().execute({'tasks':[
        {'id':'overlay','operator':'intersection','params':{'input_gdf':left,'gdf_b':right,'id_field':'a_key','reference_id_field':'b_key'},'depends_on':[]}]})
    assert caught.value.error_code=='EXECUTION_FAILED' and caught.value.diagnostic is None


@pytest.mark.parametrize('side',['a','b'])
@pytest.mark.parametrize('problem',['null','empty','invalid','z','m','nonfinite','missing_id','null_id','empty_id','object_id','geometry_id','duplicate_columns','second_geometry'])
def test_intersection_overlay_rejects_input_without_repair(side,problem):
    from operators.geometric_operators import IntersectionInputError
    left,right=overlay_inputs();frame=left if side=='a' else right;field='a_key' if side=='a' else 'b_key'
    shapes={'null':None,'empty':Point(),'invalid':Polygon([(0,0),(2,2),(0,2),(2,0),(0,0)]),
            'z':shapely.from_wkt('POINT Z (0 0 1)'), 'm':shapely.from_wkt('POINT M (0 0 1)'),
            'nonfinite':shapely.from_wkt('LINESTRING (0 0,Infinity 1)')}
    if problem in shapes:frame[frame.geometry.name]=gpd.GeoSeries([shapes[problem]],crs=frame.crs)
    elif problem=='missing_id':frame.drop(columns=[field],inplace=True)
    elif problem=='null_id':frame[field]=pd.Series([None],dtype='string')
    elif problem=='empty_id':frame[field]=[' ']
    elif problem=='object_id':frame[field]=[{'private':1}]
    elif problem=='geometry_id':
        frame[field]=gpd.GeoSeries([Point(0,0)],crs=frame.crs)
    elif problem=='duplicate_columns':frame.columns=[frame.columns[0]]*len(frame.columns)
    elif problem=='second_geometry':frame['other_shape']=gpd.GeoSeries([Point(0,0)],crs=frame.crs)
    with pytest.raises(IntersectionInputError) as caught:exact_overlay(left,right)
    assert caught.value.code==('source' if side=='a' else 'reference')


@pytest.mark.parametrize('side',['a','b'])
@pytest.mark.parametrize('crs',[None,'EPSG:4326','EPSG:4978','EPSG:2263'])
def test_intersection_overlay_requires_matching_known_projected_crs(side,crs):
    from operators.geometric_operators import IntersectionInputError
    left,right=overlay_inputs();frame=left if side=='a' else right
    frame.set_crs(crs,allow_override=True,inplace=True)
    with pytest.raises(IntersectionInputError) as caught:exact_overlay(left,right)
    assert caught.value.code=='crs'


@pytest.mark.parametrize('crs',['EPSG:3857','EPSG:2263'])
def test_intersection_overlay_semantic_crs_and_original_units_are_preserved(crs):
    left,right=overlay_inputs();left=left.set_crs(crs,allow_override=True)
    right=right.set_crs(left.crs.to_wkt(),allow_override=True)
    result=exact_overlay(left,right)
    assert result.crs==left.crs and result.geometry.iloc[0].equals(box(1,1,2,2))
    assert result.geometry.iloc[0].area==1


def test_intersection_overlay_unsigned_identifier_is_not_coerced_to_float():
    left,right=overlay_inputs();left['a_key']=pd.Series([2**64-1],dtype='UInt64')
    result=exact_overlay(left,right)
    assert result.source_id_a.dtype==left.a_key.dtype and result.source_id_a.iloc[0]==2**64-1


@pytest.mark.parametrize('conflict',['business','result_geometry','a_reserved','b_reserved','geometry_reserved'])
def test_intersection_overlay_never_suffixes_or_overwrites_conflicts(conflict):
    from operators.geometric_operators import IntersectionInputError
    left,right=overlay_inputs()
    if conflict=='business':right['a_key']=[1]
    if conflict=='result_geometry':right[left.geometry.name]=['PRIVATE']
    if conflict=='a_reserved':left['source_id_a']=[1]
    if conflict=='b_reserved':right['source_id_b']=[1]
    if conflict=='geometry_reserved':left=left.rename_geometry('source_id_a')
    with pytest.raises(IntersectionInputError) as caught:exact_overlay(left,right)
    assert caught.value.code=='fields'


@pytest.mark.parametrize('language',['zh-cn','en'])
@pytest.mark.parametrize('problem,reason,parameter',[('source','operator_input_invalid','input_gdf'),
    ('reference','operator_reference_invalid','gdf_b'),('crs','operator_crs_invalid',''),('fields','operator_overlay_fields_conflict','')])
def test_intersection_overlay_typed_diagnostic_is_bound_and_contains_no_source_data(language,problem,reason,parameter):
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime import WorkflowExecutionError
    left,right=overlay_inputs()
    if problem=='source':left['a_key']=None
    elif problem=='reference':right['b_key']=None
    elif problem=='crs':right=right.set_crs(None,allow_override=True)
    else:right['a_key']=['PRIVATE']
    task={'id':'PRIVATE_NODE','operator':'intersection','params':{'input_gdf':left,'gdf_b':right,'id_field':'a_key','reference_id_field':'b_key'},'depends_on':[]}
    with pytest.raises(WorkflowExecutionError) as caught:GeoPythonWorkflowRunner(language=language).execute({'tasks':[task]})
    assert caught.value.error_code=='INVALID_PARAMS'
    assert caught.value.diagnostic=={'schema_version':'addp.execution-failure/v3','stage':'execution','reason':reason,
        'operator':{'operator_id':'intersection','node_index':1,**({'parameter':parameter} if parameter else {})}}
    assert 'PRIVATE' not in json.dumps(caught.value.diagnostic)
    assert ('overlay' in str(caught.value).lower() or 'intersection' in str(caught.value).lower()) if language=='en' else '相交叠加' in str(caught.value)


def test_intersection_overlay_native_error_is_not_a_parameter_error(monkeypatch):
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime import WorkflowExecutionError
    def fail(*args,**kwargs):raise shapely.errors.GEOSException('PRIVATE original source')
    left,right=overlay_inputs();monkeypatch.setattr(shapely,'intersection',fail)
    with pytest.raises(WorkflowExecutionError) as caught:GeoPythonWorkflowRunner().execute({'tasks':[
        {'id':'overlay','operator':'intersection','params':{'input_gdf':left,'gdf_b':right,'id_field':'a_key','reference_id_field':'b_key'},'depends_on':[]}]})
    assert caught.value.error_code=='EXECUTION_FAILED' and caught.value.diagnostic is None


def test_intersection_overlay_mixed_geometries_save_reload_independent_ogr_and_fail_before_save(tmp_path):
    from osgeo import ogr
    a=shapely.MultiPolygon([box(0,0,2,2),box(4,0,5,1),box(7,0,8,1)])
    b=shapely.MultiPolygon([box(1,1,3,3),box(5,0,6,1),box(8,1,9,2)])
    expected=[shapely.GeometryCollection([box(1,1,2,2),LineString([(5,0),(5,1)]),Point(8,1)]),
              LineString([(11,0),(11,1)]),Point(20,0),Point(31,0)]
    left,right=overlay_inputs([a,box(10,0,11,1),Point(20,0),LineString([(30,0),(32,0)])],
                              [b,box(11,0,12,1),LineString([(20,-1),(20,1)]),Point(31,0)])
    left['a_key']=pd.Series([2**60+1,2**60+3,2**60+5,2**60+7],dtype='Int64')
    left['grade']=pd.Series([None,2**53+1,0,-1],dtype='Int64')
    left['enabled']=pd.Series([None,False,True,False],dtype='boolean')
    left['geometry']=pd.Series(['普通业务',None,'点','线'],dtype='string')
    for name,frame in [('a',left),('b',right)]:frame.to_file(tmp_path/(name+'.gpkg'),driver='GPKG',index=False)
    originals={name:(tmp_path/(name+'.gpkg')).read_bytes() for name in ('a','b')}
    conn={'engine_type':'nfs','mount_path':str(tmp_path)}
    tasks=[{'id':'overlay','operator':'intersection','params':{'input_gdf':{'$ref':'a'},'gdf_b':{'$ref':'b'},'id_field':'a_key','reference_id_field':'b_key'},'depends_on':['a','b']}]
    tasks += [{'id':name,'operator':'load','params':{'connection_info':conn,'path':name+'.gpkg'},'depends_on':[]} for name in ('a','b')]
    tasks += [{'id':'save','operator':'save','params':{'input_df':{'$ref':'overlay'},'connection_info':conn,'path':'result.gpkg','mode':'fail'},'depends_on':['overlay']}]
    result=execute_workflow({'tasks':tasks});assert result['status']=='success',result
    ds=ogr.Open(str(tmp_path/'result.gpkg'));layer=ds.GetLayer(0);rows=list(layer)
    assert len(rows)==4 and layer.GetSpatialRef().GetAuthorityCode(None)=='3857'
    assert layer.GetLayerDefn().GetFieldDefn(layer.GetLayerDefn().GetFieldIndex('source_id_a')).GetType()==ogr.OFTInteger64
    assert layer.GetLayerDefn().GetFieldDefn(layer.GetLayerDefn().GetFieldIndex('enabled')).GetSubType()==ogr.OFSTBoolean
    for i,row in enumerate(rows):
        assert row.GetGeometryRef().Normalize().ExportToIsoWkb()==ogr.CreateGeometryFromWkt(expected[i].wkt).Normalize().ExportToIsoWkb()
        assert row.GetField('source_id_a')==2**60+1+2*i and row.GetField('a_key')==2**60+1+2*i
        assert row.GetField('source_id_b')==str(11+i)
        assert row.GetField('grade')==[None,2**53+1,0,-1][i]
        assert row.GetField('enabled')==[None,False,True,False][i]
        assert row.GetField('geometry')==['普通业务',None,'点','线'][i]
    physical_geometry=layer.GetGeometryColumn();assert physical_geometry
    ds=None
    loaded=get_operator('load')(connection_info=conn,path='result.gpkg')
    assert loaded.geometry.name==physical_geometry and len(loaded)==4
    assert loaded.grade.dtype=='Int64' and loaded.enabled.dtype=='boolean'
    assert [g.equals(e) for g,e in zip(loaded.geometry,expected)]==[True]*4
    for name,original in originals.items():assert (tmp_path/(name+'.gpkg')).read_bytes()==original
    tasks[0]['params']['reference_id_field']='PRIVATE_MISSING'
    tasks[-1]['params']['path']='forbidden.gpkg'
    failed=execute_workflow({'tasks':tasks})
    assert failed['status']=='failed' and failed['error_code']=='INVALID_PARAMS'
    assert failed['diagnostic']['reason']=='operator_reference_invalid'
    assert not (tmp_path/'forbidden.gpkg').exists()
    for name,original in originals.items():assert (tmp_path/(name+'.gpkg')).read_bytes()==original


def lines(geometries, crs="EPSG:3857"):
    return gpd.GeoDataFrame(
        {"source_name": [f"line-{i}" for i in range(len(geometries))]},
        geometry=geometries, crs=crs,
    ).rename_geometry("boundary")


def line_overlap_frame(geometries, ids=None, crs="EPSG:3857"):
    frame = lines(geometries, crs=crs)
    frame["route_key"] = pd.Series(range(len(frame)), dtype="Int64") if ids is None else ids
    return frame


def line_overlap(frame, id_field="route_key"):
    return get_operator("check_line_overlap")(input_gdf=frame, id_field=id_field)


def split_at_intersections(frame, id_field="route_key"):
    return get_operator("split_line_intersections")(input_gdf=frame, id_field=id_field)


def connected_parts(frame, id_field="route_key"):
    return get_operator("group_line_components")(input_gdf=frame, id_field=id_field)


def merge_chains(frame, fields=None):
    return get_operator("merge_line_chains")(frame, "route_key", [] if fields is None else fields)


def test_merge_line_chains_keeps_all_source_attributes_and_exact_traversal():
    frame = line_overlap_frame([LineString([(1,0),(2,0)]),LineString([(0,0),(1,0)]),LineString([(3,0),(2,0)])],
                               pd.Series([10,20,30],dtype="Int64"))
    frame["grade"] = pd.Series([7,7,7],dtype="Int64")
    frame["note"] = pd.Series(["middle",None,"reverse"],dtype="string")
    frame["enabled"] = pd.Series([True,None,False],dtype="boolean")
    frame["kind"] = pd.Categorical(["a","b","a"])
    frame.index = [7,7,-2];before = frame.copy(deep=True)
    outputs = merge_chains(frame,["grade"]);result,members = outputs["result"],outputs["members"]
    assert set(outputs)=={"result","members"}
    assert list(result.columns)==["grade","boundary","chain_id","member_count"]
    assert list(result.geometry.iloc[0].coords)==[(0,0),(1,0),(2,0),(3,0)]
    assert result.chain_id.tolist()==[1] and result.member_count.tolist()==[3]
    assert members.route_key.tolist()==[20,10,30] and members.member_no.tolist()==[1,2,3]
    assert members.source_part_no.tolist()==[1,1,1] and members.is_reversed.tolist()==[False,False,True]
    assert_geodataframe_equal(members[frame.columns],frame.iloc[[1,0,2]].reset_index(drop=True))
    assert result.grade.dtype==frame.grade.dtype
    for field in frame.columns:assert members[field].dtype==frame[field].dtype
    assert result.index.equals(pd.RangeIndex(1)) and members.index.equals(pd.RangeIndex(3))
    assert result.crs==members.crs==frame.crs
    assert_geodataframe_equal(frame,before)


@pytest.mark.parametrize("degree",[3,4,5])
def test_merge_line_chains_branch_uses_full_network_before_attributes(degree):
    endpoints=[(1,0),(-1,0),(0,1),(0,-1),(1,1)]
    frame=line_overlap_frame([LineString([p,(0,0)]) for p in endpoints[:degree]])
    frame["road"]=["same","same"]+["different"]*(degree-2)
    result=merge_chains(frame,["road"])["result"]
    assert len(result)==degree and result.member_count.tolist()==[1]*degree
    assert [g.wkb for g in result.geometry]==frame.geometry.to_wkb().tolist()


@pytest.mark.parametrize("values",[["a","b"],["a","a "],["A","a"],[0.0,2**-40],[True,1],[None,0]])
def test_merge_line_chains_exact_attribute_difference_stops_not_fails(values):
    frame=line_overlap_frame([LineString([(0,0),(1,0)]),LineString([(1,0),(2,0)])])
    frame["tag"]=pd.Series(values,dtype=object)
    output=merge_chains(frame,["tag"])
    assert output["result"].member_count.tolist()==[1,1]
    assert output["members"].route_key.tolist()==[0,1]


@pytest.mark.parametrize("values",[[None,pd.NA],[np.nan,None],[pd.NaT,None],["same","same"]])
def test_merge_line_chains_missing_matches_missing(values):
    frame=line_overlap_frame([LineString([(0,0),(1,0)]),LineString([(1,0),(2,0)])])
    frame["tag"]=pd.Series(values,dtype=object)
    assert merge_chains(frame,["tag"])["result"].member_count.tolist()==[2]


def test_merge_line_chains_explicit_empty_fields_ignores_no_source_information():
    frame=line_overlap_frame([LineString([(0,0),(1,0)]),LineString([(1,0),(2,0)])])
    frame["grade"]=[1,2]
    output=merge_chains(frame,[])
    assert list(output["result"].columns)==["boundary","chain_id","member_count"]
    assert output["result"].member_count.tolist()==[2]
    assert output["members"].grade.tolist()==[1,2]
    with pytest.raises(TypeError):get_operator("merge_line_chains")(frame,"route_key")


def test_merge_line_chains_matches_all_selected_fields_and_preserves_selection_order():
    frame=line_overlap_frame([LineString([(0,0),(1,0)]),LineString([(1,0),(2,0)]),LineString([(2,0),(3,0)])])
    frame["kind"]=["a"]*3;frame["grade"]=[1,1,2]
    result=merge_chains(frame,["grade","kind"])["result"]
    assert list(result.columns)==["grade","kind","boundary","chain_id","member_count"]
    assert result.member_count.tolist()==[2,1] and result.grade.tolist()==[1,2]


def test_merge_line_chains_multipart_members_are_original_parts_not_source_shortcuts():
    frame=line_overlap_frame([MultiLineString([[(0,0),(1,0)],[(10,0),(11,0)],[(1,0),(2,0)]]),LineString([(11,0),(12,0)])],
                               pd.Series(["多源","其他"],dtype="string"))
    output=merge_chains(frame);r,m=output["result"],output["members"]
    assert [list(g.coords) for g in r.geometry]==[[(0,0),(1,0),(2,0)],[(10,0),(11,0),(12,0)]]
    assert m.route_key.tolist()==["多源","多源","多源","其他"]
    assert m.source_part_no.tolist()==[1,3,2,1] and m.chain_id.tolist()==[1,1,2,2]
    assert [g.wkb for g in m.geometry]==[shapely.get_parts(frame.geometry.iloc[0])[i].wkb for i in [0,2,1]]+[frame.geometry.iloc[1].wkb]


@pytest.mark.parametrize("multipart",[False,True])
def test_merge_line_chains_isolated_closed_path_keeps_earliest_part_seam_and_direction(multipart):
    shapes=[LineString([(2,0),(2,2),(0,2)]),LineString([(0,0),(2,0)]),LineString([(0,2),(0,0)])]
    frame=line_overlap_frame([MultiLineString(shapes)] if multipart else shapes)
    output=merge_chains(frame);r,m=output["result"],output["members"]
    assert len(r)==1 and r.member_count.tolist()==[3]
    assert list(r.geometry.iloc[0].coords)==[(2,0),(2,2),(0,2),(0,0),(2,0)]
    assert m.is_reversed.tolist()==[False,False,False]
    assert m.source_part_no.tolist()==([1,3,2] if multipart else [1,1,1])


def test_merge_line_chains_closed_component_at_branch_is_not_opened_or_routed():
    ring=LineString([(0,0),(2,0),(2,2),(0,0)]);arm=LineString([(0,0),(-1,0)])
    frame=line_overlap_frame([ring,arm]);result=merge_chains(frame)["result"]
    assert result.member_count.tolist()==[1,1] and result.geometry.to_wkb().tolist()==frame.geometry.to_wkb().tolist()


def test_merge_line_chains_duplicate_vertices_and_micro_gap_remain_exact():
    frame=line_overlap_frame([LineString([(0,0),(0,0),(1,0),(1,0)]),LineString([(2,0),(2,0),(1,0),(1,0)]),
                              LineString([(2+2**-40,0),(3,0)])])
    output=merge_chains(frame);r,m=output["result"],output["members"]
    assert r.member_count.tolist()==[2,1]
    assert list(r.geometry.iloc[0].coords)==[(0,0),(0,0),(1,0),(1,0),(1,0),(2,0),(2,0)]
    assert list(r.geometry.iloc[1].coords)==[(2+2**-40,0),(3,0)]
    assert m.is_reversed.tolist()==[False,True,False]
    assert m.geometry.to_wkb().tolist()==frame.geometry.to_wkb().tolist()


def test_merge_line_chains_order_does_not_follow_identifier_or_index_candidates(monkeypatch):
    import operators.geometric_operators as module
    frame=line_overlap_frame([LineString([(10,0),(11,0)]),LineString([(1,0),(2,0)]),LineString([(0,0),(1,0)])],[90,70,20])
    expected=merge_chains(frame);original=module.shapely.STRtree
    class Reverse:
        def __init__(self,shapes):self.tree=original(shapes)
        def query(self,*args,**kwargs):return self.tree.query(*args,**kwargs)[::-1]
    monkeypatch.setattr(module.shapely,"STRtree",Reverse)
    actual=merge_chains(frame)
    for port in expected:assert_geodataframe_equal(actual[port],expected[port])
    assert actual["members"].route_key.tolist()==[90,20,70]
    assert actual["result"].member_count.tolist()==[1,2]


@pytest.mark.parametrize("field",["chain_id","member_count","member_no","source_part_no","is_reversed"])
def test_merge_line_chains_rejects_reserved_columns(field):
    frame=line_overlap_frame([LineString([(0,0),(1,0)])]);frame[field]=1
    with pytest.raises(ValueError,match="column"):merge_chains(frame)


@pytest.mark.parametrize("fields",[None,"grade",("grade",),["missing"],["boundary"],["route_key"],["grade","grade"],[1]])
def test_merge_line_chains_requires_explicit_valid_business_field_list(fields):
    frame=line_overlap_frame([LineString([(0,0),(1,0)])]);frame["grade"]=1
    with pytest.raises(ValueError,match="match_fields"):get_operator("merge_line_chains")(frame,"route_key",fields)


@pytest.mark.parametrize("value",[[1],{"nested":1},np.array([1])])
def test_merge_line_chains_rejects_non_scalar_consistency_values(value):
    frame=line_overlap_frame([LineString([(0,0),(1,0)])]);frame["tag"]=[value]
    with pytest.raises(ValueError,match="scalar"):merge_chains(frame,["tag"])


@pytest.mark.parametrize("shapes",[
    [LineString([(0,0),(2,0)]),LineString([(1,-1),(1,0)])],
    [LineString([(0,0),(2,0)]),LineString([(1,-1),(1,1)])],
    [LineString([(0,0),(2,0)]),LineString([(1,0),(3,0)])],
    [LineString([(0,0),(2,0)]),LineString([(2,0),(0,0)])],
    [LineString([(0,0),(2,2),(0,2),(2,0)])],
    [LineString([(0,0),(2,0),(0,0)])],
    [MultiLineString([[(0,0),(2,0)],[(1,-1),(1,0)]])],
])
def test_merge_line_chains_rejects_unresolved_crossings_and_overlap_without_mutation(shapes):
    frame=line_overlap_frame(shapes);before=frame.copy(deep=True)
    with pytest.raises(ValueError,match="intersection|overlap"):merge_chains(frame)
    assert_geodataframe_equal(frame,before)


@pytest.mark.parametrize("shape",[None,LineString(),Point(0,0),LineString([(0,0),(0,0)]),
    LineString([(0,0,1),(1,0,1)]),shapely.from_wkt("LINESTRING M (0 0 1, 1 0 1)"),
    LineString([(0,0),(float("inf"),1)]),LineString([(0,0),(float("nan"),1)])])
def test_merge_line_chains_rejects_invalid_geometry(shape):
    frame=line_overlap_frame([shape]);before=frame.geometry.to_wkb().tolist()
    with pytest.raises(ValueError):merge_chains(frame)
    assert frame.geometry.to_wkb().tolist()==before


@pytest.mark.parametrize("crs",[None,"EPSG:4326","EPSG:4978"])
def test_merge_line_chains_requires_projected_crs(crs):
    with pytest.raises(ValueError,match="projected"):merge_chains(line_overlap_frame([LineString([(0,0),(1,0)])],crs=crs))


@pytest.mark.parametrize("ids",[[None],[" "],[1,1],[[1]]])
def test_merge_line_chains_requires_unique_nonempty_scalar_source_id(ids):
    frame=line_overlap_frame([LineString([(i,0),(i+1,0)]) for i in range(len(ids))],ids)
    with pytest.raises(ValueError):merge_chains(frame)


@pytest.mark.parametrize("empty",[False,True])
def test_merge_line_chains_preserves_nullable_schema_and_source_identity(empty):
    frame=line_overlap_frame([LineString([(0,0),(1,0)])],pd.Series([2**60+5],dtype="Int64"))
    frame["tag"]=pd.Series([pd.NA],dtype="string");frame["kind"]=pd.Categorical(["a"])
    if empty:frame=frame.iloc[:0]
    r,m=merge_chains(frame,["tag","kind"]).values()
    assert r.tag.dtype==m.tag.dtype==frame.tag.dtype and r.kind.dtype==frame.kind.dtype
    assert m.route_key.dtype==frame.route_key.dtype
    assert r.chain_id.dtype==r.member_count.dtype==m.member_no.dtype==m.source_part_no.dtype=="int64"
    assert m.is_reversed.dtype=="bool" and len(r)==len(m)==len(frame)
    assert r.crs==m.crs==frame.crs and r.geometry.name==m.geometry.name=="boundary"


def test_merge_line_chains_metadata_workflow_only_two_geodataframe_ports(monkeypatch):
    import api_server
    op=next(o for o in list_operators() if o["id"]=="merge_line_chains")
    assert op["display_name"]=="链路合并" and op["category_path"]==["矢量处理","线网处理"]
    assert op["execution_modes"]==["workflow"] and op["effects"]==["read"]
    assert [(p["name"],p["required"]) for p in op["parameters"]]==[("input_gdf",True),("id_field",True),("match_fields",True)]
    assert [(p["name"],p["type"],p["is_default"]) for p in op["output_ports"]]==[("result","geodataframe",True),("members","geodataframe",False)]
    assert op["parameters"][-1]["type"]=="array" and op["parameters"][-1]["item_type"]=="string" and "default" not in op["parameters"][-1]
    with api_server.app.test_client() as client:
        listed=next(o for o in client.get("/api/operators").get_json()["operators"] if o["id"]=="merge_line_chains")
        assert listed==op
        assert client.post("/api/operators/merge_line_chains/invoke",json={"params":{}}).status_code==403


def test_merge_line_chains_workflow_two_saves_reloaded_by_independent_ogr(tmp_path):
    from osgeo import ogr
    ogr.UseExceptions()
    frame=line_overlap_frame([LineString([(1,0),(2,0)]),LineString([(0,0),(1,0)]),LineString([(3,0),(2,0)])],["中","左","右"])
    frame["grade"]=pd.Series([7,7,7],dtype="Int64");frame["note"]=pd.Series(["m",None,"r"],dtype="string")
    source=tmp_path/"source.gpkg";frame.to_file(source,driver="GPKG",layer="source");before=source.read_bytes()
    connection={"engine_type":"nfs","mount_path":str(tmp_path)}
    tasks=[{"id":"load","operator":"load","params":{"connection_info":connection,"path":"source.gpkg"},"depends_on":[]},
           {"id":"merge","operator":"merge_line_chains","params":{"input_gdf":{"$ref":"load"},"id_field":"route_key","match_fields":["grade"]},"depends_on":["load"]}]
    for port in ["result","members"]:
        ref={"$ref":"merge"} if port=="result" else {"$ref":"merge","port":port}
        tasks.append({"id":"save_"+port,"operator":"save","params":{"input_df":ref,"connection_info":connection,"path":port+".gpkg"},"depends_on":["merge"]})
    execution=execute_workflow({"tasks":tasks});assert execution["status"]=="success",execution.get("error")
    preview=json.loads(execution["all_results"]["merge"]);assert set(preview)=={"result","members"}
    for port,count,shapes in [("result",1,[[(0,0),(1,0),(2,0),(3,0)]]),("members",3,[[(0,0),(1,0)],[(1,0),(2,0)],[(3,0),(2,0)]])]:
        ds=ogr.Open(str(tmp_path/(port+".gpkg")),0);layer=ds.GetLayer(0)
        assert layer.GetFeatureCount()==count and layer.GetSpatialRef().GetAuthorityCode(None)=="3857"
        rows=list(layer);assert [list(f.GetGeometryRef().GetPoints()) for f in rows]==shapes
        assert all(f.GetGeometryRef().GetGeometryName()=="LINESTRING" for f in rows)
        if port=="result":assert rows[0].GetField("chain_id")==1 and rows[0].GetField("member_count")==3 and "route_key" not in [f.GetName() for f in layer.schema]
        else:
            assert [f.GetField("route_key") for f in rows]==["左","中","右"]
            assert [f.GetField("note") for f in rows]==[None,"m","r"]
            assert [f.GetField("is_reversed") for f in rows]==[False,False,True]
            assert [f.GetField("member_no") for f in rows]==[1,2,3]
        for name in (["grade","chain_id","member_count"] if port=="result" else ["grade","chain_id","member_no","source_part_no"]):
            assert layer.GetLayerDefn().GetFieldDefn(layer.GetLayerDefn().GetFieldIndex(name)).GetType()==ogr.OFTInteger64
        if port=="members":
            assert layer.GetLayerDefn().GetFieldDefn(layer.GetLayerDefn().GetFieldIndex("is_reversed")).GetSubType()==ogr.OFSTBoolean
    assert source.read_bytes()==before


def test_merge_line_chains_branch_on_closed_path_remains_the_chain_boundary():
    frame=line_overlap_frame([LineString([(2,0),(2,2)]),LineString([(0,0),(2,0)]),
        LineString([(2,2),(0,0)]),LineString([(0,0),(-1,0)])])
    result,members=merge_chains(frame).values()
    assert result.member_count.tolist()==[3,1]
    assert list(result.geometry.iloc[0].coords)==[(0,0),(2,0),(2,2),(0,0)]
    assert members.route_key.tolist()==[1,0,2,3] and members.is_reversed.tolist()==[False]*4


def test_merge_line_chains_keeps_upstream_source_columns_in_members_only():
    frame=line_overlap_frame([LineString([(0,0),(1,0)]),LineString([(1,0),(2,0)])],["segment-a","segment-b"])
    frame["source_id"]=["original", "original"];frame["part_no"]=[1,1];frame["segment_no"]=[1,2]
    result,members=merge_chains(frame).values()
    assert len(result)==1 and result.member_count.tolist()==[2]
    assert "source_id" not in result.columns
    assert members.source_id.tolist()==["original"]*2 and members.segment_no.tolist()==[1,2]
    assert members.source_part_no.tolist()==[1,1] and members.route_key.tolist()==["segment-a","segment-b"]


def test_merge_line_chains_lens_with_two_common_endpoints_produces_one_closed_chain():
    frame=line_overlap_frame([LineString([(0,0),(1,1),(2,0)]),LineString([(0,0),(1,-1),(2,0)])])
    result,members=merge_chains(frame).values()
    assert result.member_count.tolist()==[2] and members.is_reversed.tolist()==[False,True]
    assert list(result.geometry.iloc[0].coords)==[(0,0),(1,1),(2,0),(1,-1),(0,0)]


def test_merge_line_chains_input_rejection_leaves_no_dag_save_targets(tmp_path):
    source=tmp_path/"source.gpkg";line_overlap_frame([LineString([(0,0),(2,0)]),LineString([(1,-1),(1,1)])]).to_file(source,driver="GPKG")
    connection={"engine_type":"nfs","mount_path":str(tmp_path)}
    tasks=[{"id":"load","operator":"load","params":{"connection_info":connection,"path":"source.gpkg"},"depends_on":[]},
        {"id":"merge","operator":"merge_line_chains","params":{"input_gdf":{"$ref":"load"},"id_field":"route_key","match_fields":[]},"depends_on":["load"]}]
    for port in ["result","members"]:
        tasks.append({"id":port,"operator":"save","params":{"input_df":{"$ref":"merge","port":port},"connection_info":connection,"path":port+".gpkg"},"depends_on":["merge"]})
    state=execute_workflow({"tasks":tasks})
    assert state["status"]=="failed" and "non-endpoint intersection" in state["error"]
    assert not (tmp_path/"result.gpkg").exists() and not (tmp_path/"members.gpkg").exists()


def test_group_line_components_transitive_labels_follow_first_source_not_identifier_sort():
    frame = line_overlap_frame([
        LineString([(0, 0), (1, 0)]), LineString([(4, 0), (5, 0)]),
        LineString([(100, 0), (101, 0)]), LineString([(1, 0), (4, 0)]),
    ], pd.Series([90, 10, 70, 20], dtype="Int64"))
    result = connected_parts(frame)
    assert result.component_id.tolist() == [1, 1, 2, 1]
    assert result.source_id.tolist() == [90, 10, 70, 20]
    assert result.part_no.tolist() == [1, 1, 1, 1]
    assert result.source_id.dtype == frame.route_key.dtype
    assert [g.wkb for g in result.geometry] == [g.wkb for g in frame.geometry]


def test_group_line_components_labels_do_not_depend_on_tree_candidate_order(monkeypatch):
    import operators.geometric_operators as module
    frame = line_overlap_frame([LineString([(0,0),(1,0)]),LineString([(4,0),(5,0)]),
                                LineString([(100,0),(101,0)]),LineString([(1,0),(4,0)])])
    expected = connected_parts(frame)
    original = module.shapely.STRtree
    class ReverseCandidates:
        def __init__(self, shapes):
            self.tree = original(shapes)
        def query(self, *args, **kwargs):
            return self.tree.query(*args, **kwargs)[::-1]
    monkeypatch.setattr(module.shapely, "STRtree", ReverseCandidates)
    assert_geodataframe_equal(connected_parts(frame), expected)


@pytest.mark.parametrize("other,connected", [
    ([(2, 0), (3, 1)], True),  # endpoint
    ([(1, -1), (1, 0)], True),  # T contact
    ([(1, -1), (1, 1)], True),  # crossing without a source vertex
    ([(1, 0), (3, 0)], True),  # positive overlap
    ([(2, 0), (0, 0)], True),  # reversed duplicate
    ([(2 + 2**-40, 0), (3, 0)], False),  # exact micro gap
    ([(0, 2**-40), (2, 2**-40)], False),  # near parallel
])
def test_group_line_components_exact_contacts_and_micro_gaps(other, connected):
    frame = line_overlap_frame([LineString([(0, 0), (2, 0)]), LineString(other)])
    result = connected_parts(frame)
    assert result.component_id.tolist() == ([1, 1] if connected else [1, 2])
    assert [g.wkb for g in result.geometry] == [g.wkb for g in frame.geometry]


def test_group_line_components_overlapping_envelopes_are_not_connectivity():
    frame = line_overlap_frame([LineString([(0, 0), (2, 2)]), LineString([(0, 1), (1, 2)])])
    assert connected_parts(frame).component_id.tolist() == [1, 2]


def test_group_line_components_multipart_is_not_an_implicit_bridge_and_keeps_attributes():
    parts = [[(2, 0), (1, 0), (0, 0)], [(10, 0), (11, 0)], [(30, 0), (32, 0), (32, 2), (30, 0)]]
    frame = line_overlap_frame([MultiLineString(parts), LineString([(1, -1), (1, 1)]),
                                LineString([(40, 0), (42, 0), (42, 2), (40, 0)])],
                               pd.Series([2**60 + 1, 2**60 + 2, 2**60 + 3], dtype="Int64"))
    frame["grade"] = pd.Series([None, 4, 8], dtype="Int64")
    frame["label"] = pd.Series(["道路甲", None, "闭环"], dtype="string")
    frame["enabled"] = pd.Series([True, None, False], dtype="boolean")
    frame["kind"] = pd.Series(pd.Categorical(["a", "b", "a"]))
    frame.index = pd.Index([7, 7, -2], name="old_index")
    before = frame.copy(deep=True)
    result = connected_parts(frame)
    assert result.component_id.tolist() == [1, 2, 3, 1, 4]
    assert result.part_no.tolist() == [1, 2, 3, 1, 1]
    assert result.source_id.tolist() == [2**60 + 1] * 3 + [2**60 + 2, 2**60 + 3]
    assert result.index.equals(pd.RangeIndex(5)) and result.geometry.name == "boundary"
    assert result.part_no.dtype == result.component_id.dtype == "int64"
    assert list(result.columns) == list(frame.columns) + ["source_id", "part_no", "component_id"]
    expected = frame.iloc[[0, 0, 0, 1, 2]].reset_index(drop=True)
    expected["boundary"] = gpd.GeoSeries([*(LineString(v) for v in parts), *frame.geometry.iloc[1:]], crs=frame.crs)
    assert_geodataframe_equal(result[frame.columns], expected)
    assert_geodataframe_equal(frame, before)


@pytest.mark.parametrize("shape", [
    LineString([(0, 0), (2, 2), (0, 2), (2, 0)]),
    LineString([(0, 0), (2, 0), (0, 0)]),
    LineString([(0, 0), (0, 0), (2, 0), (2, 0)]),
    LineString([(0, 0), (2, 0), (2, 2), (0, 0)]),
    MultiLineString([[(0, 0), (2, 0)], [(1, -1), (1, 1)], [(2, 0), (0, 0)]]),
])
def test_group_line_components_keeps_internal_crossing_overlap_and_original_vertices(shape):
    frame = line_overlap_frame([shape], ["原线"])
    result = connected_parts(frame)
    expected = list(shapely.get_parts(shape))
    assert result.component_id.tolist() == [1] * len(expected)
    assert result.part_no.tolist() == list(range(1, len(expected) + 1))
    assert [g.wkb for g in result.geometry] == [g.wkb for g in expected]


@pytest.mark.parametrize("identifiers", [pd.Series(["甲"], dtype="string"), pd.Series([2**60 + 5], dtype="Int64"), pd.Series([True], dtype="boolean")])
@pytest.mark.parametrize("empty", [False, True])
def test_group_line_components_empty_and_nonempty_preserve_source_schema(identifiers, empty):
    frame = line_overlap_frame([LineString([(0, 0), (1, 0)])], identifiers)
    if empty:
        frame = frame.iloc[:0]
    result = connected_parts(frame)
    assert len(result) == len(frame) and result.crs == frame.crs and result.geometry.name == frame.geometry.name
    assert result.source_id.dtype == frame.route_key.dtype
    assert all(result[c].dtype == frame[c].dtype for c in frame.columns)
    assert result.part_no.dtype == result.component_id.dtype == "int64"


@pytest.mark.parametrize("field", ["source_id", "part_no", "component_id"])
def test_group_line_components_rejects_output_column_collision(field):
    frame = line_overlap_frame([LineString([(0, 0), (1, 0)])]);frame[field] = 8
    with pytest.raises(ValueError, match="column|field"):
        connected_parts(frame)


@pytest.mark.parametrize("shape", [None, LineString(), MultiLineString(), Point(0, 0), box(0, 0, 1, 1),
    LineString([(0, 0, 1), (1, 1, 2)]), shapely.from_wkt("LINESTRING M (0 0 1, 1 1 2)"),
    LineString([(0, 0), (float("inf"), 1)]), LineString([(0, 0), (float("nan"), 1)]),
    LineString([(0, 0), (0, 0)])])
def test_group_line_components_rejects_bad_geometry_without_mutation(shape):
    frame = line_overlap_frame([shape]);before = frame.copy(deep=True)
    with pytest.raises(ValueError):
        connected_parts(frame)
    # Invalid NaN/Inf inputs cannot be compared by topological equality.
    # Compare original bytes and non-geometry values without asking GEOS to
    # evaluate an invalid geometry a second time.
    assert frame.geometry.to_wkb().tolist() == before.geometry.to_wkb().tolist()
    pd.testing.assert_frame_equal(frame.drop(columns=frame.geometry.name), before.drop(columns=before.geometry.name))


@pytest.mark.parametrize("crs", [None, "EPSG:4326", "EPSG:4978"])
def test_group_line_components_requires_known_projected_xy(crs):
    with pytest.raises(ValueError, match="projected"):
        connected_parts(line_overlap_frame([LineString([(0, 0), (1, 0)])], crs=crs))


@pytest.mark.parametrize("identifiers", [[None, 2], [1, 1], ["", "b"], ["  ", "b"], [[1], [2]]])
def test_group_line_components_requires_unique_scalar_source_identity(identifiers):
    with pytest.raises(ValueError, match="id_field"):
        connected_parts(line_overlap_frame([LineString([(0, 0), (1, 0)]), LineString([(2, 0), (3, 0)])], identifiers))


@pytest.mark.parametrize("id_field", ["absent", "boundary", None])
def test_group_line_components_does_not_infer_identifier(id_field):
    with pytest.raises(ValueError, match="id_field"):
        connected_parts(line_overlap_frame([LineString([(0, 0), (1, 0)])]), id_field)


@pytest.mark.parametrize("empty", [False, True])
def test_group_line_components_workflow_native_save_reload_and_rejection_no_publish(tmp_path, empty):
    from osgeo import ogr
    frame = line_overlap_frame([MultiLineString([[(2, 0), (1, 0), (0, 0)], [(10, 0), (11, 0)]]),
                                LineString([(1, -1), (1, 1)])], ["甲", "乙"])
    frame["note"] = pd.Series([None, "支线"], dtype="string")
    frame["grade"] = pd.Series([2**60 + 1, 3], dtype="Int64")
    if empty:frame = frame.iloc[:0]
    frame.to_file(tmp_path / "source.gpkg", layer="routes", driver="GPKG")
    before = (tmp_path / "source.gpkg").read_bytes();connection = {"engine_type":"nfs", "mount_path":str(tmp_path)}
    tasks = [
        {"id":"load", "operator":"load", "params":{"connection_info":connection,"path":"source.gpkg"},"depends_on":[]},
        {"id":"group", "operator":"group_line_components", "params":{"input_gdf":{"$ref":"load"},"id_field":"route_key"},"depends_on":["load"]},
        {"id":"save", "operator":"save", "params":{"input_df":{"$ref":"group"},"connection_info":connection,"path":"groups.gpkg","mode":"fail"},"depends_on":["group"]},
    ]
    execution = execute_workflow({"tasks":tasks});assert execution["status"] == "success", execution
    dataset = ogr.Open(str(tmp_path / "groups.gpkg"));layer = dataset.GetLayer(0)
    assert layer.GetFeatureCount() == (0 if empty else 3) and layer.GetSpatialRef().GetAuthorityCode(None) == "3857"
    assert [field.name for field in layer.schema] == ["source_name","route_key","note","grade","source_id","part_no","component_id"]
    for field in ("grade","part_no","component_id"):
        assert layer.GetLayerDefn().GetFieldDefn(layer.GetLayerDefn().GetFieldIndex(field)).GetType() == ogr.OFTInteger64
    expected = [[(2,0),(1,0),(0,0)],[(10,0),(11,0)],[(1,-1),(1,1)]]
    for i,feature in enumerate(layer):
        assert feature.GetGeometryRef().GetGeometryName() == "LINESTRING" and feature.GetGeometryRef().GetPoints() == expected[i]
        assert feature.GetField("source_id") == feature.GetField("route_key") == ("甲" if i < 2 else "乙")
        assert feature.GetField("component_id") == [1,2,1][i] and feature.GetField("part_no") == [1,2,1][i]
        assert feature.GetField("note") == (None if i < 2 else "支线") and feature.GetField("grade") == (2**60 + 1 if i < 2 else 3)
    dataset = None;assert (tmp_path / "source.gpkg").read_bytes() == before
    tasks[1]["params"]["id_field"] = "absent";tasks[2]["params"]["path"] = "rejected.gpkg"
    assert execute_workflow({"tasks":tasks})["status"] == "failed" and not (tmp_path / "rejected.gpkg").exists()


def test_group_line_components_metadata_async_discovery_and_workflow_only(tmp_path, monkeypatch):
    import api_server
    from addp_common.workflow_runtime import ExecutionRegistry
    frame = line_overlap_frame([MultiLineString([[(0,0),(1,0)],[(10,0),(11,0)]]),LineString([(1,0),(2,0)])], ["甲","乙"])
    frame.to_file(tmp_path / "source.gpkg", layer="routes", driver="GPKG")
    tasks = [
        {"id":"load","operator":"load","params":{"connection_info":{"engine_type":"nfs","mount_path":str(tmp_path)},"path":"source.gpkg"},"depends_on":[]},
        {"id":"group","operator":"group_line_components","params":{"input_gdf":{"$ref":"load"},"id_field":"route_key"},"depends_on":["load"]},
    ]
    monkeypatch.setattr(api_server,"executions",ExecutionRegistry())
    with api_server.app.test_client() as client:
        op = next(o for o in client.get("/api/operators").get_json()["operators"] if o["id"] == "group_line_components")
        assert op["display_name"] == "连通分组" and op["category_path"] == ["矢量处理","线网处理"]
        assert op["execution_modes"] == ["workflow"] and op["effects"] == ["read"]
        assert [(p["name"],p["required"]) for p in op["parameters"]] == [("input_gdf",True),("id_field",True)]
        assert len(op["output_ports"]) == 1 and op["output_ports"][0]["name"] == "result" and op["output_ports"][0]["is_default"] is True
        assert client.post("/api/operators/group_line_components/invoke",json={"params":{}}).status_code == 403
        response = client.post("/api/workflow",json={"workflow_def":{"tasks":tasks},"runtime":{"tenant_id":7,"execution_authorization":{"id":1,"effects":["read"]}}})
        assert response.status_code == 202
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            state = client.get("/api/executions/" + response.get_json()["execution_id"]).get_json()
            if state["status"] in ("success","failed"):break
            time.sleep(.01)
        assert state["status"] == "success", state
        features = json.loads(state["result"])["features"]
        assert [f["properties"]["component_id"] for f in features] == [1,2,1]
        assert [f["properties"]["source_id"] for f in features] == ["甲","甲","乙"]
    tasks[1]["params"].pop("id_field")
    assert execute_workflow({"tasks":tasks})["status"] == "failed"


def test_split_line_intersections_crossings_t_junctions_vertices_contacts_and_attributes():
    frame = line_overlap_frame([
        LineString([(0, 0), (1, 0), (3, 0)]), LineString([(1, -1), (1, 1)]),
        LineString([(2, -1), (2, 0)]), LineString([(3, 0), (4, 1)]),
    ], pd.Series([2**60 + i for i in range(4)], dtype="Int64"))
    frame["grade"] = pd.Series([None, 3, 4, 5], dtype="Int64")
    frame["label"] = pd.Series(["甲", None, "丙", "丁"], dtype="string")
    frame.index = [9, 9, 2, 1]
    before = frame.copy(deep=True)
    result = split_at_intersections(frame)
    assert result.columns.tolist() == frame.columns.tolist() + ["source_id", "part_no", "segment_no"]
    expected_rows = [0, 0, 0, 1, 1, 2, 3]
    assert result.source_id.tolist() == [frame.route_key.iloc[i] for i in expected_rows]
    assert result.source_id.dtype == frame.route_key.dtype
    assert result.part_no.tolist() == [1] * 7
    assert result.segment_no.tolist() == [1, 2, 3, 1, 2, 1, 1]
    expected = [[(0, 0), (1, 0)], [(1, 0), (2, 0)], [(2, 0), (3, 0)],
                [(1, -1), (1, 0)], [(1, 0), (1, 1)], [(2, -1), (2, 0)], [(3, 0), (4, 1)]]
    assert [list(g.coords) for g in result.geometry] == expected
    attributes = frame.iloc[expected_rows].reset_index(drop=True).drop(columns="boundary")
    pd.testing.assert_frame_equal(result[attributes.columns], attributes)
    assert result.geometry.name == "boundary" and result.crs == frame.crs
    assert result.index.equals(pd.RangeIndex(7))
    assert result.part_no.dtype == result.segment_no.dtype == "int64"
    assert_geodataframe_equal(frame, before)


def test_split_line_intersections_multipart_direction_and_duplicate_cut_deduplication():
    frame = line_overlap_frame([
        MultiLineString([[(4, 0), (2, 0), (0, 0)], [(4, 3), (0, 3)]]),
        LineString([(1, -1), (1, 0)]), LineString([(1, 0), (1, 1)]),
    ])
    result = split_at_intersections(frame)
    assert [list(g.coords) for g in result.geometry] == [
        [(4, 0), (2, 0), (1, 0)], [(1, 0), (0, 0)], [(4, 3), (0, 3)],
        [(1, -1), (1, 0)], [(1, 0), (1, 1)],
    ]
    assert result.part_no.tolist() == [1, 1, 2, 1, 1]
    assert result.segment_no.tolist() == [1, 2, 1, 1, 1]


def test_split_line_intersections_closed_seam_and_repeated_original_coordinates():
    frame = line_overlap_frame([
        LineString([(0, 0), (0, 0), (2, 0), (2, 2), (0, 2), (0, 0)]),
        LineString([(1, -1), (1, 0)]),
    ])
    result = split_at_intersections(frame)
    assert [list(g.coords) for g in result.geometry] == [
        [(0, 0), (0, 0), (1, 0)], [(1, 0), (2, 0), (2, 2), (0, 2), (0, 0)],
        [(1, -1), (1, 0)],
    ]
    assert all(g.is_valid and not g.is_empty for g in result.geometry)
    assert list(split_at_intersections(frame.iloc[:1]).geometry.iloc[0].coords) == list(frame.geometry.iloc[0].coords)


def test_split_line_intersections_endpoint_contact_preserves_trailing_repeated_vertices():
    vertices = [(0, 0), (0, 0), (1, 0), (1, 0), (1, 0)]
    result = split_at_intersections(line_overlap_frame([
        LineString(vertices), LineString([(1, 0), (2, 1)]),
    ]))
    assert len(result) == 2
    assert list(result.geometry.iloc[0].coords) == vertices


@pytest.mark.parametrize("crs", ["EPSG:3857", "EPSG:2263"])
def test_split_line_intersections_binary_tiny_pieces_without_cumulative_distance_rounding(crs):
    tiny = 2**-40
    frame = line_overlap_frame([
        LineString([(-2**50, 0), (0, 0), (1, 0)]),
        LineString([(tiny, -1), (tiny, 1)]), LineString([(2*tiny, -1), (2*tiny, 1)]),
    ], crs=crs)
    result = split_at_intersections(frame)
    assert [list(g.coords) for g in result.geometry[:3]] == [
        [(-2**50, 0), (0, 0), (tiny, 0)], [(tiny, 0), (2*tiny, 0)], [(2*tiny, 0), (1, 0)],
    ]
    assert result.crs == frame.crs


def test_split_line_intersections_nonbinary_diagonal_crossing_uses_one_shared_cut_coordinate():
    result = split_at_intersections(line_overlap_frame([
        LineString([(0, 0), (7, 3)]), LineString([(1, 2), (5, 0)]),
    ]))
    point = result.geometry.iloc[0].coords[-1]
    assert len(result) == 4
    assert point == result.geometry.iloc[1].coords[0] == result.geometry.iloc[2].coords[-1] == result.geometry.iloc[3].coords[0]


@pytest.mark.parametrize("reverse", [False, True])
def test_split_line_intersections_steep_path_orders_cuts_when_minor_axis_rounds_equal(reverse):
    x = 2**50
    vertices = [(x, 0), (x + 1, 64)]
    if reverse:
        vertices.reverse()
    frame = line_overlap_frame([LineString(vertices)] + [LineString([(x - 1, y), (x + 2, y)]) for y in range(1, 5)])
    result = split_at_intersections(frame)
    pieces = result.loc[result.source_id == 0].geometry
    expected_y = [0, 1, 2, 3, 4, 64]
    if reverse:
        expected_y.reverse()
    assert [(g.coords[0][1], g.coords[-1][1]) for g in pieces] == list(zip(expected_y, expected_y[1:]))


def test_split_line_intersections_matches_independent_integer_grid_path_oracle():
    horizontal = [LineString([(0, y), (6, y)]) for y in range(5)]
    vertical = [LineString([(x, 5), (x, -1)]) for x in range(1, 6)]
    frame = line_overlap_frame(horizontal + vertical)
    result = split_at_intersections(frame)
    expected = []
    for row in range(10):
        vertices = ([(x, row) for x in range(7)] if row < 5
                    else [(row - 4, y) for y in range(5, -2, -1)])
        for number, (start, end) in enumerate(zip(vertices, vertices[1:]), 1):
            expected.append((row, 1, number, [start, end]))
    actual = [(row.source_id, row.part_no, row.segment_no, list(row.boundary.coords)) for row in result.itertuples()]
    assert actual == expected


@pytest.mark.parametrize("field", [None, "missing", "boundary"])
def test_split_line_intersections_rejects_missing_or_geometry_identity(field):
    with pytest.raises(ValueError, match="id_field"):
        split_at_intersections(line_overlap_frame([LineString([(0, 0), (1, 0)])]), field)


def test_split_line_intersections_requires_active_geodataframe_and_unique_columns():
    with pytest.raises(ValueError, match="GeoDataFrame"):
        split_at_intersections(pd.DataFrame({"route_key": [1]}))
    frame = line_overlap_frame([LineString([(0, 0), (1, 0)])])
    with pytest.raises(ValueError, match="active geometry"):
        split_at_intersections(gpd.GeoDataFrame({"route_key": [1]}))
    frame.columns = ["route_key", "boundary", "route_key"]
    with pytest.raises(ValueError, match="id_field"):
        split_at_intersections(frame)


@pytest.mark.parametrize("geometries", [
    [LineString([(0, 0), (2, 2), (0, 2), (2, 0)])],
    [LineString([(0, 0), (2, 0), (1, 0)])],
    [MultiLineString([[(0, 0), (1, 0)], [(1, 0), (2, 1)]])],
    [MultiLineString([[(0, 0), (2, 0)], [(1, -1), (1, 1)]])],
    [LineString([(0, 0), (2, 0)]), LineString([(1, 0), (3, 0)])],
    [LineString([(0, 0), (2, 0)]), LineString([(2, 0), (0, 0)])],
])
def test_split_line_intersections_rejects_all_internal_contacts_and_positive_overlap(geometries):
    frame = line_overlap_frame(geometries)
    before = frame.copy(deep=True)
    with pytest.raises(ValueError, match="internal|overlap"):
        split_at_intersections(frame)
    assert_geodataframe_equal(frame, before)


@pytest.mark.parametrize("field", ["source_id", "part_no", "segment_no"])
def test_split_line_intersections_rejects_reserved_column_conflicts(field):
    frame = line_overlap_frame([LineString([(0, 0), (1, 0)])])
    frame[field] = 1
    with pytest.raises(ValueError, match="field|column"):
        split_at_intersections(frame)


@pytest.mark.parametrize("empty", [False, True])
def test_split_line_intersections_empty_and_no_crossing_keep_schema_and_identifier_types(empty):
    frame = line_overlap_frame([LineString([(0, 0), (1, 0)])], pd.Series(["甲"], dtype="string"))
    if empty:
        frame = frame.iloc[:0]
    result = split_at_intersections(frame)
    assert len(result) == len(frame)
    assert result.source_id.dtype == frame.route_key.dtype and result.crs == frame.crs
    assert result.geometry.name == frame.geometry.name
    assert result.part_no.dtype == result.segment_no.dtype == "int64"


@pytest.mark.parametrize("shape", [None, LineString(), Point(0, 0),
    LineString([(0, 0, 0), (1, 1, 1)]), LineString([(0, 0), (float("inf"), 1)])])
def test_split_line_intersections_rejects_unsupported_geometries(shape):
    with pytest.raises(ValueError):
        split_at_intersections(line_overlap_frame([shape]))


@pytest.mark.parametrize("crs", [None, "EPSG:4326"])
def test_split_line_intersections_rejects_unknown_or_geographic_crs(crs):
    with pytest.raises(ValueError, match="projected"):
        split_at_intersections(line_overlap_frame([LineString([(0, 0), (1, 0)])], crs=crs))


@pytest.mark.parametrize("ids", [[None, 2], [1, 1], ["", "b"], ["  ", "b"], [[1], [2]]])
def test_split_line_intersections_requires_unique_explicit_scalar_ids(ids):
    with pytest.raises(ValueError, match="id_field"):
        split_at_intersections(line_overlap_frame([LineString([(0, 0), (1, 0)]), LineString([(2, 0), (3, 0)])], ids))


@pytest.mark.parametrize("empty", [False, True])
def test_split_line_intersections_workflow_native_save_reload_and_failure_no_publish(tmp_path, empty):
    from osgeo import ogr
    frame = line_overlap_frame([LineString([(0, 0), (2, 0)]), LineString([(1, -1), (1, 1)])], ["甲", "乙"])
    if empty:
        frame = frame.iloc[:0]
    frame.to_file(tmp_path / "source.gpkg", layer="routes", driver="GPKG")
    original = (tmp_path / "source.gpkg").read_bytes()
    connection = {"engine_type": "nfs", "mount_path": str(tmp_path)}
    tasks = [
        {"id": "load", "operator": "load", "params": {"connection_info": connection, "path": "source.gpkg"}, "depends_on": []},
        {"id": "split", "operator": "split_line_intersections", "params": {"input_gdf": {"$ref": "load"}, "id_field": "route_key"}, "depends_on": ["load"]},
        {"id": "save", "operator": "save", "params": {"input_df": {"$ref": "split"}, "connection_info": connection, "path": "pieces.gpkg", "mode": "fail"}, "depends_on": ["split"]},
    ]
    execution = execute_workflow({"tasks": tasks})
    assert execution["status"] == "success", execution
    dataset = ogr.Open(str(tmp_path / "pieces.gpkg")); layer = dataset.GetLayer(0)
    assert layer.GetFeatureCount() == (0 if empty else 4)
    assert layer.GetSpatialRef().GetAuthorityCode(None) == "3857"
    assert [field.name for field in layer.schema] == ["source_name", "route_key", "source_id", "part_no", "segment_no"]
    expected = [[(0, 0), (1, 0)], [(1, 0), (2, 0)], [(1, -1), (1, 0)], [(1, 0), (1, 1)]]
    for index, feature in enumerate(layer):
        assert feature.GetGeometryRef().GetGeometryName() == "LINESTRING"
        assert feature.GetGeometryRef().GetCoordinateDimension() == 2
        assert feature.GetGeometryRef().GetPoints() == expected[index]
        assert feature.GetField("source_id") == feature.GetField("route_key") == ("甲" if index < 2 else "乙")
        assert feature.GetField("source_name") == f"line-{index // 2}"
        assert feature.GetField("part_no") == 1 and feature.GetField("segment_no") == index % 2 + 1
    dataset = None
    assert (tmp_path / "source.gpkg").read_bytes() == original
    bad = line_overlap_frame([LineString([(0, 0), (2, 0)]), LineString([(1, 0), (3, 0)])])
    bad.to_file(tmp_path / "bad.gpkg", driver="GPKG")
    tasks[0]["params"]["path"] = "bad.gpkg"; tasks[2]["params"]["path"] = "rejected.gpkg"
    assert execute_workflow({"tasks": tasks})["status"] == "failed"
    assert not (tmp_path / "rejected.gpkg").exists()


def test_split_line_intersections_async_discovery_workflow_only_and_required_identity(tmp_path, monkeypatch):
    import api_server
    from addp_common.workflow_runtime import ExecutionRegistry
    frame = line_overlap_frame([LineString([(0, 0), (2, 0)]), LineString([(1, -1), (1, 1)])], [17, 23])
    frame.to_file(tmp_path / "source.gpkg", driver="GPKG")
    tasks = [
        {"id": "load", "operator": "load", "params": {"connection_info": {"engine_type": "nfs", "mount_path": str(tmp_path)}, "path": "source.gpkg"}, "depends_on": []},
        {"id": "split", "operator": "split_line_intersections", "params": {"input_gdf": {"$ref": "load"}, "id_field": "route_key"}, "depends_on": ["load"]},
    ]
    monkeypatch.setattr(api_server, "executions", ExecutionRegistry())
    with api_server.app.test_client() as client:
        op = next(o for o in client.get("/api/operators").get_json()["operators"] if o["id"] == "split_line_intersections")
        assert op["display_name"] == "交点打断" and op["category_path"] == ["矢量处理", "线网处理"]
        assert op["execution_modes"] == ["workflow"] and op["effects"] == ["read"]
        assert [(p["name"], p["required"]) for p in op["parameters"]] == [("input_gdf", True), ("id_field", True), ("scope", False)]
        assert client.post("/api/operators/split_line_intersections/invoke", json={"params": {}}).status_code == 403
        response = client.post("/api/workflow", json={"workflow_def": {"tasks": tasks}, "runtime": {"tenant_id": 7, "execution_authorization": {"id": 1, "effects": ["read"]}}})
        assert response.status_code == 202
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            state = client.get("/api/executions/" + response.get_json()["execution_id"]).get_json()
            if state["status"] in ("success", "failed"):
                break
            time.sleep(.01)
        assert state["status"] == "success", state
        assert len(json.loads(state["result"])["features"]) == 4
    tasks[1]["params"].pop("id_field")
    assert execute_workflow({"tasks": tasks})["status"] == "failed"


def test_line_overlap_reports_partial_contained_reversed_and_resampled_pairs_without_source_changes():
    frame = line_overlap_frame([
        LineString([(0, 0), (4, 0)]), LineString([(2, 0), (6, 0)]),
        LineString([(1, 0), (3, 0)]), LineString([(4, 0), (2, 0), (0, 0)]),
    ], pd.Series([90, 10, 30, 20], dtype="Int64"))
    frame.index = [8, 8, 2, 9]
    before = frame.copy(deep=True)
    result = line_overlap(frame)
    assert result.columns.tolist() == ["source_id_a", "source_id_b", "overlap_length", "geometry"]
    assert list(zip(result.source_id_a, result.source_id_b, result.overlap_length)) == [
        (90, 10, 2), (90, 30, 2), (90, 20, 4), (10, 30, 1), (10, 20, 2), (30, 20, 2),
    ]
    expected = [(2, 4), (1, 3), (0, 4), (2, 3), (2, 4), (1, 3)]
    for geometry, (start, end) in zip(result.geometry, expected):
        assert geometry.geom_type == "MultiLineString" and geometry.is_valid
        assert geometry.equals(LineString([(start, 0), (end, 0)]))
    assert result.crs == frame.crs
    assert_geodataframe_equal(frame, before)


def test_line_overlap_keeps_disconnected_parts_and_excludes_contact_points():
    a = MultiLineString([[(0, 0), (3, 0)], [(0, 2), (3, 2)], [(5, 0), (5, 2)]])
    b = MultiLineString([[(1, 0), (2, 0)], [(1, 2), (2, 2)], [(4, 1), (6, 1)]])
    result = line_overlap(line_overlap_frame([a, b], ["线甲", "线乙"]))
    assert len(result) == 1 and result.overlap_length.tolist() == [2]
    assert result.geometry.iloc[0].equals(MultiLineString([[(1, 0), (2, 0)], [(1, 2), (2, 2)]]))
    assert len(result.geometry.iloc[0].geoms) == 2


def test_line_overlap_does_not_report_crossings_contacts_parallel_lines_or_self_overlap():
    frame = line_overlap_frame([
        LineString([(0, 0), (2, 0)]), LineString([(1, -1), (1, 1)]),
        LineString([(2, 0), (3, 1)]), LineString([(0, 2**-40), (0.5, 2**-40)]),
    ])
    assert line_overlap(frame).empty
    for geometry in [LineString([(0, 0), (2, 0), (0, 0)]),
                     MultiLineString([[(0, 0), (2, 0)], [(1, 0), (3, 0)]])]:
        assert line_overlap(line_overlap_frame([geometry])).empty


def test_line_overlap_counts_each_common_position_once_per_pair_for_retraced_parts():
    frame = line_overlap_frame([
        LineString([(0, 0), (4, 0), (0, 0)]),
        MultiLineString([[(1, 0), (3, 0)], [(3, 0), (1, 0)], [(2, 0), (4, 0)]]),
    ])
    result = line_overlap(frame)
    assert result.overlap_length.tolist() == [3]
    assert result.geometry.iloc[0].equals(LineString([(1, 0), (4, 0)]))


def test_line_overlap_closed_lines_and_direction_have_identical_normalized_output():
    ring = LineString([(0, 0), (2, 0), (2, 1), (0, 1), (0, 0)])
    reverse = shapely.reverse(ring)
    result = line_overlap(line_overlap_frame([ring, reverse]))
    other = line_overlap(line_overlap_frame([reverse, ring]))
    assert result.overlap_length.tolist() == [6]
    assert result.geometry.iloc[0].equals(ring)
    assert result.geometry.iloc[0].wkb == other.geometry.iloc[0].wkb


@pytest.mark.parametrize("crs", ["EPSG:3857", "EPSG:2263"])
@pytest.mark.parametrize("length", [2.0, 2**-40])
def test_line_overlap_retains_positive_lengths_in_source_units(crs, length):
    frame = line_overlap_frame([LineString([(0, 0), (length, 0)])] * 3, crs=crs)
    result = line_overlap(frame)
    assert list(zip(result.source_id_a, result.source_id_b)) == [(0, 1), (0, 2), (1, 2)]
    assert result.overlap_length.tolist() == [length] * 3
    assert result.crs == frame.crs


@pytest.mark.parametrize("ids", [pd.Series([2**60, 2**60+1], dtype="Int64"), pd.Series(["甲", "乙"], dtype="string")])
def test_line_overlap_retains_identifier_dtypes_and_empty_schema(ids):
    frame = line_overlap_frame([LineString([(0, 0), (1, 0)])] * 2, ids)
    result = line_overlap(frame)
    assert result.source_id_a.tolist() == ids.iloc[:1].tolist()
    assert result.source_id_b.tolist() == ids.iloc[1:].tolist()
    for source in [frame.iloc[:0], frame.iloc[:1], frame]:
        output = line_overlap(source)
        assert output.source_id_a.dtype == output.source_id_b.dtype == ids.dtype
        assert output.overlap_length.dtype == np.dtype("float64")
        assert output.crs == frame.crs


@pytest.mark.parametrize("ids", [[1, 1], [None, 1], [" ", "x"], [[1], [2]]])
def test_line_overlap_rejects_invalid_source_ids(ids):
    with pytest.raises(ValueError, match="identifiers"):
        line_overlap(line_overlap_frame([LineString([(0, 0), (1, 0)])] * 2, ids))


@pytest.mark.parametrize("id_field", [None, "missing", "boundary", ""])
def test_line_overlap_requires_explicit_nongeometry_identity(id_field):
    with pytest.raises(ValueError, match="id_field"):
        line_overlap(line_overlap_frame([LineString([(0, 0), (1, 0)])]), id_field)


@pytest.mark.parametrize("crs", [None, "EPSG:4326"])
def test_line_overlap_rejects_unknown_or_geographic_crs(crs):
    with pytest.raises(ValueError, match="projected"):
        line_overlap(line_overlap_frame([], crs=crs))


@pytest.mark.parametrize("geometry", [None, LineString(), Point(0, 0), box(0, 0, 1, 1),
    shapely.from_wkt("MULTILINESTRING (EMPTY, (0 0, 1 0))"),
    LineString([(0, 0), (0, 0)]), LineString([(0, 0, 1), (1, 0, 1)]),
    shapely.from_wkt("LINESTRING M (0 0 1, 1 0 1)"), LineString([(0, 0), (float("inf"), 1)])])
def test_line_overlap_rejects_unsupported_geometry(geometry):
    with pytest.raises(ValueError):
        line_overlap(line_overlap_frame([geometry]))


def test_line_overlap_rejects_nonfinite_output_length():
    frame = line_overlap_frame([LineString([(-1e308, 0), (1e308, 0)])] * 2)
    with np.errstate(over="ignore", invalid="ignore"):
        with pytest.raises(ValueError, match="finite positive"):
            line_overlap(frame)


@pytest.mark.parametrize("length", [2**-600, 1e200])
def test_line_overlap_retains_representable_lengths_without_squared_distance_underflow_or_overflow(length):
    frame = line_overlap_frame([LineString([(0, 0), (length, 0)])] * 2)
    result = line_overlap(frame)
    assert result.overlap_length.tolist() == [length]
    assert list(result.geometry.iloc[0].geoms[0].coords) == [(0, 0), (length, 0)]


def test_line_overlap_matches_independent_interval_pair_oracle():
    intervals = [(0, 2), (1, 4), (4, 5), (2, 3), (0, 2), (10, 12)]
    expected = [(a, b, min(y, v)-max(x, u))
                for a, (x, y) in enumerate(intervals) for b, (u, v) in enumerate(intervals)
                if a < b and min(y, v) > max(x, u)]
    result = line_overlap(line_overlap_frame([LineString([(x, 0), (y, 0)]) for x, y in intervals]))
    assert list(zip(result.source_id_a, result.source_id_b, result.overlap_length)) == expected


def test_line_overlap_discovery_workflow_save_reload_and_native_coordinates(tmp_path):
    import api_server
    from osgeo import ogr
    tiny = 2**-40
    source = line_overlap_frame([
        LineString([(0, 0), (4, 0)]), LineString([(3, 0), (1, 0)]),
        LineString([(0, 10), (tiny, 10)]), LineString([(tiny, 10), (0, 10)]),
    ], ["线甲", "线乙", "微甲", "微乙"])
    source.to_file(tmp_path / "lines.gpkg", layer="routes", driver="GPKG")
    before = (tmp_path / "lines.gpkg").read_bytes()
    connection = {"engine_type": "nfs", "mount_path": str(tmp_path)}
    tasks = [
        {"id": "load", "operator": "load", "params": {"connection_info": connection, "path": "lines.gpkg"}, "depends_on": []},
        {"id": "check", "operator": "check_line_overlap", "params": {"input_gdf": {"$ref": "load"}, "id_field": "route_key"}, "depends_on": ["load"]},
        {"id": "save", "operator": "save", "params": {"input_df": {"$ref": "check"}, "connection_info": connection, "path": "overlaps.gpkg", "mode": "fail"}, "depends_on": ["check"]},
    ]
    execution = execute_workflow({"tasks": tasks})
    assert execution["status"] == "success", execution.get("error")
    dataset = ogr.Open(str(tmp_path / "overlaps.gpkg"))
    layer = dataset.GetLayer(0)
    assert layer.GetFeatureCount() == 2 and layer.GetSpatialRef().GetAuthorityCode(None) == "3857"
    expected = [("线甲", "线乙", 2, [(1, 0), (3, 0)]), ("微甲", "微乙", tiny, [(0, 10), (tiny, 10)])]
    for feature, (a, b, length, points) in zip(layer, expected):
        geometry = feature.GetGeometryRef()
        assert geometry.GetGeometryName() == "MULTILINESTRING" and geometry.IsValid()
        assert geometry.GetGeometryCount() == 1
        assert (feature.GetField("source_id_a"), feature.GetField("source_id_b"), feature.GetField("overlap_length")) == (a, b, length)
        assert geometry.Length() == length
        assert [p[:2] for p in geometry.GetGeometryRef(0).GetPoints()] == points
    dataset = None
    assert (tmp_path / "lines.gpkg").read_bytes() == before
    with api_server.app.test_client() as client:
        op = next(o for o in client.get("/api/operators").get_json()["operators"] if o["id"] == "check_line_overlap")
        assert op["display_name"] == "线叠检查" and op["category_path"] == ["矢量处理", "拓扑检查"]
        assert op["execution_modes"] == ["workflow"] and op["effects"] == ["read"]
        assert [(p["name"], p["param_type"], p["required"]) for p in op["parameters"]] == [("input_gdf", "input", True), ("id_field", "param", True)]
        assert [(p["name"], p["type"], p["is_default"]) for p in op["output_ports"]] == [("result", "geodataframe", True)]
        assert client.post("/api/operators/check_line_overlap/invoke", json={"params": {}}).status_code == 403


def test_line_overlap_invalid_identity_fails_before_save(tmp_path):
    source = line_overlap_frame([LineString([(0, 0), (2, 0)])] * 2, ["重复", "重复"])
    source.to_file(tmp_path / "lines.gpkg", driver="GPKG")
    connection = {"engine_type": "nfs", "mount_path": str(tmp_path)}
    result = execute_workflow({"tasks": [
        {"id": "load", "operator": "load", "params": {"connection_info": connection, "path": "lines.gpkg"}, "depends_on": []},
        {"id": "check", "operator": "check_line_overlap", "params": {"input_gdf": {"$ref": "load"}, "id_field": "route_key"}, "depends_on": ["load"]},
        {"id": "save", "operator": "save", "params": {"input_df": {"$ref": "check"}, "connection_info": connection, "path": "invalid-result.gpkg", "mode": "fail"}, "depends_on": ["check"]},
    ]})
    assert result["status"] == "failed" and result["error_code"] == "INVALID_PARAMS"
    assert result["diagnostic"] == {
        "schema_version": "addp.execution-failure/v3", "stage": "execution",
        "reason": "operator_input_invalid",
        "operator": {"node_index": 2, "operator_id": "check_line_overlap", "parameter": "input_gdf"},
    }
    assert not (tmp_path / "invalid-result.gpkg").exists()


def self_overlap(frame, id_field="route_key"):
    return get_operator("check_line_self_overlap")(input_gdf=frame, id_field=id_field)


@pytest.mark.parametrize("geometry, expected", [
    (LineString([(0, 0), (4, 0), (0, 0)]), MultiLineString([[(0, 0), (4, 0)]])),
    (LineString([(0, 0), (4, 0), (1, 0)]), MultiLineString([[(1, 0), (4, 0)]])),
    (LineString([(0, 0), (0, 0), (4, 0), (4, 0), (0, 0)]), MultiLineString([[(0, 0), (4, 0)]])),
    (MultiLineString([[(0, 0), (4, 0)], [(3, 0), (1, 0)], [(1, 0), (3, 0)]]), MultiLineString([[(1, 0), (3, 0)]])),
    (MultiLineString([[(0, 0), (4, 0)], [(2, 0), (6, 0)], [(0, 2), (4, 2)], [(2, 2), (6, 2)], [(6, 0), (7, 1)]]), MultiLineString([[(2, 0), (4, 0)], [(2, 2), (4, 2)]])),
    (LineString([(0, 0), (2, 0), (2, 2), (0, 2), (0, 0), (2, 0)]), MultiLineString([[(0, 0), (2, 0)]])),
])
def test_line_self_overlap_reports_original_retracing_and_multipart_positions(geometry, expected):
    result = self_overlap(line_overlap_frame([geometry], ["路线甲"]))
    assert result.columns.tolist() == ["source_id", "overlap_length", "geometry"]
    assert result.source_id.tolist() == ["路线甲"]
    assert result.geometry.iloc[0].geom_type == "MultiLineString"
    assert result.geometry.iloc[0].equals(expected)
    assert result.overlap_length.tolist() == [expected.length]


@pytest.mark.parametrize("geometry", [
    LineString([(0, 0), (1, 0), (2, 0)]),
    LineString([(0, 0), (0, 0), (2, 0)]),
    LineString([(0, 0), (2, 2), (0, 2), (2, 0)]),
    LineString([(0, 0), (2, 0), (2, 2), (0, 2), (0, 0)]),
    MultiLineString([[(0, 0), (2, 0)], [(1, 0), (1, 1)], [(2, 0), (3, 0)]]),
    MultiLineString([[(0, 0), (2, 0)], [(0, 2**-40), (2, 2**-40)]]),
])
def test_line_self_overlap_ignores_points_closed_seams_zero_segments_and_near_lines(geometry):
    assert self_overlap(line_overlap_frame([geometry])).empty



@pytest.mark.parametrize("visits", [2, 4, 8])
def test_line_self_overlap_deduplicates_only_generated_intersections(visits, monkeypatch):
    from operators import geometric_operators as operators
    original_intersection = operators.shapely.intersection
    original_union = operators.shapely.union_all
    compared, merged = [], []

    def intersect(a, b):
        compared.extend((left.wkb, right.wkb) for left, right in zip(np.atleast_1d(a), np.atleast_1d(b)))
        return original_intersection(a, b)

    def merge(parts, *args, **kwargs):
        merged.append(len(parts))
        return original_union(parts, *args, **kwargs)

    monkeypatch.setattr(operators.shapely, "intersection", intersect)
    monkeypatch.setattr(operators.shapely, "union_all", merge)
    source = MultiLineString([[(0, 0), (8, 0)] if i % 2 else [(8, 0), (0, 0)] for i in range(visits)])
    frame = line_overlap_frame([source, source], ["first", "second"])
    before = frame.copy(deep=True)
    result = self_overlap(frame)
    # Every original pair must still be examined, but repeated positive pieces
    # must not reach GEOS union as a quadratic duplicate geometry collection.
    assert len(compared) == visits * (visits - 1)
    assert merged == [1, 1]
    assert result.source_id.tolist() == ["first", "second"]
    assert result.overlap_length.tolist() == [8, 8]
    assert all(list(g.geoms[0].coords) == [(0, 0), (8, 0)] for g in result.geometry)
    assert_geodataframe_equal(frame, before)


@pytest.mark.parametrize("count, expected_sizes", [(64, [2016]), (65, [2048, 32])])
def test_line_self_overlap_batch2048_preserves_all_original_pair_order(count, expected_sizes, monkeypatch):
    from operators import geometric_operators as operators
    original = operators.shapely.intersection
    segments = [LineString([(0, 0), (i + 1, 0)]) for i in range(count)]
    compared, sizes = [], []

    def intersect(left, right):
        assert isinstance(left, np.ndarray) and isinstance(right, np.ndarray)
        sizes.append(len(left))
        compared.extend((int(a.coords[-1][0]) - 1, int(b.coords[-1][0]) - 1)
                        for a, b in zip(left, right))
        return original(left, right)

    monkeypatch.setattr(operators.shapely, "intersection", intersect)
    result = list(operators._self_overlap_intersections(segments))
    assert sizes == expected_sizes
    assert compared == [(a, b) for a in range(count) for b in range(a + 1, count)]
    assert [list(part.coords) for part in result] == [[(0, 0), (i + 1, 0)] for i in range(count - 1)]


def test_line_self_overlap_batch2048_exact_limit_and_no_empty_tail(monkeypatch):
    from operators import geometric_operators as operators
    original = operators.shapely.intersection
    # C(64,2) + 32 disjoint duplicate pairs = exactly 2048 bbox pairs.
    segments = [LineString([(0, 0), (1, 0)])] * 64
    segments += [LineString([(3 * i, 0), (3 * i + 1, 0)]) for i in range(1, 33) for _ in range(2)]
    sizes = []

    def intersect(left, right):
        assert isinstance(left, np.ndarray) and isinstance(right, np.ndarray)
        sizes.append(len(left))
        return original(left, right)

    monkeypatch.setattr(operators.shapely, "intersection", intersect)
    assert len(list(operators._self_overlap_intersections(segments))) == 33
    assert sizes == [2048]
    assert list(operators._self_overlap_intersections([])) == []
    assert list(operators._self_overlap_intersections(segments[:1])) == []
    assert sizes == [2048]


def test_line_self_overlap_batch2048_dedup_spans_batches_but_not_sources(monkeypatch):
    from operators import geometric_operators as operators
    original_intersection, original_union = operators.shapely.intersection, operators.shapely.union_all
    sizes, merged = [], []

    def intersect(left, right):
        assert isinstance(left, np.ndarray) and isinstance(right, np.ndarray)
        sizes.append(len(left))
        return original_intersection(left, right)

    def merge(parts, *args, **kwargs):
        merged.append(len(parts))
        return original_union(parts, *args, **kwargs)

    monkeypatch.setattr(operators.shapely, "intersection", intersect)
    monkeypatch.setattr(operators.shapely, "union_all", merge)
    source = MultiLineString([[(0, 0), (8, 0)] if i % 2 else [(8, 0), (0, 0)] for i in range(65)])
    frame = line_overlap_frame([source, source], ["first", "second"])
    before = frame.copy(deep=True)
    result = self_overlap(frame)
    assert sizes == [2048, 32, 2048, 32]
    assert merged == [1, 1]
    assert result.source_id.tolist() == ["first", "second"]
    assert result.overlap_length.tolist() == [8, 8]
    assert all(list(shape.geoms[0].coords) == [(0, 0), (8, 0)] for shape in result.geometry)
    assert_geodataframe_equal(frame, before)


def test_line_self_overlap_batch2048_failure_does_not_fallback_or_save(tmp_path, monkeypatch):
    from operators import geometric_operators as operators
    source = line_overlap_frame([MultiLineString([[(0, 0), (8, 0)]] * 65)])
    source.to_file(tmp_path / "source.gpkg", layer="routes", driver="GPKG")
    before = (tmp_path / "source.gpkg").read_bytes()
    original, calls = operators.shapely.intersection, []

    def intersect(left, right):
        assert isinstance(left, np.ndarray) and isinstance(right, np.ndarray)
        calls.append(len(left))
        if len(calls) == 2:
            raise RuntimeError("batch intersection failure")
        return original(left, right)

    monkeypatch.setattr(operators.shapely, "intersection", intersect)
    connection = {"engine_type": "nfs", "mount_path": str(tmp_path)}
    tasks = [
        {"id": "load", "operator": "load", "params": {"connection_info": connection, "path": "source.gpkg"}, "depends_on": []},
        {"id": "check", "operator": "check_line_self_overlap", "params": {"input_gdf": {"$ref": "load"}, "id_field": "route_key"}, "depends_on": ["load"]},
        {"id": "save", "operator": "save", "params": {"input_df": {"$ref": "check"}, "connection_info": connection, "path": "rejected.gpkg", "mode": "fail"}, "depends_on": ["check"]},
    ]
    result = execute_workflow({"tasks": tasks})
    assert result["status"] == "failed" and result["error_code"] == "EXECUTION_FAILED"
    assert "diagnostic" not in result and "batch intersection failure" in result["error"]
    assert calls == [2048, 32]
    assert not (tmp_path / "rejected.gpkg").exists()
    assert (tmp_path / "source.gpkg").read_bytes() == before


def test_line_self_overlap_exact_collection_retains_tiny_differences_and_segmentation():
    tiny = 2**-40
    source = MultiLineString([[(0, 0), (4, 0)], [(4, 0), (0, 0)],
        [(0, 0), (1, 0), (4, 0)], [(0, tiny), (tiny, tiny)], [(tiny, tiny), (0, tiny)]])
    result = self_overlap(line_overlap_frame([source]))
    shape = result.geometry.iloc[0]
    assert result.overlap_length.tolist() == [4 + tiny]
    assert shape.covers(Point(.5, 0)) and shape.covers(Point(3, 0))
    assert shape.covers(Point(tiny / 2, tiny))
    assert not shape.covers(Point(.5, tiny))
    assert sorted([list(part.coords) for part in shape.geoms]) == [
        [(0, 0), (1, 0)], [(0, tiny), (tiny, tiny)], [(1, 0), (4, 0)]]


def test_line_self_overlap_does_not_compare_different_features():
    frame = line_overlap_frame([LineString([(0, 0), (4, 0)])] * 2)
    assert self_overlap(frame).empty
    assert line_overlap(frame).overlap_length.tolist() == [4]


@pytest.mark.parametrize("visits", [2, 3, 7])
def test_line_self_overlap_metric_is_independent_of_visits_direction_and_vertex_sampling(visits):
    coarse = MultiLineString([[(0, 0), (4, 0)]] * visits)
    sampled = MultiLineString([[(4, 0), (3, 0), (1, 0), (0, 0)]] * visits)
    for geometry in [coarse, sampled, shapely.reverse(sampled)]:
        result = self_overlap(line_overlap_frame([geometry]))
        assert result.overlap_length.tolist() == [4]
        assert result.geometry.iloc[0].equals(LineString([(0, 0), (4, 0)]))
    a = self_overlap(line_overlap_frame([sampled])).geometry.iloc[0]
    b = self_overlap(line_overlap_frame([shapely.reverse(sampled)])).geometry.iloc[0]
    assert a.wkb == b.wkb


@pytest.mark.parametrize("crs", ["EPSG:3857", "EPSG:2263"])
@pytest.mark.parametrize("length", [2**-40, 1e-200, 1e200])
def test_line_self_overlap_preserves_finite_lengths_and_source_units(crs, length):
    result = self_overlap(line_overlap_frame([LineString([(0, 0), (length, 0), (0, 0)])], crs=crs))
    assert result.overlap_length.tolist() == [length]
    assert result.crs.to_string() == crs
    assert list(result.geometry.iloc[0].geoms[0].coords) == [(0, 0), (length, 0)]


@pytest.mark.parametrize("ids", [pd.Series(["丙", "甲", "乙"], dtype="string"), pd.Series([90, 3, 12], dtype="Int64")])
def test_line_self_overlap_preserves_identity_order_schema_and_source(ids):
    frame = line_overlap_frame([LineString([(0, 0), (2, 0), (0, 0)]), LineString([(0, 0), (1, 0)]),
                                LineString([(0, 2), (1, 2), (0, 2)])], ids)
    frame.index = [8, 8, 1]
    before = frame.copy(deep=True)
    result = self_overlap(frame)
    assert result.source_id.tolist() == ids.iloc[[0, 2]].tolist()
    assert result.source_id.dtype == ids.dtype
    assert result.crs == frame.crs and result.overlap_length.tolist() == [2, 1]
    assert_geodataframe_equal(frame, before)
    for source in [frame.iloc[:0], frame.iloc[[1]]]:
        output = self_overlap(source)
        assert output.empty and output.source_id.dtype == ids.dtype
        assert output.overlap_length.dtype == "float64" and output.crs == frame.crs
        assert output.columns.tolist() == result.columns.tolist()


@pytest.mark.parametrize("ids", [[None, "b"], ["a", "a"], [" ", "b"], [[1], [2]]])
def test_line_self_overlap_rejects_invalid_ids(ids):
    with pytest.raises(ValueError, match="identifier"):
        self_overlap(line_overlap_frame([LineString([(0, 0), (1, 0)])] * 2, ids))


@pytest.mark.parametrize("field", [None, "", "missing", "boundary"])
def test_line_self_overlap_requires_explicit_id(field):
    with pytest.raises(ValueError, match="id_field"):
        self_overlap(line_overlap_frame([LineString([(0, 0), (1, 0)])]), field)


@pytest.mark.parametrize("crs", [None, "EPSG:4326"])
def test_line_self_overlap_rejects_nonprojected_crs(crs):
    with pytest.raises(ValueError, match="projected CRS"):
        self_overlap(line_overlap_frame([], crs=crs))


@pytest.mark.parametrize("geometry", [None, LineString(), Point(0, 0), box(0, 0, 1, 1),
    LineString([(0, 0), (0, 0)]), LineString([(0, 0, 1), (1, 0, 2)]),
    shapely.from_wkt("LINESTRING M (0 0 1, 1 0 2)"), LineString([(0, 0), (np.inf, 1)])])
def test_line_self_overlap_rejects_unsupported_geometry(geometry):
    with pytest.raises(ValueError):
        self_overlap(line_overlap_frame([geometry]))


def test_line_self_overlap_rejects_nonfinite_total_length():
    frame = line_overlap_frame([MultiLineString([[(0, 0), (1e308, 0)], [(1e308, 0), (0, 0)],
                                                  [(0, 2), (1e308, 2)], [(1e308, 2), (0, 2)]])])
    with np.errstate(over="ignore", invalid="ignore"):
        with pytest.raises(ValueError, match="finite positive Float64"):
            self_overlap(frame)


def test_line_self_overlap_matches_independent_integer_coverage_oracle():
    # Half-open integer cells give independent exact coverage counts. No GEOS
    # overlay or production function is used to construct the expected union.
    intervals = [(0, 5), (3, 8), (8, 6), (10, 12), (12, 10), (20, 21)]
    expected_cells = [x for x in range(21) if sum(min(a, b) <= x < max(a, b) for a, b in intervals) >= 2]
    source = MultiLineString([[(a, 0), (b, 0)] for a, b in intervals])
    result = self_overlap(line_overlap_frame([source]))
    assert result.overlap_length.tolist() == [len(expected_cells)]
    geometry = result.geometry.iloc[0]
    for x in range(21):
        assert geometry.covers(Point(x + .5, 0)) == (x in expected_cells)


@pytest.mark.parametrize("empty", [False, True])
def test_line_self_overlap_workflow_native_save_reload_and_discovery(tmp_path, empty):
    import api_server
    from osgeo import ogr
    tiny = 2**-40
    source = line_overlap_frame([LineString([(0, 0), (4, 0), (1, 0)]),
        MultiLineString([[(0, 10), (tiny, 10)], [(tiny, 10), (0, 10)]])], ["往返", "微线"])
    if empty:
        source = line_overlap_frame([LineString([(0, 0), (4, 0)])], ["正常"])
    source.to_file(tmp_path / "source.gpkg", layer="routes", driver="GPKG")
    original = (tmp_path / "source.gpkg").read_bytes()
    connection = {"engine_type": "nfs", "mount_path": str(tmp_path)}
    tasks = [
        {"id": "load", "operator": "load", "params": {"connection_info": connection, "path": "source.gpkg"}, "depends_on": []},
        {"id": "check", "operator": "check_line_self_overlap", "params": {"input_gdf": {"$ref": "load"}, "id_field": "route_key"}, "depends_on": ["load"]},
        {"id": "save", "operator": "save", "params": {"input_df": {"$ref": "check"}, "connection_info": connection, "path": "self-overlaps.gpkg", "mode": "fail"}, "depends_on": ["check"]},
    ]
    result = execute_workflow({"tasks": tasks})
    assert result["status"] == "success", result.get("error")
    dataset = ogr.Open(str(tmp_path / "self-overlaps.gpkg"))
    layer = dataset.GetLayer(0)
    assert layer.GetFeatureCount() == (0 if empty else 2)
    assert layer.GetSpatialRef().GetAuthorityCode(None) == "3857"
    assert [field.name for field in layer.schema] == ["source_id", "overlap_length"]
    # Empty GeoDataFrames carry a geometry dtype, not a concrete subtype.
    # The existing save path preserves fields/CRS and declares generic geometry.
    assert layer.GetGeomType() == (ogr.wkbUnknown if empty else ogr.wkbMultiLineString)
    if not empty:
        expected = [("往返", 3, [(1, 0), (4, 0)]), ("微线", tiny, [(0, 10), (tiny, 10)])]
        for feature, (identity, length, points) in zip(layer, expected):
            geometry = feature.GetGeometryRef()
            assert feature.GetField("source_id") == identity and feature.GetField("overlap_length") == length
            assert geometry.GetGeometryName() == "MULTILINESTRING" and geometry.IsValid()
            assert geometry.GetGeometryCount() == 1 and geometry.Length() == length
            assert [p[:2] for p in geometry.GetGeometryRef(0).GetPoints()] == points
    dataset = None
    assert (tmp_path / "source.gpkg").read_bytes() == original
    with api_server.app.test_client() as client:
        op = next(o for o in client.get("/api/operators").get_json()["operators"] if o["id"] == "check_line_self_overlap")
        assert op["display_name"] == "线内自叠" and op["category_path"] == ["矢量处理", "拓扑检查"]
        assert op["execution_modes"] == ["workflow"] and op["effects"] == ["read"]
        assert [(p["name"], p["param_type"], p["required"]) for p in op["parameters"]] == [("input_gdf", "input", True), ("id_field", "param", True)]
        assert [(p["name"], p["type"], p["is_default"]) for p in op["output_ports"]] == [("result", "geodataframe", True)]
        assert client.post("/api/operators/check_line_self_overlap/invoke", json={"params": {}}).status_code == 403
    tasks[1]["params"]["id_field"] = "missing_id"
    tasks[2]["params"]["path"] = "rejected.gpkg"
    assert execute_workflow({"tasks": tasks})["status"] == "failed"
    assert not (tmp_path / "rejected.gpkg").exists()


def polygonize(frame):
    return get_operator("polygonize")(input_gdf=frame)


@pytest.mark.parametrize("crs", [None, "EPSG:4326", "EPSG:3857"])
def test_polygonize_nodes_intersections_and_removes_duplicate_edges_without_mutating_source(crs):
    frame = lines([
        box(0, 0, 4, 4).boundary,
        LineString([(-1, 2), (5, 2)]),
        MultiLineString([[(2, -1), (2, 5)], [(0, 0), (4, 0)]]),
        LineString([(4, 0), (0, 0)]),
    ], crs=crs)
    frame.index = [3, 5, 7, 9]
    original = frame.copy(deep=True)
    result = polygonize(frame)
    faces, diagnostics = result["result"], result["diagnostics"]

    assert len(faces) == 4
    assert set(faces.geom_type) == {"Polygon"}
    assert sorted(shapely.area(faces.geometry.array)) == [4, 4, 4, 4]
    assert shapely.union_all(faces.geometry.array).equals(box(0, 0, 4, 4))
    assert faces.columns.tolist() == ["geometry"]
    assert diagnostics.columns.tolist() == ["issue_type", "geometry"]
    assert diagnostics.issue_type.tolist() == ["dangle"] * 4
    assert shapely.length(diagnostics.geometry.array).sum() == 4
    assert faces.crs == diagnostics.crs == frame.crs
    assert_geodataframe_equal(frame, original)


def test_polygonize_keeps_nested_faces_holes_and_self_crossing_regions():
    nested = polygonize(lines([box(0, 0, 4, 4).boundary, box(1, 1, 3, 3).boundary]))
    faces = nested["result"]
    assert sorted(faces.area) == [4, 12]
    assert sorted(len(face.interiors) for face in faces.geometry) == [0, 1]
    assert nested["diagnostics"].empty
    assert faces.geometry.iloc[0].intersection(faces.geometry.iloc[1]).area == 0

    crossing = polygonize(lines([LineString([(0, 0), (2, 2), (0, 2), (2, 0), (0, 0)])]))
    assert sorted(crossing["result"].area) == [1, 1]
    assert crossing["result"].is_valid.all()
    assert crossing["diagnostics"].empty


def test_polygonize_distinguishes_cut_edges_from_dangles():
    bridge = LineString([(1, 0.5), (2, 0.5)])
    tail = LineString([(3, 0.5), (4, 0.5)])
    result = polygonize(lines([box(0, 0, 1, 1).boundary, box(2, 0, 3, 1).boundary, bridge, tail]))
    assert len(result["result"]) == 2
    diagnostics = result["diagnostics"]
    assert set(diagnostics.issue_type) == {"cut_edge", "dangle"}
    assert diagnostics.loc[diagnostics.issue_type == "cut_edge"].geometry.iloc[0].equals(bridge)
    assert diagnostics.loc[diagnostics.issue_type == "dangle"].geometry.iloc[0].equals(tail)


def test_polygonize_does_not_snap_small_gaps_or_invent_polygons():
    open_ring = LineString([(0, 0), (1, 0), (1, 1), (0, 1), (0, 1e-12)])
    result = polygonize(lines([open_ring]))
    assert result["result"].empty
    assert result["diagnostics"].issue_type.tolist() == ["dangle"]
    assert result["diagnostics"].geometry.iloc[0].equals(open_ring)


@pytest.mark.parametrize("crs", [None, "EPSG:4326", "EPSG:3857"])
def test_polygonize_preserves_empty_schema_and_crs(crs):
    result = polygonize(lines([], crs=crs))
    assert result["result"].empty and result["diagnostics"].empty
    assert result["result"].columns.tolist() == ["geometry"]
    assert result["diagnostics"].columns.tolist() == ["issue_type", "geometry"]
    assert str(result["diagnostics"].issue_type.dtype) == "string"
    for output in result.values():
        assert output.crs == lines([], crs=crs).crs
        assert json.loads(output.to_json())["features"] == []


@pytest.mark.parametrize("geometry,message", [
    (None, "non-null"), (LineString(), "empty"), (Point(0, 0), "LineString"),
    (Polygon([(0, 0), (1, 0), (1, 1)]), "LineString"),
    (LineString([(0, 0, 1), (1, 1, 2)]), "two-dimensional"),
    (shapely.from_wkt("LINESTRING M (0 0 1, 1 1 2)"), "two-dimensional"),
    (LineString([(0, 0), (np.inf, 1)]), "finite coordinates"),
    (LineString([(0, 0), (0, 0)]), "valid lines"),
    (shapely.from_wkt("MULTILINESTRING (EMPTY, (0 0, 1 1))"), "empty"),
])
def test_polygonize_rejects_unsupported_rows_without_silently_dropping_them(geometry, message):
    from operators.geometric_operators import PolygonizeInputError

    with pytest.raises(PolygonizeInputError, match=message):
        polygonize(lines([box(0, 0, 1, 1).boundary, geometry]))


@pytest.mark.parametrize("frame", [None, pd.DataFrame({"x": [1]}), gpd.GeoDataFrame({"x": [1]})])
def test_polygonize_requires_an_active_geodataframe(frame):
    from operators.geometric_operators import PolygonizeInputError

    with pytest.raises(PolygonizeInputError, match="GeoDataFrame|geometry"):
        polygonize(frame)


@pytest.mark.parametrize(
    "fault, error_code",
    [
        (None, "INVALID_PARAMS"),
        ("get_parts", "EXECUTION_FAILED"),
        ("is_valid", "EXECUTION_FAILED"),
        ("node", "EXECUTION_FAILED"),
        ("polygonize_full", "EXECUTION_FAILED"),
    ],
)
def test_polygonize_diagnostic_classification_blocks_both_dependent_saves(monkeypatch, fault, error_code):
    import workflow_engine
    from addp_common.workflow_runtime import WorkflowExecutionError

    source = lines([Point(0, 0)] if fault is None else [box(0, 0, 1, 1).boundary], crs=None)
    original = workflow_engine.get_operator
    saves = []

    def native_failure(*args, **kwargs):
        # Even an exception with the same text as a deliberate rejection stays an execution failure.
        raise ValueError("input_gdf requires valid lines with finite coordinates")

    def resolve(name):
        if name == "load":
            return lambda **params: source
        if name == "save":
            return lambda **params: saves.append(params)
        operator = original(name)
        if name != "polygonize" or fault is None:
            return operator

        def invoke(**params):
            with monkeypatch.context() as scope:
                scope.setattr(shapely, fault, native_failure)
                return operator(**params)

        return invoke

    monkeypatch.setattr(workflow_engine, "get_operator", resolve)
    tasks = [
        {"id": "build", "operator": "polygonize", "params": {"input_gdf": {"$ref": "load"}}, "depends_on": ["load"]},
        {"id": "load", "operator": "load", "params": {}, "depends_on": []},
    ]
    for port in ("result", "diagnostics"):
        tasks.append({
            "id": f"save_{port}", "operator": "save", "depends_on": ["build"],
            "params": {"input_df": {"$ref": "build", "port": port}},
        })
    with pytest.raises(WorkflowExecutionError) as error:
        workflow_engine.GeoPythonWorkflowRunner(language="en").execute({"tasks": tasks})
    assert error.value.error_code == error_code
    assert not saves
    expected = {
        "schema_version": "addp.execution-failure/v3",
        "stage": "execution",
        "reason": "operator_input_invalid",
        "operator": {"node_index": 1, "operator_id": "polygonize", "parameter": "input_gdf"},
    }
    assert error.value.diagnostic == (expected if fault is None else None)


def test_polygonize_metadata_declares_existing_workflow_ports_only():
    operator = next(op for op in list_operators() if op["id"] == "polygonize")
    assert operator["display_name"] == "拓扑构面"
    assert operator["category_path"] == ["矢量处理", "线网处理"]
    assert operator["execution_modes"] == ["workflow"]
    assert operator["effects"] == ["read"]
    assert [(p["name"], p["param_type"]) for p in operator["parameters"]] == [("input_gdf", "input")]
    assert [(p["name"], p["type"], p["is_default"]) for p in operator["output_ports"]] == [
        ("result", "geodataframe", True), ("diagnostics", "geodataframe", False),
    ]


def test_polygonize_runtime_discovery_async_execution_and_direct_rejection(monkeypatch):
    import api_server
    from addp_common.workflow_runtime import ExecutionRegistry

    monkeypatch.setattr(api_server, "executions", ExecutionRegistry())
    with api_server.app.test_client() as client:
        response = client.get("/api/operators")
        assert response.status_code == 200
        discovered = next(op for op in response.get_json()["operators"] if op["id"] == "polygonize")
        assert [p["name"] for p in discovered["output_ports"]] == ["result", "diagnostics"]
        source = lines([box(0, 0, 1, 1).boundary], crs="EPSG:4326")
        params = {"input_gdf": json.loads(source.to_json())}
        assert client.post("/api/operators/polygonize/invoke", json={"params": params}).status_code == 403
        workflow = {"tasks": [{"id": "faces", "operator": "polygonize", "params": params, "depends_on": []}]}
        assert client.post("/api/workflow", json={"workflow_def": workflow}).status_code == 400
        submitted = client.post("/api/workflow", json={"workflow_def": workflow, "runtime": {
            "tenant_id": 7, "execution_authorization": {"id": 1, "effects": ["read"]},
        }})
        assert submitted.status_code == 202
        execution_id = submitted.get_json()["execution_id"]
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            response = client.get("/api/executions/" + execution_id)
            assert response.status_code == 200
            state = response.get_json()
            if state["status"] in {"success", "failed"}:
                break
            time.sleep(0.01)
        assert state["status"] == "success", state
        preview = json.loads(state["result"])
        assert len(preview["result"]["features"]) == 1
        assert preview["diagnostics"]["features"] == []


@pytest.mark.parametrize("has_closed_region", [True, False])
def test_polygonize_workflow_saves_and_reloads_both_ports(tmp_path, has_closed_region):
    source = lines([box(0, 0, 1, 1).boundary, LineString([(1, 1), (2, 2)])] if has_closed_region
                   else [LineString([(0, 0), (1, 1)])])
    source.to_file(tmp_path / "source.geojson", driver="GeoJSON")
    source_bytes = (tmp_path / "source.geojson").read_bytes()
    connection = {"engine_type": "nfs", "mount_path": str(tmp_path)}
    tasks = [
        {"id": "load", "operator": "load", "params": {"connection_info": connection, "path": "source.geojson"}, "depends_on": []},
        {"id": "build", "operator": "polygonize", "params": {"input_gdf": {"$ref": "load"}}, "depends_on": ["load"]},
    ]
    for name, reference in [("faces", {"$ref": "build"}), ("diagnostics", {"$ref": "build", "port": "diagnostics"})]:
        tasks.append({"id": name, "operator": "save", "params": {
            "input_df": reference, "connection_info": connection, "path": name + ".geojson",
        }, "depends_on": ["build"]})
    execution = execute_workflow({"tasks": tasks})
    assert execution["status"] == "success", execution.get("error")
    preview = json.loads(execution["all_results"]["build"])
    assert set(preview) == {"result", "diagnostics"}
    faces = gpd.read_file(tmp_path / "faces.geojson")
    diagnostics = gpd.read_file(tmp_path / "diagnostics.geojson")
    assert len(faces) == int(has_closed_region)
    assert len(diagnostics) == 1
    assert diagnostics.issue_type.tolist() == ["dangle"]
    assert faces.crs == diagnostics.crs == source.crs
    assert (tmp_path / "source.geojson").read_bytes() == source_bytes
    if has_closed_region:
        assert faces.geometry.iloc[0].equals(box(0, 0, 1, 1))
    assert "source_name" not in faces.columns and "source_name" not in diagnostics.columns


def test_hosted_topology_physical_oracle_accepts_real_fifteen_saves_and_rejects_corruption(tmp_path):
    import importlib
    fixture = importlib.import_module('business.scripts.online-vector-topology-fixture')
    (tmp_path / 'outputs').mkdir()
    (tmp_path / '.addp-online-owner').write_text('topology-native')
    seeded = fixture.worker('seed', tmp_path)
    connection = {'engine_type': 'nfs', 'mount_path': str(tmp_path)}
    tasks = [
        {'id': 'load', 'operator': 'load', 'params': {'connection_info': connection, 'path': 'source.geojson'}, 'depends_on': []},
        {'id': 'build', 'operator': 'polygonize', 'params': {'input_gdf': {'$ref': 'load'}}, 'depends_on': ['load']},
    ]
    for port, name in zip(('result','diagnostics'), fixture.TARGETS):
        reference = {'$ref': 'build'} if port == 'result' else {'$ref': 'build', 'port': port}
        tasks.append({'id': 'save_' + port, 'operator': 'save', 'depends_on': ['build'], 'params': {
            'input_df': reference, 'connection_info': connection, 'path': 'outputs/' + name, 'mode': 'fail'}})
    tasks.extend([
        {'id': 'invalid', 'operator': 'load', 'params': {'connection_info': connection, 'path': 'invalid.geojson'}, 'depends_on': []},
        {'id': 'check', 'operator': 'check_geometry', 'params': {'input_gdf': {'$ref': 'invalid'}}, 'depends_on': ['invalid']},
        {'id': 'repair', 'operator': 'repair_geometry', 'params': {'input_gdf': {'$ref': 'check'}}, 'depends_on': ['check']},
        {'id': 'recheck', 'operator': 'check_geometry', 'params': {'input_gdf': {'$ref': 'repair'}, 'valid_field': 'after_valid', 'reason_field': 'after_reason'}, 'depends_on': ['repair']},
        {'id': 'save_repaired', 'operator': 'save', 'params': {'input_df': {'$ref': 'recheck'}, 'connection_info': connection, 'path': 'outputs/repaired.geojson', 'mode': 'fail'}, 'depends_on': ['recheck']},
    ])
    tasks.extend([
        {'id': 'snap_input', 'operator': 'load', 'params': {'connection_info': connection, 'path': 'snap-input.gpkg'}, 'depends_on': []},
        {'id': 'snap_reference', 'operator': 'load', 'params': {'connection_info': connection, 'path': 'snap-reference.gpkg'}, 'depends_on': []},
        {'id': 'snap', 'operator': 'snap_geometry', 'params': {'input_gdf': {'$ref': 'snap_input'}, 'reference_gdf': {'$ref': 'snap_reference'}, 'tolerance': .75}, 'depends_on': ['snap_input','snap_reference']},
        {'id': 'save_snapped', 'operator': 'save', 'params': {'input_df': {'$ref': 'snap'}, 'connection_info': connection, 'path': 'outputs/snapped.gpkg', 'mode': 'fail'}, 'depends_on': ['snap']},
    ])
    tasks.extend([
        {'id': 'overlap_input', 'operator': 'load', 'params': {'connection_info': connection, 'path': 'overlap-input.gpkg'}, 'depends_on': []},
        {'id': 'overlap', 'operator': 'check_polygon_overlap', 'params': {'input_gdf': {'$ref': 'overlap_input'}, 'id_field': 'parcel_id'}, 'depends_on': ['overlap_input']},
        {'id': 'save_overlaps', 'operator': 'save', 'params': {'input_df': {'$ref': 'overlap'}, 'connection_info': connection, 'path': 'outputs/overlaps.gpkg', 'mode': 'fail'}, 'depends_on': ['overlap']},
    ])
    tasks.extend([
        {'id': 'gap_input', 'operator': 'load', 'params': {'connection_info': connection, 'path': 'gap-input.gpkg'}, 'depends_on': []},
        {'id': 'gap_coverage', 'operator': 'load', 'params': {'connection_info': connection, 'path': 'gap-coverage.gpkg'}, 'depends_on': []},
        {'id': 'gap', 'operator': 'check_polygon_gap', 'params': {'input_gdf': {'$ref': 'gap_input'}, 'coverage_gdf': {'$ref': 'gap_coverage'}}, 'depends_on': ['gap_input','gap_coverage']},
        {'id': 'save_gaps', 'operator': 'save', 'params': {'input_df': {'$ref': 'gap'}, 'connection_info': connection, 'path': 'outputs/gaps.gpkg', 'mode': 'fail'}, 'depends_on': ['gap']},
    ])
    tasks.extend([
        {'id':'dangling_input','operator':'load','params':{'connection_info':connection,'path':'dangling-input.gpkg'},'depends_on':[]},
        {'id':'dangling','operator':'check_dangling_points','params':{'input_gdf':{'$ref':'dangling_input'},'id_field':'route_key'},'depends_on':['dangling_input']},
        {'id':'save_dangling','operator':'save','params':{'input_df':{'$ref':'dangling'},'connection_info':connection,'path':'outputs/dangling.gpkg','mode':'fail'},'depends_on':['dangling']},
    ])
    tasks.extend([
        {'id':'line_overlap_input','operator':'load','params':{'connection_info':connection,'path':'line-overlap-input.gpkg'},'depends_on':[]},
        {'id':'line_overlap','operator':'check_line_overlap','params':{'input_gdf':{'$ref':'line_overlap_input'},'id_field':'route_key'},'depends_on':['line_overlap_input']},
        {'id':'save_line_overlaps','operator':'save','params':{'input_df':{'$ref':'line_overlap'},'connection_info':connection,'path':'outputs/line-overlaps.gpkg','mode':'fail'},'depends_on':['line_overlap']},
    ])
    tasks.extend([
        {'id':'line_self_overlap_input','operator':'load','params':{'connection_info':connection,'path':'line-self-overlap-input.gpkg'},'depends_on':[]},
        {'id':'line_self_overlap','operator':'check_line_self_overlap','params':{'input_gdf':{'$ref':'line_self_overlap_input'},'id_field':'route_key'},'depends_on':['line_self_overlap_input']},
        {'id':'save_line_self_overlaps','operator':'save','params':{'input_df':{'$ref':'line_self_overlap'},'connection_info':connection,'path':'outputs/line-self-overlaps.gpkg','mode':'fail'},'depends_on':['line_self_overlap']},
    ])
    tasks.extend([
        {'id':'line_intersection_input','operator':'load','params':{'connection_info':connection,'path':'line-intersection-input.gpkg'},'depends_on':[]},
        {'id':'line_intersection','operator':'check_line_intersection','params':{'input_gdf':{'$ref':'line_intersection_input'},'id_field':'route_key'},'depends_on':['line_intersection_input']},
        {'id':'save_line_intersections','operator':'save','params':{'input_df':{'$ref':'line_intersection'},'connection_info':connection,'path':'outputs/line-intersections.gpkg','mode':'fail'},'depends_on':['line_intersection']},
    ])
    tasks.extend([
        {'id':'line_self_intersection_input','operator':'load','params':{'connection_info':connection,'path':'line-self-intersection-input.gpkg'},'depends_on':[]},
        {'id':'line_self_intersection','operator':'check_line_self_intersection','params':{'input_gdf':{'$ref':'line_self_intersection_input'},'id_field':'route_key'},'depends_on':['line_self_intersection_input']},
        {'id':'save_line_self_intersections','operator':'save','params':{'input_df':{'$ref':'line_self_intersection'},'connection_info':connection,'path':'outputs/line-self-intersections.gpkg','mode':'fail'},'depends_on':['line_self_intersection']},
    ])
    tasks.extend([
        {'id':'split_line_input','operator':'load','params':{'connection_info':connection,'path':'split-line-input.gpkg'},'depends_on':[]},
        {'id':'split_line','operator':'split_line_intersections','params':{'input_gdf':{'$ref':'split_line_input'},'id_field':'route_key','scope':'all'},'depends_on':['split_line_input']},
        {'id':'save_split_lines','operator':'save','params':{'input_df':{'$ref':'split_line'},'connection_info':connection,'path':'outputs/split-lines.gpkg','mode':'fail'},'depends_on':['split_line']},
    ])
    tasks.extend([
        {'id':'line_component_input','operator':'load','params':{'connection_info':connection,'path':'line-component-input.gpkg'},'depends_on':[]},
        {'id':'line_component','operator':'group_line_components','params':{'input_gdf':{'$ref':'line_component_input'},'id_field':'route_key'},'depends_on':['line_component_input']},
        {'id':'save_line_components','operator':'save','params':{'input_df':{'$ref':'line_component'},'connection_info':connection,'path':'outputs/line-components.gpkg','mode':'fail'},'depends_on':['line_component']},
    ])
    tasks.extend([
        {'id':'line_chain_input','operator':'load','params':{'connection_info':connection,'path':'line-chain-input.gpkg'},'depends_on':[]},
        {'id':'line_chain','operator':'merge_line_chains','params':{'input_gdf':{'$ref':'line_chain_input'},'id_field':'route_key','match_fields':['road','grade']},'depends_on':['line_chain_input']},
        {'id':'save_line_chains','operator':'save','params':{'input_df':{'$ref':'line_chain'},'connection_info':connection,'path':'outputs/line-chains.gpkg','mode':'fail'},'depends_on':['line_chain']},
        {'id':'save_line_chain_members','operator':'save','params':{'input_df':{'$ref':'line_chain','port':'members'},'connection_info':connection,'path':'outputs/line-chain-members.gpkg','mode':'fail'},'depends_on':['line_chain']},
    ])
    execution = execute_workflow({'tasks': tasks})
    assert execution['status'] == 'success', execution.get('error')
    actual = fixture.worker('verify', tmp_path)
    assert actual['sources'] == seeded['sources']
    assert [actual['targets'][name]['feature_count'] for name in fixture.TARGETS] == [4,4,2,5,7,8,17,9,8,10,15,43,15,12,18]
    for name in fixture.TARGETS[:3]:
        path = tmp_path / 'outputs' / name
        original = path.read_bytes()
        content = json.loads(original)
        if name == 'faces.geojson':
            content['features'][0]['geometry']['coordinates'][0][0][0] += .1
        elif name == 'diagnostics.geojson':
            content['features'][0]['properties']['issue_type'] = 'cut_edge'
        else:
            content['features'][0]['properties']['after_valid'] = False
        path.write_text(json.dumps(content))
        with pytest.raises(fixture.FixtureError):
            fixture.worker('verify', tmp_path)
        path.write_bytes(original)


    # Corrupt the real GeoPackage, not an in-memory mock of the snap result.
    from osgeo import ogr, osr
    path = tmp_path / 'outputs/snapped.gpkg'; original = path.read_bytes()
    for problem in ('chain', 'boundary', 'tie', 'insert', 'attribute', 'crs', 'count'):
        dataset = ogr.Open(str(path), 1); layer = dataset.GetLayer(0)
        if problem == 'crs':
            dataset.ExecuteSQL('UPDATE gpkg_geometry_columns SET srs_id=4326')
        elif problem == 'count':
            layer.DeleteFeature(1)
        else:
            feature = layer.GetFeature(1 if problem in ('chain','insert','attribute') else 2 if problem=='boundary' else 3)
            if problem == 'attribute': feature.SetField('grade', 999)
            else:
                coordinates = [[1,0],[10,0]] if problem=='chain' else [[.75,10],[10,10]] if problem=='boundary' else [[.5,20],[10,20]] if problem=='tie' else [[.5,0],[1,0],[10,0]]
                feature.SetGeometry(ogr.CreateGeometryFromJson(json.dumps({'type':'LineString','coordinates':coordinates})))
            layer.SetFeature(feature)
        dataset = None
        with pytest.raises(fixture.FixtureError): fixture.worker('verify', tmp_path)
        path.write_bytes(original)
    path = tmp_path / 'outputs/overlaps.gpkg'; original = path.read_bytes()
    for problem in ('source_id', 'area', 'tiny_area', 'part', 'coordinate', 'crs', 'count'):
        dataset = ogr.Open(str(path), 1); layer = dataset.GetLayer(0)
        if problem == 'crs':
            dataset.ExecuteSQL('UPDATE gpkg_geometry_columns SET srs_id=4326')
        elif problem == 'count':
            layer.DeleteFeature(7)
        else:
            feature = layer.GetFeature(7 if problem=='tiny_area' else 6 if problem=='part' else 1)
            if problem == 'source_id': feature.SetField('source_id_a', '共边')
            elif problem in ('area','tiny_area'): feature.SetField('overlap_area', 0)
            else:
                shape = json.loads(feature.GetGeometryRef().ExportToJson())
                if problem == 'part': shape['coordinates'].pop()
                else: shape['coordinates'][0][0][0][0] += .25
                feature.SetGeometry(ogr.CreateGeometryFromJson(json.dumps(shape)))
            layer.SetFeature(feature)
        dataset = None
        with pytest.raises(fixture.FixtureError): fixture.worker('verify', tmp_path)
        path.write_bytes(original)
    path = tmp_path / 'outputs/gaps.gpkg'; original = path.read_bytes()
    for problem in ('area','tiny_area','coordinate','hole','filled','crs','count','order','duplicate'):
        dataset=ogr.Open(str(path),1);layer=dataset.GetLayer(0)
        if problem=='crs':dataset.ExecuteSQL('UPDATE gpkg_geometry_columns SET srs_id=4326')
        elif problem=='count':layer.DeleteFeature(8)
        elif problem in ('order','duplicate'):
            first,second=layer.GetFeature(1),layer.GetFeature(2)
            shape=first.GetGeometryRef().Clone();first.SetGeometry(second.GetGeometryRef())
            if problem=='order':layer.SetFeature(first)
            second.SetGeometry(shape);layer.SetFeature(second)
        else:
            feature=layer.GetFeature(8 if problem=='tiny_area' else 3 if problem=='hole' else 1)
            if problem in ('area','tiny_area'):feature.SetField('gap_area',0 if problem=='area' else 1e-12)
            else:
                geometry=json.loads(feature.GetGeometryRef().ExportToJson())
                if problem=='hole':geometry['coordinates'].pop()
                elif problem=='filled':geometry={'type':'Polygon','coordinates':[fixture.rectangle(100,0,106,4)]}
                else:geometry['coordinates'][0][0][0]-=.25
                changed=ogr.CreateGeometryFromJson(json.dumps(geometry));feature.SetGeometry(changed)
                feature.SetField('gap_area',changed.GetArea())
            layer.SetFeature(feature)
        dataset=None
        with pytest.raises(fixture.FixtureError):fixture.worker('verify',tmp_path)
        path.write_bytes(original)
    for name in ('gap-input.gpkg','gap-coverage.gpkg'):
        path=tmp_path/name;original=path.read_bytes()
        dataset=ogr.Open(str(path),1);layer=dataset.GetLayer(0);feature=layer.GetFeature(1)
        feature.SetField('source_name','foreign-source');layer.SetFeature(feature);dataset=None
        with pytest.raises(fixture.FixtureError):fixture.worker('verify',tmp_path)
        path.write_bytes(original)
    path=tmp_path/'outputs/dangling.gpkg';original=path.read_bytes()
    for problem in ('source_id','fid_as_id','coordinate','tiny','count','duplicate','order','crs'):
        dataset=ogr.Open(str(path),1);layer=dataset.GetLayer(0)
        if problem=='crs':dataset.ExecuteSQL('UPDATE gpkg_geometry_columns SET srs_id=4326')
        elif problem=='count':layer.DeleteFeature(17)
        elif problem in ('duplicate','order'):
            first,second=layer.GetFeature(1),layer.GetFeature(2);shape=first.GetGeometryRef().Clone()
            if problem=='order':first.SetGeometry(second.GetGeometryRef());assert layer.SetFeature(first)==0
            second.SetGeometry(shape);assert layer.SetFeature(second)==0
        else:
            feature=layer.GetFeature(14 if problem=='tiny' else 1)
            if problem in ('source_id','fid_as_id'):feature.SetField('source_id','闭环' if problem=='source_id' else '1')
            else:
                geometry=feature.GetGeometryRef();point=ogr.Geometry(ogr.wkbPoint)
                point.AddPoint_2D(251 if problem=='tiny' else geometry.GetX()+.25,geometry.GetY());feature.SetGeometry(point)
            assert layer.SetFeature(feature)==0
        dataset=None
        with pytest.raises(fixture.FixtureError):fixture.worker('verify',tmp_path)
        path.write_bytes(original)
    path=tmp_path/'dangling-input.gpkg';original=path.read_bytes()
    dataset=ogr.Open(str(path),1);layer=dataset.GetLayer(0);feature=layer.GetFeature(1)
    feature.SetField('route_key','foreign-source');assert layer.SetFeature(feature)==0;dataset=None
    with pytest.raises(fixture.FixtureError):fixture.worker('verify',tmp_path)
    path.write_bytes(original)
    path=tmp_path/'outputs/line-overlaps.gpkg';original=path.read_bytes()
    for problem in ('source_id','fid_as_id','length','tiny','part','coordinate','count','duplicate','order','crs'):
        dataset=ogr.Open(str(path),1);layer=dataset.GetLayer(0)
        if problem=='crs':dataset.ExecuteSQL('UPDATE gpkg_geometry_columns SET srs_id=4326')
        elif problem=='count':layer.DeleteFeature(9)
        elif problem in ('duplicate','order'):
            first,second=layer.GetFeature(1),layer.GetFeature(2)
            props={key:first.GetField(key) for key in ('source_id_a','source_id_b','overlap_length')}
            shape=first.GetGeometryRef().Clone()
            if problem=='order':
                for key in props:first.SetField(key,second.GetField(key))
                first.SetGeometry(second.GetGeometryRef());assert layer.SetFeature(first)==0
            for key,value in props.items():second.SetField(key,value)
            second.SetGeometry(shape);assert layer.SetFeature(second)==0
        else:
            feature=layer.GetFeature(7 if problem=='tiny' else 6 if problem=='part' else 1)
            if problem in ('source_id','fid_as_id'):feature.SetField('source_id_a','接点' if problem=='source_id' else '1')
            elif problem in ('length','tiny'):feature.SetField('overlap_length',0 if problem=='length' else 1e-12)
            else:
                shape=json.loads(feature.GetGeometryRef().ExportToJson())
                if problem=='part':shape['coordinates'].pop()
                else:shape['coordinates'][0][0][0]+=.25
                feature.SetGeometry(ogr.CreateGeometryFromJson(json.dumps(shape)))
            assert layer.SetFeature(feature)==0
        dataset=None
        with pytest.raises(fixture.FixtureError):fixture.worker('verify',tmp_path)
        path.write_bytes(original)
    path=tmp_path/'line-overlap-input.gpkg';original=path.read_bytes()
    dataset=ogr.Open(str(path),1);layer=dataset.GetLayer(0);feature=layer.GetFeature(1)
    feature.SetField('route_key','foreign-source');assert layer.SetFeature(feature)==0;dataset=None
    with pytest.raises(fixture.FixtureError):fixture.worker('verify',tmp_path)
    path.write_bytes(original)
    path=tmp_path/'outputs/line-self-overlaps.gpkg';original=path.read_bytes()
    for problem in ('source_id','fid_as_id','length','tiny','part','coordinate','count','duplicate','order','crs'):
        dataset=ogr.Open(str(path),1);layer=dataset.GetLayer(0)
        if problem=='crs':dataset.ExecuteSQL('UPDATE gpkg_geometry_columns SET srs_id=4326')
        elif problem=='count':layer.DeleteFeature(8)
        elif problem in ('duplicate','order'):
            first,second=layer.GetFeature(1),layer.GetFeature(2)
            props={key:first.GetField(key) for key in ('source_id','overlap_length')}
            shape=first.GetGeometryRef().Clone()
            if problem=='order':
                for key in props:first.SetField(key,second.GetField(key))
                first.SetGeometry(second.GetGeometryRef());assert layer.SetFeature(first)==0
            for key,value in props.items():second.SetField(key,value)
            second.SetGeometry(shape);assert layer.SetFeature(second)==0
        else:
            feature=layer.GetFeature(5 if problem=='tiny' else 4 if problem=='part' else 1)
            if problem in ('source_id','fid_as_id'):feature.SetField('source_id','接点' if problem=='source_id' else '1')
            elif problem in ('length','tiny'):feature.SetField('overlap_length',0 if problem=='length' else 1e-12)
            else:
                shape=json.loads(feature.GetGeometryRef().ExportToJson())
                if problem=='part':shape['coordinates'].pop()
                else:shape['coordinates'][0][0][0]+=.25
                feature.SetGeometry(ogr.CreateGeometryFromJson(json.dumps(shape)))
            assert layer.SetFeature(feature)==0
        dataset=None
        with pytest.raises(fixture.FixtureError):fixture.worker('verify',tmp_path)
        path.write_bytes(original)
    path=tmp_path/'line-self-overlap-input.gpkg';original=path.read_bytes()
    dataset=ogr.Open(str(path),1);layer=dataset.GetLayer(0);feature=layer.GetFeature(1)
    feature.SetField('route_key','foreign-source');assert layer.SetFeature(feature)==0;dataset=None
    with pytest.raises(fixture.FixtureError):fixture.worker('verify',tmp_path)
    path.write_bytes(original)
    path=tmp_path/'outputs/line-intersections.gpkg';original=path.read_bytes()
    for problem in ('source_a','source_b','fid_as_id','coordinate','tiny','count','duplicate','order','crs'):
        dataset=ogr.Open(str(path),1);layer=dataset.GetLayer(0)
        if problem=='crs':dataset.ExecuteSQL('UPDATE gpkg_geometry_columns SET srs_id=4326')
        elif problem=='count':layer.DeleteFeature(10)
        elif problem in ('duplicate','order'):
            first,second=layer.GetFeature(1),layer.GetFeature(2)
            props={key:first.GetField(key) for key in ('source_id_a','source_id_b')};shape=first.GetGeometryRef().Clone()
            if problem=='order':
                for key in props:first.SetField(key,second.GetField(key))
                first.SetGeometry(second.GetGeometryRef());assert layer.SetFeature(first)==0
            for key,value in props.items():second.SetField(key,value)
            second.SetGeometry(shape);assert layer.SetFeature(second)==0
        else:
            feature=layer.GetFeature(10 if problem=='tiny' else 1)
            if problem in ('source_a','source_b','fid_as_id'):
                feature.SetField('source_id_b' if problem=='source_b' else 'source_id_a','1' if problem=='fid_as_id' else '叠甲')
            else:
                shape=feature.GetGeometryRef().Clone();shape.SetPoint_2D(0,0 if problem=='tiny' else 603,120 if problem=='tiny' else 100);feature.SetGeometry(shape)
            assert layer.SetFeature(feature)==0
        dataset=None
        with pytest.raises(fixture.FixtureError):fixture.worker('verify',tmp_path)
        path.write_bytes(original)
    path=tmp_path/'line-intersection-input.gpkg';original=path.read_bytes()
    dataset=ogr.Open(str(path),1);layer=dataset.GetLayer(0);feature=layer.GetFeature(1)
    feature.SetField('route_key','foreign-source');assert layer.SetFeature(feature)==0;dataset=None
    with pytest.raises(fixture.FixtureError):fixture.worker('verify',tmp_path)
    path.write_bytes(original)
    path=tmp_path/'outputs/line-self-intersections.gpkg';original=path.read_bytes()
    for problem in ('source','fid_as_id','coordinate','tiny','count','duplicate','order','crs','overlap-point','global-dedup'):
        dataset=ogr.Open(str(path),1);layer=dataset.GetLayer(0)
        if problem=='crs':dataset.ExecuteSQL('UPDATE gpkg_geometry_columns SET srs_id=4326')
        elif problem in ('count','global-dedup'):layer.DeleteFeature(15 if problem=='count' else 11)
        elif problem in ('duplicate','order'):
            first,second=layer.GetFeature(1),layer.GetFeature(2)
            identity=first.GetField('source_id');shape=first.GetGeometryRef().Clone()
            if problem=='order':
                first.SetField('source_id',second.GetField('source_id'));first.SetGeometry(second.GetGeometryRef());assert layer.SetFeature(first)==0
            second.SetField('source_id',identity);second.SetGeometry(shape);assert layer.SetFeature(second)==0
        else:
            feature=layer.GetFeature(9 if problem=='tiny' else 1)
            if problem in ('source','fid_as_id'):feature.SetField('source_id','1' if problem=='fid_as_id' else '重复')
            else:
                shape=feature.GetGeometryRef().Clone()
                shape.SetPoint_2D(0,0 if problem=='tiny' else 902 if problem=='overlap-point' else 802,160 if problem=='tiny' else 140 if problem=='overlap-point' else 141)
                if problem=='overlap-point':feature.SetField('source_id','叠支')
                feature.SetGeometry(shape)
            assert layer.SetFeature(feature)==0
        dataset=None
        with pytest.raises(fixture.FixtureError):fixture.worker('verify',tmp_path)
        path.write_bytes(original)
    path=tmp_path/'line-self-intersection-input.gpkg';original=path.read_bytes()
    dataset=ogr.Open(str(path),1);layer=dataset.GetLayer(0);feature=layer.GetFeature(1)
    feature.SetField('route_key','foreign-source');assert layer.SetFeature(feature)==0;dataset=None
    with pytest.raises(fixture.FixtureError):fixture.worker('verify',tmp_path)
    path.write_bytes(original)
    baseline = fixture.worker('verify', tmp_path)
    failed_tasks = [task.copy() for task in tasks if task['id'] in ('snap_input','snap_reference','snap','save_snapped')]
    failed_tasks[2]['params']['tolerance'] = 1.25
    failed_tasks[3]['params']['path'] = 'outputs/failed-snap.gpkg'
    failed = execute_workflow({'tasks': failed_tasks})
    assert failed['status'] == 'failed'
    assert 'snapping produced invalid geometries' in str(failed)
    assert not (tmp_path/'outputs/failed-snap.gpkg').exists()
    assert fixture.worker('verify', tmp_path) == baseline


def test_hosted_split_all_fixture_has_internal_and_cross_meetings_and_rejects_default_scope(tmp_path):
    import importlib
    fixture=importlib.import_module('business.scripts.online-vector-topology-fixture')
    fixture.seed_split_line(tmp_path/'source.gpkg')
    original=(tmp_path/'source.gpkg').read_bytes()
    source=gpd.read_file(tmp_path/'source.gpkg')
    with pytest.raises(ValueError,match='internal'):split_at_intersections(source)
    result=split_all_intersections(source)
    expected=fixture.split_line_expected_features()
    assert len(result)==43 and len(source)==17
    assert [list(map(list,shape.coords)) for shape in result.geometry]==[row['geometry']['coordinates'] for row in expected]
    assert result.drop(columns=source.geometry.name).to_dict('records')==[row['properties'] for row in expected]
    assert fixture.verify_split_line(tmp_path/'source.gpkg',source=True)['feature_count']==17
    assert (tmp_path/'source.gpkg').read_bytes()==original


@pytest.mark.parametrize("problem", ["source_id", "route_key", "null", "grade", "part_no", "segment_no", "direction", "tiny", "count", "crs", "field_type", "internal_cut", "repeat_visit", "same_source_part", "closed_seam", "duplicate_vertices"])
def test_hosted_split_line_fixture_rejects_real_physical_corruption(tmp_path, problem):
    import importlib
    from osgeo import ogr
    fixture = importlib.import_module('business.scripts.online-vector-topology-fixture')
    fixture.seed_split_line(tmp_path/'source.gpkg')
    before = (tmp_path/'source.gpkg').read_bytes()
    connection = {'engine_type':'nfs','mount_path':str(tmp_path)}
    tasks = [
        {'id':'load','operator':'load','params':{'connection_info':connection,'path':'source.gpkg'},'depends_on':[]},
        {'id':'split','operator':'split_line_intersections','params':{'input_gdf':{'$ref':'load'},'id_field':'route_key','scope':'all'},'depends_on':['load']},
        {'id':'save','operator':'save','params':{'input_df':{'$ref':'split'},'connection_info':connection,'path':'pieces.gpkg','mode':'fail'},'depends_on':['split']},
    ]
    execution = execute_workflow({'tasks':tasks})
    assert execution['status']=='success', execution
    assert fixture.verify_split_line(tmp_path/'pieces.gpkg')['feature_count']==43
    assert fixture.verify_split_line(tmp_path/'source.gpkg',source=True)['feature_count']==17
    assert (tmp_path/'source.gpkg').read_bytes()==before
    dataset = ogr.Open(str(tmp_path/'pieces.gpkg'),1); layer=dataset.GetLayer(0)
    if problem=='count': layer.DeleteFeature(43)
    elif problem=='repeat_visit':layer.DeleteFeature(35)
    elif problem=='crs': dataset.ExecuteSQL('UPDATE gpkg_geometry_columns SET srs_id=4326')
    elif problem=='field_type':
        assert layer.AlterFieldDefn(layer.GetLayerDefn().GetFieldIndex('grade'),ogr.FieldDefn('grade',ogr.OFTReal),ogr.ALTER_TYPE_FLAG)==0
    else:
        feature=layer.GetFeature({'null':4,'tiny':16,'internal_cut':21,'same_source_part':39,'closed_seam':31,'duplicate_vertices':42}.get(problem,1))
        if problem in ('source_id','route_key'): feature.SetField(problem,'1')
        elif problem=='null': feature.SetField('note','')
        elif problem=='same_source_part':feature.SetField('part_no',1)
        elif problem in ('grade','part_no','segment_no'): feature.SetField(problem,999)
        else:
            geometry=feature.GetGeometryRef()
            points=[list(point[:2]) for point in geometry.GetPoints()]
            if problem in ('direction','closed_seam'): points.reverse()
            elif problem=='internal_cut':points[-1]=[102,202]
            elif problem=='duplicate_vertices':points.pop(1)
            else: points[-1][0]=1e-12
            feature.SetGeometry(ogr.CreateGeometryFromJson(json.dumps({'type':'LineString','coordinates':points})))
        assert layer.SetFeature(feature)==0
    dataset=None
    with pytest.raises(fixture.FixtureError): fixture.verify_split_line(tmp_path/'pieces.gpkg')
    assert (tmp_path/'source.gpkg').read_bytes()==before


@pytest.mark.parametrize("problem", ["source_id", "route_key", "null", "grade", "part_no", "component_id", "direction", "tiny", "count", "crs", "field_type", "same_source", "split_connected"])
def test_hosted_line_component_fixture_rejects_real_physical_corruption(tmp_path, problem):
    import importlib
    from osgeo import ogr
    fixture = importlib.import_module('business.scripts.online-vector-topology-fixture')
    fixture.seed_line_component(tmp_path/'source.gpkg')
    before = (tmp_path/'source.gpkg').read_bytes()
    connection = {'engine_type':'nfs','mount_path':str(tmp_path)}
    tasks = [
        {'id':'load','operator':'load','params':{'connection_info':connection,'path':'source.gpkg'},'depends_on':[]},
        {'id':'split','operator':'group_line_components','params':{'input_gdf':{'$ref':'load'},'id_field':'route_key'},'depends_on':['load']},
        {'id':'save','operator':'save','params':{'input_df':{'$ref':'split'},'connection_info':connection,'path':'pieces.gpkg','mode':'fail'},'depends_on':['split']},
    ]
    execution = execute_workflow({'tasks':tasks})
    assert execution['status']=='success', execution
    assert fixture.verify_line_component(tmp_path/'pieces.gpkg')['feature_count']==15
    assert fixture.verify_line_component(tmp_path/'source.gpkg',source=True)['feature_count']==11
    assert (tmp_path/'source.gpkg').read_bytes()==before
    dataset = ogr.Open(str(tmp_path/'pieces.gpkg'),1); layer=dataset.GetLayer(0)
    if problem=='count': layer.DeleteFeature(15)
    elif problem=='crs': dataset.ExecuteSQL('UPDATE gpkg_geometry_columns SET srs_id=4326')
    elif problem in ('same_source','split_connected'):
        feature=layer.GetFeature(2 if problem=='same_source' else 13)
        feature.SetField('component_id',1 if problem=='same_source' else 9)
        assert layer.SetFeature(feature)==0
    elif problem=='field_type':
        assert layer.AlterFieldDefn(layer.GetLayerDefn().GetFieldIndex('grade'),ogr.FieldDefn('grade',ogr.OFTReal),ogr.ALTER_TYPE_FLAG)==0
    else:
        feature=layer.GetFeature(4 if problem=='null' else 11 if problem=='tiny' else 1)
        if problem in ('source_id','route_key'): feature.SetField(problem,'1')
        elif problem=='null': feature.SetField('note','')
        elif problem in ('grade','part_no','component_id'): feature.SetField(problem,999)
        else:
            geometry=feature.GetGeometryRef()
            points=[list(point[:2]) for point in geometry.GetPoints()]
            if problem=='direction': points.reverse()
            else: points[0][0]=1
            feature.SetGeometry(ogr.CreateGeometryFromJson(json.dumps({'type':'LineString','coordinates':points})))
        assert layer.SetFeature(feature)==0
    dataset=None
    with pytest.raises(fixture.FixtureError): fixture.verify_line_component(tmp_path/'pieces.gpkg')
    assert (tmp_path/'source.gpkg').read_bytes()==before


@pytest.mark.parametrize('port,problem',[
    ('result','chain'),('result','count'),('result','attribute'),('result','tiny'),('result','direction'),('result','field_type'),
    ('members','source'),('members','null'),('members','member'),('members','part'),('members','reverse'),('members','bool_type'),
    ('members','direction'),('members','tiny'),('members','count'),('members','crs'),
])
def test_hosted_line_chain_fixture_rejects_real_physical_corruption(tmp_path,port,problem):
    import importlib
    from osgeo import ogr
    fixture=importlib.import_module('business.scripts.online-vector-topology-fixture')
    fixture.seed_line_chain(tmp_path/'source.gpkg');original=(tmp_path/'source.gpkg').read_bytes()
    connection={'engine_type':'nfs','mount_path':str(tmp_path)}
    tasks=[{'id':'load','operator':'load','params':{'connection_info':connection,'path':'source.gpkg'},'depends_on':[]},
        {'id':'chain','operator':'merge_line_chains','params':{'input_gdf':{'$ref':'load'},'id_field':'route_key','match_fields':['road','grade']},'depends_on':['load']}]
    for name in ('result','members'):
        tasks.append({'id':'save_'+name,'operator':'save','params':{'input_df':{'$ref':'chain','port':name},'connection_info':connection,'path':name+'.gpkg','mode':'fail'},'depends_on':['chain']})
    execution=execute_workflow({'tasks':tasks});assert execution['status']=='success',execution.get('error')
    assert fixture.verify_line_chain(tmp_path/'source.gpkg','source')['feature_count']==16
    assert fixture.verify_line_chain(tmp_path/'result.gpkg')['feature_count']==12
    assert fixture.verify_line_chain(tmp_path/'members.gpkg','members')['feature_count']==18
    path=tmp_path/(port+'.gpkg');dataset=ogr.Open(str(path),1);layer=dataset.GetLayer(0)
    if problem=='count':layer.DeleteFeature(1)
    elif problem=='crs':dataset.ExecuteSQL('UPDATE gpkg_geometry_columns SET srs_id=4326')
    elif problem in ('field_type','bool_type'):
        field='grade' if problem=='field_type' else 'is_reversed'
        assert layer.AlterFieldDefn(layer.GetLayerDefn().GetFieldIndex(field),ogr.FieldDefn(field,ogr.OFTReal if problem=='field_type' else ogr.OFTInteger64),ogr.ALTER_TYPE_FLAG)==0
    else:
        feature=layer.GetFeature((9 if port=='result' else 12) if problem=='tiny' else 1)
        if problem=='chain':feature.SetField('member_count',1)
        elif problem=='attribute':feature.SetField('road','wrong')
        elif problem=='source':feature.SetField('route_key','lost')
        elif problem=='null':feature.SetField('note','')
        elif problem=='member':feature.SetField('member_no',2)
        elif problem=='part':feature.SetField('source_part_no',2)
        elif problem=='reverse':feature.SetField('is_reversed',1)
        else:
            points=[list(p[:2]) for p in feature.GetGeometryRef().GetPoints()]
            if problem=='direction':points.reverse()
            else:points[0][0]=1
            feature.SetGeometry(ogr.CreateGeometryFromJson(json.dumps({'type':'LineString','coordinates':points})))
        assert layer.SetFeature(feature)==0
    dataset=None
    with pytest.raises(fixture.FixtureError):fixture.verify_line_chain(path,port)
    assert (tmp_path/'source.gpkg').read_bytes()==original


# Single-feature validity is distinct from polygonize's whole-network face building.
def test_geometry_check_preserves_features_and_reports_valid_invalid_missing_and_empty():
    bow = Polygon([(0, 0), (2, 2), (0, 2), (2, 0), (0, 0)])
    frame = lines([box(0, 0, 1, 1), bow, None, Polygon(), Point(2, 3)])
    frame.index = [9, 9, 3, 7, 1]
    before = frame.copy(deep=True)
    checked = get_operator("check_geometry")(frame)
    assert checked.geometry_valid.tolist() == [True, False, False, True, True]
    assert checked.geometry_reason.iloc[0] == "Valid Geometry"
    assert checked.geometry_reason.iloc[1].startswith("Self-intersection")
    assert checked.geometry_reason.iloc[2] == "Missing Geometry"
    assert str(checked.geometry_valid.dtype) == "bool"
    assert str(checked.geometry_reason.dtype) == "string"
    assert_geodataframe_equal(checked.drop(columns=["geometry_valid", "geometry_reason"]), before)
    assert_geodataframe_equal(frame, before)


@pytest.mark.parametrize("kwargs", [
    {"valid_field": "source_name"}, {"reason_field": "boundary"},
    {"valid_field": "x", "reason_field": "x"}, {"valid_field": ""},
    {"reason_field": " reason "}, {"valid_field": 2},
])
def test_geometry_check_rejects_output_field_collision_and_bad_names(kwargs):
    with pytest.raises(ValueError, match="field"):
        get_operator("check_geometry")(lines([Point(0, 0)]), **kwargs)


def test_geometry_repair_keeps_multipart_collapsed_parts_and_valid_source_exactly():
    bow = Polygon([(0, 0), (2, 2), (0, 2), (2, 0), (0, 0)])
    collapsed = Polygon([(0, 0), (1, 1), (1, 2), (1, 1), (0, 0)])
    mixed = Polygon([(0, 0), (2, 0), (2, 2), (1, 1), (1, 2), (1, 1), (0, 0)])
    frame = lines([bow, collapsed, mixed, box(9, 9, 10, 10), None, Polygon()])
    frame.index = [5, 5, 2, 8, 4, 1]
    before = frame.copy(deep=True)
    repaired = get_operator("repair_geometry")(frame)
    assert repaired.geometry.iloc[0].geom_type == "MultiPolygon"
    assert sorted(part.area for part in repaired.geometry.iloc[0].geoms) == [1, 1]
    assert repaired.geometry.iloc[1].geom_type == "MultiLineString"
    assert repaired.geometry.iloc[1].equals(MultiLineString([[(0, 0), (1, 1)], [(1, 1), (1, 2)]]))
    assert repaired.geometry.iloc[2].geom_type == "GeometryCollection"
    assert {part.geom_type for part in repaired.geometry.iloc[2].geoms} == {"Polygon", "LineString"}
    assert repaired.geometry.iloc[3].wkb == frame.geometry.iloc[3].wkb
    assert repaired.geometry.iloc[4] is None and repaired.geometry.iloc[5].is_empty
    assert repaired.geometry.dropna().is_valid.all()
    assert repaired.drop(columns="boundary").equals(frame.drop(columns="boundary"))
    assert repaired.geometry.name == "boundary" and repaired.crs == frame.crs
    assert_geodataframe_equal(frame, before)


@pytest.mark.parametrize("crs", [None, "OGC:CRS84", "EPSG:3857"])
def test_geometry_validity_empty_batch_and_custom_fields_keep_crs(crs):
    frame = lines([], crs=crs)
    checked = get_operator("check_geometry")(frame, valid_field="ok", reason_field="why")
    assert checked.columns.tolist() == ["source_name", "boundary", "ok", "why"]
    assert checked.empty and checked.crs == frame.crs
    assert str(checked.ok.dtype) == "bool" and str(checked.why.dtype) == "string"
    assert_geodataframe_equal(get_operator("repair_geometry")(frame), frame)


@pytest.mark.parametrize("name", ["check_geometry", "repair_geometry"])
@pytest.mark.parametrize("frame", [None, pd.DataFrame({"x": [1]}), gpd.GeoDataFrame({"x": [1]})])
def test_geometry_validity_requires_active_geodataframe(name, frame):
    with pytest.raises(ValueError, match="GeoDataFrame|geometry"):
        get_operator(name)(frame)


@pytest.mark.parametrize("name", ["check_geometry", "repair_geometry"])
@pytest.mark.parametrize("wkt", ["POINT Z (1 2 3)", "POINT M (1 2 3)", "GEOMETRYCOLLECTION (POINT Z (1 2 3))", "POINT (Infinity 2)"])
def test_geometry_validity_rejects_nonfinite_or_extra_dimensions(name, wkt):
    frame = lines([shapely.from_wkt(wkt)])
    with pytest.raises(ValueError, match="finite|XY"):
        get_operator(name)(frame)


def test_geometry_repair_workflow_check_recheck_save_reload_and_discovery(tmp_path):
    source = lines([Polygon([(0, 0), (2, 2), (0, 2), (2, 0), (0, 0)])], crs="OGC:CRS84")
    source.to_file(tmp_path / "invalid.geojson", driver="GeoJSON")
    original = (tmp_path / "invalid.geojson").read_bytes()
    connection = {"engine_type": "nfs", "mount_path": str(tmp_path)}
    execution = execute_workflow({"tasks": [
        {"id": "load", "operator": "load", "params": {"connection_info": connection, "path": "invalid.geojson"}, "depends_on": []},
        {"id": "check", "operator": "check_geometry", "params": {"input_gdf": {"$ref": "load"}}, "depends_on": ["load"]},
        {"id": "repair", "operator": "repair_geometry", "params": {"input_gdf": {"$ref": "check"}}, "depends_on": ["check"]},
        {"id": "recheck", "operator": "check_geometry", "params": {"input_gdf": {"$ref": "repair"}, "valid_field": "after_valid", "reason_field": "after_reason"}, "depends_on": ["repair"]},
        {"id": "save", "operator": "save", "params": {"input_df": {"$ref": "recheck"}, "connection_info": connection, "path": "repaired.geojson", "mode": "fail"}, "depends_on": ["recheck"]},
    ]})
    assert execution["status"] == "success", execution.get("error")
    output = gpd.read_file(tmp_path / "repaired.geojson")
    assert output.source_name.tolist() == ["line-0"]
    assert output.geometry_valid.tolist() == [False] and output.after_valid.tolist() == [True]
    assert output.geometry.iloc[0].geom_type == "MultiPolygon" and output.geometry.iloc[0].area == 2
    assert output.crs == source.crs
    assert (tmp_path / "invalid.geojson").read_bytes() == original
    import api_server
    with api_server.app.test_client() as client:
        discovered = {op["id"]: op for op in client.get("/api/operators").get_json()["operators"]}
        for name in ["check_geometry", "repair_geometry"]:
            op = discovered[name]
            assert op["category_path"] == ["矢量处理", "几何整理"]
            assert op["execution_modes"] == ["workflow"] and op["effects"] == ["read"]
            assert [(p["name"], p["type"], p["is_default"]) for p in op["output_ports"]] == [("result", "geodataframe", True)]


@pytest.mark.parametrize("name", ["check_geometry", "repair_geometry"])
def test_geometry_validity_runtime_async_requires_context_and_rejects_direct(name, monkeypatch):
    import api_server
    from addp_common.workflow_runtime import ExecutionRegistry
    monkeypatch.setattr(api_server, "executions", ExecutionRegistry())
    frame = lines([Polygon([(0, 0), (2, 2), (0, 2), (2, 0), (0, 0)])])
    params = {"input_gdf": json.loads(frame.to_json())}
    with api_server.app.test_client() as client:
        assert client.post(f"/api/operators/{name}/invoke", json={"params": params}).status_code == 403
        workflow = {"tasks": [{"id": "compute", "operator": name, "params": params, "depends_on": []}]}
        assert client.post("/api/workflow", json={"workflow_def": workflow}).status_code == 400
        response = client.post("/api/workflow", json={"workflow_def": workflow, "runtime": {
            "tenant_id": 7, "execution_authorization": {"id": 1, "effects": ["read"]},
        }})
        assert response.status_code == 202
        execution_id = response.get_json()["execution_id"]
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            state = client.get("/api/executions/" + execution_id).get_json()
            if state["status"] in {"success", "failed"}:
                break
            time.sleep(0.01)
        assert state["status"] == "success", state
        feature = json.loads(state["result"])["features"][0]
        if name == "check_geometry":
            assert feature["properties"]["geometry_valid"] is False
            assert feature["geometry"]["type"] == "Polygon"
        else:
            assert feature["geometry"]["type"] == "MultiPolygon"
        assert feature["properties"]["source_name"] == "line-0"


@pytest.mark.parametrize("crs", [None, "OGC:CRS84", "EPSG:3857"])
def test_geometry_repair_keeps_crs_and_repairs_hole_outside_shell_and_degenerate_line(crs):
    misplaced_hole = Polygon(box(0, 0, 2, 2).exterior.coords, [box(3, 3, 4, 4).exterior.coords])
    frame = lines([misplaced_hole, LineString([(5, 5), (5, 5)]), Point(6, 6)], crs=crs)
    repaired = get_operator("repair_geometry")(frame)
    assert repaired.crs == frame.crs and repaired.geometry.name == frame.geometry.name
    assert repaired.geometry.iloc[0].equals(shapely.union_all([box(0, 0, 2, 2), box(3, 3, 4, 4)]))
    assert repaired.geometry.iloc[1].equals(Point(5, 5))
    assert repaired.geometry.iloc[2].wkb == frame.geometry.iloc[2].wkb
    assert repaired.is_valid.all()


def test_geometry_repair_saves_geometry_collection_and_collapsed_lines_without_discarding(tmp_path):
    mixed = Polygon([(0, 0), (2, 0), (2, 2), (1, 1), (1, 2), (1, 1), (0, 0)])
    collapsed = Polygon([(0, 0), (1, 1), (1, 2), (1, 1), (0, 0)])
    frame = lines([mixed, collapsed], crs="OGC:CRS84")
    repaired = get_operator("repair_geometry")(frame)
    connection = {"engine_type": "nfs", "mount_path": str(tmp_path)}
    get_operator("save")(input_df=repaired, connection_info=connection, path="mixed.geojson", mode="fail")
    reloaded = get_operator("load")(connection_info=connection, path="mixed.geojson")
    assert reloaded.geom_type.tolist() == ["GeometryCollection", "MultiLineString"]
    assert reloaded.source_name.tolist() == frame.source_name.tolist()
    assert reloaded.crs == frame.crs
    assert all(actual.equals(expected) for actual, expected in zip(reloaded.geometry, repaired.geometry))


def snap(frame, reference, tolerance):
    return get_operator("snap_geometry")(input_gdf=frame, reference_gdf=reference, tolerance=tolerance)


def test_reference_snap_preserves_rows_attributes_reference_and_uses_all_reference_vertices():
    frame = lines([Point(.1, .1), LineString([(10.1, 0), (20, 0)]), None, Point()])
    frame.index = [7, 7, 2, 9]
    frame['grade'] = pd.array([1, 2, None, 4], dtype='Int64')
    reference = lines([Point(0, 0), LineString([(10, 0), (10, 4)]), None, Point(), Point(0, 0)])
    reference.index = [99, 3, 8, 8, 0]
    before, reference_before = frame.copy(deep=True), reference.copy(deep=True)
    result = snap(frame, reference, .2)
    assert result.geometry.iloc[0].equals(Point(0, 0))
    assert list(result.geometry.iloc[1].coords) == [(10, 0), (20, 0)]
    assert result.geometry.iloc[2] is None and result.geometry.iloc[3].is_empty
    assert result.geometry.name == 'boundary' and result.crs == frame.crs
    pd.testing.assert_frame_equal(result.drop(columns='boundary'), before.drop(columns='boundary'))
    assert result.index.tolist() == [7, 7, 2, 9]
    assert_geodataframe_equal(frame, before)
    assert_geodataframe_equal(reference, reference_before)
    assert_geodataframe_equal(snap(frame, reference.iloc[::-1], .2), result)


@pytest.mark.parametrize('crs', ['EPSG:3857', 'EPSG:2263'])
def test_reference_snap_tolerance_is_in_coordinate_units_including_feet(crs):
    frame, reference = lines([Point(.25, 0)], crs), lines([Point(0, 0)], crs)
    assert snap(frame, reference, .3).geometry.iloc[0].equals(Point(0, 0))
    assert snap(frame, reference, .2).geometry.iloc[0].equals(Point(.25, 0))


def test_reference_snap_zero_and_exact_tolerance_boundary_are_unchanged():
    frame, reference = lines([Point(1, 0)]), lines([Point(0, 0)])
    assert_geodataframe_equal(snap(frame, reference, 0), frame)
    assert_geodataframe_equal(snap(frame, reference, 1), frame)
    assert snap(frame, reference, np.nextafter(1., 2.)).geometry.iloc[0].equals(Point(0, 0))


def test_reference_snap_does_not_insert_vertices_project_to_segments_or_new_intersections():
    frame = lines([LineString([(0, 0), (10, 0)])])
    assert_geodataframe_equal(snap(frame, lines([Point(5, 0)]), 0), frame)
    result = snap(frame, lines([Point(5, .1)]), .2)
    assert_geodataframe_equal(result, frame)
    point = lines([Point(5, .1)])
    assert_geodataframe_equal(snap(point, lines([LineString([(0, 0), (10, 0)])]), .2), point)
    crossing = lines([LineString([(0, 0), (10, 0)]), LineString([(5, -5), (5, 5)])])
    assert_geodataframe_equal(snap(point, crossing, .2), point)


def test_reference_snap_preserves_polygon_holes_and_nested_multipart_geometry():
    polygon = Polygon(box(0, 0, 10, 10).exterior.coords, [box(3, 3, 4, 4).exterior.coords])
    frame = lines([polygon, shapely.GeometryCollection([Point(.1, .1), LineString([(20.1, 0), (25, 0)])])])
    reference = lines([Point(.1, 0), Point(0, 0), Point(20, 0)])
    result = snap(frame, reference, .2)
    assert result.geometry.iloc[0].geom_type == 'Polygon'
    assert len(result.geometry.iloc[0].interiors) == 1
    assert result.geometry.iloc[0].interiors[0].equals(polygon.interiors[0])
    mixed = result.geometry.iloc[1]
    assert mixed.geom_type == 'GeometryCollection' and len(mixed.geoms) == 2
    assert mixed.geoms[1].equals(LineString([(20, 0), (25, 0)]))
    assert result.is_valid.all()


def test_reference_snap_tied_candidates_have_a_stable_reference_vertex_order():
    frame = lines([Point(0, 0)])
    reference = lines([Point(1, 0), Point(-1, 0), Point(1, 0)])
    first = snap(frame, reference, 1.5)
    assert first.geometry.iloc[0].equals(Point(-1, 0))
    assert_geodataframe_equal(first, snap(frame, reference.iloc[::-1], 1.5))


@pytest.mark.parametrize('reference', [[], [None, Point()]])
def test_reference_snap_empty_reference_is_identity_and_empty_input_keeps_schema(reference):
    frame = lines([Point(1, 2)])
    assert_geodataframe_equal(snap(frame, lines(reference), 1), frame)
    empty = lines([])
    assert_geodataframe_equal(snap(empty, lines([Point(0, 0)]), 1), empty)


@pytest.mark.parametrize('tolerance', [-1, np.nan, np.inf, -np.inf, True, np.bool_(False), '1', None, [1], 10**400])
def test_reference_snap_rejects_invalid_tolerance(tolerance):
    with pytest.raises(ValueError, match='tolerance'):
        snap(lines([Point(0, 0)]), lines([Point(0, 0)]), tolerance)


@pytest.mark.parametrize('source_crs,reference_crs', [
    (None, 'EPSG:3857'), ('EPSG:3857', None), (None, None),
    ('EPSG:4326', 'EPSG:4326'), ('OGC:CRS84', 'OGC:CRS84'),
    ('EPSG:3857', 'EPSG:32650'), ('EPSG:3857', 'EPSG:2263'),
])
def test_reference_snap_rejects_unknown_geographic_and_mismatched_crs(source_crs, reference_crs):
    with pytest.raises(ValueError, match='CRS'):
        snap(lines([Point(0, 0)], source_crs), lines([Point(0, 0)], reference_crs), 1)


@pytest.mark.parametrize('side', ['input', 'reference'])
@pytest.mark.parametrize('geometry', [
    Point(1, 2, 3),
    shapely.from_wkt('POINT M (1 2 3)'),
    Point(np.inf, 0),
    Polygon([(0, 0), (2, 2), (0, 2), (2, 0), (0, 0)]),
])
def test_reference_snap_rejects_unsupported_geometry_on_either_side(side, geometry):
    from operators.geometric_operators import ReferenceSnapInputError
    bad, good = lines([geometry]), lines([Point(0, 0)])
    with pytest.raises(ReferenceSnapInputError) as error:
        snap(bad if side == 'input' else good, bad if side == 'reference' else good, 1)
    assert error.value.code == ('source' if side == 'input' else 'reference')


@pytest.mark.parametrize('side', ['input', 'reference'])
@pytest.mark.parametrize('frame', [None, pd.DataFrame({'x': [1]}), gpd.GeoDataFrame({'x': [1]})])
def test_reference_snap_requires_two_active_geodataframes(side, frame):
    good = lines([Point(0, 0)])
    with pytest.raises(ValueError, match='GeoDataFrame|active geometry'):
        snap(frame if side == 'input' else good, frame if side == 'reference' else good, 1)


def test_reference_snap_rejects_collapsed_result_without_repair_or_partial_output():
    frame = lines([Point(5, 5), LineString([(.1, 0), (.2, 0)])])
    before = frame.copy(deep=True)
    with pytest.raises(ValueError, match='snap.*valid|invalid.*snap'):
        snap(frame, lines([Point(5.05, 5), Point(.15, 0)]), .1)
    assert_geodataframe_equal(frame, before)


def test_reference_snap_uses_original_positions_once_and_preserves_exact_reference_hits():
    frame = lines([Point(0, 0), Point(.5, 0), LineString([(0, 2), (3, 2)])])
    reference = lines([Point(1, 0), Point(.5, 0), Point(.5, 2), Point(1, 2)])
    result = snap(frame, reference, .75)
    assert result.geometry.iloc[0].equals(Point(.5, 0))
    assert result.geometry.iloc[1].equals(Point(.5, 0))
    assert list(result.geometry.iloc[2].coords) == [(.5, 2), (3, 2)]
    before, after = shapely.get_coordinates(frame.geometry), shapely.get_coordinates(result.geometry)
    assert len(before) == len(after)
    assert (np.hypot(*(after-before).T) < .75).all()


@pytest.mark.parametrize('source,reference,tolerance,moves', [
    (1e200, 1.1e200, 2e199, True),
    (0., 1e-200, 2e-200, True),
    (-np.finfo(float).max, np.finfo(float).max, np.finfo(float).max, False),
])
def test_reference_snap_finite_distances_do_not_overflow_or_underflow(source, reference, tolerance, moves):
    frame = lines([Point(source, 0)])
    result = snap(frame, lines([Point(reference, 0)]), tolerance)
    assert result.geometry.iloc[0].equals(Point(reference if moves else source, 0))


def test_reference_snap_runtime_discovery_and_two_input_workflow_save_reload(tmp_path, monkeypatch):
    import api_server
    from addp_common.workflow_runtime import ExecutionRegistry
    source = lines([LineString([(.1, .1), (5, 0)])])
    reference = lines([Point(0, 0)])
    for name, frame in [('source', source), ('reference', reference)]:
        frame.to_file(tmp_path / (name+'.gpkg'), layer=name, driver='GPKG')
    original = {n: (tmp_path / (n+'.gpkg')).read_bytes() for n in ['source', 'reference']}
    connection = {'engine_type': 'nfs', 'mount_path': str(tmp_path)}
    tasks = [{'id': n, 'operator': 'load', 'params': {'connection_info': connection, 'path': n+'.gpkg'}, 'depends_on': []} for n in ['source', 'reference']]
    tasks += [
        {'id': 'snap', 'operator': 'snap_geometry', 'params': {'input_gdf': {'$ref': 'source'}, 'reference_gdf': {'$ref': 'reference'}, 'tolerance': .2}, 'depends_on': ['source', 'reference']},
        {'id': 'save', 'operator': 'save', 'params': {'input_df': {'$ref': 'snap'}, 'connection_info': connection, 'path': 'snapped.gpkg', 'mode': 'fail'}, 'depends_on': ['snap']},
    ]
    execution = execute_workflow({'tasks': tasks})
    assert execution['status'] == 'success', execution.get('error')
    saved = gpd.read_file(tmp_path/'snapped.gpkg')
    assert list(saved.geometry.iloc[0].coords) == [(0, 0), (5, 0)]
    assert saved.source_name.tolist() == source.source_name.tolist() and saved.crs == source.crs
    assert {n: (tmp_path / (n+'.gpkg')).read_bytes() for n in original} == original
    monkeypatch.setattr(api_server, 'executions', ExecutionRegistry())
    with api_server.app.test_client() as client:
        op = next(o for o in client.get('/api/operators').get_json()['operators'] if o['id']=='snap_geometry')
        assert op['display_name']=='参考吸附' and op['category_path']==['矢量处理', '几何整理']
        assert op['execution_modes']==['workflow'] and op['effects']==['read']
        assert [(x['name'],x['param_type']) for x in op['parameters']]==[('input_gdf','input'),('reference_gdf','input'),('tolerance','param')]
        assert [(x['name'],x['type'],x['is_default']) for x in op['output_ports']]==[('result','geodataframe',True)]
        params = {'input_gdf': json.loads(source.to_json()), 'reference_gdf': json.loads(reference.to_json()), 'tolerance': .2}
        assert client.post('/api/operators/snap_geometry/invoke', json={'params': params}).status_code==403
        # Literal GeoJSON inputs enter the Runtime as EPSG:4326. Projected
        # geometries must arrive through the existing load/reproject nodes.
        workflow = {'tasks': tasks[:-1]}
        assert client.post('/api/workflow', json={'workflow_def':workflow}).status_code==400
        response = client.post('/api/workflow', json={'workflow_def':workflow,'runtime':{'tenant_id':7,'execution_authorization':{'id':1,'effects':['read']}}})
        assert response.status_code==202
        deadline = time.monotonic()+5
        while time.monotonic()<deadline:
            state = client.get('/api/executions/'+response.get_json()['execution_id']).get_json()
            if state['status'] in {'success','failed'}:break
            time.sleep(.01)
        assert state['status']=='success',state
        assert json.loads(state['result'])['features'][0]['geometry']['coordinates']==[[0.,0.],[5.,0.]]


def overlap_frame(geometries, ids=None, crs="EPSG:3857"):
    return gpd.GeoDataFrame(
        {"parcel_id": ids if ids is not None else list(range(len(geometries))),
         "business_value": ["unchanged"] * len(geometries)},
        geometry=geometries, crs=crs,
    ).rename_geometry("footprint")


def overlap(frame, id_field="parcel_id"):
    return get_operator("check_polygon_overlap")(input_gdf=frame, id_field=id_field)


def test_polygon_overlap_reports_partial_contained_and_duplicate_pairs_without_changing_source():
    frame = overlap_frame([box(0, 0, 4, 4), box(2, 0, 6, 4), box(1, 1, 2, 2), box(0, 0, 4, 4)], [90, 10, 30, 40])
    frame.index = [7, 7, 1, 9]
    original = frame.copy(deep=True)
    result = overlap(frame)
    assert result.columns.tolist() == ["source_id_a", "source_id_b", "overlap_area", "geometry"]
    assert list(zip(result.source_id_a, result.source_id_b, result.overlap_area)) == [
        (90, 10, 8), (90, 30, 1), (90, 40, 16), (10, 40, 8), (30, 40, 1),
    ]
    assert result.geom_type.tolist() == ["MultiPolygon"] * 5
    assert result.is_valid.all() and result.crs == frame.crs
    assert result.geometry.iloc[0].equals(box(2, 0, 4, 4))
    assert_geodataframe_equal(frame, original)


def test_polygon_overlap_ignores_line_point_contacts_and_respects_holes():
    ring = Polygon(box(0, 0, 4, 4).exterior.coords, [box(1, 1, 3, 3).exterior.coords])
    result = overlap(overlap_frame([ring, box(1, 1, 3, 3), box(4, 0, 5, 4), box(4, 4, 5, 5), box(10, 10, 11, 11)]))
    assert result.empty
    assert result.columns.tolist() == ["source_id_a", "source_id_b", "overlap_area", "geometry"]
    assert result.overlap_area.dtype == np.dtype("float64")


def test_polygon_overlap_keeps_all_polygon_parts_in_one_pair_and_drops_contact_lines():
    a = shapely.MultiPolygon([box(0, 0, 2, 2), box(4, 0, 5, 1), box(8, 0, 10, 2)])
    b = shapely.MultiPolygon([box(1, 0, 3, 2), box(5, 0, 6, 1), box(9, 0, 11, 2)])
    assert a.intersection(b).geom_type == "GeometryCollection"
    result = overlap(overlap_frame([a, b], ["地块甲", "地块乙"]))
    assert len(result) == 1 and result.overlap_area.tolist() == [4]
    assert len(result.geometry.iloc[0].geoms) == 2
    assert result.geometry.iloc[0].equals(shapely.MultiPolygon([box(1, 0, 2, 2), box(9, 0, 10, 2)]))


@pytest.mark.parametrize("crs,area", [("EPSG:3857", 1e-12), ("EPSG:2263", 1e-12)])
def test_polygon_overlap_has_no_small_area_threshold_and_does_not_convert_square_units(crs, area):
    result = overlap(overlap_frame([box(0, 0, 1, area), box(0, 0, 1, area)], crs=crs))
    assert result.overlap_area.tolist() == [area]
    assert result.crs == overlap_frame([], crs=crs).crs


@pytest.mark.parametrize("ids", [pd.Series([2**63+1, 2**63+2], dtype="UInt64"), pd.Series(["地块/甲", "地块%乙"], dtype="string")])
def test_polygon_overlap_preserves_identifier_dtype_without_float_or_string_coercion(ids):
    result = overlap(overlap_frame([box(0, 0, 2, 2)] * 2, ids))
    for name, position in [("source_id_a", 0), ("source_id_b", 1)]:
        assert result[name].dtype == ids.dtype
        assert result[name].iloc[0] == ids.iloc[position]


@pytest.mark.parametrize("geometries", [[], [Polygon()], [box(0, 0, 1, 1)], [Polygon(), box(0, 0, 1, 1)]])
def test_polygon_overlap_empty_and_single_feature_results_keep_typed_schema(geometries):
    frame = overlap_frame(geometries, pd.Series(range(len(geometries)), dtype="Int64"))
    result = overlap(frame)
    assert result.empty and result.crs == frame.crs
    assert result.source_id_a.dtype == result.source_id_b.dtype == frame.parcel_id.dtype
    assert result.overlap_area.dtype == np.dtype("float64")


@pytest.mark.parametrize("ids", [[None, "b"], ["a", "a"], ["", "b"], ["  ", "b"], [["a"], ["b"]]])
def test_polygon_overlap_rejects_missing_duplicate_blank_or_nonscalar_ids(ids):
    with pytest.raises(ValueError, match="scalar|non-empty|unique"):
        overlap(overlap_frame([box(0, 0, 1, 1)] * 2, ids))


@pytest.mark.parametrize("id_field", [None, "", "absent", "footprint"])
def test_polygon_overlap_requires_an_explicit_existing_nongeometry_id_field(id_field):
    with pytest.raises(ValueError, match="id_field"):
        overlap(overlap_frame([box(0, 0, 1, 1)]), id_field)


@pytest.mark.parametrize("crs", [None, "EPSG:4326", "OGC:CRS84"])
def test_polygon_overlap_rejects_unknown_or_geographic_crs_even_for_empty_input(crs):
    with pytest.raises(ValueError, match="projected CRS"):
        overlap(overlap_frame([], crs=crs))


@pytest.mark.parametrize("geometry,message", [
    (None, "non-null"), (Point(0, 0), "Polygon"),
    (LineString([(0, 0), (1, 1)]), "Polygon"),
    (Polygon([(0, 0), (2, 2), (0, 2), (2, 0), (0, 0)]), "valid"),
    (shapely.from_wkt("POLYGON Z ((0 0 1, 1 0 1, 1 1 1, 0 0 1))"), "two-dimensional"),
    (shapely.from_wkt("POLYGON M ((0 0 1, 1 0 1, 1 1 1, 0 0 1))"), "two-dimensional"),
    (shapely.from_wkt("POLYGON ((0 0, Infinity 0, 1 1, 0 0))"), "finite"),
])
def test_polygon_overlap_rejects_unsupported_geometries_without_implicit_repair(geometry, message):
    with pytest.raises(ValueError, match=message):
        overlap(overlap_frame([geometry]))


def test_polygon_overlap_rejects_nonfinite_or_underflowed_result_area():
    for size in [1e160, 1e-170]:
        frame = overlap_frame([box(0, 0, size, size)] * 2)
        with np.errstate(over="ignore", under="ignore", invalid="ignore"):
            with pytest.raises(ValueError, match="finite positive.*area"):
                overlap(frame)


def test_polygon_overlap_requires_an_active_geodataframe():
    for frame in [pd.DataFrame({"parcel_id": [1]}), gpd.GeoDataFrame({"parcel_id": [1]})]:
        with pytest.raises(ValueError, match="GeoDataFrame|active geometry"):
            overlap(frame)


def test_polygon_overlap_rejects_geometry_id_columns_and_ambiguous_column_names():
    frame = overlap_frame([box(0, 0, 1, 1)])
    frame["secondary_geometry"] = frame.geometry.copy()
    with pytest.raises(ValueError, match="id_field"):
        overlap(frame, "secondary_geometry")
    frame["another_id"] = frame.parcel_id
    frame = frame.rename(columns={"another_id": "parcel_id"})
    with pytest.raises(ValueError, match="id_field"):
        overlap(frame)


def test_polygon_overlap_invalid_id_fails_workflow_before_publishing_a_file(tmp_path):
    source = overlap_frame([box(0, 0, 2, 2), box(1, 1, 3, 3)], ["duplicate", "duplicate"])
    source.to_file(tmp_path / "invalid-ids.gpkg", driver="GPKG")
    before = (tmp_path / "invalid-ids.gpkg").read_bytes()
    connection = {"engine_type": "nfs", "mount_path": str(tmp_path)}
    result = execute_workflow({"tasks": [
        {"id": "load", "operator": "load", "params": {"connection_info": connection, "path": "invalid-ids.gpkg"}, "depends_on": []},
        {"id": "check", "operator": "check_polygon_overlap", "params": {"input_gdf": {"$ref": "load"}, "id_field": "parcel_id"}, "depends_on": ["load"]},
        {"id": "save", "operator": "save", "params": {"input_df": {"$ref": "check"}, "connection_info": connection, "path": "must-not-exist.gpkg", "mode": "fail"}, "depends_on": ["check"]},
    ]})
    assert result["status"] == "failed" and result["error_code"] == "INVALID_PARAMS"
    assert result["diagnostic"] == {
        "schema_version": "addp.execution-failure/v3", "stage": "execution",
        "reason": "operator_input_invalid",
        "operator": {"node_index": 2, "operator_id": "check_polygon_overlap", "parameter": "input_gdf"},
    }
    assert not (tmp_path / "must-not-exist.gpkg").exists()
    assert (tmp_path / "invalid-ids.gpkg").read_bytes() == before


def test_polygon_overlap_matches_independent_integer_grid_pair_oracle():
    # Exact rectangle occupancy supplies an area oracle independent of GEOS
    # intersection; dense and disjoint envelopes exercise pair de-duplication.
    bounds = [(0, 0, 4, 4), (1, 2, 6, 5), (2, 1, 3, 6), (8, 8, 9, 9), (0, 0, 4, 4)]
    frame = overlap_frame([box(*bound) for bound in bounds])
    cells = [{(x, y) for x in range(a, c) for y in range(b, d)} for a, b, c, d in bounds]
    expected = [(a, b, len(cells[a] & cells[b])) for a in range(len(cells)) for b in range(a+1, len(cells)) if cells[a] & cells[b]]
    result = overlap(frame)
    assert list(zip(result.source_id_a, result.source_id_b, result.overlap_area)) == expected


def test_polygon_overlap_runtime_discovery_workflow_save_reload_and_source_bytes(tmp_path, monkeypatch):
    import api_server
    from addp_common.workflow_runtime import ExecutionRegistry
    source = overlap_frame([box(0, 0, 4, 4), box(2, 0, 6, 4), box(1, 1, 2, 2)], ["甲", "乙", "丙"])
    source.to_file(tmp_path / "parcels.gpkg", layer="parcels", driver="GPKG")
    before = (tmp_path / "parcels.gpkg").read_bytes()
    connection = {"engine_type": "nfs", "mount_path": str(tmp_path)}
    tasks = [
        {"id": "load", "operator": "load", "params": {"connection_info": connection, "path": "parcels.gpkg"}, "depends_on": []},
        {"id": "check", "operator": "check_polygon_overlap", "params": {"input_gdf": {"$ref": "load"}, "id_field": "parcel_id"}, "depends_on": ["load"]},
        {"id": "save", "operator": "save", "params": {"input_df": {"$ref": "check"}, "connection_info": connection, "path": "overlaps.gpkg", "mode": "fail"}, "depends_on": ["check"]},
    ]
    execution = execute_workflow({"tasks": tasks})
    assert execution["status"] == "success", execution.get("error")
    saved = gpd.read_file(tmp_path / "overlaps.gpkg")
    assert list(zip(saved.source_id_a, saved.source_id_b, saved.overlap_area)) == [("甲", "乙", 8), ("甲", "丙", 1)]
    assert saved.crs == source.crs and saved.geom_type.tolist() == ["MultiPolygon"] * 2
    assert saved.geometry.iloc[0].equals(box(2, 0, 4, 4))
    assert (tmp_path / "parcels.gpkg").read_bytes() == before
    from osgeo import ogr
    dataset = ogr.Open(str(tmp_path / "overlaps.gpkg"))
    layer = dataset.GetLayer(0)
    assert layer.GetFeatureCount() == 2
    assert layer.GetSpatialRef().GetAuthorityCode(None) == "3857"
    records = []
    for feature in layer:
        geometry = feature.GetGeometryRef()
        assert geometry.GetGeometryName() == "MULTIPOLYGON" and geometry.IsValid()
        records.append((feature.GetField("source_id_a"), feature.GetField("source_id_b"), feature.GetField("overlap_area"), geometry.GetArea()))
    assert records == [("甲", "乙", 8, 8), ("甲", "丙", 1, 1)]
    dataset = None
    monkeypatch.setattr(api_server, "executions", ExecutionRegistry())
    with api_server.app.test_client() as client:
        op = next(o for o in client.get("/api/operators").get_json()["operators"] if o["id"] == "check_polygon_overlap")
        assert op["display_name"] == "面叠检查" and op["category_path"] == ["矢量处理", "拓扑检查"]
        assert op["execution_modes"] == ["workflow"] and op["effects"] == ["read"]
        assert [(p["name"], p["param_type"], p["required"]) for p in op["parameters"]] == [("input_gdf", "input", True), ("id_field", "param", True)]
        assert [(p["name"], p["type"], p["is_default"]) for p in op["output_ports"]] == [("result", "geodataframe", True)]
        assert client.post("/api/operators/check_polygon_overlap/invoke", json={"params": {}}).status_code == 403
        workflow = {"tasks": tasks[:-1]}
        assert client.post("/api/workflow", json={"workflow_def": workflow}).status_code == 400
        response = client.post("/api/workflow", json={"workflow_def": workflow, "runtime": {"tenant_id": 7, "execution_authorization": {"id": 1, "effects": ["read"]}}})
        assert response.status_code == 202
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            state = client.get("/api/executions/" + response.get_json()["execution_id"]).get_json()
            if state["status"] in {"success", "failed"}:
                break
            time.sleep(.01)
        assert state["status"] == "success", state
        assert len(json.loads(state["result"])["features"]) == 2


def gap(frame, coverage):
    return get_operator("check_polygon_gap")(input_gdf=frame, coverage_gdf=coverage)


def test_polygon_gap_reports_internal_and_edge_gaps_without_mutating_sources():
    frame = overlap_frame([Polygon(box(0, 0, 3, 4).exterior.coords, [box(1, 1, 2, 2).exterior.coords])])
    coverage = lines([box(0, 0, 4, 4)]).rename_geometry("extent")
    frame.index = [19]
    originals = [frame.copy(deep=True), coverage.copy(deep=True)]
    result = gap(frame, coverage)
    assert result.columns.tolist() == ["gap_area", "geometry"]
    assert result.gap_area.tolist() == [1, 4]
    assert result.geom_type.tolist() == ["Polygon", "Polygon"]
    assert result.geometry.iloc[0].equals(box(1, 1, 2, 2))
    assert result.geometry.iloc[1].equals(box(3, 0, 4, 4))
    assert result.crs == frame.crs == coverage.crs
    for source, before in zip([frame, coverage], originals):
        assert_geodataframe_equal(source, before)


def test_polygon_gap_unions_both_layers_and_ignores_outside_coverage_and_row_order():
    coverage = lines([box(0, 0, 4, 4), box(2, 0, 6, 4), box(0, 0, 4, 4)])
    frame = lines([box(-2, -2, 1, 8), box(3, 0, 4, 4), box(3, 0, 4, 4), box(10, 0, 11, 1)])
    result = gap(frame, coverage)
    assert result.gap_area.tolist() == [8, 8]
    assert result.geometry.iloc[0].equals(box(1, 0, 3, 4))
    assert result.geometry.iloc[1].equals(box(4, 0, 6, 4))
    reversed_source = frame.iloc[::-1].copy()
    reversed_source.geometry = shapely.reverse(reversed_source.geometry.array)
    assert_geodataframe_equal(gap(reversed_source, coverage.iloc[::-1]), result)


def test_polygon_gap_excludes_coverage_holes_and_retains_islands_as_result_holes():
    boundary = Polygon(box(0, 0, 6, 6).exterior.coords, [box(1, 1, 2, 2).exterior.coords])
    coverage = lines([boundary])
    result = gap(lines([box(3, 3, 4, 4)]), coverage)
    assert len(result) == 1 and result.gap_area.tolist() == [34]
    assert len(result.geometry.iloc[0].interiors) == 2
    assert not result.geometry.iloc[0].contains(Point(1.5, 1.5))
    assert not result.geometry.iloc[0].contains(Point(3.5, 3.5))
    assert gap(lines([box(0, 0, 6, 6)]), coverage).empty


def test_polygon_gap_splits_multipart_and_point_contacts_into_separate_rows():
    coverage = lines([shapely.MultiPolygon([box(0, 0, 1, 1), box(1, 1, 2, 2), box(3, 0, 4, 1)])])
    result = gap(lines([]), coverage)
    assert result.gap_area.tolist() == [1, 1, 1]
    assert result.geom_type.tolist() == ["Polygon"] * 3
    assert [geometry.bounds for geometry in result.geometry] == [(0, 0, 1, 1), (1, 1, 2, 2), (3, 0, 4, 1)]


@pytest.mark.parametrize("crs", ["EPSG:3857", "EPSG:2263"])
def test_polygon_gap_keeps_tiny_exact_positive_area_and_source_square_units(crs):
    tiny = 2. ** -40
    result = gap(lines([], crs), lines([box(0, 0, 1, tiny)], crs))
    assert result.gap_area.tolist() == [tiny]
    assert result.geometry.iloc[0].bounds == (0, 0, 1, tiny)
    assert result.crs == lines([], crs).crs


@pytest.mark.parametrize("geometries", [[], [Polygon()], [shapely.MultiPolygon([])]])
def test_polygon_gap_empty_input_reports_entire_range_without_inventing_source_ids(geometries):
    result = gap(lines(geometries), lines([box(0, 0, 2, 2)]))
    assert result.columns.tolist() == ["gap_area", "geometry"]
    assert result.gap_area.tolist() == [4]
    assert result.geometry.iloc[0].equals(box(0, 0, 2, 2))


@pytest.mark.parametrize("source", [[box(0, 0, 2, 2)], [box(-1, -1, 3, 3)], [box(0, 0, 1, 2), box(1, 0, 2, 2)]])
def test_polygon_gap_full_coverage_returns_typed_empty_schema(source):
    result = gap(lines(source), lines([box(0, 0, 2, 2), Polygon()]))
    assert result.empty and result.columns.tolist() == ["gap_area", "geometry"]
    assert result.gap_area.dtype == np.dtype("float64") and result.crs.to_epsg() == 3857


@pytest.mark.parametrize("coverage", [[], [Polygon()], [shapely.MultiPolygon([]), Polygon()]])
def test_polygon_gap_rejects_absent_effective_coverage(coverage):
    with pytest.raises(ValueError, match="coverage_gdf.*non-empty"):
        gap(lines([]), lines(coverage))
    with pytest.raises(TypeError, match="coverage_gdf"):
        get_operator("check_polygon_gap")(input_gdf=lines([]))


@pytest.mark.parametrize("source_crs,coverage_crs", [(None, "EPSG:3857"), ("EPSG:3857", None), ("EPSG:4326", "EPSG:4326"), ("EPSG:3857", "EPSG:2263")])
def test_polygon_gap_rejects_unknown_geographic_or_different_crs_before_empty_result(source_crs, coverage_crs):
    with pytest.raises(ValueError, match="CRS"):
        gap(lines([], source_crs), lines([box(0, 0, 1, 1)], coverage_crs))


@pytest.mark.parametrize("side", ["input", "coverage"])
@pytest.mark.parametrize("geometry,message", [
    (None, "non-null"), (Point(0, 0), "Polygon"), (LineString([(0, 0), (1, 1)]), "Polygon"),
    (shapely.GeometryCollection([box(0, 0, 1, 1)]), "Polygon"),
    (Polygon([(0, 0), (1, 1), (0, 1), (1, 0), (0, 0)]), "valid"),
    (shapely.from_wkt("POLYGON Z ((0 0 1, 1 0 1, 1 1 1, 0 0 1))"), "XY"),
    (shapely.from_wkt("POLYGON M ((0 0 1, 1 0 1, 1 1 1, 0 0 1))"), "XY"),
    (Polygon([(0, 0), (1, 0), (float('inf'), 1), (0, 0)]), "finite"),
])
def test_polygon_gap_rejects_unsupported_geometry_on_either_side(side, geometry, message):
    good, bad = lines([box(0, 0, 2, 2)]), lines([geometry])
    with pytest.raises(ValueError, match=message):
        gap(bad if side == "input" else good, bad if side == "coverage" else good)


@pytest.mark.parametrize("side", ["input", "coverage"])
@pytest.mark.parametrize("frame", [None, pd.DataFrame(), gpd.GeoDataFrame({"value": [1]})])
def test_polygon_gap_requires_two_active_geodataframes(side, frame):
    good = lines([box(0, 0, 1, 1)])
    with pytest.raises(ValueError, match="GeoDataFrame|active geometry"):
        gap(frame if side == "input" else good, frame if side == "coverage" else good)


@pytest.mark.parametrize("size", [1e200, 1e-200])
def test_polygon_gap_fails_unrepresentable_area_without_discarding_a_gap(size):
    with np.errstate(over="ignore", under="ignore", invalid="ignore"):
        with pytest.raises(ValueError, match="finite positive Float64"):
            gap(lines([]), lines([box(0, 0, size, size)]))


def test_polygon_gap_matches_an_independent_integer_cell_oracle():
    boundary = [(0, 0, 6, 4), (4, 0, 8, 2)]
    source = [(-1, -1, 1, 5), (2, 0, 3, 4), (4, 1, 7, 3), (5, 0, 6, 1)]
    def cells(bounds):
        return {(x, y) for a, b, c, d in bounds for x in range(a, c) for y in range(b, d)}
    expected = cells(boundary) - cells(source)
    result = gap(lines([box(*bound) for bound in source]), lines([box(*bound) for bound in boundary]))
    assert result.gap_area.sum() == len(expected)
    for x in range(-1, 9):
        for y in range(-1, 6):
            matches = sum(geometry.contains(Point(x + .5, y + .5)) for geometry in result.geometry)
            assert matches == int((x, y) in expected)


@pytest.mark.parametrize("fully_covered", [False, True])
def test_polygon_gap_two_source_workflow_save_reload_independent_gdal_and_source_bytes(tmp_path, fully_covered):
    from osgeo import ogr
    source = lines([box(0, 0, 4 if fully_covered else 3, 4)])
    coverage = lines([box(0, 0, 4, 4)])
    for name, frame in [("source", source), ("coverage", coverage)]:
        frame.to_file(tmp_path / f"{name}.gpkg", layer=name, driver="GPKG")
    before = {name: (tmp_path / f"{name}.gpkg").read_bytes() for name in ["source", "coverage"]}
    connection = {"engine_type": "nfs", "mount_path": str(tmp_path)}
    tasks = [
        {"id": name, "operator": "load", "params": {"connection_info": connection, "path": f"{name}.gpkg"}, "depends_on": []}
        for name in ["source", "coverage"]
    ] + [
        {"id": "check", "operator": "check_polygon_gap", "params": {"input_gdf": {"$ref": "source"}, "coverage_gdf": {"$ref": "coverage"}}, "depends_on": ["source", "coverage"]},
        {"id": "save", "operator": "save", "params": {"input_df": {"$ref": "check"}, "connection_info": connection, "path": "gaps.gpkg", "mode": "fail"}, "depends_on": ["check"]},
    ]
    execution = execute_workflow({"tasks": tasks})
    assert execution["status"] == "success", execution.get("error")
    saved = gpd.read_file(tmp_path / "gaps.gpkg")
    assert saved.crs == source.crs and saved.gap_area.tolist() == ([] if fully_covered else [4])
    dataset = ogr.Open(str(tmp_path / "gaps.gpkg"))
    layer = dataset.GetLayer(0)
    assert layer.GetFeatureCount() == int(not fully_covered)
    assert layer.GetSpatialRef().GetAuthorityCode(None) == "3857"
    if not fully_covered:
        feature = layer.GetNextFeature()
        geometry = feature.GetGeometryRef()
        assert geometry.GetGeometryName() == "POLYGON" and geometry.IsValid()
        assert feature.GetField("gap_area") == geometry.GetArea() == 4
        assert set(geometry.GetGeometryRef(0).GetPoints()) == {(3, 0), (3, 4), (4, 4), (4, 0)}
    dataset = None
    assert all((tmp_path / f"{name}.gpkg").read_bytes() == data for name, data in before.items())


def test_polygon_gap_invalid_or_missing_coverage_fails_before_publishing(tmp_path):
    connection = {"engine_type": "nfs", "mount_path": str(tmp_path)}
    lines([box(0, 0, 1, 1)]).to_file(tmp_path / "source.gpkg", driver="GPKG")
    lines([]).to_file(tmp_path / "empty.gpkg", driver="GPKG")
    for extra_params in [{}, {"coverage_gdf": {"$ref": "coverage"}}]:
        result = execute_workflow({"tasks": [
            {"id": "source", "operator": "load", "params": {"connection_info": connection, "path": "source.gpkg"}, "depends_on": []},
            {"id": "coverage", "operator": "load", "params": {"connection_info": connection, "path": "empty.gpkg"}, "depends_on": []},
            {"id": "check", "operator": "check_polygon_gap", "params": {"input_gdf": {"$ref": "source"}, **extra_params}, "depends_on": ["source", "coverage"]},
            {"id": "save", "operator": "save", "params": {"input_df": {"$ref": "check"}, "connection_info": connection, "path": "must-not-exist.gpkg", "mode": "fail"}, "depends_on": ["check"]},
        ]})
        assert result["status"] == "failed" and result["error_code"] == "INVALID_PARAMS"
        assert result["diagnostic"] == {
            "schema_version": "addp.execution-failure/v3", "stage": "execution",
            "reason": "operator_mask_invalid",
            "operator": {"node_index": 3, "operator_id": "check_polygon_gap", "parameter": "coverage_gdf"},
        }
        assert not (tmp_path / "must-not-exist.gpkg").exists()


@pytest.mark.parametrize("operator,problem", [
    (operator, problem)
    for operator in ("check_polygon_overlap", "check_polygon_gap")
    for problem in ("source", "missing_source", "crs", "native_check", "native_compute", "area")
] + [("check_polygon_overlap", "missing_id"), ("check_polygon_gap", "invalid_result")])
def test_polygon_check_classification_blocks_result_save(monkeypatch, operator, problem):
    import workflow_engine
    from addp_common.workflow_runtime import WorkflowExecutionError

    overlap_check = operator == "check_polygon_overlap"
    geometry = [box(0, 0, 2, 2), box(1, 1, 3, 3)]
    if problem == "source":
        geometry[0] = Polygon([(0, 0), (2, 2), (0, 2), (2, 0), (0, 0)])
    source = overlap_frame(geometry)
    params = {"input_gdf": source, **({"id_field": "parcel_id"} if overlap_check else {
        "coverage_gdf": lines([box(0, 0, 4, 4)]),
    })}
    if problem == "missing_source":
        del params["input_gdf"]
    elif problem == "missing_id":
        del params["id_field"]
    elif problem == "crs":
        if overlap_check:
            params["input_gdf"] = source.set_crs(None, allow_override=True)
        else:
            params["coverage_gdf"] = params["coverage_gdf"].set_crs("EPSG:2263", allow_override=True)
    function = get_operator(operator)
    saves = []
    original = workflow_engine.get_operator

    def resolve(name):
        if name == "save":
            return lambda **kwargs: saves.append(kwargs)
        if name != operator:
            return original(name)

        def invoke(**kwargs):
            def native_failure(*_args, **_kwargs):
                raise ValueError("input_gdf requires valid polygons")

            with monkeypatch.context() as scope:
                if problem == "native_check":
                    scope.setattr(shapely, "is_valid", native_failure)
                elif problem == "native_compute":
                    scope.setattr(shapely, "intersection" if overlap_check else "difference", native_failure)
                elif problem == "area":
                    scope.setattr(shapely, "area", lambda values: np.full(np.shape(values), np.inf))
                elif problem == "invalid_result":
                    scope.setattr(shapely, "difference", lambda *_args: Point(0, 0))
                return function(**kwargs)

        return invoke

    monkeypatch.setattr(workflow_engine, "get_operator", resolve)
    tasks = [
        {"id": "operation", "operator": operator, "params": params, "depends_on": []},
        {"id": "save_result", "operator": "save", "depends_on": ["operation"],
         "params": {"input_df": {"$ref": "operation", "port": "result"}}},
    ]
    with pytest.raises(WorkflowExecutionError) as error:
        workflow_engine.GeoPythonWorkflowRunner(language="en").execute({"tasks": tasks})
    assert not saves
    if problem in {"source", "missing_source", "missing_id", "crs"}:
        context = {"node_index": 1, "operator_id": operator}
        if problem != "crs":
            context["parameter"] = "input_gdf"
        assert error.value.error_code == "INVALID_PARAMS"
        assert error.value.diagnostic == {
            "schema_version": "addp.execution-failure/v3", "stage": "execution",
            "reason": "operator_crs_invalid" if problem == "crs" else "operator_input_invalid",
            "operator": context,
        }
    else:
        assert error.value.error_code == "EXECUTION_FAILED"
        assert error.value.diagnostic is None
        if problem == "area":
            assert "finite positive Float64 area" in str(error.value)
        elif problem == "invalid_result":
            assert "did not produce valid polygon components" in str(error.value)


@pytest.mark.parametrize("crs,authority", [("EPSG:3857", "3857"), ("EPSG:2263", "2263")])
def test_polygon_gap_native_save_retains_exact_tiny_coordinates_and_square_units(tmp_path, crs, authority):
    from osgeo import ogr
    tiny = 2. ** -40
    frame = lines([box(0, tiny, 1, 1)], crs)
    frame["gap_area"] = [-1]  # Diagnostic output must not copy source attributes.
    result = gap(frame, lines([box(0, 0, 1, 1)], crs))
    assert result.gap_area.tolist() == [tiny]
    get_operator("save")(
        input_df=result, connection_info={"engine_type": "nfs", "mount_path": str(tmp_path)},
        path="tiny.gpkg", mode="fail",
    )
    dataset = ogr.Open(str(tmp_path / "tiny.gpkg"))
    layer = dataset.GetLayer(0)
    assert layer.GetFeatureCount() == 1 and layer.GetSpatialRef().GetAuthorityCode(None) == authority
    feature = layer.GetNextFeature()
    geometry = feature.GetGeometryRef()
    assert geometry.GetArea() == feature.GetField("gap_area") == tiny
    assert set(geometry.GetGeometryRef(0).GetPoints()) == {(0, 0), (0, tiny), (1, tiny), (1, 0)}
    dataset = None


def test_polygon_gap_discovery_two_required_input_ports_async_workflow_and_direct_rejection(tmp_path, monkeypatch):
    import api_server
    from addp_common.workflow_runtime import ExecutionRegistry
    connection = {"engine_type": "nfs", "mount_path": str(tmp_path)}
    for name, geometry in [("source", box(0, 0, 3, 4)), ("coverage", box(0, 0, 4, 4))]:
        lines([geometry]).to_file(tmp_path / f"{name}.gpkg", driver="GPKG")
    tasks = [
        {"id": name, "operator": "load", "params": {"connection_info": connection, "path": f"{name}.gpkg"}, "depends_on": []}
        for name in ["source", "coverage"]
    ] + [{"id": "check", "operator": "check_polygon_gap", "params": {"input_gdf": {"$ref": "source"}, "coverage_gdf": {"$ref": "coverage"}}, "depends_on": ["source", "coverage"]}]
    monkeypatch.setattr(api_server, "executions", ExecutionRegistry())
    with api_server.app.test_client() as client:
        op = next(o for o in client.get("/api/operators").get_json()["operators"] if o["id"] == "check_polygon_gap")
        assert op["display_name"] == "面缝检查" and op["category_path"] == ["矢量处理", "拓扑检查"]
        assert op["execution_modes"] == ["workflow"] and op["effects"] == ["read"]
        assert [(p["name"], p["param_type"], p["required"]) for p in op["parameters"]] == [("input_gdf", "input", True), ("coverage_gdf", "input", True)]
        assert [(p["name"], p["type"], p["is_default"]) for p in op["output_ports"]] == [("result", "geodataframe", True)]
        assert client.post("/api/operators/check_polygon_gap/invoke", json={"params": {}}).status_code == 403
        workflow = {"tasks": tasks}
        assert client.post("/api/workflow", json={"workflow_def": workflow}).status_code == 400
        response = client.post("/api/workflow", json={"workflow_def": workflow, "runtime": {"tenant_id": 7, "execution_authorization": {"id": 1, "effects": ["read"]}}})
        assert response.status_code == 202
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            state = client.get("/api/executions/" + response.get_json()["execution_id"]).get_json()
            if state["status"] in {"success", "failed"}:
                break
            time.sleep(.01)
        assert state["status"] == "success", state
        features = json.loads(state["result"])["features"]
        assert len(features) == 1 and features[0]["properties"]["gap_area"] == 4


def dangles(geometries, ids=None, crs="EPSG:3857"):
    frame = lines(geometries, crs)
    frame["line_id"] = ids if ids is not None else list(range(len(frame)))
    return get_operator("check_dangling_points")(input_gdf=frame, id_field="line_id")


@pytest.mark.parametrize("geometries,expected", [
    ([LineString([(0,0),(2,0)])], [(0,0,0),(0,2,0)]),
    ([LineString([(0,0),(1,0)]),LineString([(1,0),(2,0)])], [(0,0,0),(1,2,0)]),
    ([LineString([(0,0),(4,0)]),LineString([(2,0),(2,2)])], [(0,0,0),(0,4,0),(1,2,2)]),
    ([LineString([(0,0),(2,2)]),LineString([(0,2),(2,0)])], [(0,0,0),(0,2,2),(1,0,2),(1,2,0)]),
    ([box(0,0,2,2).boundary], []),
    ([LineString([(0,0),(2,2),(0,2),(2,0)])], [(0,0,0),(0,2,0)]),
    ([LineString([(0,0),(4,0)]),LineString([(4,0),(0,0)])], [(0,0,0),(0,4,0),(1,0,0),(1,4,0)]),
    ([LineString([(0,0),(4,0)]),LineString([(2,0),(6,0)])], [(0,0,0),(1,6,0)]),
    ([LineString([(0,0),(4,0)]),LineString([(1,0),(3,0)])], [(0,0,0),(0,4,0)]),
    ([box(0,0,2,2).boundary,LineString([(0,0),(-1,0)])], [(1,-1,0)]),
    ([MultiLineString([[(0,0),(1,0)],[(1,0),(2,0)],[(1,0),(0,0)]])], [(0,0,0),(0,2,0)]),
])
def test_dangling_points_reports_geometric_degree_one_with_exact_source_binding(geometries, expected):
    result = dangles(geometries)
    assert result.columns.tolist() == ["source_id", "geometry"]
    assert list(zip(result.source_id, result.geometry.x, result.geometry.y)) == expected
    assert result.geom_type.tolist() == ["Point"]*len(expected)
    assert result.crs.to_epsg() == 3857


@pytest.mark.parametrize("crs", ["EPSG:3857","EPSG:2263"])
def test_dangling_points_keeps_binary_tiny_disconnect_without_tolerance(crs):
    tiny=2.**-40
    result=dangles([LineString([(0,0),(1,0)]),LineString([(1+tiny,0),(2,0)])],crs=crs)
    assert list(zip(result.source_id,result.geometry.x)) == [(0,0),(0,1),(1,1+tiny),(1,2)]
    assert str(result.crs)==crs


def test_dangling_points_preserves_source_ids_order_dtype_custom_geometry_and_source():
    import pandas as pd
    frame=lines([LineString([(0,0),(4,0)]),LineString([(2,0),(6,0)]),LineString([(10,0),(11,0)])])
    frame["line_id"]=pd.array([2**60+7,2**60+3,2**60+1],dtype="Int64")
    frame["source_id"]=["business"]*3
    frame=frame.rename_geometry("route");frame.index=[9,9,1];before=frame.copy(deep=True)
    result=get_operator("check_dangling_points")(input_gdf=frame,id_field="line_id")
    assert result.source_id.tolist()==[2**60+7,2**60+3,2**60+1,2**60+1]
    assert str(result.source_id.dtype)=="Int64"
    assert list(zip(result.geometry.x,result.geometry.y))==[(0,0),(6,0),(10,0),(11,0)]
    assert_geodataframe_equal(frame,before)


def test_dangling_points_empty_batch_retains_source_id_schema_and_crs():
    import pandas as pd
    frame=lines([]);frame["identity"]=pd.Series([],dtype="string")
    result=get_operator("check_dangling_points")(input_gdf=frame,id_field="identity")
    assert result.empty and result.columns.tolist()==["source_id","geometry"]
    assert str(result.source_id.dtype)=="string" and result.crs==frame.crs


@pytest.mark.parametrize("value", [None,"", "missing", "geometry", "boundary"])
def test_dangling_points_rejects_missing_or_geometry_id_field(value):
    with pytest.raises(ValueError,match="id_field"):
        get_operator("check_dangling_points")(input_gdf=lines([LineString([(0,0),(1,0)])]),id_field=value)


@pytest.mark.parametrize("ids", [[None,1],[1,1],[" ","x"],[[1],[2]]])
def test_dangling_points_rejects_non_scalar_missing_blank_or_duplicate_ids(ids):
    with pytest.raises(ValueError,match="identifiers"):
        dangles([LineString([(0,0),(1,0)]),LineString([(2,0),(3,0)])],ids)


@pytest.mark.parametrize("geometry", [None,Point(0,0),box(0,0,1,1),LineString(),
    LineString([(0,0),(0,0)]),LineString([(0,0,0),(1,0,0)]),
    shapely.from_wkt("LINESTRING M (0 0 1,1 0 1)"),LineString([(0,0),(float('inf'),0)])])
def test_dangling_points_rejects_missing_empty_invalid_non_line_or_non_xy(geometry):
    with pytest.raises(ValueError):dangles([geometry])


@pytest.mark.parametrize("crs", [None,"EPSG:4326","OGC:CRS84"])
def test_dangling_points_rejects_unknown_or_geographic_crs(crs):
    with pytest.raises(ValueError,match="projected CRS"):
        dangles([LineString([(0,0),(1,0)])],crs=crs)


def test_dangling_points_requires_active_geodataframe_and_unique_columns():
    import pandas as pd
    operator=get_operator("check_dangling_points")
    for frame in [pd.DataFrame({"id":[1]}),gpd.GeoDataFrame({"id":[1]})]:
        with pytest.raises(ValueError):operator(input_gdf=frame,id_field="id")
    frame=lines([LineString([(0,0),(1,0)])]);frame["id"]=1;frame["other"]=2
    frame.columns=["source_name","boundary","id","id"]
    with pytest.raises(ValueError,match="id_field"):operator(input_gdf=frame,id_field="id")


@pytest.mark.parametrize("closed", [False,True])
def test_dangling_points_workflow_native_point_save_reload_and_source_unchanged(tmp_path,closed):
    from osgeo import ogr
    frame=lines([box(0,0,1,1).boundary if closed else LineString([(0,0),(1,0)])]);frame["line_id"]=[19]
    frame.to_file(tmp_path/"source.gpkg",driver="GPKG");before=(tmp_path/"source.gpkg").read_bytes()
    connection={"engine_type":"nfs","mount_path":str(tmp_path)}
    result=execute_workflow({"tasks":[
        {"id":"load","operator":"load","params":{"connection_info":connection,"path":"source.gpkg"},"depends_on":[]},
        {"id":"check","operator":"check_dangling_points","params":{"input_gdf":{"$ref":"load"},"id_field":"line_id"},"depends_on":["load"]},
        {"id":"save","operator":"save","params":{"input_df":{"$ref":"check"},"connection_info":connection,"path":"dangles.gpkg","mode":"fail"},"depends_on":["check"]}]})
    assert result["status"]=="success",result.get("error")
    dataset=ogr.Open(str(tmp_path/"dangles.gpkg"),0);layer=dataset.GetLayer(0)
    assert layer.GetFeatureCount()==(0 if closed else 2) and layer.GetSpatialRef().GetAuthorityCode(None)=="3857"
    rows=[(f.GetField("source_id"),f.GetGeometryRef().GetX(),f.GetGeometryRef().GetY()) for f in layer]
    assert rows==([] if closed else [(19,0,0),(19,1,0)])
    dataset=None
    assert (tmp_path/"source.gpkg").read_bytes()==before


def test_dangling_points_discovery_workflow_only_and_required_explicit_id():
    op=next(o for o in list_operators() if o["id"]=="check_dangling_points")
    assert op["display_name"]=="悬点检查" and op["category_path"]==["矢量处理","拓扑检查"]
    assert op["execution_modes"]==["workflow"] and op["effects"]==["read"]
    assert [(p["name"],p["param_type"],p["required"]) for p in op["parameters"]]==[("input_gdf","input",True),("id_field","param",True)]
    assert [(p["name"],p["type"],p["is_default"]) for p in op["output_ports"]]==[("result","geodataframe",True)]


def test_dangling_points_async_runtime_discovery_direct_denial_and_no_implicit_id(tmp_path,monkeypatch):
    import api_server
    from addp_common.workflow_runtime import ExecutionRegistry
    frame=lines([LineString([(0,0),(1,0)])]);frame["line_id"]=[17]
    frame.to_file(tmp_path/"source.gpkg",driver="GPKG")
    connection={"engine_type":"nfs","mount_path":str(tmp_path)}
    tasks=[{"id":"load","operator":"load","params":{"connection_info":connection,"path":"source.gpkg"},"depends_on":[]},
           {"id":"check","operator":"check_dangling_points","params":{"input_gdf":{"$ref":"load"},"id_field":"line_id"},"depends_on":["load"]}]
    monkeypatch.setattr(api_server,"executions",ExecutionRegistry())
    with api_server.app.test_client() as client:
        assert any(o["id"]=="check_dangling_points" for o in client.get("/api/operators").get_json()["operators"])
        assert client.post("/api/operators/check_dangling_points/invoke",json={"params":{}}).status_code==403
        assert client.post("/api/workflow",json={"workflow_def":{"tasks":tasks}}).status_code==400
        response=client.post("/api/workflow",json={"workflow_def":{"tasks":tasks},"runtime":{"tenant_id":7,"execution_authorization":{"id":1,"effects":["read"]}}})
        assert response.status_code==202
        deadline=time.monotonic()+5
        while time.monotonic()<deadline:
            state=client.get('/api/executions/'+response.get_json()['execution_id']).get_json()
            if state['status'] in ('success','failed'):break
            time.sleep(.01)
        assert state['status']=='success',state
        features=json.loads(state['result'])['features']
        assert [feature['properties']['source_id'] for feature in features]==[17,17]
        assert [feature['geometry'] for feature in features]==[
            {'type':'Point','coordinates':[0,0]}, {'type':'Point','coordinates':[1,0]}]
    tasks[1]['params'].pop('id_field')
    tasks.append({"id":"save","operator":"save","params":{"input_df":{"$ref":"check"},"connection_info":connection,"path":"must-not-exist.gpkg","mode":"fail"},"depends_on":["check"]})
    failed=execute_workflow({'tasks':tasks})
    assert failed['status'] == 'failed' and failed['error_code'] == 'INVALID_PARAMS'
    assert failed['diagnostic'] == {
        'schema_version': 'addp.execution-failure/v3', 'stage': 'execution',
        'reason': 'operator_input_invalid',
        'operator': {'node_index': 2, 'operator_id': 'check_dangling_points', 'parameter': 'input_gdf'},
    }
    assert not (tmp_path/'must-not-exist.gpkg').exists()


@pytest.mark.parametrize("problem,reason", [
    ("missing_source", "operator_input_invalid"),
    ("invalid_line", "operator_input_invalid"),
    ("identifier", "operator_input_invalid"),
    ("crs", "operator_crs_invalid"),
    ("native_check", None),
    ("native_noding", None),
    ("missing_association", None),
])
def test_dangling_points_diagnostics_block_result_save(monkeypatch, problem, reason):
    import workflow_engine
    from addp_common.workflow_runtime import WorkflowExecutionError

    end = (0, 0) if problem == "invalid_line" else (1, 0)
    frame = lines([LineString([(0, 0), end])], crs=None if problem == "crs" else "EPSG:3857")
    frame["line_id"] = [1]
    params = {"input_gdf": frame, "id_field": "PRIVATE_SOURCE_FIELD" if problem == "identifier" else "line_id"}
    if problem == "missing_source":
        del params["input_gdf"]
    op = "check_dangling_points"
    saves = []
    original = workflow_engine.get_operator

    def resolve(name):
        if name == "save":
            return lambda **kwargs: saves.append(kwargs)
        function = original(name)
        if name != op:
            return function

        def invoke(**kwargs):
            def native_failure(*args, **kwargs):
                raise ValueError("input_gdf requires valid lines")

            with monkeypatch.context() as scope:
                if problem == "native_check":
                    scope.setattr(shapely, "is_valid", native_failure)
                elif problem == "native_noding":
                    scope.setattr(shapely, "node", native_failure)
                elif problem == "missing_association":
                    scope.setattr(shapely.STRtree, "query", lambda *args, **kwargs: [])
                return function(**kwargs)

        return invoke

    monkeypatch.setattr(workflow_engine, "get_operator", resolve)
    tasks = [
        {"id": "operation", "operator": op, "params": params, "depends_on": []},
        {"id": "save_result", "operator": "save", "depends_on": ["operation"],
         "params": {"input_df": {"$ref": "operation", "port": "result"}}},
    ]
    with pytest.raises(WorkflowExecutionError) as error:
        workflow_engine.GeoPythonWorkflowRunner(language="en").execute({"tasks": tasks})
    assert not saves
    if reason is None:
        assert error.value.error_code == "EXECUTION_FAILED"
        assert error.value.diagnostic is None
        return
    context = {"node_index": 1, "operator_id": op}
    if reason == "operator_input_invalid":
        context["parameter"] = "input_gdf"
    assert error.value.error_code == "INVALID_PARAMS"
    assert error.value.diagnostic == {
        "schema_version": "addp.execution-failure/v3", "stage": "execution",
        "reason": reason, "operator": context,
    }
    assert "PRIVATE_SOURCE_FIELD" not in str(error.value)


def test_dangling_points_source_lookup_uses_exact_lines_not_bounding_boxes():
    result=dangles([box(0,0,4,4).boundary,LineString([(1,1),(2,1)])],["ring","isolated"])
    assert list(zip(result.source_id,result.geometry.x,result.geometry.y))==[("isolated",1,1),("isolated",2,1)]


def test_polygon_overlap_empty_string_ids_use_shared_scalar_validation():
    import pandas as pd
    frame=overlap_frame([]);frame["parcel_id"]=pd.Series([],dtype="string")
    result=overlap(frame)
    assert result.empty and str(result.source_id_a.dtype)==str(result.source_id_b.dtype)=="string"


# Distinct-feature point intersections complement positive-length overlap checks.
def line_intersection(frame, id_field="route_key"):
    return get_operator("check_line_intersection")(input_gdf=frame, id_field=id_field)


@pytest.mark.parametrize("other,expected", [
    (LineString([(1,-1),(1,1)]), [(1,0)]),
    (LineString([(1,0),(1,1)]), [(1,0)]),
    (LineString([(2,0),(3,1)]), [(2,0)]),
    (LineString([(2,0),(3,0)]), [(2,0)]),
    (LineString([(0,0),(1,0),(2,0)]), []),
    (LineString([(2,0),(0,0)]), []),
    (LineString([(0,2**-40),(2,2**-40)]), []),
    (LineString([(2+2**-40,0),(3,0)]), []),
])
def test_line_intersection_exact_point_contacts_without_overlap_endpoints(other, expected):
    result=line_intersection(line_overlap_frame([LineString([(0,0),(2,0)]),other],["甲","乙"]))
    assert list(zip(result.source_id_a,result.source_id_b))==[("甲","乙")]*len(expected)
    assert [list(point.coords)[0] for point in result.geometry]==expected
    assert result.geometry.geom_type.tolist()==["Point"]*len(expected)


def test_line_intersection_mixed_collection_keeps_only_isolated_points():
    a=MultiLineString([[(0,0),(3,0)],[(0,2),(3,2)],[(5,0),(5,2)]])
    b=MultiLineString([[(1,0),(2,0)],[(1,2),(2,2)],[(4,1),(6,1)]])
    frame=line_overlap_frame([a,b],["主线","支线"])
    result=line_intersection(frame)
    assert list(zip(result.source_id_a,result.source_id_b,result.geometry.x,result.geometry.y))==[("主线","支线",5,1)]
    assert line_overlap(frame).overlap_length.tolist()==[2]


def test_line_intersection_pair_identity_point_dedup_order_and_source_unchanged():
    frame=line_overlap_frame([
        MultiLineString([[(0,0),(4,0)],[(4,0),(0,0)]]),
        MultiLineString([[(3,-1),(3,1)],[(1,1),(1,-1)],[(1,-1),(1,1)]]),
        LineString([(1,-2),(1,2)]),
    ],pd.Series([90,10,30],dtype="Int64"))
    frame.index=[8,8,2];before=frame.copy(deep=True)
    result=line_intersection(frame)
    assert result.columns.tolist()==["source_id_a","source_id_b","geometry"]
    assert list(zip(result.source_id_a,result.source_id_b,result.geometry.x,result.geometry.y))==[
        (90,10,1,0),(90,10,3,0),(90,30,1,0)]
    # The last pair overlaps along a line; its endpoints are not point intersections.
    assert result.source_id_a.dtype==result.source_id_b.dtype==frame.route_key.dtype
    assert result.crs==frame.crs
    assert_geodataframe_equal(frame,before)
    reversed_frame=frame.copy();reversed_frame[frame.geometry.name]=shapely.reverse(frame.geometry.array)
    assert_geodataframe_equal(line_intersection(reversed_frame),result)


def test_line_intersection_three_sources_same_point_retain_three_pairs():
    frame=line_overlap_frame([LineString([(-1,0),(1,0)]),LineString([(0,-1),(0,1)]),
                              LineString([(-1,-1),(1,1)])],["丙","甲","乙"])
    result=line_intersection(frame)
    assert list(zip(result.source_id_a,result.source_id_b))==[("丙","甲"),("丙","乙"),("甲","乙")]
    assert list(zip(result.geometry.x,result.geometry.y))==[(0,0)]*3


@pytest.mark.parametrize("geometry", [LineString([(0,0),(2,2),(0,2),(2,0)]),
    MultiLineString([[(0,0),(2,0)],[(1,-1),(1,1)]])])
def test_line_intersection_does_not_check_within_single_feature(geometry):
    assert line_intersection(line_overlap_frame([geometry])).empty


@pytest.mark.parametrize("crs",["EPSG:3857","EPSG:2263"])
def test_line_intersection_retains_distinct_binary_tiny_points_and_original_units(crs):
    tiny=2**-40
    frame=line_overlap_frame([LineString([(0,0),(1,0)]),
        MultiLineString([[(tiny,-1),(tiny,1)],[(0,-1),(0,1)]])],crs=crs)
    result=line_intersection(frame)
    assert list(zip(result.geometry.x,result.geometry.y))==[(0,0),(tiny,0)]
    assert result.crs==frame.crs


def test_line_intersection_bbox_candidates_require_real_intersection():
    frame=line_overlap_frame([LineString([(0,0),(2,2)]),LineString([(0,1),(.4,1.4)])])
    assert line_intersection(frame).empty


@pytest.mark.parametrize("ids",[pd.Series([2**60,2**60+1],dtype="Int64"),pd.Series(["甲","乙"],dtype="string")])
def test_line_intersection_preserves_identifier_type_and_empty_schema(ids):
    frame=line_overlap_frame([LineString([(0,0),(2,0)]),LineString([(1,-1),(1,1)])],ids)
    for source in [frame.iloc[:0],frame.iloc[:1],frame]:
        result=line_intersection(source)
        assert result.columns.tolist()==["source_id_a","source_id_b","geometry"]
        assert result.source_id_a.dtype==result.source_id_b.dtype==ids.dtype and result.crs==source.crs
    result=line_intersection(frame)
    assert result.source_id_a.tolist()==ids.iloc[:1].tolist() and result.source_id_b.tolist()==ids.iloc[1:].tolist()


@pytest.mark.parametrize("ids",[[1,1],[None,1],[" ","x"],[[1],[2]]])
def test_line_intersection_rejects_invalid_source_ids(ids):
    with pytest.raises(ValueError,match="identifiers"):
        line_intersection(line_overlap_frame([LineString([(0,0),(1,0)])]*2,ids))


@pytest.mark.parametrize("field",[None,"","missing","boundary"])
def test_line_intersection_requires_explicit_nongeometry_id(field):
    with pytest.raises(ValueError,match="id_field"):
        line_intersection(line_overlap_frame([LineString([(0,0),(1,0)])]),field)


@pytest.mark.parametrize("crs",[None,"EPSG:4326"])
def test_line_intersection_rejects_unknown_or_geographic_crs(crs):
    with pytest.raises(ValueError,match="projected"):
        line_intersection(line_overlap_frame([],crs=crs))


@pytest.mark.parametrize("geometry",[None,LineString(),Point(0,0),box(0,0,1,1),
    shapely.from_wkt("MULTILINESTRING (EMPTY, (0 0, 1 0))"),LineString([(0,0),(0,0)]),
    LineString([(0,0,1),(1,0,1)]),shapely.from_wkt("LINESTRING M (0 0 1, 1 0 1)"),
    LineString([(0,0),(float("inf"),1)])])
def test_line_intersection_rejects_unsupported_geometry(geometry):
    with pytest.raises(ValueError):line_intersection(line_overlap_frame([geometry]))


def test_line_intersection_requires_active_geodataframe_and_unique_columns():
    operator=get_operator("check_line_intersection")
    with pytest.raises(ValueError):operator(input_gdf=pd.DataFrame(),id_field="route_key")
    frame=line_overlap_frame([LineString([(0,0),(1,0)])])
    frame=pd.concat([frame,frame[["route_key"]]],axis=1)
    with pytest.raises(ValueError):operator(input_gdf=frame,id_field="route_key")
    frame=line_overlap_frame([LineString([(0,0),(1,0)])]).drop(columns="boundary")
    with pytest.raises(ValueError):operator(input_gdf=frame,id_field="route_key")


def test_line_intersection_matches_independent_integer_grid_oracle():
    horizontal=[(0,4,0),(2,6,2),(-2,2,4)];vertical=[(1,-1,5),(3,0,4),(5,1,3)]
    shapes=[LineString([(a,y),(b,y)]) for a,b,y in horizontal]+[LineString([(x,a),(x,b)]) for x,a,b in vertical]
    frame=line_overlap_frame(shapes)
    expected=[(i,len(horizontal)+j,x,y) for i,(a,b,y) in enumerate(horizontal)
              for j,(x,c,d) in enumerate(vertical) if a<=x<=b and c<=y<=d]
    result=line_intersection(frame)
    assert list(zip(result.source_id_a,result.source_id_b,result.geometry.x,result.geometry.y))==expected


@pytest.mark.parametrize("empty",[False,True])
def test_line_intersection_workflow_native_point_save_reload_and_discovery(tmp_path,empty):
    from osgeo import ogr
    tiny=2**-40
    source=line_overlap_frame([LineString([(0,0),(2,0)]),
        MultiLineString([[(2,-1),(2,1)],[(tiny,-1),(tiny,1)],[(0,-1),(0,1)]])],["路线甲","路线乙"])
    if empty:source=source.iloc[:1]
    source.to_file(tmp_path/"source.gpkg",layer="routes",driver="GPKG")
    original=(tmp_path/"source.gpkg").read_bytes();connection={"engine_type":"nfs","mount_path":str(tmp_path)}
    tasks=[{"id":"load","operator":"load","params":{"connection_info":connection,"path":"source.gpkg"},"depends_on":[]},
        {"id":"check","operator":"check_line_intersection","params":{"input_gdf":{"$ref":"load"},"id_field":"route_key"},"depends_on":["load"]},
        {"id":"save","operator":"save","params":{"input_df":{"$ref":"check"},"connection_info":connection,"path":"points.gpkg","mode":"fail"},"depends_on":["check"]}]
    execution=execute_workflow({"tasks":tasks});assert execution["status"]=="success",execution.get("error")
    dataset=ogr.Open(str(tmp_path/"points.gpkg"));layer=dataset.GetLayer(0)
    assert layer.GetFeatureCount()==(0 if empty else 3)
    assert layer.GetSpatialRef().GetAuthorityCode(None)=="3857"
    assert [field.name for field in layer.schema]==["source_id_a","source_id_b"]
    assert layer.GetGeomType()==(ogr.wkbUnknown if empty else ogr.wkbPoint)
    if not empty:
        for i,(feature,x) in enumerate(zip(layer,[0,tiny,2]),1):
            geometry=feature.GetGeometryRef()
            assert feature.GetFID()==i and feature.GetField("source_id_a")=="路线甲" and feature.GetField("source_id_b")=="路线乙"
            assert geometry.GetGeometryName()=="POINT" and geometry.IsValid() and geometry.GetCoordinateDimension()==2
            assert geometry.GetPoint_2D()==(x,0)
    dataset=None
    assert (tmp_path/"source.gpkg").read_bytes()==original
    tasks[1]["params"]["id_field"]="missing";tasks[2]["params"]["path"]="rejected.gpkg"
    assert execute_workflow({"tasks":tasks})["status"]=="failed"
    assert not (tmp_path/"rejected.gpkg").exists()


def test_line_intersection_async_runtime_discovery_and_required_identity(tmp_path,monkeypatch):
    import api_server
    from addp_common.workflow_runtime import ExecutionRegistry
    frame=line_overlap_frame([LineString([(0,0),(2,0)]),LineString([(1,-1),(1,1)])],[17,23])
    frame.to_file(tmp_path/"source.gpkg",driver="GPKG")
    connection={"engine_type":"nfs","mount_path":str(tmp_path)}
    tasks=[{"id":"load","operator":"load","params":{"connection_info":connection,"path":"source.gpkg"},"depends_on":[]},
        {"id":"check","operator":"check_line_intersection","params":{"input_gdf":{"$ref":"load"},"id_field":"route_key"},"depends_on":["load"]}]
    monkeypatch.setattr(api_server,"executions",ExecutionRegistry())
    with api_server.app.test_client() as client:
        op=next(o for o in client.get("/api/operators").get_json()["operators"] if o["id"]=="check_line_intersection")
        assert op["display_name"]=="线交检查" and op["category_path"]==["矢量处理","拓扑检查"]
        assert op["execution_modes"]==["workflow"] and op["effects"]==["read"]
        assert [(p["name"],p["param_type"],p["required"]) for p in op["parameters"]]==[("input_gdf","input",True),("id_field","param",True)]
        assert [(p["name"],p["type"],p["is_default"]) for p in op["output_ports"]]==[("result","geodataframe",True)]
        assert client.post("/api/operators/check_line_intersection/invoke",json={"params":{}}).status_code==403
        assert client.post("/api/workflow",json={"workflow_def":{"tasks":tasks}}).status_code==400
        response=client.post("/api/workflow",json={"workflow_def":{"tasks":tasks},"runtime":{"tenant_id":7,"execution_authorization":{"id":1,"effects":["read"]}}})
        assert response.status_code==202
        deadline=time.monotonic()+5
        while time.monotonic()<deadline:
            state=client.get('/api/executions/'+response.get_json()['execution_id']).get_json()
            if state['status'] in ('success','failed'):break
            time.sleep(.01)
        assert state['status']=='success',state
        features=json.loads(state['result'])['features']
        assert len(features)==1 and features[0]['type']=='Feature'
        assert features[0]['properties']=={'source_id_a':17,'source_id_b':23}
        assert features[0]['geometry']=={'type':'Point','coordinates':[1,0]}
    tasks[1]['params'].pop('id_field')
    failed=execute_workflow({'tasks':tasks})
    assert failed['status']=='failed' and failed['error_code']=='INVALID_PARAMS'
    assert failed['diagnostic']['operator']=={
        'node_index':2,'operator_id':'check_line_intersection','parameter':'input_gdf'}


@pytest.mark.parametrize('operator,problem,reason', [
    (operator, problem, reason)
    for operator in ('check_line_intersection', 'check_line_self_intersection',
                     'check_line_overlap', 'check_line_self_overlap')
    for problem, reason in (
        ('missing_layer', 'operator_input_invalid'),
        ('missing_identifier', 'operator_input_invalid'),
        ('invalid_identifier', 'operator_input_invalid'),
        ('invalid_line', 'operator_input_invalid'),
        ('crs', 'operator_crs_invalid'),
        ('native_check', None),
        ('native_intersection', None),
        ('length_overflow' if operator.endswith('overlap') else 'result_validation', None),
    )
])
def test_line_check_diagnostics_block_result_save(monkeypatch, operator, problem, reason):
    import workflow_engine
    from addp_common.workflow_runtime import WorkflowExecutionError
    from operators import geometric_operators

    crossing = LineString([(0, 0), (2, 2), (0, 2), (2, 0)])
    second = LineString([(1, -1), (1, 3)])
    if problem == 'invalid_line':
        second = LineString([(1, 1), (1, 1)])
    shapes = [crossing, second]
    if problem == 'length_overflow':
        if operator == 'check_line_overlap':
            shapes = [LineString([(-1e308, 0), (1e308, 0)])] * 2
        else:
            shapes = [MultiLineString([[(0, 0), (1e308, 0)], [(1e308, 0), (0, 0)],
                                       [(0, 2), (1e308, 2)], [(1e308, 2), (0, 2)]])]
    frame = line_overlap_frame(shapes, crs=None if problem == 'crs' else 'EPSG:3857')
    params = {'input_gdf': frame, 'id_field': 'route_key'}
    if problem == 'missing_layer':
        del params['input_gdf']
    elif problem == 'missing_identifier':
        del params['id_field']
    elif problem == 'invalid_identifier':
        params['id_field'] = 'PRIVATE_SOURCE_FIELD'

    native_message = 'input_gdf requires non-empty valid lines'

    def fail(*args, **kwargs):
        raise ValueError(native_message)

    if problem == 'native_check':
        monkeypatch.setattr(shapely, 'is_valid', fail)
    elif problem == 'native_intersection':
        monkeypatch.setattr(shapely, 'intersection', fail)
    elif problem == 'result_validation':
        validate = geometric_operators._finite_xy_geometry

        def validate_with_failure(frame, name='input_gdf', **kwargs):
            if name == 'result':
                fail()
            return validate(frame, name, **kwargs)

        monkeypatch.setattr(geometric_operators, '_finite_xy_geometry', validate_with_failure)

    saves = []
    resolve = workflow_engine.get_operator

    def resolve_with_save_probe(name):
        if name == 'save':
            return lambda **kwargs: saves.append(kwargs)
        return resolve(name)

    monkeypatch.setattr(workflow_engine, 'get_operator', resolve_with_save_probe)
    tasks = [
        {'id': 'check', 'operator': operator, 'params': params, 'depends_on': []},
        {'id': 'save_result', 'operator': 'save', 'depends_on': ['check'],
         'params': {'input_df': {'$ref': 'check', 'port': 'result'}}},
    ]
    with np.errstate(over='ignore', invalid='ignore'), pytest.raises(WorkflowExecutionError) as caught:
        workflow_engine.GeoPythonWorkflowRunner(language='en').execute({'tasks': tasks})
    assert not saves
    error = caught.value
    if reason is None:
        assert error.error_code == 'EXECUTION_FAILED' and error.diagnostic is None
        expected_message = 'finite positive Float64 length' if problem == 'length_overflow' else native_message
        assert expected_message in str(error)
        return

    context = {'node_index': 1, 'operator_id': operator}
    if reason == 'operator_input_invalid':
        context['parameter'] = 'input_gdf'
    assert error.error_code == 'INVALID_PARAMS'
    assert error.diagnostic == {
        'schema_version': 'addp.execution-failure/v3', 'stage': 'execution',
        'reason': reason, 'operator': context,
    }
    assert 'PRIVATE_SOURCE_FIELD' not in str(error)


def line_self_intersection(frame, id_field="route_key"):
    return get_operator("check_line_self_intersection")(input_gdf=frame,id_field=id_field)


@pytest.mark.parametrize("shape,expected",[
    (LineString([(0,0),(2,2),(0,2),(2,0)]),[(1,1)]),
    (LineString([(0,0),(1,0),(1,1),(0,1),(1,0),(2,0)]),[(1,0)]),
    (MultiLineString([[(0,0),(2,0)],[(1,-1),(1,1)],[(2,0),(3,1)]]),[(1,0),(2,0)]),
    (MultiLineString([[(0,0),(2,0)],[(2,0),(2,2)],[(2,2),(0,2)],[(0,2),(0,0)]]),[(0,0),(0,2),(2,0),(2,2)]),
])
def test_line_self_intersection_reports_nonadjacent_and_cross_part_isolated_contacts(shape,expected):
    frame=line_overlap_frame([shape],["线路甲"]);before=frame.copy(deep=True)
    result=line_self_intersection(frame)
    assert result.columns.tolist()==["source_id","geometry"]
    assert result.source_id.tolist()==["线路甲"]*len(expected)
    assert [list(point.coords)[0] for point in result.geometry]==expected
    assert result.geometry.geom_type.tolist()==["Point"]*len(expected) and result.crs==frame.crs
    assert_geodataframe_equal(frame,before)
    reversed_frame=frame.copy();reversed_frame[frame.geometry.name]=shapely.reverse(frame.geometry.array)
    assert_geodataframe_equal(line_self_intersection(reversed_frame),result)


@pytest.mark.parametrize("shape",[
    LineString([(0,0),(1,0),(2,1)]),
    LineString([(0,0),(1,0),(1,0),(2,1)]),
    LineString([(0,0),(0,0),(2,0),(2,2),(0,2),(0,0),(0,0)]),
    LineString([(0,0),(2,0),(2,2),(0,2),(0,0)]),
    LineString([(0,0),(4,0),(0,0)]),
    MultiLineString([[(0,0),(1,0),(2,0)],[(0,0),(1,0),(2,0)]]),
    MultiLineString([[(0,0),(4,0)],[(4,0),(2,0),(0,0)]]),
    MultiLineString([[(0,0),(2,0)],[(0,2**-40),(2,2**-40)]]),
])
def test_line_self_intersection_excludes_normal_vertices_closed_seams_retracing_and_near_parts(shape):
    assert line_self_intersection(line_overlap_frame([shape])).empty


@pytest.mark.parametrize("x,expected",[(1,[(1,0)]),(2,[]),(3,[]),(4,[]),(5,[(5,0)])])
def test_line_self_intersection_excludes_overlap_positions_including_endpoints_and_other_branches(x,expected):
    shape=MultiLineString([[(0,0),(4,0)],[(2,0),(6,0)],[(x,-1),(x,1)]])
    result=line_self_intersection(line_overlap_frame([shape]))
    assert list(zip(result.geometry.x,result.geometry.y))==expected


def test_line_self_intersection_collects_adjacent_retracing_before_filtering_crossings():
    frame=line_overlap_frame([MultiLineString([[(0,0),(4,0),(0,0)],[(2,-1),(2,1)]])])
    assert line_self_intersection(frame).empty
    assert self_overlap(frame).overlap_length.tolist()==[4]


def test_line_self_intersection_preserves_isolated_crossing_beside_overlap_and_tiny_coverage():
    shape=MultiLineString([[(0,0),(4,0)],[(2,0),(6,0)],[(3,-1),(3,1)],
        [(10,0),(12,2),(10,2),(12,0)]])
    result=line_self_intersection(line_overlap_frame([shape]))
    assert list(zip(result.geometry.x,result.geometry.y))==[(11,1)]
    tiny=2**-600
    frame=line_overlap_frame([MultiLineString([[(0,0),(tiny,0)],[(tiny,0),(0,0)],
        [(tiny/2,-tiny),(tiny/2,tiny)]])])
    assert line_self_intersection(frame).empty


@pytest.mark.parametrize("crs",["EPSG:3857","EPSG:2263"])
def test_line_self_intersection_preserves_binary_distinct_points_without_threshold(crs):
    tiny=2**-40
    shape=MultiLineString([[(0,0),(1,0)],[(0,-1),(0,1)],[(tiny,-1),(tiny,1)]])
    result=line_self_intersection(line_overlap_frame([shape],crs=crs))
    assert list(zip(result.geometry.x,result.geometry.y))==[(0,0),(tiny,0)] and result.crs==crs


def test_line_self_intersection_does_not_compare_distinct_features_or_deduplicate_between_sources():
    cross=line_overlap_frame([LineString([(0,0),(2,0)]),LineString([(1,-1),(1,1)])])
    assert line_self_intersection(cross).empty and len(line_intersection(cross))==1
    bow=LineString([(0,0),(2,2),(0,2),(2,0)])
    frame=line_overlap_frame([bow,bow],["乙","甲"]);frame.index=[5,5]
    result=line_self_intersection(frame)
    assert result.source_id.tolist()==["乙","甲"] and list(zip(result.geometry.x,result.geometry.y))==[(1,1)]*2


@pytest.mark.parametrize("ids",[pd.Series([2**60,2**60+1],dtype="Int64"),pd.Series(["甲","乙"],dtype="string")])
def test_line_self_intersection_keeps_identifier_type_and_empty_schema(ids):
    bow=LineString([(0,0),(2,2),(0,2),(2,0)])
    frame=line_overlap_frame([bow,LineString([(0,0),(1,0)])],ids)
    for source in [frame.iloc[:0],frame.iloc[1:],frame]:
        result=line_self_intersection(source)
        assert result.columns.tolist()==["source_id","geometry"] and result.source_id.dtype==ids.dtype and result.crs==source.crs
    assert line_self_intersection(frame).source_id.tolist()==ids.iloc[:1].tolist()


@pytest.mark.parametrize("ids",[[1,1],[None,1],[" ","x"],[[1],[2]]])
def test_line_self_intersection_rejects_invalid_identifiers(ids):
    with pytest.raises(ValueError,match="identifiers"):
        line_self_intersection(line_overlap_frame([LineString([(0,0),(1,0)])]*2,ids))


@pytest.mark.parametrize("field",[None,"","missing","boundary"])
def test_line_self_intersection_requires_explicit_nongeometry_identity(field):
    with pytest.raises(ValueError,match="id_field"):
        line_self_intersection(line_overlap_frame([LineString([(0,0),(1,0)])]),field)


@pytest.mark.parametrize("crs",[None,"EPSG:4326"])
def test_line_self_intersection_rejects_nonprojected_crs(crs):
    with pytest.raises(ValueError,match="projected"):
        line_self_intersection(line_overlap_frame([],crs=crs))


@pytest.mark.parametrize("shape",[None,LineString(),Point(0,0),box(0,0,1,1),
    shapely.from_wkt("MULTILINESTRING (EMPTY, (0 0, 1 0))"),LineString([(0,0),(0,0)]),
    LineString([(0,0,1),(1,0,1)]),shapely.from_wkt("LINESTRING M (0 0 1, 1 0 1)"),
    LineString([(0,0),(float('inf'),1)])])
def test_line_self_intersection_rejects_unsupported_geometry(shape):
    with pytest.raises(ValueError):line_self_intersection(line_overlap_frame([shape]))


def test_line_self_intersection_requires_active_geodataframe_and_unique_columns():
    operator=get_operator("check_line_self_intersection")
    with pytest.raises(ValueError):operator(input_gdf=pd.DataFrame(),id_field="route_key")
    frame=line_overlap_frame([LineString([(0,0),(1,0)])])
    with pytest.raises(ValueError):operator(input_gdf=pd.concat([frame,frame[['route_key']]],axis=1),id_field="route_key")
    with pytest.raises(ValueError):operator(input_gdf=frame.drop(columns='boundary'),id_field="route_key")


def test_line_self_intersection_matches_independent_integer_grid_oracle():
    horizontal=[(0,4,0),(2,6,2),(-2,2,4)];vertical=[(1,-1,5),(3,0,4),(5,1,3)]
    parts=[[(a,y),(b,y)] for a,b,y in horizontal]+[[(x,a),(x,b)] for x,a,b in vertical]
    expected=sorted({(x,y) for a,b,y in horizontal for x,c,d in vertical if a<=x<=b and c<=y<=d})
    result=line_self_intersection(line_overlap_frame([MultiLineString(parts)]))
    assert list(zip(result.geometry.x,result.geometry.y))==expected


@pytest.mark.parametrize("empty",[False,True])
def test_line_self_intersection_workflow_native_point_save_reload_and_discovery(tmp_path,empty):
    import api_server
    from osgeo import ogr
    tiny=2**-40
    shape=MultiLineString([[(0,0),(1,0)],[(0,-1),(0,1)],[(tiny,-1),(tiny,1)]])
    if empty:shape=LineString([(0,0),(1,0),(1,0),(2,1)])
    source=line_overlap_frame([shape],["原线甲"]);source.to_file(tmp_path/'source.gpkg',layer='routes',driver='GPKG')
    original=(tmp_path/'source.gpkg').read_bytes();connection={'engine_type':'nfs','mount_path':str(tmp_path)}
    tasks=[{'id':'load','operator':'load','params':{'connection_info':connection,'path':'source.gpkg'},'depends_on':[]},
           {'id':'check','operator':'check_line_self_intersection','params':{'input_gdf':{'$ref':'load'},'id_field':'route_key'},'depends_on':['load']},
           {'id':'save','operator':'save','params':{'input_df':{'$ref':'check'},'connection_info':connection,'path':'points.gpkg','mode':'fail'},'depends_on':['check']}]
    execution=execute_workflow({'tasks':tasks});assert execution['status']=='success',execution.get('error')
    dataset=ogr.Open(str(tmp_path/'points.gpkg'));layer=dataset.GetLayer(0)
    assert layer.GetFeatureCount()==(0 if empty else 2) and layer.GetSpatialRef().GetAuthorityCode(None)=='3857'
    assert [field.name for field in layer.schema]==['source_id']
    assert layer.GetGeomType()==(ogr.wkbUnknown if empty else ogr.wkbPoint)
    if not empty:
        for feature,x in zip(layer,[0,tiny]):
            point=feature.GetGeometryRef()
            assert feature.GetField('source_id')=='原线甲' and point.GetGeometryName()=='POINT' and point.IsValid()
            assert point.GetCoordinateDimension()==2 and point.GetPoint_2D()==(x,0)
    dataset=None
    assert (tmp_path/'source.gpkg').read_bytes()==original
    tasks[1]['params'].pop('id_field');tasks[2]['params']['path']='rejected.gpkg'
    assert execute_workflow({'tasks':tasks})['status']=='failed' and not (tmp_path/'rejected.gpkg').exists()
    with api_server.app.test_client() as client:
        op=next(o for o in client.get('/api/operators').get_json()['operators'] if o['id']=='check_line_self_intersection')
        assert op['display_name']=='线内自交' and op['category_path']==['矢量处理','拓扑检查']
        assert op['execution_modes']==['workflow'] and op['effects']==['read']
        assert [(p['name'],p['param_type'],p['required']) for p in op['parameters']]==[('input_gdf','input',True),('id_field','param',True)]
        assert [(p['name'],p['type'],p['is_default']) for p in op['output_ports']]==[('result','geodataframe',True)]
        assert client.post('/api/operators/check_line_self_intersection/invoke',json={'params':{}}).status_code==403


def split_all_intersections(frame, id_field='route_key'):
    return get_operator('split_line_intersections')(frame, id_field, scope='all')


@pytest.mark.parametrize('reverse', [False, True])
def test_split_line_all_internal_crossing_keeps_attributes_direction_and_source(reverse):
    vertices=[(0,0),(2,2),(0,2),(2,0)]
    expected=[[(0,0),(1,1)],[(1,1),(2,2),(0,2),(1,1)],[(1,1),(2,0)]]
    if reverse:
        vertices.reverse();expected=[list(reversed(piece)) for piece in reversed(expected)]
    frame=line_overlap_frame([LineString(vertices)],pd.Series([2**60+1],dtype='Int64'))
    frame['grade']=pd.Series([None],dtype='Int64');frame['label']=pd.Series(['甲'],dtype='string');frame.index=[9]
    before=frame.copy(deep=True);result=split_all_intersections(frame)
    assert [list(g.coords) for g in result.geometry]==expected
    assert result.source_id.tolist()==[2**60+1]*3 and result.part_no.tolist()==[1]*3 and result.segment_no.tolist()==[1,2,3]
    assert result.source_id.dtype==frame.route_key.dtype
    assert result.part_no.dtype==result.segment_no.dtype=='int64'
    attributes=frame.iloc[[0,0,0]].reset_index(drop=True).drop(columns='boundary')
    pd.testing.assert_frame_equal(result[attributes.columns],attributes)
    assert result.geometry.name=='boundary' and result.crs==frame.crs and result.index.equals(pd.RangeIndex(3))
    assert_geodataframe_equal(frame,before)
    with pytest.raises(ValueError,match='internal'):split_at_intersections(frame)


def test_split_line_all_internal_and_cross_feature_meetings_use_same_path():
    frame=line_overlap_frame([LineString([(0,0),(4,4),(0,4),(4,0)]),LineString([(1,-1),(1,5)])],['自交','穿越'])
    result=split_all_intersections(frame)
    expected=[[(0,0),(1,1)],[(1,1),(2,2)],[(2,2),(4,4),(1,4)],[(1,4),(0,4),(1,3)],
              [(1,3),(2,2)],[(2,2),(4,0)],[(1,-1),(1,1)],[(1,1),(1,3)],[(1,3),(1,4)],[(1,4),(1,5)]]
    assert [list(g.coords) for g in result.geometry]==expected
    assert result.source_id.tolist()==['自交']*6+['穿越']*4
    assert result.segment_no.tolist()==list(range(1,7))+list(range(1,5))


@pytest.mark.parametrize('shapes,expected,parts,numbers', [
    ([MultiLineString([[(0,0),(4,0)],[(2,0),(2,2)]])],
     [[(0,0),(2,0)],[(2,0),(4,0)],[(2,0),(2,2)]],[1,1,2],[1,2,1]),
    ([MultiLineString([[(0,0),(1,0)],[(1,0),(2,1)]])],
     [[(0,0),(1,0)],[(1,0),(2,1)]],[1,2],[1,1]),
    ([MultiLineString([[(4,0),(0,0)],[(1,-1),(1,1)]])],
     [[(4,0),(1,0)],[(1,0),(0,0)],[(1,-1),(1,0)],[(1,0),(1,1)]],[1,1,2,2],[1,2,1,2]),
])
def test_split_line_all_same_source_multipart_contacts(shapes,expected,parts,numbers):
    frame=line_overlap_frame(shapes,['多部件']);before=frame.copy(deep=True);result=split_all_intersections(frame)
    assert [list(g.coords) for g in result.geometry]==expected
    assert result.source_id.tolist()==['多部件']*len(expected)
    assert result.part_no.tolist()==parts and result.segment_no.tolist()==numbers
    assert_geodataframe_equal(frame,before)


@pytest.mark.parametrize('reverse', [False,True])
def test_split_line_all_repeated_visits_same_xy_are_distinct_path_occurrences(reverse):
    vertices=[(0,0),(1,0),(1,1),(0,1),(0,0),(-1,0),(-1,-1),(0,-1),(0,0),(1,-1)]
    expected=[vertices[:5],vertices[4:9],vertices[8:]]
    if reverse:
        vertices.reverse();expected=[list(reversed(piece)) for piece in reversed(expected)]
    result=split_all_intersections(line_overlap_frame([LineString(vertices)]))
    assert [list(g.coords) for g in result.geometry]==expected
    assert result.segment_no.tolist()==[1,2,3]
    rebuilt=list(result.geometry.iloc[0].coords)
    for shape in result.geometry.iloc[1:]:rebuilt.extend(list(shape.coords)[1:])
    assert rebuilt==vertices


def test_split_line_all_cut_vertices_keep_every_original_repeated_coordinate():
    point=(1,1);vertices=[(0,0),point,point,(2,2),(0,2),point,point,(2,0),(2,0)]
    result=split_all_intersections(line_overlap_frame([LineString(vertices)]))
    assert [list(g.coords) for g in result.geometry]==[[(0,0),point],[point,point,(2,2),(0,2),point],[point,point,(2,0),(2,0)]]
    rebuilt=list(result.geometry.iloc[0].coords)
    for shape in result.geometry.iloc[1:]:rebuilt.extend(list(shape.coords)[1:])
    assert rebuilt==vertices


@pytest.mark.parametrize('reverse',[False,True])
def test_split_line_all_self_crossed_closed_path_retains_original_seam(reverse):
    vertices=[(0,0),(2,2),(0,2),(2,0),(0,0)]
    expected=[[(0,0),(1,1)],[(1,1),(2,2),(0,2),(1,1)],[(1,1),(2,0),(0,0)]]
    if reverse:
        vertices.reverse();expected=[list(reversed(piece)) for piece in reversed(expected)]
    result=split_all_intersections(line_overlap_frame([LineString(vertices)]))
    assert [list(g.coords) for g in result.geometry]==expected
    assert result.geometry.iloc[0].coords[0]==result.geometry.iloc[-1].coords[-1]==vertices[0]


@pytest.mark.parametrize('vertices', [[(0,0),(1,0),(1,0),(1,1)],[(0,0),(1,0),(1,1),(0,0)],
    [(0,0),(0,0),(1,0),(1,1),(0,0),(0,0)]])
def test_split_line_all_normal_adjacent_and_closed_seam_do_not_create_cuts(vertices):
    frame=line_overlap_frame([LineString(vertices)])
    assert_geodataframe_equal(split_all_intersections(frame),split_at_intersections(frame))


@pytest.mark.parametrize('crs',['EPSG:3857','EPSG:2263'])
@pytest.mark.parametrize('reverse',[False,True])
def test_split_line_all_tiny_internal_multipart_cuts_use_original_position(crs,reverse):
    tiny=2**-40;main=[(-2**50,0),(0,0),(1,0)]
    expected=[[(-2**50,0),(0,0),(tiny,0)],[(tiny,0),(2*tiny,0)],[(2*tiny,0),(1,0)]]
    if reverse:main.reverse();expected=[list(reversed(piece)) for piece in reversed(expected)]
    frame=line_overlap_frame([MultiLineString([main,[(tiny,-1),(tiny,1)],[(2*tiny,-1),(2*tiny,1)]])],crs=crs)
    result=split_all_intersections(frame)
    assert [list(g.coords) for g in result.geometry[:3]]==expected
    assert result.part_no.tolist()==[1,1,1,2,2,3,3] and result.segment_no.tolist()==[1,2,3,1,2,1,2]
    assert result.crs==frame.crs


def test_split_line_all_nonbinary_crossing_reuses_identical_cut_on_each_occurrence():
    result=split_all_intersections(line_overlap_frame([LineString([(0,0),(7,3),(1,2),(5,0)])]))
    assert len(result)==3
    point=result.geometry.iloc[0].coords[-1]
    assert point==result.geometry.iloc[1].coords[0]==result.geometry.iloc[1].coords[-1]==result.geometry.iloc[2].coords[0]


@pytest.mark.parametrize('shapes', [
    [LineString([(0,0),(2,0),(1,0)])],
    [LineString([(0,0),(2,0),(2,1),(0,1),(0,0),(1,0)])],
    [MultiLineString([[(0,0),(2,0)],[(1,0),(3,0)]])],
    [LineString([(0,0),(2,0)]),LineString([(2,0),(0,0)])],
    [LineString([(0,0),(1,0)]),LineString([(2**-40,0),(2**-39,0)])],
])
def test_split_line_all_any_positive_overlap_rejects_entire_batch_without_source_changes(shapes):
    frame=line_overlap_frame(shapes);before=frame.copy(deep=True)
    with pytest.raises(ValueError,match='overlap'):split_all_intersections(frame)
    assert_geodataframe_equal(frame,before)


@pytest.mark.parametrize('scope',[None,True,False,0,[],{},'', 'ALL','all ','within'])
def test_split_line_all_scope_is_explicit_and_strict(scope):
    frame=line_overlap_frame([LineString([(0,0),(1,0)])])
    with pytest.raises(ValueError,match='scope'):get_operator('split_line_intersections')(frame,'route_key',scope=scope)


@pytest.mark.parametrize('empty',[False,True])
def test_split_line_all_workflow_real_save_reload_and_overlap_no_publication(tmp_path,empty):
    from osgeo import ogr
    frame=line_overlap_frame([LineString([(0,0),(2,2),(0,2),(2,0)])],['甲'])
    frame['note']=pd.Series([None],dtype='string');frame['grade']=pd.Series([7],dtype='int64')
    if empty:frame=frame.iloc[:0]
    frame.to_file(tmp_path/'source.gpkg',layer='paths',driver='GPKG');original=(tmp_path/'source.gpkg').read_bytes()
    connection={'engine_type':'nfs','mount_path':str(tmp_path)}
    tasks=[{'id':'load','operator':'load','params':{'connection_info':connection,'path':'source.gpkg'},'depends_on':[]},
        {'id':'split','operator':'split_line_intersections','params':{'input_gdf':{'$ref':'load'},'id_field':'route_key','scope':'all'},'depends_on':['load']},
        {'id':'save','operator':'save','params':{'input_df':{'$ref':'split'},'connection_info':connection,'path':'pieces.gpkg','mode':'fail'},'depends_on':['split']}]
    execution=execute_workflow({'tasks':tasks});assert execution['status']=='success',execution
    ds=ogr.Open(str(tmp_path/'pieces.gpkg'));layer=ds.GetLayer(0)
    assert layer.GetFeatureCount()==(0 if empty else 3) and layer.GetSpatialRef().GetAuthorityCode(None)=='3857'
    assert [f.GetName() for f in layer.schema]==['source_name','route_key','note','grade','source_id','part_no','segment_no']
    for name in ['grade','part_no','segment_no']:assert layer.schema[layer.GetLayerDefn().GetFieldIndex(name)].GetType()==ogr.OFTInteger64
    expected=[[(0,0),(1,1)],[(1,1),(2,2),(0,2),(1,1)],[(1,1),(2,0)]]
    for index,feature in enumerate(layer):
        shape=feature.GetGeometryRef();assert shape.GetGeometryName()=='LINESTRING' and shape.GetCoordinateDimension()==2 and not shape.IsMeasured()
        assert shape.GetPoints()==expected[index] and feature.GetField('source_id')==feature.GetField('route_key')=='甲'
        assert feature.GetField('note') is None and feature.GetField('grade')==7
        assert feature.GetField('part_no')==1 and feature.GetField('segment_no')==index+1
    ds=layer=None;assert (tmp_path/'source.gpkg').read_bytes()==original
    bad=line_overlap_frame([LineString([(0,0),(2,0),(1,0)])]);bad.to_file(tmp_path/'bad.gpkg',driver='GPKG')
    before=(tmp_path/'bad.gpkg').read_bytes();tasks[0]['params']['path']='bad.gpkg';tasks[2]['params']['path']='rejected.gpkg'
    execution=execute_workflow({'tasks':tasks});assert execution['status']=='failed' and 'overlap' in execution['error']
    assert not (tmp_path/'rejected.gpkg').exists() and (tmp_path/'bad.gpkg').read_bytes()==before


def test_split_line_all_metadata_and_async_workflow_use_explicit_scope(tmp_path,monkeypatch):
    import api_server
    from addp_common.workflow_runtime import ExecutionRegistry
    frame=line_overlap_frame([LineString([(0,0),(2,2),(0,2),(2,0)])],['甲']);frame.to_file(tmp_path/'source.gpkg',driver='GPKG')
    tasks=[{'id':'load','operator':'load','params':{'connection_info':{'engine_type':'nfs','mount_path':str(tmp_path)},'path':'source.gpkg'},'depends_on':[]},
           {'id':'split','operator':'split_line_intersections','params':{'input_gdf':{'$ref':'load'},'id_field':'route_key','scope':'all'},'depends_on':['load']}]
    monkeypatch.setattr(api_server,'executions',ExecutionRegistry())
    with api_server.app.test_client() as client:
        listed=[o for o in client.get('/api/operators').get_json()['operators'] if o['id']=='split_line_intersections'];assert len(listed)==1
        scope=next(p for p in listed[0]['parameters'] if p['name']=='scope')
        assert scope['enum']==['between','all'] and scope['default']=='between' and scope['required'] is False
        assert listed[0]['execution_modes']==['workflow'] and listed[0]['effects']==['read']
        assert client.post('/api/operators/split_line_intersections/invoke',json={'params':{}}).status_code==403
        response=client.post('/api/workflow',json={'workflow_def':{'tasks':tasks},'runtime':{'tenant_id':7,'execution_authorization':{'id':1,'effects':['read']}}})
        assert response.status_code==202
        deadline=time.monotonic()+5
        while time.monotonic()<deadline:
            state=client.get('/api/executions/'+response.get_json()['execution_id']).get_json()
            if state['status'] in ('success','failed'):break
            time.sleep(.01)
        assert state['status']=='success',state
        assert len(json.loads(state['result'])['features'])==3

def normalized_lines(frame, id_field='route_key'):
    return get_operator('normalize_line_network')(frame,id_field)


def test_normalize_line_network_exact_edges_and_lossless_occurrence_attributes():
    frame=line_overlap_frame([LineString([(0,0),(4,0),(4,2)]),LineString([(3,0),(1,0)]),LineString([(2,-1),(2,1)])],
        pd.Series([2**60+1,2**60+2,2**60+3],dtype='Int64'))
    frame['note']=pd.Series(['主路',None,'cross'],dtype='string');frame['grade']=pd.Series([7,None,9],dtype='Int64')
    frame['enabled']=pd.Series([True,None,False],dtype='boolean');frame['kind']=pd.Categorical(['a','b','a'])
    frame.index=pd.Index([7,7,-2],name='original');before=frame.copy(deep=True)
    result,members=normalized_lines(frame).values()
    expected=[[(0,0),(1,0)],[(1,0),(2,0)],[(2,0),(3,0)],[(3,0),(4,0)],[(4,0),(4,2)],[(2,-1),(2,0)],[(2,0),(2,1)]]
    assert list(result.columns)==['boundary','edge_id','occurrence_count']
    assert [list(g.coords) for g in result.geometry]==expected
    assert result.edge_id.tolist()==list(range(1,8)) and result.occurrence_count.tolist()==[1,2,2,1,1,1,1]
    assert members.edge_id.tolist()==[1,2,3,4,5,3,2,6,7]
    assert members.source_part_no.tolist()==[1]*9 and members.source_segment_no.tolist()==[1,1,1,1,2,1,1,1,1]
    assert members.piece_no.tolist()==[1,2,3,4,1,1,2,1,2] and members.is_reversed.tolist()==[False]*5+[True]*2+[False]*2
    sources=[0]*5+[1]*2+[2]*2
    attributes=frame.iloc[sources].reset_index(drop=True).drop(columns='boundary')
    pd.testing.assert_frame_equal(members[attributes.columns],attributes)
    assert members.source_id.tolist()==frame.route_key.iloc[sources].tolist()
    assert members.source_id.dtype==frame.route_key.dtype
    assert list(members.columns)==list(frame.columns)+['edge_id','source_id','source_part_no','source_segment_no','piece_no','is_reversed']
    for name in ('edge_id','source_part_no','source_segment_no','piece_no'):assert members[name].dtype=='int64'
    assert members.is_reversed.dtype=='bool' and result.occurrence_count.dtype==result.edge_id.dtype=='int64'
    for piece,edge_id,reverse in zip(members.geometry,members.edge_id,members.is_reversed):
        points=list(result.geometry.iloc[edge_id-1].coords)
        assert list(piece.coords)==(points[::-1] if reverse else points)
    for value in (result,members):assert value.crs==frame.crs and value.index.equals(pd.RangeIndex(len(value)))
    assert_geodataframe_equal(frame,before)


def test_normalize_line_network_repeated_original_segments_and_sampling_are_not_lost():
    frame=line_overlap_frame([MultiLineString([[(0,0),(2,0),(0,0)],[(2,0),(0,0)]]),LineString([(0,0),(1,0),(2,0)])],['重复','细分'])
    result,members=normalized_lines(frame).values()
    assert [list(g.coords) for g in result.geometry]==[[(0,0),(1,0)],[(1,0),(2,0)]]
    assert result.occurrence_count.tolist()==[4,4]
    assert members.edge_id.tolist()==[1,2,2,1,2,1,1,2]
    assert members.source_id.tolist()==['重复']*6+['细分']*2
    assert members.source_part_no.tolist()==[1]*4+[2]*2+[1]*2
    assert members.source_segment_no.tolist()==[1,1,2,2,1,1,1,2]
    assert members.piece_no.tolist()==[1,2,1,2,1,2,1,1]
    assert members.is_reversed.tolist()==[False,False,True,True,True,True,False,False]


@pytest.mark.parametrize('reverse',[False,True])
def test_normalize_line_network_internal_and_cross_feature_points_with_retracing(reverse):
    vertices=[(0,0),(4,4),(0,4),(4,0),(0,0)]
    if reverse:vertices.reverse()
    frame=line_overlap_frame([LineString(vertices),LineString([(1,-1),(1,5)]),LineString([(0,4),(4,4)])])
    result,members=normalized_lines(frame).values()
    assert len(result)==15 and len(members)==17
    assert sum(result.occurrence_count)==len(members)
    for source_no,part in [(0,vertices),(1,[(1,-1),(1,5)]),(2,[(0,4),(4,4)])]:
        source_members=members[members.source_id==source_no]
        for original_no,(start,end) in enumerate(zip(part,part[1:]),1):
            pieces=source_members[source_members.source_segment_no==original_no]
            assert list(pieces.geometry.iloc[0].coords)[0]==start
            assert list(pieces.geometry.iloc[-1].coords)[-1]==end
            assert pieces.piece_no.tolist()==list(range(1,len(pieces)+1))
            assert all(a.coords[-1]==b.coords[0] for a,b in zip(pieces.geometry,pieces.geometry.iloc[1:]))
    # No positive overlap or interior crossing survives among unique edges.
    for a,shape in enumerate(result.geometry):
        for other in result.geometry.iloc[a+1:]:
            crossing=shapely.intersection(shape,other)
            assert crossing.is_empty or crossing.geom_type=='Point'
            if not crossing.is_empty:
                point=(crossing.x,crossing.y)
                assert point in shape.coords and point in other.coords


@pytest.mark.parametrize('crs',['EPSG:3857','EPSG:2263'])
def test_normalize_line_network_original_vertices_micro_overlap_and_nearby_lines(crs):
    tiny=2**-40
    frame=line_overlap_frame([LineString([(-2**50,0),(0,0),(1,0)]),LineString([(2*tiny,0),(tiny,0)]),
        LineString([(0,tiny),(1,tiny)]),LineString([(1+tiny,0),(2,0)])],crs=crs)
    result,members=normalized_lines(frame).values()
    assert [list(g.coords) for g in result.geometry]==[[(-2**50,0),(0,0)],[(0,0),(tiny,0)],[(tiny,0),(2*tiny,0)],[(2*tiny,0),(1,0)],[(0,tiny),(1,tiny)],[(1+tiny,0),(2,0)]]
    assert result.occurrence_count.tolist()==[1,1,2,1,1,1]
    assert members.edge_id.tolist()==[1,2,3,4,3,5,6] and members.is_reversed.tolist()==[False]*4+[True,False,False]
    assert result.crs==members.crs==frame.crs


def test_normalize_line_network_zero_original_segments_keep_ordinal_gaps():
    frame=line_overlap_frame([LineString([(0,0),(0,0),(1,0),(1,0),(2,0),(2,0)])])
    before=frame.geometry.to_wkb().tolist();result,members=normalized_lines(frame).values()
    assert members.source_segment_no.tolist()==[2,4] and members.piece_no.tolist()==[1,1]
    assert [list(g.coords) for g in result.geometry]==[[(0,0),(1,0)],[(1,0),(2,0)]]
    assert frame.geometry.to_wkb().tolist()==before


def test_normalize_line_network_closed_paths_are_edges_without_implicit_chain_merging():
    frame=line_overlap_frame([LineString([(0,0),(2,0),(2,2),(0,0)]),LineString([(0,0),(2,2),(2,0),(0,0)])])
    result,members=normalized_lines(frame).values()
    assert len(result)==3 and result.occurrence_count.tolist()==[2]*3
    assert all(len(g.coords)==2 for g in result.geometry)
    assert members.edge_id.tolist()==[1,2,3,3,2,1]
    assert members.is_reversed.tolist()==[False,False,True,False,True,True]


def test_normalize_line_network_candidate_order_does_not_change_identifiers(monkeypatch):
    import operators.geometric_operators as module
    frame=line_overlap_frame([LineString([(4,0),(0,0)]),LineString([(1,-1),(1,1)]),LineString([(3,0),(1,0)])])
    expected=normalized_lines(frame);original=module.shapely.STRtree
    class ReversedCandidates:
        def __init__(self,shapes):self.tree=original(shapes)
        def query(self,*args,**kwargs):return self.tree.query(*args,**kwargs)[::-1]
    monkeypatch.setattr(module.shapely,'STRtree',ReversedCandidates)
    for name,value in normalized_lines(frame).items():assert_geodataframe_equal(value,expected[name])


@pytest.mark.parametrize('ids',[pd.Series(['甲'],dtype='string'),pd.Series([2**60+1],dtype='Int64'),pd.Series([True],dtype='boolean'),pd.Series(pd.Categorical(['a']))])
@pytest.mark.parametrize('empty',[False,True])
def test_normalize_line_network_empty_and_nonempty_full_source_schema(ids,empty):
    frame=line_overlap_frame([LineString([(0,0),(1,0)])],ids);frame['tag']=pd.Series([None],dtype='string')
    if empty:frame=frame.iloc[:0]
    result,members=normalized_lines(frame).values()
    assert len(result)==len(members)==len(frame) and members.source_id.dtype==frame.route_key.dtype
    for name in frame.columns:assert members[name].dtype==frame[name].dtype
    assert result.geometry.name==members.geometry.name=='boundary' and result.crs==members.crs==frame.crs
    assert result.edge_id.dtype==result.occurrence_count.dtype==members.source_part_no.dtype==members.source_segment_no.dtype==members.piece_no.dtype=='int64'
    assert members.is_reversed.dtype=='bool'


@pytest.mark.parametrize('name',['edge_id','occurrence_count','source_id','source_part_no','source_segment_no','piece_no','is_reversed'])
def test_normalize_line_network_output_fields_never_overwrite_source(name):
    frame=line_overlap_frame([LineString([(0,0),(1,0)])]);frame[name]=1
    before=frame.copy(deep=True)
    with pytest.raises(ValueError,match='output'):normalized_lines(frame)
    assert_geodataframe_equal(frame,before)


@pytest.mark.parametrize('ids',[[None],[' '],[1,1],[[1]],[{'a':1}]])
def test_normalize_line_network_requires_unique_explicit_source_id(ids):
    frame=line_overlap_frame([LineString([(i,0),(i+1,0)]) for i in range(len(ids))],ids)
    with pytest.raises(ValueError):normalized_lines(frame)


@pytest.mark.parametrize('field',[None,'missing','boundary',True,[],{}])
def test_normalize_line_network_rejects_implicit_or_geometry_source_id(field):
    with pytest.raises(ValueError):normalized_lines(line_overlap_frame([LineString([(0,0),(1,0)])]),field)


@pytest.mark.parametrize('shape',[None,LineString(),Point(0,0),Polygon([(0,0),(1,0),(0,1)]),LineString([(0,0),(0,0)]),
    LineString([(0,0,1),(1,0,1)]),shapely.from_wkt('LINESTRING M (0 0 1,1 0 2)'),LineString([(0,0),(float('inf'),1)]),LineString([(0,0),(float('nan'),1)])])
def test_normalize_line_network_rejects_invalid_nonfinite_nonxy_geometry(shape):
    frame=line_overlap_frame([shape]);before=frame.geometry.to_wkb().tolist()
    with pytest.raises(ValueError):normalized_lines(frame)
    assert frame.geometry.to_wkb().tolist()==before


@pytest.mark.parametrize('crs',[None,'EPSG:4326','EPSG:4978'])
def test_normalize_line_network_requires_known_projected_horizontal_units(crs):
    with pytest.raises(ValueError,match='projected'):normalized_lines(line_overlap_frame([LineString([(0,0),(1,0)])],crs=crs))


def test_normalize_line_network_requires_real_frame_active_geometry_and_unique_columns():
    with pytest.raises(ValueError):normalized_lines(pd.DataFrame({'route_key':[1]}))
    frame=line_overlap_frame([LineString([(0,0),(1,0)])]);frame=pd.concat([frame,frame[['route_key']]],axis=1)
    with pytest.raises(ValueError):normalized_lines(frame)
    frame=gpd.GeoDataFrame({'route_key':[1],'shape':[LineString([(0,0),(1,0)])]})
    with pytest.raises(ValueError):normalized_lines(frame)


def test_normalize_line_network_metadata_two_ports_workflow_only(monkeypatch):
    import api_server
    with api_server.app.test_client() as client:
        operators=client.get('/api/operators').get_json()['operators']
        selected=[o for o in operators if o['id']=='normalize_line_network'];assert len(selected)==1
        op=selected[0]
        assert op['display_name']=='重合规整' and op['category_path']==['矢量处理','线网处理']
        assert op['execution_modes']==['workflow'] and op['effects']==['read']
        assert [(p['name'],p['required']) for p in op['parameters']]==[('input_gdf',True),('id_field',True)]
        assert [(p['name'],p['type'],p['is_default']) for p in op['output_ports']]==[('result','geodataframe',True),('members','geodataframe',False)]
        assert client.post('/api/operators/normalize_line_network/invoke',json={'params':{}}).status_code==403


@pytest.mark.parametrize('empty',[False,True])
def test_normalize_line_network_native_dual_save_and_independent_ogr_reload(tmp_path,empty):
    from osgeo import ogr
    ogr.UseExceptions()
    frame=line_overlap_frame([LineString([(0,0),(2,0),(0,0)]),LineString([(1,0),(3,0)])],pd.Series([2**60+1,2**60+2],dtype='Int64'))
    frame['note']=pd.Series([None,'乙'],dtype='string');frame['grade']=pd.Series([7,None],dtype='Int64');frame['enabled']=pd.Series([True,False],dtype='boolean')
    if empty:frame=frame.iloc[:0]
    source=tmp_path/'source.gpkg';frame.to_file(source,driver='GPKG',layer='source');original=source.read_bytes()
    connection={'engine_type':'nfs','mount_path':str(tmp_path)}
    # load must preserve the declared nullable physical schema end to end.
    loaded=get_operator('load')(connection_info=connection,path='source.gpkg')
    assert str(loaded.grade.dtype)=='Int64' and str(loaded.enabled.dtype)=='boolean'
    tasks=[{'id':'load','operator':'load','params':{'connection_info':connection,'path':'source.gpkg'},'depends_on':[]},
        {'id':'normal','operator':'normalize_line_network','params':{'input_gdf':{'$ref':'load'},'id_field':'route_key'},'depends_on':['load']}]
    for port in ('result','members'):
        ref={'$ref':'normal'} if port=='result' else {'$ref':'normal','port':port}
        tasks.append({'id':'save_'+port,'operator':'save','params':{'input_df':ref,'connection_info':connection,'path':port+'.gpkg','mode':'fail'},'depends_on':['normal']})
    execution=execute_workflow({'tasks':tasks});assert execution['status']=='success',execution.get('error')
    assert set(json.loads(execution['all_results']['normal']))=={'result','members'}
    coordinates={'result':[[(0,0),(1,0)],[(1,0),(2,0)],[(2,0),(3,0)]],
        'members':[[(0,0),(1,0)],[(1,0),(2,0)],[(2,0),(1,0)],[(1,0),(0,0)],[(1,0),(2,0)],[(2,0),(3,0)]]}
    for port in ('result','members'):
        ds=ogr.Open(str(tmp_path/(port+'.gpkg')),0);layer=ds.GetLayer(0);schema=layer.GetLayerDefn();rows=list(layer)
        assert layer.GetSpatialRef().GetAuthorityCode(None)=='3857'
        assert [list(f.GetGeometryRef().GetPoints()) for f in rows]==([] if empty else coordinates[port])
        fields=[f.GetName() for f in layer.schema]
        assert fields==(['edge_id','occurrence_count'] if port=='result' else ['source_name','route_key','note','grade','enabled','edge_id','source_id','source_part_no','source_segment_no','piece_no','is_reversed'])
        for name in (['edge_id','occurrence_count'] if port=='result' else ['route_key','grade','edge_id','source_id','source_part_no','source_segment_no','piece_no']):
            assert schema.GetFieldDefn(schema.GetFieldIndex(name)).GetType()==ogr.OFTInteger64
        if port=='members':
            assert schema.GetFieldDefn(schema.GetFieldIndex('is_reversed')).GetSubType()==ogr.OFSTBoolean
            if not empty:
                assert [f.GetField('route_key') for f in rows]==[2**60+1]*4+[2**60+2]*2
                assert [f.GetField('source_id') for f in rows]==[2**60+1]*4+[2**60+2]*2
                assert [f.GetField('note') for f in rows]==[None]*4+['乙']*2
                assert [f.GetField('grade') for f in rows]==[7]*4+[None]*2
                assert [bool(f.GetField('is_reversed')) for f in rows]==[False,False,True,True,False,False]
        elif not empty:assert [f.GetField('occurrence_count') for f in rows]==[2,3,1]
        ds=None
    assert source.read_bytes()==original


def test_normalize_line_network_async_default_and_explicit_port_and_failure_no_publish(tmp_path,monkeypatch):
    import api_server
    frame=line_overlap_frame([LineString([(0,0),(2,0)]),LineString([(2,0),(0,0)])],['甲','乙'])
    frame.to_file(tmp_path/'source.gpkg',driver='GPKG',layer='source');original=(tmp_path/'source.gpkg').read_bytes()
    connection={'engine_type':'nfs','mount_path':str(tmp_path)}
    tasks=[{'id':'load','operator':'load','params':{'connection_info':connection,'path':'source.gpkg'},'depends_on':[]},
        {'id':'normal','operator':'normalize_line_network','params':{'input_gdf':{'$ref':'load'},'id_field':'route_key'},'depends_on':['load']}]
    for port in ('result','members'):
        tasks.append({'id':port,'operator':'save','params':{'input_df':{'$ref':'normal',**({} if port=='result' else {'port':port})},'connection_info':connection,'path':port+'.gpkg','mode':'fail'},'depends_on':['normal']})
    from addp_common.workflow_runtime import ExecutionRegistry
    monkeypatch.setattr(api_server,'executions',ExecutionRegistry())
    with api_server.app.test_client() as client:
        response=client.post('/api/workflow',json={'workflow_def':{'tasks':tasks},'runtime':{'tenant_id':7,'execution_authorization':{'id':1,'effects':['read','write']}}});assert response.status_code==202
        task_id=response.get_json()['execution_id']
        deadline=time.monotonic()+5
        while time.monotonic()<deadline:
            status=client.get('/api/executions/'+task_id).get_json()
            if status['status'] in ('success','failed'):break
            time.sleep(.02)
        assert status['status']=='success',status
    assert len(gpd.read_file(tmp_path/'result.gpkg'))==1 and len(gpd.read_file(tmp_path/'members.gpkg'))==2
    assert (tmp_path/'source.gpkg').read_bytes()==original
    tasks[1]['params']['id_field']='missing'
    for port in ('result','members'):next(t for t in tasks if t['id']==port)['params']['path']='failed-'+port+'.gpkg'
    failed=execute_workflow({'tasks':tasks});assert failed['status']=='failed'
    assert not (tmp_path/'failed-result.gpkg').exists() and not (tmp_path/'failed-members.gpkg').exists()
    assert (tmp_path/'source.gpkg').read_bytes()==original

@pytest.mark.parametrize('reverse',[False,True])
def test_normalize_line_network_nonbinary_crossing_on_exact_overlapping_segments_is_one_node(reverse):
    parts=[[(0,0),(7,3)],[(1.75,.75),(5.25,2.25)],[(0,3),(8,-1)]]
    if reverse:parts=[part[::-1] for part in parts]
    frame=line_overlap_frame([LineString(part) for part in parts])
    result,members=normalized_lines(frame).values()
    assert len(result)==6 and len(members)==8
    assert sorted(result.occurrence_count)==[1,1,1,1,2,2]
    shared=[set(result.geometry.iloc[row.edge_id-1].coords) for row in members[members.source_id<2].itertuples()]
    crosses=set(members[members.source_id==2].geometry.iloc[0].coords) & set(members[members.source_id==2].geometry.iloc[1].coords)
    assert len(crosses)==1
    point=next(iter(crosses))
    assert sum(point in edge for edge in shared)==4


def test_normalize_line_network_vertex_and_overlap_boundaries_propagate_to_every_original_occurrence():
    frame=line_overlap_frame([LineString([(0,0),(10,0)]),LineString([(1,0),(9,0)]),LineString([(2,0),(8,0)]),
        LineString([(3,0),(7,0)]),LineString([(4,-1),(4,1)]),LineString([(5,0),(6,0)])])
    result,members=normalized_lines(frame).values()
    horizontal=[list(g.coords) for g in result.geometry if g.coords[0][1]==g.coords[1][1]==0]
    assert horizontal==[[(float(i),0),(float(i+1),0)] for i in range(10)]
    assert result.occurrence_count.iloc[:10].tolist()==[1,2,3,4,4,5,4,3,2,1]
    assert members.source_id.tolist().count(0)==10

def test_normalize_line_network_nonbinary_overlapping_intersections_keep_one_exact_edge_identity():
    # Different original endpoint spans may share one supporting line. All
    # occurrences must receive the same node, without epsilon or grid snapping.
    for x in range(1,8):
        for y in range(1,8):
            for multiplier in (1,3,4,5):
                frame=line_overlap_frame([LineString([(0,0),(x,y)]),
                    LineString([(x/4,y/4),(3*x/4,3*y/4)]),LineString([(0,y),(multiplier*x,-y)])])
                result,members=normalized_lines(frame).values()
                assert len(result)==6 and len(members)==8,(x,y,multiplier,[list(g.coords) for g in result.geometry])
                assert sorted(result.occurrence_count.tolist())==[1,1,1,1,2,2]

def _normalize_chain_fixture(name, id_kind):
    if name=='overlap':
        shapes=[LineString([(0,0),(4,0)]),LineString([(3,0),(1,0)])]
        edges=[[(0,0),(1,0)],[(1,0),(3,0)],[(3,0),(4,0)]]
        visits=[(0,1,1,1,1,False),(0,1,1,2,2,False),(0,1,1,3,3,False),(1,1,1,1,2,True)]
        walks=[[(1,False),(2,False),(3,False)]];chains=[[(0,0),(1,0),(3,0),(4,0)]];grades=[1,2]
    elif name=='directions':
        shapes=[LineString([(10,0),(12,2),(10,4)]),MultiLineString([[(12,2),(10,0)],[(10,4),(12,2)]])]
        edges=[[(10,0),(12,2)],[(10,4),(12,2)]]
        visits=[(0,1,1,1,1,False),(0,1,2,1,2,True),(1,1,1,1,1,True),(1,2,1,1,2,False)]
        walks=[[(1,False),(2,True)]];chains=[[(10,0),(12,2),(10,4)]];grades=[7,None]
    elif name=='branch':
        shapes=[LineString([(0,10),(4,10)]),LineString([(2,10),(2,12)])]
        edges=[[(0,10),(2,10)],[(2,10),(4,10)],[(2,10),(2,12)]]
        visits=[(0,1,1,1,1,False),(0,1,1,2,2,False),(1,1,1,1,3,False)]
        walks=[[(1,False)],[(2,False)],[(3,False)]];chains=edges;grades=[1,1]
    elif name=='ring':
        shapes=[LineString([(20,0),(22,0),(22,2),(20,0)]),MultiLineString([[(20,0),(22,2),(22,0),(20,0)],[(20,0),(22,0)]])]
        edges=[[(20,0),(22,0)],[(22,0),(22,2)],[(20,0),(22,2)]]
        visits=[(0,1,1,1,1,False),(0,1,2,1,2,False),(0,1,3,1,3,True),
                (1,1,1,1,3,False),(1,1,2,1,2,True),(1,1,3,1,1,True),(1,2,1,1,1,False)]
        walks=[[(1,False),(2,False),(3,True)]];chains=[[(20,0),(22,0),(22,2),(20,0)]];grades=[2,None]
    elif name=='precision':
        gap=2**-40
        shapes=[LineString([(31,0),(31,0),(30,0),(30,0)]),LineString([(31+gap,0),(32,0)]),LineString([(40,0),(41,0),(42,0)])]
        edges=[[(30,0),(31,0)],[(31+gap,0),(32,0)],[(40,0),(41,0)],[(41,0),(42,0)]]
        visits=[(0,1,2,1,1,True),(1,1,1,1,2,False),(2,1,1,1,3,False),(2,1,2,1,4,False)]
        walks=[[(1,False)],[(2,False)],[(3,False),(4,False)]];chains=[edges[0],edges[1],[(40,0),(41,0),(42,0)]];grades=[None,3,4]
    else:
        assert name=='empty';shapes=[];edges=[];visits=[];walks=[];chains=[];grades=[]
    ids=pd.Series([f'原路段-{i}' for i in range(len(shapes))],dtype='string') if id_kind=='string' else pd.Series([2**60+i+1 for i in range(len(shapes))],dtype='Int64')
    frame=line_overlap_frame(shapes,ids)
    frame['grade']=pd.Series(grades,dtype='Int64');frame['note']=pd.Series([None if i%2==0 else f'原属性-{i}' for i in range(len(frame))],dtype='string')
    frame['enabled']=pd.Series([(True,None,False)[i%3] for i in range(len(frame))],dtype='boolean')
    # These original business names must remain scoped to edge_sources.
    frame['chain_id']=pd.Series([9000+i for i in range(len(frame))],dtype='Int64')
    frame['member_no']=pd.Series([100+i for i in range(len(frame))],dtype='Int64')
    return frame,edges,visits,walks,chains


def _composition_scalar(value):
    return None if value is pd.NA or value is None else value.item() if isinstance(value,np.generic) else value


def _composition_expected(frame, edges, visits, walks, chains):
    from collections import Counter
    counts=Counter(edge for row,part,segment,piece,edge,reverse in visits)
    chain_rows=[];edge_rows=[];source_rows=[]
    for chain_id,(walk,points) in enumerate(zip(walks,chains),1):
        chain_rows.append(({'chain_id':chain_id,'member_count':len(walk)},points))
        for member_no,(edge,reverse) in enumerate(walk,1):
            edge_rows.append(({'edge_id':edge,'occurrence_count':counts[edge],'chain_id':chain_id,
                               'member_no':member_no,'source_part_no':1,'is_reversed':reverse},edges[edge-1]))
    for row,part,segment,piece,edge,reverse in visits:
        properties={k:_composition_scalar(frame.iloc[row][k]) for k in frame.columns if k!=frame.geometry.name}
        source_rows.append(({**properties,'edge_id':edge,'source_id':properties['route_key'],'source_part_no':part,
                             'source_segment_no':segment,'piece_no':piece,'is_reversed':reverse},
                            edges[edge-1][::-1] if reverse else edges[edge-1]))
    return {'chains':chain_rows,'chain_edges':edge_rows,'edge_sources':source_rows}


def _verify_composition_trace(tables, fixture_name):
    from collections import Counter
    chains={p['chain_id']:(p,points) for p,points in tables['chains']}
    edges={p['edge_id']:(p,points) for p,points in tables['chain_edges']}
    assert len(edges)==len(tables['chain_edges']) and len(chains)==len(tables['chains'])
    assert sum(p['member_count'] for p,points in tables['chains'])==len(edges)
    counts=Counter(p['edge_id'] for p,points in tables['edge_sources'])
    assert counts=={edge:p['occurrence_count'] for edge,(p,points) in edges.items()}
    assert sum(counts.values())==len(tables['edge_sources'])
    assert len({(p['source_id'],p['source_part_no'],p['source_segment_no'],p['piece_no']) for p,points in tables['edge_sources']})==len(tables['edge_sources'])
    combinations=set()
    for source,points in tables['edge_sources']:
        edge,canonical=edges[source['edge_id']];chain,coordinates=chains[edge['chain_id']]
        index=edge['member_no']-1;along_chain=coordinates[index:index+2]
        assert along_chain==(canonical[::-1] if edge['is_reversed'] else canonical)
        reversed_relative=bool(source['is_reversed']) ^ bool(edge['is_reversed'])
        assert points==(along_chain[::-1] if reversed_relative else along_chain)
        assert source['chain_id']>=9000 and source['member_no']>=100
        assert edge['source_part_no']==1
        combinations.add((source['is_reversed'],edge['is_reversed']))
    if fixture_name=='directions':assert combinations=={(False,False),(False,True),(True,False),(True,True)}


def _composition_tables_from_memory(frame, normalized, chained):
    tables={}
    for label,value in [('chains',chained['result']),('chain_edges',chained['members']),('edge_sources',normalized['members'])]:
        assert value.crs==frame.crs and value.geometry.name==frame.geometry.name
        assert value.index.equals(pd.RangeIndex(len(value)))
        rows=[]
        for _,row in value.iterrows():
            attrs={name:_composition_scalar(row[name]) for name in value.columns if name!=value.geometry.name}
            rows.append((attrs,[tuple(p) for p in row[value.geometry.name].coords]))
        tables[label]=rows
    return tables


@pytest.mark.parametrize('fixture_name',['overlap','directions','branch','ring','precision','empty'])
@pytest.mark.parametrize('id_kind',['string','int64'])
def test_normalize_line_network_chains_composition_preserves_three_relations(fixture_name,id_kind):
    frame,edges,visits,walks,chains=_normalize_chain_fixture(fixture_name,id_kind)
    # Index is deliberately unrelated to the explicit source identifiers.
    frame.index=pd.Index([7]*(len(frame)));before=frame.copy(deep=True)
    normalized=normalized_lines(frame);normal_before={k:v.copy(deep=True) for k,v in normalized.items()}
    chained=get_operator('merge_line_chains')(normalized['result'],'edge_id',[])
    tables=_composition_tables_from_memory(frame,normalized,chained)
    assert tables==_composition_expected(frame,edges,visits,walks,chains)
    assert list(chained['result'].columns)==[frame.geometry.name,'chain_id','member_count']
    for field in frame.columns:assert normalized['members'][field].dtype==frame[field].dtype
    _verify_composition_trace(tables,fixture_name)
    for key in normal_before:assert_geodataframe_equal(normalized[key],normal_before[key])
    assert_geodataframe_equal(frame,before)


def _seed_composition_source(frame,path):
    from osgeo import ogr,osr
    sr=osr.SpatialReference();sr.ImportFromEPSG(3857)
    ds=ogr.GetDriverByName('GPKG').CreateDataSource(str(path))
    layer=ds.CreateLayer('original_routes',sr,ogr.wkbUnknown,options=['GEOMETRY_NAME='+frame.geometry.name])
    for name in frame.columns:
        if name==frame.geometry.name:continue
        kind=(ogr.OFTInteger if pd.api.types.is_bool_dtype(frame[name].dtype) else
              ogr.OFTInteger64 if pd.api.types.is_integer_dtype(frame[name].dtype) else ogr.OFTString)
        definition=ogr.FieldDefn(name,kind)
        if name=='enabled':definition.SetSubType(ogr.OFSTBoolean)
        assert layer.CreateField(definition)==0
    for _,row in frame.iterrows():
        feature=ogr.Feature(layer.GetLayerDefn())
        for name in frame.columns:
            if name==frame.geometry.name:continue
            value=_composition_scalar(row[name])
            if value is not None:feature.SetField(name,value)
        feature.SetGeometry(ogr.CreateGeometryFromWkb(row[frame.geometry.name].wkb))
        assert layer.CreateFeature(feature)==0
    ds=None


def _read_composition_file(path,label,expected,id_kind):
    from osgeo import ogr
    ds=ogr.Open(str(path),0);assert ds and ds.GetLayerCount()==1
    layer=ds.GetLayer(0);assert layer.GetSpatialRef().GetAuthorityCode(None)=='3857'
    source_fields=['source_name','route_key','grade','note','enabled','chain_id','member_no']
    fields={'chains':['chain_id','member_count'],
        'chain_edges':['edge_id','occurrence_count','chain_id','member_no','source_part_no','is_reversed'],
        'edge_sources':source_fields+['edge_id','source_id','source_part_no','source_segment_no','piece_no','is_reversed']}[label]
    assert [field.GetName() for field in layer.schema]==fields
    for field in layer.schema:
        name=field.GetName()
        native=(ogr.OFTString if name in ('source_name','note') or name in ('route_key','source_id') and id_kind=='string' else
                ogr.OFTInteger if name in ('enabled','is_reversed') else ogr.OFTInteger64)
        assert field.GetType()==native
        if name in ('enabled','is_reversed'):assert field.GetSubType()==ogr.OFSTBoolean
    assert layer.GetFeatureCount()==len(expected);rows=[]
    for i,feature in enumerate(layer,1):
        assert feature.GetFID()==i
        shape=feature.GetGeometryRef();assert shape.GetGeometryName()=='LINESTRING' and shape.GetCoordinateDimension()==2 and not shape.IsMeasured() and shape.IsValid()
        attrs={name:feature.GetField(name) for name in fields}
        for name in ('enabled','is_reversed'):
            if name in attrs and attrs[name] is not None:
                assert type(attrs[name]) in (bool,int) and attrs[name] in (0,1);attrs[name]=bool(attrs[name])
        rows.append((attrs,[tuple(p) for p in shape.GetPoints()]))
    assert rows==expected
    for (actual,_),(wanted,_) in zip(rows,expected):
        for name,value in wanted.items():assert type(actual[name]) is type(value)
    ds=None
    return rows


def _composition_workflow(connection):
    tasks=[{'id':'source','operator':'load','params':{'connection_info':connection,'path':'source.gpkg'},'depends_on':[]},
        {'id':'normalize','operator':'normalize_line_network','params':{'input_gdf':{'$ref':'source'},'id_field':'route_key'},'depends_on':['source']},
        {'id':'chains','operator':'merge_line_chains','params':{'input_gdf':{'$ref':'normalize'},'id_field':'edge_id','match_fields':[]},'depends_on':['normalize']}]
    for label,ref in [('chains',{'$ref':'chains'}),('chain_edges',{'$ref':'chains','port':'members'}),('edge_sources',{'$ref':'normalize','port':'members'})]:
        tasks.append({'id':'save_'+label,'operator':'save','params':{'input_df':ref,'connection_info':connection,'path':label+'.gpkg','mode':'fail'},'depends_on':[ref['$ref']]})
    return tasks


def _composition_native_run(root,fixture_name,id_kind):
    frame,edges,visits,walks,chains=_normalize_chain_fixture(fixture_name,id_kind)
    source=root/'source.gpkg';_seed_composition_source(frame,source);original=source.read_bytes()
    connection={'engine_type':'nfs','mount_path':str(root)}
    loaded=get_operator('load')(connection_info=connection,path='source.gpkg')
    assert str(loaded.grade.dtype)=='Int64' and str(loaded.enabled.dtype)=='boolean'
    tasks=_composition_workflow(connection)
    result=execute_workflow({'tasks':tasks});assert result['status']=='success',result.get('error')
    assert set(json.loads(result['all_results']['normalize']))==set(json.loads(result['all_results']['chains']))=={'result','members'}
    expected=_composition_expected(frame,edges,visits,walks,chains)
    tables={label:_read_composition_file(root/(label+'.gpkg'),label,rows,id_kind) for label,rows in expected.items()}
    _verify_composition_trace(tables,fixture_name)
    assert source.read_bytes()==original
    return expected


@pytest.mark.parametrize('fixture_name',['overlap','directions','branch','ring','precision','empty'])
@pytest.mark.parametrize('id_kind',['string','int64'])
def test_normalize_line_network_chains_composition_native_three_saves(tmp_path,fixture_name,id_kind):
    _composition_native_run(tmp_path,fixture_name,id_kind)


@pytest.mark.parametrize('label,problem',[
    ('chains','chain_id'),('chains','member_count'),('chains','direction'),('chains','coordinate'),('chains','missing'),('chains','crs'),('chains','int_type'),
    ('chain_edges','edge_id'),('chain_edges','occurrence_count'),('chain_edges','member_no'),('chain_edges','source_part_no'),('chain_edges','is_reversed'),('chain_edges','bool_type'),
    ('edge_sources','source_id'),('edge_sources','source_part_no'),('edge_sources','source_segment_no'),('edge_sources','piece_no'),('edge_sources','is_reversed'),
    ('edge_sources','chain_id'),('edge_sources','member_no'),('edge_sources','grade'),('edge_sources','enabled'),('edge_sources','direction'),('edge_sources','int_type'),('edge_sources','bool_type')])
def test_normalize_line_network_chains_composition_native_rejects_corruption(tmp_path,label,problem):
    from osgeo import ogr
    expected=_composition_native_run(tmp_path,'directions','int64');path=tmp_path/(label+'.gpkg')
    if problem in ('int_type','bool_type'):
        source=ogr.Open(str(path),0);layer=source.GetLayer(0)
        changed=tmp_path/'changed.gpkg';destination=ogr.GetDriverByName('GPKG').CreateDataSource(str(changed))
        target=destination.CreateLayer('changed',layer.GetSpatialRef(),ogr.wkbLineString)
        selected='chain_id' if label=='chains' else 'is_reversed' if label=='chain_edges' else 'grade' if problem=='int_type' else 'enabled'
        for definition in layer.schema:
            field=ogr.FieldDefn(definition.GetName(),definition.GetType());field.SetSubType(definition.GetSubType())
            if field.GetName()==selected:
                if problem=='int_type':field.SetType(ogr.OFTReal)
                else:field.SetSubType(ogr.OFSTNone)
            assert target.CreateField(field)==0
        for feature in layer:
            clone=ogr.Feature(target.GetLayerDefn());clone.SetFrom(feature);assert target.CreateFeature(clone)==0
        source=layer=target=destination=None;changed.replace(path)
        with pytest.raises(AssertionError):_read_composition_file(path,label,expected[label],'int64')
        return
    ds=ogr.Open(str(path),1);layer=ds.GetLayer(0)
    if problem=='crs':ds.ExecuteSQL('UPDATE gpkg_geometry_columns SET srs_id=4326')
    elif problem=='missing':layer.DeleteFeature(1)
    else:
        feature=layer.GetFeature(3 if problem in ('grade','enabled') else 1)
        if problem in ('direction','coordinate'):
            points=feature.GetGeometryRef().GetPoints();shape=ogr.Geometry(ogr.wkbLineString)
            for x,y in (points[::-1] if problem=='direction' else points):shape.AddPoint_2D(x+.25 if problem=='coordinate' else x,y)
            feature.SetGeometry(shape)
        elif problem in ('is_reversed','enabled'):feature.SetField(problem,0 if feature.GetField(problem) else 1)
        elif problem in ('grade',):feature.SetField(problem,999)
        else:feature.SetField(problem,feature.GetField(problem)+1)
        assert layer.SetFeature(feature)==0
    ds=layer=None
    with pytest.raises(AssertionError):_read_composition_file(path,label,expected[label],'int64')


@pytest.mark.parametrize('field',['edge_id','occurrence_count','source_id','source_part_no','source_segment_no','piece_no','is_reversed'])
def test_normalize_line_network_chains_composition_invalid_source_publishes_none(tmp_path,field):
    frame,*_= _normalize_chain_fixture('directions','int64');frame[field]=pd.Series([1,2],dtype='Int64')
    source=tmp_path/'source.gpkg';_seed_composition_source(frame,source);original=source.read_bytes()
    result=execute_workflow({'tasks':_composition_workflow({'engine_type':'nfs','mount_path':str(tmp_path)})})
    assert result['status']=='failed' and 'network output columns must not overwrite source fields' in result['error']
    assert sorted(p.name for p in tmp_path.iterdir())==['source.gpkg']
    assert source.read_bytes()==original


@pytest.mark.parametrize('port,problem',[(None,None),('result','count'),('result','occurrences'),('result','crs'),
    ('members','source'),('members','null'),('members','segment'),('members','reverse'),('members','direction'),
    ('members','int_type'),('members','bool_type')])
def test_hosted_normalize_case_dual_save_independent_physical_oracle(tmp_path,port,problem):
    import importlib
    from osgeo import ogr
    fixture=importlib.import_module('business.scripts.online-vector-topology-fixture')
    (tmp_path/'outputs').mkdir();(tmp_path/'.addp-online-owner').touch()
    fixture.worker('seed',tmp_path,fixture.NORMALIZE_CASE)
    source=tmp_path/fixture.NORMALIZE_SOURCE;original=source.read_bytes()
    conn={'engine_type':'nfs','mount_path':str(tmp_path)}
    frame=get_operator('load')(connection_info=conn,path=source.name)
    assert str(frame.grade.dtype)=='Int64' and str(frame.enabled.dtype)=='boolean'
    tasks=[{'id':'load','operator':'load','params':{'connection_info':conn,'path':source.name},'depends_on':[]},
        {'id':'normal','operator':'normalize_line_network','params':{'input_gdf':{'$ref':'load'},'id_field':'route_key'},'depends_on':['load']}]
    for kind,name in zip(fixture.NORMALIZE_PORTS,fixture.NORMALIZE_TARGETS):
        ref={'$ref':'normal'} if kind=='result' else {'$ref':'normal','port':'members'}
        tasks.append({'id':'save_'+kind,'operator':'save','params':{'input_df':ref,'connection_info':conn,'path':'outputs/'+name,'mode':'fail'},'depends_on':['normal']})
    execution=execute_workflow({'tasks':tasks});assert execution['status']=='success',execution.get('error')
    verified=fixture.worker('verify',tmp_path,fixture.NORMALIZE_CASE)
    assert [verified['targets'][n]['feature_count'] for n in fixture.NORMALIZE_TARGETS]==[9,17]
    if port is None:
        root=tmp_path.parent/(tmp_path.name+'-archive');root.mkdir()
        fixture.archive_physical_evidence(tmp_path,root,verified,fixture.NORMALIZE_CASE)
        assert (root/'topology-physical'/source.name).read_bytes()==original
        tasks[1]['params']['id_field']='missing_id'
        for task in tasks[2:]:task['params']['path']=task['params']['path'].replace('outputs/','outputs/failed-')
        failed=execute_workflow({'tasks':tasks});assert failed['status']=='failed'
        assert 'id_field must explicitly name' in failed['error']
        assert fixture.worker('verify',tmp_path,fixture.NORMALIZE_CASE)==verified
    else:
        path=tmp_path/'outputs'/fixture.NORMALIZE_TARGETS[fixture.NORMALIZE_PORTS.index(port)]
        ds=ogr.Open(str(path),1);layer=ds.GetLayer(0)
        if problem=='count':layer.DeleteFeature(1)
        elif problem=='crs':ds.ExecuteSQL('UPDATE gpkg_geometry_columns SET srs_id=4326')
        elif problem in ('int_type','bool_type'):
            name='grade' if problem=='int_type' else 'enabled'
            assert layer.AlterFieldDefn(layer.GetLayerDefn().GetFieldIndex(name),ogr.FieldDefn(name,ogr.OFTReal),ogr.ALTER_TYPE_FLAG)==0
        else:
            feature=layer.GetFeature(1 if problem!='null' else 6)
            if problem=='occurrences':feature.SetField('occurrence_count',2)
            elif problem=='source':feature.SetField('source_id','foreign')
            elif problem=='null':feature.SetField('grade',0)
            elif problem=='segment':feature.SetField('source_segment_no',2)
            elif problem=='reverse':feature.SetField('is_reversed',1)
            elif problem=='direction':
                points=list(feature.GetGeometryRef().GetPoints())[::-1]
                feature.SetGeometry(ogr.CreateGeometryFromJson(json.dumps({'type':'LineString','coordinates':points})))
            assert layer.SetFeature(feature)==0
        ds=None
        with pytest.raises(fixture.FixtureError):fixture.verify_normalize(path,port)
    assert source.read_bytes()==original


@pytest.mark.parametrize('port,problem',[(None,None),('chains','count'),('chains','member_count'),('chains','direction'),('chains','crs'),('chains','int_type'),
    ('chain_edges','edge_id'),('chain_edges','occurrence_count'),('chain_edges','member_no'),('chain_edges','source_part_no'),('chain_edges','is_reversed'),('chain_edges','bool_type'),
    ('edge_sources','source_id'),('edge_sources','source_part_no'),('edge_sources','source_segment_no'),('edge_sources','piece_no'),('edge_sources','is_reversed'),
    ('edge_sources','chain_id'),('edge_sources','member_no'),('edge_sources','null'),('edge_sources','int_type'),('edge_sources','bool_type'),('edge_sources','direction')])
def test_hosted_normalize_chains_case_three_save_independent_physical_oracle(tmp_path,port,problem):
    import importlib
    from osgeo import ogr
    fixture=importlib.import_module('business.scripts.online-vector-topology-fixture')
    owner=importlib.import_module('scripts.test.vector-topology-persistence-online')
    case=fixture.COMPOSITION_CASE;profile=fixture.network_profile(case)
    (tmp_path/'outputs').mkdir();(tmp_path/'.addp-online-owner').touch()
    fixture.worker('seed',tmp_path,case);source=tmp_path/profile['source'];original=source.read_bytes()
    connection={'engine_type':'nfs','mount_path':str(tmp_path)}
    def definition(failed=False):
        value=owner.normalize_definition('source','parent',failed,case)
        value['tasks'][0]['params']={'connection_info':connection,'path':source.name}
        for task in value['tasks'][3:]:
            params=task['params'];params.pop('target_parent_locator');name=params.pop('target_name')
            params.update(connection_info=connection,path='outputs/'+name)
        return value
    frame=get_operator('load')(connection_info=connection,path=source.name)
    assert str(frame.route_key.dtype)=='Int64' and str(frame.grade.dtype)=='Int64' and str(frame.enabled.dtype)=='boolean'
    execution=execute_workflow(definition());assert execution['status']=='success',execution.get('error')
    verified=fixture.worker('verify',tmp_path,case)
    assert [verified['targets'][n]['feature_count'] for n in profile['targets']]==[9,15,22]
    if port is None:
        archive=tmp_path.parent/(tmp_path.name+'-archive');archive.mkdir()
        fixture.archive_physical_evidence(tmp_path,archive,verified,case)
        assert (archive/'topology-physical'/source.name).read_bytes()==original
        failed=execute_workflow(definition(True));assert failed['status']=='failed'
        assert 'id_field must explicitly name one existing non-geometry scalar column' in failed['error']
        assert fixture.worker('verify',tmp_path,case)==verified
    else:
        path=tmp_path/'outputs'/profile['targets'][profile['ports'].index(port)]
        ds=ogr.Open(str(path),1);layer=ds.GetLayer(0)
        if problem=='count':assert layer.DeleteFeature(1)==0
        elif problem=='crs':ds.ExecuteSQL('UPDATE gpkg_geometry_columns SET srs_id=4326')
        elif problem in ('int_type','bool_type'):
            field=('grade' if port=='edge_sources' else 'chain_id') if problem=='int_type' else 'is_reversed'
            assert layer.AlterFieldDefn(layer.GetLayerDefn().GetFieldIndex(field),ogr.FieldDefn(field,ogr.OFTReal),ogr.ALTER_TYPE_FLAG)==0
        else:
            feature=layer.GetFeature(7 if problem=='null' else 1)
            if problem=='direction':
                shape=ogr.Geometry(ogr.wkbLineString)
                for x,y in feature.GetGeometryRef().GetPoints()[::-1]:shape.AddPoint_2D(x,y)
                feature.SetGeometry(shape)
            elif problem=='null':feature.SetField('grade',0)
            elif problem=='is_reversed':feature.SetField(problem,not feature.GetField(problem))
            else:feature.SetField(problem,feature.GetField(problem)+1)
            assert layer.SetFeature(feature)==0
        feature=layer=ds=None
        with pytest.raises(fixture.FixtureError):fixture.worker('verify',tmp_path,case)
    assert source.read_bytes()==original



def near_line_frames(crs="EPSG:3857"):
    source = line_overlap_frame([LineString([(-10, 0), (0, 0)])], pd.Series([2**53 + 1], dtype="Int64"), crs)
    source = source.rename_geometry("source_shape")
    reference = line_overlap_frame([
        LineString([(.5, -1), (.5, 1)]),
        LineString([(-.5, -1), (-.5, 1)]),
        MultiLineString([[(.25, -1), (.25, 1)], [(-.25, 1), (-.25, -1)], [(.25, 1), (.25, -1)]]),
    ], pd.Series(["east", "west", "multi"], dtype="string"), crs)
    reference = reference.rename_geometry("reference_shape").rename(columns={"route_key": "ref_key"})
    return source, reference


def near_lines(source, reference, tolerance=1., **kwargs):
    return get_operator("check_line_proximity")(
        source, reference, kwargs.get("id_field", "route_key"), kwargs.get("reference_id_field", "ref_key"), tolerance,
    )


def test_near_line_keeps_all_reference_candidates_ids_attributes_and_exact_nearest_positions():
    source, reference = near_line_frames()
    source.index = pd.Index([42], name="source_row")
    reference.index = [8, 8, 1]
    source["geometry"] = gpd.GeoSeries([Point(999, 999)], index=source.index, crs=source.crs)
    reference["geometry"] = gpd.GeoSeries([Point(999, 999)] * 3, index=reference.index, crs=reference.crs)
    source["grade"] = pd.Series([2**53 + 3], index=source.index, dtype="Int64")
    before_source, before_reference = source.copy(deep=True), reference.copy(deep=True)
    result = near_lines(source, reference)
    assert result.columns.tolist() == ["source_id", "reference_id", "distance", "nearest_x", "nearest_y", "geometry"]
    assert result.source_id.tolist() == [2**53 + 1] * 3
    assert result.reference_id.tolist() == ["east", "west", "multi"]
    assert result['distance'].tolist() == [.5, .5, .25]
    assert result.nearest_x.tolist() == [.5, -.5, -.25] and result.nearest_y.tolist() == [0., 0., 0.]
    assert [tuple(point.coords[0]) for point in result.geometry] == [(0., 0.)] * 3
    assert result.source_id.dtype == source.route_key.dtype and result.reference_id.dtype == reference.ref_key.dtype
    assert all(str(result[column].dtype) == "float64" for column in ["distance", "nearest_x", "nearest_y"])
    assert result.index.equals(pd.RangeIndex(3)) and result.crs == source.crs
    assert_geodataframe_equal(source, before_source)
    assert_geodataframe_equal(reference, before_reference)


@pytest.mark.parametrize("location", ["endpoint", "interior", "part"])
def test_near_line_exact_contact_to_any_reference_suppresses_every_near_candidate(location):
    source, reference = near_line_frames()
    touching = {"endpoint": LineString([(0, 0), (0, 1)]),
                "interior": LineString([(0, -1), (0, 1)]),
                "part": MultiLineString([[(100, 100), (101, 101)], [(0, -1), (0, 1)]])}[location]
    reference.loc[2, "reference_shape"] = touching
    assert near_lines(source, reference).empty
    assert near_lines(source, source.rename(columns={"route_key": "ref_key"})).empty


@pytest.mark.parametrize("offset,reported", [(1., False), (np.nextafter(1., 0.), True),
    (np.nextafter(1., 2.), False), (2**-40, True), (2**-500, True)])
def test_near_line_strict_distance_boundary_and_tiny_positive_distance(offset, reported):
    source, reference = near_line_frames()
    reference = reference.iloc[:1].copy()
    reference.loc[reference.index[0], "reference_shape"] = LineString([(offset, -1), (offset, 1)])
    result = near_lines(source, reference)
    assert len(result) == int(reported)
    if reported:
        assert result['distance'].tolist() == [offset] and result.nearest_x.tolist() == [offset]
        assert result.nearest_y.tolist() == [0.] and tuple(result.geometry.iloc[0].coords[0]) == (0., 0.)


@pytest.mark.parametrize("reverse", [False, True])
@pytest.mark.parametrize("parts,expected", [
    ([LineString([(-.5, -1), (-.5, 2), (.5, 2), (.5, -1)])], (-.5, 0.)),
    ([LineString([(-1, .5), (1, .5)]), LineString([(-1, -.5), (1, -.5)])], (0., -.5)),
])
def test_near_line_equal_distance_uses_xy_not_part_or_direction_order(parts, expected, reverse):
    source, reference = near_line_frames()
    if reverse:
        parts = [LineString(list(part.coords)[::-1]) for part in parts[::-1]]
    reference = reference.iloc[:1].copy()
    reference.loc[reference.index[0], "reference_shape"] = parts[0] if len(parts) == 1 else MultiLineString(parts)
    result = near_lines(source, reference)
    assert len(result) == 1 and result['distance'].tolist() == [.5]
    assert (result.nearest_x.iloc[0], result.nearest_y.iloc[0]) == expected


def test_near_line_uses_exact_network_dangles_not_every_original_part_endpoint():
    source = line_overlap_frame([LineString([(0, 0), (2, 0)]), LineString([(1, 0), (1, 2)])])
    reference = line_overlap_frame([LineString([(.5, .125), (1.5, .125)])]).rename(columns={"route_key": "ref_key"})
    assert near_lines(source, reference, .2).empty
    closed = line_overlap_frame([LineString([(0, 0), (1, 0), (1, 1), (0, 0)])])
    assert near_lines(closed, reference, 10.).empty


def test_near_line_shared_source_dangle_preserves_each_source_and_reference_row_order():
    source = line_overlap_frame([LineString([(-10, 0), (0, 0)]), LineString([(0, 0), (-10, 0)])],
                               pd.Series([9, 7], dtype="Int64"))
    _, reference = near_line_frames()
    result = near_lines(source, reference)
    assert result.source_id.tolist() == [9, 9, 9, 7, 7, 7]
    assert result.reference_id.tolist() == ["east", "west", "multi"] * 2
    assert result['distance'].tolist() == [.5, .5, .25] * 2


@pytest.mark.parametrize("crs", ["EPSG:3857", "EPSG:2263"])
def test_near_line_native_units_are_not_converted_to_metres(crs):
    source, reference = near_line_frames(crs)
    result = near_lines(source, reference)
    assert result['distance'].tolist() == [.5, .5, .25] and result.crs == source.crs


@pytest.mark.parametrize("side", ["source", "reference", "both"])
def test_near_line_empty_batches_keep_both_id_types_and_complete_schema(side):
    source, reference = near_line_frames()
    if side in {"source", "both"}: source = source.iloc[:0].copy()
    if side in {"reference", "both"}: reference = reference.iloc[:0].copy()
    result = near_lines(source, reference)
    assert result.empty and result.columns.tolist() == ["source_id", "reference_id", "distance", "nearest_x", "nearest_y", "geometry"]
    assert result.source_id.dtype == source.route_key.dtype and result.reference_id.dtype == reference.ref_key.dtype
    assert result.crs == source.crs and result.geometry.name == "geometry"
    assert all(str(result[column].dtype) == "float64" for column in ["distance", "nearest_x", "nearest_y"])


@pytest.mark.parametrize("tolerance", [0, -1, np.nan, np.inf, -np.inf, True, np.bool_(False), "1", None, [], 10**400])
def test_near_line_rejects_nonpositive_nonfinite_boolean_or_nonnumeric_tolerance(tolerance):
    from operators.geometric_operators import NearLineInputError
    source, reference = near_line_frames()
    with pytest.raises(NearLineInputError) as error: near_lines(source, reference, tolerance)
    assert error.value.code == "tolerance"


@pytest.mark.parametrize("source_crs,reference_crs", [(None, "EPSG:3857"), ("EPSG:3857", None),
    ("EPSG:4326", "EPSG:4326"), ("OGC:CRS84", "OGC:CRS84"), ("EPSG:4978", "EPSG:4978"),
    ("EPSG:3857", "EPSG:32650"), ("EPSG:3857", "EPSG:2263")])
def test_near_line_rejects_unknown_nonprojected_or_mismatched_crs(source_crs, reference_crs):
    from operators.geometric_operators import NearLineInputError
    source, _ = near_line_frames(source_crs)
    _, reference = near_line_frames(reference_crs)
    with pytest.raises(NearLineInputError): near_lines(source, reference)


@pytest.mark.parametrize("side", ["source", "reference"])
@pytest.mark.parametrize("geometry", [None, LineString(), Point(0, 0), LineString([(0, 0), (0, 0)]),
    LineString([(0, 0, 1), (1, 0, 1)]), shapely.from_wkt("LINESTRING M (0 0 1, 1 0 1)"),
    LineString([(0, 0), (np.inf, 1)])])
def test_near_line_rejects_missing_empty_non_line_nonxy_or_invalid_geometry(side, geometry):
    from operators.geometric_operators import NearLineInputError
    source, reference = near_line_frames()
    frame, field = (source, "source_shape") if side == "source" else (reference, "reference_shape")
    frame.loc[frame.index[0], field] = geometry
    with pytest.raises(NearLineInputError) as error: near_lines(source, reference)
    assert error.value.code == side


@pytest.mark.parametrize("side", ["source", "reference"])
@pytest.mark.parametrize("field", [None, "missing", "geometry_column", "duplicate", "null", "blank", "nonscalar"])
def test_near_line_requires_explicit_nonnull_unique_scalar_ids_on_both_inputs(side, field):
    from operators.geometric_operators import NearLineInputError
    source, reference = near_line_frames()
    frame = source if side == "source" else reference
    key = "route_key" if side == "source" else "ref_key"
    if field == "geometry_column": key = frame.geometry.name
    elif field in {None, "missing"}: key = field
    else:
        frame[key] = (["same"] * len(frame) if field == "duplicate" else [None] * len(frame) if field == "null" else
                      [" "] * len(frame) if field == "blank" else [[1]] * len(frame))
        if field == "duplicate" and len(frame) == 1:
            source = pd.concat([frame, frame], ignore_index=True)
    with pytest.raises(NearLineInputError) as error:
        near_lines(source, reference, **({"id_field": key} if side == "source" else {"reference_id_field": key}))
    assert error.value.code == side


@pytest.mark.parametrize("empty_result", [False, True])
def test_near_line_workflow_point_save_reloaded_by_independent_ogr_and_sources_unchanged(tmp_path, empty_result):
    from osgeo import ogr
    source, reference = near_line_frames()
    source.to_file(tmp_path / "source.gpkg", driver="GPKG")
    reference.to_file(tmp_path / "reference.gpkg", driver="GPKG")
    before = {name: (tmp_path / name).read_bytes() for name in ["source.gpkg", "reference.gpkg"]}
    connection = {"engine_type": "nfs", "mount_path": str(tmp_path)}
    tasks = [
        {"id": "source", "operator": "load", "params": {"connection_info": connection, "path": "source.gpkg"}, "depends_on": []},
        {"id": "reference", "operator": "load", "params": {"connection_info": connection, "path": "reference.gpkg"}, "depends_on": []},
        {"id": "near", "operator": "check_line_proximity", "params": {"input_gdf": {"$ref": "source"}, "reference_gdf": {"$ref": "reference"}, "id_field": "route_key", "reference_id_field": "ref_key", "tolerance": .1 if empty_result else 1.}, "depends_on": ["source", "reference"]},
        {"id": "save", "operator": "save", "params": {"input_df": {"$ref": "near"}, "connection_info": connection, "path": "near.gpkg", "mode": "fail"}, "depends_on": ["near"]},
    ]
    execution = execute_workflow({"tasks": tasks})
    assert execution["status"] == "success", execution
    dataset = ogr.Open(str(tmp_path / "near.gpkg"))
    layer = dataset.GetLayer(0)
    assert layer.GetFeatureCount() == (0 if empty_result else 3)
    assert layer.GetSpatialRef().GetAuthorityCode(None) == "3857"
    definition = layer.GetLayerDefn()
    assert [definition.GetFieldDefn(i).GetName() for i in range(definition.GetFieldCount())] == ["source_id", "reference_id", "distance", "nearest_x", "nearest_y"]
    assert definition.GetFieldDefn(0).GetType() == ogr.OFTInteger64
    assert definition.GetFieldDefn(1).GetType() == ogr.OFTString
    assert all(definition.GetFieldDefn(i).GetType() == ogr.OFTReal for i in [2, 3, 4])
    if not empty_result:
        for feature, ref, distance, nx in zip(layer, ["east", "west", "multi"], [.5, .5, .25], [.5, -.5, -.25]):
            assert feature.GetField("source_id") == 2**53 + 1 and feature.GetField("reference_id") == ref
            assert feature.GetField("distance") == distance and feature.GetField("nearest_x") == nx
            assert feature.GetField("nearest_y") == 0. and feature.GetGeometryRef().GetGeometryName() == "POINT"
            assert feature.GetGeometryRef().GetX() == feature.GetGeometryRef().GetY() == 0.
    dataset = None
    tasks[2]["params"]["tolerance"] = 0
    tasks[3]["params"]["path"] = "rejected.gpkg"
    assert execute_workflow({"tasks": tasks})["status"] == "failed"
    assert not (tmp_path / "rejected.gpkg").exists()
    assert all((tmp_path / name).read_bytes() == value for name, value in before.items())


@pytest.mark.parametrize("language,expected", [("zh-cn", "近接检查的容差必须"), ("en", "Line proximity tolerance must")])
def test_near_line_runtime_discovery_workflow_only_context_and_localized_failure(language, expected, monkeypatch):
    import api_server
    from addp_common.workflow_runtime import ExecutionRegistry
    monkeypatch.setattr(api_server, "executions", ExecutionRegistry())
    source, reference = near_line_frames()
    params = {"input_gdf": json.loads(source.to_json()), "reference_gdf": json.loads(reference.to_json()),
              "id_field": "route_key", "reference_id_field": "ref_key", "tolerance": 0}
    with api_server.app.test_client() as client:
        spec = next(op for op in client.get("/api/operators").get_json()["operators"] if op["id"] == "check_line_proximity")
        assert spec["description"] == "近接检查" and spec["category_path"] == ["矢量处理", "拓扑检查"]
        assert spec["execution_modes"] == ["workflow"] and spec["effects"] == ["read"]
        assert [(port["name"], port["type"], port["is_default"]) for port in spec["output_ports"]] == [("result", "geodataframe", True)]
        assert {param["name"] for param in spec["parameters"]} == set(params)
        assert all(param["required"] for param in spec["parameters"])
        assert client.post("/api/operators/check_line_proximity/invoke", json={"params": params}).status_code == 403
        workflow = {"tasks": [{"id": "near", "operator": "check_line_proximity", "params": params, "depends_on": []}]}
        assert client.post("/api/workflow", json={"workflow_def": workflow}).status_code == 400
        response = client.post("/api/workflow", headers={"Accept-Language": language}, json={"workflow_def": workflow, "runtime": {
            "tenant_id": 7, "execution_authorization": {"id": 1, "effects": ["read"]}}})
        assert response.status_code == 202
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            state = client.get("/api/executions/" + response.get_json()["execution_id"]).get_json()
            if state["status"] in {"success", "failed"}: break
            time.sleep(.01)
        assert state["status"] == "failed" and state["error_code"] == "INVALID_PARAMS"
        assert state["diagnostic"] == {
            "schema_version": "addp.execution-failure/v3",
            "stage": "execution",
            "reason": "operator_positive_number_required",
            "operator": {"node_index": 1, "operator_id": "check_line_proximity", "parameter": "tolerance"},
        }
        assert expected in state["error"]


@pytest.mark.parametrize("problem", ["empty", "nonfinite"])
def test_near_line_nearest_calculation_failure_does_not_return_partial_results(monkeypatch, problem):
    from operators.geometric_operators import NearLineInputError
    source, reference = near_line_frames()
    real = shapely.shortest_line
    calls = []
    def faulty(a, b):
        calls.append(1)
        if len(calls) == 2:
            return LineString() if problem == "empty" else LineString([(0, 0), (np.nan, 0)])
        return real(a, b)
    monkeypatch.setattr(shapely, "shortest_line", faulty)
    with pytest.raises(NearLineInputError) as error: near_lines(source, reference)
    assert error.value.code == "calculation" and len(calls) == 2


@pytest.mark.parametrize('problem',[None,'count','distance','id','null','bool_type','int_type','crs','coordinate','direction','geometry_column'])
def test_hosted_buffer_case_real_save_and_independent_oracle(tmp_path,problem):
    import importlib
    from osgeo import ogr
    fixture=importlib.import_module('business.scripts.online-vector-topology-fixture')
    (tmp_path/'outputs').mkdir();(tmp_path/'.addp-online-owner').touch()
    seeded=fixture.worker('seed',tmp_path,fixture.BUFFER_CASE)
    originals={name:(tmp_path/name).read_bytes() for name in fixture.BUFFER_SOURCES}
    conn={'engine_type':'nfs','mount_path':str(tmp_path)}
    tasks=[{'id':'normal_input','operator':'load','params':{'connection_info':conn,'path':fixture.BUFFER_SOURCES[0]},'depends_on':[]},
        {'id':'normal','operator':'batch_buffer','params':{'input_gdf':{'$ref':'normal_input'},'distances':[2,1,2],'resolution':1},'depends_on':['normal_input']},
        {'id':'save_result','operator':'save','params':{'input_df':{'$ref':'normal'},'connection_info':conn,'path':'outputs/buffers.gpkg','mode':'fail'},'depends_on':['normal']}]
    result=execute_workflow({'tasks':tasks});assert result['status']=='success',result
    evidence=fixture.worker('verify',tmp_path,fixture.BUFFER_CASE)
    assert evidence['targets']['buffers.gpkg']['feature_count']==6
    assert evidence['sources']==seeded['sources']
    path=tmp_path/'outputs/buffers.gpkg'
    if problem is None:
        archive=tmp_path.parent/(tmp_path.name+'-archive');archive.mkdir()
        fixture.archive_physical_evidence(tmp_path,archive,evidence,fixture.BUFFER_CASE)
        assert (archive/'topology-physical'/fixture.BUFFER_SOURCES[1]).read_bytes()==originals[fixture.BUFFER_SOURCES[1]]
        tasks[0]['params']['path']=fixture.BUFFER_SOURCES[1];tasks[2]['params']['path']='outputs/failed-buffers.gpkg'
        failed=execute_workflow({'tasks':tasks});assert failed['status']=='failed'
        assert 'distance 与多距缓冲输出列冲突' in failed['error']
        assert not (tmp_path/'outputs/failed-buffers.gpkg').exists()
        assert fixture.worker('verify',tmp_path,fixture.BUFFER_CASE)==evidence
    else:
        ds=ogr.Open(str(path),1);layer=ds.GetLayer(0)
        if problem=='count':layer.DeleteFeature(1)
        elif problem=='crs':ds.ExecuteSQL('UPDATE gpkg_geometry_columns SET srs_id=4326')
        elif problem in ('bool_type','int_type'):
            name='enabled' if problem=='bool_type' else 'grade'
            assert layer.AlterFieldDefn(layer.GetLayerDefn().GetFieldIndex(name),ogr.FieldDefn(name,ogr.OFTReal),ogr.ALTER_TYPE_FLAG)==0
        elif problem=='geometry_column':ds.ExecuteSQL('UPDATE gpkg_geometry_columns SET column_name=\'foreign\'')
        else:
            feature=layer.GetFeature(2 if problem=='null' else 1)
            if problem=='distance':feature.SetField('distance',1.)
            elif problem=='id':feature.SetField('route_key',1)
            elif problem=='null':feature.SetField('grade',0)
            else:
                points=[list(p) for p in feature.GetGeometryRef().GetGeometryRef(0).GetPoints()]
                if problem=='direction':points.reverse()
                else:points[1][0]=.25
                feature.SetGeometry(ogr.CreateGeometryFromJson(json.dumps({'type':'Polygon','coordinates':[points]})))
            assert layer.SetFeature(feature)==0
        ds=None
        with pytest.raises((fixture.FixtureError,RuntimeError) if problem=='geometry_column' else fixture.FixtureError):fixture.verify_buffer(path,'result')
    assert all((tmp_path/name).read_bytes()==raw for name,raw in originals.items())


@pytest.mark.parametrize('key',['field-conflict','number-array','crs-mismatch','positive-number'])
def test_hosted_input_diagnostic_sources_reach_exact_typed_failure_without_save(tmp_path,key,monkeypatch):
    """Native proof of the approved real-file triggers, not ordinary User T4."""
    import importlib
    import api_server
    from addp_common.workflow_runtime import ExecutionRegistry
    fixture=importlib.import_module('business.scripts.online-vector-topology-fixture')
    acceptance=importlib.import_module('scripts.test.vector-topology-persistence-online')
    (tmp_path/'outputs').mkdir();(tmp_path/'.addp-online-owner').touch()
    original=fixture.worker('seed',tmp_path,fixture.DIAGNOSTIC_CASE)
    workflow=acceptance.diagnostic_definition(key,list(fixture.DIAGNOSTIC_SOURCES),'unused-parent')
    for task in workflow['tasks']:
        if task['operator']=='load':task['params']={'connection_info':{'engine_type':'nfs','mount_path':str(tmp_path)},'path':task['params']['locator']}
        elif task['operator']=='save':
            task['params'].pop('target_parent_locator');task['params'].pop('target_name')
            task['params'].update(connection_info={'engine_type':'nfs','mount_path':str(tmp_path)},path='outputs/forbidden.gpkg')
    monkeypatch.setattr(api_server,'executions',ExecutionRegistry())
    with api_server.app.test_client() as client:
        response=client.post('/api/workflow',json={'workflow_def':workflow,'runtime':{'tenant_id':7,'execution_authorization':{'id':1,'effects':['read','write']}}})
        assert response.status_code==202
        deadline=time.monotonic()+10
        while time.monotonic()<deadline:
            state=client.get('/api/executions/'+response.get_json()['execution_id']).get_json()
            if state['status'] in ('failed','success'):break
            time.sleep(.01)
    assert state['status']=='failed' and state['error_code']=='INVALID_PARAMS',state
    spec=next(spec for spec in acceptance.DIAGNOSTIC_SPECS if spec[0]==key)
    assert state['diagnostic']==acceptance.expected_input_diagnostic(spec)
    assert fixture.worker('verify',tmp_path,fixture.DIAGNOSTIC_CASE)==original
    fixture.archive_physical_evidence(tmp_path,tmp_path.parent/(key+'-evidence'),original,fixture.DIAGNOSTIC_CASE)
    assert not list((tmp_path/'outputs').iterdir())


@pytest.mark.parametrize('problem',[None,'missing','source_id','reference_id','distance','nearest','point','row_order','crs','source_changed','extra_output'])
def test_hosted_line_proximity_case_exact_seven_points_and_independent_oracle(tmp_path,problem):
    import importlib
    from osgeo import ogr,osr
    fixture=importlib.import_module('business.scripts.online-vector-topology-fixture')
    acceptance=importlib.import_module('scripts.test.vector-topology-persistence-online')
    (tmp_path/'outputs').mkdir();(tmp_path/'.addp-online-owner').touch()
    seeded=fixture.worker('seed',tmp_path,fixture.PROXIMITY_CASE)
    before={name:(tmp_path/name).read_bytes() for name in fixture.PROXIMITY_SOURCES}
    workflow=acceptance.proximity_definition(list(fixture.PROXIMITY_SOURCES),'unused')
    for task in workflow['tasks']:
        if task['operator']=='load':task['params']={'connection_info':{'engine_type':'nfs','mount_path':str(tmp_path)},'path':task['params']['locator']}
        elif task['operator']=='save':
            task['params'].pop('target_parent_locator');task['params'].pop('target_name')
            task['params'].update(connection_info={'engine_type':'nfs','mount_path':str(tmp_path)},path='outputs/'+fixture.PROXIMITY_TARGET)
    execution=execute_workflow(workflow);assert execution['status']=='success',execution
    native=fixture.worker('verify',tmp_path,fixture.PROXIMITY_CASE)
    assert native['sources']==seeded['sources'] and native['targets'][fixture.PROXIMITY_TARGET]['feature_count']==7
    assert len({tuple(row['geometry']['coordinates']) for row in native['targets'][fixture.PROXIMITY_TARGET]['features']})==3
    assert all((tmp_path/name).read_bytes()==data for name,data in before.items())
    output=tmp_path/'outputs'/fixture.PROXIMITY_TARGET
    if problem in ('missing','source_id','reference_id','distance','nearest','point','row_order'):
        ds=ogr.Open(str(output),1);layer=ds.GetLayer(0);feature=layer.GetFeature(1)
        if problem=='missing':layer.DeleteFeature(7)
        elif problem=='point':feature.SetGeometry(ogr.CreateGeometryFromWkt('POINT (0.5 0)'));layer.SetFeature(feature)
        elif problem=='row_order':feature.SetField('source_id',2**40+1);layer.SetFeature(feature)
        else:
            field,value={'source_id':('source_id',1),'reference_id':('reference_id','wrong'),'distance':('distance',1.),'nearest':('nearest_x',.25)}[problem]
            feature.SetField(field,value);layer.SetFeature(feature)
        feature=None;layer=None;ds=None
    elif problem=='crs':
        import sqlite3
        with sqlite3.connect(output) as db:
            db.execute('UPDATE gpkg_geometry_columns SET srs_id=3395');db.execute('UPDATE gpkg_contents SET srs_id=3395')
    elif problem=='source_changed':
        ds=ogr.Open(str(tmp_path/fixture.PROXIMITY_SOURCES[0]),1);layer=ds.GetLayer(0);feature=layer.GetFeature(1);feature.SetField('note','changed');layer.SetFeature(feature);feature=None;layer=None;ds=None
    elif problem=='extra_output':(tmp_path/'outputs'/'extra.gpkg').write_bytes(b'not allowed')
    if problem is None:
        fixture.archive_physical_evidence(tmp_path,tmp_path.parent/'proximity-archive',native,fixture.PROXIMITY_CASE)
        assert len(list((tmp_path.parent/'proximity-archive/topology-physical').rglob('*.gpkg')))==3
    else:
        with pytest.raises(fixture.FixtureError):fixture.worker('verify',tmp_path,fixture.PROXIMITY_CASE)


def endpoint_snap_frames(crs="EPSG:3857"):
    source = gpd.GeoDataFrame({
        'route_key': pd.Series([2**53+1, 9], dtype='Int64'),
        'grade': pd.Series([3, None], dtype='Int64'),
        'enabled': pd.Series([True, None], dtype='boolean'),
        'note': pd.Series(['original', None], dtype='string'),
    }, geometry=[LineString([(0, 0), (0, 0), (4, 0), (4, 10)]),
                 LineString([(20, 0), (20, 10)])], crs=crs).rename_geometry('source_shape')
    source.index = [31, 11]
    reference = gpd.GeoDataFrame({'ref_key': pd.Series(['z', 'a', 'far', 'other'], dtype='string')},
        geometry=[LineString([(.5, -1), (.5, 1)]), LineString([(.5, 1), (.5, -1)]),
                  LineString([(.75, -1), (.75, 1)]), LineString([(19, .25), (21, .25)])],
        crs=crs).rename_geometry('reference_shape')
    return source, reference


def endpoint_snap(source, reference, tolerance=1., **kwargs):
    return get_operator('snap_line_endpoints')(input_gdf=source, reference_gdf=reference,
        id_field=kwargs.get('id_field', 'route_key'), reference_id_field=kwargs.get('reference_id_field', 'ref_key'),
        tolerance=tolerance)


def test_endpoint_snap_keeps_source_attributes_index_repeated_vertices_and_all_target_sources():
    source, reference = endpoint_snap_frames()
    before, ref_before = source.copy(deep=True), reference.copy(deep=True)
    outputs = endpoint_snap(source, reference)
    assert set(outputs) == {'result', 'details'}
    result, details = outputs['result'], outputs['details']
    assert result.geometry.iloc[0].coords[:] == [(.5, 0.), (.5, 0.), (4., 0.), (4., 10.)]
    assert result.geometry.iloc[1].coords[:] == [(20., .25), (20., 10.)]
    assert result.index.equals(source.index) and result.columns.equals(source.columns)
    pd.testing.assert_frame_equal(pd.DataFrame(result.drop(columns=result.geometry.name)),
                                  pd.DataFrame(source.drop(columns=source.geometry.name)))
    assert result.geometry.name == 'source_shape' and result.crs == source.crs
    assert details.columns.tolist() == ['source_id', 'reference_id', 'part_no', 'end', 'original_x', 'original_y', 'target_x', 'target_y', 'distance', 'geometry']
    assert details.source_id.tolist() == [2**53+1, 2**53+1, 9]
    assert details.reference_id.tolist() == ['z', 'a', 'other']
    assert details.part_no.tolist() == [1, 1, 1] and details['end'].tolist() == ['start']*3
    assert details['distance'].tolist() == [.5, .5, .25]
    assert details.original_x.tolist() == [0., 0., 20.] and details.target_x.tolist() == [.5, .5, 20.]
    assert details.original_y.tolist() == [0., 0., 0.] and details.target_y.tolist() == [0., 0., .25]
    assert [p.coords[0] for p in details.geometry] == [(0., 0.), (0., 0.), (20., 0.)]
    assert details.source_id.dtype == source.route_key.dtype and details.reference_id.dtype == reference.ref_key.dtype
    assert str(details.part_no.dtype) == 'int64' and str(details['end'].dtype) == 'string'
    assert all(str(details[k].dtype) == 'float64' for k in ['original_x','original_y','target_x','target_y','distance'])
    assert_geodataframe_equal(source, before); assert_geodataframe_equal(reference, ref_before)


@pytest.mark.parametrize('crs', ['EPSG:3857', 'EPSG:2263'])
def test_endpoint_snap_multipart_and_both_ends_use_frozen_source_native_units(crs):
    source = line_overlap_frame([MultiLineString([[(0,0),(2,0)],[(10,0),(12,0)]])], crs=crs)
    reference = line_overlap_frame([LineString([(-1,.25),(13,.25)])], crs=crs).rename(columns={'route_key':'ref_key'})
    result = endpoint_snap(source, reference)
    assert [part.coords[:] for part in result['result'].geometry.iloc[0].geoms] == [[(0.,.25),(2.,.25)],[(10.,.25),(12.,.25)]]
    assert result['details'].part_no.tolist() == [1,1,2,2]
    assert result['details']['end'].tolist() == ['start','end','start','end']
    assert result['details']['distance'].tolist() == [.25]*4 and result['result'].crs == source.crs
    assert result['details'].original_y.tolist()==[0.]*4 and result['details'].target_y.tolist()==[.25]*4


@pytest.mark.parametrize('offset,moves', [(1.,False),(np.nextafter(1.,0.),True),
    (np.nextafter(1.,2.),False),(2**-40,True),(2**-500,True)])
def test_endpoint_snap_strict_original_distance_boundary_and_no_chaining(offset,moves):
    source = line_overlap_frame([LineString([(0,0),(0,10)])])
    reference = line_overlap_frame([LineString([(offset,-1),(offset,1)]),LineString([(offset+1,-1),(offset+1,1)])]).rename(columns={'route_key':'ref_key'})
    out=endpoint_snap(source,reference)
    assert out['result'].geometry.iloc[0].coords[0] == ((offset,0.) if moves else (0.,0.))
    assert len(out['details']) == int(moves)
    if moves: assert out['details']['distance'].tolist() == [offset]


@pytest.mark.parametrize('kind',['rows','multipart','same_part'])
def test_endpoint_snap_rejects_all_equidistant_distinct_target_positions(kind):
    source=line_overlap_frame([LineString([(0,0),(0,10)])])
    parts=[LineString([(-.5,-1),(-.5,1)]),LineString([(.5,-1),(.5,1)])]
    shapes=parts if kind=='rows' else [MultiLineString(parts)] if kind=='multipart' else [LineString([(-.5,-1),(-.5,1),(.5,1),(.5,-1)])]
    reference=line_overlap_frame(shapes).rename(columns={'route_key':'ref_key'})
    before,ref_before=source.copy(deep=True),reference.copy(deep=True)
    with pytest.raises(ValueError,match='等距'):
        endpoint_snap(source,reference,.75)
    assert_geodataframe_equal(source,before);assert_geodataframe_equal(reference,ref_before)


def test_endpoint_snap_ignores_farther_ties_and_does_not_choose_reference_row_order():
    source=line_overlap_frame([LineString([(0,0),(0,10)])])
    reference=line_overlap_frame([LineString([(-.5,-1),(-.5,1)]),LineString([(.5,-1),(.5,1)]),
                                 LineString([(.25,-1),(.25,1)])]).rename(columns={'route_key':'ref_key'})
    for frame in [reference,reference.iloc[::-1]]:
        out=endpoint_snap(source,frame)
        assert out['result'].geometry.iloc[0].coords[0] == (.25,0.)
        assert out['details'].reference_id.tolist()==[2]


def test_endpoint_snap_all_references_covering_same_unique_target_are_retained():
    source=line_overlap_frame([LineString([(0,0),(0,10)])])
    reference=line_overlap_frame([LineString([(.5,-1),(.5,1)]),LineString([(.5,0),(2,0)])]).rename(columns={'route_key':'ref_key'})
    out=endpoint_snap(source,reference)
    assert out['details'].reference_id.tolist()==[0,1]
    assert out['details'].target_x.tolist()==[.5,.5]


@pytest.mark.parametrize('kind',['touch','connected','closed','no_candidate'])
def test_endpoint_snap_leaves_exact_contacts_connected_endpoints_closed_and_out_of_range_unchanged(kind):
    source=line_overlap_frame([LineString([(0,0),(0,10)])])
    reference=line_overlap_frame([LineString([(.5,-1),(.5,1)]),LineString([(-.5,-1),(-.5,1)])]).rename(columns={'route_key':'ref_key'})
    if kind=='touch': reference.loc[reference.index[0],reference.geometry.name]=LineString([(0,-1),(0,1)])
    if kind=='connected':
        source=line_overlap_frame([LineString([(-2,0),(0,0)]),LineString([(0,0),(2,0)])])
        reference=line_overlap_frame([LineString([(-.1,.5),(.1,.5)])]).rename(columns={'route_key':'ref_key'})
    if kind=='closed': source=line_overlap_frame([LineString([(0,0),(1,0),(1,1),(0,0)])])
    if kind=='no_candidate': source=line_overlap_frame([LineString([(20,0),(20,10)])])
    result=endpoint_snap(source,reference,.75)
    assert_geodataframe_equal(result['result'],source)
    assert result['details'].empty


@pytest.mark.parametrize('shape',[
    LineString([(-2,0),(0,0),(-2,0),(-2,2)]),
    LineString([(0,0),(2,0),(0,0),(2,0),(2,2)]),
    LineString([(-2,0),(0,0),(-2,0)]),
    MultiLineString([[(-2,0),(0,0)],[(0,0),(-2,0),(0,0)]]),
])
def test_endpoint_snap_rejects_dangles_requiring_internal_vertex_or_closed_part_moves(shape):
    source=line_overlap_frame([shape]);reference=line_overlap_frame([LineString([(.25,-1),(.25,1)])]).rename(columns={'route_key':'ref_key'})
    assert any(p.coords[0]==(0.,0.) for p in get_operator('check_dangling_points')(input_gdf=source,id_field='route_key').geometry)
    before=source.copy(deep=True)
    with pytest.raises(ValueError,match='端点完整'):
        endpoint_snap(source,reference,.5)
    assert_geodataframe_equal(source,before)


def test_endpoint_snap_shared_dangle_moves_every_source_endpoint_and_keeps_occurrences():
    source=line_overlap_frame([LineString([(-2,0),(0,0)]),LineString([(0,0),(-2,0)])])
    reference=line_overlap_frame([LineString([(.5,-1),(.5,1)])]).rename(columns={'route_key':'ref_key'})
    out=endpoint_snap(source,reference)
    assert [g.coords[:] for g in out['result'].geometry] == [[(-2.,0.),(.5,0.)],[(.5,0.),(-2.,0.)]]
    assert out['details'].source_id.tolist()==[0,1] and out['details']['end'].tolist()==['end','start']


@pytest.mark.parametrize('shape,reference',[
    (LineString([(0,0),(.5,0),(0,10)]),LineString([(.5,-1),(.5,0)])),
    (LineString([(0,0),(.5,0)]),LineString([(.25,-1),(.25,1)])),
])
def test_endpoint_snap_rejects_local_or_complete_collapse_even_if_remaining_geometry_is_valid(shape,reference):
    source=line_overlap_frame([shape]);refs=line_overlap_frame([reference]).rename(columns={'route_key':'ref_key'})
    with pytest.raises(ValueError,match='塌缩|无效'):
        endpoint_snap(source,refs)


@pytest.mark.parametrize('problem',['off_line','empty','nonfinite'])
def test_endpoint_snap_rejects_non_exact_landing_or_unreliable_projection(monkeypatch,problem):
    source=line_overlap_frame([LineString([(0,0),(0,10)])]);reference=line_overlap_frame([LineString([(.5,-1),(.5,1)])]).rename(columns={'route_key':'ref_key'})
    monkeypatch.setattr(shapely,'shortest_line',lambda *a: LineString() if problem=='empty' else
        LineString([(0,0),(np.nan,0)]) if problem=='nonfinite' else LineString([(0,0),(np.nextafter(.5,1.),0)]))
    with pytest.raises(ValueError,match='精确落|可靠计算'):
        endpoint_snap(source,reference)


@pytest.mark.parametrize('side',['source','reference','both'])
def test_endpoint_snap_empty_batches_and_no_moves_keep_complete_schema(side):
    source,reference=endpoint_snap_frames()
    if side in {'source','both'}: source=source.iloc[:0].copy()
    if side in {'reference','both'}: reference=reference.iloc[:0].copy()
    out=endpoint_snap(source,reference)
    assert_geodataframe_equal(out['result'],source)
    assert out['details'].empty and out['details'].source_id.dtype==source.route_key.dtype
    assert out['details'].reference_id.dtype==reference.ref_key.dtype and out['details'].crs==source.crs
    assert str(out['details'].part_no.dtype)=='int64' and str(out['details']['end'].dtype)=='string'


@pytest.mark.parametrize('tolerance',[0,-1,True,np.bool_(False),np.nan,np.inf,None,'1',10**400])
def test_endpoint_snap_rejects_invalid_tolerance(tolerance):
    source,reference=endpoint_snap_frames()
    with pytest.raises(ValueError): endpoint_snap(source,reference,tolerance)


@pytest.mark.parametrize('source_crs,ref_crs',[(None,'EPSG:3857'),('EPSG:3857',None),('EPSG:4326','EPSG:4326'),('EPSG:3857','EPSG:32650')])
def test_endpoint_snap_rejects_unknown_geographic_or_mismatched_crs(source_crs,ref_crs):
    source,_=endpoint_snap_frames(source_crs);_,reference=endpoint_snap_frames(ref_crs)
    with pytest.raises(ValueError): endpoint_snap(source,reference)


@pytest.mark.parametrize('column',['distance','source_id','reference_id','part_no'])
def test_endpoint_snap_result_business_or_geometry_columns_do_not_conflict_with_detail_columns(column):
    source,reference=endpoint_snap_frames()
    source=source.rename_geometry(column)
    result=endpoint_snap(source,reference)['result']
    assert result.geometry.name==column and result.columns.equals(source.columns)


def test_endpoint_snap_discovery_has_one_default_result_and_details_without_new_resource_parameters():
    spec=next(op for op in list_operators() if op['id']=='snap_line_endpoints')
    assert spec['display_name']=='端点吸附' and spec['category_path']==['矢量处理','几何整理']
    assert spec['execution_modes']==['workflow'] and spec['effects']==['read']
    assert [(p['name'],p['type'],p['is_default']) for p in spec['output_ports']]==[('result','geodataframe',True),('details','geodataframe',False)]
    assert {p['name'] for p in spec['parameters']}=={'input_gdf','reference_gdf','id_field','reference_id_field','tolerance'}
    assert all(p['required'] for p in spec['parameters'])


def test_endpoint_snap_preserves_duplicate_input_index_and_business_columns():
    source,reference=endpoint_snap_frames();source.index=[7,7]
    source['source_id']=pd.array(['business-a','business-b'],dtype='string')
    source['distance']=pd.array([1,None],dtype='Int64')
    out=endpoint_snap(source,reference)
    assert out['result'].index.equals(source.index)
    pd.testing.assert_frame_equal(pd.DataFrame(out['result'].drop(columns='source_shape')),pd.DataFrame(source.drop(columns='source_shape')))
    assert out['details'].source_id.tolist()==[2**53+1,2**53+1,9]


@pytest.mark.parametrize('side',['source','reference'])
@pytest.mark.parametrize('problem',['missing_id','geometry_id','duplicate_id','null_geometry','z'])
def test_endpoint_snap_rejects_invalid_source_or_reference_without_mutation(side,problem):
    source,reference=endpoint_snap_frames();frame=source if side=='source' else reference
    key='route_key' if side=='source' else 'ref_key';options={}
    if problem=='missing_id': options['id_field' if side=='source' else 'reference_id_field']='absent'
    if problem=='geometry_id': options['id_field' if side=='source' else 'reference_id_field']=frame.geometry.name
    if problem=='duplicate_id': frame[key]=[1]*len(frame)
    if problem=='null_geometry': frame.loc[frame.index[0],frame.geometry.name]=None
    if problem=='z': frame.loc[frame.index[0],frame.geometry.name]=LineString([(0,0,1),(0,1,1)])
    before,ref_before=source.copy(deep=True),reference.copy(deep=True)
    with pytest.raises(ValueError): endpoint_snap(source,reference,**options)
    assert_geodataframe_equal(source,before);assert_geodataframe_equal(reference,ref_before)


def test_endpoint_snap_real_nonrepresentable_slanted_nearest_point_is_rejected():
    # The perpendicular foot is (1/10, 3/10), which is not an exact binary XY
    # point on y=3*x at the nearest Float64 coordinates returned by GEOS.
    source=line_overlap_frame([LineString([(1,0),(10,-10)])])
    reference=line_overlap_frame([LineString([(0,0),(1,3)])]).rename(columns={'route_key':'ref_key'})
    foot=shapely.shortest_line(shapely.Point(1,0),reference.geometry.iloc[0]).coords[-1]
    assert not shapely.intersects(shapely.Point(foot),reference.geometry.iloc[0])
    from operators.geometric_operators import EndpointSnapError
    with pytest.raises(EndpointSnapError,match='不能精确落在') as error: endpoint_snap(source,reference)
    assert error.value.code=='landing'


@pytest.mark.parametrize('language,expected',[('zh-cn','等距但位置不同'),('en','equally near candidates at different positions')])
def test_endpoint_snap_workflow_localizes_exact_domain_failure_without_fabricating_parameter_diagnostic(language,expected):
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime import WorkflowExecutionError
    source=line_overlap_frame([LineString([(0,0),(0,10)])])
    reference=line_overlap_frame([LineString([(-.5,-1),(-.5,1)]),LineString([(.5,-1),(.5,1)])]).rename(columns={'route_key':'ref_key'})
    task={'id':'snap','operator':'snap_line_endpoints','params':{'input_gdf':source,'reference_gdf':reference,'id_field':'route_key','reference_id_field':'ref_key','tolerance':1.},'depends_on':[]}
    with pytest.raises(WorkflowExecutionError) as error:
        GeoPythonWorkflowRunner(language=language).execute({'tasks':[task]})
    assert error.value.error_code=='EXECUTION_FAILED' and error.value.diagnostic is None
    assert expected in str(error.value)


@pytest.mark.parametrize('problem',[None,'ambiguous','endpoint','landing','collapse'])
def test_endpoint_snap_workflow_saves_both_ports_with_independent_ogr_or_publishes_neither(tmp_path,monkeypatch,problem):
    from osgeo import ogr
    source,reference=endpoint_snap_frames()
    if problem=='ambiguous':
        reference.loc[len(reference),['ref_key','reference_shape']]=['other-side',LineString([(19,-.25),(21,-.25)])]
    if problem=='endpoint': source.loc[source.index[0],source.geometry.name]=LineString([(-2,0),(0,0),(-2,0),(-2,2)])
    if problem=='collapse': source.loc[source.index[0],source.geometry.name]=LineString([(0,0),(.5,0),(4,10)])
    source.to_file(tmp_path/'source.gpkg',driver='GPKG',index=False)
    reference.to_file(tmp_path/'reference.gpkg',driver='GPKG',index=False)
    original={name:(tmp_path/name).read_bytes() for name in ['source.gpkg','reference.gpkg']}
    connection={'engine_type':'nfs','mount_path':str(tmp_path)}
    tasks=[
        {'id':'source','operator':'load','params':{'connection_info':connection,'path':'source.gpkg'},'depends_on':[]},
        {'id':'reference','operator':'load','params':{'connection_info':connection,'path':'reference.gpkg'},'depends_on':[]},
        {'id':'snap','operator':'snap_line_endpoints','params':{'input_gdf':{'$ref':'source'},'reference_gdf':{'$ref':'reference'},'id_field':'route_key','reference_id_field':'ref_key','tolerance':1.},'depends_on':['source','reference']},
        {'id':'save_lines','operator':'save','params':{'input_df':{'$ref':'snap'},'connection_info':connection,'path':'lines.gpkg','mode':'fail'},'depends_on':['snap']},
        {'id':'save_details','operator':'save','params':{'input_df':{'$ref':'snap','port':'details'},'connection_info':connection,'path':'details.gpkg','mode':'fail'},'depends_on':['snap']},
    ]
    if problem=='landing':
        monkeypatch.setattr(shapely,'shortest_line',lambda *a: LineString([(0,0),(np.nextafter(.5,1.),0)]))
    result=execute_workflow({'tasks':tasks})
    assert all((tmp_path/name).read_bytes()==value for name,value in original.items())
    if problem:
        assert result['status']=='failed' and result['error_code']=='EXECUTION_FAILED'
        assert not (tmp_path/'lines.gpkg').exists() and not (tmp_path/'details.gpkg').exists()
        assert 'snap' not in result.get('all_results',{})
        expected={'ambiguous':'等距','endpoint':'端点完整','landing':'不能精确落在','collapse':'塌缩'}[problem]
        assert expected in result['error']
        return
    assert result['status']=='success',result
    lines_ds=ogr.Open(str(tmp_path/'lines.gpkg'));layer=lines_ds.GetLayerByIndex(0)
    assert layer.GetFeatureCount()==2 and layer.GetSpatialRef().GetAuthorityCode(None)=='3857'
    definition=layer.GetLayerDefn()
    assert {definition.GetFieldDefn(i).GetName() for i in range(definition.GetFieldCount())}=={'route_key','grade','enabled','note'}
    assert definition.GetFieldDefn(definition.GetFieldIndex('route_key')).GetType()==ogr.OFTInteger64
    assert definition.GetFieldDefn(definition.GetFieldIndex('grade')).GetType()==ogr.OFTInteger64
    boolean=definition.GetFieldDefn(definition.GetFieldIndex('enabled'));assert boolean.GetType()==ogr.OFTInteger and boolean.GetSubType()==ogr.OFSTBoolean
    rows=list(layer);assert [f.GetGeometryRef().GetPoints() for f in rows]==[
        [(.5,0.),(.5,0.),(4.,0.),(4.,10.)],[(20.,.25),(20.,10.)]]
    assert [f.GetField('route_key') for f in rows]==[2**53+1,9]
    assert [f.GetField('grade') for f in rows]==[3,None] and [f.GetField('enabled') for f in rows]==[True,None]
    assert [f.GetField('note') for f in rows]==['original',None]
    detail_ds=ogr.Open(str(tmp_path/'details.gpkg'));details=detail_ds.GetLayerByIndex(0)
    assert details.GetFeatureCount()==3 and details.GetSpatialRef().GetAuthorityCode(None)=='3857'
    fields=details.GetLayerDefn()
    assert fields.GetFieldDefn(fields.GetFieldIndex('source_id')).GetType()==ogr.OFTInteger64
    assert fields.GetFieldDefn(fields.GetFieldIndex('reference_id')).GetType()==ogr.OFTString
    assert fields.GetFieldDefn(fields.GetFieldIndex('part_no')).GetType()==ogr.OFTInteger64
    assert all(fields.GetFieldDefn(fields.GetFieldIndex(k)).GetType()==ogr.OFTReal for k in ['distance','original_x','original_y','target_x','target_y'])
    records=list(details)
    assert [f.GetField('source_id') for f in records]==[2**53+1,2**53+1,9]
    assert [f.GetField('reference_id') for f in records]==['z','a','other']
    assert [f.GetField('end') for f in records]==['start']*3
    assert [f.GetField('part_no') for f in records]==[1]*3
    assert [f.GetGeometryRef().GetPoints() for f in records]==[[(0.,0.)],[(0.,0.)],[(20.,0.)]]
    assert [f.GetField('original_x') for f in records]==[0.,0.,20.]
    assert [f.GetField('original_y') for f in records]==[0.,0.,0.]
    assert [f.GetField('target_x') for f in records]==[.5,.5,20.]
    assert [f.GetField('target_y') for f in records]==[0.,0.,.25]
    assert [f.GetField('distance') for f in records]==[.5,.5,.25]


@pytest.mark.parametrize('language,expected',[('zh-cn','端点吸附的容差必须'),('en','Endpoint snapping tolerance must')])
def test_endpoint_snap_async_runtime_discovery_direct_denial_and_parameter_diagnostic(language,expected,monkeypatch):
    import api_server
    from addp_common.workflow_runtime import ExecutionRegistry
    monkeypatch.setattr(api_server,'executions',ExecutionRegistry())
    source,reference=endpoint_snap_frames()
    params={'input_gdf':json.loads(source.to_json()),'reference_gdf':json.loads(reference.to_json()),
            'id_field':'route_key','reference_id_field':'ref_key','tolerance':0}
    workflow={'tasks':[{'id':'snap','operator':'snap_line_endpoints','params':params,'depends_on':[]}]}
    with api_server.app.test_client() as client:
        operators=client.get('/api/operators').get_json()['operators']
        assert len([item for item in operators if item['id']=='snap_line_endpoints'])==1
        assert client.post('/api/operators/snap_line_endpoints/invoke',json={'params':params}).status_code==403
        assert client.post('/api/workflow',json={'workflow_def':workflow}).status_code==400
        response=client.post('/api/workflow',headers={'Accept-Language':language},json={'workflow_def':workflow,
            'runtime':{'tenant_id':7,'execution_authorization':{'id':1,'effects':['read']}}})
        assert response.status_code==202
        deadline=time.monotonic()+5
        while time.monotonic()<deadline:
            state=client.get('/api/executions/'+response.get_json()['execution_id']).get_json()
            if state['status'] in {'success','failed'}:break
            time.sleep(.01)
        assert state["status"] == "failed" and state["error_code"] == "INVALID_PARAMS"
        assert state["diagnostic"] == {
            "schema_version": "addp.execution-failure/v3", "stage": "execution", "reason": "operator_positive_number_required",
            "operator": {"node_index": 1, "operator_id": "snap_line_endpoints", "parameter": "tolerance"},
        }
        assert expected in state['error']


def test_endpoint_snap_reference_reverse_and_row_reorder_keep_targets_and_complete_provenance():
    source,reference=endpoint_snap_frames()
    reference=reference.iloc[::-1].copy();reference[reference.geometry.name]=reference.geometry.map(shapely.reverse)
    result=endpoint_snap(source,reference)
    assert result['result'].geometry.iloc[0].coords[0]==(.5,0.)
    assert result['result'].geometry.iloc[1].coords[0]==(20.,.25)
    assert result['details'].reference_id.tolist()==['a','z','other']


def test_endpoint_snap_never_chains_beyond_the_original_position_limit():
    source=line_overlap_frame([LineString([(0,0),(0,10)])])
    reference=line_overlap_frame([LineString([(.5,-1),(.5,1)]),LineString([(1,-1),(1,1)])]).rename(columns={'route_key':'ref_key'})
    out=endpoint_snap(source,reference,.75)
    assert out['result'].geometry.iloc[0].coords[0]==(.5,0.)
    assert out['details'].reference_id.tolist()==[0] and out['details']['distance'].tolist()==[.5]


def test_endpoint_snap_later_ambiguity_does_not_modify_an_earlier_valid_source():
    source,reference=endpoint_snap_frames()
    reference.loc[len(reference),['ref_key','reference_shape']]=['other-side',LineString([(19,-.25),(21,-.25)])]
    before,ref_before=source.copy(deep=True),reference.copy(deep=True)
    with pytest.raises(ValueError,match='等距但位置不同'): endpoint_snap(source,reference)
    assert_geodataframe_equal(source,before);assert_geodataframe_equal(reference,ref_before)


def test_endpoint_snap_unselected_internal_dangle_does_not_require_implicit_normalization():
    source=line_overlap_frame([LineString([(-2,0),(0,0),(-2,0),(-2,2)])])
    reference=line_overlap_frame([LineString([(100,0),(100,1)])]).rename(columns={'route_key':'ref_key'})
    out=endpoint_snap(source,reference)
    assert_geodataframe_equal(out['result'],source)
    assert out['details'].empty


# Line extension: bounded forward first hit, immutable sources and complete provenance.
def line_extension(source, reference, max_distance=2., **kwargs):
    return get_operator('extend_line_endpoints')(
        input_gdf=source, reference_gdf=reference,
        id_field=kwargs.get('id_field', 'route_key'),
        reference_id_field=kwargs.get('reference_id_field', 'ref_key'), max_distance=max_distance)


def line_extension_frames(crs='EPSG:3857'):
    source, _ = endpoint_snap_frames(crs)
    source[source.geometry.name] = [LineString([(0,0),(0,0),(1,0)]), LineString([(20,0),(20,10)])]
    reference = gpd.GeoDataFrame({'ref_key':pd.Series(['start','first','duplicate','far','side','other'],dtype='string')},
        geometry=[LineString([(-1,-1),(-1,1)]), LineString([(3,-1),(3,1)]),
                  LineString([(3,1),(3,-1)]), LineString([(4,-1),(4,1)]),
                  LineString([(.25,.1),(.75,.1)]), LineString([(19,-1),(21,-1)])],crs=crs).rename_geometry('reference_shape')
    return source,reference


def test_line_extension_first_hit_inclusive_limit_keeps_originals_attributes_and_all_sources():
    source,reference=line_extension_frames();before=source.copy(deep=True);ref_before=reference.copy(deep=True)
    result,details=line_extension(source,reference).values()
    assert result.geometry.iloc[0].coords[:] == [(-1.,0.),(0.,0.),(0.,0.),(1.,0.),(3.,0.)]
    assert result.geometry.iloc[1].coords[:] == [(20.,-1.),(20.,0.),(20.,10.)]
    assert result.geometry.name==source.geometry.name and result.index.equals(source.index)
    pd.testing.assert_frame_equal(pd.DataFrame(result.drop(columns=result.geometry.name)),pd.DataFrame(source.drop(columns=source.geometry.name)))
    assert details.source_id.tolist()==[2**53+1]*3+[9]
    assert details.reference_id.tolist()==['start','first','duplicate','other']
    assert details.part_no.tolist()==[1]*4 and details['end'].tolist()==['start','end','end','start']
    assert details['distance'].tolist()==[1.,2.,2.,1.]
    assert [g.coords[:] for g in details.geometry]==[[(0.,0.),(-1.,0.)],[(1.,0.),(3.,0.)],[(1.,0.),(3.,0.)],[(20.,0.),(20.,-1.)]]
    assert details.source_id.dtype==source.route_key.dtype and details.reference_id.dtype==reference.ref_key.dtype
    assert str(details.part_no.dtype)=='int64' and str(details['end'].dtype)=='string'
    assert all(str(details[c].dtype)=='float64' for c in ['original_x','original_y','target_x','target_y','distance'])
    assert details.crs==source.crs and result.crs==source.crs
    assert_geodataframe_equal(source,before);assert_geodataframe_equal(reference,ref_before)


@pytest.mark.parametrize('limit,expected',[(2.,3.),(np.nextafter(2.,0.),1.),(np.nextafter(2.,3.),3.)])
def test_line_extension_maximum_boundary(limit,expected):
    source=line_overlap_frame([LineString([(0,0),(1,0)])])
    reference=line_overlap_frame([LineString([(3,-1),(3,1)])]).rename(columns={'route_key':'ref_key'})
    result=line_extension(source,reference,limit)
    assert result['result'].geometry.iloc[0].coords[-1]==(expected,0.)
    assert len(result['details'])==int(expected==3.)


@pytest.mark.parametrize('reference_coords,expected',[
    ([(3,0),(5,0)],[(0.,0.),(1.,0.),(3.,0.)]),
    ([(5,0),(3,0)],[(0.,0.),(1.,0.),(3.,0.)]),
    ([(1,0),(5,0)],[(0.,0.),(1.,0.)]),
    ([(-4,0),(-3,0)],[(0.,0.),(1.,0.)]),
    ([(.2,.2),(.8,.2)],[(0.,0.),(1.,0.)]),
])
def test_line_extension_collinear_entry_existing_contact_and_no_forward_hit(reference_coords,expected):
    source=line_overlap_frame([LineString([(0,0),(1,0)])])
    reference=line_overlap_frame([LineString(reference_coords)]).rename(columns={'route_key':'ref_key'})
    result=line_extension(source,reference)
    assert result['result'].geometry.iloc[0].coords[:]==expected


@pytest.mark.parametrize('crs',['EPSG:3857','EPSG:2263'])
def test_line_extension_multipart_frozen_input_native_units_and_no_chaining(crs):
    source=line_overlap_frame([MultiLineString([[(0,0),(1,0)],[(10,0),(11,0)]])],crs=crs)
    reference=line_overlap_frame([LineString([(2,0),(3,0)]),LineString([(12,0),(13,0)])],crs=crs).rename(columns={'route_key':'ref_key'})
    result=line_extension(source,reference,1.)
    assert [list(p.coords) for p in result['result'].geometry.iloc[0].geoms]==[[(0.,0.),(1.,0.),(2.,0.)],[(10.,0.),(11.,0.),(12.,0.)]]
    assert result['details'].part_no.tolist()==[1,2] and result['details']['distance'].tolist()==[1.,1.]
    assert [list(g.coords) for g in source.geometry.iloc[0].geoms]==[[(0.,0.),(1.,0.)],[(10.,0.),(11.,0.)]]


def test_line_extension_reversed_duplicate_sources_keep_all_parts_and_reference_owners():
    source=line_overlap_frame([LineString([(0,0),(1,0)]),MultiLineString([[(1,0),(0,0)],[(0,0),(1,0)]])])
    reference=line_overlap_frame([MultiLineString([[(2,-1),(2,1)],[(2,1),(2,-1)]]),LineString([(2,0),(4,0)])]).rename(columns={'route_key':'ref_key'})
    result=line_extension(source,reference,1.)
    assert len(result['details'])==6
    assert result['details']['end'].tolist()==['end','end','start','start','end','end']
    assert result['result'].geometry.iloc[0].coords[:]==[(0.,0.),(1.,0.),(2.,0.)]
    assert result['result'].geometry.iloc[1].geoms[0].coords[:]==[(2.,0.),(1.,0.),(0.,0.)]


def test_line_extension_exact_diagonal_and_tiny_binary_distance():
    source=line_overlap_frame([LineString([(0,0),(1,1)])])
    reference=line_overlap_frame([LineString([(2,0),(2,3)])]).rename(columns={'route_key':'ref_key'})
    assert line_extension(source,reference)['result'].geometry.iloc[0].coords[-1]==(2.,2.)
    tiny=2.**-500
    source=line_overlap_frame([LineString([(-tiny,0),(0,0)])])
    reference=line_overlap_frame([LineString([(tiny,-tiny),(tiny,tiny)])]).rename(columns={'route_key':'ref_key'})
    result=line_extension(source,reference,tiny)
    assert result['details']['distance'].tolist()==[tiny] and result['result'].geometry.iloc[0].coords[-1]==(tiny,0.)


def test_line_extension_unrepresentable_first_hit_fails_instead_of_choosing_later():
    source=line_overlap_frame([LineString([(-1,0),(0,0)])])
    reference=line_overlap_frame([LineString([(0,1),(1,-2)]),LineString([(1,-1),(1,1)])]).rename(columns={'route_key':'ref_key'})
    before=source.copy(deep=True)
    with pytest.raises(ValueError,match='精确'):line_extension(source,reference)
    assert_geodataframe_equal(source,before)


@pytest.mark.parametrize("operator,limit", [
    ("snap_line_endpoints", "tolerance"),
    ("extend_line_endpoints", "max_distance"),
])
@pytest.mark.parametrize("problem", ["source", "reference", "crs", "limit", "native_check", "native_network"])
def test_line_endpoint_diagnostics_block_both_saves(monkeypatch, operator, limit, problem):
    import workflow_engine
    from addp_common.workflow_runtime import WorkflowExecutionError

    source, reference = line_extension_frames()
    params = {"input_gdf": source, "reference_gdf": reference, "id_field": "route_key",
              "reference_id_field": "ref_key", limit: 1.0}
    if problem == "source":
        params["id_field"] = "PRIVATE_SOURCE_FIELD"
    elif problem == "reference":
        params["reference_id_field"] = "PRIVATE_REFERENCE_FIELD"
    elif problem == "crs":
        params["reference_gdf"] = reference.set_crs("EPSG:2263", allow_override=True)
    elif problem == "limit":
        params[limit] = 0

    saves = []
    original = workflow_engine.get_operator

    def resolve(name):
        if name == "save":
            return lambda **kwargs: saves.append(kwargs)
        function = original(name)
        fault = {"native_check": "is_valid", "native_network": "node"}.get(problem)
        if name != operator or fault is None:
            return function

        def fail(*args, **kwargs):
            raise ValueError("requires valid lines with finite coordinates")

        def invoke(**kwargs):
            with monkeypatch.context() as scope:
                scope.setattr(shapely, fault, fail)
                return function(**kwargs)

        return invoke

    monkeypatch.setattr(workflow_engine, "get_operator", resolve)
    tasks = [{"id": "operation", "operator": operator, "params": params, "depends_on": []}]
    for port in ("result", "details"):
        tasks.append({"id": f"save_{port}", "operator": "save", "depends_on": ["operation"],
                      "params": {"input_df": {"$ref": "operation", "port": port}}})
    with pytest.raises(WorkflowExecutionError) as error:
        workflow_engine.GeoPythonWorkflowRunner(language="en").execute({"tasks": tasks})
    assert not saves
    if problem.startswith("native"):
        assert error.value.error_code == "EXECUTION_FAILED"
        assert error.value.diagnostic is None
        return
    reason, parameter = {
        "source": ("operator_input_invalid", "input_gdf"),
        "reference": ("operator_reference_invalid", "reference_gdf"),
        "crs": ("operator_crs_invalid", ""),
        "limit": ("operator_positive_number_required" if limit == "tolerance" else
                  "operator_distance_limit_invalid", limit),
    }[problem]
    context = {"node_index": 1, "operator_id": operator}
    if parameter:
        context["parameter"] = parameter
    assert error.value.error_code == "INVALID_PARAMS"
    assert error.value.diagnostic == {"schema_version": "addp.execution-failure/v3",
                                      "stage": "execution", "reason": reason, "operator": context}
    assert "PRIVATE_" not in str(error.value)


@pytest.mark.parametrize('bad',[0,-1,True,np.bool_(False),float('inf'),float('nan'),'1',None])
def test_line_extension_rejects_invalid_maximum(bad):
    source,reference=line_extension_frames()
    with pytest.raises(ValueError,match='最大延伸距离'):line_extension(source,reference,bad)


@pytest.mark.parametrize('problem',['source_id','reference_id','source_crs','mismatch','empty_geometry','z','point'])
def test_line_extension_rejects_invalid_sources(problem):
    source,reference=line_extension_frames()
    if problem=='source_id':source['route_key']=[1,1]
    elif problem=='reference_id':reference['ref_key']=None
    elif problem=='source_crs':source=source.set_crs(None,allow_override=True)
    elif problem=='mismatch':reference=reference.set_crs('EPSG:2263',allow_override=True)
    elif problem=='empty_geometry':source.loc[source.index[0],source.geometry.name]=LineString()
    elif problem=='z':source.loc[source.index[0],source.geometry.name]=LineString([(0,0,0),(1,0,0)])
    else:source.loc[source.index[0],source.geometry.name]=Point(0,0)
    with pytest.raises(ValueError):line_extension(source,reference)


@pytest.mark.parametrize('which',['source','reference','both'])
def test_line_extension_empty_batches_preserve_schema(which):
    source,reference=line_extension_frames()
    if which in ('source','both'):source=source.iloc[:0]
    if which in ('reference','both'):reference=reference.iloc[:0]
    outputs=line_extension(source,reference)
    assert_geodataframe_equal(outputs['result'],source)
    details=outputs['details'];assert len(details)==0 and details.crs==source.crs
    assert details.source_id.dtype==source.route_key.dtype and details.reference_id.dtype==reference.ref_key.dtype
    assert details.columns.tolist()==['source_id','reference_id','part_no','end','original_x','original_y','target_x','target_y','distance','geometry']


def test_line_extension_closed_part_is_not_extended():
    source=line_overlap_frame([LineString([(0,0),(1,0),(0,1),(0,0)])])
    reference=line_overlap_frame([LineString([(2,-2),(2,2)])]).rename(columns={'route_key':'ref_key'})
    result=line_extension(source,reference)
    assert_geodataframe_equal(result['result'],source);assert result['details'].empty


@pytest.mark.parametrize('language,expected',[('zh-cn','最大延伸距离'),('en','maximum extension distance')])
def test_line_extension_runner_localizes_invalid_maximum(language,expected):
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime import WorkflowExecutionError
    source,reference=line_extension_frames()
    task={'id':'extend','operator':'extend_line_endpoints','params':{'input_gdf':source,'reference_gdf':reference,'id_field':'route_key','reference_id_field':'ref_key','max_distance':0},'depends_on':[]}
    with pytest.raises(WorkflowExecutionError) as error:GeoPythonWorkflowRunner(language=language).execute({'tasks':[task]})
    assert error.value.error_code == "INVALID_PARAMS"
    assert error.value.diagnostic["reason"] == "operator_distance_limit_invalid"
    assert error.value.diagnostic["operator"]["parameter"] == "max_distance"
    assert expected in str(error.value)


@pytest.mark.parametrize('problem',[None,'first_hit','invalid_limit'])
def test_line_extension_workflow_dual_save_independent_ogr_and_failure_no_publish(tmp_path,problem):
    from osgeo import ogr
    source,reference=line_extension_frames()
    if problem=='first_hit':reference.loc[reference.index[0],reference.geometry.name]=LineString([(0,1),(-1,-2)])
    for name,frame in [('source',source),('reference',reference)]:frame.to_file(tmp_path/f'{name}.gpkg',driver='GPKG',index=False)
    originals={name:(tmp_path/f'{name}.gpkg').read_bytes() for name in ['source','reference']}
    conn={'engine_type':'nfs','mount_path':str(tmp_path)}
    tasks=[{'id':name,'operator':'load','params':{'connection_info':conn,'path':f'{name}.gpkg'},'depends_on':[]} for name in ['source','reference']]
    tasks.append({'id':'extend','operator':'extend_line_endpoints','params':{'input_gdf':{'$ref':'source'},'reference_gdf':{'$ref':'reference'},'id_field':'route_key','reference_id_field':'ref_key','max_distance':0 if problem=='invalid_limit' else 2.},'depends_on':['source','reference']})
    for name,ref in [('lines',{'$ref':'extend'}),('details',{'$ref':'extend','port':'details'})]:
        tasks.append({'id':f'save_{name}','operator':'save','params':{'input_df':ref,'connection_info':conn,'path':f'{name}.gpkg','mode':'fail'},'depends_on':['extend']})
    result=execute_workflow({'tasks':tasks})
    assert all((tmp_path/f'{name}.gpkg').read_bytes()==value for name,value in originals.items())
    if problem:
        assert result["status"] == "failed"
        if problem == "invalid_limit":
            assert result["error_code"] == "INVALID_PARAMS"
            assert result["diagnostic"]["reason"] == "operator_distance_limit_invalid"
        else:
            assert result["error_code"] == "EXECUTION_FAILED"
            assert "diagnostic" not in result
        assert not (tmp_path/'lines.gpkg').exists() and not (tmp_path/'details.gpkg').exists()
        return
    assert result['status']=='success',result
    ds=ogr.Open(str(tmp_path/'lines.gpkg'));layer=ds.GetLayer(0)
    assert layer.GetSpatialRef().GetAuthorityCode(None)=='3857' and layer.GetFeatureCount()==2
    features=list(layer);assert features[0].GetField('route_key')==2**53+1
    definition=layer.GetLayerDefn()
    assert definition.GetFieldDefn(definition.GetFieldIndex('route_key')).GetType()==ogr.OFTInteger64
    assert definition.GetFieldDefn(definition.GetFieldIndex('grade')).GetType()==ogr.OFTInteger64
    assert definition.GetFieldDefn(definition.GetFieldIndex('enabled')).GetSubType()==ogr.OFSTBoolean
    assert features[0].GetField('grade')==3 and features[0].GetField('enabled')==1
    assert features[0].GetField('note')=='original' and features[1].GetField('note') is None
    assert features[0].GetGeometryRef().GetPoints()==[(-1.,0.),(0.,0.),(0.,0.),(1.,0.),(3.,0.)]
    assert features[1].GetField('grade') is None and features[1].GetField('enabled') is None
    ds2=ogr.Open(str(tmp_path/'details.gpkg'));detail_layer=ds2.GetLayer(0)
    assert detail_layer.GetSpatialRef().GetAuthorityCode(None)=='3857' and detail_layer.GetFeatureCount()==4
    rows=list(detail_layer)
    assert [f.GetField('reference_id') for f in rows]==['start','first','duplicate','other']
    assert [f.GetGeometryRef().GetPoints() for f in rows]==[[(0.,0.),(-1.,0.)],[(1.,0.),(3.,0.)],[(1.,0.),(3.,0.)],[(20.,0.),(20.,-1.)]]


def test_line_extension_metadata_has_single_explicit_owner_and_two_ports():
    spec=next(op for op in list_operators() if op['id']=='extend_line_endpoints')
    assert spec['display_name']=='线端延伸' and spec['category_path']==['矢量处理','几何整理']
    assert spec['execution_modes']==['workflow'] and spec['effects']==['read']
    assert [(p['name'],p['is_default']) for p in spec['output_ports']]==[('result',True),('details',False)]
    assert {p['name'] for p in spec['parameters']}=={'input_gdf','reference_gdf','id_field','reference_id_field','max_distance'}


def test_line_extension_reference_order_direction_and_duplicate_index_do_not_choose_main_source():
    source,reference=line_extension_frames();source.index=[5,5]
    reference=reference.iloc[::-1].copy();reference[reference.geometry.name]=reference.geometry.map(shapely.reverse)
    result=line_extension(source,reference)
    assert result['result'].index.tolist()==[5,5]
    assert result['details'].reference_id.tolist()==['start','duplicate','first','other']
    assert result['result'].geometry.iloc[0].coords[:]==[(-1.,0.),(0.,0.),(0.,0.),(1.,0.),(3.,0.)]


def test_line_extension_first_point_contact_keeps_all_touching_reference_features():
    source=line_overlap_frame([LineString([(0,0),(1,0)])])
    reference=line_overlap_frame([LineString([(2,0),(3,1)]),LineString([(2,0),(3,-1)]),LineString([(2,-1),(2,1)])]).rename(columns={'route_key':'ref_key'})
    result=line_extension(source,reference,1.)
    assert result['details'].reference_id.tolist()==[0,1,2]
    assert result['result'].geometry.iloc[0].coords[:]==[(0.,0.),(1.,0.),(2.,0.)]


def test_line_extension_internal_foldback_dangle_rejected_without_mutating_prior_plans():
    source=line_overlap_frame([LineString([(10,0),(11,0)]),LineString([(-2,0),(0,0),(-2,0),(-2,2)])])
    reference=line_overlap_frame([LineString([(12,-1),(12,1)]),LineString([(1,-1),(1,1)])]).rename(columns={'route_key':'ref_key'})
    assert (0.,0.) in {tuple(p.coords[0]) for p in get_operator('check_dangling_points')(source,'route_key').geometry}
    before=source.copy(deep=True);ref_before=reference.copy(deep=True)
    with pytest.raises(ValueError,match='端点'):line_extension(source,reference)
    assert_geodataframe_equal(source,before);assert_geodataframe_equal(reference,ref_before)


def test_line_extension_terminal_helper_and_ray_parameter_reject_ambiguous_or_interior_inputs():
    from fractions import Fraction
    from operators.geometric_operators import _open_endpoint_occurrence, _ray_segment_entry, LineExtensionError
    for shape,coordinate in [(LineString([(0,0),(1,0),(0,1),(0,0)]),(0.,0.)),
                              (LineString([(-1,0),(0,0),(1,0)]),(0.,0.))]:
        with pytest.raises(LineExtensionError,match='端点'):_open_endpoint_occurrence(shape,list(shape.coords),coordinate,LineExtensionError)
    with pytest.raises(LineExtensionError,match='精确'):
        _ray_segment_entry((Fraction(0),Fraction(0)),(Fraction(1),Fraction(0)),LineString([(-1,0),(1,0)]))


def test_line_extension_original_source_is_not_an_implicit_target_or_a_chained_reference():
    source=line_overlap_frame([LineString([(0,0),(1,0)]),LineString([(2,-1),(2,1)])])
    reference=line_overlap_frame([LineString([(3,-2),(3,2)])]).rename(columns={'route_key':'ref_key'})
    result=line_extension(source,reference)
    assert result['result'].geometry.iloc[0].coords[-1]==(3.,0.)
    assert result['result'].geometry.iloc[1].coords[:]==[(2.,-1.),(2.,1.)]
    assert len(result['details'])==1


@pytest.mark.parametrize('language,expected',[('zh-cn','最大延伸距离'),('en','maximum extension distance')])
def test_line_extension_async_discovery_and_parameter_diagnostic(language,expected,monkeypatch):
    import api_server
    from addp_common.workflow_runtime import ExecutionRegistry
    monkeypatch.setattr(api_server,'executions',ExecutionRegistry())
    source,reference=line_extension_frames()
    params={'input_gdf':json.loads(source.to_json()),'reference_gdf':json.loads(reference.to_json()),
        'id_field':'route_key','reference_id_field':'ref_key','max_distance':0}
    workflow={'tasks':[{'id':'extend','operator':'extend_line_endpoints','params':params,'depends_on':[]}]}
    with api_server.app.test_client() as client:
        specs=client.get('/api/operators').get_json()['operators']
        assert len([s for s in specs if s['id']=='extend_line_endpoints'])==1
        assert client.post('/api/operators/extend_line_endpoints/invoke',json={'params':params}).status_code==403
        assert client.post('/api/workflow',json={'workflow_def':workflow}).status_code==400
        response=client.post('/api/workflow',headers={'Accept-Language':language},json={'workflow_def':workflow,
            'runtime':{'tenant_id':7,'execution_authorization':{'id':1,'effects':['read']}}})
        assert response.status_code==202
        deadline=time.monotonic()+5
        while time.monotonic()<deadline:
            state=client.get('/api/executions/'+response.get_json()['execution_id']).get_json()
            if state['status'] in {'success','failed'}:break
            time.sleep(.01)
        assert state["status"] == "failed" and state["error_code"] == "INVALID_PARAMS"
        assert state["diagnostic"] == {
            "schema_version": "addp.execution-failure/v3", "stage": "execution", "reason": "operator_distance_limit_invalid",
            "operator": {"node_index": 1, "operator_id": "extend_line_endpoints", "parameter": "max_distance"},
        }
        assert expected in state['error']


def test_line_extension_multiple_outward_directions_fail_without_choosing_a_part(monkeypatch):
    import operators.geometric_operators as module
    source=line_overlap_frame([LineString([(0,0),(1,0)]),LineString([(0,0),(0,1)])])
    reference=line_overlap_frame([LineString([(-1,-2),(-1,2)])]).rename(columns={'route_key':'ref_key'})
    # Defensive occurrence guard: inject a shared point to exercise direction
    # disagreement, without claiming this degree-two node is a genuine dangle.
    diagnostic=gpd.GeoDataFrame({'source_id':source.route_key},geometry=[Point(0,0),Point(0,0)],crs=source.crs)
    monkeypatch.setattr(module, '_dangling_points', lambda *_: diagnostic)
    before=source.copy(deep=True)
    with pytest.raises(module.LineExtensionError,match='朝外方向'):line_extension(source,reference)
    assert_geodataframe_equal(source,before)


def test_line_extension_exact_binary_maximum_uses_length_not_axis_box_only():
    source=line_overlap_frame([LineString([(0,0),(1,1)])])
    reference=line_overlap_frame([LineString([(2,0),(2,3)])]).rename(columns={'route_key':'ref_key'})
    too_short=line_extension(source,reference,1.)
    assert_geodataframe_equal(too_short['result'],source);assert too_short['details'].empty
    source=line_overlap_frame([LineString([(0,0),(3,4)])])
    reference=line_overlap_frame([LineString([(6,0),(6,9)])]).rename(columns={'route_key':'ref_key'})
    assert line_extension(source,reference,5.)['result'].geometry.iloc[0].coords[-1]==(6.,8.)
    assert line_extension(source,reference,np.nextafter(5.,0.))['details'].empty


@pytest.mark.parametrize('problem',[None,'coordinate','direction','source_id','reference_id','distance','part','null','integer_type','boolean_type','boolean_value','crs','missing','source_changed','extra_output'])
def test_hosted_line_extension_case_dual_save_failure_barrier_and_independent_oracle(tmp_path,problem):
    import importlib
    from osgeo import ogr
    fixture=importlib.import_module('business.scripts.online-vector-topology-fixture');acceptance=importlib.import_module('scripts.test.vector-topology-persistence-online')
    (tmp_path/'outputs').mkdir();(tmp_path/'.addp-online-owner').touch()
    seeded=fixture.worker('seed',tmp_path,fixture.EXTENSION_CASE)
    originals={name:(tmp_path/name).read_bytes() for name in fixture.EXTENSION_SOURCES}
    def definition(failed=False):
        workflow=acceptance.extension_definition(list(fixture.EXTENSION_SOURCES),'unused',failed=failed)
        for task in workflow['tasks']:
            if task['operator']=='load':task['params']={'connection_info':{'engine_type':'nfs','mount_path':str(tmp_path)},'path':task['params']['locator']}
            elif task['operator']=='save':
                name=task['params'].pop('target_name');task['params'].pop('target_parent_locator')
                task['params'].update(connection_info={'engine_type':'nfs','mount_path':str(tmp_path)},path='outputs/'+name)
        return workflow
    result=execute_workflow(definition());assert result['status']=='success',result
    native=fixture.worker('verify',tmp_path,fixture.EXTENSION_CASE)
    assert native['sources']==seeded['sources'] and [native['targets'][name]['feature_count'] for name in fixture.EXTENSION_TARGETS]==[7,8]
    assert all((tmp_path/name).read_bytes()==raw for name,raw in originals.items())
    if problem is None:
        failed=execute_workflow(definition(True));assert failed['status']=='failed',failed
        assert failed['error_code']=='EXECUTION_FAILED' and fixture.EXTENSION_FAILURE in failed['error']
        assert not any((tmp_path/'outputs'/('failed-'+name)).exists() for name in fixture.EXTENSION_TARGETS)
        assert fixture.worker('verify',tmp_path,fixture.EXTENSION_CASE)==native
        archive=tmp_path.parent/(tmp_path.name+'-archive');fixture.archive_physical_evidence(tmp_path,archive,native,fixture.EXTENSION_CASE)
        assert len(list((archive/'topology-physical').rglob('*.gpkg')))==5
        return
    path=tmp_path/'outputs'/fixture.EXTENSION_TARGETS[0 if problem in ('null','integer_type','boolean_type','boolean_value','direction') else 1]
    if problem=='extra_output':(tmp_path/'outputs/extra.gpkg').write_bytes(b'rejected')
    else:
        if problem=='source_changed':path=tmp_path/fixture.EXTENSION_SOURCES[0]
        ds=ogr.Open(str(path),1);layer=ds.GetLayer(0)
        if problem=='boolean_value':ds.ExecuteSQL('UPDATE "'+layer.GetName()+'" SET enabled=2 WHERE fid=1')
        elif problem=='crs':ds.ExecuteSQL('UPDATE gpkg_geometry_columns SET srs_id=4326')
        elif problem=='missing':layer.DeleteFeature(1)
        elif problem in ('integer_type','boolean_type'):
            field='grade' if problem=='integer_type' else 'enabled'
            assert layer.AlterFieldDefn(layer.GetLayerDefn().GetFieldIndex(field),ogr.FieldDefn(field,ogr.OFTReal),ogr.ALTER_TYPE_FLAG)==0
        else:
            feature=layer.GetFeature(2 if problem=='null' else 1)
            if problem=='null':feature.SetField('grade',99)
            elif problem=='source_id':feature.SetField('source_id',1)
            elif problem=='reference_id':feature.SetField('reference_id','foreign')
            elif problem=='distance':feature.SetField('distance',0.)
            elif problem=='part':feature.SetField('part_no',99)
            elif problem=='source_changed':feature.SetField('note','changed')
            else:
                points=fixture.proximity_geometry(feature.GetGeometryRef())
                if problem=='direction':points['coordinates'][0].reverse()
                else:points['coordinates'][1][0]+=1
                feature.SetGeometry(ogr.CreateGeometryFromJson(json.dumps(points)))
            layer.SetFeature(feature);feature=None
        layer=None;ds=None
    with pytest.raises(fixture.FixtureError):fixture.worker('verify',tmp_path,fixture.EXTENSION_CASE)


@pytest.mark.parametrize('problem',[None,'coordinate','direction','source_id','reference_id','distance','part','null','integer_type','boolean_type','boolean_value','crs','missing','source_changed','extra_output'])
def test_hosted_endpoint_case_dual_save_four_real_failures_and_independent_oracle(tmp_path,problem):
    import importlib
    from osgeo import ogr
    fixture=importlib.import_module('business.scripts.online-vector-topology-fixture');acceptance=importlib.import_module('scripts.test.vector-topology-persistence-online')
    (tmp_path/'outputs').mkdir();(tmp_path/'.addp-online-owner').touch()
    seeded=fixture.worker('seed',tmp_path,fixture.ENDPOINT_CASE)
    originals={name:(tmp_path/name).read_bytes() for name in fixture.ENDPOINT_SOURCES}
    def definition(failure=None):
        workflow=acceptance.endpoint_definition(list(fixture.ENDPOINT_SOURCES),'unused',failure=failure)
        for task in workflow['tasks']:
            if task['operator']=='load':task['params']={'connection_info':{'engine_type':'nfs','mount_path':str(tmp_path)},'path':task['params']['locator']}
            elif task['operator']=='save':
                name=task['params'].pop('target_name');task['params'].pop('target_parent_locator')
                task['params'].update(connection_info={'engine_type':'nfs','mount_path':str(tmp_path)},path='outputs/'+name)
        return workflow
    result=execute_workflow(definition());assert result['status']=='success',result
    native=fixture.worker('verify',tmp_path,fixture.ENDPOINT_CASE)
    assert native['sources']==seeded['sources'] and [native['targets'][name]['feature_count'] for name in fixture.ENDPOINT_TARGETS]==[12,10]
    assert all((tmp_path/name).read_bytes()==raw for name,raw in originals.items())
    if problem is None:
        for key,reason in fixture.ENDPOINT_FAILURES:
            failed=execute_workflow(definition(key));assert failed['status']=='failed',failed
            assert failed['error_code']=='EXECUTION_FAILED' and reason in failed['error'],failed
            assert not any((tmp_path/'outputs'/('failed-'+key+'-'+name)).exists() for name in fixture.ENDPOINT_TARGETS)
            assert fixture.worker('verify',tmp_path,fixture.ENDPOINT_CASE)==native
        archive=tmp_path.parent/(tmp_path.name+'-archive');fixture.archive_physical_evidence(tmp_path,archive,native,fixture.ENDPOINT_CASE)
        assert len(list((archive/'topology-physical').rglob('*.gpkg')))==9
        return
    path=tmp_path/'outputs'/fixture.ENDPOINT_TARGETS[0 if problem in ('null','integer_type','boolean_type','boolean_value','direction') else 1]
    if problem=='extra_output':(tmp_path/'outputs/extra.gpkg').write_bytes(b'rejected')
    else:
        if problem=='source_changed':path=tmp_path/fixture.ENDPOINT_SOURCES[0]
        ds=ogr.Open(str(path),1);layer=ds.GetLayer(0)
        if problem=='boolean_value':ds.ExecuteSQL('UPDATE "'+layer.GetName()+'" SET enabled=2 WHERE fid=1')
        elif problem=='crs':ds.ExecuteSQL('UPDATE gpkg_geometry_columns SET srs_id=4326')
        elif problem=='missing':layer.DeleteFeature(1)
        elif problem in ('integer_type','boolean_type'):
            field='grade' if problem=='integer_type' else 'enabled'
            assert layer.AlterFieldDefn(layer.GetLayerDefn().GetFieldIndex(field),ogr.FieldDefn(field,ogr.OFTReal),ogr.ALTER_TYPE_FLAG)==0
        else:
            feature=layer.GetFeature(2 if problem=='null' else 1)
            if problem=='null':feature.SetField('grade',99)
            elif problem=='source_id':feature.SetField('source_id',1)
            elif problem=='reference_id':feature.SetField('reference_id','foreign')
            elif problem=='distance':feature.SetField('distance',0.)
            elif problem=='part':feature.SetField('part_no',99)
            elif problem=='source_changed':feature.SetField('note','changed')
            else:
                points=fixture.proximity_geometry(feature.GetGeometryRef())
                if problem=='direction':points['coordinates'][0].reverse()
                else:points['coordinates'][0]+=1
                feature.SetGeometry(ogr.CreateGeometryFromJson(json.dumps(points)))
            layer.SetFeature(feature);feature=None
        layer=None;ds=None
    with pytest.raises(fixture.FixtureError):fixture.worker('verify',tmp_path,fixture.ENDPOINT_CASE)


# Polygon boundaries are original rings, not unioned linework.
def polygon_ring_fixture(ids_kind="integer", crs="EPSG:3857"):
    from shapely.geometry import MultiPolygon
    rings = [
        [(0,0),(8,0),(8,8),(0,8),(0,0)],
        [(1,1),(1,2),(2,2),(2,1),(1,1)],
        [(4,4),(6,4),(6,6),(4,6),(4,4)],
        [(10,0),(10,3),(13,3),(13,0),(10,0)],
        [(11,.5),(12,.5),(12,1.5),(11,1.5),(11,.5)],
        [(8,0),(16,0),(16,8),(8,8),(8,0)],
    ]
    shapes=[MultiPolygon([Polygon(rings[0],rings[1:3]),Polygon(rings[3],[rings[4]])]),Polygon(rings[5])]
    ids=pd.Series([2**60+1,7],dtype="Int64") if ids_kind=="integer" else pd.Series(["地块甲","地块乙"],dtype="string")
    frame=gpd.GeoDataFrame({"parcel_key":ids,"note":pd.Series([None,"原始属性"],dtype="string"),
        "grade":pd.Series([None,2**53+1],dtype="Int64"),"enabled":pd.Series([None,False],dtype="boolean"),
        "weight":pd.Series([None,.125],dtype="Float64"),"kind":pd.Categorical(["甲","乙"]),"geometry":["业务几何名甲","业务几何名乙"],
        "parcel_shape":gpd.GeoSeries(shapes,crs=crs)},geometry="parcel_shape",crs=crs)
    frame.index=pd.Index([9,9],name="source_index")
    return frame,rings


@pytest.fixture
def ring_extractor():
    return get_operator("extract_polygon_rings")


@pytest.mark.parametrize("ids_kind",["integer","string"])
def test_extract_polygon_rings_keeps_each_ring_source_attributes_order_and_direction(ring_extractor,ids_kind):
    frame,expected=polygon_ring_fixture(ids_kind);before=frame.copy(deep=True)
    result=ring_extractor(frame,"parcel_key");rows=[0,0,0,0,0,1]
    assert isinstance(result.index,pd.RangeIndex) and result.index.name is None
    assert result.geometry.name=="parcel_shape" and result.crs==frame.crs
    assert result.columns.tolist()==frame.columns.tolist()+["source_id","part_no","ring_no","ring_id","is_hole"]
    for column in frame.columns:
        if column!=frame.geometry.name:
            pd.testing.assert_series_equal(result[column],frame[column].iloc[rows].reset_index(drop=True))
    assert result.source_id.dtype==frame.parcel_key.dtype
    assert result.source_id.tolist()==frame.parcel_key.iloc[rows].tolist()
    assert result.part_no.tolist()==[1,1,1,2,2,1] and result.ring_no.tolist()==[1,2,3,1,2,1]
    assert result.ring_id.tolist()==[1,2,3,4,5,6] and result.ring_id.is_unique
    assert result.is_hole.tolist()==[False,True,True,False,True,False]
    for name in ("part_no","ring_no","ring_id"):assert str(result[name].dtype)=="int64"
    assert str(result.is_hole.dtype)=="bool"
    assert all(g.geom_type=="LineString" and g.is_valid and g.is_closed and not g.has_z for g in result.geometry)
    assert [list(g.coords) for g in result.geometry]==expected
    assert_geodataframe_equal(frame,before)


@pytest.mark.parametrize("crs",[None,"EPSG:4326","OGC:CRS84","EPSG:3857"])
@pytest.mark.parametrize("empty",[False,True])
def test_extract_polygon_rings_preserves_source_crs_and_complete_empty_schema(ring_extractor,crs,empty):
    frame,expected=polygon_ring_fixture("string",crs)
    if empty:frame=frame.iloc[:0]
    result=ring_extractor(frame,"parcel_key")
    assert result.crs==frame.crs and result.geometry.name==frame.geometry.name and len(result)==(0 if empty else 6)
    for name in frame.columns:assert result[name].dtype==frame[name].dtype
    assert result.source_id.dtype==frame.parcel_key.dtype
    for name in ("part_no","ring_no","ring_id"):assert str(result[name].dtype)=="int64"
    assert str(result.is_hole.dtype)=="bool"


def test_extract_polygon_rings_keeps_shared_and_duplicate_boundaries_without_union(ring_extractor,monkeypatch):
    first=[(0,0),(1,0),(1,1),(0,1),(0,0)];second=[(1,0),(2,0),(2,1),(1,1),(1,0)]
    frame=gpd.GeoDataFrame({"key":["甲","乙","重复甲"]},geometry=[Polygon(first),Polygon(second),Polygon(first)],crs=3857)
    def forbidden(*a,**k):raise AssertionError("boundary extraction must not union or node source rings")
    monkeypatch.setattr(shapely,"union_all",forbidden);monkeypatch.setattr(shapely,"node",forbidden)
    result=ring_extractor(frame,"key")
    assert [list(g.coords) for g in result.geometry]==[first,second,first]
    assert result.source_id.tolist()==["甲","乙","重复甲"] and result.ring_id.tolist()==[1,2,3]


@pytest.mark.parametrize("coords",[
    [(1,0),(1,0),(1,1),(0,1),(0,0),(1,0)],
    [(0,0),(2**-40,0),(2**-40,2**-40),(0,2**-40),(0,0)],
])
def test_extract_polygon_rings_preserves_original_seam_repeats_and_exact_tiny_coordinates(ring_extractor,coords):
    frame=gpd.GeoDataFrame({"key":[1]},geometry=[Polygon(coords)])
    assert list(ring_extractor(frame,"key").geometry.iloc[0].coords)==coords


@pytest.mark.parametrize("wkt",[
    "POLYGON EMPTY", "POINT (0 0)", "LINESTRING (0 0,1 1)",
    "GEOMETRYCOLLECTION (POLYGON ((0 0,1 0,1 1,0 0)))",
    "POLYGON ((0 0,2 2,0 2,2 0,0 0))",
    "POLYGON Z ((0 0 1,1 0 1,1 1 1,0 0 1))",
    "POLYGON M ((0 0 1,1 0 1,1 1 1,0 0 1))",
    "POLYGON ((0 0,1 0,Infinity 1,0 0))",
    "POLYGON ((0 0,1 0,NaN 1,0 0))",
    "MULTIPOLYGON (EMPTY,((0 0,1 0,1 1,0 0)))",
    "POLYGON ((0 0,2 0,2 2,0 2,0 0),EMPTY)",
])
def test_extract_polygon_rings_rejects_invalid_missing_dimension_or_empty_parts(ring_extractor,wkt):
    frame=gpd.GeoDataFrame({"key":[1]},geometry=[shapely.from_wkt(wkt)],crs=3857)
    # GEOS topological equality is undefined for these intentionally malformed
    # rings. Exact WKB and scalar/index equality prove nonmutation without it.
    original=frame.geometry.to_wkb().tolist();attributes=frame.drop(columns=frame.geometry.name).copy(deep=True)
    with pytest.raises(ValueError):ring_extractor(frame,"key")
    assert frame.geometry.to_wkb().tolist()==original
    pd.testing.assert_frame_equal(frame.drop(columns=frame.geometry.name),attributes)


def test_extract_polygon_rings_rejects_missing_geometry_and_inactive_frame(ring_extractor):
    for value in [pd.DataFrame({"key":[1]}),gpd.GeoDataFrame({"key":[1],"shapes":gpd.GeoSeries([box(0,0,1,1)])}),gpd.GeoDataFrame({"key":[1]},geometry=[None])]:
        with pytest.raises(ValueError):ring_extractor(value,"key")


@pytest.mark.parametrize("field",["source_id","part_no","ring_no","ring_id","is_hole"])
@pytest.mark.parametrize("active_geometry",[False,True])
def test_extract_polygon_rings_rejects_source_or_geometry_output_column_collision(ring_extractor,field,active_geometry):
    frame,_=polygon_ring_fixture()
    if active_geometry:frame=frame.rename_geometry(field)
    else:frame[field]="原属性"
    before=frame.copy(deep=True)
    from operators.geometric_operators import PolygonRingInputError
    for value in [frame, frame.iloc[:0]]:
        with pytest.raises(PolygonRingInputError,match="must not overwrite") as caught: ring_extractor(value,"parcel_key")
        assert (caught.value.code,caught.value.field)==("field_conflict",field)
    assert_geodataframe_equal(frame,before)


@pytest.mark.parametrize("ids",[[1,1],[None,2],[" ","valid"],[[1],[2]]])
def test_extract_polygon_rings_rejects_missing_duplicate_or_nonscalar_source_identifiers(ring_extractor,ids):
    frame,_=polygon_ring_fixture();frame["parcel_key"]=ids
    with pytest.raises(ValueError,match="identifiers"):ring_extractor(frame,"parcel_key")


@pytest.mark.parametrize("field",[None,"","unknown","parcel_shape"])
def test_extract_polygon_rings_does_not_guess_source_identity(ring_extractor,field):
    frame,_=polygon_ring_fixture()
    with pytest.raises(ValueError,match="id_field"):ring_extractor(frame,field)


def test_extract_polygon_rings_rejects_duplicate_column_names(ring_extractor):
    frame,_=polygon_ring_fixture();frame.columns=["parcel_key" if c=="note" else c for c in frame.columns]
    with pytest.raises(ValueError,match="id_field"):ring_extractor(frame,"parcel_key")


@pytest.mark.parametrize("empty",[False,True])
def test_extract_polygon_rings_native_dag_full_ogr_reload_and_rejection_no_publish(ring_extractor,tmp_path,empty):
    from osgeo import ogr
    frame,rings=polygon_ring_fixture();frame=frame.drop(columns="kind").reset_index(drop=True)
    if empty:frame=frame.iloc[:0]
    frame.to_file(tmp_path/"source.gpkg",driver="GPKG",index=False)
    before=(tmp_path/"source.gpkg").read_bytes();connection={"engine_type":"nfs","mount_path":str(tmp_path)}
    tasks=[
        {"id":"load","operator":"load","params":{"connection_info":connection,"path":"source.gpkg"},"depends_on":[]},
        {"id":"rings","operator":"extract_polygon_rings","params":{"input_gdf":{"$ref":"load"},"id_field":"parcel_key"},"depends_on":["load"]},
        {"id":"save","operator":"save","params":{"input_df":{"$ref":"rings"},"connection_info":connection,"path":"rings.gpkg","mode":"fail"},"depends_on":["rings"]},
    ]
    result=execute_workflow({"tasks":tasks});assert result["status"]=="success",result
    ds=ogr.Open(str(tmp_path/"rings.gpkg"));assert ds and ds.GetLayerCount()==1;layer=ds.GetLayer(0)
    assert layer.GetFeatureCount()==(0 if empty else 6) and layer.GetSpatialRef().GetAuthorityCode(None)=="3857"
    assert [f.name for f in layer.schema]==["parcel_key","note","grade","enabled","weight","geometry","source_id","part_no","ring_no","ring_id","is_hole"]
    expected_types={"parcel_key":ogr.OFTInteger64,"note":ogr.OFTString,"grade":ogr.OFTInteger64,"enabled":ogr.OFTInteger,"weight":ogr.OFTReal,"geometry":ogr.OFTString,"source_id":ogr.OFTInteger64,"part_no":ogr.OFTInteger64,"ring_no":ogr.OFTInteger64,"ring_id":ogr.OFTInteger64,"is_hole":ogr.OFTInteger}
    for f in layer.schema:
        assert f.GetType()==expected_types[f.name]
        if f.name in ("enabled","is_hole"):assert f.GetSubType()==ogr.OFSTBoolean
    for i,f in enumerate(layer):
        assert f.GetFID()==i+1 and f.GetGeometryRef().GetGeometryName()=="LINESTRING"
        assert f.GetGeometryRef().GetCoordinateDimension()==2 and f.GetGeometryRef().GetPoints()==rings[i]
        assert f.GetField("parcel_key")==f.GetField("source_id")==([2**60+1]*5+[7])[i]
        assert f.GetField("part_no")==[1,1,1,2,2,1][i] and f.GetField("ring_no")==[1,2,3,1,2,1][i]
        assert f.GetField("ring_id")==i+1 and f.GetField("is_hole")==[False,True,True,False,True,False][i]
        assert f.GetField("grade")==([None]*5+[2**53+1])[i] and f.GetField("note")==([None]*5+["原始属性"])[i]
        assert f.GetField("enabled")==([None]*5+[False])[i] and f.GetField("weight")==([None]*5+[.125])[i]
    f=layer=ds=None;assert (tmp_path/"source.gpkg").read_bytes()==before
    tasks[1]["params"]["id_field"]="missing";tasks[2]["params"]["path"]="rejected.gpkg"
    rejected=execute_workflow({"tasks":tasks});assert rejected["status"]=="failed" and not (tmp_path/"rejected.gpkg").exists()
    assert (tmp_path/"source.gpkg").read_bytes()==before


def test_extract_polygon_rings_metadata_async_default_port_and_direct_forbidden(ring_extractor,tmp_path,monkeypatch):
    import api_server
    from addp_common.workflow_runtime import ExecutionRegistry
    frame,_=polygon_ring_fixture("string");frame=frame.drop(columns="kind").reset_index(drop=True)
    frame.to_file(tmp_path/"source.gpkg",driver="GPKG",index=False)
    tasks=[{"id":"load","operator":"load","params":{"connection_info":{"engine_type":"nfs","mount_path":str(tmp_path)},"path":"source.gpkg"},"depends_on":[]},
        {"id":"rings","operator":"extract_polygon_rings","params":{"input_gdf":{"$ref":"load"},"id_field":"parcel_key"},"depends_on":["load"]}]
    monkeypatch.setattr(api_server,"executions",ExecutionRegistry())
    with api_server.app.test_client() as client:
        op=next(o for o in client.get("/api/operators").get_json()["operators"] if o["id"]=="extract_polygon_rings")
        assert op["display_name"]=="面界提取" and op["category_path"]==["矢量处理","线网处理"]
        assert op["execution_modes"]==["workflow"] and op["effects"]==["read"]
        assert [(p["name"],p["required"]) for p in op["parameters"]]==[("input_gdf",True),("id_field",True)]
        assert len(op["output_ports"])==1 and op["output_ports"][0]["name"]=="result" and op["output_ports"][0]["is_default"] is True
        assert client.post("/api/operators/extract_polygon_rings/invoke",json={"params":{}}).status_code==403
        response=client.post("/api/workflow",json={"workflow_def":{"tasks":tasks},"runtime":{"tenant_id":7,"execution_authorization":{"id":1,"effects":["read"]}}})
        assert response.status_code==202;deadline=time.monotonic()+5
        while time.monotonic()<deadline:
            state=client.get("/api/executions/"+response.get_json()["execution_id"]).get_json()
            if state["status"] in ("success","failed"):break
            time.sleep(.01)
        assert state["status"]=="success",state
        features=json.loads(state["result"])["features"]
        assert [f["properties"]["ring_id"] for f in features]==[1,2,3,4,5,6]
        assert [f["properties"]["source_id"] for f in features]==["地块甲"]*5+["地块乙"]
        assert all(f["geometry"]["type"]=="LineString" for f in features)


def test_extract_polygon_rings_downstream_explicit_rename_retains_two_level_sources(ring_extractor):
    frame,_=polygon_ring_fixture();rings=ring_extractor(frame,"parcel_key");before=rings.copy(deep=True)
    normalizer=get_operator("normalize_line_network")
    with pytest.raises(ValueError,match="must not overwrite"):normalizer(rings,"ring_id")
    renamed=get_operator("rename_fields")(rings,{"source_id":"polygon_source_id","part_no":"polygon_part_no"})
    output=normalizer(renamed,"ring_id");members=output["members"]
    assert set(members.source_id)==set(rings.ring_id)
    assert set(members.polygon_source_id)==set(frame.parcel_key)
    assert members.polygon_source_id.dtype==frame.parcel_key.dtype and members.note.dtype==frame.note.dtype
    assert members.groupby("source_id").polygon_source_id.nunique().eq(1).all()
    assert len(output["result"])<len(members)  # shared boundary retains two original occurrences
    assert_geodataframe_equal(rings,before)


@pytest.mark.parametrize("empty",[False,True])
def test_extract_polygon_rings_direct_native_save_preserves_business_geometry_column(ring_extractor,tmp_path,empty):
    from osgeo import ogr
    frame,coordinates=polygon_ring_fixture();frame=frame.drop(columns="kind").reset_index(drop=True)
    if empty:frame=frame.iloc[:0]
    before=frame.copy(deep=True);result=ring_extractor(frame,"parcel_key")
    get_operator("save")(result,connection_info={"engine_type":"nfs","mount_path":str(tmp_path)},path="direct-rings.gpkg",mode="fail")
    ds=ogr.Open(str(tmp_path/"direct-rings.gpkg"));layer=ds.GetLayer(0)
    assert layer.GetFeatureCount()==len(result) and layer.GetSpatialRef().GetAuthorityCode(None)=="3857"
    fields=[name for name in result.columns if name!=result.geometry.name]
    assert [f.name for f in layer.schema]==fields
    for i,f in enumerate(layer):
        assert f.GetGeometryRef().GetGeometryName()=="LINESTRING" and f.GetGeometryRef().GetPoints()==coordinates[i]
        for name in fields:
            value=result[name].iloc[i];expected=None if pd.isna(value) else value
            assert f.GetField(name)==expected
        assert f.GetField("geometry")==(["业务几何名甲"]*5+["业务几何名乙"])[i]
    assert_geodataframe_equal(frame,before)


@pytest.mark.parametrize('problem',[None,'direction','coordinate','source_id','part','hole','null','big_integer','integer_type','boolean_type','boolean_value','crs','missing','extra_output','source_changed'])
def test_hosted_polygon_rings_save_two_failures_and_independent_oracle(tmp_path,problem):
    import importlib
    from osgeo import ogr
    fixture=importlib.import_module('business.scripts.online-vector-topology-fixture');acceptance=importlib.import_module('scripts.test.vector-topology-persistence-online')
    (tmp_path/'outputs').mkdir();(tmp_path/'.addp-online-owner').touch();seeded=fixture.worker('seed',tmp_path,fixture.RINGS_CASE)
    def definition(key=None):
        workflow=acceptance.rings_definition(list(fixture.RINGS_SOURCES),'unused',key)
        for task in workflow['tasks']:
            if task['operator']=='load':task['params']={'connection_info':{'engine_type':'nfs','mount_path':str(tmp_path)},'path':task['params']['locator']}
            elif task['operator']=='save':
                name=task['params'].pop('target_name');task['params'].pop('target_parent_locator');task['params'].update(connection_info={'engine_type':'nfs','mount_path':str(tmp_path)},path='outputs/'+name)
        return workflow
    result=execute_workflow(definition());assert result['status']=='success',result
    native=fixture.worker('verify',tmp_path,fixture.RINGS_CASE);assert native['sources']==seeded['sources'];assert native['targets'][fixture.RINGS_TARGET]['feature_count']==8
    if problem is None:
        for key,reason in fixture.RINGS_FAILURES:
            failed=execute_workflow(definition(key));assert failed['status']=='failed' and reason in failed['error'],failed
            assert fixture.worker('verify',tmp_path,fixture.RINGS_CASE)==native
        archive=tmp_path.parent/(tmp_path.name+'-archive');fixture.archive_physical_evidence(tmp_path,archive,native,fixture.RINGS_CASE)
        assert len(list((archive/'topology-physical').rglob('*.gpkg')))==4
        return
    path=tmp_path/'outputs'/fixture.RINGS_TARGET
    if problem=='extra_output':(tmp_path/'outputs/extra.gpkg').write_bytes(b'reject')
    else:
        if problem=='source_changed':path=tmp_path/fixture.RINGS_SOURCES[0]
        ds=ogr.Open(str(path),1);layer=ds.GetLayer(0)
        if problem=='boolean_value':ds.ExecuteSQL('UPDATE "'+layer.GetName()+'" SET is_hole=2 WHERE fid=1')
        elif problem=='crs':ds.ExecuteSQL('UPDATE gpkg_geometry_columns SET srs_id=4326')
        elif problem=='missing':layer.DeleteFeature(1)
        elif problem in ('integer_type','boolean_type'):
            name='part_no' if problem=='integer_type' else 'is_hole';assert layer.AlterFieldDefn(layer.GetLayerDefn().GetFieldIndex(name),ogr.FieldDefn(name,ogr.OFTReal),ogr.ALTER_TYPE_FLAG)==0
        else:
            feature=layer.GetFeature(6 if problem=='big_integer' else 1)
            if problem=='source_changed':feature.SetField('note','changed')
            elif problem=='source_id':feature.SetField('source_id','foreign')
            elif problem=='part':feature.SetField('part_no',99)
            elif problem=='hole':feature.SetField('is_hole',1)
            elif problem=='null':feature.SetField('grade',0)
            elif problem=='big_integer':feature.SetField('grade',2**53)
            else:
                points=[list(v) for v in feature.GetGeometryRef().GetPoints()]
                if problem=='direction':points.reverse()
                else:points[1][0]+=.25
                feature.SetGeometry(ogr.CreateGeometryFromJson(json.dumps({'type':'LineString','coordinates':points})))
            layer.SetFeature(feature);feature=None
        layer=ds=None
    with pytest.raises(fixture.FixtureError):fixture.worker('verify',tmp_path,fixture.RINGS_CASE)


def test_extract_polygon_rings_conflict_uses_fixed_priority_before_validity(ring_extractor):
    from operators.geometric_operators import PolygonRingInputError
    frame=gpd.GeoDataFrame({'key':['private'], 'is_hole':[True],'ring_id':[7],'ring_no':[3],'part_no':[2],'source_id':['private']},
        geometry=[shapely.from_wkt('POLYGON ((0 0,2 2,0 2,2 0,0 0))')],crs=3857)
    for field in ['source_id','part_no','ring_no','ring_id','is_hole']:
        before=frame.copy(deep=True)
        with pytest.raises(PolygonRingInputError) as caught:ring_extractor(frame,'key')
        assert (caught.value.code,caught.value.field)==('field_conflict',field)
        assert frame.geometry.to_wkb().tolist()==before.geometry.to_wkb().tolist()
        pd.testing.assert_frame_equal(frame.drop(columns='geometry'),before.drop(columns='geometry'))
        frame=frame.drop(columns=field)
    with pytest.raises(PolygonRingInputError) as caught:ring_extractor(frame,'key')
    assert (caught.value.code,caught.value.field)==('geometry_invalid','')


@pytest.mark.parametrize('wkt', [None,'POLYGON EMPTY','POINT (0 0)','POLYGON Z ((0 0 1,1 0 1,1 1 1,0 0 1))',
    'POLYGON M ((0 0 1,1 0 1,1 1 1,0 0 1))','POLYGON ((0 0,1 0,Infinity 1,0 0))',
    'MULTIPOLYGON (EMPTY,((0 0,1 0,1 1,0 0)))'])
def test_extract_polygon_rings_other_rejections_are_not_validity_diagnostics(ring_extractor,wkt):
    from operators.geometric_operators import PolygonRingInputError
    frame=gpd.GeoDataFrame({'key':[1]},geometry=[None if wkt is None else shapely.from_wkt(wkt)],crs=3857)
    with pytest.raises(ValueError) as caught:ring_extractor(frame,'key')
    assert not isinstance(caught.value,PolygonRingInputError)


@pytest.mark.parametrize('key',['ring-field-conflict','ring-geometry-invalid'])
def test_hosted_polygon_ring_diagnostics_only_two_real_file_rejections(tmp_path,key,monkeypatch):
    import importlib
    import api_server
    from addp_common.workflow_runtime import ExecutionRegistry
    fixture=importlib.import_module('business.scripts.online-vector-topology-fixture')
    acceptance=importlib.import_module('scripts.test.vector-topology-persistence-online')
    (tmp_path/'outputs').mkdir();(tmp_path/'.addp-online-owner').touch()
    original=fixture.worker('seed',tmp_path,fixture.RINGS_DIAGNOSTIC_CASE)
    assert set(original['sources'])==set(fixture.RINGS_DIAGNOSTIC_SOURCES) and not original['targets']
    workflow=acceptance.diagnostic_definition(key,list(fixture.RINGS_DIAGNOSTIC_SOURCES),'unused-parent')
    for task in workflow['tasks']:
        if task['operator']=='load':task['params']={'connection_info':{'engine_type':'nfs','mount_path':str(tmp_path)},'path':task['params']['locator']}
        elif task['operator']=='save':
            task['params'].pop('target_parent_locator');task['params'].pop('target_name')
            task['params'].update(connection_info={'engine_type':'nfs','mount_path':str(tmp_path)},path='outputs/forbidden.gpkg')
    monkeypatch.setattr(api_server,'executions',ExecutionRegistry())
    with api_server.app.test_client() as client:
        response=client.post('/api/workflow',json={'workflow_def':workflow,'runtime':{'tenant_id':7,'execution_authorization':{'id':1,'effects':['read','write']}}})
        assert response.status_code==202
        deadline=time.monotonic()+10
        while time.monotonic()<deadline:
            state=client.get('/api/executions/'+response.get_json()['execution_id']).get_json()
            if state['status'] in ('failed','success'):break
            time.sleep(.01)
    spec=next(spec for spec in acceptance.RINGS_DIAGNOSTIC_SPECS if spec[0]==key)
    assert state['status']=='failed' and state['error_code']=='INVALID_PARAMS',state
    assert state['diagnostic']==acceptance.expected_input_diagnostic(spec)
    assert state['diagnostic']['operator']['node_index']==1
    assert fixture.worker('verify',tmp_path,fixture.RINGS_DIAGNOSTIC_CASE)==original
    fixture.archive_physical_evidence(tmp_path,tmp_path.parent/(key+'-evidence'),original,fixture.RINGS_DIAGNOSTIC_CASE)
    assert not list((tmp_path/'outputs').iterdir())
    (tmp_path/'outputs/forbidden.gpkg').touch()
    with pytest.raises(fixture.FixtureError,match='no physical publication'):fixture.worker('verify',tmp_path,fixture.RINGS_DIAGNOSTIC_CASE)


# Line trimming: original-path distance, branch boundaries and exact immutable plans.
def line_trim(source, reference, maximum=2.):
    return get_operator('trim_line_endpoints')(
        input_gdf=source, reference_gdf=reference,
        id_field='route_key', reference_id_field='ref_key', max_distance=maximum)


def line_trim_frames(crs='EPSG:3857'):
    source, _ = endpoint_snap_frames(crs)
    source[source.geometry.name] = [LineString([(0,0),(0,0),(2,0),(2,2),(4,2),(4,2)]),
                                    LineString([(20,0),(20,10)])]
    reference = gpd.GeoDataFrame({'ref_key':pd.Series(['first','duplicate','later','end','other'],dtype='string')},
        geometry=[LineString([(1,-1),(1,1)]),LineString([(1,1),(1,-1)]),
                  LineString([(2,.5),(3,.5)]),LineString([(3,1),(3,3)]),
                  LineString([(19,1),(21,1)])],crs=crs).rename_geometry('reference_shape')
    return source,reference


def test_line_trim_frozen_both_ends_all_owners_point_details_and_attribute_types():
    source,reference=line_trim_frames();before=source.copy(deep=True);ref_before=reference.copy(deep=True)
    out=line_trim(source,reference)
    result,details=out.values()
    assert result.geometry.iloc[0].coords[:]==[(1.,0.),(2.,0.),(2.,2.),(3.,2.)]
    assert result.geometry.iloc[1].coords[:]==[(20.,1.),(20.,10.)]
    assert details.source_id.tolist()==[2**53+1]*3+[9]
    assert details.reference_id.tolist()==['first','duplicate','end','other']
    assert details['end'].tolist()==['start','start','end','start']
    assert details['distance'].tolist()==[1.]*4 and details.part_no.tolist()==[1]*4
    assert [tuple(g.coords[0]) for g in details.geometry]==[(1.,0.),(1.,0.),(3.,2.),(20.,1.)]
    assert details.source_id.dtype==source.route_key.dtype and details.reference_id.dtype==reference.ref_key.dtype
    assert str(details.part_no.dtype)=='int64' and str(details['end'].dtype)=='string'
    assert all(str(details[c].dtype)=='float64' for c in ['original_x','original_y','target_x','target_y','distance'])
    assert result.geometry.name==source.geometry.name and result.index.equals(source.index)
    pd.testing.assert_frame_equal(pd.DataFrame(result.drop(columns=result.geometry.name)),pd.DataFrame(source.drop(columns=source.geometry.name)))
    assert_geodataframe_equal(source,before);assert_geodataframe_equal(reference,ref_before)


@pytest.mark.parametrize('maximum,changed',[(4.,True),(np.nextafter(4.,0.),False),(np.nextafter(4.,5.),True)])
def test_line_trim_path_length_not_endpoint_distance_and_inclusive_limit(maximum,changed):
    source=line_overlap_frame([LineString([(0,0),(2,0),(2,4),(10,4)])])
    reference=line_overlap_frame([LineString([(1,2),(3,2)])]).rename(columns={'route_key':'ref_key'})
    result=line_trim(source,reference,maximum)
    assert result['result'].geometry.iloc[0].coords[0]==((2.,2.) if changed else (0.,0.))
    assert result['details']['distance'].tolist()==([4.] if changed else [])


@pytest.mark.parametrize('target_x,trimmed',[(1.,True),(2.,True),(3.,False)])
def test_line_trim_first_branch_stops_search_but_reference_on_branch_allowed(target_x,trimmed):
    source=line_overlap_frame([LineString([(0,0),(6,0)]),LineString([(2,0),(2,4)])])
    reference=line_overlap_frame([LineString([(target_x,-1),(target_x,1)])]).rename(columns={'route_key':'ref_key'})
    out=line_trim(source,reference,3.)
    assert out['result'].geometry.iloc[0].coords[0]==((target_x,0.) if trimmed else (0.,0.))
    assert out['result'].geometry.iloc[0].coords[-1]==((3.,0.) if target_x==3. else (6.,0.))


@pytest.mark.parametrize('refs',[[(2,0),(4,0)],[(4,0),(2,0)]])
def test_line_trim_collinear_entry_and_dedup_reference_segments(refs):
    source=line_overlap_frame([LineString([(0,0),(10,0)])])
    reference=line_overlap_frame([MultiLineString([refs,refs[::-1]])]).rename(columns={'route_key':'ref_key'})
    out=line_trim(source,reference,2.)
    assert out['result'].geometry.iloc[0].coords[:]==[(2.,0.),(10.,0.)]
    assert out['details'].reference_id.tolist()==[0] and out['details']['distance'].tolist()==[2.]


@pytest.mark.parametrize('crs',['EPSG:3857','EPSG:2263'])
def test_line_trim_vertex_cut_keeps_inner_repeats_multipart_order_and_native_units(crs):
    source=line_overlap_frame([MultiLineString([[(0,0),(1,0),(1,0),(5,0)],[(10,0),(15,0)]]),
                               LineString([(30,0),(31,0),(31,1),(30,0)])],crs=crs)
    reference=line_overlap_frame([LineString([(1,-1),(1,1)]),LineString([(14,-1),(14,1)])],crs=crs).rename(columns={'route_key':'ref_key'})
    out=line_trim(source,reference,1.)
    assert [p.coords[:] for p in out['result'].geometry.iloc[0].geoms]==[[(1.,0.),(1.,0.),(5.,0.)],[(10.,0.),(14.,0.)]]
    assert out['details'].part_no.tolist()==[1,2] and out['details']['end'].tolist()==['start','end']
    assert out['result'].geometry.iloc[1].wkb==source.geometry.iloc[1].wkb and out['result'].crs==source.crs


@pytest.mark.parametrize('which',['source','reference','both'])
def test_line_trim_empty_batches_keep_exact_schema(which):
    source,reference=line_trim_frames()
    if which in ('source','both'):source=source.iloc[:0]
    if which in ('reference','both'):reference=reference.iloc[:0]
    out=line_trim(source,reference)
    assert_geodataframe_equal(out['result'],source);assert out['details'].empty
    assert out['details'].source_id.dtype==source.route_key.dtype and out['details'].reference_id.dtype==reference.ref_key.dtype
    assert out['details'].columns.tolist()==['source_id','reference_id','part_no','end','original_x','original_y','target_x','target_y','distance','geometry']


@pytest.mark.parametrize('bad',[0,-1,True,np.bool_(False),float('inf'),float('nan'),'1',None])
def test_line_trim_limit_rejection_is_typed(bad):
    from operators.geometric_operators import LineTrimError
    source,reference=line_trim_frames()
    with pytest.raises(LineTrimError) as error:line_trim(source,reference,bad)
    assert error.value.code=='tolerance'


@pytest.mark.parametrize('shapes',[
    [LineString([(0,0),(2,2),(0,2),(2,0)])],
    [LineString([(0,0),(2,0),(1,0),(1,2)])],
    [LineString([(0,0),(4,0)]),LineString([(1,0),(3,0)])],
    [MultiLineString([[(0,0),(4,0)],[(3,0),(1,0)]])],
])
def test_line_trim_rejects_internal_self_intersections_or_any_source_overlap_even_without_target(shapes):
    from operators.geometric_operators import LineTrimError
    source=line_overlap_frame(shapes);reference=source.iloc[:0].rename(columns={'route_key':'ref_key'})
    with pytest.raises(LineTrimError) as error:line_trim(source,reference)
    assert error.value.code=='topology'


def test_line_trim_two_original_plans_meet_entire_part_rejected_without_mutating_previous_result():
    from operators.geometric_operators import LineTrimError
    source=line_overlap_frame([LineString([(10,0),(20,0)]),LineString([(0,0),(4,0)])])
    reference=line_overlap_frame([LineString([(11,-1),(11,1)]),LineString([(2,-1),(2,1)])]).rename(columns={'route_key':'ref_key'})
    before=source.copy(deep=True)
    with pytest.raises(LineTrimError) as error:line_trim(source,reference,2.)
    assert error.value.code=='conflict';assert_geodataframe_equal(source,before)


def test_line_trim_already_covered_end_no_target_and_degree_two_non_dangles_do_not_move():
    source=line_overlap_frame([LineString([(0,0),(4,0)]),LineString([(4,0),(8,0)])])
    reference=line_overlap_frame([LineString([(0,0),(0,1)]),LineString([(5,-1),(5,1)])]).rename(columns={'route_key':'ref_key'})
    out=line_trim(source,reference,1.)
    assert_geodataframe_equal(out['result'],source);assert out['details'].empty


def test_line_trim_exact_tiny_binary_target_and_unrepresentable_first_hit_rejected():
    from operators.geometric_operators import LineTrimError
    tiny=2.**-500
    source=line_overlap_frame([LineString([(0,0),(8*tiny,0)])])
    reference=line_overlap_frame([LineString([(tiny,-tiny),(tiny,tiny)])]).rename(columns={'route_key':'ref_key'})
    out=line_trim(source,reference,tiny)
    assert out['result'].geometry.iloc[0].coords[0]==(tiny,0.) and out['details']['distance'].tolist()==[tiny]
    source=line_overlap_frame([LineString([(0,0),(8,0)])])
    reference=line_overlap_frame([LineString([(0,1),(1,-2)]),LineString([(1,-1),(1,1)])]).rename(columns={'route_key':'ref_key'})
    with pytest.raises(LineTrimError) as error:line_trim(source,reference)
    assert error.value.code=='landing'


def test_line_trim_reference_reverse_order_and_source_reverse_preserve_targets_and_duplicate_index():
    source,reference=line_trim_frames();source.index=[7,7]
    source[source.geometry.name]=source.geometry.map(shapely.reverse)
    reference=reference.iloc[::-1].copy();reference[reference.geometry.name]=reference.geometry.map(shapely.reverse)
    out=line_trim(source,reference)
    assert out['result'].geometry.iloc[0].coords[:]==[(3.,2.),(2.,2.),(2.,0.),(1.,0.)]
    assert out['result'].index.tolist()==[7,7]
    assert out['details'].reference_id.tolist()==['end','duplicate','first','other']


@pytest.mark.parametrize('problem,reason,parameter',[
    ('limit','operator_distance_limit_invalid','max_distance'),('source','operator_input_invalid','input_gdf'),
    ('reference','operator_reference_invalid','reference_gdf'),('crs','operator_crs_invalid',''),
    ('topology','operator_source_topology_invalid','input_gdf'),('conflict','operator_trim_conflict',''),
    ('landing',None,''),
])
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_line_trim_runner_typed_diagnostics_bind_frozen_position_never_leak_or_classify_calculation(problem,reason,parameter,language):
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime import WorkflowExecutionError
    source,reference=line_trim_frames();maximum=2.
    if problem=='limit':maximum=0
    elif problem=='source':source['route_key']=None
    elif problem=='reference':reference['ref_key']=None
    elif problem=='crs':reference=reference.set_crs('EPSG:2263',allow_override=True)
    else:
        source=line_overlap_frame([LineString([(0,0),(4,0)])])
        if problem=='topology':source[source.geometry.name]=[LineString([(0,0),(2,2),(0,2),(2,0)])]
        ref=LineString([(0,1),(1,-2)]) if problem=='landing' else LineString([(2,-1),(2,1)])
        reference=line_overlap_frame([ref]).rename(columns={'route_key':'ref_key'})
    params=dict(input_gdf=source,reference_gdf=reference,id_field='route_key',reference_id_field='ref_key',max_distance=maximum)
    task={'id':'PRIVATE_NODE','operator':'trim_line_endpoints','params':params,'depends_on':[]}
    with pytest.raises(WorkflowExecutionError) as error:GeoPythonWorkflowRunner(language=language).execute({'tasks':[task]})
    failure=error.value
    if reason is None:
        assert failure.error_code=='EXECUTION_FAILED' and failure.diagnostic is None
    else:
        assert failure.error_code=='INVALID_PARAMS'
        assert failure.diagnostic=={'schema_version':'addp.execution-failure/v3','stage':'execution','reason':reason,
            'operator':{'operator_id':'trim_line_endpoints','node_index':1,**({'parameter':parameter} if parameter else {})}}
        assert 'PRIVATE' not in json.dumps(failure.diagnostic)


def test_line_trim_metadata_explicit_two_ports_one_owner_and_no_direct_invoke():
    spec=next(op for op in list_operators() if op['id']=='trim_line_endpoints')
    assert spec['display_name']=='线端修剪' and spec['category_path']==['矢量处理','几何整理']
    assert spec['execution_modes']==['workflow'] and spec['effects']==['read']
    assert [(p['name'],p['is_default']) for p in spec['output_ports']]==[('result',True),('details',False)]
    assert {p['name'] for p in spec['parameters']}=={'input_gdf','reference_gdf','id_field','reference_id_field','max_distance'}


@pytest.mark.parametrize('problem',[None,'conflict','limit','topology','landing'])
def test_line_trim_workflow_dual_save_independent_ogr_failure_no_publish(tmp_path,problem):
    from osgeo import ogr
    source,reference=line_trim_frames()
    if problem in ('conflict','topology','landing'):
        source=line_overlap_frame([LineString([(0,0),(4,0)])])
        if problem=='topology':source[source.geometry.name]=[LineString([(0,0),(2,2),(0,2),(2,0)])]
        reference=line_overlap_frame([LineString([(0,1),(1,-2)]) if problem=='landing' else LineString([(2,-1),(2,1)])]).rename(columns={'route_key':'ref_key'})
    for name,frame in [('source',source),('reference',reference)]:frame.to_file(tmp_path/f'{name}.gpkg',driver='GPKG',index=False)
    originals={name:(tmp_path/f'{name}.gpkg').read_bytes() for name in ('source','reference')}
    conn={'engine_type':'nfs','mount_path':str(tmp_path)}
    tasks=[{'id':name,'operator':'load','params':{'connection_info':conn,'path':f'{name}.gpkg'},'depends_on':[]} for name in ('source','reference')]
    tasks.insert(0,{'id':'trim','operator':'trim_line_endpoints','params':{'input_gdf':{'$ref':'source'},'reference_gdf':{'$ref':'reference'},'id_field':'route_key','reference_id_field':'ref_key','max_distance':0 if problem=='limit' else 2.},'depends_on':['source','reference']})
    for name,ref in [('lines',{'$ref':'trim'}),('details',{'$ref':'trim','port':'details'})]:
        tasks.append({'id':f'save_{name}','operator':'save','params':{'input_df':ref,'connection_info':conn,'path':f'{name}.gpkg','mode':'fail'},'depends_on':['trim']})
    outcome=execute_workflow({'tasks':tasks})
    assert all((tmp_path/f'{name}.gpkg').read_bytes()==value for name,value in originals.items())
    if problem:
        assert outcome['status']=='failed'
        assert outcome['error_code']==('EXECUTION_FAILED' if problem=='landing' else 'INVALID_PARAMS')
        if problem!='landing':assert outcome['diagnostic']['operator']['node_index']==1
        assert not (tmp_path/'lines.gpkg').exists() and not (tmp_path/'details.gpkg').exists()
        return
    assert outcome['status']=='success',outcome
    ds=ogr.Open(str(tmp_path/'lines.gpkg'));layer=ds.GetLayer(0);rows=list(layer)
    assert layer.GetSpatialRef().GetAuthorityCode(None)=='3857' and len(rows)==2
    assert rows[0].GetGeometryRef().GetPoints()==[(1.,0.),(2.,0.),(2.,2.),(3.,2.)]
    assert rows[0].GetField('route_key')==2**53+1 and rows[0].GetField('grade')==3
    assert rows[1].GetField('grade') is None and rows[1].GetField('enabled') is None
    assert layer.GetLayerDefn().GetFieldDefn(layer.GetLayerDefn().GetFieldIndex('grade')).GetType()==ogr.OFTInteger64
    ds2=ogr.Open(str(tmp_path/'details.gpkg'));details=ds2.GetLayer(0);features=list(details)
    assert details.GetSpatialRef().GetAuthorityCode(None)=='3857' and len(features)==4
    assert [f.GetGeometryRef().GetPoint_2D() for f in features]==[(1.,0.),(1.,0.),(3.,2.),(20.,1.)]
    assert [f.GetField('reference_id') for f in features]==['first','duplicate','end','other']


@pytest.mark.parametrize('language',['zh-cn','en'])
def test_line_trim_async_runtime_admission_uses_unified_diagnostic_and_rejects_direct_invocation(language,monkeypatch):
    import api_server
    from addp_common.workflow_runtime import ExecutionRegistry
    monkeypatch.setattr(api_server,'executions',ExecutionRegistry())
    source,reference=line_trim_frames()
    params={'input_gdf':json.loads(source.to_json()),'reference_gdf':json.loads(reference.to_json()),
            'id_field':'route_key','reference_id_field':'ref_key','max_distance':0}
    workflow={'tasks':[{'id':'PRIVATE_NODE','operator':'trim_line_endpoints','params':params,'depends_on':[]}]}
    with api_server.app.test_client() as client:
        assert client.post('/api/operators/trim_line_endpoints/invoke',json={'params':params}).status_code==403
        assert client.post('/api/workflow',json={'workflow_def':workflow}).status_code==400
        reply=client.post('/api/workflow',headers={'Accept-Language':language},json={'workflow_def':workflow,
            'runtime':{'tenant_id':7,'execution_authorization':{'id':1,'effects':['read']}}})
        assert reply.status_code==202
        deadline=time.monotonic()+5
        while time.monotonic()<deadline:
            state=client.get('/api/executions/'+reply.get_json()['execution_id']).get_json()
            if state['status'] in {'success','failed'}:break
            time.sleep(.01)
        assert state['status']=='failed' and state['error_code']=='INVALID_PARAMS'
        assert state['diagnostic']['operator']=={'node_index':1,'operator_id':'trim_line_endpoints','parameter':'max_distance'}
        assert 'PRIVATE' not in json.dumps(state['diagnostic'])
        assert ('最大路径距离' if language=='zh-cn' else 'maximum trim path distance') in state['error']


@pytest.mark.parametrize('problem',['source_id','reference_id','source_crs','empty_geometry','z','point'])
def test_line_trim_rejects_source_reference_admission_without_guessing_fields(problem):
    from operators.geometric_operators import LineTrimError
    source,reference=line_trim_frames()
    if problem=='source_id':source['route_key']=[1,1]
    elif problem=='reference_id':reference['ref_key']=None
    elif problem=='source_crs':source=source.set_crs(None,allow_override=True)
    elif problem=='empty_geometry':source.loc[source.index[0],source.geometry.name]=LineString()
    elif problem=='z':source.loc[source.index[0],source.geometry.name]=LineString([(0,0,0),(1,0,0)])
    else:source.loc[source.index[0],source.geometry.name]=Point(0,0)
    with pytest.raises(LineTrimError) as error:line_trim(source,reference)
    assert error.value.code==('reference' if problem=='reference_id' else 'source')


def test_line_trim_distinct_parts_touch_at_degree_two_without_becoming_eligible_dangles():
    source=line_overlap_frame([MultiLineString([[(0,0),(4,0)],[(4,0),(8,0)]])])
    reference=line_overlap_frame([LineString([(5,-1),(5,1)])]).rename(columns={'route_key':'ref_key'})
    out=line_trim(source,reference,2.)
    assert_geodataframe_equal(out['result'],source);assert out['details'].empty


def test_line_trim_inexact_branch_before_inexact_reference_stops_without_rounding_or_false_landing():
    source=line_overlap_frame([LineString([(0,0),(10,0)]),LineString([(0,1),(1,-2)])])
    reference=line_overlap_frame([LineString([(1,1),(2,-2)])]).rename(columns={'route_key':'ref_key'})
    out=line_trim(source,reference,2.)
    assert out['result'].geometry.iloc[0].coords[:]==[(0.,0.),(10.,0.)]


def test_line_trim_partial_distance_does_not_underflow_the_segment_parameter():
    source=line_overlap_frame([LineString([(0,0),(1e200,0)])])
    reference=line_overlap_frame([LineString([(1e-200,-1),(1e-200,1)])]).rename(columns={'route_key':'ref_key'})
    out=line_trim(source,reference,1e-200)
    assert out['result'].geometry.iloc[0].coords[0]==(1e-200,0.)
    assert out['details']['distance'].tolist()==[1e-200]


def test_line_trim_custom_active_geometry_and_ordinary_geometry_business_column_never_collide():
    source,reference=line_trim_frames()
    source=source.rename_geometry('distance')
    source['geometry']=pd.Series(['ordinary',None],index=source.index,dtype='string')
    before=source.copy(deep=True)
    out=line_trim(source,reference)
    assert out['result'].geometry.name=='distance'
    pd.testing.assert_series_equal(out['result']['geometry'],before['geometry'])
    assert out['details'].geometry.name=='geometry' and out['details']['distance'].tolist()==[1.]*4
    assert_geodataframe_equal(source,before)


@pytest.mark.parametrize('problem',[None,'coordinate','direction','source_id','reference_id','distance','part','null','integer_type','boolean_type','boolean_value','crs','missing','source_changed','extra_output'])
def test_hosted_line_trim_case_dual_save_eight_failures_and_independent_oracle(tmp_path,problem):
    import importlib
    from osgeo import ogr
    fixture=importlib.import_module('business.scripts.online-vector-topology-fixture');acceptance=importlib.import_module('scripts.test.vector-topology-persistence-online')
    (tmp_path/'outputs').mkdir();(tmp_path/'.addp-online-owner').touch()
    seeded=fixture.worker('seed',tmp_path,fixture.TRIM_CASE)
    originals={name:(tmp_path/name).read_bytes() for name in fixture.TRIM_SOURCES}
    def definition(failure=None):
        workflow=acceptance.trim_definition(list(fixture.TRIM_SOURCES),'unused',failure=failure)
        for task in workflow['tasks']:
            if task['operator']=='load':task['params']={'connection_info':{'engine_type':'nfs','mount_path':str(tmp_path)},'path':task['params']['locator']}
            elif task['operator']=='save':
                name=task['params'].pop('target_name');task['params'].pop('target_parent_locator')
                task['params'].update(connection_info={'engine_type':'nfs','mount_path':str(tmp_path)},path='outputs/'+name)
        return workflow
    result=execute_workflow(definition());assert result['status']=='success',result
    native=fixture.worker('verify',tmp_path,fixture.TRIM_CASE)
    assert native['sources']==seeded['sources'] and [native['targets'][name]['feature_count'] for name in fixture.TRIM_TARGETS]==[15,9]
    assert all((tmp_path/name).read_bytes()==raw for name,raw in originals.items())
    if problem is None:
        for spec in fixture.TRIM_FAILURES:
            key=spec[0]
            failed=execute_workflow(definition(key));assert failed['status']=='failed',failed
            if spec[3]:
                assert failed['error_code']=='INVALID_PARAMS' and failed['diagnostic']==acceptance.trim_diagnostic(spec),failed
            else:
                assert failed['error_code']=='EXECUTION_FAILED' and fixture.TRIM_LANDING_FAILURE in failed['error'] and not failed.get('diagnostic'),failed
            assert not any((tmp_path/'outputs'/('failed-'+key+'-'+name)).exists() for name in fixture.TRIM_TARGETS)
            assert fixture.worker('verify',tmp_path,fixture.TRIM_CASE)==native
        archive=tmp_path.parent/(tmp_path.name+'-archive');fixture.archive_physical_evidence(tmp_path,archive,native,fixture.TRIM_CASE)
        assert len(list((archive/'topology-physical').rglob('*.gpkg')))==13
        return
    path=tmp_path/'outputs'/fixture.TRIM_TARGETS[0 if problem in ('null','integer_type','boolean_type','boolean_value','direction') else 1]
    if problem=='extra_output':(tmp_path/'outputs/extra.gpkg').write_bytes(b'rejected')
    else:
        if problem=='source_changed':path=tmp_path/fixture.TRIM_SOURCES[0]
        ds=ogr.Open(str(path),1);layer=ds.GetLayer(0)
        if problem=='boolean_value':ds.ExecuteSQL('UPDATE "'+layer.GetName()+'" SET enabled=2 WHERE fid=1')
        elif problem=='crs':ds.ExecuteSQL('UPDATE gpkg_geometry_columns SET srs_id=4326')
        elif problem=='missing':layer.DeleteFeature(1)
        elif problem in ('integer_type','boolean_type'):
            field='grade' if problem=='integer_type' else 'enabled'
            assert layer.AlterFieldDefn(layer.GetLayerDefn().GetFieldIndex(field),ogr.FieldDefn(field,ogr.OFTReal),ogr.ALTER_TYPE_FLAG)==0
        else:
            feature=layer.GetFeature(2 if problem=='null' else 1)
            if problem=='null':feature.SetField('grade',99)
            elif problem=='source_id':feature.SetField('source_id',1)
            elif problem=='reference_id':feature.SetField('reference_id','foreign')
            elif problem=='distance':feature.SetField('distance',0.)
            elif problem=='part':feature.SetField('part_no',99)
            elif problem=='source_changed':feature.SetField('note','changed')
            else:
                points=fixture.proximity_geometry(feature.GetGeometryRef())
                if problem=='direction':points['coordinates'][0].reverse()
                else:points['coordinates'][0]+=1
                feature.SetGeometry(ogr.CreateGeometryFromJson(json.dumps(points)))
            layer.SetFeature(feature);feature=None
        layer=None;ds=None
    with pytest.raises(fixture.FixtureError):fixture.worker('verify',tmp_path,fixture.TRIM_CASE)


def test_extract_line_nodes_closed_ring_twice_and_complete_part_incidence():
    frame = line_overlap_frame([
        MultiLineString([[(0,0),(1,0),(0,1),(0,0)], [(0,0),(-1,0)]]),
        LineString([(-1,0),(-2,0),(-2,1)]),
    ], pd.Series([2**40+1,2**40+3],dtype='Int64'))
    frame['grade']=pd.Series([None,2**40],dtype='Int64')
    frame['enabled']=pd.Series([None,False],dtype='boolean')
    frame['geometry']=pd.Series(['普通业务几何',None],dtype='string')
    frame.index=[9,9]
    before=frame.copy(deep=True)
    out=get_operator('extract_line_nodes')(frame,'route_key')
    result,members=out['result'],out['members']
    assert result.geometry.name==members.geometry.name=='boundary'
    assert result.geometry.tolist()==[Point(0,0),Point(-1,0),Point(-2,1)]
    assert result.node_id.tolist()==[1,2,3] and result.degree.tolist()==[3,2,1]
    assert members.node_id.tolist()==[1,1,1,2,2,3]
    assert members.source_id.tolist()==[2**40+1]*4+[2**40+3]*2
    assert members.source_part_no.tolist()==[1,1,2,2,1,1]
    assert members.end.tolist()==['start','end']*3
    assert members.geometry.tolist()==[Point(0,0),Point(0,0),Point(0,0),Point(-1,0),Point(-1,0),Point(-2,1)]
    assert len(members)==result.degree.sum()==6
    assert members['geometry'].tolist()==['普通业务几何']*4+[pd.NA]*2
    for field in ['grade','enabled','geometry','route_key']:
        assert members[field].dtype==frame[field].dtype
    assert members.source_id.dtype==frame.route_key.dtype
    assert result.crs==members.crs==frame.crs
    assert_geodataframe_equal(frame,before)


@pytest.mark.parametrize('shapes,points,degrees',[
    ([LineString([(0,0),(1,0),(1,1),(0,0)])],[(0,0)],[2]),
    ([LineString([(0,0),(1,0)]),LineString([(1,0),(0,1)]),LineString([(0,1),(0,0)])],[(0,0),(1,0),(0,1)],[2,2,2]),
    ([LineString([(0,0),(1,1),(2,0)]),LineString([(0,0),(1,-1),(2,0)])],[(0,0),(2,0)],[2,2]),
    ([LineString([(0,0),(0,0),(1,0),(2,0),(2,0)])],[(0,0),(2,0)],[1,1]),
    ([MultiLineString([[(0,0),(1,0)],[(10,0),(11,0)]])],[(0,0),(1,0),(10,0),(11,0)],[1,1,1,1]),
    ([LineString([(0,0),(1,0)]),LineString([(1+2**-40,0),(2,0)])],[(0,0),(1,0),(1+2**-40,0),(2,0)],[1,1,1,1]),
])
def test_extract_line_nodes_exact_topology_closed_lens_multicomponent_and_microgap(shapes,points,degrees):
    frame=line_overlap_frame(shapes)
    out=get_operator('extract_line_nodes')(frame,'route_key')
    assert [p.coords[0] for p in out['result'].geometry]==points
    assert out['result'].degree.tolist()==degrees
    assert out['result'].degree.sum()==len(out['members'])==2*sum(shapely.get_num_geometries(frame.geometry.array))
    assert all(str(out['result'][k].dtype)=='int64' for k in ['node_id','degree'])


@pytest.mark.parametrize('shapes',[
    [LineString([(0,0),(1,0)]),LineString([(0,0),(1,0)])],
    [LineString([(0,0),(1,0)]),LineString([(1,0),(0,0)])],
    [LineString([(0,0),(4,0)]),LineString([(1,0),(2,0),(3,0)])],
    [MultiLineString([[(0,0),(4,0)],[(3,0),(1,0)]])],
    [LineString([(0,0),(2,0),(1,0)])],
    [LineString([(0,0),(2,2),(0,2),(2,0)])],
    [LineString([(0,0),(2,0)]),LineString([(1,-1),(1,1)])],
    [LineString([(0,0),(2,0)]),LineString([(1,0),(1,1)])],
    [MultiLineString([[(0,0),(2,0)],[(1,0),(1,1)]])],
    [LineString([(0,0),(2,0),(2,2),(0,0)]),LineString([(1,0),(1,-1)])],
])
def test_extract_line_nodes_rejects_overlap_and_unresolved_contacts_atomically(shapes):
    from operators.geometric_operators import LineNodeInputError
    frame=line_overlap_frame(shapes);before=frame.copy(deep=True)
    with pytest.raises(LineNodeInputError) as error:get_operator('extract_line_nodes')(frame,'route_key')
    assert error.value.code=='network'
    assert_geodataframe_equal(frame,before)


@pytest.mark.parametrize('field',['node_id','degree','source_id','source_part_no','end'])
@pytest.mark.parametrize('active',[False,True])
def test_extract_line_nodes_fixed_output_conflicts_include_active_geometry(field,active):
    from operators.geometric_operators import LineNodeInputError
    frame=line_overlap_frame([LineString([(0,0),(1,0)])])
    if active:frame=frame.rename_geometry(field)
    else:frame[field]='PRIVATE_VALUE'
    with pytest.raises(LineNodeInputError) as error:get_operator('extract_line_nodes')(frame,'route_key')
    assert error.value.code=='field_conflict' and error.value.field==field


@pytest.mark.parametrize('crs',[None,'EPSG:4326','EPSG:4978'])
@pytest.mark.parametrize('empty',[False,True])
def test_extract_line_nodes_rejects_nonprojected_even_empty(crs,empty):
    from operators.geometric_operators import LineNodeInputError
    frame=line_overlap_frame([] if empty else [LineString([(0,0),(1,0)])],crs=crs)
    with pytest.raises(LineNodeInputError) as error:get_operator('extract_line_nodes')(frame,'route_key')
    assert error.value.code=='crs'


@pytest.mark.parametrize('ids',[['a','a'],[None,'a'],['  ','a'],[['nested'],'a']])
def test_extract_line_nodes_rejects_nonunique_missing_or_nonscalar_ids(ids):
    from operators.geometric_operators import LineNodeInputError
    frame=line_overlap_frame([LineString([(0,0),(1,0)]),LineString([(1,0),(2,0)])],ids)
    with pytest.raises(LineNodeInputError) as error:get_operator('extract_line_nodes')(frame,'route_key')
    assert error.value.code=='source'


@pytest.mark.parametrize('wkt',['POINT (0 0)','LINESTRING EMPTY','MULTILINESTRING (EMPTY,(0 0,1 0))',
    'LINESTRING (0 0,0 0)','LINESTRING Z (0 0 1,1 0 1)','LINESTRING M (0 0 1,1 0 1)',
    'LINESTRING (0 0,NaN 1)'])
def test_extract_line_nodes_rejects_bad_geometry_admission(wkt):
    from operators.geometric_operators import LineNodeInputError
    frame=line_overlap_frame([shapely.from_wkt(wkt)])
    with pytest.raises(LineNodeInputError) as error:get_operator('extract_line_nodes')(frame,'route_key')
    assert error.value.code=='source'


@pytest.mark.parametrize('language',['zh-cn','en'])
@pytest.mark.parametrize('problem,reason,field',[
    ('source','operator_input_invalid',''),('crs','operator_projected_crs_required',''),
    ('network','operator_endpoint_network_invalid',''),
    *[(field,'operator_field_conflict',field) for field in ['node_id','degree','source_id','source_part_no','end']],
])
def test_extract_line_nodes_typed_diagnostic_exact_frozen_node_and_no_private_fields(language,problem,reason,field):
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime import WorkflowExecutionError
    frame=line_overlap_frame([LineString([(0,0),(1,0)])])
    if problem=='source':frame['route_key']=None
    elif problem=='crs':frame=frame.set_crs('EPSG:4326',allow_override=True)
    elif problem=='network':frame=line_overlap_frame([LineString([(0,0),(1,0),(0,0)])])
    else:frame[field]='PRIVATE_DATA'
    task={'id':'PRIVATE_NODE','operator':'extract_line_nodes','params':{'input_gdf':frame,'id_field':'route_key'},'depends_on':[]}
    with pytest.raises(WorkflowExecutionError) as error:GeoPythonWorkflowRunner(language=language).execute({'tasks':[task]})
    assert error.value.error_code=='INVALID_PARAMS'
    assert error.value.diagnostic=={'schema_version':'addp.execution-failure/v3','stage':'execution','reason':reason,
        'operator':{'node_index':1,'operator_id':'extract_line_nodes',**({} if problem=='crs' else {'parameter':'input_gdf'}),**({'field':field} if field else {})}}
    assert 'PRIVATE' not in json.dumps(error.value.diagnostic)
    assert ('node extraction' in str(error.value).lower()) if language=='en' else ('节点提取' in str(error.value))


def test_extract_line_nodes_native_exception_is_not_reclassified_as_input(monkeypatch):
    from operators import geometric_operators as module
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime import WorkflowExecutionError
    frame=line_overlap_frame([LineString([(0,0),(1,0)])])
    def fail(*args,**kwargs):raise shapely.errors.GEOSException('PRIVATE GEOS calculation')
    monkeypatch.setattr(module,'_endpoint_line_network',fail)
    with pytest.raises(WorkflowExecutionError) as error:GeoPythonWorkflowRunner().execute({'tasks':[
        {'id':'PRIVATE','operator':'extract_line_nodes','params':{'input_gdf':frame,'id_field':'route_key'},'depends_on':[]}]})
    assert error.value.error_code=='EXECUTION_FAILED' and error.value.diagnostic is None


@pytest.mark.parametrize('empty',[False,True])
@pytest.mark.parametrize('id_kind',['Int64','string'])
def test_extract_line_nodes_dual_save_native_ogr_full_schema_source_and_empty_preserved(tmp_path,empty,id_kind):
    from osgeo import ogr
    ids=pd.Series([2**60+1,2**60+3],dtype='Int64') if id_kind=='Int64' else pd.Series(['主环','外支'],dtype='string')
    frame=line_overlap_frame([MultiLineString([[(0,0),(1,0),(0,1),(0,0)],[(0,0),(-1,0)]]),LineString([(-1,0),(-2,0)])],ids)
    frame['grade']=pd.Series([None,2**53+1],dtype='Int64');frame['enabled']=pd.Series([None,False],dtype='boolean')
    frame['geometry']=pd.Series(['普通业务几何',None],dtype='string')
    if empty:frame=frame.iloc[:0]
    frame.to_file(tmp_path/'source.gpkg',driver='GPKG',index=False)
    before=(tmp_path/'source.gpkg').read_bytes();conn={'engine_type':'nfs','mount_path':str(tmp_path)}
    tasks=[{'id':'nodes','operator':'extract_line_nodes','params':{'input_gdf':{'$ref':'load'},'id_field':'route_key'},'depends_on':['load']},
        {'id':'load','operator':'load','params':{'connection_info':conn,'path':'source.gpkg'},'depends_on':[]}]
    for name,ref in [('nodes',{'$ref':'nodes'}),('members',{'$ref':'nodes','port':'members'})]:
        tasks.append({'id':'save_'+name,'operator':'save','params':{'input_df':ref,'connection_info':conn,'path':name+'.gpkg','mode':'fail'},'depends_on':['nodes']})
    result=execute_workflow({'tasks':tasks});assert result['status']=='success',result
    expected_points={'nodes':[(0,0),(-1,0),(-2,0)],'members':[(0,0),(0,0),(0,0),(-1,0),(-1,0),(-2,0)]}
    for name in ['nodes','members']:
        ds=ogr.Open(str(tmp_path/(name+'.gpkg')),0);layer=ds.GetLayer(0)
        assert ds.GetLayerCount()==1 and layer.GetSpatialRef().GetAuthorityCode(None)=='3857'
        assert layer.GetFeatureCount()==(0 if empty else len(expected_points[name]))
        if not empty:assert layer.GetGeomType()==ogr.wkbPoint
        schema={f.name:(f.GetType(),f.GetSubType()) for f in layer.schema}
        if name=='nodes':assert schema=={'node_id':(ogr.OFTInteger64,0),'degree':(ogr.OFTInteger64,0)}
        else:
            assert schema['grade']==(ogr.OFTInteger64,0) and schema['enabled']==(ogr.OFTInteger,ogr.OFSTBoolean)
            assert schema['geometry']==(ogr.OFTString,0) and schema['node_id']==schema['source_part_no']==(ogr.OFTInteger64,0)
            assert schema['source_id']==schema['route_key']==((ogr.OFTInteger64,0) if id_kind=='Int64' else (ogr.OFTString,0))
            assert schema['end']==(ogr.OFTString,0)
        for i,item in enumerate(layer):
            assert item.GetFID()==i+1
            g=item.GetGeometryRef();assert g.GetGeometryName()=='POINT' and g.GetPoint_2D()==expected_points[name][i]
            if name=='nodes':assert item.GetField('node_id')==i+1 and item.GetField('degree')==[3,2,1][i]
            else:
                source=0 if i<4 else 1
                assert item.GetField('source_id')==item.GetField('route_key')==ids.iloc[source]
                assert item.GetField('node_id')==[1,1,1,2,2,3][i] and item.GetField('source_part_no')==[1,1,2,2,1,1][i]
                assert item.GetField('end')==['start','end'][i%2]
                assert item.GetField('geometry')==('普通业务几何' if source==0 else None)
                assert item.GetField('grade')==(None if source==0 else 2**53+1)
                assert item.GetField('enabled')==(None if source==0 else False)
        item=layer=ds=None
    assert (tmp_path/'source.gpkg').read_bytes()==before
    tasks[0]['params']['id_field']='PRIVATE_MISSING';tasks[2]['params']['path']='bad_nodes.gpkg';tasks[3]['params']['path']='bad_members.gpkg'
    failure=execute_workflow({'tasks':tasks})
    assert failure['status']=='failed' and failure['error_code']=='INVALID_PARAMS'
    assert failure['diagnostic']['operator']['node_index']==1
    assert not (tmp_path/'bad_nodes.gpkg').exists() and not (tmp_path/'bad_members.gpkg').exists()
    assert (tmp_path/'source.gpkg').read_bytes()==before


def test_extract_line_nodes_protocol_discovery_async_dual_ports_and_direct_denied(tmp_path,monkeypatch):
    import api_server
    from addp_common.workflow_runtime import ExecutionRegistry
    frame=line_overlap_frame([LineString([(0,0),(1,0)])])
    frame.to_file(tmp_path/'source.gpkg',driver='GPKG',index=False)
    conn={'engine_type':'nfs','mount_path':str(tmp_path)}
    tasks=[{'id':'load','operator':'load','params':{'connection_info':conn,'path':'source.gpkg'},'depends_on':[]},
        {'id':'nodes','operator':'extract_line_nodes','params':{'input_gdf':{'$ref':'load'},'id_field':'route_key'},'depends_on':['load']}]
    monkeypatch.setattr(api_server,'executions',ExecutionRegistry())
    with api_server.app.test_client() as client:
        op=next(o for o in client.get('/api/operators').get_json()['operators'] if o['id']=='extract_line_nodes')
        assert op['display_name']=='节点提取' and op['category_path']==['矢量处理','线网处理']
        assert op['execution_modes']==['workflow'] and op['effects']==['read']
        assert [(p['name'],p['is_default']) for p in op['output_ports']]==[('result',True),('members',False)]
        assert {p['name'] for p in op['parameters']}=={'input_gdf','id_field'}
        assert client.post('/api/operators/extract_line_nodes/invoke',json={'params':{}}).status_code==403
        response=client.post('/api/workflow',json={'workflow_def':{'tasks':tasks},'runtime':{'tenant_id':7,'execution_authorization':{'id':1,'effects':['read']}}})
        assert response.status_code==202,response.get_json()
        execution_id=response.get_json()['execution_id'];deadline=time.monotonic()+10
        while time.monotonic()<deadline:
            status=client.get('/api/executions/'+execution_id).get_json()
            if status['status'] in ('success','failed'):break
            time.sleep(.01)
        assert status['status']=='success',status
        ports=json.loads(status['all_results']['nodes'])
        assert set(ports)=={'result','members'} and len(ports['result']['features'])==len(ports['members']['features'])==2


@pytest.mark.parametrize('problem',['network','field','crs'])
def test_extract_line_nodes_late_invalid_source_prevents_both_saves(tmp_path,problem):
    shapes=[LineString([(100,0),(101,0)]),LineString([(0,0),(1,0),(0,0)]) if problem=='network' else LineString([(0,0),(1,0)])]
    frame=line_overlap_frame(shapes,crs='EPSG:4326' if problem=='crs' else 'EPSG:3857')
    if problem=='field':frame['node_id']='PRIVATE'
    frame.to_file(tmp_path/'input.gpkg',driver='GPKG',index=False)
    before=(tmp_path/'input.gpkg').read_bytes();conn={'engine_type':'nfs','mount_path':str(tmp_path)}
    tasks=[{'id':'load','operator':'load','params':{'connection_info':conn,'path':'input.gpkg'},'depends_on':[]},
        {'id':'nodes','operator':'extract_line_nodes','params':{'input_gdf':{'$ref':'load'},'id_field':'route_key'},'depends_on':['load']}]
    for name,ref in [('points',{'$ref':'nodes'}),('members',{'$ref':'nodes','port':'members'})]:
        tasks.append({'id':'save_'+name,'operator':'save','params':{'input_df':ref,'connection_info':conn,'path':name+'.gpkg','mode':'fail'},'depends_on':['nodes']})
    result=execute_workflow({'tasks':tasks})
    assert result['status']=='failed' and result['error_code']=='INVALID_PARAMS'
    assert not (tmp_path/'points.gpkg').exists() and not (tmp_path/'members.gpkg').exists()
    assert (tmp_path/'input.gpkg').read_bytes()==before


def test_extract_line_nodes_fixed_conflict_priority_and_normalize_explicit_composition():
    from operators.geometric_operators import LineNodeInputError
    frame=line_overlap_frame([LineString([(0,0),(1,0)])])
    for key in ['end','source_part_no','source_id','degree','node_id']:frame[key]=None
    with pytest.raises(LineNodeInputError) as error:get_operator('extract_line_nodes')(frame,'route_key')
    assert error.value.field=='node_id'
    repeated=line_overlap_frame([LineString([(0,0),(1,0)]),LineString([(1,0),(0,0)])])
    normalized=get_operator('normalize_line_network')(repeated,'route_key')
    nodes=get_operator('extract_line_nodes')(normalized['result'],'edge_id')
    assert nodes['result'].degree.tolist()==[1,1]
    assert nodes['members'].edge_id.tolist()==nodes['members'].source_id.tolist()==[1,1]
    assert normalized['members'].source_id.tolist()==[0,1]


def test_extract_line_nodes_nonframe_missing_id_duplicate_columns_and_empty_schema():
    from operators.geometric_operators import LineNodeInputError
    frame=line_overlap_frame([LineString([(0,0),(1,0)])])
    for value,field in [(pd.DataFrame({'a':[1]}),'a'),(frame,'missing'),(frame,frame.geometry.name),
                        (pd.concat([frame,frame[['route_key']]],axis=1),'route_key')]:
        with pytest.raises(LineNodeInputError) as error:get_operator('extract_line_nodes')(value,field)
        assert error.value.code=='source'
    empty=frame.iloc[:0].copy();empty['geometry']=pd.Series([],dtype='string')
    out=get_operator('extract_line_nodes')(empty,'route_key')
    assert list(out['result'])==['boundary','node_id','degree']
    assert out['members'].source_id.dtype==empty.route_key.dtype
    assert str(out['members'].end.dtype)=='string'
    assert out['result'].empty and out['members'].empty and out['result'].crs==empty.crs


@pytest.mark.parametrize('problem',[None,'degree','incidence','part','end','coordinate','null','integer_type','boolean_type','extra_output','source_changed'])
def test_hosted_line_nodes_dual_point_nine_failures_and_independent_oracle(tmp_path,problem):
    import importlib
    from osgeo import ogr
    f=importlib.import_module('business.scripts.online-vector-topology-fixture');a=importlib.import_module('scripts.test.vector-topology-persistence-online')
    (tmp_path/'outputs').mkdir();(tmp_path/'.addp-online-owner').touch();seed=f.worker('seed',tmp_path,f.NODES_CASE)
    def definition(failure=None):
        result=a.nodes_definition(list(f.NODES_SOURCES),'unused',failure)
        for task in result['tasks']:
            if task['operator']=='load':task['params']={'connection_info':{'engine_type':'nfs','mount_path':str(tmp_path)},'path':task['params']['locator']}
            elif task['operator']=='save':
                name=task['params'].pop('target_name');task['params'].pop('target_parent_locator');task['params'].update(connection_info={'engine_type':'nfs','mount_path':str(tmp_path)},path='outputs/'+name)
        return result
    result=execute_workflow(definition());assert result['status']=='success',result
    native=f.worker('verify',tmp_path,f.NODES_CASE);assert native['sources']==seed['sources']
    assert [native['targets'][n]['feature_count'] for n in f.NODES_TARGETS]==[15,22]
    assert sum(r['properties']['degree'] for r in native['targets'][f.NODES_TARGETS[0]]['features'])==22
    if problem is None:
        for spec in f.NODES_FAILURES:
            failed=execute_workflow(definition(spec[0]));assert failed['status']=='failed' and failed['error_code']=='INVALID_PARAMS',failed
            assert failed['diagnostic']==a.nodes_diagnostic(spec),failed
            assert f.worker('verify',tmp_path,f.NODES_CASE)==native
        archive=tmp_path.parent/(tmp_path.name+'-archive');f.archive_physical_evidence(tmp_path,archive,native,f.NODES_CASE)
        assert len(list((archive/'topology-physical').rglob('*.gpkg')))==12
        return
    path=tmp_path/'outputs'/f.NODES_TARGETS[0 if problem=='degree' else 1]
    if problem=='extra_output':(tmp_path/'outputs/extra.gpkg').touch()
    else:
        if problem=='source_changed':path=tmp_path/f.NODES_SOURCES[0]
        ds=ogr.Open(str(path),1);layer=ds.GetLayer(0)
        if problem in ('integer_type','boolean_type'):
            field='grade' if problem=='integer_type' else 'enabled'
            assert layer.AlterFieldDefn(layer.GetLayerDefn().GetFieldIndex(field),ogr.FieldDefn(field,ogr.OFTReal),ogr.ALTER_TYPE_FLAG)==0
        else:
            feature=layer.GetFeature(5 if problem=='null' else 1)
            if problem=='degree':feature.SetField('degree',2)
            elif problem=='incidence':feature.SetField('node_id',2)
            elif problem=='part':feature.SetField('source_part_no',2)
            elif problem=='end':feature.SetField('end','end')
            elif problem=='null':feature.SetField('grade',99)
            elif problem=='source_changed':feature.SetField('geometry','changed')
            else:feature.SetGeometry(ogr.CreateGeometryFromWkt('POINT (99 0)'))
            layer.SetFeature(feature);feature=None
        layer=ds=None
    with pytest.raises(f.FixtureError):f.worker('verify',tmp_path,f.NODES_CASE)


@pytest.mark.parametrize('problem',[None,'coordinate','source_id','order','null','boolean_type','extra_output','source_changed'])
def test_intersection_overlay_t4_fixture_full_mixed_and_four_refusals(tmp_path,problem):
    import importlib
    from osgeo import ogr
    f=importlib.import_module('business.scripts.online-vector-topology-fixture')
    a=importlib.import_module('scripts.test.vector-topology-persistence-online')
    (tmp_path/'outputs').mkdir();(tmp_path/'.addp-online-owner').touch()
    seeded=f.worker('seed',tmp_path,f.OVERLAY_CASE)
    def definition(failure=None):
        result=a.overlay_definition(list(f.OVERLAY_SOURCES),'unused',failure)
        for task in result['tasks']:
            if task['operator']=='load':task['params']={'connection_info':{'engine_type':'nfs','mount_path':str(tmp_path)},'path':task['params']['locator']}
            elif task['operator']=='save':
                name=task['params'].pop('target_name');task['params'].pop('target_parent_locator')
                task['params'].update(connection_info={'engine_type':'nfs','mount_path':str(tmp_path)},path='outputs/'+name)
        return result
    result=execute_workflow(definition());assert result['status']=='success',result
    native=f.worker('verify',tmp_path,f.OVERLAY_CASE)
    assert native['sources']==seeded['sources'] and native['targets'][f.OVERLAY_TARGET]['feature_count']==16
    assert [t['feature_count'] for t in native['source_tables'].values()]==[17,18,18,18]
    assert {row['geometry']['type'] for row in native['targets'][f.OVERLAY_TARGET]['features']}=={'Point','LineString','Polygon','MultiPoint','MultiLineString','MultiPolygon','GeometryCollection'}
    if problem is None:
        original=(tmp_path/'outputs'/f.OVERLAY_TARGET).read_bytes()
        for spec in f.OVERLAY_FAILURES:
            failed=execute_workflow(definition(spec[0]));assert failed['status']=='failed' and failed['error_code']=='INVALID_PARAMS',failed
            assert failed['diagnostic']==a.overlay_diagnostic(spec),failed
            assert f.worker('verify',tmp_path,f.OVERLAY_CASE)==native
            assert (tmp_path/'outputs'/f.OVERLAY_TARGET).read_bytes()==original
        archive=tmp_path.parent/(tmp_path.name+'-archive')
        f.archive_physical_evidence(tmp_path,archive,native,f.OVERLAY_CASE)
        assert len(list((archive/'topology-physical').rglob('*.gpkg')))==5
        return
    path=tmp_path/'outputs'/f.OVERLAY_TARGET
    if problem=='extra_output':(tmp_path/'outputs/extra.gpkg').touch()
    elif problem=='boolean_type':
        import sqlite3
        # Type corruption is performed at the declared schema, not by coercing an equivalent value.
        with sqlite3.connect(path) as conn:
            conn.execute('PRAGMA writable_schema=ON')
            name=native['targets'][f.OVERLAY_TARGET]['layer_name']
            conn.execute("UPDATE sqlite_master SET sql=replace(sql,'BOOLEAN','INTEGER') WHERE name=?",(name,))
    else:
        if problem=='source_changed':path=tmp_path/f.OVERLAY_SOURCES[0]
        ds=ogr.Open(str(path),1);layer=ds.GetLayer(0);row=layer.GetNextFeature()
        if problem in ('coordinate','source_changed'):row.SetGeometry(ogr.CreateGeometryFromWkt('POINT (99 99)'))
        elif problem=='source_id':row.SetField('source_id_a',17)
        elif problem=='null':row.SetFieldNull('a_grade')
        elif problem=='order':
            other=layer.GetFeature(2);other.SetFID(1);row.SetFID(2);layer.SetFeature(other);other=None
        layer.SetFeature(row);row=layer=ds=None
    with pytest.raises(f.FixtureError):f.worker('verify',tmp_path,f.OVERLAY_CASE)


def difference_inputs(a=None,b=None):
    return overlay_inputs(a,[box(1,-1,3,3)] if b is None else b)


def exact_difference(left,right):
    return get_operator('difference')(left,right,id_field='a_key')


def test_difference_exact_whole_mask_not_pairwise_and_one_multipart_per_source():
    left,right=difference_inputs([box(0,0,4,2)],[box(1,-1,2.5,3),box(2,-1,3,3),box(1,-1,2.5,3)])
    result=exact_difference(left,right)
    expected=shapely.MultiPolygon([box(0,0,1,2),box(3,0,4,2)])
    assert len(result)==1 and result.geometry.iloc[0].equals(expected)
    assert list(result.columns)==list(left.columns) and result.a_key.tolist()==[1]

@pytest.mark.parametrize('shape,expected',[
    (Point(0,0),Point(0,0)),(Point(2,0),None),(Point(1,0),None),
    (LineString([(-1,0),(4,0)]),shapely.MultiLineString([[(-1,0),(1,0)],[(3,0),(4,0)]])),
    (box(0,0,4,2),shapely.MultiPolygon([box(0,0,1,2),box(3,0,4,2)])),
    (shapely.MultiPoint([(0,0),(2,0),(4,0)]),shapely.MultiPoint([(0,0),(4,0)])),
    (shapely.MultiLineString([[(-1,0),(4,0)],[(0,4),(4,4)]]),shapely.MultiLineString([[(-1,0),(1,0)],[(3,0),(4,0)],[(0,4),(4,4)]])),
    (shapely.MultiPolygon([box(0,0,4,2),box(10,0,11,1)]),shapely.MultiPolygon([box(0,0,1,2),box(3,0,4,2),box(10,0,11,1)])),
    (shapely.GeometryCollection([Point(-2,0),LineString([(-1,0),(4,0)]),box(10,0,11,1)]),shapely.GeometryCollection([Point(-2,0),LineString([(-1,0),(1,0)]),LineString([(3,0),(4,0)]),box(10,0,11,1)])),
])
def test_difference_full_dimensions_native_boundary_and_empty_rows(shape,expected):
    left,right=difference_inputs([shape],[box(1,-1,3,3)])
    result=exact_difference(left,right)
    assert len(result)==(0 if expected is None else 1)
    if expected is not None:assert result.geometry.iloc[0].equals(expected)
    assert list(result.columns)==list(left.columns)
    assert result.geometry.name==left.geometry.name and result.crs==left.crs


def test_difference_source_order_nullable_types_and_business_geometry_are_not_lost():
    left,right=difference_inputs([Point(0,0),Point(2,0),Point(4,0)])
    left['a_key']=pd.Series([2**60+1,2**60+3,2**60+5],dtype='Int64')
    left['grade']=pd.Series([None,3,2**53+1],dtype='Int64')
    left['enabled']=pd.Series([None,True,False],dtype='boolean')
    left['geometry']=pd.Series(['业务几何',None,'末行'],dtype='string')
    left['category']=pd.Categorical(['b','a','c']);left.index=[8,8,-1]
    right['a_key']=['B字段不用复制'];right['geometry']=['B业务几何']
    originals=[g.copy(deep=True) for g in (left,right)]
    result=exact_difference(left,right)
    assert list(result.columns)==list(left.columns) and list(result.index)==[0,1]
    for name in left.columns:
        if name!=left.geometry.name:pd.testing.assert_series_equal(result[name],left[name].iloc[[0,2]].reset_index(drop=True))
    assert result.a_key.tolist()==[2**60+1,2**60+5]
    for frame,before in zip((left,right),originals):assert_geodataframe_equal(frame,before)


@pytest.mark.parametrize('empty',['a','b','both','covered'])
def test_difference_empty_batches_preserve_schema_and_require_admission(empty):
    left,right=difference_inputs([Point(2,0)])
    left['optional']=pd.Series([None],dtype='Int64')
    if empty in ('a','both'):left=left.iloc[:0].copy()
    if empty in ('b','both'):right=right.iloc[:0].copy()
    result=exact_difference(left,right)
    assert len(result)==(1 if empty=='b' else 0)
    assert list(result.columns)==list(left.columns) and result.dtypes.equals(left.dtypes)
    assert result.geometry.name==left.geometry.name and result.crs==left.crs
    from operators.geometric_operators import DifferenceInputError
    broken=right.copy();broken.set_crs(None,allow_override=True,inplace=True)
    with pytest.raises(DifferenceInputError) as caught:exact_difference(left,broken)
    assert caught.value.code=='crs'


def test_difference_holes_contacts_duplicate_masks_and_tiny_remainders():
    tiny=2**-40
    a=shapely.Polygon(box(0,0,8,8).exterior.coords,[box(2,2,6,6).exterior.coords])
    left,right=difference_inputs([a,box(10,0,11,1),box(20,0,21,1)],[box(3,3,4,4),box(11,0,12,1),box(20+tiny,-1,22,2)])
    result=exact_difference(left,right)
    assert len(result)==3 and result.geometry.iloc[0].equals(a) and result.geometry.iloc[1].equals(box(10,0,11,1))
    assert result.geometry.iloc[2].equals(box(20,0,20+tiny,1)) and result.geometry.iloc[2].area==tiny
    assert_geodataframe_equal(result,exact_difference(left,pd.concat([right.iloc[::-1],right],ignore_index=True)))


@pytest.mark.parametrize('side',['a','b'])
@pytest.mark.parametrize('problem',['missing','empty','invalid','z','m','nonfinite','duplicate_columns','second_geometry','not_frame'])
def test_difference_rejects_bad_geometry_without_repair(side,problem):
    from operators.geometric_operators import DifferenceInputError
    left,right=difference_inputs();frame=left if side=='a' else right
    bad={'missing':None,'empty':Polygon(),'invalid':Polygon([(0,0),(2,2),(0,2),(2,0),(0,0)]),
         'z':shapely.from_wkt('POLYGON Z ((0 0 1,1 0 1,1 1 1,0 0 1))'),
         'm':shapely.from_wkt('POLYGON M ((0 0 1,1 0 1,1 1 1,0 0 1))'),
         'nonfinite':shapely.from_wkt('POLYGON ((0 0,1 0,Infinity 1,0 0))')}
    if problem in bad:frame[frame.geometry.name]=gpd.GeoSeries([bad[problem]],crs=frame.crs)
    elif problem=='duplicate_columns':frame.columns=[frame.columns[0]]*len(frame.columns)
    elif problem=='second_geometry':frame['another']=gpd.GeoSeries([Point(0,0)],crs=frame.crs)
    elif problem=='not_frame':
        if side=='a':left=pd.DataFrame({'key':[1]})
        else:right=pd.DataFrame({'key':[1]})
    with pytest.raises(DifferenceInputError) as caught:exact_difference(left,right)
    assert caught.value.code==('source' if side=='a' else 'mask')


@pytest.mark.parametrize('mask',[Point(0,0),LineString([(0,0),(1,1)]),shapely.GeometryCollection([box(0,0,1,1)]),shapely.MultiPoint([(0,0),(1,1)])])
def test_difference_mask_is_polygon_only_even_when_a_empty(mask):
    from operators.geometric_operators import DifferenceInputError
    left,right=difference_inputs(b=[mask]);left=left.iloc[:0]
    with pytest.raises(DifferenceInputError) as caught:exact_difference(left,right)
    assert caught.value.code=='mask'


@pytest.mark.parametrize('problem',['missing','null','blank','object','duplicate','geometry'])
def test_difference_explicit_a_identity_not_index_or_fid(problem):
    from operators.geometric_operators import DifferenceInputError
    left,right=difference_inputs([Point(0,0),Point(4,0)])
    if problem=='missing':left=left.drop(columns=['a_key'])
    if problem=='null':left['a_key']=[1,None]
    if problem=='blank':left['a_key']=['a',' ']
    if problem=='object':left['a_key']=[{},{}]
    if problem=='duplicate':left['a_key']=[1,1]
    if problem=='geometry':left['a_key']=gpd.GeoSeries([Point(0,0),Point(1,1)],crs=left.crs)
    with pytest.raises(DifferenceInputError) as caught:exact_difference(left,right)
    assert caught.value.code=='source'


@pytest.mark.parametrize('side',['a','b'])
@pytest.mark.parametrize('crs',[None,'EPSG:4326','EPSG:4978','EPSG:2263'])
def test_difference_same_known_projected_crs_required(side,crs):
    from operators.geometric_operators import DifferenceInputError
    left,right=difference_inputs();(left if side=='a' else right).set_crs(crs,allow_override=True,inplace=True)
    with pytest.raises(DifferenceInputError) as caught:exact_difference(left,right)
    assert caught.value.code=='crs'


@pytest.mark.parametrize('crs',['EPSG:3857','EPSG:2263'])
def test_difference_equivalent_crs_and_native_units_and_unsigned_ids(crs):
    left,right=difference_inputs([box(0,0,4,2)]);left.set_crs(crs,allow_override=True,inplace=True)
    right.set_crs(left.crs.to_wkt(),allow_override=True,inplace=True);left['a_key']=pd.Series([2**64-1],dtype='UInt64')
    result=exact_difference(left,right)
    assert result.crs==left.crs and result.a_key.dtype==left.a_key.dtype and result.a_key.iloc[0]==2**64-1
    assert result.geometry.iloc[0].area==4


@pytest.mark.parametrize('language',['zh-cn','en'])
@pytest.mark.parametrize('problem,reason,param',[('source','operator_input_invalid','input_gdf'),('mask','operator_mask_invalid','gdf_b'),('crs','operator_crs_invalid','')])
def test_difference_typed_diagnostic_bound_to_definition_and_private_free(language,problem,reason,param):
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime import WorkflowExecutionError
    left,right=difference_inputs()
    if problem=='source':left['a_key']=['']
    if problem=='mask':right[right.geometry.name]=gpd.GeoSeries([Point(0,0)],crs=right.crs)
    if problem=='crs':right.set_crs(None,allow_override=True,inplace=True)
    task={'id':'PRIVATE_SOURCE_NODE','operator':'difference','depends_on':[],'params':{'input_gdf':left,'gdf_b':right,'id_field':'a_key'}}
    with pytest.raises(WorkflowExecutionError) as caught:GeoPythonWorkflowRunner(language=language).execute({'tasks':[task]})
    assert caught.value.error_code=='INVALID_PARAMS'
    assert caught.value.diagnostic=={'schema_version':'addp.execution-failure/v3','stage':'execution','reason':reason,'operator':{'node_index':1,'operator_id':'difference',**({'parameter':param} if param else {})}}
    assert 'PRIVATE' not in json.dumps(caught.value.diagnostic)
    assert ('Difference' in str(caught.value)) if language=='en' else ('几何差集' in str(caught.value))


@pytest.mark.parametrize('stage',['union','difference'])
@pytest.mark.parametrize('bad',['exception','invalid','nonfinite','z','m'])
def test_difference_native_calculation_failures_are_not_input_diagnostics(monkeypatch,stage,bad):
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime import WorkflowExecutionError
    def compute(*args,**kwargs):
        if bad=='exception':raise shapely.errors.GEOSException('PRIVATE original source')
        if bad in ('z','m'):return shapely.from_wkt('POLYGON '+bad.upper()+' ((0 0 1,1 0 1,1 1 1,0 0 1))')
        return Polygon([(0,0),(2,2),(0,2),(2,0),(0,0)]) if bad=='invalid' else shapely.from_wkt('LINESTRING (0 0,Infinity 1)')
    monkeypatch.setattr(shapely,'union_all' if stage=='union' else 'difference',compute)
    left,right=difference_inputs()
    with pytest.raises(WorkflowExecutionError) as caught:GeoPythonWorkflowRunner().execute({'tasks':[{'id':'difference','operator':'difference','params':{'input_gdf':left,'gdf_b':right,'id_field':'a_key'},'depends_on':[]}]})
    assert caught.value.error_code=='EXECUTION_FAILED' and caught.value.diagnostic is None


def test_difference_never_calls_old_overlay_or_automatic_repair(monkeypatch):
    def forbidden(*args,**kwargs):raise AssertionError('old or repairing path')
    monkeypatch.setattr(gpd,'overlay',forbidden);monkeypatch.setattr(shapely,'make_valid',forbidden)
    left,right=difference_inputs();assert len(exact_difference(left,right))==1


def test_difference_real_gpkg_save_reload_with_typed_fields_and_fail_before_save(tmp_path):
    from osgeo import ogr
    left,right=difference_inputs([box(0,0,4,2),Point(2,0),Point(10,0),LineString([(20,0),(24,0)])],
                               [box(1,-1,3,3),box(21,-1,23,1)])
    left['a_key']=pd.Series([2**60+1,2**60+3,2**60+5,2**60+7],dtype='Int64')
    left['grade']=pd.Series([None,1,2**53+1,-2],dtype='Int64');left['enabled']=pd.Series([None,True,False,True],dtype='boolean')
    left['geometry']=pd.Series(['业务几何','被扣除',None,'末行'],dtype='string')
    right['a_key']=['无需来源一','无需来源二'];right=right.drop(columns=['b_key'])
    for name,gdf in [('a',left),('b',right)]:gdf.to_file(tmp_path/(name+'.gpkg'),driver='GPKG',index=False)
    originals={name:(tmp_path/(name+'.gpkg')).read_bytes() for name in ('a','b')};conn={'engine_type':'nfs','mount_path':str(tmp_path)}
    tasks=[{'id':'PRIVATE_DIFF','operator':'difference','depends_on':['a','b'],'params':{'input_gdf':{'$ref':'a'},'gdf_b':{'$ref':'b'},'id_field':'a_key'}}]
    tasks.extend({'id':name,'operator':'load','depends_on':[],'params':{'connection_info':conn,'path':name+'.gpkg'}} for name in ('a','b'))
    tasks.append({'id':'save','operator':'save','depends_on':['PRIVATE_DIFF'],'params':{'input_df':{'$ref':'PRIVATE_DIFF'},'connection_info':conn,'path':'result.gpkg','mode':'fail'}})
    state=execute_workflow({'tasks':tasks});assert state['status']=='success',state
    expected=[shapely.MultiPolygon([box(0,0,1,2),box(3,0,4,2)]),Point(10,0),shapely.MultiLineString([[(20,0),(21,0)],[(23,0),(24,0)]])]
    ds=ogr.Open(str(tmp_path/'result.gpkg'));layer=ds.GetLayer(0);rows=list(layer)
    assert len(rows)==3 and layer.GetSpatialRef().GetAuthorityCode(None)=='3857'
    assert [f.name for f in layer.schema]==['a_key','grade','enabled','geometry']
    assert layer.GetLayerDefn().GetFieldDefn(0).GetType()==ogr.OFTInteger64
    assert layer.GetLayerDefn().GetFieldDefn(2).GetSubType()==ogr.OFSTBoolean
    for i,(row,geometry) in enumerate(zip(rows,expected)):
        assert row.GetGeometryRef().Normalize().ExportToIsoWkb()==ogr.CreateGeometryFromWkt(geometry.wkt).Normalize().ExportToIsoWkb()
        assert row.GetField('a_key')==[2**60+1,2**60+5,2**60+7][i]
        assert row.GetField('grade')==[None,2**53+1,-2][i]
        assert row.GetField('enabled')==[None,False,True][i]
        assert row.GetField('geometry')==['业务几何',None,'末行'][i]
    name=layer.GetGeometryColumn();row=rows=layer=ds=None
    loaded=get_operator('load')(connection_info=conn,path='result.gpkg')
    assert loaded.geometry.name==name and loaded.grade.dtype=='Int64' and loaded.enabled.dtype=='boolean'
    assert all(g.equals(e) for g,e in zip(loaded.geometry,expected))
    tasks[0]['params']['id_field']='PRIVATE_MISSING';tasks[-1]['params']['path']='forbidden.gpkg'
    failed=execute_workflow({'tasks':tasks});assert failed['status']=='failed' and failed['error_code']=='INVALID_PARAMS',failed
    assert failed['diagnostic']['operator']=={'node_index':1,'operator_id':'difference','parameter':'input_gdf'}
    assert not (tmp_path/'forbidden.gpkg').exists()
    for name,data in originals.items():assert (tmp_path/(name+'.gpkg')).read_bytes()==data


@pytest.mark.parametrize('problem',[None,'source','mask','crs'])
def test_difference_async_api_discovery_and_frozen_input_diagnosis(tmp_path,monkeypatch,problem):
    import api_server
    from addp_common.workflow_runtime import ExecutionRegistry
    left,right=difference_inputs([box(0,0,4,2)])
    if problem=='source':left['a_key']=['']
    if problem=='mask':right[right.geometry.name]=gpd.GeoSeries([Point(0,0)],crs=right.crs)
    if problem=='crs':right.set_crs('EPSG:3395',allow_override=True,inplace=True)
    for name,frame in [('a',left),('b',right)]:frame.to_file(tmp_path/(name+'.gpkg'),driver='GPKG',index=False)
    conn={'engine_type':'nfs','mount_path':str(tmp_path)}
    tasks=[{'id':'PRIVATE_DIFF_NODE','operator':'difference','depends_on':['a','b'],'params':{'input_gdf':{'$ref':'a'},'gdf_b':{'$ref':'b'},'id_field':'a_key'}}]
    tasks.extend({'id':name,'operator':'load','depends_on':[],'params':{'connection_info':conn,'path':name+'.gpkg'}} for name in ('a','b'))
    monkeypatch.setattr(api_server,'executions',ExecutionRegistry())
    with api_server.app.test_client() as client:
        spec=next(v for v in client.get('/api/operators').get_json()['operators'] if v['id']=='difference')
        assert spec['display_name']=='几何差集' and spec['category_path']==['矢量处理','叠加分析']
        assert [(p['name'],p['required']) for p in spec['parameters']]==[('input_gdf',True),('gdf_b',True),('id_field',True)]
        assert client.post('/api/operators/difference/invoke',json={'params':{}}).status_code==403
        response=client.post('/api/workflow',json={'workflow_def':{'tasks':tasks},'runtime':{'tenant_id':7,'execution_authorization':{'id':1,'effects':['read']}}})
        assert response.status_code==202
        deadline=time.monotonic()+5
        while time.monotonic()<deadline:
            observed=client.get('/api/executions/'+response.get_json()['execution_id']);assert observed.status_code==200
            state=observed.get_json()
            if state['status'] in ('success','failed'):break
            time.sleep(.01)
        if problem is None:
            assert state['status']=='success',state
            assert len(json.loads(state['result'])['features'])==1
        else:
            reason,param={'source':('operator_input_invalid','input_gdf'),'mask':('operator_mask_invalid','gdf_b'),'crs':('operator_crs_invalid','')}[problem]
            assert state['status']=='failed' and state['error_code']=='INVALID_PARAMS',state
            assert state['diagnostic']=={'schema_version':'addp.execution-failure/v3','stage':'execution','reason':reason,'operator':{'node_index':1,'operator_id':'difference',**({'parameter':param} if param else {})}}
            assert 'PRIVATE' not in json.dumps(state['diagnostic'])


def exact_clip(a,b):
    return get_operator('clip')(a,b,id_field='a_key')


def test_vector_clip_whole_mask_one_source_row_retains_lower_dimension_contacts():
    left,right=overlay_inputs([box(0,0,4,1),box(10,0,11,1),box(20,0,21,1)],
                             [box(1,-1,2.5,2),box(2,-1,3,2),box(11,0,12,1),box(21,1,22,2)])
    result=exact_clip(left,right)
    expected=[box(1,0,3,1),LineString([(11,0),(11,1)]),Point(21,1)]
    assert len(result)==3 and all(g.equals(e) for g,e in zip(result.geometry,expected))
    assert list(result.columns)==list(left.columns) and result.a_key.tolist()==[1,2,3]

@pytest.mark.parametrize('shape,expected',[
    (Point(0,0),Point(0,0)),(Point(2,0),None),(Point(1,0),Point(1,0)),
    (LineString([(-2,0),(2,0)]),LineString([(-1,0),(1,0)])),
    (box(-2,-2,2,2),box(-1,-1,1,1)),
    (shapely.MultiPoint([(0,0),(1,0),(3,0)]),shapely.MultiPoint([(0,0),(1,0)])),
    (shapely.MultiLineString([[(-2,0),(2,0)],[(-2,.5),(2,.5)]]),shapely.MultiLineString([[(-1,0),(1,0)],[(-1,.5),(1,.5)]])),
    (shapely.MultiPolygon([box(-2,-.8,-.5,.8),box(.5,-.8,2,.8)]),shapely.MultiPolygon([box(-1,-.8,-.5,.8),box(.5,-.8,1,.8)])),
])
def test_vector_clip_full_source_dimensions_and_closed_boundary(shape,expected):
    left,right=overlay_inputs([shape],[box(-1,-1,1,1)]);result=exact_clip(left,right)
    assert len(result)==(0 if expected is None else 1)
    if expected is not None:assert result.geometry.iloc[0].equals(expected)
    assert list(result.columns)==list(left.columns) and result.geometry.name==left.geometry.name and result.crs==left.crs


def test_vector_clip_collection_input_and_mixed_dimension_collection_result_are_one_row():
    a=shapely.GeometryCollection([Point(0,0),LineString([(5,-2),(5,2)]),box(10,0,12,2)])
    b=shapely.MultiPolygon([box(-1,-1,1,1),box(4,-1,6,1),box(11,1,13,3)])
    left,right=overlay_inputs([a],[b]);result=exact_clip(left,right)
    expected=shapely.GeometryCollection([Point(0,0),LineString([(5,-1),(5,1)]),box(11,1,12,2)])
    assert len(result)==1 and result.geometry.iloc[0].equals(expected)
    a=shapely.MultiPolygon([box(0,0,2,2),box(4,0,5,1),box(7,0,8,1)])
    b=shapely.MultiPolygon([box(1,1,3,3),box(5,0,6,1),box(8,1,9,2)])
    left,right=overlay_inputs([a],[b]);result=exact_clip(left,right)
    assert len(result)==1 and result.geometry.iloc[0].equals(shapely.GeometryCollection([box(1,1,2,2),LineString([(5,0),(5,1)]),Point(8,1)]))


def test_vector_clip_mask_holes_hole_boundary_and_tiny_exact_intersection():
    mask=shapely.Polygon(box(0,0,8,8).exterior.coords,[box(2,2,6,6).exterior.coords]);tiny=2**-40
    left,right=overlay_inputs([Point(0,0),Point(2,3),Point(4,4),box(3,3,5,5),mask,box(20,0,21,1)],
                             [mask,box(20,-1,20+tiny,2)])
    result=exact_clip(left,right)
    assert result.a_key.tolist()==[1,2,5,6]
    assert result.geometry.iloc[0].equals(Point(0,0)) and result.geometry.iloc[1].equals(Point(2,3))
    assert result.geometry.iloc[2].equals(mask) and result.geometry.iloc[3].equals(box(20,0,20+tiny,1))
    assert result.geometry.iloc[3].area==tiny
    assert_geodataframe_equal(result,exact_clip(left,pd.concat([right.iloc[::-1],right],ignore_index=True)))


def test_vector_clip_preserves_source_fields_types_order_and_b_does_not_supply_attributes():
    left,right=overlay_inputs([Point(0,0),Point(3,0),Point(.5,0)],[box(-1,-1,1,1)])
    left['a_key']=pd.Series([2**60+1,2**60+3,2**60+5],dtype='Int64')
    left['grade']=pd.Series([None,3,2**53+1],dtype='Int64');left['enabled']=pd.Series([None,True,False],dtype='boolean')
    left['geometry']=pd.Series(['普通业务',None,'末行'],dtype='string');left['category']=pd.Categorical(['b','a','c']);left.index=[9,9,-1]
    right=right.drop(columns=['b_key']);right['a_key']=['不复制'];right['geometry']=['掩膜业务']
    original=[f.copy(deep=True) for f in (left,right)];result=exact_clip(left,right)
    assert list(result.columns)==list(left.columns) and list(result.index)==[0,1]
    for name in left.columns:
        if name!=left.geometry.name:pd.testing.assert_series_equal(result[name],left[name].iloc[[0,2]].reset_index(drop=True))
    for frame,before in zip((left,right),original):assert_geodataframe_equal(frame,before)


@pytest.mark.parametrize('empty',['a','b','both','disjoint'])
def test_vector_clip_empty_source_or_mask_keeps_empty_schema_and_admission(empty):
    left,right=overlay_inputs([Point(30,0)],[box(-1,-1,1,1)]);left['grade']=pd.Series([None],dtype='Int64')
    if empty in ('a','both'):left=left.iloc[:0].copy()
    if empty in ('b','both'):right=right.iloc[:0].copy()
    result=exact_clip(left,right)
    assert len(result)==0 and result.dtypes.equals(left.dtypes) and list(result.columns)==list(left.columns)
    assert result.geometry.name==left.geometry.name and result.crs==left.crs
    from operators.data_operations import ClipInputError
    right=right.set_crs(None,allow_override=True)
    with pytest.raises(ClipInputError) as caught:exact_clip(left,right)
    assert caught.value.code=='crs'


def test_vector_clip_empty_mask_and_difference_empty_mask_have_distinct_semantics():
    left,right=overlay_inputs();right=right.iloc[:0]
    assert exact_clip(left,right).empty
    assert_geodataframe_equal(exact_difference(left,right),left.reset_index(drop=True))


@pytest.mark.parametrize('problem',['missing_id','null_id','duplicate_id','object_id','source_z','source_empty','source_invalid','source_nonfinite','second_geometry','mask_point','mask_gc','mask_invalid','mask_missing','mask_z','mask_m','crs_missing','crs_mismatch','crs_geographic'])
def test_vector_clip_typed_admission_rejects_bad_geometry_identity_masks_and_crs(problem):
    from operators.data_operations import ClipInputError
    left,right=overlay_inputs([box(0,0,2,2),box(3,0,5,2)],[box(-1,-1,6,3)])
    expected='source'
    if problem=='missing_id':left=left.drop(columns=['a_key'])
    elif problem=='null_id':left['a_key']=[None,2]
    elif problem=='duplicate_id':left['a_key']=[1,1]
    elif problem=='object_id':left['a_key']=[{},{}]
    elif problem.startswith('source_'):
        shapes={'z':shapely.from_wkt('POINT Z (0 0 1)'),'empty':Point(),'invalid':Polygon([(0,0),(2,2),(0,2),(2,0),(0,0)]),'nonfinite':shapely.from_wkt('LINESTRING (0 0,Infinity 1)')}
        left[left.geometry.name]=gpd.GeoSeries([shapes[problem[7:]],Point(0,0)],crs=left.crs)
    elif problem=='second_geometry':left['other']=gpd.GeoSeries([Point(0,0),Point(1,1)],crs=left.crs)
    elif problem.startswith('mask_'):
        expected='mask';shapes={'point':Point(0,0),'gc':shapely.GeometryCollection([box(0,0,1,1)]),'invalid':Polygon([(0,0),(2,2),(0,2),(2,0),(0,0)]),'missing':None,'z':shapely.from_wkt('POLYGON Z ((0 0 1,1 0 1,1 1 1,0 0 1))'),'m':shapely.from_wkt('POLYGON M ((0 0 1,1 0 1,1 1 1,0 0 1))')}
        right[right.geometry.name]=gpd.GeoSeries([shapes[problem[5:]]],crs=right.crs)
    else:
        expected='crs';right.set_crs({'crs_missing':None,'crs_mismatch':'EPSG:2263','crs_geographic':'EPSG:4326'}[problem],allow_override=True,inplace=True)
    with pytest.raises(ClipInputError) as caught:exact_clip(left,right)
    assert caught.value.code==expected


@pytest.mark.parametrize('language',['zh-cn','en'])
@pytest.mark.parametrize('problem,reason,parameter',[('source','operator_input_invalid','input_gdf'),('mask','operator_mask_invalid','mask_gdf'),('crs','operator_crs_invalid','')])
def test_vector_clip_closed_diagnostic_has_actual_operator_and_no_source_values(language,problem,reason,parameter):
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime import WorkflowExecutionError
    left,right=overlay_inputs()
    if problem=='source':left['a_key']=['']
    if problem=='mask':right[right.geometry.name]=gpd.GeoSeries([Point(0,0)],crs=right.crs)
    if problem=='crs':right.set_crs(None,allow_override=True,inplace=True)
    tasks=[{'id':'PRIVATE_CLIP_NODE','operator':'clip','depends_on':[],'params':{'input_gdf':left,'mask_gdf':right,'id_field':'a_key'}}]
    with pytest.raises(WorkflowExecutionError) as caught:GeoPythonWorkflowRunner(language=language).execute({'tasks':tasks})
    assert caught.value.error_code=='INVALID_PARAMS'
    assert caught.value.diagnostic=={'schema_version':'addp.execution-failure/v3','stage':'execution','reason':reason,'operator':{'node_index':1,'operator_id':'clip',**({'parameter':parameter} if parameter else {})}}
    assert 'PRIVATE' not in json.dumps(caught.value.diagnostic)
    assert ('Clip' in str(caught.value)) if language=='en' else ('裁剪' in str(caught.value))


@pytest.mark.parametrize('stage',['union_all','intersection'])
@pytest.mark.parametrize('problem',['native','invalid','nonfinite','z','m'])
def test_vector_clip_calculation_faults_never_masquerade_as_input_errors(monkeypatch,stage,problem):
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime import WorkflowExecutionError
    def bad(*args,**kwargs):
        if problem=='native':raise shapely.errors.GEOSException('PRIVATE source values')
        if problem=='invalid':return Polygon([(0,0),(2,2),(0,2),(2,0),(0,0)])
        if problem=='nonfinite':return shapely.from_wkt('LINESTRING (0 0,Infinity 1)')
        return shapely.from_wkt('POLYGON '+problem.upper()+' ((0 0 1,1 0 1,1 1 1,0 0 1))')
    monkeypatch.setattr(shapely,stage,bad);left,right=overlay_inputs()
    with pytest.raises(WorkflowExecutionError) as caught:GeoPythonWorkflowRunner().execute({'tasks':[{'id':'clip','operator':'clip','depends_on':[],'params':{'input_gdf':left,'mask_gdf':right,'id_field':'a_key'}}]})
    assert caught.value.error_code=='EXECUTION_FAILED' and caught.value.diagnostic is None


def test_vector_clip_never_calls_old_clip_or_automatic_repair(monkeypatch):
    def forbidden(*a,**k):raise AssertionError('old fast clip or repair path')
    monkeypatch.setattr(gpd,'clip',forbidden);monkeypatch.setattr(shapely,'make_valid',forbidden)
    left,right=overlay_inputs();assert len(exact_clip(left,right))==1


@pytest.mark.parametrize('crs',['EPSG:3857','EPSG:2263'])
def test_vector_clip_equivalent_projected_crs_preserves_units_and_uint64_identity(crs):
    left,right=overlay_inputs();left.set_crs(crs,allow_override=True,inplace=True);right.set_crs(left.crs.to_wkt(),allow_override=True,inplace=True);left['a_key']=pd.Series([2**64-1],dtype='UInt64')
    result=exact_clip(left,right);assert result.crs==left.crs and result.a_key.dtype==left.a_key.dtype and result.a_key.iloc[0]==2**64-1
    assert result.geometry.iloc[0].area==1


def test_vector_clip_gpkg_mixed_save_reload_preserves_typed_fields_and_failure_guard(tmp_path):
    from osgeo import ogr
    a=shapely.MultiPolygon([box(0,0,2,2),box(4,0,5,1),box(7,0,8,1)])
    b=shapely.MultiPolygon([box(1,1,3,3),box(5,0,6,1),box(8,1,9,2)])
    left,right=overlay_inputs([a,box(10,0,11,1),Point(20,0),LineString([(30,0),(32,0)]),Point(40,0)],
                             [b,box(11,0,12,1),box(19,-1,21,1),box(31,-1,33,1)])
    left['a_key']=pd.Series([2**60+1+2*i for i in range(5)],dtype='Int64')
    left['grade']=pd.Series([None,2**53+1,0,-1,8],dtype='Int64');left['enabled']=pd.Series([None,False,True,False,True],dtype='boolean')
    left['geometry']=pd.Series(['普通业务',None,'点','线','未命中'],dtype='string')
    right=right.drop(columns=['b_key']);right['a_key']=['仅范围']*4
    for name,frame in [('a',left),('b',right)]:frame.to_file(tmp_path/(name+'.gpkg'),driver='GPKG',index=False)
    originals={name:(tmp_path/(name+'.gpkg')).read_bytes() for name in ('a','b')};conn={'engine_type':'nfs','mount_path':str(tmp_path)}
    tasks=[{'id':'PRIVATE_CLIP','operator':'clip','depends_on':['a','b'],'params':{'input_gdf':{'$ref':'a'},'mask_gdf':{'$ref':'b'},'id_field':'a_key'}}]
    tasks.extend({'id':name,'operator':'load','depends_on':[],'params':{'connection_info':conn,'path':name+'.gpkg'}} for name in ('a','b'))
    tasks.append({'id':'save','operator':'save','depends_on':['PRIVATE_CLIP'],'params':{'input_df':{'$ref':'PRIVATE_CLIP'},'connection_info':conn,'path':'result.gpkg','mode':'fail'}})
    state=execute_workflow({'tasks':tasks});assert state['status']=='success',state
    expected=[shapely.GeometryCollection([box(1,1,2,2),LineString([(5,0),(5,1)]),Point(8,1)]),LineString([(11,0),(11,1)]),Point(20,0),LineString([(31,0),(32,0)])]
    ds=ogr.Open(str(tmp_path/'result.gpkg'));layer=ds.GetLayer(0);rows=list(layer)
    assert len(rows)==4 and layer.GetSpatialRef().GetAuthorityCode(None)=='3857'
    assert [f.name for f in layer.schema]==['a_key','grade','enabled','geometry']
    assert layer.GetLayerDefn().GetFieldDefn(0).GetType()==ogr.OFTInteger64 and layer.GetLayerDefn().GetFieldDefn(2).GetSubType()==ogr.OFSTBoolean
    for i,(row,shape) in enumerate(zip(rows,expected)):
        assert row.GetGeometryRef().Normalize().ExportToIsoWkb()==ogr.CreateGeometryFromWkt(shape.wkt).Normalize().ExportToIsoWkb()
        assert row.GetField('a_key')==2**60+1+2*i and row.GetField('grade')==[None,2**53+1,0,-1][i]
        assert row.GetField('enabled')==[None,False,True,False][i] and row.GetField('geometry')==['普通业务',None,'点','线'][i]
    column=layer.GetGeometryColumn();row=rows=layer=ds=None
    loaded=get_operator('load')(connection_info=conn,path='result.gpkg');assert loaded.geometry.name==column and loaded.grade.dtype=='Int64' and loaded.enabled.dtype=='boolean'
    assert all(g.equals(e) for g,e in zip(loaded.geometry,expected))
    tasks[0]['params']['id_field']='PRIVATE_MISSING';tasks[-1]['params']['path']='forbidden.gpkg';state=execute_workflow({'tasks':tasks})
    assert state['status']=='failed' and state['error_code']=='INVALID_PARAMS' and not (tmp_path/'forbidden.gpkg').exists(),state
    for name,data in originals.items():assert (tmp_path/(name+'.gpkg')).read_bytes()==data

@pytest.mark.parametrize('problem',[None,'source','mask','crs'])
def test_vector_clip_async_api_discovery_and_frozen_input_diagnosis(tmp_path,monkeypatch,problem):
    import api_server
    from addp_common.workflow_runtime import ExecutionRegistry
    left,right=difference_inputs([box(0,0,4,2)])
    if problem=='source':left['a_key']=['']
    if problem=='mask':right[right.geometry.name]=gpd.GeoSeries([Point(0,0)],crs=right.crs)
    if problem=='crs':right.set_crs('EPSG:3395',allow_override=True,inplace=True)
    for name,frame in [('a',left),('b',right)]:frame.to_file(tmp_path/(name+'.gpkg'),driver='GPKG',index=False)
    conn={'engine_type':'nfs','mount_path':str(tmp_path)}
    tasks=[{'id':'PRIVATE_DIFF_NODE','operator':'clip','depends_on':['a','b'],'params':{'input_gdf':{'$ref':'a'},'mask_gdf':{'$ref':'b'},'id_field':'a_key'}}]
    tasks.extend({'id':name,'operator':'load','depends_on':[],'params':{'connection_info':conn,'path':name+'.gpkg'}} for name in ('a','b'))
    monkeypatch.setattr(api_server,'executions',ExecutionRegistry())
    with api_server.app.test_client() as client:
        spec=next(v for v in client.get('/api/operators').get_json()['operators'] if v['id']=='clip')
        assert spec['display_name']=='裁剪几何' and spec['category_path']==['矢量处理','叠加分析']
        assert [(p['name'],p['required']) for p in spec['parameters']]==[('input_gdf',True),('mask_gdf',True),('id_field',True)]
        assert client.post('/api/operators/clip/invoke',json={'params':{}}).status_code==403
        response=client.post('/api/workflow',json={'workflow_def':{'tasks':tasks},'runtime':{'tenant_id':7,'execution_authorization':{'id':1,'effects':['read']}}})
        assert response.status_code==202
        deadline=time.monotonic()+5
        while time.monotonic()<deadline:
            observed=client.get('/api/executions/'+response.get_json()['execution_id']);assert observed.status_code==200
            state=observed.get_json()
            if state['status'] in ('success','failed'):break
            time.sleep(.01)
        if problem is None:
            assert state['status']=='success',state
            assert len(json.loads(state['result'])['features'])==1
        else:
            reason,param={'source':('operator_input_invalid','input_gdf'),'mask':('operator_mask_invalid','mask_gdf'),'crs':('operator_crs_invalid','')}[problem]
            assert state['status']=='failed' and state['error_code']=='INVALID_PARAMS',state
            assert state['diagnostic']=={'schema_version':'addp.execution-failure/v3','stage':'execution','reason':reason,'operator':{'node_index':1,'operator_id':'clip',**({'parameter':param} if param else {})}}
            assert 'PRIVATE' not in json.dumps(state['diagnostic'])


def exact_union(a,b):
    return get_operator('union')(a,b,id_field='a_key',reference_id_field='b_key')


def test_vector_union_three_regions_keep_both_and_nullable_one_sided_sources():
    a,b=overlay_inputs([box(0,0,4,2)],[box(2,0,6,2)])
    result=exact_union(a,b)
    assert len(result)==3 and result.geometry.name=='source_shape' and result.crs==a.crs
    assert all(g.equals(e)for g,e in zip(result.geometry,[box(2,0,4,2),box(0,0,2,2),box(4,0,6,2)]))
    assert result.source_id_a.tolist()[:2]==[1,1] and pd.isna(result.source_id_a.iloc[2])
    assert result.source_id_b.iloc[[0,2]].tolist()==['11','11'] and pd.isna(result.source_id_b.iloc[1])
    assert result.source_id_a.dtype==a.a_key.dtype and result.source_id_b.dtype==b.b_key.dtype


@pytest.mark.parametrize('kind',['equal','inside','disjoint','edge','point'])
def test_vector_union_partition_containment_duplicates_across_layers_and_contacts(kind):
    b={'equal':box(0,0,4,4),'inside':box(1,1,2,2),'disjoint':box(5,0,6,1),'edge':box(4,0,5,1),'point':box(4,4,5,5)}[kind]
    a,b=overlay_inputs([box(0,0,4,4)],[b]);result=exact_union(a,b)
    assert len(result)=={'equal':1,'inside':2,'disjoint':2,'edge':2,'point':2}[kind]
    assert result.geometry.geom_type.isin(['Polygon','MultiPolygon']).all()
    if kind=='equal':assert result.geometry.iloc[0].equals(a.geometry.iloc[0])
    elif kind=='inside':assert result.geometry.iloc[0].equals(box(1,1,2,2)) and result.geometry.iloc[1].equals(Polygon(box(0,0,4,4).exterior.coords,[box(1,1,2,2).exterior.coords]))
    else:assert result.geometry.iloc[0].equals(a.geometry.iloc[0]) and result.geometry.iloc[1].equals(b.geometry.iloc[0])


def test_vector_union_whole_opposite_layer_masks_row_order_and_multipart_per_source():
    a,b=overlay_inputs([box(0,0,8,2),box(20,0,21,1)],[box(5,-1,6,3),box(2,-1,3,3)])
    result=exact_union(a,b)
    assert len(result)==6 and list(result.index)==list(range(6))
    assert result.source_id_a.iloc[:4].tolist()==[1,1,1,2]
    assert result.source_id_b.iloc[:2].tolist()==['11','12']
    assert result.geometry.iloc[0].equals(box(5,0,6,2)) and result.geometry.iloc[1].equals(box(2,0,3,2))
    assert result.geometry.iloc[2].equals(shapely.MultiPolygon([box(0,0,2,2),box(3,0,5,2),box(6,0,8,2)]))
    assert result.geometry.iloc[4].equals(shapely.MultiPolygon([box(5,-1,6,0),box(5,2,6,3)]))
    assert result.geometry.iloc[5].equals(shapely.MultiPolygon([box(2,-1,3,0),box(2,2,3,3)]))


def test_vector_union_extracts_only_polygon_members_from_mixed_intersection():
    a,b=overlay_inputs([shapely.MultiPolygon([box(0,0,2,2),box(4,0,5,1),box(7,0,8,1)])],
                      [shapely.MultiPolygon([box(1,1,3,3),box(5,0,6,1),box(8,1,9,2)])])
    result=exact_union(a,b)
    expected=[box(1,1,2,2),shapely.MultiPolygon([Polygon([(0,0),(2,0),(2,1),(1,1),(1,2),(0,2),(0,0)]),box(4,0,5,1),box(7,0,8,1)]),shapely.MultiPolygon([Polygon([(1,2),(2,2),(2,1),(3,1),(3,3),(1,3),(1,2)]),box(5,0,6,1),box(8,1,9,2)])]
    assert len(result)==3 and all(g.equals(e)for g,e in zip(result.geometry,expected))


def test_vector_union_holes_and_tiny_positive_areas_have_no_threshold():
    donut=Polygon(box(0,0,8,8).exterior.coords,[box(2,2,6,6).exterior.coords])
    a,b=overlay_inputs([donut],[box(3,3,5,5)]);result=exact_union(a,b)
    assert len(result)==2 and result.geometry.iloc[0].equals(donut) and result.geometry.iloc[1].equals(box(3,3,5,5))
    tiny=2**-40;a,b=overlay_inputs([box(0,0,1,1)],[box(1-tiny,0,2,1)]);result=exact_union(a,b)
    expected=[box(1-tiny,0,1,1),box(0,0,1-tiny,1),box(1,0,2,1)]
    assert all(g.equals(e)for g,e in zip(result.geometry,expected)) and result.geometry.iloc[0].area==tiny


@pytest.mark.parametrize('empty',['a','b','both'])
def test_vector_union_empty_batches_keep_complete_nullable_schema(empty):
    a,b=overlay_inputs();a['small']=pd.Series([1],dtype='int16');b['enabled']=pd.Series([True],dtype='bool')
    if empty in ('a','both'):a=a.iloc[:0].copy()
    if empty in ('b','both'):b=b.iloc[:0].copy()
    result=exact_union(a,b)
    assert len(result)==(0 if empty=='both'else 1)
    assert list(result.columns)==list(a.columns)+['b_key','enabled','source_id_a','source_id_b']
    assert str(result.small.dtype)=='Int16' and str(result.enabled.dtype)=='boolean'
    assert result.geometry.name==a.geometry.name and result.crs==a.crs and str(result.a_key.dtype)=='Int64'
    if empty=='a':assert result.a_key.isna().all()and result.source_id_a.isna().all()
    if empty=='b':assert result.b_key.isna().all()and result.source_id_b.isna().all()


@pytest.mark.parametrize('crs',['EPSG:3857','EPSG:2263'])
def test_vector_union_preserves_wide_integer_boolean_category_date_and_business_geometry(crs):
    a,b=overlay_inputs([box(0,0,4,2)],[box(2,0,6,2)])
    a.set_crs(crs,allow_override=True,inplace=True);b.set_crs(a.crs.to_wkt(),allow_override=True,inplace=True)
    a['a_key']=pd.Series([2**64-1],dtype='uint64');b['b_key']=pd.Series([2**60+1],dtype='int64')
    a['grade']=pd.Series([None],dtype='Int32');a['geometry']=pd.Series(['业务几何同名列'],dtype='string')
    a['category']=pd.Series(pd.Categorical(['x'],categories=['x','y']));b['flag']=pd.Series([True],dtype='bool');b['date']=pd.to_datetime(['2025-01-01'],utc=True)
    a.index=[7];b.index=[-1];original=[a.copy(deep=True),b.copy(deep=True)];result=exact_union(a,b)
    assert result.source_id_a.iloc[:2].tolist()==[2**64-1]*2 and str(result.source_id_a.dtype)=='UInt64'
    assert result.source_id_b.iloc[[0,2]].tolist()==[2**60+1]*2 and str(result.source_id_b.dtype)=='Int64'
    assert str(result.flag.dtype)=='boolean' and result.flag.iloc[[0,2]].tolist()==[True,True] and pd.isna(result.flag.iloc[1])
    assert result.category.dtype==a.category.dtype and result.date.dtype==b.date.dtype and result.grade.dtype==a.grade.dtype
    assert result['geometry'].iloc[:2].tolist()==['业务几何同名列']*2 and pd.isna(result['geometry'].iloc[2])
    for frame,before in zip((a,b),original):assert_geodataframe_equal(frame,before)


@pytest.mark.parametrize('side',['a','b'])
@pytest.mark.parametrize('kind',['overlap','contains','duplicate','tiny'])
@pytest.mark.parametrize('other_empty',[False,True])
def test_vector_union_rejects_intralayer_overlap_before_output_even_with_empty_other(side,kind,other_empty):
    from operators.geometric_operators import UnionInputError
    shape={'overlap':box(1,0,3,2),'contains':box(.5,.5,1.5,1.5),'duplicate':box(0,0,2,2),'tiny':box(2-2**-40,0,3,2)}[kind]
    a,b=overlay_inputs([box(10,0,12,2)]if side=='b'else[box(0,0,2,2),shape], [box(10,0,12,2)]if side=='a'else[box(0,0,2,2),shape])
    if other_empty:
        if side=='a':b=b.iloc[:0]
        else:a=a.iloc[:0]
    with pytest.raises(UnionInputError)as caught:exact_union(a,b)
    assert caught.value.code==('source_overlap'if side=='a'else'reference_overlap')


@pytest.mark.parametrize('side',['a','b'])
@pytest.mark.parametrize('problem',['id_missing','id_duplicate','id_null','id_object','point','gc','empty','missing','invalid','z','m','nonfinite','extra_geometry','duplicate_field'])
def test_vector_union_rejects_invalid_source_and_reference(side,problem):
    from operators.geometric_operators import UnionInputError
    a,b=overlay_inputs([box(0,0,1,1),box(2,0,3,1)],[box(5,0,6,1),box(7,0,8,1)]);frame=a if side=='a'else b;key='a_key'if side=='a'else'b_key'
    if problem=='id_missing':frame.drop(columns=[key],inplace=True)
    elif problem=='id_duplicate':frame[key]=[1,1]
    elif problem=='id_null':frame[key]=[1,None]
    elif problem=='id_object':frame[key]=[{},{}]
    elif problem=='extra_geometry':frame['other']=gpd.GeoSeries([Point(0,0),Point(1,1)],crs=frame.crs)
    elif problem=='duplicate_field':frame.columns=[frame.geometry.name,frame.geometry.name]
    else:
        shapes={'point':Point(0,0),'gc':shapely.GeometryCollection([box(0,0,1,1)]),'empty':Polygon(),'missing':None,'invalid':Polygon([(0,0),(2,2),(0,2),(2,0),(0,0)]),'z':shapely.from_wkt('POLYGON Z ((0 0 1,1 0 1,1 1 1,0 0 1))'),'m':shapely.from_wkt('POLYGON M ((0 0 1,1 0 1,1 1 1,0 0 1))'),'nonfinite':shapely.from_wkt('POLYGON ((0 0, Infinity 0,1 1,0 0))')}
        frame[frame.geometry.name]=gpd.GeoSeries([shapes[problem],box(20,0,21,1)],crs=frame.crs)
    with pytest.raises(UnionInputError)as caught:exact_union(a,b)
    assert caught.value.code==('source'if side=='a'else'reference')


@pytest.mark.parametrize('problem',['shared','geometry','fixed_a','fixed_b','crs_missing','crs_geographic','crs_mismatch'])
def test_vector_union_rejects_field_conflicts_and_crs_even_when_both_empty(problem):
    from operators.geometric_operators import UnionInputError
    a,b=overlay_inputs([],[])
    if problem=='shared':a['shared']=pd.Series(dtype='int64');b['shared']=pd.Series(dtype='string')
    elif problem=='geometry':b[a.geometry.name]=pd.Series(dtype='int64')
    elif problem.startswith('fixed'):a['source_id_a'if problem=='fixed_a'else'source_id_b']=pd.Series(dtype='string')
    else:b.set_crs({'crs_missing':None,'crs_geographic':'EPSG:4326','crs_mismatch':'EPSG:2263'}[problem],allow_override=True,inplace=True)
    with pytest.raises(UnionInputError)as caught:exact_union(a,b)
    assert caught.value.code==('crs'if problem.startswith('crs')else'fields')


@pytest.mark.parametrize('language',['zh-cn','en'])
@pytest.mark.parametrize('problem,reason,param',[('source','operator_input_invalid','input_gdf'),('reference','operator_reference_invalid','gdf_b'),('crs','operator_crs_invalid',''),('fields','operator_overlay_fields_conflict',''),('source_overlap','operator_input_overlap','input_gdf'),('reference_overlap','operator_reference_overlap','gdf_b')])
def test_vector_union_typed_diagnostics_distinguish_both_overlap_sides_without_source_values(language,problem,reason,param):
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime import WorkflowExecutionError
    a,b=overlay_inputs()
    if problem=='source':a['a_key']=['']
    if problem=='reference':b['b_key']=[None]
    if problem=='crs':b.set_crs(None,allow_override=True,inplace=True)
    if problem=='fields':a['PRIVATE']=1;b['PRIVATE']=2
    if problem=='source_overlap':a=pd.concat([a,a.assign(a_key=2)],ignore_index=True)
    if problem=='reference_overlap':b=pd.concat([b,b.assign(b_key='b2')],ignore_index=True)
    tasks=[{'id':'PRIVATE_UNION_NODE','operator':'union','depends_on':[],'params':{'input_gdf':a,'gdf_b':b,'id_field':'a_key','reference_id_field':'b_key'}}]
    with pytest.raises(WorkflowExecutionError)as caught:GeoPythonWorkflowRunner(language=language).execute({'tasks':tasks})
    assert caught.value.error_code=='INVALID_PARAMS' and caught.value.diagnostic=={'schema_version':'addp.execution-failure/v3','stage':'execution','reason':reason,'operator':{'node_index':1,'operator_id':'union',**({'parameter':param}if param else{})}}
    assert 'PRIVATE'not in json.dumps(caught.value.diagnostic)
    assert ('Union'in str(caught.value))if language=='en'else('联合'in str(caught.value))


@pytest.mark.parametrize('stage',['relate_pattern','union_all','intersection','difference'])
def test_vector_union_native_faults_remain_execution_failed(monkeypatch,stage):
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime import WorkflowExecutionError
    def bad(*args,**kwargs):raise shapely.errors.GEOSException('PRIVATE source values')
    a,b=overlay_inputs([box(0,0,1,1),box(1,0,2,1)],[box(.5,-1,1.5,2)])
    monkeypatch.setattr(shapely,stage,bad)
    with pytest.raises(WorkflowExecutionError)as caught:GeoPythonWorkflowRunner().execute({'tasks':[{'id':'union','operator':'union','depends_on':[],'params':{'input_gdf':a,'gdf_b':b,'id_field':'a_key','reference_id_field':'b_key'}}]})
    assert caught.value.error_code=='EXECUTION_FAILED'and caught.value.diagnostic is None


@pytest.mark.parametrize('shape',[shapely.from_wkt('POLYGON Z ((0 0 1,1 0 1,1 1 1,0 0 1))'),Polygon([(0,0),(2,2),(0,2),(2,0),(0,0)]),shapely.from_wkt('POLYGON ((0 0, Infinity 0,1 1,0 0))')])
def test_vector_union_invalid_native_results_are_not_parameter_errors(monkeypatch,shape):
    monkeypatch.setattr(shapely,'intersection',lambda *a,**k:shape)
    from operators.geometric_operators import UnionInputError
    a,b=overlay_inputs()
    with pytest.raises(RuntimeError)as caught:exact_union(a,b)
    assert not isinstance(caught.value,UnionInputError)


def test_vector_union_never_uses_legacy_overlay_or_automatic_repair(monkeypatch):
    def forbidden(*args,**kwargs):raise AssertionError('legacy overlay or implicit repair')
    monkeypatch.setattr(gpd,'overlay',forbidden);monkeypatch.setattr(shapely,'make_valid',forbidden)
    a,b=overlay_inputs();assert len(exact_union(a,b))==3


def test_vector_union_gpkg_save_reload_keeps_nullable_fields_sources_and_failure_guard(tmp_path):
    from osgeo import ogr
    a,b=overlay_inputs([box(0,0,4,2)],[box(2,0,6,2)])
    a['a_key']=pd.Series([2**60+1],dtype='Int64');a['grade']=pd.Series([2**53+1],dtype='Int64');a['geometry']=pd.Series(['业务字段'],dtype='string');b['enabled']=pd.Series([True],dtype='boolean')
    for name,frame in [('a',a),('b',b)]:frame.to_file(tmp_path/(name+'.gpkg'),driver='GPKG',index=False)
    original={n:(tmp_path/(n+'.gpkg')).read_bytes()for n in ('a','b')};conn={'engine_type':'nfs','mount_path':str(tmp_path)}
    tasks=[{'id':'PRIVATE_UNION','operator':'union','depends_on':['a','b'],'params':{'input_gdf':{'$ref':'a'},'gdf_b':{'$ref':'b'},'id_field':'a_key','reference_id_field':'b_key'}}]
    tasks.extend({'id':n,'operator':'load','depends_on':[],'params':{'connection_info':conn,'path':n+'.gpkg'}}for n in ('a','b'))
    tasks.append({'id':'save','operator':'save','depends_on':['PRIVATE_UNION'],'params':{'input_df':{'$ref':'PRIVATE_UNION'},'connection_info':conn,'path':'result.gpkg','mode':'fail'}})
    state=execute_workflow({'tasks':tasks});assert state['status']=='success',state
    ds=ogr.Open(str(tmp_path/'result.gpkg'));layer=ds.GetLayer(0);rows=list(layer)
    assert len(rows)==3 and layer.GetSpatialRef().GetAuthorityCode(None)=='3857'
    for field in ['a_key','grade','source_id_a']:assert layer.GetLayerDefn().GetFieldDefn(layer.GetLayerDefn().GetFieldIndex(field)).GetType()==ogr.OFTInteger64
    for i,(row,expected)in enumerate(zip(rows,[box(2,0,4,2),box(0,0,2,2),box(4,0,6,2)])):
        assert row.GetGeometryRef().Normalize().ExportToIsoWkb()==ogr.CreateGeometryFromWkt(expected.wkt).Normalize().ExportToIsoWkb()
        assert row.GetField('source_id_a')==([2**60+1]*2+[None])[i]and row.GetField('source_id_b')==['11',None,'11'][i]
        assert row.GetField('grade')==[2**53+1,2**53+1,None][i]and row.GetField('geometry')==['业务字段','业务字段',None][i]
        assert row.GetField('enabled')==[True,None,True][i]
    column=layer.GetGeometryColumn();row=rows=layer=ds=None
    saved=get_operator('load')(connection_info=conn,path='result.gpkg');assert saved.geometry.name==column and str(saved.grade.dtype)=='Int64'and str(saved.enabled.dtype)=='boolean'
    tasks[0]['params']['reference_id_field']='PRIVATE_MISSING';tasks[-1]['params']['path']='forbidden.gpkg';state=execute_workflow({'tasks':tasks})
    assert state['status']=='failed'and state['error_code']=='INVALID_PARAMS'and not(tmp_path/'forbidden.gpkg').exists()
    for n,data in original.items():assert(tmp_path/(n+'.gpkg')).read_bytes()==data


@pytest.mark.parametrize('problem',[None,'source','reference','crs','fields','source_overlap','reference_overlap'])
def test_vector_union_async_api_discovery_six_admissions_and_frozen_node(tmp_path,monkeypatch,problem):
    import api_server
    from addp_common.workflow_runtime import ExecutionRegistry
    a,b=overlay_inputs([box(0,0,4,2)],[box(2,0,6,2)])
    if problem=='source':a['a_key']=['']
    if problem=='reference':b['b_key']=[None]
    if problem=='crs':b.set_crs('EPSG:3395',allow_override=True,inplace=True)
    if problem=='fields':a['PRIVATE']=1;b['PRIVATE']=2
    if problem=='source_overlap':a=pd.concat([a,a.assign(a_key=2)],ignore_index=True)
    if problem=='reference_overlap':b=pd.concat([b,b.assign(b_key='b2')],ignore_index=True)
    for name,frame in [('a',a),('b',b)]:frame.to_file(tmp_path/(name+'.gpkg'),driver='GPKG',index=False)
    conn={'engine_type':'nfs','mount_path':str(tmp_path)}
    tasks=[{'id':'PRIVATE_UNION_NODE','operator':'union','depends_on':['a','b'],'params':{'input_gdf':{'$ref':'a'},'gdf_b':{'$ref':'b'},'id_field':'a_key','reference_id_field':'b_key'}}]
    tasks.extend({'id':n,'operator':'load','depends_on':[],'params':{'connection_info':conn,'path':n+'.gpkg'}}for n in ('a','b'))
    monkeypatch.setattr(api_server,'executions',ExecutionRegistry())
    with api_server.app.test_client()as client:
        spec=next(s for s in client.get('/api/operators').get_json()['operators']if s['id']=='union')
        assert spec['display_name']=='联合叠加'and all(p['required']for p in spec['parameters'])and len(spec['parameters'])==4
        assert client.post('/api/operators/union/invoke',json={'params':{}}).status_code==403
        response=client.post('/api/workflow',json={'workflow_def':{'tasks':tasks},'runtime':{'tenant_id':7,'execution_authorization':{'id':1,'effects':['read']}}});assert response.status_code==202
        deadline=time.monotonic()+5
        while time.monotonic()<deadline:
            observed=client.get('/api/executions/'+response.get_json()['execution_id']);assert observed.status_code==200;state=observed.get_json()
            if state['status']in('success','failed'):break
            time.sleep(.01)
        if problem is None:assert state['status']=='success'and len(json.loads(state['result'])['features'])==3,state
        else:
            reason,param={'source':('operator_input_invalid','input_gdf'),'reference':('operator_reference_invalid','gdf_b'),'crs':('operator_crs_invalid',''),'fields':('operator_overlay_fields_conflict',''),'source_overlap':('operator_input_overlap','input_gdf'),'reference_overlap':('operator_reference_overlap','gdf_b')}[problem]
            assert state['status']=='failed'and state['error_code']=='INVALID_PARAMS',state
            assert state['diagnostic']=={'schema_version':'addp.execution-failure/v3','stage':'execution','reason':reason,'operator':{'node_index':1,'operator_id':'union',**({'parameter':param}if param else{})}}
            assert 'PRIVATE'not in json.dumps(state['diagnostic'])


@pytest.mark.parametrize('side',['a','b'])
def test_intersection_overlay_duplicate_geometry_labels_are_typed_before_column_resolution(side):
    from operators.geometric_operators import IntersectionInputError
    a,b=overlay_inputs();frame=a if side=='a'else b;frame.columns=[frame.geometry.name,frame.geometry.name]
    with pytest.raises(IntersectionInputError)as caught:exact_overlay(a,b)
    assert caught.value.code==('source'if side=='a'else'reference')


@pytest.mark.parametrize('touch',['edge','point'])
def test_vector_union_allows_intralayer_boundary_contacts(touch):
    other=box(1,0,2,1)if touch=='edge'else box(1,1,2,2)
    a,b=overlay_inputs([box(0,0,1,1),other],[box(10,0,11,1)])
    result=exact_union(a,b)
    assert len(result)==3 and all(g.equals(e)for g,e in zip(result.geometry,[box(0,0,1,1),other,box(10,0,11,1)]))


def test_vector_dissolve_groups_keep_null_members_and_both_ports():
    frame=gpd.GeoDataFrame({'key':pd.Series([2**60+1,2**60+3,2**60+5],dtype='Int64'),'zone':pd.Series(['a',None,None],dtype='string'),'grade':pd.Series([1,2,3],dtype='Int64')},geometry=[box(0,0,1,1),box(2,0,3,1),box(2.5,0,4,1)],crs='EPSG:3857').rename_geometry('shape')
    out=get_operator('dissolve')(frame,id_field='key',group_fields=['zone'])
    assert set(out)=={'result','members'}
    assert len(out['result'])==2 and out['result'].member_count.tolist()==[1,2]
    assert out['result'].geometry.iloc[1].equals(box(2,0,4,1))
    assert out['members'].source_id.tolist()==frame.key.tolist() and out['members'].fusion_id.tolist()==[1,2,2]
    assert 'grade'not in out['result'] and out['members'].grade.tolist()==[1,2,3]


def dissolve_frame(shapes=None):
    a,_=overlay_inputs([box(0,0,2,1),box(1,0,3,1),box(5,0,6,1)]if shapes is None else shapes)
    a['zone']=pd.Series(['x']*len(a),dtype='string');a['grade']=pd.Series(range(len(a)),dtype='Int64')
    return a


@pytest.mark.parametrize('groups',[[],['zone']])
def test_vector_dissolve_global_duplicate_members_types_and_order(groups):
    a=dissolve_frame();a['a_key']=pd.Series([2**64-1,2**64-2,2**64-3],dtype='UInt64');a['geometry']=pd.Series(['a',None,'c'],dtype='string');a.index=[8,8,-1];before=a.copy(deep=True)
    out=get_operator('dissolve')(a,'a_key',groups);result,members=out['result'],out['members']
    assert len(result)==1 and result.geometry.iloc[0].equals(shapely.MultiPolygon([box(0,0,3,1),box(5,0,6,1)]))
    assert result.member_count.tolist()==[3]and result.fusion_id.tolist()==[1]
    assert list(result.columns)==groups+['fusion_id','member_count',a.geometry.name]
    assert members.source_id.dtype==a.a_key.dtype and members.source_id.tolist()==a.a_key.tolist()
    assert members['geometry'].tolist()==['a',pd.NA,'c'] and members.grade.dtype==a.grade.dtype
    assert_geodataframe_equal(members.drop(columns=['source_id','fusion_id']),a.reset_index(drop=True));assert_geodataframe_equal(a,before)


def test_vector_dissolve_multifield_null_group_first_appearance_and_cross_group_overlap():
    a=dissolve_frame([box(0,0,1,1)]*4);a['zone']=pd.Series([None,'x',None,'x'],dtype='string');a['flag']=pd.Series([True,False,True,True],dtype='boolean')
    out=get_operator('dissolve')(a,'a_key',['zone','flag']);assert out['result'].member_count.tolist()==[2,1,1]and out['members'].fusion_id.tolist()==[1,2,1,3]
    assert out['result'].zone.isna().tolist()==[True,False,False]
    assert all(g.equals(box(0,0,1,1))for g in out['result'].geometry)


@pytest.mark.parametrize('groups',[[],['zone']])
def test_vector_dissolve_empty_schema_and_original_active_geometry(groups):
    a=dissolve_frame([]);out=get_operator('dissolve')(a,'a_key',groups)
    assert all(f.empty and f.geometry.name==a.geometry.name and f.crs==a.crs for f in out.values())
    assert out['members'].source_id.dtype==a.a_key.dtype and str(out['result'].fusion_id.dtype)=='int64'
    assert list(out['result'].columns)==groups+['fusion_id','member_count',a.geometry.name]


@pytest.mark.parametrize('problem',['id_missing','id_null','id_duplicate','point','empty','missing','invalid','z','m','extra_geometry','duplicate_columns','crs_missing','crs_geographic','groups_none','groups_string','groups_duplicate','groups_missing','groups_geometry','groups_nested','field_fusion_id','field_member_count','field_source_id','geometry_fusion_id'])
def test_vector_dissolve_typed_rejections(problem):
    from operators.geometric_operators import DissolveInputError
    a=dissolve_frame();groups=[];code='source'
    if problem=='id_missing':a.drop(columns='a_key',inplace=True)
    elif problem=='id_null':a.loc[0,'a_key']=None
    elif problem=='id_duplicate':a.loc[0,'a_key']=2
    elif problem=='extra_geometry':a['extra']=gpd.GeoSeries([Point(0,0)]*3,crs=a.crs)
    elif problem=='duplicate_columns':a.columns=[a.geometry.name]*len(a.columns)
    elif problem.startswith('crs'):
        code='crs';a.set_crs(None if problem=='crs_missing'else'EPSG:4326',allow_override=True,inplace=True)
    elif problem.startswith('groups'):
        code='groups';groups={'groups_none':None,'groups_string':'zone','groups_duplicate':['zone','zone'],'groups_missing':['PRIVATE'],'groups_geometry':[a.geometry.name],'groups_nested':['zone']}[problem]
        if problem=='groups_nested':a['zone']=[{},[],{}]
    elif problem.startswith('field'):
        code='fields';a[problem[6:]]=1
    elif problem=='geometry_fusion_id':code='fields';a=a.rename_geometry('fusion_id')
    else:
        shapes={'point':Point(0,0),'empty':Polygon(),'missing':None,'invalid':Polygon([(0,0),(2,2),(0,2),(2,0),(0,0)]),'z':shapely.from_wkt('POLYGON Z ((0 0 1,1 0 1,1 1 1,0 0 1))'),'m':shapely.from_wkt('POLYGON M ((0 0 1,1 0 1,1 1 1,0 0 1))')};a[a.geometry.name]=gpd.GeoSeries([shapes[problem],box(2,0,3,1),box(4,0,5,1)],crs=a.crs)
    with pytest.raises(DissolveInputError)as caught:get_operator('dissolve')(a,'a_key',groups)
    assert caught.value.code==code


@pytest.mark.parametrize('lang',['zh-cn','en'])
@pytest.mark.parametrize('problem,reason,param',[('source','operator_input_invalid','input_gdf'),('crs','operator_crs_invalid',''),('groups','operator_group_fields_invalid','group_fields'),('fields','operator_dissolve_fields_conflict','')])
def test_vector_dissolve_four_frozen_diagnostics(lang,problem,reason,param):
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime import WorkflowExecutionError
    a=dissolve_frame();groups=[]
    if problem=='source':a.loc[0,'a_key']=None
    if problem=='crs':a.set_crs(None,allow_override=True,inplace=True)
    if problem=='groups':groups=['PRIVATE']
    if problem=='fields':a['fusion_id']=1
    with pytest.raises(WorkflowExecutionError)as caught:GeoPythonWorkflowRunner(language=lang).execute({'tasks':[{'id':'PRIVATE','operator':'dissolve','depends_on':[],'params':{'input_gdf':a,'id_field':'a_key','group_fields':groups}}]})
    assert caught.value.error_code=='INVALID_PARAMS'and caught.value.diagnostic=={'schema_version':'addp.execution-failure/v3','stage':'execution','reason':reason,'operator':{'node_index':1,'operator_id':'dissolve',**({'parameter':param}if param else{})}}
    assert 'PRIVATE'not in json.dumps(caught.value.diagnostic)


def test_vector_dissolve_native_fault_not_parameter_error(monkeypatch):
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime import WorkflowExecutionError
    def bad(*a,**k):raise shapely.errors.GEOSException('PRIVATE native')
    monkeypatch.setattr(shapely,'union_all',bad)
    with pytest.raises(WorkflowExecutionError)as caught:GeoPythonWorkflowRunner().execute({'tasks':[{'id':'f','operator':'dissolve','depends_on':[],'params':{'input_gdf':dissolve_frame(),'id_field':'a_key','group_fields':[]}}]})
    assert caught.value.error_code=='EXECUTION_FAILED'and caught.value.diagnostic is None


def test_vector_dissolve_gpkg_double_save_independent_ogr_and_failure_guard(tmp_path):
    from osgeo import ogr
    a=dissolve_frame();a['a_key']=pd.Series([2**60+1,2**60+3,2**60+5],dtype='Int64');a.to_file(tmp_path/'a.gpkg',driver='GPKG',index=False);original=(tmp_path/'a.gpkg').read_bytes();conn={'engine_type':'nfs','mount_path':str(tmp_path)}
    tasks=[{'id':'load','operator':'load','depends_on':[],'params':{'connection_info':conn,'path':'a.gpkg'}},{'id':'f','operator':'dissolve','depends_on':['load'],'params':{'input_gdf':{'$ref':'load'},'id_field':'a_key','group_fields':[]}}]
    for name in ['result','members']:tasks.append({'id':name,'operator':'save','depends_on':['f'],'params':{'input_df':{'$ref':'f','port':name},'connection_info':conn,'path':name+'.gpkg','mode':'fail'}})
    state=execute_workflow({'tasks':tasks});assert state['status']=='success',state
    for name,expected in [('result',[shapely.MultiPolygon([box(0,0,3,1),box(5,0,6,1)])]),('members',a.geometry.tolist())]:
        ds=ogr.Open(str(tmp_path/(name+'.gpkg')));layer=ds.GetLayer(0);rows=list(layer);assert len(rows)==len(expected)and layer.GetSpatialRef().GetAuthorityCode(None)=='3857'
        for i,(row,shape)in enumerate(zip(rows,expected)):
            actual=row.GetGeometryRef();oracle_shape=ogr.CreateGeometryFromWkt(shape.wkt)
            assert actual.IsValid() and actual.SymDifference(oracle_shape).IsEmpty() and actual.GetArea()==oracle_shape.GetArea()
            if name=='members':assert actual.Normalize().ExportToIsoWkb()==oracle_shape.Normalize().ExportToIsoWkb()
            if name=='members':assert row.GetField('source_id')==2**60+1+2*i and row.GetField('grade')==i
            else:assert row.GetField('member_count')==3
        actual=oracle_shape=row=rows=layer=ds=None
        saved=get_operator('load')(connection_info=conn,path=name+'.gpkg');assert len(saved)==len(expected)
    tasks[1]['params']['group_fields']=['PRIVATE'];tasks[-2]['params']['path']='forbidden-result.gpkg';tasks[-1]['params']['path']='forbidden-members.gpkg';state=execute_workflow({'tasks':tasks})
    assert state['status']=='failed'and state['error_code']=='INVALID_PARAMS'and not(tmp_path/'forbidden-result.gpkg').exists()and not(tmp_path/'forbidden-members.gpkg').exists()
    assert(tmp_path/'a.gpkg').read_bytes()==original


@pytest.mark.parametrize('problem',[None,'source','crs','groups','fields'])
def test_vector_dissolve_async_api_four_admissions(tmp_path,monkeypatch,problem):
    import api_server
    from addp_common.workflow_runtime import ExecutionRegistry
    a=dissolve_frame();groups=[]
    if problem=='source':a['a_key']=['', 'b', 'c']
    if problem=='crs':a.set_crs('EPSG:4326',allow_override=True,inplace=True)
    if problem=='groups':groups=['PRIVATE']
    if problem=='fields':a['source_id']=1
    a.to_file(tmp_path/'a.gpkg',driver='GPKG',index=False);conn={'engine_type':'nfs','mount_path':str(tmp_path)}
    tasks=[{'id':'PRIVATE','operator':'dissolve','depends_on':['load'],'params':{'input_gdf':{'$ref':'load'},'id_field':'a_key','group_fields':groups}},{'id':'load','operator':'load','depends_on':[],'params':{'connection_info':conn,'path':'a.gpkg'}}]
    monkeypatch.setattr(api_server,'executions',ExecutionRegistry())
    with api_server.app.test_client()as client:
        spec=next(v for v in client.get('/api/operators').get_json()['operators']if v['id']=='dissolve');assert len(spec['output_ports'])==2
        assert client.post('/api/operators/dissolve/invoke',json={'params':{}}).status_code==403
        response=client.post('/api/workflow',json={'workflow_def':{'tasks':tasks},'runtime':{'tenant_id':7,'execution_authorization':{'id':1,'effects':['read']}}});assert response.status_code==202
        deadline=time.monotonic()+5
        while time.monotonic()<deadline:
            got=client.get('/api/executions/'+response.get_json()['execution_id']);assert got.status_code==200;state=got.get_json()
            if state['status']in('success','failed'):break
            time.sleep(.01)
        if problem is None:assert state['status']=='success',state
        else:
            reason,param={'source':('operator_input_invalid','input_gdf'),'crs':('operator_crs_invalid',''),'groups':('operator_group_fields_invalid','group_fields'),'fields':('operator_dissolve_fields_conflict','')}[problem]
            assert state['status']=='failed'and state['error_code']=='INVALID_PARAMS',state
            assert state['diagnostic']=={'schema_version':'addp.execution-failure/v3','stage':'execution','reason':reason,'operator':{'node_index':1,'operator_id':'dissolve',**({'parameter':param}if param else{})}}


@pytest.mark.parametrize('problem',[None,'count','source_id','member_order','member_geometry','null','boolean_type','extra_output','source_changed'])
def test_dissolve_t4_two_success_eight_refusals_four_saves_and_ogr(tmp_path,problem):
    import importlib
    from osgeo import ogr
    f=importlib.import_module('business.scripts.online-vector-topology-fixture');a=importlib.import_module('scripts.test.vector-topology-persistence-online')
    (tmp_path/'outputs').mkdir();(tmp_path/'.addp-online-owner').touch();seeded=f.worker('seed',tmp_path,f.DISSOLVE_CASE)
    def definition(variant='grouped',failure=None):
        value=a.dissolve_definition(list(f.DISSOLVE_SOURCES),'unused',variant,failure)
        for task in value['tasks']:
            if task['operator']=='load':task['params']={'connection_info':{'engine_type':'nfs','mount_path':str(tmp_path)},'path':task['params']['locator']}
            elif task['operator']=='save':
                name=task['params'].pop('target_name');task['params'].pop('target_parent_locator');task['params'].update(connection_info={'engine_type':'nfs','mount_path':str(tmp_path)},path='outputs/'+name)
        return value
    for variant in('grouped','global'):
        result=execute_workflow(definition(variant));assert result['status']=='success',result
        native=f.worker('verify',tmp_path,f.DISSOLVE_CASE);assert native['sources']==seeded['sources']
        assert len(native['targets'])==(2 if variant=='grouped'else 4)
    assert [native['targets'][n]['feature_count']for n in f.DISSOLVE_TARGETS]==[4,10,1,10]
    for variant in('grouped','global'):
        conn={'engine_type':'nfs','mount_path':str(tmp_path)}
        result=get_operator('load')(connection_info=conn,path='outputs/'+variant+'-result.gpkg');members=get_operator('load')(connection_info=conn,path='outputs/'+variant+'-members.gpkg')
        assert sum(result.member_count)==len(members)==10
        assert members.source_id.tolist()==[r['properties']['parcel_key']for r in f.dissolve_features('source')]
        assert members.geometry.name not in members.drop(columns=members.geometry.name).columns and 'geometry'in members.columns
        assert str(members['rank'].dtype)=='Int64'and str(members['enabled'].dtype)=='boolean'
    if problem is None:
        for spec in f.DISSOLVE_FAILURES:
            failed=execute_workflow(definition(failure=spec[0]));assert failed['status']=='failed'and failed['error_code']=='INVALID_PARAMS',failed
            assert failed['diagnostic']==a.dissolve_diagnostic(spec),failed
            assert f.worker('verify',tmp_path,f.DISSOLVE_CASE)==native
        return
    if problem=='extra_output':(tmp_path/'outputs'/'unexpected.gpkg').write_bytes(b'x')
    else:
        name='grouped-result.gpkg'if problem=='count'else'grouped-members.gpkg';path=tmp_path/'outputs'/name
        if problem=='source_changed':path=tmp_path/f.DISSOLVE_SOURCES[0]
        ds=ogr.Open(str(path),1);layer=ds.GetLayer(0);row=layer.GetNextFeature()
        if problem=='count':row.SetField('member_count',3)
        elif problem=='source_id':row.SetField('source_id',17)
        elif problem=='member_order':row.SetField('row_no',2)
        elif problem in('member_geometry','source_changed'):row.SetGeometry(ogr.CreateGeometryFromWkt('POLYGON ((99 0,100 0,100 1,99 1,99 0))'))
        elif problem=='null':row.SetField('note','changed')
        elif problem=='boolean_type':
            field=ogr.FieldDefn('enabled',ogr.OFTInteger);field.SetSubType(ogr.OFSTNone)
            assert layer.AlterFieldDefn(layer.GetLayerDefn().GetFieldIndex('enabled'),field,ogr.ALTER_TYPE_FLAG)==0
        if problem!='boolean_type':assert layer.SetFeature(row)==0
        row=layer=ds=None
    with pytest.raises(f.FixtureError):f.worker('verify',tmp_path,f.DISSOLVE_CASE)


@pytest.mark.parametrize('problem',[None,'coordinate','identity','order','null','boolean_type','extra_output','source_changed','collection_member'])
def test_difference_t4_mixed_mask_and_four_refusals_independent_ogr(tmp_path,problem):
    import importlib
    from osgeo import ogr
    f=importlib.import_module('business.scripts.online-vector-topology-fixture');a=importlib.import_module('scripts.test.vector-topology-persistence-online')
    (tmp_path/'outputs').mkdir();(tmp_path/'.addp-online-owner').touch();seed=f.worker('seed',tmp_path,f.DIFFERENCE_CASE)
    def definition(failure=None):
        value=a.difference_definition(list(f.DIFFERENCE_SOURCES),'unused',failure)
        for task in value['tasks']:
            if task['operator']=='load':task['params']={'connection_info':{'engine_type':'nfs','mount_path':str(tmp_path)},'path':task['params']['locator']}
            elif task['operator']=='save':
                name=task['params'].pop('target_name');task['params'].pop('target_parent_locator');task['params'].update(connection_info={'engine_type':'nfs','mount_path':str(tmp_path)},path='outputs/'+name)
        return value
    result=execute_workflow(definition());assert result['status']=='success',result
    native=f.worker('verify',tmp_path,f.DIFFERENCE_CASE);assert native['sources']==seed['sources']
    table=native['targets'][f.DIFFERENCE_TARGET];assert table['feature_count']==9
    assert {row['geometry']['type']for row in table['features']}=={'Point','LineString','Polygon','MultiPoint','MultiLineString','MultiPolygon','GeometryCollection'}
    assert table['features'][2]['geometry']['coordinates']==[[[400,0],[401,0]],[[404,0],[406,0]]]
    loaded=get_operator('load')(connection_info={'engine_type':'nfs','mount_path':str(tmp_path)},path='outputs/'+f.DIFFERENCE_TARGET)
    assert loaded.grade.dtype=='Int64'and loaded.enabled.dtype=='boolean'
    assert loaded.a_key.tolist()==[r['properties']['a_key']for r in f.difference_features('result')]
    assert loaded.geometry.name!='geometry'and 'geometry'in loaded.columns and 'source_id'not in loaded.columns
    if problem is None:
        for spec in f.DIFFERENCE_FAILURES:
            failed=execute_workflow(definition(spec[0]));assert failed['status']=='failed'and failed['error_code']=='INVALID_PARAMS',failed
            assert failed['diagnostic']==a.difference_diagnostic(spec),failed
            assert f.worker('verify',tmp_path,f.DIFFERENCE_CASE)==native
        return
    if problem=='extra_output':(tmp_path/'outputs'/'unexpected.gpkg').write_bytes(b'x')
    else:
        path=tmp_path/'outputs'/f.DIFFERENCE_TARGET
        if problem=='source_changed':path=tmp_path/f.DIFFERENCE_SOURCES[0]
        ds=ogr.Open(str(path),1);layer=ds.GetLayer(0);row=layer.GetFeature(8)if problem=='collection_member'else layer.GetNextFeature()
        if problem in('coordinate','source_changed'):row.SetGeometry(ogr.CreateGeometryFromWkt('POINT (99 99)'))
        elif problem=='identity':row.SetField('a_key',17)
        elif problem=='order':row.SetField('order_tag',2)
        elif problem=='null':row.SetField('grade',0)
        elif problem=='collection_member':row.GetGeometryRef().RemoveGeometry(0)
        elif problem=='boolean_type':
            field=ogr.FieldDefn('enabled',ogr.OFTInteger);assert layer.AlterFieldDefn(layer.GetLayerDefn().GetFieldIndex('enabled'),field,ogr.ALTER_TYPE_FLAG)==0
        if problem!='boolean_type':assert layer.SetFeature(row)==0
        row=layer=ds=None
    with pytest.raises(f.FixtureError):f.worker('verify',tmp_path,f.DIFFERENCE_CASE)


@pytest.mark.parametrize('operator', ['contains', 'intersects'])
@pytest.mark.parametrize('geom_name', ['shape', 'contains', 'intersects'])
def test_relation_filter_preserves_every_source_column_index_and_duplicate_row(operator, geom_name):
    source = gpd.GeoDataFrame({
        'grade': pd.Series([2**53+1, None, -1], dtype='Int64'),
        'enabled': pd.Series([True, None, False], dtype='boolean'),
        **{name: [f'{name}-{i}' for i in range(3)] for name in ('contains', 'intersects') if name != geom_name},
    }, geometry=[box(0,0,2,2), box(9,9,10,10), box(0,0,2,2)], crs=4326).rename_geometry(geom_name)
    source['geometry'] = pd.Series(['business-a', None, 'business-c'], dtype='string')
    source.index = pd.Index(['same', 'away', 'same'], name='source-index')
    reference = gpd.GeoDataFrame({'grade':[None,None]}, geometry=[Point(1,1),Point(1,1)], crs=4326)
    before_a, before_b = source.copy(), reference.copy()
    result = get_operator(operator)(source, reference)
    assert_geodataframe_equal(result, source.iloc[[0,2]])
    assert_geodataframe_equal(source, before_a)
    assert_geodataframe_equal(reference, before_b)
    assert list(result.columns) == list(source.columns)


@pytest.mark.parametrize('a,b,contains_expected,intersects_expected', [
    (box(0,0,2,2), Point(1,1), True, True),
    (box(0,0,2,2), Point(0,1), False, True),
    (box(0,0,2,2), LineString([(0,0),(0,2)]), False, True),
    (box(0,0,2,2), box(0,0,1,1), True, True),
    (box(0,0,2,2), box(0,0,2,2), True, True),
    (box(0,0,2,2), box(2,0,3,1), False, True),
    (Point(1,1), Point(1,1), True, True),
    (Point(1,1), LineString([(0,1),(2,1)]), False, True),
    (Point(1,1), box(0,0,2,2), False, True),
    (LineString([(0,1),(2,1)]), Point(1,1), True, True),
    (LineString([(0,1),(2,1)]), Point(0,1), False, True),
    (LineString([(0,1),(2,1)]), LineString([(1,0),(1,2)]), False, True),
    (LineString([(0,1),(2,1)]), box(0,0,2,2), False, True),
    (Polygon([(0,0),(4,0),(4,4),(0,4)], holes=[[(1,1),(3,1),(3,3),(1,3)]]), Point(2,2), False, False),
    (shapely.MultiPoint([(0,0),(1,1)]), shapely.MultiPoint([(0,0),(1,1)]), True, True),
    (shapely.GeometryCollection([Point(5,5), box(0,0,2,2)]), Point(5,5), True, True),
    (box(0,0,2,2), shapely.GeometryCollection([Point(1,1),LineString([(0.5,0.5),(1.5,1.5)])]), True, True),
    (box(0,0,2,2), Point(2+2**-40,1), False, False),
])
def test_relation_filter_exact_topology_not_bbox_or_proper_contains(a,b,contains_expected,intersects_expected):
    left,right=overlay_inputs([a],[b])
    for name,expected in [('contains',contains_expected),('intersects',intersects_expected)]:
        result=get_operator(name)(left,right)
        assert len(result)==int(expected)
        assert_geodataframe_equal(result,left if expected else left.iloc[:0])


@pytest.mark.parametrize('operator', ['contains','intersects'])
@pytest.mark.parametrize('side', ['left','right','both'])
def test_relation_filter_empty_batch_keeps_source_schema(operator,side):
    left,right=overlay_inputs([box(0,0,2,2)],[Point(1,1)])
    if side in ('left','both'):left=left.iloc[:0]
    if side in ('right','both'):right=right.iloc[:0]
    assert_geodataframe_equal(get_operator(operator)(left,right),left.iloc[:0])


def test_relation_filter_contains_matches_any_individual_not_reference_union():
    left,right=overlay_inputs([box(0,0,2,2)],[Point(1,1),Point(100,100)])
    assert_geodataframe_equal(get_operator('contains')(left,right),left)
    assert_geodataframe_equal(get_operator('intersects')(left,right),left)


@pytest.mark.parametrize('operator',['contains','intersects'])
@pytest.mark.parametrize('problem',['missing','empty','invalid','z','m','infinite','duplicate_columns','extra_geometry','unknown_crs','mismatch','geocentric','three_axis'])
@pytest.mark.parametrize('side',['left','right'])
def test_relation_filter_rejects_known_input_facts_even_when_other_side_empty(operator,problem,side):
    from operators.spatial_relations import RelationInputError
    left,right=overlay_inputs([box(0,0,2,2)],[Point(1,1)])
    frame=left if side=='left' else right
    if problem in ('missing','empty','invalid','z','m','infinite'):
        value={'missing':None,'empty':Point(),'invalid':Polygon([(0,0),(2,2),(0,2),(2,0),(0,0)]),
               'z':Point(0,0,1),'m':shapely.from_wkt('POINT M (0 0 1)'), 'infinite':Point(float('inf'),0)}[problem]
        frame[frame.geometry.name]=[value]
    elif problem=='duplicate_columns':frame.columns=[frame.geometry.name]*len(frame.columns)
    elif problem=='extra_geometry':frame['second_shape']=frame.geometry.copy()
    elif problem=='unknown_crs':frame=frame.set_crs(None,allow_override=True)
    elif problem=='mismatch':frame=frame.set_crs(4326,allow_override=True)
    elif problem=='geocentric':frame=frame.set_crs(4978,allow_override=True)
    elif problem=='three_axis':frame=frame.set_crs(4979,allow_override=True)
    if side=='left':left,right=frame,right.iloc[:0]
    else:left,right=left.iloc[:0],frame
    with pytest.raises(RelationInputError) as caught:get_operator(operator)(left,right)
    assert caught.value.code==('crs' if problem in ('unknown_crs','mismatch','geocentric','three_axis') else 'source' if side=='left' else 'reference')


@pytest.mark.parametrize('operator',['contains','intersects'])
@pytest.mark.parametrize('language',['zh-cn','en'])
@pytest.mark.parametrize('problem,reason,param',[('source','operator_input_invalid','input_gdf'),('reference','operator_reference_invalid','gdf_b'),('crs','operator_crs_invalid','')])
def test_relation_filter_typed_diagnostics_and_failure_before_save(operator,language,problem,reason,param,monkeypatch):
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime import WorkflowExecutionError
    import workflow_engine
    left,right=overlay_inputs([box(0,0,2,2)],[Point(1,1)])
    if problem=='source':left[left.geometry.name]=[None]
    elif problem=='reference':right[right.geometry.name]=[None]
    else:right=right.set_crs(None,allow_override=True)
    original=workflow_engine.get_operator
    def lookup(name):
        if name=='save':raise AssertionError('invalid relation input reached save')
        return original(name)
    monkeypatch.setattr(workflow_engine,'get_operator',lookup)
    tasks=[{'id':'PRIVATE_NODE','operator':operator,'params':{'input_gdf':left,'gdf_b':right},'depends_on':[]},
           {'id':'save','operator':'save','params':{'input_df':{'$ref':'PRIVATE_NODE'}},'depends_on':['PRIVATE_NODE']}]
    with pytest.raises(WorkflowExecutionError) as caught:GeoPythonWorkflowRunner(language=language).execute({'tasks':tasks})
    assert caught.value.error_code=='INVALID_PARAMS'
    assert caught.value.diagnostic=={'schema_version':'addp.execution-failure/v3','stage':'execution','reason':reason,
        'operator':{'node_index':1,'operator_id':operator,**({'parameter':param}if param else{})}}
    assert 'PRIVATE' not in json.dumps(caught.value.diagnostic)
    if problem=='crs':assert ('地理或投影' if language=='zh-cn' else 'geographic or projected') in str(caught.value)


@pytest.mark.parametrize('operator',['contains','intersects'])
def test_relation_filter_calculation_exception_not_misclassified(monkeypatch,operator):
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime import WorkflowExecutionError
    left,right=overlay_inputs([box(0,0,2,2)],[Point(1,1)])
    def fail(*args,**kwargs):raise shapely.errors.GEOSException('PRIVATE calculation')
    monkeypatch.setattr(shapely,operator,fail)
    with pytest.raises(WorkflowExecutionError) as caught:GeoPythonWorkflowRunner().execute({'tasks':[
        {'id':'filter','operator':operator,'params':{'input_gdf':left,'gdf_b':right},'depends_on':[]}]})
    assert caught.value.error_code=='EXECUTION_FAILED' and caught.value.diagnostic is None


@pytest.mark.parametrize('operator',['contains','intersects'])
def test_relation_filter_uses_index_for_distant_candidates(monkeypatch,operator):
    left,right=overlay_inputs([box(0,0,2,2)],[Point(1,1)]+[Point(100+i,100+i)for i in range(100)])
    original=getattr(shapely,operator);calls=[]
    def observed(a,b):
        calls.append(b)
        return original(a,b)
    monkeypatch.setattr(shapely,operator,observed)
    assert_geodataframe_equal(get_operator(operator)(left,right),left)
    assert len(calls)==1 and calls[0]==Point(1,1)


@pytest.mark.parametrize('operator',['contains','intersects'])
@pytest.mark.parametrize('crs',[4326,3857])
def test_relation_filter_native_save_reload_preserves_business_fields_and_original_geometries(tmp_path,operator,crs):
    from osgeo import ogr
    source=gpd.GeoDataFrame({'key':pd.Series([2**60+1,2**60+3,2**60+5],dtype='Int64'),
        'grade':pd.Series([None,1,2**53+1],dtype='Int64'),
        'enabled':pd.Series([None,True,False],dtype='boolean'),
        'contains':['kept-a','away','kept-c'],'intersects':['kept-x','away','kept-z']},
        geometry=[box(0,0,2,2),box(9,9,10,10),box(0,0,2,2)],crs=crs).rename_geometry('actual_shape')
    source['geometry']=pd.Series(['ordinary',None,'original'],dtype='string')
    reference=gpd.GeoDataFrame({'key':[None,None]},geometry=[Point(1,1),Point(1,1)],crs=crs).rename_geometry('reference_shape')
    conn={'engine_type':'nfs','mount_path':str(tmp_path)}
    result=get_operator(operator)(source,reference)
    get_operator('save')(input_df=result,connection_info=conn,path='result.gpkg',mode='fail')
    ds=ogr.Open(str(tmp_path/'result.gpkg'));layer=ds.GetLayer(0);rows=list(layer)
    assert len(rows)==2 and layer.GetSpatialRef().GetAuthorityCode(None)==str(crs)
    defn=layer.GetLayerDefn()
    assert defn.GetFieldDefn(defn.GetFieldIndex('key')).GetType()==ogr.OFTInteger64
    assert defn.GetFieldDefn(defn.GetFieldIndex('enabled')).GetSubType()==ogr.OFSTBoolean
    for i,row in enumerate(rows):
        assert row.GetField('key')==[2**60+1,2**60+5][i]
        assert row.GetField('grade')==[None,2**53+1][i]
        assert row.GetField('enabled')==[None,False][i]
        assert row.GetField('geometry')==['ordinary','original'][i]
        assert row.GetField('contains')==['kept-a','kept-c'][i]
        assert row.GetField('intersects')==['kept-x','kept-z'][i]
        assert row.GetGeometryRef().Normalize().ExportToIsoWkb()==ogr.CreateGeometryFromWkt(source.geometry.iloc[[0,2][i]].wkt).Normalize().ExportToIsoWkb()
    physical_name=layer.GetGeometryColumn();assert physical_name
    ds=None
    reloaded=get_operator('load')(connection_info=conn,path='result.gpkg')
    assert reloaded.geometry.name==physical_name
    assert str(reloaded.grade.dtype)=='Int64' and str(reloaded.enabled.dtype)=='boolean'
    assert list(reloaded['contains'])==['kept-a','kept-c']
    assert list(reloaded['intersects'])==['kept-x','kept-z']
    assert list(reloaded['geometry'])==['ordinary','original']
    assert all(a.equals_exact(b,0)for a,b in zip(reloaded.geometry,result.geometry))


def voronoi_inputs(points=None, masks=None):
    points=[Point(0,0),Point(4,0)] if points is None else points
    masks=[box(-2,-2,6,2)] if masks is None else masks
    left=gpd.GeoDataFrame({'station':pd.Series(range(1,len(points)+1),dtype='Int64')},geometry=points,crs=3857).rename_geometry('point_shape')
    right=gpd.GeoDataFrame({'range_name':['ignored']*len(masks)},geometry=masks,crs=3857).rename_geometry('coverage_shape')
    return left,right


def assert_voronoi_polygons(result,source,indices,expected):
    assert list(result.columns)==list(source.columns)
    assert list(result.index)==list(range(len(indices)))
    assert list(result.station)==list(source.station.iloc[indices])
    assert result.geometry.name==source.geometry.name and result.crs==source.crs
    for got,want in zip(result.geometry,expected):
        assert got.is_valid and got.geom_type in ('Polygon','MultiPolygon')
        assert got.normalize().equals_exact(want.normalize(),0)
    assert len(result)==len(expected)


def test_voronoi_contract_explicit_range_and_original_station_attributes():
    left,right=voronoi_inputs([Point(4,0),Point(0,0)])
    left['grade']=pd.Series([None,2**53+1],dtype='Int64')
    left['enabled']=pd.Series([None,False],dtype='boolean')
    left['geometry']=pd.Series(['business',None],dtype='string')
    left['fid']=pd.Series([2**60+1,2**60+3],dtype='Int64')
    left.index=pd.Index(['same','same'],name='original')
    before_a,before_b=left.copy(),right.copy()
    result=get_operator('voronoi')(left,right,'station')
    assert_voronoi_polygons(result,left,[0,1],[box(2,-2,6,2),box(-2,-2,2,2)])
    pd.testing.assert_frame_equal(result.drop(columns=result.geometry.name),left.reset_index(drop=True).drop(columns=left.geometry.name))
    assert_geodataframe_equal(left,before_a);assert_geodataframe_equal(right,before_b)


@pytest.mark.parametrize('outside',[False,True])
def test_voronoi_single_station_owns_complete_multipart_range_with_hole(outside):
    outer=Polygon([(-2,-2),(2,-2),(2,2),(-2,2)],holes=[[(-1,-1),(1,-1),(1,1),(-1,1)]])
    expected=shapely.MultiPolygon([outer,box(10,0,11,1)])
    left,right=voronoi_inputs([Point(100,100)if outside else Point(0,0)],[outer,box(10,0,11,1),outer])
    assert_voronoi_polygons(get_operator('voronoi')(left,right,'station'),left,[0],[expected])


def test_voronoi_collinear_stations_and_far_coverage_are_supported():
    left,right=voronoi_inputs([Point(4,0),Point(0,0),Point(2,0)],[box(-2,-1,6,1)])
    assert_voronoi_polygons(get_operator('voronoi')(left,right,'station'),left,[0,1,2],
        [box(3,-1,6,1),box(-2,-1,1,1),box(1,-1,3,1)])
    left,right=voronoi_inputs([Point(-10,0),Point(10,0)],[box(-1,-1,1,1)])
    result=get_operator('voronoi')(left,right,'station')
    assert_voronoi_polygons(result,left,[0,1],[box(-1,-1,0,1),box(0,-1,1,1)])
    assert all(not geom.covers(point)for geom,point in zip(result.geometry,left.geometry))


def test_voronoi_hole_and_positive_area_components_only_without_minimum_threshold():
    hole=[(-1,-.5),(.5,-.5),(.5,.5),(-1,.5)]
    mask=Polygon([(-2,-2),(6,-2),(6,2),(-2,2)],holes=[hole])
    left,right=voronoi_inputs(masks=[mask])
    expected=[Polygon([(-2,-2),(2,-2),(2,2),(-2,2)],holes=[hole]),box(2,-2,6,2)]
    assert_voronoi_polygons(get_operator('voronoi')(left,right,'station'),left,[0,1],expected)
    triangle=Polygon([(2,5),(3,4),(3,6)])
    left,right=voronoi_inputs(masks=[box(-1,-1,1,1),box(2,-1,3,1),triangle])
    expected=[box(-1,-1,1,1),shapely.MultiPolygon([box(2,-1,3,1),triangle])]
    assert_voronoi_polygons(get_operator('voronoi')(left,right,'station'),left,[0,1],expected)
    tiny=box(0,0,2**-20,2**-20)
    left,right=voronoi_inputs([Point(100,100)],[tiny])
    result=get_operator('voronoi')(left,right,'station')
    assert_voronoi_polygons(result,left,[0],[tiny]);assert result.geometry.iloc[0].area==2**-40


def test_voronoi_omits_zero_area_clips_and_keeps_one_multipart_row_per_station():
    left,right=voronoi_inputs(masks=[box(-2,-2,0,-1),box(-2,1,0,2)])
    assert_voronoi_polygons(get_operator('voronoi')(left,right,'station'),left,[0],
        [shapely.MultiPolygon([box(-2,-2,0,-1),box(-2,1,0,2)])])
    left,right=voronoi_inputs(masks=[box(-2,-1,2,1)])
    assert_voronoi_polygons(get_operator('voronoi')(left,right,'station'),left,[0],[box(-2,-1,2,1)])


@pytest.mark.parametrize('empty',['source','mask','both'])
def test_voronoi_empty_batch_preserves_full_source_schema(empty):
    left,right=voronoi_inputs()
    if empty in ('source','both'):left=left.iloc[:0]
    if empty in ('mask','both'):right=right.iloc[:0]
    assert_geodataframe_equal(get_operator('voronoi')(left,right,'station'),left.iloc[:0].reset_index(drop=True))


@pytest.mark.parametrize('side',['source','mask'])
@pytest.mark.parametrize('problem',['missing','empty','wrong_type','z','m','infinite','invalid','duplicate_columns','extra_geometry','unknown_crs','geographic','mismatch'])
def test_voronoi_typed_admission_rejects_invalid_inputs_before_empty_shortcut(side,problem):
    from operators.data_operations import VoronoiInputError
    left,right=voronoi_inputs();frame=left if side=='source'else right
    if problem=='duplicate_columns':frame.columns=[frame.geometry.name]*len(frame.columns)
    elif problem=='extra_geometry':frame['second_geometry']=frame.geometry.copy()
    elif problem in ('unknown_crs','geographic','mismatch'):
        frame=frame.set_crs({'unknown_crs':None,'geographic':4326,'mismatch':3395}[problem],allow_override=True)
    else:
        value={'missing':None,'empty':Point()if side=='source'else Polygon(),
            'wrong_type':shapely.MultiPoint([(0,0),(1,1)])if side=='source'else Point(0,0),
            'z':Point(0,0,1)if side=='source'else Polygon([(0,0,1),(1,0,1),(0,1,1)]),
            'm':shapely.from_wkt('POINT M (0 0 1)'if side=='source'else'POLYGON M ((0 0 1,1 0 1,0 1 1,0 0 1))'),
            'infinite':Point(float('inf'),0)if side=='source'else Polygon([(0,0),(float('inf'),0),(0,1)]),
            'invalid':LineString([(0,0),(0,0)])if side=='source'else Polygon([(0,0),(2,2),(0,2),(2,0),(0,0)])}[problem]
        frame[frame.geometry.name]=[value]*len(frame)
    if side=='source':left,right=frame,right.iloc[:0]
    else:left,right=left.iloc[:0],frame
    with pytest.raises(VoronoiInputError)as caught:get_operator('voronoi')(left,right,'station')
    assert caught.value.code==('crs'if problem in ('unknown_crs','geographic','mismatch')else side)


@pytest.mark.parametrize('problem',['same_xy','signed_zero','id_missing','id_null','id_duplicate','id_geometry','id_non_scalar'])
def test_voronoi_rejects_ambiguous_station_identity_without_deduplicating(problem):
    from operators.data_operations import VoronoiInputError
    left,right=voronoi_inputs();field='station'
    if problem=='same_xy':left[left.geometry.name]=[Point(1,2),Point(1,2)]
    elif problem=='signed_zero':left[left.geometry.name]=[Point(0.,0.),Point(-0.,-0.)]
    elif problem=='id_missing':field='PRIVATE'
    elif problem=='id_null':left['station']=pd.Series([None,2],dtype='Int64')
    elif problem=='id_duplicate':left['station']=[1,1]
    elif problem=='id_geometry':field=left.geometry.name
    else:left['station']=[[1],[2]]
    before=left.copy()
    with pytest.raises(VoronoiInputError)as caught:get_operator('voronoi')(left,right,field)
    assert caught.value.code=='source';assert_geodataframe_equal(left,before)


@pytest.mark.parametrize('language',['zh-cn','en'])
@pytest.mark.parametrize('problem,reason,param',[('source','operator_input_invalid','input_gdf'),('mask','operator_mask_invalid','mask_gdf'),('crs','operator_crs_invalid','')])
def test_voronoi_diagnostic_binds_frozen_node_and_blocks_save(monkeypatch,language,problem,reason,param):
    import workflow_engine
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime import WorkflowExecutionError
    left,right=voronoi_inputs()
    if problem=='source':left[left.geometry.name]=[Point(0,0)]*2
    elif problem=='mask':right[right.geometry.name]=[Point(0,0)]
    else:right=right.set_crs(None,allow_override=True)
    lookup=workflow_engine.get_operator
    def guarded(name):
        if name=='save':raise AssertionError('invalid voronoi input reached save')
        return lookup(name)
    monkeypatch.setattr(workflow_engine,'get_operator',guarded)
    tasks=[{'id':'PRIVATE','operator':'voronoi','params':{'input_gdf':left,'mask_gdf':right,'id_field':'station'},'depends_on':[]},
        {'id':'save','operator':'save','params':{'input_df':{'$ref':'PRIVATE'}},'depends_on':['PRIVATE']}]
    with pytest.raises(WorkflowExecutionError)as caught:GeoPythonWorkflowRunner(language=language).execute({'tasks':tasks})
    assert caught.value.error_code=='INVALID_PARAMS'
    assert caught.value.diagnostic=={'schema_version':'addp.execution-failure/v3','stage':'execution','reason':reason,
        'operator':{'node_index':1,'operator_id':'voronoi',**({'parameter':param}if param else{})}}
    assert 'PRIVATE'not in json.dumps(caught.value.diagnostic)


def test_voronoi_binding_is_independent_of_native_cell_order(monkeypatch):
    original=shapely.voronoi_polygons;calls=[]
    def shuffled(points,**kwargs):
        calls.append(kwargs)
        result=original(points,**kwargs)
        return shapely.GeometryCollection(list(reversed(result.geoms)))
    monkeypatch.setattr(shapely,'voronoi_polygons',shuffled)
    left,right=voronoi_inputs()
    assert_voronoi_polygons(get_operator('voronoi')(left,right,'station'),left,[0,1],[box(-2,-2,2,2),box(2,-2,6,2)])
    assert len(calls)==1 and calls[0]['tolerance']==0 and 'ordered'not in calls[0]
    left,right=voronoi_inputs(masks=[box(100,0,101,1)])
    assert_voronoi_polygons(get_operator('voronoi')(left,right,'station'),left,[1],[box(100,0,101,1)])


@pytest.mark.parametrize('stage',['union_all','voronoi_polygons','intersection'])
def test_voronoi_native_exception_remains_execution_failed(monkeypatch,stage):
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime import WorkflowExecutionError
    left,right=voronoi_inputs()
    def fail(*args,**kwargs):raise shapely.errors.GEOSException('PRIVATE numerical failure')
    monkeypatch.setattr(shapely,stage,fail)
    with pytest.raises(WorkflowExecutionError)as caught:GeoPythonWorkflowRunner().execute({'tasks':[
        {'id':'regions','operator':'voronoi','params':{'input_gdf':left,'mask_gdf':right,'id_field':'station'},'depends_on':[]}]})
    assert caught.value.error_code=='EXECUTION_FAILED'and caught.value.diagnostic is None


@pytest.mark.parametrize('fault',['count','same_station','missing_station','invalid_polygon','non_polygon'])
def test_voronoi_native_partition_or_binding_failure_is_not_input_error(monkeypatch,fault):
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime import WorkflowExecutionError
    left,right=voronoi_inputs()
    first=box(-2,-2,2,2);last=box(2,-2,6,2)
    cells={'count':[first], 'same_station':[first,first], 'missing_station':[box(-10,-10,-9,-9),last],
        'invalid_polygon':[Polygon([(0,0),(2,2),(0,2),(2,0),(0,0)]),last], 'non_polygon':[Point(0,0),last]}[fault]
    monkeypatch.setattr(shapely,'voronoi_polygons',lambda *args,**kwargs:shapely.GeometryCollection(cells))
    with pytest.raises(WorkflowExecutionError)as caught:GeoPythonWorkflowRunner().execute({'tasks':[
        {'id':'regions','operator':'voronoi','params':{'input_gdf':left,'mask_gdf':right,'id_field':'station'},'depends_on':[]}]})
    assert caught.value.error_code=='EXECUTION_FAILED'and caught.value.diagnostic is None


@pytest.mark.parametrize('crs',[3857,2263])
def test_voronoi_native_gpkg_dag_reload_independent_ogr_preserves_source_identity(tmp_path,crs):
    from osgeo import ogr
    left,right=voronoi_inputs([Point(4,0),Point(0,0)])
    left=left.set_crs(crs,allow_override=True);right=right.set_crs(crs,allow_override=True)
    left['station']=pd.Series([2**60+1,2**60+3],dtype='Int64')
    left['grade']=pd.Series([None,2**53+1],dtype='Int64')
    left['enabled']=pd.Series([None,False],dtype='boolean')
    left['geometry']=pd.Series(['station-east','station-west'],dtype='string')
    left['fid']=pd.Series([None,7],dtype='Int64')
    conn={'engine_type':'nfs','mount_path':str(tmp_path)}
    for name,frame in [('points',left),('mask',right)]:get_operator('save')(input_df=frame,connection_info=conn,path=name+'.gpkg',mode='fail')
    before={n:(tmp_path/(n+'.gpkg')).read_bytes()for n in ('points','mask')}
    tasks=[{'id':'regions','operator':'voronoi','params':{'input_gdf':{'$ref':'points'},'mask_gdf':{'$ref':'mask'},'id_field':'station'},'depends_on':['points','mask']}]
    tasks.extend({'id':n,'operator':'load','params':{'connection_info':conn,'path':n+'.gpkg'},'depends_on':[]}for n in ('points','mask'))
    tasks.append({'id':'save','operator':'save','params':{'input_df':{'$ref':'regions'},'connection_info':conn,'path':'regions.gpkg','mode':'fail'},'depends_on':['regions']})
    result=execute_workflow({'tasks':tasks});assert result['status']=='success',result
    ds=ogr.Open(str(tmp_path/'regions.gpkg'));layer=ds.GetLayer(0);rows=list(layer);defn=layer.GetLayerDefn()
    assert len(rows)==2 and layer.GetSpatialRef().GetAuthorityCode(None)==str(crs)
    assert layer.GetFIDColumn().lower()!='fid'
    assert defn.GetFieldDefn(defn.GetFieldIndex('station')).GetType()==ogr.OFTInteger64
    assert defn.GetFieldDefn(defn.GetFieldIndex('enabled')).GetSubType()==ogr.OFSTBoolean
    expected=[box(2,-2,6,2),box(-2,-2,2,2)]
    for i,row in enumerate(rows):
        assert row.GetField('station')==[2**60+1,2**60+3][i]
        assert row.GetField('grade')==[None,2**53+1][i]
        assert row.GetField('enabled')==[None,False][i]
        assert row.GetField('fid')==[None,7][i]
        assert row.GetField('geometry')==['station-east','station-west'][i]
        assert row.GetFieldIndex('range_name')==-1
        assert row.GetGeometryRef().Normalize().ExportToIsoWkb()==ogr.CreateGeometryFromWkt(expected[i].wkt).Normalize().ExportToIsoWkb()
    physical_name=layer.GetGeometryColumn();ds=None
    loaded=get_operator('load')(connection_info=conn,path='regions.gpkg')
    assert loaded.geometry.name==physical_name and loaded.station.dtype=='Int64'and loaded.grade.dtype=='Int64'and loaded.enabled.dtype=='boolean'
    assert list(loaded['geometry'])==['station-east','station-west']
    for n in before:assert(tmp_path/(n+'.gpkg')).read_bytes()==before[n]


@pytest.mark.parametrize('problem',[None,'source','mask','crs'])
def test_voronoi_async_api_discovery_three_admissions_and_definition_binding(tmp_path,monkeypatch,problem):
    import api_server
    from addp_common.workflow_runtime import ExecutionRegistry
    left,right=voronoi_inputs()
    if problem=='source':left[left.geometry.name]=[Point(0,0)]*2
    elif problem=='mask':right[right.geometry.name]=[Point(0,0)]
    elif problem=='crs':right=right.set_crs(3395,allow_override=True)
    for name,frame in [('points',left),('mask',right)]:frame.to_file(tmp_path/(name+'.gpkg'),driver='GPKG',index=False)
    conn={'engine_type':'nfs','mount_path':str(tmp_path)}
    tasks=[{'id':'PRIVATE_REGION','operator':'voronoi','params':{'input_gdf':{'$ref':'points'},'mask_gdf':{'$ref':'mask'},'id_field':'station'},'depends_on':['points','mask']}]
    tasks.extend({'id':n,'operator':'load','params':{'connection_info':conn,'path':n+'.gpkg'},'depends_on':[]}for n in ('points','mask'))
    monkeypatch.setattr(api_server,'executions',ExecutionRegistry())
    with api_server.app.test_client()as client:
        spec=next(s for s in client.get('/api/operators').get_json()['operators']if s['id']=='voronoi')
        assert{p['name']for p in spec['parameters']}=={'input_gdf','mask_gdf','id_field'}and all(p['required']for p in spec['parameters'])
        assert client.post('/api/operators/voronoi/invoke',json={'params':{}}).status_code==403
        response=client.post('/api/workflow',json={'workflow_def':{'tasks':tasks},'runtime':{'tenant_id':7,'execution_authorization':{'id':1,'effects':['read']}}})
        assert response.status_code==202
        deadline=time.monotonic()+5
        while time.monotonic()<deadline:
            observed=client.get('/api/executions/'+response.get_json()['execution_id']);assert observed.status_code==200;state=observed.get_json()
            if state['status']in('success','failed'):break
            time.sleep(.01)
        if problem is None:assert state['status']=='success'and len(json.loads(state['result'])['features'])==2,state
        else:
            reason,param={'source':('operator_input_invalid','input_gdf'),'mask':('operator_mask_invalid','mask_gdf'),'crs':('operator_crs_invalid','')}[problem]
            assert state['status']=='failed'and state['error_code']=='INVALID_PARAMS',state
            assert state['diagnostic']=={'schema_version':'addp.execution-failure/v3','stage':'execution','reason':reason,
                'operator':{'node_index':1,'operator_id':'voronoi',**({'parameter':param}if param else{})}}
            assert 'PRIVATE'not in json.dumps(state['diagnostic'])


def buffer_source(geometry=None,crs=3857):
    geometry=[Point(0,0),LineString([(10,0),(12,0)]),box(20,0,24,4)] if geometry is None else geometry
    source=gpd.GeoDataFrame({'label':pd.Series([f'row-{i}'for i in range(len(geometry))],dtype='string'),
        'grade':pd.Series([None if i==0 else 2**53+1+i for i in range(len(geometry))],dtype='Int64')},geometry=geometry,crs=crs).rename_geometry('shape')
    source['geometry']=pd.Series(['business']*len(source),dtype='string')
    source.index=pd.Index([8]*len(source),name='source-index')
    return source


def test_buffer_strict_boolean_distance_rejected():
    with pytest.raises(ValueError):get_operator('buffer')(buffer_source(),True)


@pytest.mark.parametrize('distance',[2.,0.,-1.,-100.])
@pytest.mark.parametrize('crs',[3857,2263])
def test_buffer_projected_native_units_and_zero_negative_empty_rows(distance,crs):
    source=buffer_source(crs=crs);before=source.copy();result=get_operator('buffer')(source,distance,resolution=1)
    expected=source.geometry.buffer(distance,resolution=1)
    assert result.geometry.to_wkb().tolist()==expected.to_wkb().tolist()
    assert result.index.equals(source.index)and len(result)==len(source)and result.crs==source.crs
    pd.testing.assert_frame_equal(result.drop(columns='shape'),source.drop(columns='shape'))
    assert_geodataframe_equal(source,before)
    if distance<=0:assert result.geometry.iloc[0].is_empty and result.geometry.iloc[1].is_empty
    if distance==-100:assert result.geometry.is_empty.all()
    if distance==2:assert result.geometry.iloc[0].bounds==(-2.,-2.,2.,2.)


@pytest.mark.parametrize('name',['distance','shape'])
def test_buffer_plain_distance_and_active_geometry_field_names_are_preserved(name):
    source=buffer_source().rename_geometry(name)if name!='shape'else buffer_source()
    if name!='distance':source['distance']=[17.,18.,19.]
    before=source.copy();result=get_operator('buffer')(source,1)
    assert result.geometry.name==name and result.columns.equals(source.columns)
    if name!='distance':assert result['distance'].tolist()==[17.,18.,19.]
    assert_geodataframe_equal(source,before)


@pytest.mark.parametrize('operator',['buffer','batch_buffer'])
@pytest.mark.parametrize('problem',['missing','empty','invalid','z','m','infinite','duplicate_columns','extra_geometry','no_crs','geographic','geocentric'])
def test_buffer_unified_source_admission_and_no_automatic_repair(operator,problem):
    source=buffer_source([Point(0,0)])
    if problem=='duplicate_columns':source.columns=['shape','shape','shape','shape']
    elif problem=='extra_geometry':source['second_shape']=source.geometry.copy()
    elif problem in ('no_crs','geographic','geocentric'):source=source.set_crs({'no_crs':None,'geographic':4326,'geocentric':4978}[problem],allow_override=True)
    else:source['shape']=[{'missing':None,'empty':Point(),'invalid':Polygon([(0,0),(2,2),(0,2),(2,0),(0,0)]),'z':Point(0,0,1),'m':shapely.from_wkt('POINT M (0 0 1)'),'infinite':Point(float('inf'),0)}[problem]]
    before=source.copy()
    with pytest.raises(ValueError)as caught:get_operator(operator)(source,1 if operator=='buffer'else[1])
    assert caught.value.code==('crs'if problem in ('no_crs','geographic','geocentric')else'source')
    if problem=='duplicate_columns':pd.testing.assert_frame_equal(pd.DataFrame(source),pd.DataFrame(before))
    else:assert_geodataframe_equal(source,before)


@pytest.mark.parametrize('value',[True,False,None,'1',[1],float('nan'),float('inf'),float('-inf'),10**500])
def test_buffer_finite_scalar_distance_is_validated_even_for_empty_source(value):
    source=buffer_source().iloc[:0]
    with pytest.raises(ValueError)as caught:get_operator('buffer')(source,value)
    assert caught.value.code=='distance'


@pytest.mark.parametrize('operator',['buffer','batch_buffer'])
@pytest.mark.parametrize('resolution',[True,False,None,0,-1,1.5,'2'])
def test_buffer_positive_integral_quarter_circle_resolution(operator,resolution):
    with pytest.raises(ValueError)as caught:get_operator(operator)(buffer_source().iloc[:0],1 if operator=='buffer'else[],resolution)
    assert caught.value.code=='resolution'


def test_buffer_single_and_multiple_distances_share_one_admission_and_kernel(monkeypatch):
    import operators.data_operations as operations
    import operators.geometric_operators as geometry
    source=buffer_source();calls=[];original=geometry._prepare_projected_xy_source
    def observed(*args,**kwargs):calls.append(1);return original(*args,**kwargs)
    monkeypatch.setattr(operations,'_prepare_projected_xy_source',observed)
    result=get_operator('batch_buffer')(source,[2,0,-1,2],resolution=1)
    assert len(calls)==1 and len(result)==4*len(source)
    for i,distance in enumerate([2,0,-1,2]):
        expected=get_operator('buffer')(source,distance,resolution=1)
        assert result.geometry.iloc[i*3:i*3+3].to_wkb().tolist()==expected.geometry.to_wkb().tolist()
    assert result['distance'].tolist()==[2.]*3+[0.]*3+[-1.]*3+[2.]*3


@pytest.mark.parametrize('operator',['buffer','batch_buffer'])
@pytest.mark.parametrize('case',['empty_source','empty_distances','bad_crs'])
def test_buffer_empty_schema_and_crs_policy_are_consistent(operator,case):
    source=buffer_source().iloc[:0]if case=='empty_source'else buffer_source()
    if case=='bad_crs':source=source.iloc[:0].set_crs(None,allow_override=True)
    value=1 if operator=='buffer'else[]if case=='empty_distances'else[1]
    if case=='bad_crs':
        with pytest.raises(ValueError)as caught:get_operator(operator)(source,value)
        assert caught.value.code=='crs'
    else:
        result=get_operator(operator)(source,value)
        assert result.geometry.name=='shape'and result.crs==source.crs
        if case=='empty_source'or(operator=='batch_buffer'and case=='empty_distances'):assert result.empty


def test_buffer_point_resolution_is_number_of_segments_per_quarter_circle():
    result=get_operator('buffer')(buffer_source([Point(0,0)]),2,resolution=1)
    assert len(result.geometry.iloc[0].exterior.coords)==5
    assert result.geometry.iloc[0].area==8 and result.geometry.iloc[0].bounds==(-2,-2,2,2)


@pytest.mark.parametrize('operator',['buffer','batch_buffer'])
def test_buffer_native_calculation_exception_is_not_input_diagnostic(monkeypatch,operator):
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime import WorkflowExecutionError
    source=buffer_source()
    def fail(*args,**kwargs):raise shapely.errors.GEOSException('PRIVATE numerical failure')
    monkeypatch.setattr(gpd.GeoSeries,'buffer',fail)
    params={'input_gdf':source,**({'distance':1}if operator=='buffer'else{'distances':[1]})}
    with pytest.raises(WorkflowExecutionError)as caught:GeoPythonWorkflowRunner().execute({'tasks':[{'id':'buffers','operator':operator,'params':params,'depends_on':[]}]})
    assert caught.value.error_code=='EXECUTION_FAILED'and caught.value.diagnostic is None


@pytest.mark.parametrize('fault',['null','point','invalid','non_finite','length','index','positive_empty'])
def test_buffer_invalid_native_result_is_rejected_instead_of_published(monkeypatch,fault):
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime import WorkflowExecutionError
    source=buffer_source([Point(0,0)])
    value={'null':None,'point':Point(0,0),'invalid':Polygon([(0,0),(2,2),(0,2),(2,0),(0,0)]),'non_finite':Polygon([(0,0),(float('inf'),0),(0,1)]),'length':box(-1,-1,1,1),'index':box(-1,-1,1,1),'positive_empty':Polygon()}[fault]
    values=[]if fault=='length'else[value]
    result=gpd.GeoSeries(values,crs=source.crs,index=[]if fault=='length'else['wrong']if fault=='index'else source.index)
    monkeypatch.setattr(gpd.GeoSeries,'buffer',lambda *a,**kw:result)
    with pytest.raises(WorkflowExecutionError)as caught:GeoPythonWorkflowRunner().execute({'tasks':[{'id':'buffer','operator':'buffer','params':{'input_gdf':source,'distance':1},'depends_on':[]}]})
    assert caught.value.error_code=='EXECUTION_FAILED'and caught.value.diagnostic is None


@pytest.mark.parametrize('operator',['buffer','batch_buffer'])
@pytest.mark.parametrize('language',['zh-cn','en'])
@pytest.mark.parametrize('problem',['source','crs','distance','resolution'])
def test_buffer_unified_runtime_diagnostics_are_closed_and_block_save(operator,language,problem,monkeypatch):
    import workflow_engine
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime import WorkflowExecutionError
    source=buffer_source();value=1 if operator=='buffer'else[1];resolution=16
    if problem=='source':source['shape']=[None]*len(source)
    elif problem=='crs':source=source.set_crs(4326,allow_override=True)
    elif problem=='distance':value=True if operator=='buffer'else[True]
    else:resolution=True
    reason,param={'source':('operator_input_invalid','input_gdf'),'crs':('operator_crs_invalid',''),'distance':('operator_number_invalid','distance')if operator=='buffer'else('operator_number_array_invalid','distances'),'resolution':('operator_positive_integer_required','resolution')}[problem]
    lookup=workflow_engine.get_operator
    def guarded(name):
        if name=='save':raise AssertionError('buffer input error reached save')
        return lookup(name)
    monkeypatch.setattr(workflow_engine,'get_operator',guarded)
    params={'input_gdf':source,'resolution':resolution,**({'distance':value}if operator=='buffer'else{'distances':value})}
    tasks=[{'id':'PRIVATE','operator':operator,'params':params,'depends_on':[]},{'id':'save','operator':'save','params':{'input_df':{'$ref':'PRIVATE'}},'depends_on':['PRIVATE']}]
    with pytest.raises(WorkflowExecutionError)as caught:GeoPythonWorkflowRunner(language=language).execute({'tasks':tasks})
    assert caught.value.error_code=='INVALID_PARAMS'
    assert caught.value.diagnostic=={'schema_version':'addp.execution-failure/v3','stage':'execution','reason':reason,'operator':{'node_index':1,'operator_id':operator,**({'parameter':param}if param else{})}}
    assert 'PRIVATE'not in json.dumps(caught.value.diagnostic)


@pytest.mark.parametrize('operator',['buffer','batch_buffer'])
def test_buffer_native_save_reload_keeps_empty_polygons_and_all_source_attributes(tmp_path,operator):
    from osgeo import ogr
    source=buffer_source();source['enabled']=pd.array([None,True,False],dtype='boolean')
    before=source.copy();params={'distance':-1}if operator=='buffer'else{'distances':[0,-1,1]}
    result=get_operator(operator)(source,resolution=1,**params);computed=result.geometry.to_wkb(byte_order=1,flavor='iso').tolist()
    conn={'engine_type':'nfs','mount_path':str(tmp_path)}
    get_operator('save')(input_df=result,connection_info=conn,path='buffer.gpkg',mode='fail')
    ds=ogr.Open(str(tmp_path/'buffer.gpkg'));layer=ds.GetLayer(0);rows=list(layer);schema=layer.GetLayerDefn()
    assert len(rows)==len(result)and layer.GetSpatialRef().GetAuthorityCode(None)=='3857'
    assert schema.GetFieldDefn(schema.GetFieldIndex('grade')).GetType()==ogr.OFTInteger64
    assert schema.GetFieldDefn(schema.GetFieldIndex('enabled')).GetSubType()==ogr.OFSTBoolean
    for i,row in enumerate(rows):
        geom=row.GetGeometryRef();assert geom is not None
        assert bytes(geom.ExportToIsoWkb())==computed[i]
        assert bool(geom.IsEmpty())==result.geometry.iloc[i].is_empty
        j=i%3;assert row.GetField('label')==f'row-{j}'and row.GetField('geometry')=='business'
        assert row.GetField('grade')==[None,2**53+2,2**53+3][j]
        assert row.GetField('enabled')==[None,True,False][j]
        if operator=='batch_buffer':assert row.GetField('distance')==result['distance'].iloc[i]
    physical_name=layer.GetGeometryColumn();ds=None
    loaded=get_operator('load')(connection_info=conn,path='buffer.gpkg')
    assert loaded.geometry.name==physical_name and loaded.geometry.to_wkb(byte_order=1,flavor='iso').tolist()==computed
    assert str(loaded.grade.dtype)=='Int64'and str(loaded.enabled.dtype)=='boolean'
    assert_geodataframe_equal(source,before)


def test_buffer_and_multi_buffer_discovery_explicit_types_defaults_and_projected_policy():
    for name in ['buffer','batch_buffer']:
        spec=next(s for s in list_operators()if s['id']==name);params={p['name']:p for p in spec['parameters']}
        assert spec['execution_modes']==['workflow']and spec['effects']==['read']
        assert params['resolution']['type']=='integer'and params['resolution']['default']==16
        assert '四分之一圆'in params['resolution']['description']
        assert len(spec['output_ports'])==1 and spec['output_ports'][0]['type']=='geodataframe'
        assert '投影'in spec['detailed_description']['overview']


@pytest.mark.parametrize('crs', [3857, 2263])
def test_simplify_native_units_zero_collinear_and_source_rows_are_preserved(crs):
    source = buffer_source([LineString([(0, 0), (1, .5), (2, 0)]), Point(10, 10), box(20, 0, 24, 4)], crs)
    source['enabled'] = pd.array([None, True, False], dtype='boolean')
    before = source.copy(deep=True)
    result = get_operator('simplify')(source, .6)
    assert list(result.geometry.iloc[0].coords) == [(0., 0.), (2., 0.)]
    assert result.geometry.iloc[1].equals_exact(Point(10, 10), 0)
    assert result.geometry.iloc[2].equals(source.geometry.iloc[2])
    assert result.geometry.name == 'shape' and result.crs == source.crs
    assert result.index.equals(source.index) and result.columns.equals(source.columns)
    pd.testing.assert_frame_equal(result.drop(columns='shape'), source.drop(columns='shape'))
    assert_geodataframe_equal(source, before)
    collinear = buffer_source([LineString([(0, 0), (1, 0), (2, 0)])], crs)
    assert list(get_operator('simplify')(collinear, 0).geometry.iloc[0].coords) == [(0., 0.), (2., 0.)]


@pytest.mark.parametrize('value', [True, False, None, '1', [1], np.array([1]), -1, float('nan'), float('inf'), -float('inf'), 10 ** 500])
def test_simplify_tolerance_rejects_nonfinite_nonreal_and_negative_even_empty_source(value):
    from operators.geometric_operators import SimplifyInputError
    source = buffer_source().iloc[:0]
    with pytest.raises(SimplifyInputError) as caught:
        get_operator('simplify')(source, value)
    assert caught.value.code == 'tolerance'


@pytest.mark.parametrize('value', [0, 1, 'true', 'false', None, np.bool_(True), np.bool_(False)])
def test_simplify_preserve_topology_requires_python_bool_even_empty_source(value):
    from operators.geometric_operators import SimplifyInputError
    with pytest.raises(SimplifyInputError) as caught:
        get_operator('simplify')(buffer_source().iloc[:0], 0, value)
    assert caught.value.code == 'preserve_topology'


@pytest.mark.parametrize('problem', ['not_gdf', 'no_active_geometry', 'duplicate_columns', 'extra_geometry', 'missing', 'empty', 'invalid', 'z', 'm', 'infinite', 'no_crs', 'geographic', 'geocentric'])
def test_simplify_source_admission_is_explicit_without_repair(problem):
    from operators.geometric_operators import SimplifyInputError
    source = buffer_source([Point(0, 0)])
    if problem == 'not_gdf':
        source = pd.DataFrame({'a': [1]})
    elif problem == 'no_active_geometry':
        source = gpd.GeoDataFrame({'a': [1]})
    elif problem == 'duplicate_columns':
        source.columns = ['same'] * len(source.columns)
    elif problem == 'extra_geometry':
        source['second_shape'] = source.geometry.copy()
    elif problem in ('no_crs', 'geographic', 'geocentric'):
        source = source.set_crs({'no_crs': None, 'geographic': 4326, 'geocentric': 4978}[problem], allow_override=True)
    else:
        source['shape'] = [{'missing': None, 'empty': Point(),
            'invalid': Polygon([(0, 0), (2, 2), (0, 2), (2, 0), (0, 0)]),
            'z': Point(0, 0, 1), 'm': shapely.from_wkt('POINT M (0 0 1)'),
            'infinite': Point(float('inf'), 0)}[problem]]
    before = source.copy(deep=True)
    with pytest.raises(SimplifyInputError) as caught:
        get_operator('simplify')(source, 1)
    assert caught.value.code == ('crs' if problem in ('no_crs', 'geographic', 'geocentric') else 'source')
    pd.testing.assert_frame_equal(pd.DataFrame(source), pd.DataFrame(before))


def test_simplify_empty_batch_keeps_schema_but_still_requires_projected_crs():
    from operators.geometric_operators import SimplifyInputError
    source = buffer_source().iloc[:0]
    assert_geodataframe_equal(get_operator('simplify')(source, 0, False), source)
    for crs in (None, 4326):
        with pytest.raises(SimplifyInputError) as caught:
            get_operator('simplify')(source.set_crs(crs, allow_override=True), 0)
        assert caught.value.code == 'crs'


def test_simplify_false_collapse_keeps_empty_polygon_row_through_real_geopackage(tmp_path):
    from osgeo import ogr
    source = buffer_source([Polygon([(0, 0), (2, 0), (1, 1), (0, 0)]), box(10, 0, 14, 4)])
    source['enabled'] = pd.array([None, True], dtype='boolean')
    before = source.copy(deep=True)
    assert not get_operator('simplify')(source, 100).geometry.is_empty.any()
    result = get_operator('simplify')(source, 100, False)
    assert len(result) == 2 and result.geometry.is_empty.all() and not result.geometry.isna().any()
    assert result.index.equals(source.index) and result.crs == source.crs
    pd.testing.assert_frame_equal(result.drop(columns='shape'), source.drop(columns='shape'))
    computed = result.geometry.to_wkb(byte_order=1, flavor='iso').tolist()
    conn = {'engine_type': 'nfs', 'mount_path': str(tmp_path)}
    get_operator('save')(input_df=result, connection_info=conn, path='simplified.gpkg', mode='fail')
    ds = ogr.Open(str(tmp_path / 'simplified.gpkg')); layer = ds.GetLayer(0)
    rows = list(layer)
    assert len(rows) == 2 and layer.GetSpatialRef().GetAuthorityCode(None) == '3857'
    for i, row in enumerate(rows):
        geometry = row.GetGeometryRef()
        assert geometry is not None and geometry.IsEmpty()
        assert bytes(geometry.ExportToIsoWkb()) == computed[i]
        assert row.GetField('label') == f'row-{i}' and row.GetField('geometry') == 'business'
        assert row.GetField('grade') == [None, 2 ** 53 + 2][i]
        assert row.GetField('enabled') == [None, True][i]
    physical_name = layer.GetGeometryColumn(); ds = None
    loaded = get_operator('load')(connection_info=conn, path='simplified.gpkg')
    assert loaded.geometry.name == physical_name and loaded.geometry.to_wkb(byte_order=1, flavor='iso').tolist() == computed
    assert loaded.geometry.is_empty.all() and not loaded.geometry.isna().any()
    assert str(loaded.grade.dtype) == 'Int64' and str(loaded.enabled.dtype) == 'boolean'
    assert_geodataframe_equal(source, before)


@pytest.mark.parametrize('language', ['zh-cn', 'en'])
@pytest.mark.parametrize('problem', ['source', 'crs', 'tolerance', 'preserve_topology'])
def test_simplify_runtime_diagnostic_binds_frozen_position_and_blocks_save(monkeypatch, language, problem):
    import workflow_engine
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime import WorkflowExecutionError
    from operators.geometric_operators import SimplifyInputError
    source = buffer_source(); tolerance = 1; preserve_topology = True
    if problem == 'source': source['shape'] = [None] * len(source)
    elif problem == 'crs': source = source.set_crs(4326, allow_override=True)
    elif problem == 'tolerance': tolerance = -1
    else: preserve_topology = 1
    lookup = workflow_engine.get_operator
    calls = []
    def guarded(name):
        calls.append(name)
        if name == 'save': raise AssertionError('simplify admission reached save')
        if name == 'load': return lambda **params: source
        return lookup(name)
    monkeypatch.setattr(workflow_engine, 'get_operator', guarded)
    tasks = [
        {'id': 'PRIVATE', 'operator': 'simplify', 'params': {'input_gdf': {'$ref': 'load'}, 'tolerance': tolerance, 'preserve_topology': preserve_topology}, 'depends_on': ['load']},
        {'id': 'load', 'operator': 'load', 'params': {}, 'depends_on': []},
        {'id': 'saved', 'operator': 'save', 'params': {'input_df': {'$ref': 'PRIVATE'}}, 'depends_on': ['PRIVATE']},
    ]
    with pytest.raises(WorkflowExecutionError) as caught:
        GeoPythonWorkflowRunner(language=language).execute({'tasks': tasks})
    reason, parameter = {'source': ('operator_input_invalid', 'input_gdf'), 'crs': ('operator_crs_invalid', ''),
        'tolerance': ('operator_nonnegative_number_required', 'tolerance'), 'preserve_topology': ('operator_boolean_required', 'preserve_topology')}[problem]
    assert caught.value.error_code == 'INVALID_PARAMS' and calls == ['load', 'simplify']
    assert caught.value.task_order == ['load', 'PRIVATE']
    assert caught.value.diagnostic == {'schema_version': 'addp.execution-failure/v3', 'stage': 'execution', 'reason': reason,
        'operator': {'node_index': 1, 'operator_id': 'simplify', **({'parameter': parameter} if parameter else {})}}
    assert SimplifyInputError(problem).message(language) in str(caught.value)
    assert 'PRIVATE' not in json.dumps(caught.value.diagnostic)


@pytest.mark.parametrize('fault', ['exception', 'missing', 'nonfinite', 'length', 'index'])
def test_simplify_native_calculation_failures_never_become_input_diagnostics(monkeypatch, fault):
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime import WorkflowExecutionError
    source = buffer_source([LineString([(0, 0), (1, .5), (2, 0)])])
    def failed(*args, **kwargs):
        if fault == 'exception': raise shapely.errors.GEOSException('PRIVATE native failure')
        value = None if fault == 'missing' else LineString([(0, 0), (float('inf'), 0)]) if fault == 'nonfinite' else source.geometry.iloc[0]
        return gpd.GeoSeries([] if fault == 'length' else [value], crs=source.crs,
                             index=[] if fault == 'length' else ['wrong'] if fault == 'index' else source.index)
    monkeypatch.setattr(gpd.GeoSeries, 'simplify', failed)
    with pytest.raises(WorkflowExecutionError) as caught:
        GeoPythonWorkflowRunner().execute({'tasks': [{'id': 'simplify', 'operator': 'simplify',
            'params': {'input_gdf': source, 'tolerance': 1}, 'depends_on': []}]})
    assert caught.value.error_code == 'EXECUTION_FAILED' and caught.value.diagnostic is None


@pytest.mark.parametrize('operator', ['convex_hull', 'minimum_rotated_rectangle'])
@pytest.mark.parametrize('crs', [3857, 2263])
def test_enclosing_constructions_exact_degenerate_and_rotated_native_units(operator, crs):
    rotated = Polygon([(0, 0), (2, 2), (1, 3), (-1, 1), (0, 0)])
    tiny = 2 ** -40
    source = buffer_source([
        Point(8, 9), shapely.MultiPoint([(8, 9), (8, 9)]),
        shapely.MultiPoint([(4, 4), (0, 0)]),
        MultiLineString([[(2, 2), (0, 0), (1, 1)], [(4, 4), (3, 3)]]),
        shapely.MultiPoint(list(rotated.exterior.coords)[:-1]),
        shapely.MultiPoint([(0, 0), (1, 0), (0, tiny)]),
    ], crs)
    before = source.copy(deep=True)
    result = get_operator(operator)(source)
    for i in (0, 1): assert result.geometry.iloc[i].equals_exact(Point(8, 9), 0)
    for i in (2, 3):
        line = result.geometry.iloc[i]
        assert line.geom_type == 'LineString' and set(line.coords) == {(0., 0.), (4., 4.)}
        assert len(line.coords) == 2
    assert result.geometry.iloc[4].equals(rotated)
    assert result.geometry.iloc[4].area == 4
    assert result.geometry.iloc[5].geom_type == 'Polygon'
    assert result.geometry.iloc[5].area == (tiny / 2 if operator == 'convex_hull' else tiny)
    assert result.geometry.name == 'shape' and result.crs == source.crs
    assert result.index.equals(source.index) and result.columns.equals(source.columns)
    pd.testing.assert_frame_equal(result.drop(columns='shape'), source.drop(columns='shape'))
    assert_geodataframe_equal(source, before)


@pytest.mark.parametrize('operator', ['convex_hull', 'minimum_rotated_rectangle'])
def test_enclosing_constructions_each_row_combines_parts_and_collection_without_merging_rows(operator):
    triangle = Polygon([(0, 0), (4, 0), (0, 4), (0, 0)])
    concave = Polygon([(0, 0), (4, 0), (4, 1), (1, 1), (1, 4), (0, 4), (0, 0)])
    with_hole = Polygon([(0, 0), (4, 0), (4, 4), (0, 4), (0, 0)], [[(2, 2), (3, 2), (3, 3), (2, 3), (2, 2)]])
    source = buffer_source([
        MultiLineString([[(0, 0), (4, 0)], [(0, 4), (0, 0)]]),
        shapely.GeometryCollection([Point(0, 0), LineString([(4, 0), (0, 4)]), Point(0, 0)]),
        concave, with_hole, Point(100, 100),
    ])
    before = source.copy(deep=True); result = get_operator(operator)(source)
    assert len(result) == 5 and result.geometry.iloc[4].equals_exact(Point(100, 100), 0)
    for i in (0, 1):
        assert result.geometry.iloc[i].area == (8 if operator == 'convex_hull' else 16)
        assert result.geometry.iloc[i].covers(triangle)
        if operator == 'convex_hull': assert result.geometry.iloc[i].equals(triangle)
    assert result.geometry.iloc[2].area == (11.5 if operator == 'convex_hull' else 16)
    assert result.geometry.iloc[2].covers(concave)
    assert result.geometry.iloc[3].equals(box(0, 0, 4, 4))
    assert len(result.geometry.iloc[3].interiors) == 0
    assert_geodataframe_equal(source, before)


@pytest.mark.parametrize('operator', ['envelope', 'centroid', 'convex_hull', 'minimum_rotated_rectangle'])
@pytest.mark.parametrize('problem', ['not_gdf', 'no_active', 'duplicate_columns', 'extra_geometry', 'missing', 'empty', 'invalid', 'z', 'm', 'infinite', 'no_crs', 'geographic', 'geocentric'])
def test_enclosing_constructions_source_and_crs_admission_without_autofix(operator, problem):
    from operators.geometric_operators import UnaryConstructionInputError
    source = buffer_source([Point(0, 0)])
    if problem == 'not_gdf': source = pd.DataFrame({'a': [1]})
    elif problem == 'no_active': source = gpd.GeoDataFrame({'a': [1]})
    elif problem == 'duplicate_columns': source.columns = ['same'] * len(source.columns)
    elif problem == 'extra_geometry': source['other_shape'] = source.geometry.copy()
    elif problem in ('no_crs', 'geographic', 'geocentric'):
        source = source.set_crs({'no_crs': None, 'geographic': 4326, 'geocentric': 4978}[problem], allow_override=True)
    else:
        source['shape'] = [{'missing': None, 'empty': Point(),
            'invalid': Polygon([(0, 0), (2, 2), (0, 2), (2, 0), (0, 0)]),
            'z': Point(0, 0, 1), 'm': shapely.from_wkt('POINT M (0 0 1)'), 'infinite': Point(float('inf'), 0)}[problem]]
    before = source.copy(deep=True)
    with pytest.raises(UnaryConstructionInputError) as caught: get_operator(operator)(source)
    assert caught.value.code == ('crs' if problem in ('no_crs', 'geographic', 'geocentric') else 'source')
    pd.testing.assert_frame_equal(pd.DataFrame(source), pd.DataFrame(before))


@pytest.mark.parametrize('operator', ['envelope', 'centroid', 'convex_hull', 'minimum_rotated_rectangle'])
def test_enclosing_constructions_empty_batch_keeps_schema_and_validates_crs(operator):
    from operators.geometric_operators import UnaryConstructionInputError
    source = buffer_source().iloc[:0]
    assert_geodataframe_equal(get_operator(operator)(source), source)
    for crs in (None, 4326, 4978):
        with pytest.raises(UnaryConstructionInputError) as caught:
            get_operator(operator)(source.set_crs(crs, allow_override=True))
        assert caught.value.code == 'crs'


@pytest.mark.parametrize('operator', ['convex_hull', 'minimum_rotated_rectangle'])
def test_enclosing_constructions_real_mixed_geopackage_roundtrip(operator, tmp_path):
    from osgeo import ogr
    source = buffer_source([Point(5, 6), MultiLineString([[(2, 2), (0, 0)], [(4, 4), (3, 3)]]),
        shapely.MultiPoint([(0, 0), (2, 2), (1, 3), (-1, 1)])])
    source['enabled'] = pd.array([None, True, False], dtype='boolean'); before = source.copy(deep=True)
    result = get_operator(operator)(source)
    assert result.geometry.geom_type.tolist() == ['Point', 'LineString', 'Polygon']
    computed = result.geometry.to_wkb(byte_order=1, flavor='iso').tolist()
    conn = {'engine_type': 'nfs', 'mount_path': str(tmp_path)}
    get_operator('save')(input_df=result, connection_info=conn, path='enclosing.gpkg', mode='fail')
    ds = ogr.Open(str(tmp_path / 'enclosing.gpkg')); layer = ds.GetLayer(0); rows = list(layer)
    assert len(rows) == 3 and layer.GetSpatialRef().GetAuthorityCode(None) == '3857'
    for i, row in enumerate(rows):
        native = row.GetGeometryRef()
        assert native is not None and bytes(native.ExportToIsoWkb()) == computed[i]
        assert row.GetField('label') == f'row-{i}' and row.GetField('geometry') == 'business'
        assert row.GetField('grade') == [None, 2 ** 53 + 2, 2 ** 53 + 3][i]
        assert row.GetField('enabled') == [None, True, False][i]
    physical_name = layer.GetGeometryColumn(); ds = None
    loaded = get_operator('load')(connection_info=conn, path='enclosing.gpkg')
    assert loaded.geometry.name == physical_name and loaded.crs == source.crs
    assert loaded.geometry.to_wkb(byte_order=1, flavor='iso').tolist() == computed
    assert str(loaded.grade.dtype) == 'Int64' and str(loaded.enabled.dtype) == 'boolean'
    assert_geodataframe_equal(source, before)


@pytest.mark.parametrize('operator', ['envelope', 'centroid', 'convex_hull', 'minimum_rotated_rectangle'])
@pytest.mark.parametrize('language', ['zh-cn', 'en'])
@pytest.mark.parametrize('problem', ['source', 'crs'])
def test_enclosing_constructions_runtime_diagnostics_bind_frozen_node_and_block_save(operator, language, problem, monkeypatch):
    import workflow_engine
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime import WorkflowExecutionError
    from operators.geometric_operators import UnaryConstructionInputError
    source = buffer_source()
    if problem == 'source': source['shape'] = [None] * len(source)
    else: source = source.set_crs(4326, allow_override=True)
    lookup = workflow_engine.get_operator; calls = []
    def guarded(name):
        calls.append(name)
        if name == 'save': raise AssertionError('enclosing input error reached save')
        if name == 'load': return lambda **params: source
        return lookup(name)
    monkeypatch.setattr(workflow_engine, 'get_operator', guarded)
    tasks = [
        {'id': 'PRIVATE', 'operator': operator, 'params': {'input_gdf': {'$ref': 'load'}}, 'depends_on': ['load']},
        {'id': 'load', 'operator': 'load', 'params': {}, 'depends_on': []},
        {'id': 'saved', 'operator': 'save', 'params': {'input_df': {'$ref': 'PRIVATE'}}, 'depends_on': ['PRIVATE']},
    ]
    with pytest.raises(WorkflowExecutionError) as caught: GeoPythonWorkflowRunner(language=language).execute({'tasks': tasks})
    reason, parameter = ('operator_input_invalid', 'input_gdf') if problem == 'source' else ('operator_crs_invalid', '')
    assert caught.value.error_code == 'INVALID_PARAMS' and calls == ['load', operator]
    assert caught.value.task_order == ['load', 'PRIVATE']
    assert caught.value.diagnostic == {'schema_version': 'addp.execution-failure/v3', 'stage': 'execution', 'reason': reason,
        'operator': {'node_index': 1, 'operator_id': operator, **({'parameter': parameter} if parameter else {})}}
    assert UnaryConstructionInputError(problem).message(language) in str(caught.value)
    assert 'PRIVATE' not in json.dumps(caught.value.diagnostic)


@pytest.mark.parametrize('operator', ['convex_hull', 'minimum_rotated_rectangle'])
@pytest.mark.parametrize('fault', ['exception', 'missing', 'empty', 'invalid', 'z', 'm', 'nonfinite', 'length', 'rank', 'collection'])
def test_enclosing_constructions_native_faults_are_execution_failures(operator, fault, monkeypatch):
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime import WorkflowExecutionError
    source = buffer_source([Point(0, 0)])
    def broken(*args, **kwargs):
        if fault == 'exception': raise shapely.errors.GEOSException('PRIVATE native failure')
        value = {'missing': None, 'empty': Point(), 'invalid': Polygon([(0, 0), (2, 2), (0, 2), (2, 0), (0, 0)]),
            'z': Point(0, 0, 1), 'm': shapely.from_wkt('POINT M (0 0 1)'), 'nonfinite': Point(float('inf'), 0),
            'length': Point(0, 0), 'rank': Point(0, 0), 'collection': shapely.GeometryCollection([Point(0, 0)])}[fault]
        if fault == 'length': return np.array([], dtype=object)
        return np.array([[value]] if fault == 'rank' else [value], dtype=object)
    monkeypatch.setattr(shapely, operator, broken)
    with pytest.raises(WorkflowExecutionError) as caught:
        GeoPythonWorkflowRunner().execute({'tasks': [{'id': 'PRIVATE', 'operator': operator, 'params': {'input_gdf': source}, 'depends_on': []}]})
    assert caught.value.error_code == 'EXECUTION_FAILED' and caught.value.diagnostic is None


@pytest.mark.parametrize('crs', [3857, 2263])
def test_axis_aligned_envelope_exact_degenerate_types_and_all_parts(crs):
    tiny = 2 ** -40
    source = buffer_source([
        Point(1, 2), shapely.MultiPoint([(1, 2), (1, 2)]),
        LineString([(5, 2), (1, 2), (3, 2)]), LineString([(3, 7), (3, 1)]),
        LineString([(1, 1), (5, 5)]), shapely.MultiPoint([(0, 0), (1, tiny)]),
        MultiLineString([[(0, 2), (1, 2)], [(4, 2), (5, 2)]]),
        shapely.GeometryCollection([Point(-1, 2), LineString([(1, 2), (3, 2)])]),
        shapely.MultiPolygon([box(0, 0, 1, 1), box(4, 4, 5, 5)]),
        Polygon([(0, 0), (6, 0), (6, 6), (0, 6), (0, 0)], [[(1, 1), (5, 1), (5, 5), (1, 5), (1, 1)]]),
        Point(100, 100),
    ], crs)
    expected_bounds = [(1, 2, 1, 2), (1, 2, 1, 2), (1, 2, 5, 2), (3, 1, 3, 7),
        (1, 1, 5, 5), (0, 0, 1, tiny), (0, 2, 5, 2), (-1, 2, 3, 2), (0, 0, 5, 5), (0, 0, 6, 6), (100, 100, 100, 100)]
    expected_types = ['Point', 'Point', 'LineString', 'LineString', 'Polygon', 'Polygon', 'LineString', 'LineString', 'Polygon', 'Polygon', 'Point']
    before = source.copy(deep=True); result = get_operator('envelope')(source)
    assert result.geometry.geom_type.tolist() == expected_types
    assert result.geometry.is_valid.all() and not result.geometry.is_empty.any()
    for value, bounds in zip(result.geometry, expected_bounds): assert value.bounds == bounds
    for row in (2, 3, 6, 7):
        xmin, ymin, xmax, ymax = expected_bounds[row]
        assert list(result.geometry.iloc[row].coords) == [(xmin, ymin), (xmax, ymax)]
    for row in (4, 5, 8, 9):
        xmin, ymin, xmax, ymax = expected_bounds[row]
        assert set(result.geometry.iloc[row].exterior.coords) == {(xmin,ymin),(xmax,ymin),(xmax,ymax),(xmin,ymax)}
        assert len(result.geometry.iloc[row].interiors) == 0
    assert result.geometry.iloc[5].area == tiny
    assert result.crs == source.crs and result.geometry.name == 'shape'
    assert result.columns.equals(source.columns) and result.index.equals(source.index)
    pd.testing.assert_frame_equal(result.drop(columns='shape'), source.drop(columns='shape'))
    assert_geodataframe_equal(source, before)


def test_axis_aligned_envelope_huge_axis_span_does_not_subtract_or_widen():
    source = buffer_source([shapely.MultiPoint([(-1e308, 2), (1e308, 2)])])
    result = get_operator('envelope')(source).geometry.iloc[0]
    assert result.geom_type == 'LineString' and result.is_valid
    assert list(result.coords) == [(-1e308, 2), (1e308, 2)]


@pytest.mark.parametrize('invalid', [LineString([(1, 1), (1, 1)]), Polygon([(0, 0), (1, 0), (2, 0), (0, 0)])])
def test_axis_aligned_envelope_invalid_degenerate_sources_are_not_legal_collapse(invalid):
    from operators.geometric_operators import UnaryConstructionInputError
    with pytest.raises(UnaryConstructionInputError) as caught: get_operator('envelope')(buffer_source([invalid]))
    assert caught.value.code == 'source'


@pytest.mark.parametrize('fault', ['exception', 'nan', 'rank', 'length', 'reversed'])
def test_axis_aligned_envelope_native_bounds_fault_is_atomic_execution_failure(fault, monkeypatch):
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime import WorkflowExecutionError
    source = buffer_source([Point(0, 0)])
    def broken(*args, **kwargs):
        if fault == 'exception': raise shapely.errors.GEOSException('PRIVATE bounds failure')
        return {'nan': np.array([[0, 0, float('nan'), 1]]), 'rank': np.zeros((1, 2, 2)),
            'length': np.zeros((0, 4)), 'reversed': np.array([[1, 0, 0, 1]])}[fault]
    monkeypatch.setattr(shapely, 'bounds', broken)
    with pytest.raises(WorkflowExecutionError) as caught:
        GeoPythonWorkflowRunner().execute({'tasks': [{'id': 'PRIVATE', 'operator': 'envelope', 'params': {'input_gdf': source}, 'depends_on': []}]})
    assert caught.value.error_code == 'EXECUTION_FAILED' and caught.value.diagnostic is None


@pytest.mark.parametrize('kind,geometry', [('point', Point(0, 0)), ('line', LineString([(0, 0), (1, 0)])), ('polygon', box(0, 0, 1, 1))])
@pytest.mark.parametrize('fault', ['exception', 'missing', 'empty', 'z', 'nonfinite', 'wrong_bounds', 'wrong_type'])
def test_axis_aligned_envelope_native_construction_faults_never_become_input_errors(kind, geometry, fault, monkeypatch):
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime import WorkflowExecutionError
    source = buffer_source([geometry])
    def broken(*args, **kwargs):
        if fault == 'exception': raise shapely.errors.GEOSException('PRIVATE construction failure')
        value = {'missing': None, 'empty': Point(), 'z': Point(0, 0, 1), 'nonfinite': Point(float('inf'), 0),
            'wrong_bounds': Point(99, 99), 'wrong_type': shapely.GeometryCollection([geometry])}[fault]
        return np.array([value], dtype=object)
    monkeypatch.setattr(shapely, {'point':'points','line':'linestrings','polygon':'box'}[kind], broken)
    with pytest.raises(WorkflowExecutionError) as caught:
        GeoPythonWorkflowRunner().execute({'tasks': [{'id': 'PRIVATE', 'operator': 'envelope', 'params': {'input_gdf': source}, 'depends_on': []}]})
    assert caught.value.error_code == 'EXECUTION_FAILED' and caught.value.diagnostic is None


def test_axis_aligned_envelope_real_mixed_geopackage_keeps_properties_and_source(tmp_path):
    from osgeo import ogr
    source = buffer_source([Point(5, 6), LineString([(4, 2), (0, 2)]), LineString([(3, 7), (3, 1)]), LineString([(0, 0), (4, 4)])])
    source['enabled'] = pd.array([None, True, False, True], dtype='boolean')
    before = source.copy(deep=True); result = get_operator('envelope')(source)
    conn = {'storage_type': 'nfs', 'mount_path': str(tmp_path)}
    get_operator('save')(input_df=result, connection_info=conn, path='envelope.gpkg', mode='fail')
    ds = ogr.Open(str(tmp_path / 'envelope.gpkg')); layer = ds.GetLayer(0); rows = list(layer)
    assert len(rows) == 4 and layer.GetSpatialRef().GetAuthorityCode(None) == '3857'
    definition = layer.GetLayerDefn()
    for field in ('label', 'geometry'):
        assert definition.GetFieldDefn(definition.GetFieldIndex(field)).GetType() == ogr.OFTString
    assert definition.GetFieldDefn(definition.GetFieldIndex('grade')).GetType() == ogr.OFTInteger64
    names = ['POINT', 'LINESTRING', 'LINESTRING', 'POLYGON']
    for i, row in enumerate(rows):
        geom = row.GetGeometryRef()
        assert geom.GetGeometryName() == names[i] and geom.IsValid()
        assert row.GetField('label') == f'row-{i}' and row.GetField('geometry') == 'business'
        assert row.GetField('grade') == [None, 2**53+2, 2**53+3, 2**53+4][i]
        assert row.GetField('enabled') == [None, True, False, True][i]
        if i == 0: assert geom.GetPoint_2D(0) == (5, 6)
        elif i == 1: assert geom.GetPoints() == [(0, 2), (4, 2)]
        elif i == 2: assert geom.GetPoints() == [(3, 1), (3, 7)]
        else: assert set(geom.GetGeometryRef(0).GetPoints()) == {(0,0),(4,0),(4,4),(0,4)}
    physical_name = layer.GetGeometryColumn(); ds = None
    loaded = get_operator('load')(connection_info=conn, path='envelope.gpkg')
    assert loaded.geometry.name == physical_name and loaded.crs == source.crs
    assert loaded.geometry.to_wkb(byte_order=1, flavor='iso').tolist() == result.geometry.to_wkb(byte_order=1, flavor='iso').tolist()
    expected = source.drop(columns='shape').reset_index(drop=True)
    # OGR String fields reload as Python string objects, not pandas StringDtype.
    for field in ('label', 'geometry'): expected[field] = expected[field].astype(object)
    pd.testing.assert_frame_equal(loaded.drop(columns=physical_name).reset_index(drop=True), expected)
    assert str(loaded.grade.dtype) == 'Int64' and str(loaded.enabled.dtype) == 'boolean'
    assert_geodataframe_equal(source, before)


@pytest.mark.parametrize('crs', [3857, 2263])
def test_centroid_analytic_native_weights_repeated_points_paths_and_parts(crs):
    holed = Polygon([(0, 0), (6, 0), (6, 2), (0, 2), (0, 0)],
                     [[(.5, .5), (4.5, .5), (4.5, 1.5), (.5, 1.5), (.5, .5)]])
    repeated_line = LineString([(0, 0), (2, 0), (0, 0), (0, 4)])
    source = buffer_source([
        Point(7, 8), shapely.MultiPoint([(0, 0), (0, 0), (6, 0)]),
        LineString([(0, 0), (2, 0), (2, 2)]), repeated_line,
        MultiLineString([[(0, 0), (2, 0)], [(0, 0), (2, 0)], [(0, 0), (0, 4)]]),
        holed, shapely.MultiPolygon([box(0, 0, 2, 2), box(10, 0, 16, 2)]),
        shapely.GeometryCollection([box(0, 0, 2, 2), box(0, 0, 2, 2), box(10, 0, 14, 2)]),
    ], crs)
    source['enabled'] = pd.array([None, True, False, None, True, False, None, True], dtype='boolean')
    before = source.copy(deep=True); result = get_operator('centroid')(source)
    # Equal point occurrences; each traversed segment length; polygon net area.
    expected = [(7., 8.), (2., 0.), (1.5, .5), (.5, 1.), (.5, 1.), (3.25, 1.), (10., 1.), (6.5, 1.)]
    assert [(point.x, point.y) for point in result.geometry] == expected
    assert result.geometry.geom_type.tolist() == ['Point'] * len(source)
    assert not source.geometry.iloc[2].covers(result.geometry.iloc[2])  # L-line centroid is off the line.
    assert not holed.covers(result.geometry.iloc[5])  # Net-area centroid lies inside the hole.
    assert result.geometry.name == source.geometry.name and result.crs == source.crs
    assert result.index.equals(source.index) and result.columns.equals(source.columns)
    pd.testing.assert_frame_equal(result.drop(columns='shape'), source.drop(columns='shape'))
    assert_geodataframe_equal(source, before)


def test_centroid_collection_uses_highest_dimension_and_can_lie_outside_polygon():
    line = LineString([(0, 0), (2, 0), (2, 2)])
    concave = Polygon([(0, 0), (4, 0), (4, 1), (1, 1), (1, 4), (0, 4), (0, 0)])
    source = buffer_source([
        shapely.GeometryCollection([Point(1000, 1000), LineString([(100, 0), (100, 10)]), box(0, 0, 4, 2)]),
        shapely.GeometryCollection([line, Point(1000, 1000)]),
        shapely.GeometryCollection([Point(0, 0), Point(0, 0), Point(6, 0)]),
        concave,
    ])
    before = source.copy(deep=True); result = get_operator('centroid')(source)
    assert [(point.x, point.y) for point in result.geometry] == [(2., 1.), (1.5, .5), (2., 0.), (19 / 14, 19 / 14)]
    assert not concave.covers(result.geometry.iloc[3])
    assert_geodataframe_equal(source, before)


def test_centroid_real_point_geopackage_keeps_every_attribute_and_nullable_type(tmp_path):
    from osgeo import ogr
    source = buffer_source([shapely.MultiPoint([(0, 0), (0, 0), (6, 0)]),
                           LineString([(0, 0), (2, 0), (2, 2)]), box(10, 0, 16, 2)])
    source['enabled'] = pd.array([None, True, False], dtype='boolean'); before = source.copy(deep=True)
    result = get_operator('centroid')(source)
    expected = [(2., 0.), (1.5, .5), (13., 1.)]
    assert [(point.x, point.y) for point in result.geometry] == expected
    computed = result.geometry.to_wkb(byte_order=1, flavor='iso').tolist()
    conn = {'engine_type': 'nfs', 'mount_path': str(tmp_path)}
    get_operator('save')(input_df=result, connection_info=conn, path='centroids.gpkg', mode='fail')
    ds = ogr.Open(str(tmp_path / 'centroids.gpkg')); layer = ds.GetLayer(0); rows = list(layer)
    assert len(rows) == 3 and layer.GetSpatialRef().GetAuthorityCode(None) == '3857'
    for i, row in enumerate(rows):
        geometry = row.GetGeometryRef()
        assert geometry.GetGeometryName() == 'POINT' and (geometry.GetX(), geometry.GetY()) == expected[i]
        assert bytes(geometry.ExportToIsoWkb()) == computed[i]
        assert row.GetField('label') == f'row-{i}' and row.GetField('geometry') == 'business'
        assert row.GetField('grade') == [None, 2 ** 53 + 2, 2 ** 53 + 3][i]
        assert row.GetField('enabled') == [None, True, False][i]
    physical_name = layer.GetGeometryColumn(); ds = None
    loaded = get_operator('load')(connection_info=conn, path='centroids.gpkg')
    assert loaded.geometry.name == physical_name and loaded.crs == source.crs
    assert loaded.geometry.to_wkb(byte_order=1, flavor='iso').tolist() == computed
    assert str(loaded.grade.dtype) == 'Int64' and str(loaded.enabled.dtype) == 'boolean'
    assert_geodataframe_equal(source, before)


@pytest.mark.parametrize('fault', ['exception', 'missing', 'empty', 'line', 'polygon', 'multipoint', 'z', 'm', 'nonfinite', 'length', 'rank'])
def test_centroid_native_faults_require_one_valid_nonempty_finite_xy_point_per_source(fault, monkeypatch):
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime import WorkflowExecutionError
    source = buffer_source([Point(0, 0)])
    def broken(*args, **kwargs):
        if fault == 'exception': raise shapely.errors.GEOSException('PRIVATE native centroid failure')
        value = {'missing': None, 'empty': Point(), 'line': LineString([(0, 0), (1, 1)]), 'polygon': box(0, 0, 1, 1),
                 'multipoint': shapely.MultiPoint([(0, 0), (1, 1)]), 'z': Point(0, 0, 1), 'm': shapely.from_wkt('POINT M (0 0 1)'),
                 'nonfinite': Point(float('inf'), 0), 'length': Point(0, 0), 'rank': Point(0, 0)}[fault]
        if fault == 'length': return np.array([], dtype=object)
        return np.array([[value]] if fault == 'rank' else [value], dtype=object)
    monkeypatch.setattr(shapely, 'centroid', broken)
    with pytest.raises(WorkflowExecutionError) as caught:
        GeoPythonWorkflowRunner().execute({'tasks': [{'id': 'PRIVATE', 'operator': 'centroid', 'params': {'input_gdf': source}, 'depends_on': []}]})
    assert caught.value.error_code == 'EXECUTION_FAILED' and caught.value.diagnostic is None


@pytest.mark.parametrize('point', [Point(float('inf'), 0), Point(100, 100, 1), shapely.from_wkt('POINT M (100 100 1)')])
def test_centroid_ignored_lower_dimension_still_obeys_whole_source_xy_admission(point):
    from operators.geometric_operators import UnaryConstructionInputError
    source = buffer_source([shapely.GeometryCollection([box(0, 0, 2, 2), point])])
    with pytest.raises(UnaryConstructionInputError) as caught:
        get_operator('centroid')(source)
    assert caught.value.code == 'source'


def test_centroid_keeps_nonunique_index_and_original_row_order():
    source = buffer_source([shapely.MultiPoint([(0, 0), (0, 0), (6, 0)]), LineString([(0, 0), (2, 0), (2, 2)])])
    source.index = pd.Index(['repeated', 'repeated'], name='source_index')
    before = source.copy(deep=True)
    result = get_operator('centroid')(source)
    assert result.index.equals(source.index) and result.index.name == 'source_index'
    assert [(value.x, value.y) for value in result.geometry] == [(2., 0.), (1.5, .5)]
    pd.testing.assert_frame_equal(result.drop(columns='shape'), source.drop(columns='shape'))
    assert_geodataframe_equal(source, before)


@pytest.mark.parametrize('crs', [3857, 2263])
def test_representative_point_selects_largest_net_area_instead_of_widest_scanline(crs):
    largest = box(0, 0, 2, 10)
    widest = box(10, 0, 19, 1)
    perforated = Polygon([(30, 0), (40, 0), (40, 10), (30, 10), (30, 0)],
                        [[(30.5, .5), (39.5, .5), (39.5, 9.5), (30.5, 9.5), (30.5, .5)]])
    net_largest = box(50, 0, 55, 5)
    source = buffer_source([shapely.MultiPolygon([largest, widest]), shapely.MultiPolygon([perforated, net_largest])], crs)
    source['enabled'] = pd.array([None, True], dtype='boolean'); before = source.copy(deep=True)
    result = get_operator('representative_point')(source)
    assert largest.area == 20 and widest.area == 9 and perforated.area == 19 and net_largest.area == 25
    for i, selected in enumerate([largest, net_largest]):
        point = result.geometry.iloc[i]
        assert point.geom_type == 'Point' and selected.contains(point) and source.geometry.iloc[i].contains(point)
    assert result.geometry.name == 'shape' and result.crs == source.crs
    assert result.index.equals(source.index) and result.columns.equals(source.columns)
    pd.testing.assert_frame_equal(result.drop(columns='shape'), source.drop(columns='shape'))
    assert_geodataframe_equal(source, before)


@pytest.mark.parametrize('reverse', [False, True])
def test_representative_point_equal_net_area_selects_first_original_part(reverse):
    parts = [box(0, 0, 2, 2), box(10, 0, 14, 1)]
    if reverse: parts.reverse()
    source = buffer_source([shapely.MultiPolygon(parts)]); before = source.copy(deep=True)
    point = get_operator('representative_point')(source).geometry.iloc[0]
    assert parts[0].contains(point) and not parts[1].contains(point)
    assert_geodataframe_equal(source, before)


def test_representative_point_is_strictly_inside_concave_holed_and_tiny_positive_area_sources():
    source = buffer_source([
        Polygon([(0, 0), (4, 0), (4, 1), (1, 1), (1, 4), (0, 4), (0, 0)]),
        Polygon([(10, 0), (16, 0), (16, 2), (10, 2), (10, 0)], [[(10.5, .5), (14.5, .5), (14.5, 1.5), (10.5, 1.5), (10.5, .5)]]),
        box(20, 0, 21, 2 ** -40),
    ])
    before = source.copy(deep=True); result = get_operator('representative_point')(source)
    assert len(result) == len(source) and not result.geometry.is_empty.any()
    assert all(shape.contains(point) for shape, point in zip(source.geometry, result.geometry))
    assert_geodataframe_equal(source, before)


@pytest.mark.parametrize('problem', ['not_gdf', 'no_active', 'duplicate_columns', 'extra_geometry', 'point', 'line', 'collection', 'missing', 'empty', 'invalid', 'z', 'm', 'nonfinite', 'no_crs', 'geographic', 'geocentric'])
def test_representative_point_source_rejection_is_not_silent_conversion(problem):
    from operators.geometric_operators import InteriorPointInputError
    source = buffer_source([box(0, 0, 2, 2)])
    if problem == 'not_gdf': source = pd.DataFrame({'a': [1]})
    elif problem == 'no_active': source = gpd.GeoDataFrame({'a': [1]})
    elif problem == 'duplicate_columns': source.columns = ['same'] * len(source.columns)
    elif problem == 'extra_geometry': source['other'] = source.geometry.copy()
    elif problem in ('no_crs', 'geographic', 'geocentric'):
        source = source.set_crs({'no_crs': None, 'geographic': 4326, 'geocentric': 4978}[problem], allow_override=True)
    else:
        source['shape'] = [{'point': Point(0, 0), 'line': LineString([(0, 0), (1, 1)]),
            'collection': shapely.GeometryCollection([box(0, 0, 2, 2)]), 'missing': None, 'empty': Polygon(),
            'invalid': Polygon([(0, 0), (2, 2), (0, 2), (2, 0), (0, 0)]),
            'z': shapely.from_wkt('POLYGON Z ((0 0 1,2 0 1,2 2 1,0 2 1,0 0 1))'),
            'm': shapely.from_wkt('POLYGON M ((0 0 1,2 0 1,2 2 1,0 2 1,0 0 1))'),
            'nonfinite': Polygon([(0, 0), (float('inf'), 0), (0, 2), (0, 0)])}[problem]]
    before = source.copy(deep=True)
    with pytest.raises(InteriorPointInputError) as caught: get_operator('representative_point')(source)
    assert caught.value.code == ('crs' if problem in ('no_crs', 'geographic', 'geocentric') else 'source')
    pd.testing.assert_frame_equal(pd.DataFrame(source), pd.DataFrame(before))


def test_representative_point_empty_batch_retains_schema_but_requires_projected_crs():
    from operators.geometric_operators import InteriorPointInputError
    source = buffer_source([box(0, 0, 1, 1)]).iloc[:0]
    assert_geodataframe_equal(get_operator('representative_point')(source), source)
    for crs in (None, 4326, 4978):
        with pytest.raises(InteriorPointInputError) as caught:
            get_operator('representative_point')(source.set_crs(crs, allow_override=True))
        assert caught.value.code == 'crs'


def test_representative_point_real_point_geopackage_roundtrip_preserves_source_attributes(tmp_path):
    from osgeo import ogr
    source = buffer_source([box(0, 0, 4, 4), shapely.MultiPolygon([box(10, 0, 11, 1), box(20, 0, 24, 4)])])
    source['enabled'] = pd.array([None, True], dtype='boolean'); before = source.copy(deep=True)
    result = get_operator('representative_point')(source)
    assert source.geometry.iloc[0].contains(result.geometry.iloc[0]) and source.geometry.iloc[1].geoms[1].contains(result.geometry.iloc[1])
    computed = result.geometry.to_wkb(byte_order=1, flavor='iso').tolist()
    conn = {'engine_type': 'nfs', 'mount_path': str(tmp_path)}
    get_operator('save')(input_df=result, connection_info=conn, path='interior.gpkg', mode='fail')
    ds = ogr.Open(str(tmp_path / 'interior.gpkg')); layer = ds.GetLayer(0); rows = list(layer)
    assert len(rows) == 2 and layer.GetSpatialRef().GetAuthorityCode(None) == '3857'
    for i, row in enumerate(rows):
        point = row.GetGeometryRef()
        assert point.GetGeometryName() == 'POINT' and bytes(point.ExportToIsoWkb()) == computed[i]
        assert row.GetField('label') == f'row-{i}' and row.GetField('geometry') == 'business'
        assert row.GetField('grade') == [None, 2 ** 53 + 2][i] and row.GetField('enabled') == [None, True][i]
    physical_name = layer.GetGeometryColumn(); ds = None
    loaded = get_operator('load')(connection_info=conn, path='interior.gpkg')
    assert loaded.geometry.name == physical_name and loaded.crs == source.crs
    assert loaded.geometry.to_wkb(byte_order=1, flavor='iso').tolist() == computed
    assert str(loaded.grade.dtype) == 'Int64' and str(loaded.enabled.dtype) == 'boolean'
    assert_geodataframe_equal(source, before)


@pytest.mark.parametrize('language', ['zh-cn', 'en'])
@pytest.mark.parametrize('problem', ['source', 'crs'])
def test_representative_point_runtime_diagnostic_is_frozen_and_blocks_save(language, problem, monkeypatch):
    import workflow_engine
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime import WorkflowExecutionError
    from operators.geometric_operators import InteriorPointInputError
    source = buffer_source([box(0, 0, 2, 2)])
    if problem == 'source': source['shape'] = [None]
    else: source = source.set_crs(4326, allow_override=True)
    lookup = workflow_engine.get_operator; calls = []
    def guarded(name):
        calls.append(name)
        if name == 'save': raise AssertionError('interior point input error reached save')
        if name == 'load': return lambda **params: source
        return lookup(name)
    monkeypatch.setattr(workflow_engine, 'get_operator', guarded)
    tasks = [
        {'id': 'PRIVATE', 'operator': 'representative_point', 'params': {'input_gdf': {'$ref': 'load'}}, 'depends_on': ['load']},
        {'id': 'load', 'operator': 'load', 'params': {}, 'depends_on': []},
        {'id': 'saved', 'operator': 'save', 'params': {'input_df': {'$ref': 'PRIVATE'}}, 'depends_on': ['PRIVATE']},
    ]
    with pytest.raises(WorkflowExecutionError) as caught: GeoPythonWorkflowRunner(language=language).execute({'tasks': tasks})
    reason, parameter = ('operator_input_invalid', 'input_gdf') if problem == 'source' else ('operator_crs_invalid', '')
    assert caught.value.error_code == 'INVALID_PARAMS' and calls == ['load', 'representative_point']
    assert caught.value.diagnostic == {'schema_version': 'addp.execution-failure/v3', 'stage': 'execution', 'reason': reason,
        'operator': {'node_index': 1, 'operator_id': 'representative_point', **({'parameter': parameter} if parameter else {})}}
    assert InteriorPointInputError(problem).message(language) in str(caught.value)
    assert 'PRIVATE' not in json.dumps(caught.value.diagnostic)


@pytest.mark.parametrize('fault', ['exception', 'missing', 'empty', 'line', 'multipoint', 'z', 'm', 'nonfinite', 'length', 'rank', 'boundary', 'hole', 'outside', 'smaller_part'])
def test_representative_point_native_candidate_failure_never_moves_or_falls_back(fault, monkeypatch):
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime import WorkflowExecutionError
    largest = Polygon([(0, 0), (6, 0), (6, 4), (0, 4), (0, 0)], [[(2, 1), (4, 1), (4, 3), (2, 3), (2, 1)]])
    smaller = box(10, 0, 11, 1); source = buffer_source([shapely.MultiPolygon([largest, smaller])]); calls = []
    def broken(values, *args, **kwargs):
        calls.append(values.copy())
        if fault == 'exception': raise shapely.errors.GEOSException('PRIVATE interior failure')
        value = {'missing': None, 'empty': Point(), 'line': LineString([(0, 0), (1, 1)]),
            'multipoint': shapely.MultiPoint([(0, 0), (1, 1)]), 'z': Point(1, 1, 1), 'm': shapely.from_wkt('POINT M (1 1 1)'),
            'nonfinite': Point(float('inf'), 0), 'length': Point(1, 1), 'rank': Point(1, 1),
            'boundary': Point(0, 2), 'hole': Point(3, 2), 'outside': Point(100, 100), 'smaller_part': Point(10.5, .5)}[fault]
        if fault == 'length': return np.array([], dtype=object)
        return np.array([[value]] if fault == 'rank' else [value], dtype=object)
    monkeypatch.setattr(shapely, 'point_on_surface', broken)
    with pytest.raises(WorkflowExecutionError) as caught:
        GeoPythonWorkflowRunner().execute({'tasks': [{'id': 'PRIVATE', 'operator': 'representative_point', 'params': {'input_gdf': source}, 'depends_on': []}]})
    assert caught.value.error_code == 'EXECUTION_FAILED' and caught.value.diagnostic is None
    assert len(calls) == 1 and len(calls[0]) == 1 and calls[0][0].equals(largest)


@pytest.mark.parametrize('area', [float('nan'), float('inf'), 0., -1.])
def test_representative_point_native_net_area_failure_is_not_source_diagnostic(area, monkeypatch):
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime import WorkflowExecutionError
    source = buffer_source([shapely.MultiPolygon([box(0, 0, 4, 4), box(10, 0, 11, 1)])])
    monkeypatch.setattr(shapely, 'area', lambda values, *args, **kwargs: np.full(np.shape(values), area))
    calls = []
    def forbidden(*args, **kwargs):
        calls.append(1)
        raise AssertionError('unproven net area reached candidate selection')
    monkeypatch.setattr(shapely, 'point_on_surface', forbidden)
    with pytest.raises(WorkflowExecutionError) as caught:
        GeoPythonWorkflowRunner().execute({'tasks': [{'id': 'PRIVATE', 'operator': 'representative_point', 'params': {'input_gdf': source}, 'depends_on': []}]})
    assert caught.value.error_code == 'EXECUTION_FAILED' and caught.value.diagnostic is None
    assert calls == []


MEASUREMENT_CASES = [('get_area','operator_input_invalid','input_gdf',''),('get_area','operator_crs_invalid','',''),
    ('get_area','operator_geometry_field_conflict','input_gdf','area'),('get_length','operator_input_invalid','input_gdf',''),
    ('get_length','operator_crs_invalid','',''),('get_length','operator_geometry_field_conflict','input_gdf','length'),
    ('split_by_area','operator_input_invalid','input_gdf',''),('split_by_area','operator_crs_invalid','',''),
    ('split_by_area','operator_geometry_field_conflict','input_gdf','area'),('split_by_area','operator_nonnegative_number_required','threshold','')]


def measurement_source(crs=3857):
    hole = Polygon([(0,0),(10,0),(10,8),(0,8),(0,0)], [[(1,1),(3,1),(3,4),(1,4),(1,1)]])
    line = LineString([(0,0),(3,0),(3,4)])
    geometries = [Point(2,3), line, hole, shapely.MultiPolygon([hole,box(20,0,22,2)]),
        MultiLineString([[(0,0),(3,0),(3,4)],[(0,0),(3,0),(3,4)]]),
        shapely.GeometryCollection([hole,line,Point(5,5)]),
        shapely.GeometryCollection([box(0,0,1,1),box(.5,0,1.5,1)]),
        Point(), LineString(), Polygon(), shapely.GeometryCollection()]
    return buffer_source(geometries,crs)


@pytest.mark.parametrize('crs',[3857,2263])
def test_measurement_exact_holes_parts_collection_repeated_contributions_empty_and_units(crs):
    source=measurement_source(crs);source['area']=-999;source['length']=-888
    before=source.copy(deep=True)
    expected_area=[0,0,74,78,0,74,2,0,0,0,0]
    expected_length=[0,7,46,54,14,53,8,0,0,0,0]
    for operator,field,expected in [('get_area','area',expected_area),('get_length','length',expected_length)]:
        result=get_operator(operator)(source)
        assert result[field].tolist()==expected and result[field].dtype==np.dtype('float64')
        assert result.columns.equals(source.columns) and result.index.equals(source.index)
        assert result.crs==source.crs and result.geometry.name=='shape'
        assert result.geometry.to_wkb().tolist()==source.geometry.to_wkb().tolist()
        pd.testing.assert_frame_equal(result.drop(columns=field),source.drop(columns=field))
    assert_geodataframe_equal(source,before)


@pytest.mark.parametrize('threshold',[0.,2.,74.,100.])
def test_measurement_area_split_is_total_disjoint_preserves_duplicate_indices_and_reuses_area(threshold,monkeypatch):
    import operators.data_operations as data_ops
    source=measurement_source();source['area']=-1;before=source.copy(deep=True)
    original=data_ops.get_area;calls=[]
    def counted(frame):calls.append(frame);return original(frame)
    monkeypatch.setattr(data_ops,'get_area',counted)
    result=get_operator('split_by_area')(source,threshold)
    assert len(calls)==1 and calls[0] is source
    areas=[0,0,74,78,0,74,2,0,0,0,0]
    for port,positions in [('large',[i for i,v in enumerate(areas) if v>threshold]),('small',[i for i,v in enumerate(areas) if v<=threshold])]:
        expected=source.iloc[positions].copy();expected['area']=np.array(areas,dtype=float)[positions]
        assert_geodataframe_equal(result[port],expected)
    assert len(result['large'])+len(result['small'])==len(source)
    assert set(result['large'].label).isdisjoint(result['small'].label)
    assert_geodataframe_equal(source,before)


@pytest.mark.parametrize('operator',['get_area','get_length','split_by_area'])
@pytest.mark.parametrize('problem',['not_gdf','duplicate','no_active','extra_geometry','null','invalid','z','m','nonfinite','crs_missing','geographic','geocentric'])
def test_measurement_rejects_unproven_source_and_crs_atomically(operator,problem):
    from operators.properties_operators import MeasurementInputError
    source=buffer_source([Point(1,2)])
    if problem=='not_gdf':source=pd.DataFrame({'x':[1]})
    elif problem=='duplicate':source.columns=['same']*len(source.columns)
    elif problem=='no_active':source=gpd.GeoDataFrame({'x':[1]})
    elif problem=='extra_geometry':source['other']=source.geometry.copy()
    elif problem in ('crs_missing','geographic','geocentric'):
        source=source.set_crs({'crs_missing':None,'geographic':4326,'geocentric':4978}[problem],allow_override=True)
    else:source['shape']=[{'null':None,'invalid':Polygon([(0,0),(2,2),(0,2),(2,0),(0,0)]),
        'z':Point(1,2,3),'m':shapely.from_wkt('POINT M (1 2 3)'),'nonfinite':Point(float('inf'),1)}[problem]]
    before=source.copy(deep=True)
    with pytest.raises(MeasurementInputError) as caught:get_operator(operator)(source,1.) if operator=='split_by_area' else get_operator(operator)(source)
    assert caught.value.code==('crs' if problem in ('crs_missing','geographic','geocentric') else 'source')
    pd.testing.assert_frame_equal(source,before)


@pytest.mark.parametrize('value',[-1,True,False,'2',None,float('nan'),float('inf'),-float('inf'),[],[1]])
def test_measurement_split_rejects_bad_threshold_even_for_empty_batch(value):
    from operators.properties_operators import MeasurementInputError
    for source in (measurement_source(),measurement_source().iloc[:0]):
        with pytest.raises(MeasurementInputError) as caught:get_operator('split_by_area')(source,value)
        assert caught.value.code=='threshold'


@pytest.mark.parametrize('operator,field',[('get_area','area'),('get_length','length'),('split_by_area','area')])
def test_measurement_geometry_output_collision_is_typed_and_other_field_names_are_safe(operator,field):
    from operators.properties_operators import MeasurementInputError
    for empty in (False,True):
        source=buffer_source([box(0,0,3,2)]).rename_geometry(field)
        if empty:source=source.iloc[:0]
        with pytest.raises(MeasurementInputError) as caught:get_operator(operator)(source,1.) if operator=='split_by_area' else get_operator(operator)(source)
        assert caught.value.code==field
    safe=buffer_source([box(0,0,3,2)]).rename_geometry('length' if field=='area' else 'area')
    result=get_operator(operator)(safe,1.)['large'] if operator=='split_by_area' else get_operator(operator)(safe)
    assert result.geometry.name==safe.geometry.name and result.geometry.to_wkb().tolist()==safe.geometry.to_wkb().tolist()


@pytest.mark.parametrize('operator',['get_area','get_length','split_by_area'])
def test_measurement_empty_batch_keeps_schema_and_checks_crs(operator):
    from operators.properties_operators import MeasurementInputError
    source=measurement_source().iloc[:0];result=get_operator(operator)(source,0.) if operator=='split_by_area' else get_operator(operator)(source)
    for frame in result.values() if isinstance(result,dict) else [result]:
        assert frame.empty and frame.index.equals(source.index) and frame.crs==source.crs
        assert frame.geometry.name=='shape' and set(frame.columns)==set(source.columns)|{('length'if operator=='get_length'else'area')}
    with pytest.raises(MeasurementInputError):get_operator(operator)(source.set_crs(None,allow_override=True),0.) if operator=='split_by_area' else get_operator(operator)(source.set_crs(None,allow_override=True))


@pytest.mark.parametrize('operator',['get_area','get_length','split_by_area'])
@pytest.mark.parametrize('fault',['exception','negative','nan','inf','rank','length','empty_nonzero'])
def test_measurement_native_faults_remain_execution_failures_and_block_saves(operator,fault,monkeypatch):
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime import WorkflowExecutionError
    source=buffer_source([Point() if fault=='empty_nonzero' else box(0,0,1,1)])
    def broken(*args,**kwargs):
        if fault=='exception':raise shapely.errors.GEOSException('PRIVATE native issue')
        return {'negative':np.array([-1.]),'nan':np.array([float('nan')]),'inf':np.array([float('inf')]),
            'rank':np.array([[1.]]),'length':np.array([]),'empty_nonzero':np.array([1.])}[fault]
    monkeypatch.setattr(shapely,'length'if operator=='get_length'else'area',broken)
    with pytest.raises(WorkflowExecutionError) as caught:GeoPythonWorkflowRunner().execute({'tasks':[
        {'id':'PRIVATE','operator':operator,'params':{'input_gdf':source,**({'threshold':0.}if operator=='split_by_area'else{})},'depends_on':[]},
        {'id':'save','operator':'save','params':{},'depends_on':['PRIVATE']}]})
    assert caught.value.error_code=='EXECUTION_FAILED' and caught.value.diagnostic is None
    assert caught.value.task_order==['PRIVATE']


@pytest.mark.parametrize('language',['zh-cn','en'])
@pytest.mark.parametrize('operator,reason,parameter,field',MEASUREMENT_CASES)
def test_measurement_runtime_closed_diagnostic_freezes_definition_and_blocks_save(language,operator,reason,parameter,field,monkeypatch):
    import workflow_engine
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime import WorkflowExecutionError
    source=buffer_source([Point(1,2)])
    if reason=='operator_input_invalid':source['shape']=[None]
    elif reason=='operator_crs_invalid':source=source.set_crs(4326,allow_override=True)
    elif reason=='operator_geometry_field_conflict':source=source.rename_geometry(field)
    lookup=workflow_engine.get_operator;calls=[]
    def guarded(name):
        calls.append(name)
        if name=='load':return lambda **kwargs:source
        if name=='save':raise AssertionError('measurement refusal reached save')
        return lookup(name)
    monkeypatch.setattr(workflow_engine,'get_operator',guarded)
    tasks=[{'id':'PRIVATE','operator':operator,'params':{'input_gdf':{'$ref':'load'},**({'threshold':-1. if parameter=='threshold'else 0.}if operator=='split_by_area'else{})},'depends_on':['load']},
        {'id':'load','operator':'load','params':{},'depends_on':[]},
        {'id':'save','operator':'save','params':{},'depends_on':['PRIVATE']}]
    with pytest.raises(WorkflowExecutionError) as caught:GeoPythonWorkflowRunner(language=language).execute({'tasks':tasks})
    assert caught.value.error_code=='INVALID_PARAMS' and calls==['load',operator]
    assert caught.value.task_order==['load','PRIVATE']
    assert caught.value.diagnostic=={'schema_version':'addp.execution-failure/v3','stage':'execution','reason':reason,
        'operator':{'node_index':1,'operator_id':operator,**({'parameter':parameter}if parameter else{}),**({'field':field}if field else{})}}
    assert 'PRIVATE' not in json.dumps(caught.value.diagnostic)


def test_measurement_real_geopackage_preserves_geometry_attributes_and_numeric_values(tmp_path):
    from osgeo import ogr
    source=buffer_source([Polygon([(0,0),(10,0),(10,8),(0,8),(0,0)], [[(1,1),(3,1),(3,4),(1,4),(1,1)]]),box(20,0,22,2)])
    source['enabled']=pd.array([None,True],dtype='boolean');source['area']=-7;source['length']=-8
    before=source.copy(deep=True);result=get_operator('get_length')(get_operator('get_area')(source))
    conn={'storage_type':'nfs','mount_path':str(tmp_path)}
    get_operator('save')(input_df=result,connection_info=conn,path='measurements.gpkg',mode='fail')
    ds=ogr.Open(str(tmp_path/'measurements.gpkg'));layer=ds.GetLayer(0);assert layer.GetSpatialRef().GetAuthorityCode(None)=='3857'
    rows=list(layer);assert len(rows)==2
    for i,row in enumerate(rows):
        assert row.GetField('area')==[74,4][i] and row.GetField('length')==[46,8][i]
        assert row.GetField('geometry')=='business' and row.GetField('label')==f'row-{i}'
        assert row.GetField('grade')==[None,2**53+2][i] and row.GetField('enabled')==[None,True][i]
        assert bytes(row.GetGeometryRef().ExportToIsoWkb())==source.geometry.to_wkb(byte_order=1,flavor='iso').iloc[i]
    name=layer.GetGeometryColumn();ds=None
    loaded=get_operator('load')(connection_info=conn,path='measurements.gpkg')
    assert loaded.geometry.name==name and loaded.crs==source.crs
    assert loaded['area'].tolist()==[74,4] and loaded['length'].tolist()==[46,8]
    assert str(loaded.grade.dtype)=='Int64' and str(loaded.enabled.dtype)=='boolean'
    assert loaded.geometry.to_wkb().tolist()==source.geometry.to_wkb().tolist()
    assert_geodataframe_equal(source,before)


def test_measurement_null_source_cannot_silently_disappear_from_both_area_split_ports():
    from operators.properties_operators import MeasurementInputError
    source=buffer_source([box(0,0,1,1),None])
    before=source.copy(deep=True)
    with pytest.raises(MeasurementInputError) as caught:get_operator('split_by_area')(source,0.)
    assert caught.value.code=='source'
    assert_geodataframe_equal(source,before)


DISTANCE_ADMISSION_RULES = [('operator_input_invalid','input_gdf',''),('operator_reference_invalid','target_geom',''),
    ('operator_geometry_field_conflict','input_gdf','distance'),('operator_projected_crs_required','',''),('operator_crs_mismatch','','')]


@pytest.mark.parametrize('crs',[3857,2263])
def test_distance_admission_analytic_nearest_whole_target_parts_duplicates_and_native_units(crs):
    source=buffer_source([Point(-3,-4),LineString([(3,4),(3,8)]),
        shapely.GeometryCollection([Point(5,12),Point(5,-12)]),
        shapely.MultiPoint([(100,100),(10,0)]),Point(20,0)],crs)
    source['distance']=-999.
    target=buffer_source([Point(0,0),Point(10,0),Point(0,0)],crs).rename_geometry('distance')
    before_source=source.copy(deep=True);before_target=target.copy(deep=True)
    result=get_operator('distance_to')(source,target)
    assert result['distance'].tolist()==[5.,5.,13.,0.,10.] and result['distance'].dtype==np.dtype('float64')
    assert result.index.equals(source.index) and result.columns.equals(source.columns)
    pd.testing.assert_frame_equal(result.drop(columns='distance'),source.drop(columns='distance'))
    assert result.geometry.to_wkb().tolist()==source.geometry.to_wkb().tolist() and result.crs==source.crs
    assert_geodataframe_equal(source,before_source);assert_geodataframe_equal(target,before_target)


def test_distance_admission_polygon_interior_contact_hole_and_separation_are_not_boundary_only_distances():
    polygon=Polygon([(0,0),(10,0),(10,10),(0,10),(0,0)], [[(2,2),(4,2),(4,4),(2,4),(2,2)]])
    source=buffer_source([Point(1,1),Point(3,3),Point(2,3),Point(13,14),LineString([(2.5,3),(3.5,3)])])
    result=get_operator('distance_to')(source,buffer_source([polygon]))
    assert result['distance'].tolist()==[0.,1.,0.,5.,.5]


def test_distance_admission_nonempty_collections_may_keep_empty_members_without_mutation():
    source=buffer_source([shapely.GeometryCollection([Point(),Point(0,0)])])
    target=buffer_source([shapely.GeometryCollection([LineString(),Point(3,4)])])
    before_source=source.copy(deep=True);before_target=target.copy(deep=True)
    assert get_operator('distance_to')(source,target)['distance'].tolist()==[5.]
    assert_geodataframe_equal(source,before_source);assert_geodataframe_equal(target,before_target)


@pytest.mark.parametrize('side',['source','reference'])
@pytest.mark.parametrize('problem',['not_gdf','duplicate','no_active','null','empty_point','empty_line','empty_polygon','empty_collection','invalid','z','m','nonfinite'])
def test_distance_admission_rejects_each_bad_record_without_skipping_or_mutation(side,problem,monkeypatch):
    from operators.spatial_relations import DistanceInputError
    source=buffer_source([Point(0,0),Point(1,1)]);target=buffer_source([Point(3,4),Point(10,10)])
    bad=source.copy(deep=True)if side=='source'else target.copy(deep=True)
    if problem=='not_gdf':bad=pd.DataFrame({'x':[1]})
    elif problem=='duplicate':bad.columns=['same']*len(bad.columns)
    elif problem=='no_active':bad=gpd.GeoDataFrame({'x':[1]})
    else:bad['shape']=[Point(3,4),{'null':None,'empty_point':Point(),'empty_line':LineString(),'empty_polygon':Polygon(),
        'empty_collection':shapely.GeometryCollection(),'invalid':Polygon([(0,0),(2,2),(0,2),(2,0),(0,0)]),
        'z':Point(1,2,3),'m':shapely.from_wkt('POINT M (1 2 3)'),'nonfinite':Point(float('inf'),0)}[problem]]
    if side=='source':source=bad
    else:target=bad
    before_source=source.copy(deep=True);before_target=target.copy(deep=True)
    def forbidden(*args,**kwargs):raise AssertionError('bad distance input reached union')
    monkeypatch.setattr(shapely,'union_all',forbidden)
    with pytest.raises(DistanceInputError)as caught:get_operator('distance_to')(source,target)
    assert caught.value.code==side
    pd.testing.assert_frame_equal(source,before_source);pd.testing.assert_frame_equal(target,before_target)


@pytest.mark.parametrize('empty_source',[False,True])
def test_distance_admission_zero_row_target_is_rejected_even_for_empty_source(empty_source):
    from operators.spatial_relations import DistanceInputError
    source=buffer_source([Point(0,0)]);target=buffer_source([Point(3,4)]).iloc[:0]
    if empty_source:source=source.iloc[:0]
    with pytest.raises(DistanceInputError)as caught:get_operator('distance_to')(source,target)
    assert caught.value.code=='reference'


@pytest.mark.parametrize('bad_target',[None,Point(),LineString(),Polygon(),shapely.GeometryCollection()])
def test_distance_admission_empty_source_still_rejects_each_bad_target_record(bad_target):
    from operators.spatial_relations import DistanceInputError
    source=buffer_source([Point(0,0)]).iloc[:0];target=buffer_source([Point(3,4),bad_target])
    with pytest.raises(DistanceInputError)as caught:get_operator('distance_to')(source,target)
    assert caught.value.code=='reference'


def test_distance_admission_empty_source_validates_all_inputs_but_skips_numerical_kernels(monkeypatch):
    from operators.spatial_relations import DistanceInputError
    source=buffer_source([Point(0,0)]).iloc[:0];source['distance']=pd.Series(dtype='float64')
    target=buffer_source([Point(3,4)])
    def forbidden(*args,**kwargs):raise AssertionError('empty source should not enter native kernels')
    monkeypatch.setattr(shapely,'union_all',forbidden);monkeypatch.setattr(shapely,'distance',forbidden)
    result=get_operator('distance_to')(source,target)
    assert result.empty and result['distance'].dtype==np.dtype('float64')
    assert result.columns.equals(source.columns)and result.index.equals(source.index)
    assert_geodataframe_equal(result.drop(columns='distance'),source.drop(columns='distance'))
    with pytest.raises(DistanceInputError)as caught:get_operator('distance_to')(source,target.set_crs(4326,allow_override=True))
    assert caught.value.code=='projected_crs'
    with pytest.raises(DistanceInputError)as caught:get_operator('distance_to')(source,target.set_crs(3395,allow_override=True))
    assert caught.value.code=='crs_mismatch'


@pytest.mark.parametrize('fault',['exception','missing','empty','invalid','z','m','nonfinite','array','not_geometry'])
def test_distance_admission_bad_union_is_computation_failure_without_input_diagnostic(fault,monkeypatch):
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime import WorkflowExecutionError
    source=buffer_source([Point(0,0)]);target=buffer_source([Point(3,4)])
    def broken(*args,**kwargs):
        if fault=='exception':raise shapely.errors.GEOSException('PRIVATE native union failure')
        return {'missing':None,'empty':Point(),'invalid':Polygon([(0,0),(2,2),(0,2),(2,0),(0,0)]),
            'z':Point(1,2,3),'m':shapely.from_wkt('POINT M (1 2 3)'),'nonfinite':Point(float('inf'),0),
            'array':np.array([Point(3,4)],dtype=object),'not_geometry':1}[fault]
    monkeypatch.setattr(shapely,'union_all',broken)
    with pytest.raises(WorkflowExecutionError)as caught:GeoPythonWorkflowRunner().execute({'tasks':[
        {'id':'PRIVATE','operator':'distance_to','params':{'input_gdf':source,'target_geom':target},'depends_on':[]},
        {'id':'save','operator':'save','params':{},'depends_on':['PRIVATE']}]})
    assert caught.value.error_code=='EXECUTION_FAILED'and caught.value.diagnostic is None and caught.value.task_order==['PRIVATE']


@pytest.mark.parametrize('fault',['exception','nan','infinity','negative','rank','length','missing','string','boolean','complex'])
def test_distance_admission_bad_distance_is_computation_failure_and_blocks_save(fault,monkeypatch):
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime import WorkflowExecutionError
    source=buffer_source([Point(0,0)]);target=buffer_source([Point(3,4)])
    def broken(*args,**kwargs):
        if fault=='exception':raise shapely.errors.GEOSException('PRIVATE native distance failure')
        return {'nan':np.array([float('nan')]),'infinity':np.array([float('inf')]),'negative':np.array([-1.]),
            'rank':np.array([[1.]]),'length':np.array([]),'missing':None,'string':np.array(['5']),
            'boolean':np.array([True]),'complex':np.array([5+0j])}[fault]
    monkeypatch.setattr(shapely,'distance',broken)
    with pytest.raises(WorkflowExecutionError)as caught:GeoPythonWorkflowRunner().execute({'tasks':[
        {'id':'PRIVATE','operator':'distance_to','params':{'input_gdf':source,'target_geom':target},'depends_on':[]},
        {'id':'save','operator':'save','params':{},'depends_on':['PRIVATE']}]})
    assert caught.value.error_code=='EXECUTION_FAILED'and caught.value.diagnostic is None and caught.value.task_order==['PRIVATE']


@pytest.mark.parametrize('language',['zh-cn','en'])
@pytest.mark.parametrize('reason,parameter,field',DISTANCE_ADMISSION_RULES)
def test_distance_admission_runtime_diagnostics_freeze_definition_and_block_save(language,reason,parameter,field,monkeypatch):
    import workflow_engine
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime import WorkflowExecutionError
    source=buffer_source([Point(0,0)]);target=buffer_source([Point(3,4)])
    if reason=='operator_input_invalid':source['shape']=[None]
    elif reason=='operator_reference_invalid':target=target.iloc[:0]
    elif field:source=source.rename_geometry(field)
    elif reason=='operator_projected_crs_required':target=target.set_crs(4326,allow_override=True)
    else:target=target.set_crs(3395,allow_override=True)
    original=workflow_engine.get_operator;calls=[]
    def guarded(name):
        calls.append(name)
        if name=='source_load':return lambda **kwargs:source
        if name=='target_load':return lambda **kwargs:target
        if name=='save':raise AssertionError('distance input refusal reached save')
        return original(name)
    monkeypatch.setattr(workflow_engine,'get_operator',guarded)
    # Existing registered load is used twice, with the actual frame chosen by its controlled parameter.
    def lookup(name):
        if name=='load':
            return lambda **kwargs: source if kwargs.get('path')=='source'else target
        return guarded(name)
    monkeypatch.setattr(workflow_engine,'get_operator',lookup)
    tasks=[{'id':'PRIVATE','operator':'distance_to','params':{'input_gdf':{'$ref':'s'},'target_geom':{'$ref':'t'}},'depends_on':['s','t']},
        {'id':'s','operator':'load','params':{'path':'source'},'depends_on':[]},
        {'id':'t','operator':'load','params':{'path':'target'},'depends_on':[]},
        {'id':'save','operator':'save','params':{},'depends_on':['PRIVATE']}]
    with pytest.raises(WorkflowExecutionError)as caught:GeoPythonWorkflowRunner(language=language).execute({'tasks':tasks})
    assert caught.value.error_code=='INVALID_PARAMS'and calls==['distance_to']
    assert caught.value.diagnostic=={'schema_version':'addp.execution-failure/v3','stage':'execution','reason':reason,
        'operator':{'node_index':1,'operator_id':'distance_to',**({'parameter':parameter}if parameter else{}),**({'field':field}if field else{})}}
    assert caught.value.task_order[-1]=='PRIVATE'and'save'not in caught.value.task_order
    assert 'PRIVATE'not in json.dumps(caught.value.diagnostic)


def test_distance_admission_real_geopackage_keeps_distances_source_wkb_and_business_fields(tmp_path):
    from osgeo import ogr
    source=buffer_source([Point(-3,-4),Point(10,0)]);source['enabled']=pd.array([None,True],dtype='boolean');source['distance']=-1
    target=buffer_source([Point(0,0),Point(10,0)]).rename_geometry('reference_shape')
    before_source=source.copy(deep=True);before_target=target.copy(deep=True)
    result=get_operator('distance_to')(source,target);conn={'storage_type':'nfs','mount_path':str(tmp_path)}
    get_operator('save')(input_df=result,connection_info=conn,path='distances.gpkg',mode='fail')
    ds=ogr.Open(str(tmp_path/'distances.gpkg'));layer=ds.GetLayer(0);rows=list(layer)
    assert len(rows)==2 and layer.GetSpatialRef().GetAuthorityCode(None)=='3857'
    for i,row in enumerate(rows):
        assert row.GetField('distance')==[5.,0.][i]and row.GetField('geometry')=='business'
        assert row.GetField('grade')==[None,2**53+2][i]and row.GetField('enabled')==[None,True][i]
        assert row.GetGeometryRef().GetPoint_2D(0)==[(-3.,-4.),(10.,0.)][i]
        assert bytes(row.GetGeometryRef().ExportToIsoWkb())==source.geometry.to_wkb(byte_order=1,flavor='iso').iloc[i]
    actual_name=layer.GetGeometryColumn();ds=None
    loaded=get_operator('load')(connection_info=conn,path='distances.gpkg')
    assert loaded.geometry.name==actual_name and loaded.crs==source.crs and loaded['distance'].tolist()==[5.,0.]
    assert loaded.geometry.to_wkb().tolist()==source.geometry.to_wkb().tolist()
    assert str(loaded.grade.dtype)=='Int64'and str(loaded.enabled.dtype)=='boolean'
    assert_geodataframe_equal(source,before_source);assert_geodataframe_equal(target,before_target)


@pytest.mark.parametrize('bad_target',[None,Point()])
def test_distance_admission_bad_target_must_not_be_silently_ignored_during_union(bad_target):
    from operators.spatial_relations import DistanceInputError
    source=buffer_source([Point(0,0)]);target=buffer_source([Point(3,4),bad_target])
    with pytest.raises(DistanceInputError)as caught:get_operator('distance_to')(source,target)
    assert caught.value.code=='reference'


def test_distance_admission_preserves_inactive_geometry_typed_columns_and_uses_only_active_columns():
    source=buffer_source([Point(0,0),Point(9,12)])
    target=buffer_source([Point(3,4),Point(30,40)]).rename_geometry('reference_shape')
    source['geometry']=gpd.GeoSeries([Point(100,100),Point(101,101)],index=source.index,crs=source.crs)
    target['geometry']=gpd.GeoSeries([Point(100,100),Point(200,200)],index=target.index,crs=target.crs)
    before_source=source.copy(deep=True);before_target=target.copy(deep=True)
    result=get_operator('distance_to')(source,target)
    assert result['distance'].tolist()==[5.,10.]
    assert_geodataframe_equal(result.drop(columns='distance'),source)
    assert_geodataframe_equal(source,before_source);assert_geodataframe_equal(target,before_target)


BOUNDS_FIELDS = ['minx','miny','maxx','maxy']
BOUNDS_CASES = [('operator_input_invalid','input_gdf',''),('operator_crs_invalid','','')]+[('operator_geometry_field_conflict','input_gdf',f)for f in BOUNDS_FIELDS]


def bounds_admission_source(shapes,crs='EPSG:3857'):
    f=gpd.GeoDataFrame({'geometry':pd.array(['business']*len(shapes),dtype='string'),
       'grade':pd.array([None if i%2==0 else 2**53+i for i in range(len(shapes))],dtype='Int64'),
       'enabled':pd.array([None if i%2==0 else True for i in range(len(shapes))],dtype='boolean'),
       'shape':gpd.GeoSeries(shapes,crs=crs)},geometry='shape')
    f.index=pd.Index(['duplicate']*len(f),name='source-row')
    return f


@pytest.mark.parametrize('crs',['EPSG:3857','EPSG:2263','EPSG:4326','OGC:CRS84','+proj=aeqd +lat_0=23 +lon_0=113 +datum=WGS84 +units=m +no_defs'])
def test_bounds_admission_raw_xy_all_parts_degeneracies_and_empty(crs):
    shapes=[Point(2,3),LineString([(-4,2),(7,2)]),LineString([(5,-2),(5,8)]),
      Polygon([(0,0),(4,0),(4,4),(0,4),(0,0)], [[(1,1),(2,1),(2,2),(1,2),(1,1)]]),
      shapely.MultiPoint([(-179,3),(179,-3)]),shapely.GeometryCollection([Point(),Point(-7,9),LineString([(-5,3),(8,-4)])]),Point(),Polygon(),shapely.GeometryCollection()]
    source=bounds_admission_source(shapes,crs);source['minx']='replace';source['maxy']=-99
    source['inactive_shape']=gpd.GeoSeries([Point(999,999)]*len(source),index=source.index,crs=crs)
    before=source.copy(deep=True);result=get_operator('get_bounds')(source)
    expected=[[2,3,2,3],[-4,2,7,2],[5,-2,5,8],[0,0,4,4],[-179,-3,179,3],[-7,-4,8,9]]
    assert result[BOUNDS_FIELDS].iloc[:6].values.tolist()==expected
    assert result[BOUNDS_FIELDS].iloc[6:].isna().all().all()
    assert all(str(result[f].dtype)=='Float64'for f in BOUNDS_FIELDS)
    assert_geodataframe_equal(source,before)
    assert_geodataframe_equal(result.drop(columns=BOUNDS_FIELDS),source.drop(columns=['minx','maxy']))
    from workflow_engine import _project
    projected=_project(get_operator('get_bounds')(source.drop(columns='inactive_shape')))
    assert all(feature['properties'][f]is None for feature in projected['features'][6:]for f in BOUNDS_FIELDS)
    json.dumps(projected,allow_nan=False)


@pytest.mark.parametrize('problem',['null','invalid','z','m','inf','nan','duplicate_columns','no_active','not_geoframe'])
def test_bounds_admission_rejects_bad_source_atomically(problem):
    from operators.properties_operators import BoundsInputError
    shape={'null':None,'invalid':Polygon([(0,0),(2,2),(0,2),(2,0),(0,0)]),'z':Point(1,2,3),
      'm':shapely.from_wkt('POINT M (1 2 3)'),'inf':Point(float('inf'),1),'nan':Point(float('nan'),1)}.get(problem,Point(1,2))
    source=bounds_admission_source([Point(0,0),shape])
    if problem=='duplicate_columns':source.columns=['geometry','grade','grade','shape']
    elif problem=='no_active':source=source.drop(columns='shape')
    elif problem=='not_geoframe':source=pd.DataFrame(source)
    before=source.copy(deep=True)
    with pytest.raises(BoundsInputError)as caught:get_operator('get_bounds')(source)
    assert caught.value.code=='source'
    pd.testing.assert_frame_equal(source,before)


@pytest.mark.parametrize('crs',[None,'EPSG:4978','EPSG:4979','EPSG:7415'])
@pytest.mark.parametrize('empty',[False,True])
def test_bounds_admission_rejects_unknown_geocentric_3d_compound_crs(crs,empty):
    from operators.properties_operators import BoundsInputError
    source=bounds_admission_source([]if empty else[Point(1,2)],crs)
    with pytest.raises(BoundsInputError)as caught:get_operator('get_bounds')(source)
    assert caught.value.code=='crs'


@pytest.mark.parametrize('field',BOUNDS_FIELDS)
@pytest.mark.parametrize('empty',[False,True])
def test_bounds_admission_active_field_conflicts_never_overwrite_geometry(field,empty):
    from operators.properties_operators import BoundsInputError
    source=bounds_admission_source([]if empty else[Point(1,2)]).rename_geometry(field);before=source.copy(deep=True)
    with pytest.raises(BoundsInputError)as caught:get_operator('get_bounds')(source)
    assert caught.value.code=='field'and caught.value.field==field
    assert_geodataframe_equal(source,before)


def test_bounds_admission_empty_batch_four_nullable_columns_and_metadata():
    source=bounds_admission_source([],'OGC:CRS84');result=get_operator('get_bounds')(source)
    assert len(result)==0 and all(str(result[f].dtype)=='Float64'for f in BOUNDS_FIELDS)
    assert result.crs==source.crs and result.geometry.name=='shape'
    meta=next(m for m in list_operators()if m['name']=='get_bounds')
    assert meta['execution_modes']==['workflow']and meta['effects']==['read']
    assert any('EMPTY' in n and 'NULL' in n for n in meta['detailed_description']['notes'])


@pytest.mark.parametrize('fault',['raise','shape','rows','bool','text','nan','inf','reversed_x','reversed_y','empty_not_nan'])
def test_bounds_admission_native_faults_remain_execution_failures(fault,monkeypatch):
    from addp_common.workflow_runtime import WorkflowExecutionError
    import workflow_engine
    source=bounds_admission_source([Point(1,2),Point()]);native=shapely.bounds
    def bad(*args):
        if fault=='raise':raise RuntimeError('PRIVATE GEOS')
        values=native(*args).copy()
        if fault=='shape':return values[:,:3]
        if fault=='rows':return values[:1]
        if fault=='bool':return np.zeros((2,4),dtype=bool)
        if fault=='text':return values.astype(str)
        if fault in ['nan','inf']:values[0,0]=float(fault)
        if fault=='reversed_x':values[0,0]=2
        if fault=='reversed_y':values[0,1]=3
        if fault=='empty_not_nan':values[1]=0
        return values
    monkeypatch.setattr(shapely,'bounds',bad)
    lookup=workflow_engine.get_operator
    def guarded(name):
        if name=='load':return lambda **kw:source
        if name=='save':raise AssertionError('invalid bounds reached save')
        return lookup(name)
    monkeypatch.setattr(workflow_engine,'get_operator',guarded)
    tasks=[{'id':'load','operator':'load','params':{},'depends_on':[]},
      {'id':'get','operator':'get_bounds','params':{'input_gdf':{'$ref':'load'}},'depends_on':['load']},
      {'id':'save','operator':'save','params':{},'depends_on':['get']}]
    with pytest.raises(WorkflowExecutionError)as caught:workflow_engine.GeoPythonWorkflowRunner().execute({'tasks':tasks})
    assert caught.value.error_code=='EXECUTION_FAILED'and caught.value.diagnostic is None


@pytest.mark.parametrize('language',['zh-cn','en'])
@pytest.mark.parametrize('reason,parameter,field',BOUNDS_CASES)
def test_bounds_admission_runtime_frozen_diagnostic_and_no_save(language,reason,parameter,field,monkeypatch):
    from addp_common.workflow_runtime import WorkflowExecutionError
    import workflow_engine
    source=bounds_admission_source([Point(1,2)])
    if reason=='operator_input_invalid':source['shape']=[None]
    elif reason=='operator_crs_invalid':source=source.set_crs(None,allow_override=True)
    else:source=source.rename_geometry(field)
    lookup=workflow_engine.get_operator;calls=[]
    def guarded(name):
        calls.append(name)
        if name=='load':return lambda **kw:source
        if name=='save':raise AssertionError('bounds refusal reached save')
        return lookup(name)
    monkeypatch.setattr(workflow_engine,'get_operator',guarded)
    tasks=[{'id':'PRIVATE','operator':'get_bounds','params':{'input_gdf':{'$ref':'load'}},'depends_on':['load']},
      {'id':'load','operator':'load','params':{},'depends_on':[]}, {'id':'save','operator':'save','params':{},'depends_on':['PRIVATE']}]
    with pytest.raises(WorkflowExecutionError)as caught:workflow_engine.GeoPythonWorkflowRunner(language=language).execute({'tasks':tasks})
    assert calls==['load','get_bounds']and caught.value.error_code=='INVALID_PARAMS'
    assert caught.value.diagnostic=={'schema_version':'addp.execution-failure/v3','stage':'execution','reason':reason,
      'operator':{'node_index':1,'operator_id':'get_bounds',**({'parameter':parameter}if parameter else{}),**({'field':field}if field else{})}}
    assert 'PRIVATE'not in json.dumps(caught.value.diagnostic)
    from operators.properties_operators import BoundsInputError
    code={'operator_input_invalid':'source','operator_crs_invalid':'crs','operator_geometry_field_conflict':'field'}[reason]
    assert BoundsInputError(code,field).message(language) in str(caught.value)


def test_bounds_admission_real_geopackage_null_scalars_geometry_and_attributes(tmp_path):
    from osgeo import ogr
    source=bounds_admission_source([box(-4,2,7,9),Polygon()]);before=source.copy(deep=True)
    result=get_operator('get_bounds')(source)
    conn={'storage_type':'nfs','mount_path':str(tmp_path)}
    get_operator('save')(input_df=result,connection_info=conn,path='bounds.gpkg',mode='fail')
    ds=ogr.Open(str(tmp_path/'bounds.gpkg'));layer=ds.GetLayer(0)
    assert layer.GetSpatialRef().GetAuthorityCode(None)=='3857'
    rows=list(layer);assert len(rows)==2
    assert [rows[0].GetField(f)for f in BOUNDS_FIELDS]==[-4,2,7,9]
    assert all(rows[1].GetField(f)is None for f in BOUNDS_FIELDS)
    assert all(layer.GetLayerDefn().GetFieldDefn(layer.GetLayerDefn().GetFieldIndex(f)).GetType()==ogr.OFTReal for f in BOUNDS_FIELDS)
    for i,row in enumerate(rows):
        assert row.GetField('geometry')=='business'and row.GetField('grade')==[None,2**53+1][i]
        assert bytes(row.GetGeometryRef().ExportToIsoWkb())==source.geometry.to_wkb(byte_order=1,flavor='iso').iloc[i]
    physical_geometry_name=layer.GetGeometryColumn();ds=None
    loaded=get_operator('load')(connection_info=conn,path='bounds.gpkg')
    assert loaded[BOUNDS_FIELDS].iloc[0].tolist()==[-4,2,7,9]and loaded[BOUNDS_FIELDS].iloc[1].isna().all()
    assert loaded.geometry.to_wkb().tolist()==source.geometry.to_wkb().tolist()
    assert loaded.geometry.name==physical_geometry_name and loaded.crs==source.crs
    assert_geodataframe_equal(source,before)


MULTIPART_CASES = [('operator_input_invalid','input_gdf',''),('operator_crs_invalid','',''),('operator_field_conflict','input_gdf','source_id'),('operator_field_conflict','input_gdf','part_no')]


def multipart_source(shapes,crs='EPSG:3857',id_type='Int64'):
    n=len(shapes);ids=[2**53+3+i for i in range(n)]if id_type!='string'else[f'feature-{i}'for i in range(n)]
    f=gpd.GeoDataFrame({'feature_key':pd.array(ids,dtype=id_type),'geometry':pd.array(['business'if i%2 else None for i in range(n)],dtype='string'),
       'grade':pd.array([None if i%2==0 else 2**53+i for i in range(n)],dtype='Int64'),
       'enabled':pd.array([None if i%2==0 else True for i in range(n)],dtype='boolean'),
       'fid':pd.array([None if i%2==0 else 4 for i in range(n)],dtype='Int64'),
       'shape':gpd.GeoSeries(shapes,crs=crs)},geometry='shape')
    f.index=pd.Index(['same-index']*n,name='original-row');return f


def multipart_mixed_witness():
    p=Point(2,3);p1=Point(4,5);p2=Point(-1,-2)
    line=LineString([(0,0),(2,1),(0,0)]);a=LineString([(9,8),(7,6),(7,6),(3,2)]);b=LineString([(5,0),(5,4)])
    hole=Polygon([(10,10),(10,18),(18,18),(18,10),(10,10)], [[(11,11),(13,11),(13,13),(11,13),(11,11)]])
    face_a=Polygon([(30,0),(33,0),(33,2),(30,2),(30,0)]);face_b=box(20,0,21,1)
    return [p,shapely.MultiPoint([p1,p1,p2]),line,MultiLineString([a,a,b]),hole,shapely.MultiPolygon([face_a,face_b])], [p,p1,p1,p2,line,a,a,b,hole,face_a,face_b], [0,1,1,1,2,3,3,3,4,5,5], [1,1,2,3,1,1,2,3,1,1,2]


@pytest.mark.parametrize('crs',['EPSG:3857','EPSG:4326','OGC:CRS84','EPSG:2263','+proj=aeqd +lat_0=23 +lon_0=113 +datum=WGS84 +units=m +no_defs'])
@pytest.mark.parametrize('id_type',['Int64','UInt64','string'])
def test_multipart_split_exact_original_parts_attributes_ids_holes_repeats_and_order(crs,id_type):
    shapes,expected,source_rows,part_no=multipart_mixed_witness();source=multipart_source(shapes,crs,id_type);before=source.copy(deep=True)
    result=get_operator('split_multipart')(source,'feature_key')
    assert result.geometry.to_wkb().tolist()==[p.wkb for p in expected]
    assert result.geometry.name=='shape'and result.crs==source.crs
    assert result.part_no.tolist()==part_no and str(result.part_no.dtype)=='int64'
    assert result.source_id.tolist()==source.feature_key.iloc[source_rows].tolist()and str(result.source_id.dtype)==id_type
    assert len(result)==11 and result.index.equals(pd.RangeIndex(11))
    assert list(result.columns)==list(source.columns)+['source_id','part_no']
    pd.testing.assert_frame_equal(result.drop(columns=['shape','source_id','part_no']),source.iloc[source_rows].drop(columns='shape').reset_index(drop=True))
    assert len(result.geometry.iloc[8].interiors)==1
    assert not result[['source_id','part_no']].duplicated().any()
    assert_geodataframe_equal(source,before)


def test_multipart_split_preserves_nonactive_geometry_without_using_it():
    source=multipart_source([shapely.MultiPoint([(0,0),(1,1)])]);source['inactive_shape']=gpd.GeoSeries([Point(99,99)],index=source.index,crs=3857)
    before=source.copy(deep=True);result=get_operator('split_multipart')(source,'feature_key')
    assert result.geometry.to_wkt().tolist()==['POINT (0 0)','POINT (1 1)']
    assert result.inactive_shape.to_wkt().tolist()==['POINT (99 99)','POINT (99 99)']
    assert_geodataframe_equal(source,before)


@pytest.mark.parametrize('wkt',['GEOMETRYCOLLECTION (POINT (1 2))','GEOMETRYCOLLECTION EMPTY','POINT EMPTY','LINESTRING EMPTY','POLYGON EMPTY','MULTIPOINT EMPTY','MULTILINESTRING EMPTY','MULTIPOLYGON EMPTY','MULTIPOINT (EMPTY,(1 2))','MULTILINESTRING (EMPTY,(0 0,1 1))','MULTIPOLYGON (EMPTY,((0 0,1 0,1 1,0 0)))','POINT Z (1 2 3)','POINT M (1 2 3)','POINT (Infinity 2)','POINT (NaN 2)','POLYGON ((0 0,2 2,0 2,2 0,0 0))','MULTIPOLYGON (((0 0,2 0,2 2,0 2,0 0)),((1 1,3 1,3 3,1 3,1 1)))',None])
def test_multipart_split_rejects_collection_missing_empty_invalid_and_dimensions_atomically(wkt):
    from operators.geometric_operators import MultipartInputError
    source=multipart_source([Point(8,9),shapely.from_wkt(wkt)if wkt else None]);before=source.copy(deep=True)
    with pytest.raises(MultipartInputError)as caught:get_operator('split_multipart')(source,'feature_key')
    assert caught.value.code=='source'
    assert source.geometry.to_wkb().tolist()==before.geometry.to_wkb().tolist()
    assert source.geometry.name==before.geometry.name and source.crs==before.crs
    pd.testing.assert_frame_equal(source.drop(columns=source.geometry.name),before.drop(columns=before.geometry.name))


@pytest.mark.parametrize('problem',['unknown_id','geometry_id','empty_id_name','non_string_id','null_id','duplicate_id','blank_id','container_id','duplicate_columns','no_active','plain_frame'])
@pytest.mark.parametrize('empty',[False,True])
def test_multipart_split_checks_source_schema_and_explicit_id_even_on_empty_batches(problem,empty):
    from operators.geometric_operators import MultipartInputError
    source=multipart_source([Point(1,2),Point(3,4)]);id_field='feature_key'
    if problem=='unknown_id':id_field='PRIVATE_FIELD'
    elif problem=='geometry_id':id_field='shape'
    elif problem=='empty_id_name':id_field=''
    elif problem=='non_string_id':id_field=False
    elif problem=='null_id':source['feature_key']=pd.array([None,2],dtype='Int64')
    elif problem=='duplicate_id':source['feature_key']=pd.array([2,2],dtype='Int64')
    elif problem=='blank_id':source['feature_key']=pd.array(['ok','  '],dtype='string')
    elif problem=='container_id':source['feature_key']=[[1],[2]]
    elif problem=='duplicate_columns':source.columns=['feature_key','geometry','grade','grade','fid','shape']
    elif problem=='no_active':source=source.drop(columns='shape')
    elif problem=='plain_frame':source=pd.DataFrame(source)
    if empty:source=source.iloc[:0]
    # Value-only problems have no invalid records in an actually empty batch.
    if empty and problem in ['null_id','duplicate_id','blank_id','container_id']:
        result=get_operator('split_multipart')(source,id_field);assert len(result)==0;return
    before=source.copy(deep=True)
    with pytest.raises(MultipartInputError)as caught:get_operator('split_multipart')(source,id_field)
    assert caught.value.code=='source';pd.testing.assert_frame_equal(source,before)


@pytest.mark.parametrize('crs',[None,'EPSG:4978','EPSG:4979','EPSG:7415'])
@pytest.mark.parametrize('empty',[False,True])
def test_multipart_split_rejects_unproven_or_non2d_horizontal_crs(crs,empty):
    from operators.geometric_operators import MultipartInputError
    source=multipart_source([]if empty else[Point(1,2)],crs)
    with pytest.raises(MultipartInputError)as caught:get_operator('split_multipart')(source,'feature_key')
    assert caught.value.code=='crs'


@pytest.mark.parametrize('field',['source_id','part_no'])
@pytest.mark.parametrize('active',[False,True])
@pytest.mark.parametrize('empty',[False,True])
def test_multipart_split_never_overwrites_reserved_source_or_active_geometry(field,active,empty):
    from operators.geometric_operators import MultipartInputError
    source=multipart_source([]if empty else[Point(1,2)])
    if active:source=source.rename_geometry(field)
    else:source[field]='business'
    before=source.copy(deep=True)
    with pytest.raises(MultipartInputError)as caught:get_operator('split_multipart')(source,'feature_key')
    assert caught.value.code=='field'and caught.value.field==field
    assert_geodataframe_equal(source,before)


@pytest.mark.parametrize('id_type',['Int64','UInt64','string'])
def test_multipart_split_empty_batch_retains_full_schema_and_source_type(id_type):
    source=multipart_source([],id_type=id_type);result=get_operator('split_multipart')(source,'feature_key')
    assert len(result)==0 and result.geometry.name=='shape'and result.crs==source.crs
    assert list(result.columns)==list(source.columns)+['source_id','part_no']
    assert str(result.source_id.dtype)==id_type and str(result.part_no.dtype)=='int64'
    assert result.index.equals(pd.RangeIndex(0))


@pytest.mark.parametrize('language',['zh-cn','en'])
@pytest.mark.parametrize('reason,parameter,field',MULTIPART_CASES)
def test_multipart_split_runtime_typed_diagnostic_original_index_and_no_partial_save(language,reason,parameter,field,monkeypatch):
    from addp_common.workflow_runtime import WorkflowExecutionError
    from operators.geometric_operators import MultipartInputError
    import workflow_engine
    source=multipart_source([Point(1,2)])
    if reason=='operator_input_invalid':source['shape']=[None]
    elif reason=='operator_crs_invalid':source=source.set_crs(None,allow_override=True)
    else:source[field]='PRIVATE'
    lookup=workflow_engine.get_operator;calls=[]
    def guarded(name):
        calls.append(name)
        if name=='load':return lambda **kw:source
        if name=='save':raise AssertionError('multipart refusal reached save')
        return lookup(name)
    monkeypatch.setattr(workflow_engine,'get_operator',guarded)
    tasks=[{'id':'PRIVATE','operator':'split_multipart','params':{'input_gdf':{'$ref':'load'},'id_field':'feature_key'},'depends_on':['load']},
      {'id':'load','operator':'load','params':{},'depends_on':[]},{'id':'save','operator':'save','params':{},'depends_on':['PRIVATE']}]
    with pytest.raises(WorkflowExecutionError)as caught:workflow_engine.GeoPythonWorkflowRunner(language=language).execute({'tasks':tasks})
    assert calls==['load','split_multipart']and caught.value.error_code=='INVALID_PARAMS'
    assert caught.value.diagnostic=={'schema_version':'addp.execution-failure/v3','stage':'execution','reason':reason,
      'operator':{'node_index':1,'operator_id':'split_multipart',**({'parameter':parameter}if parameter else{}),**({'field':field}if field else{})}}
    assert 'PRIVATE'not in json.dumps(caught.value.diagnostic)
    assert MultipartInputError({'operator_input_invalid':'source','operator_crs_invalid':'crs','operator_field_conflict':'field'}[reason],field).message(language) in str(caught.value)


@pytest.mark.parametrize('fault',['exception','wrong_count','wrong_rows','rows_float','count_shape','count_negative','multi_part','nonfinite_part'])
def test_multipart_split_native_faults_stay_execution_failures_and_block_save(fault,monkeypatch):
    from addp_common.workflow_runtime import WorkflowExecutionError
    import workflow_engine
    source=multipart_source([shapely.MultiPoint([(0,0),(1,2)])]);native=shapely.get_parts;native_counts=shapely.get_num_geometries
    def broken(*a,**kw):
        if fault=='exception':raise RuntimeError('PRIVATE native error')
        parts,positions=native(*a,**kw)
        if fault=='wrong_count':return parts[:1],positions[:1]
        if fault=='wrong_rows':return parts,np.array([1,0],dtype=np.int64)
        if fault=='rows_float':return parts,positions.astype(float)
        if fault=='multi_part':parts[0]=shapely.MultiPoint([(0,0),(2,2)])
        if fault=='nonfinite_part':parts[0]=Point(float('inf'),2)
        return parts,positions
    monkeypatch.setattr(shapely,'get_parts',broken)
    if fault=='count_shape':monkeypatch.setattr(shapely,'get_num_geometries',lambda *a:np.array([[2]]))
    if fault=='count_negative':monkeypatch.setattr(shapely,'get_num_geometries',lambda *a:np.array([-1]))
    lookup=workflow_engine.get_operator
    def guarded(name):
        if name=='load':return lambda **kw:source
        if name=='save':raise AssertionError('native multipart fault reached save')
        return lookup(name)
    monkeypatch.setattr(workflow_engine,'get_operator',guarded)
    tasks=[{'id':'load','operator':'load','params':{},'depends_on':[]},
      {'id':'split','operator':'split_multipart','params':{'input_gdf':{'$ref':'load'},'id_field':'feature_key'},'depends_on':['load']},
      {'id':'save','operator':'save','params':{},'depends_on':['split']}]
    with pytest.raises(WorkflowExecutionError)as caught:workflow_engine.GeoPythonWorkflowRunner().execute({'tasks':tasks})
    assert caught.value.error_code=='EXECUTION_FAILED'and caught.value.diagnostic is None


@pytest.mark.parametrize('family',['point','line','polygon'])
def test_multipart_split_real_geopackage_loaded_source_independent_ogr_and_reload(tmp_path,family):
    from osgeo import ogr
    if family=='point':a,b=Point(3,5),Point(3,5);multi=shapely.MultiPoint([a,b]);single=Point(-1,8)
    elif family=='line':a,b=LineString([(5,0),(2,2)]),LineString([(1,1),(2,3),(1,1)]);multi=MultiLineString([a,b]);single=LineString([(9,9),(10,10)])
    else:
        a=Polygon([(0,0),(0,5),(5,5),(5,0),(0,0)], [[(1,1),(2,1),(2,2),(1,2),(1,1)]]);b=box(10,0,11,2);multi=shapely.MultiPolygon([a,b]);single=box(-4,0,-2,1)
    expected=[a,b,single];source=multipart_source([multi,single]);conn={'storage_type':'nfs','mount_path':str(tmp_path)}
    get_operator('save')(input_df=source,connection_info=conn,path='source.gpkg',mode='fail')
    loaded_source=get_operator('load')(connection_info=conn,path='source.gpkg');before=loaded_source.copy(deep=True)
    result=get_operator('split_multipart')(loaded_source,'feature_key')
    assert result.geometry.to_wkb().tolist()==[x.wkb for x in expected]
    get_operator('save')(input_df=result,connection_info=conn,path='parts.gpkg',mode='fail')
    ds=ogr.Open(str(tmp_path/'parts.gpkg'));layer=ds.GetLayer(0);records=list(layer)
    assert layer.GetSpatialRef().GetAuthorityCode(None)=='3857'and len(records)==3
    for i,row in enumerate(records):
        parent=[0,0,1][i]
        assert row.GetField('source_id')==[2**53+3,2**53+3,2**53+4][i]and row.GetField('part_no')==[1,2,1][i]
        assert row.GetField('feature_key')==row.GetField('source_id')
        assert row.GetField('grade')==[None,None,2**53+1][i]and row.GetField('enabled')==[None,None,True][i]
        assert row.GetField('geometry')==[None,None,'business'][i]and row.GetField('fid')==[None,None,4][i]
        assert bytes(row.GetGeometryRef().ExportToIsoWkb())==shapely.to_wkb(expected[i],byte_order=1,flavor='iso')
    physical_name=layer.GetGeometryColumn();ds=None
    reloaded=get_operator('load')(connection_info=conn,path='parts.gpkg')
    assert reloaded.geometry.name==physical_name and reloaded.crs==loaded_source.crs
    assert reloaded.geometry.to_wkb().tolist()==[p.wkb for p in expected]
    expected_attributes=result.drop(columns=result.geometry.name).copy()
    expected_attributes['part_no']=pd.array(result.part_no,dtype='Int64')
    pd.testing.assert_frame_equal(reloaded.drop(columns=physical_name),expected_attributes)
    assert_geodataframe_equal(loaded_source,before)


@pytest.mark.parametrize('explicit_port',[False,True])
def test_multipart_split_successful_runtime_implicit_and_explicit_result_refs(explicit_port,monkeypatch):
    import workflow_engine
    source=multipart_source([shapely.MultiPoint([(1,2),(3,4)]),Point(5,6)])
    lookup=workflow_engine.get_operator;calls=[]
    def guarded(name):
        calls.append(name)
        if name=='load':return lambda **kw:source
        return lookup(name)
    monkeypatch.setattr(workflow_engine,'get_operator',guarded)
    ref={'$ref':'parts',**({'port':'result'}if explicit_port else{})}
    tasks=[{'id':'load','operator':'load','params':{},'depends_on':[]},
      {'id':'parts','operator':'split_multipart','params':{'input_gdf':{'$ref':'load'},'id_field':'feature_key'},'depends_on':['load']},
      {'id':'bounds','operator':'get_bounds','params':{'input_gdf':ref},'depends_on':['parts']}]
    run=workflow_engine.GeoPythonWorkflowRunner().execute({'tasks':tasks})
    assert calls==['load','split_multipart','get_bounds']and run.task_order==['load','parts','bounds']
    result=json.loads(run.final_result)
    assert len(result['features'])==3
    assert [f['properties']['source_id']for f in result['features']]==[2**53+3,2**53+3,2**53+4]
    assert [f['properties']['part_no']for f in result['features']]==[1,2,1]
    assert [f['properties']['minx']for f in result['features']]==[1,3,5]
    assert [f['geometry']['coordinates']for f in result['features']]==[[1,2],[3,4],[5,6]]
    assert all('geometry'in f['properties']for f in result['features'])


VERTEX_FIELDS = ['source_id','part_no','ring_no','vertex_no','vertex_id']
VERTEX_CASES = [('operator_input_invalid','input_gdf',''),('operator_crs_invalid','','')]+[('operator_field_conflict','input_gdf',f)for f in VERTEX_FIELDS]


def vertices_witness():
    shapes,_,_,_=multipart_mixed_witness()
    shapes[-1]=shapely.MultiPolygon([Polygon([(30,0),(33,0),(33,2),(30,2),(30,0)]),Polygon([(20,0),(20,1),(21,1),(21,0),(20,0)])])
    coordinates=[(2,3),(4,5),(4,5),(-1,-2),(0,0),(2,1),(0,0),
      (9,8),(7,6),(7,6),(3,2),(9,8),(7,6),(7,6),(3,2),(5,0),(5,4),
      (10,10),(10,18),(18,18),(18,10),(10,10),(11,11),(13,11),(13,13),(11,13),(11,11),
      (30,0),(33,0),(33,2),(30,2),(30,0),(20,0),(20,1),(21,1),(21,0),(20,0)]
    parents=[0]+[1]*3+[2]*3+[3]*10+[4]*10+[5]*10
    parts=[1]+[1,2,3]+[1]*3+[1]*4+[2]*4+[3]*2+[1]*10+[1]*5+[2]*5
    rings=[0]*17+[1]*5+[2]*5+[1]*10
    vertices=[1]+[1]*3+[1,2,3]+[1,2,3,4]*2+[1,2]+list(range(1,6))*4
    assert len(coordinates)==len(parents)==len(parts)==len(rings)==len(vertices)==37
    return shapes,coordinates,parents,parts,rings,vertices


@pytest.mark.parametrize('crs',['EPSG:3857','EPSG:4326','OGC:CRS84','EPSG:2263','+proj=aeqd +lat_0=23 +lon_0=113 +datum=WGS84 +units=m +no_defs'])
@pytest.mark.parametrize('id_type',['Int64','UInt64','string'])
def test_vertices_extraction_exact_closures_repeats_parts_holes_attributes_ids_and_order(crs,id_type):
    shapes,xy,parents,parts,rings,numbers=vertices_witness();source=multipart_source(shapes,crs,id_type);before=source.copy(deep=True)
    result=get_operator('extract_vertices')(source,'feature_key')
    assert result.geometry.to_wkb().tolist()==[Point(*p).wkb for p in xy]
    assert shapely.get_coordinates(result.geometry.array).tolist()==[list(p)for p in xy]
    assert result.part_no.tolist()==parts and result.ring_no.tolist()==rings and result.vertex_no.tolist()==numbers
    assert result.vertex_id.tolist()==list(range(1,38))and result.vertex_id.is_unique
    assert result.source_id.tolist()==source.feature_key.iloc[parents].tolist()and str(result.source_id.dtype)==id_type
    assert result.geometry.name=='shape'and result.crs==source.crs and result.index.equals(pd.RangeIndex(37))
    assert list(result.columns)==list(source.columns)+VERTEX_FIELDS
    assert all(str(result[f].dtype)=='int64'for f in VERTEX_FIELDS[1:])
    assert not result[['source_id','part_no','ring_no','vertex_no']].duplicated().any()
    pd.testing.assert_frame_equal(result.drop(columns=['shape',*VERTEX_FIELDS]),source.iloc[parents].drop(columns='shape').reset_index(drop=True))
    assert_geodataframe_equal(source,before)
    # The ring seam and consecutive duplicates are separate occurrences.
    assert xy[7:11]==[(9,8),(7,6),(7,6),(3,2)]
    assert result.vertex_id.iloc[17]!=result.vertex_id.iloc[21]and result.geometry.iloc[17].equals(result.geometry.iloc[21])


@pytest.mark.parametrize('shape,xy',[(LineString([(-0.,0.),(2**-40,2**-40),(2**-40,0.)]),[(-0.,0.),(2**-40,2**-40),(2**-40,0.)]),(Point(1e308,-1e308),[(1e308,-1e308)])])
def test_vertices_extraction_keeps_binary_tiny_coordinates_and_large_finite_positions(shape,xy):
    result=get_operator('extract_vertices')(multipart_source([shape]),'feature_key')
    assert result.geometry.to_wkb().tolist()==[Point(*p).wkb for p in xy]


def test_vertices_extraction_preserves_nonactive_geometry_as_source_attribute():
    source=multipart_source([LineString([(0,0),(1,1)])]);source['inactive_shape']=gpd.GeoSeries([Point(9,9)],index=source.index,crs=3857)
    before=source.copy(deep=True);result=get_operator('extract_vertices')(source,'feature_key')
    assert result.geometry.to_wkt().tolist()==['POINT (0 0)','POINT (1 1)']and result.inactive_shape.to_wkt().tolist()==['POINT (9 9)','POINT (9 9)']
    assert_geodataframe_equal(source,before)


@pytest.mark.parametrize('wkt',['GEOMETRYCOLLECTION (POINT (1 2))','GEOMETRYCOLLECTION EMPTY','POINT EMPTY','LINESTRING EMPTY','POLYGON EMPTY','MULTIPOINT EMPTY','MULTILINESTRING EMPTY','MULTIPOLYGON EMPTY','MULTIPOINT (EMPTY,(1 2))','MULTILINESTRING (EMPTY,(0 0,1 1))','MULTIPOLYGON (EMPTY,((0 0,1 0,1 1,0 0)))','POINT Z (1 2 3)','POINT M (1 2 3)','POINT (Infinity 2)','POINT (NaN 2)','POLYGON ((0 0,2 2,0 2,2 0,0 0))','MULTIPOLYGON (((0 0,2 0,2 2,0 2,0 0)),((1 1,3 1,3 3,1 3,1 1)))',None])
def test_vertices_extraction_refuses_invalid_empty_collections_or_dimensions_atomically(wkt):
    from operators.geometric_operators import VertexInputError
    source=multipart_source([Point(8,9),shapely.from_wkt(wkt)if wkt else None]);before=source.copy(deep=True)
    with pytest.raises(VertexInputError)as caught:get_operator('extract_vertices')(source,'feature_key')
    assert caught.value.code=='source'
    assert source.geometry.to_wkb().tolist()==before.geometry.to_wkb().tolist()
    pd.testing.assert_frame_equal(source.drop(columns='shape'),before.drop(columns='shape'))


@pytest.mark.parametrize('problem',['unknown_id','geometry_id','empty_id_name','non_string_id','null_id','duplicate_id','blank_id','container_id','duplicate_columns','no_active','plain_frame'])
@pytest.mark.parametrize('empty',[False,True])
def test_vertices_extraction_requires_proven_structure_and_source_ids(problem,empty):
    from operators.geometric_operators import VertexInputError
    source=multipart_source([Point(1,2),Point(3,4)]);id_field='feature_key'
    if problem=='unknown_id':id_field='PRIVATE_FIELD'
    elif problem=='geometry_id':id_field='shape'
    elif problem=='empty_id_name':id_field=''
    elif problem=='non_string_id':id_field=False
    elif problem=='null_id':source['feature_key']=pd.array([None,2],dtype='Int64')
    elif problem=='duplicate_id':source['feature_key']=pd.array([2,2],dtype='Int64')
    elif problem=='blank_id':source['feature_key']=pd.array(['ok','  '],dtype='string')
    elif problem=='container_id':source['feature_key']=[[1],[2]]
    elif problem=='duplicate_columns':source.columns=['feature_key','geometry','grade','grade','fid','shape']
    elif problem=='no_active':source=source.drop(columns='shape')
    elif problem=='plain_frame':source=pd.DataFrame(source)
    if empty:source=source.iloc[:0]
    if empty and problem in ['null_id','duplicate_id','blank_id','container_id']:
        assert len(get_operator('extract_vertices')(source,id_field))==0;return
    with pytest.raises(VertexInputError)as caught:get_operator('extract_vertices')(source,id_field)
    assert caught.value.code=='source'


@pytest.mark.parametrize('crs',[None,'EPSG:4978','EPSG:4979','EPSG:7415'])
@pytest.mark.parametrize('empty',[False,True])
def test_vertices_extraction_requires_known_2d_geographic_or_projected_crs(crs,empty):
    from operators.geometric_operators import VertexInputError
    source=multipart_source([]if empty else[Point(1,2)],crs)
    with pytest.raises(VertexInputError)as caught:get_operator('extract_vertices')(source,'feature_key')
    assert caught.value.code=='crs'


@pytest.mark.parametrize('field',VERTEX_FIELDS)
@pytest.mark.parametrize('active',[False,True])
@pytest.mark.parametrize('empty',[False,True])
def test_vertices_extraction_rejects_all_fixed_field_collisions_including_empty_frames(field,active,empty):
    from operators.geometric_operators import VertexInputError
    source=multipart_source([]if empty else[Point(1,2)])
    if active:source=source.rename_geometry(field)
    else:source[field]='PRIVATE'
    before=source.copy(deep=True)
    with pytest.raises(VertexInputError)as caught:get_operator('extract_vertices')(source,'feature_key')
    assert caught.value.code=='field'and caught.value.field==field;assert_geodataframe_equal(source,before)


@pytest.mark.parametrize('id_type',['Int64','UInt64','string'])
def test_vertices_extraction_empty_batch_retains_all_field_types_without_point_placeholder(id_type):
    source=multipart_source([],id_type=id_type);result=get_operator('extract_vertices')(source,'feature_key')
    assert len(result)==0 and result.geometry.name=='shape'and result.crs==source.crs
    assert str(result.source_id.dtype)==id_type and all(str(result[f].dtype)=='int64'for f in VERTEX_FIELDS[1:])
    assert result.index.equals(pd.RangeIndex(0))and list(result.columns)==list(source.columns)+VERTEX_FIELDS


@pytest.mark.parametrize('language',['zh-cn','en'])
@pytest.mark.parametrize('reason,parameter,field',VERTEX_CASES)
def test_vertices_extraction_runtime_diagnostics_bind_original_node_and_stop_save(language,reason,parameter,field,monkeypatch):
    from addp_common.workflow_runtime import WorkflowExecutionError
    from operators.geometric_operators import VertexInputError
    import workflow_engine
    source=multipart_source([Point(1,2)])
    if reason=='operator_input_invalid':source['shape']=[None]
    elif reason=='operator_crs_invalid':source=source.set_crs(None,allow_override=True)
    else:source[field]='PRIVATE'
    lookup=workflow_engine.get_operator;calls=[]
    def guarded(name):
        calls.append(name)
        if name=='load':return lambda **kw:source
        if name=='save':raise AssertionError('invalid vertices reached save')
        return lookup(name)
    monkeypatch.setattr(workflow_engine,'get_operator',guarded)
    tasks=[{'id':'PRIVATE','operator':'extract_vertices','params':{'input_gdf':{'$ref':'load'},'id_field':'feature_key'},'depends_on':['load']},
      {'id':'load','operator':'load','params':{},'depends_on':[]},{'id':'save','operator':'save','params':{},'depends_on':['PRIVATE']}]
    with pytest.raises(WorkflowExecutionError)as caught:workflow_engine.GeoPythonWorkflowRunner(language=language).execute({'tasks':tasks})
    assert calls==['load','extract_vertices']and caught.value.error_code=='INVALID_PARAMS'
    assert caught.value.diagnostic=={'schema_version':'addp.execution-failure/v3','stage':'execution','reason':reason,
      'operator':{'node_index':1,'operator_id':'extract_vertices',**({'parameter':parameter}if parameter else{}),**({'field':field}if field else{})}}
    assert 'PRIVATE'not in json.dumps(caught.value.diagnostic)
    assert VertexInputError({'operator_input_invalid':'source','operator_crs_invalid':'crs','operator_field_conflict':'field'}[reason],field).message(language) in str(caught.value)


@pytest.mark.parametrize('fault',['exception','count','shape','missing','empty','line','z','inf','shifted','reordered'])
def test_vertices_extraction_native_point_faults_are_execution_failures_without_partial_save(fault,monkeypatch):
    import workflow_engine
    from addp_common.workflow_runtime import WorkflowExecutionError
    source=multipart_source([LineString([(1,2),(3,4),(5,6)])]);native=shapely.points
    def broken(xy):
        if fault=='exception':raise RuntimeError('PRIVATE native')
        points=native(xy)
        if fault=='count':return points[:1]
        if fault=='shape':return points.reshape(-1,1)
        if fault=='missing':points[0]=None
        if fault=='empty':points[0]=Point()
        if fault=='line':points[0]=LineString([(1,2),(3,4)])
        if fault=='z':points[0]=Point(1,2,3)
        if fault=='inf':points[0]=Point(float('inf'),2)
        if fault=='shifted':points[0]=Point(9,9)
        if fault=='reordered':return points[::-1]
        return points
    monkeypatch.setattr(shapely,'points',broken);lookup=workflow_engine.get_operator
    def guarded(name):
        if name=='load':return lambda **kw:source
        if name=='save':raise AssertionError('bad native points reached save')
        return lookup(name)
    monkeypatch.setattr(workflow_engine,'get_operator',guarded)
    tasks=[{'id':'load','operator':'load','params':{},'depends_on':[]},
      {'id':'vertices','operator':'extract_vertices','params':{'input_gdf':{'$ref':'load'},'id_field':'feature_key'},'depends_on':['load']},
      {'id':'save','operator':'save','params':{},'depends_on':['vertices']}]
    with pytest.raises(WorkflowExecutionError)as caught:workflow_engine.GeoPythonWorkflowRunner().execute({'tasks':tasks})
    assert caught.value.error_code=='EXECUTION_FAILED'and caught.value.diagnostic is None


@pytest.mark.parametrize('explicit_port',[False,True])
def test_vertices_extraction_successful_workflow_implicit_and_explicit_result_refs(explicit_port,monkeypatch):
    import workflow_engine
    source=multipart_source([LineString([(1,2),(3,4),(1,2)])]);lookup=workflow_engine.get_operator
    def guarded(name):return (lambda **kw:source)if name=='load'else lookup(name)
    monkeypatch.setattr(workflow_engine,'get_operator',guarded)
    tasks=[{'id':'load','operator':'load','params':{},'depends_on':[]},
      {'id':'vertices','operator':'extract_vertices','params':{'input_gdf':{'$ref':'load'},'id_field':'feature_key'},'depends_on':['load']},
      {'id':'bounds','operator':'get_bounds','params':{'input_gdf':{'$ref':'vertices',**({'port':'result'}if explicit_port else{})}},'depends_on':['vertices']}]
    run=workflow_engine.GeoPythonWorkflowRunner().execute({'tasks':tasks});result=json.loads(run.final_result)
    assert run.task_order==['load','vertices','bounds']and len(result['features'])==3
    assert [f['geometry']['coordinates']for f in result['features']]==[[1,2],[3,4],[1,2]]
    assert [f['properties']['vertex_id']for f in result['features']]==[1,2,3]
    assert [f['properties']['vertex_no']for f in result['features']]==[1,2,3]
    assert all(f['properties']['ring_no']==0 for f in result['features'])


@pytest.mark.parametrize('family',['point','line','polygon'])
def test_vertices_extraction_real_geopackage_source_independent_ogr_point_order_and_reload(tmp_path,family):
    from osgeo import ogr
    if family=='point':
        shapes=[shapely.MultiPoint([(1,2),(1,2)]),Point(3,4)];xy=[(1,2),(1,2),(3,4)];parents=[0,0,1];parts=[1,2,1];rings=[0,0,0];vertices=[1,1,1]
    elif family=='line':
        shapes=[MultiLineString([[(1,2),(3,4),(1,2)],[(5,6),(5,6),(7,8)]]),LineString([(9,10),(11,12)])]
        xy=[(1,2),(3,4),(1,2),(5,6),(5,6),(7,8),(9,10),(11,12)];parents=[0]*6+[1]*2;parts=[1]*3+[2]*3+[1]*2;rings=[0]*8;vertices=[1,2,3,1,2,3,1,2]
    else:
        a=Polygon([(0,0),(0,5),(5,5),(5,0),(0,0)], [[(1,1),(2,1),(2,2),(1,2),(1,1)]]);b=Polygon([(10,0),(11,0),(11,2),(10,2),(10,0)])
        shapes=[shapely.MultiPolygon([a,b]),Polygon([(-4,0),(-2,0),(-2,1),(-4,1),(-4,0)])]
        xy=[(0,0),(0,5),(5,5),(5,0),(0,0),(1,1),(2,1),(2,2),(1,2),(1,1),(10,0),(11,0),(11,2),(10,2),(10,0),(-4,0),(-2,0),(-2,1),(-4,1),(-4,0)]
        parents=[0]*15+[1]*5;parts=[1]*10+[2]*5+[1]*5;rings=[1]*5+[2]*5+[1]*10;vertices=list(range(1,6))*4
    source=multipart_source(shapes);conn={'storage_type':'nfs','mount_path':str(tmp_path)}
    get_operator('save')(input_df=source,connection_info=conn,path='source.gpkg',mode='fail')
    loaded=get_operator('load')(connection_info=conn,path='source.gpkg');before=loaded.copy(deep=True);result=get_operator('extract_vertices')(loaded,'feature_key')
    get_operator('save')(input_df=result,connection_info=conn,path='vertices.gpkg',mode='fail')
    ds=ogr.Open(str(tmp_path/'vertices.gpkg'));layer=ds.GetLayer(0);records=list(layer)
    assert layer.GetSpatialRef().GetAuthorityCode(None)=='3857'and len(records)==len(xy)
    for i,feature in enumerate(records):
        parent=parents[i];geom=feature.GetGeometryRef()
        assert geom.GetGeometryType()==ogr.wkbPoint and geom.GetX()==xy[i][0]and geom.GetY()==xy[i][1]
        assert bytes(geom.ExportToIsoWkb())==Point(*xy[i]).wkb
        assert [feature.GetField(f)for f in VERTEX_FIELDS]==[2**53+3+parent,parts[i],rings[i],vertices[i],i+1]
        assert feature.GetField('feature_key')==2**53+3+parent
        assert feature.GetField('grade')==[None,2**53+1][parent]and feature.GetField('enabled')==[None,True][parent]
        assert feature.GetField('geometry')==[None,'business'][parent]and feature.GetField('fid')==[None,4][parent]
    physical_name=layer.GetGeometryColumn();ds=None;reloaded=get_operator('load')(connection_info=conn,path='vertices.gpkg')
    assert reloaded.geometry.name==physical_name and reloaded.crs==loaded.crs
    assert reloaded.geometry.to_wkb().tolist()==[Point(*p).wkb for p in xy]
    expected=result.drop(columns=result.geometry.name).copy()
    for field in VERTEX_FIELDS[1:]:expected[field]=pd.array(expected[field],dtype='Int64')
    pd.testing.assert_frame_equal(reloaded.drop(columns=physical_name),expected)
    assert_geodataframe_equal(loaded,before)


@pytest.mark.parametrize('statement',[
    'UPDATE gpkg_geometry_columns SET srs_id=0',
    "UPDATE gpkg_contents SET srs_id=0 WHERE data_type='features'",
    "UPDATE gpkg_spatial_ref_sys SET organization='EPSG' WHERE srs_id=-1",
    "UPDATE gpkg_spatial_ref_sys SET definition='not an undefined SRS' WHERE srs_id=-1",
])
def test_vertex_unknown_crs_fixture_rejects_physical_identity_drift(tmp_path,statement):
    import importlib
    import sqlite3
    fixture=importlib.import_module('business.scripts.online-vector-topology-fixture')
    path=tmp_path/'unknown.gpkg'
    fixture.seed_vertices(path,'crs')
    assert fixture.verify_vertices(path,'crs')['source_crs'] is None
    with sqlite3.connect(path) as physical:
        physical.execute(statement)
    with pytest.raises(fixture.FixtureError,match='unknown CRS source acquired an identity'):
        fixture.verify_vertices(path,'crs')


def test_vertex_extraction_t4_three_saves_seven_refusals_and_independent_ogr(tmp_path):
    """Runs the proposed DAG against real GPKG bytes; no Online/T4 authentication claimed."""
    import importlib
    from workflow_engine import GeoPythonWorkflowRunner
    from operators.geometric_operators import VertexInputError
    from addp_common.workflow_runtime import WorkflowExecutionError
    fixture=importlib.import_module('business.scripts.online-vector-topology-fixture')
    owner=importlib.import_module('scripts.test.vector-topology-persistence-online')
    root=tmp_path/'vertices';root.mkdir();(root/'outputs').mkdir();(root/'.addp-online-owner').write_text('native-only')
    seeded=fixture.vertices_worker('seed',root)
    connection={'storage_type':'nfs','mount_path':str(root)}
    sources=[]
    for kind,name in zip(fixture.VERTICES_KINDS,fixture.VERTICES_SOURCES):
        fixture.verify_vertices(root/name,kind)
        sources.append(get_operator('load')(connection_info=connection,path=name))
    assert sources[-1].crs is None
    workflow=owner.vertices_definition(list(fixture.VERTICES_SOURCES),'unused')
    for task in workflow['tasks']:
        if task['operator']=='load':task['params']={'connection_info':connection,'path':task['params']['locator']}
        elif task['operator']=='save':task['params']={'input_df':task['params']['input_df'],'connection_info':connection,'path':'outputs/'+task['params']['target_name'],'mode':'fail'}
    GeoPythonWorkflowRunner().execute(workflow)
    for family,source,name in zip(fixture.VERTICES_FAMILIES,sources[:3],fixture.VERTICES_TARGETS):
        before=source.copy(deep=True);result=get_operator('extract_vertices')(source,'feature_key')
        expected=fixture.vertices_features(family+'-result')
        assert len(result)==len(expected)and result.geometry.name=='source_shape'
        assert result.geometry.to_wkb().tolist()==[Point(*row['geometry']['coordinates']).wkb for row in expected]
        assert result.vertex_id.tolist()==list(range(1,len(expected)+1))
        for i,row in enumerate(expected):
            for field in fixture.VERTICES_FIELDS:assert result.iloc[i][field]==row['properties'][field]
        observed=fixture.verify_vertices(root/'outputs'/name,family+'-result')
        assert observed['features']==expected
        reloaded=get_operator('load')(connection_info=connection,path='outputs/'+name)
        assert reloaded.geometry.to_wkb().tolist()==result.geometry.to_wkb().tolist()
        assert_geodataframe_equal(source,before)
    before=fixture.vertices_worker('verify',root)
    assert before['sources']==seeded['sources']and [before['targets'][n]['feature_count'] for n in fixture.VERTICES_TARGETS]==[3,8,20]
    for index,spec in enumerate(fixture.VERTICES_FAILURES,3):
        with pytest.raises(VertexInputError) as failure:get_operator('extract_vertices')(sources[index],'feature_key')
        assert failure.value.code=={'operator_field_conflict':'field','operator_input_invalid':'source','operator_crs_invalid':'crs'}[spec[1]]
        assert fixture.vertices_worker('verify',root)==before
    # Real Runtime failure protocol and dependent save barrier for every rejection.
    import workflow_engine
    original=workflow_engine.get_operator
    def resolve(name):
        if name=='load':return lambda locator:sources[int(locator)]
        if name=='save':return lambda **kw:pytest.fail('vertex rejected batch must never reach save')
        return original(name)
    from unittest.mock import patch
    for index,spec in enumerate(fixture.VERTICES_FAILURES,3):
        tasks=owner.vertices_definition([str(i) for i in range(10)],'unused',spec[0])['tasks']
        with patch.object(workflow_engine,'get_operator',resolve),pytest.raises(WorkflowExecutionError) as failure:GeoPythonWorkflowRunner().execute({'tasks':tasks})
        assert failure.value.error_code=='INVALID_PARAMS'
        assert failure.value.diagnostic==owner.vertices_diagnostic(spec)
        assert fixture.vertices_worker('verify',root)==before


@pytest.mark.parametrize('operator',['check_geometry','repair_geometry'])
@pytest.mark.parametrize('kind',['none','table','inactive','z','m','infinite'])
def test_geometry_validity_typed_admission_only_for_existing_rejections(operator,kind):
    from operators.geometric_operators import GeometryValidityInputError
    source=None if kind=='none' else pd.DataFrame({'x':[1]}) if kind=='table' else gpd.GeoDataFrame({'x':[1]}) if kind=='inactive' else lines([shapely.from_wkt({'z':'POINT Z (1 2 3)','m':'POINT M (1 2 3)','infinite':'POINT (Infinity 2)'}[kind])])
    with pytest.raises(GeometryValidityInputError)as error:get_operator(operator)(source)
    assert error.value.code=='source'


@pytest.mark.parametrize('params',[{'valid_field':None},{'reason_field':[]},{'valid_field':True},{'valid_field':''},{'reason_field':' reason '},
    {'valid_field':'same','reason_field':'same'},{'valid_field':'boundary'},{'reason_field':'boundary'},
    {'valid_field':'PRIVATE_BIZ_FIELD'},{'reason_field':'PRIVATE_BIZ_FIELD'}])
def test_geometry_validity_output_fields_typed_without_disclosing_configured_names(params):
    from operators.geometric_operators import GeometryValidityInputError
    source=lines([Point(1,2)]);source['PRIVATE_BIZ_FIELD']='PRIVATE_VALUE';before=source.copy(deep=True)
    with pytest.raises(GeometryValidityInputError)as error:get_operator('check_geometry')(source,**params)
    assert error.value.code=='fields'
    for locale in ['zh-cn','en']:
        assert 'PRIVATE'not in error.value.message(locale)and 'valid_field'in error.value.message(locale)and 'reason_field'in error.value.message(locale)
    assert_geodataframe_equal(source,before)


@pytest.mark.parametrize('locale',['zh-cn','en'])
@pytest.mark.parametrize('operator,reason,parameter',[('check_geometry','operator_input_invalid','input_gdf'),('check_geometry','operator_output_fields_invalid',''),('repair_geometry','operator_input_invalid','input_gdf')])
def test_geometry_validity_runtime_controlled_context_and_save_barrier(locale,operator,reason,parameter,monkeypatch):
    import workflow_engine
    from addp_common.workflow_runtime import WorkflowExecutionError
    source=lines([Point(1,2)]);source['PRIVATE_FIELD']='PRIVATE_VALUE';before=source.copy(deep=True)
    if reason=='operator_input_invalid':source.loc[source.index[0],source.geometry.name]=shapely.from_wkt('POINT Z (1 2 3)')
    params={'input_gdf':{'$ref':'load'},**({'valid_field':'PRIVATE_FIELD'}if reason=='operator_output_fields_invalid'else{})}
    original=workflow_engine.get_operator;calls=[]
    def resolve(name):
        calls.append(name)
        if name=='load':return lambda **kw:source
        if name=='save':pytest.fail('invalid geometry-check batch reached save')
        return original(name)
    monkeypatch.setattr(workflow_engine,'get_operator',resolve)
    tasks=[{'id':'PRIVATE_NODE','operator':operator,'params':params,'depends_on':['load']},{'id':'load','operator':'load','params':{},'depends_on':[]},{'id':'save','operator':'save','params':{},'depends_on':['PRIVATE_NODE']}]
    with pytest.raises(WorkflowExecutionError)as error:workflow_engine.GeoPythonWorkflowRunner(language=locale).execute({'tasks':tasks})
    assert calls==['load',operator]and error.value.error_code=='INVALID_PARAMS'
    assert error.value.diagnostic=={'schema_version':'addp.execution-failure/v3','stage':'execution','reason':reason,'operator':{'node_index':1,'operator_id':operator,**({'parameter':parameter}if parameter else{})}}
    assert 'PRIVATE'not in json.dumps(error.value.diagnostic)
    if reason=='operator_output_fields_invalid':assert_geodataframe_equal(source,before)


@pytest.mark.parametrize('operator,operation',[('check_geometry','has_z'),('repair_geometry','has_m'),('check_geometry','get_coordinates'),
    ('check_geometry','is_valid'),('check_geometry','is_valid_reason'),('repair_geometry','is_valid'),('repair_geometry','make_valid')])
def test_geometry_validity_native_valueerrors_remain_execution_failures(operator,operation,monkeypatch):
    import workflow_engine
    from addp_common.workflow_runtime import WorkflowExecutionError
    bow=Polygon([(0,0),(2,2),(0,2),(2,0),(0,0)]);source=lines([bow]);before=source.geometry.to_wkb().tolist();original=workflow_engine.get_operator;calls=[]
    def resolve(name):
        calls.append(name)
        if name=='load':return lambda **kw:source
        if name=='save':pytest.fail('native geometry failure reached save')
        function=original(name)
        def execute(*args,**kwargs):
            with monkeypatch.context()as scope:
                scope.setattr(shapely,operation,fail)
                return function(*args,**kwargs)
        return execute
    monkeypatch.setattr(workflow_engine,'get_operator',resolve)
    def fail(*a,**kw):raise ValueError('PRIVATE_FIELD finite XY geometry_valid Z/M invalid field')
    with pytest.raises(WorkflowExecutionError)as error:workflow_engine.GeoPythonWorkflowRunner().execute({'tasks':[
      {'id':'PRIVATE','operator':operator,'params':{'input_gdf':{'$ref':'load'}},'depends_on':['load']},
      {'id':'load','operator':'load','params':{},'depends_on':[]},{'id':'save','operator':'save','params':{},'depends_on':['PRIVATE']}]})
    assert error.value.error_code=='EXECUTION_FAILED'and error.value.diagnostic is None and calls==['load',operator]
    assert source.geometry.to_wkb().tolist()==before


def test_geometry_validity_invalid_repair_result_is_not_parameter_error(monkeypatch):
    import workflow_engine
    from addp_common.workflow_runtime import WorkflowExecutionError
    source=lines([Polygon([(0,0),(2,2),(0,2),(2,0),(0,0)])]);original=workflow_engine.get_operator
    monkeypatch.setattr(workflow_engine,'get_operator',lambda name:(lambda **kw:source) if name=='load'else original(name))
    monkeypatch.setattr(shapely,'make_valid',lambda values,**kw:values)
    with pytest.raises(WorkflowExecutionError)as error:workflow_engine.GeoPythonWorkflowRunner().execute({'tasks':[{'id':'load','operator':'load','params':{},'depends_on':[]},{'id':'repair','operator':'repair_geometry','params':{'input_gdf':{'$ref':'load'}},'depends_on':['load']}]})
    assert error.value.error_code=='EXECUTION_FAILED'and error.value.diagnostic is None


@pytest.mark.parametrize('crs',[None,'EPSG:4326','OGC:CRS84','EPSG:3857'])
def test_geometry_validity_retains_null_empty_collections_invalid_and_source_layout(crs):
    source=lines([None,Point(),shapely.GeometryCollection([Point(2,3),LineString([(0,0),(1,1)])]),Polygon([(0,0),(2,2),(0,2),(2,0),(0,0)])],crs=crs)
    source.index=[7,7,2,9];source['PRIVATE_FIELD']=pd.array([None,True,False,True],dtype='boolean');before=source.copy(deep=True)
    checked=get_operator('check_geometry')(source,valid_field='valid_custom',reason_field='why_custom')
    assert checked.valid_custom.tolist()==[False,True,True,False]and checked.why_custom.iloc[0]=='Missing Geometry'
    assert_geodataframe_equal(checked.drop(columns=['valid_custom','why_custom']),before)
    repaired=get_operator('repair_geometry')(source)
    assert len(repaired)==4 and repaired.index.equals(source.index)and repaired.crs==source.crs and repaired.geometry.name==source.geometry.name
    assert repaired.geometry.iloc[0] is None and repaired.geometry.iloc[1].is_empty and repaired.geometry.iloc[2].equals_exact(source.geometry.iloc[2],0)and repaired.geometry.iloc[3].is_valid
    pd.testing.assert_series_equal(repaired['PRIVATE_FIELD'],source['PRIVATE_FIELD']);assert_geodataframe_equal(source,before)

@pytest.mark.parametrize('side',['source','reference'])
@pytest.mark.parametrize('kind',['none','table','inactive','z','m','infinite','invalid'])
def test_reference_snap_typed_existing_source_and_reference_admissions(side,kind):
    from operators.geometric_operators import ReferenceSnapInputError
    value=None if kind=='none' else pd.DataFrame({'x':[1]}) if kind=='table' else gpd.GeoDataFrame({'x':[1]}) if kind=='inactive' else lines([Polygon([(0,0),(2,2),(0,2),(2,0),(0,0)])if kind=='invalid'else shapely.from_wkt({'z':'POINT Z (1 2 3)','m':'POINT M (1 2 3)','infinite':'POINT (Infinity 2)'}[kind])])
    source,reference=(value,lines([Point(0,0)]))if side=='source'else(lines([Point(1,1)]),value)
    with pytest.raises(ReferenceSnapInputError)as error:get_operator('snap_geometry')(source,reference,0.5)
    assert error.value.code==side


@pytest.mark.parametrize('value',[None,True,np.bool_(False),'0.5',[],float('nan'),float('inf'),-0.1,10**400])
def test_reference_snap_typed_tolerance_keeps_zero_admitted(value):
    from operators.geometric_operators import ReferenceSnapInputError
    with pytest.raises(ReferenceSnapInputError)as error:get_operator('snap_geometry')(lines([Point(0,0)]),lines([Point(1,0)]),value)
    assert error.value.code=='tolerance'
    assert '123.456'not in error.value.message('en')


@pytest.mark.parametrize('source_crs,reference_crs',[(None,None),('EPSG:4326','EPSG:4326'),('EPSG:3857','EPSG:32650'),('EPSG:3857',None)])
def test_reference_snap_typed_crs_admissions_preserve_known_projection_rule(source_crs,reference_crs):
    from operators.geometric_operators import ReferenceSnapInputError
    with pytest.raises(ReferenceSnapInputError)as error:get_operator('snap_geometry')(lines([Point(0,0)],crs=source_crs),lines([Point(1,0)],crs=reference_crs),0.5)
    assert error.value.code=='crs'


@pytest.mark.parametrize('locale',['zh-cn','en'])
@pytest.mark.parametrize('reason,parameter',[('operator_input_invalid','input_gdf'),('operator_reference_invalid','reference_gdf'),('operator_crs_invalid',''),('operator_nonnegative_number_required','tolerance')])
def test_reference_snap_runtime_controlled_context_and_save_barrier(locale,reason,parameter,monkeypatch):
    import workflow_engine
    from addp_common.workflow_runtime import WorkflowExecutionError
    source=lines([Point(0,0)]);reference=lines([Point(1,0)]);tolerance=0.5
    if reason=='operator_input_invalid':source=lines([shapely.from_wkt('POINT Z (1 2 3)')])
    if reason=='operator_reference_invalid':reference=lines([shapely.from_wkt('POINT M (1 2 3)')])
    if reason=='operator_crs_invalid':reference=reference.to_crs('EPSG:4326')
    if reason=='operator_nonnegative_number_required':tolerance=-123.456
    before_source=source.copy(deep=True);before_reference=reference.copy(deep=True);original=workflow_engine.get_operator;calls=[]
    def resolve(name):
        calls.append(name)
        if name=='load':return lambda **kw:source if kw['which']=='source'else reference
        if name=='save':pytest.fail('invalid reference-snap batch reached save')
        return original(name)
    monkeypatch.setattr(workflow_engine,'get_operator',resolve)
    tasks=[{'id':'PRIVATE_NODE','operator':'snap_geometry','params':{'input_gdf':{'$ref':'load_source'},'reference_gdf':{'$ref':'load_reference'},'tolerance':tolerance},'depends_on':['load_source','load_reference']},
      {'id':'load_source','operator':'load','params':{'which':'source'},'depends_on':[]},{'id':'load_reference','operator':'load','params':{'which':'reference'},'depends_on':[]},
      {'id':'save','operator':'save','params':{},'depends_on':['PRIVATE_NODE']}]
    with pytest.raises(WorkflowExecutionError)as error:workflow_engine.GeoPythonWorkflowRunner(language=locale).execute({'tasks':tasks})
    assert calls==['load','load','snap_geometry']and error.value.error_code=='INVALID_PARAMS'
    assert error.value.diagnostic=={'schema_version':'addp.execution-failure/v3','stage':'execution','reason':reason,'operator':{'node_index':1,'operator_id':'snap_geometry',**({'parameter':parameter}if parameter else{})}}
    assert 'PRIVATE'not in json.dumps(error.value.diagnostic)and '123.456'not in json.dumps(error.value.diagnostic)
    assert_geodataframe_equal(source,before_source);assert_geodataframe_equal(reference,before_reference)


@pytest.mark.parametrize('operation',['has_z','has_m','get_coordinates','is_valid','points','STRtree','box','set_coordinates'])
def test_reference_snap_native_valueerrors_remain_execution_failures(operation,monkeypatch):
    import workflow_engine
    from addp_common.workflow_runtime import WorkflowExecutionError
    source=lines([Point(0,0)]);reference=lines([Point(0.5,0)]);before=source.copy(deep=True);original=workflow_engine.get_operator;calls=[]
    def fail(*a,**kw):raise ValueError('PRIVATE_REFERENCE tolerance finite projected CRS invalid XY')
    def resolve(name):
        calls.append(name)
        if name=='load':return lambda **kw:source if kw['which']=='source'else reference
        if name=='save':pytest.fail('native snap failure reached save')
        function=original(name)
        def execute(*args,**kwargs):
            with monkeypatch.context()as scope:
                scope.setattr(shapely,operation,fail)
                return function(*args,**kwargs)
        return execute
    monkeypatch.setattr(workflow_engine,'get_operator',resolve)
    tasks=[{'id':'source','operator':'load','params':{'which':'source'},'depends_on':[]},{'id':'reference','operator':'load','params':{'which':'reference'},'depends_on':[]},
      {'id':'snap','operator':'snap_geometry','params':{'input_gdf':{'$ref':'source'},'reference_gdf':{'$ref':'reference'},'tolerance':1.0},'depends_on':['source','reference']},
      {'id':'save','operator':'save','params':{},'depends_on':['snap']}]
    with pytest.raises(WorkflowExecutionError)as error:workflow_engine.GeoPythonWorkflowRunner().execute({'tasks':tasks})
    assert error.value.error_code=='EXECUTION_FAILED'and error.value.diagnostic is None and calls==['load','load','snap_geometry']
    assert_geodataframe_equal(source,before)


def test_reference_snap_collapsed_output_remains_execution_failure(monkeypatch):
    import workflow_engine
    from addp_common.workflow_runtime import WorkflowExecutionError
    source=lines([LineString([(0,0),(0.5,0)])]);reference=lines([Point(0,0)]);before=source.copy(deep=True);original=workflow_engine.get_operator
    monkeypatch.setattr(workflow_engine,'get_operator',lambda name:(lambda **kw:source if kw['which']=='source'else reference)if name=='load'else original(name))
    tasks=[{'id':'source','operator':'load','params':{'which':'source'},'depends_on':[]},{'id':'reference','operator':'load','params':{'which':'reference'},'depends_on':[]},
      {'id':'snap','operator':'snap_geometry','params':{'input_gdf':{'$ref':'source'},'reference_gdf':{'$ref':'reference'},'tolerance':1.0},'depends_on':['source','reference']}]
    with pytest.raises(WorkflowExecutionError)as error:workflow_engine.GeoPythonWorkflowRunner().execute({'tasks':tasks})
    assert error.value.error_code=='EXECUTION_FAILED'and error.value.diagnostic is None
    assert 'snapping produced invalid geometries'in str(error.value)
    assert_geodataframe_equal(source,before)


@pytest.mark.parametrize('crs',['EPSG:3857','EPSG:2263'])
def test_reference_snap_diagnostics_keep_null_empty_collections_zero_and_empty_reference(crs):
    source=lines([None,Point(),shapely.GeometryCollection([Point(2,3),LineString([(0,0),(1,1)])])],crs=crs);source.index=[7,7,2];source['PRIVATE_FIELD']=pd.array([None,True,False],dtype='boolean');before=source.copy(deep=True)
    for reference,tolerance in [(lines([Point(2.1,3)],crs=crs),0),(lines([None,Point()],crs=crs),1),(lines([],crs=crs),1)]:
        original=reference.copy(deep=True);result=get_operator('snap_geometry')(source,reference,tolerance)
        assert_geodataframe_equal(result,before);assert_geodataframe_equal(reference,original);assert_geodataframe_equal(source,before)
    empty=lines([],crs=crs);assert_geodataframe_equal(get_operator('snap_geometry')(empty,lines([Point(0,0)],crs=crs),1),empty)


@pytest.mark.parametrize('units',[[],[1],[1,2],[float('nan'),float('nan')],[0,0],[-1,-1]])
def test_reference_snap_projected_unit_admission_is_typed_only_at_explicit_rejection(units):
    from types import SimpleNamespace
    from operators.geometric_operators import ReferenceSnapInputError,_require_projected_xy_crs
    crs=SimpleNamespace(is_projected=True,axis_info=[SimpleNamespace(unit_conversion_factor=u)for u in units])
    with pytest.raises(ReferenceSnapInputError)as caught:_require_projected_xy_crs(crs,input_error=ReferenceSnapInputError.crs)
    assert caught.value.code=='crs'
    with pytest.raises(ValueError)as original:_require_projected_xy_crs(crs)
    assert type(original.value)is ValueError


def test_reference_snap_projected_crs_native_property_error_is_not_typed():
    from operators.geometric_operators import ReferenceSnapInputError,_require_projected_xy_crs
    class NativeCRS:
        @property
        def is_projected(self):raise ValueError('PRIVATE_PROJECTED_CRS')
    with pytest.raises(ValueError)as caught:_require_projected_xy_crs(NativeCRS(),input_error=ReferenceSnapInputError.crs)
    assert type(caught.value)is ValueError
