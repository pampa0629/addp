"""Independent binary Surfer bytes, GDAL row/node/NoData and isolated source proof."""
from pathlib import Path
import struct
from unittest.mock import patch

import numpy as np
import pytest
from osgeo import gdal

from operators import raster_compute as compute
from operators.surfer import MAX_BYTES, SURFER6_BLANK, inspect_surfer, validate_surfer6_mask
from test_raster_compute import source_plan


def grid(path, version=2, blank=1.70141e38):
    # Surfer stores the minimum-Y row first, independent of GDAL's top row.
    values=np.arange(15,dtype='<f8').reshape(3,5)+.25
    values[1,2]=blank
    valid=values[values!=blank]
    header=(b'DSRB'+struct.pack('<ii',4,version)+b'GRID'+struct.pack('<i',72)
            +struct.pack('<ii8d',3,5,100.,200.,2.,3.,valid.min(),valid.max(),0.,blank)
            +b'DATA'+struct.pack('<i',values.nbytes))
    assert len(header)==100
    path.write_bytes(header+values.tobytes())
    return values[::-1].copy()


@pytest.mark.parametrize('version',[1,2])
def test_surfer7_native_full_non_square_pixels_nodes_and_nodata(tmp_path,version):
    path=tmp_path/'height.grd';expected=grid(path,version);before=path.read_bytes()
    with compute.raster_workspace():
        loaded=compute.raster_load(source_plan(path,'grd'));native=compute._open(loaded)
        assert loaded.path!=path and native.GetDriver().ShortName=='GS7BG'
        np.testing.assert_array_equal(native.ReadAsArray(),expected)
        np.testing.assert_array_equal(native.GetRasterBand(1).GetMaskBand().ReadAsArray(),np.where(expected==1.70141e38,0,255))
        assert native.GetGeoTransform()==(99.,2.,0.,207.5,0.,-3.)
        assert native.GetRasterBand(1).DataType==gdal.GDT_Float64
        assert not native.GetProjection() and native.RasterCount==1
    report=compute.raster_inspect(source_plan(path,'grd'))['description']
    assert report['spatial'] is None
    assert (report['media']['width'],report['media']['height'],report['media']['encoding'])==(5,3,'GS7BG')
    assert report['format_info']['version']==version and report['format_info']['scan_complete'] is True
    assert report['format_info']['node_origin']==[100.,200.]
    assert report['format_info']['node_spacing']==[2.,3.]
    assert report['format_info']['node_extent']==[100.,200.,108.,206.]
    assert path.read_bytes()==before


def test_surfer7_sidecars_cannot_supply_crs_and_explicit_workflow_crs_does_not_mutate_source(tmp_path):
    path=tmp_path/'height.grd';expected=grid(path);before=path.read_bytes()
    path.with_suffix('.grd.aux.xml').write_text('<PAMDataset><SRS>EPSG:4326</SRS></PAMDataset>')
    path.with_suffix('.grd.gsr2').write_text('unselected projection settings')
    description=compute.raster_inspect(source_plan(path,'grd'))['description']
    assert description['spatial'] is None
    with compute.raster_workspace():
        loaded=compute.raster_load(source_plan(path,'grd'),source_crs='EPSG:32650')
        facts=compute.raster_info(loaded)
        assert facts['extent_srid']==32650 and facts['transform']==[99.,2.,0.,207.5,0.,-3.]
        np.testing.assert_array_equal(compute._open(loaded).ReadAsArray(),expected)
    assert path.read_bytes()==before


@pytest.mark.parametrize('fault',['ascii','version','grid_tag','data_tag','length','rows','spacing','rotation',
    'nonfinite_grid','nonfinite_pixel','half_pixel_underflow','tail','fault_section','truncated'])
