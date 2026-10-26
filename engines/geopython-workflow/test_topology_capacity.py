"""Small exhaustive Native checks; never execute the manual capacity matrix here."""
import importlib.util
import json
from pathlib import Path
import sys

import pytest

SCRIPT = Path(__file__).resolve().parents[2] / 'scripts/test/vector-topology-capacity.py'
SPEC = importlib.util.spec_from_file_location('topology_capacity_native', SCRIPT)
m = importlib.util.module_from_spec(SPEC); sys.modules[SPEC.name] = m; SPEC.loader.exec_module(m)


@pytest.mark.parametrize('kind,size,operator', [('many', 6, 'self'), ('many', 6, 'overlap'),
    ('long', 12, 'self'), ('dense', 4, 'self'), ('dense', 4, 'overlap')])
def test_topology_capacity_native_full_coordinate_oracle_and_source_immutability(tmp_path, kind, size, operator):
    case = m.Case(kind, size, operator)
    report = m.run_worker(case, tmp_path)
    assert report['result'] == 'passed'
    assert report['physical']['rows'] == m.expected_rows(case)
    assert report['physical']['all_coordinates_ids_lengths_verified'] is True
    assert set(report['timings']) == {'generate', 'read', 'source_oracle', 'compute', 'save', 'reload_oracle'}
    assert report['worker_peak_rss_bytes'] > 0
    m.validate_worker(report, case)
    samples = [json.loads(line) for line in (tmp_path / 'compute-resources.jsonl').read_text().splitlines()]
    assert len(samples) == report['compute_observation']['samples'] > 0
    assert all(sample['stage'] in {'prepare', 'collect', 'fuse', 'finalize'} for sample in samples)
    # Local Native can run outside Linux cgroups; it must not fabricate memory.
    assert all(sample['memory_current_bytes'] is None or sample['memory_current_bytes'] > 0 for sample in samples)


@pytest.mark.parametrize('corruption', ['id', 'fid-as-id', 'length', 'coordinate', 'missing', 'extra', 'order', 'crs'])
def test_topology_capacity_native_oracle_rejects_real_saved_corruption(tmp_path, corruption):
    from osgeo import ogr
    case = m.Case('many', 6, 'self'); m.run_worker(case, tmp_path)
    path = tmp_path / 'result.gpkg'; ds = ogr.Open(str(path), 1); layer = ds.GetLayer(0)
    if corruption == 'crs': ds.ExecuteSQL('UPDATE gpkg_geometry_columns SET srs_id=4326')
    elif corruption == 'missing': layer.DeleteFeature(6)
    elif corruption == 'extra':
        feature = layer.GetFeature(1); feature.SetFID(-1); assert layer.CreateFeature(feature) == 0
    elif corruption == 'order':
        a, b = layer.GetFeature(1), layer.GetFeature(2)
        a.SetField('source_id', m.source_id(1)); b.SetField('source_id', m.source_id(0))
        assert layer.SetFeature(a) == layer.SetFeature(b) == 0
    else:
        feature = layer.GetFeature(1)
        if corruption == 'id': feature.SetField('source_id', 'foreign')
        elif corruption == 'fid-as-id': feature.SetField('source_id', '1')
        elif corruption == 'length': feature.SetField('overlap_length', 16)
        else:
            shape = feature.GetGeometryRef().Clone(); part = shape.GetGeometryRef(0)
            part.SetPoint_2D(0, .25, 0); feature.SetGeometry(shape)
        assert layer.SetFeature(feature) == 0
    ds = None
    with pytest.raises(AssertionError): m.verify_output(case, path)


def test_topology_capacity_native_observer_modes_preserve_exact_result(tmp_path):
    from operators import geometric_operators as operators
    original=(operators._self_overlap_intersections,operators.shapely.union_all)
    reports=[];case=m.Case('many',6,'self')
    for mode in 'ABC':
        directory=tmp_path/mode;directory.mkdir()
        report=m.run_worker(case,directory,mode,f'trial-{mode.lower()}')
        m.validate_worker(report,case,mode,f'trial-{mode.lower()}')
        assert (directory/'compute-resources.jsonl').exists() is (mode=='C')
        assert (operators._self_overlap_intersections,operators.shapely.union_all)==original
        reports.append(report)
    assert len({r['source_frame_sha256'] for r in reports})==1
    assert len({r['physical']['full_result_sha256'] for r in reports})==1


