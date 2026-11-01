"""Exact independent classic CDF fixture and physical cleanup in Hosted isolation."""
from __future__ import annotations
import hashlib
import io
import json
import os
from pathlib import Path
import re
import struct
import subprocess
import sys

NAMES = ('cdf1-unknown.nc', 'cdf2-projected.nc', 'cdf5-int64.nc')

def build(version, projected=False):
    # Encoder follows Unidata/PnetCDF framing; does not invoke the product reader.
    number=lambda n:struct.pack('>Q' if version==5 else '>I',n)
    word=lambda n:struct.pack('>I',n)
    def text(value):
        raw=value.encode();return number(len(raw))+raw+b'\0'*((-len(raw))%4)
    codes={4:'i',6:'d',10:'q'}
    def attributes(items):
        if not items:return word(0)+number(0)
        result=word(12)+number(len(items))
        for name,kind,value in items:
            raw=value.encode() if kind==2 else struct.pack('>'+codes[kind]*len(value),*value)
            count=len(raw) if kind==2 else len(value)
            result+=text(name)+word(kind)+number(count)+raw+b'\0'*((-len(raw))%4)
        return result
    base=9007199254740993 if version==5 else 0
    values=[base+i for i in range(24)];values[6]=-9999
    kind=10 if version==5 else 4
    variables=[dict(name='temperature',type=kind,dims=[0,1,2],values=values,
        attrs=[('_FillValue',kind,[-9999]),('units',2,'K'),('scale_factor',6,[.5]),('add_offset',6,[273.15])]),
        dict(name='time',type=4,dims=[0],values=[0,30],attrs=[('units',2,'days since 2000-01-01'),('calendar',2,'360_day'),('axis',2,'T')])]
    if projected:
        # Fixed declared UTM50N, checked independently as exact 10m grid below.
        wkt='PROJCS["WGS 84 / UTM zone 50N",GEOGCS["WGS 84",DATUM["WGS_1984",SPHEROID["WGS 84",6378137,298.257223563]],PRIMEM["Greenwich",0],UNIT["degree",0.0174532925199433]],PROJECTION["Transverse_Mercator"],PARAMETER["latitude_of_origin",0],PARAMETER["central_meridian",117],PARAMETER["scale_factor",0.9996],PARAMETER["false_easting",500000],PARAMETER["false_northing",0],UNIT["metre",1],AUTHORITY["EPSG","32650"]]'
        variables[0]['attrs'].append(('grid_mapping',2,'crs'))
        variables.extend([
          dict(name='y',type=6,dims=[1],values=[3000000.,3000010.,3000020.],attrs=[('axis',2,'Y'),('standard_name',2,'projection_y_coordinate'),('units',2,'m')]),
          dict(name='x',type=6,dims=[2],values=[500000.,500010.,500020.,500030.],attrs=[('axis',2,'X'),('standard_name',2,'projection_x_coordinate'),('units',2,'m')]),
          dict(name='crs',type=4,dims=[],values=[0],attrs=[('spatial_ref',2,wkt),('grid_mapping_name',2,'transverse_mercator')]),
        ])
    data=[]
    for v in variables:
        raw=struct.pack('>'+codes[v['type']]*len(v['values']),*v['values']);data.append(raw+b'\0'*((-len(raw))%4))
    def header(begins):
        result=b'CDF'+bytes([version])+number(0)+word(10)+number(3)
        result+=b''.join(text(name)+number(size) for name,size in [('time',2),('y',3),('x',4)])
        result+=attributes([('title',2,'ADDP synthetic classic NetCDF')])+word(11)+number(len(variables))
        for v,raw,start in zip(variables,data,begins):
            result+=text(v['name'])+number(len(v['dims']))+b''.join(number(i) for i in v['dims'])+attributes(v['attrs'])+word(v['type'])+number(len(raw))
            result+=struct.pack('>Q',start) if version in (2,5) else word(start)
        return result
    offset=len(header([0]*len(variables)));begins=[]
    for raw in data:begins.append(offset);offset+=len(raw)
    return header(begins)+b''.join(data)


def expected_values(version, layer, x=0,y=0,width=4,height=3):
    base=9007199254740993 if version==5 else 0
    return [[str(-9999 if layer==0 and row==1 and col==2 else base+layer*12+row*4+col) for col in range(x,x+width)] for row in range(y,y+height)]


def sources():
    return [dict(name=name,version=version,projected=version==2,size_bytes=len(build(version,version==2)),sha256=hashlib.sha256(build(version,version==2)).hexdigest()) for name,version in zip(NAMES,(1,2,5))]


