package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"sync"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/google/uuid"
)

const NotebookSessionCookieName = "addp_notebook_session"
const NotebookCopilotSessionCookieName = "addp_notebook_copilot_session"
const NotebookKernelCapabilityPrefix = "addp_nkc_"

var (
	ErrNotebookSessionNotFound      = errors.New("notebook session not found")
	ErrNotebookSessionConflict      = errors.New("notebook already has an active interactive session")
	ErrNotebookTableScanInvalid     = errors.New("notebook table scan request is invalid")
	ErrNotebookTableScanUnsupported = errors.New("notebook table scan is unsupported by this engine")
	ErrNotebookQueryInvalid         = errors.New("notebook query request is invalid")
	ErrNotebookQueryUnsupported     = errors.New("notebook query is unsupported by this engine")
)

const (
	notebookExecutionLeaseCheckInterval = time.Minute
	notebookReadSessionCloseTimeout     = 15 * time.Second
)

type NotebookSession struct {
	ID                     string    `json:"id"`
	TaskID                 uint      `json:"task_id"`
	URL                    string    `json:"url"`
	ExpiresAt              time.Time `json:"expires_at"`
	TenantID               uint      `json:"-"`
	UserID                 uint      `json:"-"`
	EngineID               uint      `json:"-"`
	Endpoint               string    `json:"-"`
	RuntimeToken           string    `json:"-"`
	ControlURL             string    `json:"-"`
	SessionAuthorizationID string    `json:"-"`
	secretHash             [32]byte
	kernelCapabilityHash   [32]byte
}

type NotebookSessionService struct {
	jupyter          *JupyterService
	tasks            *DevTaskService
	catalog          NotebookSessionControlPlane
	ttl              time.Duration
	ownerAPIBaseURL  string
	mu               sync.RWMutex
	items            map[string]*NotebookSession
	activeExecutions map[string]map[string]context.CancelFunc
	stop             chan struct{}
	once             sync.Once
}

func NewNotebookSessionService(jupyter *JupyterService, tasks *DevTaskService, catalog NotebookSessionControlPlane, ttl time.Duration, ownerAPIBaseURL string) *NotebookSessionService {
	if ttl <= 0 {
		ttl = time.Hour
	}
	service := &NotebookSessionService{
		jupyter:          jupyter,
		tasks:            tasks,
		catalog:          catalog,
		ttl:              ttl,
		ownerAPIBaseURL:  strings.TrimRight(strings.TrimSpace(ownerAPIBaseURL), "/"),
		items:            make(map[string]*NotebookSession),
		activeExecutions: make(map[string]map[string]context.CancelFunc),
		stop:             make(chan struct{}),
	}
	go service.reap()
	return service
}

