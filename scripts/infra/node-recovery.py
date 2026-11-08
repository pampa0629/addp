"""Exact existing-container recovery for an explicitly selected node source."""
import hashlib
import importlib.util
import json
import os
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


def load(name):
    spec = importlib.util.spec_from_file_location('node_recovery_'+name, ROOT/'scripts/utils'/name)
    module = importlib.util.module_from_spec(spec); spec.loader.exec_module(module)
    return module


private = load('private-state.py')
deployment = load('deployment-docker.py')
group = load('metrics-project.py')
OWNER = 'io.addp.node-metrics.owner'
REASONS = frozenset({'node-proof-missing', 'node-proof-disabled', 'node-proof-invalid',
                     'node-source-incomplete', 'node-ownership-rejected', 'node-inputs-changed',
                     'node-installation-changed', 'source-image-unavailable'})
FACT = ('{"id":{{json .Id}},"image":{{json .Image}},"running":{{json .State.Running}},'
        '"command":{{json .Config.Cmd}},"entrypoint":{{json .Config.Entrypoint}},'
        '"user":{{json .Config.User}},"labels":{{json .Config.Labels}},'
        '"host":{{json .HostConfig}},"mounts":{{json .Mounts}},'
        '"networks":{{json .NetworkSettings.Networks}}}')


def rejected(message, reason):
    error = ValueError(message)
    error.node_recovery_reason = reason
    return error


def state_directory():
    return Path.home().resolve()/'.local/state/addp'/hashlib.sha256(str(ROOT).encode()).hexdigest()[:16]/'node-metrics'


def write(path, document):
    previous = private.read(path,0o600)[0] if os.path.lexists(path) else None
    private.atomic(path,json.dumps(document,sort_keys=True),0o600,previous=previous)


def decode(text):
    def unique(pairs):
        result = {}
        for key,value in pairs:
            if key in result: raise ValueError('Duplicate node installation field')
            result[key] = value
        return result
    return json.loads(text,object_pairs_hook=unique)


def containers(docker, project):
    ids = group.container_ids(docker.run,project,{'node-exporter','block-capacity'})
    result = {}
    for identity in ids:
        item = decode(docker.run(['inspect','--format',FACT,identity]))
        labels = item['labels'] or {}; service = labels.get('com.docker.compose.service')
        if (labels.get(OWNER) != str(ROOT) or labels.get('com.docker.compose.project') != project
                or service not in {'node-exporter','block-capacity'} or service in result):
            raise rejected('Node recovery ownership or service set rejected', 'node-ownership-rejected')
        item['mounts'] = sorted(item['mounts'],key=lambda value:json.dumps(value,sort_keys=True))
        # Addresses/endpoint IDs legitimately change on restart; attached network
        # identities must remain the installation's exact set.
        item['networks'] = {name:value['NetworkID'] for name,value in (item['networks'] or {}).items()}
        result[service] = item
    return result


def canonical_containers(items):
    result = {}
    for name,item in items.items():
        if not isinstance(item,dict) or not isinstance(item.get('host'),dict):
            result[name] = item
            continue
        host = dict(item['host'])
        # Moby applies these overrides only when len > 0. Preserve missing keys,
        # nonempty values, invalid types and every other HostConfig field exactly.
        for field in ('Dns','DnsOptions','DnsSearch'):
            if field in host and host[field] is None:
                host[field] = []
        result[name] = dict(item,host=host)
    return result


def stable(items):
    return canonical_containers({name:{k:v for k,v in item.items() if k!='running'} for name,item in items.items()})


def persistent_inputs(inputs):
    """Compare prior file facts without treating st_dev as durable identity.

    Keep the raw eight-field proof and in-operation snapshots unchanged. Tuples
    distinguish the comparison view from malformed persisted metadata lists.
    """
    if not isinstance(inputs,dict): return inputs
    result = dict(inputs)
    for group_name in ('templates','tls'):
        group = inputs.get(group_name)
        if not isinstance(group,dict): continue
        result[group_name] = dict(group)
        for name,item in group.items():
            if not isinstance(item,dict): continue
            metadata = item.get('metadata')
            if (type(metadata) is list and len(metadata)==8
                    and all(type(value) is int for value in metadata) and metadata[0]>=0):
                result[group_name][name] = dict(item,metadata=tuple(metadata[1:]))
    return result


def volume(docker,project):
    name = project+'_block-capacity'
    names = docker.run(['volume','ls','-q','--filter','name=^'+name+'$']).split()
    if not names: return None
    if names != [name]: raise rejected('Unexpected node capacity volume', 'node-ownership-rejected')
    value = decode(docker.run(['volume','inspect',name]))[0]
    if (value.get('Name') != name or (value.get('Labels') or {}).get(OWNER) != str(ROOT)
            or value.get('Driver') != 'local'):
        raise rejected('Node recovery volume ownership rejected', 'node-ownership-rejected')
    return value


