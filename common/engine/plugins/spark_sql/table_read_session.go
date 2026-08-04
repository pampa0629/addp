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
	cursor.Exec(ctx, "SET spark.sql.session.timeZone=UTC")
	if cursor.Err != nil {
		cursor.Close()
		_ = connection.Close()
		return nil, fmt.Errorf("configure Spark table read session timezone: %w", cursor.Err)
	}
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
	cursor     sparkTableCursor
	fields     []datatype.FieldInfo
	offset     int64
	exhausted  bool
	closed     bool
}

type sparkTableCursor interface {
	HasMore(context.Context) bool
	RowMap(context.Context) map[string]interface{}
	Error() error
	Close()
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
		if err := s.cursor.Error(); err != nil {
			return nil, fmt.Errorf("read Spark table cursor: %w", err)
		}
		if err := normalizeSparkTableRow(row, s.fields); err != nil {
			return nil, err
		}
		batch.Rows = append(batch.Rows, row)
	}
	if err := s.cursor.Error(); err != nil {
		return nil, fmt.Errorf("read Spark table cursor: %w", err)
	}
	if len(batch.Rows) < limit {
		s.exhausted = true
	}
	s.offset += int64(len(batch.Rows))
	return batch, nil
}

func normalizeSparkTableRow(row map[string]interface{}, fields []datatype.FieldInfo) error {
	for _, field := range fields {
		value, exists := row[field.Name]
		if !exists || value == nil {
			continue
		}
		var normalized interface{}
		var err error
		switch field.Type {
		case datatype.FieldTypeDate:
			normalized, err = normalizeSparkDate(value)
		case datatype.FieldTypeTimestamp:
			normalized, err = normalizeSparkTimestamp(value)
		default:
			continue
		}
		if err != nil {
			return fmt.Errorf("normalize Spark table field %q: %w", field.Name, err)
		}
		row[field.Name] = normalized
	}
	return nil
}

func normalizeSparkDate(value interface{}) (time.Time, error) {
	switch typed := value.(type) {
	case time.Time:
		return typed.UTC(), nil
	case string:
		parsed, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(typed), time.UTC)
		if err != nil {
			return time.Time{}, fmt.Errorf("parse date %q: %w", typed, err)
		}
		return parsed, nil
	default:
		return time.Time{}, fmt.Errorf("expected date string or time.Time, got %T", value)
	}
}

func normalizeSparkTimestamp(value interface{}) (time.Time, error) {
	switch typed := value.(type) {
	case time.Time:
		return typed.UTC(), nil
	case string:
		text := strings.TrimSpace(typed)
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05", "2006-01-02T15:04:05"} {
			parsed, err := time.ParseInLocation(layout, text, time.UTC)
			if err == nil {
				return parsed.UTC(), nil
			}
		}
		return time.Time{}, fmt.Errorf("parse timestamp %q", typed)
	default:
		return time.Time{}, fmt.Errorf("expected timestamp string or time.Time, got %T", value)
	}
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
