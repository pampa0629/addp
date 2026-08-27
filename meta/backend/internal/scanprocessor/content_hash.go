package scanprocessor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/models"
)

const contentHashAlgorithmSHA256 = "sha256"

func computeContentSHA256(ctx context.Context, readableProvider plugin.ContentReadableProvider, connInfo plugin.ConnectionInfo, catalogPath plugin.EngineCatalogPath) (string, error) {
	if readableProvider == nil || len(catalogPath.Segments) == 0 {
		return "", nil
	}
	rc, err := readableProvider.OpenContent(ctx, connInfo, catalogPath, plugin.ReadOptions{})
	if err != nil {
		return "", err
	}
	defer rc.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, rc); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func setStorageContentHash(attrs models.JSONMap, hash string) {
	if attrs == nil || hash == "" {
		return
	}
	metaattr.SetStorage(attrs, "content_hash", hash)
	metaattr.SetStorage(attrs, "content_hash_algorithm", contentHashAlgorithmSHA256)
}
