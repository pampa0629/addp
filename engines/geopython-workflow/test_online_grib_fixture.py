"""Independent Hosted bytes traversing the real short-lived Native operators."""
from datetime import datetime, timedelta, timezone
from hashlib import sha256
import importlib.util
import math
from pathlib import Path
import numpy as np
import pytest
from operators import grib

ROOT=Path(__file__).resolve().parents[2]
SPEC=importlib.util.spec_from_file_location('online_grib_fixture',ROOT/'business/scripts/online-grib-fixture.py')
FIXTURE=importlib.util.module_from_spec(SPEC);SPEC.loader.exec_module(FIXTURE)

@pytest.mark.parametrize('spec',FIXTURE.sources(),ids=lambda spec:spec['case_id'])
def test_hosted_grib_oracle_through_actual_native_worker(tmp_path,spec):
    path=tmp_path/spec['name'];raw=FIXTURE.source_bytes(spec);path.write_bytes(raw)
    plan={'schema_version':'addp.workflow.access-plan/v1','source':{'kind':'file','format':'grib','access':{'method':'mounted_path','path':str(path)}}}
    info=grib.OPERATORS['grib.inspect']['function'](access_plan=plan)
    assert info['format_info']['source_version']=='sha256:'+spec['sha256']
    assert [c['name'] for c in info['container']['children']]==[f"message-{field['number']}" for field in spec['fields']]
    if spec['case_id'] in ('ecmwf-precipitation', 'ecmwf-precipitation-0h'):
        assert all('map_profile' not in child['native'] for child in info['container']['children'])
    for field in spec['fields']:
        select=FIXTURE.selection(field)
        result=grib.OPERATORS['grib.read_slice']['function'](access_plan=plan,source_version=info['format_info']['source_version'],selection=select)
        assert (result['values'],result['valid'])==FIXTURE.expected(field,select['x'],select['y'],select['width'],select['height'])
        native=result['variable'];assert native['attributes']['GRIB_UNIT']['values']==[field.get('unit','[K]')]
        spacing = field.get('spacing', 1)
        x_spacing = spacing
        if spacing == 0.1:
            x_spacing = native['grid']['transform'][1]
            assert abs(x_spacing-spacing) <= math.ulp(spacing)
        assert native['grid']['transform']==[field['west']-spacing/2,x_spacing,0.,field['north']+spacing/2,0.,-spacing]
        assert native['attributes']['GRIB_FORECAST_SECONDS']['values']==[str(field.get('start_hour',field['forecast'])*3600)]
        if spec['case_id'] == 'noaa-gfs-intervals':
            time = native['message']['time']
            reference = datetime.fromtimestamp(spec['reference_time'],timezone.utc)
            assert time['reference_utc'] == reference.isoformat()
            assert time['start_utc'] == (reference+timedelta(hours=field['start_hour'])).isoformat()
            assert time['end_utc'] == (reference+timedelta(hours=field['forecast'])).isoformat()
            assert time['interval_length'] == field['forecast'] - field['start_hour']
            assert time['operation'] == 'accumulation'
        if spec.get('packing') == 'ccsds':
            assert native['message']['data_template'] == 42
            assert native['message']['local']['mars_stream'] == 'oper'
            initial_zero = field.get('product_template') == 8 and field['forecast'] == 0
            precision = 'exact_zero_float64_output' if initial_zero else 'float32_scaling_float64_output'
            assert native['attributes']['GRIB2_DECODE_PRECISION']['values'] == [precision]
            if field.get('product_template') == 8:
                facts = native['message']
                start = datetime.fromtimestamp(spec.get('reference_time',1767322800),timezone.utc)
                assert facts['product_template'] == 8 and facts['parameter']['local_table'] == 1
                assert facts['level'] == dict(type_code=1, raw_scale=None, raw_value=None, reference='surface')
                assert facts['time']['operation'] == 'accumulation'
                assert facts['time']['start_utc'] == start.isoformat()
                assert facts['time']['end_utc'] == (start + timedelta(hours=field['forecast'])).isoformat()
                assert (facts['time']['increment_unit'], facts['time']['increment_value']) == (13, field.get('increment_seconds',450))
                assert native['attributes']['GRIB_ELEMENT']['values'] == ['tp']
            else:
                assert native['message']['level']['value'] == field['height_m']
        if spec.get('edition')==1:
            assert native['message']['edition']==1
            assert native['message']['level']['type_code']==field['level']
            assert native['message']['level']['raw_octets']==field['level_raw']
            assert native['message']['time']['range_indicator']==field['time_indicator']
            assert native['message']['time']['p1']==field['p1'] and native['message']['time']['p2']==field['p2']
    path.write_bytes(raw[:-1]+bytes([raw[-1]^1]))
    assert grib.OPERATORS['grib.read_slice']['function'](access_plan=plan,source_version=info['format_info']['source_version'],selection=select)['error_code']=='GRIB_SOURCE_CHANGED'


