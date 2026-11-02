"""The Hosted oracle's independently encoded pairs agree with the product driver."""
import importlib.util
from pathlib import Path
import numpy as np
import pytest
from osgeo import gdal,osr

ROOT=Path(__file__).resolve().parents[2]
SPEC=importlib.util.spec_from_file_location('online_envi_fixture',ROOT/'business/scripts/online-envi-fixture.py')
FIXTURE=importlib.util.module_from_spec(SPEC);SPEC.loader.exec_module(FIXTURE)

@pytest.mark.parametrize('spec',FIXTURE.sources(),ids=lambda spec:spec['case'])
def test_hosted_independent_envi_pair_complete_values_and_grid(tmp_path,spec):
    for name,raw in FIXTURE.pair(spec).items():(tmp_path/name).write_bytes(raw)
    dataset=gdal.OpenEx(str(tmp_path/spec['name']),gdal.OF_RASTER,allowed_drivers=['ENVI'])
    assert dataset is not None
    assert dataset.RasterXSize==dataset.RasterYSize==256 and dataset.RasterCount==3
    assert set(map(Path,dataset.GetFileList()))=={tmp_path/spec['name'],tmp_path/spec['header']}
    assert np.array_equal(dataset.ReadAsArray(),np.asarray(FIXTURE.pixels(),dtype='uint8'))
    for band in range(1,4):assert np.all(dataset.GetRasterBand(band).GetMaskBand().ReadAsArray()==255)
    if spec['source_srid'] is None:
        assert dataset.GetSpatialRef() is None and dataset.GetGeoTransform(can_return_null=True) is None
    else:
        expected=osr.SpatialReference();expected.ImportFromEPSG(spec['source_srid']);expected.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER)
        assert dataset.GetSpatialRef().IsSame(expected), (dataset.GetSpatialRef().ExportToWkt(),expected.ExportToWkt(),dataset.GetSpatialRef().GetDataAxisToSRSAxisMapping(),expected.GetDataAxisToSRSAxisMapping())
        assert dataset.GetSpatialRef().GetAuthorityCode(None)==str(spec['source_srid'])
        assert dataset.GetGeoTransform()==((113.,.0001,0.,28.,0.,-.0001) if spec['source_srid']==4326 else (500000.,10.,0.,3100000.,0.,-10.))
