package engineaccess

import (
	"context"
	"errors"
	"testing"
	"time"

	shared "github.com/addp/common/authorization"
	"github.com/addp/common/dataitem/authorizationfacts/factstest"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/google/uuid"
)

// Formal service commands and repository-rule integration, not filtered Meta
// output or cross-service proof. All mutations retain audit and history checks.
func exerciseMetadataRules(t *testing.T, ctx context.Context, repo *Repository, service *Service, actor Actor,
	path engineplugin.EngineCatalogPath, newUser func(*testing.T, time.Duration) (userProvenance, time.Time),
	grantRead func(*testing.T, string, int64, *time.Time) uuid.UUID,
	revoke func(*testing.T, uuid.UUID)) {
	t.Run("metadata and content rule inclusion", func(t *testing.T) {
		user, _ := newUser(t, time.Hour)
		check := func(action, reason string) *sourceReadRules {
			t.Helper()
			result, err := repo.readCurrentSourceRules(ctx, sourceReadRequest{Action: action, TenantID: actor.TenantID,
				Source: user, Targets: []engineplugin.EngineCatalogPath{path}, Bindings: testReadBindings(path)})
			if err != nil || result == nil || result.Targets[0].Reason != reason || result.Covered != (reason == "grant") {
				t.Fatalf("action=%s want=%s result=%+v err=%v", action, reason, result, err)
			}
			return result
		}
		list := func(expected map[string]string) []SourceGrantRelation {
			t.Helper()
			rows, total, err := repo.listAccountGrantRelations(ctx, actor.TenantID, int64(path.EngineID), user.PrincipalID, 1, 100, SourceGrantFilter{CatalogPath: &path})
			if err != nil || total != int64(len(expected)) || len(rows) != len(expected) {
				t.Fatalf("action-separated list: rows=%+v total=%d expected=%v err=%v", rows, total, expected, err)
			}
			seen := make(map[string]bool)
			for _, row := range rows {
				want, exists := expected[row.Action]
				if !exists || seen[row.Action] || row.Inspection == nil || row.Inspection.Reason != want {
					t.Fatalf("wrong action verdict: row=%+v inspection=%+v expected=%v", row, row.Inspection, expected)
				}
				seen[row.Action] = true
				for _, source := range row.Inspection.Sources {
					if !shared.ValidSourceAccessAction(source.Action) {
						t.Fatalf("source lost actual action: %+v", source)
					}
				}

			}
			for page := 1; page <= len(expected)+1; page++ {
				part, count, err := repo.listAccountGrantRelations(ctx, actor.TenantID, int64(path.EngineID), user.PrincipalID, page, 1, SourceGrantFilter{CatalogPath: &path})
				if err != nil || count != total {
					t.Fatalf("action pagination: %+v total=%d err=%v", part, count, err)
				}
				if page <= len(rows) {
					if len(part) != 1 || part[0].Action != rows[page-1].Action {
						t.Fatalf("unstable action page %d: %+v", page, part)
					}
				} else if len(part) != 0 {
					t.Fatalf("nonempty final page: %+v", part)
				}
			}
			return rows
		}
		check(shared.SourceAccessRead, "no_grant")
		check(shared.SourceAccessMetadataRead, "no_grant")
		list(map[string]string{})
		readID := grantRead(t, "user", user.PrincipalID, nil)
		check(shared.SourceAccessRead, "grant")
		check(shared.SourceAccessMetadataRead, "grant")
		list(map[string]string{shared.SourceAccessRead: "grant"})

		// Deliberately exercise sub-microsecond input on every platform. The
		// public sharing contract truncates it before persistence; both the
		// receipt and current rules must match that exact canonical deadline.
		requestedExpiry := time.Now().UTC().Add(10 * time.Minute).Truncate(time.Second).Add(123456789 * time.Nanosecond)
		expires := requestedExpiry.Truncate(time.Microsecond)
		binding := factstest.Binding(path)
		metadataInput := CreateIndependentGrantInput{Actor: actor, EngineID: int64(path.EngineID),
			RequestID: uuid.New(), CatalogPath: path, ItemFingerprint: binding.ItemFingerprint, ExpectedBoundaryHash: binding.BoundaryHash,
			RecipientType: "user", RecipientID: user.PrincipalID, Action: shared.SourceAccessMetadataRead, ExpiryMode: shared.SharingExpiryAtTime, ExpiresAt: &requestedExpiry, Reason: "Metadata rule fixture"}
		metadata, err := service.CreateIndependentGrant(ctx, metadataInput)
		if err != nil {
			t.Fatal(err)
		}
		if metadata.ExpiresAt == nil || !metadata.ExpiresAt.Equal(expires) {
			t.Fatalf("metadata receipt deadline: input=%s expected=%s returned=%v", requestedExpiry.Format(time.RFC3339Nano), expires.Format(time.RFC3339Nano), metadata.ExpiresAt)
		}
		if metadata.Action != shared.SourceAccessMetadataRead {
			t.Fatal("metadata Grant action was upgraded")
		}
		if replay, err := service.CreateIndependentGrant(ctx, metadataInput); err != nil || replay.RequestID != metadata.RequestID {
			t.Fatalf("metadata replay: %+v %v", replay, err)
		}
		changed := metadataInput
		changed.Action = shared.SourceAccessRead
		if _, err := service.CreateIndependentGrant(ctx, changed); !errors.Is(err, ErrIndependentGrantConflict) {
			t.Fatalf("action substitution did not conflict: %v", err)
		}
		rows := list(map[string]string{shared.SourceAccessRead: "grant", shared.SourceAccessMetadataRead: "grant"})
		if len(rows[0].Inspection.Sources) != 2 {
			t.Fatalf("metadata observation must retain both independent sources: %+v", rows[0].Inspection)
		}
		if observed := check(shared.SourceAccessMetadataRead, "grant"); observed.ExpiresAt == nil || !observed.ExpiresAt.After(expires) {
			t.Fatal("read source must dominate finite metadata deadline within current membership")
		}
		check(shared.SourceAccessRead, "grant")
		revoke(t, readID)
		check(shared.SourceAccessRead, "no_grant")
		check(shared.SourceAccessMetadataRead, "grant")

		readID = grantRead(t, "user", user.PrincipalID, nil)
		check(shared.SourceAccessRead, "grant")
		check(shared.SourceAccessMetadataRead, "grant")
		revoke(t, metadata.RequestID)
		list(map[string]string{shared.SourceAccessRead: "grant"})
		other, _ := newUser(t, time.Hour)
		otherMetadata := metadataInput
		otherMetadata.RequestID = uuid.New()
		otherMetadata.RecipientID = other.PrincipalID
		if _, err := service.CreateIndependentGrant(ctx, otherMetadata); err != nil {
			t.Fatal(err)
		}
		list(map[string]string{shared.SourceAccessRead: "grant"}) // Another account must not add a synthetic metadata relation.
		revoke(t, otherMetadata.RequestID)
		check(shared.SourceAccessMetadataRead, "grant") // Only the current read Grant includes it.
		revoke(t, readID)
		check(shared.SourceAccessMetadataRead, "no_grant")
		check(shared.SourceAccessRead, "no_grant")
	})
}
