package nfs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strings"

	gonfs "github.com/vmware/go-nfs-client/nfs"
	"github.com/vmware/go-nfs-client/nfs/rpc"
	"github.com/vmware/go-nfs-client/nfs/xdr"
)

// All read operations use the existing authenticated RPC transport. Handles
// remain private I/O state and are never persisted as catalog or grant identity.
type nfsReadProtocol interface {
	Root() []byte
	Attr([]byte) (*gonfs.Fattr, error)
	LookupAt([]byte, string) (*gonfs.Fattr, []byte, error)
	ReadHandle([]byte, uint64, []byte) (int, error)
	ListHandle(context.Context, []byte) ([]*gonfs.EntryPlus, error)
}

type nfsRPCCaller interface {
	Call(any) (io.ReadSeeker, error)
}

type nfsRPCReader struct {
	target  nfsRPCCaller
	root    []byte
	auth    rpc.Auth
	maxRead uint32
}

func (r *nfsRPCReader) Root() []byte { return append([]byte(nil), r.root...) }
func (r *nfsRPCReader) header(proc uint32) rpc.Header {
	return rpc.Header{Rpcvers: 2, Prog: gonfs.Nfs3Prog, Vers: gonfs.Nfs3Vers, Proc: proc, Cred: r.auth, Verf: rpc.AuthNull}
}
func (r *nfsRPCReader) call(args any) (io.ReadSeeker, error) {
	response, err := r.target.Call(args)
	if err != nil {
		return nil, err
	}
	status, err := xdr.ReadUint32(response)
	if err != nil {
		return nil, err
	}
	if err := gonfs.NFS3Error(status); err != nil {
		return nil, err
	}
	return response, nil
}
func (r *nfsRPCReader) Attr(fh []byte) (*gonfs.Fattr, error) {
	args := struct {
		rpc.Header
		FH []byte
	}{r.header(1), fh}
	response, err := r.call(&args)
	if err != nil {
		return nil, err
	}
	var attr gonfs.Fattr
	if err = xdr.Read(response, &attr); err != nil {
		return nil, err
	}
	return &attr, nil
}
func (r *nfsRPCReader) LookupAt(parent []byte, name string) (*gonfs.Fattr, []byte, error) {
	args := struct {
		rpc.Header
		What gonfs.Diropargs3
	}{r.header(gonfs.NFSProc3Lookup), gonfs.Diropargs3{FH: parent, Filename: name}}
	response, err := r.call(&args)
	if err != nil {
		return nil, nil, err
	}
	fh, err := readNFSOpaque(response, 64)
	if err != nil || len(fh) == 0 {
		return nil, nil, errors.New("invalid NFS file handle")
	}
	var attr gonfs.PostOpAttr
	if err = xdr.Read(response, &attr); err != nil {
		return nil, nil, err
	}
	if !attr.IsSet {
		a, e := r.Attr(fh)
		return a, fh, e
	}
	return &attr.Attr, fh, nil
}
func readNFSOpaque(reader io.Reader, limit uint32) ([]byte, error) {
	n, err := xdr.ReadUint32(reader)
	if err != nil {
		return nil, err
	}
	if n > limit {
		return nil, errors.New("NFS opaque value exceeds bound")
	}
	data := make([]byte, n)
	if _, err = io.ReadFull(reader, data); err != nil {
		return nil, err
	}
	var padding [3]byte
	_, err = io.ReadFull(reader, padding[:(4-n%4)%4])
	return data, err
}
func (r *nfsRPCReader) ReadHandle(fh []byte, offset uint64, p []byte) (int, error) {
	count := len(p)
	if count > 64*1024 {
		count = 64 * 1024
	}
	if r.maxRead > 0 && uint32(count) > r.maxRead {
		count = int(r.maxRead)
	}
	if count == 0 {
		return 0, nil
	}
	args := struct {
		rpc.Header
		FH     []byte
		Offset uint64
		Count  uint32
	}{r.header(gonfs.NFSProc3Read), fh, offset, uint32(count)}
	response, err := r.call(&args)
	if err != nil {
		return 0, err
	}
	var attr gonfs.PostOpAttr
	var n uint32
	var eof bool
	if err = xdr.Read(response, &attr); err != nil {
		return 0, err
	}
	if attr.IsSet && attr.Attr.Type != gonfs.NF3Reg {
		return 0, errors.New("NFS read target is not a regular file")
	}
	if err = xdr.Read(response, &n); err != nil {
		return 0, err
	}
	if err = xdr.Read(response, &eof); err != nil {
		return 0, err
	}
	data, err := readNFSOpaque(response, uint32(count))
	if err != nil {
		return 0, err
	}
	if n != uint32(len(data)) || n > uint32(count) || n == 0 && !eof {
		return 0, errors.New("invalid NFS read response")
	}
	copy(p, data)
	if eof {
		return len(data), io.EOF
	}
	return len(data), nil
}
func (r *nfsRPCReader) ListHandle(ctx context.Context, fh []byte) ([]*gonfs.EntryPlus, error) {
	var entries []*gonfs.EntryPlus
	var cookie, verifier uint64
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		args := struct {
			rpc.Header
			FH                 []byte
			Cookie, CookieVerf uint64
			DirCount, MaxCount uint32
		}{r.header(gonfs.NFSProc3ReadDirPlus), fh, cookie, verifier, 4096, 32768}
		response, err := r.call(&args)
		if err != nil {
			return nil, err
		}
		var attr gonfs.PostOpAttr
		if err = xdr.Read(response, &attr); err != nil {
			return nil, err
		}
		if attr.IsSet && attr.Attr.Type != gonfs.NF3Dir {
			return nil, errors.New("NFS list target is not a directory")
		}
		var nextVerifier uint64
		if err = xdr.Read(response, &nextVerifier); err != nil {
			return nil, err
		}
		if cookie != 0 && nextVerifier != verifier {
			return nil, errors.New("NFS directory changed during listing")
		}
		verifier = nextVerifier
		previous := cookie
		for {
			var follows bool
			if err = xdr.Read(response, &follows); err != nil {
				return nil, err
			}
			if !follows {
				break
			}
			item := new(gonfs.EntryPlus)
			if err = xdr.Read(response, &item.FileId); err != nil {
				return nil, err
			}
			name, e := readNFSOpaque(response, 255)
			if e != nil {
				return nil, e
			}
			item.FileName = string(name)
			if err = xdr.Read(response, &item.Cookie); err != nil {
				return nil, err
			}
			if err = xdr.Read(response, &item.Attr); err != nil {
				return nil, err
			}
			if err = xdr.Read(response, &item.Handle.IsSet); err != nil {
				return nil, err
			}
			if item.Handle.IsSet {
				item.Handle.FH, err = readNFSOpaque(response, 64)
				if err != nil {
					return nil, err
				}
			}
			cookie = item.Cookie
			entries = append(entries, item)
		}
		var eof bool
		if err = xdr.Read(response, &eof); err != nil {
			return nil, err
		}
		if eof {
			return entries, nil
		}
		if cookie == previous {
			return nil, errors.New("NFS directory listing made no progress")
		}
	}
}
func validNFSReadName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/\x00")
}
func resolveNFSRead(ctx context.Context, protocol nfsReadProtocol, path string, want uint32) (*gonfs.Fattr, []byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if strings.HasPrefix(path, "/") || strings.ContainsRune(path, 0) {
		return nil, nil, errors.New("NFS read path must be relative")
	}
	fh := protocol.Root()
	if len(fh) == 0 {
		return nil, nil, errors.New("NFS export handle is unavailable")
	}
	attr, err := protocol.Attr(fh)
	if err != nil {
		return nil, nil, err
	}
	if attr == nil || attr.Type != gonfs.NF3Dir {
		return nil, nil, errors.New("NFS export is not a directory")
	}
	if path != "" {
		for _, part := range strings.Split(path, "/") {
			if !validNFSReadName(part) {
				return nil, nil, errors.New("invalid NFS read path component")
			}
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
			if attr.Type != gonfs.NF3Dir {
				return nil, nil, errors.New("NFS read ancestor is not a directory")
			}
			attr, fh, err = protocol.LookupAt(fh, part)
			if err != nil {
				return nil, nil, err
			}
			if attr == nil || len(fh) == 0 || (attr.Type != gonfs.NF3Dir && attr.Type != gonfs.NF3Reg) {
				return nil, nil, errors.New("NFS links, special files and unknown types are unsupported")
			}
		}
	}
	if attr.Type != want || attr.Filesize > math.MaxInt64 {
		return nil, nil, errors.New("NFS target type or size is invalid")
	}
	return attr, append([]byte(nil), fh...), nil
}