def test_surfer7_invalid_source_is_rejected_before_native_open(tmp_path,fault):
    path=tmp_path/'bad.grd';grid(path);raw=bytearray(path.read_bytes())
    if fault=='ascii':raw[:4]=b'DSAA'
    if fault=='version':struct.pack_into('<i',raw,8,3)
    if fault=='grid_tag':raw[12:16]=b'GRID';struct.pack_into('<i',raw,16,71)
    if fault=='data_tag':raw[92:96]=b'DATX'
    if fault=='length':struct.pack_into('<i',raw,96,112)
    if fault=='rows':struct.pack_into('<i',raw,20,1)
    if fault=='spacing':struct.pack_into('<d',raw,44,-2.)
    if fault=='rotation':struct.pack_into('<d',raw,76,10.)
    if fault=='nonfinite_grid':struct.pack_into('<d',raw,28,float('nan'))
    if fault=='nonfinite_pixel':struct.pack_into('<d',raw,100,float('inf'))
    if fault=='half_pixel_underflow':
        struct.pack_into('<d',raw,28,0.)
        struct.pack_into('<d',raw,44,5e-324)
    if fault=='tail':raw+=b'extra'
    if fault=='fault_section':raw+=b'FLTI'+struct.pack('<iii',8,0,0)
    if fault=='truncated':raw=raw[:-1]
    path.write_bytes(raw)
    with patch.object(compute.gdal,'OpenEx') as native:
        with pytest.raises(ValueError):compute.raster_inspect(source_plan(path,'grd'))
        native.assert_not_called()
    assert path.read_bytes()==raw


def test_surfer7_version_one_range_nodata_is_not_silently_read_as_valid(tmp_path):
    path=tmp_path/'range.grd';grid(path,version=1,blank=10.)
    with pytest.raises(ValueError,match='exact BlankValue'):inspect_surfer(path)
    grid(path,version=2,blank=10.)
    with compute.raster_workspace():
        loaded=compute.raster_load(source_plan(path,'grd'))
        dataset=compute._open(loaded)
        band=dataset.GetRasterBand(1)
        # Version 2 values greater than BlankValue remain valid.
        assert np.count_nonzero(band.GetMaskBand().ReadAsArray()==0)==1


def test_surfer7_oversize_is_rejected_without_reading_payload(tmp_path):
    path=tmp_path/'large.grd'
    with path.open('wb') as out:out.truncate(MAX_BYTES+1)
    with pytest.raises(ValueError,match='64 MiB'):inspect_surfer(path)


@pytest.mark.parametrize('language,fragment',[('zh-cn','完整网格'),('en','complete grid')])
def test_surfer7_source_error_uses_selected_language(tmp_path,language,fragment):
    path=tmp_path/'short.grd';path.write_bytes(b'DSRB')
    with pytest.raises(ValueError,match=fragment):inspect_surfer(path,language)


def grid6(path, all_missing=False, extreme=False):
    values=np.arange(15,dtype='<f4').reshape(3,5)+np.float32(.125)
    values[0,0]=-12.5
    values[1,2]=SURFER6_BLANK
    values[2,4]=np.float32(2e38 if extreme else 16777216.)
    if all_missing:
        values[:]=SURFER6_BLANK
    valid=values[values!=SURFER6_BLANK]
    minimum,maximum=(float(valid.min()),float(valid.max())) if valid.size else (1.70141e38,1.70141e38)
    path.write_bytes(b'DSBB'+struct.pack('<hh6d',5,3,100.,108.,200.,206.,minimum,maximum)+values.tobytes())
    return values[::-1].copy()


@pytest.mark.parametrize('all_missing',[False,True])
def test_surfer6_exact_native_float32_pixels_mask_grid_and_readonly_facts(tmp_path,all_missing):
    path=tmp_path/'height.grd';expected=grid6(path,all_missing);before=path.read_bytes()
    path.with_suffix('.grd.aux.xml').write_text('<PAMDataset><SRS>EPSG:4326</SRS></PAMDataset>')
    path.with_suffix('.gsr2').write_text('unselected projection settings')
    with compute.raster_workspace():
        loaded=compute.raster_load(source_plan(path,'grd'));native=compute._open(loaded)
        assert native.GetDriver().ShortName=='GSBG' and native.RasterCount==1
        assert native.GetFileList()==[str(loaded.path)] and native.GetSpatialRef() is None
        band=native.GetRasterBand(1)
        assert band.DataType==gdal.GDT_Float32 and band.GetNoDataValue()==SURFER6_BLANK
        np.testing.assert_array_equal(native.ReadAsArray(),expected)
        np.testing.assert_array_equal(band.GetMaskBand().ReadAsArray(),np.where(expected==SURFER6_BLANK,0,255))
        assert native.GetGeoTransform()==(99.,2.,0.,207.5,0.,-3.)
        declared=compute.raster_load(source_plan(path,'grd'),source_crs='EPSG:32650')
        assert compute.raster_info(declared)['extent_srid']==32650
        np.testing.assert_array_equal(compute._open(declared).ReadAsArray(),expected)
    report=compute.raster_inspect(source_plan(path,'grd'))['description']
    assert report['spatial'] is None and report['media']['encoding']=='GSBG'
    info=report['format_info']
    assert info['profile']=='surfer6_binary' and 'version' not in info
    assert info['node_origin']==[100.,200.] and info['node_spacing']==[2.,3.]
    assert info['node_shape']==[3,5] and info['node_extent']==[100.,200.,108.,206.]
    assert info['blank_value']==SURFER6_BLANK and info['bands'][0]['dtype']=='Float32'
    assert path.read_bytes()==before


