"""Ordinary image exports: real codecs, complete pixels and no GIS sidecars."""
import hashlib
from pathlib import Path

import numpy as np
import pytest
from osgeo import gdal, osr

from operators import list_operators
from operators import raster_compute as compute
from test_raster_compute import source_plan, target_plan


def image_source(path, channels=3, dtype=gdal.GDT_Byte, *, located=True):
    ds = gdal.GetDriverByName('GTiff').Create(str(path), 37, 23, channels, dtype)
    if located:
        crs = osr.SpatialReference(); crs.ImportFromEPSG(32650)
        ds.SetSpatialRef(crs)
        ds.SetGeoTransform((500000., 2., .25, 3100000., .5, -2.))
    ds.SetMetadata({'EXIF_GPSLatitude': '28 0 0', 'EXIF_Orientation': '6'})
    roles = {1: [gdal.GCI_GrayIndex], 2: [gdal.GCI_GrayIndex, gdal.GCI_AlphaBand],
             3: [gdal.GCI_RedBand, gdal.GCI_GreenBand, gdal.GCI_BlueBand],
             4: [gdal.GCI_RedBand, gdal.GCI_GreenBand, gdal.GCI_BlueBand, gdal.GCI_AlphaBand]}[channels]
    rows, cols = np.indices((23, 37))
    scale = 200 if dtype == gdal.GDT_UInt16 else 1
    for i, role in enumerate(roles, 1):
        values = (30 * i + rows + cols) * scale
        if role == gdal.GCI_AlphaBand:
            values = ((rows + cols) % 3) * (32767 if dtype == gdal.GDT_UInt16 else 127)
        band = ds.GetRasterBand(i)
        band.SetColorInterpretation(role)
        band.WriteArray(values)
    ds = None
    return path


def image_plan(path, mode='create', profile='png'):
    plan = target_plan(path, mode)
    plan['target']['format'] = profile
    return plan


@pytest.mark.parametrize('channels', [1, 2, 3, 4])
@pytest.mark.parametrize('dtype', [gdal.GDT_Byte, gdal.GDT_UInt16])
@pytest.mark.parametrize('located', [False, True])
def test_image_export_png_complete_native_roundtrip(tmp_path, channels, dtype, located):
    path = image_source(tmp_path / 'source.tif', channels, dtype, located=located)
    before = hashlib.sha256(path.read_bytes()).hexdigest()
    target = tmp_path / 'ordinary.PNG'
    with compute.raster_workspace():
        artifact = compute.raster_save(compute.raster_load(source_plan(path)), image_plan(target), profile='png')
    original = gdal.Open(str(path)); result = gdal.OpenEx(str(target), gdal.OF_RASTER, allowed_drivers=['PNG'])
    assert (artifact['artifact_type'], artifact['format'], artifact['profile']) == ('raster', 'png', 'png')
    assert artifact['lossy'] is False and 'jpeg_quality' not in artifact
    assert artifact['crs'] == '' and artifact['source_crs'] == '' and artifact['extent_srid'] == 0
    assert artifact['transform'] == [] and artifact['extent'] == []
    assert result.GetGeoTransform(can_return_null=True) is None and result.GetProjection() == ''
    assert result.GetGCPCount() == 0 and result.GetMetadata('RPC') == {}
    assert result.GetFileList() == [str(target)]
    assert result.ReadRaster() == original.ReadRaster()
    for i in range(1, channels + 1):
        a, b = original.GetRasterBand(i), result.GetRasterBand(i)
        assert a.DataType == b.DataType
        assert a.GetColorInterpretation() == b.GetColorInterpretation()
        assert a.GetMaskBand().ReadRaster() == b.GetMaskBand().ReadRaster()
    result = original = a = b = None
    assert hashlib.sha256(path.read_bytes()).hexdigest() == before
    assert {p.name for p in tmp_path.iterdir()} == {'source.tif', 'ordinary.PNG'}


@pytest.mark.parametrize('channels', [1, 3])
@pytest.mark.parametrize('suffix', ['.jpg', '.jpeg', '.JPEG'])
@pytest.mark.parametrize('quality', [35, 90, 100])
def test_image_export_jpeg_real_lossy_encoding(tmp_path, channels, suffix, quality):
    path = image_source(tmp_path / 'source.tif', channels)
    before = hashlib.sha256(path.read_bytes()).hexdigest()
    target = tmp_path / ('ordinary' + suffix)
    with compute.raster_workspace():
        artifact = compute.raster_save(compute.raster_load(source_plan(path)),
            image_plan(target, profile='jpeg'), profile='jpeg', jpeg_quality=quality)
    original = gdal.Open(str(path)); result = gdal.OpenEx(str(target), gdal.OF_RASTER, allowed_drivers=['JPEG'])
    assert artifact['format'] == 'jpeg' and artifact['lossy'] is True and artifact['jpeg_quality'] == quality
    assert result.GetGeoTransform(can_return_null=True) is None and result.GetProjection() == ''
    assert result.GetMetadata('EXIF') == {} and result.GetGCPCount() == 0
    assert result.GetFileList() == [str(target)] and result.RasterCount == channels
    difference = np.abs(original.ReadAsArray().astype(float) - result.ReadAsArray().astype(float))
    # Independent smooth fixture; a black/rotated/channel-swapped output must fail.
    assert float(np.mean(difference)) < 5 and float(np.max(difference)) < 15
    assert hashlib.sha256(path.read_bytes()).hexdigest() == before
    assert {p.name for p in tmp_path.iterdir()} == {'source.tif', target.name}


