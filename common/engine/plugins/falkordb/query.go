package falkordb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	sdk "github.com/FalkorDB/falkordb-go/v2"
	"github.com/addp/common/engine/plugin"
	commonquery "github.com/addp/common/query"
	"github.com/addp/common/query/cypherread"
	rdb "github.com/redis/go-redis/v9"
)

var errLocalRead = plugin.NewQueryError(plugin.QueryErrorCodeLocalReadUnsupported, errors.New("FalkorDB query requires a certified local graph, reviewed built-in functions and supported read-only openCypher syntax"))

// Reviewed against v4.20.6 arithmetic/builtin_funcs.c and func_desc.c:
// built-ins are resolved before UDFs, so a UDF cannot shadow these functions.
// No procedure, UDF or external data function is admitted by name guessing.
var localBuiltins = map[string]bool{}

func init() {
	for _, n := range strings.Fields("id labels type typeof properties keys head last size length nodes relationships startnode endnode exists coalesce count sum avg min max collect distinct tointeger tofloat tostring toboolean abs ceil floor round sign sqrt log log10 exp e pi rand range reverse tail trim ltrim rtrim tolower toupper left right substring replace split contains percentilecont percentiledisc stdev stdevp randomuuid timestamp all any none single isempty") {
		localBuiltins[n] = true
	}
}

