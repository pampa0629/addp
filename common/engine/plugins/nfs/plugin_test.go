package nfs

import (
	"context"
	"errors"
	"io"
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
	types       map[string]uint32
	lookups     int
	paths       []string
	readHandles []string
	lookupErr   error
}

func (s *testNFSReadTarget) Root() []byte { return []byte("root") }
func (s *testNFSReadTarget) Attr([]byte) (*gonfs.Fattr, error) {
	return &gonfs.Fattr{Type: gonfs.NF3Dir}, nil
}
func (s *testNFSReadTarget) LookupAt(parent []byte, name string) (*gonfs.Fattr, []byte, error) {
	s.lookups++
	p := name
	if string(parent) != "root" {
		p = string(parent) + "/" + name
	}
	s.paths = append(s.paths, p)
	if s.lookupErr != nil {
		return nil, nil, s.lookupErr
	}
	kind := s.types[p]
	return &gonfs.Fattr{Type: kind, Filesize: 6}, []byte(p), nil
}
func (s *testNFSReadTarget) ReadHandle(handle []byte, offset uint64, p []byte) (int, error) {
	s.readHandles = append(s.readHandles, string(handle))
	if offset >= 6 {
		return 0, io.EOF
	}
	n := copy(p, "abcdef"[offset:])
	if int(offset)+n == 6 {
		return n, io.EOF
	}
	return n, nil
}
func (s *testNFSReadTarget) ListHandle(context.Context, []byte) ([]*gonfs.EntryPlus, error) {
	return nil, nil
}
func TestNFSRangeOpenBindsValidatedHandle(t *testing.T) {
	target := &testNFSReadTarget{types: map[string]uint32{"3d": gonfs.NF3Dir, "3d/sample.ply": gonfs.NF3Reg}}
	reader, err := openNFSReadFile(context.Background(), target, &sync.Mutex{}, "3d/sample.ply")
	if err != nil {
		t.Fatal(err)
	}
	// A path replacement after open must not cause another path lookup.
	target.types["3d/sample.ply"] = gonfs.NF3Lnk
	data, err := io.ReadAll(reader)
	if err != nil || string(data) != "abcdef" {
		t.Fatalf("read=%q,%v", data, err)
	}
	if target.lookups != 2 || len(target.readHandles) == 0 || target.readHandles[0] != "3d/sample.ply" {
		t.Fatalf("unexpected I/O: %+v", target)
	}
}
func TestNFSContentOpenRetainsSizeForSeekEnd(t *testing.T) {
	target := &testNFSReadTarget{types: map[string]uint32{"content": gonfs.NF3Reg}}
	reader, err := openNFSReadFile(context.Background(), target, &sync.Mutex{}, "content")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if pos, err := reader.Seek(-2, io.SeekEnd); err != nil || pos != 4 {
		t.Fatalf("seek=%d,%v", pos, err)
	}
	if target.lookups != 1 {
		t.Fatalf("lookups=%d", target.lookups)
	}
}
func TestNFSReadRejectsLinksSpecialUnknownAndPathAliases(t *testing.T) {
	for _, kind := range []uint32{0, gonfs.NF3Lnk, gonfs.NF3FIFO, gonfs.NF3Sock, gonfs.NF3Blk, gonfs.NF3Chr} {
		for _, ancestor := range []bool{false, true} {
			target := &testNFSReadTarget{types: map[string]uint32{"dir": gonfs.NF3Dir, "dir/file": gonfs.NF3Reg}}
			if ancestor {
				target.types["dir"] = kind
			} else {
				target.types["dir/file"] = kind
			}
			if _, err := openNFSReadFile(context.Background(), target, &sync.Mutex{}, "dir/file"); err == nil {
				t.Fatalf("accepted kind=%d ancestor=%v", kind, ancestor)
			}
			if len(target.readHandles) != 0 {
				t.Fatal("rejected path was read")
			}
		}
	}
	for _, name := range []string{"/file", "../file", "dir/../file", "dir/./file", "dir//file", "file/", "file\x00"} {
		target := &testNFSReadTarget{types: map[string]uint32{"dir": gonfs.NF3Dir, "file": gonfs.NF3Reg}}
		if _, err := openNFSReadFile(context.Background(), target, &sync.Mutex{}, name); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
	for _, name := range []string{" file ", " ", "%20", "中文"} {
		target := &testNFSReadTarget{types: map[string]uint32{name: gonfs.NF3Reg}}
		if _, err := openNFSReadFile(context.Background(), target, &sync.Mutex{}, name); err != nil {
			t.Fatalf("rejected exact name %q: %v", name, err)
		}
	}
}
func TestNFSOpenCancellationAndErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	target := &testNFSReadTarget{}
	if _, err := openNFSReadFile(ctx, target, &sync.Mutex{}, "content"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if target.lookups != 0 {
		t.Fatal("canceled open did I/O")
	}
	denied := errors.New("access denied")
	target.lookupErr = denied
	if _, err := openNFSReadFile(context.Background(), target, &sync.Mutex{}, "content"); !errors.Is(err, denied) {
		t.Fatal(err)
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