@pytest.mark.parametrize('fault',['ascii','signed_dimension','single_row','truncated','tail','nonfinite_header',
    'reversed_extent','zero_extent','range_overflow','collapsed_half_pixel','nonfinite_pixel','invalid_z_range'])
def test_surfer6_invalid_frames_never_reach_native_open(tmp_path,fault):
    path=tmp_path/'bad.grd';grid6(path);raw=bytearray(path.read_bytes())
    if fault=='ascii':raw[:4]=b'DSAA'
    if fault=='signed_dimension':struct.pack_into('<h',raw,4,-5)
    if fault=='single_row':struct.pack_into('<h',raw,6,1)
    if fault=='truncated':raw=raw[:-1]
    if fault=='tail':raw+=b'ignored?'
    if fault=='nonfinite_header':struct.pack_into('<d',raw,8,float('nan'))
    if fault=='reversed_extent':struct.pack_into('<d',raw,16,99.)
    if fault=='zero_extent':struct.pack_into('<d',raw,16,100.)
    if fault=='range_overflow':struct.pack_into('<2d',raw,8,-1e308,1e308)
    if fault=='collapsed_half_pixel':struct.pack_into('<2d',raw,8,1e16,np.nextafter(1e16,float('inf')))
    if fault=='nonfinite_pixel':struct.pack_into('<f',raw,56,float('inf'))
    if fault=='invalid_z_range':struct.pack_into('<2d',raw,40,10.,-10.)
    path.write_bytes(raw)
    with patch.object(compute.gdal,'OpenEx') as native:
        with pytest.raises(ValueError):compute.raster_inspect(source_plan(path,'grd'))
        native.assert_not_called()


@pytest.mark.parametrize('language,fragment',[('zh-cn','完整'),('en','complete')])
def test_surfer6_frame_error_uses_selected_language(tmp_path,language,fragment):
    path=tmp_path/'short.grd';path.write_bytes(b'DSBB')
    with pytest.raises(ValueError,match=fragment):inspect_surfer(path,language)


@pytest.mark.parametrize('language,fragment',[('zh-cn','掩膜'),('en','validity mask')])
def test_surfer6_native_mask_mismatch_is_rejected_without_rewriting_source(tmp_path,language,fragment):
    path=tmp_path/'huge-value.grd';grid6(path,extreme=True);before=path.read_bytes()
    assert inspect_surfer(path)['driver']=='GSBG'
    native=gdal.Open(str(path));band=native.GetRasterBand(1)
    faithful=np.array_equal(band.GetMaskBand().ReadAsArray(),np.where(native.ReadAsArray()==SURFER6_BLANK,0,255))
    native=None
    with compute.raster_workspace(), patch.object(compute,'_DISK_LANGUAGE') as selected:
        selected.get.return_value=language
        if faithful:
            loaded=compute.raster_load(source_plan(path,'grd'))
            assert compute._open(loaded).ReadAsArray()[0,4]==np.float32(2e38)
        else:
            with pytest.raises(ValueError,match=fragment):compute.raster_load(source_plan(path,'grd'))
    assert path.read_bytes()==before


