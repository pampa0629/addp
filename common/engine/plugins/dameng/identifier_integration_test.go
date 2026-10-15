//go:build dameng_official

package dameng

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonquery "github.com/addp/common/query"
	"github.com/google/uuid"
)

// Invoked by the existing ProviderContract test so the standard DM8 gate
// discovers this regression without a second test route.
func testDM8QuotedIdentifiers(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 180*time.Second)
	defer cancel()
	p := &Plugin{}
	connInfo := integrationConnInfo()
	db, err := p.openDB(connInfo)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	schema, err := p.currentSchema(ctx, connInfo)
	if err != nil {
		t.Fatal(err)
	}
	dialect := commonquery.ForDialect(p.SQLDialect())
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	upper := "ADDP_CASE_" + strings.ToUpper(suffix)
	names := []string{upper, strings.ToLower(upper), "ADDP_Case_" + suffix, "ADDP_quote_'\"_" + suffix}
	for _, name := range names {
		var count int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM USER_OBJECTS WHERE OBJECT_TYPE='TABLE' AND OBJECT_NAME=?", name).Scan(&count); err != nil || count != 0 {
			t.Fatalf("refusing occupied fixture name %q: count=%d error=%v", name, count, err)
		}
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, name := range names {
			path := plugin.TabularItemPath(98001, plugin.EngineCatalogTermSchema, schema, name)
			if err := p.DeleteResource(cleanup, connInfo, path); err != nil {
				t.Errorf("Provider cleanup %q: %v", name, err)
			}
			var count int
			if err := db.QueryRowContext(cleanup, "SELECT COUNT(*) FROM USER_OBJECTS WHERE OBJECT_TYPE='TABLE' AND OBJECT_NAME=?", name).Scan(&count); err != nil {
				t.Errorf("cleanup facts %q: %v", name, err)
				continue
			}
			if count != 0 {
				// If the regression is present, still remove only this test's
				// preflighted random fixture; never leave a broken test table.
				t.Errorf("Provider DeleteResource left %q behind", name)
				if _, err := db.ExecContext(cleanup, "DROP TABLE "+dialect.QualifiedTable(schema, name)); err != nil {
					t.Errorf("owned fixture cleanup %q: %v", name, err)
				}
			}
			if err := db.QueryRowContext(cleanup, "SELECT COUNT(*) FROM USER_OBJECTS WHERE OBJECT_TYPE='TABLE' AND OBJECT_NAME=?", name).Scan(&count); err != nil || count != 0 {
				t.Errorf("zero residue not proven for %q: count=%d error=%v", name, count, err)
			}
		}
	})
	// Uppercase and lowercase twins are physically distinct in the certified
	// DM8 fixture. Give them different fields so wrong catalog binding is visible.
	for index, name := range names {
		t.Run(name, func(t *testing.T) {
			path := plugin.TabularItemPath(98001, plugin.EngineCatalogTermSchema, schema, name)
			key, doc, binary := "keyId", "Doc", "bytes"
			if index == 0 {
				key, doc, binary = "ID", "UPPER_DOC", "UPPER_BYTES"
			}
			fields := []datatype.FieldInfo{{Name: key, Type: datatype.FieldTypeBigInt, PrimaryKey: true}, {Name: doc, Type: datatype.FieldTypeJSON, Nullable: true}, {Name: binary, Type: datatype.FieldTypeBytes, Nullable: true}}
			if err := p.PrepareTableWrite(ctx, connInfo, path, plugin.TableWriteOptions{Fields: fields}); err != nil {
				t.Fatal(err)
			}
			if err := p.PrepareTableWrite(ctx, connInfo, path, plugin.TableWriteOptions{Fields: fields}); err != nil {
				t.Fatalf("prepare same quoted table twice: %v", err)
			}
			for rowID := int64(1); rowID <= 2; rowID++ {
				session, err := p.OpenTableWriteSession(ctx, connInfo, path, plugin.TableWriteSessionOptions{Fields: fields})
				if err != nil {
					t.Fatal(err)
				}
				defer session.Abort(context.Background())
				if err := session.WriteBatch(ctx, &plugin.BatchData{Rows: []map[string]interface{}{{key: rowID, doc: map[string]interface{}{"table": name}, binary: []byte{0, 255}}}}); err != nil {
					t.Fatal(err)
				}
				if err := session.Close(ctx); err != nil {
					t.Fatal(err)
				}
			}
			entry, err := p.ResolvePath(ctx, connInfo, path)
			if err != nil || entry.Table == nil || entry.Table.Name != name || len(entry.Table.Fields) != 0 || len(entry.Table.PrimaryKey) != 0 {
				t.Fatalf("exact lightweight ResolvePath: entry=%+v error=%v", entry, err)
			}
			facts, err := p.DescribeEngineCatalogFacts(ctx, connInfo, path, plugin.EngineCatalogFactsOptions{IncludeStatistics: true, IncludeConstraints: true})
			if err != nil {
				t.Fatal(err)
			}
			if facts.Table.Name != name || len(facts.Table.Fields) != 3 || facts.Table.GetField(doc) == nil || len(facts.Table.PrimaryKey) != 1 || facts.Table.PrimaryKey[0] != key || facts.Table.RowCount == nil || *facts.Table.RowCount != 2 {
				t.Fatalf("wrong object facts: %+v", facts.Table)
			}
			foundKey := false
			for _, constraint := range facts.Constraints {
				if constraint.ConstraintType == plugin.ConstraintTypePrimaryKey && len(constraint.Fields) == 1 && constraint.Fields[0] == key {
					foundKey = true
				}
			}
			if !foundKey {
				t.Fatalf("wrong constraint facts: %+v", facts.Constraints)
			}
			upsert := plugin.TableUpsertOptions{Fields: fields, Keys: []string{key}}
			if err := p.PrepareTableUpsert(ctx, connInfo, path, upsert); err != nil {
				t.Fatal(err)
			}
			batch := &plugin.BatchData{Fields: fields, Rows: []map[string]interface{}{{key: int64(2), doc: map[string]interface{}{"updated": name}, binary: []byte{1, 255}}}}
			if err := p.UpsertBatch(ctx, connInfo, path, batch, upsert); err != nil {
				t.Fatal(err)
			}
			var writtenDocument string
			var writtenBytes []byte
			statement := "SELECT " + dialect.QuoteIdentifier(doc) + ", " + dialect.QuoteIdentifier(binary) + " FROM " + dialect.QualifiedTable(schema, name) + " WHERE " + dialect.QuoteIdentifier(key) + " = ?"
			if err := db.QueryRowContext(ctx, statement, int64(2)).Scan(&writtenDocument, &writtenBytes); err != nil {
				t.Fatal(err)
			}
			wantDocument, _ := json.Marshal(map[string]interface{}{"updated": name})
			if writtenDocument != string(wantDocument) || !bytes.Equal(writtenBytes, []byte{1, 255}) {
				t.Fatalf("quoted MERGE changed JSON/BLOB: document=%q bytes=%v", writtenDocument, writtenBytes)
			}
			if index != 0 {
				if err := p.validateUniqueFields(ctx, connInfo, path, []string{"KEYID"}); err == nil {
					t.Fatal("different quoted key name accepted")
				}
			}
			read, err := p.OpenBoundedWatermarkRead(ctx, connInfo, path, plugin.BoundedWatermarkReadOptions{WatermarkField: key})
			if err != nil {
				t.Fatal(err)
			}
			defer read.Close(context.Background())
			rows, err := read.ReadBatch(ctx, 10)
			if err != nil || len(rows.Rows) != 2 {
				t.Fatalf("quoted watermark read: rows=%+v error=%v", rows, err)
			}
			if err := read.Close(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
	// Removing the lowercase twin must preserve the uppercase table and its rows.
	lowerPath := plugin.TabularItemPath(98001, plugin.EngineCatalogTermSchema, schema, names[1])
	if err := p.DeleteResource(ctx, connInfo, lowerPath); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM USER_OBJECTS WHERE OBJECT_TYPE='TABLE' AND OBJECT_NAME=?", names[1]).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("lowercase delete failed: count=%d error=%v", remaining, err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+dialect.QualifiedTable(schema, upper)).Scan(&remaining); err != nil || remaining != 2 {
		t.Fatalf("uppercase sentinel changed: rows=%d error=%v", remaining, err)
	}
	if _, err := p.ResolvePath(ctx, connInfo, lowerPath); !plugin.IsEngineCatalogErrorKind(err, plugin.EngineCatalogErrorNotFound) {
		t.Fatalf("missing lowercase path resolved uppercase twin: %v", err)
	}
	if _, err := p.ResolvePath(ctx, connInfo, plugin.TabularNamespacePath(98001, plugin.EngineCatalogTermSchema, strings.ToLower(schema))); !plugin.IsEngineCatalogErrorKind(err, plugin.EngineCatalogErrorNotFound) {
		t.Fatalf("noncanonical schema resolved: %v", err)
	}
}
