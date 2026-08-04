package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
)

const notebookNativeTestEngineType = "notebook_native_test"

type notebookNativeTestPlugin struct {
	recordSession plugin.RecordReadSession
	graphQuery    string
	graphOptions  plugin.QueryOptions
	contentCalls  int
	rangeCalls    int
	rangeOptions  plugin.ReadOptions
	changeReader  plugin.ChangeStreamReader
}

func (p *notebookNativeTestPlugin) Type() string         { return notebookNativeTestEngineType }
func (p *notebookNativeTestPlugin) DisplayName() string  { return "Notebook Native Test" }
func (p *notebookNativeTestPlugin) EngineOrigin() string { return "general" }
func (p *notebookNativeTestPlugin) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (p *notebookNativeTestPlugin) ValidateConnectionInfo(plugin.ConnectionInfo) error { return nil }
func (p *notebookNativeTestPlugin) DefaultPort() int                                   { return 0 }
func (p *notebookNativeTestPlugin) RequiredFields() []string                           { return nil }
func (p *notebookNativeTestPlugin) SensitiveFields() []string                          { return nil }
func (p *notebookNativeTestPlugin) Capabilities() plugin.EngineCapabilities {
	return plugin.EngineCapabilities{}
}
func (p *notebookNativeTestPlugin) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemantics{}
}
func (p *notebookNativeTestPlugin) OpenRecordReadSession(context.Context, plugin.ConnectionInfo, plugin.CatalogPath, plugin.RecordReadSessionOptions) (plugin.RecordReadSession, error) {
	return p.recordSession, nil
}
func (p *notebookNativeTestPlugin) SampleGraph(context.Context, plugin.ConnectionInfo, plugin.CatalogPath, plugin.GraphSampleOptions) (*plugin.GraphData, error) {
	return &plugin.GraphData{}, nil
}
func (p *notebookNativeTestPlugin) ExecuteGraphQuery(_ context.Context, _ plugin.ConnectionInfo, query string, options plugin.QueryOptions) (*plugin.GraphQueryResult, error) {
	p.graphQuery = query
	p.graphOptions = options
	return &plugin.GraphQueryResult{}, nil
}
func (p *notebookNativeTestPlugin) OpenContent(context.Context, plugin.ConnectionInfo, plugin.CatalogPath, plugin.ReadOptions) (io.ReadCloser, error) {
	p.contentCalls++
	return io.NopCloser(strings.NewReader("complete")), nil
}
func (p *notebookNativeTestPlugin) OpenRange(_ context.Context, _ plugin.ConnectionInfo, _ plugin.CatalogPath, options plugin.ReadOptions) (io.ReadCloser, error) {
	p.rangeCalls++
	p.rangeOptions = options
	return io.NopCloser(strings.NewReader("range")), nil
}
func (p *notebookNativeTestPlugin) OpenChangeStream(context.Context, plugin.ConnectionInfo, plugin.CatalogPath, plugin.ChangeStreamReadOptions) (plugin.ChangeStreamReader, error) {
	return p.changeReader, nil
}

type notebookRecordReadSession struct {
	records     []map[string]interface{}
	read        bool
	closeCalled chan struct{}
	closeOnce   sync.Once
}

func (s *notebookRecordReadSession) ReadBatch(context.Context, int) (*plugin.RecordBatchData, error) {
	if s.read {
		return &plugin.RecordBatchData{}, nil
	}
	s.read = true
	return &plugin.RecordBatchData{Records: s.records}, nil
}

func (s *notebookRecordReadSession) Close(ctx context.Context) error {
	if _, ok := ctx.Deadline(); !ok {
		return errors.New("record read session close context has no deadline")
	}
	s.closeOnce.Do(func() { close(s.closeCalled) })
	return nil
}

type notebookBlockingChangeReader struct {
	pollStarted chan struct{}
	closed      chan struct{}
	pollOnce    sync.Once
	closeOnce   sync.Once
}

func (r *notebookBlockingChangeReader) Poll(ctx context.Context, _ int) (*plugin.ChangeRecordBatch, error) {
	r.pollOnce.Do(func() { close(r.pollStarted) })
	<-ctx.Done()
	return nil, ctx.Err()
}
func (r *notebookBlockingChangeReader) PositionRanges(context.Context) ([]plugin.ChangeStreamPositionRange, error) {
	return nil, nil
}
func (r *notebookBlockingChangeReader) Assignments() []string { return nil }
func (r *notebookBlockingChangeReader) Pause(context.Context, []string) error {
	return nil
}
func (r *notebookBlockingChangeReader) Resume(context.Context, []string) error {
	return nil
}
func (r *notebookBlockingChangeReader) Close(ctx context.Context) error {
	if _, ok := ctx.Deadline(); !ok {
		return errors.New("change stream close context has no deadline")
	}
	r.closeOnce.Do(func() { close(r.closed) })
	return nil
}

