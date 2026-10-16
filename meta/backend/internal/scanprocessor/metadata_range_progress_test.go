package scanprocessor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
			if source.opens == 0 || source.opens != source.closes || source.first != int64(len(header)) || source.last != size-56 || input.ContentReader != source {
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
	if _, ok := observeMetadataRanges(streamOnly).(plugin.RangeReadableProvider); ok {
		t.Fatal("stream-only provider gained range capability")
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

type nativeMetadataStream struct {
	*strings.Reader
	size    int64
	offsets []int64
	closed  bool
	closes  int
}

func (s *nativeMetadataStream) ReadAt(p []byte, off int64) (int, error) {
	if s.closed {
		return 0, io.ErrClosedPipe
	}
	s.offsets = append(s.offsets, off)
	return s.Reader.ReadAt(p, off)
}
func (s *nativeMetadataStream) Close() error { s.closed = true; s.closes++; return nil }

type nativeMetadataSource struct {
	staticObjectContentReader
	stream  *nativeMetadataStream
	opens   int
	streams []*nativeMetadataStream
}

func (s *nativeMetadataSource) OpenContent(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.ReadOptions) (io.ReadCloser, error) {
	s.opens++
	stream := s.stream
	if s.opens > 1 {
		stream = &nativeMetadataStream{Reader: strings.NewReader(s.content), size: s.stream.size}
	}
	s.streams = append(s.streams, stream)
	return stream, nil
}

type nativeMetadataRangeSource struct {
	*nativeMetadataSource
	ranges int
}

func (s *nativeMetadataRangeSource) OpenRange(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.ReadOptions) (io.ReadCloser, error) {
	s.ranges++
	return nil, errors.New("native sampling reopened the file")
}

func TestPLYNativeReadsReuseOneContentStreamAndReportProgress(t *testing.T) {
	for _, withRanges := range []bool{false, true} {
		for _, cancelled := range []bool{false, true} {
			t.Run(fmt.Sprintf("rangeProvider=%v/cancel=%v", withRanges, cancelled), func(t *testing.T) {
				header := "ply\nformat binary_little_endian 1.0\nelement vertex 100001\n"
				for _, name := range []string{"x", "y", "z", "f_dc_0", "f_dc_1", "f_dc_2", "opacity", "scale_0", "scale_1", "scale_2", "rot_0", "rot_1", "rot_2", "rot_3"} {
					header += "property float " + name + "\n"
				}
				header += "end_header\n"
				size := int64(len(header) + 100001*56)
				content := header + strings.Repeat("\x00", 100001*56)
				stream := &nativeMetadataStream{Reader: strings.NewReader(content), size: size}
				source := &nativeMetadataSource{staticObjectContentReader: staticObjectContentReader{content: content}, stream: stream}
				ranges := &nativeMetadataRangeSource{nativeMetadataSource: source}
				var provider plugin.ContentReadableProvider = source
				if withRanges {
					provider = ranges
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				recorder := &hashProgressRecorder{}
				ctx, progress := scanflow.WithFileProgress(ctx, recorder)
				progress.Operation("enriching", "3d/sample.ply")
				if cancelled {
					recorder.onReport = func(s models.ScanProgress) {
						if s.CurrentRangesRead > 0 {
							cancel()
						}
					}
				}
				item := &metaitem.DetectedItem{}
				item.Layout, item.DataType, item.Format, item.SizeBytes = "single", datatype.GaussianSplat, "ply", &size
				input := &input{ContentReader: provider, EngineID: 14, Detected: item, PhysicalPath: "3d/sample.ply", SizeBytes: size, EngineCatalogPathFor: plugin.FileItemPathForEngine(14)}
				input.StrictDeepEnrich = true
				attrs, err := (Processor{}).enrichDeep(ctx, input, models.JSONMap{})
				want := 8192
				if cancelled {
					want = 1
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("cancel error=%v", err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
				sampleStreams := 0
				for _, opened := range source.streams {
					if opened.closes != 1 {
						t.Fatal("opened content stream was not closed once")
					}
					if len(opened.offsets) > 0 {
						sampleStreams++
					}
				}
				if sampleStreams != 1 || stream.closes != 1 || ranges.ranges != 0 || len(stream.offsets) != want {
					t.Fatalf("lifetime opens=%d closes=%d range opens=%d native reads=%d", source.opens, stream.closes, ranges.ranges, len(stream.offsets))
				}
				last := recorder.snapshots[len(recorder.snapshots)-1]
				if last.CurrentRangesRead != int64(want) || last.CurrentRangeBytesRead != int64(want*56) || last.CurrentBytesRead != 0 {
					t.Fatalf("native progress=%+v", last)
				}
				if !cancelled {
					if stream.offsets[0] != int64(len(header)) || stream.offsets[len(stream.offsets)-1] != size-56 {
						t.Fatal("sample endpoints changed")
					}
					data, _ := json.Marshal(attrs)
					if !strings.Contains(string(data), `"sampled_bounds_sample_count":8192`) {
						t.Fatalf("sample facts=%s", data)
					}
				}
				if input.ContentReader != provider {
					t.Fatal("observation provider escaped its enrichment scope")
				}
			})
		}
	}
}

func TestNativeMetadataProgressCountsOnlyPositionedReadBytes(t *testing.T) {
	recorder := &hashProgressRecorder{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx, progress := scanflow.WithFileProgress(ctx, recorder)
	progress.Operation("enriching", "3d/sample.ply")
	source := catalogRangeSource{&metadataRangeSource{}}
	rc, err := observeMetadataRanges(source).OpenContent(ctx, nil, plugin.FileItemPath(14, "3d/sample.ply"), plugin.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	at, ok := rc.(io.ReaderAt)
	if !ok {
		t.Fatal("native ReaderAt was stripped")
	}
	if _, err := rc.Read(make([]byte, 1)); err != nil {
		t.Fatal(err)
	}
	if recorder.snapshots[len(recorder.snapshots)-1].CurrentRangesRead != 0 {
		t.Fatal("sequential header read counted as range")
	}
	if n, err := at.ReadAt(make([]byte, 3), 1); n != 3 || err != nil {
		t.Fatalf("range=%d,%v", n, err)
	}
	if n, err := at.ReadAt(make([]byte, 2), 3); n != 1 || err != io.EOF {
		t.Fatalf("partial=%d,%v", n, err)
	}
	before := recorder.snapshots[len(recorder.snapshots)-1]
	if before.CurrentRangesRead != 2 || before.CurrentRangeBytesRead != 4 {
		t.Fatalf("actual counters=%+v", before)
	}
	_, _ = at.ReadAt(make([]byte, 1), 4)
	_, _ = at.ReadAt(nil, 0)
	if recorder.snapshots[len(recorder.snapshots)-1] != before {
		t.Fatal("empty/EOF read manufactured progress")
	}
	if position, err := rc.(io.Seeker).Seek(0, io.SeekCurrent); position != 1 || err != nil {
		t.Fatalf("sequential cursor=%d,%v", position, err)
	}
	cancel()
	if _, err := at.ReadAt(make([]byte, 1), 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	if recorder.snapshots[len(recorder.snapshots)-1] != before {
		t.Fatal("cancel manufactured progress")
	}
}
