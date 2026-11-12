package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"reflect"
	"strconv"
	"sync"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
)

var errFileSourceWindow = errors.New("Develop file source observation expired or changed")
var errFileSourceComposition = errors.New("Develop file source consistency could not be verified")
var errFileSourceCleanupPending = errors.New("Develop file source I/O has not stopped")

// Only the System client supplies fresh eligibility; neither a scope digest nor
// a completed read substitutes for an observation. This interface permits exact
// protocol doubles in deterministic tests, not a second authorization route.
type fileSourceAuthority interface {
	CheckDevelopFileSourceAccess(context.Context, client.IssuedDevelopFileAuthorization, client.DevelopFileSourceAccessRequest) (*client.DevelopFileSourceAccessObservation, error)
}

type fileSourceProvider interface {
	plugin.ContentReadableProvider
	plugin.EngineCatalogFactsProvider
}

type fileSourceChunk struct {
	data []byte
	err  error
}
type fileSourceReadRequest struct {
	data   []byte
	result chan fileSourceChunk
}

type fileSourceReceipt struct {
	EngineVersion    int64
	Size             int64
	SHA256           string
	BusinessDeadline time.Time
	ReadSet          *plugin.QueryReadSet
}

type fileSourceReadState struct {
	Stopped        bool
	CleanupPending bool
}

// fileSourceReader is Owner-private. It never opens business content in Runtime
// and never retries Open/Read. One worker owns the actual handle; cancellation
// discards late bytes without claiming that an uninterruptible RPC has stopped.
type fileSourceReader struct {
	ctx        context.Context
	cancel     context.CancelCauseFunc
	stopBudget context.CancelFunc
	authority  fileSourceAuthority
	issued     client.IssuedDevelopFileAuthorization
	request    client.DevelopFileSourceAccessRequest
	provider   fileSourceProvider
	format     string
	requests   chan fileSourceReadRequest
	stopped    chan struct{}

	mu          sync.Mutex
	observation client.DevelopFileSourceAccessObservation
	readSet     *plugin.QueryReadSet
	sourceSize  int64
	closeErr    error

	refreshMu sync.Mutex
	readMu    sync.Mutex
	digest    hash.Hash
	size      int64
	eof       bool
}

func newFileSourceReader(ctx context.Context, authority fileSourceAuthority, provider fileSourceProvider,
	issued client.IssuedDevelopFileAuthorization, request client.DevelopFileSourceAccessRequest) (*fileSourceReader, error) {
	if ctx == nil || authority == nil || provider == nil || issued.Validate() != nil || request.ExecutionID != issued.Authorization.ExecutionID ||
		request.Attempt != issued.Scope.ExpectedAttempt || request.ScopeDigest != issued.ScopeDigest || request.LeaseToken == "" {
		return nil, errFileSourceWindow
	}
	frozen := *issued.Clone()
	found := false
	sourceFormat := ""
	for _, source := range frozen.Scope.Sources {
		if reflect.DeepEqual(source.Target, request.Source) {
			request.Source = source.Target
			sourceFormat = source.Format
			found = true
			break
		}
	}
	if !found {
		return nil, errFileSourceWindow
	}
	// Even the first authorization request cannot outlive the original EA.
	checkCtx, stopCheck := context.WithDeadline(ctx, frozen.Authorization.ExpiresAt)
	observation, err := authority.CheckDevelopFileSourceAccess(checkCtx, frozen, request)
	stopCheck()
	if err != nil {
		return nil, err
	}
	if !validFileSourceObservation(frozen, request, observation) || provider.Type() != observation.Engine.EngineType {
		return nil, errFileSourceWindow
	}
	budgetCtx, stopBudget := context.WithDeadline(ctx, observation.BusinessDeadline)
	readCtx, cancel := context.WithCancelCause(budgetCtx)
	s := &fileSourceReader{ctx: readCtx, cancel: cancel, stopBudget: stopBudget, authority: authority,
		issued: frozen, request: request, provider: provider, format: sourceFormat, requests: make(chan fileSourceReadRequest), stopped: make(chan struct{}),
		observation: *observation, digest: sha256.New()}
	go s.observe()
	go s.run()
	return s, nil
}

