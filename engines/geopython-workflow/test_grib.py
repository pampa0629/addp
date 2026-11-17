"""Independent WMO section fixtures, through the product's only GDAL reader."""
import hashlib
import json
import struct
from pathlib import Path
import numpy as np
import pytest
from operators import grib


def encoded_message(values, *, scanning=0, bitmap=None, packing='ieee64', west=110, north=30, forecast=3):
    values=np.asarray(values,dtype='float64');height,width=values.shape
    valid=np.ones(values.shape,dtype=bool) if bitmap is None else np.asarray(bitmap,dtype=bool)
    file_values=values[::-1] if scanning==64 else values
    file_valid=valid[::-1] if scanning==64 else valid
    selected=file_values[file_valid]
    def section(number,length):
        raw=bytearray(length);raw[:4]=length.to_bytes(4,'big');raw[4]=number;return raw
    def u32(raw,offset,value):raw[offset:offset+4]=int(value).to_bytes(4,'big')
    def angle(value):return (int(abs(value)*1000000) | (0x80000000 if value<0 else 0))
    s1=section(1,21);s1[5:7]=(7).to_bytes(2,'big');s1[9]=2;s1[11]=1;s1[12:14]=(2026).to_bytes(2,'big');s1[14:19]=bytes([1,2,3,0,0]);s1[19]=0;s1[20]=1
    s3=section(3,72);u32(s3,6,width*height);s3[14]=6;u32(s3,30,width);u32(s3,34,height)
    south=north-height+1;u32(s3,46,angle(south if scanning==64 else north));u32(s3,50,angle(west));s3[54]=48
    u32(s3,55,angle(north if scanning==64 else south));u32(s3,59,angle(west+width-1));u32(s3,63,1000000);u32(s3,67,1000000);s3[71]=scanning
    s4=section(4,34);s4[11]=2;s4[17]=1;u32(s4,18,forecast);s4[22]=103;u32(s4,24,2);s4[28:34]=b'\xff'*6
    s5=section(5,12 if packing.startswith('ieee') else 21);u32(s5,5,len(selected))
    if packing.startswith('ieee'):
        s5[9:11]=(4).to_bytes(2,'big');s5[11]=2 if packing=='ieee64' else 1
        payload=selected.astype('>f8' if packing=='ieee64' else '>f4').tobytes()
    else:
        # Simple packing reference0/scales0, each selected integer is one byte.
        assert np.all(selected==selected.astype('uint8'));s5[19]=8
        payload=selected.astype('uint8').tobytes()
    s6=section(6,6 if bitmap is None else 6+(width*height+7)//8);s6[5]=255 if bitmap is None else 0
    if bitmap is not None:s6[6:]=np.packbits(file_valid.reshape(-1)).tobytes()
    s7=section(7,5+len(payload));s7[5:]=payload
    body=s1+s3+s4+s5+s6+s7+b'7777';header=bytearray(b'GRIB\0\0\0\x02'+(16+len(body)).to_bytes(8,'big'))
    return bytes(header+body)


def source(tmp_path,raw):
    target=tmp_path/'source.grb2';target.write_bytes(raw)
    return {'schema_version':'addp.workflow.access-plan/v1','source':{'kind':'file','format':'grib','access':{'method':'mounted_path','path':str(target)}}}


def window(name='message-1',width=4,height=3,**changes):
    return dict(variable=name,indices=[],x=0,y=0,width=width,height=height,**changes)


@pytest.mark.parametrize('scanning',[0,64])
@pytest.mark.parametrize('packing',['ieee32','ieee64','simple'])
def test_grib_independent_values_bitmap_units_and_declared_grid(tmp_path,scanning,packing):
    expected=np.arange(12,dtype='float64').reshape(3,4)+20
    # A genuine9999 must survive the driver's own missing-value sentinel.
    if packing!='simple':expected[0,0]=9999.;expected[0,1]=280.25
    valid=np.ones((3,4),dtype=bool);valid[1,2]=False
    raw=encoded_message(expected,scanning=scanning,packing=packing,bitmap=valid)
    plan=source(tmp_path,raw);description=grib.inspect(plan)
    assert description['format_info']['scan_complete'] is True
    child=description['container']['children'][0]['native']
    assert child['attributes']['GRIB_UNIT']['values']==['[K]']
    assert child['attributes']['GRIB_ELEMENT']['values']==['TMP']
    assert child['attributes']['GRIB_FORECAST_SECONDS']['values']==['10800']
    assert child['grid']['transform']==[109.5,1.0,0.0,30.5,0.0,-1.0]
    result=grib.read_slice(plan,description['format_info']['source_version'],window())
    assert result['value_mode']=='decoded_native_units' and result['spatial'] is None
    assert result['valid']==valid.tolist()
    actual=np.array(result['values'],dtype='float64')
    np.testing.assert_array_equal(actual[valid],expected[valid]);assert np.isnan(actual[1,2])
    assert (tmp_path/'source.grb2').read_bytes()==raw
    assert sorted(p.name for p in tmp_path.iterdir())==['source.grb2']


def test_grib_multiple_independent_grids_and_duplicate_elements(tmp_path):
    a=np.arange(12).reshape(3,4)+280;b=np.arange(6).reshape(2,3)+290
    raw=encoded_message(a)+encoded_message(b,west=250,north=-10,forecast=6)
    plan=source(tmp_path,raw);description=grib.inspect(plan);children=description['container']['children']
    assert [c['name'] for c in children]==['message-1','message-2']
    assert [c['native']['dimensions'][-1]['size'] for c in children]==['4','3']
    assert all(c['native']['attributes']['GRIB_ELEMENT']['values']==['TMP'] for c in children)
    result=grib.read_slice(plan,description['format_info']['source_version'],window('message-2',width=3,height=2))
    np.testing.assert_array_equal(np.array(result['values'],dtype='float64'),b)
    assert result['variable']['grid']['transform'][0]==249.5
    # A malicious sidecar cannot reduce enumeration or change metadata.
    (tmp_path/'source.grb2.idx').write_text('1:0:d=20200101:wrong:surface:anl:')
    assert grib.inspect(plan)==description


def test_grib_rejects_changes_and_invalid_selection(tmp_path):
    raw=encoded_message(np.arange(12).reshape(3,4));plan=source(tmp_path,raw);version=grib.inspect(plan)['format_info']['source_version']
    for selection in [window('message-01'),dict(window(),indices=[0]),dict(window(),x=-1),dict(window(),x=True),dict(window(),width=129),dict(window(),height=128,width=128),dict(window(),x=1),dict(window(),other=1)]:
        with pytest.raises(grib.SliceSelectionError):grib.read_slice(plan,version,selection)
    (tmp_path/'source.grb2').write_bytes(encoded_message(np.arange(12).reshape(3,4)+10))
    with pytest.raises(grib.SourceChanged):grib.read_slice(plan,version,window())


def sections(raw):
    result={};p=16
    while p<len(raw)-4:
        n=int.from_bytes(raw[p:p+4],'big');result[raw[p+4]]=(p,n);p+=n
    return result


@pytest.mark.parametrize('fault',['edition1','truncated','trailing','prefix','bad_size','duplicate_section','local_section','grid_template','scanning','grid_count','bitmap_reference','bitmap_padding','bitmap_count','data_template','packed_count','product_template'])
def test_grib_rejects_incomplete_or_unproved_content_before_gdal(tmp_path,monkeypatch,fault):
    raw=bytearray(encoded_message(np.arange(6).reshape(2,3),bitmap=[[1,0,1],[1,1,1]]));locations=sections(raw)
    if fault=='edition1':raw[7]=1
    elif fault=='truncated':raw=raw[:-2]
    elif fault=='trailing':raw+=b'unknown'
    elif fault=='prefix':raw=b'prefix'+raw
    elif fault=='bad_size':raw[8:16]=(999999).to_bytes(8,'big')
    elif fault in ('duplicate_section','local_section'):raw[locations[3][0]+4]=1 if fault=='duplicate_section' else 2
    elif fault=='grid_template':raw[locations[3][0]+13]=1
    elif fault=='scanning':raw[locations[3][0]+71]=16
    elif fault=='grid_count':raw[locations[3][0]+9]=99
    elif fault=='bitmap_reference':raw[locations[6][0]+5]=254
    elif fault=='bitmap_padding':raw[locations[6][0]+6]|=1
    elif fault=='bitmap_count':raw[locations[6][0]+6]=0
    elif fault=='data_template':raw[locations[5][0]+10]=2
    elif fault=='packed_count':raw[locations[5][0]+8]=99
    elif fault=='product_template':raw[locations[4][0]+8]=99
    def no_decoder(*a,**k):pytest.fail('unsupported content reached GDAL')
    monkeypatch.setattr(grib.gdal,'OpenEx',no_decoder)
    with pytest.raises(ValueError,match='GRIB'):grib.inspect(source(tmp_path,bytes(raw)))


def test_grib_runtime_declares_only_bounded_reads():
    assert set(grib.OPERATORS)=={'grib.inspect','grib.read_slice'}
    for operator in grib.OPERATORS.values():
        assert operator is not None


def test_grib_statistical_product_time_range_and_full_values(tmp_path):
    values=np.arange(12).reshape(3,4)+280
    raw=encoded_message(values);location=sections(raw)[4];start,length=location
    product=bytearray(raw[start:start+length]+b'\0'*24)
    product[:4]=(58).to_bytes(4,'big');product[7:9]=(8).to_bytes(2,'big')
    product[34:36]=(2026).to_bytes(2,'big');product[36:41]=bytes([1,2,9,0,0]);product[41]=1
    product[46:49]=bytes([0,2,1]);product[49:53]=(3).to_bytes(4,'big');product[53]=255
    raw=bytearray(raw[:start]+product+raw[start+length:]);raw[8:16]=len(raw).to_bytes(8,'big')
    plan=source(tmp_path,bytes(raw));info=grib.inspect(plan)
    result=grib.read_slice(plan,info['format_info']['source_version'],window())
    assert result['variable']['message']['product_template']==8
    assert int(result['variable']['attributes']['GRIB_VALID_TIME']['values'][0])-int(result['variable']['attributes']['GRIB_REF_TIME']['values'][0])==6*3600
    np.testing.assert_array_equal(np.asarray(result['values'],dtype='float64'),values)


# The Hosted oracle encodes GRIB1 independently; product code never packs or
# decodes the measurements, and these tests traverse the real GDAL reader.
import copy
import importlib.util
_fixture_spec=importlib.util.spec_from_file_location('grib1_oracle',Path(__file__).resolve().parents[2]/'business/scripts/online-grib-fixture.py')
ORACLE=importlib.util.module_from_spec(_fixture_spec);_fixture_spec.loader.exec_module(ORACLE)


def grib1_source(tmp_path, **changes):
    field=ORACLE.grib1_fields()[0]
    field.update(changes)
    return source(tmp_path,ORACLE.grib1_message(field)),field


@pytest.mark.parametrize('level,raw,unit,values',[
    (1,[0,0],'none',{}),(100,[1,244],'hPa',{'value':500}),
    (101,[50,100],'kPa',{'upper':50,'lower':100}),(102,[0,0],'none',{}),
    (103,[0,20],'m',{'value':20}),(105,[0,2],'m',{'value':2}),
    (111,[0,100],'cm',{'value':100}),(112,[10,30],'cm',{'upper':10,'lower':30})])
def test_grib1_level_units_and_independent_bounds(tmp_path,level,raw,unit,values):
    plan,field=grib1_source(tmp_path,level=level,level_raw=raw)
    info=grib.inspect(plan);native=info['container']['children'][0]['native']
    assert info['format_info']['encoding']=='GRIB1' and info['format_info']['profile']=='grib1_regular_fields/v1'
    actual=native['message']['level']
    assert actual['type_code']==level and actual['raw_octets']==raw and actual['unit']==unit
    assert {k:actual[k] for k in values}==values
    assert json.loads(native['attributes']['GRIB1_LEVEL']['values'][0])==actual


@pytest.mark.parametrize('unit,seconds',[(0,60),(1,3600),(2,86400),(10,10800),(11,21600),(12,43200),(254,1)])
@pytest.mark.parametrize('indicator',[0,1,2,3,4,5,10])
def test_grib1_time_is_bound_to_original_interval_and_statistic(tmp_path,unit,seconds,indicator):
    field=ORACLE.grib1_fields()[0];field.update(time_indicator=indicator,p1=0 if indicator==1 else 3,p2=6 if indicator in (2,3,4,5) else 0)
    raw=bytearray(ORACLE.grib1_message(field));raw[8+17]=unit
    plan=source(tmp_path,raw);info=grib.inspect(plan);native=info['container']['children'][0]['native'];time=native['message']['time']
    assert time['unit_code']==unit and time['range_indicator']==indicator
    assert time['p1']==field['p1'] and time['p2']==field['p2']
    assert time['operation']=={0:'instant',1:'initialized_analysis',2:'interval',3:'average',4:'accumulation',5:'difference',10:'instant'}[indicator]
    expected=(768 if indicator==10 else 6 if indicator in (2,3,4,5) else 0 if indicator==1 else 3)*seconds
    assert native['attributes']['GRIB_FORECAST_SECONDS']['values']==[str(expected)]
    assert time['reference_utc']=='2026-01-02T03:00:00+00:00'
    assert json.loads(native['attributes']['GRIB1_TIME']['values'][0])==time


@pytest.mark.parametrize('number,identity',sorted(grib.GRIB1_PARAMETERS.items()))
@pytest.mark.parametrize('table',[2,3])
def test_grib1_standard_parameter_name_and_native_unit(tmp_path,number,identity,table):
    field=ORACLE.grib1_fields()[0];raw=bytearray(ORACLE.grib1_message(field));raw[11]=table;raw[16]=number
    plan=source(tmp_path,raw);info=grib.inspect(plan);native=info['container']['children'][0]['native'];attrs=native['attributes']
    assert (attrs['GRIB_ELEMENT']['values'][0],attrs['GRIB_UNIT']['values'][0])==identity
    assert native['message']['parameter']==dict(centre=7,subcentre=0,process=81,table_version=table,number=number)
    result=grib.read_slice(plan,info['format_info']['source_version'],window())
    assert (result['values'],result['valid'])==ORACLE.expected(field)


@pytest.mark.parametrize('year,century,expected',[(100,20,'2000'),(1,21,'2001')])
def test_grib1_century_boundary(tmp_path,year,century,expected):
    plan,field=grib1_source(tmp_path);path=Path(plan['source']['access']['path']);raw=bytearray(path.read_bytes());raw[20]=year;raw[32]=century;path.write_bytes(raw)
    assert grib.inspect(plan)['container']['children'][0]['native']['message']['time']['reference_utc'].startswith(expected)


@pytest.mark.parametrize('scan',[0,64])
def test_grib1_all_missing_constant_and_decimal_scaling(tmp_path,scan):
    field=ORACLE.grib1_fields()[1];field.update(scan=scan,base=2.,step=.5,reference=-20.,binary_scale=-1,decimal_scale=1)
    raw=ORACLE.grib1_message(field);plan=source(tmp_path,raw);info=grib.inspect(plan)
    result=grib.read_slice(plan,info['format_info']['source_version'],window(width=3,height=3))
    assert (result['values'],result['valid'])==ORACLE.expected(field)
    field=ORACLE.grib1_fields()[2];field.update(scan=scan);plan=source(tmp_path,ORACLE.grib1_message(field));info=grib.inspect(plan)
    result=grib.read_slice(plan,info['format_info']['source_version'],window(width=2,height=4))
    assert result['values']==[['280.0']*2]*4
    field=ORACLE.grib1_fields()[0];raw=bytearray(ORACLE.grib1_message(field))
    # Explicit all-zero bitmap and no packed values, with even-section padding.
    bms_at=8+28+32;raw[bms_at+6:bms_at+8]=bytes(2);bds_at=bms_at+8
    data=bytearray(raw[bds_at:bds_at+12]);data[:3]=(12).to_bytes(3,'big');data[3]=8;data[11]=0
    raw=raw[:bds_at]+data+b'7777';raw[4:7]=len(raw).to_bytes(3,'big')
    plan=source(tmp_path,raw);info=grib.inspect(plan);result=grib.read_slice(plan,info['format_info']['source_version'],window())
    assert result['valid']==[[False]*4]*3 and result['values']==[['NaN']*4]*3


@pytest.mark.parametrize('fault',['local_table','unknown_parameter','private_pds','no_grid','grid_kind','reduced_grid','vertical_coordinates','grid_reserved','scan','endpoint','bitmap_reference','bitmap_unused','bitmap_padding','complex','spectral','extended_flags','bits','packed_count','packed_padding','time_month','time_quarter','time_half','time_unknown','time_interval','time_count','year_zero','century_zero','level','layer_inverted','surface_value','trailing','prefix','truncated','mixed'])
def test_grib1_rejects_unproved_semantics_before_decoder(tmp_path,monkeypatch,fault):
    field=ORACLE.grib1_fields()[0];raw=bytearray(ORACLE.grib1_message(field));p,g,b,d=8,36,68,76
    patches={'local_table':(p+3,128),'unknown_parameter':(p+8,255),'no_grid':(p+7,64),
        'grid_kind':(g+5,4),'reduced_grid':(g+6,255),'vertical_coordinates':(g+3,1),'grid_reserved':(g+31,1),
        'scan':(g+27,128),'endpoint':(g+22,1),'bitmap_reference':(b+5,1),'bitmap_unused':(b+3,0),'bitmap_padding':(b+7,1),
        'complex':(d+3,64),'spectral':(d+3,128),'extended_flags':(d+3,16),'bits':(d+10,33),'packed_count':(d+10,15),'packed_padding':(len(raw)-5,1),
        'time_month':(p+17,3),'time_quarter':(p+17,13),'time_half':(p+17,14),'time_unknown':(p+20,51),'time_interval':(p+20,4),'time_count':(p+23,1),
        'year_zero':(p+12,0),'century_zero':(p+24,0),'level':(p+9,109),'layer_inverted':(p+10,255),'surface_value':(p+9,1)}
    if fault in patches:
        position,value=patches[fault];raw[position]=value
        if fault=='layer_inverted':raw[p+9]=101
    elif fault=='private_pds':raw[p:p+3]=(30).to_bytes(3,'big');raw=raw[:g]+bytes(2)+raw[g:];raw[4:7]=len(raw).to_bytes(3,'big')
    elif fault=='trailing':raw+=b'x'
    elif fault=='prefix':raw=b'x'+raw
    elif fault=='truncated':raw=raw[:-1]
    elif fault=='mixed':raw+=encoded_message(np.arange(12).reshape(3,4))
    def reject_decoder(*args,**kwargs):pytest.fail('unproved GRIB1 reached GDAL')
    monkeypatch.setattr(grib.gdal,'OpenEx',reject_decoder)
    with pytest.raises(ValueError):grib.inspect(source(tmp_path,raw))


@pytest.mark.parametrize('reserved_size',[28,40])
def test_grib1_reserved_product_bytes_and_sidecars(tmp_path,reserved_size):
    plan,field=grib1_source(tmp_path);path=Path(plan['source']['access']['path']);raw=bytearray(path.read_bytes())
    if reserved_size==40:
        raw[8:11]=(40).to_bytes(3,'big');raw=raw[:36]+bytes(12)+raw[36:];raw[4:7]=len(raw).to_bytes(3,'big');path.write_bytes(raw)
    description=grib.inspect(plan)
    path.with_suffix(path.suffix+'.idx').write_text('untrusted index')
    assert grib.inspect(plan)==description
    result=grib.read_slice(plan,description['format_info']['source_version'],window())
    assert (result['values'],result['valid'])==ORACLE.expected(field)
    assert path.read_bytes()==raw
    if reserved_size==40:
        raw[36]=1;path.write_bytes(raw)
        with pytest.raises(ValueError):grib.inspect(plan)


def test_grib1_decoder_time_and_parameter_must_agree_with_original(tmp_path,monkeypatch):
    plan,_=grib1_source(tmp_path)
    with grib._source(plan) as (raw,fields,version):
        field=fields[0]
        with grib._dataset(raw,field) as dataset:
            band=dataset.GetRasterBand(1);original=band.GetMetadata()
            for key,value in [('GRIB_VALID_TIME','0'),('GRIB_UNIT','[C]'),('GRIB_ELEMENT','fallback')]:
                band.SetMetadata(dict(original,**{key:value}))
                with pytest.raises(ValueError):grib._description(dataset,field)
            band.SetMetadata(original)


@pytest.mark.parametrize('scan',[0,64])
def test_grib1_subdegree_grid_declared_increments(tmp_path,scan):
    field=ORACLE.grib1_fields()[0];field['scan']=scan;raw=bytearray(ORACLE.grib1_message(field));g=36
    lat1,lat2=(29500,30000) if scan==64 else (30000,29500)
    raw[g+10:g+13]=lat1.to_bytes(3,'big');raw[g+17:g+20]=lat2.to_bytes(3,'big')
    raw[g+20:g+23]=(110375).to_bytes(3,'big');raw[g+23:g+25]=(125).to_bytes(2,'big');raw[g+25:g+27]=(250).to_bytes(2,'big')
    plan=source(tmp_path,raw);native=grib.inspect(plan)['container']['children'][0]['native']
    assert native['grid']['transform']==[109.9375,.125,0.,30.125,0.,-.25]
