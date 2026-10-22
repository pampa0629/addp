"""Small exhaustive Native checks; never execute the manual capacity matrix here."""
import importlib.util
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
