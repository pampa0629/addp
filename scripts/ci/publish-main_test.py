import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


class MainPublicationTest(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory(prefix="addp-main-publication-")
        self.addCleanup(temp.cleanup)
        self.root = Path(temp.name)
        self.remote = self.root / "origin.git"
        self.local = self.root / "local"
        self.local.mkdir()
        self.env = {
            **os.environ,
            "GIT_CONFIG_NOSYSTEM": "1",
            "GIT_CONFIG_GLOBAL": os.devnull,
            "GIT_AUTHOR_NAME": "Publication Test",
            "GIT_AUTHOR_EMAIL": "publication@example.invalid",
            "GIT_COMMITTER_NAME": "Publication Test",
            "GIT_COMMITTER_EMAIL": "publication@example.invalid",
        }
        self.git("init", "--bare", "--initial-branch=main", str(self.remote))
        self.git("init", "--initial-branch=main")
        self.git("remote", "add", "origin", str(self.remote))
        self.git("commit", "--allow-empty", "-m", "remote baseline")
        self.git("push", "origin", "main")
        self.base = self.head()
        self.publisher = self.local / "scripts/ci/publish-main.sh"
        self.publisher.parent.mkdir(parents=True)
        shutil.copy2(Path(__file__).with_name("publish-main.sh"), self.publisher)
        self.publish("--install")
        self.lock = self.local / ".git/addp-main-publish.lock"

    def run_command(self, *args, success=True):
        result = subprocess.run(
            args, cwd=self.local, env=self.env, text=True, capture_output=True, timeout=20,
        )
        self.assertEqual(result.returncode == 0, success, result.stderr + result.stdout)
        return result

    def git(self, *args, success=True):
        return self.run_command("git", *args, success=success).stdout.strip()

    def head(self):
        return self.git("rev-parse", "HEAD")

    def remote_head(self, remote=None):
        return self.git("--git-dir", str(remote or self.remote), "rev-parse", "refs/heads/main")

    def publish(self, *args, success=True):
        return self.run_command("bash", str(self.publisher), *args, success=success)

    def partial(self):
        result = self.publish(
            "--", "bash", "-ec", "git commit --allow-empty -m 'first phase'; exit 7", success=False,
        )
        self.assertEqual(result.returncode, 7)
        self.assertEqual(self.remote_head(), self.base)
        self.assertFalse(self.lock.exists())
        return self.head()

    def resume(self, pending, *args, baseline=None, success=True):
        return self.publish("--resume", pending, baseline or self.base, "--", *args, success=success)

    def assert_unpublished(self, pending):
        self.assertEqual(self.head(), pending)
        self.assertEqual(self.remote_head(), self.base)
        self.assertFalse(self.lock.exists())

    def test_resume_publishes_both_phases_and_keeps_unrelated_work(self):
        pending = self.partial()
        self.assertNotEqual(pending, self.base)
        unrelated = self.local / "unrelated"
        unrelated.write_text("other work\n")
        self.publish("--", "touch", "unexpected", success=False)
        self.assertFalse((self.local / "unexpected").exists())
        self.resume(pending, "git", "commit", "--allow-empty", "-m", "final phase")
        self.assertEqual(self.git("rev-parse", "HEAD^"), pending)
        self.assertEqual(self.remote_head(), self.head())
        self.assertEqual(unrelated.read_text(), "other work\n")
        self.assertFalse(self.lock.exists())

    def test_resume_rejects_unexpected_local_and_remote_before_command(self):
        pending = self.partial()
        self.resume(pending[:12], "touch", "unexpected", success=False)
        self.resume(pending, "touch", "unexpected", baseline=pending, success=False)
        self.assertFalse((self.local / "unexpected").exists())
        self.assert_unpublished(pending)

    def test_resume_requires_pending_work_and_an_appended_commit(self):
        self.resume(self.base, "touch", "unexpected", success=False)
        self.assertFalse((self.local / "unexpected").exists())
        pending = self.partial()
        self.resume(pending, "true", success=False)
        self.assert_unpublished(pending)
        self.resume(pending, "git", "commit", "--amend", "--allow-empty", "-m", "rewrite", success=False)
        self.assertEqual(self.git("rev-parse", "HEAD^"), self.base)
        self.assertEqual(self.remote_head(), self.base)
        self.assertFalse(self.lock.exists())

    def test_resume_failure_preserves_new_local_commit_for_review(self):
        pending = self.partial()
        result = self.resume(
            pending, "bash", "-ec", "git commit --allow-empty -m 'remaining phase'; exit 9", success=False,
        )
        self.assertEqual(result.returncode, 9)
        self.assertEqual(self.git("rev-parse", "HEAD^"), pending)
        self.assertEqual(self.remote_head(), self.base)
        self.assertFalse(self.lock.exists())

    def test_resume_does_not_take_an_existing_lock(self):
        pending = self.partial()
        self.lock.mkdir()
        owner = self.lock / "owner"
        owner.write_text("other publisher\n")
        self.resume(pending, "touch", "unexpected", success=False)
        self.assertFalse((self.local / "unexpected").exists())
        self.assertEqual(owner.read_text(), "other publisher\n")
        self.assertEqual(self.remote_head(), self.base)

    def test_failed_fetch_cannot_use_stale_fetch_head(self):
        pending = self.partial()
        self.git("remote", "set-url", "origin", str(self.root / "missing.git"))
        self.resume(pending, "touch", "unexpected", success=False)
        self.assertFalse((self.local / "unexpected").exists())
        self.assert_unpublished(pending)

    def test_resume_rechecks_remote_after_running_the_command(self):
        pending = self.partial()
        result = self.resume(
            pending, "bash", "-ec",
            'git --git-dir="$1" fetch "$PWD" HEAD\n'
            'git --git-dir="$1" update-ref refs/heads/main FETCH_HEAD "$2"\n'
            "git commit --allow-empty -m 'final phase'",
            "publication-test", str(self.remote), self.base, success=False,
        )
        self.assertIn("远端 main 在本轮验证期间已更新", result.stderr)
        self.assertEqual(self.git("rev-parse", "HEAD^"), pending)
        self.assertEqual(self.remote_head(), pending)
        self.assertFalse(self.lock.exists())

    def test_push_hook_rejects_a_changed_advertised_baseline(self):
        pending = self.partial()
        target = self.root / "alternate.git"
        self.git("clone", "--bare", str(self.local), str(target))
        self.assertEqual(self.remote_head(target), pending)
        self.git("remote", "set-url", "--push", "origin", str(target))
        result = self.resume(pending, "git", "commit", "--allow-empty", "-m", "final phase", success=False)
        self.assertIn("推送握手时远端 main 基线已变化", result.stderr)
        self.assertEqual(self.git("rev-parse", "HEAD^"), pending)
        self.assertEqual(self.remote_head(target), pending)
        self.assertEqual(self.remote_head(), self.base)
        self.assertFalse(self.lock.exists())


if __name__ == "__main__":
    unittest.main()
