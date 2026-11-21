package engineaccess

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/infra/database"
	platformmysql "github.com/addp/common/infra/database/mysql"
	"github.com/addp/common/production"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestSourceGrantWriterAgainstMySQL(t *testing.T) {
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
	defer pool.Close()
	repo := NewRepository(db)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	request := testFulfillmentRequest()
	catalogPath, _, err := request.encode()
	if err != nil {
		t.Fatal(err)
	}
	actor := Actor{TenantID: 7, PrincipalID: 21, MembershipID: 31}
	executionID, resultID := uuid.New(), uuid.New()
	ref := production.Reference{Owner: "transfer", ExecutionID: executionID.String(), ResultID: resultID.String()}
	id := productionGrantCommand(actor.TenantID, ref)
	grant := &sourceGrant{
		RequestID: id, ApprovalMode: productionApprovalMode, TenantID: actor.TenantID, EngineID: int64(request.Path.EngineID),
		CatalogPath: catalogPath, ItemFingerprint: request.ItemFingerprint, BoundaryHash: request.BoundaryHash,
		RecipientID: 41, RecipientType: "user", Action: "read", ExpiryMode: "until_revoked",
		OperatorPrincipalID: 41, OperatorMembershipID: 51, OperatorAuthorizationVersion: 1,
		ProducerOwner: &ref.Owner, ProductionExecutionID: &executionID, ProductionResultID: &resultID,
		IssuerPrincipalID: &actor.PrincipalID, IssuerMembershipID: &actor.MembershipID,
	}
	if err := repo.transaction(ctx, func(tx *Repository) error {
		_, err := tx.insertInitialProductionOutcome(ctx, actor, ref, grant, "")
		return err
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
	uploadID := uuid.New()
	denial := "not_new"
	upload := &uploadGrantOutcome{
		TenantID: actor.TenantID, UploadID: uploadID, ItemFingerprint: request.ItemFingerprint,
		IssuerPrincipalID: actor.PrincipalID, IssuerMembershipID: actor.MembershipID,
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
