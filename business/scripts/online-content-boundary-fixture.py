#!/usr/bin/env python3
"""Generate the same tiny, deterministic content boundary fixture for NFS and MinIO."""

import argparse
import struct
from pathlib import Path


def point_shapefile(root: Path, name: str, x: float, y: float) -> None:
    def header(words: int) -> bytes:
        return (struct.pack(">7i", 9994, 0, 0, 0, 0, 0, words)
                + struct.pack("<2i8d", 1000, 1, x, y, x, y, 0, 0, 0, 0))

    (root / f"{name}.shp").write_bytes(header(64) + struct.pack(">2i", 1, 10) + struct.pack("<i2d", 1, x, y))
    (root / f"{name}.shx").write_bytes(header(54) + struct.pack(">2i", 50, 10))
    dbf_header = bytearray(32)
    struct.pack_into("<4BI2H", dbf_header, 0, 3, 126, 1, 1, 1, 65, 17)
    field = bytearray(32)
    field[:4] = b"NAME"
    field[11], field[16] = ord("C"), 16
    (root / f"{name}.dbf").write_bytes(dbf_header + field + b"\r " + name.encode().ljust(16) + b"\x1a")


def generate(root: Path) -> None:
    for scope in ("directory", "refs"):
        directory = root / "scan-boundary" / scope
        directory.mkdir(parents=True, exist_ok=False)
        point_shapefile(directory, "roads", 120, 30)
        (directory / "roads.cpg").write_text("UTF-8\n", encoding="ascii")
        point_shapefile(directory, "parcels", 121, 31)
        (directory / "parcels.prj").write_text('GEOGCS["WGS 84",DATUM["WGS_1984",SPHEROID["WGS 84",6378137,298.257223563]],PRIMEM["Greenwich",0],UNIT["degree",0.0174532925199433],AUTHORITY["EPSG","4326"]]\n', encoding="ascii")
        (directory / "roads.qmd").write_text("unclaimed same-basename resource\n", encoding="ascii")
        (directory / "readme.txt").write_text("independent resource\n", encoding="ascii")
    outside = root / "scan-boundary" / "outside"
    outside.mkdir()
    (outside / "roads.dbf").write_bytes((root / "scan-boundary/directory/roads.dbf").read_bytes())


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("root", type=Path)
    generate(parser.parse_args().root)
