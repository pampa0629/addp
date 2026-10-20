package executor

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/addp/common/contentio"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/contentadapter"
	engineplugin "github.com/addp/common/engine/plugin"
	s3plugin "github.com/addp/common/engine/plugins/s3"
	"github.com/addp/common/format"
	geojsonformat "github.com/addp/common/format/plugins/geojson"
	jsonformat "github.com/addp/common/format/plugins/json"
	parquetformat "github.com/addp/common/format/plugins/parquet"
	commonSpatial "github.com/addp/common/spatial"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/wkb"
)

// This is a deterministic component test, not a real MinIO or Hosted T4 run.
// The production S3 SDK, content adapter, format providers and Transfer pipeline
// consume a local HTTP S3 protocol fixture; native writes use the existing fake.
func TestS3NullableGeometryTransfer(t *testing.T) {
	for _, sourceFormat := range []format.FormatType{format.FormatGeoJSON} {
		for _, order := range []string{"null-first", "null-last", "all-null"} {
			for _, target := range []string{"native", "parquet", "native-meta", "parquet-meta"} {
				t.Run(string(sourceFormat)+"/"+order+"/"+target, func(t *testing.T) {
					data, ids, emptyIDs := nullableTransferFixture(t, sourceFormat, order)
					connection, requests := nullableTransferS3Source(t, data, sourceFormat)
					options := format.DefaultParseOptions()
					options.GeometryEncoding = format.GeometryEncodingEWKB
					var provider interface {
						format.TableInfoProvider
						format.TableReaderProvider
					}
					if sourceFormat == format.FormatGeoJSON {
						provider = geojsonformat.NewPlugin(nil)
					} else {
						provider = jsonformat.NewPlugin(nil)
					}
					native := &fakeBatchWriter{}
					preparer := &fakeTableWritePreparer{}
					output := &fakeContentWriter{}
					exec := &TableTransferExecutor{SourceContentReader: &s3plugin.S3Plugin{}, SourceInfoProvider: provider, SourceTableReadProvider: provider,
						TargetNativeWriter: native, TargetNativePreparer: preparer}
					plan := TableTransferPlan{Source: TableSourcePlan{Kind: TableEndpointEncoded, ConnInfo: connection,
						Path: engineplugin.ObjectItemPath(7, "samples", "source."+string(sourceFormat)), Format: sourceFormat, ParseOptions: options},
						Target: TableTargetPlan{Kind: TableEndpointNative}, BatchSize: 20}
					if strings.HasPrefix(target, "parquet") {
						plan.Target.Kind = TableEndpointEncoded
						plan.Target.Format = format.FormatParquet
						exec.TargetContentWriter = output
						exec.TargetTableWriterProvider = parquetformat.NewPlugin()
					}
					if strings.HasSuffix(target, "-meta") {
						declared, err := provider.DescribeTable(context.Background(), bytes.NewReader(data), options)
						if err != nil {
							t.Fatal(err)
						}
						plan.Source.TableInfo = declared.Table
						plan.Source.SpatialInfo = declared.Spatial
					}
					metrics, err := exec.Execute(context.Background(), plan)
					if err != nil {
						t.Fatal(err)
					}
					if metrics.RecordsRead != int64(len(ids)) || metrics.RecordsWritten != int64(len(ids)) || metrics.Batches != int64((len(ids)+19)/20) {
						t.Fatalf("lost records or incorrect batch boundary: %#v", metrics)
					}
					var fields []datatype.FieldInfo
					var rows []map[string]interface{}
					if strings.HasPrefix(target, "native") {
						fields = preparer.fields
						for _, batch := range native.batches {
							rows = append(rows, batch.Rows...)
						}
					} else {
						p := parquetformat.NewPlugin()
						info, err := p.DescribeTable(context.Background(), bytes.NewReader(output.buf.Bytes()), nil)
						if err != nil {
							t.Fatal(err)
						}
						if (info.Spatial != nil) != (order != "all-null") {
							t.Fatalf("unexpected output spatial facts: %#v", info.Spatial)
						}
						fields = info.Table.Fields
						rows, err = p.SampleTable(context.Background(), bytes.NewReader(output.buf.Bytes()), 0, 100, options)
						if err != nil {
							t.Fatal(err)
						}
					}
					geometryField := "shape"
					if sourceFormat == format.FormatGeoJSON {
						geometryField = "geometry"
					}
					geometryFields, matchingFields := 0, 0
					for _, field := range fields {
						if field.Type == datatype.FieldTypeGeometry {
							geometryFields++
						}
						if field.Name == geometryField {
							matchingFields++
							if order != "all-null" && (field.Type != datatype.FieldTypeGeometry || !field.Nullable) {
								t.Fatalf("target lost nullable geometry: %#v", field)
							}
						}
					}
					if matchingFields > 1 || (order != "all-null" && (matchingFields != 1 || geometryFields != 1)) || (order == "all-null" && geometryFields != 0) {
						t.Fatalf("duplicated or invented geometry: %#v", fields)
					}
					if len(rows) != len(ids) {
						t.Fatalf("target rows=%d, want %d", len(rows), len(ids))
					}
					for i, row := range rows {
						if fmt.Sprint(row["id"]) != strconv.Itoa(ids[i]) {
							t.Fatalf("row order changed: %#v", row)
						}
						value := row[geometryField]
						if emptyIDs[ids[i]] {
							if value != nil {
								t.Fatalf("empty geometry fabricated for id %d: %#v", ids[i], value)
							}
							continue
						}
						raw, ok := value.([]byte)
						if !ok {
							t.Fatalf("nonstandard geometry value: %T", value)
						}
						geometry, err := commonSpatial.ParseGeometryBytes(raw)
						if err != nil {
							t.Fatal(err)
						}
						point, ok := geometry.(*geom.Point)
						if !ok || point.X() != float64(ids[i]) || point.Y() != float64(ids[i])+0.5 {
							t.Fatalf("coordinates changed: %#v", geometry)
						}
					}
					if requests.Load() != 2 {
						t.Fatalf("source must Describe once and read once, got %d requests", requests.Load())
					}
				})
			}
		}
	}
}

