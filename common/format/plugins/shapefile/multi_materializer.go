package shapefile

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/addp/common/contentio"
)

func materializeRefs(ctx context.Context, refs contentio.MultiReader) (tempDir string, basePath string, cleanup func(), err error) {
	tempDir, err = os.MkdirTemp("", "shapefile-refs-*")
	if err != nil {
		return "", "", nil, err
	}
	cleanup = func() {
		os.RemoveAll(tempDir)
	}

	var mainLocalPath string
	for _, ref := range refs.Refs() {
		localPath := filepath.Join(tempDir, filepath.Base(ref.Path))
		if err := materializeRef(ctx, refs, ref, localPath); err != nil {
			if ref.Required {
				cleanup()
				return "", "", nil, fmt.Errorf("failed to read required ref %s: %w", ref.Path, err)
			}
			continue
		}
		if ref.Role == contentio.RoleMain || ref.Primary {
			mainLocalPath = localPath
		}
	}
	if mainLocalPath == "" {
		cleanup()
		return "", "", nil, fmt.Errorf("main ref missing")
	}
	return tempDir, strings.TrimSuffix(mainLocalPath, filepath.Ext(mainLocalPath)), cleanup, nil
}

func materializeRef(ctx context.Context, refs contentio.MultiReader, ref contentio.Ref, destPath string) error {
	src, err := refs.Open(ctx, ref)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer dst.Close()
	_, err = io.Copy(dst, src)
	return err
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
