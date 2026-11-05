"""Exercise the owner wrapper with a stand-in for the shared Hosted lifecycle."""

import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

GATE = Path(__file__).with_name('online-hosted-raster-focal-square-seven-gate.sh')
LIFECYCLE = r"""
set -euo pipefail
[ "$ONLINE_SUITE" = raster-focal-square-seven-persistence ]
[ "$RUNTIME_CONTAINER" = addp-raster-focal-square-seven-runtime ]
[ "${#HOSTED_FIXTURE_CONTAINERS[@]}" = 2 ]
[ "${#HOSTED_FIXTURE_IMAGES[@]}" = 2 ]
if [ "${1:-}" = --check-only ]; then
  printf '%s\n' readiness >> "$TRACE"
  exit 0
fi

export ADDP_ONLINE_SECRET_DIR="$PWD/secrets"
mkdir -p "$ADDP_ONLINE_SECRET_DIR"
IDENTITY_ENV="$ADDP_ONLINE_SECRET_DIR/identity.env"
ENGINE_RESULT_ENV="$ADDP_ONLINE_SECRET_DIR/engine.env"

trace() { printf '%s\n' "$*" >> "$TRACE"; }
docker() { return 1; }
curl() { return 0; }
run_daemon_launcher_logged() { trace "owner $*"; }
start_online_geopython_runtime() { trace "runtime $*"; }
run_logged() {
  local bounded=0
  if [ "$1" = timeout ]; then
    [ "$2" = --kill-after=5 ]
    python3 -c 'import sys; assert 0 < float(sys.argv[1]) < 600' "$3"
    shift 3
    bounded=1
  fi
  case "$*" in
    *online-raster-focal-square-seven-fixture.py\ start)
      printf '%s\n' "$PWD/owned-nfs" > "$ADDP_ONLINE_SECRET_DIR/raster-focal-square-seven-root"
      trace nfs-start ;;
    *online-raster-focal-square-seven-fixture.py\ stop)
      trace nfs-stop
      [ "${FAIL_PHASE:-}" != stop ] ;;
    *online-raster-focal-square-seven-fixture.py\ seed)
      [ "$bounded" = 1 ]
      [ "$ADDP_ONLINE_TEST_TIMEOUT_SECONDS" = 600 ]
      [ "$ADDP_ONLINE_TEST_RUN_ID" = raster-focal-square-seven-123-1 ]
      trace seed
      [ "${FAIL_PHASE:-}" != seed ] ;;
    *'--suite raster-workflow --wait-protection-owners develop,manager'*)
      [ "$bounded" = 1 ]
      printf 'export ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN=temporary\n' > "$IDENTITY_ENV"
      trace identity ;;
    *online-engine-registration.py*)
      [ "$bounded" = 1 ]
      [[ "$*" = *raster-focal-square-seven-nfs-engine.json* ]]
      printf 'export ADDP_ONLINE_CONSUMER_ENGINE_ID=15\n' > "$ENGINE_RESULT_ENV"
      trace engine ;;
    *online_raster_lineage.py\ prepare)
      [ "$bounded" = 1 ]
      [ "$ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN" = temporary ]
      trace observer
      [ "${FAIL_PHASE:-}" != observer ] ;;
    'env -u POSTGRES_PASSWORD make test-online ONLINE_SUITE=raster-focal-square-seven-persistence')
      [ "$bounded" = 1 ]
      [ "$ADDP_ONLINE_FOCAL_SQUARE_SEVEN_NFS_ENGINE_ID" = 15 ]
      [ -z "${ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN:-}" ]
      trace business
      [ "${FAIL_PHASE:-}" != business ] ;;
    docker\ *|bash\ scripts/infra/up.sh) ;;
    *) trace "unexpected $*"; return 1 ;;
  esac
}
finish() {
  local rc=$?
  if [ "${fixture_owned:-}" = 1 ]; then
    stop_online_fixture || rc=1
  fi
  rm -rf "$ADDP_ONLINE_SECRET_DIR"
  trace lifecycle-cleanup
  exit "$rc"
}
trap finish EXIT
"""


class HostedFocalSquareSevenGateTest(unittest.TestCase):
    def run_gate(self, *, check=False, failure=''):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'scripts/test').mkdir(parents=True)
            (root / 'scripts/utils').mkdir()
            gate = root / 'scripts/test' / GATE.name
            shutil.copy2(GATE, gate)
            (root / 'scripts/utils/hosted-online.sh').write_text(LIFECYCLE)
            trace = root / 'trace.txt'
            env = {**os.environ, 'TRACE': str(trace), 'FAIL_PHASE': failure, 'GITHUB_RUN_ID': '123',
                   'GITHUB_RUN_ATTEMPT': '1'}
            result = subprocess.run(['/bin/bash', str(gate), *(['--check-only'] if check else [])],
                                    cwd=root, env=env, capture_output=True, text=True, timeout=30)
            self.assertFalse((root / 'secrets').exists())
            return result, trace.read_text().splitlines() if trace.exists() else []

    def test_readiness_is_delegated_before_any_business(self):
        result, trace = self.run_gate(check=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(trace, ['readiness'])

    def test_product_runtime_initialization_and_scenario_share_the_same_budget(self):
        result, trace = self.run_gate()
        self.assertEqual(result.returncode, 0, result.stderr)
        runtime = next(step for step in trace if step.startswith('runtime '))
        self.assertIn('source=', runtime)
        self.assertIn('owned-nfs', runtime)
        self.assertEqual(trace[-7:], ['seed', 'identity', 'engine', 'observer', 'business', 'nfs-stop', 'lifecycle-cleanup'])

    def test_seed_observer_business_and_cleanup_failures_remain_failures(self):
        for phase in ('seed', 'observer', 'business', 'stop'):
            with self.subTest(phase=phase):
                result, trace = self.run_gate(failure=phase)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(trace[-2:], ['nfs-stop', 'lifecycle-cleanup'])
                self.assertEqual('business' in trace, phase in ('business', 'stop'))