@pytest.mark.parametrize('profile,channels,dtype', [('jpeg', 2, gdal.GDT_Byte), ('jpeg', 4, gdal.GDT_Byte),
    ('jpeg', 3, gdal.GDT_UInt16), ('png', 1, gdal.GDT_Float32), ('png', 3, gdal.GDT_Float64)])
def test_image_export_rejects_silent_reduction_and_alpha(tmp_path, profile, channels, dtype):
    path = image_source(tmp_path / 'source.tif', channels, dtype)
    target = tmp_path / ('ordinary.' + ('png' if profile == 'png' else 'jpg'))
    target.write_bytes(b'existing output')
    with compute.raster_workspace(), pytest.raises(ValueError):
        compute.raster_save(compute.raster_load(source_plan(path)), image_plan(target, 'replace', profile), profile=profile)
    assert target.read_bytes() == b'existing output'


@pytest.mark.parametrize('quality', [True, 0, 101, 90.5, '90'])
def test_image_export_rejects_invalid_jpeg_quality(tmp_path, quality):
    path = image_source(tmp_path / 'source.tif')
    with compute.raster_workspace(), pytest.raises(ValueError):
        compute.raster_save(compute.raster_load(source_plan(path)), image_plan(tmp_path/'out.jpg', profile='jpeg'),
                            profile='jpeg', jpeg_quality=quality)
    assert not (tmp_path/'out.jpg').exists()


@pytest.mark.parametrize('damage', ['pixel', 'sidecar', 'wrong_codec'])
def test_image_export_png_failed_validation_never_replaces(tmp_path, monkeypatch, damage):
    path = image_source(tmp_path / 'source.tif')
    target = tmp_path / 'out.png'; target.write_bytes(b'existing output')
    translate = gdal.Translate

    def corrupt(destination, source, **kwargs):
        if damage == 'wrong_codec':
            result = translate(destination, source, format='GTiff')
        elif damage == 'pixel':
            changed = gdal.GetDriverByName('MEM').CreateCopy('', source)
            changed.GetRasterBand(1).WriteArray(np.array([[222]], dtype=np.uint8), 2, 3)
            result = translate(destination, changed, **kwargs)
        else:
            result = translate(destination, source, **kwargs)
            Path(str(destination)+'.aux.xml').write_text('<PAMDataset/>')
        return result

    monkeypatch.setattr(gdal, 'Translate', corrupt)
    with compute.raster_workspace(), pytest.raises(ValueError):
        compute.raster_save(compute.raster_load(source_plan(path)), image_plan(target, 'replace'), profile='png')
    assert target.read_bytes() == b'existing output'


def test_image_export_create_collision_and_replace(tmp_path):
    path = image_source(tmp_path / 'source.tif')
    target = tmp_path / 'out.png'; target.write_bytes(b'existing output')
    with compute.raster_workspace():
        loaded = compute.raster_load(source_plan(path))
        with pytest.raises(ValueError): compute.raster_save(loaded, image_plan(target), profile='png')
        assert target.read_bytes() == b'existing output'
        compute.raster_save(loaded, image_plan(target, 'replace'), profile='png')
    assert target.read_bytes().startswith(b'\x89PNG\r\n\x1a\n')


def test_image_export_metadata_does_not_expand_tiles_or_cog():
    specs = {item['id']: item for item in list_operators()}
    params = {p['name']: p for p in specs['raster_save']['parameters']}
    assert params['profile']['enum'] == ['geotiff', 'cog', 'png', 'jpeg']
    assert params['jpeg_quality']['default'] == 90
    assert params['jpeg_quality']['show_when'] == {'profile': 'jpeg'}
    tiles = {p['name']:p for p in specs['raster_save_tiles']['parameters']}
    assert tiles['profile']['enum'] == ['geotiff', 'cog']


def test_image_export_png_preserves_gray_transparent_nodata(tmp_path):
    path = image_source(tmp_path/'source.tif', 1)
    ds = gdal.Open(str(path), gdal.GA_Update)
    ds.GetRasterBand(1).SetNoDataValue(30)
    ds = None
    target = tmp_path/'out.png'
    with compute.raster_workspace():
        compute.raster_save(compute.raster_load(source_plan(path)), image_plan(target), profile='png')
    ds = gdal.Open(str(target))
    assert ds.GetRasterBand(1).GetMaskBand().ReadAsArray()[0, 0] == 0
    assert ds.GetRasterBand(1).GetMaskBand().ReadAsArray()[1, 1] == 255


