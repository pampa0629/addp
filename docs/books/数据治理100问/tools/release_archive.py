#!/usr/bin/env python3
"""发行压缩包的纯标准库辅助函数。"""

from __future__ import annotations

from pathlib import Path
from xml.sax.saxutils import escape
from zipfile import ZIP_DEFLATED, ZipFile


def add_html_stylesheet(archive_path: Path, stylesheet: Path) -> None:
    """把 Pandoc chunkedhtml 引用但未收录的样式表写入 ZIP。"""
    with ZipFile(archive_path, "a", compression=ZIP_DEFLATED) as archive:
        archive.writestr(stylesheet.name, stylesheet.read_bytes())


def clean_release_output(
    output_dir: Path,
    combined_names: tuple[str, ...],
    docx_dir_name: str,
) -> None:
    """清除统一工具可重建的发行物，保留目录中的其他文件。"""
    output_dir.mkdir(parents=True, exist_ok=True)
    for path in output_dir.glob("*.docx"):
        if path.is_file():
            path.unlink()
    docx_dir = output_dir / docx_dir_name
    if docx_dir.is_dir():
        for path in docx_dir.glob("*.docx"):
            if path.is_file():
                path.unlink()
    for name in combined_names:
        path = output_dir / name
        if path.is_file():
            path.unlink()


def write_fontconfig(config_path: Path, cache_dir: Path, font_dirs: list[Path]) -> None:
    """生成供 LibreOffice 使用的最小 Fontconfig 配置。"""
    cache_dir.mkdir(parents=True, exist_ok=True)
    directories = "\n".join(f"  <dir>{escape(str(path))}</dir>" for path in font_dirs)
    content = (
        '<?xml version="1.0"?>\n'
        '<!DOCTYPE fontconfig SYSTEM "fonts.dtd">\n'
        "<fontconfig>\n"
        f"{directories}\n"
        f"  <cachedir>{escape(str(cache_dir))}</cachedir>\n"
        "</fontconfig>\n"
    )
    config_path.write_text(content, encoding="utf-8")
