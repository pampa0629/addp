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
	return client.TransferProductionReceipt{SchemaVersion: client.TransferProductionReceiptSchema,
		TenantID: tenant, ExecutionID: uuid.NewString(), Attempt: 1,
		Target:      plugin.TabularItemPath(engine, "schema", "a.b", "c"),
		Preparation: plugin.TableTargetCreated, CommittedAt: time.Now().UTC(),
		UserOrigin: &client.TransferProductionUserOrigin{PrincipalID: 31, TenantMembershipID: 41, AuthorizationVersion: 5}}
}

func TestTransferProductionBindingUsesCompleteExactMetaProof(t *testing.T) {
	for _, scenario := range []string{"complete", "recreated", "reused", "retry", "no_human", "incomplete", "needs_refresh", "unsupported", "dotted_collision", "wrong_fingerprint", "nil_facts", "missing_facts", "offline_facts", "wrong_tenant", "wrong_execution", "invalid_receipt", "nil_receipt", "missing_receipt", "offline_receipt", "other_engine_type", "disabled_engine", "missing_engine", "engine_changed_during_meta", "engine_disabled_during_meta"} {
		t.Run(scenario, func(t *testing.T) {
			db := productionBindingDB(t)
			receipt := productionBindingReceipt(7, 2)
			receipt.SourceReadSet, _ = plugin.NewQueryReadSet(plugin.TabularItemPath(3, "schema", "outdoor", "source"))
			facts := factstest.Single(receipt.Target)
			var receiptErr, factsErr, want error
			wantReceiptCalls, wantFactsCalls := 1, 1
			switch scenario {
			case "recreated":
				receipt.Preparation = plugin.TableTargetRecreated
			case "reused":
				receipt.Preparation = plugin.TableTargetReused
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
				WithTransferProductionReceiptReader(productionReceiptReaderFunc(func(_ context.Context, tenant uint, id string) (*client.TransferProductionReceipt, error) {
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
			result, err := service.prepareTransferProduction(ctx, 7, executionID)
			if !errors.Is(err, want) || receiptCalls != wantReceiptCalls || factsCalls != wantFactsCalls {
				t.Fatalf("error=%v want=%v receipt/facts calls=%d/%d", err, want, receiptCalls, factsCalls)
			}
			if want != nil {
				if result != nil || strings.Contains(err.Error(), "secret") {
					t.Fatal("failed proof returned evidence or upstream detail")
				}
				return
			}
			if result == nil || result.binding.Validate() != nil || result.binding.ItemFingerprint != facts.ItemFingerprint || result.engineVersion != 1 || result.receipt.Preparation != receipt.Preparation || result.receipt.Attempt != receipt.Attempt || (result.receipt.UserOrigin == nil) != (receipt.UserOrigin == nil) {
				t.Fatalf("wrong transient evidence: %+v", result)
			}
			// Neither reader-owned references nor later fixture mutations may
			// enlarge the already resolved evidence. This is not cached permission.
			receipt.Target.Segments[2].Name = "other"
			receipt.SourceReadSet.Paths[0].Segments[2].Name = "changed"
			if result.receipt.SourceReadSet.Paths[0].Segments[2].Name != "source" {
				t.Fatal("actual source evidence aliases the producer response")
			}
			if receipt.UserOrigin != nil {
				receipt.UserOrigin.PrincipalID++
				if result.receipt.UserOrigin.PrincipalID != 31 {
					t.Fatal("human origin aliases the owner response")
				}
			}
			if receipt.RetryOfExecutionID != nil {
				*receipt.RetryOfExecutionID = uuid.NewString()
				if *result.receipt.RetryOfExecutionID == *receipt.RetryOfExecutionID {
					t.Fatal("retry origin aliases the owner response")
				}
			}
			if result.receipt.Target.Segments[2].Name != "c" || result.binding.NativeTarget.Segments[2].Name != "c" {
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
	service := NewService(NewRepository(db), nil).WithTransferProductionReceiptReader(reader)
	for _, id := range []string{"", uuid.Nil.String(), strings.ToUpper(uuid.NewString()), "../execution", "not-a-uuid"} {
		if _, err := service.prepareTransferProduction(t.Context(), 7, id); !errors.Is(err, commonapi.ErrBadRequest) {
			t.Fatalf("invalid execution accepted: %q %v", id, err)
		}
	}
	if _, err := service.prepareTransferProduction(t.Context(), 0, uuid.NewString()); !errors.Is(err, commonapi.ErrBadRequest) {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		_, err := NewService(NewRepository(tx), nil).WithTransferProductionReceiptReader(reader).prepareTransferProduction(t.Context(), 7, uuid.NewString())
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
		if _, err := service.prepareTransferProduction(t.Context(), 7, uuid.NewString()); !errors.Is(err, errProductionReceiptUnavailable) {
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
		checkRequest(r, "/api/v1/meta/runtime/data-items/"+facts.ItemFingerprint+"/authorization-facts")
		// Completes while the remote call is in flight. System cannot retain
		// a transaction/connection or lock on this Engine across owner IO.
		if changeEngine.Load() {
			if err := db.Exec("UPDATE system.engines SET version=version+1 WHERE id=?", engine).Error; err != nil {
				t.Error(err)
			}
		}
		_ = json.NewEncoder(w).Encode(facts)
	}))
	defer meta.Close()
	service := NewService(NewRepository(db), nil).
		WithTransferProductionReceiptReader(client.NewTransferClient(producer.URL, tokens)).
		WithAuthorizationFactsReader(client.NewMetaClient(meta.URL, tokens))
	if result, err := service.prepareTransferProduction(t.Context(), tenant, receipt.ExecutionID); result != nil || !errors.Is(err, errProductionTargetChanged) || producerCalls.Load() != 1 || metaCalls.Load() != 1 {
		t.Fatalf("changed Engine accepted: result=%+v error=%v calls=%d/%d", result, err, producerCalls.Load(), metaCalls.Load())
	}
	changeEngine.Store(false)
	if result, err := service.prepareTransferProduction(t.Context(), tenant, receipt.ExecutionID); err != nil || result == nil || result.binding.Validate() != nil || result.engineVersion != 2 || producerCalls.Load() != 2 || metaCalls.Load() != 2 {
		t.Fatalf("complete owner evidence not resolved: %+v %v calls=%d/%d", result, err, producerCalls.Load(), metaCalls.Load())
	}
}

func TestTransferProductionBindingThroughSharedClients(t *testing.T) {
	exerciseTransferProductionClients(t, productionBindingDB(t), 7, 2)
}
