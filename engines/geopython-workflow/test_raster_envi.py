"""Independent ENVI bytes: layout, endianness, masks, grid and COG readback."""
import hashlib
from pathlib import Path

import numpy as np
import pytest
from osgeo import gdal, osr

from operators import raster_compute as compute
from operators.envi import ENVIProfileError, inspect_envi, parse_header
from test_raster_compute import source_plan


def fixture(tmp_path, interleave='bsq', byte_order=0, dtype='int16', located=True, missing=None):
    specifications = {'uint8': (1, 255), 'int16': (2, -999), 'int32': (3, -999), 'float32': (4, -999),
                      'float64': (5, -999), 'uint16': (12, 65535), 'uint32': (13, 4294967295)}
    code, nodata = specifications[dtype]
    if missing is not None:
        nodata = missing
    values = (np.arange(30).reshape(2, 3, 5) * 2 + 1).astype(dtype)
    if dtype.startswith('float'):
        values += .25
    values[0, 1, 2] = nodata
    shape = {'bsq': values, 'bil': values.transpose(1, 0, 2), 'bip': values.transpose(1, 2, 0)}[interleave]
    raster, header = tmp_path / ' 原名 %.img', tmp_path / ' 原名 %.HDR'
    raster.write_bytes(bytes(16) + shape.astype(np.dtype(dtype).newbyteorder('>' if byte_order else '<')).tobytes())
    text = ('ENVI\nsamples = 5\nlines = 3\nbands = 2\nheader offset = 16\nfile type = ENVI Standard\n'
            f'data type = {code}\ninterleave = {interleave}\nbyte order = {byte_order}\ndata ignore value = {nodata}\n'
            'band names = {red,infrared}\nwavelength = {600,850}\nwavelength units = Nanometers\n')
    if located:
        text += 'map info = {UTM,1,1,500000,3000000,10,20,50,North,WGS-84}\n'
    header.write_text(text, encoding='utf-8')
    plan = source_plan(raster, 'envi')
    plan['source']['related_files'] = [{'role': 'header', 'access': {'method': 'mounted_path', 'path': str(header)}}]
    plan['source']['metadata'] = {'expected_header_sha256': hashlib.sha256(header.read_bytes()).hexdigest()}
    return raster, header, plan, values, nodata


@pytest.mark.parametrize('interleave', ['bsq', 'bil', 'bip'])
@pytest.mark.parametrize('byte_order', [0, 1])
@pytest.mark.parametrize('dtype', ['uint8', 'int16', 'int32', 'float32', 'float64', 'uint16', 'uint32'])
def test_envi_independent_pixels_masks_grid_and_cog(tmp_path, interleave, byte_order, dtype):
    raster, header, plan, expected, nodata = fixture(tmp_path, interleave, byte_order, dtype)
    before = {p.name: p.read_bytes() for p in (raster, header)}
    description = compute.raster_inspect(plan)['description']
    assert description['media']['encoding'] == 'ENVI'
    assert description['media']['size_bytes'] == sum(len(raw) for raw in before.values())
    assert description['format_info']['header_sha256'] == hashlib.sha256(before[header.name]).hexdigest()
    assert description['format_info']['composition_complete'] is True
    assert description['spatial']['extent'] == [500000., 2999940., 500050., 3000000.]
    reference = osr.SpatialReference()
    reference.ImportFromEPSG(32650)
    spatial = description['spatial']
    if 'srid' in spatial:
        assert spatial['srid'] == 32650
    else:
        assert spatial['crs_ref'] == spatial['crs_definitions'][0]['id']
        declared = osr.SpatialReference()
        declared.ImportFromWkt(spatial['crs_definitions'][0]['definition'])
        assert declared.IsSame(reference)
    with compute.raster_workspace():
        loaded = compute.raster_load(plan)
        native = compute._open(loaded)
        np.testing.assert_array_equal(native.ReadAsArray(), expected)
        assert native.GetGeoTransform() == (500000., 10., 0., 3000000., 0., -20.)
        for band in (1, 2):
            np.testing.assert_array_equal(native.GetRasterBand(band).GetMaskBand().ReadAsArray() != 0, expected[band-1] != nodata)
        assert set(p.name for p in loaded.path.parent.iterdir()) == {raster.name, header.name}
    target = tmp_path / 'result.tif'
    full = {**plan, 'target': {'kind': 'file', 'format': 'tiff', 'name': target.name, 'write_mode': 'create',
                             'access': {'method': 'mounted_path', 'path': str(target)}}}
    compute.raster_to_cog(full)
    output = gdal.OpenEx(str(target), gdal.OF_RASTER, allowed_drivers=['GTiff'])
    np.testing.assert_array_equal(output.ReadAsArray(), expected)
    assert output.GetSpatialRef().IsSame(reference)
    assert output.GetGeoTransform() == (500000., 10., 0., 3000000., 0., -20.)
    for band in (1, 2):
        np.testing.assert_array_equal(output.GetRasterBand(band).GetMaskBand().ReadAsArray() != 0, expected[band-1] != nodata)
    output = None
    with compute.raster_workspace():
        assert compute.validate_cog(compute.raster_load(source_plan(target)))['valid']
    assert {p.name: p.read_bytes() for p in (raster, header)} == before


