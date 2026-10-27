"""Physical ORC/Parquet oracle inside the existing Hosted GeoPython product image."""

from __future__ import annotations
import base64
from decimal import Decimal
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile

COUNT = 2503
SCHEMA = "struct<id:bigint,measure:decimal(38,9),flag:boolean,label:string,blob:binary>"


def row(index):
    return (
        2**53 + index,
        Decimal(f"12345678901234567890.{index:09d}"),
        None if index % 7 == 0 else index % 2 == 0,
        f"ORC记录-{index:04d}",
        None if index % 11 == 0 else bytes([0, index % 256, 255]),
    )


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
        client.fput_object(bucket, "orc/table.orc", str(path), content_type="application/orc")
        client.put_object(bucket, "orc-output/.keep", __import__("io").BytesIO(b""), 0)
        raw = path.read_bytes()
        return {
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
                assert table.schema.field("measure").type.precision == 38
                assert table.schema.field("measure").type.scale == 9
                assert (
                    table.column_names == ["id", "measure", "flag", "label", "blob"]
                    and table.num_rows == COUNT
                )
                for i, actual in enumerate(table.to_pylist()):
                    assert tuple(actual.values()) == row(i)
                reports.append(
                    {
                        "object": name,
                        "rows": table.num_rows,
                        "sha256": hashlib.sha256(path.read_bytes()).hexdigest(),
                        "size_bytes": path.stat().st_size,
                        "all_values_and_nulls_verified": True,
                        "content_base64": base64.b64encode(path.read_bytes()).decode("ascii"),
                    }
                )
            source = Path(directory) / "source.orc"
            client.fget_object(bucket, "orc/table.orc", str(source))
            assert source.read_bytes() == (root / "orc/table.orc").read_bytes()
            return {
                "outputs": reports,
                "source_sha256": hashlib.sha256(source.read_bytes()).hexdigest(),
                "pyarrow": __import__("pyarrow").__version__,
            }
    if action == "delete":
        for name in names:
            client.remove_object(bucket, name)
        remaining = [
            o.object_name for o in client.list_objects(bucket, prefix="orc-output/", recursive=True)
        ]
        assert remaining == ["orc-output/.keep"]
        return {"output_objects_removed": True, "remaining_owned_outputs": 0}
    raise ValueError("unknown ORC fixture action")


def physical(action, repository, environment):
    if action not in {"seed", "verify", "delete"}:
        raise ValueError("unknown ORC fixture action")
    if any(
        environment.get(k) != v
        for k, v in {
            "ADDP_ONLINE_HOSTED": "1",
            "GITHUB_ACTIONS": "true",
            "RUNNER_OS": "Linux",
            "ADDP_ONLINE_MANAGER_CASE": "native-table-orc",
        }.items()
    ):
        raise ValueError("ORC physical verifier requires its exclusive Hosted case")
    secret = Path(environment["ADDP_ONLINE_SECRET_DIR"])
    state = secret / "nfs-root"
    if state.is_symlink():
        raise ValueError("invalid ORC fixture state")
    root = Path(state.read_text().strip())
    if (
        not re.fullmatch(r"/tmp/addp-online-manager-nfs\.[A-Za-z0-9]{6}", str(root))
        or root.is_symlink()
        or (root / ".addp-online-owner").read_text().strip()
        != environment["ADDP_ONLINE_TEST_RUN_ID"]
    ):
        raise ValueError("ORC fixture ownership mismatch")
    config = {
        "action": action,
        "nfs_root": str(root),
        "storage": {
            "endpoint": "127.0.0.1:" + environment["ADDP_ONLINE_MANAGER_MINIO_PORT"],
            "access_key": environment["ADDP_ONLINE_MANAGER_MINIO_ACCESS_KEY"],
            "secret_key": environment["ADDP_ONLINE_MANAGER_MINIO_SECRET_KEY"],
            "bucket": environment["ADDP_ONLINE_MANAGER_MINIO_BUCKET"],
        },
    }
    script = Path(repository) / "business/scripts/online-orc-fixture.py"
    result = subprocess.run(
        [
            "docker",
            "run",
            "--rm",
            "-i",
            "--user",
            f"{os.getuid()}:{os.getgid()}",
            "--name",
            "addp-manager-raster-verifier",
            "--network",
            "host",
            "--label",
            "com.addp.online-fixture=manager-internal-artifact-lineage",
            "--mount",
            f"type=bind,src={script},dst=/fixture.py,readonly",
            "--mount",
            f"type=bind,src={root},dst={root}",
            environment["ADDP_ONLINE_RASTER_RUNTIME_IMAGE"],
            "python",
            "/fixture.py",
            "worker",
        ],
        input=json.dumps(config),
        text=True,
        capture_output=True,
        timeout=120,
    )
    if result.returncode:
        raise ValueError(f"ORC physical {action} failed (exit {result.returncode})")
    return json.loads(result.stdout)


if __name__ == "__main__":
    if sys.argv[1] == "worker":
        print(json.dumps(worker(json.load(sys.stdin))))
    else:
        print(
            json.dumps(physical(sys.argv[1], Path(__file__).resolve().parents[2], dict(os.environ)))
        )
