package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/addp/common/client"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	_ "github.com/addp/common/engine/plugins/builtin/general"
	"github.com/addp/common/engine/plugins/postgresql"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/transfer/internal/protection"
)

func TestFieldRecommendationTargetUsesDeclaredDecimalLimits(t *testing.T) {
	for _, engineType := range []string{"mysql", "oceanbase"} {
		t.Run(engineType, func(t *testing.T) {
			plug, err := plugin.Get(engineType)
			if err != nil {
				t.Fatal(err)
			}
			limits := plug.Capabilities().Limits.TableWrite.Decimal
			if !fitsDecimalFieldLimits(65, 30, limits) {
				t.Fatalf("%s limits should accept decimal(65,30)", engineType)
			}
			if fitsDecimalFieldLimits(66, 30, limits) || fitsDecimalFieldLimits(65, 31, limits) {
				t.Fatalf("%s limits should reject values beyond decimal(65,30)", engineType)
			}
		})
	}
}

type allowFieldRecommendationProtectionGate struct{}

func (allowFieldRecommendationProtectionGate) RequireLocator(context.Context, uint, string) error {
	return nil
}

// Use the production PostgreSQL catalog model and a deterministic source row stream.
type recommendationSource struct {
	postgresql.PostgreSQLPlugin
	opens    atomic.Int32
	finished atomic.Bool
}

func (*recommendationSource) Type() string { return "recommendation-test-source" }
func (s *recommendationSource) OpenTableReadSession(_ context.Context, info plugin.ConnectionInfo, path plugin.EngineCatalogPath, _ plugin.TableReadSessionOptions) (plugin.TableReadSession, error) {
	if !reflect.DeepEqual(path, plugin.TabularItemPath(8, "schema", "public", "amounts")) {
		return nil, fmt.Errorf("unexpected source path: %v", path)
	}
	if info["password"] != "scan-only-fixture" {
		return nil, fmt.Errorf("dedicated connection missing")
	}
	s.opens.Add(1)
	return &recommendationRows{source: s}, nil
}

type recommendationRows struct {
	done   bool
	source *recommendationSource
}

func (r *recommendationRows) ReadBatch(context.Context, int) (*plugin.BatchData, error) {
	if r.done {
		r.source.finished.Store(true)
		return &plugin.BatchData{}, nil
	}
	r.done = true
	return &plugin.BatchData{Fields: []datatype.FieldInfo{{Name: "amount", Type: datatype.FieldTypeDecimal}},
		Rows: []map[string]interface{}{{"amount": "123.45"}, {"amount": nil}, {"amount": "0.001"}}}, nil
}
func (*recommendationRows) Close(context.Context) error { return nil }

type recommendationTokenSource struct{ executionTestTokenSource }

func (recommendationTokenSource) Token(_ context.Context, tenant uint) (string, error) {
	return fmt.Sprintf("tenant-%d", tenant), nil
}

