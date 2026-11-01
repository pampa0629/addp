#!/usr/bin/env python3
"""Deployment-owned Redis account persistence and optional mTLS source lifecycle."""
import argparse
import base64
from contextlib import contextmanager
import hashlib
import importlib.util
import io
import ipaddress
import json
import os
from pathlib import Path
import re
import secrets
import subprocess
import sys
import tarfile
import tempfile

ROOT = Path(__file__).resolve().parents[2]


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


private = load('redis_private_state', ROOT/'scripts/utils/private-state.py')
deployment = load('redis_deployment_transport', ROOT/'scripts/utils/deployment-docker.py')
inventory = deployment.inventory
Error = deployment.DeploymentError
OWNER, SCOPE = 'io.addp.redis-metrics.owner', 'io.addp.redis-metrics.scope'
RECORD = '/data/.addp-redis-observation/account.record'


def unique(pairs):
    value = {}
    for key, item in pairs:
        if key in value:
            raise Error('Duplicate private lifecycle field')
        value[key] = item
    return value


def decode(value):
    return json.loads(value, object_pairs_hook=unique)


def write(path, value):
    previous = private.read(path, 0o600)[0] if os.path.lexists(path) else None
    private.atomic(path, value, 0o600, previous=previous)


def state_directory(env, scope):
    value = env.get('ADDP_REDIS_METRICS_STATE_DIR', '')
    parent = Path(value) if value else Path.home().resolve()/'.local/state/addp'/hashlib.sha256(str(ROOT).encode()).hexdigest()[:16]/'redis-metrics'
    if not parent.is_absolute() or '..' in parent.parts or parent.resolve().is_relative_to(ROOT):
        raise Error('Redis metrics state must be outside the workspace')
    return parent/scope


def transport(env):
    match = re.fullmatch(r'(\[[0-9a-f:]+\]|[0-9.]+):([1-9][0-9]{0,4})', env.get('ADDP_REDIS_METRICS_LISTEN', ''))
    if not match:
        raise Error('Redis source requires an explicit private IP and port')
    ip, port = ipaddress.ip_address(match[1].strip('[]')), int(match[2])
    if port > 65535 or ip.is_unspecified or ip.is_multicast or ip.is_link_local or (ip.version == 6 and ip.ipv4_mapped):
        raise Error('Redis source publication rejected')
    networks = tuple(ipaddress.ip_network(n) for n in ('127.0.0.0/8','::1/128','10.0.0.0/8','172.16.0.0/12','192.168.0.0/16','fc00::/7'))
    if not any(ip.version == n.version and ip in n for n in networks):
        raise Error('Redis source publication rejected')
    if env['ADDP_REDIS_METRICS_LISTEN'] != (f'[{ip}]:{port}' if ip.version == 6 else f'{ip}:{port}'):
        raise Error('Redis source publication is not canonical')
    directory = Path(env.get('ADDP_REDIS_METRICS_TLS_DIR', ''))
    if not directory.is_absolute() or directory.resolve().is_relative_to(ROOT) or directory.is_symlink():
        raise Error('Redis source TLS requires an external directory')
    material = {}
    for name in ('ca.crt', 'server.crt', 'server.key'):
        path = directory/name
        if path.is_symlink() or not path.is_file() or path.stat().st_size > 65536 or path.stat().st_nlink != 1:
            raise Error('Redis source TLS material rejected')
        material[name] = path.read_bytes()
        if not material[name]:
            raise Error('Redis source TLS material rejected')
    if (directory/'server.key').stat().st_uid != os.getuid() or (directory/'server.key').stat().st_mode & 0o077:
        raise Error('Redis source private key ownership rejected')
    try:
        subprocess.run(['openssl', 'verify', '-CAfile', str(directory/'ca.crt'), '-purpose', 'sslserver', '-verify_ip', str(ip), str(directory/'server.crt')], check=True, capture_output=True, timeout=5)
        public = subprocess.run(['openssl', 'x509', '-in', str(directory/'server.crt'), '-pubkey', '-noout'], check=True, capture_output=True, timeout=5).stdout
        key = subprocess.run(['openssl', 'pkey', '-in', str(directory/'server.key'), '-passin', 'pass:', '-pubout'], check=True, capture_output=True, timeout=5).stdout
        if public != key:
            raise Error('Redis source certificate key mismatch')
    except (OSError, subprocess.SubprocessError):
        raise Error('Redis source TLS verification failed') from None
    return str(ip), port, material


