"""Independent Surfer 7 bytes, GDAL row/node/NoData and isolated source proof."""
from pathlib import Path
import struct
from unittest.mock import patch

import numpy as np
import pytest
from osgeo import gdal

from operators import raster_compute as compute
from operators.surfer7 import MAX_BYTES, inspect_surfer7
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
    with pytest.raises(ValueError,match='exact BlankValue'):inspect_surfer7(path)
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
    with pytest.raises(ValueError,match='64 MiB'):inspect_surfer7(path)


@pytest.mark.parametrize('language,fragment',[('zh-cn','完整网格'),('en','complete grid')])
def test_surfer7_source_error_uses_selected_language(tmp_path,language,fragment):
    path=tmp_path/'short.grd';path.write_bytes(b'DSRB')
    with pytest.raises(ValueError,match=fragment):inspect_surfer7(path,language)
