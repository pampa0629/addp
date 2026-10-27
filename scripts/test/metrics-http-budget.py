"""The existing HTTP T2 case has one local resource/request window, no retries."""
import json
import fcntl
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


class BudgetFailure(RuntimeError):
    """Closed diagnostic facts only; never retain commands or daemon output."""
    def __init__(self, code, stage, kind, **facts):
        super().__init__(code)
        self.detail = {'stage': stage, 'kind': kind, **facts}


def validate_request_phase(value, limit):
    keys={'phase','failure_attempts','healthy_attempts','failure_limit','healthy_limit','failure_last_ms','healthy_last_ms'}
    if not isinstance(value,dict) or set(value)!=keys or value['phase'] not in ('failure','healthy'):
        raise RuntimeError('http_request_phase_invalid')
    for phase in ('failure','healthy'):
        count=value[phase+'_attempts'];maximum=value[phase+'_limit'];last=value[phase+'_last_ms']
        if type(count) is not int or not 0<=count<=limit or type(maximum) is not int or maximum!=limit:
            raise RuntimeError('http_request_phase_invalid')
        if (count==0 and last is not None) or (count>0 and (type(last) is not int or not 0<=last<=600000)):
            raise RuntimeError('http_request_phase_invalid')
    if value['phase']=='failure' and value['healthy_attempts']!=0:
        raise RuntimeError('http_request_phase_invalid')
    return value['failure_attempts']+value['healthy_attempts']


def request_traffic(source):
    """Read only after the owned producers stop, before their directory is removed."""
    counts=[];phases={}
    for key,stem,identity,limit in (('go','process-1.json','process-1.json',90),('python_1','python-1','python-1.record.json',45),('python_2','python-2','python-2.record.json',15)):
        counter=source/(stem+'.requests');record=source/(stem+'.http-load.json')
        count=0
        if counter.exists():
            with counter.open('rb') as stream:text=stream.read(4)
            if len(text)>3 or not re.fullmatch(rb'[0-9]+',text):raise RuntimeError('http_request_count_invalid')
            count=int(text)
        proof=None
        if record.exists() and record.stat().st_size:
            if record.stat().st_size>1024:raise RuntimeError('http_request_phase_invalid')
            try:proof=json.loads(record.read_text())
            except (ValueError,UnicodeError):raise RuntimeError('http_request_phase_invalid') from None
            if validate_request_phase(proof,limit)!=count:raise RuntimeError('http_request_count_mismatch')
        elif count or (source/identity).exists() and (source/identity).stat().st_size:
            raise RuntimeError('http_request_phase_missing')
        counts.append(count);phases[key]=proof
    return {'request_counts':{'go':counts[0],'python':counts[1:],'total':sum(counts)},'request_phases':phases}


def complete_request_traffic(counts, phases):
    try:
        if (not isinstance(counts,dict) or set(counts)!={'go','python','total'} or type(counts['go']) is not int
                or type(counts['total']) is not int or type(counts['python']) is not list or len(counts['python'])!=2
                or any(type(value) is not int for value in counts['python'])):return False
        if not isinstance(phases,dict) or set(phases)!={'go','python_1','python_2'}:return False
        totals=[validate_request_phase(phases[key],limit) for key,limit in (('go',90),('python_1',45),('python_2',15))]
        return (all(p['failure_attempts']>0 and p['healthy_attempts']>0 and p['phase']=='healthy' for p in phases.values())
                and counts=={'go':totals[0],'python':totals[1:],'total':sum(totals)})
    except (RuntimeError,TypeError):
        return False


def resource_totals(rows, work_bytes, volume_bytes):
    cpu=memory=writable=tmpfs=0
    for row in rows:
        config=row['HostConfig'];state=row['State']
        if state.get('OOMKilled') or state.get('Status') in ('dead','restarting'):
            raise BudgetFailure('http_budget_container_failure','resource_totals','container_failure')
        cores=config.get('NanoCpus',0)/1e9
        if not cores and config.get('CpuPeriod',0)>0:cores=config.get('CpuQuota',0)/config['CpuPeriod']
        limit=config.get('Memory',0)
        if cores<=0 or limit<=0:raise BudgetFailure('http_budget_hard_limit_missing','resource_totals','hard_limit_missing')
        cpu+=cores;memory+=limit;writable+=row.get('SizeRw',0)
        for mount,options in (config.get('Tmpfs') or {}).items():
            match=re.search(r'(?:^|,)size=([0-9]+)([kmg]?)(?:,|$)',options.lower())
            if not match:raise BudgetFailure('http_budget_tmpfs_unbounded','resource_totals','tmpfs_unbounded')
            tmpfs+=int(match[1])*{'':1,'k':1024,'m':1024**2,'g':1024**3}[match[2]]
    total=work_bytes+volume_bytes+writable+tmpfs
    facts={'cpu_hard_limit':cpu,'memory_hard_limit_bytes':memory,'private_write_upper_bytes':total,
           'work_bytes':work_bytes,'volume_bytes':volume_bytes,'writable_layers_bytes':writable,'tmpfs_reserved_bytes':tmpfs}
    violations=[name for name,exceeded in [('cpu',cpu>4),('memory',memory>4*1024**3),('private_write',total>=2*1024**3)] if exceeded]
    if violations:
        raise BudgetFailure('http_budget_limit','resource_totals','limit_exceeded',violations=violations,measurements=facts)
    return facts


