package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/common/authorization/authtest"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/execution"
	"github.com/addp/common/exportartifact"
	"github.com/addp/common/format"
	_ "github.com/addp/common/format/builtin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/google/uuid"
)

type fileGDBExportBindFunc func(context.Context, uint, *commonClient.IssuedFileGDBExportAuthorization) error

func (f fileGDBExportBindFunc) BindFileGDBExportAuthorization(ctx context.Context, tenant uint, issued *commonClient.IssuedFileGDBExportAuthorization) error {
	return f(ctx, tenant, issued)
}

func TestPrepareFileGDBExportIssuesAsCurrentUserAndBindsExactSource(t *testing.T) {
	for _, engineType := range []string{"postgresql", "mysql"} {
		t.Run(engineType, func(t *testing.T) {
			const locator = "addp://engine/5/path/business/roads?type=table&item_id=6"
			loc, err := resourcetree.ParseURI(locator)
			if err != nil {
				t.Fatal(err)
			}
			engine := &commonModels.Engine{ID: 5, EngineType: engineType, Version: 8}
			req := &ExportRequest{TenantID: 7, UserID: 31, UserCredential: "addp_at_filegdb_user"}
			create := exportartifact.CreateRequest{ExecutionConfig: buildTableExportExecutionConfig(
				locator, "addp-infra://object/manager/tenant_7/export/manager/session?type=prefix", "roads.gdb.zip", format.FormatFileGDB)}
			executionID := uuid.NewString()
			var scope *execution.FileGDBExportReadScope
			system := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var got commonClient.IssueManagerFileAuthorizationRequest
				if r.URL.Path != "/api/v1/system/auth/execution-authorizations/filegdb-export" ||
					r.Header.Get("Authorization") != "Bearer "+req.UserCredential ||
					json.NewDecoder(r.Body).Decode(&got) != nil || got.ExecutionID != executionID {
					t.Error("System did not receive the current user and exact pending execution")
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(commonClient.IssuedFileGDBExportAuthorization{
					ID: "17", ExecutionID: executionID, TenantID: "7", Audience: execution.AudienceTransfer,
					SourceReadScope: *scope, ExpiresAt: time.Now().Add(time.Hour),
				})
			}))
			defer system.Close()
			var bound bool
			svc := &ExportService{}
			svc.SetFileGDBAuthorization(commonClient.NewSystemExecutionAuthorizationClient(system.URL, system.Client()),
				fileGDBExportBindFunc(func(_ context.Context, tenant uint, issued *commonClient.IssuedFileGDBExportAuthorization) error {
					if !issued.Matches(tenant, executionID, scope) {
						t.Fatal("Transfer did not receive the exact issued authorization")
					}
					bound = true
					return nil
				}))
			if err := svc.prepareFileGDBExport(t.Context(), req, loc, engine, &create); !errors.Is(err, commonAPI.ErrForbidden) {
				t.Fatalf("request without verified user context was accepted: %v", err)
			}
			ctx := execution.WithUserOrigin(t.Context(), authtest.NewTenantUserAuthContext("7", "31", nil))
			if err := svc.prepareFileGDBExport(ctx, req, loc, engine, &create); err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(create.ExecutionConfig)
			if err != nil {
				t.Fatal(err)
			}
			scope, err = execution.NewFileGDBExportReadScope(raw)
			if err != nil || scope.FileGDBSource.EngineType() != engineType || create.FileGDBActor.PrincipalID != 31 {
				t.Fatalf("exact actor or source was not frozen: %v", err)
			}
			if err := create.AdmitFileGDB(ctx, executionID, create.ExecutionConfig); err != nil || !bound {
				t.Fatalf("authorization was not bound: %v", err)
			}
		})
	}
}

func TestBuildTableExportTaskConfigUsesTransferSyncShape(t *testing.T) {
	config := buildTableExportExecutionConfig(
		"addp://engine/8/path/public/roads?type=table&item_id=54",
		"addp-infra://object/manager/tenant_7/export/20260620/session-1?type=prefix",
		"roads.geojson",
		format.FormatGeoJSON,
	)
	if config.Runtime.Boundary != "bounded" {
		t.Fatalf("runtime = %#v, want bounded", config.Runtime)
	}
	if config.Load.Mode != "snapshot" {
		t.Fatalf("load = %#v, want snapshot", config.Load)
	}

	source := config.Source
	if source.Locator != "addp://engine/8/path/public/roads?type=table&item_id=54" ||
		source.Representation != "native" || source.DataType != "table" {
		t.Fatalf("source config = %#v", source)
	}
	target := config.Target
	if target.ParentLocator != "addp-infra://object/manager/tenant_7/export/20260620/session-1?type=prefix" ||
		target.Name != "roads.geojson" || target.Representation != "encoded" || target.Format != "geojson" {
		t.Fatalf("target config = %#v", target)
	}
	policy := target.Policy
	if _, ok := policy["write_mode"]; ok {
		t.Fatalf("policy = %#v, must not contain legacy write_mode", policy)
	}
	if policy["apply_mode"] != "replace" {
		t.Fatalf("policy = %#v, want replace", policy)
	}
}

func TestBuildEncodedRecordExportTaskConfigUsesNativeCollectionShape(t *testing.T) {
	config := buildEncodedRecordExportExecutionConfig(
		"addp://engine/11/path/Outdoor/Persons?type=collection&item_id=81",
		"addp-infra://object/manager/tenant_7/export/20260902/session-1?type=prefix",
		"Persons.ejsonl",
		format.FormatMongoDBExtendedJSONL,
	)
	source := config.Source
	if source.Representation != "native" || source.DataType != "unknown" || source.Format != "" {
		t.Fatalf("source = %#v", source)
	}
	target := config.Target
	if target.Representation != "encoded" || target.Format != "mongodb_extended_jsonl" || target.Name != "Persons.ejsonl" {
		t.Fatalf("target = %#v", target)
	}
}

func TestSupportedExportFormatIncludesSingleAndMultiRefWriters(t *testing.T) {
	if !supportedExportFormat(format.FormatCSV) {
		t.Fatal("csv should be supported")
	}
	if !supportedExportFormat(format.FormatShapefile) {
		t.Fatal("shapefile multi-ref export should be supported")
	}
}

func TestKMLContainerWriterIsExposedForTableExport(t *testing.T) {
	if !supportedExportFormat(format.FormatKML) {
		t.Fatal("KML single-file writer missing")
	}
	found := false
	for _, f := range tableExportFormats() {
		if f == "kml" {
			found = true
		}
		if f == "gml" || f == "topojson" {
			t.Fatalf("read-only format advertised: %s", f)
		}
	}
	if !found {
		t.Fatal("container descriptor hid the KML writer")
	}
	config := buildTableExportExecutionConfig("addp://engine/1/path/public/roads?type=table", "", "roads.kml", format.FormatKML)
	if config.Target.Format != "kml" || config.Target.Representation != "encoded" {
		t.Fatal(config.Target)
	}
}