func nullableTransferFixture(t *testing.T, sourceFormat format.FormatType, order string) ([]byte, []int, map[int]bool) {
	t.Helper()
	ids := make([]int, 25)
	empty := map[int]bool{1: true, 21: true, 25: true}
	for i := range ids {
		ids[i] = i + 1
	}
	if order == "all-null" {
		ids = ids[:3]
		empty = map[int]bool{1: true, 2: true, 3: true}
	}
	if order == "null-last" {
		ids = append(append([]int(nil), ids[1:]...), ids[0])
	}
	records := make([]map[string]interface{}, 0, len(ids))
	for _, id := range ids {
		if sourceFormat == format.FormatGeoJSON {
			var geometry interface{}
			if !empty[id] {
				geometry = map[string]interface{}{"type": "Point", "coordinates": []float64{float64(id), float64(id) + 0.5}}
			}
			records = append(records, map[string]interface{}{"type": "Feature", "geometry": geometry,
				"properties": map[string]interface{}{"id": id, "geometry": nil}})
		} else {
			row := map[string]interface{}{"id": id}
			if !empty[id] {
				raw, err := wkb.Marshal(geom.NewPointFlat(geom.XY, []float64{float64(id), float64(id) + 0.5}), binary.LittleEndian)
				if err != nil {
					t.Fatal(err)
				}
				row["shape"] = hex.EncodeToString(raw)
			} else if id%2 == 1 {
				row["shape"] = nil
			}
			records = append(records, row)
		}
	}
	var document interface{} = records
	if sourceFormat == format.FormatGeoJSON {
		document = map[string]interface{}{"type": "FeatureCollection", "features": records}
	}
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	return data, ids, empty
}

func nullableTransferS3Source(t *testing.T, data []byte, sourceFormat format.FormatType) (engineplugin.ConnectionInfo, *atomic.Int32) {
	t.Helper()
	requests := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
			t.Errorf("unexpected S3 request method/signature: %s", r.Method)
			http.Error(w, "invalid request", http.StatusForbidden)
			return
		}
		if r.URL.Path == "/samples/" || r.URL.Path == "/samples" {
			if _, ok := r.URL.Query()["location"]; ok {
				w.Header().Set("Content-Type", "application/xml")
				fmt.Fprint(w, `<LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/">us-east-1</LocationConstraint>`)
				return
			}
		}
		if r.URL.Path != "/samples/source."+string(sourceFormat) || r.URL.RawQuery != "" {
			t.Errorf("request escaped the exact source object: %s", r.URL.String())
			http.NotFound(w, r)
			return
		}
		requests.Add(1)
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", `"nullable-source"`)
		w.Header().Set("Last-Modified", "Mon, 19 Oct 2026 12:00:00 GMT")
		_, _ = w.Write(data)
	}))
	t.Cleanup(server.Close)
	return engineplugin.ConnectionInfo{"endpoint": server.URL, "access_key": "component-test", "secret_key": "component-test-secret", "use_ssl": false}, requests
}

