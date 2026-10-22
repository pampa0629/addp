"""Canonical read-only cgroup-v2 measurements for the 4 CPU / 2 GiB capacity profile."""
from pathlib import Path

class ResourceError(RuntimeError):
    pass


def cgroup_directory(pid,proc=Path('/proc'),root=Path('/sys/fs/cgroup')):
    groups=[line.split(':',2) for line in (proc/str(pid)/'cgroup').read_text().splitlines()]
    unified=[parts[2] for parts in groups if len(parts)==3 and parts[0]=='0' and not parts[1]]
    if len(unified)!=1 or '..' in Path(unified[0]).parts:
        raise ResourceError('capacity requires an unambiguous cgroup v2 path')
    return root/unified[0].lstrip('/')


def read_metrics(directory,proc=Path('/proc')):
    events={key:int(value) for key,value in (line.split() for line in (directory/'memory.events').read_text().splitlines())}
    cpu={key:int(value) for key,value in (line.split() for line in (directory/'cpu.stat').read_text().splitlines())}
    maximum=int((directory/'memory.max').read_text()); quota,period=map(int,(directory/'cpu.max').read_text().split())
    if maximum!=2*(1<<30) or quota/period!=4 or int((directory/'memory.swap.max').read_text())!=0:
        raise ResourceError('capacity observed cgroup limits are wrong')
    high_water={}
    for value in (directory/'cgroup.procs').read_text().split():
        try:
            status=(proc/value/'status').read_text()
            for line in status.splitlines():
                if line.startswith('VmHWM:'): high_water[value]=int(line.split()[1])*1024
        except FileNotFoundError:
            continue
    return {'memory_current_bytes':int((directory/'memory.current').read_text()),
        'memory_peak_bytes':int((directory/'memory.peak').read_text()),'memory_limit_bytes':maximum,
        'cpu_quota':quota/period,'cpu_usage_usec':cpu['usage_usec'],
        'oom':events.get('oom',0),'oom_kill':events.get('oom_kill',0),
        'memory_max_events':events.get('max',0),'swap_current_bytes':int((directory/'memory.swap.current').read_text()),
        'process_peak_rss_bytes':sum(high_water.values())}
