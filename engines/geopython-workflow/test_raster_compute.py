import json
from pathlib import Path
import time
from concurrent.futures import ThreadPoolExecutor
import numpy as np
import pytest
from osgeo import gdal, ogr, osr
from operators import OPERATORS, list_operators
from operators.raster_compute import (
    raster_workspace, raster_load, raster_info, raster_save, raster_to_cog,
    raster_resample, raster_reproject, raster_clip, raster_mosaic,
    raster_band_math, raster_statistics, raster_histogram, raster_build_overviews, validate_cog,
)
from workflow_engine import execute_workflow


def source_plan(path, format='tiff'):
    return {'schema_version': 'addp.workflow.access-plan/v1', 'source': {
        'kind': 'file', 'format': format, 'access': {'method': 'mounted_path', 'path': str(path)}}}


def target_plan(path, mode='create'):
    return {'schema_version': 'addp.workflow.access-plan/v1', 'target': {
        'kind': 'file', 'format': 'tiff', 'name': path.name, 'write_mode': mode,
        'access': {'method': 'mounted_path', 'path': str(path)}}}


def read_band_values(band):
    return np.frombuffer(band.ReadRaster(buf_type=gdal.GDT_Float64),
                         dtype=np.float64).reshape(band.YSize,band.XSize)


def create_raster(path, array, transform=(0, 1, 0, 4, 0, -1), crs='EPSG:4326', nodata=-9999):
    if array.ndim == 2:
        array = array[None, ...]
    ds = gdal.GetDriverByName('GTiff').Create(str(path), array.shape[2], array.shape[1], array.shape[0], gdal.GDT_Float64)
    ds.SetGeoTransform(transform)
    if crs:
        definition = osr.SpatialReference(); definition.SetFromUserInput(crs)
        ds.SetProjection(definition.ExportToWkt())
    for index, values in enumerate(array, 1):
        band = ds.GetRasterBand(index); band.SetNoDataValue(nodata); band.WriteRaster(0, 0, values.shape[1], values.shape[0], values.astype(np.float64).tobytes(), buf_type=gdal.GDT_Float64)
    ds = None
    return path


@pytest.fixture
def raster_file(tmp_path):
    values = np.arange(1, 17, dtype=float).reshape(4, 4)
    values[0, 0] = -9999
    return create_raster(tmp_path / 'source.tif', np.stack([values, values * 2]))


def test_block_statistics_histogram_and_nodata(raster_file):
    with raster_workspace():
        raster = raster_load(source_plan(raster_file))
        stats = raster_statistics(raster)
        np.testing.assert_allclose([stats['min'], stats['max'], stats['mean'], stats['stddev']], [2, 16, 9, np.std(np.arange(2, 17))])
        assert (stats['valid_count'], stats['invalid_count']) == (15, 1)
        histogram = raster_histogram(raster, bins=4, value_range=[4, 12])
        expected, edges = np.histogram(np.arange(2, 17), bins=4, range=(4, 12))
        assert histogram['counts'] == expected.tolist()
        assert histogram['edges'] == edges.tolist()
        assert histogram['outside_count'] == 6


def test_statistics_combines_blocks_and_all_nodata(tmp_path):
    data = np.arange(1025 * 513, dtype=float).reshape(513, 1025) + 1e9
    path = create_raster(tmp_path / 'blocks.tif', data)
    with raster_workspace():
        stats = raster_statistics(raster_load(source_plan(path)))
        assert stats['valid_count'] == data.size
        assert stats['mean'] == pytest.approx(data.mean())
        assert stats['stddev'] == pytest.approx(data.std())
    path = create_raster(tmp_path / 'empty.tif', np.full((3, 3), -9999.0))
    with raster_workspace():
        raster = raster_load(source_plan(path))
        assert raster_statistics(raster)['mean'] is None
        assert raster_histogram(raster)['counts'] == []


@pytest.mark.parametrize('dtype,alpha,positions,counts,alpha_valid', [
    (gdal.GDT_Byte, [[0,255,128,0],[255,0,255,255]],
     [(0,1),(0,2),(1,0),(1,2),(1,3)], [2,1,2], 8),
    (gdal.GDT_UInt16, [[0,65535,1,0],[65535,0,65535,65535]],
     [(0,1),(0,2),(1,0),(1,2),(1,3)], [2,1,2], 8),
    (gdal.GDT_Float64, [[0,255,128,0],[255,0,255,255]],
     [(0,1),(0,2),(1,0),(1,2),(1,3)], [2,1,2], 8),
    (gdal.GDT_Float64, [[np.nan,1,0.5,0],[np.inf,-1,0,1]],
     [(0,1),(0,2),(1,3)], [2,0,1], 6),
], ids=['byte-alpha', 'uint16-alpha', 'float64-alpha', 'fractional-and-invalid-alpha'])
def test_numeric_analysis_excludes_transparent_source_data(tmp_path, dtype, alpha, positions, counts, alpha_valid):
    source = tmp_path / 'alpha.tif'
    data = np.array([[0,20,30,40],[50,60,70,80]], dtype=np.float64)
    alpha = np.array(alpha, dtype=np.float64)
    expected_values = np.array([data[y,x] for y,x in positions])
    dataset = gdal.GetDriverByName('GTiff').Create(str(source), 4,2,2,dtype)
    dataset.SetGeoTransform((0,1,0,2,0,-1))
    crs = osr.SpatialReference(); crs.ImportFromEPSG(4326)
    dataset.SetSpatialRef(crs)
    for index, array in enumerate([data,alpha],1):
        dataset.GetRasterBand(index).WriteRaster(0,0,4,2,array.tobytes(),buf_type=gdal.GDT_Float64)
    dataset.GetRasterBand(2).SetColorInterpretation(gdal.GCI_AlphaBand)
    dataset = None
    original = source.read_bytes()
    with raster_workspace():
        raster = raster_load(source_plan(source))
        stats = raster_statistics(raster)
        assert (stats['valid_count'], stats['invalid_count'], stats['min'], stats['max']) == (len(positions),8-len(positions),20,80)
        assert stats['mean'] == pytest.approx(expected_values.mean())
        histogram = raster_histogram(raster, bins=3, value_range=[20,80])
        assert histogram['counts'] == counts
        assert (histogram['valid_count'], histogram['invalid_count'], histogram['outside_count']) == (len(positions),8-len(positions),0)
        calculated = raster_band_math(raster, 'b1*2')
        assert raster_statistics(calculated)['valid_count'] == len(positions)
        target = tmp_path / 'math.tif'
        raster_save(calculated, target_plan(target), profile='cog')
        persisted = raster_load(source_plan(target))
        assert raster_statistics(persisted)['mean'] == pytest.approx(expected_values.mean()*2)
        # Alpha remains an explicit selectable band; its zero values are coverage facts.
        assert raster_statistics(raster, band=2)['valid_count'] == alpha_valid
    output = gdal.Open(str(target))
    expected = np.full(data.shape, np.nan)
    for y,x in positions:
        expected[y,x] = data[y,x]*2
    np.testing.assert_equal(read_band_values(output.GetRasterBand(1)), expected)
    assert source.read_bytes() == original


