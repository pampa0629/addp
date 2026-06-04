package service

import (
	"strings"

	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scantask"
)

type ScanScopeMode string

const (
	ScanScopeModeEngine       ScanScopeMode = "engine"
	ScanScopeModeNode         ScanScopeMode = "node"
	ScanScopeModeItem         ScanScopeMode = "item"
	ScanScopeModeTargets      ScanScopeMode = "targets"
	ScanScopeModeCatalogPaths ScanScopeMode = "catalog_paths"
	ScanScopeModeRefGroups    ScanScopeMode = "ref_groups"
)

type ScanScope struct {
	EngineID     uint
	Mode         ScanScopeMode
	CatalogPaths []string
	RefGroups    []models.ScanRefGroup
	Source       string
	ScanDepth    string
	Force        bool
}

func (s *ScanService) ResolveScanScope(tenantID uint, opts ScanOptions) (ScanScope, error) {
	engineID, err := s.resolveScanEngineID(tenantID, opts)
	if err != nil {
		return ScanScope{}, err
	}

	resolvedCatalogPaths, err := s.resolveScanTargets(tenantID, opts)
	if err != nil {
		return ScanScope{}, err
	}
	catalogPaths := uniqueNonEmpty(append(opts.CatalogPaths, resolvedCatalogPaths...))

	scanDepth, err := scantask.NormalizeScanDepth(opts.ScanDepth, scantask.ScanDepthBasic)
	if err != nil {
		return ScanScope{}, err
	}

	return ScanScope{
		EngineID:     engineID,
		Mode:         scanScopeMode(opts, catalogPaths),
		CatalogPaths: catalogPaths,
		RefGroups:    normalizeScanRefGroups(opts.RefGroups),
		Source:       normalizeScanSource(opts.Source),
		ScanDepth:    scanDepth,
		Force:        opts.Force,
	}, nil
}

func scanScopeMode(opts ScanOptions, catalogPaths []string) ScanScopeMode {
	if len(opts.RefGroups) > 0 {
		return ScanScopeModeRefGroups
	}
	if opts.ItemID > 0 {
		return ScanScopeModeItem
	}
	if opts.NodeID > 0 {
		return ScanScopeModeNode
	}
	if len(opts.Targets) > 0 {
		return ScanScopeModeTargets
	}
	if len(catalogPaths) > 0 {
		return ScanScopeModeCatalogPaths
	}
	return ScanScopeModeEngine
}

func normalizeScanRefGroups(groups []models.ScanRefGroup) []models.ScanRefGroup {
	if len(groups) == 0 {
		return nil
	}
	normalized := make([]models.ScanRefGroup, 0, len(groups))
	for _, group := range groups {
		primary := strings.TrimSpace(group.Primary)
		refs := make([]models.ScanRef, 0, len(group.Refs))
		for _, ref := range group.Refs {
			path := strings.TrimSpace(ref.Path)
			if path == "" {
				continue
			}
			refs = append(refs, models.ScanRef{
				Path:     path,
				Role:     strings.TrimSpace(ref.Role),
				Required: ref.Required,
			})
		}
		if primary == "" && len(refs) == 0 {
			continue
		}
		normalized = append(normalized, models.ScanRefGroup{
			Primary: primary,
			Refs:    refs,
		})
	}
	return normalized
}
