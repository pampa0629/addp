#!/usr/bin/env python3
"""Manual formal-identity Trace acceptance on a disposable local deployment.

This entry never dispatches Online/Hosted, touches personal lifecycle state, or
runs a capacity model. Product images and Compose contracts remain authoritative.
"""
from __future__ import annotations

import base64
import copy
import hashlib
import importlib
import json
import os
from pathlib import Path
import platform
import re
import secrets
import shutil
import signal
import socket
import subprocess
import sys
import tempfile
import threading
import time
import uuid

ROOT = Path(__file__).resolve().parents[2]
LABEL = 'com.addp.execution-trace-local'
BACKENDS = ('system-backend', 'gateway', 'security-backend', 'develop-backend', 'orchestrator-backend', 'monitor-backend')
FRONTENDS = ('console', 'system-frontend', 'manager-frontend', 'meta-frontend', 'transfer-frontend',
             'orchestrator-frontend', 'develop-frontend', 'service-frontend', 'monitor-frontend', 'nginx')
TRACE = ('minio', 'runtime-log-store-init', 'loki', 'runtime-log-api', 'trace-store-init', 'tempo', 'trace-query', 'alloy')
# All concurrently running services, browser and the sampling helper fit 8 CPU/12 GiB.
LIMITS = {name: (.25, 512) for name in BACKENDS} | {name: (.025, 32) for name in FRONTENDS} | {
    'system-backend': (.5, 768), 'postgres': (.5, 768), 'business-postgres': (.25, 256), 'redis': (.1, 128),
    'minio': (.5, 512), 'runtime-log-store-init': (.1, 128), 'loki': (.5, 512), 'runtime-log-api': (.1, 64),
    'trace-store-init': (.1, 128), 'tempo': (2, 2048), 'trace-query': (.25, 128), 'alloy': (1, 1024)}
OWN_INPUTS = (
    'Makefile', 'scripts/ci/check-online-ci-registration.py', 'scripts/ci/check-execution-trace-local-registration_test.py',
    'scripts/test/monitor-traces-local-gate.py', 'scripts/test/monitor-traces-local-gate_test.py',
    'scripts/test/monitor-traces-local-online.py', 'scripts/test/monitor-traces-local-online_test.py',
    'scripts/test/monitor-traces-gate.sh', 'common/httptrace/execution.go', 'common/httptrace/execution_test.go',
    'common/httptrace/tracetest/runtime.go', 'develop/backend/internal/repository/execution_repository.go',
    'develop/backend/internal/repository/execution_trace.go', 'develop/backend/internal/repository/execution_trace_test.go',
    'develop/backend/internal/service/execution_trace.go', 'develop/backend/internal/service/execution_trace_test.go',
    'orchestrator/backend/internal/service/execution_advance.go', 'orchestrator/backend/internal/service/execution_ownership.go',
    'orchestrator/backend/internal/service/execution_service.go', 'orchestrator/backend/internal/service/execution_trace.go',
    'orchestrator/backend/internal/service/execution_trace_test.go', 'system/backend/go.mod', 'system/backend/go.sum', 'system/backend/cmd/online-test-fixture/main.go',
    'system/backend/cmd/online-test-fixture/execution_trace_local.go', 'system/backend/cmd/online-test-fixture/execution_trace_local_test.go',
    'console/frontend/e2e/online/execution-trace-local.spec.js', 'console/frontend/e2e/online/execution-trace-local-support.mjs',
    'console/frontend/tests/online/executionTraceLocalBrowser.test.js',
)
MAX_SECONDS = 3600
VOLUME_STOP = 4 * 1024**3


class GateError(RuntimeError):
    """Only fixed, non-secret diagnostics cross the private evidence boundary."""


def require(condition, code):
    if not condition:
        raise GateError(code)


def module(name, root=ROOT):
    import importlib.util
    spec = importlib.util.spec_from_file_location(name.replace('-', '_'), root / 'scripts/test' / (name + '.py'))
    value = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = value
    spec.loader.exec_module(value)
    return value


def safe_environment():
    return {k: v for k, v in os.environ.items() if k in {'PATH', 'HOME', 'TMPDIR', 'DOCKER_HOST', 'DOCKER_CONTEXT'}}


def validate_profile(environment, machine, daemon):
    require(not any(environment.get(k) for k in ('GITHUB_ACTIONS', 'ADDP_ONLINE_HOSTED', 'ADDP_ONLINE_OWNER_MANAGED',
                                                'ADDP_ONLINE_HOST', 'ADDP_ONLINE_LOCAL_SGM')), 'github_or_online_profile_forbidden')
    require(machine in ('arm64', 'aarch64') and daemon in ('arm64', 'aarch64'), 'native_arm64_docker_required')


