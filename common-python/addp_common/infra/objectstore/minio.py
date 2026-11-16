"""MinIO SDK ownership and response lifetime remain inside this provider."""
import io
from functools import wraps
import os
from minio import Minio
from minio.error import S3Error
from .area import ObjectStat


class _Reader(io.RawIOBase):
    def __init__(self, response):
        self._response = response

    def read(self, size=-1):
        return self._response.read(size)

    def close(self):
        if not self.closed:
            try:
                self._response.close()
            finally:
                self._response.release_conn()
                super().close()


def _storage_errors(method):
    @wraps(method)
    def call(self, key, *args, **kwargs):
        try:
            return method(self, key, *args, **kwargs)
        except S3Error as exc:
            if exc.code == "NoSuchKey":
                raise FileNotFoundError(key) from exc
            if exc.code == "AccessDenied":
                raise PermissionError(key) from exc
            raise OSError("object storage operation failed") from exc
    return call


class MinioArea:
    def __init__(self, client, bucket):
        self._client, self._bucket = client, bucket

    def check_ready(self):
        if not self._client.bucket_exists(self._bucket):
            raise FileNotFoundError("platform object bucket missing")

    @_storage_errors
    def put(self, key, body, size, media_type):
        self._client.put_object(self._bucket, key, body, size, content_type=media_type)

    @_storage_errors
    def stat(self, key):
        value = self._client.stat_object(self._bucket, key)
        return ObjectStat(value.size, value.content_type)

    @_storage_errors
    def open(self, key):
        return _Reader(self._client.get_object(self._bucket, key))

    @_storage_errors
    def delete(self, key):
        self._client.remove_object(self._bucket, key)


def bind(bucket):
    endpoint = os.environ.get("MINIO_ENDPOINT") or "localhost:" + os.environ.get("MINIO_API_PORT", "19000")
    client = Minio(endpoint, access_key=os.environ["MINIO_ROOT_USER"], secret_key=os.environ["MINIO_ROOT_PASSWORD"],
                   secure=os.environ.get("MINIO_USE_SSL", "false").lower() == "true")
    area = MinioArea(client, bucket)
    area.check_ready()
    return area
