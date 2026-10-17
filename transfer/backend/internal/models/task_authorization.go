package models

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

// ScheduleAuthorizationHash excludes runtime summaries and immutable task identity.
func (t TransferTask) ScheduleAuthorizationHash() (string, error) {
	encoded, err := json.Marshal(struct {
		Name             string  `json:"name"`
		Description      string  `json:"description"`
		TaskType         string  `json:"task_type"`
		Config           JSONMap `json:"config"`
		BatchSize        int     `json:"batch_size"`
		Schedule         string  `json:"schedule"`
		Enabled          bool    `json:"enabled"`
		AutoScanMetadata bool    `json:"auto_scan_metadata"`
	}{t.Name, t.Description, t.TaskType, t.Config, t.BatchSize, t.Schedule, t.Enabled, t.AutoScanMetadata})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(encoded)), nil
}
