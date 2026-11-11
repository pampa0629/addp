"""One deployment Compose group; exact member selection and explicit retirement only."""
import hashlib
import json
from pathlib import Path


PROJECT = 'addp-metrics-sources'


def service(kind, scope):
    if kind == 'node' and scope == 'host': return 'node-exporter'
    if kind in {'redpanda', 'kafka-connect'} and scope == 'infra': return scope+'-'+kind+'-metrics'
    if kind not in {'postgres', 'redis', 'minio'} or scope not in {'infra', 'business'}:
        raise ValueError('Unknown metrics source member')
    return scope+'-'+kind+'-metrics'


def filters(project_name, services):
    # Docker label filters are ANDed, even for the same key. Never emulate OR
    # by repeating compose.service filters: that silently returns no containers.
    if len(services) != 1:
        raise ValueError('Docker source query requires exactly one service')
    return ['--filter', 'label=com.docker.compose.project='+project_name,
            '--filter', 'label=com.docker.compose.service='+next(iter(services))]


def container_ids(run, project_name, services):
    identities = set()
    for name in sorted(services):
        identities.update(run(['ps','-aq','--no-trunc',*filters(project_name,{name})]).split())
    return sorted(identities)


def previous_project(root):
    digest = hashlib.sha256(str(Path(root).resolve()).encode()).hexdigest()[:16]
    return PROJECT+'-'+digest


def retire(docker, root, kind, scope):
    # Only explicit up retires the selected member of the former shared group.
    old = previous_project(root)
    member = service(kind, scope)
    namespace = 'io.addp.'+kind+'-metrics'
    services = {member}
    if kind == 'node':
        services.add('block-capacity')
    elif kind == 'postgres':
        services.add(member+'-web')
    ids = container_ids(docker.run, old, services)
    selected = {}
    for identity in ids:
        fact = docker.fact(identity); labels = fact.get('labels') or {}
        name = labels.get('com.docker.compose.service')
        if (labels.get('com.docker.compose.project') != old or labels.get(namespace+'.owner') != str(root)
                or name not in services or name in selected
                or labels.get('com.docker.compose.oneoff', '').lower() != 'false'
                or kind != 'node' and labels.get(namespace+'.scope') != scope):
            raise ValueError('Previous metrics source ownership rejected; no migration performed')
        selected[name] = identity
    volume = old+'_block-capacity' if kind == 'node' else (
        old+'-'+scope+'-'+kind+'-private' if kind in {'redis','minio'} else None)
    volumes = docker.run(['volume','ls','-q','--filter','name=^'+volume+'$']).split() if volume else []
    for name in volumes:
        fact = json.loads(docker.run(['volume','inspect',name]))[0]; labels = fact.get('Labels') or {}
        if (name != volume or fact.get('Name') != name or fact.get('Driver') != 'local'
                or labels.get(namespace+'.owner') != str(root)
                or kind != 'node' and labels.get(namespace+'.scope') != scope):
            raise ValueError('Previous metrics volume ownership rejected; no migration performed')
    network = None
    if kind == 'node':
        names = docker.run(['network','ls','-q','--filter','name=^'+old+'_default$']).split()
        if names:
            facts = json.loads(docker.run(['network','inspect',*names]))
            if len(facts) != 1: raise ValueError('Previous node network ownership rejected')
            network = facts[0]; labels = network.get('Labels') or {}
            if (network.get('Name') != old+'_default' or network.get('Driver') != 'bridge'
                    or labels.get('com.docker.compose.project') != old or labels.get('com.docker.compose.network') != 'default'
                    or labels.get(namespace+'.owner') != str(root)
                    or not set(network.get('Containers') or {}).issubset(ids)):
                raise ValueError('Previous node network ownership rejected; no migration performed')
    # Validate the entire old member and its volume before any removal.
    for name in sorted(selected, key=lambda name: (name == member, name)):
        docker.run(['rm','-f',selected[name]], timeout=20)
    for name in volumes: docker.run(['volume','rm',name])
    if network is not None:
        current = json.loads(docker.run(['network','inspect',network['Id']]))[0]
        if (current.get('Id') != network['Id'] or current.get('Labels') != network.get('Labels')
                or current.get('Containers')):
            raise ValueError('Previous node network changed during retirement')
        docker.run(['network','rm',network['Id']])
