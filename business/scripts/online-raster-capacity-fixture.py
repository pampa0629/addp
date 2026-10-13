#!/usr/bin/env python3
"""Business-owned complete public raster and disposable disk-backed S3 fixtures."""
from __future__ import annotations
import concurrent.futures
import hashlib
import importlib.util
import json
import math
import os
from pathlib import Path
import re
import secrets
import shutil
import sys
import tempfile
import urllib.request

_spec = importlib.util.spec_from_file_location('capacity_shared_fixture', Path(__file__).with_name('online-raster-minio-fixture.py'))
shared = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(shared)
SUITE = 'raster-workflow-capacity'
CONTAINERS = tuple('addp-raster-capacity-' + role for role in ('source', 'target', 'worker'))
SOURCE_NAME = 'capacity-source.tif'
ITEM_ID = 'S2A_17SPV_20220304_0_L2A'
PUBLIC_ROOT = 'https://sentinel-cogs.s3.us-west-2.amazonaws.com/sentinel-s2-l2a-cogs/17/S/PV/2022/3/' + ITEM_ID
ASSETS = (
 ('B02',218372114,'92f9961d2f5b3053fababedee482d70b3b98b9e6d4001ffe4367800b4f80fd65'),
 ('B03',223362776,'5b8a9bb93173d2feaa42c0d1a165c30936b6fd29850574ba2de5305089099f7d'),
 ('B04',231120537,'f0eaad8f9cf8ed4f54bdb52a73e14848397998985be3ad5686e8ce89aeff1fa0'),
 ('B08',247492987,'840e7a4de70a653b823df4aa106178e80b4c248e0da7e67101ca94875351fb1a'))
SIZE = 10980
TRANSFORM = (600000,10,0,4000020,0,-10)
TARGET_PATTERN = re.compile(r'capacity-c(64|128|256)-n[12]-r[123]-[01]\.cog\.tif')


def digest(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream,'sha256').hexdigest()


def names_checked(names):
    if (not isinstance(names,list) or not 1 <= len(names) <= 2 or any(not isinstance(name,str) or not TARGET_PATTERN.fullmatch(name) for name in names)
        or len(set(names)) != len(names)):
        raise shared.FixtureError('invalid capacity target names')
    return names


def stop():
    errors=[]
    for name in reversed(CONTAINERS):
        try:
            if shared.owned(name,SUITE): shared.command(['docker','rm','-fv',name])
            if shared.owned(name,SUITE): raise shared.FixtureError('capacity fixture cleanup residual')
        except Exception as error: errors.append(type(error).__name__)
    if errors: raise shared.FixtureError('capacity fixture cleanup failed: '+','.join(errors))


def start(root):
    if any(shared.owned(name,SUITE) for name in CONTAINERS):
        raise shared.FixtureError('refusing existing capacity fixtures')
    config={role:{'endpoint':f'127.0.0.1:{59012+i}', 'bucket':'raster-'+role,
                  'access_key':'capacity-'+role,'secret_key':secrets.token_hex(32)}
            for i,role in enumerate(('source','target'))}
    config['target']['writer']={'access_key':'capacity-writer','secret_key':secrets.token_hex(32)}
    shared.private_json(root/'capacity-fixture.json',config)
    try:
        for i,role in enumerate(('source','target')):
            entry=config[role]
            shared.start_minio(root,entry,CONTAINERS[i],SUITE,data_directory=root/('data-'+role))
            connection={'endpoint':entry['endpoint'], 'use_ssl':False,
                        **(entry['writer'] if role=='target' else {key:entry[key] for key in ('access_key','secret_key')})}
            shared.private_json(root/f'raster-{role}-engine.json',{
                'name':f'Hosted Raster Capacity {role} {os.environ["ADDP_ONLINE_TEST_RUN_ID"]}',
                'description':'Disposable full-scene capacity fixture', 'engine_type':'minio',
                'engine_origin':'general','connection_info':connection})
    except BaseException:
        stop(); raise


