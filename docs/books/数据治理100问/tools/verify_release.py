#!/usr/bin/env python3
"""校验《数据治理100问》完整发行物的结构、锚点、链接和资源。"""

from __future__ import annotations

import posixpath
import re
from pathlib import Path
from urllib.parse import unquote, urlsplit
from zipfile import ZipFile

from book_sources import BOOK_AUTHOR, DOCX_DIR_NAME, PREFACE_SOURCE, all_sources


COMBINED_DOCX_NAME = "数据治理100问-合订本.docx"
PDF_NAME = "数据治理100问.pdf"
EPUB_NAME = "数据治理100问.epub"
HTML_NAME = "数据治理100问-html.zip"
ATTRIBUTE = re.compile(r'\b(?:href|src)="([^"]+)"')
IDENTIFIER = re.compile(r'\bid="([^"]+)"')


def verify_docx(output_dir: Path) -> tuple[int, int]:
    docx_dir = output_dir / DOCX_DIR_NAME
    expected_names = {f"{source.stem}.docx" for source in all_sources()}
    missing = sorted(name for name in expected_names if not (docx_dir / name).is_file())
    if missing:
        raise ValueError(f"缺少分篇 DOCX：{', '.join(missing[:5])}")

    unexpected = sorted(path.name for path in docx_dir.glob("*.docx") if path.name not in expected_names)
    if unexpected:
        raise ValueError(f"存在过期或未知 DOCX：{', '.join(unexpected[:5])}")
    unexpected_root = sorted(
        path.name for path in output_dir.glob("*.docx") if path.name != COMBINED_DOCX_NAME
    )
    if unexpected_root:
        raise ValueError(f"分篇 DOCX 未收拢到 {DOCX_DIR_NAME}：{', '.join(unexpected_root[:5])}")

    relationship_targets = 0
    for source in all_sources():
        name = f"{source.stem}.docx"
        with ZipFile(docx_dir / name) as archive:
            relationships = archive.read("word/_rels/document.xml.rels").decode("utf-8")
            core_properties = archive.read("docProps/core.xml").decode("utf-8")
            document = archive.read("word/document.xml").decode("utf-8")
        if source.name == PREFACE_SOURCE.name:
            if BOOK_AUTHOR not in core_properties:
                raise ValueError(f"{name} 缺少作者元数据")
            if "作者" not in document or BOOK_AUTHOR not in document:
                raise ValueError(f"{name} 缺少可见作者署名")
        elif BOOK_AUTHOR in core_properties or f"作者：{BOOK_AUTHOR}" in document:
            raise ValueError(f"{name} 不应重复显示作者署名")
        targets = re.findall(r'Target="([^"]+\.docx)"', relationships)
        unknown = sorted(set(targets) - expected_names)
        if unknown:
            raise ValueError(f"{name} 存在未知 DOCX 目标：{unknown}")
        relationship_targets += len(targets)

    combined = output_dir / COMBINED_DOCX_NAME
    if not combined.is_file():
        raise ValueError(f"缺少合订本 DOCX：{combined}")
    with ZipFile(combined) as archive:
        document = archive.read("word/document.xml").decode("utf-8")
        core_properties = archive.read("docProps/core.xml").decode("utf-8")
    if BOOK_AUTHOR not in core_properties or "作者" not in document or BOOK_AUTHOR not in document:
        raise ValueError("合订本 DOCX 缺少作者署名或作者元数据")
    bookmarks = set(re.findall(r'w:bookmarkStart[^>]+w:name="([^"]+)"', document))
    anchors = re.findall(r'w:hyperlink w:anchor="([^"]+)"', document)
    required = {"contents", *{f"q{number:03d}" for number in range(105)}}
    missing_bookmarks = sorted(required - bookmarks)
    invalid_anchors = sorted(set(anchors) - bookmarks)
    if missing_bookmarks or invalid_anchors:
        raise ValueError(
            f"合订本 DOCX 书签或内部链接失效："
            f"missing={missing_bookmarks}, invalid={invalid_anchors}"
        )
    return relationship_targets, len(anchors)


def archive_documents(archive: ZipFile) -> tuple[set[str], dict[str, set[str]]]:
    names = set(archive.namelist())
    identifiers: dict[str, set[str]] = {}
    for name in names:
        if not name.endswith((".html", ".xhtml", ".opf", ".ncx")):
            continue
        text = archive.read(name).decode("utf-8")
        identifiers[name] = set(IDENTIFIER.findall(text))
    return names, identifiers


