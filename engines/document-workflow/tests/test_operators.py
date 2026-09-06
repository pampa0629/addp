import sys
import zipfile
from pathlib import Path

import pytest
from pypdf import PdfWriter


for parent in Path(__file__).resolve().parents:
    contract_path = parent / "docs" / "workflow_operator_contract.py"
    if contract_path.exists():
        sys.path.insert(0, str(contract_path.parent))
        break

import operators
from operators import CommandResult, ConverterError, converter_status, invoke_operator, list_operators
from workflow_operator_contract import assert_operator_metadata_contract


def mounted_plan(source: Path, target: Path, source_format: str = "pptx"):
    return {
        "schema_version": "addp.workflow.access-plan/v1",
        "source": {
            "kind": "file",
            "format": source_format,
            "access": {"method": "mounted_path", "path": str(source)},
            "metadata": {"source_size_bytes": source.stat().st_size},
        },
        "target": {
            "kind": "file",
            "format": "pdf",
            "name": target.name,
            "write_mode": "create",
            "content_type": "application/pdf",
            "access": {"method": "mounted_path", "path": str(target)},
        },
    }


def make_pptx(path: Path, *, include_media: bool = True) -> None:
    with zipfile.ZipFile(path, "w") as archive:
        archive.writestr("[Content_Types].xml", "<Types/>")
        archive.writestr("ppt/presentation.xml", "<presentation/>")
        archive.writestr("ppt/media/image1.png", b"png")
        if include_media:
            archive.writestr("ppt/media/video1.mp4", b"video" * 10)


def make_converter(path: Path) -> None:
    path.write_text("#!/bin/sh\nexit 0\n", encoding="utf-8")
    path.chmod(0o755)


def test_operator_metadata_contract_and_modes():
    ops = list_operators()
    assert [operator["name"] for operator in ops] == ["document_to_pdf"]
    assert_operator_metadata_contract(ops, expected_engine_type="document_workflow")
    assert ops[0]["execution_modes"] == ["workflow", "direct"]
    assert ops[0]["effects"] == ["read", "write"]


def test_converter_status_defaults_to_engine_bound_binary():
    status = converter_status(env={})
    assert status["binding"] == "document_workflow"
    assert status["env"] == "DOCUMENT_LIBREOFFICE_BIN"
    assert status["path"].endswith("engines/document-workflow/bin/soffice")


def test_document_conversion_strips_embedded_video_and_publishes_pdf(tmp_path):
    source = tmp_path / "slides.pptx"
    target = tmp_path / "output" / "slides.pdf"
    executable = tmp_path / "soffice"
    make_pptx(source)
    make_converter(executable)
    captured = {}

    def fake_runner(command, timeout_seconds):
        captured["command"] = command
        captured["timeout"] = timeout_seconds
        source_path = Path(command[-1])
        captured["source"] = source_path
        with zipfile.ZipFile(source_path) as archive:
            captured["names"] = archive.namelist()
        output_dir = Path(command[command.index("--outdir") + 1])
        writer = PdfWriter()
        writer.add_blank_page(width=100, height=100)
        writer.add_blank_page(width=100, height=100)
        with (output_dir / f"{source_path.stem}.pdf").open("wb") as stream:
            writer.write(stream)
        return CommandResult(returncode=0, stdout="convert slides.pptx as pdf")

    result = invoke_operator(
        "document_to_pdf",
        {"access_plan": mounted_plan(source, target)},
        runner=fake_runner,
        env={
            "DOCUMENT_LIBREOFFICE_BIN": str(executable),
            "DOCUMENT_CONVERSION_CONCURRENCY": "2",
        },
        timeout_seconds=30,
    )

    assert target.read_bytes().startswith(b"%PDF-")
    assert result["pdf_uri"] == str(target)
    assert result["page_count"] == 2
    assert result["media_preprocessing"] == {"removed_files": 1, "removed_bytes": 50}
    assert "ppt/media/video1.mp4" not in captured["names"]
    assert "ppt/media/image1.png" in captured["names"]
    assert captured["timeout"] == 30


