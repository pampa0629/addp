"""Actual GDAL/CSV oracle and accurate rejection tests; no Hosted claims."""
import copy
import csv
import importlib
import json
import math
import hashlib
from pathlib import Path
import shutil
import subprocess
import sys
import os
import pytest
from workflow_engine import execute_workflow, GeoPythonWorkflowRunner
from addp_common.workflow_runtime.errors import WorkflowExecutionError

f = importlib.import_module('business.scripts.online-raster-zonal-weighted-stddev-fixture')
m = importlib.import_module('scripts.test.raster-zonal-weighted-stddev-persistence-online')

def localize(dag, root, target):
    for task in dag['tasks']:
        p = task['params']
        if task['operator'] == 'raster_load':
            source = root/p.pop('locator')
            p['access_plan'] = {'schema_version': 'addp.workflow.access-plan/v1', 'source': {'kind': 'file', 'format': 'tiff', 'access': {'method': 'mounted_path', 'path': str(source)}}}
        elif task['operator'] == 'save':
            p.pop('target_parent_locator'); p.pop('target_name'); p.update(connection_info={'mount_path': str(target.parent)}, path=target.name)

@pytest.fixture(scope='module')
def zonal_sources(tmp_path_factory):
    root = tmp_path_factory.mktemp('zonal-weighted-stddev'); (root/'outputs').mkdir(); (root/'.addp-online-owner').write_text('owned')
    initial = f.seed(root); expected = f.oracle(); (root/f.ORACLE).write_bytes(f.encoded(expected))
    previews = {}
    for name in f.CASES:
        dag = m.definition({n:n for n in f.SOURCES}, 'parent', name); localize(dag, root, root/'outputs'/name)
        result = execute_workflow(dag); assert result['status'] == 'success', result
        previews[name] = json.loads(result['final_result'])
    return root, initial, expected, previews


def test_zonal_weighted_stddev_sixteen_real_csvs_complete_independent_oracle_and_raw_archive(zonal_sources, tmp_path):
    root, initial, expected, previews = zonal_sources
    report = f.verify(root, list(f.CASES), expected)
    assert report['sources'] == initial['sources'] and report['full_csv_checks'] == 16
    archive = f.archive_files(root/'outputs', tmp_path, list(f.CASES), {}, 'zonal-weighted-stddev-csvs')
    assert len(archive) == 16
    for name in f.CASES:
        m.validate_preview(previews[name], expected[name], name)
        assert (tmp_path/archive[name]['path']).read_bytes() == (root/'outputs'/name).read_bytes()
        assert archive[name]['size_bytes'] > 0
    assert len(expected['weighted.csv']) == 79 and previews['weighted.csv']['truncated'] is True
    assert expected['empty-zones.csv'] == [] and all(r['weighted_mean'] is None for r in expected['invalid-values.csv'])
    assert all([r[k] for k in f.WEIGHT_COLUMNS] == [0.,0.,None,None] for r in expected['zero.csv'])
    assert [r['zone_value'] for r in expected['int64.csv']] == [0,2**63-2,2**63-1]
    ones=f.read_csv(root/'outputs/ones.csv','ones.csv')
    assert all(r['weighted_stddev']==r['stddev']for r in ones)
    assert sum(f.enabled(n)for n in f.CASES)==13
    mixed=f.read_csv(root/'outputs/mixed-empty.csv','mixed-empty.csv')
    assert mixed[0]['weighted_stddev']==mixed[1]['weighted_stddev']==0.
    assert mixed[2]['weighted_stddev']==math.sqrt(18.75)and mixed[2]['stddev']!=mixed[2]['weighted_stddev']
    assert mixed[3]['weighted_stddev']is None and mixed[4]['weighted_stddev']is None
    assert sum(case[2] is not None for case in f.CASES.values()) == 15

@pytest.mark.parametrize('problem', f.FAILURES)
@pytest.mark.parametrize('language', ['zh-cn','en'])
def test_zonal_weighted_stddev_ninety_eight_exact_submitted_failures_and_no_output(zonal_sources, tmp_path, monkeypatch, problem, language):
    import workflow_engine
    root, initial, expected, _ = zonal_sources
    dag = m.definition({n:n for n in f.SOURCES}, 'parent', 'coverage.csv', invalid=problem, language=language)
    target = tmp_path/'failed.csv'; localize(dag, root, target)
    workspaces = []; original = workflow_engine._execute_operator
    def track(name, params):
        if name == 'raster_zonal_statistics': workspaces.append(params['input_raster'].workspace)
        return original(name, params)
    monkeypatch.setattr(workflow_engine, '_execute_operator', track)
    with pytest.raises(WorkflowExecutionError) as error: GeoPythonWorkflowRunner(language=language).execute(dag)
    assert error.value.error_code == 'EXECUTION_FAILED' and str(error.value) == '任务 analysis 执行失败: '+m.MESSAGES[problem][language == 'en']
    assert workspaces and all(not p.exists() for p in workspaces) and not target.exists()
    assert f.verify(root, list(f.CASES), expected)['sources'] == initial['sources']

