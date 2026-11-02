import copy
import importlib
import struct
import tempfile
from pathlib import Path
import unittest
from unittest import mock

SCENE=importlib.import_module('scripts.test.manager-netcdf-online')
FIXTURE=SCENE.FIXTURE

class NetCDFOracleTest(unittest.TestCase):
    def test_exact_fixture_inventory_rejects_missing_or_changed_bytes(self):
        request={'sources':FIXTURE.sources(),'source_bytes':{s['name']:FIXTURE.source_bytes(s).hex() for s in FIXTURE.sources()}}
        self.assertEqual(len(FIXTURE._owned_sources(request)),7)
        missing=copy.deepcopy(request);missing['sources'].pop()
        with self.assertRaises(ValueError):FIXTURE._owned_sources(missing)
        changed=copy.deepcopy(request);changed['source_bytes']['nc4-int64.nc']='00'
        with self.assertRaises(ValueError):FIXTURE._owned_sources(changed)

    def test_byte_oracle_classic_and_netcdf4_signatures_and_exact_int64(self):
        self.assertEqual([s['version'] for s in FIXTURE.sources()],[1,2,5,4,4,4,4])
        for spec in FIXTURE.sources():
            raw=FIXTURE.source_bytes(spec)
            if spec['version']==4:self.assertEqual(raw[:8],b'\x89HDF\r\n\x1a\n')
            else:self.assertEqual(raw[:4],b'CDF'+bytes([spec['version']]))
            self.assertEqual(len(raw),spec['size_bytes'])
        self.assertIn(struct.pack('>q',9007199254740993),FIXTURE.build(5))
        self.assertEqual(FIXTURE.expected_values(FIXTURE.sources()[2],1,1,1,2,2),[['9007199254741010','9007199254741011'],['9007199254741014','9007199254741015']])

    def response(self,spec,layer=0):
        return dict(schema_version='gdal.netcdf.slice/v1',source_version='sha256:'+spec['sha256'],format='netcdf',value_mode='raw_storage',
          selection=dict(variable='/temperature',indices=[layer],x=0,y=0,width=4,height=3),values=FIXTURE.expected_values(spec,layer),
          valid=[[not(layer==0 and y==1 and x==2) for x in range(4)] for y in range(3)],
          variable={'attributes':{'scale_factor':{'values':['0.5']},'add_offset':{'values':['273.15']}}},
          spatial={'transform':[499995.,10.,0.,2999995.,0.,10.],'footprint':{'geometry':{'type':'Polygon'}}} if spec['projected'] else None)

    def test_acceptance_rejects_precision_mask_grid_and_identity_drift(self):
        for spec in FIXTURE.sources():
            result=self.response(spec);SCENE.validate_slice(result,spec,0)
            for edit in [lambda r:r['values'][0].__setitem__(0,'lost'),lambda r:r['valid'][1].__setitem__(2,True),lambda r:r.__setitem__('source_version','wrong'),lambda r:r['selection'].__setitem__('indices',[1])]:
                bad=copy.deepcopy(result);edit(bad)
                with self.assertRaises(ValueError):SCENE.validate_slice(bad,spec,0)
            bad=copy.deepcopy(result)
            if spec['projected']:bad['spatial']['transform'][1]=100
            else:bad['spatial']={'srid':4326}
            with self.assertRaises(ValueError):SCENE.validate_slice(bad,spec,0)

    def test_physical_guard_refuses_non_hosted_before_docker(self):
        with mock.patch.object(FIXTURE.subprocess,'run') as run:
            with self.assertRaises(ValueError):FIXTURE.physical('seed',Path('/repository'),{})
            run.assert_not_called()

    def test_worker_cleanup_removes_only_named_fixture_files_and_objects(self):
        import sys,types
        class Client:
            def __init__(self,*args,**kwargs):pass
            def remove_object(self,bucket,name):removed.append((bucket,name))
            def list_objects(self,*args,**kwargs):return []
        with tempfile.TemporaryDirectory() as d:
            root=Path(d);(root/'netcdf').mkdir();(root/'unrelated').write_text('keep');removed=[]
            for name in FIXTURE.NAMES:(root/'netcdf'/name).write_bytes(b'owned')
            with mock.patch.dict(sys.modules,{'minio':types.SimpleNamespace(Minio=Client)}):
                result=FIXTURE.worker({'storage':{'endpoint':'local','access_key':'a','secret_key':'b','bucket':'test'},'nfs_root':str(root),'action':'delete'})
            self.assertEqual(result,{'objects_removed':True,'files_removed':True})
            self.assertEqual(removed,[('test','netcdf/'+name) for name in FIXTURE.NAMES]);self.assertEqual((root/'unrelated').read_text(),'keep')