@pytest.mark.parametrize('kind,size', [('many', 6), ('long', 12), ('dense', 8)])
def test_topology_capacity_native_comparison_variants_keep_exact_oracle(tmp_path, monkeypatch, kind, size):
    import types
    import numpy as np
    import shapely
    from operators import geometric_operators as operators
    from operators.io_operators import load, save
    variants, manifest = m.comparison_sources(SCRIPT.parents[2])
    results = []; case = m.Case(kind, size, 'self')
    for name in ('old', 'new'):
        folder = tmp_path / name; folder.mkdir(); path = folder / 'geometry.py'; path.write_text(variants[name])
        # Compile the complete module to preserve the compiler's imported-name
        # symbol information, but instantiate only the isolated collector definitions.
        codes = compile(variants[name], str(path), 'exec', dont_inherit=True).co_consts
        namespace = {'np': np, 'shapely': shapely, '_SELF_OVERLAP_PAIR_LIMIT': 2048}
        for code in codes:
            if isinstance(code, types.CodeType) and code.co_name in m.COMPARISON_DEFINITIONS:
                namespace[code.co_name] = types.FunctionType(code, namespace)
        module = types.SimpleNamespace(__file__=str(path), _self_overlap_intersections=namespace['_self_overlap_intersections'])
        assert m.loaded_implementation_identity(module) == {**manifest[name], 'loaded_collector_matches_source': True}
        with pytest.raises(AssertionError, match='loaded collector'):
            m.loaded_implementation_identity(types.SimpleNamespace(__file__=str(path), _self_overlap_intersections=lambda parts: iter(())))
        with monkeypatch.context() as patch:
            patch.setattr(operators, '_self_overlap_intersections', module._self_overlap_intersections)
            m.seed(case, folder / 'source.gpkg'); before = m.digest(folder / 'source.gpkg')
            connection = {'engine_type': 'nfs', 'mount_path': str(folder)}
            frame = load(connection_info=connection, path='source.gpkg'); m.verify_source(frame, case)
            original = m.frame_digest(frame)
            result = operators.check_line_self_overlap(frame, 'route_key')
            save(result, connection_info=connection, path='result.gpkg', mode='fail')
            results.append(m.verify_output(case, folder / 'result.gpkg'))
            assert m.frame_digest(frame) == original and m.digest(folder / 'source.gpkg') == before
    assert results[0] == results[1]


@pytest.mark.parametrize('kind,size', [('chain-open',12),('chain-branch',20),('chain-ring',12),('chain-rings',12),('chain-hub',6)])
def test_chain_capacity_native_dual_outputs_and_unwrapped_production(tmp_path, monkeypatch, kind, size):
    def forbidden(*args, **kwargs): raise AssertionError('chain capacity must not wrap geometry functions')
    monkeypatch.setattr(m, 'observe_compute', forbidden)
    case = m.Case(kind, size, 'chains'); report = m.run_worker(case, tmp_path, 'A')
    m.validate_worker(report, case, 'A')
    assert report['physical']['result']['rows'] == m.chain_counts(case)['result']
    assert report['physical']['members']['rows'] == size
    assert report['implementation_identity']['loaded_merge_matches_source'] is True
    assert not (tmp_path/'compute-resources.jsonl').exists()
    if kind == 'chain-branch':
        expected = list(m.chain_expected(case, 'result'))
        assert [v[0]['member_count'] for v in expected[:5]] == [2,1,2,4,1]
        assert expected[0][1] == [[0,0],[2,0],[30,0]]
        assert expected[3][1] == [[10,6],[12,6],[12,8],[10,8],[10,6]]


@pytest.mark.parametrize('port,corruption', [
    ('result','chain_id'),('result','member_count'),('result','grade'),('result','road'),
    ('result','coordinate'),('result','direction'),('result','measured'),('result','missing'),('result','extra'),('result','crs'),('result','int64'),
    ('members','chain_id'),('members','member_no'),('members','source_part_no'),('members','is_reversed'),
    ('members','route_key'),('members','note'),('members','road'),('members','coordinate'),('members','direction'),
    ('members','measured'),('members','missing'),('members','extra'),('members','crs'),('members','boolean'),('members','int64')])
