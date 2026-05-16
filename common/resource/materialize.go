package resource

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type MaterializeResult struct {
	LocalDir string
	Files    map[ResourceRef]string
}

func MaterializeResourceScope(ctx context.Context, reader ResourceReader, scope ResourceRef, localDir string) (*MaterializeResult, error) {
	if reader == nil {
		return nil, fmt.Errorf("resource reader is required")
	}
	if err := os.MkdirAll(localDir, 0755); err != nil {
		return nil, err
	}

	refs, err := reader.List(ctx, scope)
	if err != nil {
		return nil, err
	}
	scopePrefix := EnsurePrefix(strings.Trim(scope.Path, "/"))
	result := &MaterializeResult{
		LocalDir: localDir,
		Files:    make(map[ResourceRef]string, len(refs)),
	}
	for _, ref := range refs {
		relativePath := strings.TrimPrefix(strings.Trim(ref.Path, "/"), scopePrefix)
		if relativePath == "" {
			continue
		}
		localPath := filepath.Join(localDir, filepath.FromSlash(relativePath))
		if err := materializeResource(ctx, reader, ref, localPath); err != nil {
			return nil, err
		}
		result.Files[ref] = localPath
	}
	if len(result.Files) == 0 {
		return nil, ErrResourceNotFound
	}
	return result, nil
}

func materializeResource(ctx context.Context, reader ResourceReader, ref ResourceRef, localPath string) error {
	rc, err := reader.Open(ctx, ref)
	if err != nil {
		return err
	}
	defer rc.Close()

	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return err
	}
	file, err := os.Create(localPath)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(file, rc)
	return err
}

func EnsurePrefix(prefix string) string {
	prefix = strings.Trim(prefix, "/")
	if prefix == "" || strings.HasSuffix(prefix, "/") {
		return prefix
	}
	return prefix + "/"
}