func (s *NotebookSessionService) Create(ctx context.Context, userAccessToken string, tenantID, userID, taskID uint) (*NotebookSession, string, error) {
	if s == nil || s.jupyter == nil || s.tasks == nil || s.catalog == nil {
		return nil, "", fmt.Errorf("notebook session service is not configured")
	}
	task, err := s.tasks.GetDevTask(taskID, tenantID)
	if err != nil {
		return nil, "", ErrNotebookNotFound
	}
	if !task.IsNotebookScript() {
		return nil, "", ErrTaskNotNotebook
	}
	engineID := task.GetEngineID()
	if engineID == nil {
		return nil, "", fmt.Errorf("notebook task has no bound engine")
	}
	notebookPath, _ := task.Content["notebook_path"].(string)
	kernel, _ := task.Content["kernel"].(string)
	if strings.TrimSpace(kernel) == "" {
		kernel = "python3"
	}

	s.mu.Lock()
	for _, existing := range s.items {
		if existing.TenantID == tenantID && existing.TaskID == taskID && existing.ExpiresAt.After(time.Now()) {
			if existing.UserID != userID {
				s.mu.Unlock()
				return nil, "", ErrNotebookSessionConflict
			}
			secret, hash, secretErr := newNotebookSessionSecret()
			if secretErr != nil {
				s.mu.Unlock()
				return nil, "", secretErr
			}
			existing.secretHash = hash
			public := publicNotebookSession(existing)
			s.mu.Unlock()
			return public, secret, nil
		}
	}
	s.mu.Unlock()

	sessionID := uuid.NewString()
	basePath := "/api/v1/develop/notebook-sessions/" + sessionID + "/"
	secret, hash, err := newNotebookSessionSecret()
	if err != nil {
		return nil, "", err
	}
	if s.ownerAPIBaseURL == "" {
		return nil, "", fmt.Errorf("Develop service URL is required for notebook kernel capabilities")
	}
	kernelCapabilityToken, kernelCapabilityHash, err := newNotebookKernelCapability()
	if err != nil {
		return nil, "", err
	}
	ownerSessionBase := s.ownerAPIBaseURL + "/api/v1/develop/notebook-kernel-sessions/" + url.PathEscape(sessionID)
	ownerAPIEndpoint := ownerSessionBase + "/engine-descriptors"
	ownerCatalogAPIEndpoint := ownerSessionBase + "/catalog/children"
	ownerTableScanAPIEndpoint := ownerSessionBase + "/table-scans"
	ownerRecordScanAPIEndpoint := ownerSessionBase + "/record-scans"
	ownerQueryAPIEndpoint := ownerSessionBase + "/queries"
	ownerGraphSampleAPIEndpoint := ownerSessionBase + "/graph-samples"
	ownerGraphQueryAPIEndpoint := ownerSessionBase + "/graph-queries"
	ownerContentReadAPIEndpoint := ownerSessionBase + "/content-reads"
	ownerChangeStreamAPIEndpoint := ownerSessionBase + "/change-streams"
	runtimeSession, controlURL, err := s.jupyter.OpenInteractiveSession(ctx, tenantID, *engineID, plugin.InteractiveScriptSessionRequest{
		SessionID:                    sessionID,
		TenantID:                     tenantID,
		UserID:                       userID,
		TaskID:                       taskID,
		NotebookPath:                 notebookPath,
		Kernel:                       kernel,
		BasePath:                     basePath,
		TTLSeconds:                   int(s.ttl.Seconds()),
		OwnerAPIEndpoint:             ownerAPIEndpoint,
		OwnerCatalogAPIEndpoint:      ownerCatalogAPIEndpoint,
		OwnerTableScanAPIEndpoint:    ownerTableScanAPIEndpoint,
		OwnerRecordScanAPIEndpoint:   ownerRecordScanAPIEndpoint,
		OwnerQueryAPIEndpoint:        ownerQueryAPIEndpoint,
		OwnerGraphSampleAPIEndpoint:  ownerGraphSampleAPIEndpoint,
		OwnerGraphQueryAPIEndpoint:   ownerGraphQueryAPIEndpoint,
		OwnerContentReadAPIEndpoint:  ownerContentReadAPIEndpoint,
		OwnerChangeStreamAPIEndpoint: ownerChangeStreamAPIEndpoint,
		OwnerCapabilityToken:         kernelCapabilityToken,
	})
	if err != nil {
		return nil, "", err
	}
	expiresIn := int64(time.Until(runtimeSession.ExpiresAt).Seconds())
	if expiresIn <= 0 {
		_ = s.jupyter.CloseInteractiveSession(ctx, tenantID, controlURL, sessionID)
		return nil, "", fmt.Errorf("notebook runtime session expired during creation")
	}
	issued, err := s.catalog.Issue(ctx, userAccessToken, commonClient.IssueNotebookSessionAuthorizationRequest{
		SessionID: sessionID, TaskID: taskID, ExpiresIn: expiresIn,
	})
	if err != nil {
		_ = s.jupyter.CloseInteractiveSession(ctx, tenantID, controlURL, sessionID)
		return nil, "", err
	}
	expiresAt := runtimeSession.ExpiresAt
	if issued.ExpiresAt.Before(expiresAt) {
		expiresAt = issued.ExpiresAt
	}
	session := &NotebookSession{
		ID:                     sessionID,
		TaskID:                 taskID,
		URL:                    basePath + "lab/tree/" + url.PathEscape(runtimeSession.NotebookName),
		ExpiresAt:              expiresAt,
		TenantID:               tenantID,
		UserID:                 userID,
		EngineID:               *engineID,
		Endpoint:               runtimeSession.Endpoint,
		RuntimeToken:           runtimeSession.RuntimeToken,
		ControlURL:             controlURL,
		SessionAuthorizationID: issued.ID,
		secretHash:             hash,
		kernelCapabilityHash:   kernelCapabilityHash,
	}
	s.mu.Lock()
	s.items[sessionID] = session
	s.mu.Unlock()
	return publicNotebookSession(session), secret, nil
}