def test_chain_capacity_native_rejects_real_dual_output_corruption(tmp_path, port, corruption):
    from osgeo import ogr
    case = m.Case('chain-branch',10,'chains'); m.run_worker(case,tmp_path,'A');path = tmp_path/(port+'.gpkg')
    if corruption in ('int64','boolean'):
        source = ogr.Open(str(path),0); layer = source.GetLayer(0)
        changed = tmp_path/'changed.gpkg'; destination = ogr.GetDriverByName('GPKG').CreateDataSource(str(changed))
        target = destination.CreateLayer('changed',layer.GetSpatialRef(),ogr.wkbLineString)
        for definition in layer.schema:
            field = ogr.FieldDefn(definition.GetName(),definition.GetType());field.SetSubType(definition.GetSubType())
            if corruption == 'int64' and field.GetName() == 'grade': field.SetType(ogr.OFTInteger)
            if corruption == 'boolean' and field.GetName() == 'is_reversed': field.SetSubType(ogr.OFSTNone)
            target.CreateField(field)
        for feature in layer:
            clone = ogr.Feature(target.GetLayerDefn()); clone.SetFrom(feature); target.CreateFeature(clone)
        source = destination = target = layer = None;changed.replace(path)
    else:
        ds = ogr.Open(str(path),1); layer = ds.GetLayer(0)
        if corruption == 'crs': ds.ExecuteSQL('UPDATE gpkg_geometry_columns SET srs_id=4326')
        elif corruption == 'missing': layer.DeleteFeature(1)
        elif corruption == 'extra':
            feature = layer.GetFeature(1);feature.SetFID(-1);assert layer.CreateFeature(feature)==0
        else:
            feature = layer.GetFeature(1)
            if corruption in ('coordinate','direction','measured'):
                points = feature.GetGeometryRef().GetPoints()
                shape = ogr.Geometry(ogr.wkbLineStringM if corruption == 'measured' else ogr.wkbLineString)
                for x,y,*_ in (points[::-1] if corruption == 'direction' else points):
                    if corruption == 'measured': shape.AddPointM(x,y,1)
                    else: shape.AddPoint_2D(x+.25 if corruption == 'coordinate' else x,y)
                feature.SetGeometry(shape)
            elif corruption == 'is_reversed': feature.SetField(corruption,1-feature.GetField(corruption))
            elif corruption in ('route_key','note','road'): feature.SetField(corruption,'foreign')
            else: feature.SetField(corruption,feature.GetField(corruption)+1)
            assert layer.SetFeature(feature)==0
        ds = layer = None
    with pytest.raises(AssertionError): m.verify_chain_output(case,path,port)


def test_chain_capacity_native_source_all_columns_are_immutable_and_verified(tmp_path):
    from operators.io_operators import load
    case = m.Case('chain-open',12,'chains');m.seed_chains(case,tmp_path/'source.gpkg')
    frame = load(connection_info={'engine_type':'nfs','mount_path':str(tmp_path)},path='source.gpkg')
    m.verify_chain_source(frame,case); original = m.attributed_frame_digest(frame)
    for field,value in [('route_key','foreign'),('note','foreign'),('grade',8),('road','foreign')]:
        changed=frame.copy();changed.at[0,field]=value
        assert m.attributed_frame_digest(changed)!=original
        with pytest.raises(AssertionError):m.verify_chain_source(changed,case)