def test_math_is_safe_and_propagates_nodata(raster_file):
    with raster_workspace():
        raster = raster_load(source_plan(raster_file))
        result = raster_band_math(raster, '(b2-b1)/(b2+b1)')
        stats = raster_statistics(result)
        assert stats['valid_count'] == 15
        assert stats['mean'] == pytest.approx(1/3)
        reciprocal = raster_band_math(raster, 'b1**-1')
        assert raster_statistics(reciprocal)['mean'] == pytest.approx(np.mean(1 / np.arange(2, 17)))
        invalid = raster_band_math(raster, 'b1/(b1-b1)')
        assert raster_statistics(invalid)['valid_count'] == 0
        for expression in ["__import__('os').system('id')", 'b1.__class__', 'b1[0]', '[b1]', 'b3', '1', 'abs(b1,b2)', 'True', 'b1**(10**1000)']:
            with pytest.raises((ValueError, OverflowError, SyntaxError)):
                raster_band_math(raster, expression)


def test_resampling_and_reprojection(raster_file):
    with raster_workspace():
        raster = raster_load(source_plan(raster_file))
        half = raster_resample(raster, size=[2, 2])
        facts = raster_info(half)
        assert facts['transform'] == [0, 2, 0, 4, 0, -2]
        assert (facts['width'], facts['height']) == (2, 2)
        assert raster_statistics(half)['mean'] == pytest.approx(11)
        warped = raster_reproject(raster, 'EPSG:3857')
        assert raster_info(warped)['extent_srid'] == 3857
        assert raster_info(warped)['extent'][2] > 400000
        for kwargs in [{}, {'size': [2,2], 'resolution': [1,1]}, {'size': [True,2]}, {'resolution':[0,1]}]:
            with pytest.raises(ValueError): raster_resample(raster, **kwargs)