def test_envi_nan_mask_unknown_location_and_header_version(tmp_path):
    raster, header, plan, expected, _ = fixture(tmp_path, dtype='float64', located=False, missing=float('nan'))
    assert compute.raster_inspect(plan)['description']['spatial'] is None
    with compute.raster_workspace():
        native = compute._open(compute.raster_load(plan))
        np.testing.assert_array_equal(native.ReadAsArray(), expected)
        np.testing.assert_array_equal(native.GetRasterBand(1).GetMaskBand().ReadAsArray() != 0, ~np.isnan(expected[0]))
    header.write_text(header.read_text() + 'description = {changed}\n')
    with compute.raster_workspace(), pytest.raises(ENVIProfileError) as failure:
        compute.raster_load(plan)
    assert failure.value.reason == 'header_version'


@pytest.mark.parametrize('fault', ['external', 'duplicate', 'complex', 'truncated', 'trailing', 'crs resource', 'scalar block', 'unknown'])
def test_envi_rejects_unsupported_composition_before_gdal(tmp_path, monkeypatch, fault):
    raster, header, plan, _, _ = fixture(tmp_path)
    text = header.read_text()
    if fault == 'external':
        text += 'dem file = ../private.img\n'
    elif fault == 'duplicate':
        text += 'Samples = 5\n'
    elif fault == 'complex':
        text = text.replace('data type = 2', 'data type = 6')
    elif fault == 'truncated':
        raster.write_bytes(raster.read_bytes()[:-1])
    elif fault == 'trailing':
        raster.write_bytes(raster.read_bytes() + b'x')
    elif fault == 'crs resource':
        text += 'coordinate system string = {GEOGCS["x",EXTENSION["PROJ4","+nadgrids=/private/file"]]}\n'
    elif fault == 'scalar block':
        text = text.replace('samples = 5', 'samples = {5}')
    else:
        text += 'unexplained resource = image.bin\n'
    header.write_text(text)
    monkeypatch.setattr(compute.gdal, 'OpenEx', lambda *a, **kw: pytest.fail('unproven ENVI reached native open'))
    with compute.raster_workspace(), pytest.raises(ENVIProfileError):
        compute.raster_load(plan)


def test_envi_private_snapshot_does_not_read_pam_stats_or_overview(tmp_path):
    raster, header, plan, expected, _ = fixture(tmp_path, located=False)
    (tmp_path / (raster.name + '.aux.xml')).write_text('<PAMDataset><SRS>EPSG:4326</SRS></PAMDataset>')
    (tmp_path / (raster.name + '.ovr')).write_bytes(b'not selected')
    header.with_suffix('.sta').write_bytes(b'not selected')
    description = compute.raster_inspect(plan)['description']
    assert description['spatial'] is None
    with compute.raster_workspace():
        loaded = compute.raster_load(plan)
        np.testing.assert_array_equal(compute._open(loaded).ReadAsArray(), expected)
        assert {p.name for p in loaded.path.parent.iterdir()} == {raster.name, header.name}
