import copy
import importlib
from pathlib import Path
import tempfile
import types
import sys
import unittest
from unittest import mock

SCENE=importlib.import_module('scripts.test.manager-grib-online')
FIXTURE=SCENE.FIXTURE

class GRIBOracleTest(unittest.TestCase):
    def test_closed_inventory_and_bytes_before_physical_mutation(self):
        request={'sources':FIXTURE.sources(),'source_bytes':{s['name']:FIXTURE.source_bytes(s).hex() for s in FIXTURE.sources()}}
        self.assertEqual(len(FIXTURE._owned_sources(request)),2)
        for change in ('missing','tampered'):
            bad=copy.deepcopy(request)
            if change=='missing':bad['sources'].pop()
            else:bad['source_bytes'][FIXTURE.NAMES[0]]='00'
            with self.assertRaises(ValueError):FIXTURE._owned_sources(bad)
        for spec in FIXTURE.sources():
            raw=FIXTURE.source_bytes(spec);offset=0;count=0
            while offset<len(raw):
                self.assertEqual(raw[offset:offset+8],b'GRIB\0\0\0\2')
                length=int.from_bytes(raw[offset+8:offset+16],'big');self.assertEqual(raw[offset+length-4:offset+length],b'7777');offset+=length;count+=1
            self.assertEqual((offset,count),(len(raw),3))

    def response(self,spec,field):
        values,valid=FIXTURE.expected(field)
        return dict(schema_version='gdal.grib.slice/v1',format='grib',source_version='sha256:'+spec['sha256'],value_mode='decoded_native_units',
            selection=dict(variable='message-'+str(field['number']),indices=[],x=0,y=0,width=field['width'],height=field['height']),values=values,valid=valid,spatial=None,
            variable=dict(attributes={'GRIB_UNIT':{'values':['[K]']},'GRIB_REF_TIME':{'values':['1767322800']},'GRIB_VALID_TIME':{'values':[str(1767322800+field['forecast']*3600)]},'GRIB_SHORT_NAME':{'values':['2-HTGL']},'GRIB_ELEMENT':{'values':['TMP']},'GRIB_FORECAST_SECONDS':{'values':[str(field['forecast']*3600)]}},
              dimensions=[dict(name='y',size=str(field['height']),axis_type='HORIZONTAL_Y',direction='NORTH_TO_SOUTH'),dict(name='x',size=str(field['width']),axis_type='HORIZONTAL_X',direction='WEST_TO_EAST')],
              grid=dict(transform=[field['west']-.5,1.,0.,field['north']+.5,0.,-1.],longitude_policy='unchanged',crs_wkt='source CRS')))

    def test_acceptance_rejects_unit_value_mask_longitude_and_message_drift(self):
        spec=FIXTURE.sources()[0];field=spec['fields'][1];result=self.response(spec,field);SCENE.validate_slice(result,spec,field)
        self.assertEqual((result['values'][0][0],result['valid'][0][0]),('9999.0',True))
        for change in [lambda r:r.__setitem__('value_mode','raw_storage'),lambda r:r['values'][0].__setitem__(1,'7.1'),lambda r:r['valid'][0].__setitem__(0,False),lambda r:r['selection'].__setitem__('variable','message-1'),lambda r:r['variable']['grid']['transform'].__setitem__(0,-110.5),lambda r:r['variable']['attributes']['GRIB_UNIT'].__setitem__('values',['[C]'])]:
            bad=copy.deepcopy(result);change(bad)
            with self.assertRaises(ValueError):SCENE.validate_slice(bad,spec,field)

    def test_browser_dom_presence_cannot_replace_viewport_evidence(self):
        spec=FIXTURE.sources()[0];case=dict(locator='exact-source',spec=spec)
        observed=dict(locator='exact-source',values_verified=True,viewport_grid_verified=True,visible_message_count=3,download_sha256=spec['sha256'],download_size=spec['size_bytes'])
        SCENE.validate_browser_case(case,observed)
        for field,value in [('viewport_grid_verified',False),('visible_message_count',2),('download_sha256','wrong')]:
            bad=dict(observed);bad[field]=value
            with self.assertRaises(ValueError):SCENE.validate_browser_case(case,bad)
        del observed['viewport_grid_verified']
        with self.assertRaises(ValueError):SCENE.validate_browser_case(case,observed)

    def test_non_hosted_guard_never_starts_container(self):
        with mock.patch.object(FIXTURE.subprocess,'run') as run:
            with self.assertRaises(ValueError):FIXTURE.physical('seed',Path('/repository'),{})
            run.assert_not_called()

    def test_cleanup_keeps_unrelated_sources(self):
        removed=[]
        class Client:
            def __init__(self,*args,**kwargs):pass
            def remove_object(self,bucket,name):removed.append((bucket,name))
            def list_objects(self,*args,**kwargs):return []
        with tempfile.TemporaryDirectory() as d:
            root=Path(d);(root/'grib').mkdir();(root/'unrelated').write_text('keep')
            for name in FIXTURE.NAMES:(root/'grib'/name).write_bytes(b'owned')
            with mock.patch.dict(sys.modules,{'minio':types.SimpleNamespace(Minio=Client)}):
                result=FIXTURE.worker({'storage':dict(endpoint='local',access_key='a',secret_key='b',bucket='test'),'nfs_root':str(root),'action':'delete'})
            self.assertEqual(result,dict(objects_removed=True,files_removed=True));self.assertEqual((root/'unrelated').read_text(),'keep')
            self.assertEqual(removed,[('test','grib/'+name) for name in FIXTURE.NAMES])