def test_ecmwf_01_instant_original_messages_through_native_worker():
    path = ROOT / 'business/fixtures/manager/ecmwf-ifs-20261117-instant-0p1.grib2'
    digest = '6572e70940c633a342a1c154e1d4338f742abcb7e2608b8b9f6cf98c499a9b18'
    assert sha256(path.read_bytes()).hexdigest() == digest
    plan = {'schema_version': 'addp.workflow.access-plan/v1', 'source': {
        'kind': 'file', 'format': 'grib', 'access': {'method': 'mounted_path', 'path': str(path)},
    }}
    info = grib.OPERATORS['grib.inspect']['function'](access_plan=plan)
    version = info['format_info']['source_version']
    assert version == 'sha256:' + digest
    assert [child['name'] for child in info['container']['children']] == ['message-1', 'message-2', 'message-3']

    # ecCodes 2.50.0 source values, rows 899–901 and columns 1798–1801.
    # For these unscaled-decimal fields, GDAL rounds the decoded value to Float32.
    source_windows = [
        ('TMP', '[K]', 2, 9, (0, 0), [
            [299.5925750732422, 299.8425750732422, 299.8425750732422, 299.8425750732422],
            [299.8425750732422, 299.8425750732422, 299.8425750732422, 299.8425750732422],
            [299.8425750732422, 299.8425750732422, 299.8425750732422, 299.5925750732422],
        ]),
        ('UGRD', '[m/s]', 10, 11, (2, 2), [
            [-1.232879638671875, -0.982879638671875, -0.982879638671875, -1.357879638671875],
            [-1.107879638671875, -0.982879638671875, -1.170379638671875, -1.545379638671875],
            [-1.232879638671875, -1.170379638671875, -1.607879638671875, -1.795379638671875],
        ]),
        ('VGRD', '[m/s]', 10, 11, (2, 3), [
            [4.6987152099609375, 5.1362152099609375, 5.3862152099609375, 4.8237152099609375],
            [4.8862152099609375, 5.3862152099609375, 5.3862152099609375, 4.6987152099609375],
            [5.0737152099609375, 5.4487152099609375, 5.2612152099609375, 4.8862152099609375],
        ]),
    ]
    for number, (element, unit, height, bits, (category, parameter), source_values) in enumerate(source_windows, 1):
        selection = dict(variable=f'message-{number}', indices=[], x=1798, y=899, width=4, height=3)
        result = grib.OPERATORS['grib.read_slice']['function'](
            access_plan=plan, source_version=version, selection=selection,
        )
        assert result['valid'] == [[True] * 4] * 3
        rounded = np.asarray(source_values, dtype='float32')
        actual = np.asarray(result['values'], dtype='float64')
        np.testing.assert_array_equal(actual, rounded.astype('float64'))
        if element == 'TMP':
            errors = np.abs(actual - source_values)
            assert 0 < errors.max() <= 2**-16
        else:
            np.testing.assert_array_equal(actual, source_values)

        native = result['variable']
        assert [dimension['size'] for dimension in native['dimensions']] == ['1801', '3600']
        assert native['grid']['longitude_policy'] == 'unchanged'
        np.testing.assert_allclose(native['grid']['transform'], [179.95, .1, 0., 90.05, 0., -.1], rtol=0, atol=1e-12)
        attributes = native['attributes']
        assert attributes['GRIB_ELEMENT']['values'] == [element]
        assert attributes['GRIB_UNIT']['values'] == [unit]
        assert attributes['GRIB2_DECODE_PRECISION']['values'] == ['float32_scaling_float64_output']
        message = native['message']
        assert (message['product_template'], message['data_template']) == (0, 42)
        assert message['parameter'] == dict(centre=98, subcentre=0, master_table=32, local_table=0,
                                            discipline=0, category=category, number=parameter)
        assert message['time'] == dict(reference_utc='2026-11-17T00:00:00+00:00', unit_code=1,
                                       forecast_value=6, start_utc='2026-11-17T06:00:00+00:00',
                                       end_utc='2026-11-17T06:00:00+00:00', operation='instant')
        assert message['level']['value'] == height and message['level']['unit'] == 'm'
        assert message['packing']['bits_per_value'] == bits


