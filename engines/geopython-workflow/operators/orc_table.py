"""Single-file ORC read operators. Native decoding lives only in this Runtime."""

from __future__ import annotations
import base64
import datetime as dt
import hashlib
import io
import json
import math
import os
from pathlib import Path
import stat
import sys
from functools import wraps
from addp_common.workflow_access import (
    require_source_plan,
    _object_store_client,
    _source_storage_name,
)
from . import _native_process
from .base import (
    OperatorCategory,
    OperatorMetadata,
    OperatorParam,
    OperatorType,
    OutputPort,
    register_operator,
)

PROTOCOL = "orc.table-batch/v1"
MAX_FILE_BYTES = 256 * 1024 * 1024
MAX_READ_BYTES = 128 * 1024 * 1024
MAX_SINGLE_READ = 4 * 1024 * 1024
MAX_COLUMNS = 256
MAX_BATCH_ROWS = 2000
MAX_VALUE_BYTES = 1024 * 1024
MAX_RESPONSE_BYTES = 16 * 1024 * 1024


class SourceChanged(ValueError):
    pass


class SourceFile(io.RawIOBase):
    """Seek on a bound descriptor or conditional object ranges; never stage all bytes."""

    def __init__(self, plan, expected=""):
        self.position = self.read_bytes = 0
        self.file = self.client = None
        access = plan["source"]["access"]
        try:
            if access["method"] == "mounted_path":
                self.file = os.fdopen(os.open(access["path"], os.O_RDONLY | os.O_NOFOLLOW), "rb")
                self.size = os.fstat(self.file.fileno()).st_size
            else:
                self.client = _object_store_client(access)
                self.bucket = _source_storage_name(access, "bucket")
                self.object = _source_storage_name(access, "object")
                fact = self.client.stat_object(self.bucket, self.object)
                self.size, self.etag = fact.size, fact.etag
                if not self.etag:
                    raise ValueError("ORC source has no stable object version")
            if not 4 <= self.size <= MAX_FILE_BYTES:
                raise ValueError("ORC encoded file size exceeds budget")
            self.version = self.current_version()
            if expected and self.version != expected:
                raise SourceChanged("ORC source version changed; refresh metadata")
        except Exception:
            self.close()
            raise

    def current_version(self):
        if self.file:
            f = os.fstat(self.file.fileno())
            if not stat.S_ISREG(f.st_mode):
                raise ValueError("ORC source must be a regular file")
            identity = [f.st_dev, f.st_ino, f.st_size, f.st_mtime_ns, f.st_ctime_ns]
        else:
            fact = self.client.stat_object(self.bucket, self.object)
            identity = [self.bucket, self.object, fact.size, fact.etag, fact.version_id]
        return hashlib.sha256(json.dumps(identity, separators=(",", ":")).encode()).hexdigest()

    def check(self):
        if self.current_version() != self.version:
            raise SourceChanged("ORC source version changed; refresh metadata")

    def readable(self):
        return True

    def seekable(self):
        return True

    def tell(self):
        return self.position

    def seek(self, offset, whence=0):
        if whence not in (0, 1, 2):
            raise ValueError("invalid ORC seek origin")
        position = offset + (0 if whence == 0 else self.position if whence == 1 else self.size)
        if not 0 <= position <= self.size:
            raise ValueError("ORC seek outside source")
        self.position = position
        return position

    def read(self, size=-1):
        if size is None or size < 0:
            size = self.size - self.position
        size = min(size, self.size - self.position)
        if size > MAX_SINGLE_READ or self.read_bytes + size > MAX_READ_BYTES:
            raise ValueError("ORC read exceeds byte budget")
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
            raise ValueError("ORC source range is incomplete")
        self.position += len(data)
        self.read_bytes += len(data)
        return data

    def close(self):
        if self.file:
            self.file.close()
            self.file = None
        super().close()


def _open(access_plan, source_version):
    plan = require_source_plan({"access_plan": access_plan})
    if plan["source"].get("kind") != "file" or plan["source"].get("format") != "orc":
        raise ValueError("ORC requires one file source")
    if not isinstance(source_version, str):
        raise ValueError("ORC source version must be text")
    return SourceFile(plan, source_version)


def _converters(pyorc):
    class ExactTimestamp(pyorc.converters.ORCConverter):
        @staticmethod
        def from_orc(seconds, nanoseconds, timezone):
            return seconds, nanoseconds

    return {
        pyorc.TypeKind.TIMESTAMP: ExactTimestamp,
        pyorc.TypeKind.TIMESTAMP_INSTANT: ExactTimestamp,
    }


