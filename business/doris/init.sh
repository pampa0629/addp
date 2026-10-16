#!/usr/bin/env bash
# Single-FE/BE Docker Desktop entrypoint. Preserve the existing Backend identity.
set -eo pipefail

doris_sql() {
    mysql -h127.0.0.1 -P9030 -uroot --connect-timeout=5 --batch "$@"
}

doris_backend_facts() {
    # PROC is served by FE itself, before any Backend is registered/alive.
    doris_sql -e "SHOW PROC '/backends';" | awk -F '\t' '
        NR == 1 {
            for (i=1; i<=NF; i++) index_by_name[$i]=i
            if (!index_by_name["BackendId"] || !index_by_name["Host"] ||
                !index_by_name["HeartbeatPort"] || !index_by_name["Alive"]) exit 1
            next
        }
        { print $index_by_name["BackendId"] "\t" $index_by_name["Host"] "\t" \
                $index_by_name["HeartbeatPort"] "\t" $index_by_name["Alive"] }
        END { if (NR == 0) exit 1 }
    '
}

doris_health() (
    set -euo pipefail
    local facts
    doris_sql -e 'SHOW FRONTENDS;' >/dev/null
    facts=$(doris_backend_facts)
    [[ "$facts" =~ ^[0-9]+$'\t'host\.docker\.internal$'\t'9050$'\t'true$ ]]
)

doris_register_backend() (
    set -euo pipefail
    local facts='' ready=false backend_id host heartbeat alive after_id after_host after_port after_alive
    for _ in {1..300}; do
        if facts=$(doris_backend_facts); then ready=true; break; fi
        sleep 1
    done
    [ "$ready" = true ] || { echo 'Doris FE did not become queryable' >&2; exit 1; }
    if [ -z "$facts" ]; then
        doris_sql -e 'ALTER SYSTEM ADD BACKEND "host.docker.internal:9050";'
    else
        [[ "$facts" != *$'\n'* ]] || { echo 'Expected exactly one Business Doris Backend' >&2; exit 1; }
        IFS=$'\t' read -r backend_id host heartbeat alive <<< "$facts"
        [[ "$backend_id" =~ ^[0-9]+$ && "$heartbeat" = 9050 ]] || exit 1
        case "$host" in
            127.0.0.1)
                doris_sql -e 'ALTER SYSTEM MODIFY BACKEND "127.0.0.1:9050" HOSTNAME "host.docker.internal";'
                ;;
            host.docker.internal) ;;
            *) echo "Unexpected Business Doris Backend host: $host" >&2; exit 1 ;;
        esac
    fi
    facts=$(doris_backend_facts)
    [[ "$facts" != *$'\n'* ]] || exit 1
    IFS=$'\t' read -r after_id after_host after_port after_alive <<< "$facts"
    [[ "$after_id" =~ ^[0-9]+$ && "$after_host" = host.docker.internal && "$after_port" = 9050 ]] || exit 1
    [ -z "${backend_id:-}" ] || [ "$after_id" = "$backend_id" ] || {
        echo 'Business Doris Backend identity changed unexpectedly' >&2; exit 1;
    }
    echo "Doris Backend $after_id advertises host.docker.internal:9050"
)

if [[ "${BASH_SOURCE[0]}" = "$0" ]]; then
    if [ "${1:-}" = --health ]; then doris_health; exit $?; fi
    getent ahostsv4 host.docker.internal >/dev/null || {
        echo 'Docker Desktop host.docker.internal resolution is required' >&2; exit 1;
    }
    getent ahostsv4 host.docker.internal | awk '$1 != "127.0.0.1" { bad=1 } END { exit bad }' || {
        echo 'Doris must resolve its advertised hostname to its own loopback interface' >&2; exit 1;
    }
    sed -i '/^[[:space:]]*enable_fqdn_mode[[:space:]]*=/d; /^[[:space:]]*force_olap_table_replication_num[[:space:]]*=/d' /opt/apache-doris/fe/conf/fe.conf
    printf '\nenable_fqdn_mode = true\nforce_olap_table_replication_num = 1\n' >> /opt/apache-doris/fe/conf/fe.conf
    # Source the official lifecycle once; replace only its loopback registration.
    source /usr/local/bin/entry_point.sh
    register_be_to_fe() { doris_register_backend; }
    _main "$@"
fi
