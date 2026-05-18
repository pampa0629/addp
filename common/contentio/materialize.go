package contentio

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
	Files    map[Ref]string
}

func MaterializeScope(ctx context.Context, reader Reader, scope Ref, localDir string) (*MaterializeResult, error) {
	if reader == nil {
		return nil, fmt.Errorf("content reader is required")
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
		Files:    make(map[Ref]string, len(refs)),
	}
	for _, ref := range refs {
		relativePath := strings.TrimPrefix(strings.Trim(ref.Path, "/"), scopePrefix)
		if relativePath == "" {
			continue
		}
		localPath := filepath.Join(localDir, filepath.FromSlash(relativePath))
		if err := materializeContent(ctx, reader, ref, localPath); err != nil {
			return nil, err
		}
		result.Files[ref] = localPath
	}
	if len(result.Files) == 0 {
		return nil, ErrContentNotFound
	}
	return result, nil
}

func materializeContent(ctx context.Context, reader Reader, ref Ref, localPath string) error {
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
