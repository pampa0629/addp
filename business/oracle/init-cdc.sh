#!/usr/bin/env bash
set -euo pipefail

CONTAINER="${ORACLE_CONTAINER:-business-oracle}"
SYS_PASSWORD="${ORACLE_SYS_PASSWORD:-oracle_sys_password}"
CDC_USER="${ORACLE_CDC_USER:-C##ADDP_CDC}"
CDC_PASSWORD="${ORACLE_CDC_PASSWORD:-addp_oracle_cdc_password}"
CDB_NAME="${ORACLE_CDC_DATABASE_NAME:-FREE}"
PDB_NAME="${ORACLE_SERVICE_NAME:-FREEPDB1}"

if [[ ! "${CDC_USER}" =~ ^C##[A-Za-z0-9_$#]+$ ]]; then
  echo "Oracle CDC 用户必须是合法 common user: ${CDC_USER}" >&2
  exit 1
fi
if [[ "${CDC_PASSWORD}" == *'"'* || "${CDC_PASSWORD}" == *$'\n'* || "${CDC_PASSWORD}" == *$'\r'* ]]; then
  echo "Oracle CDC 密码不能包含双引号或换行" >&2
  exit 1
fi

sqlplus_sys() {
  docker exec -i "${CONTAINER}" sqlplus -s "sys/${SYS_PASSWORD}@//localhost:1521/${CDB_NAME} as sysdba"
}

log_mode="$(sqlplus_sys <<'SQL'
set heading off feedback off pagesize 0 verify off echo off
select trim(log_mode) from v$database;
exit
SQL
)"
log_mode="$(printf '%s' "${log_mode}" | tr -d '[:space:]')"

if [[ "${log_mode}" != "ARCHIVELOG" ]]; then
  docker exec -i "${CONTAINER}" sqlplus -s "/ as sysdba" <<'SQL'
WHENEVER SQLERROR EXIT SQL.SQLCODE
shutdown immediate;
startup mount;
alter database archivelog;
alter database open;
exit
SQL
fi

sqlplus_sys <<SQL
WHENEVER SQLERROR EXIT SQL.SQLCODE
set verify off

DECLARE
  force_logging_state VARCHAR2(3);
BEGIN
  SELECT force_logging INTO force_logging_state FROM v\$database;
  IF force_logging_state <> 'YES' THEN
    EXECUTE IMMEDIATE 'ALTER DATABASE FORCE LOGGING';
  END IF;
END;
/

DECLARE
  supplemental_min VARCHAR2(3);
BEGIN
  SELECT supplemental_log_data_min INTO supplemental_min FROM v\$database;
  IF supplemental_min <> 'YES' THEN
    EXECUTE IMMEDIATE 'ALTER DATABASE ADD SUPPLEMENTAL LOG DATA';
  END IF;
END;
/

DECLARE
  user_count NUMBER;
BEGIN
  SELECT COUNT(*) INTO user_count FROM cdb_users WHERE username = UPPER('${CDC_USER}') AND con_id = 1;
  IF user_count = 0 THEN
    EXECUTE IMMEDIATE 'CREATE USER ${CDC_USER} IDENTIFIED BY "${CDC_PASSWORD}" CONTAINER=ALL';
  ELSE
    EXECUTE IMMEDIATE 'ALTER USER ${CDC_USER} IDENTIFIED BY "${CDC_PASSWORD}" CONTAINER=ALL';
  END IF;
END;
/

GRANT CREATE SESSION, SET CONTAINER, SELECT ANY TABLE, FLASHBACK ANY TABLE,
      SELECT ANY TRANSACTION, LOGMINING, CREATE TABLE, UNLIMITED TABLESPACE,
      LOCK ANY TABLE TO ${CDC_USER} CONTAINER=ALL;
GRANT SELECT_CATALOG_ROLE, EXECUTE_CATALOG_ROLE TO ${CDC_USER} CONTAINER=ALL;
GRANT EXECUTE ON DBMS_LOGMNR TO ${CDC_USER} CONTAINER=ALL;
GRANT EXECUTE ON DBMS_LOGMNR_D TO ${CDC_USER} CONTAINER=ALL;
GRANT SELECT ON V_\$DATABASE TO ${CDC_USER} CONTAINER=ALL;
GRANT SELECT ON V_\$LOG TO ${CDC_USER} CONTAINER=ALL;
GRANT SELECT ON V_\$LOG_HISTORY TO ${CDC_USER} CONTAINER=ALL;
GRANT SELECT ON V_\$LOGMNR_LOGS TO ${CDC_USER} CONTAINER=ALL;
GRANT SELECT ON V_\$LOGMNR_CONTENTS TO ${CDC_USER} CONTAINER=ALL;
GRANT SELECT ON V_\$LOGMNR_PARAMETERS TO ${CDC_USER} CONTAINER=ALL;
GRANT SELECT ON V_\$LOGFILE TO ${CDC_USER} CONTAINER=ALL;
GRANT SELECT ON V_\$ARCHIVED_LOG TO ${CDC_USER} CONTAINER=ALL;
GRANT SELECT ON V_\$ARCHIVE_DEST_STATUS TO ${CDC_USER} CONTAINER=ALL;
GRANT SELECT ON V_\$TRANSACTION TO ${CDC_USER} CONTAINER=ALL;
ALTER SYSTEM SWITCH LOGFILE;
exit
SQL

docker exec -i "${CONTAINER}" sqlplus -s "${CDC_USER}/${CDC_PASSWORD}@//localhost:1521/${PDB_NAME}" <<'SQL'
WHENEVER SQLERROR EXIT SQL.SQLCODE
set heading off feedback off pagesize 0
select 'ORACLE_CDC_READY' from dual;
exit
SQL