def effective_docker_endpoint(environment, context_reader):
    # DOCKER_CONTEXT takes precedence over DOCKER_HOST in the Docker CLI.
    if environment.get('DOCKER_CONTEXT'):
        endpoint=context_reader(environment['DOCKER_CONTEXT'])
    elif environment.get('DOCKER_HOST'):
        endpoint=environment['DOCKER_HOST']
    else:
        endpoint=context_reader(None)
    require(isinstance(endpoint,str) and endpoint.startswith('unix://') and endpoint[7:].startswith('/'), 'local_unix_docker_required')
    return endpoint


def private_docker_client(life, endpoint):
    directory=life.work/'docker-client'
    directory.mkdir(mode=0o700)
    config={'auths':{},'cliPluginsExtraDirs':[str(Path.home()/'.docker/cli-plugins')]}
    path=directory/'config.json'
    with open(path,'x',opener=lambda p,f:os.open(p,f,0o600)) as stream:
        json.dump(config,stream)
    life.env.pop('DOCKER_CONTEXT',None)
    life.env.update(DOCKER_HOST=endpoint,DOCKER_CONFIG=str(directory))
    return directory


def validate_resources(rows, allowed_exits=()):
    cpu = memory = 0
    for row in rows:
        host, state = row['HostConfig'], row['State']
        require(not state.get('OOMKilled'), 'owned_container_oom')
        if state.get('Status') in ('exited', 'dead'):
            require(state.get('ExitCode') == 0, 'owned_container_failed')
            labels=row.get('Config',{}).get('Labels',{}) or {}
            require(row.get('Id') in allowed_exits or labels.get('com.docker.compose.service') in ('runtime-log-store-init','trace-store-init') or labels.get(LABEL+'.role')=='oneshot', 'owned_service_exited')
        if not state.get('Running'):
            continue
        quota = host.get('NanoCpus', 0) / 1e9
        if not quota and host.get('CpuQuota', 0) > 0 and host.get('CpuPeriod', 0) > 0:
            quota = host['CpuQuota'] / host['CpuPeriod']
        require(quota > 0 and host.get('Memory', 0) > 0, 'owned_container_without_hard_limits')
        cpu += quota
        memory += host['Memory']
    require(cpu <= 8 + 1e-7 and memory <= 12 * 1024**3, 'aggregate_hard_limit_exceeded')
    return cpu, memory


def isolate(document, run_id, images):
    result = {'name': run_id, 'services': {}, 'networks': {
        'data': {'internal': True, 'labels': {LABEL: run_id}},
        'entry': {'labels': {LABEL: run_id}}}, 'volumes': {}}
    for name, original in document.items():
        service = copy.deepcopy(original)
        require(name in LIMITS and not service.get('privileged') and 'network_mode' not in service, 'unexpected_service_contract')
        service.update(container_name=run_id + '-' + name, image=images.get(name, service.get('image')),
                       restart='no', labels={**service.get('labels', {}), LABEL: run_id},
                       networks={'data': {'aliases': [name]}}, cpus=LIMITS[name][0], mem_limit=f'{LIMITS[name][1]}m')
        require(isinstance(service.get('image'),str) and service['image'], 'every_service_requires_built_image')
        for key in ('build', 'depends_on', 'ports', 'profiles', 'extends'):
            service.pop(key, None)
        mounts = []
        for mount in service.get('volumes', []):
            target = mount['target']
            if target == '/var/log/addp':
                source = 'runtime-logs'
            elif mount['type'] == 'volume':
                source = str(mount['source'])
            else:
                # Only product configuration and this run's private TLS are mounts.
                require(mount.get('read_only') and not target.startswith('/capacity'), 'unexpected_writable_bind')
                mounts.append(mount)
                continue
            result['volumes'][source] = {'labels': {LABEL: run_id}}
            mounts.append({'type': 'volume', 'source': source, 'target': target, 'read_only': mount.get('read_only', False)})
        service['volumes'] = mounts
        if name in ('postgres', 'system-backend', 'gateway', 'nginx'):
            service['networks']['entry'] = {}
            port = {'postgres': 5432, 'system-backend': 8180, 'gateway': 8000, 'nginx': 80}[name]
            service['ports'] = [{'target': port, 'host_ip': '127.0.0.1', 'published': '0'}]
        result['services'][name] = service
    return result


