package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/standard/internal/models"
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
		`INSERT INTO standard.glossaries (id, tenant_id, scope_type, code, draft_revision_id, version, lifecycle_state, created_by) VALUES (31, 7, 'tenant_common', 'leader', 311, 1, 'active', 1)`,
		`INSERT INTO standard.glossary_revisions (id, glossary_id, revision_no, status, name, definition, change_summary, created_by) VALUES (311, 31, 1, 'draft', 'Leader', 'Activity leader', 'initial', 1)`,
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

func TestCreateGlossaryGeneratesInitialSummary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct{ language, summary string }{{"zh-CN", "初始创建"}, {"en-US", "Initial creation"}} {
		t.Run(tc.language, func(t *testing.T) {
			db := newGlossaryHandlerTestDB(t)
			handler := NewGlossaryHandler(service.NewGlossaryService(repository.NewGlossaryRepository(db), repository.NewTenantReferenceRepository(db)))
			router := gin.New()
			router.Use(commoni18n.I18nMiddleware())
			router.POST("/glossaries", withElementHandlerAuth(7), handler.CreateGlossary)
			request := httptest.NewRequest(http.MethodPost, "/glossaries", strings.NewReader(`{"scope_type":"tenant_common","code":"customer","name":"Customer","definition":"A buyer of products or services"}`))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Accept-Language", tc.language)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusCreated {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
			var aggregate models.GlossaryAggregate
			if err := json.Unmarshal(response.Body.Bytes(), &aggregate); err != nil {
				t.Fatal(err)
			}
			if aggregate.DraftRevision == nil || aggregate.DraftRevision.RevisionNo != 1 || aggregate.DraftRevision.ChangeSummary != tc.summary {
				t.Fatalf("initial revision = %#v", aggregate.DraftRevision)
			}
			var stored models.GlossaryRevision
			if err := db.First(&stored, aggregate.DraftRevision.ID).Error; err != nil {
				t.Fatal(err)
			}
			if stored.ChangeSummary != tc.summary {
				t.Fatalf("stored summary = %q, want %q", stored.ChangeSummary, tc.summary)
			}
		})
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
		`CREATE TABLE standard.element_revisions (id INTEGER PRIMARY KEY, element_id INTEGER NOT NULL, revision_no INTEGER NOT NULL, name TEXT NOT NULL DEFAULT '', status TEXT NOT NULL, effective_from DATETIME, effective_to DATETIME)`,
		`CREATE TABLE standard.glossaries (id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, scope_type TEXT NOT NULL, owner_domain_id INTEGER, code TEXT NOT NULL, tags TEXT, draft_revision_id INTEGER, created_by INTEGER NOT NULL, updated_by INTEGER, created_at DATETIME, updated_at DATETIME, version INTEGER NOT NULL DEFAULT 1, lifecycle_state TEXT NOT NULL)`,
		`CREATE TABLE standard.glossary_revisions (id INTEGER PRIMARY KEY AUTOINCREMENT, glossary_id INTEGER NOT NULL REFERENCES glossaries(id) ON DELETE CASCADE, revision_no INTEGER NOT NULL, status TEXT NOT NULL, name TEXT NOT NULL, alias TEXT, definition TEXT NOT NULL, example TEXT, note TEXT, related_ids TEXT, change_summary TEXT NOT NULL, effective_from DATETIME, effective_to DATETIME, submitted_by INTEGER, submitted_at DATETIME, published_by INTEGER, published_at DATETIME, created_by INTEGER NOT NULL, updated_by INTEGER, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE standard.glossary_element_mappings (glossary_id INTEGER NOT NULL, element_id INTEGER NOT NULL, PRIMARY KEY (glossary_id, element_id))`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestGlossaryMappingsKeepNonEffectiveIdentitiesOnRoundTrip(t *testing.T) {
	db := newGlossaryHandlerTestDB(t)
	for _, statement := range []string{
		`INSERT INTO standard.glossaries (id, tenant_id, scope_type, code, version, lifecycle_state, created_by) VALUES (31, 7, 'tenant_common', 'leader', 1, 'active', 1)`,
		`INSERT INTO standard.elements (id, tenant_id, code, lifecycle_state) VALUES (41, 7, 'draft', 'active'), (42, 7, 'withdrawn', 'active'), (43, 7, 'future', 'active'), (44, 7, 'expired', 'active'), (45, 7, 'effective', 'active'), (46, 8, 'other_tenant', 'active')`,
		`INSERT INTO standard.element_revisions (id, element_id, revision_no, name, status, effective_from, effective_to) VALUES
		 (411, 41, 1, '草稿数据元', 'draft', NULL, NULL),
		 (421, 42, 1, '已撤回数据元', 'withdrawn', '2020-01-01', NULL),
		 (431, 43, 1, '待生效数据元', 'published', '2099-01-01', NULL),
		 (441, 44, 1, '已过期数据元', 'published', '2020-01-01', '2021-01-01'),
		 (451, 45, 1, '生效数据元', 'published', '2020-01-01', NULL),
		 (452, 45, 2, '后续草稿', 'draft', NULL, NULL)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	handler := NewGlossaryHandler(service.NewGlossaryService(repository.NewGlossaryRepository(db), repository.NewTenantReferenceRepository(db)))
	router := gin.New()
	router.GET("/glossaries/:id/elements", withElementHandlerAuth(7), handler.GetElementMappings)
	router.PUT("/glossaries/:id/elements", withElementHandlerAuth(7), handler.UpdateElementMappings)
	put := func(version int, ids string, wantStatus int) {
		t.Helper()
		request := httptest.NewRequest(http.MethodPut, "/glossaries/31/elements", strings.NewReader(fmt.Sprintf(`{"version":%d,"element_ids":%s}`, version, ids)))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != wantStatus {
			t.Fatalf("PUT status=%d, want=%d: %s", response.Code, wantStatus, response.Body.String())
		}
	}
	read := func() []models.GlossaryElementReference {
		t.Helper()
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/glossaries/31/elements", nil))
		if response.Code != http.StatusOK {
			t.Fatalf("GET: %d %s", response.Code, response.Body.String())
		}
		var items []models.GlossaryElementReference
		if err := json.Unmarshal(response.Body.Bytes(), &items); err != nil {
			t.Fatal(err)
		}
		return items
	}
	if items := read(); items == nil || len(items) != 0 {
		t.Fatalf("empty response = %#v, want []", items)
	}
	put(1, `[41,42,43,44,45]`, http.StatusOK)
	items := read()
	if len(items) != 5 {
		t.Fatalf("mapped identities=%+v, want all five", items)
	}
	ids := make([]int64, len(items))
	for i, item := range items {
		ids[i] = item.ID
		if item.ID != int64(41+i) || item.IsEffective != (item.ID == 45) || item.RevisionID == nil || *item.RevisionID != item.ID*10+1 {
			t.Fatalf("unexpected mapping: %+v", item)
		}
	}
	payload, err := json.Marshal(ids)
	if err != nil {
		t.Fatal(err)
	}
	put(2, string(payload), http.StatusOK)
	put(2, `[]`, http.StatusConflict)
	put(3, `[46]`, http.StatusBadRequest)
	if len(read()) != 5 {
		t.Fatal("round trip, stale write or cross-tenant write lost associations")
	}
	put(3, `[41,43,44,45]`, http.StatusOK)
	if remaining := read(); len(remaining) != 4 || remaining[1].ID != 43 {
		t.Fatalf("explicit removal = %+v", remaining)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/glossaries/99/elements", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing glossary = %d", response.Code)
	}
}
