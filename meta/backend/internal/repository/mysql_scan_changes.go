package repository

import (
	"embed"
	"fmt"
	"strings"

	"github.com/addp/common/schema"
	"gorm.io/gorm"
)

//go:embed mysql_scan_changes/*.sql
var mysqlScanChangesDDL embed.FS

// The same projection is used for initial installation and ongoing changes.
// JSON_MERGE_PATCH omits absent optional fields. Content previews stay out of Catalog.
const mysqlChangeSnapshot = `JSON_MERGE_PATCH(JSON_OBJECT(),JSON_OBJECT(
 'item_id',x.id,'node_id',x.node_id,'engine_id',x.engine_id,'name',x.name,
 'item_type',x.item_type,'full_name',x.full_name,'row_count',x.row_count,'size_bytes',x.size_bytes,
 'schema_name',NULLIF(JSON_UNQUOTE(JSON_EXTRACT(x.attributes,'$.storage.schema_name')),''),
 'table_name',IF(x.item_type IN ('table','view'),x.name,NULL),
 'fields',JSON_EXTRACT(x.attributes,'$.type_info.table.fields'),'scanned_depth',x.scanned_depth,
 'scanned_at',DATE_FORMAT(CONVERT_TZ(x.scanned_at,@@session.time_zone,'+00:00'),'%Y-%m-%dT%H:%i:%s.%fZ'),
 'data_updated_at',DATE_FORMAT(CONVERT_TZ(x.data_updated_at,@@session.time_zone,'+00:00'),'%Y-%m-%dT%H:%i:%s.%fZ')))`

const mysqlAppendItemChange = `INSERT INTO meta.data_item_changes (tenant_id,item_id,source_identity,operation,snapshot)
 SELECT x.tenant_id,x.id,x.fingerprint,`

func mysqlFactTriggers() []string {
	var triggers []string
	for _, table := range []string{"meta_item", "meta_node"} {
		columns := "id tenant_id engine_id name full_name attributes deleted_at"
		if table == "meta_node" {
			columns += " parent_node_id node_type path"
		} else {
			columns += " node_id item_type fingerprint"
		}
		var unchanged []string
		for _, col := range strings.Fields(columns) {
			unchanged = append(unchanged, "OLD."+col+" <=> NEW."+col)
		}
		for _, event := range []string{"INSERT", "UPDATE", "DELETE"} {
			header := fmt.Sprintf("CREATE TRIGGER IF NOT EXISTS %s_%s_facts\nAFTER %s ON %s FOR EACH ROW\n",
				table, strings.ToLower(event), event, table)
			advance := func(row string) string {
				return "INSERT INTO meta.authorization_basis_revisions (tenant_id,version) VALUES (" + row + ".tenant_id,1) ON DUPLICATE KEY UPDATE version=version+1;"
			}
			row := "NEW"
			body := advance(row)
			operation := "IF(x.deleted_at IS NULL,'upsert','missing')"
			switch event {
			case "DELETE":
				row = "OLD"
				body = advance(row)
				operation = "'missing'"
			case "UPDATE":
				body = "IF NOT (" + strings.Join(unchanged, " AND ") + ") THEN\n" + advance("OLD") +
					"\nIF NEW.tenant_id <> OLD.tenant_id THEN\n" + advance("NEW") + "\nEND IF;"
				if table == "meta_item" {
					// Statistics-only updates still append a Catalog cursor. Hold the
					// tenant row until commit without advancing the authorization basis.
					body += "\nELSE\nUPDATE meta.authorization_basis_revisions SET version=version WHERE tenant_id=NEW.tenant_id;"
				}
				body += "\nEND IF;"
			}
			if table == "meta_item" {
				body += "\n" + strings.ReplaceAll(mysqlAppendItemChange+operation+","+mysqlChangeSnapshot, "x.", row+".") + ";"
			}
			triggers = append(triggers, header+"BEGIN\n"+body+"\nEND")
		}
	}
	return triggers
}

// MigrateMySQLScanChanges publishes its revision only after the DBA installs the
// fact triggers and existing items and tenant revisions have been seeded atomically.
func MigrateMySQLScanChanges(db *gorm.DB) error {
	triggers := mysqlFactTriggers()
	return schema.MigrateMySQLWithData(db, "meta_scan_changes", 1, func(conn *gorm.DB) error {
		files, err := mysqlScanChangesDDL.ReadDir("mysql_scan_changes")
		if err != nil {
			return err
		}
		for _, file := range files {
			ddl, err := mysqlScanChangesDDL.ReadFile("mysql_scan_changes/" + file.Name())
			if err != nil {
				return err
			}
			if err := conn.Exec(string(ddl)).Error; err != nil {
				return fmt.Errorf("install %s: %w", file.Name(), err)
			}
		}
		return schema.CheckMySQLTriggers(conn, triggers)
	}, func(tx *gorm.DB, _ int64) error {
		if err := tx.Exec("INSERT INTO meta.authorization_basis_generation(singleton,generation) VALUES(1,UUID())").Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO meta.authorization_basis_revisions(tenant_id,version)
			SELECT tenant_id,1 FROM (SELECT tenant_id FROM meta.meta_node UNION SELECT tenant_id FROM meta.meta_item) tenants`).Error; err != nil {
			return err
		}
		return tx.Exec(mysqlAppendItemChange + "IF(x.deleted_at IS NULL,'upsert','missing')," + mysqlChangeSnapshot + " FROM meta.meta_item x ORDER BY x.id").Error
	})
}
