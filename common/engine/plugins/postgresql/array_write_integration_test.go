package postgresql

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/lib/pq"
)

func TestIntegrationPostgresTypedArrayWrites(t *testing.T) {
	db, pg, connInfo := openPostgresPrepareIntegration(t, false)
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	schema := fmt.Sprintf("array_write_%d", time.Now().UnixNano())
	invalidSchema := schema + "_invalid"
	t.Cleanup(func() {
		for _, ownedSchema := range []string{schema, invalidSchema} {
			if _, err := db.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+pq.QuoteIdentifier(ownedSchema)+" CASCADE"); err != nil {
				t.Errorf("cleanup array schema: %v", err)
			}
			var exists bool
			if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname=$1)`, ownedSchema).Scan(&exists); err != nil || exists {
				t.Errorf("array fixture cleanup not proven: exists=%v error=%v", exists, err)
			}
		}
	})
	fields := []datatype.FieldInfo{
		{Name: "id", Type: datatype.FieldTypeBigInt, PrimaryKey: true},
		{Name: "members", Type: datatype.FieldTypeArray, ElementType: datatype.FieldTypeString, Nullable: true},
	}
	cases := []struct {
		value interface{}
		json  interface{}
	}{
		{[]string{"户外", "", "NULL", "a,b", "{x}", "a\"b", "a\\b", "a\nb"}, `["户外","","NULL","a,b","{x}","a\"b","a\\b","a\nb"]`},
		{[]interface{}{"a", nil, "NULL", ""}, `["a",null,"NULL",""]`},
		{[2]string{"one", "two"}, `["one","two"]`},
		{[]string{}, `[]`}, {[]interface{}{}, `[]`}, {[0]string{}, `[]`},
		{nil, nil}, {[]string(nil), nil}, {[]interface{}(nil), nil}, {[]byte(nil), nil},
		{`{"a,b",NULL,"NULL","","a\\b"}`, `["a,b",null,"NULL","","a\\b"]`},
		{[]byte(`{"户外",NULL}`), `["户外",null]`}, {`{}`, `[]`},
		{`{null,NuLl,"null"," NULL ",a b}`, `[null,null,"null"," NULL ","a b"]`},
		{`{ a , b }`, `["a","b"]`}, {`{a\\b}`, `["a\\b"]`},
	}
	for _, method := range []string{"session", "copy", "insert", "upsert"} {
		t.Run(method, func(t *testing.T) {
			path := postgresPrepareTablePath(schema, method)
			if err := pg.PrepareTableWrite(ctx, connInfo, path, plugin.TableWriteOptions{Fields: fields}); err != nil {
				t.Fatal(err)
			}
			// Existing text[] must also pass preparation, without schema changes.
			if err := pg.PrepareTableWrite(ctx, connInfo, path, plugin.TableWriteOptions{Fields: fields}); err != nil {
				t.Fatal(err)
			}
			assertPostgresPrepareColumn(t, ctx, db, schema, method, "members", "ARRAY", "YES", "")
			table := pq.QuoteIdentifier(schema) + "." + pq.QuoteIdentifier(method)
			write := func(rows []map[string]interface{}) error {
				batch := &plugin.BatchData{Fields: fields, Rows: rows, Hints: map[string]interface{}{"chunk_size": 2}}
				if method == "session" {
					optsFields := append([]datatype.FieldInfo(nil), fields...)
					session, err := pg.OpenTableWriteSession(ctx, connInfo, path, plugin.TableWriteSessionOptions{Fields: optsFields})
					if err != nil {
						return err
					}
					defer session.Abort(ctx)
					optsFields[1].Type = datatype.FieldTypeJSON
					batch.Fields = []datatype.FieldInfo{{Name: "members", Type: datatype.FieldTypeJSON}}
					for start := 0; start < len(rows); start += 2 {
						batch.Rows = rows[start:min(start+2, len(rows))]
						if err := session.WriteBatch(ctx, batch); err != nil {
							return err
						}
					}
					return session.Close(ctx)
				}
				if method == "upsert" {
					batch.Fields = nil
					return pg.UpsertBatch(ctx, connInfo, path, batch, plugin.TableUpsertOptions{Fields: fields, Keys: []string{"id"}})
				}
				return pg.WriteBatch(ctx, connInfo, path, batch, plugin.BatchWriteOptions{Method: method})
			}
			rows := make([]map[string]interface{}, len(cases))
			for i, tc := range cases {
				rows[i] = map[string]interface{}{"id": i + 1, "members": tc.value}
			}
			if err := write(rows); err != nil {
				t.Fatal(err)
			}
			for i, tc := range cases {
				var ok bool
				if err := db.QueryRowContext(ctx, `SELECT array_to_json(members)::jsonb IS NOT DISTINCT FROM $1::jsonb FROM `+table+` WHERE id=$2`, tc.json, i+1).Scan(&ok); err != nil || !ok {
					t.Fatalf("row %d (%T): preserved=%v error=%v", i+1, tc.value, ok, err)
				}
			}
			if method == "upsert" {
				if err := write([]map[string]interface{}{{"id": 1, "members": []interface{}{nil, "changed"}}}); err != nil {
					t.Fatal(err)
				}
				var ok bool
				if err := db.QueryRowContext(ctx, `SELECT members IS NOT DISTINCT FROM ARRAY[NULL,'changed']::text[] FROM `+table+` WHERE id=1`).Scan(&ok); err != nil || !ok {
					t.Fatalf("matched upsert: %v %v", ok, err)
				}
			}
			badValues := []interface{}{
				[]interface{}{"valid", map[string]interface{}{"id": 1}}, []interface{}{"valid", []string{"nested"}},
				[]interface{}{"valid", 1}, []interface{}{"valid", true}, []interface{}{"valid", []byte("binary")},
				[]interface{}{"valid", "nul\x00"}, []interface{}{"valid", string([]byte{0xff})},
				`["json"]`, `{{"multi"},{"dimension"}}`, `[0:1]={a,b}`, `[1:2]={a,b}`, `{"unterminated}`, []byte{}, 42,
			}
			for _, bad := range badValues {
				err := write([]map[string]interface{}{{"id": 1000, "members": []string{"valid"}}, {"id": 1001, "members": bad}})
				if err == nil || !strings.Contains(err.Error(), `column "members"`) || !strings.Contains(err.Error(), "row 1") {
					t.Fatalf("invalid %T lacks row/field location: %v", bad, err)
				}
				if strings.HasPrefix(fmt.Sprintf("%T", bad), "[]interface") && !strings.Contains(err.Error(), "element 1") {
					t.Fatalf("missing element location: %v", err)
				}
				var count int
				if err := db.QueryRowContext(ctx, `SELECT count(*) FROM `+table).Scan(&count); err != nil || count != len(cases) {
					t.Fatalf("failed write changed rows: count=%d error=%v", count, err)
				}
			}
			// A late failure rolls back the session/INSERT/upsert transaction.
			// Batch COPY commits each completed chunk independently.
			err := write([]map[string]interface{}{
				{"id": 2000, "members": []string{"first"}}, {"id": 2001, "members": []string{"second"}},
				{"id": 2002, "members": []string{"third"}}, {"id": 2003, "members": []interface{}{"valid", true}},
			})
			if err == nil || !strings.Contains(err.Error(), "element 1") {
				t.Fatalf("expected late array error: %v", err)
			}
			wantCount := len(cases)
			if method == "copy" {
				wantCount += 2
			}
			var count int
			if err := db.QueryRowContext(ctx, `SELECT count(*) FROM `+table).Scan(&count); err != nil || count != wantCount {
				t.Fatalf("late failure count=%d want=%d error=%v", count, wantCount, err)
			}
			if err := write([]map[string]interface{}{{"id": 3000, "members": []string{"recovery"}}}); err != nil {
				t.Fatalf("new write after failure: %v", err)
			}
		})
	}
	// Unsupported element declarations must fail before even creating a schema.
	for _, elementType := range []datatype.FieldType{datatype.FieldTypeJSON, datatype.FieldTypeInt, datatype.FieldTypeArray, datatype.FieldTypeMixed} {
		err := pg.PrepareTableWrite(ctx, connInfo, postgresPrepareTablePath(invalidSchema, "bad"), plugin.TableWriteOptions{Fields: []datatype.FieldInfo{{Name: "members", Type: datatype.FieldTypeArray, ElementType: elementType}}})
		if err == nil || !strings.Contains(err.Error(), `"members"`) {
			t.Fatalf("accepted array element type %s: %v", elementType, err)
		}
	}
	var exists bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname=$1)`, invalidSchema).Scan(&exists); err != nil || exists {
		t.Fatalf("invalid declaration performed DDL: exists=%v error=%v", exists, err)
	}
	// Other native array element types are not interchangeable with TEXT[].
	if _, err := db.ExecContext(ctx, "CREATE TABLE "+pq.QuoteIdentifier(schema)+`.wrong (members integer[])`); err != nil {
		t.Fatal(err)
	}
	if err := pg.PrepareTableWrite(ctx, connInfo, postgresPrepareTablePath(schema, "wrong"), plugin.TableWriteOptions{Fields: fields[1:]}); err == nil {
		t.Fatal("accepted integer[] target as text[]")
	}
	// Real source arrays arrive as text values. Their declared element types
	// must survive reading so numeric/object arrays cannot masquerade as text[].
	if _, err := db.ExecContext(ctx, "CREATE TABLE "+pq.QuoteIdentifier(schema)+`.native_arrays (labels text[], numbers integer[], objects jsonb[], amounts numeric(8,2)[]);
		INSERT INTO `+pq.QuoteIdentifier(schema)+`.native_arrays VALUES (ARRAY['a',NULL],ARRAY[1,2],ARRAY['{"nested":[1,true]}'::jsonb],ARRAY[1.25]::numeric(8,2)[])`); err != nil {
		t.Fatal(err)
	}
	reader, err := pg.OpenTableReadSession(ctx, connInfo, postgresPrepareTablePath(schema, "native_arrays"), plugin.TableReadSessionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close(ctx)
	batch, err := reader.ReadBatch(ctx, 10)
	if err != nil || batch == nil || len(batch.Rows) != 1 || len(batch.Fields) != 4 {
		t.Fatalf("read native array fixture: %v", err)
	}
	wantElements := map[string]datatype.FieldType{"labels": datatype.FieldTypeString, "numbers": datatype.FieldTypeInt, "objects": datatype.FieldTypeJSON, "amounts": datatype.FieldTypeDecimal}
	for _, field := range batch.Fields {
		if field.Type != datatype.FieldTypeArray || field.ElementType != wantElements[field.Name] {
			t.Fatalf("native array element facts lost: %#v", field)
		}
		if field.Name == "labels" {
			if encoded, err := postgresWriteValue(batch.Rows[0][field.Name], field.Type); err != nil || encoded != batch.Rows[0][field.Name] {
				t.Fatalf("native text[] read/write encoding: %#v %v", encoded, err)
			}
			continue
		}
		_, err := pg.OpenTableWriteSession(ctx, nil, postgresPrepareTablePath(invalidSchema, "from_native"), plugin.TableWriteSessionOptions{Fields: []datatype.FieldInfo{field}})
		if err == nil || !strings.Contains(err.Error(), "element_type") {
			t.Fatalf("native non-text array reached connection opening: %s %v", field.Name, err)
		}
	}
}
