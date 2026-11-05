package clickhouse

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/google/uuid"
)

func TestIntegrationClickHouseBinaryTableRead(t *testing.T) {
	if os.Getenv("ADDP_CLICKHOUSE_INTEGRATION") != "1" {
		t.Skip("requires the disposable ClickHouse gate")
	}
	ctx := t.Context()
	conn := clickhouseIntegrationConnInfo(t)
	p := &ClickHousePlugin{}
	dbName := plugin.GetString(conn, "database")
	tableName := "binary_read_" + uuid.NewString()
	path := plugin.TabularItemPath(93003, plugin.EngineCatalogTermDatabase, dbName, tableName)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := p.DeleteResource(ctx, conn, path); err != nil {
			t.Error(err)
		}
	})
	fields := []datatype.FieldInfo{
		{Name: "id", Type: datatype.FieldTypeInt},
		{Name: "Payload", Type: datatype.FieldTypeBytes, Nullable: true, Comment: "Binary's \\description\nSecond line"},
		{Name: "payload", Type: datatype.FieldTypeString},
	}
	if err := p.PrepareTableWrite(ctx, conn, path, plugin.TableWriteOptions{Fields: fields}); err != nil {
		t.Fatal(err)
	}
	unmarked := append([]datatype.FieldInfo(nil), fields...)
	unmarked[2].Type = datatype.FieldTypeBytes
	if err := p.PrepareTableWrite(ctx, conn, path, plugin.TableWriteOptions{Fields: unmarked}); err == nil {
		t.Fatal("write preparation silently accepted an unmarked existing String column as bytes")
	}
	fields = append(fields, datatype.FieldInfo{Name: "extra", Type: datatype.FieldTypeBytes, Nullable: true})
	if err := p.PrepareTableWrite(ctx, conn, path, plugin.TableWriteOptions{Fields: fields}); err != nil {
		t.Fatal(err)
	}
	raw := []byte{0x41, 0, 0xff, 0x42}
	if err := p.WriteBatch(ctx, conn, path, &plugin.BatchData{Fields: fields, Rows: []map[string]interface{}{
		{"id": 1, "Payload": raw, "payload": "中文", "extra": []byte("AB")},
		{"id": 2, "Payload": []byte{}, "payload": "AB"},
		{"id": 3, "Payload": nil, "payload": "null"},
	}}, plugin.BatchWriteOptions{}); err != nil {
		t.Fatal(err)
	}
	facts, err := p.DescribeEngineCatalogFacts(ctx, conn, path, plugin.EngineCatalogFactsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range []int{1, 3} {
		if field := facts.Table.Fields[i]; field.Type != datatype.FieldTypeBytes || field.NativeType != "Nullable(String)" || field.Comment != fields[i].Comment {
			t.Fatalf("unexpected binary catalog field: %+v", field)
		}
	}
	page, err := p.ReadCatalogTablePage(ctx, conn, path, plugin.CatalogTablePageOptions{Limit: 1, Predicate: "id = ?", Args: []interface{}{1}})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Rows) != 1 || page.Fields[1].Type != datatype.FieldTypeBytes || page.Fields[2].Type != datatype.FieldTypeString {
		t.Fatalf("unexpected page: %#v", page)
	}
	got, ok := page.Rows[0]["Payload"].([]byte)
	if !ok || !bytes.Equal(got, raw) || page.Rows[0]["payload"] != "中文" || !bytes.Equal(page.Rows[0]["extra"].([]byte), []byte("AB")) {
		t.Fatalf("unexpected page values: %#v", page.Rows)
	}
	emptyPage, err := p.ReadCatalogTablePage(ctx, conn, path, plugin.CatalogTablePageOptions{Limit: 1, Offset: 1, Predicate: "id = ?", Args: []interface{}{1}})
	if err != nil || emptyPage.Offset != 1 || len(emptyPage.Rows) != 0 || emptyPage.Fields[1].Type != datatype.FieldTypeBytes {
		t.Fatalf("unexpected empty page: %#v, err=%v", emptyPage, err)
	}
	session, err := p.OpenTableReadSession(ctx, conn, path, plugin.TableReadSessionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(ctx)
	observed := make(map[int32]interface{})
	for offset := int64(0); offset < 3; offset++ {
		batch, err := session.ReadBatch(ctx, 1)
		if err != nil || len(batch.Rows) != 1 || batch.Offset != offset {
			t.Fatalf("unexpected stream batch: %#v, err=%v", batch, err)
		}
		row := batch.Rows[0]
		observed[row["id"].(int32)] = row["Payload"]
	}
	first, ok := observed[1].([]byte)
	empty, emptyOK := observed[2].([]byte)
	if !ok || !bytes.Equal(first, raw) || !emptyOK || empty == nil || len(empty) != 0 || observed[3] != nil {
		t.Fatalf("unexpected stream values: %#v", observed)
	}
	if err := session.Close(ctx); err != nil {
		t.Fatal(err)
	}
	rawSQL, err := p.ReadBatch(ctx, conn, path, plugin.BatchReadOptions{Query: "SELECT 'AB' AS Payload"})
	if err != nil || rawSQL.Fields[0].Type != datatype.FieldTypeString || rawSQL.Rows[0]["Payload"] != "AB" {
		t.Fatalf("arbitrary SQL inherited the table marker: %#v, err=%v", rawSQL, err)
	}
}
