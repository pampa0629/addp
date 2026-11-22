package engineaccess

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/infra/database"
	platformmysql "github.com/addp/common/infra/database/mysql"
	"github.com/addp/common/production"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func mysqlSourceGrantRepository(t *testing.T) *Repository {
	t.Helper()
	if os.Getenv("ADDP_TEST_MYSQL_SOURCE_GRANT") != "1" {
		t.Skip("run the disposable platform-source-grant-boundary gate")
	}
	dialect, err := platformmysql.Dialector(database.Config{
		Host: "127.0.0.1", Port: os.Getenv("ADDP_TEST_MYSQL_PORT"), DBName: "system",
		User: "system_gate", Password: os.Getenv("ADDP_TEST_MYSQL_PASSWORD"), ConnectTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(dialect, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close() })
	return NewRepository(db)
}

func TestSourceGrantWriterAgainstMySQL(t *testing.T) {
	repo := mysqlSourceGrantRepository(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	request := testFulfillmentRequest()
	catalogPath, _, err := request.encode()
	if err != nil {
		t.Fatal(err)
	}
	prior, err := repo.findSourceGrant(ctx, uuid.MustParse("00000000-0000-4000-8000-000000000011"))
	if err != nil || prior.IssuerPrincipalID == nil || prior.IssuerMembershipID == nil {
		t.Fatalf("production source fixture: %v", err)
	}
	actor := Actor{TenantID: prior.TenantID, PrincipalID: *prior.IssuerPrincipalID, MembershipID: *prior.IssuerMembershipID}
	executionID, resultID := uuid.New(), uuid.New()
	ref := production.Reference{Owner: "transfer", ExecutionID: executionID.String(), ResultID: resultID.String()}
	id := productionGrantCommand(actor.TenantID, ref)
	grant := &sourceGrant{
		RequestID: id, ApprovalMode: productionApprovalMode, TenantID: actor.TenantID, EngineID: int64(request.Path.EngineID),
		CatalogPath: catalogPath, ItemFingerprint: request.ItemFingerprint, BoundaryHash: request.BoundaryHash,
		RecipientID: prior.OperatorPrincipalID, RecipientType: "user", Action: "read", ExpiryMode: "until_revoked",
		OperatorPrincipalID: prior.OperatorPrincipalID, OperatorMembershipID: prior.OperatorMembershipID, OperatorAuthorizationVersion: prior.OperatorAuthorizationVersion,
		ProducerOwner: &ref.Owner, ProductionExecutionID: &executionID, ProductionResultID: &resultID,
		IssuerPrincipalID: &actor.PrincipalID, IssuerMembershipID: &actor.MembershipID,
	}
	// Exercise the production audit writer after both rows were written in the outer transaction.
	failedRef := ref
	failedRef.ResultID = "00000000-0000-4000-8000-000000000501"
	failedID := uuid.MustParse(failedRef.ResultID)
	failedGrant := *grant
	failedGrant.ProductionResultID = &failedID
	failedGrant.RequestID = productionGrantCommand(actor.TenantID, failedRef)
	err = repo.transaction(ctx, func(tx *Repository) error {
		if _, err := tx.insertInitialProductionOutcome(ctx, actor, failedRef, &failedGrant, ""); err != nil {
			return err
		}
		return tx.auditInitialProduction(ctx, ProductionRuntimeActor{Actor: actor, ClientID: "addp-transfer"}, failedRef, "issued", "")
	})
	if err == nil || !strings.Contains(err.Error(), "gate rejected initial grant audit") {
		t.Fatalf("expected audit rejection after issuance: %v", err)
	}
	var remaining int
	if err := repo.db.Raw("SELECT (SELECT COUNT(*) FROM engine_access_grants WHERE request_id=?) + (SELECT COUNT(*) FROM engine_access_initial_grant_outcomes WHERE result_id=?) + (SELECT COUNT(*) FROM audit_logs WHERE entity_id=?)", failedGrant.RequestID, failedID, failedRef.ResultID).Row().Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("failed audit left %d rows: %v", remaining, err)
	}
	t.Log("audit rejected after both writes; persisted Grant, initial outcome and audit counts are zero")
	if err := repo.transaction(ctx, func(tx *Repository) error {
		if _, err := tx.insertInitialProductionOutcome(ctx, actor, ref, grant, ""); err != nil {
			return err
		}
		return tx.auditInitialProduction(ctx, ProductionRuntimeActor{Actor: actor, ClientID: "addp-transfer"}, ref, "issued", "")
	}); err != nil {
		t.Fatal(err)
	}
	outcome, err := repo.findInitialProductionOutcome(ctx, actor.TenantID, ref)
	if err != nil || outcome.GrantRequestID == nil || *outcome.GrantRequestID != id || outcome.Outcome != "issued" || outcome.RecordedAt.IsZero() {
		t.Fatalf("committed production result: %+v, %v", outcome, err)
	}
	if grant.GrantedAt.IsZero() || !json.Valid(grant.CatalogPath) || !strings.Contains(string(grant.CatalogPath), "业务.域") {
		t.Fatalf("Grant readback lost database time or target: %+v", grant)
	}
	managerGrant, err := repo.findSourceGrant(ctx, uuid.MustParse("00000000-0000-4000-8000-000000000012"))
	if err != nil || managerGrant.IssuerPrincipalID == nil || managerGrant.IssuerMembershipID == nil {
		t.Fatalf("upload source fixture: %v", err)
	}
	uploadID := uuid.New()
	denial := "not_new"
	upload := &uploadGrantOutcome{
		TenantID: actor.TenantID, UploadID: uploadID, ItemFingerprint: request.ItemFingerprint,
		IssuerPrincipalID: *managerGrant.IssuerPrincipalID, IssuerMembershipID: *managerGrant.IssuerMembershipID,
		Outcome: "denied", RecordedAt: outcome.RecordedAt, DenialCode: &denial,
	}
	if err := repo.transaction(ctx, func(tx *Repository) error {
		return tx.persistSourceGrant(ctx, nil, upload)
	}); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.uploadOutcome(ctx, actor.TenantID, uploadID, request.ItemFingerprint)
	if err != nil || stored.GrantRequestID != nil || stored.DenialCode == nil || *stored.DenialCode != denial {
		t.Fatalf("committed upload denial: %+v, %v", stored, err)
	}
}

func TestFulfillmentReceiptAgainstMySQL(t *testing.T) {
	repo := mysqlSourceGrantRepository(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	request := testFulfillmentRequest()
	callerID, err := strconv.ParseInt(os.Getenv("ADDP_TEST_CATALOG_PRINCIPAL_ID"), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	request.CallerPrincipalID = callerID
	closeRequest := func(tx *Repository) error {
		_, err := tx.settleFulfillment(ctx, request, false, userProvenance{}, nil)
		return err
	}
	rollback := errors.New("roll back the caller transaction")
	err = repo.transaction(ctx, func(tx *Repository) error {
		if err := closeRequest(tx); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("close before outer rollback: %v", err)
	}
	if _, err := repo.readFulfillment(ctx, request); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("observed rolled-back receipt: %v", err)
	}
	if err := repo.transaction(ctx, closeRequest); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.readFulfillment(ctx, request)
	if err != nil || stored.Outcome != "closed" || stored.Deadline != nil || stored.RecordedAt.IsZero() {
		t.Fatalf("committed receipt: %+v, %v", stored, err)
	}
	if err := repo.transaction(ctx, closeRequest); err != nil {
		t.Fatalf("repeat close: %v", err)
	}
	if _, err := repo.readFulfillmentGrant(ctx, request); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("closed receipt Grant lookup: %v", err)
	}
	request.DecisionID = uuid.New()
	if _, err := repo.readFulfillment(ctx, request); !errors.Is(err, errFulfillmentBinding) {
		t.Fatalf("same request with changed decision: %v", err)
	}
}
