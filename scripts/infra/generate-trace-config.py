"""Render the sole shared Alloy configuration from Monitor's fixed policy.

Secret files are deployment-owned; this command creates no credentials and
does not activate a process. Its receipt describes intent, not delivery.
"""
from pathlib import Path
import json
import os
import stat
import tempfile

ROOT = Path(__file__).resolve().parents[2]
FILES = ('server.crt', 'server.key', 'source-ca.crt', 'query-ca.crt')


def render(directory, logs):
    p = Path(directory)
    if not p.is_absolute() or p.is_symlink() or not p.is_dir() or p.resolve().is_relative_to(ROOT):
        raise ValueError('Trace deployment requires a private absolute directory outside the repository')
    if p.stat().st_mode & 0o077:
        raise ValueError('Trace deployment directory must be owner-only')
    for name in FILES:
        f = p / name
        if f.is_symlink() or not f.is_file() or not 0 < f.stat().st_size <= 256 * 1024 or f.stat().st_nlink != 1:
            raise ValueError('Invalid trace certificate file')
        if name.endswith('.key') and stat.S_IMODE(f.stat().st_mode) & 0o077:
            raise ValueError('Trace key must be owner-only')
    policy = json.loads((ROOT / 'monitor/backend/internal/tracequery/policy.json').read_text())
    if (policy.get('version') != 2 or set(policy) != {'version', 'decision_wait_seconds', 'pending_traces',
            'normal_percent', 'slow_ms', 'retained_spans_per_second', 'retention_hours'}
            or any(type(v) is not int or v <= 0 for v in policy.values()) or policy['normal_percent'] > 100):
        raise ValueError('Unpublished trace policy definition')
    text = (ROOT / 'scripts/infra/traces.alloy').read_text()
    for k, v in dict(DECISION_WAIT=policy['decision_wait_seconds'], PENDING_TRACES=policy['pending_traces'],
                     RETAINED_SPANS=policy['retained_spans_per_second'], SLOW_MS=policy['slow_ms'],
                     NORMAL_PERCENT=policy['normal_percent']).items():
        text = text.replace('@' + k + '@', str(v))
    if logs:
        text = (ROOT / 'scripts/infra/runtime-logs.alloy').read_text() + '\n' + text
    text = '// addp.trace.policy/v2; definition version 2, deployment intent only\n' + text
    fd, temporary = tempfile.mkstemp(prefix='.alloy-', dir=p)
    try:
        with os.fdopen(fd, 'w') as out:
            out.write(text)
            out.flush()
            os.fsync(out.fileno())
        os.chmod(temporary, 0o600)
        os.replace(temporary, p / 'runtime.alloy')
    finally:
        Path(temporary).unlink(missing_ok=True)
    tempo = (ROOT / 'scripts/infra/tempo.yml').read_text().replace('@RETENTION_HOURS@', str(policy['retention_hours'])).replace('@INGESTION_SLACK@', str(policy['decision_wait_seconds'] + 180))
    fd, temporary = tempfile.mkstemp(prefix='.tempo-', dir=p)
    try:
        with os.fdopen(fd, 'w') as out:
            out.write(tempo)
            out.flush()
            os.fsync(out.fileno())
        os.chmod(temporary, 0o644)
        os.replace(temporary, p / 'tempo.yml')
    finally:
        Path(temporary).unlink(missing_ok=True)


if __name__ == '__main__':
    try:
        flag = os.environ.get('ADDP_OBSERVABILITY_LOGS_ENABLED', 'true')
        if flag not in ('true', 'false'):
            raise ValueError('Invalid log capability selection')
        render(os.environ.get('ADDP_TRACE_DEPLOYMENT_DIR', ''), flag == 'true')
    except (ValueError, OSError, KeyError):
        raise SystemExit('trace_deployment_invalid') from None