func validFileSourceObservation(issued client.IssuedDevelopFileAuthorization, request client.DevelopFileSourceAccessRequest, observation *client.DevelopFileSourceAccessObservation) bool {
	if observation == nil || observation.AuthorizationID != issued.Authorization.ID || observation.ExecutionID != request.ExecutionID ||
		observation.Attempt != request.Attempt || observation.ScopeDigest != request.ScopeDigest || !reflect.DeepEqual(observation.Source, request.Source) ||
		observation.ObservedAt.IsZero() || observation.ObservedAt.After(time.Now()) || !observation.ExpiresAt.After(time.Now()) ||
		!observation.ExpiresAt.After(observation.ObservedAt) || observation.BusinessDeadline.IsZero() ||
		observation.ExpiresAt.After(observation.BusinessDeadline) || observation.BusinessDeadline.After(issued.Authorization.ExpiresAt) || observation.Engine == nil {
		return false
	}
	engine := observation.Engine
	if engine.ID == 0 || engine.ID != request.Source.EngineID || (engine.EngineType != "nfs" && engine.EngineType != "minio") ||
		engine.TenantID == nil || strconv.FormatUint(uint64(*engine.TenantID), 10) != issued.Authorization.TenantID || engine.LifecycleState != commonModels.EngineLifecycleActive ||
		engine.DeletedAt != nil || len(engine.ConnectionInfo) == 0 {
		return false
	}
	for _, source := range issued.Scope.Sources {
		if reflect.DeepEqual(source.Target, request.Source) {
			return strconv.FormatInt(engine.Version, 10) == source.EngineVersion && ((source.Format == "geopackage" && engine.EngineType == "nfs") || (source.Format == "geojson" && engine.EngineType == "minio"))
		}
	}
	return false
}

func (s *fileSourceReader) activeLocked() error {
	if err := context.Cause(s.ctx); err != nil {
		return err
	}
	if !time.Now().Before(s.observation.ExpiresAt) {
		s.cancel(errFileSourceWindow)
		return errFileSourceWindow
	}
	return nil
}

func (s *fileSourceReader) active() error { s.mu.Lock(); defer s.mu.Unlock(); return s.activeLocked() }

