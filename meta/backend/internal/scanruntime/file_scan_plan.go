package scanruntime

import (
	"context"
	"fmt"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scanresource"
)

type fileDirectoryPlan struct {
	files        []metaitem.StorageFileRef
	subdirs      []metaitem.StorageDirectoryRef
	ignored      []plugin.EngineCatalogEntry
	listingErr   error
	detection    *metaitem.DetectionResult
	detectionErr error
}

type fileScanPlan struct {
	directories map[string]*fileDirectoryPlan
	progress    *scanflow.FileProgress
	complete    bool
}

// prepareFileScan enumerates each physical directory once. Resolvers and processing
// consume this same inventory, including the descendants of a whole-layout item.
func (s *FilesystemCatalogRuntime) prepareFileScan(ctx context.Context, reader plugin.ContentReadableProvider, provider plugin.EngineCatalogProvider, conn plugin.ConnectionInfo, resource *commonModels.Engine, roots []string, depth string) (*fileScanPlan, error) {
	plan := &fileScanPlan{directories: make(map[string]*fileDirectoryPlan), progress: scanflow.FileProgressFrom(ctx), complete: true}
	plan.progress.Update(func(p *models.ScanProgress) { p.Phase = "discovering" })
	visiting := make(map[string]bool)
	var discover func(string) error
	discover = func(path string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if visiting[path] {
			return fmt.Errorf("cyclic file catalog directory %q", path)
		}
		if _, found := plan.directories[path]; found {
			return nil
		}
		visiting[path] = true
		defer delete(visiting, path)
		plan.progress.Operation("listing", path)
		files, dirs, ignored, err := s.listDirectoryWithIgnored(ctx, resource, provider, conn, path)
		d := &fileDirectoryPlan{files: files, subdirs: dirs, ignored: ignored, listingErr: err}
		plan.directories[path] = d
		if err != nil {
			plan.complete = false
			return nil
		}
		plan.progress.Update(func(p *models.ScanProgress) { p.FilesDiscovered += int64(len(files)); p.DirectoriesDiscovered++ })
		for _, dir := range dirs {
			if err := discover(dir.Path); err != nil {
				return err
			}
		}
		return nil
	}
	for _, path := range roots {
		if err := discover(path); err != nil {
			return nil, err
		}
	}
	plan.progress.Update(func(p *models.ScanProgress) { p.FilesTotalKnown = plan.complete; p.Phase = "identifying" })
	var identify func(string, bool) error
	identify = func(path string, isRoot bool) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		d := plan.directories[path]
		if d == nil || d.listingErr != nil {
			return nil
		}
		plan.progress.Operation("identifying", path)
		detectionReader := scanflow.ContentReaderForDepth(reader, depth)
		if isRoot {
			d.detection, d.detectionErr = resolveNonExclusiveScopeItems(ctx, detectionReader, conn, resource, path, d.files, d.subdirs)
		} else {
			var files []metaitem.StorageFileRef
			var dirs []metaitem.StorageDirectoryRef
			var err error
			if len(d.subdirs) > 0 {
				files, dirs, err = plan.recursive(path)
			}
			if err != nil {
				d.detectionErr = err
			} else {
				d.detection, d.detectionErr = scanflow.DetectFileCatalogDirectoryItems(ctx, detectionReader, conn, resource.ID, path, d.files, d.subdirs, files, dirs)
			}
		}
		if d.detectionErr != nil {
			plan.complete = false
		}
		count := int64(0)
		claims := metaitem.ResourceClaimSet{}
		if d.detection != nil {
			claims = d.detection.Claims
			for _, item := range d.detection.Items {
				if item != nil {
					if _, ok := scanresource.PlanFileDetectedItem(resource.ID, path, item, plugin.EngineCatalogTermFile); ok {
						count++
					}
				}
			}
		}
		if d.detection == nil || !d.detection.Exclusive {
			for _, file := range d.files {
				if !claims[file.Path] {
					count++
				}
			}
		}
		plan.progress.Update(func(p *models.ScanProgress) { p.ItemsPlanned += count })
		if d.detection != nil && d.detection.Exclusive {
			return nil
		}
		for _, dir := range d.subdirs {
			if err := identify(dir.Path, false); err != nil {
				return err
			}
		}
		return nil
	}
	for _, path := range roots {
		if err := identify(path, path == ""); err != nil {
			return nil, err
		}
	}
	plan.progress.Update(func(p *models.ScanProgress) {
		p.ItemsTotalKnown = plan.complete
		p.Phase = "processing"
		p.Operation = ""
		p.CurrentPath = ""
	})
	return plan, nil
}

// recursive returns the cached subtree, preserving listing failures rather than
// passing an incomplete inventory to an exclusive dataset resolver.
func (p *fileScanPlan) recursive(path string) ([]metaitem.StorageFileRef, []metaitem.StorageDirectoryRef, error) {
	d := p.directories[path]
	if d == nil {
		return nil, nil, fmt.Errorf("directory missing from file scan inventory: %q", path)
	}
	if d.listingErr != nil {
		return nil, nil, d.listingErr
	}
	files := append([]metaitem.StorageFileRef(nil), d.files...)
	dirs := append([]metaitem.StorageDirectoryRef(nil), d.subdirs...)
	for _, child := range d.subdirs {
		childFiles, childDirs, err := p.recursive(child.Path)
		if err != nil {
			return nil, nil, err
		}
		files = append(files, childFiles...)
		dirs = append(dirs, childDirs...)
	}
	return files, dirs, nil
}