@pytest.mark.parametrize('fault', ['weighted-stddev', 'one-ulp', 'class', 'count', 'null', 'column-order', 'extra-row', 'nonfinite', 'source-mask', 'sidecar', 'source-symlink', 'target-symlink', 'directory-symlink'])
def test_zonal_weighted_stddev_real_physical_corruption_is_rejected(zonal_sources, tmp_path, fault):
    from osgeo import gdal
    import numpy as np
    original, _, expected, _ = zonal_sources; root = tmp_path/'changed'; shutil.copytree(original, root)
    path = root/'outputs'/'ones.csv'
    if fault in ('source-symlink','target-symlink','directory-symlink'):
        chosen = root/'values.tif' if fault == 'source-symlink' else root/'outputs' if fault == 'directory-symlink' else path
        moved = tmp_path/'moved';chosen.rename(moved);chosen.symlink_to(moved,target_is_directory=fault == 'directory-symlink')
    elif fault == 'source-mask':
        ds = gdal.Open(str(root/'values.tif'), gdal.GA_Update); ds.GetRasterBand(1).GetMaskBand().WriteArray(np.zeros((1,1), np.uint8), 10, 10); ds = None
    elif fault == 'sidecar': (root/'outputs'/'unrequested.csv').write_text('unexpected')
    else:
        with path.open(newline='') as stream: rows = list(csv.reader(stream))
        row_index = next(i for i, row in enumerate(rows[1:], 1) if row[-1] != '' and float(row[-1]) != 0.)
        if fault == 'weighted-stddev': rows[row_index][-1] = str(float(rows[row_index][-1])+1)
        elif fault == 'one-ulp': rows[row_index][-1] = repr(math.nextafter(float(rows[row_index][-1]), math.inf))
        elif fault == 'class': rows[row_index][0] = str(int(rows[row_index][0])+1)
        elif fault == 'count': rows[row_index][1] += '.0'
        elif fault == 'null': rows[row_index][-1] = ''
        elif fault == 'column-order': rows[0][0], rows[0][1] = rows[0][1], rows[0][0]
        elif fault == 'extra-row': rows.append(rows[row_index])
        elif fault == 'nonfinite': rows[row_index][-1] = 'Infinity'
        with path.open('w', newline='') as stream: csv.writer(stream).writerows(rows)
    with pytest.raises(f.FixtureError): f.verify(root, list(f.CASES), expected)

@pytest.mark.parametrize('fault', ['changed', 'deleted', 'manifest', 'extra'])
def test_zonal_weighted_stddev_csv_archive_never_repairs_or_overwrites(zonal_sources, tmp_path, fault):
    root, _, _, _ = zonal_sources
    receipts = f.archive_files(root/'outputs', tmp_path, list(f.CASES), {}, 'zonal-weighted-stddev-csvs')
    path = tmp_path/receipts['weighted.csv']['path']
    if fault == 'changed': path.chmod(0o644); path.write_bytes(path.read_bytes()+b' ')
    elif fault == 'deleted': path.unlink()
    elif fault == 'manifest': (tmp_path/'zonal-weighted-stddev-csvs-manifest.json').write_text('{}')
    else: (path.parent/'extra.csv').write_text('extra')
    with pytest.raises(f.FixtureError): f.archive_files(root/'outputs', tmp_path, list(f.CASES), receipts, 'zonal-weighted-stddev-csvs')