@pytest.mark.parametrize('dtype,opaque,nodata', [
    (gdal.GDT_Byte,255,250), (gdal.GDT_UInt16,65535,65000), (gdal.GDT_Float64,255,-9999),
], ids=['byte-alpha','uint16-alpha','float64-alpha'])
@pytest.mark.parametrize('overlap', ['first','last'])
@pytest.mark.parametrize('profile', ['geotiff','cog'])
def test_alpha_clip_reproject_mosaic_save_preserves_valid_pixels(
        tmp_path, dtype, opaque, nodata, overlap, profile):
    def write_source(path, data, alpha, transform, crs):
        dataset = gdal.GetDriverByName('GTiff').Create(str(path),6,4,2,dtype)
        dataset.SetGeoTransform(transform)
        reference = osr.SpatialReference(); reference.SetFromUserInput(crs)
        dataset.SetSpatialRef(reference)
        for index,array in enumerate([data,alpha],1):
            band = dataset.GetRasterBand(index)
            band.SetNoDataValue(nodata)
            band.WriteRaster(0,0,6,4,array.astype(np.float64).tobytes(),buf_type=gdal.GDT_Float64)
        dataset.GetRasterBand(2).SetColorInterpretation(gdal.GCI_AlphaBand)
        dataset = band = None
        return path

    values = np.arange(24,dtype=np.float64).reshape(4,6)
    values[1,4] = nodata
    alpha = np.full((4,6),opaque,dtype=np.float64)
    alpha[0,1] = alpha[3,5] = 0
    alpha[0,0] = opaque//2
    source = write_source(tmp_path/'source.tif',values,alpha,(0,1,0,4,0,-1),'EPSG:4326')
    clipped_expected = values.copy()
    clipped_expected[(alpha==0)|(values==nodata)] = np.nan
    clipped_expected[1:3,2:4] = np.nan
    polygon = {'type':'Polygon','coordinates':[
        [[0,0],[6,0],[6,4],[0,4],[0,0]],
        [[2,1],[2,3],[4,3],[4,1],[2,1]],
    ]}
    # Independent spherical Mercator formulas determine the target grid and source cells.
    radius = 6378137.0
    resolution = radius*np.pi/180
    north = radius*np.log(np.tan(np.pi/4+np.deg2rad(4)/2))
    target_transform = (0,resolution,0,north,0,-resolution)
    other_values = np.arange(24,dtype=np.float64).reshape(4,6)+100
    other_values[1,4] = nodata
    other_values[3,5] = 0
    other_alpha = np.full((4,6),opaque,dtype=np.float64)
    other_alpha[0,2] = other_alpha[2,0] = 0
    other_alpha[3,5] = opaque//2
    other = write_source(tmp_path/'other.tif',other_values,other_alpha,
                         (2*resolution,resolution,0,5*resolution,0,-resolution),'EPSG:3857')
    originals = {path:path.read_bytes() for path in [source,other]}
    target = tmp_path/'result.tif'
    with raster_workspace():
        raster = raster_load(source_plan(source))
        workspace = raster.workspace
        clipped = raster_clip(raster,'EPSG:4326',geometry=polygon)
        clipped_dataset = gdal.Open(str(clipped.path))
        assert clipped_dataset.RasterCount==2
        np.testing.assert_equal(read_band_values(clipped_dataset.GetRasterBand(1)),clipped_expected)
        clipped_alpha = clipped_dataset.GetRasterBand(2)
        assert clipped_alpha.GetColorInterpretation()==gdal.GCI_AlphaBand
        coverage = read_band_values(clipped_alpha)
        np.testing.assert_equal(coverage>0,np.isfinite(clipped_expected))
        assert 0<coverage[0,0]<coverage.max()
        clipped_dataset = clipped_alpha = None
        warped = raster_reproject(clipped,'EPSG:3857',[resolution,resolution])
        warped_dataset = gdal.Open(str(warped.path))
        assert (warped_dataset.RasterXSize,warped_dataset.RasterYSize,warped_dataset.RasterCount)==(6,4,2)
        np.testing.assert_allclose(warped_dataset.GetGeoTransform(),target_transform,rtol=0,atol=1e-7)
        assert warped_dataset.GetSpatialRef().GetAuthorityCode(None)=='3857'
        longitude = np.rad2deg((np.arange(6)+.5)*resolution/radius)
        latitude = np.rad2deg(np.arctan(np.sinh((north-(np.arange(4)+.5)*resolution)/radius)))
        source_x = np.floor(longitude).astype(int)
        source_y = np.floor(4-latitude).astype(int)
        warped_expected = clipped_expected[np.ix_(source_y,source_x)]
        np.testing.assert_equal(read_band_values(warped_dataset.GetRasterBand(1)),warped_expected)
        np.testing.assert_equal(read_band_values(warped_dataset.GetRasterBand(2))>0,np.isfinite(warped_expected))
        warped_dataset = None
        other_raster = raster_load(source_plan(other))
        mosaic = raster_mosaic(warped,other_raster,'EPSG:3857',[resolution,resolution],overlap)
        mosaic_dataset = gdal.Open(str(mosaic.path))
        width,height = mosaic_dataset.RasterXSize,mosaic_dataset.RasterYSize
        assert width==8 and height>=5
        np.testing.assert_allclose(mosaic_dataset.GetGeoTransform(),
                                   (0,resolution,0,5*resolution,0,-resolution),rtol=0,atol=1e-7)
        # Sample both independently known source grids at every returned target center.
        # GDAL may retain blank edge rows in its automatically suggested union extent.
        rows,columns = np.indices((height,width))
        center_x = (columns+.5)*resolution
        center_y = (5-rows-.5)*resolution
        left_x = np.floor(center_x/resolution).astype(int)
        left_y = np.floor((north-center_y)/resolution).astype(int)
        left_inside = (left_x>=0)&(left_x<6)&(left_y>=0)&(left_y<4)
        left = np.full((height,width),np.nan)
        left[left_inside] = warped_expected[left_y[left_inside],left_x[left_inside]]
        right_x = np.floor((center_x-2*resolution)/resolution).astype(int)
        right_y = np.floor((5*resolution-center_y)/resolution).astype(int)
        right_inside = (right_x>=0)&(right_x<6)&(right_y>=0)&(right_y<4)
        right = np.full((height,width),np.nan)
        other_valid = other_values.copy()
        other_valid[(other_alpha==0)|(other_values==nodata)] = np.nan
        right[right_inside] = other_valid[right_y[right_inside],right_x[right_inside]]
        preferred,fallback = (left,right) if overlap=='first' else (right,left)
        expected = np.where(np.isfinite(preferred),preferred,fallback)
        np.testing.assert_equal(read_band_values(mosaic_dataset.GetRasterBand(1)),expected)
        np.testing.assert_equal(read_band_values(mosaic_dataset.GetRasterBand(2))>0,np.isfinite(expected))
        mosaic_dataset = None
        raster_save(mosaic,target_plan(target),profile=profile,blocksize=128)
        persisted = raster_load(source_plan(target))
        facts = raster_info(persisted)
        assert (facts['width'],facts['height'],facts['band_count'],facts['extent_srid'])==(width,height,2,3857)
        stats = raster_statistics(persisted)
        valid = expected[np.isfinite(expected)]
        assert (stats['valid_count'],stats['invalid_count'])==(valid.size,expected.size-valid.size)
        np.testing.assert_allclose([stats['min'],stats['max'],stats['mean'],stats['stddev']],
                                   [valid.min(),valid.max(),valid.mean(),valid.std()])
        histogram = raster_histogram(persisted,bins=5,value_range=[0,125])
        counts,edges = np.histogram(valid,bins=5,range=(0,125))
        assert histogram['counts']==counts.tolist() and histogram['edges']==edges.tolist()
        assert histogram['outside_count']==0
        calculated = raster_band_math(persisted,'b1+1')
        calculated_dataset = gdal.Open(str(calculated.path))
        np.testing.assert_equal(read_band_values(calculated_dataset.GetRasterBand(1)),expected+1)
        calculated_dataset = None
        if profile=='cog':
            assert validate_cog(persisted)['valid']
    assert not workspace.exists()
    output = gdal.Open(str(target))
    np.testing.assert_equal(read_band_values(output.GetRasterBand(1)),expected)
    assert output.GetRasterBand(1).DataType==gdal.GDT_Float64
    assert np.isnan(output.GetRasterBand(1).GetNoDataValue())
    output = None
    assert all(path.read_bytes()==original for path,original in originals.items())
    assert {path.name for path in tmp_path.iterdir()}=={'source.tif','other.tif','result.tif'}


