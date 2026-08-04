package spark_sql

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/resume"
	"github.com/beltran/gohive"
)

var _ plugin.TableReadSessionProvider = (*SparkSQLPlugin)(nil)

func (p *SparkSQLPlugin) OpenTableReadSession(
	ctx context.Context,
	connInfo plugin.ConnectionInfo,
	path plugin.CatalogPath,
	opts plugin.TableReadSessionOptions,
) (plugin.TableReadSession, error) {
	if err := resume.RejectUnsupported(opts.ResumeMarker, "spark.table_read_session"); err != nil {
		return nil, err
	}
	if strings.TrimSpace(opts.Query) != "" || len(opts.Args) != 0 {
		return nil, fmt.Errorf("Spark table read session requires a catalog table path")
	}
	segments, root, err := sparkCatalogBusinessSegments(path)
	if err != nil || root || len(segments) != 2 {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("Spark table read requires database/table catalog path")
	}
	facts, err := p.DescribeCatalogFacts(ctx, connInfo, path, plugin.CatalogFactsOptions{})
	if err != nil {
		return nil, err
	}
	if facts == nil || facts.Table == nil || len(facts.Table.Fields) == 0 {
		return nil, fmt.Errorf("Spark table catalog returned no fields")
	}

	host := plugin.NormalizeHost(plugin.GetString(connInfo, "host"))
	port := plugin.GetInt(connInfo, "port")
	if port == 0 {
		port = p.DefaultPort()
	}
	configuration := gohive.NewConnectConfiguration()
	if user := plugin.GetString(connInfo, "user"); user != "" {
		configuration.Username = user
		configuration.Password = plugin.GetString(connInfo, "password")
	}
	configuration.ConnectTimeout = 30 * time.Second
	configuration.SocketTimeout = 30 * time.Second
	connection, err := gohive.Connect(host, port, "NONE", configuration)
	if err != nil {
		return nil, fmt.Errorf("open Spark table read connection: %w", err)
	}
	cursor := connection.Cursor()
	query := "SELECT * FROM " + quoteSparkIdentifier(segments[0].Name) + "." + quoteSparkIdentifier(segments[1].Name)
	cursor.Exec(ctx, query)
	if cursor.Err != nil {
		cursor.Close()
		_ = connection.Close()
		return nil, fmt.Errorf("open Spark table cursor: %w", cursor.Err)
	}
	return &sparkTableReadSession{
		connection: connection,
		cursor:     cursor,
		fields:     append([]datatype.FieldInfo(nil), facts.Table.Fields...),
	}, nil
}

type sparkTableReadSession struct {
	connection *gohive.Connection
	cursor     *gohive.Cursor
	fields     []datatype.FieldInfo
	offset     int64
	exhausted  bool
	closed     bool
}

func (s *sparkTableReadSession) ReadBatch(ctx context.Context, limit int) (*plugin.BatchData, error) {
	if s.closed {
		return nil, fmt.Errorf("Spark table read session is closed")
	}
	if limit <= 0 {
		limit = 1000
	}
	batch := &plugin.BatchData{
		Fields: append([]datatype.FieldInfo(nil), s.fields...),
		Offset: s.offset,
	}
	if s.exhausted {
		return batch, nil
	}
	batch.Rows = make([]map[string]interface{}, 0, limit)
	for len(batch.Rows) < limit && s.cursor.HasMore(ctx) {
		row := s.cursor.RowMap(ctx)
		if s.cursor.Err != nil {
			return nil, fmt.Errorf("read Spark table cursor: %w", s.cursor.Err)
		}
		batch.Rows = append(batch.Rows, row)
	}
	if s.cursor.Err != nil {
		return nil, fmt.Errorf("read Spark table cursor: %w", s.cursor.Err)
	}
	if len(batch.Rows) < limit {
		s.exhausted = true
	}
	s.offset += int64(len(batch.Rows))
	return batch, nil
}

func (s *sparkTableReadSession) Close(context.Context) error {
	if s.closed {
		return nil
	}
	s.closed = true
	if s.cursor != nil {
		s.cursor.Close()
	}
	if s.connection != nil {
		return s.connection.Close()
	}
	return nil
}
