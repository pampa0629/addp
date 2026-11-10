from bson import Decimal128, ObjectId
import hashlib
import json
from datetime import date, datetime
from decimal import Decimal
from pathlib import Path

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
    with pytest.raises(RuntimeError if fmt == "gpkg" else pa.ArrowInvalid):
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


@pytest.mark.parametrize("first_srs,second_srs", [(0, 4326), (-1, 4326), (4326, 0), ("custom", 0)])
def test_load_gpkg_resolves_crs_from_selected_layer_registration(tmp_path, first_srs, second_srs):
    import sqlite3
    from pyproj import CRS

    path = tmp_path / "source #登记.gpkg"
    custom_wkt = ('GEOGCS["Custom geographic test",DATUM["Custom datum",'
                  'SPHEROID["Custom sphere",6370997,0]],PRIMEM["Greenwich",0],'
                  'UNIT["degree",0.0174532925199433]]')
    ds = ogr.GetDriverByName("GPKG").CreateDataSource(str(path))
    for name, srid in [("z_first", first_srs), ("a_second", second_srs)]:
        srs = osr.SpatialReference()
        if srid == "custom":
            assert srs.ImportFromWkt(custom_wkt) == 0
        else:
            assert srs.ImportFromEPSG(4326) == 0
        layer = ds.CreateLayer(name, srs, ogr.wkbPoint, options=["GEOMETRY_NAME=real_shape"])
        assert layer.CreateField(ogr.FieldDefn("record_id", ogr.OFTInteger64)) == 0
        row = ogr.Feature(layer.GetLayerDefn())
        row.SetField("record_id", 2**53 + (1 if name == "z_first" else 3))
        row.SetGeometry(ogr.CreateGeometryFromWkt("POINT (1 2)"))
        assert layer.CreateFeature(row) == 0
        row = layer = None
        if srid in (0, -1):
            ds.ExecuteSQL(f'UPDATE "{name}" SET real_shape=SetSRID(real_shape,{srid})', dialect="SQLITE")
            ds.ExecuteSQL(f"UPDATE gpkg_geometry_columns SET srs_id={srid} WHERE table_name='{name}'", dialect="SQLITE")
            ds.ExecuteSQL(f"UPDATE gpkg_contents SET srs_id={srid} WHERE table_name='{name}'", dialect="SQLITE")
    ds = None
    before = path.read_bytes()
    with sqlite3.connect(path.as_uri() + "?mode=ro", uri=True) as db:
        for name, srid in [("z_first", first_srs), ("a_second", second_srs)]:
            registered, = db.execute("SELECT srs_id FROM gpkg_geometry_columns WHERE table_name=? AND column_name='real_shape'", (name,)).fetchone()
            blob, = db.execute(f'SELECT real_shape FROM "{name}"').fetchone()
            assert int.from_bytes(blob[4:8], "little" if blob[3] & 1 else "big", signed=True) == registered
            assert registered not in (0, -1, 4326) if srid == "custom" else registered == srid

    loaded = _load_local(path)
    assert loaded.geometry.name == "real_shape" and loaded.geometry.iloc[0].wkt == "POINT (1 2)"
    assert loaded.record_id.dtype == "Int64" and loaded.record_id.tolist() == [2**53 + 1]
    if first_srs in (0, -1):
        assert loaded.crs is None
    elif first_srs == "custom":
        assert loaded.crs.to_authority() is None
        assert loaded.crs.equals(CRS.from_wkt(custom_wkt), ignore_axis_order=True)
    else:
        assert loaded.crs.to_epsg() == 4326
    assert path.read_bytes() == before


