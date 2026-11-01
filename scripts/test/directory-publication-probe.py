"""Standalone, disposable host rename / Linux bind-mount reproducer.

ADDP standard entry: make test-monitor-metrics MONITOR_METRICS_TEST_ARGS=--case=directory-publication
A copied file runs independently of ADDP: python3 directory-publication-probe.py
It uses only Python stdlib, Docker and the existing pinned PG image's Bash.
No PostgreSQL process, SQL, account, application config, port or host setting.
"""
import argparse
import json
import os
from pathlib import Path
import re
import selectors
import subprocess
import tempfile
import time
import uuid

IMAGE = 'postgres:15@sha256:74e110c41804365e3915fcc09d5e7a1eff50161aaa94d5da0e58e0cd75ae509c'

# No external commands, subshells or child processes: reads, timed waits and
# filename expansion all execute in this one Bash process.
DIRECTORY_PROCESS_SCRIPT = r'''
set -eu
exec 2>/dev/null
a=$1; b=$2
first=$a; second=$b
if [[ "$3" == b ]]; then first=$b; second=$a; fi
sample() {
    local phase=$1 av bv value uptime rest
    if IFS= read -r value < "$a"; then av=$value; else av=unreadable; fi
    if IFS= read -r value < "$b"; then bv=$value; else bv=unreadable; fi
    case "$av" in before|after-version|unreadable) ;; *) av=other ;; esac
    case "$bv" in before|after-version|unreadable) ;; *) bv=other ;; esac
    IFS=' ' read -r uptime rest < /proc/uptime
    printf '%s\t%s\t%s\t%s\t%s\n' "$phase" "$$" "$uptime" "$av" "$bv"
}
enumerate() {
    local target=$1 entry found=0
    local entries=("${target%/*}"/*)
    for entry in "${entries[@]}"; do
        if [[ "$entry" == "$target" ]]; then found=1; fi
    done
    [[ "$found" == 1 ]]
}
sample baseline
IFS= read -r -t 2 signal
[[ "$signal" == published ]]
sample published
if IFS= read -r -t 1 signal; then exit 75; fi
sample control_before_enum
enumerate "$first"
sample enumerate_first
if IFS= read -r -t 1 signal; then exit 75; fi
sample control_before_second_enum
enumerate "$second"
sample enumerate_second
'''


def directory_process_observations(data):
    phases = ['baseline','published','control_before_enum','enumerate_first','control_before_second_enum','enumerate_second']
    lines = data.decode('ascii').splitlines()
    assert len(lines)==len(phases), 'Owned directory process output incomplete'
    rows, pids, times = [], set(), []
    for phase,line in zip(phases,lines):
        fields = line.split('\t')
        assert len(fields)==5 and fields[0]==phase, 'Owned directory process phase mismatch'
        _,pid,clock,a,b = fields
        assert re.fullmatch(r'[1-9][0-9]{0,9}',pid) and re.fullmatch(r'[0-9]{1,12}\.[0-9]{1,6}',clock), 'Owned directory process clock or identity invalid'
        assert a in ('before','after-version','unreadable','other') and b in ('before','after-version','unreadable','other'), 'Owned directory process state invalid'
        pids.add(pid);times.append(float(clock))
        rows.append({'phase':phase,'elapsed_ms':round((times[-1]-times[0])*1000,2),'a':a,'b':b})
    assert len(pids)==1 and times==sorted(times) and times[-1]-times[0]<=8, 'Owned directory process identity or monotonic order changed'
    assert round((times[2]-times[1])*1000)>=990 and round((times[4]-times[3])*1000)>=990, 'Owned directory no-enumeration interval incomplete'
    assert rows[0]['a']==rows[0]['b']=='before', 'Owned directory process baseline differs'
    return {'single_process':True,'samples':rows}

def observe(command, publish):
    deadline = time.monotonic()+8
    process = subprocess.Popen(command,stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.DEVNULL)
    data = bytearray()
    try:
        with selectors.DefaultSelector() as selector:
            selector.register(process.stdout,selectors.EVENT_READ)
            def receive():
                remaining = deadline-time.monotonic()
                assert remaining>0 and selector.select(remaining), 'Owned directory process deadline reached'
                chunk = os.read(process.stdout.fileno(),4096)
                assert len(data)+len(chunk)<=8192, 'Owned directory process output exceeds budget'
                data.extend(chunk)
                return bool(chunk)
            while b'\n' not in data:
                assert receive(), 'Owned directory process ended before baseline'
            # Validate the baseline before changing either test-owned file.
            baseline = bytes(data).split(b'\n',1)[0].decode('ascii').split('\t')
            assert len(baseline)==5 and baseline[0]=='baseline' and baseline[3:]==['before','before'], 'Owned directory baseline unavailable'
            publish()
            process.stdin.write(b'published\n');process.stdin.flush()
            while receive():
                pass
            remaining = deadline-time.monotonic()
            assert remaining>0 and process.wait(timeout=remaining)==0, 'Owned directory process failed'
            return directory_process_observations(bytes(data))
    finally:
        try:
            if process.poll() is None:
                process.kill()
                process.wait(timeout=2)
        finally:
            process.stdin.close();process.stdout.close()


