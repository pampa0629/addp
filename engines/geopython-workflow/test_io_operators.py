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


@pytest.mark.parametrize('order', ['gdal-first', 'arrow-first'])
def test_native_arrow_filesystem_parquet_admission(order):
    # Preparation and Native CI exercise the identical admitted library graph.
    # A new interpreter is required; the suite itself already imported GIS.
    import sys
    from pathlib import Path
    from operators._native_process import run
    helper = Path(__file__).resolve().parents[2] / 'scripts/dev/gdal-arrow.py'
    result = run([sys.executable, str(helper), 'admit', '--order', order],
                 capture_output=True, text=True, timeout=60, check=False)
    assert result.returncode == 0, result.stdout + result.stderr
    evidence = json.loads(result.stdout.splitlines()[-1])
    assert evidence['result'] == 'passed' and evidence['order'] == order


# Native file geometry identity must not consume an ordinary "geometry" column.
def _native_geometry_identity_source(path,fmt,empty=False,null_geometry=False):
    ogr.UseExceptions();crs=osr.SpatialReference();crs.ImportFromEPSG(4326)
    ds=ogr.GetDriverByName(io_operators._spatial_driver(fmt)).CreateDataSource(str(path))
    options=["GEOMETRY_NAME=parcel_shape"] if fmt=="gpkg" else []
    layer=ds.CreateLayer("source",crs,ogr.wkbLineString,options=options)
    for name,kind in [("record_id",ogr.OFTInteger64),("geometry",ogr.OFTString),("ordinary",ogr.OFTString),("enabled",ogr.OFTInteger),("amount",ogr.OFTReal)]:
        field=ogr.FieldDefn(name,kind)
        if name=="enabled":field.SetSubType(ogr.OFSTBoolean)
        assert layer.CreateField(field)==0
    if not empty:
        for row in range(3):
            f=ogr.Feature(layer.GetLayerDefn());f.SetField("record_id",2**53+1+row)
            if row!=1:f.SetField("geometry",f"business-{row}");f.SetField("enabled",row==0);f.SetField("amount",float(row))
            f.SetField("ordinary","not-WKB-"+str(row))
            if not null_geometry:
                line=ogr.Geometry(ogr.wkbLineString);line.AddPoint_2D(row,0);line.AddPoint_2D(row+0.5,0);f.SetGeometry(line)
            assert layer.CreateFeature(f)==0
        f=None
    layer=ds=None


@pytest.mark.parametrize("fmt",["gpkg","shp","geojson","kml","gml","fgb"])
def test_load_spatial_geometry_identity_keeps_real_geometry_and_every_business_field(tmp_path,fmt):
    import pyogrio
    path=tmp_path/("source."+fmt);_native_geometry_identity_source(path,fmt)
    ds=ogr.Open(str(path));layer=ds.GetLayer(0);features=list(layer);fields=list(layer.schema)
    assert "geometry" in [f.name for f in fields]
    meta,table=pyogrio.read_arrow(path)
    geometry_fields=[f.name for f in table.schema if (f.metadata or {}).get(b"ARROW:extension:name") in (b"geoarrow.wkb",b"ogc.wkb")]
    assert len(geometry_fields)==1;physical=layer.GetGeometryColumn();expected_name=physical or geometry_fields[0]
    before={p.name:hashlib.sha256(p.read_bytes()).hexdigest() for p in tmp_path.iterdir()}
    loaded=_load_local(path)
    assert loaded.geometry.name==expected_name and loaded.geometry.name!="geometry"
    assert loaded.columns.tolist()==table.schema.names
    for field in fields:
        values=[f.GetField(field.name) if f.IsFieldSetAndNotNull(field.name) else None for f in features]
        for actual,wanted in zip(loaded[field.name],values):assert pd.isna(actual) if wanted is None else actual==wanted
    assert loaded.geometry.to_wkb().tolist()==[bytes(f.GetGeometryRef().ExportToWkb()) for f in features]
    assert loaded.crs.equals(layer.GetSpatialRef().ExportToWkt(),ignore_axis_order=True)
    assert {p.name:hashlib.sha256(p.read_bytes()).hexdigest() for p in tmp_path.iterdir()}==before
    if physical:
        selected=io_operators.load(connection_info={"engine_type":"nfs","mount_path":str(tmp_path)},path=path.name,geom_column=physical)
        pd.testing.assert_frame_equal(loaded,selected)
        with pytest.raises(ValueError,match="geometry"):io_operators._read_file(str(path),fmt,"geometry")
    else:
        selected=io_operators._read_file(str(path),fmt,"selected_shape")
        assert selected.geometry.name=="selected_shape" and selected.geometry.to_wkb().tolist()==loaded.geometry.to_wkb().tolist()
        pd.testing.assert_series_equal(selected["geometry"],loaded["geometry"])
        with pytest.raises(ValueError,match="conflict"):io_operators._read_file(str(path),fmt,"geometry")


