"""Explicit local capacity mode of the sole Monitor Trace T2 lifecycle.

No shortened retention, flush endpoint, direct object writes or Docker socket in
producers. All subprocesses are bounded by the run deadline. Reports contain
synthetic counts only and survive the gate's credential/volume cleanup.
"""
from pathlib import Path
import json
import math
import os
import platform
import re
import ssl
import subprocess
import sys
import time
import urllib.request
import uuid

ROOT = Path(__file__).resolve().parents[2]
GIB = 1024 ** 3
LIMIT_SECONDS = 2400


def metrics(text):
    result = {}
    for line in text.splitlines():
        if not line or line.startswith('#'):
            continue
        key, value = line.rsplit(None, 1)
        value = float(value)
        if not math.isfinite(value):
            continue
        if key.startswith(('tempo_', 'otelcol_')):
            result[key] = value
    return result


def total(values, name, labels=None, required=False):
    found = [v for k, v in values.items()
             if k.split('{')[0].removesuffix('_total') == name.removesuffix('_total')
             and all(f'{label}="{value}"' in k for label, value in (labels or {}).items())]
    if required and not found:
        raise ValueError('missing metric: ' + name + ' ' + str(labels))
    return sum(found)


def forecast(phases):
    if len(phases) != 2:
        raise ValueError('both complete phases are required')
    sizes = []
    for phase in phases:
        spans = phase['retained_spans']
        if phase['physical_trace_bytes'] <= 0 or phase['load']['duration_seconds'] < 599:
            raise ValueError('invalid physical size or load duration')
        if (phase.get('verified', {}).get('queried_complete_traces') != 20
                or not 0 <= phase['verified'].get('query_seconds', float('inf')) <= 5):
            raise ValueError('authorized query sample did not complete')
        if spans < 10000 or phase['active_block_traces'] * 5 != spans:
            raise ValueError('insufficient or unflushed retained sample')
        sizes.append(phase['physical_trace_bytes'] / spans)
    # Count physical blocks and indices, and reserve another complete copy for
    # compaction overlap that a 20-minute input window cannot fully observe.
    worst = max(sizes)
    rate = max(100, *(p['stage_retained_spans_per_second'] for p in phases))
    projected = math.ceil(worst * rate * 259200 * 2)
    workspace = max(p['workspace_peak_bytes'] for p in phases)
    return dict(physical_bytes_per_span=worst, compaction_copy_factor=2,
                budget_rate=rate, projected_72h_bytes=projected,
                acceptance_bytes=16*GIB, max_sustainable_spans_per_second=16*GIB/(worst*259200*2),
                workspace_peak_bytes=workspace, workspace_reservation_bytes=5*GIB,
                capacity_pass=projected <= 16*GIB and workspace <= 5*GIB,
                actual_72h_retention_verified=False)


