package service

import (
	"fmt"
	"sort"

	"github.com/addp/standard/internal/models"
)

// buildDomainHierarchy is shared by the tree, reference resolution and candidates.
// A malformed hierarchy must not silently turn a child into a root.
func buildDomainHierarchy(domains []models.Domain) ([]*DomainTree, map[int64][]string, error) {
	ordered := append([]models.Domain(nil), domains...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].SortOrder == ordered[j].SortOrder {
			return ordered[i].ID < ordered[j].ID
		}
		return ordered[i].SortOrder < ordered[j].SortOrder
	})
	byID := make(map[int64]*DomainTree, len(ordered))
	for _, domain := range ordered {
		if _, exists := byID[domain.ID]; exists {
			return nil, nil, fmt.Errorf("duplicate domain identity %d", domain.ID)
		}
		byID[domain.ID] = &DomainTree{Domain: domain}
	}
	roots := make([]*DomainTree, 0)
	for _, domain := range ordered {
		node := byID[domain.ID]
		if domain.ParentID == nil {
			roots = append(roots, node)
			continue
		}
		parent, ok := byID[*domain.ParentID]
		if !ok {
			return nil, nil, fmt.Errorf("domain %d has an unavailable parent", domain.ID)
		}
		parent.Children = append(parent.Children, node)
	}
	paths := make(map[int64][]string, len(ordered))
	var visit func([]*DomainTree, []string)
	visit = func(nodes []*DomainTree, ancestors []string) {
		for _, node := range nodes {
			path := append(append([]string(nil), ancestors...), node.Name)
			paths[node.ID] = path
			visit(node.Children, path)
		}
	}
	visit(roots, nil)
	if len(paths) != len(ordered) {
		return nil, nil, fmt.Errorf("domain hierarchy contains a cycle or duplicate identity")
	}
	return roots, paths, nil
}
