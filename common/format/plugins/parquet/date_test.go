package parquet

import (
	"bytes"
	"testing"

	"github.com/addp/common/datatype"
	parquetgo "github.com/parquet-go/parquet-go"
	"github.com/stretchr/testify/require"
)

// Build physical INT32 rows independently of the ADDP writer. A plain INT32
// with the same value must remain an integer, while DATE is a calendar day.
func dateFixture(t *testing.T, days ...*int32) []byte {
	t.Helper()
	schema := parquetgo.NewSchema("dates", parquetgo.Group{
		"amount":     parquetgo.Decimal(2, 9, parquetgo.Int32Type),
		"day":        parquetgo.Optional(parquetgo.Date()),
		"plain_days": parquetgo.Int(32),
	})
	var buffer bytes.Buffer
	writer := parquetgo.NewWriter(&buffer, schema, parquetgo.MaxRowsPerRowGroup(1))
	for _, day := range days {
		value := parquetgo.NullValue().Level(0, 0, 1)
		if day != nil {
			value = parquetgo.Int32Value(*day).Level(0, 1, 1)
		}
		_, err := writer.WriteRows([]parquetgo.Row{{parquetgo.Int32Value(1234).Level(0, 0, 0), value, parquetgo.Int32Value(20763).Level(0, 0, 2)}})
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	return buffer.Bytes()
}

func TestParquetDateLogicalValuesOnPagedAndContinuousReads(t *testing.T) {
	values := []int32{-719162, -1, 0, 11016, 20763, 2932896}
	dates := make([]*int32, 0, len(values)+1)
	for i := range values {
		dates = append(dates, &values[i])
	}
	dates = append(dates, nil)
	want := []any{"0001-01-01", "1969-12-31", "1970-01-01", "2000-02-29", "2026-11-06", "9999-12-31", nil}
	raw := dateFixture(t, dates...)
	p := NewPlugin()
	description, err := p.DescribeTable(t.Context(), bytes.NewReader(raw), nil)
	require.NoError(t, err)
	require.Equal(t, datatype.FieldTypeDate, description.Table.GetField("day").Type)
	require.Equal(t, int64(len(want)), *description.Table.RowCount)
	reader, err := p.OpenTableReader(t.Context(), bytes.NewReader(raw), nil)
	require.NoError(t, err)
	defer reader.Close(t.Context())
	for i, date := range want {
		for route, read := range map[string]func() ([]map[string]interface{}, error){
			"page": func() ([]map[string]interface{}, error) {
				return p.SampleTable(t.Context(), bytes.NewReader(raw), int64(i), 1, nil)
			},
			"stream": func() ([]map[string]interface{}, error) { return reader.ReadRows(t.Context(), 1) },
		} {
			rows, err := read()
			require.NoError(t, err, route)
			require.Len(t, rows, 1, route)
			require.Equal(t, date, rows[0]["day"], route)
			require.Equal(t, int32(20763), rows[0]["plain_days"], route)
			require.Equal(t, "12.34", rows[0]["amount"], route)
		}
	}
}

func TestParquetDateRejectsOutsidePlatformCalendarRange(t *testing.T) {
	for _, days := range []int32{-719163, 2932897} {
		raw := dateFixture(t, &days)
		_, err := NewPlugin().SampleTable(t.Context(), bytes.NewReader(raw), 0, 1, nil)
		require.Error(t, err)
	}
}
