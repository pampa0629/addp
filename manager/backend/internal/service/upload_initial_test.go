package service

import (
	"context"
	"errors"
	"github.com/addp/common/authorization"
	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"io"
	"strings"
	"testing"
)

type evidenceUploadProvider struct {
	captureUploadPlugin
	disposition string
	fail        bool
	calls       int
}

func (p *evidenceUploadProvider) CreateContent(_ context.Context, _ plugin.ConnectionInfo, target plugin.EngineCatalogPath, _ plugin.WriteOptions) (io.WriteCloser, error) {
	p.calls++
	return &evidenceUploadWriter{target: target, kind: p.disposition, fail: p.fail}, nil
}

type evidenceUploadWriter struct {
	target       plugin.EngineCatalogPath
	kind         string
	fail, closed bool
	size         int64
}

func (w *evidenceUploadWriter) Write(b []byte) (int, error) {
	w.size += int64(len(b))
	return len(b), nil
}
func (w *evidenceUploadWriter) Close() error {
	w.closed = true
	if w.fail {
		return errors.New("commit failed")
	}
	return nil
}
func (w *evidenceUploadWriter) ContentWriteEvidence() (plugin.ContentWriteEvidence, error) {
	if !w.closed || w.fail {
		return plugin.ContentWriteEvidence{}, errors.New("no committed evidence")
	}
	return plugin.ContentWriteEvidence{Target: w.target, Disposition: w.kind, SizeBytes: w.size, Committed: true}, nil
}

type idleUploadIdentifier struct{}

func (idleUploadIdentifier) EnsureUploadBasicScan(context.Context, uint, string) (*client.MetaUploadIdentification, error) {
	return nil, errors.New("not run in write phase")
}
func TestNFSUploadPersistsOnlySameWriteEvidenceWithoutScanOrGrant(t *testing.T) {
	for _, kind := range []string{"created", "replaced", "commit failure", "missing identity"} {
		t.Run(kind, func(t *testing.T) {
			db := newModel3DGLBTaskServiceTestDB(t)
			if err := executiontest.EnsureSQLiteStore(db); err != nil {
				t.Fatal(err)
			}
			granted := 0
			svc := &UploadService{db: db, uploadIdentification: idleUploadIdentifier{}, issueUploadGrant: func(context.Context, uint, string, string) (*authorization.UploadInitialResult, error) {
				granted++
				return nil, nil
			}}
			provider := &evidenceUploadProvider{disposition: kind, fail: kind == "commit failure"}
			if provider.fail {
				provider.disposition = "created"
			}
			tenant := uint(7)
			engine := &commonmodels.Engine{ID: 9, EngineType: "nfs", TenantID: &tenant, Version: 2}
			req := &UploadRequest{TenantID: 7, TargetNodeLocator: "addp://engine/9/path/dir?type=directory", Files: []UploadFile{{FileName: "a.ifc", Reader: strings.NewReader("bytes")}}}
			ctx := execution.WithUserOrigin(t.Context(), authtest.NewTenantUserAuthContext("7", "31", []string{"manager.data_item.create"}))
			if kind == "missing identity" {
				ctx = t.Context()
			}
			loc, _ := resourcetree.ParseURI(req.TargetNodeLocator)
			result, err := svc.uploadNFS(ctx, req, loc, engine, provider, plugin.FileDirectoryPath(9, "dir"))
			if kind == "missing identity" {
				if err == nil || provider.calls != 0 {
					t.Fatal("wrote without verified user")
				}
				return
			}
			if err != nil || result.UploadID == "" || provider.calls != 1 || granted != 0 {
				t.Fatalf("result=%+v calls=%d grants=%d err=%v", result, provider.calls, granted, err)
			}
			receipt, readErr := svc.ReadUploadReceipt(t.Context(), 7, result.UploadID)
			if provider.fail {
				if readErr == nil || len(result.Files) != 0 || result.InitialStatus != "denied" {
					t.Fatal("failed commit became receipt")
				}
				return
			}
			if readErr != nil || receipt.Uploader.PrincipalID != 31 || len(receipt.Writes) != 1 || receipt.Writes[0].Disposition != kind || receipt.Writes[0].SizeBytes != 5 {
				t.Fatalf("receipt=%+v err=%v", receipt, readErr)
			}
			if _, err := svc.ReadUploadReceipt(t.Context(), 8, result.UploadID); err == nil {
				t.Fatal("cross tenant receipt")
			}
			if _, err := svc.GetOwnUpload(t.Context(), 7, 32, result.UploadID); err == nil {
				t.Fatal("other user saw upload")
			}
			if err := db.Model(&execution.TaskExecution{}).Where("execution_id=?", result.UploadID).Update("actor_principal_id", 32).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := svc.ReadUploadReceipt(t.Context(), 7, result.UploadID); err == nil {
				t.Fatal("actor drift trusted")
			}
		})
	}
}
