"""One GDAL encoder for ordinary, self-contained PNG/JPEG workflow outputs."""
from pathlib import Path

from osgeo import gdal


_MESSAGES = {
    'type': ('图片导出不支持该位深；PNG 仅支持 Byte/UInt16，JPEG 仅支持 Byte',
             'Unsupported image bit depth; PNG requires Byte/UInt16 and JPEG requires Byte'),
    'color': ('图片导出需要明确的灰度、RGB 或 PNG alpha 波段，不自动展开色表或选择波段',
              'Image export requires grayscale, RGB or PNG alpha bands; palettes and bands are not selected automatically'),
    'quality': ('JPEG 质量必须为 1–100 的整数', 'JPEG quality must be an integer from 1 to 100'),
    'invalid': ('JPEG 不支持透明或无效像元，请先显式处理或选择 PNG',
                'JPEG cannot represent transparent or invalid pixels; handle them explicitly or choose PNG'),
    'target': ('图片目标名称必须与 PNG 或 JPEG 编码匹配', 'Image target name must match PNG or JPEG encoding'),
    'encoding': ('图片编码或完整回读失败，目标未写入', 'Image encoding or complete read-back failed; target was not written'),
    'fidelity': ('PNG 编码未能完整保留像元、位深或有效覆盖，目标未写入',
                 'PNG did not preserve pixels, bit depth or validity; target was not written'),
    'structure': ('普通图片必须为单文件且不包含 GIS 定位或旁文件，目标未写入',
                  'An ordinary image must be one file without GIS positioning or sidecars; target was not written'),
}


def image_export_error(reason, language='zh-cn'):
    return ValueError(_MESSAGES[reason][1 if language == 'en' else 0])


def _windows(dataset):
    for y in range(0, dataset.RasterYSize, 512):
        for x in range(0, dataset.RasterXSize, 512):
            yield (x, y, min(512, dataset.RasterXSize-x), min(512, dataset.RasterYSize-y))


def encode_image(source, directory, profile, jpeg_quality, language='zh-cn'):
    """Validate, encode and fully reopen before the caller may publish any bytes."""
    def require(condition, reason):
        if not condition:
            raise image_export_error(reason, language)

    require(profile in ('png', 'jpeg'), 'target')
    require(type(jpeg_quality) is int and 1 <= jpeg_quality <= 100, 'quality')
    bands = [source.GetRasterBand(i) for i in range(1, source.RasterCount + 1)]
    types = {gdal.GDT_Byte, gdal.GDT_UInt16} if profile == 'png' else {gdal.GDT_Byte}
    require(bool(bands) and len({b.DataType for b in bands}) == 1 and all(b.DataType in types for b in bands), 'type')
    roles = [b.GetColorInterpretation() for b in bands]
    if len(bands) == 1 and roles == [gdal.GCI_Undefined]:
        roles = [gdal.GCI_GrayIndex]
    allowed = [[gdal.GCI_GrayIndex], [gdal.GCI_RedBand, gdal.GCI_GreenBand, gdal.GCI_BlueBand]]
    if profile == 'png':
        allowed += [[gdal.GCI_GrayIndex, gdal.GCI_AlphaBand],
                    [gdal.GCI_RedBand, gdal.GCI_GreenBand, gdal.GCI_BlueBand, gdal.GCI_AlphaBand]]
    require(roles in allowed and all(b.GetColorTable() is None for b in bands), 'color')
    if profile == 'jpeg':
        for band in bands:
            for window in _windows(source):
                mask = band.GetMaskBand().ReadRaster(*window)
                require(mask is not None and mask == b'\xff' * (window[2] * window[3]), 'invalid')
    directory = Path(directory)
    directory.mkdir()
    path = directory / ('image.png' if profile == 'png' else 'image.jpg')
    driver = 'PNG' if profile == 'png' else 'JPEG'
    options = ['WORLDFILE=NO'] + (['WRITE_METADATA_AS_TEXT=NO'] if profile == 'png' else
        ['QUALITY=' + str(jpeg_quality), 'WRITE_EXIF_METADATA=NO', 'INTERNAL_MASK=NO'])
    try:
        with gdal.config_option('GDAL_PAM_ENABLED', 'NO', thread_local=True):
            encoded = gdal.Translate(str(path), source, options=gdal.TranslateOptions(
                format=driver, overviewLevel='NONE', creationOptions=options))
            require(encoded is not None, 'encoding')
            encoded.FlushCache()
            encoded = None
            output = gdal.OpenEx(str(path), gdal.OF_RASTER, allowed_drivers=[driver])
            require(output is not None, 'encoding')
            require({p.resolve() for p in directory.iterdir()} == {path.resolve()}, 'structure')
            require({Path(p).resolve() for p in output.GetFileList() or []} == {path.resolve()}, 'structure')
            require(not output.GetProjection() and output.GetGeoTransform(can_return_null=True) is None
                    and output.GetGCPCount() == 0 and not output.GetMetadata('RPC')
                    and not output.GetMetadata('GEOLOCATION') and not output.GetMetadata('EXIF'), 'structure')
            require((output.RasterXSize, output.RasterYSize, output.RasterCount) ==
                    (source.RasterXSize, source.RasterYSize, source.RasterCount), 'encoding')
            for index, original in enumerate(bands, 1):
                result = output.GetRasterBand(index)
                require(result.DataType == original.DataType and result.GetColorInterpretation() == roles[index-1], 'fidelity')
                for window in _windows(source):
                    pixels = result.ReadRaster(*window)
                    mask = result.GetMaskBand().ReadRaster(*window)
                    require(pixels is not None and mask is not None, 'encoding')
                    if profile == 'png':
                        require(pixels == original.ReadRaster(*window)
                                and mask == original.GetMaskBand().ReadRaster(*window), 'fidelity')
                    else:
                        require(mask == b'\xff' * (window[2] * window[3]), 'invalid')
            output = None
    except RuntimeError as exc:
        raise image_export_error('encoding', language) from exc
    return path
