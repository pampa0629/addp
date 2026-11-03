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
