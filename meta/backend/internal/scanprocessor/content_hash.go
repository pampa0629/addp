package scanprocessor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
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
	if _, err := io.Copy(hash, progressContentReader{ctx: ctx, reader: rc}); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// Keep cancellation and byte telemetry inside the actual hash read loop.
type progressContentReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r progressContentReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(buffer)
	if n > 0 {
		scanflow.FileProgressFrom(r.ctx).Update(func(p *models.ScanProgress) { p.CurrentBytesRead += int64(n) })
	}
	return n, err
}

func setStorageContentHash(attrs models.JSONMap, hash string) {
	if attrs == nil || hash == "" {
		return
	}
	metaattr.SetStorage(attrs, "content_hash", hash)
	metaattr.SetStorage(attrs, "content_hash_algorithm", contentHashAlgorithmSHA256)
}