func TestNotebookSessionRecordScanEnforcesMaxRowsAndClosesCursor(t *testing.T) {
	service, catalog, _ := newNotebookCatalogSessionTestService(t, nil)
	public, _, err := service.Create(context.Background(), "addp_at_user", 7, 9, 14)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	session := &notebookRecordReadSession{
		records:     []map[string]interface{}{{"id": 1}, {"id": 2}, {"id": 3}, {"id": 4}},
		closeCalled: make(chan struct{}),
	}
	provider := &notebookNativeTestPlugin{recordSession: session}
	registerNotebookNativeTestPlugin(t, provider)
	catalog.engine = notebookNativeTestEngine(21)

	var destination bytes.Buffer
	err = service.StreamRecords(context.Background(), public.ID, storedKernelTokenForTest(t, service.items[public.ID]),
		NotebookRecordScanRequest{
			EngineID: 21, Path: notebookNativeTestPath(21), BatchSize: 100, MaxRows: 3,
		}, &destination, nil)
	if err != nil {
		t.Fatalf("StreamRecords() error = %v", err)
	}
	ids := notebookArrowDocumentIDs(t, destination.Bytes())
	if len(ids) != 3 || ids[0] != 1 || ids[2] != 3 {
		t.Fatalf("record ids = %#v", ids)
	}
	select {
	case <-session.closeCalled:
	default:
		t.Fatal("record cursor was not closed")
	}
}

func TestNotebookSessionGraphQueryForcesReadOnlyAndLimit(t *testing.T) {
	service, catalog, _ := newNotebookCatalogSessionTestService(t, nil)
	public, _, err := service.Create(context.Background(), "addp_at_user", 7, 9, 14)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	provider := &notebookNativeTestPlugin{}
	registerNotebookNativeTestPlugin(t, provider)
	catalog.engine = notebookNativeTestEngine(21)

	_, err = service.QueryGraph(context.Background(), public.ID, storedKernelTokenForTest(t, service.items[public.ID]),
		NotebookGraphQueryRequest{EngineID: 21, Query: "MATCH (n) RETURN n", MaxRows: 37, Timeout: 15 * time.Second})
	if err != nil {
		t.Fatalf("QueryGraph() error = %v", err)
	}
	if provider.graphQuery != "MATCH (n) RETURN n" || !provider.graphOptions.ReadOnly ||
		provider.graphOptions.Limit != 37 || provider.graphOptions.Timeout != 15*time.Second {
		t.Fatalf("graph request query=%q options=%#v", provider.graphQuery, provider.graphOptions)
	}
}

func TestNotebookSessionContentSelectsFullOrRangeProvider(t *testing.T) {
	service, catalog, _ := newNotebookCatalogSessionTestService(t, nil)
	public, _, err := service.Create(context.Background(), "addp_at_user", 7, 9, 14)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	provider := &notebookNativeTestPlugin{}
	registerNotebookNativeTestPlugin(t, provider)
	catalog.engine = notebookNativeTestEngine(21)
	token := storedKernelTokenForTest(t, service.items[public.ID])
	path := notebookNativeTestPath(21)

	var full bytes.Buffer
	if err := service.StreamContent(context.Background(), public.ID, token,
		NotebookContentReadRequest{EngineID: 21, Path: path}, &full, nil); err != nil {
		t.Fatalf("StreamContent(full) error = %v", err)
	}
	var partial bytes.Buffer
	if err := service.StreamContent(context.Background(), public.ID, token,
		NotebookContentReadRequest{EngineID: 21, Path: path, Range: &NotebookByteRange{Offset: 11, Length: 7}},
		&partial, nil); err != nil {
		t.Fatalf("StreamContent(range) error = %v", err)
	}
	if full.String() != "complete" || partial.String() != "range" || provider.contentCalls != 1 || provider.rangeCalls != 1 ||
		provider.rangeOptions.Offset != 11 || provider.rangeOptions.Length != 7 {
		t.Fatalf("full=%q range=%q calls=%d/%d options=%#v", full.String(), partial.String(),
			provider.contentCalls, provider.rangeCalls, provider.rangeOptions)
	}
}

