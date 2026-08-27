package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/develop/backend/internal/repository"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
)

type notebookSessionControlPlaneRecorder struct {
	issueError          error
	issuedAuthorization string
	issuedRequest       commonClient.IssueNotebookSessionAuthorizationRequest
	issuedUserToken     string
	listRequest         commonClient.NotebookEngineCatalogChildrenRequest
	listTenantID        uint
	listAuthorizationID string
	revokeRequest       commonClient.RevokeNotebookSessionAuthorizationRequest
	revokeTenantID      uint
	revokeAuthorization string
	engine              *commonModels.Engine
}

func (r *notebookSessionControlPlaneRecorder) Issue(
	_ context.Context,
	userToken string,
	request commonClient.IssueNotebookSessionAuthorizationRequest,
) (*commonClient.IssuedNotebookSessionAuthorization, error) {
	r.issuedUserToken = userToken
	r.issuedRequest = request
	if r.issueError != nil {
		return nil, r.issueError
	}
	return &commonClient.IssuedNotebookSessionAuthorization{
		ID: r.issuedAuthorization, SessionID: request.SessionID, TaskID: request.TaskID,
		ExpiresAt: time.Now().UTC().Add(5 * time.Minute),
	}, nil
}

func (r *notebookSessionControlPlaneRecorder) ListChildren(
	_ context.Context,
	tenantID uint,
	authorizationID string,
	request commonClient.NotebookEngineCatalogChildrenRequest,
) ([]commonClient.EngineCatalogEntry, error) {
	r.listTenantID = tenantID
	r.listAuthorizationID = authorizationID
	r.listRequest = request
	return []commonClient.EngineCatalogEntry{{Name: "public"}}, nil
}

func (r *notebookSessionControlPlaneRecorder) ListEngineDescriptors(
	_ context.Context,
	_ uint,
	_ string,
	_ string,
) ([]commonModels.EngineRuntimeDescriptor, error) {
	if r.engine == nil {
		return []commonModels.EngineRuntimeDescriptor{}, nil
	}
	return []commonModels.EngineRuntimeDescriptor{{
		ID: r.engine.ID, Name: r.engine.Name, EngineType: r.engine.EngineType,
		LifecycleState: r.engine.LifecycleState, Capabilities: r.engine.Capabilities,
	}}, nil
}

func (r *notebookSessionControlPlaneRecorder) Revoke(
	_ context.Context,
	tenantID uint,
	authorizationID string,
	request commonClient.RevokeNotebookSessionAuthorizationRequest,
) error {
	r.revokeTenantID = tenantID
	r.revokeAuthorization = authorizationID
	r.revokeRequest = request
	return nil
}

func (r *notebookSessionControlPlaneRecorder) DeriveExecutionEngineAccess(
	_ context.Context,
	_ uint,
	_ string,
	request commonClient.NotebookExecutionEngineAccessRequest,
) (*commonClient.ExecutionEngineAccess, error) {
	return &commonClient.ExecutionEngineAccess{
		AuthorizationID: "1", ExecutionID: request.ExecutionID, EngineID: strconv.FormatUint(uint64(request.EngineID), 10),
		Audience: "develop", Effects: []string{"read"}, ExpiresAt: time.Now().Add(time.Minute),
		Engine: r.engine,
	}, nil
}

type notebookTestReadSession struct {
	mu          sync.Mutex
	fields      []datatype.FieldInfo
	rows        []map[string]interface{}
	offset      int
	blockAtEnd  bool
	readBlocked chan struct{}
	closeCalled chan struct{}
	blockOnce   sync.Once
	closeOnce   sync.Once
}

