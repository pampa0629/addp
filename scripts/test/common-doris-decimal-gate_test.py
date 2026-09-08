#!/usr/bin/env python3

from __future__ import annotations

import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("common-doris-decimal-gate.sh")


class CommonDorisDecimalGateTest(unittest.TestCase):
    def test_readiness_requires_an_alive_non_decommissioned_backend(self) -> None:
        source = SCRIPT.read_text(encoding="utf-8")

        self.assertIn("SHOW BACKENDS;", source)
        self.assertIn('$column == "Alive"', source)
        self.assertIn('$column == "SystemDecommissioned"', source)
        self.assertIn('tolower($alive_column) == "true"', source)
        self.assertIn('tolower($decommissioned_column) == "false"', source)
        self.assertNotIn("SHOW FRONTENDS;", source)


if __name__ == "__main__":
    unittest.main()