class Lifecycle:
    def __init__(self):
        self.run_id = 'addp-trace-local-' + uuid.uuid4().hex
        self.uuid = str(uuid.uuid4())
        self.work = Path(tempfile.mkdtemp(prefix=self.run_id + '-')).resolve()
        self.work.chmod(0o700)
        require(not self.work.is_relative_to(ROOT), 'private_work_must_be_external')
        self.started = time.monotonic()
        self.phase = 'admission'
        self.builder = self.run_id
        self.builder_ids = set()
        self.builder_created = False
        self.builder_stopped = False
        self.builder_volumes = set()
        self.tags = []
        self.env = safe_environment()
        self.stop = threading.Event()
        self.failure = None
        self.samples = []
        self.thread = None
        self.sample_deadline = None
        self.checkout = self.work / 'source'

    def check(self):
        require(time.monotonic() - self.started < MAX_SECONDS, "total_deadline_exceeded")
        require(self.failure is None, self.failure or "resource_guard_failed")

    def wait(self, seconds):
        deadline = time.monotonic() + seconds
        while time.monotonic() < deadline:
            self.check()
            time.sleep(min(.1, max(0, deadline-time.monotonic())))

    def command(self, args, *, env=None, cwd=None, data=None, timeout=600, guarded=True):
        if guarded:
            require(time.monotonic() - self.started < MAX_SECONDS, 'total_deadline_exceeded')
            require(self.failure is None, self.failure or 'resource_guard_failed')
        with tempfile.TemporaryFile() as output:
            p = subprocess.Popen(args, cwd=cwd or self.checkout if self.checkout.exists() else ROOT,
                                 env=env or self.env, stdin=subprocess.PIPE if data is not None else subprocess.DEVNULL,
                                 stdout=output, stderr=subprocess.STDOUT, start_new_session=True)
            try:
                if data is not None:
                    p.stdin.write(data.encode() if isinstance(data, str) else data)
                    p.stdin.close()
                deadline = time.monotonic() + timeout
                while p.poll() is None:
                    if time.monotonic() > deadline or guarded and (self.failure or time.monotonic() - self.started > MAX_SECONDS):
                        raise GateError(self.failure or 'command_deadline_exceeded')
                    time.sleep(.05)
            finally:
                if p.poll() is None:
                    os.killpg(p.pid, signal.SIGTERM)
                    try:
                        p.wait(timeout=1)
                    except subprocess.TimeoutExpired:
                        os.killpg(p.pid, signal.SIGKILL)
                        p.wait(timeout=1)
            output.seek(0)
            raw = output.read()
        if p.returncode:
            # Keep only an input hash and authored code. Raw output may carry login/SQL details.
            self.last_failure = {'exit_code': p.returncode, 'output_sha256': hashlib.sha256(raw).hexdigest()}
            raise GateError('command_failed_' + str(p.returncode))
        return raw.decode(errors='replace')

    def docker(self, *args, **kw):
        return self.command(['docker', *args], **kw)

    def private_json(self, name, value):
        p = self.work / name
        with open(p, 'x', opener=lambda path, flags: os.open(path, flags, 0o600)) as f:
            json.dump(value, f)
        return p

    def owned(self, kind, **kw):
        return self.docker(kind, 'ls', *(['-a'] if kind == 'container' else []), '-q', '--filter',
                           f'label={LABEL}={self.run_id}', guarded=False, **kw).split()

    def owned_bind_bytes(self, *, timeout):
        output=self.command(['du','-sk',str(self.work)],guarded=False,timeout=timeout)
        return int(output.split()[0])*1024

    def sample(self):
        deadline = time.monotonic()+4
        def remaining():
            value=deadline-time.monotonic()
            require(value>0, 'resource_sample_deadline_exceeded')
            return value
        ids = set(self.owned('container', timeout=remaining())) | self.builder_ids
        rows = json.loads(self.docker('inspect', *sorted(ids), guarded=False, timeout=remaining())) if ids else []
        cpu, memory = validate_resources(rows, self.builder_ids if self.builder_stopped else ())
        volumes = self.owned('volume', timeout=remaining())
        if self.builder_ids:
            for row in rows:
                if row['Id'] in self.builder_ids:
                    builder_volumes = [m['Name'] for m in row.get('Mounts', []) if m['Type'] == 'volume']
                    self.builder_volumes.update(builder_volumes)
                    volumes += builder_volumes
        total = 0
        if volumes:
            # A separate tiny container uses the same immutable MinIO image only for du.
            args = ['run', '--rm', '--name', self.run_id + '-space', '--label', f'{LABEL}={self.run_id}',
                    '--label', LABEL+'.role=oneshot', '--cpus', '.05', '--memory', '64m', '--network', 'none', '--entrypoint', '/bin/sh']
            for i, name in enumerate(sorted(set(volumes))):
                args += ['--mount', f'type=volume,source={name},target=/space/{i},readonly']
            args += [self.minio_image, '-ec', 'du -sk /space/* | awk \'{s+=$1} END {print s*1024}\'']
            total = int(float(self.docker(*args, guarded=False, timeout=remaining()).strip()))
        total += self.owned_bind_bytes(timeout=remaining())
        require(total < VOLUME_STOP, 'owned_volume_stop_line_reached')
        now = time.monotonic()
        self.samples.append({'elapsed_seconds': round(now-self.started, 2), 'cpu_hard_limit': cpu,
                             'memory_hard_limit_bytes': memory, 'volume_allocated_bytes': total})

    def monitor(self):
        while not self.stop.is_set():
            began=time.monotonic()
            try:
                self.sample()
            except Exception as error:
                self.failure = str(error) if isinstance(error, GateError) else 'resource_sample_failed'
                return
            self.stop.wait(max(0, 4-time.monotonic()+began))

    def cleanup(self):
        self.stop.set()
        errors = []
        if self.thread:
            self.thread.join(timeout=6)
            if self.thread.is_alive():
                errors.append('resource_guard_not_stopped')
        try:
            self.docker('buildx', 'rm', '-f', self.builder, guarded=False)
        except GateError:
            # Absence is expected before builder creation; verify below.
            if self.builder_created:
                errors.append('builder')
        try:
            require(not self.docker('ps','-aq','--filter','name=^buildx_buildkit_'+self.builder+'0$',guarded=False).strip(), 'builder_container_residue')
            for volume in self.builder_volumes:
                require(not self.docker('volume','ls','-q','--filter','name=^'+re.escape(volume)+'$',guarded=False).strip(), 'builder_volume_residue')
        except GateError:
            errors.append('builder_residue')
        for kind, operation in [('container', ['rm', '-fv']), ('network', ['rm']), ('volume', ['rm'])]:
            try:
                ids = self.owned(kind)
                if ids:
                    self.docker(kind, *operation, *ids, guarded=False)
                require(not self.owned(kind), kind + '_residue')
            except GateError:
                errors.append(kind)
        for tag in reversed(self.tags):
            try:
                if self.docker('image', 'ls', '-q', tag, guarded=False).strip():
                    self.docker('image', 'rm', tag, guarded=False)
            except GateError:
                errors.append('image_tag')
        try:
            shutil.rmtree(self.work)
            require(not self.work.exists(), 'private_directory_residue')
        except OSError:
            errors.append('private_directory')
        return errors


