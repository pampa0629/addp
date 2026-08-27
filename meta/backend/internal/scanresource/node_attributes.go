package scanresource

import "github.com/addp/meta/internal/models"

func ObjectBucketNodeAttributes(bucket string) models.JSONMap {
	return models.JSONMap{
		"schema_version": 1,
		"storage": map[string]interface{}{
			"bucket": bucket,
		},
	}
}

func ObjectPrefixNodeAttributes(bucket, prefix string) models.JSONMap {
	return models.JSONMap{
		"schema_version": 1,
		"storage": map[string]interface{}{
			"bucket": bucket,
			"path":   prefix,
		},
	}
}

func FileDirectoryNodeAttributes(path string) models.JSONMap {
	return models.JSONMap{
		"schema_version": 1,
		"storage": map[string]interface{}{
			"path": path,
		},
	}
}
