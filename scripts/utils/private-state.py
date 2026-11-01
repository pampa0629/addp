"""Private deployment files: exact ownership, nonblocking lock and CAS publication."""
from contextlib import contextmanager
import fcntl
import os
from pathlib import Path
import stat
import tempfile

LIMIT = 256 * 1024


class PrivateStateError(ValueError):
    """Closed diagnosis; never include paths or private content."""


def directory(path, mode):
    # Reject linked components, including a link into another private directory.
    # Callers using /tmp on macOS pass its canonical /private/tmp spelling.
    for parent in (path, *path.parents):
        if parent.is_symlink():
            raise PrivateStateError('Linked private deployment directory')
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
        raise PrivateStateError('Private deployment directory ownership or mode mismatch')


@contextmanager
def locked(path):
    folder = Path(path)
    if not folder.is_absolute():
        raise PrivateStateError('Private deployment directory must be absolute')
    directory(folder, 0o700)
    fd = os.open(folder / 'control.lock', os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW | os.O_NONBLOCK, 0o600)
    try:
        info = os.fstat(fd)
        if (not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid()
                or info.st_nlink != 1 or stat.S_IMODE(info.st_mode) != 0o600):
            raise PrivateStateError('Invalid private deployment lock')
        try:
            fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise PrivateStateError('Private deployment is busy') from None
        yield
    finally:
        os.close(fd)


def read(path, mode):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    try:
        info = os.fstat(fd)
        if (not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or info.st_uid != os.getuid()
                or stat.S_IMODE(info.st_mode) != mode or info.st_size > LIMIT):
            raise PrivateStateError('Invalid private deployment file ownership or budget')
        with os.fdopen(fd, 'r', encoding='utf-8', newline='', closefd=False) as stream:
            value = stream.read(LIMIT + 1)
        if len(value.encode('utf-8')) > LIMIT or '\x00' in value:
            raise PrivateStateError('Invalid private deployment file')
        return value, (info.st_uid, info.st_gid)
    finally:
        os.close(fd)


def atomic(path, value, mode, *, previous=None):
    data = value.encode('utf-8')
    if len(data) > LIMIT or b'\x00' in data:
        raise PrivateStateError('Private deployment replacement exceeds budget')
    metadata = None
    if previous is None:
        if os.path.lexists(path):
            raise PrivateStateError('Private deployment file already exists')
    else:
        current, metadata = read(path, mode)
        if current != previous:
            raise PrivateStateError('Private deployment input version changed')
        if current == value:
            return
    fd, temporary = tempfile.mkstemp(prefix='.addp-private-', dir=path.parent)
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
            if read(path, mode)[0] != previous:
                raise PrivateStateError('Private deployment input version changed')
            os.replace(temporary, path)
        directory_fd = os.open(path.parent, os.O_RDONLY)
        try:
            os.fsync(directory_fd)
        finally:
            os.close(directory_fd)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)
