import json
from pathlib import Path

import pytest

from max_converter import conversion_options, declared_texture
from operators import CommandResult, ConverterError, invoke_operator
from .glb_fixture import glb_bytes, triangle_doc
from .test_operators import file_plan


@pytest.mark.parametrize("unit,scale", [(None, 1.0), ("", 1.0), ("mm", .001), ("cm", .01),
                                        ("m", 1), ("km", 1000), ("in", .0254), ("ft", .3048), ("mi", 1609.344)])
def test_unknown_source_unit_uses_user_choice_then_default_metres(unit, scale):
    options = {} if unit is None else {"source_unit": unit}
    facts, textures = conversion_options(options)
    assert facts == {"source_unit": unit or "m", "unit_source": "user" if unit else "default", "scale_to_meters": scale}
    assert textures == {}


@pytest.mark.parametrize("options", [{"source_unit": "meters"}, {"source_unit": True}, {"source_unit": None},
                                     {"source_unit": " m"}, {"scale": .001}, {"texture_files": []}])
def test_invalid_unit_and_undeclared_options_are_rejected(options):
    with pytest.raises(ValueError):
        conversion_options(options)


@pytest.mark.parametrize("path", ["../outside.png", "/etc/passwd", "C:/outside.png", "a\\b.png", "https://example.org/a.png"])
def test_declared_bitmap_cannot_escape_the_input_bundle(tmp_path, path):
    with pytest.raises(ValueError):
        declared_texture(tmp_path, {"bitmap": path}, "bitmap")


def test_only_explicit_texture_is_read_even_when_same_named_file_exists(tmp_path):
    image = tmp_path / "same.png"
    image.write_bytes(b"image")
    with pytest.raises(ValueError, match="no declared input"):
        declared_texture(tmp_path, {}, r"C:\old\same.png")
    assert declared_texture(tmp_path, {r"C:\old\same.png": "same.png"}, r"C:\old\same.png") == image
    outside = tmp_path.parent / (tmp_path.name + "-outside.png")
    outside.write_bytes(b"outside")
    link = tmp_path / "linked.png"
    link.symlink_to(outside)
    try:
        with pytest.raises(ValueError, match="unavailable"):
            declared_texture(tmp_path, {"bitmap": "linked.png"}, "bitmap")
    finally:
        outside.unlink()


@pytest.mark.parametrize("valid", [True, False])
def test_max_operator_publishes_only_valid_glb_and_audits_default_unit(tmp_path, valid):
    source, target = tmp_path / "input.max", tmp_path / "ready.glb"
    source.write_bytes(b"MAX input controlled by fake converter")
    target.write_bytes(b"previous valid artifact")
    plan = file_plan(source, target, "max", "glb")
    plan["target"]["write_mode"] = "replace"
    commands = []
    def runner(command, timeout):
        commands.append(command)
        assert command[1:4] == ["--background", "--factory-startup", "--disable-autoexec"]
        assert "--python-exit-code" in command
        args = command[command.index("--")+1:]
        assert json.loads(Path(args[2]).read_text()) == {}
        output = Path(args[1])
        output.write_bytes(glb_bytes(*triangle_doc()) if valid else b"broken output")
        output.with_suffix(".max-facts.json").write_text(json.dumps(conversion_options({})[0]))
        return CommandResult(0)
    if valid:
        result = invoke_operator("max_to_glb", {"access_plan": plan}, runner=runner,
                                 env={"MODEL3D_BLENDER_BIN": "/private/blender", "MODEL3D_MAX_ADDON_PATH": "/private/source"})
        assert result["source_format"] == "max"
        assert result["conversion"]["unit_source"] == "default"
        assert result["conversion"]["source_unit"] == "m"
        assert target.read_bytes() != b"previous valid artifact"
    else:
        with pytest.raises(ConverterError):
            invoke_operator("max_to_glb", {"access_plan": plan}, runner=runner, env={})
        assert target.read_bytes() == b"previous valid artifact"
    assert len(commands) == 1


def test_invalid_max_unit_is_rejected_before_converter_is_called(tmp_path):
    source, target = tmp_path / "input.max", tmp_path / "output.glb"
    source.write_bytes(b"input")
    with pytest.raises(ConverterError) as error:
        invoke_operator("max_to_glb", {"access_plan": file_plan(source, target, "max", "glb"), "options": {"source_unit": "wrong"}},
                        runner=lambda *args: pytest.fail("converter called for invalid options"))
    assert error.value.error_code == "INVALID_MAX_OPTIONS"
    assert not target.exists()


@pytest.mark.parametrize('report', [None, '{', '[]'])
def test_missing_or_invalid_max_audit_report_preserves_previous_artifact(tmp_path, report):
    source, target = tmp_path / 'input.max', tmp_path / 'ready.glb'
    source.write_bytes(b'controlled MAX input')
    target.write_bytes(b'previous artifact')
    plan = file_plan(source, target, 'max', 'glb')
    plan['target']['write_mode'] = 'replace'
    def runner(command, timeout):
        args = command[command.index('--') + 1:]
        output = Path(args[1])
        output.write_bytes(glb_bytes(*triangle_doc()))
        if report is not None:
            output.with_suffix('.max-facts.json').write_text(report)
        return CommandResult(0)
    with pytest.raises(ConverterError) as error:
        invoke_operator('max_to_glb', {'access_plan': plan}, runner=runner, env={})
    assert error.value.error_code == 'INVALID_CONVERSION_FACTS'
    assert target.read_bytes() == b'previous artifact'


@pytest.mark.parametrize('kind', ['parent', 'absolute', 'symlink'])
def test_max_directory_entrypoint_cannot_escape_authorized_input(tmp_path, kind):
    root = tmp_path / 'authorized'
    root.mkdir()
    outside = tmp_path / 'outside.max'
    outside.write_bytes(b'outside the authorized source directory')
    target = tmp_path / 'previous.glb'
    target.write_bytes(b'previous artifact')
    entrypoint = '../outside.max' if kind == 'parent' else str(outside)
    if kind == 'symlink':
        (root / 'linked.max').symlink_to(outside)
        entrypoint = 'linked.max'
    plan = file_plan(outside, target, 'max', 'glb')
    plan['source'] = {'kind': 'directory', 'format': 'max', 'entrypoint': entrypoint,
                      'access': {'method': 'mounted_path', 'path': str(root)}}
    plan['target']['write_mode'] = 'replace'
    with pytest.raises(ConverterError) as error:
        invoke_operator('max_to_glb', {'access_plan': plan}, runner=lambda *args: pytest.fail('outside input reached converter'), env={})
    assert error.value.error_code == 'SOURCE_OUTSIDE_INPUT'
    assert target.read_bytes() == b'previous artifact'
