package preview

import (
	"context"
	"strings"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
)

// A parent index is committed Meta data. Every FileGDB content request must
// instead carry a current-user check before source resolution or runtime I/O.
func checkWholeContainerRead(ctx context.Context, req *PreviewResolverRequest, source *PreviewRequest) error {
	if string(formatTypeFromMetaAttributes(source.Attributes)) != "filegdb" {
		return nil
	}
	if itemLayoutFromMetaAttributes(source.Attributes) != "whole" || itemDataTypeFromMetaAttributes(source.Attributes) != "container" {
		return ErrEngineAccessDenied
	}
	if source.ChildName == "" && source.RefPath == "" && source.NestedChildPath == "" {
		return nil
	}
	if req.Engine.EngineType != "nfs" ||
		source.ChildName == "" || source.RefPath != "" || source.NestedChildPath != "" || req.CheckWholeContainerRead == nil {
		return ErrEngineAccessDenied
	}
	root := plugin.FileDirectoryPath(req.Engine.ID, source.ScopePath)
	if req.Locator == nil || req.Locator.EngineID != req.Engine.ID || strings.Join(req.Locator.Path, "/") != source.ScopePath || !authorizationfacts.ValidWholeScopeRoot(root) || root.StringPath() != source.ScopePath || req.ItemFullName != source.ScopePath ||
		req.ItemFingerprint != models.GenerateItemFingerprint(req.Engine.ID, source.ScopePath) {
		return ErrEngineAccessDenied
	}
	return req.CheckWholeContainerRead(ctx, root)
}
