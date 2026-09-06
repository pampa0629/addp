package oceanbase

import (
	"context"
	"database/sql"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

func TestIntegrationOceanBaseTableWrite(t *testing.T) {
	if os.Getenv("ADDP_OCEANBASE_INTEGRATION") != "1" {
		t.Skip("set ADDP_OCEANBASE_INTEGRATION=1 to run OceanBase integration test")
	}

	const engineID uint = 94002
	const tableName = "addp_table_write_gate"
	p := &Plugin{}
	connInfo := oceanBaseIntegrationConnInfo()
	databaseName := oceanBaseIntegrationEnv("ADDP_TEST_OCEANBASE_DATABASE", "addp_oceanbase_disposable")
	path := plugin.TabularItemPath(engineID, plugin.EngineCatalogTermDatabase, databaseName, tableName)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	t.Cleanup(func() { _ = plugin.ClosePool(engineID) })

	dsn, err := p.BuildDSN(connInfo)
	if err != nil {
		t.Fatalf("BuildDSN() error = %v", err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	dropGateTable := func(cleanupContext context.Context) error {
		_, dropErr := db.ExecContext(cleanupContext, "DROP TABLE IF EXISTS `"+tableName+"`")
		return dropErr
	}
	if err := dropGateTable(ctx); err != nil {
		t.Fatalf("drop stale gate table error = %v", err)
	}
	t.Cleanup(func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if err := dropGateTable(cleanupContext); err != nil {
			t.Errorf("cleanup gate table error = %v", err)
		}
	})

	fields := []datatype.FieldInfo{
		{Name: "id", Type: datatype.FieldTypeBigInt, PrimaryKey: true, Nullable: false},
		{Name: "customer_code", Type: datatype.FieldTypeString, Nullable: false},
		{Name: "amount", Type: datatype.FieldTypeDecimal, Precision: 18, Scale: 2, Nullable: false},
		{Name: "active", Type: datatype.FieldTypeBool, Nullable: false},
		{Name: "occurred_at", Type: datatype.FieldTypeTimestamp, Nullable: false},
		{Name: "payload", Type: datatype.FieldTypeJSON, Nullable: true},
	}
	if err := p.PrepareTableWrite(ctx, connInfo, path, plugin.TableWriteOptions{Fields: fields}); err != nil {
		t.Fatalf("PrepareTableWrite(create) error = %v", err)
	}

	evolvedFields := append(append([]datatype.FieldInfo(nil), fields...), datatype.FieldInfo{Name: "note", Type: datatype.FieldTypeString, Nullable: true})
	if err := p.PrepareTableWrite(ctx, connInfo, path, plugin.TableWriteOptions{Fields: evolvedFields}); err != nil {
		t.Fatalf("PrepareTableWrite(evolve) error = %v", err)
	}

	session, err := p.OpenTableWriteSession(ctx, connInfo, path, plugin.TableWriteSessionOptions{
		Method: "copy",
		Fields: evolvedFields,
	})
	if err != nil {
		t.Fatalf("OpenTableWriteSession() error = %v", err)
	}
	occurredAt := time.Date(2026, time.September, 6, 10, 11, 12, 345000000, time.UTC)
	batch := &plugin.BatchData{Rows: []map[string]interface{}{
		{"id": int64(1), "customer_code": "C-OCEAN-001", "amount": "1234.56", "active": true, "occurred_at": occurredAt, "payload": `{"source":"transfer"}`, "note": "王小丽"},
		{"id": int64(2), "customer_code": "C-OCEAN-002", "amount": "0.10", "active": false, "occurred_at": occurredAt.Add(time.Microsecond), "payload": nil, "note": nil},
	}}
	if err := session.WriteBatch(ctx, batch); err != nil {
		_ = session.Abort(ctx)
		t.Fatalf("WriteBatch() error = %v", err)
	}
	if err := session.Close(ctx); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	markerProvider, ok := session.(plugin.CommitMarkerProvider)
	if !ok {
		t.Fatal("OceanBase table write session must expose a commit marker")
	}
	marker := markerProvider.CommitMarker()
	if marker == nil || marker.Provider != "oceanbase.table_write_session" || marker.PositionUnit != "session_commit" {
		t.Fatalf("CommitMarker() = %#v", marker)
	}
	if marker.CommitPosition["rows_committed"] != int64(2) || marker.Fingerprint["method"] != "oceanbase_insert" {
		t.Fatalf("CommitMarker() payload = %#v", marker)
	}

	rows, err := db.QueryContext(ctx, "SELECT id, customer_code, amount, active, occurred_at, payload, note FROM `"+tableName+"` ORDER BY id")
	if err != nil {
		t.Fatalf("query written rows error = %v", err)
	}
	defer rows.Close()
	type writtenRow struct {
		id           int64
		customerCode string
		amount       string
		active       bool
		occurredAt   time.Time
		payload      sql.NullString
		note         sql.NullString
	}
	written := make([]writtenRow, 0, 2)
	for rows.Next() {
		var row writtenRow
		if err := rows.Scan(&row.id, &row.customerCode, &row.amount, &row.active, &row.occurredAt, &row.payload, &row.note); err != nil {
			t.Fatalf("scan written row error = %v", err)
		}
		written = append(written, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate written rows error = %v", err)
	}
	if len(written) != 2 || written[0].customerCode != "C-OCEAN-001" || written[0].amount != "1234.56" || !written[0].active || !written[0].note.Valid || written[0].note.String != "王小丽" {
		t.Fatalf("written rows = %#v", written)
	}
	if written[1].payload.Valid || written[1].note.Valid {
		t.Fatalf("nullable values were not preserved: %#v", written[1])
	}

	facts, err := p.DescribeEngineCatalogFacts(ctx, connInfo, path, plugin.EngineCatalogFactsOptions{IncludeStatistics: true})
	if err != nil {
		t.Fatalf("DescribeEngineCatalogFacts(write target) error = %v", err)
	}
	if facts.Table == nil || facts.Table.RowCount == nil || *facts.Table.RowCount != 2 {
		t.Fatalf("write target facts = %#v, want row_count=2", facts.Table)
	}
	fieldNames := make([]string, len(facts.Table.Fields))
	for index := range facts.Table.Fields {
		fieldNames[index] = facts.Table.Fields[index].Name
	}
	if want := []string{"id", "customer_code", "amount", "active", "occurred_at", "payload", "note"}; !reflect.DeepEqual(fieldNames, want) {
		t.Fatalf("write target fields = %#v, want %#v", fieldNames, want)
	}

	if err := p.DeleteResource(ctx, connInfo, path); err != nil {
		t.Fatalf("DeleteResource() error = %v", err)
	}
	var remaining int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM information_schema.tables
		WHERE table_schema = ? AND table_name = ?
	`, databaseName, tableName).Scan(&remaining); err != nil {
		t.Fatalf("query deleted gate table error = %v", err)
	}
	if remaining != 0 {
		t.Fatalf("DeleteResource() left %d matching gate tables", remaining)
	}
}

func TestIntegrationOceanBaseTableUpsert(t *testing.T) {
	if os.Getenv("ADDP_OCEANBASE_INTEGRATION") != "1" {
		t.Skip("set ADDP_OCEANBASE_INTEGRATION=1 to run OceanBase integration test")
	}

	const engineID uint = 94003
	const tableName = "addp_table_upsert_gate"
	const ambiguousTableName = "addp_table_upsert_ambiguous_gate"
	p := &Plugin{}
	connInfo := oceanBaseIntegrationConnInfo()
	databaseName := oceanBaseIntegrationEnv("ADDP_TEST_OCEANBASE_DATABASE", "addp_oceanbase_disposable")
	path := plugin.TabularItemPath(engineID, plugin.EngineCatalogTermDatabase, databaseName, tableName)
	ambiguousPath := plugin.TabularItemPath(engineID, plugin.EngineCatalogTermDatabase, databaseName, ambiguousTableName)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	t.Cleanup(func() { _ = plugin.ClosePool(engineID) })

	dsn, err := p.BuildDSN(connInfo)
	if err != nil {
		t.Fatalf("BuildDSN() error = %v", err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, cleanupPath := range []plugin.EngineCatalogPath{path, ambiguousPath} {
		if err := p.DeleteResource(ctx, connInfo, cleanupPath); err != nil {
			t.Fatalf("drop stale upsert gate table error = %v", err)
		}
	}
	t.Cleanup(func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		for _, cleanupPath := range []plugin.EngineCatalogPath{path, ambiguousPath} {
			if err := p.DeleteResource(cleanupContext, connInfo, cleanupPath); err != nil {
				t.Errorf("cleanup upsert gate table error = %v", err)
			}
		}
	})

	fields := []datatype.FieldInfo{
		{Name: "id", Type: datatype.FieldTypeBigInt, Nullable: false},
		{Name: "tenant_id", Type: datatype.FieldTypeBigInt, Nullable: false},
		{Name: "source_pk", Type: datatype.FieldTypeBigInt, PrimaryKey: true, Nullable: false},
		{Name: "name", Type: datatype.FieldTypeString, Nullable: true},
		{Name: "updated_at", Type: datatype.FieldTypeTimestamp, Nullable: false},
	}
	opts := plugin.TableUpsertOptions{Fields: fields, Keys: []string{"tenant_id", "id"}}
	if err := p.PrepareTableUpsert(ctx, connInfo, path, opts); err != nil {
		t.Fatalf("PrepareTableUpsert() error = %v", err)
	}

	updatedAt := time.Date(2026, time.September, 6, 12, 0, 0, 123000000, time.UTC)
	initial := &plugin.BatchData{Fields: fields, Rows: []map[string]interface{}{
		{"tenant_id": int64(1), "id": int64(10), "source_pk": int64(100), "name": "首次写入", "updated_at": updatedAt},
	}}
	if err := p.UpsertBatch(ctx, connInfo, path, initial, opts); err != nil {
		t.Fatalf("initial UpsertBatch() error = %v", err)
	}
	changed := &plugin.BatchData{Fields: fields, Rows: []map[string]interface{}{
		{"tenant_id": int64(1), "id": int64(10), "source_pk": int64(100), "name": "更新成功", "updated_at": updatedAt.Add(time.Microsecond)},
		{"tenant_id": int64(1), "id": int64(11), "source_pk": int64(100), "name": "新增成功", "updated_at": updatedAt.Add(2 * time.Microsecond)},
	}}
	for attempt := 0; attempt < 2; attempt++ {
		if err := p.UpsertBatch(ctx, connInfo, path, changed, opts); err != nil {
			t.Fatalf("idempotent UpsertBatch() attempt %d error = %v", attempt+1, err)
		}
	}

	rows, err := db.QueryContext(ctx, "SELECT id, source_pk, name FROM `"+tableName+"` ORDER BY id")
	if err != nil {
		t.Fatalf("query upserted rows error = %v", err)
	}
	defer rows.Close()
	type upsertedRow struct {
		id       int64
		sourcePK int64
		name     string
	}
	upserted := make([]upsertedRow, 0, 2)
	for rows.Next() {
		var row upsertedRow
		if err := rows.Scan(&row.id, &row.sourcePK, &row.name); err != nil {
			t.Fatalf("scan upserted row error = %v", err)
		}
		upserted = append(upserted, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate upserted rows error = %v", err)
	}
	if len(upserted) != 2 || upserted[0].id != 10 || upserted[0].name != "更新成功" || upserted[1].id != 11 || upserted[1].name != "新增成功" {
		t.Fatalf("upserted rows = %#v", upserted)
	}
	if upserted[0].sourcePK != upserted[1].sourcePK {
		t.Fatalf("source primary-key metadata leaked into target unique constraints: %#v", upserted)
	}

	qualifiedAmbiguous := "`" + databaseName + "`.`" + ambiguousTableName + "`"
	if _, err := db.ExecContext(ctx, "CREATE TABLE "+qualifiedAmbiguous+" (`id` BIGINT NOT NULL, `email` VARCHAR(255) NOT NULL, PRIMARY KEY (`id`), UNIQUE KEY `unique_email` (`email`)) ENGINE=InnoDB"); err != nil {
		t.Fatalf("create ambiguous upsert target error = %v", err)
	}
	ambiguousOpts := plugin.TableUpsertOptions{
		Fields: []datatype.FieldInfo{
			{Name: "id", Type: datatype.FieldTypeBigInt, Nullable: false},
			{Name: "email", Type: datatype.FieldTypeString, Nullable: false},
		},
		Keys: []string{"id"},
	}
	if err := p.PrepareTableUpsert(ctx, connInfo, ambiguousPath, ambiguousOpts); err == nil || !strings.Contains(err.Error(), "unique constraints must all exactly match") {
		t.Fatalf("PrepareTableUpsert() ambiguous unique constraints error = %v", err)
	}
}

func TestIntegrationOceanBaseCatalogAndRead(t *testing.T) {
	if os.Getenv("ADDP_OCEANBASE_INTEGRATION") != "1" {
		t.Skip("set ADDP_OCEANBASE_INTEGRATION=1 to run OceanBase integration test")
	}

	const engineID uint = 94001
	p := &Plugin{}
	connInfo := oceanBaseIntegrationConnInfo()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	t.Cleanup(func() { _ = plugin.ClosePool(engineID) })

	if err := p.TestConnection(ctx, connInfo); err != nil {
		t.Fatalf("TestConnection() error = %v", err)
	}
	databaseName := oceanBaseIntegrationEnv("ADDP_TEST_OCEANBASE_DATABASE", "addp_oceanbase_disposable")
	const probeTable = "addp_catalog_probe_gate"
	const customersTable = "addp_catalog_customers_gate"
	const ordersTable = "addp_catalog_orders_gate"
	fixturePaths := []plugin.EngineCatalogPath{
		plugin.TabularItemPath(engineID, plugin.EngineCatalogTermDatabase, databaseName, probeTable),
		plugin.TabularItemPath(engineID, plugin.EngineCatalogTermDatabase, databaseName, customersTable),
		plugin.TabularItemPath(engineID, plugin.EngineCatalogTermDatabase, databaseName, ordersTable),
	}
	dsn, err := p.BuildDSN(connInfo)
	if err != nil {
		t.Fatalf("BuildDSN() error = %v", err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, path := range fixturePaths {
		if err := p.DeleteResource(ctx, connInfo, path); err != nil {
			t.Fatalf("drop stale catalog gate table error = %v", err)
		}
	}
	t.Cleanup(func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		for _, path := range fixturePaths {
			if err := p.DeleteResource(cleanupContext, connInfo, path); err != nil {
				t.Errorf("cleanup catalog gate table error = %v", err)
			}
		}
	})
	fixtureStatements := []string{
		"CREATE TABLE `" + probeTable + "` (`id` BIGINT NOT NULL PRIMARY KEY, `name` VARCHAR(255) NOT NULL, `created_at` DATETIME(6) NOT NULL) ENGINE=InnoDB",
		"INSERT INTO `" + probeTable + "` VALUES (1, 'OceanBase Community Edition', '2026-09-06 08:00:00.000001')",
		"CREATE TABLE `" + customersTable + "` (`id` BIGINT NOT NULL PRIMARY KEY, `name` VARCHAR(255) NOT NULL, `active` TINYINT(1) NOT NULL, `created_at` DATETIME(6) NOT NULL) ENGINE=InnoDB",
		"INSERT INTO `" + customersTable + "` VALUES (1, '王小丽', 1, '2026-09-06 08:00:00.000001'), (2, '李明', 1, '2026-09-06 08:00:00.000002')",
		"CREATE TABLE `" + ordersTable + "` (`id` BIGINT NOT NULL PRIMARY KEY, `order_no` VARCHAR(64) NOT NULL, `customer_id` BIGINT NOT NULL, `total_amount` DECIMAL(18,2) NOT NULL, `status` VARCHAR(32) NOT NULL, INDEX `idx_customer_id` (`customer_id`)) ENGINE=InnoDB",
		"INSERT INTO `" + ordersTable + "` VALUES (1, 'ORD-OCEANBASE-GATE-001', 1, 88.50, 'delivered')",
	}
	for _, statement := range fixtureStatements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("prepare catalog gate fixture: %v", err)
		}
	}

	root := plugin.EngineCatalogRootPath(p.EngineCatalogModel(), engineID)
	databases, err := p.ListChildren(ctx, connInfo, root, plugin.ListOptions{})
	if err != nil {
		t.Fatalf("ListChildren(root) error = %v", err)
	}
	for _, systemDatabase := range []string{"information_schema", "mysql", "oceanbase"} {
		if findOceanBaseCatalogEntry(databases, systemDatabase) != nil {
			t.Fatalf("system database %q must be filtered from %#v", systemDatabase, oceanBaseCatalogEntryNames(databases))
		}
	}
	businessDatabase := findOceanBaseCatalogEntry(databases, databaseName)
	if businessDatabase == nil {
		t.Fatalf("database %q not found in %#v", databaseName, oceanBaseCatalogEntryNames(databases))
	}

	items, err := p.ListChildren(ctx, connInfo, businessDatabase.Path, plugin.ListOptions{})
	if err != nil {
		t.Fatalf("ListChildren(database) error = %v", err)
	}
	for _, tableName := range []string{probeTable, customersTable, ordersTable} {
		entry := findOceanBaseCatalogEntry(items, tableName)
		if entry == nil || entry.Kind != plugin.EngineCatalogKindTable {
			t.Fatalf("%s table not found in %#v", tableName, oceanBaseCatalogEntryNames(items))
		}
	}
	probe := findOceanBaseCatalogEntry(items, probeTable)
	if probe == nil || probe.Kind != plugin.EngineCatalogKindTable {
		t.Fatalf("%s table not found in %#v", probeTable, oceanBaseCatalogEntryNames(items))
	}

	facts, err := p.DescribeEngineCatalogFacts(ctx, connInfo, probe.Path, plugin.EngineCatalogFactsOptions{IncludeStatistics: true})
	if err != nil {
		t.Fatalf("DescribeEngineCatalogFacts() error = %v", err)
	}
	if facts.Table == nil || facts.Table.RowCount == nil || *facts.Table.RowCount != 1 {
		t.Fatalf("%s facts = %#v, want row_count=1", probeTable, facts.Table)
	}
	assertOceanBaseIntegrationField(t, facts.Table.Fields, "id", datatype.FieldTypeBigInt, true)
	assertOceanBaseIntegrationField(t, facts.Table.Fields, "name", datatype.FieldTypeString, false)
	assertOceanBaseIntegrationField(t, facts.Table.Fields, "created_at", datatype.FieldTypeTimestamp, false)

	batch, err := p.ReadBatch(ctx, connInfo, probe.Path, plugin.BatchReadOptions{Limit: 10})
	if err != nil {
		t.Fatalf("ReadBatch() error = %v", err)
	}
	if len(batch.Rows) != 1 || batch.Rows[0]["name"] != "OceanBase Community Edition" {
		t.Fatalf("ReadBatch() rows = %#v", batch.Rows)
	}

	customers := findOceanBaseCatalogEntry(items, customersTable)
	customerFacts, err := p.DescribeEngineCatalogFacts(ctx, connInfo, customers.Path, plugin.EngineCatalogFactsOptions{IncludeStatistics: true, IncludeIndexes: true, IncludeConstraints: true})
	if err != nil {
		t.Fatalf("DescribeEngineCatalogFacts(customers) error = %v", err)
	}
	if customerFacts.Table == nil || customerFacts.Table.RowCount == nil || *customerFacts.Table.RowCount != 2 {
		t.Fatalf("customers facts = %#v, want row_count=2", customerFacts.Table)
	}
	assertOceanBaseIntegrationField(t, customerFacts.Table.Fields, "id", datatype.FieldTypeBigInt, true)
	assertOceanBaseIntegrationField(t, customerFacts.Table.Fields, "active", datatype.FieldTypeBool, false)
	assertOceanBaseIntegrationField(t, customerFacts.Table.Fields, "created_at", datatype.FieldTypeTimestamp, false)

	prepared, err := p.PrepareQuery(ctx, connInfo, plugin.QueryRequest{
		EngineID:   engineID,
		Language:   "sql",
		Query:      "SELECT name FROM `" + databaseName + "`.`" + probeTable + "` WHERE id = :id",
		TargetPath: &probe.Path,
		Options: plugin.QueryOptions{
			ReadOnly:   true,
			Limit:      1,
			Parameters: map[string]interface{}{"id": 1},
		},
	})
	if err != nil {
		t.Fatalf("PrepareQuery() error = %v", err)
	}
	readSet, err := prepared.ReadSet(ctx)
	if err != nil {
		t.Fatalf("PreparedQuery.ReadSet() error = %v", err)
	}
	if len(readSet.Paths) != 1 || readSet.Paths[0].StringPath() != databaseName+"/"+probeTable {
		t.Fatalf("PreparedQuery.ReadSet() = %#v", readSet.Paths)
	}
	lineage, err := prepared.OutputLineage(ctx)
	if err != nil {
		t.Fatalf("PreparedQuery.OutputLineage() error = %v", err)
	}
	if len(lineage.Sources) != 1 || len(lineage.Sources[0].Bindings) != 1 || lineage.Sources[0].Bindings[0].OutputPath[0] != "name" {
		t.Fatalf("PreparedQuery.OutputLineage() = %#v", lineage.Sources)
	}
	result, err := prepared.Execute(ctx)
	if err != nil {
		t.Fatalf("PreparedQuery.Execute() error = %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0]["name"] != "OceanBase Community Edition" {
		t.Fatalf("PreparedQuery.Execute() rows = %#v", result.Rows)
	}

	orders := findOceanBaseCatalogEntry(items, ordersTable)
	prepared, err = p.PrepareQuery(ctx, connInfo, plugin.QueryRequest{
		EngineID: engineID,
		Language: "sql",
		Query: "SELECT o.order_no, c.name AS customer_name, o.total_amount " +
			"FROM `" + databaseName + "`.`" + ordersTable + "` o " +
			"JOIN `" + databaseName + "`.`" + customersTable + "` c ON c.id = o.customer_id " +
			"WHERE o.status = :status ORDER BY o.id LIMIT 1",
		TargetPath: &orders.Path,
		Options: plugin.QueryOptions{
			ReadOnly:   true,
			Limit:      1,
			Parameters: map[string]interface{}{"status": "delivered"},
		},
	})
	if err != nil {
		t.Fatalf("PrepareQuery(join) error = %v", err)
	}
	result, err = prepared.Execute(ctx)
	if err != nil {
		t.Fatalf("PreparedQuery.Execute(join) error = %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0]["order_no"] != "ORD-OCEANBASE-GATE-001" || result.Rows[0]["customer_name"] != "王小丽" {
		t.Fatalf("PreparedQuery.Execute(join) rows = %#v", result.Rows)
	}

	if _, err := p.PrepareQuery(ctx, connInfo, plugin.QueryRequest{
		EngineID: engineID,
		Language: "sql",
		Query:    "UPDATE `" + databaseName + "`.`" + probeTable + "` SET name = 'unexpected' WHERE id = 1",
		Options:  plugin.QueryOptions{ReadOnly: true},
	}); err == nil {
		t.Fatal("PrepareQuery() must reject a write statement in read-only mode")
	}
}

func oceanBaseIntegrationConnInfo() plugin.ConnectionInfo {
	tenant := oceanBaseIntegrationEnv("ADDP_TEST_OCEANBASE_TENANT", "test")
	return plugin.ConnectionInfo{
		"host":     oceanBaseIntegrationEnv("ADDP_TEST_OCEANBASE_HOST", "127.0.0.1"),
		"port":     oceanBaseIntegrationEnv("ADDP_TEST_OCEANBASE_PORT", "2881"),
		"database": oceanBaseIntegrationEnv("ADDP_TEST_OCEANBASE_DATABASE", "addp_oceanbase_disposable"),
		"user":     oceanBaseIntegrationEnv("ADDP_TEST_OCEANBASE_USER", "root@"+tenant),
		"password": oceanBaseIntegrationEnv("ADDP_TEST_OCEANBASE_PASSWORD", ""),
	}
}

func oceanBaseIntegrationEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func findOceanBaseCatalogEntry(entries []plugin.EngineCatalogEntry, name string) *plugin.EngineCatalogEntry {
	for index := range entries {
		if strings.EqualFold(entries[index].Name, name) {
			return &entries[index]
		}
	}
	return nil
}

func oceanBaseCatalogEntryNames(entries []plugin.EngineCatalogEntry) []string {
	names := make([]string, len(entries))
	for index := range entries {
		names[index] = entries[index].Name
	}
	return names
}

func assertOceanBaseIntegrationField(t *testing.T, fields []datatype.FieldInfo, name string, fieldType datatype.FieldType, primaryKey bool) {
	t.Helper()
	for _, field := range fields {
		if strings.EqualFold(field.Name, name) {
			if field.Type != fieldType || field.PrimaryKey != primaryKey {
				t.Fatalf("field %s = %#v, want type=%s primary_key=%v", name, field, fieldType, primaryKey)
			}
			return
		}
	}
	t.Fatalf("field %s not found in %#v", name, fields)
}
