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
	"path"
	"reflect"
	"strings"
	"sync"

	"github.com/addp/common/engine/plugin"
	gonfs "github.com/vmware/go-nfs-client/nfs"
)

const publicationStagingKey = "publication_staging_path"

var errNFSPrivatePublicationResource = errors.New("NFS private staging is not a public resource")

type nfsPreparedFile struct {
	Version                         int                              `json:"version"`
	Request                         plugin.NewFilePublicationRequest `json:"request"`
	StagingPath                     string                           `json:"staging_path"`
	Export, Directory, Parent, File []byte
	FSID, FileID                    uint64
	IdentityConfirmed, Ready        bool
}

func publicationStagingPath(info plugin.ConnectionInfo) (string, error) {
	raw, exists := info[publicationStagingKey]
	if !exists {
		return "", nil
	}
	s, ok := raw.(string)
	if !ok {
		return "", errors.New("NFS private staging path must be text")
	}
	if s == "" {
		return "", nil
	}
	if strings.HasPrefix(s, "/") || strings.ContainsRune(s, 0) {
		return "", errors.New("invalid NFS private staging path")
	}
	for _, part := range strings.Split(s, "/") {
		if !validNFSReadName(part) {
			return "", errors.New("invalid NFS private staging path")
		}
	}
	return s, nil
}

func allowPublicNFSPath(info plugin.ConnectionInfo, file string) error {
	staging, err := publicationStagingPath(info)
	if err != nil {
		return err
	}
	if staging != "" {
		// Canonicalize only for this denial check, never to authorize an alias.
		file = path.Clean("/" + file)
		private := "/" + staging
		if file == private || strings.HasPrefix(file, private+"/") {
			return errNFSPrivatePublicationResource
		}
	}
	return nil
}

func privateNFSDirectory(ctx context.Context, protocol nfsPublicationProtocol, staging string) (*gonfs.Fattr, []byte, error) {
	attr, fh, err := resolveNFSRead(ctx, protocol, staging, gonfs.NF3Dir)
	if err != nil {
		return nil, nil, err
	}
	if attr.UID != uint32(os.Getuid()) || attr.FileMode&07777 != 0700 {
		return nil, nil, errors.New("NFS staging must be owned by the service UID with mode 0700")
	}
	return attr, fh, nil
}

func (p *NFSPlugin) publicationConnection(ctx context.Context, info plugin.ConnectionInfo) (*poolEntry, nfsPublicationProtocol, string, error) {
	staging, err := publicationStagingPath(info)
	if err != nil || staging == "" {
		return nil, nil, "", errors.New("NFS private staging is not configured")
	}
	if plugin.GetString(info, "access_mode") == "ro" {
		return nil, nil, "", errors.New("NFS export is read-only")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, "", err
	}
	server, export, err := p.parseConnInfo(info)
	if err != nil {
		return nil, nil, "", err
	}
	entry, err := getOrCreateMount(server, export)
	if err != nil {
		return nil, nil, "", err
	}
	protocol, ok := entry.reader.(nfsPublicationProtocol)
	if !ok {
		return nil, nil, "", errors.New("NFS publication protocol unavailable")
	}
	return entry, protocol, staging, nil
}