@pytest.mark.parametrize("empty,null_geometry",[(True,False),(False,True)])
def test_load_spatial_geometry_identity_empty_or_all_null_keeps_complete_schema(tmp_path,empty,null_geometry):
    path=tmp_path/"source.gpkg";_native_geometry_identity_source(path,"gpkg",empty,null_geometry)
    loaded=_load_local(path)
    assert loaded.geometry.name=="parcel_shape" and len(loaded)==(0 if empty else 3)
    assert loaded.geometry.isna().all() and loaded.crs.to_epsg()==4326
    assert loaded.record_id.dtype=="Int64" and loaded.enabled.dtype=="boolean"
    assert loaded.geometry.name!="geometry" and loaded["geometry"].tolist()==([] if empty else ["business-0",None,"business-2"])


def test_load_spatial_geometry_identity_preserves_plain_nonspatial_table(tmp_path):
    ds=ogr.GetDriverByName("GPKG").CreateDataSource(str(tmp_path/"table.gpkg"));layer=ds.CreateLayer("plain",geom_type=ogr.wkbNone)
    layer.CreateField(ogr.FieldDefn("geometry",ogr.OFTString));f=ogr.Feature(layer.GetLayerDefn());f.SetField("geometry","business");layer.CreateFeature(f);f=layer=ds=None
    value=_load_local(tmp_path/"table.gpkg")
    assert isinstance(value,pd.DataFrame) and not isinstance(value,gpd.GeoDataFrame) and value["geometry"].tolist()==["business"]
    with pytest.raises(ValueError,match="geometry"):io_operators._read_file(str(tmp_path/"table.gpkg"),"gpkg","geometry")


@pytest.mark.parametrize("physical",[False,True])
@pytest.mark.parametrize("problem",["duplicate","missing_marker","multiple_geometry","physical_mismatch","business_binary","wrong_request","empty_request"])
def test_load_spatial_geometry_identity_rejects_ambiguous_or_conflicting_schema(monkeypatch,physical,problem):
    import pyogrio,shapely
    data={"geometry":pa.array(["business"],type=pa.string()),"payload":pa.array([b"not a geometry"],type=pa.binary())}
    shape=pa.field("shape",pa.binary(),metadata={b"ARROW:extension:name":b"geoarrow.wkb"})
    fields=[pa.field(n,a.type) for n,a in data.items()]+[shape]
    values=[*data.values(),pa.array([shapely.Point(0,0).wkb],type=pa.binary())]
    meta={"crs":"EPSG:4326","geometry_name":"shape" if physical else "","geometry_type":"Point"}
    request=None
    if problem=="duplicate":fields[0]=pa.field("shape",pa.string())
    elif problem=="missing_marker":fields[-1]=pa.field("shape",pa.binary())
    elif problem=="multiple_geometry":fields[1]=pa.field("payload",pa.binary(),metadata={b"ARROW:extension:name":b"geoarrow.wkb"})
    elif problem=="physical_mismatch":meta["geometry_name"]="other"
    elif problem=="business_binary":meta["geometry_name"]="payload"
    elif problem=="wrong_request":request="payload"
    else:request=""
    table=pa.Table.from_arrays(values,schema=pa.schema(fields))
    monkeypatch.setattr(pyogrio,"read_arrow",lambda *a,**kw:(meta,table))
    with pytest.raises(ValueError):io_operators._read_file("file.gpkg","gpkg",request)


@pytest.mark.parametrize("extension",[b"geoarrow.wkb",b"ogc.wkb"])
def test_load_spatial_geometry_identity_geometry_marker_is_not_a_column_name_guess(monkeypatch,extension):
    import pyogrio,shapely
    table=pa.Table.from_arrays([pa.array([b"ordinary blob"]),pa.array(["business"]),pa.array([shapely.Point(1,2).wkb])],schema=pa.schema([
        pa.field("wkb_geometry",pa.binary()),pa.field("geometry",pa.string()),pa.field("real_shape",pa.binary(),metadata={b"ARROW:extension:name":extension})]))
    monkeypatch.setattr(pyogrio,"read_arrow",lambda *a,**kw:({"crs":None,"geometry_name":"real_shape","geometry_type":"Point"},table))
    loaded=io_operators._read_file("file.gpkg","gpkg")
    assert loaded.geometry.name=="real_shape" and loaded["geometry"].tolist()==["business"] and loaded["wkb_geometry"].tolist()==[b"ordinary blob"]
    assert loaded.geometry.iloc[0].wkt=="POINT (1 2)" and loaded.crs is None