@pytest.mark.parametrize('language,message', [('zh-cn','无效像元'), ('en','invalid pixels')])
def test_image_export_jpeg_rejects_invalid_coverage_bilingually(tmp_path, language, message):
    path = image_source(tmp_path/'source.tif', 1)
    ds = gdal.Open(str(path), gdal.GA_Update)
    ds.GetRasterBand(1).SetNoDataValue(30)
    ds = None
    with compute.raster_workspace(), compute.raster_error_language(language), pytest.raises(ValueError, match=message):
        compute.raster_save(compute.raster_load(source_plan(path)), image_plan(tmp_path/'out.jpg', profile='jpeg'), profile='jpeg')
    assert not (tmp_path/'out.jpg').exists()


@pytest.mark.parametrize('profile,name,declared', [('png','bad.jpg','png'), ('png','bad.png','jpeg'),
    ('jpeg','bad.png','jpeg'), ('cog','bad.png','png'), ('geotiff','bad.jpg','jpeg')])
def test_image_export_rejects_wrong_target_identity(tmp_path, profile, name, declared):
    path = image_source(tmp_path/'source.tif')
    target = tmp_path/name; target.write_bytes(b'existing output')
    with compute.raster_workspace(), pytest.raises(ValueError):
        compute.raster_save(compute.raster_load(source_plan(path)), image_plan(target,'replace',declared),profile=profile)
    assert target.read_bytes() == b'existing output'


@pytest.mark.parametrize('profile,invalid', [('png',False), ('jpeg',False), ('jpeg',True)])
def test_image_export_async_real_dag_and_workspace_cleanup(tmp_path, monkeypatch, profile, invalid):
    import json
    import time
    import api_server
    import workflow_engine
    path = image_source(tmp_path/'source.tif', 4 if invalid else 3)
    before = path.read_bytes()
    target = tmp_path/('out.png' if profile=='png' else 'out.jpg')
    definition = {'tasks':[
        {'id':'load','operator':'raster_load','depends_on':[], 'params':{'access_plan':source_plan(path)}},
        {'id':'save','operator':'raster_save','depends_on':['load'], 'params':{
            'input_raster':{'$ref':'load'},'access_plan':image_plan(target, profile=profile), 'profile':profile}}]}
    workspaces=[]; original=workflow_engine._execute_operator
    def observe(name, params):
        if name=='raster_save':workspaces.append(params['input_raster'].workspace)
        return original(name,params)
    monkeypatch.setattr(workflow_engine,'_execute_operator',observe)
    client=api_server.app.test_client()
    receipt=client.post('/api/workflow',json={'workflow_def':definition,'input_data':{},
        'runtime':{'tenant_id':7,'execution_authorization':{'id':1,'effects':['read','write']}}})
    assert receipt.status_code==202,receipt.json
    deadline=time.monotonic()+10
    while time.monotonic()<deadline:
        status=client.get('/api/executions/'+receipt.json['execution_id']).json
        if status['status'] in ('success','failed'):break
        time.sleep(.01)
    assert status['status']==('failed' if invalid else 'success'),status
    assert workspaces and all(not p.exists() for p in workspaces)
    assert path.read_bytes()==before
    if invalid:assert not target.exists()
    else:
        artifact=json.loads(status['result'])
        assert artifact['format']==profile and artifact['size_bytes']==target.stat().st_size>0


@pytest.mark.parametrize('profile', ['png', 'jpeg'])
@pytest.mark.parametrize('fault', [None, 'pixels', 'position'])
def test_hosted_independent_image_oracle_reads_real_files(tmp_path, profile, fault):
    from test_online_raster_fixture import fixture
    source = tmp_path/'source.tif'
    fixture.write_ordinary_image_source(source, profile)
    dataset = gdal.Open(str(source), gdal.GA_Update)
    if fault == 'pixels':
        dataset.GetRasterBand(1).Fill(255)
    target = tmp_path/('ordinary.png' if profile == 'png' else 'ordinary.jpg')
    with gdal.config_option('GDAL_PAM_ENABLED', 'NO', thread_local=True):
        output = gdal.Translate(str(target), dataset, format='PNG' if profile == 'png' else 'JPEG',
                                creationOptions=[] if profile == 'png' else ['QUALITY=85'])
        output = None
    dataset = None
    if fault == 'position':
        target.with_suffix('.wld').write_text('2\n0\n0\n-2\n500001\n3099999\n')
    if fault:
        with pytest.raises(fixture.FixtureError):
            fixture.verify_ordinary_image(target, profile)
    else:
        facts = fixture.verify_ordinary_image(target, profile)
        assert facts['size_bytes'] == target.stat().st_size > 0
        assert facts['georeferencing_absent'] and facts['sidecars_absent']
        assert facts['lossy'] is (profile == 'jpeg')
        assert len(facts['decoded_sha256']) == 64