func (s *notebookTestReadSession) ReadBatch(ctx context.Context, limit int) (*plugin.BatchData, error) {
	s.mu.Lock()
	if s.offset < len(s.rows) {
		end := min(s.offset+limit, len(s.rows))
		rows := append([]map[string]interface{}(nil), s.rows[s.offset:end]...)
		s.offset = end
		s.mu.Unlock()
		return &plugin.BatchData{Fields: s.fields, Rows: rows}, nil
	}
	s.mu.Unlock()
	if s.blockAtEnd {
		s.blockOnce.Do(func() { close(s.readBlocked) })
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return &plugin.BatchData{Fields: s.fields}, nil
}

func (s *notebookTestReadSession) Close(ctx context.Context) error {
	if _, ok := ctx.Deadline(); !ok {
		return errors.New("Notebook read session close context has no deadline")
	}
	s.closeOnce.Do(func() { close(s.closeCalled) })
	return nil
}

type notebookTestQueryPlugin struct {
	request plugin.QueryRequest
	result  *plugin.QueryResult
}

func (p *notebookTestQueryPlugin) Type() string         { return "postgresql" }
func (p *notebookTestQueryPlugin) DisplayName() string  { return "Notebook Test PostgreSQL" }
func (p *notebookTestQueryPlugin) EngineOrigin() string { return "general" }
func (p *notebookTestQueryPlugin) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (p *notebookTestQueryPlugin) ValidateConnectionInfo(plugin.ConnectionInfo) error { return nil }
func (p *notebookTestQueryPlugin) DefaultPort() int                                   { return 5432 }
func (p *notebookTestQueryPlugin) RequiredFields() []string                           { return nil }
func (p *notebookTestQueryPlugin) SensitiveFields() []string                          { return nil }
func (p *notebookTestQueryPlugin) Capabilities() plugin.EngineCapabilities {
	return plugin.EngineCapabilities{}
}
func (p *notebookTestQueryPlugin) QueryLanguages() []string { return []string{"sql"} }
func (p *notebookTestQueryPlugin) GenerateSampleQuery(context.Context, plugin.ConnectionInfo, plugin.SampleQueryOptions) (string, string) {
	return "", "sql"
}
func (p *notebookTestQueryPlugin) ExecuteRuntimeQuery(_ context.Context, _ plugin.ConnectionInfo, request plugin.QueryRequest) (*plugin.QueryResult, error) {
	p.request = request
	return p.result, nil
}
func (p *notebookTestQueryPlugin) SQLDialect() string { return "postgresql" }
func (p *notebookTestQueryPlugin) ExecuteSQL(context.Context, plugin.ConnectionInfo, string, plugin.QueryOptions) (*plugin.QueryResult, error) {
	return p.result, nil
}
func (p *notebookTestQueryPlugin) SupportsParameterizedQueries() bool {
	return true
}

func (r *notebookSessionControlPlaneRecorder) ValidateExecutionEngineAccess(
	_ context.Context,
	_ uint,
	_ string,
	request commonClient.ExecutionEngineAccessRequest,
) (*commonClient.ExecutionEngineAccess, error) {
	return &commonClient.ExecutionEngineAccess{
		AuthorizationID: "1", ExecutionID: request.ExecutionID, EngineID: request.EngineID,
		Audience: "develop", Effects: []string{"read"}, ExpiresAt: time.Now().Add(time.Minute),
	}, nil
}

func TestNotebookSessionResolveRequiresMatchingSecretAndLiveSession(t *testing.T) {
	secret := "browser-capability"
	service := &NotebookSessionService{items: map[string]*NotebookSession{
		"live": {
			ID: "live", TaskID: 12, TenantID: 7, UserID: 9,
			Endpoint: "http://jupyter:31000", RuntimeToken: "runtime-secret",
			ExpiresAt: time.Now().Add(time.Minute), secretHash: sha256.Sum256([]byte(secret)),
		},
	}}
	resolved, err := service.Resolve("live", secret)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.RuntimeToken != "runtime-secret" {
		t.Fatalf("resolved session = %#v", resolved)
	}
	if _, err := service.Resolve("live", "wrong"); err != ErrNotebookSessionNotFound {
		t.Fatalf("wrong secret error = %v", err)
	}
	service.items["live"].ExpiresAt = time.Now().Add(-time.Second)
	if _, err := service.Resolve("live", secret); err != ErrNotebookSessionNotFound {
		t.Fatalf("expired session error = %v", err)
	}
}

func TestNotebookSessionResolveKernelCapabilityRequiresBoundLiveToken(t *testing.T) {
	token := "addp_nkc_kernel-capability"
	service := &NotebookSessionService{items: map[string]*NotebookSession{
		"live": {
			ID: "live", TaskID: 12, TenantID: 7, UserID: 9,
			ExpiresAt: time.Now().Add(time.Minute), kernelCapabilityHash: sha256.Sum256([]byte(token)),
		},
	}}

	resolved, err := service.ResolveKernelCapability("live", token)
	if err != nil {
		t.Fatalf("ResolveKernelCapability() error = %v", err)
	}
	if resolved.TenantID != 7 || resolved.UserID != 9 || resolved.TaskID != 12 {
		t.Fatalf("resolved session = %#v", resolved)
	}
	for _, invalid := range []string{"kernel-capability", "addp_nkc_wrong", ""} {
		if _, err := service.ResolveKernelCapability("live", invalid); err != ErrNotebookSessionNotFound {
			t.Fatalf("token %q error = %v", invalid, err)
		}
	}
	service.items["live"].ExpiresAt = time.Now().Add(-time.Second)
	if _, err := service.ResolveKernelCapability("live", token); err != ErrNotebookSessionNotFound {
		t.Fatalf("expired session error = %v", err)
	}
}

func TestPublicNotebookSessionDoesNotExposeProxyCredentials(t *testing.T) {
	public := publicNotebookSession(&NotebookSession{
		ID: "session", TaskID: 12, URL: "/proxy", ExpiresAt: time.Now().Add(time.Hour),
		Endpoint: "http://jupyter:31000", RuntimeToken: "runtime-secret", ControlURL: "http://jupyter:8097",
		kernelCapabilityHash: sha256.Sum256([]byte("addp_nkc_kernel-secret")),
	})
	if public.Endpoint != "" || public.RuntimeToken != "" || public.ControlURL != "" {
		t.Fatalf("public session leaked internal facts: %#v", public)
	}
	if public.kernelCapabilityHash != ([32]byte{}) {
		t.Fatal("public session leaked the kernel capability hash")
	}
}

func TestNotebookSessionAuthorizationFollowsSessionLifecycle(t *testing.T) {
	service, catalog, runtimeCloseCount := newNotebookEngineCatalogSessionTestService(t, nil)
	public, secret, err := service.Create(context.Background(), "addp_at_user", 7, 9, 14)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if public.ID == "" || secret == "" || catalog.issuedRequest.SessionID != public.ID ||
		catalog.issuedRequest.TaskID != 14 || catalog.issuedUserToken != "addp_at_user" {
		t.Fatalf("issued session=%#v request=%#v token=%q", public, catalog.issuedRequest, catalog.issuedUserToken)
	}

	stored := service.items[public.ID]
	if stored == nil || stored.SessionAuthorizationID != catalog.issuedAuthorization {
		t.Fatalf("stored session = %#v", stored)
	}
	kernelToken := storedKernelTokenForTest(t, stored)
	nodes, err := service.ListEngineCatalogChildren(context.Background(), public.ID, kernelToken,
		commonClient.NotebookEngineCatalogChildrenRequest{SessionID: "must-be-replaced", EngineID: 21})
	if err != nil || len(nodes) != 1 || catalog.listRequest.SessionID != public.ID ||
		catalog.listTenantID != 7 || catalog.listAuthorizationID != catalog.issuedAuthorization {
		t.Fatalf("ListEngineCatalogChildren() nodes=%#v error=%v request=%#v", nodes, err, catalog.listRequest)
	}

	if err := service.Close(context.Background(), 7, 9, 14, public.ID); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if service.items[public.ID] != nil || catalog.revokeRequest.SessionID != public.ID ||
		catalog.revokeTenantID != 7 || catalog.revokeAuthorization != catalog.issuedAuthorization ||
		*runtimeCloseCount != 1 {
		t.Fatalf("close state items=%#v revoke=%#v runtimeClose=%d", service.items, catalog.revokeRequest, *runtimeCloseCount)
	}
}

func TestNotebookSessionIssueFailureClosesRuntimeWithoutPublishingSession(t *testing.T) {
	issueError := errors.New("authorization issue failed")
	service, catalog, runtimeCloseCount := newNotebookEngineCatalogSessionTestService(t, issueError)
	public, secret, err := service.Create(context.Background(), "addp_at_user", 7, 9, 14)
	if !errors.Is(err, issueError) || public != nil || secret != "" {
		t.Fatalf("Create() session=%#v secret=%q error=%v", public, secret, err)
	}
	if len(service.items) != 0 || catalog.issuedRequest.SessionID == "" || *runtimeCloseCount != 1 {
		t.Fatalf("failure state items=%#v request=%#v runtimeClose=%d", service.items, catalog.issuedRequest, *runtimeCloseCount)
	}
}

func TestNotebookSessionStreamWritesMultipleArrowBatchesAndHonorsMaxRows(t *testing.T) {
	fields := []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt}}
	readSession := &notebookTestReadSession{
		fields: fields,
		rows: []map[string]interface{}{
			{"id": int64(1)}, {"id": int64(2)}, {"id": int64(3)}, {"id": int64(4)},
		},
		closeCalled: make(chan struct{}),
	}
	service := &NotebookSessionService{catalog: &notebookSessionControlPlaneRecorder{}}
	var destination bytes.Buffer
	err := service.streamNotebookReadSession(
		context.Background(),
		&NotebookSession{ID: "session", TenantID: 7},
		"execution",
		&commonClient.ExecutionEngineAccess{AuthorizationID: "1", EngineID: "21"},
		readSession,
		2,
		3,
		&destination,
		nil,
	)
	if err != nil {
		t.Fatalf("streamNotebookReadSession() error = %v", err)
	}
	ids, batchCount := notebookArrowIDs(t, destination.Bytes())
	if batchCount != 2 || len(ids) != 3 || ids[0] != 1 || ids[1] != 2 || ids[2] != 3 {
		t.Fatalf("Arrow stream batches=%d ids=%#v", batchCount, ids)
	}
}

