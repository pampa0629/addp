"""Version-bound random access shared by native single-file table codecs."""
import hashlib
import io
import json
import os
import stat
from addp_common.workflow_access import _source_storage_name


class TableSourceFile(io.RawIOBase):
    """Seek on a bound descriptor or conditional object ranges; never stage all bytes."""

    def __init__(self, plan, expected="", *, label, changed_error, client_factory, max_file_bytes, max_read_bytes, max_single_read):
        self.label, self.changed_error = label, changed_error
        self.max_read_bytes, self.max_single_read = max_read_bytes, max_single_read
        self.position = self.read_bytes = 0
        self.file = self.client = None
        access = plan["source"]["access"]
        try:
            if access["method"] == "mounted_path":
                self.file = os.fdopen(
                    os.open(access["path"], os.O_RDONLY | os.O_NOFOLLOW), "rb"
                )
                self.size = os.fstat(self.file.fileno()).st_size
            else:
                self.client = client_factory(access)
                self.bucket = _source_storage_name(access, "bucket")
                self.object = _source_storage_name(access, "object")
                fact = self.client.stat_object(self.bucket, self.object)
                self.size, self.etag = fact.size, fact.etag
                if not self.etag:
                    raise ValueError(f"{self.label} source has no stable object version")
            if not 4 <= self.size <= max_file_bytes:
                raise ValueError(f"{self.label} encoded file size exceeds budget")
            self.version = self.current_version()
            if expected and self.version != expected:
                raise self.changed_error(f"{self.label} source version changed; refresh metadata")
        except Exception:
            self.close()
            raise

    def current_version(self):
        if self.file:
            f = os.fstat(self.file.fileno())
            if not stat.S_ISREG(f.st_mode):
                raise ValueError(f"{self.label} source must be a regular file")
            identity = [f.st_dev, f.st_ino, f.st_size, f.st_mtime_ns, f.st_ctime_ns]
        else:
            fact = self.client.stat_object(self.bucket, self.object)
            identity = [self.bucket, self.object, fact.size, fact.etag, fact.version_id]
        return hashlib.sha256(
            json.dumps(identity, separators=(",", ":")).encode()
        ).hexdigest()

    def check(self):
        if self.current_version() != self.version:
            raise self.changed_error(f"{self.label} source version changed; refresh metadata")

    def readable(self):
        return True

    def seekable(self):
        return True

    def tell(self):
        return self.position

    def seek(self, offset, whence=0):
        if whence not in (0, 1, 2):
            raise ValueError("invalid table seek origin")
        position = offset + (
            0 if whence == 0 else self.position if whence == 1 else self.size
        )
        if not 0 <= position <= self.size:
            raise ValueError(f"{self.label} seek outside source")
        self.position = position
        return position

    def read(self, size=-1):
        if size is None or size < 0:
            size = self.size - self.position
        size = min(size, self.size - self.position)
        if size > self.max_single_read or self.read_bytes + size > self.max_read_bytes:
            raise ValueError(f"{self.label} read exceeds byte budget")
        if not size:
            return b""
        if self.file:
            self.file.seek(self.position)
            data = self.file.read(size)
        else:
            response = self.client.get_object(
                self.bucket,
                self.object,
                offset=self.position,
                length=size,
                request_headers={"If-Match": '"' + self.etag + '"'},
            )
            try:
                data = response.read(size + 1)
            finally:
                response.close()
                response.release_conn()
        if len(data) != size:
            raise ValueError(f"{self.label} source range is incomplete")
        self.position += len(data)
        self.read_bytes += len(data)
        return data

    def close(self):
        if self.file:
            self.file.close()
            self.file = None
        super().close()