func (p *NFSPlugin) PrepareNewFile(ctx context.Context, info plugin.ConnectionInfo, request plugin.NewFilePublicationRequest, content io.Reader) (plugin.PreparedNewFile, error) {
	if err := request.Validate(); err != nil {
		return plugin.PreparedNewFile{}, err
	}
	if err := allowPublicNFSPath(info, request.Target.StringPath()); err != nil {
		return plugin.PreparedNewFile{}, err
	}
	entry, protocol, staging, err := p.publicationConnection(ctx, info)
	if err != nil {
		return plugin.PreparedNewFile{}, err
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	return prepareNFSFile(ctx, protocol, staging, request, content)
}

func prepareNFSFile(ctx context.Context, protocol nfsPublicationProtocol, staging string, request plugin.NewFilePublicationRequest, content io.Reader) (plugin.PreparedNewFile, error) {
	var empty plugin.PreparedNewFile
	if err := request.Validate(); err != nil {
		return empty, err
	}
	// Freeze before target resolution or any I/O side effect.
	encoded, err := json.Marshal(request)
	if err != nil {
		return empty, err
	}
	var frozen plugin.NewFilePublicationRequest
	if err := json.Unmarshal(encoded, &frozen); err != nil {
		return empty, err
	}
	request = frozen
	if content == nil || staging == "" {
		return empty, errors.New("missing prepared content or private staging")
	}
	stagingAttr, directory, err := privateNFSDirectory(ctx, protocol, staging)
	if err != nil {
		return empty, err
	}
	parentPath := path.Dir(request.Target.StringPath())
	if parentPath == "." {
		parentPath = ""
	}
	parentAttr, parent, err := resolveNFSRead(ctx, protocol, parentPath, gonfs.NF3Dir)
	if err != nil {
		return empty, err
	}
	if parentAttr.FSID != stagingAttr.FSID {
		return empty, errors.New("NFS publication crosses filesystems")
	}
	if _, _, err := protocol.LookupAt(parent, path.Base(request.Target.StringPath())); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			err = os.ErrExist
		}
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	record := nfsPreparedFile{Version: 1, Request: request, StagingPath: staging, Export: protocol.Root(), Directory: directory, Parent: parent, FSID: stagingAttr.FSID}
	encode := func() plugin.PreparedNewFile {
		state, _ := json.Marshal(record)
		return plugin.PreparedNewFile{NewFilePublicationRequest: request, ProviderState: state}
	}
	fh, err := protocol.CreateGuarded(directory, request.OperationID)
	record.File = fh
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return empty, err
		}
		return encode(), &plugin.NewFilePreparationError{OperationID: request.OperationID, CleanupPending: true, Cause: err}
	} // Unknown create is never repeated or guessed owned.
	attr, err := protocol.Attr(fh)
	if err != nil || attr == nil || attr.Type != gonfs.NF3Reg || attr.FSID != stagingAttr.FSID || attr.UID != uint32(os.Getuid()) || attr.Nlink != 1 {
		return encode(), &plugin.NewFilePreparationError{OperationID: request.OperationID, CleanupPending: true, Cause: errors.Join(errors.New("NFS private creation identity is unconfirmed"), err)}
	}
	record.FileID, record.IdentityConfirmed = attr.Fileid, true
	prepared := encode()
	fail := func(cause error) (plugin.PreparedNewFile, error) {
		// An owned stage may need later cleanup if cancellation makes I/O unavailable.
		if cleanupErr := cleanupNFSFile(ctx, protocol, staging, prepared, plugin.NewFileNotPublished); cleanupErr != nil {
			return prepared, &plugin.NewFilePreparationError{OperationID: request.OperationID, CleanupPending: true, Cause: errors.Join(cause, cleanupErr)}
		}
		return prepared, cause
	}
	hash := sha256.New()
	writer := &committedNFSWriter{protocol: protocol, ctx: ctx, mu: &sync.Mutex{}, fh: fh}
	// The outer connection lock serializes the transport; the writer's lock is
	// separate and never reacquires that same lock.
	n, err := io.Copy(writer, io.TeeReader(io.LimitReader(content, request.SizeBytes+1), hash))
	if err != nil {
		return fail(err)
	}
	if n != request.SizeBytes || hex.EncodeToString(hash.Sum(nil)) != request.SHA256 {
		return fail(errors.New("prepared bytes differ from declared content"))
	}
	if err := writer.Close(); err != nil {
		return fail(err)
	}
	if err := verifyNFSPreparedBytes(ctx, protocol, record); err != nil {
		return fail(err)
	}
	record.Ready = true
	return encode(), nil
}

func verifyNFSPreparedBytes(ctx context.Context, protocol nfsPublicationProtocol, r nfsPreparedFile) error {
	attr, err := protocol.Attr(r.File)
	if err != nil {
		return err
	}
	if !sameNFSObject(r.File, r.File, attr, r.FSID, r.FileID) || attr.Filesize != uint64(r.Request.SizeBytes) {
		return errors.New("NFS prepared content identity or size changed")
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(&nfsHandleStream{ctx: ctx, protocol: protocol, handle: r.File}, r.Request.SizeBytes+1))
	if err != nil {
		return err
	}
	if n != r.Request.SizeBytes || hex.EncodeToString(h.Sum(nil)) != r.Request.SHA256 {
		return errors.New("NFS stored content differs from prepared bytes")
	}
	return nil
}

