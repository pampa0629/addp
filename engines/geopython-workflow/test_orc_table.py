"""Real Apache ORC decoding: no mocks for schema, values, stripes or masks."""

import base64
import datetime as dt
from decimal import Decimal
import io
import json
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch
import pytest
import pyorc
from operators import orc_table as orc


class Timestamp(pyorc.converters.ORCConverter):
    @staticmethod
    def from_orc(s, n, t):
        return s, n

    @staticmethod
    def to_orc(v, t):
        return v


CONVERTERS = {pyorc.TypeKind.TIMESTAMP: Timestamp, pyorc.TypeKind.TIMESTAMP_INSTANT: Timestamp}


def plan(path):
    return {
        "schema_version": "addp.workflow.access-plan/v1",
        "source": {
            "kind": "file",
            "format": "orc",
            "access": {"method": "mounted_path", "path": str(path)},
        },
    }


def write(
    path, rows, schema="struct<id:bigint,text:string>", compression=pyorc.CompressionKind.ZLIB
):
    with path.open("wb") as f:
        with pyorc.Writer(
            f,
            schema,
            compression=compression,
            converters=CONVERTERS,
            batch_size=256,
            stripe_size=65536,
        ) as w:
            w.writerows(rows)
    return plan(path)


def describe(p):
    return orc.inspect(orc.PROTOCOL, p)["describe"]


def read(p, d, offset=0, limit=20, columns=None):
    return orc.read_batch(
        orc.PROTOCOL,
        p,
        d["format_info"]["source_version"],
        offset,
        limit,
        columns or [f["name"] for f in d["table"]["fields"]],
    )


@pytest.mark.parametrize(
    "compression",
    [
        pyorc.CompressionKind.NONE,
        pyorc.CompressionKind.ZLIB,
        pyorc.CompressionKind.SNAPPY,
        pyorc.CompressionKind.LZ4,
        pyorc.CompressionKind.ZSTD,
    ],
)
def test_exact_scalars(tmp_path, compression):
    schema = "struct<flag:boolean,tiny:tinyint,small:smallint,number:int,id:bigint,d:decimal(38,9),text:string,blob:binary,day:date,wall:timestamp,instant:timestamp with local time zone>"
    row = (
        True,
        -128,
        -32768,
        -(2**31),
        2**63 - 1,
        Decimal("12345678901234567890123456789.123456789"),
        "中文",
        b"\x00\xff",
        dt.date(1969, 12, 31),
        (0, 123456789),
        (0, 987654321),
    )
    p = write(
        tmp_path / "values.orc",
        [
            row,
            (None,) * 11,
            (
                False,
                127,
                32767,
                2**31 - 1,
                -(2**63),
                Decimal("0.000000000"),
                "",
                b"",
                dt.date(2026, 10, 27),
                (0, 0),
                (0, 1),
            ),
        ],
        schema,
        compression,
    )
    d = describe(p)
    assert d["table"]["row_count"] == 3
    fields = {f["name"]: f for f in d["table"]["fields"]}
    assert (
        fields["d"]["precision"] == 38
        and fields["d"]["scale"] == 9
        and all(f["nullable"] for f in fields.values())
    )
    rows = read(p, d)["rows"]
    assert rows[0]["id"] == str(2**63 - 1)
    assert rows[2]["id"] == str(-(2**63))
    assert rows[0]["d"] == str(row[5])
    assert rows[0]["wall"] == "1970-01-01T00:00:00.123456789"
    assert rows[0]["instant"] == "1970-01-01T00:00:00.987654321Z"
    assert rows[0]["blob"] == base64.b64encode(row[7]).decode()
    assert (
        rows[2]["blob"] == "" and rows[2]["text"] == "" and all(v is None for v in rows[1].values())
    )


@pytest.mark.parametrize("count", [0, 1, 10000])
def test_paging_projection_eof_and_source_unchanged(tmp_path, count):
    rows = [(i, str(i) * 100) for i in range(count)]
    p = write(tmp_path / "rows.orc", rows)
    before = (tmp_path / "rows.orc").read_bytes()
    d = describe(p)
    assert d["table"]["row_count"] == count and d["format_info"]["validation"] == "footer"
    actual = []
    for offset in range(0, count + 137, 137):
        actual += read(p, d, offset, 137, ["id"])["rows"]
    assert actual == [{"id": str(i)} for i in range(count)]
    assert read(p, d, count + 20)["rows"] == [] and (tmp_path / "rows.orc").read_bytes() == before


@pytest.mark.parametrize(
    "schema,rows",
    [
        ("struct<a:array<int>>", [([1],)]),
        ("struct<a:map<string,int>>", [({"x": 1},)]),
        ("int", [3]),
    ],
)
def test_profile_rejection(tmp_path, schema, rows):
    p = write(tmp_path / "unsupported.orc", rows, schema)
    with pytest.raises(ValueError):
        describe(p)