class Capacity:
    def __init__(self, work):
        self.work = Path(work).resolve()
        self.begin = time.monotonic()
        self.base = ['docker', 'compose', '--env-file', '/dev/null', '-p', os.environ['TRACE_T2_PROJECT'],
                     '-f', os.environ['TRACE_T2_COMPOSE']]
        self.report_path = Path(os.environ.get('MONITOR_TRACE_CAPACITY_REPORT',
                               str(self.work.parent / ('addp-trace-capacity-' + uuid.uuid4().hex + '.json')))).absolute()
        if self.report_path.is_relative_to(ROOT) or self.report_path.is_relative_to(self.work) or self.report_path.exists():
            raise ValueError('report must be a fresh absolute path outside repository and cleanup directory')
        self.report = dict(status='running', phases=[], samples=[], policy=json.loads(
            (ROOT/'monitor/backend/internal/tracequery/policy.json').read_text()),
            scope='local disposable T2; 5-span Go HTTP chain; no Hosted',
            caps=dict(cpu=6, memory_bytes=6*GIB, volume_stop_bytes=2*GIB, duration_seconds=2400))
        self.producer = None
        (self.work/"capacity-report.path").write_text(str(self.report_path))
        self.save()

    def save(self):
        self.report_path.write_text(json.dumps(self.report, ensure_ascii=False, indent=2)+'\n')
        self.report_path.chmod(0o600)

    def run(self, *args, timeout=30, env=None):
        remaining = LIMIT_SECONDS - (time.monotonic()-self.begin)
        if remaining <= 0:
            raise RuntimeError('40-minute run deadline exceeded')
        return subprocess.check_output(args, text=True, stderr=subprocess.PIPE,
                                       timeout=min(timeout, remaining), env=env, cwd=ROOT)

    def compose(self, *args, **kwargs):
        return self.run(*self.base, *args, **kwargs)

    def minio(self, command):
        return self.compose('exec', '-T', 'minio', 'sh', '-ec', command)

    def tempo_metrics(self):
        return metrics(self.minio('curl -fsS --max-time 4 http://tempo:3200/metrics'))

    def objects(self):
        out = self.minio('mc alias set capacity http://127.0.0.1:9000 "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD" >/dev/null; mc ls --recursive --json capacity/addp-runtime-traces-test')
        objects = [json.loads(row) for row in out.splitlines() if row]
        if any(o.get('status') != 'success' for o in objects):
            raise RuntimeError('object listing failed')
        meta = []
        for obj in objects:
            key = obj['key']
            if key.endswith('/meta.json'):
                if not re.fullmatch(r'single-tenant/[a-f0-9-]{36}/meta.json', key):
                    raise ValueError('unexpected synthetic block key')
                meta.append(json.loads(self.minio('mc cat capacity/addp-runtime-traces-test/'+key)))
        return dict(object_bytes=sum(o.get('size',0) for o in objects),
                    object_count=len(objects), active_block_traces=sum(m['totalObjects'] for m in meta),
                    block_meta=meta, compacted_meta_objects=sum(o['key'].endswith('/meta.compacted.json') for o in objects))

    def sample(self):
        ids = self.run('docker','ps','-aq','--filter','label=com.docker.compose.project='+os.environ['TRACE_T2_PROJECT']).split()
        states = json.loads(self.run('docker','inspect',*ids))
        cpu = ram = 0
        for c in states:
            service=c['Config']['Labels']['com.docker.compose.service']
            state=c['State']
            if state['OOMKilled'] or (state['Status']=='exited' and state['ExitCode'] != 0):
                raise RuntimeError('owned container failed/OOM: '+service)
            if state['Running']:
                conf=c['HostConfig']; cpu+=conf['NanoCpus']/1e9; ram+=conf['Memory']
                if conf['NanoCpus']<=0 or conf['Memory']<=0:
                    raise RuntimeError('unbounded owned container: '+service)
        if cpu>6 or ram>6*GIB:
            raise RuntimeError('aggregate resource cap exceeded')
        du=self.minio('du -sk /data /capacity-tempo /capacity-loki /capacity-alloy; du -sk /data/addp-runtime-traces-test /data/.minio.sys')
        sizes={path:int(size)*1024 for size,path in (row.split() for row in du.splitlines())}
        volumes=sum(sizes[p] for p in ['/data','/capacity-tempo','/capacity-loki','/capacity-alloy'])
        if volumes>=2*GIB:
            raise RuntimeError('2 GiB volume stop line reached')
        tempo=self.tempo_metrics()
        with urllib.request.urlopen(self.alloy+'/metrics',timeout=4) as response:
            alloy=metrics(response.read().decode())
        for name in ['tempo_discarded_spans_total','tempo_ingester_failed_flushes_total','tempo_ingester_replay_errors_total']:
            if total(tempo,name)>0: raise RuntimeError('Tempo loss: '+name)
        for name in ['otelcol_receiver_refused_spans_total','otelcol_exporter_send_failed_spans_total',
                     'otelcol_processor_tail_sampling_sampling_trace_dropped_too_early_total',
                     'otelcol_processor_tail_sampling_sampling_policy_evaluation_error_total']:
            if total(alloy,name)>0: raise RuntimeError('Alloy loss: '+name)
        stats=[json.loads(row) for row in self.run('docker','stats','--no-stream','--format','{{json .}}',
                                                 *[c['Id'] for c in states if c['State']['Running']]).splitlines()]
        sample=dict(elapsed_seconds=round(time.monotonic()-self.begin,2),volume_bytes=volumes,
                    physical_trace_bytes=sizes['/data/addp-runtime-traces-test']+sizes['/data/.minio.sys'],
                    workspace_bytes=sizes['/capacity-tempo'],cpu_limit=cpu,memory_limit=ram,stats=stats,
                    tempo=tempo,alloy=alloy)
        self.report['samples'].append(sample);self.save()
        if self.producer and self.producer.poll() is not None and not (self.work/'capacity-done-2.json').exists():
            raise RuntimeError('load process exited before both phases completed')
        return sample

    def wait_file(self, name, seconds):
        deadline=time.monotonic()+seconds
        while time.monotonic()<deadline:
            if (self.work/name).exists():
                return json.loads((self.work/name).read_text())
            self.sample();time.sleep(5)
        raise RuntimeError('phase deadline: '+name)

    def execute(self):
        print('Capacity: compile bounded Linux producer fixture',flush=True)
        # Record content hashes, not private file contents or developer env.
        import hashlib
        self.report['inputs']={p:hashlib.sha256((ROOT/p).read_bytes()).hexdigest() for p in [
            'scripts/infra/traces.alloy','scripts/infra/tempo.yml','scripts/test/trace-capacity.py',
            'monitor/backend/internal/service/traces_capacity_test.go','scripts/test/docker-compose.monitor-traces-t2.yml']}
        env=os.environ|{'GOOS':'linux','GOARCH':{'arm64':'arm64','aarch64':'arm64','x86_64':'amd64'}.get(platform.machine(),platform.machine()),'CGO_ENABLED':'0','GOMAXPROCS':'2'}
        # This binary is an owned test fixture, never a development server.
        self.run('go','-C','monitor/backend','test','-p=2','-c','-o',str(self.work/'trace-capacity.test'),'./internal/service',timeout=480,env=env)
        print('Capacity: build and start owned collectors/stores',flush=True)
        self.compose('build','minio','runtime-log-store-init','trace-store-init','trace-load',timeout=480)
        self.compose('run','--rm','--no-deps','alloy','validate','/trace-deployment/runtime.alloy',timeout=60)
        self.compose('up','-d','--wait','--wait-timeout','180','alloy',timeout=200)
        self.alloy='http://'+self.compose('port','alloy','12345').strip()
        # Readiness before first sample; no personal/shared endpoint is used.
        self.sample()
        log=open(self.work/'capacity-producer.log','w')
        self.producer=subprocess.Popen(self.base+['run','--rm','--no-deps','trace-load'],stdout=log,stderr=subprocess.STDOUT,cwd=ROOT)
        try:
            self.wait_file('capacity-ready.json',60)
            previous_spans=0
            for n in (1,2):
                print('Capacity: begin 600-second load phase',n,flush=True)
                (self.work/f'capacity-start-{n}').touch()
                load=self.wait_file(f'capacity-loaded-{n}.json',620)
                self.report.setdefault('loads',[]).append(load);self.save()
                # Real tail wait + natural 5-minute block rotation. Require all
                # retained traces in object blocks before using the denominator.
                print('Capacity: phase',n,'load complete; wait for real tail decision and natural flush',flush=True)
                loaded=time.monotonic();deadline=loaded+470
                while time.monotonic()<deadline:
                    sample=self.sample()
                    spans=total(sample['tempo'],'tempo_distributor_spans_received_total',required=True)
                    if time.monotonic()-loaded>=150:
                        obj=self.objects()
                        exported=total(sample['alloy'],'otelcol_exporter_sent_spans_total',required=True)
                        if obj['active_block_traces']*5==spans and spans==exported and spans>previous_spans:
                            break
                    time.sleep(5)
                else: raise RuntimeError('natural object flush incomplete')
                received=spans-previous_spans
                if received<10000: raise RuntimeError('retained sample insufficient')
                phase=dict(load=load,verified={},retained_spans=spans,
                           phase_retained_spans=received,physical_trace_bytes=sample['physical_trace_bytes'],
                           active_block_traces=obj['active_block_traces'],objects=obj,
                           workspace_peak_bytes=max(s['workspace_bytes'] for s in self.report['samples']))
                # Cumulative byte/span cost uses cumulative stored denominator;
                # actual stage rates use only that phase's delta.
                phase['stage_retained_spans_per_second']=received/load['duration_seconds']
                self.report['phases'].append(phase);self.save()
                (self.work/f'capacity-verify-{n}').touch()
                phase['verified']=self.wait_file(f'capacity-done-{n}.json',30)
                previous_spans=spans;self.save()
                print('capacity phase',n,'stored spans',spans,'physical bytes',sample['physical_trace_bytes'],flush=True)
            self.producer.wait(timeout=15)
            if self.producer.returncode!=0: raise RuntimeError('load fixture failed')
        finally:
            log.close()
        self.report['forecast']=forecast(self.report['phases'])
        self.report['status']='passed' if self.report['forecast']['capacity_pass'] else 'capacity_failed'
        self.save()
        print(json.dumps(self.report['forecast'],ensure_ascii=False),flush=True)
        if self.report['status']!='passed': raise RuntimeError('20 GiB capacity acceptance failed')


def main():
    c=Capacity(sys.argv[1])
    try:
        c.execute()
    except BaseException as e:
        c.report['status']='failed';c.report['error']=str(e)[:500];c.save()
        if c.producer and c.producer.poll() is None:
            c.producer.terminate()
            try:
                c.producer.wait(timeout=10)
            except subprocess.TimeoutExpired:
                c.producer.kill()
                c.producer.wait(timeout=5)
        # Synthetic fixture output only; no environment or TLS material.
        p=c.work/'capacity-producer.log'
        if p.exists():print(p.read_text(errors='replace')[-6000:],file=sys.stderr)
        raise
    finally:
        print('Capacity report:',c.report_path,flush=True)

if __name__=='__main__':main()
