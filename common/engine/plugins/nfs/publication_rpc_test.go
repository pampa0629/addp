package nfs

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	gonfs "github.com/vmware/go-nfs-client/nfs"
	"github.com/vmware/go-nfs-client/nfs/rpc"
)

// Exercise the actual pinned rpc.Client, not a mock of Call: count TCP record
// payloads for the dependency's explicit-rejection retry and recv ambiguity.
func TestNFSLINKPinnedRPCDispatchCounts(t *testing.T) {
	for _, fault := range []string{"success", "garbage_then_success", "garbage_twice", "lost_response"} {
		t.Run(fault, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			type observation struct {
				packets [][]byte
				err     error
			}
			done := make(chan observation, 1)
			go func() {
				var got observation
				c, e := listener.Accept()
				if e != nil {
					got.err = e
					done <- got
					return
				}
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(3 * time.Second))
				for i := 0; i < 3; i++ {
					var length uint32
					if e = binary.Read(c, binary.BigEndian, &length); e != nil {
						if e != io.EOF {
							got.err = e
						}
						break
					}
					if length&0x80000000 == 0 || length&0x7fffffff > 4096 {
						got.err = io.ErrShortBuffer
						break
					}
					packet := make([]byte, length&0x7fffffff)
					if _, e = io.ReadFull(c, packet); e != nil {
						got.err = e
						break
					}
					got.packets = append(got.packets, packet)
					if fault == "lost_response" {
						break
					}
					status := uint32(rpc.Success)
					if fault == "garbage_twice" || (fault == "garbage_then_success" && i == 0) {
						status = rpc.GarbageArgs
					}
					var response bytes.Buffer
					for _, v := range []uint32{binary.BigEndian.Uint32(packet[:4]), 1, rpc.MsgAccepted, 0, 0, status} {
						_ = binary.Write(&response, binary.BigEndian, v)
					}
					if status == rpc.Success {
						response.Write(nfsResponse(t, uint32(0), gonfs.PostOpAttr{}, gonfs.WccData{}))
					}
					var wire bytes.Buffer
					_ = binary.Write(&wire, binary.BigEndian, uint32(response.Len())|0x80000000)
					wire.Write(response.Bytes())
					if _, e = c.Write(wire.Bytes()); e != nil {
						got.err = e
						break
					}
				}
				done <- got
			}()
			client, err := rpc.DialTCP("tcp", nil, listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			client.SetTimeout(3 * time.Second)
			r := &nfsRPCReader{target: client, auth: rpc.AuthNull}
			err = r.LinkHandle([]byte("prepared-file"), []byte("bound-parent"), "difference.gpkg")
			_ = client.Close()
			got := <-done
			if got.err != nil {
				t.Fatal(got.err)
			}
			want := 1
			if fault == "garbage_then_success" || fault == "garbage_twice" {
				want = 2
			}
			if len(got.packets) != want {
				t.Fatalf("%s issued %d records, want %d", fault, len(got.packets), want)
			}
			if want == 2 && !bytes.Equal(got.packets[0], got.packets[1]) {
				t.Fatal("explicit rejection retry changed XID or payload")
			}
			if (fault == "lost_response" || fault == "garbage_twice") != (err != nil) {
				t.Fatal("unexpected RPC result", err)
			}
		})
	}
}
