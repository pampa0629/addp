package capture

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/addp/common/datatype"
	oracleplugin "github.com/addp/common/engine/plugins/oracle"
	"github.com/addp/transfer/internal/models"
)

func ensureOracleSpatialOwnedResources(ctx context.Context, plan *CapturePlan, resource *models.CaptureResource) error {
	if plan == nil || resource == nil || resource.Oracle == nil {
		return fmt.Errorf("Oracle capture provisioning requires provider resources")
	}
	spatial := plan.SourceSpatialInfo != nil && len(plan.SourceSpatialInfo.GeometryColumns) > 0
	provider := resource.Oracle
	if !spatial {
		if provider.SpatialArtifactsOwned || provider.SpatialMirrorTableName != "" || provider.SpatialRowTriggerName != "" || provider.SpatialDDLGuardName != "" {
			return fmt.Errorf("Oracle non-spatial capture cannot own Spatial mirror resources")
		}
		return nil
	}
	if !provider.SpatialArtifactsOwned || provider.SpatialMirrorTableName == "" || provider.SpatialRowTriggerName == "" || provider.SpatialDDLGuardName == "" {
		return fmt.Errorf("Oracle Spatial capture requires owned mirror table, row trigger and DDL guard names")
	}
	db, err := openOracle(plan.SourceConnInfo)
	if err != nil {
		return fmt.Errorf("open Oracle Spatial capture owner connection: %w", err)
	}
	defer db.Close()
	if err := validateOracleSpatialOwner(ctx, db, plan.SourceSchema); err != nil {
		return err
	}
	marker := oracleSpatialOwnershipMarker(resource, plan)
	if err := ensureOracleSpatialMirrorTable(ctx, db, plan, resource, marker); err != nil {
		return err
	}
	if err := ensureOracleSpatialMirrorTrigger(ctx, db, plan, resource); err != nil {
		return err
	}
	if err := ensureOracleSpatialDDLGuard(ctx, db, plan, resource); err != nil {
		return err
	}
	if err := synchronizeOracleSpatialMirror(ctx, db, plan, resource); err != nil {
		return err
	}
	return nil
}

func dropOracleSpatialOwnedResources(ctx context.Context, plan *CapturePlan, resource *models.CaptureResource) error {
	provider := resource.Oracle
	if provider == nil {
		return fmt.Errorf("Oracle capture cleanup requires provider resources")
	}
	if !provider.SpatialArtifactsOwned {
		if provider.SpatialMirrorTableName != "" || provider.SpatialRowTriggerName != "" || provider.SpatialDDLGuardName != "" {
			return fmt.Errorf("Oracle capture has unowned Spatial resource names")
		}
		return nil
	}
	if provider.SpatialMirrorTableName == "" || provider.SpatialRowTriggerName == "" || provider.SpatialDDLGuardName == "" {
		return fmt.Errorf("Oracle Spatial capture cleanup requires mirror table, row trigger and DDL guard names")
	}
	db, err := openOracle(plan.SourceConnInfo)
	if err != nil {
		return fmt.Errorf("open Oracle Spatial capture owner connection for cleanup: %w", err)
	}
	defer db.Close()
	if err := validateOracleSpatialOwner(ctx, db, resource.SourceSchema); err != nil {
		return err
	}
	marker := oracleSpatialOwnershipMarker(resource, plan)
	tableExists, err := oracleTableExists(ctx, db, resource.SourceSchema, provider.SpatialMirrorTableName)
	if err != nil {
		return err
	}
	if tableExists {
		actualMarker, err := oracleTableComment(ctx, db, resource.SourceSchema, provider.SpatialMirrorTableName)
		if err != nil {
			return err
		}
		if actualMarker != marker {
			return fmt.Errorf("refuse to drop Oracle Spatial mirror table %q because its ownership marker does not match", provider.SpatialMirrorTableName)
		}
	}
	ddlGuardExists, err := validateExistingOracleSpatialDDLGuard(ctx, db, plan, resource)
	if err != nil {
		return err
	}
	if ddlGuardExists {
		if _, err := db.ExecContext(ctx, "DROP TRIGGER "+oracleQualifiedIdentifier(resource.SourceSchema, provider.SpatialDDLGuardName)); err != nil {
			return fmt.Errorf("drop ADDP-owned Oracle Spatial DDL guard: %w", err)
		}
	}
	rowTriggerExists, err := validateExistingOracleSpatialTrigger(ctx, db, plan, resource)
	if err != nil {
		return err
	}
	if rowTriggerExists {
		if _, err := db.ExecContext(ctx, "DROP TRIGGER "+oracleQualifiedIdentifier(resource.SourceSchema, provider.SpatialRowTriggerName)); err != nil {
			return fmt.Errorf("drop ADDP-owned Oracle Spatial trigger: %w", err)
		}
	}
	if tableExists {
		if _, err := db.ExecContext(ctx, "DROP TABLE "+oracleQualifiedIdentifier(resource.SourceSchema, provider.SpatialMirrorTableName)+" PURGE"); err != nil {
			return fmt.Errorf("drop ADDP-owned Oracle Spatial mirror table: %w", err)
		}
	}
	return nil
}