def test_grib_map_cog_keeps_message_identity_and_world_bounds(tmp_path):
    from osgeo import gdal
    from operators.raster_compute import raster_to_cog, validate_layout

    fixtures = ROOT / 'business/fixtures/manager'
    initial = (fixtures / 'ecmwf-ifs-20261117-2t-0p1-initial.grib2').read_bytes()
    assert sha256(initial).hexdigest() == '7902e79e5ef98b1972298251db7ae327019b6b1e3d2f2617e12896c26b9f4071'
    source = tmp_path / 'forecast.grib2'
    raw = initial + (fixtures / 'ecmwf-ifs-20261117-instant-0p1.grib2').read_bytes()
    source.write_bytes(raw)
    version = 'sha256:' + sha256(raw).hexdigest()
    plan = {'schema_version': 'addp.workflow.access-plan/v1', 'source': {
        'kind': 'file', 'format': 'grib', 'access': {'method': 'mounted_path', 'path': str(source)},
    }}
    children = grib.OPERATORS['grib.inspect']['function'](access_plan=plan)['container']['children']
    assert [child['native'].get('map_profile') for child in children] == [grib.MAP_PROFILE] * 2 + [None, None]
    maps = []
    for message in ('message-1', 'message-2'):
        target = tmp_path / (message + '.tif')
        access = {**plan, 'target': {'kind': 'file', 'format': 'tiff', 'name': target.name, 'write_mode': 'replace',
                                    'access': {'method': 'mounted_path', 'path': str(target)}}}
        selection = {'source_version': version, 'message': message}
        result = raster_to_cog(access, {'grib_map_selection': selection})
        assert result['grib_map_selection'] == selection and result['map_profile'] == grib.MAP_PROFILE
        assert result['extent'] == [-180, -90, 180, 90]
        ds = gdal.Open(str(target))
        assert ds.GetSpatialRef().GetSemiMajor() == 6371229 and ds.GetSpatialRef().GetSemiMinor() == 6371229
        assert (ds.RasterXSize, ds.RasterYSize) == (4096, 2048)
        assert not validate_layout(ds, full_check=True)[1]
        band = ds.GetRasterBand(1)
        assert band.GetBlockSize() == [256, 256] and band.GetUnitType() == 'K'
        values = band.ReadAsArray()
        np.testing.assert_array_equal(values[:, 0], values[:, -1])
        proof = FIXTURE.inspect_map_pixels(target.read_bytes(), message, 'temperature')
        assert proof['equator_kelvin'] == float(values[1023, 0])
        other = 'message-2' if message == 'message-1' else 'message-1'
        with pytest.raises(AssertionError):
            FIXTURE.inspect_map_pixels(target.read_bytes(), other, 'temperature')
        maps.append(values)
        del band, ds
    assert maps[0][1023, 0] != maps[1][1023, 0]
    assert float(maps[1][0, 2275]) == 254.3425750732422
    assert float(maps[1][-1, 2275]) == 240.8425750732422
    for selection in ({'source_version': version, 'message': 'message-3'},
                      {'source_version': 'sha256:' + '0' * 64, 'message': 'message-1'}):
        with pytest.raises(ValueError, match='GRIB map'):
            raster_to_cog(access, {'grib_map_selection': selection})