func TestS3NullableGeometryMetadataAndSample(t *testing.T) {
	for _, sourceFormat := range []format.FormatType{format.FormatGeoJSON, format.FormatJSON} {
		for _, order := range []string{"null-first", "null-last", "all-null"} {
			t.Run(string(sourceFormat)+"/"+order, func(t *testing.T) {
				data, ids, emptyIDs := nullableTransferFixture(t, sourceFormat, order)
				connection, requests := nullableTransferS3Source(t, data, sourceFormat)
				path := engineplugin.ObjectItemPath(7, "samples", "source."+string(sourceFormat))
				content := contentadapter.NewReader(&s3plugin.S3Plugin{}, connection, path, engineplugin.ReadOptions{})
				ref := contentio.NewRef(path.StringPath(), contentio.RoleMain)
				var provider interface {
					format.TableInfoProvider
					format.TableSampleReader
				}
				if sourceFormat == format.FormatGeoJSON {
					provider = geojsonformat.NewPlugin(nil)
				} else {
					provider = jsonformat.NewPlugin(nil)
				}
				input, err := content.Open(context.Background(), ref)
				if err != nil {
					t.Fatal(err)
				}
				info, describeErr := provider.DescribeTable(context.Background(), input, nil)
				closeErr := input.Close()
				if describeErr != nil || closeErr != nil {
					t.Fatalf("describe=%v close=%v", describeErr, closeErr)
				}
				if info.Table.RowCount == nil || *info.Table.RowCount != int64(len(ids)) || (info.Spatial != nil) != (order != "all-null") {
					t.Fatalf("wrong source facts: %#v", info)
				}
				geometryField := "shape"
				if sourceFormat == format.FormatGeoJSON {
					geometryField = "geometry"
				}
				matches, geometries := 0, 0
				for _, field := range info.Table.Fields {
					if field.Type == datatype.FieldTypeGeometry {
						geometries++
					}
					if field.Name == geometryField {
						matches++
						if order != "all-null" && (field.Type != datatype.FieldTypeGeometry || !field.Nullable) {
							t.Fatalf("lost source nullability: %#v", field)
						}
					}
				}
				if matches > 1 || (order != "all-null" && geometries != 1) || (order == "all-null" && geometries != 0) {
					t.Fatalf("invented/duplicate geometry: %#v", info.Table.Fields)
				}
				var actualIDs []int
				for offset := 0; offset < len(ids); offset += 20 {
					input, err := content.Open(context.Background(), ref)
					if err != nil {
						t.Fatal(err)
					}
					rows, sampleErr := provider.SampleTable(context.Background(), input, int64(offset), 20, nil)
					closeErr := input.Close()
					if sampleErr != nil || closeErr != nil {
						t.Fatalf("sample=%v close=%v", sampleErr, closeErr)
					}
					for _, row := range rows {
						id, err := strconv.Atoi(fmt.Sprint(row["id"]))
						if err != nil {
							t.Fatal(err)
						}
						actualIDs = append(actualIDs, id)
						if emptyIDs[id] && row[geometryField] != nil {
							t.Fatalf("invented empty geometry: %#v", row)
						}
						if !emptyIDs[id] && row[geometryField] == nil {
							t.Fatalf("lost geometry: %#v", row)
						}
					}
				}
				if fmt.Sprint(actualIDs) != fmt.Sprint(ids) || requests.Load() < 2 {
					t.Fatalf("lost rows/S3 access: ids=%v requests=%d", actualIDs, requests.Load())
				}
			})
		}
	}
}