@pytest.mark.parametrize('faithful',[False,True])
@pytest.mark.parametrize('driver',['GSBG','GSAG'])
def test_surfer6_mask_gate_follows_exact_validity_even_if_native_bug_changes(faithful,driver):
    from unittest.mock import Mock
    from operators.surfer import SURFER6_ASCII_BLANK
    blank=SURFER6_BLANK if driver=='GSBG' else SURFER6_ASCII_BLANK
    values=np.array([blank,2e38],dtype=np.float32 if driver=='GSBG' else np.float64)
    band=Mock();band.ReadRaster.return_value=values.tobytes()
    band.GetMaskBand.return_value.ReadRaster.return_value=bytes([0,255 if faithful else 0])
    dataset=Mock(RasterXSize=2,RasterYSize=1);dataset.GetRasterBand.return_value=band
    dataset.GetDriver.return_value.ShortName=driver;band.GetNoDataValue.return_value=blank
    if faithful:validate_surfer6_mask(dataset,blank)
    else:
        with pytest.raises(ValueError,match='validity mask'):validate_surfer6_mask(dataset,blank)


def grid_ascii(path, ending='\n', all_missing=False):
    values=['-12.5','0.123456789012345','1.25','-0','4','5','1.70141e38','7','8','9','10','11','12','16777217','9007199254740991']
    if all_missing:values=['1.70141e38']*15
    limits='1.70141e38 1.70141e38' if all_missing else '-12.5 9007199254740991'
    # Tabs and scientific notation; data wrapping is independent of grid rows.
    path.write_bytes(ending.join(['DSAA','5\t3','1e2 108','200 206',limits,' '.join(values[:4]),'\t'.join(values[4:]),'']).encode('ascii'))
    return np.array([float(v) for v in values],dtype=np.float64).reshape(3,5)[::-1].copy()


@pytest.mark.parametrize('ending',['\n','\r\n'])
@pytest.mark.parametrize('all_missing',[False,True])
def test_surfer6_ascii_complete_native_float64_values_grid_mask_and_isolation(tmp_path,ending,all_missing):
    from operators.surfer import SURFER6_ASCII_BLANK
    path=tmp_path/'ascii.grd';expected=grid_ascii(path,ending,all_missing);before=path.read_bytes()
    path.with_suffix('.gsr2').write_text('unselected CRS')
    path.with_suffix('.grd.aux.xml').write_text('<PAMDataset><SRS>EPSG:4326</SRS></PAMDataset>')
    with compute.raster_workspace():
        loaded=compute.raster_load(source_plan(path,'grd'));native=compute._open(loaded)
        assert native.GetDriver().ShortName=='GSAG' and native.RasterCount==1
        assert native.GetFileList()==[str(loaded.path)] and not native.GetProjection()
        assert native.GetRasterBand(1).DataType==gdal.GDT_Float64
        assert native.GetRasterBand(1).GetNoDataValue()==SURFER6_ASCII_BLANK
        np.testing.assert_array_equal(native.ReadAsArray(),expected)
        np.testing.assert_array_equal(np.signbit(native.ReadAsArray()),np.signbit(expected))
        np.testing.assert_array_equal(native.GetRasterBand(1).GetMaskBand().ReadAsArray(),np.where(expected==SURFER6_ASCII_BLANK,0,255))
        assert native.GetGeoTransform()==(99.,2.,0.,207.5,0.,-3.)
        declared=compute.raster_load(source_plan(path,'grd'),source_crs='EPSG:32650')
        assert compute.raster_info(declared)['extent_srid']==32650
        np.testing.assert_array_equal(compute._open(declared).ReadAsArray(),expected)
    report=compute.raster_inspect(source_plan(path,'grd'))['description']
    assert report['spatial'] is None and report['media']['encoding']=='GSAG'
    info=report['format_info']
    assert info['profile']=='surfer6_ascii' and 'version' not in info and info['scan_complete']
    assert info['node_shape']==[3,5] and info['node_extent']==[100.,200.,108.,206.]
    assert info['bands'][0]['dtype']=='Float64' and info['blank_value']==SURFER6_ASCII_BLANK
    assert path.read_bytes()==before


@pytest.mark.parametrize('fault',['bom','non_ascii','comma','comment','nan','infinity','overflow','underflow',
    'tail_number','tail_text','truncated','dimension','single_row','reversed_extent','collapsed_grid',
    'too_long_token','too_long_header','header_controls','double_cr','bad_number','no_header_newline'])
