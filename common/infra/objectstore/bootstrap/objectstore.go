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

// OpenObjectArea resolves the area once at startup. The first batch owns only standard.
func OpenObjectArea(ctx context.Context, area string) (objectstore.Area, error) {
	if area != objectstore.Standard {
		return nil, fmt.Errorf("unsupported platform object area %q", area)
	}
	if provider := config.GetEnv("ADDP_INFRA_OBJECTSTORE_PROVIDER", "minio"); provider != "minio" {
		return nil, fmt.Errorf("unsupported platform object store provider %q", provider)
	}
	bucket := area
	if mapping := os.Getenv("ADDP_INFRA_OBJECTSTORE_BINDINGS"); mapping != "" {
		name, target, ok := strings.Cut(mapping, "=")
		if !ok || name != area {
			return nil, fmt.Errorf("unsupported object store binding %q (expected standard=bucket)", mapping)
		}
		bucket = target
	}
	return minio.Bind(ctx, area, bucket, config.LoadBuiltinMinIOConfig())
}
