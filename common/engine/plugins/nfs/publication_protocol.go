package nfs

import (
	"bytes"

	gonfs "github.com/vmware/go-nfs-client/nfs"
	"github.com/vmware/go-nfs-client/nfs/rpc"
	"github.com/vmware/go-nfs-client/nfs/xdr"
)

type nfsPublicationProtocol interface {
	nfsWriteProtocol
	LinkHandle(source, parent []byte, name string) error
}

func (r *nfsRPCReader) LinkHandle(source, parent []byte, name string) error {
	args := struct {
		rpc.Header
		File []byte
		Link gonfs.Diropargs3
	}{r.header(15), source, gonfs.Diropargs3{FH: parent, Filename: name}}
	// One Call, using the existing transport. Its explicit GARBAGE_ARGS retry
	// is covered separately; transport/recv ambiguity is never retried here.
	response, err := r.call(&args)
	if err != nil {
		return err
	}
	var result struct {
		Attr gonfs.PostOpAttr
		Wcc  gonfs.WccData
	}
	return xdr.Read(response, &result)
}

// sameNFSObject deliberately requires the original opaque handle as well as
// native stable identity. Ambiguous aliases remain unresolved, never inferred.
func sameNFSObject(want, got []byte, attr *gonfs.Fattr, fsid, fileid uint64) bool {
	return len(want) != 0 && bytes.Equal(want, got) && attr != nil && attr.Type == gonfs.NF3Reg && attr.FSID == fsid && attr.Fileid == fileid
}