def defaults(life):
    text = life.command(['bash', '-c', 'set -a; source .env.example; env -0'], env=life.env)
    env = dict(item.split('=', 1) for item in text.split('\0') if '=' in item)
    # Do not retain user's shell environment from the shell defaults.
    env.update(life.env)
    for key in env:
        if key.endswith(('_PASSWORD', '_SECRET', '_KEY', '_PEPPER')):
            env[key] = secrets.token_hex(32)
    for key in ('ENCRYPTION_KEY', 'IAM_MFA_ENCRYPTION_KEY', 'OAUTH_USER_CODE_PEPPER'):
        env[key] = base64.b64encode(secrets.token_bytes(32)).decode()
    secret = secrets.token_hex(32)
    env.update(ENV='test', POSTGRES_DB='addp_online', POSTGRES_USER='trace_local', POSTGRES_PASSWORD=secrets.token_hex(24),
               POSTGRES_HOST='127.0.0.1', POSTGRES_PORT='5432', ADDP_EXECUTION_TRACE_LOCAL='1',
               ADDP_EXECUTION_TRACE_RUN_ID=life.uuid, COMPOSE_DISABLE_ENV_FILE='1',
               ADDP_PUBLIC_ORIGIN='http://nginx', ALLOWED_ORIGINS='http://nginx',
               ADDP_HOST_NODE_NAME='execution-trace-local', ADDP_RUNTIME_LOG_ROOT=str(life.work/'unused-logs'),
               ADDP_OBSERVABILITY_LOGS_ENABLED='true', ADDP_OBSERVABILITY_METRICS_ENABLED='false',
               LOKI_TEST_SECRET=secret, LOKI_READ_TOKEN=secret+'-read', LOKI_WRITE_TOKEN=secret+'-write',
               MINIO_ROOT_USER='test-admin', MINIO_ROOT_PASSWORD=secret, LOKI_RETENTION_HOURS='168',
               LOKI_URL='http://runtime-log-api:3100', TEMPO_S3_SECRET_KEY=secrets.token_hex(32),
               ADDP_TRACE_DEPLOYMENT_DIR=str(life.work/'deployment'), LOKI_TEST_SOURCE=str(life.work/'unused-logs'),
               ADDP_RUNTIME_LOG_OWNER='0:0', IMAGE_TAG=life.run_id, REGISTRY='localhost:5001',
               NO_PROXY='127.0.0.1,localhost', no_proxy='127.0.0.1,localhost')
    return env


