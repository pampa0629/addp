#!/usr/bin/env python3
"""《数据治理100问》发行工具回归测试。"""

from __future__ import annotations

import tempfile
import unittest
from pathlib import Path
from zipfile import ZipFile

from release_archive import add_html_stylesheet, clean_release_output, write_fontconfig
from verify_release import has_cjk_font_name


BOOK_CSS = Path(__file__).with_name("book.css")


class ReleaseToolsTest(unittest.TestCase):
    def test_add_html_stylesheet_packages_referenced_css(self) -> None:
        with tempfile.TemporaryDirectory() as temp:
            archive_path = Path(temp) / "book.zip"
            with ZipFile(archive_path, "w") as archive:
                archive.writestr("index.html", '<link rel="stylesheet" href="book.css">')

            add_html_stylesheet(archive_path, BOOK_CSS)

            with ZipFile(archive_path) as archive:
                self.assertEqual(archive.read("book.css"), BOOK_CSS.read_bytes())

    def test_clean_release_output_removes_only_generated_formats(self) -> None:
        with tempfile.TemporaryDirectory() as temp:
            output_dir = Path(temp)
            generated = [output_dir / "001-question.docx", output_dir / "book.pdf"]
            preserved = output_dir / "editor-note"
            for path in [*generated, preserved]:
                path.write_text("test", encoding="utf-8")

            docx_dir = output_dir / "分篇DOCX"
            docx_dir.mkdir()
            nested_docx = docx_dir / "002-question.docx"
            nested_docx.write_text("test", encoding="utf-8")

            clean_release_output(output_dir, ("book.pdf", "book.epub"), "分篇DOCX")

            self.assertTrue(all(not path.exists() for path in generated))
            self.assertFalse(nested_docx.exists())
            self.assertTrue(preserved.is_file())

    def test_write_fontconfig_includes_fonts_and_cache(self) -> None:
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            config = root / "fonts.conf"
            cache = root / "font-cache"
            write_fontconfig(config, cache, [Path("/System/Library/Fonts"), Path("/Library/Fonts")])

            content = config.read_text(encoding="utf-8")
            self.assertIn("<dir>/System/Library/Fonts</dir>", content)
            self.assertIn("<dir>/Library/Fonts</dir>", content)
            self.assertIn(f"<cachedir>{cache}</cachedir>", content)
            self.assertTrue(cache.is_dir())

    def test_cjk_font_gate_rejects_unrelated_fallback_fonts(self) -> None:
        self.assertFalse(has_cjk_font_name({"/AAAAAA+LinuxLibertineG", "/BBBBBB+NotoSansLisu"}))
        self.assertTrue(has_cjk_font_name({"/AAAAAA+ArialUnicodeMS"}))


if __name__ == "__main__":
    unittest.main()
