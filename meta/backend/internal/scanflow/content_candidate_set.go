package scanflow

import (
	"context"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/meta/internal/metaitem"
)

type ContentCandidateSet struct {
	StructureReader      plugin.ContentReadableProvider
	DirPath              string
	Files                []metaitem.StorageFileRef
	Subdirs              []metaitem.StorageDirectoryRef
	RecursiveFiles       []metaitem.StorageFileRef
	RecursiveSubdirs     []metaitem.StorageDirectoryRef
	ResolveOptions       metaitem.ResolveOptions
	EngineCatalogPathFor func(string) plugin.EngineCatalogPath
}

func ResolveContentCandidates(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	candidates ContentCandidateSet,
) (*metaitem.DetectionResult, error) {
	return metaitem.ResolveItems(ctx, metaitem.DirectoryResolveInput{
		ContentReader:        contentReader,
		StructureReader:      candidates.StructureReader,
		ConnInfo:             connInfo,
		EngineID:             engineID,
		EngineCatalogPathFor: candidates.EngineCatalogPathFor,
		DirPath:              candidates.DirPath,
		Files:                candidates.Files,
		Subdirs:              candidates.Subdirs,
		Options:              candidates.ResolveOptions,
		RecursiveFiles:       candidates.RecursiveFiles,
		RecursiveSubdirs:     candidates.RecursiveSubdirs,
	})
}

func ResolveNonExclusiveContentCandidates(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	candidates ContentCandidateSet,
) (*metaitem.DetectionResult, error) {
	return metaitem.ResolveNonExclusiveItems(ctx, metaitem.DirectoryResolveInput{
		ContentReader:        contentReader,
		StructureReader:      candidates.StructureReader,
		ConnInfo:             connInfo,
		EngineID:             engineID,
		EngineCatalogPathFor: candidates.EngineCatalogPathFor,
		DirPath:              candidates.DirPath,
		Files:                candidates.Files,
		Subdirs:              candidates.Subdirs,
		Options:              candidates.ResolveOptions,
	})
}

// ContentReaderForDepth keeps content-dependent detection and enrichment in deep scans.
func ContentReaderForDepth(reader plugin.ContentReadableProvider, scanDepth string) plugin.ContentReadableProvider {
	if !strings.EqualFold(scanDepth, "deep") {
		return nil
	}
	return reader
}
