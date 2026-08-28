package service

import (
	"context"
	"errors"
	"time"

	commonclient "github.com/addp/common/client"
)

func resolveElementRevisionSnapshot(client *commonclient.StandardClient, tenantID int64, elementIDs []int64, asOf time.Time) (map[int64]int64, error) {
	unique := make([]int64, 0, len(elementIDs))
	seen := make(map[int64]struct{}, len(elementIDs))
	for _, elementID := range elementIDs {
		if elementID <= 0 {
			continue
		}
		if _, exists := seen[elementID]; exists {
			continue
		}
		seen[elementID] = struct{}{}
		unique = append(unique, elementID)
	}
	if len(unique) == 0 {
		return map[int64]int64{}, nil
	}
	if client == nil {
		return nil, standardReferenceError(errors.New("standard client is required to freeze data element revisions"), "element_revision_not_found")
	}
	resolved, err := client.WithTenantID(uint(tenantID)).ResolveElementRevisions(context.Background(), unique, asOf)
	if err != nil {
		return nil, standardReferenceError(err, "element_revision_not_found")
	}
	bindings := make(map[int64]int64, len(resolved))
	for elementID, binding := range resolved {
		bindings[elementID] = binding.RevisionID
	}
	return bindings, nil
}