func validateOracleSpatialOwner(ctx context.Context, db *sql.DB, schema string) error {
	var user string
	if err := db.QueryRowContext(ctx, `SELECT USER FROM DUAL`).Scan(&user); err != nil {
		return fmt.Errorf("query Oracle Spatial capture owner: %w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(user), strings.TrimSpace(schema)) {
		return fmt.Errorf("Oracle Spatial CDC requires the source Engine account to own schema %q (connected user %q)", schema, user)
	}
	rows, err := db.QueryContext(ctx, `SELECT PRIVILEGE FROM SESSION_PRIVS WHERE PRIVILEGE IN ('CREATE TABLE', 'CREATE TRIGGER')`)
	if err != nil {
		return fmt.Errorf("query Oracle Spatial capture privileges: %w", err)
	}
	defer rows.Close()
	privileges := map[string]bool{}
	for rows.Next() {
		var privilege string
		if err := rows.Scan(&privilege); err != nil {
			return err
		}
		privileges[strings.ToUpper(strings.TrimSpace(privilege))] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !privileges["CREATE TABLE"] || !privileges["CREATE TRIGGER"] {
		return fmt.Errorf("Oracle Spatial CDC source Engine account requires CREATE TABLE and CREATE TRIGGER privileges")
	}
	return nil
}

func ensureOracleSpatialMirrorTable(ctx context.Context, db *sql.DB, plan *CapturePlan, resource *models.CaptureResource, marker string) error {
	provider := resource.Oracle
	exists, err := oracleTableExists(ctx, db, plan.SourceSchema, provider.SpatialMirrorTableName)
	if err != nil {
		return err
	}
	if exists {
		actualMarker, err := oracleTableComment(ctx, db, plan.SourceSchema, provider.SpatialMirrorTableName)
		if err != nil {
			return err
		}
		if actualMarker != marker {
			return fmt.Errorf("Oracle Spatial mirror table name %q is occupied by a non-ADDP object", provider.SpatialMirrorTableName)
		}
		return validateOracleSpatialMirrorTable(ctx, db, plan, resource)
	}
	createSQL, primaryKeySQL, supplementalSQL := oracleSpatialMirrorTableDDL(plan, resource)
	if _, err := db.ExecContext(ctx, createSQL); err != nil {
		return fmt.Errorf("create Oracle Spatial mirror table: %w", err)
	}
	commentSQL := "COMMENT ON TABLE " + oracleQualifiedIdentifier(plan.SourceSchema, provider.SpatialMirrorTableName) + " IS " + oracleStringLiteral(marker)
	if _, err := db.ExecContext(ctx, commentSQL); err != nil {
		return fmt.Errorf("mark Oracle Spatial mirror ownership: %w", err)
	}
	if _, err := db.ExecContext(ctx, primaryKeySQL); err != nil {
		return fmt.Errorf("create Oracle Spatial mirror primary key: %w", err)
	}
	if _, err := db.ExecContext(ctx, supplementalSQL); err != nil {
		return fmt.Errorf("enable Oracle Spatial mirror supplemental logging: %w", err)
	}
	return validateOracleSpatialMirrorTable(ctx, db, plan, resource)
}

func oracleSpatialMirrorTableDDL(plan *CapturePlan, resource *models.CaptureResource) (string, string, string) {
	columns := make([]string, 0, len(plan.SourceFields))
	for _, field := range plan.SourceFields {
		quoted := oracleQuoteIdentifier(field)
		expression := "source_row." + quoted
		if plan.SourceFieldTypes[field] == datatype.FieldTypeGeometry {
			expression = "SDO_UTIL.TO_WKBGEOMETRY(" + expression + ")"
		}
		columns = append(columns, expression+" AS "+quoted)
	}
	mirror := oracleQualifiedIdentifier(plan.SourceSchema, resource.Oracle.SpatialMirrorTableName)
	source := oracleQualifiedIdentifier(plan.SourceSchema, plan.SourceTable)
	createSQL := "CREATE TABLE " + mirror + " AS SELECT " + strings.Join(columns, ", ") + " FROM " + source + " source_row WHERE 1 = 0"
	keyColumns := quotedOracleIdentifiers(plan.SourceKeys)
	constraint := oracleQuoteIdentifier("ADDP_P_" + strings.TrimPrefix(resource.Oracle.SpatialMirrorTableName, "ADDP_M_"))
	primaryKeySQL := "ALTER TABLE " + mirror + " ADD CONSTRAINT " + constraint + " PRIMARY KEY (" + strings.Join(keyColumns, ", ") + ")"
	supplementalSQL := "ALTER TABLE " + mirror + " ADD SUPPLEMENTAL LOG DATA (ALL) COLUMNS"
	return createSQL, primaryKeySQL, supplementalSQL
}

func ensureOracleSpatialMirrorTrigger(ctx context.Context, db *sql.DB, plan *CapturePlan, resource *models.CaptureResource) error {
	exists, err := validateExistingOracleSpatialTrigger(ctx, db, plan, resource)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	if _, err := db.ExecContext(ctx, oracleSpatialMirrorTriggerDDL(plan, resource)); err != nil {
		return fmt.Errorf("create Oracle Spatial mirror trigger: %w", err)
	}
	exists, err = validateExistingOracleSpatialTrigger(ctx, db, plan, resource)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("Oracle Spatial mirror trigger was not created")
	}
	return nil
}

func oracleSpatialMirrorTriggerDDL(plan *CapturePlan, resource *models.CaptureResource) string {
	fields := quotedOracleIdentifiers(plan.SourceFields)
	newValues := make([]string, 0, len(plan.SourceFields))
	updates := make([]string, 0, len(plan.SourceFields)-len(plan.SourceKeys))
	keySet := make(map[string]bool, len(plan.SourceKeys))
	for _, key := range plan.SourceKeys {
		keySet[key] = true
	}
	for _, field := range plan.SourceFields {
		quoted := oracleQuoteIdentifier(field)
		value := ":NEW." + quoted
		if plan.SourceFieldTypes[field] == datatype.FieldTypeGeometry {
			value = "SDO_UTIL.TO_WKBGEOMETRY(" + value + ")"
		}
		newValues = append(newValues, value)
		if !keySet[field] {
			updates = append(updates, quoted+" = "+value)
		}
	}
	oldKeyPredicate := oracleTriggerKeyPredicate(plan.SourceKeys, ":OLD")
	keysUnchanged := make([]string, 0, len(plan.SourceKeys))
	for _, key := range plan.SourceKeys {
		quoted := oracleQuoteIdentifier(key)
		keysUnchanged = append(keysUnchanged, ":OLD."+quoted+" = :NEW."+quoted)
	}
	mirror := oracleQualifiedIdentifier(plan.SourceSchema, resource.Oracle.SpatialMirrorTableName)
	return "CREATE OR REPLACE TRIGGER " + oracleQualifiedIdentifier(plan.SourceSchema, resource.Oracle.SpatialRowTriggerName) + "\n" +
		"AFTER INSERT OR UPDATE OR DELETE ON " + oracleQualifiedIdentifier(plan.SourceSchema, plan.SourceTable) + "\n" +
		"FOR EACH ROW\nBEGIN\n" +
		"  IF INSERTING THEN\n" +
		"    INSERT INTO " + mirror + " (" + strings.Join(fields, ", ") + ") VALUES (" + strings.Join(newValues, ", ") + ");\n" +
		"  ELSIF UPDATING THEN\n" +
		"    IF " + strings.Join(keysUnchanged, " AND ") + " THEN\n" +
		"      UPDATE " + mirror + " SET " + strings.Join(updates, ", ") + " WHERE " + oldKeyPredicate + ";\n" +
		"    ELSE\n" +
		"      DELETE FROM " + mirror + " WHERE " + oldKeyPredicate + ";\n" +
		"      INSERT INTO " + mirror + " (" + strings.Join(fields, ", ") + ") VALUES (" + strings.Join(newValues, ", ") + ");\n" +
		"    END IF;\n" +
		"  ELSE\n" +
		"    DELETE FROM " + mirror + " WHERE " + oldKeyPredicate + ";\n" +
		"  END IF;\nEND;"
}

func ensureOracleSpatialDDLGuard(ctx context.Context, db *sql.DB, plan *CapturePlan, resource *models.CaptureResource) error {
	exists, err := validateExistingOracleSpatialDDLGuard(ctx, db, plan, resource)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	if _, err := db.ExecContext(ctx, oracleSpatialDDLGuardDDL(plan, resource)); err != nil {
		return fmt.Errorf("create Oracle Spatial DDL guard: %w", err)
	}
	exists, err = validateExistingOracleSpatialDDLGuard(ctx, db, plan, resource)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("Oracle Spatial DDL guard was not created")
	}
	return nil
}