def test_grib_precipitation_map_keeps_original_metres_and_valid_zero(tmp_path):
    from operators.raster_compute import raster_to_cog

    raw = FIXTURE.map_source(ROOT, 'precipitation')
    source = tmp_path / 'forecast.grib2'
    source.write_bytes(raw)
    version = 'sha256:' + sha256(raw).hexdigest()
    plan = {'schema_version': 'addp.workflow.access-plan/v1', 'source': {
        'kind': 'file', 'format': 'grib', 'access': {'method': 'mounted_path', 'path': str(source)},
    }}
    children = grib.OPERATORS['grib.inspect']['function'](access_plan=plan)['container']['children']
    for index, child in enumerate(children):
        profile = grib.PRECIPITATION_MAP_PROFILE
        assert child['native']['map_profile'] == profile
        time = child['native']['message']['time']
        assert time['operation'] == 'accumulation' and time['interval_length'] == index * 6
        target = tmp_path / (child['name'] + '.tif')
        access = {**plan, 'target': {'kind': 'file', 'format': 'tiff', 'name': target.name, 'write_mode': 'replace',
                                    'access': {'method': 'mounted_path', 'path': str(target)}}}
        selection = {'source_version': version, 'message': child['name']}
        result = raster_to_cog(access, {'grib_map_selection': selection})
        assert result['map_profile'] == profile and result['grib_map_selection'] == selection
        assert result['extent'] == [-180, -90, 180, 90]
        FIXTURE.inspect_map_pixels(target.read_bytes(), child['name'], 'precipitation')


def test_noaa_grib_map_preserves_distinct_intervals_and_source_units(tmp_path):
    from operators.raster_compute import raster_to_cog

    kind = 'noaa-precipitation'
    raw = FIXTURE.map_source(ROOT, kind)
    source = tmp_path / 'gfs.grib2'
    source.write_bytes(raw)
    version = 'sha256:' + sha256(raw).hexdigest()
    plan = {'schema_version': 'addp.workflow.access-plan/v1', 'source': {
        'kind': 'file', 'format': 'grib', 'access': {'method': 'mounted_path', 'path': str(source)},
    }}
    children = grib.OPERATORS['grib.inspect']['function'](access_plan=plan)['container']['children']
    spec = FIXTURE.MAP_SOURCES[kind]
    assert len(children) == len(spec['intervals']) == 2
    for child, (start, end, hours) in zip(children, spec['intervals']):
        assert child['native']['map_profile'] == spec['profile']
        time = child['native']['message']['time']
        assert (time['start_utc'], time['end_utc'], time['interval_length']) == (start, end, hours)
        message = child['name']
        target = tmp_path / (message + '.tif')
        access = {**plan, 'target': {'kind': 'file', 'format': 'tiff', 'name': target.name,
                                    'write_mode': 'replace', 'access': {'method': 'mounted_path', 'path': str(target)}}}
        selection = {'source_version': version, 'message': message}
        result = raster_to_cog(access, {'grib_map_selection': selection})
        assert result['map_profile'] == spec['profile'] and result['grib_map_selection'] == selection
        payload = target.read_bytes()
        assert FIXTURE.inspect_map_pixels(payload, message, kind)['verified_pixels'] == 4096 * 2048
        other = 'message-2' if message == 'message-1' else 'message-1'
        with pytest.raises(AssertionError):
            FIXTURE.inspect_map_pixels(payload, other, kind)

    # A different NCEP process continues to qualify for the existing read-only
    # slice, but cannot use the GFS map admission.
    altered = bytearray(raw[:int.from_bytes(raw[8:16], 'big')])
    product = 16 + 21 + 72
    assert altered[product + 4] == 4 and altered[product + 13] == 96
    altered[product + 13] = 0
    source.write_bytes(altered)
    alteration = grib.OPERATORS['grib.inspect']['function'](access_plan=plan)
    assert 'map_profile' not in alteration['container']['children'][0]['native']
    target.unlink()
    with pytest.raises(ValueError, match='GRIB map'):
        raster_to_cog(access, {'grib_map_selection': {
            'source_version': 'sha256:' + sha256(altered).hexdigest(), 'message': 'message-1'}})
    assert not target.exists()


