package nfs

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/addp/common/engine/plugin"
	gonfs "github.com/vmware/go-nfs-client/nfs"
)

type testReadSeekCloser struct {
	*strings.Reader
}

func TestNFSResolveDirectoryPreservesCanonicalPath(t *testing.T) {
	p := &NFSPlugin{}
	for _, target := range []plugin.EngineCatalogPath{
		plugin.FileRootPath(14),
		plugin.FileDirectoryPath(14, "shp"),
		plugin.FileDirectoryPath(14, "shp/子目录"),
	} {
		t.Run(target.StringPath(), func(t *testing.T) {
			entry, err := p.ResolvePath(context.Background(), nil, target)
			if err != nil || entry == nil {
				t.Fatalf("resolve directory: %v", err)
			}
			if !reflect.DeepEqual(entry.Path, target) {
				t.Fatalf("resolved path changed: got %+v, want %+v", entry.Path, target)
			}
			if entry.Role != plugin.EngineCatalogRoleBranch {
				t.Fatalf("directory role = %q", entry.Role)
			}
		})
	}
}

func (r testReadSeekCloser) Close() error {
	return nil
}

func TestNFSRootCatalogEntryIsSemanticRoot(t *testing.T) {
	p := &NFSPlugin{}

	root := plugin.EngineCatalogRootEntry(p.EngineCatalogModel(), 7, "Business NFS")
	if root.Name != "Business NFS" {
		t.Fatalf("expected root name from engine, got %q", root.Name)
	}
	if got := root.Path.StringPath(); got != "" {
		t.Fatalf("expected root string path '', got %q", got)
	}
	if root.Term != plugin.EngineCatalogTermRoot || root.Kind != plugin.EngineCatalogKindRoot {
		t.Fatalf("expected root term/kind, got %s/%s", root.Term, root.Kind)
	}
	if got := p.EngineCatalogModel().RootTerm; got != plugin.EngineCatalogTermRoot {
		t.Fatalf("expected file catalog root term %q, got %q", plugin.EngineCatalogTermRoot, got)
	}
}

func TestFileCatalogNormalizesDotRoot(t *testing.T) {
	callbacks := plugin.FileCatalogCallbacks{
		ListDirectoryFunc: func(ctx context.Context, connInfo plugin.ConnectionInfo, parent plugin.EngineCatalogPath) ([]plugin.EngineCatalogEntry, error) {
			if path := parent.StringPath(); path != "" {
				t.Fatalf("expected semantic root list path '', got %q", path)
			}
			return []plugin.EngineCatalogEntry{
				plugin.FileDirectoryCatalogEntry(parent, "shp", "./shp/"),
			}, nil
		},
	}

	children, err := plugin.ListFileCatalogChildren(context.Background(), callbacks, nil, 7, plugin.FileRootPath(7), plugin.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 1 {
		t.Fatalf("expected one child, got %d", len(children))
	}
	if got := children[0].Path.StringPath(); got != "shp" {
		t.Fatalf("expected child path 'shp', got %q", got)
	}
	if children[0].Storage == nil || children[0].Storage.Path != "shp" {
		t.Fatalf("expected child storage path 'shp', got %#v", children[0].Storage)
	}
}

func TestNFSCapabilitiesDeclareRangeRead(t *testing.T) {
	p := &NFSPlugin{}

	if _, ok := interface{}(p).(plugin.RangeReadableProvider); !ok {
		t.Fatal("NFSPlugin should implement RangeReadableProvider")
	}
	caps := p.Capabilities()
	if caps.Storage == nil || caps.Storage.Store == nil || !caps.Storage.Store.RangeRead {
		t.Fatalf("NFS range_read capability = %#v, want true", caps.Storage)
	}
	if err := plugin.ValidatePluginCapabilities(p); err != nil {
		t.Fatalf("ValidatePluginCapabilities() error = %v", err)
	}
}

func TestNFSFileReadCloserSupportsSeekEndWithKnownSize(t *testing.T) {
	reader := &nfsFileReadCloser{
		ReadCloser: testReadSeekCloser{Reader: strings.NewReader("abcdef")},
		mu:         &sync.Mutex{},
		size:       6,
	}
	defer reader.Close()

	pos, err := reader.Seek(-2, io.SeekEnd)
	if err != nil {
		t.Fatalf("SeekEnd() error = %v", err)
	}
	if pos != 4 {
		t.Fatalf("SeekEnd position = %d, want 4", pos)
	}
	buf := make([]byte, 2)
	n, err := reader.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("Read() error = %v", err)
	}
	if n != 2 || string(buf) != "ef" {
		t.Fatalf("Read after SeekEnd = n:%d data:%q, want ef", n, string(buf))
	}
}

