package nfs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/addp/common/engine/plugin"
	gonfs "github.com/vmware/go-nfs-client/nfs"
)

func nativePublicationFixture(t *testing.T) (plugin.ConnectionInfo, string) {
	t.Helper()
	if os.Getenv("ADDP_NFS_PUBLICATION_INTEGRATION") != "1" {
		t.Skip("exclusive publication fixture not selected")
	}
	root := os.Getenv("ADDP_NFS_TEST_EXPORT")
	if root == "" {
		t.Fatal("missing owned NFS export")
	}
	if _, e := os.Stat(filepath.Join(root, ".addp-online-owner")); e != nil {
		t.Fatal("export ownership marker missing", e)
	}
	return plugin.ConnectionInfo{"server": "127.0.0.1", "export_path": root, "access_mode": "rw", publicationStagingKey: "private"}, root
}
func TestIntegrationNFSPublicationCompleteFileAndPrivateNamespace(t *testing.T) {
	info, root := nativePublicationFixture(t)
	p := &NFSPlugin{}
	ctx := t.Context()
	request := publicationRequest("native complete immutable content")
	prepared, e := p.PrepareNewFile(ctx, info, request, strings.NewReader("native complete immutable content"))
	if e != nil {
		t.Fatal(e)
	}
	final := filepath.Join(root, "outputs", "difference.gpkg")
	if _, e = os.Stat(final); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("prepare exposed final", e)
	}
	// Simulate durable owner storage before dispatch; the private recovery data
	// stays separate from the public request/outputs.
	journal := filepath.Join(root, "private", request.OperationID+".receipt")
	if e = os.WriteFile(journal, prepared.ProviderState, 0600); e != nil {
		t.Fatal(e)
	}
	prepared.ProviderState, e = os.ReadFile(journal)
	if e != nil {
		t.Fatal(e)
	}
	out, e := p.PublishNewFile(ctx, info, prepared, plugin.NewFileNotPublished)
	if e != nil || out.Validate(prepared) != nil || out.State != plugin.NewFilePublished {
		t.Fatal(out, e)
	}
	if e = p.CleanupNewFile(ctx, info, prepared, out.State); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(root, "private", request.OperationID)); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("private stage remains", e)
	}
	reader, e := p.OpenContent(ctx, info, request.Target, plugin.ReadOptions{})
	if e != nil {
		t.Fatal(e)
	}
	source := requireNFSReadSource(t, reader, request.Target)
	source.Paths[0].Segments[len(source.Paths[0].Segments)-1].Name = "not-the-source.gpkg"
	data, e := io.ReadAll(reader)
	reader.Close()
	if e != nil || string(data) != "native complete immutable content" {
		t.Fatal("wrong final bytes", e)
	}
	requireNFSReadSource(t, reader, request.Target)
	entries, e := p.ListChildren(ctx, info, plugin.FileRootPath(14), plugin.ListOptions{})
	if e != nil {
		t.Fatal(e)
	}
	for _, entry := range entries {
		if entry.Name == "private" {
			t.Fatal("private stage entered catalog")
		}
	}
	if _, e = p.OpenContent(ctx, info, plugin.FileItemPath(14, "private/"+request.OperationID+".receipt"), plugin.ReadOptions{}); !errors.Is(e, errNFSPrivatePublicationResource) {
		t.Fatal("private receipt read", e)
	}
	// Ordinary exact-leaf deletion remains usable and is separate from cleanup.
	if e = p.DeleteResource(ctx, info, request.Target); e != nil {
		t.Fatal("ordinary delete", e)
	}
	if _, e = os.Stat(final); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("ordinary delete did not remove final", e)
	}
	if e = os.Remove(journal); e != nil {
		t.Fatal(e)
	}
}

type lostNFSLinkReply struct {
	nfsPublicationProtocol
	calls int
}

