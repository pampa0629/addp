"""Independent WMO GRIB2 byte oracle and exclusive Hosted source lifecycle."""
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

NAMES=('mixed-fields.grib2','south-to-north.grb2')


def fields(reverse=False):
    return [dict(number=i+1,width=w,height=h,packing=packing,scan=64 if reverse else 0,
        west=west,north=north,forecast=3*(i+1),base=base,missing=missing)
        for i,(w,h,packing,west,north,base,missing) in enumerate([
            (4,3,'simple',110,30,20,False),
            (3,2,'ieee64',250,-10,280.25,True),
            (2,4,'ieee32',170,50,300.5,False)])]


def expected(field,x=0,y=0,width=None,height=None):
    width=field['width'] if width is None else width;height=field['height'] if height is None else height
    values=[];valid=[]
    for row in range(y,y+height):
        line=[];mask=[]
        for col in range(x,x+width):
            present=not(field['missing'] and row==1 and col==1)
            value=9999. if field['missing'] and row==0 and col==0 else float(field['base']+row*field['width']+col)
            line.append(repr(value) if present else 'NaN');mask.append(present)
        values.append(line);valid.append(mask)
    return values,valid


def message(field):
    # WMO sections encoded independently of the product GDAL reader.
    def section(n,length):
        raw=bytearray(length);raw[:4]=struct.pack('>I',length);raw[4]=n;return raw
    def u32(raw,p,value):raw[p:p+4]=struct.pack('>I',value)
    def angle(value):return int(abs(value)*1_000_000)|(0x80000000 if value<0 else 0)
    width,height=field['width'],field['height'];scan=field['scan']
    values,valid=expected(field);rows=range(height-1,-1,-1) if scan==64 else range(height)
    points=[(values[y][x],valid[y][x]) for y in rows for x in range(width)]
    s1=section(1,21);s1[5:7]=struct.pack('>H',7);s1[9]=2;s1[11]=1;s1[12:14]=struct.pack('>H',2026);s1[14:19]=bytes([1,2,3,0,0]);s1[20]=1
    s3=section(3,72);u32(s3,6,width*height);s3[14]=6;u32(s3,30,width);u32(s3,34,height)
    south=field['north']-height+1
    u32(s3,46,angle(south if scan==64 else field['north']));u32(s3,50,angle(field['west']));s3[54]=48
    u32(s3,55,angle(field['north'] if scan==64 else south));u32(s3,59,angle(field['west']+width-1));u32(s3,63,1_000_000);u32(s3,67,1_000_000);s3[71]=scan
    s4=section(4,34);s4[11]=2;s4[17]=1;u32(s4,18,field['forecast']);s4[22]=103;u32(s4,24,2);s4[28:34]=b'\xff'*6
    selected=[float(v) for v,ok in points if ok];simple=field['packing']=='simple'
    s5=section(5,21 if simple else 12);u32(s5,5,len(selected))
    if simple:
        s5[19]=8;payload=bytes(int(v) for v in selected)
    else:
        s5[9:11]=struct.pack('>H',4);s5[11]=2 if field['packing']=='ieee64' else 1
        payload=struct.pack('>'+('d' if s5[11]==2 else 'f')*len(selected),*selected)
    s6=section(6,6+(width*height+7)//8 if field['missing'] else 6);s6[5]=0 if field['missing'] else 255
    if field['missing']:
        for i,(_,ok) in enumerate(points):
            if ok:s6[6+i//8]|=1<<(7-i%8)
    s7=section(7,5+len(payload));s7[5:]=payload
    body=s1+s3+s4+s5+s6+s7+b'7777'
    return b'GRIB\0\0\0\x02'+struct.pack('>Q',16+len(body))+body


def source_bytes(spec):return b''.join(message(field) for field in spec['fields'])


def sources():
    result=[]
    for reverse,name in enumerate(NAMES):
        spec=dict(name=name,case_id='reverse' if reverse else 'forward',fields=fields(bool(reverse)))
        raw=source_bytes(spec);spec.update(size_bytes=len(raw),sha256=hashlib.sha256(raw).hexdigest());result.append(spec)
    return result

def _owned_sources(request):
    specs=request['sources'];raws=request['source_bytes']
    if len(specs)!=len(NAMES) or {s['name'] for s in specs}!=set(NAMES) or set(raws)!=set(NAMES):
        raise ValueError('incomplete GRIB fixture inventory')
    result=[]
    for spec in specs:
        raw=bytes.fromhex(raws[spec['name']])
        if len(raw)!=spec['size_bytes'] or hashlib.sha256(raw).hexdigest()!=spec['sha256']:
            raise ValueError('fixture bytes differ from declared independent source')
        result.append((spec,raw))
    return result


def worker(request):
    from minio import Minio
    storage=request['storage'];client=Minio(storage['endpoint'],access_key=storage['access_key'],secret_key=storage['secret_key'],secure=False)
    root=Path(request['nfs_root']);bucket=storage['bucket'];action=request['action']
    directory=root/'grib'
    if action=='seed':
        owned=_owned_sources(request)
        directory.mkdir(mode=0o755,exist_ok=False)
        for spec,raw in owned:
            path=directory/spec['name'];path.write_bytes(raw);path.chmod(0o644)
            client.put_object(bucket,'grib/'+spec['name'],io.BytesIO(raw),len(raw),content_type='application/x-grib')
        return {'sources':[spec for spec,_ in owned]}
    if action in ('drift','restore'):
        for spec,raw in _owned_sources(request):
            if action=='drift':raw=raw[:-1]+bytes([raw[-1]^1])
            path=directory/spec['name'];path.write_bytes(raw);path.chmod(0o644)
            client.put_object(bucket,'grib/'+spec['name'],io.BytesIO(raw),len(raw),content_type='application/x-grib')
        return {'changed':action=='drift'}
    if action=='verify':
        owned=_owned_sources(request)
        for spec,expected in owned:
            raw=(directory/spec['name']).read_bytes();response=client.get_object(bucket,'grib/'+spec['name'])
            try:remote=response.read()
            finally:response.close();response.release_conn()
            assert raw==remote==expected and len(raw)==spec['size_bytes'] and hashlib.sha256(raw).hexdigest()==spec['sha256'],'source_changed'
        return {'sources_unchanged':True,'sources':[spec for spec,_ in owned]}
    if action=='delete':
        for name in NAMES:
            (directory/name).unlink(missing_ok=True);client.remove_object(bucket,'grib/'+name)
        remaining=list(client.list_objects(bucket,prefix='grib/',recursive=True));assert not remaining,'object_cleanup'
        directory.rmdir()
        assert not directory.exists(),'nfs_cleanup'
        return {'objects_removed':True,'files_removed':True}
    raise ValueError('unknown GRIB physical action')


def physical(action,repository,environment):
    if action not in {'seed','verify','drift','restore','delete'}:raise ValueError('unknown fixture action')
    for key,value in {'ADDP_ONLINE_HOSTED':'1','GITHUB_ACTIONS':'true','RUNNER_OS':'Linux','ADDP_ONLINE_MANAGER_CASE':'native-grib'}.items():
        if environment.get(key)!=value:raise ValueError('GRIB requires exclusive Hosted case')
    state=Path(environment['ADDP_ONLINE_SECRET_DIR'])/'nfs-root'
    if state.is_symlink():raise ValueError('invalid fixture state')
    root=Path(state.read_text().strip())
    if not re.fullmatch(r'/tmp/addp-online-manager-nfs\.[A-Za-z0-9]{6}',str(root)) or root.is_symlink() or (root/'.addp-online-owner').read_text().strip()!=environment['ADDP_ONLINE_TEST_RUN_ID']:
        raise ValueError('GRIB fixture ownership mismatch')
    specs=sources()
    config={'action':action,'nfs_root':str(root),'sources':specs,'source_bytes':{spec['name']:source_bytes(spec).hex() for spec in specs},'storage':{'endpoint':'127.0.0.1:'+environment['ADDP_ONLINE_MANAGER_MINIO_PORT'],'access_key':environment['ADDP_ONLINE_MANAGER_MINIO_ACCESS_KEY'],'secret_key':environment['ADDP_ONLINE_MANAGER_MINIO_SECRET_KEY'],'bucket':environment['ADDP_ONLINE_MANAGER_MINIO_BUCKET']}}
    script=Path(repository)/'business/scripts/online-grib-fixture.py'
    result=subprocess.run(['docker','run','--rm','-i','--user',f'{os.getuid()}:{os.getgid()}','--name','addp-manager-raster-verifier','--network','host','--label','com.addp.online-fixture=manager-internal-artifact-lineage','--mount',f'type=bind,src={script},dst=/fixture.py,readonly','--mount',f'type=bind,src={root},dst={root}',environment['ADDP_ONLINE_RASTER_RUNTIME_IMAGE'],'python','/fixture.py','worker'],input=json.dumps(config),text=True,capture_output=True,timeout=120)
    if result.returncode:raise ValueError(f'GRIB physical {action} failed (exit {result.returncode})')
    return json.loads(result.stdout)


if __name__=='__main__':
    try:print(json.dumps(worker(json.load(sys.stdin))))
    except Exception as error:
        print(json.dumps({'error_code':type(error).__name__}));raise SystemExit(1)