def test_product_image_grib_smoke_uses_real_packaged_worker_bytes(capsys):
    docker=(ROOT/'engines/geopython-workflow/Dockerfile').read_text()
    smoke=docker.split("RUN python - <<'PYGRIB'\n",1)[1].split('\nPYGRIB',1)[0]
    # Every OPERATORS call already launches the real isolated native worker.
    # Execute the image's exact assertions without a redundant outer process.
    exec(compile(smoke,'Dockerfile:PYGRIB','exec'),{})
    captured=capsys.readouterr()
    assert 'GRIB2 native units and bitmap image smoke passed' in captured.out
    assert 'GRIB1 values bitmap time and layer image smoke passed' in captured.out
    assert 'GRIB2 complex spatial interval smoke passed' in captured.out
    assert 'GRIB2 decimal Float32 precision smoke passed' in captured.out
    assert 'GRIB2 ECMWF CCSDS complete stream and values smoke passed' in captured.out
    assert 'GRIB2 ECMWF CCSDS precipitation interval and source metres smoke passed' in captured.out
    assert 'GRIB2 ECMWF CCSDS 0h exact zero smoke passed' in captured.out
    assert 'GRIB2 ECMWF 0.1 degree precipitation smoke passed' in captured.out


@pytest.mark.parametrize('reverse',[False,True])
def test_noaa_complex_spatial_oracle_red_green(tmp_path,reverse):
    fields=FIXTURE.complex_fields(reverse);spec=dict(name='complex.grib2',packing='complex-spatial',fields=fields)
    raw=FIXTURE.source_bytes(spec);path=tmp_path/spec['name'];path.write_bytes(raw)
    plan={'schema_version':'addp.workflow.access-plan/v1','source':{'kind':'file','format':'grib','access':{'method':'mounted_path','path':str(path)}}}
    info=grib.OPERATORS['grib.inspect']['function'](access_plan=plan)
    for field in fields:
        result=grib.OPERATORS['grib.read_slice']['function'](access_plan=plan,source_version=info['format_info']['source_version'],selection=dict(variable='message-'+str(field['number']),indices=[],x=0,y=0,width=field['width'],height=field['height']))
        assert (result['values'],result['valid'])==FIXTURE.expected(field)
        assert result['variable']['attributes']['GRIB_UNIT']['values']==[field['unit']]
        assert result['variable']['message']['data_template']==3


def test_complex_decimal_precision_is_explicit_and_independently_bounded(tmp_path):
    spec=FIXTURE.sources()[4];field=spec['fields'][1]
    raw=FIXTURE.complex_message(field);path=tmp_path/'decimal.grib2';path.write_bytes(raw)
    plan={'schema_version':'addp.workflow.access-plan/v1','source':{'kind':'file','format':'grib','access':{'method':'mounted_path','path':str(path)}}}
    info=grib.OPERATORS['grib.inspect']['function'](access_plan=plan)
    result=grib.OPERATORS['grib.read_slice']['function'](access_plan=plan,source_version=info['format_info']['source_version'],selection=dict(variable='message-1',indices=[],x=0,y=0,width=field['width'],height=field['height']))
    assert result['variable']['attributes']['GRIB2_DECODE_PRECISION']['values']==['float32_scaling_float64_output']
    assert (result['values'],result['valid'])==FIXTURE.expected(field)
    assert result['values']!=FIXTURE.source_values(field)[0]
    audit=FIXTURE.precision_bound(field,result['values'],result['valid'])
    assert 0<audit['max_absolute_error']<=audit['max_derived_bound']