@pytest.mark.parametrize("wkt",["POINT ZM (1 2 3 4)","POINT Z (1 2 3)","POINT M (1 2 4)","POINT EMPTY",None])
def test_load_spatial_geometry_identity_preserves_dimensions_empty_and_missing(tmp_path,wkt):
    import shapely
    path=tmp_path/"source.gpkg";source=ogr.CreateGeometryFromWkt(wkt) if wkt else None
    ds=ogr.GetDriverByName("GPKG").CreateDataSource(str(path));layer=ds.CreateLayer("source",geom_type=source.GetGeometryType() if source is not None else ogr.wkbPoint,options=["GEOMETRY_NAME=raw_shape"])
    layer.CreateField(ogr.FieldDefn("geometry",ogr.OFTString));feature=ogr.Feature(layer.GetLayerDefn());feature.SetField("geometry","business")
    if source is not None:feature.SetGeometry(source)
    assert layer.CreateFeature(feature)==0;feature=layer=ds=None
    ds=ogr.Open(str(path));feature=ds.GetLayer(0).GetFeature(1);source=feature.GetGeometryRef();wanted=bytes(source.ExportToIsoWkb()) if source is not None else None
    expected_z=source.Is3D() if source is not None else False;expected_m=source.IsMeasured() if source is not None else False;expected_empty=source.IsEmpty() if source is not None else False
    original=path.read_bytes();result=_load_local(path);geometry=result.geometry.iloc[0]
    assert result.geometry.name=="raw_shape" and result["geometry"].tolist()==["business"]
    assert (None if geometry is None else shapely.to_wkb(geometry,flavor="iso",output_dimension=4))==wanted
    if geometry is not None:assert geometry.has_z==expected_z and geometry.has_m==expected_m and geometry.is_empty==expected_empty
    assert path.read_bytes()==original


def test_load_spatial_geometry_identity_invalid_wkb_fails_without_ignoring(monkeypatch):
    import pyogrio,shapely
    table=pa.Table.from_arrays([pa.array([b"invalid WKB"])],schema=pa.schema([pa.field("shape",pa.binary(),metadata={b"ARROW:extension:name":b"geoarrow.wkb"})]))
    monkeypatch.setattr(pyogrio,"read_arrow",lambda *a,**kw:({"geometry_name":"shape","geometry_type":"Point","crs":None},table))
    with pytest.raises(shapely.errors.GEOSException):io_operators._read_file("file.gpkg","gpkg")
    assert table.column("shape").to_pylist()==[b"invalid WKB"]



def _loaded_geometry_consumer_source(tmp_path):
    shapes=gpd.GeoSeries.from_wkt(["POLYGON ((0 0,2 0,2 1,0 1,0 0))","POLYGON ((10 0,13 0,13 3,10 3,10 0))"],crs=3857)
    frame=gpd.GeoDataFrame({"geometry":["业务甲","业务乙"],"value":pd.Series([5,7],dtype="Int64"),"parcel_shape":shapes},geometry="parcel_shape")
    frame.to_file(tmp_path/"consumer.gpkg",driver="GPKG",index=False)
    return _load_local(tmp_path/"consumer.gpkg")


@pytest.mark.parametrize("operator",["get_area","get_length","get_bounds","split_by_area","export_to_wkt","aggregate"])
def test_load_spatial_geometry_identity_consumers_keep_active_geometry_and_business_fields(tmp_path,operator):
    from operators import get_operator
    from geopandas.testing import assert_geodataframe_equal
    frame=_loaded_geometry_consumer_source(tmp_path);before=frame.copy(deep=True);op=get_operator(operator)
    if operator=="split_by_area":
        result=op(frame,2.)
        assert result["large"].value.tolist()==[7] and result["small"].value.tolist()==[5]
        for value in result.values():assert value.geometry.name==frame.geometry.name and set(value["geometry"])<=set(frame["geometry"])
    elif operator=="export_to_wkt":assert op(frame)==["POLYGON ((0 0, 2 0, 2 1, 0 1, 0 0))","POLYGON ((10 0, 13 0, 13 3, 10 3, 10 0))"]
    elif operator=="aggregate":
        result=op(frame,["value"],"sum",["geometry"])
        assert result["geometry"].tolist()==sorted(["业务甲","业务乙"])
        assert dict(zip(result["geometry"],result.value))=={"业务甲":5,"业务乙":7}
        assert frame.geometry.name not in result.columns
    else:
        result=op(frame);assert result.geometry.name==frame.geometry.name and result["geometry"].tolist()==frame["geometry"].tolist()
        if operator=="get_area":assert result["area"].tolist()==[2.,9.]
        elif operator=="get_length":assert result["length"].tolist()==[6.,12.]
        else:assert result[["minx","miny","maxx","maxy"]].values.tolist()==[[0.,0.,2.,1.],[10.,0.,13.,3.]]
    assert_geodataframe_equal(frame,before)


