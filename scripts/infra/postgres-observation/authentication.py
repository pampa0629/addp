"""Deployment-owned Business HBA directory; never a database or web API.

The container receives only runtime/, read-only. Private ownership/version facts
remain outside that mount. This module does not connect, reload or restart PG.
"""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import sys

ROOT = Path(__file__).resolve().parents[3]
_spec = importlib.util.spec_from_file_location('addp_postgres_auth_source', Path(__file__).with_name('source.py'))
source = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(source)
SCHEMA = 'addp.postgres-auth-directory/v1'


def digest(value):
    return hashlib.sha256(value.encode('utf-8')).hexdigest()


def state_directory(env):
    value = env.get('ADDP_POSTGRES_AUTH_STATE_DIR', '')
    if value:
        path = Path(value)
    else:
        workspace = hashlib.sha256(str(ROOT).encode()).hexdigest()[:16]
        path = Path.home().resolve() / '.local/state/addp' / workspace / 'postgres-auth'
    if (not path.is_absolute() or '..' in path.parts or path.is_relative_to(ROOT)
            or path.resolve().is_relative_to(ROOT)):
        raise AuthenticationError('PostgreSQL authentication state must be outside the workspace')
    return path


# Shared private-file implementation; HBA ownership and parsing stay here.
_private_spec = importlib.util.spec_from_file_location('addp_private_state', ROOT / 'scripts/utils/private-state.py')
private = importlib.util.module_from_spec(_private_spec)
_private_spec.loader.exec_module(private)
AuthenticationError = private.PrivateStateError
_directory, locked, _read, _atomic = private.directory, private.locked, private.read, private.atomic


def _baseline(value):
    if not value or source.HBA_MARKER in value:
        raise AuthenticationError('Invalid PostgreSQL authentication baseline')
    # Reuse the sole HBA parser: reserved account records and malformed quoting
    # cannot be introduced via the deployment template either.
    source.authentication_configuration(value, 'business', ['127.0.0.1/32'])
    return value


def _parts(current, *, business_only=False):
    if source.HBA_MARKER not in current:
        return '', current
    try:
        declaration = json.loads(current.split('\n', 1)[0][len(source.HBA_BEGIN):])
        if (type(declaration) is not dict or set(declaration) != {'scope', 'database', 'networks'}
                or business_only and declaration.get('scope') != 'business'
                or source.authentication_configuration(current, **declaration) != current):
            raise ValueError()
        end = current.index(source.HBA_END) + len(source.HBA_END)
    except (TypeError, ValueError):
        raise AuthenticationError('PostgreSQL authentication block drift') from None
    return current[:end], current[end:]


def _owner(directory, workspace):
    previous, _ = _read(directory / 'owner.json', 0o600)
    value = json.loads(previous)
    if (type(value) is not dict or set(value) != {'schema', 'workspace', 'baseline_sha256'}
            or value['schema'] != SCHEMA or value['workspace'] != str(Path(workspace).resolve())
            or type(value['baseline_sha256']) is not str
            or not re.fullmatch('[0-9a-f]{64}', value['baseline_sha256'])):
        raise AuthenticationError('PostgreSQL authentication workspace mismatch')
    return previous, value


def prepare_business(directory, baseline, *, workspace=ROOT):
    """Prepare canonical runtime directory before starting Business PostgreSQL."""
    baseline = _baseline(baseline)
    directory = Path(directory)
    if directory.resolve().is_relative_to(Path(workspace).resolve()):
        raise AuthenticationError('PostgreSQL authentication state must be outside the workspace')
    with locked(directory):
        runtime = directory / 'runtime'
        _directory(runtime, 0o755)
        owner_file, hba = directory / 'owner.json', runtime / 'pg_hba.conf'
        owner = {'schema': SCHEMA, 'workspace': str(Path(workspace).resolve()), 'baseline_sha256': digest(baseline)}
        owner_text = json.dumps(owner, sort_keys=True, separators=(',', ':')) + '\n'
        if not os.path.lexists(owner_file):
            if any(runtime.iterdir()):
                raise AuthenticationError('Unowned PostgreSQL authentication directory')
            _atomic(hba, baseline, 0o644)
            _atomic(owner_file, owner_text, 0o600)
        else:
            previous_owner, existing = _owner(directory, workspace)
            current, _ = _read(hba, 0o644)
            prefix, previous_baseline = _parts(current, business_only=True)
            if digest(previous_baseline) != existing['baseline_sha256']:
                raise AuthenticationError('PostgreSQL authentication baseline drift')
            if baseline != previous_baseline:
                _atomic(hba, prefix + baseline, 0o644, previous=current)
                _atomic(owner_file, owner_text, 0o600, previous=previous_owner)
        return runtime


def replace(directory, expected, replacement, *, workspace=ROOT):
    """Version-checked replacement under the same deployment lock.

    The caller owns database identity/session checks, reload and real connection
    verification. A successful file write alone never activates a source.
    """
    with locked(Path(directory)):
        _, owner = _owner(Path(directory), workspace)
        _directory(Path(directory) / 'runtime', 0o755)
        for value in (expected, replacement):
            if digest(_parts(value)[1]) != owner['baseline_sha256']:
                raise AuthenticationError('PostgreSQL authentication baseline drift')
        _atomic(Path(directory) / 'runtime/pg_hba.conf', replacement, 0o644, previous=expected)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=['prepare-business'])
    parser.parse_args()
    try:
        if os.environ.get('ADDP_BUSINESS_POSTGRES_AUTH_DIR'):
            raise AuthenticationError('Business authentication mount is a generated input')
        baseline = (ROOT / 'business/postgres/pg_hba.conf').read_bytes().decode('utf-8')
        print(prepare_business(state_directory(os.environ), baseline))
    except AuthenticationError as error:
        print(str(error), file=sys.stderr)
        return 1
    except (OSError, ValueError):
        print('PostgreSQL authentication directory unavailable; ownership, policy or input must be checked', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