func TestDynamicEncodedSourceRejectsUnverifiedSchemaBeforeTargetEffects(t *testing.T) {
	for _, tc := range []struct {
		name, data string
		fields     []datatype.FieldInfo
		noInfo     bool
	}{
		{name: "changed-count", data: `[{"id":1,"label":"kept"}]`, fields: []datatype.FieldInfo{{Name: "id"}}},
		{name: "changed-name", data: `[{"id":1,"private_user_value":"private_token_value"}]`, fields: []datatype.FieldInfo{{Name: "id"}, {Name: "label"}}},
		{name: "changed-order", data: `[{"id":1,"label":"kept"}]`, fields: []datatype.FieldInfo{{Name: "label"}, {Name: "id"}}},
		{name: "malformed-source", data: `[{"id":1},invalid]`, fields: []datatype.FieldInfo{{Name: "id"}}},
		{name: "missing-info-provider", data: `[{"id":1}]`, fields: []datatype.FieldInfo{{Name: "id"}}, noInfo: true},
	} {
		for _, encoded := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/encoded=%t", tc.name, encoded), func(t *testing.T) {
				p := jsonformat.NewPlugin(nil)
				source := &headerTrackedContent{fakeContentReader: fakeContentReader{content: tc.data}}
				reader := &headerTrackedProvider{TableReaderProvider: p}
				target := &fakeNativeTableWriter{}
				output := &headerTargetContent{}
				exec := &TableTransferExecutor{SourceContentReader: source, SourceTableReadProvider: reader, SourceInfoProvider: p,
					TargetNativePreparer: target, TargetNativeWriter: target, TargetDeleteProvider: target}
				if tc.noInfo {
					exec.SourceInfoProvider = nil
				}
				plan := TableTransferPlan{Source: TableSourcePlan{Kind: TableEndpointEncoded, TableInfo: &datatype.TableInfo{Fields: tc.fields}},
					Target: TableTargetPlan{Kind: TableEndpointNative, DeleteBeforeWrite: true}}
				if encoded {
					plan.Target.Kind = TableEndpointEncoded
					exec.TargetContentWriter = output
					exec.TargetTableWriterProvider = parquetformat.NewPlugin()
				}
				_, err := exec.Execute(context.Background(), plan)
				if err == nil || strings.Contains(err.Error(), "private_user_value") || strings.Contains(err.Error(), "private_token_value") {
					t.Fatalf("wrong or unsafe diagnostic: %v", err)
				}
				if target.deleted || len(target.preparedFields) > 0 || len(target.batches) > 0 || output.creates > 0 {
					t.Fatal("target effects before current schema verification")
				}
				if source.closed != len(source.opens) || source.closed == 0 || reader.closed != 1 {
					t.Fatalf("leaked input/reader: source opens=%d closes=%d, reader closes=%d", len(source.opens), source.closed, reader.closed)
				}
			})
		}
	}
}

func TestDynamicEncodedSourceSchemaCheckAppliesFieldSelection(t *testing.T) {
	provider := jsonformat.NewPlugin(nil)
	options := format.DefaultParseOptions()
	options.FieldSelection = &format.FieldSelectionOptions{Include: []string{"label", "id"}, MissingFieldPolicy: format.MissingFieldError}
	declared := &datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt}, {Name: "label", Type: datatype.FieldTypeString}}}
	source := &headerTrackedContent{fakeContentReader: fakeContentReader{content: `[{"id":1,"label":"kept","unselected":"value"}]`}}
	target := &fakeBatchWriter{}
	preparer := &fakeTableWritePreparer{}
	exec := &TableTransferExecutor{SourceContentReader: source, SourceTableReadProvider: provider, SourceInfoProvider: provider, TargetNativePreparer: preparer, TargetNativeWriter: target}
	_, err := exec.Execute(context.Background(), TableTransferPlan{Source: TableSourcePlan{Kind: TableEndpointEncoded, TableInfo: declared, ParseOptions: options}, Target: TableTargetPlan{Kind: TableEndpointNative}})
	if err != nil {
		t.Fatal(err)
	}
	if len(preparer.fields) != 2 || preparer.fields[0].Name != "id" || preparer.fields[0].Type != datatype.FieldTypeBigInt || preparer.fields[1].Name != "label" || len(target.batches) != 1 || target.batches[0].Rows[0]["label"] != "kept" {
		t.Fatalf("lost selection or Meta types: %#v / %#v", preparer.fields, target.batches)
	}
	if _, exists := target.batches[0].Rows[0]["unselected"]; exists {
		t.Fatal("unselected field written")
	}
	if len(source.opens) != 2 || source.closed != 2 || len(declared.Fields) != 2 || declared.Fields[0].Name != "id" {
		t.Fatal("extra reads, leaked source or mutated declaration")
	}
}

