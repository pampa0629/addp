"""Python producers use the existing owned Metrics transport and Monitor tests."""
import datetime
import json
import os
import runpy
from pathlib import Path
import shutil
import subprocess
import sys
import time


def startup_evidence(source):
    core=runpy.run_path(str(Path(__file__).with_name('python-http-metrics-source.py')))
    return {f'python_{i}':core['read_startup'](source/f'python-{i}.startup.json') for i in (1,2)}


def python_evidence(source):
    result={'startup':startup_evidence(source),'cleanup':{'status':'unavailable'}}
    try:
        with (source/'python-cleanup.json').open('rb') as stream:raw=stream.read(4097)
        if len(raw)>4096:raise ValueError()
        value=json.loads(raw)
        if set(value)!={'sources','discovery_restored','traffic_available','failure_stage'} or set(value['sources'])!={'python_1','python_2'}:raise ValueError()
        if value['failure_stage'] not in (None,'prepare','producer_start','query','alert','source_stop','complete'):raise ValueError()
        if type(value['discovery_restored']) is not bool or type(value['traffic_available']) is not bool:raise ValueError()
        for row in value['sources'].values():
            if set(row)!={'stop_error','wait_error','nonzero','waited'}:raise ValueError()
            if any(type(row[k]) is not bool for k in ('stop_error','wait_error','nonzero')) or type(row['waited']) is not int or not 0<=row['waited']<=10:raise ValueError()
        result['cleanup']=value
    except (OSError,ValueError,TypeError,KeyError,UnicodeError):pass
    return result


def finish_sources(source,services,compose,discovery,original,failure_stage):
    facts={'sources':{f'python_{i}':{'stop_error':False,'wait_error':False,'nonzero':False,'waited':0} for i in (1,2)},
           'discovery_restored':False,'traffic_available':False,'failure_stage':failure_stage}
    for index in (1,2):
        try:(source/f'python-{index}.stop').touch()
        except OSError:facts['sources'][f'python_{index}']['stop_error']=True
    for index,name in enumerate(services,1):
        row=facts['sources'][f'python_{index}']
        try:identifiers=compose('ps','-aq',name).split()
        except Exception:
            row['wait_error']=True
            continue
        for identifier in identifiers:
            row['waited']+=1
            try:
                result=subprocess.check_output(['docker','wait',identifier],text=True,timeout=10).strip()
                row['nonzero']=row['nonzero'] or result!='0'
            except Exception:row['wait_error']=True
    try:
        traffic=runpy.run_path(str(Path(__file__).with_name('metrics-http-budget.py')))['request_traffic'](source)
        print('Python HTTP T2: controlled request phases '+json.dumps(traffic,sort_keys=True),flush=True)
        facts['traffic_available']=True
    except Exception:pass
    try:
        discovery(original);facts['discovery_restored']=True
    except Exception:pass
    try:(source/'python-cleanup.json').write_text(json.dumps(facts))
    except OSError:return True
    print('Python HTTP T2: producer lifecycle '+json.dumps(python_evidence(source),sort_keys=True),flush=True)
    return (not facts['traffic_available'] or not facts['discovery_restored'] or
            any(row['stop_error'] or row['wait_error'] or row['nonzero'] for row in facts['sources'].values()))


