#!/usr/bin/env bash
# ADDP_T2_HOSTED_ONLY=nfs
# ADDP_T2_NATIVE_LIFECYCLE=scripts/utils/online_nfs_fixture.py
# ADDP_T2_INPUT_FILES=common/engine/plugins/nfs/ common/engine/plugin/ scripts/utils/online_nfs_fixture.py
# This disposable native NFS export is owned by an exclusive Hosted Linux job.
set -euo pipefail
[[ ${GITHUB_ACTIONS:-} == true && ${RUNNER_OS:-} == Linux && ${RUNNER_ENVIRONMENT:-} == github-hosted && $(uname -sm) == 'Linux x86_64' ]] || {
    echo 'NFS publication integration requires an exclusive GitHub Hosted Linux x86_64 runner' >&2; exit 1;
}
[[ -n ${GITHUB_RUN_ID:-} && -n ${GITHUB_RUN_ATTEMPT:-} ]] || exit 1
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
cd "$ROOT_DIR"
export ADDP_ONLINE_HOST=1 ADDP_ONLINE_HOSTED=1
export ADDP_ONLINE_TEST_RUN_ID="nfs-publication-${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}"
ADDP_ONLINE_SECRET_DIR=$(mktemp -d /tmp/addp-nfs-publication-secret.XXXXXX)
chmod 0700 "$ADDP_ONLINE_SECRET_DIR"
export ADDP_ONLINE_SECRET_DIR
ARTIFACT_DIR=${ADDP_NFS_PUBLICATION_ARTIFACT_DIR:-$(mktemp -d /tmp/addp-nfs-publication-evidence.XXXXXX)}
mkdir -p "$ARTIFACT_DIR"
fixture() {
    python3 - "$1" <<'PY'
import json,os,subprocess,sys
from scripts.utils.online_nfs_fixture import NFSExport
def command(args,**kwargs):
    return subprocess.run(args,check=True,text=True,capture_output=True,**kwargs).stdout.strip()
fixture=NFSExport('nfs-publication',command)
secret=fixture.guard(os.environ)
result=fixture.lifecycle(sys.argv[1],secret,os.environ['ADDP_ONLINE_TEST_RUN_ID'])
if sys.argv[1]=='start':
    root=fixture.owned_root(secret,os.environ['ADDP_ONLINE_TEST_RUN_ID'])
    (root/'private').mkdir(mode=0o700)
print(json.dumps(result))
PY
}
cleanup() {
    local status=$?
    trap - EXIT INT TERM
    fixture stop > "$ARTIFACT_DIR/cleanup.json" 2> "$ARTIFACT_DIR/cleanup-error.log" || status=1
    printf 'exit_code=%s\n' "$status" > "$ARTIFACT_DIR/result.txt"
    if [[ $status == 0 ]]; then rm -rf -- "$ADDP_ONLINE_SECRET_DIR"; fi
    exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
fixture start > "$ARTIFACT_DIR/start.json"
ADDP_NFS_TEST_EXPORT=$(cat "$ADDP_ONLINE_SECRET_DIR/nfs-publication-root")
export ADDP_NFS_TEST_EXPORT ADDP_NFS_PUBLICATION_INTEGRATION=1
export ADDP_NFS_PUBLICATION_ARTIFACT_DIR="$ARTIFACT_DIR"
(cd common && GOWORK=off go test ./engine/plugins/nfs -run '^TestIntegrationNFSPublication' -count=1 -v) 2>&1 | tee "$ARTIFACT_DIR/provider.log"
! grep -q -- '--- SKIP:' "$ARTIFACT_DIR/provider.log"
for test_name in CompleteFileAndPrivateNamespace LostReplyRecovery ConcurrentNoOverwrite PreparationUnknownRetainsResidue; do
    grep -q -- "--- PASS: TestIntegrationNFSPublication${test_name} " "$ARTIFACT_DIR/provider.log"
done