func TestInferredCSVContentTypeDoesNotClaimEncoding(t *testing.T) {
	if got := inferContentType("utf-16le-bom-quoted.csv"); got != "text/csv" {
		t.Fatalf("extension-only content type = %q, want text/csv without a guessed charset", got)
	}
}

// The pinned NFS client uses File.Close only for COMMIT; a read stream owns no connection.
type testCommitStream struct {
	*strings.Reader
	commits int
}

func (s *testCommitStream) Close() error                { s.commits++; return nil }
func (s *testCommitStream) Write(p []byte) (int, error) { return len(p), nil }

func TestNFSReadCloseDoesNotCommitAndStopsReadSeek(t *testing.T) {
	stream := &testCommitStream{Reader: strings.NewReader("abcdef")}
	reader := &nfsFileReadCloser{ReadCloser: stream, mu: &sync.Mutex{}, size: 6}
	for i := 0; i < 2; i++ {
		if err := reader.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if stream.commits != 0 {
		t.Fatalf("read-only close issued %d write commits", stream.commits)
	}
	if n, err := reader.Read(make([]byte, 1)); n != 0 || !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("read after close = %d, %v", n, err)
	}
	for _, whence := range []int{io.SeekStart, io.SeekCurrent, io.SeekEnd} {
		if _, err := reader.Seek(0, whence); !errors.Is(err, io.ErrClosedPipe) {
			t.Fatalf("seek after close = %v", err)
		}
	}
}

