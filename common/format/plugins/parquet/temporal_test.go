package parquet

import (
	"bytes"
	"context"
	"math"
	"testing"

	"github.com/addp/common/datatype"
	parquetgo "github.com/parquet-go/parquet-go"
	"github.com/stretchr/testify/require"
)

func TestParquetNativeTemporalRoundTrip(t *testing.T) {
	ctx := context.Background()
	plugin := NewPlugin()
	fields := []datatype.FieldInfo{
		{Name: "zoned", Type: datatype.FieldTypeTimestamp, Temporal: &datatype.TemporalInfo{Unit: "ns", Timezone: "America/New_York"}},
		{Name: "clock", Type: datatype.FieldTypeTime, Temporal: &datatype.TemporalInfo{Unit: "s"}, Nullable: true},
		{Name: "local", Type: datatype.FieldTypeTimestamp, Temporal: &datatype.TemporalInfo{Unit: "ns"}},
		{Name: "offset", Type: datatype.FieldTypeTimestamp, Temporal: &datatype.TemporalInfo{Unit: "s", Timezone: "+05:30"}},
		{Name: "label", Type: datatype.FieldTypeString, Nullable: true},
	}
	rows := []map[string]any{
		{"zoned": int64(1636263000000000001), "clock": int64(86399), "local": int64(-1), "offset": int64(-1), "label": ""},
		{"zoned": int64(1636266600000000001), "clock": nil, "local": int64(math.MinInt64), "offset": int64(2147483648), "label": nil},
	}
	var buffer bytes.Buffer
	writer, err := plugin.OpenTableWriter(ctx, &buffer, &datatype.TableInfo{Fields: fields}, nil)
	require.NoError(t, err)
	require.NoError(t, writer.WriteRows(ctx, rows))
	require.NoError(t, writer.Close(ctx))
	raw := buffer.Bytes()
	file, err := parquetgo.OpenFile(bytes.NewReader(raw), int64(len(raw)))
	require.NoError(t, err)
	clock, ok := file.Schema().Lookup("clock")
	require.True(t, ok)
	require.Equal(t, parquetgo.Int32, clock.Node.Type().Kind())
	require.False(t, clock.Node.Type().LogicalType().Time.IsAdjustedToUTC)
	require.NotNil(t, clock.Node.Type().LogicalType().Time.Unit.Millis)
	local, _ := file.Schema().Lookup("local")
	require.False(t, local.Node.Type().LogicalType().Timestamp.IsAdjustedToUTC)
	zone, _ := file.Schema().Lookup("zoned")
	require.True(t, zone.Node.Type().LogicalType().Timestamp.IsAdjustedToUTC)
	require.NotNil(t, zone.Node.Type().LogicalType().Timestamp.Unit.Nanos)

	desc, err := plugin.DescribeTable(ctx, bytes.NewReader(raw), nil)
	require.NoError(t, err)
	require.Equal(t, &datatype.TemporalInfo{Unit: "ms"}, desc.Table.GetField("clock").Temporal)
	require.Equal(t, fields[0].Temporal, desc.Table.GetField("zoned").Temporal)
	require.Equal(t, &datatype.TemporalInfo{Unit: "ms", Timezone: "+05:30"}, desc.Table.GetField("offset").Temporal)

	rows[0]["clock"] = int64(86399000)
	rows[0]["offset"] = int64(-1000)
	rows[1]["offset"] = int64(2147483648000)
	page, err := plugin.SampleTable(ctx, bytes.NewReader(raw), 0, 2, nil)
	require.NoError(t, err)
	require.Equal(t, rows, page)
	reader, err := plugin.OpenTableReader(ctx, bytes.NewReader(raw), nil)
	require.NoError(t, err)
	defer reader.Close(ctx)
	batch, err := reader.ReadRows(ctx, 2)
	require.NoError(t, err)
	require.Equal(t, rows, batch)
	preview, err := datatype.RowsForJSON(desc.Table.Fields, batch)
	require.NoError(t, err)
	require.Equal(t, "1969-12-31T23:59:59.999999999", preview[0]["local"])
	require.Equal(t, "1677-09-21T00:12:43.145224192", preview[1]["local"])
	require.Equal(t, "2021-11-07T05:30:00.000000001Z", preview[0]["zoned"])
	require.Equal(t, "2021-11-07T06:30:00.000000001Z", preview[1]["zoned"])
	require.Equal(t, "23:59:59.000", preview[0]["clock"])
	require.Nil(t, preview[1]["clock"])
}

func TestParquetTemporalRejectsInvalidTicks(t *testing.T) {
	seconds := &datatype.TemporalInfo{Unit: "s"}
	field := datatype.FieldInfo{Name: "stamp", Type: datatype.FieldTypeTimestamp, Temporal: seconds}
	for _, bad := range []any{int64(math.MaxInt64), int64(math.MinInt64), nil, "12", float64(12)} {
		_, err := temporalWriterValue(bad, field)
		require.Error(t, err)
	}
	ticks, err := temporalWriterValue(int64(math.MinInt64/1000), field)
	require.NoError(t, err)
	require.Equal(t, int64(-9223372036854775000), ticks)
	field.Type = datatype.FieldTypeTime
	for _, bad := range []int64{-1, 86400} {
		_, err := temporalWriterValue(bad, field)
		require.Error(t, err)
	}
	require.Error(t, readArrowTimezones("/////wQAAAAAAAAA", nil))
}