def _reader(source, columns=None):
    import pyorc

    if pyorc.__version__ != "0.12.0":
        raise ValueError("ORC runtime dependency version differs from the fixed version")
    if source.read(3) != b"ORC":
        raise ValueError("invalid ORC file signature")
    source.seek(0)
    reader = pyorc.Reader(source, batch_size=64, converters=_converters(pyorc))
    if (
        reader.schema.kind != pyorc.TypeKind.STRUCT
        or not 1 <= len(reader.schema.fields) <= MAX_COLUMNS
    ):
        raise ValueError("ORC requires a bounded nonempty flat struct")
    if reader.compression.name not in {"NONE", "ZLIB", "SNAPPY", "LZ4", "ZSTD"}:
        raise ValueError("ORC compression is not supported")
    if reader.num_of_stripes > 4096 or len(reader) > 2**53 - 1:
        raise ValueError("ORC footer exceeds budget")
    if any("acid" in k.lower() for k in reader.user_metadata):
        raise ValueError("ORC transactional table files are not supported")
    fields = []
    kinds = {
        "BOOLEAN": "bool",
        "BYTE": "int",
        "SHORT": "int",
        "INT": "int",
        "LONG": "bigint",
        "FLOAT": "float",
        "DOUBLE": "double",
        "STRING": "string",
        "VARCHAR": "string",
        "CHAR": "string",
        "BINARY": "bytes",
        "DECIMAL": "decimal",
        "DATE": "date",
        "TIMESTAMP": "timestamp",
        "TIMESTAMP_INSTANT": "timestamp",
    }
    for index, (name, typ) in enumerate(reader.schema.fields.items()):
        if typ.kind.name not in kinds or not name or len(name.encode()) > 1024:
            raise ValueError("ORC field type or name is not supported")
        field = {
            "name": name,
            "type": kinds[typ.kind.name],
            "native_type": str(typ),
            "nullable": True,
            "ordinal_position": index + 1,
        }
        if typ.kind.name == "DECIMAL":
            field.update(precision=typ.precision, scale=typ.scale)
        fields.append(field)
    if columns is not None:
        if (
            not isinstance(columns, list)
            or not columns
            or any(not isinstance(c, str) for c in columns)
            or len(set(columns)) != len(columns)
        ):
            raise ValueError("invalid ORC column selection")
        by_name = {f["name"]: f for f in fields}
        if any(c not in by_name for c in columns):
            raise ValueError("ORC selected field does not exist")
        fields = [by_name[c] for c in columns]
        source.seek(0)
        reader = pyorc.Reader(
            source,
            batch_size=64,
            column_indices=[list(by_name).index(name) for name in columns],
            struct_repr=pyorc.StructRepr.DICT,
            converters=_converters(pyorc),
        )
    return reader, fields


def inspect(protocol, access_plan, source_version=""):
    _protocol(protocol)
    with _open(access_plan, source_version) as source:
        reader, fields = _reader(source)
        result = {
            "protocol": PROTOCOL,
            "describe": {
                "table": {"fields": fields, "row_count": len(reader), "size_bytes": source.size},
                "format_info": {
                    "schema": str(reader.schema),
                    "compression": reader.compression.name,
                    "stripes": reader.num_of_stripes,
                    "source_version": source.version,
                    "validation": "footer",
                },
            },
        }
        source.check()
        return result


def _value(value, field):
    if value is None:
        return None
    kind = field["type"]
    if kind == "bigint":
        return str(value)
    if kind == "decimal":
        return format(value, "f")
    if kind == "bytes":
        if len(value) > MAX_VALUE_BYTES:
            raise ValueError("ORC value exceeds byte budget")
        return base64.b64encode(value).decode("ascii")
    if kind == "timestamp":
        seconds, nanos = value
        if not 0 <= nanos < 10**9:
            raise ValueError("invalid ORC timestamp nanoseconds")
        # UTC reader preserves wall clock values independently of host timezone;
        # only TIMESTAMP_INSTANT is tagged as a fixed instant on the wire.
        text = (dt.datetime(1970, 1, 1) + dt.timedelta(seconds=seconds)).isoformat(
            timespec="seconds"
        ) + f".{nanos:09d}"
        return text + ("Z" if field["native_type"] == "timestamp with local time zone" else "")
    if kind == "date":
        return value.isoformat()
    if isinstance(value, str) and len(value.encode()) > MAX_VALUE_BYTES:
        raise ValueError("ORC value exceeds byte budget")
    if isinstance(value, float) and not math.isfinite(value):
        raise ValueError("ORC non-finite float cannot be represented")
    return value