@pytest.mark.parametrize("fmt",["gpkg","shp","geojson","kml","gml","fgb"])
def test_load_spatial_geometry_identity_keeps_real_geometry_and_every_business_field(tmp_path,fmt):
    path=tmp_path/("source."+fmt);_native_geometry_identity_source(path,fmt)
    ds=ogr.Open(str(path));layer=ds.GetLayer(0);features=list(layer);fields=list(layer.schema)
    assert "geometry" in [f.name for f in fields]
    # Pyogrio may bundle a different GDAL version, with a different native schema.
    layer.ResetReading()
    with layer.GetArrowStreamAsPyArrow(["INCLUDE_FID=NO"]) as stream:
        schema = pa.schema(list(stream.schema))
    geometry_fields=[f.name for f in schema if (f.metadata or {}).get(b"ARROW:extension:name") in (b"geoarrow.wkb",b"ogc.wkb")]
    assert len(geometry_fields)==1;physical=layer.GetGeometryColumn();expected_name=physical or geometry_fields[0]
    before={p.name:hashlib.sha256(p.read_bytes()).hexdigest() for p in tmp_path.iterdir()}
    loaded=_load_local(path)
    assert loaded.geometry.name==expected_name and loaded.geometry.name!="geometry"
    assert loaded.columns.tolist()==schema.names
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
    import shapely
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
    monkeypatch.setattr(io_operators,"_read_spatial_arrow",lambda *a,**kw:(meta,table))
    with pytest.raises(ValueError):io_operators._read_file("file.fgb","fgb",request)


@pytest.mark.parametrize("extension",[b"geoarrow.wkb",b"ogc.wkb"])
def test_load_spatial_geometry_identity_geometry_marker_is_not_a_column_name_guess(monkeypatch,extension):
    import shapely
    table=pa.Table.from_arrays([pa.array([b"ordinary blob"]),pa.array(["business"]),pa.array([shapely.Point(1,2).wkb])],schema=pa.schema([
        pa.field("wkb_geometry",pa.binary()),pa.field("geometry",pa.string()),pa.field("real_shape",pa.binary(),metadata={b"ARROW:extension:name":extension})]))
    monkeypatch.setattr(io_operators,"_read_spatial_arrow",lambda *a,**kw:({"crs":None,"geometry_name":"real_shape","geometry_type":"Point"},table))
    loaded=io_operators._read_file("file.fgb","fgb")
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


def test_wkt_batch_round_trip_preserves_rows_null_and_typed_empty():
    import json
    from operators import list_operators
    from operators.format_operators import export_to_wkt, load_from_wkt
    from workflow_engine import GeoPythonWorkflowRunner

    values = ["POINT (1.12345678912345 2)", None, "POINT EMPTY", "POLYGON EMPTY"]
    properties = [{"key": key} for key in [11, 22, 33, 44]]
    frame = load_from_wkt(values, properties, crs=None)
    assert frame.crs is None and frame["key"].tolist() == [11, 22, 33, 44]
    assert frame.geometry.iloc[1] is None
    assert frame.geometry.iloc[2].is_empty and frame.geometry.iloc[2].geom_type == "Point"
    assert frame.geometry.iloc[3].is_empty and frame.geometry.iloc[3].geom_type == "Polygon"
    assert export_to_wkt(frame.rename_geometry("shape")) == values
    empty = load_from_wkt([], [])
    assert empty.geometry.name == "geometry" and empty.crs.to_epsg() == 4326
    assert export_to_wkt(empty) == []
    tasks = [
        {"id": "source", "operator": "load_from_wkt", "params": {"wkt_list": values, "properties": properties},
         "depends_on": []},
        {"id": "export", "operator": "export_to_wkt", "params": {"input_gdf": {"$ref": "source"}},
         "depends_on": ["source"]},
    ]
    result = GeoPythonWorkflowRunner().execute({"tasks": tasks})
    assert json.loads(result.final_result) == values
    operators = {operator["id"]: operator for operator in list_operators()}
    parameter = next(item for item in operators["load_from_wkt"]["parameters"] if item["name"] == "wkt_list")
    assert parameter["type"] == "array" and "item_type" not in parameter
    assert operators["export_to_wkt"]["output_ports"][0]["type"] == "array"


