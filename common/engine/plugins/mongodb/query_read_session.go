package mongodb

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/addp/common/engine/plugin"
	commonquery "github.com/addp/common/query"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var _ plugin.QueryReadSessionProvider = (*MongoDBPlugin)(nil)

type mongoQueryReadPlan struct {
	database   string
	collection string
	command    string
	document   map[string]interface{}
}

type mongoPreparedQuery struct {
	provider     *MongoDBPlugin
	connInfo     plugin.ConnectionInfo
	request      plugin.QueryRequest
	plan         *mongoQueryReadPlan
	analysis     *plugin.QueryAnalysis
	mu           sync.Mutex
	consumed     bool
	readSetReady bool
	readSet      *plugin.QueryReadSet
	readSetErr   error
	lineageReady bool
	lineage      *plugin.QueryOutputLineage
	lineageErr   error
}

func (q *mongoPreparedQuery) Analysis(context.Context) (*plugin.QueryAnalysis, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.analysis.Clone(), nil
}

func (q *mongoPreparedQuery) ReadSet(ctx context.Context) (*plugin.QueryReadSet, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.resolveReadSetLocked(ctx)
	return q.readSet.Clone(), q.readSetErr
}

func (q *mongoPreparedQuery) resolveReadSetLocked(ctx context.Context) {
	if q.readSetReady {
		return
	}
	if q.consumed {
		q.readSetErr = plugin.ErrPreparedQueryConsumed
		q.readSetReady = true
		return
	}
	q.readSetReady = true
	q.readSet, q.readSetErr = q.resolveReadSet(ctx)
}

func (q *mongoPreparedQuery) OutputLineage(ctx context.Context) (*plugin.QueryOutputLineage, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.lineageReady {
		return q.lineage.Clone(), q.lineageErr
	}
	if q.consumed {
		return nil, plugin.ErrPreparedQueryConsumed
	}
	q.lineageReady = true
	q.resolveReadSetLocked(ctx)
	if q.readSetErr != nil {
		q.lineageErr = q.readSetErr
		return nil, q.lineageErr
	}
	q.lineage, q.lineageErr = q.resolveOutputLineage(ctx)
	if q.lineageErr == nil {
		q.lineageErr = plugin.ValidateQueryOutputLineage(q.readSet, q.lineage)
	}
	return q.lineage.Clone(), q.lineageErr
}

func (q *mongoPreparedQuery) resolveReadSet(ctx context.Context) (*plugin.QueryReadSet, error) {
	if !q.request.Options.ReadOnly || q.request.EngineID == 0 || q.plan.collection == "" {
		return nil, fmt.Errorf("%w: MongoDB query must be a read-only collection query with an engine", plugin.ErrQueryReadSetUnresolved)
	}
	collections := map[string]struct{}{q.plan.collection: {}}
	if q.plan.command == "aggregate" {
		if err := collectMongoAggregateReadCollections(q.plan.document["pipeline"], collections); err != nil {
			return nil, err
		}
	}
	if err := q.provider.expandMongoViewReadCollections(ctx, q.connInfo, q.plan.database, collections); err != nil {
		return nil, err
	}
	return mongoQueryReadSet(q.request, q.plan.database, collections)
}

func (q *mongoPreparedQuery) Execute(ctx context.Context) (*plugin.QueryResult, error) {
	q.mu.Lock()
	if q.consumed {
		q.mu.Unlock()
		return nil, plugin.ErrPreparedQueryConsumed
	}
	q.consumed = true
	q.mu.Unlock()
	return q.provider.executePreparedQuery(ctx, q.connInfo, q.plan.document, q.request.Options)
}

