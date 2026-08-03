package duckdb

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const OfficialExtensionRepository = "https://extensions.duckdb.org"

func PrepareRequiredExtensions(
	ctx context.Context,
	httpClient *http.Client,
	repositoryURL, platform, outputDirectory string,
) error {
	if httpClient == nil {
		return fmt.Errorf("HTTP client is required")
	}
	repositoryURL = strings.TrimRight(strings.TrimSpace(repositoryURL), "/")
	platform = strings.TrimSpace(platform)
	outputDirectory = strings.TrimSpace(outputDirectory)
	if repositoryURL == "" || !duckDBExtensionPart.MatchString(platform) || outputDirectory == "" {
		return fmt.Errorf("DuckDB extension preparation parameters are invalid")
	}
	if err := os.MkdirAll(outputDirectory, 0o755); err != nil {
		return fmt.Errorf("create DuckDB extension directory: %w", err)
	}
	for _, fileName := range RequiredExtensionFileNames() {
		target := filepath.Join(outputDirectory, fileName)
		if info, err := os.Stat(target); err == nil && info.Mode().IsRegular() && info.Size() > 0 {
			continue
		}
		url := fmt.Sprintf("%s/%s/%s/%s.gz", repositoryURL, DuckDBVersion, platform, fileName)
		if err := downloadGzipFile(ctx, httpClient, url, target); err != nil {
			return err
		}
	}
	return nil
}

func downloadGzipFile(ctx context.Context, httpClient *http.Client, url, target string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create DuckDB extension request: %w", err)
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("download DuckDB extension %s: %w", filepath.Base(target), err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download DuckDB extension %s returned HTTP %d", filepath.Base(target), response.StatusCode)
	}
	compressed, err := gzip.NewReader(response.Body)
	if err != nil {
		return fmt.Errorf("open DuckDB extension archive %s: %w", filepath.Base(target), err)
	}
	defer compressed.Close()
	temporary, err := os.CreateTemp(filepath.Dir(target), ".duckdb-extension-*")
	if err != nil {
		return fmt.Errorf("create DuckDB extension temporary file: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if _, err := io.Copy(temporary, compressed); err != nil {
		temporary.Close()
		return fmt.Errorf("write DuckDB extension %s: %w", filepath.Base(target), err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync DuckDB extension %s: %w", filepath.Base(target), err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close DuckDB extension %s: %w", filepath.Base(target), err)
	}
	if err := os.Chmod(temporaryName, 0o644); err != nil {
		return fmt.Errorf("set DuckDB extension permissions: %w", err)
	}
	if err := os.Rename(temporaryName, target); err != nil {
		return fmt.Errorf("publish DuckDB extension %s: %w", filepath.Base(target), err)
	}
	return nil
}
