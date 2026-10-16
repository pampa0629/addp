package api

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/preview"
	managerprotection "github.com/addp/manager/internal/protection"
	"github.com/gin-gonic/gin"
)

func TestTrajectoryQueryIsBoundedAndCannotCombineResourceSelections(t *testing.T) {
	gin.SetMode(gin.TestMode)
	raw := `{"device_field":"device","time_field":"at","time_encoding":"iso","devices":["A"],"start":"2026-10-16T01:00:00Z","end":"2026-10-16T03:00:00Z"}`
	base := "/preview?locator=example&page=1&page_size=2000&trajectory_selection=" + url.QueryEscape(raw)
	for _, suffix := range []string{"", "&child_name=", "&ref_path=x", "&graph_sample_kind=node_shape", "&key_name=x", "&page=2", "&trajectory_selection=" + url.QueryEscape(raw)} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", base+suffix, nil)
		selection, err := trajectorySelectionFromQuery(c)
		if suffix == "" {
			if err != nil || selection.DeviceField != "device" {
				t.Fatal(selection, err)
			}
		} else if !errors.Is(err, preview.ErrTrajectorySelection) {
			t.Fatal("conflicting range query accepted")
		}
	}
}

func TestTrajectoryPredicatesUseEffectiveFieldProtectionWithoutRawCountOracle(t *testing.T) {
	selection := &models.TrajectorySelection{DeviceField: "device", TimeField: "at"}
	subject := dataprotection.SubjectReference{Type: "user", ID: "7"}
	rule := dataprotection.Rule{Action: "preview", Component: dataprotection.Component{Path: []dataprotection.PathSegment{{Name: "device", Container: "scalar"}}}, Decision: dataprotection.Decision{Effect: dataprotection.EffectMask}}
	if err := checkTrajectoryProtection(selection, []dataprotection.Rule{rule}, subject, nil); !errors.Is(err, managerprotection.ErrRequired) {
		t.Fatal("masked predicate accepted", err)
	}
	rule.Authorizations = []dataprotection.TemporaryAuthorization{{Subject: subject, Effect: dataprotection.EffectAllow, ValidFrom: time.Now().Add(-time.Minute), ValidUntil: time.Now().Add(time.Minute)}}
	if err := checkTrajectoryProtection(selection, []dataprotection.Rule{rule}, subject, nil); err != nil {
		t.Fatal("active exact grant rejected", err)
	}
	if err := checkTrajectoryProtection(selection, []dataprotection.Rule{rule}, dataprotection.SubjectReference{Type: "user", ID: "8"}, nil); !errors.Is(err, managerprotection.ErrRequired) {
		t.Fatal("another subject inherited grant", err)
	}
	rule.Authorizations[0].ValidUntil = time.Now().Add(-time.Second)
	if err := checkTrajectoryProtection(selection, []dataprotection.Rule{rule}, subject, nil); !errors.Is(err, managerprotection.ErrRequired) {
		t.Fatal("expired grant accepted", err)
	}
	rule.Component.Path[0].Name = "other"
	if err := checkTrajectoryProtection(selection, []dataprotection.Rule{rule}, subject, nil); err != nil {
		t.Fatal("unrelated output field blocked predicate", err)
	}
}

func TestTrajectoryFailuresExposeStableCodeWithoutPartialData(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{{preview.ErrTrajectorySelection, 400, "trajectory_selection_invalid"}, {preview.ErrTrajectoryPoints, 422, "trajectory_point_limit_exceeded"}, {preview.ErrTrajectoryBudget, 422, "trajectory_scan_budget_exceeded"}, {preview.ErrTrajectorySource, 422, "trajectory_source_invalid"}} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/preview", nil)
		if !writeTrajectoryError(c, tc.err) || w.Code != tc.status {
			t.Fatal(w.Code, w.Body.String())
		}
		var body map[string]any
		if json.Unmarshal(w.Body.Bytes(), &body) != nil || body["error_code"] != tc.code || body["data"] != nil {
			t.Fatal("unstable trajectory error", body)
		}
	}
}

func TestTrajectoryOutputExpiryHidesRowsAndMatchingCountsIncludingEmptyRanges(t *testing.T) {
	for _, rows := range [][]map[string]interface{}{{}, {{"device": "A", "at": "2026-10-16T00:00:00Z"}}} {
		table := &models.TablePreview{Mode: "table", Columns: []string{"device", "at"}, Rows: rows, TrajectoryScope: &models.TrajectoryScope{Complete: true, ScannedRows: 88}}
		result := &preview.PreviewResult{PreviewType: "table", Data: table}
		checks := 0
		check := func() error {
			checks++
			if checks == 2 {
				return managerprotection.ErrRequired
			}
			return nil
		}
		if err := bindTrajectoryOutputProtection(result, check, nil, dataprotection.SubjectReference{}); err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/preview", nil)
		writePreviewResult(c, result, nil, dataprotection.SubjectReference{}, nil)
		var body map[string]any
		if w.Code != 403 || json.Unmarshal(w.Body.Bytes(), &body) != nil || body["data"] != nil || body["trajectory_scope"] != nil || checks != 2 {
			t.Fatalf("expired range leaked output: %d %s", w.Code, w.Body.String())
		}
	}
}
