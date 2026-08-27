package mongodb

import (
	"context"
	"fmt"
	"strings"

	"github.com/addp/common/engine/plugin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var _ plugin.RecordReadSessionProvider = (*MongoDBPlugin)(nil)

func (p *MongoDBPlugin) OpenRecordReadSession(
	ctx context.Context,
	connInfo plugin.ConnectionInfo,
	path plugin.EngineCatalogPath,
	_ plugin.RecordReadSessionOptions,
) (plugin.RecordReadSession, error) {
	segments := plugin.EngineCatalogPathWithoutRoot(path).Segments
	if len(segments) != 2 || segments[0].Term != plugin.EngineCatalogTermDatabase ||
		segments[1].Term != plugin.EngineCatalogTermCollection || strings.TrimSpace(segments[0].Name) == "" ||
		strings.TrimSpace(segments[1].Name) == "" {
		return nil, fmt.Errorf("MongoDB record read requires database/collection catalog path")
	}
	client, err := p.createClient(ctx, connInfo)
	if err != nil {
		return nil, err
	}
	cursor, err := client.Database(segments[0].Name).Collection(segments[1].Name).Find(
		ctx,
		bson.D{},
		options.Find().SetBatchSize(1024),
	)
	if err != nil {
		_ = client.Disconnect(context.Background())
		return nil, fmt.Errorf("open MongoDB collection cursor: %w", err)
	}
	return &mongoRecordReadSession{client: client, cursor: cursor}, nil
}

type mongoRecordReadSession struct {
	client    *mongo.Client
	cursor    *mongo.Cursor
	offset    int64
	exhausted bool
	closed    bool
}

func (s *mongoRecordReadSession) ReadBatch(ctx context.Context, limit int) (*plugin.RecordBatchData, error) {
	if s.closed {
		return nil, fmt.Errorf("MongoDB record read session is closed")
	}
	if limit <= 0 {
		limit = 1000
	}
	batch := &plugin.RecordBatchData{Offset: s.offset}
	if s.exhausted {
		return batch, nil
	}
	batch.Records = make([]map[string]interface{}, 0, limit)
	for len(batch.Records) < limit {
		if !s.cursor.Next(ctx) {
			s.exhausted = true
			break
		}
		var document bson.M
		if err := s.cursor.Decode(&document); err != nil {
			return nil, fmt.Errorf("decode MongoDB collection record: %w", err)
		}
		converted, ok := convertBSONValue(document).(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("normalize MongoDB collection record")
		}
		batch.Records = append(batch.Records, converted)
	}
	if err := s.cursor.Err(); err != nil {
		return nil, fmt.Errorf("iterate MongoDB collection cursor: %w", err)
	}
	s.offset += int64(len(batch.Records))
	return batch, nil
}

func (s *mongoRecordReadSession) Close(ctx context.Context) error {
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
