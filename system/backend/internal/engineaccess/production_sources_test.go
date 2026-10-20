package engineaccess

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/dataitem/authorizationfacts/factstest"
	"github.com/addp/common/engine/plugin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestInitialProductionSourcesRejectUnknownAndPriorOperations(t *testing.T) {
	for _, scenario := range []string{"complete", "missing", "unproven_commit", "recreated", "reused", "retry", "later_attempt", "no_human", "unknown_sources", "empty_sources", "noncanonical_sources", "self_source", "wrong_target", "database_target", "missing_engine_version"} {
		t.Run(scenario, func(t *testing.T) {
			r := productionBindingReceipt(7, 2)
			r.SourceReadSet, _ = plugin.NewQueryReadSet(plugin.TabularItemPath(2, "schema", "public", "source"))
			prepared := &preparedTransferProduction{receipt: r, binding: factstest.Binding(r.Target), engineVersion: 1}
			switch scenario {
			case "missing":
				prepared = nil
			case "unproven_commit":
				prepared.receipt.CreatedAndCommitted = false
			case "recreated":
				prepared.receipt.Preparation = plugin.TableTargetRecreated
			case "reused":
				prepared.receipt.Preparation = plugin.TableTargetReused
			case "retry":
				id := uuid.NewString()
				prepared.receipt.RetryOfExecutionID = &id
			case "later_attempt":
				prepared.receipt.Attempt = 2
			case "no_human":
				prepared.receipt.UserOrigin = nil
			case "unknown_sources":
				prepared.receipt.SourceReadSet = nil
			case "empty_sources":
				prepared.receipt.SourceReadSet = &plugin.QueryReadSet{}
			case "noncanonical_sources":
				prepared.receipt.SourceReadSet.Paths = append(prepared.receipt.SourceReadSet.Paths, prepared.receipt.SourceReadSet.Paths[0])
			case "self_source":
				prepared.receipt.SourceReadSet, _ = plugin.NewQueryReadSet(r.Target)
			case "wrong_target":
				prepared.binding = factstest.Binding(plugin.TabularItemPath(2, "schema", "public", "other"))
			case "database_target":
				prepared.receipt.Target = plugin.TabularItemPath(2, "database", "public", "other")
				prepared.binding = factstest.Binding(prepared.receipt.Target)
			case "missing_engine_version":
				prepared.engineVersion = 0
			}
			request, err := initialProductionSources(prepared)
			if scenario != "complete" {
				if !errors.Is(err, errInitialProductionEvidence) || len(request.Targets) != 0 {
					t.Fatal("ineligible producer facts yielded a source request", request, err)
				}
				return
			}
			if err != nil || request.Source.PrincipalID != 31 || request.Source.MembershipID != 41 || request.Source.AuthorizationVersion != 5 || request.TenantID != 7 {
				t.Fatal("trusted source identity lost", request, err)
			}
			request.Targets[0].Segments[2].Name = "changed"
			if r.SourceReadSet.Paths[0].Segments[2].Name != "source" {
				t.Fatal("source request aliases the receipt")
			}
		})
	}
}

func TestInitialProductionObservationRequiresOwnerRepositoryAndPreparation(t *testing.T) {
	for _, repository := range []*Repository{nil, NewRepository(nil)} {
		if got, err := repository.observeInitialProduction(t.Context(), nil); got != nil || !errors.Is(err, errInitialProductionEvidence) {
			t.Fatal("missing owner state accepted", got, err)
		}
	}
}

func TestInitialProductionPreparesEveryPGSourceOutsideTransactions(t *testing.T) {
	for _, scenario := range []string{"complete", "missing_source", "incomplete_source", "unsupported_source", "engine_changed", "target_changed", "ambient_transaction"} {
		t.Run(scenario, func(t *testing.T) {
			db := productionBindingDB(t)
			if err := db.Exec("INSERT INTO system.engines VALUES (3,7,'postgresql',8,'active')").Error; err != nil {
				t.Fatal(err)
			}
			r := productionBindingReceipt(7, 2)
			a := plugin.TabularItemPath(2, "schema", "a.b", "source")
			b := plugin.TabularItemPath(3, "schema", "public", "source")
			r.SourceReadSet, _ = plugin.NewQueryReadSet(a, b)
			factsCalls := 0
			service := NewService(NewRepository(db), nil).
				WithTransferProductionReceiptReader(productionReceiptReaderFunc(func(context.Context, uint, string) (*client.TransferProductionReceipt, error) { return &r, nil })).
				WithAuthorizationFactsReader(bindingFactsReaderFunc(func(_ context.Context, tenant uint, fingerprint string) (*authorizationfacts.AuthorizationFacts, error) {
					factsCalls++
					if tenant != 7 {
						t.Fatal("wrong Meta tenant")
					}
					for _, path := range []plugin.EngineCatalogPath{r.Target, a, b} {
						facts := factstest.Single(path)
						if facts.ItemFingerprint != fingerprint {
							continue
						}
						if factsCalls > 1 {
							switch scenario {
							case "missing_source":
								return nil, &client.TenantAPIError{StatusCode: 404}
							case "incomplete_source":
								facts.BasisState, facts.Boundary, facts.BoundaryHash = authorizationfacts.AuthorizationBasisIncomplete, nil, nil
							case "engine_changed", "target_changed":
								id := 3
								if scenario == "target_changed" {
									id = 2
								}
								if err := db.Exec("UPDATE system.engines SET version=version+1 WHERE id=?", id).Error; err != nil {
									t.Fatal(err)
								}
							}
						}
						return &facts, nil
					}
					t.Fatal("unexpected fingerprint", fingerprint)
					return nil, nil
				}))
			if scenario == "unsupported_source" {
				if err := db.Exec("UPDATE system.engines SET engine_type='mysql' WHERE id=3").Error; err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "ambient_transaction" {
				if err := db.Transaction(func(tx *gorm.DB) error {
					service.repository = NewRepository(tx)
					if got, err := service.prepareInitialTransferProduction(t.Context(), 7, r.ExecutionID); got != nil || !errors.Is(err, errProductionReceiptUnavailable) {
						t.Fatal("cross-owner preparation ran inside transaction", got, err)
					}
					return nil
				}); err != nil || factsCalls != 0 {
					t.Fatal("ambient transaction contacted Meta", err, factsCalls)
				}
				return
			}
			got, err := service.prepareInitialTransferProduction(t.Context(), 7, r.ExecutionID)
			if scenario != "complete" {
				want := ErrAuthorizationBoundaryChanged
				if scenario == "unsupported_source" || strings.HasSuffix(scenario, "changed") {
					want = errProductionTargetChanged
				}
				if got != nil || !errors.Is(err, want) {
					t.Fatal("incomplete source preparation succeeded", got, err, want)
				}
				return
			}
			if err != nil || factsCalls != 3 || len(got.sources.Bindings) != 2 || got.engineVersions[2] != 1 || got.engineVersions[3] != 8 {
				t.Fatal("not all exact source boundaries and engines proven", got, factsCalls, err)
			}
			r.SourceReadSet.Paths[0].Segments[2].Name = "mutated"
			if got.sources.Targets[0].Segments[2].Name != "source" {
				t.Fatal("source preparation aliases producer data")
			}
		})
	}
}
