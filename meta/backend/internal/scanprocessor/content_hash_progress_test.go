package scanprocessor

import (
	"context"
	"errors"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
	"io"
	"strings"
	"testing"
)

type hashProgressRecorder struct {
	snapshots []models.ScanProgress
	onReport  func(models.ScanProgress)
}

func (p *hashProgressRecorder) SetTotal(int)                                     {}
func (p *hashProgressRecorder) Advance(string, int, int, map[string]interface{}) {}
func (p *hashProgressRecorder) Message(string)                                   {}
func (p *hashProgressRecorder) Report(s models.ScanProgress) {
	p.snapshots = append(p.snapshots, s)
	if p.onReport != nil {
		p.onReport(s)
	}
}

type chunkContentProvider struct {
	staticObjectContentReader
	chunk *chunkReadCloser
}

func (p chunkContentProvider) OpenContent(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.ReadOptions) (io.ReadCloser, error) {
	return p.chunk, nil
}

type chunkReadCloser struct {
	io.Reader
	closed bool
	reads  int
}

func (r *chunkReadCloser) Read(b []byte) (int, error) {
	r.reads++
	return r.Reader.Read(b[:min(len(b), 1024)])
}
func (r *chunkReadCloser) Close() error { r.closed = true; return nil }
func TestHashProgressAdvancesBeforeEOFAndCancellationStopsFurtherReads(t *testing.T) {
	for _, cancelRead := range []bool{false, true} {
		t.Run(map[bool]string{false: "complete", true: "cancel"}[cancelRead], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			recorder := &hashProgressRecorder{}
			ctx, p := scanflow.WithFileProgress(ctx, recorder)
			p.Update(func(s *models.ScanProgress) {
				s.Phase = "processing"
				s.Operation = "hashing"
				s.CurrentPath = "3d/large.obj"
				s.CurrentBytesTotal = 4096
			})
			if cancelRead {
				recorder.onReport = func(s models.ScanProgress) {
					if s.CurrentBytesRead >= 1024 {
						cancel()
					}
				}
			}
			stream := &chunkReadCloser{Reader: strings.NewReader(strings.Repeat("x", 4096))}
			hash, err := computeContentSHA256(ctx, chunkContentProvider{chunk: stream}, nil, plugin.FileItemPath(14, "3d/large.obj"))
			if !stream.closed {
				t.Fatal("content reader leaked")
			}
			sawIntermediate := false
			for _, s := range recorder.snapshots {
				if s.CurrentBytesRead > 0 && s.CurrentBytesRead < 4096 {
					sawIntermediate = true
				}
			}
			if !sawIntermediate {
				t.Fatal("only terminal progress was emitted")
			}
			if cancelRead {
				if !errors.Is(err, context.Canceled) || hash != "" || stream.reads != 1 {
					t.Fatalf("cancel: hash=%q err=%v reads=%d", hash, err, stream.reads)
				}
			} else if err != nil || len(hash) != 64 || recorder.snapshots[len(recorder.snapshots)-1].CurrentBytesRead != 4096 {
				t.Fatalf("hash/read result: %q %v %+v", hash, err, recorder.snapshots)
			}
		})
	}
}
