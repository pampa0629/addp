package connector

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/addp/transfer/pkg/pipeline"
)

// JDBCWriter JDBC 数据写入器
type JDBCWriter struct {
	db            *sql.DB
	table         string
	columns       []string
	insertStmt    *sql.Stmt
	buffer        []map[string]interface{}
	bufferSize    int
	batchSize     int
	writeMode     string // insert, upsert, replace
	conflictKey   string // upsert 时的唯一键
}

// NewJDBCWriter 创建 JDBC Writer
func NewJDBCWriter(config pipeline.ConnectorConfig) (pipeline.Writer, error) {
	var jdbcConfig JDBCWriterConfig
	if err := mapToStruct(config.Config, &jdbcConfig); err != nil {
		return nil, fmt.Errorf("invalid jdbc config: %w", err)
	}

	batchSize := config.BatchSize
	if batchSize <= 0 {
		batchSize = 1000
	}

	writeMode := jdbcConfig.WriteMode
	if writeMode == "" {
		writeMode = "insert"
	}

	return &JDBCWriter{
		batchSize:   batchSize,
		buffer:      make([]map[string]interface{}, 0, batchSize),
		writeMode:   writeMode,
		conflictKey: jdbcConfig.ConflictKey,
	}, nil
}

// JDBCWriterConfig JDBC Writer 配置
type JDBCWriterConfig struct {
	Driver      string `json:"driver"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Database    string `json:"database"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	Table       string `json:"table"`
	WriteMode   string `json:"write_mode"`   // insert, upsert, replace
	ConflictKey string `json:"conflict_key"` // upsert 时的唯一键
	SSLMode     string `json:"ssl_mode"`
}

// Open 打开数据库连接
func (w *JDBCWriter) Open(ctx context.Context, config pipeline.ConnectorConfig) error {
	var writerConfig JDBCWriterConfig
	if err := mapToStruct(config.Config, &writerConfig); err != nil {
		return err
	}

	// 构建连接字符串
	connStr, err := w.buildConnectionString(writerConfig)
	if err != nil {
		return err
	}

	// 标准化 driver 名称（postgresql -> postgres）
	driverName := w.normalizeDriverName(writerConfig.Driver)

	// 打开数据库连接
	db, err := sql.Open(driverName, connStr)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// 测试连接
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("failed to ping database: %w", err)
	}

	w.db = db
	w.table = writerConfig.Table

	return nil
}

// Write 写入数据批次
func (w *JDBCWriter) Write(ctx context.Context, batch *pipeline.DataBatch) error {
	if batch.IsEmpty() {
		return nil
	}

	// 第一次写入时初始化列名
	if w.columns == nil {
		w.columns = make([]string, 0, len(batch.Rows[0]))
		for col := range batch.Rows[0] {
			w.columns = append(w.columns, col)
		}
	}

	// 添加到缓冲区
	w.buffer = append(w.buffer, batch.Rows...)

	// 缓冲区满时执行批量写入
	if len(w.buffer) >= w.batchSize {
		return w.flushBuffer(ctx)
	}

	return nil
}

// Flush 刷新缓冲区
func (w *JDBCWriter) Flush(ctx context.Context) error {
	if len(w.buffer) > 0 {
		return w.flushBuffer(ctx)
	}
	return nil
}

// Close 关闭连接
func (w *JDBCWriter) Close() error {
	if w.insertStmt != nil {
		w.insertStmt.Close()
	}
	if w.db != nil {
		return w.db.Close()
	}
	return nil
}

// flushBuffer 刷新缓冲区到数据库
func (w *JDBCWriter) flushBuffer(ctx context.Context) error {
	if len(w.buffer) == 0 {
		return nil
	}

	// 开启事务
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// 构建 INSERT 语句
	insertSQL := w.buildInsertSQL()
	stmt, err := tx.PrepareContext(ctx, insertSQL)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	// 批量插入
	for _, row := range w.buffer {
		values := make([]interface{}, len(w.columns))
		for i, col := range w.columns {
			values[i] = row[col]
		}

		if _, err := stmt.ExecContext(ctx, values...); err != nil {
			return fmt.Errorf("failed to insert row: %w", err)
		}
	}

	// 提交事务
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// 清空缓冲区
	w.buffer = w.buffer[:0]

	return nil
}

// buildInsertSQL 构建 INSERT SQL 语句
func (w *JDBCWriter) buildInsertSQL() string {
	columnsStr := strings.Join(w.columns, ", ")
	placeholders := make([]string, len(w.columns))

	for i := range placeholders {
		placeholders[i] = fmt.Sprintf("$%d", i+1) // PostgreSQL 占位符
	}
	placeholdersStr := strings.Join(placeholders, ", ")

	switch w.writeMode {
	case "insert":
		return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", w.table, columnsStr, placeholdersStr)

	case "upsert":
		// PostgreSQL UPSERT 语法
		updateClauses := make([]string, 0, len(w.columns))
		for _, col := range w.columns {
			if col != w.conflictKey {
				updateClauses = append(updateClauses, fmt.Sprintf("%s = EXCLUDED.%s", col, col))
			}
		}
		updateStr := strings.Join(updateClauses, ", ")

		return fmt.Sprintf(
			"INSERT INTO %s (%s) VALUES (%s) ON CONFLICT (%s) DO UPDATE SET %s",
			w.table, columnsStr, placeholdersStr, w.conflictKey, updateStr,
		)

	case "replace":
		// MySQL REPLACE 语法
		return fmt.Sprintf("REPLACE INTO %s (%s) VALUES (%s)", w.table, columnsStr, placeholdersStr)

	default:
		return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", w.table, columnsStr, placeholdersStr)
	}
}

// normalizeDriverName 标准化 driver 名称为 sql.Open 识别的名称
func (w *JDBCWriter) normalizeDriverName(driver string) string {
	switch driver {
	case "postgresql":
		return "postgres"
	default:
		return driver
	}
}

// buildConnectionString 构建连接字符串
func (w *JDBCWriter) buildConnectionString(config JDBCWriterConfig) (string, error) {
	switch config.Driver {
	case "postgres", "postgresql":
		sslMode := config.SSLMode
		if sslMode == "" {
			sslMode = "disable"
		}
		return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
			config.Host, config.Port, config.Username, config.Password, config.Database, sslMode), nil

	case "mysql":
		return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true",
			config.Username, config.Password, config.Host, config.Port, config.Database), nil

	default:
		return "", fmt.Errorf("unsupported driver: %s", config.Driver)
	}
}
