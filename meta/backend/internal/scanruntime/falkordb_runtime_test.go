package scanruntime

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
	falkor "github.com/addp/common/engine/plugins/falkordb"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	rdb "github.com/redis/go-redis/v9"
)

func TestIntegrationFalkorDBDirectGraphScan(t *testing.T) {
	if os.Getenv("ADDP_FALKORDB_INTEGRATION") != "1" {
		t.Skip("owned FalkorDB T2 gate required")
	}
	host, portText, err := net.SplitHostPort(os.Getenv("FALKORDB_T2_ENDPOINT"))
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	client := rdb.NewClient(&rdb.Options{Addr: os.Getenv("FALKORDB_T2_ENDPOINT"), Password: os.Getenv("INFRA_FALKORDB_PASSWORD"), Protocol: 2, DisableIdentity: true})
	defer client.Close()
	const name = "scan.图/opaque"
	if err = client.Do(t.Context(), "GRAPH.QUERY", name, "CREATE (:Person {name:'甲'})").Err(); err != nil {
		t.Fatal(err)
	}
	defer client.Do(context.Background(), "GRAPH.DELETE", name)
	db := metatest.OpenMetadataDB(t)
	repo := metaRepo.NewScanRepository(db)
	p := &falkor.FalkorDBPlugin{}
	engine := &commonModels.Engine{ID: 91, Name: "FalkorDB T2", EngineType: p.Type(), ConnectionInfo: commonModels.ConnectionInfo{"host": host, "port": port, "password": os.Getenv("INFRA_FALKORDB_PASSWORD")}}
	runtime := NewDirectLeafRuntime(slog.New(slog.NewTextHandler(io.Discard, nil)), repo, nil)
	path, err := resourcetree.EngineCatalogPathFromLocator(p.EngineCatalogModel(), &resourcetree.ResourceLocator{EngineID: 91, Type: resourcetree.TypeGraph, Path: []string{name}})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := p.ResolvePath(t.Context(), plugin.ConnectionInfo(engine.ConnectionInfo), path)
	if err != nil {
		t.Fatal(err)
	}
	if _, count, _, err := runtime.ScanLeaf(t.Context(), p, engine, 1, *entry, models.ScannedDepthDeep, true); err != nil || count != 1 {
		t.Fatalf("scan count=%d err=%v", count, err)
	}
	item, found, err := repo.FindItemByFullName(1, 91, name)
	if err != nil || !found || item.ItemType != "graph" {
		t.Fatal(item, found, err)
	}
	encoded, _ := json.Marshal(item.Attributes)
	if !strings.Contains(string(encoded), `"data_type":"graph"`) || !strings.Contains(string(encoded), `"node_shapes"`) {
		t.Fatal(string(encoded))
	}
	identity, err := resourcetree.DataItemIdentityFromCatalogPath(p.EngineCatalogModel(), path)
	if err != nil || identity.Fingerprint != item.Fingerprint {
		t.Fatalf("provider and Meta identity diverged: %v", err)
	}
	before := item.ID
	if _, count, _, err := runtime.ScanLeaf(t.Context(), p, engine, 1, *entry, models.ScannedDepthDeep, true); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	again, _, _ := repo.FindItemByFullName(1, 91, name)
	if again.ID != before || again.Fingerprint != item.Fingerprint {
		t.Fatal("rescan changed graph identity")
	}
}
