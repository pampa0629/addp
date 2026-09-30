package service

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
	"gorm.io/gorm"
)

// Exercise the production services and PostgreSQL transactions, rather than
// inserting an already-withdrawn revision. Object storage is a controlled fixture.
func TestPostgresStandardRevisionLifecycleRetainsWithdrawnContent(t *testing.T) {
	db := openStandardReferenceDeletionPostgres(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close PostgreSQL: %v", err)
		}
	})
	from := time.Now().UTC().Add(-time.Hour)

	t.Run("element", func(t *testing.T) {
		f := newRevisionLifecycleFixture(t, db)
		svc := NewElementService(repository.NewElementRepository(f.tx), nil, repository.NewTenantReferenceRepository(f.tx), nil)
		created, err := svc.CreateElement(&models.CreateElementRequest{ScopeType: models.StandardScopeTenantCommon, Code: "lifecycle_element", Name: "历史数据元", Definition: "保留业务定义", DataType: "string", ValueDomainKind: models.ValueDomainUnrestricted, EffectiveFrom: &from}, f.tenantID, 1, "初始创建")
		if err != nil {
			t.Fatal(err)
		}
		f.track(t, "elements", "element_revisions", created)
		withdrawn := runRevisionLifecycle(t, created, f.tenantID, svc.SubmitRevision, svc.PublishRevision, svc.WithdrawRevision)
		detail, err := svc.GetElement(created.ID, f.tenantID)
		assertWithdrawnLifecycleRead(t, detail, err, withdrawn)
		list, total, err := svc.ListElements(f.tenantID, repository.ListElementOptions{Keyword: "历史数据元"})
		assertWithdrawnLifecycleList(t, list, total, err, withdrawn)
		list, total, err = svc.ListElements(f.tenantID, repository.ListElementOptions{Status: models.RevisionStatusWithdrawn})
		assertWithdrawnLifecycleList(t, list, total, err, withdrawn)
		if detail.LatestRevision.Definition != "保留业务定义" || detail.LatestRevision.DataType != "string" || len(detail.LatestRevision.CompiledQualityRules) == 0 {
			t.Fatalf("element content or compiled constraints lost: %#v", detail.LatestRevision)
		}
	})

	t.Run("code_set", func(t *testing.T) {
		f := newRevisionLifecycleFixture(t, db)
		svc := NewCodeSetService(repository.NewCodeSetRepository(f.tx), repository.NewTenantReferenceRepository(f.tx))
		created, err := svc.CreateCodeSet(f.tenantID, 1, &models.CreateCodeSetRequest{ScopeType: models.StandardScopeTenantCommon, Code: "lifecycle_codes", Name: "历史码值集", Description: "保留码值定义", ValueType: "string", EffectiveFrom: &from}, "初始创建")
		if err != nil {
			t.Fatal(err)
		}
		f.track(t, "code_sets", "code_set_revisions", created)
		item, err := svc.CreateCodeItem(created.ID, created.DraftRevision.ID, f.tenantID, &models.CreateCodeItemRequest{Version: created.Version, Code: "active", Label: "有效", Definition: "码值项定义", Status: models.CodeItemStatusActive})
		if err != nil {
			t.Fatal(err)
		}
		created, err = svc.GetCodeSet(created.ID, f.tenantID)
		if err != nil {
			t.Fatal(err)
		}
		withdrawn := runRevisionLifecycle(t, created, f.tenantID, svc.SubmitRevision, svc.PublishRevision, svc.WithdrawRevision)
		detail, err := svc.GetCodeSet(created.ID, f.tenantID)
		assertWithdrawnLifecycleRead(t, detail, err, withdrawn)
		list, total, err := svc.ListCodeSets(f.tenantID, repository.ListCodeSetOptions{Keyword: "历史码值集"})
		assertWithdrawnLifecycleList(t, list, total, err, withdrawn)
		list, total, err = svc.ListCodeSets(f.tenantID, repository.ListCodeSetOptions{Status: models.RevisionStatusWithdrawn})
		assertWithdrawnLifecycleList(t, list, total, err, withdrawn)
		items := detail.LatestRevision.Items
		if detail.LatestRevision.Description != "保留码值定义" || len(items) != 1 || items[0].ID != item.Item.ID || items[0].Label != "有效" || items[0].Definition != "码值项定义" {
			t.Fatalf("code set items lost: %#v", detail.LatestRevision)
		}
	})

	t.Run("metric", func(t *testing.T) {
		f := newRevisionLifecycleFixture(t, db)
		svc := NewMetricService(nil, repository.NewMetricRepository(f.tx), repository.NewTenantReferenceRepository(f.tx), nil)
		base, err := svc.CreateMetric(&models.CreateMetricRequest{ScopeType: models.StandardScopeTenantCommon, Code: "lifecycle_base", Name: "基准指标", Definition: "基准定义", StatisticalCaliber: "基准口径", MetricType: models.MetricTypeAtomic, EffectiveFrom: &from}, f.tenantID, 1, "初始创建")
		if err != nil {
			t.Fatal(err)
		}
		f.track(t, "metric_definitions", "metric_definition_revisions", base)
		base, err = svc.SubmitRevision(base.ID, base.DraftRevision.ID, f.tenantID, 1, base.Version)
		if err != nil {
			t.Fatal(err)
		}
		base, err = svc.PublishRevision(base.ID, base.DraftRevision.ID, f.tenantID, 1, base.Version)
		if err != nil {
			t.Fatal(err)
		}
		derivedFrom := from.Add(time.Minute)
		created, err := svc.CreateMetric(&models.CreateMetricRequest{ScopeType: models.StandardScopeTenantCommon, Code: "lifecycle_metric", Name: "历史派生指标", Definition: "保留指标定义", StatisticalCaliber: "保留统计口径", MetricType: models.MetricTypeDerived, EffectiveFrom: &derivedFrom, Dependencies: []models.MetricDefinitionDependencyInput{{MetricDefinitionID: base.ID, RelationKind: models.MetricDependencyBase, Note: "冻结基准依赖"}}}, f.tenantID, 1, "初始创建")
		if err != nil {
			t.Fatal(err)
		}
		f.track(t, "metric_definitions", "metric_definition_revisions", created)
		withdrawn := runRevisionLifecycle(t, created, f.tenantID, svc.SubmitRevision, svc.PublishRevision, svc.WithdrawRevision)
		detail, err := svc.GetMetric(created.ID, f.tenantID)
		assertWithdrawnLifecycleRead(t, detail, err, withdrawn)
		list, total, err := svc.ListMetrics(f.tenantID, repository.ListMetricOptions{Keyword: "历史派生指标"})
		assertWithdrawnLifecycleList(t, list, total, err, withdrawn)
		list, total, err = svc.ListMetrics(f.tenantID, repository.ListMetricOptions{Status: models.RevisionStatusWithdrawn})
		assertWithdrawnLifecycleList(t, list, total, err, withdrawn)
		deps := detail.LatestRevision.Dependencies
		if detail.LatestRevision.Definition != "保留指标定义" || detail.LatestRevision.StatisticalCaliber != "保留统计口径" || len(deps) != 1 || deps[0].DependencyDefinitionID != base.ID || deps[0].DependencyRevisionID == nil || *deps[0].DependencyRevisionID != base.CurrentRevision.ID {
			t.Fatalf("metric definition or frozen dependency lost: %#v", detail.LatestRevision)
		}
	})

	t.Run("document", func(t *testing.T) {
		f := newRevisionLifecycleFixture(t, db)
		store := &fakeDocumentObjectStore{objects: map[string][]byte{}}
		svc := &DocumentService{repo: repository.NewDocumentRepository(f.tx), refs: repository.NewTenantReferenceRepository(f.tx), objectStore: store, maxFileSize: 1024, timeout: time.Second}
		created, err := svc.CreateDocument(&models.CreateDocumentRequest{ScopeType: models.StandardScopeTenantCommon, Code: "lifecycle_document", DocType: "reference", Name: "历史标准文档", Description: "保留文档定义", EffectiveFrom: &from}, f.tenantID, 1, "初始创建")
		if err != nil {
			t.Fatal(err)
		}
		f.track(t, "documents", "document_revisions", created)
		content := []byte("# 标准文档\n\n撤回后仍保留文件内容。\n")
		created, err = svc.UploadFile(created.ID, created.DraftRevision.ID, f.tenantID, 1, created.Version, "history.md", bytes.NewReader(content), int64(len(content)), "text/markdown")
		if err != nil {
			t.Fatal(err)
		}
		withdrawn := runRevisionLifecycle(t, created, f.tenantID, svc.SubmitRevision, svc.PublishRevision, svc.WithdrawRevision)
		detail, err := svc.GetDocument(created.ID, f.tenantID)
		assertWithdrawnLifecycleRead(t, detail, err, withdrawn)
		list, total, err := svc.ListDocuments(f.tenantID, repository.ListDocumentOptions{Keyword: "历史标准文档"})
		assertWithdrawnLifecycleList(t, list, total, err, withdrawn)
		list, total, err = svc.ListDocuments(f.tenantID, repository.ListDocumentOptions{Status: models.RevisionStatusWithdrawn})
		assertWithdrawnLifecycleList(t, list, total, err, withdrawn)
		if detail.LatestRevision.Description != "保留文档定义" || detail.LatestRevision.FileName != "history.md" || detail.LatestRevision.ContentSHA256 != created.LatestRevision.ContentSHA256 {
			t.Fatalf("document file metadata lost: %#v", detail.LatestRevision)
		}
		reader, name, _, size, err := svc.DownloadFile(created.ID, detail.LatestRevision.ID, f.tenantID)
		if err != nil {
			t.Fatal(err)
		}
		actual, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil || name != "history.md" || size != int64(len(content)) || !bytes.Equal(actual, content) {
			t.Fatalf("withdrawn file changed: name=%q size=%d read=%v close=%v", name, size, readErr, closeErr)
		}
	})
}