func oracleSpatialDDLGuardDDL(plan *CapturePlan, resource *models.CaptureResource) string {
	return "CREATE OR REPLACE TRIGGER " + oracleQualifiedIdentifier(plan.SourceSchema, resource.Oracle.SpatialDDLGuardName) + "\n" +
		"BEFORE ALTER OR DROP OR RENAME ON SCHEMA\nBEGIN\n" +
		"  IF ORA_DICT_OBJ_OWNER = " + oracleStringLiteral(plan.SourceSchema) +
		" AND ORA_DICT_OBJ_NAME = " + oracleStringLiteral(plan.SourceTable) +
		" AND ORA_DICT_OBJ_TYPE = 'TABLE' THEN\n" +
		"    RAISE_APPLICATION_ERROR(-20042, 'ADDP Oracle Spatial CDC source schema is frozen; Stop the CDC task before DDL');\n" +
		"  END IF;\nEND;"
}

func synchronizeOracleSpatialMirror(ctx context.Context, db *sql.DB, plan *CapturePlan, resource *models.CaptureResource) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin Oracle Spatial mirror synchronization: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "LOCK TABLE "+oracleQualifiedIdentifier(plan.SourceSchema, plan.SourceTable)+" IN EXCLUSIVE MODE"); err != nil {
		return fmt.Errorf("lock Oracle Spatial CDC source table: %w", err)
	}
	deleteSQL, mergeSQL := oracleSpatialMirrorSynchronizationSQL(plan, resource)
	if _, err := tx.ExecContext(ctx, deleteSQL); err != nil {
		return fmt.Errorf("delete stale Oracle Spatial mirror rows: %w", err)
	}
	if _, err := tx.ExecContext(ctx, mergeSQL); err != nil {
		return fmt.Errorf("backfill Oracle Spatial mirror rows: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit Oracle Spatial mirror synchronization: %w", err)
	}
	return nil
}

