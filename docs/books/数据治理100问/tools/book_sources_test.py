#!/usr/bin/env python3

from __future__ import annotations

import re
import unittest

from book_sources import (
    CONTENTS_SOURCE,
    PREFACE_SOURCE,
    all_sources,
    combined_markdown,
    navigation_markdown,
    rewrite_links_for_docx,
)


class BookSourcesTest(unittest.TestCase):
    def test_source_order_is_stable(self) -> None:
        sources = all_sources()
        self.assertEqual(106, len(sources))
        self.assertEqual("目录.md", sources[0].name)
        self.assertEqual("000-为什么要写这本书.md", sources[1].name)
        self.assertTrue(sources[2].name.startswith("001-"))
        self.assertTrue(sources[-1].name.startswith("104-"))

    def test_combined_markdown_has_stable_anchors_and_no_local_md_links(self) -> None:
        content = combined_markdown()
        for number in range(105):
            self.assertEqual(1, content.count(f"{{#q{number:03d}}}"))
        self.assertEqual(1, content.count("{#contents .unnumbered}"))
        self.assertNotIn("{#book-title", content)
        self.assertIsNone(re.search(r"\]\([^)]+\.md(?:#[^)]+)?\)", content))

    def test_docx_links_are_flattened_to_sibling_documents(self) -> None:
        content = CONTENTS_SOURCE.read_text(encoding="utf-8")
        rewritten = rewrite_links_for_docx(content)
        self.assertIn("](001-数据治理的第一性原理.docx)", rewritten)
        self.assertNotIn(".md)", rewritten)

    def test_navigation_covers_contents_previous_and_next(self) -> None:
        first = navigation_markdown(PREFACE_SOURCE, combined=True)
        self.assertIn("](#contents)", first)
        self.assertIn("](#q001)", first)
        self.assertNotIn("上一问", first)


if __name__ == "__main__":
    unittest.main()