def verify_archive_links(path: Path) -> tuple[int, int]:
    if not path.is_file():
        raise ValueError(f"缺少发行物：{path}")
    checked_links = 0
    resources = 0
    with ZipFile(path) as archive:
        names, identifiers = archive_documents(archive)
        for source in identifiers:
            text = archive.read(source).decode("utf-8")
            for raw_target in ATTRIBUTE.findall(text):
                parsed = urlsplit(raw_target)
                if parsed.scheme or parsed.netloc or raw_target.startswith(("mailto:", "tel:")):
                    continue
                target_path = unquote(parsed.path)
                if target_path:
                    target = posixpath.normpath(posixpath.join(posixpath.dirname(source), target_path))
                else:
                    target = source
                if target not in names:
                    raise ValueError(f"{path.name}: {source} -> {raw_target} 目标不存在")
                resources += 1
                if parsed.fragment:
                    target_ids = identifiers.get(target, set())
                    if unquote(parsed.fragment) not in target_ids:
                        raise ValueError(f"{path.name}: {source} -> {raw_target} 锚点不存在")
                    checked_links += 1
    return checked_links, resources


def verify_archive_author(path: Path, *, epub: bool) -> None:
    with ZipFile(path) as archive:
        if epub:
            metadata_names = [name for name in archive.namelist() if name.endswith(".opf")]
            metadata = "\n".join(
                archive.read(name).decode("utf-8") for name in metadata_names
            )
            if "dc:creator" not in metadata or BOOK_AUTHOR not in metadata:
                raise ValueError(f"{path.name} 缺少作者元数据")
        else:
            index = archive.read("index.html").decode("utf-8")
            if 'name="author"' not in index or BOOK_AUTHOR not in index:
                raise ValueError(f"{path.name} 缺少作者元数据")


CJK_FONT_MARKERS = (
    "ArialUnicode",
    "HiraginoSansGB",
    "NotoSansCJK",
    "PingFang",
    "Songti",
    "STHeiti",
    "STSong",
    "SourceHan",
    "WenQuanYi",
)


def has_cjk_font_name(font_names: set[str]) -> bool:
    return any(marker in name for name in font_names for marker in CJK_FONT_MARKERS)


def verify_pdf(output_dir: Path) -> tuple[int, int, int]:
    path = output_dir / PDF_NAME
    if not path.is_file() or path.stat().st_size < 1_000_000:
        raise ValueError(f"PDF 未生成或体积异常：{path}")
    if path.read_bytes()[:5] != b"%PDF-":
        raise ValueError(f"PDF 文件头无效：{path}")
    try:
        from pypdf import PdfReader
    except ImportError:
        return 0, 0, 0
    reader = PdfReader(path)
    if not reader.metadata or reader.metadata.author != BOOK_AUTHOR:
        raise ValueError(f"PDF 作者元数据不是“{BOOK_AUTHOR}”")

    def flatten(items):
        for item in items:
            if isinstance(item, list):
                yield from flatten(item)
            else:
                yield item

    outlines = sum(1 for _ in flatten(reader.outline))
    internal_links = 0
    external_links = 0
    font_names: set[str] = set()
    for page in reader.pages:
        resources = page.get("/Resources")
        if hasattr(resources, "get_object"):
            resources = resources.get_object()
        fonts = resources.get("/Font", {}) if resources else {}
        if hasattr(fonts, "get_object"):
            fonts = fonts.get_object()
        for reference in fonts.values():
            font_names.add(str(reference.get_object().get("/BaseFont", "")))
        for reference in page.get("/Annots", []):
            annotation = reference.get_object()
            if annotation.get("/Subtype") != "/Link":
                continue
            action = annotation.get("/A")
            if action and action.get("/S") == "/URI":
                external_links += 1
            elif annotation.get("/Dest") is not None or (action and action.get("/S") == "/GoTo"):
                internal_links += 1
    if len(reader.pages) < 100 or outlines < 106 or internal_links < 100:
        raise ValueError(
            f"PDF 页数、书签或内部链接异常："
            f"pages={len(reader.pages)}, outlines={outlines}, internal={internal_links}"
        )
    if not has_cjk_font_name(font_names):
        raise ValueError(f"PDF 未嵌入可识别的中文字体：{sorted(font_names)}")
    return len(reader.pages), outlines, internal_links + external_links


def verify_release(output_dir: Path) -> None:
    docx_targets, docx_anchors = verify_docx(output_dir)
    epub_links, epub_resources = verify_archive_links(output_dir / EPUB_NAME)
    html_links, html_resources = verify_archive_links(output_dir / HTML_NAME)
    verify_archive_author(output_dir / EPUB_NAME, epub=True)
    verify_archive_author(output_dir / HTML_NAME, epub=False)
    pdf_pages, pdf_outlines, pdf_links = verify_pdf(output_dir)
    print(
        "发行物校验通过："
        f"DOCX 跨文档链接 {docx_targets} 个、合订本内部链接 {docx_anchors} 个；"
        f"EPUB 锚点链接 {epub_links} 个/资源 {epub_resources} 个；"
        f"HTML 锚点链接 {html_links} 个/资源 {html_resources} 个；"
        f"PDF {pdf_pages or '未深度解析'} 页、{pdf_outlines or '未深度解析'} 个书签、"
        f"{pdf_links or '未深度解析'} 个链接。"
    )
