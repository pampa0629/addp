#!/usr/bin/env python3
"""Capture login, Catalog and business PostgreSQL recovery inputs from a running K3s site."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import time
import tarfile


def table_query(table):
    if not re.fullmatch(r"[a-z_][a-z0-9_]*\.[a-z_][a-z0-9_]*", table):
        raise ValueError("use a schema-qualified verification table")
    return ("SET TIME ZONE 'UTC'; SELECT json_build_object('rows',count(*),'md5',"
            f"md5(coalesce(string_agg(md5(to_jsonb(r)::text),'' ORDER BY md5(to_jsonb(r)::text)),''))) "
            f"FROM {table} r")


DATABASE_FORMAT = """SELECT json_build_object('version',current_setting('server_version_num')::int,
    'extensions',(SELECT json_object_agg(extname,extversion) FROM pg_extension))"""


def capture_db(k, deploy, name, table, root):
    base = k + ["exec", "-i", "deploy/" + deploy, "--", "sh", "-ec"]
    command = 'exec psql -qXAt -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$1"'
    with subprocess.Popen(base + [command, "psql", name], stdin=subprocess.PIPE,
                          stdout=subprocess.PIPE, text=True) as tx:
        try:
            tx.stdin.write("BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;\n"
                           "SELECT json_build_object('snapshot',pg_export_snapshot(),"
                           "'lower',transaction_timestamp(),'observed',clock_timestamp());\n"
                           + table_query(table) + ";\n" + DATABASE_FORMAT + ";\n")
            tx.stdin.flush()
            info = json.loads(tx.stdout.readline())
            info.update(database=name, table=table, witness=json.loads(tx.stdout.readline()))
            info["format"] = json.loads(tx.stdout.readline())
            for suffix, command, args in [
                ("roles.sql", 'exec pg_dumpall -U "$POSTGRES_USER" -l "$1" --roles-only', [name]),
                ("dump", 'exec pg_dump -U "$POSTGRES_USER" -Fc -C --snapshot="$2" -d "$1"',
                 [name, info["snapshot"]]),
            ]:
                with (root / (deploy + "." + suffix)).open("wb") as out:
                    subprocess.run(base + [command, "dump", *args], stdout=out,
                                   stdin=subprocess.DEVNULL, check=True)
        finally:
            tx.stdin.close()
        if tx.wait() != 0:
            raise RuntimeError("source snapshot transaction failed")
    return info


def object_witness(archive):
    result = {}
    with tarfile.open(archive) as tar:
        for member in tar:
            if (not (member.isfile() or member.isdir()) or member.name.startswith("/")
                    or ".." in Path(member.name).parts):
                raise ValueError("unexpected object in snapshot")
            if member.isdir():
                continue
            if member.name in result:
                raise ValueError("duplicate snapshot member")
            with tar.extractfile(member) as stream:
                result[member.name] = hashlib.file_digest(stream, "sha256").hexdigest()
    return result


# The client config and downloads live only in a new temporary Pod/container directory.
MC_SETUP = """set -eu
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
mc() { /usr/local/bin/mc --config-dir "$tmp/config" "$@"; }
mc alias set dr http://localhost:9000 "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD" >/dev/null
mkdir "$tmp/objects"
"""
MC_EXPORT = MC_SETUP + """
for bucket do
    mkdir "$tmp/objects/$bucket"
    mc mirror "dr/$bucket" "$tmp/objects/$bucket" >&2
