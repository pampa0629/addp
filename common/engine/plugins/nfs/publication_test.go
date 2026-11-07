package nfs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/google/uuid"
	gonfs "github.com/vmware/go-nfs-client/nfs"
)

type publicationNode struct {
	attr  gonfs.Fattr
	bytes []byte
}
type publicationMemoryNFS struct {
	mu                           sync.Mutex
	objects                      map[string]*publicationNode
	entries                      map[string]map[string]string
	creates, links, removes      int
	linkFailure, commitFailure   bool
	applyLostLink, corrupt       bool
	beforeCreate                 func()
	attrFailure, createReplyLost bool
}

func memoryPublicationNFS() *publicationMemoryNFS {
	p := &publicationMemoryNFS{objects: map[string]*publicationNode{}, entries: map[string]map[string]string{}}
	for i, name := range []string{"root", "private", "outputs", "private-next"} {
		mode := uint32(0755)
		if name == "private" {
			mode = 0700
		}
		p.objects[name] = &publicationNode{attr: gonfs.Fattr{Type: gonfs.NF3Dir, UID: uint32(os.Getuid()), FileMode: mode, FSID: 1, Fileid: uint64(i + 1)}}
		p.entries[name] = map[string]string{}
	}
	p.entries["root"] = map[string]string{"private": "private", "outputs": "outputs", "private-next": "private-next"}
	return p
}
func (p *publicationMemoryNFS) Root() []byte { return []byte("root") }
func (p *publicationMemoryNFS) Attr(h []byte) (*gonfs.Fattr, error) {
	if p.attrFailure && strings.HasPrefix(string(h), "file-") {
		return nil, io.ErrUnexpectedEOF
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	n := p.objects[string(h)]
	if n == nil {
		return nil, os.ErrNotExist
	}
	a := n.attr
	return &a, nil
}
func (p *publicationMemoryNFS) LookupAt(parent []byte, name string) (*gonfs.Fattr, []byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	h := p.entries[string(parent)][name]
	n := p.objects[h]
	if n == nil {
		return nil, nil, os.ErrNotExist
	}
	a := n.attr
	return &a, []byte(h), nil
}
func (p *publicationMemoryNFS) ListHandle(_ context.Context, h []byte) ([]*gonfs.EntryPlus, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []*gonfs.EntryPlus
	for name, key := range p.entries[string(h)] {
		out = append(out, &gonfs.EntryPlus{FileName: name, Attr: gonfs.PostOpAttr{IsSet: true, Attr: p.objects[key].attr}})
	}
	return out, nil
}
func (p *publicationMemoryNFS) ReadHandle(h []byte, offset uint64, b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := p.objects[string(h)]
	if n == nil {
		return 0, os.ErrNotExist
	}
	if offset >= uint64(len(n.bytes)) {
		return 0, io.EOF
	}
	count := copy(b, n.bytes[offset:])
	if p.corrupt && count > 0 {
		b[0] ^= 1
	}
	if int(offset)+count == len(n.bytes) {
		return count, io.EOF
	}
	return count, nil
}
func (p *publicationMemoryNFS) CreateGuarded(parent []byte, name string) ([]byte, error) {
	if p.beforeCreate != nil {
		p.beforeCreate()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.creates++
	if p.entries[string(parent)][name] != "" {
		return nil, os.ErrExist
	}
	h := "file-" + name
	p.objects[h] = &publicationNode{attr: gonfs.Fattr{Type: gonfs.NF3Reg, UID: uint32(os.Getuid()), Nlink: 1, FSID: 1, Fileid: uint64(p.creates + 100)}}
	p.entries[string(parent)][name] = h
	if p.createReplyLost {
		return nil, io.ErrUnexpectedEOF
	}
	return []byte(h), nil
}

func TestNFSPreparationUncertaintyRetainsOriginalPrivateOperation(t *testing.T) {
	for _, lostCreate := range []bool{false, true} {
		t.Run(map[bool]string{false: "identity_unavailable", true: "create_reply_lost"}[lostCreate], func(t *testing.T) {
			p := memoryPublicationNFS()
			p.attrFailure = !lostCreate
			p.createReplyLost = lostCreate
			request := publicationRequest("not yet written")
			prepared, e := prepareNFSFile(t.Context(), p, "private", request, strings.NewReader("not yet written"))
			var unresolved *plugin.NewFilePreparationError
			if !errors.As(e, &unresolved) || !unresolved.CleanupPending || unresolved.OperationID != request.OperationID {
				t.Fatal("uncertainty not reported", e)
			}
			if p.creates != 1 || p.links != 0 || p.removes != 0 || len(p.entries["private"]) != 1 || len(p.entries["outputs"]) != 0 {
				t.Fatal("uncertain creation retried/guessed/removed")
			}
			var state nfsPreparedFile
			if e := json.Unmarshal(prepared.ProviderState, &state); e != nil {
				t.Fatal(e)
			}
			if state.IdentityConfirmed || state.Ready || state.Request.OperationID != request.OperationID || len(state.Directory) == 0 {
				t.Fatal("private operation state lost", state)
			}
			if (len(state.File) == 0) != lostCreate {
				t.Fatal("original create handle discarded or invented")
			}
			if e := cleanupNFSFile(t.Context(), p, "private", prepared, plugin.NewFileNotPublished); e == nil || p.removes != 0 {
				t.Fatal("unconfirmed identity deleted by name", e)
			}
		})
	}
}
func (p *publicationMemoryNFS) WriteHandle(h []byte, off uint64, b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := p.objects[string(h)]
	if int(off) != len(n.bytes) {
		return 0, errors.New("nonsequential write")
	}
	n.bytes = append(n.bytes, b...)
	n.attr.Filesize = uint64(len(n.bytes))
	return len(b), nil
}
func (p *publicationMemoryNFS) CommitHandle([]byte) error {
	if p.commitFailure {
		return errors.New("commit lost")
	}
	return nil
}
func (p *publicationMemoryNFS) LinkHandle(h, parent []byte, name string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.links++
	if p.entries[string(parent)][name] != "" {
		return os.ErrExist
	}
	if !p.linkFailure || p.applyLostLink {
		p.entries[string(parent)][name] = string(h)
		p.objects[string(h)].attr.Nlink++
	}
	if p.linkFailure {
		return errors.New("response lost")
	}
	return nil
}
func (p *publicationMemoryNFS) RemoveAt(parent []byte, name string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.removes++
	h := p.entries[string(parent)][name]
	if h == "" {
		return os.ErrNotExist
	}
	delete(p.entries[string(parent)], name)
	p.objects[h].attr.Nlink--
	return nil
}
func publicationRequest(data string) plugin.NewFilePublicationRequest {
	h := sha256.Sum256([]byte(data))
	return plugin.NewFilePublicationRequest{OperationID: uuid.NewString(), Target: plugin.FileItemPath(14, "outputs/difference.gpkg"), SizeBytes: int64(len(data)), SHA256: hex.EncodeToString(h[:])}
}
func preparePublication(t *testing.T, p *publicationMemoryNFS) plugin.PreparedNewFile {
	t.Helper()
	r, e := prepareNFSFile(t.Context(), p, "private", publicationRequest("complete closed GPKG bytes"), strings.NewReader("complete closed GPKG bytes"))
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func TestNFSPublicationCompleteBytesAndIndependentCleanup(t *testing.T) {
	p := memoryPublicationNFS()
	r := preparePublication(t, p)
	if p.links != 0 || len(p.entries["outputs"]) != 0 {
		t.Fatal("preparation exposed final file")
	}
	publicJSON, e := json.Marshal(r)
	if e != nil || bytes.Contains(publicJSON, []byte("file-")) || bytes.Contains(publicJSON, []byte("ProviderState")) {
		t.Fatal("private handles leaked", string(publicJSON), e)
	}
	o, e := publishNFSFile(t.Context(), p, "private", r, plugin.NewFileNotPublished)
	if e != nil || o.Validate(r) != nil || o.State != plugin.NewFilePublished || !o.CleanupPending {
		t.Fatal(o, e)
	}
	if e := cleanupNFSFile(t.Context(), p, "private", r, o.State); e != nil {
		t.Fatal(e)
	}
	if len(p.entries["private"]) != 0 || len(p.entries["outputs"]) != 1 {
		t.Fatal("cleanup removed final or retained private name")
	}
	o, e = resolveNFSFile(t.Context(), p, "private", r)
	if e != nil || o.State != plugin.NewFilePublished || o.CleanupPending || p.links != 1 {
		t.Fatal("resolve republished or lost identity", o, e, p.links)
	}
	o, e = publishNFSFile(t.Context(), p, "private", r, plugin.NewFileUnknown)
	if e == nil || o.State != plugin.NewFileUnknown || p.links != 1 {
		t.Fatal("existing operation reissued LINK", o, e)
	}
}
func TestNFSPublicationUnknownRetainsOwnershipAndNeverRetries(t *testing.T) {
	for _, applied := range []bool{false, true} {
		t.Run(map[bool]string{false: "before_apply", true: "after_apply"}[applied], func(t *testing.T) {
			p := memoryPublicationNFS()
			r := preparePublication(t, p)
			p.linkFailure = true
			p.applyLostLink = applied
			o, e := publishNFSFile(t.Context(), p, "private", r, plugin.NewFileNotPublished)
			if e == nil || o.State != plugin.NewFileUnknown || o.Evidence != nil || p.links != 1 {
				t.Fatal(o, e)
			}
			if e := cleanupNFSFile(t.Context(), p, "private", r, o.State); e == nil || p.removes != 0 || len(p.entries["private"]) != 1 {
				t.Fatal("unknown stage destroyed", e)
			}
			o, e = resolveNFSFile(t.Context(), p, "private", r)
			if p.links != 1 {
				t.Fatal("resolve retried LINK")
			}
			if applied {
				if e != nil || o.Validate(r) != nil || o.State != plugin.NewFilePublished {
					t.Fatal(o, e)
				}
			} else if e == nil || o.State != plugin.NewFileUnknown {
				t.Fatal("absence inferred no historical commit", o, e)
			}
		})
	}
}
func TestNFSPublicationRefusesUnprovenPreparation(t *testing.T) {
	for _, fault := range []string{"mode", "uid", "symlink", "cross_fs", "digest", "short", "long", "remote_corrupt", "commit", "cancelled"} {
		t.Run(fault, func(t *testing.T) {
			p := memoryPublicationNFS()
			req := publicationRequest("bytes")
			data := "bytes"
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch fault {
			case "mode":
				p.objects["private"].attr.FileMode = 0755
			case "uid":
				p.objects["private"].attr.UID++
			case "symlink":
				p.objects["private"].attr.Type = gonfs.NF3Lnk
			case "cross_fs":
				p.objects["outputs"].attr.FSID = 2
			case "digest":
				req.SHA256 = strings.Repeat("0", 64)
			case "short":
				data = "byte"
			case "long":
				data = "bytes!"
			case "remote_corrupt":
				p.corrupt = true
			case "commit":
				p.commitFailure = true
			case "cancelled":
				cancel()
			}
			_, e := prepareNFSFile(ctx, p, "private", req, strings.NewReader(data))
			if e == nil || p.links != 0 || len(p.entries["outputs"]) != 0 {
				t.Fatal("invalid content became visible", e)
			}
		})
	}
}
func TestNFSPublicationCancellationAndRetargeting(t *testing.T) {
	p := memoryPublicationNFS()
	r := preparePublication(t, p)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	o, e := publishNFSFile(ctx, p, "private", r, plugin.NewFileNotPublished)
	if e == nil || o.State != plugin.NewFileNotPublished || p.links != 0 {
		t.Fatal(o, e)
	}
	changed := r
	changed.Target = plugin.FileItemPath(14, "outputs/neighbor.gpkg")
	if _, e := publishNFSFile(t.Context(), p, "private", changed, plugin.NewFileNotPublished); e == nil || p.links != 0 {
		t.Fatal("retarget accepted", e)
	}
	p.entries["root"]["outputs"] = "private-next"
	if _, e := publishNFSFile(t.Context(), p, "private", r, plugin.NewFileNotPublished); e == nil || p.links != 0 {
		t.Fatal("renamed parent accepted", e)
	}
}

type racingPublicationNFS struct {
	*publicationMemoryNFS
	ready   sync.WaitGroup
	lookups atomic.Int32
}

func (p *racingPublicationNFS) LookupAt(parent []byte, name string) (*gonfs.Fattr, []byte, error) {
	if string(parent) == "outputs" && name == "difference.gpkg" && p.lookups.Add(1) <= 2 {
		p.ready.Done()
		p.ready.Wait()
		return nil, nil, os.ErrNotExist
	}
	return p.publicationMemoryNFS.LookupAt(parent, name)
}
func TestNFSPublicationConcurrentSameNameHasOneWinner(t *testing.T) {
	mem := memoryPublicationNFS()
	a, b := preparePublication(t, mem), preparePublication(t, mem)
	p := &racingPublicationNFS{publicationMemoryNFS: mem}
	p.ready.Add(2)
	type result struct {
		prepared plugin.PreparedNewFile
		out      plugin.NewFilePublicationOutcome
		err      error
	}
	results := make(chan result, 2)
	for _, r := range []plugin.PreparedNewFile{a, b} {
		go func(r plugin.PreparedNewFile) {
			o, e := publishNFSFile(t.Context(), p, "private", r, plugin.NewFileNotPublished)
			results <- result{r, o, e}
		}(r)
	}
	winners, losers := 0, 0
	for range 2 {
		r := <-results
		switch r.out.State {
		case plugin.NewFilePublished:
			if r.err != nil {
				t.Fatal(r.err)
			}
			winners++
		case plugin.NewFileNotPublished:
			if !errors.Is(r.err, os.ErrExist) {
				t.Fatal("not a definite refusal", r.err)
			}
			if e := cleanupNFSFile(t.Context(), p, "private", r.prepared, r.out.State); e != nil {
				t.Fatal("loser cannot clean its own stage", e)
			}
			losers++
		default:
			t.Fatal("definite collision classified ambiguous", r.out, r.err)
		}
	}
	if winners != 1 || losers != 1 || p.links != 2 || len(p.entries["outputs"]) != 1 || len(p.entries["private"]) != 1 {
		t.Fatal("wrong winner/cleanup", winners, losers, p.links)
	}
}
func TestNFSPublicationFreezesTargetBeforeCreate(t *testing.T) {
	p := memoryPublicationNFS()
	request := publicationRequest("data")
	p.beforeCreate = func() { request.Target.Segments[len(request.Target.Segments)-1].Name = "neighbor.gpkg" }
	prepared, e := prepareNFSFile(t.Context(), p, "private", request, strings.NewReader("data"))
	if e != nil {
		t.Fatal(e)
	}
	if prepared.Target.StringPath() != "outputs/difference.gpkg" {
		t.Fatal("caller mutation retargeted preparation", prepared.Target)
	}
}
func TestNFSUnknownPublicationCannotBeRedispatched(t *testing.T) {
	p := memoryPublicationNFS()
	r := preparePublication(t, p)
	p.linkFailure = true
	p.applyLostLink = true
	_, _ = publishNFSFile(t.Context(), p, "private", r, plugin.NewFileNotPublished)
	// The destination may be replaced or removed after a response was lost.
	delete(p.entries["outputs"], "difference.gpkg")
	o, e := publishNFSFile(t.Context(), p, "private", r, plugin.NewFileUnknown)
	if e == nil || o.State != plugin.NewFileUnknown || p.links != 1 {
		t.Fatal("unknown outcome reissued LINK", o, e, p.links)
	}
	if e := cleanupNFSFile(t.Context(), p, "private", r, plugin.NewFileUnknown); e == nil || p.removes != 0 {
		t.Fatal("unknown stage discarded", e)
	}
}
func TestNFSPublicationCleanupRejectsSubstitution(t *testing.T) {
	p := memoryPublicationNFS()
	r := preparePublication(t, p)
	p.entries["private"][r.OperationID] = "outputs"
	if e := cleanupNFSFile(t.Context(), p, "private", r, plugin.NewFileNotPublished); e == nil || p.removes != 0 {
		t.Fatal("cleanup removed foreign item", e)
	}
}
func TestNFSPrivatePublicationDirectoryCannotEnterPublicInterfaces(t *testing.T) {
	p := &NFSPlugin{}
	protocol := memoryPublicationNFS()
	protocol.entries["root"]["internal"] = "internal"
	protocol.objects["internal"] = &publicationNode{attr: gonfs.Fattr{Type: gonfs.NF3Dir, UID: uint32(os.Getuid()), FileMode: 0755, FSID: 1, Fileid: 9}}
	protocol.entries["internal"] = map[string]string{"stage": "private", "stage-next": "private-next"}
	key := poolKey{server: "publication-valid-interfaces-fixture", exportPath: "/owned"}
	poolMu.Lock()
	pool[key] = &poolEntry{reader: protocol, createdAt: time.Now()}
	poolMu.Unlock()
	t.Cleanup(func() { poolMu.Lock(); delete(pool, key); poolMu.Unlock() })
	info := plugin.ConnectionInfo{"server": key.server, "export_path": key.exportPath, publicationStagingKey: "internal/stage"}
	privateFile, e := protocol.CreateGuarded([]byte("private"), "file")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = protocol.WriteHandle(privateFile, 0, []byte("private staged content")); e != nil {
		t.Fatal(e)
	}
	// Real positive controls on this same valid pool: create/write/commit/read/Facts.
	sibling := plugin.FileItemPath(14, "sibling.bin")
	writer, e := p.CreateContent(t.Context(), info, sibling, plugin.WriteOptions{})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = writer.Write([]byte("sibling data")); e != nil {
		t.Fatal(e)
	}
	if e = writer.Close(); e != nil {
		t.Fatal(e)
	}
	reader, e := p.OpenContent(t.Context(), info, sibling, plugin.ReadOptions{})
	if e != nil {
		t.Fatal(e)
	}
	data, e := io.ReadAll(reader)
	reader.Close()
	if e != nil || string(data) != "sibling data" {
		t.Fatal(e, string(data))
	}
	if _, e = p.DescribeEngineCatalogFacts(t.Context(), info, sibling, plugin.EngineCatalogFactsOptions{}); e != nil {
		t.Fatal(e)
	}
	entries, e := p.ListChildren(t.Context(), info, plugin.FileDirectoryPath(14, "internal"), plugin.ListOptions{})
	if e != nil {
		t.Fatal(e)
	}
	names := map[string]bool{}
	for _, entry := range entries {
		names[entry.Name] = true
	}
	if names["stage"] || !names["stage-next"] {
		t.Fatal("ancestor leaks private stage or hides neighbor", names)
	}
	for _, name := range []string{"internal/stage", "internal/stage/file", "internal/./stage/file", "internal/stage/../stage/file"} {
		if allowPublicNFSPath(info, name) == nil {
			t.Fatal("private alias accepted", name)
		}
	}
	for _, name := range []string{"", "internal", "internal/stage-next", "internal/staged/file"} {
		if e := allowPublicNFSPath(info, name); e != nil {
			t.Fatal("sibling or ancestor blocked", name, e)
		}
	}
	leaf := plugin.FileItemPath(14, "internal/stage/file")
	dir := plugin.FileDirectoryPath(14, "internal/stage")
	if _, e := p.ListChildren(t.Context(), info, dir, plugin.ListOptions{}); !errors.Is(e, errNFSPrivatePublicationResource) {
		t.Fatal("listed private directory")
	}
	if _, e := p.ResolvePath(t.Context(), info, dir); !errors.Is(e, errNFSPrivatePublicationResource) {
		t.Fatal("resolved private directory")
	}
	if _, e := p.DescribeEngineCatalogFacts(t.Context(), info, leaf, plugin.EngineCatalogFactsOptions{}); !errors.Is(e, errNFSPrivatePublicationResource) {
		t.Fatal("private facts")
	}
	if _, e := p.OpenContent(t.Context(), info, leaf, plugin.ReadOptions{}); !errors.Is(e, errNFSPrivatePublicationResource) {
		t.Fatal("private read")
	}
	if _, e := p.OpenRange(t.Context(), info, leaf, plugin.ReadOptions{Length: 1}); !errors.Is(e, errNFSPrivatePublicationResource) {
		t.Fatal("private range")
	}
	if _, e := p.CreateContent(t.Context(), info, leaf, plugin.WriteOptions{}); !errors.Is(e, errNFSPrivatePublicationResource) {
		t.Fatal("private write")
	}
	if e := p.DeleteResource(t.Context(), info, leaf); !errors.Is(e, errNFSPrivatePublicationResource) {
		t.Fatal("private delete")
	}
	if e := p.MkdirAll(t.Context(), info, "internal/stage/new"); !errors.Is(e, errNFSPrivatePublicationResource) {
		t.Fatal("private mkdir")
	}
}
func TestNFSRootEnumerationHidesOnlyPrivateStaging(t *testing.T) {
	p := memoryPublicationNFS()
	key := poolKey{server: "publication-enumeration-fixture", exportPath: "/owned"}
	poolMu.Lock()
	pool[key] = &poolEntry{reader: p, createdAt: time.Now()}
	poolMu.Unlock()
	t.Cleanup(func() { poolMu.Lock(); delete(pool, key); poolMu.Unlock() })
	info := plugin.ConnectionInfo{"server": key.server, "export_path": key.exportPath, publicationStagingKey: "private"}
	entries, e := (&NFSPlugin{}).ListChildren(t.Context(), info, plugin.FileRootPath(14), plugin.ListOptions{})
	if e != nil {
		t.Fatal(e)
	}
	names := map[string]bool{}
	for _, entry := range entries {
		names[entry.Name] = true
	}
	if names["private"] || !names["private-next"] || !names["outputs"] {
		t.Fatal("private namespace leaked or sibling hidden", names)
	}
}
