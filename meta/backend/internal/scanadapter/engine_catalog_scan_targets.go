package scanadapter

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/meta/internal/scanflow"
)

type catalogLeafScanner interface {
	ScanLeaf(context.Context, plugin.EnginePlugin, *commonModels.Engine, uint, plugin.EngineCatalogEntry, string, bool) (int, int, int, error)
}

// Resolve every target before scanning any of them. A malformed or unsupported
// target must never disappear into the empty-scope engine-wide scan.
func (d *EngineCatalogScanDispatcher) dispatchTargets(p plugin.EnginePlugin, plan scanflow.EngineCatalogScanPlan, req scanflow.DispatchRequest) (scanflow.DispatchResult, error) {
	model := scanflow.EngineCatalogModelForPlugin(p)
	provider, ok := p.(plugin.EngineCatalogProvider)
	if model == nil || !ok {
		return scanflow.DispatchResult{}, fmt.Errorf("precise scan requires a catalog model and EngineCatalogProvider")
	}
	if len(req.CatalogPaths) > 0 || len(req.RefGroups) > 0 {
		return scanflow.DispatchResult{}, fmt.Errorf("locator targets cannot be combined with another scan selector")
	}
	type target struct {
		path  plugin.EngineCatalogPath
		entry *plugin.EngineCatalogEntry
	}
	targets := make([]target, 0, len(req.Targets))
	for _, uri := range scanflow.UniqueNonEmpty(req.Targets) {
		loc, err := resourcetree.ParseURI(uri)
		if err != nil || loc.EngineID != req.Resource.ID {
			return scanflow.DispatchResult{}, scanTargetResolutionFailure(uri, fmt.Errorf("invalid or cross-engine scan target"))
		}
		path, err := resourcetree.EngineCatalogPathFromLocator(*model, loc)
		if err != nil {
			return scanflow.DispatchResult{}, scanTargetResolutionFailure(uri, err)
		}
		t := target{path: path}
		if len(path.Segments) > 1 {
			t.entry, err = provider.ResolvePath(req.Context, plugin.ConnectionInfo(req.Resource.ConnectionInfo), path)
			if err != nil {
				return scanflow.DispatchResult{}, scanTargetResolutionFailure(uri, fmt.Errorf("resolve scan target: %w", err))
			}
			if t.entry == nil || !reflect.DeepEqual(t.entry.Path, path) || t.entry.Name != path.Segments[len(path.Segments)-1].Name {
				return scanflow.DispatchResult{}, scanTargetResolutionFailure(uri, fmt.Errorf("scan target was not resolved exactly"))
			}
			leaf := t.entry.Role == plugin.EngineCatalogRoleLeaf
			expectedLeaf := path.Segments[len(path.Segments)-1].Term == plugin.EngineCatalogLeafTerm(*model)
			if leaf != expectedLeaf {
				return scanflow.DispatchResult{}, scanTargetResolutionFailure(uri, fmt.Errorf("resolved scan target role differs from requested path"))
			}
			if !leaf && t.entry.Role != plugin.EngineCatalogRoleBranch {
				return scanflow.DispatchResult{}, scanTargetResolutionFailure(uri, fmt.Errorf("scan target has no catalog role"))
			}
			if plan.Strategy == scanflow.EngineCatalogScanTabular || plan.Strategy == scanflow.EngineCatalogScanBranchLeaves {
				if (leaf && len(path.Segments) != 3) || (!leaf && len(path.Segments) != 2) {
					return scanflow.DispatchResult{}, scanTargetResolutionFailure(uri, fmt.Errorf("unsupported catalog scan path depth"))
				}
			}
		}
		targets = append(targets, t)
	}
	if len(targets) == 0 {
		return scanflow.DispatchResult{}, fmt.Errorf("scan targets are empty")
	}
	result := scanflow.DispatchResult{}
	failures := &scanflow.FailedTargetCollector{}
	if req.Reporter != nil {
		req.Reporter.SetTotal(len(targets))
	}
	for i, t := range targets {
		if err := req.Context.Err(); err != nil {
			return result, err
		}
		var part scanflow.DispatchResult
		var err error
		if t.entry != nil && t.entry.Role == plugin.EngineCatalogRoleLeaf {
			if plan.Strategy == scanflow.EngineCatalogScanObject || plan.Strategy == scanflow.EngineCatalogScanFile {
				if d.contentScanner == nil {
					err = fmt.Errorf("content catalog scanner is nil")
				} else if plan.Strategy == scanflow.EngineCatalogScanObject {
					part, err = d.contentScanner.ScanObjectCatalogLeaf(req, *t.entry)
				} else {
					part, err = d.contentScanner.ScanFileCatalogLeaf(req, *t.entry)
				}
			} else {
				var scanner catalogLeafScanner
				switch plan.Strategy {
				case scanflow.EngineCatalogScanTabular:
					scanner, _ = d.namespaceScan.(catalogLeafScanner)
				case scanflow.EngineCatalogScanBranchLeaves:
					scanner, _ = d.branchScan.(catalogLeafScanner)
				case scanflow.EngineCatalogScanDirectLeaves:
					scanner, _ = d.directLeafScan.(catalogLeafScanner)
				}
				if scanner == nil {
					err = fmt.Errorf("catalog scanner does not support precise leaves")
				} else {
					part, err = d.scanTargetLeaf(scanner, p, plan, req, *t.entry)
				}
			}
		} else {
			sub := req
			sub.Targets = nil
			sub.Reporter = nil
			if t.entry != nil {
				sub.CatalogPaths = []string{t.path.StringPath()}
			}
			part, err = d.Dispatch(sub)
		}
		result.CatalogNodes += part.CatalogNodes
		result.Items += part.Items
		result.Fields += part.Fields
		result.Extraction = scanflow.MergeExtractionCounts(result.Extraction, part.Extraction)
		failures.Add(t.path.StringPath(), err)
		if req.Reporter != nil {
			req.Reporter.Advance(t.path.StringPath(), i+1, len(targets), map[string]interface{}{"items": part.Items, "fields": part.Fields})
		}
	}
	return result, failures.Err()
}

