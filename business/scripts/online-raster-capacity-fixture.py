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


def owned(name):
    if not shared.owned(name,SUITE): return False
    container=json.loads(shared.command(['docker','inspect',name]))[0]
    if container.get('Config',{}).get('Labels',{}).get('com.addp.online-run-id')!=os.environ['ADDP_ONLINE_TEST_RUN_ID']:
        raise shared.FixtureError('capacity container belongs to another run')
    return True


def stop():
    errors=[]
    for name in reversed(CONTAINERS):
        try:
            if owned(name): shared.command(['docker','rm','-fv',name])
            if owned(name): raise shared.FixtureError('capacity fixture cleanup residual')
        except Exception as error: errors.append(type(error).__name__)
    if errors: raise shared.FixtureError('capacity fixture cleanup failed: '+','.join(errors))


def start(root):
    if any(owned(name) for name in CONTAINERS):
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
        'full_pixel_and_mask_verification':True,'raw_values_preserved':True,'gdal_version':gdal.VersionInfo('--version')}
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


# Complete-scene directory workload. The original single-file oracle stays intact.
MOSAIC_SOURCE_PREFIX = 'mosaic-source'
MOSAIC_TARGET_PATTERN = re.compile(r'capacity-mosaic-l[14]-r[12]')


def mosaic_names_checked(names):
    if not isinstance(names,list) or len(names)!=1 or not isinstance(names[0],str) or not MOSAIC_TARGET_PATTERN.fullmatch(names[0]):
        raise shared.FixtureError('invalid capacity mosaic dataset name')
    return names


def mosaic_windows():
    if type(SIZE) is not int or SIZE<=0 or SIZE%2:
        raise shared.FixtureError('capacity mosaic requires an even complete-scene grid')
    half=SIZE//2
    return {f'r{y}_c{x}.tif':(x,y,half,half) for y in (0,half) for x in (0,half)}


def mosaic_paths():
    return {'mosaic.addp.json','index/source-index.json','overviews/overview.cog.tif',
            *('leaf/'+Path(name).stem+'.cog.tif' for name in mosaic_windows())}


def mosaic_objects_checked(objects,name):
    mosaic_names_checked([name])
    expected={name+'/'+relative for relative in mosaic_paths()}
    markers={name+'/',name+'/leaf/',name+'/overviews/',name+'/index/'}
    if (not expected.issubset(objects) or set(objects)-expected-markers
        or any(type(objects[key]) is not int or objects[key]!=0 for key in set(objects)&markers)):
        raise shared.FixtureError('capacity mosaic target inventory differs')
    return sorted(set(objects)&markers)


