"""Managed, bounded Linux sysfs capacity sampler; no device or business-file reads."""
import argparse
import os
from pathlib import Path
import platform
import re
import signal
import tempfile
import time

PREFIX = 'addp_block_capacity_'
MAX_OBJECTS = 4096
MAX_DEVICES = 100
MAX_INTEGER = 2**53 - 1
FILENAME = 'block-capacity.prom'


class Unsupported(ValueError):
    pass


def read(path, limit=65536):
    with path.open('rb') as stream:
        raw = stream.read(limit + 1)
    if len(raw) > limit:
        raise ValueError('read_budget')
    return raw.decode('ascii').strip()


def integer(path):
    value = read(path, 32)
    if not re.fullmatch(r'0|[1-9][0-9]*', value):
        raise ValueError('integer')
    return int(value)


def boot_time(proc):
    matches = re.findall(r'^btime ([0-9]+)$', read(proc / 'stat', 1 << 20), re.M)
    if len(matches) != 1:
        raise ValueError('boot')
    return int(matches[0])


def snapshot(sysfs, proc, expected_kernel):
    # Source roots are fixed by Compose; kernel-global proc facts also cover the
    # Desktop VM view without host PID, rootfs or device mounts.
    if (not expected_kernel or read(proc / 'sys/kernel/osrelease', 256) != expected_kernel
            or platform.release() != expected_kernel):
        raise Unsupported('kernel')
    boot = boot_time(proc)
    drivers = {}
    in_blocks = False
    for line in read(proc / 'devices').splitlines():
        if line == 'Block devices:':
            in_blocks = True
        elif line.endswith('devices:'):
            in_blocks = False
        elif in_blocks and line.strip():
            major, name = line.split()
            drivers[int(major)] = name
    seen_numbers = set()
    for line in read(proc / 'diskstats', 1 << 20).splitlines():
        fields = line.split()
        if len(fields) < 3:
            raise ValueError('diskstats')
        number = (int(fields[0]), int(fields[1]))
        if number in seen_numbers:
            raise ValueError('duplicate_device_number')
        seen_numbers.add(number)
    root = sysfs.resolve(strict=True)
    entries = []
    for item in (root / 'class/block').iterdir():
        entries.append(item)
        if len(entries) > MAX_OBJECTS:
            raise ValueError('object_budget')
    rows = []
    for item in sorted(entries):
        if not re.fullmatch(r'[A-Za-z0-9_.!+-]{1,255}', item.name):
            raise ValueError('device_name')
        device = item.resolve(strict=True)
        if not device.is_relative_to(root / 'devices'):
            raise ValueError('sysfs_boundary')
        if (device / 'partition').exists():
            continue
        number_text = read(device / 'dev', 32)
        if not re.fullmatch(r'[0-9]+:[0-9]+', number_text):
            raise ValueError('device_number')
        number = tuple(map(int, number_text.split(':')))
        if number not in seen_numbers or number[0] not in drivers:
            raise Unsupported('unproven_device')
        if drivers[number[0]] in ('ramdisk', 'loop'):
            continue
        sequence = integer(device / 'diskseq') if (device / 'diskseq').exists() else None
        sectors = integer(device / 'size')
        if sectors == 0:
            continue
        capacity = sectors * 512
        if capacity > MAX_INTEGER:
            raise ValueError('capacity_budget')
        # Kernel-owned attributes, never device-name prefixes or serial numbers.
        kind = ('mapped' if (device / 'dm').is_dir() else
                'raid' if (device / 'md').is_dir() else
                'unknown' if device.is_relative_to(root / 'devices/virtual') else 'device')
        rows.append((item.name, kind, capacity))
        if len(rows) > MAX_DEVICES:
            raise ValueError('device_budget')
        # Detect replacement/removal while reading. No persistent device identity
        # or capacity history is claimed by this instant-only protocol.
        if (read(device / 'dev', 32) != number_text or integer(device / 'size') != sectors or item.resolve(strict=True) != device
                or (integer(device / 'diskseq') if (device / 'diskseq').exists() else None) != sequence):
            raise ValueError('device_changed')
    if boot_time(proc) != boot or read(proc / 'sys/kernel/osrelease', 256) != expected_kernel:
        raise ValueError('boot_changed')
    return boot, rows


def publish(directory, *, sampled_at, boot=0, rows=(), success=False, supported=True):
    # Only the owned file is accepted. An arbitrary *.prom would otherwise be
    # able to forge native node metrics in the same exporter scrape.
    if any(p.name != FILENAME for p in directory.iterdir()):
        (directory / FILENAME).unlink(missing_ok=True)
        raise ValueError('foreign_file')
    lines = [f'{PREFIX}success {int(success)}',
             f'{PREFIX}supported {int(supported)}',
             f'{PREFIX}sample_time_seconds {sampled_at}',
             f'{PREFIX}boot_time_seconds {boot}',
             f'{PREFIX}device_count {len(rows) if success else 0}']
    if success:
        lines += [f'{PREFIX}bytes{{device="{name}",kind="{kind}"}} {size}'
                  for name, kind, size in rows]
    descriptor, temporary = tempfile.mkstemp(prefix='.block-', dir=directory)
    try:
        with os.fdopen(descriptor, 'w') as stream:
            stream.write('\n'.join(lines) + '\n')
            stream.flush()
            os.fsync(stream.fileno())
        os.chmod(temporary, 0o600)
        os.replace(temporary, directory / FILENAME)
    finally:
        Path(temporary).unlink(missing_ok=True)


def sample(sysfs, proc, directory, expected_kernel):
    try:
        boot, rows = snapshot(sysfs, proc, expected_kernel)
    except Unsupported:
        publish(directory, sampled_at=int(time.time()), supported=False)
        return False
    except (OSError, ValueError, TimeoutError):
        publish(directory, sampled_at=int(time.time()))
        return False
    publish(directory, sampled_at=int(time.time()), boot=boot, rows=rows, success=True)
    return True


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--kernel', required=True)
    parser.add_argument('--sysfs', choices=('/sys', '/source-sys'), required=True)
    parser.add_argument('--proc', choices=('/proc', '/source-proc'), required=True)
    args = parser.parse_args()
    directory = Path('/textfile')
    stopped = False
    def stop(*_):
        nonlocal stopped
        stopped = True
    def timeout(*_):
        raise TimeoutError('sample_budget')
    signal.signal(signal.SIGTERM, stop)
    signal.signal(signal.SIGINT, stop)
    signal.signal(signal.SIGALRM, timeout)
    try:
        while not stopped:
            start = time.monotonic()
            signal.alarm(2)
            try:
                sample(Path(args.sysfs), Path(args.proc), directory, args.kernel)
            finally:
                signal.alarm(0)
            while not stopped and time.monotonic() - start < 15:
                time.sleep(0.25)
    finally:
        (directory / FILENAME).unlink(missing_ok=True)


if __name__ == '__main__':
    main()
