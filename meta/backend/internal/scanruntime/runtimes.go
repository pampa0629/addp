package scanruntime

import (
	"log/slog"

	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanadapter"
	"gorm.io/gorm"
)

type Runtimes struct {
	Database              *DatabaseRuntime
	BranchLeaf            *BranchLeafRuntime
	ObjectCatalog         *ObjectStorageCatalogRuntime
	FilesystemCatalog     *FilesystemCatalogRuntime
	ItemRefresh           *ItemRefreshRuntime
	ContentCatalogScanner *scanadapter.ContentCatalogScanner
}

func NewRuntimes(
	db *gorm.DB,
	log *slog.Logger,
	repo *metaRepo.ScanRepository,
	indexer RuntimeIndexer,
) *Runtimes {
	runtimes := &Runtimes{
		Database:          NewDatabaseRuntime(db, log, repo, indexer),
		BranchLeaf:        NewBranchLeafRuntime(db, log, repo),
		ObjectCatalog:     NewObjectStorageCatalogRuntime(db, log, repo, indexer),
		FilesystemCatalog: NewFilesystemCatalogRuntime(db, log, repo, indexer),
		ItemRefresh:       NewItemRefreshRuntime(repo, indexer, log),
	}
	runtimes.ContentCatalogScanner = NewContentCatalogScanner(runtimes.ObjectCatalog, runtimes.FilesystemCatalog)
	return runtimes
}