def download_asset(root,asset):
    band,size,sha=asset; path=root/(band+'.tif'); url=PUBLIC_ROOT+'/'+band+'.tif'
    with urllib.request.urlopen(url,timeout=90) as response, path.open('wb') as stream:
        if int(response.headers['Content-Length']) != size:
            raise shared.FixtureError('public asset length changed')
        while block:=response.read(1<<20): stream.write(block)
    if path.stat().st_size!=size or digest(path)!=sha:
        raise shared.FixtureError('public asset checksum changed')
    return {'band':band,'href':url,'size_bytes':size,'sha256':sha}


def require_disk(root):
    if shutil.disk_usage(root).free < 6 * (1 << 30):
        raise shared.FixtureError('capacity requires at least 6 GiB free disk space')


def prepare_source(root):
    import numpy as np
    from osgeo import gdal,osr
    gdal.UseExceptions(); gdal.SetCacheMax(64<<20)
    require_disk(root)
    data=root/'public-source'; data.mkdir(mode=0o700)
    with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
        assets=list(pool.map(lambda asset:download_asset(data,asset),ASSETS))
    path=data/SOURCE_NAME
    output=gdal.GetDriverByName('GTiff').Create(str(path),SIZE,SIZE,4,gdal.GDT_UInt16,
        ['TILED=YES','BLOCKXSIZE=512','BLOCKYSIZE=512','INTERLEAVE=BAND','COMPRESS=DEFLATE','PREDICTOR=2','BIGTIFF=YES'])
    projection=osr.SpatialReference(); projection.ImportFromEPSG(32617)
    output.SetGeoTransform(TRANSFORM); output.SetProjection(projection.ExportToWkt())
    counts=[]
    for index,(band,_,_) in enumerate(ASSETS,1):
        source=gdal.Open(str(data/(band+'.tif'))); source_band=source.GetRasterBand(1)
        if ((source.RasterXSize,source.RasterYSize,source.RasterCount)!=(SIZE,SIZE,1)
            or source.GetGeoTransform()!=TRANSFORM or not source.GetSpatialRef().IsSame(projection)
            or source_band.DataType!=gdal.GDT_UInt16 or source_band.GetNoDataValue()!=0):
            raise shared.FixtureError('public source grid/type/NoData changed')
        target=output.GetRasterBand(index); target.SetNoDataValue(0); target.SetDescription(band)
        target.SetColorInterpretation(gdal.GCI_GrayIndex if index==1 else gdal.GCI_Undefined)
        if source_band.GetScale() is not None: target.SetScale(source_band.GetScale())
        if source_band.GetOffset() is not None: target.SetOffset(source_band.GetOffset())
        valid=0
        for y in range(0,SIZE,64):
            values=source_band.ReadAsArray(0,y,SIZE,min(64,SIZE-y))
            valid+=int(np.count_nonzero(values)); target.WriteArray(values,0,y)
        counts.append(valid); target=source_band=source=None
    output=None
    combined=gdal.Open(str(path))
    if combined.GetFileList()!=[str(path)]: raise shared.FixtureError('capacity source has sidecars')
    for index,(band,_,sha) in enumerate(ASSETS,1):
        original=gdal.Open(str(data/(band+'.tif')))
        before,after=original.GetRasterBand(1),combined.GetRasterBand(index)
        for y in range(0,SIZE,64):
            height=min(64,SIZE-y)
            if (before.ReadRaster(0,y,SIZE,height)!=after.ReadRaster(0,y,SIZE,height)
                or before.GetMaskBand().ReadRaster(0,y,SIZE,height)!=after.GetMaskBand().ReadRaster(0,y,SIZE,height)):
                raise shared.FixtureError('combined source changed pixels or mask')
        before=after=original=None
        if digest(data/(band+'.tif'))!=sha: raise shared.FixtureError('source bytes changed')
    combined=None
    evidence={'item_id':ITEM_ID,'source_name':SOURCE_NAME,'source_sha256':digest(path),
        'size_bytes':path.stat().st_size,'width':SIZE,'height':SIZE,'band_count':4,'srid':32617,
        'transform':list(TRANSFORM),'valid_pixels_per_band':counts,'assets':assets,
        'full_pixel_and_mask_verification':True,'raw_values_preserved':True}
    (root/'capacity-source-evidence.json').write_text(json.dumps(evidence))
    return path,evidence