func (s *NotebookSessionService) ListCatalogChildren(
	ctx context.Context,
	sessionID, token string,
	request commonClient.NotebookCatalogChildrenRequest,
) ([]commonClient.EngineCatalogEntry, error) {
	session, err := s.ResolveKernelCapability(sessionID, token)
	if err != nil {
		return nil, err
	}
	request.SessionID = session.ID
	return s.catalog.ListChildren(ctx, session.TenantID, session.SessionAuthorizationID, request)
}

type NotebookTableScanRequest struct {
	EngineID  uint
	Path      commonClient.EngineCatalogPath
	BatchSize int
	MaxRows   int64
}

func (s *NotebookSessionService) StreamTable(
	ctx context.Context,
	sessionID, token string,
	request NotebookTableScanRequest,
	destination io.Writer,
	ready func(),
) error {
	if request.EngineID == 0 || request.BatchSize <= 0 || request.BatchSize > 1_000_000 || request.MaxRows < 0 ||
		request.Path.EngineID != request.EngineID || request.Path.Version != plugin.CatalogPathVersion ||
		len(request.Path.Segments) == 0 || destination == nil {
		return ErrNotebookTableScanInvalid
	}
	session, err := s.ResolveKernelCapability(sessionID, token)
	if err != nil {
		return err
	}
	executionID := uuid.NewString()
	executionCtx, cancel, err := s.registerNotebookExecution(ctx, session.ID, executionID)
	if err != nil {
		return err
	}
	defer cancel()
	defer s.unregisterNotebookExecution(session.ID, executionID)
	expiresIn := int64(time.Until(session.ExpiresAt).Seconds())
	if expiresIn <= 0 {
		return ErrNotebookSessionNotFound
	}
	access, err := s.catalog.DeriveExecutionEngineAccess(executionCtx, session.TenantID, session.SessionAuthorizationID,
		commonClient.NotebookExecutionEngineAccessRequest{
			SessionID: session.ID, ExecutionID: executionID, EngineID: request.EngineID, ExpiresIn: expiresIn,
		})
	if err != nil {
		return err
	}
	if access.Engine == nil || access.Engine.ID != request.EngineID || access.Engine.EngineType == "" {
		return fmt.Errorf("notebook execution engine access is invalid")
	}
	enginePlugin, err := plugin.Get(access.Engine.EngineType)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNotebookTableScanUnsupported, err)
	}
	provider, ok := enginePlugin.(plugin.TableReadSessionProvider)
	if !ok {
		return ErrNotebookTableScanUnsupported
	}
	path := notebookPluginCatalogPath(request.Path)
	readSession, err := provider.OpenTableReadSession(executionCtx, plugin.ConnectionInfo(access.Engine.ConnectionInfo), path,
		plugin.TableReadSessionOptions{Hints: map[string]interface{}{plugin.TableReadHintGeometryEncoding: "ewkb"}})
	if err != nil {
		return err
	}
	defer closeNotebookReadSession(readSession)

	return s.streamNotebookReadSession(executionCtx, session, executionID, access, readSession,
		request.BatchSize, request.MaxRows, destination, ready)
}

type NotebookQueryRequest struct {
	EngineID uint
	Language string
	Query    string
	Params   []interface{}
	MaxRows  int64
	Timeout  time.Duration
}

