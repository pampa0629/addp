"""GeoPython's private native-process launcher; never fork a loaded GIS worker.

posix_spawn starts an isolated, single-threaded Python bootstrap. Before exec it
closes every non-standard descriptor, changes directory if requested, and resolves
the command using the supplied PATH. Thus cwd/short commands cannot select fork,
and even deliberately inheritable parent descriptors cannot reach the command.
The bootstrap carries no GIS imports. Credentials remain on stdin, never argv.
"""
from __future__ import annotations

import io
import json
import locale
import os
import signal
import subprocess
import sys
import threading
import time

_BOOTSTRAP = '''
import json, os, sys
filename = None
try:
    os.set_inheritable(3, False)
    # This fresh -I -S interpreter has no application imports or other threads.
    # Enumerate here, not in the multithreaded parent: no descriptor-open race.
    descriptors = [int(n) for n in os.listdir('/dev/fd') if n.isdigit()]
    os.closerange(4, max(descriptors, default=3) + 1)
    if sys.argv[1]:
        filename = sys.argv[1]
        os.chdir(filename)
    filename = sys.argv[2]
    os.execvpe(sys.argv[2], sys.argv[2:], os.environ)
except OSError as error:
    os.write(3, json.dumps([error.errno, error.strerror, filename]).encode())
    os._exit(127)
except BaseException:
    os.write(3, b'[5,"native process bootstrap failed",null]')
    os._exit(127)
'''


