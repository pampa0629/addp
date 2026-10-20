"""Deployment-owned Business HBA directory; never a database or web API.

The container receives only runtime/, read-only. Private ownership/version facts
remain outside that mount. This module does not connect, reload or restart PG.
"""
import argparse
from contextlib import contextmanager
import fcntl
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import stat
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[3]
_spec = importlib.util.spec_from_file_location('addp_postgres_auth_source', Path(__file__).with_name('source.py'))
source = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(source)
SCHEMA = 'addp.postgres-auth-directory/v1'


class AuthenticationError(ValueError):
    """Closed deployment diagnosis: do not include file contents or secrets."""


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


def _directory(path, mode):
    # Reject linked components, including a link into another private directory.
    # Callers using /tmp on macOS pass its canonical /private/tmp spelling.
    for parent in (path, *path.parents):
        if parent.is_symlink():
            raise AuthenticationError('Linked PostgreSQL authentication directory')
    path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    try:
        path.mkdir(mode=mode)
    except FileExistsError:
        pass
    else:
        fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            os.fchmod(fd, mode)
        finally:
            os.close(fd)
    info = path.stat()
    if (not stat.S_ISDIR(info.st_mode) or info.st_uid != os.getuid()
            or stat.S_IMODE(info.st_mode) != mode):
        raise AuthenticationError('PostgreSQL authentication directory ownership or mode mismatch')


@contextmanager
def locked(directory):
    directory = Path(directory)
    if not directory.is_absolute():
        raise AuthenticationError('PostgreSQL authentication directory must be absolute')
    _directory(directory, 0o700)
    fd = os.open(directory / 'control.lock', os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW | os.O_NONBLOCK, 0o600)
    try:
        info = os.fstat(fd)
        if (not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid()
                or info.st_nlink != 1 or stat.S_IMODE(info.st_mode) != 0o600):
            raise AuthenticationError('Invalid PostgreSQL authentication lock')
        try:
            fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise AuthenticationError('PostgreSQL authentication deployment is busy') from None
        yield
    finally:
        os.close(fd)


def _read(path, mode):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    try:
        info = os.fstat(fd)
        if (not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or info.st_uid != os.getuid()
                or stat.S_IMODE(info.st_mode) != mode or info.st_size > source.HBA_LIMIT):
            raise AuthenticationError('Invalid PostgreSQL authentication file ownership or budget')
        with os.fdopen(fd, 'r', encoding='utf-8', newline='', closefd=False) as stream:
            value = stream.read(source.HBA_LIMIT + 1)
        if len(value.encode('utf-8')) > source.HBA_LIMIT or '\x00' in value:
            raise AuthenticationError('Invalid PostgreSQL authentication file')
        return value, (info.st_uid, info.st_gid)
    finally:
        os.close(fd)


def _atomic(path, value, mode, *, previous=None):
    data = value.encode('utf-8')
    if len(data) > source.HBA_LIMIT or b'\x00' in data:
        raise AuthenticationError('PostgreSQL authentication replacement exceeds budget')
    metadata = None
    if previous is None:
        if os.path.lexists(path):
            raise AuthenticationError('PostgreSQL authentication file already exists')
    else:
        current, metadata = _read(path, mode)
        if current != previous:
            raise AuthenticationError('PostgreSQL authentication input version changed')
    fd, temporary = tempfile.mkstemp(prefix='.pg-auth-', dir=path.parent)
    try:
        with os.fdopen(fd, 'wb') as stream:
            stream.write(data)
            stream.flush()
            if metadata is not None:
                os.fchown(stream.fileno(), *metadata)
            os.fchmod(stream.fileno(), mode)
            os.fsync(stream.fileno())
        if previous is None:
            # Hard-link publication is create-only; never overwrite a raced file.
            os.link(temporary, path)
        else:
            if _read(path, mode)[0] != previous:
                raise AuthenticationError('PostgreSQL authentication input version changed')
            os.replace(temporary, path)
        directory_fd = os.open(path.parent, os.O_RDONLY)
        try:
            os.fsync(directory_fd)
        finally:
            os.close(directory_fd)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


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
