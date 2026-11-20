#!/usr/bin/env python3
"""Install/recover fixed deployment component metrics sources; manage only observation sources and their dedicated accounts.

Usage: cdc-metrics.py up|down|recover --scope infra --kind redpanda|kafka-connect|meilisearch
The public lifecycle entry is metrics-sources.py.
"""
import argparse
import hashlib
import importlib.util
import ipaddress
import json
import os
from pathlib import Path
import re
import sys
import uuid

ROOT = Path(__file__).resolve().parents[2]

def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    value = importlib.util.module_from_spec(spec); spec.loader.exec_module(value)
    return value

deployment = load('cdc_deployment', ROOT/'scripts/utils/deployment-docker.py')
sources = load('cdc_source', ROOT/'scripts/utils/metrics-source.py')
private = sources.private
Error = deployment.DeploymentError
NETWORK = 'addp-network'
PREFIXES = {'redpanda':'ADDP_REDPANDA_METRICS', 'kafka-connect':'ADDP_KAFKA_CONNECT_METRICS',
            'meilisearch':'ADDP_MEILISEARCH_METRICS'}


PRODUCTS = sources.group.PRODUCTS
PREFIXES.update({kind: 'ADDP_'+kind.upper()+'_METRICS' for kind in PRODUCTS})


def source_inputs(kind=None):
    if kind in PRODUCTS:
        return ['common/go.mod', 'common/go.sum', 'monitor/backend/go.mod', 'monitor/backend/go.sum',
                'scripts/infra/Dockerfile.deployment-metrics', 'monitor/backend/cmd/deployment-metrics-source/main.go',
                *(str(path.relative_to(ROOT)) for path in sorted((ROOT/'monitor/backend/internal/sourceserver').glob('*.go'))
                  if not path.name.endswith('_test.go'))]
    if kind == 'meilisearch':
        return ['common/go.mod', 'common/go.sum', 'monitor/backend/go.mod', 'monitor/backend/go.sum',
                'scripts/infra/Dockerfile.meilisearch-metrics', 'monitor/backend/cmd/meilisearch-metrics-source/main.go',
                *(str(p.relative_to(ROOT)) for p in sorted((ROOT/'monitor/backend/internal/sourceserver').glob('*.go'))
                  if not p.name.endswith('_test.go'))]
    files = ['common/go.mod', 'common/go.sum', 'monitor/backend/go.mod', 'monitor/backend/go.sum', 'scripts/infra/Dockerfile.cdc-metrics']
    for folder in ('monitor/backend/internal/cdcsource', 'monitor/backend/internal/sourceserver', 'monitor/backend/cmd/cdc-metrics-source'):
        files.extend(str(path.relative_to(ROOT)) for path in sorted((ROOT/folder).glob('*.go')) if not path.name.endswith('_test.go'))
    return files

@sources.observed('private-state')
def state_directory(env, scope, kind):
    if kind not in PREFIXES or scope != PRODUCTS.get(kind, ('infra', kind))[0]: raise Error('Invalid Infra source selection')
    configured = env.get(PREFIXES[kind]+'_STATE_DIR')
    path = Path(configured) if configured else Path.home().resolve()/'.local/state/addp'/hashlib.sha256(str(ROOT).encode()).hexdigest()[:16]/'cdc-metrics'/kind
    if not path.is_absolute() or '..' in path.parts or path.resolve().is_relative_to(ROOT): raise Error('Invalid CDC private state')
    if any(parent.is_symlink() for parent in (path,*path.parents)): raise Error('Linked CDC private state')
    return path