func TestFieldRecommendationIssuesConnectionOnlyForAuthorizedScan(t *testing.T) {
	const descriptorPath = "/api/v1/system/runtime/engine-descriptors/9"
	const checkPath = "/api/v1/system/engine-access/read-checks/transfer-field-recommendation"
	const itemPath = "/api/v1/meta/items/60"
	const sourcePath = "/api/v1/system/runtime/engine-descriptors/8"
	const openPath = "/api/v1/system/engine-access/connections/transfer-field-recommendation"
	source := &recommendationSource{}
	plugin.Register(source)
	t.Cleanup(func() { plugin.Unregister(source.Type()) })
	for _, tc := range []struct {
		name             string
		engineType       string
		connectionStatus string
		status           int
		wantErr          error
		checks           []int
		itemMismatch     string
	}{
		{"available", "mysql", "online", 200, nil, []int{200, 200}, ""},
		{"other tenant", "", "", 403, ErrFieldRecommendationUnavailable, nil, ""},
		{"offline", "mysql", "offline", 200, ErrFieldRecommendationUnavailable, nil, ""},
		{"no decimal limit", "postgresql", "online", 200, ErrFieldRecommendationUnsupported, nil, ""},
		{"missing read", "mysql", "online", 200, client.ErrManagerPreviewReadDenied, []int{403}, ""},
		{"revoked at release", "mysql", "online", 200, client.ErrManagerPreviewReadDenied, []int{200, 403}, ""},
		{"source Grant expired during scan", "mysql", "online", 200, client.ErrManagerPreviewReadDenied, []int{200, 403}, ""},
		{"expired user", "mysql", "online", 200, client.ErrManagerPreviewCredentialRejected, []int{200, 401}, ""},
		{"unavailable at release", "mysql", "online", 200, client.ErrManagerPreviewReadUnavailable, []int{200, 503}, ""},
		{"item does not match native leaf", "mysql", "online", 200, client.ErrManagerPreviewReadDenied, nil, "name"},
		{"wrong scanned fingerprint", "mysql", "online", 200, client.ErrManagerPreviewReadDenied, nil, "fingerprint"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var target commonmodels.EngineRuntimeDescriptor
			if tc.status == http.StatusOK {
				plug, err := plugin.Get(tc.engineType)
				if err != nil {
					t.Fatal(err)
				}
				data, err := plugin.MarshalEngineCapabilities(plug.Capabilities())
				if err != nil {
					t.Fatal(err)
				}
				caps := commonmodels.JSONString(data)
				target = commonmodels.EngineRuntimeDescriptor{
					ID: 9, EngineType: tc.engineType, LifecycleState: "active",
					ConnectionStatus: tc.connectionStatus, Capabilities: &caps,
				}
			}
			var paths []string
			checks := 0
			issued := 0
			source.opens.Store(0)
			source.finished.Store(false)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.URL.Path)
				if r.URL.Path == openPath || r.URL.Path == checkPath {
					opening := r.URL.Path == openPath
					if checks >= len(tc.checks) || opening != (checks == 0) || r.Method != "POST" {
						t.Error("unexpected source authorization request")
						w.WriteHeader(400)
						return
					}
					targetPath := plugin.TabularItemPath(8, "schema", "public", "amounts")
					if opening {
						var body client.TransferFieldConnectionRequest
						if json.NewDecoder(r.Body).Decode(&body) != nil || r.Header.Get("Authorization") != "Bearer tenant-7" || body.UserCredential != "addp_at_current" || body.EngineVersion != 3 || !reflect.DeepEqual(body.Target, targetPath) {
							t.Error("wrong user, service, version or table at open")
							w.WriteHeader(403)
							return
						}
					} else {
						var body struct {
							Targets []plugin.EngineCatalogPath `json:"targets"`
						}
						if json.NewDecoder(r.Body).Decode(&body) != nil || r.Header.Get("Authorization") != "Bearer addp_at_current" || !reflect.DeepEqual(body.Targets, []plugin.EngineCatalogPath{targetPath}) {
							t.Error("wrong release check")
							w.WriteHeader(403)
							return
						}
					}
					if (checks == 1) != source.finished.Load() {
						t.Error("source check performed at the wrong phase")
					}
					status := tc.checks[checks]
					checks++
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(status)
					if status == 200 && opening {
						issued++
						_ = json.NewEncoder(w).Encode(client.TransferFieldConnection{ConnectionInfo: commonmodels.ConnectionInfo{"password": "scan-only-fixture"}})
					} else if status == 200 {
						_, _ = w.Write([]byte(`{"observed_at":"2026-11-24T00:00:00Z"}`))
					}
					return
				}
				if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer tenant-7" {
					t.Error("unexpected System request method or tenant identity")
					w.WriteHeader(403)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case descriptorPath:
					if tc.status != 200 {
						w.WriteHeader(tc.status)
						return
					}
					_ = json.NewEncoder(w).Encode(target)
				case itemPath:
					name := "amounts"
					fingerprint := commonmodels.GenerateItemFingerprint(8, "public.amounts")
					if tc.itemMismatch == "fingerprint" {
						fingerprint = commonmodels.GenerateItemFingerprint(8, "public.other")
					}
					if tc.itemMismatch == "name" {
						name = "other"
					}
					_ = json.NewEncoder(w).Encode(commonmodels.MetaItem{ID: 60, TenantID: 7, EngineID: 8, Name: name, FullName: "public.amounts", Fingerprint: fingerprint, ItemType: "table"})
				case sourcePath:
					_ = json.NewEncoder(w).Encode(commonmodels.EngineRuntimeDescriptor{ID: 8, Version: 3, EngineType: source.Type(), LifecycleState: "active", ConnectionStatus: "online"})
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			tokens := recommendationTokenSource{}

			descriptor := protection.EngineDescriptorLookup(client.NewSystemServiceClient(server.URL, tokens, server.Client()))
			svc := NewFieldDefinitionRecommendationService(descriptor, client.NewMetaClient(server.URL, tokens), client.NewSystemServiceClient(server.URL, tokens, server.Client()), allowFieldRecommendationProtectionGate{})
			got, err := svc.Recommend(context.Background(), 7, "addp_at_current", FieldDefinitionRecommendationRequest{SourceLocator: "addp://engine/8/path/public/amounts?type=table&item_id=60", SourceFields: []string{"amount"}, TargetEngineID: 9})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("recommendation error = %v, want %v", err, tc.wantErr)
			}
			wantPaths := []string{descriptorPath}
			if len(tc.checks) > 0 || tc.itemMismatch != "" {
				wantPaths = append(wantPaths, sourcePath, itemPath)
			}
			for i := range tc.checks {
				if i > 0 {
					wantPaths = append(wantPaths, itemPath)
				}
				if i == 0 {
					wantPaths = append(wantPaths, openPath)
				} else {
					wantPaths = append(wantPaths, checkPath)
				}
			}
			wantOpens := int32(0)
			if len(tc.checks) > 0 && tc.checks[0] == 200 {
				wantOpens = 1
			}
			if int32(issued) != wantOpens || source.opens.Load() != wantOpens || (tc.wantErr != nil && got != nil) {
				t.Fatalf("opens=%d, want %d; result=%+v", source.opens.Load(), wantOpens, got)
			}
			if tc.wantErr == nil {
				want := &FieldDefinitionRecommendationResult{TargetEngineType: tc.engineType, Basis: "exact_source_values", RowsScanned: 3,
					Fields: []DecimalFieldRecommendation{{SourceField: "amount", Precision: 6, Scale: 3, NonNullCount: 2, FitsTarget: true}}}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("recommendation = %+v, want %+v", got, want)
				}
			}
			if !reflect.DeepEqual(paths, wantPaths) {
				t.Fatalf("System requests = %v, want %v", paths, wantPaths)
			}
		})
	}
}

