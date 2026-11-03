package nfs

import (
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"sync"
	"testing"

	"github.com/addp/common/engine/plugin"
	gonfs "github.com/vmware/go-nfs-client/nfs"
)

type evidenceWriteProtocol struct {
	testNFSReadTarget
	existing                          bool
	collided                          bool
	removed, created, commits, writes int
	failure                           error
}

func (p *evidenceWriteProtocol) LookupAt(parent []byte, name string) (*gonfs.Fattr, []byte, error) {
	if name == "new.ifc" {
		if p.existing {
			return &gonfs.Fattr{Type: gonfs.NF3Reg}, []byte("old"), nil
		}
		return nil, nil, os.ErrNotExist
	}
	return p.testNFSReadTarget.LookupAt(parent, name)
}
func (p *evidenceWriteProtocol) CreateGuarded([]byte, string) ([]byte, error) {
	p.created++
	if p.collided {
		return nil, os.ErrExist
	}
	return []byte("created-handle"), nil
}
func (p *evidenceWriteProtocol) RemoveAt([]byte, string) error { p.removed++; return nil }
func (p *evidenceWriteProtocol) WriteHandle(fh []byte, offset uint64, data []byte) (int, error) {
	p.writes++
	if string(fh) != "created-handle" {
		panic("changed write handle")
	}
	return len(data), p.failure
}
func (p *evidenceWriteProtocol) CommitHandle(fh []byte) error {
	p.commits++
	if string(fh) != "created-handle" {
		panic("changed commit handle")
	}
	return p.failure
}

func TestNFSContentCommitEvidenceRequiresRealCreateWriteAndCommit(t *testing.T) {
	for _, kind := range []string{"created", "replaced", "collision", "write_failure", "commit_failure", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			p := &evidenceWriteProtocol{testNFSReadTarget: testNFSReadTarget{types: map[string]uint32{"data": gonfs.NF3Dir}}, existing: kind == "replaced", collided: kind == "collision"}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			writer, err := openCommittedNFSWriter(ctx, p, &sync.Mutex{}, "data/new.ifc", true)
			if kind == "collision" {
				if !errors.Is(err, os.ErrExist) || p.removed != 0 || p.writes != 0 {
					t.Fatal("collision overwrote another file", err, p)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			writer.target = plugin.FileItemPath(9, "data/new.ifc")
			if _, err := writer.ContentWriteEvidence(); err == nil {
				t.Fatal("uncommitted writer supplied evidence")
			}
			failure := errors.New("disconnected")
			if kind == "write_failure" {
				p.failure = failure
			}
			_, writeErr := writer.Write([]byte("IFC DATA"))
			if kind == "commit_failure" {
				p.failure = failure
			}
			if kind == "cancelled" {
				cancel()
			}
			closeErr := writer.Close()
			result, evidenceErr := writer.ContentWriteEvidence()
			if kind == "created" || kind == "replaced" {
				if writeErr != nil || closeErr != nil || evidenceErr != nil || result.Validate(writer.target) != nil || result.Disposition != kind || result.SizeBytes != 8 || p.commits != 1 {
					t.Fatalf("result=%+v write=%v close=%v evidence=%v protocol=%+v", result, writeErr, closeErr, evidenceErr, p)
				}
				if err := writer.Close(); err != nil || p.commits != 1 {
					t.Fatal("close recommitted")
				}
				if _, err := writer.Write(nil); !errors.Is(err, io.ErrClosedPipe) {
					t.Fatal("closed writer accepted write")
				}
			} else if closeErr == nil || evidenceErr == nil {
				t.Fatal("failed or cancelled write produced success", result, closeErr, evidenceErr)
			}
		})
	}
}

func TestNFSGuardedCreateAndStableWriteWireContract(t *testing.T) {
	for _, fault := range []string{"none", "zero_progress", "oversize", "unstable", "truncated", "collision"} {
		t.Run(fault, func(t *testing.T) {
			create := nfsResponse(t, uint32(0), gonfs.PostOpFH3{IsSet: true, FH: []byte("created")}, gonfs.PostOpAttr{}, gonfs.WccData{})
			count, how := uint32(3), uint32(2)
			switch fault {
			case "zero_progress":
				count = 0
			case "oversize":
				count = 4
			case "unstable":
				how = 0
			case "collision":
				create = nfsResponse(t, uint32(17))
			}
			write := nfsResponse(t, uint32(0), gonfs.WccData{}, count, how, uint64(9))
			if fault == "truncated" {
				write = write[:len(write)-1]
			}
			call := &fixtureNFSCALL{t: t, replies: [][]byte{create, write, nfsResponse(t, uint32(0), gonfs.WccData{}, uint64(9))}}
			call.request = func(args any) {
				value := reflect.ValueOf(args).Elem()
				switch value.FieldByName("Proc").Uint() {
				case gonfs.NFSProc3Create:
					if value.FieldByName("How").FieldByName("Mode").Uint() != 1 {
						t.Fatal("creation was not atomic GUARDED")
					}
				case gonfs.NFSProc3Write:
					if value.FieldByName("How").Uint() != 2 || value.FieldByName("Count").Uint() != 3 || string(value.FieldByName("FH").Bytes()) != "created" {
						t.Fatal("changed stable write contract")
					}
				}
			}
			protocol := &nfsRPCReader{target: call, maxWrite: 3}
			fh, err := protocol.CreateGuarded([]byte("parent"), "new.ifc")
			if fault == "collision" {
				if !errors.Is(err, os.ErrExist) || call.calls != 1 {
					t.Fatal("collision was retried", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			n, err := protocol.WriteHandle(fh, 0, []byte("data"))
			if fault != "none" {
				if err == nil || n != 0 {
					t.Fatal("invalid stable write accepted", n, err)
				}
				return
			}
			if err != nil || n != 3 {
				t.Fatal(n, err)
			}
			if err := protocol.CommitHandle(fh); err != nil || call.calls != 3 {
				t.Fatal("commit missing", err)
			}
		})
	}
}