type nfsHandleStream struct {
	ctx      context.Context
	protocol nfsReadProtocol
	handle   []byte
	offset   int64
}

func (r *nfsHandleStream) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.protocol.ReadHandle(r.handle, uint64(r.offset), p)
	r.offset += int64(n)
	return n, err
}
func (r *nfsHandleStream) Close() error { return nil }
func (r *nfsHandleStream) Seek(offset int64, whence int) (int64, error) {
	if whence == io.SeekCurrent {
		if offset > 0 && r.offset > math.MaxInt64-offset {
			return 0, errors.New("NFS seek overflow")
		}
		offset += r.offset
	} else if whence != io.SeekStart {
		return 0, fmt.Errorf("unsupported NFS handle seek: %d", whence)
	}
	if offset < 0 {
		return 0, errors.New("negative NFS seek")
	}
	r.offset = offset
	return offset, nil
}

func newNFSRPCReader(target *gonfs.Target) (*nfsRPCReader, error) {
	_, root, err := target.Lookup("")
	if err != nil {
		return nil, err
	}
	if len(root) == 0 || len(root) > 64 {
		return nil, errors.New("invalid NFS export handle")
	}
	fsinfo, err := target.FSInfo()
	if err != nil {
		return nil, err
	}
	if fsinfo == nil || fsinfo.RTMax == 0 || fsinfo.RTPref == 0 {
		return nil, errors.New("NFS read limits are unavailable")
	}
	maxRead := fsinfo.RTPref
	if maxRead > fsinfo.RTMax {
		maxRead = fsinfo.RTMax
	}
	if maxRead > 64*1024 {
		maxRead = 64 * 1024
	}
	auth := rpc.NewAuthUnix("addp-nfs-client", uint32(os.Getuid()), uint32(os.Getgid())).Auth()
	return &nfsRPCReader{target: target, root: append([]byte(nil), root...), auth: auth, maxRead: maxRead}, nil
}
