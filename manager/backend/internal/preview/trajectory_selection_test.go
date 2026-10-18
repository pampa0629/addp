package preview

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/addp/common/contentio"
	"github.com/addp/common/format"
)

const selectionJSON = `{"device_field":"device","time_field":"at","time_encoding":"iso","devices":["A"],"start":"2026-10-16T01:00:00Z","end":"2026-10-16T03:00:00Z"}`

func trajectoryTestRequest(t *testing.T, f format.FormatType) *PreviewRequest {
	t.Helper()
	s, err := ParseTrajectorySelection(selectionJSON)
	if err != nil {
		t.Fatal(err)
	}
	return &PreviewRequest{ItemType: "file", Page: 1, PageSize: TrajectoryPointLimit, TrajectorySelection: s, CheckTrajectoryRead: func() error { return nil }, Attributes: map[string]interface{}{
		"item": map[string]interface{}{"format": string(f), "data_type": "table", "layout": "single"},
		"type_info": map[string]interface{}{"table": map[string]interface{}{"fields": []interface{}{
			map[string]interface{}{"name": "device", "type": "string"}, map[string]interface{}{"name": "at", "type": "string"}, map[string]interface{}{"name": "x", "type": "double"}, map[string]interface{}{"name": "y", "type": "double"}, map[string]interface{}{"name": "geometry", "type": "geometry"},
		}}},
	}}
}

func TestTrajectorySelectionRejectsAmbiguousOrUnboundedInput(t *testing.T) {
	for _, raw := range []string{"null", `[]`, selectionJSON + ` {}`, strings.Replace(selectionJSON, `"devices":["A"]`, `"devices":null`, 1), strings.Replace(selectionJSON, `"devices":["A"]`, `"devices":["A","A"]`, 1), strings.Replace(selectionJSON, `"devices":["A"]`, `"devices":[9007199254740992]`, 1), strings.Replace(selectionJSON, `"devices":["A"]`, `"devices":[{}]`, 1), strings.Replace(selectionJSON, `"device_field":"device"`, `"device_field":"device","device_field":"at"`, 1), strings.Replace(selectionJSON, `"device_field"`, `"Device_Field"`, 1), strings.Replace(selectionJSON, `"time_field":"at"`, `"time_field":"device"`, 1), strings.Replace(selectionJSON, `01:00:00Z`, `03:00:00Z`, 1), strings.Replace(selectionJSON, `01:00:00Z`, `01:00:00`, 1)} {
		if _, err := ParseTrajectorySelection(raw); !errors.Is(err, ErrTrajectorySelection) {
			t.Fatalf("invalid selection accepted: %v", err)
		}
	}
	s, err := ParseTrajectorySelection(strings.Replace(selectionJSON, `["A"]`, `[1,"1"]`, 1))
	if err != nil || len(s.Devices) != 2 {
		t.Fatal("typed device identities were collapsed", err)
	}
}

func TestTrajectoryTimeContractHasExactMillisecondsAndTimezone(t *testing.T) {
	for _, tc := range []struct {
		value    any
		encoding string
		want     int64
	}{{"1.001", "unix_s", 1001}, {"-0.001", "unix_s", -1}, {"1760580000.001", "unix_s", 1760580000001}, {json.Number("1001"), "unix_ms", 1001}, {json.Number("1e3"), "unix_s", 1000000}, {"1970-01-01T08:00:01.001+08:00", "iso", 1001}} {
		got, err := trajectoryMillis(tc.value, tc.encoding)
		if err != nil || got != tc.want {
			t.Fatalf("milliseconds=%d err=%v", got, err)
		}
	}
	for _, value := range []any{"2026-02-29T00:00:00Z", "2026-01-01T24:00:00Z", "2026-01-01T00:00:00+24:00", "2026-01-01T00:00:00+08:60", "2026-01-01T00:00:00", "2026-01-01T00:00:00.0001Z", nil, true} {
		if _, err := trajectoryMillis(value, "iso"); err == nil {
			t.Fatal("invalid ISO time accepted")
		}
	}
	for _, value := range []any{"1.0001", "1e3", true, nil, "8640000000001"} {
		if _, err := trajectoryMillis(value, "unix_s"); err == nil {
			t.Fatal("invalid epoch accepted")
		}
	}
}

