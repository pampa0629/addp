package capture

import (
	"context"
	"strings"
	"testing"

	"github.com/addp/transfer/internal/models"
)

func TestPostgreSQLSourceResourcesRejectsChangedConnectionIdentity(t *testing.T) {
	err := (PostgreSQLSourceResources{}).DropOwnedResources(context.Background(),
		&CapturePlan{SourceConnectionFingerprint: "new"},
		&models.CaptureResource{SourceConnectionFingerprint: "original"},
	)
	if err == nil || !strings.Contains(err.Error(), "source connection identity changed") {
		t.Fatalf("error = %v", err)
	}
}