func (s *fileSourceReader) observe() {
	for {
		s.mu.Lock()
		previous := s.observation
		err := s.activeLocked()
		s.mu.Unlock()
		if err != nil {
			return
		}
		// Renew halfway through the remaining observation. The floor prevents a
		// burst of observations when an unchanged lease approaches expiry.
		delay := time.Until(previous.ExpiresAt) / 2
		if delay < 100*time.Millisecond {
			delay = 100 * time.Millisecond
		}
		remaining := time.Until(previous.ExpiresAt)
		if delay > remaining {
			delay = remaining
		}
		timer := time.NewTimer(delay)
		select {
		case <-s.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if s.active() != nil {
			return
		}
		if s.refreshObservation() != nil {
			return
		}
	}
}

// Periodic renewal and the final post-read check share one current observation
// sequence. Neither can overwrite a newer response or reopen an expired window.
func (s *fileSourceReader) refreshObservation() error {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	s.mu.Lock()
	previous := s.observation
	err := s.activeLocked()
	s.mu.Unlock()
	if err != nil {
		return err
	}
	checkCtx, stop := context.WithDeadline(s.ctx, previous.ExpiresAt)
	expiry := time.AfterFunc(time.Until(previous.ExpiresAt), func() { s.cancel(errFileSourceWindow) })
	observation, err := s.authority.CheckDevelopFileSourceAccess(checkCtx, *s.issued.Clone(), s.request)
	expiry.Stop()
	stop()
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil && s.activeLocked() == nil && validFileSourceObservation(s.issued, s.request, observation) && observation.BusinessDeadline.Equal(previous.BusinessDeadline) && !observation.ObservedAt.Before(previous.ObservedAt) && reflect.DeepEqual(observation.Engine.ConnectionInfo, previous.Engine.ConnectionInfo) {
		s.observation = *observation
		return nil
	}
	s.cancel(errFileSourceWindow)
	return errFileSourceWindow
}

const maxWorkflowGeoJSONBytes = 8 << 20

func (s *fileSourceReader) observeContent(conn plugin.ConnectionInfo) (any, int64, error) {
	if s.format == "geopackage" {
		facts, err := format.ObserveGeoPackageSource(s.ctx, s.provider, conn, s.request.Source)
		if err != nil || facts == nil {
			return nil, 0, errFileSourceComposition
		}
		return facts, facts.SizeBytes, nil
	}
	facts, err := s.provider.DescribeEngineCatalogFacts(s.ctx, conn, s.request.Source, plugin.EngineCatalogFactsOptions{})
	if err != nil || facts == nil || facts.Storage == nil || facts.Storage.SizeBytes == nil || *facts.Storage.SizeBytes <= 0 || *facts.Storage.SizeBytes > maxWorkflowGeoJSONBytes || facts.Storage.ETag == "" || facts.UpdatedAt == nil {
		return nil, 0, errFileSourceComposition
	}
	return facts, *facts.Storage.SizeBytes, nil
}

func (s *fileSourceReader) run() {
	var reader io.ReadCloser
	var before any
	var sourceSize int64
	var eof bool
	var conn plugin.ConnectionInfo
	defer func() {
		var err error
		if reader != nil {
			err = reader.Close()
		}
		if eof && err == nil && s.active() == nil {
			if s.refreshObservation() == nil {
				after, _, factsErr := s.observeContent(conn)
				if factsErr != nil || after == nil || !reflect.DeepEqual(before, after) {
					s.cancel(errFileSourceComposition)
				}
			}
		}
		s.mu.Lock()
		s.closeErr = err
		s.mu.Unlock()
		close(s.stopped)
	}()
	if s.active() != nil {
		return
	}
	s.mu.Lock()
	conn = plugin.ConnectionInfo(s.observation.Engine.ConnectionInfo)
	s.mu.Unlock()
	var err error
	before, sourceSize, err = s.observeContent(conn)
	if err != nil || before == nil {
		s.cancel(errFileSourceComposition)
		return
	}
	if s.active() != nil {
		return
	}
	reader, err = s.provider.OpenContent(s.ctx, conn, s.request.Source, plugin.ReadOptions{})
	if err != nil || reader == nil {
		s.cancel(errFileSourceWindow)
		return
	}
	if s.active() != nil {
		return
	}
	evidence, ok := reader.(plugin.ReadSessionEvidence)
	if !ok {
		s.cancel(plugin.ErrQueryReadSetUnresolved)
		return
	}
	set, err := evidence.ReadSet(s.ctx)
	if err != nil || set == nil || len(set.Paths) != 1 || !reflect.DeepEqual(set.Paths[0], s.request.Source) {
		s.cancel(plugin.ErrQueryReadSetUnresolved)
		return
	}
	s.mu.Lock()
	s.readSet = set.Clone()
	s.sourceSize = sourceSize
	s.mu.Unlock()
	for {
		select {
		case <-s.ctx.Done():
			return
		case request := <-s.requests:
			if s.active() != nil {
				return
			}
			n, err := reader.Read(request.data)
			if n < 0 || n > len(request.data) {
				s.cancel(errFileSourceWindow)
				return
			}
			if s.active() != nil {
				return
			}
			eof = err == io.EOF
			request.result <- fileSourceChunk{data: request.data[:n], err: err}
			if err != nil {
				return
			}
		}
	}
}

func (s *fileSourceReader) Read(p []byte) (int, error) {
	s.readMu.Lock()
	defer s.readMu.Unlock()
	if err := s.active(); err != nil {
		return 0, err
	}
	if len(p) == 0 {
		return 0, nil
	}
	if s.eof {
		return 0, io.EOF
	}
	size := len(p)
	if size > 64*1024 {
		size = 64 * 1024
	}
	request := fileSourceReadRequest{data: make([]byte, size), result: make(chan fileSourceChunk, 1)}
	select {
	case <-s.ctx.Done():
		return 0, context.Cause(s.ctx)
	case <-s.stopped:
		return 0, io.ErrUnexpectedEOF
	case s.requests <- request:
	}
	select {
	case <-s.ctx.Done():
		return 0, context.Cause(s.ctx)
	case chunk := <-request.result:
		s.mu.Lock()
		defer s.mu.Unlock()
		if err := s.activeLocked(); err != nil {
			return 0, err
		}
		if chunk.err != nil && chunk.err != io.EOF {
			s.cancel(chunk.err)
			return 0, chunk.err
		}
		n := copy(p, chunk.data)
		_, _ = s.digest.Write(chunk.data)
		s.size += int64(n)
		if s.format == "geojson" && s.size > maxWorkflowGeoJSONBytes {
			s.cancel(errFileSourceComposition)
			return 0, errFileSourceComposition
		}
		s.eof = chunk.err == io.EOF
		return n, chunk.err
	}
}

// Receipt proves bytes actually returned through this guarded reader and a
// complete EOF. The caller must not hand private staging to Runtime without a
// current receipt; this is not a content snapshot or permission grant.
func (s *fileSourceReader) Receipt(ctx context.Context) (*fileSourceReceipt, error) {
	s.readMu.Lock()
	defer s.readMu.Unlock()
	if !s.eof {
		return nil, io.ErrUnexpectedEOF
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.ctx.Done():
		return nil, context.Cause(s.ctx)
	case <-s.stopped:
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.activeLocked(); err != nil {
		return nil, err
	}
	if s.closeErr != nil {
		return nil, s.closeErr
	}
	if s.size != s.sourceSize {
		return nil, errFileSourceComposition
	}
	return &fileSourceReceipt{EngineVersion: s.observation.Engine.Version, Size: s.size, SHA256: hex.EncodeToString(s.digest.Sum(nil)), BusinessDeadline: s.observation.BusinessDeadline, ReadSet: s.readSet.Clone()}, nil
}

func (s *fileSourceReader) State() fileSourceReadState {
	select {
	case <-s.stopped:
		s.mu.Lock()
		defer s.mu.Unlock()
		return fileSourceReadState{Stopped: true, CleanupPending: s.closeErr != nil}
	default:
		return fileSourceReadState{CleanupPending: s.ctx.Err() != nil}
	}
}

// Close requests cancellation, not proof of RPC termination. Callers retain the
// session while CleanupPending; late completion can only settle that session.
func (s *fileSourceReader) Close() error {
	s.cancel(context.Canceled)
	s.stopBudget()
	select {
	case <-s.stopped:
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.closeErr
	default:
		return errFileSourceCleanupPending
	}
}
