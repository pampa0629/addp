package service

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/google/uuid"
)

var (
	ErrNotebookRecordScanInvalid       = errors.New("notebook record scan request is invalid")
	ErrNotebookRecordScanUnsupported   = errors.New("notebook record scan is unsupported by this engine")
	ErrNotebookGraphRequestInvalid     = errors.New("notebook graph request is invalid")
	ErrNotebookGraphUnsupported        = errors.New("notebook graph operation is unsupported by this engine")
	ErrNotebookContentReadInvalid      = errors.New("notebook content read request is invalid")
	ErrNotebookContentReadUnsupported  = errors.New("notebook content read is unsupported by this engine")
	ErrNotebookChangeStreamInvalid     = errors.New("notebook change stream request is invalid")
	ErrNotebookChangeStreamUnsupported = errors.New("notebook change stream is unsupported by this engine")
)

type NotebookRecordScanRequest struct {
	EngineID  uint
	Path      commonClient.EngineCatalogPath
	BatchSize int
	MaxRows   int64
}

type NotebookGraphSampleRequest struct {
	EngineID uint
	Path     commonClient.EngineCatalogPath
	Limit    int
	Timeout  time.Duration
}

type NotebookGraphQueryRequest struct {
	EngineID uint
	Query    string
	MaxRows  int
	Timeout  time.Duration
}

type NotebookByteRange struct {
	Offset int64
	Length int64
}

type NotebookContentReadRequest struct {
	EngineID uint
	Path     commonClient.EngineCatalogPath
	Range    *NotebookByteRange
}

type NotebookChangeStreamRequest struct {
	EngineID        uint
	Path            commonClient.EngineCatalogPath
	InitialPosition string
	Positions       map[string]int64
	BatchSize       int
	PollTimeout     time.Duration
}

type notebookEngineExecution struct {
	ctx         context.Context
	session     *NotebookSession
	executionID string
	access      *commonClient.ExecutionEngineAccess
	plugin      plugin.EnginePlugin
	cancel      context.CancelFunc
	service     *NotebookSessionService
}

func (s *NotebookSessionService) beginNotebookEngineExecution(
	ctx context.Context, sessionID, token string, engineID uint,
) (*notebookEngineExecution, error) {
	session, err := s.ResolveKernelCapability(sessionID, token)
	if err != nil {
		return nil, err
	}
	executionID := uuid.NewString()
	executionCtx, cancel, err := s.registerNotebookExecution(ctx, session.ID, executionID)
	if err != nil {
		return nil, err
	}
	expiresIn := int64(time.Until(session.ExpiresAt).Seconds())
	if expiresIn <= 0 {
		cancel()
		s.unregisterNotebookExecution(session.ID, executionID)
		return nil, ErrNotebookSessionNotFound
	}
	access, err := s.catalog.DeriveExecutionEngineAccess(executionCtx, session.TenantID, session.SessionAuthorizationID,
		commonClient.NotebookExecutionEngineAccessRequest{
			SessionID: session.ID, ExecutionID: executionID, EngineID: engineID, ExpiresIn: expiresIn,
		})
	if err != nil {
		cancel()
		s.unregisterNotebookExecution(session.ID, executionID)
		return nil, err
	}
	if access.Engine == nil || access.Engine.ID != engineID || strings.TrimSpace(access.Engine.EngineType) == "" {
		cancel()
		s.unregisterNotebookExecution(session.ID, executionID)
		return nil, fmt.Errorf("notebook execution engine access is invalid")
	}
	enginePlugin, err := plugin.Get(access.Engine.EngineType)
	if err != nil {
		cancel()
		s.unregisterNotebookExecution(session.ID, executionID)
		return nil, err
	}
	return &notebookEngineExecution{
		ctx: executionCtx, session: session, executionID: executionID,
		access: access, plugin: enginePlugin, cancel: cancel, service: s,
	}, nil
}

func (e *notebookEngineExecution) Close() {
	if e == nil {
		return
	}
	e.cancel()
	e.service.unregisterNotebookExecution(e.session.ID, e.executionID)
}

func (e *notebookEngineExecution) ValidateLease() error {
	_, err := e.service.catalog.ValidateExecutionEngineAccess(e.ctx, e.session.TenantID, e.access.AuthorizationID,
		commonClient.ExecutionEngineAccessRequest{
			ExecutionID: e.executionID, EngineID: e.access.EngineID, RequiredEffects: []string{"read"},
		})
	return err
}

