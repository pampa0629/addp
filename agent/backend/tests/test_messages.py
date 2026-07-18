import unittest

from services.messages import MESSAGE_PARTS_MAX_BYTES, bounded_message_parts


class MessagePersistenceTests(unittest.TestCase):
    def test_message_parts_are_copied_before_persistence(self):
        source = [{"type": "text", "text": "hello"}]

        bounded = bounded_message_parts(source)
        source[0]["text"] = "changed"

        self.assertEqual(bounded[0]["text"], "hello")

    def test_message_parts_reject_total_payload_over_byte_limit(self):
        with self.assertRaisesRegex(ValueError, "message parts exceed"):
            bounded_message_parts([{"type": "text", "text": "x" * MESSAGE_PARTS_MAX_BYTES}])


if __name__ == "__main__":
    unittest.main()
