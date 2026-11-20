#!/usr/bin/env bash
# Design fixture on a fresh container owned by common-mysql-data-protection-gate.
set -euo pipefail
container=$1
work=$2
runtime_password=$(openssl rand -hex 24)
worker_password=$(openssl rand -hex 24)
bootstrap_password=$(openssl rand -hex 24)
admin() {
    docker exec -i "$container" sh -ec 'export MYSQL_PWD="$MYSQL_ROOT_PASSWORD"; exec mysql --protocol=TCP -h127.0.0.1 -uroot -NB system'
}
admin <<SQL
CREATE USER 'grant_owner_gate'@'127.0.0.1' ACCOUNT LOCK;
CREATE USER 'system_gate'@'127.0.0.1' IDENTIFIED BY '$runtime_password';
CREATE USER 'worker_gate'@'127.0.0.1' IDENTIFIED BY '$worker_password';
CREATE USER 'bootstrap_gate'@'127.0.0.1' IDENTIFIED BY '$bootstrap_password';
SQL
admin <<'SQL'
CREATE TABLE sample_grant (
 request_id INT PRIMARY KEY, tenant INT NOT NULL, origin VARCHAR(24) NOT NULL,
 actor INT NOT NULL, member INT NOT NULL, granted_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 UNIQUE(tenant,origin,request_id,actor,member)
) ENGINE=InnoDB;
CREATE TABLE sample_outcome (
 tenant INT NOT NULL, origin VARCHAR(24) NOT NULL, source_key VARCHAR(64) NOT NULL, result_key VARCHAR(64) NOT NULL,
 request_id INT, actor INT NOT NULL, member INT NOT NULL, recorded_at DATETIME(6) NOT NULL, denial VARCHAR(40),
 PRIMARY KEY(tenant,origin,source_key,result_key), UNIQUE(request_id),
 CHECK ((request_id IS NULL) = (denial IS NOT NULL)),
 CHECK (denial IS NULL OR
   (origin='task_production' AND denial IN ('production_ineligible','source_access_denied','execution_permission_denied','target_history_exists','engine_changed')) OR
   (origin='upload_creation' AND denial IN ('not_new','boundary_ineligible','target_history_exists','uploader_inactive','engine_changed'))),
 FOREIGN KEY(tenant,origin,request_id,actor,member) REFERENCES sample_grant(tenant,origin,request_id,actor,member)
) ENGINE=InnoDB;
CREATE TABLE sample_audit (id INT PRIMARY KEY) ENGINE=InnoDB;
DELIMITER //
CREATE DEFINER='grant_owner_gate'@'127.0.0.1' PROCEDURE persist_source(
 IN p_origin VARCHAR(24), IN p_tenant INT, IN p_source VARCHAR(64), IN p_result VARCHAR(64),
 IN p_request INT, IN p_actor INT, IN p_member INT, IN p_recorded DATETIME(6), IN p_denial VARCHAR(40))
SQL SECURITY DEFINER
BEGIN
 -- Outside a transaction the savepoint disappears. Reject before either insert.
 SAVEPOINT addp_system_source_pair;
 ROLLBACK TO SAVEPOINT addp_system_source_pair;
 BEGIN
  DECLARE EXIT HANDLER FOR SQLEXCEPTION
  BEGIN ROLLBACK TO SAVEPOINT addp_system_source_pair; RELEASE SAVEPOINT addp_system_source_pair; RESIGNAL; END;
  IF p_origin IN ('task_production','upload_creation') THEN
   IF p_source IS NULL OR p_result IS NULL OR (p_request IS NULL) = (p_denial IS NULL) THEN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='invalid first result';
   END IF;
   IF p_request IS NOT NULL THEN
    INSERT INTO sample_grant(request_id,tenant,origin,actor,member) VALUES(p_request,p_tenant,p_origin,p_actor,p_member);
   END IF;
   INSERT INTO sample_outcome VALUES(p_tenant,p_origin,p_source,p_result,p_request,p_actor,p_member,p_recorded,p_denial);
  ELSEIF p_origin IN ('catalog','independent') AND p_request IS NOT NULL AND p_denial IS NULL
         AND p_source IS NULL AND p_result IS NULL THEN
   INSERT INTO sample_grant(request_id,tenant,origin,actor,member) VALUES(p_request,p_tenant,p_origin,p_actor,p_member);
  ELSE
   SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='invalid origin shape';
  END IF;
 END;
 RELEASE SAVEPOINT addp_system_source_pair;
END//
DELIMITER ;
GRANT INSERT ON system.sample_grant TO 'grant_owner_gate'@'127.0.0.1';
GRANT INSERT ON system.sample_outcome TO 'grant_owner_gate'@'127.0.0.1';
GRANT EXECUTE ON PROCEDURE system.persist_source TO 'grant_owner_gate'@'127.0.0.1','system_gate'@'127.0.0.1';
GRANT SELECT ON system.sample_grant TO 'system_gate'@'127.0.0.1';
GRANT SELECT ON system.sample_outcome TO 'system_gate'@'127.0.0.1';
GRANT INSERT ON system.sample_audit TO 'system_gate'@'127.0.0.1','bootstrap_gate'@'127.0.0.1';
SQL
caller() {
    local user=$1
    shift
    local password=$runtime_password
    case "$user" in
        worker_gate) password=$worker_password ;;
        bootstrap_gate) password=$bootstrap_password ;;
        system_gate) ;;
        *) return 2 ;;
    esac
    MYSQL_PWD="$password" docker exec -i -e MYSQL_PWD "$container" mysql --protocol=TCP -h127.0.0.1 -u"$user" -NB "$@"
}

