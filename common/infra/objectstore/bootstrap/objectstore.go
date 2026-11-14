package bootstrap

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/addp/common/config"
	"github.com/addp/common/infra/objectstore"
	"github.com/addp/common/infra/objectstore/minio"
)

// OpenObjectArea resolves the area once at startup. Manager remains mapped to its same-name bucket; only standard can be rebound.
func OpenObjectArea(ctx context.Context, area string) (objectstore.Area, error) {
	if area != objectstore.Standard && area != objectstore.Manager {
		return nil, fmt.Errorf("unsupported platform object area %q", area)
	}
	if provider := config.GetEnv("ADDP_INFRA_OBJECTSTORE_PROVIDER", "minio"); provider != "minio" {
		return nil, fmt.Errorf("unsupported platform object store provider %q", provider)
	}
	bucket := area
	if mapping := os.Getenv("ADDP_INFRA_OBJECTSTORE_BINDINGS"); mapping != "" {
		name, target, ok := strings.Cut(mapping, "=")
		if !ok || name != objectstore.Standard {
			return nil, fmt.Errorf("unsupported object store binding %q (expected standard=bucket)", mapping)
		}
		if area == objectstore.Standard {
			bucket = target
		}
	}
	return minio.Bind(ctx, area, bucket, config.LoadBuiltinMinIOConfig())
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
