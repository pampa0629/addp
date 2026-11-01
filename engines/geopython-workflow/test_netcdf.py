"""Independent classic-CDF bytes, not a GDAL round-trip fixture."""
import hashlib
from pathlib import Path
import struct
from unittest.mock import patch

import numpy as np
import pytest

from operators import netcdf
from test_raster_compute import source_plan


def classic_dataset(path, dimensions, variables, version=1, global_attributes=()):
    wide = version == 5
    number = lambda n: struct.pack('>Q' if wide else '>I', n)
    word = lambda n: struct.pack('>I', n)
    types = {1:'b',3:'h',4:'i',5:'f',6:'d',7:'B',8:'H',9:'I',10:'q',11:'Q'}
    def text(s):
        raw=s.encode();return number(len(raw))+raw+b'\0'*((-len(raw))%4)
    def attrs(items):
        if not items:return word(0)+number(0)
        out=word(12)+number(len(items))
        for name,kind,value in items:
            raw=value.encode() if kind==2 else struct.pack('>'+types[kind]*len(value),*value)
            count=len(raw) if kind==2 else len(value)
            out+=text(name)+word(kind)+number(count)+raw+b'\0'*((-len(raw))%4)
        return out
    data=[]
    for variable in variables:
        raw=struct.pack('>'+types[variable['type']]*len(variable['values']),*variable['values'])
        data.append(raw+b'\0'*((-len(raw))%4))
    def header(begins):
        out=(b'CDF'+bytes([version])+number(0)+word(10)+number(len(dimensions))
             +b''.join(text(name)+number(size) for name,size in dimensions)+attrs(global_attributes)+word(11)+number(len(variables)))
        for v,raw,begin in zip(variables,data,begins):
            out+=(text(v['name'])+number(len(v['dims']))+b''.join(number(i) for i in v['dims'])+attrs(v.get('attrs',()))
                +word(v['type'])+number(len(raw))+(struct.pack('>Q',begin) if version in (2,5) else word(begin)))
        return out
    initial=header([0]*len(variables));begins=[];position=len(initial)
    for raw in data:begins.append(position);position+=len(raw)
    path.write_bytes(header(begins)+b''.join(data))


def classic_fixture(path, version=1, values=None):
    values=list(range(24)) if values is None else list(values);values[6]=-9999
    kind=10 if version==5 else 4
    classic_dataset(path,[('time',2),('y',3),('x',4)],
        [dict(name='temperature',type=kind,dims=[0,1,2],values=values,attrs=[('_FillValue',kind,[-9999]),('units',2,'K')])],
        version,[('title',2,'synthetic classic grid')])
    return np.array(values).reshape(2,3,4)


@pytest.mark.parametrize('version',[1,2,5])
def test_classic_metadata_and_exact_slice(tmp_path,version):
    path=tmp_path/'input.nc'
    expected=classic_fixture(path,version, [9007199254740993+i for i in range(24)] if version==5 else None)
    original=path.read_bytes()
    with patch('operators.netcdf.tempfile.TemporaryDirectory', wraps=netcdf.tempfile.TemporaryDirectory) as temporary:
        report=netcdf.inspect(source_plan(path,'netcdf'))
        assert report['format_info']['source_version']=='sha256:'+hashlib.sha256(original).hexdigest()
        assert report['format_info']['encoding']==f'CDF-{version}'
        assert report['container']['child_count']==1
        variable=report['container']['children'][0]['native']
        assert variable['name']=='/temperature' and variable['slice_supported'] is True
        assert variable['type']==('Int64' if version==5 else 'Int32')
        assert [(d['name'],d['size']) for d in variable['dimensions']]==[('/time','2'),('/y','3'),('/x','4')]
        selection={'variable':'/temperature','indices':[0],'x':1,'y':1,'width':3,'height':2}
        result=netcdf.read_slice(source_plan(path,'netcdf'),report['format_info']['source_version'],selection)
        assert result['values']==[[str(int(v)) for v in row] for row in expected[0,1:,1:]]
        assert result['valid']==[[True,False,True],[True,True,True]]
        assert result['spatial'] is None and result['value_mode']=='raw_storage'
        selection['indices']=[1]
        result2=netcdf.read_slice(source_plan(path,'netcdf'),report['format_info']['source_version'],selection)
        assert result2['values']==[[str(int(v)) for v in row] for row in expected[1,1:,1:]]
        assert result2['valid']==[[True]*3]*2
    assert path.read_bytes()==original


