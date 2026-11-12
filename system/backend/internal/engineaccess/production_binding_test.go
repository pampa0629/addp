package engineaccess

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/dataitem/authorizationfacts/factstest"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/production"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type productionReceiptReaderFunc func(context.Context, uint, string) (*client.TransferProductionReceipt, error)

func (f productionReceiptReaderFunc) ReadProductionReceipt(ctx context.Context, tenant uint, execution string) (*client.TransferProductionReceipt, error) {
	return f(ctx, tenant, execution)
}

func productionBindingDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	for _, statement := range []string{
		"ATTACH DATABASE ':memory:' AS system",
		"CREATE TABLE system.engines (id INTEGER PRIMARY KEY, tenant_id INTEGER, engine_type TEXT, version INTEGER, lifecycle_state TEXT)",
		"INSERT INTO system.engines VALUES (2,7,'postgresql',1,'active')",
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func productionBindingReceipt(tenant int64, engine uint) client.TransferProductionReceipt {
	return client.TransferProductionReceipt{TargetEngineVersion: 1, SchemaVersion: client.TransferProductionReceiptSchema,
		TenantID: tenant, ExecutionID: uuid.NewString(), Attempt: 1,
		Target:      plugin.TabularItemPath(engine, "schema", "a.b", "c"),
		Preparation: plugin.TableTargetCreated, CommittedAt: time.Now().UTC(),
		CreatedAndCommitted: true,
		UserOrigin:          &client.TransferProductionUserOrigin{PrincipalID: 31, TenantMembershipID: 41, AuthorizationVersion: 5}}
}

func TestTransferProductionBindingUsesCompleteExactMetaProof(t *testing.T) {
	for _, scenario := range []string{"complete", "newer_logical_snapshot", "recreated", "reused", "retry", "no_human", "incomplete", "needs_refresh", "unsupported", "dotted_collision", "wrong_fingerprint", "nil_facts", "missing_facts", "offline_facts", "wrong_tenant", "wrong_execution", "invalid_receipt", "nil_receipt", "missing_receipt", "offline_receipt", "other_engine_type", "disabled_engine", "stale_write_version", "missing_engine", "engine_changed_during_meta", "engine_disabled_during_meta"} {
		t.Run(scenario, func(t *testing.T) {
			db := productionBindingDB(t)
			receipt := productionBindingReceipt(7, 2)
			receipt.SourceReadSet, _ = plugin.NewQueryReadSet(plugin.TabularItemPath(3, "schema", "outdoor", "source"))
			facts := factstest.Single(receipt.Target)
			var receiptErr, factsErr, want error
			wantReceiptCalls, wantFactsCalls := 1, 1
			switch scenario {
			case "stale_write_version":
				receipt.TargetEngineVersion = 2
				want, wantFactsCalls = errProductionTargetChanged, 0
			case "newer_logical_snapshot":
				// A later observation of the same logical boundary must not
				// rewrite the producer's successful commit time or demand a
				// physical-instance identity absent from the DataItem contract.
				receipt.CommittedAt = facts.ObservedAt.Add(-time.Hour)
			case "recreated":
				receipt.Preparation = plugin.TableTargetRecreated
				receipt.CreatedAndCommitted = false
			case "reused":
				receipt.Preparation = plugin.TableTargetReused
				receipt.CreatedAndCommitted = false
			case "retry":
				id := uuid.NewString()
				receipt.Attempt, receipt.RetryOfExecutionID = 2, &id
			case "no_human":
				receipt.UserOrigin = nil
			case "incomplete", "needs_refresh", "unsupported":
				facts.BasisState = authorizationfacts.AuthorizationBasisState(scenario)
				facts.Boundary, facts.BoundaryHash = nil, nil
				want = ErrAuthorizationBoundaryChanged
			case "dotted_collision":
				facts = factstest.Single(plugin.TabularItemPath(2, "schema", "a", "b.c"))
				want = ErrAuthorizationBoundaryChanged
			case "wrong_fingerprint":
				facts.ItemFingerprint = strings.Repeat("a", 64)
				want = ErrAuthorizationFactsUnavailable
			case "nil_facts":
				want = ErrAuthorizationFactsUnavailable
			case "missing_facts":
				factsErr, want = &client.TenantAPIError{StatusCode: 404}, ErrAuthorizationBoundaryChanged
			case "offline_facts":
				factsErr, want = errors.New("secret upstream failure"), ErrAuthorizationFactsUnavailable
			case "wrong_tenant", "wrong_execution", "invalid_receipt", "nil_receipt", "missing_receipt", "offline_receipt":
				want, wantFactsCalls = errProductionReceiptUnavailable, 0
				if scenario == "missing_receipt" {
					receiptErr, want = &client.TenantAPIError{StatusCode: 404}, commonapi.ErrNotFound
				}
				if scenario == "offline_receipt" {
					receiptErr = errors.New("secret upstream failure")
				}
			case "other_engine_type", "disabled_engine", "missing_engine":
				want, wantFactsCalls = errProductionTargetChanged, 0
				statement := "UPDATE system.engines SET engine_type='mysql' WHERE id=2"
				if scenario == "disabled_engine" {
					statement = "UPDATE system.engines SET lifecycle_state='disabled' WHERE id=2"
				}
				if scenario == "missing_engine" {
					statement, want = "UPDATE system.engines SET tenant_id=8 WHERE id=2", commonapi.ErrNotFound
				}
				if err := db.Exec(statement).Error; err != nil {
					t.Fatal(err)
				}
			case "engine_changed_during_meta", "engine_disabled_during_meta":
				want = errProductionTargetChanged
			}
			executionID := receipt.ExecutionID
			receiptCalls, factsCalls := 0, 0
			service := NewService(NewRepository(db), nil).
				withTestProductionReader(productionReceiptReaderFunc(func(_ context.Context, tenant uint, id string) (*client.TransferProductionReceipt, error) {
					receiptCalls++
					if tenant != 7 || id != executionID {
						t.Fatal("wrong producer scope")
					}
					copy := receipt
					switch scenario {
					case "wrong_tenant":
						copy.TenantID++
					case "wrong_execution":
						copy.ExecutionID = uuid.NewString()
					case "invalid_receipt":
						copy.SchemaVersion = "unexpected"
					case "nil_receipt":
						return nil, nil
					}
					return &copy, receiptErr
				})).WithAuthorizationFactsReader(bindingFactsReaderFunc(func(ctx context.Context, tenant uint, fingerprint string) (*authorizationfacts.AuthorizationFacts, error) {
				factsCalls++
				if tenant != 7 || fingerprint != factstest.Binding(receipt.Target).ItemFingerprint {
					t.Fatal("wrong Meta scope")
				}
				if scenario == "engine_changed_during_meta" || scenario == "engine_disabled_during_meta" {
					statement := "UPDATE system.engines SET version=version+1 WHERE id=2"
					if scenario == "engine_disabled_during_meta" {
						statement = "UPDATE system.engines SET lifecycle_state='disabled' WHERE id=2"
					}
					if err := db.WithContext(ctx).Exec(statement).Error; err != nil {
						t.Fatal(err)
					}
				}
				if scenario == "nil_facts" {
					return nil, nil
				}
				return &facts, factsErr
			}))
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			result, err := service.prepareProduction(ctx, 7, transferProductionRef(executionID))
			if !errors.Is(err, want) || receiptCalls != wantReceiptCalls || factsCalls != wantFactsCalls {
				t.Fatalf("error=%v want=%v receipt/facts calls=%d/%d", err, want, receiptCalls, factsCalls)
			}
			if want != nil {
				if result != nil || strings.Contains(err.Error(), "secret") {
					t.Fatal("failed proof returned evidence or upstream detail")
				}
				return
			}
			if result == nil || result.binding.Validate() != nil || result.binding.ItemFingerprint != facts.ItemFingerprint || result.engineVersion != 1 || result.receipt.Objects[0].Created != receipt.CreatedAndCommitted || !result.receipt.CommittedAt.Equal(receipt.CommittedAt) || result.receipt.Attempt != receipt.Attempt || (result.receipt.Origin == nil) != (receipt.UserOrigin == nil) {
				t.Fatalf("wrong transient evidence: %+v", result)
			}
			// Neither reader-owned references nor later fixture mutations may
			// enlarge the already resolved evidence. This is not cached permission.
			receipt.Target.Segments[2].Name = "other"
			receipt.SourceReadSet.Paths[0].Segments[2].Name = "changed"
			if result.receipt.Sources.ReadSet.Paths[0].Segments[2].Name != "source" {
				t.Fatal("actual source evidence aliases the producer response")
			}
			if receipt.UserOrigin != nil {
				receipt.UserOrigin.PrincipalID++
				if result.receipt.Origin.PrincipalID != 31 {
					t.Fatal("human origin aliases the owner response")
				}
			}
			if receipt.RetryOfExecutionID != nil {
				*receipt.RetryOfExecutionID = uuid.NewString()
				if *result.receipt.RetryOfExecutionID == *receipt.RetryOfExecutionID {
					t.Fatal("retry origin aliases the owner response")
				}
			}
			if result.receipt.Primary.Segments[2].Name != "c" || result.binding.NativeTarget.Segments[2].Name != "c" {
				t.Fatal("target aliases the owner response")
			}
		})
	}
}

func TestTransferProductionBindingRefusesInvalidInputsAndAmbientTransaction(t *testing.T) {
	db := productionBindingDB(t)
	calls := 0
	reader := productionReceiptReaderFunc(func(context.Context, uint, string) (*client.TransferProductionReceipt, error) {
		calls++
		return nil, errors.New("must not read remote owner")
	})
	service := NewService(NewRepository(db), nil).withTestProductionReader(reader)
	for _, id := range []string{"", uuid.Nil.String(), strings.ToUpper(uuid.NewString()), "../execution", "not-a-uuid"} {
		if _, err := service.prepareProduction(t.Context(), 7, transferProductionRef(id)); !errors.Is(err, commonapi.ErrBadRequest) {
			t.Fatalf("invalid execution accepted: %q %v", id, err)
		}
	}
	if _, err := service.prepareProduction(t.Context(), 0, transferProductionRef(uuid.NewString())); !errors.Is(err, commonapi.ErrBadRequest) {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		_, err := NewService(NewRepository(tx), nil).withTestProductionReader(reader).prepareProduction(t.Context(), 7, transferProductionRef(uuid.NewString()))
		if !errors.Is(err, errProductionReceiptUnavailable) {
			t.Fatal("ambient transaction accepted", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal("invalid invocation contacted producer")
	}
	for _, service := range []*Service{nil, NewService(nil, nil), NewService(NewRepository(db), nil)} {
		if _, err := service.prepareProduction(t.Context(), 7, transferProductionRef(uuid.NewString())); !errors.Is(err, errProductionReceiptUnavailable) {
			t.Fatal("missing dependency accepted", err)
		}
	}
}

// Same owner-contract exercise runs against SQLite T1 and the existing real
// PostgreSQL engineaccess fixture. HTTP owners are controlled, so this is NOT T4.
func exerciseTransferProductionClients(t *testing.T, db *gorm.DB, tenant int64, engine uint) {
	t.Helper()
	receipt := productionBindingReceipt(tenant, engine)
	facts := factstest.Single(receipt.Target)
	var producerCalls, metaCalls atomic.Int32
	var changeEngine atomic.Bool
	changeEngine.Store(true)
	tokens := client.ServiceTokenProviderFunc(func(ctx context.Context, requested uint) (string, error) {
		if ctx.Err() != nil || requested != uint(tenant) {
			t.Error("wrong System credential context")
		}
		return "test-system-service", nil
	})
	checkRequest := func(r *http.Request, path string) {
		if r.Method != "GET" || r.URL.Path != path || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "Bearer test-system-service" || r.Header.Get("X-Tenant-ID") != "" || r.ContentLength > 0 {
			t.Error("wrong System machine request")
		}
	}
	producer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		producerCalls.Add(1)
		checkRequest(r, "/api/v1/transfer/runtime/executions/"+receipt.ExecutionID+"/production-receipt")
		_ = json.NewEncoder(w).Encode(receipt)
	}))
	defer producer.Close()
	meta := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		metaCalls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/meta/runtime/data-items/authorization-facts/query" || r.Header.Get("Authorization") != "Bearer test-system-service" {
			t.Error("wrong System fact query")
		}
		// Completes while the remote call is in flight. System cannot retain
		// a transaction/connection or lock on this Engine across owner IO.
		if changeEngine.Load() {
			if err := db.Exec("UPDATE system.engines SET version=version+1 WHERE id=?", engine).Error; err != nil {
				t.Error(err)
			}
		}
		_ = json.NewEncoder(w).Encode(authorizationfacts.AuthorizationFactsBatch{Data: []authorizationfacts.AuthorizationFactsResult{{ItemFingerprint: facts.ItemFingerprint, Facts: &facts}}})
	}))
	defer meta.Close()
	service := NewService(NewRepository(db), nil).
		withTestProductionReader(client.NewTransferClient(producer.URL, tokens)).
		WithAuthorizationFactsReader(client.NewMetaClient(meta.URL, tokens))
	if result, err := service.prepareProduction(t.Context(), tenant, transferProductionRef(receipt.ExecutionID)); result != nil || !errors.Is(err, errProductionTargetChanged) || producerCalls.Load() != 1 || metaCalls.Load() != 1 {
		t.Fatalf("changed Engine accepted: result=%+v error=%v calls=%d/%d", result, err, producerCalls.Load(), metaCalls.Load())
	}
	changeEngine.Store(false)
	receipt.TargetEngineVersion = 2
	if result, err := service.prepareProduction(t.Context(), tenant, transferProductionRef(receipt.ExecutionID)); err != nil || result == nil || result.binding.Validate() != nil || result.engineVersion != 2 || producerCalls.Load() != 2 || metaCalls.Load() != 2 {
		t.Fatalf("complete owner evidence not resolved: %+v %v calls=%d/%d", result, err, producerCalls.Load(), metaCalls.Load())
	}
}

