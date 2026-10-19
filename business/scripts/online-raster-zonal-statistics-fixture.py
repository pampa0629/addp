#!/usr/bin/env python3
"""Owned NFS inputs and independent complete CSV oracle for zonal statistics."""
from __future__ import annotations
import csv
from decimal import Decimal, InvalidOperation
import hashlib
import json
import math
import os
from pathlib import Path
import re
import statistics
import subprocess
import sys
import tempfile

SUITE='raster-zonal-statistics-persistence'
RUNTIME='addp-raster-zonal-statistics-runtime'
TARGETS=('classified.csv','reclassified.csv','many-zones.csv','empty.csv')
SOURCES=('values.tif','zones.tif')
COLUMNS=('zone_value','total_count','valid_count','invalid_count','sum','min','max','mean','stddev')
TRANSFORM=(110.,1/1024,1/8192,30.,1/4096,-1/512)
WIDTH,HEIGHT=12,6


class FixtureError(RuntimeError):pass


def command(args,**kwargs):
    return subprocess.run(args,check=True,capture_output=True,text=True,timeout=120,**kwargs).stdout.strip()


if sys.argv[1:2]!=['--worker']:
    sys.path.insert(0,str(Path(__file__).resolve().parents[2]))
    from scripts.utils.online_nfs_fixture import NFSExport,FixtureError,run_runtime_worker


def guard(env=os.environ):return NFSExport('zonal-statistics',command).guard(env)
def owned_root(secret):return NFSExport('zonal-statistics',command).owned_root(secret,os.environ['ADDP_ONLINE_TEST_RUN_ID'])


def digest(path):
    if path.is_symlink() or not path.is_file() or path.stat().st_size<=0:
        raise FixtureError('expected a positive-size ordinary owned file')
    return {'sha256':hashlib.sha256(path.read_bytes()).hexdigest(),'size_bytes':path.stat().st_size}


def source_values():
    classes=[[-3,0,2,7]*3 for _ in range(HEIGHT)]
    classes[1][3]=-999
    values=[[(y*WIDTH+x)*.25-2 if classes[y][x]!=7 else -999. for x in range(WIDTH)] for y in range(HEIGHT)]
    values[2][6]=math.nan;values[3][4]=math.inf;values[0][4]=-math.inf
    z_alpha=[[255.]*WIDTH for _ in range(HEIGHT)];z_alpha[1][4]=0;z_alpha[0][0]=128
    v_alpha=[[255.]*WIDTH for _ in range(HEIGHT)];v_alpha[5][2]=0;v_alpha[0][1]=128;v_alpha[4][8]=math.nan
    z_mask=[[255]*WIDTH for _ in range(HEIGHT)];z_mask[4][5]=0
    v_mask=[[255]*WIDTH for _ in range(HEIGHT)];v_mask[2][4]=0
    return classes,values,z_alpha,v_alpha,z_mask,v_mask


def seed(root):
    import numpy as np
    from osgeo import gdal,osr
    if any((root/name).exists() for name in SOURCES) or list((root/'outputs').iterdir()):
        raise FixtureError('refusing to overwrite existing fixture data')
    classes,values,z_alpha,v_alpha,z_mask,v_mask=source_values()
    crs=osr.SpatialReference();crs.ImportFromEPSG(4326)
    arrays={SOURCES[0]:([np.ones((HEIGHT,WIDTH)),values,v_alpha],v_mask),
            SOURCES[1]:([np.full((HEIGHT,WIDTH),.125),classes,np.arange(WIDTH*HEIGHT).reshape(HEIGHT,WIDTH),np.full((HEIGHT,WIDTH),-999),z_alpha],z_mask)}
    for name,(bands,mask) in arrays.items():
        dataset=gdal.GetDriverByName('GTiff').Create(str(root/name),WIDTH,HEIGHT,len(bands),gdal.GDT_Float64)
        dataset.SetProjection(crs.ExportToWkt());dataset.SetGeoTransform(TRANSFORM)
        for i,data in enumerate(bands,1):dataset.GetRasterBand(i).WriteArray(np.asarray(data,dtype=np.float64))
        dataset.GetRasterBand(2).SetNoDataValue(-999)
        dataset.GetRasterBand(len(bands)).SetColorInterpretation(gdal.GCI_AlphaBand)
        with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):
            dataset.CreateMaskBand(gdal.GMF_PER_DATASET)
        dataset.GetRasterBand(1).GetMaskBand().WriteArray(np.asarray(mask,dtype=np.uint8))
        if dataset.GetFileList()!=[str(root/name)]:raise FixtureError('source must be self-contained')
        dataset=None
    return {'sources':{name:digest(root/name) for name in SOURCES}}