done
tar -C "$tmp/objects" -cf - "$@"
"""


def capture(args):
    os.umask(0o077)
    root = args.out.resolve()
    root.mkdir(mode=0o700)
    k = ["kubectl", "-n", args.namespace]
    evidence = {"namespace": args.namespace, "databases": {}}
    for dep, db, table in [("postgres", "addp", "catalog.entries"),
                           ("business-postgres", args.business_db, args.business_table)]:
        evidence["databases"][dep] = capture_db(k, dep, db, table, root)

    services = ["system-backend", "catalog-backend", "gateway", "meilisearch-recovered"]
    for service in services:
        item = json.loads(subprocess.check_output(k + ["get", "deploy", service, "-o", "json"]))
        container = item["spec"]["template"]["spec"]["containers"][0]
        # Collect only keys explicitly configured for this service, with actual Pod overrides.
        keys = set()
        for src in container.get("envFrom", []):
            if "secretRef" in src:
                sec = json.loads(subprocess.check_output(k + ["get", "secret", src["secretRef"]["name"], "-o", "json"]))
                keys.update(sec["data"])
        keys.update(x["name"] for x in container.get("env", []))
        raw = subprocess.check_output(k + ["exec", "deploy/" + service, "-c", container["name"], "--", "env", "-0"])
        env = dict(pair.split("=", 1) for pair in raw.decode().split("\0") if pair)
        (root / (service + ".json")).write_text(json.dumps({n: env[n] for n in keys if n in env}))
    if not re.fullmatch(r"[A-Za-z0-9_-]+\.dump", args.search_dump):
        raise ValueError("specify an existing dump filename")
    search = k + ["exec", "deploy/meilisearch-recovered", "--"]
    with (root / "search.dump").open("wb") as f:
        subprocess.run(search + ["cat", "/meili_data/dumps/" + args.search_dump], stdout=f, check=True)
    def search_get(path):
        return json.loads(subprocess.check_output(search + ["sh", "-ec",
            'exec curl -fsS -H "Authorization: Bearer $MEILI_MASTER_KEY" "http://localhost:7700$1"', "get", path]))
    receipt = search_get(f"/tasks/{args.search_task}")
    if (receipt["type"] != "dumpCreation" or receipt["status"] != "succeeded"
            or receipt["details"]["dumpUid"] + ".dump" != args.search_dump):
        raise ValueError("search dump does not match source task")
    evidence["search_dump"] = {"file": args.search_dump, "task": args.search_task,
                               "finished_at": receipt["finishedAt"]}
    evidence["search"] = search_witness(search_get)
    if not args.bucket or len(set(args.bucket)) != len(args.bucket) or any(
            not re.fullmatch(r"[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]", b) for b in args.bucket):
        raise ValueError("specify distinct business bucket names")
    objects = root / "objects.tar"
    beginning = time.time()
    with objects.open("wb") as out:
        subprocess.run(k + ["exec", "deploy/minio", "--", "sh", "-ec", MC_EXPORT, "export", *args.bucket],
                       stdout=out, check=True)
    evidence["minio"] = {"buckets": args.bucket, "objects": object_witness(objects),
                         "started": beginning, "ended": time.time()}
    evidence["captured_at"] = time.time()
    (root / "snapshot.json").write_text(json.dumps(evidence, indent=2))
    sums = []
    for path in sorted(root.iterdir()):
        with path.open("rb") as f:
            sums.append(hashlib.file_digest(f, "sha256").hexdigest() + "  " + path.name + "\n")
    (root / "SHA256SUMS").write_text("".join(sums))
    print(root)


def search_witness(get):
    result = {}
    offset = 0
    while True:
        page = get(f"/indexes?limit=100&offset={offset}")["results"]
        for index in page:
            uid = index["uid"]
            if not re.fullmatch(r"[A-Za-z0-9_-]+", uid):
                raise ValueError("invalid index uid")
            docs = []
            while True:
                batch = get(f"/indexes/{uid}/documents?limit=500&offset={len(docs)}")["results"]
                docs.extend(json.dumps(x, sort_keys=True, separators=(",", ":")) for x in batch)
                if len(batch) < 500:
                    break
            def digest(obj):
                return hashlib.sha256(json.dumps(obj, sort_keys=True).encode()).hexdigest()
            result[uid] = dict(count=len(docs), documents=digest(sorted(docs)),
                               primary_key=index["primaryKey"], settings=digest(get(f"/indexes/{uid}/settings")))
        if len(page) < 100:
            return result
        offset += len(page)


if __name__ == "__main__":
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--namespace", required=True)
    p.add_argument("--out", required=True, type=Path)
    p.add_argument("--business-db", required=True)
    p.add_argument("--business-table", required=True)
    p.add_argument("--search-dump", required=True, help="existing verified export in /meili_data/dumps")
    p.add_argument("--search-task", required=True, type=int)
    p.add_argument("--bucket", action="append", required=True, help="business bucket to export; repeat for each")
    capture(p.parse_args())
