package connector

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/addp/transfer/pkg/pipeline"
)

// TestShapefileWriterCreatesDBF verifies that DBF files are created with correct naming
func TestShapefileWriterCreatesDBF(t *testing.T) {
	tmpDir := t.TempDir()
	shpPath := filepath.Join(tmpDir, "test.shp")

	// Create writer config
	config := pipeline.ConnectorConfig{
		Type:      "shapefile",
		BatchSize: 10,
		Config: map[string]interface{}{
			"file_path":      shpPath,
			"geometry_field": "geom",
			"shape_type":     "POINT",
		},
	}

	// Create writer
	writer, err := NewShapefileWriter(config)
	if err != nil {
		t.Fatalf("Failed to create writer: %v", err)
	}

	ctx := context.Background()
	if err := writer.Open(ctx, config); err != nil {
		t.Fatalf("Failed to open writer: %v", err)
	}

	// Write test data
	batch := &pipeline.DataBatch{
		Rows: []map[string]interface{}{
			{
				"id":   1,
				"name": "Test Point",
				"geom": "POINT(120.0 30.0)",
			},
		},
	}

	if err := writer.Write(ctx, batch); err != nil {
		t.Fatalf("Failed to write data: %v", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("Failed to close writer: %v", err)
	}

	// Verify all three files exist
	basePath := shpPath[:len(shpPath)-4]

	shpFile := basePath + ".shp"
	shxFile := basePath + ".shx"
	dbfFile := basePath + ".dbf"
	wrongDbfFile := basePath + "dbf" // The buggy filename

	// Check that correct files exist
	if _, err := os.Stat(shpFile); os.IsNotExist(err) {
		t.Errorf("SHP file not created: %s", shpFile)
	}
	if _, err := os.Stat(shxFile); os.IsNotExist(err) {
		t.Errorf("SHX file not created: %s", shxFile)
	}
	if _, err := os.Stat(dbfFile); os.IsNotExist(err) {
		t.Errorf("DBF file not created with correct name: %s", dbfFile)
	}

	// Check that wrong filename doesn't exist
	if _, err := os.Stat(wrongDbfFile); !os.IsNotExist(err) {
		t.Errorf("Buggy DBF file still exists (should have been renamed): %s", wrongDbfFile)
	}

	t.Logf("All shapefile components created successfully:")
	t.Logf("  - %s", shpFile)
	t.Logf("  - %s", shxFile)
	t.Logf("  - %s", dbfFile)
}