def test_change_and_truncation_rejected(tmp_path):
    path = tmp_path / "source.orc"
    p = write(path, [(1, "before")])
    d = describe(p)
    write(path, [(1, "after")])
    with pytest.raises(ValueError, match="version changed"):
        read(p, d)
    path.write_bytes(path.read_bytes()[:20])
    with pytest.raises(Exception):
        describe(p)


def test_budgets_unknown_columns_nan_and_protocol(tmp_path):
    p = write(tmp_path / "source.orc", [(1, "value")])
    d = describe(p)
    for offset, limit in [(-1, 1), (0, 0), (0, 2001), (True, 1)]:
        with pytest.raises(ValueError):
            read(p, d, offset, limit)
    with pytest.raises(ValueError):
        read(p, d, columns=["absent"])
    with pytest.raises(ValueError):
        orc.inspect("bad", p)
    with patch.object(orc, "MAX_FILE_BYTES", 4):
        with pytest.raises(ValueError):
            describe(p)
    with patch.object(orc, "MAX_RESPONSE_BYTES", 1):
        with pytest.raises(ValueError):
            read(p, d)
    q = write(tmp_path / "nan.orc", [(float("nan"),)], "struct<x:double>")
    n = describe(q)
    with pytest.raises(ValueError):
        read(q, n)


class ObjectClient:
    def __init__(self, data):
        self.data = data
        self.etag = "fixed"
        self.ranges = []

    def stat_object(self, *args):
        return SimpleNamespace(size=len(self.data), etag=self.etag, version_id=None)

    def get_object(self, bucket, name, offset, length, request_headers):
        assert (
            bucket == "sample"
            and name == "exact.orc"
            and request_headers == {"If-Match": '"fixed"'}
        )
        self.ranges.append((offset, length))
        stream = io.BytesIO(self.data[offset : offset + length])
        stream.release_conn = lambda: None
        return stream


def test_object_storage_conditional_ranges_and_version(tmp_path):
    path = tmp_path / "source.orc"
    write(path, [(i, str(i) * 100) for i in range(10000)], compression=pyorc.CompressionKind.NONE)
    client = ObjectClient(path.read_bytes())
    p = {
        "schema_version": "addp.workflow.access-plan/v1",
        "source": {
            "kind": "file",
            "format": "orc",
            "access": {
                "method": "object_store",
                "endpoint": "example.invalid:9000",
                "bucket": "sample",
                "object": "exact.orc",
                "access_key": "fake",
                "secret_key": "fake",
            },
        },
    }
    with patch.object(orc, "_object_store_client", return_value=client):
        d = describe(p)
        result = read(p, d, 5000, 3, ["id"])
        assert result["rows"] == [{"id": str(i)} for i in range(5000, 5003)]
        assert all(length < len(client.data) for _, length in client.ranges)
        client.etag = "changed"
        with pytest.raises(ValueError, match="version changed"):
            read(p, d)


def test_real_registered_native_process(tmp_path):
    p = write(tmp_path / "source.orc", [(2**63 - 1, "child")])
    registered = orc.OPERATORS["orc.inspect"]["function"]
    d = registered(protocol=orc.PROTOCOL, access_plan=p)["describe"]
    assert d["table"]["row_count"] == 1
    result = orc.OPERATORS["orc.read_batch"]["function"](
        protocol=orc.PROTOCOL,
        access_plan=p,
        source_version=d["format_info"]["source_version"],
        offset=0,
        limit=1,
        columns=["id"],
    )
    assert result["rows"] == [{"id": str(2**63 - 1)}]


def test_real_http_returns_typed_source_change(tmp_path):
    from api_server import app

    p = write(tmp_path / "source.orc", [(1, "before")])
    d = describe(p)
    write(tmp_path / "source.orc", [(2, "after")])
    response = app.test_client().post(
        "/api/operators/orc.read_batch/invoke",
        json={
            "params": {
                "protocol": orc.PROTOCOL,
                "access_plan": p,
                "source_version": d["format_info"]["source_version"],
                "offset": 0,
                "limit": 1,
                "columns": ["id"],
            }
        },
    )
    assert response.status_code == 409 and response.json["error_code"] == "ORC_SOURCE_CHANGED"
    assert "rows" not in response.json and str(tmp_path) not in response.get_data(as_text=True)


