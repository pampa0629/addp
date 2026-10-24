"""Deployment-only cgroup v2 proofs. No Docker/host access is given to apps."""
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import stat
import time
import uuid


def bounded(path, limit=8192):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, 'rb') as source:
        if not stat.S_ISREG(os.fstat(source.fileno()).st_mode):
            raise ValueError('not a kernel file')
        value = source.read(limit + 1)
    if not value or len(value) > limit:
        raise ValueError('kernel file budget')
    return value.decode('ascii').strip()


def unsigned(value):
    if not re.fullmatch(r'0|[1-9][0-9]{0,19}', value) or int(value) > 2**64-1:
        raise ValueError('invalid kernel integer')
    return int(value)


def cpus(value):
    # Count canonical ordered ranges without materializing an unbounded set.
    count, last = 0, -1
    for part in value.split(','):
        span = part.split('-')
        if len(span) not in (1, 2):
            raise ValueError('invalid cpuset')
        lo, hi = unsigned(span[0]), unsigned(span[-1])
        if lo <= last or hi < lo or hi >= 2**20:
            raise ValueError('invalid cpuset')
        count += hi-lo+1
        last = hi
    return count


def limits(groups):
    """Input is leaf-to-root, root excluded. Every visible ancestor is required."""
    cpu, memory, assigned = [], [], False
    for group in groups:
        fields = group['cpu.max'].split()
        if len(fields) != 2 or unsigned(fields[1]) == 0:
            raise ValueError('invalid cpu quota')
        if fields[0] != 'max':
            quota = unsigned(fields[0])
            if quota == 0:
                raise ValueError('invalid cpu quota')
            cpu.append(quota / unsigned(fields[1]))
        # Burst bandwidth is not the same upper bound as quota/period. The
        # first profile does not normalize it using an understated divisor.
        if unsigned(group['cpu.max.burst']) != 0:
            raise ValueError('CPU burst limit profile is unsupported')
        if group['memory.max'] != 'max':
            size = unsigned(group['memory.max'])
            if size == 0 or size > 2**53-1:
                raise ValueError('invalid memory limit')
            memory.append(size)
        if group['cpuset.cpus']:
            cpus(group['cpuset.cpus'])
            assigned = True
    effective = cpus(groups[0]['cpuset.cpus.effective'])
    return (min([effective, *cpu]) if cpu or assigned else None,
            min(memory) if memory else None)


def kernel_proof(pid, container_id, *, proc=Path('/proc'), root=Path('/sys/fs/cgroup')):
    """Called only by a trusted native Linux host publisher, never in an app."""
    if type(pid) is not int or pid <= 1:
        raise ValueError('invalid container host pid')
    # A subtree mount/private observer is not full hierarchy evidence.
    for ns in ('pid', 'cgroup'):
        if (proc/'self/ns'/ns).stat().st_ino != (proc/'1/ns'/ns).stat().st_ino:
            raise ValueError('publisher is not in host namespaces')
    mounts = bounded(proc/'self/mountinfo', 128*1024).splitlines()
    matches = [line.split() for line in mounts if ' - cgroup2 ' in line and line.split()[4] == str(root)]
    if len(matches) != 1 or matches[0][3] != '/':
        raise ValueError('complete cgroup root unavailable')
    membership = bounded(proc/str(pid)/'cgroup')
    if not membership.startswith('0::/') or '\n' in membership:
        raise ValueError('unified cgroup required')
    path = PurePosixPath(membership[3:])
    if str(path) != membership[3:] or '..' in path.parts or len(path.parts) > 64:
        raise ValueError('invalid cgroup path')
    if path.name not in (container_id, 'docker-'+container_id+'.scope'):
        raise ValueError('container cgroup mismatch')
    boot = bounded(proc/'sys/kernel/random/boot_id', 64)
    if str(uuid.UUID(boot)) != boot or uuid.UUID(boot).int == 0:
        raise ValueError('invalid boot identity')
    leaf = root / str(path).lstrip('/')
    identity = {'kernel_boot_id': boot, 'cgroup_id': str(leaf.stat().st_ino),
                'pid_namespace_id': str((proc/str(pid)/'ns/pid').stat().st_ino)}
    groups = []
    parent = leaf
    while parent != root:
        if parent.is_symlink() or not parent.is_dir():
            raise ValueError('unsafe cgroup directory')
        record = {key: bounded(parent/key) for key in ('cpu.max', 'cpu.max.burst', 'memory.max', 'cpuset.cpus.effective')}
        # Empty inherited cpuset is valid; bounded() rejects only empty bytes.
        record['cpuset.cpus'] = bounded(parent/'cpuset.cpus')
        record['inode'] = parent.stat().st_ino
        groups.append(record)
        parent = parent.parent
    if not groups or len(groups) > 64:
        raise ValueError('incomplete hierarchy')
    cpu, memory = limits(groups)
    if bounded(proc/str(pid)/'cgroup') != membership or str(leaf.stat().st_ino) != identity['cgroup_id']:
        raise ValueError('cgroup changed')
    return {'identity': identity, 'cpu_cores': cpu, 'memory_bytes': memory,
            'hierarchy_digest': hashlib.sha256(json.dumps(groups, sort_keys=True).encode()).hexdigest()}


