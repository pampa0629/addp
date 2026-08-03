package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	commonclient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/sqleffect"
	duckdb "github.com/addp/engines/duckdb/internal/duckdb"
)

type Executor struct {
	system             *commonclient.SystemServiceClient
	meta               *commonclient.MetaClient
	maxRows            int
	maxMemory          string
	threads            int
	defaultTimeout     time.Duration
	sourceLoopbackHost string
}

func NewExecutor(
	system *commonclient.SystemServiceClient,
	meta *commonclient.MetaClient,
	maxRows int,
	maxMemory string,
	threads int,
	defaultTimeout time.Duration,
	sourceLoopbackHost string,
) *Executor {
	return &Executor{
		system: system, meta: meta, maxRows: maxRows, maxMemory: maxMemory,
		threads: threads, defaultTimeout: defaultTimeout, sourceLoopbackHost: strings.TrimSpace(sourceLoopbackHost),
	}
}

func (e *Executor) Execute(
	ctx context.Context,
	tenantID uint,
	req plugin.FederatedQueryRequest,
) (*plugin.QueryResult, error) {
	if e == nil || e.system == nil || e.meta == nil || tenantID == 0 {
		return nil, errors.New("DuckDB runtime is not initialized")
	}
	if req.Language != "sql" || !req.Options.ReadOnly {
		return nil, errors.New("DuckDB runtime only accepts read-only SQL")
	}
	if err := sqleffect.RequireReadOnly(req.Query); err != nil {
		return nil, err
	}
	if len(req.SourceEngineIDs) == 0 {
		return nil, errors.New("federated query requires at least one source engine")
	}
	timeout := req.Options.Timeout
	if timeout <= 0 || timeout > e.defaultTimeout {
		timeout = e.defaultTimeout
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	engines := make([]commonmodels.Engine, 0, len(req.SourceEngineIDs))
	for _, engineID := range req.SourceEngineIDs {
		access, err := e.system.WithTenantID(tenantID).GetExecutionEngineAccess(execCtx, req.ExecutionAuthorizationID, commonclient.ExecutionEngineAccessRequest{
			ExecutionID:     req.ExecutionID,
			EngineID:        strconv.FormatUint(uint64(engineID), 10),
			RequiredEffects: []string{"read"},
		})
		if err != nil {
			return nil, fmt.Errorf("authorize source engine %d: %w", engineID, err)
		}
		if access.Audience != "duckdb" || access.Engine == nil || !duckdb.SupportsMount(access.Engine.EngineType) {
			return nil, fmt.Errorf("source engine %d is not supported by DuckDB runtime", engineID)
		}
		engines = append(engines, mapSourceEngineLoopbackAddress(*access.Engine, e.sourceLoopbackHost))
	}

	objectTables := req.ObjectTables
	if objectTables == nil {
		objectTables = duckdb.BuildObjectTableMap(execCtx, tenantID, engines, e.meta)
	}
	session, err := duckdb.PrepareFederatedQueryWithEngines(execCtx, req.Query, engines, objectTables, e.sessionOptions(req.Options))
	if err != nil {
		return nil, err
	}
	defer session.Close()
	query := runtimeQuery(session.RewrittenSQL, req.Options, e.maxRows)
	args, err := normalizeQueryArgs(req.Options.Args)
	if err != nil {
		return nil, err
	}
	result, err := duckdb.ExecuteQuery(execCtx, session.Conn, query, args...)
	if err != nil {
		return nil, err
	}
	return &plugin.QueryResult{Columns: result.Columns, Rows: result.Rows}, nil
}

func (e *Executor) sessionOptions(options plugin.QueryOptions) duckdb.FederatedSessionOptions {
	return duckdb.FederatedSessionOptions{
		MemoryLimit: e.maxMemory,
		Threads:     e.threads,
		LoadSpatial: options.Spatial,
	}
}

func normalizeQueryArgs(args []interface{}) ([]interface{}, error) {
	result := make([]interface{}, len(args))
	for index, value := range args {
		number, ok := value.(json.Number)
		if !ok {
			result[index] = value
			continue
		}
		if strings.ContainsAny(number.String(), ".eE") {
			parsed, err := number.Float64()
			if err != nil {
				return nil, fmt.Errorf("invalid numeric query argument: %w", err)
			}
			result[index] = parsed
			continue
		}
		parsed, err := number.Int64()
		if err != nil {
			return nil, fmt.Errorf("invalid integer query argument: %w", err)
		}
		result[index] = parsed
	}
	return result, nil
}

func runtimeQuery(rewrittenSQL string, options plugin.QueryOptions, maxRows int) string {
	inner := strings.TrimSuffix(strings.TrimSpace(rewrittenSQL), ";")
	if options.Describe {
		return "DESCRIBE " + inner
	}
	limit := options.Limit
	if limit <= 0 || limit > maxRows {
		limit = maxRows
	}
	return fmt.Sprintf("SELECT * FROM (%s) AS addp_query LIMIT %d OFFSET %d", inner, limit, max(options.Offset, 0))
}