@pytest.mark.parametrize(("operator", "params", "reason", "parameter", "field"), [
    ("load_from_wkt", {}, "operator_input_invalid", "wkt_list", ""),
    ("load_from_wkt", {"wkt_list": ["PRIVATE"]}, "operator_input_invalid", "wkt_list", ""),
    ("load_from_wkt", {"wkt_list": [1]}, "operator_input_invalid", "wkt_list", ""),
    ("load_from_wkt", {"wkt_list": [" "]}, "operator_input_invalid", "wkt_list", ""),
    ("load_from_wkt", {"wkt_list": [None], "properties": []}, "operator_wkt_properties_invalid", "properties", ""),
    ("load_from_wkt", {"wkt_list": [None], "properties": [None]}, "operator_wkt_properties_invalid", "properties", ""),
    ("load_from_wkt", {"wkt_list": [None], "properties": [{"geometry": "PRIVATE"}]},
     "operator_field_conflict", "properties", "geometry"),
    ("load_from_wkt", {"wkt_list": [None], "crs": "PRIVATE"}, "operator_crs_invalid", "crs", ""),
    ("export_to_wkt", {}, "operator_input_invalid", "input_gdf", ""),
])
def test_wkt_batch_reports_controlled_input_errors_and_blocks_save(monkeypatch, operator, params, reason, parameter, field):
    from addp_common.workflow_runtime import WorkflowExecutionError
    import workflow_engine

    def unexpected_save(**_kwargs):
        pytest.fail("save ran after a rejected WKT node")

    resolve = workflow_engine.get_operator
    monkeypatch.setattr(workflow_engine, "get_operator", lambda name: unexpected_save if name == "save" else resolve(name))
    tasks = [
        {"id": "save", "operator": "save", "params": {"input_df": {"$ref": "wkt"}}, "depends_on": ["wkt"]},
        {"id": "wkt", "operator": operator, "params": params, "depends_on": []},
    ]
    with pytest.raises(WorkflowExecutionError) as caught:
        workflow_engine.GeoPythonWorkflowRunner().execute({"tasks": tasks})
    assert caught.value.error_code == "INVALID_PARAMS" and "PRIVATE" not in str(caught.value)
    assert caught.value.diagnostic == {
        "schema_version": "addp.execution-failure/v3", "stage": "execution", "reason": reason,
        "operator": {"node_index": 2, "operator_id": operator, "parameter": parameter,
                     **({"field": field} if field else {})},
    }


def test_wkt_batch_native_parser_exception_is_execution_failure(monkeypatch):
    from addp_common.workflow_runtime import WorkflowExecutionError
    from operators import format_operators
    from workflow_engine import GeoPythonWorkflowRunner

    def fail(*_args, **_kwargs):
        raise RuntimeError("native parser unavailable")

    monkeypatch.setattr(format_operators.shapely, "from_wkt", fail)
    tasks = [{"id": "wkt", "operator": "load_from_wkt", "params": {"wkt_list": ["POINT (1 2)"]}, "depends_on": []}]
    with pytest.raises(WorkflowExecutionError) as caught:
        GeoPythonWorkflowRunner().execute({"tasks": tasks})
    assert caught.value.error_code == "EXECUTION_FAILED" and caught.value.diagnostic is None


def test_load_spatial_geometry_identity_invalid_wkb_fails_without_ignoring(monkeypatch):
    import shapely
    table=pa.Table.from_arrays([pa.array([b"invalid WKB"])],schema=pa.schema([pa.field("shape",pa.binary(),metadata={b"ARROW:extension:name":b"geoarrow.wkb"})]))
    monkeypatch.setattr(io_operators,"_read_spatial_arrow",lambda *a,**kw:({"geometry_name":"shape","geometry_type":"Point","crs":None},table))
    with pytest.raises(shapely.errors.GEOSException):io_operators._read_file("file.fgb","fgb")
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
    from operators.properties_operators import MeasurementInputError, BoundsInputError
    error_type = BoundsInputError if operator=="get_bounds" else MeasurementInputError if operator in ('get_area','get_length','split_by_area') else ValueError
    with pytest.raises(error_type) as caught:
        get_operator(operator)(frame,2.) if operator=="split_by_area" else get_operator(operator)(frame)
    if error_type is MeasurementInputError: assert caught.value.code == field
    elif error_type is BoundsInputError: assert caught.value.code=='field' and caught.value.field==field
    else: assert 'geometry' in str(caught.value) and 'conflict' in str(caught.value)
    assert_geodataframe_equal(frame,before)