def test_clip_and_mosaic(raster_file, tmp_path):
    right = create_raster(tmp_path / 'right.tif', np.full((2, 4, 4), 100.0), transform=(2,1,0,4,0,-1))
    with raster_workspace():
        raster = raster_load(source_plan(raster_file))
        clipped = raster_clip(raster, 'EPSG:4326', bbox=[1, 1, 3, 3])
        assert raster_info(clipped)['extent'] == [1, 1, 3, 3]
        assert raster_statistics(clipped)['mean'] == pytest.approx(8.5)
        geometry = {'type':'Polygon','coordinates':[[[1,1],[3,1],[3,3],[1,3],[1,1]]]}
        masked = raster_clip(raster, 'EPSG:4326', geometry=geometry)
        assert raster_info(masked)['width'] == 2
        with pytest.raises(ValueError, match='intersect'): raster_clip(raster, 'EPSG:4326', bbox=[10,10,12,12])
        other = raster_load(source_plan(right))
        last = raster_mosaic(raster, other, 'EPSG:4326', [1,1], 'last')
        first = raster_mosaic(raster, other, 'EPSG:4326', [1,1], 'first')
        assert raster_info(last)['extent'] == [0,0,6,4]
        assert raster_statistics(last)['mean'] > raster_statistics(first)['mean']


def test_polygon_clip_integer_data_keeps_valid_zero_and_excludes_transparent_pixels(tmp_path):
    values = np.arange(16, dtype=float).reshape(4, 4)
    values[0, 0] = -9999
    values[0, 1] = 0
    source = create_raster(tmp_path / 'float.tif', values)
    integer = tmp_path / 'integer.tif'
    dataset = gdal.Translate(str(integer), str(source), outputType=gdal.GDT_Int16)
    dataset = None
    geometry = {'type': 'Polygon', 'coordinates': [[[0,0],[4,0],[4,2],[2,2],[2,4],[0,4],[0,0]]]}
    with raster_workspace():
        clipped = raster_clip(raster_load(source_plan(integer)), 'EPSG:4326', geometry=geometry)
        facts = raster_info(clipped)
        assert facts['band_count'] == 2
        assert facts['bands'][0]['dtype'] == 'Float64'
        assert facts['bands'][0]['nodata_is_nan']
        expected = [values[y, x] for y in range(4) for x in range(4)
                    if (x < 2 or y >= 2) and (x, y) != (0, 0)]
        statistics = raster_statistics(clipped)
        assert (statistics['valid_count'], statistics['invalid_count'], statistics['min']) == (11, 5, 0)
        assert statistics['mean'] == pytest.approx(np.mean(expected))
        assert raster_statistics(raster_band_math(clipped, 'b1+1'))['valid_count'] == 11


def test_polygon_clip_rejects_complex_bands_before_float64_conversion(tmp_path):
    source = tmp_path / 'complex.tif'
    dataset = gdal.GetDriverByName('GTiff').Create(str(source), 4, 4, 1, gdal.GDT_CFloat64)
    dataset.SetGeoTransform((0,1,0,4,0,-1))
    crs = osr.SpatialReference(); crs.ImportFromEPSG(4326)
    dataset.SetProjection(crs.ExportToWkt())
    pixels = np.full((4,4), 1+2j, dtype=np.complex128)
    dataset.GetRasterBand(1).WriteRaster(0,0,4,4,pixels.tobytes(),buf_type=gdal.GDT_CFloat64)
    dataset = None
    with raster_workspace():
        raster = raster_load(source_plan(source))
        with pytest.raises(ValueError, match='Complex'):
            raster_clip(raster, 'EPSG:4326', geometry={'type': 'Polygon', 'coordinates': [
                [[0,0],[4,0],[4,4],[0,4],[0,0]]]})


@pytest.mark.parametrize('transform,boundary', [
    ((0,1,0,4,0,-1), {'geometry': {'type': 'Polygon', 'coordinates': [
        [[-1,-1],[5,-1],[5,5],[-1,5],[-1,-1]],
        [[-0.5,-0.5],[-0.5,4.5],[4.5,4.5],[4.5,-0.5],[-0.5,-0.5]],
    ]}}),
    ((0,1,0,4,0,-1), {'geometry': {'type': 'MultiPolygon', 'coordinates': [
        [[[-2,1],[-1,1],[-1,3],[-2,3],[-2,1]]],
        [[[5,1],[6,1],[6,3],[5,3],[5,1]]],
    ]}}),
    ((0,1,0,4,0,-1), {'geometry': {'type': 'Polygon', 'coordinates': [
        [[-1,3],[1,5],[-1,5],[-1,3]],
    ]}}),
    ((0,1,1,4,1,-1), {'bbox': [0,0,0.5,0.5]}),
    ((0,1,1,4,1,-1), {'geometry': {'type': 'Polygon', 'coordinates': [
        [[0,0],[0.5,0],[0.5,0.5],[0,0.5],[0,0]],
    ]}}),
], ids=['inside-hole', 'outside-multipolygon', 'touch-only', 'rotated-bbox', 'rotated-polygon'])
def test_clip_rejects_disjoint_footprints_even_when_envelopes_overlap(tmp_path, transform, boundary):
    source = create_raster(tmp_path / 'source.tif', np.ones((4,4)), transform=transform)
    original = source.read_bytes()
    with raster_workspace():
        raster = raster_load(source_plan(source))
        with pytest.raises(ValueError, match='intersect'):
            raster_clip(raster, 'EPSG:4326', **boundary)
    assert source.read_bytes() == original