func TestNotebookSessionCloseCancelsActiveReadAndUsesBoundedCursorClose(t *testing.T) {
	service, _, _ := newNotebookEngineCatalogSessionTestService(t, nil)
	public, _, err := service.Create(context.Background(), "addp_at_user", 7, 9, 14)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	executionCtx, cancel, err := service.registerNotebookExecution(context.Background(), public.ID, "execution")
	if err != nil {
		t.Fatalf("registerNotebookExecution() error = %v", err)
	}
	readSession := &notebookTestReadSession{
		fields:     []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt}},
		rows:       []map[string]interface{}{{"id": int64(1)}},
		blockAtEnd: true, readBlocked: make(chan struct{}), closeCalled: make(chan struct{}),
	}
	streamErr := make(chan error, 1)
	go func() {
		defer cancel()
		defer service.unregisterNotebookExecution(public.ID, "execution")
		defer closeNotebookReadSession(readSession)
		streamErr <- service.streamNotebookReadSession(
			executionCtx,
			&NotebookSession{ID: public.ID, TenantID: 7},
			"execution",
			&commonClient.ExecutionEngineAccess{AuthorizationID: "1", EngineID: "21"},
			readSession,
			1,
			0,
			&bytes.Buffer{},
			nil,
		)
	}()
	select {
	case <-readSession.readBlocked:
	case <-time.After(time.Second):
		t.Fatal("read session did not reach its blocking cursor read")
	}
	if err := service.Close(context.Background(), 7, 9, 14, public.ID); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	select {
	case err := <-streamErr:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("stream error = %v, want context cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("active Notebook read was not canceled by Session close")
	}
	select {
	case <-readSession.closeCalled:
	case <-time.After(time.Second):
		t.Fatal("Notebook cursor was not closed")
	}
}