func (p *lostNFSLinkReply) LinkHandle(file, parent []byte, name string) error {
	p.calls++
	if e := p.nfsPublicationProtocol.LinkHandle(file, parent, name); e != nil {
		return e
	}
	return io.ErrUnexpectedEOF
}
func nativePublicationProtocol(t *testing.T, info plugin.ConnectionInfo) (nfsPublicationProtocol, func()) {
	t.Helper()
	mount, target, e := dialNFS(plugin.GetString(info, "server"), plugin.GetString(info, "export_path"))
	if e != nil {
		t.Fatal(e)
	}
	reader, e := newNFSRPCReader(target)
	if e != nil {
		mount.Close()
		t.Fatal(e)
	}
	return reader, func() { _ = target.Close(); _ = mount.Unmount(); _ = mount.Close() }
}
func TestIntegrationNFSPublicationLostReplyRecovery(t *testing.T) {
	info, root := nativePublicationFixture(t)
	protocol, closeProtocol := nativePublicationProtocol(t, info)
	defer closeProtocol()
	request := publicationRequest("response loss keeps original bytes")
	prepared, e := prepareNFSFile(t.Context(), protocol, "private", request, strings.NewReader("response loss keeps original bytes"))
	if e != nil {
		t.Fatal(e)
	}
	fault := &lostNFSLinkReply{nfsPublicationProtocol: protocol}
	out, e := publishNFSFile(t.Context(), fault, "private", prepared, plugin.NewFileNotPublished)
	if e == nil || out.State != plugin.NewFileUnknown || fault.calls != 1 {
		t.Fatal(out, e)
	}
	if e = cleanupNFSFile(t.Context(), fault, "private", prepared, plugin.NewFileUnknown); e == nil {
		t.Fatal("unknown stage removed")
	}
	if _, e = os.Stat(filepath.Join(root, "private", request.OperationID)); e != nil {
		t.Fatal("recovery ownership lost", e)
	}
	// A new protocol transport recovers only the original operation.
	recovered, closeRecovered := nativePublicationProtocol(t, info)
	defer closeRecovered()
	out, e = resolveNFSFile(context.Background(), recovered, "private", prepared)
	if e != nil || out.Validate(prepared) != nil || out.State != plugin.NewFilePublished || fault.calls != 1 {
		t.Fatal(out, e)
	}
	if e = cleanupNFSFile(t.Context(), recovered, "private", prepared, out.State); e != nil {
		t.Fatal(e)
	}
	if data, e := os.ReadFile(filepath.Join(root, "outputs", "difference.gpkg")); e != nil || string(data) != "response loss keeps original bytes" {
		t.Fatal("recovery changed final", e)
	}
	if e = os.Remove(filepath.Join(root, "outputs", "difference.gpkg")); e != nil {
		t.Fatal(e)
	}
}

type racingNativeNFS struct {
	nfsPublicationProtocol
	ready   *sync.WaitGroup
	lookups *atomic.Int32
}