func TestS3JSONRecordsTransfer(t *testing.T) {
	for _, order := range []string{"null-first", "null-last", "all-null"} {
		for _, target := range []string{"native", "parquet", "native-meta", "parquet-meta"} {
			t.Run(order+"/"+target, func(t *testing.T) {
				seed, ids, empty := nullableTransferFixture(t, format.FormatJSON, order)
				var records []map[string]interface{}
				if err := json.Unmarshal(seed, &records); err != nil {
					t.Fatal(err)
				}
				for i, row := range records {
					delete(row, "shape")
					if !empty[ids[i]] {
						row["signal"] = fmt.Sprintf("sample-%d", ids[i])
					} else if ids[i]%2 == 1 {
						row["signal"] = nil
					}
				}
				data, err := json.Marshal(records)
				if err != nil {
					t.Fatal(err)
				}
				connection, requests := nullableTransferS3Source(t, data, format.FormatJSON)
				provider := jsonformat.NewPlugin(nil)
				native := &fakeBatchWriter{}
				preparer := &fakeTableWritePreparer{}
				output := &fakeContentWriter{}
				exec := &TableTransferExecutor{SourceContentReader: &s3plugin.S3Plugin{}, SourceInfoProvider: provider, SourceTableReadProvider: provider, TargetNativeWriter: native, TargetNativePreparer: preparer}
				plan := TableTransferPlan{Source: TableSourcePlan{Kind: TableEndpointEncoded, ConnInfo: connection, Path: engineplugin.ObjectItemPath(7, "samples", "source.json"), Format: format.FormatJSON}, Target: TableTargetPlan{Kind: TableEndpointNative}, BatchSize: 20}
				if strings.HasSuffix(target, "-meta") {
					declared, err := provider.DescribeTable(context.Background(), bytes.NewReader(data), nil)
					if err != nil {
						t.Fatal(err)
					}
					plan.Source.TableInfo = declared.Table
				}
				if strings.HasPrefix(target, "parquet") {
					plan.Target.Kind = TableEndpointEncoded
					exec.TargetContentWriter = output
					exec.TargetTableWriterProvider = parquetformat.NewPlugin()
				}
				metrics, err := exec.Execute(context.Background(), plan)
				if err != nil {
					t.Fatal(err)
				}
				if metrics.RecordsRead != int64(len(ids)) || metrics.RecordsWritten != int64(len(ids)) || metrics.Batches != int64((len(ids)+19)/20) {
					t.Fatalf("wrong transfer counts: %#v", metrics)
				}
				fields := preparer.fields
				var rows []map[string]interface{}
				if strings.HasPrefix(target, "parquet") {
					p := parquetformat.NewPlugin()
					info, err := p.DescribeTable(context.Background(), bytes.NewReader(output.buf.Bytes()), nil)
					if err != nil {
						t.Fatal(err)
					}
					fields = info.Table.Fields
					if info.Spatial != nil {
						t.Fatal("ordinary records became spatial")
					}
					rows, err = p.SampleTable(context.Background(), bytes.NewReader(output.buf.Bytes()), 0, 100, nil)
					if err != nil {
						t.Fatal(err)
					}
				} else {
					for _, batch := range native.batches {
						rows = append(rows, batch.Rows...)
					}
				}
				if len(rows) != len(ids) || len(fields) != 2 {
					t.Fatalf("lost rows or fields: %d / %#v", len(rows), fields)
				}
				for _, field := range fields {
					if field.Type == datatype.FieldTypeGeometry || (field.Name == "signal" && !field.Nullable) {
						t.Fatalf("wrong nullable field: %#v", field)
					}
				}
				for i, row := range rows {
					if fmt.Sprint(row["id"]) != strconv.Itoa(ids[i]) {
						t.Fatalf("wrong row order: %#v", row)
					}
					if empty[ids[i]] {
						if row["signal"] != nil {
							t.Fatalf("null was fabricated: %#v", row)
						}
					} else if row["signal"] != fmt.Sprintf("sample-%d", ids[i]) {
						t.Fatalf("value changed: %#v", row)
					}
				}
				if requests.Load() != 2 {
					t.Fatalf("expected Describe once and read once, got %d", requests.Load())
				}
			})
		}
	}
}