def run(scope):
    work,compose,port,query,eventually,discovery = (scope[k] for k in ('WORK','compose','port','query','eventually','discovery_text'))
    root=Path(__file__).resolve().parents[2]
    source=work/'source'
    for p in source.glob('python-*'):
        if p.is_file():p.unlink()
    shutil.copy2(root/'scripts/test/python-http-metrics-source.py',source/'python-http-metrics-source.py')
    shutil.copytree(root/'common-python/addp_common',source/'python-common/addp_common',ignore=shutil.ignore_patterns('__pycache__'),dirs_exist_ok=True)
    for name in ('pyproject.toml','README.md'):
        shutil.copy2(root/'common-python'/name,source/'python-common'/name)
    (source/'python-requirements.txt').write_text('fastapi==0.115.0\nuvicorn==0.32.0\n')
    services=('python-http-agent','python-http-copilot')
    for index in (1,2):
        startup=source/f'python-{index}.startup.json';startup.write_text('');startup.chmod(0o666)
    original=(source/'discovery.json').read_text()
    phase='prepare'
    try:
        compose('--profile','python-http','up','-d',*services)
        phase='producer_start'
        groups=[];records=[]
        for index,name in enumerate(services,1):
            identifier=compose('ps','-q',name)
            row=json.loads(subprocess.check_output(['docker','inspect',identifier],text=True))[0]
            address=next(iter(row['NetworkSettings']['Networks'].values()))['IPAddress']
            assert row['HostConfig']['NanoCpus']==500_000_000 and row['HostConfig']['Memory']==512*1024**2
            tls=source/f'python-{index}-tls';tls.mkdir(mode=0o700)
            shutil.copy2(work/'source-server.key',tls/'server.key');(tls/'server.key').chmod(0o600)
            shutil.copy2(work/'deployment/source-ca.crt',tls/'ca.crt')
            extension=source/f'python-{index}.extensions'
            extension.write_text('basicConstraints=critical,CA:FALSE\nsubjectAltName=DNS:localhost,DNS:host.docker.internal,IP:127.0.0.1,IP:'+address+'\nextendedKeyUsage=serverAuth\n')
            subprocess.run(['openssl','x509','-req','-in',str(work/'source-server.csr'),'-CA',str(work/'deployment/source-ca.crt'),'-CAkey',str(work/'source-ca.key'),'-CAserial',str(work/'source-ca.srl'),'-days','1','-extfile',str(extension),'-out',str(tls/'server.crt')],check=True,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
            module='agent' if index==1 else 'copilot'
            endpoint=address+':'+str(19443+index)
            entry={'module_name':module,'role':'backend','listen':endpoint,'endpoint':'https://'+endpoint+'/metrics','tls_dir':'/source/'+tls.name}
            (source/f'python-{index}.deployment.json').write_text(json.dumps(entry))
            if os.geteuid()==0:
                uid,gid=map(int,os.environ['ADDP_POSTGRES_METRICS_USER'].split(':'))
                os.chown(source,uid,gid);os.chown(tls,uid,gid)
                for file in tls.iterdir():os.chown(file,uid,gid)
            record=source/f'python-{index}.record.json'
            eventually(lambda: record.exists() or compose('ps','--status','running','-q',name)=='','Python SDK producer starts',seconds=120)
            if not record.exists():raise AssertionError('Python HTTP producer failed before publishing identity')
            records.append(json.loads(record.read_text()))
            groups.append({'targets':[endpoint],'labels':{'__scheme__':'https','__metrics_path__':'/metrics','addp_monitor_kind':'process_resources','addp_source':'application','addp_module_name':module,'addp_instance_id':records[-1]['instance_id'],'addp_runtime_role':'backend'}})
        if scope['desktop_transport']:
            published=[port(name,19444+i) for i,name in enumerate(services)]
            os.environ['ADDP_METRICS_DESKTOP_LOOPBACK_PORTS']=','.join(published)
            subprocess.run(['python3',str(root/'scripts/infra/generate-metrics-config.py')],check=True)
            compose('up','-d','--force-recreate','--wait','--wait-timeout','90','prometheus')
            scope['base']='https://localhost:'+port('prometheus',9090)
            for group,number in zip(groups,published):group['targets']=['127.0.0.1:'+number]
        for record,group in zip(records,groups):record['instance']=group['targets'][0]
        discovery(json.dumps(groups))
        record_path=source/'process-records.json';record_path.write_text(json.dumps(records))
        selector='{job="addp_nodes",addp_monitor_kind="process_resources",addp_module_name=~"agent|copilot"}'
        eventually(lambda: len(query('addp_http_server_requests_total'+selector))==2,'two Python HTTP identities in the sole SD job')
        assert not query('process_cpu_seconds_total'+selector) and not query('process_resident_memory_bytes'+selector)
        def complete_window():
            samples=query('addp_http_server_requests_total'+selector+' offset 1m')
            return len(samples)==2
        eventually(complete_window,'two Python SDK minute windows',seconds=100)
        env=dict(os.environ,GOWORK='off',ADDP_METRICS_QUERY_INTEGRATION='1',MONITOR_PROMETHEUS_URL=scope['base'],MONITOR_PROMETHEUS_CA_FILE=str(scope['TLS']/'ca.crt'),MONITOR_PROMETHEUS_CLIENT_CERT_FILE=str(work/'query-tls/client.crt'),MONITOR_PROMETHEUS_CLIENT_KEY_FILE=str(work/'query-tls/client.key'),ADDP_PROCESS_T2_RECORDS=str(record_path))
        phase='query'
        subprocess.run([str(work/'http-query.test'),'-test.run=^TestIntegrationMetricsHTTPRealSource$','-test.count=1','-test.v'],cwd=root/'monitor/backend',env=env,check=True,timeout=120)
        phase='alert'
        subprocess.run([str(work/'http-alert.test'),'-test.run=^TestIntegrationMetricsHTTPAlertLifecycle$','-test.count=1','-test.timeout=300s','-test.v'],cwd=root/'monitor/backend',env=dict(env,ADDP_HTTP_RULE_LIFECYCLE_INTEGRATION='1',ADDP_POSTGRES_INTEGRATION='1',ADDP_TEST_POSTGRES_HOST='127.0.0.1',ADDP_TEST_POSTGRES_PORT=port('postgres',5432),ADDP_TEST_POSTGRES_DATABASE='addp_test',ADDP_TEST_POSTGRES_USER='addp',ADDP_TEST_POSTGRES_PASSWORD=os.environ['METRICS_T2_POSTGRES_PASSWORD'],ADDP_TEST_POSTGRES_SSLMODE='disable'),check=True,timeout=320)
        phase='source_stop'
        (source/'python-1.stop').touch()
        eventually(lambda: bool(query('up{job="addp_nodes",addp_instance_id="python-http-1"} == 0',at=int(time.time()))),'Python source stop is visible')
        assert query('up{job="addp_nodes",addp_instance_id="python-http-2"} == 1')
        subprocess.run([str(work/'http-query.test'),'-test.run=^TestIntegrationMetricsHTTPRealSource$','-test.count=1','-test.v'],cwd=root/'monitor/backend',env=dict(env,ADDP_PROCESS_T2_STOPPED='1'),check=True,timeout=30)
        print('Python HTTP T2: two actual ASGI SDK sources, identity isolation, natural minute, Monitor queries, historical trend, signed retry/recovery and source-stop isolation passed',flush=True)
        phase='complete'
    finally:
        primary=sys.exc_info()[0] is not None
        try:cleanup_failed=finish_sources(source,services,compose,discovery,original,phase if primary else None)
        except Exception:cleanup_failed=True
        if cleanup_failed and not primary:raise AssertionError('Python SDK cleanup failed')
