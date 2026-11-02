import copy
from pathlib import Path

import pytest

from addp_common import workflow_access as access


def pair_plan(main, header):
    return {"schema_version": access.SCHEMA_VERSION, "source": {
        "kind": "file", "format": "envi", "access": main,
        "related_files": [{"role": "header", "access": header}],
    }}


def mounted_plan(tmp_path):
    main = tmp_path / " 原名 %.img "
    header = tmp_path / " 原名 %.hdr"
    main.write_bytes(b"\x01\x02\x03\x04")
    header.write_text("ENVI\n", encoding="utf-8")
    return pair_plan({"method": "mounted_path", "path": str(main)}, {"method": "mounted_path", "path": str(header)})


def test_envi_stages_only_two_exact_files_and_preserves_names(tmp_path):
    plan = mounted_plan(tmp_path)
    (tmp_path / "other.txt").write_bytes(b"unrelated")
    result = access.stage_source_file(access.require_source_plan({"access_plan": plan}), tmp_path / "work")
    assert result.name == " 原名 %.img "
    assert result.read_bytes() == b"\x01\x02\x03\x04"
    assert {file.name for file in result.parent.iterdir()} == {" 原名 %.img ", " 原名 %.hdr"}
    Path(plan["source"]["access"]["path"]).write_bytes(b"changed")
    assert result.read_bytes() == b"\x01\x02\x03\x04"


@pytest.mark.parametrize("failure", ["main missing", "main budget", "header budget", "symbolic link"])
def test_envi_staging_failure_removes_owned_snapshot_only(tmp_path, failure):
    plan = mounted_plan(tmp_path)
    work = tmp_path / "work"
    work.mkdir()
    unrelated = work / "unrelated.txt"
    unrelated.write_text("keep")
    main = Path(plan["source"]["access"]["path"])
    if failure == "main missing":
        main.unlink()
    elif failure == "header budget":
        Path(plan["source"]["related_files"][0]["access"]["path"]).write_bytes(b"x" * ((1 << 20) + 1))
    elif failure == "symbolic link":
        main.unlink()
        main.symlink_to(unrelated)
    with pytest.raises(access.WorkflowAccessError):
        access.stage_source_file(plan, work, max_bytes=3 if failure == "main budget" else 32)
    assert not (work / "source").exists()
    assert unrelated.read_text() == "keep"


def test_envi_never_reclaims_an_existing_snapshot(tmp_path):
    plan = mounted_plan(tmp_path)
    existing = tmp_path / "work" / "source"
    existing.mkdir(parents=True)
    (existing / "keep").write_text("unchanged")
    with pytest.raises(FileExistsError):
        access.stage_source_file(plan, existing.parent)
    assert (existing / "keep").read_text() == "unchanged"


@pytest.mark.parametrize("mutation", ["single", "wrong format", "directory", "wrong role", "different parent", "different connection", "extra", "override"])
def test_envi_rejects_unconfirmed_sources_before_staging(tmp_path, mutation):
    plan = mounted_plan(tmp_path)
    source = plan["source"]
    if mutation == "single":
        del source["related_files"]
    elif mutation == "wrong format":
        source["format"] = "img"
    elif mutation == "directory":
        source["kind"] = "directory"
    elif mutation == "wrong role":
        source["related_files"][0]["role"] = "texture"
    elif mutation == "different parent":
        source["related_files"][0]["access"]["path"] = str(tmp_path / "different" / "file.hdr")
    elif mutation == "different connection":
        source["related_files"][0]["access"]["server"] = "different"
    elif mutation == "extra":
        source["related_files"].append(copy.deepcopy(source["related_files"][0]))
    else:
        source["entrypoint"] = "other.img"
    with pytest.raises(access.WorkflowAccessError):
        access.stage_source_file(plan, tmp_path / "work")
    assert not (tmp_path / "work").exists()


def test_envi_object_source_reads_exact_keys_without_listing(tmp_path, monkeypatch):
    import io
    from types import SimpleNamespace
    source = {"method": "object_store", "endpoint": "minio:9000", "access_key": "key", "secret_key": "secret", "bucket": "business", "object": "imagery/field.img"}
    header = {**source, "object": "imagery/field.HDR"}
    calls = []
    contents = {source["object"]: b"\x01\x02\x03\x04", header["object"]: b"ENVI\n"}

    class Response(io.BytesIO):
        def release_conn(self):
            pass

    class Client:
        def stat_object(self, bucket, key):
            assert bucket == "business"
            return SimpleNamespace(size=len(contents[key]))

        def get_object(self, bucket, key):
            calls.append((bucket, key))
            return Response(contents[key])

    monkeypatch.setattr(access, "_object_store_client", lambda _: Client())
    result = access.stage_source_file(pair_plan(source, header), tmp_path / "work")
    assert result.read_bytes() == contents[source["object"]]
    assert calls == [("business", header["object"]), ("business", source["object"])]
    assert {file.name for file in result.parent.iterdir()} == {"field.img", "field.HDR"}