@pytest.mark.parametrize('change',[
 {'variable':'NETCDF:/etc/passwd:secret'}, {'indices':[]}, {'indices':[2]}, {'indices':[False]},
 {'x':-1}, {'x':4}, {'y':3}, {'width':0}, {'width':129}, {'height':3}, {'extra':1},
])
def test_classic_rejects_invalid_selection(tmp_path,change):
    path=tmp_path/'input.nc';classic_fixture(path);plan=source_plan(path,'netcdf');version=netcdf.inspect(plan)['format_info']['source_version']
    selection={'variable':'/temperature','indices':[0],'x':1,'y':1,'width':3,'height':2};selection.update(change)
    with pytest.raises(ValueError):netcdf.read_slice(plan,version,selection)


def test_classic_source_drift_and_no_sidecar_authority(tmp_path):
    path=tmp_path/'input.nc';classic_fixture(path);plan=source_plan(path,'netcdf');version=netcdf.inspect(plan)['format_info']['source_version']
    path.with_suffix('.nc.aux.xml').write_text('<PAMDataset><SRS>EPSG:4326</SRS></PAMDataset>')
    selection={'variable':'/temperature','indices':[0],'x':0,'y':0,'width':4,'height':3}
    assert netcdf.read_slice(plan,version,selection)['spatial'] is None
    classic_fixture(path,values=list(range(100,124)))
    with pytest.raises(netcdf.SourceChanged):netcdf.read_slice(plan,version,selection)


@pytest.mark.parametrize('raw',[b'\x89HDF\r\n\x1a\n',b'CDF\x03',b'CDF\x01',b'not netcdf'])
def test_non_classic_and_truncation_rejected_before_native(tmp_path,raw):
    path=tmp_path/'invalid.nc';path.write_bytes(raw)
    with pytest.raises((ValueError,RuntimeError)):netcdf.inspect(source_plan(path,'netcdf'))


def test_snapshot_byte_budget_and_cleanup(tmp_path,monkeypatch):
    path=tmp_path/'input.nc';classic_fixture(path);before=path.read_bytes();monkeypatch.setattr(netcdf,'MAX_BYTES',len(before)-1)
    with pytest.raises(ValueError,match='budget'):netcdf.inspect(source_plan(path,'netcdf'))
    assert path.read_bytes()==before