def test_multipolygon_clip_preserves_separate_valid_islands(tmp_path):
    values = np.arange(1,17,dtype=float).reshape(4,4)
    source = create_raster(tmp_path / 'source.tif', values)
    geometry = {'type': 'MultiPolygon', 'coordinates': [
        [[[0,0],[1,0],[1,4],[0,4],[0,0]]],
        [[[3,0],[4,0],[4,4],[3,4],[3,0]]],
    ]}
    with raster_workspace():
        clipped = raster_clip(raster_load(source_plan(source)), 'EPSG:4326', geometry=geometry)
        statistics = raster_statistics(clipped)
        assert (statistics['valid_count'], statistics['invalid_count']) == (8,8)
        assert statistics['mean'] == pytest.approx(8.5)
        target = tmp_path / 'islands.tif'
        raster_save(clipped, target_plan(target), profile='cog')
        persisted = raster_load(source_plan(target))
        assert validate_cog(persisted)['valid']
        assert raster_statistics(persisted) == statistics
    dataset = gdal.Open(str(target))
    expected = values.copy(); expected[:,1:3] = np.nan
    np.testing.assert_equal(read_band_values(dataset.GetRasterBand(1)), expected)
    np.testing.assert_equal(read_band_values(dataset.GetRasterBand(2)), [[255,0,0,255]]*4)


@pytest.mark.parametrize('native_exceptions', [False,True], ids=['return-code','native-exception'])
def test_clip_rejects_failed_boundary_crs_transformation(tmp_path, native_exceptions):
    source = create_raster(tmp_path / 'mercator.tif', np.ones((4,4)), crs='EPSG:3857')
    original = ogr.GetUseExceptions()
    (ogr.UseExceptions if native_exceptions else ogr.DontUseExceptions)()
    try:
        with raster_workspace():
            raster = raster_load(source_plan(source))
            with pytest.raises(ValueError, match='transform'):
                raster_clip(raster, 'EPSG:4326', geometry={'type': 'Polygon', 'coordinates': [
                    [[0,94],[1,94],[1,95],[0,95],[0,94]],
                ]})
    finally:
        (ogr.UseExceptions if original else ogr.DontUseExceptions)()


def test_cog_save_validate_overviews_and_failed_replace(raster_file, tmp_path):
    target = tmp_path / 'result.tif'
    with raster_workspace():
        raster = raster_load(source_plan(raster_file))
        strip_file = create_raster(tmp_path / 'strips.tif', np.ones((1025, 1025)))
        assert not validate_cog(raster_load(source_plan(strip_file)))['valid']
        overview = raster_build_overviews(raster, levels=[2,4])
        assert raster_info(overview)['bands'][0]['overviews'] == [[2,2],[1,1]]
        assert raster_info(raster)['bands'][0]['overviews'] == []
        artifact = raster_save(raster, target_plan(target), profile='cog')
        assert artifact['format'] == 'tiff' and artifact['profile'] == 'cog'
        assert validate_cog(raster_load(source_plan(target)))['valid']
        original = target.read_bytes()
        with pytest.raises(ValueError): raster_save(raster, target_plan(target), profile='cog')
        with pytest.raises(ValueError): raster_save(raster, target_plan(target,'replace'), profile='cog', blocksize=1)
        assert target.read_bytes() == original
        assert set(artifact).isdisjoint({'path','access_plan','connection_info','storage_ref'})


