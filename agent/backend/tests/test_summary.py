import unittest
from types import SimpleNamespace

from agents.context import HISTORY_MESSAGE_LIMIT
from utils.summary import _select_incremental_messages


class IncrementalSummaryTests(unittest.TestCase):
    def test_watermark_advances_without_recompressing_previous_messages(self):
        first_batch = [SimpleNamespace(id=index) for index in range(1, HISTORY_MESSAGE_LIMIT + 6)]

        first_messages, first_watermark = _select_incremental_messages(first_batch)

        self.assertEqual([message.id for message in first_messages], [1, 2, 3, 4, 5])
        self.assertEqual(first_watermark, 5)

        second_batch = [
            SimpleNamespace(id=index)
            for index in range(first_watermark + 1, HISTORY_MESSAGE_LIMIT + 8)
        ]
        second_messages, second_watermark = _select_incremental_messages(second_batch)

        self.assertEqual([message.id for message in second_messages], [6, 7])
        self.assertEqual(second_watermark, 7)

    def test_does_not_advance_watermark_inside_recent_window(self):
        recent_messages = [
            SimpleNamespace(id=index)
            for index in range(1, HISTORY_MESSAGE_LIMIT + 1)
        ]

        messages, watermark = _select_incremental_messages(recent_messages)

        self.assertEqual(messages, [])
        self.assertIsNone(watermark)


if __name__ == "__main__":
    unittest.main()