func TestNotebookSessionQueryRejectsMissingLanguageOrQuery(t *testing.T) {
	service := &NotebookSessionService{}
	for _, request := range []NotebookQueryRequest{
		{EngineID: 21, Query: "SELECT 1", MaxRows: 10, Timeout: 30 * time.Second},
		{EngineID: 21, Language: "sql", Query: "", MaxRows: 10, Timeout: 30 * time.Second},
	} {
		err := service.StreamQuery(context.Background(), "session", "token", NotebookQueryRequest{
			EngineID: request.EngineID, Language: request.Language, Query: request.Query,
			MaxRows: request.MaxRows, Timeout: request.Timeout,
		}, &bytes.Buffer{}, nil)
		if !errors.Is(err, ErrNotebookQueryInvalid) {
			t.Fatalf("request %#v error = %v, want invalid query", request, err)
		}
	}
}

func TestNotebookSessionQueryAppliesServerMaxRows(t *testing.T) {
	service, catalog, _ := newNotebookEngineCatalogSessionTestService(t, nil)
	public, _, err := service.Create(context.Background(), "addp_at_user", 7, 9, 14)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	kernelToken := storedKernelTokenForTest(t, service.items[public.ID])
	provider := &notebookTestQueryPlugin{result: &plugin.QueryResult{
		Columns: []string{"id"},
		Rows: []map[string]interface{}{
			{"id": int64(1)}, {"id": int64(2)}, {"id": int64(3)}, {"id": int64(4)},
		},
	}}
	previous, previousErr := plugin.Get("postgresql")
	plugin.Register(provider)
	t.Cleanup(func() {
		if previousErr == nil {
			plugin.Register(previous)
		} else {
			plugin.Unregister("postgresql")
		}
	})
	catalog.engine = &commonModels.Engine{
		ID: 21, EngineType: "postgresql", ConnectionInfo: commonModels.ConnectionInfo{"host": "database"},
	}
	var destination bytes.Buffer
	err = service.StreamQuery(context.Background(), public.ID, kernelToken, NotebookQueryRequest{
		EngineID: 21, Language: "sql", Query: "SELECT id FROM public.roads", Params: []interface{}{},
		MaxRows: 3, Timeout: 30 * time.Second,
	}, &destination, nil)
	if err != nil {
		t.Fatalf("StreamQuery() error = %v", err)
	}
	ids, _ := notebookArrowIDs(t, destination.Bytes())
	if len(ids) != 3 || ids[2] != 3 {
		t.Fatalf("bounded query ids = %#v", ids)
	}
	if provider.request.Language != "sql" || provider.request.Options.Limit != 3 || !provider.request.Options.ReadOnly {
		t.Fatalf("query request = %#v", provider.request)
	}
}

