"""Independent Hosted bytes traversing the real short-lived Native operators."""
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
        native=result['variable'];assert native['attributes']['GRIB_UNIT']['values']==['[K]']
        assert native['grid']['transform']==[field['west']-.5,1.,0.,field['north']+.5,0.,-1.]
        assert native['attributes']['GRIB_FORECAST_SECONDS']['values']==[str(field['forecast']*3600)]
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
