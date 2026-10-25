from bson import Decimal128, ObjectId
import hashlib
import json
from datetime import date, datetime
from decimal import Decimal

import geopandas as gpd
import pandas as pd
import pyarrow as pa
import pyarrow.feather as feather
import pyarrow.parquet as parquet
import pytest
from osgeo import ogr, osr
from pyogrio.errors import DataSourceError

from operators import io_operators
from operators.io_operators import _load_mongodb_collection, _map_loopback_host


def _load_local(path):
    return io_operators.load(
        connection_info={'engine_type': 'nfs', 'mount_path': str(path.parent)},
        path=path.name,
    )


def _write_arrow_source(path, table):
    # External producer: no pandas dtype metadata and no GDAL writer inference.
    buffer = pa.BufferOutputStream()
    writer = parquet.write_table if path.suffix == '.parquet' else feather.write_feather
    writer(table, buffer)
    path.write_bytes(buffer.getvalue().to_pybytes())


_INTEGER_TYPES = [
    (pa.int8(), 'Int8', -(2**7), 2**7-1),
    (pa.int16(), 'Int16', -(2**15), 2**15-1),
    (pa.int32(), 'Int32', -(2**31), 2**31-1),
    (pa.int64(), 'Int64', -(2**63), 2**63-1),
    (pa.uint8(), 'UInt8', 0, 2**8-1),
    (pa.uint16(), 'UInt16', 0, 2**16-1),
    (pa.uint32(), 'UInt32', 0, 2**32-1),
    (pa.uint64(), 'UInt64', 0, 2**64-1),
]


@pytest.mark.parametrize('fmt', ['parquet', 'feather'])
@pytest.mark.parametrize('arrow_type,dtype,minimum,maximum', _INTEGER_TYPES)
@pytest.mark.parametrize('state', ['nullable', 'present', 'all_null', 'empty'])
def test_load_typed_integer_schema_survives_nulls_and_boundaries(tmp_path, fmt, arrow_type, dtype, minimum, maximum, state):
    values = {'nullable': [minimum, None, maximum], 'present': [minimum, maximum],
              'all_null': [None, None], 'empty': []}[state]
    path = tmp_path / f'整数.{fmt}'
    _write_arrow_source(path, pa.table({'number': pa.array(values, type=arrow_type)}))
    original = path.read_bytes()
    result = _load_local(path)
    pd.testing.assert_series_equal(result.number, pd.Series(values, name='number', dtype=dtype))
    assert path.read_bytes() == original


@pytest.mark.parametrize('fmt', ['parquet', 'feather'])
@pytest.mark.parametrize('values', [[True, None, False], [True, False], [None, None], []])
def test_load_typed_boolean_schema_is_consistent(tmp_path, fmt, values):
    path = tmp_path / f'source.{fmt}'
    _write_arrow_source(path, pa.table({'flag': pa.array(values, type=pa.bool_())}))
    pd.testing.assert_series_equal(_load_local(path).flag, pd.Series(values, name='flag', dtype='boolean'))


@pytest.mark.parametrize('fmt', ['parquet', 'feather'])
def test_load_typed_files_only_changes_integer_and_boolean_projection(tmp_path, fmt):
    table = pa.table({
        'large': pa.array([2**53+1, None, 2**63-1], type=pa.int64()),
        'real': pa.array([1., None, 2.], type=pa.float64()),
        'text': pa.array(['001', None, ''], type=pa.string()),
        'binary': pa.array([b'\x00\xff', None, b''], type=pa.binary()),
        'decimal': pa.array([Decimal('1.20'), None, Decimal('2.00')], type=pa.decimal128(8, 2)),
        'date': pa.array([date(2026, 1, 2), None, date(2026, 1, 3)], type=pa.date32()),
        'timestamp': pa.array([datetime(2026, 1, 2), None, datetime(2026, 1, 3)], type=pa.timestamp('ms')),
    })
    path = tmp_path / f'source.{fmt}'
    _write_arrow_source(path, table)
    result = _load_local(path)
    assert list(result.columns) == table.column_names
    assert result.large.dtype == 'Int64' and result.large.iloc[0] == 2**53+1
    # Declared Real stays Real even when all non-null values are whole numbers.
    pd.testing.assert_frame_equal(pd.DataFrame(result.drop(columns='large')), table.drop(['large']).to_pandas())


