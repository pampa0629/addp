package migration

import (
	_ "embed"
	"fmt"
	"slices"
	"strings"

	"github.com/addp/common/schema"
	"gorm.io/gorm"
)

const mysqlGrantProcedureOwner = "system_source_grant_procedure"
const mysqlGrantProcedureVersion = 1

//go:embed mysql_source_grant/persist_source_grant.sql
var grantProcedureDDL string

// VerifyMySQLSourceGrantProcedure runs offline with the deployment DBA identity.
// It reads the catalog again on repeat invocations, including a current revision.
func VerifyMySQLSourceGrantProcedure(db *gorm.DB) error {
	if err := schema.RequireMySQL(db, mysqlSourceGrantCatalogOwner, mysqlSourceGrantCatalogVersion); err != nil {
		return err
	}
	var verificationErr error
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`INSERT INTO startup_schema_revision(owner,version,pending_version) VALUES (?,0,?)
			ON DUPLICATE KEY UPDATE owner=owner`, mysqlGrantProcedureOwner, mysqlGrantProcedureVersion).Error; err != nil {
			return err
		}
		var version, pending int
		if err := tx.Raw("SELECT version,pending_version FROM startup_schema_revision WHERE owner=? FOR UPDATE", mysqlGrantProcedureOwner).Row().Scan(&version, &pending); err != nil {
			return err
		}
		if version > mysqlGrantProcedureVersion || pending > mysqlGrantProcedureVersion {
			return fmt.Errorf("System source Grant procedure revision is newer than the verifier")
		}
		verificationErr = inspectMySQLGrantProcedure(tx)
		if verificationErr != nil {
			return tx.Exec("UPDATE startup_schema_revision SET pending_version=? WHERE owner=?", mysqlGrantProcedureVersion, mysqlGrantProcedureOwner).Error
		}
		return tx.Exec("UPDATE startup_schema_revision SET version=?, pending_version=0 WHERE owner=?", mysqlGrantProcedureVersion, mysqlGrantProcedureOwner).Error
	})
	if err != nil {
		return err
	}
	return verificationErr
}

func inspectMySQLGrantProcedure(db *gorm.DB) error {
	var body, definer, security, access string
	err := db.Raw(`SELECT routine_definition, definer, security_type, sql_data_access
FROM information_schema.routines WHERE routine_schema='system' AND routine_name='persist_source_grant'
AND routine_type='PROCEDURE'`).Row().Scan(&body, &definer, &security, &access)
	if err != nil {
		return fmt.Errorf("read System source Grant procedure as deployment DBA: %w", err)
	}
	header, wantBody, _ := strings.Cut(grantProcedureDDL, "\nBEGIN\n")
	if definer != "addp_system_source_grant@localhost" || security != "DEFINER" || access != "MODIFIES SQL DATA" ||
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
	var locked, mandatory string
	if err := db.Raw("SELECT account_locked, @@global.mandatory_roles FROM mysql.user WHERE user='addp_system_source_grant' AND host='localhost'").Row().Scan(&locked, &mandatory); err != nil {
		return err
	}
	if locked != "Y" || mandatory != "" {
		return fmt.Errorf("cannot attest a locked System source Grant definer with only its explicit grants")
	}
	var grants []string
	if err := db.Raw("SHOW GRANTS FOR 'addp_system_source_grant'@'localhost'").Scan(&grants).Error; err != nil {
		return err
	}
	want = []string{"USAGE ON *.*", "INSERT ON `system`.`engine_access_grants`", "INSERT ON `system`.`engine_access_initial_grant_outcomes`",
		"INSERT ON `system`.`engine_access_upload_grant_outcomes`", "EXECUTE ON PROCEDURE `system`.`persist_source_grant`"}
	for i, grant := range want {
		want[i] = "GRANT " + grant + " TO `addp_system_source_grant`@`localhost`"
	}
	slices.Sort(want)
	slices.Sort(grants)
	if !slices.Equal(want, grants) {
		return fmt.Errorf("System source Grant definer privileges differ")
	}
	return nil
}
