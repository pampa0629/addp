package shapefile

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/addp/common/contentio"
	"github.com/addp/common/format"
)

func materializeRefs(ctx context.Context, reader contentio.Reader, refs []format.RelatedRef) (tempDir string, basePath string, cleanup func(), err error) {
	tempDir, err = os.MkdirTemp("", "shapefile-refs-*")
	if err != nil {
		return "", "", nil, err
	}
	cleanup = func() {
		os.RemoveAll(tempDir)
	}
	primaryRef, err := format.PrimaryRelatedRef(refs)
	if err != nil {
		cleanup()
		return "", "", nil, fmt.Errorf("invalid shapefile related refs: %w", err)
	}

	var mainLocalPath string
	for _, ref := range refs {
		localPath := filepath.Join(tempDir, filepath.Base(ref.Ref.Path))
		if err := materializeRef(ctx, reader, ref, localPath); err != nil {
			if ref.Required {
				cleanup()
				return "", "", nil, fmt.Errorf("failed to read required ref %s: %w", ref.Ref.Path, err)
			}
			continue
		}
		if ref.Ref.Path == primaryRef.Ref.Path {
			mainLocalPath = localPath
		}
	}
	if mainLocalPath == "" {
		cleanup()
		return "", "", nil, fmt.Errorf("main ref missing")
	}
	return tempDir, strings.TrimSuffix(mainLocalPath, filepath.Ext(mainLocalPath)), cleanup, nil
}

func materializeRef(ctx context.Context, reader contentio.Reader, ref format.RelatedRef, destPath string) error {
	src, err := reader.Open(ctx, ref.Ref)
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
