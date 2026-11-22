package migration

import (
	_ "embed"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/addp/common/schema"
	mysqldriver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

const mysqlGrantProcedureOwner = "system_source_grant_procedure"
const mysqlGrantProcedureVersion = 2

//go:embed mysql_source_grant/persist_source_grant.sql
var grantProcedureDDL string

// PrepareMySQLSourceGrant configures the sole runtime write exception during deployment.
func PrepareMySQLSourceGrant(db, runtime *gorm.DB) error {
	if err := schema.RequireMySQL(db, mysqlSourceGrantCatalogOwner, mysqlSourceGrantCatalogVersion); err != nil {
		return err
	}
	var account string
	if err := runtime.Raw("SELECT CURRENT_USER()").Row().Scan(&account); err != nil {
		return err
	}
	at := strings.LastIndexByte(account, '@')
	if at <= 0 {
		return fmt.Errorf("invalid MySQL runtime account")
	}
	quote := func(s string) string { return "`" + strings.ReplaceAll(s, "`", "``") + "`" }
	grantee := quote(account[:at]) + "@" + quote(account[at+1:])
	protected := []string{"engine_access_grants", "engine_access_initial_grant_outcomes", "engine_access_upload_grant_outcomes"}
	// EXPLAIN checks the connected runtime account, including inherited grants, without modifying data.
	checkAccess := func() error {
		for _, table := range protected {
			for _, stmt := range []string{"INSERT INTO system." + table + " () VALUES ()", "DELETE FROM system." + table + " WHERE FALSE", "UPDATE system." + table + " SET tenant_id=tenant_id WHERE FALSE"} {
				err := runtime.Exec("EXPLAIN " + stmt).Error
				var denied *mysqldriver.MySQLError
				if !errors.As(err, &denied) || denied.Number != 1142 {
					return fmt.Errorf("expected runtime write denial on system.%s, got %v", table, err)
				}
			}
		}
		return nil
	}
	var tables []string
	if err := db.Raw("SELECT table_name FROM information_schema.tables WHERE table_schema='system' AND table_type='BASE TABLE' ORDER BY table_name").Scan(&tables).Error; err != nil {
		return err
	}
	for _, table := range tables {
		privileges := "SELECT, INSERT, UPDATE, DELETE"
		if slices.Contains(protected, table) {
			privileges = "SELECT"
		}
		if err := db.Exec("GRANT " + privileges + " ON `system`." + quote(table) + " TO " + grantee).Error; err != nil {
			return err
		}
	}
	return schema.MigrateMySQLVerified(db, mysqlGrantProcedureOwner, mysqlGrantProcedureVersion, func(conn *gorm.DB) error {
		if err := conn.Exec("DROP PROCEDURE IF EXISTS system.persist_source_grant").Error; err != nil {
			return err
		}
		return conn.Exec(grantProcedureDDL).Error
	}, func(conn *gorm.DB) error {
		if err := inspectMySQLGrantProcedure(conn); err != nil {
			return err
		}
		if err := checkAccess(); err != nil {
			return err
		}
		return conn.Exec("GRANT EXECUTE ON PROCEDURE `system`.persist_source_grant TO " + grantee).Error
	})
}

func inspectMySQLGrantProcedure(db *gorm.DB) error {
	var body, security, access string
	var owned bool
	err := db.Raw(`SELECT routine_definition, definer=CURRENT_USER(), security_type, sql_data_access
FROM information_schema.routines WHERE routine_schema='system' AND routine_name='persist_source_grant'
AND routine_type='PROCEDURE'`).Row().Scan(&body, &owned, &security, &access)
	if err != nil {
		return fmt.Errorf("read System source Grant procedure as deployment DBA: %w", err)
	}
	header, wantBody, _ := strings.Cut(grantProcedureDDL, "\nBEGIN\n")
	if !owned || security != "DEFINER" || access != "MODIFIES SQL DATA" ||
		strings.TrimSpace(body) != "BEGIN\n"+strings.TrimRight(wantBody, "\n") {
		return fmt.Errorf("System source Grant procedure definition or security differs")
	}
	_, params, _ := strings.Cut(header, "persist_source_grant(")
	params, _, _ = strings.Cut(params, "\n)")
	want := strings.Split(strings.ToLower(strings.Join(strings.Fields(params), " ")), ", ")
	for i, param := range want {
		want[i] = strings.Replace(param, " boolean", " tinyint(1)", 1)
	}
	var actual []string
	if err := db.Raw(`SELECT LOWER(CONCAT(parameter_mode,' ',parameter_name,' ',dtd_identifier))
FROM information_schema.parameters WHERE specific_schema='system' AND specific_name='persist_source_grant'
ORDER BY ordinal_position`).Scan(&actual).Error; err != nil {
		return err
	}
	if !slices.Equal(want, actual) {
		return fmt.Errorf("System source Grant procedure signature differs")
	}
	return nil
}
