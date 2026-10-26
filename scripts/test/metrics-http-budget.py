"""The existing HTTP T2 case has one local resource/request window, no retries."""
import json
import hashlib
import os
from pathlib import Path
import re
import signal
import subprocess
import sys
import tempfile
import time
import uuid
from datetime import datetime, timezone

ROOT=Path(__file__).resolve().parents[2]


def resource_totals(rows, work_bytes, volume_bytes):
    cpu=memory=writable=tmpfs=0
    for row in rows:
        config=row['HostConfig'];state=row['State']
        if state.get('OOMKilled') or state.get('Status') in ('dead','restarting'):
            raise RuntimeError('http_budget_container_failure')
        cores=config.get('NanoCpus',0)/1e9
        if not cores and config.get('CpuPeriod',0)>0:cores=config.get('CpuQuota',0)/config['CpuPeriod']
        limit=config.get('Memory',0)
        if cores<=0 or limit<=0:raise RuntimeError('http_budget_hard_limit_missing')
        cpu+=cores;memory+=limit;writable+=row.get('SizeRw',0)
        for mount,options in (config.get('Tmpfs') or {}).items():
            match=re.search(r'(?:^|,)size=([0-9]+)([kmg]?)(?:,|$)',options.lower())
            if not match:raise RuntimeError('http_budget_tmpfs_unbounded')
            tmpfs+=int(match[1])*{'':1,'k':1024,'m':1024**2,'g':1024**3}[match[2]]
    total=work_bytes+volume_bytes+writable+tmpfs
    if cpu>4 or memory>4*1024**3 or total>=2*1024**3:raise RuntimeError('http_budget_limit')
    return {'cpu_hard_limit':cpu,'memory_hard_limit_bytes':memory,'private_write_upper_bytes':total,
            'work_bytes':work_bytes,'volume_bytes':volume_bytes,'writable_layers_bytes':writable,'tmpfs_reserved_bytes':tmpfs}


def sample(ownership):
    began=time.monotonic();end=began+4
    def command(args):
        remaining=end-time.monotonic()
        if remaining<=0:raise RuntimeError('http_budget_sample_deadline')
        value=subprocess.check_output(args,timeout=remaining)
        if time.monotonic()>=end:raise RuntimeError('http_budget_sample_deadline')
        return value.decode()
    project,work=ownership['project'],Path(ownership['work'])
    if not re.fullmatch(r'addp-metrics-t2-[0-9a-f]{32}',project) or not work.is_absolute() or not work.name.startswith('addp-metrics-t2.') or work.is_relative_to(ROOT):
        raise RuntimeError('http_budget_ownership_invalid')
    ids=command(['docker','ps','-aq','--filter','label=com.docker.compose.project='+project]).split()
    rows=json.loads(command(['docker','inspect','--size',*ids])) if ids else []
    volume=0
    for row in rows:
        if row.get('Config',{}).get('Labels',{}).get('com.docker.compose.service')=='prometheus' and row['State'].get('Running'):
            volume=int(command(['docker','exec',row['Id'],'du','-sk','/prometheus']).split()[0])*1024
    disk=int(command(['du','-sk',str(work)]).split()[0])*1024 if work.exists() else 0
    return {**resource_totals(rows,disk,volume),'duration_seconds':time.monotonic()-began}


def input_hashes():
    owner=ROOT/'scripts/test/monitor-metrics-gate.sh'
    declared=re.search(r'^# ADDP_T2_INPUT_FILES=(.*)$',owner.read_text(),re.M)[1].split()
    names=subprocess.check_output(['git','ls-files','--cached','--others','--exclude-standard','--',*declared,'Makefile','scripts/test/monitor-metrics-gate.sh','scripts/test/docker-compose.monitor-metrics-t2.yml'],cwd=ROOT,text=True).splitlines()
    return {name:hashlib.sha256((ROOT/name).read_bytes()).hexdigest() for name in sorted(set(names)) if (ROOT/name).is_file()}


def main():
    if sys.argv[1:3] != ['bash','scripts/test/monitor-metrics-gate.sh']:
        raise RuntimeError('http_budget_owner_command_required')
    args=sys.argv[3:]
    command=['bash',str(ROOT/'scripts/test/monitor-metrics-gate.sh'),*args]
    if args!=['--case=http-quality']:
        os.execvpe('bash',command,os.environ)
    started=time.monotonic()
    wall_started=datetime.now(timezone.utc).isoformat()
    inputs=input_hashes()
    report={'started_at':wall_started,'input_sha256':inputs,'result':'failed','maximum_seconds':1200,'samples':[],'hosted':False,'capacity':False,'schedule':False}
    output=Path(os.environ.get('ADDP_HTTP_BUDGET_REPORT',str(Path(tempfile.gettempdir())/('addp-http-metrics-'+uuid.uuid4().hex+'.json'))))
    if not output.is_absolute() or output.is_relative_to(ROOT) or output.exists() or output.with_suffix('.started').exists():
        raise RuntimeError('http_budget_receipt_exists')
    with output.with_suffix('.started').open('x') as receipt:
        json.dump({'started_monotonic':started,'attempt_limit':1},receipt)
    with tempfile.TemporaryDirectory(prefix='addp-http-budget-') as directory:
        ownership_path=Path(directory)/'ownership.json'
        child=subprocess.Popen(command,env=dict(os.environ,ADDP_HTTP_BUDGET_RECEIPT=str(ownership_path)),start_new_session=True)
        error=None;last_sample=None;ownership=None
        try:
            while child.poll() is None:
                now=time.monotonic()
                if now-started>=1080:raise RuntimeError('http_budget_work_deadline')
                if ownership_path.exists():
                    ownership=json.loads(ownership_path.read_text())
                    if not ownership.get("cleanup_started") and (last_sample is None or now-last_sample>=4):
                        if last_sample is not None and now-last_sample>5:raise RuntimeError('http_budget_sample_cadence')
                        last_sample=now
                        report['samples'].append(sample(ownership))
                time.sleep(.1)
        except BaseException as failure:
            error=type(failure).__name__ if not isinstance(failure,RuntimeError) else str(failure)
            try:os.killpg(child.pid,signal.SIGTERM)
            except ProcessLookupError:pass
        finally:
            # Mandatory owned cleanup is allowed to finish after work cutoff;
            # an overdue/failed run remains failed, never renewed or retried.
            try:result=child.wait(timeout=120)
            except subprocess.TimeoutExpired:
                report['cleanup_overdue']=True
                result=child.wait()
            report.update(error_code=error,exit_code=result,elapsed_seconds=time.monotonic()-started,finished_at=datetime.now(timezone.utc).isoformat())
            if ownership_path.exists():
                ownership=json.loads(ownership_path.read_text())
            if ownership:
                report['project']=ownership['project']
                report['private_work_path']=ownership['work']
                report['request_counts']=ownership.get('request_counts')
                report['cleanup_verified']=ownership.get('cleanup_verified',False)
                report['private_directory_removed']=not Path(ownership['work']).exists()
            report['inputs_unchanged']=input_hashes()==inputs
            if result==0 and error is None and report['inputs_unchanged'] and report.get('cleanup_verified') and report.get('request_counts') and report['elapsed_seconds']<1200 and report.get('private_directory_removed') and report['samples']:
                report['result']='passed'
            output.write_text(json.dumps(report,indent=2))
    print('HTTP functional budget report: '+str(output),flush=True)
    return 0 if report['result']=='passed' else 1


if __name__=='__main__':raise SystemExit(main())