def test_preexisting_grib1_grib2_hosted_bytes_are_unchanged():
    expected = {
        'mixed-fields.grib2': '98d4150bb752fbdd54219060d67e7f3446e0263c6fff1bd2fbe8d4992f61f110',
        'south-to-north.grb2': 'addd8a56405b421b3645e592808af3c576ee501ed325e93651f30a891ca4dce6',
        'regular-fields.grib1': 'fdb514b1f996bb192b2507613af42ece2d4d7e89fa2976f4627d9710e20a6232',
        'south-to-north.grb1': '6e1f711d8624742c3cfb9cbae4c0641763f079b1fbf1b19b616d4a0ea3815116',
        'complex-fields.grib2': '76e83b64e1ccbd32790e8825fd731fd5eb57b351289bd124f7617d1af811e9da',
        'complex-south-to-north.grb2': '9a624331a8980ee2fdd5d6a0eba7f14b69ccc1d28e720a4f0661aec8803b7b0b',
        'ecmwf-ccsds.grib2': '20604c89b64afc613a504d20da4085d89dd3e0ce4de4f0ce6808c3a982c17590',
        'ecmwf-precipitation.grib2': '0c7f69c585c2d52bdc37f0db7894e753b5806f12416f750ed37cef28147cbaee',
        'ecmwf-precipitation-0h.grib2': 'be63fa46f1c52f8c37198e43ef56a12dce28fd23edee15bf759206c67dfc81a0',
        FIXTURE.NOAA_INTERVALS: '3f6b645fe67ac2000a80a55b99b2c4ee59d86653a8e347bd5259bcafb95f7585',
    }
    assert {s['name']:s['sha256'] for s in FIXTURE.sources() if s['name'] in expected} == expected

@pytest.mark.parametrize('fault', ['empty', 'truncated', 'extra-zero', 'extra-ff',
                                  'unknown-local', 'missing-local', 'local-parameter', 'bad-flags'])
def test_ecmwf_ccsds_invalid_stream_or_identity_rejected_before_gdal(monkeypatch, fault):
    raw = FIXTURE.ccsds_message(FIXTURE.ccsds_fields()[0])
    sections = []
    offset = 16
    while offset < len(raw) - 4:
        length = int.from_bytes(raw[offset:offset+4], 'big')
        sections.append(bytearray(raw[offset:offset+length]))
        offset += length
    if fault in ('empty', 'truncated', 'extra-zero', 'extra-ff'):
        payload = sections[-1][5:]
        payload = {'empty': b'', 'truncated': payload[:-1], 'extra-zero': payload + b'\0',
                   'extra-ff': payload + b'\xff'}[fault]
        sections[-1] = (len(payload) + 5).to_bytes(4, 'big') + b'\x07' + payload
    elif fault == 'unknown-local':
        sections[1][-1] ^= 1
    elif fault == 'missing-local':
        del sections[1]
    elif fault == 'local-parameter':
        sections[3][9:11] = bytes([1, 193])
    else:
        sections[4][21] = 0
    body = b''.join(sections) + b'7777'
    source = raw[:8] + (len(body) + 16).to_bytes(8, 'big') + body
    monkeypatch.setattr(grib.gdal, 'OpenEx', lambda *args, **kwargs: pytest.fail('invalid source reached GDAL'))
    with pytest.raises(ValueError, match='ECMWF CCSDS'):
        grib._fields(source)


def replace_precipitation_bytes(raw, section_id, start, stop, replacement):
    sections = []
    offset = 16
    while offset < len(raw) - 4:
        length = int.from_bytes(raw[offset:offset + 4], 'big')
        section = bytearray(raw[offset:offset + length])
        if section[4] == section_id:
            section[start:stop] = replacement
            section[:4] = len(section).to_bytes(4, 'big')
        sections.append(section)
        offset += length
    body = b''.join(sections) + b'7777'
    return raw[:8] + (len(body) + 16).to_bytes(8, 'big') + body


@pytest.mark.parametrize('section_id, position, replacement, reason', [
    (1, 10, b'\x00', 'identification'),
    (3, 63, b'\x00\x0f\x42\x40', 'grid'),
    (4, 10, b'\x08', 'parameter or level'),
    (4, 22, b'\x67', 'parameter or level'),
    (4, 38, b'\x08', 'end time'),
    (4, 18, b'\x00\x00\x00\x01', 'interval'),
    (4, 49, b'\x00\x00\x00\x00', 'end time'),
    (4, 53, b'\xff', 'interval'),
    (4, 57, b'\xc3', 'interval'),
    (5, 19, b'\x00', 'packing'),
    (7, -1, b'', 'codestream'),
])
def test_ecmwf_precipitation_rejects_conflicting_source_facts(monkeypatch, section_id, position, replacement, reason):
    raw = FIXTURE.ccsds_message(FIXTURE.ccsds_precipitation_fields()[0])
    stop = None if position == -1 else position + len(replacement)
    source = replace_precipitation_bytes(raw, section_id, position, stop, replacement)
    monkeypatch.setattr(grib.gdal, 'OpenEx', lambda *a, **k: pytest.fail('invalid source reached GDAL'))
    with pytest.raises(ValueError, match=reason):
        grib._fields(source)