def _native_spatial_source(path, fmt, empty=False, epsg=4326):
    ogr.UseExceptions()
    crs = osr.SpatialReference()
    crs.ImportFromEPSG(epsg)
    source = ogr.GetDriverByName(io_operators._spatial_driver(fmt)).CreateDataSource(str(path))
    layer = source.CreateLayer('source', crs, ogr.wkbLineString)
    for name, kind in [('key', ogr.OFTString), ('small', ogr.OFTInteger),
                       ('large', ogr.OFTInteger64), ('flag', ogr.OFTInteger),
                       ('real', ogr.OFTReal), ('text', ogr.OFTString), ('all_null', ogr.OFTInteger64)]:
        field = ogr.FieldDefn(name, kind)
        if name == 'flag':
            field.SetSubType(ogr.OFSTBoolean)
        layer.CreateField(field)
    if not empty:
        for i in range(3):
            feature = ogr.Feature(layer.GetLayerDefn())
            for name, value in [('key', f'row-{i}'), ('small', [1, None, 2**31-1][i]),
                                ('large', [2**53+1, None, 2**63-1][i]),
                                ('flag', [True, None, False][i]), ('real', [1., None, 2.][i]),
                                ('text', ['001', None, ''][i])]:
                if value is not None:
                    feature.SetField(name, value)
            shape = ogr.Geometry(ogr.wkbLineString)
            shape.AddPoint_2D(i, 0)
            shape.AddPoint_2D(i+0.5, 0)
            feature.SetGeometry(shape)
            layer.CreateFeature(feature)
    source = None


@pytest.mark.parametrize('empty', [False, True])
def test_load_gpkg_preserves_native_schema_exact_values_geometry_and_save(tmp_path, empty):
    path = tmp_path / 'source.gpkg'
    _native_spatial_source(path, 'gpkg', empty)
    original = path.read_bytes()
    result = _load_local(path)
    assert result.small.dtype == 'Int32' and result.large.dtype == result.all_null.dtype == 'Int64'
    assert result.flag.dtype == 'boolean' and result.real.dtype == 'float64'
    if not empty:
        assert result.large.iloc[0] == 2**53+1 and result.large.iloc[2] == 2**63-1
        assert result.large.isna().tolist() == [False, True, False]
        assert result.flag.tolist() == [True, pd.NA, False] and result.all_null.isna().all()
        assert result.text.iloc[0] == '001' and result.text.iloc[2] == ''
    assert path.read_bytes() == original
    target = tmp_path / 'saved.gpkg'
    io_operators.save(result, connection_info={'engine_type': 'nfs', 'mount_path': str(tmp_path)}, path=target.name, mode='fail')
    source, saved = ogr.Open(str(path)), ogr.Open(str(target))
    source_layer, saved_layer = source.GetLayer(0), saved.GetLayer(0)
    schema = saved_layer.GetLayerDefn()
    assert schema.GetFieldDefn(schema.GetFieldIndex('large')).GetType() == ogr.OFTInteger64
    assert schema.GetFieldDefn(schema.GetFieldIndex('flag')).GetSubType() == ogr.OFSTBoolean
    before, after = list(source_layer), list(saved_layer)
    assert len(before) == len(after) == len(result)
    assert source_layer.GetSpatialRef().IsSame(saved_layer.GetSpatialRef())
    for original_feature, feature in zip(before, after):
        for name in ['key', 'small', 'large', 'flag', 'real', 'text', 'all_null']:
            assert feature.GetField(name) == original_feature.GetField(name)
        assert feature.GetGeometryRef().ExportToWkb() == original_feature.GetGeometryRef().ExportToWkb()


@pytest.mark.parametrize('fmt', ['shp', 'geojson', 'fgb', 'gml', 'kml'])
def test_load_each_spatial_driver_preserves_its_actual_declared_schema(tmp_path, fmt):
    path = tmp_path / f'source.{fmt}'
    _native_spatial_source(path, fmt)
    original = {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in tmp_path.iterdir()}
    source = ogr.Open(str(path))
    layer = source.GetLayer(0)
    fields = list(layer.schema)
    features = list(layer)
    result = _load_local(path)
    assert len(result) == len(features) == 3
    for field in fields:
        name = field.GetName()
        if field.GetType() in (ogr.OFTInteger, ogr.OFTInteger64):
            expected_dtype = 'boolean' if field.GetSubType() == ogr.OFSTBoolean else (
                'Int64' if field.GetType() == ogr.OFTInteger64 else 'Int16' if field.GetSubType() == ogr.OFSTInt16 else 'Int32')
            expected = [f.GetField(name) if f.IsFieldSetAndNotNull(name) else None for f in features]
            pd.testing.assert_series_equal(result[name], pd.Series(expected, name=name, dtype=expected_dtype))
        else:
            expected = [f.GetField(name) if f.IsFieldSetAndNotNull(name) else None for f in features]
            for actual, value in zip(result[name], expected):
                assert (pd.isna(actual) if value is None else actual == value)
    assert result.geometry.to_wkb().tolist() == [bytes(f.GetGeometryRef().ExportToWkb()) for f in features]
    assert result.crs.equals(layer.GetSpatialRef().ExportToWkt(), ignore_axis_order=True)
    assert {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in tmp_path.iterdir()} == original


