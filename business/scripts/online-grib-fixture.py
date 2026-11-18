"""Independent WMO GRIB2 byte oracle and exclusive Hosted source lifecycle."""
from __future__ import annotations
from datetime import datetime, timedelta
import hashlib
from fractions import Fraction
import io
import json
import os
from pathlib import Path
import re
import struct
import subprocess
import sys

NAMES=('mixed-fields.grib2','south-to-north.grb2','regular-fields.grib1','south-to-north.grb1','complex-fields.grib2','complex-south-to-north.grb2','ecmwf-ccsds.grib2','ecmwf-precipitation.grib2','ecmwf-precipitation-0h.grib2')


def fields(reverse=False):
    return [dict(number=i+1,width=w,height=h,packing=packing,scan=64 if reverse else 0,
        west=west,north=north,forecast=3*(i+1),base=base,missing=missing)
        for i,(w,h,packing,west,north,base,missing) in enumerate([
            (4,3,'simple',110,30,20,False),
            (3,2,'ieee64',250,-10,280.25,True),
            (2,4,'ieee32',170,50,300.5,False)])]


def source_values(field,x=0,y=0,width=None,height=None):
    width=field['width'] if width is None else width;height=field['height'] if height is None else height
    values=[];valid=[]
    for row in range(y,y+height):
        line=[];mask=[]
        for col in range(x,x+width):
            present=not(field['missing'] and row==1 and col==1)
            value=9999. if field['missing'] and row==0 and col==0 else float(field['base']+(0 if field.get('constant') else row*field['width']+col)*field.get('step',1))
            if field.get('packing')=='complex-spatial' and not(field['missing'] and row==0 and col==0):
                value=float(Fraction(str(field['base']))+(0 if field.get('constant') else row*field['width']+col)*Fraction(str(field.get('step',1))))
            line.append(repr(value) if present else 'NaN');mask.append(present)
        values.append(line);valid.append(mask)
    return values,valid


def expected(field,x=0,y=0,width=None,height=None):
    values,valid=source_values(field,x,y,width,height)
    if field.get('packing')=='complex-spatial':
        # Independent scalar IEEE32 arithmetic oracle, not product decoding.
        f32=lambda v:struct.unpack('>f',struct.pack('>f',v))[0]
        r=f32(field['reference']);b=f32(2.**field['binary_scale']);d=f32(10.**(-field['decimal_scale']))
        for row,line in enumerate(values):
            for col,value in enumerate(line):
                if valid[row][col]:
                    integer=(Fraction(value)*10**field['decimal_scale']-Fraction(r))/Fraction(2)**field['binary_scale']
                    assert integer==int(integer)
                    line[col]=repr(f32(f32(f32(f32(integer)*b)+r)*d))
    return values,valid


def precision_bound(field,values,valid,x=0,y=0):
    """Check normal-coefficient fixtures against the independent physical values."""
    source,source_valid=source_values(field,x,y,len(values[0]),len(values))
    assert source_valid==valid
    u=2.**-24;gamma=5*u/(1-5*u);maximum=0.;bound=0.
    for row,line in enumerate(values):
        for col,value in enumerate(line):
            if valid[row][col]:
                original=float(source[row][col]);r=field['reference'];b=2.**field['binary_scale'];d=10.**(-field['decimal_scale'])
                integer=(original/d-r)/b
                absolute_bound=gamma*(abs(integer*b)+abs(r))*abs(d)
                error=abs(float(value)-original)
                assert error<=absolute_bound
                maximum=max(maximum,error);bound=max(bound,absolute_bound)
    return dict(max_absolute_error=maximum,max_derived_bound=bound,precision='float32_scaling_float64_output')