def committed_inputs(life, commit):
    integration={'Makefile':b'test-monitor-traces-local:', 'scripts/ci/check-online-ci-registration.py':b'def validate_local_execution_trace_profile', 'system/backend/cmd/online-test-fixture/main.go':b'return prepareExecutionTraceLocalIdentities(db, cfg, *output)'}
    result={}
    for relative in OWN_INPUTS:
        expected=life.command(['git','show',commit+':'+relative]).encode()
        # Shared integration files can contain another session's uncommitted
        # hunks. The committed source is the only build input; the suite's own
        # new files and private owner paths must match it byte for byte.
        if relative in integration:
            require(integration[relative] in expected, 'committed_local_integration_missing')
        else:
            require((ROOT/relative).read_bytes()==expected, 'commit_all_local_inputs_before_acceptance')
        result[relative]=hashlib.sha256(expected).hexdigest()
    return result


def build(life):
    life.phase = 'source_snapshot'
    commit = life.command(['git', 'rev-parse', 'HEAD']).strip()
    life.input_hashes=committed_inputs(life,commit)
    life.command(['bash','-c','umask 022; exec "$@"','_', 'git', 'clone', '--local', '--no-hardlinks', '--single-branch', '--branch', 'main', str(ROOT), str(life.checkout)])
    require(life.command(['git', 'rev-parse', 'HEAD']).strip() == commit, 'snapshot_commit_changed')
    env = defaults(life)
    life.env = env
    # All builds use the product Make entry, with a private bounded BuildKit worker.
    life.phase = 'bounded_builder'
    config = life.work/'buildkit.toml'
    config.write_text('[registry."localhost:5001"]\n  http = true\n  insecure = true\n')
    life.docker('buildx', 'create', '--name', life.builder, '--driver', 'docker-container', '--driver-opt',
                'network=host,cpu-period=100000,cpu-quota=300000,memory=5g', '--buildkitd-config', str(config))
    life.builder_created = True
    life.docker('buildx', 'inspect', life.builder, '--bootstrap')
    ids = life.docker('ps', '-aq', '--filter', 'name=^buildx_buildkit_' + life.builder + '0$').split()
    require(len(ids) == 1, 'owned_builder_identity_missing')
    life.builder_ids = set(json.loads(life.docker('inspect', *ids))[0]['Id'] for _ in [0])
    # This immutable MinIO image is the standard source-built product, not a stand-in service.
    life.minio_image = life.docker('image', 'inspect', 'addp-minio:RELEASE.2025-10-15T17-29-55Z', '--format', '{{.Id}}').strip()
    life.sample()
    life.thread = threading.Thread(target=life.monitor, daemon=True)
    life.thread.start()
    docker_path = shutil.which('docker', path=env['PATH'])
    shim = life.work/'tools';shim.mkdir()
    wrapper = shim/'docker'
    wrapper.write_text('#!/usr/bin/env python3\nimport os,sys\na=sys.argv[1:]\n'
       'if a and a[0]=="build": a=["buildx","--builder",os.environ["TRACE_LOCAL_BUILDER"],"build","--load",*a[1:]]\n'
       'elif a and a[0]=="run": a=["run","--cpus","3","--memory","5g","--label",os.environ["TRACE_LOCAL_LABEL"],"--label","com.addp.execution-trace-local.role=oneshot",*a[1:]]\n'
       'if a and a[0]=="run":\n'
       ' a=[v+":ro" if v.endswith(":/go/pkg/mod") else v for v in a]\n'
       'os.execv(os.environ["TRACE_LOCAL_DOCKER"],[os.environ["TRACE_LOCAL_DOCKER"],*a])\n')
    wrapper.chmod(0o700)
    life.public_modcache=Path(life.command(['go','env','GOMODCACHE']).strip()).resolve()
    require(life.public_modcache.is_dir() and (life.public_modcache/'cache/download').is_dir(), 'public_readonly_go_cache_required')
    build_env={**env, 'PATH':str(shim)+os.pathsep+env['PATH'], 'TRACE_LOCAL_DOCKER':docker_path, 'TRACE_LOCAL_BUILDER':life.builder, 'TRACE_LOCAL_LABEL':LABEL+'='+life.run_id, 'ADDP_BUILD_GOMODCACHE':str(life.public_modcache)}
    life.phase='product_compile'
    life.docker('run', '--rm', '--name', life.run_id+'-compile', '--label', f'{LABEL}={life.run_id}', '--label', LABEL+'.role=oneshot', '--cpus', '3', '--memory', '5g',
                '--mount', f'type=bind,source={life.checkout},target=/workspace',
                '--mount', f'type=bind,source={life.public_modcache},target=/dependencies,readonly',
                '-e','ADDP_BUILD_GOMODCACHE=/dependencies','-w', '/workspace',
                '-e', 'GOMAXPROCS=3', '-e', 'GOFLAGS=-p=3', 'golang:1.24',
                'make', 'build', 'BUILD_ARGS=--arch arm64 --services '+','.join(BACKENDS+('runtime-log',)), timeout=2200)
    # Product binaries are complete; reclaim only this run's disposable Go
    # object cache before the next bounded build consumes the same disk budget.
    shutil.rmtree(life.checkout/'.cache/go-build',ignore_errors=False)
    life.phase='product_minio'
    minio_tag=life.run_id+'-minio:local'
    life.tags.append(minio_tag)
    life.command(['docker','build','-f','scripts/infra/Dockerfile.minio','-t',minio_tag,'scripts/infra'],env=build_env,timeout=1200)
    life.minio_image=life.docker('image','inspect',minio_tag,'--format','{{.Id}}').strip()
    life.docker('buildx','prune','--builder',life.builder,'--all','--force',timeout=60)
    life.phase='product_images'
    services=BACKENDS+FRONTENDS
    references=[f'localhost:5001/addp-{name}:{life.run_id}' for name in services]
    life.tags.extend(references)
    for service in services:
        life.phase='product_image_'+service.replace('-','_')
        life.command(['make','build-images','IMAGE_BUILD_ARGS=--verify --jobs 1 --services '+service+
                      ' --registry localhost:5001 --tag '+life.run_id],env=build_env,timeout=600)
        # Only this run's cache is pruned, keeping the approved disk stop line
        # meaningful without accumulating nine independent npm build trees.
        life.docker('buildx','prune','--builder',life.builder,'--all','--force',timeout=60)
    images={name:life.docker('image','inspect',tag,'--format','{{.Id}}').strip() for name,tag in zip(services,references)}
    life.builder_stopped=True
    life.docker('buildx','stop',life.builder)
    life.phase='browser_dependencies'
    version=json.loads((life.checkout/'console/frontend/package-lock.json').read_text())['packages']['node_modules/@playwright/test']['version']
    life.browser_image='mcr.microsoft.com/playwright:v'+version+'-noble'
    life.docker('run','--rm','--name',life.run_id+'-browser-prepare','--label',f'{LABEL}={life.run_id}',
                '--label',LABEL+'.role=oneshot','--cpus','1','--memory','2g',
                '--mount',f'type=bind,source={life.checkout},target=/workspace','-w','/workspace/console/frontend',
                life.browser_image,'npm','ci','--no-audit','--no-fund',timeout=300)
    # Builder's state remains under the same disk stop line until final cleanup.
    return commit,env,images