def verify_mosaic_raster(source,path,window=None,*,overview=False,cog=True):
    """Compare every raw band and mask with a window of the original full scene."""
    import numpy as np
    from osgeo import gdal
    from osgeo_utils.samples.validate_cloud_optimized_geotiff import validate
    target=gdal.Open(str(path)); factor=2 if overview else 1
    x,y,width,height=window or (0,0,SIZE,SIZE)
    out_width,out_height=width//factor,height//factor
    gt=source.GetGeoTransform()
    transform=(gt[0]+x*gt[1]+y*gt[2],gt[1]*factor,gt[2]*factor,
               gt[3]+x*gt[4]+y*gt[5],gt[4]*factor,gt[5]*factor)
    if (not target or (target.RasterXSize,target.RasterYSize,target.RasterCount)!=(out_width,out_height,4)
        or target.GetGeoTransform()!=transform or not target.GetSpatialRef().IsSame(source.GetSpatialRef())
        or target.GetFileList()!=[str(path)]):
        raise shared.FixtureError('capacity mosaic raster grid or sidecars differ')
    if cog:
        _,errors,_=validate(target,full_check=True)
        if errors or target.GetMetadataItem('LAYOUT','IMAGE_STRUCTURE')!='COG':
            raise shared.FixtureError('capacity mosaic COG layout differs')
    overview_sizes=[];valid_counts=[]
    expected_sizes=[];w,h=out_width,out_height
    while max(w,h)>512:
        w,h=w//2,h//2;expected_sizes.append([w,h])
    for index in range(1,5):
        original,band=source.GetRasterBand(index),target.GetRasterBand(index)
        if (band.DataType!=gdal.GDT_UInt16 or band.GetNoDataValue()!=0
            or original.GetNoDataValue()!=0 or band.GetColorInterpretation()!=original.GetColorInterpretation()):
            raise shared.FixtureError('capacity mosaic band type/NoData/color differs')
        count=0
        for row in range(0,out_height,32):
            count_rows=min(32,out_height-row)
            args=(x+factor//2,y+row*factor+factor//2,(out_width-1)*factor+1,(count_rows-1)*factor+1)
            expected=original.ReadAsArray(*args)[::factor,::factor]
            mask=original.GetMaskBand().ReadAsArray(*args)[::factor,::factor]
            if (not np.array_equal(band.ReadAsArray(0,row,out_width,count_rows),expected)
                or not np.array_equal(band.GetMaskBand().ReadAsArray(0,row,out_width,count_rows),mask)):
                raise shared.FixtureError('capacity mosaic full pixels or independent mask differ')
            count+=int(np.count_nonzero(mask))
        valid_counts.append(count)
        sizes=[[band.GetOverview(level).XSize,band.GetOverview(level).YSize] for level in range(band.GetOverviewCount())]
        if cog and sizes!=expected_sizes:
            raise shared.FixtureError('capacity mosaic pyramid dimensions differ')
        if index==1:overview_sizes=sizes
        elif sizes!=overview_sizes:raise shared.FixtureError('capacity mosaic band pyramids differ')
        rows,columns=np.arange(out_height),np.arange(out_width)
        for level in range(band.GetOverviewCount()):
            lower=band.GetOverview(level)
            rows=rows[np.floor(np.arange(lower.YSize)*rows.size/lower.YSize+.5).astype(int)]
            columns=columns[np.floor(np.arange(lower.XSize)*columns.size/lower.XSize+.5).astype(int)]
            for start in range(0,lower.YSize,16):
                selected=rows[start:start+16];first,last=int(selected[0]),int(selected[-1])+1
                expected=band.ReadAsArray(0,first,out_width,last-first)[selected[:,None]-first,columns]
                mask=band.GetMaskBand().ReadAsArray(0,first,out_width,last-first)[selected[:,None]-first,columns]
                if (not np.array_equal(lower.ReadAsArray(0,start,lower.XSize,len(selected)),expected)
                    or not np.array_equal(lower.GetMaskBand().ReadAsArray(0,start,lower.XSize,len(selected)),mask)):
                    raise shared.FixtureError('capacity mosaic full pyramid pixels or masks differ')
    target=None
    return {'size_bytes':path.stat().st_size,'sha256':digest(path),'cog_valid':cog,
            'width':out_width,'height':out_height,'band_count':4,'transform':list(transform),
            'band_valid_pixels':valid_counts,'overview_sizes':overview_sizes,
            'full_pixel_and_mask_verification':True}


def prepare_mosaic_source(root):
    from osgeo import gdal
    source_path=root/'public-source'/SOURCE_NAME; source=gdal.Open(str(source_path))
    directory=root/MOSAIC_SOURCE_PREFIX;directory.mkdir(mode=0o700)
    tiles={}
    for name,window in mosaic_windows().items():
        path=directory/name
        converted=gdal.Translate(str(path),source,format='GTiff',srcWin=list(window),
            creationOptions=['TILED=YES','COMPRESS=DEFLATE','PREDICTOR=2','INTERLEAVE=BAND','BLOCKXSIZE=512','BLOCKYSIZE=512'])
        converted=None
        tiles[name]=verify_mosaic_raster(source,path,window,cog=False)
    source=None
    return tiles


def verify_mosaic_directory(source,directory,name,object_sizes):
    """Validate the exact whole scope; no producer size is invented for a manifest."""
    paths={p.relative_to(directory).as_posix():p for p in directory.rglob('*') if p.is_file()}
    if set(paths)!=mosaic_paths() or set(object_sizes)!=set(paths):
        raise shared.FixtureError('capacity mosaic exact seven-object inventory differs')
    for relative,path in paths.items():
        size=object_sizes[relative]
        if type(size) is not int or size<=0 or size!=path.stat().st_size:
            raise shared.FixtureError('capacity mosaic object/file sizes differ')
    manifest=json.loads(paths['mosaic.addp.json'].read_text());index=json.loads(paths['index/source-index.json'].read_text())
    extent=[TRANSFORM[0],TRANSFORM[3]-10*SIZE,TRANSFORM[0]+10*SIZE,TRANSFORM[3]]
    if (manifest.get('schema_version')!='addp.raster_mosaic.v1' or manifest.get('dataset_name')!=name
        or (manifest.get('data_type'),manifest.get('format'),manifest.get('layout'))!=('media','raster_mosaic','whole')
        or manifest.get('refs')!={'index':'index/source-index.json','overview':'overviews/overview.cog.tif'}
        or manifest.get('summary')!={'leaf_count':4,'source_count':4,'failed_count':0,'extent':extent,
            'source_crs':'EPSG:32617','vrt_width':SIZE,'vrt_height':SIZE,'overview_width':SIZE//2,'overview_height':SIZE//2}
        or index.get('schema_version')!='addp.raster_mosaic.source_index.v1' or index.get('leaf_count')!=4):
        raise shared.FixtureError('capacity mosaic manifest/index grid or references differ')
    leaves=index.get('leaves')
    if not isinstance(leaves,list) or len(leaves)!=4:
        raise shared.FixtureError('capacity mosaic index leaves differ')
    rasters={}
    for filename,window in mosaic_windows().items():
        reference='leaf/'+Path(filename).stem+'.cog.tif'
        matches=[leaf for leaf in leaves if leaf.get('source_ref')==filename and leaf.get('leaf_ref')==reference]
        if len(matches)!=1 or matches[0].get('generation_status')!='generated' or matches[0].get('cog_validation',{}).get('status')!='valid':
            raise shared.FixtureError('capacity mosaic source-to-leaf binding differs')
        rasters[reference]=verify_mosaic_raster(source,paths[reference],window)
    rasters['overviews/overview.cog.tif']=verify_mosaic_raster(source,paths['overviews/overview.cog.tif'],overview=True)
    return {'physical_size_bytes':sum(p.stat().st_size for p in paths.values()),
            'object_size_bytes':sum(object_sizes.values()),'object_sizes':object_sizes,
            'object_sha256':{relative:digest(path) for relative,path in paths.items()},
            'rasters':rasters,'summary':manifest['summary'],'object_count':len(paths)}


def mosaic_worker(action,root,names):
    from minio import Minio
    from osgeo import gdal
    gdal.UseExceptions();gdal.SetCacheMax(64<<20)
    config=json.loads((root/'capacity-fixture.json').read_text())
    clients={role:Minio(entry['endpoint'],access_key=entry['access_key'],secret_key=entry['secret_key'],secure=False)
             for role,entry in config.items()}
    source_path=root/'public-source'/SOURCE_NAME
    evidence=json.loads((root/'capacity-source-evidence.json').read_text())
    if digest(source_path)!=evidence['source_sha256']:
        raise shared.FixtureError('capacity mosaic local source changed')
    if action=='seed-mosaic':
        require_disk(root);tiles=prepare_mosaic_source(root)
        for filename in tiles:
            clients['source'].fput_object(config['source']['bucket'],MOSAIC_SOURCE_PREFIX+'/'+filename,str(root/MOSAIC_SOURCE_PREFIX/filename))
        (root/'capacity-mosaic-source-evidence.json').write_text(json.dumps(tiles,sort_keys=True))
        return {'tiles':tiles,'source_sha256':evidence['source_sha256'],'source_unchanged':True}
    name=mosaic_names_checked(names)[0]
    tiles=json.loads((root/'capacity-mosaic-source-evidence.json').read_text())
    expected_source={SOURCE_NAME,*[MOSAIC_SOURCE_PREFIX+'/'+filename for filename in mosaic_windows()]}
    if {item.object_name for item in clients['source'].list_objects(config['source']['bucket'],recursive=True)}!=expected_source:
        raise shared.FixtureError('capacity mosaic source inventory changed')
    actual={item.object_name:item.size for item in clients['target'].list_objects(config['target']['bucket'],recursive=True)}
    markers=mosaic_objects_checked(actual,name)
    with tempfile.TemporaryDirectory(prefix='capacity-mosaic-verify-',dir=root) as temporary:
        temporary=Path(temporary)
        for key in sorted(expected_source):
            copy=temporary/'source-check.tif';clients['source'].fget_object(config['source']['bucket'],key,str(copy))
            expected=evidence['source_sha256'] if key==SOURCE_NAME else tiles[Path(key).name]['sha256']
            local=source_path if key==SOURCE_NAME else root/key
            if digest(copy)!=expected or digest(local)!=expected:
                raise shared.FixtureError('capacity mosaic source bytes changed')
            copy.unlink()
        if action=='purge-mosaic':
            for key in sorted(actual):clients['target'].remove_object(config['target']['bucket'],key)
            if list(clients['target'].list_objects(config['target']['bucket'],recursive=True)):
                raise shared.FixtureError('capacity mosaic purge has residuals')
            return {'source_unchanged':True,'purged':[name]}
        for relative in mosaic_paths():
            path=temporary/relative;path.parent.mkdir(parents=True,exist_ok=True)
            clients['target'].fget_object(config['target']['bucket'],name+'/'+relative,str(path))
        source=gdal.Open(str(source_path))
        stat_sizes={relative:clients['target'].stat_object(config['target']['bucket'],name+'/'+relative).size for relative in mosaic_paths()}
        if any(stat_sizes[relative]!=actual[name+'/'+relative] for relative in stat_sizes):
            raise shared.FixtureError('capacity mosaic object list/stat sizes differ')
        if any(clients['target'].stat_object(config['target']['bucket'],key).size!=0 for key in markers):
            raise shared.FixtureError('capacity mosaic directory marker stat differs')
        result=verify_mosaic_directory(source,temporary,name,stat_sizes);source=None
    return {**result,'source_unchanged':True,'directory_markers':markers,'raw_object_count':len(actual)}


def physical(action,names):
    if action not in ('seed','verify','purge','seed-mosaic','verify-mosaic','purge-mosaic'): raise shared.FixtureError('unknown capacity physical action')
    root=shared.environment()
    names=[] if action in ('seed','seed-mosaic') else mosaic_names_checked(names) if action.endswith('-mosaic') else names_checked(names)
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
        operation=action.removeprefix('worker-')
        function=mosaic_worker if operation.endswith('-mosaic') else worker
        result=function(operation,Path(sys.argv[2]),json.loads(sys.argv[3]))
    else:
        root=shared.environment()
        if action=='start': start(root);result={'started':True,'storage':'owned-disk'}
        elif action=='stop': stop();result={'cleanup':'passed'}
        elif action in ('seed','verify','purge','seed-mosaic','verify-mosaic','purge-mosaic'): result=physical(action,[] if len(sys.argv)<3 else json.loads(sys.argv[2]))
        else: raise shared.FixtureError('unknown capacity fixture action')
    print(json.dumps(result,sort_keys=True))


if __name__=='__main__':
    try: main()
    except Exception as error:
        print('Raster capacity physical fixture failed: '+type(error).__name__,file=sys.stderr);raise SystemExit(1)
