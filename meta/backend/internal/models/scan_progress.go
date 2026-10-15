package models

import (
	"encoding/json"
	"strings"
	"time"

	commonModels "github.com/addp/common/models"
)

// ScanProgress is Meta's bounded file-scan observation, not execution configuration.
type ScanProgress struct {
	Phase                 string    `json:"phase" enums:"discovering,identifying,processing,finalizing"`
	Operation             string    `json:"operation,omitempty" enums:"listing,identifying,enriching,hashing,extracting,saving,indexing,cleaning_index"`
	FilesDiscovered       int64     `json:"files_discovered"`
	DirectoriesDiscovered int64     `json:"directories_discovered"`
	FilesTotalKnown       bool      `json:"files_total_known"`
	ItemsPlanned          int64     `json:"items_planned"`
	ItemsTotalKnown       bool      `json:"items_total_known"`
	ItemsCompleted        int64     `json:"items_completed"`
	ItemsSucceeded        int64     `json:"items_succeeded"`
	ItemsFailed           int64     `json:"items_failed"`
	CurrentPath           string    `json:"current_path,omitempty"`
	CurrentBytesRead      int64     `json:"current_bytes_read"`
	CurrentBytesTotal     int64     `json:"current_bytes_total"`
	CurrentRangeBytesRead int64     `json:"current_range_bytes_read"`
	CurrentRangesRead     int64     `json:"current_ranges_read"`
	IndexCleanupTotal     int64     `json:"index_cleanup_total"`
	IndexCleanupCompleted int64     `json:"index_cleanup_completed"`
	IndexCleanupFailed    int64     `json:"index_cleanup_failed"`
	LastProgressAt        time.Time `json:"last_progress_at"`
}

func (p ScanProgress) Percent() int {
	if !p.ItemsTotalKnown || p.ItemsPlanned <= 0 {
		return 0
	}
	return int(min(int64(99), max(int64(0), p.ItemsCompleted*100/p.ItemsPlanned)))
}

// ObserveScanProgress projects only the fixed contract; never expose arbitrary metadata.
func ObserveScanProgress(metadata commonModels.JSONMap) *ScanProgress {
	raw, ok := metadata["scan_progress"]
	if !ok {
		return nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var p ScanProgress
	if json.Unmarshal(data, &p) != nil {
		return nil
	}
	switch p.Phase {
	case "discovering", "identifying", "processing", "finalizing":
	default:
		return nil
	}
	switch p.Operation {
	case "", "listing", "identifying", "enriching", "hashing", "extracting", "saving", "indexing", "cleaning_index":
	default:
		p.Operation = ""
	}
	// Paths are catalog-relative, never URLs, host paths, or credential-bearing locators.
	if strings.HasPrefix(p.CurrentPath, "/") || strings.ContainsAny(p.CurrentPath, "\\\r\n") || strings.Contains(p.CurrentPath, "://") {
		p.CurrentPath = ""
	}
	if len([]rune(p.CurrentPath)) > 512 {
		p.CurrentPath = string([]rune(p.CurrentPath)[:512])
	}
	if p.FilesDiscovered < 0 || p.DirectoriesDiscovered < 0 || p.ItemsPlanned < 0 || p.ItemsCompleted < 0 || p.ItemsSucceeded < 0 || p.ItemsFailed < 0 || p.CurrentBytesRead < 0 || p.CurrentBytesTotal < 0 || p.CurrentRangeBytesRead < 0 || p.CurrentRangesRead < 0 || p.ItemsCompleted != p.ItemsSucceeded+p.ItemsFailed || p.IndexCleanupTotal < 0 || p.IndexCleanupCompleted < 0 || p.IndexCleanupFailed < 0 || p.IndexCleanupCompleted+p.IndexCleanupFailed > p.IndexCleanupTotal {
		return nil
	}
	return &p
}
