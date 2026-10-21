package scanruntime

import (
	"context"
	"fmt"
	"path"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
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
	claimed     metaitem.ResourceClaimSet
	order       []string
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
		plan.order = append(plan.order, path)
		plan.progress.Operation("identifying", path)
		detectionReader := scanflow.ContentReaderForDepth(reader, depth)
		structureReader := ftpModelStructureReader(resource, reader, depth)
		if isRoot {
			candidates := scanflow.FileCatalogNonExclusiveCandidateSet(resource.ID, path, d.files, d.subdirs)
			candidates.StructureReader = structureReader
			d.detection, d.detectionErr = scanflow.ResolveNonExclusiveContentCandidates(ctx, detectionReader, conn, resource.ID, candidates)
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
				candidates := scanflow.FileCatalogDirectoryCandidateSet(resource.ID, path, d.files, d.subdirs, files, dirs)
				candidates.StructureReader = structureReader
				d.detection, d.detectionErr = scanflow.ResolveContentCandidates(ctx, detectionReader, conn, resource.ID, candidates)
			}
		}
		if d.detectionErr != nil {
			plan.complete = false
		}

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
	total, err := plan.normalizeItems(resource.ID)
	if err != nil {
		return nil, err
	}
	plan.progress.Update(func(p *models.ScanProgress) { p.ItemsPlanned = total })
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

// normalizeItems assigns exactly one processing owner to each canonical identity.
// Ancestor claims remain effective across the inventory; processing and force
// reconciliation both consume the normalized owner directory.
func (p *fileScanPlan) normalizeItems(engineID uint) (int64, error) {
	type plannedItem struct {
		item   *metaitem.DetectedItem
		owner  string
		native bool
	}
	selected := map[string]plannedItem{}
	fingerprints := []string{}
	active := map[string]bool{}
	p.claimed = metaitem.ResourceClaimSet{}
	for _, dir := range p.order {
		active[dir] = true
	}
	for _, dir := range p.order {
		d := p.directories[dir]
		if d.detection == nil {
			continue
		}
		for path, claimed := range d.detection.Claims {
			if claimed {
				p.claimed[path] = true
			}
		}
		for _, item := range d.detection.Items {
			planned, ok := scanresource.PlanFileDetectedItem(engineID, dir, item, plugin.EngineCatalogTermFile)
			if !ok {
				continue
			}
			p.claimed[planned.FullName] = true
			owner := dir
			if item.Layout != format.LayoutWhole {
				owner = path.Dir(planned.FullName)
				if owner == "." {
					owner = ""
				}
			}
			if !active[owner] {
				return 0, fmt.Errorf("logical file item %q has no owner directory in the scan plan", planned.FullName)
			}
			previous, exists := selected[planned.Fingerprint]
			if !exists {
				fingerprints = append(fingerprints, planned.Fingerprint)
			}
			if !exists || (!previous.native && dir == owner) {
				selected[planned.Fingerprint] = plannedItem{item: item, owner: owner, native: dir == owner}
			}
		}
		d.detection.Items = nil
	}
	for _, fingerprint := range fingerprints {
		selectedItem := selected[fingerprint]
		d := p.directories[selectedItem.owner]
		if d.detection == nil {
			d.detection = &metaitem.DetectionResult{}
		}
		d.detection.Items = append(d.detection.Items, selectedItem.item)
	}
	total := int64(len(fingerprints))
	for _, dir := range p.order {
		d := p.directories[dir]
		if d.detection != nil && d.detection.Exclusive {
			continue
		}
		for _, file := range d.files {
			if !p.claimed[file.Path] {
				total++
			}
		}
	}
	return total, nil
}