def publish(path):
    """Ordinary POSIX write / fsync / rename; no ADDP authentication code."""
    fd, pending = tempfile.mkstemp(prefix='.replace-',dir=path.parent)
    try:
        with os.fdopen(fd,'wb') as stream:
            stream.write(b'after-version\n');stream.flush()
            os.fchmod(stream.fileno(),0o644);os.fsync(stream.fileno())
        os.replace(pending,path)
        directory = os.open(path.parent,os.O_RDONLY)
        try:os.fsync(directory)
        finally:os.close(directory)
        assert path.read_bytes()==b'after-version\n', 'Host atomic replacement differs'
    finally:
        Path(pending).unlink(missing_ok=True)


def run(project):
    assert re.fullmatch(r'addp-metrics-t2-[0-9a-f]{32}',project), 'Private project identity required'
    if subprocess.run(['docker','image','inspect',IMAGE],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,timeout=5).returncode:
        subprocess.run(['docker','pull','--quiet',IMAGE],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,check=True,timeout=90)
    result = {'project':project,'image':IMAGE,'database_started':False,'trials':[],'cleanup_verified':False}
    names = []
    with tempfile.TemporaryDirectory(prefix='addp-directory-repro-') as temporary:
        runtime = Path(temporary).resolve()
        try:
            for order in ('a','b'):
                root = runtime/order;root.mkdir(mode=0o755)
                paths = []
                for side in ('a','b'):
                    directory = root/side;directory.mkdir(mode=0o755)
                    path = directory/'item';path.write_bytes(b'before\n');path.chmod(0o644)
                    paths.append(path)
                name = project+'-directory-'+order;names.append(name)
                command = ['docker','run','--rm','-i','--name',name,
                    '--label','com.docker.compose.project='+project,
                    '--label','addp.directory-publication.owner='+project,
                    '--network','none','--read-only','--cap-drop','ALL','--security-opt','no-new-privileges',
                    '--cpus','0.5','--memory','64m','--pids-limit','32',
                    '--mount','type=bind,src='+str(root)+',dst=/probe,readonly',
                    # Mask the base image's VOLUME: no anonymous database volume.
                    '--mount','type=tmpfs,dst=/var/lib/postgresql/data,tmpfs-size=65536',
                    '--entrypoint','/bin/bash',IMAGE,'--noprofile','--norc','-c',
                    DIRECTORY_PROCESS_SCRIPT,'owned-directory-observation','/probe/a/item','/probe/b/item',order]
                def replace():
                    for path in paths:publish(path)
                trial = observe(command,replace)
                trial['enumerated_first'] = order
                result['trials'].append(trial)
        except (OSError,ValueError,AssertionError,subprocess.SubprocessError) as error:
            result['failure_class'] = type(error).__name__
        finally:
            cleaned = True
            for name in names:
                record = subprocess.run(['docker','inspect','--format','{{json .Config.Labels}}',name],
                    capture_output=True,text=True,timeout=5)
                if record.returncode==0:
                    labels = json.loads(record.stdout)
                    if labels.get('addp.directory-publication.owner')!=project or labels.get('com.docker.compose.project')!=project:
                        cleaned=False;continue
                    removed = subprocess.run(['docker','rm','--force','--volumes',name],
                        stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,timeout=5)
                    cleaned = removed.returncode==0 and cleaned
            for resource,command in [('containers',['docker','ps','-aq']),('networks',['docker','network','ls','-q']),('volumes',['docker','volume','ls','-q'])]:
                remaining = subprocess.run(command+['--filter','label=com.docker.compose.project='+project],
                    capture_output=True,text=True,timeout=5)
                zero = remaining.returncode==0 and not remaining.stdout.strip()
                result['zero_'+resource] = zero;cleaned = cleaned and zero
            result['cleanup_verified'] = cleaned
    result['temporary_removed'] = not runtime.exists()
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--project',default='addp-metrics-t2-'+uuid.uuid4().hex)
    args = parser.parse_args()
    try:
        result = run(args.project)
    except (OSError,ValueError,AssertionError,subprocess.SubprocessError) as error:
        print(json.dumps({'failure_class':type(error).__name__,'cleanup_verified':False}),flush=True)
        return 2
    print(json.dumps(result),flush=True)
    complete = len(result['trials'])==2 and result['cleanup_verified'] and result['temporary_removed'] and 'failure_class' not in result
    visible = complete and all(row['a']==row['b']=='after-version' for trial in result['trials'] for row in trial['samples'][1:])
    # Exit 1 means the filesystem contract is broken, even if enumeration heals it.
    return 0 if visible else 1


if __name__=='__main__':
    raise SystemExit(main())