def test_media_preprocessing_can_be_disabled(tmp_path):
    source = tmp_path / "slides.pptx"
    target = tmp_path / "slides.pdf"
    executable = tmp_path / "soffice"
    make_pptx(source)
    make_converter(executable)
    captured = {}

    def fake_runner(command, timeout_seconds):
        captured["source"] = Path(command[-1])
        output_dir = Path(command[command.index("--outdir") + 1])
        writer = PdfWriter()
        writer.add_blank_page(width=100, height=100)
        with (output_dir / "slides.pdf").open("wb") as stream:
            writer.write(stream)
        return CommandResult(returncode=0)

    result = invoke_operator(
        "document_to_pdf",
        {"access_plan": mounted_plan(source, target), "options": {"strip_embedded_media": False}},
        runner=fake_runner,
        env={"DOCUMENT_LIBREOFFICE_BIN": str(executable)},
    )

    assert captured["source"] == source
    assert result["media_preprocessing"] == {"removed_files": 0, "removed_bytes": 0}


def test_source_format_and_target_extension_are_strict(tmp_path):
    source = tmp_path / "slides.pptx"
    make_pptx(source)
    with pytest.raises(ConverterError, match="does not support source format"):
        invoke_operator("document_to_pdf", {"access_plan": mounted_plan(source, tmp_path / "slides.pdf", "odp")})
    with pytest.raises(ConverterError, match="must end with .pdf"):
        invoke_operator("document_to_pdf", {"access_plan": mounted_plan(source, tmp_path / "slides.png")})


def test_invalid_pptx_is_rejected_before_converter(tmp_path):
    source = tmp_path / "slides.pptx"
    source.write_bytes(b"not-a-zip")
    executable = tmp_path / "soffice"
    make_converter(executable)
    with pytest.raises(ConverterError, match="not a valid Open XML package"):
        invoke_operator(
            "document_to_pdf",
            {"access_plan": mounted_plan(source, tmp_path / "slides.pdf")},
            env={"DOCUMENT_LIBREOFFICE_BIN": str(executable)},
        )


def test_container_runtime_rewrites_loopback_object_store_endpoints(tmp_path, monkeypatch):
    source = tmp_path / "slides.pptx"
    target = tmp_path / "slides.pdf"
    executable = tmp_path / "soffice"
    make_pptx(source, include_media=False)
    make_converter(executable)
    plan = mounted_plan(source, target)
    plan["source"]["access"] = {
        "method": "object_store",
        "endpoint": "127.0.0.1:9002",
        "access_key": "source-ak",
        "secret_key": "source-sk",
        "bucket": "addp",
        "object": "doc/slides.pptx",
        "use_ssl": False,
    }
    plan["target"]["access"] = {
        "method": "object_store",
        "endpoint": "localhost:19000",
        "access_key": "target-ak",
        "secret_key": "target-sk",
        "bucket": "manager",
        "object": "tenant_1/slides.pdf",
        "use_ssl": False,
    }
    captured = {}

    def fake_stage(access_plan, work_dir):
        captured["source_endpoint"] = access_plan["source"]["access"]["endpoint"]
        return source

    def fake_publish(path, access_plan):
        captured["target_endpoint"] = access_plan["target"]["access"]["endpoint"]
        return {"object_uri": "s3://manager/slides.pdf", "object_name": "slides.pdf", "uploaded_bytes": path.stat().st_size}

    def fake_runner(command, timeout_seconds):
        output_dir = Path(command[command.index("--outdir") + 1])
        writer = PdfWriter()
        writer.add_blank_page(width=100, height=100)
        with (output_dir / "slides.pdf").open("wb") as stream:
            writer.write(stream)
        return CommandResult(returncode=0)

    monkeypatch.setattr(operators, "stage_source_file", fake_stage)
    monkeypatch.setattr(operators, "publish_target_file", fake_publish)
    invoke_operator(
        "document_to_pdf",
        {"access_plan": plan, "options": {"strip_embedded_media": False}},
        runner=fake_runner,
        env={
            "DOCUMENT_LIBREOFFICE_BIN": str(executable),
            "DOCUMENT_OBJECT_STORE_LOOPBACK_HOST": "host.docker.internal",
        },
    )

    assert captured == {
        "source_endpoint": "host.docker.internal:9002",
        "target_endpoint": "host.docker.internal:19000",
    }
    assert plan["source"]["access"]["endpoint"] == "127.0.0.1:9002"
