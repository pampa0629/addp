package nfs

import (
	"context"
	"errors"
	"io"
	"os"
	"path"
	"sync"

	"github.com/addp/common/engine/plugin"
	gonfs "github.com/vmware/go-nfs-client/nfs"
	"github.com/vmware/go-nfs-client/nfs/rpc"
	"github.com/vmware/go-nfs-client/nfs/xdr"
)

// The same authenticated RPC transport owns creation and all writes. A missing
// target is created atomically; no Lookup+UNCHECKED sequence proves newness.
type nfsWriteProtocol interface {
	nfsReadProtocol
	CreateGuarded([]byte, string) ([]byte, error)
	RemoveAt([]byte, string) error
	WriteHandle([]byte, uint64, []byte) (int, error)
	CommitHandle([]byte) error
}

func (r *nfsRPCReader) CreateGuarded(parent []byte, name string) ([]byte, error) {
	args := struct {
		rpc.Header
		Where gonfs.Diropargs3
		How   struct {
			Mode uint32
			Attr gonfs.Sattr3
		}
	}{Header: r.header(gonfs.NFSProc3Create), Where: gonfs.Diropargs3{FH: parent, Filename: name}}
	args.How.Mode = 1
	args.How.Attr.Mode = gonfs.SetMode{SetIt: true, Mode: 0644}
	response, err := r.call(&args)
	if err != nil {
		return nil, err
	}
	var result struct {
		FH     gonfs.PostOpFH3
		Attr   gonfs.PostOpAttr
		DirWcc gonfs.WccData
	}
	if err := xdr.Read(response, &result); err != nil {
		return nil, err
	}
	if !result.FH.IsSet || len(result.FH.FH) == 0 || len(result.FH.FH) > 64 {
		return nil, errors.New("created NFS handle unavailable")
	}
	return result.FH.FH, nil
}
func (r *nfsRPCReader) RemoveAt(parent []byte, name string) error {
	args := struct {
		rpc.Header
		Where gonfs.Diropargs3
	}{r.header(gonfs.NFSProc3Remove), gonfs.Diropargs3{FH: parent, Filename: name}}
	response, err := r.call(&args)
	if err != nil {
		return err
	}
	var result gonfs.WccData
	return xdr.Read(response, &result)
}
func (r *nfsRPCReader) WriteHandle(fh []byte, offset uint64, data []byte) (int, error) {
	count := len(data)
	if count == 0 {
		return 0, nil
	}
	if r.maxWrite == 0 {
		return 0, errors.New("NFS write limits unavailable")
	}
	if uint32(count) > r.maxWrite {
		count = int(r.maxWrite)
	}
	args := struct {
		rpc.Header
		FH       []byte
		Offset   uint64
		Count    uint32
		How      uint32
		Contents []byte
	}{r.header(gonfs.NFSProc3Write), fh, offset, uint32(count), 2, data[:count]}
	response, err := r.call(&args)
	if err != nil {
		return 0, err
	}
	var result struct {
		Wcc       gonfs.WccData
		Count     uint32
		How       uint32
		WriteVerf uint64
	}
	if err := xdr.Read(response, &result); err != nil {
		return 0, err
	}
	if result.Count == 0 || result.Count > uint32(count) || result.How != 2 {
		return 0, errors.New("NFS write did not confirm stable progress")
	}
	return int(result.Count), nil
}
func (r *nfsRPCReader) CommitHandle(fh []byte) error {
	args := struct {
		rpc.Header
		FH     []byte
		Offset uint64
		Count  uint32
	}{r.header(gonfs.NFSProc3Commit), fh, 0, 0}
	response, err := r.call(&args)
	if err != nil {
		return err
	}
	var result struct {
		Wcc       gonfs.WccData
		WriteVerf uint64
	}
	return xdr.Read(response, &result)
}

type committedNFSWriter struct {
	protocol    nfsWriteProtocol
	ctx         context.Context
	mu          *sync.Mutex
	fh          []byte
	target      plugin.EngineCatalogPath
	disposition string
	size        int64
	closed      bool
	failure     error
}

func openCommittedNFSWriter(ctx context.Context, protocol nfsWriteProtocol, mu *sync.Mutex, file string, overwrite bool) (*committedNFSWriter, error) {
	dir, name := path.Dir(file), path.Base(file)
	if dir == "." {
		dir = ""
	}
	if !validNFSReadName(name) {
		return nil, errors.New("invalid NFS write name")
	}
	_, parent, err := resolveNFSRead(ctx, protocol, dir, gonfs.NF3Dir)
	if err != nil {
		return nil, err
	}
	existing, _, lookupErr := protocol.LookupAt(parent, name)
	disposition := "created"
	if lookupErr == nil {
		if existing == nil || existing.Type != gonfs.NF3Reg {
			return nil, errors.New("NFS overwrite target is not a regular file")
		}
		if !overwrite {
			return nil, os.ErrExist
		}
		disposition = "replaced"
		if err := protocol.RemoveAt(parent, name); err != nil {
			return nil, err
		}
	} else if !errors.Is(lookupErr, os.ErrNotExist) {
		return nil, lookupErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fh, err := protocol.CreateGuarded(parent, name)
	if err != nil {
		return nil, err
	}
	return &committedNFSWriter{protocol: protocol, ctx: ctx, mu: mu, fh: fh, disposition: disposition}, nil
}
func (w *committedNFSWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, io.ErrClosedPipe
	}
	if w.failure != nil {
		return 0, w.failure
	}
	written := 0
	for written < len(data) {
		if err := w.ctx.Err(); err != nil {
			w.failure = err
			return written, err
		}
		n, err := w.protocol.WriteHandle(w.fh, uint64(w.size), data[written:])
		if n < 0 || n > len(data)-written || (err == nil && n == 0) {
			err = io.ErrShortWrite
			n = 0
		}
		written += n
		w.size += int64(n)
		if err != nil {
			w.failure = err
			return written, err
		}
	}
	return written, nil
}
func (w *committedNFSWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return w.failure
	}
	w.closed = true
	if w.failure != nil {
		return w.failure
	}
	if err := w.ctx.Err(); err != nil {
		w.failure = err
		return err
	}
	w.failure = w.protocol.CommitHandle(w.fh)
	return w.failure
}
func (w *committedNFSWriter) ContentWriteEvidence() (plugin.ContentWriteEvidence, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.closed || w.failure != nil {
		return plugin.ContentWriteEvidence{}, errors.New("NFS content commit unconfirmed")
	}
	result := plugin.ContentWriteEvidence{Target: w.target, Disposition: w.disposition, SizeBytes: w.size, Committed: true}
	return result, result.Validate(w.target)
}