INSPECT = ('{"id":{{json .Id}},"running":{{json .State.Running}},"pid":{{json .State.Pid}},'
           '"started_at":{{json .State.StartedAt}},'
           '"project":{{json (index .Config.Labels "com.docker.compose.project")}},'
           '"working_dir":{{json (index .Config.Labels "com.docker.compose.project.working_dir")}},'
           '"module":{{if eq (len .Config.Entrypoint) 7}}{{if and (eq (index .Config.Entrypoint 0) "/app/runtime-log") (eq (index .Config.Entrypoint 1) "launch") (eq (index .Config.Entrypoint 2) "--module") (eq (index .Config.Entrypoint 4) "--role") (eq (index .Config.Entrypoint 6) "--")}}{{json (index .Config.Entrypoint 3)}}{{else}}null{{end}}{{else}}null{{end}},'
           '"role":{{if eq (len .Config.Entrypoint) 7}}{{if and (eq (index .Config.Entrypoint 0) "/app/runtime-log") (eq (index .Config.Entrypoint 1) "launch") (eq (index .Config.Entrypoint 2) "--module") (eq (index .Config.Entrypoint 4) "--role") (eq (index .Config.Entrypoint 6) "--")}}{{json (index .Config.Entrypoint 5)}}{{else}}null{{end}}{{else}}null{{end}}}')


def discover(docker, info, workspace, observed_at, previous=None, *, kernel=kernel_proof):
    ids = docker('ps', '--no-trunc', '--filter', 'label=com.docker.compose.project=addp-platform', '--format', '{{.ID}}').splitlines()
    if len(ids)>64 or len(set(ids))!=len(ids) or any(not re.fullmatch(r'[0-9a-f]{64}', cid) for cid in ids):
        raise ValueError('container budget')
    started = time.monotonic()
    def inspect(cid):
        return json.loads(docker('inspect','--type','container','--format',INSPECT,cid))
    proofs = []
    for cid in ids:
        if time.monotonic()-started>25:
            raise ValueError('proof budget')
        value = inspect(cid)
        if value.get('module') is None:
            continue
        if (value.get('id')!=cid or value.get('project')!='addp-platform' or value.get('running') is not True
                or not isinstance(value.get('working_dir'), str) or not Path(value['working_dir']).is_absolute() or Path(value['working_dir']).resolve()!=workspace.resolve()
                or not re.fullmatch(r'[a-z][a-z0-9_-]{0,49}', value['module'])
                or value.get('role') not in ('backend','worker','scheduler','ingress')):
            raise ValueError('container identity invalid')
        from datetime import datetime
        parse_at = lambda stamp: datetime.fromisoformat(stamp.replace('Z', '+00:00'))
        if parse_at(value['started_at']) > parse_at(observed_at):
            continue  # Never carry a rounded pre-start timestamp into this epoch.
        try:
            first = kernel(value['pid'],cid)
            second = kernel(value['pid'],cid)
            if first!=second or inspect(cid)!=value:
                raise ValueError('container boundary changed')
        except (ValueError, OSError):
            continue  # Never publish a partial hierarchy as valid evidence.
        proof = dict(first, docker_engine_id=info['id'],container_id=cid,module_name=value['module'],
                     role=value['role'],started_at=value['started_at'],observed_at=observed_at,stable_since=observed_at)
        if previous and previous.get('schema')=='addp.container-proofs/v1':
            old = [p for p in previous.get('proofs',[]) if p.get('container_id')==cid]
            if len(old)==1:
                a, b = dict(old[0]), dict(proof)
                for field in ('observed_at','stable_since'):
                    a.pop(field,None); b.pop(field,None)
                from datetime import datetime
                at = lambda s: datetime.fromisoformat(s.replace('Z','+00:00'))
                if a==b and 0 <= (at(observed_at)-at(old[0]['observed_at'])).total_seconds()<=90:
                    proof['stable_since']=old[0]['stable_since']
        proofs.append(proof)
    return {'schema':'addp.container-proofs/v1','observed_at':observed_at,'proofs':proofs}
