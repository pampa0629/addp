package scanprocessor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
)

type metadataRangeSource struct {
	staticObjectContentReader
	opens, closes int
	first, last   int64
}

func (r *metadataRangeSource) OpenRange(_ context.Context, _ plugin.ConnectionInfo, _ plugin.EngineCatalogPath, opts plugin.ReadOptions) (io.ReadCloser, error) {
	if r.opens == 0 {
		r.first = opts.Offset
	}
	r.last = opts.Offset
	r.opens++
	return &metadataRangeStream{Reader: bytes.NewReader(make([]byte, opts.Length)), close: func() { r.closes++ }}, nil
}

type metadataRangeStream struct {
	*bytes.Reader
	close func()
}

func (r *metadataRangeStream) Close() error { r.close(); return nil }

func metadataRangeCounters(s models.ScanProgress) (int64, int64) {
	data, _ := json.Marshal(s)
	var counters struct {
		Bytes  int64 `json:"current_range_bytes_read"`
		Ranges int64 `json:"current_ranges_read"`
	}
	_ = json.Unmarshal(data, &counters)
	return counters.Bytes, counters.Ranges
}

func TestPLYAttributeRangesReportRealProgressAndHonorCancellation(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "complete", true: "cancel"}[cancelled], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			recorder := &hashProgressRecorder{}
			ctx, progress := scanflow.WithFileProgress(ctx, recorder)
			progress.Update(func(s *models.ScanProgress) { s.Phase = "processing" })
			progress.Operation("enriching", "3d/sample.ply")
			if cancelled {
				recorder.onReport = func(s models.ScanProgress) {
					if _, ranges := metadataRangeCounters(s); ranges > 0 {
						cancel()
					}
				}
			}
			header := "ply\nformat binary_little_endian 1.0\nelement vertex 100001\n"
			for _, name := range []string{"x", "y", "z", "f_dc_0", "f_dc_1", "f_dc_2", "opacity", "scale_0", "scale_1", "scale_2", "rot_0", "rot_1", "rot_2", "rot_3"} {
				header += "property float " + name + "\n"
			}
			header += "end_header\n"
			source := &metadataRangeSource{staticObjectContentReader: staticObjectContentReader{content: header}}
			size := int64(len(header) + 100001*56)
			item := &metaitem.DetectedItem{}
			item.Layout, item.DataType, item.Format, item.SizeBytes = "single", datatype.GaussianSplat, "ply", &size
			input := &input{ContentReader: source, EngineID: 14, Detected: item, PhysicalPath: "3d/sample.ply", SizeBytes: size, EngineCatalogPathFor: plugin.FileItemPathForEngine(14)}
			var attrs models.JSONMap
			var err error
			if cancelled {
				input.Resource = &commonModels.Engine{ID: 14}
				input.ParentNode = &models.MetaNode{ID: 1, TenantID: 1, EngineID: 14}
				input.ScanDepth = "deep"
				_, err = (Processor{}).Process(ctx, *input)
				if !errors.Is(err, context.Canceled) || source.opens != 1 || source.closes != 1 {
					t.Fatalf("cancel: err=%v opens=%d closes=%d", err, source.opens, source.closes)
				}
				return
			}
			attrs, err = (Processor{}).enrichDeep(ctx, input, models.JSONMap{})
			if err != nil {
				t.Fatal(err)
			}
			last := recorder.snapshots[len(recorder.snapshots)-1]
			read, ranges := metadataRangeCounters(last)
			if read != 8192*56 || ranges != 8192 || last.CurrentBytesRead != 0 || last.ItemsCompleted != 0 || last.ItemsFailed != 0 {
				t.Fatalf("missing or mixed metadata observations: %+v", last)
			}
			if source.opens != 8192 || source.closes != 8192 || source.first != int64(len(header)) || source.last != size-56 || input.ContentReader != source {
				t.Fatalf("sampling or provider lifetime changed: %+v", source)
			}
			data, _ := json.Marshal(attrs)
			if !strings.Contains(string(data), `"sampled_bounds_sample_count":8192`) {
				t.Fatalf("sampling facts changed: %s", data)
			}
			intermediate := false
			for _, s := range recorder.snapshots {
				if _, count := metadataRangeCounters(s); count > 0 && count < ranges {
					intermediate = true
					break
				}
			}
			if !intermediate {
				t.Fatal("no progress before range sampling completed")
			}
			progress.Operation("hashing", "3d/sample.ply")
			if read, ranges := metadataRangeCounters(recorder.snapshots[len(recorder.snapshots)-1]); read != 0 || ranges != 0 {
				t.Fatal("metadata range counters escaped their operation")
			}
		})
	}
}