func (s *NotebookSessionService) StreamRecords(
	ctx context.Context, sessionID, token string, request NotebookRecordScanRequest,
	destination io.Writer, ready func(),
) error {
	if request.EngineID == 0 || request.BatchSize <= 0 || request.BatchSize > 1_000_000 || request.MaxRows < 0 ||
		!validNotebookCatalogPath(request.EngineID, request.Path) || destination == nil {
		return ErrNotebookRecordScanInvalid
	}
	execution, err := s.beginNotebookEngineExecution(ctx, sessionID, token, request.EngineID)
	if err != nil {
		return err
	}
	defer execution.Close()
	provider, ok := execution.plugin.(plugin.RecordReadSessionProvider)
	if !ok {
		return ErrNotebookRecordScanUnsupported
	}
	readSession, err := provider.OpenRecordReadSession(execution.ctx,
		plugin.ConnectionInfo(execution.access.Engine.ConnectionInfo), notebookPluginCatalogPath(request.Path),
		plugin.RecordReadSessionOptions{})
	if err != nil {
		return err
	}
	defer closeNotebookRecordReadSession(readSession)

	fields := []datatype.FieldInfo{{Name: "document", Type: datatype.FieldTypeJSON, Nullable: false}}
	writer, err := plugin.NewBatchArrowStreamWriter(destination, fields)
	if err != nil {
		return err
	}
	if ready != nil {
		ready()
	}
	rowsRead := int64(0)
	nextLeaseCheck := time.Now().Add(notebookExecutionLeaseCheckInterval)
	for request.MaxRows == 0 || rowsRead < request.MaxRows {
		if !time.Now().Before(nextLeaseCheck) {
			if err := execution.ValidateLease(); err != nil {
				_ = writer.Close()
				return err
			}
			nextLeaseCheck = time.Now().Add(notebookExecutionLeaseCheckInterval)
		}
		limit := notebookScanBatchLimit(request.BatchSize, request.MaxRows, rowsRead)
		batch, err := readSession.ReadBatch(execution.ctx, limit)
		if err != nil {
			_ = writer.Close()
			return err
		}
		if batch == nil || len(batch.Records) == 0 {
			break
		}
		if remaining := request.MaxRows - rowsRead; request.MaxRows > 0 && int64(len(batch.Records)) > remaining {
			batch.Records = batch.Records[:remaining]
		}
		rows := make([]map[string]interface{}, len(batch.Records))
		for index, record := range batch.Records {
			rows[index] = map[string]interface{}{"document": record}
		}
		if err := writer.WriteBatch(&plugin.BatchData{Fields: fields, Rows: rows, Offset: rowsRead}); err != nil {
			_ = writer.Close()
			return err
		}
		notifyNotebookStreamFlush(destination)
		rowsRead += int64(len(rows))
	}
	return writer.Close()
}

func (s *NotebookSessionService) SampleGraph(
	ctx context.Context, sessionID, token string, request NotebookGraphSampleRequest,
) (*plugin.GraphData, error) {
	if request.EngineID == 0 || request.Limit <= 0 || request.Limit > 10_000 || request.Timeout <= 0 ||
		request.Timeout > 5*time.Minute || !validNotebookCatalogPath(request.EngineID, request.Path) {
		return nil, ErrNotebookGraphRequestInvalid
	}
	queryCtx, cancel := context.WithTimeout(ctx, request.Timeout)
	defer cancel()
	execution, err := s.beginNotebookEngineExecution(queryCtx, sessionID, token, request.EngineID)
	if err != nil {
		return nil, err
	}
	defer execution.Close()
	provider, ok := execution.plugin.(plugin.GraphSampleProvider)
	if !ok {
		return nil, ErrNotebookGraphUnsupported
	}
	return provider.SampleGraph(execution.ctx, plugin.ConnectionInfo(execution.access.Engine.ConnectionInfo),
		notebookPluginCatalogPath(request.Path), plugin.GraphSampleOptions{Limit: request.Limit})
}

func (s *NotebookSessionService) QueryGraph(
	ctx context.Context, sessionID, token string, request NotebookGraphQueryRequest,
) (*plugin.GraphQueryResult, error) {
	if request.EngineID == 0 || strings.TrimSpace(request.Query) == "" || request.MaxRows <= 0 ||
		request.MaxRows > 1_000_000 || request.Timeout <= 0 || request.Timeout > 5*time.Minute {
		return nil, ErrNotebookGraphRequestInvalid
	}
	queryCtx, cancel := context.WithTimeout(ctx, request.Timeout)
	defer cancel()
	execution, err := s.beginNotebookEngineExecution(queryCtx, sessionID, token, request.EngineID)
	if err != nil {
		return nil, err
	}
	defer execution.Close()
	provider, ok := execution.plugin.(plugin.GraphQueryProvider)
	if !ok {
		return nil, ErrNotebookGraphUnsupported
	}
	return provider.ExecuteGraphQuery(execution.ctx, plugin.ConnectionInfo(execution.access.Engine.ConnectionInfo),
		request.Query, plugin.QueryOptions{Limit: request.MaxRows, Timeout: request.Timeout, ReadOnly: true})
}