func (p *racingNativeNFS) LookupAt(parent []byte, name string) (*gonfs.Fattr, []byte, error) {
	if name == "difference.gpkg" && p.lookups.Add(1) <= 2 {
		p.ready.Done()
		p.ready.Wait()
		return nil, nil, os.ErrNotExist
	}
	return p.nfsPublicationProtocol.LookupAt(parent, name)
}
func TestIntegrationNFSPublicationConcurrentNoOverwrite(t *testing.T) {
	info, root := nativePublicationFixture(t)
	a, closeA := nativePublicationProtocol(t, info)
	defer closeA()
	b, closeB := nativePublicationProtocol(t, info)
	defer closeB()
	rA, e := prepareNFSFile(t.Context(), a, "private", publicationRequest("first complete content"), strings.NewReader("first complete content"))
	if e != nil {
		t.Fatal(e)
	}
	rB, e := prepareNFSFile(t.Context(), b, "private", publicationRequest("second complete content"), strings.NewReader("second complete content"))
	if e != nil {
		t.Fatal(e)
	}
	var ready sync.WaitGroup
	ready.Add(2)
	var lookups atomic.Int32
	type result struct {
		prepared plugin.PreparedNewFile
		protocol nfsPublicationProtocol
		out      plugin.NewFilePublicationOutcome
		err      error
	}
	results := make(chan result, 2)
	for _, work := range []struct {
		prepared plugin.PreparedNewFile
		protocol nfsPublicationProtocol
	}{{rA, a}, {rB, b}} {
		go func(r plugin.PreparedNewFile, protocol nfsPublicationProtocol) {
			wrapped := &racingNativeNFS{nfsPublicationProtocol: protocol, ready: &ready, lookups: &lookups}
			out, e := publishNFSFile(t.Context(), wrapped, "private", r, plugin.NewFileNotPublished)
			results <- result{r, protocol, out, e}
		}(work.prepared, work.protocol)
	}
	winners, losers := 0, 0
	for range 2 {
		r := <-results
		if r.out.State == plugin.NewFilePublished && r.err == nil {
			winners++
		} else if r.out.State == plugin.NewFileNotPublished && errors.Is(r.err, os.ErrExist) {
			losers++
		} else {
			t.Fatal("not a definite competing result", r.out, r.err)
		}
		if e := cleanupNFSFile(t.Context(), r.protocol, "private", r.prepared, r.out.State); e != nil {
			t.Fatal(e)
		}
	}
	if winners != 1 || losers != 1 {
		t.Fatal(winners, losers)
	}
	data, e := os.ReadFile(filepath.Join(root, "outputs", "difference.gpkg"))
	if e != nil || (string(data) != "first complete content" && string(data) != "second complete content") {
		t.Fatal("partial/overwritten content", string(data), e)
	}
	if e = os.Remove(filepath.Join(root, "outputs", "difference.gpkg")); e != nil {
		t.Fatal(e)
	}
}

type unavailableCreatedIdentity struct {
	nfsPublicationProtocol
	created []byte
}

func (p *unavailableCreatedIdentity) CreateGuarded(parent []byte, name string) ([]byte, error) {
	fh, e := p.nfsPublicationProtocol.CreateGuarded(parent, name)
	p.created = append([]byte(nil), fh...)
	return fh, e
}
func (p *unavailableCreatedIdentity) Attr(fh []byte) (*gonfs.Fattr, error) {
	if len(p.created) > 0 && bytes.Equal(fh, p.created) {
		return nil, io.ErrUnexpectedEOF
	}
	return p.nfsPublicationProtocol.Attr(fh)
}
func TestIntegrationNFSPublicationPreparationUnknownRetainsResidue(t *testing.T) {
	info, root := nativePublicationFixture(t)
	protocol, closeProtocol := nativePublicationProtocol(t, info)
	defer closeProtocol()
	fault := &unavailableCreatedIdentity{nfsPublicationProtocol: protocol}
	request := publicationRequest("not uploaded")
	prepared, e := prepareNFSFile(t.Context(), fault, "private", request, strings.NewReader("not uploaded"))
	var pending *plugin.NewFilePreparationError
	if !errors.As(e, &pending) || !pending.CleanupPending || pending.OperationID != request.OperationID || len(prepared.ProviderState) == 0 {
		t.Fatal("preparation uncertainty omitted", e)
	}
	if _, e = os.Stat(filepath.Join(root, "private", request.OperationID)); e != nil {
		t.Fatal("actual private creation not retained", e)
	}
	if _, e = os.Stat(filepath.Join(root, "outputs", "difference.gpkg")); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("incomplete content exposed", e)
	}
	if e = cleanupNFSFile(t.Context(), fault, "private", prepared, plugin.NewFileNotPublished); e == nil {
		t.Fatal("identity guessed from staged name")
	}
	// Test fixture teardown is the administrator of its whole disposable export;
	// this does NOT turn the business operation into a confirmed cleanup result.
	report := map[string]any{"operation_id": request.OperationID, "preparation": "unknown", "cleanup_pending": true, "original_handle_retained": len(fault.created) > 0, "private_residue_retained_until_fixture_destroy": true, "final_exposed": false}
	if evidenceDir := os.Getenv("ADDP_NFS_PUBLICATION_ARTIFACT_DIR"); evidenceDir != "" {
		raw, e := json.MarshalIndent(report, "", "  ")
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(evidenceDir, "preparation-unknown.json"), raw, 0600); e != nil {
			t.Fatal(e)
		}
	}
}