class Database:
    def __init__(self, docker, scope, project, working, password, *, service='redis'):
        self.docker, self.scope = docker, scope
        self.admin = 'default' if scope == 'infra' else 'addp_business_admin'
        if not password or len(password) > 512 or any(ord(c) < 32 or ord(c) > 126 for c in password):
            raise Error('Redis deployment credential rejected')
        self.password = password
        ids = docker.run(['ps', '-aq', '--no-trunc', '--filter', 'label=com.docker.compose.project='+project, '--filter', 'label=com.docker.compose.service='+service]).split()
        if len(ids) != 1 or not inventory.identifier(ids[0]):
            raise Error('Redis deployment is not uniquely available')
        self.identity = ids[0]
        self.image = docker.run(['inspect','--format','{{.Image}}',self.identity]).strip()
        if not re.fullmatch(r'sha256:[0-9a-f]{64}',self.image):
            raise Error('Redis deployment image identity rejected')
        self.facts = docker.fact(self.identity)
        labels = self.facts['labels']
        if (not self.facts['running'] or labels.get('com.docker.compose.project') != project or labels.get('com.docker.compose.service') != service
                or labels.get('com.docker.compose.project.working_dir') != str(Path(working).resolve())
                or labels.get('com.docker.compose.oneoff', '').lower() != 'false' or len(self.facts['networks']) != 1):
            raise Error('Redis deployment ownership rejected')
        self.network, attachment = next(iter(self.facts['networks'].items()))
        network = decode(docker.run(['network', 'inspect', attachment['NetworkID']]))[0]
        if network.get('Driver') != 'bridge' or network.get('Name') != self.network or (network.get('Labels') or {}).get('com.docker.compose.project') != project:
            raise Error('Redis network ownership rejected')
        address = ipaddress.ip_address(attachment['IPAddress'] or attachment['GlobalIPv6Address'])
        if not address.is_private or address.is_unspecified or address.is_link_local or address.is_multicast:
            raise Error('Redis deployment address rejected')
        self.address = f'[{address}]:6379' if address.version == 6 else f'{address}:6379'
        mounts = [m for m in self.facts['mounts'] if m.get('Destination') == '/data']
        if len(mounts) != 1 or mounts[0].get('Type') != 'volume' or mounts[0].get('RW') is not True:
            raise Error('Redis persistence requires an owned named volume')
        self.volume = mounts[0]['Name']
        volume = decode(docker.run(['volume', 'inspect', self.volume]))[0]
        if (volume.get('Driver') != 'local' or (volume.get('Labels') or {}).get('com.docker.compose.project') != project):
            raise Error('Redis volume ownership rejected')
        if self.cli(['CONFIG', 'GET', 'aclfile']) != {'aclfile': '/run/addp-redis/users.acl'}:
            raise Error('Redis ACL bootstrap requires the standard deployment entry')

    def cli(self, args, last=None):
        # Password and optional hashed ACL argument travel only through stdin.
        script = 'set -eu; IFS= read -r REDISCLI_AUTH; export REDISCLI_AUTH; exec redis-cli -3 -e --json --user "$@"'
        tail = [self.admin] + (['-x'] if last is not None else []) + args
        value = self.docker.run(['exec', '-i', self.identity, 'sh', '-c', script, 'redis-observation', *tail],
                                input=self.password+'\n'+(last or ''), timeout=8)
        return decode(value)

    def account(self):
        value = self.cli(['ACL', 'GETUSER', 'addp_'+self.scope+'_observer'])
        if value is None:
            return None
        if not isinstance(value, dict):
            raise Error('Redis account observation rejected')
        return value

    def record(self):
        return self.docker.run(['exec', '--user', 'redis', self.identity, 'sh', '-c', '''set -eu
d=/data/.addp-redis-observation
test ! -L "$d" && test -d "$d" && test "$(stat -c %u "$d")" = "$(id -u)" && test "$(stat -c %a "$d")" = 700
p=/data/.addp-redis-observation/account.record
test ! -L "$p"
if test -e "$p"; then
 test -f "$p" && test "$(stat -c %u "$p")" = "$(id -u)" && test "$(stat -c %a "$p")" = 600 && test "$(stat -c %h "$p")" = 1
 head -c 1025 "$p"
fi'''])

    def replace_record(self, expected, value):
        if len(value.encode()) > 1024:
            raise Error('Redis account record exceeds budget')
        digest = hashlib.sha256(expected.encode()).hexdigest() if expected else 'missing'
        self.docker.run(['exec', '-i', '--user', 'redis', self.identity, 'sh', '-c', '''set -eu
umask 077
d=/data/.addp-redis-observation
test ! -L "$d" && test -d "$d" && test "$(stat -c %u "$d")" = "$(id -u)" && test "$(stat -c %a "$d")" = 700
p=/data/.addp-redis-observation/account.record
check() { test ! -L "$p"; if test "$1" = missing; then test ! -e "$p"; else test -f "$p" && test "$(sha256sum "$p" | cut -d ' ' -f 1)" = "$1"; fi; }
check "$1"
t=$(mktemp /data/.addp-redis-observation/.pending.XXXXXX)
trap 'rm -f "$t"' EXIT
cat > "$t"; chmod 600 "$t"
check "$1"; mv "$t" "$p"
trap - EXIT''', 'redis-observation', digest], input=value)
        if self.record() != value:
            raise Error('Redis account persistence readback failed')

    @contextmanager
    def locked(self):
        token = secrets.token_hex(16)
        self.docker.run(['exec', '--user', 'redis', self.identity, 'sh', '-c', 'set -eu; umask 077; d=/data/.addp-redis-observation; test ! -L "$d" && test -d "$d" && test "$(stat -c %u "$d")" = "$(id -u)" && test "$(stat -c %a "$d")" = 700; mkdir /data/.addp-redis-observation/control.lock; printf %s "$1" > /data/.addp-redis-observation/control.lock/owner', 'redis-observation', token])
        try:
            yield
        finally:
            self.docker.run(['exec', '--user', 'redis', self.identity, 'sh', '-c', 'set -eu; test "$(cat /data/.addp-redis-observation/control.lock/owner)" = "$1"; rm /data/.addp-redis-observation/control.lock/owner; rmdir /data/.addp-redis-observation/control.lock', 'redis-observation', token])

    def unchanged(self):
        if self.docker.fact(self.identity) != self.facts:
            raise Error('Redis deployment changed during lifecycle')


