package mongodb

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/addp/common/engine/plugin"
	commonquery "github.com/addp/common/query"
	"go.mongodb.org/mongo-driver/bson"
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

func (p *MongoDBPlugin) OpenQueryReadSession(
	ctx context.Context,
	connInfo plugin.ConnectionInfo,
	req plugin.QueryRequest,
) (plugin.QueryReadSession, error) {
	plan, err := parseMongoQueryReadPlan(connInfo, req)
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

func parseMongoQueryReadPlan(connInfo plugin.ConnectionInfo, req plugin.QueryRequest) (*mongoQueryReadPlan, error) {
	if !req.Options.ReadOnly {
		return nil, fmt.Errorf("MongoDB query read session requires read_only=true")
	}
	if req.Options.Limit != 0 || req.Options.Offset != 0 {
		return nil, fmt.Errorf("MongoDB query read session does not accept preview limit or offset")
	}
	if language := strings.ToLower(strings.TrimSpace(req.Language)); language != "mql" {
		return nil, fmt.Errorf("MongoDB query read session requires language mql")
	}

	boundQuery, err := commonquery.BindMQL(req.Query, req.Options.Parameters)
	if err != nil {
		return nil, fmt.Errorf("bind MongoDB query read parameters: %w", err)
	}
	var command map[string]interface{}
	if err := json.Unmarshal([]byte(boundQuery), &command); err != nil {
		return nil, fmt.Errorf("invalid MongoDB query read command: %w", err)
	}

	database := plugin.GetString(connInfo, "database")
	if selected, ok := mongoDatabaseFromCatalogPath(valueOrEmptyPath(req.TargetPath)); ok {
		database = selected
	}
	if strings.TrimSpace(database) == "" {
		return nil, plugin.NewQueryError(
			plugin.QueryErrorCodeMongoDBDatabaseRequired,
			fmt.Errorf("MongoDB query read session requires a database selected in the resource path or configured on the engine"),
		)
	}

	if collection, ok := getStringKey(command, "find"); ok && strings.TrimSpace(collection) != "" {
		return &mongoQueryReadPlan{database: database, collection: collection, command: "find", document: command}, nil
	}
	if collection, ok := getStringKey(command, "aggregate"); ok && strings.TrimSpace(collection) != "" {
		if mongoAggregateHasWriteStage(command["pipeline"]) {
			return nil, fmt.Errorf("MongoDB query read session rejects $out and $merge aggregate stages")
		}
		if _, ok := command["pipeline"].([]interface{}); !ok {
			return nil, fmt.Errorf("MongoDB aggregate query read session requires a pipeline array")
		}
		return &mongoQueryReadPlan{database: database, collection: collection, command: "aggregate", document: command}, nil
	}
	return nil, fmt.Errorf("MongoDB query read session only supports find and aggregate")
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