func (q *mongoPreparedQuery) resolveOutputLineage(ctx context.Context) (*plugin.QueryOutputLineage, error) {
	sources := make([]plugin.QueryOutputSource, 0, len(q.readSet.Paths))
	primary := -1
	for _, path := range q.readSet.Paths {
		facts, err := q.provider.DescribeEngineCatalogFacts(ctx, q.connInfo, path, plugin.EngineCatalogFactsOptions{})
		if err != nil || facts == nil || facts.Table == nil || len(facts.Table.Fields) == 0 {
			return nil, fmt.Errorf("%w: read MongoDB source fields", plugin.ErrQueryOutputLineageUnresolved)
		}
		sources = append(sources, plugin.QueryOutputSource{Path: path, Fields: facts.Table.Fields})
		segments := plugin.EngineCatalogPathWithoutRoot(path).Segments
		if len(segments) == 2 && segments[0].Name == q.plan.database && segments[1].Name == q.plan.collection {
			primary = len(sources) - 1
		}
	}
	if primary < 0 {
		return nil, fmt.Errorf("%w: MongoDB primary collection is missing from read set", plugin.ErrQueryOutputLineageUnresolved)
	}
	for index := range sources {
		if index != primary {
			sources[index].OpaqueOutput = true
		}
	}
	switch q.plan.command {
	case "find":
		if !mongoFindProjectionPreservesPaths(q.plan.document["projection"]) {
			sources[primary].OpaqueOutput = true
		} else {
			sources[primary].IdentityOutput = true
		}
	case "count":
	case "distinct":
		field, ok := q.plan.document["key"].(string)
		path := splitMongoFieldPath(field)
		if !ok || len(path) == 0 {
			return nil, fmt.Errorf("%w: MongoDB distinct key is unresolved", plugin.ErrQueryOutputLineageUnresolved)
		}
		sources[primary].Bindings = []plugin.QueryOutputBinding{{
			SourcePath: path, OutputPath: []string{"value"}, Transformation: plugin.QueryOutputTransformationDirect,
		}}
	default:
		for index := range sources {
			sources[index].OpaqueOutput = true
			sources[index].IdentityOutput = false
			sources[index].Bindings = nil
		}
	}
	return &plugin.QueryOutputLineage{Sources: sources}, nil
}

