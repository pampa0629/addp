package repository

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/infra/bootstrap"
	"github.com/addp/common/infra/database"
	"github.com/addp/standard/internal/models"
	"gorm.io/gorm"
)

func TestInstalledMetricDependencyTransactions(t *testing.T) {
	if os.Getenv("ADDP_TEST_STANDARD_DEPLOYMENT") != "1" {
		t.Skip("requires isolated Standard deployment")
	}
	db, err := bootstrap.OpenPlatformRuntime(database.Config{
		Host: os.Getenv("POSTGRES_HOST"), Port: os.Getenv("POSTGRES_PORT"), DBName: os.Getenv("POSTGRES_DB"), Schema: "standard",
	}, "standard", &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	db = db.WithContext(t.Context())
	const tenant, otherTenant, actor int64 = 51, 52, 9
	repo := NewMetricRepository(db)
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	create := func(owner int64, code, metricType string) (models.MetricDefinition, models.MetricDefinitionRevision) {
		t.Helper()
		id := models.MetricDefinition{TenantID: owner, Code: code, CreatedBy: actor}
		rev := models.MetricDefinitionRevision{
			MetricType: metricType, Name: code, Definition: "fixture", StatisticalCaliber: "fixture",
			ChangeSummary: "initial", CreatedBy: actor, EffectiveFrom: &from,
		}
		if err := repo.Create(&id, &rev, nil); err != nil {
			t.Fatal(err)
		}
		return id, rev
	}
	a, ar := create(tenant, "metric_dependency_a", "derived")
	b, br := create(tenant, "metric_dependency_b", "atomic")
	c, cr := create(otherTenant, "metric_dependency_c", "atomic")
	aToB := []models.MetricDefinitionRevisionDependency{{DependencyDefinitionID: b.ID, RelationKind: models.MetricDependencyBase}}
	bToA := []models.MetricDefinitionRevisionDependency{{DependencyDefinitionID: a.ID, RelationKind: models.MetricDependencyBase}}

	holder := db.Begin()
	if holder.Error != nil {
		t.Fatal(holder.Error)
	}
	defer holder.Rollback()
	if err := lockMetricDependencies(holder, tenant); err != nil {
		t.Fatal(err)
	}

	err = db.Connection(func(conn *gorm.DB) error {
		edit := func(tx *gorm.DB) error {
			r := NewMetricRepository(tx)
			if err := r.UpdateDraft(c.ID, cr.ID, otherTenant, actor, c.Version, &cr, nil); err != nil {
				t.Fatalf("a different tenant could not edit while the first tenant held the lock: %v", err)
			}
			return r.UpdateDraft(b.ID, br.ID, tenant, actor, b.Version, &br, bToA)
		}
		if db.Dialector.Name() == "postgres" {
			return conn.Transaction(func(tx *gorm.DB) error {
				if err := tx.Exec("SET LOCAL lock_timeout = '1s'").Error; err != nil {
					return err
				}
				return edit(tx)
			})
		}
		if err := conn.Exec("SET SESSION innodb_lock_wait_timeout = 1").Error; err != nil {
			return err
		}
		defer func() {
			if err := conn.Exec("SET SESSION innodb_lock_wait_timeout = DEFAULT").Error; err != nil {
				t.Errorf("restore test session: %v", err)
			}
		}()
		return edit(conn)
	})
	expectedTimeout := "SQLSTATE 55P03"
	if db.Dialector.Name() == "mysql" {
		expectedTimeout = "Error 1205"
	}
	if err == nil || !strings.Contains(err.Error(), expectedTimeout) {
		t.Fatalf("same-tenant draft edit returned %v while the tenant lock was held, expected %s", err, expectedTimeout)
	}

	if err := NewMetricRepository(holder).UpdateDraft(a.ID, ar.ID, tenant, actor, a.Version, &ar, aToB); err != nil {
		t.Fatal(err)
	}
	if err := holder.Commit().Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateDraft(b.ID, br.ID, tenant, actor, b.Version, &br, bToA); !errors.Is(err, ErrMetricDependencyCycle) {
		t.Fatalf("reverse dependency after the commit: %v, want dependency cycle", err)
	}
	var got []models.MetricDefinitionRevisionDependency
	if err := db.Where("metric_definition_revision_id IN ?", []int64{ar.ID, br.ID}).Find(&got).Error; err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].MetricDefinitionRevisionID != ar.ID || got[0].DependencyDefinitionID != b.ID {
		t.Fatalf("unexpected committed metric dependencies: %+v", got)
	}
	publish := func(id, revisionID, version int64) {
		t.Helper()
		if err := repo.TransitionRevision(id, revisionID, tenant, actor, version, models.RevisionStatusDraft, models.RevisionStatusInReview); err != nil {
			t.Fatal(err)
		}
		if err := repo.PublishRevision(id, revisionID, tenant, actor, version+1); err != nil {
			t.Fatal(err)
		}
	}
	publish(b.ID, br.ID, b.Version)
	publish(a.ID, ar.ID, a.Version+1)
	published, err := repo.GetRevision(a.ID, ar.ID, tenant)
	if err != nil {
		t.Fatal(err)
	}
	if published.Status != models.RevisionStatusPublished || len(published.Dependencies) != 1 ||
		published.Dependencies[0].DependencyRevisionID == nil || *published.Dependencies[0].DependencyRevisionID != br.ID {
		t.Fatalf("published metric did not freeze the dependency to revision %d: %+v", br.ID, published.Dependencies)
	}
	t.Log("tenant edits serialized, the reverse dependency was rejected, and publication froze the committed base revision")
}