class Component:
    @sources.observed('deployment-check')
    def __init__(self, docker, kind, *, service=None):
        scope, owner_service = PRODUCTS.get(kind, ('infra', kind))
        service = service or owner_service
        self.docker = docker
        project, working = deployment.inventory.SCOPES[scope]
        ids = docker.run(['ps','-aq','--no-trunc','--filter','label=com.docker.compose.project='+project,
                          '--filter','label=com.docker.compose.service='+service]).split()
        if len(ids) != 1 or not deployment.inventory.identifier(ids[0]): raise Error('CDC component is not unique')
        self.identity = ids[0]
        self.facts = docker.fact(self.identity)
        labels = self.facts['labels'] or {}
        if (not self.facts['running'] or labels.get('com.docker.compose.project') != project or labels.get('com.docker.compose.service') != service
            or labels.get('com.docker.compose.project.working_dir') != str(working) or labels.get('com.docker.compose.oneoff','').lower() != 'false'):
            raise Error('CDC component ownership rejected')
        networks = []
        for name, attachment in self.facts['networks'].items():
            network = json.loads(docker.run(['network','inspect',attachment['NetworkID']]))[0]
            if (network.get('Id') == attachment['NetworkID'] and network.get('Name') == name and network.get('Driver') == 'bridge'
                and (network.get('Labels') or {}).get('com.docker.compose.project') == project
                and network['Labels'].get('com.docker.compose.network') == (NETWORK if scope == 'infra' else 'business-network')):
                networks.append((name,attachment))
        if len(networks) != 1: raise Error('CDC private network rejected')
        self.network, attachment = networks[0]
        address = ipaddress.ip_address(attachment.get('IPAddress') or attachment.get('GlobalIPv6Address'))
        if not address.is_private or address.is_link_local or address.is_multicast or address.is_unspecified: raise Error('CDC private address rejected')
        self.address = str(address)

    @sources.observed('identity-recheck')
    def unchanged(self):
        if self.docker.fact(self.identity) != self.facts: raise Error('CDC component changed during source operation')

def meili_key_uid(env):
    value = env.get('ADDP_MEILISEARCH_METRICS_KEY_UID', '')
    if str(uuid.UUID(value)) != value or uuid.UUID(value).int == 0:
        raise Error('Meilisearch task Key UID is invalid')
    return value


@sources.observed('deployment-check')
def meili_config(component, env):
    uid = meili_key_uid(env)
    request = ("printf 'Authorization: Bearer %s\\n' \"$MEILI_MASTER_KEY\" | "
               'curl -q -fsS --noproxy "*" --max-time 5 --max-filesize 16384 '
               '--header @- "http://127.0.0.1:7700/keys/$1"')
    raw = component.docker.run(['exec', component.identity, 'sh', '-ec', request, 'meilisearch-key', uid], timeout=8)
    value = json.loads(raw)
    if (value.get('uid') != uid or value.get('actions') != ['tasks.get'] or value.get('indexes') != ['*']
            or not re.fullmatch('[a-f0-9]{64}', value.get('key', ''))):
        raise Error('Meilisearch task Key permissions rejected')
    return {'address':component.address, 'task_key':value['key']}


@sources.observed('account-update')
def product_password(component, kind, env):
    path = Path(env.get(PREFIXES[kind]+'_PASSWORD_FILE', ''))
    if not path.is_absolute():
        raise Error('A deployment-owned observation password is required')
    password = private.read(path, 0o600)[0].rstrip('\n')
    if not re.fullmatch('[0-9a-f]{64}', password):
        raise Error('Observation credential must be a generated 256-bit value')
    # Inspect accounts using the product deployment owner. Only the dedicated
    # observer credential enters the source volume. Never alter an existing grant.
    if kind == 'mysql':
        command = ['exec','-i',component.identity,'sh','-ec',
                   'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql --batch --skip-column-names']
        existing = component.docker.run(command, input="SELECT Host FROM mysql.user WHERE User='addp_metrics_reader';").splitlines()
        if not existing:
            component.docker.run(command, input="CREATE USER 'addp_metrics_reader'@'%' IDENTIFIED BY '"+password+"';")
        elif existing != ['%']:
            raise Error('MySQL observation account is ambiguous')
        grants = component.docker.run(command, input="SHOW GRANTS FOR 'addp_metrics_reader'@'%';").splitlines()
        if grants != ['GRANT USAGE ON *.* TO `addp_metrics_reader`@`%`']:
            raise Error('MySQL observation account has unexpected privileges')
    else:
        command = ['exec','-i',component.identity,'redis-cli','--json']
        account = json.loads(component.docker.run(command+['ACL','GETUSER','addp_metrics_reader']))
        digest = hashlib.sha256(password.encode()).hexdigest()
        if account is None:
            # The deployment does not persist ACLs. Re-establish only this named
            # observer on recovery, with no key patterns or graph commands.
            component.docker.run(command, input='ACL SETUSER addp_metrics_reader reset on #'+digest+' +info\n')
            account = json.loads(component.docker.run(command+['ACL','GETUSER','addp_metrics_reader']))
        if (not isinstance(account,dict) or 'on' not in account['flags'] or account['passwords'] != [digest]
                or account['commands'] != '-@all +info' or account['keys'] or account['channels'] or account['selectors']):
            raise Error('FalkorDB observation account has unexpected privileges')
    return password