@pytest.mark.parametrize('dtype', ['int64', 'float64', 'object', 'Int32'])
def test_chain_capacity_native_source_requires_the_single_nullable_schema(tmp_path, dtype):
    from osgeo import ogr
    from operators.io_operators import load
    case = m.Case('chain-open', 12, 'chains')
    path = tmp_path/'source.gpkg'; m.seed_chains(case, path)
    source = ogr.Open(str(path), 0); layer = source.GetLayer(0)
    field = layer.GetLayerDefn().GetFieldDefn(layer.GetLayerDefn().GetFieldIndex('grade'))
    assert field.GetType() == ogr.OFTInteger64 and field.GetSubType() == ogr.OFSTNone
    source = layer = None
    frame = load(connection_info={'engine_type':'nfs','mount_path':str(tmp_path)}, path=path.name)
    assert str(frame.grade.dtype) == 'Int64'
    m.verify_chain_source(frame, case)
    changed = frame.copy(); changed['grade'] = changed.grade.astype(dtype)
    assert changed.grade.tolist() == frame.grade.tolist()
    assert m.attributed_frame_digest(changed) != m.attributed_frame_digest(frame)
    with pytest.raises(AssertionError): m.verify_chain_source(changed, case)


@pytest.mark.parametrize('change', ['values', 'name', 'dtype'])
def test_chain_capacity_native_source_digest_and_oracle_include_index_metadata(tmp_path, change):
    import pandas as pd
    from operators.io_operators import load
    case = m.Case('chain-open', 12, 'chains'); m.seed_chains(case, tmp_path/'source.gpkg')
    frame = load(connection_info={'engine_type':'nfs','mount_path':str(tmp_path)}, path='source.gpkg')
    m.verify_chain_source(frame, case); changed = frame.copy()
    if change == 'values': changed.index = pd.RangeIndex(1, len(frame)+1)
    elif change == 'name': changed.index = changed.index.rename('foreign')
    else: changed.index = changed.index.astype('Int64')
    assert m.attributed_frame_digest(changed) != m.attributed_frame_digest(frame)
    with pytest.raises(AssertionError): m.verify_chain_source(changed, case)


def test_chain_capacity_native_nullable_schema_exact_dual_save_and_source_invariance(tmp_path):
    import pandas as pd
    from geopandas.testing import assert_geodataframe_equal
    from osgeo import ogr, osr
    from operators.geometric_operators import merge_line_chains
    from operators.io_operators import load, save

    # This bounded companion is not a new capacity scene. Native construction
    # makes Integer64/Boolean and nulls independent of pandas writer inference.
    ogr.UseExceptions(); path = tmp_path/'nullable.gpkg'
    crs = osr.SpatialReference(); crs.ImportFromEPSG(3857)
    ds = ogr.GetDriverByName('GPKG').CreateDataSource(str(path))
    layer = ds.CreateLayer('nullable', crs, ogr.wkbLineString)
    for name, kind in [('route_key',ogr.OFTString),('note',ogr.OFTString),
                       ('grade',ogr.OFTInteger64),('road',ogr.OFTString),('enabled',ogr.OFTInteger)]:
        field = ogr.FieldDefn(name, kind)
        if name == 'enabled': field.SetSubType(ogr.OFSTBoolean)
        layer.CreateField(field)
    grades = [2**53+1, 2**53+1, None, -(2**63)]
    flags = [True, True, None, False]
    notes = [None, 'reverse', 'null', 'minimum']
    points = [[(0,0),(1,0)],[(2,0),(1,0)],[(2,0),(3,0)],[(3,0),(4,0)]]
    for row in range(4):
        feature = ogr.Feature(layer.GetLayerDefn())
        for name, value in [('route_key',f'row-{row}'),('note',notes[row]),
                            ('grade',grades[row]),('road','main'),('enabled',flags[row])]:
            if value is not None: feature.SetField(name, value)
        shape = ogr.Geometry(ogr.wkbLineString)
        for x,y in points[row]: shape.AddPoint_2D(x,y)
        feature.SetGeometry(shape); assert layer.CreateFeature(feature) == 0
    ds = layer = None; physical_before = m.digest(path)
    connection = {'engine_type':'nfs','mount_path':str(tmp_path)}
    frame = load(connection_info=connection, path=path.name); before = frame.copy(deep=True)
    assert str(frame.grade.dtype) == 'Int64' and str(frame.enabled.dtype) == 'boolean'
    pd.testing.assert_series_equal(frame.grade, pd.Series(grades, name='grade', dtype='Int64'))
    pd.testing.assert_series_equal(frame.enabled, pd.Series(flags, name='enabled', dtype='boolean'))
    assert frame.index.equals(pd.RangeIndex(4)) and frame.index.name is None
    outputs = merge_line_chains(frame, 'route_key', ['road','grade','enabled'])
    pd.testing.assert_series_equal(outputs['result'].grade,
        pd.Series([2**53+1,None,-(2**63)], name='grade', dtype='Int64'))
    pd.testing.assert_series_equal(outputs['result'].enabled,
        pd.Series([True,None,False], name='enabled', dtype='boolean'))
    assert outputs['members'].route_key.tolist() == [f'row-{i}' for i in range(4)]
    assert outputs['members'].is_reversed.tolist() == [False,True,False,False]
    assert_geodataframe_equal(outputs['members'][frame.columns], frame)
    for port in ['result','members']:
        saved = tmp_path/(port+'.gpkg'); save(outputs[port], connection_info=connection, path=saved.name, mode='fail')
        ds = ogr.Open(str(saved), 0); layer = ds.GetLayer(0); schema = layer.GetLayerDefn()
        assert layer.GetSpatialRef().GetAuthorityCode(None) == '3857'
        assert schema.GetFieldDefn(schema.GetFieldIndex('grade')).GetType() == ogr.OFTInteger64
        assert schema.GetFieldDefn(schema.GetFieldIndex('enabled')).GetSubType() == ogr.OFSTBoolean
        features = list(layer)
        expected_grades = [2**53+1,None,-(2**63)] if port == 'result' else grades
        expected_flags = [True,None,False] if port == 'result' else flags
        expected_points = [[(0,0),(1,0),(2,0)],points[2],points[3]] if port == 'result' else points
        assert [f.GetField('grade') for f in features] == expected_grades
        assert [f.GetField('enabled') for f in features] == expected_flags
        assert [list(f.GetGeometryRef().GetPoints()) for f in features] == expected_points
        assert all(f.GetGeometryRef().GetCoordinateDimension() == 2 and not f.GetGeometryRef().IsMeasured()
                   and f.GetGeometryRef().IsValid() for f in features)
        if port == 'members': assert [f.GetField('note') for f in features] == notes
        ds = layer = None
        reloaded = load(connection_info=connection, path=saved.name)
        assert str(reloaded.grade.dtype) == 'Int64' and str(reloaded.enabled.dtype) == 'boolean'
        pd.testing.assert_series_equal(reloaded.grade, outputs[port].grade)
        pd.testing.assert_series_equal(reloaded.enabled, outputs[port].enabled)
    assert_geodataframe_equal(frame, before)
    assert m.digest(path) == physical_before