func (s *NotebookSessionService) StreamQuery(
	ctx context.Context,
	sessionID, token string,
	request NotebookQueryRequest,
	destination io.Writer,
	ready func(),
) error {
	request.Language = strings.ToLower(strings.TrimSpace(request.Language))
	if request.EngineID == 0 || request.Language == "" || strings.TrimSpace(request.Query) == "" || request.MaxRows <= 0 ||
		request.MaxRows > 1_000_000 || request.Timeout <= 0 || request.Timeout > 5*time.Minute || destination == nil {
		return ErrNotebookQueryInvalid
	}
	session, err := s.ResolveKernelCapability(sessionID, token)
	if err != nil {
		return err
	}
	queryCtx, timeoutCancel := context.WithTimeout(ctx, request.Timeout)
	defer timeoutCancel()
	executionID := uuid.NewString()
	executionCtx, cancel, err := s.registerNotebookExecution(queryCtx, session.ID, executionID)
	if err != nil {
		return err
	}
	defer cancel()
	defer s.unregisterNotebookExecution(session.ID, executionID)
	expiresIn := int64(time.Until(session.ExpiresAt).Seconds())
	if expiresIn <= 0 {
		return ErrNotebookSessionNotFound
	}
	access, err := s.catalog.DeriveExecutionEngineAccess(executionCtx, session.TenantID, session.SessionAuthorizationID,
		commonClient.NotebookExecutionEngineAccessRequest{
			SessionID: session.ID, ExecutionID: executionID, EngineID: request.EngineID, ExpiresIn: expiresIn,
		})
	if err != nil {
		return err
	}
	if access.Engine == nil || access.Engine.ID != request.EngineID || access.Engine.EngineType == "" {
		return ErrNotebookQueryUnsupported
	}
	enginePlugin, err := plugin.Get(access.Engine.EngineType)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNotebookQueryUnsupported, err)
	}
	provider, ok := enginePlugin.(plugin.QueryRuntimeProvider)
	if !ok {
		return ErrNotebookQueryUnsupported
	}
	if !notebookQueryLanguageSupported(provider.QueryLanguages(), request.Language) {
		return ErrNotebookQueryUnsupported
	}
	if len(request.Params) > 0 {
		parameterized, ok := enginePlugin.(plugin.ParameterizedSQLQueryRuntimeProvider)
		if request.Language != "sql" || !ok || !parameterized.SupportsParameterizedQueries() {
			return ErrNotebookQueryUnsupported
		}
	}
	result, err := provider.ExecuteRuntimeQuery(executionCtx, plugin.ConnectionInfo(access.Engine.ConnectionInfo), plugin.QueryRequest{
		EngineID: request.EngineID,
		Language: request.Language,
		Query:    request.Query,
		Options: plugin.QueryOptions{
			EngineID: request.EngineID, EngineType: access.Engine.EngineType,
			Limit: int(request.MaxRows), Timeout: request.Timeout, ReadOnly: true, Args: request.Params,
		},
	})
	if err != nil {
		return err
	}
	batch := plugin.QueryResultToBatchData(result, 0)
	if len(batch.Rows) > int(request.MaxRows) {
		batch.Rows = batch.Rows[:request.MaxRows]
	}
	writer, err := plugin.NewBatchArrowStreamWriter(destination, batch.Fields)
	if err != nil {
		return err
	}
	if ready != nil {
		ready()
	}
	if err := writer.WriteBatch(batch); err != nil {
		_ = writer.Close()
		return err
	}
	notifyNotebookStreamFlush(destination)
	return writer.Close()
}

func notebookQueryLanguageSupported(languages []string, requested string) bool {
	for _, language := range languages {
		if strings.EqualFold(strings.TrimSpace(language), requested) {
			return true
		}
	}
	return false
}

func (s *NotebookSessionService) streamNotebookReadSession(
	executionCtx context.Context,
	session *NotebookSession,
	executionID string,
	access *commonClient.ExecutionEngineAccess,
	readSession plugin.TableReadSession,
	batchSize int,
	maxRows int64,
	destination io.Writer,
	ready func(),
) error {
	firstLimit := notebookScanBatchLimit(batchSize, maxRows, 0)
	firstBatch, err := readSession.ReadBatch(executionCtx, firstLimit)
	if err != nil {
		return err
	}
	if firstBatch == nil || len(firstBatch.Fields) == 0 {
		return fmt.Errorf("notebook table scan returned no schema")
	}
	trimNotebookBatchRows(firstBatch, maxRows)
	if ready != nil {
		ready()
	}
	arrowWriter, err := plugin.NewBatchArrowStreamWriter(destination, firstBatch.Fields)
	if err != nil {
		return err
	}
	defer arrowWriter.Close()
	if err := arrowWriter.WriteBatch(firstBatch); err != nil {
		return err
	}
	notifyNotebookStreamFlush(destination)
	rowsRead := int64(len(firstBatch.Rows))
	nextLeaseCheck := time.Now().Add(notebookExecutionLeaseCheckInterval)
	for len(firstBatch.Rows) == firstLimit && (maxRows == 0 || rowsRead < maxRows) {
		if !time.Now().Before(nextLeaseCheck) {
			if _, err := s.catalog.ValidateExecutionEngineAccess(executionCtx, session.TenantID, access.AuthorizationID,
				commonClient.ExecutionEngineAccessRequest{
					ExecutionID: executionID, EngineID: access.EngineID, RequiredEffects: []string{"read"},
				}); err != nil {
				return err
			}
			nextLeaseCheck = time.Now().Add(notebookExecutionLeaseCheckInterval)
		}
		limit := notebookScanBatchLimit(batchSize, maxRows, rowsRead)
		batch, err := readSession.ReadBatch(executionCtx, limit)
		if err != nil {
			return err
		}
		if batch == nil || len(batch.Rows) == 0 {
			break
		}
		trimNotebookBatchRows(batch, maxRows-rowsRead)
		if err := arrowWriter.WriteBatch(batch); err != nil {
			return err
		}
		notifyNotebookStreamFlush(destination)
		rowsRead += int64(len(batch.Rows))
		firstBatch = batch
	}
	return arrowWriter.Close()
}

