package config

import (
	"testing"
	"time"
)

func TestLoadConfigReadsQualityCheckTimeout(t *testing.T) {
	t.Setenv("QUALITY_CHECK_TIMEOUT", "45s")
	t.Setenv("QUALITY_WORKER_CONCURRENCY", "7")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.CheckTimeout != 45*time.Second {
		t.Fatalf("CheckTimeout = %v, want 45s", cfg.CheckTimeout)
	}
	if cfg.WorkerConcurrency != 7 {
		t.Fatalf("WorkerConcurrency = %d, want 7", cfg.WorkerConcurrency)
	}
}

func TestLoadConfigRejectsNonPositiveQualityCheckTimeout(t *testing.T) {
	t.Setenv("QUALITY_CHECK_TIMEOUT", "0s")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("LoadConfig unexpectedly accepted zero QUALITY_CHECK_TIMEOUT")
	}
}

func TestLoadConfigRejectsNonPositiveWorkerConcurrency(t *testing.T) {
	t.Setenv("QUALITY_WORKER_CONCURRENCY", "0")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("LoadConfig unexpectedly accepted zero QUALITY_WORKER_CONCURRENCY")
	}
}