def run(action, scope, kind, env):
    state = state_directory(env,scope,kind)
    prefix = PREFIXES[kind]
    enabled = env.get(prefix+'_ENABLED','false')
    if action != 'down' and enabled not in ('true','false'): raise Error('Invalid CDC enable flag')
    if action == 'recover' and enabled == 'false': return 'recovery not selected'
    if action == 'recover' and not state.exists(): return 'not installed; recovery skipped'
    if (action == 'down' or enabled == 'false') and not state.exists(): return 'not installed; disabled'
    with private.locked(state):
        if action == 'recover' and not (state/'compose.json').exists(): return 'not installed; recovery skipped'
        docker = deployment.Docker(env)
        source = sources.Source(docker,scope,state,ROOT,kind,deployment.inventory.metrics_project.PROJECT,Error)
        if action == 'down' or enabled == 'false':
            source.stop(); return 'disabled; owned source removed'
        ip,port,material = sources.transport(env,ROOT,prefix,Error)
        component = Component(docker,kind)
        config = meili_config(component,env) if kind == 'meilisearch' else {'kind':kind,'address':component.address}
        expected = {'schema':'addp.meilisearch-metrics/v1' if kind == 'meilisearch' else 'addp.deployment-metrics/v1' if kind in PRODUCTS else 'addp.cdc-metrics/v1','workspace':str(ROOT),'kind':kind,
                    'docker_engine_id':docker.info['id'],'project':deployment.inventory.SCOPES[scope][0]}
        owner = state/'owner.json'
        if os.path.lexists(owner):
            if json.loads(private.read(owner,0o600)[0]) != expected: raise Error('CDC source deployment owner changed')
        elif action == 'recover':
            raise Error('CDC source installation proof is missing')

        if kind in ('mysql', 'falkordb'):
            config['password'] = product_password(component, kind, env)

        if action == 'recover':
            if not (state/'compose.json').exists(): return 'not installed; recovery skipped'
            return source.recover(component,config,ip,port,material,component.unchanged)
        image = source.build(source_inputs(kind))
        component.unchanged()
        sources.write(owner,json.dumps(expected,sort_keys=True))
        source.stop()
        try:
            source.start(component,image,config,ip,port,material,component.unchanged)
        except Exception as error:
            try: source.stop()
            except Exception: error.source_recovery_cleanup_failed = True
            raise
        return 'source deployed; successful scraping requires Monitor verification'

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action',choices=('up','down','recover'))
    parser.add_argument('--scope',choices=('infra','business'),required=True)
    parser.add_argument('--kind',choices=tuple(PREFIXES),required=True)
    args = parser.parse_args()
    try:
        print(run(args.action,args.scope,args.kind,os.environ))
    except Exception as error:
        stage = getattr(error,'observation_stage','deployment-check')
        reason = getattr(error,'source_recovery_reason',None)
        fields = '[stage='+stage+']'
        if reason in sources.RECOVERY_REASONS: fields += '[reason='+reason+']'
        if getattr(error,'source_recovery_cleanup_failed',False): fields += '[cleanup=failed]'
        if getattr(error,'source_tls_reason',None) in sources.TLS_REASONS: fields += '[tls-reason='+error.source_tls_reason+']'
        print('Infra source operation failed '+fields,file=sys.stderr)
        return 1
    return 0

if __name__ == '__main__': raise SystemExit(main())