func trimNotebookBatchRows(batch *plugin.BatchData, maxRows int64) {
	if batch == nil || maxRows <= 0 || int64(len(batch.Rows)) <= maxRows {
		return
	}
	batch.Rows = batch.Rows[:maxRows]
}

func notebookPluginCatalogPath(path commonClient.EngineCatalogPath) plugin.CatalogPath {
	segments := make([]plugin.CatalogSegment, len(path.Segments))
	for index, segment := range path.Segments {
		segments[index] = plugin.CatalogSegment{Term: segment.Term, Kind: segment.Kind, Name: segment.Name}
	}
	return plugin.CatalogPath{Version: path.Version, EngineID: path.EngineID, Segments: segments}
}

func notebookScanBatchLimit(batchSize int, maxRows, rowsRead int64) int {
	if maxRows == 0 || maxRows-rowsRead >= int64(batchSize) {
		return batchSize
	}
	return int(maxRows - rowsRead)
}

func closeNotebookReadSession(readSession plugin.TableReadSession) {
	if readSession == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), notebookReadSessionCloseTimeout)
	defer cancel()
	_ = readSession.Close(ctx)
}

func notifyNotebookStreamFlush(destination io.Writer) {
	if flusher, ok := destination.(interface{ Flush() }); ok {
		flusher.Flush()
	}
}

func (s *NotebookSessionService) registerNotebookExecution(
	ctx context.Context, sessionID, executionID string,
) (context.Context, context.CancelFunc, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if session := s.items[sessionID]; session == nil || !session.ExpiresAt.After(time.Now()) {
		return nil, nil, ErrNotebookSessionNotFound
	}
	executionCtx, cancel := context.WithCancel(ctx)
	if s.activeExecutions[sessionID] == nil {
		s.activeExecutions[sessionID] = make(map[string]context.CancelFunc)
	}
	s.activeExecutions[sessionID][executionID] = cancel
	return executionCtx, cancel, nil
}

func (s *NotebookSessionService) unregisterNotebookExecution(sessionID, executionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.activeExecutions[sessionID], executionID)
	if len(s.activeExecutions[sessionID]) == 0 {
		delete(s.activeExecutions, sessionID)
	}
}

func (s *NotebookSessionService) cancelNotebookExecutionsLocked(sessionID string) {
	for _, cancel := range s.activeExecutions[sessionID] {
		cancel()
	}
	delete(s.activeExecutions, sessionID)
}

func (s *NotebookSessionService) ResolveKernelCapability(sessionID, token string) (*NotebookSession, error) {
	if !strings.HasPrefix(token, NotebookKernelCapabilityPrefix) {
		return nil, ErrNotebookSessionNotFound
	}
	s.mu.RLock()
	session := s.items[sessionID]
	if session == nil || !session.ExpiresAt.After(time.Now()) {
		s.mu.RUnlock()
		return nil, ErrNotebookSessionNotFound
	}
	provided := sha256.Sum256([]byte(token))
	valid := subtle.ConstantTimeCompare(session.kernelCapabilityHash[:], provided[:]) == 1
	if !valid {
		s.mu.RUnlock()
		return nil, ErrNotebookSessionNotFound
	}
	copy := *session
	s.mu.RUnlock()
	return &copy, nil
}