type lifecycleRevisionSnapshot struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type lifecycleAggregateSnapshot struct {
	ID      int64                      `json:"id"`
	Version int64                      `json:"version"`
	Draft   *lifecycleRevisionSnapshot `json:"draft_revision"`
	Current *lifecycleRevisionSnapshot `json:"current_revision"`
	Latest  *lifecycleRevisionSnapshot `json:"latest_revision"`
}

func lifecycleSnapshot(t *testing.T, value any) lifecycleAggregateSnapshot {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var result lifecycleAggregateSnapshot
	if err := json.Unmarshal(payload, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func runRevisionLifecycle[T any](t *testing.T, initial *T, tenantID int64, steps ...func(int64, int64, int64, int64, int64) (*T, error)) *T {
	t.Helper()
	value := initial
	first := lifecycleSnapshot(t, initial)
	if first.Draft == nil || first.Draft.Status != models.RevisionStatusDraft {
		t.Fatalf("initial aggregate is not a draft: %#v", first)
	}
	for i, status := range []string{models.RevisionStatusInReview, models.RevisionStatusPublished, models.RevisionStatusWithdrawn} {
		before := lifecycleSnapshot(t, value)
		next, err := steps[i](first.ID, first.Draft.ID, tenantID, 1, before.Version)
		if err != nil {
			t.Fatalf("transition to %s: %v", status, err)
		}
		after := lifecycleSnapshot(t, next)
		if after.Version != before.Version+1 || after.Latest == nil || after.Latest.ID != first.Draft.ID || after.Latest.Name != first.Draft.Name || after.Latest.Status != status {
			t.Fatalf("transition to %s lost content or version: %#v", status, after)
		}
		if status == models.RevisionStatusPublished && (after.Current == nil || after.Current.ID != first.Draft.ID || after.Draft != nil) {
			t.Fatalf("published revision is not current: %#v", after)
		}
		if status == models.RevisionStatusWithdrawn && (after.Current != nil || after.Draft != nil) {
			t.Fatalf("withdrawn revision treated as effective or editable: %#v", after)
		}
		value = next
	}
	return value
}

func assertWithdrawnLifecycleRead(t *testing.T, value any, err error, expected any) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	got, want := lifecycleSnapshot(t, value), lifecycleSnapshot(t, expected)
	if got.ID != want.ID || got.Version != want.Version || got.Current != nil || got.Draft != nil || got.Latest == nil || *got.Latest != *want.Latest {
		t.Fatalf("withdrawn read lost content: got=%#v want=%#v", got, want)
	}
}

func assertWithdrawnLifecycleList[T any](t *testing.T, list []T, total int64, err error, expected any) {
	t.Helper()
	if err != nil || total != 1 || len(list) != 1 {
		t.Fatalf("withdrawn list: count=%d total=%d err=%v", len(list), total, err)
	}
	assertWithdrawnLifecycleRead(t, list[0], nil, expected)
}

type lifecycleOwnedRow struct {
	table, column string
	id            int64
}

type revisionLifecycleFixture struct {
	tx       *gorm.DB
	tenantID int64
	rows     []lifecycleOwnedRow
}

func newRevisionLifecycleFixture(t *testing.T, db *gorm.DB) *revisionLifecycleFixture {
	t.Helper()
	f := &revisionLifecycleFixture{tx: db.Begin(), tenantID: time.Now().UnixNano()}
	if f.tx.Error != nil {
		t.Fatal(f.tx.Error)
	}
	t.Cleanup(func() {
		if err := f.tx.Rollback().Error; err != nil {
			t.Errorf("rollback lifecycle fixtures: %v", err)
		}
		for _, row := range f.rows {
			var count int64
			if err := db.Table("standard."+row.table).Where(row.column+" = ?", row.id).Count(&count).Error; err != nil || count != 0 {
				t.Errorf("fixture residue in %s: count=%d err=%v", row.table, count, err)
			}
		}
		t.Logf("rollback checked %d fixture identity, revision and child row selectors", len(f.rows))
	})
	return f
}

func (f *revisionLifecycleFixture) track(t *testing.T, identityTable, revisionTable string, aggregate any) {
	t.Helper()
	state := lifecycleSnapshot(t, aggregate)
	if state.Draft == nil {
		t.Fatal("created fixture has no draft")
	}
	f.rows = append(f.rows, lifecycleOwnedRow{identityTable, "id", state.ID}, lifecycleOwnedRow{revisionTable, "id", state.Draft.ID})
	switch identityTable {
	case "code_sets":
		f.rows = append(f.rows, lifecycleOwnedRow{"code_set_revision_items", "code_set_revision_id", state.Draft.ID})
	case "metric_definitions":
		f.rows = append(f.rows, lifecycleOwnedRow{"metric_definition_revision_dependencies", "metric_definition_revision_id", state.Draft.ID})
	}
}
