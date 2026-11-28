package bootstrap

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/minio/minio-go/v7/pkg/s3utils"

	"github.com/addp/common/config"
	"github.com/addp/common/infra/objectstore"
	"github.com/addp/common/infra/objectstore/s3"
)

// OpenObjectArea resolves the area once at startup. Manager remains mapped to its same-name bucket; standard and develop can be rebound.
func OpenObjectArea(ctx context.Context, area string) (objectstore.Area, error) {
	if area != objectstore.Standard && area != objectstore.Manager && area != objectstore.Develop {
		return nil, fmt.Errorf("unsupported platform object area %q", area)
	}
	if provider := config.GetEnv("ADDP_INFRA_OBJECTSTORE_PROVIDER", "minio"); provider != "minio" && provider != "rustfs" {
		return nil, fmt.Errorf("unsupported platform object store provider %q", provider)
	}
	bucket, err := resolveBucket(area, os.Getenv("ADDP_INFRA_OBJECTSTORE_BINDINGS"))
	if err != nil {
		return nil, err
	}
	settings := config.LoadBuiltinMinIOConfig()
	return s3.Bind(ctx, area, bucket, s3.Config{
		Endpoint: settings.Endpoint, AccessKey: settings.AccessKey,
		SecretKey: settings.SecretKey, UseSSL: settings.UseSSL,
	})
}

// OpenContentArea requires the range and directory capabilities used by formats.
func OpenContentArea(ctx context.Context, name string) (objectstore.ContentArea, error) {
	area, err := OpenObjectArea(ctx, name)
	if err != nil {
		return nil, err
	}
	content, ok := area.(objectstore.ContentArea)
	if !ok {
		return nil, fmt.Errorf("object area %q lacks range or directory operations", name)
	}
	return content, nil
}

// resolveBucket validates the whole deployment mapping, including unused areas.
func resolveBucket(area, raw string) (string, error) {
	bucket := area
	seen := map[string]bool{}
	if raw == "" {
		return bucket, nil
	}
	for _, entry := range strings.Split(raw, ",") {
		name, target, ok := strings.Cut(entry, "=")
		if !ok || (name != objectstore.Standard && name != objectstore.Develop) || seen[name] {
			return "", fmt.Errorf("invalid object store binding %q", entry)
		}
		if err := s3utils.CheckValidBucketNameStrict(target); err != nil {
			return "", fmt.Errorf("invalid object store bucket: %w", err)
		}
		seen[name] = true
		if name == area {
			bucket = target
		}
	}
	return bucket, nil
}
