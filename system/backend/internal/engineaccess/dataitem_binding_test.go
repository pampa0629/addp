package engineaccess

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	commonauth "github.com/addp/common/authorization"
	"github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/dataitem/authorizationfacts/factstest"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/iam"
	"gorm.io/gorm"
)

// IAM fixtures must read the authority clock at each command, not extrapolate
// an old DB timestamp with the host's monotonic clock: the Docker VM wall clock
// can adjust during this long-running suite. Production validity is unchanged.
func testDatabaseClock(t *testing.T, db *gorm.DB) func() time.Time {
	t.Helper()
	return func() time.Time {
		now, err := NewRepository(db).wallClock(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		return now
	}
}

func testBindingFacts(path plugin.EngineCatalogPath) authorizationfacts.AuthorizationFacts {
	return factstest.Single(path)
}

type bindingFactsReaderFunc func(context.Context, uint, string) (*authorizationfacts.AuthorizationFacts, error)

func (f bindingFactsReaderFunc) ReadAuthorizationFacts(ctx context.Context, tenant uint, fp string) (*authorizationfacts.AuthorizationFacts, error) {
	return f(ctx, tenant, fp)
}

func TestDataItemReadBindingsRequireCompleteTrustedNativeProof(t *testing.T) {
	path := plugin.TabularItemPath(2, "schema", "a.b", "c")
	for _, scenario := range []string{"complete", "missing", "offline", "nil", "incomplete", "needs_refresh", "unsupported", "wrong_fingerprint", "dotted_collision"} {
		t.Run(scenario, func(t *testing.T) {
			facts := factstest.Single(path)
			var readErr error
			want := ErrAuthorizationBoundaryChanged
			switch scenario {
			case "complete":
				want = nil
			case "missing":
				readErr = &client.TenantAPIError{StatusCode: http.StatusNotFound}
			case "offline":
				readErr = errors.New("Meta offline")
				want = ErrAuthorizationFactsUnavailable
			case "nil":
				want = ErrAuthorizationFactsUnavailable
			case "incomplete", "needs_refresh", "unsupported":
				facts.BasisState = authorizationfacts.AuthorizationBasisState(scenario)
				facts.Boundary, facts.BoundaryHash = nil, nil
			case "wrong_fingerprint":
				facts.ItemFingerprint = strings.Repeat("a", 64)
				want = ErrAuthorizationFactsUnavailable
			case "dotted_collision":
				facts = factstest.Single(plugin.TabularItemPath(2, "schema", "a", "b.c"))
			}
			calls := 0
			service := NewService(nil, nil).WithAuthorizationFactsReader(bindingFactsReaderFunc(func(_ context.Context, tenant uint, fp string) (*authorizationfacts.AuthorizationFacts, error) {
				calls++
				if tenant != 7 || fp != factstest.Binding(path).ItemFingerprint {
					t.Fatal("wrong trusted Meta lookup scope")
				}
				if scenario == "nil" {
					return nil, nil
				}
				return &facts, readErr
			}))
			result, err := service.resolveReadBindings(context.Background(), 7, []plugin.EngineCatalogPath{path})
			if calls != 1 || (want == nil && (err != nil || len(result) != 1)) || (want != nil && (!errors.Is(err, want) || len(result) != 0)) {
				t.Fatalf("bindings=%+v error=%v want=%v calls=%d", result, err, want, calls)
			}
			if scenario == "complete" {
				if err := service.verifyApprovedBoundary(context.Background(), 7, path, facts.ItemFingerprint, *facts.BoundaryHash); err != nil {
					t.Fatal(err)
				}
				if err := service.verifyApprovedBoundary(context.Background(), 7, path, facts.ItemFingerprint, strings.Repeat("b", 64)); !errors.Is(err, ErrAuthorizationBoundaryChanged) {
					t.Fatalf("old digest accepted: %v", err)
				}
			}
		})
	}
}

func TestPGApprovedBindingSurvivesBasicObservationRefresh(t *testing.T) {
	path := plugin.TabularItemPath(2, "schema", "outdoor", "ods_outdoor_persons")
	facts := factstest.Single(path)
	facts.ObservedAt = time.Now().Add(-time.Minute)
	approved, err := facts.ApprovedBinding()
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(nil, nil).WithAuthorizationFactsReader(bindingFactsReaderFunc(func(_ context.Context, tenant uint, fingerprint string) (*authorizationfacts.AuthorizationFacts, error) {
		if tenant != 7 || fingerprint != approved.ItemFingerprint {
			t.Fatal("wrong PG DataItem lookup scope")
		}
		return &facts, nil
	}))
	check := func(want error) {
		t.Helper()
		err := service.verifyApprovedBoundary(t.Context(), 7, path, approved.ItemFingerprint, approved.BoundaryHash)
		if !errors.Is(err, want) {
			t.Fatalf("PG binding check: got %v, want %v", err, want)
		}
	}
	check(nil)
	// A new basic observation of the same target is not a new grant boundary.
	// No deep-scan facts participate in this contract.
	facts.ObservedAt = time.Now()
	check(nil)
	facts.BasisState = authorizationfacts.AuthorizationBasisNeedsRefresh
	facts.Boundary, facts.BoundaryHash = nil, nil
	check(ErrAuthorizationBoundaryChanged)
	// Once complete facts for the same boundary return, the original approved
	// binding remains usable; an unknown boundary never gets an ACL fallback.
	facts = factstest.Single(path)
	check(nil)
}

func testReadBindings(paths ...plugin.EngineCatalogPath) []authorizationfacts.Binding {
	result := make([]authorizationfacts.Binding, 0, len(paths))
	seen := map[string]bool{}
	for _, path := range paths {
		facts := testBindingFacts(path)
		binding, err := facts.ApprovedBinding()
		if err != nil {
			panic(err)
		}
		if !seen[binding.ItemFingerprint] {
			result = append(result, binding)
			seen[binding.ItemFingerprint] = true
		}
	}
	return result
}

type testMetaFacts struct{ paths []plugin.EngineCatalogPath }

func (reader testMetaFacts) ReadAuthorizationFacts(_ context.Context, _ uint, fingerprint string) (*authorizationfacts.AuthorizationFacts, error) {
	for _, path := range reader.paths {
		facts := testBindingFacts(path)
		if facts.ItemFingerprint == fingerprint {
			return &facts, nil
		}
	}
	return nil, ErrAuthorizationBoundaryChanged
}

func (r *Repository) testReadCurrentUserSourceRules(ctx context.Context, credential string, paths []plugin.EngineCatalogPath) (*sourceReadRules, error) {
	// This fixture's tenant is derived from a real trusted credential, never a
	// browser-supplied actor. Production calls require an explicit prepared set.
	if _, _, err := encodeSourceReadTargets(paths); err != nil {
		return nil, err
	}
	if credential == "" {
		return nil, commonapi.ErrUnauthorized
	}
	if r == nil || r.db == nil {
		return nil, errSourceReadRules
	}
	service, err := iam.NewAuthContextService(r.identity())
	if err != nil {
		return nil, err
	}
	auth, err := service.ResolveUserAccessToken(ctx, credential)
	if err != nil {
		return nil, err
	}
	if auth.Context.TenantID == nil {
		return nil, commonapi.ErrForbidden
	}
	tenant, err := strconv.ParseInt(*auth.Context.TenantID, 10, 64)
	if err != nil {
		return nil, err
	}
	return r.observeSourceRules(ctx, credential, paths, testReadBindings(paths...), tenant,
		func(auth *iam.AuthContextService) (*commonauth.AuthContext, error) {
			return auth.ResolveUserAccessToken(ctx, credential)
		}, nil)
}
