import unittest

from agents.context import (
    HISTORY_CHAR_LIMIT,
    HISTORY_MESSAGE_LIMIT,
    MESSAGE_CHAR_LIMIT,
    SUMMARY_CHAR_LIMIT,
    build_context_window,
)


class ContextBudgetTests(unittest.TestCase):
    def test_context_keeps_latest_messages_with_explicit_omission_metrics(self):
        messages = [
            {"role": "user", "content": f"message-{index}"}
            for index in range(HISTORY_MESSAGE_LIMIT + 5)
        ]

        selected, _summary, metrics = build_context_window(messages, None)

        self.assertEqual(len(selected), HISTORY_MESSAGE_LIMIT)
        self.assertEqual(selected[0]["content"], "message-5")
        self.assertEqual(selected[-1]["content"], f"message-{HISTORY_MESSAGE_LIMIT + 4}")
        self.assertEqual(metrics["omitted_message_count"], 5)

    def test_context_applies_character_budgets_from_newest_to_oldest(self):
        messages = [
            {"role": "user", "content": str(index) * MESSAGE_CHAR_LIMIT}
            for index in range(5)
        ]

        selected, _summary, metrics = build_context_window(messages, None)

        self.assertLessEqual(metrics["message_char_count"], HISTORY_CHAR_LIMIT)
        self.assertTrue(selected[-1]["content"].startswith("4"))
        self.assertGreaterEqual(metrics["omitted_message_count"], 1)

    def test_context_truncates_one_message_and_summary(self):
        selected, summary, metrics = build_context_window(
            [{"role": "user", "content": "x" * (MESSAGE_CHAR_LIMIT + 100)}],
            "s" * (SUMMARY_CHAR_LIMIT + 100),
        )

        self.assertIn("上下文已截断", selected[0]["content"])
        self.assertIn("上下文已截断", summary)
        self.assertEqual(metrics["truncated_message_count"], 1)


if __name__ == "__main__":
    unittest.main()