def read_batch(protocol, access_plan, source_version, offset, limit, columns):
    _protocol(protocol)
    if not source_version:
        raise ValueError("ORC read requires a bound source version")
    if (
        type(offset) is not int
        or offset < 0
        or type(limit) is not int
        or not 1 <= limit <= MAX_BATCH_ROWS
    ):
        raise ValueError("invalid ORC window")
    with _open(access_plan, source_version) as source:
        reader, fields = _reader(source, columns)
        reader.seek(min(offset, len(reader)))
        rows = []
        encoded_bytes = 0
        for raw in reader.read(limit):
            if raw is None:
                raise ValueError("ORC NULL root structs are not flat table rows")
            row = {field["name"]: _value(raw[field["name"]], field) for field in fields}
            encoded_bytes += len(json.dumps(row, ensure_ascii=False, allow_nan=False).encode())
            if encoded_bytes > MAX_RESPONSE_BYTES:
                raise ValueError("ORC response exceeds byte budget")
            rows.append(row)
        if len(rows) != min(limit, max(0, len(reader) - offset)):
            raise ValueError("ORC rows differ from declared footer")
        source.check()
        return {"protocol": PROTOCOL, "source_version": source.version, "rows": rows}


def _protocol(protocol):
    if protocol != PROTOCOL:
        raise ValueError("unsupported ORC batch protocol")


def _native_main():
    import resource

    resource.setrlimit(resource.RLIMIT_CPU, (30, 30))
    if sys.platform.startswith("linux"):
        resource.setrlimit(resource.RLIMIT_AS, (4 * 1024**3, 4 * 1024**3))
    request = json.load(sys.stdin)
    try:
        functions = {"inspect": inspect, "read_batch": read_batch}
        result = functions[request["operation"]](**request["params"])
        response = {"result": result}
    except SourceChanged:
        response = {"error": "orc_source_changed"}
    except Exception:
        # Storage credentials, object names and native exception details stay private.
        response = {"error": "orc_read_rejected"}
    print(json.dumps(response, ensure_ascii=False, allow_nan=False))


def _register(name, function, params):
    @wraps(function)
    def invoke(**values):
        completed = _native_process.run(
            [sys.executable, "-c", "from operators.orc_table import _native_main; _native_main()"],
            input=json.dumps({"operation": function.__name__, "params": values}, allow_nan=False),
            capture_output=True,
            text=True,
            timeout=60,
        )
        if completed.returncode != 0:
            raise ValueError("orc_read_rejected")
        response = json.loads(completed.stdout)
        if response.get("error") == "orc_source_changed":
            raise SourceChanged("ORC source changed")
        if "error" in response:
            raise ValueError(response["error"])
        return response["result"]

    metadata = OperatorMetadata(
        name=name,
        type=OperatorType.GENERAL,
        category=OperatorCategory.COMMON_IO,
        description=name,
        brief_description="ORC single-file table read",
        execution_modes=["direct"],
        effects=["read"],
        overview="受控单文件 ORC 表格读取",
        params=params,
        output_ports=[
            OutputPort(name="default", type="object", description="ORC table batch protocol")
        ],
        use_cases=["元数据", "表格分页", "连续批次"],
        notes=["只读；不提供目录合并或写出"],
        workflow_example={},
        attributes={"protocol": PROTOCOL, "runtime_session": "stateless"},
    )
    return register_operator(metadata, invoke)


def _param(name, kind, required=True, default=None):
    return OperatorParam(
        name=name,
        type="param",
        data_type=kind,
        description=name,
        required=required,
        default=default,
    )


_BASE = [_param("protocol", "string"), _param("access_plan", "object")]
OPERATORS = dict(
    [
        _register("orc.inspect", inspect, _BASE + [_param("source_version", "string", False, "")]),
        _register(
            "orc.read_batch",
            read_batch,
            _BASE
            + [
                _param("source_version", "string"),
                _param("offset", "int"),
                _param("limit", "int"),
                _param("columns", "list[str]"),
            ],
        ),
    ]
)
