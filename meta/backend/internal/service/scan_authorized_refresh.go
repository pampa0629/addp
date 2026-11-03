package service

import (
	"context"
	"database/sql"
	"reflect"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/authorization"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scanresource"
	"gorm.io/gorm"
)

// Each selected node is dispatched directly; precise items never pass through
// directory discovery. The maximum comes only from the frozen owner plan.
func (s *ScanService) scanAuthorizedRefresh(opts scanflow.Options, scope *authorization.MetadataRefreshScope) (*models.ScanResponse, error) {
	start := time.Now()
	ctx := opts.Context
	if scope.Validate() != nil || scope.TenantID != int64(opts.TenantID) || scope.Query.Target.EngineID != opts.EngineID {
		return nil, commonapi.ErrForbidden
	}
	resource, err := s.engineService.GetResourceByIDWithContext(ctx, opts.EngineID, opts.TenantID)
	if err != nil {
		return nil, err
	}
	p, err := s.engineService.ResolveScanPlugin(ctx, resource, opts.TenantID)
	if err != nil {
		return nil, err
	}
	ranges := minimalRefreshRanges(scope.Ranges)
	counts := scanflow.ScanCounts{}
	seen := map[string]bool{}
	refreshItem := func(callCtx context.Context, r authorization.MetadataRefreshRange) error {
		if r.Item == nil || seen[r.Item.ItemFingerprint] {
			return nil
		}
		if err := scanresource.CheckCollectionRead(callCtx, resource); err != nil {
			return err
		}
		// Use the same committed item/parent snapshot for proof and execution.
		// Re-reading mutable attributes after validating their digest could change
		// the physical refs without changing this execution's frozen maximum.
		var item models.MetaItem
		var parent models.MetaNode
		var refs []plugin.EngineCatalogPath
		err := s.db.WithContext(callCtx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Where("tenant_id=? AND engine_id=? AND fingerprint=?", opts.TenantID, resource.ID, r.Item.ItemFingerprint).First(&item).Error; err != nil {
				return err
			}
			lookup, err := loadMetadataIdentityNodes(tx, opts.TenantID, []models.MetaItem{item})
			if err != nil {
				return err
			}
			facts, err := projectDataItemAuthorizationFacts(item, tx.NowFunc().UTC(), lookup)
			if err != nil {
				return err
			}
			binding, err := facts.MetadataBinding()
			if err != nil || !reflect.DeepEqual(binding, *r.Item) {
				return commonapi.ErrForbidden
			}
			parent, err = lookup(opts.TenantID, resource.ID, item.NodeID)
			if err != nil {
				return err
			}
			if binding.MetadataTarget != nil && binding.MetadataTarget.Layout == "multi" {
				descriptor := scanflow.KnownItemDescriptorFromAttributes(item.Attributes)
				if len(descriptor.Refs) == 0 {
					return commonapi.ErrForbidden
				}
				for _, ref := range descriptor.Refs {
					target := plugin.FileItemPath(resource.ID, ref.Path)
					if ref.Path == "" || target.StringPath() != ref.Path {
						return commonapi.ErrForbidden
					}
					if _, _, err := authorizationfacts.MetadataTargetIdentity(target); err != nil {
						return commonapi.ErrForbidden
					}
					refs = append(refs, target)
				}
			}
			return nil
		}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
		if err != nil {
			return err
		}
		local := *scope
		local.Ranges = []authorization.MetadataRefreshRange{r}
		callCtx = scanresource.WithMetadataRefreshScope(callCtx, &local)
		callCtx = scanresource.WithVerifiedRefreshItemRefs(callCtx, refs)
		result, err := s.runtimes.ItemRefresh.RefreshKnownItemWithPlugin(callCtx, p, resource, opts.TenantID, item, parent, opts.Encoding, opts.HasHeader, opts.ScanDepth)
		if err != nil {
			return err
		}
		seen[item.Fingerprint] = true
		counts.Items++
		counts.Fields += result.Fields
		counts.Extraction = scanflow.MergeExtractionCounts(counts.Extraction, result.Extraction)
		return nil
	}
	for _, r := range ranges {
		if err := scanresource.CheckCollectionRead(ctx, resource); err != nil {
			return nil, err
		}
		if r.Item != nil {
			if err := refreshItem(ctx, r); err != nil {
				return nil, err
			}
			continue
		}
		local := *scope
		local.Ranges = []authorization.MetadataRefreshRange{r}
		callCtx := scanresource.WithMetadataRefreshScope(ctx, &local)
		if resource.EngineType == "postgresql" {
			if len(r.Node.Target.Segments) != 2 {
				return nil, commonapi.ErrForbidden
			}
			nodes, items, fields, err := s.runtimes.Database.ScanNamespace(callCtx, p, resource, opts.TenantID, resource.ID, r.Node.Target.Segments[1].Name, opts.ScanDepth, opts.Force)
			if err != nil {
				return nil, err
			}
			counts.CatalogNodes += nodes
			counts.Items += items
			counts.Fields += fields
			continue
		}
		if resource.EngineType != "nfs" {
			return nil, commonapi.ErrForbidden
		}
		if !r.Node.Recursive {
			callCtx = scanresource.WithDirectDirectoryRefresh(callCtx)
		}
		result, err := s.runtimes.FilesystemCatalog.ScanPaths(callCtx, resource, opts.TenantID, []string{r.Node.Target.StringPath()}, opts.ScanDepth, opts.Force, opts.Reporter)
		if err != nil {
			return nil, err
		}
		counts.CatalogNodes += result.CatalogNodes
		counts.Items += result.Items
		counts.Extraction = scanflow.MergeExtractionCounts(counts.Extraction, result.Extraction)
		if !r.Node.Recursive {
			// Known whole DataItems directly below this node are single members of
			// a nonrecursive range. Unknown child directories are never enumerated.
			items, err := s.directWholeRefreshItems(ctx, opts.TenantID, resource, r.Node.Target)
			if err != nil {
				return nil, err
			}
			for _, item := range items {
				facts, err := NewMetadataQueryService(s.db).GetDataItemAuthorizationFacts(ctx, opts.TenantID, item.Fingerprint)
				if err != nil {
					return nil, err
				}
				binding, err := facts.MetadataBinding()
				if err != nil || !r.Node.CoversBinding(binding) {
					return nil, commonapi.ErrForbidden
				}
				if err := refreshItem(ctx, authorization.MetadataRefreshRange{Item: &binding}); err != nil {
					return nil, err
				}
			}
		}
	}
	return scanflow.NewScanResponse("success", "metadata refreshed", counts, start, time.Now()), nil
}

