package scanflow

import (
	"strings"

	"github.com/addp/meta/internal/models"
)

type Mode string

const (
	ModeEngine       Mode = "engine"
	ModeNode         Mode = "node"
	ModeItem         Mode = "item"
	ModeTargets      Mode = "targets"
	ModeCatalogPaths Mode = "catalog_paths"
	ModeRefGroups    Mode = "ref_groups"
)

type ProgressReporter interface {
	SetTotal(total int)
	Advance(label string, completed, total int, meta map[string]interface{})
	Message(message string)
}

type Options struct {
	EngineID     uint
	TenantID     uint
	CatalogPaths []string
	RefGroups    []models.ScanRefGroup
	Token        string
	ScanDepth    string
	Force        bool
	Source       string
	Reporter     ProgressReporter
	NodeID       uint
	ItemID       uint
	Targets      []string
}

type Scope struct {
	EngineID     uint
	Mode         Mode
	CatalogPaths []string
	RefGroups    []models.ScanRefGroup
	Source       string
	ScanDepth    string
	Force        bool
}

func ModeFor(opts Options, catalogPaths []string) Mode {
	if opts.ItemID > 0 {
		return ModeItem
	}
	if len(opts.RefGroups) > 0 {
		return ModeRefGroups
	}
	if opts.NodeID > 0 {
		return ModeNode
	}
	if len(opts.Targets) > 0 {
		return ModeTargets
	}
	if len(catalogPaths) > 0 {
		return ModeCatalogPaths
	}
	return ModeEngine
}

func NormalizeRefGroups(groups []models.ScanRefGroup) []models.ScanRefGroup {
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
				Primary:  ref.Primary,
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

func NormalizeSource(source string) string {
	normalized := strings.TrimSpace(source)
	if normalized == "" {
		return "meta"
	}
	return normalized
}
