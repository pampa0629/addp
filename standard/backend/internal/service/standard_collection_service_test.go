package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	commonclient "github.com/addp/common/client"
	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type collectionTestTokenSource struct{}

func (collectionTestTokenSource) Token(context.Context, uint) (string, error) {
	return "tenant-token", nil
}
func (collectionTestTokenSource) PlatformToken(context.Context) (string, error) {
	return "platform-token", nil
}

func TestStandardCollectionServiceEnforcesAssignmentsAndSeparationOfDuties(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS standard").Error; err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`CREATE TABLE standard.standard_collections (id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, code TEXT NOT NULL, draft_revision_id INTEGER, created_by INTEGER NOT NULL, updated_by INTEGER, created_at DATETIME, updated_at DATETIME, version INTEGER NOT NULL DEFAULT 1, UNIQUE(tenant_id, code))`,
		`CREATE TABLE standard.standard_collection_revisions (id INTEGER PRIMARY KEY AUTOINCREMENT, collection_id INTEGER NOT NULL, revision_no INTEGER NOT NULL, status TEXT NOT NULL, name TEXT NOT NULL, description TEXT NOT NULL, change_summary TEXT NOT NULL, submitted_by INTEGER, submitted_at DATETIME, published_by INTEGER, published_at DATETIME, created_by INTEGER NOT NULL, updated_by INTEGER, created_at DATETIME, updated_at DATETIME, UNIQUE(collection_id, revision_no))`,
		`CREATE TABLE standard.standard_collection_members (id INTEGER PRIMARY KEY AUTOINCREMENT, collection_revision_id INTEGER NOT NULL, member_type TEXT NOT NULL, member_id INTEGER NOT NULL, created_by INTEGER NOT NULL, created_at DATETIME, UNIQUE(collection_revision_id, member_type, member_id))`,
		`CREATE TABLE standard.standard_collection_assignments (id INTEGER PRIMARY KEY AUTOINCREMENT, collection_id INTEGER NOT NULL, principal_id INTEGER NOT NULL, role TEXT NOT NULL, created_by INTEGER NOT NULL, created_at DATETIME, UNIQUE(collection_id, principal_id, role))`,
		`CREATE TABLE standard.standard_collection_events (id INTEGER PRIMARY KEY AUTOINCREMENT, collection_id INTEGER NOT NULL, revision_id INTEGER, event_type TEXT NOT NULL, actor_id INTEGER NOT NULL, detail TEXT NOT NULL, created_at DATETIME)`,
		`CREATE TABLE standard.elements (id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, code TEXT NOT NULL, lifecycle_state TEXT NOT NULL)`,
		`CREATE TABLE standard.element_revisions (id INTEGER PRIMARY KEY AUTOINCREMENT, element_id INTEGER NOT NULL, revision_no INTEGER NOT NULL, status TEXT NOT NULL, name TEXT NOT NULL)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec(`INSERT INTO standard.elements (id, tenant_id, code, lifecycle_state) VALUES (31, 7, 'person_id', 'active')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO standard.element_revisions (id, element_id, revision_no, status, name) VALUES (311, 31, 1, 'draft', 'Person ID')`).Error; err != nil {
		t.Fatal(err)
	}

	directory := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			var request struct {
				References []struct {
					ID string `json:"id"`
				} `json:"references"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			results := make([]map[string]any, 0, len(request.References))
			for _, reference := range request.References {
				name := "Owner"
				code := "owner"
				if reference.ID == "12" {
					name, code = "Reviewer", "reviewer"
				}
				results = append(results, map[string]any{"subject_type": "user", "id": reference.ID, "found": true, "referenceable": true, "name": name, "code": code, "status": "active"})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"results": results})
			return
		}
		_, _ = w.Write([]byte(`{"data":[],"total":0,"page":1,"page_size":20,"total_pages":0}`))
	}))
	defer directory.Close()
	systemClient := commonclient.NewSystemServiceClient(directory.URL, collectionTestTokenSource{}, directory.Client())
	repo := repository.NewStandardCollectionRepository(db)
	svc := NewStandardCollectionService(repo, repository.NewTenantReferenceRepository(db), systemClient)

	created, err := svc.Create(context.Background(), 7, 11, &models.CreateStandardCollectionRequest{
		Code: "core", Name: "Core", Description: "Core standards", ChangeSummary: "initial",
		Members: []models.StandardCollectionMemberInput{{MemberType: models.CollectionMemberElement, MemberID: 31}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.Submit(context.Background(), created.ID, created.DraftRevision.ID, 7, 11, created.Version); !errors.Is(err, ErrStandardCollectionReviewerRequired) {
		t.Fatalf("submit without distinct reviewer err=%v", err)
	}
	assigned, err := svc.ReplaceAssignments(context.Background(), created.ID, 7, 11, &models.ReplaceStandardCollectionAssignmentsRequest{
		Version:     created.Version,
		Assignments: []models.StandardCollectionAssignmentInput{{PrincipalID: 11, Role: models.CollectionAssignmentOwner}, {PrincipalID: 12, Role: models.CollectionAssignmentReviewer}},
	})
	if err != nil {
		t.Fatalf("assign: %v", err)
	}
	submitted, err := svc.Submit(context.Background(), created.ID, created.DraftRevision.ID, 7, 11, assigned.Version)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := svc.Publish(context.Background(), created.ID, created.DraftRevision.ID, 7, 11, submitted.Version); !errors.Is(err, ErrStandardCollectionAccessDenied) {
		t.Fatalf("non-reviewer publish err=%v", err)
	}
	published, err := svc.Publish(context.Background(), created.ID, created.DraftRevision.ID, 7, 12, submitted.Version)
	if err != nil {
		t.Fatalf("reviewer publish: %v", err)
	}
	if published.CurrentRevision == nil || published.CurrentRevision.Status != models.RevisionStatusPublished {
		t.Fatalf("published=%#v", published)
	}
}
