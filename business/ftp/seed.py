"""Generate small Business examples without committing generated data directories."""
import base64
import csv
import hashlib
import io
import json
import os
from pathlib import Path
import sys

ROWS = [
    {"id": 1, "name": "长沙客户", "amount": 100.25},
    {"id": 2, "name": "株洲客户", "amount": 200.50},
    {"id": 3, "name": "湘潭客户", "amount": 300.75},
]
# Immutable Parquet seed encoded by parquet-go v0.25.1. The Common certification
# test decodes it through the real reader and checks the complete typed records.
# Keeping this tiny template avoids adding a Python native Parquet dependency.
PARQUET_TEMPLATE = (
    "UEFSMRUGFTAVMBWOtty8BUwVBhUAFQYVABUAFQASAAABAAAAAAAAAAIAAAAAAAAAAwAAAAAAAAAVBhV8FXwV8qCf4gpMFQYV"
    "ABUGFQwVABUAEgAAgAEEAxgXBAAAAMwAAAAAAAAAAAAAAAAAAADplb/mspnlrqLmiLfmoKrmtLLlrqLmiLfmuZjmva3lrqLm"
    "iLcVBhUwFTAV+vfz8QVMFQYVABUGFQAVABUAEgAAAAAAAAAQWUAAAAAAABBpQAAAAAAAzHJAGRIAGRgIAQAAAAAAAAAZGAgD"
    "AAAAAAAAABUAGRYAABkSABkYDOagqua0suWuouaItxkYDOmVv+aymeWuouaItxUAGRYAABkSABkYCAAAAAAAEFlAGRgIAAAA"
    "AADMckAVABkWAAAZHBYIFWgWAAAAGRwWcBW0ARYAAAAZHBakAhVoFgAAABUEGUxIA3JvdxUGABUEFYABFQAYAmlkJSRMrBNA"
    "EQAAABUMJQAYBG5hbWUlAEwcAAAAFQoVgAEVABgGYW1vdW50ABYGGRwZPCYAHBUEGRUAGRgCaWQVABYGFmgWaCYIPFgIAwAA"
    "AAAAAAAYCAEAAAAAAAAAABkcFQYVABUCAAAW1gQVFBaMAxU+ACYAHBUMGRUMGRgEbmFtZRUAFgYWtAEWtAEmcDxYDOmVv+ay"
    "meWuouaItxgM5qCq5rSy5a6i5oi3ABkcFQYVDBUCAAAW6gQVFhbKAxVOACYAHBUKGRUAGRgGYW1vdW50FQAWBhZoFmgmpAI8"
    "WAgAAAAAAMxyQBgIAAAAAAAQWUAAGRwVBhUAFQIAABaABRUWFpgEFT4AFoQDFgYZDBYIFoQDABkMGCBnaXRodWIuY29tL3Bh"
    "cnF1ZXQtZ28vcGFycXVldC1nbxk8HAAAHAAAHAAAAFwBAABQQVIx"
)


def write_samples(directory):
    directory = Path(directory)
    if directory.is_symlink():
        raise ValueError('FTP data root must not be a symbolic link')
    directory.mkdir(parents=True, exist_ok=True, mode=0o755)
    directory.chmod(0o755)
    directory = directory / 'examples'
    if directory.is_symlink():
        raise ValueError('FTP examples directory must not be a symbolic link')
    directory.mkdir(parents=True, exist_ok=True, mode=0o755)
    directory.chmod(0o755)
    output = io.StringIO(newline='')
    writer = csv.DictWriter(output, fieldnames=['id', 'name', 'amount'], lineterminator='\n')
    writer.writeheader()
    writer.writerows(ROWS)
    samples = {
        '客户 中文%.csv': output.getvalue().encode('utf-8'),
        '订单 示例.json': (json.dumps(ROWS, ensure_ascii=False, separators=(',', ':')) + '\n').encode('utf-8'),
        '客户 示例.parquet': base64.b64decode(PARQUET_TEMPLATE, validate=True),
    }
    for name, content in samples.items():
        path = directory / name
        try:
            descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o644)
        except FileExistsError:
            if path.is_symlink() or not path.is_file():
                raise ValueError('FTP seed target must be an ordinary file')
            continue
        with os.fdopen(descriptor, 'wb') as stream:
            stream.write(content)
            os.fchmod(stream.fileno(), 0o644)


MODEL_SAMPLE_FILES = (
    ('ifc', 'models/ifc/model.ifc', 'addp_online_model_fixture.ifc'),
    ('3ds', 'models/3ds/model.3ds', 'addp_online_model_fixture.3ds'),
    ('dae', 'models/dae/model.dae', 'addp_online_model_fixture.dae'),
    ('dae_png', 'models/dae/texture.png', 'addp_online_exchange_texture.png'),
    ('png', 'models/3ds/texture.png', 'addp_online_exchange_texture.png'),
    ('obj', 'models/obj/model.obj', 'addp_online_multi_material.obj'),
    ('obj_mtl_red', 'models/obj/paint red.mtl', 'addp_online_multi_material_red.mtl'),
    ('obj_mtl_blue', 'models/obj/paint blue.mtl', 'addp_online_multi_material_blue.mtl'),
    ('obj_png_red', 'models/obj/red/color.png', 'addp_online_obj_red.png'),
    ('obj_png_blue', 'models/obj/blue/color.png', 'addp_online_obj_blue.png'),
)


def write_model_samples(directory):
    """Publish fixed Hosted inputs through the existing FTP account root.

    This is explicit test preparation, not part of the default Business seed.
    Never replace a host file or follow a destination symbolic link.
    """
    root = Path(directory)
    fixture = Path(__file__).resolve().parents[1] / 'fixtures/manager'
    report = []
    for kind, relative, source in MODEL_SAMPLE_FILES:
        target = root / relative
        for directory_path in (root, *[root.joinpath(*Path(relative).parts[:i]) for i in range(1, len(Path(relative).parts))]):
            if directory_path.is_symlink():
                raise ValueError('FTP model seed directory must not be a symbolic link')
            directory_path.mkdir(parents=True, exist_ok=True, mode=0o755)
            directory_path.chmod(0o755)
        content = (fixture / source).read_bytes()
        try:
            descriptor = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o644)
        except FileExistsError:
            if target.is_symlink() or not target.is_file() or target.read_bytes() != content:
                raise ValueError('FTP model seed refuses to replace a host file')
        else:
            with os.fdopen(descriptor, 'wb') as stream:
                stream.write(content)
                os.fchmod(stream.fileno(), 0o644)
        report.append({'format': kind, 'full_name': relative, 'size': len(content),
                       'sha256': hashlib.sha256(content).hexdigest()})
    return report


if __name__ == '__main__':
    write_samples(sys.argv[1])