func oracleSpatialMirrorSynchronizationSQL(plan *CapturePlan, resource *models.CaptureResource) (string, string) {
	mirror := oracleQualifiedIdentifier(plan.SourceSchema, resource.Oracle.SpatialMirrorTableName)
	source := oracleQualifiedIdentifier(plan.SourceSchema, plan.SourceTable)
	keyMatch := oracleAliasKeyPredicate(plan.SourceKeys, "source_row", "mirror_row")
	deleteSQL := "DELETE FROM " + mirror + " mirror_row WHERE NOT EXISTS (SELECT 1 FROM " + source + " source_row WHERE " + keyMatch + ")"
	selectFields := make([]string, 0, len(plan.SourceFields))
	insertFields := quotedOracleIdentifiers(plan.SourceFields)
	insertValues := make([]string, 0, len(plan.SourceFields))
	updates := make([]string, 0, len(plan.SourceFields)-len(plan.SourceKeys))
	keySet := make(map[string]bool, len(plan.SourceKeys))
	for _, key := range plan.SourceKeys {
		keySet[key] = true
	}
	for _, field := range plan.SourceFields {
		quoted := oracleQuoteIdentifier(field)
		expression := "source_base." + quoted
		if plan.SourceFieldTypes[field] == datatype.FieldTypeGeometry {
			expression = "SDO_UTIL.TO_WKBGEOMETRY(" + expression + ")"
		}
		selectFields = append(selectFields, expression+" AS "+quoted)
		insertValues = append(insertValues, "source_row."+quoted)
		if !keySet[field] {
			updates = append(updates, "mirror_row."+quoted+" = source_row."+quoted)
		}
	}
	mergeSQL := "MERGE INTO " + mirror + " mirror_row USING (SELECT " + strings.Join(selectFields, ", ") + " FROM " + source + " source_base) source_row ON (" +
		oracleAliasKeyPredicate(plan.SourceKeys, "source_row", "mirror_row") + ") WHEN MATCHED THEN UPDATE SET " + strings.Join(updates, ", ") +
		" WHEN NOT MATCHED THEN INSERT (" + strings.Join(insertFields, ", ") + ") VALUES (" + strings.Join(insertValues, ", ") + ")"
	return deleteSQL, mergeSQL
}

