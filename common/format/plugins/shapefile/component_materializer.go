package shapefile

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/addp/common/resource"
)

func materializeComponents(ctx context.Context, components resource.ComponentReader) (tempDir string, basePath string, cleanup func(), err error) {
	tempDir, err = os.MkdirTemp("", "shapefile-components-*")
	if err != nil {
		return "", "", nil, err
	}
	cleanup = func() {
		os.RemoveAll(tempDir)
	}

	var mainLocalPath string
	for _, component := range components.Components() {
		localPath := filepath.Join(tempDir, filepath.Base(component.Path))
		if err := materializeComponent(ctx, components, component, localPath); err != nil {
			if component.Required {
				cleanup()
				return "", "", nil, fmt.Errorf("failed to read required component %s: %w", component.Path, err)
			}
			continue
		}
		if component.ComponentRole == "main" {
			mainLocalPath = localPath
		}
	}
	if mainLocalPath == "" {
		cleanup()
		return "", "", nil, fmt.Errorf("main component missing")
	}
	return tempDir, strings.TrimSuffix(mainLocalPath, filepath.Ext(mainLocalPath)), cleanup, nil
}

func materializeComponent(ctx context.Context, components resource.ComponentReader, component resource.ComponentRef, destPath string) error {
	src, err := components.OpenComponent(ctx, component)
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
