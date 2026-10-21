package service

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"path"
	"testing"
	"time"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/ftp"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanruntime"
	"gorm.io/gorm"
)

// Only the source transport is synthetic. Directory creation, item detection,
// persistence and the formal authorization projection use product code.
type fileFactsScanProvider struct {
	ftp.FTPPlugin
	engineID uint
	primary  string
	content  []byte
	entries  []plugin.EngineCatalogEntry
	lists    int
	ranges   int
	streams  int
}

func (p *fileFactsScanProvider) ListChildren(_ context.Context, _ plugin.ConnectionInfo, target plugin.EngineCatalogPath, _ plugin.ListOptions) ([]plugin.EngineCatalogEntry, error) {
	p.lists++
	if target.StringPath() != path.Dir(p.primary) {
		return nil, fmt.Errorf("unexpected source directory")
	}
	return p.entries, nil
}

func (p *fileFactsScanProvider) ResolvePath(_ context.Context, _ plugin.ConnectionInfo, target plugin.EngineCatalogPath) (*plugin.EngineCatalogEntry, error) {
	for _, entry := range p.entries {
		if entry.Path.StringPath() == target.StringPath() {
			return &entry, nil
		}
	}
	return nil, nil
}

func (p *fileFactsScanProvider) OpenContent(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.ReadOptions) (io.ReadCloser, error) {
	p.streams++
	return nil, fmt.Errorf("basic identification must not open content streams")
}

func (p *fileFactsScanProvider) OpenRange(ctx context.Context, _ plugin.ConnectionInfo, target plugin.EngineCatalogPath, opts plugin.ReadOptions) (io.ReadCloser, error) {
	p.ranges++
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if target.StringPath() != p.primary || target.EngineID != p.engineID || opts.Offset < 0 || opts.Length <= 0 || opts.Offset+opts.Length > int64(len(p.content)) {
		return nil, fmt.Errorf("structure read escaped the observed primary")
	}
	return io.NopCloser(bytes.NewReader(p.content[opts.Offset : opts.Offset+opts.Length])), nil
}

func TestScannedFileAuthorizationFacts(t *testing.T) {
	exerciseScannedFileAuthorizationFacts(t, metatest.OpenMetadataDB(t))
}

