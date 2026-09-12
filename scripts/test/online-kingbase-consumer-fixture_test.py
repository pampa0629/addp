import unittest
from pathlib import Path


SCRIPT = (
    Path(__file__).parents[2]
    / "business/scripts/online-kingbase-consumer-fixture.sh"
)


class OnlineKingbaseConsumerFixtureTest(unittest.TestCase):
    def test_declares_single_licensed_disposable_lifecycle(self) -> None:
        content = SCRIPT.read_text(encoding="utf-8")

        for fragment in (
            "kingbase_validate_license_input",
            "kingbase_ensure_official_image",
            "docker create",
            "kingbase_install_license_into_created_container",
            "docker rm --force",
            "docker image rm",
            "start|advance|stop|status",
            '"engine_type": "kingbase"',
        ):
            self.assertIn(fragment, content)
        self.assertNotIn("docker run", content)
        self.assertNotIn("--volume", content)

    def test_uses_native_on_conflict_increment_without_merge(self) -> None:
        content = SCRIPT.read_text(encoding="utf-8")

        self.assertIn("ON CONFLICT (id) DO UPDATE", content)
        self.assertIn("updated_at > TIMESTAMP", content)
        self.assertNotIn("MERGE INTO", content)


if __name__ == "__main__":
    unittest.main()
