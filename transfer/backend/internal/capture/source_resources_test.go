package capture

import (
	"context"
	"strings"
	"testing"

	"github.com/addp/transfer/internal/models"
)

func TestDatabaseSourceResourcesRejectsChangedConnectionIdentity(t *testing.T) {
	err := (DatabaseSourceResources{}).DropOwnedResources(context.Background(),
		&CapturePlan{SourceType: models.CaptureSourcePostgreSQL, SourceConnectionFingerprint: "new"},
		&models.CaptureResource{
			SourceType: models.CaptureSourcePostgreSQL, SourceConnectionFingerprint: "original",
			PostgreSQL: &models.PostgreSQLCaptureResource{},
		},
	)
	if err == nil || !strings.Contains(err.Error(), "source connection identity changed") {
		t.Fatalf("error = %v", err)
	}
}

func TestDatabaseSourceResourcesAcceptsMySQLWithoutSourceOwnedObjects(t *testing.T) {
	err := (DatabaseSourceResources{}).DropOwnedResources(context.Background(),
		&CapturePlan{SourceType: models.CaptureSourceMySQL, SourceConnectionFingerprint: "same"},
		&models.CaptureResource{
			SourceType: models.CaptureSourceMySQL, SourceConnectionFingerprint: "same",
			MySQL: &models.MySQLCaptureResource{ConnectorServerID: 1, SchemaHistoryTopicName: "history", SchemaHistoryTopicOwned: true},
		})
	if err != nil {
		t.Fatalf("MySQL cleanup error = %v", err)
	}
}

func TestDatabaseSourceResourcesAcceptsOracleWithoutGenerationOwnedDatabaseObjects(t *testing.T) {
	err := (DatabaseSourceResources{}).DropOwnedResources(context.Background(),
		&CapturePlan{SourceType: models.CaptureSourceOracle, SourceConnectionFingerprint: "same"},
		&models.CaptureResource{
			SourceType: models.CaptureSourceOracle, SourceConnectionFingerprint: "same",
			Oracle: &models.OracleCaptureResource{SchemaHistoryTopicName: "history", SchemaHistoryTopicOwned: true},
		})
	if err != nil {
		t.Fatalf("Oracle cleanup error = %v", err)
	}
}
