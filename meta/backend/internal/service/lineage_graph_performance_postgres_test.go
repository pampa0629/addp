package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/addp/meta/internal/models"
	metaMigrations "github.com/addp/meta/migrations"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type lineageGraphQueryMetrics struct {
	logger.Interface
	queries, evidenceQueries int
}

func (m *lineageGraphQueryMetrics) Trace(_ context.Context, _ time.Time, query func() (string, int64), _ error) {
	sql, _ := query()
	m.queries++
	if strings.Contains(sql, "lineage_observations") {
		m.evidenceQueries++
	}
}

// This is a service/JSON benchmark on real PostgreSQL, not HTTP, System or
// browser latency. Fixed inputs and query-count assertions make it a CI gate;
// elapsed samples are diagnostic and deliberately have no wall-clock limit.
func TestLineageGraphPerformanceAgainstPostgres(t *testing.T) {
	for _, shape := range [][2]int{{10, 9}, {20, 24}} {
		width, leaves := shape[0], shape[1]
		count := width * (leaves + 1)
		t.Run(fmt.Sprintf("edges_%d", count), func(t *testing.T) {
			tx := openLineageCollectorPostgresDB(t)
			if err := tx.Migrator().DropTable(&models.LineageObservation{}, &models.LineageItemRelation{}, &models.LineageServiceDependency{}); err != nil {
				t.Fatal(err)
			}
			// Use the production relation constraints and indexes, not only the
			// single-column indexes inferred by AutoMigrate.
			for _, name := range []string{"018_add_lineage_relations.sql", "023_lineage_service_display.sql", "026_field_lineage.sql"} {
				migration, err := metaMigrations.FS.ReadFile(name)
				if err != nil {
					t.Fatal(err)
				}
				if err := tx.Exec(string(migration)).Error; err != nil {
					t.Fatal(err)
				}
			}
			items := make([]models.MetaItem, 1+width+leaves)
			for i := range items {
				items[i] = models.MetaItem{TenantID: 7, EngineID: 9, NodeID: 1, ItemType: "table",
					Name:        fmt.Sprintf("graph-%d-node-%03d", count, i),
					FullName:    fmt.Sprintf("public.graph_%d_node_%03d", count, i),
					Fingerprint: fmt.Sprintf("graph-%d-node-%03d", count, i),
					Attributes:  models.JSONMap{"unused": strings.Repeat("metadata", 1024)}}
			}
			items[0].EngineID = 10
			if err := tx.Create(&items).Error; err != nil {
				t.Fatal(err)
			}
			at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			relations := make([]models.LineageItemRelation, 0, count)
			add := func(source, target int) {
				relations = append(relations, models.LineageItemRelation{TenantID: 7,
					SourceItemID: items[source].ID, TargetItemID: items[target].ID,
					RelationKind: "derive", Granularity: "item", Status: "active",
					FirstObservedAt: at, LastObservedAt: at})
			}
			for middle := 1; middle <= width; middle++ {
				add(0, middle)
				for leaf := width + 1; leaf < len(items); leaf++ {
					add(middle, leaf)
				}
			}
			if len(relations) != count {
				t.Fatalf("fixture has %d relations, want %d", len(relations), count)
			}
			if err := tx.CreateInBatches(&relations, 100).Error; err != nil {
				t.Fatal(err)
			}
			observations := make([]models.LineageObservation, 0, count*2)
			for i, relation := range relations {
				for _, version := range []string{"old", "latest"} {
					observations = append(observations, models.LineageObservation{TenantID: 7,
						SourceItemID: uintPtr(relation.SourceItemID), TargetItemID: uintPtr(relation.TargetItemID),
						RelationKind: "derive", Granularity: "item", ProducerModule: "develop", CaptureMethod: "runtime",
						SourceSnapshot: models.JSONMap{"unused": strings.Repeat("source", 1024)},
						TargetSnapshot: models.JSONMap{"unused": strings.Repeat("target", 1024)},
						Evidence: models.JSONMap{"execution_id": fmt.Sprintf("graph-%d-edge-%03d-%s", count, i, version),
							"write_mode": "append", "custom": "preserved"}, ObservedAt: at})
				}
			}
			if err := tx.CreateInBatches(&observations, 100).Error; err != nil {
				t.Fatal(err)
			}
			metrics := &lineageGraphQueryMetrics{Interface: logger.Default.LogMode(logger.Silent)}
			svc := NewLineageService(tx.Session(&gorm.Session{Logger: metrics}), lineageTestEngineCatalog{})
			request := models.LineageGraphRequest{SubjectKind: "data_item", ItemID: &items[0].ID,
				Direction: "downstream", Depth: 2, Limit: 500}
			var graphTimes, encodingTimes []float64
			var digest [32]byte
			var bytes, queries, proofs int
			for sample := 0; sample < 21; sample++ {
				metrics.queries, metrics.evidenceQueries = 0, 0
				started := time.Now()
				graph, err := svc.GetGraph(t.Context(), 7, request)
				elapsed := float64(time.Since(started)) / float64(time.Millisecond)
				if err != nil {
					t.Fatal(err)
				}
				if graph.Truncated || len(graph.Edges) != count || len(graph.Nodes) != len(items) {
					t.Fatalf("graph: %d edges/%d nodes, truncated=%v", len(graph.Edges), len(graph.Nodes), graph.Truncated)
				}
				for _, edge := range graph.Edges {
					id, _ := edge.Evidence["execution_id"].(string)
					if !strings.HasSuffix(id, "-latest") || edge.Evidence["custom"] != "preserved" {
						t.Fatalf("latest evidence changed: %+v", edge.Evidence)
					}
				}
				started = time.Now()
				encoded, err := json.Marshal(graph)
				encodingTimes = append(encodingTimes, float64(time.Since(started))/float64(time.Millisecond))
				if err != nil {
					t.Fatal(err)
				}
				if sample == 0 {
					digest, bytes = sha256.Sum256(encoded), len(encoded)
					t.Logf("first_graph_ms=%.3f", elapsed)
				} else if sha256.Sum256(encoded) != digest {
					t.Fatal("repeated query changed the graph response")
				}
				graphTimes = append(graphTimes, elapsed)
				queries, proofs = metrics.queries, metrics.evidenceQueries
			}
			sort.Float64s(graphTimes)
			sort.Float64s(encodingTimes)
			t.Logf("edges=%d samples=21 sql=%d evidence_sql=%d graph_p50_ms=%.3f graph_p95_ms=%.3f json_p50_ms=%.3f json_p95_ms=%.3f bytes=%d sha256=%x",
				count, queries, proofs, graphTimes[10], graphTimes[19], encodingTimes[10], encodingTimes[19], bytes, digest)
			if proofs > 2 {
				t.Errorf("%d evidence queries for two layers; want at most 2 independent of edge count", proofs)
			}
		})
	}
}