func TestDecimalValueShapePreservesExactRequiredDigits(t *testing.T) {
	tests := []struct {
		value         interface{}
		integerDigits int
		scale         int
	}{
		{"43854577.120000", 8, 2},
		{"0.00120", 0, 4},
		{"-12.3400", 2, 2},
		{"1.2e3", 4, 0},
		{"1.2e-3", 0, 4},
		{"0", 1, 0},
	}
	for _, test := range tests {
		integerDigits, scale, err := decimalValueShape(test.value)
		if err != nil {
			t.Fatalf("decimalValueShape(%v): %v", test.value, err)
		}
		if integerDigits != test.integerDigits || scale != test.scale {
			t.Fatalf("decimalValueShape(%v) = (%d,%d), want (%d,%d)", test.value, integerDigits, scale, test.integerDigits, test.scale)
		}
	}
}

func TestDecimalRecommendationCombinesColumnWideBounds(t *testing.T) {
	accumulator := decimalRecommendationAccumulator{}
	for _, value := range []interface{}{"99999", "0.123456", nil} {
		if err := accumulator.Add(value); err != nil {
			t.Fatal(err)
		}
	}
	precision, scale := accumulator.Recommendation()
	if precision != 11 || scale != 6 || accumulator.NonNullCount != 2 {
		t.Fatalf("recommendation = decimal(%d,%d), non-null=%d", precision, scale, accumulator.NonNullCount)
	}
}

func TestRecommendationFieldsPreserveQuotedIdentifierCase(t *testing.T) {
	fields, err := normalizedRecommendationFields([]string{"Amount", "amount", "Amount"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Amount", "amount"}; !reflect.DeepEqual(fields, want) {
		t.Fatalf("normalized fields = %#v, want %#v", fields, want)
	}

	actual := []datatype.FieldInfo{
		{Name: "Amount", Type: datatype.FieldTypeDecimal},
		{Name: "amount", Type: datatype.FieldTypeString},
	}
	if err := validateDecimalRecommendationFields(actual, []string{"Amount"}); err != nil {
		t.Fatalf("exact decimal field should be accepted: %v", err)
	}
	if err := validateDecimalRecommendationFields(actual, []string{"amount"}); err == nil {
		t.Fatal("case-distinct non-decimal field should be rejected")
	}
}
