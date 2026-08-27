package service

import (
	"testing"

	commonClient "github.com/addp/common/client"
	"github.com/addp/meta/internal/config"
)

func TestNewRuntimeScanServiceInjectsSharedConfigAndIndexer(t *testing.T) {
	cfg := &config.Config{}

	client := commonClient.NewManagerContentClient("http://manager.test", nil, nil)
	scanService := NewRuntimeScanService(nil, nil, cfg, client)
	if scanService.config != cfg {
		t.Fatal("scan runtime did not receive its config")
	}
	if scanService.indexerService.contentIndex != client {
		t.Fatal("scan runtime must receive the Manager content index client")
	}
}