def test_load_spatial_geometry_identity_voronoi_reads_actual_point_geometry(tmp_path):
    from operators import get_operator
    from geopandas.testing import assert_geodataframe_equal
    points=gpd.GeoDataFrame({"station":[1,2,3],"geometry":["甲","乙","丙"],"point_shape":gpd.GeoSeries.from_wkt(["POINT (0 0)","POINT (2 0)","POINT (1 2)"],crs=3857)},geometry="point_shape")
    points.to_file(tmp_path/"points.gpkg",driver="GPKG",index=False);frame=_load_local(tmp_path/"points.gpkg");before=frame.copy(deep=True)
    coverage=gpd.GeoDataFrame(geometry=gpd.GeoSeries.from_wkt(["POLYGON ((-1 -1,3 -1,3 3,-1 3,-1 -1))"],crs=3857))
    expected=get_operator("voronoi")(frame.drop(columns="geometry").rename_geometry("geometry"),coverage,"station")
    actual=get_operator("voronoi")(frame,coverage,"station")
    assert actual["geometry"].tolist()==["甲","乙","丙"] and actual.station.tolist()==[1,2,3]
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



@pytest.mark.parametrize(("suffix", "drivers"), [
    ("geojson", ["GeoJSON"]),
    ("shp", ["ESRI Shapefile"]),
    ("kml", ["KML", "LIBKML"]),
    ("gml", ["GML"]),
    ("fgb", ["FlatGeobuf"]),
])
def test_load_rejects_geopackage_under_another_vector_format(tmp_path, monkeypatch, suffix, drivers):
    path = tmp_path / "source.gpkg"
    _native_geometry_identity_source(path, "gpkg")
    disguised = path.with_suffix(f".{suffix}")
    path.rename(disguised)
    before = disguised.read_bytes()
    original_open = io_operators.gdal.OpenEx
    opens = []

    def open_dataset(filename, flags, *, allowed_drivers):
        opens.append((filename, allowed_drivers))
        return original_open(filename, flags, allowed_drivers=allowed_drivers)

    monkeypatch.setattr(io_operators.gdal, "OpenEx", open_dataset)
    with pytest.raises(RuntimeError):
        _load_local(disguised)
    assert opens == [(str(disguised), drivers)]
    assert disguised.read_bytes() == before
    assert list(tmp_path.iterdir()) == [disguised]


def test_load_uses_one_driver_constrained_dataset_for_geopackage(tmp_path, monkeypatch):
    path = tmp_path / "source.gpkg"
    _native_geometry_identity_source(path, "gpkg")
    original_open = io_operators.gdal.OpenEx
    opens = []

    def open_dataset(filename, flags, *, allowed_drivers):
        opens.append((filename, flags, allowed_drivers))
        return original_open(filename, flags, allowed_drivers=allowed_drivers)

    monkeypatch.setattr(io_operators.gdal, "OpenEx", open_dataset)
    frame = _load_local(path)
    assert opens == [(str(path), io_operators.gdal.OF_VECTOR | io_operators.gdal.OF_READONLY, ["GPKG"])]
    assert frame.geometry.name == "parcel_shape"
    assert frame.crs.to_epsg() == 4326