@pytest.mark.parametrize('section_id, start, stop, replacement, reason', [
    (4, 38, 39, b'\x04', 'end time'),
    (5, 5, 9, b'\x00\x00\x00\x1f', 'packed value count'),
    (5, 11, 15, b'\x3f\x80\x00\x00', '0h zero field'),
    (5, 11, 15, b'\x80\x00\x00\x00', '0h zero field'),
    (5, 16, 17, b'\x01', '0h zero field'),
    (5, 18, 19, b'\x01', 'packing'),
    (5, 19, 20, b'\x0c', '0h zero field'),
    (6, 5, 6, b'\x00\xff\xff\xff\xff', 'packing'),
    (7, 5, 5, b'\x00', '0h zero field'),
])
def test_ecmwf_initial_precipitation_requires_exact_constant_profile(monkeypatch, section_id, start, stop, replacement, reason):
    spec = next(spec for spec in FIXTURE.sources() if spec['case_id'] == 'ecmwf-precipitation-0h')
    source = replace_precipitation_bytes(FIXTURE.source_bytes(spec), section_id, start, stop, replacement)
    monkeypatch.setattr(grib.gdal, 'OpenEx', lambda *a, **k: pytest.fail('invalid source reached GDAL'))
    with pytest.raises(ValueError, match=reason):
        grib._fields(source)


@pytest.mark.parametrize('high_resolution, section, start, stop, value, reason', [
    (True, 4, 54, 58, 450, 'precipitation interval'),
    (False, 4, 54, 58, 225, 'precipitation interval'),
    (True, 3, 59, 63, 179800000, 'precipitation 0.1 grid'),
    (True, 3, 63, 67, 250000, 'precipitation grid'),
    (True, 3, 71, 72, 64, 'precipitation grid'),
])
def test_ecmwf_precipitation_binds_increment_to_grid(monkeypatch, high_resolution, section, start, stop, value, reason):
    if high_resolution:
        raw = FIXTURE.source_bytes({'name': FIXTURE.ECMWF_HIGH_RES_PRECIPITATION})[:224]
    else:
        raw = FIXTURE.ccsds_message(FIXTURE.ccsds_precipitation_fields()[0])
    corrupted = replace_precipitation_bytes(raw, section, start, stop, value.to_bytes(stop-start, 'big'))
    monkeypatch.setattr(grib.gdal, 'OpenEx', lambda *a, **k: pytest.fail('invalid source reached GDAL'))
    with pytest.raises(ValueError, match=reason):
        grib._fields(corrupted)


def test_ecmwf_high_resolution_retains_field_and_slice_budgets(tmp_path):
    raw = FIXTURE.source_bytes({'name': FIXTURE.ECMWF_HIGH_RES_PRECIPITATION})[:224]
    assert len(grib._fields(raw * 10)) == 10
    with pytest.raises(ValueError, match='total grid budget'):
        grib._fields(raw * 11)
    path = tmp_path/'global-zero.grib2'
    path.write_bytes(raw)
    plan = dict(schema_version='addp.workflow.access-plan/v1',
                source=dict(kind='file', format='grib', access=dict(method='mounted_path', path=str(path))))
    inspect = grib.OPERATORS['grib.inspect']['function'](access_plan=plan)
    read = grib.OPERATORS['grib.read_slice']['function']
    version = inspect['format_info']['source_version']
    selection = dict(variable='message-1', indices=[], x=3472, y=1769, width=128, height=32)
    result = read(access_plan=plan, source_version=version, selection=selection)
    assert result['values'] == [['0.0'] * 128 for _ in range(32)]
    assert result['valid'] == [[True] * 128 for _ in range(32)]
    for invalid in (dict(selection, x=3473), dict(selection, y=1768, height=33)):
        assert read(access_plan=plan, source_version=version, selection=invalid)['error_code'] == 'GRIB_SLICE_INVALID'