func mongoFindProjectionPreservesPaths(value interface{}) bool {
	if value == nil {
		return true
	}
	document, ok := mongoStageDocument(value)
	if !ok {
		return false
	}
	for key, item := range document {
		if len(splitMongoFieldPath(key)) == 0 {
			return false
		}
		switch current := item.(type) {
		case bool:
		case float64:
			if current != 0 && current != 1 {
				return false
			}
		case int:
			if current != 0 && current != 1 {
				return false
			}
		case int32:
			if current != 0 && current != 1 {
				return false
			}
		case int64:
			if current != 0 && current != 1 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func splitMongoFieldPath(value string) []string {
	parts := strings.Split(strings.TrimSpace(value), ".")
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			return nil
		}
	}
	return parts
}

func (q *mongoPreparedQuery) consumeForReadSession() (*mongoQueryReadPlan, plugin.ConnectionInfo, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.consumed {
		return nil, nil, plugin.ErrPreparedQueryConsumed
	}
	if err := validateMongoQueryReadSession(q.request, q.plan); err != nil {
		return nil, nil, err
	}
	q.consumed = true
	return q.plan, cloneConnectionInfo(q.connInfo), nil
}

func mongoQueryReadSet(
	req plugin.QueryRequest,
	database string,
	collections map[string]struct{},
) (*plugin.QueryReadSet, error) {
	paths := make([]plugin.EngineCatalogPath, 0, len(collections))
	model := plugin.DynamicSchemaCatalogModel()
	for collection := range collections {
		paths = append(paths, plugin.EngineCatalogBranchLeafPath(
			model, req.EngineID,
			plugin.EngineCatalogTermDatabase, database,
			plugin.EngineCatalogTermCollection, plugin.EngineCatalogKindCollection, collection,
		))
	}
	readSet, err := plugin.NewQueryReadSet(paths...)
	if err != nil {
		return nil, err
	}
	if err := plugin.ValidateQueryReadSet(req, readSet); err != nil {
		return nil, err
	}
	return readSet, nil
}

func (p *MongoDBPlugin) expandMongoViewReadCollections(
	ctx context.Context,
	connInfo plugin.ConnectionInfo,
	database string,
	collections map[string]struct{},
) error {
	client, err := p.createClient(ctx, connInfo)
	if err != nil {
		return fmt.Errorf("%w: inspect MongoDB collection dependencies: %v", plugin.ErrQueryReadSetUnresolved, err)
	}
	defer client.Disconnect(ctx) //nolint:errcheck

	inspected := make(map[string]struct{}, len(collections))
	for {
		pending := ""
		for collection := range collections {
			if _, exists := inspected[collection]; !exists {
				pending = collection
				break
			}
		}
		if pending == "" {
			return nil
		}
		inspected[pending] = struct{}{}
		specifications, err := client.Database(database).ListCollectionSpecifications(ctx, bson.D{{Key: "name", Value: pending}})
		if err != nil || len(specifications) != 1 {
			return fmt.Errorf("%w: inspect MongoDB collection %s", plugin.ErrQueryReadSetUnresolved, pending)
		}
		specification := specifications[0]
		if specification.Type != "view" {
			if specification.Type != "collection" && specification.Type != "timeseries" {
				return fmt.Errorf("%w: unsupported MongoDB collection type %s", plugin.ErrQueryReadSetUnresolved, specification.Type)
			}
			continue
		}
		var viewOptions struct {
			ViewOn   string      `bson:"viewOn"`
			Pipeline primitive.A `bson:"pipeline"`
		}
		if err := bson.Unmarshal(specification.Options, &viewOptions); err != nil || strings.TrimSpace(viewOptions.ViewOn) == "" {
			return fmt.Errorf("%w: decode MongoDB view %s", plugin.ErrQueryReadSetUnresolved, pending)
		}
		collections[strings.TrimSpace(viewOptions.ViewOn)] = struct{}{}
		if err := collectMongoAggregateReadCollections(viewOptions.Pipeline, collections); err != nil {
			return err
		}
	}
}

func collectMongoAggregateReadCollections(value interface{}, collections map[string]struct{}) error {
	switch current := value.(type) {
	case []interface{}:
		for _, item := range current {
			if err := collectMongoAggregateReadCollections(item, collections); err != nil {
				return err
			}
		}
	case primitive.A:
		return collectMongoAggregateReadCollections([]interface{}(current), collections)
	case map[string]interface{}:
		return collectMongoAggregateReadDocument(current, collections)
	case bson.M:
		return collectMongoAggregateReadDocument(map[string]interface{}(current), collections)
	case bson.D:
		for _, element := range current {
			if err := collectMongoAggregateReadEntry(element.Key, element.Value, collections); err != nil {
				return err
			}
		}
	}
	return nil
}

func collectMongoAggregateReadDocument(current map[string]interface{}, collections map[string]struct{}) error {
	for key, item := range current {
		if err := collectMongoAggregateReadEntry(key, item, collections); err != nil {
			return err
		}
	}
	return nil
}

func collectMongoAggregateReadEntry(key string, item interface{}, collections map[string]struct{}) error {
	switch key {
	case "$unionWith":
		collection, err := mongoUnionCollection(item)
		if err != nil {
			return err
		}
		collections[collection] = struct{}{}
	case "$lookup", "$graphLookup":
		stage, ok := mongoStageDocument(item)
		if !ok {
			return fmt.Errorf("%w: MongoDB %s stage must be an object", plugin.ErrQueryReadSetUnresolved, key)
		}
		collection, ok := stage["from"].(string)
		if !ok || strings.TrimSpace(collection) == "" {
			return fmt.Errorf("%w: MongoDB %s stage requires a string from collection", plugin.ErrQueryReadSetUnresolved, key)
		}
		collections[strings.TrimSpace(collection)] = struct{}{}
	case "$out", "$merge":
		return fmt.Errorf("%w: MongoDB write stage %s is not a read dependency", plugin.ErrQueryReadSetUnresolved, key)
	}
	return collectMongoAggregateReadCollections(item, collections)
}

func mongoStageDocument(value interface{}) (map[string]interface{}, bool) {
	switch current := value.(type) {
	case map[string]interface{}:
		return current, true
	case bson.M:
		return map[string]interface{}(current), true
	case bson.D:
		result := make(map[string]interface{}, len(current))
		for _, element := range current {
			result[element.Key] = element.Value
		}
		return result, true
	}
	return nil, false
}

func mongoUnionCollection(value interface{}) (string, error) {
	if collection, ok := value.(string); ok && strings.TrimSpace(collection) != "" {
		return strings.TrimSpace(collection), nil
	}
	stage, ok := mongoStageDocument(value)
	if !ok {
		return "", fmt.Errorf("%w: MongoDB $unionWith must be a collection string or object", plugin.ErrQueryReadSetUnresolved)
	}
	collection, ok := stage["coll"].(string)
	if !ok || strings.TrimSpace(collection) == "" {
		return "", fmt.Errorf("%w: MongoDB $unionWith object requires a coll string", plugin.ErrQueryReadSetUnresolved)
	}
	return strings.TrimSpace(collection), nil
}

func (p *MongoDBPlugin) OpenQueryReadSession(
	ctx context.Context,
	prepared plugin.PreparedQuery,
) (plugin.QueryReadSession, error) {
	mongoPrepared, ok := prepared.(*mongoPreparedQuery)
	if !ok || mongoPrepared.provider != p {
		return nil, fmt.Errorf("MongoDB query read session requires a PreparedQuery from the same provider")
	}
	plan, connInfo, err := mongoPrepared.consumeForReadSession()
	if err != nil {
		return nil, err
	}

	client, err := p.createClient(ctx, connInfo)
	if err != nil {
		return nil, fmt.Errorf("open MongoDB query read client: %w", err)
	}
	coll := client.Database(plan.database).Collection(plan.collection)

	var cursor *mongo.Cursor
	switch plan.command {
	case "find":
		cursor, err = openMongoFindReadCursor(ctx, coll, plan.document)
	case "aggregate":
		cursor, err = openMongoAggregateReadCursor(ctx, coll, plan.document)
	default:
		err = fmt.Errorf("unsupported MongoDB query read command %q", plan.command)
	}
	if err != nil {
		_ = client.Disconnect(context.Background())
		return nil, err
	}
	return &mongoQueryReadSession{client: client, cursor: cursor}, nil
}

func prepareMongoQueryReadPlan(connInfo plugin.ConnectionInfo, req plugin.QueryRequest) (*mongoQueryReadPlan, error) {
	if language := strings.ToLower(strings.TrimSpace(req.Language)); language != "mql" {
		return nil, fmt.Errorf("MongoDB query runtime requires language mql")
	}

	boundQuery, err := commonquery.BindMQL(req.Query, req.Options.Parameters)
	if err != nil {
		return nil, fmt.Errorf("bind MongoDB query read parameters: %w", err)
	}
	var command map[string]interface{}
	if err := json.Unmarshal([]byte(boundQuery), &command); err != nil {
		return nil, fmt.Errorf("invalid MongoDB query command: %w", err)
	}

	database := plugin.GetString(connInfo, "database")
	if selected, ok := mongoDatabaseFromCatalogPath(valueOrEmptyPath(req.TargetPath)); ok {
		database = selected
	}
	if strings.TrimSpace(database) == "" {
		return nil, plugin.NewQueryError(
			plugin.QueryErrorCodeMongoDBDatabaseRequired,
			fmt.Errorf("MongoDB query requires a database selected in the resource path or configured on the engine"),
		)
	}

	if collection, ok := getStringKey(command, "find"); ok && strings.TrimSpace(collection) != "" {
		return &mongoQueryReadPlan{database: database, collection: strings.TrimSpace(collection), command: "find", document: command}, nil
	}
	if collection, ok := getStringKey(command, "aggregate"); ok && strings.TrimSpace(collection) != "" {
		if req.Options.ReadOnly && mongoAggregateHasWriteStage(command["pipeline"]) {
			return nil, fmt.Errorf("MongoDB read-only query rejects $out and $merge aggregate stages")
		}
		if _, ok := command["pipeline"].([]interface{}); !ok {
			return nil, fmt.Errorf("MongoDB aggregate query requires a pipeline array")
		}
		return &mongoQueryReadPlan{database: database, collection: strings.TrimSpace(collection), command: "aggregate", document: command}, nil
	}
	if collection, ok := getStringKey(command, "count"); ok && strings.TrimSpace(collection) != "" {
		return &mongoQueryReadPlan{database: database, collection: strings.TrimSpace(collection), command: "count", document: command}, nil
	}
	if collection, ok := getStringKey(command, "distinct"); ok && strings.TrimSpace(collection) != "" {
		return &mongoQueryReadPlan{database: database, collection: strings.TrimSpace(collection), command: "distinct", document: command}, nil
	}
	if req.Options.ReadOnly {
		return nil, fmt.Errorf("read-only MQL only supports find, aggregate, count, and distinct")
	}
	return &mongoQueryReadPlan{database: database, command: "command", document: command}, nil
}

func validateMongoQueryReadSession(req plugin.QueryRequest, plan *mongoQueryReadPlan) error {
	if !req.Options.ReadOnly {
		return fmt.Errorf("MongoDB query read session requires read_only=true")
	}
	if req.Options.Limit != 0 || req.Options.Offset != 0 {
		return fmt.Errorf("MongoDB query read session does not accept preview limit or offset")
	}
	if plan == nil || (plan.command != "find" && plan.command != "aggregate") {
		return fmt.Errorf("MongoDB query read session only supports find and aggregate")
	}
	return nil
}

func openMongoFindReadCursor(ctx context.Context, coll *mongo.Collection, command map[string]interface{}) (*mongo.Cursor, error) {
	findOptions := options.Find().SetBatchSize(1024)
	if value, ok := command["limit"]; ok {
		limit, valid := toInt64(value)
		if !valid || limit < 0 {
			return nil, fmt.Errorf("MongoDB find query read limit must be a non-negative integer")
		}
		if limit > 0 {
			findOptions.SetLimit(limit)
		}
	}
	if value, ok := command["skip"]; ok {
		skip, valid := toInt64(value)
		if !valid || skip < 0 {
			return nil, fmt.Errorf("MongoDB find query read skip must be a non-negative integer")
		}
		findOptions.SetSkip(skip)
	}
	if sortValue, ok := command["sort"].(map[string]interface{}); ok {
		findOptions.SetSort(sortValue)
	}
	if projection, ok := command["projection"].(map[string]interface{}); ok {
		findOptions.SetProjection(projection)
	}
	filter := bson.M{}
	if value, ok := command["filter"]; ok {
		filterValue, valid := value.(map[string]interface{})
		if !valid {
			return nil, fmt.Errorf("MongoDB find query read filter must be an object")
		}
		filter = bson.M(filterValue)
	}
	cursor, err := coll.Find(ctx, filter, findOptions)
	if err != nil {
		return nil, fmt.Errorf("open MongoDB find query read cursor: %w", err)
	}
	return cursor, nil
}

func openMongoAggregateReadCursor(ctx context.Context, coll *mongo.Collection, command map[string]interface{}) (*mongo.Cursor, error) {
	pipeline := command["pipeline"].([]interface{})
	aggregateOptions := options.Aggregate().SetBatchSize(1024)
	if allowDiskUse, ok := command["allowDiskUse"].(bool); ok {
		aggregateOptions.SetAllowDiskUse(allowDiskUse)
	}
	cursor, err := coll.Aggregate(ctx, pipeline, aggregateOptions)
	if err != nil {
		return nil, fmt.Errorf("open MongoDB aggregate query read cursor: %w", err)
	}
	return cursor, nil
}

type mongoQueryReadSession struct {
	client    *mongo.Client
	cursor    *mongo.Cursor
	offset    int64
	exhausted bool
	closed    bool
}

func (s *mongoQueryReadSession) ReadBatch(ctx context.Context, limit int) (*plugin.BatchData, error) {
	if s.closed {
		return nil, fmt.Errorf("MongoDB query read session is closed")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("MongoDB query read batch limit must be positive")
	}
	batch := &plugin.BatchData{Offset: s.offset, Rows: make([]map[string]interface{}, 0, limit)}
	if s.exhausted {
		return batch, nil
	}
	for len(batch.Rows) < limit {
		if !s.cursor.Next(ctx) {
			s.exhausted = true
			break
		}
		var document bson.M
		if err := s.cursor.Decode(&document); err != nil {
			return nil, fmt.Errorf("decode MongoDB query read row: %w", err)
		}
		converted, ok := convertBSONValue(document).(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("normalize MongoDB query read row")
		}
		batch.Rows = append(batch.Rows, converted)
	}
	if err := s.cursor.Err(); err != nil {
		return nil, fmt.Errorf("iterate MongoDB query read cursor: %w", err)
	}
	s.offset += int64(len(batch.Rows))
	return batch, nil
}

func (s *mongoQueryReadSession) Close(ctx context.Context) error {
	if s.closed {
		return nil
	}
	s.closed = true
	var closeErr error
	if s.cursor != nil {
		closeErr = s.cursor.Close(ctx)
	}
	if s.client != nil {
		if err := s.client.Disconnect(ctx); err != nil && closeErr == nil {
			closeErr = err
		}
	}
	return closeErr
}
