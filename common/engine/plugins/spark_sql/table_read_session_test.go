package spark_sql

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

func TestSparkTableReadSessionNormalizesTemporalValuesForArrow(t *testing.T) {
	fields := []datatype.FieldInfo{
		{Name: "business_date", Type: datatype.FieldTypeDate},
		{Name: "created_at", Type: datatype.FieldTypeTimestamp},
	}
	session := &sparkTableReadSession{
		cursor: &sparkTableCursorStub{rows: []map[string]interface{}{{
			"business_date": "2026-08-04",
			"created_at":    "2026-08-04 11:52:03.123456",
		}}},
		fields: fields,
	}

	batch, err := session.ReadBatch(context.Background(), 10)
	if err != nil {
		t.Fatalf("ReadBatch() error = %v", err)
	}
	if len(batch.Rows) != 1 {
		t.Fatalf("ReadBatch() rows = %d, want 1", len(batch.Rows))
	}
	for _, name := range []string{"business_date", "created_at"} {
		if _, ok := batch.Rows[0][name].(time.Time); !ok {
			t.Fatalf("ReadBatch() field %q type = %T, want time.Time", name, batch.Rows[0][name])
		}
	}

	var payload bytes.Buffer
	writer, err := plugin.NewBatchArrowStreamWriter(&payload, fields)
	if err != nil {
		t.Fatalf("NewBatchArrowStreamWriter() error = %v", err)
	}
	if err := writer.WriteBatch(batch); err != nil {
		t.Fatalf("WriteBatch() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestSparkTableReadSessionRejectsInvalidTemporalValue(t *testing.T) {
	session := &sparkTableReadSession{
		cursor: &sparkTableCursorStub{rows: []map[string]interface{}{{"created_at": "not-a-timestamp"}}},
		fields: []datatype.FieldInfo{{Name: "created_at", Type: datatype.FieldTypeTimestamp}},
	}

	_, err := session.ReadBatch(context.Background(), 10)
	if err == nil || !strings.Contains(err.Error(), `field "created_at"`) || !strings.Contains(err.Error(), "timestamp") {
		t.Fatalf("ReadBatch() error = %v, want field-specific timestamp error", err)
	}
}

type sparkTableCursorStub struct {
	rows   []map[string]interface{}
	index  int
	err    error
	closed bool
}

func (c *sparkTableCursorStub) HasMore(context.Context) bool {
	return c.err == nil && c.index < len(c.rows)
}

func (c *sparkTableCursorStub) RowMap(context.Context) map[string]interface{} {
	if !c.HasMore(context.Background()) {
		return nil
	}
	row := c.rows[c.index]
	c.index++
	return row
}

func (c *sparkTableCursorStub) Error() error {
	return c.err
}

func (c *sparkTableCursorStub) Close() {
	c.closed = true
}
