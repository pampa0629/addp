import os
from pathlib import Path
import subprocess
import tempfile
import unittest


class NFSHostedGuardTest(unittest.TestCase):
    def test_refuses_non_hosted_before_any_lifecycle_action(self):
        script = Path(__file__).with_name('common-nfs-publication-gate.sh')
        for actions, runner_os, uname, runner_environment in [('false', 'Linux', 'Linux x86_64', 'github-hosted'), ('true', 'Darwin', 'Linux x86_64', 'github-hosted'), ('true', 'Linux', 'Darwin arm64', 'github-hosted'), ('true', 'Linux', 'Linux x86_64', 'self-hosted')]:
            with self.subTest(actions=actions, runner_os=runner_os, uname=uname), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                marker = root / 'lifecycle-started'
                for name, content in {
                    'uname': '#!/bin/sh\nprintf "%s\\n" "$FAKE_NFS_UNAME"\n',
                    'mktemp': '#!/bin/sh\ntouch "$NFS_GUARD_MARKER"\nexit 99\n',
                }.items():
                    path = root / name
                    path.write_text(content)
                    path.chmod(0o700)
                env = dict(os.environ, PATH=str(root) + os.pathsep + os.environ['PATH'], GITHUB_ACTIONS=actions,
                           RUNNER_OS=runner_os, FAKE_NFS_UNAME=uname, NFS_GUARD_MARKER=str(marker),
                           RUNNER_ENVIRONMENT=runner_environment,
                           GITHUB_RUN_ID='123', GITHUB_RUN_ATTEMPT='1')
                result = subprocess.run(['/bin/bash', str(script)], env=env, text=True, capture_output=True)
                self.assertEqual(result.returncode, 1, result.stderr)
                self.assertIn('requires an exclusive GitHub Hosted Linux', result.stderr)
                self.assertFalse(marker.exists(), 'created fixture state before admission')
