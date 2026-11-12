package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"

	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/common/format/plugins/sqlite"
	"github.com/addp/develop/backend/internal/repository"
	"github.com/google/uuid"
)

var errFileWorkflowInputs = errors.New("invalid or incomplete private workflow inputs")

// Recording must durably bind the original Runtime UUID and complete manifest
// to the same Owner execution before any remote mutation. A lost acknowledgement
// is not permission to repeat registration or create a second Runtime UUID.
type fileWorkflowRuntimeRecorder func(context.Context, repository.PrivateFileRuntimeRecord) error

type fileWorkflowInputCleanup struct {
	observed             bool
	SourcesStopped       bool
	RuntimeStopped       bool
	SourceCleanupPending bool
	RuntimeCleanup       string
	LocalRemoved         bool
}

type fileWorkflowInputs struct {
	ctx         context.Context
	cancel      context.CancelCauseFunc
	directory   string
	instanceID  string
	runtimeID   string
	sources     []*fileSourceReader
	manifest    plugin.WorkflowPrivateFileManifest
	sourceReads []repository.PrivateFileSourceRead

	prepared        bool
	attempted       bool
	uploaded        bool
	dispatched      bool
	runtime         *plugin.WorkflowPrivateFilesClient
	outputAttempted bool
}

// One Owner attempt calls the batch methods sequentially; only the source
// observation workers run concurrently.
// Preparation is a component of the file-purpose Owner executor, not a public
// submission route or a replacement for System admission. On error a non-nil
// batch retains its original sessions so callers can observe pending cleanup.
func prepareFileWorkflowInputs(ctx context.Context, instanceID string, authority fileSourceAuthority, provider fileSourceProvider,
	facts *client.DevelopFileExecutionFacts, issued client.IssuedDevelopFileAuthorization, lease execution.Lease, registerDirectory func(context.Context, string, string) error) (*fileWorkflowInputs, error) {
	if ctx == nil || instanceID == "" || registerDirectory == nil || authority == nil || provider == nil || facts.Validate() != nil || issued.Validate() != nil || facts.AdmissionState != "ready" ||
		facts.ExecutionID != issued.Authorization.ExecutionID || facts.AuthorizationID != issued.Authorization.ID || facts.ScopeDigest != issued.ScopeDigest ||
		!reflect.DeepEqual(facts.BoundScope, &issued.Scope) || facts.TenantID != issued.Authorization.TenantID ||
		facts.Actor.PrincipalID != issued.Authorization.ActorPrincipalID || facts.Actor.TenantMembershipID != issued.Authorization.TenantMembershipID ||
		facts.Actor.AuthorizationVersion != issued.Authorization.IssuedAuthorizationVersion || lease.ExecutionID != facts.ExecutionID || lease.Attempt != facts.Intent.ExpectedAttempt ||
		strconv.Itoa(lease.TenantID) != facts.TenantID || lease.Owner == "" {
		return nil, errFileWorkflowInputs
	}
	token, err := uuid.Parse(lease.Token)
	if err != nil || token == uuid.Nil || token.String() != lease.Token {
		return nil, errFileWorkflowInputs
	}
	facts = facts.Clone()
	issued = *issued.Clone()
	workflow, err := fileWorkflowRuntimeDefinition(facts)
	if err != nil {
		return nil, err
	}
	tenant, _ := strconv.ParseUint(facts.TenantID, 10, 64)
	authorization, _ := strconv.ParseInt(facts.AuthorizationID, 10, 64)
	root, err := os.MkdirTemp("", "addp-develop-inputs-")
	if err != nil {
		return nil, errFileWorkflowInputs
	}
	batchCtx, cancel := context.WithCancelCause(ctx)
	b := &fileWorkflowInputs{ctx: batchCtx, cancel: cancel, directory: root, instanceID: instanceID, manifest: plugin.WorkflowPrivateFileManifest{
		SchemaVersion: plugin.WorkflowPrivateFilesAPIV1, TenantID: uint(tenant), ExecutionID: facts.ExecutionID, Attempt: lease.Attempt,
		AuthorizationID: authorization, ScopeDigest: facts.ScopeDigest, WorkflowJSON: workflow, Inputs: []plugin.WorkflowPrivateFile{}, Outputs: []string{}}}
	if facts.Intent.Target != nil {
		b.manifest.Outputs = append(b.manifest.Outputs, facts.Intent.Target.NodeID)
	}
	if err := registerDirectory(b.ctx, instanceID, root); err != nil {
		b.cancel(err)
		return b, err
	}
	for index, source := range facts.Intent.Sources {
		req := client.DevelopFileSourceAccessRequest{ExecutionID: facts.ExecutionID, Attempt: lease.Attempt, LeaseToken: lease.Token, ScopeDigest: facts.ScopeDigest, Source: source.Target}
		reader, err := newFileSourceReader(b.ctx, authority, provider, issued, req)
		if err != nil {
			b.cancel(err)
			return b, err
		}
		b.sources = append(b.sources, reader)
		go func() { <-reader.ctx.Done(); b.cancel(context.Cause(reader.ctx)) }()
		target, err := os.OpenFile(b.inputPath(index), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			b.cancel(errFileWorkflowInputs)
			return b, errFileWorkflowInputs
		}
		_, readErr := io.Copy(target, reader)
		syncErr := target.Sync()
		info, statErr := target.Stat()
		closeErr := target.Close()
		if err = errors.Join(readErr, syncErr, statErr, closeErr); err != nil {
			b.cancel(err)
			return b, err
		}
		receipt, err := reader.Receipt(b.ctx)
		if err != nil || receipt.Size != info.Size() {
			b.cancel(errFileWorkflowInputs)
			return b, errFileWorkflowInputs
		}
		if err := sqlite.ValidateStandaloneGeoPackage(reader.ctx, b.inputPath(index)); err != nil {
			b.cancel(errFileSourceComposition)
			return b, errFileSourceComposition
		}
		if err := reader.active(); err != nil {
			b.cancel(err)
			return b, err
		}
		if index == 0 {
			b.manifest.Deadline = receipt.BusinessDeadline
		} else if !b.manifest.Deadline.Equal(receipt.BusinessDeadline) {
			b.cancel(errFileWorkflowInputs)
			return b, errFileWorkflowInputs
		}
		b.manifest.Inputs = append(b.manifest.Inputs, plugin.WorkflowPrivateFile{NodeID: source.NodeID, Size: receipt.Size, SHA256: receipt.SHA256})
		b.sourceReads = append(b.sourceReads, repository.PrivateFileSourceRead{NodeID: source.NodeID, ReadSet: receipt.ReadSet.Clone(), EngineVersion: receipt.EngineVersion})
	}
	b.prepared = true
	if err := b.live(); err != nil {
		return b, err
	}
	return b, nil
}