@pytest.mark.parametrize('kind,size',[('normalize-sparse',8),('normalize-duplicates',7),('normalize-parts',7),('normalize-grid',3),('normalize-sampling',4)])
def test_normalize_capacity_native_dual_output_exact_oracle_unwrapped(tmp_path,monkeypatch,kind,size):
    def forbidden(*a,**kw):raise AssertionError('normalize must not run old profile or wrap production')
    monkeypatch.setattr(m,'observe_compute',forbidden);monkeypatch.setattr(m,'seed_chains',forbidden)
    case=m.Case(kind,size,'normalize');report=m.run_worker(case,tmp_path,'A');m.validate_worker(report,case,'A')
    assert report['implementation_identity']['loaded_normalize_matches_source'] is True
    assert [report['physical'][p]['rows'] for p in ('result','members')]==[m.normalize_counts(case)[p] for p in ('result','members')]
    assert not (tmp_path/'compute-resources.jsonl').exists()


@pytest.mark.parametrize('port,problem',[
    ('result','edge_id'),('result','occurrence_count'),('result','coordinate'),('result','direction'),('result','missing'),('result','extra'),('result','crs'),('result','int64'),('result','measured'),
    ('members','edge_id'),('members','source_id'),('members','source_part_no'),('members','source_segment_no'),('members','piece_no'),('members','is_reversed'),
    ('members','route_key'),('members','note'),('members','null_grade'),('members','null_enabled'),('members','direction'),('members','missing'),('members','extra'),('members','crs'),('members','boolean'),('members','int64')])
