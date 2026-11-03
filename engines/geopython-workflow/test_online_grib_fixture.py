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
    path.write_bytes(raw[:-1]+bytes([raw[-1]^1]))
    assert grib.OPERATORS['grib.read_slice']['function'](access_plan=plan,source_version=info['format_info']['source_version'],selection=select)['error_code']=='GRIB_SOURCE_CHANGED'