def expected_rows(source,y,height):
    """Independent equal-area 2x2 mean of four-band scalar arithmetic."""
    import numpy as np
    total=np.zeros((height*2,SIZE),dtype=np.float64); valid=np.ones(total.shape,dtype=bool)
    for index in range(1,5):
        band=source.GetRasterBand(index); values=band.ReadAsArray(0,y*2,SIZE,height*2)
        total+=values.astype(np.float64)
        valid &= (values!=0) & (band.GetMaskBand().ReadAsArray(0,y*2,SIZE,height*2)!=0)
    scalar=total*2+1
    sums=np.where(valid,scalar,0).reshape(height,2,SIZE//2,2).sum(axis=(1,3))
    counts=valid.reshape(height,2,SIZE//2,2).sum(axis=(1,3))
    output=np.full(sums.shape,np.nan)
    np.divide(sums,counts,out=output,where=counts>0)
    return output


def verify_file(source,path):
    import numpy as np
    from osgeo import gdal
    from osgeo_utils.samples.validate_cloud_optimized_geotiff import validate
    target=gdal.Open(str(path)); band=target.GetRasterBand(1)
    _,errors,_=validate(target,full_check=True)
    if (errors or target.GetMetadataItem('LAYOUT','IMAGE_STRUCTURE')!='COG'
        or (target.RasterXSize,target.RasterYSize,target.RasterCount)!=(SIZE//2,SIZE//2,1)
        or target.GetGeoTransform()!=(600000,20,0,4000020,0,-20)
        or not target.GetSpatialRef().IsSame(source.GetSpatialRef())
        or band.DataType!=gdal.GDT_Float64 or not isinstance(band.GetNoDataValue(),float) or not math.isnan(band.GetNoDataValue())
        or band.GetColorInterpretation()!=gdal.GCI_GrayIndex):
        raise shared.FixtureError('capacity COG layout/grid/type changed')
    count=0
    for y in range(0,SIZE//2,32):
        height=min(32,SIZE//2-y); expected=expected_rows(source,y,height)
        actual=band.ReadAsArray(0,y,SIZE//2,height)
        if (not np.allclose(actual,expected,rtol=1e-10,atol=1e-7,equal_nan=True)
            or not np.array_equal(band.GetMaskBand().ReadAsArray(0,y,SIZE//2,height)!=0,np.isfinite(expected))):
            raise shared.FixtureError('capacity full pixels/mask disagree')
        count+=int(np.isfinite(expected).sum())
    sizes=[]
    if band.GetOverviewCount()!=4: raise shared.FixtureError('capacity pyramid levels changed')
    rows,columns=np.arange(SIZE//2),np.arange(SIZE//2)
    for level in range(band.GetOverviewCount()):
        overview=band.GetOverview(level); sizes.append([overview.XSize,overview.YSize])
        if (overview.XSize,overview.YSize)!=(SIZE//2//(2**(level+1)),)*2:
            raise shared.FixtureError('capacity pyramid dimensions changed')
        # GDAL NEAREST overviews round top-left coordinates in the preceding grid.
        # This differs from Warp nearest at pixel centers. Verify every cascaded sample.
        rows=rows[np.floor(np.arange(overview.YSize)*rows.size/overview.YSize+.5).astype(int)]
        columns=columns[np.floor(np.arange(overview.XSize)*columns.size/overview.XSize+.5).astype(int)]
        for row,base_row in enumerate(rows):
            expected=band.ReadAsArray(0,int(base_row),SIZE//2,1)[0,columns]
            actual=overview.ReadAsArray(0,row,overview.XSize,1)[0]
            if (not np.array_equal(actual,expected,equal_nan=True)
                or not np.array_equal(overview.GetMaskBand().ReadAsArray(0,row,overview.XSize,1)[0]!=0,np.isfinite(expected))):
                raise shared.FixtureError('capacity pyramid disagrees with verified base pixels')
    band=target=None
    return {'size_bytes':path.stat().st_size,'sha256':digest(path),'cog_valid':True,
            'valid_pixels':count,'has_overviews':True,'overview_sizes':sizes,'full_pixel_and_mask_verification':True}


def worker(action,root,names):
    from minio import Minio
    from osgeo import gdal
    gdal.UseExceptions();gdal.SetCacheMax(256<<20)  # Oracle runs outside the measured Runtime.
    config=json.loads((root/'capacity-fixture.json').read_text())
    clients={role:Minio(entry['endpoint'],access_key=entry['access_key'],secret_key=entry['secret_key'],secure=False)
             for role,entry in config.items()}
    if action=='seed':
        for role,entry in config.items():
            clients[role].make_bucket(entry['bucket'])
        probe=shared.provision_target_writer(config['target'],root)
        source,evidence=prepare_source(root)
        clients['source'].fput_object(config['source']['bucket'],SOURCE_NAME,str(source))
        return {'source':evidence,'writer_probe':probe}
    names=names_checked(names)
    evidence=json.loads((root/'capacity-source-evidence.json').read_text()); source_path=root/'public-source'/SOURCE_NAME
    if digest(source_path)!=evidence['source_sha256']:
        raise shared.FixtureError('capacity local source bytes changed')
    with tempfile.TemporaryDirectory(prefix='capacity-verify-',dir=root) as temporary:
        temporary=Path(temporary); source_copy=temporary/SOURCE_NAME
        clients['source'].fget_object(config['source']['bucket'],SOURCE_NAME,str(source_copy))
        if digest(source_copy)!=evidence['source_sha256']:
            raise shared.FixtureError('capacity source object bytes changed')
        actual={item.object_name for item in clients['target'].list_objects(config['target']['bucket'],recursive=True)}
        if actual!=set(names): raise shared.FixtureError('capacity target inventory differs')
        if action=='purge':
            for name in names: clients['target'].remove_object(config['target']['bucket'],name)
            if list(clients['target'].list_objects(config['target']['bucket'],recursive=True)):
                raise shared.FixtureError('capacity target purge has residuals')
            return {'purged':names,'source_unchanged':True}
        source=gdal.Open(str(source_copy)); outputs={}
        for name in names:
            target=temporary/name;clients['target'].fget_object(config['target']['bucket'],name,str(target))
            outputs[name]=verify_file(source,target)
        source=None
    return {'source_unchanged':True,'source_sha256':evidence['source_sha256'],'outputs':outputs}


def physical(action,names):
    if action not in ('seed','verify','purge'): raise shared.FixtureError('unknown capacity physical action')
    root=shared.environment(); names=[] if action=='seed' else names_checked(names)
    image=os.environ['ADDP_ONLINE_RASTER_RUNTIME_IMAGE']
    output=shared.command(['docker','run','--rm','--name',CONTAINERS[2],
        '--label',f'com.addp.online-fixture={SUITE}', '--label',f'com.addp.online-run-id={os.environ["ADDP_ONLINE_TEST_RUN_ID"]}',
        '--network','host','--user',f'{os.getuid()}:{os.getgid()}', '--entrypoint','python',
        '-e','PYTHONPATH=/repo','-v',f'{Path(__file__).resolve().parents[2]}:/repo:ro','-v',f'{root}:/secrets',
        image,'/repo/business/scripts/online-raster-capacity-fixture.py','worker-'+action,'/secrets',json.dumps(names)],timeout=1800)
    return json.loads(output)


def main():
    action=sys.argv[1]
    if action.startswith('worker-'):
        result=worker(action.removeprefix('worker-'),Path(sys.argv[2]),json.loads(sys.argv[3]))
    else:
        root=shared.environment()
        if action=='start': start(root);result={'started':True,'storage':'owned-disk'}
        elif action=='stop': stop();result={'cleanup':'passed'}
        elif action in ('seed','verify','purge'): result=physical(action,[] if len(sys.argv)<3 else json.loads(sys.argv[2]))
        else: raise shared.FixtureError('unknown capacity fixture action')
    print(json.dumps(result,sort_keys=True))


if __name__=='__main__':
    try: main()
    except Exception as error:
        print('Raster capacity physical fixture failed: '+type(error).__name__,file=sys.stderr);raise SystemExit(1)