def test_load_spatial_geometry_identity_read_failure_has_no_alternate_reader(monkeypatch):
    import pyogrio
    calls=[]
    def read(*a,**kw):calls.append(1);raise RuntimeError("original Arrow read failure")
    def forbidden(*a,**kw):raise AssertionError("alternate spatial reader is forbidden")
    monkeypatch.setattr(io_operators,"_read_spatial_arrow",read)
    monkeypatch.setattr(pyogrio,"read_dataframe",forbidden)
    monkeypatch.setattr(gpd,"read_file",forbidden)
    with pytest.raises(RuntimeError,match="original Arrow read failure"):io_operators._read_file("file.fgb","fgb")
    assert calls==[1]


# GeoPackage business identifiers are attributes, never storage row identities.
def _gpkg_business_frame(field='fid', empty=False):
    from shapely.geometry import Point
    values = [] if empty else [2**53+1, 2**53+1, None]
    count = len(values)
    result = gpd.GeoDataFrame({
        field: pd.array(values, dtype='Int64'),
        '_addp_fid': pd.array([10]*count, dtype='Int64'),
        '_ADDP_FID_1': ['preserve']*count,
        'flag': pd.array([True, None, False][:count], dtype='boolean'),
        'text': ['001', '', None][:count],
        'shape': gpd.GeoSeries([Point(i, i+1) for i in range(count)], crs=3857),
    }, geometry='shape', crs=3857)
    result.index = pd.Index([7]*count, name='fid_index')
    return result


def _save_gpkg(frame, path, mode='fail'):
    return io_operators.save(frame, connection_info={'mount_path': str(path.parent)}, path=path.name, mode=mode)


def _assert_gpkg_business(path, frame):
    # Read with native OGR: independent of the product reader and writer.
    source = ogr.Open(str(path)); assert source is not None
    layer = source.GetLayer(0); definition = layer.GetLayerDefn()
    attributes = [name for name in frame.columns if name != frame.geometry.name]
    assert [definition.GetFieldDefn(i).GetName() for i in range(definition.GetFieldCount())] == attributes
    assert layer.GetName() == path.stem
    assert layer.GetFIDColumn().casefold() not in {str(name).casefold() for name in frame.columns}
    assert layer.GetFeatureCount() == len(frame)
    crs = osr.SpatialReference(); crs.ImportFromWkt(frame.crs.to_wkt())
    assert layer.GetSpatialRef().IsSame(crs)
    ids = []
    for position, feature in enumerate(layer):
        ids.append(feature.GetFID())
        for name in attributes:
            expected = frame.iloc[position][name]
            if pd.isna(expected): assert feature.IsFieldNull(name)
            else: assert feature.GetField(name) == expected
        assert feature.GetGeometryRef().ExportToWkb() == frame.geometry.iloc[position].wkb
    assert len(set(ids)) == len(ids)
    field = attributes[0]
    assert definition.GetFieldDefn(definition.GetFieldIndex(field)).GetType() == ogr.OFTInteger64
    assert definition.GetFieldDefn(definition.GetFieldIndex('flag')).GetSubType() == ogr.OFSTBoolean
    assert path.stat().st_size > 0
    # Round-trip must expose business attributes, not turn internal FID into one.
    loaded = _load_local(path)
    assert list(loaded.columns) == attributes + [loaded.geometry.name]
    for name in attributes:
        assert loaded[name].tolist() == frame[name].tolist()
    assert str(loaded[field].dtype) == 'Int64'
    assert loaded.geometry.to_wkb().tolist() == frame.geometry.to_wkb().tolist()
    assert loaded.crs == frame.crs


@pytest.mark.parametrize('field', ['fid', 'FID', 'FiD'])
@pytest.mark.parametrize('empty', [False, True])
@pytest.mark.parametrize('mode', ['fail', 'replace'])
def test_gpkg_save_business_fid_and_collision_columns_are_independent_storage_keys(tmp_path, field, empty, mode):
    frame = _gpkg_business_frame(field, empty); before = frame.copy(deep=True)
    target = tmp_path/'结果.gpkg'
    if mode == 'replace': target.write_bytes(b'existing file preserved until commit')
    assert _save_gpkg(frame, target, mode) is frame
    _assert_gpkg_business(target, frame)
    pd.testing.assert_frame_equal(frame, before)
    assert {p.name for p in tmp_path.iterdir()} == {target.name}


