package executor

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/resource"
)

type contentComponentWriter struct {
	provider    engineplugin.ContentWritableProvider
	connInfo    engineplugin.ConnectionInfo
	targetPath  engineplugin.CatalogPath
	targetWrite engineplugin.WriteOptions
	components  []resource.ComponentRef
}

func newContentComponentWriter(provider engineplugin.ContentWritableProvider, connInfo engineplugin.ConnectionInfo, targetPath engineplugin.CatalogPath, targetWrite engineplugin.WriteOptions, specs []resource.ComponentSpec) resource.ComponentWriter {
	return &contentComponentWriter{
		provider:    provider,
		connInfo:    connInfo,
		targetPath:  targetPath,
		targetWrite: targetWrite,
		components:  resource.SameBasenameComponents(targetPath.StringPath(), specs),
	}
}

func (w *contentComponentWriter) Components() []resource.ComponentRef {
	if w == nil {
		return nil
	}
	return append([]resource.ComponentRef(nil), w.components...)
}

func (w *contentComponentWriter) CreateComponent(ctx context.Context, component resource.ComponentRef) (io.WriteCloser, error) {
	if w == nil || w.provider == nil {
		return nil, fmt.Errorf("content component writer requires content writable provider")
	}
	path, err := componentCatalogPath(w.targetPath, component)
	if err != nil {
		return nil, err
	}
	return w.provider.CreateContent(ctx, w.connInfo, path, w.targetWrite)
}

func (w *contentComponentWriter) CommitComponents(ctx context.Context) error {
	return nil
}

func (w *contentComponentWriter) AbortComponents(ctx context.Context) error {
	return nil
}

func componentCatalogPath(target engineplugin.CatalogPath, component resource.ComponentRef) (engineplugin.CatalogPath, error) {
	if len(target.Segments) == 0 {
		return engineplugin.CatalogPath{}, fmt.Errorf("component target path requires at least one segment")
	}
	componentName := filepath.Base(component.Path)
	if componentName == "." || componentName == "/" || componentName == "" {
		return engineplugin.CatalogPath{}, fmt.Errorf("component path %q has no file name", component.Path)
	}
	next := engineplugin.CatalogPath{
		Version:  target.Version,
		EngineID: target.EngineID,
		Segments: append([]engineplugin.CatalogSegment(nil), target.Segments...),
	}
	if next.Version == "" {
		next.Version = engineplugin.CatalogPathVersion
	}
	last := &next.Segments[len(next.Segments)-1]
	last.Name = componentName
	if strings.TrimSpace(last.Term) == "" {
		last.Term = engineplugin.CatalogTermFile
	}
	if strings.TrimSpace(last.Kind) == "" {
		last.Kind = engineplugin.CatalogKindFile
	}
	return next, nil
}