def test_normalize_capacity_native_rejects_real_dual_output_corruption(tmp_path,port,problem):
    from osgeo import ogr
    case=m.Case('normalize-sampling',4,'normalize');m.run_worker(case,tmp_path,'A');path=tmp_path/(port+'.gpkg')
    if problem in ('int64','boolean'):
        source=ogr.Open(str(path),0);layer=source.GetLayer(0);changed=tmp_path/'changed.gpkg'
        destination=ogr.GetDriverByName('GPKG').CreateDataSource(str(changed));target=destination.CreateLayer('changed',layer.GetSpatialRef(),ogr.wkbLineString)
        for definition in layer.schema:
            field=ogr.FieldDefn(definition.GetName(),definition.GetType());field.SetSubType(definition.GetSubType())
            if problem=='int64' and field.GetName()==('edge_id' if port=='result' else 'grade'):field.SetType(ogr.OFTReal)
            if problem=='boolean' and field.GetName()=='enabled':field.SetSubType(ogr.OFSTNone)
            target.CreateField(field)
        for feature in layer:
            clone=ogr.Feature(target.GetLayerDefn());clone.SetFrom(feature);target.CreateFeature(clone)
        source=destination=target=layer=None;changed.replace(path)
    else:
        ds=ogr.Open(str(path),1);layer=ds.GetLayer(0)
        if problem=='crs':ds.ExecuteSQL('UPDATE gpkg_geometry_columns SET srs_id=4326')
        elif problem=='missing':layer.DeleteFeature(1)
        elif problem=='extra':
            feature=layer.GetFeature(1);feature.SetFID(-1);assert layer.CreateFeature(feature)==0
        else:
            feature=layer.GetFeature(5 if problem.startswith('null_') else 1)
            if problem in ('coordinate','direction','measured'):
                points=feature.GetGeometryRef().GetPoints();shape=ogr.Geometry(ogr.wkbLineStringM if problem=='measured' else ogr.wkbLineString)
                for x,y,*_ in (points[::-1] if problem=='direction' else points):
                    if problem=='measured':shape.AddPointM(x,y,1)
                    else:shape.AddPoint_2D(x+.25 if problem=='coordinate' else x,y)
                feature.SetGeometry(shape)
            elif problem in ('route_key','note','source_id'):feature.SetField(problem,'foreign')
            elif problem.startswith('null_'):feature.SetField(problem.removeprefix('null_'),0)
            elif problem=='is_reversed':feature.SetField(problem,1-feature.GetField(problem))
            else:feature.SetField(problem,feature.GetField(problem)+1)
            assert layer.SetFeature(feature)==0
        ds=layer=None
    with pytest.raises(AssertionError):m.verify_normalize_output(case,path,port)


def test_normalize_capacity_native_source_schema_and_index_are_immutable(tmp_path):
    from operators.io_operators import load
    case=m.Case('normalize-sampling',4,'normalize');m.seed_normalize(case,tmp_path/'source.gpkg')
    frame=load(connection_info={'engine_type':'nfs','mount_path':str(tmp_path)},path='source.gpkg');m.verify_normalize_source(frame,case)
    original=m.attributed_frame_digest(frame)
    for column,dtype in [('grade','float64'),('enabled','object')]:
        changed=frame.copy();changed[column]=changed[column].astype(dtype)
        assert m.attributed_frame_digest(changed)!=original
        with pytest.raises(AssertionError):m.verify_normalize_source(changed,case)
    changed=frame.copy();changed.index.name='foreign'
    assert m.attributed_frame_digest(changed)!=original
    with pytest.raises(AssertionError):m.verify_normalize_source(changed,case)


def test_normalize_capacity_native_oracle_failure_retains_incomplete_stage(tmp_path,monkeypatch):
    def reject(*args):raise RuntimeError('independent physical oracle failed')
    monkeypatch.setattr(m,'verify_normalize_output',reject)
    with pytest.raises(RuntimeError,match='physical oracle failed'):
        m.run_worker(m.Case('normalize-sampling',4,'normalize'),tmp_path,'A')
    progress=json.loads((tmp_path/'progress.json').read_text())
    assert progress['result']=='incomplete' and progress['stage']=='reload_oracle'
    assert 'save_result' in progress['timings'] and 'save_members' in progress['timings']