func TestTrajectoryScansBeyondOrdinaryPageAndHonoursHalfOpenRange(t *testing.T) {
	var source strings.Builder
	source.WriteString("device,at,x,y\n")
	for i := 0; i < 3000; i++ {
		source.WriteString("B,not-a-time,1,2\n")
	}
	source.WriteString("A,2026-10-16T02:00:00Z,3,4\nA,2026-10-16T09:00:00+08:00,1,2\nA,2026-10-16T03:00:00Z,5,6\n")
	req := trajectoryTestRequest(t, format.FormatCSV)
	p := &FileTablePreviewProvider{}
	result, err := p.previewTrajectory(context.Background(), staticContentReader{[]byte(source.String())}, "", "points.csv", format.FormatCSV, mustFileParseOptions(t, p, format.FormatCSV, req), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 2 || *result.Total != 2 || result.HasMore || !result.TrajectoryScope.Complete || result.TrajectoryScope.ScannedRows != 3003 || fmt.Sprint(result.Rows[0]["x"]) != "3" {
		t.Fatalf("incomplete/incorrect selected range: %#v", result)
	}
}

func TestTrajectoryJSONAndGeoJSONUseExistingContinuousReaders(t *testing.T) {
	for _, tc := range []struct {
		f      format.FormatType
		source string
	}{
		{format.FormatJSON, `[{"device":"A","at":"2026-10-16T01:00:00Z","x":1,"y":2},{"device":"B","at":"bad","x":3,"y":4}]`},
		{format.FormatGeoJSON, `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"device":"A","at":"2026-10-16T01:00:00Z"},"geometry":{"type":"Point","coordinates":[1,2]}}]}`},
	} {
		t.Run(string(tc.f), func(t *testing.T) {
			req := trajectoryTestRequest(t, tc.f)
			p := &FileTablePreviewProvider{}
			result, err := p.previewTrajectory(context.Background(), staticContentReader{[]byte(tc.source)}, "", "points", tc.f, mustFileParseOptions(t, p, tc.f, req), req)
			if err != nil || len(result.Rows) != 1 || !result.TrajectoryScope.Complete {
				t.Fatalf("range=%#v err=%v", result, err)
			}
		})
	}
}

func TestTrajectoryNeverPublishesPartialRows(t *testing.T) {
	p := &FileTablePreviewProvider{}
	for _, tc := range []struct {
		name, source string
		want         error
	}{
		{"invalid selected time", "device,at,x,y\nA,bad,1,2\n", ErrTrajectorySource},
		{"too many points", "device,at,x,y\n" + strings.Repeat("A,2026-10-16T01:00:00Z,1,2\n", 2001), ErrTrajectoryPoints},
		{"row budget", "device,at,x,y\n" + strings.Repeat("B,bad,1,2\n", trajectoryRowLimit+1), ErrTrajectoryBudget},
		{"byte budget", "device,at,x,y\nB,bad,\"" + strings.Repeat("x", trajectoryByteLimit) + "\",2\n", ErrTrajectoryBudget},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := trajectoryTestRequest(t, format.FormatCSV)
			result, err := p.previewTrajectory(context.Background(), staticContentReader{[]byte(tc.source)}, "", "points.csv", format.FormatCSV, mustFileParseOptions(t, p, format.FormatCSV, req), req)
			if result != nil || !errors.Is(err, tc.want) {
				t.Fatalf("partial result=%v err=%v want=%v", result, err, tc.want)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := trajectoryTestRequest(t, format.FormatCSV)
	result, err := p.previewTrajectory(ctx, staticContentReader{[]byte("device,at,x,y\n")}, "", "points.csv", format.FormatCSV, mustFileParseOptions(t, p, format.FormatCSV, req), req)
	if result != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled range=%v %v", result, err)
	}
}

func TestTrajectoryBudgetDistinguishesRealEOF(t *testing.T) {
	for _, tc := range []struct {
		source string
		want   error
	}{{"abcd", nil}, {"abcde", ErrTrajectoryBudget}} {
		reader := &trajectoryBudgetReader{source: bytes.NewBufferString(tc.source), remaining: 4, ctx: context.Background()}
		data, err := io.ReadAll(reader)
		if string(data) != "abcd" || !errors.Is(err, tc.want) {
			t.Fatalf("read=%q err=%v", data, err)
		}
	}
}

func TestTrajectoryRejectsUnsupportedTargetsAndProtectionExpiry(t *testing.T) {
	req := trajectoryTestRequest(t, format.FormatCSV)
	for _, change := range []func(*PreviewRequest){func(r *PreviewRequest) { r.ItemType = "table" }, func(r *PreviewRequest) { r.ChildName = "Sheet1" }, func(r *PreviewRequest) { r.RefPath = "inner.csv" }, func(r *PreviewRequest) { r.Page = 2 }, func(r *PreviewRequest) { r.CheckTrajectoryRead = nil }, func(r *PreviewRequest) { r.TrajectorySelection.DeviceField = "missing" }} {
		copy := *req
		selection := *req.TrajectorySelection
		copy.TrajectorySelection = &selection
		change(&copy)
		if !errors.Is(validateTrajectoryTarget(&copy), ErrTrajectorySelection) {
			t.Fatal("unsupported selection accepted")
		}
	}
	calls := 0
	denied := fmt.Errorf("expired read protection")
	req.CheckTrajectoryRead = func() error {
		calls++
		if calls > 2 {
			return denied
		}
		return nil
	}
	p := &FileTablePreviewProvider{}
	result, err := p.previewTrajectory(context.Background(), staticContentReader{[]byte("device,at,x,y\nA,2026-10-16T01:00:00Z,1,2\n")}, "", "points.csv", format.FormatCSV, mustFileParseOptions(t, p, format.FormatCSV, req), req)
	if result != nil || !errors.Is(err, denied) {
		t.Fatalf("expired read returned %v %v", result, err)
	}
}

type trajectoryTransientFailure struct{ failed bool }

func (r *trajectoryTransientFailure) Read([]byte) (int, error) {
	if r.failed {
		return 0, io.EOF
	}
	r.failed = true
	return 0, io.ErrUnexpectedEOF
}

type trajectoryFailureContent struct{}

func (trajectoryFailureContent) Open(context.Context, contentio.Ref) (io.ReadCloser, error) {
	return io.NopCloser(io.MultiReader(strings.NewReader("device,at,x,y\nA,2026-10-16T01:00:00Z,1,2\nA,\"partial"), &trajectoryTransientFailure{})), nil
}
func (trajectoryFailureContent) Stat(context.Context, contentio.Ref) (*contentio.Stat, error) {
	return nil, nil
}
func TestTrajectoryPropagatesMidRecordSourceFailureWithoutPartialResult(t *testing.T) {
	req := trajectoryTestRequest(t, format.FormatCSV)
	p := &FileTablePreviewProvider{}
	result, err := p.previewTrajectory(context.Background(), trajectoryFailureContent{}, "", "points.csv", format.FormatCSV, mustFileParseOptions(t, p, format.FormatCSV, req), req)
	if result != nil || !errors.Is(err, ErrTrajectorySource) {
		t.Fatalf("partial source returned result=%v err=%v", result, err)
	}
}

func TestTrajectoryIncompleteJSONRootsNeverPublishMatchingRows(t *testing.T) {
	p := &FileTablePreviewProvider{}
	for _, tc := range []struct {
		f   format.FormatType
		raw string
	}{
		{format.FormatJSON, `[{"device":"A","at":"2026-10-16T01:00:00Z"}`},
		{format.FormatJSON, `[{"device":"A","at":"2026-10-16T01:00:00Z"}] {"device":"B"}`},
		{format.FormatGeoJSON, `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"device":"A","at":"2026-10-16T01:00:00Z"},"geometry":null}]`},
	} {
		req := trajectoryTestRequest(t, tc.f)
		result, err := p.previewTrajectory(context.Background(), staticContentReader{[]byte(tc.raw)}, "", "points", tc.f, mustFileParseOptions(t, p, tc.f, req), req)
		if result != nil || !errors.Is(err, ErrTrajectorySource) {
			t.Fatalf("unclosed source returned result=%v err=%v", result, err)
		}
	}
}

type trajectoryBlockingInput struct {
	initial bool
	started chan struct{}
	closed  chan struct{}
	once    sync.Once
}

func (r *trajectoryBlockingInput) Read(p []byte) (int, error) {
	if !r.initial {
		r.initial = true
		return copy(p, []byte("device,at,x,y\nA,2026-10-16T01:00:00Z,1,2\n")), nil
	}
	close(r.started)
	<-r.closed
	return 0, io.ErrClosedPipe
}
func (r *trajectoryBlockingInput) Close() error { r.once.Do(func() { close(r.closed) }); return nil }

type trajectoryBlockingContent struct{ input *trajectoryBlockingInput }

func (r trajectoryBlockingContent) Open(context.Context, contentio.Ref) (io.ReadCloser, error) {
	return r.input, nil
}
func (trajectoryBlockingContent) Stat(context.Context, contentio.Ref) (*contentio.Stat, error) {
	return nil, nil
}
func TestTrajectoryCancellationClosesBlockedSourceAndPublishesNothing(t *testing.T) {
	input := &trajectoryBlockingInput{started: make(chan struct{}), closed: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := trajectoryTestRequest(t, format.FormatCSV)
	p := &FileTablePreviewProvider{}
	done := make(chan error, 1)
	go func() {
		result, err := p.previewTrajectory(ctx, trajectoryBlockingContent{input}, "", "points.csv", format.FormatCSV, mustFileParseOptions(t, p, format.FormatCSV, req), req)
		if result != nil {
			done <- fmt.Errorf("cancelled read published data")
			return
		}
		done <- err
	}()
	select {
	case <-input.started:
	case <-time.After(time.Second):
		t.Fatal("source read did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled read did not close blocked source")
	}
}

func TestFileTrajectoryEntryDoesNotTrustStaleExactCountInsteadOfRealLookahead(t *testing.T) {
	req := trajectoryTestRequest(t, format.FormatCSV)
	req.TrajectorySelection = nil
	req.Attributes["type_info"].(map[string]interface{})["table"].(map[string]interface{})["row_count"] = int64(2000)
	source := "device,at,x,y\n" + strings.Repeat("A,2026-10-16T01:00:00Z,1,2\n", 2001)
	p := &FileTablePreviewProvider{}
	sample, err := format.GetTableSampleReader(format.FormatCSV)
	if err != nil {
		t.Fatal(err)
	}
	result, err := p.previewStreamable(context.Background(), staticContentReader{[]byte(source)}, "", "points.csv", format.FormatCSV, nil, sample, mustFileParseOptions(t, p, format.FormatCSV, req), req)
	if err != nil || len(result.Rows) != 2000 || !result.HasMore {
		t.Fatalf("stale count hid additional observations: %v %v", result, err)
	}
}

func TestTrajectoryBigintIdentityUsesPublicExactTextContract(t *testing.T) {
	req := trajectoryTestRequest(t, format.FormatJSON)
	req.TrajectorySelection.Devices = []interface{}{"9223372036854775807"}
	table := req.Attributes["type_info"].(map[string]interface{})["table"].(map[string]interface{})
	table["fields"].([]interface{})[0].(map[string]interface{})["type"] = "bigint"
	p := &FileTablePreviewProvider{}
	source := `[{"device":9223372036854775807,"at":"2026-10-16T01:00:00Z","x":1,"y":2}]`
	result, err := p.previewTrajectory(context.Background(), staticContentReader{[]byte(source)}, "", "points.json", format.FormatJSON, mustFileParseOptions(t, p, format.FormatJSON, req), req)
	if err != nil || result == nil || len(result.Rows) != 1 || !result.TrajectoryScope.Complete {
		t.Fatalf("exact bigint range=%#v err=%v", result, err)
	}
	if got := fmt.Sprint(result.Rows[0]["device"]); got != "9223372036854775807" {
		t.Fatalf("identity changed: %s", got)
	}
	req.TrajectorySelection.Devices = []interface{}{json.Number("1")}
	result, err = p.previewTrajectory(context.Background(), staticContentReader{[]byte(source)}, "", "points.json", format.FormatJSON, mustFileParseOptions(t, p, format.FormatJSON, req), req)
	if err != nil || len(result.Rows) != 0 {
		t.Fatalf("bigint must not match numeric selector: %#v %v", result, err)
	}
}
