package service

import (
	"testing"

	"github.com/addp/meta/internal/config"
)

func TestNewRuntimeScanServiceInjectsSharedConfigAndIndexer(t *testing.T) {
	cfg := &config.Config{}

	scanService, indexer, err := NewRuntimeScanService(nil, nil, cfg)
	if err != nil {
		t.Fatalf("NewRuntimeScanService() error = %v", err)
	}
	if scanService.config != cfg {
		t.Fatal("scan runtime did not receive its config")
	}
	if scanService.indexer != indexer || scanService.indexerService.indexer != indexer {
		t.Fatal("scan runtime and indexer service must share one indexer")
	}
}