def matches(account, password_hash, enabled):
    if not isinstance(account, dict):
        return False
    flags = set(account.get('flags', []))
    return (flags in ({'on'}, {'on', 'sanitize-payload'}) if enabled else flags in ({'off'}, {'off', 'sanitize-payload'})) and \
        account.get('passwords') == ([password_hash] if enabled else []) and \
        set(account.get('commands', '').split()) == ({'-@all', '+hello', '+ping', '+info'} if enabled else {'-@all'}) and \
        account.get('keys') in ('', []) and account.get('channels') in ('', []) and account.get('selectors', []) == []


class Lifecycle:
    def __init__(self, docker, scope, state, database, *, project=None):
        self.docker, self.scope, self.state, self.database = docker, scope, Path(state), database
        self.project = project or inventory.redis_metrics_project(scope)
        self.config_volume = self.project+'-private'

    def stop_sources(self):
        identities = self.docker.run(['ps', '-aq', '--filter', 'label=com.docker.compose.project='+self.project]).split()
        for identity in identities:
            labels = self.docker.fact(identity)['labels']
            if labels.get(OWNER) != str(ROOT) or labels.get(SCOPE) != self.scope or labels.get('com.docker.compose.service') not in ('redis-metrics', 'redis-metrics-init'):
                raise Error('Refusing foreign Redis source containers')
        for identity in identities:
            self.docker.run(['rm', '-f', identity], timeout=20)
        volumes = self.docker.run(['volume', 'ls', '-q', '--filter', 'name=^'+self.config_volume+'$']).split()
        for volume in volumes:
            fact = decode(self.docker.run(['volume', 'inspect', volume]))[0]
            if volume != self.config_volume or (fact.get('Labels') or {}).get(OWNER) != str(ROOT) or (fact.get('Labels') or {}).get(SCOPE) != self.scope:
                raise Error('Refusing foreign Redis source volume')
        for volume in volumes:
            self.docker.run(['volume', 'rm', volume])

    def image(self):
        files = ['common/go.mod', 'common/go.sum', 'monitor/backend/go.mod', 'monitor/backend/go.sum', 'scripts/infra/Dockerfile.redis-metrics']
        for folder in ('monitor/backend/internal/redissource', 'monitor/backend/cmd/redis-metrics-source'):
            files.extend(str(p.relative_to(ROOT)) for p in sorted((ROOT/folder).glob('*.go')) if not p.name.endswith('_test.go'))
        digest = hashlib.sha256()
        with tempfile.TemporaryDirectory(prefix='redis-build-', dir=self.state) as temporary:
            context = Path(temporary)
            for name in files:
                path = ROOT/name
                if path.is_symlink() or not path.is_file():
                    raise Error('Redis source build input rejected')
                data = path.read_bytes(); digest.update(name.encode()+b'\0'+data)
                target = context/name; target.parent.mkdir(parents=True, exist_ok=True); target.write_bytes(data)
            tag = 'addp-redis-metrics:'+digest.hexdigest()
            self.docker.run(['build', '--label', OWNER+'='+str(ROOT), '-t', tag, '-f', str(context/'scripts/infra/Dockerfile.redis-metrics'), str(context)], timeout=240)
        fact = decode(self.docker.run(['image', 'inspect', tag]))[0]
        if not re.fullmatch(r'sha256:[0-9a-f]{64}', fact['Id']) or (fact.get('Config', {}).get('Labels') or {}).get(OWNER) != str(ROOT):
            raise Error('Redis source build identity rejected')
        return fact['Id']

    def plan(self, image, state):
        args = ['run', '--rm', '--pull=never', '-i', '--network', 'none', '--read-only', '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges:true', '--memory', '64m', '--cpus', '.25', '--pids-limit', '32',
                '--label', 'com.docker.compose.project='+self.project, '--label', 'com.docker.compose.service=redis-metrics-init', '--label', OWNER+'='+str(ROOT), '--label', SCOPE+'='+self.scope,
                image, 'account-plan']
        value = decode(self.docker.run(args, input=json.dumps({k:state[k] for k in ('scope', 'password', 'owner')})))
        if set(value) != {'enabled', 'disabled', 'enabled_record', 'disabled_record', 'password_hash'}:
            raise Error('Redis account plan rejected')
        return value

    def load_state(self, database, create):
        expected = {'schema':'addp.redis-metrics/v1', 'workspace':str(ROOT), 'scope':self.scope,
            'source_key':{'docker_engine_id':self.docker.info['id'], 'project':database.facts['labels']['com.docker.compose.project'], 'service':database.facts['labels']['com.docker.compose.service']}, 'volume':database.volume}
        path = self.state/'owner.json'
        if os.path.lexists(path):
            state = decode(private.read(path, 0o600)[0])
            if (set(state) != set(expected)|{'owner', 'password', 'record', 'pending_record','image'} or any(state[k] != value for k,value in expected.items())
                    or not all(isinstance(state[k], str) and re.fullmatch('[0-9a-f]{64}', state[k]) for k in ('owner','password'))
                    or not all(isinstance(state[k],str) for k in ('record','pending_record'))
                    or not isinstance(state['image'],str) or state['image'] and not re.fullmatch(r'sha256:[0-9a-f]{64}',state['image'])):
                raise Error('Redis source private state missing or drifted')
            return state
        if not create:
            return None
        if database.account() is not None or database.record():
            raise Error('Refusing to adopt an existing Redis observer')
        state = dict(expected, owner=secrets.token_hex(32), password=secrets.token_hex(32), record='', pending_record='', image='')
        write(path, json.dumps(state, sort_keys=True))
        return state

    def transition(self, database, state, plan, enabled):
        current = database.record()
        records = {state['record'], state['pending_record']}
        if not isinstance(current,str) or current not in records or any(v and v not in (plan['enabled_record'],plan['disabled_record']) for v in records):
            raise Error('Redis persisted account ownership changed')
        account = database.account()
        if not (matches(account, plan['password_hash'], True) or matches(account, plan['password_hash'], False) or account is None and current==''):
            raise Error('Redis observer permissions drifted; no account adopted')
        key = 'enabled' if enabled else 'disabled'
        record = plan[key+'_record']
        database.unchanged()
        state['pending_record'] = record
        write(self.state/'owner.json', json.dumps(state, sort_keys=True))
        args = list(plan[key])
        hashed = next((value for value in args if value.startswith('#')), None)
        if hashed: args.remove(hashed)
        if database.cli(args, last=hashed) != 'OK':
            raise Error('Redis observer mutation failed')
        if not matches(database.account(), plan['password_hash'], enabled):
            raise Error('Redis observer verification failed')
        database.replace_record(current, record)
        database.unchanged()
        state['record'], state['pending_record'] = record, ''
        write(self.state/'owner.json', json.dumps(state, sort_keys=True))

    def start_source(self, database, image, state, ip, port, material):
        files = dict(material, **{'config.json':json.dumps({'address':database.address, 'scope':self.scope, 'password':state['password']}).encode()})
        archive = io.BytesIO()
        with tarfile.open(fileobj=archive, mode='w') as tar:
            for name, value in files.items():
                item = tarfile.TarInfo(name); item.size=len(value); item.mode=0o400; item.uid=item.gid=65532
                tar.addfile(item, io.BytesIO(value))
        self.docker.run(['volume','create','--label',OWNER+'='+str(ROOT),'--label',SCOPE+'='+self.scope,self.config_volume])
        volume=decode(self.docker.run(['volume','inspect',self.config_volume]))[0]
        if volume.get('Name')!=self.config_volume or volume.get('Driver')!='local' or (volume.get('Labels') or {}).get(OWNER)!=str(ROOT) or (volume.get('Labels') or {}).get(SCOPE)!=self.scope:
            raise Error('Redis source private volume ownership changed')
        self.docker.run(['run','--rm','--pull=never','-i','--network','none','--read-only','--cap-drop','ALL','--cap-add','CHOWN','--security-opt','no-new-privileges:true','--memory','64m','--cpus','.25','--pids-limit','32',
            '--label','com.docker.compose.project='+self.project,'--label','com.docker.compose.service=redis-metrics-init','--label',OWNER+'='+str(ROOT),'--label',SCOPE+'='+self.scope,
            '--mount','type=volume,src='+self.config_volume+',dst=/private',database.image,'sh','-c','set -eu; base64 -d | tar -xo -C /private; chmod 700 /private; chown -R 65532:65532 /private'],
            input=base64.b64encode(archive.getvalue()).decode())
        labels = {OWNER:str(ROOT), SCOPE:self.scope, 'io.addp.redis-metrics.database-id':database.identity,
                  'io.addp.redis-metrics.database-started-at':database.facts['started_at']}
        document = {'services':{'redis-metrics':{'image':image,'user':'65532:65532','read_only':True,'cap_drop':['ALL'],
            'security_opt':['no-new-privileges:true'],'cpus':.25,'mem_limit':'128m','pids_limit':32,'restart':'no',
            'labels':labels,'networks':['database'],'ports':[{'host_ip':ip,'published':str(port),'target':9443}],
            'volumes':[{'type':'volume','source':'private','target':'/source','read_only':True,'volume':{'nocopy':True}}]}},
            'networks':{'database':{'external':True,'name':database.network}},'volumes':{'private':{'external':True,'name':self.config_volume}}}
        write(self.state/'compose.json',json.dumps(document,sort_keys=True))
        database.unchanged()
        self.docker.run(['compose','--env-file','/dev/null','-p',self.project,'-f',str(self.state/'compose.json'),'up','-d','--no-deps','redis-metrics'],timeout=60)
        database.unchanged()
        ids=self.docker.run(['ps','-aq','--filter','label=com.docker.compose.project='+self.project,'--filter','label=com.docker.compose.service=redis-metrics']).split()
        if len(ids)!=1 or not self.docker.fact(ids[0])['running']:
            raise Error('Redis source did not stay running')

    def _run(self, action, env):
        with private.locked(self.state):
            self.stop_sources()
            if action=='down' or env.get('ADDP_REDIS_METRICS_ENABLED','false')=='false':
                if not os.path.lexists(self.state/'owner.json'): return
                database=self.database()
                with database.locked():
                    state=self.load_state(database,False)
                    if not state['image']:
                        if database.account() is not None or database.record(): raise Error('Incomplete Redis account installation')
                        return
                    plan=self.plan(state['image'],state)
                    self.transition(database,state,plan,False)
                return
            if env.get('ADDP_REDIS_METRICS_ENABLED')!='true':
                raise Error('ADDP_REDIS_METRICS_ENABLED must be true or false')
            ip,port,material=transport(env)
            database=self.database()
            with database.locked():
                state=self.load_state(database,True); image=self.image()
                state['image']=image; write(self.state/'owner.json',json.dumps(state,sort_keys=True))
                plan=self.plan(image,state)
                # Reconcile both runtime and persisted records to the disabled
                # baseline before making a new source eligible to start.
                self.transition(database,state,plan,False)
                self.transition(database,state,plan,True)
                self.start_source(database,image,state,ip,port,material)

    def run(self, action, env):
        try:
            return self._run(action,env)
        except Exception as error:
            if action=='up':
                try: self._run('down',env)
                except Exception: error.redis_cleanup_failed=True
            raise