def test_zonal_weighted_stddev_worker_stdin_frozen_oracle_poison_and_cleanup(tmp_path):
    root = tmp_path/'source'; root.mkdir(); (root/'outputs').mkdir(); (root/'.addp-online-owner').write_text('owned')
    f.seed(root); scratch = tmp_path/'runtime-tmp'; scratch.mkdir()
    result = subprocess.run([sys.executable, '-', '--worker', 'oracle', str(root)], input=Path(f.__file__).read_text(),
        cwd=tmp_path, env={**os.environ, 'TMPDIR': str(scratch)}, capture_output=True, text=True, timeout=90)
    assert result.returncode == 0, result.stderr
    phases=result.stderr
    for marker in ('oracle:start','population:weighted.csv:start','population:weighted.csv:done','oracle:computed','oracle:encoded','oracle:frozen','verify:start','verify:done'):
        assert 'stage='+marker in phases, phases
    assert phases.index('stage=oracle:computed')<phases.index('stage=oracle:frozen')<phases.index('stage=verify:start')<phases.index('stage=verify:done')
    evidence = json.loads(result.stdout); assert len(evidence['expectations']) == 16 and evidence['full_source_pixels_verified'] is True
    path = root/f.ORACLE; path.chmod(0o644); path.write_bytes(path.read_bytes()+b' ')
    with pytest.raises(f.FixtureError, match='oracle cache hash changed'): f.worker('verify', root, [evidence['oracle_sha256']])
    assert f.worker('cleanup-oracle', root, []) == {'oracle_cache_removed': True} and not path.exists()
    assert not any(text in Path(f.__file__).read_text() for text in ('operators.raster_compute', 'np.percentile', 'np.quantile'))


@pytest.mark.parametrize('fault',['normalize-sum','drop-scale','zero-as-null','no-valid-as-zero','ignore-weights','native-nodata'])
def test_zonal_weighted_stddev_independent_oracle_rejects_wrong_weight_semantics(zonal_sources,tmp_path,fault):
    original,_,expected,_=zonal_sources;root=tmp_path/'changed';shutil.copytree(original,root)
    name = {'zero-as-null':'zero.csv','no-valid-as-zero':'invalid-values.csv','drop-scale':'scaled.csv','native-nodata':'int64.csv'}.get(fault,'weighted.csv')
    path=root/'outputs'/name
    with path.open(newline='') as stream: rows=list(csv.DictReader(stream)); fields=list(f.columns(name))
    row=next((r for r in rows if r['weighted_mean'] not in ('','0.0','0')),rows[0])
    if fault == 'normalize-sum': row['weighted_sum']=row['weighted_mean']
    elif fault == 'drop-scale': row['weight_sum']=str(float(row['weight_sum'])/8)
    elif fault == 'zero-as-null': row['weight_sum']=''
    elif fault == 'no-valid-as-zero': row['weight_sum']='0'
    elif fault == 'ignore-weights': row['weighted_sum']=row['sum']
    else: row['weighted_sum']=str(float(row['weighted_sum'])+1e30)
    with path.open('w',newline='') as stream:
        writer=csv.DictWriter(stream,fieldnames=fields);writer.writeheader();writer.writerows(rows)
    with pytest.raises(f.FixtureError):f.verify(root,list(f.CASES),expected)


@pytest.mark.parametrize('fault',['sample-denominator','one-ulp-stddev','zero-stddev','zero-as-zero','disabled-extra-column','all-one-both'])
def test_zonal_weighted_stddev_rejects_stddev_semantics_and_unchanged_original_controls(zonal_sources,tmp_path,fault):
    original,_,expected,_=zonal_sources;root=tmp_path/'changed';shutil.copytree(original,root)
    name={'zero-as-zero':'zero.csv','disabled-extra-column':'control-false.csv','all-one-both':'ones.csv'}.get(fault,'weighted.csv')
    path=root/'outputs'/name
    with path.open(newline='')as stream:reader=csv.DictReader(stream);fields=reader.fieldnames;rows=list(reader)
    row=next((v for v in rows if v.get('weighted_stddev')not in(None,'','0.0','0')),rows[0])
    if fault=='sample-denominator':row['weighted_stddev']=str(float(row['weighted_stddev'])*math.sqrt(float(row['weight_sum'])/(float(row['weight_sum'])-1)))
    elif fault=='one-ulp-stddev':row['weighted_stddev']=repr(math.nextafter(float(row['weighted_stddev']),math.inf))
    elif fault in('zero-stddev','zero-as-zero'):row['weighted_stddev']='0'
    elif fault=='disabled-extra-column':fields.append('weighted_stddev');[v.update(weighted_stddev='0')for v in rows]
    else:
        changed=repr(math.nextafter(float(row['weighted_stddev']),math.inf));row['stddev']=row['weighted_stddev']=changed
    with path.open('w',newline='')as stream:writer=csv.DictWriter(stream,fieldnames=fields);writer.writeheader();writer.writerows(rows)
    with pytest.raises(f.FixtureError):f.verify(root,list(f.CASES),expected)