func decodeNFSPreparation(ctx context.Context, protocol nfsPublicationProtocol, staging string, prepared plugin.PreparedNewFile) (nfsPreparedFile, error) {
	var r nfsPreparedFile
	if err := prepared.Validate(); err != nil {
		return r, err
	}
	if len(prepared.ProviderState) == 0 || len(prepared.ProviderState) > 8192 {
		return r, errors.New("invalid NFS publication recovery data")
	}
	d := json.NewDecoder(bytes.NewReader(prepared.ProviderState))
	d.DisallowUnknownFields()
	if err := d.Decode(&r); err != nil {
		return r, err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return r, errors.New("trailing NFS publication recovery data")
	}
	if r.Version != 1 || !reflect.DeepEqual(r.Request, prepared.NewFilePublicationRequest) || r.StagingPath != staging || !bytes.Equal(protocol.Root(), r.Export) {
		return r, errors.New("NFS publication binding changed")
	}
	if !r.IdentityConfirmed {
		return r, errors.New("NFS preparation identity is unresolved")
	}
	if err := allowPublicNFSPath(plugin.ConnectionInfo{publicationStagingKey: staging}, prepared.Target.StringPath()); err != nil {
		return r, err
	}
	for _, fh := range [][]byte{r.Directory, r.Parent, r.File, r.Export} {
		if len(fh) == 0 || len(fh) > 64 {
			return r, errors.New("invalid NFS publication handle")
		}
	}
	a, directory, err := privateNFSDirectory(ctx, protocol, staging)
	if err != nil {
		return r, err
	}
	if !bytes.Equal(directory, r.Directory) || a.FSID != r.FSID {
		return r, errors.New("NFS staging identity changed")
	}
	parentPath := path.Dir(r.Request.Target.StringPath())
	if parentPath == "." {
		parentPath = ""
	}
	a, parent, err := resolveNFSRead(ctx, protocol, parentPath, gonfs.NF3Dir)
	if err != nil {
		return r, err
	}
	if !bytes.Equal(parent, r.Parent) || a.FSID != r.FSID {
		return r, errors.New("NFS publication parent changed")
	}
	return r, nil
}

func publishedNFSOutcome(p plugin.PreparedNewFile) plugin.NewFilePublicationOutcome {
	return plugin.NewFilePublicationOutcome{State: plugin.NewFilePublished, CleanupPending: true, Evidence: &plugin.ContentWriteEvidence{Target: p.Target, Disposition: "created", SizeBytes: p.SizeBytes, Committed: true}}
}

func publishNFSFile(ctx context.Context, protocol nfsPublicationProtocol, staging string, p plugin.PreparedNewFile, previous plugin.NewFilePublicationState) (plugin.NewFilePublicationOutcome, error) {
	if previous != plugin.NewFileNotPublished {
		return plugin.NewFilePublicationOutcome{State: plugin.NewFileUnknown, CleanupPending: true}, errors.New("previously dispatched publication is resolve-only")
	}
	notPublished := plugin.NewFilePublicationOutcome{State: plugin.NewFileNotPublished, CleanupPending: true}
	r, err := decodeNFSPreparation(ctx, protocol, staging, p)
	if err != nil {
		return notPublished, err
	}
	if !r.Ready {
		return notPublished, errors.New("NFS preparation is incomplete")
	}
	if err := verifyNFSPreparedBytes(ctx, protocol, r); err != nil {
		return notPublished, err
	}
	if _, _, err := protocol.LookupAt(r.Parent, path.Base(p.Target.StringPath())); err == nil {
		// No LINK from this original dispatch has been sent. An existing leaf
		// is a definite refusal, not evidence of our own publication.
		return notPublished, os.ErrExist
	} else if !errors.Is(err, os.ErrNotExist) {
		return notPublished, err
	}
	// No more than one Provider-level LINK invocation. The caller must persist
	// dispatch intent and obtain System release before entering this function.
	if err := ctx.Err(); err != nil {
		return notPublished, err
	}
	err = protocol.LinkHandle(r.File, r.Parent, path.Base(p.Target.StringPath()))
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return notPublished, err
		}
		return plugin.NewFilePublicationOutcome{State: plugin.NewFileUnknown, CleanupPending: true}, err
	}
	return resolveNFSFile(ctx, protocol, staging, p)
}

