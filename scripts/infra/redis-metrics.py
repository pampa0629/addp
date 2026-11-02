#!/usr/bin/env python3
"""Deployment-owned Redis account persistence and optional mTLS source lifecycle."""
import argparse
from contextlib import contextmanager
import hashlib
import importlib.util
import ipaddress
import json
import os
from pathlib import Path
import re
import secrets
import sys

ROOT = Path(__file__).resolve().parents[2]


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


private = load('redis_private_state', ROOT/'scripts/utils/private-state.py')
deployment = load('redis_deployment_transport', ROOT/'scripts/utils/deployment-docker.py')
sources = load('redis_source_deployment', ROOT/'scripts/utils/metrics-source.py')
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
    return sources.transport(env, ROOT, 'ADDP_REDIS_METRICS', Error)

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

    def container_source(self):
        return sources.Source(self.docker,self.scope,self.state,ROOT,'redis',self.project,Error)

    def stop_sources(self):
        self.container_source().stop()

    def image(self):
        files = ['common/go.mod', 'common/go.sum', 'monitor/backend/go.mod', 'monitor/backend/go.sum', 'scripts/infra/Dockerfile.redis-metrics']
        for folder in ('monitor/backend/internal/redissource', 'monitor/backend/internal/sourceserver', 'monitor/backend/cmd/redis-metrics-source'):
            files.extend(str(p.relative_to(ROOT)) for p in sorted((ROOT/folder).glob('*.go')) if not p.name.endswith('_test.go'))
        return self.container_source().build(files)

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
        self.container_source().start(database,image,{'address':database.address,'scope':self.scope,'password':state['password']},ip,port,material,database.unchanged)

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