def test_zonal_weighted_stddev_oracle_preserves_every_frozen_legacy_row(zonal_sources):
    # Complete old 2000-digit centered oracle, business fixture SHA256
    # 5bd333e2ca2a955175249869316cf29adc984616f153da4a4c427d9ecfb2b4cc.
    # No product results define this baseline; include every column of all 16 tables.
    expected_hashes = {'weighted.csv': '5f76fbbb12b3b26011eb630ef0a44afa1e7a34f8ff1cfba7bb466194e464435b', 'scaled.csv': '2d1dc17bd35a9f0ad135d6ce54bd90641dfb44df5eb7205d48328eba57a3c953', 'ones.csv': 'b2be5eafec75539c4bf20eed6ce284c45c61fcfd33c06e6a541bcf9dc70e38f0', 'control-default.csv': '3734350f1c044c739933d8a3511a439dfde9712025b2d365b7988934dbf4307b', 'control-false.csv': '3734350f1c044c739933d8a3511a439dfde9712025b2d365b7988934dbf4307b', 'control-original.csv': '2fe81d095d27b2f9fad3d4019d03f94b1d265a643c78f18b204e183226e3ab57', 'weights-reclassified.csv': '5f76fbbb12b3b26011eb630ef0a44afa1e7a34f8ff1cfba7bb466194e464435b', 'zones-reclassified.csv': '1e7926b08254831d79be5364f71ffb816dacc4f16f20bae2420be45621d31412', 'zero.csv': 'f25212e94bb3d916f6b67fff6e0a339cd340de556c38df3ce357fcd4267a820c', 'mixed-empty.csv': '0c9b2c4d0ca45d55f18e6bfc0db189a95aed669227d5f02f68f9585bd85301b0', 'invalid-values.csv': 'ce365c861aceec628aa2c6322400a12e53445bd54e477cc9c71f53a5428e3693', 'empty-zones.csv': '37517e5f3dc66819f61f5a7bb8ace1921282415f10551d2defa5c3eb0985b570', 'coverage.csv': 'a2f6a4fe767336919268b79243d70c67a779d84f4dd407b21c5daa5cbcc818da', 'rotated.csv': 'b2be5eafec75539c4bf20eed6ce284c45c61fcfd33c06e6a541bcf9dc70e38f0', 'int64.csv': 'b0383f7e0bfceeb2beb5a6b4e36c760544d391c7e55558c5c905423a645b4163', 'uint64.csv': '476cbf1ce54773ad0afa4e388e4d2ce2c886b6b0f124fd5cd7e57a852c46577f'}
    _,_,expected,_=zonal_sources
    assert set(expected_hashes)==set(f.CASES)==set(expected)
    assert {name:hashlib.sha256(f.encoded(rows)).hexdigest()for name,rows in expected.items()}==expected_hashes


@pytest.mark.parametrize('values,weights',[
    ([1e16,1e16+2,1e16+4],[1.,2.,3.]),
    ([1e150,1e150,1e150],[1.,2.,3.]),
    ([1e150,math.nextafter(1e150,math.inf),math.nextafter(1e150,0.)],[1.,2.,3.]),
    ([-7.,0.,11.],[2.**-1022,2.**-1021,2.**-1020]),
    ([0.,4.,12.],[0.,0.,0.]),
    ([1e300,0.,0.],[0.,1.,2.]),
])
def test_zonal_weighted_stddev_fixed_reference_matches_direct_centered_decimal(monkeypatch,values,weights):
    import numpy as np
    from decimal import Decimal,localcontext
    def source(name):
        data=np.array([values if name=='v'else weights if name=='w'else[7.,7.,7.]])
        return np.stack([np.zeros(data.shape),data,np.full(data.shape,128.)]),np.full(data.shape,255,np.uint8),'Float64',-9999.,3857,f.GRID
    monkeypatch.setattr(f,'source_arrays',source)
    row=f.population(('v','z','w'))[0]
    with localcontext()as context:
        context.prec=2000;pairs=[(Decimal.from_float(v),Decimal.from_float(w))for v,w in zip(values,weights)];mass=sum((w for v,w in pairs),Decimal(0))
        if mass:
            center=sum((v*w for v,w in pairs),Decimal(0))/mass
            expected=float((sum((w*(v-center)**2 for v,w in pairs),Decimal(0))/mass).sqrt())
        else:expected=None
    assert row['weighted_stddev']==expected