func TestNFSWriteCloseStillCommits(t *testing.T) {
	stream := &testCommitStream{Reader: strings.NewReader("")}
	writer := &lockedWriteCloser{WriteCloser: stream, mu: &sync.Mutex{}}
	if n, err := writer.Write([]byte("payload")); n != 7 || err != nil {
		t.Fatalf("write = %d, %v", n, err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if stream.commits != 1 {
		t.Fatalf("write close commits = %d, want 1", stream.commits)
	}
}

type testNFSReadTarget struct {
	info           os.FileInfo
	lookups, opens int
	openErr        error
	paths          []string
}

func (s *testNFSReadTarget) Lookup(path string) (os.FileInfo, []byte, error) {
	s.lookups++
	s.paths = append(s.paths, path)
	return s.info, nil, nil
}

func (s *testNFSReadTarget) Open(path string) (*gonfs.File, error) {
	s.opens++
	// Model the pinned client's Open path resolution, not an additional Stat.
	_, _, _ = s.Lookup(path)
	if s.openErr != nil {
		return nil, s.openErr
	}
	return &gonfs.File{}, nil
}

func TestNFSRangeOpenOnlyResolvesEachRequestedPathOnce(t *testing.T) {
	target := &testNFSReadTarget{}
	mu := &sync.Mutex{}
	for i := 0; i < 8192; i++ {
		reader, err := openNFSReadFile(context.Background(), target, mu, "3d/sample.ply", false)
		if err != nil {
			t.Fatal(err)
		}
		offset := int64(200 + i*56)
		if pos, err := reader.Seek(offset, io.SeekStart); err != nil || pos != offset {
			t.Fatalf("range seek = %d, %v", pos, err)
		}
		if err := reader.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if target.opens != 8192 || target.lookups != 8192 {
		t.Fatalf("opens=%d lookups=%d; want 8192 each", target.opens, target.lookups)
	}
	for _, path := range target.paths {
		if path != "3d/sample.ply" {
			t.Fatalf("path changed to %q", path)
		}
	}
}

func TestNFSContentOpenRetainsSizeForSeekEnd(t *testing.T) {
	path := filepath.Join(t.TempDir(), "content")
	if err := os.WriteFile(path, []byte("abcdef"), 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	target := &testNFSReadTarget{info: info}
	reader, err := openNFSReadFile(context.Background(), target, &sync.Mutex{}, "content", true)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if pos, err := reader.Seek(-2, io.SeekEnd); err != nil || pos != 4 {
		t.Fatalf("content seek end = %d, %v", pos, err)
	}
	if target.lookups != 2 || target.opens != 1 {
		t.Fatalf("content opens=%d lookups=%d", target.opens, target.lookups)
	}
}

func TestNFSOpenCancellationAndErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	target := &testNFSReadTarget{}
	if _, err := openNFSReadFile(ctx, target, &sync.Mutex{}, "content", false); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled open = %v", err)
	}
	if target.opens != 0 || target.lookups != 0 {
		t.Fatal("canceled open performed remote work")
	}
	denied := errors.New("access denied")
	target.openErr = denied
	if _, err := openNFSReadFile(context.Background(), target, &sync.Mutex{}, "content", false); !errors.Is(err, denied) {
		t.Fatalf("open error lost: %v", err)
	}
}

func TestNFSRangeReadLimitAndReadOnlyClose(t *testing.T) {
	stream := &testCommitStream{Reader: strings.NewReader("0123456789")}
	reader := &nfsFileReadCloser{ReadCloser: stream, mu: &sync.Mutex{}, size: -1}
	if _, err := reader.Seek(3, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	limited := &limitedReadCloser{Reader: io.LimitReader(reader, 4), closer: reader}
	data, err := io.ReadAll(limited)
	if err != nil || string(data) != "3456" {
		t.Fatalf("range = %q, %v", data, err)
	}
	if err := limited.Close(); err != nil {
		t.Fatal(err)
	}
	if stream.commits != 0 {
		t.Fatal("range close committed writes")
	}
}

func TestNFSReadSeekCloseSerialized(t *testing.T) {
	stream := &testCommitStream{Reader: strings.NewReader("0123456789")}
	reader := &nfsFileReadCloser{ReadCloser: stream, mu: &sync.Mutex{}, size: 10}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = reader.Seek(0, io.SeekStart)
			_, _ = reader.Read(make([]byte, 1))
			_ = reader.Close()
		}()
	}
	wg.Wait()
	if stream.commits != 0 {
		t.Fatal("read close committed writes")
	}
}

func TestNFSReaderAtReusesHandleAndPreservesCursor(t *testing.T) {
	stream := &testCommitStream{Reader: strings.NewReader("0123456789")}
	reader := &nfsFileReadCloser{ReadCloser: stream, mu: &sync.Mutex{}, size: 10}
	at, ok := any(reader).(io.ReaderAt)
	if !ok {
		t.Fatal("opened NFS stream lacks ReaderAt; sampling would reopen the file")
	}
	if _, err := reader.Seek(2, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	for _, offset := range []int64{7, 0, 4} {
		buf := make([]byte, 2)
		if n, err := at.ReadAt(buf, offset); n != 2 || err != nil || string(buf) != "0123456789"[offset:offset+2] {
			t.Fatalf("ReadAt(%d)=%d,%q,%v", offset, n, buf, err)
		}
		if pos, err := reader.Seek(0, io.SeekCurrent); pos != 2 || err != nil {
			t.Fatalf("cursor changed: %d,%v", pos, err)
		}
	}
	buf := make([]byte, 3)
	if n, err := at.ReadAt(buf, 9); n != 1 || err != io.EOF {
		t.Fatalf("short read=%d,%v", n, err)
	}
	if _, err := at.ReadAt(buf, -1); err == nil {
		t.Fatal("negative offset accepted")
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := at.ReadAt(buf, 0); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("closed ReadAt=%v", err)
	}
	if stream.commits != 0 {
		t.Fatal("read handle committed writes")
	}
}

func TestNFSConcurrentReadAtPreservesSequentialCursor(t *testing.T) {
	stream := &testCommitStream{Reader: strings.NewReader("0123456789")}
	reader := &nfsFileReadCloser{ReadCloser: stream, mu: &sync.Mutex{}, size: 10}
	defer reader.Close()
	if _, err := reader.Seek(3, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(offset int64) {
			defer wg.Done()
			p := make([]byte, 1)
			if n, err := reader.ReadAt(p, offset); n != 1 || err != nil || p[0] != byte('0'+offset) {
				t.Errorf("ReadAt(%d)=%d,%q,%v", offset, n, p, err)
			}
		}(int64(i % 10))
	}
	wg.Wait()
	if position, err := reader.Seek(0, io.SeekCurrent); position != 3 || err != nil {
		t.Fatalf("cursor=%d,%v", position, err)
	}
}