def directory_failure_diagnostic(stderr):
    """Classify bounded CLI text, never infer errno or retain any raw prefix."""
    result={'directory_error':'empty','stderr_bytes':len(stderr),'stderr_over_budget':len(stderr)>4096}
    if result['stderr_over_budget']:
        result['directory_error']='over_budget'
        return result
    suffixes={b'No such file or directory':'path_disappeared',b'Permission denied':'permission_denied',
              b'Operation not permitted':'permission_denied',b'Input/output error':'io_error'}
    kinds={suffixes.get(line.rpartition(b': ')[2],'unclassified') for line in stderr.splitlines() if line.strip()}
    if kinds:result['directory_error']=next(iter(kinds)) if len(kinds)==1 else 'mixed'
    return result


def acquire_lock(fd, end, stage, stopped):
    while True:
        stop=stopped()
        remaining=end-time.monotonic()
        if remaining<=0:raise BudgetFailure('http_budget_handoff_deadline',stage,'deadline')
        if stop:return False
        try:
            fcntl.flock(fd,fcntl.LOCK_EX|fcntl.LOCK_NB)
            return True
        except BlockingIOError:time.sleep(min(.01,remaining))
        except OSError:raise BudgetFailure('http_budget_handoff_failed',stage,'lock_unavailable') from None


def begin_cleanup(receipt, fd):
    began=time.monotonic()
    value=json.loads(receipt.read_text())
    value['cleanup_started']=True
    value['cleanup_deadline_monotonic']=began+120
    pending=receipt.with_suffix('.pending');pending.write_text(json.dumps(value));pending.replace(receipt)
    try:acquire_lock(fd,value['cleanup_deadline_monotonic'],'cleanup_handoff',lambda:False)
    except BaseException:
        finished=time.monotonic()
        value.update(cleanup_verified=False,cleanup_overdue=finished>=value['cleanup_deadline_monotonic'],cleanup_lock_wait_seconds=finished-began)
        pending.write_text(json.dumps(value));pending.replace(receipt)
        raise
    value['cleanup_lock_wait_seconds']=time.monotonic()-began
    pending.write_text(json.dumps(value));pending.replace(receipt)
    return value


def owned_sample(ownership,receipt):
    began=time.monotonic();end=began+4
    def stopped():
        latest=json.loads(receipt.read_text())
        if any(latest.get(k)!=ownership.get(k) for k in ('project','work')):
            raise RuntimeError('http_budget_ownership_invalid')
        return latest.get('cleanup_started') is True
    with open(str(receipt)+'.lock','a') as lock:
        try:
            if not acquire_lock(lock.fileno(),end,'sample_handoff',stopped):return None
            cleaning=stopped()
            if time.monotonic()>=end:raise BudgetFailure('http_budget_handoff_deadline','sample_handoff','deadline')
            if cleaning:return None
            waited=time.monotonic()-began
            result=sample(ownership,end)
            if time.monotonic()>=end:raise BudgetFailure('http_budget_sample_deadline','sample_handoff','deadline')
            result['lock_wait_seconds']=waited
            return result
        finally:
            primary=sys.exc_info()[0] is not None
            try:fcntl.flock(lock.fileno(),fcntl.LOCK_UN)
            except OSError:
                if not primary:raise BudgetFailure('http_budget_handoff_failed','sample_handoff','unlock_failed') from None


