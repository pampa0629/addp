package tilecache

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	StorageRefTypeObjectPrefix = "object_prefix"
	StorageRefProviderADDP     = "addp_object_storage"
)

type StorageRefPayload struct {
	Type         string `json:"type"`
	Provider     string `json:"provider"`
	Bucket       string `json:"bucket"`
	ObjectPrefix string `json:"object_prefix"`
	Manifest     string `json:"manifest"`
}

func ObjectPrefixStorageRef(tenantID uint, fingerprint string) string {
	cacheKey := strings.TrimSpace(fingerprint)
	payload := StorageRefPayload{
		Type:         StorageRefTypeObjectPrefix,
		Provider:     StorageRefProviderADDP,
		Bucket:       "manager",
		ObjectPrefix: fmt.Sprintf("tenant_%d/mvt-tiles/%s/", tenantID, cacheKey),
		Manifest:     fmt.Sprintf("tenant_%d/mvt-tiles/%s/metadata.json", tenantID, cacheKey),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(data)
}

func TileObjectLocation(storageRef string, defaultBucket string, z, x, y int) (string, string, error) {
	bucket, prefix, _, err := ParseObjectPrefix(storageRef, defaultBucket)
	if err != nil {
		return "", "", err
	}
	return bucket, fmt.Sprintf("%s/tiles/z%d/%d_%d.mvt.gz", prefix, z, x, y), nil
}

func ManifestObjectLocation(storageRef string, defaultBucket string) (string, string, error) {
	bucket, prefix, manifest, err := ParseObjectPrefix(storageRef, defaultBucket)
	if err != nil {
		return "", "", err
	}
	if manifest == "" {
		manifest = fmt.Sprintf("%s/metadata.json", prefix)
	}
	return bucket, manifest, nil
}

func ObjectPrefix(storageRef string, defaultBucket string) (string, string, error) {
	bucket, prefix, _, err := ParseObjectPrefix(storageRef, defaultBucket)
	return bucket, prefix, err
}

func ParseObjectPrefix(storageRef string, defaultBucket string) (string, string, string, error) {
	var payload StorageRefPayload
	if err := json.Unmarshal([]byte(strings.TrimSpace(storageRef)), &payload); err != nil {
		return "", "", "", fmt.Errorf("invalid tile cache storage_ref: %w", err)
	}
	if payload.Type != StorageRefTypeObjectPrefix {
		return "", "", "", fmt.Errorf("unsupported tile cache storage_ref type %q", payload.Type)
	}
	if payload.Provider != "" && payload.Provider != StorageRefProviderADDP {
		return "", "", "", fmt.Errorf("unsupported tile cache storage_ref provider %q", payload.Provider)
	}
	prefix := strings.Trim(strings.TrimSpace(payload.ObjectPrefix), "/")
	if prefix == "" {
		return "", "", "", fmt.Errorf("tile cache storage_ref object_prefix is required")
	}
	bucket := strings.TrimSpace(payload.Bucket)
	if bucket == "" {
		bucket = defaultBucket
	}
	manifest := strings.Trim(strings.TrimSpace(payload.Manifest), "/")
	return bucket, prefix, manifest, nil
}