def test_explicit_crs_regular_axes_and_nonlinear_unknown(tmp_path):
    from osgeo import osr
    reference=osr.SpatialReference();reference.ImportFromEPSG(32650)
    variables=[
      dict(name='temperature',type=4,dims=[0,1],values=list(range(12)),attrs=[('grid_mapping',2,'crs'),('scale_factor',6,[.5]),('add_offset',6,[273.15])]),
      dict(name='y',type=6,dims=[0],values=[3000000.,3000010.,3000020.],attrs=[('axis',2,'Y'),('standard_name',2,'projection_y_coordinate'),('units',2,'m')]),
      dict(name='x',type=6,dims=[1],values=[500000.,500010.,500020.,500030.],attrs=[('axis',2,'X'),('standard_name',2,'projection_x_coordinate'),('units',2,'m')]),
      dict(name='crs',type=4,dims=[],values=[0],attrs=[('spatial_ref',2,reference.ExportToWkt()),('grid_mapping_name',2,'transverse_mercator')]),
    ]
    path=tmp_path/'grid.nc';classic_dataset(path,[('y',3),('x',4)],variables)
    plan=source_plan(path,'netcdf');report=netcdf.inspect(plan)
    selection=dict(variable='/temperature',indices=[],x=1,y=1,width=2,height=2)
    result=netcdf.read_slice(plan,report['format_info']['source_version'],selection)
    assert result['values']==[['5','6'],['9','10']]
    assert result['variable']['attributes']['scale_factor']['values']==['0.5']
    assert result['spatial']['transform']==[500005.,10.,0.,3000005.,0.,10.]
    assert result['spatial']['footprint']['geometry']['type']=='Polygon'
    variables[2]['values']=[500000.,500010.,500021.,500030.]
    classic_dataset(path,[('y',3),('x',4)],variables)
    version=netcdf.inspect(plan)['format_info']['source_version']
    assert netcdf.read_slice(plan,version,selection)['spatial'] is None
    variables[2]['values']=[500000.,500010.,500020.,500030.]
    variables[3]['attrs']=[]
    classic_dataset(path,[('y',3),('x',4)],variables)
    version=netcdf.inspect(plan)['format_info']['source_version']
    assert netcdf.read_slice(plan,version,selection)['spatial'] is None


def test_cdf5_uint64_mask_and_value_are_exact(tmp_path):
    path=tmp_path/'uint.nc';blank=18446744073709551615
    values=[9007199254740993,18446744073709551614,blank,1]
    classic_dataset(path,[('y',2),('x',2)],[dict(name='count',type=11,dims=[0,1],values=values,attrs=[('_FillValue',11,[blank])])],version=5)
    plan=source_plan(path,'netcdf');report=netcdf.inspect(plan)
    result=netcdf.read_slice(plan,report['format_info']['source_version'],dict(variable='/count',indices=[],x=0,y=0,width=2,height=2))
    assert result['values']==[[str(v) for v in values[:2]],[str(v) for v in values[2:]]]
    assert result['valid']==[[True,True],[False,True]]
    assert result['variable']['attributes']['_FillValue']['values']==[str(blank)]


def test_native_operator_dispatch_and_temporary_files_are_cleaned(tmp_path,monkeypatch):
    from operators import get_operator
    isolated = tmp_path/'native-tmp';isolated.mkdir();monkeypatch.setenv('TMPDIR', str(isolated))
    path=tmp_path/'source.nc';classic_fixture(path);plan=source_plan(path,'netcdf')
    report=get_operator('netcdf.inspect')(access_plan=plan)
    assert report['format_info']['scan_complete'] is True
    result=get_operator('netcdf.read_slice')(access_plan=plan,source_version=report['format_info']['source_version'],selection=dict(variable='/temperature',indices=[1],x=0,y=0,width=4,height=3))
    assert result['values'][0]==['12','13','14','15']
    assert result['valid']==[[True]*4]*3
    assert not list(isolated.iterdir())


def test_hosted_independent_classic_sources_match_native_and_exact_oracle(tmp_path):
    import importlib.util
    module_path=Path(__file__).resolve().parents[2]/'business/scripts/online-netcdf-fixture.py'
    spec=importlib.util.spec_from_file_location('netcdf_online_fixture',module_path);fixture=importlib.util.module_from_spec(spec);spec.loader.exec_module(fixture)
    for source in fixture.sources():
        path=tmp_path/source['name'];path.write_bytes(fixture.build(source['version'],source['projected']))
        plan=source_plan(path,'netcdf');report=netcdf.inspect(plan)
        assert len(report['container']['children'])==(5 if source['projected'] else 2)
        for layer in (0,1):
            result=netcdf.read_slice(plan,report['format_info']['source_version'],dict(variable='/temperature',indices=[layer],x=0,y=0,width=4,height=3))
            assert result['values']==fixture.expected_values(source['version'],layer)
            assert result['valid']==[[not(layer==0 and y==1 and x==2) for x in range(4)] for y in range(3)]
            assert (result['spatial'] is not None)==source['projected']