@pytest.mark.parametrize('format,driver,suffix', [('png','PNG','.png'), ('jpeg','JPEG','.jpg')])
@pytest.mark.parametrize('source_crs', ['', 'EPSG:4326'], ids=['missing-crs','explicit-crs'])
@pytest.mark.parametrize('profile', ['geotiff','cog'])
def test_async_image_pyramid_save_keeps_pixels_overviews_and_missing_georeferencing(
        tmp_path, format, driver, suffix, source_crs, profile):
    import api_server
    height, width = 128,256
    y,x = np.indices((height,width))
    # Constant 8x8 cells let both pyramid levels select the same independent value.
    pixels = (((x//8 + y//8*7)%100)*2+25).astype(np.uint8)
    alpha = np.full((height,width),255,dtype=np.uint8)
    alpha[:16,:16] = 0
    alpha[64:80,128:144] = 128
    memory = gdal.GetDriverByName('MEM').Create('',width,height,2 if format=='png' else 1,gdal.GDT_Byte)
    memory.GetRasterBand(1).WriteRaster(0,0,width,height,pixels.tobytes(),buf_type=gdal.GDT_Byte)
    if format=='png':
        memory.GetRasterBand(2).WriteRaster(0,0,width,height,alpha.tobytes(),buf_type=gdal.GDT_Byte)
        memory.GetRasterBand(2).SetColorInterpretation(gdal.GCI_AlphaBand)
    source = tmp_path / ('source'+suffix)
    encoded = gdal.GetDriverByName(driver).CreateCopy(str(source),memory)
    encoded = memory = None
    original = source.read_bytes()
    decoded = gdal.Open(str(source))
    assert decoded.GetDriver().ShortName == driver
    assert decoded.GetGeoTransform(can_return_null=True) is None and not decoded.GetProjection()
    decoded_pixels = read_band_values(decoded.GetRasterBand(1))
    if format=='png':
        np.testing.assert_equal(decoded_pixels,pixels)
    expected = decoded_pixels*2
    if format=='png':
        expected[:16,:16] = np.nan
    # JPEG's oracle is its actual decoded pixels, without claiming lossless encoding.
    for block_y in range(0,height,8):
        for block_x in range(0,width,8):
            cell = expected[block_y:block_y+8,block_x:block_x+8]
            np.testing.assert_equal(cell,np.full((8,8),cell[0,0]))
    decoded = None
    target = tmp_path / ('workflow.'+profile+'.tif')
    workflow = {'tasks': [
        {'id':'load','operator':'raster_load','depends_on':[],
         'params':{'access_plan':source_plan(source,format),'source_crs':source_crs}},
        {'id':'math','operator':'raster_band_math','depends_on':['load'],
         'params':{'input_raster':{'$ref':'load'},'expression':'b1*2'}},
        {'id':'pyramid','operator':'raster_build_overviews','depends_on':['math'],
         'params':{'input_raster':{'$ref':'math'},'levels':[2,4],'resampling':'nearest'}},
        {'id':'save','operator':'raster_save','depends_on':['pyramid'],
         'params':{'input_raster':{'$ref':'pyramid'},'access_plan':target_plan(target),
                   'profile':profile,'blocksize':128}},
        {'id':'reload','operator':'raster_load','depends_on':['save'],
         'params':{'access_plan':source_plan(target)}},
        {'id':'statistics','operator':'raster_statistics','depends_on':['reload'],
         'params':{'input_raster':{'$ref':'reload'}}},
    ]}
    client = api_server.app.test_client()
    response = client.post('/api/workflow',json={'workflow_def':workflow,'runtime':{
        'tenant_id':7,'execution_authorization':{'id':1,'effects':['read','write']}}})
    assert response.status_code == 202, response.json
    deadline = time.monotonic()+5
    while time.monotonic()<deadline:
        status = client.get('/api/executions/'+response.json['execution_id']).json
        if status['status'] in ('success','failed'): break
        time.sleep(.01)
    assert status['status']=='success', status
    stats = json.loads(status['result'])
    valid = expected[np.isfinite(expected)]
    assert (stats['valid_count'],stats['invalid_count']) == (valid.size,width*height-valid.size)
    np.testing.assert_allclose([stats['min'],stats['max'],stats['mean'],stats['stddev']],
                               [valid.min(),valid.max(),valid.mean(),valid.std()])
    assert str(tmp_path) not in json.dumps(status) and 'addp-raster-' not in json.dumps(status)
    output = gdal.Open(str(target))
    assert output.GetGeoTransform(can_return_null=True) is None
    assert bool(output.GetProjection()) == bool(source_crs)
    if source_crs:
        assert output.GetSpatialRef().GetAuthorityCode(None)=='4326'
    assert (output.RasterXSize,output.RasterYSize,output.RasterCount)==(width,height,1)
    band = output.GetRasterBand(1)
    assert band.DataType==gdal.GDT_Float64 and np.isnan(band.GetNoDataValue())
    np.testing.assert_equal(read_band_values(band),expected)
    assert band.GetOverviewCount()==2
    for index,level in enumerate([2,4]):
        overview = band.GetOverview(index)
        assert (overview.XSize,overview.YSize)==(width//level,height//level)
        np.testing.assert_equal(read_band_values(overview),expected[::level,::level])
    output = band = None
    with raster_workspace():
        persisted = raster_load(source_plan(target))
        facts = raster_info(persisted)
        assert facts['transform']==[] and facts['extent']==[]
        assert facts['bands'][0]['overviews']==[[128,64],[64,32]]
        if profile=='cog':
            assert validate_cog(persisted)['valid']
        with pytest.raises(ValueError,match='geotransform' if source_crs else 'CRS'):
            raster_reproject(persisted,'EPSG:3857')
        with pytest.raises(ValueError,match='Source format'):
            raster_load(source_plan(source))
    if profile=='cog':
        direct_target = tmp_path / 'direct.tif'
        plan = {**source_plan(source,format),'target':target_plan(direct_target)['target']}
        direct = client.post('/api/operators/raster_to_cog/invoke',json={'params':{
            'access_plan':plan,'options':{'source_crs':source_crs,'blocksize':128}}})
        assert direct.status_code==200, direct.json
        assert direct.json['result']['band_count']==(2 if format=='png' else 1)
        direct_dataset = gdal.Open(str(direct_target))
        np.testing.assert_equal(read_band_values(direct_dataset.GetRasterBand(1)),decoded_pixels)
        if format=='png':
            np.testing.assert_equal(read_band_values(direct_dataset.GetRasterBand(2)),alpha)
        direct_dataset = None
        with raster_workspace():
            assert validate_cog(raster_load(source_plan(direct_target)))['valid']
        original_target = direct_target.read_bytes()
        plan['source']['format']='tiff'
        plan['target']['write_mode']='replace'
        rejected = client.post('/api/operators/raster_to_cog/invoke',json={'params':{'access_plan':plan}})
        assert rejected.status_code==500
        assert rejected.json['error_code']=='EXECUTION_FAILED'
        assert direct_target.read_bytes()==original_target
    assert source.read_bytes()==original
    assert set(path.name for path in tmp_path.iterdir())=={
        source.name,target.name,*(['direct.tif'] if profile=='cog' else []),
    }


def test_source_crs_is_explicit_and_conflicts_fail(tmp_path):
    path = create_raster(tmp_path / 'unknown.tif', np.ones((2,2)), crs='')
    with raster_workspace():
        raster = raster_load(source_plan(path))
        assert raster_info(raster)['crs'] == ''
        with pytest.raises(ValueError): raster_reproject(raster, 'EPSG:3857')
        assigned = raster_load(source_plan(path), 'EPSG:4326')
        assert raster_info(assigned)['extent_srid'] == 4326
        assert raster_info(raster)['crs'] == ''
        known = create_raster(tmp_path / 'known.tif', np.ones((2,2)))
        with pytest.raises(ValueError, match='conflicts'): raster_load(source_plan(known), 'EPSG:3857')


def test_rotated_extent_and_unforgeable_runtime_input(tmp_path):
    path = create_raster(tmp_path / 'rotated.tif', np.ones((2,2)), transform=(10,2,1,20,1,-2))
    with raster_workspace():
        raster = raster_load(source_plan(path))
        assert raster_info(raster)['extent'] == [10,16,16,22]
        with pytest.raises(ValueError): raster_info({'path':str(path)})
    with raster_workspace():
        with pytest.raises(ValueError): raster_info(raster)


def test_dag_persists_artifact_and_hides_temporary_paths(raster_file, tmp_path):
    target = tmp_path / 'dag.tif'
    workflow = {'tasks':[
        {'id':'load','operator':'raster_load','params':{'access_plan':source_plan(raster_file)},'depends_on':[]},
        {'id':'math','operator':'raster_band_math','params':{'input_raster':{'$ref':'load','port':'default'},'expression':'b1*2'},'depends_on':['load']},
        {'id':'save','operator':'raster_save','params':{'input_raster':{'$ref':'math'},'access_plan':target_plan(target),'profile':'cog'},'depends_on':['math']},
    ]}
    result = execute_workflow(workflow)
    assert result['status'] == 'success', result
    assert target.exists()
    assert json.loads(result['final_result'])['profile'] == 'cog'
    encoded = json.dumps(result)
    assert str(raster_file) not in encoded and 'addp-raster-' not in encoded
    workflow['tasks'][1]['params']['expression'] = 'b99'
    failed = execute_workflow(workflow)
    assert failed['status'] == 'failed'


def test_conversion_source_target_credentials_are_separate(monkeypatch, raster_file, tmp_path):
    import addp_common.workflow_access as access
    calls = []
    class Client:
        def __init__(self, endpoint): self.endpoint = endpoint
        def fget_object(self, bucket, key, path):
            calls.append(('read', self.endpoint, bucket, key)); Path(path).write_bytes(raster_file.read_bytes())
        def bucket_exists(self, bucket): return True
        def stat_object(self, bucket, key):
            class NotFound(Exception): code = 'NoSuchKey'
            raise NotFound()
        def fput_object(self, bucket, key, path, content_type):
            calls.append(('write', self.endpoint, bucket, key)); assert Path(path).exists()
    monkeypatch.setattr(access, '_object_store_client', lambda config: Client(config['endpoint']))
    plan = {**source_plan(raster_file), 'target':target_plan(tmp_path/'remote.tif')['target']}
    for label, endpoint in [('source','source:9000'),('target','target:9000')]:
        plan[label]['access'] = {'method':'object_store','endpoint':endpoint,'access_key':label+'-key','secret_key':label+'-secret','bucket':label,'object':'sample.tif'}
    result = raster_to_cog(plan)
    assert calls == [('read','source:9000','source','sample.tif'),('write','target:9000','target','sample.tif')]
    assert 'secret' not in json.dumps(result)


@pytest.mark.parametrize('input_data', ['omitted', {}, None], ids=['omitted', 'empty-object', 'reject-null'])
def test_async_api_runs_authorized_raster_dag(raster_file, input_data):
    import api_server
    workflow = {'tasks':[
        {'id':'load','operator':'raster_load','params':{'access_plan':source_plan(raster_file)},'depends_on':[]},
        {'id':'stats','operator':'raster_statistics','params':{'input_raster':{'$ref':'load'}},'depends_on':['load']},
    ]}
    client = api_server.app.test_client()
    assert client.post('/api/workflow',json={'workflow_def':workflow}).status_code == 400
    payload = {'workflow_def':workflow,'runtime':{
        'tenant_id':7,'execution_authorization':{'id':1,'effects':['read']}}}
    if input_data != 'omitted':
        payload['input_data'] = input_data
    response = client.post('/api/workflow',json=payload)
    if input_data is None:
        assert response.status_code == 400
        assert response.json['error_code'] == 'INVALID_PARAMS'
        return
    assert response.status_code == 202
    execution_id = response.json['execution_id']
    deadline = time.monotonic() + 5
    while time.monotonic() < deadline:
        status = client.get('/api/executions/'+execution_id).json
        if status['status'] in ['success','failed']: break
        time.sleep(0.01)
    assert status['status'] == 'success', status
    assert json.loads(status['result'])['valid_count'] == 15


def test_metadata_has_typed_raster_ports_and_no_old_conversion():
    specs = {item['id']:item for item in list_operators()}
    assert 'tiff_to_cog' not in specs
    assert specs['raster_load']['output_ports'][0]['type'] == 'raster'
    assert specs['raster_band_math']['parameters'][0]['type'] == 'raster'
    assert specs['raster_to_cog']['execution_modes'] == ['workflow','direct']
    assert specs['raster_save']['effects'] == ['write']


def test_concurrent_workspaces_are_isolated(raster_file):
    def run(_):
        with raster_workspace():
            raster = raster_load(source_plan(raster_file))
            return raster.workspace, raster_statistics(raster)['mean']
    with ThreadPoolExecutor(max_workers=2) as pool:
        results = list(pool.map(run, range(2)))
    assert results[0][0] != results[1][0]
    assert all(not path.exists() and mean == 9 for path, mean in results)


def test_source_snapshot_and_driver_restriction(raster_file, tmp_path):
    with raster_workspace():
        raster = raster_load(source_plan(raster_file))
        before = raster_statistics(raster)
        create_raster(raster_file, np.full((4,4), 777.0))
        assert raster_statistics(raster) == before
        # Renaming a VRT cannot turn arbitrary filesystem references into a TIFF.
        disguised = tmp_path / 'disguised.tif'
        disguised.write_text('<VRTDataset rasterXSize="1" rasterYSize="1"><VRTRasterBand dataType="Byte" band="1"/></VRTDataset>')
        with pytest.raises((ValueError, RuntimeError)): raster_load(source_plan(disguised))


@pytest.mark.parametrize('operator,params', [
    ('raster_band_math', {'expression': 'b99'}),
    ('raster_clip', {'boundary_crs': 'EPSG:4326', 'geometry': {'type': 'Polygon', 'coordinates': [
        [[-1,-1],[5,-1],[5,5],[-1,5],[-1,-1]],
        [[-0.5,-0.5],[-0.5,4.5],[4.5,4.5],[4.5,-0.5],[-0.5,-0.5]],
    ]}}),
], ids=['invalid-band', 'disjoint-clip'])
def test_failed_dag_cleans_workspace(monkeypatch, raster_file, tmp_path, operator, params):
    from operators.raster_compute import _WORKSPACE
    original = OPERATORS['raster_load']['function']
    paths = []

    def track(*args, **kwargs):
        paths.append(_WORKSPACE.get())
        return original(*args, **kwargs)

    monkeypatch.setitem(OPERATORS['raster_load'], 'function', track)
    target = tmp_path / 'failed.tif'
    original_source = raster_file.read_bytes()
    result = execute_workflow({'tasks': [
        {'id': 'load', 'operator': 'raster_load', 'depends_on': [], 'params': {'access_plan': source_plan(raster_file)}},
        {'id': 'bad', 'operator': operator, 'depends_on': ['load'], 'params': {'input_raster': {'$ref': 'load'}, **params}},
        {'id': 'save', 'operator': 'raster_save', 'depends_on': ['bad'], 'params': {
            'input_raster': {'$ref': 'bad'}, 'access_plan': target_plan(target), 'profile': 'cog',
        }},
    ]})
    assert result['status'] == 'failed'
    assert paths and all(not path.exists() for path in paths)
    assert not target.exists()
    assert raster_file.read_bytes() == original_source


def test_mosaic_preserves_empty_pixels_and_overviews_survive_save(tmp_path):
    left = create_raster(tmp_path / 'left.tif', np.ones((4, 4)))
    right = create_raster(tmp_path / 'right.tif', np.full((4, 4), 3.0), transform=(6, 1, 0, 4, 0, -1))
    with raster_workspace():
        result = raster_mosaic(raster_load(source_plan(left)), raster_load(source_plan(right)), 'EPSG:4326', [1, 1])
        stats = raster_statistics(result)
        assert stats['valid_count'] == 32 and stats['invalid_count'] == 8
        assert stats['mean'] == 2
        assert raster_info(result)['bands'][0]['dtype'] == 'Float64'
        overview = raster_build_overviews(result, levels=[2, 4])
        target = tmp_path / 'overviews.tif'
        raster_save(overview, target_plan(target))
        assert raster_info(raster_load(source_plan(target)))['bands'][0]['overviews'] == [[5, 2], [3, 1]]


def test_direct_cog_api_uses_the_same_access_plan_contract(raster_file, tmp_path):
    import api_server
    target = tmp_path / 'direct.tif'
    plan = {**source_plan(raster_file), 'target': target_plan(target)['target']}
    client = api_server.app.test_client()
    response = client.post('/api/operators/raster_to_cog/invoke', json={'params': {
        'access_plan': plan, 'options': {'source_crs': '+proj=longlat +datum=WGS84 +no_defs'},
    }})
    assert response.status_code == 200, response.json
    assert response.json['result']['profile'] == 'cog'
    assert str(tmp_path) not in json.dumps(response.json)
    with raster_workspace():
        output = raster_load(source_plan(target))
        assert validate_cog(output)['valid']
        assert raster_info(output)['extent_srid'] == 4326
    assert client.post('/api/operators/tiff_to_cog/invoke', json={'params': {}}).status_code == 404
    assert client.post('/api/operators/raster_info/invoke', json={'params': {}}).status_code == 403
    with pytest.raises(ValueError, match='object'):
        raster_to_cog(plan, options=[['compression', 'LZW']])


def test_non_epsg_authority_is_not_reported_as_epsg(monkeypatch):
    import operators.raster_compute as compute
    dataset = gdal.GetDriverByName('MEM').Create('', 2, 2, 1)
    crs = osr.SpatialReference()
    crs.SetFromUserInput('EPSG:3857')
    crs.SetAuthority(None, 'ESRI', 102100)
    dataset.SetSpatialRef(crs)
    assert dataset.GetSpatialRef().GetAuthorityName(None) == 'ESRI'
    monkeypatch.setattr(compute, '_open', lambda _: dataset)
    facts = raster_info(None)
    assert facts['extent_srid'] == 0
    assert facts['source_crs'] == dataset.GetProjection()
    assert not facts['source_crs'].startswith('EPSG:')
