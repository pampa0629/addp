#!/usr/bin/env python3
"""《数据治理100问》源稿清单、稳定锚点和跨格式链接规则。"""

from __future__ import annotations

import os
import re
from pathlib import Path
from typing import Callable


BOOK_ROOT = Path(__file__).resolve().parent.parent
BOOK_AUTHOR = "攀爬"
DOCX_DIR_NAME = "分篇DOCX"
CONTENTS_SOURCE = BOOK_ROOT / "目录.md"
PREFACE_SOURCE = BOOK_ROOT / "000-为什么要写这本书.md"
LOCAL_MARKDOWN_LINK = re.compile(r"\]\(([^)]+?\.md)(#[^)]+)?\)")
ARTICLE_NAME = re.compile(r"(\d{3})-(.+)\.md$")


def article_sources() -> list[Path]:
    return sorted(
        path
        for path in BOOK_ROOT.glob("[0-9][0-9]-*篇/[0-9][0-9][0-9]-*.md")
        if path.is_file()
    )


def reading_sources() -> list[Path]:
    return [PREFACE_SOURCE, *article_sources()]


def all_sources() -> list[Path]:
    return [CONTENTS_SOURCE, *reading_sources()]


def question_number(source: Path) -> str | None:
    match = ARTICLE_NAME.fullmatch(source.name)
    return match.group(1) if match else None


def source_anchor(source: Path) -> str:
    if source.name == CONTENTS_SOURCE.name:
        return "contents"
    number = question_number(source)
    if number is None:
        raise ValueError(f"无法为源稿生成锚点：{source}")
    return f"q{number}"


def source_heading(source: Path) -> str:
    first_line = source.read_text(encoding="utf-8").splitlines()[0]
    if not first_line.startswith("# "):
        raise ValueError(f"源稿缺少一级标题：{source}")
    return first_line[2:]


def source_by_filename() -> dict[str, Path]:
    return {source.name: source for source in all_sources()}


def rewrite_local_links(content: str, target_for: Callable[[Path], str]) -> str:
    sources = source_by_filename()

    def replace(match: re.Match[str]) -> str:
        filename = Path(match.group(1)).name
        target = sources.get(filename)
        if target is None:
            raise ValueError(f"本地 Markdown 链接目标不在书稿清单中：{match.group(1)}")
        fragment = match.group(2) or ""
        return f"]({target_for(target)}{fragment})"

    return LOCAL_MARKDOWN_LINK.sub(replace, content)


def rewrite_links_for_docx(content: str) -> str:
    return rewrite_local_links(content, lambda target: f"{target.stem}.docx")


def rewrite_links_for_combined(content: str) -> str:
    return rewrite_local_links(content, lambda target: f"#{source_anchor(target)}")


def relative_markdown_target(source: Path, target: Path) -> str:
    return Path(os.path.relpath(target, source.parent)).as_posix()


def navigation_markdown(source: Path, combined: bool) -> str:
    sources = reading_sources()
    if source not in sources:
        return ""
    index = sources.index(source)

    def target_for(target: Path) -> str:
        return f"#{source_anchor(target)}" if combined else f"{target.stem}.docx"

    links = []
    if index > 0:
        previous = sources[index - 1]
        links.append(f"[← 上一问：{source_heading(previous)}]({target_for(previous)})")
    contents_target = "#contents" if combined else "目录.docx"
    links.append(f"[返回目录]({contents_target})")
    if index + 1 < len(sources):
        following = sources[index + 1]
        links.append(f"[下一问：{source_heading(following)} →]({target_for(following)})")
    return "\n\n---\n\n" + " · ".join(links)


def add_heading_anchor(content: str, source: Path) -> str:
    anchor = source_anchor(source)
    if source.name == CONTENTS_SOURCE.name:
        replacement = f"# 目录 {{#{anchor} .unnumbered}}"
    else:
        replacement = rf"# \1 {{#{anchor}}}"
    rewritten, count = re.subn(r"^# (.+)$", replacement, content, count=1, flags=re.M)
    if count != 1:
        raise ValueError(f"无法为源稿一级标题添加锚点：{source}")
    return rewritten


def combined_markdown() -> str:
    sections = []
    for source in all_sources():
        content = source.read_text(encoding="utf-8")
        content = rewrite_links_for_combined(content)
        content = add_heading_anchor(content, source)
        content += navigation_markdown(source, combined=True)
        sections.append(content)
    return "\n\n".join(sections) + "\n"