@pytest.mark.parametrize('field,values,dtype', [
    ('fid', ['source', 'source', None], 'string'),
    ('FID', [True, None, True], 'boolean'),
    ('FiD', [1.25, 1.25, None], 'Float64'),
])
def test_gpkg_save_business_fid_uses_its_attribute_type_not_integer_primary_key(tmp_path, field, values, dtype):
    frame = _gpkg_business_frame(field); frame[field] = pd.array(values, dtype=dtype)
    target=tmp_path/'typed.gpkg'; _save_gpkg(frame,target)
    source=ogr.Open(str(target));layer=source.GetLayer(0);features=list(layer)
    assert layer.GetFIDColumn().casefold() != field.casefold()
    assert len(features)==3 and len({f.GetFID() for f in features})==3
    for feature,expected in zip(features,values):
        if expected is None: assert feature.IsFieldNull(field)
        else: assert feature.GetField(field)==expected
    assert list(tmp_path.iterdir()) == [target]


@pytest.mark.parametrize('mode', ['fail', 'replace'])
@pytest.mark.parametrize('exists', [False, True])
def test_gpkg_save_failed_real_writer_leaves_no_partial_file_or_sidecar_and_protects_old(tmp_path,monkeypatch,mode,exists):
    frame=_gpkg_business_frame();before=frame.copy(deep=True);target=tmp_path/'result.gpkg';old=tmp_path/'previous.gpkg'
    _save_gpkg(frame,old);old_bytes=old.read_bytes()
    if exists: _save_gpkg(frame,target);target_bytes=target.read_bytes()
    original=gpd.GeoDataFrame.to_file;calls=[]
    def broken(self,path,*args,**kwargs):
        calls.append(path); original(self,path,*args,**kwargs)
        assert Path(path).is_file() and (not target.exists() or target.read_bytes()==target_bytes)
        Path(str(path)+'-journal').write_bytes(b'only this save owns the partial journal')
        raise RuntimeError('injected failure after real GeoPackage write')
    monkeypatch.setattr(gpd.GeoDataFrame,'to_file',broken)
    error = FileExistsError if exists and mode=='fail' else RuntimeError
    with pytest.raises(error): _save_gpkg(frame,target,mode)
    if exists: assert target.read_bytes()==target_bytes
    else: assert not target.exists()
    assert old.read_bytes()==old_bytes
    assert len(calls)==(0 if exists and mode=='fail' else 1)
    assert {p.name for p in tmp_path.iterdir()} == ({old.name,target.name}if exists else{old.name})
    pd.testing.assert_frame_equal(frame,before)


@pytest.mark.parametrize('mode',['fail','replace'])
def test_gpkg_save_failed_publication_protects_target_and_cleans_completed_stage(tmp_path,monkeypatch,mode):
    frame=_gpkg_business_frame();target=tmp_path/'target.gpkg';previous=tmp_path/'old.gpkg'
    _save_gpkg(frame,previous);before=previous.read_bytes()
    if mode=='replace': _save_gpkg(frame,target);target_bytes=target.read_bytes()
    def fail(*args,**kwargs): raise PermissionError('injected publication denial')
    monkeypatch.setattr(io_operators.os,'link'if mode=='fail'else'replace',fail)
    with pytest.raises(PermissionError,match='publication denial'): _save_gpkg(frame,target,mode)
    assert previous.read_bytes()==before
    if mode=='replace': assert target.read_bytes()==target_bytes
    else: assert not target.exists()
    assert {p.name for p in tmp_path.iterdir()}==({previous.name,target.name}if mode=='replace'else{previous.name})