class NativeProcess:
    """Only the pipe/wait/signal operations used by GeoPython's native calls."""

    def __init__(self, args, *, stdin=None, stdout=None, stderr=None, text=False,
                 env=None, cwd=None, bufsize=-1):
        self.args = [os.fspath(value) for value in args]
        if not self.args or any('\0' in value for value in self.args):
            raise ValueError('native process requires nonempty NUL-free arguments')
        self.pid = None
        self.returncode = None
        self.stdin = self.stdout = self.stderr = None
        self._wait_lock = threading.Lock()
        owned, child_ends, parent_ends, actions = set(), set(), {}, []

        def descriptor(value, target):
            if value is None: return target
            if value == subprocess.STDOUT:
                if target != 2: raise ValueError('STDOUT is only valid for stderr')
                return output
            if value == subprocess.PIPE:
                read, write = os.pipe(); owned.update((read, write))
                parent, child = (write, read) if target == 0 else (read, write)
                parent_ends[target] = parent; child_ends.add(child)
                return child
            if value == subprocess.DEVNULL:
                fd = os.open(os.devnull, os.O_RDONLY if target == 0 else os.O_WRONLY)
                owned.add(fd); child_ends.add(fd); return fd
            return value if isinstance(value, int) else value.fileno()

        try:
            source = descriptor(stdin, 0)
            output = descriptor(stdout, 1)
            error = descriptor(stderr, 2)
            error_read, error_write = os.pipe(); owned.update((error_read, error_write))
            actions.extend((os.POSIX_SPAWN_DUP2, fd, target)
                           for target, fd in enumerate((source, output, error)) if fd != target)
            # Target 3 is installed last, after all stdio sources have been read.
            actions.append((os.POSIX_SPAWN_DUP2, error_write, 3))
            actions.extend((os.POSIX_SPAWN_CLOSE, fd) for fd in owned if fd > 3)
            defaults = tuple(getattr(signal, name) for name in ('SIGPIPE', 'SIGXFZ', 'SIGXFSZ') if hasattr(signal, name))
            self.pid = os.posix_spawn(os.path.abspath(sys.executable),
                [sys.executable, '-I', '-S', '-c', _BOOTSTRAP, os.fspath(cwd) if cwd is not None else '', *self.args],
                dict(os.environ) if env is None else env, file_actions=actions, setsigdef=defaults)
            for fd in child_ends | {error_write}: os.close(fd); owned.remove(fd)
            failure = bytearray()
            while chunk := os.read(error_read, 4096): failure.extend(chunk)
            os.close(error_read); owned.remove(error_read)
            if failure:
                self.wait()
                number, message, filename = json.loads(failure)
                raise OSError(number, message, filename)
            for target, fd in parent_ends.items():
                stream = io.open(fd, 'wb' if target == 0 else 'rb', buffering=bufsize)
                owned.remove(fd)
                if text:
                    stream = io.TextIOWrapper(stream, encoding=locale.getencoding(), write_through=target == 0)
                setattr(self, ('stdin', 'stdout', 'stderr')[target], stream)
        except BaseException:
            if self.pid is not None and self.poll() is None:
                self.kill(); self.wait()
            for stream in (self.stdin, self.stdout, self.stderr):
                if stream is not None: stream.close()
            raise
        finally:
            for fd in owned: os.close(fd)

    def poll(self):
        with self._wait_lock:
            if self.returncode is None:
                pid, status = os.waitpid(self.pid, os.WNOHANG)
                if pid: self.returncode = os.waitstatus_to_exitcode(status)
        return self.returncode

    def wait(self, timeout=None):
        deadline = None if timeout is None else time.monotonic() + timeout
        while self.poll() is None:
            if deadline is not None and time.monotonic() >= deadline:
                raise subprocess.TimeoutExpired(self.args, timeout)
            time.sleep(.005)
        return self.returncode

    def send_signal(self, number):
        if self.poll() is None:
            try: os.kill(self.pid, number)
            except ProcessLookupError: pass

    def terminate(self): self.send_signal(signal.SIGTERM)
    def kill(self): self.send_signal(signal.SIGKILL)

    def __enter__(self): return self

    def __exit__(self, kind, value, traceback):
        if kind is not None and self.poll() is None: self.kill()
        for stream in (self.stdin, self.stdout, self.stderr):
            if stream is not None: stream.close()
        self.wait()

    def communicate(self, input=None, timeout=None):
        # Pipe readers/writer must run concurrently: neither a large credential
        # request nor full stdout/stderr may deadlock the other pipe.
        results = [None, None]
        errors = []
        def read(index, stream):
            try: results[index] = stream.read()
            except BaseException as error: errors.append(error)
            finally: stream.close()
        def write():
            try:
                offset = 0
                while input is not None and offset < len(input):
                    offset += self.stdin.write(input[offset:])
            except BrokenPipeError: pass
            except BaseException as error: errors.append(error)
            finally:
                try: self.stdin.close()
                except BrokenPipeError: pass
        threads = [threading.Thread(target=read, args=(i, stream))
                   for i, stream in enumerate((self.stdout, self.stderr)) if stream is not None]
        if self.stdin is not None: threads.append(threading.Thread(target=write))
        for thread in threads: thread.start()
        expired = None
        try: self.wait(timeout)
        except subprocess.TimeoutExpired as error:
            expired = error; self.kill(); self.wait()
        except BaseException:
            self.kill(); self.wait(); raise
        finally:
            for thread in threads: thread.join()
        if expired is not None:
            expired.output, expired.stderr = [value.encode(stream.encoding) if isinstance(value, str) else value
                for value, stream in zip(results, (self.stdout, self.stderr))]
            raise expired
        if errors: raise errors[0]
        return tuple(results)


def start(args, **kwargs):
    return NativeProcess(args, **kwargs)


def run(args, *, input=None, capture_output=False, check=False, timeout=None, **kwargs):
    if input is not None:
        if 'stdin' in kwargs: raise ValueError('stdin and input cannot be combined')
        kwargs['stdin'] = subprocess.PIPE
    if capture_output:
        if 'stdout' in kwargs or 'stderr' in kwargs: raise ValueError('capture_output and output streams cannot be combined')
        kwargs.update(stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    with start(args, **kwargs) as process:
        stdout, stderr = process.communicate(input, timeout)
        result = subprocess.CompletedProcess(args, process.returncode, stdout, stderr)
        if check: result.check_returncode()
        return result