func TestNotebookSessionCloseCancelsAndClosesChangeStream(t *testing.T) {
	service, catalog, _ := newNotebookCatalogSessionTestService(t, nil)
	public, _, err := service.Create(context.Background(), "addp_at_user", 7, 9, 14)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	reader := &notebookBlockingChangeReader{pollStarted: make(chan struct{}), closed: make(chan struct{})}
	provider := &notebookNativeTestPlugin{changeReader: reader}
	registerNotebookNativeTestPlugin(t, provider)
	catalog.engine = notebookNativeTestEngine(21)
	token := storedKernelTokenForTest(t, service.items[public.ID])

	streamErr := make(chan error, 1)
	go func() {
		streamErr <- service.StreamChanges(context.Background(), public.ID, token, NotebookChangeStreamRequest{
			EngineID: 21, Path: notebookNativeTestPath(21), InitialPosition: plugin.ChangeStreamInitialEarliest,
			BatchSize: 10, PollTimeout: time.Second,
		}, &bytes.Buffer{}, nil)
	}()
	select {
	case <-reader.pollStarted:
	case <-time.After(time.Second):
		t.Fatal("change stream did not start polling")
	}
	if err := service.Close(context.Background(), 7, 9, 14, public.ID); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	select {
	case err := <-streamErr:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("StreamChanges() error = %v, want context cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("change stream was not canceled")
	}
	select {
	case <-reader.closed:
	case <-time.After(time.Second):
		t.Fatal("change stream reader was not closed")
	}
}

func TestNotebookSessionQueryRejectsUnsupportedLanguage(t *testing.T) {
	service, catalog, _ := newNotebookCatalogSessionTestService(t, nil)
	public, _, err := service.Create(context.Background(), "addp_at_user", 7, 9, 14)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	provider := &notebookTestQueryPlugin{result: &plugin.QueryResult{}}
	previous, previousErr := plugin.Get("postgresql")
	plugin.Register(provider)
	t.Cleanup(func() {
		if previousErr == nil {
			plugin.Register(previous)
		} else {
			plugin.Unregister("postgresql")
		}
	})
	catalog.engine = &commonModels.Engine{ID: 21, EngineType: "postgresql"}

	err = service.StreamQuery(context.Background(), public.ID, storedKernelTokenForTest(t, service.items[public.ID]),
		NotebookQueryRequest{EngineID: 21, Language: "mql", Query: "{}", MaxRows: 10, Timeout: time.Second},
		&bytes.Buffer{}, nil)
	if !errors.Is(err, ErrNotebookQueryUnsupported) {
		t.Fatalf("StreamQuery() error = %v, want unsupported language", err)
	}
}

func registerNotebookNativeTestPlugin(t *testing.T, provider plugin.EnginePlugin) {
	t.Helper()
	previous, previousErr := plugin.Get(notebookNativeTestEngineType)
	plugin.Register(provider)
	t.Cleanup(func() {
		if previousErr == nil {
			plugin.Register(previous)
		} else {
			plugin.Unregister(notebookNativeTestEngineType)
		}
	})
}

func notebookNativeTestEngine(id uint) *commonModels.Engine {
	return &commonModels.Engine{ID: id, EngineType: notebookNativeTestEngineType, ConnectionInfo: commonModels.ConnectionInfo{"endpoint": "test"}}
}

func notebookNativeTestPath(engineID uint) commonClient.EngineCatalogPath {
	return commonClient.EngineCatalogPath{
		Version: plugin.CatalogPathVersion, EngineID: engineID,
		Segments: []commonClient.EngineCatalogSegment{{Term: "server", Kind: "server", Name: "root"}, {Term: "item", Kind: "item", Name: "target"}},
	}
}

func notebookArrowDocumentIDs(t *testing.T, payload []byte) []int {
	t.Helper()
	reader, err := ipc.NewReader(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("open record Arrow stream: %v", err)
	}
	defer reader.Release()
	var ids []int
	for reader.Next() {
		column, ok := reader.Record().Column(0).(*array.String)
		if !ok {
			t.Fatalf("record Arrow column type = %T", reader.Record().Column(0))
		}
		for index := 0; index < column.Len(); index++ {
			var document struct {
				ID int `json:"id"`
			}
			if err := json.Unmarshal([]byte(column.Value(index)), &document); err != nil {
				t.Fatalf("decode record document: %v", err)
			}
			ids = append(ids, document.ID)
		}
	}
	if err := reader.Err(); err != nil {
		t.Fatalf("read record Arrow stream: %v", err)
	}
	return ids
}
