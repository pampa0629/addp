#!/usr/bin/env bash

set -euo pipefail

SPARK_CONTAINER="${SPARK_CONTAINER:-business-spark-master}"
SPARK_READY_FILE="${SPARK_READY_FILE:-/tmp/addp-spark-sample-ready}"
SPARK_INIT_LOCK="${SPARK_INIT_LOCK:-/tmp/addp-spark-sample-init.lock}"

if [ -x /opt/spark/bin/beeline ]; then
    BEELINE_URL="${BEELINE_URL:-jdbc:hive2://localhost:10000/default}"
    run_beeline() {
        /opt/spark/bin/beeline "$@"
    }
elif command -v docker >/dev/null 2>&1 && docker inspect "$SPARK_CONTAINER" >/dev/null 2>&1; then
    exec docker exec -i \
        -e BEELINE_URL="${BEELINE_URL:-jdbc:hive2://localhost:10000/default}" \
        -e SPARK_READY_FILE="$SPARK_READY_FILE" \
        -e SPARK_INIT_LOCK="$SPARK_INIT_LOCK" \
        "$SPARK_CONTAINER" \
        /opt/spark/conf/init-test-data.sh
else
    echo "找不到 Spark Beeline，也无法访问容器 ${SPARK_CONTAINER}" >&2
    exit 1
fi

lock_acquired=false
for _ in $(seq 1 90); do
    if mkdir "$SPARK_INIT_LOCK" 2>/dev/null; then
        lock_acquired=true
        break
    fi
    if [ -f "$SPARK_READY_FILE" ]; then
        echo "Spark 样例数据已由并发初始化任务验证"
        exit 0
    fi
    sleep 1
done
if [ "$lock_acquired" != true ]; then
    echo "无法取得 Spark 样例初始化锁" >&2
    exit 1
fi
cleanup_lock() {
    rmdir "$SPARK_INIT_LOCK" 2>/dev/null || true
}
trap cleanup_lock EXIT
rm -f "$SPARK_READY_FILE"

echo "等待 Spark Thrift Server 就绪..."
ready=false
for _ in $(seq 1 90); do
    if run_beeline -u "$BEELINE_URL" -n spark --silent=true -e "SELECT 1" >/dev/null 2>&1; then
        ready=true
        break
    fi
    sleep 1
done
if [ "$ready" != true ]; then
    echo "Spark Thrift Server 未在 90 秒内就绪" >&2
    exit 1
fi

echo "初始化 Spark 真实样例数据..."
run_beeline -u "$BEELINE_URL" -n spark --silent=true -f /dev/stdin >/dev/null <<'SQL'
CREATE OR REPLACE VIEW default.addp_sample_orders AS
SELECT *
FROM VALUES
    (1001, '华北', CAST(128.50 AS DECIMAL(10, 2)), DATE '2026-07-01'),
    (1002, '华东', CAST(356.20 AS DECIMAL(10, 2)), DATE '2026-07-02'),
    (1003, '华南', CAST(89.90 AS DECIMAL(10, 2)), DATE '2026-07-03'),
    (1004, '西南', CAST(612.00 AS DECIMAL(10, 2)), DATE '2026-07-04'),
    (1005, '东北', CAST(245.80 AS DECIMAL(10, 2)), DATE '2026-07-05')
AS orders(order_id, region, amount, ordered_on);
SQL

row_count=$(run_beeline \
    -u "$BEELINE_URL" \
    -n spark \
    --silent=true \
    --showHeader=false \
    --outputformat=csv2 \
    -e "SELECT COUNT(*) FROM default.addp_sample_orders" 2>/dev/null \
    | awk '/> [0-9]+$/ { value=$NF } END { print value }')

if [ "$row_count" != "5" ]; then
    echo "Spark 样例数据验证失败：期望 5 行，实际 ${row_count:-未知}" >&2
    exit 1
fi

touch "$SPARK_READY_FILE"

echo "Spark 样例数据已就绪：default.addp_sample_orders，共 ${row_count} 行"
