package executor

import (
	"context"
	"io"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/resource"
)

type contentComponentWriter struct {
	writer     *engineResourceWriter
	components []resource.ComponentRef
}

func newContentComponentWriter(provider engineplugin.ContentWritableProvider, connInfo engineplugin.ConnectionInfo, targetPath engineplugin.CatalogPath, targetWrite engineplugin.WriteOptions, specs []resource.ComponentSpec) resource.ComponentWriter {
	return &contentComponentWriter{
		writer:     newEngineResourceWriter(provider, connInfo, targetPath, targetWrite),
		components: resource.SameBasenameComponents(targetPath.StringPath(), specs),
	}
}

func (w *contentComponentWriter) Components() []resource.ComponentRef {
	if w == nil {
		return nil
	}
	return append([]resource.ComponentRef(nil), w.components...)
}

func (w *contentComponentWriter) CreateComponent(ctx context.Context, component resource.ComponentRef) (io.WriteCloser, error) {
	if w == nil || w.writer == nil {
		return nil, resource.ErrComponentNotFound
	}
	return w.writer.Create(ctx, component.ResourceRef)
}

func (w *contentComponentWriter) CommitComponents(ctx context.Context) error {
	return nil
}

func (w *contentComponentWriter) AbortComponents(ctx context.Context) error {
	return nil
}
