#!/usr/bin/env bash
# Hosted-only NFSv3 export; never changes the developer's macOS NFS service.
set -euo pipefail
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
fail() { echo "Online Manager NFS fixture failed: $*" >&2; exit 1; }
[ "${ADDP_ONLINE_HOST:-}" = 1 ] && [ "${ADDP_ONLINE_HOSTED:-}" = 1 ] &&
  [ "${GITHUB_ACTIONS:-}" = true ] && [ "${RUNNER_OS:-}" = Linux ] &&
  [ "$(uname -s)" = Linux ] && [ "$(uname -m)" = x86_64 ] || fail 'requires an exclusive GitHub Hosted Linux x86_64 Runner'
case "${1:-}" in start|stop) ;; *) fail 'usage: start|stop' ;; esac
[ "$#" = 1 ] || fail 'exactly one action is required'
[[ "${ADDP_ONLINE_TEST_RUN_ID:-}" =~ ^manager-[0-9]+-[0-9]+$ ]] || fail 'requires the Hosted Manager run identity'
[ -d "${ADDP_ONLINE_SECRET_DIR:-}" ] && [ ! -L "$ADDP_ONLINE_SECRET_DIR" ] || fail 'private fixture state directory is missing'
STATE="$ADDP_ONLINE_SECRET_DIR/nfs-root"
EXPORT="/etc/exports.d/addp-${ADDP_ONLINE_TEST_RUN_ID}.exports"

case "$1" in
  start)
    [ ! -e "$STATE" ] && [ ! -e "$EXPORT" ] || fail 'fixture state already exists'
    existing=$(sudo exportfs -s)
    [ -z "$existing" ] || fail 'Runner has pre-existing NFS exports'
    root=$(mktemp -d /tmp/addp-online-manager-nfs.XXXXXX)
    printf '%s\n' "$root" > "$STATE"
    chmod 600 "$STATE"
    printf '%s\n' "$ADDP_ONLINE_TEST_RUN_ID" > "$root/.addp-online-owner"
    python3 "$SCRIPT_DIR/online-content-boundary-fixture.py" "$root"
    chmod -R a+rX "$root"
    sudo mkdir -p /etc/exports.d
    printf '%s 127.0.0.1(ro,sync,no_subtree_check,insecure,root_squash)\n' "$root" | sudo tee "$EXPORT" >/dev/null
    sudo systemctl start rpcbind.service nfs-kernel-server.service
    sudo exportfs -ra
    showmount -e 127.0.0.1 | awk '{print $1}' | grep -Fx "$root" >/dev/null || fail 'export is not discoverable'
    python3 - "$root" <<'PY'
import json, os, sys
from pathlib import Path
versions = Path(sys.argv[1]) / 'index-source-versions'
versions.mkdir(mode=0o755)
versions.chmod(0o755)
for suffix, separator in (('csv', ','), ('tsv', '\t')):
    source = versions / ('source.' + suffix)
    source.write_bytes(('id,name\n1,Alice\n2,Bobby\n').replace(',', separator).encode('ascii'))
    source.chmod(0o644)
path = Path(os.environ['ADDP_ONLINE_SECRET_DIR']) / 'manager-nfs-engine.json'
with path.open('x') as stream:
    path.chmod(0o600)
    json.dump({'name': 'Hosted Manager NFS', 'engine_type': 'nfs', 'engine_origin': 'general',
               'description': 'Disposable content boundary T4 source',
               'connection_info': {'server': '127.0.0.1', 'export_path': sys.argv[1]}}, stream)
PY
    ;;
  stop)
    if [ -f "$STATE" ] && [ ! -L "$STATE" ]; then
      root=$(cat "$STATE")
      [[ "$root" =~ ^/tmp/addp-online-manager-nfs\.[A-Za-z0-9]{6}$ ]] && [ ! -L "$root" ] &&
        [ "$(cat "$root/.addp-online-owner")" = "$ADDP_ONLINE_TEST_RUN_ID" ] || fail 'fixture root ownership mismatch'
      # Removing only our export also handles partially completed startup.
      sudo rm -f "$EXPORT"
      sudo exportfs -ra
      remaining=$(sudo exportfs -s)
      [ -z "$remaining" ] || fail 'NFS export cleanup has residuals'
      sudo systemctl stop nfs-kernel-server.service rpcbind.service rpcbind.socket
      rm -rf "$root"
      rm -f "$STATE" "$ADDP_ONLINE_SECRET_DIR/manager-nfs-engine.json"
      [ ! -e "$root" ] && [ ! -e "$EXPORT" ] || fail 'NFS fixture cleanup has residuals'
    elif [ -e "$STATE" ] || [ -e "$EXPORT" ]; then
      fail 'invalid fixture state'
    fi
    ;;
esac
