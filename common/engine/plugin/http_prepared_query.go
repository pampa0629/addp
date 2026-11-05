package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/google/uuid"
)

type remotePreparedQuery struct {
	PreparedQuery
	models map[uint]EngineCatalogModelSpec
	close  func(context.Context) error
}

func (p *remotePreparedQuery) SourceCatalogModels() map[uint]EngineCatalogModelSpec {
	result := make(map[uint]EngineCatalogModelSpec, len(p.models))
	for id, model := range p.models {
		result[id] = model
	}
	return result
}
func (p *remotePreparedQuery) Close(ctx context.Context) error { return p.close(ctx) }

// HTTPPrepareFederatedQuery accepts facts only from the authenticated Runtime.
// The response is not a caller-supplied resource selection or authorization.
func HTTPPrepareFederatedQuery(ctx context.Context, conn ConnectionInfo, req FederatedQueryRequest) (PreparedFederatedQuery, error) {
	base, err := RuntimeBaseURL(conn)
	if err != nil {
		return nil, err
	}
	if req.CallerAccessToken == "" {
		return nil, fmt.Errorf("query runtime caller required")
	}
	timeout := req.Options.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	client := &http.Client{Timeout: timeout + 5*time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	call := func(ctx context.Context, method, path string, body interface{}) ([]byte, int, error) {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		request, err := http.NewRequestWithContext(ctx, method, base+path, bytes.NewReader(raw))
		if err != nil {
			return nil, 0, err
		}
		request.Header.Set("Authorization", "Bearer "+req.CallerAccessToken)
		request.Header.Set("Accept-Language", commoni18n.Language(ctx))
		request.Header.Set("Content-Type", "application/json")
		response, err := client.Do(request)
		if err != nil {
			return nil, 0, err
		}
		defer response.Body.Close()
		raw, err = io.ReadAll(io.LimitReader(response.Body, 4*1024*1024+1))
		if err != nil || len(raw) > 4*1024*1024 {
			return nil, 0, fmt.Errorf("query runtime output invalid")
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return raw, response.StatusCode, fmt.Errorf("query runtime returned HTTP %d", response.StatusCode)
		}
		return raw, response.StatusCode, nil
	}
	raw, _, err := call(ctx, http.MethodPost, "/api/v1/queries", req)
	if err != nil {
		return nil, err
	}
	var facts struct {
		QueryID string `json:"query_id"`
		Sources []struct {
			ID   uint   `json:"engine_id"`
			Type string `json:"engine_type"`
		} `json:"sources"`
		Scope *QueryEngineReadScope `json:"engine_read_scope"`
		Reads *QueryReadSet         `json:"read_set"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&facts); err != nil {
		return nil, err
	}
	if decoder.Decode(new(interface{})) != io.EOF {
		return nil, fmt.Errorf("query runtime output invalid")
	}
	id, err := uuid.Parse(facts.QueryID)
	if err != nil || id == uuid.Nil || id.String() != facts.QueryID {
		return nil, fmt.Errorf("query runtime query identity invalid")
	}
	path := "/api/v1/queries/" + id.String()
	var once sync.Once
	close := func(ctx context.Context) (err error) {
		once.Do(func() { _, _, err = call(ctx, http.MethodDelete, path, nil) })
		return
	}
	failed := true
	defer func() {
		if failed {
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = close(cleanup)
		}
	}()
	allowed := make(map[uint]bool, len(req.SourceEngineIDs))
	for _, id := range req.SourceEngineIDs {
		allowed[id] = true
	}
	models := make(map[uint]EngineCatalogModelSpec, len(facts.Sources))
	for _, source := range facts.Sources {
		if !allowed[source.ID] {
			return nil, fmt.Errorf("query runtime source exceeds authorization")
		}
		if _, exists := models[source.ID]; exists {
			return nil, fmt.Errorf("query runtime duplicate source")
		}
		engine, err := Get(source.Type)
		if err != nil {
			return nil, err
		}
		catalog, ok := engine.(EngineCatalogModelProvider)
		if !ok {
			return nil, ErrQueryReadSetUnresolved
		}
		models[source.ID] = catalog.EngineCatalogModel()
	}
	if len(models) != len(allowed) {
		return nil, fmt.Errorf("query runtime sources incomplete")
	}
	if facts.Scope != nil {
		if err := facts.Scope.Validate(); err != nil {
			return nil, err
		}
		for _, id := range facts.Scope.EngineIDs {
			if !allowed[id] {
				return nil, ErrQueryEngineReadScopeUnresolved
			}
		}
	}
	if facts.Reads != nil {
		normalized, err := NewQueryReadSet(facts.Reads.Paths...)
		if err != nil {
			return nil, err
		}
		if !sameQueryReadPaths(facts.Reads.Paths, normalized.Paths) {
			return nil, ErrQueryReadSetUnresolved
		}
		for _, p := range facts.Reads.Paths {
			if !allowed[p.EngineID] || facts.Scope == nil || !facts.Scope.Contains(p.EngineID) {
				return nil, ErrQueryReadSetUnresolved
			}
		}
	}
	analysis, _ := NewQueryAnalysis(req.Language, QuerySchemaCoverageUnknown)
	prepared, err := NewPreparedQuery(analysis,
		func(context.Context) (*QueryEngineReadScope, error) {
			if facts.Scope == nil {
				return nil, ErrQueryEngineReadScopeUnresolved
			}
			return facts.Scope, nil
		},
		func(context.Context) (*QueryReadSet, error) {
			if facts.Reads == nil {
				return nil, ErrQueryReadSetUnresolved
			}
			return facts.Reads, nil
		}, nil,
		func(ctx context.Context) (*QueryResult, error) {
			raw, status, err := call(ctx, http.MethodPost, path+"/execute", struct{}{})
			if err != nil {
				if status == http.StatusBadRequest && isPreparedQueryMissingColumn(raw) {
					return nil, NewQueryError(QueryErrorCodeColumnNotFound, nil)
				}
				return nil, err
			}
			return decodeFederatedQueryResult(status, raw)
		})
	if err != nil {
		return nil, err
	}
	failed = false
	return &remotePreparedQuery{PreparedQuery: prepared, models: models, close: close}, nil
}

func isPreparedQueryMissingColumn(raw []byte) bool {
	var failure struct {
		Code    string `json:"error_code"`
		Message string `json:"error"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(&failure) == nil && decoder.Decode(new(interface{})) == io.EOF &&
		failure.Code == "query_column_not_found" && failure.Message != ""
}