func (p *FalkorDBPlugin) prepare(c plugin.ConnectionInfo, req plugin.QueryRequest) (plugin.ConnectionInfo, string, string, map[string]interface{}, int, error) {
	if req.EngineID == 0 || req.Language != "cypher" || req.TargetPath == nil || req.TargetPath.EngineID != req.EngineID {
		return nil, "", "", nil, 0, errLocalRead
	}
	if err := p.ValidateConnectionInfo(c); err != nil {
		return nil, "", "", nil, 0, err
	}
	name, err := p.graphName(*req.TargetPath)
	if err != nil {
		return nil, "", "", nil, 0, err
	}
	proof, err := cypherread.ParseLocalRead(req.Query)
	if err != nil {
		return nil, "", "", nil, 0, errLocalRead
	}
	for _, fn := range proof.Functions {
		if !localBuiltins[strings.ToLower(fn)] {
			return nil, "", "", nil, 0, errLocalRead
		}
	}
	if err := commonquery.ValidateCypher(req.Query, req.Options.Parameters); err != nil {
		return nil, "", "", nil, 0, err
	}
	params, err := normalizeParameters(req.Options.Parameters)
	if err != nil {
		return nil, "", "", nil, 0, err
	}
	limit := req.Options.Limit
	if limit <= 0 {
		limit = 1000
	}
	if limit > 10000 {
		return nil, "", "", nil, 0, fmt.Errorf("FalkorDB query limit exceeds 10000")
	}
	query, err := proof.BoundedStatement(limit, params)
	if err != nil {
		return nil, "", "", nil, 0, errLocalRead
	}
	frozen := plugin.ConnectionInfo{}
	for k, v := range c {
		frozen[k] = v
	}
	return frozen, name, query, params, limit, nil
}
func (p *FalkorDBPlugin) PrepareGraphQuery(_ context.Context, c plugin.ConnectionInfo, req plugin.QueryRequest) (plugin.PreparedGraphQuery, error) {
	conn, name, query, params, limit, err := p.prepare(c, req)
	if err != nil {
		return nil, err
	}
	engineID := req.EngineID
	timeout := req.Options.Timeout
	path := p.graphPath(engineID, name)
	analysis, _ := plugin.NewQueryAnalysis("cypher", plugin.QuerySchemaCoverageUnknown)
	resolve := func(ctx context.Context) (*plugin.QueryEngineReadScope, error) {
		ctx, cancel := operationContext(ctx, timeout)
		defer cancel()
		client, close, err := p.open(ctx, conn)
		if err != nil {
			return nil, err
		}
		defer close()
		if err = verifyLocal(ctx, client, name); err != nil {
			return nil, err
		}
		return plugin.NewQueryEngineReadScope(engineID)
	}
	readSet := func(context.Context) (*plugin.QueryReadSet, error) { return plugin.NewQueryReadSet(path) }
	return plugin.NewPreparedGraphQuery(analysis, resolve, readSet, func(ctx context.Context) (*plugin.GraphQueryResult, error) {
		ctx, cancel := operationContext(ctx, timeout)
		defer cancel()
		client, close, err := p.open(ctx, conn)
		if err != nil {
			return nil, err
		}
		defer close()
		if err = verifyLocal(ctx, client, name); err != nil {
			return nil, err
		}
		return runQuery(ctx, client, name, query, params, limit, true)
	})
}
func (p *FalkorDBPlugin) PrepareQuery(ctx context.Context, c plugin.ConnectionInfo, req plugin.QueryRequest) (plugin.PreparedQuery, error) {
	graph, err := p.PrepareGraphQuery(ctx, c, req)
	if err != nil {
		return nil, err
	}
	analysis, err := graph.Analysis(ctx)
	if err != nil {
		return nil, err
	}
	return plugin.NewPreparedQuery(analysis, graph.EngineReadScope, graph.ReadSet, nil, func(ctx context.Context) (*plugin.QueryResult, error) {
		result, err := graph.ExecuteGraph(ctx)
		if err != nil {
			return nil, err
		}
		return &result.QueryResult, nil
	})
}
func verifyLocal(ctx context.Context, client *rdb.Client, name string) error {
	version, err := moduleVersion(ctx, client)
	if err != nil {
		return err
	}
	if version != 42006 {
		return errLocalRead
	}
	return graphExists(ctx, client, name)
}
func (p *FalkorDBPlugin) ExecuteGraphQueryAtPath(ctx context.Context, c plugin.ConnectionInfo, path plugin.EngineCatalogPath, query string, opts plugin.QueryOptions) (*plugin.GraphQueryResult, error) {
	ctx, cancel := operationContext(ctx, opts.Timeout)
	defer cancel()
	if opts.ReadOnly {
		plan, err := p.PrepareGraphQuery(ctx, c, plugin.QueryRequest{EngineID: path.EngineID, Language: "cypher", Query: query, TargetPath: &path, Options: opts})
		if err != nil {
			return nil, err
		}
		return plan.ExecuteGraph(ctx)
	}
	name, err := p.graphName(path)
	if err != nil {
		return nil, err
	}
	if err = commonquery.ValidateCypher(query, opts.Parameters); err != nil {
		return nil, err
	}
	params, err := normalizeParameters(opts.Parameters)
	if err != nil {
		return nil, err
	}
	client, close, err := p.open(ctx, c)
	if err != nil {
		return nil, err
	}
	defer close()
	return runQuery(ctx, client, name, query, params, opts.Limit, false)
}
func (p *FalkorDBPlugin) ExecuteGraphQuery(ctx context.Context, c plugin.ConnectionInfo, query string, opts plugin.QueryOptions) (*plugin.GraphQueryResult, error) {
	name := plugin.GetString(c, "graph")
	if name == "" || opts.EngineID == 0 {
		return nil, fmt.Errorf("FalkorDB requires an explicit target graph")
	}
	return p.ExecuteGraphQueryAtPath(ctx, c, p.graphPath(opts.EngineID, name), query, opts)
}
func (*FalkorDBPlugin) GenerateSampleQuery(_ context.Context, _ plugin.ConnectionInfo, _ plugin.SampleQueryOptions) (string, string) {
	return "MATCH (n) RETURN n LIMIT 10", "cypher"
}

var identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func normalizeParameters(params map[string]interface{}) (map[string]interface{}, error) {
	normalized := make(map[string]interface{}, len(params))
	for k, v := range params {
		if !identifier.MatchString(k) {
			return nil, fmt.Errorf("invalid Cypher parameter identifier")
		}
		value, err := normalizeValue(v, 0)
		if err != nil {
			return nil, err
		}
		normalized[k] = value
	}
	encoded, err := json.Marshal(normalized)
	if err != nil || len(encoded) > 1<<20 {
		return nil, fmt.Errorf("FalkorDB parameter budget exceeded")
	}
	return normalized, nil
}
func normalizeValue(v interface{}, depth int) (interface{}, error) {
	if depth > 16 {
		return nil, fmt.Errorf("Cypher parameter nesting exceeded")
	}
	if v == nil {
		return nil, nil
	}
	switch v := v.(type) {
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return n, nil
		}
		n, err := v.Float64()
		if err != nil || math.IsInf(n, 0) || math.IsNaN(n) {
			return nil, fmt.Errorf("invalid Cypher number")
		}
		return n, nil
	case map[string]interface{}:
		m := map[string]interface{}{}
		for k, item := range v {
			if !identifier.MatchString(k) {
				return nil, fmt.Errorf("invalid Cypher map key")
			}
			n, err := normalizeValue(item, depth+1)
			if err != nil {
				return nil, err
			}
			m[k] = n
		}
		return m, nil
	}
	value := reflect.ValueOf(v)
	switch value.Kind() {
	case reflect.String:
		return value.String(), nil
	case reflect.Bool:
		return value.Bool(), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return value.Int(), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n := value.Uint()
		if n > math.MaxInt64 {
			return nil, fmt.Errorf("Cypher integer overflow")
		}
		return int64(n), nil
	case reflect.Float32, reflect.Float64:
		n := value.Float()
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return nil, fmt.Errorf("invalid Cypher number")
		}
		return n, nil
	case reflect.Slice, reflect.Array:
		items := make([]interface{}, value.Len())
		for i := range items {
			n, err := normalizeValue(value.Index(i).Interface(), depth+1)
			if err != nil {
				return nil, err
			}
			items[i] = n
		}
		return items, nil
	}
	return nil, fmt.Errorf("unsupported Cypher parameter type")
}

// The official decoder resolves labels and property keys through this wrapper.
// Its SDK uses Background internally; all I/O is rebound to the request context.
type contextClient struct {
	rdb.UniversalClient
	ctx context.Context
}

func (c contextClient) Do(_ context.Context, args ...interface{}) *rdb.Cmd {
	// The SDK resolves compact label/property IDs through fixed CALL metadata
	// queries. Keep those reads on the native read-only command as well.
	if len(args) > 0 && args[0] == "GRAPH.QUERY" {
		args = append([]interface{}{}, args...)
		args[0] = "GRAPH.RO_QUERY"
	}
	return c.UniversalClient.Do(c.ctx, args...)
}