def run(action, scope, env):
    state=state_directory(env,scope)
    # Off/uninstalled is independent from Docker and Redis readiness.
    if env.get('ADDP_REDIS_METRICS_ENABLED','false')=='false' and not state.exists():
        print('Redis metrics '+scope+': not installed; disabled')
        return
    docker=deployment.Docker(env)
    project,working=inventory.SCOPES[scope]
    password=env.get('REDIS_PASSWORD','') if scope=='infra' else env.get('BUSINESS_REDIS_ADMIN_PASSWORD','')
    manager=Lifecycle(docker,scope,state,lambda:Database(docker,scope,project,working,password))
    manager.run(action,env)
    print('Redis metrics '+scope+(': disabled; owned source removed and account cleared' if action=='down' or env.get('ADDP_REDIS_METRICS_ENABLED','false')=='false' else ': source deployed; successful scraping requires Monitor verification'))


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action',choices=['up','down']); parser.add_argument('--scope',choices=['infra','business'],required=True)
    args=parser.parse_args()
    try: run(args.action,args.scope,os.environ)
    except (ValueError,OSError,KeyError,TypeError) as error:
        detail=' [cleanup=failed]' if getattr(error,'redis_cleanup_failed',False) else ''
        reason=str(error) if isinstance(error,(Error,private.PrivateStateError)) else 'deployment ownership, private state or source configuration rejected'
        print('Redis metrics unavailable'+detail+': '+reason+'; core services remain independent',file=sys.stderr)
        return 1
    return 0


if __name__=='__main__':
    sys.exit(main())