def test_load_spatial_nullable_route_is_independent_of_default_engine_and_environment(tmp_path, monkeypatch):
    path = tmp_path / 'source.gpkg'
    _native_spatial_source(path, 'gpkg')
    monkeypatch.setenv('PYOGRIO_USE_ARROW', '0')
    monkeypatch.setattr(gpd.options, 'io_engine', 'fiona')
    result = _load_local(path)
    assert result.large.dtype == 'Int64' and result.large.iloc[0] == 2**53+1


@pytest.mark.parametrize('fmt', ['gpkg', 'parquet', 'feather'])
def test_load_broken_typed_file_fails_without_silent_reclassification(tmp_path, fmt):
    path = tmp_path / f'broken.{fmt}'
    path.write_bytes(b'not a dataset')
    with pytest.raises((DataSourceError, pa.ArrowInvalid)):
        _load_local(path)
    assert path.read_bytes() == b'not a dataset'


@pytest.mark.parametrize('fmt', ['parquet', 'feather'])
def test_load_typed_files_keeps_pandas_index_metadata_and_integer_schema(tmp_path, fmt):
    frame = pd.DataFrame({'large': [2**53+1, 2**53+2], 'label': ['乙', '甲']},
                         index=pd.Index([7, 3], name='original_index'))
    frame['kind'] = pd.Categorical(['a', 'b'])
    path = tmp_path / f'metadata.{fmt}'
    _write_arrow_source(path, pa.Table.from_pandas(frame))
    result = _load_local(path)
    expected = frame.copy()
    expected['large'] = expected.large.astype('Int64')
    expected.index = pd.Index(pd.array(expected.index, dtype='Int64'), name=expected.index.name)
    pd.testing.assert_frame_equal(pd.DataFrame(result), expected)


@pytest.mark.parametrize('fmt', ['parquet', 'feather'])
def test_load_typed_files_save_roundtrip_preserves_physical_integer_nulls(tmp_path, fmt):
    path = tmp_path / f'source.{fmt}'
    values = [2**53+1, None, 2**63-1]
    _write_arrow_source(path, pa.table({'large': pa.array(values, type=pa.int64()),
                                      'flag': pa.array([True, None, False], type=pa.bool_())}))
    result = _load_local(path)
    target = tmp_path / f'saved.{fmt}'
    io_operators.save(result, connection_info={'engine_type': 'nfs', 'mount_path': str(tmp_path)},
                      path=target.name, mode='fail')
    reader = parquet.read_table if fmt == 'parquet' else feather.read_table
    with target.open('rb') as source:
        table = reader(source)
    assert table.schema.field('large').type == pa.int64()
    assert table['large'].to_pylist() == values and table['flag'].to_pylist() == [True, None, False]
    pd.testing.assert_frame_equal(pd.DataFrame(_load_local(target)), pd.DataFrame(result))


def test_load_nullable_types_flow_through_filter_attributes_aggregate_and_topology_dag(tmp_path):
    from operators.attribute_operators import aggregate, rename_fields
    from operators.filter_operators import drop_duplicates, filter_by_attribute, sort_by_field
    from workflow_engine import execute_workflow

    path = tmp_path / 'source.gpkg'
    _native_spatial_source(path, 'gpkg', epsg=3857)
    original = path.read_bytes()
    frame = _load_local(path)
    filtered = filter_by_attribute(frame, 'large', '==', 2**53+1)
    assert filtered.key.tolist() == ['row-0'] and filtered.large.dtype == 'Int64'
    renamed = rename_fields(filtered, {'large': 'identity'})
    assert renamed.identity.iloc[0] == 2**53+1 and renamed.identity.dtype == 'Int64'
    assert drop_duplicates(frame, ['large']).large.dtype == 'Int64'
    ordered = sort_by_field(frame, 'large', ascending=False)
    assert ordered.key.tolist() == ['row-2', 'row-0', 'row-1']
    grouped = aggregate(frame, ['small'], 'sum', group_by=['key'])
    assert grouped.small.tolist() == [1, 0, 2**31-1] and grouped.small.dtype == 'Int32'
    connection = {'engine_type': 'nfs', 'mount_path': str(tmp_path)}
    tasks = [
        {'id': 'read', 'operator': 'load', 'params': {'connection_info': connection, 'path': path.name}, 'depends_on': []},
        {'id': 'group', 'operator': 'group_line_components', 'params': {
            'input_gdf': {'$ref': 'read'}, 'id_field': 'key'}, 'depends_on': ['read']},
        {'id': 'save', 'operator': 'save', 'params': {
            'input_df': {'$ref': 'group'}, 'connection_info': connection,
            'path': 'grouped.gpkg', 'mode': 'fail'}, 'depends_on': ['group']},
    ]
    execution = execute_workflow({'tasks': tasks})
    assert execution['status'] == 'success', execution
    preview = json.loads(execution['all_results']['group'])
    assert preview['features'][0]['properties']['large'] == 2**53+1
    assert preview['features'][1]['properties']['large'] is None
    saved = ogr.Open(str(tmp_path / 'grouped.gpkg'))
    layer = saved.GetLayer(0)
    assert layer.GetLayerDefn().GetFieldDefn(layer.GetLayerDefn().GetFieldIndex('large')).GetType() == ogr.OFTInteger64
    assert [f.GetField('large') for f in layer] == [2**53+1, None, 2**63-1]
    assert path.read_bytes() == original


