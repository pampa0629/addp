#!/usr/bin/env python3
"""Check the Quality recovery case's existing Hosted/runner registration."""
import argparse
from pathlib import Path
import re


def validate(repository):
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    match = re.search(r'(?ms)^      quality_case:\n(.*?)(?=^      \w+:)', workflow)
    if not match or any(value not in match[1] for value in ('default: full', '- full', '- bounded-recovery')):
        raise ValueError('Quality recovery requires an explicit case with the original default')
    job = re.search(r'(?ms)^  quality-hosted-t4:\n(.*?)(?=^  [a-z][a-z0-9-]*:)', workflow)
    if not job or "ADDP_ONLINE_QUALITY_CASE: ${{ inputs.quality_case || 'full' }}" not in job[1]:
        raise ValueError('Quality Hosted job must pass its registered case')
    target = re.search(r'(?m)^test-quality-online-runner:.*\n((?:\t.*\n)+)', (repository / 'Makefile').read_text())
    if not target:
        raise ValueError('Quality deterministic runner is missing')
    for name in ('quality-bounded-recovery-online', 'quality-bounded-recovery-faults'):
        for suffix in ('.py', '_test.py'):
            if not (repository / f'scripts/test/{name}{suffix}').is_file():
                raise ValueError('Quality recovery implementation/regressions are missing')
        if f'{name}_test.py' not in target[1]:
            raise ValueError('Quality recovery regression must be in its standard runner')
    if 'scripts/ci/check-quality-bounded-recovery.py' not in target[1]:
        raise ValueError('Quality recovery registration check must run on the platform')
    lifecycle = (repository / 'scripts/test/online-hosted-quality-gate.sh').read_text()
    for marker in ('full|bounded-recovery', 'before_online_application_stop',
                   'scripts.test.quality-bounded-recovery-faults resume', 'QUALITY_WORKER_LEASE_DURATION=30s',
                   'ADDP_DEV_QUALITY_WORKER_REPLICAS=2'):
        if marker not in lifecycle:
            raise ValueError('Quality recovery admission or outer cleanup is missing')
    shared = (repository / 'scripts/utils/hosted-online.sh').read_text()
    finish = shared[shared.index('finish() {'):]
    if ('before_online_application_stop ||' not in finish
            or finish.index('before_online_application_stop ||') > finish.index('stop_online_application ||')):
        raise ValueError('paused Worker recovery must precede Hosted application shutdown')


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--repository', type=Path, default=Path.cwd())
    args = parser.parse_args()
    try:
        validate(args.repository)
    except (OSError, ValueError) as error:
        parser.exit(1, f'Quality recovery registration failed: {error}\n')
    print('Quality bounded recovery registration is consistent')