func resolveNFSFile(ctx context.Context, protocol nfsPublicationProtocol, staging string, p plugin.PreparedNewFile) (plugin.NewFilePublicationOutcome, error) {
	unknown := plugin.NewFilePublicationOutcome{State: plugin.NewFileUnknown, CleanupPending: true}
	r, err := decodeNFSPreparation(ctx, protocol, staging, p)
	if err != nil {
		return unknown, err
	}
	if !r.Ready {
		return unknown, errors.New("NFS preparation is incomplete")
	}
	attr, fh, err := protocol.LookupAt(r.Parent, path.Base(p.Target.StringPath()))
	if err != nil {
		return unknown, err
	}
	if !sameNFSObject(r.File, fh, attr, r.FSID, r.FileID) {
		return unknown, errors.New("NFS final publication ownership is unresolved")
	}
	if err := verifyNFSPreparedBytes(ctx, protocol, r); err != nil {
		return unknown, err
	}
	out := publishedNFSOutcome(p)
	if _, _, cleanupErr := protocol.LookupAt(r.Directory, r.Request.OperationID); errors.Is(cleanupErr, os.ErrNotExist) {
		out.CleanupPending = false
	}
	return out, nil
}

func cleanupNFSFile(ctx context.Context, protocol nfsPublicationProtocol, staging string, p plugin.PreparedNewFile, state plugin.NewFilePublicationState) error {
	// The trusted owner's persisted dispatch state is required: absence of a
	// final leaf cannot establish that a possibly in-flight LINK never committed.
	if state != plugin.NewFileNotPublished && state != plugin.NewFilePublished {
		return errors.New("cannot clean unresolved NFS publication")
	}
	r, err := decodeNFSPreparation(ctx, protocol, staging, p)
	if err != nil {
		return err
	}
	if state == plugin.NewFilePublished {
		if _, err := resolveNFSFile(ctx, protocol, staging, p); err != nil {
			return err
		}
	}
	a, fh, err := protocol.LookupAt(r.Directory, r.Request.OperationID)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !sameNFSObject(r.File, fh, a, r.FSID, r.FileID) {
		return errors.New("NFS cleanup does not own staged item")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return protocol.RemoveAt(r.Directory, r.Request.OperationID)
}

func (p *NFSPlugin) PublishNewFile(ctx context.Context, info plugin.ConnectionInfo, prepared plugin.PreparedNewFile, previous plugin.NewFilePublicationState) (plugin.NewFilePublicationOutcome, error) {
	if previous != plugin.NewFileNotPublished {
		return plugin.NewFilePublicationOutcome{State: plugin.NewFileUnknown, CleanupPending: true}, errors.New("previously dispatched publication is resolve-only")
	}
	e, protocol, staging, err := p.publicationConnection(ctx, info)
	if err != nil {
		return plugin.NewFilePublicationOutcome{State: plugin.NewFileNotPublished, CleanupPending: true}, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return publishNFSFile(ctx, protocol, staging, prepared, previous)
}
func (p *NFSPlugin) ResolveNewFile(ctx context.Context, info plugin.ConnectionInfo, prepared plugin.PreparedNewFile) (plugin.NewFilePublicationOutcome, error) {
	e, protocol, staging, err := p.publicationConnection(ctx, info)
	if err != nil {
		return plugin.NewFilePublicationOutcome{State: plugin.NewFileUnknown, CleanupPending: true}, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return resolveNFSFile(ctx, protocol, staging, prepared)
}
func (p *NFSPlugin) CleanupNewFile(ctx context.Context, info plugin.ConnectionInfo, prepared plugin.PreparedNewFile, state plugin.NewFilePublicationState) error {
	e, protocol, staging, err := p.publicationConnection(ctx, info)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return cleanupNFSFile(ctx, protocol, staging, prepared, state)
}