func (s *NotebookSessionService) ListDataEngineDescriptors(ctx context.Context, sessionID, token string) ([]commonModels.EngineRuntimeDescriptor, error) {
	session, err := s.ResolveKernelCapability(sessionID, token)
	if err != nil {
		return nil, err
	}
	return s.catalog.ListEngineDescriptors(ctx, session.TenantID, session.SessionAuthorizationID, session.ID)
}

func (s *NotebookSessionService) Resolve(sessionID, secret string) (*NotebookSession, error) {
	s.mu.RLock()
	session := s.items[sessionID]
	if session == nil || !session.ExpiresAt.After(time.Now()) {
		s.mu.RUnlock()
		return nil, ErrNotebookSessionNotFound
	}
	provided := sha256.Sum256([]byte(secret))
	valid := subtle.ConstantTimeCompare(session.secretHash[:], provided[:]) == 1
	if !valid {
		s.mu.RUnlock()
		return nil, ErrNotebookSessionNotFound
	}
	copy := *session
	s.mu.RUnlock()
	return &copy, nil
}

func (s *NotebookSessionService) Close(ctx context.Context, tenantID, userID, taskID uint, sessionID string) error {
	s.mu.Lock()
	session := s.items[sessionID]
	if session == nil || session.TenantID != tenantID || session.UserID != userID || session.TaskID != taskID {
		s.mu.Unlock()
		return ErrNotebookSessionNotFound
	}
	delete(s.items, sessionID)
	s.cancelNotebookExecutionsLocked(sessionID)
	s.mu.Unlock()
	revokeErr := s.catalog.Revoke(ctx, session.TenantID, session.SessionAuthorizationID,
		commonClient.RevokeNotebookSessionAuthorizationRequest{SessionID: session.ID})
	closeErr := s.jupyter.CloseInteractiveSession(ctx, session.TenantID, session.ControlURL, session.ID)
	return errors.Join(revokeErr, closeErr)
}

func (s *NotebookSessionService) Shutdown(ctx context.Context) {
	if s == nil {
		return
	}
	s.once.Do(func() { close(s.stop) })
	s.mu.Lock()
	items := make([]*NotebookSession, 0, len(s.items))
	for _, session := range s.items {
		items = append(items, session)
	}
	s.items = make(map[string]*NotebookSession)
	for sessionID := range s.activeExecutions {
		s.cancelNotebookExecutionsLocked(sessionID)
	}
	s.mu.Unlock()
	for _, session := range items {
		_ = s.catalog.Revoke(ctx, session.TenantID, session.SessionAuthorizationID,
			commonClient.RevokeNotebookSessionAuthorizationRequest{SessionID: session.ID})
		_ = s.jupyter.CloseInteractiveSession(ctx, session.TenantID, session.ControlURL, session.ID)
	}
}

func (s *NotebookSessionService) reap() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case now := <-ticker.C:
			s.mu.Lock()
			expired := make([]*NotebookSession, 0)
			for id, session := range s.items {
				if !session.ExpiresAt.After(now) {
					expired = append(expired, session)
					delete(s.items, id)
					s.cancelNotebookExecutionsLocked(id)
				}
			}
			s.mu.Unlock()
			for _, session := range expired {
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				_ = s.catalog.Revoke(ctx, session.TenantID, session.SessionAuthorizationID,
					commonClient.RevokeNotebookSessionAuthorizationRequest{SessionID: session.ID})
				_ = s.jupyter.CloseInteractiveSession(ctx, session.TenantID, session.ControlURL, session.ID)
				cancel()
			}
		}
	}
}

func newNotebookSessionSecret() (string, [32]byte, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", [32]byte{}, fmt.Errorf("generate notebook session secret: %w", err)
	}
	secret := base64.RawURLEncoding.EncodeToString(raw[:])
	return secret, sha256.Sum256([]byte(secret)), nil
}

func newNotebookKernelCapability() (string, [32]byte, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", [32]byte{}, fmt.Errorf("generate notebook kernel capability: %w", err)
	}
	token := NotebookKernelCapabilityPrefix + base64.RawURLEncoding.EncodeToString(raw[:])
	return token, sha256.Sum256([]byte(token)), nil
}

func publicNotebookSession(session *NotebookSession) *NotebookSession {
	return &NotebookSession{ID: session.ID, TaskID: session.TaskID, URL: session.URL, ExpiresAt: session.ExpiresAt}
}