def test_hosted_physical_oracle_independently_checks_every_parquet_value(tmp_path, monkeypatch):
    import importlib.util
    import pyarrow as pa
    import pyarrow.parquet as parquet
    import minio

    script = Path(__file__).resolve().parents[2] / "business/scripts/online-orc-fixture.py"
    spec = importlib.util.spec_from_file_location("orc_physical_fixture", script)
    fixture = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(fixture)
    objects = {}

    class Client:
        def fput_object(self, bucket, name, path, **kwargs):
            objects[name] = Path(path).read_bytes()

        def put_object(self, bucket, name, data, length):
            objects[name] = data.read(length)

        def fget_object(self, bucket, name, path):
            Path(path).write_bytes(objects[name])

        def remove_object(self, bucket, name):
            objects.pop(name, None)

        def list_objects(self, bucket, prefix, **kwargs):
            return [SimpleNamespace(object_name=k) for k in sorted(objects) if k.startswith(prefix)]

    monkeypatch.setattr(minio, "Minio", lambda *a, **k: Client())
    request = {
        "action": "seed",
        "nfs_root": str(tmp_path),
        "storage": {
            "endpoint": "fake",
            "access_key": "fake",
            "secret_key": "fake",
            "bucket": "fake",
        },
    }
    seed = fixture.worker(request)
    schema = pa.schema(
        [
            ("id", pa.int64()),
            ("measure", pa.decimal128(38, 9)),
            ("flag", pa.bool_()),
            ("label", pa.string()),
            ("blob", pa.binary()),
        ]
    )
    values = [dict(zip(schema.names, fixture.row(i))) for i in range(fixture.COUNT)]
    # ADDP's existing parquet-go Group writer orders physical columns by name.
    table = pa.Table.from_pylist(values, schema=schema).select(sorted(schema.names))
    file = tmp_path / "output.parquet"
    parquet.write_table(table, file)
    for source in ("minio", "nfs"):
        objects["orc-output/from-" + source + ".parquet"] = file.read_bytes()
    proof = fixture.worker(dict(request, action="verify"))
    assert proof["source_sha256"] == seed["sha256"] and all(
        x["all_values_and_nulls_verified"] for x in proof["outputs"]
    )
    values[2000]["blob"] = b"wrong"
    parquet.write_table(pa.Table.from_pylist(values, schema=schema).select(sorted(schema.names)), file)
    objects["orc-output/from-nfs.parquet"] = file.read_bytes()
    with pytest.raises(AssertionError):
        fixture.worker(dict(request, action="verify"))
    assert fixture.worker(dict(request, action="delete"))["remaining_owned_outputs"] == 0


def test_literal_field_names_are_not_interpreted_as_nested_column_paths(tmp_path):
    p = write(
        tmp_path / "literal.orc", [(2**63 - 1, "literal")], "struct<`a.b`:bigint,`a space`:string>"
    )
    d = describe(p)
    result = read(p, d, columns=["a space", "a.b"])
    assert result["rows"] == [{"a space": "literal", "a.b": str(2**63 - 1)}]


def test_wall_clock_and_instant_have_different_timezone_semantics(tmp_path):
    from zoneinfo import ZoneInfo

    path = tmp_path / "timezone.orc"
    value = dt.datetime(2026, 7, 1, 12, 30, 5, 123456, tzinfo=ZoneInfo("America/New_York"))
    with path.open("wb") as stream:
        with pyorc.Writer(
            stream,
            "struct<wall:timestamp,instant:timestamp with local time zone>",
            timezone=ZoneInfo("America/New_York"),
        ) as writer:
            writer.write((value, value))
    p = plan(path)
    d = describe(p)
    assert read(p, d)["rows"] == [
        {"wall": "2026-07-01T12:30:05.123456000", "instant": "2026-07-01T16:30:05.123456000Z"}
    ]


def test_discovery_declares_readonly_object_batch_contract():
    from operators import list_operators

    descriptions = {d["name"]: d for d in list_operators()}
    for name in ["orc.inspect", "orc.read_batch"]:
        metadata = descriptions[name]
        assert metadata["execution_modes"] == ["direct"] and metadata["effects"] == ["read"]
        assert metadata["output_ports"][0]["type"] == "object"
    columns = next(
        p for p in descriptions["orc.read_batch"]["parameters"] if p["name"] == "columns"
    )
    assert columns["type"] == "array" and columns["item_type"] == "string"


def test_float_double_and_bounded_text_types(tmp_path):
    import math
    import struct

    single = struct.unpack("f", struct.pack("f", 0.1))[0]
    double = math.nextafter(1.0, 2.0)
    p = write(
        tmp_path / "floating.orc",
        [(single, double, "中文", "four"), (None, None, None, None)],
        "struct<f:float,d:double,v:varchar(12),c:char(4)>",
    )
    d = describe(p)
    assert [(f["type"], f["native_type"]) for f in d["table"]["fields"]] == [
        ("float", "float"),
        ("double", "double"),
        ("string", "varchar(12)"),
        ("string", "char(4)"),
    ]
    assert read(p, d)["rows"] == [
        {"f": single, "d": double, "v": "中文", "c": "four"},
        {"f": None, "d": None, "v": None, "c": None},
    ]


def test_null_root_struct_is_not_silently_flattened(tmp_path):
    p = write(tmp_path / "root-null.orc", [None], "struct<a:int>")
    d = describe(p)
    with pytest.raises(ValueError, match="NULL root"):
        read(p, d)
