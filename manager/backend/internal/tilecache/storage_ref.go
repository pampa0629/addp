package tilecache

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	StorageRefTypeObject   = "object"
	StorageRefProviderADDP = "addp_object_storage"
)

type StorageRefPayload struct {
	Type     string `json:"type"`
	Provider string `json:"provider"`
	Bucket   string `json:"bucket"`
	Object   string `json:"object"`
}

func ObjectStorageRef(tenantID uint, fingerprint, profileHash string) string {
	payload := StorageRefPayload{
		Type:     StorageRefTypeObject,
		Provider: StorageRefProviderADDP,
		Bucket:   "manager",
		Object: fmt.Sprintf(
			"tenant_%d/vector-tile-cache/%s/%s.pmtiles",
			tenantID,
			strings.TrimSpace(fingerprint),
			strings.TrimSpace(profileHash),
		),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(data)
}

func ObjectLocation(storageRef, defaultBucket string) (string, string, error) {
	var payload StorageRefPayload
	if err := json.Unmarshal([]byte(strings.TrimSpace(storageRef)), &payload); err != nil {
		return "", "", fmt.Errorf("invalid tile cache storage_ref: %w", err)
	}
	if payload.Type != StorageRefTypeObject {
		return "", "", fmt.Errorf("unsupported tile cache storage_ref type %q", payload.Type)
	}
	if payload.Provider != "" && payload.Provider != StorageRefProviderADDP {
		return "", "", fmt.Errorf("unsupported tile cache storage_ref provider %q", payload.Provider)
	}
	objectName := strings.Trim(strings.TrimSpace(payload.Object), "/")
	if objectName == "" || !strings.HasSuffix(strings.ToLower(objectName), ".pmtiles") {
		return "", "", fmt.Errorf("tile cache storage_ref must point to a .pmtiles object")
	}
	bucket := strings.TrimSpace(payload.Bucket)
	if bucket == "" {
		bucket = strings.TrimSpace(defaultBucket)
	}
	if bucket == "" {
		return "", "", fmt.Errorf("tile cache storage_ref bucket is required")
	}
	return bucket, objectName, nil
}