def test_gpkg_save_create_race_never_replaces_concurrently_created_target(tmp_path,monkeypatch):
    frame=_gpkg_business_frame();target=tmp_path/'race.gpkg';real_link=io_operators.os.link;competitor=b'another creator owns this target'
    def race(source,destination,*args,**kwargs):
        assert Path(source).is_file();target.write_bytes(competitor)
        return real_link(source,destination,*args,**kwargs)
    monkeypatch.setattr(io_operators.os,'link',race)
    with pytest.raises(FileExistsError): _save_gpkg(frame,target)
    assert target.read_bytes()==competitor and list(tmp_path.iterdir())==[target]


@pytest.mark.parametrize('state',['symlink','dangling','directory','-wal','-shm','-journal'])
@pytest.mark.parametrize('mode',['fail','replace'])
def test_gpkg_save_protects_nonregular_targets_and_sqlite_companions_before_writing(tmp_path,monkeypatch,state,mode):
    frame=_gpkg_business_frame();target=tmp_path/'target.gpkg';old=tmp_path/'old.gpkg';_save_gpkg(frame,old);old_bytes=old.read_bytes()
    if state in('symlink','dangling'): target.symlink_to(old if state=='symlink'else tmp_path/'missing')
    elif state=='directory': target.mkdir();(target/'owned').write_bytes(b'keep')
    else: target.write_bytes(old_bytes);Path(str(target)+state).write_bytes(b'external SQLite state')
    before={str(p.relative_to(tmp_path)):('link',str(p.readlink()))if p.is_symlink()else('dir',)if p.is_dir()else('file',p.read_bytes())for p in tmp_path.rglob('*')}
    monkeypatch.setattr(gpd.GeoDataFrame,'to_file',lambda *a,**kw:pytest.fail('must reject before writing'))
    with pytest.raises((FileExistsError,ValueError,OSError)): _save_gpkg(frame,target,mode)
    after={str(p.relative_to(tmp_path)):('link',str(p.readlink()))if p.is_symlink()else('dir',)if p.is_dir()else('file',p.read_bytes())for p in tmp_path.rglob('*')}
    assert before==after and old.read_bytes()==old_bytes


def test_gpkg_save_file_append_is_rejected_without_touching_existing_target(tmp_path):
    frame=_gpkg_business_frame();target=tmp_path/'target.gpkg';_save_gpkg(frame,target);before=target.read_bytes()
    with pytest.raises(ValueError): _save_gpkg(frame,target,'append')
    assert target.read_bytes()==before and list(tmp_path.iterdir())==[target]


@pytest.mark.parametrize('kind',['wrong-count','invalid-file','journal'])
@pytest.mark.parametrize('mode',['fail','replace'])
def test_gpkg_save_verifies_closed_complete_single_file_before_publication(tmp_path,monkeypatch,kind,mode):
    frame=_gpkg_business_frame();target=tmp_path/'result.gpkg'
    if mode=='replace':_save_gpkg(frame,target);old=target.read_bytes()
    writer=gpd.GeoDataFrame.to_file
    def incomplete(self,path,*args,**kwargs):
        if kind=='invalid-file': Path(path).write_bytes(b'not a GeoPackage');return
        writer(self.iloc[:1] if kind=='wrong-count'else self,path,*args,**kwargs)
        if kind=='journal':Path(str(path)+'-journal').write_bytes(b'unfinished state')
    monkeypatch.setattr(gpd.GeoDataFrame,'to_file',incomplete)
    with pytest.raises((io_operators.GeoPackageSaveError,DataSourceError)): _save_gpkg(frame,target,mode)
    if mode=='replace':assert target.read_bytes()==old and list(tmp_path.iterdir())==[target]
    else:assert list(tmp_path.iterdir())==[]


