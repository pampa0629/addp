package scanprocessor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"sort"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
)

const contentHashAlgorithmSHA256 = "sha256"

func computeContentSHA256(ctx context.Context, readableProvider plugin.ContentReadableProvider, connInfo plugin.ConnectionInfo, catalogPath plugin.EngineCatalogPath) (string, error) {
	version, err := computeContentVersion(ctx, readableProvider, connInfo, catalogPath)
	return version.SHA256, err
}

func computeContentVersion(ctx context.Context, readableProvider plugin.ContentReadableProvider, connInfo plugin.ConnectionInfo, catalogPath plugin.EngineCatalogPath) (dataitem.ContentVersion, error) {
	if readableProvider == nil || len(catalogPath.Segments) == 0 {
		return dataitem.ContentVersion{}, fmt.Errorf("content version requires a readable catalog leaf")
	}
	rc, err := readableProvider.OpenContent(ctx, connInfo, catalogPath, plugin.ReadOptions{})
	if err != nil {
		return dataitem.ContentVersion{}, err
	}
	defer rc.Close()

	hash := sha256.New()
	size, err := io.Copy(hash, progressContentReader{ctx: ctx, reader: rc})
	if err != nil {
		return dataitem.ContentVersion{}, err
	}
	if err := ctx.Err(); err != nil {
		return dataitem.ContentVersion{}, err
	}
	return dataitem.ContentVersion{Path: catalogPath.StringPath(), SizeBytes: size, SHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}

func collectTIFFContentVersions(ctx context.Context, input *input, attrs models.JSONMap) error {
	paths := map[string]bool{input.PhysicalPath: true}
	for _, ref := range input.Detected.RefList {
		paths[ref.Path] = true
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	versions := make([]dataitem.ContentVersion, 0, len(ordered))
	for _, path := range ordered {
		scanflow.FileProgressFrom(ctx).Operation("hashing", path)
		version, err := computeContentVersion(ctx, input.ContentReader, input.ConnInfo, input.EngineCatalogPathFor(path))
		if err != nil {
			return fmt.Errorf("TIFF source component version: %w", err)
		}
		versions = append(versions, version)
	}
	storage := metaattr.Section(attrs, "storage")
	delete(storage, "content_hash")
	delete(storage, "content_hash_algorithm")
	storage["content_versions"] = versions
	return nil
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
