package service

import (
	"testing"

	"github.com/addp/meta/internal/models"
)

func TestSetSchemaFieldsWritesPartitionAndFlatCompatibility(t *testing.T) {
	t.Parallel()

	fields := []map[string]interface{}{{"name": "id", "type": "integer"}}
	attrs := models.JSONMap{}

	setSchemaFields(attrs, fields)

	if attrs["fields"] == nil {
		t.Fatalf("flat fields missing: %#v", attrs)
	}
	schema := attrs["schema"].(map[string]interface{})
	if schema["fields"] == nil {
		t.Fatalf("schema.fields missing: %#v", schema)
	}
}