def start_deployment(compose):
    compose('up','-d','--wait','--no-build','postgres','business-postgres','redis','minio')
    for name in ('runtime-log-store-init','trace-store-init'):
        compose('run','--rm','--no-deps',name)
    for group in [('loki','runtime-log-api','tempo','trace-query','alloy'),('system-backend',),
                  ('gateway','security-backend','develop-backend','orchestrator-backend','monitor-backend'),FRONTENDS]:
        compose('up','-d','--wait','--no-build',*group)


def setup(life, env, images):
    life.phase='isolated_deployment'
    (life.work/'unused-logs').mkdir(mode=0o700)
    life.command([sys.executable,'scripts/test/trace-deployment-fixture.py',str(life.work)])
    env.update(ADDP_OBSERVABILITY_TRACES_ENABLED='true', MONITOR_TEMPO_URL='https://trace-query:8443',
               MONITOR_TRACE_TLS_DIR=str(life.work/'query-tls'),
               ADDP_TRACE_DEPLOYMENTS=json.dumps([{'module':m,'role':'ingress' if m=='gateway' else 'backend',
                  'endpoint':'https://alloy:4318/v1/traces','tls_dir':str(life.work/'source-tls')} for m in
                  ('system','gateway','security','develop','orchestrator','monitor')]))
    life.command([sys.executable,'scripts/prod/generate-trace-platform.py'],env=env)
    documents={}
    for filename,names in [('docker-compose.infra.yml',('postgres','redis')),('docker-compose.yml',BACKENDS+FRONTENDS),
                           ('scripts/test/docker-compose.monitor-traces-t2.yml',TRACE)]:
        doc=json.loads(life.command(['docker','compose','--env-file','/dev/null','-f',filename,'config','--format','json'],env=env))
        documents.update({name:doc['services'][name] for name in names})
    # The same product PostgreSQL image owns the synthetic read-only query source.
    documents['business-postgres']=copy.deepcopy(documents['postgres'])
    documents['business-postgres']['environment'].update(POSTGRES_DB='trace_fixture')
    documents['business-postgres']['volumes'][0]['source']='business-postgres-data'
    documents['postgres']['command']+=['-c','cluster_name=addp-execution-trace-local-'+life.uuid]
    overlay=json.loads((life.work/'deployment/platform-traces.json').read_text())
    for name,row in overlay['services'].items():
        documents[name]['environment'].update(row['environment'])
        documents[name]['volumes']+=row['volumes']
    # Shared runtime source is a fresh named volume, consumed by receiver and Alloy.
    for name in TRACE:
        documents[name]['volumes']=[m for m in documents[name].get('volumes',[]) if not m['target'].startswith('/capacity')]
    documents['alloy']['volumes']=[m for m in documents['alloy']['volumes'] if m['target']!='/var/log/addp']+[
        {'type':'volume','source':'runtime-logs','target':'/var/log/addp','read_only':True}]
    for name in ('minio','runtime-log-store-init','trace-store-init'):
        images[name]=life.minio_image
    config=isolate(documents,life.run_id,images)
    # System log reads use the standard product runtime source path, not host logs.
    config['services']['system-backend']['environment'].update(LOKI_URL='http://runtime-log-api:3100',LOKI_READ_TOKEN=env['LOKI_READ_TOKEN'])
    path=life.private_json('compose.json',config)
    life.compose=lambda *args:life.command(['docker','compose','--env-file','/dev/null','-f',str(path),*args],env=env,timeout=300)
    start_deployment(life.compose)
    rows=json.loads(life.docker('inspect',*[life.run_id+'-'+name for name in BACKENDS]))
    require(all(row['Image']==images[name] for row,name in zip(rows,BACKENDS)), 'module_image_identity_changed')
    published={}
    for name,port in [('system-backend',8180),('gateway',8000),('nginx',80)]:
        binding=json.loads(life.docker('inspect',life.run_id+'-'+name))[0]['NetworkSettings']['Ports'][str(port)+'/tcp']
        require(len(binding)==1 and binding[0]['HostIp']=='127.0.0.1','loopback_publication_required')
        published[name]='http://127.0.0.1:'+binding[0]['HostPort']
    env.update(SYSTEM_URL=published['system-backend'],GATEWAY_URL=published['gateway'],CONSOLE_URL='http://nginx')
    probe='\n'.join('curl -fsS http://'+name+':'+str(port)+'/health/live; echo' for name,port in
                       [('system-backend',8180),('gateway',8000),('security-backend',8194),
                        ('develop-backend',8185),('orchestrator-backend',8084),('monitor-backend',8100)])
    output=life.docker('run','--rm','--name',life.run_id+'-build-probe','--label',f'{LABEL}={life.run_id}',
                      '--label',LABEL+'.role=oneshot','--cpus','.05','--memory','64m',
                      '--network',life.run_id+'_data','--entrypoint','/bin/sh',life.minio_image,'-ec',probe)
    facts=[json.loads(line) for line in output.splitlines() if line.strip()]
    require(len(facts)==len(BACKENDS) and all(row.get('git_commit')==life.source_commit and row.get('status')=='live' for row in facts),
            'module_live_build_commit_mismatch')
    life.module_commits={row['module']:row['git_commit'] for row in facts}
    return config


