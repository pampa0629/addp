#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
source "$root/scripts/lib/kingbase-official-media.sh"
image_config=10ba6f33e228ddd6be155e80523afe712be333ee25ccf9f912ee3a31899dc82f
image="addp-k3s.local/site/kingbase:$image_config"

usage() {
    cat <<'HELP'
用法：
  k3s-kingbase-trial.sh load-node <官方 AMD64 Docker tar>
  k3s-kingbase-trial.sh render <namespace> <hostname-label> <secret>
  k3s-kingbase-trial.sh initialize <namespace>
  k3s-kingbase-trial.sh verify <namespace>
HELP
}
dns_label() { [[ "$1" =~ ^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$ ]]; }
dns_subdomain() {
    [[ ${#1} -le $2 && "$1" =~ ^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$ ]]
}

case "${1:-}" in
    load-node)
        [[ $# == 2 && "$(uname -m)" == x86_64 ]] || { usage >&2; exit 2; }
        kingbase_verify_sha256 "$KINGBASE_OFFICIAL_MEDIA_SHA256" "$2" || {
            printf '%s\n' '金仓官方介质 SHA-256 不匹配。' >&2
            exit 1
        }
        k3s ctr -n k8s.io images import --platform linux/amd64 "$2" >&2
        source_image="docker.io/library/$KINGBASE_OFFICIAL_SOURCE_IMAGE"
        digest=$(k3s crictl inspecti "$source_image" | python3 -c 'import json, sys; print(json.load(sys.stdin)["status"]["id"])')
        [[ "$digest" == "sha256:$image_config" ]] || {
            printf '%s\n' '金仓镜像原生配置摘要不匹配。' >&2
            exit 1
        }
        k3s ctr -n k8s.io images tag --force "$source_image" "$image" >&2
        printf '%s\n' "$image"
        ;;
    render)
        [[ $# == 4 ]] && dns_label "$2" && dns_subdomain "$3" 63 && dns_subdomain "$4" 253 || { usage >&2; exit 2; }
        cat <<YAML
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: kingbase
  namespace: "$2"
spec:
  storageClassName: local-path
  accessModes: [ReadWriteOnce]
  resources:
    requests:
      storage: 5Gi
---
apiVersion: v1
kind: Service
metadata:
  name: kingbase
  namespace: "$2"
spec:
  selector:
    app.kubernetes.io/name: kingbase
  ports:
    - name: kingbase
      port: 54321
      targetPort: kingbase
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: kingbase
  namespace: "$2"
spec:
  replicas: 1
  strategy:
    type: Recreate
  selector:
    matchLabels:
      app.kubernetes.io/name: kingbase
  template:
    metadata:
      labels:
        app.kubernetes.io/name: kingbase
        app.kubernetes.io/part-of: addp-business
      annotations:
        addp.io/license-purpose: vendor-trial-technical-evaluation
    spec:
      hostname: kingbase
      nodeSelector:
        kubernetes.io/arch: amd64
        kubernetes.io/hostname: "$3"
      automountServiceAccountToken: false
      enableServiceLinks: false
      terminationGracePeriodSeconds: 60
      volumes:
        - name: data
          persistentVolumeClaim:
            claimName: kingbase
      containers:
        - name: kingbase
          image: $image
          imagePullPolicy: Never
          env:
            - name: DB_MODE
              value: pg
            - name: DB_USER
              value: system
            - name: DB_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: "$4"
                  key: DB_PASSWORD
            - name: ADDP_READER_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: "$4"
                  key: ADDP_READER_PASSWORD
          ports:
            - name: kingbase
              containerPort: 54321
          resources:
            requests:
              cpu: 150m
              memory: 512Mi
            limits:
              memory: 1536Mi
          volumeMounts:
            - name: data
              mountPath: /home/kingbase/userdata
          readinessProbe:
            exec:
              command:
                - /bin/bash
                - -ec
                - |
                  export KINGBASE_PASSWORD="\$DB_PASSWORD"
                  exec $KINGBASE_HOME/bin/ksql -h 127.0.0.1 -U system -d kingbase -p $KINGBASE_DATABASE_PORT -At -c 'SELECT 1'
            timeoutSeconds: 5
            periodSeconds: 10
          lifecycle:
            preStop:
              exec:
                command: ["$KINGBASE_HOME/bin/sys_ctl", "-D", "/home/kingbase/userdata/data", "-m", "fast", "-w", "-t", "45", "stop"]
YAML
        ;;
    initialize|verify)
        [[ $# == 2 ]] && dns_label "$2" || { usage >&2; exit 2; }
        purpose=$(kubectl -n "$2" get deployment kingbase -o 'go-template={{index .spec.template.metadata.annotations "addp.io/license-purpose"}}')
        [[ "$purpose" == vendor-trial-technical-evaluation ]] || {
            printf '%s\n' '目标 Deployment 未声明为金仓试用夹具。' >&2
            exit 1
        }
        kubectl -n "$2" rollout status deployment/kingbase --timeout=180s
        {
            cat <<'POD'
set -euo pipefail
client=/home/kingbase/install/kingbase/bin/ksql
admin() { KINGBASE_PASSWORD="$DB_PASSWORD" "$client" -X -h 127.0.0.1 -U system -p 54321 -v ON_ERROR_STOP=1 "$@"; }
reader() { KINGBASE_PASSWORD="$ADDP_READER_PASSWORD" "$client" -X -h 127.0.0.1 -U addp_reader -d business -p 54321 -v ON_ERROR_STOP=1 "$@"; }
days=$(admin -d kingbase -Atc 'SELECT get_license_validdays()')
[[ "$days" =~ ^[0-9]+$ ]] && (( days > 0 )) || { echo '金仓试用许可已失效或无法核验。' >&2; exit 1; }
printf 'KingbaseES trial remaining days: %s\n' "$days"
POD
            if [[ "$1" == initialize ]]; then
                cat <<'POD'
[[ "$ADDP_READER_PASSWORD" =~ ^[a-zA-Z0-9]{20,64}$ && "$ADDP_READER_PASSWORD" != "$DB_PASSWORD" ]] || {
    echo '只读账号密码须为独立的 20–64 位字母数字组合。' >&2
    exit 1
}
if [[ $(admin -d kingbase -Atc "SELECT 1 FROM pg_database WHERE datname='business'") != 1 ]]; then
    admin -d kingbase -c 'CREATE DATABASE business' >/dev/null
fi
if [[ -z $(admin -d business -Atc "SELECT to_regclass('public.addp_engine_probe')") ]]; then
    admin -d business -1 -f - >/dev/null <<'KINGBASE_SAMPLE'
POD
                cat "$root/business/kingbase/init.sql"
                cat <<'POD'
KINGBASE_SAMPLE
fi
if [[ $(admin -d kingbase -Atc "SELECT 1 FROM pg_roles WHERE rolname='addp_reader'") != 1 ]]; then
    printf "CREATE ROLE addp_reader LOGIN PASSWORD '%s';\n" "$ADDP_READER_PASSWORD" | admin -d kingbase >/dev/null
fi
admin -d business -c 'GRANT CONNECT ON DATABASE business TO addp_reader; GRANT USAGE ON SCHEMA public TO addp_reader; GRANT SELECT ON addp_engine_probe, customers, products, orders, order_items TO addp_reader' >/dev/null
POD
            fi
            cat <<'POD'
[[ $(reader -Atc 'SELECT engine_name FROM addp_engine_probe WHERE id=1') == 'KingbaseES V9R1C10' ]]
[[ $(reader -Atc "SELECT NOT has_table_privilege(current_user,'public.orders','INSERT')") == t ]]
reader -Atc "SELECT 'customers='||count(*) FROM customers UNION ALL SELECT 'products='||count(*) FROM products UNION ALL SELECT 'orders='||count(*) FROM orders UNION ALL SELECT 'order_items='||count(*) FROM order_items"
POD
        } | kubectl -n "$2" exec -i deployment/kingbase -- /bin/bash -s
        ;;
    --help|-h) usage ;;
    *) usage >&2; exit 2 ;;
esac
