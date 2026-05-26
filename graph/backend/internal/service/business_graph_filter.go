package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/addp/common/dbbridge"
	commonmodels "github.com/addp/common/models"
)

const internalRelationshipTypeList = "['RTREE_METADATA', 'RTREE_REFERENCE', 'RTREE_ROOT']"
const internalRelationshipTypePredicate = "type(r) IN " + internalRelationshipTypeList

func isInternalRelationshipType(relType string) bool {
	switch strings.ToUpper(strings.TrimSpace(relType)) {
	case "RTREE_METADATA", "RTREE_REFERENCE", "RTREE_ROOT":
		return true
	default:
		return false
	}
}

func businessRelationshipProjection(ctx context.Context, engine *commonmodels.Engine) (string, error) {
	result, err := dbbridge.ExecuteGraphQuery(ctx, engine,
		"CALL db.relationshipTypes() YIELD relationshipType RETURN relationshipType ORDER BY relationshipType")
	if err != nil {
		return "", fmt.Errorf("failed to list relationship types for GDS projection: %w", err)
	}
	quoted := make([]string, 0, len(result.Rows))
	for _, row := range result.Rows {
		relType := fmt.Sprintf("%v", row["relationshipType"])
		if relType == "" || relType == "<nil>" || isInternalRelationshipType(relType) {
			continue
		}
		quoted = append(quoted, fmt.Sprintf("'%s'", escapeCypher(relType)))
	}
	if len(quoted) == 0 {
		return "", fmt.Errorf("no business relationship types available")
	}
	return "[" + strings.Join(quoted, ",") + "]", nil
}