func exerciseScannedFileAuthorizationFacts(t *testing.T, db *gorm.DB) {
	t.Helper()
	for fi, format := range []string{"ifc", "3ds"} {
		for mi, mode := range []string{"directory", "refs"} {
			t.Run("scanned/"+format+"/"+mode, func(t *testing.T) {
				engineID := uint(770 + fi*2 + mi)
				primary := "nfs-copy/3d/models/panel." + format
				p := &fileFactsScanProvider{engineID: engineID, primary: primary, content: []byte("synthetic IFC source; basic scan does not parse it")}
				if format == "3ds" {
					chunk := func(id uint16, body []byte) []byte {
						header := make([]byte, 6)
						binary.LittleEndian.PutUint16(header, id)
						binary.LittleEndian.PutUint32(header[2:], uint32(len(body)+6))
						return append(header, body...)
					}
					p.content = chunk(0x4d4d, chunk(0x3d3d, chunk(0xafff, chunk(0xa200, chunk(0xa300, []byte("TEST.PNG\x00"))))))
				}
				entry := func(name string, size int64) plugin.EngineCatalogEntry {
					return plugin.EngineCatalogEntry{Name: path.Base(name), Path: plugin.FileItemPath(engineID, name), Term: plugin.EngineCatalogTermFile,
						Kind: plugin.EngineCatalogKindFile, Role: plugin.EngineCatalogRoleLeaf,
						Storage: &plugin.EngineCatalogStorageFacts{Path: name, SizeBytes: &size}}
				}
				p.entries = []plugin.EngineCatalogEntry{entry(primary, int64(len(p.content)))}
				refs := []models.ScanRef{{Path: primary, Role: "model", Required: true}}
				if format == "3ds" {
					texture := path.Dir(primary) + "/TEST.PNG"
					p.entries = append(p.entries, entry(texture, 20))
					refs = append(refs, models.ScanRef{Path: texture, Role: "texture", Required: true})
				}
				previous, previousErr := plugin.Get("ftp")
				plugin.Register(p)
				t.Cleanup(func() {
					plugin.Unregister("ftp")
					if previousErr == nil {
						plugin.Register(previous)
					}
				})
				runtime := scanruntime.NewFilesystemCatalogRuntime(db, slog.New(slog.NewTextHandler(io.Discard, nil)), metaRepo.NewScanRepository(db), nil)
				engine := &commonModels.Engine{ID: engineID, EngineType: "ftp", Name: "FTP synthetic"}
				var err error
				if mode == "directory" {
					_, err = runtime.ScanPaths(t.Context(), engine, 7, []string{path.Dir(primary)}, "basic", true, nil)
				} else {
					_, err = runtime.ScanRefGroups(t.Context(), engine, 7, []models.ScanRefGroup{{Primary: primary, Refs: refs}}, "basic", true, nil)
				}
				if err != nil {
					t.Fatal(err)
				}
				if p.streams != 0 || (format == "3ds" && p.ranges == 0) || (mode == "refs" && p.lists != 0) {
					t.Fatal("scan bypassed its basic structural read boundary")
				}
				var item models.MetaItem
				if err := db.Where("tenant_id=? AND engine_id=? AND full_name=?", 7, engineID, primary).First(&item).Error; err != nil {
					t.Fatal(err)
				}
				query := NewMetadataQueryService(db)
				reads := func(t *testing.T, want authorizationfacts.AuthorizationBasisState) *authorizationfacts.AuthorizationFacts {
					t.Helper()
					facts, err := query.GetDataItemAuthorizationFacts(t.Context(), 7, item.Fingerprint)
					if err != nil || facts.BasisState != want {
						t.Fatalf("formal facts=%+v err=%v, want=%s", facts, err, want)
					}
					if want != authorizationfacts.AuthorizationBasisComplete && (facts.Boundary != nil || facts.BoundaryHash != nil) {
						t.Fatal("invalid parent chain retained a provable boundary")
					}
					return facts
				}
				beforeLists, beforeRanges := p.lists, p.ranges
				facts := reads(t, authorizationfacts.AuthorizationBasisComplete)
				binding, err := facts.ProvePrimaryTarget(plugin.FileItemPath(engineID, primary))
				if err != nil || binding.FileComposition == nil || len(binding.FileComposition.Refs) != len(refs) {
					t.Fatalf("scanner result did not prove its exact model composition: %+v %v", binding, err)
				}
				if _, err := facts.ProvePrimaryTarget(plugin.FileItemPath(engineID, path.Dir(primary)+"/sibling."+format)); err == nil {
					t.Fatal("formal facts proved a sibling model")
				}
				var parent models.MetaNode
				if err := db.First(&parent, item.NodeID).Error; err != nil || parent.NodeType != "dir" || parent.ParentNodeID == nil {
					t.Fatalf("fixture did not use scanner-owned Meta dir: %+v %v", parent, err)
				}
				for _, tc := range []struct {
					name  string
					field string
					value any
					old   any
					state authorizationfacts.AuthorizationBasisState
				}{
					{"catalog term is not a Meta node type", "node_type", "directory", "dir", authorizationfacts.AuthorizationBasisNeedsRefresh},
					{"other tenant", "tenant_id", 8, 7, authorizationfacts.AuthorizationBasisIncomplete},
					{"other engine", "engine_id", engineID + 1, engineID, authorizationfacts.AuthorizationBasisIncomplete},
					{"wrong directory name", "name", "other", parent.Name, authorizationfacts.AuthorizationBasisNeedsRefresh},
					{"wrong directory path", "full_name", "other", parent.FullName, authorizationfacts.AuthorizationBasisNeedsRefresh},
					{"orphan directory", "parent_node_id", nil, parent.ParentNodeID, authorizationfacts.AuthorizationBasisNeedsRefresh},
					{"missing ancestor", "parent_node_id", uint(99999999), parent.ParentNodeID, authorizationfacts.AuthorizationBasisIncomplete},
					{"deleted directory", "deleted_at", time.Now().UTC(), nil, authorizationfacts.AuthorizationBasisIncomplete},
				} {
					t.Run(tc.name, func(t *testing.T) {
						if err := db.Model(&models.MetaNode{}).Where("id=?", parent.ID).Update(tc.field, tc.value).Error; err != nil {
							t.Fatal(err)
						}
						t.Cleanup(func() {
							if err := db.Unscoped().Model(&models.MetaNode{}).Where("id=?", parent.ID).Update(tc.field, tc.old).Error; err != nil {
								t.Error(err)
							}
						})
						reads(t, tc.state)
					})
				}
				reads(t, authorizationfacts.AuthorizationBasisComplete)
				if p.lists != beforeLists || p.ranges != beforeRanges || p.streams != 0 {
					t.Fatal("formal facts projection performed source I/O")
				}
			})
		}
	}
}