def test_map_loopback_host_uses_container_shared_host(monkeypatch):
    monkeypatch.setenv('GEOPYTHON_WORKFLOW_LOOPBACK_HOST', 'host.docker.internal')

    assert _map_loopback_host('localhost') == 'host.docker.internal'
    assert _map_loopback_host('127.0.0.1') == 'host.docker.internal'
    assert _map_loopback_host('::1') == 'host.docker.internal'


def test_map_loopback_host_preserves_remote_host(monkeypatch):
    monkeypatch.setenv('GEOPYTHON_WORKFLOW_LOOPBACK_HOST', 'host.docker.internal')

    assert _map_loopback_host('database.internal') == 'database.internal'


def test_map_loopback_host_is_inactive_without_shared_host(monkeypatch):
    monkeypatch.delenv('GEOPYTHON_WORKFLOW_LOOPBACK_HOST', raising=False)

    assert _map_loopback_host('localhost') == 'localhost'


def test_load_mongodb_collection_executes_pipeline_and_normalizes_bson(monkeypatch):
    calls = {}
    object_id = ObjectId()

    class FakeCollection:
        def aggregate(self, pipeline, allowDiskUse):
            calls['pipeline'] = pipeline
            calls['allow_disk_use'] = allowDiskUse
            return [{'_id': object_id, 'amount': Decimal128('1.25')}]

    class FakeDatabase:
        def __getitem__(self, name):
            calls['collection'] = name
            return FakeCollection()

    class FakeClient:
        def __init__(self, **kwargs):
            calls['connection'] = kwargs

        def __getitem__(self, name):
            calls['database'] = name
            return FakeDatabase()

        def close(self):
            calls['closed'] = True

    monkeypatch.setattr(io_operators, 'MongoClient', FakeClient)

    result = _load_mongodb_collection(
        {
            'host': 'localhost', 'port': 27017, 'username': 'reader',
            'password': 'secret', 'auth_source': 'admin',
        },
        'Outdoor',
        'Outdoors',
        [{'$project': {'amount': 1}}],
    )

    assert calls['database'] == 'Outdoor'
    assert calls['collection'] == 'Outdoors'
    assert calls['pipeline'] == [{'$project': {'amount': 1}}]
    assert calls['allow_disk_use'] is True
    assert calls['closed'] is True
    assert result.to_dict('records') == [{'_id': str(object_id), 'amount': Decimal128('1.25').to_decimal()}]


def test_load_mongodb_uses_schema_as_database_when_connection_database_is_empty(monkeypatch):
    calls = {}

    class FakeCollection:
        def find(self, query):
            calls['query'] = query
            return [{'activity_id': 'activity-1'}]

    class FakeDatabase:
        def __getitem__(self, name):
            calls['collection'] = name
            return FakeCollection()

    class FakeClient:
        def __init__(self, **kwargs):
            calls['connection'] = kwargs

        def __getitem__(self, name):
            calls['database'] = name
            return FakeDatabase()

        def close(self):
            calls['closed'] = True

    monkeypatch.setattr(io_operators, 'MongoClient', FakeClient)

    result = io_operators.load(
        connection_info={
            'engine_type': 'mongodb', 'host': 'localhost', 'port': 27017,
            'username': 'reader', 'password': 'secret', 'auth_source': 'admin',
            'database': '',
        },
        schema='Outdoor',
        table='Outdoors',
    )

    assert calls['database'] == 'Outdoor'
    assert calls['collection'] == 'Outdoors'
    assert calls['query'] == {}
    assert calls['closed'] is True
    assert result.to_dict('records') == [{'activity_id': 'activity-1'}]