func (s *NotebookSessionService) StreamContent(
	ctx context.Context, sessionID, token string, request NotebookContentReadRequest,
	destination io.Writer, ready func(),
) error {
	if request.EngineID == 0 || !validNotebookCatalogPath(request.EngineID, request.Path) || destination == nil ||
		(request.Range != nil && (request.Range.Offset < 0 || request.Range.Length <= 0)) {
		return ErrNotebookContentReadInvalid
	}
	execution, err := s.beginNotebookEngineExecution(ctx, sessionID, token, request.EngineID)
	if err != nil {
		return err
	}
	defer execution.Close()
	var reader io.ReadCloser
	path := notebookPluginCatalogPath(request.Path)
	connInfo := plugin.ConnectionInfo(execution.access.Engine.ConnectionInfo)
	if request.Range == nil {
		provider, ok := execution.plugin.(plugin.ContentReadableProvider)
		if !ok {
			return ErrNotebookContentReadUnsupported
		}
		reader, err = provider.OpenContent(execution.ctx, connInfo, path, plugin.ReadOptions{})
	} else {
		provider, ok := execution.plugin.(plugin.RangeReadableProvider)
		if !ok {
			return ErrNotebookContentReadUnsupported
		}
		reader, err = provider.OpenRange(execution.ctx, connInfo, path, plugin.ReadOptions{
			Offset: request.Range.Offset, Length: request.Range.Length,
		})
	}
	if err != nil {
		return err
	}
	defer reader.Close()
	if ready != nil {
		ready()
	}
	buffer := make([]byte, 64*1024)
	nextLeaseCheck := time.Now().Add(notebookExecutionLeaseCheckInterval)
	for {
		if !time.Now().Before(nextLeaseCheck) {
			if err := execution.ValidateLease(); err != nil {
				return err
			}
			nextLeaseCheck = time.Now().Add(notebookExecutionLeaseCheckInterval)
		}
		count, readErr := reader.Read(buffer)
		if count > 0 {
			if _, err := destination.Write(buffer[:count]); err != nil {
				return err
			}
			notifyNotebookStreamFlush(destination)
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

func (s *NotebookSessionService) StreamChanges(
	ctx context.Context, sessionID, token string, request NotebookChangeStreamRequest,
	destination io.Writer, ready func(),
) error {
	initial := strings.ToLower(strings.TrimSpace(request.InitialPosition))
	if request.EngineID == 0 || !validNotebookCatalogPath(request.EngineID, request.Path) || destination == nil ||
		request.BatchSize <= 0 || request.BatchSize > 10_000 || request.PollTimeout <= 0 || request.PollTimeout > time.Minute ||
		(initial != plugin.ChangeStreamInitialEarliest && initial != plugin.ChangeStreamInitialLatest) {
		return ErrNotebookChangeStreamInvalid
	}
	positions := make(map[string]plugin.ChangeStreamPosition, len(request.Positions))
	for partition, nextOffset := range request.Positions {
		partition = strings.TrimSpace(partition)
		if partition == "" || nextOffset < 0 {
			return ErrNotebookChangeStreamInvalid
		}
		positions[partition] = plugin.ChangeStreamPosition{
			Type: plugin.ChangeStreamPositionTypeKafkaOffset, Version: plugin.ChangeStreamPositionVersionV1,
			Partition: partition, Values: map[string]string{"next_offset": strconv.FormatInt(nextOffset, 10)},
		}
	}
	execution, err := s.beginNotebookEngineExecution(ctx, sessionID, token, request.EngineID)
	if err != nil {
		return err
	}
	defer execution.Close()
	provider, ok := execution.plugin.(plugin.ChangeStreamReaderProvider)
	if !ok {
		return ErrNotebookChangeStreamUnsupported
	}
	reader, err := provider.OpenChangeStream(execution.ctx, plugin.ConnectionInfo(execution.access.Engine.ConnectionInfo),
		notebookPluginCatalogPath(request.Path), plugin.ChangeStreamReadOptions{
			ConsumerGroup:      "addp-notebook-" + execution.executionID,
			CommittedPositions: positions, InitialPosition: initial, PollTimeout: request.PollTimeout,
		})
	if err != nil {
		return err
	}
	defer closeNotebookChangeStream(reader)
	if ready != nil {
		ready()
	}
	buffered := bufio.NewWriter(destination)
	encoder := json.NewEncoder(buffered)
	nextLeaseCheck := time.Now().Add(notebookExecutionLeaseCheckInterval)
	for {
		if !time.Now().Before(nextLeaseCheck) {
			if err := execution.ValidateLease(); err != nil {
				return err
			}
			nextLeaseCheck = time.Now().Add(notebookExecutionLeaseCheckInterval)
		}
		batch, err := reader.Poll(execution.ctx, request.BatchSize)
		if err != nil {
			return err
		}
		if batch == nil || len(batch.Records) == 0 {
			continue
		}
		for _, record := range batch.Records {
			if err := encoder.Encode(record); err != nil {
				return err
			}
		}
		if err := buffered.Flush(); err != nil {
			return err
		}
		notifyNotebookStreamFlush(destination)
	}
}

func validNotebookCatalogPath(engineID uint, path commonClient.EngineCatalogPath) bool {
	return engineID > 0 && path.EngineID == engineID && path.Version == plugin.CatalogPathVersion && len(path.Segments) > 0
}

func closeNotebookRecordReadSession(session plugin.RecordReadSession) {
	if session == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), notebookReadSessionCloseTimeout)
	defer cancel()
	_ = session.Close(ctx)
}

func closeNotebookChangeStream(reader plugin.ChangeStreamReader) {
	if reader == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), notebookReadSessionCloseTimeout)
	defer cancel()
	_ = reader.Close(ctx)
}
