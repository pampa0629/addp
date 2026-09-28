package service

import (
	"context"
	"errors"
	"strconv"

	commonClient "github.com/addp/common/client"
)

// DomainOverview contains only the Standard facts explicitly discoverable to Catalog readers.
type DomainOverview struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Code        string           `json:"code"`
	Description string           `json:"description"`
	ParentID    *string          `json:"parent_id,omitempty"`
	Children    []DomainOverview `json:"children,omitempty"`
}

type DomainOverviewReader interface {
	ListDomainOverviews(context.Context, int64) ([]DomainOverview, error)
}

type standardDomainOverviewReader struct{ client *commonClient.StandardClient }

func NewStandardDomainOverviewReader(client *commonClient.StandardClient) DomainOverviewReader {
	return &standardDomainOverviewReader{client: client}
}

func (r *standardDomainOverviewReader) ListDomainOverviews(ctx context.Context, tenantID int64) ([]DomainOverview, error) {
	if r == nil || r.client == nil || tenantID <= 0 {
		return nil, errors.New("Standard domain overview reader is unavailable")
	}
	ownerTree, err := r.client.WithTenantID(uint(tenantID)).ListDomains(ctx)
	if err != nil {
		return nil, err
	}
	return projectDomainOverviews(ownerTree), nil
}

func projectDomainOverviews(ownerTree []*commonClient.StandardDomainTree) []DomainOverview {
	result := make([]DomainOverview, 0, len(ownerTree))
	for _, node := range ownerTree {
		if node == nil {
			continue
		}
		item := DomainOverview{
			ID: strconv.FormatInt(node.ID, 10), Name: node.Name, Code: node.Code,
			Description: node.Description, Children: projectDomainOverviews(node.Children),
		}
		if node.ParentID != nil {
			parentID := strconv.FormatInt(*node.ParentID, 10)
			item.ParentID = &parentID
		}
		result = append(result, item)
	}
	return result
}
