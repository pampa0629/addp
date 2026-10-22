package nfs

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"testing"

	gonfs "github.com/vmware/go-nfs-client/nfs"
	"github.com/vmware/go-nfs-client/nfs/xdr"
)

type fixtureNFSCALL struct {
	t       *testing.T
	calls   int
	replies [][]byte
	request func(any)
}

func (f *fixtureNFSCALL) Call(args any) (io.ReadSeeker, error) {
	f.t.Helper()
	if f.calls >= len(f.replies) {
		f.t.Fatal("unexpected RPC")
	}
	if f.request != nil {
		f.request(args)
	}
	response := bytes.NewReader(f.replies[f.calls])
	f.calls++
	return response, nil
}
func nfsResponse(t *testing.T, values ...any) []byte {
	t.Helper()
	var out bytes.Buffer
	for _, v := range values {
		if err := xdr.Write(&out, v); err != nil {
			t.Fatal(err)
		}
	}
	return out.Bytes()
}
func TestNFSReadProtocolBindsHandleAndBoundsWireValues(t *testing.T) {
	for _, fault := range []string{"none", "length-mismatch", "too-large", "wrong-type", "zero-progress", "status-error", "truncated"} {
		t.Run(fault, func(t *testing.T) {
			attr := gonfs.PostOpAttr{IsSet: true, Attr: gonfs.Fattr{Type: gonfs.NF3Reg}}
			count := uint32(3)
			data := []byte("abc")
			eof := false
			status := uint32(0)
			switch fault {
			case "length-mismatch":
				count = 2
			case "too-large":
				data = []byte("oversize")
				count = 8
			case "wrong-type":
				attr.Attr.Type = gonfs.NF3Lnk
			case "zero-progress":
				data = nil
				count = 0
			case "status-error":
				status = 13
			}
			response := nfsResponse(t, status, attr, count, eof, data)
			if fault == "truncated" {
				response = response[:len(response)-4]
			}
			call := &fixtureNFSCALL{t: t, replies: [][]byte{response}, request: func(args any) {
				value := reflect.ValueOf(args).Elem()
				if !bytes.Equal(value.FieldByName("FH").Bytes(), []byte("verified-handle")) || value.FieldByName("Offset").Uint() != 7 || value.FieldByName("Proc").Uint() != gonfs.NFSProc3Read {
					t.Fatal("read changed handle/offset/procedure")
				}
			}}
			protocol := &nfsRPCReader{target: call}
			buffer := make([]byte, 3)
			n, err := protocol.ReadHandle([]byte("verified-handle"), 7, buffer)
			if fault == "none" {
				if n != 3 || err != nil || string(buffer) != "abc" {
					t.Fatalf("read=%d,%q,%v", n, buffer, err)
				}
			} else if err == nil || n != 0 {
				t.Fatalf("accepted invalid response: %d,%v", n, err)
			}
		})
	}
}
func TestNFSLookupMissingAttributesUsesSameHandleGetattr(t *testing.T) {
	call := &fixtureNFSCALL{t: t, replies: [][]byte{
		nfsResponse(t, uint32(0), []byte("exact"), gonfs.PostOpAttr{IsSet: false}),
		nfsResponse(t, uint32(0), gonfs.Fattr{Type: gonfs.NF3Reg, Filesize: 12}),
	}}
	protocol := &nfsRPCReader{target: call}
	attr, fh, err := protocol.LookupAt([]byte("parent"), " file ")
	if err != nil || attr == nil || attr.Type != gonfs.NF3Reg || string(fh) != "exact" || call.calls != 2 {
		t.Fatalf("lookup=%v,%q,%v", attr, fh, err)
	}
}
func TestNFSDirectoryProtocolRetainsNativeTypesAndExactNames(t *testing.T) {
	attr := gonfs.PostOpAttr{IsSet: true, Attr: gonfs.Fattr{Type: gonfs.NF3Dir}}
	// One READDIRPLUS page, containing a symlink and an unknown type. Catalog
	// filtering must use these native facts rather than permission mode bits.
	reply := nfsResponse(t, uint32(0), attr, uint64(1), true, uint64(1), " link ", uint64(2), gonfs.PostOpAttr{IsSet: true, Attr: gonfs.Fattr{Type: gonfs.NF3Lnk}}, false, true, uint64(2), "unknown", uint64(3), gonfs.PostOpAttr{IsSet: false}, false, false, true)
	call := &fixtureNFSCALL{t: t, replies: [][]byte{reply}}
	entries, err := (&nfsRPCReader{target: call}).ListHandle(t.Context(), []byte("dir"))
	if err != nil || len(entries) != 2 || entries[0].FileName != " link " || entries[0].Attr.Attr.Type != gonfs.NF3Lnk || entries[1].Attr.IsSet {
		t.Fatalf("directory=%+v,%v", entries, err)
	}
}
func TestNFSHandleStreamEOFAndCancellation(t *testing.T) {
	protocol := &testNFSReadTarget{}
	stream := &nfsHandleStream{ctx: t.Context(), protocol: protocol, handle: []byte("bound"), offset: 6}
	if _, err := stream.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	stream.ctx = ctx
	before := len(protocol.readHandles)
	if _, err := stream.Read(make([]byte, 1)); !errors.Is(err, context.Canceled) || len(protocol.readHandles) != before {
		t.Fatal("canceled read performed I/O", err)
	}

}