def identity(life, env):
    life.phase='formal_identity_preparation'
    # Helper has this run's PG network namespace; localhost never means shared PG.
    identity_env=life.work/'identity.env'
    helper_env={k:v for k,v in env.items() if k not in ('PATH','HOME','TMPDIR','DOCKER_HOST','DOCKER_CONTEXT')}
    helper_env.update(GOWORK='off',GOMAXPROCS='1',GOFLAGS='-p=1',POSTGRES_HOST='127.0.0.1',POSTGRES_PORT='5432',
                      GOCACHE='/private/helper-go-cache',GOPATH='/private/helper-go-path',GOMODCACHE='/dependencies')
    private=life.work/'helper.env';private.write_text('\n'.join(k+'='+v for k,v in helper_env.items())+'\n');private.chmod(0o600)
    life.docker('run','--rm','--name',life.run_id+'-identity','--label',f'{LABEL}={life.run_id}',
        '--label',LABEL+'.role=oneshot','--user',str(os.getuid())+':'+str(os.getgid()),'--cpus','.5','--memory','1g','--network','container:'+life.run_id+'-postgres',
        '--env-file',str(private),'--mount',f'type=bind,source={life.checkout},target=/workspace',
        '--mount',f'type=bind,source={life.public_modcache},target=/dependencies,readonly',
        '--mount',f'type=bind,source={life.work},target=/private','-w','/workspace/system/backend','golang:1.24',
        'go','run','./cmd/online-test-fixture','--suite','execution-trace-local','--output','/private/identity.env',timeout=300)
    # The helper emits shell-quoted values; source only that private owned file.
    text=life.command(['bash','-c','set -a; source "$1"; env -0','_',str(identity_env)],env=life.env)
    env.update({k:v for item in text.split('\0') if '=' in item for k,v in [item.split('=',1)] if k.startswith('ADDP_EXECUTION_TRACE_')})