def snapshot(docker,project,inputs):
    items = containers(docker,project)
    expected = {'node-exporter','block-capacity'} if inputs['capacity'] else {'node-exporter'}
    if set(items) != expected:
        raise rejected('Node recovery installation is incomplete; explicitly apply up', 'node-source-incomplete')
    vol = volume(docker,project)
    if (vol is not None) != inputs['capacity']:
        raise rejected('Node recovery capacity volume set changed', 'node-installation-changed')
    return {'schema':'addp.node-recovery/v1','workspace':str(ROOT),'project':project,
            'inputs':inputs,'containers':stable(items),'volume':vol},items


def recover(docker,project,env,signature,path):
    if not path.exists(): raise rejected('Node recovery installation proof missing; explicitly apply up', 'node-proof-missing')
    try:
        saved = decode(private.read(path,0o600)[0])
    except (OSError,ValueError) as error:
        error.node_recovery_reason = 'node-proof-invalid'
        raise
    if not isinstance(saved,dict) or saved.get('enabled') is not True:
        if isinstance(saved,dict) and saved.get('enabled') is False:
            raise rejected('Node recovery installation is disabled', 'node-proof-disabled')
        raise rejected('Node recovery installation proof invalid', 'node-proof-invalid')
    if isinstance(saved.get('containers'),dict):
        saved = dict(saved,containers=canonical_containers(saved['containers']))
    inputs = signature(env,docker)
    desired,items = snapshot(docker,project,inputs)
    prior_inputs,current_inputs = persistent_inputs(saved.get('inputs')),persistent_inputs(inputs)
    if dict(saved,inputs=prior_inputs) != dict(desired,enabled=True,inputs=current_inputs):
        reason = 'node-inputs-changed' if prior_inputs != current_inputs else 'node-installation-changed'
        raise rejected('Node recovery installation or inputs changed; explicitly apply up', reason)
    started = []
    def unchanged():
        current,current_items = snapshot(docker,project,signature(env,docker))
        if current != desired:
            reason = 'node-inputs-changed' if current['inputs'] != desired['inputs'] else 'node-installation-changed'
            raise rejected('Node recovery facts changed', reason)
        for item in current_items.values():
            try:
                image = decode(docker.run(['image','inspect','--format','{{json .Id}}',item['image']]))
                if image != item['image']: raise ValueError('Image identity mismatch')
            except ValueError:
                raise rejected('Node recovery installed image is unavailable', 'source-image-unavailable') from None
        return current_items
    items = unchanged()
    stopped = [items[name]['id'] for name in ('block-capacity','node-exporter') if name in items and not items[name]['running']]
    if not stopped: return 'healthy; source unchanged'
    try:
        for identity in stopped:
            current = unchanged()
            if next(item for item in current.values() if item['id']==identity)['running']:
                continue
            # Track before sending: a lost CLI result cannot prove start failed.
            started.append(identity)
            docker.run(['start',identity],timeout=30)
        current = unchanged()
        if not all(item['running'] for item in current.values()): raise ValueError('Node source did not remain running')
    except (Exception,KeyboardInterrupt) as error:
        try:
            current = containers(docker,project)
            owned = {item['id']:item for item in current.values()}
            original = {item['id']:item for item in items.values()}
            for identity in reversed(started):
                if identity not in owned or stable({'source':owned[identity]}) != stable({'source':original[identity]}):
                    raise ValueError('Node recovery cleanup identity changed')
                docker.run(['stop','--time','5',identity],timeout=15)
            current = containers(docker,project)
            if any(item['running'] for item in current.values() if item['id'] in started):
                raise ValueError('Node recovery cleanup did not stop started sources')
        except Exception: error.node_recovery_cleanup_failed = True
        raise
    return 'source recovered; successful scraping requires Monitor verification'


def manage(action,env,project,signature,operation):
    folder = state_directory()
    if action == 'recover':
        if env.get('ADDP_NODE_METRICS_ENABLED','false') == 'false':
            print('Node metrics: recovery not selected'); return
        if not folder.exists(): raise rejected('Node recovery installation proof missing; explicitly apply up', 'node-proof-missing')
    with private.locked(folder):
        path = folder/'installation.json'
        if action == 'recover':
            print('Node metrics: '+recover(deployment.Docker(env),project,env,signature,path)); return
        # Retire proof before applying changes, including a failed or interrupted up.
        write(path,{'enabled':False})
        if action == 'up':
            docker = deployment.Docker(env)
            before = signature(env,docker)
        operation()
        if action == 'up':
            after = signature(env,docker)
            if before != after: raise ValueError('Node installation inputs changed during up')
            desired,items = snapshot(docker,project,after)
            if not all(item['running'] for item in items.values()): raise ValueError('Node installation is not running')
            write(path,dict(desired,enabled=True))