def message(field):
    # WMO sections encoded independently of the product GDAL reader.
    def section(n,length):
        raw=bytearray(length);raw[:4]=struct.pack('>I',length);raw[4]=n;return raw
    def u32(raw,p,value):raw[p:p+4]=struct.pack('>I',value)
    def angle(value):return int(abs(value)*1_000_000)|(0x80000000 if value<0 else 0)
    width,height=field['width'],field['height'];scan=field['scan']
    values,valid=source_values(field);rows=range(height-1,-1,-1) if scan==64 else range(height)
    points=[(values[y][x],valid[y][x]) for y in rows for x in range(width)]
    s1=section(1,21);s1[5:7]=struct.pack('>H',7);s1[9]=2;s1[11]=1;s1[12:14]=struct.pack('>H',2026);s1[14:19]=bytes([1,2,3,0,0]);s1[20]=1
    s3=section(3,72);u32(s3,6,width*height);s3[14]=6;u32(s3,30,width);u32(s3,34,height)
    spacing=field.get('spacing',1)
    south=field['north']-(height-1)*spacing
    u32(s3,46,angle(south if scan==64 else field['north']));u32(s3,50,angle(field['west']));s3[54]=48
    u32(s3,55,angle(field['north'] if scan==64 else south));u32(s3,59,angle(field['west']+(width-1)*spacing));u32(s3,63,angle(spacing));u32(s3,67,angle(spacing));s3[71]=scan
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


def grib1_fields(reverse=False):
    result=[]
    for i,(base,step,constant,missing,level,level_raw,tri,p1,p2,reference,binary_scale) in enumerate([
        (280.,1.,False,True,100,[1,244],0,3,0,0.,0),
        (-1.25,.25,False,False,101,[50,100],4,3,6,-5.,-2),
        (280.,1.,True,False,105,[0,2],10,1,44,280.,0)]):
        result.append(dict(number=i+1,width=4-i,height=3 if i<2 else 4,packing='simple',scan=64 if reverse else 0,
            west=250 if i==1 else 110,north=30,forecast=300 if tri==10 else p2 if tri==4 else p1,
            base=base,step=step,constant=constant,missing=missing,level=level,level_raw=level_raw,
            time_indicator=tri,p1=p1,p2=p2,reference=reference,binary_scale=binary_scale,decimal_scale=0,bits=0 if constant else 16))
    return result


