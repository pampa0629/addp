package migration

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
	commonmodels "github.com/addp/common/models"
)

func TestDevelopControlledFileInputsForwardMigrationAgainstPostgres(t *testing.T) {
	testAuthorizationFactsMigration(t, "000290_develop_controlled_file_inputs.up.sql", 290, 0, func(db *sql.DB) func() {
		return func() {
			object, err := plugin.ObjectItemPathFromRefPath(11, "bucket/input.geojson")
			if err != nil {
				t.Fatal(err)
			}
			for _, scenario := range []struct {
				format string
				target plugin.EngineCatalogPath
				valid  bool
			}{
				{"geojson", object, true}, {"geopackage", plugin.FileItemPath(11, "input.gpkg"), true},
				{"geopackage", object, false}, {"geojson", plugin.FileItemPath(11, "input.geojson"), false},
				{"", object, false}, {"tiff", object, false},
			} {
				scope := map[string]any{"schema_version": "develop.file-bound-scope/v1", "intent_digest": strings.Repeat("a", 64), "expected_attempt": 1, "timeout_seconds": 45, "sources": []any{map[string]any{"target": scenario.target, "format": scenario.format, "item_fingerprint": commonmodels.GenerateItemFingerprint(11, scenario.target.StringPath()), "boundary_hash": strings.Repeat("b", 64), "engine_version": "1"}}}
				raw, err := json.Marshal(scope)
				if err != nil {
					t.Fatal(err)
				}
				var valid bool
				if err := db.QueryRow("SELECT system.valid_develop_file_scope($1::jsonb)", string(raw)).Scan(&valid); err != nil || valid != scenario.valid {
					t.Fatal("controlled scope constraint", scenario.format, valid, err)
				}
			}
		}
	})
}