def test_surfer6_ascii_rejects_unproved_text_before_native_open(tmp_path,fault):
    path=tmp_path/'bad.grd';grid_ascii(path);raw=path.read_bytes()
    changes={
        'bom':(b'DSAA',b'\xef\xbb\xbfDSAA'), 'non_ascii':(b'1.25',b'\xff'),
        'comma':(b'1.25',b'1,25'), 'comment':(b'1.25',b'#1.25'),
        'nan':(b'1.25',b'NaN'), 'infinity':(b'1.25',b'inf'), 'overflow':(b'1.25',b'1e400'),
        'underflow':(b'1.25',b'1e-400'), 'dimension':(b'5\t3',b'5.0 3'),
        'single_row':(b'5\t3',b'5 1'), 'reversed_extent':(b'1e2 108',b'108 100'),
        'collapsed_grid':(b'1e2 108',b'1e16 10000000000000002'),
        'too_long_token':(b'1.25',b'1.'+b'0'*128),
        'too_long_header':(b'1e2 108',b' '*200+b'100 108'),
        'header_controls':(b'5\t3',b'5\x0b3'), 'double_cr':(b'DSAA\n',b'DSAA\r\r\n'),
        'bad_number':(b'1.25',b'1.2.3'), 'no_header_newline':(b'DSAA\n',b'DSAA '),
    }
    if fault in changes:raw=raw.replace(*changes[fault],1)
    if fault=='tail_number':raw+=b' 99\n'
    if fault=='tail_text':raw+=b' END\n'
    if fault=='truncated':raw=raw.rsplit(b'9007199254740991',1)[0]
    path.write_bytes(raw)
    with patch.object(compute.gdal,'OpenEx') as native:
        with pytest.raises(ValueError):compute.raster_inspect(source_plan(path,'grd'))
        native.assert_not_called()
    assert path.read_bytes()==raw


def test_surfer6_ascii_chunk_split_and_no_final_newline(tmp_path):
    path=tmp_path/'split.grd';grid_ascii(path)
    raw=path.read_bytes();offset=raw.index(b'0.123456789012345')
    # Make a decimal token cross the payload scanner's 64KiB boundary.
    header_size=sum(len(line) for line in raw.splitlines(keepends=True)[:5])
    path.write_bytes(raw[:offset]+b' '*(header_size+65536-5-offset)+raw[offset:].rstrip())
    with compute.raster_workspace():
        loaded=compute.raster_load(source_plan(path,'grd'))
        assert compute._open(loaded).ReadAsArray()[2,1]==.123456789012345


@pytest.mark.parametrize('language,fragment',[('zh-cn','掩膜'),('en','validity mask')])
def test_surfer6_ascii_near_blank_native_mask_mismatch_is_rejected(tmp_path,language,fragment):
    from operators.surfer import SURFER6_ASCII_BLANK
    path=tmp_path/'near-blank.grd';grid_ascii(path)
    path.write_bytes(path.read_bytes().replace(b'1.25',b'1.701410009187828e38'))
    before=path.read_bytes();ds=gdal.Open(str(path));pixels=ds.ReadAsArray()
    faithful=np.array_equal(ds.GetRasterBand(1).GetMaskBand().ReadAsArray(),np.where(pixels==SURFER6_ASCII_BLANK,0,255))
    ds=None
    with compute.raster_workspace(),patch.object(compute,'_DISK_LANGUAGE') as selected:
        selected.get.return_value=language
        if faithful:compute.raster_load(source_plan(path,'grd'))
        else:
            with pytest.raises(ValueError,match=fragment):compute.raster_load(source_plan(path,'grd'))
    assert path.read_bytes()==before


def test_surfer6_ascii_native_pixel_difference_is_rejected(tmp_path):
    from unittest.mock import Mock
    from operators.surfer import validate_surfer_ascii_pixels
    path=tmp_path/'ascii.grd';grid_ascii(path)
    dataset=Mock(RasterXSize=5,RasterYSize=3)
    dataset.GetRasterBand.return_value.ReadRaster.return_value=np.zeros(5,dtype=np.float64).tobytes()
    with pytest.raises(ValueError,match='representable'):validate_surfer_ascii_pixels(dataset,path)
