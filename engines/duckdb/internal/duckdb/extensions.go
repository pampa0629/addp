package duckdb

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var duckDBExtensionPart = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

const DuckDBVersion = "v1.4.5"

var requiredExtensions = []struct {
	loadName string
	fileName string
}{
	{loadName: "httpfs", fileName: "httpfs"},
	{loadName: "mysql", fileName: "mysql_scanner"},
	{loadName: "postgres", fileName: "postgres_scanner"},
	{loadName: "spatial", fileName: "spatial"},
}

func ensureDuckDBExtension(ctx context.Context, conn *sql.Conn, loadName, fileName string) error {
	if conn == nil || !duckDBExtensionPart.MatchString(loadName) || !duckDBExtensionPart.MatchString(fileName) {
		return fmt.Errorf("DuckDB 扩展参数无效")
	}
	if _, err := conn.ExecContext(ctx, "SET autoinstall_known_extensions = false"); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, "SET autoload_known_extensions = false"); err != nil {
		return err
	}
	loadTarget := loadName
	if directory := strings.TrimSpace(os.Getenv("DUCKDB_EXTENSION_DIRECTORY")); directory != "" {
		loadTarget = duckDBString(filepath.Join(directory, fileName+".duckdb_extension"))
	}
	if _, err := conn.ExecContext(ctx, "LOAD "+loadTarget); err != nil {
		return fmt.Errorf("加载本地 DuckDB 扩展 %s 失败: %w", loadName, err)
	}
	return nil
}

// VerifyRequiredExtensions fails startup when an extension is missing or does
// not match the embedded DuckDB version/platform. Query handling never installs
// or downloads extensions.
func VerifyRequiredExtensions(ctx context.Context) error {
	db, err := OpenDB()
	if err != nil {
		return err
	}
	defer db.Close()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var version string
	if err := conn.QueryRowContext(ctx, "SELECT version()").Scan(&version); err != nil {
		return fmt.Errorf("读取 DuckDB 版本失败: %w", err)
	}
	if version != DuckDBVersion {
		return fmt.Errorf("DuckDB 版本不匹配: runtime=%s extensions=%s", version, DuckDBVersion)
	}
	for _, extension := range requiredExtensions {
		if err := ensureDuckDBExtension(ctx, conn, extension.loadName, extension.fileName); err != nil {
			return err
		}
	}
	return nil
}

func RequiredExtensionFileNames() []string {
	files := make([]string, 0, len(requiredExtensions))
	for _, extension := range requiredExtensions {
		files = append(files, extension.fileName+".duckdb_extension")
	}
	return files
}

func CurrentPlatform(ctx context.Context) (string, error) {
	db, err := OpenDB()
	if err != nil {
		return "", err
	}
	defer db.Close()
	var platform string
	if err := db.QueryRowContext(ctx, "PRAGMA platform").Scan(&platform); err != nil {
		return "", fmt.Errorf("读取 DuckDB 平台失败: %w", err)
	}
	if !duckDBExtensionPart.MatchString(platform) {
		return "", fmt.Errorf("DuckDB 返回了无效平台 %q", platform)
	}
	return platform, nil
}

func duckDBString(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
