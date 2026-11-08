"""Physical ORC/Parquet oracle inside the existing Hosted GeoPython product image."""

from __future__ import annotations
import base64
from decimal import Decimal
import hashlib
import json
import os
from pathlib import Path
import re
import sys
import tempfile

COUNT = 2503
SCHEMA = "struct<id:bigint,measure:decimal(38,9),flag:boolean,label:string,blob:binary>"
FIELD_NAMES = ["id", "measure", "flag", "label", "blob"]


def row(index):
    return (
        2**53 + index,
        Decimal(f"12345678901234567890.{index:09d}"),
        None if index % 7 == 0 else index % 2 == 0,
        f"ORC记录-{index:04d}",
        None if index % 11 == 0 else bytes([0, index % 256, 255]),
    )


NESTED_COUNT = 43
NESTED_SCHEMA = "struct<id:bigint,profile:struct<counter:bigint,amount:decimal(38,9),blob:binary>,samples:array<struct<value:bigint,label:string>>,matrix:array<array<bigint>>>"


def nested_row(index):
    number = 2**53 + index
    profile = (
        None
        if index % 3 == 0
        else (
            (None, None, None)
            if index % 5 == 0
            else (
                number,
                Decimal(f"12345678901234567890.{index:09d}"),
                b"" if index % 2 == 0 else b"\x00\xff",
            )
        )
    )
    samples = (
        None
        if index % 7 == 0
        else ([] if index % 5 == 0 else [(number, None), (None, "嵌套")])
    )
    matrix = (
        None
        if index % 7 == 0
        else ([] if index % 5 == 0 else [[number, None], [], None])
    )
    return number, profile, samples, matrix


def worker(request):
    import pyorc
    from minio import Minio

    access = request["storage"]
    client = Minio(
        access["endpoint"],
        access_key=access["access_key"],
        secret_key=access["secret_key"],
        secure=False,
    )
    bucket = access["bucket"]
    root = Path(request["nfs_root"])
    action = request["action"]
    if action == "seed":
        path = root / "orc" / "table.orc"
        path.parent.mkdir(exist_ok=True)
        with path.open("wb") as output:
            with pyorc.Writer(
                output,
                SCHEMA,
                compression=pyorc.CompressionKind.ZSTD,
                batch_size=256,
                stripe_size=65536,
            ) as writer:
                writer.writerows(row(i) for i in range(COUNT))
        path.chmod(0o644)
        client.fput_object(
            bucket, "orc/table.orc", str(path), content_type="application/orc"
        )
        client.put_object(bucket, "orc-output/.keep", __import__("io").BytesIO(b""), 0)
        nested_path = path.with_name("nested.orc")
        with nested_path.open("wb") as output:
            with pyorc.Writer(
                output, NESTED_SCHEMA, compression=pyorc.CompressionKind.ZSTD
            ) as writer:
                writer.writerows(nested_row(i) for i in range(NESTED_COUNT))
        nested_path.chmod(0o644)
        client.fput_object(
            bucket, "orc/nested.orc", str(nested_path), content_type="application/orc"
        )
        nested_bytes = nested_path.read_bytes()
        raw = path.read_bytes()
        return {
            "nested": {
                "count": NESTED_COUNT,
                "schema": NESTED_SCHEMA,
                "size_bytes": len(nested_bytes),
                "sha256": hashlib.sha256(nested_bytes).hexdigest(),
            },
            "count": COUNT,
            "schema": SCHEMA,
            "size_bytes": len(raw),
            "sha256": hashlib.sha256(raw).hexdigest(),
            "pyorc": pyorc.__version__,
            "orc": str(pyorc.orc_version),
        }
    names = ["orc-output/from-minio.parquet", "orc-output/from-nfs.parquet"]
    if action == "verify":
        import pyarrow.parquet as parquet

        reports = []
        with tempfile.TemporaryDirectory() as directory:
            for name in names:
                path = Path(directory) / Path(name).name
                client.fget_object(bucket, name, str(path))
                table = parquet.read_table(path)
                assert (
                    table.schema.field("measure").type.precision == 38
                ), "output_precision"
                assert table.schema.field("measure").type.scale == 9, "output_scale"
                assert (
                    table.column_names == sorted(FIELD_NAMES)
                    and table.num_rows == COUNT
                ), "output_columns_or_count"
                for i, actual in enumerate(table.to_pylist()):
                    assert tuple(actual[name] for name in FIELD_NAMES) == row(
                        i
                    ), "output_values"
                reports.append(
                    {
                        "object": name,
                        "rows": table.num_rows,
                        "sha256": hashlib.sha256(path.read_bytes()).hexdigest(),
                        "size_bytes": path.stat().st_size,
                        "all_values_and_nulls_verified": True,
                        "content_base64": base64.b64encode(path.read_bytes()).decode(
                            "ascii"
                        ),
                    }
                )
            source = Path(directory) / "source.orc"
            client.fget_object(bucket, "orc/table.orc", str(source))
            assert (
                source.read_bytes() == (root / "orc/table.orc").read_bytes()
            ), "source_bytes"
            nested = Path(directory) / "nested.orc"
            client.fget_object(bucket, "orc/nested.orc", str(nested))
            assert (
                nested.read_bytes() == (root / "orc/nested.orc").read_bytes()
            ), "nested_source_bytes"
            with nested.open("rb") as handle:
                actual = list(pyorc.Reader(handle))
            assert actual == [
                nested_row(i) for i in range(NESTED_COUNT)
            ], "nested_source_values"
            return {
                "nested_source_sha256": hashlib.sha256(nested.read_bytes()).hexdigest(),
                "nested_all_values_verified": True,
                "outputs": reports,
                "source_sha256": hashlib.sha256(source.read_bytes()).hexdigest(),
                "pyarrow": __import__("pyarrow").__version__,
            }
    if action == "delete":
        for name in names:
            client.remove_object(bucket, name)
        remaining = [
            o.object_name
            for o in client.list_objects(bucket, prefix="orc-output/", recursive=True)
        ]
        assert remaining == ["orc-output/.keep"]
        return {"output_objects_removed": True, "remaining_owned_outputs": 0}
    raise ValueError("unknown ORC fixture action")


def physical(action, repository, environment):
    import importlib
    return importlib.import_module('scripts.test.online-table-fixture-support').physical_worker(
        action, repository, environment, profile='orc', script='business/scripts/online-orc-fixture.py')


if __name__ == "__main__":
    if sys.argv[1] == "worker":
        try:
            print(json.dumps(worker(json.load(sys.stdin))))
        except Exception as error:
            code = (
                str(error)
                if isinstance(error, AssertionError)
                else type(error).__name__
            )
            if not re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]{0,80}", code):
                code = "worker_failed"
            print(json.dumps({"error_code": code}))
            raise SystemExit(1)
    else:
        print(
            json.dumps(
                physical(
                    sys.argv[1], Path(__file__).resolve().parents[2], dict(os.environ)
                )
            )
        )
