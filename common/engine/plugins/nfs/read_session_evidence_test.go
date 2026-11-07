package nfs

import (
	"context"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	gonfs "github.com/vmware/go-nfs-client/nfs"
)

type evidenceNFSReadTarget struct {
	*testNFSReadTarget
	beforeRoot func()
}

func (p *evidenceNFSReadTarget) Root() []byte {
	if p.beforeRoot != nil {
		f := p.beforeRoot
		p.beforeRoot = nil
		f()
	}
	return p.testNFSReadTarget.Root()
}

func nfsReadEvidenceFixture(t *testing.T) (plugin.ConnectionInfo, *evidenceNFSReadTarget) {
	t.Helper()
	target := &evidenceNFSReadTarget{testNFSReadTarget: &testNFSReadTarget{types: map[string]uint32{
		"sources": gonfs.NF3Dir, "sources/ 原始.gpkg ": gonfs.NF3Reg,
	}}}
	key := poolKey{server: "nfs-read-evidence-" + t.Name(), exportPath: "/owned"}
	poolMu.Lock()
	pool[key] = &poolEntry{reader: target, createdAt: time.Now()}
	poolMu.Unlock()
	t.Cleanup(func() { poolMu.Lock(); delete(pool, key); poolMu.Unlock() })
	return plugin.ConnectionInfo{"server": key.server, "export_path": key.exportPath}, target
}

func requireNFSReadSource(t *testing.T, reader io.ReadCloser, want plugin.EngineCatalogPath) *plugin.QueryReadSet {
	t.Helper()
	evidence, ok := reader.(plugin.ReadSessionEvidence)
	if !ok {
		t.Fatal("actual NFS content reader has no session evidence")
	}
	source, err := evidence.ReadSet(t.Context())
	if err != nil || source == nil || len(source.Paths) != 1 || !reflect.DeepEqual(source.Paths[0], want) {
		t.Fatalf("session source = %+v, %v; want %+v", source, err, want)
	}
	return source
}

func TestNFSContentReadSessionFreezesSourceBeforeIO(t *testing.T) {
	info, target := nfsReadEvidenceFixture(t)
	path := plugin.FileItemPath(14, "sources/ 原始.gpkg ")
	want := plugin.FileItemPath(14, "sources/ 原始.gpkg ")
	target.beforeRoot = func() { path.Segments[2].Name = "neighbor.gpkg" }
	reader, err := (&NFSPlugin{}).OpenContent(t.Context(), info, path, plugin.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil || string(data) != "abcdef" {
		t.Fatal("caller mutation changed opened file", string(data), err)
	}
	requireNFSReadSource(t, reader, want)
	if target.lookups != 2 || len(target.readHandles) == 0 || target.readHandles[0] != want.StringPath() {
		t.Fatal("proof did not bind original actual handle", target.testNFSReadTarget)
	}
}

func TestNFSContentReadEvidenceSurvivesPartialReadSeekAndClose(t *testing.T) {
	info, target := nfsReadEvidenceFixture(t)
	path := plugin.FileItemPath(14, "sources/ 原始.gpkg ")
	reader, err := (&NFSPlugin{}).OpenContent(t.Context(), info, path, plugin.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	// This is source-closure evidence even before EOF, not a completion receipt.
	if _, err := io.ReadFull(reader, make([]byte, 1)); err != nil {
		t.Fatal(err)
	}
	returned := requireNFSReadSource(t, reader, path)
	returned.Paths[0].Segments[2].Name = "forged.gpkg"
	returned.Paths[0].EngineID = 99
	target.types[path.StringPath()] = gonfs.NF3Lnk
	if _, err := reader.(io.Seeker).Seek(5, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 2)
	if n, err := reader.(io.ReaderAt).ReadAt(buf, 0); n != 2 || err != nil || string(buf) != "ab" {
		t.Fatal("ReadAt changed handle", n, string(buf), err)
	}
	if n, err := reader.Read(buf); n != 1 || !errors.Is(err, io.EOF) || string(buf[:n]) != "f" {
		t.Fatal("ReadAt changed sequential cursor", n, string(buf), err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	requireNFSReadSource(t, reader, path)
	if target.lookups != 2 {
		t.Fatal("evidence or random read re-resolved a substituted path", target.lookups)
	}
	for _, handle := range target.readHandles {
		if handle != path.StringPath() {
			t.Fatal("actual source handle changed", handle)
		}
	}
}

func TestNFSContentReadSessionUnprovenIdentityIsUnresolved(t *testing.T) {
	for _, fault := range []string{"missing-engine", "noncanonical-root", "noncanonical-leaf"} {
		t.Run(fault, func(t *testing.T) {
			info, _ := nfsReadEvidenceFixture(t)
			path := plugin.FileItemPath(14, "sources/ 原始.gpkg ")
			switch fault {
			case "missing-engine":
				path.EngineID = 0
			case "noncanonical-root":
				path.Segments[0].Name = "not-a-business-name"
			case "noncanonical-leaf":
				path.Segments[2].Kind = plugin.EngineCatalogKindDirectory
			}
			reader, err := (&NFSPlugin{}).OpenContent(t.Context(), info, path, plugin.ReadOptions{})
			if err != nil {
				t.Fatal("optional proof changed ordinary read", err)
			}
			defer reader.Close()
			data, err := io.ReadAll(reader)
			if err != nil || string(data) != "abcdef" {
				t.Fatal(string(data), err)
			}
			evidence := reader.(plugin.ReadSessionEvidence)
			if source, err := evidence.ReadSet(t.Context()); source != nil || !errors.Is(err, plugin.ErrQueryReadSetUnresolved) {
				t.Fatal("unproven identity became complete evidence", source, err)
			}
		})
	}
}

func TestNFSInternalAndRangeReadersHaveNoSourceEvidence(t *testing.T) {
	info, _ := nfsReadEvidenceFixture(t)
	p := &NFSPlugin{}
	reader, err := p.readFile(t.Context(), info, "sources/ 原始.gpkg ")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if source, err := reader.ReadSet(t.Context()); source != nil || !errors.Is(err, plugin.ErrQueryReadSetUnresolved) {
		t.Fatal("untyped internal read invented identity", source, err)
	}
	ranged, err := p.OpenRange(t.Context(), info, plugin.FileItemPath(14, "sources/ 原始.gpkg "), plugin.ReadOptions{Offset: 1, Length: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer ranged.Close()
	if _, ok := ranged.(plugin.ReadSessionEvidence); ok {
		t.Fatal("range exposed full content session evidence")
	}
	if data, err := io.ReadAll(ranged); err != nil || string(data) != "bc" {
		t.Fatal(string(data), err)
	}
}

func TestNFSCancelledContentOpenReturnsNoSession(t *testing.T) {
	info, target := nfsReadEvidenceFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	reader, err := (&NFSPlugin{}).OpenContent(ctx, info, plugin.FileItemPath(14, "sources/ 原始.gpkg "), plugin.ReadOptions{})
	if reader != nil || !errors.Is(err, context.Canceled) || target.lookups != 0 {
		t.Fatal("cancelled open returned a session or performed I/O", reader, err, target.lookups)
	}
}
