package service

import (
	"testing"
	"time"

	"github.com/addp/common/events"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
)

func TestCleanupExpectedForModuleProtocolHelper(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		expected []string
		module   string
		want     bool
	}{
		{name: "empty means all modules", expected: nil, module: events.ModuleManager, want: true},
		{name: "includes module", expected: []string{events.ModuleMeta, events.ModuleManager}, module: events.ModuleManager, want: true},
		{name: "does not include module", expected: []string{events.ModuleMeta}, module: events.ModuleManager, want: false},
		{name: "trims stream values", expected: []string{" manager "}, module: events.ModuleManager, want: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := events.CleanupExpectedForModule(tt.expected, tt.module); got != tt.want {
				t.Fatalf("CleanupExpectedForModule() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestManagerCleanupSummaries(t *testing.T) {
	t.Parallel()

	stats := &ManagerCleanupStats{
		PreviewStates:            2,
		TileCaches:               3,
		Embeddings:               5,
		VectorMaterializedViews:  7,
		DeletedPhysicalArtifacts: 4,
		MarkedMissingSource:      6,
		SkippedExternalTargets:   1,
		DisabledTaskDefinitions:  8,
		Errors:                   []string{"one", "two"},
	}

	scanSummary := managerScanSummary(stats)
	if scanSummary.ScannedItems != 25 || scanSummary.DisabledTaskDefinitions != 8 || scanSummary.SkippedItems != 1 || scanSummary.ErrorCount != 2 {
		t.Fatalf("scan summary = %#v", scanSummary)
	}
	if scanSummary.RiskLevel != "low" {
		t.Fatalf("scan risk = %q, want low", scanSummary.RiskLevel)
	}

	executeSummary := managerExecuteSummary(stats)
	if executeSummary.AffectedRecords != 25 {
		t.Fatalf("affected_records = %d, want 25", executeSummary.AffectedRecords)
	}
	if executeSummary.DeletedPhysicalArtifacts != 4 || executeSummary.MarkedMissingSource != 6 || executeSummary.DisabledTaskDefinitions != 8 {
		t.Fatalf("execute summary = %#v", executeSummary)
	}
	if executeSummary.SkippedItems != 1 || executeSummary.ErrorCount != 2 {
		t.Fatalf("execute summary skip/error = %#v", executeSummary)
	}
}

func TestManagerCleanupContextMatching(t *testing.T) {
	t.Parallel()

	service := &CleanupService{}
	locator := "addp://engine/12/path/public/roads?type=table&item_id=99"

	tests := []struct {
		name            string
		locator         string
		itemFingerprint string
		itemID          uint
		context         map[string]interface{}
		want            bool
	}{
		{
			name:            "empty context matches",
			locator:         locator,
			itemFingerprint: "fp-1",
			itemID:          99,
			context:         nil,
			want:            true,
		},
		{
			name:            "engine item and fingerprint match",
			locator:         locator,
			itemFingerprint: "fp-1",
			itemID:          99,
			context:         map[string]interface{}{"engine_id": "12", "item_id": float64(99), "item_fingerprint": "fp-1"},
			want:            true,
		},
		{
			name:            "engine mismatch",
			locator:         locator,
			itemFingerprint: "fp-1",
			itemID:          99,
			context:         map[string]interface{}{"engine_id": uint(13)},
			want:            false,
		},
		{
			name:            "item id mismatch",
			locator:         locator,
			itemFingerprint: "fp-1",
			itemID:          99,
			context:         map[string]interface{}{"item_id": 100},
			want:            false,
		},
		{
			name:            "fingerprint mismatch",
			locator:         locator,
			itemFingerprint: "fp-1",
			itemID:          99,
			context:         map[string]interface{}{"item_fingerprint": "fp-2"},
			want:            false,
		},
		{
			name:            "invalid locator fails engine-scoped context",
			locator:         "invalid",
			itemFingerprint: "fp-1",
			itemID:          99,
			context:         map[string]interface{}{"engine_id": 12},
			want:            false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := service.matchesCleanupContext(tt.locator, tt.itemFingerprint, tt.itemID, tt.context)
			if got != tt.want {
				t.Fatalf("matchesCleanupContext() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestManagerCleanupRiskLevelForCount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		count int
		want  string
	}{
		{count: 0, want: "low"},
		{count: 100, want: "low"},
		{count: 101, want: "medium"},
		{count: 1000, want: "medium"},
		{count: 1001, want: "high"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()

			if got := riskLevelForCount(tt.count); got != tt.want {
				t.Fatalf("riskLevelForCount(%d) = %q, want %q", tt.count, got, tt.want)
			}
		})
	}
}

func TestCleanupTaskTargetFromConfig(t *testing.T) {
	t.Parallel()

	embeddingTarget := cleanupTaskTargetFromConfig(commonModels.JSONMap{
		"target": commonModels.JSONMap{
			"engine_id":        float64(12),
			"item_id":          float64(99),
			"item_fingerprint": "fp-1",
			"locator":          "addp://engine/12/path/public/roads?type=table&item_id=99",
		},
	})
	if embeddingTarget.EngineID != 12 || embeddingTarget.ItemID != 99 || embeddingTarget.ItemFingerprint != "fp-1" {
		t.Fatalf("embedding target = %#v", embeddingTarget)
	}

	artifactTarget := cleanupTaskTargetFromConfig(commonModels.JSONMap{
		"target": commonModels.JSONMap{
			"source_engine_id": uint(13),
			"item_fingerprint": "fp-2",
		},
	})
	if artifactTarget.EngineID != 13 || artifactTarget.ItemFingerprint != "fp-2" {
		t.Fatalf("artifact target = %#v", artifactTarget)
	}
}

func TestCleanupTaskDefinitionReason(t *testing.T) {
	t.Parallel()

	if got := cleanupTaskDefinitionReason(map[string]interface{}{"engine_id": 12}); got != "missing_engine" {
		t.Fatalf("reason = %q, want missing_engine", got)
	}
	if got := cleanupTaskDefinitionReason(map[string]interface{}{"item_id": 99}); got != "missing_source" {
		t.Fatalf("reason = %q, want missing_source", got)
	}
}

func TestCleanupTaskTargetSourceExistsTreatsDeletedEngineAsMissing(t *testing.T) {
	t.Parallel()

	service := &CleanupService{}
	target := cleanupTaskTarget{EngineID: 12, ItemID: 99, Locator: "addp://engine/12/path/public/roads?type=table&item_id=99"}
	if got := service.taskTargetSourceExists(nil, 1, target, map[string]interface{}{"engine_id": 12}); got {
		t.Fatal("taskTargetSourceExists() = true, want false for engine deleted context")
	}
	if got := service.taskTargetSourceExists(nil, 1, cleanupTaskTarget{}, nil); !got {
		t.Fatal("empty task target should be treated as existing to avoid broad cleanup")
	}
}

func TestNormalizeExportCleanupOptionsUsesInternalDefaults(t *testing.T) {
	t.Parallel()

	got := normalizeExportCleanupOptions(ExportCleanupOptions{})
	if got.SuccessRetention != 24*time.Hour {
		t.Fatalf("success retention = %v, want 24h", got.SuccessRetention)
	}
	if got.FailedRetention != 6*time.Hour {
		t.Fatalf("failed retention = %v, want 6h", got.FailedRetention)
	}
	if got.MaxRunningAge != 6*time.Hour {
		t.Fatalf("max running age = %v, want 6h", got.MaxRunningAge)
	}
	if got.Interval != 30*time.Minute {
		t.Fatalf("interval = %v, want 30m", got.Interval)
	}
}

func TestExportSessionCleanupPrefixUsesParentLocator(t *testing.T) {
	t.Parallel()

	service := &CleanupService{minioBucket: "manager"}
	session := &models.ExportSession{
		TargetParentLocator: "addp-infra://minio/manager/tenant_7/export/20260620/session-1?type=prefix",
		TargetLocator:       "addp-infra://minio/manager/tenant_7/export/20260620/session-1/roads.csv?type=object",
	}
	got, err := service.exportSessionCleanupPrefix(session)
	if err != nil {
		t.Fatalf("exportSessionCleanupPrefix() error = %v", err)
	}
	if got != "tenant_7/export/20260620/session-1/" {
		t.Fatalf("prefix = %q", got)
	}
}

func TestExportSessionCleanupPrefixRejectsDifferentBucket(t *testing.T) {
	t.Parallel()

	service := &CleanupService{minioBucket: "manager"}
	session := &models.ExportSession{
		TargetParentLocator: "addp-infra://minio/other/tenant_7/export/20260620/session-1?type=prefix",
	}
	if _, err := service.exportSessionCleanupPrefix(session); err == nil {
		t.Fatal("exportSessionCleanupPrefix() accepted different bucket")
	}
}
