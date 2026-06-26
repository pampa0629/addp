package cog

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

func ObjectStorageRef(bucket, object string) string {
	payload := StorageRefPayload{
		Type:     StorageRefTypeObject,
		Provider: StorageRefProviderADDP,
		Bucket:   strings.TrimSpace(bucket),
		Object:   strings.Trim(strings.TrimSpace(object), "/"),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(data)
}

func ObjectLocation(storageRef string, defaultBucket string) (string, string, error) {
	var payload StorageRefPayload
	if err := json.Unmarshal([]byte(strings.TrimSpace(storageRef)), &payload); err != nil {
		return "", "", fmt.Errorf("invalid raster COG storage_ref: %w", err)
	}
	if payload.Type != StorageRefTypeObject {
		return "", "", fmt.Errorf("unsupported raster COG storage_ref type %q", payload.Type)
	}
	if payload.Provider != "" && payload.Provider != StorageRefProviderADDP {
		return "", "", fmt.Errorf("unsupported raster COG storage_ref provider %q", payload.Provider)
	}
	object := strings.Trim(strings.TrimSpace(payload.Object), "/")
	if object == "" {
		return "", "", fmt.Errorf("raster COG storage_ref object is required")
	}
	bucket := strings.TrimSpace(payload.Bucket)
	if bucket == "" {
		bucket = defaultBucket
	}
	return bucket, object, nil
}