func (b *fileWorkflowInputs) inputPath(index int) string {
	return filepath.Join(b.directory, fmt.Sprintf("input-%d.gpkg", index))
}

func (b *fileWorkflowInputs) live() error {
	if err := context.Cause(b.ctx); err != nil {
		return err
	}
	if !b.prepared {
		return errFileWorkflowInputs
	}
	for _, reader := range b.sources {
		if err := reader.active(); err != nil {
			b.cancel(err)
			return err
		}
	}
	return nil
}

func (b *fileWorkflowInputs) bounded(ctx context.Context) (context.Context, func()) {
	callCtx, cancel := context.WithCancelCause(ctx)
	stop := context.AfterFunc(b.ctx, func() { cancel(context.Cause(b.ctx)) })
	bounded, stopBudget := context.WithDeadline(callCtx, b.manifest.Deadline)
	return bounded, func() { stop(); stopBudget(); cancel(context.Canceled) }
}

func cloneFileWorkflowManifest(m plugin.WorkflowPrivateFileManifest) plugin.WorkflowPrivateFileManifest {
	m.Inputs = append([]plugin.WorkflowPrivateFile(nil), m.Inputs...)
	m.Outputs = append([]string{}, m.Outputs...)
	return m
}

type fileWorkflowUploadReader struct {
	batch  *fileWorkflowInputs
	source io.Reader
}

func (r fileWorkflowUploadReader) Read(p []byte) (int, error) {
	if err := r.batch.live(); err != nil {
		return 0, err
	}
	// A caller-owned buffer is never exposed to a background file read.
	n, err := r.source.Read(p)
	if activeErr := r.batch.live(); activeErr != nil {
		return 0, activeErr
	}
	return n, err
}

// Upload registers the full immutable request before one Create and one Upload
// per ordinal. Attempted remains true after any unknown outcome, including a
// lost registration acknowledgement; it is never an implicit retry controller.
func (b *fileWorkflowInputs) Upload(ctx context.Context, connection plugin.ConnectionInfo, runtimeID string, credential plugin.WorkflowRuntimeCredential, record fileWorkflowRuntimeRecorder) error {
	if record == nil || b.attempted {
		return errFileWorkflowInputs
	}
	if err := b.live(); err != nil {
		return err
	}
	exchange, err := plugin.NewWorkflowPrivateFilesClient(connection, runtimeID, cloneFileWorkflowManifest(b.manifest), credential)
	if err != nil {
		return err
	}
	callCtx, stop := b.bounded(ctx)
	defer stop()
	b.attempted = true
	b.runtimeID = runtimeID
	if err := record(callCtx, repository.PrivateFileRuntimeRecord{RuntimeExecutionID: runtimeID, Manifest: cloneFileWorkflowManifest(b.manifest), OwnerInstanceID: b.instanceID, InputDirectory: b.directory, SourceReads: b.sourceReads}); err != nil {
		return err
	}
	if err := b.live(); err != nil {
		return err
	}
	if err := callCtx.Err(); err != nil {
		return err
	}
	b.runtime = exchange // Retain this exact identity even when Create loses its reply.
	status, err := exchange.Create(callCtx)
	if err != nil {
		return err
	}
	for _, state := range status.Inputs {
		if state != "missing" {
			return errFileWorkflowInputs
		}
	}
	for index := range b.manifest.Inputs {
		if err := b.live(); err != nil {
			return err
		}
		source, err := os.Open(b.inputPath(index))
		if err != nil {
			return errFileWorkflowInputs
		}
		uploadErr := exchange.Upload(callCtx, index, fileWorkflowUploadReader{batch: b, source: source})
		closeErr := source.Close()
		if err = errors.Join(uploadErr, closeErr); err != nil {
			return err
		}
	}
	if err := b.live(); err != nil {
		return err
	}
	b.uploaded = true
	return nil
}

