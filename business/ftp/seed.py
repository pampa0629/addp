"""Generate small Business examples without committing generated data directories."""
import base64
import csv
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


if __name__ == '__main__':
    write_samples(sys.argv[1])
