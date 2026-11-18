"""Independent Hosted bytes traversing the real short-lived Native operators."""
from datetime import datetime, timedelta, timezone
import importlib.util
from pathlib import Path
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
    assert [c['name'] for c in info['container']['children']]==['message-1','message-2','message-3']
    for field in spec['fields']:
        select=dict(variable='message-'+str(field['number']),indices=[],x=0,y=0,width=field['width'],height=field['height'])
        result=grib.OPERATORS['grib.read_slice']['function'](access_plan=plan,source_version=info['format_info']['source_version'],selection=select)
        assert (result['values'],result['valid'])==FIXTURE.expected(field)
        native=result['variable'];assert native['attributes']['GRIB_UNIT']['values']==[field.get('unit','[K]')]
        spacing = field.get('spacing', 1)
        assert native['grid']['transform']==[field['west']-spacing/2,spacing,0.,field['north']+spacing/2,0.,-spacing]
        assert native['attributes']['GRIB_FORECAST_SECONDS']['values']==[str(field.get('start_hour',field['forecast'])*3600)]
        if spec.get('packing') == 'ccsds':
            assert native['message']['data_template'] == 42
            assert native['message']['local']['mars_stream'] == 'oper'
            assert native['attributes']['GRIB2_DECODE_PRECISION']['values'] == ['float32_scaling_float64_output']
            if field.get('product_template') == 8:
                facts = native['message']
                start = datetime(2026, 1, 2, 3, tzinfo=timezone.utc)
                assert facts['product_template'] == 8 and facts['parameter']['local_table'] == 1
                assert facts['level'] == dict(type_code=1, raw_scale=None, raw_value=None, reference='surface')
                assert facts['time']['operation'] == 'accumulation'
                assert facts['time']['start_utc'] == start.isoformat()
                assert facts['time']['end_utc'] == (start + timedelta(hours=field['forecast'])).isoformat()
                assert (facts['time']['increment_unit'], facts['time']['increment_value']) == (13, 450)
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


@pytest.mark.parametrize('section_id, position, replacement, reason', [
    (1, 10, b'\x00', 'identification'),
    (3, 63, b'\x00\x0f\x42\x40', 'grid'),
    (4, 10, b'\x08', 'parameter or level'),
    (4, 22, b'\x67', 'parameter or level'),
    (4, 38, b'\x08', 'end time'),
    (4, 18, b'\x00\x00\x00\x01', 'interval'),
    (4, 49, b'\x00\x00\x00\x00', 'interval'),
    (4, 53, b'\xff', 'interval'),
    (4, 57, b'\xc3', 'interval'),
    (5, 19, b'\x00', 'packing'),
    (7, -1, b'', 'codestream'),
])
def test_ecmwf_precipitation_rejects_conflicting_source_facts(monkeypatch, section_id, position, replacement, reason):
    raw = FIXTURE.ccsds_message(FIXTURE.ccsds_precipitation_fields()[0])
    sections = []
    offset = 16
    while offset < len(raw) - 4:
        length = int.from_bytes(raw[offset:offset + 4], 'big')
        section = bytearray(raw[offset:offset + length])
        if section[4] == section_id:
            if position == -1:
                section.pop()
                section[:4] = len(section).to_bytes(4, 'big')
            else:
                section[position:position + len(replacement)] = replacement
        sections.append(section)
        offset += length
    body = b''.join(sections) + b'7777'
    source = raw[:8] + (len(body) + 16).to_bytes(8, 'big') + body
    monkeypatch.setattr(grib.gdal, 'OpenEx', lambda *a, **k: pytest.fail('invalid source reached GDAL'))
    with pytest.raises(ValueError, match=reason):
        grib._fields(source)
