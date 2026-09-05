package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/standard/internal/repository"
	"github.com/addp/standard/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestListGlossariesByElementKeepsPaginatedResponseContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newGlossaryHandlerTestDB(t)
	for _, statement := range []string{
		`INSERT INTO standard.elements (id, tenant_id, code, lifecycle_state) VALUES (41, 7, 'activity_id', 'active')`,
		`INSERT INTO standard.element_revisions (id, element_id, revision_no, status, effective_from) VALUES (411, 41, 1, 'published', '2020-01-01 00:00:00')`,
		`INSERT INTO standard.glossaries (id, tenant_id, scope_type, code, draft_revision_id, version, lifecycle_state) VALUES (31, 7, 'tenant_common', 'leader', 311, 1, 'active')`,
		`INSERT INTO standard.glossary_revisions (id, glossary_id, revision_no, status, name, definition, change_summary) VALUES (311, 31, 1, 'draft', 'Leader', 'Activity leader', 'initial')`,
		`INSERT INTO standard.glossary_element_mappings (glossary_id, element_id) VALUES (31, 41)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("prepare glossary fixture: %v", err)
		}
	}

	handler := NewGlossaryHandler(service.NewGlossaryService(repository.NewGlossaryRepository(db), repository.NewTenantReferenceRepository(db)))
	router := gin.New()
	router.GET("/glossaries", withElementHandlerAuth(7), handler.ListGlossaries)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/glossaries?element_id=41", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if body := response.Body.String(); !strings.Contains(body, `"total":1`) || !strings.Contains(body, `"data":[`) || !strings.Contains(body, `"code":"leader"`) {
		t.Fatalf("response = %s, want paginated glossary aggregate", body)
	}
}

func newGlossaryHandlerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS standard").Error; err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE standard.elements (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, code TEXT NOT NULL, lifecycle_state TEXT NOT NULL)`,
		`CREATE TABLE standard.element_revisions (id INTEGER PRIMARY KEY, element_id INTEGER NOT NULL, revision_no INTEGER NOT NULL, status TEXT NOT NULL, effective_from DATETIME, effective_to DATETIME)`,
		`CREATE TABLE standard.glossaries (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, scope_type TEXT NOT NULL, owner_domain_id INTEGER, code TEXT NOT NULL, draft_revision_id INTEGER, version INTEGER NOT NULL, lifecycle_state TEXT NOT NULL, created_at DATETIME)`,
		`CREATE TABLE standard.glossary_revisions (id INTEGER PRIMARY KEY, glossary_id INTEGER NOT NULL, revision_no INTEGER NOT NULL, status TEXT NOT NULL, name TEXT NOT NULL, definition TEXT NOT NULL, change_summary TEXT NOT NULL, effective_from DATETIME, effective_to DATETIME)`,
		`CREATE TABLE standard.glossary_element_mappings (glossary_id INTEGER NOT NULL, element_id INTEGER NOT NULL, PRIMARY KEY (glossary_id, element_id))`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db
}