func validateOracleSpatialMirrorTable(ctx context.Context, db *sql.DB, plan *CapturePlan, resource *models.CaptureResource) error {
	rows, err := db.QueryContext(ctx, `
		SELECT COLUMN_NAME, DATA_TYPE FROM ALL_TAB_COLS
		WHERE OWNER = :1 AND TABLE_NAME = :2 AND HIDDEN_COLUMN = 'NO'
		ORDER BY COLUMN_ID`, plan.SourceSchema, resource.Oracle.SpatialMirrorTableName)
	if err != nil {
		return fmt.Errorf("inspect Oracle Spatial mirror columns: %w", err)
	}
	defer rows.Close()
	actualFields := make([]string, 0)
	actualTypes := map[string]string{}
	for rows.Next() {
		var name, dataType string
		if err := rows.Scan(&name, &dataType); err != nil {
			return err
		}
		actualFields = append(actualFields, name)
		actualTypes[name] = strings.ToUpper(strings.TrimSpace(dataType))
	}
	if err := rows.Err(); err != nil {
		return err
	}
	expectedFields := append([]string(nil), plan.SourceFields...)
	sort.Strings(actualFields)
	sort.Strings(expectedFields)
	if !reflect.DeepEqual(actualFields, expectedFields) {
		return fmt.Errorf("Oracle Spatial mirror fields do not match the frozen source schema: actual=%v expected=%v", actualFields, expectedFields)
	}
	for _, column := range plan.SourceSpatialInfo.GeometryColumns {
		if actualTypes[column.Name] != "BLOB" {
			return fmt.Errorf("Oracle Spatial mirror field %q must be BLOB", column.Name)
		}
	}
	rows, err = db.QueryContext(ctx, `
		SELECT cols.COLUMN_NAME
		FROM ALL_CONSTRAINTS cons
		JOIN ALL_CONS_COLUMNS cols ON cols.OWNER = cons.OWNER AND cols.CONSTRAINT_NAME = cons.CONSTRAINT_NAME
		WHERE cons.OWNER = :1 AND cons.TABLE_NAME = :2 AND cons.CONSTRAINT_TYPE = 'P'
		ORDER BY cols.POSITION`, plan.SourceSchema, resource.Oracle.SpatialMirrorTableName)
	if err != nil {
		return fmt.Errorf("inspect Oracle Spatial mirror primary key: %w", err)
	}
	defer rows.Close()
	actualKeys := make([]string, 0)
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return err
		}
		actualKeys = append(actualKeys, key)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !reflect.DeepEqual(actualKeys, plan.SourceKeys) {
		return fmt.Errorf("Oracle Spatial mirror primary key does not match source keys: actual=%v expected=%v", actualKeys, plan.SourceKeys)
	}
	var loggingGroups int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM ALL_LOG_GROUPS
		WHERE OWNER = :1 AND TABLE_NAME = :2 AND LOG_GROUP_TYPE = 'ALL COLUMN LOGGING' AND ALWAYS = 'ALWAYS'`,
		plan.SourceSchema, resource.Oracle.SpatialMirrorTableName).Scan(&loggingGroups); err != nil {
		return fmt.Errorf("inspect Oracle Spatial mirror supplemental logging: %w", err)
	}
	if loggingGroups == 0 {
		return fmt.Errorf("Oracle Spatial mirror table is missing ALL COLUMN LOGGING")
	}
	return nil
}

func validateExistingOracleSpatialTrigger(ctx context.Context, db *sql.DB, plan *CapturePlan, resource *models.CaptureResource) (bool, error) {
	var tableOwner, tableName, status, triggerBody string
	err := db.QueryRowContext(ctx, `
		SELECT TABLE_OWNER, TABLE_NAME, STATUS, TRIGGER_BODY FROM ALL_TRIGGERS
		WHERE OWNER = :1 AND TRIGGER_NAME = :2`, plan.SourceSchema, resource.Oracle.SpatialRowTriggerName).
		Scan(&tableOwner, &tableName, &status, &triggerBody)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect Oracle Spatial mirror trigger: %w", err)
	}
	if !strings.EqualFold(tableOwner, plan.SourceSchema) || !strings.EqualFold(tableName, plan.SourceTable) || !strings.EqualFold(status, "ENABLED") ||
		!strings.Contains(strings.ToUpper(triggerBody), strings.ToUpper(resource.Oracle.SpatialMirrorTableName)) ||
		!strings.Contains(strings.ToUpper(triggerBody), "SDO_UTIL.TO_WKBGEOMETRY") {
		return false, fmt.Errorf("Oracle Spatial row trigger name %q is occupied by an incompatible object", resource.Oracle.SpatialRowTriggerName)
	}
	return true, nil
}

func validateExistingOracleSpatialDDLGuard(ctx context.Context, db *sql.DB, plan *CapturePlan, resource *models.CaptureResource) (bool, error) {
	var baseObjectType, status, triggerBody string
	err := db.QueryRowContext(ctx, `
		SELECT BASE_OBJECT_TYPE, STATUS, TRIGGER_BODY FROM ALL_TRIGGERS
		WHERE OWNER = :1 AND TRIGGER_NAME = :2`, plan.SourceSchema, resource.Oracle.SpatialDDLGuardName).
		Scan(&baseObjectType, &status, &triggerBody)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect Oracle Spatial DDL guard: %w", err)
	}
	if !strings.EqualFold(baseObjectType, "SCHEMA") || !strings.EqualFold(status, "ENABLED") ||
		!strings.Contains(strings.ToUpper(triggerBody), strings.ToUpper(oracleStringLiteral(plan.SourceTable))) ||
		!strings.Contains(triggerBody, "-20042") {
		return false, fmt.Errorf("Oracle Spatial DDL guard name %q is occupied by an incompatible object", resource.Oracle.SpatialDDLGuardName)
	}
	return true, nil
}

func oracleTableExists(ctx context.Context, db *sql.DB, schema, table string) (bool, error) {
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ALL_TABLES WHERE OWNER = :1 AND TABLE_NAME = :2`, schema, table).Scan(&count); err != nil {
		return false, fmt.Errorf("inspect Oracle capture table: %w", err)
	}
	return count > 0, nil
}

