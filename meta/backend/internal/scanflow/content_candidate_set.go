package scanflow

import (
	"context"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/meta/internal/metaitem"
)

type ContentCandidateSet struct {
	DirPath          string
	Files            []metaitem.StorageFileRef
	Subdirs          []metaitem.StorageDirectoryRef
	RecursiveFiles   []metaitem.StorageFileRef
	RecursiveSubdirs []metaitem.StorageDirectoryRef
	ResolveOptions   metaitem.ResolveOptions
	CatalogPathFor   func(string) plugin.CatalogPath
}

func ResolveContentCandidates(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	candidates ContentCandidateSet,
) (*metaitem.DetectionResult, error) {
	return metaitem.ResolveItems(ctx, metaitem.DirectoryResolveInput{
		ContentReader:    contentReader,
		ConnInfo:         connInfo,
		EngineID:         engineID,
		CatalogPathFor:   candidates.CatalogPathFor,
		DirPath:          candidates.DirPath,
		Files:            candidates.Files,
		Subdirs:          candidates.Subdirs,
		Options:          candidates.ResolveOptions,
		RecursiveFiles:   candidates.RecursiveFiles,
		RecursiveSubdirs: candidates.RecursiveSubdirs,
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
		ContentReader:  contentReader,
		ConnInfo:       connInfo,
		EngineID:       engineID,
		CatalogPathFor: candidates.CatalogPathFor,
		DirPath:        candidates.DirPath,
		Files:          candidates.Files,
		Subdirs:        candidates.Subdirs,
		Options:        candidates.ResolveOptions,
	})
}
