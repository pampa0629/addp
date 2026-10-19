package repository_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/addp/system/internal/service"
	"github.com/addp/system/internal/testsupport"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestEngineDeploymentDeclarationsAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("requires standard System PostgreSQL gate")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", "system")
	u.RawQuery = q.Encode()
	db, err := gorm.Open(postgres.Open(u.String()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	reset := func() {
		if err := db.Exec("DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE").Error; err != nil {
			t.Fatal(err)
		}
	}
	reset()
	defer reset()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatal(err)
	}
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatal("migration idempotence", err)
	}
	var grants, excess int64
	if err := db.Raw(`SELECT count(*) FROM role_permissions rp JOIN roles r ON r.id=rp.role_id JOIN permissions p ON p.id=rp.permission_id WHERE p.permission_key LIKE 'platform.engine_deployment.%'`).Scan(&grants).Error; err != nil || grants != 3 {
		t.Fatalf("grants=%d err=%v", grants, err)
	}
	if err := db.Raw(`SELECT count(*) FROM role_permissions rp JOIN roles r ON r.id=rp.role_id JOIN permissions p ON p.id=rp.permission_id WHERE p.permission_key LIKE 'platform.engine_deployment.%' AND (r.role_key <> 'platform.system_administrator' OR r.tenant_id IS NOT NULL)`).Scan(&excess).Error; err != nil || excess != 0 {
		t.Fatalf("excess=%d err=%v", excess, err)
	}
	nodes := service.NewHostNodeService(repository.NewHostNodeRepository(db))
	enabled := true
	nodeInput := models.HostNodeInput{DisplayName: "VM", NodeKind: "virtual", Enabled: &enabled, Addresses: []string{}, AllowedModuleBindings: []models.HostNodeModuleBinding{}}
	node, err := nodes.Create(ctx, nodeInput, iam.AuditMetadata{})
	if err != nil {
		t.Fatal(err)
	}
	node2, err := nodes.Create(ctx, nodeInput, iam.AuditMetadata{})
	if err != nil {
		t.Fatal(err)
	}
	repo := repository.NewEngineDeploymentRepository(db)
	svc := service.NewEngineDeploymentService(repo)
	var inventory models.EngineDeploymentInventory
	fixture := `{"schema":"addp.engine-deployment-inventory/v1","observed_at":"2026-10-19T00:00:00Z","source":{"type":"docker_compose","runtime":"desktop_linux_vm","docker_engine_id":"fixture-engine","project":"business"},"services":[{"service":"postgres","engine_type":"postgresql","containers":[{"container_id":"` + strings.Repeat("a", 64) + `","started_at":"2026-10-18T00:00:00.123456789Z","restart_count":0,"published_ports":[{"address":"127.0.0.1","port":5433,"container_port":5432,"protocol":"tcp"}]}]}]}`
	if err := json.Unmarshal([]byte(fixture), &inventory); err != nil {
		t.Fatal(err)
	}
	input := models.EngineDeploymentInput{DisplayName: "Postgres_100%", Enabled: &enabled, Inventory: inventory, DeclaredNodeID: node.NodeID}
	created, err := svc.Create(ctx, input, iam.AuditMetadata{})
	if err != nil {
		t.Fatal(err)
	}
	if created.Version != 1 || created.DeploymentID == "" || created.NodeID != node.NodeID || created.HostBindingState != "bound" {
		t.Fatalf("created=%+v", created)
	}
	if _, err := svc.Create(ctx, input, iam.AuditMetadata{}); !errors.Is(err, repository.ErrDeploymentSourceOccupied) {
		t.Fatalf("duplicate source=%v", err)
	}
	input.DeclaredNodeID = uuid.NewString()
	if _, err := svc.Create(ctx, input, iam.AuditMetadata{}); !errors.Is(err, repository.ErrDeploymentHostUnavailable) {
		t.Fatalf("unknown host=%v", err)
	}
	input.DeclaredNodeID = node2.NodeID
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.Update(ctx, created.DeploymentID, models.EngineDeploymentUpdateRequest{DisplayName: input.DisplayName, Enabled: input.Enabled, DeclaredNodeID: input.DeclaredNodeID, Version: 1}, iam.AuditMetadata{})
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	wins, conflicts := 0, 0
	for err := range results {
		if err == nil {
			wins++
		} else if errors.Is(err, commonapi.ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatalf("CAS wins=%d conflicts=%d", wins, conflicts)
	}
	current, err := svc.Get(ctx, created.DeploymentID)
	if err != nil || current.Version != 2 || current.NodeID != node2.NodeID {
		t.Fatalf("current=%+v err=%v", current, err)
	}
	var versions, open, wrongInterval int64
	for query, dest := range map[string]*int64{
		"SELECT count(*) FROM engine_deployment_declarations":                        &versions,
		"SELECT count(*) FROM engine_deployment_declarations WHERE valid_to IS NULL": &open,
		"SELECT count(*) FROM engine_deployment_declarations old JOIN engine_deployment_declarations fresh USING(deployment_id) WHERE old.version=1 AND fresh.version=2 AND old.valid_to IS DISTINCT FROM fresh.valid_from": &wrongInterval,
	} {
		if err := db.Raw(query).Scan(dest).Error; err != nil {
			t.Fatal(err)
		}
	}
	if versions != 2 || open != 1 || wrongInterval != 0 {
		t.Fatalf("history versions=%d open=%d gaps=%d", versions, open, wrongInterval)
	}
	if err := db.Exec("UPDATE engine_deployment_declarations SET node_id=? WHERE deployment_id=? AND version=1", node2.NodeID, created.DeploymentID).Error; err == nil {
		t.Fatal("rewrote closed history")
	}
	if err := db.Exec("DELETE FROM engine_deployment_declarations WHERE deployment_id=?", created.DeploymentID).Error; err == nil {
		t.Fatal("deleted declaration history")
	}
	for _, search := range []string{"_", "%"} {
		page, err := svc.List(ctx, 1, 20, models.EngineDeploymentFilter{Search: search, NodeID: node2.NodeID})
		if err != nil || page.Total != 1 {
			t.Fatalf("literal search=%s result=%+v err=%v", search, page, err)
		}
	}
	copyValue := *current
	copyValue.DisplayName = "must roll back"
	auditFailure := errors.New("audit failure")
	if err := repo.Save(ctx, &copyValue, 2, func(*gorm.DB, *models.EngineDeployment, models.EngineDeployment) error { return auditFailure }); !errors.Is(err, auditFailure) {
		t.Fatalf("audit failure=%v", err)
	}
	current, err = svc.Get(ctx, created.DeploymentID)
	if err != nil || current.Version != 2 || current.DisplayName != "Postgres_100%" {
		t.Fatalf("failed audit changed aggregate: %+v %v", current, err)
	}
	if err := db.Raw("SELECT count(*) FROM engine_deployment_declarations").Scan(&versions).Error; err != nil || versions != 2 {
		t.Fatalf("failed audit changed history: %d %v", versions, err)
	}
	disabled := false
	nodeInput.Enabled = &disabled
	node2, err = nodes.Update(ctx, node2.NodeID, models.HostNodeUpdateRequest{HostNodeInput: nodeInput, Version: node2.Version}, iam.AuditMetadata{})
	if err != nil {
		t.Fatal(err)
	}
	current, err = svc.Get(ctx, created.DeploymentID)
	if err != nil || current.NodeID != "" || current.DeclaredNodeID != node2.NodeID || current.Version != 2 || current.HostBindingReason != "node_disabled" {
		t.Fatalf("disabled host=%+v err=%v", current, err)
	}
	if _, err := svc.Update(ctx, created.DeploymentID, models.EngineDeploymentUpdateRequest{DisplayName: input.DisplayName, Enabled: input.Enabled, DeclaredNodeID: input.DeclaredNodeID, Version: 2}, iam.AuditMetadata{}); !errors.Is(err, repository.ErrDeploymentHostUnavailable) {
		t.Fatalf("enabled declaration on disabled host=%v", err)
	}
	input.Enabled = &disabled
	retired, err := svc.Update(ctx, created.DeploymentID, models.EngineDeploymentUpdateRequest{DisplayName: input.DisplayName, Enabled: input.Enabled, DeclaredNodeID: input.DeclaredNodeID, Version: 2}, iam.AuditMetadata{})
	if err != nil || retired.Version != 3 || retired.HostBindingReason != "deployment_disabled" {
		t.Fatalf("retire=%+v err=%v", retired, err)
	}
	nodeInput.Enabled = &enabled
	if _, err := nodes.Update(ctx, node2.NodeID, models.HostNodeUpdateRequest{HostNodeInput: nodeInput, Version: node2.Version}, iam.AuditMetadata{}); err != nil {
		t.Fatal(err)
	}
	input.Enabled = &enabled
	restored, err := svc.Update(ctx, created.DeploymentID, models.EngineDeploymentUpdateRequest{DisplayName: input.DisplayName, Enabled: input.Enabled, DeclaredNodeID: input.DeclaredNodeID, Version: 3}, iam.AuditMetadata{})
	if err != nil || restored.Version != 4 || restored.NodeID != node2.NodeID {
		t.Fatalf("restore=%+v err=%v", restored, err)
	}
	nodeInput.NodeKind = "physical"
	node2, err = nodes.Get(ctx, node2.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	node2, err = nodes.Update(ctx, node2.NodeID, models.HostNodeUpdateRequest{HostNodeInput: nodeInput, Version: node2.Version}, iam.AuditMetadata{})
	if err != nil {
		t.Fatal(err)
	}
	changedKind, err := svc.Get(ctx, created.DeploymentID)
	if err != nil || changedKind.NodeID != "" || changedKind.Version != 4 || changedKind.HostBindingReason != "node_kind_mismatch" {
		t.Fatalf("changed host kind=%+v %v", changedKind, err)
	}

	var audits int64
	if err := db.Model(&iam.AuditLog{}).Where("entity_type=? AND entity_id=?", "engine_deployment", created.DeploymentID).Count(&audits).Error; err != nil || audits != 4 {
		t.Fatalf("audit rows=%d err=%v", audits, err)
	}
	t.Run("batch_admission_rolls_back_all_members_and_registers_sources", func(t *testing.T) {
		directory := t.TempDir()
		if err := os.Chmod(directory, 0700); err != nil {
			t.Fatal(err)
		}
		for _, scope := range []string{"infra", "business"} {
			project := "business"
			if scope == "infra" {
				project = "addp-infra"
			}
			value := models.DeploymentDiscoveryScope{Schema: "addp.deployment-discovery/v1", Scope: scope, State: "available", ObservedAt: time.Now().UTC().Format(time.RFC3339), Source: &models.EngineDeploymentSource{Type: "docker_compose", Runtime: "desktop_linux_vm", DockerEngineID: "batch-daemon", Project: project}, Services: []models.DiscoveredDeploymentService{{Service: "postgres", EngineType: "postgresql", Containers: []models.DiscoveredDeploymentContainer{{ContainerID: strings.Repeat("b", 64), Status: "running", StartedAt: time.Now().UTC().Add(-time.Hour).Format(time.RFC3339), PublishedPorts: []models.EngineDeploymentPort{{Address: "127.0.0.1", Port: 5433, ContainerPort: 5432, Protocol: "tcp"}}}}}}}
			raw, _ := json.Marshal(value)
			if err := os.WriteFile(filepath.Join(directory, scope+".json"), raw, 0600); err != nil {
				t.Fatal(err)
			}
		}
		reader := service.DeploymentDiscoveryService{Directory: directory}
		discovery := reader.Read(ctx)
		request := models.EngineDeploymentAdmissionRequest{}
		for _, candidate := range discovery.Candidates {
			request.Items = append(request.Items, models.EngineDeploymentAdmissionItem{Scope: candidate.Scope, Service: candidate.Service, Fingerprint: candidate.Fingerprint, DisplayName: candidate.Scope + " PostgreSQL", Enabled: &enabled, DeclaredNodeID: node.NodeID})
		}
		if len(request.Items) != 2 {
			t.Fatalf("candidates=%+v", discovery)
		}
		counts := func() [3]int64 {
			var result [3]int64
			db.Model(&models.EngineDeployment{}).Count(&result[0])
			db.Table("engine_deployment_declarations").Count(&result[1])
			db.Model(&iam.AuditLog{}).Where("entity_type=?", "engine_deployment").Count(&result[2])
			return result
		}
		before := counts()
		request.Items[1].DeclaredNodeID = uuid.NewString()
		if result, err := svc.Admit(ctx, request, discovery, iam.AuditMetadata{}); !errors.Is(err, repository.ErrDeploymentHostUnavailable) || result != nil {
			t.Fatalf("invalid second member=%+v %v", result, err)
		}
		if after := counts(); after != before {
			t.Fatalf("partial batch persisted: %v -> %v", before, after)
		}
		request.Items[1].DeclaredNodeID = node.NodeID
		result, err := svc.Admit(ctx, request, discovery, iam.AuditMetadata{})
		if err != nil || len(result.Data) != 2 {
			t.Fatalf("batch=%+v %v", result, err)
		}
		for _, row := range result.Data {
			if row.Version != 1 || row.HostBindingState != "bound" || row.NodeID != node.NodeID {
				t.Fatalf("admitted=%+v", row)
			}
		}
		if after := counts(); after != [3]int64{before[0] + 2, before[1] + 2, before[2] + 2} {
			t.Fatalf("batch counts=%v", after)
		}
		if err := svc.MarkRegisteredDiscovery(ctx, &discovery); err != nil {
			t.Fatal(err)
		}
		for _, candidate := range discovery.Candidates {
			if candidate.DeploymentID == "" {
				t.Fatal("registered source missing stable ID")
			}
		}
		before = counts()
		if _, err := svc.Admit(ctx, request, discovery, iam.AuditMetadata{}); !errors.Is(err, repository.ErrDeploymentSourceOccupied) {
			t.Fatalf("duplicate batch=%v", err)
		}
		if counts() != before {
			t.Fatal("duplicate batch changed state")
		}
	})

}
