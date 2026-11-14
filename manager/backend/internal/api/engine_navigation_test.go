package api

import (
	"context"
	"errors"
	"github.com/addp/common/authorization"
	"github.com/addp/common/models"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"testing"
	"time"
)

type navigationCheckFunc func(context.Context, string, authorization.SourceNavigationCheckRequest) (*authorization.SourceMetadataCheck, error)

func (f navigationCheckFunc) CheckManagerNavigation(ctx context.Context, token string, q authorization.SourceNavigationCheckRequest) (*authorization.SourceMetadataCheck, error) {
	return f(ctx, token, q)
}
func TestEngineListFiltersCurrentUserBeforeDeliveringNames(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/engines", nil)
	c.Request.Header.Set("Authorization", "Bearer addp_at_user")
	engines := []*models.Engine{{ID: 1, EngineType: "postgresql"}, {ID: 2, EngineType: "nfs"}, {ID: 3, EngineType: "minio"}, {ID: 4, EngineType: "mysql"}}
	mode := 0
	calls := 0
	h := &ExplorerHandler{navigationChecker: navigationCheckFunc(func(ctx context.Context, token string, q authorization.SourceNavigationCheckRequest) (*authorization.SourceMetadataCheck, error) {
		calls++
		if token != "addp_at_user" || !q.Valid() || len(q.Targets) != 3 {
			t.Fatal("wrong identity or candidate batch")
		}
		if mode == 3 {
			return nil, errors.New("dependency unavailable")
		}
		r := &authorization.SourceMetadataCheck{ObservedAt: time.Now()}
		for _, path := range q.Targets {
			r.Targets = append(r.Targets, authorization.SourceMetadataTargetCheck{Target: path, Visible: mode == 1 || mode == 2 && path.EngineID == 2})
		}
		return r, nil
	})}
	for _, step := range []struct {
		mode int
		ids  []uint
	}{{0, []uint{4}}, {1, []uint{1, 2, 3, 4}}, {2, []uint{2, 4}}, {0, []uint{4}}} {
		mode = step.mode
		got, err := h.filterVisibleEngines(c, engines)
		if err != nil || len(got) != len(step.ids) {
			t.Fatal(got, err)
		}
		for i, e := range got {
			if e.ID != step.ids[i] {
				t.Fatal("hidden engine disclosed")
			}
		}
	}
	mode = 3
	if got, err := h.filterVisibleEngines(c, engines); err == nil || got != nil {
		t.Fatal("dependency failure disclosed engines")
	}
	if calls != 5 {
		t.Fatal("stale engine visibility reused")
	}
}
