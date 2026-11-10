package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/develop/backend/internal/repository"
)

type publicationAuthorityFunc func(context.Context, client.IssuedDevelopFileAuthorization, client.DevelopFilePublicationRequest) (*client.DevelopFilePublicationObservation, error)

func (f publicationAuthorityFunc) AuthorizeDevelopFilePublication(ctx context.Context, issued client.IssuedDevelopFileAuthorization, req client.DevelopFilePublicationRequest) (*client.DevelopFilePublicationObservation, error) {
	return f(ctx, issued, req)
}

type publicationProviderFixture struct {
	*sourceWindowProvider
	prepare func(context.Context, plugin.ConnectionInfo, plugin.NewFilePublicationRequest, io.Reader) (plugin.PreparedNewFile, error)
	publish func(context.Context, plugin.ConnectionInfo, plugin.PreparedNewFile, plugin.NewFilePublicationState) (plugin.NewFilePublicationOutcome, error)
	resolve func(context.Context, plugin.ConnectionInfo, plugin.PreparedNewFile) (plugin.NewFilePublicationOutcome, error)
	cleanup func(context.Context, plugin.ConnectionInfo, plugin.PreparedNewFile, plugin.NewFilePublicationState) error
}

func (p *publicationProviderFixture) PrepareNewFile(ctx context.Context, conn plugin.ConnectionInfo, request plugin.NewFilePublicationRequest, body io.Reader) (plugin.PreparedNewFile, error) {
	return p.prepare(ctx, conn, request, body)
}

func (p *publicationProviderFixture) PublishNewFile(ctx context.Context, conn plugin.ConnectionInfo, prepared plugin.PreparedNewFile, previous plugin.NewFilePublicationState) (plugin.NewFilePublicationOutcome, error) {
	return p.publish(ctx, conn, prepared, previous)
}

func (p *publicationProviderFixture) ResolveNewFile(ctx context.Context, conn plugin.ConnectionInfo, prepared plugin.PreparedNewFile) (plugin.NewFilePublicationOutcome, error) {
	return p.resolve(ctx, conn, prepared)
}

func (p *publicationProviderFixture) CleanupNewFile(ctx context.Context, conn plugin.ConnectionInfo, prepared plugin.PreparedNewFile, state plugin.NewFilePublicationState) error {
	return p.cleanup(ctx, conn, prepared, state)
}

