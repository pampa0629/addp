package executor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"testing"

	engineplugin "github.com/addp/common/engine/plugin"
)

func TestRawCopyExecutorCopiesBytesAndReportsProgress(t *testing.T) {
	source := &rawCopyContentStore{files: map[string][]byte{"docs/a.pdf": []byte("hello pdf")}}
	target := &rawCopyContentStore{files: map[string][]byte{"backup/a.pdf": []byte("old")}}
	exec := &RawCopyExecutor{
		SourceContentReader:  source,
		TargetContentWriter:  target,
		TargetDeleteProvider: target,
	}
	var events []RawCopyProgressEvent
	metrics, err := exec.Execute(context.Background(), RawCopyPlan{
		Source: RawCopyEndpointPlan{Path: engineplugin.ObjectItemPath(1, "docs", "a.pdf")},
		Target: RawCopyEndpointPlan{Path: engineplugin.FileItemPath(2, "backup/a.pdf"), DeleteBeforeWrite: true},
		ProgressCallback: func(_ context.Context, event RawCopyProgressEvent) error {
			events = append(events, event)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if got := string(target.files["backup/a.pdf"]); got != "hello pdf" {
		t.Fatalf("target content = %q, want hello pdf", got)
	}
	if !target.deleted["backup/a.pdf"] {
		t.Fatal("target was not deleted before write")
	}
	if metrics.BytesRead != int64(len("hello pdf")) || metrics.BytesWritten != int64(len("hello pdf")) || metrics.RecordsRead != 1 || metrics.RecordsWritten != 1 {
		t.Fatalf("metrics = %#v", metrics)
	}
	if len(events) != 1 || !events[0].Final || events[0].BytesWritten != int64(len("hello pdf")) {
		t.Fatalf("events = %#v", events)
	}
}

func TestRawCopyExecutorRejectsOverwriteWithoutDeleteProvider(t *testing.T) {
	exec := &RawCopyExecutor{
		SourceContentReader: &rawCopyContentStore{files: map[string][]byte{"docs/a.pdf": []byte("hello")}},
		TargetContentWriter: &rawCopyContentStore{files: map[string][]byte{}},
	}
	_, err := exec.Execute(context.Background(), RawCopyPlan{
		Source: RawCopyEndpointPlan{Path: engineplugin.ObjectItemPath(1, "docs", "a.pdf")},
		Target: RawCopyEndpointPlan{Path: engineplugin.FileItemPath(2, "backup/a.pdf"), DeleteBeforeWrite: true},
	})
	if err == nil {
		t.Fatal("Execute error = nil, want delete provider error")
	}
}

type rawCopyContentStore struct {
	files   map[string][]byte
	deleted map[string]bool
}

func (s *rawCopyContentStore) Type() string { return "raw_copy_store" }

func (s *rawCopyContentStore) DisplayName() string { return "Raw Copy Store" }

func (s *rawCopyContentStore) EngineOrigin() string { return "general" }

func (s *rawCopyContentStore) DefaultPort() int { return 0 }

func (s *rawCopyContentStore) RequiredFields() []string { return nil }

func (s *rawCopyContentStore) SensitiveFields() []string { return nil }

func (s *rawCopyContentStore) ValidateConnectionInfo(engineplugin.ConnectionInfo) error { return nil }

func (s *rawCopyContentStore) TestConnection(context.Context, engineplugin.ConnectionInfo) error {
	return nil
}

func (s *rawCopyContentStore) Capabilities() engineplugin.EngineCapabilities {
	return engineplugin.EngineCapabilities{}
}

func (s *rawCopyContentStore) StoreSemantics() engineplugin.StoreSemantics {
	return engineplugin.StoreSemantics{}
}

func (s *rawCopyContentStore) OpenContent(_ context.Context, _ engineplugin.ConnectionInfo, path engineplugin.CatalogPath, _ engineplugin.ReadOptions) (io.ReadCloser, error) {
	data, ok := s.files[path.StringPath()]
	if !ok {
		return nil, fmt.Errorf("content %s not found", path.StringPath())
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (s *rawCopyContentStore) CreateContent(_ context.Context, _ engineplugin.ConnectionInfo, path engineplugin.CatalogPath, _ engineplugin.WriteOptions) (io.WriteCloser, error) {
	buf := &bytes.Buffer{}
	return rawCopyWriteCloser{Writer: buf, close: func() {
		if s.files == nil {
			s.files = map[string][]byte{}
		}
		s.files[path.StringPath()] = append([]byte(nil), buf.Bytes()...)
	}}, nil
}

func (s *rawCopyContentStore) DeleteResource(_ context.Context, _ engineplugin.ConnectionInfo, path engineplugin.CatalogPath) error {
	if s.deleted == nil {
		s.deleted = map[string]bool{}
	}
	s.deleted[path.StringPath()] = true
	delete(s.files, path.StringPath())
	return nil
}

type rawCopyWriteCloser struct {
	io.Writer
	close func()
}

func (w rawCopyWriteCloser) Close() error {
	if w.close != nil {
		w.close()
	}
	return nil
}
