package engineaccess

import (
	"context"
	"testing"
	"time"

	shared "github.com/addp/common/authorization"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Runs inside the registered file-composition gate, with real User credentials,
// commands and PostgreSQL expiry; Meta owns the fixture's formal TIFF boundary.
func exerciseObjectPreviewVisibility(t *testing.T, db *gorm.DB, service *Service, ctx context.Context, actor Actor, reader userProvenance, credential string, target plugin.EngineCatalogPath, facts authorizationfacts.AuthorizationFacts) {
	t.Helper()
	request := shared.SourceMetadataCheckRequest{Targets: []plugin.EngineCatalogPath{target}}
	check := func(visible, readable bool) {
		t.Helper()
		result, err := service.CheckManagerObjectPreview(ctx, credential, request)
		if err != nil || !result.Matches(request) || result.Targets[0].Visible != visible || result.Targets[0].ContentReadable != readable {
			t.Fatalf("object visibility want=%v/%v result=%+v err=%v", visible, readable, result, err)
		}
	}
	check(false, false)
	command := CreateIndependentGrantInput{Actor: actor, EngineID: int64(target.EngineID), RequestID: uuid.New(), CatalogPath: target, ItemFingerprint: facts.ItemFingerprint, ExpectedBoundaryHash: *facts.BoundaryHash, RecipientType: "user", RecipientID: reader.PrincipalID, Action: shared.SourceAccessMetadataRead, ExpiryMode: shared.SharingExpiryUntilRevoked, Reason: "Object metadata visibility fixture"}
	grant, err := service.CreateIndependentGrant(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	check(true, false)
	read := command
	read.RequestID = uuid.New()
	read.Action = shared.SourceAccessRead
	content, err := service.CreateIndependentGrant(ctx, read)
	if err != nil {
		t.Fatal(err)
	}
	check(true, true)
	if _, err := service.RevokeGrant(ctx, RevokeGrantInput{Actor: actor, EngineID: int64(target.EngineID), RequestID: content.RequestID, Reason: "Withdraw object content"}); err != nil {
		t.Fatal(err)
	}
	check(true, false)
	if _, err := service.RevokeGrant(ctx, RevokeGrantInput{Actor: actor, EngineID: int64(target.EngineID), RequestID: grant.RequestID, Reason: "Withdraw object metadata"}); err != nil {
		t.Fatal(err)
	}
	check(false, false)
	// Revoke after Meta facts were obtained but before the final rule snapshot.
	// The same original credential must observe the revocation in this call.
	command.RequestID = uuid.New()
	inflight, err := service.CreateIndependentGrant(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	original := service.authorizationFacts
	service.WithAuthorizationFactsReader(objectPreviewFactsObserver{source: original, after: func() {
		if _, err := service.RevokeGrant(ctx, RevokeGrantInput{Actor: actor, EngineID: int64(target.EngineID), RequestID: inflight.RequestID, Reason: "Withdraw while Meta returns"}); err != nil {
			t.Fatal(err)
		}
	}})
	check(false, false)
	service.WithAuthorizationFactsReader(original)
	for _, action := range []string{shared.SourceAccessMetadataRead, shared.SourceAccessRead} {
		command.Action = action
		var expiry time.Time
		if err := db.Raw("SELECT clock_timestamp()+interval '2 seconds'").Scan(&expiry).Error; err != nil {
			t.Fatal(err)
		}
		command.RequestID = uuid.New()
		command.ExpiryMode = shared.SharingExpiryAtTime
		command.ExpiresAt = &expiry
		if _, err := service.CreateIndependentGrant(ctx, command); err != nil {
			t.Fatal(err)
		}
		check(true, action == shared.SourceAccessRead)
		if err := db.Exec("SELECT pg_sleep(GREATEST(0,EXTRACT(EPOCH FROM (?::timestamptz-clock_timestamp())))+0.03)", expiry).Error; err != nil {
			t.Fatal(err)
		}
		check(false, false)
	}
	t.Log("object preview: no Grant, metadata/read, independent revocation, post-Meta withdrawal and both natural expiries passed with unchanged User credential")
}

type objectPreviewFactsObserver struct {
	source AuthorizationFactsReader
	after  func()
}

func (f objectPreviewFactsObserver) ReadAuthorizationFacts(ctx context.Context, tenant uint, fingerprints []string) (*authorizationfacts.AuthorizationFactsBatch, error) {
	result, err := f.source.ReadAuthorizationFacts(ctx, tenant, fingerprints)
	if err == nil {
		f.after()
	}
	return result, err
}
