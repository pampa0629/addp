package scanruntime

import (
	"log/slog"

	"github.com/addp/meta/internal/metaenrich"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanadapter"
	"github.com/addp/meta/internal/scanprocessor"
	"gorm.io/gorm"
)

type Runtimes struct {
	Database                    *DatabaseRuntime
	BranchLeaf                  *BranchLeafRuntime
	DirectLeaf                  *DirectLeafRuntime
	ObjectCatalog               *ObjectStorageCatalogRuntime
	FilesystemCatalog           *FilesystemCatalogRuntime
	ItemRefresh                 *ItemRefreshRuntime
	EngineCatalogContentScanner *scanadapter.EngineCatalogContentScanner
}

func (r *Runtimes) SetContainerInspector(inspector metaenrich.ContainerInspector) {
	if r == nil {
		return
	}
	if r.ObjectCatalog != nil {
		r.ObjectCatalog.containerInspector = inspector
	}
	if r.FilesystemCatalog != nil {
		r.FilesystemCatalog.containerInspector = inspector
	}
	if r.ItemRefresh != nil {
		r.ItemRefresh.containerInspector = inspector
	}
}

func NewRuntimes(
	db *gorm.DB,
	log *slog.Logger,
	repo *metaRepo.ScanRepository,
	indexer scanprocessor.AssetIndexer,
) *Runtimes {
	runtimes := &Runtimes{
		Database:          NewDatabaseRuntime(db, log, repo, indexer),
		BranchLeaf:        NewBranchLeafRuntime(db, log, repo, indexer),
		DirectLeaf:        NewDirectLeafRuntime(log, repo, indexer),
		ObjectCatalog:     NewObjectStorageCatalogRuntime(db, log, repo, indexer),
		FilesystemCatalog: NewFilesystemCatalogRuntime(db, log, repo, indexer),
		ItemRefresh:       NewItemRefreshRuntime(repo, indexer, log),
	}
	runtimes.EngineCatalogContentScanner = NewRuntimeEngineCatalogContentScanner(runtimes.ObjectCatalog, runtimes.FilesystemCatalog)
	return runtimes
}
