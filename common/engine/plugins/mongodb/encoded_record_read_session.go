package mongodb

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var _ plugin.EncodedRecordReadSessionProvider = (*MongoDBPlugin)(nil)

func (p *MongoDBPlugin) OpenEncodedRecordReadSession(
	ctx context.Context,
	connInfo plugin.ConnectionInfo,
	path plugin.EngineCatalogPath,
	opts plugin.EncodedRecordReadSessionOptions,
) (plugin.EncodedRecordReadSession, error) {
	if strings.TrimSpace(opts.Format) != string(format.FormatMongoDBExtendedJSONL) {
		return nil, fmt.Errorf("MongoDB encoded record read does not support format %q", opts.Format)
	}
	database, collection, ok := mongoCollectionFromCatalogPath(path)
	if !ok {
		return nil, fmt.Errorf("MongoDB encoded record read requires database/collection catalog path")
	}
	client, err := p.createClient(ctx, connInfo)
	if err != nil {
		return nil, err
	}
	cursor, err := client.Database(database).Collection(collection).Find(
		ctx,
		bson.D{},
		options.Find().SetBatchSize(1024),
	)
	if err != nil {
		_ = client.Disconnect(context.Background())
		return nil, fmt.Errorf("open MongoDB encoded collection cursor: %w", err)
	}
	return &mongoEncodedRecordReadSession{client: client, cursor: cursor, beforeEncode: opts.BeforeEncode}, nil
}

type mongoEncodedRecordReadSession struct {
	client       *mongo.Client
	cursor       *mongo.Cursor
	offset       int64
	exhausted    bool
	closed       bool
	beforeEncode plugin.EncodedRecordTransform
}

func (s *mongoEncodedRecordReadSession) ReadBatch(ctx context.Context, limit int) (*plugin.EncodedRecordBatchData, error) {
	if s.closed {
		return nil, fmt.Errorf("MongoDB encoded record read session is closed")
	}
	if limit <= 0 {
		limit = 1000
	}
	batch := &plugin.EncodedRecordBatchData{Offset: s.offset}
	if s.exhausted {
		return batch, nil
	}

	var content bytes.Buffer
	for batch.Records < int64(limit) {
		if !s.cursor.Next(ctx) {
			s.exhausted = true
			break
		}
		line, err := marshalMongoCanonicalExtendedJSONLine(s.cursor.Current, s.beforeEncode)
		if err != nil {
			return nil, fmt.Errorf("encode MongoDB collection record: %w", err)
		}
		if _, err := content.Write(line); err != nil {
			return nil, fmt.Errorf("buffer MongoDB encoded collection record: %w", err)
		}
		batch.Records++
	}
	if err := s.cursor.Err(); err != nil {
		return nil, fmt.Errorf("iterate MongoDB encoded collection cursor: %w", err)
	}
	batch.Content = append([]byte(nil), content.Bytes()...)
	s.offset += batch.Records
	return batch, nil
}

func (s *mongoEncodedRecordReadSession) Close(ctx context.Context) error {
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

func marshalMongoCanonicalExtendedJSONLine(raw bson.Raw, beforeEncode plugin.EncodedRecordTransform) ([]byte, error) {
	var native bson.M
	if err := bson.Unmarshal(raw, &native); err != nil {
		return nil, fmt.Errorf("decode BSON record before encoding: %w", err)
	}
	document, ok := normalizeMongoEncodedRecordValue(native).(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("normalize BSON record before encoding")
	}
	if beforeEncode != nil {
		if err := beforeEncode(document); err != nil {
			return nil, fmt.Errorf("transform MongoDB record before encoding: %w", err)
		}
	}
	encoded, err := bson.MarshalExtJSON(document, true, false)
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

func normalizeMongoEncodedRecordValue(value interface{}) interface{} {
	switch typed := value.(type) {
	case bson.M:
		result := make(map[string]interface{}, len(typed))
		for key, child := range typed {
			result[key] = normalizeMongoEncodedRecordValue(child)
		}
		return result
	case bson.D:
		result := make(map[string]interface{}, len(typed))
		for _, element := range typed {
			result[element.Key] = normalizeMongoEncodedRecordValue(element.Value)
		}
		return result
	case primitive.A:
		result := make([]interface{}, len(typed))
		for index, child := range typed {
			result[index] = normalizeMongoEncodedRecordValue(child)
		}
		return result
	case []interface{}:
		result := make([]interface{}, len(typed))
		for index, child := range typed {
			result[index] = normalizeMongoEncodedRecordValue(child)
		}
		return result
	default:
		return value
	}
}
