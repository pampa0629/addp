package scanflow

import (
	"context"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/meta/internal/metaitem"
)

func FileCatalogDirectoryCandidateSet(
	engineID uint,
	dirPath string,
	files []metaitem.StorageFileRef,
	subdirs []metaitem.StorageDirectoryRef,
	recursiveFiles []metaitem.StorageFileRef,
	recursiveSubdirs []metaitem.StorageDirectoryRef,
) ContentCandidateSet {
	return ContentCandidateSet{
		CatalogPathFor:   plugin.FileItemPathForEngine(engineID),
		DirPath:          dirPath,
		Files:            files,
		Subdirs:          subdirs,
		RecursiveFiles:   recursiveFiles,
		RecursiveSubdirs: recursiveSubdirs,
	}
}

func FileCatalogNonExclusiveCandidateSet(
	engineID uint,
	dirPath string,
	files []metaitem.StorageFileRef,
	subdirs []metaitem.StorageDirectoryRef,
) ContentCandidateSet {
	return ContentCandidateSet{
		CatalogPathFor: plugin.FileItemPathForEngine(engineID),
		DirPath:        dirPath,
		Files:          files,
		Subdirs:        subdirs,
	}
}

func DetectFileCatalogDirectoryItems(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	dirPath string,
	files []metaitem.StorageFileRef,
	subdirs []metaitem.StorageDirectoryRef,
	recursiveFiles []metaitem.StorageFileRef,
	recursiveSubdirs []metaitem.StorageDirectoryRef,
) (*metaitem.DetectionResult, error) {
	return ResolveContentCandidates(ctx, contentReader, connInfo, engineID, FileCatalogDirectoryCandidateSet(
		engineID,
		dirPath,
		files,
		subdirs,
		recursiveFiles,
		recursiveSubdirs,
	))
}

func DetectFileCatalogNonExclusiveScopeItems(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	dirPath string,
	files []metaitem.StorageFileRef,
	subdirs []metaitem.StorageDirectoryRef,
) (*metaitem.DetectionResult, error) {
	return ResolveNonExclusiveContentCandidates(ctx, contentReader, connInfo, engineID, FileCatalogNonExclusiveCandidateSet(
		engineID,
		dirPath,
		files,
		subdirs,
	))
}
