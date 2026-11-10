#!/usr/bin/env bash
set -euo pipefail

if [[ $# -eq 1 ]]; then
    case "$1" in
        check|up|status|down|recover)
            script_dir="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
            exec python3 "$script_dir/metrics-sources.py" "$1" --scope host --kind node
            ;;
        -h|--help)
            printf '用法：%s {check|up|status|down|recover}\n' "$0"
            exit 0
            ;;
    esac
fi

printf '用法：%s {check|up|status|down|recover}\n' "$0" >&2
exit 2