func minimalRefreshRanges(ranges []authorization.MetadataRefreshRange) []authorization.MetadataRefreshRange {
	result := make([]authorization.MetadataRefreshRange, 0, len(ranges))
	for i, r := range ranges {
		covered := false
		for j, other := range ranges {
			if i == j || other.Node == nil {
				continue
			}
			if (r.Node != nil && other.Node.CoversNode(*r.Node)) || (r.Item != nil && other.Node.CoversBinding(*r.Item)) {
				covered = true
				break
			}
		}
		if !covered {
			result = append(result, r)
		}
	}
	return result
}

func (s *ScanService) directWholeRefreshItems(ctx context.Context, tenant uint, resource *commonmodels.Engine, target plugin.EngineCatalogPath) ([]models.MetaItem, error) {
	parent, err := NewMetadataQueryService(s.db).ResolveNodeIdentity(ctx, tenant, authorizationfacts.NodeIdentityQuery{Target: target})
	if err != nil || parent == nil || !parent.Exists {
		return nil, commonapi.ErrForbidden
	}
	var node models.MetaNode
	if err := s.db.WithContext(ctx).Where("tenant_id=? AND engine_id=? AND node_type='dir' AND full_name=?", tenant, resource.ID, target.StringPath()).First(&node).Error; err != nil {
		return nil, err
	}
	var items []models.MetaItem
	err = s.db.WithContext(ctx).Model(&models.MetaItem{}).Joins("JOIN meta.meta_node child ON child.id=meta.meta_item.node_id AND child.tenant_id=meta.meta_item.tenant_id AND child.engine_id=meta.meta_item.engine_id AND child.deleted_at IS NULL").
		Where("meta.meta_item.tenant_id=? AND meta.meta_item.engine_id=? AND child.parent_node_id=? AND meta.meta_item.attributes -> 'item' ->> 'layout'='whole'", tenant, resource.ID, node.ID).Find(&items).Error
	return items, err
}