@pytest.mark.parametrize("operator,field",[("get_area","area"),("get_length","length"),("get_bounds","minx"),("get_bounds","miny"),("get_bounds","maxx"),("get_bounds","maxy"),("split_by_area","area")])
@pytest.mark.parametrize("empty",[False,True])
def test_load_spatial_geometry_identity_consumers_reject_geometry_output_collision(tmp_path,operator,field,empty):
    from operators import get_operator
    from geopandas.testing import assert_geodataframe_equal
    frame=_loaded_geometry_consumer_source(tmp_path).rename_geometry(field)
    if empty:frame=frame.iloc[:0]
    before=frame.copy(deep=True)
    with pytest.raises(ValueError,match="geometry.*conflict"):
        get_operator(operator)(frame,2.) if operator=="split_by_area" else get_operator(operator)(frame)
    assert_geodataframe_equal(frame,before)


def test_load_spatial_geometry_identity_voronoi_reads_actual_point_geometry(tmp_path):
    from operators import get_operator
    from geopandas.testing import assert_geodataframe_equal
    points=gpd.GeoDataFrame({"geometry":["甲","乙","丙"],"point_shape":gpd.GeoSeries.from_wkt(["POINT (0 0)","POINT (2 0)","POINT (1 2)"],crs=3857)},geometry="point_shape")
    points.to_file(tmp_path/"points.gpkg",driver="GPKG",index=False);frame=_load_local(tmp_path/"points.gpkg");before=frame.copy(deep=True)
    expected=get_operator("voronoi")(frame.drop(columns="geometry").rename_geometry("geometry"))
    actual=get_operator("voronoi")(frame)
    assert len(actual)==len(expected)==3 and actual.geometry.to_wkb().tolist()==expected.geometry.to_wkb().tolist()
    assert_geodataframe_equal(frame,before)


@pytest.mark.parametrize("is_geo",[False,True])
def test_load_spatial_geometry_identity_aggregate_retains_plain_geometry_attribute(is_geo):
    from operators import get_operator
    frame=pd.DataFrame({"geometry":["a","b","a"],"amount":[1,2,3]})
    if is_geo:frame=gpd.GeoDataFrame(frame,geometry=None)
    before=frame.copy(deep=True);result=get_operator("aggregate")(frame,["amount"],"sum",["geometry"])
    assert result["geometry"].tolist()==["a","b"] and result.amount.tolist()==[4,2]
    pd.testing.assert_frame_equal(frame,before)


@pytest.mark.parametrize("engine",["mysql","doris"])
@pytest.mark.parametrize("active",[False,True])
def test_load_spatial_geometry_identity_sql_text_save_encodes_only_active_geometry(monkeypatch,engine,active):
    from shapely.geometry import Point
    from geopandas.testing import assert_geodataframe_equal
    frame=pd.DataFrame({"geometry":["business",None,"keep"],"value":[1,2,3]})
    if active:frame=gpd.GeoDataFrame({**frame.to_dict("list"),"actual_shape":gpd.GeoSeries([Point(1,2),None,Point()],crs=3857)},geometry="actual_shape")
    before=frame.copy(deep=True);calls=[]
    monkeypatch.setattr(io_operators,"create_engine",lambda *a,**kw:object())
    monkeypatch.setattr(pd.DataFrame,"to_sql",lambda self,*a,**kw:calls.append(pd.DataFrame(self.copy())))
    io_operators.save(frame,connection_info={"engine_type":engine,"host":"127.0.0.1","port":3306,"user":"test","database":"unused"},table="output")
    assert len(calls)==1 and calls[0]["geometry"].tolist()==frame["geometry"].tolist()
    if active:
        assert calls[0].actual_shape.tolist()==["POINT (1 2)",None,"POINT EMPTY"]
        assert_geodataframe_equal(frame,before)
    else:pd.testing.assert_frame_equal(frame,before)



def test_load_spatial_geometry_identity_read_failure_has_no_alternate_reader(monkeypatch):
    import pyogrio
    calls=[]
    def read(*a,**kw):calls.append(1);raise RuntimeError("original Arrow read failure")
    def forbidden(*a,**kw):raise AssertionError("alternate spatial reader is forbidden")
    monkeypatch.setattr(pyogrio,"read_arrow",read)
    monkeypatch.setattr(pyogrio,"read_dataframe",forbidden)
    monkeypatch.setattr(gpd,"read_file",forbidden)
    with pytest.raises(RuntimeError,match="original Arrow read failure"):io_operators._read_file("file.gpkg","gpkg")
    assert calls==[1]
