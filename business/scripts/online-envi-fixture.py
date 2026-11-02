"""Independent ENVI pairs and bounded physical proof in the owned Hosted fixture."""
from __future__ import annotations
import hashlib
import io
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile

WGS='GEOGCS["WGS 84",DATUM["WGS_1984",SPHEROID["WGS 84",6378137,298.257223563]],PRIMEM["Greenwich",0],UNIT["degree",0.0174532925199433],AUTHORITY["EPSG","4326"]]'
UTM='PROJCS["WGS 84 / UTM zone 50N",'+WGS+',PROJECTION["Transverse_Mercator"],PARAMETER["latitude_of_origin",0],PARAMETER["central_meridian",117],PARAMETER["scale_factor",0.9996],PARAMETER["false_easting",500000],PARAMETER["false_northing",0],UNIT["metre",1],AUTHORITY["EPSG","32650"]]'

def require(value,message):
    if not value:raise ValueError(message)

def pixels():
    # Three independent full bands, with cyan/magenta/yellow blocks in every image.
    return [[[0 if col//86==band else 255 for col in range(256)] for row in range(256)] for band in range(3)]

def pair(spec):
    values=pixels();order=spec['interleave']
    if order=='bsq':raw=bytes(values[b][y][x] for b in range(3) for y in range(256) for x in range(256))
    elif order=='bil':raw=bytes(values[b][y][x] for y in range(256) for b in range(3) for x in range(256))
    elif order=='bip':raw=bytes(values[b][y][x] for y in range(256) for x in range(256) for b in range(3))
    else:raise ValueError('unknown interleave')
    header=f'ENVI\nsamples = 256\nlines = 256\nbands = 3\nheader offset = 0\nfile type = ENVI Standard\ndata type = 1\ninterleave = {order}\nbyte order = 0\n'
    if spec['source_srid']==4326:
        header+='map info = {Geographic Lat/Lon, 1, 1, 113, 28, 0.0001, 0.0001, WGS-84}\ncoordinate system string = {'+WGS+'}\n'
    elif spec['source_srid']==32650:
        header+='map info = {UTM, 1, 1, 500000, 3100000, 10, 10, 50, North, WGS-84}\ncoordinate system string = {'+UTM+'}\n'
    return {spec['name']:raw,spec['header']:header.encode()}

def sources():
    result=[]
    for interleave,srid in [('bsq',4326),('bil',32650),('bip',None)]:
        spec=dict(case=interleave,name=interleave+'.img',header=interleave+'.HDR',interleave=interleave,source_srid=srid,source_format='envi',driver='ENVI',width=256,height=256,band_count=3)
        if srid:spec['extent']=[113.,27.9744,113.0256,28.] if srid==4326 else [500000.,3097440.,502560.,3100000.]
        spec['members']={name:dict(size_bytes=len(raw),sha256=hashlib.sha256(raw).hexdigest()) for name,raw in pair(spec).items()}
        spec['size_bytes']=sum(v['size_bytes'] for v in spec['members'].values())
        spec['sha256']=spec['members'][spec['name']]['sha256']
        result.append(spec)
    return result

def worker(request):
    from minio import Minio
    storage=request['storage'];client=Minio(storage['endpoint'],access_key=storage['access_key'],secret_key=storage['secret_key'],secure=False)
    root=Path(request['nfs_root']);directory=root/'envi';bucket=storage['bucket'];action=request['action']
    specs=sources()
    def verify_sources():
        for spec in specs:
            for name,expected in pair(spec).items():
                response=client.get_object(bucket,'envi/'+name)
                try:raw=response.read()
                finally:response.close();response.release_conn()
                require(raw==(directory/name).read_bytes()==expected,'source bytes changed')
    if action=='seed':
        directory.mkdir(mode=0o755,exist_ok=False)
        for spec in specs:
            for name,raw in pair(spec).items():
                target=directory/name;target.write_bytes(raw);target.chmod(0o644)
                client.put_object(bucket,'envi/'+name,io.BytesIO(raw),len(raw),content_type='application/octet-stream')
        verify_sources();return {'sources':specs}
    if action in ('header-layout','header-version'):
        require(request.get('source_type') in ('nfs','minio') and request.get('case')=='bsq' and type(request.get('changed')) is bool,'bounded header mutation required')
        spec=next(s for s in specs if s['case']=='bsq');original=spec['header'];alternate=spec['name']+'.hdr'
        raw=pair(spec)[original];changed=request['changed']
        name=alternate if action=='header-layout' and changed else original
        if action=='header-version' and changed:raw+=b'description = {owned Hosted version change}\n'
        if request['source_type']=='nfs':
            require(not (directory/name).is_symlink(),'symlink fixture header')
            (directory/name).write_bytes(raw);(directory/name).chmod(0o644)
            if action=='header-layout':(directory/(original if changed else alternate)).unlink(missing_ok=True)
        else:
            client.put_object(bucket,'envi/'+name,io.BytesIO(raw),len(raw),content_type='application/octet-stream')
            if action=='header-layout':client.remove_object(bucket,'envi/'+(original if changed else alternate))
        return {'changed':changed,'header':name,'sha256':hashlib.sha256(raw).hexdigest()}
    if action=='verify':
        verify_sources();return {'sources_unchanged':True}
    if action=='cog':
        from osgeo import gdal,osr
        import numpy as np
        from osgeo_utils.samples import validate_cloud_optimized_geotiff as cog
        spec=next(s for s in specs if s['case']==request['case'])
        ref=request['result'];payload=json.loads(ref['storage_ref'])
        require(payload['bucket']==request['infra']['bucket'] and payload['object'].startswith(f"tenant_{request['tenant_id']}/cog/"),'foreign result')
        infra=Minio(request['infra']['endpoint'],access_key=request['infra']['access_key'],secret_key=request['infra']['secret_key'],secure=False)
        response=infra.get_object(payload['bucket'],payload['object'])
        try:raw=response.read()
        finally:response.close();response.release_conn()
        require(len(raw)==ref['size_bytes'] and hashlib.sha256(raw).hexdigest()==request['sha256'],'COG HTTP/physical mismatch')
        with tempfile.TemporaryDirectory(prefix='envi-oracle-') as work:
            target=Path(work)/'result.tif';target.write_bytes(raw);ds=gdal.OpenEx(str(target),gdal.OF_RASTER,allowed_drivers=['GTiff'])
            require(ds and ds.RasterXSize==ds.RasterYSize==256 and ds.RasterCount==3,'COG dimensions')
            expected=np.asarray(pixels(),dtype='uint8');actual=ds.ReadAsArray();require(actual.dtype==expected.dtype and np.array_equal(actual,expected),'complete ENVI pixels')
            for index in range(1,4):require(np.all(ds.GetRasterBand(index).GetMaskBand().ReadAsArray()==255),'complete ENVI mask')
            srs=osr.SpatialReference();srs.ImportFromEPSG(spec['source_srid']);srs.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER);require(ds.GetSpatialRef().IsSame(srs),'COG CRS')
            expected_grid=(113.,.0001,0.,28.,0.,-.0001) if spec['source_srid']==4326 else (500000.,10.,0.,3100000.,0.,-10.)
            require(tuple(ds.GetGeoTransform())==expected_grid,'COG complete affine grid')
            warnings,errors,_=cog.validate(str(target),full_check=True);require(not errors and not warnings,'COG full layout')
        verify_sources();return {'pixels_verified':196608,'valid_pixels':196608,'complete_mask_verified':True,'crs_verified':True,'grid_verified':True,'cog_valid':True,'sha256':request['sha256'],'size_bytes':len(raw)}
    if action=='deleted-cog':
        from minio.error import S3Error
        payload=json.loads(request['result']['storage_ref']);infra=request['infra']
        require(payload['bucket']==infra['bucket'] and payload['object'].startswith(f"tenant_{request['tenant_id']}/cog/"),'foreign cleanup')
        storage_client=Minio(infra['endpoint'],access_key=infra['access_key'],secret_key=infra['secret_key'],secure=False)
        try:storage_client.stat_object(payload['bucket'],payload['object'])
        except S3Error as error:require(error.code in ('NoSuchKey','NoSuchObject'),'unknown cleanup outcome')
        else:raise ValueError('COG remains')
        # No recursive deletion: this is only a residual observation of this Tenant.
        require(not list(storage_client.list_objects(infra['bucket'],prefix=f"temp/raster-input/tenant_{request['tenant_id']}/",recursive=True)),'temporary ENVI inputs remain')
        verify_sources();return {'object_deleted':True,'temporary_inputs_removed':True,'source_unchanged':True}
    if action=='delete':
        for spec in specs:
            for name in pair(spec):
                (directory/name).unlink(missing_ok=True);client.remove_object(bucket,'envi/'+name)
        (directory/'bsq.img.hdr').unlink(missing_ok=True);client.remove_object(bucket,'envi/bsq.img.hdr')
        require(not list(client.list_objects(bucket,prefix='envi/',recursive=True)),'source objects remain');directory.rmdir()
        return {'objects_removed':True,'files_removed':True}
    raise ValueError('unknown action')

def capture_infra_diagnostic(environment,*,runner=subprocess.run):
    # The existing exclusive Hosted lifecycle owns this Compose deployment.
    require(all(environment.get(k)==v for k,v in {'ADDP_ONLINE_HOSTED':'1','GITHUB_ACTIONS':'true','RUNNER_OS':'Linux','ADDP_ONLINE_MANAGER_CASE':'native-envi','ONLINE_SUITE':'manager-internal-artifact-lineage'}.items()),'exclusive ENVI diagnostic required')
    artifact=Path(environment['ADDP_ONLINE_ARTIFACT_DIR'])/'envi-infra-redpanda.json'
    evidence={'schema_version':'addp.manager-envi-infra-diagnostic/v1','container':'addp-redpanda','status':'unavailable'}
    secrets=[value for key,value in environment.items() if value and re.search(r'password|secret|token|access_key',key,re.I)]
    def redact(text):
        for secret in sorted(secrets,key=len,reverse=True):text=text.replace(secret,'[REDACTED]')
        return text
    try:
        inspected=runner(['docker','container','inspect','addp-redpanda'],capture_output=True,text=True,timeout=5)
        if inspected.returncode:
            evidence['status']='absent'
        else:
            container=json.loads(inspected.stdout)[0]
            if container.get('Config',{}).get('Labels',{}).get('com.docker.compose.project')!='addp-infra':
                evidence['status']='not_owned'
            else:
                state=container['State']
                selected={key:state.get(key) for key in ('Status','ExitCode','OOMKilled','Error')}
                selected['Health']=(state.get('Health') or {}).get('Status')
                logs=runner(['docker','logs','--tail','80','addp-redpanda'],capture_output=True,text=True,timeout=5)
                selected={key:redact(value) if isinstance(value,str) else value for key,value in selected.items()}
                tail=redact(logs.stdout+'\n'+logs.stderr).encode('utf-8')[-8192:].decode('utf-8','ignore')
                evidence.update(status='captured',state=selected,log_exit_code=logs.returncode,log_tail=tail)
    except (OSError,ValueError,KeyError,subprocess.SubprocessError) as error:
        evidence['diagnostic_error']=type(error).__name__
    artifact.write_text(json.dumps(evidence,indent=2)+'\n')
    return evidence

def physical(action,repository,environment,request=None):
    require(action in ('seed','verify','delete','cog','deleted-cog','header-layout','header-version'),'unknown physical action')
    require(all(environment.get(k)==v for k,v in {'ADDP_ONLINE_HOSTED':'1','GITHUB_ACTIONS':'true','RUNNER_OS':'Linux','ADDP_ONLINE_MANAGER_CASE':'native-envi'}.items()),'exclusive Hosted ENVI case required')
    state=Path(environment['ADDP_ONLINE_SECRET_DIR'])/'nfs-root';require(not state.is_symlink(),'invalid state');root=Path(state.read_text().strip())
    require(re.fullmatch(r'/tmp/addp-online-manager-nfs\.[A-Za-z0-9]{6}',str(root)) and not root.is_symlink() and (root/'.addp-online-owner').read_text().strip()==environment['ADDP_ONLINE_TEST_RUN_ID'],'unowned NFS root')
    config=dict(request or {},action=action,nfs_root=str(root),storage={'endpoint':'127.0.0.1:'+environment['ADDP_ONLINE_MANAGER_MINIO_PORT'],'access_key':environment['ADDP_ONLINE_MANAGER_MINIO_ACCESS_KEY'],'secret_key':environment['ADDP_ONLINE_MANAGER_MINIO_SECRET_KEY'],'bucket':environment['ADDP_ONLINE_MANAGER_MINIO_BUCKET']})
    if action in ('cog','deleted-cog'):
        # Secret values are supplied only on private worker stdin, never reports.
        config['infra']={'endpoint':'127.0.0.1:'+environment['MINIO_API_PORT'],'access_key':environment['MINIO_ROOT_USER'],'secret_key':environment['MINIO_ROOT_PASSWORD'],'bucket':'manager'}
    script=Path(repository)/'business/scripts/online-envi-fixture.py'
    result=subprocess.run(['docker','run','--rm','-i','--user',f'{os.getuid()}:{os.getgid()}','--name','addp-manager-raster-verifier','--network','host','--label','com.addp.online-fixture=manager-internal-artifact-lineage','--mount',f'type=bind,src={script},dst=/fixture.py,readonly','--mount',f'type=bind,src={root},dst={root}',environment['ADDP_ONLINE_RASTER_RUNTIME_IMAGE'],'python','/fixture.py'],input=json.dumps(config),text=True,capture_output=True,timeout=120)
    require(result.returncode==0,f'ENVI physical {action} failed: exit {result.returncode}')
    return json.loads(result.stdout)

if __name__=='__main__':
    try:
        require(sys.argv[1:] in ([],['--infra-diagnostic']),'unknown ENVI fixture command')
        result=capture_infra_diagnostic(os.environ) if sys.argv[1:] else worker(json.load(sys.stdin))
        print(json.dumps({'diagnostic_status':result['status']} if sys.argv[1:] else result))
    except Exception as error:print(json.dumps({'error_code':type(error).__name__}));raise SystemExit(1)
