package extractors

import (
	"github.com/addp/common/format"
	"context"
	"database/sql"
	"fmt"
	"github.com/addp/common/format/sqlite"
	"io"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

// SQLiteExtractor SQLite数据库文件的元数据提取器
type SQLiteExtractor struct{}

func (e *SQLiteExtractor) SupportedTypes() []string {
	return []string{
		"application/x-sqlite3",
		"application/vnd.sqlite3",
		"application/octet-stream", // SQLite文件可能被识别为通用二进制
	}
}

func (e *SQLiteExtractor) Priority() int {
	// 优先级75，低于PDF (80)，但高于Default (50)
	return 75
}

func (e *SQLiteExtractor) Extract(ctx context.Context, input format.ExtractInput) (*format.ExtractedMetadata, error) {
	// 1. 将内容写入临时文件（SQLite需要文件路径）
	tmpFile, err := os.CreateTemp("", "sqlite-extract-*.db")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() {
		tmpFile.Close()
		os.Remove(tmpPath)
	}()

	// 2. 复制内容到临时文件
	written, err := io.Copy(tmpFile, input.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to write temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return nil, fmt.Errorf("failed to close temp file: %w", err)
	}

	// 3. 验证是否为有效的SQLite文件
	dsn := fmt.Sprintf("file:%s?mode=ro&_query_only=1&_busy_timeout=5000", strings.ReplaceAll(tmpPath, "\\", "/"))
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("not a valid SQLite database: %w", err)
	}
	defer db.Close()

	// 4. 提取数据库元数据
	options := sqlite.DefaultOptions()
	options.TableLimit = 10
	options.SampleRowLimit = 0
	analysis, err := sqlite.Analyze(ctx, db, &options)
	if err != nil {
		return nil, fmt.Errorf("failed to extract SQLite metadata: %w", err)
	}

	// 5. 构建基础元数据
	metadata := &format.ExtractedMetadata{
		BasicInfo: format.BasicMetadata{
			FileName:     filepath.Base(input.ObjectKey),
			FileType:     "SQLite Database",
			Size:         written, // 使用实际写入的大小
			ContentType:  "application/x-sqlite3",
			LastModified: input.LastModified,
			ETag:         input.ETag,
		},
		CustomAttrs: make(map[string]interface{}),
	}

	// 6. 添加SQLite专用元数据
	metadata.CustomAttrs["sqlite_metadata"] = analysis.Metadata

	// 7. SQLite的schema信息已经包含在sqlite_metadata中
	// 如果需要统一的SchemaMetadata格式，可以在这里转换
	// 但SQLite数据库包含多个表，不适合用单一SchemaMetadata表示

	return metadata, nil
}
