package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/addp/service/internal/models"
)

// QueryShapeFingerprint hashes the structured query shape without literal
// values or cursor contents. It is safe to store in execution audit details.
func QueryShapeFingerprint(request *models.QueryExecutionRequest) (string, error) {
	if request == nil {
		return "", nil
	}
	shape := queryShape{
		Parameters: queryParameterNames(request.Parameters),
		Select:     append([]string(nil), request.Select...),
		Filter:     queryFilterShapeOf(request.Filter),
		OrderBy:    append([]models.QueryOrder(nil), request.OrderBy...),
		Limit:      request.Page.Limit,
		HasCursor:  strings.TrimSpace(request.Page.Cursor) != "",
		Format:     strings.ToLower(strings.TrimSpace(request.Format)),
	}
	encoded, err := json.Marshal(shape)
	if err != nil {
		return "", fmt.Errorf("encode query shape: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

type queryShape struct {
	Parameters []string            `json:"parameters"`
	Select     []string            `json:"select"`
	Filter     *queryFilterShape   `json:"filter,omitempty"`
	OrderBy    []models.QueryOrder `json:"order_by"`
	Limit      int                 `json:"limit"`
	HasCursor  bool                `json:"has_cursor"`
	Format     string              `json:"format"`
}

func queryParameterNames(parameters map[string]interface{}) []string {
	names := make([]string, 0, len(parameters))
	for name := range parameters {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

type queryFilterShape struct {
	Field      string             `json:"field,omitempty"`
	Op         string             `json:"op,omitempty"`
	ValueCount int                `json:"value_count,omitempty"`
	And        []queryFilterShape `json:"and,omitempty"`
	Or         []queryFilterShape `json:"or,omitempty"`
	Not        *queryFilterShape  `json:"not,omitempty"`
}

func queryFilterShapeOf(filter *models.QueryFilter) *queryFilterShape {
	if filter == nil {
		return nil
	}
	shape := &queryFilterShape{Field: filter.Field, Op: strings.ToLower(strings.TrimSpace(filter.Op))}
	if values, ok := interfaceSlice(filter.Value); ok {
		shape.ValueCount = len(values)
	} else if filter.Value != nil {
		shape.ValueCount = 1
	}
	shape.And = queryFilterShapes(filter.And)
	shape.Or = queryFilterShapes(filter.Or)
	shape.Not = queryFilterShapeOf(filter.Not)
	return shape
}

func queryFilterShapes(filters []models.QueryFilter) []queryFilterShape {
	if len(filters) == 0 {
		return nil
	}
	result := make([]queryFilterShape, len(filters))
	for index := range filters {
		result[index] = *queryFilterShapeOf(&filters[index])
	}
	return result
}