func (d *EngineCatalogScanDispatcher) scanTargetLeaf(scanner catalogLeafScanner, p plugin.EnginePlugin, plan scanflow.EngineCatalogScanPlan, req scanflow.DispatchRequest, entry plugin.EngineCatalogEntry) (scanflow.DispatchResult, error) {
	// Share the branch lock with full branch scans, so cleanup cannot race a leaf.
	if d.locker != nil {
		name := entry.Path.Segments[1].Name
		if plan.Strategy == scanflow.EngineCatalogScanDirectLeaves {
			name = ""
		}
		key := d.locker.GenerateBranchLockKey(req.TenantID, req.Resource.ID, name)
		if plan.Strategy == scanflow.EngineCatalogScanTabular {
			key = d.locker.GenerateNamespaceLockKey(req.TenantID, req.Resource.ID, name)
		}
		acquired, err := d.locker.TryAcquireLock(req.Context, key, 2*time.Hour)
		if err != nil {
			return scanflow.DispatchResult{}, err
		}
		if !acquired {
			return scanflow.DispatchResult{}, fmt.Errorf("scan target range is already running")
		}
		defer d.clearLock(req.Context, true, key, "清除扫描目标锁失败", "target", entry.Name)
	}
	nodes, items, fields, err := scanner.ScanLeaf(req.Context, p, req.Resource, req.TenantID, entry, req.ScanDepth, req.Force)
	return scanflow.DispatchResult{CatalogNodes: nodes, Items: items, Fields: fields}, err
}

func scanTargetResolutionFailure(target string, err error) error {
	failures := &scanflow.FailedTargetCollector{}
	failures.Add(target, err)
	return failures.Err()
}
