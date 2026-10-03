package iam

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	systemauthorization "github.com/addp/system/internal/authorization"
	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/testsupport"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestCredentialValidationEvidenceAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ADDP_SYSTEM_POSTGRES_TEST_DSN to a disposable PostgreSQL database")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`DROP SCHEMA IF EXISTS system CASCADE`).Error; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatal(err)
	}
	repository := NewRepository(db)
	var databaseNow time.Time
	if err := db.Raw("SELECT clock_timestamp()").Scan(&databaseNow).Error; err != nil {
		t.Fatal(err)
	}
	now := databaseNow.UTC().Add(-time.Second).Truncate(time.Microsecond)
	clock := func() time.Time { return now }
	tokens, err := NewTokenFamilyService(repository, BrowserSessionConfig{ResourceTicketOwners: []string{"manager"}}, nil, testDatabaseTime(clock))
	if err != nil {
		t.Fatal(err)
	}
	selection, err := NewContextSelectionService(repository, tokens)
	if err != nil {
		t.Fatal(err)
	}
	fixture := issueResourceTicketInvalidationFixture(t, ctx, NewIdentityService(repository, testDatabaseTime(clock)),
		NewTenantMembershipService(repository, clock), selection, db, now, "original-evidence")
	original, err := repository.GetAccessTokenAuthSnapshot(ctx, hashOpaqueToken(fixture.Session.AccessToken))
	if err != nil {
		t.Fatal(err)
	}
	engineID := insertEngineFixture(t, db, fixture.TenantID, "Evidence Engine", "postgresql", map[string]interface{}{
		"host": "evidence-engine", "port": 5432, "database": "evidence",
	}, false)
	// A transaction's time stays fixed while the wall clock advances. Seed one
	// new disposable credential; do not rewrite immutable facts or either clock.
	var sourceToken string
	var future time.Time
	var credentialExpiry, familyExpiry time.Time
	var saved []*credentialValidationDiagnostic
	err = repository.Transaction(ctx, func(tx *Repository) error {
		if err := tx.db.Raw(`SELECT clock_timestamp()+INTERVAL '100 milliseconds'`).Scan(&future).Error; err != nil {
			return err
		}
		now = future
		principal, err := tx.LockPrincipal(ctx, fixture.PrincipalID)
		if err != nil {
			return err
		}
		session, err := tokens.createBrowserSessionTx(ctx, tx, browserSessionIssueInput{
			Principal:      principal,
			Context:        ResolvedSessionContext{Type: ContextTypeTenant, TenantID: &fixture.TenantID, TenantMembershipID: &fixture.TenantMembershipID},
			Authentication: SessionAuthentication{Methods: []string{"password"}, AssuranceLevel: AssuranceLevelAAL1, AuthenticatedAt: original.FamilyAuthenticatedAt},
		})
		if err != nil {
			return err
		}
		sourceToken, credentialExpiry, familyExpiry = session.AccessToken, session.AccessTokenExpiresAt, session.RefreshTokenFamilyExpiresAt
		delegation, err := NewDelegationService(tx, systemauthorization.ToolAuthorizationCatalog{}, DelegationServiceConfig{})
		if err != nil {
			return err
		}
		execution, err := NewExecutionAuthorizationService(tx)
		if err != nil {
			return err
		}
		notebook, err := NewNotebookSessionAuthorizationService(tx)
		if err != nil {
			return err
		}
		for _, consumer := range []struct {
			name  string
			issue func() error
		}{
			{"delegation", func() error {
				_, err := delegation.IssueDelegatedAccessToken(ctx, IssueDelegatedAccessTokenInput{
					SourceAccessToken: sourceToken, Audience: "develop", Scopes: []string{"workflow.run"},
					AgentRunID: "evidence-run", ToolCallID: "evidence-call",
				})
				return err
			}},
			{"execution", func() error {
				_, err := execution.Issue(ctx, IssueExecutionAuthorizationInput{
					SourceAccessToken: sourceToken, Audience: "develop", ExecutionID: uuid.New(),
					Accesses: executionAccessScopes([]int64{engineID}, "read"), ExpiresIn: time.Minute,
				})
				return err
			}},
			{"notebook", func() error {
				_, err := notebook.Issue(ctx, IssueNotebookSessionAuthorizationInput{
					SourceAccessToken: sourceToken, SessionID: uuid.New(), TaskID: 42, ExpiresIn: time.Minute,
				})
				return err
			}},
		} {
			t.Run(consumer.name, func(t *testing.T) {
				err := consumer.issue()
				var validation *CredentialValidationError
				if !errors.As(err, &validation) || !errors.Is(err, commonapi.ErrUnauthorized) || validation.Reason != CredentialInvalidContext || validation.diagnostic == nil {
					t.Fatalf("consumer lost original validation evidence: %v", err)
				}
				d := validation.diagnostic
				if d.condition != "credential_created_after_database" || !d.credentialCreatedAt.Equal(future) ||
					!d.databaseTime.Before(future) || !d.familyAuthenticatedAt.Equal(original.FamilyAuthenticatedAt) ||
					!d.credentialExpiresAt.Equal(credentialExpiry) || !d.familyExpiresAt.Equal(familyExpiry) {
					t.Fatalf("unexpected failed snapshot evidence: %#v", d)
				}
				saved = append(saved, d)
				logCredentialValidationBoundaries(t, err)
			})
		}
		// Wait only for the fixture's declared boundary, then close the transaction.
		return tx.db.Exec(`SELECT pg_sleep(GREATEST(0, EXTRACT(EPOCH FROM (?::timestamptz-clock_timestamp()))))`, future.Add(time.Millisecond)).Error
	})
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := resolveDelegationSourceAccessTokenSnapshot(ctx, repository, sourceToken)
	if err != nil || fresh == nil {
		t.Fatalf("later transaction must now validate the unchanged fixture: %v", err)
	}
	if len(saved) != 3 {
		t.Fatalf("missing consumer evidence: %d", len(saved))
	}
	for _, diagnostic := range saved {
		if !diagnostic.credentialCreatedAt.Equal(future) || !diagnostic.databaseTime.Before(future) {
			t.Fatal("later database state replaced original failure evidence")
		}
	}
}