def sample(ownership,end):
    began=end-4
    def command(stage,args):
        started=time.monotonic();remaining=end-started
        def failed(code,kind,finished,**facts):
            raise BudgetFailure(code,stage,kind,command_seconds=round(finished-started,6),
                                sample_seconds=round(finished-began,6),command_budget_seconds=round(max(0,remaining),6),**facts) from None
        if remaining<=0:failed('http_budget_sample_deadline','deadline',started)
        options={'timeout':remaining,'stderr':subprocess.PIPE}
        if stage=='private_directory_size':options['env']=dict(os.environ,LC_ALL='C')
        try:
            value=subprocess.check_output(args,**options)
        except subprocess.TimeoutExpired:
            failed('http_budget_sample_deadline','deadline',time.monotonic())
        except subprocess.CalledProcessError as failure:
            finished=time.monotonic()
            missing=stage in {'inspect','volume_size'} and b'No such container' in (failure.stderr or b'')
            diagnostic=directory_failure_diagnostic(failure.stderr or b'') if stage=='private_directory_size' else {}
            failed('http_budget_sample_command_failed','container_disappeared' if missing else 'command_failed',
                   finished,exit_code=failure.returncode,**diagnostic)
        except OSError:
            failed('http_budget_sample_command_failed','command_unavailable',time.monotonic())
        finished=time.monotonic()
        if finished>=end:failed('http_budget_sample_deadline','deadline',finished)
        try:
            return value.decode()
        except UnicodeError:
            failed('http_budget_sample_invalid','invalid_output',finished)
    def size(stage,args):
        text=command(stage,args)
        try:
            result=int(text.split()[0])*1024
            if result<0:raise ValueError()
            return result
        except (IndexError,ValueError):
            raise BudgetFailure('http_budget_sample_invalid',stage,'invalid_size') from None
    project,work=ownership['project'],Path(ownership['work'])
    if not re.fullmatch(r'addp-metrics-t2-[0-9a-f]{32}',project) or not work.is_absolute() or not work.name.startswith('addp-metrics-t2.') or work.is_relative_to(ROOT):
        raise RuntimeError('http_budget_ownership_invalid')
    ids=command('inventory',['docker','ps','-aq','--filter','label=com.docker.compose.project='+project]).split()
    try:
        rows=json.loads(command('inspect',['docker','inspect','--size',*ids])) if ids else []
    except json.JSONDecodeError:
        raise BudgetFailure('http_budget_sample_invalid','inspect','invalid_output') from None
    volume=0
    for row in rows:
        if row.get('Config',{}).get('Labels',{}).get('com.docker.compose.service')=='prometheus' and row['State'].get('Running'):
            volume=size('volume_size',['docker','exec',row['Id'],'du','-sk','/prometheus'])
    disk=size('private_directory_size',['du','-sk',str(work)]) if work.exists() else 0
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
    if args not in (['--case=http-quality'],['--case=mtls-preflight']):
        os.execvpe('bash',command,os.environ)
    started=time.monotonic()
    wall_started=datetime.now(timezone.utc).isoformat()
    inputs=input_hashes()
    report={'case':args[0].split('=',1)[1],'started_at':wall_started,'input_sha256':inputs,'result':'failed','maximum_seconds':1200,'samples':[],'hosted':False,'capacity':False,'schedule':False}
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
                        reading=owned_sample(ownership,ownership_path)
                        if reading is not None:report['samples'].append(reading)
                time.sleep(.1)
        except BaseException as failure:
            error=type(failure).__name__ if not isinstance(failure,RuntimeError) else str(failure)
            if isinstance(failure,BudgetFailure):report['failure_detail']=failure.detail
            # Keep every read failure. Cleanup can already be waiting for the
            # released sample lock; do not interrupt that mandatory cleanup.
            cleaning=False
            try:
                latest=json.loads(ownership_path.read_text())
                cleaning=bool(ownership is not None and isinstance(latest.get('project'),str) and
                              re.fullmatch(r'addp-metrics-t2-[0-9a-f]{32}',latest['project']) and
                              isinstance(latest.get('work'),str) and latest.get('project')==ownership.get('project') and
                              latest.get('work')==ownership.get('work') and latest.get('cleanup_started') is True)
            except (OSError,ValueError,AttributeError):pass
            report['failure_during_cleanup']=cleaning
            if not cleaning:
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
                report['cleanup_lock_wait_seconds']=ownership.get('cleanup_lock_wait_seconds')
                if ownership.get('cleanup_overdue'):report['cleanup_overdue']=True
                cleanup_end=ownership.get('cleanup_deadline_monotonic')
                if cleanup_end is not None and time.monotonic()>=cleanup_end:report['cleanup_overdue']=True
                report['project']=ownership['project']
                report['private_work_path']=ownership['work']
                report['request_counts']=ownership.get('request_counts')
                report['request_phases']=ownership.get('request_phases')
                report['request_traffic_error']=ownership.get('request_traffic_error')
                report['python_sources']=ownership.get('python_sources')
                report['source_tls_proof']=ownership.get('source_tls_proof')
                report['source_tls_publication']=ownership.get('source_tls_publication')
                report['cleanup_verified']=ownership.get('cleanup_verified',False)
                report['private_directory_removed']=not Path(ownership['work']).exists()
            report['inputs_unchanged']=input_hashes()==inputs
            preflight=report['case']=='mtls-preflight'
            tls_proven=(report.get('source_tls_proof') or {}).get('passed') is True if preflight else True
            counts_proven=report.get('request_counts')=={'go':0,'python':[0,0],'total':0} if preflight else complete_request_traffic(report.get('request_counts'),report.get('request_phases'))
            if result==0 and error is None and not report.get('cleanup_overdue') and tls_proven and counts_proven and report['inputs_unchanged'] and report.get('cleanup_verified') and report['elapsed_seconds']<1200 and report.get('private_directory_removed') and report['samples']:
                report['result']='passed'
            output.write_text(json.dumps(report,indent=2))
    summary={key:report.get(key) for key in ('case','result','error_code','failure_detail','exit_code','elapsed_seconds','source_tls_proof','source_tls_publication',
             'cleanup_verified','cleanup_overdue','cleanup_lock_wait_seconds','private_directory_removed','inputs_unchanged','request_counts','request_phases','request_traffic_error','python_sources','failure_during_cleanup')}
    print('HTTP functional budget result: '+json.dumps(summary,sort_keys=True),flush=True)
    print('HTTP functional budget report: '+str(output),flush=True)
    return 0 if report['result']=='passed' else 1


if __name__=='__main__':raise SystemExit(main())