# --force keeps the same connection for the explicit post-error COMMIT or ROLLBACK.
expect_error() {
    local user=$1 code=$2 statements=$3
    caller "$user" --force <<<"$statements" >"$work/boundary.out" 2>"$work/boundary.err" || true
    test "$(grep -c '^ERROR ' "$work/boundary.err")" = 1
    grep -q "^ERROR $code " "$work/boundary.err"
}
test "$(admin <<<"SELECT CONCAT(SECURITY_TYPE, '/', DEFINER) FROM information_schema.routines WHERE routine_schema='system' AND routine_name='persist_source'")" = 'DEFINER/grant_owner_gate@127.0.0.1'
test "$(admin <<<"SELECT account_locked FROM mysql.user WHERE user='grant_owner_gate' AND host='127.0.0.1'")" = Y
# Check the complete grants, including role membership and grant options. These
# accounts have no inherited database-wide or global privileges.
grants_for() {
    admin <<<"SHOW GRANTS FOR '$1'@'127.0.0.1'" | sed 's/`//g' | LC_ALL=C sort
}
check_grants() {
    local user=$1
    shift
    local principal="$user@127.0.0.1"
    {
        echo "GRANT USAGE ON *.* TO $principal"
        for grant in "$@"; do echo "GRANT $grant TO $principal"; done
    } | LC_ALL=C sort >"$work/expected-grants"
    grants_for "$user" >"$work/actual-grants"
    diff -u "$work/expected-grants" "$work/actual-grants"
}
check_grants system_gate 'EXECUTE ON PROCEDURE system.persist_source' 'SELECT ON system.sample_grant' 'SELECT ON system.sample_outcome' 'INSERT ON system.sample_audit'
check_grants grant_owner_gate 'EXECUTE ON PROCEDURE system.persist_source' 'INSERT ON system.sample_grant' 'INSERT ON system.sample_outcome'
check_grants worker_gate
check_grants bootstrap_gate 'INSERT ON system.sample_audit'
pair="CALL system.persist_source('task_production',7,'transfer/exec','result',11,21,31,'2026-01-02 03:04:05.123456',NULL)"
expect_error worker_gate 1370 "START TRANSACTION; $pair; ROLLBACK;"
expect_error bootstrap_gate 1370 "START TRANSACTION; $pair; ROLLBACK;"
expect_error system_gate 1305 "$pair;"
expect_error system_gate 1142 "INSERT INTO system.sample_grant(request_id,tenant,origin,actor,member) VALUES(90,7,'independent',21,31);"
expect_error system_gate 1142 "INSERT INTO system.sample_outcome VALUES(7,'upload_creation','u','fp',NULL,21,31,NOW(6),'not_new');"
test "$(admin <<<"SELECT (SELECT COUNT(*) FROM sample_grant)+(SELECT COUNT(*) FROM sample_outcome)")" = 0
caller system_gate <<SQL
START TRANSACTION;
$pair;
CALL system.persist_source('upload_creation',7,'upload','fp',12,21,31,NOW(6),NULL);
CALL system.persist_source('task_production',7,'transfer/exec','refused',NULL,21,31,NOW(6),'source_access_denied');
CALL system.persist_source('upload_creation',7,'upload','refused',NULL,21,31,NOW(6),'not_new');
CALL system.persist_source('catalog',7,NULL,NULL,13,21,31,NOW(6),NULL);
CALL system.persist_source('independent',7,NULL,NULL,14,21,31,NOW(6),NULL);
INSERT INTO system.sample_audit VALUES(1),(2),(3),(4),(5),(6);
COMMIT;
SQL
# A duplicate natural key fails the second insert. Keep the outer transaction
# and its prior write to show that the procedure rolled back only its own pair.
expect_error system_gate 1062 "START TRANSACTION; INSERT INTO system.sample_audit VALUES(7);
 CALL system.persist_source('task_production',7,'transfer/exec','result',15,21,31,NOW(6),NULL); COMMIT;"
expect_error system_gate 1062 "START TRANSACTION;
 CALL system.persist_source('upload_creation',7,'upload2','fp',16,21,31,NOW(6),NULL);
 INSERT INTO system.sample_audit VALUES(1); ROLLBACK;"
expect_error system_gate 1644 "START TRANSACTION; CALL system.persist_source('independent',7,'upload','fp',17,21,31,NOW(6),NULL); COMMIT;"
expect_error system_gate 3819 "START TRANSACTION; CALL system.persist_source('task_production',7,'transfer/exec','invalid',NULL,21,31,NOW(6),'not_new'); ROLLBACK;"
test "$(admin <<<"SELECT COUNT(*) FROM sample_grant")" = 4
test "$(admin <<<"SELECT COUNT(*) FROM sample_outcome WHERE request_id IS NOT NULL")" = 2
test "$(admin <<<"SELECT COUNT(*) FROM sample_outcome WHERE denial IS NOT NULL")" = 2
test "$(admin <<<"SELECT COUNT(*) FROM sample_outcome WHERE (origin='upload_creation' AND request_id=12) OR (origin='upload_creation' AND denial='not_new') OR (origin='task_production' AND denial='source_access_denied')")" = 3
test "$(admin <<<"SELECT COUNT(*) FROM sample_grant WHERE (origin='catalog' AND request_id=13) OR (origin='independent' AND request_id=14)")" = 2
test "$(admin <<<"SELECT COUNT(*) FROM sample_audit")" = 7
test "$(admin <<<"SELECT COUNT(*) FROM sample_outcome WHERE request_id=11 AND actor=21 AND member=31 AND recorded_at='2026-01-02 03:04:05.123456'")" = 1
echo 'MySQL source Grant permission model passed: 4 Grants, 2 issued + 2 denied results; transaction, writer and audit boundaries verified'
