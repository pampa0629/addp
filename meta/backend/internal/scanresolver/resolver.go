package scanresolver

import (
	"fmt"

	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
	"gorm.io/gorm"
)

type Resolver struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Resolver {
	return &Resolver{db: db}
}

func (r *Resolver) ResolveScope(tenantID uint, opts scanflow.Options) (scanflow.Scope, error) {
	engineID, err := r.ResolveEngineID(tenantID, opts)
	if err != nil {
		return scanflow.Scope{}, err
	}

	scanDepth, err := scanflow.NormalizeScanDepth(opts.ScanDepth, scanflow.ScanDepthBasic)
	if err != nil {
		return scanflow.Scope{}, err
	}
	if opts.ItemID > 0 {
		return scanflow.Scope{
			EngineID:  engineID,
			Mode:      scanflow.ModeItem,
			Source:    scanflow.NormalizeSource(opts.Source),
			ScanDepth: scanDepth,
			Force:     opts.Force,
		}, nil
	}

	resolvedCatalogPaths, err := r.ResolveTargets(tenantID, opts)
	if err != nil {
		return scanflow.Scope{}, err
	}
	catalogPaths := scanflow.UniqueNonEmpty(append(opts.CatalogPaths, resolvedCatalogPaths...))

	return scanflow.Scope{
		EngineID:     engineID,
		Mode:         scanflow.ModeFor(opts, catalogPaths),
		CatalogPaths: catalogPaths,
		RefGroups:    scanflow.NormalizeRefGroups(opts.RefGroups),
		Source:       scanflow.NormalizeSource(opts.Source),
		ScanDepth:    scanDepth,
		Force:        opts.Force,
	}, nil
}

func (r *Resolver) ResolveEngineID(tenantID uint, opts scanflow.Options) (uint, error) {
	if opts.EngineID > 0 {
		return opts.EngineID, nil
	}
	if opts.NodeID > 0 {
		if r.db == nil {
			return 0, fmt.Errorf("node target resolver requires database")
		}
		var node models.MetaNode
		if err := r.db.Select("engine_id").Where("tenant_id = ? AND id = ?", tenantID, opts.NodeID).First(&node).Error; err != nil {
			return 0, fmt.Errorf("node target not found: %w", err)
		}
		return node.EngineID, nil
	}
	if opts.ItemID > 0 {
		if r.db == nil {
			return 0, fmt.Errorf("item target resolver requires database")
		}
		var item models.MetaItem
		if err := r.db.Select("engine_id").Where("tenant_id = ? AND id = ?", tenantID, opts.ItemID).First(&item).Error; err != nil {
			return 0, fmt.Errorf("item target not found: %w", err)
		}
		return item.EngineID, nil
	}
	for _, target := range opts.Targets {
		if id, ok := scanflow.EngineIDFromLocator(target); ok {
			return id, nil
		}
	}
	return 0, fmt.Errorf("engine_id is required")
}

func (r *Resolver) ResolveTargets(tenantID uint, opts scanflow.Options) ([]string, error) {
	catalogPaths := []string{}

	if opts.NodeID > 0 {
		if r.db == nil {
			return nil, fmt.Errorf("node target resolver requires database")
		}
		var node models.MetaNode
		if err := r.db.Where("tenant_id = ? AND id = ?", tenantID, opts.NodeID).First(&node).Error; err != nil {
			return nil, fmt.Errorf("node target not found: %w", err)
		}
		catalogPaths = append(catalogPaths, scanflow.TargetPathsFromNode(node)...)
	}

	if opts.ItemID > 0 {
		if r.db == nil {
			return nil, fmt.Errorf("item target resolver requires database")
		}
		var item models.MetaItem
		if err := r.db.Where("tenant_id = ? AND id = ?", tenantID, opts.ItemID).First(&item).Error; err != nil {
			return nil, fmt.Errorf("item target not found: %w", err)
		}
		catalogPaths = append(catalogPaths, scanflow.TargetPathsFromItem(item)...)
	}

	for _, target := range opts.Targets {
		catalogPaths = append(catalogPaths, scanflow.TargetPathsFromLocator(target)...)
	}

	return scanflow.UniqueNonEmpty(catalogPaths), nil
}