def expected_rows(name):
    if name not in TARGETS:raise FixtureError('unknown exact zonal statistics target')
    classes,values,z_alpha,v_alpha,z_mask,v_mask=source_values()
    groups={}
    for y in range(HEIGHT):
        for x in range(WIDTH):
            if name=='empty.csv' or not z_mask[y][x] or z_alpha[y][x]<=0:continue
            code=y*WIDTH+x if name=='many-zones.csv' else classes[y][x]
            if code==-999:continue
            if name=='reclassified.csv':code={-3:10,0:10,2:20,7:30}[code]
            group=groups.setdefault(code,{'total':0,'values':[]});group['total']+=1
            value=values[y][x]
            if (v_mask[y][x] and math.isfinite(v_alpha[y][x]) and v_alpha[y][x]>0
                and math.isfinite(value) and value!=-999):group['values'].append(value)
    rows=[]
    for code,group in sorted(groups.items()):
        numbers=group['values'];n=len(numbers)
        stats=[math.fsum(numbers),min(numbers),max(numbers),statistics.fmean(numbers),statistics.pstdev(numbers)] if n else [None]*5
        rows.append(dict(zip(COLUMNS,[code,group['total'],n,group['total']-n,*stats])))
    return rows


def validate_records(actual,expected):
    if len(actual)!=len(expected):raise FixtureError('complete table row count differs')
    for row,wanted in zip(actual,expected):
        if set(row)!=set(COLUMNS):raise FixtureError('complete table schema differs')
        for i,name in enumerate(COLUMNS):
            value=row[name];target=wanted[name]
            if target is None:
                if value is not None:raise FixtureError('empty zone statistics must remain null')
            elif i<4:
                if type(value) is not int or value!=target:raise FixtureError('classification or count differs')
            elif isinstance(value,bool) or not isinstance(value,(int,float)) or not math.isfinite(value) or not math.isclose(value,target,rel_tol=1e-12,abs_tol=1e-12):
                raise FixtureError('complete table statistics differ')


def verify(root,names):
    if len(set(names))!=len(names) or any(name not in TARGETS for name in names):raise FixtureError('invalid exact zonal target inventory')
    if sorted(p.name for p in (root/'outputs').iterdir())!=sorted(names):raise FixtureError('unexpected output, sidecar or temporary file')
    result={'sources':{name:digest(root/name) for name in SOURCES},'targets':{}}
    for name in names:
        path=root/'outputs'/name;fact=digest(path)
        with path.open(newline='',encoding='utf-8') as stream:
            reader=csv.DictReader(stream)
            if reader.fieldnames!=list(COLUMNS):raise FixtureError('CSV schema differs')
            rows=[]
            for record in reader:
                if set(record)!=set(COLUMNS) or any(value is None for value in record.values()):raise FixtureError('malformed CSV record')
                row={}
                for i,column in enumerate(COLUMNS):
                    text=record[column]
                    if i<4:
                        if re.fullmatch(r'-?(0|[1-9][0-9]*)',text) is None:raise FixtureError('CSV classification and counts must be exact integers')
                        row[column]=int(text)
                    elif text=='':row[column]=None
                    else:
                        try:value=Decimal(text)
                        except InvalidOperation:raise FixtureError('CSV statistics must be finite numbers')
                        if not value.is_finite():raise FixtureError('CSV statistics must be finite numbers')
                        row[column]=float(value)
                rows.append(row)
        validate_records(rows,expected_rows(name))
        result['targets'][name]={**fact,'row_count':len(rows),'schema':list(COLUMNS),
            'complete_table_verified':True,'null_zone_count':sum(row['valid_count']==0 for row in rows)}
    return result


def worker(action,root,names):
    if action not in {'seed','verify'}:raise FixtureError('unknown worker action')
    if action=='verify' and list(Path(tempfile.gettempdir()).glob('addp-raster-*')):
        raise FixtureError('Runtime raster execution workspace has residuals')
    return seed(root) if action=='seed' else {**verify(root,names),'runtime_workspaces_removed':True}


def runtime_worker(action,root,names=()):return run_runtime_worker(command,RUNTIME,SUITE,Path(__file__),action,root,names)


def main(action):
    secret=guard()
    if action in {'start','stop'}:return NFSExport('zonal-statistics',command).lifecycle(action,secret,os.environ['ADDP_ONLINE_TEST_RUN_ID'])
    root=owned_root(secret)
    if action=='seed':
        result=runtime_worker('seed',root);(secret/'zonal-statistics-source.json').write_text(json.dumps(result));return result
    if action=='verify':
        result=runtime_worker('verify',root,sys.argv[2:])
        if result['sources']!=json.loads((secret/'zonal-statistics-source.json').read_text())['sources']:raise FixtureError('source file changed')
        return result
    raise FixtureError('unknown fixture action')


if __name__=='__main__':
    try:
        result=worker(sys.argv[2],Path(sys.argv[3]),sys.argv[4:]) if sys.argv[1:2]==['--worker'] else main(sys.argv[1])
        print(json.dumps(result,sort_keys=True))
    except Exception as error:
        print(f'Zonal fixture failed: {type(error).__name__}: {error}',file=sys.stderr);raise SystemExit(1)