func TestPrivateFilePublicationProviderSequence(t *testing.T) {
	for _, tc := range []struct {
		name, calls               string
		state                     plugin.NewFilePublicationState
		cleanupPending, wantError bool
	}{
		{"published", "admit prepare release publish cleanup", plugin.NewFilePublished, false, false},
		{"already exists", "admit prepare", plugin.NewFileNotPublished, false, true},
		{"unknown prepare", "admit prepare", plugin.NewFileNotPublished, false, true},
		{"release response lost", "admit prepare release", plugin.NewFileNotPublished, false, true},
		{"concurrent target creation", "admit prepare release publish cleanup", plugin.NewFileNotPublished, false, true},
		{"link response lost and resolved", "admit prepare release publish resolve cleanup", plugin.NewFilePublished, false, false},
		{"link remains unknown", "admit prepare release publish resolve", plugin.NewFileUnknown, false, true},
		{"cleanup pending", "admit prepare release publish cleanup", plugin.NewFilePublished, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, repo, item, lease, facts, issued, _ := privateFileWorkerFixture(t, true)
			ctx := t.Context()
			directory := t.TempDir()
			if err := repo.RecordPrivateInputDirectory(ctx, *lease, "fixture-instance", directory); err != nil {
				t.Fatal(err)
			}
			target := facts.Intent.Target
			authorizationID, err := strconv.ParseInt(issued.Authorization.ID, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			manifest := plugin.WorkflowPrivateFileManifest{ExecutionID: item.ExecutionID, TenantID: 3,
				Attempt: lease.Attempt, AuthorizationID: authorizationID, ScopeDigest: issued.ScopeDigest,
				Outputs: []string{target.NodeID}}
			if err := repo.RecordPrivateRuntime(ctx, *lease, repository.PrivateFileRuntimeRecord{
				RuntimeExecutionID: fileInputRuntimeID, Manifest: manifest, OwnerInstanceID: "fixture-instance", InputDirectory: directory,
			}); err != nil {
				t.Fatal(err)
			}
			const payload = "private Runtime output bytes"
			sum := sha256.Sum256([]byte(payload))
			output := plugin.WorkflowPrivateFile{NodeID: target.NodeID, Size: int64(len(payload)), SHA256: hex.EncodeToString(sum[:])}
			dropped := errors.New("test lost response")
			var calls []string
			var original plugin.PreparedNewFile
			connection := plugin.ConnectionInfo{"server": "only from original admission"}
			assertOriginal := func(got plugin.ConnectionInfo, value plugin.PreparedNewFile) {
				t.Helper()
				if !reflect.DeepEqual(got, connection) || !reflect.DeepEqual(value, original) {
					t.Fatal("provider connection or original preparation changed")
				}
			}
			published := func() plugin.NewFilePublicationOutcome {
				return plugin.NewFilePublicationOutcome{State: plugin.NewFilePublished, CleanupPending: true,
					Evidence: &plugin.ContentWriteEvidence{Target: target.Target, Disposition: "created", SizeBytes: output.Size, Committed: true}}
			}
			provider := &publicationProviderFixture{sourceWindowProvider: &sourceWindowProvider{}}
			provider.prepare = func(_ context.Context, c plugin.ConnectionInfo, request plugin.NewFilePublicationRequest, reader io.Reader) (plugin.PreparedNewFile, error) {
				calls = append(calls, "prepare")
				value, err := io.ReadAll(reader)
				if err != nil || string(value) != payload || !reflect.DeepEqual(c, connection) ||
					!reflect.DeepEqual(request.Target, target.Target) || request.SHA256 != output.SHA256 || request.SizeBytes != output.Size {
					t.Fatal("prepare changed output bytes or target", err)
				}
				original = plugin.PreparedNewFile{NewFilePublicationRequest: request, ProviderState: []byte("original private Provider receipt")}
				switch tc.name {
				case "already exists":
					return plugin.PreparedNewFile{}, os.ErrExist
				case "unknown prepare":
					return original, dropped
				default:
					return original, nil
				}
			}
			provider.publish = func(_ context.Context, c plugin.ConnectionInfo, value plugin.PreparedNewFile, previous plugin.NewFilePublicationState) (plugin.NewFilePublicationOutcome, error) {
				calls = append(calls, "publish")
				assertOriginal(c, value)
				durable, err := repo.ReadPrivatePublication(ctx, 3, item.ExecutionID)
				if err != nil || durable.DispatchState != plugin.NewFileUnknown || previous != plugin.NewFileNotPublished {
					t.Fatal("publish did not follow one durable dispatch", err)
				}
				switch tc.name {
				case "concurrent target creation":
					return plugin.NewFilePublicationOutcome{State: plugin.NewFileNotPublished, CleanupPending: true}, os.ErrExist
				case "link response lost and resolved", "link remains unknown":
					return plugin.NewFilePublicationOutcome{State: plugin.NewFileUnknown, CleanupPending: true}, dropped
				default:
					return published(), nil
				}
			}
			provider.resolve = func(_ context.Context, c plugin.ConnectionInfo, value plugin.PreparedNewFile) (plugin.NewFilePublicationOutcome, error) {
				calls = append(calls, "resolve")
				assertOriginal(c, value)
				if tc.name == "link remains unknown" {
					return plugin.NewFilePublicationOutcome{State: plugin.NewFileUnknown, CleanupPending: true}, dropped
				}
				return published(), nil
			}
			provider.cleanup = func(_ context.Context, c plugin.ConnectionInfo, value plugin.PreparedNewFile, state plugin.NewFilePublicationState) error {
				calls = append(calls, "cleanup")
				assertOriginal(c, value)
				durable, err := repo.ReadPrivatePublication(ctx, 3, item.ExecutionID)
				if err != nil || state != tc.state || durable.Outcome == nil || durable.Outcome.State != state || !durable.Outcome.CleanupPending {
					t.Fatal("cleanup lacks original durable Provider evidence", err)
				}
				if tc.cleanupPending {
					return dropped
				}
				return nil
			}
			f := &privateFileWorkflowExecutor{repository: repo, provider: provider}
			business := time.Now().Add(30 * time.Second)
			f.publication = func(tenant uint) filePublicationAuthority {
				if tenant != 3 {
					t.Fatal("wrong tenant for publication")
				}
				return publicationAuthorityFunc(func(_ context.Context, ea client.IssuedDevelopFileAuthorization, r client.DevelopFilePublicationRequest) (*client.DevelopFilePublicationObservation, error) {
					record, err := repo.ReadPrivatePublication(ctx, 3, item.ExecutionID)
					if err != nil || r.Validate() != nil || ea.Authorization.ID != issued.Authorization.ID || r.LeaseToken != lease.Token ||
						record.Prepared.OperationID != r.OperationID || record.ApprovalID != r.ApprovalID {
						t.Fatal("System called without original operation and lease", err)
					}
					now := time.Now()
					result := &client.DevelopFilePublicationObservation{AuthorizationID: ea.Authorization.ID, ExecutionID: r.ExecutionID,
						Attempt: r.Attempt, ScopeDigest: r.ScopeDigest, ApprovalID: r.ApprovalID, OperationID: r.OperationID, Stage: r.Stage,
						PreparationDigest: r.PreparationDigest, ObservedAt: now, ExpiresAt: now.Add(15 * time.Second), BusinessDeadline: business}
					if r.Stage == client.DevelopFilePublicationPrepare {
						calls = append(calls, "admit")
						result.Engine = &commonmodels.Engine{ConnectionInfo: commonmodels.ConnectionInfo(connection)}
					} else {
						calls = append(calls, "release")
						if record.PreparationDigest == "" || r.PreparationDigest != record.PreparationDigest {
							t.Fatal("release requested before recording exact preparation")
						}
						if tc.name == "release response lost" {
							return nil, dropped
						}
					}
					return result, nil
				})
			}
			_, err = f.publishPrivateFile(ctx, *lease, fileInputRuntimeID, output, strings.NewReader(payload))
			if (err != nil) != tc.wantError || strings.Join(calls, " ") != tc.calls {
				t.Fatal(calls, err)
			}
			record, err := repository.NewFileExecutionRepository(db).ReadPrivatePublication(ctx, 3, item.ExecutionID)
			if err != nil || record.DispatchState != tc.state || (record.Outcome != nil && record.Outcome.CleanupPending != tc.cleanupPending) {
				t.Fatal("unexpected durable outcome", err)
			}
			if tc.name == "unknown prepare" && (record.PreparationDigest != "" || !reflect.DeepEqual(record.Prepared, original)) {
				t.Fatal("uncertain preparation was lost or promoted")
			}
			if _, err := f.publishPrivateFile(ctx, *lease, fileInputRuntimeID, output, strings.NewReader(payload)); err == nil || strings.Join(calls, " ") != tc.calls {
				t.Fatal("second call replayed the original operation")
			}
		})
	}
}