func oracleTableComment(ctx context.Context, db *sql.DB, schema, table string) (string, error) {
	var comment sql.NullString
	err := db.QueryRowContext(ctx, `SELECT COMMENTS FROM ALL_TAB_COMMENTS WHERE OWNER = :1 AND TABLE_NAME = :2`, schema, table).Scan(&comment)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("inspect Oracle capture table ownership marker: %w", err)
	}
	return strings.TrimSpace(comment.String), nil
}

func oracleSpatialOwnershipMarker(resource *models.CaptureResource, plan *CapturePlan) string {
	return fmt.Sprintf("%s%d source %s.%s", oracleplugin.InternalCaptureTableCommentPrefix, resource.ID, plan.SourceSchema, plan.SourceTable)
}

func oracleQualifiedIdentifier(schema, object string) string {
	return oracleQuoteIdentifier(schema) + "." + oracleQuoteIdentifier(object)
}

func quotedOracleIdentifiers(values []string) []string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, oracleQuoteIdentifier(value))
	}
	return quoted
}

func oracleTriggerKeyPredicate(keys []string, record string) string {
	predicates := make([]string, 0, len(keys))
	for _, key := range keys {
		quoted := oracleQuoteIdentifier(key)
		predicates = append(predicates, quoted+" = "+record+"."+quoted)
	}
	return strings.Join(predicates, " AND ")
}

func oracleAliasKeyPredicate(keys []string, sourceAlias, targetAlias string) string {
	predicates := make([]string, 0, len(keys))
	for _, key := range keys {
		quoted := oracleQuoteIdentifier(key)
		predicates = append(predicates, targetAlias+"."+quoted+" = "+sourceAlias+"."+quoted)
	}
	return strings.Join(predicates, " AND ")
}

func oracleStringLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