def main():
    require(len(sys.argv)==1,'local_entry_has_no_capacity_or_hosted_options')
    remaining=int(os.environ.get('MONITOR_TRACE_LOCAL_REMAINING_SECONDS',str(MAX_SECONDS)))
    require(0<remaining<=MAX_SECONDS,'remaining_budget_cannot_exceed_sixty_minutes')
    life=Lifecycle()
    life.started-=MAX_SECONDS-remaining
    report={'schema':'addp.execution-trace-local/v1','run_id':life.run_id,'result':'failed',
                            'formal_runner_t4':False,'hosted':False,'capacity':False}
    destination=os.environ.get('MONITOR_TRACE_LOCAL_REPORT')
    target=Path(tempfile.gettempdir())/(life.run_id+'-report.json')
    try:
      if destination:
        candidate=Path(destination)
        require(candidate.is_absolute() and not candidate.is_symlink(), 'external_report_required')
        candidate=candidate.resolve()
        require(not candidate.is_relative_to(ROOT) and not candidate.is_relative_to(life.work) and not candidate.exists(), 'external_report_required')
        target=candidate
    except Exception:
        shutil.rmtree(life.work)
        raise
    try:
        endpoint=effective_docker_endpoint(os.environ,lambda name: json.loads(life.docker('context','inspect',*([name] if name else [])))[0]['Endpoints']['docker']['Host'])
        private_docker_client(life,endpoint)
        validate_profile(os.environ,platform.machine(),life.docker('info','--format','{{.Architecture}}').strip())
        commit,env,images=build(life)
        life.source_commit=commit
        report.update(source_commit=commit,images=images,input_sha256=life.input_hashes)
        config=setup(life,env,images)
        report['module_commits']=life.module_commits
        identity(life,env)
        business=module('monitor-traces-local-online',life.checkout)
        browser=business.run(life,env,report)
        life.phase='real_console_browser'
        browser_path=life.private_json('browser.json',browser)
        secret_env=life.work/'browser.env'
        selected={k:v for k,v in env.items() if k.startswith('ADDP_EXECUTION_TRACE_ADMIN_')}
        selected.update(ADDP_EXECUTION_TRACE_LOCAL='1',CONSOLE_URL='http://nginx',
                        ADDP_EXECUTION_TRACE_BROWSER_CONFIG_FILE='/private/browser.json',
                        ADDP_EXECUTION_TRACE_BROWSER_REPORT_FILE='/private/browser-report.json',
                        ADDP_ONLINE_ARTIFACT_DIR='/tmp/trace-browser')
        secret_env.write_text('\n'.join(k+'='+v for k,v in selected.items())+'\n');secret_env.chmod(0o600)
        version=json.loads((life.checkout/'console/frontend/package-lock.json').read_text())['packages']['node_modules/@playwright/test']['version']
        browser_image='mcr.microsoft.com/playwright:v'+version+'-noble'
        life.docker('run','--rm','--name',life.run_id+'-browser','--label',f'{LABEL}={life.run_id}',
            '--label',LABEL+'.role=oneshot','--cpus','.5','--memory','1g','--network',life.run_id+'_data','--env-file',str(secret_env),
            '--mount',f'type=bind,source={life.checkout},target=/workspace',
            '--mount',f'type=bind,source={life.work},target=/private','-w','/workspace/console/frontend',browser_image,
            'node','node_modules/@playwright/test/cli.js','test','--config=playwright.online.config.js','execution-trace-local.spec.js','--reporter=line',timeout=300)
        report['browser']=json.loads((life.work/'browser-report.json').read_text())
        require(report['browser'].get('result')=='passed','browser_report_not_passed')
        business.finish(life,env,report)
        life.sample()
        require(life.failure is None,life.failure or 'resource_guard_failed')
        report['result']='passed'
    except BaseException as error:
        report.update(stage=life.phase,error_code=str(error) if isinstance(error,GateError) and re.fullmatch('[a-z0-9_]+',str(error)) else type(error).__name__)
        if hasattr(life,'last_failure'):report['command_failure']=life.last_failure
    finally:
        report['resource_samples']=life.samples
        errors=life.cleanup()
        report['cleanup']={'zero_owned_resources':not errors,'failures':errors}
        if errors:report['result']='failed'
        target.parent.mkdir(parents=True,exist_ok=True)
        with open(target,'x',opener=lambda path,flags:os.open(path,flags,0o600)) as f:json.dump(report,f,indent=2)
    print('Local execution Trace: '+report['result']+'; report='+str(target),flush=True)
    return 0 if report['result']=='passed' else 1


def interrupted(signum, frame):
    raise GateError('local_acceptance_interrupted')


if __name__=='__main__':
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    raise SystemExit(main())