// Computation uses the existing canonical workflow endpoint only once. This
// does not publish a business output or issue an initial content grant.
func (b *fileWorkflowInputs) Execute(ctx context.Context, language string) (*plugin.WorkflowExecuteResult, error) {
	if !b.uploaded || b.dispatched || b.runtime == nil {
		return nil, errFileWorkflowInputs
	}
	if err := b.live(); err != nil {
		return nil, err
	}
	callCtx, stop := b.bounded(ctx)
	defer stop()
	b.dispatched = true
	return b.runtime.Execute(callCtx, language)
}

// Cleanup is recovery on the same private session; it never replays creation,
// uploads or computation. Pending source RPCs and unknown Runtime cleanup must
// remain visible even after the initiating request has returned.
func (b *fileWorkflowInputs) Cleanup(ctx context.Context) (fileWorkflowInputCleanup, error) {
	b.cancel(context.Canceled)
	state := fileWorkflowInputCleanup{observed: true, SourcesStopped: true, RuntimeStopped: true, RuntimeCleanup: "not_created"}
	for _, reader := range b.sources {
		_ = reader.Close()
		source := reader.State()
		if !source.Stopped {
			state.SourcesStopped = false
		}
		state.SourceCleanupPending = state.SourceCleanupPending || source.CleanupPending
	}
	var err error
	if b.runtime != nil {
		state.RuntimeCleanup = "pending"
		state.RuntimeStopped = false
		result, cleanupErr := b.runtime.Cleanup(ctx)
		if cleanupErr == nil {
			state.RuntimeCleanup = result.Cleanup
			// This fixed Runtime receipt is emitted only after the worker and all
			// transfers stopped; only removal of its private directory failed.
			state.RuntimeStopped = result.Cleanup == "passed" || result.ErrorCode == "PRIVATE_FILES_CLEANUP_FAILED"
		} else {
			err = cleanupErr
		}
	}
	if !state.SourcesStopped || state.SourceCleanupPending || state.RuntimeCleanup == "pending" {
		return state, errors.Join(err, errFileSourceCleanupPending)
	}
	if os.RemoveAll(b.directory) != nil {
		return state, errFileSourceCleanupPending
	}
	state.LocalRemoved = true
	return state, nil
}

// A persisted path is not deletion authority. Recovery must still hold the
// original local batch/source handles and match the existing supervisor's
// instance identity. A new process cannot reconstruct ownership from a string.
func (b *fileWorkflowInputs) CleanupRecorded(ctx context.Context, currentInstanceID string, record repository.PrivateFileRuntimeRecord) (fileWorkflowInputCleanup, error) {
	if currentInstanceID != b.instanceID || record.OwnerInstanceID != b.instanceID || record.InputDirectory != b.directory || record.RuntimeExecutionID != b.runtimeID ||
		record.Manifest.ExecutionID != b.manifest.ExecutionID || record.Manifest.Attempt != b.manifest.Attempt || record.Manifest.TenantID != b.manifest.TenantID ||
		record.Manifest.AuthorizationID != b.manifest.AuthorizationID || record.Manifest.ScopeDigest != b.manifest.ScopeDigest {
		return fileWorkflowInputCleanup{RuntimeCleanup: "pending"}, errFileSourceCleanupPending
	}
	return b.Cleanup(ctx)
}

// Directory-only recovery never invents a Runtime request. Once a Runtime
// identity was reserved, the full original Runtime record is required instead.
func (b *fileWorkflowInputs) CleanupInputDirectory(ctx context.Context, currentInstanceID string, record repository.PrivateInputDirectoryRecord) (fileWorkflowInputCleanup, error) {
	if b.runtimeID != "" || currentInstanceID != b.instanceID || record.OwnerInstanceID != b.instanceID || record.InputDirectory != b.directory ||
		record.ExecutionID != b.manifest.ExecutionID || record.Attempt != b.manifest.Attempt || record.TenantID != b.manifest.TenantID || record.AuthorizationID != b.manifest.AuthorizationID || record.ScopeDigest != b.manifest.ScopeDigest {
		return fileWorkflowInputCleanup{RuntimeCleanup: "pending"}, errFileSourceCleanupPending
	}
	return b.Cleanup(ctx)
}