def grib1_message(field):
    # Independent GRIB1 encoding oracle, never used by the product reader.
    def signed(value,width):return (abs(value)|(1<<(8*width-1) if value<0 else 0)).to_bytes(width,'big')
    pds=bytearray(28);pds[:3]=(28).to_bytes(3,'big')
    pds[3:12]=bytes([2,7,81,255,128+(64 if field['missing'] else 0),11,field['level'],*field['level_raw']])
    pds[12:26]=bytes([26,1,2,3,0,1,field['p1'],field['p2'],field['time_indicator'],0,2 if field['time_indicator'] in (3,4,5) else 0,0,21,0])
    pds[26:28]=signed(field['decimal_scale'],2)
    width,height,scan=field['width'],field['height'],field['scan']
    south=field['north']-height+1
    grid=bytearray(32);grid[:3]=(32).to_bytes(3,'big');grid[4]=255;grid[6:8]=width.to_bytes(2,'big');grid[8:10]=height.to_bytes(2,'big')
    grid[10:13]=signed((south if scan==64 else field['north'])*1000,3);grid[13:16]=signed(field['west']*1000,3);grid[16]=128
    grid[17:20]=signed((field['north'] if scan==64 else south)*1000,3);grid[20:23]=signed((field['west']+width-1)*1000,3)
    grid[23:27]=bytes.fromhex('03e803e8');grid[27]=scan
    values,valid=source_values(field);rows=range(height-1,-1,-1) if scan==64 else range(height)
    points=[(values[y][x],valid[y][x]) for y in rows for x in range(width)]
    bitmap=b''
    if field['missing']:
        length=6+((width*height+15)//16)*2;bitmap=bytearray(length);bitmap[:3]=length.to_bytes(3,'big');bitmap[3]=(length-6)*8-width*height
        for i,(_,ok) in enumerate(points):
            if ok:bitmap[6+i//8]|=1<<(7-i%8)
    bitstring=''
    if field['bits']:
        for value,ok in points:
            if ok:
                integer=(float(value)*10**field['decimal_scale']-field['reference'])/2**field['binary_scale']
                assert integer==int(integer) and 0<=integer<2**field['bits']
                bitstring+=format(int(integer),'0'+str(field['bits'])+'b')
    unused=(-len(bitstring)-88)%16;bitstring+='0'*unused
    data=bytearray(11+len(bitstring)//8);data[:3]=len(data).to_bytes(3,'big');data[3]=unused;data[4:6]=signed(field['binary_scale'],2);data[10]=field['bits']
    if field['reference']:
        fraction=abs(field['reference']);exponent=0
        while fraction>=1:fraction/=16;exponent+=1
        while fraction<1/16:fraction*=16;exponent-=1
        data[6:10]=((int(field['reference']<0)<<31)|((exponent+64)<<24)|int(fraction*2**24)).to_bytes(4,'big')
    if bitstring:data[11:]=int(bitstring,2).to_bytes(len(bitstring)//8,'big')
    body=pds+grid+bitmap+data+b'7777'
    return b'GRIB'+(8+len(body)).to_bytes(3,'big')+b'\x01'+body


def complex_fields(reverse=False):
    return [dict(number=i+1,width=4 if i==0 else 3,height=3,packing='complex-spatial',scan=64 if reverse else 0,
        west=110 if i!=1 else 250,north=30,forecast=3 if i==0 else 6,start_hour=0 if i==2 else 3 if i==0 else 6,
        base=base,step=step,missing=i==0,reference=reference,binary_scale=scale,decimal_scale=2 if i==1 else 0,
        parameter=parameter,unit=unit,element=element,short_name=short,product_template=8 if i==2 else 0,
        height_m=2 if i==0 else 10 if i==1 else 0)
        for i,(base,step,reference,scale,parameter,unit,element,short) in enumerate([
            (280.,1.,0.,0,[0,0],'[K]','TMP','2-HTGL'),
            (-1.23,.01,-500.,0,[2,2],'[m/s]','UGRD','10-HTGL'),
            (0.,.25,0.,-2,[1,8],'[kg/(m^2)]','APCP06','0-SFC')])]


def complex_message(field):
    # Independent WMO5.3 encoder for the fixture; production never uses it.
    base=dict(field,packing='ieee64');raw=message(base);sections={};pos=16
    while pos<len(raw)-4:
        size=int.from_bytes(raw[pos:pos+4],'big');sections[raw[pos+4]]=bytearray(raw[pos:pos+size]);pos+=size
    values,valid=source_values(field);rows=range(field['height']-1,-1,-1) if field['scan']==64 else range(field['height'])
    selected=[float(values[y][x]) for y in rows for x in range(field['width']) if valid[y][x]]
    numbers=[int((Fraction(str(v))*10**field['decimal_scale']-Fraction(field['reference']))/Fraction(2)**field['binary_scale']) for v in selected]
    assert all(float((Fraction(field['reference'])+n*Fraction(2)**field['binary_scale'])/10**field['decimal_scale'])==v for n,v in zip(numbers,selected))
    differences=[numbers[i]-2*numbers[i-1]+numbers[i-2] for i in range(2,len(numbers))];minimum=min(differences)
    residuals=[0,0]+[v-minimum for v in differences]
    groups=[residuals[i:i+4] for i in range(0,len(residuals),4)]
    references=[min(g) for g in groups];widths=[(max(g)-min(g)).bit_length() for g in groups];lengths=[len(g) for g in groups]
    ref_bits=max(references).bit_length();width_bits=max(widths).bit_length();length_bits=(max(lengths)-1).bit_length()
    def packed(values,bits):
        text=''.join(format(v,'0'+str(bits)+'b') for v in values) if bits else ''
        text+='0'*(-len(text)%8)
        return int(text,2).to_bytes(len(text)//8,'big') if text else b''
    def signed(value,width):return (abs(value)|(1<<(width*8-1) if value<0 else 0)).to_bytes(width,'big')
    extra=field.get('extra_octets',2)
    payload=b''.join(v.to_bytes(extra,'big') for v in numbers[:2])+signed(minimum,extra)
    payload+=packed(references,ref_bits)+packed(widths,width_bits)+packed([v-1 for v in lengths],length_bits)
    bits=''.join(format(v-r,'0'+str(w)+'b') for g,r,w in zip(groups,references,widths) if w for v in g)
    bits+='0'*(-len(bits)%8)
    if bits:payload+=int(bits,2).to_bytes(len(bits)//8,'big')
    s5=bytearray(49);s5[:5]=(49).to_bytes(4,'big')+b'\x05';s5[5:9]=len(numbers).to_bytes(4,'big');s5[9:11]=(3).to_bytes(2,'big')
    s5[11:15]=struct.pack('>f',field['reference']);s5[15:17]=signed(field['binary_scale'],2);s5[17:19]=signed(field['decimal_scale'],2);s5[19]=ref_bits
    s5[21]=1;s5[23:31]=b'\xff'*8;s5[31:35]=len(groups).to_bytes(4,'big');s5[36]=width_bits;s5[37:41]=(1).to_bytes(4,'big');s5[41]=1;s5[42:46]=lengths[-1].to_bytes(4,'big');s5[46:49]=bytes([length_bits,2,extra])
    s4=sections[4];s4[9:11]=bytes(field['parameter']);s4[22]=1 if field['height_m']==0 else 103;s4[24:28]=field['height_m'].to_bytes(4,'big');s4[18:22]=field['start_hour'].to_bytes(4,'big')
    if field['product_template']==8:
        s4+=bytearray(24);s4[:4]=(58).to_bytes(4,'big');s4[7:9]=(8).to_bytes(2,'big');s4[34:41]=bytes([7,234,1,2,3+field['forecast'],0,0]);s4[41]=1
        s4[46:58]=bytes([1,2,1])+(field['forecast']-field['start_hour']).to_bytes(4,'big')+bytes([255])+bytes(4)
    sections[4]=s4;sections[5]=s5;sections[7]=bytearray((5+len(payload)).to_bytes(4,'big')+b'\x07'+payload)
    body=b''.join(sections[n] for n in (1,3,4,5,6,7))+b'7777'
    return raw[:8]+(16+len(body)).to_bytes(8,'big')+body


def ccsds_fields():
    return [
        dict(number=index, width=8, height=4, packing='ccsds', scan=0,
             west=110, north=30, forecast=3 * index, base=base, step=0.25,
             missing=False, parameter=parameter, unit=unit, element=element,
             short_name=f'{height}-HTGL', height_m=height)
        for index, (base, parameter, unit, element, height) in enumerate((
            (270., [0, 0], '[K]', 'TMP', 2),
            (-4., [2, 2], '[m/s]', 'UGRD', 10),
            (-6., [2, 3], '[m/s]', 'VGRD', 10),
        ), 1)
    ]


def ccsds_precipitation_fields():
    return [dict(number=index, width=8, height=4, packing='ccsds', scan=0,
                 west=110, north=30, spacing=0.25, forecast=hours, start_hour=0,
                 base=0., step=2.**scale, binary_scale=scale, missing=False,
                 product_template=8, parameter=[1, 193], unit='[m]', element='tp', short_name='0-SFC')
            for index, (hours, scale) in enumerate(((6, -14), (150, -12), (360, -11)), 1)]


def ccsds_message(field):
    # Frozen libaec encoding of unsigned integers 0..31, one block of 32.
    # Source declarations and expected physical values are independent of GDAL.
    cumulative = field.get('product_template') == 8
    initial_zero = cumulative and field['forecast'] == 0
    payload = bytes.fromhex('1000492492492492492492492490' if cumulative else '20092492492492492492492492')
    if initial_zero:
        payload = b''
    raw = message(dict(field, packing='ieee64'))
    sections = {}
    offset = 16
    while offset < len(raw) - 4:
        size = int.from_bytes(raw[offset:offset+4], 'big')
        sections[raw[offset+4]] = bytearray(raw[offset:offset+size])
        offset += size
    sections[1][5:11] = bytes.fromhex('006200002000')
    sections[2] = bytes.fromhex('0000001102000100010009040130303031')
    sections[4][9:11] = bytes(field['parameter'])
    sections[4][12:14] = bytes.fromhex('ffa1')
    if cumulative:
        sections[1][10] = 1
        product = sections[4]
        product[7:9] = b'\x00\x08'
        product[18:22] = b'\x00' * 4
        product[22:34] = b'\x01' + b'\xff' * 11
        end = datetime(2026, 1, 2, 3) + timedelta(hours=field['forecast'])
        product.extend(struct.pack('>H5B', end.year, end.month, end.day, end.hour, end.minute, end.second))
        product.extend(bytes.fromhex('0100000000010201') + struct.pack('>IBI', field['forecast'], 13, 450))
        product[:4] = struct.pack('>I', len(product))
    else:
        sections[4][24:28] = field['height_m'].to_bytes(4, 'big')
    scale = field.get('binary_scale', -2)
    encoded_scale = 0x8000 | -scale if scale < 0 else scale
    bits = 0 if initial_zero else 12 if cumulative else 8
    sections[5] = (struct.pack('>IBIHf', 25, 5, 32, 42, field['base'])
                   + struct.pack('>HHB', encoded_scale, 0, bits)
                   + bytes.fromhex('000e200080'))
    sections[7] = struct.pack('>IB', 5 + len(payload), 7) + payload
    body = b''.join(sections[n] for n in range(1, 8)) + b'7777'
    return b'GRIB\xff\xff\x00\x02' + struct.pack('>Q', 16 + len(body)) + body


def source_bytes(spec):
    if spec.get('edition') == 1:
        encode = grib1_message
    else:
        encode = {'complex-spatial': complex_message, 'ccsds': ccsds_message}.get(spec.get('packing'), message)
    return b''.join(encode(field) for field in spec['fields'])


def sources():
    result=[]
    for reverse,name in enumerate(NAMES[:2]):
        spec=dict(name=name,case_id='reverse' if reverse else 'forward',fields=fields(bool(reverse)))
        raw=source_bytes(spec);spec.update(size_bytes=len(raw),sha256=hashlib.sha256(raw).hexdigest());result.append(spec)
    for reverse,name in enumerate(NAMES[2:4]):
        spec=dict(name=name,case_id='grib1-reverse' if reverse else 'grib1-forward',edition=1,fields=grib1_fields(bool(reverse)))
        raw=source_bytes(spec);spec.update(size_bytes=len(raw),sha256=hashlib.sha256(raw).hexdigest());result.append(spec)
    for reverse,name in enumerate(NAMES[4:6]):
        spec=dict(name=name,case_id='complex-reverse' if reverse else 'complex-forward',packing='complex-spatial',fields=complex_fields(bool(reverse)))
        raw=source_bytes(spec);spec.update(size_bytes=len(raw),sha256=hashlib.sha256(raw).hexdigest());result.append(spec)
    spec = dict(name=NAMES[6], case_id='ecmwf-ccsds', packing='ccsds', fields=ccsds_fields())
    raw = source_bytes(spec)
    result.append(dict(spec, size_bytes=len(raw), sha256=hashlib.sha256(raw).hexdigest()))
    spec = dict(name=NAMES[7], case_id='ecmwf-precipitation', packing='ccsds', fields=ccsds_precipitation_fields())
    raw = source_bytes(spec)
    result.append(dict(spec, size_bytes=len(raw), sha256=hashlib.sha256(raw).hexdigest()))
    zero = dict(ccsds_precipitation_fields()[0], forecast=0, constant=True, binary_scale=0, step=0.)
    spec = dict(name=NAMES[8], case_id='ecmwf-precipitation-0h', packing='ccsds', fields=[zero])
    raw = source_bytes(spec)
    result.append(dict(spec, size_bytes=len(raw), sha256=hashlib.sha256(raw).hexdigest()))
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