func runQuery(ctx context.Context, client *rdb.Client, name, query string, params map[string]interface{}, limit int, readOnly bool) (result *plugin.GraphQueryResult, err error) {
	// Treat malformed upstream protocol as failure, including SDK assertion panics.
	defer func() {
		if recover() != nil {
			result = nil
			err = fmt.Errorf("invalid FalkorDB query response")
		}
	}()
	command := "GRAPH.QUERY"
	if readOnly {
		command = "GRAPH.RO_QUERY"
	}
	if len(params) > 0 {
		query = sdk.BuildParamsHeader(params) + query
	}
	args := []interface{}{command, name, query, "--compact"}
	timeout := int64(30000)
	if deadline, ok := ctx.Deadline(); ok {
		timeout = time.Until(deadline).Milliseconds()
		if timeout <= 0 {
			return nil, context.DeadlineExceeded
		}
	}
	config, configErr := client.Do(ctx, "GRAPH.CONFIG", "GET", "TIMEOUT_MAX").Slice()
	if configErr != nil {
		return nil, configErr
	}
	if len(config) != 2 {
		return nil, fmt.Errorf("invalid FalkorDB timeout metadata")
	}
	maximum, ok := config[1].(int64)
	if !ok || maximum < 0 {
		return nil, fmt.Errorf("invalid FalkorDB timeout limit")
	}
	if maximum > 0 && timeout > maximum {
		timeout = maximum
	}
	args = append(args, "timeout", timeout)
	response, err := client.Do(ctx, args...).Result()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	raw, ok := response.([]interface{})
	if !ok || len(raw) != 3 && len(raw) != 1 {
		return nil, fmt.Errorf("invalid FalkorDB result envelope")
	}
	result = &plugin.GraphQueryResult{QueryResult: plugin.QueryResult{Columns: []string{}, Rows: []map[string]interface{}{}}}
	if len(raw) == 1 {
		return result, nil
	}
	for _, col := range raw[0].([]interface{}) {
		result.Columns = append(result.Columns, col.([]interface{})[1].(string))
	}
	db := sdk.FalkorDB{Conn: contextClient{UniversalClient: client, ctx: ctx}}
	decoded, err := sdk.QueryResultNew(db.SelectGraph(name), response)
	if err != nil {
		return nil, err
	}
	nodes := map[string]plugin.GraphNode{}
	edges := map[string]plugin.GraphRelationship{}
	for decoded.Next() {
		if limit > 0 && len(result.Rows) >= limit {
			break
		}
		record := decoded.Record()
		row := map[string]interface{}{}
		for i, key := range record.Keys() {
			row[key] = graphValue(record.Values()[i], nodes, edges)
		}
		result.Rows = append(result.Rows, row)
	}
	if len(nodes) > 0 || len(edges) > 0 {
		data := &plugin.GraphData{Nodes: []plugin.GraphNode{}, Relationships: []plugin.GraphRelationship{}}
		for _, n := range nodes {
			data.Nodes = append(data.Nodes, n)
		}
		for _, r := range edges {
			data.Relationships = append(data.Relationships, r)
		}
		sort.Slice(data.Nodes, func(i, j int) bool { return data.Nodes[i].ElementId < data.Nodes[j].ElementId })
		sort.Slice(data.Relationships, func(i, j int) bool { return data.Relationships[i].ElementId < data.Relationships[j].ElementId })
		result.GraphData = data
	}
	return result, nil
}
func graphValue(v interface{}, nodes map[string]plugin.GraphNode, edges map[string]plugin.GraphRelationship) interface{} {
	switch value := v.(type) {
	case *sdk.Node:
		id := strconv.FormatUint(value.ID, 10)
		node := plugin.GraphNode{ElementId: id, Labels: append([]string{}, value.Labels...), Properties: value.Properties}
		nodes[id] = node
		return node
	case *sdk.Edge:
		id := strconv.FormatUint(value.ID, 10)
		edge := plugin.GraphRelationship{ElementId: id, Type: value.Relation, StartNodeId: strconv.FormatUint(value.SourceNodeID(), 10), EndNodeId: strconv.FormatUint(value.DestNodeID(), 10), Properties: value.Properties}
		edges[id] = edge
		return edge
	case sdk.Path:
		pathNodes := make([]interface{}, len(value.Nodes))
		pathEdges := make([]interface{}, len(value.Edges))
		for i, n := range value.Nodes {
			pathNodes[i] = graphValue(n, nodes, edges)
		}
		for i, e := range value.Edges {
			pathEdges[i] = graphValue(e, nodes, edges)
		}
		return map[string]interface{}{"nodes": pathNodes, "relationships": pathEdges}
	case []interface{}:
		items := make([]interface{}, len(value))
		for i, v := range value {
			items[i] = graphValue(v, nodes, edges)
		}
		return items
	case map[string]interface{}:
		m := map[string]interface{}{}
		for k, v := range value {
			m[k] = graphValue(v, nodes, edges)
		}
		return m
	}
	return v
}

func operationContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		if _, ok := ctx.Deadline(); ok {
			return context.WithCancel(ctx)
		}
		timeout = 30 * time.Second
	}
	return context.WithTimeout(ctx, timeout)
}