func notebookArrowIDs(t *testing.T, payload []byte) ([]int64, int) {
	t.Helper()
	reader, err := ipc.NewReader(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("open Arrow stream: %v", err)
	}
	defer reader.Release()
	var ids []int64
	batches := 0
	for reader.Next() {
		batches++
		column := reader.Record().Column(0).(*array.Int64)
		for index := 0; index < column.Len(); index++ {
			ids = append(ids, column.Value(index))
		}
	}
	if err := reader.Err(); err != nil {
		t.Fatalf("read Arrow stream: %v", err)
	}
	return ids, batches
}

func newNotebookEngineCatalogSessionTestService(
	t *testing.T,
	issueError error,
) (*NotebookSessionService, *notebookSessionControlPlaneRecorder, *int) {
	t.Helper()
	db := newNotebookBindingTestDB(t)
	if err := db.Exec(`
		INSERT INTO develop.dev_tasks (
			id, tenant_id, name, display_name, dev_type, content, execution_config,
			editor_layout, timeout, tags, created_by, status
		) VALUES (
			14, 7, 'analysis', 'Analysis', 'script',
			CAST('{"notebook_path":"analysis.ipynb","kernel":"python3"}' AS BLOB),
			CAST('{"engine_id":10}' AS BLOB), CAST('{}' AS BLOB), 600, '{}', 9, 'active'
		)
	`).Error; err != nil {
		t.Fatalf("seed notebook: %v", err)
	}

	runtimeCloseCount := 0
	var runtimeServer *httptest.Server
	runtimeServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/api/interactive-sessions":
			var payload map[string]any
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success", "session_id": payload["session_id"],
				"endpoint": runtimeServer.URL, "runtime_token": "runtime-secret",
				"notebook_name": "analysis.ipynb",
				"expires_at":    time.Now().UTC().Add(10 * time.Minute).Format(time.RFC3339Nano),
			})
		case request.Method == http.MethodDelete:
			runtimeCloseCount++
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, request)
		}
	}))
	t.Cleanup(runtimeServer.Close)

	catalog := &notebookSessionControlPlaneRecorder{
		issueError: issueError, issuedAuthorization: "00000000-0000-0000-0000-000000000010",
	}
	service := NewNotebookSessionService(
		newJupyterServiceForRuntimeTest(t, runtimeServer.URL),
		NewDevTaskService(repository.NewDevTaskRepository(db), nil),
		catalog,
		10*time.Minute,
		"http://develop:8185",
	)
	t.Cleanup(func() { service.Shutdown(context.Background()) })
	return service, catalog, &runtimeCloseCount
}

func storedKernelTokenForTest(t *testing.T, session *NotebookSession) string {
	t.Helper()
	// The token itself is intentionally not stored. Generate candidates until the
	// hash matches is impossible, so tests replace the hash with a known capability.
	token := NotebookKernelCapabilityPrefix + "test-capability"
	session.kernelCapabilityHash = sha256.Sum256([]byte(token))
	return token
}