func TestTransferProductionBindingThroughSharedClients(t *testing.T) {
	exerciseTransferProductionClients(t, productionBindingDB(t), 7, 2)
}

func transferProductionRef(id string) production.Reference {
	return production.Reference{Owner: "transfer", ExecutionID: id, ResultID: id}
}
func (s *Service) withTestProductionReader(reader interface {
	ReadProductionReceipt(context.Context, uint, string) (*client.TransferProductionReceipt, error)
}) *Service {
	if reader == nil {
		return s.WithProductionRegistry(nil)
	}
	var last production.Receipt
	registry, err := production.NewRegistry(production.Owner{Module: "transfer", ClientID: "addp-transfer", ReceiptPermission: "transfer.production_receipt.read", ExecutionPermission: "transfer.task.execute",
		ReadReceipt: func(ctx context.Context, tenant uint, ref production.Reference) (*production.Receipt, error) {
			if ref.ResultID != ref.ExecutionID {
				return nil, production.ErrEvidence
			}
			native, err := reader.ReadProductionReceipt(ctx, tenant, ref.ExecutionID)
			if err != nil {
				return nil, err
			}
			if native == nil || native.Validate() != nil {
				return nil, production.ErrEvidence
			}
			receipt := native.ProductionEvidence()
			last = receipt
			return &receipt, nil
		}})
	if err != nil {
		panic(err)
	}
	s.WithProductionIdentificationReader(productionIdentificationFunc(func(_ context.Context, tenant uint, ref production.Reference) (*client.MetaProductionIdentification, error) {
		binding := factstest.Binding(last.Primary)
		return &client.MetaProductionIdentification{Reference: ref, TenantID: int64(tenant), EngineVersion: last.TargetEngineVersion, ScanExecutionID: uuid.NewString(), Status: "success", Binding: &binding, LogicalItemState: "new"}, nil
	}))
	return s.WithProductionRegistry(registry)
}

