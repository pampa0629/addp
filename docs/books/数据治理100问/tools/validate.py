#!/usr/bin/env python3
"""校验书稿目录、标题、本地链接和延伸阅读。"""

from __future__ import annotations

import re
import sys
from pathlib import Path

from book_sources import BOOK_ROOT, CONTENTS_SOURCE, LOCAL_MARKDOWN_LINK, article_sources


DIRECTORY_ENTRY = re.compile(r"^([1-9]\d*)\. \[([^\]]+)\]\(([^)]+\.md)\)$", re.M)
EXTENDED_READING = re.compile(r"^- \[第(\d{3})问：([^\]]+)\]\(([^)]+\.md)\)$", re.M)


def fail(message: str) -> None:
    raise ValueError(message)


def validate() -> tuple[int, int]:
    articles = article_sources()
    if len(articles) != 104:
        fail(f"正文文件应为 104 篇，实际为 {len(articles)} 篇")

    by_number: dict[str, tuple[Path, str]] = {}
    for path in articles:
        number = path.name[:3]
        heading = path.read_text(encoding="utf-8").splitlines()[0]
        prefix = f"# {number} "
        if not heading.startswith(prefix):
            fail(f"正文一级标题与文件编号不一致：{path}")
        by_number[number] = (path, heading[len(prefix):])
    if sorted(by_number) != [f"{number:03d}" for number in range(1, 105)]:
        fail("正文编号不是连续的 001—104")

    contents = CONTENTS_SOURCE.read_text(encoding="utf-8")
    entries = DIRECTORY_ENTRY.findall(contents)
    if [int(number) for number, _, _ in entries] != list(range(1, 105)):
        fail("目录正文条目不是连续的 001—104")
    for number, title, link in entries:
        expected_path, expected_title = by_number[f"{int(number):03d}"]
        if (BOOK_ROOT / link).resolve() != expected_path.resolve() or title != expected_title:
            fail(f"目录第 {number} 问的题名或路径与正文不一致")

    extended_total = 0
    for source in [CONTENTS_SOURCE, BOOK_ROOT / "000-为什么要写这本书.md", *articles]:
        text = source.read_text(encoding="utf-8")
        for target_text, _ in LOCAL_MARKDOWN_LINK.findall(text):
            target = (source.parent / target_text).resolve()
            if not target.is_file():
                fail(f"本地链接失效：{source} -> {target_text}")
        if source not in articles:
            continue
        if text.count("\n## 延伸阅读\n") != 1:
            fail(f"正文必须有且只有一个“延伸阅读”：{source}")
        section = text.split("\n## 延伸阅读\n", 1)[1]
        if re.search(r"(?m)^- 第\d{3}问：", section):
            fail(f"延伸阅读仍包含未加链接的跨问引用：{source}")
        references = EXTENDED_READING.findall(section)
        if not 4 <= len(references) <= 7:
            fail(f"延伸阅读应为 4—7 条：{source} 实际为 {len(references)} 条")
        seen: set[str] = set()
        source_number = source.name[:3]
        for number, title, link in references:
            if number == source_number:
                fail(f"延伸阅读不能引用自身：{source}")
            if number in seen:
                fail(f"延伸阅读重复引用第 {number} 问：{source}")
            seen.add(number)
            target, expected_title = by_number.get(number, (None, None))
            if target is None or title != expected_title:
                fail(f"延伸阅读第 {number} 问的题名不正确：{source}")
            if (source.parent / link).resolve() != target.resolve():
                fail(f"延伸阅读第 {number} 问的路径不正确：{source}")
        extended_total += len(references)
    if extended_total != 641:
        fail(f"延伸阅读跨问链接应为 641 条，实际为 {extended_total} 条")
    return len(entries), extended_total


def main() -> int:
    try:
        directory_entries, extended_readings = validate()
    except ValueError as error:
        print(f"书稿校验失败：{error}", file=sys.stderr)
        return 1
    print(
        f"书稿校验通过：{directory_entries} 个正文目录条目，"
        f"{extended_readings} 条延伸阅读链接。"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