def test_netcdf_float_fill_nan_and_valid_range_preserve_raw_mask(tmp_path):
    path=tmp_path/'float.nc'
    classic_dataset(path,[('y',2),('x',3)],[dict(name='values',type=6,dims=[0,1],values=[float('nan'),1.25,-9,10,11,0],attrs=[('_FillValue',6,[float('nan')]),('valid_range',6,[0,10]),('scale_factor',6,[.5])])])
    plan=source_plan(path,'netcdf');version=netcdf.inspect(plan)['format_info']['source_version']
    result=netcdf.read_slice(plan,version,dict(variable='/values',indices=[],x=0,y=0,width=3,height=2))
    assert result['values']==[['NaN','1.25','-9.0'],['10.0','11.0','0.0']]
    assert result['valid']==[[False,True,False],[True,False,True]]


def test_netcdf_native_invalid_selection_returns_stable_rejection(tmp_path):
    from operators import get_operator
    path=tmp_path/'source.nc';classic_fixture(path);plan=source_plan(path,'netcdf')
    version=netcdf.inspect(plan)['format_info']['source_version']
    result=get_operator('netcdf.read_slice')(access_plan=plan,source_version=version,selection=dict(variable='/temperature',indices=[2],x=0,y=0,width=4,height=3))
    assert result['error_code']=='NETCDF_SLICE_INVALID'


def test_int64_attributes_use_the_existing_raw_api_not_newer_helpers(tmp_path, monkeypatch):
    from osgeo import gdal
    # GDAL 3.8.4 has ReadAsRaw, but no Attribute.ReadAsInt64Array.
    monkeypatch.delattr(gdal.Attribute, 'ReadAsInt64Array', raising=False)
    path = tmp_path / 'int64-attribute.nc'
    classic_fixture(path, version=5)
    result = netcdf.inspect(source_plan(path, 'netcdf'))
    attribute = result['container']['children'][0]['native']['attributes']['_FillValue']
    assert attribute == {'type': 'Int64', 'values': ['-9999']}


def test_numeric_attribute_storage_values_do_not_use_string_or_double_conversion(tmp_path, monkeypatch):
    from osgeo import gdal
    original_strings = gdal.Attribute.ReadAsStringArray
    def strings_only(attribute, *args, **kwargs):
        assert attribute.GetDataType().GetClass() == gdal.GEDTC_STRING
        return original_strings(attribute, *args, **kwargs)
    def forbidden(*args, **kwargs):
        raise AssertionError('numeric attributes must use their declared native storage type')
    monkeypatch.setattr(gdal.Attribute, 'ReadAsStringArray', strings_only)
    monkeypatch.setattr(gdal.Attribute, 'ReadAsDoubleArray', forbidden)
    monkeypatch.setattr(gdal.Attribute, 'ReadAsInt64Array', forbidden, raising=False)
    cases = [(1, [-128, 127]), (3, [-32768, 32767]), (4, [-2147483648, 2147483647]),
             (5, [-1.25, .5]), (6, [-273.15, .5]), (7, [0, 255]),
             (8, [0, 65535]), (9, [0, 4294967295]),
             (10, [-9223372036854775808, 9223372036854775807]),
             (11, [9007199254740993, 18446744073709551615])]
    attrs = [(f'number{kind}', kind, values) for kind, values in cases]
    path = tmp_path / 'typed-attributes.nc'
    classic_dataset(path, [('y', 2), ('x', 2)],
                    [dict(name='data', type=4, dims=[0, 1], values=[0, 1, 2, 3],
                          attrs=[('units', 2, 'K')])], version=5, global_attributes=attrs)
    report = netcdf.inspect(source_plan(path, 'netcdf'))
    for kind, values in cases:
        assert report['format_info']['attributes'][f'number{kind}']['values'] == [str(v) for v in values]