type catalogRangeSource struct{ *metadataRangeSource }

func (p catalogRangeSource) ListChildren(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.ListOptions) ([]plugin.EngineCatalogEntry, error) {
	return []plugin.EngineCatalogEntry{{Name: "known"}}, nil
}

func (p catalogRangeSource) ResolvePath(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath) (*plugin.EngineCatalogEntry, error) {
	return &plugin.EngineCatalogEntry{Name: "known"}, nil
}

func (p catalogRangeSource) OpenContent(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.ReadOptions) (io.ReadCloser, error) {
	return &metadataRangeStream{Reader: bytes.NewReader([]byte("body")), close: func() {}}, nil
}

func TestAttributeObservationPreservesCatalogAndSequentialStreamCapabilities(t *testing.T) {
	streamOnly := &staticObjectContentReader{}
	if observeMetadataRanges(streamOnly) != streamOnly {
		t.Fatal("stream-only provider was replaced or given range capability")
	}
	source := catalogRangeSource{&metadataRangeSource{}}
	observed := observeMetadataRanges(source)
	if _, ok := observed.(plugin.RangeReadableProvider); !ok {
		t.Fatal("range capability lost")
	}
	catalog, ok := observed.(plugin.EngineCatalogProvider)
	if !ok {
		t.Fatal("catalog capability lost")
	}
	ctx := context.Background()
	path := plugin.FileItemPath(14, "3d/sample.ply")
	entries, err := catalog.ListChildren(ctx, nil, path, plugin.ListOptions{})
	if err != nil || len(entries) != 1 || entries[0].Name != "known" {
		t.Fatalf("catalog listing changed: %+v %v", entries, err)
	}
	entry, err := catalog.ResolvePath(ctx, nil, path)
	if err != nil || entry == nil || entry.Name != "known" {
		t.Fatalf("catalog stat changed: %+v %v", entry, err)
	}
	rc, err := observed.OpenContent(ctx, nil, path, plugin.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	if _, ok := rc.(io.Seeker); !ok {
		t.Fatal("sequential stream lost its seek capability")
	}
}

func TestAttributeRangeCountsOnlyActualReadBytesAndOneCountPerStream(t *testing.T) {
	recorder := &hashProgressRecorder{}
	ctx, progress := scanflow.WithFileProgress(context.Background(), recorder)
	progress.Operation("enriching", "3d/sample.ply")
	stream := &metadataProgressStream{ReadCloser: io.NopCloser(strings.NewReader("abc")), ctx: ctx}
	defer stream.Close()
	for i := 0; i < 3; i++ {
		if n, err := stream.Read(make([]byte, 1)); err != nil || n != 1 {
			t.Fatalf("read=%d err=%v", n, err)
		}
	}
	beforeEOF := recorder.snapshots[len(recorder.snapshots)-1]
	if n, err := stream.Read(make([]byte, 1)); n != 0 || err != io.EOF {
		t.Fatalf("EOF changed: n=%d err=%v", n, err)
	}
	if read, ranges := metadataRangeCounters(beforeEOF); read != 3 || ranges != 1 || recorder.snapshots[len(recorder.snapshots)-1] != beforeEOF {
		t.Fatal("EOF or read chunks fabricated progress")
	}
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	source := &metadataRangeSource{}
	if _, err := observeMetadataRanges(source).(plugin.RangeReadableProvider).OpenRange(ctx, nil, plugin.FileItemPath(14, "3d/sample.ply"), plugin.ReadOptions{Length: 4}); !errors.Is(err, context.Canceled) || source.opens != 0 {
		t.Fatal("cancelled sampling opened another range")
	}
}