type productionIdentificationFunc func(context.Context, uint, production.Reference) (*client.MetaProductionIdentification, error)

func (f productionIdentificationFunc) ReadProductionIdentification(ctx context.Context, tenant uint, ref production.Reference) (*client.MetaProductionIdentification, error) {
	return f(ctx, tenant, ref)
}

func TestProductionRequiresMetaNewLogicalItemAndCurrentBoundary(t *testing.T) {
	for _, scenario := range []string{"new", "existing", "pending", "missing_newness", "foreign_result", "foreign_tenant", "engine_changed", "different_boundary"} {
		t.Run(scenario, func(t *testing.T) {
			db := productionBindingDB(t)
			native := productionBindingReceipt(7, 2)
			facts := factstest.Single(native.Target)
			binding := factstest.Binding(native.Target)
			svc := NewService(NewRepository(db), nil).withTestProductionReader(productionReceiptReaderFunc(func(context.Context, uint, string) (*client.TransferProductionReceipt, error) { return &native, nil })).WithAuthorizationFactsReader(bindingFactsReaderFunc(func(context.Context, uint, string) (*authorizationfacts.AuthorizationFacts, error) {
				return &facts, nil
			}))
			svc.WithProductionIdentificationReader(productionIdentificationFunc(func(_ context.Context, tenant uint, ref production.Reference) (*client.MetaProductionIdentification, error) {
				result := &client.MetaProductionIdentification{Reference: ref, TenantID: int64(tenant), ScanExecutionID: uuid.NewString(), Status: "success", EngineVersion: 1, Binding: &binding, LogicalItemState: "new"}
				switch scenario {
				case "existing":
					result.LogicalItemState = "existing"
				case "pending":
					result.Status = "running"
					result.Binding = nil
					result.LogicalItemState = ""
				case "missing_newness":
					result.LogicalItemState = ""
				case "foreign_result":
					result.Reference.ResultID = uuid.NewString()
				case "foreign_tenant":
					result.TenantID++
				case "engine_changed":
					result.EngineVersion++
				case "different_boundary":
					b := factstest.Binding(plugin.TabularItemPath(2, "schema", "public", "other"))
					result.Binding = &b
				}
				return result, nil
			}))
			got, err := svc.prepareProduction(t.Context(), 7, transferProductionRef(native.ExecutionID))
			if scenario == "new" {
				if err != nil || got == nil {
					t.Fatal(got, err)
				}
				return
			}
			want := errProductionReceiptUnavailable
			switch scenario {
			case "existing":
				want = errInitialProductionEvidence
			case "engine_changed":
				want = errProductionTargetChanged
			case "different_boundary":
				want = ErrAuthorizationBoundaryChanged
			}
			if got != nil || !errors.Is(err, want) {
				t.Fatalf("%s result=%+v err=%v want=%v", scenario, got, err, want)
			}
		})
	}
}