def worker(request):
    from minio import Minio
    storage=request['storage'];client=Minio(storage['endpoint'],access_key=storage['access_key'],secret_key=storage['secret_key'],secure=False)
    root=Path(request['nfs_root']);bucket=storage['bucket'];action=request['action']
    directory=root/'netcdf'
    if action=='seed':
        directory.mkdir(mode=0o755,exist_ok=False)
        for spec in sources():
            raw=build(spec['version'],spec['projected']);path=directory/spec['name'];path.write_bytes(raw);path.chmod(0o644)
            client.put_object(bucket,'netcdf/'+spec['name'],io.BytesIO(raw),len(raw),content_type='application/x-netcdf')
        return {'sources':sources()}
    if action in ('drift','restore'):
        spec=sources()[-1];raw=build(5)
        if action=='drift':raw=raw[:-8]+struct.pack('>q',777)
        path=directory/spec['name'];path.write_bytes(raw);path.chmod(0o644)
        client.put_object(bucket,'netcdf/'+spec['name'],io.BytesIO(raw),len(raw),content_type='application/x-netcdf')
        return {'sha256':hashlib.sha256(raw).hexdigest(),'changed':action=='drift'}
    if action=='verify':
        for spec in sources():
            raw=(directory/spec['name']).read_bytes();response=client.get_object(bucket,'netcdf/'+spec['name'])
            try:remote=response.read()
            finally:response.close();response.release_conn()
            assert raw==remote and len(raw)==spec['size_bytes'] and hashlib.sha256(raw).hexdigest()==spec['sha256'],'source_changed'
        return {'sources_unchanged':True,'sources':sources()}
    if action=='delete':
        for name in NAMES:
            (directory/name).unlink(missing_ok=True);client.remove_object(bucket,'netcdf/'+name)
        remaining=list(client.list_objects(bucket,prefix='netcdf/',recursive=True));assert not remaining,'object_cleanup'
        directory.rmdir()
        assert not directory.exists(),'nfs_cleanup'
        return {'objects_removed':True,'files_removed':True}
    raise ValueError('unknown NetCDF physical action')


def physical(action,repository,environment):
    if action not in {'seed','verify','drift','restore','delete'}:raise ValueError('unknown fixture action')
    for key,value in {'ADDP_ONLINE_HOSTED':'1','GITHUB_ACTIONS':'true','RUNNER_OS':'Linux','ADDP_ONLINE_MANAGER_CASE':'native-netcdf'}.items():
        if environment.get(key)!=value:raise ValueError('NetCDF requires exclusive Hosted case')
    state=Path(environment['ADDP_ONLINE_SECRET_DIR'])/'nfs-root'
    if state.is_symlink():raise ValueError('invalid fixture state')
    root=Path(state.read_text().strip())
    if not re.fullmatch(r'/tmp/addp-online-manager-nfs\.[A-Za-z0-9]{6}',str(root)) or root.is_symlink() or (root/'.addp-online-owner').read_text().strip()!=environment['ADDP_ONLINE_TEST_RUN_ID']:
        raise ValueError('NetCDF fixture ownership mismatch')
    config={'action':action,'nfs_root':str(root),'storage':{'endpoint':'127.0.0.1:'+environment['ADDP_ONLINE_MANAGER_MINIO_PORT'],'access_key':environment['ADDP_ONLINE_MANAGER_MINIO_ACCESS_KEY'],'secret_key':environment['ADDP_ONLINE_MANAGER_MINIO_SECRET_KEY'],'bucket':environment['ADDP_ONLINE_MANAGER_MINIO_BUCKET']}}
    script=Path(repository)/'business/scripts/online-netcdf-fixture.py'
    result=subprocess.run(['docker','run','--rm','-i','--user',f'{os.getuid()}:{os.getgid()}','--name','addp-manager-raster-verifier','--network','host','--label','com.addp.online-fixture=manager-internal-artifact-lineage','--mount',f'type=bind,src={script},dst=/fixture.py,readonly','--mount',f'type=bind,src={root},dst={root}',environment['ADDP_ONLINE_RASTER_RUNTIME_IMAGE'],'python','/fixture.py','worker'],input=json.dumps(config),text=True,capture_output=True,timeout=120)
    if result.returncode:raise ValueError(f'NetCDF physical {action} failed (exit {result.returncode})')
    return json.loads(result.stdout)


if __name__=='__main__':
    try:print(json.dumps(worker(json.load(sys.stdin))))
    except Exception as error:
        print(json.dumps({'error_code':type(error).__name__}));raise SystemExit(1)