@pytest.mark.parametrize('business_failure',[False,True])
def test_gpkg_save_reports_cleanup_failure_with_original_failure_or_committed_fact(tmp_path,monkeypatch,business_failure):
    frame=_gpkg_business_frame();target=tmp_path/'result.gpkg';real=io_operators.tempfile.TemporaryDirectory;owned=[]
    class DeniedCleanup:
        def __init__(self,*args,**kwargs):
            self.real=real(*args,**kwargs);self.name=self.real.name;owned.append(self)
        def cleanup(self):raise PermissionError('injected cleanup denial')
    writer=gpd.GeoDataFrame.to_file
    first=RuntimeError('first business failure remains visible')
    def failed(self,*args,**kwargs):writer(self,*args,**kwargs);raise first
    if business_failure:monkeypatch.setattr(gpd.GeoDataFrame,'to_file',failed)
    monkeypatch.setattr(io_operators.tempfile,'TemporaryDirectory',DeniedCleanup)
    try:
        with pytest.raises(io_operators.GeoPackageSaveError)as error:_save_gpkg(frame,target)
        assert 'cleanup denial'in str(error.value)
        if business_failure:
            assert error.value.code=='cleanup'and error.value.causes[0]is first
            assert str(first)in str(error.value)and not target.exists()
        else:
            assert error.value.code=='published_cleanup'and target.is_file()
            _assert_gpkg_business(target,frame)
    finally:
        for item in owned:item.real.cleanup()
    assert {p.name for p in tmp_path.iterdir()}== (set()if business_failure else{target.name})


@pytest.mark.parametrize('language',['zh-cn','en'])
@pytest.mark.parametrize('fault',['mode','busy','target','incomplete'])
def test_gpkg_save_new_diagnostics_follow_execution_language(tmp_path,monkeypatch,language,fault):
    from workflow_engine import GeoPythonWorkflowRunner
    frame=_gpkg_business_frame();target=tmp_path/'result.gpkg';mode='replace'
    if fault=='mode':mode='append'
    elif fault=='busy':Path(str(target)+'-wal').write_bytes(b'external state')
    elif fault=='target':target.mkdir()
    else:
        writer=gpd.GeoDataFrame.to_file
        monkeypatch.setattr(gpd.GeoDataFrame,'to_file',lambda self,*a,**kw:writer(self.iloc[:1],*a,**kw))
    with pytest.raises(ValueError)as error:GeoPythonWorkflowRunner(language=language)._execute_operator('save',{'input_df':frame,'connection_info':{'mount_path':str(tmp_path)},'path':target.name,'mode':mode})
    assert str(error.value)==io_operators.GeoPackageSaveError._messages[fault][language=='en']


def test_gpkg_save_existing_fail_target_stops_before_allocating_temp_directory(tmp_path,monkeypatch):
    target=tmp_path/'existing.gpkg';target.write_bytes(b'old')
    monkeypatch.setattr(io_operators.tempfile,'TemporaryDirectory',lambda *a,**kw:pytest.fail('preexisting target must fail first'))
    with pytest.raises(FileExistsError):_save_gpkg(_gpkg_business_frame(),target)
    assert target.read_bytes()==b'old'and list(tmp_path.iterdir())==[target]


@pytest.mark.parametrize('state',['symlink','-wal','-shm','-journal'])
def test_gpkg_save_rechecks_target_state_after_writing_without_touching_external_state(tmp_path,monkeypatch,state):
    frame=_gpkg_business_frame();target=tmp_path/'target.gpkg';other=tmp_path/'other.gpkg';_save_gpkg(frame,other);original=other.read_bytes();writer=gpd.GeoDataFrame.to_file
    def concurrent(self,*a,**kw):
        writer(self,*a,**kw)
        if state=='symlink':target.symlink_to(other)
        else:Path(str(target)+state).write_bytes(b'concurrent external state')
    monkeypatch.setattr(gpd.GeoDataFrame,'to_file',concurrent)
    with pytest.raises(io_operators.GeoPackageSaveError):_save_gpkg(frame,target,'replace')
    assert other.read_bytes()==original
    assert {p.name for p in tmp_path.iterdir()}=={other.name,target.name if state=='symlink'else target.name+state}
    if state=='symlink':assert target.is_symlink()and target.readlink()==other
    else:assert Path(str(target)+state).read_bytes()==b'concurrent external state'and not target.exists()
